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

package controller

import (
	"context"
	"d8-controller/pkg/config"
	"d8-controller/pkg/logger"
	"fmt"
	"time"

	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"gopkg.in/yaml.v3"
	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	NodeSelectorReconcilerName = "nfs-node-selector-reconciler"
	NFSNodeLabelKey            = "storage.deckhouse.io/csi-nfs-node"
)

var (
	nfsNodeLabels                      = map[string]string{NFSNodeLabelKey: ""}
	nfsNodeSelector                    = map[string]string{NFSNodeLabelKey: ""}
	csiNFSExternalSnapshotterLeaseName = "external-snapshotter-leader-nfs-csi-k8s-io"
)

func RunNodeSelectorReconciler(
	mgr manager.Manager,
	cfg config.Options,
	log logger.Logger,
) (controller.Controller, error) {
	cl := mgr.GetClient()

	clusterWideClient := mgr.GetAPIReader()

	c, err := controller.New(NodeSelectorReconcilerName, mgr, controller.Options{
		Reconciler: reconcile.Func(func(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
			if request.Name == cfg.ConfigSecretName {
				log.Info(fmt.Sprintf("Start reconcile of NFS node selectors. Get config secret: %s/%s", request.Namespace, request.Name))
				err := reconcileNodeSelector(ctx, cl, clusterWideClient, log, request.Namespace, request.Name)
				if err != nil {
					log.Error(err, "Failed reconcile of NFS node selectors.")
				} else {
					log.Info("END reconcile of NFS node selectors.")
				}

				return reconcile.Result{
					RequeueAfter: time.Duration(cfg.RequeueNodeSelectorInterval) * time.Second,
				}, nil
			}

			return reconcile.Result{}, nil
		}),
	})

	if err != nil {
		return nil, err
	}

	err = c.Watch(source.Kind(mgr.GetCache(), &corev1.Secret{}, &handler.TypedEnqueueRequestForObject[*corev1.Secret]{}))

	return c, err
}

