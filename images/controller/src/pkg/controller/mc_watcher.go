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
	"errors"
	"fmt"
	"github.com/deckhouse/csi-nfs/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"reflect"
	"time"

	v1 "k8s.io/api/storage/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	//	NFSStorageClassCtrlName = "nfs-storage-class-controller"
	ModuleConfigCtrlName = "csi-nfs-module-config-controller"
	//	StorageClassKind       = "StorageClass"
	//	StorageClassAPIVersion = "storage.k8s.io/v1"

	// NFSStorageClassProvisioner = "nfs.csi.k8s.io"
	//
	// NFSStorageClassControllerFinalizerName = "storage.deckhouse.io/nfs-storage-class-controller"
	// NFSStorageClassManagedLabelKey         = "storage.deckhouse.io/managed-by"
	// NFSStorageClassManagedLabelValue       = "nfs-storage-class-controller"
	//
	// StorageClassDefaultAnnotationKey     = "storageclass.kubernetes.io/is-default-class"
	// StorageClassDefaultAnnotationValTrue = "true"
	//
	// AllowVolumeExpansionDefaultValue = true
	//
	// FailedStatusPhase  = "Failed"
	// CreatedStatusPhase = "Created"
	//
	// CreateReconcile = "Create"
	// UpdateReconcile = "Update"
	// DeleteReconcile = "Delete"
	//
	// serverParamKey           = "server"
	// shareParamKey            = "share"
	// MountPermissionsParamKey = "mountPermissions"
	// MountOptionsSecretKey    = "mountOptions"
	//
	// SecretForMountOptionsPrefix = "nfs-mount-options-for-"
	// StorageClassSecretNameKey   = "csi.storage.k8s.io/provisioner-secret-name"
	// StorageClassSecretNSKey     = "csi.storage.k8s.io/provisioner-secret-namespace"
	// NFS3PrometheusLabel         = "nfsv3-configured-but-not-enabled"
	//   CsiNfsModuleName            = "csi-nfs"
)

var (
// allowedProvisioners = []string{NFSStorageClassProvisioner}
)

func RunModuleConfigWatcherController(
	mgr manager.Manager,
	cfg config.Options,
	log logger.Logger,
) (controller.Controller, error) {
	cl := mgr.GetClient()

	c, err := controller.New(ModuleConfigCtrlName, mgr, controller.Options{
		Reconciler: reconcile.Func(func(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
			log.Info(fmt.Sprintf("[ModuleConfigReconciler] starts Reconcile for the ModuleConfig %q", request.Name))
			mc := &v1alpha1.ModuleConfig{}
			nsc := &v1alpha1.NFSStorageClass{}
			err := cl.Get(ctx, request.NamespacedName, mc)
			if err != nil && !k8serr.IsNotFound(err) {
				log.Error(err, fmt.Sprintf("[ModuleConfigReconciler] unable to get ModuleConfig, name: %s", request.Name))
				return reconcile.Result{}, err
			}

			if mc.Name == "" {
				log.Info(fmt.Sprintf("[ModuleConfigReconciler] seems like the ModuleConfig for the request %s was deleted. Reconcile retrying will stop.", request.Name))
				return reconcile.Result{}, nil
			}

			scList := &v1.StorageClassList{}
			err = cl.List(ctx, scList)
			if err != nil {
				log.Error(err, "[ModuleConfigReconciler] unable to list NFSStorageClasses")
				return reconcile.Result{}, err
			}

			shouldRequeue, err := RunMCEventReconcile(ctx, cl, log, scList, mc, nsc, cfg.ControllerNamespace)
			if err != nil {
				log.Error(err, fmt.Sprintf("[ModuleConfigReconciler] an error occured while reconciling the NFSStorageClass, name: %s", nsc.Name))
			}

			if shouldRequeue {
				log.Warning(fmt.Sprintf("[NFSStorageClassReconciler] Reconciler will requeue the request, name: %s", request.Name))
				return reconcile.Result{
					RequeueAfter: cfg.RequeueStorageClassInterval * time.Second,
				}, nil
			}

			log.Info(fmt.Sprintf("[ModuleConfigReconciler] ends Reconcile for the ModuleConfig %q", request.Name))
			return reconcile.Result{}, nil
		}),
	})
	if err != nil {
		log.Error(err, "[RunModuleConfigWatcherController] unable to create controller")
		return nil, err
	}

	err = c.Watch(source.Kind(mgr.GetCache(), &v1alpha1.ModuleConfig{}), handler.Funcs{

		UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.RateLimitingInterface) {
			log.Info(fmt.Sprintf("[UpdateFunc] get event for ModuleConfig %q. Check if it should be reconciled", e.ObjectNew.GetName()))

			oldMC, ok := e.ObjectOld.(*v1alpha1.ModuleConfig)
			if !ok {
				err = errors.New("unable to cast event object to a given type")
				log.Error(err, "[UpdateFunc] an error occurred while handling create event")
				return
			}
			newMC, ok := e.ObjectNew.(*v1alpha1.ModuleConfig)
			if !ok {
				err = errors.New("unable to cast event object to a given type")
				log.Error(err, "[UpdateFunc] an error occurred while handling create event")
				return
			}

			if reflect.DeepEqual(oldMC.Spec, newMC.Spec) && newMC.DeletionTimestamp == nil {
				log.Info(fmt.Sprintf("[UpdateFunc] an update event for the ModuleConfig %s has no Spec field updates. It will not be reconciled", newMC.Name))
				return
			}

			log.Info(fmt.Sprintf("[UpdateFunc] the ModuleConfig %q will be reconciled. Add to the queue", newMC.Name))
			request := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: newMC.Namespace, Name: newMC.Name}}
			q.Add(request)
		},
	})
	if err != nil {
		log.Error(err, "[RunModuleConfigController] unable to watch the events")
		return nil, err
	}

	return c, nil
}

