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
	v1alpha1 "d8-controller/api/v1alpha1"
	"d8-controller/pkg/config"
	"d8-controller/pkg/logger"
	"d8-controller/pkg/monitoring"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/storage/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"sigs.k8s.io/controller-runtime/pkg/manager"
)

const (
	NFSStorageClassCtrlName = "nfs-storage-class-controller"

	StorageClassKind       = "StorageClass"
	StorageClassAPIVersion = "storage.k8s.io/v1"

	NFSStorageClassProvisioner = "nfs.csi.k8s.io"

	NFSStorageClassFinalizerName     = "storage.deckhouse.io/nfs-storage-class-controller"
	NFSStorageClassManagedLabelKey   = "storage.deckhouse.io/managed-by"
	NFSStorageClassManagedLabelValue = "nfs-storage-class-controller"

	AllowVolumeExpansionDefaultValue = true

	FailedStatusPhase  = "Failed"
	CreatedStatusPhase = "Created"

	CreateReconcile = "Create"
	UpdateReconcile = "Update"
	DeleteReconcile = "Delete"

	serverParamKey           = "server"
	shareParamKey            = "share"
	mountPermissionsParamKey = "mountPermissions"
	mountOptionsSecretKey    = "mountOptions"

	SecretForMountOptionsPrefix = "nfs-mount-options-for-"
)

func RunNFSStorageClassWatcherController(
	mgr manager.Manager,
	cfg config.Options,
	log logger.Logger,
	metrics monitoring.Metrics,
) (controller.Controller, error) {
	cl := mgr.GetClient()

	c, err := controller.New(NFSStorageClassCtrlName, mgr, controller.Options{
		Reconciler: reconcile.Func(func(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
			log.Info("[NFSStorageClassReconciler] starts Reconcile for the NFSStorageClass %q", request.Name)
			nsc := &v1alpha1.NFSStorageClass{}
			err := cl.Get(ctx, request.NamespacedName, nsc)
			if err != nil && !k8serr.IsNotFound(err) {
				log.Error(err, fmt.Sprintf("[NFSStorageClassReconciler] unable to get NFSStorageClass, name: %s", request.Name))
				return reconcile.Result{}, err
			}

			if nsc.Name == "" {
				log.Info(fmt.Sprintf("[NFSStorageClassReconciler] seems like the NFSStorageClass for the request %s was deleted. Reconcile retrying will stop.", request.Name))
				return reconcile.Result{}, nil
			}

			scList := &v1.StorageClassList{}
			err = cl.List(ctx, scList)
			if err != nil {
				log.Error(err, "[NFSStorageClassReconciler] unable to list Storage Classes")
				return reconcile.Result{}, err
			}

			shouldRequeue, err := runEventReconcile(ctx, cl, log, scList, nsc, cfg.ControllerNamespace)
			if err != nil {
				log.Error(err, fmt.Sprintf("[NFSStorageClassReconciler] an error occured while reconciles the NFSStorageClass, name: %s", nsc.Name))
			}

			if shouldRequeue {
				log.Warning(fmt.Sprintf("[NFSStorageClassReconciler] Reconciler will requeue the request, name: %s", request.Name))
				return reconcile.Result{
					RequeueAfter: cfg.RequeueStorageClassInterval * time.Second,
				}, nil
			}

			log.Info(fmt.Sprintf("[NFSStorageClassReconciler] ends Reconcile for the NFSStorageClass %q", request.Name))
			return reconcile.Result{}, nil
		}),
	})
	if err != nil {
		log.Error(err, "[RunNFSStorageClassWatcherController] unable to create controller")
		return nil, err
	}

	err = c.Watch(source.Kind(mgr.GetCache(), &v1alpha1.NFSStorageClass{}), handler.Funcs{
		CreateFunc: func(ctx context.Context, e event.CreateEvent, q workqueue.RateLimitingInterface) {
			log.Info(fmt.Sprintf("[CreateFunc] get event for NFSStorageClass %q. Add to the queue", e.Object.GetName()))
			request := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: e.Object.GetNamespace(), Name: e.Object.GetName()}}
			q.Add(request)
		},
		UpdateFunc: func(ctx context.Context, e event.UpdateEvent, q workqueue.RateLimitingInterface) {
			log.Info(fmt.Sprintf("[UpdateFunc] get event for NFSStorageClass %q. Check if it should be reconciled", e.ObjectNew.GetName()))

			oldLsc, ok := e.ObjectOld.(*v1alpha1.NFSStorageClass)
			if !ok {
				err = errors.New("unable to cast event object to a given type")
				log.Error(err, "[UpdateFunc] an error occurred while handling create event")
				return
			}
			newLsc, ok := e.ObjectNew.(*v1alpha1.NFSStorageClass)
			if !ok {
				err = errors.New("unable to cast event object to a given type")
				log.Error(err, "[UpdateFunc] an error occurred while handling create event")
				return
			}

			if reflect.DeepEqual(oldLsc.Spec, newLsc.Spec) && newLsc.DeletionTimestamp == nil {
				log.Info(fmt.Sprintf("[UpdateFunc] an update event for the NFSStorageClass %s has no Spec field updates. It will not be reconciled", newLsc.Name))
				return
			}

			log.Info(fmt.Sprintf("[UpdateFunc] the NFSStorageClass %q will be reconciled. Add to the queue", newLsc.Name))
			request := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: newLsc.Namespace, Name: newLsc.Name}}
			q.Add(request)
		},
	})
	if err != nil {
		log.Error(err, "[RunNFSStorageClassWatcherController] unable to watch the events")
		return nil, err
	}

	return c, nil
}

