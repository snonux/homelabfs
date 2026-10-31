# Cross-volume transactions

Status: all-origin transaction and object-home contract, `hz3`, 2026-10-31.
**Unimplemented.** This specification composes the
[normal protocol](Replication-protocol.md), [committed views](View-barriers.md),
and [all-origin recovery](Takeover.md#all-origin-transition-state-machine).
The fixed-owner v1 implementation/models retain their historical scope; neither
they nor the loopback probe implement this contract. Scope remains disposable
lab/canary volumes. The separate [bounded composition model](../../spec/tla/OriginTransactions-results.md)
records the exact explored inputs and assumptions, rather than extending old
fixed-owner evidence to the new pipeline.

Paul selected per-volume FUSE mounts and cross-volume transaction support before
affected implementation. The interface is an explicit application API. Ordinary
POSIX rename/link across mounts still fails with `EXDEV`, without effects. An
application must deliberately call this API; copy-then-delete is not its
implementation. This design supports regular-file, symlink and directory rename,
and regular-file hard links, across independent volume groups.

## Placement and the unit of availability

Use one preferred sequencer A and one committed logical view for every
participating volume and object home. Every admitted node O may accept and
originate this API for every volume, including O outside replica placement R.
Nonowners route workload observations and ordering/authorization to A; O itself
durably commits its complete new operation graph. A passive backing tree cannot
supply current observations or authorize redo. The initial protocol requires the
complete dependency closure
to have the same admitted three-node replica membership, a common current
sequencer A and common distinct owner-origin memory partner B. Per-group epochs,
configurations, logs and positions remain distinct. Different epoch numbers are
permitted; membership and partner compatibility are checked explicitly, not inferred from numbers.

A dependency closure contains all groups connected by an outstanding transaction,
foreign object references, session/request anchors, or still-live cross-volume
handles. Before admitting
a transaction that joins closures, close their admission gates, drain conflicting
work, and establish a durable affinity manifest on every member: closure UUID,
manifest version, sorted member UUIDs, each group's generation/configuration/epoch,
A/B identities and incarnations, admitted origin set S/registry revision,
selected/source cuts, and the transition's predecessor manifests. Protect these records
with the acknowledgement rule below. Each logical member references its exact
O-durable fragment and A projection; this does not require synchronous writes
to every replica disk. Publish only after the entire vector is protected. A
partial join stays gated and is completed under recovery before new work. No object references are changed by this step.
The manifest and transaction anchors make partial joins discoverable on restart.

Joining requires existing placement to be compatible. Otherwise return
`PLACEMENT_CONFLICT` before transaction admission, listing the conflicting UUIDs;
an explicit, fenced placement/configuration transition is required first. This
API does not relocate workloads, form a new replica configuration, or perform
an etcd operation per transaction. Incompatible partner changes and unilateral
transfers are blocked while the affinity exists. Membership changes must preserve
the entire closure's history using the separate fenced reconfiguration protocol.

Foreign references make availability coupled even after a transaction finishes.
Recover, fence and place that closure together before serving its dependent
views. Do not claim these volumes still fail over independently. Unrelated
closures remain independent. Initially affinity only grows; removing a member
requires a later verified split protocol, rather than guessing that no links
remain. This conservative choice bounds the first implementation's transition
cases at the cost of long-lived placement coupling.

## Application interface

Expose an authenticated origin-capable service on every admitted normal node;
a Unix-domain endpoint is the initial native interface. The receiving origin
forwards the original credentials and exact immutable request to A for anchored
claim lookup, ordering and authorization; transport daemon membership is never
a substitute for those credentials. Retry through another origin attaches to
the existing binding and original durable-origin role; it cannot create a second
local commit authority or change O. A guest-facing adapter must authenticate the original
numeric credentials and bind authorized volume views; guest root or transport
membership alone is not authorization. Until that adapter exists the API is a
native capability, not a claim of 9p application support. Transport framing and
Go types are implementation work; the following fields and behavior are fixed.

`Execute` accepts:

- `version = 1`, a durable anchored session `(anchor volume UUID, session nonce
  UUID)` and monotonically increasing request number. The transaction ID is
  `(authenticated principal, anchor volume UUID, session nonce UUID, request
  number)`. The client persists it before sending; reconnecting uses the same ID.
- Exactly one `Rename` or `Link` operation. Each endpoint has a canonical volume
  UUID, authorized view ID and a relative component path. Reject absolute paths,
  empty/reserved components, `.`/`..`, escapes and inconsistent UUID/view pairs.
  Intermediate symlinks are rejected initially; final symlinks are renamed as
  objects, not followed. `Link` accepts only a regular-file source.
- `Rename` specifies `replace` or `no_replace`. `Link` requires an absent target.
  An optional expected source object ID and expected parent/name revisions give
  compare-and-set behavior. Without them, resolve against current state under
  admission locks; a stale pathname is not a stable identity.
- A wait deadline. It limits waiting for a response; it is not a transaction
  lease or an instruction to roll back after that time.

`RegisterSession(anchor volume UUID, session nonce UUID)` authenticates the caller
through A for that volume and protects a session registration in the
origin's exact anchor fragment under the normal O+M rule below.
It returns the anchored session; repeating it for the same principal and nonce
returns that session. The anchor UUID is immutable, cannot be retired/reused while
its sessions remain live, and is not inferred from the source path or current
lowest-UUID coordinator. Its current sequencer is the sole ordered
authority for claiming request numbers, returning results and advancing
retirement floors. The immutable anchor
is a logical authority, not a requirement that its records live only on A disk. A retired
session registration remains a tombstone; registration cannot recreate it.

Every Execute, GetStatus, Cancel and session-retirement request first resolves
that immutable anchor through its authoritative sequencer. An admitted nonowner
may originate it with its own durable fragment and A's memory projection, but
cannot claim an ID, resolve conflicting paths or choose a result independently.
An unavailable A means waiting/unavailability, never local-only execution. Consult the anchor's
protected request claim and retirement floor **before pathname resolution, closure
selection or new namespace effects**. A serializes claims per request ID across all origins.
For an existing claim, compare the original request digest and return its recorded
result/pointer, or `ID_CONFLICT` on a different digest. Neither successful rename
removing the source nor a later closure merge can turn a retry into a new request.
If anchor history/authority is uncertain, return INDETERMINATE rather than treating
a missing local record as an unused ID. No global per-operation etcd lookup is
introduced; UUID randomness does not replace this single claim authority.

For a new request, canonicalize the original version, anchored identity, operation,
endpoint volume/view IDs, component paths, replacement policy and explicit expected
IDs/revisions, and protect a `REQUEST_CLAIM` containing that digest and the complete
original request at the anchor. Exclude wait deadlines, routing hints and newly
resolved object/parent revisions from the digest. Authenticated principal is part
of the ID; execution credentials/view-policy versions are captured separately.
Store canonical request bytes as well as their digest, and compare exact canonical
fields on retry rather than relying solely on hash equality. Preserve path component
bytes; do not apply case folding or Unicode normalization.
Only after this claim is protected may the service resolve and record stable
parent/object IDs, parent revisions, source/replacement IDs, object-home groups,
credentials and authorization versions. Two simultaneous submissions of the same
ID for A-to-B and C-to-D meet at one anchor, not independent coordinators.

Participants always include the anchor, the endpoint volumes and every group
owning touched directory entries, object metadata or lifetime state. Include the
anchor's existing dependency closure in compatible placement and affinity checks;
reject split sequencers/membership/owner-origin partners without namespace effects. A moved
directory's existing closure is included without enumerating/copying file bytes.
After resolution and closure join, protect an anchor `REQUEST_BIND` to the full
participant vector, manifest version and selected coordinator UUID before BEGIN
can authorize effects. That binding is immutable for this request. A crash before
binding can resume preflight from the original claim or protect a rejection; a
crash after binding resolves the recorded coordinator, never re-resolves paths or
selects a new coordinator from a later merged closure. Recovery terms may change
under the bound coordinator's fencing rules without changing that UUID.

The anchor stores/protects the terminal decision reference and complete result
before a terminal reply. It retains the binding and decision dependencies until
verified checkpoint replacements cover them. A claim is not yet transaction
admission: preflight rejection is protected at the anchor and consumes its ID
without namespace/object effects. Retrying its digest returns the same rejection;
correcting the request needs a new number. Claims and bindings remain discoverable
even if the original source is gone or the active closure coordinator has changed.

`Execute`, `GetStatus(transaction_id)` and `Cancel(transaction_id)` return one
of the following outcomes, always with the transaction ID and request digest
when a request claim or transaction admission occurred:

| Outcome | Meaning and client action |
|---|---|
| `REJECTED(code)` | Proven not admitted, with no namespace/object effects. A protected preflight claim still reserves this ID/digest. Fix the request and use a new number. Configuration preparation may have added conservative affinity. |
| `PENDING(stage)` | Claim or transaction admitted; a terminal outcome has not yet been established. Query/retry the same ID. |
| `COMMITTED(result)` | All effects are visible and meet the protection boundary. Result includes stable object IDs, replaced ID if any, decision identity and per-group committed positions. |
| `ABORTED(code)` | A protected abort decision proves this request has no namespace/object effects; locks/staging have been released. |
| `INDETERMINATE(reason)` | Missing evidence or authority prevents resolution. This is neither abort nor permission to issue a new request. Preserve the ID and query after recovery. |
| `RETIRED_ID` | Session/request is below a durable deduplication floor. It cannot execute again; recover its result from the client's retained receipt. |

Before admission, error codes include invalid input, unsupported version/type,
missing object, destination exists, nonempty directory, permission/read-only
denial, stale expected revision, cycle/root move, placement conflict, unavailable
authority, resource limit and I/O failure. A failure after admission is `PENDING`
or `INDETERMINATE` until a protected terminal decision exists; an errno alone
must not imply rollback. `GetStatus` is authenticated to the originating principal
or an administrator and does not execute an unknown ID. `Cancel` is idempotent:
it requests abort before a commit proposal; it returns the actual committed
outcome if commitment has won. Losing a connection, killing a client, or a wait
deadline does not cancel admitted work. Unknown IDs cannot be safely classified
as rejected while their session's recovery evidence is unavailable.

The initial API is one atomic namespace operation, not a transaction containing
arbitrary application I/O, database commits or several independently requested
renames. Retrying one admitted identity does not repeat its effects. Arbitrary
new identities after an uncertain reply have no exactly-once guarantee.

## Object identity, directories and hard-link lifetime

Introduce a logical object layer. An object ID is `(home volume UUID, allocation
UUID)` and is never recycled. Recovery generation identifies the authoritative
history containing that object, not a newly allocated replacement. Directory
entries reference object IDs; the home group owns object data, Unix metadata,
regular-file link count and the durable reference/lifetime ledger. A directory's
entries are journaled in its home group even after that directory moves beneath
another volume root. The logical namespace volume and the object's home may
therefore differ. Paths and host inode numbers are not replay identities.

Store payloads in private home-owned backing objects, with a logical namespace
index. Do not attempt a raw hard link or rename between ZFS datasets. Object-data passthrough is eligible only when it satisfies the selected-cut,
ordering and frontend-coherence obligations; it cannot bypass A's committed
logical overlay. Direct passthrough namespace mutation cannot implement this
contract. Every assigned home replica has a persistent payload/log catch-up
obligation. Completion may precede its durable coverage; exact sources remain
pinned until that obligation has a verified durable replacement. A foreign namespace entry carries a home reference, not a second
independently writable payload. Replication of the destination entry alone does
not make that destination recoverable without its home group.
Charge payload, object metadata and their replication/journal capacity to the
home; charge a foreign directory entry and its log to its owning directory's
group. Do not claim a move transfers storage usage or releases the source quota.
Admission budgets the complete closure, including reference and snapshot pins.

For `Link`, atomically add the target entry, increment the home's regular-file
link count, and add the foreign-reference dependency. Both names refer to the
same data, ownership/mode, times, locks and lifetime. Writes/truncate/chmod/chown
through either name target that same home object through the ordinary
all-origin ordering path;
the admitted writer durably holds its new fragment and A projects it into the
single home history, even when O is outside the home placement. They must
invalidate every published alias's relevant caches. A private mutable copy or
symlink is not a hard link. Directory and symlink hard links are rejected.

For `Rename`, atomically remove the source entry, install the target entry and
adjust both parent metadata and any replacement's link/lifetime state. Preserve
the source object's identity, contents and metadata apart from specified rename
times. A regular-file rename preserves its link count unless the target already
names that same object; that case is a successful no-op. `no_replace` fails if
the target exists. File/symlink and directory type mismatches are rejected;
directory replacement requires an empty target directory. A symlink's stored
text is preserved, so moving a relative symlink can change what it resolves to.

Directory rename moves the directory reference and changes its logical parent
relation; descendants retain their IDs, home groups and entries. No recursive
payload copying or rewriting is involved. Serialize ancestry checks with every
directory-parent change across the closure: reject moves into the source's own
descendants, cycles, volume-root moves and replacement of a volume root. Update
the new/old parent's directory-link accounting and the moved directory's `..`
semantics in the same decision. Workload namespace roots remain fixed. A moved
directory is reachable under its new namespace volume's views; traversal cannot
escape a view boundary through `..`. The directory's old home remains required
even if its original volume root is now empty.

Ordinary operations inside a mount can also touch foreign homes after such a
move/link. Unlink, replacement, local rename/link and lifetime changes then use
this same internal multi-group protocol; they cannot independently update a
foreign entry and its home ledger. Ordinary data writes use the home's selected history and
normal O+M commit path, with exact origin source references; they need not be
durable in A's home backing journal before completion. Per-volume barriers
include their recorded foreign-home dependencies. An internal transaction does not make an ordinary cross-mount
POSIX call succeed.

Fresh lookups require search permission along the current authorized view and
Unix checks on freshly committed metadata. Require source/target parent write
and search permissions for rename, target-parent permissions for link, sticky
directory rules, and RW access to every namespace being changed. For hard links,
require either source ownership by the caller, read-and-write Unix permission on
the regular file, or an explicitly authorized privilege; possession of the API
socket is no such privilege. A read-only source view can supply an authorized regular
file for `Link` if the link policy permits it; the destination must be RW. A RO
view cannot authorize a rename removing its source entry. API authorization never
bypasses Unix metadata rules or subtree boundaries.

Lock permissions, namespace revisions and policy versions through prepare and
decision so a concurrent permission change cannot invalidate the checked state.
Already-open descriptors remain bound to their original object; replacement
does not retarget them, and permission changes do not retroactively revoke them.
Unlinked/replaced objects remain until the durable link ledger, current-generation
handle/mapping pins, replay/catch-up and snapshot obligations all permit reclamation.
Home volume retirement/reclaim is prohibited while any foreign reference, moved
subtree, transaction or handle still depends on it. Retiring a namespace does not
delete the homes it references or silently remove references from other volumes.

Distinct FUSE mounts may report distinct device/inode pairs for one logical object;
the API exposes its stable ID. Kernel per-mount locks/caches are insufficient to
share hard-link state. The service must unify its supported locking semantics and
invalidate aliases, including mmap data, using the actual adapter contract. Dirty
mapped/buffered writes need the verified platform synchronization rules; namespace
commit is not an implicit application-data fsync. If a profile cannot make new
aliases coherent with existing handles/mappings, block that profile's publication
or transaction admission with a precise capability error. This is an implementation
gate, not permission to advertise copies as shared objects.

## Ordering, records and protection

Serialize initially with one outstanding mutating grant per closure, including
ordinary operations from every origin. A owns the reservation, not an independent
coordinator at each origin. Hold the closure gate through the entire transaction,
including result protection/publication, and acquire member gates in UUID order.
Before freezing validated redo, positively drain conflicting old frontend dirty
writes through the same sequencer queue and stop new dirtying. Do not hold a
lock needed by those drain requests. Independent read observations may use the
old complete visible cut until publication closes their domains.
This avoids distributed lock deadlocks; it deliberately sacrifices concurrency.
Bound request size, closure size, retained payloads and wait queues using admission
limits. Capacity failure before admission rejects; after prepare, preserve evidence
and finish/recover rather than dropping records to free space.

The coordinator selected at REQUEST_BIND is the lowest UUID group in that request's
closure, ordered by common A; it is a logical group, not an origin-local
sequencer. Its UUID remains bound to that request even
if later joins give the closure a different lowest UUID. The immutable session
anchor remains the routing/deduplication authority independently of this choice.
Coordinator authority is the complete manifest version plus per-group recovery
generation/configuration/epoch vector. Every message also carries transaction ID,
digest, coordinator term and sender incarnation. Coordinator terms increase only
through a fenced closure recovery; they are not time-based client leases. Reject
stale/mismatched terms, epochs, configurations and incarnations, including delayed
messages after a restart. Persist fencing promises before acknowledging them.

Call a semantic record **protected** only through the normal origin-durable
and distinct-memory exchange. For O != A, M=A; for O=A, M=B. O durably holds
the complete new multi-volume graph in its own contiguous origin streams:
claims/bindings, private redo and payload, prepares, coordinator decision,
installs, visibility/completion and stable anchor result. Each participant
reference includes full origin stream identity, local position/hash and exact
selected per-volume cut; a group name or scalar maximum is insufficient.
Older selected dependencies may remain at explicitly pinned verified durable
sources, rather than being recopied into O's new fragment.

The enclosing normal grant/acceptance phases are distinct from the semantic
2PC records: PREPARED_ACCEPT retains a candidate; it is neither an origin durable
choice nor a transaction PREPARED/DECISION. O syncs the exact phase fragment and
irrevocable choice; M retains the complete matching fragment/acceptance, and A
selects that evidence under the one closure reservation. Every new semantic
phase must meet this rule; a previously protected BEGIN does not protect a later
DECISION. Do not recursively wrap semantic COMMIT/COMPLETE in more transactions.
Before calling any semantic phase protected, finish its enclosing normal
SELECTED/RESULT_PROTECTED exchange: O durably syncs the exact selected certificate
and fixed phase result, M retains that result, and A verifies RESULT_ACCEPT.
Candidate/COMMIT acceptance alone is insufficient. COMPLETE already contains
the fixed complete API outcome, decision/install vector and visibility/recovery
graph with that protection before transaction publication. The final anchor
REQUEST_RESULT below durably indexes that already protected outcome; it does not
establish the first durable result after visibility. Missing that index gates
terminal replies and the next mutating grant while recovery repairs it from
COMPLETE, never changes COMMIT to ABORT. This preserves the normal protocol's
result-before-publication condition without recursive semantic transactions.
A retains the complete committed logical projection and dependency graph before
planning any successor, even when its disk and backing apply lag. B is required
for owner-origin completion, not an extra synchronous dependency of remote O+A.

Receipt emission certifies owned retention in the named incarnation at that
instant. A delayed receipt may arrive after unobserved RAM loss; known loss gates
further completion, but perfect crash detection is not assumed. Loss of the
unique durable origin source plus required volatile copy before durable
replacement is the accepted loss window, including late receipts. Async delivery
to assigned R remains persistent/retryable and outside the normal reply path.
Memory, disconnect, reply or backing apply never authorizes durable source release.

Required logical records (encoding remains the journal spec's responsibility):

| Record | Required contents |
|---|---|
| `SESSION`, `REQUEST_CLAIM`, `REQUEST_BIND`, `REQUEST_RESULT` at immutable anchor | Principal/anchored identity, registration/tombstone and floor, original request/digest, immutable coordinator/participant binding, terminal decision reference and complete result. |
| `BEGIN` in coordinator, participant anchors in all groups | Identity/digest, authenticated request/result schema, closure/authority vector, participant list and dependency positions. |
| `PREPARED` in each participant | Stable affected objects/entries, expected old versions, exact redo and lifetime deltas, authorization versions, dependency vector, retained staging/payload references and coordinator identity. |
| `DECISION(COMMIT or ABORT)` in coordinator | Immutable outcome for this term, request digest, complete participant prepare vector for COMMIT, result/error and decision-slot identity. |
| `INSTALL` in each participant | Decision value/reference plus complete recoverable decision evidence and applied redo position; no independently chosen participant outcome. |
| `VIEW_BARRIER`, `COMPLETE` in coordinator | Exact closure/member selected cuts, installed vector, positive reader/cache-domain barrier evidence, result and one visibility generation. |
| Checkpoint/session/affinity records | Replacement decision/dedup evidence, object references and data dependencies, GC pins and durable session retirement floors. |

## State transitions and visibility

The chosen construction is blocking two-phase commit with a replicated decision
slot, not a claim that independent ZFS renames form a transaction.

0. **CLAIMED:** route to the immutable session anchor, consult floor/dedup state,
   and protect the original request claim before resolving paths. Resume an existing
   claim/binding or return its result/conflict; preflight failures protect rejection.
1. **ADMITTED:** resolve endpoints and anchor-inclusive closure; establish authority
   and gates; protect REQUEST_BIND, BEGIN and participant anchors before new effects.
   No tentative namespace or metadata is visible.
2. **PREPARING:** create private redo/staging and protect PREPARED in every
   participant. Required prior writes, object payload and namespace dependencies
   must be protected too. A source with only undelivered guest dirty pages has
   only the guest profile's existing guarantee, never a fabricated stronger one.
   Failure can propose ABORT; participants cannot abort themselves on a timeout.
3. **DECIDING:** only after every bound prepare is protected, propose COMMIT in the
   coordinator slot and protect it. Otherwise propose/protect ABORT. Never propose
   both values in one coordinator term. Cancellation before a COMMIT proposal may
   select ABORT; after that point cancellation waits for recovery/outcome.
4. **DECIDED:** a protected decision is authoritative. COMMIT cannot become ABORT
   because installation, invalidation or reply delivery fails. Install/protect
   the decision evidence in every participant. For COMMIT, apply all namespace,
   parent, object and lifetime changes idempotently behind the gates. For ABORT,
   discard private staging only when recovery no longer needs it.
5. **PUBLISHING:** after all committed participant changes are installed in A's
   complete logical RAM projection (backing apply may lag),
   invalidate affected entry/negative/attribute/data aliases as required, drain
   old in-flight namespace observations and finish the cache/view barrier. Protect
   VIEW_BARRIER and COMPLETE, and atomically advance A's closure visibility generation,
   then reopen observation gates. Keep the mutating reservation until the
   anchor result is protected. That advance is the successful operation's visibility
   point. On ABORT reopen observations without namespace changes after protected
   cleanup, keeping the same reservation until its anchor result is protected.
6. **COMPLETE:** protect REQUEST_RESULT at the immutable session anchor, then reply
   with the durable result and positions. The anchor is a participant: O syncs
   its exact result and M retains it;
   A attests the published cut/result before O may reply. This is the closure's
   O+M protection rule, not a requirement that A sync every remote result. Loss
   of the reply is resolved there
   using the same identity; it does not repeat the operation. ABORTED/rejected
   terminal replies likewise require an anchor result record.

A publishes one logical state across all admitted mounts and API origins. No observer
may see only one participant installed in the same visibility generation. Reads,
lookups, access checks, readdir and ordinary mutations wait behind the affected
gates and their freshness dependencies. Two separate POSIX observations can
straddle the visibility point and legitimately see different generations; the
API does not promise a cross-volume snapshot to unsynchronized readers. Reads
on existing descriptors retain their object semantics through namespace changes.

Kernel/guest cache hits bypass daemon gates. Before enabling a profile, prove
that its invalidation/drain mechanism prevents new namespace/metadata observations
from using stale entries after publication, and that alias data coherence holds.
Where necessary, quiesce/revoke all affected views and remount/restart clients
before reopening the closure; an API response may wait for that barrier. If this
cannot be established, leave the operation committed but gated and report
`PENDING`/`INDETERMINATE`, never successful partial visibility. Unavailable
capabilities known before admission are rejected without namespace effects.

## Decision recovery and fencing

The decision slot requires explicit recovery rules; neither a local candidate
record nor the largest sequence number alone establishes a chosen decision.
Participants cannot choose independent outcomes during recovery. Use the
[all-origin transition](Takeover.md#all-origin-transition-state-machine) for the
entire closure, including partial affinity joins, original anchor bindings,
foreign homes and live handles:

1. Gate/discover every dependency and serialize the transition against exact
   predecessors. Positively close the old sequencer, **all admitted origins S**,
   every used memory ledger, and reader/writer/cache domains. Persist higher
   promises and preserve still-owned tails before voluntary stop. A fence closes
   rights; it does not produce missing data or a sealed journal report.
2. Collect durable sealed close reports after each source's last possible append,
   sync or choice. Include every S member, even outside R or with no known write,
   and used A/B ledgers. A report can be replaced only by positive exclusion plus
   verified complete durable choice/result/dependency coverage and independent
   proof of the **complete issued-grant frontier**. Two placement disks, a fresh
   epoch, missing reply or selected maximum are insufficient.
3. Recover exact immutable claim/binding, bound coordinator, transaction term and
   semantic decision graph. Preserve any selected COMMIT and conservatively
   adopt a complete durable COMMIT whose selection/result is unknown. All bound
   prepares and exact original redo/result remain mandatory. Conflicting choices
   or bytes quarantine; missing evidence gates rather than permits ABORT.
   A transaction timeout/Cancel is never an ordinary durable DECLINE. Any
   absence-based ABORT needs complete closed-origin/decision evidence excluding
   every old possible COMMIT, not a restarted empty partner or fixed-R majority.
4. Durably materialize the full selected graph, results, homes, source manifests
   and frontier at prospective A and distinct B for **crash recovery**; verify
   both actual-incarnation receipts. Rebuild the entire committed logical view,
   reconcile INSTALL/VIEW_BARRIER/COMPLETE, positively exclude old domains,
   install successor authority and fresh frontend generations, then reopen all
   members together. Preserve offline replica, snapshot and source obligations.

Cooperative preferred-owner handoff uses complete closed-source/frontier evidence,
positive cooperative domain closure and transfer of the full committed RAM
projection, pending/terminal results, claims, homes/handles and pins. Already
verified origin durability may remain at its sources; the new A may serve before
its local persistence/apply. Failed continuity falls back to the stronger crash
A/B durability gate. A closure is the transfer unit; moving one member out of its
affinity or changing one partner unilaterally is forbidden. Demand policy remains
separate work. No guest trust/fence policy is selected by this transaction API.

The old fixed-owner Transactions/TakeoverTransactions models and local ledger
remain evidence only for their original owner-stream assumptions. The new
[composition results](../../spec/tla/OriginTransactions-results.md) explicitly
bound the two-volume origin-conflict and phase cases they explore; they are not
an implementation/refinement proof or arbitrary distributed recovery proof.

A prepared participant without complete coordinator evidence waits and requests
recovery. It cannot infer abort from an etcd lease expiry, timeout, coordinator
death or absence of COMPLETE. If the coordinator/home group is unavailable, the
dependent closure remains unavailable. A recoverable single-host failure should
resolve automatically with valid fencing and preserved tails; missing/ambiguous
evidence instead requires waiting for a source or explicit loss recovery. Split
sequencer authority is a placement error. Authorized nonowner observations route
to the one A view; they never read a stale passive tree.

## Durability, retention and snapshots

Successful completion requires every participant's complete new redo/dependency
graph, authoritative decision and exact anchor result durable at O and retained
at distinct M, plus A's complete committed logical view and the shared positive
visibility barrier. Older durable source coverage remains pinned. This is **one
durable origin copy plus distinct retained memory**, with no finite RPO or second
durable normal-copy promise. Error/timeout can precede a committed result.

Loss of O's unique storage plus A RAM for remote work (or B RAM for owner work)
can destroy a decision, dependencies or result. Recover available evidence and
stop the full closure if incomplete. Never expose a destination alone, invent
ABORT, or synthesize an empty home. Explicit loss recovery selects a mutually
consistent whole-closure history in a new generation with separate authorization
and permanent exclusion of old uncertain IDs. A delayed pre-crash receipt does
not strengthen this guarantee.

Journal/checkpoint manifests must retain transaction identity/digest, terminal
outcome, prepare/decision/install dependencies, foreign-home reference ledger,
affinity and incarnation/fence evidence. Checkpointing one participant's applied
state is insufficient to delete its transaction evidence. Compact decisions only
after a verified closure replacement preserves the same decision, exact
origin/source graph, claims/results and complete issued frontier, and every
assigned replica has durable coverage or a pinned compatible baseline/suffix.
Protect exact source releases before removing any origin fragment; neither A
backing apply nor its RAM acceptance supplies a durable replacement. Resolve
pending transactions or retain their complete prepare
and decision-resolution dependencies. Serialize GC with closure transitions.

Deduplication has no wall-clock expiry. Clients explicitly retire a session prefix
only after persisting its terminal receipts. At the immutable session anchor,
protect a monotonic retirement floor covering only terminal request numbers, and
retain it and the session tombstone in checkpoints. Every routed retry consults
that floor first and returns RETIRED_ID without resolving paths or executing again.
An unresolved claim/binding blocks retirement past its number; unclaimed lower
numbers covered by a floor are permanently unusable too. Unretired outcomes remain
queryable; if retention cannot be afforded, reject new admission or apply
backpressure rather than forgetting IDs silently. Retiring
IDs does not release object, catch-up or snapshot pins.

`CloseSession` is an authenticated, idempotent anchor operation after all claims
are terminal and their receipts retired. Protect a closed-session tombstone that
rejects every future request number and registration retry. Keep that tombstone
or a verified permanent rejection mapping when retiring the anchor; requests to
a retired anchor UUID can never be rerouted to another volume or treated as new.
This closes the session's lifetime pin without discarding deduplication authority.

External tooling still owns ZFS snapshot scheduling/retention. A useful transaction
recovery set includes every closure member's paired data/journal datasets, the
decision/affinity manifests, every contributing durable origin source or verified
replacement (including origins outside R), and control/fence reconciliation evidence.
Use a closure checkpoint barrier and manifest with a consistent committed cut and
dependency vector; do not combine independent per-volume snapshots by timestamp.
Live snapshots are crash images and require the same decision resolution as crash
recovery. Restoring just one member is allowed only while gated and with verified
replay/dependencies restoring a consistent closure cut before serving. A standalone
destination snapshot containing foreign references is not a self-contained backup.
Snapshot holds and restore/catch-up pins prevent pruning evidence or home payloads.
Restores use new writable destinations; never modify a received lab backup sink.

## Failure cases and verification obligations

| Fault/interleaving | Required outcome |
|---|---|
| Same anchored ID arrives through two origins with different endpoint intents | Both route to the immutable anchor; one protected digest claim wins, the other receives ID_CONFLICT; incompatible placement cannot cause independent admission. |
| Retry after source removal, closure merge, coordinator change or lost result reply | Consult anchor claim/binding/result before paths; recover the original bound coordinator/outcome and never allocate a second operation. |
| Anchor restarts, claim/binding reply is lost, or session floor is checkpointed | Reconcile protected anchor history; uncertain absence blocks new admission; a retired ID cannot recreate a session/request. |
| Split owners or incompatible partner/membership before admission | Placement rejection; no namespace/object effects. |
| Crash during affinity join or BEGIN fan-out | Discover partial manifests/anchors; keep all discovered members gated; reconcile the join before admission. |
| Only one participant prepared, coordinator/client times out | No visible effects; protected ABORT after safe decision resolution, or wait with locks/evidence retained. |
| Origin COMMIT candidate syncs but M acceptance/result/reply is lost | Resolve the decision slot under fencing; never assume either success or rollback from the timeout. |
| M accepts COMMIT then A fails before INSTALL | Preserve its tail, recover complete prepares, adopt COMMIT and install all members before serving. |
| Participant installed, crash before another applies | Replay the same decision behind gates; no workload sees a half-installed namespace. |
| All apply succeeds, invalidation fails or COMPLETE reply is lost | COMMIT stays irreversible; gate until visibility is safe; same-ID retry returns its actual outcome. |
| Delayed old COMMIT/ABORT after higher promises | Reject stale authority; a returning owner follows recovered history and cannot reopen old mounts. |
| Required M RAM is lost and O unique disk is missing | Indeterminate accepted-loss window; no absence-based abort or stale async promotion. |
| Directory moves race to create a cycle; replacement races with create/chmod | Closure serialization and revision/permission checks choose one valid result; no cycle, wrong-object unlink or stale authorization. |
| Link then write via other mount; unlink/replace with open/mapped handles | One home object, coherent supported aliases, correct link count and retained handles; no payload duplication or premature reclaim. |
| Source root becomes empty after moving its only subtree | Original home and closure remain required; retirement is blocked while references exist. |
| GC/checkpoint/snapshot/prune races with prepare or owner transfer | Required evidence and payloads remain pinned, or a verified complete replacement exists. |
| Disk/quota exhaustion after decision | Preserve decision, backpressure and recover; no abort of COMMIT to regain space. |

The bounded model/simulator must check these invariants under message loss,
duplication, reorder, process restart, volatile-tail loss, partial persistence,
partial apply and fenced ownership/configuration changes:

1. One protected digest claim and outcome per anchored identity across disjoint
   closures, owners and coordinator terms; retries consult the immutable anchor
   before paths, never allocate a second effect, and retired IDs never execute again.
2. Semantic COMMIT implies complete bound protected prepares/dependencies and
   origin durable choice plus distinct M acceptance; successful reply
   implies protected decision, all installs and one published visibility generation.
3. No serving generation exposes a partial transaction or a missing home; abort
   leaves the prior namespace/object state intact.
4. No stale epoch/incarnation can admit, publish or acknowledge work after its
   fence, including cached old-owner access under the platform's fence assumption.
5. Every live reference/handle has one authoritative object home; data, link
   counts, directory ancestry, permissions and replay target the correct IDs.
6. GC/retirement/snapshot operations preserve every unresolved decision and
   advertised recovery point, or stop safely before deleting dependencies.
7. Progress occurs when authority, complete history, a ready pair, capacity and
   functioning cache barriers remain available; otherwise waiting is explicit
   and bounded client waits return uncertainty without weakening safety.

Use an external oracle of request IDs/digests, replies, namespace generations,
object hashes, home/reference counts and fault schedule. Compare recovered state
to the transaction outcomes, not just equality between replicas. Include model
counterexamples for deliberately removed prepare, fence, retention and visibility
checks. Local models/unit checks are separate from future approved native kernel
and guest-path runs; no infrastructure test was run to establish this design.

The successor normal3 foundation currently rejects transaction documents and
foreign-home redo. Keep that fail-closed boundary until downstream `8w3` adds
versioned typed claim/bind/session/floor, affinity/home/lifetime, prepare/decision/
install/barrier/complete/result schemas with exact origin references; validates
the entire acyclic graph and canonical original request/result; and integrates
authentication, one A reservation, O sync/M retention, committed views and closure
recovery. Do not feed these records through v1's common-owner ledger unchanged.
No runtime serving, working distributed filesystem, guest API or 9p trust decision
is supplied here.

Before affected implementation, follow-on work must cover: the common
decision-slot/normal-commit model and simulator; backing object/namespace and
journal/checkpoint schemas; closure recovery/reconfiguration and retirement;
API authentication/idempotency and shared-service integration; native FUSE cache,
locking and mmap alias conformance; and separately the guest API bridge/9p cache
fence. Missing runtime evidence blocks capability publication, not specification
of the required semantics. See [Volumes](Volumes.md), [Durability](Durability.md),
[replication](../Replication.md), [journal storage](../Journal-storage.md),
[recovery](../Recovery.md) and [platform interfaces](../Platform-interfaces.md).
