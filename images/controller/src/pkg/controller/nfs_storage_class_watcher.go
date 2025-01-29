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
	"errors"
	"fmt"
	"reflect"
	"time"

	"d8-controller/pkg/config"
	"d8-controller/pkg/logger"
	v1alpha1 "github.com/deckhouse/csi-nfs/api/v1alpha1"
	commonvalidating "github.com/deckhouse/csi-nfs/lib/go/common/pkg/validating"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/storage/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	NFSStorageClassCtrlName = "nfs-storage-class-controller"

	StorageClassKind       = "StorageClass"
	StorageClassAPIVersion = "storage.k8s.io/v1"

	NFSStorageClassProvisioner = "nfs.csi.k8s.io"

	NFSStorageClassControllerFinalizerName = "storage.deckhouse.io/nfs-storage-class-controller"
	NFSStorageClassManagedLabelKey         = "storage.deckhouse.io/managed-by"
	NFSStorageClassManagedLabelValue       = "nfs-storage-class-controller"

	StorageClassDefaultAnnotationKey     = "storageclass.kubernetes.io/is-default-class"
	StorageClassDefaultAnnotationValTrue = "true"

	AllowVolumeExpansionDefaultValue = true

	FailedStatusPhase  = "Failed"
	CreatedStatusPhase = "Created"

	CreateReconcile = "Create"
	UpdateReconcile = "Update"
	DeleteReconcile = "Delete"

	serverParamKey           = "server"
	shareParamKey            = "share"
	MountPermissionsParamKey = "mountPermissions"
	SubDirParamKey           = "subdir"
	MountOptionsSecretKey    = "mountOptions"

	SecretForMountOptionsPrefix = "nfs-mount-options-for-"
	StorageClassSecretNameKey   = "csi.storage.k8s.io/provisioner-secret-name"
	StorageClassSecretNSKey     = "csi.storage.k8s.io/provisioner-secret-namespace"
)

var (
	allowedProvisioners = []string{NFSStorageClassProvisioner}
)

