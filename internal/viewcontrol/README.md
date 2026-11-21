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

Every source capture flushes/reverifies actual complete original journal frames on
the serialized service worker (incrementally since te4: verified bytes are
rehashed, only appended frames decoded, any change reruns full verification), and captures complete canonical catalog plus exact
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
(`recallState.Domain`; a guest observation lease's release starts none since
je4, see below): other domains get no event, keep their leases and
fences, and their cut-preserving reads (every read but LOOKUP, which may
journal a HandlePin), READ acquisitions/renewals and lock-only reductions proceed.
Anything that may change the cut, LOOKUP included, waits for any recall.
Dirty, exclusive and conflict recalls stay all-domain with the global
barrier. The guest takes observation leases with expected_target = its
installed view, so a cut move admitted under peer leases (FORGET, a final
DESCRIPTION_RELEASE, a pinning LOOKUP) makes the peer's next observation hit
the expected-target mismatch; since e84 and wf4 that recall is scoped to the
observing domain while no peer lease could show stale state (see "Scoped
expected-target recalls" below) and all-domain otherwise. `Domain` is `omitempty`, so
all-domain recalls keep their exact schema-2 bytes; downgrading while a
scoped recall is in flight is unsupported.

Scoped expected-target recalls (`mismatch_scope.go`, `observed_cuts.go`,
task e84 after zq3-r14..r17). An observation acquire whose expected_target
is not the current cut starts the recall that installs the current cut in
the requesting domain, then answers BLOCKED for the old view.

*What an all-domain mismatch recall protected.* The requester needs a fresh
view of the current cut, which any recall covering its domain gives it.
A peer needs the recall only if its view could show something the current
cut no longer holds. A peer without an active lease cannot serve from its
view at all: every VFS read first takes its own observation lease on its
installed view and so hits its own mismatch (and mappings fault under PAGE
leases; see the je4 section). A peer with an active Targeted observation
lease (every holder acquired it with expected_target equal to its target)
has that target installed (an advance holder, task xf4, may not have adopted
it yet, but its view is cacheless and serves nothing under the unadopted
lease, a guest obligation of xf4 step B, so it shows nothing a reinstall
could change); if that cut shows the same client-observable
state as the current cut, reinstalling the current cut changes nothing it
can see. An untargeted (discovery or renewed) lease says nothing about the
installed view: in the `hfs_mmap_bootstrap` loop the view can be older than
the lease target, so such an active peer lease keeps the all-domain recall.

*Pin-only moves versus observable changes (e84 background).* Under e84 the
scope classified the expected cut against the current one; since wf4 it no
longer does (see "Idle peers" below), and the classification and digests
described here are used only to compare active peer leases' cuts with the
current cut. HandlePin/HandleRelease records
(a pinning LOOKUP or OPEN of a non-root object, a FORGET or final
DESCRIPTION_RELEASE that reclaims nothing) change only the catalog's
position, pins and duplicate-operation receipts; storage validation lets them
touch no object or entry. Every mutation (data, attributes, namespace) and
every Reclaim changes an object, an entry, a retired allocation or a payload.
In the controlled service a final DESCRIPTION_RELEASE or FORGET of an
open-unlinked file journals only HandleRelease: the Reclaim comes from the
fsservice reaper, which runs only with a ReclamationPolicy (the controlled
service has none), so the unlinked object stays, unchanged and unreachable
by name, and the move is pin-only. A Reclaim, once journaled, removes the
object and retires its allocation, an observable change
(`TestObservableCatalogOnRealStore`, `TestOpenUnlinkedFinalReleaseMismatch`).
`fsservice.ControlledViewSource.ObservableDigest` hashes exactly the
observable part (journal incarnation, epoch and serving generation, the
catalog without position/pins/receipts, every payload byte), so equal
digests mean equal observable state. The controller compares endpoint cuts,
not journal records, which is at least as strict: a change that was later
undone shows nothing, and any surviving change fails the comparison.

*Idle peers (task wf4, zq3-r18 H1).* Until wf4 the scope also required the
expected cut to be observably equal to the current one (a pin-only move), so
every mismatch after a mutation recalled all domains. That condition
concerns only the requester's view, which the recall replaces either way.
A peer lease on a pre-mutation cut is caught by the peer-lease comparison.
A peer with no active lease and no open reservation is in the state the
mutating CALL already left it in: a CALL conflicts only with active leases,
so it recalled nothing there, and je4's recall-free release produces such
idle stale views routinely. The guest serves nothing from that view without
a lease (no PTE without a PAGE lease, dentries revalidate through an
observation, attributes never become timeless permission); the peer's pins
are lifetime references, not cache authority. Its own next observation
mismatches and recalls it. In zq3-r18 T1 m1 was idle, and three of the four
EXPECTED_TARGET recalls were all-domain (7.5-9.7 s each).

*Scoped when.* The recall covers only the requesting domain when the request
is READ, the domain holds no EXCLUSIVE_DIRTY lease and no open reservation
(nothing to freeze or drain, exactly like a scoped release recall), no
domain holds an open reservation, and every active peer lease is Targeted
with a cut of the current digest (or no peer holds one). The scoped recall's
barrier rules are those of task 784: every cut-changing op of every domain
waits for it, so it installs one stable cut. Everything else (EXCLUSIVE_DIRTY
request, dirty requester, an open reservation anywhere, an untargeted active
peer lease or one on an observably different or unknown cut) keeps the
all-domain recall. A peer lease that survives a scoped mismatch recall
still sits on its pre-move cut, so its later release keeps the scoped
release recall of the moved cut (je4's current-cut condition).

*Remembered digests.* The controller keeps, in memory only, the observable
digest of the last `observedCutLimit` (64) cuts it retained, keyed by the
full cut identity (including the SHA-256 of the complete State), so a target
finds exactly the digest of its own bytes. Under e84 this let the expected
cut be classified after pruning had dropped it (the guest retires answered
tickets promptly, compaction drops released leases, pruning then drops the
cuts only they pinned). Since wf4 only active peer leases are compared, and
leases pin their cuts, so the cache only saves recomputing digests; an
unknown peer digest still keeps the all-domain recall. Eviction drops cuts
of another journal incarnation first, then the lowest position. It is not
part of the protected ledger: no ledger byte, digest or reply changes, and a
reopened controller starts empty. The replay
`TestZQ3PinningObservationsScopeMismatchRecalls` (pinning LOOKUPs, GETATTRs
and FORGETs on two mounts, retiring tickets after each observation) goes
from 9 recalls (6 all-domain + 3 scoped) before e84 to 12 recalls, all
scoped, with it: more recalls in total, because a peer's view is no longer
refreshed by another domain's recall, but no global barrier and fewer
domain stops (12 instead of 15).

Recall-free observation release (`observation_release.go`, task je4 after
zq3-r14..r17). Every guest VFS read takes a DOMAIN READ observation lease with
expected_target = its installed view and releases it when the request ends.
Before je4 the last holder's release started a scoped recall (four ACKs,
each after a fresh guest observation, 4-7 s in the lab), so guest.py's
credentials admission spent 10-12 s per mount root and raced those recalls
(R17-1).

*Why a release recalled.* RELEASED asserts that the lease owns no PTE,
mapping or dirty byte, and the runtime Facts certified that only through the
domain fence: a recall completed after the grant, so the observer measured
STOPPED/REOPENED. That protects (1) the guest's cached view not outliving its
lease, since a peer's later write no longer recalls the domain once the lease
is inactive; (2) dirty bytes draining under their original authority; (3) the
lease's pin on its cut, which keeps peers' cuts and source capacity
consistent. A release never moves the cut and only unpins one, so (3) never
needed the recall, and (2) is vacuous for a READ lease without an open
reservation.

*Why (1) holds without a recall for an observation lease.* A DOMAIN lease
authorizes no PTE (mappings fault under PAGE leases); the observation
installs attributes only while in flight under its stamped MM token;
`hfs_mmap_end` removes the observation and marks its lease revoked under
`view->lock` before queueing the release; dentries revalidate through RPC;
`hfs_mmap_pressure` never selects an observation lease (p84); and
`hfs_lease_release_locked` blocks the session on a RELEASED lease that is
still published and not revoked. The host already relies on this for every
non-last holder of a DOMAIN lease (q84). Once the lease is inactive, a peer
write that moves the cut is caught by the next observation's expected-target
mismatch (scoped to that domain unless a peer then holds a lease on an
observably older cut or an open reservation, e84/wf4), exactly as after the
scoped recall.

*Conditions.* The release is RELEASED at once, without a recall and without
moving the fence, only when: the lease is active, DOMAIN, READ and this is
its last holder; every holder acquired with expected_target equal to the
granted target, or by advance (task xf4, below) (`leaseState.Targeted`, omitempty; target:null discovery and
renewals never qualify, because the `hfs_mmap_bootstrap` loop needs the
recall to install the current cut when `hfs_view_bind_initial` refuses); no
renewed sibling of the lineage is active; the domain holds no EXCLUSIVE_DIRTY
lease and no open reservation; no recall runs (it is PENDING until then); and
the current cut equals the lease target (if a peer FORGET, final
DESCRIPTION_RELEASE or pinning LOOKUP moved it, the scoped recall is kept, as
it installs the moved cut now instead of the next observation's mismatch
recall, which is usually scoped too since e84/wf4); and the domain holds
fewer than `lineageBudget`
(Identities/(2*domains), 16 at the zq3 spec) lease lineages at its current
fence. A lineage includes the credentials (process UUID) and target, so
nearly every guest process opens one, and its revision high-water may be
recycled only after the domain fence moved past it (else a lease ID could be
reused); without recalls they would pile up past the Identities bound on
`Revisions` that reopen enforces. At the budget the release keeps its scoped
recall, so a domain pays one recall per about 16 processes instead of one
per observation. A recall-free release also needs `Revisions` as a whole below
half of Identities, and grantLease BLOCKs a new lineage once `Revisions`
holds Identities (the hard reopen bound; before je4 it was not enforced at
grant). A capture failure falls back to the recall path. The lease is
deactivated first and Facts.CheckPhysical still
certifies the release: the runtime Facts accept a Targeted DOMAIN READ lease
without a fence advance, with the same lineage and reservation checks
(`controlledruntime/facts.go` `checkRelease`); a refusal restores the lease.
PAGE and OBJECT leases, discovery leases and moved-cut observation leases
keep their recall. The replay
`TestZQ3CredentialsAdmissionReleasesWithoutRecall` counts 8 recalls for the
credentials admission before je4 and 0 after it.

*Guest follow-up.* The host trusts the guest's revoke-before-release for
DOMAIN leases rather than measuring it. A release that carries the
revocation, or an observer-measured observation count, would let Facts check
it instead. No guest change is needed for the host change to be safe.

Guest-requested view advance (`advance.go`, task xf4 step A after zq3-r18
R18-1 and zq3-r19 R19-1; design `docs/design/xf4-view-advance.md`). Every
observation after the guest's own mutation or pinning LOOKUP/OPEN expects a
cut that is no longer current, so each one cost an expected-target recall
(5-7 s scoped) and a BLOCKED retry; T1 spent most of its time in them. For a
guest view without target-bound cache (no mmap pages or mappings, PAGE
grants, dirty pages or reservations) that recall's whole guest-side effect
is "target := current cut, revision++".

*Protocol.* VIEW_ACQUIRE may carry `"advance": true` (sorted after
`access`): "my view at the expected target is cacheless; if it is stale,
grant on the current cut and I adopt it". Absent, the bytes are exactly the
old ones; `false` and every use outside a READ DOMAIN acquisition with a
CONTROLLED_LOCAL expected target are refused by the bridge schema
(`guestbridge/view_advance.go`). The bridge accepts a GRANTED lease on a
different target only for an advance request and only when
`guestbridge.AdvanceCompatible` holds. A new daemon serves old and new
kernels; a new kernel needs this daemon.

*Grant.* In acquire, before `expectedMismatch`, a stale observation is
granted an ordinary lease on the cut acquire just captured when: (1) it
requested advance, READ, DOMAIN, and its ticket has not started its mismatch
recall (after that recall it stays BLOCKED as before); (2) no recall runs
(else PENDING, and the readmitted QUERY may then be granted by advance);
(3) expected and current cut share enrollment, profile, context, journal
incarnation, epoch and visibility generation, and the expected position is
not ahead (at an equal position, the same journal hash); (4)
`mismatchRecallScope` returns the requester, i.e. wf4 would have answered
with a DOMAIN recall of the requester alone (clean requester, no open
reservation anywhere, every peer idle or Targeted on an observably current
cut); (5) the requester holds no active PAGE or OBJECT lease; (6) the grant
fits every bound (Sources, lease table, control and identity credits,
Revisions; checked without side effects), since a BLOCKED answer would
repeat on every retry where the recall's STOP and fence move relieve the
pressure. Anything else takes the unchanged recall path. The grant is the one a matching expected
target would get (`grantLease`): Targeted, at the domain's current fence, no
fence moves, and the peers see exactly what the scoped recall would have
left them. The guest adopts the target only when its installed target still
equals the expected one at the captured revision, so nothing stale is served
and no rollback is possible.

*Advance leases.* `leaseState.Advance` (omitempty, so ledgers without such a
lease keep their schema-2 bytes; downgrading after advances is unsupported)
records that every holder requested advance; `shareLease` ANDs it like
Targeted. The last holder's release of an Advance lease is recall-free even
when the cut moved (`observation_release.go`): the current-cut condition was
never a safety condition (a DOMAIN lease authorizes no PTE on any cut), it
only avoided pushing the cost to the next observation, which for an
advancing guest is an advance grant instead of a recall. Without it every
pinning LOOKUP observation would still end in a RELEASE recall. The lineage
budget and every other je4 condition still apply, and the runtime Facts
certify the release as for any Targeted DOMAIN READ lease.

*Counter and replay.* OBSERVE reports `controller.recalls.advances`, the
advance grants since the controller opened (memory only, beside the recall
history). The replay `TestZQ3AdvanceRemovesExpectedTargetRecalls` (T1
credentials case body, both mounts, dentry eviction FORGETs and pinning
LOOKUPs, the creating CALLs meeting a still-queued observation release)
counts 7 CALL_CONFLICT + 28 EXPECTED_TARGET + 9 RELEASE recalls without
advance and 7 CALL_CONFLICT recalls with 37 advances with it.

Read-only OPEN (`read_open.go`, task z94 after zq3-r11 R11-1/zq3-r14 R14-1).
An OPEN with access READ and no create/exclusive/truncate/append flag conflicts
like a read: only with EXCLUSIVE_DIRTY leases and open reservations, never with
a READ lease (the caller's own observation lease included). fsservice plans no
mutation for it and has no access-time update, so its only effects are a
HandlePin and an open handle, lifetime state no lease observes. Because the pin
may move the cut, it is not cut-preserving: every recall still holds it, and it
is charged source capacity as a non-read. Native OPENs are unchanged. The
guest observes with expected_target = its installed view, so a root pin would
still have recalled every domain through the expected-target mismatch; the
volume root, which is never reaped and cannot be unlinked, therefore keeps a
pinless lifetime in fsservice (`pinlessLifetime`): a root OPEN/close cycle
journals nothing and keeps the cut. Non-root pins still move it; since e84
the resulting mismatch recalls only the observing domain while peer leases
are observably current or absent.

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
reply. An early DRAINED counts from a still-BLOCKED peer's wait or from a
fenceable silent peer's fence point, if later (see "Stalled recall
participants" below). An ACCEPTED ACK is protected first; a later progress failure does not
change that reply.

Signed finite bounds cover retained ticket identities, separate live/control
tickets, reservation/freeze/manifests, complete source archives and peak source/
ledger serialization memory. Source slots for outstanding drains and final
installs are reserved before granting a dirty reservation. Resource refusal is
explicit. Source snapshots are not freed on reply, disconnect or mere reopen.
Fence, lease revision and per-family ticket number exhaustion remain permanent
and block new admissions. A runtime workload must enroll sufficient bounds and
retire completed pressure windows; this component does not install or
calibrate a runtime profile.

Identity recycling (`recycling.go`, tasks q64 and f84). A verified VIEW_RETIRE
floor recycles the retired tickets: their rows are deleted and their
`Identities` charge returns, so `Bounds.Identities` bounds the tickets alive
at once, not every ticket a controller ever admits (before, a guest read's
DOMAIN READ lease cost two never-returned identities, about 32 reads per
controller lifetime at the zq3 spec). Replays stay safe because each ticket
family's High/Floor counters are permanent: a recycled number is at or below
the floor, so it is never admitted again. An admission op under it (exact
retransmit or not) answers a rights-free RETIRED_ID bound to its own
operation and request bytes, which for an exact retransmit is byte-identical
to the old tombstone's answer, and CheckResponse vouches only for that reply;
QUERY and CANCEL, which carry no original operation or hash, are refused as
Stale. The guest's view code never sends one: it retires only tickets it no
longer uses and frees them once RETIRED arrives (the terminal-kernel80
harness replays retired admissions on purpose). The same
compaction drops unreferenced closed leases/reservations/snapshots, a lease
lineage's revision high-water once no lease of it remains and its domain's
fence moved past it (no grant can mint it again, so no lease id can be
reused), and every recall/provenance UUID nothing references. Only each
family's latest VIEW_RETIRE receipt is kept; an older range is refused as
Stale, because the guest builds the next retirement only from the previous
RETIRED floor; receipts written before q64 name no family and are never
pruned. The ACK archive keeps, per session, only the ACKs of the last
recall it acknowledged: the guest's view control worker sends the next ACK
only after the previous one was ACCEPTED, so an ACK of a later recall proves
an earlier one will never be resent, and such a replay is answered STALE
(its recall is gone and the session's event sequence only grows). Thus the
ACK archive no longer caps lifetime recalls. A sealed session's ticket rows
are left untouched for its terminal seal (its closed leases and reservations
can still be dropped by compaction, as before q64). The ledger only gains omitempty fields
(`family.Scope`, `family.Receipt`, `LineageFences`): a schema-2 ledger written
before q64 opens unchanged, its tombstones still answer RETIRED_ID until the
next VIEW_RETIRE recycles them, and lineages without a recorded fence keep
their revision. Downgrading after recycling is unsupported. Ticket families
themselves stay (per full credentials, bounded by the guest's
HFS_VIEW_FAMILIES_MAX per session).

Stalled recall participants (`participant_fence.go`, task 0a4 after zq3-r11
R11-2). A recall waits for the exact ACKs of every participant and had no
deadline: a guest session that stopped acknowledging (its view worker got
STALE and stopped, `hfs_mmap_fail` told the host nothing, the guest hung)
held the recall, and with it every other domain's cut-changing admission,
forever.

*When a participant is fenced.* All of these must hold (`fenceable`):

- the recall is past RECALL: every participant's STOPPED was accepted, which
  the physical Facts certified (the fixed observer measured the guest
  stopped), quiesceParticipants deactivated every participant lease, and the
  session's STOPPED manifest is protected. A participant missing its STOPPED
  is never fenced: nothing shows that it stopped using its leases;
- the session holds no active lease of any kind, in particular no PAGE lease
  that could authorize a PTE;
- every open reservation of the session is FROZEN in this recall with its
  drain result protected (`CheckFrozenResult`): it has no undrained entry, so
  no dirty byte or frozen call is abandoned. A participant with an undrained
  entry is never fenced;
- it owes an ACK of the current phase (`phaseACKs`, the allAcked conditions)
  and its latest ACK is not BLOCKED within `BlockedACKBound` (a guest whose
  ACK waits for its observation is alive);
- the phase has waited `StalledParticipantBound` (60 s, twice the guest's
  30 s window) since the controller first saw it. The stall clock is memory
  only and restarts on every phase change, reopen, Disconnect and Recover, so
  a restart can delay a fence but never hasten one.

*What the fence does.* One protected write records `Fenced[session]` (recall,
phase, last sequence, its unchanged domain fence), removes the session's
event, learned ACKs and manifest from the recall and, when no participant is
left, ends the recall (no fence advances), so no ledger holds a recall without
participants and a crash leaves either nothing or the whole fence. Every VIEW_POLL
runs the check before progress (each live domain polls, so the peers unwedge
the recall; the poller itself may be fenced, a poll is no progress), and so
does every VIEW_ACK except for its sender, whose ACK is its progress; after a
fence the ACK lets the recall advance before it is checked. The other
participants then install and REOPEN as usual, and finishRecall closes the
fenced session's drained reservations with the rest.

*Afterwards.* The fenced session gets `ErrSessionFenced` (fsapi.Unavailable)
for every request: CheckRequest refuses it lock-free (native commands are
exempt; they belong to no guest domain) and every locked admission path
(ExchangeView, Exchange, acknowledge, admissionBarrier, CheckTarget) checks the
ledger again, so a request that passed the gate just before the fence is
refused too. The endpoint answers the cell with a transport error and never
retries it; the shared transport stays up. events() and fenceExhausted skip
the session for good, so neither the rest of its recall nor any later recall
issues it an event. Its domain fence never advances: its guest never
acknowledged REOPEN, so its old leases are never certified released (the
runtime Facts need the fence advance) and it cannot resume on its stale view.
The runtime Facts' recall participant check leaves fenced sessions out
(`controlledruntime/facts.go` `recallParticipantsComplete`). A reopened
controller reloads the fences (`validFenced` checks them).

*Early DRAINED.* A live participant's DRAINED, sent right after its own
INSTALLED_DRAIN, waits BLOCKED for a silent peer's INSTALLED_DRAIN. Its bound
now runs from that peer's fence point when the peer is fenceable, so it stays
BLOCKED until 30 s after the fence point instead of failing its guest's view
worker after 30 s, before the fence could help; the first ACK after the fence
point fences the peer and the same DRAINED is ACCEPTED.

*Costs and limits.* The fenced session's unretired tickets, lineage revisions
(its fence never moves past them) and families stay charged; it cannot be
sealed (SESSION_RETIRE is refused like everything else). A recall participant
that loses its learned ACKs (Disconnect, reopen) and does not resend its
current-phase ACK within the bound is fenced too; patch 0004 does not resend
INSTALLED_DRAIN today, which before 0a4 wedged such a recall forever. Live
peers' claims and view exchanges parked behind a stalled recall still exceed
the guest's 30 s window before the 60 s fence; the bound is a liveness choice,
the safety argument above does not depend on its value. `Fenced` is
omitempty, so a ledger without a fence keeps its schema-2 bytes; downgrading
after a fence is unsupported (an older binary would serve the session again).
OBSERVE shows the fence in the recall history (`fenced` rows) and in the
ledger evidence (`Fenced`).

*Guest follow-up (not needed for safety).* The guest could tell the host at
once that its view control failed (an explicit abandon from
`hfs_mmap_fail`) so the host need not wait for the bound, and it could resend
its current-phase ACK after RESUME. Both only shorten the wait; the host never
fences on a guest's word alone without the conditions above.

Recall history (`recall_history.go`, task ce4). Because that pruning drops
everything about finished recalls, zq3-r14..r16 could not reconstruct recall
latency. The controller therefore keeps a diagnostic ring of its last
`RecallHistoryLimit` (64) recalls in memory, outside the protected ledger: it
changes no ledger byte, digest, reply or recall decision, and no pruning,
compaction or recycling can drop it; a reopened controller starts empty. Each
record names the trigger (kind, opcode, requesting domain), the scope (`ALL`
or `DOMAIN`) and participants, the start (wall-clock UTC) and, as monotonic
microsecond offsets, every protected phase start with the time its event
issue took, every accepted ACK (domain, phase, sequence, how many BLOCKED
replies and how long the same bytes waited for their physical observation,
and the accepting validation time) and the end. A record keeps at most
`RecallAckLimit` (32) ACK rows of about 130 bytes, so the ring is at most
about 320 KiB plus participant lists (about 160 KiB with three domains).
`Controller.History` copies the ring under the controller mutex and, outside
it, keeps the newest records whose complete encoding fits
`RecallEvidenceBytes` (32 KiB), which the controlled runtime's OBSERVE reports
as `controller.recalls`, together with `advances`, the count of advance
grants (task xf4, see above). That bounds only the history's share of a reply; an
overflow from other sections still takes the documented fallbacks (oversized
OBSERVE refused, RESTART `EFFECT_UNREPORTABLE`).

BLOCKED reasons (`blocked_reasons.go`, task 2h4). A BLOCKED reply does not say
why, and zq3-r19 (R19-1) could not tell an expected-target BLOCKED from a
capacity one without decoding `viewcontrol-v1.db`. The same history therefore
records each BLOCKED decision of `expectedMismatch` (reason
`EXPECTED_MISMATCH`: the stale expectation's retry after its recall) and of
`grantLease`/`shareLease` and acquire's `remember` (reason `CAPACITY`, with
`bound` one of `SOURCES`, `CONTROL`, `IDENTITY`, `LEASES`, `REVISIONS`,
`LINEAGES`; `grantBound` names the first failing bound in `grantFits`' check
order). OBSERVE carries it as `controller.recalls.blocked`:
`expected_mismatch` and `capacity` counters since the controller opened,
`omitted`, and `recent`, the newest `BlockedHistoryLimit` (16) decisions
oldest first, each with `ticket` (the ledger's ticket key, family digest `/`
number), `domain`, `opcode`, `reason`, `bound` (omitempty) and `at_utc`. It is
omitempty, so a history without any BLOCKED encodes exactly as before 2h4, and
consumers that predate it ignore it. Like the rest of the history it is memory
only (no ledger schema change, no reply or decision change; a replayed saved
BLOCKED is not a new decision) and is part of the `RecallEvidenceBytes`
wrapper: about 4 KiB at most, trimmed oldest first before it could crowd out
the bound. Other BLOCKED replies (renewal, reservation, retirement, ACK
waits) are not recorded.

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

Shared lease holders (`lease_holders.go`, task q84). grantLease hands the
active lease of a lineage to every acquisition of that lineage, and the guest
acquires a separate guest lease object per observation, each released on its
own preowned control ticket. The lease state counts the extra holders
(`Sharers`, omitted when zero so unshared ledger entries keep their bytes);
each holder reserves one release credit. For a DOMAIN (observation) lease a
release while another holder remains answers RELEASED without a recall and
leaves the lease active; only the last holder's release recalls and is
certified per lease as above (an observation lease's last holder needs no
recall since je4, see above). Before q84 the first release recalled the
domain under the other observation still in flight (guest ESTALE, refused
admission, as zq3-r9 R9-1). A shared PAGE or OBJECT (cache) lease keeps the
recall path for every holder, because the guest blocks its session on a
RELEASED cache object it still publishes; there a non-last holder's certified
RELEASED only returns its reference and credit.

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
