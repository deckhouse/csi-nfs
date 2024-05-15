---
title: "The csi-nfs module: FAQ"
description: CSI NFS module FAQ
---

## How to check module health?

To do this, you need to check the status of the pods in the `d8-csi-nfs` namespace. All pods should be in the `Running` or `Completed` state and should be running on all nodes.

```shell
kubectl -n d8-csi-nfs get pod -owide -w
```

## Is it possible to change the parameters of an NFS server for already created PVs?

No, the connection data to the NFS server is stored directly in the PV manifest and cannot be changed. Changing the Storage Class also does not affect the connection settings in already existing PVs.

## How to use the `subDir` parameter?

The `subDir` parameter allows you to specify a subdirectory for each PV.

### Example with templates

You can use 3 templates:

- `${pvc.metadata.name}`
- `${pvc.metadata.namespace}`
- `${pv.metadata.name}`

```yaml
kubectl apply -f - <<'EOF'
apiVersion: storage.deckhouse.io/v1alpha1
kind: NFSStorageClass
metadata:
  name: nfs-storage-class
spec:
  connection:
    host: 10.223.187.3
    share: /
    subDir: "${pvc.metadata.namespace}/${pvc.metadata.name}"
    nfsVersion: "4.1"
  reclaimPolicy: Delete
  volumeBindingMode: WaitForFirstConsumer
EOF
```

In this example, a directory `/<namespace>/<PVC name>` will be created on the NFS server for each volume.

> **Caution!** The PVC name is set by the user. Such `subDir` settings may lead to a situation where the directory name for a newly created volume matches the directory name of a previously deleted volume. If `reclaimPolicy` is set to `Retain`, the data from the previously allocated volumes with the same PVC name will be available in the new volume.

### Example without templates

In addition to templates, you can specify a regular string as the subdirectory name.

```yaml
kubectl apply -f - <<'EOF'
apiVersion: storage.deckhouse.io/v1alpha1
kind: NFSStorageClass
metadata:
  name: nfs-storage-class
spec:
  connection:
    host: 10.223.187.3
    share: /
    subDir: "shared-folder"
    nfsVersion: "4.1"
  reclaimPolicy: Retain
  volumeBindingMode: WaitForFirstConsumer
```

In this example, all PVs of this StorageClass will use the same directory on the server: `/shared-folder`.

> **Caution!** If `reclaimPolicy` is set to `Delete`, deleting any PVC of this StorageClass will result in the deletion of the entire `/shared-folder` directory.
