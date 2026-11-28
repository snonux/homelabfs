# Retained-9p host component (`sz3`)

This package implements host framing, admission, immutable response cells,
delegated dispatch and the capability/lifetime boundary from
[`Retained-9p-protocol.md`](../../docs/spec/Retained-9p-protocol.md).
`fuseadapter.PrivateControl` supplies the actual raw FUSE callback surface for
three distinct synthetic command/result/recall inodes. Nothing selects that
surface from a CLI, mounts it, enrolls a real exporter, grants a canary serving
role, or enables normal publication.

Native enrollment binds the actual supervised endpoint to one nonreused controller
incarnation before construction succeeds. Its mandatory provider must refuse an
already-issued endpoint or missing old ledger/exclusion; every subsequent native
verification includes that same controller incarnation. There is no built-in
endpoint enrollment or replacement provider.

Every native callback requires the real exporter UID/GID/PID, complete native
groups, supervised process incarnation and actual endpoint connection. Guest
credentials remain a separate attested request: fsuid/fsgid, complete sorted
1024-group vector, process, credential revision and pinned namespace/map.
No host-account group lookup, header substitution, raw directory fallback,
FreeBSD ioctl assumption or application-fd/native-fh uniqueness is used.

The two execution profiles have deliberately different dependencies:

* **Normal** requires `LogicalPlanner`, its actual volume-member history
  generation, built-in Unix policy, and an `Execution` implementation that
  verifies installed normal authority, the versioned public binding, source
  protection and all-domain visibility. `LogicalCut` invokes the existing
  shared planner against one captured committed RAM observation. A nonzero
  publication for a protected operation needs a real result reference.
  The handle/visibility generation is separate from the member generation.
* **Controlled validation** requires a named disposable profile and an
  operator-bound `Execution` implementation over the real local service and
  journal. `Captured` is the explicit metadata/prepare seam for that service;
  it does not require a fabricated normal3 baseline or partner. The resulting
  bridge publication is exactly null. Task `f04` owns the sealed local-service
  implementation and durable controlled claim/execution provider. There is
  no shipped runtime implementation here.

`Execution.Check` and `ServingGeneration` must verify their profile rather than
infer it from bridge HELLO, a caller flag or codec-valid authority fields. Normal
public binding/claim persistence belongs to `g04`; later normal runtime work
consumes it. The `_test.go` providers are component fixtures only, including
synthetic verifier inputs used to exercise the real shared logical planner.
They are not installed runtime providers or evidence of a normal commit.

The mandatory `Claims` provider resolves the stable public anchor before path,
owner or origin selection, reserves original operation/allocation and retained
result resources before effects, and protects exact terminal result bytes.
Cancellation never converts unknown work to no-effect. Retirement must retain
allocated unsent/rejected-number gaps until a protected no-effect resolver proves
absence and DECLINE; no missing map row or transport release advances a floor.
Authenticated recovery may install an exact `ReconciledScope` for lookup of the
original Context/credentials/intent. It cannot reexecute old-scope capabilities
as fresh operations or replace the original durable origin.

`Views` owns verified inventory, frozen manifests, page provenance/reservations,
recall phases and reconstructed recovery. `ValidateDrain` receives the entire
original binding. A previously captured exited-process credential may be used
only for its separately verified retained dirty-page obligation or exact original-
issuer FORGET/final DESCRIPTION_RELEASE cleanup. Cleanup compares the complete
live scoped grant and original canonical credential hash/revision; it cannot
revive an ordinary call, create authority, or clear the process exit marker. No timeout,
disconnect, hash-only manifest or new UUID proves quiescence or continuity.
The bridge validates domain/session/scope and closed response unions; this
package does not implement guest PTE/TLB/MMU barriers.

