# Native Stage 1 attempt: 20261030-n6

Paul approved the exact plan with canonical hash
`7b3076c5025a192f99003a488ad31877d49102dd4c1fcbe00b5e9957cc449a94`.
The actual answer, `Approve this exact n6 run`, was recorded privately at
11:06:14 UTC. He also gave standing approval for bounded non-production tests
and their planned graceful lab lifecycle; critical or dangerous operations and
production changes remain outside that scope.

Execution ran from 11:06:19 to 11:07:48 UTC, finishing after 89.85 seconds with
exit 1. The final [report](report.json) records `complete=true`, `success=false`.
Admission, clean Rocky shutdown, guest startup, pinned SSH/kernel readiness and
setup all passed. The reviewed mode guards, connection wait and diagnostics
were included in the pinned scripts. The daemon and helper retained their actual
clean `127ecc9` build identity.

The first root/direct fixture on `hfs-test0` failed during mounting:

```text
mount_fusefs: -o dev: option not supported
```

The adapter supplied Linux's `nodev` option to FreeBSD's mount helper. The
[fixture report](native-single-node/hfs-test0/root-hfs-direct.json) explicitly
states that setup failed before behavior execution. One of twelve planned
fixture invocations was attempted; no filesystem behavior case ran. Its daemon
process exited 1 without a preserved live process. The remaining eleven
invocations were not started. Task `ay3` fixes platform-specific mount options
while retaining permission checks, set-ID suppression and the adapter's
device-node restrictions.

The outer `native-test` step records category `assertion` because the driver
returned its known terminal exit 10. Its leaf failure is an infrastructure
fixture-lifecycle failure, not evidence of an incorrect filesystem operation.
The new driver diagnostics retain the actual command, return code and separate
output excerpts in [native-test.log](native-test.log).

Collection, normal cleanup, all three guest shutdowns, Rocky restart and pinned
restoration readiness passed in the original invocation. No uncertain step or
manual reconciliation remains. Rocky root SSH returned the expected kernel and
UID 0, and the final inventory established Rocky running with all four FreeBSD
guests stopped. A later read-only inventory at 11:25:59 UTC confirmed Rocky
PID 51221 and those same stopped guests.

Run-owned data and evidence remain for inspection under the declared retention;
cleanup did not delete the failed fixture. The twelve resources in the runner's
report are its declared inventory, not a measurement of retained bytes. No
forced unmount, forced stop, host reboot, production or replication-sink change
occurred. This unsuccessful run supplies no native acceptance result.

A replacement run uses the reviewed mount fix, a newly built clean candidate
and fresh stopped-image evidence after this run's guest shutdown. Its bounded
non-production execution is covered by Paul's standing authorization above.
