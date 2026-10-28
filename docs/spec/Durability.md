# Durability and read freshness

Status: contract for task `aq3`; Paul confirmed the two acknowledgement decisions
on 2026-10-28. This specifies intended behavior, not behavior implemented by the
loopback PoC. Scope is disposable lab/canary volumes.

## Confirmed decisions

1. **Ordinary writes:** Paul selected option A, retaining the architecture's full
   acknowledgement rule for each mutation delivered to the daemon. Option B
   would permit local durable acknowledgements and defer required partner
   receipt to fsync. B changes the agreed normal-mode guarantee and would
   require corresponding architecture, replication, and recovery revisions.
2. **Stronger fsync:** Paul selected an initial scope of one durable local journal
   plus the synchronous partner's retained memory, like ordinary mutations.
   Alternatively add an explicit two-durable-copy mode, requiring durable
   ordered recovery evidence on the partner before a successful sync reply.
   The latter needs protocol, recovery, capability and failure-test work.

The contract below uses A and the initial single-durable-copy sync scope.
Option B and the two-durable-copy mode are out of scope. The remaining commit
and recovery protocol decisions still need their own specifications.

## Positions and acknowledgement boundary

For each volume and ownership epoch, distinguish received, locally durable,
committed and applied contiguous positions. Positions include the epoch/history
identity; a sequence number alone cannot identify recovered history.

Under A, successful mutation completion requires:

- valid exclusive serving authority throughout admission and completion;
- a complete replayable operation and required recovery evidence durably synced
  to the local journal, including its required ordered prefix;
- synchronous partner retention of that complete prefix in owned memory under
  the current session/configuration; and
- satisfaction of the future commit protocol's commit/visibility conditions.

Local sync and partner transmission run in parallel. Receipt is necessary but
does not itself define commitment. A read following successful completion must
observe the committed result through the barrier below. Deferred application is
permitted only behind that barrier. Async delivery remains mandatory and
retryable, outside this reply path. No automatic local-only downgrade is allowed
when the partner disappears; explicit emergency mode is a separate contract.

Durability means recovery from the journal, not necessarily immediate syncing
of the backing data file. Journal retention/checkpointing must preserve every
acknowledged effect until a durable recoverable checkpoint covers it. This
requires functioning storage sync semantics; `sync=disabled` is incompatible.

One durable copy plus retained partner memory does not survive loss of the
owner's storage and the partner's volatile tail. There is no finite loss-window
claim. An error, timeout or interrupted wait after admission does not prove the
operation was rolled back; retries need the commit protocol's identity and
reconciliation rules. Reads must never expose staged uncommitted backing state.

## Successful operation completion

This table applies at the native FUSE/daemon request boundary. A syscall may
produce several requests; short writes promise only the returned byte range.
Linux guest buffering is addressed separately below.

| Operation | Successful-return promise |
|---|---|
| Ordinary write without synchronous flags | Accepted byte range and metadata needed to recover it meet the full boundary above; backing-file apply can lag. |
| Write on O_SYNC open | Same boundary, with a barrier covering prior ordered writes on the file and all metadata needed for full file integrity; no second durable replica implied. |
| Write on O_DSYNC open | Same boundary for data and retrieval-critical metadata, including size/allocation changes. May conservatively perform the full O_SYNC barrier. |
| fsync(file) | All writes ordered before the barrier on this file, plus file metadata, meet the boundary and can be recovered. It does not imply parent-directory name durability or a transaction across files/volumes. |
| fdatasync(file) | As fsync for data and metadata necessary to retrieve it. A platform without a distinct primitive may use fsync. |
| fsync(directory) | Prior namespace changes ordered before this directory's barrier, and their required dependencies, meet the boundary. A filesystem that cannot supply this must reject the capability rather than report false success. |
| close / FUSE flush / release | No additional sync promise. Previously acknowledged mutations keep their guarantees; propagate available delayed errors through appropriate callbacks. Do not rely on release alone to report a late error to an application. |
| rename | Atomic committed namespace change, including replaced name and both parent dependencies, meets the boundary before reply. Ordinary POSIX cross-mount/cross-volume rename fails explicitly (EXDEV); the separate application transaction API is specified below. |
| unlink / rmdir / create / link | Committed namespace change meets the boundary before reply. Open-unlinked handles retain object identity/lifetime until release; replay must preserve the chosen semantics. |
| truncate / chmod / chown / timestamps and other supported metadata mutations | Their committed effects and dependencies meet the same boundary; no separate weaker metadata path. |

A conservative initial implementation can use a per-volume sync barrier rather
than minimize per-file dependencies. It must define concurrent admission order:
the barrier includes requests ordered before it, not unsynchronized future writes.
Sync success need not wait for every backing-file apply if replay is sufficient;
subsequent reads and metadata queries still wait for the required applied state.
Ordinary namespace replies are deliberately stronger than local-filesystem
conventions that require a later directory fsync.

The explicit [cross-volume transaction API](Transactions.md) preserves separate
FUSE mounts while adding atomic rename/link across volume groups. Success requires
every participant's complete redo/dependencies, the coordinator decision/result
and participant installation evidence to meet the same local-durable plus partner
retained-memory boundary, followed by one shared visibility barrier. It does not
require a second durable copy. Foreign object homes create placement, recovery,
barrier and retention dependencies; ordinary operations touching such dependencies
must honor them too. A file/directory fsync is not an implicit application
transaction or a cross-volume snapshot. Missing evidence after the accepted
owner-storage/partner-memory loss window gates the whole affected closure rather
than exposing a partial transaction. The transaction spec defines retries,
uncertain results, decision retention and the prerequisite model/platform checks;
no implementation or runtime evidence is implied.

