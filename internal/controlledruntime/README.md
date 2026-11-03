# Controlled native bootstrap

This package implements C3 of `docs/lab/Controlled-runtime-integration.md`.
It owns one disposable endpoint, exporter process and private issuance root.
It also implements the C5 live Facts/source/receipt owner described below.
It does not provide guest credential attestation, transport recovery, normal
publication, an installed driver, or deployment acceptance.

`Create` exclusively creates a private root and locks its ownership file. `Start`
consumes its fresh stage before trying to launch. The exact SHA-256-admitted
executable is copied exclusively, synced, made read-only and executed through
its retained descriptor, with management socket fd 3 and executable fd 4.
No guest-selected executable, socket path or reconnect is accepted. Failed
startup files and uncertain mounts remain in place. `Open` refuses missing
ledgers; an issued/attempted root cannot launch a replacement. No RESUME or
restoration is enabled. `Close` fences admission and closes the management
socket/owner lock; it does not kill, unmount, delete, or retire obligations.
The supervised exporter must stop serving on management EOF.

On FreeBSD, `MountShell` itself opens `/dev/fuse`, executes the descriptor-pinned
root-owned mount helper, observes the newly mounted FSID, and supplies a duplicate
of that exact open device to go-fuse's supported `/dev/fd/N` path. It retains the
original device reference, actual server, mount association and shell lifetime.
Neither a device inode nor successful `fstatfs` alone establishes a live FUSE
connection. The shell supplies its tuple only after the server's kernel INIT;
`guestbridge.New` calls `Enroll` with the actual newly constructed controller.
The protected issuance stage prevents another construction from claiming it.
No constructor takes a caller-provided native endpoint witness or readiness flag.

`Bind` atomically installs the constructed Transport in that shell. Before bind,
every raw callback refuses private and workload traffic. The bound frontend has
only synthetic private entries and refuses workload operations. During tuple
opening, only the actual supervisor incarnation/full native groups may perform
exact private LOOKUP, GETATTR, leaf OPEN, and root STATFS. The raw adapter creates
an unforgeable request context; the supervisor's authentication context is
single-use and bound to that exact callback. Ordinary Snapshot/Verify and all
READ/WRITE/command paths refuse the provisioner. The supervisor stage is
irreversibly sealed before descriptors leave the process. The actual child then
becomes the sole admitted caller, checked on every authenticated callback against
its current native identity/full groups, executable, mounted shell, controller,
inherited management channel and exact private kernel node/operation. The pinned
C adapter observes descriptor closure/replacement locally before I/O. Process
death, execution of a different image, and credential changes refuse admission.
Re-executing the same pinned image can preserve PID/start/text-vnode/path and
credentials, and is outside these OS observations' detection boundary. The
trusted pinned C backend must never re-exec or fork/pass management or data
descriptors to unsupervised successors. C4 must set FD_CLOEXEC on inherited fd 3
and on every received data descriptor (for example with MSG_CMSG_CLOEXEC plus
explicit verification), close the inherited executable descriptor once startup
is complete, and retain sole ownership until protected retirement. Close-on-exec
does not itself prevent fork inheritance; the backend must not delegate those
descriptors through fork or descriptor passing.

FreeBSD STATFS retains the actual thread credentials: `fdisp_make` uses
`RECTIFY_TDCR` before `fuse_setup_ihead`. Its fixed, authenticated zero-capacity
metadata response grants no transport or source authority. FreeBSD can also
synthesize successful statfs on ENOTCONN; live device/server/process ownership is
therefore checked independently. Relevant maintained kernel sources:
https://raw.githubusercontent.com/freebsd/freebsd-src/releng/14.3/sys/fs/fuse/fuse.h
https://raw.githubusercontent.com/freebsd/freebsd-src/releng/14.3/sys/fs/fuse/fuse_ipc.c
https://raw.githubusercontent.com/freebsd/freebsd-src/releng/14.3/sys/fs/fuse/fuse_vfsops.c

## C backend interface (C4 consumes this)

