# Normal replication, commitment and visibility

Status: design contract for task `dq3`, 2026-10-28. This is an unimplemented
protocol for disposable lab/canary volumes, awaiting the `fq3` model and runtime
validation. It is neither a safety proof nor behavior of `tools/poc/`.

[Journal format](Journal-format.md) owns exact record bytes, stream keys and
limits; [Transactions](Transactions.md) owns anchored identities, affinity and
two-phase decisions; [Backing store](Backing-store.md) owns redo/checkpoints;
[Durability](Durability.md) owns successful-return and read-freshness promises.
This document specifies their fixed-owner transport and state-machine composition.
The concrete takeover/fencing mechanism is `eq3`; the recovery evidence required
from this protocol is specified here without assuming that mechanism exists.

## Scope and authority

Let A be the current owner, B its configured synchronous partner, and C the
asynchronous replica. Membership, owner, partner, recovery generation,
configuration, epoch and coordinator term are already authorized, reconciled and
installed. An initial ready pair has a verified baseline and complete selected
suffix. Every closure member has the common owner/partner and compatible
membership required by Transactions. Different groups keep distinct histories.

Only A originates owner-stream records. Only A serves workload I/O, from its
local object homes. B and C are passive even after application. Neither a larger
epoch supplied on a socket, an etcd response, nor a heartbeat creates serving
rights. Authority is checked at admission, acceptance, publication and completion
using the installed validated authority and durable promises, without a per-write
etcd lookup. Authority loss closes serving/admission gates and enters recovery.
Old cached/client access needs the separate positive fencing contract.

There is no automatic partner replacement or one-node mode. If B is unavailable
or cannot retain the required prefix, new protected completions wait, with bounded
queues and client deadlines. C cannot substitute merely by being connected.
Changing the pair, membership, epoch or history requires the fenced recovery/
configuration protocol. Emergency mode is separately authorized and outside this
normal state machine.

## Streams, progress and decision evidence

An owner stream is `(volume, generation, configuration, epoch, owner-kind)`.
Reporter evidence adds `(reporter node UUID, reporter incarnation UUID)` and
uses its own sequence space. Journal `Position` is interpreted together with
this full stream identity; a bare sequence is never a resume token. SEGMENT_OPEN
and SEGMENT_SEAL consume positions. ACCEPT, PROMISE and TAIL_REPORT occur only
in the reporter's evidence stream; B must never allocate an owner sequence.
Historical bytes are copied exactly, including their original header incarnation.

Each replica tracks these distinct facts for each stream:

| Fact | Advancement rule |
|---|---|
| Received R | Complete, validated, contiguous frames from a verified baseline, with all required graph/payload dependencies available in owned memory or verified durable coverage. An out-of-order frame alone does not advance R. |
| Durable D | The complete prefix, segment/name publication and recovery dependencies have passed storage sync barriers. D <= R; buffered append is insufficient. |
| Accepted | Exact slot/candidate/term ACCEPT records, not a scalar vote count. A tail summary records contiguous coverage only; it cannot replace the per-slot acceptance ledger. |
| Decided K | Contiguous owner history whose decision/protection predicates below have been verified, including explicit resolved aborts. Known later decisions cannot bridge an unresolved earlier mutation. |
| Applied P | Complete ordered decided redo through this position, reconstructed from the selected checkpoint; P <= K. Receive/durability alone cannot permit apply. |

R/D/K/P reports also carry their history, reporting node/incarnation and baseline;
when no same-history ordering exists, report separate streams, never compare
numeric maxima. Reporter streams have R/D progress but no filesystem applied
effects. A received frame with missing graph dependencies remains pending outside
R. A bounded pending queue may reject transport work and request retransmission.
It cannot emit a receipt for missing bytes.

