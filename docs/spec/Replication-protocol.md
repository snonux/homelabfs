# Normal replication, commitment and visibility

Status: coordinated all-origin design contract, task `cz3`, 2026-10-31.
**Unimplemented and unproven.** Task `dz3` must model this protocol before the
successor representation (`ez3`) and runtime integration are accepted. Existing
v1 fixed-owner models, codecs and golden bytes keep their original scope; they
do not implement or prove this contract. Disposable lab/canary volumes only.

[Durability](Durability.md) owns syscall promises; [Transactions](Transactions.md)
owns anchored identities, two-phase decisions and affinity. This document owns
normal ordering, origin durability, memory protection and publication. The
successor wire/storage schemas are not defined by the semantic message names
below. Do not encode them by weakening the v1 owner/reporter role rules in
[Journal format](Journal-format.md) or [Transport](Transport.md).

## Roles and installed authority

For a dependency closure, A is the preferred owner and sole sequencer, B is its
configured distinct owner-origin memory partner, and R is the configured replica
placement. S is the **complete admitted normal-node set** in the installed
cluster authority. Every node O in S may originate a request for every volume,
including O outside R. There is no owner-only or single-writable-client admission
rule. Filesystem credentials, current authority and capacity still apply.

An origin accepts the frontend request, owns its immutable request/payload and
durably commits its own origin journal. A orders/validates that request and holds
the complete committed logical view in RAM. Coordinating with A does not replace
O's durable commit with an owner-local durable commit. For O != A, the required
hosts are O durable + A retained RAM; B is **not** an extra synchronous dependency.
For O = A, they are A durable + B retained RAM. Self-counting is forbidden.
Assigned replicas receive persistent asynchronous delivery obligations in either
case; an outside origin does not silently become a replica or preferred owner.

Installed authority identifies cluster and recovery generation, immutable registry
revision and S, closure/affinity manifest and per-member configurations/epochs,
sequencer node/incarnation, B node/incarnation and the positive-fence lineage.
Membership/closure changes are gated control-plane transitions. S cannot shrink
or forget an old origin's history because a node is offline. Admission of a new
node extends S through that transition before it originates; it needs no volume
replica placement. No etcd read/transaction occurs on normal I/O. Existing etcd
allocation only authorizes starting a transition, never serving or grants.

All participants check this authority at grant, local durable choice, acceptance,
selection and completion. Higher epochs on sockets are not authority. A known
loss of authority, revoked session or lost retention continuity closes the
appropriate gates. No timeout establishes fencing; there is no automatic local-
only fallback. [Emergency](Emergency.md) is a separately authorized exception.

## Session establishment and reconnect

Authenticate observed transport identity and exact cluster/registry revision,
then bind both live daemon incarnations, session/connection IDs, closure authority,
source streams, permitted message roles and negotiated successor format. A new
connection never imports readiness from its predecessor. Exchange exact selected
cut/source manifests, origin durable fragments, unresolved grants and the memory
recipient's retained acceptance ledger; missing dependencies/credits gate READY.
Historical frames keep original creator identities while the authenticated
forwarder is separately authorized. Unsupported versions or conflicting bytes
stop before append, acceptance or serving.

An origin-only restart under a continuously valid A can be readmitted after
positive exclusion of its old lifetime/cache rights, full reconciliation of its
issued grants/choices/source obligations with A's complete ledger, and explicit
installation of its new expected incarnation. It cannot clear an uncertain slot
or substitute a shorter disk. This need not move preferred ownership. Changing
A's incarnation, installing a replacement B incarnation, or a new closure
authority uses the complete transition requirements below. B can remain unavailable
under its old binding while eligible remote O+A operations continue; they cannot
use it for new owner-origin completions. Pure reconnect still proves exact
retained continuity.

## Identities, journals and complete coverage

Keep these identities distinct:

