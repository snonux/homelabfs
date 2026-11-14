# Durable controlled view controller

`viewcontrol.Controller` implements bridge1.1 `ViewProtocolProvider`, `Views`,
`controlledvalidation.Admission` and the mandatory native service admission
boundary for an explicitly signed disposable local profile. It runs actual local
service/journal/claim ledgers. It grants no normal3 publication, partner receipt,
installed MM/PTE/TLB proof, daemon or mount activation.

Construction requires a real `fsservice.Service` with immutable
`RequireControlledViews: true`, one or more real `controlledvalidation.Provider`
instances over that exact service, operator/host/guest signatures over the full
inventory/bounds/baseline, and every method of the external `Facts` interface.
`Bounds.Sources` must be at least `MinSources(domains) = 2*domains + 4`.
Construction refuses a lower value with a named `fsapi.Invalid` error;
`source_floor.go` derives the floor from `sourceCapacity`. Admissions outside
a recall prune cuts that no lease or recall event pins (`source_pruning.go`).
Each domain's origin_journal must identify the actual admitted local journal
incarnation. Missing native/platform/source continuity facts refuse construction
or keep operations blocked. A new controller never resets a bridge endpoint:
there is still no host bridge transport restore API. Component ledger reopen
requires exact full-ledger continuity/positive exclusion; existing bridge objects
continue to point at their closed controller.

The separately signed acquisition and control Ticket families are aligned with
Domains in `Enrollment.TicketFamilies`. Each full original Credentials/context
has independent monotone counters/floors in each lane. ACQUIRE/RESERVE/RENEW use
Acquisition; FREEZE/RELEASE use Control. QUERY/CANCEL address the original family;
RETIRE closes that family's exact terminal reply ids. RENEW still travels the
existing control transport lane but cannot spend preowned freeze/close capacity.
Pre-admission refusal retains an uncertain original number/request at the caller;
it is retried or resolved under that exact identity, never skipped or reused.

The controller's exclusive private bbolt ledger retains complete original
admissions, immutable initial replies, separately protected current/cancellation
outcomes, lease versions, actual sealed source snapshots, reservations and
frozen calls, events/ack digests/manifests and permanent floors. Query may grant a
previously pending acquisition without changing its original PENDING replay.
Revoked grants never regain host authority when their old reply is replayed.
The physical fence is kept **per domain**, exactly as the guest kernel keeps it
per mount: every domain starts at fence 1, and a recall advances the fence of
each domain that received its events once all exact REOPENED acknowledgements
are in. A domain outside that recall keeps its fence, so a mount that joins
after another mount's recall is still issued fence 1 (zq3-r6 R6-1: one
controller-wide fence stamped 2 there and the guest refused the mount with
EKEYREJECTED). Leases, the unadopted-exclusion statement's old fence and the
physical observation checks all use the acknowledging domain's own fence. The
ledger records this as schema `LedgerSchema` (2); an older ledger with one
controller-wide `Fence` is refused on reopen (Permission), never migrated,
because its fence cannot be split per domain. Per-lineage
revision high-water survives compaction, preventing content-id reuse even when a
released unchanged cut is reacquired in the same physical fence.

Every source capture flushes/rereads actual complete original journal frames on
the serialized service worker, and captures complete canonical catalog plus exact
payload bytes. It binds stream epoch/incarnation/tail frame hash, actual installed
storage position, signed enrollment/policy/source identity and serving generation.
A journal tail may contain structural/lifetime records: it is never inferred to
be a mutation applied to the store. Source proof cannot be supplied by a caller
hash or normal-shaped certificate. A supplied acquire target is an expectation;
a stale/pruned expectation drives a bounded stopped install and then BLOCKED for
that old expectation. A fresh request uses the installed current cut. Returned
lease targets always pass the actual source verifier. READ/GETATTR preserve atime
and source cut, so parked fetch/acquire retries cannot create their own cut loop.

Acquisition and ordinary observation/mutation admission run outside shared
execution locks. Contention returns promptly. A fully acquired permit rechecks
its exact source cut on the worker immediately before planning/apply, including
recovery and native lock retries. Same-domain dirty pages are not excluded from
conflicts. PAGE aliases use physical home/allocation/range; DOMAIN/metadata/EOF/
namespace dependencies conservatively recall all affected enrolled observations.
Compatible disjoint page grants preserve all existing provenance. Optional
fresh admissions cannot spend preowned freeze/release/permanent-identity credit.

