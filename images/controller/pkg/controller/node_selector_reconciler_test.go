/*
Copyright 2025 Flant JSC

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

package controller_test

import (
	"context"

	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/deckhouse/csi-nfs/images/controller/pkg/controller"
	"github.com/deckhouse/csi-nfs/images/controller/pkg/logger"
)

var _ = Describe(controller.NodeSelectorReconcilerName, func() {
	var (
		ctx                 context.Context
		cl                  client.Client
		clusterWideCl       client.Reader
		log                 logger.Logger
		controllerNamespace string
		testNamespace       string
		nfsSCConfig         NFSStorageClassConfig

		nfsNodeSelectorKey = "storage.deckhouse.io/csi-nfs-node"
		provisionerNFS     = controller.NFSStorageClassProvisioner
		readOnlyFalse      = false
	)

	BeforeEach(func() {
		ctx = context.Background()
		log = logger.Logger{}

		controllerNamespace = "test-controller-ns"
		testNamespace = "test-namespace"

		cl = NewFakeClient()
		clusterWideCl = cl

		nfsSCConfig = NFSStorageClassConfig{
			Name:              "test-nfs-sc",
			Host:              "server",
			Share:             "/share",
			NFSVersion:        "4.1",
			MountMode:         "hard",
			ReadOnly:          readOnlyFalse,
			ReclaimPolicy:     string(corev1.PersistentVolumeReclaimDelete),
			VolumeBindingMode: string(storagev1.VolumeBindingWaitForFirstConsumer),
		}

		Expect(cl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: controllerNamespace}})).To(Succeed())
		Expect(cl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}})).To(Succeed())
	})

	Context("ReconcileNodeSelector() + ReconcileModulePods() Integration", func() {
		It("Scenario 1: NFSStorageClass is missing, some nodes have the csi-nfs label, also csi-nfs-node and csi-controller Pods -> label removed, Pods removed", func() {
			prepareNode(ctx, cl, "node-with-label", map[string]string{"kubernetes.io/os": "linux", nfsNodeSelectorKey: "", "test-label": "value"})
			prepareNode(ctx, cl, "node-without-label", nil)

			// prepare controller node
			prepareNode(ctx, cl, "controller-node", map[string]string{"kubernetes.io/os": "linux", nfsNodeSelectorKey: "", "test-label": "value"})
			makeNodeAsController(ctx, cl, "controller-node", controllerNamespace)

			// csi-nfs-node Pod on node-with-label
			prepareModulePod(ctx, cl, "csi-nfs-node-pod", controllerNamespace, "node-with-label", controller.CSINodeLabel)

			// csi-controller Pod on node-with-label
			prepareModulePod(ctx, cl, "csi-controller-pod", controllerNamespace, "node-with-label", controller.CSIControllerLabel)

			// ReconcileNodeSelector
			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
			Expect(err).NotTo(HaveOccurred())

			checkNodeLabels(ctx, cl, "node-with-label", map[string]string{"kubernetes.io/os": "linux", "test-label": "value"})
			checkNodeLabels(ctx, cl, "node-without-label", nil)
			checkNodeLabels(ctx, cl, "controller-node", map[string]string{"kubernetes.io/os": "linux", "test-label": "value"})

			// ReconcileModulePods
			err = controller.ReconcileModulePods(ctx, cl, clusterWideCl, log, controllerNamespace, controller.NFSNodeSelector, controller.ModulePodSelectorList)
			Expect(err).NotTo(HaveOccurred())

			// Pods removed
			checkRemovedPod(ctx, cl, controllerNamespace, "csi-nfs-node-pod")
			checkRemovedPod(ctx, cl, controllerNamespace, "csi-controller-pod")
		})

		It("Scenario 2: NFSStorageClass exists without nodeSelector; csi-nfs label is added to linux nodes; csi-nfs-node pods remain", func() {
			nsc := generateNFSStorageClass(nfsSCConfig)
			Expect(cl.Create(ctx, nsc)).To(Succeed())

			prepareNode(ctx, cl, "node-without-label-1", map[string]string{"kubernetes.io/os": "linux", "test-label": "value"})
			prepareNode(ctx, cl, "node-without-label-2", nil)
			prepareNode(ctx, cl, "node-without-label-3", map[string]string{"kubernetes.io/os": "linux", "test-label": "value"})

			prepareModulePod(ctx, cl, "csi-nfs-node-1", controllerNamespace, "node-without-label-1", controller.CSINodeLabel)

			// ReconcileNodeSelector
			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
			Expect(err).NotTo(HaveOccurred())

			checkNodeLabels(ctx, cl, "node-without-label-1", map[string]string{"kubernetes.io/os": "linux", "test-label": "value", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "node-without-label-2", nil)
			checkNodeLabels(ctx, cl, "node-without-label-3", map[string]string{"kubernetes.io/os": "linux", "test-label": "value", nfsNodeSelectorKey: ""})

			// ReconcileModulePods
			err = controller.ReconcileModulePods(ctx, cl, clusterWideCl, log, controllerNamespace, controller.NFSNodeSelector, controller.ModulePodSelectorList)
			Expect(err).NotTo(HaveOccurred())

			// csi-nfs-node-1 Pod remains
			checkRemainingPod(ctx, cl, controllerNamespace, "csi-nfs-node-1", "node-without-label-1", controller.CSINodeLabel)
		})

		It("Scenario 3: NFSStorageClass with MatchLabels -> label added to matching nodes, csi-nfs pods removed from non-labeled", func() {
			nfsSCConfig.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"project": "test-1"},
			}
			nsc := generateNFSStorageClass(nfsSCConfig)
			Expect(cl.Create(ctx, nsc)).To(Succeed())

			prepareNode(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "test-label": "value"})
			prepareNode(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-2"})

			prepareModulePod(ctx, cl, "csi-nfs-node-match", controllerNamespace, "matching-node-without-label-1", controller.CSINodeLabel)
			prepareModulePod(ctx, cl, "csi-nfs-node-nonmatch", controllerNamespace, "non-matching-node-without-label-1", controller.CSINodeLabel)

			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
			Expect(err).NotTo(HaveOccurred())

			checkNodeLabels(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-2"})

			// ReconcileModulePods
			err = controller.ReconcileModulePods(ctx, cl, clusterWideCl, log, controllerNamespace, controller.NFSNodeSelector, controller.ModulePodSelectorList)
			Expect(err).NotTo(HaveOccurred())

			// remains
			checkRemainingPod(ctx, cl, controllerNamespace, "csi-nfs-node-match", "matching-node-without-label-1", controller.CSINodeLabel)

			// removed
			checkRemovedPod(ctx, cl, controllerNamespace, "csi-nfs-node-nonmatch")
		})

		It("Scenario 4: NFSStorageClass with MatchExpressions -> label is added to matching nodes, csi-nfs pods removed on unlabeled", func() {
			nfsSCConfig.nodeSelector = metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "project",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"test-1", "test-2"},
					},
				},
			}
			nsc := generateNFSStorageClass(nfsSCConfig)
			Expect(cl.Create(ctx, nsc)).To(Succeed())

			// matching vs. non-matching
			prepareNode(ctx, cl, "matching-node-without-label-4-1", map[string]string{"project": "test-1"})
			prepareNode(ctx, cl, "matching-node-without-label-4-2", map[string]string{"project": "test-2", "role": "something"})
			prepareNode(ctx, cl, "non-matching-node-with-label-4", map[string]string{"project": "test-3", nfsNodeSelectorKey: ""})

			// pods
			prepareModulePod(ctx, cl, "csi-nfs-node-4-match1", controllerNamespace, "matching-node-without-label-4-1", controller.CSINodeLabel)
			prepareModulePod(ctx, cl, "csi-nfs-node-4-match2", controllerNamespace, "matching-node-without-label-4-2", controller.CSINodeLabel)
			prepareModulePod(ctx, cl, "csi-nfs-node-4-nonmatch", controllerNamespace, "non-matching-node-with-label-4", controller.CSINodeLabel)

			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
			Expect(err).NotTo(HaveOccurred())

			checkNodeLabels(ctx, cl, "matching-node-without-label-4-1", map[string]string{"project": "test-1", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "matching-node-without-label-4-2", map[string]string{"project": "test-2", "role": "something", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "non-matching-node-with-label-4", map[string]string{"project": "test-3"})

			err = controller.ReconcileModulePods(ctx, cl, clusterWideCl, log, controllerNamespace, controller.NFSNodeSelector, controller.ModulePodSelectorList)
			Expect(err).NotTo(HaveOccurred())

			// remain
			checkRemainingPod(ctx, cl, controllerNamespace, "csi-nfs-node-4-match1", "matching-node-without-label-4-1", controller.CSINodeLabel)
			checkRemainingPod(ctx, cl, controllerNamespace, "csi-nfs-node-4-match2", "matching-node-without-label-4-2", controller.CSINodeLabel)

			// removed
			checkRemovedPod(ctx, cl, controllerNamespace, "csi-nfs-node-4-nonmatch")
		})

		It("Scenario 5: NFSStorageClass with both MatchExpressions & MatchLabels -> label added to strictly matching nodes, csi-nfs pods removed otherwise", func() {
			nfsSCConfig.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"project": "test-1"},
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "role",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"nfs", "storage"},
					},
				},
			}
			nsc := generateNFSStorageClass(nfsSCConfig)
			Expect(cl.Create(ctx, nsc)).To(Succeed())

			// matching nodes
			prepareNode(ctx, cl, "matching-node-5a", map[string]string{"project": "test-1", "role": "nfs"})
			prepareNode(ctx, cl, "matching-node-5b-controller", map[string]string{"project": "test-1", "role": "storage"})
			// make node-5b-controller a controller node
			makeNodeAsController(ctx, cl, "matching-node-5b-controller", controllerNamespace)

			// partial match or mismatch
			prepareNode(ctx, cl, "non-match-node-5a", map[string]string{"project": "test-2", "role": "nfs", nfsNodeSelectorKey: ""})
			prepareNode(ctx, cl, "non-match-node-5b", map[string]string{"project": "test-1", "role": "worker"})

			// pods
			prepareModulePod(ctx, cl, "csi-nfs-node-5a-match", controllerNamespace, "matching-node-5a", controller.CSINodeLabel)
			prepareModulePod(ctx, cl, "csi-controller-5b-match", controllerNamespace, "matching-node-5b-controller", controller.CSIControllerLabel)
			prepareModulePod(ctx, cl, "csi-nfs-node-5a", controllerNamespace, "non-match-node-5a", controller.CSINodeLabel)
			prepareModulePod(ctx, cl, "csi-nfs-node-5b", controllerNamespace, "non-match-node-5b", controller.CSINodeLabel)

			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
			Expect(err).NotTo(HaveOccurred())

			checkNodeLabels(ctx, cl, "matching-node-5a", map[string]string{"project": "test-1", "role": "nfs", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "matching-node-5b-controller", map[string]string{"project": "test-1", "role": "storage", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "non-match-node-5a", map[string]string{"project": "test-2", "role": "nfs"})
			checkNodeLabels(ctx, cl, "non-match-node-5b", map[string]string{"project": "test-1", "role": "worker"})

			err = controller.ReconcileModulePods(ctx, cl, clusterWideCl, log, controllerNamespace, controller.NFSNodeSelector, controller.ModulePodSelectorList)
			Expect(err).NotTo(HaveOccurred())

			// remains
			checkRemainingPod(ctx, cl, controllerNamespace, "csi-nfs-node-5a-match", "matching-node-5a", controller.CSINodeLabel)
			checkRemainingPod(ctx, cl, controllerNamespace, "csi-controller-5b-match", "matching-node-5b-controller", controller.CSIControllerLabel)

			// removed
			checkRemovedPod(ctx, cl, controllerNamespace, "csi-nfs-node-5a")
			checkRemovedPod(ctx, cl, controllerNamespace, "csi-nfs-node-5b")
		})

		It("Scenario 6: Several NFSStorageClasses exist; one with MatchLabels, one with MatchExpressions, one without selector -> label added to all nodes with Linux OS, csi-nfs pods removed from non-matching", func() {
			// SC #1
			nfsSCConfig1 := nfsSCConfig
			nfsSCConfig1.Name = "test-nfs-sc-1"
			nfsSCConfig1.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"project": "test-1"},
			}
			nsc1 := generateNFSStorageClass(nfsSCConfig1)
			Expect(cl.Create(ctx, nsc1)).To(Succeed())

			// SC #2
			nfsSCConfig2 := nfsSCConfig
			nfsSCConfig2.Name = "test-nfs-sc-2"
			nfsSCConfig2.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"project": "test-2"},
			}
			nsc2 := generateNFSStorageClass(nfsSCConfig2)
			Expect(cl.Create(ctx, nsc2)).To(Succeed())

			// SC #3 no selector
			nfsSCConfig3 := nfsSCConfig
			nfsSCConfig3.Name = "test-nfs-sc-3"
			nsc3 := generateNFSStorageClass(nfsSCConfig3)
			Expect(cl.Create(ctx, nsc3)).To(Succeed())

			// Nodes
			prepareNode(ctx, cl, "matching-node-6-1", map[string]string{"kubernetes.io/os": "linux", "project": "test-1"})
			prepareNode(ctx, cl, "matching-node-6-2", map[string]string{"kubernetes.io/os": "linux", "project": "test-2"})
			prepareNode(ctx, cl, "matching-node-6-3", map[string]string{"kubernetes.io/os": "linux"})
			prepareNode(ctx, cl, "non-matching-node-6", map[string]string{"project": "test-3"})

			// Pods
			prepareModulePod(ctx, cl, "csi-nfs-node-6-match1", controllerNamespace, "matching-node-6-1", controller.CSINodeLabel)
			prepareModulePod(ctx, cl, "csi-nfs-node-6-match2", controllerNamespace, "matching-node-6-2", controller.CSINodeLabel)
			prepareModulePod(ctx, cl, "csi-nfs-node-6-match3", controllerNamespace, "matching-node-6-3", controller.CSINodeLabel)
			prepareModulePod(ctx, cl, "csi-nfs-node-6-nonmatch", controllerNamespace, "non-matching-node-6", controller.CSINodeLabel)

			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
			Expect(err).NotTo(HaveOccurred())

			checkNodeLabels(ctx, cl, "matching-node-6-1", map[string]string{"kubernetes.io/os": "linux", "project": "test-1", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "matching-node-6-2", map[string]string{"kubernetes.io/os": "linux", "project": "test-2", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "matching-node-6-3", map[string]string{"kubernetes.io/os": "linux", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "non-matching-node-6", map[string]string{"project": "test-3"})

			err = controller.ReconcileModulePods(ctx, cl, clusterWideCl, log, controllerNamespace, controller.NFSNodeSelector, controller.ModulePodSelectorList)
			Expect(err).NotTo(HaveOccurred())

			// remain
			checkRemainingPod(ctx, cl, controllerNamespace, "csi-nfs-node-6-match1", "matching-node-6-1", controller.CSINodeLabel)
			checkRemainingPod(ctx, cl, controllerNamespace, "csi-nfs-node-6-match2", "matching-node-6-2", controller.CSINodeLabel)
			checkRemainingPod(ctx, cl, controllerNamespace, "csi-nfs-node-6-match3", "matching-node-6-3", controller.CSINodeLabel)

			// removed
			checkRemovedPod(ctx, cl, controllerNamespace, "csi-nfs-node-6-nonmatch")
		})

		It("Scenario 7: Some nodes have csi-nfs label, some do not -> label removed from nodes that do not match any selector, csi-nfs pods removed if node unlabeled", func() {
			// create SC #1
			nfsSCConfig7a := nfsSCConfig
			nfsSCConfig7a.Name = "test-nfs-sc-7a"
			nfsSCConfig7a.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"project": "test-1"},
			}
			nsc7a := generateNFSStorageClass(nfsSCConfig7a)
			Expect(cl.Create(ctx, nsc7a)).To(Succeed())

			// create SC #2
			nfsSCConfig7b := nfsSCConfig
			nfsSCConfig7b.Name = "test-nfs-sc-7b"
			nfsSCConfig7b.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"project": "test-2"},
			}
			nsc7b := generateNFSStorageClass(nfsSCConfig7b)
			Expect(cl.Create(ctx, nsc7b)).To(Succeed())

			// csi-nfs-labeled nodes, some matching, some not
			prepareNode(ctx, cl, "matching-node-with-label-7-1", map[string]string{"project": "test-1", "role": "nfs", nfsNodeSelectorKey: ""})
			prepareNode(ctx, cl, "matching-node-with-label-7-2", map[string]string{"project": "test-2", nfsNodeSelectorKey: ""})

			prepareNode(ctx, cl, "non-matching-node-with-label-7-1", map[string]string{"project": "test-3", "test-label": "value", nfsNodeSelectorKey: ""})
			prepareNode(ctx, cl, "non-matching-node-with-label-7-2", map[string]string{"role": "dev", "test-label": "value", nfsNodeSelectorKey: ""})

			// unlabeled nodes, some match, some not
			prepareNode(ctx, cl, "matching-node-without-label-7-1", map[string]string{"project": "test-1", "test-label": "value"})
			prepareNode(ctx, cl, "matching-node-without-label-7-2", map[string]string{"project": "test-2", "test-label": "value"})

			prepareNode(ctx, cl, "non-matching-node-without-label-7-1", map[string]string{"project": "test-3", "test-label": "value"})
			prepareNode(ctx, cl, "non-matching-node-without-label-7-2", map[string]string{"role": "dev", "test-label": "value"})

			// csi-nfs-node pods on some labeled nodes
			prepareModulePod(ctx, cl, "csi-nfs-node-7-1a", controllerNamespace, "matching-node-with-label-7-1", controller.CSINodeLabel)
			prepareModulePod(ctx, cl, "csi-nfs-node-7-1b", controllerNamespace, "non-matching-node-with-label-7-1", controller.CSINodeLabel)

			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
			Expect(err).NotTo(HaveOccurred())

			checkNodeLabels(ctx, cl, "matching-node-with-label-7-1", map[string]string{"project": "test-1", "role": "nfs", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "matching-node-with-label-7-2", map[string]string{"project": "test-2", nfsNodeSelectorKey: ""})

			checkNodeLabels(ctx, cl, "non-matching-node-with-label-7-1", map[string]string{"project": "test-3", "test-label": "value"})
			checkNodeLabels(ctx, cl, "non-matching-node-with-label-7-2", map[string]string{"role": "dev", "test-label": "value"})

			checkNodeLabels(ctx, cl, "matching-node-without-label-7-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "matching-node-without-label-7-2", map[string]string{"project": "test-2", "test-label": "value", nfsNodeSelectorKey: ""})

			checkNodeLabels(ctx, cl, "non-matching-node-without-label-7-1", map[string]string{"project": "test-3", "test-label": "value"})
			checkNodeLabels(ctx, cl, "non-matching-node-without-label-7-2", map[string]string{"role": "dev", "test-label": "value"})

			// ReconcileModulePods
			err = controller.ReconcileModulePods(ctx, cl, clusterWideCl, log, controllerNamespace, controller.NFSNodeSelector, controller.ModulePodSelectorList)
			Expect(err).NotTo(HaveOccurred())

			// csi-nfs-node-7-1a on matching => remains
			checkRemainingPod(ctx, cl, controllerNamespace, "csi-nfs-node-7-1a", "matching-node-with-label-7-1", controller.CSINodeLabel)

			// csi-nfs-node-7-1b on non-matching => removed
			checkRemovedPod(ctx, cl, controllerNamespace, "csi-nfs-node-7-1b")
		})

		It("Scenario 9.1: Controller node has pending VolumeSnapshot and csi-controller Pod -> label NOT removed, Pods remain", func() {
			// 1) Create NFSStorageClass with new nodeSelector that the controller node does not match
			nfsSCConfig.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"project": "test-9a"},
			}
			nsc := generateNFSStorageClass(nfsSCConfig)
			Expect(cl.Create(ctx, nsc)).To(Succeed())

			// 2) Create controller node with label, but does NOT match the new selector
			prepareNode(ctx, cl, "controller-node-9a", map[string]string{"project": "something-else", nfsNodeSelectorKey: ""})
			makeNodeAsController(ctx, cl, "controller-node-9a", controllerNamespace)

			// 3) Create a csi-controller Pod on that node
			prepareModulePod(ctx, cl, "csi-controller-9a", controllerNamespace, "controller-node-9a", controller.CSIControllerLabel)

			// 4) Create a csi-nfs-node Pod on that node as well
			prepareModulePod(ctx, cl, "csi-nfs-node-9a", controllerNamespace, "controller-node-9a", controller.CSINodeLabel)

			// 5) Create a pending VolumeSnapshot (not ReadyToUse) to block removal
			prepareVolumeSnapshot(ctx, cl, testNamespace, "vs-9a", provisionerNFS, ptr.To(false))

			// 6) ReconcileNodeSelector -> tries to remove label from controller-node-9a, but pending snapshot => cannot remove
			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
			Expect(err).NotTo(HaveOccurred())

			// label should still exist
			checkNodeLabels(ctx, cl, "controller-node-9a", map[string]string{"project": "something-else", nfsNodeSelectorKey: ""})

			// 7) ReconcileModulePods -> because label remains, csi-controller & csi-nfs-node pods remain
			err = controller.ReconcileModulePods(ctx, cl, clusterWideCl, log, controllerNamespace, controller.NFSNodeSelector, controller.ModulePodSelectorList)
			Expect(err).NotTo(HaveOccurred())

			// csi-controller-9a remains
			checkRemainingPod(ctx, cl, controllerNamespace, "csi-controller-9a", "controller-node-9a", controller.CSIControllerLabel)

			// csi-nfs-node-9a remains
			checkRemainingPod(ctx, cl, controllerNamespace, "csi-nfs-node-9a", "controller-node-9a", controller.CSINodeLabel)
		})

		It("Scenario 9.2: Controller node has pending VolumeSnapshot but NO csi-controller Pod -> label removed, csi-nfs-node Pod on that node is removed", func() {
			// 1) Create NFSStorageClass so node does NOT match
			nfsSCConfig.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"project": "test-9b"},
			}
			nsc := generateNFSStorageClass(nfsSCConfig)
			Expect(cl.Create(ctx, nsc)).To(Succeed())

			// 2) Controller node has label, no csi-controller pod
			prepareNode(ctx, cl, "controller-node-9b", map[string]string{"project": "something-else", nfsNodeSelectorKey: ""})
			makeNodeAsController(ctx, cl, "controller-node-9b", controllerNamespace)

			// 3) Place a csi-nfs-node Pod on that node
			prepareModulePod(ctx, cl, "csi-nfs-node-9b", controllerNamespace, "controller-node-9b", controller.CSINodeLabel)

			// 4) Create a pending VolumeSnapshot
			prepareVolumeSnapshot(ctx, cl, testNamespace, "vs-9b", provisionerNFS, ptr.To(false))

			// NO csi-controller Pod => the logic says if there's no csi-controller pod to remove, the node is removable
			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
			Expect(err).NotTo(HaveOccurred())

			// label is removed
			checkNodeLabels(ctx, cl, "controller-node-9b", map[string]string{"project": "something-else"})

			// ReconcileModulePods -> csi-nfs-node on unlabeled node => removed
			err = controller.ReconcileModulePods(ctx, cl, clusterWideCl, log, controllerNamespace, controller.NFSNodeSelector, controller.ModulePodSelectorList)
			Expect(err).NotTo(HaveOccurred())

			checkRemovedPod(ctx, cl, controllerNamespace, "csi-nfs-node-9b")
		})

		It("Scenario 9.3: Controller node has no pending resources, csi-controller Pod -> label is removed, Pod is removed", func() {
			// 1) NFSStorageClass so node doesn't match
			nfsSCConfig.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"project": "test-9c"},
			}
			nsc := generateNFSStorageClass(nfsSCConfig)
			Expect(cl.Create(ctx, nsc)).To(Succeed())

			// 2) Node has label, csi-controller Pod
			prepareNode(ctx, cl, "controller-node-9c", map[string]string{"project": "other", nfsNodeSelectorKey: ""})
			makeNodeAsController(ctx, cl, "controller-node-9c", controllerNamespace)
			prepareModulePod(ctx, cl, "csi-controller-9c", controllerNamespace, "controller-node-9c", controller.CSIControllerLabel)

			// 3) Create ready to use snapshot
			prepareVolumeSnapshot(ctx, cl, testNamespace, "vs-9c", provisionerNFS, ptr.To(true))

			// 4) No pending PVC or snapshots => removable
			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
			Expect(err).NotTo(HaveOccurred())

			// label removed
			checkNodeLabels(ctx, cl, "controller-node-9c", map[string]string{"project": "other"})

			// ReconcileModulePods => csi-controller Pod removed
			err = controller.ReconcileModulePods(ctx, cl, clusterWideCl, log, controllerNamespace, controller.NFSNodeSelector, controller.ModulePodSelectorList)
			Expect(err).NotTo(HaveOccurred())

			checkRemovedPod(ctx, cl, controllerNamespace, "csi-controller-9c")
		})

		It("Scenario 9.4: Controller node has pending PVC and csi-controller Pod -> label NOT removed, pods remain", func() {
			// 1) nodeSelector not matched
			nfsSCConfig.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"project": "test-9d"},
			}
			nsc := generateNFSStorageClass(nfsSCConfig)
			Expect(cl.Create(ctx, nsc)).To(Succeed())

			// 2) Node + csi-controller Pod
			prepareNode(ctx, cl, "controller-node-9d", map[string]string{"project": "other", nfsNodeSelectorKey: ""})
			makeNodeAsController(ctx, cl, "controller-node-9d", controllerNamespace)
			prepareModulePod(ctx, cl, "csi-controller-9d", controllerNamespace, "controller-node-9d", controller.CSIControllerLabel)

			// 3) Create a pending PVC
			preparePVC(ctx, cl, testNamespace, "pvc-9d", provisionerNFS, v1.ClaimPending)

			// ReconcileNodeSelector => attempt remove label => sees pending PVC => keep label
			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
			Expect(err).NotTo(HaveOccurred())

			// label remains
			checkNodeLabels(ctx, cl, "controller-node-9d", map[string]string{"project": "other", nfsNodeSelectorKey: ""})

			// ReconcileModulePods => csi-controller Pod remains
			err = controller.ReconcileModulePods(ctx, cl, clusterWideCl, log, controllerNamespace, controller.NFSNodeSelector, controller.ModulePodSelectorList)
			Expect(err).NotTo(HaveOccurred())

			checkRemainingPod(ctx, cl, controllerNamespace, "csi-controller-9d", "controller-node-9d", controller.CSIControllerLabel)
		})

		It("Scenario 9.5: Controller node has pending PVC but NO csi-controller Pod -> label removed, csi-nfs-node Pod also removed if present", func() {
			// 1) nodeSelector not matched
			nfsSCConfig.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"project": "test-9e"},
			}
			nsc := generateNFSStorageClass(nfsSCConfig)
			Expect(cl.Create(ctx, nsc)).To(Succeed())

			// 2) Node with label but no csi-controller Pod
			prepareNode(ctx, cl, "controller-node-9e", map[string]string{"project": "other", nfsNodeSelectorKey: ""})
			makeNodeAsController(ctx, cl, "controller-node-9e", controllerNamespace)
			// Instead create a csi-nfs-node Pod
			prepareModulePod(ctx, cl, "csi-nfs-node-9e", controllerNamespace, "controller-node-9e", controller.CSINodeLabel)

			// 3) Pending PVC
			preparePVC(ctx, cl, testNamespace, "pvc-9e", provisionerNFS, v1.ClaimPending)

			// ReconcileNodeSelector => no csi-controller Pod to remove => node is considered removable => label removed
			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
			Expect(err).NotTo(HaveOccurred())

			checkNodeLabels(ctx, cl, "controller-node-9e", map[string]string{"project": "other"})

			// ReconcileModulePods => csi-nfs-node-9e on unlabeled node => removed
			err = controller.ReconcileModulePods(ctx, cl, clusterWideCl, log, controllerNamespace, controller.NFSNodeSelector, controller.ModulePodSelectorList)
			Expect(err).NotTo(HaveOccurred())

			checkRemovedPod(ctx, cl, controllerNamespace, "csi-nfs-node-9e")
		})

		It("Scenario 10: Node not matching selector, not the controller, but has a pod with NFS PVC -> do not remove label, csi-nfs-node remains", func() {
			nfsSCConfig.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"project": "test-10"},
			}
			nsc := generateNFSStorageClass(nfsSCConfig)
			Expect(cl.Create(ctx, nsc)).To(Succeed())

			// node that won't match new selector, but label must remain due to user pod w/ NFS
			prepareNode(ctx, cl, "non-matching-node-10", map[string]string{"project": "other", nfsNodeSelectorKey: ""})

			// user pod with NFS PVC => block label removal
			preparePodWithPVC(ctx, cl, testNamespace, "user-pod-10", "non-matching-node-10", "pvc-10", provisionerNFS)

			// also csi-nfs-node Pod
			prepareModulePod(ctx, cl, "csi-nfs-node-10", controllerNamespace, "non-matching-node-10", controller.CSINodeLabel)

			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
			Expect(err).NotTo(HaveOccurred())

			// label remains
			checkNodeLabels(ctx, cl, "non-matching-node-10", map[string]string{"project": "other", nfsNodeSelectorKey: ""})

			// ReconcileModulePods => csi-nfs-node remains
			err = controller.ReconcileModulePods(ctx, cl, clusterWideCl, log, controllerNamespace, controller.NFSNodeSelector, controller.ModulePodSelectorList)
			Expect(err).NotTo(HaveOccurred())

			checkRemainingPod(ctx, cl, controllerNamespace, "csi-nfs-node-10", "non-matching-node-10", controller.CSINodeLabel)
		})

	})
})

//-------------------------------------------------------------------------------
// Helper functions
//-------------------------------------------------------------------------------

func generateNode(name string, labels map[string]string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func prepareNode(ctx context.Context, cl client.Client, name string, labels map[string]string) {
	node := generateNode(name, labels)
	Expect(cl.Create(ctx, node)).To(Succeed())

	recheckNode := &corev1.Node{}
	Expect(cl.Get(ctx, client.ObjectKey{Name: name}, recheckNode)).To(Succeed())
	if labels != nil {
		Expect(recheckNode.Labels).To(Equal(labels))
	} else {
		Expect(recheckNode.Labels).To(BeEmpty())
	}
}

func checkNodeLabels(ctx context.Context, cl client.Client, name string, labels map[string]string) {
	node := &corev1.Node{}
	Expect(cl.Get(ctx, client.ObjectKey{Name: name}, node)).To(Succeed())
	Expect(node.Labels).To(Equal(labels))
}

func generateModulePod(name, namespace, nodeName string, lbls map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    lbls,
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
		},
	}
}

func prepareModulePod(ctx context.Context, cl client.Client, name, namespace, nodeName string, lbls map[string]string) {
	pod := generateModulePod(name, namespace, nodeName, lbls)
	Expect(cl.Create(ctx, pod)).To(Succeed())

	recheckPod := &corev1.Pod{}
	Expect(cl.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, recheckPod)).To(Succeed())
	Expect(recheckPod.Labels).To(Equal(lbls))
}

func generatePodWithPVC(name, namespace, nodeName, pvcName, _ string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Volumes: []corev1.Volume{
				{
					Name: "test-vol",
					VolumeSource: corev1.VolumeSource{
						PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
							ClaimName: pvcName,
						},
					},
				},
			},
		},
	}
}

func generatePVC(namespace, name, provisioner string, phase v1.PersistentVolumeClaimPhase) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				"volume.kubernetes.io/storage-provisioner": provisioner,
			},
		},
		Spec: v1.PersistentVolumeClaimSpec{},
		Status: v1.PersistentVolumeClaimStatus{
			Phase: phase,
		},
	}
}

func makeNodeAsController(ctx context.Context, cl client.Client, nodeName, controllerNamespace string) {
	lease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: "external-snapshotter-leader-nfs-csi-k8s-io", Namespace: controllerNamespace},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity: ptr.To(nodeName),
		},
	}
	Expect(cl.Create(ctx, lease)).To(Succeed())
}

func prepareVolumeSnapshot(
	ctx context.Context,
	cl client.Client,
	namespace, name, nfsProvisioner string, readyToUse *bool,
) {
	preparePVC(ctx, cl, namespace, "some-pvc", nfsProvisioner, v1.ClaimBound)

	vs := &snapshotv1.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: snapshotv1.VolumeSnapshotSpec{
			Source: snapshotv1.VolumeSnapshotSource{
				PersistentVolumeClaimName: ptr.To("some-pvc"),
			},
		},
		Status: &snapshotv1.VolumeSnapshotStatus{
			ReadyToUse: readyToUse,
		},
	}
	Expect(cl.Create(ctx, vs)).To(Succeed())

	vs = &snapshotv1.VolumeSnapshot{}
	Expect(cl.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, vs)).To(Succeed())
	Expect(vs.Spec.Source.PersistentVolumeClaimName).To(Equal(ptr.To("some-pvc")))
	Expect(*vs.Status.ReadyToUse).To(Equal(*readyToUse))
}

func preparePVC(
	ctx context.Context,
	cl client.Client,
	namespace, name, nfsProvisioner string, phase v1.PersistentVolumeClaimPhase,
) {
	pvc := generatePVC(namespace, name, nfsProvisioner, phase)
	Expect(cl.Create(ctx, pvc)).To(Succeed())

	pvc = &corev1.PersistentVolumeClaim{}
	Expect(cl.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, pvc)).To(Succeed())
	Expect(pvc.Annotations).To(HaveKey("volume.kubernetes.io/storage-provisioner"))
	Expect(pvc.Annotations["volume.kubernetes.io/storage-provisioner"]).To(Equal(nfsProvisioner))
	Expect(pvc.Status.Phase).To(Equal(phase))
}

func preparePodWithPVC(
	ctx context.Context,
	cl client.Client,
	namespace, name, nodeName, pvcName, provisioner string,
) {
	preparePVC(ctx, cl, namespace, pvcName, provisioner, v1.ClaimBound)

	pod := generatePodWithPVC(name, namespace, nodeName, pvcName, provisioner)
	Expect(cl.Create(ctx, pod)).To(Succeed())

	recheckPod := &corev1.Pod{}
	Expect(cl.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, recheckPod)).To(Succeed())
	Expect(recheckPod.Spec.NodeName).To(Equal(nodeName))
	Expect(recheckPod.Spec.Volumes).To(HaveLen(1))
	Expect(recheckPod.Spec.Volumes[0].VolumeSource.PersistentVolumeClaim.ClaimName).To(Equal(pvcName))
}

func checkRemainingPod(ctx context.Context, cl client.Client, namespace, name, nodeName string, labels map[string]string) {
	pod := &corev1.Pod{}
	Expect(cl.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, pod)).To(Succeed())
	Expect(pod.Name).To(Equal(name))
	Expect(pod.Namespace).To(Equal(namespace))
	Expect(pod.Spec.NodeName).To(Equal(nodeName))
	Expect(pod.Labels).To(Equal(labels))
}

func checkRemovedPod(ctx context.Context, cl client.Client, namespace, name string) {
	pod := &corev1.Pod{}
	err := cl.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, pod)
	Expect(err).To(HaveOccurred())
	Expect(k8serrors.IsNotFound(err)).To(BeTrue())
}
