# Native Stage 1 attempt: 20261030-n2

Paul explicitly approved canonical plan
`7bb7e24cadb27672ddb6f7a558d03433d250d37205a6ff35b2e1e037aea66ff5`.
Execution on 2026-10-30 failed read-only admission with
`live guest kernel changed: rocky`. No VM lifecycle, native setup or workload
step ran. The collection gate fixed by `ax3` skipped collection and all
inapplicable teardown. The run closed `complete=true`, `success=false`, exit 1.

- [Admission log](prepared-preflight.log), [report](report.json) and
  [JUnit report](report.xml) preserve the actual failed result.
- [Read-only diagnostic](kernel-output-diagnostic.json) shows the correct
  expected kernel line, preceded by SSH diagnostic warnings. The old transport
  merged stderr into stdout, making the strict kernel comparison fail.
- [Final state/source proof](final-state.json), observed at 06:25:14 UTC,
  confirms the same Rocky PID 6449, all four FreeBSD VMs stopped, zero used swap
  and the unchanged R4 source GUIDs with zero intervening writes.

Task `cx3` fixes transport stream separation while retaining diagnostics,
the original combined output cap, host-key pins and version checks. This
attempt provides no native filesystem acceptance result.
