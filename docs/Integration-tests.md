# HomelabFS integration test plan

[Architecture overview](Architecture.md)

Planned fault-injection and application tests. No distributed HomelabFS implementation or protocol fault tests have been run; the expected outcomes are acceptance requirements. Initial [9p/FUSE passthrough tests](PoC-fuse-and-benchmarks.md) now provide limited transport evidence and do not satisfy the whole matrix.

**Execution requires explicit approval for each run.** Both stages are script-driven; see [Test automation](Test-automation.md). This plan does not authorize starting VMs or tests.

## Scope and two-stage validation

**Stage 1 tests HomelabFS itself:** three disposable FreeBSD bhyve VMs, each running its own real FreeBSD kernel, ZFS storage, and HomelabFS FUSE daemon. Test programs run inside those VMs and access their local FUSE mounts directly. No Rocky guest, 9p export, Kubernetes, or nested virtualization is required for this suite.

After Stage 1 passes, deploy the same validated artifacts to the real f3s hosts. **Stage 2 runs bounded live end-to-end smoke tests from the Rocky VMs**, described in [Live smoke tests](Live-smoke-tests.md). A passing Stage 1 run does not establish that the deployed guest transport is configured correctly.

See [Disposable FreeBSD test lab](FreeBSD-test-lab.md) for provisioning, memory admission, maintenance of f3's existing Rocky VM, evidence capture, cleanup, and shutdown.

## Early compatibility gates

