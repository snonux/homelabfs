# Native Stage 1 attempt 20261030-n9

The reviewed canonical plan hash was
`717199681ab3c7290a3c2752d57f07dd3aec2b8e633cf67c15d6d72044fa24ab`.
Execution used Paul's recorded standing authorization for bounded disposable
non-production tests. The original execute invocation ran 12:55:48.418–
12:57:29.484 UTC and exited 1 after 101.066 seconds.

Admission, graceful Rocky stop, native startup/readiness and setup passed.
On hfs-test0, root/direct passed 13 checks with two expected profile skips;
root/buffered passed 14 checks with its expected append skip. Together these
fixtures passed truncate, mmap, multiuser permissions, append, locking, sync,
normal unmount and mount-loss checks. They provide native evidence for the
truncate fix, but do not complete the three-guest matrix.

The third fixture, unprivileged/direct on hfs-test0, passed binding, namespace,
permissions and sync. Concurrent append failed after 8.0039 seconds waiting
for a helper response (`read |0: i/o timeout`). The subsequent lock case saw
the already broken helper client immediately; it is a cascading failure,
not evidence of a separate lock defect. Owned helpers stopped and the mount
unmounted normally. Remaining nine fixture calls were not started. Task `hy3`
investigates the timeout; no underlying syscall/deadlock cause was captured.

Original evidence collection then lost SSH during banner exchange (exit 255).
The runner correctly blocked cleanup and preserved an ambiguous report.
Fresh completed pinned host and guest0 inspections found no original collection
SSH/proxy/helper/controller operation and no active HomelabFS daemon/FUSE mount.
The declared run-owned datasets remained. The scoped local process check found
no run command; an unrelated SSH connection was left untouched. Collection was
explicitly reconciled as failed, preserving the original transport failure and
partial evidence. No collection pass or automatic uncertain retry was inferred.
Task `iy3` removes implicit SSH configuration from native transport; the exact
cause of this connection failure remains unknown.

Cleanup with that failed reconciliation exited 1 (`session 21930`, terminal
`b50817`) because the run remained unsuccessful. Normal native cleanup, guest
shutdown, Rocky restoration and pinned SSH/kernel restoration readiness all
passed. Final report: `complete=true`, `success=false`. The original ambiguous
JSON/XML reports and reconciliation evidence remain alongside the final report.
All thirty original artifact bytes were retained privately before helper edits.

Failed roots and datasets remain for inspection under the plan's 168-hour
retention. No forced operation, production change, distributed protocol test,
replication/failover or 9p operation was performed. Partial native passes are
not a full acceptance result.
