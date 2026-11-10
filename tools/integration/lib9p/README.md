# Owned lib9p / bhyve backend (`uz3`)

This directory maintains a dedicated raw retained-9p backend and an owned patch
for FreeBSD source revision `aadd58dddcbc78f4d5594827b46b5633552b15ce`.
`upstream.json` pins the exact original inputs and SHA-256 digests. Nothing here
is installed, selects a live exporter, mounts FUSE, boots a VM or enables normal
publication. The stock lib9p filesystem backend and generic virtio-9p exports
keep their existing path. A dedicated `hfs-controller` export refuses `path`,
read-only fallback, stock attach/walk requests and missing controller authority.

The backend receives exact 9p packets before the stock lib9p fcall decoder. It
accepts only exact `9P2000.L.hfs1`/65536 negotiation and types 240/241 afterward.
Each device connection is bound by the privileged controller to an already
installed session Context. All such connections share the real native endpoint's
allocator and forwarding ledger. Guest cell1 in two sessions has independent
forward/reverse bindings; FETCH is implicitly scoped by the connection.

## Implemented boundary

* C canonical ASCII JSON tokenizer rejects duplicates, unsorted keys, invalid
  escaping/numbers, excessive depth/tokens and trailing bytes. Complete guest
  bodies, SHA-256 and exact Context are validated before any native forwarding.
  The backend checks the closed common envelope, current credentials/full sorted
  groups1024, claim/intent hash, opcode/Args field sets, WRITE max_end, mode07777
  and umask0777. It derives ordinary/drain flags from the complete parsed opcode,
  including opcodes after a large data field. Exact retained same-session donor
  cell/hash/receipt state determines reclaim and RELEASE_CELL's inherited class.
  Flags are zero on both guest directions. **The authoritative host parser still
  validates complete nested capability/time/view/result unions and Unix policy
  before effects.** The exporter interprets none of them as host POSIX requests.
* One native writer preserves exact guest/native bytes, body cell rewriting,
  chunk/hash identity and monotonically increasing endpoint cells. Lost/short
  native writes retain the identical candidate; uncertain candidates exclude
  another allocator. The exception is an original cell whose final chunk is
  refused permanently, which fails back to the guest (`e64` for donor sends,
  `p64` for the rest, `w64` for the final-chunk rule).
  EAGAIN on the first chunk of a multi-chunk non-donor write consumes no
  native number and lets control/replacement traffic progress; a single-chunk
  cell refused EAGAIN stays pinned (`p64`). All result reads accumulate bounded
  short preads and verify kind/flags/session/native cell/body hash and context/op
  before reversing the mapping. Endpoint mutex contention returns an exact
  wire AGAIN for FETCH or ERROR/CAPACITY for an uncopied SEND packet at any
  chunk. The client retries identical bytes without advancing; a command already
  copied before native resource pressure remains COPIED. This never asserts
  semantic no-effect for an earlier admitted claim. Tversion exact selection
  uses connection-local atomic state and remains available during allocator
  contention. Native transport receipt never means syscall success. Query, cancel, FileTooLarge and terminal claim results remain actual
  opaque host results; the backend invents no execution outcome.
* Positional FUSE descriptors are supplied by authenticated supervision, never
  opened from a guest path. FreeBSD construction/restoration checks fusefs (including nonempty fusefs.<subtype>),
  exact distinct inode IDs, size2^52, O_DIRECT/nonappend/access flags, confines
  descriptors with Capsicum, and authenticates the actual native endpoint and
  supervised exporter snapshot on every native read/write. Other platforms
  refuse production construction. Guest credentials never replace exporter
  process credentials or invoke host account/group lookup.
* One independent polling thread per endpoint and nonblocking native callbacks
  implement VIEW_WAIT's fixed1000ms deadline, at most16 ordered VIEW_POLLs,
  same-wait EMPTY replacement chain and immutable final RECALL kind3. EVENT
  stops polling immediately. The deadline cannot create EMPTY for an unknown
  poll. Earlier exact EMPTY request/result bytes and acknowledged mappings stay
  owned through the guest's final acknowledgement. Separate command/result/
  recall fds avoid a shared native vnode. No ioctl or unique native fh is used.
