package controller_test

import (
	"context"

	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"d8-controller/pkg/controller"
	"d8-controller/pkg/logger"
)

var _ = Describe(controller.NodeSelectorReconcilerName, func() {
	var (
		ctx              context.Context
		cl               client.Client
		clusterWideCl    client.Reader
		log              logger.Logger
		testNamespace    string
		configSecretName string

		nfsNodeSelectorKey = "storage.deckhouse.io/csi-nfs-node"
		provisionerNFS     = controller.NFSStorageClassProvisioner
	)

	BeforeEach(func() {
		ctx = context.Background()
		log = logger.Logger{}

		testNamespace = "test-ns"
		configSecretName = "csi-nfs-config"

		cl = NewFakeClient()
		clusterWideCl = cl

		Expect(cl.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: testNamespace}})).To(Succeed())
	})

	Context("ReconcileNodeSelector()", func() {

		It("Scenario 1: Secret is missing -> Expect NotFound error", func() {
			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, testNamespace, configSecretName)
			Expect(err).To(HaveOccurred())
			Expect(k8serrors.IsNotFound(err)).To(BeTrue(), "Should fail with not found when secret does not exist")
		})

		It("Scenario 2: Secret exists but has broken YAML in 'config'", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configSecretName,
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"config": []byte("::: broken ::::"),
				},
			}
			Expect(cl.Create(ctx, secret)).To(Succeed())

			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, testNamespace, configSecretName)
			Expect(err).To(HaveOccurred(), "Should fail when config in secret is not valid YAML")
		})

		It("Scenario 3: Empty user selector -> label all nodes, because empty selector returns all nodes", func() {
			cfgYAML := `
nodeSelector: {}
`
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configSecretName,
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"config": []byte(cfgYAML),
				},
			}
			Expect(cl.Create(ctx, secret)).To(Succeed())

			nodeWithLabel := makeNode("nodeA", map[string]string{
				nfsNodeSelectorKey: "",
			})
			Expect(cl.Create(ctx, nodeWithLabel)).To(Succeed())

			nodeWithoutLabel := makeNode("nodeB", nil)
			Expect(cl.Create(ctx, nodeWithoutLabel)).To(Succeed())

			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, testNamespace, configSecretName)
			Expect(err).NotTo(HaveOccurred())

			recheckNodeA := &corev1.Node{}
			Expect(cl.Get(ctx, client.ObjectKey{Name: "nodeA"}, recheckNodeA)).To(Succeed())
			_, found := recheckNodeA.Labels[nfsNodeSelectorKey]
			Expect(found).To(BeTrue(), "Label should be present on nodeA")

			recheckNodeB := &corev1.Node{}
			Expect(cl.Get(ctx, client.ObjectKey{Name: "nodeB"}, recheckNodeB)).To(Succeed())
			_, found = recheckNodeB.Labels[nfsNodeSelectorKey]
			Expect(found).To(BeTrue(), "Label should be present on nodeB")
		})

		It("Scenario 4: Some nodes match the selector, label is added, some do not match and label is removed", func() {
			cfgYAML := `
nodeSelector:
  myrole: "nfs"
`
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configSecretName,
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"config": []byte(cfgYAML),
				},
			}
			Expect(cl.Create(ctx, secret)).To(Succeed())

			lease := &coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{Name: "external-snapshotter-leader-nfs-csi-k8s-io", Namespace: testNamespace},
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity: ptr.To("nodeCtrl"),
				},
			}
			Expect(cl.Create(ctx, lease)).To(Succeed())

			nodeX := makeNode("nodeX", map[string]string{"myrole": "nfs", nfsNodeSelectorKey: ""})
			Expect(cl.Create(ctx, nodeX)).To(Succeed())

			nodeY := makeNode("nodeY", map[string]string{"myrole": "nfs"})
			Expect(cl.Create(ctx, nodeY)).To(Succeed())

			nodeZ := makeNode("nodeZ", map[string]string{nfsNodeSelectorKey: "", "some-other": "label"})
			Expect(cl.Create(ctx, nodeZ)).To(Succeed())

			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, testNamespace, configSecretName)
			Expect(err).NotTo(HaveOccurred())

			// nodeY should get the label
			recheckY := &corev1.Node{}
			Expect(cl.Get(ctx, client.ObjectKey{Name: "nodeY"}, recheckY)).To(Succeed())
			Expect(recheckY.Labels).To(HaveKey(nfsNodeSelectorKey), "nodeY should receive the label")

			// nodeX already had it, it remains
			recheckX := &corev1.Node{}
			Expect(cl.Get(ctx, client.ObjectKey{Name: "nodeX"}, recheckX)).To(Succeed())
			Expect(recheckX.Labels).To(HaveKey(nfsNodeSelectorKey), "nodeX label remains")

			// nodeZ does not match the selector -> label should be removed
			recheckZ := &corev1.Node{}
			Expect(cl.Get(ctx, client.ObjectKey{Name: "nodeZ"}, recheckZ)).To(Succeed())
			Expect(recheckZ.Labels).NotTo(HaveKey(nfsNodeSelectorKey), "nodeZ label should be removed")
		})

		It("Scenario 5: Removing label from a node with no blocking factors", func() {
			cfgYAML := `
nodeSelector:
  role: "nfs"
`
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configSecretName,
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"config": []byte(cfgYAML),
				},
			}
			Expect(cl.Create(ctx, secret)).To(Succeed())

			lease := &coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{Name: "external-snapshotter-leader-nfs-csi-k8s-io", Namespace: testNamespace},
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity: ptr.To("nodeCtrl"),
				},
			}
			Expect(cl.Create(ctx, lease)).To(Succeed())

			nodeC := makeNode("nodeC", map[string]string{
				nfsNodeSelectorKey: "",
				"role":             "some-other",
			})
			Expect(cl.Create(ctx, nodeC)).To(Succeed())

			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, testNamespace, configSecretName)
			Expect(err).NotTo(HaveOccurred())

			recheckC := &corev1.Node{}
			Expect(cl.Get(ctx, client.ObjectKey{Name: "nodeC"}, recheckC)).To(Succeed())
			Expect(recheckC.Labels).NotTo(HaveKey(nfsNodeSelectorKey))
		})

		Context("Scenario 6: Removing label from controller node with different pending resources", func() {
			It("6.1: Has pending VolumeSnapshot -> do not remove the label", func() {
				cfgYAML := `
nodeSelector:
  role: "nfs"
`
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      configSecretName,
						Namespace: testNamespace,
					},
					Data: map[string][]byte{
						"config": []byte(cfgYAML),
					},
				}

				Expect(cl.Create(ctx, secret)).To(Succeed())
				prepareControllerNodeWithSnapshot(ctx, cl, testNamespace, "ctrl-node1", "vs1", provisionerNFS)

				err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, testNamespace, configSecretName)
				Expect(err).NotTo(HaveOccurred())

				ctrlNode := &corev1.Node{}
				Expect(cl.Get(ctx, client.ObjectKey{Name: "ctrl-node1"}, ctrlNode)).To(Succeed())
				Expect(ctrlNode.Labels).To(HaveKey(nfsNodeSelectorKey), "Should not remove label because of pending snapshot")
			})

			It("6.2: Has pending PVC -> do not remove the label", func() {
				cfgYAML := `
nodeSelector:
  role: "nfs"
`
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      configSecretName,
						Namespace: testNamespace,
					},
					Data: map[string][]byte{
						"config": []byte(cfgYAML),
					},
				}
				Expect(cl.Create(ctx, secret)).To(Succeed())
				prepareControllerNodeWithPendingPVC(ctx, cl, testNamespace, "ctrl-node2", "pvc-test", provisionerNFS)

				err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, testNamespace, configSecretName)
				Expect(err).NotTo(HaveOccurred())

				ctrlNode := &corev1.Node{}
				Expect(cl.Get(ctx, client.ObjectKey{Name: "ctrl-node2"}, ctrlNode)).To(Succeed())
				Expect(ctrlNode.Labels).To(HaveKey(nfsNodeSelectorKey), "Should not remove label because of pending PVC")
			})

			It("6.3: Has both pending VolumeSnapshot and PVC -> do not remove the label", func() {
				cfgYAML := `
nodeSelector:
  role: "nfs"
`
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      configSecretName,
						Namespace: testNamespace,
					},
					Data: map[string][]byte{
						"config": []byte(cfgYAML),
					},
				}
				Expect(cl.Create(ctx, secret)).To(Succeed())
				prepareControllerNodeWithSnapshot(ctx, cl, testNamespace, "ctrl-node3", "vs3", provisionerNFS)
				preparePendingPVC(ctx, cl, testNamespace, "pvc-another", "ctrl-node3", provisionerNFS)

				err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, testNamespace, configSecretName)
				Expect(err).NotTo(HaveOccurred())

				ctrlNode := &corev1.Node{}
				Expect(cl.Get(ctx, client.ObjectKey{Name: "ctrl-node3"}, ctrlNode)).To(Succeed())
				Expect(ctrlNode.Labels).To(HaveKey(nfsNodeSelectorKey), "Should not remove label due to multiple pending resources")
			})
		})

		It("Scenario 7: Node not matching selector, not the controller, but has a pod with NFS PVC -> do not remove the label", func() {
			cfgYAML := `
nodeSelector:
  role: "nfs"
`
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configSecretName,
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"config": []byte(cfgYAML),
				},
			}
			Expect(cl.Create(ctx, secret)).To(Succeed())

			lease := &coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{Name: "external-snapshotter-leader-nfs-csi-k8s-io", Namespace: testNamespace},
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity: ptr.To("nodeA"),
				},
			}
			Expect(cl.Create(ctx, lease)).To(Succeed())

			nodeD := makeNode("nodeD", map[string]string{
				nfsNodeSelectorKey: "",
			})
			Expect(cl.Create(ctx, nodeD)).To(Succeed())

			pvcName := "my-pvc-nfs"
			podD := makePodWithPVC("pod-with-nfs", testNamespace, "nodeD", pvcName, provisionerNFS)
			Expect(cl.Create(ctx, podD)).To(Succeed())

			pvc := makePVC(pvcName, testNamespace, provisionerNFS, v1.ClaimBound)
			Expect(cl.Create(ctx, pvc)).To(Succeed())

			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, testNamespace, configSecretName)
			Expect(err).NotTo(HaveOccurred())

			recheckD := &corev1.Node{}
			Expect(cl.Get(ctx, client.ObjectKey{Name: "nodeD"}, recheckD)).To(Succeed())
			Expect(recheckD.Labels).To(HaveKey(nfsNodeSelectorKey), "Should not remove label because nodeD has a pod with NFS PVC")
		})

		It("Scenario 8: Controller node has no pending PVC/VS -> label is removed", func() {
			cfgYAML := `
nodeSelector:
  somekey: "someval"
`
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configSecretName,
					Namespace: testNamespace,
				},
				Data: map[string][]byte{
					"config": []byte(cfgYAML),
				},
			}
			Expect(cl.Create(ctx, secret)).To(Succeed())

			nodeCtrl := makeNode("nodeCtrl", map[string]string{
				nfsNodeSelectorKey: "",
			})
			Expect(cl.Create(ctx, nodeCtrl)).To(Succeed())

			nodeCtrl = &corev1.Node{}
			Expect(cl.Get(ctx, client.ObjectKey{Name: "nodeCtrl"}, nodeCtrl)).To(Succeed())
			Expect(nodeCtrl.Labels).To(HaveKey(nfsNodeSelectorKey), "Label should be present")

			lease := &coordinationv1.Lease{
				ObjectMeta: metav1.ObjectMeta{Name: "external-snapshotter-leader-nfs-csi-k8s-io", Namespace: testNamespace},
				Spec: coordinationv1.LeaseSpec{
					HolderIdentity: ptr.To("nodeCtrl"),
				},
			}
			Expect(cl.Create(ctx, lease)).To(Succeed())

			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, testNamespace, configSecretName)
			Expect(err).NotTo(HaveOccurred())

			recheckNodeCtrl := &corev1.Node{}
			Expect(cl.Get(ctx, client.ObjectKey{Name: "nodeCtrl"}, recheckNodeCtrl)).To(Succeed())
			Expect(recheckNodeCtrl.Labels).NotTo(HaveKey(nfsNodeSelectorKey), "Label should be removed because there are no blocking factors")
		})

		Context("ReconcileModulePods()", func() {

			It("Scenario 9: Pod csi-nfs-node on a node without the NFS label -> Pod should be deleted", func() {
				nodeWithoutLabel := makeNode("node-no-label", nil)
				Expect(cl.Create(ctx, nodeWithoutLabel)).To(Succeed())

				node := &corev1.Node{}
				Expect(cl.Get(ctx, client.ObjectKey{Name: "node-no-label"}, node)).To(Succeed())
				Expect(node.Labels).NotTo(HaveKey(nfsNodeSelectorKey), "Node must not have the label")

				pod := makeModulePod("csi-nfs-node", testNamespace, "node-no-label", map[string]string{"app": "csi-nfs-node"})
				Expect(cl.Create(ctx, pod)).To(Succeed())

				err := controller.ReconcileModulePods(
					ctx, cl, clusterWideCl, log, testNamespace,
					map[string]string{nfsNodeSelectorKey: ""},
					[]map[string]string{
						{"app": "csi-nfs-controller"},
						{"app": "csi-nfs-node"},
					},
				)
				Expect(err).NotTo(HaveOccurred())

				errGet := cl.Get(ctx, client.ObjectKey{Name: pod.Name, Namespace: pod.Namespace}, &corev1.Pod{})
				Expect(k8serrors.IsNotFound(errGet)).To(BeTrue(), "Pod must be deleted if node has no label")
			})

			It("Scenario 10: csi-nfs-controller Pod on a node without label, no pending PVC/VS -> controller Pod should be deleted", func() {
				nodeNoLabel := makeNode("node-nolabel2", nil)
				Expect(cl.Create(ctx, nodeNoLabel)).To(Succeed())

				node := &corev1.Node{}
				Expect(cl.Get(ctx, client.ObjectKey{Name: "node-nolabel2"}, node)).To(Succeed())
				Expect(node.Labels).NotTo(HaveKey(nfsNodeSelectorKey), "Node must not have the label")

				pod := makeModulePod("csi-nfs-ctrl", testNamespace, "node-nolabel2", map[string]string{"app": "csi-controller"})
				Expect(cl.Create(ctx, pod)).To(Succeed())
				Expect(pod.Labels).To(HaveKey("app"))
				Expect(pod.Labels["app"]).To(Equal("csi-controller"))

				err := controller.ReconcileModulePods(
					ctx, cl, clusterWideCl, log, testNamespace,
					map[string]string{nfsNodeSelectorKey: ""},
					[]map[string]string{
						{"app": "csi-controller"},
						{"app": "csi-nfs-node"},
					},
				)
				Expect(err).NotTo(HaveOccurred())

				errGet := cl.Get(ctx, client.ObjectKey{Name: pod.Name, Namespace: testNamespace}, &corev1.Pod{})
				Expect(k8serrors.IsNotFound(errGet)).To(BeTrue(), "Controller Pod should be deleted if removable and node has no label")
			})

			It("Scenario 11: csi-nfs-controller Pod on a node without label, but has pending PVC -> do not delete controller Pod", func() {
				nodeNoLabel := makeNode("node-nolabel3", nil)
				Expect(cl.Create(ctx, nodeNoLabel)).To(Succeed())

				podCtrl := makeModulePod("csi-nfs-ctrl2", testNamespace, "node-nolabel3", map[string]string{"app": "csi-controller"})
				Expect(cl.Create(ctx, podCtrl)).To(Succeed())

				pvcPending := makePVC("pending-nfs-pvc", testNamespace, provisionerNFS, v1.ClaimPending)
				Expect(cl.Create(ctx, pvcPending)).To(Succeed())

				err := controller.ReconcileModulePods(
					ctx, cl, clusterWideCl, log, testNamespace,
					map[string]string{nfsNodeSelectorKey: ""},
					[]map[string]string{
						{"app": "csi-nfs-controller"},
						{"app": "csi-nfs-node"},
					},
				)
				Expect(err).NotTo(HaveOccurred())

				errGet := cl.Get(ctx, client.ObjectKey{Name: podCtrl.Name, Namespace: testNamespace}, &corev1.Pod{})
				Expect(errGet).NotTo(HaveOccurred(), "Controller Pod should remain because of pending PVC")
			})

			It("Scenario 12: Pod on a correct node with the NFS label -> do nothing", func() {
				nodeCorrect := makeNode("node-correct", map[string]string{nfsNodeSelectorKey: ""})
				Expect(cl.Create(ctx, nodeCorrect)).To(Succeed())

				podCorrect := makeModulePod("csi-node-correct", testNamespace, "node-correct", map[string]string{"app": "csi-nfs-node"})
				Expect(cl.Create(ctx, podCorrect)).To(Succeed())

				err := controller.ReconcileModulePods(
					ctx, cl, clusterWideCl, log, testNamespace,
					map[string]string{nfsNodeSelectorKey: ""},
					[]map[string]string{
						{"app": "csi-nfs-controller"},
						{"app": "csi-nfs-node"},
					},
				)
				Expect(err).NotTo(HaveOccurred())

				errGet := cl.Get(ctx, client.ObjectKey{Name: podCorrect.Name, Namespace: testNamespace}, &corev1.Pod{})
				Expect(errGet).NotTo(HaveOccurred(), "Pod should remain because it's on a proper node with the label")
			})
		})

		Context("Integration tests (both ReconcileNodeSelector and ReconcileModulePods)", func() {

			It("Simple integration: Adding label to correct nodes, removing from incorrect, removing Pod from unlabelled node", func() {
				cfgYAML := `
nodeSelector:
  myrole: "nfs"
`
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      configSecretName,
						Namespace: testNamespace,
					},
					Data: map[string][]byte{"config": []byte(cfgYAML)},
				}
				Expect(cl.Create(ctx, secret)).To(Succeed())

				// nodeA, nodeB - match the selector (myrole=nfs)
				nodeA := makeNode("nodeA", map[string]string{"myrole": "nfs"})
				nodeB := makeNode("nodeB", map[string]string{"myrole": "nfs"})
				Expect(cl.Create(ctx, nodeA)).To(Succeed())
				Expect(cl.Create(ctx, nodeB)).To(Succeed())

				// nodeC - does not match
				nodeC := makeNode("nodeC", nil)
				Expect(cl.Create(ctx, nodeC)).To(Succeed())

				// Pod csi-nfs-node on nodeC (which does not match user selector)
				podC := makeModulePod("csi-nodeC", testNamespace, "nodeC", map[string]string{"app": "csi-nfs-node"})
				Expect(cl.Create(ctx, podC)).To(Succeed())

				// 1) Run ReconcileNodeSelector
				Expect(controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, testNamespace, configSecretName)).To(Succeed())

				// nodeA / nodeB should get the label
				checkA := &corev1.Node{}
				Expect(cl.Get(ctx, client.ObjectKey{Name: "nodeA"}, checkA)).To(Succeed())
				Expect(checkA.Labels).To(HaveKey(nfsNodeSelectorKey))

				checkB := &corev1.Node{}
				Expect(cl.Get(ctx, client.ObjectKey{Name: "nodeB"}, checkB)).To(Succeed())
				Expect(checkB.Labels).To(HaveKey(nfsNodeSelectorKey))

				checkC := &corev1.Node{}
				Expect(cl.Get(ctx, client.ObjectKey{Name: "nodeC"}, checkC)).To(Succeed())
				Expect(checkC.Labels).NotTo(HaveKey(nfsNodeSelectorKey))

				// 2) Run ReconcileModulePods
				Expect(controller.ReconcileModulePods(
					ctx, cl, clusterWideCl, log, testNamespace,
					map[string]string{nfsNodeSelectorKey: ""},
					[]map[string]string{
						{"app": "csi-nfs-controller"},
						{"app": "csi-nfs-node"},
					},
				)).To(Succeed())

				// Pod on nodeC should be deleted
				errGetPodC := cl.Get(ctx, client.ObjectKey{Name: podC.Name, Namespace: testNamespace}, &corev1.Pod{})
				Expect(k8serrors.IsNotFound(errGetPodC)).To(BeTrue(), "Pod on nodeC must be removed")
			})

			It("Expanded integration with 4 groups of nodes (1) keep label, (2) add label, (3) remove label + remove csi-nfs-node Pod, (4) cannot remove label due to block", func() {
				cfgYAML := `
nodeSelector:
  myrole: "nfs"
`
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      configSecretName,
						Namespace: testNamespace,
					},
					Data: map[string][]byte{"config": []byte(cfgYAML)},
				}
				Expect(cl.Create(ctx, secret)).To(Succeed())

				// Group 1: matching user selector, already have nfs label
				node1a := makeNode("node1a", map[string]string{"myrole": "nfs", nfsNodeSelectorKey: ""})
				node1b := makeNode("node1b", map[string]string{"myrole": "nfs", nfsNodeSelectorKey: ""})
				Expect(cl.Create(ctx, node1a)).To(Succeed())
				Expect(cl.Create(ctx, node1b)).To(Succeed())

				// Group 2: matching user selector, do not have nfs label
				node2a := makeNode("node2a", map[string]string{"myrole": "nfs"})
				node2b := makeNode("node2b", map[string]string{"myrole": "nfs"})
				Expect(cl.Create(ctx, node2a)).To(Succeed())
				Expect(cl.Create(ctx, node2b)).To(Succeed())

				// Group 3: not matching selector, have label, no blocking -> label will be removed
				node3a := makeNode("node3a", map[string]string{nfsNodeSelectorKey: ""})
				node3b := makeNode("node3b", map[string]string{nfsNodeSelectorKey: ""})
				Expect(cl.Create(ctx, node3a)).To(Succeed())
				Expect(cl.Create(ctx, node3b)).To(Succeed())

				pod3aNFS := makeModulePod("csi-nfs-node3a", testNamespace, "node3a", map[string]string{"app": "csi-nfs-node"})
				Expect(cl.Create(ctx, pod3aNFS)).To(Succeed())

				// a pod with a different provisioner
				otherPod3a := makePodWithPVC("pod-other3a", testNamespace, "node3a", "pvc-other3a", "other.csi.k8s.io")
				Expect(cl.Create(ctx, otherPod3a)).To(Succeed())
				otherPVC3a := makePVC("pvc-other3a", testNamespace, "other.csi.k8s.io", v1.ClaimBound)
				Expect(cl.Create(ctx, otherPVC3a)).To(Succeed())

				// Group 4: not matching, have label, have blocking factors -> label not removed
				node4a := makeNode("node4a", map[string]string{nfsNodeSelectorKey: ""})
				Expect(cl.Create(ctx, node4a)).To(Succeed())

				lease := &coordinationv1.Lease{
					ObjectMeta: metav1.ObjectMeta{Name: "external-snapshotter-leader-nfs-csi-k8s-io", Namespace: testNamespace},
					Spec: coordinationv1.LeaseSpec{
						HolderIdentity: ptr.To("node4a"),
					},
				}
				Expect(cl.Create(ctx, lease)).To(Succeed())

				pvcBlock4a := makePVC("pvc-block4a", testNamespace, provisionerNFS, v1.ClaimPending)
				Expect(cl.Create(ctx, pvcBlock4a)).To(Succeed())

				// 1) ReconcileNodeSelector
				Expect(controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, testNamespace, configSecretName)).To(Succeed())

				// Verify results
				// Group 1: label remains
				recheck1a := &corev1.Node{}
				Expect(cl.Get(ctx, client.ObjectKey{Name: "node1a"}, recheck1a)).To(Succeed())
				Expect(recheck1a.Labels).To(HaveKey(nfsNodeSelectorKey))

				// Group 2: label is added
				recheck2a := &corev1.Node{}
				Expect(cl.Get(ctx, client.ObjectKey{Name: "node2a"}, recheck2a)).To(Succeed())
				Expect(recheck2a.Labels).To(HaveKey(nfsNodeSelectorKey))

				// Group 3: label is removed
				recheck3a := &corev1.Node{}
				Expect(cl.Get(ctx, client.ObjectKey{Name: "node3a"}, recheck3a)).To(Succeed())
				Expect(recheck3a.Labels).NotTo(HaveKey(nfsNodeSelectorKey))

				// Group 4: label stays
				recheck4a := &corev1.Node{}
				Expect(cl.Get(ctx, client.ObjectKey{Name: "node4a"}, recheck4a)).To(Succeed())
				Expect(recheck4a.Labels).To(HaveKey(nfsNodeSelectorKey))

				// 2) ReconcileModulePods
				Expect(controller.ReconcileModulePods(
					ctx, cl, clusterWideCl, log, testNamespace,
					map[string]string{nfsNodeSelectorKey: ""},
					[]map[string]string{
						{"app": "csi-nfs-controller"},
						{"app": "csi-nfs-node"},
					},
				)).To(Succeed())

				// Group 3 node's csi-nfs-node Pod must be deleted
				errPod3a := cl.Get(ctx, client.ObjectKey{Name: "csi-nfs-node3a", Namespace: testNamespace}, &corev1.Pod{})
				Expect(k8serrors.IsNotFound(errPod3a)).To(BeTrue(), "csi-nfs-node Pod on group 3 node must be deleted")

				// The other pod with different CSI provisioner remains
				errOtherPod3a := cl.Get(ctx, client.ObjectKey{Name: "pod-other3a", Namespace: testNamespace}, &corev1.Pod{})
				Expect(errOtherPod3a).NotTo(HaveOccurred(), "Non-nfs Pod should not be deleted")

				// Group 4 node's label remains and pods (if any) are not deleted due to block
				recheck4a = &corev1.Node{}
				Expect(cl.Get(ctx, client.ObjectKey{Name: "node4a"}, recheck4a)).To(Succeed())
				Expect(recheck4a.Labels).To(HaveKey(nfsNodeSelectorKey))

			})
		})
	})
})

