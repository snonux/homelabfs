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
  model and the separate policy/physical-fence validation remain future work.
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
