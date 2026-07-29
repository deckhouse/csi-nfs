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

# Generate the RPC-with-TLS PKI for the csi-nfs e2e stand.
#
#   gen-certs.sh <outdir> <server-ip> [<server-ip> ...]
#
#   ca.crt / ca.key            - test root CA
#   server.crt / server.key    - NFS server identity, ONE certificate carrying an
#                                IP SAN for every address given on the command
#                                line, so the same pair can be installed on the
#                                tls host and the mtls host
#   client.crt / client.key    - cluster-wide client identity used for mTLS
#
# The IP SANs matter: tlshd 1.x verifies the server against the peer name the
# kernel passes with the handshake request, which for NFS is the mount's server
# string - i.e. NFSStorageClass spec.connection.host. A certificate without an IP
# SAN for that exact address is rejected with "Certificate owner unexpected".
set -euo pipefail

OUT="${1:?usage: gen-certs.sh <outdir> <server-ip> [<server-ip> ...]}"
shift
[ "$#" -ge 1 ] || { echo "usage: gen-certs.sh <outdir> <server-ip> [<server-ip> ...]" >&2; exit 2; }
DAYS=3650

mkdir -p "$OUT"
SERVER_IPS=("$@")
cd "$OUT"

openssl genrsa -out ca.key 4096 2>/dev/null
openssl req -x509 -new -nodes -key ca.key -sha256 -days $DAYS -out ca.crt \
  -subj "/CN=csi-nfs-e2e-ca/O=Flant"

san="DNS:nfs-tls"
for ip in "${SERVER_IPS[@]}"; do
  san="${san},IP:${ip}"
done

cat >server.ext <<EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage=digitalSignature,keyEncipherment
extendedKeyUsage=serverAuth
subjectAltName=${san}
EOF
openssl req -new -nodes -out server.csr -newkey rsa:4096 -keyout server.key \
  -subj "/CN=nfs-tls/O=Flant" 2>/dev/null
openssl x509 -req -in server.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out server.crt -days $DAYS -sha256 -extfile server.ext 2>/dev/null

# The client identity is cluster-wide (one cert for every csi-nfs node), so it
# carries no IP SAN - nfsd/tlshd verify it against the CA, not against a name.
cat >client.ext <<EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage=digitalSignature,keyEncipherment
extendedKeyUsage=clientAuth
subjectAltName=DNS:csi-nfs-client
EOF
openssl req -new -nodes -out client.csr -newkey rsa:4096 -keyout client.key \
  -subj "/CN=csi-nfs-client/O=Flant" 2>/dev/null
openssl x509 -req -in client.csr -CA ca.crt -CAkey ca.key -CAcreateserial \
  -out client.crt -days $DAYS -sha256 -extfile client.ext 2>/dev/null

rm -f server.csr client.csr server.ext client.ext
chmod 600 ./*.key

echo "=== generated in $OUT ==="
for f in ca.crt server.crt client.crt; do
  echo "--- $f"
  openssl x509 -in "$f" -noout -subject -issuer -dates -ext subjectAltName 2>/dev/null | sed 's/^/    /'
done
