# Native Stage 1 attempt 20261030-n16

Canonical plan: `20bb431fc9fba1ea3f7455ef815e2b7fd9deb04ddb486b70a47f4e9585e1f6f6`.
The standing-authorized invocation (`76788`, terminal `7d2a86`) ran
16:18:33.842–16:20:50.785 UTC and exited 1 after 136.943 seconds.
Final report: `complete=true`, `success=false`.

On hfs-test0, root/direct passed 13 checks with two expected skips; root/buffered
passed 14 with one expected skip. Both process-fcntl/flock contention/exit cases
passed, in 0.309 and 0.554 seconds. This validates task `sy3`'s explicit
process-owner retirement fix for the previously failing root acquisition after
helper exit. The controlled regression, negative ownership/lifecycle coverage,
fresh implementation review and local full checks are recorded with commit
a36d4a4. A specific FreeBSD kernel callback cause remains unproven.

Unprivileged/direct append then timed out waiting for helper 0 at 8.003309078
seconds. Its later lock failure reused that broken helper client; it is not a
new independent lock diagnosis. No unprivileged fsync was reached. The scan-window
optimization did not resolve this native latency. Task `ky3` remains open; the
remaining nine fixtures did not run, and task `yq3`'s full native gate remains
pending. See raw fixture results and the validation summary.

Admission, readiness, setup, collection, normal cleanup, all guest shutdown,
Rocky restoration and pinned restoration readiness passed in the original
invocation. No ambiguous step or reconciliation occurred. Lab stop completed at
16:20:34.490385939 UTC; restoration readiness at 16:20:50.772874332 UTC. A fresh
pinned inventory (`943f5c`) confirmed Rocky PID 63146 running and all four
FreeBSD VMs stopped. All thirty original artifact bytes remain privately
preserved with mode 0600 before further pinned helper edits.

Separate read-only SSH/PTR packet summaries excluded their own connection,
used no payload dump/pcap, and stopped at the 2000-packet cap after about
21 seconds, before the 180-second deadline. Their receipt and stderr are kept
here; raw summaries are retained privately. No SSH failure was captured or
cause resolved; task `uy3` remains open. No forced operation, production change,
9p, replication or failover test ran.
