---
title: "Module csi-nfs: FAQ"
description: FAQ for the csi-nfs module.
---

## How to check the module's functionality?

To do this, you need to check the pod statuses in the `d8-csi-nfs` namespace. All pods should be in the `Running` or `Completed` state and should be running on all nodes. You can check this with the following command:

```shell
d8 k -n d8-csi-nfs get pod -owide -w
```

## Is it possible to change the parameters of an NFS server for already created PVs?

No, the connection data to the NFS server is stored directly in the PV manifest and cannot be changed. Changing the StorageClass also does not affect the connection settings in already existing PVs.

## How to create volume snapshots?

Before creating snapshots, review the limitations in ["Creating volume snapshots"](./#creating-volume-snapshots).

In `csi-nfs`, snapshots are created by archiving the volume directory. The archive is stored in the root of the NFS server directory specified in the `spec.connection.share` parameter.

1. [Enable the `snapshot-controller` module](/modules/snapshot-controller/configuration.html#enable).

1. Create volume snapshots. To do this, run the following command, specifying the required parameters:

   ```shell
   d8 k apply -f - <<EOF
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
   d8 k get volumesnapshot
   ```

This command will display a list of all snapshots and their current status.

## How to select the method to clean the volume before deleting the PV?

{{< alert level="warning" >}}
Volume cleanup is only available in commercial editions of Deckhouse Kubernetes Platform.
{{< /alert >}}

Files with user data may remain on the volume to be deleted. These files will be deleted and will not be accessible to other users via NFS.

However, the deleted files' data may be available to other clients if the server grants block-level access to its storage.

The [`volumeCleanup`](./cr.html#nfsstorageclass-v1alpha1-spec-volumecleanup) parameter will help you choose how to clean the volume before deleting it.

{{< alert level="warning" >}}
This option does not affect files already deleted by the client application.

This option affects only commands sent via the NFS protocol. The server-side execution of these commands is defined by:

- NFS server service;
- the file system;
- the level of block devices and their virtualization (e.g. LVM);
- the physical devices themselves.

Make sure the server is trusted. Do not send sensitive data to servers that you are not sure of.
{{< /alert >}}

### SinglePass method

Used if `volumeCleanup` is set to `RandomFillSinglePass`.

The contents of the files are overwritten with a random sequence before deletion. The random sequence is transmitted over the network.

### ThreePass method

Used if `volumeCleanup` is set to `RandomFillThreePass`.

The contents of the files are overwritten three times with a random sequence before deletion. The three random sequences are transmitted over the network.

### Discard method

Used if `volumeCleanup` is set to `Discard`.

Many file systems implement support for solid-state drives, allowing the space occupied by a file to be freed at the block level without writing new data to extend the life of the solid-state drive. However, not all solid-state drives guarantee that the freed block data is inaccessible.

If `volumeCleanup` is set to `Discard`, file contents are marked as free via the `falloc` system call with the `FALLOC_FL_PUNCH_HOLE` flag. The file system will free the blocks fully used by the file, via the `blkdiscard` call, and the remaining space will be overwritten with zeros.

Advantages of this method:

- the amount of traffic does not depend on the size of the files, only on the number of files;
- the method can make old data unavailable in some server configurations;
- works for both hard disks and SSDs;
- can maximize SSD lifetime.

## Why are PVs created in a StorageClass with RPC-with-TLS support not being deleted, along with their <PV name> directories on the NFS server?

If the [NFSStorageClass](./cr.html#nfsstorageclass) resource was configured with RPC-with-TLS support, there might be a situation where the PV fails to be deleted.
This happens due to the removal of the secret (for example, after deleting NFSStorageClass), which holds the mount options. As a result, the controller is unable to mount the NFS directory to delete the `<PV name>` directory.

## How to place multiple CAs in the tlsParameters.ca setting in ModuleConfig?

Concatenate the certificates into a single file and encode the result in Base64. Examples:

{{< tabs name="csi-nfs_faq_multiple_ca" >}}
{{% tab name="Two CAs" %}}

```shell
cat CA1.crt CA2.crt | base64 -w0
```

{{% /tab %}}
{{% tab name="Three CAs" %}}

```shell
cat CA1.crt CA2.crt CA3.crt | base64 -w0
```

{{% /tab %}}
{{< /tabs >}}

## What are the requirements for a Linux distribution to deploy an NFS server with RPC-with-TLS support?

To deploy an NFS server with RPC-with-TLS support, the distribution must meet the following requirements:

- The kernel must be built with the `CONFIG_TLS` and `CONFIG_NET_HANDSHAKE` options enabled;
- The nfs-utils package (or nfs-common in Debian-based distributions) must be version >= 2.6.3.

## What happens if a node's repositories cannot provide rpcbind and nfs-utils?

The module installs the two daemons it needs itself, from an image of its own.

What the module wants from a node is narrower than it looks. The `csi-nfs` container mounts the export with its own `nfs-utils`, so no client tools are needed on the host. But an NFSv3 mount registers with the host's portmapper, and locking goes through the host's `rpc.statd`. Those two daemons the host owes, and until they answer the CSI node Pod on that node does not start at all: its init container blocks on `/run/rpcbind.sock`.

On every node it serves — those carrying `storage.deckhouse.io/csi-nfs-node` — the `NodeGroupConfiguration` first asks the node's package manager for `rpcbind` and `nfs-common`/`nfs-utils`. That stays the preferred path: the portmapper is a host-wide service, everything else on the node that speaks RPC expects the distribution's one, and while it is there the module does not interfere.

When the install fails — a closed environment with no route to the repositories, or a distribution that does not ship the packages — the module falls back to its own package image, `nfs-tools`. bashible pulls it from the module's registry, unpacks it on the node and runs its install script. The payload lands under `/var/lib/deckhouse/sds/csi-nfs`, every binary is started through its own dynamic loader with its own library path, and two units come up:

```shell
systemctl is-active d8-csi-nfs-rpcbind.service d8-csi-nfs-rpc-statd.service
```

The install script verifies its own outcome rather than reporting success for having written unit files. The whole contract is a socket that answers, so it waits for that socket and, if it never appears, prints what the daemons said and fails the bashible step — which is the difference between a node that reports an error and a node on which every NFSv3 volume simply waits forever.

Three things worth knowing about, because none of them is visible until a mount hangs.

The socket path is not assumed. Where `rpcbind` opens its local socket is compiled into the binary, and the image reads that path out at build time and tells the install script what to expect; a build that used `/var/run/rpcbind.sock` instead would start cleanly and leave the CSI node Pod waiting on the path it watches.

`rpcbind` and `rpc.statd` are nothing but userspace SUN RPC, and libtirpc needs a transport table to make a single call. The package carries its own `netconfig` and the wrappers point the daemons at it; without it every call fails with `RPC_UNKNOWNPROTO`.

And `rpc.statd` execs `sm-notify` by the absolute path compiled into it, which is not the copy under our prefix. The install script links that path onto the payload's copy, and records the link so that the uninstall takes back exactly it. Without the link a node that has just rebooted cannot announce the reboot to the peers holding locks on it.

The two paths never mix. A host that already has `rpcbind` of its own keeps everything of its own, and the install script does nothing there.

To tell which path a node took, look at what is running on it:

```shell
# the distribution's daemons
systemctl is-active rpcbind rpc-statd
# the module's own
ls /var/lib/deckhouse/sds/csi-nfs/bin
rpcinfo -p localhost
```

Two limits are worth stating plainly.

The fallback delivers the two daemons the module needs, not a general-purpose NFS client. Anything else on the node that mounts NFS by itself — kubelet's in-tree `nfs` volume plugin, or a `mount -t nfs` by hand — still needs the distribution's `mount.nfs` and does not get one from here. The module's own volumes are unaffected: those are mounted from inside the container.

And none of this concerns NFSv4. Version 4 carries locking in the protocol itself and needs no portmapper, so a node serving only NFSv4 shares needs neither the distribution's packages nor the fallback.

Uninstalling stops the units and takes back the files the install placed on the host, and deliberately leaves `/var/lib/nfs` in place: its `sm`/`sm.bak` lists are the record of which peers hold locks on this node, and a distribution package installed later expects to find them where they are.
