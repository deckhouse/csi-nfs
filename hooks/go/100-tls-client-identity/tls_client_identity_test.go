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

package tlsclientidentity

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func certOf(t *testing.T, s string) *x509.Certificate {
	t.Helper()
	b, _ := pem.Decode([]byte(s))
	if b == nil {
		t.Fatal("no PEM")
	}
	c, err := x509.ParseCertificate(b.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestEnsureIssuesAClientCertificateOpenSSLAccepts(t *testing.T) {
	now := time.Date(2026, 11, 5, 18, 0, 0, 0, time.UTC)
	id, err := Ensure(Identity{}, now)
	if err != nil {
		t.Fatal(err)
	}
	ca, crt := certOf(t, id.CA), certOf(t, id.Crt)
	if crt.Subject.String() == crt.Issuer.String() {
		t.Fatalf("the certificate is its own issuer by name, and OpenSSL takes it for a self-signed one: %s", crt.Subject)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ca)
	if _, err := crt.Verify(x509.VerifyOptions{Roots: pool, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatalf("the client certificate does not verify by its CA for client auth: %v", err)
	}
	if dir := os.Getenv("CLIENT_IDENTITY_DUMP"); dir != "" {
		_ = os.WriteFile(filepath.Join(dir, "ca.pem"), []byte(id.CA), 0o600)
		_ = os.WriteFile(filepath.Join(dir, "client.pem"), []byte(id.Crt), 0o600)
	}

	again, err := Ensure(id, now.Add(24*time.Hour))
	if err != nil || again != id {
		t.Fatalf("a valid identity is issued again: %v", err)
	}

	renewed, err := Ensure(id, crt.NotAfter.Add(-renewBefore).Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if renewed.Crt == id.Crt {
		t.Fatal("a certificate due for renewal is kept")
	}
	if certOf(t, renewed.Crt).CheckSignatureFrom(ca) != nil {
		t.Fatal("a renewed certificate is not signed by the CA the servers trust")
	}
	if certOf(t, renewed.Crt).NotAfter.After(ca.NotAfter) {
		t.Fatal("a renewed certificate outlives its CA")
	}

	newCA, err := Ensure(id, ca.NotAfter.Add(-renewBefore).Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if newCA.CA == id.CA {
		t.Fatal("a CA due for renewal is kept")
	}
}

func TestEnsureReplacesABrokenIdentity(t *testing.T) {
	now := time.Date(2026, 11, 5, 18, 0, 0, 0, time.UTC)
	id, err := Ensure(Identity{}, now)
	if err != nil {
		t.Fatal(err)
	}
	other, err := Ensure(Identity{}, now)
	if err != nil {
		t.Fatal(err)
	}
	foreign := id
	foreign.Crt, foreign.Key = other.Crt, other.Key
	fixed, err := Ensure(foreign, now)
	if err != nil {
		t.Fatal(err)
	}
	if fixed.CA != id.CA || certOf(t, fixed.Crt).CheckSignatureFrom(certOf(t, id.CA)) != nil {
		t.Fatal("a certificate of another CA is kept, or the CA is replaced for it")
	}
	if broken, err := Ensure(Identity{CA: "garbage"}, now); err != nil || certOf(t, broken.CA) == nil {
		t.Fatalf("an unreadable CA is not replaced: %v", err)
	}
}
