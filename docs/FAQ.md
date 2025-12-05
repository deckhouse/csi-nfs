---
title: "The csi-nfs module: FAQ"
description: CSI NFS module FAQ
---

## How to check the module's functionality?

To do this, you need to check the pod statuses in the `d8-csi-nfs` namespace. All pods should be in the `Running` or `Completed` state and should be running on all nodes. You can check this with the following command:

```shell
kubectl -n d8-csi-nfs get pod -owide -w
```

## Is it possible to change the parameters of an NFS server for already created PVs?

No, the connection data to the NFS server is stored directly in the PV manifest and cannot be changed. Changing the StorageClass also does not affect the connection settings in already existing PVs.

## How to create volume snapshots?

{{< alert level="warning" >}}
**Warning about using snapshots (Volume Snapshots)**

When creating snapshots of NFS volumes, it's important to understand their creation scheme and associated limitations. We recommend avoiding the use of snapshots in csi-nfs when possible:

1. The CSI driver creates a snapshot at the NFS server level.
2. For this, tar is used, which packages the volume contents, with all the limitations that may arise from this.
3. **Before creating a snapshot, be sure to stop the workload** (pods) using the NFS volume.
4. NFS does not ensure atomicity of operations at the file system level when creating a snapshot.
{{< /alert >}}

In `csi-nfs`, snapshots are created by archiving the volume folder. The archive is stored in the root of the NFS server folder specified in the `spec.connection.share` parameter.

1. Enable the `snapshot-controller`:

   ```yaml
   kubectl apply -f -<<EOF
   apiVersion: deckhouse.io/v1alpha1
   kind: ModuleConfig
   metadata:
     name: snapshot-controller
   spec:
     enabled: true
     version: 1
   EOF
   ```

1. Create volume snapshots. To do this, run the following command, specifying the required parameters:

   ```yaml
   kubectl apply -f -<<EOF
   apiVersion: snapshot.storage.k8s.io/v1
   kind: VolumeSnapshot
   metadata:
     name: my-snapshot
     namespace: <namespace name where the PVC is located>
   spec:
     volumeSnapshotClassName: csi-nfs-snapshot-class
     source:
       persistentVolumeClaimName: <PVC name for which you need to create the snapshot>
   EOF
   ```

1. Check the status of the created snapshot using the following command:

   ```shell
   kubectl get volumesnapshot
   ```

This command will display a list of all snapshots and their current status.

## Why are PVs created in a StorageClass with RPC-with-TLS support not being deleted, along with their `<PV name>` directories on the NFS server?

