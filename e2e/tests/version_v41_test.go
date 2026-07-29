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

	corev1 "k8s.io/api/core/v1"

	"github.com/deckhouse/csi-nfs/e2e/framework"
)

// A reduced round-trip for NFSv4.1, which shares the v4 host with 4.2: the
// requested minor version reaches the wire and a volume round-trips, without
// repeating the steps the 4.2 scenario already covers on the same server.
var _ = Describe("csi-nfs volume round-trip over NFSv4.1", Label("csi-nfs", "version-v41"), Ordered, func() {
	var (
		env *suiteEnv
		srv framework.Server

		scName  = "e2e-v41"
		pvcName = "v41-pvc"
		podName = "v41-pod"
		marker  = "csi-nfs-v41-roundtrip"
	)

	dumpDiagnosticsOnFailure(&env)

	BeforeAll(func() {
		conf := mustLoadConfig()
		srv = serverV41(conf)
		requireServer(srv)

		env = connectSuite("version-v41", conf)

		DeferCleanup(func(ctx context.Context) {
			_ = framework.DeletePodWait(ctx, env.Client, env.Namespace(), podName, podReadyTimeout)
			_ = framework.DeletePVCWait(ctx, env.Client, env.Namespace(), pvcName, resourceGoneTimeout)
			_ = framework.DeleteNFSStorageClass(ctx, env.Dynamic, env.Client, scName, resourceGoneTimeout)
		})
	})

	It("pins the requested NFS version on the StorageClass", func() {
		Expect(framework.CreateNFSStorageClass(env.Ctx, env.Dynamic,
			framework.BuildNFSStorageClass(scName, srv, framework.NSCOptions{}))).To(Succeed())
		Expect(framework.WaitNSCCreated(env.Ctx, env.Dynamic, env.Client, scName, nscReadyTimeout)).To(Succeed())

		sc, err := framework.GetStorageClass(env.Ctx, env.Client, scName)
		Expect(err).NotTo(HaveOccurred())
		Expect(sc.MountOptions).To(ContainElement("nfsvers="+srv.NFSVersion),
			"the StorageClass must pin the requested NFS version")
	})

	It("round-trips data over NFSv4.1", func() {
		pvc := framework.BuildPVC(env.Namespace(), pvcName, scName,
			env.Conf.Workload.PVCSize, corev1.ReadWriteMany, nil)
		pod := framework.BuildProbePod(framework.ProbePodSpec{
			Namespace: env.Namespace(), Name: podName, PVCName: pvcName,
			Image: env.Conf.Workload.ProbeImage, Marker: marker, Write: true,
		})
		Expect(framework.ApplyPVCAndPod(env.Ctx, env.Client, pvc, pod, pvcBindTimeout, podReadyTimeout)).To(Succeed())
		Expect(framework.VerifyProbeFile(env.Ctx, env.RESTCfg, env.Namespace(), podName, marker)).To(Succeed())
	})
})
