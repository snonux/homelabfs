# Bounded admission and owned resources

Task `6r3` implements the process-local `internal/admission` component. The
[normal protocol](Replication-protocol.md), [view barriers](View-barriers.md)
and [journal retention](../Journal-storage.md) remain authoritative. This
component creates no serving authority or normal publication and supplies no
distributed protocol, network bandwidth scheduler, physical revocation or GC.

`New(Config)` validates capacities before creating maps. Each of six independent
pools (`Live`, `CatchUp`, `Recovery`, `Control`, `Drain`, `Memory`) has `Limits`
for global, per-origin peer and per-volume holdings. Checked sums, exposed by
`Budget.Limits`, are the aggregate caps. No class borrows another class's unused
capacity. This isolation applies at every scope: ordinary work cannot fill a
shared per-volume limit and thereby prevent a reserved dirty drain or recovery.
Catch-up capacity and round-robin dispatch remain available during live overload;
actual I/O progress also requires finite work, repeatedly invoked dispatch,
available storage/network and consumers honoring these classes.

`Resources` contains `Bytes`, `Operations`, `OwnerRAM`, `Journal`,
`Dependencies`, `Workers`, `Connections`, all unsigned and checked before
increment. `Bytes` counts all caller-owned memory; `OwnerRAM` and `Dependencies`
are disjoint subcategories contained in it. `Journal` counts additional durable
source reservation, including grant, payload, choice, result and retries. All
holdings cost at least one operation, including sockets and arenas. Queued and
retained holdings keep their complete bytes/operation/source credits; only
workers are deferred until dispatch. Sockets include pending connection setup
and must remain reserved until fully drained/closed by the caller.

Hard aggregate ceilings are 4 GiB for each byte/source dimension, 65,536
operation credits, 256 executing workers and 256 connections. Scope strings
are at most 256 bytes, with no implicit membership/placement check: authenticated
outside-placement origins use the same accounting as other admitted origins.
Every scope/queue record belongs to an operation-counted live lease; empty keys
are removed and sparse queue arrays shrink. Repeated rejected floods and scope
churn cannot accumulate historical records. Bookkeeping is bounded separately
by operation counts; counters are not exact RSS. Deployment sizing must include
Go heap/GC overhead, bounded bookkeeping, transport/kernel buffers and unrelated
host processes. Hard ceilings and `DefaultConfig` are component guardrails, not
approved canary capacity or calibrated SLOs. Defaults sum to 193 MiB of owned
byte capacity, 1,344 holdings, twelve workers and twenty connections. Every
default per-volume quota is half its pool's global limit, including workers and
connections: one stalled volume cannot consume the capacity required by a second
volume on the same peer. The default owner-memory/recovery per-volume limits are
64/16 MiB, sufficient for the logical-view defaults' 33/15 MiB arenas.

## Allocation and dispatch

`Reserve(Request)` is immediate and nonblocking. An error grants no allocation
permission. Memory views reserve complete baseline/candidate/pinned-version and
transient-copy arenas before copying any snapshot; recovery gets its own pool.
The logical-view store must retain this arena until every view/claim/source pin
is destroyed. It cannot free an arena because the current cut advanced or its
backing disk caught up.

`Enqueue(Request)` reserves the full future retained package before accepting
payloads. `Next` acquires worker credits and dispatches fairly among runnable
classes, volumes, then origin peers. It skips blocked queues, exhausted workers
and closures holding a mutating grant. Within an origin queue it also scans past
an unavailable independent closure. Fairness concerns already admitted bounded
work; it cannot promise acceptance to a peer after another fills a shared pool.
Configure peer/volume quotas below the global cap where admission isolation is
required. Shared pool tuning or automatic scaling is not implemented.

One outstanding mutation holds each exact closure key until `Finish` under the
external publication authority. Closure identifiers must be canonical and bind
the complete immutable dependency closure, not frontend paths or volume aliases.
The initial ordinary consumer charges one logical volume; a future transaction
consumer must reserve every affected volume before admitting a multi-home
operation. This package does not infer affinity/authority from a string key.

A conflicting final operation enters with `Blocked:true`. It owns no worker or
closure slot while dirty packages run through the separate `Drain` pool. Each
potentially writable page reserves its entire provenance/origin/journal/result/
memory/retry package before its writable PTE is installed, then calls `Accept`
before stores are possible. STOPPED, physical quiescence, frozen provenance and
intermediate/final installed-cut acknowledgements are supplied by the actual
frontend/protocol, never inferred here. After positive drain completion the
caller uses `SetReady(true)` on the final request. Reserving a final grant before
this sequence is a caller error, not an optimization this scheduler authorizes.

