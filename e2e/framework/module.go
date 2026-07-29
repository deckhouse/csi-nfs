/*
Copyright 2026 Flant JSC

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

package framework

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	storagekube "github.com/deckhouse/storage-e2e/pkg/kubernetes"
)

// Also where the tlshd sidecar servicing RPC-with-TLS handshakes runs.
const CSINodeDaemonSetName = "csi-node"

var ModuleConfigGVR = schema.GroupVersionResource{
	Group: "deckhouse.io", Version: "v1alpha1", Resource: "moduleconfigs",
}

// Returned when the edition has no RPC-with-TLS feature; callers skip on it.
var ErrTLSFeatureUnavailable = errors.New("the RPC-with-TLS feature is not available in this edition")

func WaitModuleReady(ctx context.Context, restCfg *rest.Config, timeout time.Duration) error {
	return storagekube.WaitForModuleReady(ctx, restCfg, ModuleName, timeout)
}

func ModuleSettings(ctx context.Context, dyn dynamic.Interface) (map[string]interface{}, error) {
	mc, err := dyn.Resource(ModuleConfigGVR).Get(ctx, ModuleName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get ModuleConfig %s: %w", ModuleName, err)
	}
	settings, found, err := unstructured.NestedMap(mc.Object, "spec", "settings")
	if err != nil {
		return nil, err
	}
	if !found {
		return map[string]interface{}{}, nil
	}
	return settings, nil
}

// Admission errors are returned verbatim.
func PatchModuleSettings(ctx context.Context, dyn dynamic.Interface, mutate func(settings map[string]interface{})) error {
	mc, err := dyn.Resource(ModuleConfigGVR).Get(ctx, ModuleName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get ModuleConfig %s: %w", ModuleName, err)
	}
	settings, _, err := unstructured.NestedMap(mc.Object, "spec", "settings")
	if err != nil {
		return err
	}
	if settings == nil {
		settings = map[string]interface{}{}
	}
	mutate(settings)
	if err := unstructured.SetNestedMap(mc.Object, settings, "spec", "settings"); err != nil {
		return err
	}
	if _, err := dyn.Resource(ModuleConfigGVR).Update(ctx, mc, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update ModuleConfig %s: %w", ModuleName, err)
	}
	return nil
}

// All fields are base64-encoded PEM.
type TLSParameters struct {
	CA         string
	ClientCert string
	ClientKey  string
}

// Call lazily, from the first tls/mtls scenario, never from suite setup:
// tlsParameters add the net-handshake-checker init container to the node DaemonSet
// AND to the CSI controller Deployment, so on a node without CONFIG_NET_HANDSHAKE
// both crashloop and the cluster is left with no provisioner at all.
func EnableTLS(ctx context.Context, dyn dynamic.Interface, restCfg *rest.Config, params TLSParameters, moduleReadyTimeout time.Duration) error {
	if params.CA == "" {
		return errors.New("no CA supplied")
	}

	tlsParameters := map[string]interface{}{"ca": params.CA}
	if params.ClientCert != "" && params.ClientKey != "" {
		tlsParameters["mtls"] = map[string]interface{}{
			"clientCert": params.ClientCert,
			"clientKey":  params.ClientKey,
		}
	}

	err := PatchModuleSettings(ctx, dyn, func(settings map[string]interface{}) {
		settings["tlsParameters"] = tlsParameters
	})
	if err != nil {
		// An edition without the feature is an environment fact, not a failure.
		if strings.Contains(err.Error(), "TLS feature is not available") {
			return ErrTLSFeatureUnavailable
		}
		return err
	}

	if err := WaitModuleReady(ctx, restCfg, moduleReadyTimeout); err != nil {
		return fmt.Errorf("module did not settle after enabling tlsParameters: %w", err)
	}
	return nil
}

// The render wait is not optional: a ModuleConfig change only queues work, so for a
// while the DaemonSet is still the old sidecar-less spec while the module keeps
// reporting Ready from before the change.
func WaitTLSRollout(ctx context.Context, cl client.Client, renderTimeout, rolloutTimeout time.Duration) error {
	if err := PollUntil(ctx, renderTimeout, func(ctx context.Context) (bool, error) {
		return TlshdContainerPresent(ctx, cl)
	}); err != nil {
		return fmt.Errorf("the tlshd sidecar was never rendered into %s/%s: %w",
			ModuleNamespace, CSINodeDaemonSetName, err)
	}
	return WaitCSINodeDaemonSetReady(ctx, cl, rolloutTimeout)
}

func WaitCSINodeDaemonSetReady(ctx context.Context, cl client.Client, timeout time.Duration) error {
	var last string
	if err := PollUntil(ctx, timeout, func(ctx context.Context) (bool, error) {
		var ds appsv1.DaemonSet
		err := cl.Get(ctx, client.ObjectKey{Namespace: ModuleNamespace, Name: CSINodeDaemonSetName}, &ds)
		switch {
		case err != nil:
			last = fmt.Sprintf("get error: %v", err)
		case ds.Status.DesiredNumberScheduled == 0:
			last = "no nodes scheduled yet"
		case ds.Status.NumberReady == ds.Status.DesiredNumberScheduled &&
			ds.Status.UpdatedNumberScheduled == ds.Status.DesiredNumberScheduled &&
			ds.Status.ObservedGeneration >= ds.Generation:
			return true, nil
		default:
			last = fmt.Sprintf("ready=%d updated=%d desired=%d",
				ds.Status.NumberReady, ds.Status.UpdatedNumberScheduled, ds.Status.DesiredNumberScheduled)
		}
		return false, nil
	}); err != nil {
		return fmt.Errorf("DaemonSet %s/%s did not become Ready: %w (last: %s)",
			ModuleNamespace, CSINodeDaemonSetName, err, last)
	}
	return nil
}

func TlshdContainerPresent(ctx context.Context, cl client.Client) (bool, error) {
	var ds appsv1.DaemonSet
	if err := cl.Get(ctx, client.ObjectKey{Namespace: ModuleNamespace, Name: CSINodeDaemonSetName}, &ds); err != nil {
		return false, nil
	}
	for _, c := range ds.Spec.Template.Spec.Containers {
		if c.Name == "tlshd" {
			return true, nil
		}
	}
	return false, nil
}

// SchedulerExtenderDeploymentName appears once any NFSStorageClass carries a
// non-empty workloadNodes nodeSelector.
const SchedulerExtenderDeploymentName = "csi-nfs-scheduler-extender"

func CSINodeDesiredCount(ctx context.Context, cl client.Client) (int32, error) {
	var ds appsv1.DaemonSet
	if err := cl.Get(ctx, client.ObjectKey{Namespace: ModuleNamespace, Name: CSINodeDaemonSetName}, &ds); err != nil {
		return -1, err
	}
	return ds.Status.DesiredNumberScheduled, nil
}
