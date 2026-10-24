# HomelabFS integration test plan

[Architecture overview](Architecture.md)

Planned fault-injection and application tests. No HomelabFS implementation or fault tests have been run; the expected outcomes are acceptance requirements.

## Early compatibility gates

Before implementing extensive replication, validate the selected FreeBSD FUSE → bhyve virtio-9p → Rocky → pod path on the actual supported versions.

- Confirm guest 9p modules, protocol dialect, mount lifecycle, UID/GID handling, and application-visible errors.
- Verify write acknowledgement and sync propagation at the syscall boundary. A normal buffered write and a writable mmap store are different operations; define the promised persistence boundary for each. Kernel buffering must not silently contradict the chosen write contract.
- Verify locking in one guest/mount, across pods using that mount, through duplicate guest mounts, and between independently connected clients. Preserve correct local application locking; enforce exclusive volume serving against other clients.
- Verify cache behavior for data, metadata, permissions, negative lookups, and existing mmap pages through failover and daemon restart.
- Verify recovery of the 9p share after FUSE restart. A fresh host mount need not repair already-open bhyve file handles automatically. Determine whether remount, workload restart, or VM restart is required and automate the smallest safe action.

Two concrete findings make these gates essential. Linux's 9p documentation describes the no-cache mode as disabling mmap, while its mmap mode includes writeback behavior; disabling every cache is therefore not automatically a solution for database compatibility. The FreeBSD main-branch lib9p backend inspected during this review returns lock success without acquiring a host lock and reports regions unlocked. This does not establish the behavior of the deployed release or prove same-guest locking fails, since the guest kernel can also maintain locks. It does mean host locking cannot be assumed to protect independent clients. Sources: [Linux 9p cache modes](https://docs.kernel.org/filesystems/9p.html), [FreeBSD lib9p backend](https://github.com/freebsd/freebsd-src/blob/main/contrib/lib9p/backend/fs.c).

If a required property fails, stop migration and revise or repair the selected transport explicitly. Do not hide missing semantics behind a successful mount or filesystem benchmark.

## Test environment and evidence

Use disposable FreeBSD hosts/VMs with separate ZFS pools, three independently killable storage daemons, an independent coordinator for the test harness, and a separate three-member etcd deployment. Use real Rocky bhyve guests for end-to-end tests; nested virtualization or physical test hosts may be needed. Add an optional fourth storage node. Run the same core scenarios on the Linux FUSE frontend separately. Fault tests must not target live f3s data.

Control failures at each boundary: pod/process, guest, bhyve process, storage daemon, host, replication link, coordination link, and disk. `kill -9` tests process failure, not power loss. Include abrupt host/VM loss that removes volatile state; account for buffering in the outer hypervisor/storage when interpreting persistence results.

Instrument explicit failpoints around operation admission, journal append, local sync completion, partner receipt, partner ACK transmission, commit/visibility decisions, application, client reply, epoch persistence, checkpoint publication, and journal trimming. Deterministic failpoints make timing-sensitive failures reproducible. Network faults must include one-way loss, delay, duplication, connection reset, and reconnection; application-level reordering/gaps should also be injected even when the transport normally orders bytes.

The external harness records invocation, completion, result, volume, client identity, and ordered payload/checksum outside the failed cluster. Successful operations must be retained in the test oracle; timed-out operations are uncertain and may be present after recovery. A lost reply must not be misclassified as acknowledged success or definite rollback. Compare the recovered history against allowed outcomes, not merely replica equality: all replicas can agree on a wrong result.

For every test, assert safety while faults are active and eventual progress after prerequisites return. Compare data and metadata at a common committed checkpoint, verify journal gaps/hashes, and test access as multiple UIDs/GIDs. Track which former owner can still serve through old mounts. Capture reproducible seeds, fault times, epochs/configurations, checkpoint positions, logs, guest/kernel versions, and recovery decisions.

## Integration test matrix

Each row is a scenario family. Repeat relevant cases with every physical node in each role and with A/B/C volumes active on different primaries. “Automatic” assumes the named sources, authority, and fencing remain available.