func reconcileNodeSelector(
	ctx context.Context,
	cl client.Client,
	clusterWideClient client.Reader,
	log logger.Logger,
	namespace, configSecretName string,
) error {
	configSecret := &corev1.Secret{}
	err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: configSecretName}, configSecret)
	if err != nil {
		log.Error(err, "[reconcileNodeSelector] Failed get secret:"+configSecretName+"/"+namespace)
		return err
	}

	configNodeSelector, err := GetNodeSelectorFromConfig(*configSecret)
	if err != nil {
		log.Error(err, "[reconcileNodeSelector] Failed get node selector from secret:"+namespace+"/"+configSecretName)
		return err
	}

	selectedKubernetesNodes, err := GetKubernetesNodesBySelector(ctx, cl, configNodeSelector)
	if err != nil {
		log.Error(err, "[reconcileNodeSelector] Failed get nodes from Kubernetes by selector:"+fmt.Sprint(configNodeSelector))
		return err
	}

	if len(selectedKubernetesNodes.Items) != 0 {
		log.Info(fmt.Sprintf("[reconcileNodeSelector] Found %d nodes by selector: %v.", len(selectedKubernetesNodes.Items), configNodeSelector))
		log.Trace(fmt.Sprintf("[reconcileNodeSelector] Nodes: %+v", selectedKubernetesNodes.Items))

		for _, node := range selectedKubernetesNodes.Items {
			log.Info(fmt.Sprintf("[reconcileNodeSelector] Process label for node: %s", node.Name))
			err := AddLabelToNodeIfNeeded(ctx, cl, log, node, nfsNodeLabels)
			if err != nil {
				log.Error(err, fmt.Sprintf("[reconcileNodeSelector] Failed add labels %+v to node: %s", nfsNodeLabels, node.Name))
				return err
			}
		}
	}

	nodesWithCSI, err := GetKubernetesNodesBySelector(ctx, cl, nfsNodeSelector)
	if err != nil {
		log.Error(err, "[reconcileNodeSelector] Failed get nodes from Kubernetes by selector:"+fmt.Sprint(nfsNodeSelector))
		return err
	}

	nodesToRemove := DiffNodeLists(nodesWithCSI, selectedKubernetesNodes)
	nodesToRemoveCount := len(nodesToRemove.Items)
	if nodesToRemoveCount > 0 {
		nodeNamesToRemove := []string{}
		for _, node := range nodesToRemove.Items {
			nodeNamesToRemove = append(nodeNamesToRemove, node.Name)
		}
		log.Warning(fmt.Sprintf("[reconcileNodeSelector] Found %d nodes that not in selected nodes by user defined selector %v. Remove csi-nfs node label %v from them", nodesToRemoveCount, configNodeSelector, nfsNodeLabels))
		log.Info(fmt.Sprintf("[reconcileNodeSelector] Nodes to remove: %v", nodeNamesToRemove))
		log.Trace(fmt.Sprintf("[reconcileNodeSelector] Nodes: %+v", nodesToRemove.Items))

		controllerNodeName, err := GetCCSIControllerNodeName(ctx, cl, log, namespace, csiNFSExternalSnapshotterLeaseName)
		if err != nil {
			log.Error(err, "[reconcileNodeSelector] Failed get csi-nfs controller node name.")
			return err
		}

		namespaceList := &corev1.NamespaceList{}
		err = cl.List(ctx, namespaceList)
		if err != nil {
			log.Error(err, "[reconcileNodeSelector] Failed get namespaces.")
			return err
		}
		log.Debug(fmt.Sprintf("[reconcileNodeSelector] Found %d namespaces.", len(namespaceList.Items)))

		podsMapWithNFSVolume, err := GetPodsMapWithNFSVolume(ctx, clusterWideClient, log, namespaceList)
		if err != nil {
			log.Error(err, "[reconcileNodeSelector] Failed get pods with NFS volume.")
			return err
		}
		log.Debug(fmt.Sprintf("[reconcileNodeSelector] Pods with NFS volume: %+v", podsMapWithNFSVolume))

		for _, node := range nodesToRemove.Items {
			log.Info(fmt.Sprintf("[reconcileNodeSelector] Process remove label for node: %s", node.Name))

			if node.Name == controllerNodeName {
				log.Warning(fmt.Sprintf("[reconcileNodeSelector] Node %s is csi-nfs controller node! Check volumesnapshots and persistentvolumeclaims before remove labels.", node.Name))
				pendingVolumeSnapshots, err := GetPendingVolumeSnapshots(ctx, clusterWideClient, log, NFSStorageClassProvisioner, namespaceList)
				if err != nil {
					log.Error(err, "[reconcileNodeSelector] Failed check pending volumesnapshots.")
					return err
				}
				if len(pendingVolumeSnapshots) > 0 {
					log.Warning(fmt.Sprintf("[reconcileNodeSelector] Found %d pending volumesnapshots with NFS storage provisioner %s. Skip remove label.", len(pendingVolumeSnapshots), NFSStorageClassProvisioner))
					log.Debug(fmt.Sprintf("[reconcileNodeSelector] Pending volumesnapshots: %+v", pendingVolumeSnapshots))
					nodesToRemoveCount--
					continue
				}

				pendingPersistentVolumeClaims, err := GetPendingPersistentVolumeClaims(ctx, clusterWideClient, log, NFSStorageClassProvisioner, namespaceList)
				if err != nil {
					log.Error(err, "[reconcileNodeSelector] Failed check pending persistentvolumeclaims.")
					return err
				}

				if len(pendingPersistentVolumeClaims) > 0 {
					log.Warning(fmt.Sprintf("[reconcileNodeSelector] Found %d pending persistentvolumeclaims with NFS storage provisioner %s. Skip remove label.", len(pendingPersistentVolumeClaims), NFSStorageClassProvisioner))
					log.Debug(fmt.Sprintf("[reconcileNodeSelector] Pending persistentvolumeclaims: %+v", pendingPersistentVolumeClaims))
					nodesToRemoveCount--
					continue
				}
			}

			log.Info(fmt.Sprintf("[reconcileNodeSelector] Check volume attachments for node: %s", node.Name))

			nodePodsWithNFSVolume, ok := podsMapWithNFSVolume[node.Name]
			if ok && len(nodePodsWithNFSVolume) > 0 {
				nodePodNamesWithNFSVolume := []string{}
				for _, pod := range nodePodsWithNFSVolume {
					nodePodNamesWithNFSVolume = append(nodePodNamesWithNFSVolume, fmt.Sprintf("%s/%s", pod.Namespace, pod.Name))
				}
				log.Warning(fmt.Sprintf("[reconcileNodeSelector] Found %d pods with NFS volume for node: %s. Skip remove label.", len(nodePodsWithNFSVolume), node.Name))
				log.Info(fmt.Sprintf("[reconcileNodeSelector] Pods with NFS volume on node %s: %v", node.Name, nodePodNamesWithNFSVolume))
				log.Trace(fmt.Sprintf("[reconcileNodeSelector] Pods with NFS volume on node %s: %+v", node.Name, nodePodsWithNFSVolume))
				nodesToRemoveCount--
				continue
			}

			err := RemoveLabelFromNodeIfNeeded(ctx, cl, log, node, nfsNodeLabels)
			if err != nil {
				log.Error(err, fmt.Sprintf("[reconcileNodeSelector] Failed remove labels %+v from node: %s", nfsNodeLabels, node.Name))
				return err
			}
		}

		log.Info(fmt.Sprintf("[reconcileNodeSelector] Successfully removed labels %v from %d nodes.", nfsNodeLabels, nodesToRemoveCount))
	}

	log.Info("[reconcileNodeSelector] Successfully reconciled NFS node selectors.")

	return nil
}

