# All-origin two-volume transaction composition (`hz3`)

Completed local TLC checks on 31 October 2026: eight complete finite safety
checks (one also checks weak-fair temporal completion), seven expected policy
counterexamples and eleven reachability witnesses. The protocol and runtime
integration remain unimplemented; the proof bounds and component assumptions
below are part of every result.

## Finite inputs and semantic correspondence

[OriginTransactions.tla](OriginTransactions.tla) composes one transaction T and
one ordinary request U in a fixed two-volume closure. Both race for A's single
mutating reservation; no successor grant can pass a pending request/result.
The immutable anchor is one of these volumes, the bound coordinator/home is the
other. S is A/B/C/W and replica placement is A/B/C; W is an admitted outside
origin. T originates at W (or A in the owner-origin profile); U originates at B.
Remote records require O durable+A RAM, owner-origin records A durable+B RAM.
A recovered sequencer is C; still-admitted original origins remain eligible.

Each semantic record has separate stage, prepared-receipt emission/delivery,
origin durable sync, and protected-tail emission/delivery steps. EmitAcceptance
and Protect use an atomic verified normal-component contract: they fold remaining
choice acceptance/selection, O's stable normal-result/certificate sync, M's result
retention/RESULT_ACCEPT and delivery. The tail receipt is emitted only with the
existing live-M guard; delivery may follow unobserved RAM loss. Normal result
protection is part of that component contract, not omitted until a later anchor
index. Fine-grained crashes/messages inside this folded tail are **not** separately
explored or refinement-proved. Mapping every incoming normal implementation phase
to this component remains a downstream acceptance obligation. These are the normal
**enclosing transport/protection phases**, not transaction PREPARED or an extra
2PC wrapper. The semantic graph contains claim, immutable binding, BEGIN, two
participant prepares, one COMMIT/ABORT decision, two installs, view barrier,
COMPLETE and immutable anchor result. A protected decision requires exact
origin-durable choice and distinct memory acceptance. Every bound prepare
precedes COMMIT; explicit cancellation can choose ABORT only before the decision
candidate is fixed. A timeout never selects ABORT or releases the reservation.
Record bytes/hashes, full contiguous source-prefix checks, original numeric
credentials, policy/revision validation and immutable stable IDs are abstract
exact facts at these transitions, rather than modeled byte encoders or files.

The conflict scenarios are Rename versus chmod, Link versus a write through the
original name, and Rename versus an ordinary closure barrier. Both grant orders
are explored. Planning freezes the logical view once after protected claim lookup.
A preceding chmod denies T; a preceding Link preserves one home, shared payload
and correct two-name link count. T's two participant installs cannot publish
separately. An independent oracle checks namespace/data/permission/link-count
observations against selected published operations. Backing apply is independent
and may remain at genesis while committed observations and new planning advance.

Same-ID retries enter from a different origin, compare the original symbolic
digest, and return result/pending/indeterminate/conflict before any new path
resolution. The original durable origin and effect count remain fixed. Internal
terminal completion, reply emission, one possible dropped reply per request and
external delivery are distinct; a dropped reply can still yield its same result.
The model assumes registered, unretired sessions and fixed compatible affinity;
it does not explore session retirement, alternative anchors or closure joins.

Two reader-domain acknowledgements abstract a positive cache barrier. They do
not model dirty mappings, frontend drain, cache implementation or kernel behavior;
those are separately scoped by [View barriers](../../docs/spec/View-barriers.md).
The home, live handle and all source obligations stay pinned throughout. This
checks their preservation by the new ordering/transaction transitions, **not** a
GC, handle-release, lifetime-reclamation or arbitrary directory-ancestry proof.

## Fault and transition bounds

