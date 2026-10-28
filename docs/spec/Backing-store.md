# Backing store and stable object identity

Status: design contract for task `bq3`, selected under Paul's delegation of
routine design choices on 2026-10-28. This describes an unimplemented store for
disposable lab/canary volumes. The loopback code in `tools/poc/` implements none
of this contract and must not be imported into the daemon.

Use private, home-keyed regular payload files, an in-memory logical catalog
reconstructed from an immutable catalog checkpoint and journal redo, and durable
publication manifests. The catalog owns namespace and replicated metadata.
Underlying ZFS files supply offset I/O, allocation, truncate and persistence.
No embedded database, host namespace mirror or xattr dependency is required.

The [transaction contract](Transactions.md), [volume policy](Volumes.md),
[durability boundary](Durability.md) and [local persistence assumptions](Local-io.md)
are prerequisites. This store does not choose distributed commitment or fencing;
only the replication/recovery core can authorize replay or serving.

## Alternatives and selection

| Layout | Benefits | Costs and compatibility |
|---|---|---|
| A: path mirror plus stable-ID xattrs or sidecars | Host rename/link/readdir and Unix metadata are readily available; easy manual inspection. | Native links cannot span ZFS datasets. A moved directory still owns entries in its original home under Transactions. Mirroring the new path would move/copy ownership or require another authoritative indirection layer. Xattrs alone do not supply link, parent, request or recovery ledgers, and native inode numbers differ across replicas. |
| B: ID-keyed payloads plus logical namespace/metadata | One home and payload per identity; foreign references, rename, hard links and stable replay have direct representations. Regular-file I/O uses ZFS; small immutable catalog snapshots and the existing ordered journal suffice initially. | Service implements namespace, permissions, times, logical link counts and alias coherence. Catalog memory and full-checkpoint cost need explicit bounds. Raw backing paths are unsuitable for workload access. |
| C: authoritative ID store plus path mirror/cache | Retains B's semantics and could accelerate administrative traversal or selected local operations. | Adds mirror repair, duplicate space/accounting, publication order and a bypass risk. A mirror cannot become authoritative for foreign entries and does not eliminate the logical store. |

Select B. The existing transaction decision already requires its logical object
layer; adding a mirror or database would not remove that requirement. Initially
load the complete bounded volume catalog into memory and write a complete catalog
checkpoint. Admission caps object/entry/session counts and checkpoint staging
bytes within the run's budget; if they do not fit, reject admission or reduce the
synthetic profile. A future indexed implementation needs a measured reason and
a versioned format change, not an implied unbounded-memory promise.

## Identity, allocation and metadata

`ObjectID = (home volume UUID, allocation UUID)`. The active owner allocates a
cryptographically random UUIDv4 once per new object in its home group. It checks
for collision against the catalog, retained allocation/tombstone evidence and
pending allocations; collision means allocate another UUID before admission.
Protect CREATE with that exact ID and initial descriptor in the home journal
before publication. Followers receive the ID; they never allocate a substitute.
Initially a newly created child takes its parent directory's home, which may
differ from the namespace volume after a directory move. This keeps its new
entry, metadata and payload in one group. Rename/link never reallocate its home.
No pathname, native inode, replica node ID or mount-local inode number is an ID.
An ID never changes during rename, link, recovery, owner transfer or restore, and
is never reused after reclamation. Retained allocation records and fresh random
allocation prevent deliberate reuse; do not recycle IDs by resetting a counter.

Volume roots also have allocated, immutable ObjectIDs. Root creation is part of
the protected volume initialization record, shared by all replicas. A recovery
generation chooses authoritative history, not a replacement object identity.
Restoring an existing volume preserves its UUID/IDs behind recovery gates; an
independent writable clone needs new volume/object identities and rewritten
references under a separately specified clone operation. It cannot run as a
second owner of the restored UUID.

