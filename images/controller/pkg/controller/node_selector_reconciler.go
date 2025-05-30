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

package controller

import (
	"context"
	"fmt"
	"time"

	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	v1alpha1 "github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/csi-nfs/images/controller/pkg/config"
	"github.com/deckhouse/csi-nfs/images/controller/pkg/logger"
)

const (
	NodeSelectorReconcilerName = "nfs-node-selector-reconciler"
	NFSNodeLabelKey            = "storage.deckhouse.io/csi-nfs-node"
)

var (
	nfsNodeLabels                      = map[string]string{NFSNodeLabelKey: ""}
	NFSNodeSelector                    = map[string]string{NFSNodeLabelKey: ""}
	CSIControllerLabel                 = map[string]string{"app": "csi-controller"}
	CSINodeLabel                       = map[string]string{"app": "csi-nfs"}
	csiNFSExternalSnapshotterLeaseName = "external-snapshotter-leader-nfs-csi-k8s-io"
	ModulePodSelectorList              = []map[string]string{
		CSIControllerLabel,
		CSINodeLabel,
	}
	DefaultNodeSelector = &metav1.LabelSelector{
		MatchLabels: map[string]string{
			"kubernetes.io/os": "linux",
		},
	}
)

func RunNodeSelectorReconciler(ctx context.Context, mgr manager.Manager, cfg config.Options, log logger.Logger) {
	cl := mgr.GetClient()

	clusterWideClient := mgr.GetAPIReader()
	go func() {
		for {
			log.Info("Start reconcile of NFS node selectors.")
			err := ReconcileNodeSelector(ctx, cl, clusterWideClient, log, cfg.ControllerNamespace)
			if err != nil {
				log.Error(err, "Failed reconcile of NFS node selectors.")
			}
			log.Info("END reconcile of NFS node selectors.")

			log.Info("Start reconcile of module pods.")
			err = ReconcileModulePods(ctx, cl, clusterWideClient, log, cfg.ControllerNamespace, NFSNodeSelector, ModulePodSelectorList)
			if err != nil {
				log.Error(err, "Failed reconcile of module pods.")
			}
			log.Info("END reconcile of module pods.")

			timer := time.NewTimer(cfg.RequeueNodeSelectorInterval * time.Second)

			select {
			case <-ctx.Done():
				log.Info("Context cancelled. Stopping NodeSelectorReconciler.")
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}()
}

func ReconcileNodeSelector(ctx context.Context, cl client.Client, clusterWideClient client.Reader, log logger.Logger, namespace string) error {
	nfsStorageClasses := &v1alpha1.NFSStorageClassList{}
	err := cl.List(ctx, nfsStorageClasses)
	if err != nil {
		err = fmt.Errorf("[ReconcileNodeSelector] Failed get NFSStorageClasses: %w", err)
		return err
	}

	log.Trace(fmt.Sprintf("[GetNodeSelectorFromNFSStorageClasses] Found %d NFSStorageClasses: %+v", len(nfsStorageClasses.Items), nfsStorageClasses.Items))
	userNodeSelectorList := GetNodeSelectorFromNFSStorageClasses(log, nfsStorageClasses)
	log.Debug(fmt.Sprintf("[reconcileNodeSelector] User node selector list: %+v", userNodeSelectorList))

	selectedNodes, err := GetNodesBySelectorList(ctx, cl, log, userNodeSelectorList)
	if err != nil {
		err = fmt.Errorf("[reconcileNodeSelector] Failed get nodes by user node selector list: %+v: %w", userNodeSelectorList, err)
		return err
	}

	if len(selectedNodes.Items) != 0 {
		selectedNodeNames := []string{}
		for _, node := range selectedNodes.Items {
			selectedNodeNames = append(selectedNodeNames, node.Name)
		}
		log.Info(fmt.Sprintf("[reconcileNodeSelector] Found %d nodes: %v; by user node selector list: %+v.", len(selectedNodes.Items), selectedNodeNames, userNodeSelectorList))
		log.Trace(fmt.Sprintf("[reconcileNodeSelector] Nodes: %+v", selectedNodes.Items))

		for _, node := range selectedNodes.Items {
			log.Info(fmt.Sprintf("[reconcileNodeSelector] Process labels for node: %s", node.Name))
			err := AddLabelsToNode(ctx, cl, log, node, nfsNodeLabels)
			if err != nil {
				err = fmt.Errorf("[reconcileNodeSelector] Failed add labels %+v to node: %s: %w", nfsNodeLabels, node.Name, err)
				return err
			}
		}
	}

	csiNFSNodes, err := GetNodesBySelector(ctx, cl, NFSNodeSelector)
	if err != nil {
		err = fmt.Errorf("[reconcileNodeSelector] Failed get nodes from Kubernetes by selector: %v: %w", NFSNodeSelector, err)
		return err
	}

	nodesToRemove := DiffNodeLists(csiNFSNodes, selectedNodes)

	if len(nodesToRemove.Items) == 0 {
		log.Info("[reconcileNodeSelector] Successfully reconciled NFS node selectors.")
		return nil
	}

	nodeNamesToRemove := []string{}
	for _, node := range nodesToRemove.Items {
		nodeNamesToRemove = append(nodeNamesToRemove, node.Name)
	}
	log.Warning(fmt.Sprintf("[reconcileNodeSelector] Found nodes that not in selected nodes by user defined node selector list %+v. Remove csi-nfs node label %v from them", userNodeSelectorList, nfsNodeLabels))
	log.Info(fmt.Sprintf("[reconcileNodeSelector] Nodes to remove: %v", nodeNamesToRemove))
	log.Trace(fmt.Sprintf("[reconcileNodeSelector] Nodes: %+v", nodesToRemove.Items))

	controllerNodeName, err := GetCCSIControllerNodeName(ctx, cl, namespace, csiNFSExternalSnapshotterLeaseName, CSIControllerLabel)
	if err != nil {
		err = fmt.Errorf("[reconcileNodeSelector] Failed get csi-nfs controller node name: %w", err)
		return err
	}

	namespaceList := &corev1.NamespaceList{}
	err = cl.List(ctx, namespaceList)
	if err != nil {
		err = fmt.Errorf("[reconcileNodeSelector] Failed get namespaces: %w", err)
		return err
	}
	log.Debug(fmt.Sprintf("[reconcileNodeSelector] Found %d namespaces.", len(namespaceList.Items)))

	podsMapWithNFSVolume, err := GetPodsMapWithNFSVolume(ctx, clusterWideClient, log, namespaceList)
	if err != nil {
		err = fmt.Errorf("[reconcileNodeSelector] Failed get pods with NFS volume: %w", err)
		return err
	}
	log.Debug(fmt.Sprintf("[reconcileNodeSelector] Pods with NFS volume: %+v", podsMapWithNFSVolume))

	for _, node := range nodesToRemove.Items {
		log.Info(fmt.Sprintf("[reconcileNodeSelector] Process remove label for node: %s", node.Name))

		if node.Name == controllerNodeName {
			log.Warning(fmt.Sprintf("[reconcileNodeSelector] Node %s is csi-nfs controller node!", node.Name))
			csiControllerRemovable, err := IsCSIControllerRemovable(ctx, clusterWideClient, log, NFSStorageClassProvisioner, namespaceList)
			if err != nil {
				err = fmt.Errorf("[reconcileNodeSelector] Failed check if can remove csi-nfs controller node: %w", err)
				return err
			}

			if !csiControllerRemovable {
				log.Warning(fmt.Sprintf("[reconcileNodeSelector] Skip remove label from csi-nfs controller node: %s", node.Name))
				continue
			}
		}

		log.Info(fmt.Sprintf("[reconcileNodeSelector] Check if node %s has pods with NFS volume.", node.Name))

		nodePodsWithNFSVolume, ok := podsMapWithNFSVolume[node.Name]
		if ok && len(nodePodsWithNFSVolume) > 0 {
			nodePodNamesWithNFSVolume := []string{}
			for _, pod := range nodePodsWithNFSVolume {
				nodePodNamesWithNFSVolume = append(nodePodNamesWithNFSVolume, fmt.Sprintf("%s/%s", pod.Namespace, pod.Name))
			}
			log.Warning(fmt.Sprintf("[reconcileNodeSelector] Found %d pods with NFS volume for node: %s. Skip remove label.", len(nodePodsWithNFSVolume), node.Name))
			log.Info(fmt.Sprintf("[reconcileNodeSelector] Pods with NFS volume on node %s: %v", node.Name, nodePodNamesWithNFSVolume))
			log.Trace(fmt.Sprintf("[reconcileNodeSelector] Pods with NFS volume on node %s: %+v", node.Name, nodePodsWithNFSVolume))
			continue
		}

		err := RemoveLabelsFromNode(ctx, cl, log, node, nfsNodeLabels)
		if err != nil {
			err = fmt.Errorf("[reconcileNodeSelector] Failed remove labels %+v from node: %s: %w", nfsNodeLabels, node.Name, err)
			return err
		}
	}

	log.Info("[reconcileNodeSelector] Successfully reconciled NFS node selectors.")

	return nil
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

func GetNodesBySelectorList(ctx context.Context, cl client.Client, log logger.Logger, nodeSelectorList []*metav1.LabelSelector) (*corev1.NodeList, error) {
	allSelectedNodes := &corev1.NodeList{}
	allSelectedNodesMap := map[string]corev1.Node{}

	for _, nodeSelector := range nodeSelectorList {
		log.Trace(fmt.Sprintf("[GetNodesBySelectorList] Process node selector: %+v", nodeSelector))
		selector, err := metav1.LabelSelectorAsSelector(nodeSelector)
		if err != nil {
			err = fmt.Errorf("[GetNodesBySelectorList] Failed convert selector %+v to labels.Selector: %w", nodeSelector, err)
			return nil, err
		}
		log.Trace(fmt.Sprintf("[GetNodesBySelectorList] Successfully convert selector %+v to labels.Selector: %+v", nodeSelector, selector))

		selectedNodes := &corev1.NodeList{}
		err = cl.List(ctx, selectedNodes, &client.ListOptions{LabelSelector: selector})
		if err != nil {
			err = fmt.Errorf("[GetNodesBySelectorList] Failed get nodes from Kubernetes by labels.Selector: %+v: %w", nodeSelector, err)
			return nil, err
		}

		log.Debug(fmt.Sprintf("[GetNodesBySelectorList] Found %d nodes: by selector: %+v.", len(selectedNodes.Items), nodeSelector))
		log.Trace(fmt.Sprintf("[GetNodesBySelectorList] Nodes: %+v", selectedNodes.Items))

		for _, node := range selectedNodes.Items {
			log.Debug(fmt.Sprintf("[GetNodesBySelectorList] Process node: %s", node.Name))
			if _, ok := allSelectedNodesMap[node.Name]; !ok {
				log.Debug(fmt.Sprintf("[GetNodesBySelectorList] Add node %s to allSelectedNodes.", node.Name))
				allSelectedNodesMap[node.Name] = node
				allSelectedNodes.Items = append(allSelectedNodes.Items, node)
			}
		}
	}

	return allSelectedNodes, nil
}

func GetNodesBySelector(ctx context.Context, cl client.Client, nodeSelector map[string]string) (*corev1.NodeList, error) {
	selectedK8sNodes := &corev1.NodeList{}
	err := cl.List(ctx, selectedK8sNodes, client.MatchingLabels(nodeSelector))
	return selectedK8sNodes, err
}

func AddLabelsToNode(ctx context.Context, cl client.Client, log logger.Logger, node corev1.Node, labels map[string]string) error {
	log.Debug(fmt.Sprintf("[AddLabelsToNode] node labels: %+v", node.Labels))
	_, added := AddLabelsIfNeeded(log, node.Labels, labels)
	if !added {
		log.Debug(fmt.Sprintf("[AddLabelsToNode] Node %s already has labels %v. Skip add labels to node.", node.Name, labels))
		return nil
	}
	log.Info(fmt.Sprintf("[AddLabelsToNode] Node %s has not labels %v. Add labels to node.", node.Name, labels))
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latestNode := &corev1.Node{}
		if err := cl.Get(ctx, types.NamespacedName{Name: node.Name}, latestNode); err != nil {
			return err
		}

		latestNode.Labels, added = AddLabelsIfNeeded(log, latestNode.Labels, labels)
		if added {
			return cl.Update(ctx, latestNode)
		}

		return nil
	})
}

func AddLabelsIfNeeded(log logger.Logger, originalLabels, labelsToAdd map[string]string) (map[string]string, bool) {
	added := false

	if originalLabels == nil {
		added = true
		return labelsToAdd, added
	}

	for key, value := range labelsToAdd {
		log.Debug(fmt.Sprintf("[AddLabelsIfNeeded] Check label %s=%s", key, value))
		originalValue, ok := originalLabels[key]
		if !ok || originalValue != value {
			log.Debug(fmt.Sprintf("[AddLabelsIfNeeded] Add label %s=%s", key, value))
			originalLabels[key] = value
			added = true
		}
	}

	return originalLabels, added
}

func DiffNodeLists(leftList, rightList *corev1.NodeList) corev1.NodeList {
	var diff corev1.NodeList

	for _, leftNode := range leftList.Items {
		if !ContainsNode(rightList, leftNode.Name) {
			diff.Items = append(diff.Items, leftNode)
		}
	}
	return diff
}

func ContainsNode(nodeList *corev1.NodeList, nodeName string) bool {
	for _, item := range nodeList.Items {
		if item.Name == nodeName {
			return true
		}
	}
	return false
}

// TODO: Move to sds-local-volume
// func FilterVolumeAttachments(log logger.Logger, volumeAttachments *storagev1.VolumeAttachmentList, nodesToRemove corev1.NodeList, provisioner string) map[string][]storagev1.VolumeAttachment {
// 	// filteredVolumeAttachments := map[string]storagev1.VolumeAttachmentList{}
// 	filteredVolumeAttachments := map[string][]storagev1.VolumeAttachment{}

// 	for _, volumeAttachment := range volumeAttachments.Items {
// 		log.Debug(fmt.Sprintf("[FilterVolumeAttachments] Check volume attachment: %+v", volumeAttachment))
// 		if volumeAttachment.Spec.Source.PersistentVolumeName == nil {
// 			log.Debug(fmt.Sprintf("[FilterVolumeAttachments] Skip volume attachment %s. PersistentVolumeName is nil.", volumeAttachment.Name))
// 			continue
// 		}
// 		if !volumeAttachment.Status.Attached {
// 			log.Debug(fmt.Sprintf("[FilterVolumeAttachments] Skip volume attachment %s. Not attached.", volumeAttachment.Name))
// 			continue
// 		}

// 		if volumeAttachment.Spec.Attacher != provisioner {
// 			log.Debug(fmt.Sprintf("[FilterVolumeAttachments] Skip volume attachment %s. Attacher %s != %s.", volumeAttachment.Name, volumeAttachment.Spec.Attacher, provisioner))
// 			continue
// 		}

// 		if volumeAttachment.Spec.NodeName == "" {
// 			log.Debug(fmt.Sprintf("[FilterVolumeAttachments] Skip volume attachment %s. NodeName is nil.", volumeAttachment.Name))
// 			continue
// 		}

// 		if !ContainsNode(&nodesToRemove, corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: volumeAttachment.Spec.NodeName}}) {
// 			log.Debug(fmt.Sprintf("[FilterVolumeAttachments] Skip volume attachment %s. Node %s not in nodesToRemove.", volumeAttachment.Name, volumeAttachment.Spec.NodeName))
// 			continue
// 		}

// 		nodeName := volumeAttachment.Spec.NodeName
// 		log.Debug(fmt.Sprintf("[FilterVolumeAttachments] Add volume attachment %s to filteredVolumeAttachments for node %s.", volumeAttachment.Name, nodeName))

// 		filteredVolumeAttachments[nodeName] = append(filteredVolumeAttachments[nodeName], volumeAttachment)

// 	}

// 	return filteredVolumeAttachments
// }

func GetPodsMapWithNFSVolume(ctx context.Context, clusterWideClient client.Reader, log logger.Logger, namespaceList *corev1.NamespaceList) (map[string][]corev1.Pod, error) {
	podsMapWithNFSVolume := map[string][]corev1.Pod{}

	for i := 0; i < len(namespaceList.Items); i++ {
		namespace := &namespaceList.Items[i]
		log.Debug(fmt.Sprintf("[GetPodsMapWithNFSVolume] Get pods for namespace %s.", namespace.Name))
		pods := &corev1.PodList{}
		err := clusterWideClient.List(ctx, pods, client.InNamespace(namespace.Name))

		if err != nil {
			err = fmt.Errorf("[GetPodsMapWithNFSVolume] Failed get pods in namespace %s: %w", namespace.Name, err)
			return nil, err
		}
		log.Debug(fmt.Sprintf("[GetPodsMapWithNFSVolume] Found %d pods in namespace %s.", len(pods.Items), namespace.Name))

		for i := 0; i < len(pods.Items); i++ {
			pod := &pods.Items[i]

			log.Debug(fmt.Sprintf("[GetPodsMapWithNFSVolume] Check pod %s/%s.", pod.Namespace, pod.Name))
			log.Trace(fmt.Sprintf("[GetPodsMapWithNFSVolume] Pod volumes: %+v", pod.Spec.Volumes))

			for j := 0; j < len(pod.Spec.Volumes); j++ {
				volume := &pod.Spec.Volumes[j]
				log.Debug(fmt.Sprintf("[GetPodsMapWithNFSVolume] Check volume %s for pod %s/%s.", volume.Name, pod.Namespace, pod.Name))
				if volume.PersistentVolumeClaim == nil {
					continue
				}
				log.Debug(fmt.Sprintf("[GetPodsMapWithNFSVolume] Check pvc %s for pod %s/%s.", volume.PersistentVolumeClaim.ClaimName, pod.Namespace, pod.Name))
				pvc := &corev1.PersistentVolumeClaim{}
				err := clusterWideClient.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: volume.PersistentVolumeClaim.ClaimName}, pvc)
				if err != nil {
					err = fmt.Errorf("[GetPodsMapWithNFSVolume] Failed get pvc %s/%s for pod %s/%s: %w", pod.Namespace, volume.PersistentVolumeClaim.ClaimName, pod.Namespace, pod.Name, err)
					return nil, err
				}

				if pvc.Annotations["volume.kubernetes.io/storage-provisioner"] == NFSStorageClassProvisioner {
					log.Debug(fmt.Sprintf("[GetPodsMapWithNFSVolume] pod %s/%s has volume with NFS storage provisioner. Append pod to podsMapWithNFSVolume on node %s.", pod.Namespace, pod.Name, pod.Spec.NodeName))
					podsMapWithNFSVolume[pod.Spec.NodeName] = append(podsMapWithNFSVolume[pod.Spec.NodeName], *pod)
					break
				}
			}
		}
	}

	return podsMapWithNFSVolume, nil
}

