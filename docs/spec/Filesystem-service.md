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
responsibility. No production implementation exists here. Missing hooks prevent
normal construction; an unavailable hook blocks serving. Local durable COMMIT,
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

`PermissionPolicy` is mandatory in normal mode and receives fresh credentials,
original command and involved object/parent/replacement attributes. Its view and
policy revision are captured with exact credentials/object revisions in journal
authorizations. Permission rules, privilege checks, sticky directories, creation
special-bit authorization and lock semantics remain `qq3`/`rq3`; a privileged
backing daemon grants no implicit normal access. An explicitly configured
validation profile can omit the policy for synthetic local tests.

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