Each catalog object records its ID, immutable type (regular, directory, symlink),
creation operation identity, object revision, numeric UID/GID, mode and special
bits, logical size, atime/mtime/ctime, and link/lifetime state. Times use signed
Unix seconds and nanoseconds in `[0, 999999999]`; the owner chooses automatic
times once and journals the exact values. Replicas never substitute apply time.
Regular-file size is logical committed size; host stat size during partial apply
is not authoritative. Symlink text is stored as bytes in the catalog/journal;
a symlink object is never a host symlink. No device/FIFO/socket or ACL support
is implied. Required unsupported operations return explicit capability errors.

Atime uses an explicit initial noatime mount policy: reads do not mutate it;
explicit supported timestamp changes are replicated. mtime/ctime and metadata
changes follow the shared service's Unix rules, including creation umask/group
inheritance and special-bit changes on writes/chown. Those semantic rules are
shared across adapters; the backend never guesses them from its own credentials.
UID/GID are unsigned 32-bit values, mode is the Unix 07777 permission/special-bit
mask, and regular-file sizes/offsets are in `[0, 2^63-1]`. Reject unsupported
ranges; revision/position exhaustion stops admission rather than wrapping.
The backing root/directories are private to the daemon (0700), files 0600;
host ownership, mode and timestamps are administrative state, not replicated
Unix metadata. Workload permission checks and stat replies use fresh catalog
metadata, including the common metadata shared by every hard-link alias.

## Dataset and run mapping

Follow the [journal sibling layout](../Journal-storage.md). In the intended
layout, `data` and `journal` are sibling datasets beneath a volume UUID parent;
`control` is node/cluster storage outside per-volume rollback:

```text
zdata/enc/homelabfs/
  volumes/<volume-uuid>/data
  volumes/<volume-uuid>/journal
  control
```

This is a future layout, not permission to allocate production datasets. Control
contains registry bindings, node identity, admission/emergency policy and fence/
recovery-generation evidence. No workload can reach any raw store directory.

The prepared native lab instead has `hfsdataN/{data,journal,control}` mounted at
`/var/db/homelabfs/{data,journal,control}` inside `hfs-testN`, for N = 0, 1, 2.
Map one approved run and volume as follows; K is the validated run ID and V is
the same volume UUID on every assigned replica:

| Role | Run-owned logical location | Guest mount path |
|---|---|---|
| Data child dataset | `hfsdataN/data/hfs-run-K/V` | `/var/db/homelabfs/data/hfs-run-K/V` |
| Journal child dataset | `hfsdataN/journal/hfs-run-K/V` | `/var/db/homelabfs/journal/hfs-run-K/V` |
| Control binding directory | `hfsdataN/control/hfs-run-K/volumes/V` | `/var/tmp/hfs-run-K/volumes/V` |
| FUSE mount | Run manifest's mount entry | `/var/tmp/hfs-run-K/mnt/V` |

The three base datasets exist. The selected initial [canary mapping](../Canary-volumes.md)
plans typed `data`, `journal` and `control` run-parent datasets at
`hfsdataN/{data,journal,control}/hfs-run-K`, with seven per-volume data child
datasets and seven corresponding journal child datasets on each replica.
Per-volume `refquota` values and aggregate run-parent quotas follow that mapping
and the [capacity plan](../Capacity-plan.md). The control run dataset is mounted
at `/var/tmp/hfs-run-K`; `volumes/V`, `mnt` and helper/evidence directories are
owned subpaths inside it, not additional datasets or capacity allowances.
Data/journal mount paths above are the planned mappings; resolve and verify
actual dataset GUIDs and mountpoints in the approved manifest before use.
These run parents and children are future planned resources, not resources
created or observed by this spec. Never infer a dataset from slash notation or
fall back to a directory when the selected child dataset is absent.

A generic directory-backed alternative would have logical accounting rather
than independent per-volume ZFS quotas/snapshots. It is outside the selected
initial canary profile and needs a separately admitted aggregate quota/recovery
profile in which the complete containing dataset belongs to the same approved
recovery unit. Its manifest must include all contained roots and their snapshot
costs; it cannot borrow another owner's storage or capture unrelated runs.
Neither mode authorizes child dataset allocation or a snapshot operation here.
The historical PoC used `data/hfs-poc-K` and exported a raw loopback tree; it is
not a formatted store or a baseline to upgrade in place. Native FUSE runs inside
each FreeBSD guest; the separate host-FUSE/Rocky-9p probe is not this layout.
Read [lab operations](../Lab-operations.md) and [test automation](../Test-automation.md)
before lab work; this document authorizes no run, allocation, VM or SSH action.