func RemoveLabelsFromNode(ctx context.Context, cl client.Client, log logger.Logger, node corev1.Node, labels map[string]string) error {
	log.Debug(fmt.Sprintf("[RemoveLabelFromNode] node labels: %+v", node.Labels))
	_, removed := RemoveLabelsIfNeeded(log, node.Labels, labels)
	if !removed {
		return nil
	}
	log.Info(fmt.Sprintf("[RemoveLabelFromNode] Remove labels %v from node: %s", labels, node.Name))
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latestNode := &corev1.Node{}
		if err := cl.Get(ctx, types.NamespacedName{Name: node.Name}, latestNode); err != nil {
			return err
		}

		latestNode.Labels, removed = RemoveLabelsIfNeeded(log, latestNode.Labels, labels)
		if removed {
			return cl.Update(ctx, latestNode)
		}

		return nil
	})
}

func RemoveLabelsIfNeeded(log logger.Logger, originalLabels, labelsToRemove map[string]string) (map[string]string, bool) {
	removed := false

	if originalLabels == nil {
		return originalLabels, removed
	}

	for key := range labelsToRemove {
		log.Debug(fmt.Sprintf("[RemoveLabelsfNeeded] Check label %s.", key))
		if _, ok := originalLabels[key]; ok {
			log.Debug(fmt.Sprintf("[RemoveLabelsfNeeded] Remove label %s", key))
			delete(originalLabels, key)
			removed = true
		}
	}

	return originalLabels, removed
}

