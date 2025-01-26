package controller_test

import (
	"context"

	v1alpha1 "github.com/deckhouse/csi-nfs/api/v1alpha1"
	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"d8-controller/pkg/controller"
	"d8-controller/pkg/logger"
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
		// configSecretName string

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

	Context("ReconcileNodeSelector()", func() {

		It("Scenario 1: NFSStorageClass is missing, some nodes have the csi-nfs label, some do not -> csi-nfs label should be removed from all nodes", func() {
			// create some nodes with and without the label
			prepareNode(ctx, cl, "node-with-label", map[string]string{nfsNodeSelectorKey: "", "test-label": "value"})
			prepareNode(ctx, cl, "node-without-label", nil)

			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
			Expect(err).NotTo(HaveOccurred())

			checkNodeLabels(ctx, cl, "node-with-label", map[string]string{"test-label": "value"})
			checkNodeLabels(ctx, cl, "node-without-label", nil)
		})

		It("Scenario 2: NFSStorageClass exists without nodeSelector; all nodes does not have the csi-nfs label -> csi-nfs label should be added to linux nodes", func() {
			// create NFSStorageClass
			nsc := generateNFSStorageClass(nfsSCConfig)
			Expect(cl.Create(ctx, nsc)).To(Succeed())
			recheckNSC := &v1alpha1.NFSStorageClass{}
			Expect(cl.Get(ctx, client.ObjectKey{Name: nfsSCConfig.Name}, recheckNSC)).To(Succeed())
			Expect(recheckNSC.Spec.WorkloadNodes).To(BeNil())

			// create some nodes without the label
			prepareNode(ctx, cl, "node-without-label-1", map[string]string{"kubernetes.io/os": "linux", "test-label": "value"})
			prepareNode(ctx, cl, "node-without-label-2", nil)
			prepareNode(ctx, cl, "node-without-label-3", map[string]string{"kubernetes.io/os": "linux", "test-label": "value"})

			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
			Expect(err).NotTo(HaveOccurred())

			checkNodeLabels(ctx, cl, "node-without-label-1", map[string]string{"kubernetes.io/os": "linux", "test-label": "value", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "node-without-label-2", nil)
			checkNodeLabels(ctx, cl, "node-without-label-3", map[string]string{"kubernetes.io/os": "linux", "test-label": "value", nfsNodeSelectorKey: ""})
		})

		It("Scenario 3: NFSStorageClass exists with MatchLabels nodeSelector; all nodes does not have the csi-nfs label -> csi-nfs label should be added to nodes matching the selector", func() {
			// create NFSStorageClass
			nfsSCConfig.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"project": "test-1"},
			}
			nsc := generateNFSStorageClass(nfsSCConfig)
			Expect(cl.Create(ctx, nsc)).To(Succeed())
			recheckNSC := &v1alpha1.NFSStorageClass{}
			Expect(cl.Get(ctx, client.ObjectKey{Name: nfsSCConfig.Name}, recheckNSC)).To(Succeed())
			Expect(recheckNSC.Spec.WorkloadNodes.NodeSelector).To(Equal(&nfsSCConfig.nodeSelector))

			// create some nodes without the csi-nfs label that match the selector
			prepareNode(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "test-label": "value"})
			prepareNode(ctx, cl, "matching-node-without-label-2", map[string]string{"project": "test-1"})

			// create some nodes without the csi-nfs label that do not match the selector
			prepareNode(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-2", "test-label": "value"})
			prepareNode(ctx, cl, "non-matching-node-without-label-2", map[string]string{"project": "test-2"})

			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
			Expect(err).NotTo(HaveOccurred())

			checkNodeLabels(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "matching-node-without-label-2", map[string]string{"project": "test-1", nfsNodeSelectorKey: ""})

			checkNodeLabels(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-2", "test-label": "value"})
			checkNodeLabels(ctx, cl, "non-matching-node-without-label-2", map[string]string{"project": "test-2"})
		})

		It("Scenario 4: NFSStorageClass exists with MatchExpressions nodeSelector; all nodes does not have the csi-nfs label -> csi-nfs label should be added to nodes matching the selector", func() {
			// create NFSStorageClass
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
			recheckNSC := &v1alpha1.NFSStorageClass{}
			Expect(cl.Get(ctx, client.ObjectKey{Name: nfsSCConfig.Name}, recheckNSC)).To(Succeed())
			Expect(recheckNSC.Spec.WorkloadNodes.NodeSelector).To(Equal(&nfsSCConfig.nodeSelector))

			// create some nodes without the csi-nfs label that match the selector
			prepareNode(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "test-label": "value"})
			prepareNode(ctx, cl, "matching-node-without-label-2", map[string]string{"project": "test-2"})
			prepareNode(ctx, cl, "matching-node-without-label-3", map[string]string{"project": "test-1"})
			prepareNode(ctx, cl, "matching-node-without-label-4", map[string]string{"project": "test-2", "test-label": "value"})

			// create some nodes without the csi-nfs label that do not match the selector
			prepareNode(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-3", "test-label": "value"})
			prepareNode(ctx, cl, "non-matching-node-without-label-2", map[string]string{"project": "test-3"})
			prepareNode(ctx, cl, "non-matching-node-without-label-3", map[string]string{"project": "test-4", "test-label": "value"})

			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
			Expect(err).NotTo(HaveOccurred())

			checkNodeLabels(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "matching-node-without-label-2", map[string]string{"project": "test-2", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "matching-node-without-label-3", map[string]string{"project": "test-1", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "matching-node-without-label-4", map[string]string{"project": "test-2", "test-label": "value", nfsNodeSelectorKey: ""})

			checkNodeLabels(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-3", "test-label": "value"})
			checkNodeLabels(ctx, cl, "non-matching-node-without-label-2", map[string]string{"project": "test-3"})
			checkNodeLabels(ctx, cl, "non-matching-node-without-label-3", map[string]string{"project": "test-4", "test-label": "value"})
		})

		It("Scenario 5: NFSStorageClass exists with MatchExpressions and MatchLabels nodeSelector; all nodes does not have the csi-nfs label -> csi-nfs label should be added to nodes matching the selector", func() {
			// create NFSStorageClass
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
			recheckNSC := &v1alpha1.NFSStorageClass{}
			Expect(cl.Get(ctx, client.ObjectKey{Name: nfsSCConfig.Name}, recheckNSC)).To(Succeed())
			Expect(recheckNSC.Spec.WorkloadNodes.NodeSelector).To(Equal(&nfsSCConfig.nodeSelector))

			// create some nodes without the csi-nfs label that match the selector
			prepareNode(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "role": "nfs", "test-label": "value"})
			prepareNode(ctx, cl, "matching-node-without-label-2", map[string]string{"project": "test-1", "role": "storage"})
			prepareNode(ctx, cl, "matching-node-without-label-3", map[string]string{"project": "test-1", "role": "nfs"})

			// create some nodes without the csi-nfs label that do not match the selector
			prepareNode(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-2", "role": "nfs", "test-label": "value"})
			prepareNode(ctx, cl, "non-matching-node-without-label-2", map[string]string{"project": "test-2", "role": "storage"})
			prepareNode(ctx, cl, "non-matching-node-without-label-3", map[string]string{"project": "test-2", "role": "nfs"})
			prepareNode(ctx, cl, "non-matching-node-without-label-4", map[string]string{"project": "test-1", "role": "worker"})
			prepareNode(ctx, cl, "non-matching-node-without-label-5", map[string]string{"project": "test-1"})

			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
			Expect(err).NotTo(HaveOccurred())

			checkNodeLabels(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "role": "nfs", "test-label": "value", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "matching-node-without-label-2", map[string]string{"project": "test-1", "role": "storage", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "matching-node-without-label-3", map[string]string{"project": "test-1", "role": "nfs", nfsNodeSelectorKey: ""})

			checkNodeLabels(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-2", "role": "nfs", "test-label": "value"})
			checkNodeLabels(ctx, cl, "non-matching-node-without-label-2", map[string]string{"project": "test-2", "role": "storage"})
			checkNodeLabels(ctx, cl, "non-matching-node-without-label-3", map[string]string{"project": "test-2", "role": "nfs"})
			checkNodeLabels(ctx, cl, "non-matching-node-without-label-4", map[string]string{"project": "test-1", "role": "worker"})
			checkNodeLabels(ctx, cl, "non-matching-node-without-label-5", map[string]string{"project": "test-1"})
		})

		It("Scenario 6: Several NFSStorageClasses exist with different nodeSelectors and one without; all nodes does not have the csi-nfs label -> csi-nfs label should be added to nodes matching kubernetes.io/os=linux label", func() {
			// create NFSStorageClasses
			nfsSCConfig1 := nfsSCConfig
			nfsSCConfig1.Name = "test-nfs-sc-1"
			nfsSCConfig1.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"project": "test-1"},
			}
			nsc1 := generateNFSStorageClass(nfsSCConfig1)
			Expect(cl.Create(ctx, nsc1)).To(Succeed())
			recheckNSC1 := &v1alpha1.NFSStorageClass{}
			Expect(cl.Get(ctx, client.ObjectKey{Name: nfsSCConfig1.Name}, recheckNSC1)).To(Succeed())
			Expect(recheckNSC1.Spec.WorkloadNodes.NodeSelector).To(Equal(&nfsSCConfig1.nodeSelector))

			nfsSCConfig2 := nfsSCConfig
			nfsSCConfig2.Name = "test-nfs-sc-2"
			nfsSCConfig2.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"project": "test-1"},
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "role",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"nfs", "storage"},
					},
				},
			}
			nsc2 := generateNFSStorageClass(nfsSCConfig2)
			Expect(cl.Create(ctx, nsc2)).To(Succeed())
			recheckNSC2 := &v1alpha1.NFSStorageClass{}
			Expect(cl.Get(ctx, client.ObjectKey{Name: nfsSCConfig2.Name}, recheckNSC2)).To(Succeed())
			Expect(recheckNSC2.Spec.WorkloadNodes.NodeSelector).To(Equal(&nfsSCConfig2.nodeSelector))

			nfsSCConfig3 := nfsSCConfig
			nfsSCConfig3.Name = "test-nfs-sc-3"
			nsc3 := generateNFSStorageClass(nfsSCConfig3)
			Expect(cl.Create(ctx, nsc3)).To(Succeed())
			recheckNSC3 := &v1alpha1.NFSStorageClass{}
			Expect(cl.Get(ctx, client.ObjectKey{Name: nfsSCConfig3.Name}, recheckNSC3)).To(Succeed())
			Expect(recheckNSC3.Spec.WorkloadNodes).To(BeNil())

			// create some nodes without the csi-nfs label that match at least one selector
			prepareNode(ctx, cl, "matching-node-without-label-1", map[string]string{"kubernetes.io/os": "linux", "project": "test-1", "role": "nfs", "test-label": "value"})
			prepareNode(ctx, cl, "matching-node-without-label-2", map[string]string{"kubernetes.io/os": "linux", "project": "test-1", "role": "storage"})
			prepareNode(ctx, cl, "matching-node-without-label-3", map[string]string{"kubernetes.io/os": "linux", "project": "test-1", "role": "nfs"})
			prepareNode(ctx, cl, "matching-node-without-label-4", map[string]string{"kubernetes.io/os": "linux", "project": "test-1", "role": "worker"})
			prepareNode(ctx, cl, "matching-node-without-label-5", map[string]string{"kubernetes.io/os": "linux", "project": "test-1"})
			prepareNode(ctx, cl, "matching-node-without-label-6", map[string]string{"kubernetes.io/os": "linux", "project": "test-1", "role": "nfs", "test-label": "value"})

			// create some nodes without the csi-nfs label that do not match any selector
			prepareNode(ctx, cl, "non-matching-node-without-label-1", map[string]string{"kubernetes.io/os": "linux", "project": "test-3", "role": "worker", "test-label": "value"})
			prepareNode(ctx, cl, "non-matching-node-without-label-2", map[string]string{"kubernetes.io/os": "linux", "project": "test-3"})
			prepareNode(ctx, cl, "non-matching-node-without-label-3", map[string]string{"kubernetes.io/os": "linux", "test-label": "value"})
			prepareNode(ctx, cl, "non-matching-node-without-label-4", map[string]string{"kubernetes.io/os": "linux"})

			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
			Expect(err).NotTo(HaveOccurred())

			checkNodeLabels(ctx, cl, "matching-node-without-label-1", map[string]string{"kubernetes.io/os": "linux", "project": "test-1", "role": "nfs", "test-label": "value", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "matching-node-without-label-2", map[string]string{"kubernetes.io/os": "linux", "project": "test-1", "role": "storage", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "matching-node-without-label-3", map[string]string{"kubernetes.io/os": "linux", "project": "test-1", "role": "nfs", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "matching-node-without-label-4", map[string]string{"kubernetes.io/os": "linux", "project": "test-1", "role": "worker", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "matching-node-without-label-5", map[string]string{"kubernetes.io/os": "linux", "project": "test-1", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "matching-node-without-label-6", map[string]string{"kubernetes.io/os": "linux", "project": "test-1", "role": "nfs", "test-label": "value", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "non-matching-node-without-label-1", map[string]string{"kubernetes.io/os": "linux", "project": "test-3", "role": "worker", "test-label": "value", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "non-matching-node-without-label-2", map[string]string{"kubernetes.io/os": "linux", "project": "test-3", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "non-matching-node-without-label-3", map[string]string{"kubernetes.io/os": "linux", "test-label": "value", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "non-matching-node-without-label-4", map[string]string{"kubernetes.io/os": "linux", nfsNodeSelectorKey: ""})
		})

		It("Scenario 7: Several NFSStorageClasses exist with different nodeSelectors; some nodes have the csi-nfs label, some do not -> csi-nfs label should be removed from nodes that do not match any selector and added to nodes that match at least one selector", func() {
			// create NFSStorageClasses
			nfsSCConfig1 := nfsSCConfig
			nfsSCConfig1.Name = "test-nfs-sc-1"
			nfsSCConfig1.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"project": "test-1"},
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "role",
						Operator: metav1.LabelSelectorOpIn,
						Values:   []string{"nfs", "storage"},
					},
				},
			}
			nsc1 := generateNFSStorageClass(nfsSCConfig1)
			Expect(cl.Create(ctx, nsc1)).To(Succeed())
			recheckNSC1 := &v1alpha1.NFSStorageClass{}
			Expect(cl.Get(ctx, client.ObjectKey{Name: nfsSCConfig1.Name}, recheckNSC1)).To(Succeed())
			Expect(recheckNSC1.Spec.WorkloadNodes.NodeSelector).To(Equal(&nfsSCConfig1.nodeSelector))

			nfsSCConfig2 := nfsSCConfig
			nfsSCConfig2.Name = "test-nfs-sc-2"
			nfsSCConfig2.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"project": "test-2"},
			}
			nsc2 := generateNFSStorageClass(nfsSCConfig2)
			Expect(cl.Create(ctx, nsc2)).To(Succeed())
			recheckNSC2 := &v1alpha1.NFSStorageClass{}
			Expect(cl.Get(ctx, client.ObjectKey{Name: nfsSCConfig2.Name}, recheckNSC2)).To(Succeed())
			Expect(recheckNSC2.Spec.WorkloadNodes.NodeSelector).To(Equal(&nfsSCConfig2.nodeSelector))

			// create some nodes with the csi-nfs label that match at least one selector
			prepareNode(ctx, cl, "matching-node-with-label-1", map[string]string{"project": "test-1", "role": "nfs", "test-label": "value", nfsNodeSelectorKey: ""})
			prepareNode(ctx, cl, "matching-node-with-label-2", map[string]string{"project": "test-2", "test-label": "value", nfsNodeSelectorKey: ""})

			// create some nodes with the csi-nfs label that do not match any selector
			prepareNode(ctx, cl, "non-matching-node-with-label-1", map[string]string{"project": "test-3", "role": "worker", "test-label": "value", nfsNodeSelectorKey: ""})
			prepareNode(ctx, cl, "non-matching-node-with-label-2", map[string]string{"project": "test-1", "role": "dev", "test-label": "value", nfsNodeSelectorKey: ""})

			// create some nodes without the csi-nfs label that match at least one selector
			prepareNode(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "role": "nfs", "test-label": "value"})
			prepareNode(ctx, cl, "matching-node-without-label-2", map[string]string{"project": "test-2", "test-label": "value"})

			// create some nodes without the csi-nfs label that do not match any selector
			prepareNode(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-3", "role": "worker", "test-label": "value"})
			prepareNode(ctx, cl, "non-matching-node-without-label-2", map[string]string{"project": "test-1", "role": "dev", "test-label": "value"})

			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
			Expect(err).NotTo(HaveOccurred())

			checkNodeLabels(ctx, cl, "matching-node-with-label-1", map[string]string{"project": "test-1", "role": "nfs", "test-label": "value", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "matching-node-with-label-2", map[string]string{"project": "test-2", "test-label": "value", nfsNodeSelectorKey: ""})

			checkNodeLabels(ctx, cl, "non-matching-node-with-label-1", map[string]string{"project": "test-3", "role": "worker", "test-label": "value"})
			checkNodeLabels(ctx, cl, "non-matching-node-with-label-2", map[string]string{"project": "test-1", "role": "dev", "test-label": "value"})

			checkNodeLabels(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "role": "nfs", "test-label": "value", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "matching-node-without-label-2", map[string]string{"project": "test-2", "test-label": "value", nfsNodeSelectorKey: ""})

			checkNodeLabels(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-3", "role": "worker", "test-label": "value"})
			checkNodeLabels(ctx, cl, "non-matching-node-without-label-2", map[string]string{"project": "test-1", "role": "dev", "test-label": "value"})
		})

		Context("Scenario 9: Removing the csi-nfs label from controller node with various conditions", func() {
			It("9.1: Has pending VolumeSnapshot and csi-controller pod exists -> csi-nfs label should not be removed from controller node", func() {
				// create NFSStorageClass
				nfsSCConfig.nodeSelector = metav1.LabelSelector{
					MatchLabels: map[string]string{"project": "test-1"},
				}
				nsc := generateNFSStorageClass(nfsSCConfig)
				Expect(cl.Create(ctx, nsc)).To(Succeed())
				recheckNSC := &v1alpha1.NFSStorageClass{}
				Expect(cl.Get(ctx, client.ObjectKey{Name: nfsSCConfig.Name}, recheckNSC)).To(Succeed())
				Expect(recheckNSC.Spec.WorkloadNodes.NodeSelector).To(Equal(&nfsSCConfig.nodeSelector))

				// create controller node with the csi-nfs label, pending VolumeSnapshot and csi-controller pod
				prepareNode(ctx, cl, "controller-node", map[string]string{"kubernetes.io/os": "linux", "fake-role": "fake", nfsNodeSelectorKey: ""})
				makeNodeAsController(ctx, cl, "controller-node", controllerNamespace)
				prepareVolumeSnapshot(ctx, cl, testNamespace, "vs-1", provisionerNFS, ptr.To(bool(false)))
				prepareModulePod(ctx, cl, "csi-controller", controllerNamespace, "controller-node", controller.CSIControllerLabel)

				// create some matching nodes without the csi-nfs label
				prepareNode(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "test-label": "value"})
				prepareNode(ctx, cl, "matching-node-without-label-2", map[string]string{"project": "test-1"})

				// create some matching nodes with the csi-nfs label
				prepareNode(ctx, cl, "matching-node-with-label-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})
				prepareNode(ctx, cl, "matching-node-with-label-2", map[string]string{"project": "test-1", nfsNodeSelectorKey: ""})

				// create some non-matching nodes without the csi-nfs label
				prepareNode(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-2", "test-label": "value"})
				prepareNode(ctx, cl, "non-matching-node-without-label-2", map[string]string{"project": "test-2"})

				// create some non-matching nodes with the csi-nfs label
				prepareNode(ctx, cl, "non-matching-node-with-label-1", map[string]string{"project": "test-3", "test-label": "value", nfsNodeSelectorKey: ""})
				prepareNode(ctx, cl, "non-matching-node-with-label-2", map[string]string{"test-label": "value", nfsNodeSelectorKey: ""})

				err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
				Expect(err).NotTo(HaveOccurred())

				checkNodeLabels(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})
				checkNodeLabels(ctx, cl, "matching-node-without-label-2", map[string]string{"project": "test-1", nfsNodeSelectorKey: ""})

				checkNodeLabels(ctx, cl, "matching-node-with-label-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})
				checkNodeLabels(ctx, cl, "matching-node-with-label-2", map[string]string{"project": "test-1", nfsNodeSelectorKey: ""})

				checkNodeLabels(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-2", "test-label": "value"})
				checkNodeLabels(ctx, cl, "non-matching-node-without-label-2", map[string]string{"project": "test-2"})

				checkNodeLabels(ctx, cl, "non-matching-node-with-label-1", map[string]string{"project": "test-3", "test-label": "value"})
				checkNodeLabels(ctx, cl, "non-matching-node-with-label-2", map[string]string{"test-label": "value"})

				checkNodeLabels(ctx, cl, "controller-node", map[string]string{"kubernetes.io/os": "linux", "fake-role": "fake", nfsNodeSelectorKey: ""})
			})

			It("9.2: Has pending VolumeSnapshot and csi-controller pod does not exist -> csi-nfs label should be removed from controller node", func() {
				// create NFSStorageClass
				nfsSCConfig.nodeSelector = metav1.LabelSelector{
					MatchLabels: map[string]string{"project": "test-1"},
				}
				nsc := generateNFSStorageClass(nfsSCConfig)
				Expect(cl.Create(ctx, nsc)).To(Succeed())
				recheckNSC := &v1alpha1.NFSStorageClass{}
				Expect(cl.Get(ctx, client.ObjectKey{Name: nfsSCConfig.Name}, recheckNSC)).To(Succeed())
				Expect(recheckNSC.Spec.WorkloadNodes.NodeSelector).To(Equal(&nfsSCConfig.nodeSelector))

				// create controller node with the csi-nfs label and pending VolumeSnapshot
				prepareNode(ctx, cl, "controller-node", map[string]string{"kubernetes.io/os": "linux", "fake-role": "fake", nfsNodeSelectorKey: ""})
				makeNodeAsController(ctx, cl, "controller-node", controllerNamespace)
				prepareVolumeSnapshot(ctx, cl, testNamespace, "vs-1", provisionerNFS, ptr.To(bool(false)))
				// NOTE: csi-controller pod does not exist

				// create some matching nodes without the csi-nfs label
				prepareNode(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "test-label": "value"})
				prepareNode(ctx, cl, "matching-node-without-label-2", map[string]string{"project": "test-1"})

				// create some matching nodes with the csi-nfs label
				prepareNode(ctx, cl, "matching-node-with-label-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})
				prepareNode(ctx, cl, "matching-node-with-label-2", map[string]string{"project": "test-1", nfsNodeSelectorKey: ""})

				// create some non-matching nodes without the csi-nfs label
				prepareNode(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-2", "test-label": "value"})
				prepareNode(ctx, cl, "non-matching-node-without-label-2", map[string]string{"project": "test-2"})

				// create non-matching node with the csi-nfs label
				prepareNode(ctx, cl, "non-matching-node-with-label-1", map[string]string{"project": "test-3", "test-label": "value", nfsNodeSelectorKey: ""})

				err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
				Expect(err).NotTo(HaveOccurred())

				checkNodeLabels(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})
				checkNodeLabels(ctx, cl, "matching-node-without-label-2", map[string]string{"project": "test-1", nfsNodeSelectorKey: ""})

				checkNodeLabels(ctx, cl, "matching-node-with-label-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})
				checkNodeLabels(ctx, cl, "matching-node-with-label-2", map[string]string{"project": "test-1", nfsNodeSelectorKey: ""})

				checkNodeLabels(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-2", "test-label": "value"})
				checkNodeLabels(ctx, cl, "non-matching-node-without-label-2", map[string]string{"project": "test-2"})

				checkNodeLabels(ctx, cl, "non-matching-node-with-label-1", map[string]string{"project": "test-3", "test-label": "value"})

				checkNodeLabels(ctx, cl, "controller-node", map[string]string{"kubernetes.io/os": "linux", "fake-role": "fake"})
			})

			It("9.3: Has no pending VolumeSnapshot and csi-controller pod exists -> csi-nfs label should be removed from controller node", func() {
				// create NFSStorageClass
				nfsSCConfig.nodeSelector = metav1.LabelSelector{
					MatchLabels: map[string]string{"project": "test-1"},
				}
				nsc := generateNFSStorageClass(nfsSCConfig)
				Expect(cl.Create(ctx, nsc)).To(Succeed())
				recheckNSC := &v1alpha1.NFSStorageClass{}
				Expect(cl.Get(ctx, client.ObjectKey{Name: nfsSCConfig.Name}, recheckNSC)).To(Succeed())
				Expect(recheckNSC.Spec.WorkloadNodes.NodeSelector).To(Equal(&nfsSCConfig.nodeSelector))

				// create controller node with the csi-nfs label and no pending VolumeSnapshot
				prepareNode(ctx, cl, "controller-node", map[string]string{"kubernetes.io/os": "linux", "fake-role": "fake", nfsNodeSelectorKey: ""})
				makeNodeAsController(ctx, cl, "controller-node", controllerNamespace)
				prepareVolumeSnapshot(ctx, cl, testNamespace, "vs-1", provisionerNFS, ptr.To(bool(true)))
				prepareModulePod(ctx, cl, "csi-controller", controllerNamespace, "controller-node", controller.CSIControllerLabel)

				// create some matching nodes without the csi-nfs label
				prepareNode(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "test-label": "value"})
				prepareNode(ctx, cl, "matching-node-without-label-2", map[string]string{"project": "test-1"})

				// create some matching nodes with the csi-nfs label
				prepareNode(ctx, cl, "matching-node-with-label-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})
				prepareNode(ctx, cl, "matching-node-with-label-2", map[string]string{"project": "test-1", nfsNodeSelectorKey: ""})

				// create some non-matching nodes without the csi-nfs label
				prepareNode(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-2", "test-label": "value"})
				prepareNode(ctx, cl, "non-matching-node-without-label-2", map[string]string{"project": "test-2"})

				// create some non-matching nodes with the csi-nfs label
				prepareNode(ctx, cl, "non-matching-node-with-label-1", map[string]string{"project": "test-3", "test-label": "value", nfsNodeSelectorKey: ""})
				prepareNode(ctx, cl, "non-matching-node-with-label-2", map[string]string{"test-label": "value", nfsNodeSelectorKey: ""})

				err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
				Expect(err).NotTo(HaveOccurred())

				checkNodeLabels(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})
				checkNodeLabels(ctx, cl, "matching-node-without-label-2", map[string]string{"project": "test-1", nfsNodeSelectorKey: ""})

				checkNodeLabels(ctx, cl, "matching-node-with-label-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})
				checkNodeLabels(ctx, cl, "matching-node-with-label-2", map[string]string{"project": "test-1", nfsNodeSelectorKey: ""})

				checkNodeLabels(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-2", "test-label": "value"})
				checkNodeLabels(ctx, cl, "non-matching-node-without-label-2", map[string]string{"project": "test-2"})

				checkNodeLabels(ctx, cl, "non-matching-node-with-label-1", map[string]string{"project": "test-3", "test-label": "value"})
				checkNodeLabels(ctx, cl, "non-matching-node-with-label-2", map[string]string{"test-label": "value"})

				checkNodeLabels(ctx, cl, "controller-node", map[string]string{"kubernetes.io/os": "linux", "fake-role": "fake"})
			})

			It("9.4: Has pending PVC and csi-controller pod exists -> csi-nfs label should not be removed from controller node", func() {
				// create NFSStorageClass
				nfsSCConfig.nodeSelector = metav1.LabelSelector{
					MatchLabels: map[string]string{"project": "test-1"},
				}
				nsc := generateNFSStorageClass(nfsSCConfig)
				Expect(cl.Create(ctx, nsc)).To(Succeed())
				recheckNSC := &v1alpha1.NFSStorageClass{}
				Expect(cl.Get(ctx, client.ObjectKey{Name: nfsSCConfig.Name}, recheckNSC)).To(Succeed())
				Expect(recheckNSC.Spec.WorkloadNodes.NodeSelector).To(Equal(&nfsSCConfig.nodeSelector))

				// create controller node with the csi-nfs label and csi-controller pod
				prepareNode(ctx, cl, "controller-node", map[string]string{"kubernetes.io/os": "linux", "fake-role": "fake", nfsNodeSelectorKey: ""})
				makeNodeAsController(ctx, cl, "controller-node", controllerNamespace)
				prepareModulePod(ctx, cl, "csi-controller", controllerNamespace, "controller-node", controller.CSIControllerLabel)

				// create pending PVC
				preparePVC(ctx, cl, testNamespace, "pvc-1", provisionerNFS, v1.ClaimPending)

				// create some matching nodes without the csi-nfs label
				prepareNode(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "test-label": "value"})
				prepareNode(ctx, cl, "matching-node-without-label-2", map[string]string{"project": "test-1"})

				// create some matching nodes with the csi-nfs label
				prepareNode(ctx, cl, "matching-node-with-label-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})

				// create some non-matching nodes without the csi-nfs label
				prepareNode(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-2", "test-label": "value"})

				// create some non-matching nodes with the csi-nfs label
				prepareNode(ctx, cl, "non-matching-node-with-label-1", map[string]string{"project": "test-3", "test-label": "value", nfsNodeSelectorKey: ""})
				prepareNode(ctx, cl, "non-matching-node-with-label-2", map[string]string{"test-label": "value", nfsNodeSelectorKey: ""})

				err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
				Expect(err).NotTo(HaveOccurred())

				checkNodeLabels(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})
				checkNodeLabels(ctx, cl, "matching-node-without-label-2", map[string]string{"project": "test-1", nfsNodeSelectorKey: ""})

				checkNodeLabels(ctx, cl, "matching-node-with-label-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})

				checkNodeLabels(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-2", "test-label": "value"})

				checkNodeLabels(ctx, cl, "non-matching-node-with-label-1", map[string]string{"project": "test-3", "test-label": "value"})
				checkNodeLabels(ctx, cl, "non-matching-node-with-label-2", map[string]string{"test-label": "value"})

				checkNodeLabels(ctx, cl, "controller-node", map[string]string{"kubernetes.io/os": "linux", "fake-role": "fake", nfsNodeSelectorKey: ""})
			})

			It("9.5: Has pending PVC and csi-controller pod does not exist -> csi-nfs label should be removed from controller node", func() {
				// create NFSStorageClass
				nfsSCConfig.nodeSelector = metav1.LabelSelector{
					MatchLabels: map[string]string{"project": "test-1"},
				}
				nsc := generateNFSStorageClass(nfsSCConfig)
				Expect(cl.Create(ctx, nsc)).To(Succeed())
				recheckNSC := &v1alpha1.NFSStorageClass{}
				Expect(cl.Get(ctx, client.ObjectKey{Name: nfsSCConfig.Name}, recheckNSC)).To(Succeed())
				Expect(recheckNSC.Spec.WorkloadNodes.NodeSelector).To(Equal(&nfsSCConfig.nodeSelector))

				// create controller node with the csi-nfs label and no csi-controller pod
				prepareNode(ctx, cl, "controller-node", map[string]string{"kubernetes.io/os": "linux", "fake-role": "fake", nfsNodeSelectorKey: ""})
				makeNodeAsController(ctx, cl, "controller-node", controllerNamespace)

				// create pending PVC
				preparePVC(ctx, cl, testNamespace, "pvc-1", provisionerNFS, v1.ClaimPending)

				// create some matching nodes without the csi-nfs label
				prepareNode(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "test-label": "value"})
				prepareNode(ctx, cl, "matching-node-without-label-2", map[string]string{"project": "test-1"})

				// create some matching nodes with the csi-nfs label
				prepareNode(ctx, cl, "matching-node-with-label-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})

				// create some non-matching nodes without the csi-nfs label
				prepareNode(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-2", "test-label": "value"})

				// create some non-matching nodes with the csi-nfs label
				prepareNode(ctx, cl, "non-matching-node-with-label-1", map[string]string{"project": "test-3", "test-label": "value", nfsNodeSelectorKey: ""})
				prepareNode(ctx, cl, "non-matching-node-with-label-2", map[string]string{"test-label": "value", nfsNodeSelectorKey: ""})

				err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
				Expect(err).NotTo(HaveOccurred())

				checkNodeLabels(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})
				checkNodeLabels(ctx, cl, "matching-node-without-label-2", map[string]string{"project": "test-1", nfsNodeSelectorKey: ""})

				checkNodeLabels(ctx, cl, "matching-node-with-label-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})

				checkNodeLabels(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-2", "test-label": "value"})

				checkNodeLabels(ctx, cl, "non-matching-node-with-label-1", map[string]string{"project": "test-3", "test-label": "value"})
				checkNodeLabels(ctx, cl, "non-matching-node-with-label-2", map[string]string{"test-label": "value"})

				checkNodeLabels(ctx, cl, "controller-node", map[string]string{"kubernetes.io/os": "linux", "fake-role": "fake"})
			})
		})
		// Removing the csi-nfs label from nodes when pods with PVCs exist
		It("Scenario 10: Node not matching selector, not the controller, but has a pod with NFS PVC -> do not remove the label", func() {
			// create NFSStorageClass
			nfsSCConfig.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"project": "test-1"},
			}

			nsc := generateNFSStorageClass(nfsSCConfig)
			Expect(cl.Create(ctx, nsc)).To(Succeed())
			recheckNSC := &v1alpha1.NFSStorageClass{}
			Expect(cl.Get(ctx, client.ObjectKey{Name: nfsSCConfig.Name}, recheckNSC)).To(Succeed())
			Expect(recheckNSC.Spec.WorkloadNodes.NodeSelector).To(Equal(&nfsSCConfig.nodeSelector))

			// create node with the csi-nfs label that does not match the selector and has a pod with NFS PVC
			prepareNode(ctx, cl, "non-matching-node-with-label-and-pod-with-pvc", map[string]string{"project": "test-2", "test-label": "value", nfsNodeSelectorKey: ""})
			preparePodWithPVC(ctx, cl, testNamespace, "pod-with-pvc", "non-matching-node-with-label-and-pod-with-pvc", "pvc-1", provisionerNFS)

			// create some matching nodes without the csi-nfs label
			prepareNode(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "test-label": "value"})
			prepareNode(ctx, cl, "matching-node-without-label-2", map[string]string{"project": "test-1"})

			// create some matching nodes with the csi-nfs label
			prepareNode(ctx, cl, "matching-node-with-label-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})
			prepareNode(ctx, cl, "matching-node-with-label-2", map[string]string{"project": "test-1", nfsNodeSelectorKey: ""})

			// create some non-matching nodes without the csi-nfs label
			prepareNode(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-2", "test-label": "value"})
			prepareNode(ctx, cl, "non-matching-node-without-label-2", map[string]string{"project": "test-2"})

			// create some non-matching nodes with the csi-nfs label
			prepareNode(ctx, cl, "non-matching-node-with-label-1", map[string]string{"project": "test-3", "test-label": "value", nfsNodeSelectorKey: ""})
			prepareNode(ctx, cl, "non-matching-node-with-label-2", map[string]string{"test-label": "value", nfsNodeSelectorKey: ""})

			err := controller.ReconcileNodeSelector(ctx, cl, clusterWideCl, log, controllerNamespace)
			Expect(err).NotTo(HaveOccurred())

			checkNodeLabels(ctx, cl, "matching-node-without-label-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "matching-node-without-label-2", map[string]string{"project": "test-1", nfsNodeSelectorKey: ""})

			checkNodeLabels(ctx, cl, "matching-node-with-label-1", map[string]string{"project": "test-1", "test-label": "value", nfsNodeSelectorKey: ""})
			checkNodeLabels(ctx, cl, "matching-node-with-label-2", map[string]string{"project": "test-1", nfsNodeSelectorKey: ""})

			checkNodeLabels(ctx, cl, "non-matching-node-without-label-1", map[string]string{"project": "test-2", "test-label": "value"})
			checkNodeLabels(ctx, cl, "non-matching-node-without-label-2", map[string]string{"project": "test-2"})

			checkNodeLabels(ctx, cl, "non-matching-node-with-label-1", map[string]string{"project": "test-3", "test-label": "value"})
			checkNodeLabels(ctx, cl, "non-matching-node-with-label-2", map[string]string{"test-label": "value"})

			checkNodeLabels(ctx, cl, "non-matching-node-with-label-and-pod-with-pvc", map[string]string{"project": "test-2", "test-label": "value", nfsNodeSelectorKey: ""})
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
	Expect(cl.Get(ctx, client.ObjectKey{Name: "pod-with-pvc", Namespace: namespace}, recheckPod)).To(Succeed())
	Expect(recheckPod.Spec.NodeName).To(Equal(nodeName))
	Expect(recheckPod.Spec.Volumes).To(HaveLen(1))
	Expect(recheckPod.Spec.Volumes[0].VolumeSource.PersistentVolumeClaim.ClaimName).To(Equal(pvcName))
}