All fields are big-endian. The inherited AF_UNIX stream has no named listener.
The 16-byte header is `HSM1`, version byte 1, opcode byte, two zero reserved
bytes, uint32 sequence, uint32 payload length. Maximum payload is 184 bytes.
Unknown versions/opcodes, wrong exact lengths, oversized/truncated frames,
wrong sequence, authentication failures, uncertain rights transfer and socket
loss fence the supervisor. Sequence starts at 1, never wraps, and has no replay
or reconnect semantics. A management frame must finish within five seconds;
currently an idle channel is also fenced after five seconds, so the adapter must
send a VERIFY heartbeat within that interval while retaining a live endpoint.
No timeout retires anything. Payload unions:

| Opcode | Exact payload |
| --- | --- |
| 1 CLAIM | endpoint (40 bytes) + protected ledger digest (32 bytes) |
| 2 RECONCILE | endpoint + protected ledger digest (72 bytes); always BLOCKED until C8/C9 |
| 3 VERIFY | endpoint + controller UUID + uint64 observation sequence + three 40-byte native fd observations (184 bytes) |
| 4 TUPLE (host to child only) | endpoint + controller UUID (56 bytes), sequence 0, exactly three SCM_RIGHTS descriptors |
| request opcode OR 0x80 | uint32 status (4 bytes): 0 success, 1 denied, 2 malformed, 3 blocked |

An endpoint is its raw 16-byte nonzero session UUID followed by command/result/
recall uint64 node IDs. TUPLE sends descriptors in that same order. The child
receives only the prevalidated real FreeBSD FUSE files; no guest path discovery.
The command descriptor is write-only, other descriptors read-only, all direct
I/O with exact native inode IDs and size 2^52. Capsicum permits positional I/O,
seek, fstat, fstatfs and initially F_GETFL only; mutating fcntl commands are
removed. C4 must retain this exact F_GETFL-only CAP_FCNTL set, and retain the inherited AF_UNIX descriptor supplied by
its supervisor, never a replacement path. Bounds:
1024 complete native groups, 4096 mounted filesystems,
64 MiB executable, one child/endpoint/controller and one serialized control reader.

Required C4 startup order, based on actual `hfs9p_new` call sites:

1. Receive TUPLE with a bounded `recvmsg`, rejecting missing/extra rights,
   truncation and any unexpected frame/endpoint/controller.
2. CLAIM the fresh zero digest once; retain its authenticated ownership witness.
   Nonzero digest is blocked; it cannot manufacture host ledger restoration.
3. Capture actual local fstat/fstatfs/F_GETFL/Capsicum facts for the received
   descriptors and send VERIFY. The exact live child may issue authenticated
   GETATTR/STATFS for these private nodes before VERIFY; no data I/O is admitted.
4. Invoke `hfs9p_new`. Its `_native_fds` needs authenticated fstatfs before
   `_construct` calls `verify_cb`, then `claim_cb`. `claim_cb` must consume the
   actual one-use ownership witness already obtained above, not claim twice or
   accept a configuration boolean. `verify_cb` performs a fresh VERIFY every time.
5. Keep the sole C object/claim witness exclusive. Protect its real forwarding
   ledger using C4's owned directory adapter before any native effects. This Go
   management implementation never supplies replacement semantic filesystem I/O.

Each 40-byte observation has uint32 child fd, uint32 canonical flags (bit 0
write-only, bit 1 read-only, bit 2 O_DIRECT; no other bits), uint64 native device,
uint64 inode, uint64 size, uint32 exact rights-set selector (1, F_GETFL-only
CAP_FCNTL set), and four zero reserved bytes. Observation sequence
must equal the frame sequence, controller/endpoint must equal the issued tuple,
and all actual numeric identities, flags and sizes must match the supervisor's
prevalidated tuple. Child descriptor numbers must be distinct and remain stable.
These are observations from the source-pinned trusted native C adapter's real
syscalls over its inherited channel, never guest facts or an asserted boolean.
C4 must implement those observations, including denial on any failed syscall.
Its narrow owned `_native_fds` integration must preserve CAP_FCNTL restricted to
CAP_FCNTL_GETFL, rather than the current helper's unconditional removal of that
right. FreeBSD's `kern_fcntl(F_GETFL)` requires CAP_FCNTL. That C4 source change
is necessary before runtime use; this task does not modify lib9p sources.