| Identity | Meaning |
|---|---|
| Origin stream | `(cluster, volume, generation, configuration, epoch, origin node, origin journal incarnation)` plus contiguous local sequence; structural frames consume local positions. |
| Operation | Stable origin operation UUID and exact internal claim/intent; explicit application requests keep their immutable principal/anchor/session/request-number identity. |
| Ordered slot | `(closure authority, sequencer incarnation, serial)` with exact predecessor selected-slot certificate and affected per-volume cuts. Serial numbers do not wrap or imply origin-journal offsets. |
| Grant | Exact slot, operation/anchored claim, origin identity, canonical intent, credential snapshot, validated redo/result, predecessor, dependency graph and designated memory recipient. |
| Reference | Complete original stream identity, local position and exact frame hash. Historical forwarding preserves creator bytes; transport correlation is not an operation ID. |

One origin journal may contain fragments for several selected slots; different
origins may both use local sequence 1. Neither local sequence nor highest numeric
slot alone proves committed history. A selected-history manifest maps every slot
to its exact origin fragments, decisions/results and transitive dependencies.
The source vector records actual durable coverage by stream and node, separately
from RAM retention. It includes claims, anchors, authority/grants, payload redo,
object homes, allocation/tombstone/handle ledgers and transaction graphs.

O must durably hold its **complete new operation fragment**: exact grant,
immutable redo and payload bytes, credential/claim binding, durable choice,
acceptance evidence available at that phase and stable result. Its own stream
prefix through that fragment must be durably contiguous. It need not copy the
entire volume's older payloads or become a full replica before writing. Earlier
selected dependencies must instead have explicit pinned durable source coverage
in the manifest. Thus the complete durable history may be distributed across
several origins; no one origin's disk is assumed to contain it all.

Before any new grant, A must possess a verified baseline plus the complete
ordered committed logical suffix and all still-required candidate/dependency
bytes in owned RAM or verified readable durable storage. The designated memory
recipient must retain the granted candidate's complete replay graph and required
selected prefix/baseline coverage before acceptance. Hashes, remote fetch promises
or an incomplete dependency list are not retained data. Origin sync plus RAM
receipt never authorizes deleting any earlier durable source.

Track separate per-stream received R and durable D, exact candidate acceptances,
selected contiguous slot cut K, visible cut V, backing-installed P and backing-
durable/checkpoint coverage. `V <= K`; P is selected-only and cannot jump a hole.
D is not K, a local source position is not K, and RAM projection is not a durable
checkpoint. Report full identities/source vectors rather than scalar maxima.

## Serial grants and conflict resolution

Start with **one outstanding mutating grant per dependency closure**, including
ordinary operations and the closure's transaction coordinator. This deliberately
trades throughput and head-of-line availability for a small, explicit protocol.
Requests from different origins queue fairly with bounded credits; independent
closures may progress concurrently. No range grants or speculative concurrent
redo are part of this contract. Read-only observations of the old visible cut
may continue until their publication gates close.

Before granting, A resolves the original immutable claim at its anchor, then
resolves paths/object homes against one committed logical cut. It validates the
original numeric UID/GID/supplementary groups, mode/umask, traversal and required
permissions there. For an ordinary operation it selects exact revisions, stable object allocations,
append offset, timestamps, short-write byte range and terminal result once;
transaction phases fix their exact prepare/decision/result under this same gate.
Origin-selected allocations use never-reused identities and the closure's
retained allocation ledger. A validates exact redo against the original intent;
O independently checks the grant's authority, own request and payload bytes.
Neither a frontend path resolution nor an origin's stale backing tree authorizes
redo. Replay never re-resolves names, rechecks permissions against a different
cut, resamples time or allocates again.

A installs the exact grant reservation in its owned ledger before sending it.
No successor grant is issued until that slot is selected COMMIT or explicitly
resolved NO_EFFECT, its required result is protected and its publication gates
complete. Consequently two appends get distinct successive offsets; two creates
of one name resolve against the first result; chmod before write changes the
write's authorization; overlapping writes/truncate/rename follow one order.
A denied unclaimed request has no grant/effects. Once a claim is reserved its
rejection/result must use the same protected identity machinery.