## Version 1 private layout and encoding

Paths below are relative to the data and journal roots, not workload paths:

```text
data/
  format.json
  objects/<aa>/<allocation-uuid>/identity.json
  objects/<aa>/<allocation-uuid>/payload       # regular files only
  checkpoints/<checkpoint-uuid>/catalog.json
  checkpoints/<checkpoint-uuid>/manifest.json
  CURRENT                                    # published checkpoint pointer
  staging/<operation-uuid>/...                # private, never a workload view
journal/
  format.json
  segments/...                               # ordered recovery/redo records
  checkpoints/<checkpoint-uuid>.json          # journal checkpoint acceptance
  staging/...
control/
  ...node and cluster records...
  volumes/<volume-uuid>/store-binding.json    # run prefix applies in the lab
```

`aa` is the first two hexadecimal digits of the allocation UUID with hyphens
removed; UUID components use lowercase canonical hyphenated spelling. The data
root supplies the home UUID. Foreign payloads never appear under another home's
`objects/`. Directory/symlink objects have an identity file but no payload.
Object directories are flat identities, independent of logical parentage.

`format.json` is a newline-terminated UTF-8 JSON object with exactly these fields:
`magic = "HOMELABFS-STORE"`, `major = 1`, `minor = 0`, `role = "data"` or
`"journal"`, `volume_uuid`, and `root_object_id = {home_uuid, allocation_uuid}`.
The two roles must agree on identity/version. `identity.json` records
`magic = "HOMELABFS-OBJECT"`, `major = 1`, the full ObjectID, immutable type and
creation operation identity. Create it exclusively, sync it, and persist its
name before relying on it. Matching IDs with conflicting descriptors are
corruption; a payload is never identified by whichever file happens to be at a
user path. The backend owns the tree exclusively and rejects unexpected host
symlinks, mounts, special files and aliases.

Catalogs/manifests use strict, versioned UTF-8 JSON with integer fields decoded
without floating-point conversion; reject duplicate fields, invalid ranges,
duplicate IDs/entry keys and unknown mandatory versions/features. Major/minor
1/0 is the only initially writable format; unknown versions fail closed without
rewriting. No implicit format-on-open or automatic upgrade. Names and symlink
text use canonical padded base64 for raw bytes, preserving case and Unicode
spelling. Directory names exclude empty, slash, NUL, `.` and `..`; `.` and `..`
are synthesized by traversal. Sort object arrays by full ID and entries by
parent ID then decoded name bytes. Encode revisions/positions as exact unsigned
integers, and include history/configuration identities alongside positions.
Hashes are lowercase SHA-256 of exact file bytes; manifests record byte lengths
as well. Encoding schemas must specify field order for deterministic writers;
readers verify stored bytes rather than re-encoding to check a hash. Journal
framing/checksum/version details belong to the journal format, not Go structs.

The catalog contains all object metadata, directory entries/revisions, parent
relations, link/backreference and lifetime ledgers, allocation tombstones,
request/session state, affinity/transaction evidence references and quota/pin
state needed at its checkpoint. The manifest binds its hash/length, volume/root,
format, checkpoint UUID, recovery generation, configuration/epoch, committed and
applied cut, closure/visibility generation, dependency vector, required journal
suffix and predecessor checkpoint. `CURRENT` contains that checkpoint UUID and
manifest hash/length; it is a small pointer with its own strict schema/version.
The journal acceptance record binds the same manifest hash and required evidence.
Control binding records the configured actual data/journal identities, version,
root ID, run ownership, registry retirement state and authority generation.
Validate it against current authority; copying old control files cannot grant a
serving right. Neither format marker is an apply checkpoint or authority token.

## Namespace authority, links and lifetime

