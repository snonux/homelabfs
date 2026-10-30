# Native Stage 1 attempt 20261030-n10

The exact reviewed canonical plan hash was
`2f95f94955d11ac06b42c03e133a48e50fa955eb674acdace05fc0ac95903fa7`.
Execution used Paul's recorded standing authorization for bounded disposable
non-production tests. Actual execute `session 31330`, terminal `b13f24`, ran
13:19:48.529–13:22:05.595 UTC and exited 1 after 137.066 seconds.
Final report: `complete=true`, `success=false`.

Admission, graceful Rocky shutdown, native startup/readiness and setup passed.
On hfs-test0, root/direct passed 13 checks with two expected profile skips;
root/buffered passed 14 checks with its expected append skip. These fixtures
passed truncate, mmap, multiuser permissions, append, locks, sync, normal
unmount and mount-loss checks. The first unprivileged/direct fixture passed
binding, namespace, permissions and sync before concurrent append timed out
at 8.000329 seconds. Its later lock failure reused the broken helper client;
it is a cascading failure. The remaining nine fixture calls were not started.

The new bounded stderr diagnostics captured 76 root append events, including
both writers' completed open, sixteen writes, fsync and close. Root write-wrapper
medians were 0.143 and 0.148 seconds. The unprivileged UID 20001 writers emitted
60 events and completed records 0–12 and 0–13 respectively, with write-wrapper
medians 0.560 and 0.558 seconds. Their last observed events began records 13
and 14 at elapsed 7.931 and 8.239 seconds. Neither reached fsync before stopping.
No returned syscall error was captured. Both helpers were making write progress
through the case limit; these wrapper measurements do not identify the kernel,
daemon, credential, storage or scheduling cause and do not prove a final
unfinished syscall was deadlocked. See [captured progress analysis](append-progress-analysis.json)
and the unchanged raw stderr for exact events and hashes.

Collection, normal cleanup, guest shutdown, Rocky restoration and pinned SSH/
kernel restoration readiness all passed in the original invocation. No uncertain
step or reconciliation was needed. This exercises the explicit native SSH
context, without establishing why N9's old transport lost banner exchange.
Normal lab shutdown completed at 13:21:48.985044888 UTC; restoration readiness
finished at 13:22:05.5849923 UTC. Final pinned inventory at 13:25:23 UTC confirmed
Rocky PID 3774 running and all four FreeBSD VMs stopped.

Failed run-owned roots/datasets and private copies of all thirty original
artifact bytes remain under the plan's inspection/168-hour retention policy.
No forced operation, production change, 9p, replication or failover test ran.
The three-guest native gate remains incomplete. Follow-up task `ky3` records the
append latency investigation; `yq3` remains pending. Paul requested a pause
after this attempt's results and normal restoration. No follow-up implementation,
new preparation or additional run was started.
