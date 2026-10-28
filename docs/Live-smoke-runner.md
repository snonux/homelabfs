# Stage 2 live smoke runner

`cmd/hfs-lab` supports `stage: "stage2"`, `profile: "live-smoke"` using the
same hash-bound approval, atomic journal, uncertainty/reconciliation handling,
bounded process execution and JSON/JUnit reporting as [Stage 1](Lab-runner.md).
This implements a controller and a deployment-adapter protocol. No deployment
or live smoke run has been performed. Stage 1 approval never authorizes Stage 2.

The baseline requires an already deployed candidate, dedicated smoke volumes,
one local-primary volume per selected running Rocky guest, and all L01–L09
checks. L10 is mandatory cleanup, separately visible in the report. There are
no VM lifecycle actions, daemon restarts, fault injection, ownership transfers,
production paths or package installation actions. Kubernetes is off by default
and currently unsupported: a Kubernetes profile is rejected instead of claiming
unimplemented publication checks passed.

## Prepare a concrete plan

Build the two controller executables:

```sh
go build -o bin/hfs-lab ./cmd/hfs-lab
go build -o bin/hfs-smoke-driver ./cmd/hfs-smoke-driver
```

Supply the normal complete plan fields described in [Lab runner](Lab-runner.md)
with `prepared_guests: false`, an empty `disruptions` array,
`preserve_failures: true`, and this additional `live` object:

```json
{
  "driver": "smoke-driver",
  "adapter": "deployed-adapter",
  "probe": "rocky-probe",
  "candidate": "candidate-package",
  "known_hosts": "pinned-keys",
  "stage1_plan": "passed-stage1-plan",
  "stage1_evidence": "passed-stage1-report",
  "deployment_evidence": "authorized-deployment-record",
  "max_operations": 1000,
  "uid": 21001,
  "gid": 21001,
  "denied_uid": 21002,
  "volumes": [{
    "id": "hfs-smoke-RUN_ID-f0",
    "guest": "r0",
    "owner": "f0",
    "epoch": 1,
    "replicas": ["f0", "f1", "f2"],
    "guest_mount": "/mnt/hfs-smoke/hfs-smoke-RUN_ID-f0",
    "host_mount": "/var/run/homelabfs/smoke/hfs-smoke-RUN_ID-f0",
    "export": "hfs-smoke-RUN_ID-f0",
    "max_bytes": 16777216
  }]
}
```

These are illustrative inputs, not live inventory or approved limits. Replace
the run ID, topology, owner epoch, users, versions and budgets with reviewed
values. Every named input is a pinned artifact with an absolute controller
path, version and SHA-256. `driver` is the built `hfs-smoke-driver`; `probe` is
`tools/lab/smoke-probe.py`. The controller checks that the successful Stage 1
report matches its pinned plan and includes the exact candidate bytes/version.
The deployment adapter must additionally verify that these bytes are deployed
and that the deployment authorization applies to this topology.

There must be one corresponding `targets` entry per volume, identifying its
Rocky guest name/address, colocated FreeBSD host, `Running` state, kernel and
package-manifest versions. Each corresponding resource must have the volume
ID as `name`, `kind: "smoke-volume"`, guest as `target`, guest mount as
`location`, and exactly the volume quota as `max_bytes`. Volume IDs begin
`hfs-smoke-RUN_ID-`; mount/export paths use exactly the dedicated namespaces
above. Controller state and evidence directories cannot overlap each other
or these mounts. Neither raw backing trees nor existing application volumes
are permitted.

Use explicit ordered selections `L01` through `L09`, each with a seed. Budgets
include positive host headroom, disk/I/O/evidence caps, retention hours and
execution/cleanup seconds. Guest memory, CPUs and restoration seconds are
zero because this runner does not allocate or restore guests. Set `steps: []`
and generate the entire supported schedule locally:

```sh
bin/hfs-lab smoke-plan --input proposed-live.json --out reviewed-live.json --step-seconds 30
```

This reads local artifacts, validates the Stage 1 evidence, writes the complete
canonical plan and prints its hash. It never contacts a host, invokes an
adapter, creates a smoke volume or runs a probe. All generated steps and
deadlines are visible before approval. The generic `plan` command also accepts
an already complete Stage 2 schedule. Sum of deadlines must fit the execution
and cleanup allowances. Changing the schedule, epochs, endpoints, artifacts or
limits changes the hash and requires fresh approval.

