# Native Stage 1 attempt 20261030-n12

Canonical plan: `a682cb2bdeaae5c62b795c5340d15a0eec43eed5661b4719f4e0c29e6b84592b`.
The standing-authorized invocation (`6473`, terminal `b0a8f7`) ran
14:59:00.073–15:00:40.925 UTC and exited 1 after 100.852 seconds.
The final report is `complete=true`, `success=false`.

Admission, graceful Rocky stop, native startup/readiness and setup passed.
The first root/direct fixture on hfs-test0 passed binding, namespace, permissions,
sync, append and blocked-lock process interruption. Append completed its full
oracle, with both writers' sixteen writes, fsync and close captured in stderr.
The later process-fcntl/flock contention/exit case failed with
`fcntl-try: resource temporarily unavailable`. Its current error does not name
which expected-success acquisition failed, so neither a particular holder nor
an interruption/exit ordering cause is established. Task `qy3` adds causal
stage/holder evidence before changing lifecycle behavior. The fixture recorded
nine passes, two expected skips and two failures including its wrapper; the
remaining eleven calls were not started.

Collection, normal cleanup, native guest shutdown, Rocky restoration and pinned
SSH/kernel restoration readiness all passed in the original invocation. No
ambiguous step or reconciliation occurred. Lab shutdown completed at
15:00:23.429555283 UTC. Final pinned inventory at 15:02:05 UTC confirmed Rocky
PID 24786 running and all four FreeBSD VMs stopped. Run-owned failed resources
and private copies of all thirty artifact bytes remain for inspection under
the plan's 168-hour retention policy.

This attempt did not reach the unprivileged append fixture, so native validation
of task `ky3` remains pending despite the local journal read-call improvement.
No underlying native timeout or lock cause is claimed. No forced operation,
production change, 9p, replication or failover test ran.
