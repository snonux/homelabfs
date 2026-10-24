# HomelabFS architecture

**Live PoC:** stock Rocky 9 lacks 9p support and the matching-source module build failed. A one-time boot of ELRepo kernel-lt enabled 9p; basic I/O and SQLite WAL passed with cache=mmap. The FUSE layer remains untested. See [f3 / Rocky PoC findings](PoC-f3-rocky.md).

HomelabFS is a distributed filesystem for the f3s homelab, primarily on FreeBSD. Its purpose is to retire NFS from f3s by replacing NFS-backed workload storage with local HomelabFS access, replication, and automatic failover. The default cluster has three nodes, each providing local ZFS storage and running workloads. Additional nodes can be admitted, including a fourth node. This is an initial design, not an implemented or proven protocol.


See [Motivation and goals](Motivation-and-goals.md) for the problem HomelabFS aims to solve.

## Agreed direction

- Implement HomelabFS in Go, with a shared core and explicit FreeBSD/Linux platform adapters.

- Retire NFS from f3s. The target HomelabFS storage path has no NFS dependency; virtio-9p provides host-to-guest file access.
- For a given data set, normal workload reads and writes occur together on one active node. On failure, both move to a replacement owner. Different data sets can be active on different nodes.
- Support both FreeBSD and Linux FUSE from the start, using two platform adapters around shared filesystem and replication code.
- For f3s, expose the FreeBSD HomelabFS mount to each colocated Rocky bhyve guest through virtio-9p. Rocky uses its kernel 9p client; no HomelabFS Linux FUSE daemon is deployed in this path.
- Files and directories support traditional Unix user/group ownership and permission bits.
- Normal file access must support the f3s application portfolio with POSIX-style semantics. Compatibility is a requirement to validate, not a current certification.
- Use ZFS-backed passthrough operations where possible, with HomelabFS coordinating mutations and read freshness. Reads wait when the required change has not yet been applied locally.
- With three healthy nodes, every mutation is sent to the local replica, a synchronous partner, and an asynchronous partner. Every acknowledged normal-mode mutation must be recoverable on the synchronous partner, including the complete ordered prefix it depends on; an in-memory copy is sufficient for the partner ACK.
- Async delivery is mandatory and retryable, including metadata operations. Offline replicas retain catch-up obligations; asynchronous means outside the client acknowledgement path, not best-effort delivery.
- A returning former owner follows the current owner and catches up before serving any workload I/O. It does not automatically reclaim ownership.
- Periodic ZFS snapshots are required as an additional recovery layer. Scheduling, retention, and snapshot administration are managed by external FreeBSD-level tools, not HomelabFS.
- Normal f3s operation with HomelabFS requires at least two mutually reachable, usable storage nodes. Each successful normal-mode write requires a durable local journal entry and a synchronous partner's in-memory acknowledgement. There is no automatic single-node write mode. Explicit emergency mode is the controlled exception.
- Local persistence and transmission to the synchronous partner run in parallel.
- The synchronous partner may acknowledge receipt in memory and persist afterward. This is an intentional durability tradeoff.
- A replication log lets a node recover changes missed while offline.
- Concurrent writers on different nodes must not create conflicting file versions.
- Require an explicit admitted-node list. WireGuard membership alone grants no HomelabFS access; volume roles and emergency restrictions are authorized separately.
- Explicit emergency mode designates one serving node, permits durable local-only writes, and rejects automatic rejoining. Recovery requires manual peer admission, journal catch-up, and explicit restoration of normal replication.
- Coordination must stay off the steady-state read/write path. etcd was selected earlier; the revised recommendation retains it only for infrequent ownership and configuration changes, subject to the user's latency requirement. HomelabFS enforces ownership and maintains replication progress directly.

## Architecture diagram

![HomelabFS architecture overview](HomelabFS-architecture.svg)

[Open the SVG diagram](HomelabFS-architecture.svg).

## Design documents

| Document | Contents |
|---|---|
| [Cluster and ownership](Cluster-and-ownership.md) | Node admission, per-volume roles, ownership, and etcd |
| [Replication](Replication.md) | Write path, journals, catch-up, and failover continuity |
| [Journal storage](Journal-storage.md) | Dataset layout, checkpoints, retention, and safe cleanup |
| [Performance](Performance.md) | Bounded concurrency, connection pools, fairness, and measurement |
| [Recovery](Recovery.md) | Automatic recovery, failures, fencing, and manual escalation |
| [Emergency mode](Emergency-mode.md) | Single-node activation, exclusion, manual rejoin, and exit |
| [Kubernetes and migration](Kubernetes-and-migration.md) | PV publication, pod relocation, guest access, and retiring NFS |
| [Filesystem semantics](Filesystem-semantics.md) | POSIX-style behavior, permissions, and f3s applications |
| [Platform interfaces](Platform-interfaces.md) | Shared core, two FUSE adapters, and bhyve virtio-9p |
| [Security](Security.md) | Proposed WireGuard transport, node identity, authorization, and encryption |
| [Snapshots and restore](Snapshots-and-restore.md) | External ZFS snapshots and coordinated restore |
| [Integration tests](Integration-tests.md) | 41 scenario families, test harness, assertions, and rollout gates |

Diagrams live with the topics they illustrate: placement in Cluster and ownership; writes and failover in Replication; emergency recovery in Emergency mode; platform and guest paths in Platform interfaces.

## Decisions to make next

1. Map workload directories/PVs to replication groups and identify shared-volume exceptions, including rename across boundaries.
2. Confirm the local durability boundary, define write/fsync semantics, and specify the freshness barrier for reads that wait for catch-up.
3. Specify takeover, fencing enforcement, and recovery of acknowledged operations.
4. Specify emergency activation/fencing, manual admission and exit, retention limits, and safe group reconfiguration when adding nodes. Implement the failure policies above without automatic acknowledgement downgrades.
5. Choose a Go FUSE library supporting both platform adapters, prototype mounts on both real kernels, and validate the selected bhyve virtio-9p to FreeBSD FUSE path and f3s application semantics.

Suggested development order: validate the selected guest transport and both FUSE frontends; specify and exercise the commit/fencing state machine; local filesystem and journal replay; replication with fixed ownership; bounded queues and restart recovery; then automatic ownership transfer with partition/crash tests and Kubernetes integration. Follow the [integration-test matrix and rollout gates](Integration-tests.md#integration-test-matrix) before migrating real data.
