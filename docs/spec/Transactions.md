# Cross-volume transactions

Status: design contract for `yv3`, selected on 2026-10-28. This is an
unimplemented protocol to model and validate, not a safety proof or a capability
of the loopback PoC. It applies to disposable lab/canary volumes only.

Paul selected per-volume FUSE mounts and cross-volume transaction support before
affected implementation. The interface is an explicit application API. Ordinary
POSIX rename/link across mounts still fails with `EXDEV`, without effects. An
application must deliberately call this API; copy-then-delete is not its
implementation. This design supports regular-file, symlink and directory rename,
and regular-file hard links, across independent volume groups.

## Placement and the unit of availability

Use one serving daemon on one owner for every participating volume and object
home. No participant supplies current workload reads across owners or from a
passive replica. The initial protocol requires the complete dependency closure
to have the same admitted three-node replica membership, a common current owner
and common synchronous partner. Per-group epochs, configurations, logs and
positions remain distinct. Different epoch numbers are permitted; membership
and partner compatibility are checked explicitly, not inferred from numbers.

A dependency closure contains all groups connected by an outstanding transaction,
foreign object references, session/request anchors, or still-live cross-volume
handles. Before admitting
a transaction that joins closures, close their admission gates, drain conflicting
work, and establish a durable affinity manifest on every member: closure UUID,
manifest version, sorted member UUIDs, each group's generation/configuration/epoch,
owner/partner, and the transition's predecessor manifests. Protect these records
with the acknowledgement rule below. Publish the joined manifest only after all
members contain it. A partial join stays gated and is completed under recovery
before new work. No object references are changed by this step.
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

Expose an authenticated owner-local service; a Unix-domain endpoint is the
initial native interface. A guest-facing adapter must authenticate the original
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
at that volume's current owner and protects a session registration in its journal.
It returns the anchored session; repeating it for the same principal and nonce
returns that session. The anchor UUID is immutable, cannot be retired/reused while
its sessions remain live, and is not inferred from the source path or current
lowest-UUID coordinator. Its current owner is the sole authority for claiming
request numbers, returning results and advancing retirement floors. A retired
session registration remains a tombstone; registration cannot recreate it.

Every Execute, GetStatus, Cancel and session-retirement request first routes by
that immutable anchor to its authoritative owner. A nonowner returns a routing
hint or unavailability and cannot admit the request locally. Consult the anchor's
protected request claim and retirement floor **before pathname resolution, closure
selection or new namespace effects**. The owner serializes claims per request ID.
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
reject split ownership/membership/partners without namespace effects. A moved
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
index. Do not attempt a raw hard link or rename between ZFS datasets. Passthrough
remains possible for object data I/O; direct passthrough namespace mutation cannot
implement this contract. All replicas of the home group retain the payload and
its recovery log. A foreign namespace entry carries a home reference, not a second
independently writable payload. Replication of the destination entry alone does
not make that destination recoverable without its home group.
Charge payload, object metadata and their replication/journal capacity to the
home; charge a foreign directory entry and its log to its owning directory's
group. Do not claim a move transfers storage usage or releases the source quota.
Admission budgets the complete closure, including reference and snapshot pins.

For `Link`, atomically add the target entry, increment the home's regular-file
link count, and add the foreign-reference dependency. Both names refer to the
same data, ownership/mode, times, locks and lifetime. Writes/truncate/chmod/chown
through either name route to that same local home object and journal; they must
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
foreign entry and its home ledger. Ordinary data writes use the home journal's
normal commit path. Per-volume barriers include their recorded foreign-home
dependencies. An internal transaction does not make an ordinary cross-mount
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

Serialize transactions initially, one active transaction per closure. Hold a
closure-wide mutation/visibility gate, drain admitted conflicting operations,
and acquire member gates in UUID order. Ordinary operations also honor that gate.
This avoids distributed lock deadlocks; it deliberately sacrifices concurrency.
Bound request size, closure size, retained payloads and wait queues using admission
limits. Capacity failure before admission rejects; after prepare, preserve evidence
and finish/recover rather than dropping records to free space.

The coordinator selected at REQUEST_BIND is the lowest UUID group in that request's
closure, served by the common owner. Its UUID remains bound to that request even
if later joins give the closure a different lowest UUID. The immutable session
anchor remains the routing/deduplication authority independently of this choice.
Coordinator authority is the complete manifest version plus per-group recovery
generation/configuration/epoch vector. Every message also carries transaction ID,
digest, coordinator term and sender incarnation. Coordinator terms increase only
through a fenced closure recovery; they are not time-based client leases. Reject
stale/mismatched terms, epochs, configurations and incarnations, including delayed
messages after a restart. Persist fencing promises before acknowledging them.

