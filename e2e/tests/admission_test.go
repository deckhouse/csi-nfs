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

package tests

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/deckhouse/csi-nfs/e2e/framework"
)

// The CRD's CEL rules, the immutability guards, and the webhook reserving the
// nfs.csi.k8s.io provisioner for the controller. Provisions no volumes.
var _ = Describe("csi-nfs admission", Label("csi-nfs", "admission"), Ordered, func() {
	var (
		env *suiteEnv
		srv framework.Server
	)

	dumpDiagnosticsOnFailure(&env)

	BeforeAll(func() {
		conf := mustLoadConfig()
		// Any plain server will do: the connection details only have to parse.
		srv = serverV42(conf)
		if !srv.Configured() {
			srv = serverV3(conf)
		}
		requireServer(srv)

		env = connectSuite("admission", conf)
	})

	// Rejected resources never exist, so NotFound is the expected outcome.
	deleteNSC := func(name string) {
		ctx, cancel := context.WithTimeout(context.Background(), resourceGoneTimeout)
		defer cancel()
		err := env.Dynamic.Resource(framework.NFSStorageClassGVR).Delete(ctx, name, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			GinkgoWriter.Printf("  warning: cleaning up NFSStorageClass %s: %v\n", name, err)
		}
	}

	It("rejects mtls without tls", func() {
		const name = "e2e-reject-mtls"
		DeferCleanup(deleteNSC, name)

		bad := srv
		bad.Security = framework.SecurityMTLS
		// The combination the CEL rule guards: mtls set, tls not.
		nsc := framework.BuildNFSStorageClass(name, bad, framework.NSCOptions{})
		Expect(unstructured.SetNestedField(nsc.Object, false, "spec", "connection", "tls")).To(Succeed())

		err := framework.CreateNFSStorageClass(env.Ctx, env.Dynamic, nsc)
		Expect(err).To(HaveOccurred(), "mtls without tls must be rejected")
		Expect(err.Error()).To(ContainSubstring("mtls"))
	})

	It("rejects volumeCleanup together with reclaimPolicy Retain", func() {
		const name = "e2e-reject-retain-cleanup"
		DeferCleanup(deleteNSC, name)

		err := framework.CreateNFSStorageClass(env.Ctx, env.Dynamic,
			framework.BuildNFSStorageClass(name, srv, framework.NSCOptions{
				ReclaimPolicy: "Retain",
				VolumeCleanup: "RandomFillSinglePass",
			}))
		Expect(err).To(HaveOccurred(), "Retain together with volumeCleanup must be rejected")
		Expect(err.Error()).To(ContainSubstring("volumeCleanup"))
	})

	It("rejects volumeCleanup Discard below NFSv4.2", func() {
		const name = "e2e-reject-discard"
		DeferCleanup(deleteNSC, name)

		v41 := serverV41(env.Conf)
		if !v41.Configured() {
			Skip("no NFSv4.1 server configured")
		}

		err := framework.CreateNFSStorageClass(env.Ctx, env.Dynamic,
			framework.BuildNFSStorageClass(name, v41, framework.NSCOptions{VolumeCleanup: "Discard"}))
		Expect(err).To(HaveOccurred(), "Discard below NFSv4.2 must be rejected")
		Expect(err.Error()).To(ContainSubstring("Discard"))
	})

	It("keeps spec.connection immutable", func() {
		const name = "e2e-immutable"
		DeferCleanup(deleteNSC, name)

		Expect(framework.CreateNFSStorageClass(env.Ctx, env.Dynamic,
			framework.BuildNFSStorageClass(name, srv, framework.NSCOptions{}))).To(Succeed())
		Expect(framework.WaitNSCCreated(env.Ctx, env.Dynamic, env.Client, name, nscReadyTimeout)).To(Succeed())

		By("trying to repoint the NFSStorageClass at a different host")
		cur, err := env.Dynamic.Resource(framework.NFSStorageClassGVR).Get(env.Ctx, name, metav1.GetOptions{})
		Expect(err).NotTo(HaveOccurred())
		Expect(unstructured.SetNestedField(cur.Object, "192.0.2.1", "spec", "connection", "host")).To(Succeed())

		_, err = env.Dynamic.Resource(framework.NFSStorageClassGVR).Update(env.Ctx, cur, metav1.UpdateOptions{})
		Expect(err).To(HaveOccurred(), "spec.connection must be immutable")
		Expect(err.Error()).To(ContainSubstring("immutable"))
	})

	It("forbids creating a StorageClass with the nfs.csi.k8s.io provisioner directly", func() {
		const name = "e2e-direct-sc"
		DeferCleanup(func(ctx context.Context) {
			_ = env.Client.Delete(ctx, &storagev1.StorageClass{ObjectMeta: metav1.ObjectMeta{Name: name}})
		})

		sc := &storagev1.StorageClass{
			ObjectMeta:  metav1.ObjectMeta{Name: name},
			Provisioner: framework.CSIDriverName,
			Parameters:  map[string]string{"server": srv.Host, "share": srv.Share},
		}
		err := env.Client.Create(env.Ctx, sc)
		Expect(err).To(HaveOccurred(), "only the csi-nfs controller may manage nfs.csi.k8s.io StorageClasses")
		Expect(err.Error()).To(ContainSubstring("NFSStorageClass"))
	})
})
