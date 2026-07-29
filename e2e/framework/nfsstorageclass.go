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

// Package framework holds the csi-nfs e2e building blocks. Functions return errors
// instead of asserting, so they stay usable outside Ginkgo.
package framework

import (
	"context"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ModuleName      = "csi-nfs"
	ModuleNamespace = "d8-csi-nfs"

	CSIDriverName = "nfs.csi.k8s.io"

	// Set on the union of all NFSStorageClass workloadNodes selectors, or on every
	// node when none is set.
	NodeLabelKey = "storage.deckhouse.io/csi-nfs-node"

	// The snapshot-controller webhook rejects a VolumeSnapshot naming any other
	// class, so read it from here.
	VolumeSnapshotClassAnnotationKey = "storage.deckhouse.io/volumesnapshotclass"

	// Point at the per-NFSStorageClass Secret the provisioner mounts with.
	ProvisionerSecretNameKey      = "csi.storage.k8s.io/provisioner-secret-name"
	ProvisionerSecretNamespaceKey = "csi.storage.k8s.io/provisioner-secret-namespace"
)

// Addressed dynamically so the suite need not version-lock against the csi-nfs api
// module.
var NFSStorageClassGVR = schema.GroupVersionResource{
	Group: "storage.deckhouse.io", Version: "v1alpha1", Resource: "nfsstorageclasses",
}

type SecurityMode string

const (
	SecurityPlain SecurityMode = "plain"
	SecurityTLS   SecurityMode = "tls"
	SecurityMTLS  SecurityMode = "mtls"
)

// Server is one external NFS export a scenario provisions against.
type Server struct {
	Key        string // DNS-safe token resource names are derived from ("v3", "v42")
	Host       string
	Share      string
	NFSVersion string
	Security   SecurityMode
}

func (s Server) Configured() bool { return s.Host != "" && s.Share != "" }

// Plain: an in-tree NFS volume can mount this export.
func (s Server) Plain() bool { return s.Security == SecurityPlain }

func (s Server) String() string {
	return fmt.Sprintf("%s:%s (v%s, %s)", s.Host, s.Share, s.NFSVersion, s.Security)
}

// NSCOptions is the knob set the scenarios vary. Zero values are omitted from the
// manifest so the CRD defaults apply.
type NSCOptions struct {
	ReclaimPolicy     string // default "Delete"
	VolumeBindingMode string // default "WaitForFirstConsumer"

	MountMode       string // "hard" | "soft"
	Timeout         int    // tenths of a second
	Retransmissions int
	ReadOnly        *bool

	ChmodPermissions string
	VolumeCleanup    string

	// Restricts where csi-nfs mounts. Set on ANY NFSStorageClass it also switches
	// the scheduler extender on.
	WorkloadNodeSelector map[string]string
}

func BuildNFSStorageClass(name string, srv Server, opts NSCOptions) *unstructured.Unstructured {
	connection := map[string]interface{}{
		"host":       srv.Host,
		"share":      srv.Share,
		"nfsVersion": srv.NFSVersion,
	}
	// Edition-gated: emitted only when enabled, or a CE webhook rejects the
	// StorageClass.
	switch srv.Security {
	case SecurityMTLS:
		connection["tls"] = true
		connection["mtls"] = true
	case SecurityTLS:
		connection["tls"] = true
	case SecurityPlain:
	}

	reclaim := opts.ReclaimPolicy
	if reclaim == "" {
		reclaim = "Delete"
	}
	binding := opts.VolumeBindingMode
	if binding == "" {
		binding = "WaitForFirstConsumer"
	}

	spec := map[string]interface{}{
		"connection":        connection,
		"reclaimPolicy":     reclaim,
		"volumeBindingMode": binding,
	}

	mountOptions := map[string]interface{}{}
	if opts.MountMode != "" {
		mountOptions["mountMode"] = opts.MountMode
	}
	if opts.Timeout > 0 {
		mountOptions["timeout"] = int64(opts.Timeout)
	}
	if opts.Retransmissions > 0 {
		mountOptions["retransmissions"] = int64(opts.Retransmissions)
	}
	if opts.ReadOnly != nil {
		mountOptions["readOnly"] = *opts.ReadOnly
	}
	if len(mountOptions) > 0 {
		spec["mountOptions"] = mountOptions
	}

	if opts.ChmodPermissions != "" {
		spec["chmodPermissions"] = opts.ChmodPermissions
	}
	if opts.VolumeCleanup != "" {
		spec["volumeCleanup"] = opts.VolumeCleanup
	}
	if len(opts.WorkloadNodeSelector) > 0 {
		matchLabels := map[string]interface{}{}
		for k, v := range opts.WorkloadNodeSelector {
			matchLabels[k] = v
		}
		spec["workloadNodes"] = map[string]interface{}{
			"nodeSelector": map[string]interface{}{"matchLabels": matchLabels},
		}
	}

	nsc := &unstructured.Unstructured{}
	nsc.SetGroupVersionKind(schema.GroupVersionKind{
		Group: "storage.deckhouse.io", Version: "v1alpha1", Kind: "NFSStorageClass",
	})
	nsc.SetName(name)
	nsc.Object["spec"] = spec
	return nsc
}

