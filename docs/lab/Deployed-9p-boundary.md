# Deployed 9p boundary investigation, 2026-10-30

> Current contract update, 2026-10-30: the dated evidence below is unchanged,
> but any exclusive writable-client publication requirement is superseded.
> Any admitted HomelabFS node must be able to accept writes for any volume;
> guest-local advisory locks are distinct from filesystem ordering, commitment,
> cache coherence and stale-client fencing. See [the current architecture](../Architecture.md)
> and [review/task reconciliation](../reviews/20261030-multiwriter-review.md).


The inspected f3 lib9p implements successful server lock replies without host
locking. Its installed machine code establishes this directly. The current
Rocky boot also has no 9p support. Neither the current boot nor the installed
6.1.190 candidate is admitted for real-daemon publication. Task `zq3` and its
replication dependency remain blocked; this investigation supplies a design
decision input, not a compatibility pass.

Scope: read-only target inventory and retrieval of three public OS artifacts,
then offline source/disassembly analysis. No VM lifecycle, mount, workload,
module loading, installation, credential relaxation or production change ran.
The real daemon remains restricted to its existing disposable self/controlled
child profiles. See [preparation prerequisites](Real-daemon-9p-preparation.md),
[permission boundary](../Platform-interfaces.md#unix-permission-enforcement-boundary)
and [required matrix](../Deployment-tests.md).

## Accepted disposition, 2026-10-30

Paul retained 9p and accepted cooperating advisory locks in the same guest
**same 9p mount/kernel inode domain**, including pods sharing that underlying
mount. Separate 9p mounts (even in one guest), other guests and direct host
access have no shared-lock guarantee. The installed successful no-op callbacks
below remain a proven fact. Linux local conflict handling is supported by the
pinned candidate source, not a live exact-target pass. The
[current lock contract](../Platform-interfaces.md#accepted-9p-lock-scope-2026-10-30)
requires local contention/GETLK/blocking/nonblocking/close/exit validation and
separate proof of exclusive writable-client publication and old-client fencing,
including cached access.

This supersedes the earlier architecture-choice request below and the
[Linux frontend/RPC recommendation](Publication-boundary-options.md); that path
is not selected. No other semantic waiver follows: guest credentials/groups and
process identity, synchronous flags/barriers, cache/generation/K04/fencing and
normal-publication prerequisites remain unresolved. `zq3` stays incomplete and
`4r3` blocked. No runtime or artifact admission is changed by this decision.

## Actual target and evidence

ROOT's completed pinned observations at 17:54:29 and 18:02:52 UTC established
Rocky PID 91477 running, all four FreeBSD VMs stopped, and f3 kernel/userland
`15.1-RELEASE-p2`, kernel revision `aadd58dddcbc`. `procstat` records bhyve UID/GID
zero, groups `0,0,5`, executable `/usr/sbin/bhyve`; the later fixed `ps` query
records its parent and start time. The earlier comma-separated `ps -o` query
misparsed its header and provides no start-time evidence. The existing VM
configuration exports `homelabfs_poc=/var/tmp/homelabfs-9p-poc/share`; it is not
an admitted real-daemon export.

| Retrieved public artifact | Size | SHA-256 |
|---|---:|---|
| `/usr/sbin/bhyve` | 505672 | `c54a63615edb12eca0a62bdadba97aa39641358302f6cdcee7362a858c709865` |
| `/usr/lib/lib9p.so.1` | 100928 | `589e8d0f9db4401f8a33274a5efe31d3cd742b18cedba87601c39617501c01ed` |
| `/usr/lib/lib9p.a` | 472722 | `f37d8747b99a2823ee35a2556c4827f99e2e3b2b46f89e14ef8c9b6b48133aa7` |

ROOT retrieved only exact-size, root-owned regular files after no-follow opens,
matching fstat/path stability checks and fresh digest comparison. `procstat -v`
was filtered to the lib9p filename/count; it establishes that the process maps
that pathname, not direct attestation of its in-memory bytes. No unexpected
replacement evidence was observed. Installed source and bhyve debug files are
absent; base artifacts are not owned by the local pkg database. This is ordinary
deployment evidence plus direct on-disk machine-code inspection, not a signed
source-to-binary/reproducible-build attestation.

Rocky was running `5.14.0-687.61.1.el9_8.x86_64`. Its config disables `NET_9P`,
its 9p module directories are absent, and findmnt found no 9p mount. Installed
ELRepo `6.1.190-1.el9.elrepo.x86_64` has 9p/9pnet/virtio modules and matching
vermagic; it was neither booted nor admitted here. RPM metadata identifies
`kernel-lt-6.1.190-1.el9.elrepo.nosrc.rpm`, vendor ELRepo and signature key ID
`51600989eaa31d4a`; this is metadata, not independent signature verification.
The inspected modules expose no signer fields. Both configs enable module
signature infrastructure without `MODULE_SIG_FORCE`. The current boot reports
Secure Boot disabled, lockdown none and taint 65536. These observations do not
establish the future candidate's boot/runtime trust enforcement or module
dependency closure. SELinux is Enforcing; podman/docker executables exist,
while k3s/kubectl were not found. No pod runtime or namespace fixture was admitted.
The diagnostic itself ran in the initial user namespace with identity ID maps.

[Inventory and receipt summaries](results/20261030-9p-boundary/inventory.json),
[offline checks](results/20261030-9p-boundary/offline-checks.json) and
[installed disassembly](results/20261030-9p-boundary/installed-lib9p-disassembly.txt)
contain no binary/base64 blobs. Original receipts, streams and mode-0600 public
binary copies remain privately retained by ROOT. Earlier metadata streams/receipts
were initially mode 0644 inside their private 0700 directory; ROOT corrected
them to 0600 without changing bytes. Binary retrieval files were 0600 from creation.
The first host attempt failed
with `Timeout, server 192.168.1.133 not responding.`. The observed local
OpenSSH 10.2p1 emits this from its authenticated-session
[ServerAlive check](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/clientloop.c#L470-L485);
[ssh_login precedes client_loop](https://github.com/openssh/openssh-portable/blob/V_10_2_P1/ssh.c#L1739-L1773).
Remote command dispatch/completion and the timeout's cause remain unknown;
zero buffered stdout does not prove the helper never ran. Independent read-only
state/process inspection established the helper was absent before the successful
fresh inventory. The failure remains distinct from historical connect/banner
failures and supplies no filesystem pass. Rocky's
successful receipt separately retains the 222-byte OpenSSH key-exchange warning.

## Installed code and source binding

The actual shared library's exported `l9p_backend_fs_init` starts at `0x11760`.
It installs fsync at `0x141a0` (backend offset `0xd8`), lock at `0x14200` (`0xe0`)
and getlock at `0x14220` (`0xe8`). The 29 bytes at `0x14200` exactly equal the
named `fs_lock` function in the retrieved archive's `fs.o`, text offset `0x28f0`:
SHA-256 `0bd0c5c1a9eba82f27bb7325ab7b56fd64d6965d9d0c78b202c96471a30fc033`.
They validate lock type 0–2, set response status zero and return zero. There is
no call, syscall or retained lock state. Getlock sets unlocked and supplies an
empty client string. Thus server-side lock success is unsupported in this
installed artifact, independently of upstream-main observations.

The exact observed kernel revision resolves to
`aadd58dddcbc78f4d5594827b46b5633552b15ce`. Its
[backend layout](https://github.com/freebsd/freebsd-src/blob/aadd58dddcbc78f4d5594827b46b5633552b15ce/contrib/lib9p/backend/backend.h)
and [backend source](https://github.com/freebsd/freebsd-src/blob/aadd58dddcbc78f4d5594827b46b5633552b15ce/contrib/lib9p/backend/fs.c)
agree with inspected callback layout, lock/getlock behavior, attach calls to host
password/group services, write's `pwritev`, and fsync's `dirfd`/`fsync`. The fs.c
source digest is `3268c20143590345b59b8033d3aadc17b13a578aebda33ad8133d937d1c0959c`.
This binds the particular inspected semantics; it does not certify every source
line as the exact compiler input.

For the unbooted candidate, the
[official ELRepo recipe](https://github.com/elrepo/kernel/blob/8df48fa42eadfd2d79a5ef6036b80979846457f4/kernel-lt/el9/kernel-lt-6.1.spec)
pins upstream 6.1.190 and applies no 9p source patches in its preparation.
The corresponding upstream commit is
`79807e970724c1f90cee1316361d61b80db4eef6`. The following guest trace is a
version-specific source assessment, not observed guest behavior or independent
verification of the installed RPM contents.

## Property mapping and admission consequence

| Property | Boundary and evidence | Consequence |
|---|---|---|
| UID | Linux attaches/walks fids by fsuid; lib9p stores attach UID in its own authinfo, then issues ordinary host syscalls. Installed attach calls host account/group services; installed write calls `pwritev`. | Guest numeric UID is not forwarded as a FUSE request credential. |
| Primary/supplementary groups | lib9p fs_attach derives its authinfo groups from host account lookup, not a complete guest request group vector. | Same guest UID with differing effective groups cannot satisfy the required daemon snapshot through this path. No guessed-group fallback is allowed. |
| PID/process incarnation | Host FUSE headers use host thread credentials and process PID; ordinary requests originate from bhyve, not guest helpers. | No guest process incarnation/exit authentication reaches the existing Credentials provider. Enrolling bhyve would not recover it. |
| Fid/open description | lib9p retains a host fd per opened fid; walk retains authinfo. Guest fid numbers do not become daemon descriptor identities. | Host handles exist, but guest dup/fork/transfer ownership cannot be inferred from their identity alone. |
| Close/exit | Guest final file release drops its fid; lib9p clunk closes its host fd. No independent guest-process exit event is supplied to HomelabFS. | Guest close-any-fd POSIX cleanup and process retirement are not established by host close or bhyve lifetime. |
| POSIX locks/client | Linux first enforces local locks, then sends TLOCK with proc_id/client_id; installed lib9p returns success without host locks. | Local exclusion can appear to work while independent clients do not share an enforced server lock domain. Independent-client locking is unsupported and outside the accepted 9p lock contract; local behavior still requires live validation. |
| flock | Candidate Linux source converts flock to POSIX locking for the wire request. | Do not infer independent flock/open-description lifetime semantics from local success. |
| Ordinary write | In upstream 6.1.190, specifically `cache=mmap` write_iter calls protocol write directly; installed lib9p uses pwritev. | Current generic documentation about writeback is not version-specific proof. No daemon/journal barrier was traced here. |
| O_SYNC/O_DSYNC | Reviewed lib9p flag conversion clears both flags; installed lopen conversion agrees. Candidate ordinary write_iter returns without generic_write_sync. | Synchronous-open propagation is an additional source-supported blocker/risk; no live durability claim. |
| fsync/fdatasync | Candidate file fsync writes/waits dirty pages then sends TFSYNC. Installed lib9p invokes host fsync, ignoring datasync distinction. | Plausible stronger barrier path; real daemon admission and journal correlation still required. |
| Directory fsync | Candidate dotl directory operations use the same fsync function; installed lib9p selects dirfd when present. | Path exists; namespace durability and error propagation remain untested. |
| mmap/MS_SYNC | Candidate writable mmap obtains a root writeback fid. MS_SYNC calls vfs_fsync_range, then the fsync path above. | Root writeback identity further prevents recovery of the original guest principal. No per-store guarantee; no MS_SYNC failure/pass inferred. |
| Namespace/pod IDs | Candidate wire UID/GID conversion uses init_user_ns. Current observation describes only the diagnostic's namespace. | Pod securityContext and user-namespace mappings require separate exact fixtures. |
| K04/generation/cache | bhyve source opens and retains export rootfd at initialization; no HomelabFS identity guard appears there. | Readiness by path or guest remount alone cannot certify retained bhyve roots/fids. Fail-closed fallback, revocation and generation recovery remain unimplemented/unvalidated. |

Primary source detail: candidate
[fid/UID and root writeback selection](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/fs/9p/fid.c),
[file I/O, mmap and locks](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/fs/9p/vfs_file.c),
[directory release/fsync](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/fs/9p/vfs_dir.c),
[wire encoding](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/net/9p/protocol.c),
[MS_SYNC](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/mm/msync.c);
host [FUSE header construction](https://github.com/freebsd/freebsd-src/blob/aadd58dddcbc78f4d5594827b46b5633552b15ce/sys/fs/fuse/fuse_ipc.c)
and [retained export root](https://github.com/freebsd/freebsd-src/blob/aadd58dddcbc78f4d5594827b46b5633552b15ce/usr.sbin/bhyve/pci_virtio_9p.c).

## Historical next-decision request (superseded)

The following request preceded the accepted lock-scope decision above; its
comparison was recorded in [Publication boundary options](Publication-boundary-options.md).
It is retained as historical rationale, not a current architecture request.

There is no full-matrix disposable publication contract implementable by merely
adding bhyve to the existing daemon provider. The identity transformation loses
information; relaxing header equality or trusting host root would change the
security contract. Exclusive guest serving could restrict lock research to one
Linux lock domain, but it would not satisfy independent-client locks or repair
credentials, synchronous flags, K04 and cache/generation behavior.

Create one bounded **publication-boundary design comparison** prerequisite,
without implementing a new transport or changing the deployment selection:

| Option to assess | Scope/cost and required proof |
|---|---|
| Retain current 9p only for restricted research | Smallest change; explicitly incomplete acceptance, no replication unblock. Specify single-guest restrictions and denied unsupported properties. |
| Preserve the selected host/guest shape with a different maintained sharing implementation | Potentially substantial transport/platform work. Identify an available implementation and demonstrate complete authenticated credentials, distinct lock owners, sync/mmap and retained-root fencing before proposing a disposable prototype. No vendor patch is authorized here. |
| Reconsider where the filesystem frontend runs | Native Linux FUSE is already a project platform, but deploying it beside Rocky consumers changes the selected f3s path. Remote storage/core authentication, ownership and fencing are unimplemented design work; Linux FUSE alone supplies no solution. |

Acceptance for that task: a reviewed comparison chooses or rejects one option;
names the trust boundary, complete numeric-credential provenance, process and
open-description lifecycle, lock domains, supported namespace mapping, sync
boundary, cached-read/revocation behavior and generation/K04 publication guard;
records compatibility concessions explicitly; and proposes the smallest
synthetic isolated prototype plus target/capacity/rollback plan. It must preserve
normal-publication refusal and existing native acceptance, require no production
migration/NFS change, and make no full-matrix pass out of restricted research.
The architecture choice remains explicit. Only after it is resolved should the
implementation/setup and full zq3 harness prerequisites be selected.
