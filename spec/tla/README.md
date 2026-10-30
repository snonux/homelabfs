# Fixed-owner replication model

This is the `fq3` bounded normal-mode model of
[Replication-protocol.md](../../docs/spec/Replication-protocol.md), with the
successful-return contract from [Durability.md](../../docs/spec/Durability.md).
It models one volume and three fixed roles: owner A, synchronous partner B,
asynchronous replica C. It is neither HomelabFS implementation evidence nor a
proof for arbitrary bounds. No lab, SSH, FUSE or VM operation is involved.

Run from the repository root with Java and Mage installed:

```sh
mage tla
mage tlaNegative
```

The runner executes real exhaustive TLC checks sequentially, checks the process
exit status, and leaves logs/metadata below `.cache/tla/`. Each checker uses one
worker and a 2 GiB maximum JVM heap. It does not turn a timeout into success.
The initial safety and partner-restart liveness runs used 512 MiB; TLC warned
that the latter's complete temporal graph was low on memory. The runner's 2 GiB
cap addresses that measured limitation, with one checker at a time.
`-lncheck final` runs temporal cycle checking on the completed graph, avoiding
repeated cycle checks on partially explored graphs; it does not disable liveness.
Negative checks succeed only when TLC reports the expected `Safety` invariant
violation and exit code 12; normal checks require exit code zero. A negative
counterexample is an intentional model mutation, not a defect found in the
written protocol.

