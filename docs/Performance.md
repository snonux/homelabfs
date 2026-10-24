# HomelabFS concurrency and performance

[Architecture overview](Architecture.md)

Design for parallel independent workloads from the start, with bounded resources and configurable connection counts. Add concurrency when measured bottlenecks justify it; do not weaken ordering, durability, or fencing to improve throughput. No performance measurements have been made yet.

## Work scheduling and connections

Goroutines and network connections solve different problems. Goroutines overlap independent work and waits; adding them does not create network bandwidth, disk throughput, or independent TCP streams. Multiple logical volume streams can share one framed connection, but a congested stream can delay traffic behind it.

Recommended initial organization per active peer pair:

| Traffic | Initial handling |
|---|---|
| Authority/session control and replication acknowledgements | Small persistent control connection with bounded priority queues |
| Current synchronous and asynchronous replication | Persistent live-data connection, multiplexed fairly across volumes |
| Historical catch-up and snapshots | Separate bounded bulk connection opened when needed |

This is a starting design to benchmark, not a fixed optimal count. Extra connections use the same WireGuard tunnel; separate tunnels are not required. Give every connection the same membership/session validation, deadlines, revocation behavior, and recovery generation checks. An ACK may arrive on another connection only after the corresponding data has actually been accepted under the protocol.

Allow a capped pool of additional live-data connections when independent volumes queue behind one busy connection. Prefer assigning volumes to stable connections over striping every operation across all sockets. Move a volume only with a defined handoff/barrier or sequence-aware reconnect. Even with ordered TCP, multiple sockets and retries require explicit stream sequence checks. Parallel arrival never permits acknowledging past a missing required prefix.

## Shared Go core

Use bounded per-volume queues and ordered state machines, with shared workers for independent encoding, transfer, persistence, and replay tasks. Multiple volume state machines may run concurrently. A single volume may pipeline operations, but commit/apply and filesystem dependencies retain their defined order. Do not require one global lock for all workload data.

Assign one framing writer per socket, or otherwise serialize complete messages, so concurrent goroutines cannot interleave message headers and bodies. Go permits concurrent calls on a net.Conn, but does not provide application message framing. Readers dispatch by volume/session/operation identity. Source: [Go net.Conn contract](https://pkg.go.dev/net#Conn).

Reserve credits in bytes and operations before admitting work. Bound global, per-peer, and per-volume queued and in-flight data, worker counts, and open connections. Retained partner data remains owned until safely persisted/transferred; buffer reuse must not invalidate a memory ACK. Slow disk or network applies backpressure rather than unbounded goroutine creation. Cancellation after operation acceptance does not imply rollback.

Increasing workers should respond to sustained queuing and available CPU/I/O/memory headroom. Shrink after an idle period, with hysteresis to avoid oscillation. Start with configurable limits and observability; automatic tuning is optional and must be justified by tests. Scaling down drains work; it must not abandon accepted operations or a required connection handshake.

## Disk and fairness

Run local journal persistence and partner transmission in parallel as required by the write contract. Pipeline a bounded number of operations. Evaluate group commit: several journal appends may share a sync, but each client waits for the covering durable boundary and its required partner receipt/commit conditions. Bound batching delay and preserve fsync barriers. Do not set ZFS sync=disabled or acknowledge unsynced journal data as a performance shortcut.

Prioritize live write latency and control traffic while guaranteeing background catch-up forward progress. Reserve a configurable bandwidth/I/O share for lagging replicas; starving them increases failover time and journal retention. Chunk snapshot transfers so one large transfer cannot monopolize resources. If continuing writes consume all catch-up capacity, throttle admission rather than promise eventual convergence without spare capacity.

Separate datasets do not isolate physical I/O on the same pool. Measure ZFS sync latency, pool saturation, guest 9p/FUSE overhead, CPU use, allocations/GC, and lock contention before blaming the network. Use Go profiling/tracing and runtime metrics; keep profiling endpoints restricted. Sources: [Go diagnostics](https://go.dev/doc/diagnostics), [Go GC guide](https://go.dev/doc/gc-guide).

## Measurement and tests

Measure p50/p95/p99 latency, throughput, queue wait, local sync time, partner receipt time, commit/apply wait, in-flight bytes, memory/GC, connection utilization, and async lag. Break down the entire pod → 9p → FUSE → journal/replication path. A rough write lower bound is max(local durable append, synchronous delivery/receipt) plus dispatch and any required commit/visibility work; it is not a latency promise.

Benchmark one and many volumes; small sync writes, metadata operations, sequential transfers, and database workloads; healthy and slow peers; and live traffic during snapshot catch-up. Compare baseline versus extra connections while holding durability, cache settings, workloads, and resource budgets constant. A throughput gain that makes tail latency or catch-up unacceptable is not automatically an improvement.

Additional integration scenarios: change pool size under load; kill one data connection while its control connection survives; deliver ACKs before local processing sees the matching data; reconnect with duplicate frames; pause one volume while others progress; overload the receiver; and scale down with accepted work in flight. Require bounded resources, preserved ordering, fair progress, and no loss of acknowledged operations outside the established durability gap.

## Initial transport measurements

The [f3 / Rocky fio comparison](PoC-fuse-and-benchmarks.md) measures direct host ZFS, host Go FUSE, 9p, and 9p plus Go FUSE using identical buffered jobs. These results establish a preliminary transport baseline; they contain no replication or journal overhead and are not production targets.
