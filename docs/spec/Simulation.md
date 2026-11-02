# Deterministic core simulation

`internal/sim` composes the actual `transport.Endpoint`, static cluster registry,
journal codecs, `fsservice.Service`, and `vfs.Simulator` inside one process. It is
a disposable core fixture, not the distributed replication protocol. The normal
replication/recovery state machines remain future work. The implemented
[external history oracle](History-oracle.md) checks bounded serial workloads
against real local service recovery. The separate concurrent observation oracle
below checks overlapping external histories; distributed acknowledgement/receipt
scenarios still require the pending protocol engine.
Passing this fixture does not grant workload readiness, fencing, native FUSE,
WireGuard, VM, ZFS, deployment or production durability evidence.

## Controlled execution and replay

A scenario creates one `Scheduler` with finite step, pending-event and network
byte limits, and one `Network` with its recorded seed. It constructs N `Node`s
with the real admitted registry and distinct numeric addresses. `Restart`
constructs a fresh actual Endpoint. An optional `Factory` constructs real core
services using that lifetime's `Runtime.Disk`, timestamp clock and labelled
random source. `service_test.go` demonstrates a complete local-only service
fixture, real journal genesis, requests, crash and replay; it does not implement
a second filesystem.

The scenario is the single scheduling authority. It starts one core operation,
waits for an explicit named boundary, chooses delivery/lifecycle/fault commands,
and observes completion before starting another operation on the same actor.
Endpoint reader/writer goroutines may arrive in any order. Their arrival order
does not select the next simulated event, consume a shared random stream, assign
trace sequence numbers or advance virtual time. `Step(ctx, key)` waits for the
exact requested event. It never interprets an empty queue as quiescence.

For example, the first real control handshake has these packet keys:

```
conn/1/0/write/1   # dialer HELLO
conn/1/1/write/1   # listener HELLO
conn/1/1/write/2   # listener HELLO_OK
conn/1/0/write/2   # dialer confirmation
conn/1/1/write/3   # listener final confirmation
```

`Await(ctx, key)` observes an enqueued packet without delivering it. `Pending`
after a completed Step reports a synchronous retry of that same packet. An
explicit `Advance` drives time without delivery. `Network.AwaitRead` observes a
core reader parked after installing its socket deadline, with no readable bytes
or reset and an unsignaled current wait generation. Data, deadline and clock
wakes invalidate the old park before it can be reused; `Clock.AwaitTimer`
observes an armed transport handoff/backoff timer of the exact requested remaining
duration, excluding handshake/dial context timers. Use those boundaries before
advancing a timeout. A delivery jump that would cross an armed transport timer
or socket deadline is rejected: explicitly Advance and observe expiration first,
then choose whether to deliver the stale packet. This avoids a race between Go
worker teardown and packet arrival at the expiry instant. No sleeps, goroutine
polling, real-time quiet periods or implicit auto-run loop select a schedule.

The context passed to Step/Await and the test's outer wall-clock timeout are
safety bounds for an invalid script, not simulated elapsed time. Such an await
failure poisons the scheduler. There is no determinism claim for unrestricted
concurrent scenario commands, concurrent direct service calls, concurrent Dial
allocation, or scripts that advance time before the intended wait is armed.
Factories must create their roots serially and install bounded, cancellable
hooks; all service workers belong to the returned closer. Independent components
receive independent labelled deterministic random readers. Production transport
continues to default to wall time and cryptographic randomness.

The trace contains seed-selected packet attempts/loss/duplicate suppression,
chosen delivery order, elapsed virtual time, directed link changes, node
lifetimes, controlled calls and failpoint controls. `Trace` returns an owned
copy. Scenario inputs and seed must be retained alongside this trace; the trace
is evidence of the selected execution, not an automatically executable program
or a success/retention oracle. Repeating an identical script/seed reproduces the
trace even with deliberate goroutine yield perturbations.

## Network abstraction

