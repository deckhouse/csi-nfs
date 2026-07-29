# E2E tests for csi-nfs

End-to-end coverage for the csi-nfs data path: given real NFS servers, csi-nfs
must turn an `NFSStorageClass` into a core Kubernetes `StorageClass`, and let
workloads bind PVCs and round-trip data through the `nfs.csi.k8s.io` driver over
NFSv3, NFSv4.1/4.2 and RPC-with-TLS.

## Why the NFS servers live outside the cluster

csi-ceph can stand its backend up from inside the cluster (an sds-elastic
`ElasticCluster`). csi-nfs cannot: it only *connects* to an NFS server. The
servers are therefore external hosts, passed in through `E2E_NFS_*` env vars, and
**the test cluster must be created somewhere with direct network reachability to
them** — mounts are issued from the worker nodes' host network namespace (the CSI
node DaemonSet runs with `hostNetwork: true`), not from the pod network.

They must also stay *off* the cluster, for two independent reasons:

- **RPC-with-TLS.** The module runs its own `tlshd` as a sidecar in the CSI node
  DaemonSet, in the host network namespace, and the kernel handshake service
  (`CONFIG_NET_HANDSHAKE`) has exactly one consumer per network namespace. The
  module's `tlshd` is configured `[authenticate.client]`-only, and a
  `NodeGroupConfiguration` actively `systemctl mask`s any host `tlshd` on nodes
  labelled `storage.deckhouse.io/csi-nfs-node`. An NFS server needing a
  *server-side* handshake on such a node has nobody to service it. The module
  docs state this directly: "The `tlshd` daemon must not be running on the
  cluster nodes, otherwise it will conflict with the daemon of our module."
- **NFSv3.** With `v3support: true` the module installs `rpcbind` + `nfs-common`
  on every csi-nfs node. A kernel NFS server on the same node registers its own
  `mountd`/`statd`/`nlockmgr` with that same `rpcbind` (program 100024 can only
  be registered once), so NLM lock recovery breaks; and a local NFS client
  mounting a local `knfsd` export can deadlock under memory pressure.

## The stand: four servers

| Role | Versions | Export(s) | Security |
| ---- | -------- | --------- | -------- |
| `E2E_NFS_V3_HOST` | v3 only (`vers4 = n`) | `/srv/nfs/e2e`, `/srv/nfs/e2e-alt` | plain |
| `E2E_NFS_V4_HOST` | v4.1 + v4.2 (`vers3 = n`) | `/srv/nfs/e2e`, `/srv/nfs/e2e-alt` | plain |
| `E2E_NFS_TLS_HOST` | v4.1 + v4.2 | `/srv/nfs/tls` | `xprtsec=tls` |
| `E2E_NFS_MTLS_HOST` | v4.1 + v4.2 | `/srv/nfs/mtls` | `xprtsec=mtls` |

`stand/` provisions them on clean Ubuntu 24.04 hosts. One security mode per
host — see below for why that is not optional:

```bash
ssh <v3-host> 'sudo bash -s v3' < stand/setup-nfs-plain.sh
ssh <v4-host> 'sudo bash -s v4' < stand/setup-nfs-plain.sh

# One server certificate carrying an IP SAN for both RPC-with-TLS hosts.
./stand/gen-certs.sh ./pki <tls-host-ip> <mtls-host-ip>
for h in <tls-host> <mtls-host>; do
  tar cf - -C pki ca.crt server.crt server.key | ssh "$h" 'mkdir -p /tmp/pki && tar xf - -C /tmp/pki'
done
ssh <tls-host>  'sudo bash -s tls'  < stand/setup-nfs-tls.sh
ssh <mtls-host> 'sudo bash -s mtls' < stand/setup-nfs-tls.sh
```

Exports use `no_root_squash` (mandatory: the controller `mkdir`s and `chmod`s
`<share>/<PV name>` as root) and are limited to `10.0.0.0/8` — override with
`NFS_CLIENTS=` when the cluster sits elsewhere.

### One security mode per host

