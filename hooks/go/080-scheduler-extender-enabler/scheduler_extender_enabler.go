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

package schedulerextenderenabler

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/csi-nfs/hooks/go/consts"
	"github.com/deckhouse/module-sdk/pkg"
	"github.com/deckhouse/module-sdk/pkg/registry"
)

const (
	NFSStorageClassSnapshotName = "nfs-storage-classes"
)

var (
	_ = registry.RegisterFunc(
		&pkg.HookConfig{
			Kubernetes: []pkg.KubernetesConfig{
				{
					Name:                         NFSStorageClassSnapshotName,
					APIVersion:                   "storage.deckhouse.io/v1alpha1",
					Kind:                         "NFSStorageClass",
					ExecuteHookOnEvents:          ptr(true),
					ExecuteHookOnSynchronization: ptr(true),
					JqFilter:                     ".spec.workloadNodes",
					AllowFailure:                 ptr(true),
				},
			},
			Settings: &pkg.HookConfigSettings{
				ExecutionMinInterval: time.Second * 3,
				ExecutionBurst:       1,
			},
			Queue: fmt.Sprintf("modules/%s", consts.ModuleName),
		},
		mainHook,
	)
)

func mainHook(ctx context.Context, input *pkg.HookInput) error {
	fmt.Println("Scheduler extender enabler hook started")
	shouldEnable := false

	// Get snapshots using the standard approach
	snapshots := input.Snapshots.Get(NFSStorageClassSnapshotName)
	if len(snapshots) == 0 {
		fmt.Println("No snapshots found")
		// Don't return early - we need to disable the scheduler extender
	} else {
		fmt.Printf("Found %d snapshots\n", len(snapshots))

		for i, snapshot := range snapshots {
			fmt.Printf("Processing snapshot %d: %v\n", i, snapshot)

			// Try to access the snapshot data as JSON
			// The JqFilter extracts .spec.workloadNodes, so we should get NFSStorageClassWorkloadNodes directly
			// If workloadNodes is not configured, the JqFilter returns null

			// The snapshot contains a base64-encoded JSON string
			// First, marshal the snapshot to get the base64 string
			snapshotBytes, err := json.Marshal(snapshot)
			if err != nil {
				fmt.Printf("Error marshaling snapshot %d: %v\n", i, err)
				continue
			}

			// Remove quotes from the JSON string to get the base64 string
			base64Str := string(snapshotBytes[1 : len(snapshotBytes)-1])
			fmt.Printf("Snapshot %d base64: %s\n", i, base64Str)

			// Decode the base64 string
			jsonBytes, err := base64.StdEncoding.DecodeString(base64Str)
			if err != nil {
				fmt.Printf("Error decoding base64 for snapshot %d: %v\n", i, err)
				continue
			}
			fmt.Printf("Snapshot %d decoded JSON: %s\n", i, string(jsonBytes))

			// Try to unmarshal as NFSStorageClassWorkloadNodes
			workloadNodes := new(v1alpha1.NFSStorageClassWorkloadNodes)
			err = json.Unmarshal(jsonBytes, workloadNodes)
			if err != nil {
				fmt.Printf("Error unmarshaling snapshot %d as NFSStorageClassWorkloadNodes: %v\n", i, err)
				continue
			}

			fmt.Printf("Successfully unmarshaled workload nodes: %+v\n", workloadNodes)

			// Check if NodeSelector is configured
			if workloadNodes.NodeSelector == nil {
				fmt.Println("nodeSelector is nil")
				continue
			}

			// Check if NodeSelector has any actual selectors
			if workloadNodes.NodeSelector.MatchLabels == nil && workloadNodes.NodeSelector.MatchExpressions == nil {
				fmt.Println("nodeSelector has no matchLabels or matchExpressions")
				continue
			}

			fmt.Printf("Found valid nodeSelector: %+v\n", workloadNodes.NodeSelector)
			fmt.Println("NodeSelector is not empty. Should enable scheduler extender")
			shouldEnable = true
			break
		}
	}

	enableLabel := fmt.Sprintf("%v.internal.shedulerExtenderEnabled", consts.ModuleName)

	if shouldEnable {
		fmt.Println("Enable scheduler extender")
		input.Values.Set(enableLabel, true)
	} else {
		fmt.Println("Disable scheduler extender")
		input.Values.Set(enableLabel, false)
	}

	return nil
}

func ptr[T any](a T) *T {
	return &a
}
