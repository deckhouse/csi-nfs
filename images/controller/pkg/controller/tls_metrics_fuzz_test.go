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

package controller

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"strings"
	"testing"
	"time"
)

func fuzzCertificatePEM(f *testing.F, commonName string) string {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		f.Fatalf("generating a key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, public, private)
	if err != nil {
		f.Fatalf("creating a certificate: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func rawCertificates(certificates []*x509.Certificate) [][]byte {
	raw := make([][]byte, 0, len(certificates))
	for _, c := range certificates {
		raw = append(raw, c.Raw)
	}
	return raw
}

func sameRawCertificates(a, b [][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

func FuzzParseCertificates(f *testing.F) {
	ca := fuzzCertificatePEM(f, "nfs-ca")
	client := fuzzCertificatePEM(f, "nfs-client")
	key := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: []byte("key material")}))
	garbage := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not a certificate")}))
	for _, seed := range []string{
		"",
		"Um9vdCBjZXJ0aWZpY2F0ZQo=",
		ca,
		key + client,
		client + ca,
		garbage + ca,
		garbage,
		"text before\n" + ca + "text after\n" + client,
		strings.TrimSuffix(ca, "\n"),
		strings.Replace(ca, "-----END CERTIFICATE-----", "", 1),
	} {
		f.Add(seed)
	}

	noise := "# bundle\n" + string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: []byte{1, 2, 3}}))
	trailer := string(pem.EncodeToMemory(&pem.Block{Type: "X509 CRL", Bytes: []byte{4, 5, 6}}))

	f.Fuzz(func(t *testing.T, bundle string) {
		certificates, err := parseCertificates(bundle)
		for _, c := range certificates {
			if c == nil {
				t.Fatalf("parseCertificates(%q) returned a nil certificate", bundle)
			}
		}
		if len(certificates) == 0 && err == nil {
			t.Fatalf("parseCertificates(%q) returned neither a certificate nor an error", bundle)
		}

		noisy := noise + bundle + "\n" + trailer
		noisyCertificates, noisyErr := parseCertificates(noisy)
		if (err == nil) != (noisyErr == nil) {
			t.Fatalf("blocks that are not certificates changed the outcome: %v without them, %v with them", err, noisyErr)
		}
		raw := rawCertificates(certificates)
		if !sameRawCertificates(raw, rawCertificates(noisyCertificates)) {
			t.Fatalf("blocks that are not certificates changed the certificates: %d without them, %d with them", len(certificates), len(noisyCertificates))
		}

		if len(certificates) == 0 {
			return
		}
		var reencoded strings.Builder
		for _, c := range certificates {
			reencoded.Write(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw}))
		}
		again, err := parseCertificates(reencoded.String())
		if err != nil {
			t.Fatalf("the certificates parsed from %q, encoded again, do not parse: %v", bundle, err)
		}
		if !sameRawCertificates(raw, rawCertificates(again)) {
			t.Fatalf("the certificates changed on a round trip: %d, then %d", len(certificates), len(again))
		}
	})
}
