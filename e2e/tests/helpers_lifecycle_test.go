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

// The expansion target; restore and clone claims use it too so they are never
// smaller than the resized source.
const resizedSize = "2Gi"

// lifecycle threads one volume-lifecycle scenario's state through its steps and
// implements each of them; the scenario files own the It blocks.
type lifecycle struct {
	env *suiteEnv
	srv framework.Server

	scName string
	marker string

	basePVC    string
	basePod    string
	readerPod  string
	restorePVC string
	restorePod string
	clonePVC   string
	clonePod   string
	snapName   string

	// The server-side view; nil for TLS exports, which an in-tree NFS volume cannot
	// mount.
	inspector *framework.Inspector

	baseNode  string
	basePVDir string
}

// Objects are named after the server key so scenarios sharing a namespace never
// collide.
func newLifecycle(env *suiteEnv, srv framework.Server) *lifecycle {
	return &lifecycle{
		env: env, srv: srv,
		scName:  "e2e-" + srv.Key,
		marker:  "csi-nfs-" + srv.Key + "-lifecycle",
		basePVC: srv.Key + "-base", basePod: srv.Key + "-base",
		readerPod:  srv.Key + "-reader",
		restorePVC: srv.Key + "-restore", restorePod: srv.Key + "-restore",
		clonePVC: srv.Key + "-clone", clonePod: srv.Key + "-clone",
		snapName: srv.Key + "-snap",
	}
}

// Mirrors what the module documents for general use.
func (l *lifecycle) nscOptions() framework.NSCOptions {
	return framework.NSCOptions{
		MountMode:        "hard",
		Timeout:          600,
		Retransmissions:  3,
		ChmodPermissions: "0777",
	}
}

// In dependency order; wired through DeferCleanup by the scenario's BeforeAll.
func (l *lifecycle) cleanup() {
	ctx, cancel := context.WithTimeout(context.Background(), resourceGoneTimeout*2)
	defer cancel()

	for _, p := range []string{l.readerPod, l.restorePod, l.clonePod, l.basePod} {
		_ = framework.DeletePodWait(ctx, l.env.Client, l.env.Namespace(), p, podReadyTimeout)
	}
	_ = framework.DeleteSnapshotWait(ctx, l.env.Dynamic, l.env.Namespace(), l.snapName, resourceGoneTimeout)
	for _, p := range []string{l.restorePVC, l.clonePVC, l.basePVC} {
		_ = framework.DeletePVCWait(ctx, l.env.Client, l.env.Namespace(), p, resourceGoneTimeout)
	}
	_ = framework.DeleteNFSStorageClass(ctx, l.env.Dynamic, l.env.Client, l.scName, resourceGoneTimeout)
}

// --- steps ------------------------------------------------------------------

func (l *lifecycle) materialiseStorageClass() {
	GinkgoHelper()
	opts := l.nscOptions()

	By("creating NFSStorageClass " + l.scName + " for " + l.srv.String())
	Expect(framework.CreateNFSStorageClass(l.env.Ctx, l.env.Dynamic,
		framework.BuildNFSStorageClass(l.scName, l.srv, opts))).To(Succeed())
	Expect(framework.WaitNSCCreated(l.env.Ctx, l.env.Dynamic, l.env.Client, l.scName, nscReadyTimeout)).
		To(Succeed(), "NFSStorageClass %s should reach phase Created", l.scName)

	sc, err := framework.GetStorageClass(l.env.Ctx, l.env.Client, l.scName)
	Expect(err).NotTo(HaveOccurred())
	Expect(sc.Provisioner).To(Equal(framework.CSIDriverName))
	Expect(sc.Parameters).To(HaveKeyWithValue("server", l.srv.Host))
	Expect(sc.Parameters).To(HaveKeyWithValue("share", l.srv.Share))
	Expect(sc.AllowVolumeExpansion).NotTo(BeNil())
	Expect(*sc.AllowVolumeExpansion).To(BeTrue())

	want := framework.ExpectedMountOptions(l.srv, opts)
	Expect(framework.ContainsAll(sc.MountOptions, want)).
		To(BeTrue(), "StorageClass mountOptions %v should contain %v", sc.MountOptions, want)

	By("checking the mount-options Secret the StorageClass points the provisioner at")
	secretNS, secretName, err := framework.MountOptionsSecretRef(sc)
	Expect(err).NotTo(HaveOccurred())
	secret, err := framework.GetMountOptionsSecret(l.env.Ctx, l.env.Client, sc)
	Expect(err).NotTo(HaveOccurred(), "Secret %s/%s referenced by StorageClass %s should exist",
		secretNS, secretName, l.scName)
	Expect(secret.Data).NotTo(BeEmpty())
}

