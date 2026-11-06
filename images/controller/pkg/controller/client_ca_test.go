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
	"context"
	"encoding/base64"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/deckhouse/csi-nfs/api/v1alpha1"
	commonfeature "github.com/deckhouse/csi-nfs/lib/go/common/pkg/feature"
)

const testClientCA = "-----BEGIN CERTIFICATE-----\nCLIENT CA\n-----END CERTIFICATE-----\n"

func mtlsClass(name string, mtls bool) *v1alpha1.NFSStorageClass {
	return &v1alpha1.NFSStorageClass{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: v1alpha1.NFSStorageClassSpec{Connection: &v1alpha1.NFSStorageClassConnection{
			Host: "nfs.example.com", Share: "/", NFSVersion: "4.1", Tls: true, Mtls: mtls,
		}},
	}
}

func TestPublishClientCA(t *testing.T) {
	if !commonfeature.TLSEnabled() {
		t.Skip("RPC-with-TLS is not in this edition")
	}
	ctx := context.Background()
	withMTLS, withTLS := mtlsClass("with-mtls", true), mtlsClass("with-tls", false)
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: tlshdConfigMapName, Namespace: tlsTestNamespace},
		Data:       map[string]string{clientCAKey: testClientCA},
	}
	scheme := tlsTestScheme(t)
	if err := v1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	cl := fake.NewClientBuilder().WithScheme(scheme).WithObjects(withMTLS, withTLS, cm).
		WithStatusSubresource(&v1alpha1.NFSStorageClass{}).Build()

	for _, nsc := range []*v1alpha1.NFSStorageClass{withMTLS, withTLS} {
		if err := cl.Get(ctx, client.ObjectKeyFromObject(nsc), nsc); err != nil {
			t.Fatal(err)
		}
		if _, err := publishClientCA(ctx, cl, cl, tlsTestNamespace, nsc); err != nil {
			t.Fatalf("publishing the client CA of %s: %v", nsc.Name, err)
		}
	}

	got := &v1alpha1.NFSStorageClass{}
	if err := cl.Get(ctx, client.ObjectKeyFromObject(withMTLS), got); err != nil {
		t.Fatal(err)
	}
	if got.Status == nil || got.Status.ClientCA != base64.StdEncoding.EncodeToString([]byte(testClientCA)) {
		t.Fatalf("a class with mtls does not carry the client CA: %+v", got.Status)
	}
	if err := cl.Get(ctx, client.ObjectKeyFromObject(withTLS), got); err != nil {
		t.Fatal(err)
	}
	if got.Status != nil && got.Status.ClientCA != "" {
		t.Fatalf("a class without mtls carries a client CA: %q", got.Status.ClientCA)
	}

	cm.Data = map[string]string{}
	if err := cl.Update(ctx, cm); err != nil {
		t.Fatal(err)
	}
	if err := cl.Get(ctx, client.ObjectKeyFromObject(withMTLS), withMTLS); err != nil {
		t.Fatal(err)
	}
	next, err := publishClientCA(ctx, cl, cl, tlsTestNamespace, withMTLS)
	if err != nil {
		t.Fatal(err)
	}
	if next != clientCAPendingInterval {
		t.Fatalf("a class with mtls and no CA to publish looks again in %s, not soon", next)
	}
	if err := cl.Get(ctx, client.ObjectKeyFromObject(withMTLS), got); err != nil {
		t.Fatal(err)
	}
	if got.Status.ClientCA != "" {
		t.Fatalf("a client CA the nodes no longer have stays in the status: %q", got.Status.ClientCA)
	}
}

func TestGetSCMountOptionsNoSuidNoDev(t *testing.T) {
	nsc := mtlsClass("hardened", true)
	nsc.Spec.MountOptions = &v1alpha1.NFSStorageClassMountOptions{NoSuid: true, NoDev: true}
	got := GetSCMountOptions(nsc)
	for _, want := range []string{"nosuid", "nodev", "nfsvers=4.1"} {
		if !slices.Contains(got, want) {
			t.Fatalf("mount options %v lack %s", got, want)
		}
	}
	nsc.Spec.MountOptions = &v1alpha1.NFSStorageClassMountOptions{}
	if got := GetSCMountOptions(nsc); slices.Contains(got, "nosuid") || slices.Contains(got, "nodev") {
		t.Fatalf("mount options %v carry nosuid or nodev nobody asked for", got)
	}
}
