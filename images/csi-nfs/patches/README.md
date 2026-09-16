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

## 007-fix-cve-golang-x.patch

Fix fixable golang.org/x CVEs, incremental on top of 005. go.mod/go.sum only
(the image builds non-vendored via `go mod download`):

- golang.org/x/net v0.56.0 (CVE-2026-46600)
- golang.org/x/text v0.39.0 (CVE-2026-56852)

Pulls the transitive golang.org/x/crypto v0.53.0, x/sys v0.46.0,
x/sync v0.21.0, x/term v0.44.0 bumps.

## 008-fix-cve-grpc-hrxh.patch

Raise `google.golang.org/grpc` v1.79.3 -> **v1.82.1** for
**GHSA-hrxh-6v49-42gf** (HIGH, gRPC-Go: xDS RBAC and HTTP/2
vulnerabilities), the version patch 005 had pinned. go.mod/go.sum only,
incremental on top of 007.

Transitive bumps from `go mod tidy`: `google.golang.org/protobuf` v1.36.11,
`go.opentelemetry.io/otel` (+ metric, sdk, trace) v1.43.0,
`golang.org/x/oauth2` v0.36.0 and `google.golang.org/genproto/googleapis/{api,rpc}`.

## 009-fix-cve-grpc-x-crypto.patch

Raise `google.golang.org/grpc` v1.82.1 -> **v1.83.2** for **CVE-2026-84304**
and **CVE-2026-84303** (fixed in 1.83.1) and **CVE-2026-84445** (fixed in
1.83.2), the three findings Trivy reported against the `csi-nfs` binary, and
`golang.org/x/crypto` v0.53.0 -> **v0.57.0** for CVE-2026-56854 (the
x/crypto/ssh source-address authentication bypass), CVE-2026-56855 and
CVE-2026-78662, so the transitive pin does not age into the same advisories
here as it did in the go-hooks binary. go.mod/go.sum only, incremental on top
of 008.

Transitive bumps from `go mod tidy`: `go.opentelemetry.io/otel` (+ metric,
sdk, trace) v1.44.0, `otelhttp` v0.61.0, `golang.org/x/{net,sys,text,sync,
term,mod,tools}` and `google.golang.org/genproto/googleapis/{api,rpc}`.

The `go` directive moves 1.25.7 -> 1.27.1: `go get` raises it to the
toolchain in use when a requirement needs a newer language version (x/crypto
v0.57.0 needs 1.26), and 1.27.1 is the Go the builder/golang image ships
after the base-images v3.0.2 sync.
