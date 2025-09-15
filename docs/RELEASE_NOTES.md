---
title: "Release Notes"
---

## v0.3.5

* Added release notes
* Hooks switched from python to golang

## v0.3.4

* Added additional mountings for containerd v2 support

## v0.3.3

* Added information about the need for snapshot-controller for module operation
* Added readonlyRootFilesystem for enhanced module security

## v0.3.2

* CVE fixes

## v0.3.1

* Service account changed to "csi"
* Added dependency on snapshot-controller
* Internal changes for containerd v2 support
* CVE fixes
* Documentation fixes

## v0.3.0

* Added HA mode support in controller
* Fixes for proper volume snapshots operation
* Updated CSI version to current v4.11.0

## v0.2.5

* CSI bugfix (missing /tmp mount point was fixed)

## v0.2.4

* Technical release, module refactoring

## v0.2.3

* Added base64 certificate validation in MC
* Fixes in RPC-with-TLS mechanism
* Module refactoring

## v0.1.9

* Fixed healthcheck ports for csi
* Fixed installation and startup of rpcbind

## v0.1.10

* Multiple fixes and improvements in RBAC, MC work, controllers
* Added ability to specify workload node selector, considering which csi-nfs components and useful user workload will be deployed