func (l *lifecycle) createVolume() {
	GinkgoHelper()

	pvc := framework.BuildPVC(l.env.Namespace(), l.basePVC, l.scName,
		l.env.Conf.Workload.PVCSize, corev1.ReadWriteMany, nil)
	pod := framework.BuildProbePod(framework.ProbePodSpec{
		Namespace: l.env.Namespace(), Name: l.basePod, PVCName: l.basePVC,
		Image: l.env.Conf.Workload.ProbeImage, Marker: l.marker, Write: true,
	})
	Expect(framework.ApplyPVCAndPod(l.env.Ctx, l.env.Client, pvc, pod, pvcBindTimeout, podReadyTimeout)).
		To(Succeed(), "base PVC and Pod should bind and become Ready")
	Expect(framework.VerifyProbeFile(l.env.Ctx, l.env.RESTCfg, l.env.Namespace(), l.basePod, l.marker)).
		To(Succeed(), "the written marker should read back")

	var err error
	l.baseNode, err = framework.PodNodeName(l.env.Ctx, l.env.Client, l.env.Namespace(), l.basePod)
	Expect(err).NotTo(HaveOccurred())
	Expect(l.baseNode).NotTo(BeEmpty())

	l.basePVDir, err = framework.PVName(l.env.Ctx, l.env.Client, l.env.Namespace(), l.basePVC)
	Expect(err).NotTo(HaveOccurred())

	if l.inspector == nil {
		return
	}

	By("checking the driver created " + l.srv.Share + "/" + l.basePVDir + " on the server")
	exists, err := l.inspector.Exists(l.env.Ctx, l.basePVDir)
	Expect(err).NotTo(HaveOccurred())
	Expect(exists).To(BeTrue(), "csi-nfs should create a per-PV subdirectory on the share")

	By("checking chmodPermissions was applied to it")
	mode, err := l.inspector.Mode(l.env.Ctx, l.basePVDir)
	Expect(err).NotTo(HaveOccurred())
	Expect(mode).To(Equal("777"), "chmodPermissions should be applied to the PV directory")

	By("checking the Pod's data is visible on the server itself")
	got, err := l.inspector.ReadFile(l.env.Ctx, l.basePVDir+"/probe.txt")
	Expect(err).NotTo(HaveOccurred())
	Expect(got).To(Equal(l.marker))
}

// NFS has no geometry to grow - the driver just accepts the new size - but
// allowVolumeExpansion is advertised, so the path must complete and keep the data.
func (l *lifecycle) expandVolume() {
	GinkgoHelper()

	Expect(framework.ResizePVC(l.env.Ctx, l.env.Client, l.env.Namespace(), l.basePVC, resizedSize, 10*time.Minute)).
		To(Succeed(), "PVC %s should reach %s", l.basePVC, resizedSize)
	Expect(framework.VerifyProbeFile(l.env.Ctx, l.env.RESTCfg, l.env.Namespace(), l.basePod, l.marker)).
		To(Succeed(), "data should survive the resize")
}