`tla2tools.jar` version 1.8.0 comes from the
[official tagged GitHub release](https://github.com/tlaplus/tlaplus/releases/tag/v1.8.0).
The runner checks SHA-256
`fc6878c603f0fdc18b9fe3e5bf51860cb90ab5b1154e3c371660c036d6bdd63e`
on both downloads and cache hits. Its bytes also match the release's published
SHA-1 `bdfd4b754eee174f0bcdeaaffb96fa4d04bfbda8` (checked when pinning).
The cache, jar, logs and checker state are gitignored. A mismatching existing
jar fails closed for inspection; the runner never silently executes it.

## Recorded verification

Local checks on 28–29 October 2026 completed with no reported errors, zero queued
states and Java exit status zero for each positive profile:

| Configuration | Generated states | Distinct states | Depth | Checked properties |
| --- | ---: | ---: | ---: | --- |
| `Safety` | 29,327,420 | 2,970,447 | 52 | `Safety`, both fault budgets |
| `Liveness` | 6,558,693 (resumed) | 992,091 | 52 | `Safety`, `AsyncCatchup`, `Completion` |
| `LivenessOwner` | 7,074,239 | 631,009 | 45 | `Safety`, `AsyncCatchup`, `Completion` |
| `Ordered` | 3,165,837,794 | 186,525,073 | 77 | `Safety`, two operations |

These are completed per-profile runs, not a claim that one uninterrupted
`mage tla` invocation passed. Safety used 512 MiB; the other positive completions
used 2 GiB, one CPU/worker and final-only temporal checking. Safety's `Init`,
`Next`, invariant and configuration were unchanged by the later liveness-only
fairness refinement. The partner-restart liveness check used supported TLC
checkpoint recovery, restoring 483,225 examined states and 79,438 queued states;
its generated count above covers resumed work. Both temporal branches completed
in 22 min 49 s. Ordered completed in 8 h 58 min.

The retained successful logs are `Safety-3364523667/output.log`,
`live-b-clean-20261029-1958.log`, `live-owner-2048.log` and `ordered-2048.log`,
all below `.cache/tla/`. The latter three have explicit `.exit` files containing
zero. Early temporal attempts ran out of heap or were interrupted and are not
passes. One earlier recovered B run printed full success output but its outer
tool status was an unexplained 143; the unchanged copied-checkpoint recheck above
supplies the clean Java exit receipt without treating that anomaly as resolved.

TLC uses finite fingerprints, so exhaustive exploration is conditional on no
distinct states colliding. Ordered reported collision-probability estimates of
`.030` (calculated optimistic) and `.0012` (based on actual fingerprints); these
are verification limits, not a collision-free mathematical proof. The other
profiles' estimates are retained in their logs.

`mage tlaNegative` completed all three expected `Safety` violations with TLC exit
12: gap acknowledgment (38 distinct states), stale incarnation (409), and double
application (2,745). The runner returned success only after checking those
expected failures. Tagged Mage compilation, `go vet` and `errcheck` also passed.

## State and checked contracts

Each operation has a fixed identity and immutable intent. Its three owner-stream
positions are MUTATION, COMMIT, ORDINARY_RESULT. SESSION/REQUEST_CLAIM and their
protected immutable association are assumed installed before this suffix.
Commit's reporter ACCEPT is a separate typed control message, with its own
partner incarnation; it does not allocate an owner-stream position. Full reporter
stream segmentation and graph transfer are abstracted by that message and the
complete evidence embedded in the later result.

`tail` and `durable` separate A's complete local append and sync. B/C retain sets
of complete exact frames: receiving position n+1 first cannot advance `R` over n.
B's durable prefix and durable acceptance set are independent of received state;
ACCEPT can precede A sync and B sync. Chosen state requires A's covering sync and
a current-incarnation ACCEPT; a plain prefix receipt cannot choose COMMIT.
ORDINARY_RESULT is appended after choosing and needs its own durable/receipt cut
before reply. `decided` and `applied` remain distinct ordered operation prefixes.

The network has one bounded slot per A-to-B and A-to-C data channel, plus a
separate B-to-A control slot. Send chooses any existing frame, including later
positions first. Delivery keeps the slot available for duplicate delivery; drop
removes it; the journal remains the retry source. Thus loss, duplication and
reordering can recur arbitrarily, without an unbounded message queue. This is a
small transport abstraction, not a connection/credit implementation.
New sends are limited to three positions beyond the receiver's contiguous cut,
an explicit bounded out-of-order window. Earlier positions may always be resent.

B restart discards volatile frames and ACCEPTs, keeps its verified durable
coverage, increments its incarnation, clears A's learned current receipts and
closes readiness. A retained in-flight old control message remains in the graph.
Those gate/receipt changes at A happen in the same model step as B's RAM loss:
this is a synchronously observed restart abstraction, not a remote crash detector.
Reconciliation restores A's durable prefix before readiness; old control messages
cannot count as fresh receipts. Same-term exact historical bytes can be resent.
Owner crash loses its unsynced tail and current collected receipts; restart keeps
the same owner, authority and intact disk. Already durable operation identities
and deterministic exact candidates are never replaced with opposite outcomes.

`decided`, `applied` and `effects` survive modeled daemon/B RAM restarts as an
abstraction of reconstructing the same selected history and idempotent semantic
effects. This does not prove that volatile decision/apply metadata survives a
real restart, that payloads/checkpoints are durable, or that a physical storage
crash is safe. At A, fresh partner acceptance is required before newly appending
a result or applying; passive apply requires the complete result prefix restored
in current retained bytes. Physical storage loss and checkpoint reconstruction
are excluded from these restart transitions.

`chosen` and `replyWitness` are monotone external-oracle history. Reply witnesses
capture the actual local durable cut, B retained cut and exact acceptance,
result receipt and authority at the reply transition. Later legitimate RAM loss
does not retrospectively invalidate a previously protected reply. These ghost
variables do not grant a new acceptance, reply, or data-transfer right.
The reply-time physical B-retention witness is conditional on the observed-restart
abstraction above. A real authenticated receipt is historical retention evidence;
it cannot instantly reveal a later remote RAM loss.

`Safety` checks:

- Every partner ACCEPT covers the complete contiguous COMMIT prefix.
- Fresh learned ACCEPT/receipt state is backed by current partner retention.
- Every historical successful reply had authority, a durable local result prefix,
  B's retained result prefix and actual acceptance of its COMMIT.
- Decisions and complete ordered apply are restricted to chosen operations.
- Per-node observable semantic effect counts equal one for applied IDs and zero
  for all others, even after duplicate delivery and exact-ID apply retries.

Apply is an atomic exact-ID redo/cache-barrier transition. The conservative
profile waits for owner apply before success. Repeated apply cannot increment
the semantic effect count. This verifies the abstract dedup guard, not a real
journal/checkpoint dedup implementation or arbitrary syscall retry identity.
Retries at/below the applied prefix are semantic no-ops; actual payload byte
values, exact redo implementations and checkpoint reconstruction are not modeled.

## Bounds and fairness

`Safety.cfg` allows one operation (three owner frames), one B RAM-loss restart,
and one owner crash/restart, in either order and at any enabled phase. Fault
budgets are maxima, not obligations: zero faults and every shorter execution are
included. `Ordered.cfg` raises the bound to two operations (six frames), with
no crashes, to check inter-operation ordering. There are no state constraints,
depth cutoffs or simulation-only passes.

`Liveness.cfg` permits one B RAM-loss restart and keeps A running.
`LivenessOwner.cfg` permits one owner crash/restart and keeps B's incarnation
stable. Each has one operation and checks both liveness properties. Splitting
these two fault probes bounds the temporal graph; combined-fault liveness is not
claimed. Both faults together remain in the safety configuration.

Liveness uses the same safety transition system with explicitly fair workers and
delivery. `AsyncCatchup` requires every chosen operation eventually be applied on
C; `Completion` requires every locally durable candidate eventually receive its
protected successful reply. This is stronger than receipt-only catch-up.
Both are conditional on:

- Fixed authorized owner/pair and intact owner durable storage/history.
- Finite admitted load, with complete redo/dependency bytes retained forever in
  this bounded model; no GC, capacity exhaustion or storage errors.
- Finite faults and eventual running owner/reconciled partner.
- Weak fairness of enabled local sync, acceptance, choose, result, apply, reply,
  restart/reconciliation and channel-clearing workers.
- Fair transmission opportunities: on each channel, a persistently missing next
  frame must recur in its wire slot; a pending ACCEPT or current prefix receipt
  must likewise recur in the current-incarnation control slot. These predicates
  refer to queued messages, never eventual receipt, reply, choose or apply.
- Strong fairness of delivering the **next missing contiguous frame**, each
  operation's ACCEPT and current prefix receipts. A frame may be lost arbitrarily
  often, but recurrent delivery opportunities cannot starve it forever. Later
  frame reordering and duplicate traffic remain allowed.
- Functioning complete apply/cache barriers, abstracted by enabled apply actions.

No fairness is placed on admission, and neither property requires all possible
operations to be admitted. Reachable successful/chosen executions also exist;
the negative controls exercise the same graph rather than an empty workload.
Without these liveness assumptions the safety checks still allow indefinitely
lost messages, blocked workers and unavailable peers. No finite latency or loss
window is established.

The finite stream and finite fault budgets eventually stabilize the desired next
frame/incarnation whenever progress is blocked. Weak fairness of slot clearing
and recurrent queuing then supply infinitely recurring delivery opportunities;
strong fairness of delivery forces actual retention. Repeating this argument
advances the finite contiguous prefix. The same argument carries pending ACCEPTs
and receipts. Fair workers then choose, append results and apply. This explicit
queue-opportunity formulation avoids TLC's expensive branching over separate
strong-fair send actions while preserving the missing-prefix retry obligation.

## Negative schedules and explicit limits

`NegativeGap.cfg` incorrectly accepts a COMMIT received across a gap.
`NegativeSession.cfg` incorrectly permits old-incarnation control messages after
restart/reconciliation. `NegativeDedup.cfg` incorrectly increments an already
applied operation's semantic effect count. TLC must reject all three. Their logs
preserve the concrete schedules for inspection.

The protocol document lists obligations wider than this baseline. In particular,
this model **does not establish**:

- Delayed or unobserved B restart, an ACK sent before RAM loss but processed
  afterward, or RAM loss between receipt and client reply. A is informed of B's
  restart atomically here; no physical failure detector or instantaneous remote
  retention guarantee is proved.
- Higher-term promises, accept-versus-promise races, majority recovery selection,
  positive fencing, owner storage loss, or safe ownership/partner changes. The
  fixed owner may restart, but no alternate owner is activated. The `gq3` takeover
  model below checks a bounded subset separately; wider protocol obligations
  and policy/physical-fence validation remain open.
- Loss-free recovery after owner-storage loss plus B volatile-tail loss. Such
  storage loss is excluded from liveness, not turned into successful recovery.
- Transactions, cross-volume visibility, immutable cross-group anchors/bindings,
  session floors/retirement, conflicting canonical requests, path resolution or
  complete syscall retry semantics. Unique fixed IDs/intents are the baseline.
- Real hash/codec validation, missing external graph/payload dependencies, partial
  frames/torn disk tails, mutable sender buffers, reporter-prefix holes, segment
  boundaries or actual authenticated session handshakes. A frame/control record
  denotes complete immutable verified bytes and required evidence.
- GC/checkpoint/snapshot/transfer pin correctness, offline replica reassignment,
  C durable coverage or progress-record persistence. Source history is retained
  throughout, so restart cannot erase the async obligation; no memory receipt
  authorizes deletion in this model.
- Partial namespace apply, read-cut races, cached aliases, mmap, permissions,
  invalidation failure or runtime cache/fence enforcement. Apply/barriers are
  atomic abstract actions and the initial owner/profile are already authorized.

Consequently the larger negative schedules in Replication-protocol remain runtime
and future model obligations. Passing this single-volume normal model cannot
close the takeover, transaction, storage or kernel capability gates.

## Bounded ownership transfer model

`Takeover.tla` is the `gq3` single-volume ordinary-operation model of
[Takeover.md](../../docs/spec/Takeover.md). It is separate from `Replication.tla`:
the fixed-owner model and its recorded nine-hour Ordered check are unchanged.
Run only the takeover profiles with:

```sh
mage tlaTakeover
mage tlaTakeoverNegative
mage tlaTakeoverCoverage
```

The takeover runner uses one checker at a time, one CPU/worker and a 512 MiB
maximum heap. The original replication targets retain their 2 GiB cap. There are
no state constraints, depth cutoffs, symmetry reductions or simulation-only
passes. Fault bounds are maxima; every shorter and fault-free schedule remains
in the state graph. Every positive check requires Java exit zero. Deliberate
protocol mutations require `Safety` violation and exit 12. Coverage controls
instead require `WitnessAbsent` violation and exit 12; an unrelated safety
failure cannot satisfy a coverage control.

The finite scope is one already discovered independent closure member, fixed
admitted membership A/B/C, one immutable old candidate and one successor
mutation. A initially owns the volume; B is its synchronous partner; C starts
without the old bundle and may become owner only through the same B/C recovery
barrier. Either B or C may be selected as successor, with the other as partner.
There is at most one B RAM-loss restart before activation and one controller
crash/restart. The controller crash can occur before allocation or after any
durable step, including activation. An incarnation mismatch abandons the old
allocation and requires a higher allocated epoch, repeated promises and a new
attestation binding. Two epochs suffice for this one-restart bound. The selected
old outcome survives controller restart and abandoned attempts.

Each candidate denotes a complete verified immutable redo/result/dependency
bundle. Old proposals may be delivered repeatedly, after arbitrary delay,
after B restarts and after promises. A proposal accepted before B's promise is
captured and persisted by that promise; one arriving afterward is rejected.
Promise floors survive every modeled restart. An emitted ACK remains deliverable
after B loses RAM, and A is **not** atomically notified of that loss. Its durable
local bundle protects any historical successful completion; this model does not
prove instantaneous knowledge of remote retention or fresh sessions after an
unobserved crash. An empty restarted B has unknown continuity and cannot prove
absence. Explicit recovery of complete closed A history can restore continuity;
an unreachable intact A is distinct from permanent loss of its storage.

The **native-vm-stop-v1 assumption** is explicit: an authorized external broker
controls all launches of the enrolled native cache domain, persists inhibition
before signaling a graceful stop, positively observes the exact launch's exit
and attests continuing inhibition for the allocated attempt. `GracefulStop` is
disabled while the guest is paused or management is unreachable. The old cache
domain survives daemon gating, coordination/data partitions and arbitrarily long
pauses; no lease expiry, timestamp, timeout or UUID revokes it. No action resumes
an inhibited old launch. A fresh former-owner return remains recovery-only and
cannot reclaim ownership, including after passive catch-up. This is an assumed
broker contract, not proof that bhyve, launch guards or kernel caches implement it.

Selection follows B/C durable promises and a verified fence. It adopts actual
complete history or proves absence from a continuous captured tail. Recovery
only reads immutable durable owner history after `ReturnFormerOwner` establishes
a fresh recovery-only launch. `old.running` always refers to the revoked old
launch/cache domain; return cannot set it true. `ownerReachable` is source
availability, not evidence that a stopped old guest can answer SSH or resume its
cached workload. Before voluntarily stopping a
source, its unique proposed bundle must be durable on A or B. Selected bytes,
results and dependencies become durable on **both** prospective nodes in
`ProtectPair`; `pairDurable` abstracts that complete two-node transfer/persistence
barrier. It is not a per-frame transport or reporter-graph proof. C's lag is
represented by the missing pair barrier until this step; owner labels B/C share
the same pair, selection and publication rules.

Configuration install, apply/view freshness and activation authorization are
separate steps. A controller crash preserves their durable evidence and gates
further controller work until restart. A successor read observes the selected
cut; a successor mutation abstracts the already specified owner-durable plus
partner-retained ordinary commit and view barrier. Old cached reads bypass the
daemon admission flag and are checked independently. The safety invariant checks
activation exclusion, stale reads/epochs, preservation of acknowledged and
retained accepted values, pair readiness and no failback. The temporal safety
property `DurableStateNeverRegresses` checks monotonic promises/epochs, immutable
selection and persistent stop/inhibition/acknowledgement facts across every step.

`DeclaredGap` requires permanent A storage loss **and** loss of B's accepted RAM
tail before persistence. A timeout or reachability change cannot set it. Even
this gap does not permit an older automatic selection: incomplete evidence
leaves selection and activation blocked. Explicit authorized LOSS_RECOVERY into
a new generation is outside the model. Old excluded owner-only candidates stay
quarantined rather than being imported by continuity restoration.

Liveness profiles check eventual activation and passive former-owner catch-up.
They retain the same transition system and assume eventual stable coordination,
management and source access with an unpaused stoppable target; finite faults;
intact A durable storage; and weak fairness of concrete enabled allocation,
restart, promise, fence, owner-sync, recovery, install, apply and catch-up workers.
They assume neither eventual activation nor a successful selected outcome.
Safety profiles make no fairness or eventual-availability assumption. There is
no liveness claim for permanent source loss, a permanently paused VM or missing
management. Fault+environment liveness together remains outside the separate
temporal profiles; combined schedules are checked for safety.

### Takeover verification and counterexamples

On 29 October 2026, local exhaustive checks completed with zero queued states,
no reported errors and explicit Java exit zero for every positive profile:

| Profile | Generated | Distinct | Depth | Fault scope |
| --- | ---: | ---: | ---: | --- |
| `TakeoverPreflight` | 4,458 | 1,081 | 22 | One epoch, no faults |
| `TakeoverFaults` | 211,048 | 41,247 | 29 | Two epochs, controller crash, B RAM loss, A storage loss |
| `TakeoverEnvironment` | 1,222,405 | 63,264 | 25 | Controller crash; all coordination/source/management/pause combinations |
| `TakeoverCombined` | 15,157,649 | 744,528 | 30 | All bounded faults and environment changes together |
| `TakeoverLiveness` | 113,996 | 23,946 | 28 | Two epochs, controller crash and B RAM loss; intact A |
| `TakeoverLivenessEnvironment` | 400,061 | 21,088 | 23 | Arbitrary environment changes followed by stable availability |

All runs used 512 MiB maximum heap, one CPU/worker and final-only temporal
checking; the combined safety graph completed in 101 seconds. The checked
`Takeover.tla` SHA-256 is
`86f634a164e01a7fd4dde968aed552e21af6f7082cb837cb617db048d25d2002`.
The evidence directories below `.cache/tla/` contain `output.log`,
`java.exit`, exact source/config copies, SHA-256 hashes and command arguments:
`TakeoverPreflight-_qsp9ahn`, `TakeoverFaults-pzspk3vn`,
`TakeoverEnvironment-s93kxpxz`, `TakeoverCombined-qmr2vzhp`,
`TakeoverLiveness-6736dtg9` and
`TakeoverLivenessEnvironment-ygqxta18`.

All three protocol mutations produced the required `Safety` counterexample and
exit 12: `TakeoverNegativeFence` allows publication without positive VM exclusion
(double activation), `TakeoverNegativeEpoch` accepts an old proposal after a
promise/restart, and `TakeoverNegativeAbsence` treats a restarted empty tail as
absence (discards acknowledged old history). Their evidence directories are
`TakeoverNegativeFence-1vp2mdo3`, `TakeoverNegativeEpoch-moj5itl7` and
`TakeoverNegativeAbsence-565oplsf`. These are intentional mutations, not defects
discovered in the approved written protocol.

Four additional coverage profiles refuted `WitnessAbsent` while `Safety` held:

- `TakeoverWitnessSync`: actual old successful reply, B RAM loss, adoption of that
  old committed bundle, B promotion, successor write/read and former-owner catch-up.
- `TakeoverWitnessAsync`: actual old successful reply, adoption of that bundle,
  lagging C promotion through the durable pair barrier, successor write/read and
  former-owner catch-up.
- `TakeoverWitnessGap`: successful old reply precedes B volatile-tail loss and
  permanent A storage loss; even with fence and promises complete, no source or
  continuity exists and selection remains blocked.
- `TakeoverWitnessIntactWait`: successful old reply precedes B volatile-tail loss;
  A's durable storage survives but its recovery source is unreachable, so a
  complete fence/quorum still cannot authorize stale selection.

The four concrete traces are retained in `TakeoverWitnessSync-wjmqxxp4`,
`TakeoverWitnessAsync-wbusi78b`, `TakeoverWitnessGap-0wfrebhn` and
`TakeoverWitnessIntactWait-4q8f1qis`. They establish reachability of useful and
deliberately unavailable schedules; their truncated searches are not exhaustive
positive safety passes.

An earlier liveness run found a missing model action: after B restarted empty,
the model could not reconcile even a fully available, closed, intact A history.
`RecoverFromOwner` now models the equivalent-complete-history recovery already
required by the protocol. The original temporal counterexample is preserved in
`TakeoverLiveness-ftosyblc`; it was model undercoverage, not a written-protocol
defect, and is not counted as a pass.

Tagged Mage compilation (`go test -p 1 -tags mage -run '^$' .`), tagged `go vet`
and `errcheck`, `mage -l`, `mage tlaTakeoverNegative` and
`mage tlaTakeoverCoverage` also passed. Their logs and explicit exit receipts
are in `.cache/tla/takeover-mage-checks-doi4y1cb/`. Positive verification above
consists of the completed individual profiles; it does not claim a separate
one-shot `mage tlaTakeover` invocation or rerun the normal replication profiles.

This bounded model does not prove the entire Takeover.md contract. In particular
it excludes multi-volume closure discovery/partial joins, transactions and bound
anchors, competing overlapping allocations, multiple active transfers,
highest-term conflicting values, arbitrary B/C link failures or partial pair
barriers, membership replacement, retaining A as owner while replacing its
partner, prospective-node faults after activation, all-node
reboots, storage snapshot rollback, broker/host crashes or lost guard continuity,
attestation authentication/wrong-target coverage, streaming holes/partial payloads,
request deduplication and retention/GC. Actual broker enrollment/launcher guard,
kernel/cache/FUSE and VM validation remain separate gates with per-run approval.

## Bounded cross-volume transactions

`Transactions.tla` implements task `5w3` as separate finite projections of
[Transactions.md](../../docs/spec/Transactions.md). Existing replication and
takeover sources/configurations are unchanged. Run only these profiles with:

```sh
mage tlaTransactions
mage tlaTransactionsNegative
mage tlaTransactionsCoverage
```

The runner checks sequentially with one CPU, one worker and a 512 MiB JVM heap.
Each transaction run executes a private copy of its source/configuration and
retains those copies, SHA-256 hashes (including the pinned checker), exact
argument vector, output, TLC metadata/counterexample and Java exit receipt below
`.cache/tla/Transactions*/`. Positive profiles require exit zero; mutations
require `Safety` violation and exit 12; reachability controls require
`WitnessAbsent` violation and exit 12. A timeout, parse error, unrelated invariant
failure or incomplete search cannot satisfy these controls. There are no state
constraints, depth cutoffs, symmetry reductions or simulation-only passes.
`CHECK_DEADLOCK FALSE` permits specified safely blocked states; it does not
suppress the temporal properties.

### Bounds and composition

The profiles share the contract boundaries below, rather than multiplying every
unrelated lifetime/retention/claim state by every protocol fault state:

| Projection | Explicit finite state | Contract boundary |
| --- | --- | --- |
| `Authority` | Four groups; initially disjoint endpoint closures `{3,4}` and `{1,2}`; immutable anchor 3; one anchored request number; two different exact canonical requests; compatible, split-owner, split-partner or split-membership placement; one anchor restart | A protected claim precedes resolution; complete per-member affinity manifests precede immutable binding; binding includes the anchor. The integrated suffix uses that binding's participant vector for prepare, decision, install, apply, cache, publication and result. |
| `Protocol` | Two participant groups, three replicas A/B/C; A/B normal pair; B/C recovery majority; one decision slot; terms 1 and 2; one B volatile-tail loss and one A storage loss, independently optional; six group/replica durable checkpoint bits | Starts with the protected claim/binding and compatible joined closure established. Prepares denote the exact bound participants' complete immutable graphs. A selected decision authorizes installation and the shared visibility generation. |
| `Namespace` | Two directory nodes plus fixed roots; opposing move requests, one home-owned regular file, two names, one handle, one data update, home-version cuts 0..4, six current durable coverage bits and snapshot/catch-up pins | Moves hold closure serialization and revision/permission validation through prepare/decision/apply. Link/unlink denote already selected atomic logical redo; original home references and handle pins govern payload reclamation. |

Groups have distinct positions and promises even though the common membership,
owner and partner are shared. `JoinMember`, prepare sync/receipt, per-group
promises, INSTALL and apply can complete in different orders. A partial join
cannot bind; a partial install/apply cannot publish. Per-member affinity records
are protected atomic records, not an implementation of distributed persistence.
Later closure growth conservatively adds members and changes the current minimum
UUID while preserving the original request's coordinator binding.

The integrated witness is part of `Authority`'s exhaustive safety graph. It
requires claim and immutable binding, **every** bound prepare, selected COMMIT,
every install/apply, cache barrier, publication, protected anchor result, source
removal, a merge that changes the current minimum UUID from 3 to 1, and a retry
*after that merge* returning the original binding/result without a second effect.
It checks these interfaces on one complete execution; the separate projections
are not a refinement/composition proof for all combinations of their state.

### Decision evidence, faults and observation

A prepare bit represents complete exact immutable redo/payload/authorization and
predecessor graph bytes for that group's distinct position. Owner sync and B
owned retention are separate actions. Decisions have one designated old writer
and one candidate per term. B accepts independently of A's sync. Old acceptance
can race each group's durable higher promise; the coordinator promise captures
its prior acceptance and complete graph before reporting success. Any B promise
also preserves its retained dependencies. A delayed pre-promise candidate after
the coordinator promise is rejected, as is its obsolete session after B restarts.

Fenced recovery consults the B/C majority for **both** groups. This two-term bound
contains at most one old accepted term, so highest-accepted selection reduces to
preserving that old value; it does not explore three competing accepted terms.
Complete captured B history, or the positively fenced A durable source, supports
adoption. A continuous empty B tail can support absence ABORT. Restarted empty B
has unknown continuity and cannot prove absence. Unavailable commit dependencies
block selection. Re-protection installs the selected complete cut durably on both
prospective nodes before recovery installation/publication; ordinary successful
records retain the owner-durable plus B-memory boundary.

`chosen` and prepare witnesses are external historical oracle state. They never
supply a source or decision to recovery. Loss of A storage and B memory may remove
all operational decision/dependency sources while the oracle remembers an old
COMMIT; the gap witness demands precisely this safely blocked state, not a
fabricated ABORT or a partially served namespace. Later valid replacement
checkpoints supply the original decision and dependencies after source pruning.
A result/reply lost on the wire is represented by stopping before the next action
and retrying the anchor; there is no timeout-driven cancellation/rollback.

Protected claim/bind/manifest/install/COMPLETE/result/checkpoint records are
indivisible complete-record operations at their specified boundary. This model
does not re-prove their per-frame transport/codec or local persistence. Their
referenced graph is acyclic by construction: prepares precede decisions,
acceptance precedes installs, installs/apply precede COMPLETE/publication, and
anchor results follow visibility. Arbitrary graph corruption, torn bytes, hashes,
reporter stream holes and a transport message queue are outside these bounds.
Lost messages are arbitrary stuttering; duplicate exact installs/apply are
idempotent set updates/stuttering. Independent group actions represent reordering.
These abstractions do not establish transport retry implementation correctness.

`Fence` assumes positive revocation of the entire old execution/cache domain and
inhibition of its relaunch. It is the takeover contract's external fence
assumption, not a lease, timeout or physical fence implementation. `CacheBarrier`
is a separately schedulable successful invalidation/drain barrier. It may remain
unavailable forever in safety; committed work then remains gated. There is no
claim that FUSE, FreeBSD, Linux, mmap or Rocky/9p supplies that barrier. Success
for real kernels/guest paths remains blocked by their independent capability and
approved runtime tests.

### Checked obligations and remaining limits

| Obligation | Checked state/actions | Limit |
| --- | --- | --- |
| Conflicting same IDs on disjoint endpoint closures | Both digests meet at anchor 3 before resolution; immutable `everClaims` oracle permits one protected claim; placement rejection has no effects | One principal/session/request number; canonical byte equality is represented by distinct symbols, not a hash implementation |
| Retry/source removal/coordinator change/floors | Integrated witness, immutable binding, unknown anchor-history restart gate, terminal-only retirement, compacted claims, closed tombstone and anchor retirement | One floor advance; durable registration/claim history itself is an atomic graph, not a checkpoint decoder |
| Affinity joins and partial fan-out | Per-member manifest subsets, complete join before bind, anchor-inclusive members, incompatible placement outcomes | Complete four-group membership is fixed; reconfiguration and verified splitting are separate protocols |
| Complete prepares and irreversible decisions | Separate owner/B prepare cuts, candidate/sync/ACCEPT, captured promises, selected pair, historical chosen values | Two coordinator terms; invalid/corrupt same-term candidates are excluded by exact single-writer input contract, not quarantined by an implemented parser |
| Partial apply/visibility/cache failure | Separate participant INSTALL/apply, one publication flag/generation, delayed cache barrier, result/reply gates | One transaction visibility advance, no unsynchronized multi-call snapshot guarantee |
| Directory cycles/authorization | Opposing moves serialized; ancestry test and revision/permission state locked through decision | Two directory nodes; fixed roots and one boolean permission version. Full Unix/sticky/view checks, replacement object IDs and symlink/path parsing remain simulator/runtime obligations |
| Object homes/link counts/handles/data aliases | Names reference the same single payload, exact link count, source-name removal, open handle, immutable group affinity and home-reclaim pins | One regular file and one write. No independent per-alias caches, byte ranges, mmap/lock implementation, replacement race or directory-payload reclamation proof |
| GC/checkpoints/snapshots/catch-up | Every group/replica replacement bit and snapshot release precede original graph GC; unresolved transactions cannot meet that barrier; namespace payload reclaim requires no names/handles/pins and complete coverage of the current home-version cut | Replacement bits mean verified compatible complete manifests, not independent snapshot timestamps. No external ZFS snapshot administration, volume split or received-sink modification |
| Accepted multi-failure gap | Independent A storage loss+B RAM loss, missing graph/continuity blocking, concrete gap witness | Explicit operator data-loss recovery/new generations are deliberately not automatic model actions |

Safety profiles include optional zero, one and combined declared faults; fault
budgets are maxima, not obligations. The namespace home flag describes the
regular-file payload, not the entire group or its directory objects. Reclaiming
that dead file cannot forbid later unrelated directory transactions. An initial
namespace oracle incorrectly required this and produced a counterexample; it
was corrected to check actual file lifetime obligations. That failed run remains
preserved as model-overconstraint evidence, not a protocol finding.

### Conditional progress

`Liveness` uses the same protocol transitions with faults disabled and retained
history. Once a candidate exists it must eventually receive a reply. Weak
fairness is assigned to concrete disk, partner, designated-decision, install/apply
and publication worker unions. Each successful action consumes finite pending
work; the unions cannot make infinitely many unrelated state changes to starve a
required phase. Fairness is not assigned to `chosen`, visible, reply predicates or
the eventual outcome. COMMIT and ABORT are both possible; neither candidate
admission nor COMMIT is required by fairness.

`RecoveryLiveness` includes both declared faults and adds a fair worker for the
finite per-group promises, selection and recovered-pair protection. It checks:
if complete history and positive fenced authority eventually remain continuously
available, this single transaction eventually has a successful terminal reply.
The accepted permanent multi-failure gap can violate that antecedent and remains
explicitly blocked in safety. The concrete recovery witness separately establishes
that successful COMMIT recovery is reachable; the property alone is conditional.

Both temporal projections pin complete history (`RetainHistory = TRUE`), omitting
checkpoint/GC actions while recovery is pending. Full checkpoint/GC interleavings
remain in `Protocol` safety. Progress additionally assumes ready storage/capacity,
working cache barriers, a ready pair and fair local workers. Those conditions are
represented by enabled workers rather than an error/latency implementation. No
finite completion latency, permanent-failure availability, automatic emergency
mode or real cache-fence progress is proved.

### Mutation and reachability controls

`NegativePrepare` permits COMMIT proposal/acceptance with missing prepares;
`NegativeFence` permits the delayed old acceptance after a durable coordinator
promise; `NegativeDedup` permits both canonical requests to be protected at one
anchor; `NegativeVisibility` opens visibility before install/apply/cache completion;
`NegativeRetention` deletes the original graph while dependencies or snapshot
obligations remain. Each removes an actual safety gate and must produce its own
real `Safety` counterexample. `NegativeHomeCoverage` separately removes the
namespace invalidation/catch-up update on a same-file mutation; its counterexample
reclaims a newer file state using an older checkpoint cut.

Coverage controls demand the integrated retry path, fully published recovered
COMMIT after owner storage loss, the declared unresolvable gap, a safely hidden
partial install/apply, and opposing directory moves with one cycle rejection plus
foreign alias write/source-name removal/open-home retention. Two additional
home-coverage controls require a stale checkpoint to lose reclamation authority
after a file mutation and complete current-cut coverage to permit reclamation.
They require
`WitnessAbsent` counterexamples while retaining `Safety` first, so an unrelated
safety failure cannot be mistaken for coverage.

### Recorded transaction verification

The initial fifteen-profile batch below completed locally on 29 October 2026
against module
SHA-256 `cfbd97b84e0a23fc88cfb58d2a914e08cd35bf1cb5ce440934c5daccca47c98e`,
with the pinned TLC 1.8.0 jar, one CPU/worker and 512 MiB heap. The complete
positive checks had zero queued states; expected negative/witness searches stop
at the demonstrated counterexample. Full positive Protocol took approximately
11 seconds; recovery coverage approximately 6 seconds. No profile timed out or
required a heap increase.

| Configuration suffix | Generated | Distinct | Depth | Java exit |
| --- | ---: | ---: | ---: | ---: |
| `Authority` | 3,720 | 1,430 | 30 | 0 |
| `Protocol` | 2,554,533 | 436,166 | 40 | 0 |
| `Namespace` | 136,744 | 27,363 | 19 | 0 |
| `Liveness` | 324 | 190 | 17 | 0 |
| `RecoveryLiveness` | 28,914 | 10,168 | 31 | 0 |
| `NegativePrepare` | 412 | 218 | 5 | 12 |
| `NegativeFence` | 2,992 | 1,122 | 9 | 12 |
| `NegativeDedup` | 33 | 33 | 4 | 12 |
| `NegativeVisibility` | 4,442 | 1,611 | 10 | 12 |
| `NegativeRetention` | 930 | 413 | 7 | 12 |
| `WitnessIntegrated` | 2,259 | 926 | 20 | 12 |
| `WitnessRecovery` | 1,395,674 | 254,324 | 23 | 12 |
| `WitnessGap` | 34,169 | 11,094 | 16 | 12 |
| `WitnessPartial` | 6,236 | 2,232 | 11 | 12 |
| `WitnessLifetime` | 40,346 | 11,463 | 10 | 12 |

The receipt index `.cache/tla/transactions-5w3-receipts.json` gives every exact
`Transactions*-verified-*` evidence directory. Positive directory suffixes are
`Authority-verified-p5__48hg`, `Protocol-verified-z2ws1rb8`,
`Namespace-verified-2louw4nj`, `Liveness-verified-bvqbqa2p` and
`RecoveryLiveness-verified-z39spenl`, all prefixed by `Transactions` below
`.cache/tla/`. Each contains the executed source/configuration, checker/source
hashes, exact argument vector, output and explicit exit status. Initial preflight
and superseded-source checks remain separately named; they are not substituted
for final-source receipts. The initial overconstrained namespace failure is
`TransactionsNamespace-final-o7ecfqvx` and remains available for inspection.

Final mutation traces specifically demonstrate COMMIT without prepares, delayed
old COMMIT accepted after a durable promise, two incompatible protected canonical
claims, COMMIT visible with missing INSTALL/apply/cache barriers, and removal of
unresolved COMMIT graph evidence while a snapshot pin remains. Earlier ABORT-first
mutation traces remain preserved, but the table uses these stronger COMMIT cases.
As with the other TLC checks, exhaustive results are conditional on finite
fingerprinting without distinct-state collision; they are bounded model evidence,
not a mathematical proof for arbitrary cluster/request bounds.

Tagged Mage compilation (`go test -tags mage -run '^$' .`), tagged `go vet`,
`errcheck`, Mage discovery and actual `tlaTransactionsNegative` /
`tlaTransactionsCoverage` target invocations also passed sequentially with
`GOMAXPROCS=2` / `GOFLAGS=-p=1`. Their outer logs/exit receipts are in
`.cache/tla/Transactions-mage-checks-5cct1svv`; those target invocations additionally
retain the runner's private snapshots, `argv.txt`, hashes and `exit.txt` for each
child. No normal/takeover model rerun, broad runtime Go test suite, lab, VM, SSH,
FUSE, snapshot or network operation was used for these checks. The positive
target was discovered/compiled; its profiles were run directly with the same
pinned TLC arguments and retained per-profile receipts above, rather than claimed
as a second full Mage invocation.

### Same-file coverage refinement

Root self-review then identified an additional model defect: namespace checkpoint
bits could survive Link/WriteAlias/Unlink and incorrectly authorize reclaim of a
newer file state. This was model undercoverage of the GC/home-lifetime contract,
not evidence that such stale checkpoints are safe. Every same-file mutation now
increments a bounded home version, clears current coverage and restores the
assigned-replica catch-up obligation. It preserves an already released snapshot
hold as released. Unrelated directory operations do not invalidate this file's
coverage or resurrect it. Once all six group/replica replacements cover the
current cut, `replacementVersion` records that cut; reclamation's independent
oracle requires it to equal `homeVersion`.

The following impacted profiles were run against final namespace module SHA-256
`c52858cddb7279ee0cfecbcc1b1d5274169c71cc90e3f08bde9de50dd6cf4433` with the
same 512 MiB/one CPU/worker bound. They supersede the earlier Namespace and
WitnessLifetime rows for this projection; the earlier executed source and
receipts remain preserved without relabeling.

| Configuration suffix | Generated | Distinct | Depth | Java exit |
| --- | ---: | ---: | ---: | ---: |
| `Namespace` | 543,043 | 115,500 | 26 | 0 |
| `WitnessLifetime` | 39,012 | 11,468 | 10 | 12 |
| `WitnessStaleCoverage` | 38,651 | 11,355 | 9 | 12 |
| `WitnessReclaim` | 466,633 | 106,024 | 20 | 12 |
| `NegativeHomeCoverage` | 56,110 | 15,288 | 10 | 12 |

`WitnessStaleCoverage` completes a checkpoint and releases its pins, then links
the file: snapshot stays released, while coverage becomes empty and catch-up is
required again. `WitnessReclaim` reaches a file that has been linked, written and
unlinked at both names, then reclaims it only after all six replacement bits
cover home version 4. `NegativeHomeCoverage` actually deletes the home with
home version 1 but replacement version 0. These controls make both blocked stale
coverage and successful current-cut reclamation nonvacuous.

Evidence directories are `TransactionsNamespace-home-fix-b8c2xsfv`,
`TransactionsWitnessLifetime-home-fix-bprayypm`,
`TransactionsWitnessStaleCoverage-home-fix-_gticqpe`,
`TransactionsWitnessReclaim-home-fix-irq81bnj` and
`TransactionsNegativeHomeCoverage-home-fix-_4vmyfu_`, below `.cache/tla/`.
The receipt index retains these plus the initial full-batch source hashes.
The Authority/Protocol prefix through `ProtocolWitness` is byte-for-byte unchanged
(SHA-256 `8bd7ad0fc487d3d6a0dc9b3ea1dee0224e100272075b75b9bafcc5556f4a354d`),
as is the common dispatch/fairness suffix beginning `Init == CASE Profile`
(SHA-256 `4a25d42d5c7a5a23bfe3e768d6e104bb84ceaadf6b033b41261b79b1253764ab`).
Their earlier positive/liveness receipts therefore apply to unchanged selected
transition systems; they were executed against the explicitly listed initial
module bytes, not falsely reported as runs of the final namespace source.

After adding the three controls to the public target mappings, tagged Mage
compilation/vet/errcheck/discovery and both actual negative/coverage targets were
checked again sequentially with `GOMAXPROCS=2` / `GOFLAGS=-p=1`; all passed.
Their outer receipts are in
`.cache/tla/Transactions-home-mage-checks-14vzipiz`. The targets now require six
negative counterexamples and seven distinct coverage witnesses. Each child run
also retains the runner's exact final module/config snapshots and exit receipt.

## Emergency activation and exit (`nx3`)

`Emergency.tla` checks the single cluster-wide state in
[Emergency.md](../../docs/spec/Emergency.md) and the singleton/frontier and exit
contracts in [Emergency-format.md](../../docs/spec/Emergency-format.md). It is
TLC-only evidence; it supplies no emergency implementation or infrastructure
permission. Run these targets sequentially:

```sh
mage tlaEmergency
mage tlaEmergencyNegative
mage tlaEmergencyCoverage
```

Every emergency invocation verifies the pinned TLC 1.8.0 jar above and retains
private module/configuration snapshots, hashes, `argv.txt`, `bounds.txt`, output,
checker metadata and an explicit `exit.txt` below `.cache/tla/`. Each profile has
one CPU/worker, 512 MiB heap and a three-minute wall limit. A killed, timed-out,
syntax-failing or incompletely explored positive graph is a failure. There is no
depth/state constraint or symmetry reduction hiding reachable boundary states.
The finite model bounds, rather than an exploration cutoff, bound its state.

### State, faults and assumptions

The integrated graph contains three enrolled nodes A/B/C, two members x/y of one
complete dependency closure, one initial normal generation/configuration/history
vector, one emergency activation and its explicit exit. Two independently
approved plans designate A or B and compete against the same exact predecessor.
Three invalid plans separately carry a different predecessor history, reused
generation/transition IDs, or mismatching approved canonical bytes. Identity and
digest symbols stand for exact bytes, not ordered UUIDs, timestamps or a hash
implementation. Exit retains the emergency generation and binds a distinct exit
transition, exact predecessor, complete configuration/history and final-cut vector.
There is no bound-independent proof of repeated generation allocation.

`BeginCAS`, external frontier publication and host CURRENT publication are
separately scheduled. A durable pending operation excludes a competing runner.
Registry crash can occur between any of them; a restored genesis CURRENT cannot
lower the independent enrolled frontier. Exact reconciliation can resume its
published candidate. Losing that independent continuity permanently blocks all
fresh registry/launch/publication authority in this graph; it is not repaired by
restarting a process, resetting an etcd revision or making another registry copy.
The model assumes one authenticated enrolled registry/lock and independently
retained frontier. It does not implement their storage isolation, signatures,
OS credentials, approval windows or byte codec. Rejection observations retain
only invalid-versus-conflicting categories; the exact per-plan comparisons are
still evaluated separately on every attempted CAS.

Old launches initially retain real read/cache rights, one normal writer/owner,
and normal rejoin eligibility. Intent alone does not atomically revoke them.
Each launch independently progresses through durable inhibition, positively
supervised stop, durable exact-launch attestation and controller receipt. All
three old domains, including the designated survivor's previous domain, must be
excluded. Historical inhibition/stop/acknowledgement oracles check that restart
and restored CURRENT never remove facts. Stop represents the successful
`native-vm-stop-v1` contract; failed/hung/unreachable stop is arbitrary stuttering,
not timeout-based exclusion. Its ACPI/broker/kernel implementation is unproved.

Fresh launches separately bind the winning intent, designated versus
recovery-only role, exact registry pointers/continuity, incarnation and every
prior launch's retained stop/inhibition evidence. Gates for reads, writes,
ownership and rejoin are actual per-node state. A named peer can copy/replay and
report recovery receipts while remaining barred from workload/normal roles.
Its initial grant binds the prospective incarnation. After a crash, durable
old-grant revocation and a separate manual fresh grant precede relaunch; changing
a handshake incarnation cannot inherit or edit the old grant. Grant creation
and revocation each abstract one successful authenticated durable registry CAS;
their internal record/pointer writes are not separately refined here.
The third node stays barred and its offline obligation stays pinned after exit.
There is no membership removal, fourth-node replacement or automatic failback.

`EmergencySafety` permits zero crashes. `EmergencyFaults` permits **at most one**
optional fault in total: registry crash (optionally restoring genesis CURRENT),
controller crash, crash of either freshly launched node, or permanent loss of
registry continuity. Each can occur at every enabled transition edge. A crashed
launch can receive a positive stopped-domain attestation and a fresh recovery
launch; each node's incarnation is bounded by old 0 and fresh 1/2. Durable graphs,
mode requirements and historical facts survive, while live installation/cache
readiness and receipts are invalidated. Controller receipt loss is separate from
retained host attestations. Multiple independent crash combinations, storage
loss and continuity reconstruction are outside these checked bounds. Crash or
maintenance after completed exit is outside this transition system.

The old graph contains a protected ordinary COMMIT, transaction COMMIT, exact
claim/binding/result, retirement/rejection mappings and foreign home. Selection
must preserve all of it; no missing-partner ABORT is invented. Symbolic complete
graphs stand for verified payload/redo/anchor/home dependencies; this model does
not re-prove transaction consensus, dependency discovery, directory operations,
loss selection/identity namespaces, checksums or arbitrary graph corruption.
One new emergency request progresses separately through protected claim, local
COMMIT sync, acknowledged durable result and optional terminal retirement. Its
historical chosen/acknowledged/retired oracles cannot be used as recovery sources.
Retries consult retained operational identity state before any new execution.

Each x/y copy, installation and per-node cache barrier is independently
scheduled. Activation needs the complete survivor vector. This bound schedules
peer catch-up at the frozen final cut, and survivor replay after restart; it
abstracts background streaming of intermediate cuts while writes continue.
The survivor's finite accepted work must finish before that cut is captured.
Both actual prospective nodes durably copy/install the complete cut, report
incarnation-bound receipts and independently install `NORMAL_REQUIRED` before
exit CAS. Coordination reconstruction is a separate required action; its one bit
means successful exact new-lineage validation, not trust in an old etcd quorum.

Immutable exit receipts survive a subsequent crash; live readiness does not.
A published durable exit certificate is not an assertion that both processes
remain running forever. Actual gate release revalidates the entire pair's fresh
readiness/receipts, normal requirement, coordination and registry continuity.
A crash after partial `NORMAL_REQUIRED` can leave this bound permanently gated;
there is no automatic local-only fallback. The model checks safety and concrete
reachability, with no fairness, eventual-exit or finite-latency claim. Capacity,
archive verification, arbitrary GC/checkpoint replacement, normal operations
after exit, FUSE/cache capabilities and runtime fence progress remain separate
implementation/model/runtime obligations.

### Invariants, mutations and witnesses

`RegistrySafety` checks one authorized emergency intent, exact predecessor and
fresh identities/approved bytes, role/designation/registry binding at each fresh
launch, and permanent prior-launch exclusion. `DurableFacts` checks independent
historical inhibition/stop/attestation facts. `Preservation` checks selected old
COMMIT/claim/result/retirement/home evidence, new protected/acknowledged outcomes,
terminal retirement and absence of duplicate retry execution. `AdmissionSafety`
checks every node's read/write/ownership/rejoin gates, no normal role before exit,
and no regression from `NORMAL_REQUIRED`. `ExitSafety` checks the retained
normal successor lineage, complete pair cut, durable graph, mode vector,
coordination and exact immutable exit receipts.

| Negative configuration suffix | Removed gate and required real `Safety` violation |
| --- | --- |
| `Registry` | A restored old host pointer allocates a second intent despite the current independent frontier |
| `Lineage` / `Fresh` / `Approval` | CAS accepts the wrong predecessor history, reused generation/transition, or unapproved canonical plan bytes |
| `Launch` | A fresh launch accepts stale/mismatching registry continuity/pointers |
| `Facts` | Restoring old control erases durable inhibition/stop/acknowledgement facts |
| `Fence` | Emergency activation asserts readiness without complete old-domain exclusion |
| `Commit` / `Claim` / `Retirement` | Selection drops protected decision, exact identity/result, or retirement/rejection graph evidence |
| `Peer` | A recovery-only launch escalates to read/write/ownership/rejoin privileges |
| `Regrant` | A restarted peer launches using the previous incarnation's grant |
| `Exit` | Exit CAS proceeds without its complete pair/normal-mode/coordination barrier |
| `Failback` | An excluded node reacquires workload/ownership/rejoin rights before explicit exit |

Each witness keeps `Safety` first and must fail only `WitnessAbsent` with Java
exit 12. `Exit` reaches acknowledged and retired emergency work, an old-ID retry,
completed exit and both normal read gates. `Restore` requires acknowledged work
**before** old-CURRENT restore and then a complete exit preserving that work.
`Restart` requires acknowledged work **before** survivor crash, a positively
excluded previous launch, fresh incarnation 2, replay and emergency publication.
`PeerRestart` reaches an explicitly revoked old recovery grant, a separately
authorized new grant and fresh peer incarnation 2 with every workload/rejoin
permission still closed. `Partial` reaches one durable `NORMAL_REQUIRED` installation with all workload
gates closed. `Conflict` reaches active service with rejected incompatible and
invalid plans. `Ambiguous` reaches external publication followed by registry
crash before CURRENT or any fresh launch. `Continuity` reaches acknowledged work
followed by lost registry continuity, retaining the old outcome and no fresh
exit authority. These are reachability searches, not exhaustive positive checks
or additional genuine protocol findings.

### Recorded emergency verification

Exploratory receipts preserve initial parse/type errors, two 90-second wall-capped
positive searches and a failed fault invariant that incorrectly equated a
durable exit certificate with processes remaining alive. None is a positive
pass or a protocol finding. The corrected model separates immutable receipts
from fresh publication readiness. Self-review also found that peer relaunch had
folded fresh grant authorization into launch; the final graph requires separate
revocation/regrant and has independent negative/reachability controls. These are
model corrections to existing specification requirements, not new protocol
requirements. No genuine protocol defect was found by these bounded checks.

All 24 final profiles completed locally on 30 October 2026 against module
SHA-256 `ebfbbef8c7c188c2b4f511a35fb2721cee6296cac66838411a0b15a63b4819d5`.
Every private module/configuration snapshot was checked against the final source;
no model/configuration edit occurred during this final batch. The actual public
`mage tlaEmergencyNegative`, `mage tlaEmergencyCoverage` and `mage tlaEmergency`
invocations all exited zero, taking 25.18, 143.26 and 113.49 seconds respectively.
Each Java child used the pinned jar, one CPU/worker and 512 MiB heap. The complete
fault-positive child took approximately 108 seconds, within its three-minute cap.
Both positives exhausted their finite graph with zero queued states and Java
exit zero; negative/witness searches stop at their required real counterexample.

| Configuration suffix | Generated | Distinct | Depth | Java exit |
| --- | ---: | ---: | ---: | ---: |
| `Safety` | 75,962 | 11,318 | 56 | 0 |
| `Faults` | 3,528,138 | 461,540 | 63 | 0 |
| `NegativeRegistry` | 817 | 262 | 7 | 12 |
| `NegativeLineage` | 21 | 11 | 3 | 12 |
| `NegativeFresh` | 26 | 11 | 3 | 12 |
| `NegativeApproval` | 26 | 11 | 3 | 12 |
| `NegativeLaunch` | 46,230 | 7,595 | 19 | 12 |
| `NegativeFacts` | 667 | 234 | 7 | 12 |
| `NegativeFence` | 134 | 47 | 6 | 12 |
| `NegativeCommit` | 5,892 | 1,008 | 17 | 12 |
| `NegativeClaim` | 5,892 | 1,008 | 17 | 12 |
| `NegativeRetirement` | 5,892 | 1,008 | 17 | 12 |
| `NegativePeer` | 6,533 | 1,147 | 27 | 12 |
| `NegativeRegrant` | 68,270 | 12,052 | 31 | 12 |
| `NegativeExit` | 8,191 | 1,521 | 31 | 12 |
| `NegativeFailback` | 6,388 | 1,108 | 25 | 12 |
| `WitnessExit` | 75,699 | 11,289 | 54 | 12 |
| `WitnessRestore` | 3,463,714 | 452,667 | 52 | 12 |
| `WitnessRestart` | 517,384 | 90,976 | 39 | 12 |
| `WitnessPeerRestart` | 91,012 | 16,659 | 33 | 12 |
| `WitnessPartial` | 21,600 | 3,901 | 37 | 12 |
| `WitnessConflict` | 6,469 | 1,131 | 27 | 12 |
| `WitnessAmbiguous` | 50 | 29 | 4 | 12 |
| `WitnessContinuity` | 58,400 | 9,979 | 29 | 12 |

The receipt index `.cache/tla/emergency-nx3-final-receipts.json` records all 24
exact private evidence directories, source/configuration hashes, counts and exit
codes. Outer command logs/exit receipts are in
`.cache/tla/Emergency-mage-final-fsk0m7j9`; each listed child also contains the
executed source/configuration, jar/source hashes, exact argument vector, resource
bounds, output and explicit exit receipt. Exploratory source/exit history is
indexed separately in `.cache/tla/emergency-nx3-development-history.json`.
Earlier development and the superseded public batch are preserved, not relabeled
as final-source verification. As with the other models, exhaustive exploration
uses finite fingerprints: the final fault graph reported collision-probability
estimates `7.7E-8` calculated and `2.0E-8` based on actual fingerprints. This is
conditional bounded checking, not a collision-free mathematical proof.

Tagged Mage compilation (`go test -tags mage -run '^$' .`), tagged `go vet`,
`errcheck`, Mage discovery and `git diff --check` passed. Go/Mage commands ran
sequentially with `GOMAXPROCS=2`, `GOFLAGS=-p=1` and fresh private `/tmp` directories
for each command. No broad runtime Go suite, unrelated TLC model, lab, VM, SSH,
FUSE, power, snapshot, production-data or infrastructure operation was run.
