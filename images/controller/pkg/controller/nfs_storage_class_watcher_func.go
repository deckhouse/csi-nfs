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
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/google/go-cmp/cmp"
	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/csi-nfs/images/controller/pkg/logger"
	commonfeature "github.com/deckhouse/csi-nfs/lib/go/common/pkg/feature"
)

func reconcileStorageClassCreateFunc(
	ctx context.Context,
	cl client.Client,
	log logger.Logger,
	newSC *storagev1.StorageClass,
	nsc *v1alpha1.NFSStorageClass,
) (bool, error) {
	log.Debug(fmt.Sprintf("[reconcileStorageClassCreateFunc] starts for StorageClass %q", newSC.Name))
	log.Trace(fmt.Sprintf("[reconcileStorageClassCreateFunc] storage class: %+v", newSC))

	err := cl.Create(ctx, newSC)
	if err != nil {
		err = fmt.Errorf("[reconcileStorageClassCreateFunc] unable to create a Storage Class %s: %w", newSC.Name, err)
		upError := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, err.Error())
		if upError != nil {
			upError = fmt.Errorf("[reconcileStorageClassCreateFunc] unable to update the NFSStorageClass %s: %w", nsc.Name, upError)
			err = errors.Join(err, upError)
		}
		return true, err
	}

	log.Info(fmt.Sprintf("[reconcileStorageClassCreateFunc] successfully create storage class, name: %s", newSC.Name))

	return false, nil
}

func reconcileStorageClassRecreateFunc(
	ctx context.Context,
	cl client.Client,
	log logger.Logger,
	oldSC *storagev1.StorageClass,
	newSC *storagev1.StorageClass,
	nsc *v1alpha1.NFSStorageClass,
) (bool, error) {
	log.Info(fmt.Sprintf("[reconcileStorageClassRecreateFunc] starts for NFSStorageClass %q", nsc.Name))

	err := recreateStorageClass(ctx, cl, oldSC, newSC)
	if err != nil {
		err = fmt.Errorf("[reconcileStorageClassRecreateFunc] unable to recreate a Storage Class %s: %w", newSC.Name, err)
		upError := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, err.Error())
		if upError != nil {
			upError = fmt.Errorf("[reconcileStorageClassRecreateFunc] unable to update the NFSStorageClass %s: %w", nsc.Name, upError)
			err = errors.Join(err, upError)
		}
		return true, err
	}

	log.Info(fmt.Sprintf("[reconcileStorageClassRecreateFunc] a Storage Class %s was successfully recreated", newSC.Name))

	return false, nil
}

func reconcileStorageClassUpdateFunc(
	ctx context.Context,
	cl client.Client,
	log logger.Logger,
	oldSC *storagev1.StorageClass,
	newSC *storagev1.StorageClass,
	nsc *v1alpha1.NFSStorageClass,
) (bool, error) {
	log.Info(fmt.Sprintf("[reconcileStorageClassUpdateFunc] starts for NFSStorageClass %q", nsc.Name))

	newSC.ObjectMeta.ResourceVersion = oldSC.ObjectMeta.ResourceVersion
	err := cl.Update(ctx, newSC)
	if err != nil {
		err = fmt.Errorf("[reconcileStorageClassUpdateFunc] unable to update a Storage Class %s: %w", newSC.Name, err)
		upError := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, err.Error())
		if upError != nil {
			upError = fmt.Errorf("[reconcileStorageClassUpdateFunc] unable to update the NFSStorageClass %s: %w", nsc.Name, upError)
			err = errors.Join(err, upError)
		}
		return true, err
	}

	log.Info(fmt.Sprintf("[reconcileStorageClassUpdateFunc] successfully updated a Storage Class, name: %s", newSC.Name))

	return false, nil
}

func reconcileStorageClassDeleteFunc(
	ctx context.Context,
	cl client.Client,
	log logger.Logger,
	oldSC *storagev1.StorageClass,
	nsc *v1alpha1.NFSStorageClass,
) (bool, error) {
	log.Info(fmt.Sprintf("[reconcileStorageClassDeleteFunc] starts for NFSStorageClass %q", nsc.Name))

	if oldSC == nil {
		log.Info(fmt.Sprintf("[reconcileStorageClassDeleteFunc] no storage class found for the NFSStorageClass, name: %s", nsc.Name))
		log.Debug("[reconcileStorageClassDeleteFunc] ends the reconciliation")
		return false, nil
	}

	log.Info(fmt.Sprintf("[reconcileStorageClassDeleteFunc] successfully found a storage class %s for the NFSStorageClass %s", oldSC.Name, nsc.Name))
	log.Trace(fmt.Sprintf("[reconcileStorageClassDeleteFunc] storage class: %+v", oldSC))

	if !slices.Contains(allowedProvisioners, oldSC.Provisioner) {
		log.Info(fmt.Sprintf("[reconcileStorageClassDeleteFunc] the storage class %s provisioner %s does not belong to allowed provisioners: %v. Skip deletion", oldSC.Name, oldSC.Provisioner, allowedProvisioners))
		return false, nil
	}

	err := deleteStorageClass(ctx, cl, oldSC)
	if err != nil {
		err = fmt.Errorf("[reconcileStorageClassDeleteFunc] unable to delete a storage class %s: %w", oldSC.Name, err)
		upErr := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, fmt.Sprintf("Unable to delete a storage class, err: %s", err.Error()))
		if upErr != nil {
			upErr = fmt.Errorf("[reconcileStorageClassDeleteFunc] unable to update the NFSStorageClass %s: %w", nsc.Name, upErr)
			err = errors.Join(err, upErr)
		}
		return true, err
	}
	log.Info(fmt.Sprintf("[reconcileStorageClassDeleteFunc] successfully deleted a storage class, name: %s", oldSC.Name))

	log.Debug("[reconcileStorageClassDeleteFunc] ends the reconciliation")
	return false, nil
}

