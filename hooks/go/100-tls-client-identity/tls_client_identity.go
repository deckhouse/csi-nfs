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

// Package tlsclientidentity issues the client certificate the nodes present
// on mounts with xprtsec=mtls: tlshd holds one client identity for every such
// mount of a node, and an NFS server that requires mTLS has to trust its CA,
// which the controller publishes in the status of the classes with mtls.
//
// The CA and the certificate have different names. OpenSSL, which NFS servers
// such as ganesha verify clients with, takes a certificate whose subject is
// its issuer for a self-signed one and refuses it; the certificate hook of
// module-sdk names both after its CN. A renewed certificate is signed by the
// same CA, so the servers keep trusting the nodes through a renewal.
package tlsclientidentity

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // the subject key identifier of RFC 5280, not a security digest
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/deckhouse/module-sdk/pkg"
	"github.com/deckhouse/module-sdk/pkg/registry"

	"github.com/deckhouse/csi-nfs/hooks/go/consts"
)

const (
	// ClientCN is the common name of the client certificate of the nodes.
	ClientCN = "csi-nfs-client"
	// CACN is the common name of its CA.
	CACN = "csi-nfs-client-ca"
	// SecretName keeps the identity, the key of the CA with it, between runs.
	SecretName = "nfs-client-tls"

	// The CA outlives the certificates it signs twice over, so that a
	// certificate is renewed by the same CA and the servers that trust it keep
	// trusting the nodes.
	caValidity  = 20 * 365 * 24 * time.Hour
	validity    = 10 * 365 * 24 * time.Hour
	renewBefore = 182 * 24 * time.Hour
	backdate    = time.Hour

	snapshotName = "client-identity"
)

// ValuesPath is where the templates read the identity from.
var ValuesPath = fmt.Sprintf("%s.internal.clientTLS", consts.ModuleName)

// Identity is the client identity in PEM, as the values and the Secret keep it.
type Identity struct {
	CA    string `json:"ca"`
	CAKey string `json:"caKey"`
	Crt   string `json:"crt"`
	Key   string `json:"key"`
}

func ptr[T any](v T) *T { return &v }

var _ = registry.RegisterFunc(&pkg.HookConfig{
	OnBeforeHelm: &pkg.OrderedConfig{Order: 10},
	Kubernetes: []pkg.KubernetesConfig{{
		Name:       snapshotName,
		APIVersion: "v1",
		Kind:       "Secret",
		NamespaceSelector: &pkg.NamespaceSelector{
			NameSelector: &pkg.NameSelector{MatchNames: []string{consts.ModuleNamespace}},
		},
		NameSelector:                 &pkg.NameSelector{MatchNames: []string{SecretName}},
		JqFilter:                     `{"ca": (.data["ca.crt"] // "" | @base64d), "caKey": (.data["ca.key"] // "" | @base64d), "crt": (.data["tls.crt"] // "" | @base64d), "key": (.data["tls.key"] // "" | @base64d)}`,
		ExecuteHookOnEvents:          ptr(false),
		ExecuteHookOnSynchronization: ptr(false),
	}},
	Queue: fmt.Sprintf("modules/%s", consts.ModuleName),
}, handle)

func handle(_ context.Context, input *pkg.HookInput) error {
	var current Identity
	if snaps := input.Snapshots.Get(snapshotName); len(snaps) > 0 {
		if err := snaps[0].UnmarshalTo(&current); err != nil {
			return fmt.Errorf("reading Secret %s: %w", SecretName, err)
		}
	}
	if current.CA == "" {
		if v := input.Values.Get(ValuesPath); v.Exists() {
			_ = json.Unmarshal([]byte(v.Raw), &current)
		}
	}
	next, err := Ensure(current, time.Now())
	if err != nil {
		return err
	}
	input.Values.Set(ValuesPath, next)
	return nil
}

// Ensure returns the identity to keep: the current one while it is valid for
// more than renewBefore, a new certificate from the same CA when only the
// certificate is due, and a new CA with a new certificate when the CA is.
func Ensure(current Identity, now time.Time) (Identity, error) {
	ca, caKey, err := parsePair(current.CA, current.CAKey)
	if err != nil || !ca.IsCA || ca.NotAfter.Before(now.Add(renewBefore)) {
		return issueAll(now)
	}
	crt, _, err := parsePair(current.Crt, current.Key)
	if err == nil && crt.Subject.CommonName == ClientCN && crt.NotAfter.After(now.Add(renewBefore)) && crt.CheckSignatureFrom(ca) == nil {
		return current, nil
	}
	crtPEM, keyPEM, err := issueClient(ca, caKey, now)
	if err != nil {
		return Identity{}, err
	}
	current.Crt, current.Key = crtPEM, keyPEM
	return current, nil
}

func issueAll(now time.Time) (Identity, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return Identity{}, fmt.Errorf("generating the key of the client CA: %w", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: CACN},
		NotBefore:             now.Add(-backdate),
		NotAfter:              now.Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
		SubjectKeyId:          keyID(&key.PublicKey),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return Identity{}, fmt.Errorf("issuing the client CA: %w", err)
	}
	ca, _ := x509.ParseCertificate(der)
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return Identity{}, err
	}
	crtPEM, crtKeyPEM, err := issueClient(ca, key, now)
	if err != nil {
		return Identity{}, err
	}
	return Identity{
		CA:    string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		CAKey: string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})),
		Crt:   crtPEM,
		Key:   crtKeyPEM,
	}, nil
}

func issueClient(ca *x509.Certificate, caKey *ecdsa.PrivateKey, now time.Time) (string, string, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", fmt.Errorf("generating the key of the client certificate: %w", err)
	}
	notAfter := now.Add(validity)
	if notAfter.After(ca.NotAfter) {
		notAfter = ca.NotAfter
	}
	tmpl := &x509.Certificate{
		SerialNumber:   serial(),
		Subject:        pkix.Name{CommonName: ClientCN},
		DNSNames:       []string{ClientCN},
		NotBefore:      now.Add(-backdate),
		NotAfter:       notAfter,
		KeyUsage:       x509.KeyUsageDigitalSignature,
		ExtKeyUsage:    []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		SubjectKeyId:   keyID(&key.PublicKey),
		AuthorityKeyId: ca.SubjectKeyId,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return "", "", fmt.Errorf("issuing the client certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})),
		string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})), nil
}

func parsePair(certPEM, keyPEM string) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	cb, _ := pem.Decode([]byte(certPEM))
	kb, _ := pem.Decode([]byte(keyPEM))
	if cb == nil || kb == nil {
		return nil, nil, errors.New("no PEM")
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	key, err := x509.ParseECPrivateKey(kb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	pub, ok := cert.PublicKey.(*ecdsa.PublicKey)
	if !ok || !pub.Equal(&key.PublicKey) {
		return nil, nil, errors.New("the key does not match the certificate")
	}
	return cert, key, nil
}

func serial() *big.Int {
	n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	return n
}

func keyID(pub *ecdsa.PublicKey) []byte {
	der, _ := x509.MarshalPKIXPublicKey(pub)
	sum := sha1.Sum(der) //nolint:gosec // see the import
	return sum[:]
}