An ordinary grant has no time expiry that frees its slot. O may durably choose
exactly one of COMMIT or DECLINE for that grant. Transaction outcome choices
remain the explicit two-phase COMMIT/ABORT rules below. DECLINE records an irrevocable no-COMMIT
promise, including when revocation arrives before a delayed grant. A may resolve
NO_EFFECT only from that exact durable DECLINE and retained matching evidence.
For NO_EFFECT, M retains the exact durable DECLINE, A selects an explicit
no-effect certificate/result, O syncs it, M retains it and A acknowledges its
publication before reply/next grant. The same claim/result/authority checks
apply; the logical view is unchanged but the resolved slot advances order.
A deadline, disconnect, missing record, cancelled wait or absent result does not
close a slot. An unreachable origin can block this closure until reconciliation;
normal operation never skips its hole. Process restart cannot erase the choice.

## Commit and publication handshake

The ordinary-operation exchange below uses authenticated current-incarnation
sessions and exact graph hashes. Local fragment sync and candidate transfer can overlap;
selection cannot precede the indicated durable and retention gates.

1. **GRANTED / STAGED.** O durably records the grant, claim binding and complete
   immutable redo. Initial append/sync and transfer of those exact bytes to the
   memory recipient M (`A` for remote origin, `B` for owner-origin) may overlap.
   M verifies authority,
   predecessor/slot reservation, intent/redo and full dependencies, reserves
   retention credits and copies/owns all bytes. It returns PREPARED_ACCEPT for
   this one candidate and grant. Staging and this receipt expose no effects.
   For O=A, B additionally verifies A's grant against the installed sequencer.
2. **ORIGIN_COMMIT.** After matching PREPARED_ACCEPT, O appends and syncs an
   irrevocable COMMIT containing that exact grant/redo and acceptance identity.
   COMMIT excludes DECLINE for this slot. Sync error/timeout is uncertain, even
   if no receipt reached the caller. O sends the complete commit fragment and
   authenticated durable-coverage assertion; a socket send is not that assertion.
3. **SELECTED.** M validates and retains COMMIT plus its durable-origin assertion
   and returns COMMIT_ACCEPT. For remote O, A can then select the slot. For O=A,
   A must receive B's exact COMMIT_ACCEPT before selecting. The chosen predicate
   is `valid grant + O durable COMMIT/fragment + M retained COMMIT_ACCEPT` with
   O and M distinct. A installs an immutable selected-slot certificate linking
   this evidence and its predecessor. No receipt of redo alone satisfies it.
   COMMIT is never changed to ABORT after selection. Selection is separate from
   visibility; no workload read sees an unselected candidate.
4. **RESULT_PROTECTED.** A sends the selected certificate and previously fixed
   result to O. O appends/syncs the certificate/result, retaining the original
   COMMIT and claim association, then transfers that result fragment to M.
   M owns/retains it and acknowledges RESULT_ACCEPT. A obtains that evidence
   (directly when M=A, from B when O=A). Missing result does not disprove COMMIT;
   reconstruct it from the immutable grant/selected evidence under recovery.
5. **VISIBLE.** A advances K through the slot, publishes its complete committed
   logical state as one generation, and completes the read/cache barriers below.
   Namespace/metadata/permissions and data change together. Backing persistence
   and apply on A may lag. A sends PUBLISHED with the exact cut/generation and
   result identity only after all publication conditions hold.
6. **REPLIED.** O verifies PUBLISHED, its durable result, matching retained result
   evidence and unchanged installed authority/session eligibility, then replies
   with that stable result. Lost reply changes nothing. A opens the next grant
   only after publication; a failed result/cache gate retains the chosen slot
   and gates progress, never converts it to no effects.

These are extra control phases, not a single-round-trip latency promise. M's
acceptance ledger is initially RAM and persists asynchronously. Retention ends
only at verified equivalent durable coverage or a protected explicit transfer;
client reply, disconnect, timeout and sender-buffer reuse cannot release it.
A/M voluntarily draining must persist or hand off every such obligation first.

