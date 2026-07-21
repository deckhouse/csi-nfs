---
title: "Module csi-nfs: examples"
description: Examples of configuring the csi-nfs module.
---

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

CSI driver control pods are placed on cluster nodes according to the summarization of the `workloadNodes` parameters from all NFSStorageClass resources. If the `workloadNodes` parameter is missing in an NFSStorageClass, the workload will be placed on all nodes.

A directory `<directory from share>/<PV name>` will be created for each PV.

### Checking module health

You can verify the functionality of the module using the instructions [in FAQ](./faq.html#how-to-check-the-modules-functionality).

## Configuration of the module with RPC-with-TLS support

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
