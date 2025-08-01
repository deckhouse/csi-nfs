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
	"net/http"

	kwhhttp "github.com/slok/kubewebhook/v2/pkg/http"
	"github.com/slok/kubewebhook/v2/pkg/log"
	"github.com/slok/kubewebhook/v2/pkg/model"
	kwhmutating "github.com/slok/kubewebhook/v2/pkg/webhook/mutating"
	kwhvalidating "github.com/slok/kubewebhook/v2/pkg/webhook/validating"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	cn "github.com/deckhouse/csi-nfs/api/v1alpha1"
	commonvalidating "github.com/deckhouse/csi-nfs/lib/go/common/pkg/validating"
	d8commonapi "github.com/deckhouse/sds-common-lib/api/v1alpha1"
)

func GetMutatingWebhookHandler(mutationFunc func(ctx context.Context, _ *model.AdmissionReview, obj metav1.Object) (*kwhmutating.MutatorResult, error), mutatorID string, obj metav1.Object, logger log.Logger) (http.Handler, error) {
	mutatorFunc := kwhmutating.MutatorFunc(mutationFunc)

	mutatingWebhookConfig := kwhmutating.WebhookConfig{
		ID:      mutatorID,
		Obj:     obj,
		Mutator: mutatorFunc,
		Logger:  logger,
	}

	mutationWebhook, err := kwhmutating.NewWebhook(mutatingWebhookConfig)
	if err != nil {
		return nil, err
	}

	mutationWebhookHandler, err := kwhhttp.HandlerFor(kwhhttp.HandlerConfig{Webhook: mutationWebhook, Logger: logger})

	return mutationWebhookHandler, err
}

func GetValidatingWebhookHandler(validationFunc func(ctx context.Context, _ *model.AdmissionReview, obj metav1.Object) (*kwhvalidating.ValidatorResult, error), validatorID string, obj metav1.Object, logger log.Logger) (http.Handler, error) {
	validatorFunc := kwhvalidating.ValidatorFunc(validationFunc)

	validatingWebhookConfig := kwhvalidating.WebhookConfig{
		ID:        validatorID,
		Obj:       obj,
		Validator: validatorFunc,
		Logger:    logger,
	}

	mutationWebhook, err := kwhvalidating.NewWebhook(validatingWebhookConfig)
	if err != nil {
		return nil, err
	}

	mutationWebhookHandler, err := kwhhttp.HandlerFor(kwhhttp.HandlerConfig{Webhook: mutationWebhook, Logger: logger})

	return mutationWebhookHandler, err
}

func validateModuleConfig(mc *d8commonapi.ModuleConfig, nscList *cn.NFSStorageClassList) error {
	for _, nsc := range nscList.Items {
		if err := commonvalidating.ValidateNFSStorageClass(mc, &nsc); err != nil {
			return err
		}
	}

	return nil
}
