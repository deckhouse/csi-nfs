/*
Copyright 2024 Flant JSC

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
	"errors"
	"fmt"
	"net/http"
	"os"

	cn "github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/go-logr/logr"
	"github.com/slok/kubewebhook/v2/pkg/log"
	v1 "k8s.io/api/core/v1"
	"k8s.io/api/resource/v1alpha3"
	sv1 "k8s.io/api/storage/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiruntime "k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	kwhhttp "github.com/slok/kubewebhook/v2/pkg/http"
	"github.com/slok/kubewebhook/v2/pkg/model"
	kwhmutating "github.com/slok/kubewebhook/v2/pkg/webhook/mutating"
	kwhvalidating "github.com/slok/kubewebhook/v2/pkg/webhook/validating"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
)

func NewKubeClient(kubeconfigPath string) (client.Client, error) {
	var config *rest.Config
	var err error

	if kubeconfigPath == "" {
		kubeconfigPath = os.Getenv("kubeconfig")
	}

	controllerruntime.SetLogger(logr.New(ctrllog.NullLogSink{}))

	config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)

	if err != nil {
		return nil, err
	}

	var (
		resourcesSchemeFuncs = []func(*apiruntime.Scheme) error{
			v1alpha3.AddToScheme,
			cn.AddToScheme,
			clientgoscheme.AddToScheme,
			extv1.AddToScheme,
			v1.AddToScheme,
			sv1.AddToScheme,
		}
	)

	scheme := apiruntime.NewScheme()
	for _, f := range resourcesSchemeFuncs {
		err = f(scheme)
		if err != nil {
			return nil, err
		}
	}

	clientOpts := client.Options{
		Scheme: scheme,
	}

	return client.New(config, clientOpts)
}

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

// see images/controller/src/pkg/controller/nfs_storage_class_watcher_func.go
func validateNFSStorageClass(nfsModuleConfig *cn.ModuleConfig, nsc *cn.NFSStorageClass) error {
	var logPostfix string = "Such a combination of parameters is not allowed"

	if nsc.Spec.Connection.NFSVersion == "3" {
		if value, ok := nfsModuleConfig.Spec.Settings["v3support"]; !ok {
			return errors.New(fmt.Sprintf(
				"ModuleConfig: %s (the v3support parameter is missing); NFSStorageClass: %s (nfsVersion is set to 3); %s",
				nfsModuleConfig.Name, nsc.Name, logPostfix,
			))
		} else {
			if value == false {
				return errors.New(fmt.Sprintf(
					"ModuleConfig: %s (the v3support parameter is disabled); NFSStorageClass: %s (nfsVersion is set to 3); %s",
					nfsModuleConfig.Name, nsc.Name, logPostfix,
				))
			}
		}
	}

	if nsc.Spec.Connection.Tls || nsc.Spec.Connection.Mtls {
		var tlsParameters map[string]interface{}

		if value, ok := nfsModuleConfig.Spec.Settings["tlsParameters"]; !ok {
			return errors.New(fmt.Sprintf(
				"ModuleConfig: %s (the tlsParameters parameter is missing); NFSStorageClass: %s (tls or mtls is enabled); %s",
				nfsModuleConfig.Name, nsc.Name, logPostfix,
			))
		} else {
			tlsParameters = value.(map[string]interface{})
		}
		if value, ok := tlsParameters["ca"]; !ok || len(value.(string)) == 0 {
			return errors.New(fmt.Sprintf(
				"ModuleConfig: %s (the tlsParameters.ca parameter is either missing or has a zero length); NFSStorageClass: %s (tls or mtls is enabled); %s",
				nfsModuleConfig.Name, nsc.Name, logPostfix,
			))
		}

		if nsc.Spec.Connection.Mtls {
			var mtls map[string]interface{}

			if value, ok := tlsParameters["mtls"]; !ok {
				return errors.New(fmt.Sprintf(
					"ModuleConfig: %s (the tlsParameters.mtls parameter is missing); NFSStorageClass: %s (mtls is enabled); %s",
					nfsModuleConfig.Name, nsc.Name, logPostfix,
				))
			} else {
				mtls = value.(map[string]interface{})
			}
			if value, ok := mtls["clientCert"]; !ok || len(value.(string)) == 0 {
				return errors.New(fmt.Sprintf(
					"ModuleConfig: %s (the tlsParameters.mtls.clientCert parameter is either missing or has a zero length); NFSStorageClass: %s (mtls is enabled); %s",
					nfsModuleConfig.Name, nsc.Name, logPostfix,
				))
			}
			if value, ok := mtls["clientKey"]; !ok || len(value.(string)) == 0 {
				return errors.New(fmt.Sprintf(
					"ModuleConfig: %s (the tlsParameters.mtls.clientKey parameter is either missing or has a zero length); NFSStorageClass: %s (mtls is enabled); %s",
					nfsModuleConfig.Name, nsc.Name, logPostfix,
				))
			}
		}
	}

	return nil
}

func validateModuleConfig(mc *cn.ModuleConfig, nscList *cn.NFSStorageClassList) error {
	for _, nsc := range nscList.Items {
		if err := validateNFSStorageClass(mc, &nsc); err != nil {
			return err
		}
	}

	return nil
}
