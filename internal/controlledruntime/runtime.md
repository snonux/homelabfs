# Disposable controlled runtime (C10)

`cmd/hfs-controlled-runtime` selects a separate foreground profile:

```
hfs-controlled-runtime --config /disposable/runtime.json --disposable-validation
```

It composes the actual disposable volume service, one supervisor/exporter, one
unpublished raw FreeBSD FUSE shell, complete measured admission, each original
private provider, concrete `NewProviderRouter`, controller, Producer and durable
C8 Transport. The existing `homelabfs` configuration and restricted self-client
commands are unchanged. Linux production startup refuses before creating owners;
there is no command flag for substituted native/guest observations.

`RuntimeConfig` is the closed, case-sensitive Go JSON schema in
`runtime_config.go`, with `Version:"homelabfs-controlled-runtime/1"`. Configuration
and staged handoff files are bounded to512KiB, private, singly linked, regular
files owned by the daemon UID. Duplicate/unknown keys, field aliases, trailing
data and invalid UTF-8 refuse. No defaults select runtime capacities.

The config references an already initialized disposable version1 `VolumeConfig`;
it does not initialize/import a volume. `OwnerDirectory` must be new.
`EnrollmentFile` is an external operator handoff created atomically after the
proposal exists. `Exporter` pins the exact executable SHA256 and host-selected
argument array. Each selected bhyve device uses `hfs-controller=<session UUID>`;
there must be exactly one device for every configured original session. The
initial profile supports at most16 sessions of one VM/boot/export/volume under
one process, endpoint and FUSE mount. Other topologies refuse; no extra mounts
are created to compensate. `HostKeys` contains only host receipt private keys;
`GuestKeys` and `Operator` are pinned public keys. No operator or guest private
key is held by this command.

`Scopes` supplies operator-selected static scope identities. The actual service
journal/history/serving generation, exporter incarnation and FUSE session replace
their configuration placeholders in the inert proposal. An explicitly empty
`GuestBoot` may be filled by the external signer consistently across all domains;
that text is subsequently checked against actual guest measurements. Every other
proposal byte must match the signed enrollment exactly. This does not turn an
operator-supplied boot UUID into a measurement.

`Service` requires explicit queue/I/O/handle/lookup/reservation/lock limits and
exactly4MiB logical data. `Providers` requires finite claim/retained/source and
per-claim credits. `Views` fixes signed ticket/control/source/ledger/work bounds;
the controller independently checks its actual service metadata/workspace peak.
Capability live/identity/tombstone and credential quotas are separate explicit
fields. `Budget` separates host and guest envelopes. The host funds652MiB
transport arenas and parser plus the separate1GiB C8 mmap/serializer reservation,
service, exporter, view work, provider retained/source/identity copies and guest-copy owners.
Provider accounting reserves four decoded/ledger/serializer copies plus finite
identity metadata and16MiB per provider. Exporter admission includes64MiB pinned
executable,64MiB backend, two65MiB snapshot/protection buffers and16MiB thread/
management overhead; an actual native placement may require more.
The guest envelope includes explicit base, ordinary/control, reservation,
cookie and received owners, with a hard unchanged2GiB guest placement ceiling.
The semantic/exporter lease attaches to the supervisor before any start attempt;
`runtime-budget.json` protects its exact bounds. Close/failure never releases it
as if uncertain native/guest ownership were retired. Accounting is admission ownership, not physical memory availability or native
pressure acceptance. Real artifact/layout/allocator and unchanged4MiB canary
acceptance remain the xz3/zq3 gates.

## Staged bootstrap and readiness

1. Open the actual exclusive disposable service with controlled views required
   from construction. Start the source-pinned child with fd3 HSM1, fd4 executable,
   fd5 HSR1, fd6 private bootstrap pipe and fd7 owned ledger parent.
2. Dedicated bhyve constructors register fixed pending session UUID devices and
   return, allowing the VM to boot/network/provision. One128KiB C startup thread
   waits on the inherited pipe. Pending vqueues complete a bounded tagged
   `Rlerror(EAGAIN)` and cannot enter stock filesystem callbacks.
3. Mount one unpublished shell, observe actual INIT/connection, capture the real
   current baseline and protect `proposal.json`. Emit
   `AWAITING_OPERATOR_ENROLLMENT`, with `publication:null` and no readiness claim.
4. Within `BootstrapSeconds` (1..120), the external operator signs the exact
   aggregate and each domain, installs the signed C6 provision in the booted
   guest, and atomically supplies `RuntimeEnrollment` containing Authorization,
   provider Authorizations, complete canonical Provision and pinned GuestObserver.
   The driver verifies all original/outer signatures and independently measures
   actual running kernel/modules, boot/provider nonce and complete domain inventory.
