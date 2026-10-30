# Retained 9p: conservative synchronous-write admission

Source assessment for task `2z3`, 2026-10-30, against HomelabFS `41c3135`.
**A covering commit boundary on every daemon mutation can preserve regular
O_SYNC/O_DSYNC write semantics without forwarding those flags.** The current
single-volume worker already provides the required ordering shape. This is a
conditional implementation proposal, not a running 9p, normal-replication or
power-loss result. Guest dirty mapped pages need their explicit delivery/sync
boundary; a daemon barrier cannot discover pages still held by the guest.

Paul retained 9p with only the [same-mount advisory-lock concession](../Platform-interfaces.md#accepted-9p-lock-scope-2026-10-30).
All other [durability requirements](../spec/Durability.md), credential and
publication gates remain. This proposal does not admit `zq3` or unblock `4r3`.

## Evidence and version limits

The [installed evidence](Deployed-9p-boundary.md#installed-code-and-source-binding)
binds f3's observed FreeBSD revision
`aadd58dddcbc78f4d5594827b46b5633552b15ce` to inspected on-disk library semantics,
with the stated source/build and mapped-memory attestation limits. In the
[retained disassembly](results/20261030-9p-boundary/installed-lib9p-disassembly.txt),
lopen conversion at `0x13150` drops synchronous flags, write at `0x12c10` calls
`pwritev`, and fsync at `0x141a0` calls `fsync`, selecting `dirfd` for directories.
The retrieved shared-library SHA-256 is
`589e8d0f9db4401f8a33274a5efe31d3cd742b18cedba87601c39617501c01ed`.
Matching [FreeBSD lib9p source](https://github.com/freebsd/freebsd-src/blob/aadd58dddcbc78f4d5594827b46b5633552b15ce/contrib/lib9p/backend/fs.c)
clears O_SYNC/O_DSYNC, returns `pwritev`'s count or errno, and uses full host
fsync even for a datasync request. These facts do not imply a daemon barrier
by themselves.

The guest assessment is upstream Linux 6.1.190 commit
`79807e970724c1f90cee1316361d61b80db4eef6`. The
[official ELRepo recipe](https://github.com/elrepo/kernel/blob/8df48fa42eadfd2d79a5ef6036b80979846457f4/kernel-lt/el9/kernel-lt-6.1.spec)
has no 9p preparation patches. The installed candidate was not booted; the
observed running Rocky kernel lacks 9p support. Recipe/source agreement is not
independent installed-module or runtime verification. Admission must bind the
actual kernel/modules, `version=9p2000.L`, `cache=mmap`, exporter/library bytes,
host FUSE negotiation and daemon artifact. Other cache modes or versions need
their own assessment.

## Why every mutation can cover synchronous regular writes

This is an induction over the service queue, not an equation between a write
and a host backing-file fsync:

1. [Dispatch and the single worker](../../internal/fsservice/service.go) snapshot
   commands, serialize accepted work and execute one command to completion
   before the next. Queue order defines concurrent admission order. A guest
   operation that finishes before a subsequent guest operation begins must
   have completed all its required daemon commands first; concurrently issued
   operations require application synchronization to establish an order.
2. [Mutation planning](../../internal/fsservice/mutation.go) records bytes plus
   size, mtime, mode changes and other logical effects.
   [Plan finalization](../../internal/fsservice/plan.go) produces revisioned
   object, namespace, parent and link-ledger afterimages, including full file
   metadata rather than a data-only variant. Namespace mutations use this same
   protection path; there is no weaker create/rename/chmod reply path.
3. `protect` in [service.go](../../internal/fsservice/service.go) appends a
   MUTATION and COMMIT candidate depending on the previous selected position.
   It waits for both local journal Flush and Protocol.Protect, checks authority,
   authorizes/applies the selected history, and advances selection only after
   successful apply. [Journal Flush](../../internal/journal/segment/writer.go)
   covers prior receipts; segment creation/rotation also syncs required names.
   Durable initialization/ancestors and retained replay/checkpoint evidence
   remain prerequisites; syncing just a current data fd would be insufficient.
4. With a **real** [Protocol](../../internal/fsservice/types.go), Protect must
   retain/choose the complete exact prefix and dependency graph, including
   structural frames absent from Proposal.Frames, under the current partner
   session. Authorize must validate selection and retain gates through apply.
   [Normal replication](../spec/Replication-protocol.md) requires these actual
   conditions, not merely receipt of a last sequence or two new frames.
5. Thus successful mutation N covers N and the required prefix already covered
   by N-1, including earlier file and namespace metadata. Evaluation checks
   authority again before completion. A fault blocks the worker; later queued
   work cannot successfully jump a failed prefix.

Consequently a nonempty delivered write already has a conservative per-volume
covering boundary, regardless of the retained handle's SyncMode. Adding a second
Sync command after every Write would duplicate this ordering and cannot flush
guest-only pages. [Adapter.Write](../../internal/fuseadapter/io.go) waits for
service completion before returning the count. [Sync](../../internal/fsservice/execute.go)
serializes after prior volume commands, validates file/data/directory kind,
flushes and receives the same final authority check. It does not invent a
replication BARRIER. A zero-length write creates no new durability event.

**Present implementation limit:** validation supplies a local-only proof marker
and no partner. No production Protocol exists, and the adapter/CLI refuse
normal publication. The induction establishes how the implemented interfaces
can supply the intended boundary, not that they currently implement replication.
The accepted single-durable-copy contract still loses protection if owner
storage and the partner's volatile tail are both lost.

## Operation-to-boundary mapping

In the pinned guest, [inode operation selection](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/fs/9p/vfs_inode.c)
chooses mmap-specific operations for CACHE_MMAP; loose/fscache choose generic
buffered writes. [Protocol writes](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/net/9p/client.c)
wait for RWRITE counts and may split one syscall into multiple requests. The
table assumes the healthy, admitted host path described immediately below.

| Guest operation | Delivery and required completion |
|---|---|
| Regular write/pwrite/writev, no synchronous flags | CACHE_MMAP write_iter sends protocol writes directly. Every returned byte must have a covering daemon mutation completion. Promise only the returned count; later chunks may fail. |
| Regular write on O_SYNC / O_DSYNC open | Same direct write path, with no generic_write_sync call. Flag loss is harmless **if every returned byte and prior ordered daemon mutations already meet the full boundary above**. Full metadata protection conservatively satisfies datasync too. No second durable replica is implied. |
| fsync(file) / fdatasync(file) | Guest file_write_and_wait_range precedes TFSYNC. Host full fsync may strengthen datasync. Success requires delivered dirty data and the ordered daemon Sync boundary; prior full-file fsync covers the file's preceding mapped data. |
| fsync(directory) | Dotl directories use the file sync callback; host lib9p selects dirfd. Daemon DirectoryDurability orders prior namespace effects and dependencies. It is not a cross-volume transaction or a guest-wide dirty-page drain. |
| create/link/unlink/rename/truncate/setattr | Each delivered mutation uses complete redo and the same covering commit path. Both rename parents/dependencies must be recoverable. File sync alone is not a general parent-name durability promise; this daemon separately protects successful namespace replies. |
| Writable MAP_SHARED store | Dirties a guest page, with no per-store daemon acknowledgement. O_SYNC on the mapping's fd does not turn CPU stores into requests. |
| MS_SYNC | Guest calls vfs_fsync_range with datasync=1 for the shared mapped range; writeback then TFSYNC must complete. Host full sync may strengthen it. Application synchronization defines covered bytes during concurrent stores. |
| MS_ASYNC / munmap / close / process exit | No substitute successful durability boundary. MS_ASYNC does not start I/O in this kernel. Mapping close attempts writeback but cannot supply an application-visible successful sync result; release/clunk is not fsync. |

Pinned primary sources for the table:
[9p file callbacks](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/fs/9p/vfs_file.c),
[directory fsync](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/fs/9p/vfs_dir.c),
[mapped-page writeback and errors](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/fs/9p/vfs_addr.c),
[MS_SYNC/MS_ASYNC](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/mm/msync.c).

The host boundary matters: pinned [FreeBSD fuse_vnop_write](https://github.com/freebsd/freebsd-src/blob/aadd58dddcbc78f4d5594827b46b5633552b15ce/sys/fs/fuse/fuse_vnops.c#L2470-L2476)
forces IO_SYNC for buffered writes when writeback is disabled.
[fuse_io.c](https://github.com/freebsd/freebsd-src/blob/aadd58dddcbc78f4d5594827b46b5633552b15ce/sys/fs/fuse/fuse_io.c)
then uses synchronous bwrite; its direct backend waits for FUSE replies and
handles counts/errors. The [adapter mount options](../../internal/fuseadapter/mount.go)
do not enable writeback. This supplies source support for pwritev waiting for
daemon replies without host O_SYNC, including buffered host opens. Actual
negotiation and request correlation remain required; a writeback-enabled host
would invalidate this regular-write argument.

The native [durability table](../spec/Durability.md#successful-operation-completion)
is explicitly at the daemon request boundary. Its separate mmap contract does
not promise persistence of every CPU store before an unrelated synchronous
write. Do not silently expand the claim to guest-only dirty pages: the direct
regular-write path does not generally drain them, and its mmap invalidation
TODO warrants mixed-I/O/coherence validation. Explicit MS_SYNC/later fsync must
cover the required mapped data. A profile requiring each O_SYNC write to flush
all earlier guest-only mapped stores cannot be justified by daemon barriers;
that stronger guest requirement would need explicit resolution before admission.
Do not reject the existing daemon-boundary contract merely by assuming it.

## Minimal prerequisites and acceptance

No lib9p patch, transport replacement or extra per-write SyncMode dispatch is
needed for the conditional regular-write argument. Preserve the existing full
mutation path. Implement a bounded correlation/oracle seam in the future 9p
harness: guest operation/range/result, host FUSE requests/results, service
admission order, proposal/selected position, local Flush and final completion.
Use explicit local-validation labels until real Protocol implementation and
its independent replication validation exist. Any future pipelining/grouping
must preserve the covering-prefix invariant before replies.

The retained credential/authorization solution, exclusive consumer publication,
generation/cache invalidation, K04 and old-client fencing are separate mandatory
prerequisites. In particular pinned
[FreeBSD fuse_vnop_fsync](https://github.com/freebsd/freebsd-src/blob/aadd58dddcbc78f4d5594827b46b5633552b15ce/sys/fs/fuse/fuse_vnops.c#L1152-L1167)
returns success immediately for a dead FUSE filesystem. Thus a successful host
fsync alone cannot attest current daemon authority. Publication must prevent
stale retained guest/bhyve descriptors from yielding misleading success after
loss; a healthy-path trace cannot certify this. No fix or successful fencing is
claimed here.

Smallest proposed acceptance matrix, after those prerequisites and separately
approved lab preparation; all data synthetic and bounded:

| Positive case | Required negative counterpart |
|---|---|
| Ordinary, O_SYNC and O_DSYNC writes through identical full boundaries; include multi-request and short writes, earlier same-file data, chmod/truncate and preceding directory changes. | Delay local Flush, protection or apply independently: no positive count beyond protected bytes; failures block later requests. Zero-byte writes create no fabricated sync proof. |
| Explicit file/data/directory Sync after ordered mutations, including cross-parent rename. Reconstruct fresh local workspace from retained journal and compare bytes/size/mode/names to external reply oracle. | Missing prefix/structural frame/dependency, failed directory sync or changed authority cannot produce success. Local-only replay cannot count as partner or power-loss evidence. |
| Dirty disjoint mapped pages then MS_SYNC range; separately dirty pages then full fsync, and mixed mapping/regular writes with explicit application ordering. Correlate writeback requests before barrier completion. | Inject writeback error: guest sync reports failure; no TFSYNC success masks failed delivery. Unsynced pages outside the range and MS_ASYNC/munmap/exit get no invented durability promise. Detect mixed-I/O stale/overwritten pages. |
| Healthy host buffered writes with writeback demonstrably disabled; every RWRITE count follows daemon completion. | Reject negotiation/cache/kernel drift. Exercise dead FUSE and retained fids, lost replies, daemon restart and old-generation cached/mapped access; failure or success without the required live boundary is a gate failure. |

Existing [service tests](../../internal/fsservice/service_test.go) cover the
external-protection wait/cancellation seam, queue bounds and ordered append/Sync;
[fault tests](../../internal/fsservice/fault_test.go) cover local failure/replay
cases. Their protocol stubs and local fixtures are not deployed replication
proof. On a lost reply or canceled accepted wait, the operation may commit;
reconcile against retained history/oracle rather than assuming rollback or
blindly retrying non-idempotent work. Bounded transport deadlines must preserve
uncertain resources for inspection.

This task ran source and artifact-text inspection only. No tests, remote
operations, VM lifecycle, runtime sync trace or power-loss experiment ran.
The result removes stripped flags as an automatic regular-write blocker, while
leaving exact-target sync/error/fencing acceptance and normal replication open.