* There are 64ordinary/16control credit-owning entries per endpoint. New
  assemblies/candidates are bounded to17 aggregate,16 per session and16 using
  ordinary tags; the one extra slot requires a reserved control tag. A wait
  preparing its next native poll still owns its existing control credit through
  its retained poll trail, even while that next poll is unissued or queued
  behind an uncertain writer. It does not become a new assembly/candidate.
  Admission, protected snapshots and recovery use the same classification and
  limits. A full parsed command still determines native flags/class.
  Owned bytes are capped at64MiB, with ordinary traffic capped at48MiB; resource
  pressure backpressures before another allocation. Serialization temporarily
  duplicates owned bytes; persisted ledger inputs are capped at65MiB. Sessions are capped at16, contexts at4096bytes,
  VIEW_WAIT commands at16384bytes and native cells at2^31-1. Session-local guest
  cells may arrive out of order, but each session's originals are forwarded in
  guest order (see `e64` below). A bounded 256-range seen-cell ledger per session
  retains permanent nonreuse evidence, backpressuring before another disjoint
  range would exceed its budget. Exact active retries and arbitrary chunk order
  are supported. Retired cell numbers never reuse.

## Durable transport ledger and external facts

`hfs9p_ledger.c` provides a real local file adapter: authorized preopened state
directory, lifetime exclusive flock, bounded one-component filenames, nofollow
0600 files, exact checksummed bytes, fsync(temp), atomic renameat and fsync(dir).
Failed `.next` bytes remain for inspection. Protect the complete exact candidate,
old donor snapshots, sessions, allocator, pending holes and poll trails **before**
any native write. Recovery retries that protected candidate unchanged rather
than guessing whether a lost receipt admitted it. A checksum detects corruption;
it never supplies authority. Missing bytes, wrong endpoint, lost lock, failed
sync, old/new endpoint mismatch and unreconciled host ledgers refuse progress.

The controller must combine this concrete local adapter with three mandatory
facts/callbacks in `Hfs9pVerifier`: claim a genuinely fresh endpoint or verify the
exact protected recovery digest against the surviving host ledger; authenticate
the actual supervised exporter/FUSE tuple on every I/O; protect bytes using the
local adapter (or equivalently durable admitted transport storage).
The strong `hfs9p_controller_claim` is now supplied by the owned C supervisor
adapter. The [C10 runtime](../../../internal/controlledruntime/runtime.md)
registers explicitly dedicated pending devices, then one bounded asynchronous
worker consumes inherited fd6/fd7 inputs and calls the strong explicit-tuple
startup once for up to16 signed sessions on one endpoint. Pending/failed queues
return bounded tagged Linux-errno retries/errors and never fall through the stock
filesystem backend; the VM remains alive on failure.
Trusted startup supplies an authorized parent
fd, an exclusive new directory name and installed scope/protocol. There is no
startup path from guest bytes, named management socket or configuration boolean.
It receives the inherited fd3 tuple, durably records actual process/socket/fd
ownership, claims once, verifies, then constructs the native backend. The
constructor consumes the one-use claim witness. Its optional paired I/O hooks
serialize fresh fd observations and each actual syscall against a one-second
VERIFY heartbeat (one bounded 128 KiB stack). Each observation uses actual
fstat/fstatfs/F_GETFL and exact Capsicum rights; F_GETFL-only CAP_FCNTL survives
attenuation. Linux startup refuses before files or CLAIM. Restoration remains
blocked: no nonzero digest can be claimed by this adapter before C9.
A configuration flag, a checksum or an empty state directory cannot
substitute for host endpoint continuity. `hfs9p_restore` restores transport state;
it does not turn host RESUME into RECONCILED. The host component still keeps
RESUME BLOCKED until its claims/capabilities/cache-domain ledgers are recovered.
Disconnect deletes only the per-device connection object and preserves endpoint
state. Deleting a busy endpoint refuses; cleanup is never claim retirement.

The maintained Linux consumer must include `i04` to retry SEND/CAPACITY at
nonzero chunks; the old first-chunk-only retry path is incompatible with this
packet backpressure.

Remaining owners: `xz3` supervises/builds/pins the actual installed backend and
native enrollment facts; `f04` supplies the controlled local service/provider;
`g04` supplies separately versioned normal public bindings; `zq3` proves the
actual FreeBSD FUSE/virtio/guest stack, native credentials, private-inode hiding,
cache barriers and installed/running bytes. No normal3 or runtime proof is
fabricated here. Any-origin writers and same-mount advisory locks remain the
selected design.

## Reproduction and evidence scope