`Network` implements the existing transport `Network` with ordinary `net.Conn`
and exact numeric local/remote addresses. Real framing, greeting negotiation,
cluster/revision checks, session/incarnation pins, authorization and connection
classes still execute in transport. Missing protocol authorization continues to
deny stream messages. The tests' permissive fixture authorizer creates no
normal protocol authority.

Each connection is a reliable ordered byte stream. A framing write is one
packet abstraction with a stable connection/direction/write ordinal. Loss,
delay, jitter and packet duplication are sampled from that identity plus the
seed and attempt number. Directed partitions affect only the selected link.
Lost packets retry with finite attempts; a duplicate packet is suppressed below
the stream. Different connections can be delivered in any chosen order. Writes
on one connection stay in order because the transport has one framing writer
and each network write completes only after delivery. TCP congestion windows,
real packet segmentation and performance are outside this model. Packet
reordering here means scheduling across independent streams/retransmissions;
it does not inject application-level journal gaps or replay. Later protocol
scenarios must inject application message duplication/reordering explicitly at
that layer, preserving the actual codecs and acceptance checks.

Socket read/write deadlines, handshake/dial contexts, dispatch handoff expiry
and reconnect backoff all use the virtual clock. Due timers fire in deadline
then registration order. A context's first terminal cause remains fixed after
parent cancellation, explicit cancellation or deadline expiry; its cancellation
also propagates to ordinary context children. Local OS `TCP` callers retain
real-time defaults.

## Crash, history and pause

Every actor uses its own persistent VFS roots. Restart durably writes a fresh
synthetic incarnation to a private simulated control root before opening its
Endpoint. This models persistence ordering and lifetime identity, not the OS
lock/admission proof in `cluster.Start`/`normalnode.Start`. Normal startup is not
replaced or generalized. A factory must preserve historical journal creator IDs
and authority; a new transport incarnation does not rewrite source records.
The local validation fixture labels its source explicitly and supplies no normal
commit policy.

Crash first closes a generation fence on **all** old filesystem facades. No old
worker can reopen a root after the crash or finish a new disk operation across
that fence. Each root then executes the real VFS crash-recovery policy in sorted
root-name order. Failpoint blocks are released, local transport/services drain,
and their volatile instances are dropped. A new factory gets fresh facades and
reconstruction workspaces. Failed disk state and pending packets remain
available for inspection; no production files or OS disks are involved.

A local socket close queues `conn/<id>/<side>/reset`; the surviving peer learns
that loss only when the scenario steps this event. Already queued outgoing
bytes can arrive before that reset, even after their sender crashes and its
Send fails. Already delivered frames in a surviving receiver remain there.
Crash does not call a surviving Endpoint's revoke/incarnation reconciliation
API, clear its protocol memory, or assert that it observed remote RAM loss.
Restart does not automatically install a new expected peer incarnation.

`TestHistoricalACKSurvivesUnobservedPeerRAMLoss` carries exact actual journal
bytes in a real authenticated RECEIPT envelope. It exercises crash before ACK
delivery, and peer RAM loss after receipt but before a labelled client-reply
oracle boundary. The envelope and journal retain their original incarnation and
authority. These are executable fault schedules for a future replication oracle,
not an implementation or validation of receipt semantics or a successful normal
client write. A historical receipt cannot prove instantaneous physical remote
memory retention. In particular, this fixture does not inherit the TLA model's
observed-restart abstraction that synchronously clears an owner's knowledge in
the same step as its peer RAM loss.

