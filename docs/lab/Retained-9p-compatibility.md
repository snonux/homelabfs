# Retained 9p compatibility repair design, 2026-10-31

Status: **concrete, unselected implementation proposal for `jz3`**. Nothing
below is implemented. The [authorization decision](Retained-9p-authorization.md)
must be explicit before changing that boundary. Keep FreeBSD FUSE → bhyve/lib9p
→ Rocky kernel 9p; introduce neither Linux FUSE nor a separate guest HomelabFS
RPC/daemon. This profile needs a negotiated extension and refuses unmodified
endpoints rather than falling back silently.

## Evidence and integration points

References pin Linux upstream 6.1.190 commit
`79807e970724c1f90cee1316361d61b80db4eef6` and observed FreeBSD revision
`aadd58dddcbc78f4d5594827b46b5633552b15ce`. Installed-byte binding is limited to
the [existing investigation](Deployed-9p-boundary.md#installed-code-and-source-binding).
Future patched source/build/runtime identity needs its own evidence and
[kernel admission](../spec/Kernel-policy.md). Recorded VM state is historical.

| Integration point | Pinned source | Proposed owned change |
|---|---|---|
| Guest request/fid encoding | [protocol](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/net/9p/protocol.c), [fids](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/fs/9p/fid.c) | Per-request credentials, incarnation/handle capabilities and view-recall messages; VFS-context credential capture. |
| Guest namespace/attributes | [dotl inode operations](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/fs/9p/vfs_inode_dotl.c) | Authoritative committed create attributes, atomic metadata and cached-walk/credential revalidation. |
| Guest mapped I/O | [file callbacks](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/fs/9p/vfs_file.c), [writeback](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/fs/9p/vfs_addr.c) | Token-aware read/fault/map-pages/page-mkwrite, splice, writeback and sync paths; positive recalls. |
| Host export/backend | [bhyve device](https://github.com/freebsd/freebsd-src/blob/aadd58dddcbc78f4d5594827b46b5633552b15ce/usr.sbin/bhyve/pci_virtio_9p.c), [lib9p backend](https://github.com/freebsd/freebsd-src/blob/aadd58dddcbc78f4d5594827b46b5633552b15ce/contrib/lib9p/backend/fs.c) | Dedicated backend, negotiated features and exporter-only host FUSE control fd; generic exports unaffected. |
| Host FUSE | [vnode operations](https://github.com/freebsd/freebsd-src/blob/aadd58dddcbc78f4d5594827b46b5633552b15ce/sys/fs/fuse/fuse_vnops.c), [direct-I/O open](https://github.com/freebsd/freebsd-src/blob/aadd58dddcbc78f4d5594827b46b5633552b15ce/sys/fs/fuse/fuse_file.c), [direct read/write](https://github.com/freebsd/freebsd-src/blob/aadd58dddcbc78f4d5594827b46b5633552b15ce/sys/fs/fuse/fuse_io.c) | Private direct-I/O control endpoints using native Read/Write; no assumed FUSE ioctl hook or host-fsync authority proof. |

The reviewed guest mmap code has mixed-I/O invalidation TODOs and a writeback
fid; it does not implement the recall protocol below. This identifies missing
integration in the reviewed version, not a proof that all stock versions can
never support coherence. Every proposed patch remains a feasibility/proof gate.

## Request credentials and host FUSE admission

Negotiate a separately versioned 9p extension before attach. Required features:
per-request credentials, atomic metadata, immutable handle/request identity,
ordered barriers and acknowledged view recall. Pin the entire feature set in
the publication manifest; missing bits, unknown versions and mixed old/new
backends fail closed. Assign actual wire numbers, encodings and bounds in the
reviewed schema task, not by asserting stock 9P2000.L support. Keep virtio framing.

Each authorization request carries immutable operation/arguments, volume/session/
serving epoch, guest boot/mount incarnations, request ID, process incarnation,
current fsuid/fsgid and the **complete** supplementary-group vector. Capture an
immutable credential reference before asynchronous dispatch; worker credentials
are irrelevant. Encode initial-user-namespace numeric IDs with explicit map
identity. Reject unmapped IDs, overflow and oversized groups (current shared
bound: 1024), never truncate. Credential revisions prevent stale authorization
after setgroups/fsuid changes or between simultaneous same-UID processes.
PID alone, attach UID and host passwd groups provide no such provenance.
Keep guest capability/LSM restrictions and host UID-zero-only service policy;
a guest allow never overrides service denial. ACL support is not added implicitly.

The supervised dedicated lib9p backend opens reserved control inodes on the
admitted **real FUSE connection** before publishing its guest export. Each fd is
bound to exporter incarnation, native FUSE session, VM/export, volume and serving
generation. It is protocol state outside the journalled workload namespace.
The dedicated backend never maps their names/object IDs to guest walk/readdir/qids;
host endpoint Open also checks explicit exporter authority, beyond mode `0600`.
Other native participants, guest-chosen object IDs and other exports cannot use
it. A private inode must not become an escape through synthetic guest names.

For every control operation native FUSE UID/GID/PID/groups must equal the actual
enrolled exporter identity. Preserve existing native header equality. A separate
delegated admission entry validates the guest assertion and its exact scope,
then invokes shared Unix authorization with both provenance records attached.
Never feed guest IDs into native FUSE headers or its host Credentials provider.
The delegated context must support request-scoped credential revisions without
pretending the current adapter's fixed-credential process admission supports
dynamic groups. Select it only through the explicit new profile, never by
relaxing existing native/Participants checks or enabling normal mode.
Connection/process/authority replacement revokes the endpoint; enrolling a
running host-root PID alone is insufficient. Refuse stale rootfds/fids and raw
backing or ordinary-directory fallback, including when the mount disappears.

Proposed transport is bounded framed synchronous direct Read/Write on that
endpoint set. Open selects direct-I/O behavior; control traffic must not be
absorbed by buffered/writeback caches. Frames carry version/lengths/session/request ID,
argument digest and operation. Bounded chunks belong to one immutable request;
no incomplete frame executes. Write completion is transport completion, while
the correlated result carries exact errno/count, cut and result identity. Read
by requested result ID rather than a shared consumable response queue. Separate
requests may run concurrently through one bounded deduplication ledger. Lost
replies query the same ID. Cancellation/close cannot undo accepted work; retain
results and unresolved claims until explicit protected retirement, not timeout.
Reserve parser, result and recall/drain credits before accepting work.
Bind each operation to the canonical all-origin claim/anchor before path
resolution; another exporter/origin retry resolves that claim rather than
creating a second mutation. A transport-level request ID alone is insufficient.

Use distinct command, result and recall inodes: a blocking result/recall Read
must not hold the vnode lock needed by command writes or drain completion.
Command writes only validate/copy/enqueue bounded frames and never wait for a
long-running operation or recalled domain. Reserve independent FUSE worker and
queue capacity for recalls/drains. FreeBSD may reuse a FUSE handle for a vnode/
caller; protocol correctness depends on explicit request/session identities,
not one unique native handle per application fd. Subsequent schema work must
define offsets/result selection, virtual sizes and message fragmentation so
kernel read-size/EOF handling cannot drop control messages or misroute replies.

This is a **new local FUSE control protocol**, not an existing API. All guest
operations still cross that FUSE connection and its service authority gate;
the backend has no raw-backing or generic POSIX fallback. Validate volume-scoped
opaque object/handle capabilities rather than raw caller-selected object IDs.
Cached pathname capabilities bind ancestor/entry revisions and credential
snapshot; stale paths revalidate search/mutation against the committed view.
Wrong-volume objects, `..`, symlink escape and cross-session capabilities fail
even for guest root. Native path operations retain their separate checks.

## Atomic namespace, ownership and mode repair

The dedicated backend sends one logical service command for create, mkdir,
link, rename, unlink or setattr instead of generic create-then-chown/chmod.
The service selects request creator UID, always inherits logical parent GID,
applies umask/special-bit policy once and commits the name/data/final metadata
together. Return selected attributes and patch guest creation initialization:
Linux's conditional-setgid GID calculation must not overwrite actual parent-GID
inheritance. No reader, including one served from owner RAM before disk apply,
can observe intermediate exporter-root ownership or a temporary wrong GID/mode.

Carry supported mode bits through `07777` with explicit validity mask and
separate file type; reject unknown combinations instead of masking them away.
Shared policy enforces sticky-directory unlink/rename, chmod/chown/chgrp,
set-ID clearing and requested/inherited setgid. Guest attributes install the
committed result. Combined truncate/ownership/timestamp updates use only atomic
combinations the service supports; unsupported combinations fail before effects.
Crash/lost reply reconciles the same request ID, not a later visible repair.
Host-account groups no longer select the wrong `0004` permission class.

Opened fids carry capabilities minted by service Open using that request's
credentials. Granted descriptor rights survive chmod/unlink; new path/open
checks use current groups and metadata. Track guest descriptions independently
from process IDs and transport fids: dup/fork/transfer retain a description,
final reference retirement closes it. Delayed mapped writeback uses the grant
of the description that authorized the mapping, never root writeback-fid or
worker credentials. Shared-inode dirty pages need explicit provenance so page
merging cannot broaden grants. Credential changes alter new authorization,
not already granted descriptor access. Process-exit events are sequenced guest
kernel assertions; a host close does not invent guest exit. Advisory locks
remain confined to the accepted same-guest same-mount inode domain.

## Concrete view-token and mmap protocol

Initially use generation-bound **per-volume revocable view tokens**. Multiple
domains may hold committed read tokens. Writable shared mappings require an
exclusive dirtying token before installing writable PTEs. Recall and transfer
that token dynamically; this is serialization of conflicts, not exclusive
writable-client publication. Every admitted node can still originate writes to
every volume. Coarse tokens trade concurrency for a reviewable first algorithm;
range-token optimizations require independent proof.

Before installing each writable page, reserve its bounded dirty-data/provenance
capacity and enough protocol/journal/owner-retention/drain resources to resolve
that admitted dirtying under the `6r3` budget contract. Limit mapped dirty pages
before CPU stores, not only when a later recall allocates its manifest. Read
faults must not retain the writable token while waiting for credits that only
token recall can release. Map/unmap/fork and repeated page-mkwrite transitions
must transfer/account those reservations exactly once.

Add a bounded long-lived `VIEW_WAIT` request on a reserved 9p tag/queue. Its
reply delivers recall ID, old generation, closure and target cut. The kernel
posts another wait before acknowledging. Retain outstanding recalls with
sequence/ack replay across lost replies and reconnects. The lib9p control worker
connects this to the daemon's matching ledger. Persist/reconstruct admitted
domain inventory before restart serving; missing connections are never acks.
This is an extension over existing 9p transport, not assumed stock server push.

For every conflicting mutation, including native origins, implement this order
with the [normal protocol](../spec/Replication-protocol.md#committed-reads-caches-and-barriers):

1. Close new conflicting operations/token acquisition under the closure gate
   **before freezing the conflicting grant**. Recall all conflicting tokens and
   drain already admitted requests. Keep a reserved drain path; do not hold the
   sequencer slot, inode locks or credits needed by those drain requests.
2. Guest gates new faults, fault-around, page-mkwrite, buffered/direct I/O,
   cached dentry/metadata observations and dirtying. Revoke existing shared
   read/write/exec PTEs and complete CPU TLB/MMU synchronization. Stop writable
   access first, then snapshot dirty data and its description grants into a
   bounded frozen manifest. Return **STOPPED**, binding that manifest and all
   observation gates, before submitting drains. Drain in-flight reads and
   splice/kernel aliases before STOPPED; a user VMA lock is insufficient.
3. Submit the manifest's dirty data through those descriptions as ordered origin
   operations on reserved drain credits. Their own commit/publication barriers
   install each cut in the already STOPPED domains without reopening user
   observations or admitting a new dirtying token. Return their exact protected
   results to the kernel control worker, then complete dirty-page cleanup.
   Thus drain replies do not depend on the final DRAINED ack they enable.
   No unrelated conflicting grant can enter this reserved drain phase. Reconcile
   failed/uncertain drain claims before proceeding; do not discard dirty data.
4. Invalidate clean pages, permissions/attributes, positive/negative dentries,
   hard-link aliases, existing descriptors and open-unlinked shared mappings.
   Busy/pinned/dirty aliases that cannot be drained and revoked prevent
   **DRAINED**, which certifies the frozen manifest has resolved and no new
   dirtying is possible.
   Preserve failure and keep the closure gated. Zero TTL or ordinary mapping
   invalidation alone is not the required acknowledgement.
5. Only after every exact-barrier stopped/drained acknowledgement, freeze the
   conflicting grant and perform origin-local durable commit plus distinct-node
   retained-memory protection, selection and result protection. No owner disk
   apply is needed for visibility.
6. Publish the owner's committed logical RAM cut, install the new generation
   for future tokens/faults/reads, reopen domains and acknowledge installation
   before successful operation reply. Empty invalidated caches refill from that
   cut; stale writeback/results cannot repopulate them. A read after the reply
   obtains that cut or later from the owner's view, never an origin's stale disk.

Cache hits are allowed only while their domain holds an active view token.
Recall stops existing mapped access as well as new syscalls; concurrent readers
can linearize on the old cut while stopping before publication. Candidate or
partially disk-applied state is never exposed. Owner RAM includes namespace,
attributes and permissions, not just bytes. Native frontend domains need the
same positive barriers (`fz3`); guest repairs cannot certify those other domains.

STOPPED and DRAINED are proposed concrete phase interfaces for the canonical
pre-grant drain requirement, not new evidence or an implemented exception to
ordinary commit protection. A stopped domain acknowledges installation of an
intermediate drain cut while staying closed; it does not acknowledge complete
dirty-state retirement prematurely. Schema/model work must prove that sequence
against the normal protocol before runtime integration. A single acknowledgement
that waits for writeback before permitting its own drain barriers would deadlock.

Kernel work must prove PTE/TLB quiescence, mmap lock ordering, fault-around
exclusion, writeback provenance and pinned/GUP/DMA references, mlock, forked
and executable mappings. Reject unsupported pinning before granting it or
provide proven revocation; uncontrolled references prohibit ack. MAP_PRIVATE
COW pages are application-private memory, like already copied user buffers;
the barrier controls subsequent file-backed faults and shared file observations.
Long-term pins and ordinary file caches cannot use that exemption.

Linux [invalidation](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/mm/truncate.c)
can unmap page-table mappings and returns EBUSY for failed invalidation;
[unmapping](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/mm/memory.c)
supplies MM primitives. These are building blocks, not a race-freedom proof.
The owned integration needs reviewed lock ordering and adversarial kernel
validation. Neither stock `cache=none` nor `cache=mmap` implements this protocol.

Failed recall stops publication. Timeout never replaces ack. Resume only after
exact-generation acknowledged quiescence or an approved positive fence excluding
the CPU/cache/writeback domain, potentially the VM. Graceful unmount/VM stop
counts only after verified completion. Dead exporter, disconnected virtio,
replacement FUSE mount, etcd expiry and a new path do not revoke old mappings.
Account for collateral volume domains in a shared guest. This algorithm can
stall behind a failed guest; it does not promise partition availability or a
bounded write latency. It permits independent admitted clients throughout.

## Sync and the all-origin boundary

The [delivered-write assessment](Retained-9p-sync.md) remains conditional source
evidence. Ordinary accepted counts wait for the full all-origin mutation and
view boundary: origin O owns durable operation/result, remote O uses preferred
owner A's committed RAM, and O=A uses its distinct memory partner. Do not route
all writes into A's durable log, wait for owner disk or downgrade to local only.

Carry sync flags explicitly. Conservatively use a volume BARRIER for O_SYNC/
O_DSYNC, file/data/directory fsync and MS_SYNC after delivering required buffered/
mapped data. It captures all origins' exact selected cut and source dependencies,
not a host fd or local stream. Drain data before capturing that cut, with drain
progress while conflicting grants wait. MS_SYNC covers requested dirty ranges
(a larger drain is allowed); CPU stores, MS_ASYNC, munmap and exit gain no
per-store durability promise. Preserve short counts/writeback errors in guest
mapping/file error state and return them through sync. Control-frame transfer
or a dead-FUSE host fsync never constitutes an attested live service barrier.

## Owned work packages and acceptance

The work packages below are recorded tasks after fresh-context proposal review.
`jz3` remains open pending the explicit guest-trust and maintenance decision.
Only the generic all-frontend barrier model can proceed without selecting that
guest profile. Task readiness grants no installation, runtime acceptance or
permission to patch unrelated upstream/vendor trees. Maintain reviewed owned
patches/build inputs in project integration artifacts rather than host edits.

| Package | Task | Concrete deliverable and dependencies |
|---|---|---|
| Generic stop/drain/install model | `qz3` | Refine `dz3` barrier assumptions with reserved drain progress and positive all-domain install; prerequisite of `fz3` and `6r3`. This model does not select guest trust or certify kernel MM behavior. |
| Delegation schema/policy | `rz3` | Exact extension/frame/features, guest/exporter threat model, path/handle lifecycle, bounds/replay/provenance; depends on explicit trust choice. Revise Security/FUSE-adapter/Filesystem-service/Configuration together. |
| Guest credentials/metadata | `tz3` | Owned kernel patch series for immutable per-request credentials, dynamic groups/process/description lifecycle, authoritative creation metadata and supported full modes, replacing root writeback impersonation; depends on schema. Parser tests and reproducible binding precede installation. |
| Host lib9p/FUSE bridge | `uz3` | Dedicated backend, supervised exporter handshake, private direct-I/O control inode, bounded parser/results/recalls, atomic dispatch/confinement/K04 and session revocation; depends on schema/service delegation. Keep native checks and normal-mode refusal. |
| View tokens | `vz3` | Owned guest token/recall/MM/VFS integration, native domain barriers through `fz3`, pre-grant drain and publication acks, reviewed lock order/model; depends on successor protocol API. Prove busy/pinned aliases and reconnect handling rather than assuming invalidation. |
| Packaging/admission | `xz3` | Maintained patch manifests, source/build/artifact hashes, actual booted module identities, feature/policy revisions, lifecycle/recovery/fence receipts; integrate `tr3`/kernel policy without implicit production upgrades. |
| Host delegated service/control endpoints | `sz3` | Exact exporter authentication and separately delegated guest Unix policy, bounded control parser/results and atomic dispatch, depending on schema and `fz3`. |
| Isolated fixture plan | `wz3` | Bounded artifact/resource/lifecycle plan, with actual guest-domain and memory admission before any separately approved run. |
| Base harness (`zq3`) | `zq3` | Real-daemon single-node synthetic dynamic multiuser namespace/mode/sync/mmap/K04 oracle. Two mounts on one origin can exercise local recall; they are not distributed proof. Depends on base repairs, not future `ov3` execution. |
| Distributed acceptance (`ov3`/`pv3`, `cw3`) | Existing tasks | Independent admitted hosts/guest domains writing SAME volume, remote durable source, delayed owner disk/committed RAM reads, all-domain barriers and fences; depends on core `4r3`, `fz3`, representation and harness, without cycling through base acceptance. |

Required positive/negative evidence includes:

- Two non-root UIDs, supplementary-only access, concurrent same-UID/different
  groups, setgroups/fsuid changes, `0004` class selection, ownership/chgrp/sticky/
  set-ID/parent-GID and restrictive namespace mutations. Wrong/truncated groups,
  foreign capabilities and guest-root cross-volume attempts fail before effects.
- Concurrent observation of create/metadata from another origin: no temporary
  exporter-root/wrong-GID state. Crash/lost replies at each phase yield one
  immutable result or explicit uncertain operation, never an invented rollback.
- Same-mount POSIX/flock blocking/nonblocking/GETLK/lifetimes. Separate mounts/
  guests record lack of shared advisory locks as the accepted limitation.
- Shared mmap readers/writers, overlapping mixed regular/mapped writes,
  truncate/hole/rename/chmod, hard links, open-unlinked, forked mappings and
  negative dentries. Busy/pinned pages, paused recalls, reordered replies and
  disconnects cannot yield false acks or stale post-completion observations.
- Ordinary/O_SYNC/O_DSYNC, file/data/directory sync and MS_SYNC with short counts,
  writeback errors and delayed origin sync/owner protection. Include prior
  foreign-origin metadata/data and recovery against the external reply oracle
  while owner backing apply remains delayed.
- FUSE restart/dead fsync, retained rootfd/fids, stale generation, lost recalls
  and guest CPU/cache partition: no K04 fallback, timeout reopening or successor
  publication before actual positive domain fencing.
- Explicit pod ID maps/groups/capabilities and SELinux allow/deny/audit cases
  before isolated pod claims. Plain Rocky is not the k3s node; its historical
  inventory supplies no pod acceptance evidence.

The three 2 GiB FreeBSD guests are the native suite, not two Rocky cache domains.
Separately inventory/resource-plan extra disposable guest fixtures. Never run
the existing large Rocky VM alongside those guests on f3 or alter production
consumers to fill a missing fixture. This proposal performed no SSH, lifecycle,
mount, test, installation, patch deployment or runtime attestation.

Fresh-context proposal review found no blocking findings. The generic barrier
model, selected guest repair packages, base gate, later packaging and distributed
acceptance are separately dependency-gated. The pre-replication artifact package
`xz3` does not depend on `tr3` release packaging or `4r3`; `zq3`
consumes it, preserving the original base-gate ordering without a cycle.