`python3 tools/integration/lib9p/prepare.py NEW_DIRECTORY` downloads and verifies
only the pinned modified upstream files, applies the owned patch without fuzz,
and copies the owned C inputs. It refuses an existing destination. This creates
a reviewable **subset**, not a full FreeBSD tree or full bhyve artifact. To build
that artifact, a separately owned full matching source tree/generated build
headers are needed; no source/package installation is done by these scripts.

`bash tools/integration/lib9p/verify.sh NEW_DIRECTORY` runs sequential strict C
object compilation, production API link/refusal tests, ASan/UBSan parser/backend
transport fixtures and real temporary-file ledger tests. `native-verify.sh`
compiles the same production source and tests with FreeBSD libmd/pthreads; a
frozen fixture header must be placed beside the tests before transfer. Native
resource limits and transfer are owned by the root operator. It uses no mounts,
SSH, VM operation or installation itself.

Transport fixtures replace only native fstat/pread/pwrite and privileged facts;
they supply no filesystem executor or publication evidence. They test real C
framing, complete parsing, native offset/chunk/short-read behavior, immutable
mappings, 80-cell release/replacement, repeated EMPTY/event handling and exact
lost-receipt recovery. Separate tests use actual temporary files, exclusive
locks, process exit, fsync/rename/reopen and injected sync failure. Production
API tests link the actual production objects and prove raw files/missing native
enrollment refuse. These scopes do not prove native direct-I/O kernel behavior,
a complete patched bhyve artifact, installed-byte binding or actual guest I/O.

Component evidence is in [`evidence/20261103/`](evidence/20261103/).
Historical frozen v6 and v7 passed Linux GCC16 ASan/UBSan and native FreeBSD15.1
clang19 production-object compilation/linkage plus all three component tests.
Native v7 took5.07s, peaked at141852KiB RSS and swapped zero pages. Earlier v2's
feature-macro build failure and v3/v4/v6 historical passes remain recorded.
The R1 correction separates new candidates from credit-owning poll transitions
and makes the native transport fixture enforce immutable replay bytes/metadata.
The corrected frozen v8 passed the same native checks in5.42s with151332KiB
maximum RSS across compilation/tests and zero swaps. Its exact source and
artifact bindings are recorded in summary.json and the retained receipts.
The full patched bhyve device was not compiled: f3 has no populated matching
source or generated bhyve build headers. That artifact and actual installed/
runtime acceptance remain gated as described above.

Explicit bridge1.1 enrollment uses `hfs9p_add_session_protocol(..., 1)` and exact
`9P2000.L.hfs1.1` Tversion; existing `hfs9p_add_session`/minor0 retain the old
exact token. A connection copies the immutable enrolled minor before lock-free
Tversion selection. Frame minor, canonical protocol tuple and host reply must
match. New lease/reservation opcodes route to the ordinary acquisition lane or
reserved control lane; the host validates the full closed semantics before any
effect. Incomplete ordinary1.1 assemblies stop at12 per session, reserving four
of16 per-session candidates for control.

Any1.1 session selects H9LEDG2 protected snapshots with explicit per-session minor;
pure1.0 snapshots remain H9LEDG1. Mixed sessions retain their individual versions,
HELLO state, exact bodies and native mappings on restore. Retained body protocol
is checked against persisted session minor, including native replies and poll
trails. The local durable ledger accepts both exact magics and rejects2->1
replacement, including after reopen. A checksum cannot authorize a version
change, endpoint reset, missing old ledgers or source continuity. The supervisor
still verifies installed/running bytes and actual host enrollment; this change
authorizes no deployment or activation. See
[the closed1.1 contract](../../../docs/spec/Retained-9p-view-leases.md).

Controlled successor forwarding (`o04`) additionally accepts explicit minor2
`9P2000.L.hfs1.2` and minor3 `9P2000.L.hfs1.3`, with exact closed tuples
schema3/features4095 and schema4/features8191. The session ledger pins the
selected minor; mixed frame/body/version inputs refuse. ALLOCATE uses ordinary
capacity; GAP_RESOLVE and minor3-only READDIR_CLEANUP use reserved control.
ALLOCATE/GAP_RESOLVE carry the unchanged full ordinary Call and refuse typed
DRAIN_WRITE. Host response verification bytes survive translation unchanged.
They remain bound to native command/cell/response coordinates: task `y04` must
provide an authenticated translation contract before the missing C6 consumer
can claim guest source/event/floor verification. Existing native construction
and controller-provider refusal remain mandatory.

