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

// When the RPC-with-TLS certificates of this module stop being valid.
//
// These certificates are the one part of the module nothing rotates. The webhook
// certificates are generated and renewed by a hook; these are typed into the
// ModuleConfig by hand, from somebody else's CA, and they expire on a date nobody in
// the cluster is watching. What that looks like when it happens is not an expiry
// message: the TLS handshake of a new mount fails, existing mounts keep working until
// their next reconnect, and the symptom arrives hours later as mounts that fail on some
// nodes and not others.
//
// The numbers come from the ConfigMap the chart renders, not from the ModuleConfig,
// because the ConfigMap is what tlshd actually reads - in the controller pod and in
// every node pod. A base64 blob that never made it through the template would still be
// in the ModuleConfig and would not be protecting anything.
//
// One series per certificate rather than one number per kind, and that is deliberate.
// Either field may hold a chain, and at handshake time the whole chain has to be valid:
// an intermediate that expires next week breaks the mount exactly as the leaf would. A
// min() over the chain would report the right date and lose the answer to "which
// certificate do I have to replace", which is the only thing the operator needs from it.
//
// The private key is not here. It has no validity period, so there is nothing to expire;
// a key that does not match its certificate is a different failure with a different
// signal, and it shows up as a handshake that fails from the first attempt rather than
// on a date.
package controller

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/deckhouse/csi-nfs/images/controller/pkg/config"
	"github.com/deckhouse/sds-common-lib/logger"
)

const (
	TLSCertificateMetricsName = "nfs-tls-certificate-metrics"

	// tlsCertificateMetricsInterval is how often the certificates are re-read.
	//
	// Generous on purpose: what is being measured moves at one second per second, and
	// the alerts on it are counted in days. The interval that matters is not this one
	// but the one after a change - a certificate replaced in the ModuleConfig is
	// reported within ten minutes, while the pods that mount it are restarted by the
	// checksum annotation immediately, so the metric can lag the fix but never the
	// failure.
	tlsCertificateMetricsInterval = 10 * time.Minute

	// tlshdConfigMapName is the ConfigMap the chart renders the certificates into; see
	// templates/csi/configmap-tlshd.yaml. It exists only when the TLS feature is enabled
	// and a CA has been configured, which is what makes "no series" the right answer on
	// every cluster that does not use RPC-with-TLS.
	tlshdConfigMapName = "tlshd-conf"

	tlsCertificateKindCA     = "ca"
	tlsCertificateKindClient = "client"
)

// tlshdCertificateSources are the keys of that ConfigMap that hold certificates, and the
// kind label each one is published under. A slice and not a map, so a log line naming
// the kind that failed to parse is in the same order on every pass.
var tlshdCertificateSources = []struct {
	key  string
	kind string
}{
	{key: "ca.crt", kind: tlsCertificateKindCA},
	{key: "client.crt", kind: tlsCertificateKindClient},
}

var (
	// tlsCertificateNotAfter is when a certificate stops being valid.
	tlsCertificateNotAfter = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "csi_nfs_tls_certificate_not_after_timestamp_seconds",
		Help: "End of the validity period of an RPC-with-TLS certificate of this module, as a Unix timestamp. " +
			"kind=\"ca\" is the truststore that verifies the NFS server, kind=\"client\" is the certificate the server verifies. " +
			"One series per certificate: either field may hold a chain, and the handshake needs every certificate in it to be valid.",
	}, []string{"kind", "subject", "serial"})

	// tlsCertificateNotBefore is when it starts being valid, which is not a formality:
	// a certificate issued with a notBefore in the future fails the handshake exactly
	// as an expired one does, and it does so on a cluster where somebody has just
	// finished replacing a certificate and has every reason to believe the problem is
	// fixed.
	tlsCertificateNotBefore = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "csi_nfs_tls_certificate_not_before_timestamp_seconds",
		Help: "Start of the validity period of an RPC-with-TLS certificate of this module, as a Unix timestamp. " +
			"A value in the future means the certificate is not usable yet, which fails the handshake the same way an expired one does.",
	}, []string{"kind", "subject", "serial"})
)

func init() {
	metrics.Registry.MustRegister(tlsCertificateNotAfter, tlsCertificateNotBefore)
}

// publishedTLSCertificates is the label set of every series the last pass exported.
//
// Kept because a certificate that has been replaced has to lose its series: nothing else
// would ever remove it, and an old series that stays put keeps counting down to a date
// the cluster no longer has a certificate for - which fires the expiry alert on a
// certificate that was replaced in time.
//
// Deleting the stale ones is not the same as Reset() on the vec, which is the obvious
// shorter way to write this and is wrong: Reset drops every series, including the ones
// about to be set again, so a scrape landing in between reports no certificates at all.
//
// Only the runnable below touches this map, one pass at a time, so it needs no lock.
var publishedTLSCertificates = map[string]prometheus.Labels{}