//-------------------------------------------------------------------------------
// Helper functions
//-------------------------------------------------------------------------------

func makeNode(name string, labels map[string]string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: labels,
		},
	}
}

func makeModulePod(name, namespace, nodeName string, lbls map[string]string) *corev1.Pod {
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

func makePodWithPVC(name, namespace, nodeName, pvcName, _ string) *corev1.Pod {
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

func makePVC(name, namespace, provisioner string, phase v1.PersistentVolumeClaimPhase) *corev1.PersistentVolumeClaim {
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

// prepareControllerNodeWithSnapshot creates a node with the label,
// sets it as a controller node via Lease,
// and creates a pending VolumeSnapshot that references an NFS PVC.
func prepareControllerNodeWithSnapshot(
	ctx context.Context,
	cl client.Client,
	ns, nodeName, snapshotName, nfsProvisioner string,
) {
	node := makeNode(nodeName, map[string]string{
		"fake-role":                         "fake",
		"storage.deckhouse.io/csi-nfs-node": "",
	})
	Expect(cl.Create(ctx, node)).To(Succeed())

	node = &corev1.Node{}
	Expect(cl.Get(ctx, client.ObjectKey{Name: nodeName}, node)).To(Succeed())
	Expect(node.Labels).To(HaveKey("storage.deckhouse.io/csi-nfs-node"))

	lease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: "external-snapshotter-leader-nfs-csi-k8s-io", Namespace: ns},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity: ptr.To(nodeName),
		},
	}
	Expect(cl.Create(ctx, lease)).To(Succeed())

	vs := &snapshotv1.VolumeSnapshot{
		ObjectMeta: metav1.ObjectMeta{
			Name:      snapshotName,
			Namespace: ns,
		},
		Spec: snapshotv1.VolumeSnapshotSpec{
			Source: snapshotv1.VolumeSnapshotSource{
				PersistentVolumeClaimName: ptr.To("some-pvc"),
			},
		},
		Status: &snapshotv1.VolumeSnapshotStatus{
			ReadyToUse: ptr.To(bool(false)),
		},
	}
	Expect(cl.Create(ctx, vs)).To(Succeed())

	pvc := makePVC("some-pvc", ns, nfsProvisioner, corev1.ClaimBound)
	Expect(cl.Create(ctx, pvc)).To(Succeed())

	pvc = &corev1.PersistentVolumeClaim{}
	Expect(cl.Get(ctx, client.ObjectKey{Name: "some-pvc", Namespace: ns}, pvc)).To(Succeed())
	Expect(pvc.Annotations).To(HaveKey("volume.kubernetes.io/storage-provisioner"))
	Expect(pvc.Annotations["volume.kubernetes.io/storage-provisioner"]).To(Equal(nfsProvisioner))

	vs = &snapshotv1.VolumeSnapshot{}
	Expect(cl.Get(ctx, client.ObjectKey{Name: snapshotName, Namespace: ns}, vs)).To(Succeed())
	Expect(vs.Spec.Source.PersistentVolumeClaimName).To(Equal(ptr.To("some-pvc")))
	Expect(*vs.Status.ReadyToUse).To(BeFalse())
}

