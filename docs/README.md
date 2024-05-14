---
title: "The csi-nfs module"
description: "The csi-nfs module: General Concepts and Principles."
moduleStatus: experimental
---

This module provides CSI that manages volumes based on `NFS`. The module allows you to create a `StorageClass` in `Kubernetes` by creating [Kubernetes custom resources](./cr.html) `NFSStorageClass`.

> **Caution!** The user is not allowed to create a `StorageClass` for the `nfs.csi.k8s.io` CSI driver.

Usage instructions can be found [here]((./usage.html))

## System requirements and recommendations

### Requirements
- Stock kernels shipped with the [supported distributions](https://deckhouse.io/documentation/v1/supported_versions.html#linux).
- Presence of a deployed and configured NFS server.