The authoritative logical state is a verified catalog checkpoint plus the
complete authoritative committed journal suffix. The in-memory catalog is its
applied view. A directory entry is `(directory ObjectID, raw name bytes,
entry revision) -> target ObjectID`. Store/journal that entry in the directory's
home group, even if the directory now appears beneath another volume root.
Per-directory revision and parent/name comparisons guard replacement races.
Native readdir, paths, file stat/xattrs and a cached reverse index cannot
reconstruct the logical namespace authoritatively.

The object's home owns its payload, Unix metadata and durable lifetime ledger.
For regular files the ledger records the exact referring `(parent ID, name,
entry revision)` set, logical link count and foreign-home dependencies. Namespace
entry and home ledger updates share one committed decision. Counts are exact
result values with revisions, never an unguarded replay of `nlink++`. Reverse
lookup caches, mount-local inode maps, sorted readdir caches and alias cache
registrations are rebuildable from committed state; the reference/lifetime ledger
and transaction evidence cannot be discarded as a cache. A consistency scan can
verify/rebuild a damaged index only from a complete verified closure history,
not invent a missing reference or infer unlink from an unavailable home.

A directory has one logical parent relation (root points to itself), parent-entry
revision and directory-link accounting. Cross-volume directory rename changes
that relation and the old/new parent accounting atomically; descendants and
entry ownership stay put. Serialize ancestry checks/parent changes over the
closure. No recursive host-directory rename, payload copying or ZFS cross-dataset
hard link occurs. Traversal enforces the authorized namespace/view boundary on
`..`; the immutable home UUID does not determine that boundary.

Link adds an alias to the same object and metadata. Rename preserves source ID;
replacement removes a reference to the recorded replacement ID, never to the
current occupant of an old pathname. Unlink removes only its recorded entry and
home reference. Regular-file same-object replacement is the transaction spec's
no-op. Ordinary cross-mount rename/link still returns `EXDEV`; operations inside
a mount that touch foreign homes use the internal multi-group protocol. Reads
and writes resolve to the local home replica on the common owner and wait for
its freshness dependencies. A foreign entry is not a backup copy of its payload.
File fsync/fdatasync barriers route to that home; directory/volume barriers retain
their recorded directory-home, foreign-home and transaction dependency vector.
Initially they may conservatively wait for the closure's captured committed cut.
They do not create an application transaction or a cross-volume snapshot.

Open descriptors remain bound to `(ObjectID, serving generation, handle ID)`;
namespace replacement cannot retarget them. Separate kernel lookup/forget counts
from open/mapping pins and from regular-file link count. Before returning the
first live handle for an object/incarnation, protect a home `HANDLE_PIN` record;
subsequent handles may share that conservative pin. Protect release of the last
handle/mapping pin before GC relies on it. Retain pins across uncertain close or
crash until recovery proves that incarnation's descriptors/mappings are revoked;
never migrate open handles automatically to a new owner. In-memory handle and
shared-lock tables remain generation-bound. Restart first reconciles authority
and fences old cache/client paths, then protects obsolete-pin release.
Kernel node references that can still authorize object operations also block
reclamation; protect the same conservative incarnation pin before exposing such
a node, and release it only after its lookup/forget obligations end.
An unlinked object remains usable through its existing supported descriptors and
mappings until those pins end. FUSE alias cache/locking/mmap coherence remains an
adapter acceptance gate, not a feature of host hard links.

Charge payload/object metadata to the home, directory entries to their directory's
home, and each journal/control record to its owning group/administrative budget.
A move does not transfer payload quota or free its original home's usage. Reserve
space for complete redo, checkpoint staging, retained outcomes and all closure
participants before admission. Refuse new work before recovery reserve is spent;
space failure after COMMIT blocks installation/recovery, never changes COMMIT to
ABORT. Home retirement is pinned by foreign entries/moved subtrees, live handles,
requests/sessions, unresolved transactions, affinity, catch-up and snapshots.
An empty namespace root therefore does not imply an empty or reclaimable home.

## Apply and replay

The service resolves/authorizes user paths under namespace locks and records
stable IDs/revisions before journal admission. Redo contains exact byte ranges,
write bytes and resulting size/metadata for writes, exact size for truncate,
full metadata after-images, and expected old plus exact new entry/ledger states
for namespace operations. Include operation identity, history/position, payload
and decision dependencies. A transaction prepare stages these values privately;
it cannot mutate published objects. Apply only committed/protected operations
under the existing commit protocol, behind read and namespace gates.

