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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	cn "github.com/deckhouse/csi-nfs/api/v1alpha1"
	commonvalidating "github.com/deckhouse/csi-nfs/lib/go/common/pkg/validating"
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

	if nsc.ObjectMeta.DeletionTimestamp != nil || arReview.Operation == "delete" {
		return &kwhvalidating.ValidatorResult{Valid: true}, nil
	}

	cl, err := NewKubeClient("")
	if err != nil {
		klog.Fatal(err) // pod restarting
	}

	nfsModuleConfig := &cn.ModuleConfig{}
	err = cl.Get(ctx, types.NamespacedName{Name: csiNfsModuleName, Namespace: ""}, nfsModuleConfig)
	if err != nil {
		klog.Fatal(err)
	}

	if err := commonvalidating.ValidateNFSStorageClass(nfsModuleConfig, nsc); err != nil {
		klog.Error(err)
		return &kwhvalidating.ValidatorResult{
			Valid:   false,
			Message: fmt.Sprintf("%v", err),
		}, nil
	}

	return &kwhvalidating.ValidatorResult{Valid: true}, nil
}
