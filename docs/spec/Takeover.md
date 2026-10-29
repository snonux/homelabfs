# Ownership, fencing and takeover

Status: design contract for `eq3`, 2026-10-28. Paul approved the narrow graceful
native VM-only `native-vm-stop-v1` fence policy below. Policy approval does not
authorize installation, a VM lifecycle window or a disruptive test run.
The controller, fence broker and its attestations are unimplemented. The bounded
`gq3` model below complements `fq3` normal commitment; wider unmodeled protocol
obligations and `lv3` implementation/validation remain gates. This
document establishes no working automatic failover or production fence authority.
Scope is disposable lab/canary volumes with synthetic data.

The [bounded takeover model](../../spec/tla/README.md#bounded-ownership-transfer-model)
now checks ordinary single-volume transfer, positive native fencing, durable
promises, recovery and conditional progress. Its documented bounds and omitted
multi-volume, competing-allocation, term-conflict and physical-fence obligations
remain explicit; passing it does not complete every obligation below.

[Replication protocol](Replication-protocol.md) defines normal acceptance and
commitment; [Transactions](Transactions.md) defines immutable request anchors,
affinity and closure visibility; [Journal format](Journal-format.md) defines
the PROMISE, TAIL_REPORT, RECOVERY_SELECT, FENCE and CONFIGURATION records used
here. [Recovery](../Recovery.md) supplies the seven-step operating contract.
These contracts compose: a quorum, an etcd grant or a FENCE record alone cannot
establish serving readiness.

## Authority and transition scope

The unit of recovery is a complete dependency closure. A single independent
volume is a closure of one. Discover the transitive union of AFFINITY manifests,
partial joins and predecessor manifests, outstanding transactions, immutable
session/request anchors, bound coordinators, foreign object homes, live handles,
checkpoint dependencies and recovery pins. An empty namespace or a completed
transaction does not remove a home or anchor dependency. Initial affinity only
grows; there is no implicit closure split or unilateral partner/owner change.
Gate every discovered member before any member may resume. Missing discovery
evidence is uncertainty, not proof that a volume is independent.

Use the exact journal Authority vector: volume UUID, recovery-generation UUID,
configuration UUID, ownership epoch, owner/partner UUIDs and incarnations,
admitted replica membership and coordinator term. Normally membership has three
nodes; the two ready nodes may operate while the third is offline, retaining its
obligation. It does not become an unnamed two-member group. The entire closure
has common membership, owner and synchronous partner, with distinct per-volume
epochs/configurations/histories. Independent closures need not share a controller
gate or replication ordering.

An epoch and coordinator term are monotonically increasing u64 values within
their authorized generation/lineage; neither wraps. A new transition allocates
fresh configuration and recovery-attempt UUIDs and epochs/terms greater than
every validated promise in that lineage. A higher number from a peer is never
self-authorizing. A normal transfer preserves the recovery generation and links
the predecessor configurations and selected old histories. Intentional loss,
rollback or coordination disaster recovery uses an explicitly authorized new
generation; UUIDs are not numerically ordered. Do not compare bare sequence
numbers across streams or discard immutable creator incarnations when copying
historical bytes.

### Allocation is separate from activation

Use etcd only for infrequent, serialized ownership/configuration allocation,
current attempt registration and activation authorization. One compare-and-swap
transition reserves the affected closure and its predecessor vector. Concurrent
closure joins or proposals with overlapping member sets must serialize using
their registered predecessor manifests; retry from discovery if the set grows.
No controller may acquire a subset and activate it while another reserves the
rest. The exact etcd key codec is implementation work; it must support this
atomic predecessor check and durable attempt identity.

An etcd grant authorizes **starting takeover**, never file I/O. Replica promises,
history selection, positive old-path fencing, durable recovery and verified
activation provide the additional authority. Persist the allocation evidence as
the authorization Artifact referenced by PROMISE/CONFIGURATION. Replicas verify
the allocation, admitted identities, predecessor and closure vector themselves;
an unauthenticated higher epoch is rejected. An etcd lease expiring or a key
disappearing does not erase the attempt, promises, decisions or old cached rights.
Successor attempts must reconcile the prior attempt before proceeding.

Controller/daemon incarnation changes invalidate live sessions and fresh receipts.
They do not erase earlier accepted values or certify a lost memory tail absent.
Rebinding an owner or partner incarnation requires reconciliation and an installed
authorized configuration, never just reconnecting a socket. No per-operation
etcd call is added: admission/acceptance/completion use locally installed authority
and promises as the normal protocol requires.

If fencing stops a prospective replica itself (for example retaining the owner
while replacing its failed partner), launch it recovery-only before finalizing
the successor vector. If its new incarnation differs from an already allocated
or promised vector, preserve that abandoned vector and resume ACQUIRING with
fresh authorization/configuration and higher epochs/terms for the actual new
incarnations. Reconcile and repeat the promise barrier; never edit an allocated
Authority in place or let a handshake rewrite it. Existing old-launch revocation
remains valid, but the broker must reattest coverage against the actual successor
vector and continued old-launch inhibition. A recovery-only launch creates no
serving generation to revoke and cannot start old clients. This can repeat
safely after a further restart without inventing a fresh receipt from an old one.

## Explicit serving authority; no clock lease assumption

The initial approved policy uses positive explicit fencing. There is **no
clock-based expiry that permits a successor to serve**. Failure detectors,
etcd TTLs, PING/PONG, client deadlines and VM pause duration are hints to begin
recovery, not cache revocation evidence. Safety does not assume bounded clock
skew, bounded daemon pauses or timely cooperative shutdown. Timestamps in fence
records are diagnostic; a delayed completion cannot become valid merely by age.

A serving generation names the exact installed closure authority, owner
incarnation, visibility generation and complete admitted view/cache domain set.
Only that owner serves workload reads and writes. Before transfer, establish a
durable revocation/inhibition record for every old serving generation, and obtain
positive proof that all its access paths stopped. Pending reads, open descriptors,
kernel caches, mapped pages and dirty mappings belong to the fence domain;
blocking new FUSE RPCs alone cannot close it. New workload publication also
requires the ordinary platform cache freshness/VIEW_BARRIER contract. Takeover
fencing is not a replacement for normal-operation alias coherence.

On loss or uncertainty of coordination authority, the daemon immediately gates
new admissions and completions once detected, preserves accepted work, and
requests the external fence. Stop voluntary serving rather than continue through
an etcd outage. A paused daemon or an isolated cache may bypass that local gate;
the broker must revoke that execution domain before conflicting publication.
No claim is made that cached access ends at etcd TTL expiry. If a profile requires
a hard cached-read shutdown deadline during total management loss, this policy
does not satisfy it and the profile stays unsupported. No successor activates
while such access is unconfirmed. Reestablishing etcd quorum does not itself
reopen a previously gated volume: reconcile authority, tails and fence state.

The normal protocol still checks authority at admission and completion. Old
requests accepted before a promise remain recovery obligations; requests arriving
after the promise are rejected. An old owner can neither complete a new protected
write through a promised quorum nor regain its stopped cache domain on resume.
The new owner does not publish changes until every old path is fenced. This
separates storage acceptance exclusion from cache/read execution exclusion.

## Approved isolated fence policy: native-vm-stop-v1

**Approved policy:** permit the future broker to perform a bounded graceful
stop of the specifically enrolled disposable native FreeBSD VM containing the
old serving domain, and inhibit its restart. No automatic force-kill, VM reset,
physical-host shutdown, power-plug operation or production guest action is
included. If a paused/hung guest does not stop, takeover remains unavailable.
This trades recovery availability for a narrow, reviewable disruption boundary.

This policy is limited to the [native lab](../Lab-operations.md). Native FUSE
and all test clients run inside the same FreeBSD guest, so stopping that whole
guest removes its running client/kernel caches without claiming a cache API can
invalidate a paused mapping. f3's host FUSE/Rocky/9p probe is a different path and
is excluded. The policy does not protect an external client that retains its own
cached view; enrolling such a client requires an additional positively fenced
domain before that profile can serve.

| Property | Required policy |
|---|---|
| Allowed targets | Only enrolled `hfs-test0`, `hfs-test1`, `hfs-test2` in f3's `hfslab` datastore, rooted at `zroot/bhyve/homelabfs-lab`. Names/addresses from the historical inventory are discovery hints, never sufficient authorization. The run manifest enumerates the exact eligible subset. |
| Exact identity | Pin physical management-host identity/SSH host key and host boot identity, VM UUID, vm-bhyve datastore, canonical root-owned config path/hash, dataset GUIDs and disk identities, guest node/cluster UUIDs, current VM launch identity and each old daemon incarnation/serving-generation/view set. A process identity includes its start identity, not a reusable PID. Guest IP/MAC or a matching display name alone is insufficient. Missing UUID/GUID/launch bindings block enrollment; they are not invented from the historical TSV. |
| Management path | An external approved runner reaches a restricted broker on f3 through the existing host management network/SSH, outside the private `hfslab` guest bridge. It must work with target guest networking, guest SSH, guest daemon and test etcd unavailable. No guest-to-host management relay or dependency on a HomelabFS mount is allowed. A physical f3 failure also loses this broker: it blocks promotion rather than implies power-off. This is independent of the guest fault domain, not independent physical-host power control. |
| Credentials | A distinct run-scoped management SSH principal/key can call only the broker's fixed protocol via a forced command, with no shell, forwarding, arbitrary command, path or VM selection. Broker privilege is limited by its root-owned exact allowlist. Replication/etcd/guest credentials and Paul's general administrator key do not confer this authority. The private runner key and broker attestation key stay outside guests and HomelabFS; replicas receive only the approved verifier/public identity. Installation and credential issuance need a separate approved plan. |
| Request | Bind run/approved-plan digest, policy version and enrolled-target digest, recovery UUID, predecessor/successor authority vectors, old launch/incarnation/serving generations/views, unique request nonce and controller authorization. The broker verifies every field before taking a durable action. Conflicting target/attempt requests fail; identical requests query/resume one durable operation. |
| Restart inhibition | Before signaling stop, sync a revocation tombstone and target launch inhibition on host-local storage outside VM disks, plus an external evidence copy. The broker/launcher must prevent stale launches, resumes, restores and autostarts, including after host reboot or broker restart, until an explicit recovery-only launch is authorized. All launch paths for the enrolled targets must enforce this guard; a convention to avoid `vm start` is insufficient. If that cannot be installed/proven without changing unrelated automation, the policy is unavailable. |
| Stop action | Send the existing graceful ACPI VM stop only to the verified launch identity. Serialize with launch/configuration changes. Wait at most 120 seconds; each management RPC is bounded to 10 seconds, with at most three transport attempts inside the same overall action window. Timeout is failure, never implicit escalation. A paused VM may fail this action. Existing lifecycle helpers do not yet implement the guard, identity proof or broker. |
| Positive completion | Under the same launcher/inhibition lock, the broker's launch supervisor must observe completion of the exact enrolled bhyve process (a verified process exit event/reaped supervised launch), then verify no vCPU/execution context for that VM remains and no replacement launch exists. The durable guard must prohibit restart. Enrollment cannot infer a past process exit merely because it is missing from a later process list; lost supervisor continuity needs fresh authoritative exclusion proof. A `vm stop` return code, guest silence, failed ping, closed SSH, lack of heartbeats or `vm list` text alone is insufficient. Inability to obtain authoritative host execution-state evidence leaves UNKNOWN. |
| Attestation | Broker signs a versioned canonical artifact with its dedicated Ed25519 attestation key. The artifact contains request/plan/target digests, authority/view/serving-generation coverage, observed launch exit/no-domain facts, durable inhibition identity/revision, host/broker boot identity, monotonic action ordinal, request nonce and diagnostic times. The verifier/public key is pinned to this policy and run. Store exact signed bytes as the FENCE attestation Artifact; signature verification, current guard continuity and coverage are required before recording it as satisfied. Wrong identity, stale/replayed proof, unknown verifier or incomplete coverage blocks promotion. |
| Failure and timeout | Preserve evidence/tails and the inhibition even when stop fails or the reply is lost. Retry/query the same operation; never unlock on timeout, declare death from silence, fall back to host power or activate the new owner. If a host reboot breaks observation continuity, recheck the persisted guard and current execution state before renewing proof. Missing guard state is a manual incident. |
| Rejoin | Stop proof revokes an old launch permanently. A separately authorized, fresh launch may boot RECOVERING with a new incarnation and current authority, no old workload startup/publication. It is not a resumption of the old VM memory image. Catch up behind gates before any eligible replica role; former ownership never causes automatic failback. An old live-memory snapshot/restore remains barred. |
| Collateral impact | Stopping a VM interrupts **every** synthetic workload, mount and daemon in it, and its storage/etcd replica roles for other volumes. The approved plan must enumerate them and the quorum consequences. Include any dependent closure or refuse the action; do not label this per-volume cache revocation. Preserve unrelated accepted tails before voluntary shutdown where accessible; do not stop a target that holds the only surviving required volatile history unless it has been safely persisted/transferred. f3, its storage, Rocky, `freebsd`, production consumers and production power/NFS/CARP/monitor automation are outside the action set. |

The mechanism registry entry is named `native-vm-stop-v1`. `lv3` must assign and
pin its nonzero mechanism UUID, exact artifact codec/verifier version and
allowlist digest before any enrollment; arbitrary strings or an unknown UUID
must fail closed. The journal FENCE schema already carries this binding through
`mechanism` and `attestation`; no journal byte-format change is implied here.
The completed-at field never replaces current proof of restart inhibition.

Enrollment preflight must inspect actual state, not trust
[f3-manifest.tsv](../lab/f3-manifest.tsv), whose preparation status is historical.
It must discover the exact identities above, establish host-key pins separately
from guest pins in [f3-known_hosts](../lab/f3-known_hosts), verify that every
client/cache domain is enclosed and every launch route guarded, and demonstrate
management access under the approved guest partition. No such infrastructure
inspection or demonstration was performed for this specification.

The three native guests consume 6 GiB together; their lifecycle remains separate
from the approximately 12 GiB Rocky guest on the approximately 16 GiB f3 host.
They must never run together. Existing clean-stop lifecycle rules still apply.
Lab replication receivers on f2 are not fence targets or bootable replacements;
read [Lab replication](../Lab-replication.md) before any separately authorized
recovery. A replicated old guard/VM snapshot cannot restore authority.

### Profiles and reserved decisions

| Profile/choice | Decision and limitation |
|---|---|
| Native direct-FUSE test clients entirely inside an enrolled guest | Paul approved the graceful whole-VM policy above; implementation and `lv3` evidence remain pending. Hung/paused guests can prevent progress. All F01/F02 safety assertions still require blocked promotion in that case; successful failover is not promised. |
| Native hard-stop alternative | A separate proposal could allow terminating only a verified disposable bhyve launch after the graceful deadline, positively proving execution stopped and retaining the guard. This could cover paused-VM progress, but adds abrupt power-loss/data-recovery risk and requires a revised policy and explicit approval. It is not enabled by this document or ordinary run flags. |
| Shared f3s canary or existing Rocky/9p probe | Bounded non-disruptive smoke only. No grant to stop Rocky, r0–r2, production guests, physical hosts or shared power. Automatic takeover/cache-fence capability stays unpublished. Guest-only fencing is insufficient when a stale host daemon/local client remains able to serve. |
| Isolated guest-plus-host/9p deployment | Future supported profile must enroll every host and guest execution/cache domain with independently reachable control and positive exclusion proof. No actual physical power/console route is selected or presumed from IQ. It remains blocked until a separate isolated target inventory, concrete mechanism, approval and evidence exist. |

Paul approved the narrow graceful native policy on 2026-10-28. The abrupt
disposable-VM alternative remains outside that approval. A task-completion
annotation, blanket routine approval or `--approved-run` flag does not authorize
an actual disruptive operation. Installation and each disruptive VM window
still require the approvals in
[Test automation](../Test-automation.md).

## Durable controller state and resumable steps

Expose three independent readiness conditions:

| Condition | Meaning |
|---|---|
| Alive | Process/event loop can report health. Waiting/replay alone is not liveness failure. |
| RecoveryReady | Authenticated, correct format/identity, usable unlocked local storage, bounded capacity and gated staging can receive recovery data. It grants no workload reads or write acceptance role. |
| ServingReady | All activation predicates below hold for the exact current closure/view generation. CSI/publication/workloads consume only this condition. |

The controller has a per-volume durable entry but serializes a transition across
the closure. Its record includes recovery UUID, predecessor/successor vectors,
closure/predecessor manifests, allocation identity, intended roles, highest
promises, source/checkpoint and selected-cut vectors, every acceptance/dependency
and fence reference, pins/retention obligations, durable transfer/apply progress,
visibility generation, activation authorization, state/step completion and wait
reason. Keep resumable intent/evidence outside disposable payload directories;
recovery/coordination/broker roots cannot depend on a serving HomelabFS mount.
Replicated journal evidence is authoritative for protocol facts; a local progress
file cannot override it.

States are RECOVERING -> ACQUIRING -> PROMISING -> FENCING -> SELECTING ->
PERSISTING -> APPLYING -> ACTIVATING -> SERVING, with CATCHING_UP as continuing
background work. Any active state can enter WAITING(reason) without deleting
progress, or NEEDS_OPERATOR(reason) for unresolved history/fence-policy incidents,
or QUARANTINED for corrupt/conflicting evidence. WAITING automatically retries
when prerequisites return; it is not a permanent approval gate. No wait deadline
converts missing data or authority into success.

For each step, persist its intent before side effects, use the same recovery
identity on retry, and record verified completion only afterward. After restart,
query actual storage/broker/etcd state and reconstruct completion from verified
evidence. A lost reply is uncertainty, not rollback. Partial configuration
activation remains entirely gated; never compensate by restoring an old promise.
If a successor controller supersedes an attempt, it carries forward all promises,
selected candidates, fences, pins and configuration predecessors. Epochs cannot
be reused even when an attempt never served.

| Recovery.md step | Durable transition and completion evidence |
|---|---|
| 1. Stop admissions; obtain authority | Discover/gate the complete closure, preserve admitted requests and results, pin history and register one allocated attempt by predecessor CAS. A partitioned old owner need not cooperate; its exclusion is not yet established. Mark all destinations non-serving. |
| 2. Persist promises and preserve tails | For every closure member obtain a required old-configuration storage majority, serialize acceptance versus promise and collect exact PROMISE/TAIL_REPORT plus complete accepted graphs. Persist promised floors and captured accepted tails/dependencies before replying. Quorum means two of that admitted three-member group, never two arbitrary nodes. Partial vector completion is not activation. |
| 3. Revoke old read/cache paths | Obtain verified FENCE coverage for every prior serving generation and client/cache domain using the approved concrete policy. Preserve the target's unique surviving volatile obligations before voluntary stop. Persist fence references and continue to verify guard continuity. Unknown/failed fencing stays gated. |
| 4. Select authoritative history | Resolve all old ordinary and transaction slots with the rules below, including uncertain replies and bound coordinator terms. Record RECOVERY_SELECT and dependency graphs. Holes, lost continuity or incompatible histories stop selection; do not pick the newest timestamp or largest position. |
| 5. Persist recovered pair | Re-protect selections/results/dependencies in the authorized prospective pair and require **both** owner and new partner to durably hold the complete recovered cut, owner/reporter streams, payload bases and request/session/affinity/fence graph. This mandatory takeover barrier is stronger than ordinary memory ACKs and does not change normal fsync semantics. Persist pair-ready evidence and source pins. |
| 6. Apply and activate | Replay selected redo by immutable identity behind gates, reconstruct metadata/installs/results, satisfy cache/view barriers and one closure visibility generation, verify all activation predicates, and durably install CONFIGURATION with exact predecessors, recovered cut, promises, fences and partner readiness. Register matching activation authority before opening any member. A controller crash at publication requires revalidation; no mount-directory fallback. |
| 7. Catch up remainder | Retain offline replica obligations across old/new epochs and configurations. Transfer exact history or install a pinned compatible paired checkpoint and required suffix. Record durable progress, verify all dependencies and only then release replaced pins. Preserve divergent/failed copies until safe quarantine release under policy. |

The controller has bounded transport attempts, queues and retry backoff with
separate client deadlines; no infinite request wait consumes unbounded buffers.
Expose closure/volume, state, reason, source, selected cut, recovery identity,
epochs/configurations, old-path fence coverage, last verified progress, lag and
operator-action need. A long catch-up keeps Alive and RecoveryReady true where
appropriate; it does not trigger liveness kills that destroy retained tails.

## Promise and history-selection rules

At each replica, acceptance and PROMISE persistence share one serialized state
machine. An acceptance that wins the race is retained with its exact candidate,
term, prefix and dependencies in the report. A promise that wins prevents any
later acceptance below its epoch/term, including delayed old-session work after
restart. Sync accepted memory/evidence captured by the promise before replying
to recovery; sync the promise itself and its name/publication barriers. If a
sync fails, withhold the reply, gate the replica and preserve diagnostic state.
Do not free the accepted arena during this transition.

On restart load the highest durable authorized promise before network acceptance
or workload publication. Checkpoints/replacement catalogs retain promised floors
and all semantic replacement evidence. Restoring an older local disk/snapshot
does not establish a lower legitimate promise: quarantine/recover it against
current authority before participation. Fence broker inhibition is outside that
rollback domain. Administrative rollback of all witnesses/guards is outside
ordinary recovery and needs controlled new-generation reconstruction.

Collect actual full owner/reporter frames and dependencies, not just hashes or
R/D/K/P maxima. Tail continuity is an explicit fact: either the still-live
incarnation retains the complete prior accepted tail or equivalent complete
durable coverage proves it. A restarted partner with a shorter durable prefix
reports false/unknown continuity even if it has no locally visible missing IDs.
No empty report from it proves an unobserved accepted record never existed.

For each ordinary COMMIT or transaction DECISION slot:

1. Locate the immutable request claim and binding first. Use its bound coordinator
   and slot identity even if a closure merge changed the currently lowest UUID.
   Preserve original intent/digest, internal operation association, session floors,
   closed-session/retired-anchor rejection and complete stable results. Never
   assign a retry a new coordinator, anchor or redo because a path disappeared.
2. Reconcile exact accepted candidates/terms from the intersecting fenced
   storage majority. Adopt the highest accepted term/value consistent with any
   known protected outcome; a protected COMMIT/DECISION cannot be reversed.
   Same-term conflicting candidates or incompatible higher evidence quarantine
   the affected closure. Unavailable payload, prepare, home or acceptance graph
   blocks progress rather than authorizes a different outcome.
3. Absence-based abort is allowed only with fenced-majority proof containing the
   old common partner's continuous accepted tail or equivalent complete durable
   history, with no constraining accepted candidate. Missing COMMIT, RESULT,
   COMPLETE or a lagging async copy is insufficient. The promise must close the
   race with a delayed old proposal; an absence claim taken before that cannot
   authorize abort. Preserve partial prepares and all old evidence.
4. A complete owner-only unprotected candidate may be adopted conservatively.
   A candidate ruled out by valid fenced absence cannot revive on old-owner
   return. If not enough evidence exists for either adoption or safe absence,
   wait for the source or stop for an explicit loss decision. A timeout, Cancel,
   etcd expiry or participant vote cannot substitute for this decision.
5. Persist RECOVERY_SELECT with exact promises/reports/fences/dependencies and
   re-protect its selected value or proven abort in the new pair before apply.
   Resume INSTALL/COMPLETE and reconstruct protected anchor/ordinary terminal
   results from complete verified evidence. Loss of a client reply does not
   erase the outcome. All earlier slots must be resolved before a later prefix
   is publishable; no scalar maximum can bridge an unresolved hole.

Selecting a prior COMMIT references its immutable old candidate; copying does
not rewrite its creator authority or incarnation. New recovery records live in
their authorized streams/terms and link the old evidence. A transaction COMMIT
requires every bound protected PREPARED/dependency; a prepared participant never
unilaterally aborts. Selection and publication cover all homes/anchors and the
shared closure visibility generation, preserving the Transactions two-phase
contract. Recovered result reconstruction does not invent an exactly-once
guarantee for arbitrary application syscalls after process restart.

## Activation, partner changes and rejoin

Before ServingReady can become true, verify all of these against one immutable
activation vector:

- Current etcd allocation/activation authorization agrees with the predecessor
  lineage, complete affinity closure, new incarnations and durable promises.
- Every old serving generation/cache path has verified positive fence coverage
  and continuing restart inhibition. No old execution domain can resume I/O.
- Complete selected histories, protected outcomes and transitive dependencies
  exist durably on both prospective nodes; every unresolved slot affecting the
  visible cut has been resolved, and offline catch-up obligations remain pinned.
- Both nodes verify exact baseline/cut and compatible authority; the new partner
  is READY for the full recoverable prefix in its current incarnation. Async
  receive/apply progress alone grants no synchronous role.
- The owner has applied the selected committed closure cut, restored exact object
  and namespace identities/results and completed admitted view/cache barriers.
  Both prospective replicas install the CONFIGURATION transition for every
  closure member and one closure visibility generation before any member opens;
  the offline third replica must install it during gated rejoin. Mount identity and fresh publication are
  verified; no raw backing directory or partially installed checkpoint is served.

Promotion may select the former synchronous partner or the former asynchronous
replica. In the latter case, the destination remains fully gated while fetching
the accepted tail, dependencies and old/new history from the surviving partner.
Destination role, smallest UUID or an etcd response cannot waive that backlog.
With B and C surviving A, either B/C or C/B is a prospective owner/partner pair;
both must meet the same durable takeover cut before activation. New writes then
resume the normal owner-durable/partner-memory contract. Returning A retains its
offline obligation; it is not removed to make GC easier.

If the sync partner fails while the owner lives, stop new protected completions.
Recover its acceptance evidence and catch the third node up through a fenced
configuration transition; do not change a connection's role in place. The initial
policy closes/revokes the old serving generation even when retaining the same
owner, so the native policy may restart that owner's whole guest. This is an
explicit availability/collateral cost, not permission to silently reuse a cached
view across a configuration change. Independent closures may continue subject
to the whole-VM fence impact and shared resource limits.

Initial automatic transitions keep the admitted three-member set fixed.
Admitting a fourth cluster node gives it no volume authority. Copying a replica
there is recovery-only. A future membership replacement must use a separately
specified and modelled overlapping/joint transition with old and new group
majorities, complete retained history and the closure vector preserved. Until
that protocol is available, fourth-node admission/promotion races reject volume
membership changes; two arbitrary reachable nodes never form an authorized pair.
This deliberate boundary does not block changing the synchronous role among the
existing admitted three.

A former owner always boots RECOVERING, with new daemon/VM launch identity,
workloads inhibited and no automatic failback. Compare its full old history to
the selected lineage. Preserve divergent suffixes and uncertain operations in
read-only quarantine with their original frame identities and diagnostics; do
not concatenate them into the new owner stream or replay path effects blindly.
Copy to a separate gated recovery destination or install a verified paired
checkpoint/suffix. Only after complete catch-up, current promises/configuration
and handshake reconciliation may it become a passive eligible replica. A later
planned failback is another full fenced transfer, not an automatic preference.

## All-node reboot and exceptional recovery

Cold boot starts with clients/publication disabled. Load local storage/key and
admitted identity policy, replay control/promises/journal evidence and reconcile
etcd authority plus broker revocations before requesting any serving role. Roots
for coordination, broker guards and guest boot do not require a workload volume.
The first two nodes online are not automatically authoritative. Reconstruct all
configuration predecessors, paired checkpoints, owner/reporter acceptance graphs,
immutable claims, partial closure joins and required home/prepare histories.
Await a delayed old owner if its durable prefix is required to recover an
acknowledged operation whose partner memory vanished. If its disk survives,
temporary unavailability is preferable to selecting an older async copy.

Use the same fenced selection/activation procedure, treating all old launches
and serving generations as needing reconciled exclusion evidence. Reported
shutdown or reboot alone is not proof of all peer execution domains being dead.
If complete durable histories survive, reconcile their semantic evidence and
resume automatically; absence of volatile acceptance after reboot does not prove
an unrecorded old outcome absent. Never choose by the maximum epoch/sequence,
snapshot timestamp, number of matching replicas or restart order.

The accepted durability gap remains: loss of owner storage plus loss of the
partner's retained memory before persistence may remove acknowledged operations
or entire transaction dependencies. A durable promise does not restore those
bytes. Missing or ambiguous evidence leaves the closure unavailable, with copies
and pins preserved. Report usable sources/checkpoints, unknown client outcomes,
missing histories, candidate/protected outcomes and fence status. Offer concrete
choices: wait/recover the missing source; restore a compatible external baseline;
or explicitly accept a stated loss cut. Do not silently merge branches, fabricate
payloads, mark uncertain operations aborted or choose stale async state.

Authorized loss/rollback creates LOSS_RECOVERY in a new generation with a
consistent complete closure cut, excluded ranges/unknown request outcomes,
preserved old copies and positive exclusion of all former serving domains.
Normal rejoin cannot import excluded suffixes. Emergency local-only activation,
admission and exit remain separate explicitly authorized workflows; no shortage
of replicas or shutdown deadline automatically enters emergency mode.

Restoring etcd from an old snapshot or losing its quorum permanently is not a
normal allocation retry. Stop publication, reconcile its lineage with storage
promises and all durable fences, and use a controlled operator-authorized new
recovery generation before reuse. Do not use a newly reset etcd revision to
override a higher storage promise or reopen an inhibited VM. Concurrently restored
stale metadata/guard stores require manual reconstruction, never automatic trust.

## Model and validation obligations

The `gq3` takeover model (composing with `fq3` normal commitment) must name the
**approved native-vm-stop-v1** assumption:
an authorized external broker controls every launch of each enrolled native
cache domain; a successful verified stop leaves no old execution and a durable
guard bars its return. The broker is outside the target VM fault domain but may
fail with f3. Unavailable management, hung ACPI stop, unknown host identity or lost
guard state has no successful Fence action. No clock or pause bound permits
promotion instead. Abstracting Fence as always successful would not model this
policy. Implementation must prove correspondence of its observations/guards to
the abstraction; model success cannot establish that correspondence.

Check these safety invariants with bounded streams before implementation:

1. One activated authority/history per closure; no two unfenced execution/cache
   domains observe conflicting post-transfer state. Every successful successor
   read/write requires the full activation vector, never just etcd allocation.
2. Promised epochs/terms never regress across crash, reconnect, failed recovery or
   local snapshot restore; promise-versus-accept ordering retains every acceptance
   that preceded the fence and rejects every stale proposal afterward.
3. A protected ordinary/transaction outcome is never replaced. Highest-accepted
   term recovery and fenced absence preserve all constraints and complete
   dependencies; restarted volatile-tail loss never proves absence.
4. No acknowledged operation is lost when its promised surviving data/evidence
   is available. The accepted multi-failure durability gap yields unavailable or
   explicitly authorized new-generation loss, never fictitious recovery.
5. Closure discovery includes partial joins, immutable anchors, bound coordinators
   and foreign homes. No subset serves, changes partner or advances visibility
   while a dependent member is unreconciled. A retry cannot acquire a second
   digest, identity or effect after source removal or closure merge.
6. R/D/K/P and promises remain distinct, same-history, contiguous facts. Chosen
   cuts cannot skip unresolved earlier slots; journal numeric maxima cannot select
   across streams. Full graphs and payload bases survive selection and GC.
7. Recovered pair durability precedes activation; normal writes afterward still
   need owner durability and current partner retention. Async role, stale receipt
   or old incarnation never substitutes for that partner.
8. A FENCE is scoped to exact launches/views/serving generations and remains
   enforced across controller/broker/host restarts. Wrong-target, replayed proof,
   stale guard or partial coverage cannot open a gate. A returning old launch
   remains barred; a new replica launch starts RECOVERING without failback.
9. Crash after any durable step, lost replies and competing attempts resume one
   reconciled lineage without erasing decisions, promises, fences, retention
   obligations or stable terminal results. No failed cleanup restores authority.
10. Served state is selected/applied and cache fresh; pending install, alias drain,
    mount replacement or checkpoint publication cannot expose a partial closure
    or a raw/empty-directory fallback.

Liveness is conditional on stable coordination quorum, complete recoverable
history, authorized reachable broker with a stoppable target, intact launch
guards, a usable prospective pair, finite backlog/admission and fair workers.
Under those assumptions takeover/catch-up should progress without journal edits
or repeated manual approval. Remove any assumption and safety must remain while
the volume waits; a deadline does not supply authority or data.

| Planned scenario | Required evidence; no runtime pass claimed |
|---|---|
| R01–R03 | Owner loss after partner memory acceptance, accept-before-local-sync, lost replies and partner reincarnation preserve/recover full tails; missing memory never counts as no old acceptance. |
| R04–R06 | Async promotion waits for authoritative backlog and durable new pair; old/new epoch async obligations persist; three volumes in different roles recover separately except declared closure/whole-VM impact. |
| R07–R09 | Divergent former owner quarantines with no failback; multi-failure loss waits for explicit decision; all-reboot permutations and delayed owner use semantic lineage rather than maxima. |
| R10 | Crash each controller/replica/broker around every intent, sync, stop, attestation, pair barrier and activation edge; resume idempotently with no double publication. |
| F01–F02 | Partition old owner while native clients/cache hits remain live; pause daemon/VM and replay old messages. Successor never opens before positive VM exclusion. Graceful-stop timeout/paused guest can deliberately yield unavailable; no force-kill or invented expiry success. Include old descriptors and mmap observations. |
| F03–F04 | Separate etcd-only, replication-only and management-path partitions; quorum/leader restoration revalidates authority. Record local gate detection and actual fence completion separately; no claim of cache expiry at the etcd TTL. |
| F05 | Race proposals, sync replacement, placement and fourth-node admission; serialize closure attempts and reject unsupported membership replacement. Add joint-transition success cases only after that protocol is specified/modelled. |
| Fence negatives | Wrong UUID/config/disk/launch, PID reuse, stale/replayed artifact, broker restart, host reboot, missing guard, concurrent start and incomplete view coverage all block activation; no operation outside the approved allowlist. |

Native FUSE evidence cannot certify Rocky/9p, independent physical-host loss or
power control. Such unsupported profiles must remain explicitly not run/blocked.
This specification was drafted against the local contracts. Subsequent bounded
model evidence is recorded above; no daemon, fence, kernel/cache, VM or runtime
fault test was executed to produce this specification. Every future
runtime exercise remains subject to [the per-run approval boundary](../Test-automation.md).