Recall scope (`recall_scope.go`, task 784 after zq3-r8 R8-1). Authority
reductions (PROCESS_EXIT, DESCRIPTION_CLOSE, FORGET, DESCRIPTION_RELEASE)
conflict with no lease and only with their own domain's open reservations, so
a peer's lease never makes them start a recall; the lock-only PROCESS_EXIT and
DESCRIPTION_CLOSE keep the cut and the recall barrier never holds them, while
FORGET/DESCRIPTION_RELEASE may append lifetime state and wait while a recall
installs a cut. A clean READ release of a domain without EXCLUSIVE_DIRTY leases
or open reservations starts a recall of that domain only
(`recallState.Domain`): other domains get no event, keep their leases and
fences, and their cut-preserving reads (every read but LOOKUP, which may
journal a HandlePin), READ acquisitions/renewals and lock-only reductions proceed.
Anything that may change the cut, LOOKUP included, waits for any recall.
Dirty, exclusive, conflict and expected-target recalls stay all-domain with the
global barrier. The guest takes observation leases with expected_target = its
installed view, so a cut move admitted under peer leases (FORGET, a final
DESCRIPTION_RELEASE, a pinning LOOKUP) makes the peer's next observation hit
the expected-target mismatch and start an all-domain recall (e84 tracks
scoping it). `Domain` is `omitempty`, so all-domain recalls keep their exact
schema-2 bytes; downgrading while a scoped recall is in flight is unsupported.

DIRTY_RESERVE first protects RESERVING with complete original provenance and
controller credit, then durably earmarks its future ordinary number plus actual
provider claim/result/source credit, and only then protects GRANTED. Earmarks are
typed records without OriginalHash, distinct from immutable ordinary Claims.
Freeze protects FROZEN_PENDING and exact original call/data/credentials/recall
before idempotent `BindFrozen`; FROZEN is returned only after both ledgers are
durable. Every DRAIN_WRITE must equal those bytes. Bound dirty claims cannot be
cancelled as ordinary gaps. The sealed positional-drain planner preserves mmap
page offsets through O_APPEND descriptions; ordinary WRITE still appends.

STOPPED requires an exact complete adopted/frozen manifest and mandatory
physical/source facts. Issued but unreceived/unadopted reservations may be omitted
only after the separate mandatory `CheckUnadopted` verifies a typed universal
old-epoch exclusion statement. Omission or a hash/list alone establishes nothing.
The statement binds the signed admitted running-kernel/source contract, exact
observed STOP event and authenticated ACK bytes, domain/session/that domain's old host fence,
complete host ledger and every exact omitted reservation. Runtime verification
must obtain actual ACK provenance via the native/guest supervised endpoint and
live installation/boot/session evidence for that running component. The admitted
kernel contract must make old-revision adoption impossible: capture local revision
before RPC, require LIVE plus unchanged revision/target atomically at reservation
adoption and final writable PTE, close admission before freezing the complete
adopted registry, and increment revision before every reopen. A source hash,
signature, newly chosen UUID or caller-supplied receipt is insufficient.

`guestbridge.VerifiedRequest(ctx)` exposes only observations minted privately by
real Execute after native authentication and exact scope/version checks: detached
native headers/groups/controller/scope/protocol and canonical command/payload
hashes. Facts can bind the ACK to the actual observed command without retaining
another1MiB copy. This proves request provenance only; the actual running-kernel
contract and live barrier/source completeness must still be independently checked.
Facts must consume already available evidence and return blocked when missing,
without waiting for recall or reentering service execution.

Omitted frozen/bound calls are always refused. For positively excluded unfrozen
reservations the controller protects CANCELLING with the exact proposed CANCELLED
TicketResponse, then closes the independent typed ordinary-number gap using that
response's actual id, and only then publishes Current=CANCELLED/accepts STOPPED.
If RESERVING crashed before any earmark, closure atomically protects that exact
next ordinary High number as a typed closed gap. No ordinary Claim, Result or
placeholder OriginalHash is invented. Existing ordinary RETIRE.result_hashes uses
the actual protected CANCELLED ticket response id, which the guest obtains via
original VIEW_QUERY/CANCEL. Initial GRANTED replies stay immutable and can never
reactivate a closed reservation. Partial closure is recoverable under its original
binding; false omission and lost closure replies preserve obligations.
 Drains execute while stopped, with their reserved provider credit.
Protected terminal results permit INSTALL_DRAIN before DRAINED. Exact intermediate
install acknowledgements, all original drain results, DRAINED, final install and
REOPENED close the recall. No query, timeout, cell release, disconnect or new UUID
retires rights. Disconnect invalidates learned acknowledgements; recovery needs
full live continuity/exclusion and fresh exact acknowledgements.

VIEW_POLL is nonblocking and always answers `{status:EMPTY|EVENT}`. A domain joins
the recall inventory only through its first VIEW_ACQUIRE, but the guest kernel
starts polling as soon as its view control exists. A poll from such a not yet
registered domain whose DOMAIN capability names the session's own scope gets
the canonical `{"event":null,"status":"EMPTY"}` and changes no ledger state, so
`inventory_hash` still covers exactly the acquired domains. The backend reposts
the poll and the next one after a later ACQUIRE/RELEASE delivers the RECALL.
A registered domain presenting a different capability is still refused. Before
zq3-r4 this poll was refused with PERMISSION, the endpoint left the cell
silently `blocked`, and the guest's first release could never be recalled.

A poll for a recovering, poisoned or closed controller has no spec reply and
returns an error, which the endpoint answers with an explicit transport error.
Event issuance commits domain sequences and events only after all of them were
built, and the recall phase advances only after that, so a failed progress step
leaves the in-memory ledger equal to the protected one. A poll whose progress
meets contention (NoSpace from a full service queue in `fsservice.Controlled`,
WouldBlock, Busy) therefore still answers EMPTY/EVENT from the current state
and the next poll retries. Any other progress failure, or any failure that
poisoned the controller, is an error (`guestbridge.Final`, never retried).

