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

package validating

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cn "github.com/deckhouse/csi-nfs/api/v1alpha1"
	feature "github.com/deckhouse/csi-nfs/lib/go/common/pkg/feature"
	d8commonapi "github.com/deckhouse/sds-common-lib/api/v1alpha1"
)

func caPEM(t *testing.T) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test CA"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func moduleConfig(settings map[string]any) *d8commonapi.ModuleConfig {
	return &d8commonapi.ModuleConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "csi-nfs"},
		Spec:       d8commonapi.ModuleConfigSpec{Settings: settings},
	}
}

func tlsClass(ca string) *cn.NFSStorageClass {
	nsc := &cn.NFSStorageClass{
		ObjectMeta: metav1.ObjectMeta{Name: "secure"},
		Spec: cn.NFSStorageClassSpec{
			Connection: &cn.NFSStorageClassConnection{Host: "10.0.0.1", Share: "/", NFSVersion: "4.1", Tls: true},
		},
	}
	if ca != "" {
		nsc.Spec.TLSParameters = &cn.NFSStorageClassTLSParameters{CA: ca}
	}
	return nsc
}

func TestValidateCA(t *testing.T) {
	ca := caPEM(t)
	if err := ValidateCA(base64.StdEncoding.EncodeToString(append(append([]byte{}, ca...), caPEM(t)...))); err != nil {
		t.Fatalf("a bundle of two CAs is refused: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: []byte("key")})
	for name, value := range map[string]string{
		"not Base64":        "%%%",
		"empty":             "",
		"not PEM":           base64.StdEncoding.EncodeToString([]byte("hello")),
		"a key":             base64.StdEncoding.EncodeToString(keyPEM),
		"a CA and garbage":  base64.StdEncoding.EncodeToString(append(append([]byte{}, ca...), []byte("trailing")...)),
		"a broken PEM cert": base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("x")})),
	} {
		if err := ValidateCA(value); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestClassCA(t *testing.T) {
	ca := base64.StdEncoding.EncodeToString(caPEM(t))
	noTLS := moduleConfig(map[string]any{})

	err := ValidateNFSStorageClass(noTLS, tlsClass(ca))
	if !feature.TLSEnabled() {
		if err == nil {
			t.Fatal("an edition without TLS accepts a class with TLS")
		}
		return
	}
	if err != nil {
		t.Fatalf("a class with its own CA needs tlsParameters of the module: %v", err)
	}

	if err := ValidateNFSStorageClass(noTLS, tlsClass("")); err == nil || !strings.Contains(err.Error(), "no tlsParameters.ca of its own") {
		t.Fatalf("a class with TLS and no CA anywhere is accepted: %v", err)
	}
	if err := ValidateNFSStorageClass(moduleConfig(map[string]any{"tlsParameters": map[string]any{"ca": ca}}), tlsClass("")); err != nil {
		t.Fatalf("a class with TLS and the CA of the module is refused: %v", err)
	}
	if err := ValidateNFSStorageClass(noTLS, tlsClass(base64.StdEncoding.EncodeToString([]byte("hello")))); err == nil {
		t.Fatal("a class with a CA that is not a certificate is accepted")
	}

	mtls := tlsClass(ca)
	mtls.Spec.Connection.Mtls = true
	if err := ValidateNFSStorageClass(noTLS, mtls); err == nil || !strings.Contains(err.Error(), "tlsParameters.mtls") {
		t.Fatalf("mtls without the client certificate of the module is accepted: %v", err)
	}
}
