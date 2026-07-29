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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"

	"github.com/deckhouse/csi-nfs/e2e/framework"
)

// reclaimPolicy Retain must leave both the PersistentVolume and the data on the
// server behind when the claim goes away. It uses a dedicated export because it
// litters on purpose, which would muddy the lifecycle share's emptiness assertions.
var _ = Describe("csi-nfs reclaimPolicy Retain", Label("csi-nfs", "reclaim-retain"), Ordered, func() {
	var (
		env  *suiteEnv
		srv  framework.Server
		insp *framework.Inspector

		scName  = "e2e-retain"
		pvcName = "retain-pvc"
		podName = "retain-pod"
		marker  = "csi-nfs-retain"

		pvName string
	)

	dumpDiagnosticsOnFailure(&env)

	BeforeAll(func() {
		conf := mustLoadConfig()
		srv = serverV4Alt(conf)
		requireServer(srv)

		env = connectSuite("reclaim-retain", conf)
		insp = newInspector(env, srv)

		DeferCleanup(func(ctx context.Context) {
			_ = framework.DeletePodWait(ctx, env.Client, env.Namespace(), podName, podReadyTimeout)
			_ = framework.DeletePVCWait(ctx, env.Client, env.Namespace(), pvcName, resourceGoneTimeout)
			if pvName != "" {
				// Retain leaves both behind, so the suite owns the cleanup.
				_ = insp.RemoveAll(ctx, pvName)
				_ = framework.DeletePVIfExists(ctx, env.Client, pvName)
			}
			_ = framework.DeleteNFSStorageClass(ctx, env.Dynamic, env.Client, scName, resourceGoneTimeout)
		})
	})

	It("materialises a StorageClass with reclaimPolicy Retain", func() {
		Expect(framework.CreateNFSStorageClass(env.Ctx, env.Dynamic,
			framework.BuildNFSStorageClass(scName, srv, framework.NSCOptions{ReclaimPolicy: "Retain"}))).To(Succeed())
		Expect(framework.WaitNSCCreated(env.Ctx, env.Dynamic, env.Client, scName, nscReadyTimeout)).To(Succeed())

		sc, err := framework.GetStorageClass(env.Ctx, env.Client, scName)
		Expect(err).NotTo(HaveOccurred())
		Expect(sc.ReclaimPolicy).NotTo(BeNil())
		Expect(*sc.ReclaimPolicy).To(Equal(corev1.PersistentVolumeReclaimRetain))
	})

	It("provisions a volume and writes data to it", func() {
		pvc := framework.BuildPVC(env.Namespace(), pvcName, scName,
			env.Conf.Workload.PVCSize, corev1.ReadWriteMany, nil)
		pod := framework.BuildProbePod(framework.ProbePodSpec{
			Namespace: env.Namespace(), Name: podName, PVCName: pvcName,
			Image: env.Conf.Workload.ProbeImage, Marker: marker, Write: true,
		})
		Expect(framework.ApplyPVCAndPod(env.Ctx, env.Client, pvc, pod, pvcBindTimeout, podReadyTimeout)).To(Succeed())

		var err error
		pvName, err = framework.PVName(env.Ctx, env.Client, env.Namespace(), pvcName)
		Expect(err).NotTo(HaveOccurred())
	})

	It("keeps the PV and its data after the PVC is deleted", func() {
		By("deleting the Pod and the PVC")
		Expect(framework.DeletePodWait(env.Ctx, env.Client, env.Namespace(), podName, podReadyTimeout)).To(Succeed())
		Expect(framework.DeletePVCWait(env.Ctx, env.Client, env.Namespace(), pvcName, resourceGoneTimeout)).To(Succeed())

		By("checking the PV stays behind in Released")
		Eventually(func() (corev1.PersistentVolumePhase, error) {
			pv, err := framework.GetPV(env.Ctx, env.Client, pvName)
			if err != nil {
				return "", err
			}
			return pv.Status.Phase, nil
		}, 3*time.Minute, framework.PollInterval).Should(Equal(corev1.VolumeReleased))

		By("checking the directory and its contents are still on the server")
		exists, err := insp.Exists(env.Ctx, pvName)
		Expect(err).NotTo(HaveOccurred())
		Expect(exists).To(BeTrue(), "Retain must not remove %s/%s", srv.Share, pvName)

		got, err := insp.ReadFile(env.Ctx, pvName+"/probe.txt")
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(marker), "Retain must not touch the volume contents")
	})
})