The crash profiles allow one designated memory-holder loss during any record's
prepared/durable/acceptance phases. Emitted receipts can arrive after unobserved
loss; known loss gates new completion. Owner-origin late acceptance may precede
an externally delivered reply despite the earlier B RAM loss. This records the
accepted loss boundary, never a proof of current remote RAM at reply.
After loss, pre-transition candidate-transfer work is conservatively gated even
if a later request would use a different healthy memory recipient; eligible
remote continuation after B failure is not established here. Origin disks survive
in these profiles; simultaneous unique-origin disk loss,
torn writes, equivalent-source replacement and automatic loss recovery are absent.

One actual incoming pipeline-to-recovery transition is explored, rather than
only a seeded completed-transaction state. An origin may still sync an already
issued grant before its own sealed report. All four reports close that source's
old choice rights; all four origins plus one aggregate cache domain require
positive exclusion. After the complete closed survey, the full durable graph is
materialized at new C/B before activation. Durable decisions with unknown
selection are adopted with their exact dependencies/result; no new paths are
resolved. Complete-report bytes/authentication/issued-frontier certification and
both durable materializations are atomic verified facts here. The model does not
prove their implementation, multi-term authority allocation or arbitrary replay.
Missing W's report blocks activation even with all placement reports/exclusions.

Cooperative handoff is a separate profile after both requests are internally
terminal. It requires the complete survey, positive old-domain closure and full
RAM/source/claim/handle/pin transfer. New-owner disk may stay at genesis. In-flight
handoff and repeated authority changes are not explored. Both volume members
always move together; no closure split is part of this finite model.

## Verification and use

Run `python spec/tla/check_origin_transactions.py --group all --seconds 600`
only within the
applicable test authorization and resource scheduling. The runner pins TLC 1.8.0,
uses one worker/active processor and 1 GiB heap, snapshots exact model/config and
runner bytes, and records commands, hashes, actual process exits, wall time and
limit reasons. Positive checks require TLC completion, exit zero and zero queued
states; no depth/state cutoffs turn an incomplete run into a pass. Limits are
2 GiB/profile evidence and 4 GiB across this model's retained evidence sets.
WitnessAbsent failures establish only reachable executions; deliberately broken
policy failures exercise the named safety checks, not a positive proof.

Only the fault-free Progress profile requests weakly fair record workers,
reservation, resolution, reader barriers, publication and internal completion.
Its property is eventual internal terminal result for both requests with stable
authority, functioning barriers, retained sources/capacity and fair delivery.
The safety/crash/blocked/handoff profiles claim no unconditional progress,
external reply-delivery guarantee or recovery-time bound. In particular an
offline origin without verified equivalent evidence can block the full closure.

Existing v1 models/goldens and the previous AllOrigin transaction projection
remain unchanged and retain their narrower scopes. This model does not implement
a distributed filesystem or prove runtime refinement, successor codecs, source
retention/replacement, authenticated transport, FUSE/9p coherence or lab behavior.
Downstream `8w3` must add the typed transaction graph and anchored API/shared-home
integration; `8r3` owns general retention/checkpoint/resync and `or3` snapshots.

## Final-source completed checks

Module SHA-256: `9e849f218ccb7c3dc972d90e76ffc4dfc53241e77c6456f4aa11f14f3cc3ae74`.
Runner SHA-256: `be04aa842a2103f1fe2c16581886f9728ab1ad1831dc961e4f12a839b1828d5b`.
TLC jar SHA-256: `fc6878c603f0fdc18b9fe3e5bf51860cb90ab5b1154e3c371660c036d6bdd63e`.

Evidence sets: `.cache/tla/OriginTransactions-5f3ng7x5`, `.cache/tla/OriginTransactions-wbq5y7r3`, `.cache/tla/OriginTransactions-jda5umqb`, `.cache/tla/OriginTransactions-1f0pwdrp`, `.cache/tla/OriginTransactions-rk7hxcm8`.
Each retains exact source/config/runner bytes, command, logs and process receipts.
Completed rows had no state/depth cutoff or resource-limit termination. Positive
rows exhausted the finite graph with zero queued states; expected counterexamples
leave queued states and are not exhaustive positive checks. Fingerprint collision
remains TLC's probabilistic limitation; estimates are retained in each log.