Path grants are issuance-ledger entries with retained raw alias ancestors.
Current requests re-walk that ancestry for identity (a renamed or removed
ancestor is Stale) and recheck Unix authorization on the operation's object or
immediate parent under their captured credentials. Ancestor search is not
rechecked: the guest VFS checks it on the directories a process walks, and
POSIX fd-relative calls never re-check above the fd (task sj4, R20-1);
`credential_hash` describes issuance, not a fixed-user
attach restriction. Description grants retain issued access through chmod,
unlink and trusted transfers. TRUNCATE and SYNC with a description, and
GETATTR and SETATTR in their described shape (task s64,
[design](../../docs/design/s64-fd-attr.md)), take the open description as
the object's authority instead of the path re-walk, so fstat, fchmod,
fchown, futimens, ftruncate and fsync work on a renamed or unlinked open
file; Unix owner rules still apply to the object's current attributes.
Sequenced process exit and final description release are distinct from
native fh close. Retained capability, revision and
close-sequence maps have finite limits and never evict unresolved identities.
`MaxCapabilities` bounds live grants; `MaxCapabilityIdentities` separately bounds
all nonreused issuance rows (default 65536), including released tombstones. A
controlled provider's retirement prunes a tombstone once the claim that released
it (for a READDIR transient grant, that READDIR) is retired
(`PruneRetiredGrants`, task o64), so rows are live grants plus tombstones that
await retirement, not every grant ever issued. A READDIR reserves its whole
`limit` against both bounds while it runs; the guest kernel always asks for 256,
so both must exceed 256 plus the live grants or every listing gets NoSpace. Exact
FORGET/count1 closes one PATH token, with an optional lookup pin belonging only
to that token. READDIR grants use the typed `TransientForgetCaptured` preparation
seam to protect a grant-only closure without dropping a service lookup. Native
commands cannot select this seam. The controlled provider implements it; normal
execution remains gated and refuses that seam until its executor implements it.
Private bridge snapshots and exact controlled-ledger scope bytes carry capability
lifetime schema 2. Old unclassified snapshots/ledgers are refused, and older
readers cannot open the new exact ledger scope. There is no in-place migration
or authority to delete old ledgers; use new disposable lab resources.

Guest RESUME returns BLOCKED: its wire request cannot supply the private
same-native barrier and live continuity grant required by C9. An opaque Views callback, matching UUIDs or a hash-only
manifest cannot produce RECONCILED. The cell high-water/floors, immutable response
bytes/receipt state and issued capability ledgers must actually survive or be
verified reconstructed before this can be opened. Same-process retained state
is not discarded. Activation requires the private same-owner C9 coordinator
and cannot be supplied by this component or a decoded record.
A new Service is never an allocator reset/recovery mechanism for an issued
endpoint. Real provider/recovery work must preserve that refusal.

The endpoint allocator belongs to one real FUSE inode tuple across all guest
sessions. It monotonically issues native cells, retains immutable snapshots and
exact receipt coverage, and rejects reuse even after released bytes are freed.
Positional short reads, padding, EAGAIN, ESTALE, EOF and virtual size 2^52 are
implemented in callback code. Native flags select ordinary/control credit and
the replacement workspace from the first chunk, then must match the complete
canonical command. Guest flags stay zero. A release/replacement atomically
transfers its donor credit without an 81st active cell. RELEASE_CELL has its own
bounded worker so an ordinary operation awaiting a recall cannot block snapshot
release. Drain execution has a separate worker from view/query controls, so an
intermediate installation acknowledgement cannot sit behind its own drain.
VIEW_ACK has its own worker too (task d84): its preparation may run a guest
observation of up to ten seconds, which must not hold up
QUERY/CANCEL/RETIRE/VIEW_POLL cells (zq3-r8 R8-1). Both lanes still spend
control credits, and the Control admission pool funds three workers (control,
drain, VIEW_ACK). Endpoint construction pre-reserves 64 ordinary and 16
control arenas (8 MiB per maximum-sized cell, including decoder/receipt
copies) plus the independent 12 MiB replacement workspace through
`admission.Budget`; deployments must separately budget actual kernel/runtime
and public-claim/source resources.
Closing workers does not destroy or reset this enrollment or retire any claim.

The wrapper reserves `.homelabfs-control-v1` against ordinary namespace mutation;
constructor preflight rejects a preexisting root entry rather than covering it.
Ordinary native credentials, participants, mount profiles and publication refusal
keep their original contracts. Kernel FORGET has no caller header and is only an
inert synthetic-inode cache event; it cannot release protocol resources. Native
RELEASE attempts authentication but never retires protocol lifetimes either.

Evidence is in `evidence/sz3-20261102/` in the isolated task workspace. Tests
exercise shared logical planning and real callback methods without mounting.
The schema checker updates the WRITE guest golden for captured max_end, preserves the other three guest golden frames and adds native-only
fragment/credit/reclaim flags plus high-umask rejection. Actual FreeBSD direct-I/O
splitting/EOF, supervised exporter identity, maintained guest/backend runtime,
installed-byte binding, complete cache barriers and normal publication remain
separate acceptance gates. No lab, SSH, VM, mount or production action was run.

WRITE uses the shared PlanDelegatedWrite seam to cap its prefix after the
authoritative append offset is selected. Execution preserves the typed pre-effect
ErrDelegatedFileTooLarge in Outcome.DelegatedError; the bridge emits FileTooLarge
without extending fsapi/v1 errno or journal records. Controlled Captured providers
must enforce the same captured bound under their serialized service seam.

