---
title: "The csi-nfs module: FAQ"
description: CSI NFS module FAQ
---

## Is it possible to change the parameters of an NFS server for already created PVs?

No, the connection data to the NFS server is stored directly in the PV manifest and cannot be changed. Changing the Storage Class also does not affect the connection settings in already existing PVs.