func reconcileSecretCreateFunc(ctx context.Context, cl client.Client, log logger.Logger, nsc *v1alpha1.NFSStorageClass, controllerNamespace string) (bool, error) {
	log.Debug(fmt.Sprintf("[reconcileSecretCreateFunc] starts for NFSStorageClass %q", nsc.Name))

	newSecret := configureSecret(nsc, controllerNamespace)
	log.Debug(fmt.Sprintf("[reconcileSecretCreateFunc] successfully configurated secret for the NFSStorageClass, name: %s", nsc.Name))
	log.Trace(fmt.Sprintf("[reconcileSecretCreateFunc] secret: %+v", newSecret))

	err := cl.Create(ctx, newSecret)
	if err != nil {
		err = fmt.Errorf("[reconcileSecretCreateFunc] unable to create a Secret %s: %w", newSecret.Name, err)
		upError := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, err.Error())
		if upError != nil {
			upError = fmt.Errorf("[reconcileSecretCreateFunc] unable to update the NFSStorageClass %s: %w", nsc.Name, upError)
			err = errors.Join(err, upError)
		}
		return true, err
	}

	return false, nil
}

func reconcileSecretUpdateFunc(ctx context.Context, cl client.Client, log logger.Logger, secretList *corev1.SecretList, nsc *v1alpha1.NFSStorageClass, controllerNamespace string) (bool, error) {
	log.Debug(fmt.Sprintf("[reconcileSecretUpdateFunc] starts for secret %q", SecretForMountOptionsPrefix+nsc.Name))

	var oldSecret *corev1.Secret
	for _, s := range secretList.Items {
		if s.Name == SecretForMountOptionsPrefix+nsc.Name {
			oldSecret = &s
			break
		}
	}

	if oldSecret == nil {
		err := fmt.Errorf("[reconcileSecretUpdateFunc] unable to find a secret %s for the NFSStorageClass, name: %s", SecretForMountOptionsPrefix+nsc.Name, nsc.Name)
		upError := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, err.Error())
		if upError != nil {
			upError = fmt.Errorf("[reconcileSecretUpdateFunc] unable to update the NFSStorageClass %s: %w", nsc.Name, upError)
			err = errors.Join(err, upError)
		}
		return true, err
	}

	log.Debug(fmt.Sprintf("[reconcileSecretUpdateFunc] successfully found a secret %q for the NFSStorageClass, name: %q", oldSecret.Name, nsc.Name))

	newSecret := configureSecret(nsc, controllerNamespace)

	log.Trace(fmt.Sprintf("[reconcileSecretUpdateFunc] old secret: %+v", oldSecret))
	log.Trace(fmt.Sprintf("[reconcileSecretUpdateFunc] new secret: %+v", newSecret))

	err := cl.Update(ctx, newSecret)
	if err != nil {
		err = fmt.Errorf("[reconcileSecretUpdateFunc] unable to update a Secret %s: %w", newSecret.Name, err)
		upError := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, err.Error())
		if upError != nil {
			upError = fmt.Errorf("[reconcileSecretUpdateFunc] unable to update the NFSStorageClass %s: %w", nsc.Name, upError)
			err = errors.Join(err, upError)
		}
		return true, err
	}

	log.Info(fmt.Sprintf("[reconcileSecretUpdateFunc] ends the reconciliation for Secret %q", newSecret.Name))

	return false, nil
}