VIEW_ACK always answers `{status:ACCEPTED|STALE|BLOCKED}` or, where the spec has
no reply, an error:

- STALE: the ACK does not match the current recall, session, sequence, phase,
  hashes, manifest or target, or the session is sealed.
- BLOCKED: only classes that clear with the identical ACK: WouldBlock, Busy and
  `ErrNotObserved` (Facts report that the fixed observer has not measured the
  phase or exclusion yet, distinct from a real Permission refusal).
- Error: recovery (the disconnect discarded learned ACKs and guest RESUME stays
  blocked), closed/poisoned controllers, real Permission refusals, Unavailable,
  IO, NoSpace bounds, undecodable payloads, unknown phases and failed protected
  writes.

The guest kernel resends BLOCKED every 10 ms without a limit of its own, so one
exact ACK may stay BLOCKED for at most `BlockedACKBound` (30 s, the guest's
transport deadline); afterwards it gets an error reply. The bound is per
session, in memory only, and restarts on different ACK bytes or any other
reply. An ACCEPTED ACK is protected first; a later progress failure does not
change that reply.

Signed finite bounds cover permanent identities/tombstones, separate live/control
tickets, reservation/freeze/manifests, complete source archives and peak source/
ledger serialization memory. Source slots for outstanding drains and final
installs are reserved before granting a dirty reservation. Resource refusal is
explicit. At verified terminal retirement, complete tombstones keep operation/
request hash/full principal/floor, while retired bodies and unreferenced closed
leases/reservations/snapshots can be reclaimed. Source snapshots are not freed
on reply, disconnect or mere reopen. Identity/fence/revision exhaustion remains
permanent and blocks new admissions. A runtime workload must enroll sufficient
bounds and retire completed pressure windows; this component does not install
or calibrate a runtime profile.

Tests use actual temporary OS files, journals, service/planner, controlled claim
providers and bridge dispatch. Physical/native/enrollment verifiers are explicit
primitive-boundary shims requiring exact declared observations; they prove no
installed platform behavior. Tests also explicitly call the trusted
`Provider.Allocate` before ordinary CALL. Automatic ordinary-number allocation,
endpoint restoration and daemon/guest enrollment wiring remain concrete runtime
driver work for the later artifact/base gates. Frozen ordinary numbers are
allocated by the real EarmarkNumber/BindFrozen path here. No normal owner-disk wait
or origin-placement rule is introduced: future normal integration still requires
original-origin durability plus distinct owner/partner RAM, without owner-disk
wait.

## Source capacity and pressure refinement

Source snapshot slots and source byte credit are separate. Before DIRTY_RESERVE
can authorize a PTE, metadata-only projection accounts complete sparse growth to
every outstanding page's maximum end, the aggregate across physical objects,
per-write receipts and conservative scalar/catalog widths. Multiple pages in the
same object charge its maximum extent rather than independent payload copies.
Actual controlled plans preflight their complete mutation/pin effects before
plan/effect protection; native journal effects perform the same worker preflight,
including a creation/truncate-open's following pin. The sizing path projects
catalog metadata only and never allocates the future hole/payload. No accepted
drain may make its final source cut unrepresentable or strand INSTALL_DRAIN.

An issued lease's funded VIEW_RELEASE starts recall when needed (scoped to the
releasing domain for a clean READ lease, see above) and returns
protected PENDING. Thus a guest can initiate pressure progress through its
independent control family even while an unknown/refused acquisition-family
number bars a pressure READ. RELEASED still requires actual old-epoch quiescence,
drain/install/reopen and exact source closure; PENDING replay cannot release it.
That closure is per lease (task g84): the controlled runtime's Facts certify a
release once the lease's lineage is inactive, holds no open reservation and a
recall of its domain completed after the grant (lease fence below the domain
fence). Newer leases of the same domain, granted after that REOPEN, keep their
own authority and do not hold the older release; the guest serializes a mount's
releases on one cleanup worker, so waiting for them deadlocked until the guest's
30 s exchange timeout failed the mount. Retirement still waits for every lease
and reservation of the domain.

A DIRTY_RESERVE that was never granted and has protected BLOCKED history cannot
be retired while its promised ordinary number remains unbound. CANCEL stages the
exact original binding and proposed CANCELLED reply, closes the next typed number
gap with that reply id, and only then publishes/permits retirement. This uses
verified never-granted history and source checks, not absence inferred from an
unrecorded request. Unknown/unadmitted requests still retry their original bytes
and number first. The all-guest pressure regression exercises that complete
sequence without a native observer or third ticket family.

For controlled runtime release completion, an authenticated VIEW_QUERY can resume
only its exact protected PENDING VIEW_RELEASE. The locked controller privately
binds the actual outer observation, full scope/ticket/issuer and original payload
to its concrete Controller/Facts/Service assembly for that synchronous callback.
The borrow expires on return. Facts still rechecks current native identity and
all source/physical obligations; no public context constructor or generic
operation authorization bypass is supplied.