func RunMCEventReconcile(ctx context.Context, cl client.Client, log logger.Logger, scList *v1.StorageClassList, mc *v1alpha1.ModuleConfig, nsc *v1alpha1.NFSStorageClass, controllerNamespace string) (shouldRequeue bool, err error) {
	added, err := addFinalizerIfNotExists(ctx, cl, nsc, NFSStorageClassControllerFinalizerName)
	if err != nil {
		err = fmt.Errorf("[reconcileStorageClassCreateFunc] unable to add a finalizer %s to the NFSStorageClass %s: %w", NFSStorageClassControllerFinalizerName, nsc.Name, err)
		return true, err
	}
	log.Debug(fmt.Sprintf("[reconcileStorageClassCreateFunc] finalizer %s was added to the NFSStorageClass %s: %t", NFSStorageClassControllerFinalizerName, nsc.Name, added))

	reconcileTypeForModuleConfig, err := IdentifyReconcileFuncForModuleConfig(log, scList, nsc, mc, controllerNamespace)
	if err != nil {
		err = fmt.Errorf("[runEventReconcile] error occured while identifying the reconcile function for ModuleCOnfig %s: %w", nsc.Name, err)
		return true, err
	}

	shouldRequeue = false
	log.Debug(fmt.Sprintf("[runMCEventReconcile] reconcile operation for ModuleConfig %q: %q", nsc.Name, reconcileTypeForModuleConfig))
	switch reconcileTypeForModuleConfig {

	case UpdateReconcile:
		log.Debug(fmt.Sprintf("[runMCEventReconcile] reconcile operation for ModuleConfig %q: %q\", nsc.Name, reconcileTypeForModuleConfig))\n\tswitch reconcileTypeForModuleConfig {] UpdateReconcile starts reconciliataion of StorageClass, name: %s", nsc.Name))
		shouldRequeue, err = reconcileModuleConfigUpdateFunc(ctx, cl, log, scList, nsc, controllerNamespace)
	case DeleteReconcile:
		log.Debug(fmt.Sprintf("[runMCEventReconcile] reconcile operation for ModuleConfig %q: %q\", nsc.Name, reconcileTypeForModuleConfig))\n\tswitch reconcileTypeForModuleConfig {] DeleteReconcile starts reconciliataion of StorageClass, name: %s", nsc.Name))
		shouldRequeue, err = reconcileModuleConfigDeleteFunc(ctx, cl, log, scList, nsc)
	default:
		log.Debug(fmt.Sprintf("[runMCEventReconcile] reconcile operation for ModuleConfig %q: %q\", nsc.Name, reconcileTypeForModuleConfig))\n\tswitch reconcileTypeForModuleConfig {] StorageClass for NFSStorageClass %s should not be reconciled", nsc.Name))
	}
	log.Debug(fmt.Sprintf("[runMCEventReconcile] reconcile operation for ModuleConfig %q: %q\", nsc.Name, reconcileTypeForModuleConfig))\n\tswitch reconcileTypeForModuleConfig {] ends reconciliataion of StorageClass, name: %s, shouldRequeue: %t, err: %v", nsc.Name, shouldRequeue, err))

	if err != nil || shouldRequeue {
		return shouldRequeue, err
	}

	return false, nil

}

