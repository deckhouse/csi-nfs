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