func runEventReconcile(ctx context.Context, cl client.Client, log logger.Logger, scList *v1.StorageClassList, nsc *v1alpha1.NFSStorageClass, controllerNamespace string) (shouldRequeue bool, err error) {
	reconcileTypeForStorageClass, err := identifyReconcileFuncForStorageClass(log, scList, nsc)
	if err != nil {
		log.Error(err, fmt.Sprintf("[runEventReconcile] error occured while identifying the reconcile function for StorageClass %s", nsc.Name))
		return true, err
	}

	shouldRequeue = false
	log.Debug(fmt.Sprintf("[runEventReconcile] reconcile operation for StorageClass %q: %q", nsc.Name, reconcileTypeForStorageClass))
	switch reconcileTypeForStorageClass {
	case CreateReconcile:
		log.Debug(fmt.Sprintf("[runEventReconcile] CreateReconcile starts reconciliataion of StorageClass, name: %s", nsc.Name))
		shouldRequeue, err = reconcileStorageClassCreateFunc(ctx, cl, log, scList, nsc)
	case UpdateReconcile:
		log.Debug(fmt.Sprintf("[runEventReconcile] UpdateReconcile starts reconciliataion of StorageClass, name: %s", nsc.Name))
		shouldRequeue, err = reconcileStorageClassUpdateFunc(ctx, cl, log, scList, nsc)
	case DeleteReconcile:
		log.Debug(fmt.Sprintf("[runEventReconcile] DeleteReconcile starts reconciliataion of StorageClass, name: %s", nsc.Name))
		shouldRequeue, err = reconcileStorageClassDeleteFunc(ctx, cl, log, scList, nsc)
	default:
		log.Debug(fmt.Sprintf("[runEventReconcile] StorageClass for NFSStorageClass %s should not be reconciled", nsc.Name))
	}
	log.Debug(fmt.Sprintf("[runEventReconcile] ends reconciliataion of StorageClass, name: %s, shouldRequeue: %t, err: %v", nsc.Name, shouldRequeue, err))

	if err != nil || shouldRequeue {
		return shouldRequeue, err
	}

	secretList := &corev1.SecretList{}
	err = cl.List(ctx, secretList, client.InNamespace(controllerNamespace))
	if err != nil {
		log.Error(err, "[runEventReconcile] unable to list Secrets")
		return true, err
	}

	reconcileTypeForSecret, err := identifyReconcileFuncForSecret(log, secretList, nsc, controllerNamespace)
	if err != nil {
		log.Error(err, fmt.Sprintf("[runEventReconcile] error occured while identifying the reconcile function for the Secret %q", SecretForMountOptionsPrefix+nsc.Name))
		return true, err
	}

	log.Debug(fmt.Sprintf("[runEventReconcile] reconcile operation for Secret %q: %q", SecretForMountOptionsPrefix+nsc.Name, reconcileTypeForSecret))
	switch reconcileTypeForSecret {
	case CreateReconcile:
		log.Debug(fmt.Sprintf("[runEventReconcile] CreateReconcile starts reconciliataion of Secret, name: %s", SecretForMountOptionsPrefix+nsc.Name))
		shouldRequeue, err = reconcileSecretCreateFunc(ctx, cl, log, nsc, controllerNamespace)
	case UpdateReconcile:
		log.Debug(fmt.Sprintf("[runEventReconcile] UpdateReconcile starts reconciliataion of Secret, name: %s", SecretForMountOptionsPrefix+nsc.Name))
		shouldRequeue, err = reconcileSecretUpdateFunc(ctx, cl, log, secretList, nsc, controllerNamespace)
	case DeleteReconcile:
		log.Debug(fmt.Sprintf("[runEventReconcile] DeleteReconcile starts reconciliataion of Secret, name: %s", SecretForMountOptionsPrefix+nsc.Name))
		shouldRequeue, err = reconcileSecretDeleteFunc(ctx, cl, log, secretList, nsc)
	default:
		log.Debug(fmt.Sprintf("[runEventReconcile] Secret %q should not be reconciled", SecretForMountOptionsPrefix+nsc.Name))
	}

	log.Debug(fmt.Sprintf("[runEventReconcile] ends reconciliataion of Secret, name: %s, shouldRequeue: %t, err: %v", SecretForMountOptionsPrefix+nsc.Name, shouldRequeue, err))

	if err != nil || shouldRequeue {
		return shouldRequeue, err
	}

	log.Debug(fmt.Sprintf("[runEventReconcile] Finish all reconciliations for NFSStorageClass %q. Update status", nsc.Name))
	err = updateNFSStorageClassPhase(ctx, cl, nsc, CreatedStatusPhase, "")
	if err != nil {
		log.Error(err, fmt.Sprintf("[runEventReconcile] unable to update the NFSStorageClass, name: %s", nsc.Name))
		return true, err
	}
	log.Debug(fmt.Sprintf("[runEventReconcile] successfully updated the NFSStorageClass %s status", nsc.Name))
	return false, nil

}

