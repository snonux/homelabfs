# Preferred-owner policy and handoff (`kz3`)

The controller and distributed handoff remain **unimplemented**. These are
separate finite component checks of [the specification](../../docs/spec/Preferred-owner.md),
not a normal-publication gate, runtime/kernel proof or new fence/trust decision.
Existing AllOrigin, ViewBarrier and OriginTransactions source/configuration and
results are unchanged and retain their own proof bounds.

## Model correspondence and finite limits

`PreferredOwner.tla` separates three components; it does not claim a
machine-checked refinement/composition between their input/output interfaces.

- Counting: three original immutable request identities, one from each A/C/W;
  W's request touches both volumes. Each has one original, forward, retry and
  replication event, with arbitrary enabled interleavings. Per-volume counters
  charge W twice across two volumes; the closure union charges it once. The
  original host function is immutable across forwarding/retry. Authentication,
  restart-safe domain serial allocation, permanent anchored binding, expiration,
  bounded identity storage and late transaction binding are specification/runtime
  obligations, not modeled encoders or deduplication GC.
- Policy: six aligned one-minute rounds, four reports per round and one attempted
  older report replay. Current reports may arrive in any order, or be omitted;
  evaluation can occur with missing/stale summaries. Authentic complete input
  summaries have exact symbolic counts from {0,64,128,256,512}. The minimum sample,
  32-request/25% advantage, three confirming rounds, 15-minute tenure and 10-minute
  cooldown use the specification's numerical predicates. Mature-owner profiles
  start with tenure 15; Young starts at zero and Cooldown with ten minutes left
  after a prior attempt. Stable C demand, ties, alternating B/C demand, hottest
  outside W and C capacity-ineligible inputs are separate profiles. Every
  evaluation is consumed once. Aligned complete summary validation abstracts the
  90-second deadline and rolling five-minute exact bucket union; clock skew,
  overlapped collection rounds, report bytes, reporter restarts and metric storage
  capacity are not separately explored. No SLO/calibration is inferred.
- Handoff: S=A/B/C/W, R=A/B/C, two fixed affinity volumes, two symbolic positive
  frontend barrier domains, one A-to-C transition and five ordered operations.
  Operation 1 is owner-origin A with distinct B; operation 2 is outside W with an
  old issued grant. Begin can occur before its durable choice. Origins seal
  individually, and W may sync its already-issued COMMIT before W's own close,
  including after another node has sealed. Once W seals it cannot choose more.
  The complete reports/frontier and old domain closure precede RAM transfer and
  exact successor installation. New authority may find operation 2 durably
  committed but unselected/result-unknown, resolve it from original W evidence,
  protect its result and publish before opening new grants. If W sealed before
  choosing, complete closed absence resolves NO_EFFECT. No timer creates absence.
  Operation 3 is owner-origin C with distinct B; operation 4 originates at former
  owner A using C memory; operation 5 originates at outside W using C memory.
  All four origins can then observe the complete selected projection. Backing
  apply is independent and may stay at genesis. One optional transfer-continuity
  interruption blocks activation; crash-recovery fallback is specified, not
  modeled as successful recovery here. No VM stop is a handoff action.

Stage, local durable sync, selection, result protection, publication and reply
are separate steps. Memory acceptance/selection and result protection are atomic
verified normal-component interfaces, rather than the complete normal wire
handshake; no late receipt/RAM-loss exploration is added to the earlier models.
`ram` represents sequencer logical projections, while `mem` is the normal
component fact that the designated distinct holder owns the protected graph;
it is not a complete physical-memory inventory. The logical projection uses
selected operation identities, not file bytes or namespace execution; full data/metadata correctness belongs to the logical-view
and normal models. Immutable source identities, stable claims/results, transaction
prepares/decisions/homes, handles and pins are represented by exact opaque
obligation keys retained as a unit. Their contents, all-S report authenticity,
nondecreasing promises, closed frontier certification, owner/memory capacity and
physical cache stop/drain/install are component facts, not proofs of an actual
journal or kernel implementation. The two-volume owner vector checks indivisible
closure movement; closure discovery/joins/splits, retention release, member/R/B
changes, repeated terms, etcd allocation races and unavailable-source replacement
are outside this graph. Installed S/R remain fixed. After a blocked transfer no
liveness or time-to-recovery guarantee is claimed.

Safety profiles exhaust their configured graph without a state/depth cutoff;
witness counterexamples establish only one reachable execution. No fairness or
unconditional liveness property is claimed. Safety includes retained original
origin roles, distinct memory hosts before/after transfer, all-S/source/frontier
and whole-closure preservation, positive old/new barriers, complete new RAM and
result-before-visible ordering. Negative profiles deliberately break counting,
freshness/replay, stability/tenure/cooldown/eligibility, projection/frontier/result,
unknown durable choices, full survey, cache exclusion, closure affinity, distinct
memory or transfer continuity. They must fail the named Safety invariant.

