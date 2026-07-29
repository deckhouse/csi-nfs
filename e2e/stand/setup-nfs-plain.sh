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

# Idempotent setup of a plain (non-TLS) kernel NFS server for the csi-nfs e2e suite.
#
#   $1 = "v3"  -> NFSv3 only (v4 disabled), rpcbind/statd/mountd on fixed ports
#   $1 = "v4"  -> NFSv4.1/4.2 only (v3 disabled)
set -euo pipefail

MODE="${1:?usage: setup-nfs-plain.sh v3|v4}"
CLIENTS="${NFS_CLIENTS:-10.0.0.0/8}"

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq nfs-kernel-server nfs-common >/dev/null

# Export roots. Two per server so several NFSStorageClasses can coexist on one
# host without sharing a directory (e.g. Delete vs Retain reclaim policies).
for d in /srv/nfs/e2e /srv/nfs/e2e-alt; do
  mkdir -p "$d"
  chmod 0777 "$d"
done

# no_root_squash is mandatory: the csi-nfs controller mkdir/chowns <share>/<PV>
# as root, and DVP virtual disks require it too (see csi-nfs docs/README.md).
cat >/etc/exports <<EOF
# Managed by csi-nfs e2e setup. Do not edit by hand.
/srv/nfs/e2e      ${CLIENTS}(rw,sync,no_subtree_check,no_root_squash)
/srv/nfs/e2e-alt  ${CLIENTS}(rw,sync,no_subtree_check,no_root_squash)
EOF

case "$MODE" in
  v3)
    # v3 only: pin the ancillary RPC services to fixed ports so a firewalled
    # client subnet stays workable, and turn v4 off entirely.
    cat >/etc/nfs.conf.d/e2e.conf <<'EOF'
[nfsd]
vers3 = y
vers4 = n
vers4.0 = n
vers4.1 = n
vers4.2 = n
threads = 16

[mountd]
port = 20048

[statd]
port = 32765
outgoing-port = 32766

[lockd]
port = 32803
udp-port = 32769
EOF
    systemctl enable --now rpcbind.service rpcbind.socket
    ;;
  v4)
    # v4.1/4.2 only. rpcbind is not needed and is masked so nothing registers.
    cat >/etc/nfs.conf.d/e2e.conf <<'EOF'
[nfsd]
vers3 = n
vers4 = y
vers4.0 = n
vers4.1 = y
vers4.2 = y
threads = 16
EOF
    ;;
  *)
    echo "unknown mode: $MODE" >&2
    exit 2
    ;;
esac

systemctl enable --now nfs-server.service
systemctl restart nfs-server.service
exportfs -ra

echo "=== exportfs -v ==="
exportfs -v
echo "=== enabled versions ==="
cat /proc/fs/nfsd/versions
echo "=== rpcinfo ==="
rpcinfo -p 2>/dev/null | head -20 || echo "(rpcbind not running - expected for v4-only)"