func identifyReconcileFuncForStorageClass(log logger.Logger, scList *v1.StorageClassList, nsc *v1alpha1.NFSStorageClass) (reconcileType string, err error) {
	if shouldReconcileByDeleteFunc(nsc) {
		return DeleteReconcile, nil
	}

	if shouldReconcileStorageClassByCreateFunc(scList, nsc) {
		return CreateReconcile, nil
	}

	should, err := shouldReconcileStorageClassByUpdateFunc(log, scList, nsc)
	if err != nil {
		return "", err
	}
	if should {
		return UpdateReconcile, nil
	}

	return "", nil
}

func shouldReconcileStorageClassByCreateFunc(scList *v1.StorageClassList, nsc *v1alpha1.NFSStorageClass) bool {
	if nsc.DeletionTimestamp != nil {
		return false
	}

	for _, sc := range scList.Items {
		if sc.Name == nsc.Name &&
			nsc.Status != nil {
			return false
		}
	}

	return true
}

func shouldReconcileStorageClassByUpdateFunc(log logger.Logger, scList *v1.StorageClassList, nsc *v1alpha1.NFSStorageClass) (bool, error) {
	if nsc.DeletionTimestamp != nil {
		return false, nil
	}

	for _, oldSC := range scList.Items {
		if oldSC.Name == nsc.Name {
			if oldSC.Provisioner == NFSStorageClassProvisioner {
				newSC := configureStorageClass(nsc)
				diff, err := GetSCDiff(&oldSC, newSC)
				if err != nil {
					return false, err
				}

				if diff != "" {
					log.Debug(fmt.Sprintf("[shouldReconcileStorageClassByUpdateFunc] a storage class %s should be updated. Diff: %s", oldSC.Name, diff))
					return true, nil
				}

				if nsc.Status.Phase == FailedStatusPhase {
					return true, nil
				}

				return false, nil

			} else {
				err := fmt.Errorf("a storage class %s does not belong to %s provisioner", oldSC.Name, NFSStorageClassProvisioner)
				return false, err
			}
		}
	}

	err := fmt.Errorf("a storage class %s does not exist", nsc.Name)
	return false, err
}