The C adapter must refresh VERIFY immediately before **each** actual pread or
pwrite, including multi-chunk loops, and serialize observation plus I/O. A fresh
ordered observation funds one subsequent raw READ/WRITE callback only. The
callback privately binds it to the actual kernel Unique, node and operation;
reusing a consumed observation refuses data I/O. Metadata callbacks do not
consume it. A newer verified observation replaces an unused one. Existing
transport workers preserve the authenticated original native observation and
recheck live incarnation/controller without reissuing or replanning requests.
The host does not run `kern.proc.filedesc`, fstat on its private FUSE tuple or
other recursively forwarded metadata calls from its callback verifier:
FreeBSD `vn_fill_kinfo_vnode` itself invokes VOP_GETATTR. Process observation
uses KERN_PROC_PID/GROUPS/PATHNAME only, compares the retained text vnode/start
incarnation and the expected pinned path, and never stats an arbitrary changed
executable path. Native destination proof remains the real shell's kernel
connection/node/header; local fd facts remain the trusted C adapter's duty.

Linux implements only process/file/socket primitives for local tests. Its native
mount constructor always refuses. FreeBSD process/full-group/file ownership uses
actual libc sysctl/kinfo ABI through cgo; CGO-disabled FreeBSD builds
refuse process observation. Local tests do not mount, install, access VMs or
prove native kernel runtime behavior. Native execution and full deployed ABI /
artifact acceptance remain xz3/zq3 gates.

## Live controlled Facts and verification receipts (C5)

`ObserveAdmission` executes the fixed embedded read-only Linux probe through a
SHA256-pinned SSH executable, private pinned known-host bytes, an operator-fixed
direct guest hostname and private root identity. The intended observer runs on
the supervisor host, where the guest is directly reachable. It does not accept
caller observation JSON, a guest-selected destination or a replacement socket.
OpenSSH closes inherited fds, so one bounded private `/tmp` directory holds the
verified host/key copies during each probe. Each process has a ten-second bound,
256KiB output bound, and no shell interpolation of configured guest bytes.
No observation command has been run against a lab guest by this implementation.

Initial admission measures the actual Linux `boot_id`, running kernel GNU build
ID, complete loaded-module inventory and each module's running build ID. It
hashes the real installed kernel/module files and matches the operator-pinned
SHA256 of `/etc/homelabfs/installed-admission-v1.json`. That closed manifest is
`{version:"homelabfs-installed-artifacts/1",kernel:{path,sha256,build_id},
modules:{name:{path,sha256,build_id}},abi:"homelabfs-stop-adoption/1"}`.
Every loaded module must be listed; unsupported notes/ABI or missing files refuse.
These are measurements inside the already adopted trusted guest-root/kernel
scope, not hardware attestation or independent verification of physical behavior.

The admitted kernel/provider must implement an atomic read-only sysfs record at
`/sys/kernel/homelabfs/admission_v1`, bounded to128KiB, with exactly:

* `version:"homelabfs-kernel-admission/1"`, actual `boot_id`, a nonreused
  `provider_nonce` UUID, `physical_contract:"homelabfs-stop-adoption/1"`;
* `domains`: the complete stable ordered list of original Contexts, including
  stopped/uncertain old mounts; `domain_protocols`: exactly one session-keyed
  selected Protocol per domain, equal to its signed original enrollment;
* `observations`: exactly one `{scope,revision,fence,phase,event_hash,
  acknowledgement_hash}` per domain. LIVE has empty hashes. Other phases are
  the exact outgoing ACK names STOPPED, INSTALLED_DRAIN, DRAINED,
  INSTALLED_FINAL or REOPENED. `event_hash` hashes the complete canonical issued
  host event; `acknowledgement_hash` hashes the complete canonical ACK **payload**.
  The kernel publishes a phase only after completing its actual physical duties
  and before sending that ACK. STOP closes admission and joins entrants before
  freezing all adopted state; adoption/final PTE installation requires LIVE plus
  unchanged revision/target; reopen increments revision before reopening gates.