| ID | Fault or action | Required result |
|---|---|---|
| R01 | Kill primary after local sync and partner receipt, before partner disk persistence | Recover from the surviving partner's volatile tail, persist it during takeover, and preserve acknowledged operations |
| R02 | Kill primary before local sync; after only one branch completes; after reply is lost | Never report premature success; resolve uncertain operations consistently without duplicate append or rename |
| R03 | Restart synchronous partner, clearing its RAM, while owner stays healthy | Partner rejoins recovering; owner backfills missing state before treating it as ready; no false progress from old incarnation |
| R04 | Select lagging async replica as new primary | All destination workload I/O remains blocked until authoritative history is fetched/applied and a ready partner is established |
| R05 | Disconnect async replica through sustained writes and an owner change | New owner retains delivery obligations; every required change arrives after reconnect or valid baseline recovery |
| R06 | Fail one physical node while three volumes have different primaries | Handle lost primary, lost sync, and lost async roles independently; avoid unrelated global stalls |
| R07 | Return former primary with a divergent uncommitted suffix | No automatic failback or stale serving; preserve/reconcile suffix and catch up from current authority |
| R08 | Owner disk lost plus partner RAM lost before persistence | Detect missing/uncertain history and block; explicit loss acceptance only, never silently promote stale async state |
| R09 | Abruptly reboot all nodes; vary restart order and delay original owner | Reconstruct valid durable history; do not promote merely by maximum sequence or first two nodes online |
| R10 | Crash each participant during every takeover step | Resume or reconcile idempotently; no double activation, lost history, or permanent restart loop |
| F01 | Partition old primary from peer pair but keep its guest alive | Replacement activates only with fencing; old guest cannot successfully read stale data or acknowledge writes after transfer |
| F02 | Pause daemon, guest, or host past authority expiry; resume with old messages | Old epoch/session cannot revive access; include preexisting descriptors and cached mmap reads |
| F03 | Partition etcd traffic only, then replication traffic only | Independently enforce authority and data readiness; no assumption that one implies the other |
| F04 | Lose etcd leader, then quorum; recover network | Leader loss recovers; quorum loss respects serving deadlines; safe revalidation resumes automatically after restoration |
| F05 | Race promotion, partner replacement, pod relocation, and fourth-node admission | One valid configuration/history; no two arbitrary replicas forming an unauthorized write pair |
| D01 | Inject short write, ENOSPC, EIO, journal sync failure, or stalled ZFS pool | No false success; quarantine/fail affected path or recover from peers; preserve diagnostics and bounded queues |
| D02 | Corrupt/torn journal tail versus corruption inside a previously valid prefix | Detect both; only discard a tail when proven safe, otherwise repair from verified history or stop |
| D03 | Crash between checkpoint creation, publication, and log trimming | At least one complete valid baseline plus needed suffix remains; orphan checkpoints can be reconciled |
| D04 | Receiver behind retention; interrupt snapshot install repeatedly | Resume/restart staging safely; never expose partial snapshot; catch up its suffix before serving |
| D05 | Slow async disk, fill memory queue, exhaust retention reserve | Backpressure/retention policy is enforced; no dropped updates or unbounded RAM growth |
| K01 | Crash only pod, Rocky guest, bhyve process, or FUSE daemon in separate runs | Use the appropriate restart/remount/recovery path; no unnecessary cluster-wide promotion |
| K02 | Delete/recreate pod while old process survives a partition | New volume publication waits for actual fencing; API deletion is not evidence of old process death |
| K03 | Repeat or interrupt CSI publish/unpublish, including stale requests after promotion | Requests are idempotent and scoped to volume/session/generation; stale unpublish cannot revoke a newer valid mount |
| K04 | Remove FUSE mount or guest 9p mount; leave ordinary directory underneath | No workload writes to an empty/raw fallback directory; readiness fails clearly |
| K05 | Pod uses multiple persistent volumes; destinations reach readiness at different times | Start only when all required mounts are ready; no deadlocked ownership transfer; no implied cross-volume transaction guarantee |
| P01 | Concurrent append, exclusive create, rename replacement, hard links, unlink-open, truncate | Preserve defined syscall semantics and handle identity through replay and failover |
| P02 | Multiple users/groups, umask, chmod/chown, sticky directory; cache permission results | Correct authorization on both kernels and full guest path; no daemon-credential bypass |
| P03 | fcntl/flock contention, process exit, duplicate mounts, separate guests | Correct supported lock domain and exclusive serving; no fake lock success for conflicting permitted clients |
| P04 | Buffered I/O, synchronous opens, fsync/fdatasync, directory sync, mmap/msync under crash | Defined persistence/ordering holds at each supported boundary; required mmap behavior works with chosen caches |
| P05 | Snapshot restore or promotion while old descriptors, negative dentries, and mmap exist | Stale handles/caches are invalidated, fenced, or fail explicitly; never return a different object under an old identity |
| A01 | SQLite writes/checkpoints and PostgreSQL transactions during injected failures | Application recovery and integrity checks pass; acknowledged durable transactions retained except declared loss mode |
| A02 | Actual Forgejo, Keybr, Immich/Miniflux DB, Syncthing, and shared media workload patterns | Workload-specific integrity and access checks pass; discovery of incompatible shared access blocks that migration |
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

