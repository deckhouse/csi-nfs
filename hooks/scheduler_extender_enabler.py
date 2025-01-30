#!/usr/bin/env python3
#
# Copyright 2025 Flant JSC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.


# import yaml
# from deckhouse import hook
from deckhouse import hook

from lib.hooks.hook import Hook
from lib.module import values as module_values


config = """
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
"""

def main(ctx: hook.Context):
    print("Scheduler extender enabler hook started")
    should_enable = False
    snapshots = ctx.snapshots.get("nfs-storage-classes", [])
    for snapshot in snapshots:
        print(f"get snapshot: {snapshot}")
        filter_result = snapshot.get("filterResult", [])
        if not filter_result:
            print(f"filter result is empty")
            continue
        print(f"get filter result: {filter_result}")
        nodeSelector = filter_result.get("nodeSelector", {})
        print(f"get nodeSelector: {nodeSelector}")
        if not nodeSelector:
            print(f"nodeSelector is empty")
            continue
        print("NodeSelector is not empty. Should enable scheduler extender")
        should_enable = True
        break
    if should_enable:
        print("Enable scheduler extender")
        module_values.set_value(f"csiNfs.internal.shedulerExtenderEnabled", ctx.values, True)
    else:
        print("Disable scheduler extender")
        module_values.set_value(f"csiNfs.internal.shedulerExtenderEnabled", ctx.values, False)


if __name__ == "__main__":
    hook.run(main, config=config)
