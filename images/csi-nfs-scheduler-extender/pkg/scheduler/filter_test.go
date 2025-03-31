package scheduler_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/csi-nfs/images/csi-nfs-scheduler-extender/pkg/consts"
	"github.com/deckhouse/csi-nfs/images/csi-nfs-scheduler-extender/pkg/logger"
	"github.com/deckhouse/csi-nfs/images/csi-nfs-scheduler-extender/pkg/scheduler"
)

var _ = Describe("Filter tests", func() {
	var (
		ctx                 context.Context
		cl                  client.Client
		log                 logger.Logger
		controllerNamespace string
		testNamespace       string
		nfsSCConfig         NFSStorageClassConfig

		nfsNodeSelectorKey = "storage.deckhouse.io/csi-nfs-node"
		provisionerNFS     = consts.CSINFSProvisioner
		readOnlyFalse      = false
	)

	BeforeEach(func() {
		ctx = context.Background()
		log = logger.Logger{}

		controllerNamespace = "test-controller-ns"
		testNamespace = "test-namespace"

		cl = NewFakeClient()

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

	Context("Filter", func() {
		It("Scenario 1: NFSStorageClass exists without nodeSelector; all Linux nodes should be suitable", func() {
			nsc := generateNFSStorageClass(nfsSCConfig)
			Expect(cl.Create(ctx, nsc)).To(Succeed())

			prepareNode(ctx, cl, "linux-node-0", map[string]string{"kubernetes.io/os": "linux", nfsNodeSelectorKey: "", "test-label": "value"})
			prepareNode(ctx, cl, "linux-node-1", map[string]string{"kubernetes.io/os": "linux", nfsNodeSelectorKey: "", "test-label": "value2"})
			prepareNode(ctx, cl, "linux-node-2", map[string]string{"kubernetes.io/os": "linux"})
			prepareNode(ctx, cl, "non-linux-node-0", nil)
			nodeNames := []string{"linux-node-0", "linux-node-1", "linux-node-2", "non-linux-node-0"}

			preparePVC(ctx, cl, testNamespace, "pvc-0", nfsSCConfig.Name, provisionerNFS)
			preparePVC(ctx, cl, testNamespace, "pvc-1", "another-storage-class", "another-provisioner")

			podWithNFS1 := preparePodWithVolumes(ctx, cl, testNamespace, "pod-with-nfs-volumes-1", []string{"pvc-0"})
			podWithNFS2 := preparePodWithVolumes(ctx, cl, testNamespace, "pod-with-nfs-volumes-2", []string{"pvc-0", "pvc-1"})
			podWithoutNFS1 := preparePodWithVolumes(ctx, cl, testNamespace, "pod-without-nfs-volumes", []string{"pvc-1"})

			checkFilter(ctx, cl, log, podWithNFS1, nodeNames, []string{"linux-node-0", "linux-node-1", "linux-node-2"}, []string{"non-linux-node-0"})
			checkFilter(ctx, cl, log, podWithNFS2, nodeNames, []string{"linux-node-0", "linux-node-1", "linux-node-2"}, []string{"non-linux-node-0"})
			checkFilter(ctx, cl, log, podWithoutNFS1, nodeNames, []string{"linux-node-0", "linux-node-1", "linux-node-2", "non-linux-node-0"}, []string{})
		})

		It("Scenario 2: NFSStorageClass exists with nodeSelector; only nodes matching the selector should be suitable", func() {
			nfsSCConfig.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"project": "test-1"},
			}

			nsc := generateNFSStorageClass(nfsSCConfig)
			Expect(cl.Create(ctx, nsc)).To(Succeed())

			prepareNode(ctx, cl, "matching-node-0", map[string]string{"project": "test-1"})
			prepareNode(ctx, cl, "matching-node-1", map[string]string{"project": "test-1", "test-label": "value"})
			prepareNode(ctx, cl, "non-matching-node-0", map[string]string{"project": "test-2"})
			prepareNode(ctx, cl, "non-matching-node-1", nil)
			nodeNames := []string{"matching-node-0", "matching-node-1", "non-matching-node-0", "non-matching-node-1"}

			preparePVC(ctx, cl, testNamespace, "pvc-0", nfsSCConfig.Name, provisionerNFS)

			podWithNFS := preparePodWithVolumes(ctx, cl, testNamespace, "pod-with-nfs-volumes", []string{"pvc-0"})
			podWithoutVolumes := preparePodWithVolumes(ctx, cl, testNamespace, "pod-without-nfs-volumes", []string{})

			checkFilter(ctx, cl, log, podWithNFS, nodeNames, []string{"matching-node-0", "matching-node-1"}, []string{"non-matching-node-0", "non-matching-node-1"})
			checkFilter(ctx, cl, log, podWithoutVolumes, nodeNames, []string{"matching-node-0", "matching-node-1", "non-matching-node-0", "non-matching-node-1"}, []string{})
		})

		It("Scenario 3: Exist several NFSStorageClasses with different nodeSelectors; pods should be scheduled on nodes matching all selectors at once for all their PVCs", func() {
			nfsSCConfig.Name = "test-nfs-sc-1"
			nfsSCConfig.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"project": "test-1"},
			}
			nsc1 := generateNFSStorageClass(nfsSCConfig)
			Expect(cl.Create(ctx, nsc1)).To(Succeed())

			nfsSCConfig.Name = "test-nfs-sc-2"
			nfsSCConfig.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"role": "common"},
			}
			nsc2 := generateNFSStorageClass(nfsSCConfig)
			Expect(cl.Create(ctx, nsc2)).To(Succeed())

			nfsSCConfig.Name = "test-nfs-sc-3"
			nfsSCConfig.nodeSelector = metav1.LabelSelector{
				MatchLabels: map[string]string{"environment": "production"},
			}
			nsc3 := generateNFSStorageClass(nfsSCConfig)
			Expect(cl.Create(ctx, nsc3)).To(Succeed())

			prepareNode(ctx, cl, "matching-sc1-node-0", map[string]string{"project": "test-1"})
			prepareNode(ctx, cl, "matching-sc1-node-1", map[string]string{"project": "test-1", "test-label": "value"})
			prepareNode(ctx, cl, "matching-sc2-node-0", map[string]string{"role": "common"})
			prepareNode(ctx, cl, "matching-sc2-node-1", map[string]string{"role": "common", "test-label": "value"})
			prepareNode(ctx, cl, "matching-sc1-and-sc2-node-0", map[string]string{"project": "test-1", "role": "common"})
			prepareNode(ctx, cl, "matching-sc1-and-sc2-node-1", map[string]string{"project": "test-1", "role": "common", "test-label": "value"})
			prepareNode(ctx, cl, "matching-sc3-node-0", map[string]string{"environment": "production"})
			prepareNode(ctx, cl, "non-matching-node-0", map[string]string{"project": "test-2"})
			prepareNode(ctx, cl, "non-matching-node-1", nil)

			nodeNames := []string{"matching-sc1-node-0", "matching-sc1-node-1", "matching-sc2-node-0", "matching-sc2-node-1", "matching-sc1-and-sc2-node-0", "matching-sc1-and-sc2-node-1", "matching-sc3-node-0", "non-matching-node-0", "non-matching-node-1"}

			preparePVC(ctx, cl, testNamespace, "pvc-sc1", nsc1.Name, provisionerNFS)
			preparePVC(ctx, cl, testNamespace, "pvc-sc2", nsc2.Name, provisionerNFS)
			preparePVC(ctx, cl, testNamespace, "pvc-sc3", nsc3.Name, provisionerNFS)
			preparePVC(ctx, cl, testNamespace, "pvc-another-sc", "another-storage-class", "another-provisioner")

			podWithNFSsc1 := preparePodWithVolumes(ctx, cl, testNamespace, "pod-with-nfs-volumes-sc1", []string{"pvc-sc1"})
			podWithNFSsc2 := preparePodWithVolumes(ctx, cl, testNamespace, "pod-with-nfs-volumes-sc2", []string{"pvc-sc2"})
			podWithNFSsc1Andsc2 := preparePodWithVolumes(ctx, cl, testNamespace, "pod-with-nfs-volumes-sc1-and-sc2", []string{"pvc-sc1", "pvc-sc2"})
			podWithNFSsc1Andsc2Andsc3 := preparePodWithVolumes(ctx, cl, testNamespace, "pod-with-nfs-volumes-sc1-and-sc2-and-sc3", []string{"pvc-sc1", "pvc-sc2", "pvc-sc3"})
			podWithoutNFS := preparePodWithVolumes(ctx, cl, testNamespace, "pod-without-nfs-volumes", []string{"pvc-another-sc"})
			podWithNFSsc1AndAnotherSC := preparePodWithVolumes(ctx, cl, testNamespace, "pod-with-nfs-volumes-sc1-and-another-sc", []string{"pvc-sc1", "pvc-another-sc"})
			podWithNFSsc2AndAnotherSC := preparePodWithVolumes(ctx, cl, testNamespace, "pod-with-nfs-volumes-sc2-and-another-sc", []string{"pvc-sc2", "pvc-another-sc"})
			podWithNFSsc1Andsc2AndAnotherSC := preparePodWithVolumes(ctx, cl, testNamespace, "pod-with-nfs-volumes-sc1-and-sc2-and-another-sc", []string{"pvc-sc1", "pvc-sc2", "pvc-another-sc"})
			podWithoutVolumes := preparePodWithVolumes(ctx, cl, testNamespace, "pod-without-volumes", []string{})

			checkFilter(ctx, cl, log, podWithNFSsc1, nodeNames, []string{"matching-sc1-node-0", "matching-sc1-node-1", "matching-sc1-and-sc2-node-0", "matching-sc1-and-sc2-node-1"}, []string{"matching-sc2-node-0", "matching-sc2-node-1", "matching-sc3-node-0", "non-matching-node-0", "non-matching-node-1"})
			checkFilter(ctx, cl, log, podWithNFSsc2, nodeNames, []string{"matching-sc2-node-0", "matching-sc2-node-1", "matching-sc1-and-sc2-node-0", "matching-sc1-and-sc2-node-1"}, []string{"matching-sc1-node-0", "matching-sc1-node-1", "matching-sc3-node-0", "non-matching-node-0", "non-matching-node-1"})
			checkFilter(ctx, cl, log, podWithNFSsc1Andsc2, nodeNames, []string{"matching-sc1-and-sc2-node-0", "matching-sc1-and-sc2-node-1"}, []string{"matching-sc1-node-0", "matching-sc1-node-1", "matching-sc2-node-0", "matching-sc2-node-1", "matching-sc3-node-0", "non-matching-node-0", "non-matching-node-1"})
			checkFilter(ctx, cl, log, podWithNFSsc1Andsc2Andsc3, nodeNames, []string{}, []string{"matching-sc1-node-0", "matching-sc1-node-1", "matching-sc2-node-0", "matching-sc2-node-1", "matching-sc1-and-sc2-node-0", "matching-sc1-and-sc2-node-1", "matching-sc3-node-0", "non-matching-node-0", "non-matching-node-1"})
			checkFilter(ctx, cl, log, podWithoutNFS, nodeNames, []string{"matching-sc1-node-0", "matching-sc1-node-1", "matching-sc2-node-0", "matching-sc2-node-1", "matching-sc1-and-sc2-node-0", "matching-sc1-and-sc2-node-1", "matching-sc3-node-0", "non-matching-node-0", "non-matching-node-1"}, []string{})
			checkFilter(ctx, cl, log, podWithNFSsc1AndAnotherSC, nodeNames, []string{"matching-sc1-node-0", "matching-sc1-node-1", "matching-sc1-and-sc2-node-0", "matching-sc1-and-sc2-node-1"}, []string{"matching-sc2-node-0", "matching-sc2-node-1", "matching-sc3-node-0", "non-matching-node-0", "non-matching-node-1"})
			checkFilter(ctx, cl, log, podWithNFSsc2AndAnotherSC, nodeNames, []string{"matching-sc2-node-0", "matching-sc2-node-1", "matching-sc1-and-sc2-node-0", "matching-sc1-and-sc2-node-1"}, []string{"matching-sc1-node-0", "matching-sc1-node-1", "matching-sc3-node-0", "non-matching-node-0", "non-matching-node-1"})
			checkFilter(ctx, cl, log, podWithNFSsc1Andsc2AndAnotherSC, nodeNames, []string{"matching-sc1-and-sc2-node-0", "matching-sc1-and-sc2-node-1"}, []string{"matching-sc1-node-0", "matching-sc1-node-1", "matching-sc2-node-0", "matching-sc2-node-1", "matching-sc3-node-0", "non-matching-node-0", "non-matching-node-1"})
			checkFilter(ctx, cl, log, podWithoutVolumes, nodeNames, []string{"matching-sc1-node-0", "matching-sc1-node-1", "matching-sc2-node-0", "matching-sc2-node-1", "matching-sc1-and-sc2-node-0", "matching-sc1-and-sc2-node-1", "matching-sc3-node-0", "non-matching-node-0", "non-matching-node-1"}, []string{})
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

func generatePodWithVolumes(namespace, name string, volumes []corev1.Volume) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: corev1.PodSpec{
			Volumes: volumes,
		},
	}
}