| Profile | Exit | Generated | Distinct | Queued | Depth | Seconds | Wall cap |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Chmod | 0 | 118,338 | 21,948 | 0 | 109 | 2.568 | 60 |
| NegativeSync | 12 | 76 | 32 | 11 | 7 | 0.816 | 60 |
| WitnessUnknown | 12 | 6,136 | 1,338 | 167 | 37 | 0.717 | 60 |
| Alias | 0 | 270,015 | 49,722 | 0 | 109 | 4.039 | 60 |
| Barrier | 0 | 166,326 | 32,170 | 0 | 108 | 3.419 | 60 |
| Owner | 0 | 118,338 | 21,948 | 0 | 109 | 2.217 | 60 |
| Progress | 0 | 118,338 | 21,948 | 0 | 109 | 5.123 | 60 |
| NegativePrepare | 12 | 935 | 249 | 22 | 27 | 0.816 | 60 |
| NegativeVisibility | 12 | 158 | 49 | 8 | 10 | 0.816 | 60 |
| NegativeResult | 12 | 68 | 41 | 24 | 5 | 0.816 | 60 |
| NegativeSelf | 12 | 328 | 88 | 6 | 15 | 0.866 | 60 |
| WitnessPartial | 12 | 3,501 | 858 | 153 | 34 | 0.666 | 60 |
| WitnessLostreply | 12 | 25,781 | 5,336 | 187 | 76 | 1.57 | 60 |
| WitnessPermission | 12 | 8,895 | 1,866 | 167 | 40 | 1.269 | 60 |
| WitnessAlias | 12 | 211,735 | 40,011 | 1,417 | 97 | 3.68 | 60 |
| WitnessOrdered | 12 | 87,332 | 16,915 | 841 | 97 | 2.473 | 60 |
| WitnessRetry | 12 | 24,201 | 4,981 | 158 | 74 | 1.221 | 60 |
| Blocked | 0 | 15,335,835 | 1,783,859 | 0 | 109 | 119.381 | 180 |
| NegativeFence | 12 | 78,568 | 10,435 | 3,758 | 13 | 1.419 | 300 |
| NegativeSurvey | 12 | 100,893 | 13,463 | 1,472 | 17 | 1.52 | 300 |
| WitnessLate | 12 | 7,050,836 | 758,814 | 124,864 | 43 | 42.937 | 300 |
| WitnessBlocked | 12 | 3,338,105 | 404,695 | 37,168 | 47 | 22.168 | 300 |
| WitnessRecovery | 12 | 22,705,770 | 2,318,915 | 116,765 | 87 | 154.673 | 300 |
| WitnessHandoff | 12 | 32,599,536 | 3,178,282 | 247,930 | 109 | 274.527 | 300 |
| Crash | 0 | 44,745,440 | 4,541,309 | 0 | 122 | 335.143 | 600 |
| Handoff | 0 | 43,741,542 | 4,044,048 | 0 | 121 | 371.382 | 600 |

Negative profiles fail Safety as intended; witness profiles fail WitnessAbsent
as intended. Progress completed its weak-fair internal Completion temporal check.
The inspected decisive states are:

- Unknown: phase 4 has W's durable claim/bind/BEGIN, both prepares and COMMIT;
  protected/accepted records stop at the prepares. Old namespace remains visible,
  with zero effects/replies. Origin durability is distinct from candidate receipt
  and selection.
- NegativePrepare: COMMIT becomes protected with no participant prepares.
  NegativeVisibility: T publishes just after its claim, without decision/installs.
  NegativeResult: a terminal unknown result is delivered without protected result
  evidence. NegativeSelf: U's effect counts U's own memory as the second host.
