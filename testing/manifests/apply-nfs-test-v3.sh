#!/usr/bin/env bash
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