C4 local verification adds real inherited-socket framing/SCM_RIGHTS tests and
adapter ledger callbacks with exclusive writer and retained fsync failure bytes.
These are portable primitives, not native enrollment proof. Source cross-compiles
for x86_64 FreeBSD with Zig and the maintained FreeBSD SHA256 header; no native
runtime, FUSE mount, complete bhyve artifact or installed acceptance ran here.
All failed startup files/ledger candidates remain owned for inspection. The
trusted exporter must never fork, re-exec or pass its management/data fds after
enrollment; fd3/data are CLOEXEC and inherited executable fd4 is closed first.

CONTROL fd3 and RECOVERY fd5 share one admission (`_parent_socket`): a
connected stream whose local and peer addresses are both unnamed AF_UNIX, with
the parent as peer at our euid. Linux reports an unnamed socket as just
`sun_family` (2 bytes), and only that form passes there, so abstract names stay
refused. FreeBSD returns its 16-byte `sun_noname` placeholder with an all-zero
`sun_path`. FreeBSD has no abstract namespace and cannot bind an empty path, so
there (and only there) all-NUL path bytes also count as unnamed. Any nonzero
path byte is refused. Task `m34` fixed the older rule, which allowed at most
`offsetof(sun_path)` bytes and so rejected real FreeBSD socketpairs. That old
rule failed the l34 native run (`!_recovery_socket()`, see
[l34](../../../docs/lab/results/20261108-l34-native/README.md)). Evidence
recorded before m34, including the l34 receipts and the xz3 quarantined
artifact, describes the old sources. `supervisor_test.c` checks both address
forms as pure cases on every platform, plus real socketpair, datagram,
pathname and Linux abstract sockets, and fd5.

## Guest order and permanent refusals (`e64`)

zq3-r6 finding R6-2: mount-0's VIEW_WAIT guest cell 48 reached the runtime as
native 108, after QUERY guest cell 49 as native 107. The runtime refuses an
original at or below the session's captured guest high-water, so it refused
cell 48. The exporter then pinned that donor send as its pending writer for
good, and both sessions stalled until the guest's 30 s view-control timeout.

**Why cells were reordered.** The guest allocates a session's cells in order
under its writer mutex. When a SEND gets CAPACITY (this backend's endpoint
mutex is busy, for example with the 4 ms wait worker) or EBUSY (task 964's
bounded wait for a fixed tag), the guest yields the writer for 10-12 ms before
it retries. A later cell can then be copied completely first. The exporter
forwarded each cell when its own SEND completed, so native order followed
arrival. It could also reorder by itself: a complete cell blocked by another
candidate's writer pin or by credit was retried only by its own FETCH or the
wait worker, while a later cell's SEND or FETCH could find the writer free
first.

**Ordering.** Native numbers now go to each session's originals in guest
order (`_guest_order` in `src/hfs9p.c`):

* A complete lower original is forwarded first, lowest first, in the same
  call. If another cell's pinned writer blocks it, the later cell is blocked
  by the same pin, and the first call after the pin clears forwards the lower
  cell first, so a pin never lets a later cell overtake.
* A lower cell that was never seen, or is still incomplete, is waited for, at
  most 1 s (`HFS9P_ORDER_HOLD_MS`), so that an abandoned or never completed
  cell cannot stall its session. After the hold the cell goes anyway.
* A complete lower cell that cannot go for its own reasons (no ordinary
  credit, EAGAIN on the first chunk of a multi-chunk cell, an error before
  its write) is not waited for.
  Control, replacement and donor traffic keeps bypassing ordinary traffic, as
  before e64.

A bypassed or given-up cell that arrives below the session's high-water relies
on the runtime's guest reorder window (task d64): the runtime admits an
unissued guest cell once while it is inside 16 cells below the high-water
(`GuestHoles`, tested by `TestMappedOutOfOrderGuestWaitAcceptedReplayRefused`
and `TestMappedGuestReorderWindowBounded`). A cell is still refused as stale
once a cell 16 or more guest numbers above it was admitted, so the window
stays a bounded fallback. Since `p64` such a refused cell fails back to the
guest instead of pinning the writer (see below). The R6-2 shape (QUERY 49 copied before VIEW_WAIT 48) is a
missing lower cell, so 49 waits for 48. Replacement polls and replays of an
existing candidate never wait. The floor and hold deadlines are not
persisted: restore puts the floor below the lowest restored original that is
not issued, or at the session's highest seen cell. 964's tag-reuse wait and
564's recovery busy retry are unchanged.