func (l *lifecycle) migratePod() {
	GinkgoHelper()

	other, err := framework.OtherSchedulableWorker(l.env.Ctx, l.env.Client, l.baseNode)
	Expect(err).NotTo(HaveOccurred())
	if other == "" {
		Skip("need >=2 schedulable worker nodes to migrate the Pod")
	}

	By("deleting the original Pod so the share unmounts on " + l.baseNode)
	Expect(framework.DeletePodWait(l.env.Ctx, l.env.Client, l.env.Namespace(), l.basePod, podReadyTimeout)).To(Succeed())

	By("scheduling a new Pod on " + other + " against the same PVC")
	pod := framework.BuildProbePod(framework.ProbePodSpec{
		Namespace: l.env.Namespace(), Name: l.readerPod, PVCName: l.basePVC,
		Image: l.env.Conf.Workload.ProbeImage, Marker: l.marker, Node: other,
	})
	Expect(framework.ApplyPod(l.env.Ctx, l.env.Client, pod, podReadyTimeout)).
		To(Succeed(), "the migrated Pod should become Ready on %s", other)

	landed, err := framework.PodNodeName(l.env.Ctx, l.env.Client, l.env.Namespace(), l.readerPod)
	Expect(err).NotTo(HaveOccurred())
	Expect(landed).To(Equal(other), "the migrated Pod should run on the other node")
	Expect(framework.VerifyProbeFile(l.env.Ctx, l.env.RESTCfg, l.env.Namespace(), l.readerPod, l.marker)).
		To(Succeed(), "data should survive migration")
}

func (l *lifecycle) serveRWXFromTwoNodes() {
	GinkgoHelper()

	workers, err := framework.SchedulableWorkers(l.env.Ctx, l.env.Client)
	Expect(err).NotTo(HaveOccurred())
	if len(workers) < 2 {
		Skip("need >=2 schedulable worker nodes for the RWX multi-node check")
	}
	n1, n2 := workers[0], workers[1]

	rwxPVC := l.srv.Key + "-rwx"
	podA := l.srv.Key + "-rwx-a"
	podB := l.srv.Key + "-rwx-b"
	rwxMarker := "csi-nfs-" + l.srv.Key + "-rwx"

	DeferCleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), resourceGoneTimeout)
		defer cancel()
		_ = framework.DeletePodWait(ctx, l.env.Client, l.env.Namespace(), podA, podReadyTimeout)
		_ = framework.DeletePodWait(ctx, l.env.Client, l.env.Namespace(), podB, podReadyTimeout)
		_ = framework.DeletePVCWait(ctx, l.env.Client, l.env.Namespace(), rwxPVC, resourceGoneTimeout)
	})

	By("mounting an RWX PVC on " + n1 + " (writer) and " + n2 + " (reader) simultaneously")
	pvc := framework.BuildPVC(l.env.Namespace(), rwxPVC, l.scName,
		l.env.Conf.Workload.PVCSize, corev1.ReadWriteMany, nil)
	writer := framework.BuildProbePod(framework.ProbePodSpec{
		Namespace: l.env.Namespace(), Name: podA, PVCName: rwxPVC,
		Image: l.env.Conf.Workload.ProbeImage, Marker: rwxMarker, Write: true, Node: n1,
	})
	Expect(framework.ApplyPVCAndPod(l.env.Ctx, l.env.Client, pvc, writer, pvcBindTimeout, podReadyTimeout)).
		To(Succeed(), "the writer Pod on %s should become Ready", n1)

	reader := framework.BuildProbePod(framework.ProbePodSpec{
		Namespace: l.env.Namespace(), Name: podB, PVCName: rwxPVC,
		Image: l.env.Conf.Workload.ProbeImage, Marker: rwxMarker, Node: n2,
	})
	Expect(framework.ApplyPod(l.env.Ctx, l.env.Client, reader, podReadyTimeout)).
		To(Succeed(), "the reader Pod on %s should mount the same RWX volume", n2)

	Expect(framework.VerifyProbeFile(l.env.Ctx, l.env.RESTCfg, l.env.Namespace(), podB, rwxMarker)).
		To(Succeed(), "the Pod on %s should read the Pod on %s's data while both are mounted", n2, n1)
}

