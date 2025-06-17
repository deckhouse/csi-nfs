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
	"reflect"
	"time"

	storagev1 "k8s.io/api/storage/v1"
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

	v1alpha1 "github.com/deckhouse/csi-nfs/api/v1alpha1"
	mc "github.com/deckhouse/deckhouse/deckhouse-controller/pkg/apis/deckhouse.io/v1alpha1"
	"github.com/deckhouse/csi-nfs/images/controller/pkg/config"
	"github.com/deckhouse/csi-nfs/images/controller/pkg/logger"
	commonvalidating "github.com/deckhouse/csi-nfs/lib/go/common/pkg/validating"
)

const (
	ModuleConfigCtrlName          = "module-config-csi-nfs-controller"
	doesNotMatchModuleConfigLabel = "storage.deckhouse.io/does-not-match-moduleconfig"
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
			mc := &mc.ModuleConfig{}
			err := cl.Get(ctx, request.NamespacedName, mc)
			if err != nil && !k8serr.IsNotFound(err) {
				log.Error(err, fmt.Sprintf("[ModuleConfigReconciler] unable to get ModuleConfig, name: %s", request.Name))
				return reconcile.Result{}, err
			}

			if mc.Name == "" {
				log.Info(fmt.Sprintf("[ModuleConfigReconciler] seems like the ModuleConfig for the request %s was deleted. Reconcile retrying will stop.", request.Name))
				return reconcile.Result{}, nil
			}

			if mc.DeletionTimestamp != nil {
				log.Debug(fmt.Sprintf("[ModuleConfigReconciler] reconcile operation for ModuleConfig %s: Delete", mc.Name))
			} else {
				nscList := &v1alpha1.NFSStorageClassList{}
				err = cl.List(ctx, nscList)
				if err != nil {
					log.Error(err, "[ModuleConfigReconciler] unable to list NFSStorage Classes")
					return reconcile.Result{}, err
				}

				alertMap := validateModuleConfig(log, mc, nscList)

				scList := &storagev1.StorageClassList{}
				err = cl.List(ctx, scList)
				if err != nil {
					log.Error(err, "[ModuleConfigReconciler] unable to list Storage Classes")
					return reconcile.Result{}, err
				}

				shouldRequeue, err := RunModuleConfigEventReconcile(ctx, cl, log, nscList, alertMap, scList)
				if err != nil {
					log.Error(err, fmt.Sprintf("[ModuleConfigReconciler] an error occurred while reconciles the ModuleConfig, name: %s", mc.Name))
				}

				if shouldRequeue {
					log.Warning(fmt.Sprintf("[ModuleConfigReconciler] Reconciler will requeue the request, name: %s", request.Name))
					return reconcile.Result{
						RequeueAfter: cfg.RequeueModuleConfigInterval * time.Second,
					}, nil
				}
			}

			log.Info(fmt.Sprintf("[ModuleConfigReconciler] ends Reconcile for the ModuleConfig %q", request.Name))
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
			&mc.ModuleConfig{},
			handler.TypedFuncs[*mc.ModuleConfig, reconcile.Request]{
				CreateFunc: func(
					_ context.Context,
					e event.TypedCreateEvent[*mc.ModuleConfig],
					q workqueue.TypedRateLimitingInterface[reconcile.Request],
				) {
					// we only process our ModuleConfig
					if e.Object.GetName() != cfg.CsiNfsModuleName {
						return
					}

					log.Info(fmt.Sprintf("[CreateFunc] get event for ModuleConfig %q. Add to the queue", e.Object.GetName()))
					request := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: e.Object.GetNamespace(), Name: e.Object.GetName()}}
					q.Add(request)
				},
				UpdateFunc: func(
					_ context.Context,
					e event.TypedUpdateEvent[*mc.ModuleConfig],
					q workqueue.TypedRateLimitingInterface[reconcile.Request],
				) {
					// we only process our ModuleConfig
					if e.ObjectNew.GetName() != cfg.CsiNfsModuleName {
						return
					}

					log.Info(fmt.Sprintf("[UpdateFunc] get event for ModuleConfig %q. Check if it should be reconciled", e.ObjectNew.GetName()))

					oldMC := e.ObjectOld
					newMC := e.ObjectNew

					if reflect.DeepEqual(oldMC.Spec, newMC.Spec) && newMC.DeletionTimestamp == nil {
						log.Info(fmt.Sprintf("[UpdateFunc] an update event for the ModuleConfig %s has no Spec field updates. It will not be reconciled", newMC.Name))
						return
					}

					log.Info(fmt.Sprintf("[UpdateFunc] the ModuleConfig %q will be reconciled. Add to the queue", newMC.Name))
					request := reconcile.Request{NamespacedName: types.NamespacedName{Namespace: newMC.Namespace, Name: newMC.Name}}
					q.Add(request)
				},
			},
		),
	)
	if err != nil {
		log.Error(err, "[RunModuleConfigWatcherController] unable to watch the events")
		return nil, err
	}

	return c, nil
}

func RunModuleConfigEventReconcile(
	ctx context.Context,
	cl client.Client,
	log logger.Logger,
	nscList *v1alpha1.NFSStorageClassList,
	alertMap map[string]string,
	scList *storagev1.StorageClassList,
) (shouldRequeue bool, err error) {
	// working with labels
	for _, nsc := range nscList.Items {
		var sc *storagev1.StorageClass

		for _, s := range scList.Items {
			if s.Name == nsc.Name {
				sc = &s
				break
			}
		}

		if sc == nil {
			err = fmt.Errorf("[RunModuleConfigEventReconcile] no storage class found for the NFSStorageClass, name: %s", nsc.Name)
			return true, err
		}

		var action string
		if _, ok := alertMap[sc.Name]; !ok {
			action = "deleted"

			if sc.Labels == nil {
				continue
			}

			if _, ok := sc.Labels[doesNotMatchModuleConfigLabel]; !ok {
				continue
			}

			delete(sc.Labels, doesNotMatchModuleConfigLabel)
		} else {
			action = "added"

			if sc.Labels == nil {
				sc.Labels = make(map[string]string, 1)
			}

			if _, ok := sc.Labels[doesNotMatchModuleConfigLabel]; ok {
				continue
			}

			sc.Labels[doesNotMatchModuleConfigLabel] = alertMap[sc.Name]
		}

		if err := cl.Update(ctx, sc); err != nil {
			err = fmt.Errorf("[RunModuleConfigEventReconcile] unable to update the StorageClass %s: %w", sc.Name, err)
			return true, err
		}
		log.Debug(fmt.Sprintf("[RunModuleConfigEventReconcile] successfully %s the label '%s' to the StorageClass %s", action, doesNotMatchModuleConfigLabel, sc.Name))
	}

	return false, nil
}

func validateModuleConfig(log logger.Logger, mc *mc.ModuleConfig, nscList *v1alpha1.NFSStorageClassList) map[string]string {
	alertMap := make(map[string]string)
	for _, nsc := range nscList.Items {
		if err := commonvalidating.ValidateNFSStorageClass(mc, &nsc); err != nil {
			log.Warning(fmt.Sprintf("[validateModuleConfig] invalid NFSStorageClass (%v)", err))
			alertMap[nsc.Name] = "true"
		}
	}

	return alertMap
}
