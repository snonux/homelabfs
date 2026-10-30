# Pending-task reconciliation, 2026-10-30

Parent review: `5z3`. [Unified review](20261030-multiwriter-review.md). All 36 original pending tasks are retained, with superseding scope annotations and new prerequisites. Original descriptions, annotations and evidence remain historical context; requirements below supersede owner-only assumptions. No historical completed task is reopened merely for its old scope.

## New prerequisites and remediation tasks

| Work package | Task | Outcome |
|---|---|---|
| B1 | `7z3` | Exact native hostname admission |
| B2 | `8z3` | Signal-safe child ownership |
| B3 | `9z3` | Strict JSON size/EOF |
| B4 | `az3` | Preserve failed PoC resources |
| B5 | `bz3` | Umask-independent negative fixture |
| W1 | `cz3` | All-origin normal protocol |
| W2 | `dz3` | Bounded multi-origin/RAM models |
| W3 | `ez3` | Versioned origin/order representation |
| W4 | `fz3` | Committed logical RAM views |
| W5 | `gz3` | Recovery/checkpoint/retention contract |
| W6 | `hz3` | Transaction/affinity composition |
| W7 | `iz3` | Concurrent external history oracle |
| W8 | `jz3` | Retained9p authorization and multi-client compatibility |
| W9 | `kz3` | Demand policy and cooperative handoff spec/model |
| W10 | `lz3` | Demand placement/handoff implementation |

## Existing pending tasks