func reconcileSecretDeleteFunc(ctx context.Context, cl client.Client, log logger.Logger, secretList *corev1.SecretList, nsc *v1alpha1.NFSStorageClass) (bool, error) {
	log.Debug(fmt.Sprintf("[reconcileSecretDeleteFunc] tries to find a secret for the NFSStorageClass %q with name %q", nsc.Name, SecretForMountOptionsPrefix+nsc.Name))
	var secret *corev1.Secret
	for _, s := range secretList.Items {
		if s.Name == SecretForMountOptionsPrefix+nsc.Name {
			secret = &s
			break
		}
	}
	if secret == nil {
		log.Info(fmt.Sprintf("[reconcileSecretDeleteFunc] no secret found for the NFSStorageClass, name: %s", nsc.Name))
	}

	if secret != nil {
		log.Info(fmt.Sprintf("[reconcileSecretDeleteFunc] successfully found a secret for the NFSStorageClass %s", nsc.Name))
		log.Debug(fmt.Sprintf("[reconcileSecretDeleteFunc] starts removing a finalizer %s from the Secret, name: %s", NFSStorageClassControllerFinalizerName, secret.Name))
		_, err := removeFinalizerIfExists(ctx, cl, secret, NFSStorageClassControllerFinalizerName)
		if err != nil {
			err = fmt.Errorf("[reconcileSecretDeleteFunc] unable to remove a finalizer %s from the Secret %s: %w", NFSStorageClassControllerFinalizerName, secret.Name, err)
			upErr := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, fmt.Sprintf("Unable to remove a finalizer, err: %s", err.Error()))
			if upErr != nil {
				upErr = fmt.Errorf("[reconcileSecretDeleteFunc] unable to update the NFSStorageClass %s: %w", nsc.Name, upErr)
				err = errors.Join(err, upErr)
			}
			return true, err
		}

		err = cl.Delete(ctx, secret)
		if err != nil {
			err = fmt.Errorf("[reconcileSecretDeleteFunc] unable to delete a secret %s: %w", secret.Name, err)
			upErr := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, fmt.Sprintf("Unable to delete a secret, err: %s", err.Error()))
			if upErr != nil {
				upErr = fmt.Errorf("[reconcileSecretDeleteFunc] unable to update the NFSStorageClass %s: %w", nsc.Name, upErr)
				err = errors.Join(err, upErr)
			}
			return true, err
		}
	}

	log.Info(fmt.Sprintf("[reconcileSecretDeleteFunc] ends the reconciliation for Secret %q", SecretForMountOptionsPrefix+nsc.Name))

	log.Debug(fmt.Sprintf("[reconcileSecretDeleteFunc] starts removing a finalizer %s from the NFSStorageClass, name: %s", NFSStorageClassControllerFinalizerName, nsc.Name))
	removed, err := removeFinalizerIfExists(ctx, cl, nsc, NFSStorageClassControllerFinalizerName)
	if err != nil {
		err = fmt.Errorf("[reconcileSecretDeleteFunc] unable to remove a finalizer %s from the NFSStorageClass %s: %w", NFSStorageClassControllerFinalizerName, nsc.Name, err)
		upErr := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, fmt.Sprintf("Unable to remove a finalizer, err: %s", err.Error()))
		if upErr != nil {
			upErr = fmt.Errorf("[reconcileSecretDeleteFunc] unable to update the NFSStorageClass %s: %w", nsc.Name, upErr)
			err = errors.Join(err, upErr)
		}
		return true, err
	}
	log.Debug(fmt.Sprintf("[reconcileSecretDeleteFunc] the NFSStorageClass %s finalizer %s was removed: %t", nsc.Name, NFSStorageClassControllerFinalizerName, removed))

	return false, nil
}

func IdentifyReconcileFuncForStorageClass(log logger.Logger, scList *storagev1.StorageClassList, nsc *v1alpha1.NFSStorageClass, controllerNamespace string) (reconcileType string, oldSC, newSC *storagev1.StorageClass) {
	oldSC = findStorageClass(scList, nsc.Name)
	if oldSC == nil {
		log.Debug(fmt.Sprintf("[IdentifyReconcileFuncForStorageClass] no storage class found for the NFSStorageClass %s", nsc.Name))
	} else {
		log.Debug(fmt.Sprintf("[IdentifyReconcileFuncForStorageClass] finds old storage class for the NFSStorageClass %s", nsc.Name))
		log.Trace(fmt.Sprintf("[IdentifyReconcileFuncForStorageClass] old storage class: %+v", oldSC))
	}

	if shouldReconcileByDeleteFunc(nsc) {
		return DeleteReconcile, oldSC, nil
	}

	newSC = ConfigureStorageClass(oldSC, nsc, controllerNamespace)
	log.Debug(fmt.Sprintf("[IdentifyReconcileFuncForStorageClass] successfully configurated new storage class for the NFSStorageClass %s", nsc.Name))
	log.Trace(fmt.Sprintf("[IdentifyReconcileFuncForStorageClass] new storage class: %+v", newSC))

	if shouldReconcileStorageClassByCreateFunc(oldSC, nsc) {
		return CreateReconcile, nil, newSC
	}

	updateType := shouldReconcileStorageClassByRecreateOrUpdateFunc(log, oldSC, newSC, nsc)

	if updateType != "" {
		return updateType, oldSC, newSC
	}

	return "", oldSC, newSC
}

func shouldReconcileStorageClassByCreateFunc(oldSC *storagev1.StorageClass, nsc *v1alpha1.NFSStorageClass) bool {
	if nsc.DeletionTimestamp != nil {
		return false
	}

	if oldSC != nil {
		return false
	}

	return true
}