Pause is defined only at controlled actor boundaries. `Pause` requires no active
`Call`; an in-flight call requires `PauseAt` on a simulator-owned, reached,
unreleased Block failpoint with exactly one live waiter in that current Call
scope. Historical Triggered/Hits, replaced activations and canceled prior calls
cannot authorize pause. The test control holds that waiter atomically, including
deferring cancellation/release until Resume (or fenced Crash). An unsupported
in-flight pause fails without changing actor state. While paused, commands are rejected, network reads/deliveries are
gated, timer notifications freeze, and the checkpoint cannot be released.
`Resume` applies elapsed virtual deadlines before the scenario releases the
checkpoint. An actual local fsservice mutation test proves that a blocked
pre-append operation cannot reply while paused and can complete after resume.
This does not suspend arbitrary Go instructions or preempt a worker between
uncontrolled instructions. Calling a service behind the actor's back invalidates
this scheduling contract.

Failpoint controls wrap the real `failpoint.Registry`. ReturnError and Block
are supported; Crash (`os.Exit`) and wall-time Sleep actions are rejected.
Simulated crashes/time advances use the scheduler instead. Existing core hooks
must be wired at their precise real boundaries; the fixture never invents a
partner ACK or normal readiness gate where no implementation exists.

## Resource limits and verification

Scheduler admission rejects zero/unbounded configurations. Each fixture admits
at most 16 networks; each clock retains at most 65,536 active timers. Trace keys
and details are bounded to 256 and 4,096 bytes; root names to 256 bytes. A trace retains at
most its configured steps (absolute maximum 1,000,000); pending events are at
most 65,536 and network-owned queued plus unread bytes at most 128 MiB. Each
fixture has at most 16 actors, each with at most 16 lifetimes, 64 roots, 100,000
storage calls and 8 MiB of cumulative write/truncate extent admission (including sparse gaps). A file is at most
8 MiB. Connections are capped by the pending-event budget and listeners at 256.
A link has at most 1,000 retries with bounded delay/jitter. Limits are conservative
admission counts, not performance measurements. Exhaustion fails closed and
never removes failed resources to make a scenario pass.

Focused local verification uses `GOMAXPROCS=2 go test -race ./internal/sim
./internal/transport` and static analysis. It uses only bounded process memory;
it opens no VM, host socket, FUSE mount, production volume or remote service.
The separate lab and canary execution approval requirements remain unchanged.

## Successor all-origin protocol model

