package handlers

import (
	"context"
	"fmt"

	cn "github.com/deckhouse/csi-nfs/api/v1alpha1"
	utilsvalidating "github.com/deckhouse/csi-nfs/lib/go/utils/pkg/validating"
	"github.com/slok/kubewebhook/v2/pkg/model"
	kwhvalidating "github.com/slok/kubewebhook/v2/pkg/webhook/validating"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
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

	if err := utilsvalidating.ValidateNFSStorageClass(nfsModuleConfig, nsc); err != nil {
		klog.Error(err)
		return &kwhvalidating.ValidatorResult{
			Valid:   false,
			Message: fmt.Sprintf("%v", err),
		}, nil
	}

	return &kwhvalidating.ValidatorResult{Valid: true}, nil
}
