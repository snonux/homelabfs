# All-origin bounded model verification (`dz3`)

Completed local TLC checks on 31 October 2026. This records the finite scopes in
[the model README](README.md#successor-all-origin-commitment-model-dz3), including
its atomic complete-fragment/cache/dependency assumptions and explicitly seeded
recovery component graphs. It does not prove full ordinary-to-recovery
composition, runtime refinement, arbitrary bounds or distributed readiness.

Module SHA-256: `d6a0a69097953d62b7401bb118610a7e892dabea3a251b23ee56d698717c3414`.
Runner SHA-256: `d3cdcaa6a7f098a234f9c32701caa0cded484e465d249ffa189f1c23eb623d51`.

TLC 1.8.0 jar SHA-256:
`fc6878c603f0fdc18b9fe3e5bf51860cb90ab5b1154e3c371660c036d6bdd63e`.
Each checker used one worker, one active processor and a 1 GiB maximum heap.
Each row has exact snapshotted source/config bytes, hashes, command, log, elapsed
wall time and process exit in the listed local evidence directory. There are no
state/depth cutoffs. Per-profile wall limits fail incomplete runs; they cannot
produce a passing positive result. Exhaustion is conditional on no TLC
fingerprint collision hiding a distinct state. Logs retain the estimates.

Evidence sets: `.cache/tla/AllOrigin-vvi75tfo`, `.cache/tla/AllOrigin-k83wd8nr`, `.cache/tla/AllOrigin-b1yhxpk_`, `.cache/tla/AllOrigin-eeh8agoc`, `.cache/tla/AllOrigin-9ov2fxm_`, `.cache/tla/AllOrigin-1z4w8pm2`, `.cache/tla/AllOrigin-wex5et1g`.

## Complete finite safety checks

Every row completed with actual exit zero and zero queued states.

| Profile | Generated | Distinct | Depth | Seconds | Wall cap |
| --- | ---: | ---: | ---: | ---: | ---: |
| AllOriginAppend | 376,800 | 36,414 | 38 | 4.375 | 180 |
| AllOriginBlockedRecovery | 3,444,145 | 410,040 | 23 | 57.606 | 180 |
| AllOriginChmod | 376,800 | 36,414 | 38 | 4.726 | 180 |
| AllOriginCreate | 376,800 | 36,414 | 38 | 4.776 | 180 |
| AllOriginEquivalent | 558,769 | 69,984 | 22 | 7.834 | 180 |
| AllOriginHandoff | 167,953 | 20,736 | 20 | 3.572 | 180 |
| AllOriginOutsideLoss | 12,859,057 | 1,469,256 | 45 | 137.696 | 180 |
| AllOriginOverwrite | 376,800 | 36,414 | 38 | 5.779 | 180 |
| AllOriginOwner | 959,192 | 88,154 | 44 | 12.045 | 180 |
| AllOriginOwnerLoss | 2,144,578 | 254,598 | 46 | 23.471 | 180 |
| AllOriginRecovery | 925,873 | 110,808 | 21 | 12.547 | 180 |
| AllOriginRecoveryDecline | 8,351,334 | 992,480 | 29 | 98.546 | 180 |
| AllOriginRecoveryPending | 8,351,334 | 992,480 | 29 | 95.821 | 180 |
| AllOriginRecoveryReplied | 225,421 | 27,702 | 19 | 4.174 | 180 |
| AllOriginRecoveryResult | 2,752,921 | 332,280 | 24 | 34.806 | 180 |
| AllOriginRecoveryStaged | 34,119,575 | 4,024,264 | 31 | 394.339 | 600 |
| AllOriginRename | 376,800 | 36,414 | 38 | 4.726 | 180 |
| AllOriginRestart | 10,945,820 | 942,730 | 46 | 109.736 | 180 |
| AllOriginThird | 48,438,489 | 3,550,148 | 56 | 685.650 | 1200 |
| AllOriginTransaction | 376,885 | 36,428 | 39 | 7.132 | 180 |
| AllOriginTruncate | 8,075,391 | 592,012 | 56 | 160.403 | 180 |

The same-source incomplete attempts retained in these evidence sets were:

- AllOriginRecoveryStaged: actual exit 124 at a 180-second wall cap; incomplete, not a pass.
- AllOriginThird: actual exit 124 at a 180-second wall cap; incomplete, not a pass.
- AllOriginThird: actual exit 124 at a 600-second wall cap; incomplete, not a pass.

## Expected counterexamples

Each row has actual exit 12 and the named invariant violation. `Safety` rows
are genuine deliberately broken policies. `WitnessAbsent` rows establish only
one reachable execution of the unmutated protocol; their state graphs were not
exhausted and these rows are not positive safety or liveness proofs.

| Profile | Expected invariant | Generated | Distinct | Queued at counterexample | Depth |
| --- | --- | ---: | ---: | ---: | ---: |
| AllOriginNegativeBarrier | Safety | 504 | 123 | 49 | 9 |
| AllOriginNegativeDependencies | Safety | 155 | 43 | 18 | 6 |
| AllOriginNegativeFence | Safety | 29,863 | 5,834 | 3,591 | 7 |
| AllOriginNegativeHole | Safety | 8 | 6 | 3 | 3 |
| AllOriginNegativeMajority | Safety | 183,743 | 33,667 | 17,514 | 9 |
| AllOriginNegativePrepare | Safety | 47 | 15 | 5 | 5 |
| AllOriginNegativeResult | Safety | 1,252 | 281 | 122 | 10 |
| AllOriginNegativeSelf | Safety | 127 | 34 | 12 | 6 |
| AllOriginNegativeSpeculative | Safety | 8 | 6 | 3 | 3 |
| AllOriginNegativeStalePlan | Safety | 3,415 | 616 | 163 | 13 |
| AllOriginNegativeStaleRead | Safety | 1,525 | 322 | 115 | 11 |
| AllOriginWitnessAbsence | WitnessAbsent | 1,503,336 | 245,524 | 101,141 | 13 |
| AllOriginWitnessAppend | WitnessAbsent | 409,189 | 47,018 | 8,190 | 29 |
| AllOriginWitnessBlocked | WitnessAbsent | 30,621 | 6,829 | 4,339 | 7 |
| AllOriginWitnessDecline | WitnessAbsent | 62,898 | 8,515 | 2,218 | 25 |
| AllOriginWitnessDurableDecline | WitnessAbsent | 606,210 | 100,087 | 41,425 | 12 |
| AllOriginWitnessEquivalent | WitnessAbsent | 164,384 | 25,634 | 9,269 | 10 |
| AllOriginWitnessHandoff | WitnessAbsent | 127,212 | 18,938 | 6,117 | 10 |
| AllOriginWitnessLate | WitnessAbsent | 40,149 | 5,363 | 709 | 20 |
| AllOriginWitnessLoss | WitnessAbsent | 8,952 | 1,887 | 659 | 13 |
| AllOriginWitnessPending | WitnessAbsent | 606,210 | 100,087 | 41,425 | 12 |
| AllOriginWitnessPermission | WitnessAbsent | 161,753 | 19,421 | 4,011 | 28 |
| AllOriginWitnessRecovery | WitnessAbsent | 443,024 | 63,579 | 17,101 | 12 |
| AllOriginWitnessReplied | WitnessAbsent | 141,760 | 20,143 | 4,204 | 12 |
| AllOriginWitnessRestart | WitnessAbsent | 880,077 | 109,410 | 32,064 | 27 |
| AllOriginWitnessResult | WitnessAbsent | 836,833 | 128,397 | 41,442 | 12 |
| AllOriginWitnessServed | WitnessAbsent | 2,436,459 | 243,744 | 7,194 | 36 |
| AllOriginWitnessStaged | WitnessAbsent | 4,648 | 791 | 174 | 14 |
| AllOriginWitnessTruncate | WitnessAbsent | 7,012,292 | 617,309 | 110,461 | 42 |
| AllOriginWitnessUnknown | WitnessAbsent | 729 | 195 | 83 | 8 |

## Retained nonfinal attempts

Earlier snapshots are development history, not the final result set. They
include incomplete 180-second runs, and early positive graphs before `Publish`
was wired into `Next`; the missing service witness and vacuous barrier mutation
detected that model error. Those attempts were not treated as service evidence.
`AllOrigin-h1jy9a4w`, `AllOrigin-d8eueiye`, `AllOrigin-admvlr7v`,
`AllOrigin-j9h2tjec`, `AllOrigin-diagnostic-w2mwuxzo`, `AllOrigin-aafjkyd1`,
`AllOrigin-587eb6_j`, `AllOrigin-3p06vi50` and `AllOrigin-cs6iuixl` remain under
`.cache/tla/`. The last set is a successful targeted cost check after Boolean
guard normalization; final rows above use the consistent source listed here.
Normalization replaces a pure-state Boolean guard `p` by `p = TRUE` so TLC does
not enumerate multiple satisfying proof branches as duplicate action successors.
It does not remove transitions, introduce constraints or shorten histories.
