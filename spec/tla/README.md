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
