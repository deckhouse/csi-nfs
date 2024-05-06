---
title: "The csi-nfs module"
description: "The csi-nfs module: General Concepts and Principles."
moduleStatus: experimental
---

This module provides CSI that manages volumes based on `NFS`. 

> **Caution!** The user is not allowed to create a `StorageClass` for the nfs.csi.storage.deckhouse.io CSI driver.

## Quickstart guide

Note that all commands must be run on a machine that has administrator access to the Kubernetes API.

### Enabling modules

- Enable the `csi-nfs` module. This will result in the following actions across all cluster nodes:
    - registration of the CSI driver;
    - launch of service pods for the `csi-nfs` components.

```shell
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

- Wait for the module to become `Ready`.

```shell
kubectl get mc csi-nfs -w
```

- Make sure that all pods in `d8-csi-nfs` namespaces are `Running` or `Completed` and are running on all nodes.

```shell
kubectl -n d8-csi-nfs get pod -owide -w
```