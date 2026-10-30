# Native Stage 1 attempt 20261030-n8

The exact reviewed plan hash was
`5dcec28f8314334b9c87d328d6a324d07fcc22af59b040186a2e1e19514a45bd`.
Execution used Paul's recorded standing authorization for bounded disposable
non-production tests. The actual invocation ran 12:40:17.722–12:41:23.676 UTC
and exited 1 after 65.953 seconds. The final report is `complete=true`,
`success=false`.

Read-only admission, graceful Rocky stop and asynchronous native guest start
passed. Native readiness rejected the completed inventory row
`hfs-test0  hfslab  bhyveload  2  2G  -  No  Locked (f3.lan.buetow.org)`.
No native setup or workload ran. Collection and native cleanup were correctly
skipped because their prerequisite setup/test steps were never attempted.
The report's twelve potentially remaining resources are the conservative
declared inventory, not evidence that native setup created them.

Normal guest shutdown, Rocky restoration and pinned SSH/kernel restoration
readiness all passed in the original invocation. Lab shutdown completed at
12:41:04.045979787 UTC. A subsequent pinned host inventory showed Rocky PID
79067 running and all four FreeBSD VMs stopped. No uncertain step, forced
operation or reconciliation was needed.

Task `ey3` handles the readiness parser. Actual installed vm-bhyve 1.7.5
source shows that `vm::lock` writes the local hostname before loading the
guest. The inventory reports `Locked (hostname)` when no bhyve or bootloader
process was detected and `run.lock` exists. This explains a possible local
startup gap; a stale lock is also possible, so a local lock can only be
bounded not-ready and must never count as execution readiness. Foreign-host
locks remain refusals. The first source query exited 2 because an additional
requested `vm-subr` path did not exist; its actual source output and stderr
are retained. Both subsequent source queries exited 0.

The FreeBSD truncate fix remains unvalidated by this native attempt. Fresh
stopped-image preparation is tracked by `gy3`; image sources must postdate
this boot/shutdown before any replacement run. No distributed protocol,
replication/failover or 9p claim follows from this infrastructure failure.
