# Controlled host transport protection (C8)

`NewDurableTransport` exclusively creates a disposable private root. Runtime
assembly must explicitly select it; `NewTransport` remains the memory-only
component constructor. Neither constructor restores an endpoint. C8 alone provides no native admission
or physical exclusion authority. The [C9 private coordinator](../controlledruntime/restart.md)
reconciles actual complete ledgers and fresh physical continuity for a barriered
same-owner component restart. Guest RESUME and replacement native owners remain
blocked. Older records without complete bridge-session rows are inspection only.

Creation pins the naming parent, uses Mkdirat/Openat for the exact new child,
and retains that parent descriptor until the initial typed ledger has committed.
The actual parent directory is synced before workers start or the constructor
returns success; sync failure preserves the root and attached-service refusal.
The root contains only `owner.lock` and `transport.db`, both private regular
single-link files. Opens pin the directory descriptor, reject symlinks and
foreign/nonprivate ownership, and take the ownership flock plus bbolt's database
lock. Existing, failed, missing-file and unknown-file roots are never fresh
issuance. Close joins workers and closes the database; it does not retire cells,
claims, source rights, credits or physical domains. Resources are preserved.

The versioned index records the actual service controller and endpoint, exact
scope/selected protocol tuples, high-water, ordinary/control counts, complete
sorted live IDs and workspace presence. All IDs through High outside that live
set are released: this encodes the exact released floor and holes in bounded
space. A replacement workspace is the unissued High+1 candidate. No historical
ID is recycled and no per-released-cell tombstone grows indefinitely.
Guest cell freshness is indexed per session by its guest highwater, the bounded
recent issuance markers and the optional `GuestHoles` (unissued guest cells in
the 16-cell reorder window below the highwater that may still arrive out of
order once; see [mapping](../../docs/spec/Retained-9p-mapping.md)). An admitted
guest cell is never admitted again.

Each cell/workspace row owns its original header and complete body, chunk bitmap
and historical native observation per accepted chunk, class, exact decoded
command and command observation, job phase, immutable response and wire frames,
byte-level read bitmap, unread count and response channel. Headers do not retain
borrowed callback Data. Historical Native values remain evidence: they cannot
create VerifiedRequest, bypass live Authenticate, or authorize a callback.

The transport mutex serializes protection. One synchronous bbolt transaction
commits the index and changed cell/workspace rows, including reclaim deletion.
Write commits before returning accepted bytes or enqueueing a job. Workers
protect `executing` before calling the existing authenticated Service.Execute;
response bytes commit before readers can see them. Read commits coverage before
returning newly seen bytes. Reclaim commits before releasing old cell ownership.
An error or uncertain commit fences all subsequent traffic and dispatch; a
surviving ledger is not automatic recovery. Rejected replacement candidates
retain already acknowledged chunks in the single funded workspace. Complete
commands that encountered a full queue can retry dispatch using the same bytes.

The cell's command/native/phase is the durable owner of accepted, executing,
retry-pending and blocked work; queues/rings are live scheduling structures.
Inspection never requeues jobs or re-runs Prepare. C9 must first reconcile each
protected semantic effect with its actual authoritative ledger and reauthenticate
any preserved work. A close or process loss may leave `executing` even when an
external service ledger has protected the effect; that is deliberate uncertainty.

WouldBlock/NoSpace refusals stay `pending` and are retried, unless the caller
wrapped them with `guestbridge.Final` because the identical command cannot
succeed later. Any other Execute error makes the cell `blocked`: it is never
redispatched, and every Read of it returns `fsapi.IO` (EIO through FUSE, an
Rbridge ERROR to the guest) instead of WouldBlock, so the guest's FETCH fails at
once rather than retrying AGAIN until its 30 s deadline. A successful Execute
whose bytes cannot be framed is blocked the same way with class
`RESPONSE_EMPTY`, `RESPONSE_TOO_LARGE` or `FRAME_ENCODE`; no cell stays
`executing` without a reply. The row records `BlockedError`, the semantic fsapi
errno name (for example `PERMISSION`), one of those response classes or
`UNCLASSIFIED:<Go type>`, bounded to 96 bytes and never the error text. The field is omitted for every other phase, so older rows and
non-blocked rows keep their exact bytes, and a legacy blocked row without it
still validates. Readers built before the field existed decode rows strictly
and refuse only rows that carry it (blocked cells, which are never restorable).

Serialization uses per-cell deltas, never a full transport JSON copy or an append
history. There are at most 80 live rows and one workspace, each encoded row is
limited to 8 MiB, the index to 128 KiB, and one transaction puts at most two cell
rows (16 MiB). Default bbolt synchronous transactions remain enabled; AllocSize
is 1 MiB and no read transaction spans writes. The writer refuses a transaction
when its actual retained database descriptor is already at least 768 MiB. This
is an operating ceiling, not a proven hard physical file-size bound: bbolt can
grow while committing. Oversized inspection refuses before mapping and all
uncertain failures preserve the files and fence admission. The current bbolt
version has no MaxSize option; no upstream dependency is changed.

Durable construction additionally reserves `TransportLedgerMemoryBytes` (1 GiB)
in admission.Memory for bounded database mappings/freelist and serialization/
transaction copies, in addition to 512 MiB ordinary, 128 MiB control and the
original single 12 MiB parser workspace. This is accounting, not eager allocation
or proof of physical pressure behavior. Unfunded construction refuses before
creating a root. Future w04/xz3 profiles must fund these costs and the unchanged
4 MiB canary and guest limits, or refuse; no lab capacity increase is implied.

`InspectTransport` takes exclusive ownership and validates one bounded row at a
time, returning a digest of exact index/row bytes. A callback retaining decoded
rows needs separately funded memory. Partial callbacks followed by an error are
never complete evidence. Inspection creates no live object, restores no workers
and makes no native, physical or cross-ledger continuity claim.

Local tests exercise actual private files and real process exits without Close
at accepted chunk/workspace, protected response, exposed read, committed reclaim
and executing-job boundaries. They check exact original bytes, read ownership,
identity/class/high-water and nonreuse, failed protection and read fencing,
private/second-owner refusals, bounded row reuse and damaged-row refusal. Fixture
native identities and HELLO responses are portable component primitives. No
FreeBSD mount, guest kernel, deployed runtime or physical-pressure acceptance is
claimed by these checks.

Complete per-scope bridge-session rows share each response/protection transaction. Their aggregate encoding is bounded to8MiB, with ordinary admission reserving maximum control-closure growth before protected effects; no row/index/file or1GiB memory allowance is increased.