The first implementation applies one contiguous ordered suffix per home, with
closure gates for multi-group operations. Ordinary writes/truncates use the
private payload's offset operations after commitment; no uncommitted bytes enter
it. Publish catalog state only after the payload/namespace apply and cache
barrier required by the service. Reads/stat/access never bypass that applied
state or serve a partially modified file. An acknowledged mutation may still
need replay rather than a synced payload: the full redo/prefix stays in the
journal under the [durability contract](Durability.md).

Restart loads a verified checkpoint at C, reconstructs metadata from C, then
replays the **complete ordered committed suffix** behind closed gates, regardless
of how much partial apply survived. Do not resume from a guessed per-file mtime,
size, maximum revision or highest received position. Repeated offset writes of
identical bytes and exact truncates to the same never-reused ID are redo-safe
when the full ordered suffix is replayed; metadata starts from C and exact
revisioned after-images do not double-increment counts. A duplicate journal
position is accepted only with the identical operation/digest; a gap, conflicting
bytes or mismatching expected catalog revision stops recovery. Never replay an
old standalone write/truncate over a later serving state just because its ID is
valid. Reconciliation chooses history first; replay then follows that history.
In-place redo cannot undo bytes from a discarded divergent/later history. If
reconciliation selects an older cut or excludes any suffix that may have touched
these payloads, install a verified clean snapshot/baseline with its complete
selected suffix into a new destination. Do not reuse contaminated live payloads
or treat an older catalog file alone as an older data image.

CREATE can reconcile an incomplete object directory only if its immutable
descriptor agrees with that protected CREATE. Reinitialize that newly allocated
payload and replay all its later writes when reconstructing from a checkpoint
before its creation. An existing object at C needs its checkpoint base payload;
a missing base or mismatching identity is unavailable/corrupt, never an empty
replacement. Private staging may be removed only after proving it has no live
prepare/create/recovery reference. Physical payload deletion is deferred GC,
not the apply implementation of logical unlink. Every retained live baseline
and suffix must still have its required payload base or a verified replacement.

## Checkpoint publication and reclamation

No atomic rename spans data, journal and control datasets. Use the following
conservative protocol, with the journal/core supplying authoritative positions:

1. Gate the entire dependency closure. Resolve pending transactions, or retain
   their complete prepares and decision-resolution dependencies explicitly. Choose
   a consistent committed cut vector C with all its effects installed and the
   shared visibility state known. C is the actual installed boundary, never an
   arbitrary earlier prefix of already modified payloads. Stop backing apply
   beyond C until publication finishes. Journal admission/receipt may continue
   only as separate retained suffix state that cannot mutate this checkpoint image.
2. Sync every payload changed through C, immutable identity file and newly
   created object directory/shard parent. Sync file bytes and size before names;
   sync every changed parent directory, including new nested names. No applied
   checkpoint can lead these barriers. Retain all prior checkpoint/suffix pins.
3. Write a complete new catalog and manifest under a fresh checkpoint UUID in
   private staging. Sync each file, the checkpoint directory and its parent;
   rename into `checkpoints/` and sync both changed parents. Hashes/lengths and
   C's recovery/transaction/session/affinity dependencies must validate before
   publication. The immutable files are now durable but not yet selected.
4. Write/sync/rename/sync-parent the journal checkpoint acceptance record tying
   that exact data manifest to retained commit/decision evidence, prefix coverage
   and required suffix. Retain all old evidence. For a closure, every member's
   durable acceptance must link the same closure checkpoint UUID/vector and
   decision replacements before any member treats it as complete coverage.
5. Write a fresh `CURRENT.tmp-<uuid>`, sync it, rename over `CURRENT`, and sync
   the data root. Only after those barriers is the data pointer durable. A partial
   closure pointer fan-out stays gated; recovery completes/validates the vector.
   Journal acceptance preceding CURRENT is an orphan candidate, not permission
   to delete the old checkpoint. A CURRENT without matching durable acceptance
   and dependencies cannot authorize serving or retention.
