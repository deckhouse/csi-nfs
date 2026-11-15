---
title: "Module csi-nfs: examples"
description: Examples of configuring the csi-nfs module.
---

## Configuration of the module with RPC-with-TLS support

Example ModuleConfig with TLS parameters:

```yaml
apiVersion: deckhouse.io/v1alpha1
kind: ModuleConfig
metadata:
  name: csi-nfs
spec:
  enabled: true
  version: 1
  settings:
    tlsParameters:
      ca: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUZFVENDQXZtZ...
      mtls:
        clientCert: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1J...
        clientKey: LS0tLS1CRUdJTiBQUklWQVRFIEtFWS0tLS0tCk1JSUpRd0lCQ...
```

## Creating a StorageClass with RPC-with-TLS support

Example NFSStorageClass with RPC-with-TLS enabled:

```yaml
apiVersion: storage.deckhouse.io/v1alpha1
kind: NFSStorageClass
metadata:
  name: nfs-storage-class
spec:
  connection:
    host: nfs-server-name.io
    share: /
    nfsVersion: "4.1"
    tls: true
    mtls: true
  reclaimPolicy: Delete
  volumeBindingMode: WaitForFirstConsumer
```

## A CA of its own for a StorageClass

An NFSStorageClass can carry the CA that verifies its NFS server in `spec.tlsParameters.ca`, instead of `tlsParameters.ca` of the module:

```yaml
apiVersion: storage.deckhouse.io/v1alpha1
kind: NFSStorageClass
metadata:
  name: nfs-storage-class-own-ca
spec:
  connection:
    host: nfs-server-name.io
    share: /
    nfsVersion: "4.1"
    tls: true
  tlsParameters:
    ca: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUZFVENDQXZtZ...
  reclaimPolicy: Delete
  volumeBindingMode: WaitForFirstConsumer
```

- Such a class does not need `tlsParameters.ca` in the ModuleConfig.
- A node has one truststore for all its mounts: it trusts the CA of the module and the CAs of every class together, so a CA of a class adds trust rather than limiting it to that class.
- A changed CA reaches the nodes without restarting the pods of the module, once kubelet refreshes the `tlshd-conf` ConfigMap, usually within a minute or two.

## The client certificate of mTLS

With `mtls: true` the nodes present a client certificate to the NFS server:

- By default it is a certificate the module issues itself: valid for ten years and renewed half a year before it expires, by the same CA, which is valid for twenty years, so the NFS servers keep trusting the nodes through a renewal. Its CA is in `status.clientCA` of every NFSStorageClass with `mtls: true`, Base64-encoded PEM; the NFS server has to trust it.
- `tlsParameters.mtls.clientCert` and `clientKey` of the ModuleConfig replace it, both together. With `tlsParameters.mtls.clientCA`, the CA of that certificate, `status.clientCA` carries that CA instead; without it, `status.clientCA` is empty.
- A node presents the same client certificate on every mount with `mtls`, so every NFS server those classes use has to trust its CA.
- The key is kept in the Secret `d8-csi-nfs/tlshd-client-key`; a renewed certificate reaches the nodes without restarting the pods of the module.

## Hardened mount options

`spec.mountOptions.nosuid` and `spec.mountOptions.nodev` mount the volumes of a class with `nosuid` and `nodev`: on the node, the set-user-ID bits of the files on the share do not take effect, and device files on it cannot be opened.

The flags belong to the mount on the node. The container runtime bind-mounts the volume into a container without them (checked with containerd 1.7), so inside a pod they do not stop a set-user-ID binary; `securityContext.allowPrivilegeEscalation: false` of the container, which the `Restricted` Pod Security Standard requires, does.

```yaml
spec:
  mountOptions:
    nosuid: true
    nodev: true
```

## Installing an NFS server with RPC-with-TLS 2.7.1 support on RED OS 8

### Generating TLS certificates (optional)

#### Generating the root CA certificate

```shell
mkdir /etc/ssl/tlshd
cd /etc/ssl/tlshd
openssl genrsa -out ca.key 4096
openssl req -x509 -new -nodes -key ca.key -sha256 -days 1826 -out ca.crt -subj "/CN=sidorov/O=Flant"
```

#### Generating the server certificate, where **10.0.5.111** is the IP address of the NFS server

