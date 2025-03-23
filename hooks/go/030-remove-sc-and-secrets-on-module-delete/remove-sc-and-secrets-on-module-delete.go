package hooks_common

import (
	"context"
	"fmt"

	snapv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	sv1 "k8s.io/api/storage/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/csi-nfs/hooks/go/consts"
	"github.com/deckhouse/module-sdk/pkg"
	"github.com/deckhouse/module-sdk/pkg/registry"
	"github.com/deckhouse/sds-common-lib/kubeclient"
)

var _ = registry.RegisterFunc(configRemoveScAndSecretsOnModuleDelete, handlerRemoveScAndSecretsOnModuleDelete)

var configRemoveScAndSecretsOnModuleDelete = &pkg.HookConfig{
	OnAfterDeleteHelm: &pkg.OrderedConfig{Order: 10},
}

func handlerRemoveScAndSecretsOnModuleDelete(ctx context.Context, input *pkg.HookInput) error {
	input.Logger.Info("[remove-sc-and-secrets-on-module-delete]: Started removing SC and Secrets on module delete")
	cl, err := kubeclient.NewKubeClient("",
		v1alpha1.AddToScheme,
		clientgoscheme.AddToScheme,
		extv1.AddToScheme,
		v1.AddToScheme,
		sv1.AddToScheme,
		snapv1.AddToScheme)
	if err != nil {
		input.Logger.Error(fmt.Sprintf("Failed to initialize kube client: %v", err))
		return err
	}

	secretList := &corev1.SecretList{}
	err = cl.List(ctx, secretList, client.InNamespace(consts.ModuleNamespace))
	if err != nil {
		input.Logger.Error(fmt.Sprintf("[remove-sc-and-secrets-on-module-delete]: Failed to list secrets: %v", err))
		return err
	}

	for _, secret := range secretList.Items {
		input.Logger.Info(fmt.Sprintf("[remove-sc-and-secrets-on-module-delete]: Removing finalizers from %s secret\n", secret.Name))

		patch := client.MergeFrom(secret.DeepCopy())
		secret.ObjectMeta.Finalizers = nil

		err = cl.Patch(ctx, &secret, patch)
		if err != nil {
			input.Logger.Error(fmt.Sprintf("[remove-sc-and-secrets-on-module-delete]: Failed to patch secret %s: %v", secret.Name, err))
			return err
		}
	}

	configMapList := &corev1.ConfigMapList{}
	err = cl.List(ctx, configMapList, client.InNamespace(consts.ModuleNamespace))
	if err != nil {
		input.Logger.Error(fmt.Sprintf("[remove-sc-and-secrets-on-module-delete]: Failed to list configmaps: %v", err))
		return err
	}
	for _, configMap := range configMapList.Items {
		input.Logger.Info(fmt.Sprintf("[remove-sc-and-secrets-on-module-delete]: Removing finalizers from %s configmap\n", configMap.Name))

		patch := client.MergeFrom(configMap.DeepCopy())
		configMap.ObjectMeta.Finalizers = nil

		err = cl.Patch(ctx, &configMap, patch)
		if err != nil {
			input.Logger.Error(fmt.Sprintf("[remove-sc-and-secrets-on-module-delete]: Failed to patch configmap %s: %v", configMap.Name, err))
			return err
		}
	}

	scList := &storagev1.StorageClassList{}
	err = cl.List(ctx, scList)
	if err != nil {
		input.Logger.Error(fmt.Sprintf("[remove-sc-and-secrets-on-module-delete]: Failed to list storage classes: %v", err))
		return err
	}

	for _, sc := range scList.Items {
		for _, provisioner := range consts.AllowedProvisioners {
			if sc.Provisioner == provisioner {
				input.Logger.Info(fmt.Sprintf("[remove-sc-and-secrets-on-module-delete]: Removing finalizers from %s storage class\n", sc.Name))

				patch := client.MergeFrom(sc.DeepCopy())
				sc.ObjectMeta.Finalizers = nil

				err = cl.Patch(ctx, &sc, patch)
				if err != nil {
					input.Logger.Error(fmt.Sprintf("[remove-sc-and-secrets-on-module-delete]: Failed to patch storage class %s: %v", sc.Name, err))
					return err
				}

				input.Logger.Info(fmt.Sprintf("[remove-sc-and-secrets-on-module-delete]: Removing %s storage class\n", sc.Name))
				err = cl.Delete(ctx, &sc)
				if err != nil {
					input.Logger.Error(fmt.Sprintf("[remove-sc-and-secrets-on-module-delete]: Failed to delete storage class %s: %v", sc.Name, err))
					return err
				}
			}
		}
	}

	input.Logger.Info("[remove-sc-and-secrets-on-module-delete]: Stoped removing SC and Secrets on module delete\n")

	return nil
}