func GetCCSIControllerNodeName(ctx context.Context, cl client.Client, namespace, leaseName string, csiControllerLabel map[string]string) (string, error) {
	csiControllerPodList := &corev1.PodList{}
	err := cl.List(ctx, csiControllerPodList, client.InNamespace(namespace), client.MatchingLabels(csiControllerLabel))
	if err != nil {
		err = fmt.Errorf("[GetCCSIControllerNodeName] Failed get csi controller pod: %w", err)
		return "", err
	}

	if len(csiControllerPodList.Items) == 0 {
		return "", nil
	}

	lease := &coordinationv1.Lease{}
	err = cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: leaseName}, lease)
	if err != nil {
		err = fmt.Errorf("[GetCCSIControllerNodeName] Failed get lease: %s/%s: %w", namespace, leaseName, err)
		return "", err
	}

	if lease.Spec.HolderIdentity == nil {
		return "", fmt.Errorf("[GetCCSIControllerNodeName] HolderIdentity is nil in lease: %s", leaseName)
	}

	return *lease.Spec.HolderIdentity, nil
}

func GetPendingVolumeSnapshots(ctx context.Context, clusterWideClient client.Reader, log logger.Logger, provisioner string, namespaceList *corev1.NamespaceList) ([]snapshotv1.VolumeSnapshot, error) {
	var pendingSnapshots []snapshotv1.VolumeSnapshot

	for _, namespace := range namespaceList.Items {
		log.Debug(fmt.Sprintf("[GetPendingVolumeSnapshots] Get volumesnapshots for namespace %s.", namespace.Name))
		volumeSnapshots := &snapshotv1.VolumeSnapshotList{}
		err := clusterWideClient.List(ctx, volumeSnapshots, client.InNamespace(namespace.Name))
		if err != nil {
			err = fmt.Errorf("[GetPendingVolumeSnapshots] Failed get volumesnapshots in namespace %s: %w", namespace.Name, err)
			return nil, err
		}

		log.Debug(fmt.Sprintf("[GetPendingVolumeSnapshots] Found %d volumesnapshots in namespace %s.", len(volumeSnapshots.Items), namespace.Name))

		for _, snapshot := range volumeSnapshots.Items {
			if snapshot.Status != nil && snapshot.Status.ReadyToUse != nil && *snapshot.Status.ReadyToUse {
				continue
			}

			log.Info(fmt.Sprintf("[GetPendingVolumeSnapshots] Found pending volumesnapshot %s/%s.", snapshot.Namespace, snapshot.Name))
			log.Debug(fmt.Sprintf("[GetPendingVolumeSnapshots] Volumesnapshot: %+v", snapshot))

			if snapshot.Spec.Source.PersistentVolumeClaimName == nil {
				continue
			}

			pvc := &corev1.PersistentVolumeClaim{}
			err = clusterWideClient.Get(ctx, client.ObjectKey{Namespace: snapshot.Namespace, Name: *snapshot.Spec.Source.PersistentVolumeClaimName}, pvc)
			if err != nil {
				err = fmt.Errorf("[GetPendingVolumeSnapshots] Failed get pvc %s/%s for snapshot %s/%s: %v", snapshot.Namespace, *snapshot.Spec.Source.PersistentVolumeClaimName, snapshot.Namespace, snapshot.Name, err)
				return nil, err
			}
			log.Info(fmt.Sprintf("[GetPendingVolumeSnapshots] Found PVC %s/%s for volumesnapshot %s/%s.", pvc.Namespace, pvc.Name, snapshot.Namespace, snapshot.Name))
			log.Debug(fmt.Sprintf("[GetPendingVolumeSnapshots] PVC: %+v", pvc))

			if pvc.Annotations["volume.kubernetes.io/storage-provisioner"] == provisioner {
				log.Debug(fmt.Sprintf("[GetPendingVolumeSnapshots] PVC %s/%s has NFS storage provisioner. Append volumesnapshot %s/%s to pendingSnapshots.", pvc.Namespace, pvc.Name, snapshot.Namespace, snapshot.Name))
				pendingSnapshots = append(pendingSnapshots, snapshot)
			}
		}
	}

	return pendingSnapshots, nil
}