func IdentifyReconcileFuncForModuleConfig(log logger.Logger, scList *v1.StorageClassList, nsc *v1alpha1.NFSStorageClass, mc *v1alpha1.ModuleConfig, controllerNamespace string) (reconcileType string, err error) {
	if shouldReconcileModuleConfigByDeleteFunc(mc) {
		return DeleteReconcile, nil
	}

	should, err := shouldReconcileModuleConfigByUpdateFunc(log, scList, nsc, mc, controllerNamespace)
	if err != nil {
		return "", err
	}
	if should {
		return UpdateReconcile, nil
	}

	return "", nil
}

func shouldReconcileModuleConfigByUpdateFunc(log logger.Logger, scList *v1.StorageClassList, nsc *v1alpha1.NFSStorageClass, mc *v1alpha1.ModuleConfig, controllerNamespace string) (bool, error) {
	if nsc.DeletionTimestamp != nil {
		return false, nil
	}

	for _, oldSC := range scList.Items {
		if oldSC.Name == nsc.Name {
			if slices.Contains(allowedProvisioners, oldSC.Provisioner) {
				newSC, err := updateModuleConfig(nsc, &oldSC, controllerNamespace)
				if err != nil {
					return false, err
				}

				diff, err := GetMCDiff(oldMC, newMC)
				if err != nil {
					return false, err
				}

				if diff != "" {
					log.Debug(fmt.Sprintf("[shouldReconcileModuleConfigByUpdateFunc] a modul config %s should be updated. Diff: %s", oldMC.Name, diff))
					return true, nil
				}

				if nsc.Status != nil && nsc.Status.Phase == FailedStatusPhase {
					return true, nil
				}

				return false, nil

			} else {
				err := fmt.Errorf("a storage class %s with provisioner % s does not belong to allowed provisioners: %v", oldSC.Name, oldSC.Provisioner, allowedProvisioners)
				return false, err
			}
		}
	}

	err := fmt.Errorf("a storage class %s does not exist", nsc.Name)
	return false, err
}

func shouldReconcileModuleConfigByDeleteFunc(obj metav1.Object) bool {
	if obj.GetDeletionTimestamp() != nil {
		return true
	}

	return false
}

func updateModuleConfig(mc *v1alpha1.ModuleConfig, oldMC *v1alpha1.ModuleConfig, controllerNamespace string) (*v1alpha1.ModuleConfig, error) {
	newMC, err := ConfigureModuleConfig(mc, controllerNamespace)
	if err != nil {
		return nil, err
	}

	if oldMC.Annotations != nil {
		newMC.Annotations = oldMC.Annotations
	}

	return newMC, nil
}