**Permanent refusals.** Only an original donor send (reclaim, poll index 0,
not SESSION_RETIRE) is classified. The runtime builds such a send in its
reclaim workspace and advances its native high-water only on admission, so a
refusal consumed no native number. These native write errnos, on the final
chunk only (`w64`, below), fail the cell back to the guest (the SEND or FETCH gets an ERROR reply, later FETCHes and
resends get ESTALE through the seen ledger, the donor entry stays, and a
fresh snapshot is protected so that recovery does not replay it):

| errno | runtime source | permanent because |
|---|---|---|
| ESTALE | `fsapi.Stale` | only on a first attempt since `p64`: an identical resend of an admitted cell returns the full length, but authentication can return Stale before the cell lookup (see `p64`) |
| EINVAL | `fsapi.Invalid` | the same; a protection failure that wraps Invalid also fences the runtime transport |
| EACCES | `fsapi.Permission` | only on a first attempt: authentication, node and scope checks run before the cell lookup |

EPERM is never produced (the FUSE adapter maps `fsapi.Permission` to EACCES),
so it stays uncertain. A first-attempt EACCES can also come from a transient
failure of the FUSE adapter's per-request credential snapshot
(`PrivateControl.native` in `internal/fuseadapter`). Failing that cell back is
safe, since nothing was admitted, but it drops a cell that a retry might have
delivered. EAGAIN is transient. ESTALE or EACCES on a replay, EHOSTDOWN,
ENOSPC, EIO, EINTR and every other errno are uncertain, because a protection
or transport failure may follow admission. All of these keep the exact
candidate pinned and protected, as before. Non-donor cells are classified
separately (`p64`, below), because the runtime allocates their native number
at the first chunk, before validation.

**Runtime workspace (fixed by `n64`).** Before n64 the runtime kept a
refused donor frame in its single reclaim workspace
(`internal/guestbridge/endpoint.go`, `Transport.Write`), so every later new
native cell, from any session, got WouldBlock until that exact frame was
admitted. The runtime now frees the workspace when it refuses the complete
send ESTALE, EINVAL or EACCES (`internal/guestbridge/refusal.go`), and the
next cell takes the unspent number. EAGAIN keeps the workspace for the
replay, and so does any refusal of a SESSION_RETIRE or a continuation poll,
which this exporter always pins. A replay resends every chunk from the first, so it rebuilds the
workspace and revalidates the identical bytes.

`backend_test.c` covers ordering behind another session's pinned writer,
eight concurrent SENDs of one session, the R6-2 gap shape with hold expiry and
a late cell, a control bypassing an ordinary cell that has no credit, permanent refusals (ESTALE, EINVAL, first-attempt EACCES) with a
real ledger reload, and transient or uncertain refusals (EAGAIN, EIO,
EHOSTDOWN, EPERM, replay EACCES) that stay pinned and then replay unchanged.
The transient test is a no-change guard; it also passes on the pre-e64 code.
The fixtures prove exporter behaviour only. A native rebuild of the bhyve
backend must pick this source up before any lab run can show it.

## Permanent refusals of non-donor cells (`p64`)

Found in the e64 review: the exporter can bypass a complete ordinary cell (no
credit, EAGAIN on its first write). If that cell is written only after a cell
16 or more guest numbers above it was admitted, the runtime refuses it as a
stale guest cell. Before p64 the exporter pinned every non-donor refusal, so
that cell stayed the pinned writer for good and every session stalled: the
R6-2 symptom in another shape.

**What differs from a donor send.** The runtime allocates a non-donor cell's
native number and credit at its first chunk, before any content check
(`Transport.allocateCell`). Before `n64` it kept a refused cell in its table
for good (incomplete, never dispatched, still holding its credit); since n64
it removes it with its credit, but the number stays spent. A fail-back
must therefore know that the number was spent. If the exporter guessed wrong,
the next cell would reuse a spent number or leave a gap in the native
numbers, and the runtime would refuse it and every later cell. So a refusal
fails back only when it is permanent **and** shows that the number was spent,
and since `w64` only on the final chunk:

