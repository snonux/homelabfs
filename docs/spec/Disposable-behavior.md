# Disposable native behavior command

Task `zw3` adds an explicit fixture composition command to the real daemon.
It uses `fsservice`, the FUSE adapter and the portable `behavior.Run` oracles;
there is no second filesystem engine. The separately built, SHA256-pinned
`hfs-behavior` executable runs its existing private helper protocol. Normal
publication and changing/arbitrary workload credentials remain denied. This
command is not approval for a test: every native lab run and VM lifecycle window
still needs the separate approval described in [Test automation](../Test-automation.md).

## Command contract

An approved run supplies three existing, empty, administratively exclusive roots,
mode 0700 and owned by the invoking root UID, plus an empty mountpoint with the
same ownership/mode. All paths are absolute, clean, symlink-free and disjoint.
The complete `control/node/admin-<36-byte-volume-UUID>.sock` path must also fit
native `RawSockaddrUnix.Path` including its trailing NUL (108 bytes on Linux,
104 on FreeBSD). The command predicts this exact path and rejects an overlong
`--control-root` before report creation or root initialization; it never relocates
the socket outside the enrolled control root. Use compact approved root paths.
The native script provisions distinct run-owned ZFS datasets first; the daemon
never provisions, imports, overlays or destroys datasets. Workload and helper
access to backing roots is prohibited. For one role/profile invocation:

```sh
homelabfs behavior --disposable-validation --initialize \
  --data-root /disposable/run/data \
  --journal-root /disposable/run/journal \
  --control-root /disposable/run/control \
  --mountpoint /disposable/run/mount \
  --behavior-binary /disposable/run/hfs-behavior \
  --behavior-sha256 EXACT_LOWERCASE_64_HEX_DIGEST \
  --profile hfs-direct --role unprivileged \
  --json /disposable/evidence/unprivileged-direct.json \
  --junit /disposable/evidence/unprivileged-direct.xml
```

Both roles require a root-owned daemon. `--role root` uses fixed syscall helpers
with UID 0, GID 0 and no supplementary groups. `--role unprivileged` uses UID
20001, GID 20001 and exactly supplementary group 20002 for **all three** syscall
helpers. These are kernel credentials installed before exec; no system account,
account lookup or inferred credential substitution is involved. The parent only
composes the service, prepares logical root permissions and verifies identity.
The unprivileged helper role does all namespace, permission, append, sync, mmap
and locking case I/O. The logical root is set to 0777 for that disposable role.
Root buffered mode sets it to 0755 and adds UID 20001/GID 20001/groups [20002]
and UID 20003/GID 20003/groups [] helpers for the separate multiuser/sticky case.
Root direct and unprivileged invocations explicitly skip that root-only case;
a complete matrix requires root buffered coverage as well.

The helper must be a singly linked regular executable owned by the invoking UID,
at most 128 MiB, with no group/other write permission. Its exact digest is checked
before each helper enrollment. The binary and its parent directories must remain
administratively exclusive and unchanged throughout the run. Stage the executable
mode 0555 with traversable ancestors (for example the run parent mode 0711) so the
fixed unprivileged helpers can exec it and reach the mountpoint. Keep all backing
roots mode 0700. The command never changes host FUSE rules or ancestor modes.

Each helper completes a private `ready` pipe handshake before native mounting.
The credential provider enrolls only the still-running parent and its unreaped
direct children; publication permanently seals further spawning. After mount,
`CheckMount` and each helper's bind independently verify the exact volume,
serving generation and connection session through pinned mounted directory fds.
A missing mount never redirects case I/O into backing directories.

Run `--profile hfs-direct` and `--profile hfs-buffered` separately for each role.
Direct covers append and explicitly skips mmap; buffered covers mmap and rejects
append before effects. Both use the same service and portable oracles. The union
is required for P01–P04. `UnprivilegedValidation` explicitly admits `allow_other`
for root-owned, bound/sealed participants on either profile. The existing
`MultiuserValidation` remains buffered-only. Default direct/self-client settings
still omit `allow_other` and reject external clients. No writeback negotiation,
normal publication, arbitrary credentials or cache-fencing authority is added.

## Actual root mapping and restart

The command writes identical private `fixture.json` records in all three roots,
binding canonical node/volume IDs and all absolute roots. The normal version 1
configuration schema is unchanged; this mapping is an internal fixture input.
The real initializer and recovery code use this layout:

```text
data/fixture.json
data/volume/volume.json
data/volume/reservations/...
data/volume/workspaces/<fresh-serving-generation>/data/...
journal/fixture.json
journal/volume/source/segments/...
journal/volume/workspaces/<same-generation>/journal/...
control/fixture.json
control/node/node.json
control/node/admin-<volume>.sock
control/node/admin-<volume>.audit.jsonl
control/workspaces/<same-generation>/progress/...
```