After reviewing that plan and the pinned deployment adapter, record the
operator approval using the [existing approval format](Lab-runner.md#record-approval-and-execute).
Use `execute`, `status`, `cleanup` and explicit reconciliation exactly as for
Stage 1. Critical live operations retain their per-run approval boundary.

## Deployment adapter protocol

The deployment management/export/observability APIs are not implemented yet.
An actual deployment must provide the pinned local adapter executable before
this runner can execute. There is no successful stub or SSH-to-arbitrary-shell
fallback. The adapter receives one strict JSON `SmokeRequest` on stdin and
returns exactly one `SmokeReceipt` on stdout; stderr must not be used for
unbounded diagnostics. Stdout JSON and stderr diagnostics have one shared
aggregate byte cap, enforced while reading both streams concurrently. The
driver parses stdout alone and records captured stderr in the returned
receipt's `adapter_diagnostics` field, including on malformed output, process
failure or truncation at the cap. The driver owns this field; an adapter cannot
replace captured diagnostics through its stdout JSON. Receipts are printed to
the bounded step log. The Go wire types are in
[`smoke_driver.go`](../internal/labrunner/smoke_driver.go).

The request includes protocol `hfs-live-v1`, plan hash, complete approved plan,
exact step, operation and volume ID. Adapters receive no custom argv. Only a
fixed PATH, HOME and optional SSH agent socket are inherited. The adapter must
use the pinned known-hosts artifact, strict host-key checking and the approved
endpoints. No guest private keys belong in the request, guest or evidence.

The adapter owns every child and remote action. It must enforce remote
deadlines, quota/operation/I/O/evidence limits, capture evidence, and establish
termination before returning `terminal: true`. Lost transport, a timeout kill,
malformed/truncated output or an unknown adapter exit is uncertain and blocks
all cleanup pending explicit reconciliation. A receipt with matching protocol,
plan hash, step and volume, `terminal: true`, `success: false` is a known failed
assertion. Empty or missing evidence cannot pass.

Every successful receipt supplies the exact ordered `checks` below and a
nonempty structured object/array under `evidence[CHECK]` for each check. These
objects contain the actual inventory, syscall outcomes, hashes, histories,
timings, versions, health observations and administrative replies supporting
the check, not a boolean assertion or a label saying “passed”. Receipts become
bounded step logs outside the smoke volumes.

| Operation | Required ordered checks and adapter behavior |
|---|---|
| `preflight` | `stage1-candidate-pass`, `deployment-authorized`, `versions`, `pinned-ssh`, `headroom`, `health-before`; reject unavailable or changed live inputs before provisioning |
| `create` | `dedicated-volume`, `quota`, `local-primary`, `run-ownership`; create only the exact exclusive run-owned volume through the deployed management API, publish normally and establish the reviewed epoch |
| `L01` | `live-mount-chain`, `9p-capabilities`, `owner-ready`, `replicas-ready`, `versions`, `numeric-identities`; verify guest 9p source/export and host HomelabFS FUSE session/volume, never merely directory existence |
| `L02` | `write-fsync-reopen-checksum` |
| `L03` | `mkdir-rmdir`, `rename-replace`, `truncate`, `hardlink`, `unlink-open` |
| `L04` | `uid-gid-mode`, `allowed-access`, `denied-access` |
| `L05` | `flock-contention`, `fcntl-contention`, `mmap-flush-sync`; two-process contention in the selected guest mount, without claiming independent-client locking |
| `L06` | `sqlite-delete-full`, `sqlite-wal-full`, `sqlite-reopen-integrity` |
| `L07` | `sync-contract`, `async-catchup`; observe the checkpoint belonging to this run's mutations and poll all configured replicas with a deadline |
| `L08` | `checkpoint-inspection`, `content-metadata-match`; use supported passive-replica inspection at that checkpoint, never ordinary reads from passive mounted replicas |
| `L09` | `bounded-mix`, `health-after`, `bounded-lag`, `bounded-resources`; supplement the guest probe with bounded administrative health observations |
| `collect` | `versions`, `checksums`, `operation-history`, `replication-positions`, `health`; read-only collection remains eligible after a known forward failure |
| `L10`, successful run | `clients-stopped`, `normal-unpublish`, `volume-deleted`, `no-residue`, `health-unchanged`; stop only run clients, normally unpublish, delete through supported APIs and verify exact run ownership; never force unmount or delete other resources |
| `L10`, failed run | `clients-stopped`, `normal-unpublish`, `failed-data-preserved`, `retained-inventory`, `health-unchanged`; normally stop/unpublish run resources, preserve the failed payload/volume and return the bounded manifest described below |

Per-volume receipts identify the exact guest, owner, epoch, guest/host mounts,
export, guest `filesystem: "9p"` and `host_filesystem: "homelabfs-fuse"`.
The adapter must derive this chain from the live deployment, including the
FUSE session's volume ID; a raw ZFS export cannot satisfy it. Revalidate
identity/readiness before each probe, not just once during L01. Create/L10
receipts retain the identity observed while publishing/unpublishing that same
volume; disappearance alone does not prove cleanup.

L07/L08 receipts include a nonzero run checkpoint, an external expected
`oracle_digest`, and exactly the configured
replica set. Every replica's received, durable and applied positions must reach
it before the deadline. L08 additionally includes matching SHA-256 digests of
canonical content **and metadata** at the common checkpoint. Missing inspection
support fails the selected baseline; it never silently reduces the profile.
The adapter derives the expected digest from its controller-side oracle for the
run's known operations. The driver persists the L07 receipt under controller
state, passes that checkpoint/digest in the L08 request, and rejects inspection
of a different checkpoint or digest. Agreement between replicas alone cannot
replace the expected oracle.

I/O counters count reserved logical workload bytes and high-level operations.
Each of the six mutating probe operations receives at most `io_bytes / (7 *
volume_count)` and `max_operations / (7 * volume_count)`; the spare seventh
slice covers conservative reservations. All other receipt workload counters
are zero. Adapter administrative calls remain bounded by deadlines and quotas.
These reservations are safety budgets, not throughput measurements. The adapter
must enforce the budget before issuing I/O; rejecting an over-budget receipt
afterward is only an additional guard.

`max_operations` reserves high-level probe calls including reads, opens, fsync,
permission/lock checks and SQLite work; it is **not** a workload-mutation
counter. The minimum of 700 reservations per volume admits seven slices of 100,
including the probe's conservative 96-operation SQLite reservation. Likewise,
`io_bytes` reserves logical read/write traffic rather than live file payload.
This functional `live-smoke` profile is separate from the calibrated acceptance
profiles' 256 workload mutations and 4 MiB live payload. The runner does not
implement their exact mutation ledger, latency/rate limits or physical-I/O
accounting, and a smoke pass makes no claim that those numeric profiles passed.
Do not reuse the illustrative budgets above as numeric acceptance approval.

## Rocky probe and failure preservation

The adapter invokes the pinned Python 3 probe **inside the real selected Rocky
guest** for L02–L06/L09, forwarding the request on stdin and verifying the
remote probe digest first. Python 3 and its SQLite module must be available;
the adapter must not install packages automatically. The probe validates the
exact 9p mount/source and open mount ID from `/proc`, rejects symlink mount
paths, opens a unique operation directory exclusively, and never adopts or
deletes an existing directory. Local deadlines and file-size caps complement
the adapter's remote termination and management quota enforcement.

It performs real fsync/reopen/checksum, metadata operations, numeric permission
checks, two-process flock/fcntl contention, mmap flush/fsync, and tiny SQLite
DELETE/WAL FULL-sync transactions with reopen/integrity checks. Permission
checks require a root probe that drops to the two approved positive numeric
UIDs in short-lived children; missing privileges are a failed gate. SQLite
uses fixed transactions, eight-page main database caps and a conservative
1 MiB logical-I/O reservation. The probe's L09 performs eight independent tiny
file writes/reads; the adapter supplies the additional health checks. None of
these results claim distributed crash durability or cross-client lock safety.

The driver reads the durable run journal before L10. It always invokes the
deployment adapter for applicable known-terminal teardown. A known failure sets
`preserve_failed_data: true` in the request: stop/reap all run clients, normally
unpublish guest/host access, check service health and preserve failed data.
Preservation does not leave test clients or live publications running. Uncertain
effects already block teardown in the shared runner pending reconciliation.

A successful preservation receipt must set `clients_stopped: true`,
`preserved_failed_data: true`, `volume_deleted: false`, `no_residue: false`,
and supply exactly one `retained_volumes` inventory entry for this step's
declared volume. The typed entry contains exact `id`, `guest`, `owner`,
`exists`, `published: false`, the approved `max_bytes`, observed `used_bytes`
between zero and that quota, and `retain_until` equal to the plan's creation
time plus `retention_hours`. The management API must establish absence
explicitly (`exists: false`, `used_bytes: 0`) if creation failed before a
volume existed; that receipt uses `epoch: 0` and empty guest/host filesystem
fields instead of claiming an observed live mount chain. Approved endpoint,
volume/export and mount-path fields still identify exactly what the absence
inspection covered. A missing mount alone is not absence evidence. No destructive
volume/payload deletion is allowed in preservation mode. Retention deadlines
remain visible even if delayed recovery passes them; there is no automatic
delayed deletion or implicit retention extension.

Successful-run cleanup has `preserve_failed_data: false` and requires
`clients_stopped: true`, `preserved_failed_data: false`, `volume_deleted: true`,
`no_residue: true` and no retained inventory. An adapter that cannot normally
unpublish or provide preservation inventory must fail cleanup explicitly;
missing support cannot become a cleanup pass. Successful preservation teardown
does not erase the original failure: JSON/JUnit remain unsuccessful and list
the potentially retained resources alongside the detailed cleanup receipt.

Local unit tests use fake executors/adapters and temporary files. Probe method
tests bypass mount entry and operate only in temporary directories. They do
not establish that any real 9p/FUSE path, deployment adapter or replica API
works. No production deployment, VM action or mount operation is exercised by
these unit tests.
