---
title: "Module csi-nfs"
description: "The csi-nfs module: General Concepts and Principles."
---

The `csi-nfs` module provides a CSI driver for managing NFS volumes in Kubernetes.
Use it to provision PersistentVolumes on an NFS server through [Custom Resources](./cr.html#nfsstorageclass) `NFSStorageClass`.

{{< alert level="warning" >}}
**Warning about using snapshots (Volume Snapshots)**

When creating snapshots of NFS volumes, it is important to understand their creation scheme and associated limitations. Avoid using snapshots in `csi-nfs` when possible:

1. The CSI driver creates a snapshot at the NFS server level.
1. For this, tar is used, which packages the volume contents, with all the limitations that may arise from this.
1. **Before creating a snapshot, be sure to stop the workload** (pods) using the NFS volume.
1. NFS does not ensure atomicity of operations at the file system level when creating a snapshot.
{{< /alert >}}

{{< alert level="info" >}}

- The [snapshot-controller](/modules/snapshot-controller/) module must be connected for this module to operate.
- Creating a StorageClass for the CSI driver `nfs.csi.k8s.io` by the user is prohibited.
- Supported access modes for the module: RWO, RWX in DVP, RWX.

{{< /alert >}}

## Main Features

- Provision NFS-backed PersistentVolumes through the `NFSStorageClass` custom resource.
- Support RWO and RWX access modes, including RWX in Deckhouse Virtualization Platform.
- Restrict volume mounting to selected cluster nodes with `workloadNodes`.
- Configure RPC-with-TLS (`tls` / `mtls`) for NFS connections in commercial editions.
- Clean volume data before PV deletion with `volumeCleanup` in commercial editions.

## System requirements and recommendations

{{< alert level="warning" >}}
To use NFS as virtual disk storage in Deckhouse Virtualization Platform, configure the NFS server with the `no_root_squash` option (see below).
{{< /alert >}}

### Requirements

- Use stock kernels provided with [supported distributions](/products/kubernetes-platform/documentation/v1/supported_versions.html#linux);
- Ensure that the NFS server is correctly configured and running:
  - For DKP modules where StorageClass is used, it may be necessary to allow access to clients with root privileges. In Linux, this is implemented via the `no_root_squash` option, while on other systems (e.g., BSD or storage systems) a similar setting may have a different name;
  - For virtual disk storage in the [Deckhouse Virtualization Platform](/products/virtualization-platform/documentation/), the `no_root_squash` option is mandatory!
- To support RPC-with-TLS, enable `CONFIG_TLS` and `CONFIG_NET_HANDSHAKE` options in the Linux kernel.

### Recommendations

For module pods to restart when the `tlsParameters` parameter is changed in the module settings, the [pod-reloader](/modules/pod-reloader/) module must be enabled (enabled by default).

## Limitations

### RPC-with-TLS mode limitations

- For the `mtls` security policy, only one client certificate is supported.
- A single NFS server cannot simultaneously operate in different security modes: `tls`, `mtls`, and standard (non-TLS) mode.
- The `tlshd` daemon must not be running on the cluster nodes, otherwise it will conflict with the module daemon. To prevent conflicts when enabling TLS, the third-party `tlshd` is automatically stopped on the nodes and its autostart is disabled.

For a quickstart (enabling the module, creating a StorageClass), see [Quickstart](./examples.html#quickstart).
For other configuration examples, see [Examples](./examples.html).
For volume cleanup methods and operational questions, see [FAQ](./faq.html).