1. **Transport gate:** the actual virtio-9p/FUSE stack passes required file, permission, lock, mmap, sync, and fencing tests. Resolve any guest-kernel support gap first.
2. **Protocol gate:** deterministic failure schedules satisfy single-history, correct acknowledgement, replay, and fencing invariants; include crashes at all commit boundaries. A quorum-intersection argument alone is not a tested commit protocol.
3. **Recovery gate:** normal single-node failures, reconnects, and known-member rejoin complete without manual edits when authority and recoverable history remain. Emergency mode and ambiguous-history tests deliberately stop for operator decisions.
4. **Application gate:** realistic database and file-service tests survive failover through the real Kubernetes mount path. Shared-volume exceptions are explicitly resolved.
5. **Operations gate:** snapshot restoration, emergency exit, restart dependency order, and monitoring are exercised before migration. Start with disposable data, then a low-risk workload with rollback, then broader migration; retire NFS only after validation.

Choose measurable timeouts and targets after initial measurements. Record detection, fencing, replay, partner readiness, volume publication, and application recovery separately. A long async backlog makes failover slower; time-to-ready must be judged against available catch-up bandwidth, not a fixed promise independent of backlog. Random fault runs supplement deterministic tests and should preserve seeds and minimized failing histories. Run deterministic protocol tests frequently, real-kernel integration in CI, and full stack crash/recovery suites at release gates.

## Security coverage

The additional [security integration tests](Security.md#security-integration-tests) cover network identity, admission, revocation, emergency exclusion, API privileges, resource bounds, and encryption-key recovery. They supplement the 41 failure and filesystem scenario families above.

## Concurrency and journal housekeeping coverage

Additional [performance scenarios](Performance.md#measurement-and-tests) exercise connection scaling, mixed workloads, overload, and fairness. [Journal housekeeping scenarios](Journal-storage.md#integration-tests) cover checkpoint/cleanup crashes, offline replicas, quotas, snapshot-held space, and independent snapshot restores. These supplement the 41 core scenario families.

## Regression coverage from f3s experience

The [motivation document](Motivation-and-goals.md#lessons-from-the-f3s-blog-series) connects these requirements to the existing cluster's storage history. Extend the existing scenario families:

- **K01/K04:** leave a mount present and metadata readable while writes or sync calls stall. The end-to-end health probe must detect the failure within its configured deadline, distinguish recovery gating, and avoid overlapping repair loops.
- **R10/P05:** interrupt takeover during active writes and reopen application data through the guest mount after recovery. Validate local pool health, application integrity, and stale-handle behavior before releasing workload access.
- **A02:** include Prometheus, Grafana, Loki, and Tempo state recovery in the workload inventory, using the deployed configurations. Verify stored data after restart and failover, not just pod readiness.
- **K03/K05:** apply volume and workload manifests in deployment order while storage is still catching up. Applications must wait for real volume readiness even when PV/PVC objects already exist.

These extend the existing tests; they do not assert that the proposed FUSE/9p path has already solved the old stack's recovery problems.