func GetNodeSelectorFromConfig(secret corev1.Secret) (map[string]string, error) {
	var secretConfig config.CSINFSControllerConfig
	err := yaml.Unmarshal(secret.Data["config"], &secretConfig)
	if err != nil {
		return nil, err
	}
	nodeSelector := secretConfig.NodeSelector
	return nodeSelector, err
}

func GetKubernetesNodesBySelector(ctx context.Context, cl client.Client, nodeSelector map[string]string) (*corev1.NodeList, error) {
	selectedK8sNodes := &corev1.NodeList{}
	err := cl.List(ctx, selectedK8sNodes, client.MatchingLabels(nodeSelector))
	return selectedK8sNodes, err
}

func AddLabelToNodeIfNeeded(ctx context.Context, cl client.Client, log logger.Logger, node corev1.Node, labels map[string]string) error {
	needUpdate := false
	log.Debug(fmt.Sprintf("[AddLabelToNodeIfNeeded] node labels: %+v", node.Labels))
	if node.Labels == nil {
		needUpdate = true
		node.Labels = map[string]string{}
	}

	for key, value := range labels {
		log.Debug(fmt.Sprintf("[AddLabelToNodeIfNeeded] Check label %s=%s for node: %s", key, value, node.Name))
		nodeValue, ok := node.Labels[key]
		if !ok || nodeValue != value {
			log.Info(fmt.Sprintf("[AddLabelToNodeIfNeeded] Add label %s=%s to node: %s", key, value, node.Name))
			node.Labels[key] = value
			needUpdate = true
		}
	}

	log.Info(fmt.Sprintf("[AddLabelToNodeIfNeeded] Need update node %s: %v", node.Name, needUpdate))
	if needUpdate {
		err := cl.Update(ctx, &node)
		if err != nil {
			return err
		}
	}

	return nil
}

func DiffNodeLists(leftList, rightList *corev1.NodeList) corev1.NodeList {
	var diff corev1.NodeList

	for _, leftNode := range leftList.Items {
		if !ContainsNode(rightList, leftNode) {
			diff.Items = append(diff.Items, leftNode)
		}
	}
	return diff
}