```shell
openssl req -new -nodes -out nfs_tlshd.csr -newkey rsa:4096 -keyout nfs_tlshd.key -subj "/CN=nfs/O=Flant"

cat > "nfs.v3.ext" << EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment
subjectAltName = @alt_names
[alt_names]
IP.0 = 10.0.5.111
EOF

openssl x509 -req -in nfs_tlshd.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out nfs.crt -days 730 -sha256 -extfile nfs.v3.ext
```

#### Generating the client certificate

```shell
openssl req -new -nodes -out nfs_client.csr -newkey rsa:4096 -keyout nfs_client.key -subj "/CN=nfs-client/O=Flant"

cat > "nfs_client.v3.ext" << EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment
subjectAltName = @alt_names
[alt_names]
IP.0 = 10.0.5.117
EOF

openssl x509 -req -in nfs_client.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out nfs_client.crt -days 730 -sha256 -extfile nfs_client.v3.ext
```

### Installing tlshd

#### Checking the kernel

RPC-with-TLS requires a Linux kernel built with the `CONFIG_TLS` and `CONFIG_NET_HANDSHAKE` options (usually kernels 6.5 and later). Check the kernel version:

```shell
uname -r
```

Example output:

```console
6.6.76-1.red80.x86_64
```

Check that the kernel is built with the required options:

```shell
grep -P 'CONFIG_TLS|CONFIG_NET_HANDSHAK' /boot/config-$(uname -r)
```

Example output:

```console
CONFIG_TLS=m
CONFIG_TLS_DEVICE=y
# CONFIG_TLS_TOE is not set
CONFIG_NET_HANDSHAKE=y
```

#### Building tlshd from source

Install the dependencies:

```shell
dnf install automake gcc gnutls-devel keyutils-libs-devel libnl3-devel glib2-devel wget
```

Download the source code and build it:

```shell
wget https://github.com/oracle/ktls-utils/releases/download/ktls-utils-0.11/ktls-utils-0.11.tar.gz
tar xf ktls-utils-0.11.tar.gz
cd ktls-utils-0.11/
./autogen.sh
./configure --with-systemd
make
make install
systemctl daemon-reload
systemctl enable --now tlshd
```

### Checking the tlshd configuration for TLS

Check the tlshd configuration:

```shell
cat /etc/tlshd.conf
```

Example output:

```console
[debug]
loglevel=2
tls=2
nl=0

[authenticate]

[authenticate.client]

[authenticate.server]
x509.truststore= /etc/ssl/tlshd/ca.crt
x509.certificate= /etc/ssl/tlshd/nfs.crt
x509.private_key= /etc/ssl/tlshd/nfs_tlshd.key
```

Restart the tlshd service:

```shell
systemctl restart tlshd
```

### Installing nfs-utils 2.7.1

Check that NFS is enabled in the kernel:

```shell
grep -P 'CONFIG_NFSD_V4|CONFIG_NETWORK_FILESYSTEMS|CONFIG_NFS_FS|CONFIG_NFSD' /boot/config-$(uname -r)
```

Example output:

```console
CONFIG_NETWORK_FILESYSTEMS=y
CONFIG_NFS_FS=m
CONFIG_NFS_FSCACHE=y
CONFIG_NFSD=m
# CONFIG_NFSD_V2 is not set
CONFIG_NFSD_V3_ACL=y
CONFIG_NFSD_V4=y
CONFIG_NFSD_PNFS=y
CONFIG_NFSD_BLOCKLAYOUT=y
CONFIG_NFSD_SCSILAYOUT=y
CONFIG_NFSD_FLEXFILELAYOUT=y
CONFIG_NFSD_V4_2_INTER_SSC=y
CONFIG_NFSD_V4_SECURITY_LABEL=y
```

Build nfs-utils from source:

Install the dependencies:

```shell
dnf install libxml2-devel rpcgen libtirpc-devel libuuid-devel libevent-devel sqlite-devel device-mapper-devel sssd-krb5-common krb5-devel gssproxy libev libnfsidmap libverto-libev quota quota-nls rpcbind sssd-nfs-idmap
```

Download the source code and build it:

```shell
wget https://www.kernel.org/pub/linux/utils/nfs-utils/2.7.1/nfs-utils-2.7.1.tar.gz
tar xf nfs-utils-2.7.1.tar.gz
cd nfs-utils-2.7.1
./configure --prefix=/usr --sysconfdir=/etc/ --sbindir=/usr/sbin --enable-nfsv4server --with-systemd
make
make install
chmod u+w,go+r /usr/sbin/mount.nfs
```

