#!/usr/bin/env python3
#
# Copyright 2024 Flant JSC
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

import os

import yaml
import kubernetes
from deckhouse import hook

config = """
configVersion: v1
afterDeleteHelm: 10
"""

namespace_name = 'd8-csi-nfs'
provisioner_name = 'nfs.csi.k8s.io'


def main(ctx: hook.Context):
    kubernetes.config.load_incluster_config()
    secrets = kubernetes.client.CoreV1Api().list_namespaced_secret(namespace_name)
    for item in secrets.items:
        print(f"remove_sc_and_secrets.py: Removing finalizers from {item.metadata.name} secret")

        kubernetes.client.CoreV1Api().patch_namespaced_secret(name=item.metadata.name,
                                                              namespace=namespace_name,
                                                              body={"metadata": {"finalizers": None}})

    sc_objects = kubernetes.client.StorageV1Api().list_storage_class()
    for item in sc_objects.items:
        if item.provisioner == provisioner_name:
            print(f"remove_sc_and_secrets.py: Removing finalizers from {item.metadata.name} storage class")
            kubernetes.client.StorageV1Api().patch_storage_class(name=item.metadata.name,
                                                                 body={"metadata": {"finalizers": None}})
            print(f"remove_sc_and_secrets.py: Removing {item.metadata.name} storage class")
            kubernetes.client.StorageV1Api().delete_storage_class(name=item.metadata.name)

    nsc_objects = kubernetes.client.CustomObjectsApi().list_cluster_custom_object(group='storage.deckhouse.io',
                                                                                  plural='nfsstorageclasses',
                                                                                  version='v1alpha1')
    for item in nsc_objects['items']:
        print(f"remove_sc_and_secrets.py: Removing {item['metadata']['name']} nfs storage class finalizers")
        kubernetes.client.CustomObjectsApi().patch_cluster_custom_object(
            group='storage.deckhouse.io',
            plural='nfsstorageclasses',
            version='v1alpha1',
            name=item['metadata']['name'],
            body={"metadata": {"finalizers": None}}
            )
        print(f"remove_sc_and_secrets.py: Removing {item['metadata']['name']} nfs storage class")
        kubernetes.client.CustomObjectsApi().delete_cluster_custom_object(group='storage.deckhouse.io',
                                                                          plural='nfsstorageclasses',
                                                                          version='v1alpha1',
                                                                          name=item['metadata']['name'])


if __name__ == "__main__":
    hook.run(main, config=config)
