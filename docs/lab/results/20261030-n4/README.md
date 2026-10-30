# Native Stage 1 attempt: 20261030-n4

Canonical plan: `b4503fabe5cb6670b0537fec2c33e4f9be7bc449a74d65dcab6f9b67b23b48f1`.
Paul's instruction to continue after the pending exact n4 approval request was
recorded at 08:25:37 UTC. The private approval source preserves the actual word
`continue`; it does not claim a separate affirmative questionnaire answer.

The actual runner exited 1 (`session 58355`, terminal `16a02f`). Fresh admission,
clean Rocky ACPI shutdown and asynchronous native guest startup passed.
`native-readiness.log` records a known infrastructure failure on the expected
`Bootloader (14489)` state. No native setup or any of the twelve behavior calls
ran. Collection and run-owned cleanup were skipped because setup was never
attempted. The twelve resources in the report are its conservative declaration,
not proof that they were created.

The pinned normal `vm stop` path ended the native bootloaders; vm-bhyve printed
`Killing`, but no force flag or abrupt fault action was invoked. Lab shutdown and
Rocky startup passed. The original restoration readiness check reported uncertain
SSH transport completion before its first successful host-inventory marker. Its
log does not establish a Rocky guest SSH attempt or a specific transport cause.

`report-before-reconciliation.json`, its XML and the original execute log
preserve that ambiguous state. Fresh pinned host and guest observations at
08:27:22/24 UTC establish Rocky PID 17368 running, all four FreeBSD VMs stopped,
zero swap, no MD attachment or run mount, and later working Rocky root SSH with
the expected kernel. Host/guest process inventories and the separate local
process observation establish no outstanding original readiness command.

The explicit reconciliation records the original restoration check as **failed**.
Original cleanup then exited 1 (`dd6ff0`): its already consumed single readiness
attempt could not run again. Final `report.json` and XML retain
`complete=false`, `success=false`, with no ambiguous step. No new executor or
VM action occurred during reconciliation cleanup. Present restored topology is
separate from success of the original planned readiness check.

Tasks `ex3` and `fx3` cover the boot-state defect and restoration diagnosis.
All thirty original artifact bytes are preserved privately for recovery. This
attempt provides no native filesystem acceptance result. Replacement execution
needs fresh source evidence, a new plan and explicit approval.
