---
title: "Module csi-nfs: examples"
description: Examples of configuring the csi-nfs module.
---

## Configuration of the module with RPC-with-TLS support

Example ModuleConfig with TLS parameters:

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

Example NFSStorageClass with RPC-with-TLS enabled:

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

## A CA of its own for a StorageClass

An NFSStorageClass can carry the CA that verifies its NFS server in `spec.tlsParameters.ca`, instead of `tlsParameters.ca` of the module:

```yaml
apiVersion: storage.deckhouse.io/v1alpha1
kind: NFSStorageClass
metadata:
  name: nfs-storage-class-own-ca
spec:
  connection:
    host: nfs-server-name.io
    share: /
    nfsVersion: "4.1"
    tls: true
  tlsParameters:
    ca: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUZFVENDQXZtZ...
  reclaimPolicy: Delete
  volumeBindingMode: WaitForFirstConsumer
```

- Such a class does not need `tlsParameters.ca` in the ModuleConfig.
- A node has one truststore for all its mounts: it trusts the CA of the module and the CAs of every class together, so a CA of a class adds trust rather than limiting it to that class.
- A changed CA reaches the nodes without restarting the pods of the module, once kubelet refreshes the `tlshd-conf` ConfigMap, usually within a minute or two.

## The client certificate of mTLS

With `mtls: true` the nodes present a client certificate to the NFS server:

- By default it is a certificate the module issues itself: valid for ten years and renewed half a year before it expires, by the same CA, which is valid for twenty years, so the NFS servers keep trusting the nodes through a renewal. Its CA is in `status.clientCA` of every NFSStorageClass with `mtls: true`, Base64-encoded PEM; the NFS server has to trust it.
- `tlsParameters.mtls.clientCert` and `clientKey` of the ModuleConfig replace it, both together. With `tlsParameters.mtls.clientCA`, the CA of that certificate, `status.clientCA` carries that CA instead; without it, `status.clientCA` is empty.
- A node presents the same client certificate on every mount with `mtls`, so every NFS server those classes use has to trust its CA.
- The key is kept in the Secret `d8-csi-nfs/tlshd-client-key`; a renewed certificate reaches the nodes without restarting the pods of the module.

## Hardened mount options

`spec.mountOptions.nosuid` and `spec.mountOptions.nodev` mount the volumes of a class with `nosuid` and `nodev`: on the node, the set-user-ID bits of the files on the share do not take effect, and device files on it cannot be opened.

The flags belong to the mount on the node. The container runtime bind-mounts the volume into a container without them (checked with containerd 1.7), so inside a pod they do not stop a set-user-ID binary; `securityContext.allowPrivilegeEscalation: false` of the container, which the `Restricted` Pod Security Standard requires, does.

```yaml
spec:
  mountOptions:
    nosuid: true
    nodev: true
```
