# Canary deployment readiness

[Architecture](Architecture.md) · [Kubernetes canaries](Kubernetes-and-migration.md) · [Deployment tests](Deployment-tests.md)

## Scope and completion

This project implements HomelabFS and validates it with disposable lab and
canary volumes containing synthetic data. Its endpoint is a tested implementation,
canary evidence and documented limitations. Production workload migration and
NFS retirement are possible future steps owned by a different project. Do not
create production migration, cutover or NFS-removal tasks here.

Existing workloads, PVs, NFS exports, stunnel, CARP, mount monitors and backup
sinks remain in place. Canary installation on shared f3s hosts requires a separate
approved plan; this document authorizes no deployment or test run. Canaries use
unique identities and paths, no production data copies, explicit quotas and
bounded CPU/RAM/I/O/network use. Disruptive host/guest experiments require
isolated test infrastructure and separately approved runs.

## Observed baseline: 2026-10-28

After an authorized start, f0–f2 hosted three Ready Rocky 9.8 r-nodes on k3s
v1.36.5+k3s1 and kernel `5.14.0-687.60.1.el9_8.x86_64`.
`modinfo 9pnet_virtio` failed on all three. The f3/rocky PoC kernel is a different
validation target; new canary guests or approved kernel changes must be planned
explicitly, never assumed from the PoC.

| Host | zdata pool size | Dataset space available |
|---|---:|---:|
| f0 | 3.63 TiB | 1.42 TiB |
| f1 | 3.62 TiB | 1.43 TiB |
| f2 | 928 GiB | 670 GiB |

f0's NFS dataset referenced 1.97 TiB and used 2.30 TiB including retained state.
A bounded 40-second directory scan timed out. These observations do not establish
a migration footprint and do not require this project to replicate the NFS tree.
Only the selected bounded canary footprint needs to fit, alongside existing
services and backups. Raw transcripts were not committed. Sources were read-only
`zpool list`, `zfs list`, `vm list`, `uname`, `modinfo` and `kubectl` queries;
local chart evidence is in `~/git/conf/f3s/*/helm-chart/templates/`.
Reinspect before any approved execution; dated availability is not admission.

## Inventory before volume design

Use a focused test-pattern inventory to inform semantics, not an exhaustive
production migration inventory. Player reads Navidrome music and Syncthing data;
Navidrome/beets-art share a music directory; Immich has aliased video PVs;
Shuriken mounts a parent tree. Reproduce these shapes with small synthetic
fixtures. Distinct PVC names do not imply independent data. Define supported
same-owner co-location, aliasing and nested-root behavior, or explicitly reject
unsupported cross-owner sharing. Include transitive multi-volume placement in
tests without changing production scheduling. Earth remote mounts are recorded
as future context; replacement of remote NFS access is outside this project.

## Capacity and resource admission

Use the proposed [bounded canary capacity plan](Capacity-plan.md) for numeric
quotas, staging/reserve arithmetic and per-host admission equations. Its ceilings
are not measured spare capacity or permission to deploy or run tests.

Budget selected canary data, journals, snapshots, checkpoint staging and one
canary replica rebuild, plus evidence and cleanup reserves. Use actual allocation
and quotas, not summed PVC requests. Preserve headroom for current production
workloads, Garage, backups and VM allocations. Do not delete backups, modify
received sinks or buy larger storage to fit the full production dataset here.
If canaries do not fit, reduce the test profile or choose approved test placement.

Measure physical RAM/ARC, existing guest allocations, host etcd/daemon buffers,
CPU and WireGuard/catch-up bandwidth. The observed 12 GiB Rocky VM on f0 leaves
limited host memory on a 16 GiB machine. A 2 GiB native lab guest is not proof of
shared-host capacity. Revalidate budgets for every canary deployment and run.

## Fencing and coordinated power lifecycle

Choose the fence contract in the takeover specification before modelling it:
target identity, narrow credentials, independent management path, positive
completion evidence, deadlines, idempotency and inhibited stale rejoin. A
shutdown request is not proof that cached access stopped. Unknown fencing state
blocks promotion. Implement and validate real guest/host fences only against
explicitly approved isolated targets; shared-host canaries must not gain authority
to fence production guests or hosts. If full fencing cannot be demonstrated on
a live canary target, report that limitation and use the isolated profile.

Design and test quorum-preserving drain/shutdown and cold-start behavior on the
isolated cluster: quiesce clients while quorum exists, persist accepted work,
stop guest access, then storage/coordination. Distinguish mounted-but-gated from
serving readiness and prevent boot dependency cycles. Test interruption and
UPS-style deadlines in isolation. Current f3sctl stops non-master hosts in
parallel before the NFS master; it is context for a future production integration,
not a system to replace or deploy changes to during this project. Preserve all
existing NFS/CARP/monitor and production power automation.

## Release acceptance and independent recovery

Define numeric test-profile limits for latency, throughput, lag, catch-up,
recovery stages and resources at a stated synthetic workload size/backlog.
These are canary acceptance criteria, not production SLOs or production RPO/RTO
commitments. Record durability guarantees and the accepted volatile-partner loss
window; do not invent a bounded loss guarantee. Calibration runs need approval.

Specify supported canary kernel/daemon artifacts, provenance, signatures/trust,
pinning, update/revalidation and rollback. Missing 9p blocks publication. Test
SELinux and credentials on the selected guest versions; no fleet-wide kernel
rollout is implied. Keep test recovery artifacts independently accessible.

Implement paired canary data/journal/control snapshots with bounded retention,
test keys, explicit ownership and a separate writable restore destination. Test
restore and application integrity using synthetic fixtures. Preserve existing
production snapshot policies and received sinks. This does not establish a
production backup policy or migrate pkgrepo/registry/Forgejo storage.

Native Stage 1 and non-disruptive Stage 2 provide separate evidence. Additional
K01–K05, guest-cache fencing, physical-host loss, cold-start and independent
restore exercises use isolated test infrastructure, separately approved. If
independent physical hosts are unavailable, report that case not run; do not
require production shutdown as a condition of bounded live canary smoke success.
Keep each profile's result separate and make no untested reliability claims.

## Task ownership and ordering

| Requirement | Task aliases | Boundary |
|---|---|---|
| Representative synthetic workload patterns | `xr3` | Before volume spec `9q3`; no migration inventory |
| Canary resource budgets | `fv3` | Before fixture placement and canary installation |
| Canary fixture/volume mapping | `gv3` | After `9q3`; before CSI spec `ur3` |
| Fencing contract and isolated implementation | `eq3`, `lv3` | No production fence authority |
| Isolated lifecycle spec and harness integration | `hv3`, `mv3` | No production f3sctl/UPS changes |
| Supported canary kernels | `iv3` | Before guest compatibility `zq3` |
| Model and guest compatibility gates | `fq3`, `zq3` | Before replication implementation `4r3` |
| Numeric canary profiles | `jv3` | Before canary alerts `qr3` and acceptance |
| Canary snapshot/restore policy and tooling | `kv3`, `nv3` | No production backup-policy changes |
| Isolated deployment exercise scripts and runs | `ov3`, `pv3` | Separate from bounded live smoke; no production fault injection |
| Canary deployment and test task decomposition | `yr3` | Creates installation, smoke, cleanup and evidence tasks only |

`yr3` attaches concrete isolated setup dependencies to `pv3` before execution
and creates separate live canary installation and Stage 2 run tasks. Installation
still depends on `sr3`, `zq3`, `tr3` and actual CSI implementation where used
(`vr3` is only task decomposition). Completion of an isolated exercise or live
smoke task never authorizes production migration or NFS retirement.