Verified on the stand, not just inferred from the docs. Each mode works on its
own, but with an `mtls` mount already established, mounting the `tls` export of
the *same* address fails with `mount.nfs: Operation not permitted`. The kernel
NFS client keys its transport by server address, so the second mount reuses a
connection whose security mode the target export rejects. This is the mechanism
behind the "a single NFS server cannot simultaneously operate in different
security modes" note in the module docs.

Give each mode its own host and both suites run in one pass — also verified:
with `tls` mounted from one address and `mtls` from another, both stay mounted
and round-trip data simultaneously. `E2E_NFS_MTLS_HOST` therefore has **no**
fallback to `E2E_NFS_TLS_HOST`: pointing both at one address would fail in a way
that looks like a driver bug, so the mtls suite skips itself instead.

### Kernel and tlshd requirements

- Kernel **>= 6.5** with `CONFIG_TLS` and `CONFIG_NET_HANDSHAKE`, on the servers
  *and* on the cluster nodes — NFS `xprtsec` and the handshake upcall both landed
  in 6.5. This is why `cluster_config.yml` pins Ubuntu 24.04 / 6.8 instead of the
  22.04 / 6.2 image the other suites use; on 6.2 the TLS specs cannot work at all.
- `nfs-utils >= 2.6.3` (Ubuntu 24.04 ships 2.6.4).
- `ktls-utils`: Ubuntu 24.04 packages **0.9**, which ignores `x509.truststore`
  (falling back to the system trust store) and resolves the peer name by reverse
  DNS, so it rejects a certificate carrying only an IP SAN. `setup-nfs-tls.sh`
  builds 1.3.1 when it detects that. With 1.x the peer name comes from the kernel
  with the handshake request — for NFS that is the mount's server string, i.e.
  `NFSStorageClass spec.connection.host` — and matches the IP SAN with no DNS
  involved. The server certificate must therefore carry an IP SAN for the exact
  address the `NFSStorageClass` uses.

## Layout

```
e2e/
  cfg/          configuration, parsed from the environment with env tags
  framework/    the building blocks: everything that is not itself a scenario.
                Functions return errors instead of asserting, so they stay
                usable outside Ginkgo and can move upstream into storage-e2e.
  tests/        one scenario per file, plus helpers_*_test.go for the glue
```

Each scenario is a single top-level `Describe` carrying `Label("csi-nfs", "<scenario>")`,
and connects to the cluster itself through `e2e.Connect` in its `BeforeAll`. That
makes every scenario independently runnable:

```bash
make test-scenario SCENARIO=lifecycle-v3
```

| Scenario label | File | What it covers |
| -------------- | ---- | -------------- |
| `admission` | `admission_test.go` | CRD CEL rules (`mtls` without `tls`, `volumeCleanup` + `Retain`, `Discard` below 4.2), `spec.connection` immutability, and the webhook reserving the `nfs.csi.k8s.io` provisioner |
| `lifecycle-v3` | `lifecycle_v3_test.go` | the full volume lifecycle over NFSv3 |
| `lifecycle-v42` | `lifecycle_v42_test.go` | the same over NFSv4.2, with the server-side inspector attached |
| `lifecycle-tls` | `lifecycle_tls_test.go` | the same over `xprtsec=tls` |
| `lifecycle-mtls` | `lifecycle_mtls_test.go` | the same over `xprtsec=mtls`, on its own server |
| `version-v41` | `version_v41_test.go` | NFSv4.1 reaches the wire and a volume round-trips |
| `mount-options` | `mount_options_test.go` | `mountOptions` rendered onto the StorageClass *and* into the provisioning Secret |
| `reclaim-retain` | `reclaim_retain_test.go` | `reclaimPolicy: Retain` keeps the PV and the data on the server |
| `node-labels` | `node_labels_test.go` | without a `workloadNodes` selector every node is labelled and the node DaemonSet covers them |
| `workload-nodes` | `workload_nodes_test.go` | `workloadNodes.nodeSelector` confines the DaemonSet, the scheduler extender comes up, and a Pod pinned elsewhere stays `Pending` |

The lifecycle scenarios each run the same eight steps - StorageClass
materialisation, create + round-trip, expand, pod migration, RWX from two nodes,
snapshot + restore, clone, delete. The steps live in `helpers_lifecycle_test.go`
so the four scenario files stay readable, and each file spells out its own `It`
blocks.