| Task | Superseding scope and acceptance | Added prerequisites / removed edge |
|---|---|---|
| `4r3` | Replace fixed-owner replication with origin-local durable commit plus ordered preferred-owner RAM retention; owner-origin uses distinct partner. Any admitted host may write any volume; permissions, grants, global order and complete dependency prefix remain mandatory. Verify concurrent SAME-volume origins, delayed owner disk and no candidate reads. | `cz3`, `dz3`, `ez3`, `fz3`, `gz3`, `hz3`, `6r3` |
| `5r3` | Retain mandatory persistent async delivery; track every durable origin source and selected global history, disconnect backlog and checkpoints. RAM receipt never authorizes durable trimming. | `gz3` |
| `6r3` | Move admission budgets before replication. Reserve owner RAM logical versions, origin journal/decision/result dependencies, recovery capacity and network fairness before admitting writes. Preserve bounded catch-up fairness; no dependence on completed async implementation. | `cz3`, `dz3`, `ez3`; remove `5r3` |
| `7r3` | Restart under installed multi-origin authority: reconcile origin journals, unknown slots/results and committed logical cut; preserve incarnation fencing. Current genesis-only replay is not checkpoint-aware trimmed startup. | `gz3`, `fz3` |
| `8r3` | Trim/resync only with complete all-origin durable checkpoint/source coverage and object-home/transaction/request-result pins. Preserve archive identity/allocation history; owner RAM retention is not durable trim permission. | `gz3`, `hz3` |
| `dr3` | Supersede clock-expiry wording with durable promises and positive fencing covering all admitted origin/reader/cache domains. etcd grant or timeout alone never stops stale local/cached I/O. | `cz3`, `dz3`, `gz3` |
| `er3` | Recover/gate complete dependency closure and writer grant set from actual durable origins plus retained owner memory; validate lineage before activation. Separate crash takeover from cooperative preferred-owner handoff. | `gz3`, `hz3` |
| `fr3` | Promotion/partner replacement reconciles all-origin uncertain suffixes and retained memory continuity. No automatic old-owner failback; returning eligible host may later win demand policy after cooldown. | `gz3` |
| `gr3` | Whole-cluster reboot/manual recovery surveys all durable origin journals and global order/dependencies, including owner RAM tail never written to owner disk. Missing evidence enters explicit loss namespace/unavailable state; no stale local prefix declared fresh. | `ez3`, `gz3` |
| `hr3` | Extend bounded simulator faults to concurrent same-volume origins, paused owner/origin, delayed ACKs, origin disk loss, owner RAM reads and all client/cache domains. External concurrent oracle and new models precede acceptance; retain old serial evidence with dated scope. | `dz3`, `iz3` |
| `lv3` | Keep exact approved native VM-only positive-fence broker; cover all admitted origin/reader/cache domains and collateral volume tails. Preserve legacy form1 and explicit future schema adaptation. Local implementation does not itself prove installation/native operation. | `cz3`, `ez3`, `gz3` |
| `8w3` | Replace owner-local-only execution premise with authenticated all-origin anchored transaction execution using global order and committed RAM view. Preserve stable anchor/dedup/results, EXDEV, home affinity, complete dependency closure and atomic visibility. | `ez3`, `fz3`, `hz3` |
| `9w3` | Retain native multi-mount transaction acceptance with concurrent origins, object-home closure and owner RAM visibility. Ordinary cross-volume syscalls still EXDEV. Native matrix contains no9p. | `iz3` |
| `zq3` | Keep this as pre-replication REAL-daemon single-node guest transport/Unix/flush/cache base gate. Retained9p is selected; missing server flock does not forbid independent filesystem writers. Do not claim this base gate proves future distributed coherence. Resolve guest authorization explicitly; full concurrent-client replication belongs ov3/pv3 after4r3, preventing dependency cycle. | `jz3` |
| `br3` | Fixed placement native replication tests must exercise all origins writing SAME volume, with preferred owner disk delayed while committed RAM reads succeed. Separate replica placement from origin. Keep native FUSE direct path and real evidence, no9p. | `fz3`, `iz3` |
| `jr3` | Native protocol/recovery matrix consumes real multi-origin implementation and oracle; declarative scenarios/fake executors are not execution evidence. Include ordered RAM tail and all-origin loss/restart cases. Do not assume fourth physical node exists. | `iz3` |
| `lr3` | Keep explicit single-survivor local-only emergency exception with positive exclusion of every other origin/reader, loss identity and exact activation controls. New normal representation/recovery must exist before executable entry. | `ez3`, `gz3` |
| `mr3` | Keep recovery-only admitted peers unable to ACK/serve; exit uses two durable copies/final barrier and reinstates revised normal all-origin contract. Verify interrupted admission/exit and lost history, never auto local-only downgrade. | `gz3` |
| `rw3` | Emergency codec composes with selected new normal schema while preserving v1 and emergency goldens. Keep loss namespace/dependency DAG/checkpoint/exit proof semantics; no synthetic self-partner ACCEPT or hidden v1 weakening. | `ez3`, `gz3` |
| `ur3` | CSI design must permit admitted multi-host read/write publication independent of preferred owner and pod movement. Preserve explicit access-mode/user decisions, permission/guest cache generation fences and affinity aliases. Owner is locality hint, not exclusive write lock. | `jz3`, `kz3` |
| `vr3` | Break down CSI work from revised multi-host publication contract; preserve node admission, guest identities/coherence, independent lifecycle and ownership locality hints. Optional CSI remains unimplemented until actual tasks land. | `jz3` |
| `nr3` | Specify/model joint membership and replica placement before implementation, including authorized origins outside three-replica set and durable source coverage. Fourth node may originate writes for any volume after proper admission; empty node is not recovered replica. No automatic REPLICA-SET redistribution; periodic preferred-owner placement D9/D10 remains required and distinct. | `cz3`, `dz3`, `ez3`, `gz3` |
| `or3` | Snapshot barrier must capture complete durable all-origin/dependency cuts and metadata, not merely owner RAM visibility or an unsafe recursive parent snapshot. Keep allowlisted datasets, coordinated transaction closure and external snapshot authority. | `gz3`, `hz3` |
| `nv3` | Backup bundle must contain complete origin cuts, ordered recovery/transaction dependencies and retention keys. Preserve external snapshot ownership, f2 receive-only sink and restore verification; no direct modification/boot of sink. | `gz3` |
| `pr3` | Security binds exact admitted origin, order/slot grant, owner memory receipt and historical transfer permissions; membership/owner UUID alone not authority. Preserve transport TLS identity/revocation and etcd RBAC. Actual9p guest credentials require selected D8 boundary; never trust bhyve host-root FUSE headers as guest identity. | `cz3`, `ez3`, `jz3` |
| `qr3` | Expose receipt/durable/commit/logical-visible/disk-applied frontiers and origin durable sources separately. Add original request counts by host+volume, excluding replication/retries, bounded cardinality and guest cache-hit observability limits. D10 needs thin placement counters earlier than full monitoring stack. | `cz3`, `fz3`, `kz3` |
| `rr3` | Keep later rolling upgrade/version compatibility integration; early normal representation is separately D3 before replication/recovery. Preserve literal v1/Emergency records, strict feature/unknown-version behavior, operational readiness and whole-unit upgrade constraints; no dependency cycle through gr3. | `ez3` |
| `tr3` | Packaging must publish version/capability/readiness roles for all-origin commit and owner RAM bootstrap, complete actual dependency/license scope and reproducible validated artifacts. No distribution or normal serving claim before respective gates; source probe selection is not runtime readiness. | `ez3`, `jz3` |
| `yr3` | Break down live smoke from actual Stage2 adapter and multi-origin guest profile requirements, preserving disposable capacity/isolation and artifact admission. Plan now, execution after real bindings/cw3 and core gates. Do not add dependencies on future execution children that create cycles; CSI off until implemented. | `cz3`, `jz3`, `kz3` |
| `mv3` | Power harness drains every origin and makes complete committed RAM/dependency cut durably recoverable before graceful stop. Depend on actual recovery/fence implementation; no production power automation or forced Rocky operations. | `lv3`, `er3`, `gz3` |
| `ov3` | Isolated profiles must cover concurrent SAME-volume hosts, committed owner RAM before disk, guest caches/mmap, origin/owner crash and cooperative request-driven handoff. Plan sufficient separate guest fixtures; current one12GiB Rocky cannot imply two writable guests. Preserve numeric calibration vs actual evidence distinction. | `iz3`, `jz3`, `lz3`, `cw3` |
| `pv3` | Execute real isolated multi-origin/9p profiles only after actual integration and fixture/capacity admission. No fake executor or local flock results as cross-host storage proof. Preserve bounded disposable resources, exact critical approvals, failure evidence and normal cleanup. | `lz3` |
| `cw3` | Real Stage2 adapter must bind multi-origin identities, logical/durable frontiers and external concurrent oracle to actual operations. Preserve exact plan/resource bounds, uncertain SSH completion reconciliation and restoration; unknown completion never licenses blind retry. | `ez3`, `fz3`, `iz3`, `jz3` |
| `aw3` | Actual native transaction integration must prove concurrent origins, closure ordering and owner RAM visibility, with retained exact binaries/results and ordinary EXDEV. No9p in native suite. | `iz3` |
| `sr3` | Phase4 native apps/emergency/membership/snapshot/security/ops inherits multi-origin model, logical view and durable recovery. Include D10 preferred-owner handoff acceptance; fourth-node profiles stay unexecuted if fixture missing. Preserve explicit emergency and infrastructure safety limits. | `iz3`, `lz3` |
| `uy3` | Retain independent intermittent SSH diagnosis: cause remains UNKNOWN; correlate next actual incident with bounded exact observations, not blind retries or configuration guesses. Historical post-auth timeout is not initial-connect failure. This follow-up does not block native baseline or core multi-origin design. | None beyond review freeze |

All baseline tasks retain `5z3` as a dependency until this review is complete. Admission budgets `6r3` move before `4r3`; early representation `ez3` is split from later rolling upgrades `rr3`. The single-node real-daemon 9p gate `zq3` remains before replication. Full concurrent guest coherence acceptance follows replication in `ov3`/`pv3`; putting it inside `zq3` would create a cycle. Planned owner handoff `kz3`/`lz3` is distinct from crash takeover and replica-set admission `nr3`.

Final task-state snapshots and the before/after audit trail are retained privately under `/home/paul/tmp/codex/hfs-multiwriter-review-20261030`. This table records required changes; review completion also requires checking they are applied in `ask`, with no dependency cycles.

Audit remediation closure is `mz3`, dependent on all five bug and ten design/model/implementation tasks above. Marker task `nz3` depends on that gate and owns exact local start marker `audit/2026-10-30`; remote push still requires user authorization. Neither task is completed by review `5z3`.

Applied-state verification completed: all 36 task updates and required prerequisites are present, the obsolete budget edge is removed, and the checked graph has no cycle. See [verification status](20261030-task-reconciliation-status.json). The user requested stopping after this batch; no newly unblocked task is started.