| errno | when | permanent and spent because |
|---|---|---|
| ESTALE | first attempt only | it comes from the guest freshness check or a duplicate guest cell (`prepareMapping`, after allocation), or from a native number at or below the runtime's high-water (spent already) |
| EINVAL | after an earlier chunk of the same write was accepted | that chunk allocated the number; an identical resend of an admitted cell never returns Invalid |

Everything else keeps the exact candidate pinned, as before:

* ESTALE on a replay. Authentication runs before the cell lookup, and on
  FreeBSD the runtime's native connection check returns `fsapi.Stale` when
  the mount on the endpoint path changes (`nativeConnection.check` in
  `internal/controlledruntime`). The adapter's credential snapshot runs the
  same check first and maps its failure to EACCES, so this needs the mount to
  change between two checks of one request, but a replay of an admitted cell
  could see it. A first attempt can see it too: the mount changes before the
  second or third `nativeConnection.check` of one request. Its number was then
  not spent, but p64 still fails the cell back and moves the high-water past
  it, so the next cell's first write is refused EINVAL for the gap and stays
  pinned for good. The race is narrow but real. The same race could
  make a donor send's replay ESTALE an admitted send, so p64 also narrowed
  e64's donor rule: ESTALE fails a donor send back only on a first attempt.
* EINVAL on the first chunk: a content refusal after allocation and a frame
  or numbering refusal before it look the same.
* EACCES: an authentication refusal before allocation (possibly transient,
  `PrivateControl.native`) or a content refusal after it.
* EAGAIN, EPERM, EHOSTDOWN, ENOSPC, EIO, EINTR and every other errno, as for
  donor sends.

A failed-back non-donor cell is handled like a failed-back donor send (ERROR
reply, ESTALE afterwards through the seen ledger, fresh protected snapshot),
and the native high-water moves to its spent number. Since `n64` the runtime
has removed that cell with its ordinary or control credit (see below). The guest
(`hfs_control` in the linux9p 0003 patch) treats the ERROR like any uncertain
transport result: it keeps the cell's slot reserved, so it does not reuse the
credit either, and blocks the session's unreserved traffic (an ALLOCATE only
marks allocation uncertain) until it reconciles. Other sessions proceed.

**Runtime cleanup of refused cells (`n64`).** Before n64 each failed-back
non-donor cell left a stuck runtime cell: incomplete for good, holding an
ordinary or control credit that the exporter no longer counted, counting
against its session's incomplete limits (16, and 12 ordinary), and for minor 4
keeping every later issuance marker of its session from being pruned. Once k
stuck cells used up the credit the exporter believed free, or 12 filled a
session's incomplete limit, the next fresh cell got WouldBlock before
allocation; a single-chunk cell refused that way was pinned (see below) and
pinned the writer for good, and a SESSION_RETIRE could not pass it. For minor
4 fresh admission closed for every session after about 80 more admitted cells
of the stuck cell's session (`pruneGuestRecent`, `mappingRecentLimit`).

The runtime now mirrors the fail-back (`internal/guestbridge/refusal.go`). It
removes a complete cell that validation refuses Stale (except a
SESSION_RETIRE, whose Stale depends on later state and which the exporter
pins), or Invalid on a chunk after the first, and returns its credit. The
number stays spent, and the protected transport index records the latest
such refusal (`Refused`, omitted until one happens), so a resend of that cell,
also after a restore, gets the same errno, while an identical resend of an
admitted cell still succeeds. The removal and the record are protected with
the refusing write, and since no incomplete row stays behind, Suspend and
restore work after a refusal. Refusals the exporter keeps pinned (EACCES,
first-chunk EINVAL, EAGAIN, ENOSPC) keep the runtime cell too, so their replay
revalidates the same bytes.


**WouldBlock before or after allocation.** The exporter used to drop any
first-write candidate refused EAGAIN and let later cells take its number.
`Transport.Write` returns WouldBlock before allocation (credit, incomplete
cells, recent markers, an occupied reclaim workspace), but it validates a
command only once all chunks are in. A single-chunk cell is validated in the
same call, after allocation, and can then get WouldBlock too: its dispatch
queue is full (the 16-slot drain queue and the 64-slot acquisition queue are
smaller than the 80 credits that can reach them), or the recent-marker limit.
The exporter cannot tell the two apart: the errno is the same, and a
result-node read of the number returns WouldBlock both for an allocated cell
without a response and for an unallocated number above the high-water. Before
p64 a later cell then took the spent number and was refused EINVAL for good.