6. Revalidate the published pair/vector and retain a durable registry of snapshot,
   transfer, replica and transaction pins before advancing any retention boundary.
   Release apply gates; later apply again relies on the retained journal suffix.
   Retention advancement itself uses synced files plus atomic rename and parent
   sync. Crash before cleanup leaves extra state, never missing dependencies.

Catalog/session checkpoint contents replace journal evidence only when they
retain its full semantics. Immutable anchors keep SESSION, exact canonical
REQUEST_CLAIM bytes/digest, immutable REQUEST_BIND coordinator/participants,
terminal REQUEST_RESULT and decision dependencies. Session floors and closed
session tombstones have no wall-clock expiry. Consult them before path resolution
on retry. An unresolved claim blocks floor advancement; retired IDs never execute
again. Closing a session can release its live-session pin but cannot delete its
rejection tombstone. Initially preserve an anchor's minimal checkpoint/registry
storage even after namespace retirement; reclaiming it requires a verified
permanent rejection mapping in replicated configuration authority, not a local
control-file deletion. Affinity initially only grows as Transactions specifies.

GC is serialized with closure transitions/checkpoint/restore. Payload reclamation
requires zero logical references and live-generation pins, no unresolved prepare,
no retained checkpoint/replay/catch-up base requiring it, and no snapshot/restore
hold. Journal deletion additionally requires every assigned replica's durable
coverage or a pinned approved baseline plus suffix; partner RAM receipt is not
coverage. Replacing a participant's checkpoint alone cannot discard shared
decision/request evidence. Retain allocation tombstones and retired volume IDs.
If any dependency is unknown, preserve it and backpressure rather than guess.

## Snapshots and restore pairing

