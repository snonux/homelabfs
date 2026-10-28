# HomelabFS failure handling and recovery

[Architecture overview](Architecture.md)

Ordinary failures should recover automatically when valid authority and sufficient history are available. Manual decisions are reserved for the exceptional cases documented below.

The [takeover state-machine specification](spec/Takeover.md) supplies durable
closure-wide steps, accepted-tail selection and serving predicates. Paul approved
its native whole-VM cache-fence policy; implementation evidence remains required,
and host/Rocky/9p fence profiles remain unsupported. No automatic takeover
has been implemented or validated by that specification.

## Failure handling and recovery

These are proposed operating rules. Prefer stopping affected I/O over creating conflicting history or reporting stale data as current. Loss under the accepted volatile-replica failure window must be explicit, not disguised as successful catch-up.

Routine failures should recover automatically: pod/guest/daemon restart, one failed host, replication reconnect, and a recognized replica returning in normal mode. This requires enough recoverable data, valid authority, and a working fencing mechanism. Manual intervention is acceptable for emergency activation/admission/exit, ambiguous or missing history, intentional rollback, permanent coordination loss, or inability to fence an old owner safely. Temporary waiting for peers is not itself a permanent manual-recovery state.

The companion [integration-test plan](Integration-tests.md) defines fault injection, expected outcomes, and release gates. These are planned tests, not completed validation.

## Recovery policy

| Situation | Default response | Operator involvement |
|---|---|---|
| Pod or Rocky guest restarts; storage is healthy | Reconnect or republish after checking volume identity and ownership; preserve normal ACK rules | None if the validated mount recovery works |
| One host, daemon, or replica fails | Fence as needed, recover the authoritative prefix, configure a ready synchronous pair, resume | None if recovery and fencing prerequisites hold |
| A known replica rejoins in normal mode | Admit as recovering, replay or install a validated checkpoint, remain a follower | None for recognized identity and unambiguous history |
| Replication connection fails temporarily | Reconnect and replay from verified progress with backoff | None |
| One volume stalls or needs repair | Gate that volume; keep unrelated groups running where shared resources permit | Alert; escalate only if repair cannot proceed safely |
| No serving quorum or no ready partner | Block writes; automatically reassess when peers return | Emergency activation is manual; never downgrade automatically |
| Required history missing or ambiguous | Preserve evidence and stop affected volume | Choose source, wait for a missing node, or explicitly accept loss |
| Old owner cannot be fenced, including guest caches | Do not activate a conflicting owner | Repair fencing or explicitly isolate/shut down the old environment |
| One replica corrupt, authoritative source verified | Quarantine and rebuild only that replica under policy | Escalate if authority or remaining redundancy is uncertain |
| Wrong node identity, foreign volume, unsupported format, missing ZFS key | Refuse serving and report the precise cause | Correct identity/configuration/key/software; retry automatically afterward |
| Emergency activation, peer admission, or emergency exit | Execute explicit, durable recovery workflow | Always manual authorization |
| Intentional snapshot rollback or permanent loss of etcd quorum | Reconstruct authority and data through a controlled recovery generation | Manual source selection/restore, then automated catch-up |

Temporary unavailability is not a reason to require permanent operator approval. Conversely, a timer expiring is not permission to choose a stale copy. Routine recovery must not require an operator to edit journal offsets, force a mount, delete ownership keys, or mark a replica clean.

## Recovery controller and serving gates

Implement a per-volume recovery controller with resumable steps. Persist operation identity, recovery generation, epoch/configuration, chosen history, progress, and activation prerequisites. A controller restart must resume or safely reconcile the same attempt rather than create another competing promotion. Use one serialized recovery decision per volume, bounded retries/backoff, and no automatic failback when the old primary returns.

Keep three distinct conditions: the daemon is alive; a replica can receive recovery data; the volume is allowed to serve workload I/O. CSI publication and workload readiness use the third. A long catch-up should not trigger liveness restarts that repeatedly discard progress or volatile data. Expose the reason for waiting, source replica, last progress time, bytes remaining, and whether operator action is needed.

Before activation, verify current authority, old-owner fencing, a valid recovered prefix, local application, and synchronous-partner readiness. On loss of authority, block new I/O and enforce fencing across the entire guest path. Storage quorum protects new write acceptance; it does not invalidate cached Linux reads. Validate a mechanism that stops or revokes the old guest/workload and its cached access, including already-open descriptors and mmap. Choose the concrete fence and obtain Paul's decision on acceptable VM/host disruption during the takeover specification, before model checking and implementation; CSI consumes that decision. Define independently reachable management, positive completion proof and fail-closed behavior as deployment prerequisites. Do not promise transparent cache revocation that virtio-9p does not provide.