## mmap and guest buffering

A writable mmap store dirties a page; it is not a daemon request and has no
per-store persistence or partner-receipt promise. MS_ASYNC schedules work without
a durability promise. Successful MS_SYNC must deliver the requested dirty range
and wait for a covering durability barrier, or the platform/profile must be
rejected. A later fsync must also cover the file's preceding dirty mapped data
under the verified platform contract. Concurrent unsynchronized stores require
application synchronization to establish which bytes the barrier covers.
munmap and process exit are not substitute sync operations. Syncing file data
alone does not establish durability of a newly created parent name.

Start native FUSE with writeback cache disabled and explicit zero entry,
attribute and negative-entry timeouts. Use buffered reads (`direct_io` off) only
with verified data invalidation/update behavior for local mutations and recovery;
zero metadata timeouts do not disable the page cache. Never set keep-cache across
recovery generations. Gate publication until the adapter can enforce freshness
for cached reads and mmap. If that is unavailable, a restricted direct-I/O profile
may be offered only with mmap explicitly rejected; it is not a replacement for
the required mmap-compatible canary profile. Adapter/library selection and actual
kernel tests determine supported flags, not this document.

Rocky's 9p `cache=mmap` adds guest writeback even when host FUSE writeback is
disabled. An ordinary guest write can return before reaching HomelabFS: proposed
A cannot be claimed at every guest write syscall on that evidence alone. The
deployment gate must trace ordinary writes, synchronous opens, msync, fsync,
fdatasync and directory fsync across the actual guest, bhyve and host stack.
Admit a buffered guest profile only with an explicitly documented guest-visible
sync boundary; if per-write A is required at that syscall boundary, failure to
propagate it blocks that profile. No-cache 9p disabling mmap does not solve the
full compatibility requirement. Historical SQLite/mmap success establishes
neither power-loss persistence nor cross-client coherence.

## Read freshness and authority

At admission, each read, lookup, readdir, getattr or access check captures the
committed dependencies it must observe under the current serving generation.
Wait until the local applied prefix covers those dependencies, then access the
backing object under synchronization that excludes conflicting partial apply.
A conservative implementation waits for the volume's committed prefix captured
at admission. It may return a state including later committed operations but
must preserve namespace/metadata atomicity and never expose uncommitted effects.
Same-client reads after an acknowledged mutation must cover that mutation.
Permission checks must use equally fresh metadata and caller credentials.

Check serving authority before access and before completion; loss of authority
blocks/rejects work. Passive replicas do not serve workload reads. During takeover
recover the authoritative prefix, durably preserve required history, apply it,
establish authority and synchronous-partner readiness before opening the I/O gate.
No stale prefix can be declared fresh just because the old owner's disk is local.

Daemon barriers cannot intercept reads satisfied from existing kernel or guest
cache pages. Ownership transfer therefore also requires fencing/revoking the old
guest access path, descriptors and mappings, and invalidating or replacing caches
before admitting the new generation. A lease timeout or zero attribute TTL alone
does not accomplish this. Required remount/workload restart behavior is decided
by the deployment and fencing gates, never assumed transparent.

## Performance consequence and validation

With A, an unbatched sequential daemon write waits at least
`max(local journal sync, partner receipt) + commit/visibility work`. B would remove
the partner wait from ordinary replies but still pay local sync; fsync would wait
for the partner backlog. B trades lower possible ordinary-write latency for a
weaker failover guarantee. Group commit and bounded pipelining can amortize sync
cost under either option without acknowledging past a missing required prefix.

The historical [PoC benchmark](../PoC-fuse-and-benchmarks.md) measured host
FUSE/ZFS at 1278.9 random 4 KiB write+fsync IOPS (about 0.782 ms per serial loop),
and 9p/FUSE/ZFS at 867.8 IOPS (about 1.152 ms). Median run fsync p95 values were
0.750 and 0.954 ms respectively, excluding the write syscall. Sequential buffered
writes were 303.3 and 241.7 MiB/s. These are transport-only observations with no
journal or replication. They cannot quantify A versus B latency without partner
and journal measurements; illustrative extra per-write waiting of 1 ms on a
1.152 ms serial loop would reduce throughput to about 465 IOPS. This is arithmetic,
not a prediction or measured network cost.

Future approved tests must exercise positive and rejected operations, short
writes, concurrent barriers, denied access, delayed apply, interrupted accepted
writes, partner loss, daemon restart, journal replay and crash windows. Verify
read-after-write and rename atomicity, permission freshness, writable mmap sync,
directory persistence and cached old-generation fencing on each supported native
adapter and separately on the full 9p path. Include an external oracle of replies
and recovery, and loss of both volatile copies where applicable. No tests were
run to establish these promises while drafting this spec.

References: [Replication](../Replication.md), [filesystem semantics](../Filesystem-semantics.md),
[platform contract](../Platform-interfaces.md), [performance](../Performance.md),
[deployment gates](../Deployment-tests.md), [test approval](../Test-automation.md).