func shouldReconcileStorageClassByRecreateOrUpdateFunc(log logger.Logger, oldSC, newSC *storagev1.StorageClass, nsc *v1alpha1.NFSStorageClass) string {
	if nsc.DeletionTimestamp != nil {
		return ""
	}

	if oldSC == nil {
		return ""
	}

	needRecreate, diff := CompareStorageClasses(oldSC, newSC)
	if diff != "" {
		if needRecreate {
			log.Debug(fmt.Sprintf("[shouldReconcileStorageClassByUpdateFunc] a storage class %s should be recreated. Diff: %s", oldSC.Name, diff))
			return RecreateReconcile
		}
		log.Debug(fmt.Sprintf("[shouldReconcileStorageClassByUpdateFunc] a storage class %s should be updated. Diff: %s", oldSC.Name, diff))
		return UpdateReconcile
	}

	return ""
}

func shouldReconcileByDeleteFunc(obj metav1.Object) bool {
	return obj.GetDeletionTimestamp() != nil
}

func findStorageClass(scList *storagev1.StorageClassList, name string) *storagev1.StorageClass {
	for _, sc := range scList.Items {
		if sc.Name == name {
			return &sc
		}
	}
	return nil
}

// nolint:unparam
func removeFinalizerIfExists(ctx context.Context, cl client.Client, obj metav1.Object, finalizerName string) (bool, error) {
	removed := false
	finalizers := obj.GetFinalizers()
	for i, f := range finalizers {
		if f == finalizerName {
			finalizers = append(finalizers[:i], finalizers[i+1:]...)
			removed = true
			break
		}
	}

	if removed {
		obj.SetFinalizers(finalizers)
		err := cl.Update(ctx, obj.(client.Object))
		if err != nil {
			return false, err
		}
	}

	return removed, nil
}

func CompareStorageClasses(sc, newSC *storagev1.StorageClass) (bool, string) {
	var diffs []string
	needRecreate := false

	if sc.ReclaimPolicy != nil && newSC.ReclaimPolicy != nil && *sc.ReclaimPolicy != *newSC.ReclaimPolicy {
		diffs = append(diffs, fmt.Sprintf("ReclaimPolicy: %s -> %s", *sc.ReclaimPolicy, *newSC.ReclaimPolicy))
		needRecreate = true
	}

	if sc.AllowVolumeExpansion != nil &&
		newSC.AllowVolumeExpansion != nil &&
		*sc.AllowVolumeExpansion != *newSC.AllowVolumeExpansion {
		diffs = append(diffs,
			fmt.Sprintf("AllowVolumeExpansion: %t -> %t", *sc.AllowVolumeExpansion, *newSC.AllowVolumeExpansion))
		needRecreate = true
	}

	if sc.VolumeBindingMode != nil && newSC.VolumeBindingMode != nil && *sc.VolumeBindingMode != *newSC.VolumeBindingMode {
		diffs = append(diffs,
			fmt.Sprintf("VolumeBindingMode: %s -> %s", *sc.VolumeBindingMode, *newSC.VolumeBindingMode))
		needRecreate = true
	}

	if !cmp.Equal(sc.Parameters, newSC.Parameters) {
		diffs = append(diffs,
			fmt.Sprintf("Parameters diff: %s", cmp.Diff(sc.Parameters, newSC.Parameters)))
		needRecreate = true
	}

	if !cmp.Equal(sc.MountOptions, newSC.MountOptions) {
		diffs = append(diffs,
			fmt.Sprintf("MountOptions diff: %s", cmp.Diff(sc.MountOptions, newSC.MountOptions)))
	}

	if !cmp.Equal(sc.ObjectMeta.Labels, newSC.ObjectMeta.Labels) {
		diffs = append(diffs,
			fmt.Sprintf("Labels diff: %s", cmp.Diff(sc.ObjectMeta.Labels, newSC.ObjectMeta.Labels)))
	}

	if !cmp.Equal(sc.ObjectMeta.Annotations, newSC.ObjectMeta.Annotations) {
		diffs = append(diffs,
			fmt.Sprintf("Annotations diff: %s", cmp.Diff(sc.ObjectMeta.Annotations, newSC.ObjectMeta.Annotations)))
	}

	return needRecreate, strings.Join(diffs, ", ")
}

func addFinalizerIfNotExists(ctx context.Context, cl client.Client, obj metav1.Object, finalizerName string) (bool, error) {
	added := false
	finalizers := obj.GetFinalizers()
	if !slices.Contains(finalizers, finalizerName) {
		finalizers = append(finalizers, finalizerName)
		added = true
	}

	if added {
		obj.SetFinalizers(finalizers)
		err := cl.Update(ctx, obj.(client.Object))
		if err != nil {
			return false, err
		}
	}
	return true, nil
}

