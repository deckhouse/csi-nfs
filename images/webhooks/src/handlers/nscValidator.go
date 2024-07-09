package handlers

import (
	"context"
	"encoding/json"
	cn "github.com/deckhouse/csi-nfs/api/v1alpha1"
	dh "github.com/deckhouse/deckhouse/deckhouse-controller/pkg/apis/deckhouse.io/v1alpha1"
	"github.com/slok/kubewebhook/v2/pkg/model"
	kwhvalidating "github.com/slok/kubewebhook/v2/pkg/webhook/validating"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	if nsc.Spec.Connection.NFSVersion == "3" {
		cl, err := NewKubeClient("")
		if err != nil {
			klog.Fatal(err)
		}

		nfsModuleConfig := &dh.ModuleConfig{}

		err = cl.Get(ctx, types.NamespacedName{Name: csiNfsModuleName, Namespace: ""}, nfsModuleConfig)
		if err != nil {
			klog.Fatal(err)
		}

		if value, exists := nfsModuleConfig.Spec.Settings["v3support"]; exists && value == true {
			klog.Info("v3 support is enabled")
		} else {
			klog.Info("Enabling v3 support")
			patchBytes, err := json.Marshal(map[string]interface{}{
				"spec": map[string]interface{}{
					"settings": map[string]interface{}{
						"v3support": true,
					},
				},
			})

			if err != nil {
				klog.Fatalf("Error marshalling patch: %s", err.Error())
			}

			err = cl.Patch(context.TODO(), nfsModuleConfig, client.RawPatch(types.MergePatchType, patchBytes))
			if err != nil {
				klog.Fatalf("Error patching object: %s", err.Error())
			}
		}

		klog.Info()
	}
	return &kwhvalidating.ValidatorResult{Valid: true},
		nil
}
