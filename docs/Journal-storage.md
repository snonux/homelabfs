# HomelabFS journal storage and housekeeping

[Architecture overview](Architecture.md)

Status: all-origin retention contract `gz3`, 2026-10-31; distributed integration
is unimplemented. The [normal protocol](spec/Replication-protocol.md) uses durable
origin streams plus an exact ordered slot/source graph. A remote origin W outside
replica placement can hold the only durable new COMMIT/result fragment while A
serves the complete committed RAM view before local persistence/apply. W's journal
is an obligated recovery source. Owner-origin completion uses distinct B memory;
B is not an extra synchronous host on the W+A remote path. Complete durable
history can span many origins. Neither an owner-disk maximum nor one stream's
prefix is a closure checkpoint.

Recommended layout: separate sibling ZFS datasets for each workload's data and journal under a common parent. HomelabFS owns journal checkpointing and segment deletion; external FreeBSD tools own ZFS snapshot scheduling and retention. This is a design recommendation, not deployed configuration.

The [journal format](spec/Journal-format.md) defines portable record bytes, redo and recovery evidence, segment seals and corruption handling. It specifies representation; commit, fencing and checkpoint eligibility still require their protocol and platform gates.

## Dataset layout

```text
zdata/enc/homelabfs/
  volumes/<volume-uuid>/
    data       # backing files, namespace metadata, durable apply checkpoints
    journal    # log segments, commit/recovery evidence, retention metadata
  control      # node identity, admission/emergency policy, cluster recovery generation
```

These are ZFS dataset names, not guest-exported paths. HomelabFS publishes the logical volume through its FUSE mount; the journal and control datasets are never exposed to workloads. Keep the control dataset outside per-volume rollback scope. Its backup/restore must reconcile authority with the cluster rather than restore an old node identity into service blindly.

Separate data/journal datasets allow different quotas, compression/recordsize experiments, space accounting, and snapshot retention. They do not provide independent disks, pool-failure isolation, or automatic performance improvement. A single dataset would simplify snapshot pairing, but reduce those administrative controls. Start with the sibling layout and an explicit paired recovery contract.

