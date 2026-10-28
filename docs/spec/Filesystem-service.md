# Single-volume filesystem service

Task `pq3` implements `internal/fsservice` over `internal/fsapi`, the journal
segment/record packages, verified apply and stable-ID storage. This is local
implementation evidence for disposable volumes. It is not a distributed
filesystem, a daemon deployment, a normal replication verifier or a kernel/FUSE
compatibility result.

## Admission and serving authority

One `Service` owns one volume's source writer, catalog and payload execution.
Multiple services can serve independently admitted volumes. `New` requires an
existing initialized owner stream from genesis, authoritative complete selected
history, fresh private data/journal reconstruction roots and a fresh workspace
UUID. Startup remains RECOVERING until complete replay and external readiness
checks succeed. It never accepts `CURRENT`, opens a dirty image as a baseline,
formats a source or repairs/truncates a torn tail. Suffix anchors and repair
callbacks are rejected by service configuration.

Normal construction requires explicit `Protocol` and `PermissionPolicy`
implementations. `Protocol.Check` must establish the configured exclusive
owner/incarnation, current partner/session readiness and visibility at startup,
before access and before successful completion. `Protect` establishes the real
commit/protection conditions and partner retention for an exact proposal and its
complete prefix/dependencies; `Authorize` independently verifies the full exact
history/selection proof before apply. Async delivery obligations are also its
responsibility. No production Protocol implementation exists here. Missing hooks
prevent normal construction; an unavailable hook blocks serving. Local durable COMMIT,
codec-valid authority fields or a caller flag cannot supply these guarantees.

`ValidationProfile{Disposable: true, Name: ...}` explicitly chooses a local-only
harness. It cannot coexist with `Protocol`. In this profile complete local
COMMIT and lifetime records select local effects on restart; an unselected
MUTATION remains invisible. It writes no fake ACCEPT, claims no external partner,
implements no emergency protocol and supplies no normal durability evidence.
Tests using an in-process protocol stub establish interface ordering only. This
profile is not wired into any production daemon or adapter.

The caller owns and closes all filesystem roots after `Service.Close`, excludes
administrative mutation/retention and concurrent writers, and admits real mount,
ancestor/descendant and alias safety. The service checks distinct root physical
identities, exact private root mode and UID. Source hierarchy preparation and
protected volume initialization are explicit external operations. Old workspaces,
progress images, source frames and allocation reservations are retained; no
checkpoint acceptance or garbage collection is implied.

## Command ordering, durability and reads

`fsapi.Filesystem.Dispatch` snapshots a bounded command and credentials before
returning admission success. It rejects full queues before admission. A single
worker orders all mutations, lookup/permission checks, reads, lifetime changes
and barriers. Context cancellation affects dispatch or a reply wait, never an
accepted command. Close stops admission and drains every accepted command.
External protocol hooks must use their own bounded deadlines.

A mutation chooses its exact internal operation UUID, raw names/object IDs,
append offset, timestamp and complete revisioned metadata/namespace afterimages.
It appends a MUTATION and a COMMIT candidate, then runs the covering local Flush
and external Protect concurrently. Both and complete selection authorization
must succeed before apply. COMMIT presence alone is no selection. Any uncertain
append, proof, apply, sync or progress error closes the gate as BLOCKED; queued
requests receive unavailable results, and resources remain for inspection.
Errors and abandoned waits never promise rollback.

`apply.Result.Advance` verifies the complete bounded extended source and exact
old source bytes/selected prefix, obtains authorization and installs only new
selected effects into the existing workspace. It preserves source-to-backing
slot mapping and selected-operation deduplication. Every installed effect uses
the existing payload/image/APPLIED barriers. Its lifetime progress budget is
cumulative. A failed Advance is poisoned. Restart always performs full genesis
replay into fresh data/journal roots, preserving optional prior progress; it
never reuses possibly changed payloads from an older image.

No read or permission check runs during partial apply. A queued read carrying a
known same-history Required cut waits behind the protecting/applying command
and reads only after that cut is installed. A read may include later committed
commands. A Required cut outside the current history is stale. A genuinely
unknown future cut returns unavailable; the single-owner worker does not wait
for a mutation queued behind that read. A follower/tail-wait facility is future
replication work. Successful reads recheck authority before completion.

