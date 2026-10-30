# Restricted local FUSE adapter

Task `sq3` implements `internal/fuseadapter`, pinned to ADR 0001's
`github.com/hanwen/go-fuse/v2 v2.9.0`. Shared raw callbacks translate to `fsapi`;
Linux and FreeBSD files isolate native mount-identity calls and open flags. It
does not import `tools/poc`, expose backing paths, implement replication or wire
the daemon CLI. FreeBSD compilation is not native compatibility evidence.

## Admission and unsupported profiles

`New` accepts a real `*fsservice.Service`, a mandatory credential provider and
the exact matching named `ValidationProfile{Disposable:true}`. The service must
have the built-in Unix permission policy installed. A normal service cannot be
published by this adapter. `FrontendInfo` exposes immutable identity/configuration;
it does not establish current authority. Every workload operation still goes
through the service admission/apply/permission/completion gates.

The service's effective admitted I/O cap is exposed in `FrontendInfo.IOBytes`.
Public construction rejects a service admitting less than the fixed 64 KiB
native read/write request size, before attempting publication. The adapter does
not split a kernel read across different service state cuts. The existing
go-fuse `MaxWrite` setting governs both native read and write request limits.

`Service.AttachFrontend` permits one attached native frontend per generation.
The adapter's process identities belong to its random connection session; two
independent mounts cannot silently share correct POSIX process ownership. The
publication lease is released only after successful normal unmount, callback
drain and reference cleanup. An uncertain mount/cleanup error retains admission
and resources for inspection/recovery. Shared multi-mount process identities are
not implemented.

