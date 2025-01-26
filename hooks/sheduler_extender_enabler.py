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


import yaml
from deckhouse import hook

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
    nscs = ctx.snapshots.get("nfs-storage-classes", [])
    print(f"get nfs-storage-classes: {nscs}")
    

if __name__ == "__main__":
    hook.run(main, config=config)