func ConfigureStorageClass(oldSC *storagev1.StorageClass, nsc *v1alpha1.NFSStorageClass, controllerNamespace string) *storagev1.StorageClass {
	reclaimPolicy := corev1.PersistentVolumeReclaimPolicy(nsc.Spec.ReclaimPolicy)
	volumeBindingMode := storagev1.VolumeBindingMode(nsc.Spec.VolumeBindingMode)
	AllowVolumeExpansion := AllowVolumeExpansionDefaultValue

	newSc := &storagev1.StorageClass{
		TypeMeta: metav1.TypeMeta{
			Kind:       StorageClassKind,
			APIVersion: StorageClassAPIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      nsc.Name,
			Namespace: nsc.Namespace,
			Annotations: map[string]string{
				NFSStorageClassVolumeSnapshotClassAnnotationKey: nsc.Name,
			},
			Labels: map[string]string{
				NFSStorageClassManagedLabelKey: NFSStorageClassManagedLabelValue,
			},
			Finalizers: []string{NFSStorageClassControllerFinalizerName},
		},
		Parameters:           GetSCParams(nsc, controllerNamespace),
		MountOptions:         GetSCMountOptions(nsc),
		Provisioner:          NFSStorageClassProvisioner,
		ReclaimPolicy:        &reclaimPolicy,
		VolumeBindingMode:    &volumeBindingMode,
		AllowVolumeExpansion: &AllowVolumeExpansion,
	}

	if oldSC != nil {
		if oldSC.Labels != nil {
			newSc.Labels = labels.Merge(oldSC.Labels, newSc.Labels)
		}
		if oldSC.Annotations != nil {
			newSc.Annotations = labels.Merge(oldSC.Annotations, newSc.Annotations)
		}
	}

	return newSc
}

func updateNFSStorageClassPhase(ctx context.Context, cl client.Client, nsc *v1alpha1.NFSStorageClass, phase, reason string) error {
	if nsc.Status == nil {
		nsc.Status = &v1alpha1.NFSStorageClassStatus{}
	}
	nsc.Status.Phase = phase
	nsc.Status.Reason = reason

	// TODO: add retry logic
	err := cl.Status().Update(ctx, nsc)
	if err != nil {
		return err
	}

	return nil
}

func recreateStorageClass(ctx context.Context, cl client.Client, oldSC, newSC *storagev1.StorageClass) error {
	// It is necessary to pass the original StorageClass to the delete operation because
	// the deletion will not succeed if the fields in the StorageClass provided to delete
	// differ from those currently in the cluster.
	err := deleteStorageClass(ctx, cl, oldSC)
	if err != nil {
		err = fmt.Errorf("[recreateStorageClass] unable to delete a storage class %s: %s", oldSC.Name, err.Error())
		return err
	}

	err = cl.Create(ctx, newSC)
	if err != nil {
		err = fmt.Errorf("[recreateStorageClass] unable to create a storage class %s: %s", newSC.Name, err.Error())
		return err
	}

	return nil
}

func deleteStorageClass(ctx context.Context, cl client.Client, sc *storagev1.StorageClass) error {
	if !slices.Contains(allowedProvisioners, sc.Provisioner) {
		return fmt.Errorf("a storage class %s with provisioner %s does not belong to allowed provisioners: %v", sc.Name, sc.Provisioner, allowedProvisioners)
	}

	_, err := removeFinalizerIfExists(ctx, cl, sc, NFSStorageClassControllerFinalizerName)
	if err != nil {
		return err
	}

	err = cl.Delete(ctx, sc)
	if err != nil {
		return err
	}

	return nil
}

func GetSCMountOptions(nsc *v1alpha1.NFSStorageClass) []string {
	mountOptions := []string{}

	if nsc.Spec.Connection.NFSVersion != "" {
		mountOptions = append(mountOptions, "nfsvers="+nsc.Spec.Connection.NFSVersion)
	}

	if commonfeature.TLSEnabled() {
		if nsc.Spec.Connection.Mtls {
			mountOptions = append(mountOptions, "xprtsec=mtls")
		} else if nsc.Spec.Connection.Tls {
			mountOptions = append(mountOptions, "xprtsec=tls")
		}
	}

	if nsc.Spec.MountOptions != nil {
		if nsc.Spec.MountOptions.MountMode != "" {
			mountOptions = append(mountOptions, nsc.Spec.MountOptions.MountMode)
		}

		if nsc.Spec.MountOptions.Timeout > 0 {
			mountOptions = append(mountOptions, "timeo="+strconv.Itoa(nsc.Spec.MountOptions.Timeout))
		}

		if nsc.Spec.MountOptions.Retransmissions > 0 {
			mountOptions = append(mountOptions, "retrans="+strconv.Itoa(nsc.Spec.MountOptions.Retransmissions))
		}

		if nsc.Spec.MountOptions.ReadOnly != nil {
			if *nsc.Spec.MountOptions.ReadOnly {
				mountOptions = append(mountOptions, "ro")
			} else {
				mountOptions = append(mountOptions, "rw")
			}
		}
	}

	return mountOptions
}

func GetSCParams(nsc *v1alpha1.NFSStorageClass, controllerNamespace string) map[string]string {
	params := make(map[string]string)

	params[serverParamKey] = nsc.Spec.Connection.Host
	params[shareParamKey] = nsc.Spec.Connection.Share
	params[ProvisionerSecretNameKey] = SecretForMountOptionsPrefix + nsc.Name
	params[ProvisionerSecretNamespaceKey] = controllerNamespace

	if nsc.Spec.ChmodPermissions != "" {
		params[MountPermissionsParamKey] = nsc.Spec.ChmodPermissions
	}

	return params
}