func GetPendingPersistentVolumeClaims(ctx context.Context, clusterWideClient client.Reader, log logger.Logger, provisioner string, namespaceList *corev1.NamespaceList) ([]corev1.PersistentVolumeClaim, error) {
	var pendingPVCs []corev1.PersistentVolumeClaim

	for _, namespace := range namespaceList.Items {
		persistentVolumeClaimList := &corev1.PersistentVolumeClaimList{}
		err := clusterWideClient.List(ctx, persistentVolumeClaimList, client.InNamespace(namespace.Name))
		if err != nil {
			err = fmt.Errorf("[GetPendingPersistentVolumeClaims] Failed get persistent volume claims in namespace %s: %w", namespace.Name, err)
			return nil, err
		}

		log.Debug(fmt.Sprintf("[GetPendingPersistentVolumeClaims] Found %d persistent volume claims in namespace %s.", len(persistentVolumeClaimList.Items), namespace.Name))

		for _, pvc := range persistentVolumeClaimList.Items {
			if pvc.Status.Phase == corev1.ClaimPending {
				log.Info(fmt.Sprintf("[GetPendingPersistentVolumeClaims] Found pending PVC %s/%s.", pvc.Namespace, pvc.Name))
				log.Debug(fmt.Sprintf("[GetPendingPersistentVolumeClaims] PVC: %+v", pvc))

				if pvc.Annotations["volume.kubernetes.io/storage-provisioner"] == provisioner {
					log.Info(fmt.Sprintf("[GetPendingPersistentVolumeClaims] PVC %s/%s has NFS storage provisioner. Append PVC %s/%s to pendingPVCs.", pvc.Namespace, pvc.Name, pvc.Namespace, pvc.Name))
					pendingPVCs = append(pendingPVCs, pvc)
				}
			}
		}
	}

	return pendingPVCs, nil
}

