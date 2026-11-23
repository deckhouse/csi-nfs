# Patches

Patches are applied in order to csi-driver-nfs v4.13.4. The files under
`csi-driver-nfs/` are copied over the patched tree afterwards.

## 001-add-volume-cleanup-and-feature-pkg.patch

Add volume cleanup feature. DeleteVolume and DeleteSnapshot wipe the data with
the method from the `volumeCleanup` secret key (`Discard`,
`RandomFillSinglePass`, `RandomFillThreePass`) before the subdirectory is
removed; the cleanup itself is EE-only and gated by
`github.com/deckhouse/csi-nfs/lib/go/common/pkg/feature`, which is
required through `replace => ../csi-nfs/lib/go/common` (werf places `lib/go`
at `/src/csi-nfs`). go.mod/go.sum are `go mod tidy` output: the `go`
directive moves to 1.27.1, the one `lib/go/common` declares, and the
requirements `lib/go/common` shares with the driver are raised to its versions.

## 002-add-mountPermissions-to-snapshot.patch

Add mountPermissions to snapshot. Fix mountPermissions.

- `mountPermissions` is accepted in the VolumeSnapshotClass parameters and
  applied to the snapshot directory in CreateSnapshot.
- CreateVolume fails when chmod of the volume directory fails (upstream only
  logs a warning).
- NodePublishVolume creates the target with 0777 and does not chmod it: the
  target is the root of the NFS export.

The `syscall.Chmod` part of the former patch is dropped: upstream switched
`chmodIfPermissionMismatch` to raw Unix modes and `syscall.Chmod` in v4.13.3
(kubernetes-csi/csi-driver-nfs fb08e26f, 4a5ec144).

## 003-add-socket-permissions.patch

Add the `--socket-permissions` flag to set the mode of the CSI Unix socket
(used by csi-controller).

## 004-idempotent-createvolume-createsnapshot.patch

Make the data-copy step of CreateVolume/CreateSnapshot idempotent. A retried
CreateVolume from a snapshot used to truncate and rewrite disk.img of an
already provisioned (and possibly attached) volume, corrupting reads of a
booting VM. Content is now populated into a `.populating` staging path and
atomically renamed into place; retries skip the copy when the final path
exists. Same for the snapshot archive in CreateSnapshot, for both the
compressed (`.tar.gz`) and the uncompressed (`.tar`) archive upstream added
with `--enable-snapshot-compression` in v4.13.0. Also fix TarPack dropping
Close() errors (named return), which could hide NFS write-back failures
(e.g. ENOSPC) behind a truncated-but-ready archive. The upstream test
`TestCopyVolumeFromUncompressedSnapshot` no longer pre-creates the destination
directory: the staging rename requires it to be absent, as it is when
CreateVolume calls the copy.

## 005-fix-cve.patch

Fix CVE. go.mod/go.sum only (the image builds non-vendored via
`go mod download`), incremental on top of 001:

- google.golang.org/grpc v1.83.2 (GHSA-hrxh-6v49-42gf, CVE-2026-84303,
  CVE-2026-84304, CVE-2026-84445)
- go.opentelemetry.io/otel/sdk v1.45.0 (CVE-2026-24051, CVE-2026-39883,
  CVE-2026-81870)
- go.opentelemetry.io/otel/exporters/otlp/otlptrace and otlptracegrpc v1.45.0
  (CVE-2026-81870)
- golang.org/x/crypto v0.56.0 (CVE-2026-56854, CVE-2026-56855, CVE-2026-78662)
- golang.org/x/mod v0.40.0 (CVE-2026-56864, CVE-2026-56865)
- github.com/gorilla/websocket v1.5.3 (GHSA-w67g-5rqw-f597)

The remaining findings are covered by `known_vulnerabilities.vex`: the fixed
releases of github.com/opencontainers/runc (v1.3.6+) and
github.com/google/cel-go (v0.29.0+) do not build with the k8s.io v0.32
libraries, and golang.org/x/crypto has no release without openpgp. None of the
three is compiled into the csi-nfs binary.

## 006-deny-inline-volumes-of-in-cluster-servers.patch

