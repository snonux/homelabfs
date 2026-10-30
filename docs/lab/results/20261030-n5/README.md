# Native Stage 1 attempt: 20261030-n5

Paul explicitly approved the prepared plan with canonical hash
`fe727a549b3ddde47e13657ba566babc01be54063daadc5be06f81e86ae509fc`.
The private approval source preserves the actual message `happroval given`,
recorded at 09:13:46 UTC. Original execution session 12886 ended with exit 1
(`927435`).

Admission, clean Rocky shutdown, native guest startup and pinned root SSH/kernel
readiness all passed. Setup then returned an uncertain exit 1 before any guest
preflight receipt was saved. The native driver discarded failed-command stdout
and did not record its argv, so the original log cannot identify the exact
failing command. Reserve/setup are reached only after all three preflight
receipts are saved; none was reached. No run-owned guest resources or any of
the twelve behavior calls were created or executed.

Fresh read-only diagnosis reproduced a guest preflight failure: `stat -f %Lp`
reports `777` for a directory whose actual mode is `1777`, so the sticky-mode
comparison rejects a valid directory. Direct Python mode inspection on all
three guests established `1777`; f3 reported `%Op %Lp %Mp %Sp` as
`41777 777 1 drwxrwxrwt`. The earlier interpretation that the directory lacked
its sticky bit was incorrect. No permission change or guard waiver was made.
Task `ix3` fixes native guards; `jx3` covers the same defect in the separate
native VM fault path and its misleading stat fixture. `lx3` retains future
native-driver diagnostics without inventing discarded original output.

Positive successful ZFS inventories, filesystem checks, mount inventories and
process observations established no n5 root, dataset, mount or process on any
guest. The original setup was explicitly reconciled **failed**. Its approved
cleanup invocation also stopped at the permission-format guard. Fresh
post-attempt observations independently established the cleanup postcondition:
there was nothing to remove and no action remained outstanding. An explicit
cleanup reconciliation accepted that observed quiescent state; it does not
claim the original cleanup command returned zero. The original started and
ambiguous transitions remain in report history.

The runner then normally stopped all three guests through ACPI and started
Rocky. Its restoration readiness check failed to connect to f3 TCP port 22
before dispatching the initial host inventory command. The new SSH diagnostics
record exit 255 and the exact connection-timeout message. Task `kx3` covers
bounded host TCP readiness before that SSH dispatch; SSH uncertainty remains
non-retryable under the approved artifact.

Fresh pinned observations at 09:22:35–37 UTC established Rocky PID 31349 running,
all four FreeBSD VMs stopped, zero host swap, and working Rocky root SSH with
the expected kernel. The original restoration readiness was reconciled failed,
separately from these later successful observations. Final original cleanup
exited 1 (`067fdb`), exhausting its single readiness attempt without dispatching
another action. Final report flags remain `complete=false`, `success=false`,
with no ambiguous step. This is no native filesystem acceptance result.

The original ambiguous report, intermediate restoration report, reconciliation
records, diagnostic observations and recovery logs are preserved. All thirty
original artifact bytes remain archived privately. The report's twelve declared
resources are conservative inventory, not proof of creation. A replacement run
requires reviewed fixes, refreshed source evidence and fresh exact-plan approval.