5. Construct the actual semantic assembly and real C8 durable endpoint, bind the
   shell and export only its prevalidated tuple. HSB1 supplies all exact scopes
   through fd6. C invokes strong `hfs9p_supervisor_start_tuple` exactly once with
   the explicit terminal1.4 tuple, adds every session to the one real backend and
   claims each predeclared device. HSM1 opcode5 confirms each exact session under
   the actual endpoint/controller and fresh VERIFY. Missing/duplicate/foreign
   devices, failed native syscalls or missing scope coverage cannot become ready.
6. Emit inert `AWAITING_GUEST_BIND` with actual controller/provision hash. The
   external operator signs/installs the one-time C6 group binding and enrolls
   the configured guest devices. The driver waits for an authenticated, funded,
   completed/protected HELLO for every original scope and exact selected protocol.
   A plain session Hello flag, direct Execute, copied/decoded ledger or caller
   boolean cannot satisfy this check. One monotone per-scope witness commits
   with its immutable response in the same actual C8 transaction and survives
   normal cell reclaim. Private same-owner restoration binds its original cut;
   opening serialized witnesses supplies no native authority.
7. Refresh complete actual guest measurements and Facts once more. Only then
   emit `READY`, `profile:"CONTROLLED_LOCAL"`, full domain count and
   `publication:null`. The ongoing inherited control channel fences loss of its
   live owner. There is no normal publication or native acceptance certificate.

The credential attestor requires the private bridge VerifiedRequest, exact live
Admission/native controller, complete numeric sorted groups and the immutable
namespace/idmap from the complete operator/domain-signed provision. The adopted
trusted C6 kernel/root enforces that map against actual live mount/device and
captures request credentials. The host does not independently measure each
guest process or namespace mapping; the sysfs admission ABI supplies no such
per-request measurement. Hashes/signatures bind policy/bytes, not physical facts.

Bootstrap failure and cancellation fence the dedicated export and retain files,
native mount and VM resources. The driver never kills bhyve/Rocky, force-unmounts,
deletes failed resources or treats process exit as session retirement. A pending
or failed dedicated device always remains on the dedicated branch. Operator VM
lifecycle and normal unmount/recovery are separate approved operations. Unplanned
owner loss, replacement FUSE/exporter and guest-wire RESUME remain blocked; the
existing C9 path is limited to its same-surviving-owner barriered cut.

## Local evidence boundary

`assembly_test.go` uses actual service/journal/payload/private ledgers, complete
two-domain providers/router/controller/Producer, durable C8 and production mapped
framing. It verifies null Publication, all-session HELLO readiness and missing
adapters/old domains, wrong scope/protocol/controller, unbound provisioning and
budget refusals. Native mount/INIT/header, observed guest kernel/physical state
and external operator signing are explicitly declared OS/administrative fixture
boundaries. `controlled_test.go` opens the real initialized volume through the
same daemon lifecycle used by the command.

`driver_test.go` follows the shared command driver from actual proposal and
external signed handoff through actual supervised C strong startup/backend/ledger,
SCM_RIGHTS tuple, full device confirmations, signed guest-bind artifact, real C
Tversion/H9B HELLO, fresh Facts and READY. Missing device/service/native/host key/
provider/provision/observer, changed consent, unbound guest and one missing HELLO
all emit no READY; the C VM stand-in survives failures. Only private native
platform/fd/mount and observed guest physical boundaries are substituted; no
production setter/CLI test flag selects those substitutions.

The owned C bootstrap test exercises the actual asynchronous fixed pending table,
HSB1 parser, tagged retries and duplicate-inventory refusal while the process
survives. Production native construction still refuses Linux raw files. These
tests and C object linkage do not establish FreeBSD native FUSE/INIT, actual
bhyve boot, installed kernel MM/PTE behavior, artifact ABI, runtime pressure,
lab readiness or native canary acceptance. No lab/SSH/VM/mount/install operation
was run by this task. Any-node writes, original-origin D plus owner/partner RAM,
future periodic ownership reconciliation and same-client locks are unchanged.

## Process observer admission

Descriptor-backed launches also require the actual native
[`fdescfs`/`nodup` prerequisite](../../docs/lab/Descriptor-execution-window.md).
`--check-descriptor-exec` emits a separate read-only prerequisite report, including
unsupported profiles, with actual mount type/FSID/flags and null publication.
It starts no child and grants no readiness or host setup authority. Default
descriptor character devices and `rdlnk` pathname links refuse; there is no
path-execution fallback. Run checks this before service creation, and direct
exporter/mount/guest-observer entry points check before their side effects.

The native runtime requires a FreeBSD CGO build against its selected target
headers and libc/sysctl ABI. A FreeBSD `CGO_ENABLED=0` binary remains an
unsupported build. Startup observes and validates the daemon's own complete
credentials before opening the runtime service; every direct supervisor launch
repeats that capability check before copying an exporter, allocating inherited
channels or starting a child. A missing/denied observer fails closed with no
exporter child. A subsequent child-specific observation failure still preserves
any actually started child and consumed ownership for inspection.