## Runner and evidence discipline

Run `python spec/tla/check_preferred_owner.py --group all --seconds 60` only with
applicable local-test authorization and orchestration allocation. The runner
uses the pinned TLC 1.8.0 jar, one worker/active processor and 1 GiB heap. It
retains exact source/configuration/runner snapshots, hashes, command, actual
child exit, elapsed time and graph counts. Positive success requires TLC exit
zero, complete exploration and zero queued states. Expected negatives/witnesses
require actual exit 12 and their named invariant violation. Wall deadline or
resource termination fails, preserves partial evidence, and stops the batch.
Limits are 2 GiB/profile and 4 GiB aggregate across PreferredOwner evidence sets.
There are no state constraints, state/depth cutoffs or symmetry reductions.
TLC's probabilistic fingerprint collision limitation still applies.

## Development receipts

The first allocated five-profile attempt `.cache/tla/PreferredOwner-i8ft46ei`
used module `b270fc7e375034d36f2bab551532f4f08069dd950ecb0f5400bb6d641bdbf199`
and runner `64d47d71fa716f9a4aeab638c52e24f9501e312018037092c2328d1f9a310b06`.
Counting exhausted 1,000 distinct states (5,701 generated, depth 13, 0.968 seconds,
exit zero); Policy exhausted 22,282 (63,562 generated, depth 37, 1.517 seconds,
exit zero). Handoff then exited 75 after two states: TLC eagerly evaluated
`step[i-1]` for i=1 in a disjunct. Replacing that predecessor guard by an explicit
IF fixes the out-of-domain representation. This is a failed development attempt;
Freshness and Service were not run in that stopped batch. The separately allocated corrected checks below use the fixed source.

The corrected predecessor source
`80a36a52684299025342ff24e7ca11ee618d1812ce05fa81d2347024584d4ee8`
completed all 32 then-configured expectations in
`.cache/tla/PreferredOwner-6azwzvva` (five) and
`.cache/tla/PreferredOwner-ws3li_ky` (27). Ten safety graphs exhausted with exit
zero; sixteen mutations and six witnesses exited 12 as expected. Inspection
found the Oscillate witness's first trace used stale/missing reports; it did not
establish six fresh alternating rounds. Those exact receipts remain pre-final
history. The final source adds a ghost set of distinct complete aligned round
identities only for that witness and requires exactly rounds 0 through 5. It
changes no protocol guard or bound; other profiles keep that field constant and
have the same projected transition graph.

## Final-source completed checks

Module SHA-256: `77222db2bffc19fa544abe6e837e0676ccaddf48ecb1af13e8d372a322e53010`.
Runner SHA-256: `64d47d71fa716f9a4aeab638c52e24f9501e312018037092c2328d1f9a310b06`.
TLC jar SHA-256: `fc6878c603f0fdc18b9fe3e5bf51860cb90ab5b1154e3c371660c036d6bdd63e`.

Final evidence `.cache/tla/PreferredOwner-pr15ckc8` contains all 32 checks. Every
exact executed module/configuration snapshot and runner was byte-compared with
the final files. All profiles had a 60-second wall cap, one worker/processor and
1 GiB heap; no limit was reached. Summed measured elapsed time was 51.561 seconds.
The ten safety graphs completed with actual exit zero and zero queued states.
Sixteen mutations failed Safety and six witnesses failed WitnessAbsent with
actual exit 12. Counterexample rows leave queued states and are not exhaustive
positive safety checks. TLC fingerprint collision estimates remain in each log.