Call an owner record **protected** when A durably holds its complete required
prefix/dependencies and B retains those same complete bytes/dependencies under
the current session, with a matching receipt. This does not make a MUTATION,
PREPARED, or unselected DECISION into visible committed state. Protection also
applies to SESSION, claims, pins, barriers, results and other semantic records;
they do not acquire a new COMMIT wrapper or their own decision slot.

An ordinary slot is identified by the MUTATION's full history and stable operation
UUID. Exactly one COMMIT candidate references that mutation in its normal term.
A's durable COMMIT is a candidate until the following predicate is satisfied:

`chosen(COMMIT) = valid authority + A durable complete COMMIT prefix/graph
+ B current-term ACCEPT(COMMIT, exact graph/prefix)`.

The local durable append and B acceptance need not happen in that order. B may
accept before A's sync completes; it does not claim A is already durable. If A
fails or times out there, B's acceptance still constrains recovery. A never
reinterprets failure to collect a receipt as permission to propose an opposite
outcome. The old designated writer sends at most one outcome candidate per slot
and term. A normal ordinary mutation is committed through COMMIT; an ordinary
abort after uncertain admission requires a safe RECOVERY_SELECT resolution,
not a locally invented ABORT record.

A transaction slot is `(bound coordinator history, RequestID)` with coordinator
term identifying the ballot. Replace ordinary COMMIT above with the exact
DECISION(COMMIT or ABORT). COMMIT additionally requires all bound protected
PREPARED records and their dependencies. Each normal term has one designated
coordinator writer and one outcome candidate. Protection/chosen decision is
irreversible within the authoritative history; failures after COMMIT gate
installation/publication rather than select ABORT.

ACCEPT asserts B's owned retention, candidate identity, term, authority and
complete dependency coverage, with separate retained and durable vectors. Its
durable vector may lag. It does not assert filesystem apply or client success.
Only B's configured current-incarnation acceptance counts for a new normal
completion. C's receipt is catch-up progress, never a synchronous acceptance vote.

The evidence graph is deliberately acyclic:

```text
MUTATION <- COMMIT <- B reporter ACCEPT <- ORDINARY_RESULT
PREPARED <- DECISION <- B reporter ACCEPT <- INSTALL <- COMPLETE <- REQUEST_RESULT
```

Arrows mean "is referenced by". COMMIT does not contain its own ACCEPT; DECISION
does not contain its own acceptance or terminal visibility. Later result/install
records carry exact ACCEPT frames or references to their retained graph, and
plain prefix receipts protect those later records. They never need an ACCEPT
that refers back to the result being protected. Evidence bundle embedding obeys
Journal-format's no-nesting rule; referenced graphs may be fetched separately.
A checksum or ACCEPT digest without the actual resolvable graph is insufficient.
The later result protection adds a covering receipt/wait beyond the candidate
acceptance; this is no one-round-trip latency promise. B may still retain all
of it in memory, so it does not require a second durable replica.

## Session establishment and reconnect

Use the [Security](../Security.md) trusted-host WireGuard mapping and explicit
admission/volume-role checks on every connection. A claimed UUID is checked
against observed authorized transport identity; WireGuard membership alone
does not enroll a node. No fallback to cleartext routes or foreign volumes.

The following is the normative logical handshake. Its bounded binary codec and
numeric message tags remain implementation work; journal payloads use the exact
existing journal bytes and encodings, never a second serialization of a record.
All limits are checked before allocation. Initially support protocol 1.0 and
journal 1.0 only; reject unknown versions/features rather than silently weaken
semantics.

1. Both endpoints send HELLO: supported protocol/journal versions, cluster UUID,
   node UUID, fresh daemon incarnation UUID, registry revision, connection UUID,
   proposed connection class, and finite receive message/byte/operation limits.
   Authenticate identities and membership first. A new incarnation boots RECOVERING;
   its UUID does not by itself supersede an old daemon or certify its tail absent.
2. HELLO_OK selects exact versions, the lower compatible limits, both endpoint
   identities/incarnations, and a fresh shared session UUID. The control session
   owns subsequent volume bindings. Additional live/bulk connections repeat HELLO
   and bind that exact session and both incarnations; no unbound data or ACK is valid.
   Either endpoint may reject with a precise reason and close.