Refuse inline volumes of NFS servers inside the cluster. The node plugin
mounts from the node, and a server in the cluster that admits the nodes (the
servers of sds-nfs do) would give any pod that knows its address everything
it exports, the volumes of other namespaces included.

- The `--deny-inline-cidrs` flag takes the networks to refuse; the chart
  passes the pod and service networks of the cluster.
- NodePublishVolume of an inline volume (`csi.storage.k8s.io/ephemeral` in the
  volume context, sent because the CSIDriver sets `podInfoOnMount`) resolves
  the server and fails with `PermissionDenied` when an address is in one of
  them, or when the name does not resolve. Volumes of PersistentVolumes are
  not affected.

The check and its test are new files of the patch (`pkg/nfs/inline_server.go`,
`pkg/nfs/inline_server_test.go`), not files under `csi-driver-nfs/`: the CI of
the module runs `go test` in every directory with a test, and that directory is
not a module of its own.

## 007-do-not-stack-hung-mounts.patch

Do not start a mount while an earlier mount of the same volume on the same
target is still running. NodePublishVolume gives up waiting for `mount` after
110 s, but the process keeps running: with a `hard` NFS mount it can stay in
uninterruptible sleep for good, for example when the NFS client of the kernel
is stuck initializing its client ID. kubelet retries every two minutes, and
each retry started another `mount`; on a test node 1660 of them piled up in two
days. A mount that outlives its timeout is now recorded until its process
exits, and a retry for the same volume and target returns `Aborted` with the
age of the running mount and what a mount that never finishes means. The
volume lock is still released, so NodeUnpublishVolume tears the pod down while
the mount hangs. The record lives in the memory of the node plugin: after the
csi-node container restarts, a retry starts one new mount even if an earlier one
still hangs.

## 900-fix-cve-go-deps.patch

Generated by `fix_go_vulns.py` (deckhouse/storage/common) against `4.13.4` on top of the other patches in this directory; regenerated on every run, do not edit by hand. When the upstream version changes, delete this patch or rerun the script (`fix_go_vulns.py --only <module>`): the old one will not apply to the new tag. Raises Go dependencies with known vulnerabilities to the lowest fixed versions:

- `go.mod`: `github.com/antlr4-go/antlr/v4` v4.13.0 → v4.13.1
- `go.mod`: `github.com/cilium/ebpf` v0.17.3 → v0.22.0 (CVE-2026-10722)
- `go.mod`: `github.com/cpuguy83/go-md2man/v2` v2.0.5 → v2.0.6
- `go.mod`: `github.com/google/cel-go` v0.22.0 → v0.29.0 (GHSA-gcjh-h69q-9w9g)
- `go.mod`: `github.com/moby/sys/user` v0.3.0 → v0.4.1 (CVE-2026-61801)
- `go.mod`: `github.com/opencontainers/runc` v1.2.9 → v1.3.6 (CVE-2026-41579)
- `go.mod`: `github.com/opencontainers/runtime-spec` v1.2.0 → v1.2.1
- `go.mod`: `github.com/sirupsen/logrus` v1.9.3 → v1.9.4
- `go.mod`: `github.com/spf13/cobra` v1.8.1 → v1.10.2
- `go.mod`: `github.com/spf13/pflag` v1.0.5 → v1.0.10
- `go.mod`: `github.com/stretchr/testify` v1.11.1 → v1.12.1
- `go.mod`: `go.opentelemetry.io/otel` v1.45.0 → v1.47.0
- `go.mod`: `go.opentelemetry.io/otel/metric` v1.45.0 → v1.47.0
- `go.mod`: `go.opentelemetry.io/otel/trace` v1.45.0 → v1.47.0 (CVE-2026-128085)
- `go.mod`: `go.yaml.in/yaml/v3` v3.0.4 → v3.0.5
- `go.mod`: `golang.org/x/crypto` v0.58.0 → v0.58.1-0.20261009181718-2f1f834330b2 (GO-2026-5932)
- `go.mod`: `golang.org/x/exp` v0.0.0-20240719175910-8a7402abbf56 → v0.0.0-20240823005443-9b4947da3948
