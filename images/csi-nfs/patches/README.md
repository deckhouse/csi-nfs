# Patches

## 001-add-volume-cleanup-and-feature-pkg.patch

Add volume cleanup feature

## 002-add-mountPermissions-to-snapshot.patch

Add mountPermissions to snapshot. Fix mountPermissions.

## 003-updated-golang-version.patch

Update golang version

## 004-add-socket-permissions.patch

Add sockerPermissions to csi-controller

## 005-fix-cve.patch

Fix CVE (combined). Cumulative dependency bumps and re-vendoring:

- go.opentelemetry.io/otel and otel/metric,sdk,trace v1.41.0 (CVE-2026-29181)
- golang.org/x/net v0.55.0 (CVE-2026-25681, CVE-2026-27136, CVE-2026-39821,
  CVE-2026-25680, CVE-2026-42502, CVE-2026-42506, CVE-2026-33814)
- golang.org/x/sys v0.45.0 (CVE-2026-39824)

## 006-idempotent-createvolume-createsnapshot.patch

Make the data-copy step of CreateVolume/CreateSnapshot idempotent. A retried
CreateVolume from a snapshot used to truncate and rewrite disk.img of an
already provisioned (and possibly attached) volume, corrupting reads of a
booting VM. Content is now populated into a `.populating` staging path and
atomically renamed into place; retries skip the copy when the final path
exists. Same for the snapshot archive in CreateSnapshot. Also fix TarPack
dropping Close() errors (named return), which could hide NFS write-back
failures (e.g. ENOSPC) behind a truncated-but-ready archive.