func IdentifyReconcileFuncForSecret(log logger.Logger, secretList *corev1.SecretList, nsc *v1alpha1.NFSStorageClass, controllerNamespace string) (reconcileType string, err error) {
	if shouldReconcileByDeleteFunc(nsc) {
		return DeleteReconcile, nil
	}

	if shouldReconcileSecretByCreateFunc(secretList, nsc) {
		return CreateReconcile, nil
	}

	should, err := shouldReconcileSecretByUpdateFunc(log, secretList, nsc, controllerNamespace)
	if err != nil {
		return "", err
	}
	if should {
		return UpdateReconcile, nil
	}

	return "", nil
}

func shouldReconcileSecretByCreateFunc(secretList *corev1.SecretList, nsc *v1alpha1.NFSStorageClass) bool {
	if nsc.DeletionTimestamp != nil {
		return false
	}

	for _, s := range secretList.Items {
		if s.Name == SecretForMountOptionsPrefix+nsc.Name {
			return false
		}
	}

	return true
}

func shouldReconcileSecretByUpdateFunc(log logger.Logger, secretList *corev1.SecretList, nsc *v1alpha1.NFSStorageClass, controllerNamespace string) (bool, error) {
	if nsc.DeletionTimestamp != nil {
		return false, nil
	}

	secretSelector := labels.Set(map[string]string{
		NFSStorageClassManagedLabelKey: NFSStorageClassManagedLabelValue,
	})

	for _, oldSecret := range secretList.Items {
		if oldSecret.Name == SecretForMountOptionsPrefix+nsc.Name {
			newSecret := configureSecret(nsc, controllerNamespace)
			if !reflect.DeepEqual(oldSecret.StringData, newSecret.StringData) {
				log.Debug(fmt.Sprintf("[shouldReconcileSecretByUpdateFunc] a secret %s should be updated", oldSecret.Name))
				if !labels.Set(oldSecret.Labels).AsSelector().Matches(secretSelector) {
					err := fmt.Errorf("a secret %q does not have a label %s=%s", oldSecret.Name, NFSStorageClassManagedLabelKey, NFSStorageClassManagedLabelValue)
					return false, err
				}
				return true, nil
			}

			if !labels.Set(oldSecret.Labels).AsSelector().Matches(secretSelector) {
				log.Debug(fmt.Sprintf("[shouldReconcileSecretByUpdateFunc] a secret %s should be updated. The label %s=%s is missing", oldSecret.Name, NFSStorageClassManagedLabelKey, NFSStorageClassManagedLabelValue))
				return true, nil
			}

			return false, nil
		}
	}

	log.Debug(fmt.Sprintf("[shouldReconcileSecretByUpdateFunc] a secret %s not found. It should be created", SecretForMountOptionsPrefix+nsc.Name))
	log.Trace(fmt.Sprintf("[shouldReconcileSecretByUpdateFunc] secret list: %+v", secretList))
	return true, nil
}

func configureSecret(nsc *v1alpha1.NFSStorageClass, controllerNamespace string) *corev1.Secret {
	mountOptions := GetSCMountOptions(nsc)
	secret := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      SecretForMountOptionsPrefix + nsc.Name,
			Namespace: controllerNamespace,
			Labels: map[string]string{
				NFSStorageClassManagedLabelKey: NFSStorageClassManagedLabelValue,
			},
			Finalizers: []string{NFSStorageClassControllerFinalizerName},
		},
		StringData: map[string]string{
			MountOptionsSecretKey: strings.Join(mountOptions, ","),
		},
	}

	if nsc.Spec.VolumeCleanup != "" {
		secret.StringData[volumeCleanupMethodKey] = nsc.Spec.VolumeCleanup
	}

	return secret
}

// VolumeSnaphotClass
func IdentifyReconcileFuncForVSClass(log logger.Logger, vsClassList *snapshotv1.VolumeSnapshotClassList, nsc *v1alpha1.NFSStorageClass, controllerNamespace string) (reconcileType string, oldVSClass, newVSClass *snapshotv1.VolumeSnapshotClass) {
	oldVSClass = findVSClass(vsClassList, nsc.Name)

	if oldVSClass == nil {
		log.Debug(fmt.Sprintf("[IdentifyReconcileFuncForVSClass] no volume snapshot class found for the NFSStorageClass %s", nsc.Name))
	} else {
		log.Debug(fmt.Sprintf("[IdentifyReconcileFuncForVSClass] finds old volume snapshot class for the NFSStorageClass %s", nsc.Name))
		log.Trace(fmt.Sprintf("[IdentifyReconcileFuncForVSClass] old volume snapshot class: %+v", oldVSClass))
	}

	if shouldReconcileByDeleteFunc(nsc) {
		return DeleteReconcile, oldVSClass, nil
	}

	newVSClass = ConfigureVSClass(oldVSClass, nsc, controllerNamespace)
	log.Debug(fmt.Sprintf("[IdentifyReconcileFuncForVSClass] successfully configurated new volume snapshot class for the NFSStorageClass %s", nsc.Name))
	log.Trace(fmt.Sprintf("[IdentifyReconcileFuncForVSClass] new volume snapshot class: %+v", newVSClass))

	if shouldReconcileVSClassByCreateFunc(oldVSClass, nsc) {
		return CreateReconcile, nil, newVSClass
	}

	if shouldReconcileVSClassByUpdateFunc(log, oldVSClass, newVSClass, nsc) {
		return UpdateReconcile, oldVSClass, newVSClass
	}

	return "", oldVSClass, newVSClass
}