The separate [AllOrigin TLA+ model](../../spec/tla/README.md#successor-all-origin-commitment-model-dz3)
checks finite all-origin grant/choice/result/publication and source-survey graphs.
Its two frontend-domain observations and independent backing apply expose the
RAM-before-disk contract to a logical history oracle. Its delayed ACKs and
unobserved RAM loss do not assume instantaneous remote crash knowledge.
The model's abstract atomic publication, dependency bundles and positive fences
are requirements on future runtime work, not behavior implemented by
`internal/sim`, the local service fixture or the retained v1 codec goldens.

## Concurrent all-origin observation oracle (`iz3`)

The separate `CheckConcurrentHistory` interface accepts externally observed,
overlapping **same-volume** calls from distinct origin lifetimes, including an
origin outside placement. It also accepts one fixed multi-volume dependency
closure. It does not change `History`/`CheckHistory`: those retain their serial,
local-service scope and all original regression cases.

`ConcurrentTrace` version 1 is the replay format. Supply its seed, fixed recovery
generation, complete member list, independently declared initial names/home
objects, invocation attempts and actual responses, atomic read observations and
fault schedule. Use `EncodeConcurrentTrace` and `DecodeConcurrentTrace` for bounded JSON replay.
Unknown fields and trailing documents are rejected. The checked encoder rejects
invalid UTF-8 namespace/principal strings before JSON can replace bytes; the decoder
also rejects invalid UTF-8 and unpaired surrogate escapes. Those byte-path profiles
are explicitly unsupported/inconclusive, never normalized into another operation.
Valid Unicode names preserve their exact spelling, including distinct normalization
forms. Binary payloads and canonical results use lossless base64. `Clone` owns all
nested payloads. Attempts have distinct recorder IDs. Ordinary operation UUIDs or
principal/anchor/session/request-number identities may repeat across receiving
nodes/incarnations. Preserve original externally observed ordinals; an adapter
must not replace overlapping invocations with its chosen internal grant order.

A call's terminal `Returned` ordinal bounds its linearization. `WaitUntil`, a
missing response, `PENDING`, and `INDETERMINATE` do not bound effect completion.
An unknown operation can linearize after its timeout, between later reads.
`ClaimedAt` separately retains observed immutable claim evidence; `PENDING` itself
establishes such a claim by its response time. Same-ID retries compare original
semantic arguments before path resolution, retain the first captured execution
credentials and original durable origin, and return one immutable terminal result. Anchored terminal replies require exact
canonical `StableResult` bytes in addition to the independently checked semantic
projection; later receipts cannot change their decision/result bytes. Actual
certificate authority remains the adapter validator's obligation. A changed
original receives conflict. Successful writes compare their actual
returned byte count and selected offset with an independently computed prefix;
unknown writes may have committed any short prefix, the entire input, or nothing.
An actually observed protected aborted result establishes no effects, including
when a prepare failure/DECLINE rather than an observed Cancel caused it. A wait
failure alone never establishes abort. Known terminal results remain constraints
even when an earlier or later attempt is unknown.

The independent model imports FSAPI data types, not storage, apply, fsservice or
implementation decisions. It computes append offsets, overwrite/zero-fill,
truncate/extension, namespace replacement, link counts, permission checks and
metadata from the original workload and current candidate logical state. Creation
accepts a fresh allocation identity as legitimate output nondeterminism, constrains
its home/nonreuse, and computes UID, parent GID, mode/umask and directory inheritance
independently. When an unknown create's allocation is not captured anywhere, a
fresh anonymous identity represents its existence without inventing observed ID
evidence. Captured identities and original object preconditions remain explicit
allocation alternatives. It does not copy an observed afterimage into expected state.
The projection captures logical size independently of returned bytes, and covers
regular files, fixed roots, nested directories, preserved
symlink text without following links, traditional owner/group/other permissions,
supplementary groups, sticky directories, set-ID policy, immutable homes and
predeclared nonexpiring handle/snapshot pins. Timestamps, revisions, native inode
numbers, ACLs, handle acquisition/expiry and symlink traversal are outside it.

Application `Transaction=true` rename/link crosses volume views atomically;
ordinary cross-mount operations return cross-volume failure. Directory moves
preserve descendant/home IDs, logical parent and both parent link counts.
Foreign aliases share one object's bytes and metadata; unlink preserves a pinned
zero-link home object. Every member/root/home must belong to the declared closure.
A diagnostic whole-closure snapshot compares all names/objects in one atomic
cut, rather than accepting different participant installations independently.
Ordinary POSIX adapters instead record one `ConcurrentRead.Probe` per actual
read, getattr, lookup, access or readdir syscall. Each has its own invocation/return
interval and explicit `ConcurrentFields` selection: uncaptured fields are absent,
not interpreted as zero. Reads compare their actual returned byte range, short
reads and EOF; getattr can capture only size/mode/UID/GID/link count or the other
listed stable fields. Access uses fresh committed permissions and supplementary
groups. Predeclared pinned descriptors can retain previously granted access after
chmod. A readdir captures one complete immediate entry list. These granular calls
may legitimately straddle publication, without requiring a cross-volume snapshot.
`CutKnown=true` additionally compares its ordered successful effect identities.
`CutKnown=false` explicitly omits cut evidence; omission never means genesis.
This oracle cut is a semantic effect history, **not** a normal-format certificate,
source vector, authority validator or proof of journal commitment. Adapters retain
and validate those actual certificates/source obligations separately.

Each granular observation represents one actual syscall/captured response. A whole
closure `View` is optional and requires an externally established coherent cut; it
is not legitimate to stitch separate concurrent syscalls into such a snapshot.
A read cannot claim both forms. Thus ordinary adapters can use data-range/stat/
access probes directly, while a diagnostic snapshot adapter has the stronger
coherent-capture obligation. Owner backing disk/apply may
remain at the declared baseline while successful observations reflect committed
RAM; no backing projection is used as expected truth. With only an unknown
invocation and an overlapping read, selected completion and premature candidate
exposure can be observationally indistinguishable. The candidate negative
therefore uses a genuine externally observed later abort, which cannot coexist
with an earlier read of its effect. Internal stage labels are not expected truth.

Owner/source loss, retained-memory loss and handoff records preserve the external
schedule. They never erase known outcomes or change the baseline. Missing required
history can yield an unavailable read with a reason and no fabricated snapshot;
this does not excuse a stale successful read before or after it. The accepted
origin-storage plus required-memory durability gap is not permission to invent
absence. Authorized loss recovery into another generation is deliberately
unsupported within one trace: retain the old trace and authorization, and start
a separate explicitly scoped generation/baseline. An ordinary crash/handoff cannot
silently obtain that exception. These are observation checks, not an implementation
or proof of source surveys, actual RAM retention, fencing or takeover activation.

The search explores legal interval orders, deduplicated claims, successful effects,
short-write choices and optional unknown completions. It admits sixteen attempts,
sixteen reads, eight closure members, thirty-two diagnostic faults, 256 names/home
objects, 4 KiB per write input, 8 MiB per file/view, 64 MiB captured binary data
per trace and a separate 128 MiB encoded-evidence cap. This supports a 4 MiB
canary baseline plus its observations; the encoded cap is not confused with raw
file size. The caller must select
1..1,000,000 search states and a cancellable context. `PASS` requires one **complete**
linearization satisfying every result/read. `FAIL` requires exhaustive rejection;
invalid/unsupported evidence, cancellation and resource exhaustion return
`INCONCLUSIVE`. A consistent partial prefix can never pass after a cutoff. This
existential finite-history check is not a liveness, durability or arbitrary-bound
proof.

`GenerateConcurrentPlan` reproducibly chooses conflicting operations on the same
predeclared file across at least two origins, using its own labelled random stream.
It generates **inputs only**. A real adapter must start the attempts before
releasing its controlled grant boundary, then capture real call/read evidence.
There is no new fake distributed executor. Replay reduced failures from their
retained trace/workload. `MinimizeConcurrentTrace` with a fresh-execution callback
can delete calls and reads, accepting only complete oracle failures from the
reproduced evidence. Invalid scripts/inconclusive runs do not qualify. Offline
mode removes only read observations: deleting writes while retaining their
observed bytes would manufacture smaller counterexamples. Baselines, seed and
fault evidence remain retained and are checked against fresh reproduction inputs;
a callback changing those inputs or unrelated call/probe identities cannot qualify.
Reduction is bounded to 128 attempts, without a
global-minimality claim.

The focused oracle target is `go test ./internal/sim -run '^TestConcurrent'`.
Hand-built positive/nearby negative histories exercise both append orders,
conflicting offsets, overwritten/short writes, truncate/extension, rename/chmod,
authorization at the ordered cut, cross-origin exact retries, known claims after
timeouts, later-aborted candidate exposure, closure/alias/home consistency,
owner-RAM-before-disk observations and unavailable-versus-stale loss/handoff reads.
The actual partial-probe profile starts with a 4 MiB declared file, overwrites its
tail, checks independent range-read/stat/access responses, serializes more than
4 MiB of encoded evidence and replays it through the checker. Nearby controls
reject stale bytes, wrong captured size/offset and stale permission success.
Additional controls reject altered anchored result bytes, lossy JSON namespaces,
invalid captured-field masks and unrelated minimization reproductions.
These tests test the actual oracle, not a distributed executor. They cannot be
relabeled as actual normal commit/lost-network-reply/failover/kernel evidence.
The pending runtime adapters for `hr3`, `br3`, `jr3`, `cw3` and `ov3` must supply
that evidence through this interface while keeping their own deployment gates.