func RunNFSStorageClassWatcherController(
	mgr manager.Manager,
	cfg config.Options,
	log logger.Logger,
) (controller.Controller, error) {
	cl := mgr.GetClient()

	c, err := controller.New(NFSStorageClassCtrlName, mgr, controller.Options{
		Reconciler: reconcile.Func(func(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
			log.Info(fmt.Sprintf("[NFSStorageClassReconciler] starts Reconcile for the NFSStorageClass %q", request.Name))
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

			if nsc.DeletionTimestamp == nil {
				nfsModuleConfig := &v1alpha1.ModuleConfig{}
				err := cl.Get(ctx, types.NamespacedName{Name: cfg.CsiNfsModuleName, Namespace: ""}, nfsModuleConfig)
				if err != nil {
					log.Error(err, fmt.Sprintf("[NFSStorageClassReconciler] unable to get ModuleConfig, name: %s", cfg.CsiNfsModuleName))
					return reconcile.Result{}, err
				}

				if err := commonvalidating.ValidateNFSStorageClass(nfsModuleConfig, nsc); err != nil {
					log.Error(err, "[NFSStorageClassReconciler] invalid NFSStorageClass")
					return reconcile.Result{}, err
				}
			}

			scList := &v1.StorageClassList{}
			err = cl.List(ctx, scList)
			if err != nil {
				log.Error(err, "[NFSStorageClassReconciler] unable to list Storage Classes")
				return reconcile.Result{}, err
			}

			shouldRequeue, err := RunEventReconcile(ctx, cl, log, scList, nsc, cfg.ControllerNamespace)
			if err != nil {
				log.Error(err, fmt.Sprintf("[NFSStorageClassReconciler] an error occurred while reconciles the NFSStorageClass, name: %s", nsc.Name))
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

	err = c.Watch(
		source.Kind(
			mgr.GetCache(),
			&v1alpha1.NFSStorageClass{},
			handler.TypedFuncs[*v1alpha1.NFSStorageClass, reconcile.Request]{
				CreateFunc: func(
					_ context.Context,
					e event.TypedCreateEvent[*v1alpha1.NFSStorageClass],
					q workqueue.TypedRateLimitingInterface[reconcile.Request],
				) {
					log.Info(fmt.Sprintf("[CreateFunc] get event for NFSStorageClass %q. Add to the queue", e.Object.GetName()))
					request := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: e.Object.GetNamespace(), Name: e.Object.GetName()}}
					q.Add(request)
				},
				UpdateFunc: func(
					_ context.Context,
					e event.TypedUpdateEvent[*v1alpha1.NFSStorageClass],
					q workqueue.TypedRateLimitingInterface[reconcile.Request],
				) {
					log.Info(fmt.Sprintf("[UpdateFunc] get event for NFSStorageClass %q. Check if it should be reconciled", e.ObjectNew.GetName()))

					oldNSC := e.ObjectOld
					newNSC := e.ObjectNew

					if reflect.DeepEqual(oldNSC.Spec, newNSC.Spec) && newNSC.DeletionTimestamp == nil {
						log.Info(fmt.Sprintf("[UpdateFunc] an update event for the NFSStorageClass %s has no Spec field updates. It will not be reconciled", newNSC.Name))
						return
					}

					log.Info(fmt.Sprintf("[UpdateFunc] the NFSStorageClass %q will be reconciled. Add to the queue", newNSC.Name))
					request := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: newNSC.Namespace, Name: newNSC.Name}}
					q.Add(request)
				},
			},
		),
	)
	if err != nil {
		log.Error(err, "[RunNFSStorageClassWatcherController] unable to watch the events")
		return nil, err
	}

	return c, nil
}

func RunEventReconcile(ctx context.Context, cl client.Client, log logger.Logger, scList *v1.StorageClassList, nsc *v1alpha1.NFSStorageClass, controllerNamespace string) (shouldRequeue bool, err error) {
	added, err := addFinalizerIfNotExists(ctx, cl, nsc, NFSStorageClassControllerFinalizerName)
	if err != nil {
		err = fmt.Errorf("[reconcileStorageClassCreateFunc] unable to add a finalizer %s to the NFSStorageClass %s: %w", NFSStorageClassControllerFinalizerName, nsc.Name, err)
		return true, err
	}
	log.Debug(fmt.Sprintf("[reconcileStorageClassCreateFunc] finalizer %s was added to the NFSStorageClass %s: %t", NFSStorageClassControllerFinalizerName, nsc.Name, added))

	reconcileTypeForStorageClass, err := IdentifyReconcileFuncForStorageClass(log, scList, nsc, controllerNamespace)
	if err != nil {
		err = fmt.Errorf("[runEventReconcile] error occurred while identifying the reconcile function for StorageClass %s: %w", nsc.Name, err)
		return true, err
	}

	shouldRequeue = false
	log.Debug(fmt.Sprintf("[runEventReconcile] reconcile operation for StorageClass %q: %q", nsc.Name, reconcileTypeForStorageClass))
	switch reconcileTypeForStorageClass {
	case CreateReconcile:
		log.Debug(fmt.Sprintf("[runEventReconcile] CreateReconcile starts reconciliataion of StorageClass, name: %s", nsc.Name))
		shouldRequeue, err = ReconcileStorageClassCreateFunc(ctx, cl, log, scList, nsc, controllerNamespace)
	case UpdateReconcile:
		log.Debug(fmt.Sprintf("[runEventReconcile] UpdateReconcile starts reconciliataion of StorageClass, name: %s", nsc.Name))
		shouldRequeue, err = reconcileStorageClassUpdateFunc(ctx, cl, log, scList, nsc, controllerNamespace)
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
		err = fmt.Errorf("[runEventReconcile] unable to list Secrets: %w", err)
		upError := updateNFSStorageClassPhase(ctx, cl, nsc, FailedStatusPhase, err.Error())
		if upError != nil {
			upError = fmt.Errorf("[reconcileStorageClassCreateFunc] unable to update the NFSStorageClass %s: %w", nsc.Name, upError)
			err = errors.Join(err, upError)
		}
		return true, err
	}

	reconcileTypeForSecret, err := IdentifyReconcileFuncForSecret(log, secretList, nsc, controllerNamespace)
	if err != nil {
		log.Error(err, fmt.Sprintf("[runEventReconcile] error occurred while identifying the reconcile function for the Secret %q", SecretForMountOptionsPrefix+nsc.Name))
		return true, err
	}

	log.Debug(fmt.Sprintf("[runEventReconcile] reconcile operation for Secret %q: %q", SecretForMountOptionsPrefix+nsc.Name, reconcileTypeForSecret))
	switch reconcileTypeForSecret {
	case CreateReconcile:
		log.Debug(fmt.Sprintf("[runEventReconcile] CreateReconcile starts reconciliataion of Secret, name: %s", SecretForMountOptionsPrefix+nsc.Name))
		shouldRequeue, err = ReconcileSecretCreateFunc(ctx, cl, log, nsc, controllerNamespace)
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

	log.Debug(fmt.Sprintf("[runEventReconcile] Finish all reconciliations for NFSStorageClass %q.", nsc.Name))

	if reconcileTypeForSecret != DeleteReconcile {
		err = updateNFSStorageClassPhase(ctx, cl, nsc, CreatedStatusPhase, "")
		if err != nil {
			err = fmt.Errorf("[runEventReconcile] unable to update the NFSStorageClass %s: %w", nsc.Name, err)
			return true, err
		}
		log.Debug(fmt.Sprintf("[runEventReconcile] successfully updated the NFSStorageClass %s status", nsc.Name))
	}

	return false, nil
}