func shouldReconcileVSClassByCreateFunc(oldVSClass *snapshotv1.VolumeSnapshotClass, nsc *v1alpha1.NFSStorageClass) bool {
	if nsc.DeletionTimestamp != nil {
		return false
	}

	if oldVSClass != nil {
		return false
	}

	return true
}

func shouldReconcileVSClassByUpdateFunc(log logger.Logger, oldVSClass, newVSClass *snapshotv1.VolumeSnapshotClass, nsc *v1alpha1.NFSStorageClass) bool {
	if nsc.DeletionTimestamp != nil {
		return false
	}

	if oldVSClass == nil {
		return false
	}

	diff := CompareVSClasses(oldVSClass, newVSClass)
	if diff != "" {
		log.Debug(fmt.Sprintf("[shouldReconcileVSClassByUpdateFunc] a volume snapshot class %s should be updated. Diff: %s", oldVSClass.Name, diff))
		return true
	}

	return false
}

func findVSClass(vsClassList *snapshotv1.VolumeSnapshotClassList, name string) *snapshotv1.VolumeSnapshotClass {
	for _, vsClass := range vsClassList.Items {
		if vsClass.Name == name {
			return &vsClass
		}
	}
	return nil
}

func ConfigureVSClass(oldVSClass *snapshotv1.VolumeSnapshotClass, nsc *v1alpha1.NFSStorageClass, controllerNamespace string) *snapshotv1.VolumeSnapshotClass {
	deletionPolicy := snapshotv1.DeletionPolicy(nsc.Spec.ReclaimPolicy)


	newVSClass := &snapshotv1.VolumeSnapshotClass{
		ObjectMeta: metav1.ObjectMeta{
			Name:      nsc.Name,
			Namespace: nsc.Namespace,
			Labels: map[string]string{
				NFSStorageClassManagedLabelKey: NFSStorageClassManagedLabelValue,
			},
			Finalizers: []string{NFSStorageClassControllerFinalizerName},
		},
		Driver:         NFSStorageClassProvisioner,
		DeletionPolicy: deletionPolicy,
		Parameters: map[string]string{
			"mountOptions": strings.Join(GetSCMountOptions(nsc), ","),
		},
	}

	if oldVSClass != nil {
		if oldVSClass.Labels != nil {
			newVSClass.Labels = labels.Merge(oldVSClass.Labels, newVSClass.Labels)
		}
		if oldVSClass.Annotations != nil {
			newVSClass.Annotations = oldVSClass.Annotations
		}
	}

	return newVSClass
}

func CompareVSClasses(vsClass, newVSClass *snapshotv1.VolumeSnapshotClass) string {
	var diffs []string

	if vsClass.DeletionPolicy != newVSClass.DeletionPolicy {
		diffs = append(diffs, fmt.Sprintf("DeletionPolicy: %s -> %s", vsClass.DeletionPolicy, newVSClass.DeletionPolicy))
	}

	if !cmp.Equal(vsClass.Parameters, newVSClass.Parameters) {
		diffs = append(diffs,
			fmt.Sprintf("Parameters diff: %s", cmp.Diff(vsClass.Parameters, newVSClass.Parameters)))
	}

	if !cmp.Equal(vsClass.ObjectMeta.Labels, newVSClass.ObjectMeta.Labels) {
		diffs = append(diffs,
			fmt.Sprintf("Labels diff: %s", cmp.Diff(vsClass.ObjectMeta.Labels, newVSClass.ObjectMeta.Labels)))
	}

	if !cmp.Equal(vsClass.ObjectMeta.Annotations, newVSClass.ObjectMeta.Annotations) {
		diffs = append(diffs,
			fmt.Sprintf("Annotations diff: %s", cmp.Diff(vsClass.ObjectMeta.Annotations, newVSClass.ObjectMeta.Annotations)))
	}

	return strings.Join(diffs, ", ")
}

func reconcileVolumeSnapshotClassCreateFunc(
	ctx context.Context,
	cl client.Client,
	log logger.Logger,
	newVSClass *snapshotv1.VolumeSnapshotClass,
	nsc *v1alpha1.NFSStorageClass,
) (bool, error) {
	log.Debug(fmt.Sprintf("[reconcileVolumeSnapshotClassCreateFunc] starts for VolumeSnapshotClass %q", newVSClass.Name))
	log.Trace(fmt.Sprintf("[reconcileVolumeSnapshotClassCreateFunc] volume snapshot class: %+v", newVSClass))

	err := cl.Create(ctx, newVSClass)
	if err != nil {
		err = fmt.Errorf("[reconcileVolumeSnapshotClassCreateFunc] unable to create a VolumeSnapshotClass %s: %w", newVSClass.Name, err)
		upError := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, err.Error())
		if upError != nil {
			upError = fmt.Errorf("[reconcileVolumeSnapshotClassCreateFunc] unable to update the NFSStorageClass %s: %w", nsc.Name, upError)
			err = errors.Join(err, upError)
		}
		return true, err
	}

	log.Info(fmt.Sprintf("[reconcileVolumeSnapshotClassCreateFunc] successfully create volume snapshot class, name: %s", newVSClass.Name))

	return false, nil
}