**Receipt timing is not perfect crash knowledge.** An authenticated acceptance
proves owned retention when emitted in the named incarnation. A delayed receipt
can arrive after an unobserved crash destroyed RAM, even over an apparently live
session. Check known discontinuities and reject receipts once a restart/revocation
is known, but do not assume instantaneous detection or prove current physical RAM
existence from an ACK. An old PUBLISHED reply may likewise arrive after A crashes;
it certifies the earlier publication, not a new serving lifetime. A restarted
node stays gated until recovery. If O's durable source is then also lost before
a durable replacement exists, this is the explicitly accepted durability gap,
including the delayed-receipt schedule; no finite RPO or loss-free proof follows.

## Committed reads, caches and barriers

Every admitted node can accept reads of every volume. Initially nonowners route
the observation to A's committed logical view; they may not answer from their
own stale applied tree. This read routing does not route remote writes into
owner-local durable commitment. A's RAM view covers bytes/extents, overlapping
writes, truncation/zero-fill/holes, sizes, object and entry revisions, directory
entries, links/aliases, symlinks, permissions, timestamps and open-unlinked/home
lifetimes. It combines a verified durable baseline and ordered committed overlay;
this is not a bytes-only cache. Planning and access checks use the same view.

A captures V under the closure gate for each read/lookup/getattr/readdir/access.
The answer is an atomic observation of a committed cut at least as new as that
capture, never staged or partially applied backing data. A read invoked after a
completed write must include it. Reads concurrent with publication may linearize
before or after it. Generation-bound handles and foreign aliases carry home and
closure dependencies. No raw backing-file passthrough bypasses this rule.

The protocol requires a positive frontend view barrier for **all admitted reader
and writer domains**, including independent FUSE clients and every cached alias,
9p mount, descriptor and mmap domain. Before exposing a new cut or returning
success, close conflicting old-generation observations, drain in-flight reads,
invalidate/update their data and metadata, and obtain positive acknowledgements
before reopening them on the new generation. Entry TTL zero and daemon gating
alone do not intercept cache hits. Dirty mapped/writeback data must be drained
through ordered origin requests before a conflicting grant is frozen; the
frontend must stop new conflicting dirtying while that gate is held. The grant
wait must not hold a lock needed by such drain requests. Failure to drain or
positively exclude a domain gates the affected closure; a timeout cannot stand
in for an acknowledgement. A disconnected client is still a domain to fence.

This is a required interface, **not an implemented cache algorithm or evidence
that native FUSE/9p can supply it**. `fz3` and the platform gates must choose and
validate concrete mechanisms, including mmap, before normal publication. A
platform lacking them remains unsupported, rather than gaining stale reads or
a blanket single-client substitute. Same-guest advisory locks remain application
coordination only. Crash takeover additionally requires positive old-domain
fencing; an etcd lease/UUID/TTL provides neither barrier.

File, directory and conservative volume BARRIER requests enter the same sequencer
queue as grants and capture an exact selected cut/source graph across **all
origins**, not merely the caller's local stream. A barrier waits for preceding
slots/results/publication and protects its own origin-local fragment plus the
designated distinct memory copy. A barrier is protected nonmutating evidence,
not a second mutation/recursive COMMIT: A holds the queue cut while O syncs its
exact BARRIER/cut/result and M retains them, then returns that attested result
before the next grant. It covers foreign-home and transaction decision requirements
transitively. Later unsynchronized writes are excluded. O_SYNC,
O_DSYNC, fsync/fdatasync and directory fsync use [Durability](Durability.md); no
second durable copy or implicit cross-file transaction is added. Requests not
yet submitted by a buffered guest require the verified frontend drain contract.

## Claims, unknown results and transaction composition

