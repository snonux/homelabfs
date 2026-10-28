# Linux FUSE binding experiment

This is a disposable, separate Go module. It imports neither the root module nor
`tools/poc/`. The corrected Linux comparison completed all four profiles in
`r5`; earlier attempts and their probe defects are retained below.
The binding decision and remaining FreeBSD gates are in
[ADR 0001](../../../docs/adr/0001-fuse-library.md).

The repository `AGENTS.md` requires explicit approval. On 2026-10-28 the user
waived separate prompts for routine local operations in this session; root
coordinates execution under that authorization. Future sessions must apply
their own approval instructions. An execution flag is never approval by
itself. There is no lab, SSH, VM, production, or 9p access in this probe.

## Prepared workstation dependencies

Go `go1.26.8-X:nodwarf5 linux/amd64`, gcc, Python 3, `/dev/fuse` (mode 0666),
`fusermount3`, `fusermount`, `errcheck`, and `goimports` are present. Installed
runtime libraries: `fuse3-libs-3.18.2-1.fc44.x86_64` and
`fuse-libs-2.9.9-25.fc44.x86_64`. System development headers and pkg-config
entries for both versions were absent. No system package was installed.

Official Fedora package payloads were downloaded with
`dnf download --from-repo=fedora,updates` into the fresh directory
`/tmp/hfs-fuse-builddeps-20261028-Hl5Smo`. Only the x86_64 payloads were
extracted there with `rpm2cpio`/`cpio`; additional i686 downloads are unused.
Package metadata identifies Fedora Project and upstream libfuse:

| Package | SHA-256 |
|---|---|
| `fuse-devel-2.9.9-25.fc44.x86_64.rpm` | `d7ac5f583d5300932b323c97fea488930021995c16109fb61899d49cb6ac85c5` |
| `fuse3-devel-3.18.3-1.fc44.x86_64.rpm` | `7dc14901ce9b1d37f34eff866dba2bbd2ba81f29d864c0e23f1f055329bb8233` |

The FUSE3 headers are 3.18.3 while the installed runtime is 3.18.2. The FUSE3
binary builds but cannot load this workstation's `libfuse3.so.4`: pinned
cgofuse requests `libfuse3.so.3`. The comparative run therefore uses its
supported default FUSE2 ABI and installed `libfuse.so.2`, with matching 2.9.9
headers. No ABI substitution or system library modification was made.
cgofuse loads the runtime with `dlopen` and
needs C headers at build time; it does not require pkg-config for this build.
All C flags below affect only the current command.

From this directory, preparation commands (build/static analysis only) are:

```sh
go mod download
CGO_ENABLED=0 go build -o /tmp/hfs-fuse-builddeps-20261028-Hl5Smo/gofuse ./cmd/gofuse
CGO_CFLAGS=-I/tmp/hfs-fuse-builddeps-20261028-Hl5Smo/usr/include/fuse go build -o /tmp/hfs-fuse-builddeps-20261028-Hl5Smo/cgofuse ./cmd/cgofuse
CGO_CFLAGS=-I/tmp/hfs-fuse-builddeps-20261028-Hl5Smo/usr/include/fuse go vet ./...
CGO_CFLAGS=-I/tmp/hfs-fuse-builddeps-20261028-Hl5Smo/usr/include/fuse errcheck ./...
python3 -m py_compile probe.py
python3 probe.py
```

Both binaries built, vet and errcheck passed, Python compiled, and the last
command printed only the plan. A separate FUSE2 cgofuse build also passed. The
go-fuse `fs` package cross-built for FreeBSD/amd64 with `CGO_ENABLED=0`.
No tests or FUSE mounts are part of those preparation checks.

## Bounded future run plan

```sh
cd /home/paul/git/homelabfs/tools/experiments/fuse-library
python3 probe.py --run --root /tmp/hfs-fuse-library-20261028-hq3-linux-r6
```

This creates **only** `/tmp/hfs-fuse-library-20261028-hq3-linux-r6` (mode 0700)
and refuses an existing path. Each of its four child directories contains
`mnt`, `lock-backing`, daemon logs and client JSON/stderr evidence:

1. `gofuse-buffered`
2. `gofuse-direct`
3. `cgofuse-buffered` (FUSE2 build)
4. `cgofuse-direct` (FUSE2 build)