3. A sends OPEN_STREAM for each authorized volume: full Authority, affinity and
   serving-generation reference, authorized baseline/checkpoint, required old
   history references, intended role (sync/async/recovery-only), and proposed
   ordered stream binding. B/C validates installed authority and promises rather
   than trusting the supplied values. A receiver lacking that authority requests
   reconciliation; it cannot learn serving rights solely from OPEN_STREAM.
4. Receiver replies RESUME: exact stream keys, verified baseline, R/D/K/P,
   prior candidate and ACCEPT references, gaps/dependencies, current promises,
   credits, and continuity state. Continuity is true only if this daemon still
   owns the complete previously accepted tail or has verified equivalent complete
   durable coverage. Restart from D alone reports false/unknown. Missing frames
   on a restored disk or new incarnation never certify old absence.
5. Reconcile actual frames/hashes at overlapping boundaries and required history
   links, then transfer missing suffix/evidence or a pinned verified baseline.
   Divergent same-key bytes or incompatible authority quarantines the stream.
   Reconnect cannot discard an uncertain tail, reset promises or select history.
   A receiver with a conflicting old suffix stays gated for recovery.
6. READY names the exact reconciled stream binding, authority, baseline/cut and
   current credits. A confirms READY before sending newly admitted candidates.
   Sync readiness includes B's complete required owner/dependency prefix and
   accepted-tail reconciliation. Role changes need configuration authority;
   READY does not promote an async or recovering node.

Per-volume state is CLOSED -> NEGOTIATING -> RECONCILING -> READY, with WAITING
for temporary capacity/transport shortage, RECOVERING for authority or missing-tail
resolution, and QUARANTINED for corruption/conflict. Waits have bounded retry
backoff and request deadlines; states preserve accepted work. Protocol connection
closure does not close a session's retention obligations. Reopening a socket in
the same live incarnation can reuse verified memory after a fresh handshake.
If incarnation changes, old ACCEPTs remain historical evidence but cannot serve
as fresh receipts without continuity/reconciliation and current retention.

Old-incarnation frames are allowed only as authorized historical transfers,
with exact immutable bytes, selected history and dependency pins. They cannot
originate new proposals or fresh receipts. A delayed live message from an old
session/incarnation is rejected even when its journal checksum is valid.
The immutable frame's creator incarnation is validated against its original
history/authority, while the current authenticated transport incarnation is
validated as an authorized forwarder. They need not be equal. Retransmission,
catch-up and post-promotion forwarding must not rewrite the creator header.
An ACK delivered on a surviving control socket after a data socket dies is valid
only if it matches an actual retained frame under the still-valid binding.

## Message contract

Every post-handshake envelope carries selected version, type, bounded total
length, session UUID, connection/binding UUID, authenticated source/destination
node and incarnation, and correlation UUID. Stream messages also carry full
stream key and Authority reference; slot messages carry stable operation UUID,
candidate FrameRef, term and, for transactions, RequestID/digest/immutable binding.
Validate these against the binding on every dispatch, including acknowledgements.
Correlation UUIDs match waits; they are neither operation identities nor evidence.