Keep encryption coverage for both siblings. Preserve synchronous-write semantics on both data and journal; begin with normal sync behavior and explicit durability operations. Recordsize/compression/quota values should be benchmarked rather than guessed. Leave capacity for checkpoints and recovery; a journal quota or a shared pool filled by snapshots can still block writes. Sources: [OpenZFS dataset properties](https://openzfs.github.io/openzfs-docs/man/master/7/zfsprops.7.html).

## Segment and checkpoint lifecycle

Use bounded append-only segments per origin stream, identified by the full volume/generation/configuration/epoch/origin/incarnation identity and local sequence range. Global selected slots map to these exact source positions; they do not rename them. Rotate by size, with age as an optional operational trigger. Seal segments with validated framing/checksums. Segment size and retention windows remain configurable choices to measure.

The journal serves both local crash recovery and remote catch-up. A successful memory ACK is not permission to delete its data. Neither file age, an empty send queue, nor application to files without a durable checkpoint proves the segment is unnecessary.

The [ordered closure checkpoint contract](spec/Checkpoints.md#ordered-closure-checkpoint)
requires:

1. Capture one selected slot cut with its exact predecessor chain, per-volume
   cuts, slot-to-origin mapping and complete transitive dependency graph. Retain
   all issued/unresolved grants and results; a selected maximum is not the issued
   frontier. Gate source, apply, authority/closure and checkpoint transitions.
2. Durably establish the corresponding baseline, namespace, payload bases and
   exact immutable decision/claim/result/ID, home/handle and transaction ledgers.
   A committed RAM overlay must be persisted or backed by verified pinned origin
   fragments; backing apply or a memory receipt cannot cover it.
3. Publish authenticated immutable manifest/baseline/source artifacts and exact
   per-stream suffix boundaries with ordered file/directory syncs. Bootstrap and
   every restart validate baseline plus source/suffix graph behind recovery gates.
4. Obtain fresh complete retention coverage and persist protected source releases
   before advancing any reclaimable boundary. Preserve external snapshot/backup,
   transfer/recovery and offline-replica obligations across authority changes.

The bounded local implementation and exact publication/recovery mechanics are
documented in [Immutable checkpoints](spec/Checkpoints.md). It preserves an
independent payload baseline and every original prefix frame, requires explicit
trusted authority and retention leases, and reclaims closed source segments only.
The existing filesystem service still requires genesis and must never have its
source trimmed. Replica retention, service integration and semantic-history
compaction remain future work. A checkpoint position must not be inferred merely
from the highest journal record or a file timestamp.

## When segments may be deleted

Delete only closed segments wholly below a verified safe boundary. Check all of these dependencies:

- Local recovery has a verified durable closure checkpoint plus exact required suffix. Immutable grants/claims/intent/results/IDs, allocation/retirement and rejection ledgers, homes/handles, transaction decisions/prepares and complete issued-frontier evidence survive in verified durable replacements; covering payload effects alone is insufficient.
- Every origin source/choice/grant obligation has verified complete durable
  replacement and protected release; an outside origin cannot be removed from
  the inventory merely because it is not an assigned replica.
- Every assigned replica that still relies on incremental replay has durable coverage of that prefix, through retained journal data or a recoverable applied checkpoint. Receipt in RAM is insufficient.
- No active catch-up stream, snapshot installation/capture, backup/point-in-time plan, promotion, emergency admission, checkpoint operation, home/handle or transaction dependency pins the segment. External holds require explicit acknowledged release.
- Retained point-in-time recovery plans have a baseline plus required suffix; log deletion does not silently break an advertised recovery point.

Serialize garbage collection with membership and checkpoint transitions. Treat a missing progress record as unknown, not caught up. Make deletions restart-safe and persist the directory/manifest changes required by the chosen protocol. If a crash leaves old segments behind, reclaim them later; premature deletion is unacceptable.

A conservative first policy retains the union of every origin/source and dependency pin, including those needed by the slowest assigned replica, plus a configurable safety window after durable release becomes eligible. Replica progress alone cannot release an outside origin or immutable claim/result. The window is extra protection, not the correctness condition. Compaction must not retain only the latest bytes if that destroys the order/evidence needed for filesystem replay or takeover.

## Long outages and bounded storage

Unlimited downtime, unlimited writes, finite storage, and literal replay of every historical update cannot all be guaranteed. Choose an explicit policy before hitting capacity:

- **Incremental retention:** keep or archive the missing history; throttle or stop writes if capacity cannot preserve it. This is required when every operation must remain replayable.
- **Checkpoint resynchronization:** establish a verified baseline, mark the peer as requiring that baseline, and retain its subsequent suffix. Rebuild from the snapshot/checkpoint on return. This preserves resulting state, not every historical intermediate operation.

Checkpoint resynchronization must be a defined policy transition, not silently skipping a gap. Emergency mode retains the history required for manually admitted peers unless an explicit baseline/archive policy replaces it. Persist that obligation across promotions and restarts.

Alert on oldest retained sequence/age, bytes awaiting each replica, reclaimable bytes, blocked cleanup reason, active transfer pins, dataset/pool free space, and snapshot-held space. Run bounded housekeeping in the background, and pause admission before space exhaustion prevents checkpoint or recovery metadata writes.

## Snapshots of separate datasets

Independent snapshot schedules are useful, but restore compatibility must be explicit. An external recursive snapshot of one local workload parent captures that host's data and journal siblings together, as one recovery component. OpenZFS documents recursive snapshots as occurring at the same time. This is a storage-level point-in-time capture, not automatically an application-consistent checkpoint. Source: [OpenZFS snapshots](https://openzfs.github.io/openzfs-docs/man/master/8/zfs-snapshot.8.html).

For a clean local HomelabFS recovery component, external tooling can request a checkpoint barrier, snapshot both siblings while the barrier is held, and then release it. Make that handshake bounded and restart-recoverable. Live recursive snapshots must instead be validated as crash-recoverable, including partial apply and pending journal operations. Application-aware quiescing is separate where needed.

In the all-origin target, one host's recursive sibling snapshot is not the
complete volume/closure recovery set. Bind the global barrier to every required
origin dataset/source cut (including journals outside placement), authority,
claim/result and dependency manifest, with pins across capture. Alternatively
durably materialize all obligations in a verified selected backup set before
capture. Owner RAM itself is not a backup component. Missing one source prevents
advertising a complete clean recovery point. Use the explicit
[complete recovery-set contract](Snapshots-and-restore.md#complete-all-origin-recovery-sets);
its runtime integration remains unimplemented.

Independent data-only or journal-only snapshots may be retained additionally. Do not pair them solely by timestamp: restore requires a matching history and a complete replay suffix. Restore tooling validates the checkpoint/manifest before serving; never roll back only one live sibling beneath the daemon.

Deleting journal files does not necessarily return space to the pool when ZFS snapshots or clones still reference their blocks. External journal-snapshot retention must therefore complement HomelabFS segment deletion. Do not let daemon housekeeping destroy administrator-managed snapshots. Snapshot pruning must respect any baseline currently pinned for restore or catch-up; define a hold/manifest handshake with external tooling.

## Integration tests

Crash before/after data sync, checkpoint publication, segment rotation, retention-boundary advancement, unlink, and directory sync. Reboot and verify a valid checkpoint plus suffix remains. Replay partial rename/unlink/truncate application without modifying a replacement object incorrectly.

Also test: an offline async replica blocking deletion; a memory-only ACK followed by partner restart; promotion while cleanup runs; fourth-node replacement and transfer pins; interrupted emergency rejoin; quota and pool exhaustion; space retained by snapshots; snapshot pruning during a pinned transfer; independently timed sibling snapshots; data-only rollback; and switching an offline peer to an approved baseline recovery policy. Compare recovered application data and metadata against the external acknowledgement oracle, not just replica equality.

## Canary capacity prerequisite

Apply the [canary resource budget](Deployment-readiness.md#capacity-and-resource-admission)
before deployment and each run. Include canary snapshots retaining journal
blocks, checkpoint staging, replica rebuild and cleanup/evidence reserves.
Existing production data, backups and received sinks consume protected capacity;
they are not candidates for deletion or migration. No duplicate production
cutover copies or full-NFS-tree sizing are required in this project.