// prepareControllerNodeWithPendingPVC creates a node with the label,
// sets it as a controller node, and creates a pending PVC for NFS.
func prepareControllerNodeWithPendingPVC(
	ctx context.Context,
	cl client.Client,
	ns, nodeName, pvcName, nfsProvisioner string,
) {
	node := makeNode(nodeName, map[string]string{
		"storage.deckhouse.io/csi-nfs-node": "",
	})
	Expect(cl.Create(ctx, node)).To(Succeed())

	lease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{Name: "external-snapshotter-leader-nfs-csi-k8s-io", Namespace: ns},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity: ptr.To(nodeName),
		},
	}
	Expect(cl.Create(ctx, lease)).To(Succeed())

	pvcPending := makePVC(pvcName, ns, nfsProvisioner, v1.ClaimPending)
	Expect(cl.Create(ctx, pvcPending)).To(Succeed())
}

// preparePendingPVC is a helper if we need to create a pending PVC that references NFS provisioner.
func preparePendingPVC(
	ctx context.Context,
	cl client.Client,
	ns, pvcName, _, nfsProvisioner string,
) {
	pvc := makePVC(pvcName, ns, nfsProvisioner, corev1.ClaimPending)
	Expect(cl.Create(ctx, pvc)).To(Succeed())
}