| Message | Required body and behavior |
|---|---|
| HELLO / HELLO_OK / OPEN_STREAM / RESUME / READY | Fields and sequencing above. No serving or acceptance before readiness. |
| DATA | One complete exact journal frame with FrameRef, predecessor boundary and required dependency references. Can transfer owner or reporter frames; source must be authorized to supply that history. B/C verifies kind, length, CRC, hash, sequence and semantics. Sender batches only within admitted enclosing bounds; no partial frame is receiptable. |
| ACCEPT_REQUEST | Exact candidate FrameRef, slot kind, term, authority and required prefix/dependency cut. May accompany DATA logically; acceptance waits until every referenced byte is retained. Only configured A may request B's normal acceptance. |
| ACCEPTED | Exact B reporter ACCEPT frame and its stream predecessor/graph references. B generates it after validation/owned retention, reserves evidence-stream space and retains it across disconnect. Retransmission returns the same acceptance identity; it cannot create an alternative value. |
| RECEIPT | Target stream/boundary FrameRef (or verified baseline for sequence zero), R, reporter incarnation and retained dependency vector. Reports receipt of actual complete bytes. A plain RECEIPT never replaces ACCEPTED for a commit/decision slot. |
| PROGRESS | Per-stream R/D/K/P, baseline, candidate/acceptance references, continuity and missing dependencies. Persist catch-up tracking as REPLICA_PROGRESS. Claims are verified against known history; never use reported R/P as durable GC coverage. |
| NEED | Stream, next expected frame/predecessor, bounded missing FrameRef/artifact list or required baseline cut. A gap causes resend/catch-up, never an ACK past the gap. |
| FETCH / ARTIFACT_CHUNK | Authorized immutable artifact identity, total length/hash, bounded offset/range and transfer-pin identity. Validate staged bytes and typed codec before installing; a complete hash alone is not a valid checkpoint. |
| CREDIT / WAIT | Per-volume/peer byte and operation allowance, reason, latest safe progress and retry hint. Credits are flow control, not storage durability or authority. |
| STATUS / STATUS_REPLY | Exact internal operation or anchored RequestID/digest; return recorded stage/result or uncertainty. STATUS never executes an unknown request. Application queries first route to anchor. |
| ERROR / CLOSE | Precise unsupported/unauthorized/stale/gap/conflict/capacity/I/O reason, affected binding and known stage. Closing drains or retains accepted state; it cannot imply abort. |
| PING / PONG | Bounded service-progress heartbeat, no serving lease or fencing meaning. |

No independent COMMITTED wire assertion is authoritative. Peers learn decisions
by receiving COMMIT/DECISION plus acceptance/result/install evidence and verifying
the predicates. All metadata, namespace, payload and result records are delivered
to C as well as B; no metadata-only best-effort channel exists. Reporter evidence
must also reach replicas that need the decision graph. Source reporter identity
is preserved when another authorized peer forwards it.
ACCEPTED may inline its exact nonstructural frame, but the receiver also obtains
and retains the reporter stream's preceding OPEN/boundaries and required prefix
through DATA. An inline ACCEPT must not advance reporter R across a gap. Embedded
Evidence never acquires forbidden structural frames to work around that rule.

## Owner and partner execution

For an ordinary single-group mutation, A executes these transitions:

1. **ADMISSION:** check serving rights, readiness, permissions, fresh revisions,
   gates and global/peer/volume capacity. Reserve complete redo, evidence, result,
   retained buffers and recovery space before effects. Resolve paths once under
   locks, choose immutable IDs, append offsets, times and metadata, and allocate
   one internal operation UUID. Before effects protect the internal session and
   exact anchored InternalRequest claim; retries use that same claim. The
   single-group baseline places the immutable anchor in that group. Work that
   touches another home/anchor uses Transactions and its immutable REQUEST_BIND.
2. **ORDERED:** one volume state machine assigns consecutive owner positions to
   MUTATION and COMMIT under the installed term. The COMMIT references the exact
   MUTATION. Dependencies include the already protected claim and all selected
   prior object/namespace/payload history. Later operations may be encoded/transferred
   concurrently, but cannot commit or apply over a missing earlier decision.
3. **PROTECTING:** append/sync the full local prefix and send DATA/ACCEPT_REQUEST
   to B in parallel. Start delivery to C concurrently without waiting for it.
   Do not mutate published backing objects. A waits for its covering durable cut
   and B's matching ACCEPTED, in either order; a RECEIPT of MUTATION alone fails
   the predicate. Group sync may cover several operations without bypassing
   each candidate's acceptance/dependency checks.