// Returns admission errors verbatim so negative scenarios can assert the message.
func CreateNFSStorageClass(ctx context.Context, dyn dynamic.Interface, nsc *unstructured.Unstructured) error {
	_, err := dyn.Resource(NFSStorageClassGVR).Create(ctx, nsc, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func NSCStatus(ctx context.Context, dyn dynamic.Interface, name string) (phase, reason string, err error) {
	obj, err := dyn.Resource(NFSStorageClassGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return "", "", err
	}
	phase, _, _ = unstructured.NestedString(obj.Object, "status", "phase")
	reason, _, _ = unstructured.NestedString(obj.Object, "status", "reason")
	return phase, reason, nil
}

// Waits for phase Created plus the derived StorageClass; Failed short-circuits.
func WaitNSCCreated(ctx context.Context, dyn dynamic.Interface, cl client.Client, name string, timeout time.Duration) error {
	var last string
	err := PollUntil(ctx, timeout, func(ctx context.Context) (bool, error) {
		phase, reason, err := NSCStatus(ctx, dyn, name)
		switch {
		case err != nil:
			last = fmt.Sprintf("get error: %v", err)
		case phase == "Failed":
			return false, fmt.Errorf("NFSStorageClass %s went Failed: %s", name, reason)
		case phase == "Created":
			if _, scErr := GetStorageClass(ctx, cl, name); scErr == nil {
				return true, nil
			}
			last = "phase=Created but the StorageClass is not there yet"
		default:
			last = fmt.Sprintf("phase=%q reason=%q", phase, reason)
		}
		return false, nil
	})
	if err != nil {
		return fmt.Errorf("NFSStorageClass %s did not become Created: %w (last: %s)", name, err, last)
	}
	return nil
}

// Waits for both the NFSStorageClass and its StorageClass to disappear.
func DeleteNFSStorageClass(ctx context.Context, dyn dynamic.Interface, cl client.Client, name string, timeout time.Duration) error {
	err := dyn.Resource(NFSStorageClassGVR).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete NFSStorageClass %s: %w", name, err)
	}
	if err := PollUntil(ctx, timeout, func(ctx context.Context) (bool, error) {
		_, err := dyn.Resource(NFSStorageClassGVR).Get(ctx, name, metav1.GetOptions{})
		return apierrors.IsNotFound(err), nil
	}); err != nil {
		return fmt.Errorf("NFSStorageClass %s was not removed: %w", name, err)
	}
	if err := PollUntil(ctx, timeout, func(ctx context.Context) (bool, error) {
		_, err := GetStorageClass(ctx, cl, name)
		return apierrors.IsNotFound(err), nil
	}); err != nil {
		return fmt.Errorf("StorageClass %s was not garbage-collected: %w", name, err)
	}
	return nil
}

// The derived StorageClass shares the NFSStorageClass name.
func GetStorageClass(ctx context.Context, cl client.Client, name string) (*storagev1.StorageClass, error) {
	var sc storagev1.StorageClass
	if err := cl.Get(ctx, client.ObjectKey{Name: name}, &sc); err != nil {
		return nil, err
	}
	return &sc, nil
}

// Mirrors the controller's GetSCMountOptions ordering, so the exact rendered option
// list can be asserted.
func ExpectedMountOptions(srv Server, opts NSCOptions) []string {
	out := []string{"nfsvers=" + srv.NFSVersion}
	switch srv.Security {
	case SecurityMTLS:
		out = append(out, "xprtsec=mtls")
	case SecurityTLS:
		out = append(out, "xprtsec=tls")
	case SecurityPlain:
	}
	if opts.MountMode != "" {
		out = append(out, opts.MountMode)
	}
	if opts.Timeout > 0 {
		out = append(out, fmt.Sprintf("timeo=%d", opts.Timeout))
	}
	if opts.Retransmissions > 0 {
		out = append(out, fmt.Sprintf("retrans=%d", opts.Retransmissions))
	}
	if opts.ReadOnly != nil {
		if *opts.ReadOnly {
			out = append(out, "ro")
		} else {
			out = append(out, "rw")
		}
	}
	return out
}

// Read off the StorageClass rather than rebuilt from the controller's prefix: the
// contract is that the StorageClass points at a Secret that exists.
func MountOptionsSecretRef(sc *storagev1.StorageClass) (namespace, name string, err error) {
	name = sc.Parameters[ProvisionerSecretNameKey]
	namespace = sc.Parameters[ProvisionerSecretNamespaceKey]
	if name == "" || namespace == "" {
		return "", "", fmt.Errorf("StorageClass %s references no provisioner secret (%s=%q, %s=%q)",
			sc.Name, ProvisionerSecretNameKey, name, ProvisionerSecretNamespaceKey, namespace)
	}
	return namespace, name, nil
}

func GetMountOptionsSecret(ctx context.Context, cl client.Client, sc *storagev1.StorageClass) (*corev1.Secret, error) {
	namespace, name, err := MountOptionsSecretRef(sc)
	if err != nil {
		return nil, err
	}
	var secret corev1.Secret
	if err := cl.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &secret); err != nil {
		return nil, err
	}
	return &secret, nil
}

func VolumeSnapshotClassFor(ctx context.Context, cl client.Client, scName string) (string, error) {
	sc, err := GetStorageClass(ctx, cl, scName)
	if err != nil {
		return "", err
	}
	class := sc.Annotations[VolumeSnapshotClassAnnotationKey]
	if class == "" {
		return "", fmt.Errorf("StorageClass %s has no %s annotation", scName, VolumeSnapshotClassAnnotationKey)
	}
	return class, nil
}

func ContainsAll(got, want []string) bool {
	index := make(map[string]struct{}, len(got))
	for _, g := range got {
		index[strings.TrimSpace(g)] = struct{}{}
	}
	for _, w := range want {
		if _, ok := index[w]; !ok {
			return false
		}
	}
	return true
}