func preparePodWithVolumes(ctx context.Context, cl client.Client, namespace, name string, pvcNames []string) *corev1.Pod {
	volumes := generateVolumes(pvcNames)
	pod := generatePodWithVolumes(namespace, name, volumes)
	Expect(cl.Create(ctx, pod)).To(Succeed())

	recheckPod := &corev1.Pod{}
	Expect(cl.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, recheckPod)).To(Succeed())
	Expect(recheckPod.Spec.Volumes).To(Equal(volumes))
	return recheckPod
}

func generateVolumes(pvcNames []string) []corev1.Volume {
	var volumes []corev1.Volume
	for _, pvcName := range pvcNames {
		volumes = append(volumes, corev1.Volume{
			Name: "test-vol-" + pvcName,
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvcName,
				},
			},
		})
	}
	return volumes
}

func generatePVC(namespace, name, storageClassName, provisioner string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				"volume.kubernetes.io/storage-provisioner": provisioner,
			},
		},
		Spec: v1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClassName,
		},
		Status: v1.PersistentVolumeClaimStatus{
			Phase: v1.ClaimBound,
		},
	}
}

func preparePVC(ctx context.Context, cl client.Client, namespace, name, storageClassName, provisioner string) {
	pvc := generatePVC(namespace, name, storageClassName, provisioner)
	Expect(cl.Create(ctx, pvc)).To(Succeed())

	pvc = &corev1.PersistentVolumeClaim{}
	Expect(cl.Get(ctx, client.ObjectKey{Name: name, Namespace: namespace}, pvc)).To(Succeed())
	Expect(pvc.Annotations).To(HaveKey("volume.kubernetes.io/storage-provisioner"))
	Expect(pvc.Annotations["volume.kubernetes.io/storage-provisioner"]).To(Equal(provisioner))
	Expect(pvc.Spec.StorageClassName).To(Equal(&storageClassName))
}