4. **CHOSEN:** once the predicate holds, COMMIT cannot be undone. Preserve B's
   exact ACCEPT and graph; append/protect ORDINARY_RESULT containing that graph,
   stable result and accepted byte range. Its protection uses plain prefix
   RECEIPT, not a new recursive decision. Advance decided K only through resolved
   predecessors. A crash before the result record is reconstructed from candidate
   and acceptance evidence; absence of result is not absence of COMMIT.
5. **PUBLISHABLE:** authorize ordered apply behind object/namespace gates.
   An ordinary data write may publish its logical committed result and return
   while backing apply is pending only when the admitted platform profile proves
   freshness for every affected cached alias and mapping during deferred apply.
   Daemon-mediated observations capture a cut including that result and wait for
   complete apply, but kernel/guest cache hits and mmap loads can bypass these
   gates. Without that verified deferred-apply guarantee, await complete apply
   and the required cache update/invalidation/drain before publication or success.
   Never expose stale post-completion bytes or partial payload redo through any
   supported observation path. This condition does not select a concrete cache
   mechanism or establish that a profile has passed validation.
   Namespace changes apply their complete entry/object/parent/ledger
   change atomically under gates and complete the admitted cache invalidation/drain
   barrier before reopening views and reporting successful namespace completion.
   Cross-group publication uses protected INSTALL/VIEW_BARRIER/COMPLETE and one
   closure generation as Transactions requires.
6. **REPLIED:** recheck serving authority and completion predicates, then reply
   with the stable protected ORDINARY_RESULT indexed by the internal claim. A
   positive response never outruns required claim/result, decision or prefix
   protection. Its ordered predecessor mutations must already be decided/resolved;
   K cannot remain behind an unresolved predecessor while this reply succeeds.
   Failed reply delivery changes no outcome. Queue asynchronous
   work through persistent history obligations, not a detached best-effort task.

The internal anchor result uses the Transactions terminal schema through its
internal request machinery when the operation is multi-group. For the single-group
ordinary path, ORDINARY_RESULT is the typed terminal receipt for the MUTATION and
the anchor's claim indexes it. The REQUEST_CLAIM, MUTATION, COMMIT and
ORDINARY_RESULT share the one command's header operation UUID; the claim's exact
InternalRequest intent must agree with the mutation. Its dependencies retain
the claim FrameRef, and retry/status first resolves that same protected claim
to the command/result, never infers an unused ID from a missing result. Do not
invent a transaction DecisionRef or encode an ordinary COMMIT as a DECISION.
The checkpoint must preserve the full claim-to-command-to-result association.

B executes WAITING_FOR_FRAME -> VALIDATED -> RETAINED -> ACCEPTED, with persistence
and application as separate workers. Validate current binding/authority/promise,
expected term and single candidate, exact redo/claim consistency, contiguous
stream and complete dependencies before RETAINED. Copy bytes or transfer exclusive
ownership into its retention arena; caller/network buffers are insufficient.
Reserve credits for the entire recoverable prefix and reporter acceptance.
Emit ACCEPTED only after retaining that arena and recording the exact acceptance
in its reporter stream. That stream may be volatile initially. On duplicates
compare full bytes/identities and return the existing ACCEPT; conflicting same-key
bytes or same-term candidates quarantine instead of overwriting.

B asynchronously appends/syncs owner and reporter history, advances D only after
the complete barriers, and applies only decided K. If acceptance arrives before
A's local durability/result, B retains a candidate and waits for decision evidence;
it cannot publish or discard it. B can free memory only after equivalent verified
durable prefix/graph/payload coverage or a protected acknowledged recovery handoff
replaces it. Client reply, transport ACK, a timer or connection close is not a
release condition. C uses the same validation, ordered persistence and apply
rules, without synchronous acceptance authority.

## Barriers, retries and request lifetime