func shouldReconcileByDeleteFunc(nsc *v1alpha1.NFSStorageClass) bool {
	if nsc.DeletionTimestamp != nil {
		return true
	}

	return false
}

func reconcileStorageClassCreateFunc(
	ctx context.Context,
	cl client.Client,
	log logger.Logger,
	scList *v1.StorageClassList,
	nsc *v1alpha1.NFSStorageClass,
) (bool, error) {
	log.Debug(fmt.Sprintf("[reconcileStorageClassCreateFunc] starts for NFSStorageClass %q", nsc.Name))
	added, err := addFinalizerIfNotExistsForNSC(ctx, cl, nsc)
	if err != nil {
		log.Error(err, fmt.Sprintf("[reconcileStorageClassCreateFunc] unable to add a finalizer %s to the NFSStorageClass %s", NFSStorageClassFinalizerName, nsc.Name))
		return true, err
	}
	log.Debug(fmt.Sprintf("[reconcileStorageClassCreateFunc] finalizer %s was added to the NFSStorageClass %s: %t", NFSStorageClassFinalizerName, nsc.Name, added))

	log.Debug(fmt.Sprintf("[reconcileStorageClassCreateFunc] starts storage class configuration for the NFSStorageClass, name: %s", nsc.Name))
	newSC := configureStorageClass(nsc)
	log.Debug(fmt.Sprintf("[reconcileStorageClassCreateFunc] successfully configurated storage class for the NFSStorageClass, name: %s", nsc.Name))
	log.Trace(fmt.Sprintf("[reconcileStorageClassCreateFunc] storage class: %+v", newSC))

	created, err := createStorageClassIfNotExists(ctx, cl, scList, newSC)
	if err != nil {
		log.Error(err, fmt.Sprintf("[reconcileStorageClassCreateFunc] unable to create a Storage Class, name: %s", newSC.Name))
		upError := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, err.Error())
		if upError != nil {
			log.Error(upError, fmt.Sprintf("[reconcileStorageClassCreateFunc] unable to update the NFSStorageClass %s", nsc.Name))
			return true, upError
		}
		return true, err
	}
	log.Debug(fmt.Sprintf("[reconcileStorageClassCreateFunc] a storage class %s was created: %t", newSC.Name, created))
	if created {
		log.Info(fmt.Sprintf("[reconcileStorageClassCreateFunc] successfully create storage class, name: %s", newSC.Name))
	} else {
		log.Info(fmt.Sprintf("[reconcileStorageClassCreateFunc] a storage class %s already exists", newSC.Name))
		diff := ""
		for _, oldSC := range scList.Items {
			if oldSC.Name == newSC.Name {
				diff, err = GetSCDiff(&oldSC, newSC)
				break
			}
		}
		if err != nil {
			log.Error(err, fmt.Sprintf("[reconcileStorageClassCreateFunc] Error occured while identifying the difference between the existed StorageClass %s and the new one", newSC.Name))
			upError := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, err.Error())
			if upError != nil {
				log.Error(upError, fmt.Sprintf("[reconcileStorageClassCreateFunc] unable to update the NFSStorageClass %s", nsc.Name))
			}
			return true, err
		}
		if diff != "" {
			log.Info(fmt.Sprintf("[reconcileStorageClassCreateFunc] current Storage Class %s differs from the NFSStorageClass one. The Storage Class will be recreated", newSC.Name))
			err := recreateStorageClass(ctx, cl, newSC)
			if err != nil {
				log.Error(err, fmt.Sprintf("[reconcileStorageClassCreateFunc] unable to recreate a Storage Class %s", newSC.Name))
				upError := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, err.Error())
				if upError != nil {
					log.Error(upError, fmt.Sprintf("[reconcileStorageClassCreateFunc] unable to update the NFSStorageClass %s", nsc.Name))
				}
				return true, err
			}
			log.Info(fmt.Sprintf("[reconcileStorageClassCreateFunc] a Storage Class %s was successfully recreated", newSC.Name))
		} else {
			log.Info(fmt.Sprintf("[reconcileStorageClassCreateFunc] the Storage Class %s is up-to-date", newSC.Name))
		}
	}

	return false, nil
}

