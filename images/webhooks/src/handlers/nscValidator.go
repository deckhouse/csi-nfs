package handlers

import (
	"context"
	"encoding/json"

	cn "github.com/deckhouse/csi-nfs/api/v1alpha1"
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

	v3presents := false
	v3enabled := false

	cl, err := NewKubeClient("")
	if err != nil {
		klog.Fatal(err)
	}

	listClasses := &cn.NFSStorageClassList{}
	err = cl.List(ctx, listClasses)

	if nsc.ObjectMeta.DeletionTimestamp == nil && arReview.Operation != "delete" && nsc.Spec.Connection.NFSVersion == "3" {
		v3presents = true
	}

	for _, itemClass := range listClasses.Items {
		if itemClass.Name == nsc.Name {
			continue
		}
		if itemClass.Spec.Connection.NFSVersion == "3" {
			v3presents = true
		}
	}

	klog.Infof("NFSv3 NFSStorageClass exists: %t", v3presents)

	nfsModuleConfig := &cn.ModuleConfig{}

	err = cl.Get(ctx, types.NamespacedName{Name: csiNfsModuleName, Namespace: ""}, nfsModuleConfig)
	if err != nil {
		klog.Fatal(err)
	}

	if value, exists := nfsModuleConfig.Spec.Settings["v3support"]; exists && value == true {
		v3enabled = true
	} else {
		v3enabled = false
	}

	klog.Infof("NFSv3 support enabled: %t", v3enabled)

	if v3presents && !v3enabled {
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
	} else if !v3presents && v3enabled {
		klog.Info("Disabling v3 support")
		patchBytes, err := json.Marshal(map[string]interface{}{
			"spec": map[string]interface{}{
				"settings": map[string]interface{}{
					"v3support": false,
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

	return &kwhvalidating.ValidatorResult{Valid: true},
		nil
}