Bridge1.1 is an explicit opt-in foundation described in
[view leases](../../docs/spec/Retained-9p-view-leases.md). Config.Protocol selects
its exact schema 2/features2047 tuple for all enrolled sessions, or the mutually
exclusive Config.SessionProtocols map selects1.1 for named sessions while
unlisted enrolled sessions retain1.0. Configuration is copied and cannot be
changed by HELLO. Every1.1 frame/body/reply binds that version; old readers and
other enrolled versions refuse it. No runtime provider is shipped.

The mandatory ViewProtocolProvider independently checks enrollment, every
request's original scoped principal/authorization, actual target source proof
and every protected ticket response's original admission lineage. Resource
Ticket/ClaimNumber/DirtyReservation types retain provenance and preowned capacity
before PTEs, without assigning an ordinary OriginalHash to unknown dirty bytes.
Only frozen exact bytes bind a final immutable DRAIN_WRITE claim. Bootstrap
VIEW_ACQUIRE.target:null lets the verified issuer choose a current cut; every
lease/install target remains nonnull. The optional VIEW_ACQUIRE.advance:true
(task xf4, `view_advance.go`) is accepted only on a READ DOMAIN acquisition
with a CONTROLLED_LOCAL expected target (false is refused); only then may a
GRANTED lease name a different target, and only an AdvanceCompatible one
(same enrollment, profile, context, journal incarnation, epoch and
visibility generation, not behind the expected position). Controlled local cut identities never
populate Result.Publication. Actual token ordering, earmarking, protected
reply/floor/source lifetimes and physical MM remain k04/vz3 work.

The acquisition queue/worker uses ordinary credits, separate from drain,
control and release workers. Minor1 incomplete ordinary assemblies are capped
at12 per session, leaving four assembly slots for control, alongside the
independent16 global control credits. PENDING replies must be received and
released promptly while bounded controller tickets retain their independent
resources. This boundary supplies no unbounded waiter or timeout expiration.

The controlled runtime supports [barriered same-native component restart](../controlledruntime/restart.md). Complete C8 session rows and private transferred funding are required; inspection alone never authorizes restoration. Guest RESUME, replacement FUSE/exporter owners and incomplete cuts remain blocked.

