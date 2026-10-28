# ADR 0001: Go FUSE binding

Date: 2026-10-28. Status: **selected for initial adapters; independent review passed**.
Task: `hq3`. The corrected Linux comparison completed; native FreeBSD capability
gates below remain open and limit compatibility claims.

## Decision

Select **`github.com/hanwen/go-fuse/v2 v2.9.0`** for both platform adapters,
with the native FreeBSD gates below still required. Keep binding
types inside adapters; the shared filesystem contract owns cancellation,
identity, locking and freshness requirements. Do not import the existing
throwaway PoC. Selecting a binding does not implement HomelabFS.

The comparison is against **`github.com/winfsp/cgofuse v1.6.0`**, its current
tagged module release resolved on this date. Its default is FUSE2. Both ABI
variants build; the local FUSE3 runtime attempt failed because the pinned
loader requires `libfuse3.so.3` and this workstation provides `.so.4`.
Use the supported FUSE2 runtime for the corrected comparison, recording the
FUSE3 compatibility limitation. Pins obtained through the Go module proxy/checksum
mechanism and inspected locally:

| Binding | Module origin commit | Module checksum |
|---|---|---|
| go-fuse v2.9.0 | `3183e469f466607a92dd8c2cb44f966d7d69993b` | `h1:0AOGUkHtbOVeyGLr0tXupiid1Vg7QB7M6YUcdmVdC58=` |
| cgofuse v1.6.0 | `b1cdd03841f6da5ac8ec4f63001b3dcd23f3228f` | `h1:re3W+HTd0hj4fISPBqfsrwyvPFpzqhDu8doJ9nOPDB0=` |

Local source was read from the corresponding immutable Go module-cache
directories. Sources below identify tagged upstream files and named APIs so
claims can be checked against those pins.

## Criteria and evidence

