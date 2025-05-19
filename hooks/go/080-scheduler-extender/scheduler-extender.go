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
	"fmt"
	"log"
)

type Config struct {
	ConfigVersion string `json:"configVersion"`
	Kubernetes    []struct {
		Name                         string   `json:"name"`
		APIVersion                   string   `json:"apiVersion"`
		Kind                         string   `json:"kind"`
		IncludeSnapshotsFrom         []string `json:"includeSnapshotsFrom"`
		ExecuteHookOnEvent           []string `json:"executeHookOnEvent"`
		ExecuteHookOnSynchronization bool     `json:"executeHookOnSynchronization"`
		KeepFullObjectsInMemory      bool     `json:"keepFullObjectsInMemory"`
		JqFilter                     string   `json:"jqFilter"`
		Queue                        string   `json:"queue"`
	} `json:"kubernetes"`
	Settings struct {
		ExecutionMinInterval string `json:"executionMinInterval"`
		ExecutionBurst       int    `json:"executionBurst"`
	} `json:"settings"`
}

type Snapshot struct {
	FilterResult map[string]interface{} `json:"filterResult"`
}

type Context struct {
	Snapshots map[string][]Snapshot
	Values    map[string]interface{}
}

func run(hookFunc func(ctx Context), config Config) {

	ctx := Context{
		Snapshots: getSnapshotsFromConfig(config),
		Values:    map[string]interface{}{},
	}

	hookFunc(ctx)
}

func getSnapshotsFromConfig(config Config) map[string][]Snapshot {
	return map[string][]Snapshot{
		"nfs-storage-classes": {
			{FilterResult: map[string]interface{}{"nodeSelector": map[string]interface{}{"key": "value"}}},
		},
	}
}

func mainHook(ctx Context) {
	fmt.Println("Scheduler extender enabler hook started")
	shouldEnable := false

	snapshots, ok := ctx.Snapshots["nfs-storage-classes"]
	if !ok {
		log.Println("No snapshots found")
		return
	}

	for _, snapshot := range snapshots {
		fmt.Printf("get snapshot: %v\n", snapshot)

		filterResult, ok := snapshot.FilterResult["filterResult"].(map[string]interface{})
		if !ok {
			fmt.Println("filter result is empty")
			continue
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
		setValue("csiNfs.internal.shedulerExtenderEnabled", ctx.Values, true)
	} else {
		fmt.Println("Disable scheduler extender")
		setValue("csiNfs.internal.shedulerExtenderEnabled", ctx.Values, false)
	}
}

func setValue(key string, values map[string]interface{}, value interface{}) {
	// Sets the value for a given key in the context's values.
	values[key] = value
}

func init() {
	config := Config{
		ConfigVersion: "v1",
		Kubernetes: []struct {
			Name                         string   `json:"name"`
			APIVersion                   string   `json:"apiVersion"`
			Kind                         string   `json:"kind"`
			IncludeSnapshotsFrom         []string `json:"includeSnapshotsFrom"`
			ExecuteHookOnEvent           []string `json:"executeHookOnEvent"`
			ExecuteHookOnSynchronization bool     `json:"executeHookOnSynchronization"`
			KeepFullObjectsInMemory      bool     `json:"keepFullObjectsInMemory"`
			JqFilter                     string   `json:"jqFilter"`
			Queue                        string   `json:"queue"`
		}{
			{
				Name:                         "nfs-storage-classes",
				APIVersion:                   "storage.deckhouse.io/v1alpha1",
				Kind:                         "NFSStorageClass",
				IncludeSnapshotsFrom:         []string{"nfs-storage-classes"},
				ExecuteHookOnEvent:           []string{"Added", "Modified", "Deleted"},
				ExecuteHookOnSynchronization: true,
				KeepFullObjectsInMemory:      false,
				JqFilter:                     ".spec.workloadNodes",
				Queue:                        "/modules/csi-nfs",
			},
		},
		Settings: struct {
			ExecutionMinInterval string `json:"executionMinInterval"`
			ExecutionBurst       int    `json:"executionBurst"`
		}{
			ExecutionMinInterval: "3s",
			ExecutionBurst:       1,
		},
	}

	run(mainHook, config)
}