File/directory/volume BARRIER records capture a cut under admission order; they
cover prior ordered work and foreign-home/decision dependencies. Protect the
barrier and wait for its captured mutations' commitment/protection. Apply may
lag where Durability permits, with reads gated on the captured cut. O_SYNC,
O_DSYNC, fsync, fdatasync and directory fsync retain the exact Durability promises;
none waits for a second durable replica. A barrier does not include later
unsynchronized writes or become an implicit cross-file application transaction.

Before admission, validation/capacity failure proves no new effects. A protected
claim already reserves the exact request and records a protected rejection.
After claim/admission, a lost wait, error, client death, interrupted callback or
deadline returns PENDING/INDETERMINATE as appropriate; it does not rewind journal,
free dependencies or promise rollback. Native adapters may map uncertainty to an
appropriate error, but must document that retrying a new syscall identity may
have additional effects. Protocol retries keep the original ID and exact bytes.

Application Execute/GetStatus/Cancel first consult the immutable anchor's
session/floor/claim before paths or coordinator selection. A changed canonical
request is ID_CONFLICT. Internal requests compare exact immutable InternalRequest
including requested mode/umask, SETATTR values and selected metadata. Retry never
samples new times, reallocates an object, chooses a new append offset or adopts
another request's redo. Internal identities do not give arbitrary application
retries after restart exactly-once behavior.

Cancel requests abort before COMMIT proposal; once COMMIT has been proposed,
finish or recover the actual outcome, without choosing a second value in the
term. Participant/coordinator timeout is not safe abort evidence. Cancelled waits
detach response delivery only. If an admitted ordinary proposal needs abort,
safe fenced resolution is required; no timeout-generated success/no-effects errno.
Terminal retries return the recorded result or RETIRED_ID. Explicit protected
retirement/closure retains permanent floor/tombstone authority; no time-based
dedup expiry or anchor reassignment.

Request lifetime belongs to the state machine from claim/admission through
terminal resolution and durable replacement of its retention obligations. A
request-scoped context controls admission/response waiting, not already accepted
background replication. Once admitted, a service-lifetime worker owns completion,
recovery and retry. Local caller payloads may be released only after the state
machine has immutable owned bytes. A socket writer owns framing until its send
completes; send completion does not release B's accepted-retention obligation.
Use one framing writer per socket, bounded queues and explicit credit return
when actual retained memory is released. Draining/shutdown persists or safely
hands off accepted state before voluntary exit; abrupt process loss remains the
accepted volatile-memory failure case.

## Persistent catch-up, pins and connections

For each assigned replica retain a durable REPLICA_PROGRESS/baseline obligation
and the selected history needed after it. New admitted records extend that
obligation by their presence in the authoritative journal; there is no per-record
memory queue whose loss cancels delivery. Restart reconstructs work from durable
progress and journal/checkpoint history. A lost PROGRESS reply causes conservative
retransmission. R or P never advances durable coverage. Offline members retain
their obligation across restart, pair change and ownership transfer.

On a gap use NEED and retransmit the next expected prefix plus evidence. For a
duplicate position accept only identical complete stream-key/sequence bytes.
One command deliberately shares its operation UUID across its specified claim,
mutation, commit and result records at different positions. A second MUTATION
or incompatible claim/intent/result association for that UUID is a protocol
error, not a new allocation; valid associated record kinds are not duplicates. If
the necessary suffix is no longer retained, first pin a compatible paired
checkpoint/baseline and its entire required suffix and dependency graph. Fetch
into a new private gated destination, verify all codecs/hashes/history/authority,
then publish with Backing-store's acceptance/CURRENT barriers. Never overwrite
an uncertain replica, replay old bytes onto contaminated payloads, or use a
snapshot timestamp as selection evidence.