func ConfigureModuleConfig(mc *v1alpha1.ModuleConfig, controllerNamespace string) (*v1alpha1.ModuleConfig, error) {

	// TODO REFORMAT
	//
	//	if nsc.Spec.ReclaimPolicy == "" {
	//		err := fmt.Errorf("NFSStorageClass %q: the ReclaimPolicy field is empty", nsc.Name)
	//		return nil, err
	//	}
	//	if nsc.Spec.VolumeBindingMode == "" {
	//		err := fmt.Errorf("NFSStorageClass %q: the VolumeBindingMode field is empty", nsc.Name)
	//		return nil, err
	//	}
	//
	//	reclaimPolicy := corev1.PersistentVolumeReclaimPolicy(nsc.Spec.ReclaimPolicy)
	//	volumeBindingMode := v1.VolumeBindingMode(nsc.Spec.VolumeBindingMode)
	//	AllowVolumeExpansion := AllowVolumeExpansionDefaultValue
	//
	//	sc := &v1.StorageClass{
	//		TypeMeta: metav1.TypeMeta{
	//			Kind:       StorageClassKind,
	//			APIVersion: StorageClassAPIVersion,
	//		},
	//		ObjectMeta: metav1.ObjectMeta{
	//			Name:       nsc.Name,
	//			Namespace:  nsc.Namespace,
	//			Finalizers: []string{NFSStorageClassControllerFinalizerName},
	//		},
	//		Parameters:           GetSCParams(nsc, controllerNamespace),
	//		MountOptions:         GetSCMountOptions(nsc),
	//		Provisioner:          NFSStorageClassProvisioner,
	//		ReclaimPolicy:        &reclaimPolicy,
	//		VolumeBindingMode:    &volumeBindingMode,
	//		AllowVolumeExpansion: &AllowVolumeExpansion,
	//	}
	//
	return mc, nil
}

func GetMCDiff(oldMC *v1alpha1.ModuleConfig, newMC *v1alpha1.ModuleConfig) (string, error) {

	if !reflect.DeepEqual(oldMC, newMC) {
		diff := fmt.Sprintf("MountOptions: %v -> %v", oldMC.Spec, newMC.Spec)
		return diff, nil
	}

	return "", nil
}

func ReconcileModuleConfigCreateFunc(ctx context.Context, cl client.Client, log logger.Logger, nsc *v1alpha1.NFSStorageClass, controllerNamespace string) (bool, error) {
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

func reconcileModuleConfigUpdateFunc(ctx context.Context, cl client.Client, log logger.Logger, secretList *corev1.SecretList, nsc *v1alpha1.NFSStorageClass, controllerNamespace string) (bool, error) {
	log.Debug(fmt.Sprintf("[reconcileModuleConfigUpdateFunc] starts for secret %q", SecretForMountOptionsPrefix+nsc.Name))

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

func reconcileModuleConfigDeleteFunc(ctx context.Context, cl client.Client, log logger.Logger, secretList *corev1.SecretList, nsc *v1alpha1.NFSStorageClass) (bool, error) {
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

func checkNFSv3(ctx context.Context, nsc *v1alpha1.NFSStorageClass, cl client.Client) (bool, error) {

	v3presents := false
	v3support := false

	if nsc.ObjectMeta.DeletionTimestamp == nil && nsc.Spec.Connection.NFSVersion == "3" {
		v3presents = true
	}

	v3check, err := getModuleConfigNFSSettings(ctx, cl)
	if err != nil {
		err = fmt.Errorf("[reconcileStorageClassCreateFunc] unable to get ModuleConfig settings: %w", err)
		return true, err
	}

	if v3check == true {
		v3support = true
	}

	if v3presents && !v3support {
		klog.Infof("NFS v3 is not enabled in module config, but NFSv3StorageClass exists: %s", nsc)
		labels := addLabelToStorageClass(nsc)
		nsc.ObjectMeta.SetLabels(labels)
		err = cl.Update(ctx, nsc)
		if err != nil {
			err = fmt.Errorf("error updating labels for nsc %s: %w", nsc, err)
			return true, err
		}
	}
	return false, nil
}

func getModuleConfigNFSSettings(ctx context.Context, cl client.Client) (v3support bool, err error) {

	nfsModuleConfig := &v1alpha1.ModuleConfig{}

	err = cl.Get(ctx, types.NamespacedName{Name: CsiNfsModuleName, Namespace: ""}, nfsModuleConfig)
	if err != nil {
		klog.Fatal(err)
	}
	if value, exists := nfsModuleConfig.Spec.Settings["v3support"]; exists && value == true {
		return true, nil
	}
	return false, nil
}

func addLabelToStorageClass(nsc *v1alpha1.NFSStorageClass) map[string]string {
	var newLabels map[string]string

	if nsc.Labels == nil {
		newLabels = make(map[string]string)
		nsc.Labels = newLabels
	} else {
		newLabels = make(map[string]string, len(nsc.Labels))
	}

	for key, value := range nsc.Labels {
		newLabels[key] = value
	}
	newLabels[NFS3PrometheusLabel] = "true"

	return newLabels
}
