# Stopped-domain barrier checks (`qz3`)

Completed exact-source local TLC checks on 31 October 2026. This records the finite
scopes and component assumptions in [View-barriers.md](../../docs/spec/View-barriers.md).
It is not a kernel/MM proof, distributed read-token implementation, machine-checked
refinement/composition with AllOrigin, arbitrary-bound proof or readiness approval.
The 9p credential/maintained-patch proposal was unselected when these checks ran;
the subsequent [design decision](../../docs/lab/Retained-9p-authorization.md)
does not change their proof scope. Existing AllOrigin
modules/configurations/results and their earlier scopes remain unchanged.

Module SHA-256: `db02f81e83df96fe39900e50b4000df2ccdb5001e946db558abbb1d51a1a206f`.
Runner SHA-256: `f7ec669a82707d77d347a34669f3bae802490b25cdcd0844b748dd5a2d2b0502`.
TLC 1.8.0 jar SHA-256:
`fc6878c603f0fdc18b9fe3e5bf51860cb90ab5b1154e3c371660c036d6bdd63e`.

Each checker used one worker, one active processor and 1 GiB maximum JVM heap.
There were no state/depth cutoffs, constraints or symmetry reductions. The four
positive graphs completed with actual child exit zero and zero queued states.
Progress checked Safety and fair Completion over the same finite transition graph
as Reconnect, allowing at most one disconnect/reconnect per domain. All remaining
profiles used a 180-second wall deadline; Progress used 900 seconds after the
retained incomplete 180-second attempt. A wall/resource limit is failure, with the
actual child termination code retained, never a successful partial result.

Final exact evidence sets: `.cache/tla/ViewBarrier-c0gxfqcl` (sixteen profiles) and
`.cache/tla/ViewBarrier-h2dpimfs` (Progress). Each retains exact module/configuration,
runner bytes, source/configuration/runner/tool hashes, arguments, logs, actual
exits, counts and elapsed time. The complete local receipt index is
`.cache/tla/view-barrier-qz3-final-receipts.json`. Every indexed executed snapshot
was byte-compared with the final source/configuration. Evidence was bounded to
2 GiB per profile and 4 GiB aggregate across all retained ViewBarrier attempts;
no cap was reached and no failed evidence was deleted.

## Complete positive graphs

Two symbolic domains represent native and guest frontends. Safety admits two
once-dirtied pages/two packages, Capacity two potential pages/one package, and
Reconnect/Progress one page/one package with one possible disconnect per domain.
The dirty-token owner can be either domain. One final conflicting operation
follows the frozen-manifest drains; repeated authority/token transfers, credit
recycling and daemon/kernel crashes are outside this graph.

| Profile | Generated | Distinct | Queued | Depth | Seconds | Wall cap | Child exit |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| ViewBarrierCapacity | 81,650 | 25,064 | 0 | 54 | 4.729 | 180 | 0 |
| ViewBarrierProgress | 3,302,202 | 788,848 | 0 | 66 | 410.854 | 900 | 0 |
| ViewBarrierReconnect | 3,302,202 | 788,848 | 0 | 66 | 132.731 | 180 | 0 |
| ViewBarrierSafety | 212,818 | 64,936 | 0 | 68 | 8.348 | 180 | 0 |

## Expected counterexamples and witnesses

Seven Safety mutations require an invariant violation/child exit 12. Recursive
requires a genuine Completion temporal counterexample/exit 13: its completed
fair graph contains a dirty page with protected result, both domains STOPPED and
both intermediate commands delivered, while installation waits for DRAINED,
cleanup waits for installation and DRAINED waits for cleanup. The terminal
stuttering execution satisfies the worker fairness assumptions. It is not a
wall deadline or an assumed failed worker.

The five WitnessAbsent violations/exit 12 show reachable executions, not safety
or eventual-progress proofs. OfflineStop specifically has delivered recall,
then disconnect while recalling, then actual local Stop while disconnected,
then queued STOPPED acknowledgement offline. Its observer is enabled only in
that witness and does not affect protocol guards. Reconnect requires dirty
workload, rejection before completion of an actually emitted session-1 message
after session 2 is installed, and successful final completion. Read requires
actual post-reply observations from both domains.

