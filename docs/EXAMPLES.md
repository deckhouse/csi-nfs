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
- `mtls: true` still takes the client certificate from `tlsParameters.mtls` of the ModuleConfig.
