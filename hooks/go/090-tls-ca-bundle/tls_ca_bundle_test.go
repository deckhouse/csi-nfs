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

package tlscabundle

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"slices"
	"testing"
	"time"
)

func ca(t *testing.T, name string) []byte {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func b64(parts ...[]byte) string {
	return base64.StdEncoding.EncodeToString(bytes.Join(parts, nil))
}

func subjects(t *testing.T, bundle string) []string {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(bundle)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for rest := data; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			return out
		}
		cert, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, cert.Subject.CommonName)
	}
}

func TestBundle(t *testing.T) {
	module, sdsNFS, other := ca(t, "module"), ca(t, "sds-nfs"), ca(t, "other")

	bundle, skipped := Bundle(b64(module), []ClassCA{
		{Name: "z-plain"},
		{Name: "shared", CA: b64(sdsNFS)},
		{Name: "backup", CA: b64(other, sdsNFS)},
		{Name: "data", CA: b64(sdsNFS)},
	})
	if got := subjects(t, bundle); !slices.Equal(got, []string{"module", "other", "sds-nfs"}) {
		t.Fatalf("the truststore holds every distinct CA once, the module's first, then the classes by name: %v", got)
	}
	if len(skipped) != 0 {
		t.Fatalf("classes skipped: %v", skipped)
	}

	again, _ := Bundle(b64(module), []ClassCA{{Name: "data", CA: b64(sdsNFS)}, {Name: "backup", CA: b64(other, sdsNFS)}})
	if again != bundle {
		t.Fatal("the same CAs in another order render another truststore, and the pods restart for nothing")
	}

	if got := subjects(t, mustBundle(t, "", []ClassCA{{Name: "shared", CA: b64(sdsNFS)}})); !slices.Equal(got, []string{"sds-nfs"}) {
		t.Fatalf("a class CA without the CA of the module: %v", got)
	}
	if empty, _ := Bundle("", []ClassCA{{Name: "plain"}}); empty != "" {
		t.Fatal("no CA anywhere leaves a truststore, and tlshd starts on nodes that need none")
	}

	_, skipped = Bundle("", []ClassCA{{Name: "broken", CA: b64([]byte("hello"))}})
	if !slices.Equal(skipped, []string{"broken"}) {
		t.Fatalf("a class CA with no certificate is not reported: %v", skipped)
	}
}

func TestBundleKeepsTheModuleCAAsGiven(t *testing.T) {
	legacy := []byte("not a PEM certificate\n")
	bundle, _ := Bundle(b64(legacy), nil)
	if data, _ := base64.StdEncoding.DecodeString(bundle); !bytes.Equal(data, legacy) {
		t.Fatalf("the CA of the module is not passed as before: %q", data)
	}
}

func mustBundle(t *testing.T, moduleCA string, classes []ClassCA) string {
	t.Helper()
	bundle, skipped := Bundle(moduleCA, classes)
	if len(skipped) != 0 {
		t.Fatalf("classes skipped: %v", skipped)
	}
	return bundle
}