func IsCSIControllerRemovable(ctx context.Context, clusterWideClient client.Reader, log logger.Logger, provisioner string, namespaceList *corev1.NamespaceList) (bool, error) {
	pendingSnapshots, err := GetPendingVolumeSnapshots(ctx, clusterWideClient, log, provisioner, namespaceList)
	if err != nil {
		err = fmt.Errorf("[CheckIfCanRemoveControllerNode] Failed get pending volumesnapshots: %w", err)
		return false, err
	}

	if len(pendingSnapshots) > 0 {
		pendingSnapshotNames := []string{}
		for _, snapshot := range pendingSnapshots {
			pendingSnapshotNames = append(pendingSnapshotNames, fmt.Sprintf("%s/%s", snapshot.Namespace, snapshot.Name))
		}
		log.Warning(fmt.Sprintf("[CheckIfCanRemoveControllerNode] Found %d pending volumesnapshots: %v", len(pendingSnapshots), pendingSnapshotNames))
		log.Debug(fmt.Sprintf("[CheckIfCanRemoveControllerNode] Pending volumesnapshots: %+v", pendingSnapshots))
		return false, nil
	}

	pendingPVCs, err := GetPendingPersistentVolumeClaims(ctx, clusterWideClient, log, provisioner, namespaceList)
	if err != nil {
		err = fmt.Errorf("[CheckIfCanRemoveControllerNode] Failed get pending persistent volume claims: %w", err)
		return false, err
	}

	if len(pendingPVCs) > 0 {
		pendingPVCNames := []string{}
		for _, pvc := range pendingPVCs {
			pendingPVCNames = append(pendingPVCNames, fmt.Sprintf("%s/%s", pvc.Namespace, pvc.Name))
		}
		log.Warning(fmt.Sprintf("[CheckIfCanRemoveControllerNode] Found %d pending persistent volume claims: %v", len(pendingPVCs), pendingPVCNames))
		log.Debug(fmt.Sprintf("[CheckIfCanRemoveControllerNode] Pending persistent volume claims: %+v", pendingPVCs))
		return false, nil
	}

	return true, nil
}