Do not conflate client timeout, cancellation, and operation outcome. Once a mutation is accepted, failure to deliver its reply may leave an uncertain client result. Internal retries use stable operation identities; applications do not receive an invented exactly-once guarantee across process restarts.

Boot in dependency order: local ZFS available and unlocked; durable identity/emergency policy loaded; authority reconciled; HomelabFS mount verified; bhyve share connected to that mount; guest/CSI publication; workload start. Guest root disks and coordination state must not require a ready workload volume. Verify mount and volume identity, not merely an existing directory. Never let a missing FUSE mount or failed 9p mount expose a writable empty directory to a pod.

## Coordinated shutdown and cold start

Implement and test the [isolated power lifecycle contract](spec/Power-lifecycle.md),
subject to [deployment readiness](Deployment-readiness.md#fencing-and-coordinated-power-lifecycle).
Quiesce clients with quorum available, durably drain accepted work, close access,
then stop storage/coordination and enrolled guests using the approved graceful
native VM fence. Physical-host power remains outside that policy. Test bounded
interrupted-step recovery and cold boot without dependency cycles. Never stop
the only surviving volatile accepted tail before safe persistence/transfer or
downgrade automatically to emergency writes. Mounted-but-gated differs from serving.

Existing f3sctl/NFS/CARP/monitor/UPS automation remains unchanged. Its parallel
non-master shutdown is a future integration concern, not a mandate to change
production power management. Live shared-host canaries do not authorize host
shutdown or fencing production guests; exercise those behaviors in isolation.

## Automatic recovery requirements

Use a per-volume recovery controller with durable, resumable transition steps and one serialized recovery decision. Persist recovery identity and progress; retry idempotently after interruption. Separate daemon liveness, replica recovery readiness, and permission to publish a workload volume. Catch-up must not trigger repeated liveness kills that prevent progress. Expose wait reasons, authoritative source, lag, and operator-action status.

Boot and reconnect must verify the actual HomelabFS mount and volume identity before bhyve/CSI publication. An existing directory is not sufficient. Never allow a missing host FUSE mount or guest 9p mount to turn into a writable empty-directory fallback. Determine and automate whether a dead FUSE session requires a guest remount, pod restart, or VM restart; do not assume bhyve's retained file handles reconnect to a new mount.

Fencing must cover the Linux guest's existing descriptors and cached/mapped pages, not only new requests to the host daemon. If the sharing transport cannot revoke that access, automatic safe failover needs an effective guest/host fencing mechanism. Until that is proven, inability to fence blocks promotion. Kubernetes API deletion alone is not fencing.

## Minimum available replicas per workload

| Available configured replicas | Operating state |
|---|---|
| Three | Normal: primary, synchronous replica, and asynchronous replica per workload directory. |
| Two | Degraded but operational after recovery/configuration: primary and synchronous replica per directory. Retain catch-up obligations for the offline third node. |
| One | Normal operation unavailable; block or fail writes. Explicitly activated emergency mode permits durable local-only writes on the designated node. |
| Zero | Unavailable. |

Available means configured for the affected replication group, mutually reachable and ready for the volume, with sufficient recovered history and valid coordination authority. Two powered-on hosts alone are not sufficient. In normal mode, after a second node returns, recover and fence the replication group and establish the synchronous pair before resuming write acknowledgements. Emergency mode instead requires the explicit admission and exit procedure in [Emergency-mode.md](Emergency-mode.md). An isolated survivor cannot simply assume normal serving authority.

See [Emergency-mode.md](Emergency-mode.md) for the explicit single-node exception and manual rejoin procedure.

## Recovery state

Each replication group needs a durable identity, configuration identifier, highest promised epoch, replay checkpoints, and checksummed journal. Track operation acceptance, commit knowledge, application, and durability separately. Include node incarnation identifiers so messages from an earlier daemon lifetime cannot restore old authority.

The synchronous partner must retain complete recoverable operations until safely persisted or transferred. It must not discard memory buffers merely because the owner acknowledged the client. A restarted node rejoins as recovering; a durable epoch does not prove that it retains all previously acknowledged operations.

Use stable request identifiers for protocol retries and record enough results to avoid duplicate append, create, or rename effects. A timed-out operation may have committed. Arbitrary application retries after process restart cannot be made exactly-once without an application identity protocol.

## Failure scenarios

| Failure | Required behavior and recovery |
|---|---|
| Asynchronous replica stops | Continue with owner and synchronous partner. Preserve its catch-up obligation and replay the missing suffix or resynchronize from a checkpoint on return; it remains passive. |
| Synchronous partner stops | Stop new write acknowledgements under the old configuration. Recover the authoritative prefix, catch up the third node, and activate it as partner through a fenced configuration change. Never silently switch to local-only acknowledgement. |
| Owner crashes, partner remains alive | Preserve and persist the partner's volatile tail before completing takeover. Reconcile commit knowledge and uncertain operations, fence the old owner, resolve reader permissions, and establish a second ready replica before acknowledging new writes. |
| Owner isolated from the other two | Its writes cannot obtain a second storage acknowledgement. Stop reads when serving authority is no longer valid. The other side may take over only after fencing, read-permission resolution, and data recovery. |
| Node reaches etcd but no storage partner | No acknowledged writes. Coordination availability does not substitute for a second data copy. |
| Two nodes available but recent required data absent | Block automatic promotion. Wait for an authoritative replica or explicitly choose recovery with data loss. A metadata quorum does not prove file contents survived. |
| Owner lost and partner restarts before persisting | This is the accepted durability gap. Attempt recovery from the owner's disk, third replica, or backups. If acknowledged operations are missing, require an explicit recovery decision and establish one new authoritative history. |
| All nodes reboot or lose power | Replay and compare durable logs and epochs before activating an owner. The local durable-write assumption should preserve acknowledged operations somewhere if disks survived. Do not select solely by largest sequence number; reconcile history and commit evidence across ownership changes. |
| Daemon crashes but host stays up | Never fall back to raw backing files. Start in RECOVERING, reconcile journal and peers, and invalidate the old session. Guest workloads may need remount/restart depending on the transport. |
| Pause or asymmetric partition | Revalidate serving rights on resume. Delayed messages carry epoch/configuration/incarnation and cannot revive obsolete rights. |
| Acknowledgement lost or duplicated | Resolve by operation identity and protocol state. Never replay a non-idempotent operation twice. A timeout is an unknown result, not proof of rollback. |
| Crash during rename, unlink, or apply | Use stable object identities and replay markers. Do not replay a path operation against an unrelated replacement file. Stage or gate reads during application of atomic operations. |
| Local journal sync fails or disk fills | Do not acknowledge success. Preserve ambiguous operations for reconciliation and return appropriate errors. Reserve recovery metadata space and bound outstanding work. |
| Partner memory queue or disk stalls | Apply backpressure before acknowledging data that cannot be retained. Replace the partner only through the recovery/configuration protocol. |
| etcd quorum unavailable | No new ownership or membership decisions. Conservatively stop affected serving rights when authority cannot be renewed. Resume through revalidation; lease expiry does not erase pending work. |
| Old snapshot or replacement disk joins | Treat as recovering. Verify group identity, install a trusted checkpoint, replay, and acquire current authority. |
| Journal gap exceeds retention | Install a snapshot tied to a committed checkpoint and replay its suffix before serving. Preserve the current copy until replacement is verified. |
| Corruption or conflicting histories | Quarantine the affected history and reconcile against verified peers or backups. Never select by wall-clock timestamp or automatically merge conflicting versions. |

## Takeover sequence

1. Stop admissions to the transitioning group and obtain authority to propose a new epoch/configuration.
2. Persist fencing promises on a storage majority, gathering prior journal state before enabling new writes. Preserve old logs and volatile tails.
3. Revoke or safely expire prior read permissions, including access through caches.
4. Recover a consistent authoritative prefix. An operation whose reply was lost may be completed. Preserve acknowledged operations whenever their required copies survive; if evidence is insufficient, remain unavailable rather than inventing a commit boundary.
5. Persist the complete recovered history and dependencies durably on both the prospective owner and its new partner before activation, as required by the takeover specification. Extra durability during recovery does not change ordinary in-memory partner ACKs.
6. Apply the prefix, restore metadata, and activate the configuration. Grant serving rights and resume workloads only after activation conditions hold.
7. Catch up the remaining replica by replay or snapshot. Preserve diagnostic/recovery copies before discarding divergent suffixes.

Explicit data-loss recovery must fence or isolate former members, preserve old copies, and record a new recovery generation. If etcd is restored from an old snapshot, do not assume its revisions remain ahead of stored fencing tokens; reconcile storage epochs and rebootstrap authority before serving.

## Manual recovery evidence

When automation stops, report affected volumes, last known valid generation/configuration, surviving checkpoints, missing or uncertain operations, fencing status, and usable snapshots. Present explicit recovery choices and their expected data loss. Preserve evidence before changing history; journal repair and forced ownership must never be generic “try this” operations.

Restoring etcd is a separate disaster-recovery operation: its documentation requires a new logical cluster and discusses revision bumps and watch invalidation. HomelabFS must still reconcile its own storage epochs and generation. Kubernetes identity and persistent-volume claims do not substitute for application-process fencing. References: [etcd disaster recovery](https://etcd.io/docs/v3.6/op-guide/recovery/), [Kubernetes StatefulSets](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/).
