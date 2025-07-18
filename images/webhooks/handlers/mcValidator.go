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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"

	cn "github.com/deckhouse/csi-nfs/api/v1alpha1"
	d8commonapi "github.com/deckhouse/sds-common-lib/api/v1alpha1"
	"github.com/deckhouse/sds-common-lib/kubeclient"
)

func MCValidate(ctx context.Context, arReview *model.AdmissionReview, obj metav1.Object) (*kwhvalidating.ValidatorResult, error) {
	nfsModuleConfig, ok := obj.(*d8commonapi.ModuleConfig)
	if !ok {
		// If not a storage class just continue the validation chain(if there is one) and do nothing.
		return &kwhvalidating.ValidatorResult{}, nil
	}

	if nfsModuleConfig.ObjectMeta.DeletionTimestamp != nil || arReview.Operation == "delete" {
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
		klog.Fatal(err) // pod restarting
	}

	nscList := &cn.NFSStorageClassList{}
	err = cl.List(ctx, nscList)
	if err != nil {
		klog.Fatal(err)
	}

	if err := validateModuleConfig(nfsModuleConfig, nscList); err != nil {
		klog.Error(err)
		return &kwhvalidating.ValidatorResult{
			Valid:   false,
			Message: fmt.Sprintf("%v", err),
		}, nil
	}

	return &kwhvalidating.ValidatorResult{Valid: true}, nil
}
