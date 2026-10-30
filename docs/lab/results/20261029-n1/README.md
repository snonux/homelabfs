# Native Stage 1 attempt: 20261029-n1

Forward attempt: 2026-10-30, 05:53:48–05:54:00 UTC.
Canonical plan: `c22be74e288f900b15ebd8852c079f3d51e5e6f6abe2a558dc45053491a16f88`.

Result: **failed admission; no native workload executed**. The approved plan's
source snapshot had been pruned. Rocky stop, lab start and native setup/test
were never attempted.

- [Admission log](prepared-preflight.log): terminal infrastructure failure.
- [Collection log](native-collect.log): guest SSH banner timeout while the guest
  was stopped; initially ambiguous, subsequently reconciled as failed.
- [Final state observation](final-state.json): same Rocky PID, all four FreeBSD
  VMs stopped, no used swap and no matching local guest SSH child.
- [Final report](report.json) and [JUnit report](report.xml): completed failed
  run, including the original ambiguity and explicit reconciliation history.

Reconciliation evidence was obtained at 05:59:27 UTC. Cleanup performed no VM
actions because the corresponding forward steps had not run. Its exit 1 retains
the failed result. The declared remaining-resource inventory is conservative;
the run-owned setup step was never invoked.

Follow-ups: `ax3` (gate guest collection on attempted native setup/test) and
`bx3` (refresh pruned source metadata). No snapshot hold, retention change,
source substitution or production operation was performed.