Each mutation already meets the full normal boundary when the real Protocol is
present. File/data/directory Sync conservatively orders all prior volume
commands and checks current authority/partner readiness; it adds no fabricated
BARRIER/visibility evidence. Sync does not include later unsynchronized work.
O_SYNC/O_DSYNC writes therefore conservatively receive the same full ordering.
Release adds no data durability promise, although draining lifetime protection
requires its own protected HANDLE_RELEASE.

Protect receives owned newly authored frame bytes/references and selection.
Those frames exclude writer-owned OPEN/SEAL positions consumed by rotation. The
protocol must obtain and retain the complete ordered source, including those
frames and old/unselected dependencies, through its caller-configured source
access. Source bytes/names remain stable throughout Protect; local Flush runs
concurrently, and no next append occurs until it returns. Hook readers must
close before returning. A checksum, last sequence or two-frame receipt is
insufficient.

## Filesystem and lifetime contract

Lookup/getattr, create/open, sparse read/write/truncate, rename/link/unlink,
mkdir/rmdir, symlink/readlink, revision-bound readdir, setattr, logical statfs and
sync are implemented against the real stable-ID backing payloads/catalog.
Namespace names and symlink text preserve raw bytes. Names are single components;
symlinks are not followed by this API. Dot/dotdot lookup respects the full-volume
root; subtree-view policy is future adapter/permission integration.

Creation returns a lookup reference and Create also returns a handle. Lookup
returns one lookup reference; Forget drops its explicit count. Readdir is plain
readdir and does not silently acquire lookup references. Open and lookup first
protect a conservative home HANDLE_PIN using a separate internal operation UUID.
The pin drains only after all local lookup/open obligations have drained. Old
incarnation pins survive restart; this service does not infer fencing or reclaim.
Replacement and unlink retain original payloads and handle identities.

`Command.Generation` is mandatory for a handle or resumed directory cursor;
it must equal the issuing `Result.Generation`. Numeric handle values may repeat
in another generation, but the full reference cannot cross recovery. Readdir
uses a directory metadata revision and an exclusive raw-name cursor; a changed
revision/generation is stale. All reply buffers, directory pages, attributes and
capacity counters are independently owned by each Completion waiter.

Ordinary rename/link require identical mount UUIDs and return EXDEV otherwise.
Foreign-home operations fail closed. No internal transaction coordinator,
foreign-home closure, application Execute API or advisory lock implementation is
supplied. Those belong to `8w3`/`rq3`; the service never substitutes copies or
independent local commits for their shared visibility protocol.

Logical times use one owner sample per planned effect; reads preserve atime.
Creation honors mode/umask and the specified FreeBSD parent-group inheritance
regardless of the parent setgid bit. Directories also inherit parent setgid.
Non-root write/truncate clears set-ID bits; ownership changes clear set-ID bits.
Explicit timestamp choices and selected metadata are journaled. Combined
Setattr requesting set-ID mode bits together with UID/GID is explicitly
unsupported before journal mutation: the current record codec requires exact
requested mode and cannot also express induced set-ID clearing. Separate chmod
and chown, and nonconflicting combined changes, remain supported.

`PermissionPolicy` is mandatory in normal mode. `NewUnixPermissions(view)` supplies
version-one Unix enforcement for an explicitly admitted namespace view. The
policy receives independently owned caller credentials and a `PermissionContext`:
the original caller command, resolved object/source/replacement identities,
verified handle status and fresh involved attributes. Handle resolution and
append offset selection never overwrite the original command presented to the
policy. Successful authorization records the exact pre-operation object revisions,
view, policy revision, numeric UID/GID and sorted unique supplementary groups.
The existing journal intent stores the actual selected operation (for example,
an append's chosen offset or an open's induced truncate), not an added copy of
the whole adapter command in each authorization. Permission changes and induced
set-ID clearing use the ordinary protected mutation/apply/replay path. Replay
installs selected metadata without reevaluating old callers against today's
permissions. An explicitly configured validation profile can still omit the
policy for synthetic local tests; it does not enable normal mode.