Protocol retries retain exact operation, grant, source bytes and anchored claim.
The first lookup is always the immutable anchor/session/floor/claim, before path
resolution or coordinator choice. Changed canonical intent is ID_CONFLICT;
retired IDs return RETIRED_ID using protected permanent floor/tombstone evidence.
Retry via another origin first resolves the existing binding: it neither moves
the original durable-origin role nor makes a second grant for the same request.
After admission, interruption/cancel/client death detaches response waiting only.
Report PENDING/INDETERMINATE until the original slot resolves. A fresh syscall ID
may have additional effects; arbitrary application retry is not exactly-once.

The state machine owns admitted immutable work beyond request-context lifetime.
An ordinary COMMIT proposal with lost response is never retried as a fresh append
or aborted on a timer. If exact status cannot be recovered, preserve it and gate.
Cancellation can request the mutually exclusive durable DECLINE only before O
has chosen COMMIT. Terminal status uses the selected result even if no client
reply was delivered. Explicit session retirement cannot pass pending claims.

Transactions retain immutable anchors/REQUEST_BIND, participant homes, protected
PREPARED records, one coordinator decision, all-prepare-before-COMMIT, INSTALL,
VIEW_BARRIER/COMPLETE, exact-ID recovery and grow-only closure affinity. Their
common A/B/R placement remains an ordering/availability constraint, not an origin
restriction. The admitted transaction origin O durably holds the complete new
multi-volume operation graph (claims/bindings/prepares/decision/results); each
participant record names its exact origin fragment and selected per-volume cut.
Protection of every semantic record uses O+M above, with no recursive COMMIT
wrapper. A's closure gate serializes the entire transaction against ordinary
operations from other origins. COMMIT selection still requires every bound
prepare; ABORT uses the existing explicit transaction decision and protection
rules, never a timeout. No participant publishes until protected installation
and the shared closure visibility barrier complete. Ordinary cross-mount
rename/link remain EXDEV. Detailed successor transaction schema/runtime work
remains downstream; the old owner-stream ledger cannot be wired in unchanged.

## Outside origins, recovery and authority transitions

This two-host boundary **does not have the old fixed three-replica majority
intersection proof**. O outside R may hold the only durable new fragment; two
ready placement replicas may have neither it nor its unresolved grant. S and its
lineage are therefore an obligatory recovery-source inventory, not telemetry.

Before changing sequencer incarnation, epoch, membership, closure authority or
owner-origin partner, recovery must:

1. Gate the whole affected closure. Preserve live RAM tails before voluntarily
   stopping any host. Obtain serialized control-plane transition allocation;
   positively close/fence every old sequencer, every node in old S that could
   hold a grant, every used owner-origin memory partner and every reader/cache
   domain. Persist nondecreasing transition promises before acknowledging close.
   A physical fence may establish exclusion; it does not supply a journal report.
2. Obtain a complete authenticated **sealed close report from every node in old
   S**, plus all sequencer/partner ledgers used in the authority lineage. The
   report follows local serialization with pending appends/syncs, gives the exact
   durable stream tails and retained accepted/selected tails, grants/DECLINEs/
   COMMITs/results, continuity/lost-RAM statements, source/pin obligations and
   authority promises. It promises no more old-authority choices or serving.
   A node without an operation supplies a durable empty/closed report, not an
   inference from silence. Preserve reports and their bytes durably at the
   recovery participants. A missing node's report can be replaced only by equivalent verified complete
   durable coverage plus positive exclusion: exact decisions/results/dependencies
   for all its grants and proof of the complete issued-grant frontier (for example
   A's continuously retained, now sealed grant ledger, or an already durable
   closure manifest). A checkpoint maximum or known selected prefix alone cannot
   exclude an additional issued grant. An unresolved grant with a potentially
   unique durable choice cannot be replaced by silence. Thus a permanently dead
   origin is replaceable when all obligations and the frontier are independently
   proven; fencing alone supplies no missing data or absence evidence.
