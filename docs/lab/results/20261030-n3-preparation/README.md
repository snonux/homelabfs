# Native run 20261030-n3: preparation only

The [exact plan](plan.json) has canonical hash
`344023e57345562905ba8aef837f89763afe9ffabbd8297b24119191b49573b3`.
Its [review](review.txt) describes the unchanged VM limits, twelve native calls
and 87-minute budget. The preflight transport fix is committed as `f79f2c0`.
This directory is preparation evidence, not approval or native acceptance.

The [read-only live planning survey](planning-admission-survey.json) completed
at 06:37:43 UTC with admission checks passing. Final targets, budget and policy
match that survey's recorded input subset. Its [log](planning-admission-survey.log)
retains SSH warnings separately from machine-readable evidence. The
[first survey failure](planning-admission-survey-first-failure.log) remains
recorded: its final host SSH connection timed out during banner exchange.
After a fresh host state check, one bounded planning retry passed. Neither
survey ran a lifecycle action, native driver or workload.

The plan reuses the actual [R4 image metadata and cleanup evidence](../20261030-n2-preparation/).
No new image inspection is claimed. [Continued-source proof](continued-source-proof.json)
and the passing survey checked unchanged source identities, zero intervening
writes and stopped FreeBSD VMs. Existing clean `127ecc9` FreeBSD binaries retain
their original build provenance; Go runtime sources have not changed.

[Offline validation](offline-validation.json) records the real Go planner,
all thirty artifact pins and the local context checks. [Manifest](manifest.json)
hashes these seven copied proof files; raw log bytes are preserved.

The actual run state, test evidence directory and approval file were absent
when prepared. Actual execution needs fresh approval of this exact hash.
The runner will repeat admission and reject source pruning or other drift.
