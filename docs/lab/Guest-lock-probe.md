# Same-mount guest advisory lock probe

[`guest-lock-probe.py`](../../tools/lab/guest-lock-probe.py) implements the bounded
component for task `4z3`. It tests cooperating processes in one guest using one
retained 9p mount and the same kernel inode domain, as accepted in
[Platform interfaces](../Platform-interfaces.md#accepted-9p-lock-scope-2026-10-30).
It does not test independent mounts, other guests, direct host access, native
FUSE locks, or distributed filesystem locks. It imports no PoC or Stage 2 runner.

A passing result establishes advisory lock behavior only. Exact 9p mount/source
observation cannot identify the daemon, volume, generation, session, or exclusive
publication. `publication_identity_verified` and `security_admission_verified`
remain false, and `parent_guard_oracle_required` remains true, on success too.
The future `zq3` parent driver must provide an independent live-publication guard
and oracle, identity/security admission, old-consumer fencing, metadata and sync
gates. The [retained authorization](Retained-9p-authorization.md) decision (`3z3`)
remains pending; this fixture changes no guest trust policy or export permission.
See also [preparation](Real-daemon-9p-preparation.md),
[sync boundaries](Retained-9p-sync.md), [deployment tests](../Deployment-tests.md)
and [adapter lifetimes](../spec/FUSE-adapter.md).

## Entry and evidence

The production CLI accepts no options, including no local-filesystem override.
It reads one UTF-8 JSON object on standard input, limited to 8192 bytes:

```json
{
  "protocol": "hfs-guest-lock-v1",
  "run_id": "APPROVED_UNIQUE_RUN_ID",
  "mountpoint": "/mnt/EXACT_EXISTING_9P_MOUNT",
  "tag": "EXACT_EXISTING_9P_SOURCE_TAG",
  "mount_id": 123,
  "budgets": {"overall_seconds": 30, "case_seconds": 3}
}
```

This is a schema example, not an executable plan or approval. A future parent
must obtain fresh approval under [test automation](../Test-automation.md) and
[lab operations](../Lab-operations.md) before invoking it inside Rocky. The probe
does not mount, unmount, SSH, install packages, manage VMs, or select targets.
No live 9p or HomelabFS pass is established by implementing this component.

Initial support is Linux x86_64 (the Rocky profile). Native `struct flock` size
and offsets are checked explicitly before F_GETLK. Other ABIs fail closed.
Entry requires a canonical absolute path with no symlink, an exact `/proc/self/mountinfo`
9p mountpoint/source/mount ID match, matching retained-root-fd `mnt_id`, a repeated
mountinfo check, and matching retained/path inode identity before creating data.
No parent directory is created. A fresh `hfs-lock-RUN_ID` leaf is created exclusively
using that directory descriptor; all data opens are relative and reject symlinks.
Existing leaves or files are never adopted, deleted, or truncated. Both success
and failure preserve the small run leaf for inspection and later approved cleanup.

The single stdout JSON report (at most 64 KiB) and exit status are terminal evidence:
zero requires every case and clean child reaping. A failed/incomplete protocol,
unexpected errno, unsupported kernel observation, nonzero child status, deadline,
or cleanup uncertainty fails the run. Events include child PID, independently
opened device/inode identity, GETLK type/range/PID, errno, kernel wait evidence,
acquisition after release, and per-child cleanup status. `scope` distinguishes
`same-guest-same-9p-mount` from imported `local-selftest`; the latter never claims
9p transport observation.

## Cases and bounds

The fixture uses two empty files (zero logical payload), four total forked children,
at most two concurrent children and at most twelve owned descriptors (including
transient socket pairs and duplicate descriptors). Each child closes inherited data descriptors
before independently opening its case file. Child commands and replies have fixed
small bounds; the report and `/proc/locks` reads are bounded. The standalone
entry reserves two seconds from its 60-second ceiling for cleanup; the requested
overall budget is 5–60 seconds including stdin, admission and cleanup. A budget-aware
work alarm leaves that reservation before bounded cleanup starts. Each case/handshake deadline is 1–5
seconds; cleanup shares at most 1.5 seconds for normal TERM and nonblocking reaping.
SIGALRM, SIGTERM and SIGINT are masked only across fork/PID registration and
waitpid/reap bookkeeping; child handlers are reset and the prior mask restored
before work, with SIGTERM unblocked for supervised cleanup. There is no SIGKILL, forced
unmount, or VM lifecycle action. Kernel operations stuck
in uninterruptible I/O cannot be guaranteed to terminate by a userspace deadline;
a parent must retain uncertainty and resources if its own external deadline fires.

The POSIX cases prove overlapping nonblocking conflict, disjoint-range success,
GETLK conflicting write type/byte range/holder PID, blocking wait and wakeup,
close-any-fd process-lock release, and holder process-exit cleanup. The flock cases
prove shared/shared success and exclusive conflict, nonblocking conflict, blocking
wait and wakeup, duplicate-description retention across the original descriptor
close, retention across an unrelated open/close, final-description-close release,
and a separately reacquired lock's process-exit cleanup.

Blocking checks first receive a child intent handshake, then require a matching
owned PID/device/inode/write range waiter in `/proc/locks` before releasing the
holder. An intent event or elapsed delay alone cannot pass. The observed lock
representation is recorded: flock may appear as FLOCK locally or POSIX in Linux's
9p dotl conversion; either must match the whole-file range. POSIX requires its
exact eight-byte range. Missing or inaccessible kernel wait evidence fails closed.
Only acquisition following the release completes the blocking check.

## Local verification

[`test_guest_lock_probe.py`](../../tools/lab/test_guest_lock_probe.py) imports the
core against temporary local directories without a CLI bypass. With separately
authorized local verification, the focused command from the repository root is:

```sh
timeout 45s python3 -B -m unittest discover -s tools/lab -p test_guest_lock_probe.py -v
```

These tests use actual OS locks and processes, plus negative controls for no-op
locking, falsely unlocked GETLK, malformed/incomplete child protocol, nonzero exit,
timeout, missing kernel wait evidence, cleanup failure, actual deferred signals
after fork and successful waitpid, exact mount/source/ID
mismatch, invalid or missing paths, symlinks and preexisting run leaves. Negative
controls must fail while reaping their children and preserving run resources.
The suite runs sequentially, uses temporary directories, and does no remote I/O.
Its results verify the local fixture and failure detection, not live 9p behavior,
publication, security, sync, replication, or production admission.