Snapshot, transfer, replica-baseline, transaction, session, handle and recovery
pins cover all transitive object homes, payload bases and decision/request graphs.
Register protection before promising a transfer/recovery point; concurrent GC
must observe it. Retain the old source/pins until a verified complete durable
replacement and protected release exists. RETENTION advances only with every
assigned replica's durable coverage or pinned compatible baseline/suffix, closure
checkpoint pairing and all external snapshot holds. A partner memory receipt
never authorizes deletion. External tools own snapshot scheduling/holds; this
protocol does not prune them. If any coverage is unknown, preserve and throttle.

Use three [Performance](../Performance.md) connection classes per active peer
pair: small persistent control for handshake/ACK/progress; persistent live data
fairly multiplexed across groups; bounded bulk for historical records/artifacts.
Every class repeats identity/session/authority validation and has finite message,
queue, byte, operation, connection and worker limits. Additional live sockets
require stable volume assignment or a sequence-aware reconciled handoff; socket
ordering alone cannot decide stream ordering. ACK routing on control is allowed
only after actual acceptance on a valid data/bulk binding.

Reserve recovery/control capacity; give bulk catch-up a finite forward-progress
share while prioritizing live latency. Slow B disk causes retained-memory
backpressure before B overcommits credits. Slow C consumes pinned journal capacity
and eventually throttles A admission before recovery reserve is exhausted.
Bound batches and batching delay; do not spawn a goroutine per unbounded wait.
Independent closures may progress subject to shared pool limits. Report wait
reason, credits, R/D/K/P, unresolved slots, dependency gaps, bytes/operations/time
lag and last verified progress, with no workload payloads in logs.

## Failure and takeover boundary

| Event | Required result |
|---|---|
| B absent/not READY, full, or disk stalled | Bounded wait/backpressure; no local-only success or implicit substitution of C. Already chosen work retains its outcome. |
| A local sync fails after B ACCEPT | No successful completion; preserve candidate/acceptance and resolve uncertainty. B's earlier acceptance cannot be silently erased. |
| Data/control disconnect or lost ACCEPT/result reply | Retain accepted work, reauthenticate/reconcile, resend exact frames; no duplicate apply or rollback inference. |
| B restarts with D shorter than prior accepted tail | New incarnation RECOVERING, continuity false/unknown; reconstruct from A/verified history before readiness. Missing record is not absence proof. |
| A dies after COMMIT acceptance, before result/install | Preserve B's tail; recovery resolves same candidate and dependencies. A missing later result or COMPLETE cannot authorize ABORT. |
| Partial payload or namespace apply / cache barrier fails | COMMIT remains; reads/views gate until exact-ID redo and admitted cache barrier complete. No uncommitted/piecemeal namespace exposure. |
| Higher promise races delayed old candidate | Serialize accept versus durable promise; preserve and report any acceptance that preceded promise, reject candidates afterward. Never reply to promise before capturing its prior tail. |
| C catches up while old history/snapshot is retired | Transfer/dependency pins and durable coverage serialize with retention; stop instead of sending a missing base/graph. |
| A's storage lost plus B volatile tail lost | Accepted durability gap: use surviving complete evidence or stay unavailable. No finite RPO, automatic stale promotion, invented zero payload or inferred abort. |

The takeover task must recover the full owner and reporter histories, canonical
claims/bindings/results, highest promises, per-slot accepted term/value and
complete dependencies, continuity statements, and known protected outcomes.
Obtain intersecting storage-majority evidence for every closure member. For each
ordinary commit/transaction decision slot adopt the highest accepted term in the
recovery majority consistent with any protected outcome; retain all prepares.
If candidates conflict within a term or higher evidence is incompatible, quarantine.
Unavailable data blocks progress rather than licenses a different value.

Absence-based abort needs fenced majority evidence with B's continuous accepted
tail or equivalent complete durable history and no constraining accepted candidate.
An offline/restarted B, lagging C or missing result record cannot supply absence.
An owner-only candidate may be adopted conservatively with complete dependencies;
one excluded by verified fenced absence cannot revive on return. Persist promises
before reply, fence old workload/cache rights positively, select and re-protect
history/dependencies in the new ready pair, replay and establish visibility before
serving any member. These are `eq3` obligations, not a concrete fence selection
or permission to activate a new owner from this spec.

