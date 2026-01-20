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

package helpers

import (
	"context"
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

// nfsStorageClassGVR is the GroupVersionResource for NFSStorageClass
var nfsStorageClassGVR = schema.GroupVersionResource{
	Group:    "storage.deckhouse.io",
	Version:  "v1alpha1",
	Resource: "nfsstorageclasses",
}

// NFSStorageClassClient provides operations on NFSStorageClass resources
type NFSStorageClassClient struct {
	client dynamic.Interface
}

// NewNFSStorageClassClient creates a new NFSStorageClass client from a rest.Config
func NewNFSStorageClassClient(config *rest.Config) (*NFSStorageClassClient, error) {
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}
	return &NFSStorageClassClient{client: dynamicClient}, nil
}

// NFSStorageClassConfig represents configuration for an NFSStorageClass
type NFSStorageClassConfig struct {
	Name              string // NFSStorageClass name
	Host              string // NFS server host
	Share             string // NFS server share path
	NFSVersion        string // NFS version: "3", "4.1", or "4.2"
	ReclaimPolicy     string // Reclaim policy: Delete or Retain
	VolumeBindingMode string // Volume binding mode: Immediate or WaitForFirstConsumer
	ChmodPermissions  string // chmod rights for PVs subdirectory (optional)
	MountMode         string // Mount mode: hard or soft (optional)
	Timeout           int    // NFS timeout in tenths of a second (optional)
	Retransmissions   int    // Number of retransmissions (optional)
	ReadOnly          *bool  // Share read-only flag (optional)
	VolumeCleanup     string // Volume cleanup mode (optional, EE only)
	Tls               bool   // Use TLS for connection (optional)
	Mtls              bool   // Use mTLS for connection (optional)
}

// Create creates a new NFSStorageClass resource
func (c *NFSStorageClassClient) Create(ctx context.Context, cfg NFSStorageClassConfig) error {
	connection := map[string]interface{}{
		"host":       cfg.Host,
		"share":      cfg.Share,
		"nfsVersion": cfg.NFSVersion,
		"tls":        cfg.Tls,
		"mtls":       cfg.Mtls,
	}

	spec := map[string]interface{}{
		"connection":        connection,
		"reclaimPolicy":     cfg.ReclaimPolicy,
		"volumeBindingMode": cfg.VolumeBindingMode,
	}

	// Add optional chmod permissions
	if cfg.ChmodPermissions != "" {
		spec["chmodPermissions"] = cfg.ChmodPermissions
	}

	// Add optional mount options
	mountOptions := make(map[string]interface{})
	if cfg.MountMode != "" {
		mountOptions["mountMode"] = cfg.MountMode
	}
	if cfg.Timeout > 0 {
		mountOptions["timeout"] = cfg.Timeout
	}
	if cfg.Retransmissions > 0 {
		mountOptions["retransmissions"] = cfg.Retransmissions
	}
	if cfg.ReadOnly != nil {
		mountOptions["readOnly"] = *cfg.ReadOnly
	}
	if len(mountOptions) > 0 {
		spec["mountOptions"] = mountOptions
	}

	// Add optional volume cleanup (EE only)
	if cfg.VolumeCleanup != "" {
		spec["volumeCleanup"] = cfg.VolumeCleanup
	}

	nsc := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "storage.deckhouse.io/v1alpha1",
			"kind":       "NFSStorageClass",
			"metadata": map[string]interface{}{
				"name": cfg.Name,
			},
			"spec": spec,
		},
	}

	_, err := c.client.Resource(nfsStorageClassGVR).Create(ctx, nsc, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create NFSStorageClass %s: %w", cfg.Name, err)
	}

	return nil
}

// Get retrieves an NFSStorageClass by name
func (c *NFSStorageClassClient) Get(ctx context.Context, name string) (*unstructured.Unstructured, error) {
	nsc, err := c.client.Resource(nfsStorageClassGVR).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get NFSStorageClass %s: %w", name, err)
	}
	return nsc, nil
}

// Delete deletes an NFSStorageClass by name
func (c *NFSStorageClassClient) Delete(ctx context.Context, name string) error {
	err := c.client.Resource(nfsStorageClassGVR).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete NFSStorageClass %s: %w", name, err)
	}
	return nil
}

// WaitForCreated waits for an NFSStorageClass to reach Created phase
func (c *NFSStorageClassClient) WaitForCreated(ctx context.Context, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for NFSStorageClass %s to become Created", name)
		}

		nsc, err := c.Get(ctx, name)
		if err != nil {
			time.Sleep(2 * time.Second)
			continue
		}

		phase, found, _ := unstructured.NestedString(nsc.Object, "status", "phase")
		if found && phase == "Created" {
			return nil
		}

		// Check for Failed phase
		if found && phase == "Failed" {
			reason, _, _ := unstructured.NestedString(nsc.Object, "status", "reason")
			return fmt.Errorf("NFSStorageClass %s failed: %s", name, reason)
		}

		time.Sleep(2 * time.Second)
	}
}

// WaitForDeletion waits for an NFSStorageClass to be deleted
func (c *NFSStorageClassClient) WaitForDeletion(ctx context.Context, name string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for NFSStorageClass %s to be deleted", name)
		}

		_, err := c.Get(ctx, name)
		if err != nil {
			// Assume deleted if we can't get it
			return nil
		}

		time.Sleep(2 * time.Second)
	}
}

// GetPhase returns the current phase of an NFSStorageClass
func (c *NFSStorageClassClient) GetPhase(ctx context.Context, name string) (string, error) {
	nsc, err := c.Get(ctx, name)
	if err != nil {
		return "", err
	}

	phase, _, _ := unstructured.NestedString(nsc.Object, "status", "phase")
	return phase, nil
}

// CreateNFSStorageClass is a convenience function to create an NFSStorageClass
func CreateNFSStorageClass(ctx context.Context, kubeconfig *rest.Config, cfg NFSStorageClassConfig) error {
	client, err := NewNFSStorageClassClient(kubeconfig)
	if err != nil {
		return err
	}
	return client.Create(ctx, cfg)
}

// WaitForNFSStorageClassCreated is a convenience function to wait for an NFSStorageClass to be created
func WaitForNFSStorageClassCreated(ctx context.Context, kubeconfig *rest.Config, name string, timeout time.Duration) error {
	client, err := NewNFSStorageClassClient(kubeconfig)
	if err != nil {
		return err
	}
	return client.WaitForCreated(ctx, name, timeout)
}

// DeleteNFSStorageClass is a convenience function to delete an NFSStorageClass
func DeleteNFSStorageClass(ctx context.Context, kubeconfig *rest.Config, name string) error {
	client, err := NewNFSStorageClassClient(kubeconfig)
	if err != nil {
		return err
	}
	return client.Delete(ctx, name)
}

// WaitForNFSStorageClassDeletion is a convenience function to wait for an NFSStorageClass to be deleted
func WaitForNFSStorageClassDeletion(ctx context.Context, kubeconfig *rest.Config, name string, timeout time.Duration) error {
	client, err := NewNFSStorageClassClient(kubeconfig)
	if err != nil {
		return err
	}
	return client.WaitForDeletion(ctx, name, timeout)
}