These are actual service roots, not unused dataset claims. Source history and
workspace redo journal use the journal dataset, workspace bytes/reservations use
data, and durable progress plus node/admin state use control. No nested fallback
`data/volume/source` or data-workspace journal/progress directories are created.

`--initialize` is exclusive: every root must be empty and partial initialization
is preserved on failure. Omit it to reopen the exact marked fixture and replay
its genesis history into fresh workspace directories and a fresh serving
identity. The command checks every marker and rejects a foreign root or changed
mapping. Old workspaces and failed attempts remain. A new role/profile may reuse
the same fixture only after the preceding command normally unmounted and closed;
use new evidence filenames. Never overlay a new dataset over existing bytes.

## Evidence and shutdown

JSON/JUnit outputs must be new files outside all three roots and the mountpoint.
Readiness stdout includes role, daemon UID, actual primary helper UID/GID/groups,
profile, exact roots, pinned helper digest, volume/generation/session and
`normal_publication:false`. Both report formats also carry fixture metadata and
the actual source/reservation/workspace root paths. Adapter-declared capabilities,
individual failures and explicit skips remain visible. Pre-mount setup/lifecycle
failures are failures, never an empty passing report. The lock-interruption
oracle observes real service lock-wait/cancel admission through a bounded
fixture callback instead of substituting a timing-only success.

The case runner has an overall 60-second context and per-case 8-second bounds;
prepublication handshakes and helper shutdown are separately bounded. Helpers
stop before a normal unmount. Each normal unmount attempt is bounded to eight
seconds; timeout is uncertain failure and retains the source writer, admin and
backing resources for inspection. A timed-out normal syscall may finish later;
there is no retry by the fixture, force/lazy unmount, FUSE abort or automatic
backing deletion. Mount construction may itself block in native kernel/mount
helper code; an approved supervising script must impose its own outer deadline
and preserve/inspect remaining resources on failure. No success cleanup deletes
the three roots. The script owns safe post-run collection and lifecycle work.

The CLI helper emits bounded `behavior-append` JSON lines on stderr around the
append case's open, each of sixteen 32-byte writes, fsync and close (at most 38
lines per writer). Each includes the helper PID/UID, A/B marker, write index
(`-1` for other operations), UTC timestamp, monotonic elapsed nanoseconds, phase
and any returned errno/error. The native supervisor retains these in the
fixture's `.stderr` evidence file independently of the reply pipe. A missing
`.end` localizes the last observed boundary around the syscall wrapper, including
its bounded EINTR retries; it does not prove the syscall remains blocked or
distinguish kernel, daemon, storage, scheduling or diagnostic-output delay.
Completed phases show whether progress continued across the case budget.
Diagnostic output failure fails the case. Reply failures identify the helper
index and whether send, receive or verification failed. The workload, oracle
and eight-second bound are unchanged.

This instrumentation follows N9's first unprivileged/direct append reply timeout
at 8.0039 seconds. Root/direct append passed in 2.7686 seconds; unprivileged
namespace and permission cases were already slower than their root counterparts.
Those observations establish neither deadlock nor the timeout's cause. Local
real-process FIFO checks distinguish blocked open from blocked write and retain
both helpers' progress; successful append still passes the exact-content oracle.
A new native run is required to diagnose N9 and validate any resulting fix.

Lock failures identify the stage, helper index and operation, distinguishing
initial acquisition, acquisition after helper 0's unlock, and acquisition after
helper 1's observed exit. Only a completed helper reply reporting unexpected
`fcntl-try` contention (native EAGAIN/EACCES) triggers one read-only `F_GETLK`
diagnostic through the same helper and existing case deadline. The failure
retains its original error/errno even if the query fails; no lock is retried,
unlocked or waived. Unknown send/receive outcomes trigger no query. A successful
query records the observer PID, native lock type, holder PID and range. This is
a later snapshot: an unlocked result does not disprove a prior conflict or
distinguish it from adapter request-capacity exhaustion. Controlled local
process tests check stage/holder evidence, preserved contention and command
counts; they do not identify the cause of N12's unlocalized EAGAIN or establish
native process-exit cleanup. That diagnosis and the full native gate remain open.

N13's first root/direct failure localized the unexpected conflict to helper 0's
acquisition after helper 1's observed exit; the later `F_GETLK` snapshot still
identified helper 1's PID as holder. Controlled participants now retire the exact
service process owner before their `Wait` returns, independently of native close
callback scheduling. Local real-child/adapter/service tests reproduce the stale
range without that notification and verify retirement, pending and late-request
rejection, other-owner preservation and retained description grants with it.
This establishes the ownership-contract correction, not the native kernel
reason a close callback failed to remove that range. Native validation of the
correction and the full behavior matrix remain separate gates.

## FreeBSD credential ABI