`workload-nodes` is excluded from the default run (`!workload-nodes`): it is only
meaningful when it owns the cluster, because the controller unions the selectors
of *all* `NFSStorageClass`es, so one selector-less class elsewhere in the run
would re-label every node and void the assertions. It skips itself if any
`NFSStorageClass` already exists.

### Server-side assertions

`inspector_test.go` mounts each plain export root through the **in-tree**
`kubernetes.io/nfs` volume plugin — deliberately bypassing csi-nfs — so specs can
look at what the driver actually did to the share: that `<share>/<PV name>` was
created, that `chmodPermissions` landed on it, that the Pod's bytes are really
there, that `Delete` removed it and `Retain` did not. It does not work against
the TLS exports (an in-tree mount carries no `xprtsec`, which a TLS-only export
refuses by design), so the TLS suites run without it.

## Configuration

The cluster knobs (`TEST_CLUSTER_*`, `SSH_*`, `DKP_LICENSE_KEY`, …) are
storage-e2e's; run `make check-env` for the full list. The csi-nfs-specific ones:

| Variable | Required | Default | Meaning |
| -------- | -------- | ------- | ------- |
| `E2E_NFS_V3_HOST` | for `lifecycle-v3` | — | NFSv3 server address |
| `E2E_NFS_V3_SHARE` | no | `/srv/nfs/e2e` | export path on it |
| `E2E_NFS_V4_HOST` | for the v4 scenarios | — | NFSv4.1/4.2 server address |
| `E2E_NFS_V4_SHARE` | no | `/srv/nfs/e2e` | export path on it |
| `E2E_NFS_V4_SHARE_ALT` | no | `/srv/nfs/e2e-alt` | second export, used by `reclaim-retain` |
| `E2E_NFS_TLS_HOST` | for `lifecycle-tls` | — | `xprtsec=tls` server address |
| `E2E_NFS_TLS_SHARE` | no | `/srv/nfs/tls` | `xprtsec=tls` export |
| `E2E_NFS_MTLS_HOST` | for `lifecycle-mtls` | — | `xprtsec=mtls` server address; **must differ** from the tls host |
| `E2E_NFS_MTLS_SHARE` | no | `/srv/nfs/mtls` | `xprtsec=mtls` export |
| `E2E_NFS_TLS_CA` | for tls/mtls | — | **secret**, base64 PEM root CA |
| `E2E_NFS_TLS_CLIENT_CERT` | for mtls | — | **secret**, base64 PEM client cert |
| `E2E_NFS_TLS_CLIENT_KEY` | for mtls | — | **secret**, base64 PEM client key |
| `E2E_PVC_SIZE` | no | `1Gi` | |
| `E2E_PROBE_IMAGE` | no | `busybox:1.36` | needs `sh`, `cat`, `stat` |
| `E2E_MODULE_READY_TIMEOUT` | no | `15m` | |
| `E2E_TLS_ROLLOUT_TIMEOUT` | no | `15m` | node DaemonSet rollout after `tlsParameters` |
| `E2E_TEST_TIMEOUT` | no | `2h` local, `3h30m` in CI | whole-suite Ginkgo timeout |

The three PEM values are written into the csi-nfs `ModuleConfig` as
`settings.tlsParameters` — **lazily, by the first tls/mtls scenario**, never by
suite setup. `tlsParameters` add the `net-handshake-checker` init container to the
node DaemonSet *and* to the CSI controller Deployment, so on a node whose kernel
lacks `CONFIG_NET_HANDSHAKE` both crashloop, and a controller that lands there
leaves the cluster with no provisioner at all — which would fail the plain
NFSv3/v4 scenarios too. A run that does not exercise TLS therefore leaves the
module untouched.

If `E2E_NFS_TLS_CA` is unset, or the running edition has no RPC-with-TLS feature
(CE), those scenarios skip instead of failing.

Nothing about the stand is committed to this repo — hosts and PEM material come
from the environment only, and a scenario whose server is unset skips itself, so a
partially configured environment degrades to fewer scenarios rather than to
failures.

## Running

```bash
make check-env                              # show every knob
make test                                   # everything except workload-nodes
make test-scenario SCENARIO=lifecycle-v3    # one scenario
make test-tls                               # the RPC-with-TLS scenarios
make test-workload-nodes
```