type NFSStorageClassConfig struct {
	Name              string
	Host              string
	Share             string
	NFSVersion        string
	MountMode         string
	Timeout           int
	Retransmissions   int
	ReadOnly          bool
	ChmodPermissions  string
	ReclaimPolicy     string
	VolumeBindingMode string
	nodeSelector      metav1.LabelSelector
}

func generateNFSStorageClass(cfg NFSStorageClassConfig) *v1alpha1.NFSStorageClass {
	nfsStorageClass := &v1alpha1.NFSStorageClass{}
	nfsStorageClass.Name = cfg.Name
	nfsStorageClass.Spec.Connection = &v1alpha1.NFSStorageClassConnection{
		Host:       cfg.Host,
		Share:      cfg.Share,
		NFSVersion: cfg.NFSVersion,
	}
	nfsStorageClass.Spec.MountOptions = &v1alpha1.NFSStorageClassMountOptions{
		MountMode:       cfg.MountMode,
		Timeout:         cfg.Timeout,
		Retransmissions: cfg.Retransmissions,
		ReadOnly:        &cfg.ReadOnly,
	}

	nfsStorageClass.Spec.ChmodPermissions = cfg.ChmodPermissions
	nfsStorageClass.Spec.ReclaimPolicy = cfg.ReclaimPolicy
	nfsStorageClass.Spec.VolumeBindingMode = cfg.VolumeBindingMode

	if cfg.nodeSelector.MatchLabels != nil || cfg.nodeSelector.MatchExpressions != nil {
		nfsStorageClass.Spec.WorkloadNodes = &v1alpha1.NFSStorageClassWorkloadNodes{
			NodeSelector: &cfg.nodeSelector,
		}
	}

	return nfsStorageClass
}