// RunTLSCertificateMetrics returns the poll as a Runnable for the manager to own.
//
// It reads through the API reader rather than the manager's cache, and that is what keeps
// the RBAC of this controller to `get` on one named ConfigMap. A cached read would build
// an informer, an informer needs `list` and `watch` on every ConfigMap in the namespace,
// and it would hold all of them in memory - to re-read two certificates every ten
// minutes.
//
// Through mgr.Add, so it starts after the manager is up and, by default, on the leader
// only. Leader-only is what the alerts want here: the vec is untouched in the other
// replicas, so they export no series for it rather than a zero, and the rules can be
// written on the raw series with no aggregation to keep a non-leader's zero out of them.
//
// It never returns an error. Failing to read a ConfigMap costs an alert its input for one
// interval; taking the manager down over it would cost the module its storage class
// reconciler.
func RunTLSCertificateMetrics(reader client.Reader, cfg config.Options, log logger.Logger) manager.Runnable {
	return manager.RunnableFunc(func(ctx context.Context) error {
		for {
			if err := publishTLSCertificateMetrics(ctx, reader, log, cfg.ControllerNamespace); err != nil {
				log.Error("Failed to publish the validity of the RPC-with-TLS certificates.", logger.Err(err))
			}

			timer := time.NewTimer(tlsCertificateMetricsInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil
			case <-timer.C:
			}
		}
	})
}

// publishTLSCertificateMetrics re-reads the certificates and republishes their validity.
func publishTLSCertificateMetrics(ctx context.Context, reader client.Reader, log logger.Logger, namespace string) error {
	configMap := &corev1.ConfigMap{}
	err := reader.Get(ctx, client.ObjectKey{Namespace: namespace, Name: tlshdConfigMapName}, configMap)
	switch {
	case apierrors.IsNotFound(err):
		// RPC-with-TLS is not in use on this cluster, or it has just been turned off.
		// Retiring the series is the whole handling: no series means the alerts on them
		// have nothing to evaluate, which is how they stay silent here without an
		// absent() arm that would fire on every cluster the module is merely installed
		// on.
		retireTLSCertificateSeries(nil)
		return nil
	case err != nil:
		// The previous pass's series are deliberately left alone. An API server that did
		// not answer says nothing about the certificates, and blanking them would turn a
		// failed read into "no certificates configured".
		return fmt.Errorf("getting ConfigMap %s/%s: %w", namespace, tlshdConfigMapName, err)
	}

	current := make(map[string]prometheus.Labels)
	for _, source := range tlshdCertificateSources {
		raw, ok := configMap.Data[source.key]
		if !ok || strings.TrimSpace(raw) == "" {
			// mtls is optional, so a missing client.crt is an ordinary configuration and
			// not a fault.
			continue
		}

		certificates, err := parseCertificates(raw)
		if err != nil {
			// Logged rather than returned: the other kind is still worth publishing, and
			// a chain with one unreadable block is still a chain whose readable
			// certificates expire on a date somebody has to know.
			log.Error(fmt.Sprintf("Failed to parse the %s certificate from ConfigMap %s/%s, key %s.",
				source.kind, namespace, tlshdConfigMapName, source.key), logger.Err(err))
		}

		for _, certificate := range certificates {
			labels := prometheus.Labels{
				"kind":    source.kind,
				"subject": certificateSubject(certificate),
				"serial":  certificateSerial(certificate),
			}
			tlsCertificateNotAfter.With(labels).Set(float64(certificate.NotAfter.Unix()))
			tlsCertificateNotBefore.With(labels).Set(float64(certificate.NotBefore.Unix()))
			current[tlsCertificateSeriesKey(labels)] = labels
		}
	}

	retireTLSCertificateSeries(current)
	return nil
}

// retireTLSCertificateSeries removes the series of certificates that are no longer
// configured, and remembers the ones that are.
func retireTLSCertificateSeries(current map[string]prometheus.Labels) {
	for key, labels := range publishedTLSCertificates {
		if _, ok := current[key]; ok {
			continue
		}
		tlsCertificateNotAfter.Delete(labels)
		tlsCertificateNotBefore.Delete(labels)
	}

	if current == nil {
		current = map[string]prometheus.Labels{}
	}
	publishedTLSCertificates = current
}

// tlsCertificateSeriesKey identifies a series among the ones published, which is not the
// same as identifying a certificate: two certificates that agree on all three labels are
// one series to Prometheus, so they have to be one entry here too.
func tlsCertificateSeriesKey(labels prometheus.Labels) string {
	return labels["kind"] + "\x00" + labels["subject"] + "\x00" + labels["serial"]
}

// parseCertificates reads every certificate in a PEM bundle.
//
// Non-certificate blocks are skipped rather than refused: a bundle that also carries a
// key or a CRL is unusual but not wrong, and tlshd reads the certificates out of it the
// same way. An unparseable block is reported and the rest of the bundle is still
// returned - the caller publishes what it got and logs what it did not.
func parseCertificates(raw string) ([]*x509.Certificate, error) {
	var (
		certificates []*x509.Certificate
		failures     []error
	)

	rest := []byte(raw)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			continue
		}

		certificate, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			failures = append(failures, err)
			continue
		}
		certificates = append(certificates, certificate)
	}

	if len(certificates) == 0 && len(failures) == 0 {
		return nil, errors.New("no PEM certificate block found")
	}
	return certificates, errors.Join(failures...)
}

// certificateSubject names the certificate the way its issuer named it, falling back to
// the full distinguished name for the certificates that carry no common name at all and
// identify themselves through their SANs.
func certificateSubject(certificate *x509.Certificate) string {
	if certificate.Subject.CommonName != "" {
		return certificate.Subject.CommonName
	}
	return certificate.Subject.String()
}

// certificateSerial formats the serial as uppercase hexadecimal, which is what
// `openssl x509 -serial` prints: the label is there to be compared against the file the
// operator is holding, and a decimal serial makes that comparison manual arithmetic.
func certificateSerial(certificate *x509.Certificate) string {
	return strings.ToUpper(certificate.SerialNumber.Text(16))
}