The provider comes from `E2E_TEST_CLUSTER_PROVIDER` (`commander` or `dvp`), read
by `storage-e2e`'s `pkg/e2e` when a scenario connects. Each scenario takes its own
cluster lease, so a failed run does not leave the cluster locked.

To keep a cluster alive for debugging, use the **`e2e/keep-cluster` PR label** -
the suite itself never tears a cluster down. The label must be on the PR *before*
the label that triggers the run, because `keep_cluster` is resolved from the
labels in the triggering event's payload.

## CI

`.github/workflows/e2e-tests.yml` is a thin caller for the reusable
`deckhouse/storage-e2e/.github/workflows/e2e.yml` pipeline with
`cluster_provider: commander`, gated on the `e2e/commander/run` PR label — same
shape as csi-ceph. Other labels: `e2e/keep-cluster` (skip teardown),
`e2e/label:<scenario>` (Ginkgo label filter, e.g. `e2e/label:workload-nodes`).

In the commander flow only the `modules:` list of `cluster_config.ci.yml` is
consumed; the cluster comes from the Commander template. Two things the template
must provide, because the config file cannot:

- **node kernels that can do RPC-with-TLS** — >= 6.5 with `CONFIG_TLS` and
  `CONFIG_NET_HANDSHAKE`. This is not just a tls/mtls concern: once
  `tlsParameters` are set, *every* csi-nfs node Pod gains the
  `net-handshake-checker` init container, so a node that cannot do handshakes
  crashloops and the DaemonSet never finishes rolling out;
- **at least two schedulable workers** for the pod-migration and RWX-from-two-
  nodes specs (they `Skip` rather than fail with one).

The shared template is multi-distro (`ubuntu`, `debian`, `redos`, `astra`), and
two of those distros ship kernels without `CONFIG_NET_HANDSHAKE`: on both Astra
and Debian the `net-handshake-checker` init container crashloops
(`BackOff: Back-off restarting failed container`, observed at 19 restarts). The
node set is shaped through the repository variable `E2E_COMMANDER_VALUES`, a JSON
blob of template inputs the pipeline merges with the cluster `prefix`:

```json
{"astraNodeCount": 0, "debianNodeCount": 0, "ubuntuNodeCount": 2}
```

That leaves one master plus three workers — two Ubuntu and one RedOS, all of them
handshake-capable — which is also the minimum the migration, RWX and
`workload-nodes` scenario needs. Extend this JSON rather than replacing it if the
template ever needs more inputs.

> Excluding those distros is not a workaround for a test bug. `tlsParameters`
> add `net-handshake-checker` to the CSI **controller** Deployment as well as the
> node DaemonSet (`templates/csi/controller.yaml` feeds `csi_init_containers` to
> both), and the controller is placed by the ordinary scheduler on any node
> carrying `storage.deckhouse.io/csi-nfs-node`. A controller that lands on a
> handshake-incapable node crashloops, and the cluster is left with no
> provisioner at all — every PVC stays Pending, TLS or not.

The stand coordinates reach the runner through the pipeline's `extra_env` input.
Configure the module repo once:

**Repository variables** — non-secret, echoed to the job log:

| Variable | Value on the current stand |
| -------- | -------------------------- |
| `E2E_NFS_V3_HOST` | the NFSv3 server |
| `E2E_NFS_V4_HOST` | the NFSv4.1/4.2 server |
| `E2E_NFS_TLS_HOST` | the `xprtsec=tls` server |
| `E2E_NFS_MTLS_HOST` | the `xprtsec=mtls` server (must differ from the tls one) |

**Repository secret `E2E_MODULE_ENV`** — the pipeline reads it by fixed name and
masks every value, so `secrets: inherit` keeps working unchanged. One
`KEY=VALUE` per line:

```ini
E2E_NFS_TLS_CA=<base64 PEM>
E2E_NFS_TLS_CLIENT_CERT=<base64 PEM>
E2E_NFS_TLS_CLIENT_KEY=<base64 PEM>
```

A partially configured repo degrades gracefully: each suite skips itself when its
host or PEM material is missing, rather than failing.