| Profile | Expected violation | Generated | Distinct | Queued | Depth | Seconds | Child exit |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| ViewBarrierNegativeAlias | Safety | 912 | 376 | 245 | 5 | 1.122 | 12 |
| ViewBarrierNegativeCredit | Safety | 4 | 4 | 2 | 2 | 1.002 | 12 |
| ViewBarrierNegativeEarly | Safety | 33,266 | 9,236 | 572 | 16 | 2.326 | 12 |
| ViewBarrierNegativeHold | Safety | 4 | 4 | 2 | 2 | 1.072 | 12 |
| ViewBarrierNegativeRead | Safety | 659 | 249 | 167 | 5 | 1.174 | 12 |
| ViewBarrierNegativeRecursive | Completion (temporal) | 45,522 | 13,736 | 0 | 42 | 9.708 | 13 |
| ViewBarrierNegativeReopen | Safety | 44,245 | 12,971 | 33 | 27 | 3.133 | 12 |
| ViewBarrierNegativeStale | Safety | 38 | 38 | 33 | 3 | 0.717 | 12 |
| ViewBarrierWitnessClosedInstall | WitnessAbsent | 44,405 | 13,107 | 49 | 30 | 3.438 | 12 |
| ViewBarrierWitnessDirty | WitnessAbsent | 212,699 | 64,853 | 31 | 64 | 8.565 | 12 |
| ViewBarrierWitnessOfflineStop | WitnessAbsent | 59,714 | 14,421 | 6,149 | 9 | 2.732 | 12 |
| ViewBarrierWitnessRead | WitnessAbsent | 46,157 | 14,147 | 9 | 52 | 3.436 | 12 |
| ViewBarrierWitnessReconnect | WitnessAbsent | 3,008,316 | 723,979 | 8,312 | 53 | 98.740 | 12 |

The negative final states were checked for their intended causes: writable
permission without pre-reservation; STOPPED while busy/pinned; final grant with
unresolved frozen dirt; a waiting caller holding sequencer/vnode/drain-credit
resources; cross-domain observation while client dirt remains unselected;
reopening during an intermediate drain; and acceptance of a mismatched old
context. These are deliberate mutations, not findings in the proposed sequence.
The stale mutation bypasses the full context check together; it is not
independent field-specific sensitivity tests. The genuine reconnect witness
separately exercises an actual emitted old-session acknowledgement.

## Retained development attempts and limits

- `ViewBarrier-vh6dp2_f`: first negative exited 75 in the initial state because a
  string sentinel was tested against an integer interval. Untyped model-value
  sentinels corrected the TLC representation; this attempt is not a pass.
- `ViewBarrier-q1yjvdjy`: three Safety controls passed, then Recursive produced
  actual exit 13 and the desired Completion counterexample, but the runner
  expected different output wording and failed the batch. Its exact original
  runner bytes are retained. The final runner checks the property-specific line.
- `ViewBarrier-woldwdvt`: six predecessor-source profiles passed after that
  parser correction; this was before the offline-execution history observer.
- `ViewBarrier-5hdwvg8w`: Capacity and Reconnect completed on predecessor source;
  Progress hit its 180-second wall deadline after 180.664 seconds, actual child
  exit 143, with no final exploration summary. Its last periodic report was
  200,983 distinct states and 22,979 queued at 120 seconds. It is incomplete,
  never a passing liveness result. Remaining profiles in that batch did not run.

The final seventeen profiles above reran the frozen observer-strengthened source.
Static self-review checked all 26 actions cover all 33 state variables, every
transition is wired into Next, all seventeen configurations have no constraints,
and the Python runner parses. `git diff --check` also passed. No Go runtime test,
lab, SSH, mount, VM, kernel patch, installation or publication operation was run
for this model task.

Progress depends on stable surviving authority/ledgers, finite modeled connection
losses, fair reconnection/delivery/workers, eventual busy/pinned alias release,
available reserved capacity and working physical barriers. The observation guard
is a semantic token interface, not a proof of distributed read-token messages or
remote knowledge of CPU stores. Implementations must use known writable-token
ownership/recall before conflicting reads, without assuming remotely observable
dirty flags. CPU/client dirty bytes, delivered daemon requests
and protected selected records remain distinct. Actual kernel revocation, pinning,
provenance, credential authorization, memory/resource implementation and complete
normal/recovery composition remain separate obligations.

Complete exploration is conditional on TLC fingerprints not hiding distinct
states. Progress reported collision estimates `1.1E-7` (calculated optimistic)
and `2.0E-7` (actual fingerprints); the exact logs retain every profile's estimate.
No mathematical collision-free or arbitrary-bound proof is claimed.