Deliberate loss selection needs explicit authorization, LOSS_RECOVERY and a new
recovery generation, preserved old copies, consistent closure cut and unknown
client outcomes recorded. It does not reinterpret excluded uncertain work as
proven ABORT. The accepted owner-storage/partner-memory gap has no finite loss
window even though B is expected to persist promptly.

## Model and validation obligations

`fq3` starts with one volume, fixed A/B/C, a bounded ordered stream, ordinary
MUTATION/COMMIT/ACCEPT/result, promises and abstract authorized fence/recovery
actions. Check normal safety first; the concrete `eq3` fence assumption and
implementation must be reviewed separately. The transaction model is a separate
extension with immutable anchor, participants, decision slots and one visibility
generation; passing a single-group model cannot establish transaction safety.

Required invariants and negative schedules:

1. Successful reply implies valid authority, complete durable A prefix, current B
   acceptance/retention, chosen outcome and required protected terminal records;
   remove any check to expose local-only, receipt-only or stale-session success.
2. A slot has one recoverable outcome; accept-before-local-sync, lost ACCEPT,
   owner crash before result and delayed proposal versus promise preserve every
   constraining acceptance. Highest-term recovery cannot overturn protected COMMIT.
3. R/D/K/P are distinct contiguous same-history prefixes; reorder across sockets,
   deliver sequence n+1 before n, duplicate frames, partial tails and unknown
   dependencies. No receipt/commit/apply can jump a hole or treat hash-only state
   as complete redo. Reporter sequence never aliases owner sequence.
4. No staged/uncommitted or partially applied state is observed. Delay apply after
   reply, interleave read/barrier capture with later writes and fail invalidation.
   Include reads through cached aliases and mmap loads that bypass daemon gates
   while apply is deferred: a profile lacking verified freshness for those paths
   must delay publication/success until complete apply and its required cache
   barrier. Reads cover their admitted cut, successful completion cannot leave
   stale cached observations, and namespace publication stays atomic.
5. Owned buffers remain immutable through acceptance until durable replacement/
   protected handoff. Reuse a sender buffer, close a connection, exhaust credits,
   or restart B with volatile loss: none may silently erase a retained obligation
   or prove absence. Explicitly model the accepted multi-failure loss case as
   unavailable/authorized loss recovery, without claiming loss-free recovery.
6. Same anchored ID cannot acquire a different canonical intent or effect; retry
   after lost result/source removal/checkpoint, change CREATE mode or SETATTR
   value, and retire a floor past a pending claim. Conflicts/retired IDs stop
   before new effects; an uncertain timeout is never rollback.
7. Async/offline obligations and all snapshot/home/session/transfer dependencies
   survive restart and retention; race checkpoint publication/GC with catch-up
   and reject R/P-only coverage. Missing complete replacement preserves pins.
8. Stale session/incarnation/authority cannot create fresh acceptance or serving
   rights; historical transfer preserves bytes without reviving the sender.
   A restarted partner's shorter D must not enable absence-based abort.
9. Under stable authorized pair, complete history, finite admitted load, fair
   delivery/workers, available capacity and functioning apply/cache barriers,
   admitted work and async catch-up progress. Without these assumptions waits
   remain bounded at the client, retain evidence and never weaken success.

The later transaction model must additionally check all-prepare commitment,
highest-accepted coordinator-term recovery, claim-before-path routing, immutable
binding through closure merges, participant install/alias drain and no partial
closure visibility. Transactions supplies its detailed negative schedules.
Use an external oracle of IDs/digests, replies, chosen outcomes and observed
versions; compare recovery to that oracle rather than replica equality alone.

No model, daemon codec, kernel/cache, storage-fault or workload run is established
by this document. Local link/format/vector checks validate documentation only.
Native and guest-path runs require their own explicit approval under
[Test automation](../Test-automation.md).
