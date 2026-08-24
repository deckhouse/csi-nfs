/*
Copyright 2026 Flant JSC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Internal, like the block holder metrics test next door: what these tests pin are the
// rules the certificate alerts rest on, and they live in unexported functions.
package controller

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/deckhouse/sds-common-lib/logger"
)

const tlsTestNamespace = "d8-csi-nfs"

// Every certificate of a chain gets its own series.
//
// This is the answer to "which certificate do I have to replace": an intermediate that
// expires next week fails the handshake exactly as the leaf would, and a single number
// per kind - a min() over the bundle - reports the same date while naming nothing.
func TestPublishTLSCertificateMetricsPublishesEveryCertificateOfAChain(t *testing.T) {
	resetTLSCertificateMetrics(t)

	leaf := newTestCertificate(t, "nfs-client", time.Now().Add(-time.Hour), time.Now().Add(48*time.Hour))
	intermediate := newTestCertificate(t, "intermediate-ca", time.Now().Add(-time.Hour), time.Now().Add(72*time.Hour))

	cl := newTLSTestClient(t, map[string]string{
		"client.crt": leaf.pem + intermediate.pem,
	})

	if err := publishTLSCertificateMetrics(context.Background(), cl, logger.NewNop(), tlsTestNamespace); err != nil {
		t.Fatalf("publishing the certificate metrics: %v", err)
	}

	if got := testutil.CollectAndCount(tlsCertificateNotAfter); got != 2 {
		t.Fatalf("a chain of two certificates produced %d not_after series, want 2", got)
	}

	for _, certificate := range []testCertificate{leaf, intermediate} {
		labels := prometheus.Labels{
			"kind":    tlsCertificateKindClient,
			"subject": certificate.subject,
			"serial":  certificate.serial,
		}
		if got := testutil.ToFloat64(tlsCertificateNotAfter.With(labels)); got != float64(certificate.notAfter.Unix()) {
			t.Errorf("not_after of %s is %v, want %v", certificate.subject, got, certificate.notAfter.Unix())
		}
		if got := testutil.ToFloat64(tlsCertificateNotBefore.With(labels)); got != float64(certificate.notBefore.Unix()) {
			t.Errorf("not_before of %s is %v, want %v", certificate.subject, got, certificate.notBefore.Unix())
		}
	}
}

// A certificate that has been replaced loses its series.
//
// Nothing else would ever remove it, and while it stays it keeps counting down to a date
// the cluster no longer has a certificate for - so the expiry alert fires over a
// certificate that was replaced in time, which is the one way this alert could teach an
// operator to ignore it.
func TestPublishTLSCertificateMetricsRetiresReplacedCertificates(t *testing.T) {
	resetTLSCertificateMetrics(t)

	old := newTestCertificate(t, "nfs-client", time.Now().Add(-48*time.Hour), time.Now().Add(time.Hour))
	fresh := newTestCertificate(t, "nfs-client", time.Now().Add(-time.Hour), time.Now().Add(8760*time.Hour))

	cl := newTLSTestClient(t, map[string]string{"client.crt": old.pem})
	if err := publishTLSCertificateMetrics(context.Background(), cl, logger.NewNop(), tlsTestNamespace); err != nil {
		t.Fatalf("publishing the certificate metrics: %v", err)
	}

	configMap := &corev1.ConfigMap{}
	if err := cl.Get(context.Background(), client.ObjectKey{Namespace: tlsTestNamespace, Name: tlshdConfigMapName}, configMap); err != nil {
		t.Fatalf("reading back the ConfigMap: %v", err)
	}
	configMap.Data["client.crt"] = fresh.pem
	if err := cl.Update(context.Background(), configMap); err != nil {
		t.Fatalf("replacing the certificate: %v", err)
	}

	if err := publishTLSCertificateMetrics(context.Background(), cl, logger.NewNop(), tlsTestNamespace); err != nil {
		t.Fatalf("publishing the certificate metrics: %v", err)
	}

	if got := testutil.CollectAndCount(tlsCertificateNotAfter); got != 1 {
		t.Fatalf("after a replacement there are %d not_after series, want 1", got)
	}
	labels := prometheus.Labels{"kind": tlsCertificateKindClient, "subject": fresh.subject, "serial": fresh.serial}
	if got := testutil.ToFloat64(tlsCertificateNotAfter.With(labels)); got != float64(fresh.notAfter.Unix()) {
		t.Errorf("the surviving series is not the fresh certificate: not_after %v, want %v", got, fresh.notAfter.Unix())
	}
}

// No ConfigMap means no series at all.
//
// That is how these alerts stay silent on every cluster where RPC-with-TLS is not in use,
// without an absent() arm that would fire wherever the module is merely installed.
func TestPublishTLSCertificateMetricsExportsNothingWithoutTheConfigMap(t *testing.T) {
	resetTLSCertificateMetrics(t)

	withConfigMap := newTLSTestClient(t, map[string]string{
		"ca.crt": newTestCertificate(t, "nfs-ca", time.Now().Add(-time.Hour), time.Now().Add(time.Hour)).pem,
	})
	if err := publishTLSCertificateMetrics(context.Background(), withConfigMap, logger.NewNop(), tlsTestNamespace); err != nil {
		t.Fatalf("publishing the certificate metrics: %v", err)
	}
	if got := testutil.CollectAndCount(tlsCertificateNotAfter); got != 1 {
		t.Fatalf("a configured CA produced %d not_after series, want 1", got)
	}

	empty := fake.NewClientBuilder().WithScheme(tlsTestScheme(t)).Build()
	if err := publishTLSCertificateMetrics(context.Background(), empty, logger.NewNop(), tlsTestNamespace); err != nil {
		t.Fatalf("a missing ConfigMap must not be an error: %v", err)
	}
	if got := testutil.CollectAndCount(tlsCertificateNotAfter); got != 0 {
		t.Errorf("turning TLS off left %d not_after series behind", got)
	}
	if got := testutil.CollectAndCount(tlsCertificateNotBefore); got != 0 {
		t.Errorf("turning TLS off left %d not_before series behind", got)
	}
}

// mtls is optional, so a bundle with only a CA is an ordinary configuration.
func TestPublishTLSCertificateMetricsWithoutAClientCertificate(t *testing.T) {
	resetTLSCertificateMetrics(t)

	ca := newTestCertificate(t, "nfs-ca", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	cl := newTLSTestClient(t, map[string]string{"ca.crt": ca.pem})

	if err := publishTLSCertificateMetrics(context.Background(), cl, logger.NewNop(), tlsTestNamespace); err != nil {
		t.Fatalf("publishing the certificate metrics: %v", err)
	}

	if got := testutil.CollectAndCount(tlsCertificateNotAfter); got != 1 {
		t.Fatalf("a CA-only configuration produced %d not_after series, want 1", got)
	}
	labels := prometheus.Labels{"kind": tlsCertificateKindCA, "subject": ca.subject, "serial": ca.serial}
	if got := testutil.ToFloat64(tlsCertificateNotAfter.With(labels)); got != float64(ca.notAfter.Unix()) {
		t.Errorf("not_after of the CA is %v, want %v", got, ca.notAfter.Unix())
	}
}

// One unreadable block does not cost the readable ones their series, and it does not
// cost the other kind its series either.
func TestPublishTLSCertificateMetricsKeepsGoingPastAnUnreadableBlock(t *testing.T) {
	resetTLSCertificateMetrics(t)

	ca := newTestCertificate(t, "nfs-ca", time.Now().Add(-time.Hour), time.Now().Add(24*time.Hour))
	garbage := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not a certificate")}))

	cl := newTLSTestClient(t, map[string]string{
		"client.crt": garbage,
		"ca.crt":     ca.pem,
	})

	if err := publishTLSCertificateMetrics(context.Background(), cl, logger.NewNop(), tlsTestNamespace); err != nil {
		t.Fatalf("an unparseable certificate must not fail the pass: %v", err)
	}
	if got := testutil.CollectAndCount(tlsCertificateNotAfter); got != 1 {
		t.Fatalf("%d not_after series, want the CA's one", got)
	}
}

// parseCertificates skips what is not a certificate and refuses a bundle that holds none.
func TestParseCertificates(t *testing.T) {
	certificate := newTestCertificate(t, "nfs-client", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	key := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("key material")}))

	certificates, err := parseCertificates(key + certificate.pem)
	if err != nil {
		t.Fatalf("a bundle carrying a key alongside the certificate: %v", err)
	}
	if len(certificates) != 1 {
		t.Fatalf("parsed %d certificates, want 1", len(certificates))
	}

	if _, err := parseCertificates("Um9vdCBjZXJ0aWZpY2F0ZQo="); err == nil {
		t.Error("a bundle with no PEM block at all parsed without an error")
	}
}

// The serial is formatted the way openssl prints it, because the label exists to be
// compared against the file the operator is holding.
func TestCertificateSerialMatchesOpensslFormatting(t *testing.T) {
	certificate := &x509.Certificate{SerialNumber: big.NewInt(0x0abcde)}
	if got := certificateSerial(certificate); got != "ABCDE" {
		t.Errorf("serial %q, want %q", got, "ABCDE")
	}
}

// A certificate that identifies itself through its SANs still gets a subject label.
func TestCertificateSubjectFallsBackToTheDistinguishedName(t *testing.T) {
	certificate := &x509.Certificate{Subject: pkix.Name{Organization: []string{"Flant"}}}
	if got := certificateSubject(certificate); !strings.Contains(got, "Flant") {
		t.Errorf("subject %q says nothing about the certificate", got)
	}
}

type testCertificate struct {
	pem       string
	subject   string
	serial    string
	notBefore time.Time
	notAfter  time.Time
}

// newTestCertificate issues a self-signed certificate with the given validity. ed25519
// because the test does not verify anything with it and the key generation is free.
func newTestCertificate(t *testing.T, commonName string, notBefore, notAfter time.Time) testCertificate {
	t.Helper()

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generating a key: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("generating a serial: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    notBefore,
		NotAfter:     notAfter,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		t.Fatalf("creating a certificate: %v", err)
	}

	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing back the certificate just created: %v", err)
	}

	return testCertificate{
		pem:       string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		subject:   commonName,
		serial:    certificateSerial(parsed),
		notBefore: parsed.NotBefore,
		notAfter:  parsed.NotAfter,
	}
}

func newTLSTestClient(t *testing.T, data map[string]string) client.Client {
	t.Helper()

	return fake.NewClientBuilder().
		WithScheme(tlsTestScheme(t)).
		WithObjects(&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: tlshdConfigMapName, Namespace: tlsTestNamespace},
			Data:       data,
		}).
		Build()
}

func tlsTestScheme(t *testing.T) *apiruntime.Scheme {
	t.Helper()

	scheme := apiruntime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("adding the client-go scheme: %v", err)
	}
	return scheme
}

// resetTLSCertificateMetrics puts the package-level vecs and the bookkeeping of the
// published series back to where a fresh process starts, so that tests sharing this
// registry do not read each other's series.
func resetTLSCertificateMetrics(t *testing.T) {
	t.Helper()

	tlsCertificateNotAfter.Reset()
	tlsCertificateNotBefore.Reset()
	publishedTLSCertificates = map[string]prometheus.Labels{}
}