So only EAGAIN on the first chunk of a first write of a multi-chunk cell,
which can only come before allocation, drops the candidate. A replay never
qualifies, including a candidate restored as pending after an exporter
restart (it was protected before its write, so its uncertain flag is
clear): if an earlier attempt completed the runtime cell, the identical first
chunk revalidates it and can get WouldBlock after allocation. A single-chunk cell refused EAGAIN
stays pinned and replays its exact bytes with the same number: if the number
was allocated, the resend revalidates the stored cell, and if not, it
allocates the number again. The trade-off is that such a cell no longer lets
control or donor traffic bypass it while the runtime refuses it, and a
refusal that never clears blocks the writer. In a healthy runtime the
exporter's own credit check runs before any write, so a pre-allocation
WouldBlock needs runtime state the exporter does not mirror. Before `n64`
that was mainly cells failed back after allocation, which kept their runtime
credit and counted as incomplete; the runtime now removes them (above).

**Exposure.** No change was needed to prefer the lowest bypassed cell:
`_guest_order` already offers every complete lower cell of the session the
writer before a later cell of that session takes a number. Control and donor
traffic still overtakes a lower cell that cannot be written.

`backend_test.c` covers a bypassed ordinary cell refused ESTALE that fails
back (the writer is released, another session proceeds, the spent number is
not reused, and a real ledger reload no longer has the cell), a multi-chunk
cell refused EINVAL after its first chunk, and refusals that stay pinned and
then replay unchanged (EIO, EHOSTDOWN, EPERM, EACCES, first-chunk EINVAL, and
ESTALE on a replay), a single-chunk cell refused EAGAIN after allocation that
stays pinned and replays with its number while a later control waits, and a
donor send whose replay ESTALE stays pinned (in `refusal_transient_test`).
`refusal_restart_eagain_test` restarts the exporter from its real ledger
with a pinned multi-chunk candidate whose replayed first chunk gets
WouldBlock again; it stays pinned with its number.
`refusal_donor_replay_einval_test` shows that a donor send's replay EINVAL
fails back. All but the pinned and replay-EINVAL tests fail on the pre-p64
exporter; those two are no-change guards. A native rebuild must pick this source up before
any lab run can show it.

## Fail back only on the final chunk (`w64`)

Found in the n64 review: the exporter failed a first-attempt refusal back on
any chunk, but the runtime cleans up (`discardRefused` in
`internal/guestbridge/refusal.go`) only after it validated a complete cell,
which happens on the final chunk of an in-order write. A refusal of an
earlier chunk comes before validation: authentication (the FreeBSD
mount-change ESTALE), the FUSE adapter's per-request credential snapshot
(EACCES), or a frame, cell or chunk check. The runtime then still holds the
chunks it accepted:

* a multi-chunk donor send refused ESTALE or EACCES on chunk 1 or later left
  its half-filled reclaim workspace, so the next cell at that number got
  WouldBlock for good (the n64 wedge);
* a numbered cell refused ESTALE on chunk 1 or later stayed incomplete with
  its credit, while the exporter moved its high-water past the number (p64's
  stuck credit).

`_permanent_refusal` now classifies only a refusal of the final chunk, with
the e64/p64 errno rules unchanged; they match `discardRefused` (the same
excluded SESSION_RETIRE and continuation polls, the donor errnos, and for a
numbered cell ESTALE or EINVAL after an earlier chunk). Every refusal of an
earlier chunk keeps the exact candidate pinned, and the replay resends every
chunk from the first into what the runtime kept. That includes the first
chunk of a multi-chunk cell, which can only be refused before allocation
(EAGAIN there still drops the candidate, `_unallocated_refusal`). A
single-chunk cell's only chunk is its final chunk, so its rules are
unchanged.