func reconcileVolumeSnapshotClassUpdateFunc(
	ctx context.Context,
	cl client.Client,
	log logger.Logger,
	oldVSClass *snapshotv1.VolumeSnapshotClass,
	newVSClass *snapshotv1.VolumeSnapshotClass,
	nsc *v1alpha1.NFSStorageClass,
) (bool, error) {
	log.Debug(fmt.Sprintf("[reconcileVolumeSnapshotClassUpdateFunc] starts for VolumeSnapshotClass %q", newVSClass.Name))

	newVSClass.ObjectMeta.ResourceVersion = oldVSClass.ObjectMeta.ResourceVersion
	err := cl.Update(ctx, newVSClass)
	if err != nil {
		err = fmt.Errorf("[reconcileVolumeSnapshotClassUpdateFunc] unable to update a VolumeSnapshotClass %s: %w", newVSClass.Name, err)
		upError := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, err.Error())
		if upError != nil {
			upError = fmt.Errorf("[reconcileVolumeSnapshotClassUpdateFunc] unable to update the NFSStorageClass %s: %w", nsc.Name, upError)
			err = errors.Join(err, upError)
		}
		return true, err
	}

	log.Info(fmt.Sprintf("[reconcileVolumeSnapshotClassUpdateFunc] successfully updated a VolumeSnapshotClass, name: %s", newVSClass.Name))

	return false, nil
}

func reconcileVolumeSnapshotClassDeleteFunc(
	ctx context.Context,
	cl client.Client,
	log logger.Logger,
	oldVSClass *snapshotv1.VolumeSnapshotClass,
	nsc *v1alpha1.NFSStorageClass,
) (bool, error) {
	log.Debug(fmt.Sprintf("[reconcileVolumeSnapshotClassDeleteFunc] starts for VolumeSnapshotClass %q", nsc.Name))

	if oldVSClass == nil {
		log.Info(fmt.Sprintf("[reconcileVolumeSnapshotClassDeleteFunc] no volume snapshot class found for the NFSStorageClass, name: %s", nsc.Name))
		log.Debug("[reconcileVolumeSnapshotClassDeleteFunc] ends the reconciliation")
		return false, nil
	}

	log.Info(fmt.Sprintf("[reconcileVolumeSnapshotClassDeleteFunc] successfully found a volume snapshot class %s for the NFSStorageClass %s", oldVSClass.Name, nsc.Name))
	log.Trace(fmt.Sprintf("[reconcileVolumeSnapshotClassDeleteFunc] volume snapshot class: %+v", oldVSClass))

	if !slices.Contains(allowedProvisioners, oldVSClass.Driver) {
		log.Info(fmt.Sprintf("[reconcileVolumeSnapshotClassDeleteFunc] the volume snapshot class %s driver %s does not belong to allowed provisioners: %v. Skip deletion", oldVSClass.Name, oldVSClass.Driver, allowedProvisioners))
		return false, nil
	}

	err := deleteVolumeSnapshotClass(ctx, cl, oldVSClass)
	if err != nil {
		err = fmt.Errorf("[reconcileVolumeSnapshotClassDeleteFunc] unable to delete a volume snapshot class %s: %w", oldVSClass.Name, err)
		upError := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, err.Error())
		if upError != nil {
			upError = fmt.Errorf("[reconcileVolumeSnapshotClassDeleteFunc] unable to update the NFSStorageClass %s: %w", nsc.Name, upError)
			err = errors.Join(err, upError)
		}
		return true, err
	}

	log.Info(fmt.Sprintf("[reconcileVolumeSnapshotClassDeleteFunc] successfully deleted a volume snapshot class, name: %s", oldVSClass.Name))

	log.Debug("[reconcileVolumeSnapshotClassDeleteFunc] ends the reconciliation")
	return false, nil
}

func deleteVolumeSnapshotClass(ctx context.Context, cl client.Client, vsClass *snapshotv1.VolumeSnapshotClass) error {
	if !slices.Contains(allowedProvisioners, vsClass.Driver) {
		return fmt.Errorf("a volume snapshot class %s with driver %s does not belong to allowed provisioners: %v", vsClass.Name, vsClass.Driver, allowedProvisioners)
	}

	_, err := removeFinalizerIfExists(ctx, cl, vsClass, NFSStorageClassControllerFinalizerName)
	if err != nil {
		return err
	}

	err = cl.Delete(ctx, vsClass)
	if err != nil {
		return err
	}

	return nil
}