func ReconcileModulePods(
	ctx context.Context,
	cl client.Client,
	clusterWideClient client.Reader,
	log logger.Logger,
	moduleNamespace string,
	nodeSelector map[string]string,
	modulePodSelectorList []map[string]string,
) error {
	modulePods := &corev1.PodList{}
	err := cl.List(ctx, modulePods, client.InNamespace(moduleNamespace))
	if err != nil {
		err = fmt.Errorf("[ReconcileModulePods] Failed get module pods: %w", err)
		return err
	}

	csiNFSNodes, err := GetNodesBySelector(ctx, cl, nodeSelector)
	if err != nil {
		err = fmt.Errorf("[ReconcileModulePods] Failed get nodes from Kubernetes by selector: %+v: %w", nodeSelector, err)
		return err
	}
	log.Trace(fmt.Sprintf("[ReconcileModulePods] csi-nfs nodes: %+v", csiNFSNodes.Items))

	csiNFSNodeNamesMap := map[string]struct{}{}
	csiNFSNodeNames := []string{}
	for _, node := range csiNFSNodes.Items {
		csiNFSNodeNamesMap[node.Name] = struct{}{}
		csiNFSNodeNames = append(csiNFSNodeNames, node.Name)
	}
	log.Info(fmt.Sprintf("[ReconcileModulePods] csi-nfs node names: %v", csiNFSNodeNames))
	log.Debug(fmt.Sprintf("[ReconcileModulePods] csi-nfs node names map: %+v", csiNFSNodeNamesMap))

	csiControllerPods := []*corev1.Pod{}
	for i := 0; i < len(modulePods.Items); i++ {
		pod := &modulePods.Items[i]
		podMatchSelector := false
		log.Debug(fmt.Sprintf("[ReconcileModulePods] Reconcile pod %s/%s. Pod assigned to node: %s. And has labels: %+v", pod.Namespace, pod.Name, pod.Spec.NodeName, pod.Labels))
		log.Trace(fmt.Sprintf("[ReconcileModulePods] Pod: %+v", pod))
		if pod.Spec.NodeName == "" {
			log.Debug(fmt.Sprintf("[ReconcileModulePods] Skip pod %s/%s. NodeName is empty.", pod.Namespace, pod.Name))
			continue
		}

		for _, selector := range modulePodSelectorList {
			log.Debug(fmt.Sprintf("[ReconcileModulePods] Check pod %s/%s with labels %+v match selector %+v.", pod.Namespace, pod.Name, pod.Labels, selector))
			if isPodMatchLabels(pod, selector) {
				log.Debug(fmt.Sprintf("[ReconcileModulePods] Pod %s/%s match selector %+v.", pod.Namespace, pod.Name, selector))
				podMatchSelector = true
				break
			}
		}

		if !podMatchSelector {
			log.Debug(fmt.Sprintf("[ReconcileModulePods] Skip pod %s/%s. Pod not match any selector from list: %v.", pod.Namespace, pod.Name, modulePodSelectorList))
			continue
		}

		if _, ok := csiNFSNodeNamesMap[pod.Spec.NodeName]; ok {
			log.Debug(fmt.Sprintf("[ReconcileModulePods] Pod %s/%s assigned to node %s that in csi-nfs nodes: %v.", pod.Namespace, pod.Name, pod.Spec.NodeName, csiNFSNodeNames))
			continue
		}

		if isPodMatchLabels(pod, CSIControllerLabel) {
			log.Debug(fmt.Sprintf("[ReconcileModulePods] Add pod %s/%s to csi-controller pods.", pod.Namespace, pod.Name))
			csiControllerPods = append(csiControllerPods, pod)
		} else {
			log.Info(fmt.Sprintf("[ReconcileModulePods] Remove pod %s/%s because it is assigned to node %s that not in csi-nfs nodes: %v.", pod.Namespace, pod.Name, pod.Spec.NodeName, csiNFSNodeNames))
			if err := cl.Delete(ctx, pod); err != nil {
				err = fmt.Errorf("[ReconcileModulePods] Failed delete pod %s/%s: %w", pod.Namespace, pod.Name, err)
				return err
			}
		}
	}

	if len(csiControllerPods) == 0 {
		log.Debug("[ReconcileModulePods] Successfully reconciled module pods.")
		return nil
	}

	csiControllerPodNames := []string{}
	for _, pod := range csiControllerPods {
		csiControllerPodNames = append(csiControllerPodNames, fmt.Sprintf("%s/%s on node %s", pod.Namespace, pod.Name, pod.Spec.NodeName))
	}
	log.Warning(fmt.Sprintf("[ReconcileModulePods] Found %d csi-controller pods that assigned to nodes not in csi-nfs nodes: %v.", len(csiControllerPods), csiNFSNodeNames))
	log.Info(fmt.Sprintf("[ReconcileModulePods] csi-controller pods: %v", csiControllerPodNames))

	namespaceList := &corev1.NamespaceList{}
	err = cl.List(ctx, namespaceList)
	if err != nil {
		err = fmt.Errorf("[ReconcileModulePods] Failed get namespaces: %w", err)
		return err
	}
	log.Debug(fmt.Sprintf("[ReconcileModulePods] Found %d namespaces.", len(namespaceList.Items)))

	csiControllerRemovable, err := IsCSIControllerRemovable(ctx, clusterWideClient, log, NFSStorageClassProvisioner, namespaceList)
	if err != nil {
		err = fmt.Errorf("[ReconcileModulePods] Failed check if can remove csi-nfs controller node: %w", err)
		return err
	}
	if csiControllerRemovable {
		log.Warning("[ReconcileModulePods] Found csi-controller pods that assigned to nodes not in csi-nfs nodes. Remove csi-nfs controller pods.")
		for _, pod := range csiControllerPods {
			log.Info(fmt.Sprintf("[ReconcileModulePods] Remove csi-controller pod %s/%s.", pod.Namespace, pod.Name))
			err := cl.Delete(ctx, pod)
			if err != nil {
				err = fmt.Errorf("[ReconcileModulePods] Failed remove csi-nfs controller pod %s/%s: %w", pod.Namespace, pod.Name, err)
				return err
			}
		}
	}

	log.Debug("[ReconcileModulePods] Successfully reconciled module pods.")

	return nil
}

func isPodMatchLabels(pod *corev1.Pod, labelsMap map[string]string) bool {
	selector := labels.SelectorFromSet(labelsMap)
	return selector.Matches(labels.Set(pod.Labels))
}