func reconcileStorageClassUpdateFunc(
	ctx context.Context,
	cl client.Client,
	log logger.Logger,
	scList *v1.StorageClassList,
	nsc *v1alpha1.NFSStorageClass,
) (bool, error) {

	log.Debug(fmt.Sprintf("[reconcileStorageClassUpdateFunc] starts for NFSStorageClass %q", nsc.Name))

	var oldSC *v1.StorageClass
	for _, s := range scList.Items {
		if s.Name == nsc.Name {
			oldSC = &s
			break
		}
	}

	if oldSC == nil {
		err := fmt.Errorf("a storage class %s does not exist", nsc.Name)
		log.Error(err, fmt.Sprintf("[reconcileStorageClassUpdateFunc] unable to find a storage class for the NFSStorageClass, name: %s", nsc.Name))
		upError := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, err.Error())
		if upError != nil {
			log.Error(upError, fmt.Sprintf("[reconcileStorageClassUpdateFunc] unable to update the NFSStorageClass %s", nsc.Name))
		}
		return true, err
	}

	log.Debug(fmt.Sprintf("[reconcileStorageClassUpdateFunc] successfully found a storage class for the NFSStorageClass, name: %s", nsc.Name))

	log.Trace(fmt.Sprintf("[reconcileStorageClassUpdateFunc] storage class: %+v", oldSC))
	newSC := configureStorageClass(nsc)
	diff, err := GetSCDiff(oldSC, newSC)
	if err != nil {
		log.Error(err, fmt.Sprintf("[reconcileStorageClassCreateFunc] Error occured while identifying the difference between the existed StorageClass %s and the new one", newSC.Name))
		upError := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, err.Error())
		if upError != nil {
			log.Error(upError, fmt.Sprintf("[reconcileStorageClassUpdateFunc] unable to update the NFSStorageClass %s", nsc.Name))
		}
		return true, err
	}

	if diff != "" {
		log.Info(fmt.Sprintf("[reconcileStorageClassUpdateFunc] current Storage Class LVMVolumeGroups do not match NFSStorageClass ones. The Storage Class %s will be recreated with new ones", nsc.Name))

		err = recreateStorageClass(ctx, cl, newSC)
		if err != nil {
			log.Error(err, fmt.Sprintf("[reconcileStorageClassUpdateFunc] unable to recreate a Storage Class %s", newSC.Name))
			upError := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, err.Error())
			if upError != nil {
				log.Error(upError, fmt.Sprintf("[reconcileStorageClassUpdateFunc] unable to update the NFSStorageClass %s", nsc.Name))
			}
			return true, err
		}

		log.Info(fmt.Sprintf("[reconcileStorageClassUpdateFunc] a Storage Class %s was successfully recreated", newSC.Name))
	}

	return false, nil
}

