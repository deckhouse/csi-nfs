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
	"fmt"
	"time"

	"github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/csi-nfs/hooks/go/consts"
	"github.com/deckhouse/module-sdk/pkg"
	"github.com/deckhouse/module-sdk/pkg/registry"
)

var (
	_ = registry.RegisterFunc(
		&pkg.HookConfig{
			Kubernetes: []pkg.KubernetesConfig{
				{
					Name:                         "nfs-storage-classes",
					APIVersion:                   "storage.deckhouse.io/v1alpha1",
					Kind:                         "NFSStorageClasses",
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

	snapshots := input.Snapshots.Get("NFSStorageClassList")
	if len(snapshots) == 0 {
		fmt.Println("No snapshots found")
		// Don't return early - we need to disable the scheduler extender
	} else {
		for _, snapshot := range snapshots {
			fmt.Printf("get snapshot: %v\n", snapshot)

			snapshotItem := new(v1alpha1.NFSStorageClassWorkloadNodes)

			err := snapshot.UnmarshalTo(snapshotItem)
			if err != nil {
				fmt.Printf("Error unmarshaling snapshot item: %v, skipping\n", err)
				continue // Skip this snapshot and continue with others
			}

			if snapshotItem.NodeSelector == nil {
				fmt.Println("nodeSelector is empty")
				continue
			}

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