func checkFilter(ctx context.Context, cl client.Client, log logger.Logger, pod *corev1.Pod, nodeNames []string, expectedSuitable, expectedFailed []string) {
	schedulerExtender, err := scheduler.NewHandler(ctx, cl, log)
	Expect(err).NotTo(HaveOccurred())

	inputData := scheduler.ExtenderArgs{
		Pod:       pod,
		NodeNames: &nodeNames,
	}
	reqBody, err := json.Marshal(inputData)
	Expect(err).NotTo(HaveOccurred())

	req, err := http.NewRequest(http.MethodPost, "/scheduler/filter", bytes.NewReader(reqBody))
	Expect(err).NotTo(HaveOccurred())

	rr := httptest.NewRecorder()
	schedulerExtender.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		Expect(rr.Body.String()).To(BeNil())
		return
	}
	Expect(rr.Code).To(Equal(http.StatusOK))
	var result scheduler.ExtenderFilterResult
	err = json.Unmarshal(rr.Body.Bytes(), &result)
	Expect(err).NotTo(HaveOccurred())
	checkResult(result, expectedSuitable, expectedFailed)
}

func checkResult(result scheduler.ExtenderFilterResult, expectedSuitable, expectedFailed []string) {
	Expect(*result.NodeNames).To(HaveLen(len(expectedSuitable)), fmt.Sprintf("expected: %v, got: %v", expectedSuitable, *result.NodeNames))
	Expect(*result.NodeNames).To(ConsistOf(expectedSuitable), fmt.Sprintf("expected: %v, got: %v", expectedSuitable, *result.NodeNames))
	Expect(result.FailedNodes).To(HaveLen(len(expectedFailed)), fmt.Sprintf("expected: %v, got: %v", expectedFailed, result.FailedNodes))

	if len(expectedFailed) == 0 {
		Expect(result.FailedNodes).To(BeEmpty())
		return
	}

	expectedFailedMap := make(scheduler.FailedNodesMap)
	for _, nodeName := range expectedFailed {
		expectedFailedMap[nodeName] = "node is not selected by user selectors"
	}

	Expect(result.FailedNodes).To(Equal(expectedFailedMap), fmt.Sprintf("expected: %v, got: %v", expectedFailedMap, result.FailedNodes))
}