Controlled participants support only FreeBSD **15.1-RELEASE** (including patch
releases) on amd64. Other releases/architectures fail closed before publication.
The pinned primary reference is FreeBSD source commit
[`95719d8a895bf8170851e9844d9b4092b1d2b6fe`](https://github.com/freebsd/freebsd-src/tree/95719d8a895bf8170851e9844d9b4092b1d2b6fe).
The [`kinfo_proc` header](https://github.com/freebsd/freebsd-src/blob/95719d8a895bf8170851e9844d9b4092b1d2b6fe/sys/sys/user.h),
[amd64 size definition](https://github.com/freebsd/freebsd-src/blob/95719d8a895bf8170851e9844d9b4092b1d2b6fe/sys/amd64/include/proc.h)
and [kernel population code](https://github.com/freebsd/freebsd-src/blob/95719d8a895bf8170851e9844d9b4092b1d2b6fe/sys/kern/kern_proc.c)
define the decoded little-endian fields:

| Field | amd64 byte offset |
|---|---:|
| struct size / reserved layout | 0 / 4 |
| PID | 72 |
| effective / real / saved UID | 168 / 172 / 176 |
| real / saved GID | 180 / 184 |
| group count / group array | 188 / 192 |
| process start timeval seconds / microseconds | 336 / 344 |
| credential flags | 588 |

Only exactly 1088-byte records with size 1088/layout 0 are accepted. FreeBSD
15.1 places effective GID at `ki_groups[0]` and supplementary groups after it;
`ki_ngroups` counts both. Go 1.26.8 and x/sys v0.49.0 still use the
COMPAT14 group syscalls (79/80), which also carry effective GID first; the new
supplementary-only syscall numbers are 595/596. At parent enrollment the provider
validates and removes the legacy leading effective GID before comparing the
complete supplementary set. At helper exec it prepends the desired GID to a
separate credential copy, so legacy setgroups does not consume the first desired
supplementary group. Stored expected credentials stay supplementary-only; Linux
is unchanged. The pinned [compatibility implementations](https://github.com/freebsd/freebsd-src/blob/95719d8a895bf8170851e9844d9b4092b1d2b6fe/sys/kern/kern_prot.c)
and [syscall table](https://github.com/freebsd/freebsd-src/blob/95719d8a895bf8170851e9844d9b4092b1d2b6fe/sys/kern/syscalls.master)
define this boundary. Missing/wrong leading GID and changed syscall constants
fail closed. The provider rejects overflow, unknown credential flags,
incomplete/mutated groups, mismatched real/effective/saved IDs, malformed start
time and a changed process start time. It accepts at most 15 supplementary groups
rather than claiming the truncated array complete. The parent process is still
alive; direct children remain unreaped through snapshot and exit observation.
[`wait6(P_PID, WEXITED|WNOWAIT)`](https://github.com/freebsd/freebsd-src/blob/95719d8a895bf8170851e9844d9b4092b1d2b6fe/sys/sys/wait.h)
observes exit before credential admission is revoked and `exec.Cmd.Wait` performs
the sole reap. FreeBSD FUSE carries the process PID, not Linux task IDs. No PID
search enrolls unknown processes. x/sys v0.49.0 supplies sysctl and the native
syscall constants; it does not supply a FreeBSD `KinfoProc` Go structure.

Local byte-vector negatives, Linux participant lifecycle and real temporary-root
replay tests plus FreeBSD production/test compilation validate implementation
properties. They do not prove a FreeBSD kernel accepted the mount, mmap, groups,
lock interruption, process-exit cleanup or normal unmount. Task `yq3` supplies
that separate approved native runtime gate; no native success is claimed here.

The separately gated Linux counterpart is:

```sh
GOMAXPROCS=2 HOMELABFS_BEHAVIOR_CLI_INTEGRATION=1 \
  go test ./internal/daemoncli -run '^TestLinuxBehaviorCLIIntegration$' \
  -v -count=1 -timeout 8m
```

It requires an explicitly authorized root execution environment, builds both real
binaries, runs both roles/profiles, verifies exact identities/credentials, parses
both report formats and checks native non-FUSE status after each normal shutdown.
It preserves failures. This gate has only been compiled/skipped during the
unprivileged local implementation check; it has not produced a passing runtime
result yet. An approved ephemeral Linux CI run can supply that counterpart after
review, independently of the native FreeBSD gate.

The FreeBSD group normalization above belongs to the owning command's
`Participants` provider. The independent standalone `hfs-behavior` executable
spawner still needs the corresponding compatibility fix (task `2x3`) before
root raw-filesystem baseline evidence can be accepted. Native gate `yq3` depends
on that fix; using the binary as a pre-enrolled helper through this command does
not use its independent spawner.

The root member of the existing Linux FUSE CI matrix additionally invokes this
real-command gate through `tools/ci/run-linux-behavior-cli.sh`. It explicitly
passes Go's PATH and the runner module cache across sudo, uses isolated temporary
roots/build cache, retains Go JSON events with the existing evidence artifact,
and requires the exact parent test/package passes with no fail/skip event. The
runner-identity member and original kernel fixture/validator remain unchanged.
A configured hook is not a runtime result; acceptance needs the resulting run.