Version one uses exactly one owner/group/other class, with primary and
supplementary group membership. UID zero alone bypasses discretionary access and
ownership restrictions; daemon UID, PID and account names grant no authority.
Lookup checks parent search, namespace changes check parent write plus search,
and rename checks both parents and sticky rules for source and replacement.
Moving a directory between parents also requires write on that directory for
its changed parent link. Readdir checks read; search is separately required to
resolve its children. The component API does not resolve a full pathname or
follow symlinks: adapters perform traversal with Lookup and retain the native
node/descriptor boundary. Stable-ID Open has no chosen hard-link pathname or
ancestor vector; the adapter/kernel must recheck search on every pathname walk,
including cached dentries after chmod. Earlier lookup IDs never replace those
fresh checks or grant permanent path access. Object-based stat and readlink
impose no file mode check, just as existing resolved vnode operations do.

Open and an existing Create check requested file access, including O_TRUNC.
Create of an absent name requires parent write/search; opening an existing name
requires parent search and file access. Access to a newly created file uses the
successful creation, even when its requested final mode excludes the creator.
Verified existing handles retain their granted read/write access after chmod
or descriptor transfer; handle mode and serving generation are still checked.
Handle-based truncate follows that descriptor access, while object-based
truncate checks current write access. Setattr checks current owner/privilege
even when supplied a handle. Release/Forget remain permitted for cleanup.

Only owner or UID zero may chmod. Setting group set-ID requires membership in
the file group unless privileged; sticky on a nondirectory requires privilege.
Only UID zero may change UID to a different owner. An owner may retain their
UID and change GID to a primary/supplementary group (or retain the current GID).
Explicit timestamps require owner/privilege; timestamp-now also permits current
write access. Non-root write/truncate clears both set-ID bits. Supplying UID/GID
metadata clears both set-ID bits, including privileged changes. Creation takes
the caller UID and always the parent GID, with or without parent setgid;
directories additionally inherit parent setgid. Explicitly requesting setgid
for a parent group outside caller membership is denied unless privileged, while
inherited directory setgid remains allowed. Symlinks retain mode 0777 and
chmod of a symlink is explicitly unsupported. ACLs, supplementary-group discovery,
capabilities/jail privileges and identity mapping are outside this policy.

These are explicit project rules, rather than a promise to reproduce every
FreeBSD filesystem option. Conservative deviations from the reviewed
[FreeBSD source at `96841ea08dcf`](https://github.com/freebsd/freebsd-src/blob/96841ea08dcf/sys/ufs/ufs/ufs_vnops.c)
include clearing both set-ID bits on every UID/GID request (including root and
unchanged ownership), and denying an explicit nonmember setgid creation instead
of silently stripping that bit. Symlink chmod and the conflicting combined
set-ID mode/ownership form remain unsupported as described above. The optional
FreeBSD suiddir ownership policy is not enabled; creation always uses caller UID.
These differences require explicit adapter behavior and compatibility tests,
not a claim of complete native parity.

## Identity and resource bounds

Allocation reservations live in a separate retained private root, outside every
reconstruction workspace. Before any creation journal candidate, an exclusive
`allocation-<allocation-uuid>.hfr` file is fully written, synced and parent-synced.
Its 104 bytes contain `HFSRSV01`, home/allocation/operation UUIDs, 16 zero reserved
bytes and SHA-256 of the preceding 72 bytes. Discovery verifies exact filename,
bytes, checksum, mode, UID, native link count and device; corrupt/partial evidence
blocks recovery. All source allocations, including unselected MUTATION/PREPARED
creations, and all reservations participate in collision checks. Neither new
workspaces nor selected catalog absence permits reuse. Command UUIDs also check
retained source/reservation identities; collision retries are finite.

Default queue size is 64; I/O is capped at 1 MiB per request, open handles at
1024, lookup references/reservations at 4096 and total logical bytes at 64 MiB.
Source/apply defaults retain their 4096-frame/64 MiB history and 256 MiB progress
budgets; backing catalog/receipt/pin limits also apply. Explicit finite settings
can adjust these caps. Statfs reports admitted logical object/data capacity,
not raw ZFS free space, journal staging space or replicated usage. Unknown
post-admission resource failures block rather than publishing partial effects.

Local tests cover OS temporary roots and VFS simulator crashes/restarts,
independent payload/name/attribute assertions, old handles after replacement,
raw bytes, sparse ranges, directory accounting/ancestry, barriers and delayed
Required cuts, permissions/proof gates, canceled waits, bounded queues,
allocation collisions after unselected creation and journal/apply/progress faults.
These are local contracts; native FreeBSD kernel, FUSE, real partner retention,
failover, ZFS power loss and lab operations retain their separate gates.
