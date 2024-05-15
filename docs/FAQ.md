---
title: "The csi-nfs module: FAQ"
description: CSI NFS module FAQ
---

## How to check module functionality?

To do this, you need to check the status of the pods in the `d8-csi-nfs` namespace. All pods should be in the `Running` or `Completed` state and should be running on all nodes.

```shell
kubectl -n d8-csi-nfs get pod -owide -w
```

## Is it possible to change the parameters of an NFS server for already created PVs?

No, the connection data to the NFS server is stored directly in the PV manifest and cannot be changed. Changing the Storage Class also does not affect the connection settings in already existing PVs.