External tooling owns ZFS scheduling, holds and pruning. For a clean recovery set,
hold the closure checkpoint barrier above while it snapshots every actual data/
journal dataset covering the cut and records their snapshot GUIDs in an external
recovery manifest. Under the intended `volumes/V` layout, a recursive parent
snapshot can capture its two volume-owned siblings. The selected initial canary
profile instead explicitly enumerates the run-owned per-volume children
`hfsdataN/data/hfs-run-K/V` and `hfsdataN/journal/hfs-run-K/V` from the approved
manifest. The full seven-volume, three-replica clean set inventories 21 data and
21 journal datasets plus the three run control trees and dedicated coordination
evidence, as required by [Backup policy](Backup-policy.md#recovery-unit-and-consistent-cut).
Do not snapshot `hfsdataN/data`, `hfsdataN/journal`, the whole pool or another
shared ancestor containing unrelated runs. The data and journal run trees have
no single recursive parent that safely selects both while excluding other runs.
The separately admitted aggregate directory-backed alternative snapshots only
its complete, exclusively owned containing dataset under its own profile.
Include all closure
members, home payload dependencies, transaction/request evidence and a compatible
control/fence reconciliation record. Record the checkpoint/vector, dataset and
snapshot GUIDs, volume/root IDs, history, hashes, required suffix and holds.
A timestamp or common snapshot name alone is not a pairing proof.

Control is outside per-volume rollback. A separately captured control snapshot
is recovery evidence; it must be reconciled with current cluster/node identity,
admission, retirement and fencing authority before serving. It cannot roll back
fencing promises or restore stale ownership. Live snapshots are crash images and
need normal suffix/decision resolution; a data-only rollback or standalone
foreign-reference destination snapshot is not a self-contained restore.

Restore only into a new private writable destination, gated and unpublished.
Verify markers/bindings, snapshot GUIDs, paired checkpoint acceptance, complete
suffix/closure and authority. Restore or fetch missing dependencies and replay
before opening views. Missing history blocks serving; deliberate loss recovery
needs explicit authority and a new recovery generation. Never roll back a live
sibling beneath a daemon, boot/modify a received lab sink, or prune an external
hold to make capacity appear sufficient. See [lab replication](../Lab-replication.md),
[journal storage](../Journal-storage.md) and [recovery](../Recovery.md).

## Crash windows and negative cases

| Interruption or invalid input | Required outcome |
|---|---|
| Partial/unknown format marker, mismatched data/journal UUID, alias/overlapping root | Fail closed before publication; preserve evidence, never format or repair by guessing. |
| CREATE descriptor/name persists but commit is absent | Remain private; reconcile protected history before retaining/reclaiming staging. Do not publish from host readdir. |
| Crash during write/truncate or replay; payload retains torn/new bytes | Reconstruct from C plus complete exact-ID redo; no reads until all dependencies apply. Missing baseline/redo blocks recovery. |
| Rename/unlink committed; its old name now names a new object | Replay recorded parent/entry revisions and object IDs from C; never resolve the old path again or truncate/unlink its new occupant. |
| Repeated Link/rename/install after restart | Same decision/revision produces one entry/ledger delta; no doubled nlink, duplicate parent or second allocation. |
| One foreign entry installed, home ledger/parent update incomplete | Gate the closure, recover the bound decision, install all participants and visibility barrier before serving. |
| Replaced object has open/mapped handles; daemon restarts | Preserve original ID/payload/pins until old incarnation revocation is proven; handles never bind to replacement. |
| Data synced, catalog/acceptance/CURRENT publication interrupted | Old published checkpoint and suffix remain usable; retain orphan candidates and reconcile. No retention advance based on a temporary file. |
| CURRENT persists but closure peer pointer or durable dependency is missing | Keep closure gated; complete a verified vector or recover using a complete retained predecessor. Never serve one member alone. |
| Snapshot combines different histories, lacks a home, or restores stale control | Reject serving and request complete compatible evidence; no empty payload or stale-authority fallback. |
| Session result/floor was checkpointed and source path disappeared | Consult immutable anchor state first; return original outcome or RETIRED_ID, never re-execute on a new path occupant. |
| Quota/ENOSPC, checksum conflict or uncertain retention coverage | Backpressure/quarantine with evidence preserved; no silent acknowledgement downgrade or deletion of pinned data. |

## Implementation seams and remaining gates

The [component boundaries](../Platform-interfaces.md#component-boundaries) remain:
`fsservice` owns resolution/permissions/handles and alias state; `replication`
and `recovery` authorize history/decisions; `apply` coordinates ordered redo and
checkpoint barriers; `storage` implements the format over `internal/vfs`.
FUSE adapters translate capabilities/credentials and enforce invalidation; they
do not expose backing paths or implement a second namespace authority.

The current VFS has the offset I/O, truncate, file sync, mkdir, rename, unlink and
directory sync needed for payloads and immutable checkpoint publication. Before
store implementation, add narrowly scoped enumeration/stat and directory removal
capabilities for validated recovery discovery, descriptor/type checking and
bounded GC, with corresponding simulator semantics. A higher-level store should
supply strict codecs, exclusive identity creation, path-to-ID validation and
publication helpers; do not hide logical redo/transaction policy in raw VFS.
Replicated chmod/chown/timestamps, symlinks and logical links are catalog/journal
operations here, so host xattrs, native link/symlink and host metadata mutation
APIs are not prerequisites for them. Secure root/type/ownership validation and
initial private permissions still need backend/platform support. FUSE mmap and
locking need separate adapter/service seams; they cannot be inferred from VFS.

Open implementation questions for follow-on tasks are:

- Which catalog/record count and byte limits, checkpoint cadence and recovery
  reserve fit each admitted profile's measured resource budget?
- What exact shared journal/manifest wire schemas encode this contract, including
  deterministic field ordering, mandatory features and complete decision evidence?
- Which platform invalidation/fencing mechanisms can establish the required
  alias, mapping and stale-incarnation guarantees in each supported profile?

These do not reopen the identity/layout or transaction contract and require no
pending user choice for this specification. Model and verify full-suffix replay,
partial checkpoint/closure publication,
ledger/anchor retention, GC and authority races before enabling the real store.
Later separately approved kernel/guest tests must establish sync, open-unlinked,
permissions, mmap/alias caches and fencing behavior. This specification establishes
no runtime evidence; local documentation checks do not execute a workload test.