| Criterion | go-fuse v2.9.0 | cgofuse v1.6.0 |
|---|---|---|
| FreeBSD implementation | Has explicit FreeBSD mount, request, constants and attribute-layout files. `mount_freebsd.go` opens `/dev/fuse`, invokes `/sbin/mount_fusefs`, and unmounts via the OS. Linux direct-mount options are ignored on FreeBSD. [Mount source](https://github.com/hanwen/go-fuse/blob/v2.9.0/fuse/mount_freebsd.go). Existing native smoke evidence is below. | Has explicit FreeBSD C build flags for FUSE2 and FUSE3. The upstream build guide requires FreeBSD fusefs libraries; this is source/build support, not local runtime evidence. [C bridge](https://github.com/winfsp/cgofuse/blob/v1.6.0/fuse/host_cgo.go), [pinned build guide](https://github.com/winfsp/cgofuse/blob/v1.6.0/README.md). No cgofuse FreeBSD probe has run here. |
| Interrupt/cancellation | Raw callbacks receive a cancellation channel; the higher-level context exposes it via `Done`. An adapter honoring cancellation must return `EINTR`. Cancellation is not rollback of an already committed mutation. [Context implementation](https://github.com/hanwen/go-fuse/blob/v2.9.0/fuse/context.go), [interrupt contract](https://github.com/hanwen/go-fuse/blob/v2.9.0/fs/api.go). | Its operation interface has no context, cancellation channel or interrupt callback. Inspection found no exported wrapper for `fuse_interrupted`/request interrupt registration. Underlying libfuse interrupt handling does not supply an application cancellation contract through this binding. [Complete operation interface](https://github.com/winfsp/cgofuse/blob/v1.6.0/fuse/fsop.go). |
| Entry/attribute caching | `fs.Options` exposes entry, attribute and negative timeouts; raw replies expose individual timeouts. Zero timeouts are available but do not disable the data page cache. [Options and caching contract](https://github.com/hanwen/go-fuse/blob/v2.9.0/fs/api.go), [reply structures](https://github.com/hanwen/go-fuse/blob/v2.9.0/fuse/types.go). | Mount accepts libfuse options. Entry/attribute/negative timeout options therefore belong to the selected libfuse ABI and runtime, rather than typed per-reply Go controls. [Mount option handling](https://github.com/winfsp/cgofuse/blob/v1.6.0/fuse/host.go), [libfuse high-level configuration](https://libfuse.github.io/doxygen/structfuse__config.html). |
| Explicit kernel invalidation | `Server.InodeNotify`, `EntryNotify`, `DeleteNotify` and higher-level inode helpers are exposed. Support checks use negotiated protocol versions; an `OK` notification still needs behavioral validation on the actual kernel. Call invalidation outside relevant operation locks to avoid kernel/daemon lock cycles. [Server notification implementation](https://github.com/hanwen/go-fuse/blob/v2.9.0/fuse/server.go), [inode helper warnings](https://github.com/hanwen/go-fuse/blob/v2.9.0/fs/inode.go). | `FileSystemHost.Notify` exists, but its C implementation returns zero on every non-Windows platform. It cannot establish Linux/FreeBSD invalidation. [Unix `hostNotify`](https://github.com/winfsp/cgofuse/blob/v1.6.0/fuse/host_cgo.go). |
| Caller UID/GID/PID and groups | Raw request `Caller` and `fuse.FromContext` expose UID/GID/PID. UID/GID are effective IDs except real IDs for ACCESS. No supplementary-group vector is in `Caller`. [Caller definition](https://github.com/hanwen/go-fuse/blob/v2.9.0/fuse/types.go), [context access](https://github.com/hanwen/go-fuse/blob/v2.9.0/fuse/context.go). | `Getcontext()` reads UID/GID/PID from thread-local libfuse context. Capture it during the callback, before delegating work. No supplementary-group wrapper is exposed. [Getcontext implementation](https://github.com/winfsp/cgofuse/blob/v1.6.0/fuse/host.go). Neither binding alone meets a full supplementary-group authorization requirement. |
| POSIX locks and flock forwarding | `EnableLocks` negotiates POSIX and flock flags, with `GetLk`, `SetLk`, `SetLkw` callbacks. `LkFlags` distinguishes flock requests. The loopback file maps them to host locks; that is not distributed lock ownership/recovery. [Negotiation](https://github.com/hanwen/go-fuse/blob/v2.9.0/fuse/opcode.go), [callback contract](https://github.com/hanwen/go-fuse/blob/v2.9.0/fuse/api.go), [loopback translation](https://github.com/hanwen/go-fuse/blob/v2.9.0/fs/files.go). | The operation interface's lock method and the C operation table's lock callback are commented out; no flock callback is installed. Kernel-local same-mount contention can still pass without forwarding and cannot prove suitability for HomelabFS. [Operation interface](https://github.com/winfsp/cgofuse/blob/v1.6.0/fuse/fsop.go), [C operation table](https://github.com/winfsp/cgofuse/blob/v1.6.0/fuse/host_cgo.go). |
| mmap and direct I/O | `Open` can return `FOPEN_DIRECT_IO` per handle. mmap is serviced through kernel page-cache reads/writes, not a separate Go mmap callback. Its INIT negotiation does not enable `FUSE_DIRECT_IO_ALLOW_MMAP`. [File API](https://github.com/hanwen/go-fuse/blob/v2.9.0/fs/api.go), [INIT negotiation](https://github.com/hanwen/go-fuse/blob/v2.9.0/fuse/opcode.go). | `SetDirectIO` sets the FUSE3 host configuration globally before mounting; default FUSE2 can receive libfuse `direct_io` options. There is no equivalent typed per-open flag in the Go `Open` return. [Host API](https://github.com/winfsp/cgofuse/blob/v1.6.0/fuse/host.go). Linux cached mode supports shared mmap; direct I/O disables it by default unless explicitly negotiated. These are kernel rules and need separate FreeBSD checks. [Linux I/O modes](https://www.kernel.org/doc/html/latest/filesystems/fuse/fuse-io.html). |
| Answer later from another goroutine | Operations can wait on bounded asynchronous work and return its result. The library writes the reply after the callback returns and recycles request storage afterward. There is no public detached request/reply token; do not return and retain request buffers for a later reply. [Request lifecycle](https://github.com/hanwen/go-fuse/blob/v2.9.0/fuse/server.go). | Likewise, a callback can wait for a worker, but the C bridge uses its synchronous return value and C buffer. It has no exported low-level `fuse_reply_*` token API. [Read/write callback bridge](https://github.com/winfsp/cgofuse/blob/v1.6.0/fuse/host.go). If truly detached replies become necessary, neither current adapter choice proves that requirement. |
| cgo/cross-build | The Linux probe builds with `CGO_ENABLED=0`; the pinned `fs` package also cross-builds Linux→FreeBSD/amd64 without C headers. A runnable FreeBSD binary still requires a FreeBSD runtime/kernel/helper. [FreeBSD Go mount implementation](https://github.com/hanwen/go-fuse/blob/v2.9.0/fuse/mount_freebsd.go). | Linux/FreeBSD require cgo, a target C compiler/sysroot and matching FUSE headers. `!cgo` support is Windows-specific. Runtime libraries are dynamically loaded; this does not remove the C build requirement. [Build constraints and loader](https://github.com/winfsp/cgofuse/blob/v1.6.0/fuse/host_cgo.go), [platform build guide](https://github.com/winfsp/cgofuse/blob/v1.6.0/README.md). No Linux→FreeBSD cgo cross-toolchain was prepared or claimed. |

## Consequences and adapter obligations

The decisive source differences are usable Unix invalidation, explicit
cancellation, forwarded lock callbacks, and a simpler cgo-free FreeBSD build.
cgofuse's wider platform coverage does not compensate for these missing APIs
for this project. No upstream library patch or fork is proposed.

Go-fuse does not settle supplementary-group authorization. Keep kernel
permission checking and adapter credential resolution explicit; a
`/proc/PID/status` observation on Linux is diagnostic only because it races PID
reuse and process credential changes. FreeBSD needs its own reviewed identity
mechanism. Do not invent an empty group set or treat the daemon's own groups as
the caller's groups. Multi-user and security-context validation remains a gate.

The adapter must bound outstanding callbacks, copy data before worker handoff,
observe cancellation while waiting, and release resources on every result.
Cancellation before acceptance differs from cancelling a request after a
mutation is committed. The shared commit protocol must define the latter.
Define POSIX lock owners independently of the daemon's own process identity;
forwarding syscalls alone does not implement cross-node lock recovery.

Begin with explicit zero metadata timeouts and no FUSE writeback-cache
optimization. Data-cache coherence still requires correctly ordered
invalidation or a demonstrated direct-I/O policy. Reconcile mmap requirements
with that policy instead of globally enabling direct I/O on assumption.

## Validation status and outstanding gates

Historical evidence: go-fuse v2.9.0 passed the eight functional checks on each
of three native FreeBSD 15.1 guests, both direct ZFS and guest-local FUSE (48
checks), with bounded I/O verification. The loopback implementation logged
unimplemented BMAP. This proves only those smoke operations, with no
replication, ownership or failover. The separate Rocky/9p path is transport
evidence, outside native integration. [Native results and limitations](../Lab-operations.md#initial-results-and-setup-fixes),
[earlier transport results](../PoC-fuse-and-benchmarks.md).

Prepared on this Linux workstation: both experiment binaries build with exact
module pins; FUSE2 cgofuse build, FUSE3 vet/errcheck and Python syntax checks
pass. go-fuse `fs` cross-builds for FreeBSD/amd64 without cgo. Header provenance,
dependency versions, safe bounds and exact proposed execution are recorded in
the [experiment run plan](../../tools/experiments/fuse-library/README.md).
The approved Linux `r1` stopped during go-fuse buffered flock unlock with
ENOSYS: the probe omitted `SetLkw`, which Linux can use for unlock. The probe
now forwards that unlock and explicitly rejects unimplemented blocking
acquisition. The coordinated `r2` passed flock acquire/contender/unlock, then
exposed a second probe defect: treating MaxUint64 instead of MaxInt64 as the
FUSE EOF lock sentinel overflowed the fcntl length and returned EINVAL. The
probe now validates ranges and uses the pinned binding's `ToFlockT`/`FromFlockT`
helpers, with eight passing pure conversion cases. Ordinary unmount succeeded
and the daemon exited after both attempts; evidence remains under
`/tmp/hfs-fuse-library-20261028-hq3-linux-r1` and `-r2`. These are probe failures,
not evidence against the library. In `r3`, both lock types and invalidation
worked, but a signalled cached read returned normally after 3.002 seconds with
no cancellation event. Linux 7.2.5's cached readahead can submit background
requests; a synchronous direct read uses the interruptible foreground request
path. This is an inferred explanation, not proof of the exact r3 request path.
The revised probe uses direct I/O specifically for the cancellation fixture,
and requires signal receipt, EINTR and a daemon cancellation event.
[`fuse_send_readpages`, `fuse_direct_read_iter`](https://github.com/gregkh/linux/blob/v7.2.5/fs/fuse/file.c),
[`request_wait_answer`](https://github.com/gregkh/linux/blob/v7.2.5/fs/fuse/dev.c).
The user has waived separate
approval prompts for routine local operations in the current session; root
coordinates the rerun, with no lab or production operation authorized here.

In `r4`, both go-fuse profiles completed: identity logging, worker return,
flock/POSIX contention, data/attribute invalidation and normal unmount passed.
Buffered shared mmap passed; direct mmap returned ENODEV (19). Foreground
direct-read cancellation produced EINTR and daemon cancellation events in
approximately 13 ms in both profiles. The cgofuse FUSE3 binary then panicked
before mounting because the installed SONAME is `libfuse3.so.4`, while its
pinned loader requests `.so.3`. The final comparison below uses its supported
FUSE2 build against installed `libfuse.so.2`; no upstream patch or ABI symlink
is used.
[Pinned loader](https://github.com/winfsp/cgofuse/blob/v1.6.0/fuse/host_cgo.go),
[FUSE2 `direct_io` option](https://github.com/libfuse/libfuse/blob/fuse-2.9.9/lib/fuse.c).

The corrected **`r5` completed all four Linux profiles**, using go-fuse and
cgofuse FUSE2, on kernel `7.2.5-200.fc44.x86_64`. Evidence is retained under the
[experiment results](../../tools/experiments/fuse-library/results/r5/), with
[checksums](../../tools/experiments/fuse-library/results/SHA256SUMS).

| Runtime criterion | go-fuse buffered / direct | cgofuse FUSE2 buffered / direct |
|---|---|---|
| Caller identity | Daemon UID/GID/PID matched external client; supplementary groups only diagnostic | Same |
| Delayed worker | Completed read from worker result (250/501 ms; direct path includes an EOF read) | Completed (251/502 ms) |
| Foreground interruption | Signal received, EINTR returned in 12.2/11.2 ms, daemon cancellation events present | Signal received; returned one byte after 3.002/3.003 s; no cancellation exposed |
| Shared writable mmap | Buffered flush/fsync/read-back passed; direct rejected with ENODEV (19) | Same |
| flock and POSIX locks | Contender denied for both; daemon logs establish backend callbacks and unlocks | Same-mount contender denied, with no forwarding API; not a backend-lock pass |
| Cache change | Inode/entry notification OK; reopen and stat saw `B` and 2048 bytes before 60-second TTL | Notify false; reopen and stat also saw `B`/2048, which proves only this open/cache path and does not establish notification support |
| Normal cleanup | Both ordinary unmounts returned zero | Same |

The cgofuse fresh reopen must not be presented as successful explicit kernel
invalidation. Long-lived handles and mmap-visible freshness still need their
own checks. The source differences and runtime cancellation/forwarding
observations support choosing go-fuse; they do not certify a complete adapter.
Useful failed `r1`–`r4` diagnostics are retained beside `r5` so the final
comparison does not conceal earlier failures. No force/lazy cleanup, system
package installation, VM operation or production data change was used.

The completed Linux run covers identity, delayed worker return, targeted
foreground interruption, reopen/stat invalidation behavior, mmap/direct-I/O
and same-mount lock contention with forwarding evidence for go-fuse. It does
not certify all filesystem semantics or trust diagnostic supplementary groups.

Outstanding native FreeBSD checks, requiring separately approved disposable
lab runs, are:

1. Negotiated FUSE capabilities and actual entry/attribute/data invalidation,
   including negative entries, open handles and mmap-visible freshness.
2. Request interruption, bounded blocked reads/writes/locks, normal unmount and
   daemon shutdown with work outstanding; establish mutation cancellation rules.
3. Exact caller UID/GID/PID and supplementary groups under multiple users,
   permission denial, changed groups and guest security contexts.
4. POSIX/flock forwarding and owner lifetime across processes, independent
   mounts, close/unlink, daemon restart and later replicated ownership changes.
5. mmap/read/write/truncate coherency versus direct I/O and cache fencing on
   the real kernel; directory durability and precise fsync propagation.
6. Complete opcode coverage relevant to workloads (including the BMAP warning),
   file identity/open-unlink behavior, crash/remount and stale-handle behavior.

These binding checks supplement the existing native integration and real-daemon
guest compatibility gates. They do not add 9p to the native three-guest matrix,
authorize a VM lifecycle window, or change production storage.