Mounts run sequentially as the current unprivileged user, without `allow_other`.
Each daemon has six synthetic files of at most 4096 bytes in memory and a
4096-byte private backing lock file; total fixture payload for all four mounts
is at most 112 KiB. No arbitrary path passthrough is implemented. The go-fuse
`direct` and `cancel` fixtures use direct I/O even in the buffered mount; the `-direct`
configuration selects it for every file. cgofuse selects direct I/O globally
through the FUSE2 `direct_io` mount option. Its FUSE3 variant uses the typed
host configuration, but cannot run with this workstation's available SONAME.

Clients are separate Python processes to avoid serving FUSE requests and
accessing the mount in the same Go runtime. For each mount:

- Read fixed data and credentials; log kernel UID/GID/PID alongside the client's
  own identity. Observe `/proc/PID/status` groups diagnostically, without using
  them for authorization. This exercises the current user and its groups only.
- Have a worker goroutine return a read after 250 ms while the callback waits.
  This distinguishes asynchronous work from a true detached reply API.
- Attempt writable shared mmap, flush and fsync, then read back one byte.
  Buffered mode requires success; direct mode records success or errno.
- Attempt same-mount conflicting flock and fcntl locks from separate processes.
  go-fuse logs distinguish forwarded backend locks from kernel-local exclusion.
  The tiny backend uses Linux OFD locks so daemon file descriptors do not merge
  unrelated POSIX owners; it does not implement HomelabFS distributed locks.
- Send SIGUSR1 to an external client blocked in libc `read` after the daemon
  reports entry. Capture syscall return/errno, elapsed time and daemon
  cancellation evidence and confirm the client received its signal. go-fuse's
  cancel fixture uses direct I/O to target a foreground FUSE request; cgofuse's
  buffered profile remains a cached-read observation. cgofuse has no
  cancellation callback and its worker is capped at three seconds.
- Keep cache timeouts at 60 seconds, change cached data from 4096 `A` bytes to
  2048 `B` bytes outside the mounted client, and issue the binding's invalidation
  API from a signal goroutine. Record notification status, reopened content and
  stat size before TTL expiry. go-fuse requires fresh content/size; cgofuse
  reports the observed limitation without treating known absent Unix Notify
  support as a surprise pass. go-fuse entry notification is exercised on a
  positive name; negative-entry lifetime remains source-only evidence.

Readiness is capped at five seconds, each ordinary client at twelve seconds,
blocked-read readiness at three seconds and its completion at five seconds.
Each cleanup command has a five-second limit. Four mounts should take under
one minute normally; budget four minutes including bounded failures.

The runner always attempts ordinary `fusermount3 -u`. It never uses force or
lazy unmount. A failed unmount leaves the daemon, mount, payload and evidence
intact and reports their path/PID. If normal unmount succeeds, it waits five
seconds for daemon exit and sends SIGTERM only if that already-unmounted
disposable daemon remains. All artifacts remain even on success. There is no
recursive deletion in the runner. A later cleanup or rerun requires its own
review and the authorization applicable to that session.

## Initial run record

The explicitly approved `20261028-hq3-linux-r1` stopped during the first
go-fuse buffered client: flock acquisition/contender exclusion reached the
backend, but `flock(LOCK_UN)` returned ENOSYS. The probe implemented `SetLk`
and inherited the default `SetLkw`. The pinned dispatch calls `SetLkw` for
FUSE_SETLKW; Linux uses that route for this unlock. The corrected probe now
forwards SetLkw unlocks to the same backend, while explicitly rejecting
unimplemented blocking acquisitions. This is a probe defect, not a binding
incompatibility result. The ordinary unmount succeeded and the daemon exited.
All evidence remains at
`/tmp/hfs-fuse-library-20261028-hq3-linux-r1/gofuse-buffered`.

The coordinated `r2` passed flock acquire/contender/unlock, then stopped when
POSIX lock acquisition returned EINVAL. The probe had treated MaxUint64 as the
through-EOF sentinel; Linux FUSE supplies MaxInt64. Converting that inclusive
range to a signed length overflowed. The corrected probe validates ranges,
uses pinned go-fuse `ToFlockT`/`FromFlockT` helpers for both directions, and has
eight pure range/round-trip unit cases (passed). Ordinary unmount again
succeeded; all evidence remains under the corresponding `r2` directory.