func reconcileStorageClassDeleteFunc(
	ctx context.Context,
	cl client.Client,
	log logger.Logger,
	scList *v1.StorageClassList,
	nsc *v1alpha1.NFSStorageClass,
) (bool, error) {
	log.Debug(fmt.Sprintf("[reconcileStorageClassDeleteFunc] tries to find a storage class for the NFSStorageClass %s", nsc.Name))
	var sc *v1.StorageClass
	for _, s := range scList.Items {
		if s.Name == nsc.Name {
			sc = &s
			break
		}
	}
	if sc == nil {
		log.Info(fmt.Sprintf("[reconcileStorageClassDeleteFunc] no storage class found for the NFSStorageClass, name: %s", nsc.Name))
	}

	if sc != nil {
		log.Info(fmt.Sprintf("[reconcileStorageClassDeleteFunc] successfully found a storage class for the NFSStorageClass %s", nsc.Name))
		log.Debug(fmt.Sprintf("[reconcileStorageClassDeleteFunc] starts identifing a provisioner for the storage class %s", sc.Name))

		if sc.Provisioner != NFSStorageClassProvisioner {
			log.Info(fmt.Sprintf("[reconcileStorageClassDeleteFunc] the storage class %s does not belongs to %s provisioner. It will not be deleted", sc.Name, NFSStorageClassProvisioner))
		} else {
			log.Info(fmt.Sprintf("[reconcileStorageClassDeleteFunc] the storage class %s belongs to %s provisioner. It will be deleted", sc.Name, NFSStorageClassProvisioner))

			err := deleteStorageClass(ctx, cl, sc)
			if err != nil {
				log.Error(err, fmt.Sprintf("[reconcileStorageClassDeleteFunc] unable to delete a storage class, name: %s", sc.Name))
				upErr := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, fmt.Sprintf("Unable to delete a storage class, err: %s", err.Error()))
				if upErr != nil {
					log.Error(upErr, fmt.Sprintf("[reconcileStorageClassDeleteFunc] unable to update the NFSStorageClass, name: %s", nsc.Name))
				}
				return true, err
			}
			log.Info(fmt.Sprintf("[reconcileStorageClassDeleteFunc] successfully deleted a storage class, name: %s", sc.Name))
		}
	}

	log.Debug("[reconcileStorageClassDeleteFunc] ends the reconciliation")
	return false, nil
}

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

func GetSCDiff(oldSC, newSC *v1.StorageClass) (string, error) {

	if oldSC.Provisioner != newSC.Provisioner {
		err := fmt.Errorf("NFSStorageClass %q: the provisioner field is different in the StorageClass %q", newSC.Name, oldSC.Name)
		return "", err
	}

	if oldSC.ReclaimPolicy != newSC.ReclaimPolicy {
		diff := fmt.Sprintf("ReclaimPolicy: %q -> %q", *oldSC.ReclaimPolicy, *newSC.ReclaimPolicy)
		return diff, nil
	}

	if *oldSC.VolumeBindingMode != *newSC.VolumeBindingMode {
		diff := fmt.Sprintf("VolumeBindingMode: %q -> %q", *oldSC.VolumeBindingMode, *newSC.VolumeBindingMode)
		return diff, nil
	}

	if *oldSC.AllowVolumeExpansion != *newSC.AllowVolumeExpansion {
		diff := fmt.Sprintf("AllowVolumeExpansion: %t -> %t", *oldSC.AllowVolumeExpansion, *newSC.AllowVolumeExpansion)
		return diff, nil
	}

	if !reflect.DeepEqual(oldSC.Parameters, newSC.Parameters) {
		diff := fmt.Sprintf("Parameters: %+v -> %+v", oldSC.Parameters, newSC.Parameters)
		return diff, nil
	}

	if !reflect.DeepEqual(oldSC.MountOptions, newSC.MountOptions) {
		diff := fmt.Sprintf("MountOptions: %v -> %v", oldSC.MountOptions, newSC.MountOptions)
		return diff, nil
	}

	return "", nil
}

func createStorageClassIfNotExists(ctx context.Context, cl client.Client, scList *v1.StorageClassList, sc *v1.StorageClass) (bool, error) {
	for _, s := range scList.Items {
		if s.Name == sc.Name {
			return false, nil
		}
	}

	err := cl.Create(ctx, sc)
	if err != nil {
		return false, err
	}

	return true, err
}

func addFinalizerIfNotExistsForNSC(ctx context.Context, cl client.Client, nsc *v1alpha1.NFSStorageClass) (bool, error) {
	if !slices.Contains(nsc.Finalizers, NFSStorageClassFinalizerName) {
		nsc.Finalizers = append(nsc.Finalizers, NFSStorageClassFinalizerName)
	}

	err := cl.Update(ctx, nsc)
	if err != nil {
		return false, err
	}

	return true, nil
}

