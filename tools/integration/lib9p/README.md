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
  another allocator. A first ordinary EAGAIN consumes no native number and lets
  control/replacement traffic progress. All result reads accumulate bounded
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
  cells may arrive out of order. A bounded 256-range seen-cell ledger per session
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
local adapter (or equivalently durable admitted transport storage). No built-in
supervisor/process/installed-byte/enrollment provider exists. The weak
`hfs9p_controller_claim` **always returns ENOTSUP** until that real integration is
linked. A configuration flag, a checksum or an empty state directory cannot
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
