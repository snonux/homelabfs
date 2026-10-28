# Local administrative socket and status

Task `uq3` implements `internal/admin` and `homelabfs status --config FILE`
for the disposable CLI profile. This interface does not admit normal publication,
cluster recovery, emergency activation, membership changes, fencing, host/VM
lifecycle, history loss or snapshot restoration. Unrecognized control operations
are denied and audited, including requests from the daemon owner.

## Socket and authentication

Each successfully reconstructed local volume has a separate socket at
`<control_dir>/admin-<volume_id>.sock`. The control directory must be a real,
private mode 0700 directory owned by the effective daemon UID; the socket is mode
0600 and owned by that UID. Ancestor symlinks are refused. Existing socket paths
are preserved and refused, including stale sockets after a crash. Inspect a stale
path and confirm no daemon owns it before an administrator removes it. No stale
socket recovery or automatic deletion is implemented. Keep the absolute socket
path within the platform's Unix socket pathname limit (typically 104 bytes on
FreeBSD and 108 bytes on Linux, including the terminating NUL).

Both server and client inspect credentials on the actual connected socket.
Linux uses `SO_PEERCRED` (UID, GID and PID); FreeBSD uses
`SOL_LOCAL/LOCAL_PEERCRED`, verifies the credential version and reads UID/GID.
This is the same kernel operation used by
[FreeBSD libc getpeereid](https://raw.githubusercontent.com/freebsd/freebsd-src/main/lib/libc/gen/getpeereid.c),
without requiring cgo. FreeBSD PID is absent rather than guessed. A JSON caller
identity, process name, requested volume or filesystem UID is never trusted as
administrative authentication. Only the daemon's effective UID is admitted;
root is not an implicit alternate identity for a nonroot daemon.

Read, control and recovery are distinct authorization levels. An embedding
policy can further restrict the daemon UID for each level, but cannot expand
access to other UIDs. The disposable CLI admits owner status and, only when
explicitly configured, its bounded test hook. Recovery authority remains
unimplemented and denied for every UID and policy. File access and administrative
access are separate: the disposable data plane rejects other processes, including
same-UID clients, while its authenticated same-UID status command is allowed.

## Bounded protocol and audit

One connection carries one UTF-8, newline-terminated JSON request and one JSON
reply. Protocol version is 1; a frame is at most 16 KiB. Exact case-sensitive
field names are `version`, `operation`, `fault` and `enabled`. Duplicate/unknown
fields, null values, unsupported versions, trailing JSON and malformed inputs
are refused. Status accepts no control fields. A fault toggle requires both a
fault name and an explicit boolean, including `false` when disabling it.

The listener permits at most 16 concurrent requests and applies two-second
socket deadlines; extra connections close immediately. Status reads an immutable
in-memory snapshot and never queues behind filesystem work. Control callbacks
are serialized and must honor their context. Local audit fsync and underlying
storage operations retain their native kernel I/O behavior; a socket timeout
alone does not cancel an already admitted control effect. After a lost reply,
inspect status/audit before retrying. There is no automatic retry of controls.

Control requests, including unsupported and unauthorized requests, produce an
audit event containing UTC time, observed peer identity, node/volume IDs,
serving generation, operation, fault arguments and outcome. Permitted hooks
first write a `requested` event, perform the toggle, then write an `applied` or
`rejected` event. A crash can leave a requested intent without a completion; it
is not proof of application or rollback. Successful controls require both events
to be synced. Failed intent logging denies the effect. A completion-log failure
returns uncertainty explicitly after the effect may have occurred.

The audit is `<control_dir>/admin-<volume_id>.audit.jsonl`, an owned, mode 0600,
singly linked regular file opened without following the final symlink. Records
append and sync; initial creation also syncs the parent directory. The file is
bounded to 4 MiB. Capacity, stat, write, short-write or sync failure permanently
poisons that open audit handle and denies every later control, even if the
underlying I/O subsequently recovers. The existing listener's status remains
available. No later append can hide an uncertain partial record behind a
successful control. Context cancellation before I/O does not poison the handle.

On open/restart, the bounded existing file must contain complete newline-terminated
UTF-8 JSON Event objects, one per line. Incomplete/corrupt records are refused
without truncation or repair, preserving the original evidence. This causes
CLI startup to fail before opening its status listener or mounting. A valid,
complete file can be reopened on a separately authorized restart after a prior
handle failure; an existing requested intent without completion remains uncertain.
This does not prove that a prior failed sync was durable or that its effect ran.
No automatic truncation, retention or rotation is supplied.

Listener shutdown drains requests and removes only its unchanged socket, then
closes the audit before backing resources are released. Mount shutdown uses normal
unmount followed by adapter drain. An uncertain
native mount or failed unmount permanently closes workload admission and retains
the admin endpoint, backing roots and volume lock in-process for inspection.
Worker state publication cannot reopen that gate. No forced cleanup, background
retry or source ownership release follows uncertainty. Admission stop is not a
drain or fence: already running work and parked lock waits can retain their
resources until the worker wakes; it does not cancel or resolve them. An embedded
`Run` caller must preserve the process for inspection or explicitly end it.

## Status semantics

`homelabfs status --config FILE` returns JSON with the node ID, a single local
volume, whether test hooks are enabled, and enabled test fault names. The daemon
serves only its configured volume. The client verifies the returned node/volume
against the private configuration and node manifest.

Status configuration admission retains exact schema, UUID, private file, lexical
absolute/clean path and pairwise nonoverlap checks. It inspects only the control
path/node identity and socket; it does not stat/open the volume backing path or
mountpoint. This exception is exclusive to read-only status. It permits status
from a separate process while the restricted active FUSE mount rejects that
process. Initialization, inspection and serving retain their full path checks.

Per-volume observations include:

- `state`: `RECOVERING`, `SERVING` or `BLOCKED`, from the local service gate.
- `wait_reason`: the local phase, such as `local-journal-flush`,
  `partner-protection`, `local-apply`, `authority-check` or a blocked reason.
- `source_position`: the last journal record observed/authored by this service,
  with volume, history generation, configuration, epoch and sequence. It may be
  unflushed/unprotected and is not a commit, durability or replica receipt.
- `applied_position`: the actual locally applied source record identity.
- `backing_slots`: the separate contiguous redo-slot count. It is not a source
  sequence, replicated position or client durability promise.
- `sync_position` and `async_position`: JSON `null`. Replica progress is unknown;
  the fixture peer IDs do not establish real partner progress.
- `profile`: the explicit disposable self-client profile, and
  `normal_publication:false`, even when the local gate says `SERVING`.

Snapshots are atomically published by the service worker. Readers never access
its mutable selection/catalog directly. A long partner wait therefore preserves
last applied progress and reports the wait, without blocking status. Snapshot
publication is an observation, not distributed authority or a workload readiness
check. Startup publishes the listener only after successful local reconstruction;
failed replay preserves resources and returns an error without a status socket.
No replica source, lag estimate, remaining bytes or distributed recovery promise
is fabricated when that subsystem is absent.

## Explicit test fault

The optional version-1 configuration field `enable_test_hooks` defaults to
`false`. Only the exact boolean `true` installs the test hook. There is no
environment or command-line bypass. A protocol client can send:

```json
{"version":1,"operation":"test-fault","fault":"journal-before-sync","enabled":true}
```

`journal-before-sync` returns an injected error at the existing journal
`before-sync` boundary. It neither kills a process nor modifies/deletes history.
An affected accepted operation fails and leaves the service `BLOCKED`, preserving
uncertain state. Disabling the toggle stops future injections; it does not reopen
the gate, repair history or establish the outcome of the failed operation.
Recovery requires the existing separately authorized restart/inspection process.
Faults reset to disabled on each process start, even when hooks are configured.
No shell/host/VM/fence/loss/restore hook or generic failpoint execution is exposed.

## Validation boundaries

The opt-in local Linux CLI integration passed on 2026-10-29 in 1.22 seconds,
including separate-process status while mounted, refusal of a foreign data-plane
client, replay and normal SIGTERM unmount. No lab/VM/host lifecycle operation ran.

Focused local tests use actual Unix sockets and kernel peer credentials, verify
read/control denial and unsupported-operation audit, malformed/oversized requests,
connection/deadline bounds, private socket/audit files, sticky audit I/O errors,
partial/corrupt audit reopen refusal without repair, audit capacity, default
disabled hooks, actual journal failure and blocked status, separate-process
status and snapshots during a waiting protocol. Race checks exercise simultaneous
status and control plus worker progress. FreeBSD compilation validates the pure
Go adapter shape, not native credential or FUSE runtime behavior. The opt-in
Linux CLI integration also requests status while the self-client mount is active;
native FreeBSD, WireGuard, replicas, recovery/failover and production publication
remain separate gates.