Call a record **protected** after the owner has durably synced it and its required
ordered prefix, and the common synchronous partner retains that complete prefix
in owned memory under the current session. Local sync and transfer can run in
parallel. Partner receipt includes referenced payload/metadata and decision
dependencies, or verified durable checkpoints replacing them; a bare transaction
ID/hash is insufficient. The partner cannot release memory until durable coverage
or an acknowledged protected handoff replaces it. Every group keeps distinct
received, durable, accepted/decided and applied positions. Async delivery is
mandatory, retryable and outside the response path.

Required logical records (encoding remains the journal spec's responsibility):

| Record | Required contents |
|---|---|
| `SESSION`, `REQUEST_CLAIM`, `REQUEST_BIND`, `REQUEST_RESULT` at immutable anchor | Principal/anchored identity, registration/tombstone and floor, original request/digest, immutable coordinator/participant binding, terminal decision reference and complete result. |
| `BEGIN` in coordinator, participant anchors in all groups | Identity/digest, authenticated request/result schema, closure/authority vector, participant list and dependency positions. |
| `PREPARED` in each participant | Stable affected objects/entries, expected old versions, exact redo and lifetime deltas, authorization versions, dependency vector, retained staging/payload references and coordinator identity. |
| `DECISION(COMMIT or ABORT)` in coordinator | Immutable outcome for this term, request digest, complete participant prepare vector for COMMIT, result/error and decision-slot identity. |
| `INSTALL` in each participant | Decision value/reference plus complete recoverable decision evidence and applied redo position; no independently chosen participant outcome. |
| `COMPLETE` in coordinator | Installed participant vector, result and visibility generation. |
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
3. **DECIDING:** only after every prepare is protected, propose COMMIT in the
   coordinator slot and protect it. Otherwise propose/protect ABORT. Never propose
   both values in one coordinator term. Cancellation before a COMMIT proposal may
   select ABORT; after that point cancellation waits for recovery/outcome.
4. **DECIDED:** a protected decision is authoritative. COMMIT cannot become ABORT
   because installation, invalidation or reply delivery fails. Install/protect
   the decision evidence in every participant. For COMMIT, apply all namespace,
   parent, object and lifetime changes idempotently behind the gates. For ABORT,
   discard private staging only when recovery no longer needs it.
5. **PUBLISHING:** after all committed participant changes are applied locally,
   invalidate affected entry/negative/attribute/data aliases as required, drain
   old in-flight namespace observations and finish the cache/view barrier. Protect
   COMPLETE and atomically advance the daemon's closure visibility generation,
   then release the gates. That advance is the successful operation's visibility
   point. On ABORT release without namespace changes after protected cleanup.
6. **COMPLETE:** protect REQUEST_RESULT at the immutable session anchor, then reply
   with the durable result and positions. The anchor is a participant, so this uses
   the closure's normal protected-record rule. Loss of the reply is resolved there
   using the same identity; it does not repeat the operation. ABORTED/rejected
   terminal replies likewise require an anchor result record.

The shared service publishes one logical state across all mounts. No observer
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
Participants are passive during recovery. Takeover performs these steps before
opening any closure member:

1. Discover session claims/bindings, affinity/transaction anchors and transitive
   home dependencies. Consult the bound coordinator even if a later closure merge
   changes the current lowest UUID; reconstruct missing terminal anchor results
   only from the bound decision and complete visibility/recovery evidence.
   Gate all of them. Obtain configuration authority and positively fence the old
   owner and every old cached guest/client path using the takeover contract.
2. Persist higher promises for every member on the required storage majority,
   collecting and preserving accepted records and volatile tails *before*
   acknowledging the fence. A node reports its incarnation and whether its
   prior receipt tail is still complete. Restart with only a durable prefix does
   not certify absence of previously accepted memory records. Coordinate the
   whole vector; partial fencing cannot authorize partial serving.
3. Recover the coordinator slot through an intersecting majority with complete
   acceptance evidence. Select the value of the highest accepted coordinator term
   reported by that recovery majority, preserving any known protected decision.
   Preserve its prepare dependencies and install that same value; unavailable
   dependencies block recovery rather than permit choosing another value.
   Each old term had one designated writer and one outcome candidate. Conflicting
   candidates within that term, or incompatible higher-term evidence, are
   corruption/protocol errors: quarantine and stop, never choose by timestamp.
4. An absence-based ABORT is allowed only when the fenced recovery majority can
   prove that no old protected decision is hidden: it includes the common partner's
   continuous accepted tail, or equivalent complete durable history, and no
   recoverable accepted candidate constrains the slot. A merely missing record
   on a restarted partner or lagging async replica is not this proof. A candidate
   seen only on the old owner may be adopted conservatively if its prepares are
   complete; an unprotected candidate excluded by a valid absence/fence proof
   cannot revive when that old owner returns.
5. Re-protect the recovered decision and all participant dependencies in the new
   owner/partner configuration, reconcile INSTALL/COMPLETE, replay behind gates,
   and reconstruct the shared visibility generation. Establish the entire closure's
   normal synchronous pair and cache fences before reopening any dependent view.
   Preserve the offline member's async catch-up obligation.

This is the decision-slot obligation that the normal commit/fencing model must
implement and verify; naming a future commit protocol is not evidence it exists.
The [normal replication protocol](Replication-protocol.md#streams-progress-and-decision-evidence)
defines the shared protection/acceptance transport and acyclic evidence graph.
The fixed common partner and single-writer-per-term restrictions simplify its
intersection cases. The model must include a delayed old proposal racing the
fencing promise, not just decisions whose replies were received. No new term
may create a different outcome while a protected old decision remains possible.

A prepared participant without complete coordinator evidence waits and requests
recovery. It cannot infer abort from an etcd lease expiry, timeout, coordinator
death or absence of COMPLETE. If the coordinator/home group is unavailable, the
dependent closure remains unavailable. A recoverable single-host failure should
resolve automatically with valid fencing and preserved tails; missing/ambiguous
evidence instead requires waiting for a source or explicit loss recovery. Split
ownership is a placement error, never permission to fetch workload reads remotely.

## Durability, retention and snapshots

Successful completion requires every participant's complete redo/dependency prefix,
the authoritative decision and result to be locally durable and retained by the
common partner, plus the visibility barrier above. This keeps the confirmed
**one durable local copy plus partner retained memory** rule for mutations/fsync.
No second durable copy or bounded loss window is promised. An error/timeout after
admission can still precede a committed result.

Owner storage loss plus loss of the partner's volatile tail can destroy a decision,
some participant dependencies or object data. Recover available evidence and stop
the whole affected closure if it is incomplete. Never expose the destination
alone, invent an abort, or turn a dangling home reference into an empty file.
Explicit data-loss recovery selects a mutually consistent closure history in a
new recovery generation, fences or isolates former members and records which client
outcomes cannot be recovered. This accepted multi-failure gap can lose a whole
transaction; it does not authorize automatic partial-transaction exposure.

Journal/checkpoint manifests must retain transaction identity/digest, terminal
outcome, prepare/decision/install dependencies, foreign-home reference ledger,
affinity and incarnation/fence evidence. Checkpointing one participant's applied
state is insufficient to delete its transaction evidence. Compact decisions only
after every participant has a durable replacement manifest linking the same
decision and every assigned replica has durable coverage or a pinned compatible
baseline/suffix. Resolve pending transactions or retain their complete prepare
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
decision/affinity manifests and required control/fence reconciliation evidence.
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
| Same anchored ID concurrently requests A-to-B and C-to-D on disjoint owners | Both route to the immutable anchor; one protected digest claim wins, the other receives ID_CONFLICT; incompatible placement cannot cause independent admission. |
| Retry after source removal, closure merge, coordinator change or lost result reply | Consult anchor claim/binding/result before paths; recover the original bound coordinator/outcome and never allocate a second operation. |
| Anchor restarts, claim/binding reply is lost, or session floor is checkpointed | Reconcile protected anchor history; uncertain absence blocks new admission; a retired ID cannot recreate a session/request. |
| Split owners or incompatible partner/membership before admission | Placement rejection; no namespace/object effects. |
| Crash during affinity join or BEGIN fan-out | Discover partial manifests/anchors; keep all discovered members gated; reconcile the join before admission. |
| Only one participant prepared, coordinator/client times out | No visible effects; protected ABORT after safe decision resolution, or wait with locks/evidence retained. |
| Local COMMIT candidate syncs but partner receipt/reply is lost | Resolve the decision slot under fencing; never assume either success or rollback from the timeout. |
| Partner accepts COMMIT then owner fails before INSTALL | Preserve its tail, recover complete prepares, adopt COMMIT and install all members before serving. |
| Participant installed, crash before another applies | Replay the same decision behind gates; no workload sees a half-installed namespace. |
| All apply succeeds, invalidation fails or COMPLETE reply is lost | COMMIT stays irreversible; gate until visibility is safe; same-ID retry returns its actual outcome. |
| Delayed old COMMIT/ABORT after higher promises | Reject stale authority; a returning owner follows recovered history and cannot reopen old mounts. |
| Partner restarts without durable tail and owner disk is missing | Indeterminate accepted-loss window; no absence-based abort or stale async promotion. |
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
2. COMMIT implies complete protected prepares/dependencies; successful reply
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

Before affected implementation is frozen, follow-on work must cover: the common
decision-slot/normal-commit model and simulator; backing object/namespace and
journal/checkpoint schemas; closure recovery/reconfiguration and retirement;
API authentication/idempotency and shared-service integration; native FUSE cache,
locking and mmap alias conformance; and separately the guest API bridge/9p cache
fence. Missing runtime evidence blocks capability publication, not specification
of the required semantics. See [Volumes](Volumes.md), [Durability](Durability.md),
[replication](../Replication.md), [journal storage](../Journal-storage.md),
[recovery](../Recovery.md) and [platform interfaces](../Platform-interfaces.md).
