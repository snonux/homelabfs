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

The profile uses direct I/O for regular files, zero entry/attribute/negative
TTLs, `default_permissions`, `nosuid` and `nodev`. It enables neither writeback,
keep-cache, symlink caching, ACLs, ID mapping nor `allow_other`. READDIRPLUS is
disabled. `Capabilities` explicitly reports mmap, writeback, arbitrary workload
credentials, verified cached-ancestor search and normal publication as unsupported.
The Linux integration verifies mmap rejection; FreeBSD behavior is still a gate.
This is not the mmap-compatible canary profile required by Durability.md.

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

Native setattr maps size, mode, ownership and explicit/now timestamps. A combined
size plus metadata request is rejected before any effect because the component
API cannot atomically express it. Unsupported fields and rename exchange/whiteout
are rejected explicitly. Ordinary Linux ftruncate shrink/extend and explicit
timestamps were exercised successfully. ACCESS is delegated to mandatory kernel
`default_permissions`; if forwarded, it returns EOPNOTSUPP because the component
API lacks one coherent atomic access-check command. Unsupported native operations
retain the binding's ENOSYS behavior; no fake successful xattr, fallocate or ioctl
implementation is provided.

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
