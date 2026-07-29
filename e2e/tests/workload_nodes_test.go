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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/deckhouse/csi-nfs/e2e/framework"
)

const workloadNodeLabel = "csi-nfs-e2e.storage.deckhouse.io/workload"

// workloadNodes.nodeSelector must confine the CSI node workload - and therefore
// every NFS mount - to the selected nodes, and the scheduler extender must keep Pods
// using those volumes off every other node.
//
// Excluded from the default run: the controller unions the selectors of ALL
// NFSStorageClasses, so one selector-less NFSStorageClass elsewhere in the run would
// re-label every node and void the assertions. Skips itself if any already exists.
var _ = Describe("csi-nfs workloadNodes selector", Label("csi-nfs", "workload-nodes"), Ordered, func() {
	const (
		scName  = "e2e-workload"
		pvcOK   = "workload-ok"
		podOK   = "workload-ok"
		pvcBad  = "workload-denied"
		podBad  = "workload-denied"
		marker  = "csi-nfs-workload-nodes"
		timeout = 5 * time.Minute
	)

	var (
		env *suiteEnv
		srv framework.Server

		allowedNode  string
		excludedNode string
	)

	dumpDiagnosticsOnFailure(&env)

	BeforeAll(func() {
		conf := mustLoadConfig()
		srv = serverV42(conf)
		requireServer(srv)

		env = connectSuite("workload-nodes", conf)

		existing, err := env.Dynamic.Resource(framework.NFSStorageClassGVR).List(env.Ctx, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		if len(existing.Items) > 0 {
			Skip("this scenario needs an otherwise NFSStorageClass-free cluster; " +
				"run it on its own (label filter 'workload-nodes')")
		}

		workers, err := framework.SchedulableWorkers(env.Ctx, env.Client)
		Expect(err).NotTo(HaveOccurred())
		if len(workers) < 2 {
			Skip("need >=2 schedulable worker nodes")
		}
		allowedNode, excludedNode = workers[0], workers[1]

		By("labelling " + allowedNode + " as the only csi-nfs workload node")
		Expect(framework.SetNodeLabel(env.Ctx, env.Client, allowedNode, workloadNodeLabel, "true")).To(Succeed())

		DeferCleanup(func(ctx context.Context) {
			_ = framework.DeletePodWait(ctx, env.Client, env.Namespace(), podBad, podReadyTimeout)
			_ = framework.DeletePVCWait(ctx, env.Client, env.Namespace(), pvcBad, resourceGoneTimeout)
			_ = framework.DeletePodWait(ctx, env.Client, env.Namespace(), podOK, podReadyTimeout)
			_ = framework.DeletePVCWait(ctx, env.Client, env.Namespace(), pvcOK, resourceGoneTimeout)
			_ = framework.DeleteNFSStorageClass(ctx, env.Dynamic, env.Client, scName, resourceGoneTimeout)
			if err := framework.RemoveNodeLabel(ctx, env.Client, allowedNode, workloadNodeLabel); err != nil {
				GinkgoWriter.Printf("  warning: removing %s from %s: %v\n", workloadNodeLabel, allowedNode, err)
			}
		})
	})

	It("confines the csi-nfs node label and DaemonSet to the selected nodes", func() {
		Expect(framework.CreateNFSStorageClass(env.Ctx, env.Dynamic,
			framework.BuildNFSStorageClass(scName, srv, framework.NSCOptions{
				WorkloadNodeSelector: map[string]string{workloadNodeLabel: "true"},
			}))).To(Succeed())
		Expect(framework.WaitNSCCreated(env.Ctx, env.Dynamic, env.Client, scName, nscReadyTimeout)).To(Succeed())

		By("waiting for " + excludedNode + " to lose the csi-nfs node label")
		Eventually(func() (bool, error) {
			return framework.NodeHasLabel(env.Ctx, env.Client, excludedNode, framework.NodeLabelKey)
		}, timeout, framework.PollInterval).Should(BeFalse(),
			"%s must not be labelled %s", excludedNode, framework.NodeLabelKey)

		Eventually(func() (bool, error) {
			return framework.NodeHasLabel(env.Ctx, env.Client, allowedNode, framework.NodeLabelKey)
		}, timeout, framework.PollInterval).Should(BeTrue(),
			"%s must keep %s", allowedNode, framework.NodeLabelKey)

		By("waiting for the CSI node DaemonSet to shrink to the selected node")
		Eventually(func() (int32, error) {
			return framework.CSINodeDesiredCount(env.Ctx, env.Client)
		}, timeout, framework.PollInterval).Should(Equal(int32(1)),
			"the CSI node DaemonSet should only be scheduled on the one selected node")
	})

	It("brings up the scheduler extender", func() {
		// The 080-scheduler-extender-enabler hook switches it on for any non-empty
		// nodeSelector.
		Eventually(func() error {
			var dep appsv1.Deployment
			return env.Client.Get(env.Ctx, client.ObjectKey{
				Namespace: framework.ModuleNamespace,
				Name:      framework.SchedulerExtenderDeploymentName,
			}, &dep)
		}, timeout, framework.PollInterval).Should(Succeed(),
			"Deployment %s/%s should exist once a workloadNodes selector is set",
			framework.ModuleNamespace, framework.SchedulerExtenderDeploymentName)
	})

	It("mounts the volume on a selected node", func() {
		pvc := framework.BuildPVC(env.Namespace(), pvcOK, scName,
			env.Conf.Workload.PVCSize, corev1.ReadWriteMany, nil)
		// Unpinned: the extender should steer it onto the only csi-nfs node.
		pod := framework.BuildProbePod(framework.ProbePodSpec{
			Namespace: env.Namespace(), Name: podOK, PVCName: pvcOK,
			Image: env.Conf.Workload.ProbeImage, Marker: marker, Write: true,
		})
		Expect(framework.ApplyPVCAndPod(env.Ctx, env.Client, pvc, pod, pvcBindTimeout, podReadyTimeout)).To(Succeed())

		landed, err := framework.PodNodeName(env.Ctx, env.Client, env.Namespace(), podOK)
		Expect(err).NotTo(HaveOccurred())
		Expect(landed).To(Equal(allowedNode), "the extender should keep the Pod on the csi-nfs node")
		Expect(framework.VerifyProbeFile(env.Ctx, env.RESTCfg, env.Namespace(), podOK, marker)).To(Succeed())
	})

	It("keeps a Pod pinned to a non-selected node unschedulable", func() {
		pvc := framework.BuildPVC(env.Namespace(), pvcBad, scName,
			env.Conf.Workload.PVCSize, corev1.ReadWriteMany, nil)
		// nodeSelector, not nodeName: nodeName bypasses the extender under test.
		pod := framework.BuildProbePod(framework.ProbePodSpec{
			Namespace: env.Namespace(), Name: podBad, PVCName: pvcBad,
			Image: env.Conf.Workload.ProbeImage, Marker: marker, Node: excludedNode,
		})
		Expect(env.Client.Create(env.Ctx, pvc)).To(Succeed())
		Expect(env.Client.Create(env.Ctx, pod)).To(Succeed())

		By("expecting the Pod to stay Pending because " + excludedNode + " cannot mount csi-nfs volumes")
		Consistently(func() (corev1.PodPhase, error) {
			var p corev1.Pod
			if err := env.Client.Get(env.Ctx, client.ObjectKey{Namespace: env.Namespace(), Name: podBad}, &p); err != nil {
				return "", err
			}
			return p.Status.Phase, nil
		}, 90*time.Second, 10*time.Second).Should(Equal(corev1.PodPending))
	})
})