func (l *lifecycle) snapshotAndRestore() {
	GinkgoHelper()

	// Either Pod may already be gone; a missing Pod is success.
	By("stopping the workload before snapshotting (csi-nfs snapshots are tar archives)")
	Expect(framework.DeletePodWait(l.env.Ctx, l.env.Client, l.env.Namespace(), l.readerPod, podReadyTimeout)).To(Succeed())
	Expect(framework.DeletePodWait(l.env.Ctx, l.env.Client, l.env.Namespace(), l.basePod, podReadyTimeout)).To(Succeed())

	snapClass, err := framework.VolumeSnapshotClassFor(l.env.Ctx, l.env.Client, l.scName)
	Expect(err).NotTo(HaveOccurred(), "StorageClass %s should name its VolumeSnapshotClass", l.scName)

	By("creating VolumeSnapshot " + l.snapName + " in class " + snapClass)
	Expect(framework.CreateSnapshot(l.env.Ctx, l.env.Dynamic, l.env.Namespace(),
		l.snapName, l.basePVC, snapClass, snapshotReadyTimeout)).
		To(Succeed(), "snapshot %s should become readyToUse", l.snapName)

	By("restoring a new PVC from the snapshot")
	pvc := framework.BuildPVC(l.env.Namespace(), l.restorePVC, l.scName, resizedSize,
		corev1.ReadWriteMany, framework.SnapshotDataSource(l.snapName))
	pod := framework.BuildProbePod(framework.ProbePodSpec{
		Namespace: l.env.Namespace(), Name: l.restorePod, PVCName: l.restorePVC,
		Image: l.env.Conf.Workload.ProbeImage, Marker: l.marker,
	})
	Expect(framework.ApplyPVCAndPod(l.env.Ctx, l.env.Client, pvc, pod, pvcBindTimeout, podReadyTimeout)).
		To(Succeed(), "the restored PVC and Pod should bind and become Ready")
	Expect(framework.VerifyProbeFile(l.env.Ctx, l.env.RESTCfg, l.env.Namespace(), l.restorePod, l.marker)).
		To(Succeed(), "the restored volume should carry the original data")
}

func (l *lifecycle) cloneFromPVC() {
	GinkgoHelper()

	pvc := framework.BuildPVC(l.env.Namespace(), l.clonePVC, l.scName, resizedSize,
		corev1.ReadWriteMany, framework.PVCDataSource(l.basePVC))
	pod := framework.BuildProbePod(framework.ProbePodSpec{
		Namespace: l.env.Namespace(), Name: l.clonePod, PVCName: l.clonePVC,
		Image: l.env.Conf.Workload.ProbeImage, Marker: l.marker,
	})
	Expect(framework.ApplyPVCAndPod(l.env.Ctx, l.env.Client, pvc, pod, pvcBindTimeout, podReadyTimeout)).
		To(Succeed(), "the cloned PVC and Pod should bind and become Ready")
	Expect(framework.VerifyProbeFile(l.env.Ctx, l.env.RESTCfg, l.env.Namespace(), l.clonePod, l.marker)).
		To(Succeed(), "the cloned volume should carry the source data")
}

func (l *lifecycle) deleteAndReclaim() {
	GinkgoHelper()

	By("deleting the Pods")
	for _, p := range []string{l.readerPod, l.restorePod, l.clonePod, l.basePod} {
		Expect(framework.DeletePodWait(l.env.Ctx, l.env.Client, l.env.Namespace(), p, podReadyTimeout)).To(Succeed())
	}

	By("deleting the VolumeSnapshot")
	Expect(framework.DeleteSnapshotWait(l.env.Ctx, l.env.Dynamic, l.env.Namespace(), l.snapName, resourceGoneTimeout)).
		To(Succeed(), "VolumeSnapshot %s should be deleted", l.snapName)

	By("deleting the PVCs and waiting for them to be reclaimed")
	for _, p := range []string{l.restorePVC, l.clonePVC, l.basePVC} {
		Expect(framework.DeletePVCWait(l.env.Ctx, l.env.Client, l.env.Namespace(), p, resourceGoneTimeout)).
			To(Succeed(), "PVC %s should be deleted", p)
	}

	if l.inspector == nil {
		return
	}
	By("checking the per-PV directory is gone from the share (reclaimPolicy Delete)")
	Eventually(func() (bool, error) {
		return l.inspector.Exists(l.env.Ctx, l.basePVDir)
	}, 3*time.Minute, framework.PollInterval).Should(BeFalse(),
		"csi-nfs should remove %s/%s on Delete", l.srv.Share, l.basePVDir)
}