The default profile uses direct I/O for regular files, zero entry/attribute/negative
TTLs, mandatory `default_permissions` and `nosuid`, plus `nodev` on Linux.
FreeBSD's `mount_fusefs` rejects `nodev`; its
[option table](https://github.com/freebsd/freebsd-src/blob/releng/15.1/sbin/mount_fusefs/mount_fusefs.c)
accepts `default_permissions` and the
[standard `nosuid` flag](https://github.com/freebsd/freebsd-src/blob/releng/15.1/lib/libutil/mntopts.h).
Device nodes remain unavailable on both platforms: MKNOD returns ENOSYS without
service dispatch, CREATE always creates a regular file, and validated service
metadata supports only regular files, directories and symlinks, with no device
number. Adding special-file support requires a new device-safety review.
These source checks and local callback tests do not establish native runtime
compatibility; the failed N6 fixture stopped during mount setup before behavior
cases.

The profile enables neither writeback,
keep-cache, symlink caching, ACLs, ID mapping nor `allow_other`. READDIRPLUS is
disabled. `Capabilities` explicitly reports mmap, writeback, arbitrary workload
credentials, verified cached-ancestor search and normal publication as unsupported.
The Linux default-profile integration verifies mmap rejection; FreeBSD behavior is still a gate.
The separately admitted buffered disposable profile below supplies restricted local
validation, not the mmap-compatible normal canary profile required by Durability.md.

Future normal publication needs a reviewed native admission contract and actual
credential/traversal/cache/mmap evidence, as well as a service constructed with
the real external `Protocol`. Replacing the disposable profile with a flag, a
fake partner acknowledgement or kernel permission checks cannot supply them.

## Credentials and advisory owners

`Credentials.Snapshot` must bind the actual kernel request to complete numeric
credentials and a nonreused process-incarnation token. UID/GID/PID must exactly
match the request header; unknown credentials, incomplete snapshots, over 1024
groups, zero process identities and inconsistent process leaders fail closed.
The provider owns identity authentication and must handle descriptor transfer and
process/thread incarnation correctly. Returned credentials are cloned into the
immutable request; daemon identity and account-name lookup are never fallback
authorization. No arbitrary-workload provider is implemented.

Linux request headers use a task ID and filesystem UID/GID. The controlled Linux
test checks `/proc/<tid>/status` belongs to its own still-running process and
reads that thread's filesystem IDs/groups. Its credentials remain fixed during
the test. This deliberately restricted fixture does not solve the arbitrary
request-versus-`/proc` snapshot race. See the reviewed upstream
[Linux v6.18 request construction](https://github.com/torvalds/linux/blob/v6.18/fs/fuse/dev.c).

The kernel POSIX owner token is independently bound to the authenticated process
incarnation, retained without reuse and bounded. A different process reusing a
token is stale; file-table replacement within a process and file-table sharing
across independent processes are rejected by this initial contract. The provider
process token becomes the generation/session-scoped `fsapi.LockOwner`; PID alone
never names a service lock. The final RELEASE owner field is not interpreted as
a POSIX owner: on Linux it can instead describe a flock open description. The
reviewed [Linux v6.18 file callbacks](https://github.com/torvalds/linux/blob/v6.18/fs/fuse/file.c)
also explain why RELEASE can carry no caller credentials. FreeBSD owner/PID,
fork/dup/transfer and process-exit behavior still require native validation.

## Request and reference ownership

Each raw callback returns exactly once; go-fuse owns serialization of that return
to the kernel. Admission snapshots command buffers and credentials. Cancellation
before admission returns EINTR without a service effect. An ordinary admitted
command is awaited to its definitive result even if interrupted; this preserves
newly acquired lookup/open references and does not imply mutation rollback.
Protocol hooks must impose their own bounded deadlines. This is not a promise of
immediate post-admission interruption. Disconnect can still prevent delivery;
unmount drains retained obligations after the binding exits all callbacks.

Node IDs are mount-local, never reused and bound to exact service ObjectIDs and
generation. Successful LOOKUP/CREATE/MKDIR/SYMLINK acquire the documented service
lookup references. LINK's service command acquires none, so the adapter performs
a fresh Lookup and verifies the exact original ObjectID before returning it. A
raced replacement is forgotten and returns stale; an error never promises the
preceding committed link was rolled back. Plain READDIR grants no lookup refs.
FORGET decrements its explicit count independently of handles. Capacity rejection
after a successful create drains its new lookup/description without reverting
the namespace mutation.

Each native open description maps to its issuing service handle and generation,
independently of later rename/unlink/chmod. Wrong node/description/generation
references are stale. Directory cookies retain raw exclusive-name cursors and
the issuing directory revision; changed revision is stale. Rewind starts fresh.

`MaxNodes` bounds each retained per-generation node/process/owner identity map,
including forgotten objects and exited processes, rather than only active
references. Churn can therefore exhaust a map while active use is small;
exhaustion fails explicitly without eviction or identity reuse. It is a separate
bound for each map, not a shared total budget.

FLUSH sends credential-bound `DescriptorClose` for close-any-fd POSIX cleanup;
it adds no sync promise and does not close the shared description or flock. This
cleanup waits independently of request interruption. Failed cleanup closes the
adapter gate. Final kernel RELEASE/RELEASEDIR uses `Release{DescriptionOnly:true}`:
it drops the handle, flock and waits on that handle, without repeating POSIX
process cleanup. This matters when the process reacquires locks on another fd
between FLUSH and the asynchronous final RELEASE. The legacy combined service
Release remains available to callers that did not send a separate close event.

Controlled `Participants` additionally reports positively observed child exit to
the exact adapter session. `Wait` revokes that incarnation's request admission,
performs the sole reap, and awaits `ProcessExit` before returning. This retires
only its POSIX ranges and pending requests, including requests authenticated
before exit but dispatched later. It preserves other processes' locks, shared
descriptions, flock and retained cache-write grants. Notification failure closes
adapter admission and remains an error even alongside expected SIGTERM exit;
notification cannot prevent the child from being reaped. This supplements native
close callbacks without assuming their scheduling or repeating POSIX cleanup on
asynchronous RELEASE.

FORGET, final RELEASE and connection SessionExit are kernel control events with
no workload credentials. The private cleanup path permits only these operations;
the Unix policy authorizes reference draining independently of UID. Its empty
credential metadata does not stand in for any workload requester. Errors from
callbacks without an error reply close admission and remain visible through
`Adapter.Err`. Shutdown retires the session, drains descriptions and then lookup
counts; failed resources/pins remain retained for inspection.

POSIX/flock acquisition forwards to the service's shared object domain. Native
FUSE ranges are already normalized; the inclusive signed OFF_MAX sentinel means
length zero. Blocking acquisition uses generation-unique WaitID and explicitly
orders CancelLock against the original completion. Both results are awaited;
an acquisition that won cancellation returns success rather than leaving an
unnoticed lock. Control admission retries a full queue for at most five seconds;
failure closes the adapter gate and retains uncertain resources.

## Metadata, durability and identity

All semantic errno values map explicitly to native numbers; unknown values map
to EIO and unavailable service gates map to EHOSTDOWN. Reads own returned bytes;
writes cannot outlive borrowed kernel buffers. File/data/directory sync map to
their service durability requests. Close/release adds no data barrier. Cache TTLs
do not replace service authority or ancestor search checks.

Native setattr maps size, mode, ownership and explicit/now timestamps. Size with
atime/mtime (explicit or now) and explicit ctime maps to one service `Truncate`
with owned `TruncateTimes`, producing one exact size/metadata afterimage. Size
combined with mode/UID/GID, ctime without size, unknown fields (including Linux
`FATTR_KILL_SUIDGID`) and rename exchange/whiteout fail explicitly before effect.
The service still clears set-ID bits on non-root truncation. Ordinary Linux
ftruncate shrink/extend and explicit
timestamps were exercised successfully. ACCESS is delegated to mandatory kernel
`default_permissions`; if forwarded, it returns EOPNOTSUPP because the component
API lacks one coherent atomic access-check command. Unsupported native operations
retain the binding's ENOSYS behavior; no fake successful xattr, fallocate or ioctl
implementation is provided.

FreeBSD 15.1's
[`fuse_internal_setattr`](https://github.com/freebsd/freebsd-src/blob/releng/15.1/sys/fs/fuse/fuse_internal.c#L1077)
combines size and the writable handle with dirty cached atime/mtime and, at
protocol 7.23 or newer, ctime. The
[`fuse_vnode_update` path](https://github.com/freebsd/freebsd-src/blob/releng/15.1/sys/fs/fuse/fuse_node.c#L539)
retains these kernel timestamps until a successful setattr clears them.
Pinned go-fuse v2.9.0
[negotiates 7.28 on FreeBSD](https://github.com/hanwen/go-fuse/blob/v2.9.0/fuse/request_freebsd.go)
and [forwards the unchanged request](https://github.com/hanwen/go-fuse/blob/v2.9.0/fuse/opcode.go#L199).
N7 reached the namespace case and failed at ftruncate after reading an open,
unlinked file. The kernel source explains why that sequence can produce the
previously rejected combined request; N7 captured no packet mask. Focused local
callback tests reproduce both size/atime and size/atime/mtime/ctime requests,
including open-unlink shrink/zero-filled extension, one service dispatch and
rejection-before-effect. Exact metadata survives fresh-workspace service replay.
These tests establish the local fix, not a successful native rerun.

The mounted root exposes only the immutable `user.homelabfs.identity` xattr with
volume, generation and connection session, behind a service authority check.
`CheckMount` opens the directory without following a final symlink, checks native
statfs type and reads that identity through the same fd. Linux requires the FUSE
magic; FreeBSD checks `fusefs.homelabf` and uses the user extattr namespace. The
FreeBSD xattr/type path is compiled, not runtime-verified. Consumers must check
identity before publishing/accessing a mount path; this does not fence future
unmounts or replace the separate consumer-lifecycle protocol.

## Local evidence, 2026-10-29

Focused callback tests exercise raw bytes, permissions, link reference counts,
open-unlink/chmod, generation rejection, capacity cleanup, credential/owner
negatives, cancellation before/after admission, both lock cancellation outcomes,
per-fd versus final close, revisioned directories and unmount reference drain.

The opt-in Linux integration command is
`GOMAXPROCS=2 HOMELABFS_FUSE_INTEGRATION=1 go test ./internal/fuseadapter -run '^TestLinuxIntegration$' -v -count=1 -timeout 120s`.
It creates fresh run-owned storage and mount roots, limits logical synthetic data
to 4 MiB, normally unmounts and removes successful resources only. It skips with
the exact error if `/dev/fuse` cannot be opened. Running it requires authorization;
the environment variable is an opt-in mechanism, never future approval.

The first run exposed missing caller credentials on asynchronous RELEASE. Its
mount was normally removed and all run-owned evidence is retained at
`/home/paul/tmp/codex/homelabfs-fuse-validation-1627283030` (underlying filesystem
verified as btrfs after unmount, 132 KiB retained). The correction uses
description-only final release.
The second run passed in 0.54 s and removed its resources. A third bounded run
passed in 0.46 s, adding actual sparse extend/shrink, explicit nanosecond timestamps
and conflicting flock descriptions; resources were removed. The suite also checks
create/read/write/link/rename/chmod, actual kernel execute denial, file/directory
fsync, plain readdir, open-unlink, mmap rejection and mounted/wrong/unmounted
identity. A final 0.54 s run on Linux `7.2.5-200.fc44.x86_64` validated the added
exclusive publication lease, rejection while mounted and successful admission
after normal unmount; its resources were removed. These are restricted local
service results, not native FreeBSD, arbitrary
multiuser credentials, writable mmap, replicated durability, failover or 9p evidence.

## Controlled buffered validation (`xw3`)

An embedding disposable harness may select `Config.BufferedValidation` with a
matching real service and `*Participants`. This removes `FOPEN_DIRECT_IO` only
for this explicit profile; normal publication remains denied. The normal CLI `mount`/`serve` commands retain their existing direct-I/O
self-client; the explicit [disposable behavior command](Disposable-behavior.md)
composes these controlled profiles. No writeback-cache capability,
keep-cache, positive TTL, external mutation or owner-change/cache-fencing
contract is added. All mutations use one service and the same FUSE connection.
FreeBSD runtime validation remains a separate approved gate; the portable
`vq3` report runner is shared by the disposable fixture.

A successful authenticated CREATE/OPEN retains the exact issuing credentials on
its service description, object and generation. Only kernel `WRITE_CACHE`
requests may use that issued writable grant without current workload credentials;
ordinary reads/writes still require an authenticated participant. Missing,
read-only, released, wrong-node or wrong-generation descriptions fail closed.
The service's current admission, handle, permission and completion gates still
run; daemon credentials and guessed groups never substitute for authorization.
Dirty mmap stores alone have no journal guarantee. Successful `MS_SYNC` causes
actual cache writes through the service; successful fsync supplies its explicit
local durability barrier. Close alone adds none. The disposable service still
claims no partner protection or power-loss/failover durability.

Linux may choose a writable description other than the mapping's source fd for
cache writeback. Therefore one object may have only one writer authorization set
(UID, GID and complete supplementary groups) for the entire connection, including
after all descriptions close. Different enrolled processes with the same set are
allowed; mixed sets return unsupported. Such OPEN requests are rejected before
truncate; buffered nonexclusive CREATE with truncation is unsupported. These are
explicit restrictions, not general multiuser cached-I/O compatibility.
Buffered OPEN/CREATE with `O_APPEND` is unsupported, including `O_TRUNC` opens
and exclusive creates; rejection occurs before any service dispatch. Otherwise
Linux could select an append description for an mmap cache write and the service
would incorrectly move its page offset to EOF. The default direct-I/O profile
retains its append semantics. Existing
stable-ID handles continue to authorize I/O after chmod/unlink. The kernel may
retain a description and its flock through a live mapping after all fds close.

`Participants` binds once to the exact volume/generation/session. It registers
only its own fixed credentials and trusted helpers it directly spawns, retains
each child unreaped, and reads Linux task credentials through retained process
directories. On supported FreeBSD 15.1 amd64 it verifies the pinned process-start
and complete credential ABI described in the [fixture contract](Disposable-behavior.md).
Every request must match all real/effective/saved/filesystem IDs
and the exact group set; unknown, retired or mutated identities are denied.
Process tokens never reuse, and at most 64 process incarnations are admitted.
Helpers must never change credentials, exec again, share file tables or transfer
fds outside this fixture. This trust requirement is why proc snapshots suffice
here; it does not solve arbitrary mutable workload credential races.

All helpers must be spawned idle **before mounting**. Publication seals further
spawning: pre-exec inherited-FUSE-fd cleanup can otherwise issue a child FLUSH
before `exec.Cmd.Start` returns a PID. Helpers use private pipes for ordinary
fixture commands, without an HFS daemon readiness protocol. `Wait` observes child
exit with `WNOWAIT`, revokes it, then performs the sole reap. Helpers open the
exact mount identity with `OpenMount` and use `openat` through the returned pinned
directory fd. An absent/unmounted mount is refused; no raw-backing/empty-directory
fallback exists. Mmap accesses run in helpers, because a Go daemon accessing its
own FUSE mmap can deadlock its runtime GC while waiting for its own page-fault
callback.

`MultiuserValidation` additionally requires root and buffered validation and explicitly enables
`allow_other` for synthetic fixed numeric users/groups. Each backing root remains
private; no package, account or FUSE system configuration is changed. Unprivileged
runs skip that separate case with a reason. `UnprivilegedValidation` separately
allows root-owned fixed unprivileged helper fixtures on both direct and buffered
profiles; it requires the same exact connection binding and publication seal.
FreeBSD runtime multiuser compatibility remains unverified. A skipped case is not
evidence of permission compatibility.

The opt-in focused command is:

```
GOMAXPROCS=2 HOMELABFS_FUSE_INTEGRATION=1 go test ./internal/fuseadapter -run '^TestLinux(BufferedIntegration|Integration|MultiuserBufferedIntegration)$' -v -count=1 -timeout 35s
```

It requires authorization, uses fresh private run-owned roots and at most 4 MiB
of logical synthetic file data. The buffered case checks actual shared writable
mmap/MS_SYNC/fsync, fresh-root/generation replay, alias read/write and truncate
cache coherence, separate-process fcntl/flock, kernel cancellation after process
interruption, close-any-fd POSIX cleanup, dup/mapping flock lifetime and mmap
writes after fd close. It also refuses an empty fallback after normal unmount.
Default direct-I/O mmap rejection and normal-publication denial remain regression
checks. This is local Linux service evidence only; native FreeBSD, arbitrary
credentials, external mutation, fencing, replication and failover remain gates.

During development, run `1158590810` deadlocked on pre-exec inherited-fd cleanup.
Its exact run-owned connection 84 was positively verified, explicitly aborted
for failed-run recovery and then normally unmounted. This was **not** a passing
normal lifecycle. Run `2924589301` exposed daemon-self-mmap/GC deadlock and was
terminated for diagnostics, then normally unmounted. Run `3701936258` failed an
incorrect flock-lifetime assertion and was normally unmounted. All three retain
their disk evidence beneath `/home/paul/tmp/codex/homelabfs-buffered-validation-*`.
The corrected buffered run passed in 0.638 seconds. A combined non-race focused
run passed buffered validation in 0.64 seconds and default direct-I/O regression
in 0.53 seconds (1.167 seconds total); root-only multiuser validation explicitly
skipped under UID 1001. Successful resources were removed after normal unmount.
Focused race/unit, vet, errcheck and FreeBSD amd64 test compilation passed. The
final real-kernel run with the race detector also passed: buffered 4.81 seconds,
default direct I/O 3.08 seconds, total 8.895 seconds; multiuser again explicitly
skipped. All successful resources were removed; no HomelabFS mounts remain. No lab, VM, infrastructure or production action
was involved.
