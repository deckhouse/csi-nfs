#!/usr/bin/env bash

# Copyright 2026 Flant JSC
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

set -euo pipefail

# NFSv3 mountd/statd register on the node IP (hostNetwork). Service DNS breaks v3 mount
# with "Protocol not supported" — NFSStorageClass.host must be InternalIP of the node
# where nfs-server-v3 runs.

NS=nfs-test
SC=nfs-test-v3

if ! kubectl get ns "$NS" >/dev/null 2>&1; then
  echo "Namespace $NS not found. Run: kubectl apply -k testing/manifests/" >&2
  exit 1
fi

kubectl -n "$NS" wait --for=condition=Ready pod -l app=nfs-server-v3 --timeout=120s

NODE=$(kubectl -n "$NS" get pod -l app=nfs-server-v3 -o jsonpath='{.items[0].spec.nodeName}')
NODE_IP=$(kubectl get node "$NODE" -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}')

if [[ -z "$NODE_IP" ]]; then
  echo "Failed to resolve InternalIP for node $NODE" >&2
  exit 1
fi

echo "nfs-server-v3 on node $NODE ($NODE_IP)"

EXISTING_HOST=$(kubectl get nfsstorageclass "$SC" -o jsonpath='{.spec.connection.host}' 2>/dev/null || true)
if [[ -n "$EXISTING_HOST" && "$EXISTING_HOST" != "$NODE_IP" ]]; then
  echo "Deleting NFSStorageClass $SC (host was $EXISTING_HOST, need $NODE_IP)"
  kubectl delete nfsstorageclass "$SC" --wait=true
fi

kubectl apply -f - <<EOF
apiVersion: storage.deckhouse.io/v1alpha1
kind: NFSStorageClass
metadata:
  name: $SC
spec:
  connection:
    host: $NODE_IP
    share: /exports
    nfsVersion: "3"
  reclaimPolicy: Delete
  volumeBindingMode: WaitForFirstConsumer
EOF

echo "NFSStorageClass $SC created with host=$NODE_IP"