The nonce and every original domain/session remain owned until actual protected
retirement; unload/reload cannot reconstruct old identities. Initial host
admission requires LIVE/revision1/fence1. The source implementing this sysfs ABI
belongs to S04; absent support currently refuses production construction. No
kernel or installed module implementing it is claimed here.

The supervisor protects the exact initial measurement and native exporter/tuple
binding in exclusive `guest-admission.json`. It never reopens that file as live
authority. Refresh remeasures running notes, manifest identity, boot, nonce,
complete inventory and phase records; it does not rehash large on-disk artifacts
whose exact installed/running correspondence was established at initial admission.
Ordinary requests reuse that bounded static admission for at most five seconds
and still check the actual live native exporter/endpoint on every callback.
Every VIEW_ACK refreshes before entering controller/service locks. One Prepare
has a shared ten-second bound across all guest observers. This conservative
per-barrier SSH latency has no native calibration or pressure acceptance claim.
Failures, identity changes, revision rollback and old-revision reopen fence
admission and retain obligations. The fifteen-second admission freshness limit
never retires resources. An exact old ACK retry succeeds only through the
controller's already accepted protected archive; a stale measurement cannot
authorize another physical phase. Granted-but-unproven CANCEL stays blocked;
no plain cancellation request is treated as quiescence proof.

`NewFacts` verifies the complete signed-domain/service inventory;
`Authorize` requires the private actual `Service.Execute` observation and native
recheck. Physical checks bind exact observed ACK/event/phase/fence and complete
reservation/manifest/source obligations. Universal unadopted exclusion checks
every omitted reservation, refusing bound/frozen omissions. Initial recovery
accepts only an empty fresh controller; existing ownership requires C9.

An actual worker `ControlledCut.ViewSource` capture mints a private live service
seal over complete catalog/payload state, applied position, original journal
prefix and visibility. `VerifyControlledSource` verifies the full retained bytes
and rereads every original prefix frame through the backing OS roots under a
nonblocking source read cut. Writers/capture/flush/close hold the matching write
cut. It never queues another service request or walks FUSE paths. This is trusted
live-capture provenance, not independent historical replay. Serialized/reopened
sources lose the seal and refuse until C9 reconstructs correspondence. The seal
contains immutable metadata/digests, never aliases State bytes, has no global
registry, and lives/dies within the controller's existing bounded source slots.

The controller privately supplies its actual complete retained source set to
Facts, comparing every source to the raw ledger before checking it. No caller
JSON creates that provenance. `NewProducer` binds the actual supervisor, Facts,
controller and provider objects (including the provider's actual Facts owner)
and exact enrolled host endpoint signing keys.
After dispatch releases locks it verifies source/event evidence or the real
protected complete original retirement coverage, then signs the closed receipt
over original command/response hashes, cell, scope, endpoint, controller and
serving generation. All targets are CONTROLLED_LOCAL; publication remains null.
Non-authoritative status replies carry null verification. READDIR_CLEANUP is
authority reduction and creates no SOURCE/EVENT/FLOOR receipt. Only the explicit
signed1.2/1.3 family tuples can be measured; codec selection of1.3 additionally
requires its separately maintained successor implementation.

Current bridge binding accepts one actual per-scope
`controlledvalidation.Provider` as Claims and Execution. The Facts/observer can
check complete inventories, but these component tests do not assemble multiple
providers behind one endpoint. C10 must add a concrete Claims/Execution router
whose actual per-scope provider ownership is independently verifiable by the
producer; an asserted provider map, copied enrollment or a successful generic
binding callback must not broaden this check. The native endpoint remains issued
once to one bridge controller, so constructing another bridge is no substitute
for that router.

Local tests use the real maintained probe against disposable file/process
primitives, actual supervised child credentials, service/journal/controller and
provider ledgers, real producer and canonical signatures. Mount/INIT/header,
guest credentials and SSH/sysfs are declared OS/kernel primitive boundaries.
They reject altered installed/running/source/journal bytes, missing old domains,
forged ACKs, false frozen omission and old-revision reopen, and exercise complete
physical phase sequencing and original floor/lost-reply verification. They do
not establish an installed guest, native FUSE, real PTE/TLB behavior or readiness.