## Acceptance and release

`Lease.Accept(obligation)` binds an immutable, bounded external obligation.
Accepted cancellation, lost sessions, timeouts, authority transitions and fences
have no release side effect. Cancellation detaches the response waiter, while
the protocol continues reconciliation and owns the lease. There are no internal
contexts, timers, background goroutines or unbounded waiter queues.

`Finish(evidence)` releases workers and the closure slot while keeping the owned
bytes, journal space, dependency and operation credits. Accepted finish requires
injected `Policy.Authorize` for the exact `FinishGrant` action, independently of
later source release. This policy must verify protected results and completed
publication; source durability alone cannot open the next slot.

`Release` cancels/destroys unaccepted allocations only. `ReleaseAccepted` requires
the policy's distinct `ReleaseResources` approval for complete verified durable
equivalence or protected transfer. Accepted mutating work must finish first.
No built-in permissive policy exists. Evidence is at most 4 KiB and is opaque to
the component: the protocol verifies exact immutable references, current action,
source coverage, namespace and pins. Client reply, raw RAM ACK, local apply or
timeout evidence alone is insufficient. Releasing counters never deletes source
files or authorizes durable GC.

Policy callbacks must be bounded, nonblocking and safe for concurrent calls;
they may inspect the budget. They run outside its lock. A changed lease generation
or failed policy check preserves ownership. Callbacks must not mutate the private
transfer destination. `Transfer` first reserves overlapping destination credits,
asks for `TransferResources` authorization binding both holdings, then rechecks
source lifecycle and atomically changes accounting scopes in this same Budget.
The accepted obligation survives. Failure retains the old holding. Cross-process
handoff still requires actual owned bytes and the protocol's protected transfer;
a network receipt is not this local accounting operation.

## Component verification

Tests cover all seven resource dimensions at all three scopes, invalid/overflow
limits, bounded concurrent reservations, rejected floods and scope churn;
volume/origin fairness past a stalled accepted closure; reserved catch-up/control/
recovery/drain execution while live workers and queues are full; pre-dirty page
credit exhaustion and drain-before-final sequencing; cancellation, reentrant and
stale policy decisions, protected handoff and delayed release. The overload
component simulation keeps 64 actual 4 KiB payloads through 1,000 rejected requests
and cancellation, syncs replacement files plus their directory, verifies exact
bytes before releasing ownership and rereads after original buffers are cleared.
Its accepted operations are component holdings, not distributed acknowledged
normal operations. The separate simulator/network integration follows in `4r3`
and `hr3`, and actual D05/lab acceptance remains unexecuted here.

`BenchmarkSmallSynchronousWrite` records local 4 KiB reservation/dispatch, owned
buffer copy, file and directory sync and injected-policy overhead. It is
non-gating and measures no network, FUSE, 9p, distributed commitment or hardware
power-loss guarantee. Run only under the coordinating local verification budget;
results do not calibrate canary profiles.

On 2026-10-31 the coordinating local batch passed package build, race tests
(96.1% statement coverage), vet, errcheck, golangci-lint and FreeBSD/amd64
cross-build with CGO disabled. The race command used `GOMAXPROCS=1`, `-p 1`,
`-count=1`, `-timeout=90s` and a coverage profile. Its outer elapsed time was
1.717 seconds. These checks covered this component, not the final merged project
or any native runtime. Exact commands, exits, output and unchanged source
bindings are in `/tmp/hfs-admission-final-t1507me1/`.

The same batch ran the following non-gating command with `GOMAXPROCS=1` on
Linux/amd64, Intel Core i7-1185G7, using local `/tmp` **tmpfs** storage:

```sh
go test -p 1 -run '^$' -bench '^BenchmarkSmallSynchronousWrite$' -benchtime=100x -count=1 -timeout=30s ./internal/admission
```

Its 100 measured 4 KiB operations reported **4,717 ns/op**, **868.28 MB/s**,
**4,504 B/op**, **11 allocations/op**. File/directory Sync calls on this tmpfs
fixture measure component overhead; they provide no disk-persistence latency,
hardware durability, normal replication throughput or distributed SLO evidence.
