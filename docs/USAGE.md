---
title: "The csi-nfs-volume module: configuration examples"
description: The csi-nfs usage and configuration examples.
---

## Quickstart guide

Note that all commands must be run on a machine that has administrator access to the Kubernetes API.

### Enabling module

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
