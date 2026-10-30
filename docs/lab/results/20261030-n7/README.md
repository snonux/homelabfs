# Native Stage 1 attempt: 20261030-n7

This bounded disposable-lab run used canonical plan hash
`8d2b54e5e4935cb4201b4d79c299230ca5a67813169fc71fc6f8d76dda1cb01c`.
Execution was authorized by Paul's standing instruction for non-production
tests and their planned graceful lifecycle. The private authorization source
records that basis explicitly and does not invent another per-run user reply.
Critical or dangerous scope changes and production remain excluded.

The new FreeBSD binaries recorded actual clean source `733d843`, including the
reviewed mount-option fix. An earlier build without VCS metadata was rejected
before use. Execution ran from 11:47:29 to 11:49:08 UTC, finishing after
98.71 seconds with exit 1. The final [report](report.json) records
`complete=true`, `success=false`.

Admission, clean Rocky shutdown, native guest startup, pinned SSH/kernel
readiness and setup passed. The first root/direct fixture on `hfs-test0`
mounted successfully and ran real behavior cases. Its
[namespace case](native-single-node/hfs-test0/root-hfs-direct.json) failed at:

```text
namespace: ftruncate: operation not supported
```

The leaf report contains nine passing checks: binding, permission/metadata,
sync, append, two locking/process checks, owned-helper shutdown, normal unmount
and mount-loss/no-fallback. Two expected direct-profile skips defer mmap and
root multiuser coverage to the buffered companion. The namespace failure and
its enclosing fixture-lifecycle failure are both retained. The remaining eleven
fixture invocations were not started; this is no complete native matrix pass.

Task `cy3` addresses the rejected native truncate. FreeBSD kernel source folds
dirty cached timestamps into size changes; focused callback tests reproduced
the rejected size/atime and size/atime/mtime/ctime forms. N7 did not capture the
exact request mask, so that diagnosis is source-inferred. The fix selects the
timestamps and size in one existing TRUNCATE metadata afterimage, preserving
journal 1.0 and rejection of unsupported mode/ownership combinations. Native
verification with that new candidate remains a separate run.

Collection, normal cleanup, all three guest shutdowns, Rocky restart and pinned
restoration readiness passed in the original invocation. No uncertain step or
manual reconciliation remains. Guest shutdown completed at 11:48:51.877 UTC.
A later pinned read-only inventory at 11:59:43 UTC confirmed Rocky PID 64854
running with all four FreeBSD VMs stopped.

Run-owned resources remain for inspection under the declared retention. The
twelve resources listed by the runner are its declared inventory, not a
retained-byte measurement. No forced unmount/stop, host reboot, production or
replication-sink change occurred. A replacement uses the reviewed truncate fix,
a fresh clean build and stopped-image evidence after this run's shutdown,
within the same standing-authorized bounded non-production profile.