**First attempt to reach the final chunk.** After a middle-chunk refusal
every retry is a replay, but the replay rules (ESTALE and EACCES stay
pinned) exist only because an earlier attempt could have been admitted, and
only an attempt that wrote the final chunk can be. So the final-chunk
classification asks whether an earlier attempt wrote the final chunk
(`final_sent`), not whether this is a replay. Without that, a replay whose
final chunk validation refuses (guest-freshness ESTALE, a donor's
`checkReclaim` EACCES) stayed pinned while the runtime discarded the cell,
and every later replay was refused for good. `final_sent` is set before the
final chunk is written. A replay that is the first to reach it protects a
snapshot first; a first attempt does not (that would cost one protection
per cell), and its mark reaches the ledger with the next snapshot. The mark
is ledger entry bit 4096, accepted in every ledger format but only on the
pending candidate, and cleared on issue; an exporter from before w64
refuses a ledger that carries it. Every pin after a refused chunk is
protected too (`_forward_pin`, one ledger write per refusal), so `uncertain`
and the mark are durable before the attempt ends, also across a normal
restart. Only a pending candidate restored without `uncertain` is guessed
at: it was interrupted inside its first attempt's writes (a crash before
its pin or issue), or stopped before writing anything, so it may have
written its final chunk unprotected and is restored as sent with the
replay rules. An offset-0 ESTALE is never permanent for a multi-chunk cell:
the replay of an admitted cell can get an authentication ESTALE there.

**Residual cases.** An authentication ESTALE or credential EACCES on the
final chunk looks the same as a validation refusal. A first attempt then
still fails back although the runtime kept the other chunks. The window is
now one chunk write instead of every chunk after the first. A final-chunk
EINVAL from a frame, cell or chunk check (`writeFrame`, `writeCell`,
`storeChunk`) also still fails back although the runtime discards nothing;
the exporter's frames are deterministic, so only an exporter bug can
produce one.

**What the runtime's refusal record is for.** `discardRefused` keeps the
latest removed numbered cell (`Refused` in the C8 index) so that a write to
that number gets the same answer as if the cell had been kept, also after a
restore. It does not unpin an exporter that crashed before its fail-back was
protected: that replay resends from the first chunk and gets ESTALE, which a
replay keeps pinned, or EINVAL at offset 0, which is kept pinned as well.

`backend_test.c`: `refusal_mid_chunk_donor_test` (a multi-chunk donor send
refused EACCES or ESTALE on its second of five chunks stays pinned with its
number, replays from the first chunk and is admitted) and
`refusal_mid_chunk_numbered_test` (a numbered cell refused ESTALE on its
second chunk stays pinned, a later control waits, both are then issued in
order) fail on the pre-w64 exporter, which failed them back.
`refusal_final_chunk_donor_test` (EACCES, ESTALE, EINVAL on a donor send's
final chunk still fail back and the next donor send takes the number) is a
no-change guard. `refusal_original_permanent_test` and
`refusal_original_inserted_test` now refuse the final chunk, where a
validation refusal happens (derived from the actual scoped body).
`refusal_first_chunk_pinned_test` shows that ESTALE on the first chunk of a
multi-chunk cell is now pinned. `refusal_final_after_mid_test` (a middle-chunk
ESTALE or EACCES, then a final-chunk validation refusal on the replay, fails
back, numbered and donor) and `refusal_final_sent_restore_test` (the mark
survives a restart and keeps a crash replay pinned; a crash in the first
attempt is restored as sent; one or two middle-chunk refusals restore
without it and the final-chunk refusal fails back) fail on the first w64
commit; its one-refusal case (`single-mid-restart`) also fails on round 1,
before the pin was protected. A
native rebuild must pick this source up before any lab run can show it.

## Controlled terminal owner (`614`)

Explicit features32767 adds SESSION_RETIRE and preserves the exact16383 and
older contracts. Before native forwarding, the exporter protects a permanent
scope fence, the original candidate, the complete donor/settled set and its
actual native mappings. Final protection atomically removes the old entries
and retains one charged native entry and the original ordinary/control funding
class. Other sessions keep the shared allocator. Only the exact original
same-live-owner retry is admitted; decoded ledger records do not admit a
replacement exporter or restore native authority. C9 remains required for that.

Each32767 session preowns88504 bytes inside the existing64MiB live arena:
four closed-grammar terminal body slots and the full bounded native manifest.
Ordinary/control pressure cannot spend unused terminal capacity. Permanent
final ownership keeps this package charged. Existing snapshot protection can
simultaneously hold two separately bounded65MiB staging buffers while checking
the prior ledger; those buffers are separate from live entry accounting.
No general body, group, arena or signed guest budget is increased.

[614 evidence](../linux9p/evidence/614/evidence.json) distinguishes sanitized
exporter transport fixtures, actual C/Go signed handoff, and maintained Linux
private consumption. It makes no installed FUSE, kernel ABI, PTE/TLB or positive
replacement-owner restoration claim.