If the [NFSStorageClass](./cr.html#nfsstorageclass) resource was configured with RPC-with-TLS support, there might be a situation where the PV fails to be deleted.
This happens due to the removal of the secret (for example, after deleting `NFSStorageClass`), which holds the mount options. As a result, the controller is unable to mount the NFS folder to delete the `<PV name>` folder.

## How to place multiple CAs in the `tlsParameters.ca` setting in ModuleConfig?

- for two CAs
```shell
cat CA1.crt CA2.crt | base64 -w0
```

- for three CAs
```shell
cat CA1.crt CA2.crt CA3.crt | base64 -w0
```

- and so on

## What are the requirements for a Linux distribution to deploy an NFS server with RPC-with-TLS support?

- The kernel must be built with the `CONFIG_TLS` and `CONFIG_NET_HANDSHAKE` options enabled;
- The nfs-utils package (or nfs-common in Debian-based distributions) must be version >= 2.6.3.



## Installing NFS server with RPC-with-TLS 2.7.1 support on RedOS 8


### Generating TLS certificates (optional)
#### Generating the root CA certificate

```
# mkdir /etc/ssl/tlshd
# cd /etc/ssl/tlshd
# openssl genrsa -out ca.key 4096
# openssl req -x509 -new -nodes -key ca.key -sha256 -days 1826 -out ca.crt -subj "/CN=sidorov/O=Flant"
```

#### Generating the server certificate, where **10.0.5.111** is the IP address of the NFS server

```
# openssl req -new -nodes -out nfs_tlshd.csr -newkey rsa:4096 -keyout nfs_tlshd.key -subj "/CN=nfs/O=Flant"

# cat > "nfs.v3.ext" << EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment
subjectAltName = @alt_names
[alt_names]
IP.0 = 10.0.5.111
EOF

# openssl x509 -req -in nfs_tlshd.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out nfs.crt -days 730 -sha256 -extfile nfs.v3.ext
```

#### Generating the client certificate

```
# openssl req -new -nodes -out nfs_client.csr -newkey rsa:4096 -keyout nfs_client.key -subj "/CN=nfs-client/O=Flant"

# cat > "nfs_client.v3.ext" << EOF
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, nonRepudiation, keyEncipherment, dataEncipherment
subjectAltName = @alt_names
[alt_names]
IP.0 = 10.0.5.117
EOF

# openssl x509 -req -in nfs_client.csr -CA ca.crt -CAkey ca.key -CAcreateserial -out nfs_client.crt -days 730 -sha256 -extfile nfs_client.v3.ext
```

### Installing tlshd

#### Checking the kernel

For RPC-with-TLS to work, a Linux kernel newer than 6.4 is required:

```
# uname -r
6.6.76-1.red80.x86_64
```

The kernel is compiled with the necessary parameters:

```
# grep -P 'CONFIG_TLS|CONFIG_NET_HANDSHAK' /boot/config-$(uname -r)
CONFIG_TLS=m
CONFIG_TLS_DEVICE=y
# CONFIG_TLS_TOE is not set
CONFIG_NET_HANDSHAKE=y
```

 

#### Building tlshd from source

Installing dependencies: 

```
# dnf install automake gcc gnutls-devel keyutils-libs-devel libnl3-devel glib2-devel wget
```

Download the source code and compile:

```
# wget https://github.com/oracle/ktls-utils/releases/download/ktls-utils-0.11/ktls-utils-0.11.tar.gz
# tar xf ktls-utils-0.11.tar.gz 
# cd ktls-utils-0.11/ 
# ./autogen.sh 
# ./configure —with-systemd 
# make 
# make install 
# systemctl daemon-reload 
# systemctl enable —now tlshd
```

### Configuring tlshd to use TLS

Edit the tlshd configuration

```
# cat /etc/tlshd.conf
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

Restart the tlshd service

```
# systemctl restart tlshd
```

### Installing nfs-utils version 2.7.1

Check that NFS is enabled in the kernel:

```
# grep -P 'CONFIG_NFSD_V4|CONFIG_NETWORK_FILESYSTEMS|CONFIG_NFS_FS|CONFIG_NFSD' /boot/config-$(uname -r)
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

Building nfs-utils from source:

Install dependencies:

```
# dnf install libxml2-devel rpcgen libtirpc-devel libuuid-devel libevent-devel sqlite-devel device-mapper-devel sssd-krb5-common krb5-devel gssproxy libev libnfsidmap libverto-libev quota quota-nls rpcbind sssd-nfs-idmap
```

 

Download the source code and compile:

```
# wget https://www.kernel.org/pub/linux/utils/nfs-utils/2.7.1/nfs-utils-2.7.1.tar.gz
# tar xf nfs-utils-2.7.1.tar.gz
# cd nfs-utils-2.7.1
# ./configure --prefix=/usr --sysconfdir=/etc/ --sbindir=/usr/sbin --enable-nfsv4server —with-systemd
# make
# make install
# chmod u+w,go+r /usr/sbin/mount.nfs
```

Create an NFS share and configure it:

```
# echo '/mnt/shared 10.0.5.0/24(rw,sync,no_subtree_check,no_root_squash,xprtsec=mtls)' >> /etc/exports
# exportfs -a
# exportfs -v
/mnt/shared     10.0.5.0/24(sync,wdelay,hide,no_subtree_check,sec=sys,rw,secure,no_root_squash,no_all_squash,xprtsec=mtls)
```

### Configuring csi-nfs

Enable the module with TLS configured:

```
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

Wait for the module to transition to the Ready state:

```
# kubectl get module csi-nfs -w
```

Create NFSStorageClass

```
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

Create a deployment with a disk request in the created NFS

```
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

The tlshd logs will contain information about successful connection to the NFS server

```
# journalctl -fu tlshd
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
