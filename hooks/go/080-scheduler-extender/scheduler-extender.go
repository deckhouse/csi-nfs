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
	"github.com/deckhouse/csi-nfs/api/v1alpha1"
	"github.com/deckhouse/csi-nfs/hooks/go/consts"
	"github.com/deckhouse/module-sdk/pkg"
	"github.com/deckhouse/module-sdk/pkg/registry"
	"log"
)

var (
	_ = registry.RegisterFunc(
		&pkg.HookConfig{

			Kubernetes: []pkg.KubernetesConfig{
				{Name: "nfs-storage-classes",
					APIVersion:        "storage.deckhouse.io/v1alpha1",
					Kind:              "NFSStorageClass",
					NameSelector:      "",
					NamespaceSelector: "",
					LabelSelector:     "",
					FieldSelector:     "",
					ExecuteHookOnEvents: [ 'Added', 'Modified', 'Deleted' ],
					ExecuteHookOnSynchronization: true,
					JqFilter: ".spec.workloadNodes",
					AllowFailure: "",
					ResynchronizationPeriod:
				},
			},


			/*
				configVersion: v1
				kubernetes:
				- name: nfs-storage-classes
				apiVersion: storage.deckhouse.io/v1alpha1
				kind: NFSStorageClass
				includeSnapshotsFrom:
				- nfs-storage-classes
				executeHookOnEvent: [ "Added", "Modified", "Deleted" ]
				executeHookOnSynchronization: true
				keepFullObjectsInMemory: false
				jqFilter: ".spec.workloadNodes"
				queue: /modules/csi-nfs
				settings:
				executionMinInterval: 3s
				executionBurst: 1
			*/
			Settings: struct {

			}{}
			Schedule: []pkg.ScheduleConfig{
				{Name: "daily", Crontab: "40 12 * * *"},
			},
			Queue: fmt.Sprintf("modules/%s", consts.ModuleName),
		},
		mainHook,
	)
)

type WorkloadNode struct {
	APIVersion string           `json:"apiVersion"`
	Kind       string           `json:"kind"`
	Metadata   NodeInfoMetadata `json:"metadata"`
}


func mainHook(ctx context.Context, input *pkg.HookInput)error {
	fmt.Println("Scheduler extender enabler hook started")
	shouldEnable := false

	snapshots := input.Snapshots.Get("nfs-storage-classes")
	if len(snapshots) == 0 {
		log.Println("No snapshots found")
		return nil
	}

	nodeInfo := new(NodeInfo)

	for _, snapshot := range snapshots {
		fmt.Printf("get snapshot: %v\n", snapshot)

		filterResult := snapshot.UnmarshalTo()
		if len(snapshots) == 0 {
			log.Println("Filter result is empty")
			return nil
		}

		fmt.Printf("get filter result: %v\n", filterResult)



		nodeSelector, ok := filterResult["nodeSelector"].(map[string]interface{})





		if !ok {
			fmt.Println("nodeSelector is empty")
			continue
		}

		fmt.Println("NodeSelector is not empty. Should enable scheduler extender")
		shouldEnable = true
		break
	}

	if shouldEnable {
		fmt.Println("Enable scheduler extender")
		input.Values.Set("csiNfs.internal.schedulerExtenderEnabled", true)
	} else {
		fmt.Println("Disable scheduler extender")
		input.Values.Set("csiNfs.internal.schedulerExtenderEnabled", false)
	}
	return nil
}


