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

package handlers

import (
	"context"
	"fmt"

	"github.com/slok/kubewebhook/v2/pkg/model"
	kwhvalidating "github.com/slok/kubewebhook/v2/pkg/webhook/validating"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/resource/v1alpha3"
	sv1 "k8s.io/api/storage/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cn "github.com/deckhouse/csi-nfs/api/v1alpha1"
	commonvalidating "github.com/deckhouse/csi-nfs/lib/go/common/pkg/validating"
	d8commonapi "github.com/deckhouse/sds-common-lib/api/v1alpha1"
	"github.com/deckhouse/sds-common-lib/kubeclient"
	"github.com/deckhouse/sds-common-lib/logger"
)

const (
	csiNfsModuleName = "csi-nfs"
)

func NSCValidate(ctx context.Context, arReview *model.AdmissionReview, obj metav1.Object) (*kwhvalidating.ValidatorResult, error) {
	nsc, ok := obj.(*cn.NFSStorageClass)
	if !ok {
		// If not a storage class just continue the validation chain(if there is one) and do nothing.
		return &kwhvalidating.ValidatorResult{}, nil
	}

	if nsc.DeletionTimestamp != nil || arReview.Operation == "delete" {
		return &kwhvalidating.ValidatorResult{Valid: true}, nil
	}

	cl, err := kubeclient.New(d8commonapi.AddToScheme,
		v1alpha3.AddToScheme,
		cn.AddToScheme,
		clientgoscheme.AddToScheme,
		extv1.AddToScheme,
		v1.AddToScheme,
		sv1.AddToScheme,
	)
	if err != nil {
		handlerLog.Fatal("unable to create the kube client", logger.Err(err)) // pod restarting
	}

	// The controller names the StorageClass after the NFSStorageClass, so a
	// name held by a StorageClass of another provisioner could only ever leave
	// the class Failed.
	if arReview.Operation == model.OperationCreate {
		sc, err := foreignStorageClass(ctx, cl, nsc.Name)
		if err != nil {
			handlerLog.Error("unable to check for a foreign StorageClass", logger.Err(err))
			return nil, err
		}
		if sc != nil {
			return &kwhvalidating.ValidatorResult{
				Valid:   false,
				Message: fmt.Sprintf("StorageClass %q already exists with provisioner %q; choose a different NFSStorageClass name", sc.Name, sc.Provisioner),
			}, nil
		}
	}

	nfsModuleConfig := &d8commonapi.ModuleConfig{}
	err = cl.Get(ctx, types.NamespacedName{Name: csiNfsModuleName, Namespace: ""}, nfsModuleConfig)
	if err != nil {
		handlerLog.Fatal("unable to get the csi-nfs ModuleConfig", logger.Err(err))
	}

	if err := commonvalidating.ValidateNFSStorageClass(nfsModuleConfig, nsc); err != nil {
		handlerLog.Error("the NFSStorageClass is rejected", logger.Err(err))
		return &kwhvalidating.ValidatorResult{
			Valid:   false,
			Message: fmt.Sprintf("%v", err),
		}, nil
	}

	return &kwhvalidating.ValidatorResult{Valid: true}, nil
}

// foreignStorageClass returns the StorageClass named name when it belongs to a
// provisioner other than the module's, and nil when there is no such
// StorageClass or it is the module's own.
func foreignStorageClass(ctx context.Context, cl client.Client, name string) (*sv1.StorageClass, error) {
	sc := &sv1.StorageClass{}
	err := cl.Get(ctx, client.ObjectKey{Name: name}, sc)
	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("unable to get the StorageClass %s: %w", name, err)
	}
	if sc.Provisioner == NFSStorageClassProvisioner {
		return nil, nil
	}
	return sc, nil
}