The coordinated `r3` passed both lock types and cache invalidation but failed
the assumption that interrupting a cached libc read must cancel its FUSE
request. The read returned one byte after 3.002 s without a daemon cancellation
event. In kernel 7.2.5, cached readahead can submit background FUSE reads,
whereas a synchronous direct read calls `fuse_simple_request` and its
interruptible waiter queues FUSE_INTERRUPT. This explains a plausible kernel
path difference, rather than establishing a missing go-fuse API. Sources:
[`fuse_send_readpages`, `fuse_send_read`, `fuse_direct_read_iter`](https://github.com/gregkh/linux/blob/v7.2.5/fs/fuse/file.c)
and [`request_wait_answer`](https://github.com/gregkh/linux/blob/v7.2.5/fs/fuse/dev.c).
The revised go-fuse cancel fixture explicitly requests direct I/O, preserves
the cancellation assertion, and checks both signal receipt and EINTR. The
ordinary r3 unmount succeeded; evidence remains in its run directory.

The coordinated `r4` completed both go-fuse profiles: buffered mmap passed,
direct mmap returned ENODEV (19), both lock types excluded the contender,
invalidation refreshed content/size, and foreground cancellation returned
EINTR in about 13 ms in both profiles. Both mounts unmounted normally. The
cgofuse FUSE3 binary then panicked before mounting: it could not load FUSE.
The pinned loader calls `dlopen("libfuse3.so.3")`, whereas `ldconfig` reports
only `libfuse3.so.4` and the supported FUSE2 `libfuse.so.2` here. The corrected
comparative plan uses the pinned library's supported FUSE2 build and
`direct_io` option, without changing upstream code or forging an ABI symlink.
Sources: [cgofuse loader](https://github.com/winfsp/cgofuse/blob/v1.6.0/fuse/host_cgo.go)
and [libfuse 2.9.9 option handling](https://github.com/libfuse/libfuse/blob/fuse-2.9.9/lib/fuse.c).

## Completed comparison

The coordinated `r5` exited successfully and observed all four mount profiles.
Raw JSON, daemon logs, stderr and normal-unmount evidence are preserved under
[`results/r5`](results/r5/); useful failed-attempt evidence is in `r1`–`r4`.
[`results/SHA256SUMS`](results/SHA256SUMS) covers the copied evidence.
No binaries, backing fixtures or mount directories were copied into the repo.

| Observation | go-fuse buffered/direct | cgofuse FUSE2 buffered/direct |
|---|---|---|
| Caller credentials | UID/GID/PID match the invoking client; procfs groups recorded diagnostically | Same |
| Worker result | Read completed after a worker delay | Same |
| Cancellation | Signal receipt, raw EINTR and daemon cancellation event; 12.2/11.2 ms | Signal received; read completed normally after 3.002/3.003 s; no request cancellation API |
| Shared writable mmap | Buffered passed; direct returned ENODEV (19) | Same |
| flock and POSIX contention | Conflicting process denied; daemon logs prove backend forwarding | Conflicting process denied; this does not establish backend forwarding |
| Cache mutation | Inode/entry notifications returned OK and reopen/stat saw `B`/2048 bytes | Notify returned false; reopen/stat also saw `B`/2048 bytes, an observed open/cache path rather than evidence for notifications |
| Cleanup | Normal unmount returned zero for both profiles | Same |

All captured r5 client stderr files are empty. The probe uses separate
processes on one mount as one user; it cannot establish supplementary-group
authorization, long-lived open-handle
cache behavior or distributed locks. No force/lazy unmount, system package
installation, lab operation or production change occurred.

## Evidence limits

This probe does not test multiple users or changed supplementary groups,
blocking lock cancellation/recovery, cross-mount locks, directory negative
cache expiry, full mmap coherency, crashes, durability, replication, fencing,
or FreeBSD behavior. `Fsync` here acknowledges an in-memory fixture; it is
strictly a binding exercise and supplies no durability evidence. These gaps
must remain explicit in the ADR and native adapter gate.
