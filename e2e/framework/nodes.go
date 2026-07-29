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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func NodeSchedulable(n *corev1.Node) bool {
	if n.Spec.Unschedulable {
		return false
	}
	for _, t := range n.Spec.Taints {
		if t.Effect == corev1.TaintEffectNoSchedule || t.Effect == corev1.TaintEffectNoExecute {
			return false
		}
	}
	return true
}

// SchedulableWorkers lists them in a stable order.
func SchedulableWorkers(ctx context.Context, cl client.Client) ([]string, error) {
	var nodes corev1.NodeList
	if err := cl.List(ctx, &nodes); err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	var names []string
	for i := range nodes.Items {
		n := &nodes.Items[i]
		if _, isMaster := n.Labels["node-role.kubernetes.io/control-plane"]; isMaster {
			continue
		}
		if NodeSchedulable(n) {
			names = append(names, n.Name)
		}
	}
	return names, nil
}

// OtherSchedulableWorker returns a schedulable worker distinct from exclude, or
// "" when there is none.
func OtherSchedulableWorker(ctx context.Context, cl client.Client, exclude string) (string, error) {
	workers, err := SchedulableWorkers(ctx, cl)
	if err != nil {
		return "", err
	}
	for _, n := range workers {
		if n != exclude {
			return n, nil
		}
	}
	return "", nil
}

// NodeHasLabel reports whether the node carries the label key.
func NodeHasLabel(ctx context.Context, cl client.Client, nodeName, key string) (bool, error) {
	var node corev1.Node
	if err := cl.Get(ctx, client.ObjectKey{Name: nodeName}, &node); err != nil {
		return false, err
	}
	_, ok := node.Labels[key]
	return ok, nil
}

// SetNodeLabel adds or updates a label on a node.
func SetNodeLabel(ctx context.Context, cl client.Client, nodeName, key, value string) error {
	var node corev1.Node
	if err := cl.Get(ctx, client.ObjectKey{Name: nodeName}, &node); err != nil {
		return err
	}
	if node.Labels == nil {
		node.Labels = map[string]string{}
	}
	if node.Labels[key] == value {
		return nil
	}
	node.Labels[key] = value
	if err := cl.Update(ctx, &node); err != nil {
		return fmt.Errorf("label node %s: %w", nodeName, err)
	}
	return nil
}

// A missing node or label is success, so this is safe as cleanup.
func RemoveNodeLabel(ctx context.Context, cl client.Client, nodeName, key string) error {
	var node corev1.Node
	if err := cl.Get(ctx, client.ObjectKey{Name: nodeName}, &node); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}
	if _, ok := node.Labels[key]; !ok {
		return nil
	}
	delete(node.Labels, key)
	if err := cl.Update(ctx, &node); err != nil {
		return fmt.Errorf("unlabel node %s: %w", nodeName, err)
	}
	return nil
}

// NodesMissingLabel returns the subset of want that does not carry key yet.
func NodesMissingLabel(ctx context.Context, cl client.Client, want []string, key string) ([]string, error) {
	var nodes corev1.NodeList
	if err := cl.List(ctx, &nodes); err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	labelled := map[string]struct{}{}
	for i := range nodes.Items {
		if _, ok := nodes.Items[i].Labels[key]; ok {
			labelled[nodes.Items[i].Name] = struct{}{}
		}
	}
	var missing []string
	for _, n := range want {
		if _, ok := labelled[n]; !ok {
			missing = append(missing, n)
		}
	}
	return missing, nil
}

// Includes masters: the node DaemonSet covers them too when no NFSStorageClass
// restricts workloadNodes.
func AllSchedulableNodes(ctx context.Context, cl client.Client) ([]string, error) {
	var nodes corev1.NodeList
	if err := cl.List(ctx, &nodes); err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	var names []string
	for i := range nodes.Items {
		if NodeSchedulable(&nodes.Items[i]) {
			names = append(names, nodes.Items[i].Name)
		}
	}
	return names, nil
}
