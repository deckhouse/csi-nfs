/*
Copyright 2024 Flant JSC

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

package scheduler

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/csi-nfs/images/csi-nfs-scheduler-extender/pkg/logger"
)

const (
	annotationBetaStorageProvisioner = "volume.beta.kubernetes.io/storage-provisioner"
	annotationStorageProvisioner     = "volume.kubernetes.io/storage-provisioner"
)

var (
	DefaultNodeSelector = &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"kubernetes.io/os": "linux",
		},
	}
)

func shouldProcessPod(ctx context.Context, cl client.Client, log logger.Logger, pod *corev1.Pod, targetProvisioner string) (bool, []corev1.Volume, error) {
	log.Trace(fmt.Sprintf("[ShouldProcessPod] targetProvisioner=%s, pod: %+v", targetProvisioner, pod))
	var discoveredProvisioner string
	shouldProcessPod := false
	targetProvisionerVolumes := make([]corev1.Volume, 0)

	pvcs := &corev1.PersistentVolumeClaimList{}
	err := cl.List(ctx, pvcs, client.InNamespace(pod.Namespace))
	if err != nil {
		return false, nil, fmt.Errorf("[ShouldProcessPod] error getting PVCs in namespace %s: %v", pod.Namespace, err)
	}

	pvcMap := make(map[string]*corev1.PersistentVolumeClaim, len(pvcs.Items))
	for _, pvc := range pvcs.Items {
		pvcMap[pvc.Name] = &pvc
	}

	for _, volume := range pod.Spec.Volumes {
		if volume.PersistentVolumeClaim == nil {
			log.Trace(fmt.Sprintf("[ShouldProcessPod] skip volume %s because it doesn't have PVC", volume.Name))
			continue
		}

		log.Trace(fmt.Sprintf("[ShouldProcessPod] process volume: %+v that has pvc: %+v", volume, volume.PersistentVolumeClaim))
		pvcName := volume.PersistentVolumeClaim.ClaimName
		pvc, found := pvcMap[pvcName]
		if !found {
			return false, nil, fmt.Errorf("[ShouldProcessPod] found no pvc %s in namespace %s", pvcName, pod.Namespace)
		}

		log.Trace(fmt.Sprintf("[ShouldProcessPod] Successfully get PVC %s/%s: %+v", pod.Namespace, pvcName, pvc))

		discoveredProvisioner, err = getProvisionerFromPVC(ctx, cl, log, pvc)
		if err != nil {
			return false, nil, fmt.Errorf("[ShouldProcessPod] error getting provisioner from PVC %s/%s: %v", pod.Namespace, pvcName, err)
		}
		log.Trace(fmt.Sprintf("[ShouldProcessPod] discovered provisioner: %s", discoveredProvisioner))
		if discoveredProvisioner == targetProvisioner {
			log.Trace(fmt.Sprintf("[ShouldProcessPod] provisioner matches targetProvisioner %s. Pod: %s/%s", pod.Namespace, pod.Name, targetProvisioner))
			shouldProcessPod = true
			targetProvisionerVolumes = append(targetProvisionerVolumes, volume)
		}
		log.Trace(fmt.Sprintf("[ShouldProcessPod] provisioner %s doesn't match targetProvisioner %s. Skip volume %s.", discoveredProvisioner, targetProvisioner, volume.Name))
	}

	if shouldProcessPod {
		log.Trace(fmt.Sprintf("[ShouldProcessPod] targetProvisioner %s found in pod volumes. Pod: %s/%s. Volumes that match: %+v", targetProvisioner, pod.Namespace, pod.Name, targetProvisionerVolumes))
		return true, targetProvisionerVolumes, nil
	}

	log.Trace(fmt.Sprintf("[ShouldProcessPod] can't find targetProvisioner %s in pod volumes. Skip pod: %s/%s", targetProvisioner, pod.Namespace, pod.Name))
	return false, nil, nil
}

func getNodeNames(inputData ExtenderArgs) ([]string, error) {
	if inputData.NodeNames != nil && len(*inputData.NodeNames) > 0 {
		return *inputData.NodeNames, nil
	}

	if inputData.Nodes != nil && len(inputData.Nodes.Items) > 0 {
		nodeNames := make([]string, 0, len(inputData.Nodes.Items))
		for _, node := range inputData.Nodes.Items {
			nodeNames = append(nodeNames, node.Name)
		}
		return nodeNames, nil
	}

	return nil, fmt.Errorf("no nodes provided")
}

func GetNFSStorageClassesFromVolumes(ctx context.Context, cl client.Client, log logger.Logger, namespace string, volumes []corev1.Volume) (*v1alpha1.NFSStorageClassList, error) {
	nfsStorageClasses := &v1alpha1.NFSStorageClassList{}
	for _, volume := range volumes {
		log.Trace(fmt.Sprintf("[GetNFSStorageClassesFromVolumes] process volume: %+v", volume))
		if volume.PersistentVolumeClaim != nil {
			pvcName := volume.PersistentVolumeClaim.ClaimName
			pvc := &corev1.PersistentVolumeClaim{}
			err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: pvcName}, pvc)
			if err != nil {
				return nil, fmt.Errorf("error getting PVC %s: %v", pvcName, err)
			}
			log.Trace(fmt.Sprintf("[GetNFSStorageClassesFromVolumes] get pvc: %+v", pvc))
			storageClassName := pvc.Spec.StorageClassName
			if storageClassName == nil {
				return nil, fmt.Errorf("PVC %s has no storage class", pvcName)
			}

			log.Trace(fmt.Sprintf("[GetNFSStorageClassesFromVolumes] get storage class name: %s", *storageClassName))
			nsc := &v1alpha1.NFSStorageClass{}
			err = cl.Get(ctx, client.ObjectKey{Name: *storageClassName}, nsc)
			if err != nil {
				return nil, fmt.Errorf("error getting NFSStorageClass %s: %v", *storageClassName, err)
			}
			log.Trace(fmt.Sprintf("[GetNFSStorageClassesFromVolumes] get NFSStorageClass: %+v", nsc))
			nfsStorageClasses.Items = append(nfsStorageClasses.Items, *nsc)
		}
	}
	return nfsStorageClasses, nil
}

func GetKubernetesNodeNamesBySelector(ctx context.Context, cl client.Client, nodeSelector map[string]string) ([]string, error) {
	selectedK8sNodes, err := GetKubernetesNodesBySelector(ctx, cl, nodeSelector)
	if err != nil {
		return nil, err
	}
	nodeNames := make([]string, 0, len(selectedK8sNodes.Items))
	for _, node := range selectedK8sNodes.Items {
		nodeNames = append(nodeNames, node.Name)
	}
	return nodeNames, nil
}

func GetKubernetesNodesBySelector(ctx context.Context, cl client.Client, nodeSelector map[string]string) (*corev1.NodeList, error) {
	selectedK8sNodes := &corev1.NodeList{}
	err := cl.List(ctx, selectedK8sNodes, client.MatchingLabels(nodeSelector))
	return selectedK8sNodes, err
}

func GetNodeSelectorFromNFSStorageClasses(log logger.Logger, nfsStorageClasses *v1alpha1.NFSStorageClassList) []*metav1.LabelSelector {
	nodeSelectorList := []*metav1.LabelSelector{}
	for _, nfsStorageClass := range nfsStorageClasses.Items {
		log.Debug(fmt.Sprintf("[GetNodeSelectorFromNFSStorageClasses] Process NFSStorageClass %s.", nfsStorageClass.Name))
		if nfsStorageClass.Spec.WorkloadNodes == nil || nfsStorageClass.Spec.WorkloadNodes.NodeSelector == nil {
			log.Debug(fmt.Sprintf("[GetNodeSelectorFromNFSStorageClasses] NFSStorageClass %s has not NodeSelector. Return default NodeSelector %+v.", nfsStorageClass.Name, DefaultNodeSelector))
			return []*metav1.LabelSelector{DefaultNodeSelector}
		}
		log.Debug(fmt.Sprintf("[GetNodeSelectorFromNFSStorageClasses] Add NodeSelector %+v from NFSStorageClass %s.", nfsStorageClass.Spec.WorkloadNodes.NodeSelector, nfsStorageClass.Name))
		nodeSelectorList = append(nodeSelectorList, nfsStorageClass.Spec.WorkloadNodes.NodeSelector)
	}

	// TODO: make unique nodeSelectorList
	return nodeSelectorList
}

func GetCommonNodesByNodeSelectorList(ctx context.Context, cl client.Client, log logger.Logger, nodeSelectorList []*metav1.LabelSelector) ([]string, error) {
	if len(nodeSelectorList) == 0 {
		return nil, fmt.Errorf("[GetCommonNodesByNodeSelectorList] Empty nodeSelectorList")
	}

	commonNodeNames := make([]string, 0)
	for i, nodeSelector := range nodeSelectorList {
		log.Debug(fmt.Sprintf("[GetCommonNodesByNodeSelectorList] Process NodeSelector %d: %+v", i, nodeSelector))
		selectedNodeNames, err := GetKubernetesNodeNamesBySelector(ctx, cl, nodeSelector.MatchLabels)
		if err != nil {
			return nil, fmt.Errorf("[GetCommonNodesByNodeSelectorList] Error getting nodes by selector: %v", err)
		}
		log.Debug(fmt.Sprintf("[GetCommonNodesByNodeSelectorList] Node names selected by NodeSelector %d: %+v", i, selectedNodeNames))

		if i == 0 {
			commonNodeNames = selectedNodeNames
		} else {
			commonNodeNames = getCommonNodeNames(commonNodeNames, selectedNodeNames)
		}
	}
	log.Debug(fmt.Sprintf("[GetCommonNodesByNodeSelectorList] Common nodes: %+v", commonNodeNames))
	return commonNodeNames, nil
}

func getCommonNodeNames(nodeNames, selectedNodeNames []string) []string {
	selectedNodeNamesMap := make(map[string]struct{}, len(selectedNodeNames))
	for _, nodeName := range selectedNodeNames {
		selectedNodeNamesMap[nodeName] = struct{}{}
	}

	commonNodeNames := make([]string, 0)
	for _, nodeName := range nodeNames {
		if _, ok := selectedNodeNamesMap[nodeName]; ok {
			commonNodeNames = append(commonNodeNames, nodeName)
		}
	}
	return commonNodeNames
}

func getProvisionerFromPVC(ctx context.Context, cl client.Client, log logger.Logger, pvc *corev1.PersistentVolumeClaim) (string, error) {
	discoveredProvisioner := ""

	log.Trace(fmt.Sprintf("[getProvisionerFromPVC] check provisioner in pvc annotations: %+v", pvc.Annotations))
	discoveredProvisioner = pvc.Annotations[annotationStorageProvisioner]
	if discoveredProvisioner != "" {
		log.Trace(fmt.Sprintf("[getProvisionerFromPVC] discovered provisioner in pvc annotations: %s", discoveredProvisioner))
	} else {
		discoveredProvisioner = pvc.Annotations[annotationBetaStorageProvisioner]
		log.Trace(fmt.Sprintf("[getProvisionerFromPVC] discovered provisioner in beta pvc annotations: %s", discoveredProvisioner))
	}

	if discoveredProvisioner == "" && pvc.Spec.StorageClassName != nil && *pvc.Spec.StorageClassName != "" {
		log.Trace(fmt.Sprintf("[getProvisionerFromPVC] can't find provisioner in pvc annotations, check in storageClass with name: %s", *pvc.Spec.StorageClassName))
		storageClass := &storagev1.StorageClass{}
		err := cl.Get(ctx, client.ObjectKey{Name: *pvc.Spec.StorageClassName}, storageClass)
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				return "", fmt.Errorf("[getProvisionerFromPVC] error getting StorageClass %s: %v", *pvc.Spec.StorageClassName, err)
			}
			log.Warning(fmt.Sprintf("[getProvisionerFromPVC] StorageClass %s for PVC %s/%s not found", *pvc.Spec.StorageClassName, pvc.Namespace, pvc.Name))
		}
		discoveredProvisioner = storageClass.Provisioner
		log.Trace(fmt.Sprintf("[getProvisionerFromPVC] discover provisioner %s in storageClass: %+v", discoveredProvisioner, storageClass))
	}

	if discoveredProvisioner == "" && pvc.Spec.VolumeName != "" {
		log.Trace(fmt.Sprintf("[getProvisionerFromPVC] can't find provisioner in pvc annotations and StorageClass, check in PV with name: %s", pvc.Spec.VolumeName))
		pv := &corev1.PersistentVolume{}
		err := cl.Get(ctx, client.ObjectKey{Name: pvc.Spec.VolumeName}, pv)
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				return "", fmt.Errorf("[getProvisionerFromPVC] error getting PV %s for PVC %s/%s: %v", pvc.Spec.VolumeName, pvc.Namespace, pvc.Name, err)
			}
			log.Warning(fmt.Sprintf("[getProvisionerFromPVC] PV %s for PVC %s/%s not found", pvc.Spec.VolumeName, pvc.Namespace, pvc.Name))
		}

		if pv.Spec.CSI != nil {
			discoveredProvisioner = pv.Spec.CSI.Driver
		}

		log.Trace(fmt.Sprintf("[getProvisionerFromPVC] discover provisioner %s in PV: %+v", discoveredProvisioner, pv))
	}

	return discoveredProvisioner, nil
}