The C observer uses the compiler's actual `struct kinfo_proc` layout and checks
both returned size and `ki_structsize`. Two process snapshots bracket complete,
bounded group and executable-path samples. It refuses mixed real/effective/saved
UIDs or primary GIDs, incomplete/changed groups, zombie state, changed PID,
birth time, text vnode or path, and any sysctl error. The owned executable's
protected descriptor and path checks remain mandatory after launch and on live
verification.

For a separately reviewed, bounded read-only qualification, the actual selected
daemon supports this mutually exclusive invocation:

```
hfs-controlled-runtime --check-process-observer
```

Stage that binary with mode0500 and one hard link under its intended runtime
owner. This invocation observes only itself, checks its complete numeric
credentials against the OS, validates the protected executable descriptor,
hashes that executable and repeats its identity observation. It reads no runtime
configuration, starts no exporter, creates no owner and mounts nothing. Its JSON
record identifies the exact executable SHA256 and observed process identity with
`publication:null`. Actual native execution and independently captured compiler,
header, runtime-library and source bindings are needed before an artifact can
consume this qualification. A compiled binary, Linux fixture, or C boundary
harness alone provides no native process qualification.

`process_freebsd_boundary.c` is an explicitly substituted sysctl boundary harness
compiled against the selected actual target headers. It checks ABI sizes,
credential/group/path races, zombie and stale snapshots, full1024-group capture,
oversized/partial groups and syscall errors without starting any process. These
fixtures are distinct from real native self observation. Neither read-only proof
establishes native READY, FUSE/filesystem behavior, installed PTE semantics,
pressure acceptance, publication or approval for VM operations.

## Bounded exporter diagnostics

Every actual supervisor launch captures stdout and stderr independently in
`OwnerDirectory/exporter.stdout` and `exporter.stderr`. These are exclusive,
private0600 regular files, capped at256KiB each. The runtime checks this owner
root against all actual volume/control/workspace/mount roots before launch.
Observer capability preflight still precedes executable pinning, inherited
channels, diagnostic creation and child startup. Failed capture setup or exec
preserves the partial files and consumes the existing launch attempt.

Exactly two drain workers each own one32KiB buffer. Each consumes its actual
output channel through EOF; after a cap or file-write refusal it continues discarding
incoming bytes, retaining an overflow/error indication. On actual FreeBSD the
channels are AF_UNIX stream socketpairs with SO_NOSIGPIPE on the child's two
inherited output sockets before exec. Each socket's send/receive high-water is
explicitly set and read back at no more than4096 bytes; unknown/excess bounds
refuse. The unused directions are shut down. The host's actual
`kern.ipc.sockbuf_waste_factor` must be1..8, bounding mbuf accounting for all
eight socket buffers to256KiB within the diagnostic reservation. Larger or
unreadable host policies refuse before child launch. No host sysctl is changed.
If the daemon/main exits or crashes, the actual child writes receive EPIPE
without a kernel SIGPIPE from these sockets. This does not fabricate successful
output or guarantee that every application ignores EPIPE. No ambient signal
policy or executable wrapper is introduced. Linux component tests use ordinary
pipes as an explicitly substituted OS boundary because Linux has no equivalent
SO_NOSIGPIPE option for inherited write descriptors; production Linux startup
still refuses. No diagnostic limit
kills, cancels, signals or waits for bhyve/Rocky. The child still uses
`exec.Command`; there is no `CommandContext`, WaitDelay or file-size rlimit.
A storage stall can delay its drain; this adapter does not claim disk I/O
latency bounds or uninterrupted VM output under a failed storage device.

`exporter-diagnostics.json` is one bounded4096-byte padded JSON status cell.
It records per-stream observed/stored bytes (observed counters saturate), EOF,
overflow and read/write errors, plus actual process-wait completion/outcome/exit code (-1 if unknown or signaled),
status-write errors and diagnostic `Complete`. Capture completeness requires
process exit, both channel EOFs and no lost evidence. It says nothing about runtime
readiness, successful child behavior, filesystem acceptance or retirement.
`WaitError` records the process outcome separately; a nonzero exit can still
have complete diagnostics. The in-process `Supervisor.Diagnostics` snapshot
reports these same inert facts. Status is checkpointed at setup/start, each
drain finish, process exit and Supervisor.Close. A live checkpoint is incomplete;
a missing, malformed, unsynced or stale cell supplies no final evidence. Output
may still be arriving after a live checkpoint, so its byte counters are not a
continuous external oracle. Overflow and I/O refusal never become a successful
truncation result. A terminal status WriteAt/Sync/Close error is sticky in the
live owner and returned by later Supervisor.Close even after the status
file descriptor closes. A failed checkpoint attempts one rewrite with
Complete=false/StatusError=true; it never clears the original error if that
invalidation succeeds. Failed storage cannot guarantee that this repair is
durable or even readable, so saved success bytes alone cannot certify evidence.
The retained first error is bounded and does not grow on repeated Close calls.

