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

# Idempotent setup of an RPC-with-TLS (RFC 9289) NFS server for the csi-nfs e2e
# stand. Run as root on a clean Ubuntu 24.04 host:
#
#   $1 = "tls"   -> single export /srv/nfs/tls  with xprtsec=tls
#   $1 = "mtls"  -> single export /srv/nfs/mtls with xprtsec=mtls
#
#   ./gen-certs.sh ./pki <tls-host-ip> <mtls-host-ip>
#   tar cf - -C pki ca.crt server.crt server.key | ssh <server> 'mkdir -p /tmp/pki && tar xf - -C /tmp/pki'
#   ssh <server> 'sudo bash -s tls' < setup-nfs-tls.sh
#
# ONE MODE PER HOST, deliberately. The kernel NFS client keys its transport by
# server address, so once a node holds an mtls mount to a host, a tls mount to
# that same host is refused with "Operation not permitted" (and vice versa) -
# verified on this stand. That is the mechanism behind the "a single NFS server
# cannot simultaneously operate in different security modes" note in the csi-nfs
# docs. Separate hosts let the tls and mtls suites run in one pass.
#
# Requires kernel >= 6.5 with CONFIG_TLS and CONFIG_NET_HANDSHAKE: NFS xprtsec
# support and the kernel handshake upcall both landed in Linux 6.5.
set -euo pipefail

MODE="${1:?usage: setup-nfs-tls.sh tls|mtls}"
case "$MODE" in
  tls|mtls) ;;
  *) echo "unknown mode: $MODE (expected tls or mtls)" >&2; exit 2 ;;
esac

CLIENTS="${NFS_CLIENTS:-10.0.0.0/8}"
PKI_SRC="${PKI_SRC:-/tmp/pki}"
KTLS_VERSION="${KTLS_VERSION:-1.3.1}"

export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq nfs-kernel-server nfs-common ktls-utils >/dev/null

# ---- kernel TLS ------------------------------------------------------------
grep -qE 'CONFIG_NET_HANDSHAKE=[ym]' "/boot/config-$(uname -r)" || {
  echo "kernel $(uname -r) has no CONFIG_NET_HANDSHAKE; RPC-with-TLS cannot work here" >&2
  exit 1
}
modprobe tls
grep -qxF tls /etc/modules-load.d/tls.conf 2>/dev/null || echo tls >/etc/modules-load.d/tls.conf

# ---- tlshd -----------------------------------------------------------------
# Ubuntu 24.04 ships ktls-utils 0.9, which understands only x509.certificate and
# x509.private_key: it silently IGNORES x509.truststore and validates peers
# against the system trust store instead. It also derives the peer name from a
# reverse DNS lookup, so it rejects a server certificate that only carries an IP
# SAN ("Certificate owner unexpected").
#
# The 1.x series reads the peer name the kernel passes with the handshake
# request - for NFS that is the mount's server string, i.e. NFSStorageClass
# spec.connection.host - and matches it against the IP SAN, no DNS involved.
# Build it when the packaged binary lacks truststore support, so the stand
# behaves like the tlshd the csi-nfs module runs on the client side.
if ! grep -aq 'x509.truststore' /usr/sbin/tlshd; then
  echo "packaged tlshd has no x509.truststore support; building ktls-utils ${KTLS_VERSION}"
  apt-get install -y -qq build-essential automake autoconf libtool pkg-config \
    libgnutls28-dev libkeyutils-dev libnl-3-dev libnl-genl-3-dev libglib2.0-dev wget >/dev/null
  workdir=$(mktemp -d)
  trap 'rm -rf "$workdir"' EXIT
  wget -q -O "$workdir/src.tar.gz" \
    "https://github.com/oracle/ktls-utils/archive/refs/tags/ktls-utils-${KTLS_VERSION}.tar.gz"
  tar xf "$workdir/src.tar.gz" -C "$workdir"
  (
    cd "$workdir/ktls-utils-ktls-utils-${KTLS_VERSION}"
    ./autogen.sh >/dev/null
    ./configure --with-systemd --prefix=/usr --sysconfdir=/etc >/dev/null
    make -j"$(nproc)" >/dev/null
  )
  systemctl stop tlshd 2>/dev/null || true
  install -m 0755 "$workdir/ktls-utils-ktls-utils-${KTLS_VERSION}/src/tlshd/tlshd" /usr/sbin/tlshd
fi

# ---- certificates ----------------------------------------------------------
install -d -m 0755 /etc/ssl/tlshd
install -m 0644 "$PKI_SRC/ca.crt"     /etc/ssl/tlshd/ca.crt
install -m 0644 "$PKI_SRC/server.crt" /etc/ssl/tlshd/server.crt
install -m 0600 "$PKI_SRC/server.key" /etc/ssl/tlshd/server.key

# Server side only: this host is NOT a csi-nfs node, so nothing competes for the
# kernel handshake service in its network namespace. On a cluster node the
# csi-nfs module owns that service (its tlshd runs in the CSI node DaemonSet
# with hostNetwork) and masks any host tlshd - which is exactly why the NFS
# servers have to live outside the cluster.
cat >/etc/tlshd.conf <<'EOF'
# Managed by csi-nfs e2e setup.
[debug]
loglevel=1
tls=1
nl=0

[authenticate]

[authenticate.client]

[authenticate.server]
x509.truststore= /etc/ssl/tlshd/ca.crt
x509.certificate= /etc/ssl/tlshd/server.crt
x509.private_key= /etc/ssl/tlshd/server.key
EOF
chmod 0644 /etc/tlshd.conf

# ---- exports ---------------------------------------------------------------
# Exactly one export, in exactly one security mode (see the header).
SHARE="/srv/nfs/${MODE}"
mkdir -p "$SHARE"
chmod 0777 "$SHARE"

cat >/etc/exports <<EOF
# Managed by csi-nfs e2e setup. Do not edit by hand.
${SHARE}  ${CLIENTS}(rw,sync,no_subtree_check,no_root_squash,xprtsec=${MODE})
EOF

cat >/etc/nfs.conf.d/e2e.conf <<'EOF'
[nfsd]
vers3 = n
vers4 = y
vers4.0 = n
vers4.1 = y
vers4.2 = y
threads = 16
EOF

systemctl unmask tlshd.service 2>/dev/null || true
systemctl enable --now tlshd.service
systemctl restart tlshd.service
systemctl enable --now nfs-server.service
systemctl restart nfs-server.service
exportfs -ra

echo "=== exportfs -v ==="
exportfs -v
echo "=== enabled versions ==="
cat /proc/fs/nfsd/versions
echo "=== tlshd ==="
systemctl is-active tlshd.service
tlshd -v 2>&1 | head -1
