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

package hooks_common

import (
	"context"
	"errors"
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

func removeFinalizers(ctx context.Context, cl client.Client, obj client.Object, logger pkg.Logger) error {
	logger.Info(fmt.Sprintf("[remove-sc-and-secrets-on-module-delete]: Removing finalizers from %s %s\n", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName()))

	patch := client.MergeFrom(obj.DeepCopyObject().(client.Object))
	obj.SetFinalizers(nil)

	if err := cl.Patch(ctx, obj, patch); err != nil {
		return fmt.Errorf("failed to patch %s %s: %w", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName(), err)
	}
	return nil
}

var _ = registry.RegisterFunc(configRemoveScAndSecretsOnModuleDelete, handlerRemoveScAndSecretsOnModuleDelete)

var configRemoveScAndSecretsOnModuleDelete = &pkg.HookConfig{
	OnAfterDeleteHelm: &pkg.OrderedConfig{Order: 10},
}

func handlerRemoveScAndSecretsOnModuleDelete(ctx context.Context, input *pkg.HookInput) error {
	input.Logger.Info("[remove-sc-and-secrets-on-module-delete]: Started removing SC and Secrets on module delete")
	var resultErr error

	cl, err := kubeclient.New("",
		v1alpha1.AddToScheme,
		clientgoscheme.AddToScheme,
		extv1.AddToScheme,
		v1.AddToScheme,
		sv1.AddToScheme,
		snapv1.AddToScheme)
	if err != nil {
		return fmt.Errorf("failed to initialize kube client: %w", err)
	}

	secretList := &corev1.SecretList{}
	if err := cl.List(ctx, secretList, client.InNamespace(consts.ModuleNamespace)); err != nil {
		resultErr = errors.Join(resultErr, fmt.Errorf("[remove-sc-and-secrets-on-module-delete]: failed to list secrets: %w", err))
		return resultErr
	}

	for _, secret := range secretList.Items {
		input.Logger.Info(fmt.Sprintf("[remove-sc-and-secrets-on-module-delete]: Removing finalizers from %s secret\n", secret.Name))

		if err := removeFinalizers(ctx, cl, &secret, input.Logger); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("[remove-sc-and-secrets-on-module-delete]: failed to patch secret %s: %w", secret.Name, err))
			continue
		}
	}

	configMapList := &corev1.ConfigMapList{}
	if err := cl.List(ctx, configMapList, client.InNamespace(consts.ModuleNamespace)); err != nil {
		return errors.Join(resultErr, fmt.Errorf("[remove-sc-and-secrets-on-module-delete]: failed to list configmaps: %w", err))
	}

	for _, configMap := range configMapList.Items {
		input.Logger.Info(fmt.Sprintf("[remove-sc-and-secrets-on-module-delete]: Removing finalizers from %s configmap\n", configMap.Name))

		if err := removeFinalizers(ctx, cl, &configMap, input.Logger); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("[remove-sc-and-secrets-on-module-delete]: Failed to patch configmap %s: %w", configMap.Name, err))
			continue
		}
	}

	for _, provisioner := range consts.AllowedProvisioners {
		scList := &storagev1.StorageClassList{}
		if err := cl.List(ctx, scList); err != nil {
			resultErr = errors.Join(resultErr, fmt.Errorf("[remove-sc-and-secrets-on-module-delete]: Failed to list storage classes: %w", err))
			return resultErr
		}

		for _, sc := range scList.Items {
			if sc.Provisioner != provisioner {
				continue
			}
			input.Logger.Info(fmt.Sprintf("[remove-sc-and-secrets-on-module-delete]: Removing finalizers from %s storage class\n", sc.Name))

			if err := removeFinalizers(ctx, cl, &sc, input.Logger); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("[remove-sc-and-secrets-on-module-delete]: Failed to patch storage class %s: %w", sc.Name, err))
				continue
			}

			input.Logger.Info(fmt.Sprintf("[remove-sc-and-secrets-on-module-delete]: Removing %s storage class\n", sc.Name))
			if err := cl.Delete(ctx, &sc); err != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("[remove-sc-and-secrets-on-module-delete]: Failed to delete storage class %s: %w", sc.Name, err))
				continue
			}
		}
	}

	input.Logger.Info("[remove-sc-and-secrets-on-module-delete]: Stoped removing SC, ConfigMaps and Secrets on module delete\n")

	return resultErr
}
