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

package scheduler_extender

import (
	"context"
	"fmt"
)

func (h *Hook) Execute(ctx context.Context, hookCtx *hook.HookContext, bindings map[string][]types.Snapshot) error {
	fmt.Println("Scheduler extender enabler hook started")
	fmt.Printf("Bindings: %+v\n", bindings)

	shouldEnable := false
	snapshots, exists := bindings["nfs-storage-classes"]
	if !exists || len(snapshots) == 0 {
		fmt.Println("No snapshots found for nfs-storage-classes")
	} else {
		for i, snapshot := range snapshots {
			fmt.Printf("Snapshot %d: %+v\n", i, snapshot)
			filterResult, ok := snapshot.FilterResult.(map[string]interface{})
			if !ok || len(filterResult) == 0 {
				fmt.Printf("Filter result is empty or invalid: %v\n", snapshot.FilterResult)
				continue
			}
			fmt.Printf("Filter result: %+v\n", filterResult)

			nodeSelector, ok := filterResult["nodeSelector"].(map[string]interface{})
			if !ok || len(nodeSelector) == 0 {
				fmt.Printf("nodeSelector is empty or invalid: %v\n", filterResult["nodeSelector"])
				continue
			}
			fmt.Println("NodeSelector is not empty. Should enable scheduler extender")
			shouldEnable = true
			break
		}
	}

	if shouldEnable {
		fmt.Println("Enable scheduler extender")
		err := module.SetValue("csiNfs.internal.schedulerExtenderEnabled", true, ctx.Value())
		if err != nil {
			return fmt.Errorf("failed to enable scheduler extender: %v", err)
		}
	} else {
		fmt.Println("Disable scheduler extender")
		err := module.SetValue("csiNfs.internal.schedulerExtenderEnabled", false, hookCtx.Values)
		if err != nil {
			return fmt.Errorf("failed to disable scheduler extender: %v", err)
		}
	}

	return nil
}
