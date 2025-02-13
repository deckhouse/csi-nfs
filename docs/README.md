---
title: "The csi-nfs module"
description: "The csi-nfs module: General Concepts and Principles."
---

The module provides CSI for managing NFS volumes and allows creating StorageClass in Kubernetes through [Custom Resources](./cr.html#nfsstorageclass) `NFSStorageClass`.

{{< alert level="info" >}}
Creating a StorageClass for the CSI driver `nfs.csi.k8s.io` by the user is prohibited.
{{< /alert >}}

## System requirements and recommendations

### Requirements

- Use stock kernels provided with [supported distributions](https://deckhouse.io/documentation/v1/supported_versions.html#linux);
- Ensure the presence of a deployed and configured NFS server;
- To support RPC-with-TLS, enable `CONFIG_TLS` and `CONFIG_NET_HANDSHAKE` options in the Linux kernel.

### Recommendations

For module pods to restart when the `tlsParameters` parameter is changed in the module settings, the [pod-reloader](https://deckhouse.io/products/kubernetes-platform/documentation/v1/modules/pod-reloader) module must be enabled (enabled by default).

## RPC-with-TLS mode limitations

- Only one certificate authority (CA) is supported.
- For the `mtls` security policy, only one client certificate is supported.
- A single NFS server cannot simultaneously operate in different security modes: `tls`, `mtls`, and standard (non-TLS) mode.
- The `tlshd` daemon must not be running on the cluster nodes, otherwise it will conflict with the daemon of our module. To prevent conflicts when enabling TLS, the third-party `tlshd` is automatically stopped on the nodes and its autostart is disabled.

## Quickstart guide

Note that all commands must be run on a machine that has administrator access to the Kubernetes API.

### Enabling module

1. Enable the `csi-nfs` module. This will result in the following actions across all cluster nodes:
   - registration of the CSI driver;
   - launch of service pods for the `csi-nfs` components.

   ```yaml
   kubectl apply -f - <<EOF
   apiVersion: deckhouse.io/v1alpha1
   kind: ModuleConfig
   metadata:
     name: csi-nfs
   spec:
     enabled: true
     version: 1
   EOF
   ```

2. Wait for the module to become `Ready`:

   ```shell
   kubectl get module csi-nfs -w
   ```

### Creating a StorageClass

To create a StorageClass, you need to use the [NFSStorageClass](./cr.html#nfsstorageclass) resource. Here is an example command to create such a resource:

```yaml
kubectl apply -f - <<EOF
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
EOF
```

A directory `<directory from share>/<PV name>` will be created for each PV.

### Checking module health

You can verify the functionality of the module using the instructions [in FAQ](./faq.html#how-to-check-module-health).
