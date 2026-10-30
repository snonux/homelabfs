# Native Stage 1 attempt 20261030-n13

Canonical plan: `8615d1456af119f0e4d7c7e4a62deea577f3d7a722852080fb4ed770c3087c90`.
The standing-authorized invocation (`90877`, terminal `1ea7fe`) ran
15:25:29.793–15:27:06.826 UTC and exited 1 after 97.033 seconds.
The final report is `complete=true`, `success=false`.

Admission, graceful Rocky stop, startup/readiness and setup passed. The first
root/direct fixture passed append and blocked-lock interruption, then failed:
`acquire-after-exit helper 0 fcntl-try`, EAGAIN/errno 35. Its subsequent read-only
F_GETLK snapshot reported observer PID 1528 and holder PID 1529. Captured append
markers independently identify these as helper 0/A and helper 1/B. Thus the
service still reported helper 1's POSIX range after its Stop/Wait completed,
blocking helper 0. See [correlated evidence](lock-causal-evidence.json).

The fixture recorded nine passes, two expected skips and two failures including
its wrapper. The remaining eleven fixture calls were not started. Task `sy3`
uses this evidence and a controlled real-child regression to correct explicit
process-owner retirement. Neither a specific FreeBSD kernel callback path nor
a successful native fix is claimed. Unprivileged append validation in `ky3`
was not reached.

Collection, normal cleanup, guest shutdown, Rocky restoration and pinned
SSH/kernel restoration readiness all passed in the original invocation.
No ambiguous step or reconciliation occurred. Lab shutdown completed at
15:26:50.306388662 UTC; restoration readiness completed at 15:27:06.816 UTC.
A final pinned 15:34:04 UTC inventory confirmed Rocky PID 37004 running and
all four FreeBSD VMs stopped. Failed roots/datasets and private copies of all
thirty artifact bytes remain for inspection under the 168-hour retention policy.
No forced operation, production change, 9p, replication or failover test ran.
