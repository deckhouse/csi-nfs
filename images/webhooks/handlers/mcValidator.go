package handlers

import (
	"context"
	"fmt"

	"github.com/slok/kubewebhook/v2/pkg/model"
	kwhvalidating "github.com/slok/kubewebhook/v2/pkg/webhook/validating"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	cn "github.com/deckhouse/csi-nfs/api/v1alpha1"
)

func MCValidate(ctx context.Context, arReview *model.AdmissionReview, obj metav1.Object) (*kwhvalidating.ValidatorResult, error) {
	nfsModuleConfig, ok := obj.(*cn.ModuleConfig)
	if !ok {
		// If not a storage class just continue the validation chain(if there is one) and do nothing.
		return &kwhvalidating.ValidatorResult{}, nil
	}

	if nfsModuleConfig.ObjectMeta.DeletionTimestamp != nil || arReview.Operation == "delete" {
		return &kwhvalidating.ValidatorResult{Valid: true}, nil
	}

	cl, err := NewKubeClient("")
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