func ContainsNode(nodeList *corev1.NodeList, node corev1.Node) bool {
	for _, item := range nodeList.Items {
		if item.Name == node.Name {
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

	for _, namespace := range namespaceList.Items {
		log.Debug(fmt.Sprintf("[GetPodsMapWithNFSVolume] Get pods for namespace %s.", namespace.Name))
		pods := &corev1.PodList{}
		err := clusterWideClient.List(ctx, pods, client.InNamespace(namespace.Name))

		if err != nil {
			log.Error(err, "[GetPodsMapWithNFSVolume] Failed get pods.")
			return nil, err
		}
		log.Debug(fmt.Sprintf("[GetPodsMapWithNFSVolume] Found %d pods in namespace %s.", len(pods.Items), namespace.Name))

		for _, pod := range pods.Items {
			log.Debug(fmt.Sprintf("[GetPodsMapWithNFSVolume] Check pod %s/%s.", pod.Namespace, pod.Name))
			for _, volume := range pod.Spec.Volumes {
				log.Debug(fmt.Sprintf("[GetPodsMapWithNFSVolume] Check volume %s for pod %s/%s.", volume.Name, pod.Namespace, pod.Name))
				if volume.PersistentVolumeClaim != nil {
					log.Debug(fmt.Sprintf("[GetPodsMapWithNFSVolume] Check pvc %s for pod %s/%s.", volume.PersistentVolumeClaim.ClaimName, pod.Namespace, pod.Name))
					pvc := &corev1.PersistentVolumeClaim{}
					err := clusterWideClient.Get(ctx, client.ObjectKey{Namespace: pod.Namespace, Name: volume.PersistentVolumeClaim.ClaimName}, pvc)
					if err != nil {
						log.Error(err, fmt.Sprintf("[GetPodsMapWithNFSVolume] Failed get pvc %s/%s for pod %s/%s.", pod.Namespace, volume.PersistentVolumeClaim.ClaimName, pod.Namespace, pod.Name))
						return nil, err
					}

					if pvc.Annotations != nil && pvc.Annotations["volume.kubernetes.io/storage-provisioner"] == NFSStorageClassProvisioner {
						log.Debug(fmt.Sprintf("[GetPodsMapWithNFSVolume] pod %s/%s has volume with NFS storage provisioner. Append pod to podsMapWithNFSVolume on node %s.", pod.Namespace, pod.Name, pod.Spec.NodeName))
						podsMapWithNFSVolume[pod.Spec.NodeName] = append(podsMapWithNFSVolume[pod.Spec.NodeName], pod)
						break
					}
				}
			}
		}
	}

	return podsMapWithNFSVolume, nil
}

func RemoveLabelFromNodeIfNeeded(ctx context.Context, cl client.Client, log logger.Logger, node corev1.Node, labels map[string]string) error {
	needUpdate := false

	if node.Labels == nil {
		return nil
	}

	for key := range labels {
		if _, ok := node.Labels[key]; ok {
			log.Info(fmt.Sprintf("[RemoveLabelFromNodeIfNeeded] Remove label %s from node: %s", key, node.Name))
			delete(node.Labels, key)
			needUpdate = true
		}
	}

	if needUpdate {
		err := cl.Update(ctx, &node)
		if err != nil {
			return err
		}
	}

	return nil
}

func GetCCSIControllerNodeName(ctx context.Context, cl client.Client, log logger.Logger, namespace, leaseName string) (string, error) {
	lease := &coordinationv1.Lease{}
	err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: leaseName}, lease)
	if err != nil {
		log.Error(err, fmt.Sprintf("[GetCCSIControllerNodeName] Failed get lease: %s/%s", namespace, leaseName))
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
			log.Error(err, "[GetPendingVolumeSnapshots] Failed get volume snapshots.")
			return nil, err
		}

		log.Debug(fmt.Sprintf("[GetPendingVolumeSnapshots] Found %d volumesnapshots in namespace %s.", len(volumeSnapshots.Items), namespace.Name))

		for _, snapshot := range volumeSnapshots.Items {
			if snapshot.Status == nil || snapshot.Status.ReadyToUse == nil || !*snapshot.Status.ReadyToUse {
				log.Info(fmt.Sprintf("[GetPendingVolumeSnapshots] Found pending volumesnapshot %s/%s.", snapshot.Namespace, snapshot.Name))
				log.Debug(fmt.Sprintf("[GetPendingVolumeSnapshots] Volumesnapshot: %+v", snapshot))
				if snapshot.Spec.Source.PersistentVolumeClaimName != nil {
					pvc := &corev1.PersistentVolumeClaim{}
					err := clusterWideClient.Get(ctx, client.ObjectKey{Namespace: snapshot.Namespace, Name: *snapshot.Spec.Source.PersistentVolumeClaimName}, pvc)
					if err != nil {
						err = fmt.Errorf("[GetPendingVolumeSnapshots] Failed get pvc %s/%s for snapshot %s/%s: %v", snapshot.Namespace, *snapshot.Spec.Source.PersistentVolumeClaimName, snapshot.Namespace, snapshot.Name, err)
						return nil, err
					}
					log.Info(fmt.Sprintf("[GetPendingVolumeSnapshots] Found PVC %s/%s for volumesnapshot %s/%s.", pvc.Namespace, pvc.Name, snapshot.Namespace, snapshot.Name))
					log.Debug(fmt.Sprintf("[GetPendingVolumeSnapshots] PVC: %+v", pvc))

					if pvc.Annotations != nil && pvc.Annotations["volume.kubernetes.io/storage-provisioner"] == provisioner {
						log.Debug(fmt.Sprintf("[GetPendingVolumeSnapshots] PVC %s/%s has NFS storage provisioner. Append volumesnapshot %s/%s to pendingSnapshots.", pvc.Namespace, pvc.Name, snapshot.Namespace, snapshot.Name))
						pendingSnapshots = append(pendingSnapshots, snapshot)
					}
				}
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
			log.Error(err, "[GetPendingPersistentVolumeClaims] Failed get persistent volume claims.")
			return nil, err
		}

		log.Debug(fmt.Sprintf("[GetPendingPersistentVolumeClaims] Found %d persistent volume claims in namespace %s.", len(persistentVolumeClaimList.Items), namespace.Name))

		for _, pvc := range persistentVolumeClaimList.Items {
			if pvc.Status.Phase == corev1.ClaimPending {
				log.Info(fmt.Sprintf("[GetPendingPersistentVolumeClaims] Found pending PVC %s/%s.", pvc.Namespace, pvc.Name))
				log.Debug(fmt.Sprintf("[GetPendingPersistentVolumeClaims] PVC: %+v", pvc))

				if pvc.Annotations != nil && pvc.Annotations["volume.kubernetes.io/storage-provisioner"] == provisioner {
					log.Info(fmt.Sprintf("[GetPendingPersistentVolumeClaims] PVC %s/%s has NFS storage provisioner. Append PVC %s/%s to pendingPVCs.", pvc.Namespace, pvc.Name, pvc.Namespace, pvc.Name))
					pendingPVCs = append(pendingPVCs, pvc)
				}
			}
		}
	}

	return pendingPVCs, nil
}