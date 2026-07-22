---
title: "Module csi-nfs"
description: "The csi-nfs module: General Concepts and Principles."
---

The `csi-nfs` module provides a CSI driver for managing NFS volumes in Kubernetes.
Use it to provision PersistentVolumes on an NFS server through [custom resources](./cr.html#nfsstorageclass) NFSStorageClass.

## Main Features

The `csi-nfs` module provides the following capabilities:

- Provision NFS-backed PersistentVolumes through the NFSStorageClass custom resource.
- Support RWO and RWX access modes, including RWX in Deckhouse Virtualization Platform.
- Restrict volume mounting to selected cluster nodes with the [`workloadNodes`](cr.html#nfsstorageclass-v1alpha1-spec-workloadnodes) parameter.
- Support RPC-with-TLS mode (`tls` / `mtls`) for connections to an NFS server (in commercial editions of DKP).
- Clean volume data before PV deletion with the [`volumeCleanup`](cr.html#nfsstorageclass-v1alpha1-spec-volumecleanup) parameter (in commercial editions of DKP).

{{< alert level="info" >}}
StorageClasses for the CSI driver `nfs.csi.k8s.io` are created only through the [NFSStorageClass](./cr.html#nfsstorageclass) resource. Creating regular StorageClass resources for this CSI driver is prohibited.
{{< /alert >}}

## System requirements and recommendations

### Requirements

Before using the module, make sure the following requirements are met:

- Use stock kernels provided with [supported distributions](/products/kubernetes-platform/documentation/v1/supported_versions.html#linux);
- Ensure that the NFS server is correctly configured and running:
  - For DKP modules where StorageClass is used, it may be necessary to allow access to clients with root privileges. In Linux, this is implemented via the `no_root_squash` option. On other operating systems and storage systems, a similar setting may have a different name;
  - For virtual disk storage in the [Deckhouse Virtualization Platform](/products/virtualization-platform/documentation/), the `no_root_squash` option is mandatory.
- To support RPC-with-TLS, enable `CONFIG_TLS` and `CONFIG_NET_HANDSHAKE` options in the Linux kernel.
- The [snapshot-controller](/modules/snapshot-controller/) module must be enabled for this module to operate.

### Recommendations

For module pods to restart when the [`tlsParameters`](configuration.html#parameters-tlsparameters) parameter is changed, make sure the [`pod-reloader`](/modules/pod-reloader/) module is enabled (enabled by default).

## Limitations

### Creating volume snapshots

When creating snapshots of NFS volumes, it is important to understand their creation scheme and associated limitations. Avoid using snapshots in `csi-nfs` when possible:

1. The CSI driver creates a snapshot at the NFS server level.
1. For this, tar is used, which packages the volume contents, with all the limitations that may arise from this.
1. **Before creating a snapshot, be sure to stop the workload** (pods) using the NFS volume.
1. NFS does not ensure atomicity of operations at the file system level when creating a snapshot.

### RPC-with-TLS mode limitations

The following limitations apply to RPC-with-TLS mode:

- For the `mtls` security policy, only one client certificate is supported.
- A single NFS server cannot simultaneously operate in different security modes: `tls`, `mtls`, and standard (non-TLS) mode.
- The `tlshd` daemon must not be running on the cluster nodes, otherwise it will conflict with the module daemon. To prevent conflicts when enabling TLS, the third-party `tlshd` is automatically stopped on the nodes and its autostart is disabled.

## Quickstart

Run all commands on a machine that has administrator access to the Kubernetes API.

### Enabling the module

1. Enable the `csi-nfs` module. This will result in the following actions across all cluster nodes:
   - registration of the CSI driver;
   - launch of service pods for the `csi-nfs` components.

   ```shell
   d8 k apply -f - <<EOF
   apiVersion: deckhouse.io/v1alpha1
   kind: ModuleConfig
   metadata:
     name: csi-nfs
   spec:
     enabled: true
     version: 1
   EOF
   ```

1. Wait for the module to become `Ready`:

   ```shell
   d8 k get module csi-nfs -w
   ```

### Creating a StorageClass

To create a StorageClass, use the [NFSStorageClass](./cr.html#nfsstorageclass) resource. Example:

```shell
d8 k apply -f - <<EOF
apiVersion: storage.deckhouse.io/v1alpha1
kind: NFSStorageClass
metadata:
  name: nfs-storage-class
spec:
  connection:
    host: 10.223.187.3
    share: /
    nfsVersion: "4.1"
  reclaimPolicy: Delete
  volumeBindingMode: WaitForFirstConsumer
  workloadNodes:
    nodeSelector:
      matchLabels:
        storage: "true"
EOF
```

CSI driver control pods are placed on cluster nodes according to the summarization of the [`workloadNodes`](cr.html#nfsstorageclass-v1alpha1-spec-workloadnodes) parameters from all NFSStorageClass resources. If the `workloadNodes` parameter is missing in an NFSStorageClass, the workload will be placed on all nodes.

A directory `<directory from share>/<PV name>` will be created for each PV.