func configureStorageClass(nsc *v1alpha1.NFSStorageClass) *v1.StorageClass {
	reclaimPolicy := corev1.PersistentVolumeReclaimPolicy(nsc.Spec.ReclaimPolicy)
	volumeBindingMode := v1.VolumeBindingMode(nsc.Spec.VolumeBindingMode)
	AllowVolumeExpansion := AllowVolumeExpansionDefaultValue

	sc := &v1.StorageClass{
		TypeMeta: metav1.TypeMeta{
			Kind:       StorageClassKind,
			APIVersion: StorageClassAPIVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       nsc.Name,
			Namespace:  nsc.Namespace,
			Finalizers: []string{NFSStorageClassFinalizerName},
		},
		Parameters:           GetSCParams(nsc),
		MountOptions:         GetSCMountOptions(nsc),
		Provisioner:          NFSStorageClassProvisioner,
		ReclaimPolicy:        &reclaimPolicy,
		VolumeBindingMode:    &volumeBindingMode,
		AllowVolumeExpansion: &AllowVolumeExpansion,
	}

	return sc
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

func recreateStorageClass(ctx context.Context, cl client.Client, sc *v1.StorageClass) error {
	err := deleteStorageClass(ctx, cl, sc)
	if err != nil {
		return err
	}

	err = cl.Create(ctx, sc)
	if err != nil {
		return err
	}

	return nil
}

func deleteStorageClass(ctx context.Context, cl client.Client, sc *v1.StorageClass) error {
	if sc.Provisioner != NFSStorageClassProvisioner {
		return fmt.Errorf("a storage class %s does not belong to %s provisioner", sc.Name, NFSStorageClassProvisioner)
	}

	_, err := removeFinalizerIfExists(ctx, cl, sc, NFSStorageClassFinalizerName)
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

	if nsc.Spec.MountOptions.MountMode != "" {
		mountOptions = append(mountOptions, nsc.Spec.MountOptions.MountMode)
	}

	if nsc.Spec.MountOptions.Timeout > 0 {
		mountOptions = append(mountOptions, "timeo="+strconv.Itoa(nsc.Spec.MountOptions.Timeout))
	}

	if nsc.Spec.MountOptions.Retransmissions > 0 {
		mountOptions = append(mountOptions, "retrans="+strconv.Itoa(nsc.Spec.MountOptions.Retransmissions))
	}

	if nsc.Spec.MountOptions.ReadOnly {
		mountOptions = append(mountOptions, "ro")
	} else {
		mountOptions = append(mountOptions, "rw")
	}

	return mountOptions
}

func GetSCParams(nsc *v1alpha1.NFSStorageClass) map[string]string {
	params := make(map[string]string)

	params[serverParamKey] = nsc.Spec.Connection.Host
	params[shareParamKey] = nsc.Spec.Connection.Share

	if nsc.Spec.ChmodPermissions != "" {
		params[mountPermissionsParamKey] = nsc.Spec.ChmodPermissions
	}

	return params
}

func identifyReconcileFuncForSecret(log logger.Logger, secretList *corev1.SecretList, nsc *v1alpha1.NFSStorageClass, controllerNamespace string) (reconcileType string, err error) {
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
			if !reflect.DeepEqual(oldSecret.Data, newSecret.Data) {
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
			Finalizers: []string{NFSStorageClassFinalizerName},
		},
		StringData: map[string]string{
			mountOptionsSecretKey: strings.Join(mountOptions, ","),
		},
	}

	return secret
}