3. Union exact grant and slot/predecessor evidence, origin choices and source
   fragments across those reports. Resolve every slot and all transitive homes,
   anchors and dependencies before new admission. Preserve any selected COMMIT.
   A durable COMMIT whose selection is unknown is conservatively adopted with
   complete dependencies and reprotected; it may have committed without a reply.
   Conflicting same-key bytes, incompatible grants or COMMIT versus DECLINE
   quarantine. Proven closed absence of a COMMIT may resolve NO_EFFECT only
   after the complete survey or its verified equivalents, never from a placement
   majority or missing result.
4. Materialize the complete recovered cut, exact results/claims, source manifest
   and all required dependencies durably on the prospective A and its distinct
   B before activation. Restore the logical view and cache/handle generation,
   retain offline replica/backup obligations, install the new authority/source
   set and only then reopen grants/observations. Old origin fragments remain
   pinned until verified durable replacements and protected release exist.

No endpoint is readmitted with only a shorter durable tail and fresh UUID. A
restart of O/A/M loses old session readiness; reconstruct all its bound grants
and selected dependencies before it participates again. New incarnations do not
prove an old acceptance never existed. Pure transport reconnect without restart
can resume only after exact ledger/retention reconciliation; no peer may discard
accepted state on socket loss. Known loss of the required retention gates fresh
completion until the same evidence is reprotected under reconciled authority.

This deliberately conservative survey can block on an offline admitted node,
even an outside origin with no known selected write, when its complete grant
frontier/obligations have no verified surviving equivalent. It is the availability cost
of avoiding an additional mandatory third participant on the normal remote path.
No finite recovery-time guarantee or automatic majority promotion is claimed.
Missing storage/continuity may require explicit authorized loss recovery with a
new generation and preserved copies; it is not a proven ABORT. A later optimized
source-registration/removal or takeover protocol needs its own specification and
proof. Task `gz3` supplies detailed crash-recovery integration; old
[Takeover](Takeover.md) is retained scoped v1 machinery, not a sufficient survey.

Cooperative demand-based preferred-owner handoff is distinct from crash takeover
and replica redistribution. Periodic policy uses bounded fresh original-request
counts, hysteresis and tenure/cooldown. A cooperative handoff gates/drains grants,
transfers the full committed RAM projection, source/pin/dependency manifests,
claims, handles and unresolved results, closes old authority with positive domain
acknowledgements, then activates the new sequencer. It need not stop a VM when
cooperation proves closure. This cooperative transition uses the same complete
closed-origin/source coverage requirement, with live positive close acknowledgements rather than
mandatory VM stops. Already verified durable origin coverage can remain
at the origins; it need not be copied to the new owner's disk. Read readiness
may therefore precede new-owner persistence/apply. Crash recovery instead uses
the durable A/B materialization gate above. Every still-admitted origin retains
write eligibility in the new authority. Detailed handoff policy/state machine
is `kz3`; do not reuse a crash-fence operation as the
routine demand-balancing mechanism or move one member of an affinity closure.

## Retention, resources and model obligations

Async delivery to every assigned replica derives from durable origin fragments
and selected-source manifests, not a disposable send queue. Report per-source
received/durable/selected/installed coverage and explicit holes. Reconnect sends
exact original frames; unknown suffix/base installs a pinned verified checkpoint
into a private gated destination. No timestamp, RAM ACK, apply maximum or empty
send queue authorizes GC. Checkpoints bind a global cut, all origin source cuts
and exact dependency graph. External snapshots/holds and backup source pins
remain authoritative. Missing coverage preserves data and throttles admission.

Reserve bounded credits before grants for origin staging/sync/result fragments,
A's complete logical overlay, M's accepted graph, retries, reader domains and
recovery/control reserve. Fairly queue origins/volumes, bound client waits and
background workers, and persist admitted obligations beyond client cancellation.
Use persistent control/live-data/bulk classes with one framing writer per socket;
all classes authenticate authority and bound messages/bytes/connections. Slow
origin, partner or offline replica eventually backpressures its affected closure.

Concrete conflicting and uncertain histories for the successor model:

