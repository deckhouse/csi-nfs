#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${NAMESPACE:-nfs-test}"
SECRET_NAME="${SECRET_NAME:-nfs-server-tls-certs}"

WORKDIR=$(mktemp -d)
trap 'rm -rf "$WORKDIR"' EXIT
cd "$WORKDIR"

openssl genrsa -out ca.key 4096
openssl req -x509 -new -nodes -key ca.key -sha256 -days 3650 \
  -out ca.crt -subj "/CN=nfs-test-ca/O=Test"

openssl genrsa -out server.key 4096
openssl req -new -nodes -key server.key -out server.csr \
  -subj "/CN=nfs-server-tls/O=Test"
cat > server.ext <<'EOF'
subjectAltName=DNS:nfs-server-tls,DNS:nfs-server-tls.nfs-test,DNS:nfs-server-tls.nfs-test.svc,DNS:nfs-server-tls.nfs-test.svc.cluster.local
EOF
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out server.crt -days 3650 -sha256 -extfile server.ext

openssl genrsa -out client.key 4096
openssl req -new -nodes -key client.key -out client.csr \
  -subj "/CN=nfs-client/O=Test"
openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out client.crt -days 3650 -sha256

kubectl create secret generic "$SECRET_NAME" \
  --namespace="$NAMESPACE" \
  --from-file=ca.crt \
  --from-file=server.crt \
  --from-file=server.key \
  --from-file=client.crt \
  --from-file=client.key \
  --dry-run=client -o yaml