The native FUSE gate covers permissions, locking, mmap, sync, cache invalidation, and mount loss on actual FreeBSD kernels. Run small single-node cases before distributed fault injection. Existing 9p PoCs remain useful deployment evidence; their additional compatibility requirements are separate from this suite, in [Deployment compatibility tests](Deployment-tests.md#early-compatibility-gates).

## Test environment and evidence

Use three independently stoppable FreeBSD VMs with separate test disks and guest ZFS pools, an external test harness, and a private three-member etcd cluster. Each VM may host one etcd member on local storage outside HomelabFS. Add an optional fourth VM for membership cases only when memory admission permits it. Test-cluster credentials, networking, and membership must be separate from production. Run Linux FUSE portability tests separately when that adapter is available.

Control failures at the test-process, FUSE-daemon, FreeBSD-VM, replication-link, coordination-link, and disposable-disk boundaries. `kill -9` tests process failure, not power loss. Terminating one bhyve test VM removes that guest's volatile state without stopping its physical host. Account for buffering in the outer hypervisor/storage when interpreting persistence results. Three VMs on f3 do not demonstrate independent physical-host failure or power-loss durability.

Instrument explicit failpoints around operation admission, journal append, local sync completion, partner receipt, partner ACK transmission, commit/visibility decisions, application, client reply, epoch persistence, checkpoint publication, and journal trimming. Deterministic failpoints make timing-sensitive failures reproducible. Network faults must include one-way loss, delay, duplication, connection reset, and reconnection; application-level reordering/gaps should also be injected even when the transport normally orders bytes.

The external harness records invocation, completion, result, volume, client identity, and ordered payload/checksum outside the failed cluster. Successful operations must be retained in the test oracle; timed-out operations are uncertain and may be present after recovery. A lost reply must not be misclassified as acknowledged success or definite rollback. Compare the recovered history against allowed outcomes, not merely replica equality: all replicas can agree on a wrong result.

For every test, assert safety while faults are active and eventual progress after prerequisites return. Compare data and metadata at a common committed checkpoint, verify journal gaps/hashes, and test access as multiple UIDs/GIDs. Track which former owner can still serve through old mounts. Capture reproducible seeds, fault times, epochs/configurations, checkpoint positions, logs, guest/kernel versions, and recovery decisions.

## Integration test matrix

The 36 rows below are native filesystem scenario families. The five Kubernetes-specific K01–K05 cases live in [Deployment compatibility tests](Deployment-tests.md), outside Stage 1. Each row is a scenario family. Repeat relevant cases with every test VM in each role and with A/B/C volumes active on different primaries. “Automatic” assumes the named sources, authority, and fencing remain available.

| ID | Fault or action | Required result |
|---|---|---|
| R01 | Kill primary after local sync and partner receipt, before partner disk persistence | Recover from the surviving partner's volatile tail, persist it during takeover, and preserve acknowledged operations |
| R02 | Kill primary before local sync; after only one branch completes; after reply is lost | Never report premature success; resolve uncertain operations consistently without duplicate append or rename |
| R03 | Restart synchronous partner, clearing its RAM, while owner stays healthy | Partner rejoins recovering; owner backfills missing state before treating it as ready; no false progress from old incarnation |
| R04 | Select lagging async replica as new primary | All destination workload I/O remains blocked until authoritative history is fetched/applied and a ready partner is established |
| R05 | Disconnect async replica through sustained writes and an owner change | New owner retains delivery obligations; every required change arrives after reconnect or valid baseline recovery |
| R06 | Fail one test VM while three volumes have different primaries | Handle lost primary, lost sync, and lost async roles independently; avoid unrelated global stalls |
| R07 | Return former primary with a divergent uncommitted suffix | No automatic failback or stale serving; preserve/reconcile suffix and catch up from current authority |
| R08 | Owner disk lost plus partner RAM lost before persistence | Detect missing/uncertain history and block; explicit loss acceptance only, never silently promote stale async state |
| R09 | Abruptly reboot all nodes; vary restart order and delay original owner | Reconstruct valid durable history; do not promote merely by maximum sequence or first two nodes online |
| R10 | Crash each participant during every takeover step | Resume or reconcile idempotently; no double activation, lost history, or permanent restart loop |
| F01 | Partition old primary from peer pair but keep its local test process alive | Replacement activates only with fencing; old process cannot successfully read stale data or acknowledge writes after transfer |
| F02 | Pause daemon or test VM during attempted authority revocation; resume with old messages | Old epoch/session cannot revive access after completed transfer; include preexisting descriptors and cached mmap reads. Under the approved explicit native VM fence policy, a paused guest that cannot stop leaves the replacement gated; elapsed etcd TTL/pause time is never fence proof. |
| F03 | Partition etcd traffic only, then replication traffic only | Independently enforce authority and data readiness; no assumption that one implies the other |
| F04 | Lose etcd leader, then quorum; recover network | Leader loss recovers; detected authority uncertainty gates daemon admissions/completions and requests the explicit fence. Record detection and positive cache-fence completion separately; no hard cached-read expiry is promised when management is unavailable. Promotion stays blocked until fencing and safe authority/history revalidation complete. |
| F05 | Race promotion, partner replacement, workload relocation, and fourth-node admission | One valid configuration/history; no two arbitrary replicas forming an unauthorized write pair |
| D01 | Inject short write, ENOSPC, EIO, journal sync failure, or stalled ZFS pool | No false success; quarantine/fail affected path or recover from peers; preserve diagnostics and bounded queues |
| D02 | Corrupt/torn journal tail versus corruption inside a previously valid prefix | Detect both; only discard a tail when proven safe, otherwise repair from verified history or stop |
| D03 | Crash between checkpoint creation, publication, and log trimming | At least one complete valid baseline plus needed suffix remains; orphan checkpoints can be reconciled |
| D04 | Receiver behind retention; interrupt snapshot install repeatedly | Resume/restart staging safely; never expose partial snapshot; catch up its suffix before serving |
| D05 | Slow async disk, fill memory queue, exhaust retention reserve | Backpressure/retention policy is enforced; no dropped updates or unbounded RAM growth |
| P01 | Concurrent append, exclusive create, rename replacement, hard links, unlink-open, truncate | Preserve defined syscall semantics and handle identity through replay and failover |
| P02 | Multiple users/groups, umask, chmod/chown, sticky directory; cache permission results | Correct authorization through the FreeBSD kernel and local FUSE mount; no daemon-credential bypass |
| P03 | fcntl/flock contention, process exit, duplicate local mounts, separate test VMs | Correct supported lock domain and exclusive serving; no fake lock success for conflicting permitted clients |
| P04 | Buffered I/O, synchronous opens, fsync/fdatasync, directory sync, mmap/msync under crash | Defined persistence/ordering holds at each supported boundary; required mmap behavior works with chosen caches |
| P05 | Snapshot restore or promotion while old descriptors, negative dentries, and mmap exist | Stale handles/caches are invalidated, fenced, or fail explicitly; never return a different object under an old identity |
| A01 | SQLite writes/checkpoints and PostgreSQL transactions during injected failures | Application recovery and integrity checks pass; acknowledged durable transactions retained except declared loss mode |
| A02 | Native file-service clients and file-access patterns representative of the f3s workloads | Workload-specific integrity and access checks pass; unsupported shared access is rejected and reported as a compatibility limitation |
| E01 | Activate emergency mode without proven exclusion of old members | Refuse activation; losing quorum alone never enables local-only writes |
| E02 | Valid emergency activation, survivor reboot, other peers boot old state | Mode/generation persists; old peers cannot join or serve; survivor replays its durable emergency writes |
| E03 | Manual admit one peer, interrupt replay and emergency exit at every step | Peer remains recovery-only until ready; explicit exit restores synchronous ACKs before writes resume |
| E04 | Emergency log fills or survivor loses its disk | Retention policy blocks/archives before gaps; disk loss requires external recovery, never fictitious redundancy |
| S01 | Restore one old replica snapshot versus intentionally roll back whole volume | First catches up to current state; second establishes a new history without replaying unwanted later mutations |
| S02 | Restore etcd snapshot older than storage epochs; old members still exist | Reconcile recovery generation and invalidate stale clients; never reuse obsolete authority |
| M01 | Add fourth node and move a replica; crash during catch-up/configuration switch | Old configuration remains safe until valid transition; no authority for incomplete replacement |
| M02 | Known normal member rejoins versus emergency-barred member | Normal recovery is automatic; emergency admission remains explicitly manual |
| M03 | Rolling software upgrade, unsupported journal/wire format, wrong volume UUID | Compatible members recover; unsupported/foreign state fails clearly without rewriting or discarding it |

## Validation before automatic failover

Specify and test invariants: one active write history; no stale successful read; no acknowledged write loss outside the declared durability gap; replay preserving required ordering and atomicity. Exercise transitions with crashes, pauses, delayed/reordered/duplicated messages, lost replies, full disks, expired read permissions, and loss of volatile buffers. Check histories against the intended consistency model and run application recovery tests. Measure owner read latency, write latency, asynchronous lag, catch-up rate, and takeover time including partner readiness. This architecture is not yet a proof of those properties.

## Success criteria and rollout gates

1. **Native FUSE gate:** direct mounts inside the FreeBSD test VMs pass file, permission, lock, mmap, sync, and fencing tests. The VM harness proves that test I/O traverses HomelabFS, not a missing mountpoint or raw backing directory.
2. **Protocol gate:** deterministic failure schedules satisfy single-history, correct acknowledgement, replay, and fencing invariants; include crashes at all commit boundaries. A quorum-intersection argument alone is not a tested commit protocol.
3. **Recovery gate:** normal single-node failures, reconnects, and known-member rejoin complete without manual edits when authority and recoverable history remain. Emergency mode and ambiguous-history tests deliberately stop for operator decisions.
4. **Application gate:** native SQLite/PostgreSQL and filesystem clients survive failures through the local FreeBSD FUSE mount. Linux-only applications and Kubernetes lifecycle tests belong to the separate deployment suite; representative access patterns do not certify those applications.
5. **Operations gate:** snapshot restoration, emergency exit, restart dependency order, and monitoring pass in the disposable cluster. Evidence is collected, test data cleaned, test VMs shut down, and any temporarily stopped Rocky VM restored. Deploy validated artifacts for isolated canary volumes, then run separately approved Stage 2 smoke tests. Report isolated deployment/physical exercises separately, including not-run cases. Completion produces canary evidence, not authorization for production migration or NFS retirement.

Choose measurable timeouts and targets after initial measurements. Record detection, fencing, replay, partner readiness, volume publication, and application recovery separately. A long async backlog makes failover slower; time-to-ready must be judged against available catch-up bandwidth, not a fixed promise independent of backlog. Random fault runs supplement deterministic tests and should preserve seeds and minimized failing histories. Run deterministic protocol tests frequently, real-kernel integration in CI, and native FreeBSD crash/recovery suites at release gates.

The [approved takeover design](spec/Takeover.md) uses positive explicit fencing rather
than a clock-based serving lease. F02/F04 therefore require blocked promotion
when a paused/hung guest or unavailable management prevents confirmation. These
are safety outcomes, not successful failover or proof of a hard cache-expiry
deadline. Implementation and `lv3` evidence remain prerequisites; profiles that
require bounded cached-read shutdown despite management failure are unsupported.

## Security coverage

The additional [security integration tests](Security.md#security-integration-tests) cover network identity, admission, revocation, emergency exclusion, API privileges, resource bounds, and encryption-key recovery. They supplement the 36 native failure and filesystem scenario families above.

## Concurrency and journal housekeeping coverage

Additional [performance scenarios](Performance.md#measurement-and-tests) exercise connection scaling, mixed workloads, overload, and fairness. [Journal housekeeping scenarios](Journal-storage.md#integration-tests) cover checkpoint/cleanup crashes, offline replicas, quotas, snapshot-held space, and independent snapshot restores. These supplement the 36 native scenario families.

## Regression coverage from f3s experience

The [motivation document](Motivation-and-goals.md#lessons-from-the-f3s-blog-series) connects these requirements to the existing cluster's storage history:

- **D01/P04:** leave a FUSE mount present and metadata readable while writes or sync calls stall. The direct filesystem probe must detect failure within its deadline, distinguish recovery gating, and avoid overlapping repair loops.
- **R10/P05:** interrupt takeover during active writes and reopen data through the new owner's native FUSE mount. Validate pool health, application integrity, and stale-handle behavior before releasing access.
- **A02:** exercise representative metrics/log/file-service access patterns with native clients. Verify persisted state, not just process readiness. Actual Prometheus/Grafana/Loki/Tempo deployment checks remain separate.

Kubernetes publication, pod lifecycle, and 9p restart regressions are tracked in the [deployment suite](Deployment-tests.md), not run by the Stage 1 harness.


For local development, `internal/fuseadapter` now has an explicitly admitted
controlled buffered/multiprocess disposable fixture for a subset of P01–P04.
Its [contract and focused command](spec/FUSE-adapter.md#controlled-buffered-validation-xw3)
cover real Linux mmap/msync/fsync plus fresh replay, same-connection cache
coherence and separate-process lock/interruption/close lifetime. The root-only
synthetic multiuser case is separate and skips when unprivileged. This does not
complete P01–P04: native FreeBSD, multiple guests, failover, duplicate independent
mounts, arbitrary mutable credentials and normal cached publication remain
unverified or deliberately unsupported. The portable JSON/JUnit harness is `vq3`,
not supplied by this fixture. The implemented [portable behavior runner and
owned two-profile fixture](Portable-behavior.md) now provide explicit JSON/JUnit
local Linux evidence for these single-node syscall checks. Direct and buffered
reports must be assessed together; native FreeBSD and full matrix acceptance
remain separate gates.