func reconcileSecretCreateFunc(ctx context.Context, cl client.Client, log logger.Logger, nsc *v1alpha1.NFSStorageClass, controllerNamespace string) (bool, error) {
	log.Debug(fmt.Sprintf("[reconcileSecretCreateFunc] starts for NFSStorageClass %q", nsc.Name))

	newSecret := configureSecret(nsc, controllerNamespace)
	log.Debug(fmt.Sprintf("[reconcileSecretCreateFunc] successfully configurated secret for the NFSStorageClass, name: %s", nsc.Name))
	log.Trace(fmt.Sprintf("[reconcileSecretCreateFunc] secret: %+v", newSecret))

	err := cl.Create(ctx, newSecret)
	if err != nil {
		log.Error(err, fmt.Sprintf("[reconcileSecretCreateFunc] unable to create a Secret, name: %s", newSecret.Name))
		upError := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, err.Error())
		if upError != nil {
			log.Error(upError, fmt.Sprintf("[reconcileSecretCreateFunc] unable to update the NFSStorageClass %s", nsc.Name))
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
		err := fmt.Errorf("a secret %s does not exist", SecretForMountOptionsPrefix+nsc.Name)
		log.Error(err, fmt.Sprintf("[reconcileSecretUpdateFunc] unable to find a secret for the NFSStorageClass, name: %s", nsc.Name))
		upError := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, err.Error())
		if upError != nil {
			log.Error(upError, fmt.Sprintf("[reconcileSecretUpdateFunc] unable to update the NFSStorageClass %s", nsc.Name))
		}
		return true, err
	}

	log.Debug(fmt.Sprintf("[reconcileSecretUpdateFunc] successfully found a secret %q for the NFSStorageClass, name: %q", oldSecret.Name, nsc.Name))

	newSecret := configureSecret(nsc, controllerNamespace)

	log.Trace(fmt.Sprintf("[reconcileSecretUpdateFunc] old secret: %+v", oldSecret))
	log.Trace(fmt.Sprintf("[reconcileSecretUpdateFunc] new secret: %+v", newSecret))

	err := cl.Update(ctx, newSecret)
	if err != nil {
		log.Error(err, fmt.Sprintf("[reconcileSecretUpdateFunc] unable to update a Secret %s", newSecret.Name))
		upError := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, err.Error())
		if upError != nil {
			log.Error(upError, fmt.Sprintf("[reconcileSecretUpdateFunc] unable to update the NFSStorageClass %s", nsc.Name))
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
		log.Debug(fmt.Sprintf("[reconcileSecretDeleteFunc] starts removing a finalizer %s from the Secret, name: %s", NFSStorageClassFinalizerName, secret.Name))
		_, err := removeFinalizerIfExists(ctx, cl, secret, NFSStorageClassFinalizerName)
		if err != nil {
			log.Error(err, fmt.Sprintf("[reconcileSecretDeleteFunc] unable to remove a finalizer %s from the Secret, name: %s", NFSStorageClassFinalizerName, secret.Name))
			upErr := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, fmt.Sprintf("Unable to remove a finalizer, err: %s", err.Error()))
			if upErr != nil {
				log.Error(upErr, fmt.Sprintf("[reconcileSecretDeleteFunc] unable to update the NFSStorageClass, name: %s", nsc.Name))
			}
			return true, err
		}

		err = cl.Delete(ctx, secret)
		if err != nil {
			log.Error(err, fmt.Sprintf("[reconcileSecretDeleteFunc] unable to delete a secret, name: %s", secret.Name))
			upErr := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, fmt.Sprintf("Unable to delete a secret, err: %s", err.Error()))
			if upErr != nil {
				log.Error(upErr, fmt.Sprintf("[reconcileSecretDeleteFunc] unable to update the NFSStorageClass, name: %s", nsc.Name))
			}
			return true, err
		}
	}

	log.Info(fmt.Sprintf("[reconcileSecretDeleteFunc] ends the reconciliation for Secret %q", SecretForMountOptionsPrefix+nsc.Name))

	log.Debug(fmt.Sprintf("[reconcileSecretDeleteFunc] starts removing a finalizer %s from the NFSStorageClass, name: %s", NFSStorageClassFinalizerName, nsc.Name))
	removed, err := removeFinalizerIfExists(ctx, cl, nsc, NFSStorageClassFinalizerName)
	if err != nil {
		log.Error(err, fmt.Sprintf("[reconcileSecretDeleteFunc] unable to remove a finalizer %s from the NFSStorageClass, name: %s", NFSStorageClassFinalizerName, nsc.Name))
		upErr := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, fmt.Sprintf("Unable to remove a finalizer, err: %s", err.Error()))
		if upErr != nil {
			log.Error(upErr, fmt.Sprintf("[reconcileSecretDeleteFunc] unable to update the NFSStorageClass, name: %s", nsc.Name))
		}
		return true, err
	}
	log.Debug(fmt.Sprintf("[reconcileSecretDeleteFunc] the NFSStorageClass %s finalizer %s was removed: %t", nsc.Name, NFSStorageClassFinalizerName, removed))

	return false, nil
}