- Lostreply: T is committed/published and internally done; its emitted reply was
  actually dropped and remains unobserved. A same-ID retry returns result with
  one effect and one original path resolution.
- Permission: U's chmod precedes T; T's protected rejection has zero effects.
  Alias: T's Link precedes U's write, preserving one home/two names and shared
  data. Ordered: T's Rename precedes U's chmod. Both become internally terminal.
- NegativeFence activates with all reports but no old-domain exclusion;
  NegativeSurvey activates with only placement A/B/C reports despite all old
  domains excluded, omitting W's report/frontier coverage.
- Late's first trace is **DENIED**, after U's chmod. It proves delivery of an
  owner-origin protected result tail/reply after B RAM loss, with T effect zero;
  it is not a transaction-COMMIT late-receipt witness.
- Blocked's witness has an interrupted durable-unselected COMMIT, both prepares
  protected, all domains excluded but W's report missing; recovery stays closed
  with no visible effects. Recovery's witness adopts that unknown COMMIT after
  all four reports/five exclusions and B/C durable materialization, then protects
  every install/view/complete/result and replies with one T effect at original W.
- Handoff transfers after committed Rename then chmod: both requests are done,
  new logical view has dst and revoked permission, while new backing remains src
  with old permission. All source reports/exclusions, both-volume affinity and
  source pins remain present; no disk catch-up is inferred from RAM readiness.
- Partial's first trace is ABORT cleanup with only i1 installed. The actual
  **COMMIT** partial-install case appears in Ordered state 45: p1/p2/decision/i1
  protected, i2 absent, no publication, old logical view and old backing.
  Ordered later publishes the complete transaction, showing the phase is on a
  real successful path rather than a permanently unreachable candidate.

## Retained incomplete attempts

Crash cost probe `.cache/tla/OriginTransactions-ab0csumf` reached its 60-second
wall deadline (60.258 seconds, actual child exit 143, runner exit 1). It is
**incomplete, not a pass**; Blocked and Handoff were not started because the
runner stopped at the first failure. The last emitted progress was 413,535
states generated, 45,119 distinct and 4,182 queued at depth 24, three seconds
into the run. No final counts were emitted at termination, so the receipt does
not invent them. Retained evidence is 267,842,873 bytes. There was no reported
safety counterexample before the timeout. The later `.cache/tla/OriginTransactions-jda5umqb` batch completed Blocked as
recorded above, then Handoff reached its 180-second wall deadline (180.710
seconds, actual child exit 143, runner exit 1). Handoff is also **incomplete,
not a pass**. Its last progress at 123 seconds was 16,356,817 generated,
1,632,197 distinct and 165,062 queued at depth 102; no final counts were emitted.
That evidence set is 217,109,396 bytes after termination. The six fault negatives/witnesses were not started in that stopped batch;
their later separately allocated completed checks are recorded above. The later full Crash and Handoff checks completed within their separately
allocated 600-second caps, as recorded above. These failed attempts remain
incomplete history and are excluded from the twenty-six completed final rows.

## Preliminary retained checks

Evidence `.cache/tla/OriginTransactions-025hfkx9` used module
`5e74b6665ca12feb24a4e03da9949e576470a927f24477348d859bdf17cac7ba`
and runner `be04aa842a2103f1fe2c16581886f9728ab1ad1831dc961e4f12a839b1828d5b`.
At a 60-second per-profile wall cap, Chmod exhausted 21,948 distinct states
(118,338 generated, depth 109, exit zero, 2.367 seconds); NegativeSync failed
Safety as intended (exit 12, 32 distinct); WitnessUnknown reached its old
predicate (exit 12, 216 distinct). Inspection showed that witness selected a
durable **ABORT**, so it is not unknown-COMMIT evidence. The current source
requires COMMIT for that witness and latches durable-unselected COMMIT at the
crash for the recovery/blocked witnesses. These earlier receipts remain history.
