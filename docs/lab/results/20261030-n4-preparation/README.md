# Native run 20261030-n4: post-boot preparation

The [concrete plan](plan.json) has canonical hash
`b4503fabe5cb6670b0537fec2c33e4f9be7bc449a74d65dcab6f9b67b23b48f1`.
Its [review](review.txt) describes the unchanged twelve calls and 87-minute
window. This directory records preparation, not approval or native acceptance.

The [n3 attempt](../20261030-n3/) failed admission after the authorized f3 boot
invalidated its old image evidence. R6 freshly inspected all four stopped images
from existing post-boot snapshot `zrepl_20261030_074853_000`. It did not assume
unchanged images. [Helper review](helper-review.json) and the
[constant-only delta](helper-constant-delta.diff) record the reused bounded
methods, fresh roots, source identities and current Rocky PID 6440.

Four fresh metadata/package manifests, [twenty actual receipts](receipts.json),
[cleanup proof](cleanup-proof.json) and [size proof](metadata-size-and-cleanup-proof.json)
establish fresh reads and normal exact-owned cleanup. No MD/UFS inspection
attachment remains. Four host snapshot automounts were normally unmounted;
source GUIDs and zero-written checks remained valid after reads. No guest pool
import/mount, repair, snapshot creation, hold, ownership or retention change ran.
Private databases remain mode 0600, with 168-hour retention, outside Git.

The [live read-only planning survey](planning-admission-survey.json) passed at
08:13:24 UTC. Final targets, budget and policy exactly match its input subset.
The [log](planning-admission-survey.log) retains SSH diagnostics separately.
No driver, lifecycle action or workload ran in that survey.

[Offline validation](offline-validation.json) records the actual cached Go
planner, thirty artifact pins and context checks. [Manifest](manifest.json)
hashes the twenty copied proof files. Retained native binaries have their
original clean `127ecc9` build provenance; Go runtime sources have not changed.

The n4 approval, controller state and execution evidence directory were absent
when prepared. Actual execution still needs fresh approval of this exact hash
and will repeat admission. Another host restart or ordinary source pruning can
invalidate the evidence before the plan expires; checks must fail closed.
