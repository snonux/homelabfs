# Native run 20261030-n2: preparation only

The replacement [plan](plan.json) has canonical hash
`7bb7e24cadb27672ddb6f7a558d03433d250d37205a6ff35b2e1e037aea66ff5`.
It was prepared after [n1 failed admission](../20261029-n1/).
This directory records preparation, not approval or native acceptance.

The [review](review.txt) describes the exact scope, changed inputs and 87-minute
budget. [Offline validation](offline-validation.json) records the actual Go
planner and context checks; no transport or native driver was executed by those
checks. The collection gate fixed in `127ecc9` is included.

Fresh stopped-image metadata came from the existing common snapshot
`zrepl_20261030_055329_000`. Four metadata/package manifests, the
[twenty fixed receipts](receipts.json), [cleanup proof](cleanup-proof.json) and
[size verification](metadata-size-and-cleanup-proof.json) record the fresh
reads and normal cleanup. No MD/UFS inspection mount remains. Four snapshot
automounts caused by inspection were normally unmounted; source GUIDs and
zero-written checks remained valid. No holds or retention changes were made.
SQLite databases remain in the private controller directory, mode 0600, with
168-hour retention; they are not repository artifacts.

The [Rocky observation](rocky-observation.json) and [package delta](rocky-package-delta.diff)
record six GRUB updates since the old plan. The cause was not established;
the agent performed no package or service change. The running kernel is
unchanged, and the replacement plan pins the newly observed package manifest.

The two build-info files record fresh FreeBSD amd64, CGO-disabled binaries from
clean commit `127ecc9`. Binary bytes and private plan inputs remain outside the
repository. [Manifest](manifest.json) hashes the copied proof files.

The replacement run's controller state, test evidence directory and approval
file were absent when prepared. Actual VM changes and tests require fresh
approval of this exact hash. Ordinary pruning or state/version drift must still
fail admission; plan validity does not extend a source snapshot's lifetime.