| Profile | Exit | Generated | Distinct | Queued | Depth | Seconds |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Capacity | 0 | 51365 | 18676 | 0 | 37 | 1.319 |
| Cooldown | 0 | 59218 | 20556 | 0 | 37 | 1.421 |
| Counting | 0 | 5701 | 1000 | 0 | 13 | 0.867 |
| Handoff | 0 | 1065349 | 150714 | 0 | 64 | 8.047 |
| Interrupted | 0 | 1065637 | 150906 | 0 | 64 | 8.242 |
| NegativeClosure | 12 | 3447 | 989 | 41 | 22 | 0.867 |
| NegativeContinuity | 12 | 3770 | 1177 | 67 | 23 | 0.867 |
| NegativeCooldown | 12 | 57164 | 20245 | 1539 | 18 | 1.47 |
| NegativeCount | 12 | 10 | 10 | 7 | 3 | 0.716 |
| NegativeEligibility | 12 | 59263 | 20785 | 1575 | 18 | 1.573 |
| NegativeFence | 12 | 4472 | 1302 | 183 | 20 | 0.867 |
| NegativeFreshness | 12 | 505 | 302 | 161 | 7 | 0.816 |
| NegativeFrontier | 12 | 2795 | 780 | 93 | 17 | 0.515 |
| NegativeProjection | 12 | 2795 | 780 | 93 | 17 | 0.817 |
| NegativeReplay | 12 | 88 | 68 | 35 | 5 | 0.716 |
| NegativeResult | 12 | 2795 | 780 | 93 | 17 | 0.867 |
| NegativeSelf | 12 | 3874 | 1190 | 28 | 30 | 0.866 |
| NegativeStability | 12 | 106 | 80 | 42 | 6 | 0.515 |
| NegativeSurvey | 12 | 4051 | 1174 | 126 | 21 | 0.868 |
| NegativeTenure | 12 | 57164 | 20245 | 1539 | 18 | 1.47 |
| NegativeUnknown | 12 | 3600 | 1051 | 19 | 25 | 0.868 |
| Oscillate | 0 | 95695 | 32304 | 0 | 37 | 1.47 |
| Outside | 0 | 63562 | 22282 | 0 | 37 | 1.52 |
| Policy | 0 | 63562 | 22282 | 0 | 37 | 1.571 |
| Tie | 0 | 57926 | 20224 | 0 | 37 | 1.169 |
| WitnessCount | 12 | 5690 | 1000 | 8 | 13 | 0.916 |
| WitnessInterrupted | 12 | 3015 | 825 | 44 | 18 | 0.867 |
| WitnessMove | 12 | 58233 | 20326 | 28 | 21 | 1.521 |
| WitnessOscillate | 12 | 188720 | 64611 | 9 | 36 | 2.528 |
| WitnessOutside | 12 | 57164 | 20245 | 1539 | 18 | 1.572 |
| WitnessService | 12 | 171545 | 35708 | 9530 | 53 | 2.378 |
| Young | 0 | 59218 | 20556 | 0 | 37 | 1.475 |

The final decisive states were inspected for these actual causes:

- Count adds a second unit for one forwarded original ID. Freshness moves C at
  round 2 using only C's old round-0 report; A/B/W never reported. Replay accepts
  an actual older report and regresses a current stamp. Stability moves after one
  complete evaluation; Tenure moves at age 2; Cooldown moves with 8 minutes left.
  Eligibility promotes W outside R despite the installed placement constraint.
- Projection drops operation 1 from C's transferred view. Frontier transfers
  frontier 1 after grant 2. Result drops the result-obligation key. Unknown turns
  W's durable COMMIT into terminal NO_EFFECT while operation 2 remains durable
  and unselected. These exercise opaque component obligations at their boundary,
  not an independent proof of serialized result/claim bytes.
- Survey activates with only A/B/C reports, omitting W. Fence activates with no
  old cache domains closed. Closure installs C for v1 and A for v2. Self uses C
  as its own memory partner for operation 3. Continuity activates after an actual
  transfer interruption has made continuity unknown.
- Count's witness completes all 12 ingress events with exactly A=1/C=1/W=1
  closure units; W is charged once in each volume. Move has an actual rejected
  replay and an evaluated incomplete/stale round, then a valid C move. Outside
  selects C=256 while W=512 remains outside placement. Interrupted performs
  Transfer then Interrupt, stays term 1/closing and cannot reopen.
- Oscillate evaluates six distinct fresh complete aligned windows 0 through 5.
  Every report stamp equals its round; winners are C/B/C/B/C/B, streak stays one,
  staleRound remains false and owner A never moves. The witness-only ghost set
  contains exactly {0,1,2,3,4,5}; repeated evaluation cannot supply this evidence.
- Service retains operation 2 as a durable-unselected unknown COMMIT across
  handoff, then selects/protects it under C before admitting operation 3. All
  five operations are durable, selected, result-protected and visible; bound
  terms are `<<1,1,2,2,2>>`, memory holders `<<B,C,B,C,C>>`. Operation 2 keeps its
  immutable term-1 grant and W durable origin; C supplies successor reprotection,
  preserving original identities rather than editing old evidence. Both volumes
  belong to C, all four origins read, both old/new domain barriers are complete,
  and C's projection contains all five operations while every backing tree
  remains at genesis. C's own origin fragment for operation 3 is durable;
  inherited fragments 1/2 remain at A/W. No client reply was required to drain
  (all five steps are published phase 6). W syncs operation 2 just before Begin
  in this first witness; delayed old choice after Begin is allowed by the
  complete safety graph, but this witness does not establish that interleaving.

Runner AST parsing and configuration constraint/symmetry checks passed;
`git diff --check` passed. No Go code, lab, SSH, mount, VM, kernel, installation,
publication or production operation was changed or exercised by this task.
