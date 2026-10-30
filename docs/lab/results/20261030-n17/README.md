# Native Stage 1 attempt 20261030-n17

Canonical plan: `5ff915e650738e656cd7cc5ad21dad9f2a112083c351601c83c408aa4910afec`.
The standing-authorized invocation (`37984`, terminal `a7cb6f`) ran
16:38:40.168–16:40:00.861 UTC and exited 1 after 80.693 seconds.
Final report after explicit failed reconciliation: `complete=true`, `success=false`.

On hfs-test0, root/direct passed 13 checks with two expected skips, root/buffered
passed 14 with one skip, and unprivileged/direct passed 13 with two skips.
Unprivileged append passed the complete content/sync oracle in 4.304938606
seconds under the unchanged eight-second bound, versus N16's timeout at
8.003309078 seconds. This validates `ky3`'s codec improvement for the previously
failing append stage. The only runtime Go change was ceb19e8's bulk UUID/hash
encoding, reviewed and checked for identical bytes, validation and I/O/barriers.
The full native matrix is not complete.

The fourth fixture, unprivileged/buffered, failed its namespace unlink of a
replacement with EIO and subsequent EHOSTDOWN/UNAVAILABLE. Its process exited 1
without timeout; no owned process remained, but a stale FUSE mount did. Later
permission/sync/mmap/lock errors followed that initial failure. The remaining
eight fixtures did not run. Task `xy3` investigates this separate failure;
previous runs never reached this fixture, so introduction by the codec change
is not established.

The runner conservatively classified test exit as uncertain and blocked actions.
A pinned read-only inspection (`d8ed4e`) captured the fourth report, process exit
and mount state. Exact root/mode/enrollment/plan, no-process and FUSE source/type
checks preceded a normal owned unmount (`399355`, exit 0 at 16:44:15 UTC).
No forced operation occurred. Explicit reconciliation records the test as failed,
preserving original ambiguous reports. Invocation `12593` (`2d66fb`) ran only
pending collection and normal teardown/restoration, never another workload.
Collection, normal cleanup, lab shutdown, Rocky restoration and readiness passed;
the overall exit remains 1. Lab stop finished at 16:45:17.057469378 UTC. Final
pinned inventory (`ed44b8`) confirmed Rocky PID 76196 running and all four
FreeBSD VMs stopped. All thirty original artifact bytes remain private, mode0600.

Controlled local investigation reproduced IO followed by UNAVAILABLE when the
harness's process-wide 256 KiB file limit prevented journal growth; native
source-size confirmation and a corrected harness run remain pending. No native
cause or full-gate pass is inferred. No production, 9p, replication or failover
test ran, and the intermittent SSH investigation remains unresolved.
