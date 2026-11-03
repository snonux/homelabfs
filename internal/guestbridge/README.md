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
only for its separately verified retained dirty-page obligation. No timeout,
disconnect, hash-only manifest or new UUID proves quiescence or continuity.
The bridge validates domain/session/scope and closed response unions; this
package does not implement guest PTE/TLB/MMU barriers.

Path grants are issuance-ledger entries with retained raw alias ancestors.
Current requests recheck ancestor search and Unix authorization under their
captured credentials; `credential_hash` describes issuance, not a fixed-user
attach restriction. Description grants retain issued access through chmod,
unlink and trusted transfers. Sequenced process exit and final description
release are distinct from native fh close. Retained capability, revision and
close-sequence maps have finite limits and never evict unresolved identities.

RESUME currently returns BLOCKED: no controller ledger reconstruction/install
API is implemented. An opaque Views callback, matching UUIDs or a hash-only
manifest cannot produce RECONCILED. The cell high-water/floors, immutable response
bytes/receipt state and issued capability ledgers must actually survive or be
verified reconstructed before this can be opened. Same-process retained state
is not discarded, but this component does not yet activate session recovery.
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
intermediate installation acknowledgement cannot sit behind its own drain. Endpoint construction pre-reserves 64 ordinary and 16 control arenas
(8 MiB per maximum-sized cell, including decoder/receipt copies) plus the
independent 12 MiB replacement workspace through `admission.Budget`; deployments
must separately budget actual kernel/runtime and public-claim/source resources.
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
its exact schema2/features2047 tuple for all enrolled sessions, or the mutually
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
lease/install target remains nonnull. Controlled local cut identities never
populate Result.Publication. Actual token ordering, earmarking, protected
reply/floor/source lifetimes and physical MM remain k04/vz3 work.

The acquisition queue/worker uses ordinary credits, separate from drain,
control and release workers. Minor1 incomplete ordinary assemblies are capped
at12 per session, leaving four assembly slots for control, alongside the
independent16 global control credits. PENDING replies must be received and
released promptly while bounded controller tickets retain their independent
resources. This boundary supplies no unbounded waiter or timeout expiration.