Process exit and EOF are independent. An inherited output descriptor owner may
outlive the supervised process; such a channel retains its drain and incomplete
status while this daemon remains alive.
Supervisor.Close only checkpoints and closes the existing management ownership;
it neither joins nor closes active diagnostic readers/files. The workers retain
the original runtime funding and their own descriptors until actual EOF. Final
status is written through its retained owned descriptor after Close if needed;
no path reopen or owner-lock reacquisition is used. A daemon crash/return
destroys its Go drains and leaves only its last checkpoint, never a fabricated final EOF. The FreeBSD socket policy separately
prevents that reader loss from signaling the surviving VM. External evidence
collection must treat uncertain/missing final capture as incomplete.

The existing274MiB minimum exporter memory budget is unchanged:64MiB pinned
executable,64MiB backend,130MiB C snapshots/protection,15MiB other management and
1MiB diagnostic ownership. That1MiB includes the two drain buffers, bounded
status serialization, worker stacks, bounded kernel socket-buffer bookkeeping
and descriptor metadata. The checked native buffer/waste-factor bounds spend at most256KiB of
that slice on socket mbufs. Overall allocator/RSS placement remains an actual
artifact admission check. The actual supervisor-held admission lease separately
charges the two workers and two output-channel connections (the historical accounting field
is named Pipes). The protected `runtime-budget.json` records
these fixed bounds, the528384-byte file cap and descriptor inventory explicitly.
It keeps its memory/worker/pipe charges after Close; local
closure is not proof that guest/native obligations retired. At most seven parent
capture descriptors exist during setup (three files and two output-channel
pairs); after exec closes the parent's two write ends, five capture descriptors
remain. There are no per-chunk goroutines or growing buffers/queues. These
accounting reservations are not proof of physical free memory or disk capacity.

Local subprocess/pipe tests exercise real capped output, separate stderr,
continued writes after management Close, quiet EOF, incomplete process-exit
versus pipe ownership, write refusal, existing-file preservation and failed
exec. Real-file wrappers inject positive partial-write/EIO and short/nil outcomes
while actual child stdout continues draining. Final status WriteAt/EIO and
successful WriteAt followed by Sync/EIO verify exact stored bytes, readable
success invalidation, sticky errors and error retention after descriptor close. A separate real-parent-exit regression execs an owned C child, positively
reaps its actual Go supervisor, then releases actual stdout/stderr writes from
an independent oracle. Under default SIGPIPE policy FreeBSD's inherited socket
options must produce EPIPE on both writes while the child survives. A negative
control disables only the disposable fixture's socket options and reports its
reaped writer's actual SIGPIPE death. Linux's positive fixture explicitly
substitutes SIGPIPE-ignore and establishes no FreeBSD safety. The test helpers
are disposable local processes and explicit OS boundary fixtures. Actual FreeBSD
execution is needed to qualify its socket safety; these fixtures do not start
bhyve or prove guest/filesystem behavior.
Private external C9 lifecycle/evidence operations and a real response-exposure
lost-reply fixture remain separate unimplemented prerequisites for the real9p
gate; diagnostic capture does not satisfy them.

`TestActualExporterDiagnosticsParentExit` retains the complete protected
`Supervisor.Start` integration. Actual FreeBSD attempts failed before child
startup with `fork/exec /dev/fd/4: no such file or directory`: that host lacked
fdescfs. These failed receipts remain failures. Task w24 owns the separate
FD-exec capability/preflight, reviewed host setup/restoration and full native
launch qualification; xz3/zq3 retain that prerequisite. Diagnostic transport
component evidence cannot pass this integration or claim runtime READY.

`TestDiagnosticComponentParentExit` is a distinct component fixture. It uses
actual `newExporterLogs`, the same production attachment/start/exit hooks and
actual FreeBSD socket policy. Only this test execs its real private,
source-pinned C file by pathname to isolate output transport from unavailable
host FD execution. It reaps its actual owner process before releasing the real
C writes, checks the same SIGPIPE negative/default-policy EPIPE positive and
requires the saved status to remain incomplete. This is no product fallback,
replacement native endpoint, observer substitution or `Supervisor.Start` claim.
Linux exercises both fixtures with its declared signal boundary. Actual FreeBSD
component execution separately qualifies only diagnostic socket behavior;
protected native launch/runtime and filesystem acceptance remain w24/xz3/zq3.