Create the shared NFS directory and configure it:

```shell
echo '/mnt/shared 10.0.5.0/24(rw,sync,no_subtree_check,no_root_squash,xprtsec=mtls)' >> /etc/exports
exportfs -a
exportfs -v
```

Example output:

```console
/mnt/shared     10.0.5.0/24(sync,wdelay,hide,no_subtree_check,sec=sys,rw,secure,no_root_squash,no_all_squash,xprtsec=mtls)
```

### Configuring csi-nfs

Enable the module with TLS configured:

```yaml
apiVersion: deckhouse.io/v1alpha1
kind: ModuleConfig
metadata:
  name: csi-nfs
spec:
  enabled: true
  settings:
    tlsParameters:
      ca: <cat ca.crt | base64 -w0>
      mtls:
        clientCert: <cat nfs_client.crt | base64 -w0>
        clientKey: <cat nfs_client.key | base64 -w0>
  version: 1
```

Wait for the module to become `Ready`:

```shell
d8 k get module csi-nfs -w
```

Create an NFSStorageClass:

```yaml
apiVersion: storage.deckhouse.io/v1alpha1
kind: NFSStorageClass
metadata:
  name: nfs-storage-class
spec:
  connection:
    host: 10.0.5.111
    share: /mnt/shared
    nfsVersion: "4.2"
    mtls: true
    tls: true
  reclaimPolicy: Delete
  volumeBindingMode: WaitForFirstConsumer
```

### Testing

Create a Deployment that requests a disk in the created NFS.

Example manifests:

```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: nfs-one
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-deployment
  namespace: nfs-one
spec:
  replicas: 1
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
      - name: nginx
        image: nginx:latest
        volumeMounts:
        - name: nfs-volume
          mountPath: /mnt/share
      volumes:
      - name: nfs-volume
        persistentVolumeClaim:
          claimName: nfs-pvc
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: nfs-pvc
  namespace: nfs-one
spec:
  accessModes:
    - ReadWriteMany
  resources:
    requests:
      storage: 1Gi
  storageClassName: nfs-storage-class
```

The tlshd logs show a successful connection to the NFS server.

```shell
journalctl -fu tlshd
```

Example output:

```console
Mar 24 18:21:43 nfs-source-ee-test-sidorov-arch tlshd[36512]: The certificate is trusted.
Mar 24 18:21:43 nfs-source-ee-test-sidorov-arch tlshd[36512]: The peer offered 1 certificate(s).
Mar 24 18:21:43 nfs-source-ee-test-sidorov-arch tlshd[36512]: Session description: (TLS1.3)-(ECDHE-SECP384R1)-(RSA-PSS-RSAE-SHA384)-(AES-256-GCM)
Mar 24 18:21:43 nfs-source-ee-test-sidorov-arch tlshd[36512]: Handshake with unknown (10.0.5.110) was successful
Mar 24 18:26:42 nfs-source-ee-test-sidorov-arch tlshd[36527]: Querying the handshake service
Mar 24 18:26:42 nfs-source-ee-test-sidorov-arch tlshd[36527]: Parsing a valid netlink message
Mar 24 18:26:42 nfs-source-ee-test-sidorov-arch tlshd[36527]: No peer identities found
Mar 24 18:26:42 nfs-source-ee-test-sidorov-arch tlshd[36527]: No certificates found
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: Name or service not known
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: System config file: /etc/crypto-policies/back-ends/gnutls.config
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: Server x.509 truststore is /etc/ssl/tlshd/ca.crt
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: System trust: Loaded 1 certificate(s).
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: Retrieved 1 x.509 server certificate(s) from /etc/ssl/tlshd/nfs.crt
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: Retrieved private key from /etc/ssl/tlshd/nfs_tlshd.key
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: gnutls(2): checking 13.02 (GNUTLS_AES_256_GCM_SHA384) for compatibility
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: gnutls(2): Selected (RSA) cert
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: gnutls(2): EXT[0x384dc0d0]: server generated SECP384R1 shared key
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: The certificate is trusted.
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: The peer offered 1 certificate(s).
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: Session description: (TLS1.3)-(ECDHE-SECP384R1)-(RSA-PSS-RSAE-SHA384)-(AES-256-GCM)
Mar 24 18:26:43 nfs-source-ee-test-sidorov-arch tlshd[36527]: Handshake with unknown (10.0.5.117) was successful
```