| Trace (all slots share one closure authority) | Required outcome |
|---|---|
| File initially `abcd`. O1 wins slot 1: overwrite offset 1 with `XY`; O2 wins slot 2: truncate to 2, then slot 3 extends to 4. | Reads after slot 1 see `aXYd`; after slot 3 see `aX` plus two zero bytes, including on A before disk apply. A staged slot 2 cannot affect a slot-1 read. |
| Empty file. O1 and O2 concurrently append `aa` and `bb`; A grants O2 first. | O2 gets offset 0, O1 gets offset 2, final bytes `bbaa`; retrying O2's same claim cannot append again. |
| A grants O1's chmod removing U's write permission; U's O2 write was already queued. | A publishes chmod first, then authorizes the O2 request against that new cut and denies it. O2's earlier cached mode cannot supply authority. |
| Outside origin O3 durably COMMITs slot 4; A selects/publishes it only in RAM; reply is lost; then A crashes. | O3's COMMIT/result and all earlier origin dependencies preserve the selected history. R's two surviving replica disks alone cannot declare slot 4 absent. Missing O3 with no equivalent complete coverage gates recovery; same-ID retry remains unknown until resolved. |
| O1 receives slot 5 then pauses before any durable choice; O2 requests slot 6. | O2 waits. Timeout cannot free slot 5. O1's exact durable DECLINE permits protected NO_EFFECT; its delayed original grant cannot later COMMIT. |
| O=A syncs COMMIT; B emits COMMIT_ACCEPT then crashes; its old receipt arrives before A learns that fact. A can finish before that notification. | Receipt certifies B's earlier retention. Losing A's unique disk too may lose the result under the accepted durability gap. A new B incarnation cannot use its empty tail to prove ABORT or regain readiness. |

Task `dz3` must check at least these concrete traces, with delayed messages and
unobserved RAM loss, independent backing apply and an external reply/read oracle:

- O1/O2 overlap writes to one offset; append/append; create/create; rename/unlink;
  truncate then extension; chmod then an otherwise permitted write. One grant
  order determines redo, permissions, offsets and observations.
- O1 gets a grant then pauses. O2 cannot leap the hole. A deadline cannot create
  NO_EFFECT; a durable DECLINE can. A delayed grant after DECLINE cannot commit.
- O1 outside R syncs COMMIT; A selects in RAM while its disk/apply is blocked.
  A read and O2's next planning cut include the result. Staged O2 is invisible.
- Lost PREPARED_ACCEPT/COMMIT_ACCEPT/SELECTED/PUBLISHED/result reply; duplicate
  exact request through O2; A or O restart between phases. Preserve original
  choice and result; conflicting canonical intent and reused grant are rejected.
- Owner-origin rejects self memory; B's pre-crash acceptance arrives late after
  unobserved RAM loss. Model the accepted durability loss case without perfect
  detection. Known restart cannot restore old readiness or prove tail absence.
- A crashes after interleaved O1/O2 durable fragments. Survey all S, including
  outside R; a missing report without equivalent complete coverage, or a missing
  dependency, prevents activation. Two placement replicas and a new etcd epoch cannot invent the missing source.
- A transaction/foreign home and a concurrent remote chmod/write/barrier share
  the closure order and one view barrier. Missing prepare/result/source pins
  cannot yield partial namespace/data visibility or permit retention.
- Two independent reader/cache domains, delayed invalidation and writable mmap;
  no post-success stale result, speculative read or cache bypass. A failed
  positive fence/barrier blocks publication/activation rather than weakens it.

Conditional progress assumes stable installed authority, available O/A (and B
for owner-origin), fair finite load/delivery/workers, sufficient capacity and
working frontend barriers. Failed origins or unknown close reports can block;
client wait deadlines do not weaken safety. Passing old v1 profiles or this
prose review proves none of the new distributed/runtime/kernel obligations.
No test or lab execution is authorized here; each run retains
[Test automation](../Test-automation.md) approval requirements.