A disposable validation service may arm one [response-exposure lost-reply fixture](../controlledruntime/lifecycle.md#lost-reply-fixture-v24) on its transport (`lost_reply.go`). It withholds only the first actual `Transport.Read` exposure of a protected CALL response in the armed session: the first such read of any CALL accepted after arming, which with several CALLs in flight need not be the next one issued (EIO, no bytes, no coverage); it takes no caller bytes or results and is never written to the C8 ledger.
An optional intent filter (d44) narrows it to a CALL whose original guest intent opcode is MKDIR; non-matching CALLs in the armed session are exposed normally and the record names the dropped CALL's `dropped_opcode`.

### Targeting the MKDIR reply (d44)

zq3 T9 wants the lost reply on the mutating MKDIR itself. With the unfiltered
fixture that is practically unreachable, and no guest warm-up can fix it. The
evidence below is from the patched Linux 6.1 guest in
`tools/integration/linux9p/patches/0003-hfs1-guest-vfs.patch` (patch-file line
numbers; 0004/0005 do not change these functions):

* The hfs mount does not use v9fs caching at all: `v9fs_mount` hands any
  `version=9P2000.L.hfs1*` mount to `v9fs_hfs_mount` (line 88, selector at
  2772-2793), whose superblock uses `hfs_super_ops`/`hfs_dir_iops`
  (4644-4665). v9fs dentry/attribute caching therefore plays no part. Since
  g64 the mount requires `cache=mmap`, which names this behaviour (uncached
  read/write, page cache only for mappings); it enables no v9fs cache.
* `hfs_permission` (4026-4035) refuses RCU mode (`MAY_NOT_BLOCK` gives
  `-ECHILD`) and otherwise always calls `hfs_refresh` (4002-4024), a GETATTR
  CALL (4015). In one `mkdir(2)` the VFS calls it twice on the parent:
  `may_lookup` during the path walk and `may_create` inside `vfs_mkdir`,
  immediately before `->mkdir`. No attribute timeout or cached permission
  exists.
* `hfs_d_revalidate` (3987-3990) returns `-ECHILD` in RCU mode and 0
  (invalid) otherwise, so a cached positive or negative dentry is never kept:
  `filename_create` always reaches `hfs_lookup`, a LOOKUP CALL (4061), for
  the new name, even right after a `stat` of that name.
* Only then does `hfs_namespace` send MKDIR (4256). So every guest mkdir
  sends, in order, GETATTR(parent), LOOKUP(name), GETATTR(parent), MKDIR in
  the armed session, and an unfiltered fixture drops the first GETATTR.
* Background CALLs can also race in: inode eviction (`drop_inode =
  generic_delete_inode`, 4649) queues FORGET CALLs on the control workqueue
  (3956-3985, 3928-3954), and every exiting guest process that used the mount
  queues a PROCESS_EXIT CALL (596-692).
* After any failed CALL fetch the guest marks the session blocked
  (`hfs_request_run` `retained:`, 2059-2066; since g64 `hfs_session_block`
  logs the first block of each session), and later ordinary CALLs get
  `-EHOSTDOWN` (`hfs_request_admit`, 0002 line 2195). There is no second
  attempt, so a warm-up that lost the race cannot be repeated in the same run.

Decision: guest warm-up (option a) is impossible; per-operation arming
(option b) is implemented. `Transport.ArmLostReply(session, "CALL", intent)`
accepts the closed set `""` (any CALL, the original v24 behavior) and
`LostReplyIntentMkdir`. The predicate (`armedCall`) still reads only the
accepted cell's own decoded command, whose intent is bound to the claim's
original hash; it takes no response, result or replacement bytes, and a
non-matching CALL is exposed normally while the fixture stays armed. The
evidence adds `intent` and the actual `dropped_opcode`. The lifecycle endpoint
maps ARM operation code 2 to the MKDIR filter without changing the 57-byte
frame; the client spells it `--lost-operation CALL --lost-intent MKDIR`.
This is runtime code. The f44 rebuild (`b203799e…`) added it, the r54
(`5eb42c50…`) and w54 (`89c928d5…`) candidates carried the same daemon, the
z54 daemon rebuild (`2e0412a6…`, daemon `39c92d01…`) keeps it, the 964 kernel
rebuild (`e01fab21…`) reused that daemon, the c64+d64 rebuild (`66b5bb2b…`,
daemon `9b7eed8d…` from `060dea0`), the y64 rebuild (`867ede36…`, daemon
`34fb05b2…` from `b869710`), the l84 rebuild (`e585206a…`, daemon
`868dc045…` from `e92eaa1`), the r84 rebuild (`4712e479…`, daemon
`34a5360b…` from `cc6beed`), the b94 rebuild (`3b128cc6…`, daemon
`bf86d805…` from `dfdcdd9`) and the 5a4 rebuild (`150deb29…`, daemon
`e0981a0a…` from `1b0a306`) and the xc4 rebuild (`e9c77e5b…`, daemon
`c959b708…` from `b7d107b`) kept it, the xd4 rebuild (`3a7131dd…`) reused
that xc4 daemon, the r15 rebuild (`ac8020aa…`, daemon `8668e5f0…` from
`2f34b60`) kept it, the r16 rebuild (`a1af0bd7…`) reused that r15 daemon,
the r17 rebuild (`f614d52a…`, daemon `85900893…` from `3430758`) and
the r18 rebuild (`b2a4f69e…`, daemon `086d7cde…` from `633509e`) and
the r19 rebuild (`09acd09d…`, daemon `bb4ad73c…` from `e160370`) and
the r20 rebuild (`b37bcb06…`, daemon `c04c519f…` from `d71ae02`) and
the first r21 rebuild (`1428d4db…`, daemon `013515a8…` from `91b9ee1`)
and the second r21 rebuild (`f21450ec…`, daemon `c7b89648…` from
`3081d31`) and the r22 rebuild (`2c85b230…`, the same daemon
`c7b89648…`, reused) and the r23 rebuild (`21c49a55…`, daemon
`43238fe9…` from `21d8478`) and the r24 rebuild (`c9500232…`, the same
daemon `43238fe9…`, reused) and the r25 rebuild (`a3eac923…`, daemon
`43414d7f…` from `e9ae706`) kept it, and the current r26 rebuild
(`80cd01bb…`, daemon `024528a4…` from `9994339`) keeps it, so zq3 can use the
filter. The guest still
blocks m0 after the dropped MKDIR reply, so T10 is still expected to FENCE;
the difference is that m1 then shows the committed directory.

## Swallowed refusals and stranded RESERVED claims (a84)

After Reserve, `executeCall` (`service.go`) answers every execution error
with CALL ADMITTED/OK and nothing else. The claim is already reserved, and
the bridge must never invent a no-effect. So an uncertain refusal leaves the
claim RESERVED/UNPREPARED, and every QUERY answers INDETERMINATE and retries
it. Such refusals are viewcontrol's WouldBlock while a recall is active, a
full service queue, or another operation's PENDING snapshot. In zq3-r8 two
such claims stayed stuck until the guest's 30 s QUERY window ran out, and
nothing in the ledger said why
([record](../../docs/lab/results/20261114-zq3-r8/README.md)).

**Diagnostics (done).** The private provider now records the last refusal on
the claim entry (`controlledvalidation/refusal_trace.go`): a class
`<stage>:<errno>` (stage `LOAD`, `ADMISSION` or `EXECUTION`, errno named by
`ErrnoName`), a count, and the Unix seconds of the first and the latest
recorded refusal of that class. OBSERVE's provider claim summary (b84) copies
it into `Refusal`, `Refusals`, `RefusedFirst` and `RefusedLast`. All four are
omitempty, so an older reader and a never-refused claim see the b84 shape
unchanged. To keep OBSERVE bounded they appear only on the four lowest
`Pending` numbers (the claims a stuck guest waits on), never in `Recent`, and
every copied string is cut to 24 bytes. One provider's summary then stays
under `MaxClaimEvidenceBytes` (4.5 KiB) for real (ASCII) values; a damaged
entry with characters JSON escapes could exceed it, and an oversized reply is
refused whole, never truncated. 16 providers plus the recall history leave at
least 16 KiB of the 128 KiB OBSERVE reply. The budget covers OBSERVE only: a
RESTART reply carries the same evidence inside the 4 KiB smaller
`lifecycleOutcomeBytes`, so a worst-case RESTART (16 providers with full open
claims and a full recall history) can fall back to its truthful
EFFECT_UNREPORTABLE outcome. `tools/lab/retained9p/bootstrap.py` prints the
trace with each open claim. The trace is a diagnostic only:

* It is written only to the exact never-prepared claim: RESERVED/UNPREPARED,
  no plan, outcome or snapshot, and not a gap. A claim that reached
  PREPARING, PENDING or TERMINAL, or that Cancel or ResolveGap turned into a
  gap, is never rewritten, so a gap proof (the hash of the entry) stays valid.
* No decision reads it. Every credit measure of an entry (a plan's
  retained-credit check, the READDIR cleanup's owner check) measures the entry
  without it (`entryFootprint`).
* It is bounded to one class per claim, and rate-limited per claim: at most
  one write a second, whatever the class. A class change waits for the next
  slot. A QUERY loop polling every 10 ms does not cost an fsync each round,
  even when its refusals alternate between classes, and `Refusals` is a lower
  bound. A wall clock that steps backwards counts as a new slot once and the
  limit restarts from the new time.
* It is best effort and written under the provider's execution mutex. A
  failed write is dropped and never changes the answer.

**Releasing stranded claims (hh4).** A
stranded claim keeps its retained and source credit until a guest RETIRE
recycles its number, and the guest never retires past it. Only the
never-prepared claims above can be released: no plan was protected for them,
so their work was provably never issued and no effect can have happened. A
claim from PREPARING on may have reached the backend and stays for
reconciliation.

No host-observable event proves that the guest gave up: `hfs_session_block`
is guest-local, a host timeout cannot rule out a delayed QUERY, a transport
failure is the host's own fence, and SESSION_RETIRE needs `terminalEmpty`.
So the release needs a guest signal. hh4 adds it as a new controlled
operation, ABANDON (`abandon.go`; design and safety argument in
[docs/design/hh4-abandon.md](../../docs/design/hh4-abandon.md)). A blocked
session names the exact claims it gives up on and promises never to query
them again. In one ledger transaction, serialized with every preparation
under the provider's execution mutex (`controlledvalidation/abandon.go`), the
provider declines each provably never-prepared claim as a gap, returns its
credit and zeroes the entry's credit, and answers ABANDONED with the gap
proof RETIRE needs. Everything else is KEPT. OBSERVE counts the releases
(`ClaimEvidence.Abandoned`, `AbandonedCredit`). A delayed QUERY, CALL or
preparation after the abandon finds the gap and is refused or has no effect.

ABANDON is claim-ledger control work like GAP_RESOLVE. It skips the view
provider's request check, so a 0a4-fenced session can still abandon. The
guest kernel (patches 0002, 0003, 0005) queues it once from
`hfs_session_block` and runs a final pass in kill_sb, exempted from the
blocked-session halt as dh4 exempted GAP_RESOLVE; the lib9p forwarder routes
it as control work. A guest behind an older forwarder gets the exporter's
refusal and keeps its claims charged, as before; their trace shows why. No
guest kernel with this change has been booted yet.
