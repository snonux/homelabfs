# HomelabFS

**Concept phase — architecture and design notes only. No filesystem implementation is available yet.**

HomelabFS is a planned distributed filesystem for the f3s homelab, designed to retire NFS. It will use local ZFS storage on FreeBSD hosts, with replication and workload failover implemented in Go.

Each workload owns an independent directory/persistent volume. Its reads and writes normally happen on one primary node, while other nodes hold replicas. Different nodes can be primary for different workloads at the same time.

## Why HomelabFS?

The goal is highly available file storage for my home lab, built on FreeBSD and ZFS, with the POSIX-style file access my Kubernetes workloads require. Learning about distributed filesystems and having fun building one are also goals in their own right. Read [why HomelabFS exists and its design goals](docs/Motivation-and-goals.md), including why GlusterFS, Ceph, and S3 object storage do not fit this setup.

## Architecture

![HomelabFS architecture](docs/HomelabFS-architecture.svg)

[Open the SVG diagram](docs/HomelabFS-architecture.svg).

The default deployment has three storage nodes and three replicas per workload. The cluster can grow beyond three nodes; adding a node does not automatically increase every volume's replica count.

For normal writes, the proposed acknowledgement rule is:

- Persist the local journal and transmit the operation to the synchronous partner in parallel.
- Require local durability and the partner's in-memory receipt, plus the protocol's commit/visibility conditions, before acknowledging the workload.
- Deliver every operation to the asynchronous replica, retaining and replaying backlog across disconnects and ownership changes.

An in-memory partner ACK is an intentional durability tradeoff. Losing the primary and the partner's unpersisted memory can lose acknowledged writes. The precise commit, fencing, and recovery protocols still need specification and testing.

## Kubernetes and local access

For f3s, Rocky Linux Kubernetes workers run in bhyve VMs on the FreeBSD storage hosts. The selected access path is:

```text
Pod persistent volume
  → Rocky kernel 9p client
  → bhyve virtio-9p
  → FreeBSD HomelabFS FUSE mount
  → local ZFS and peer replication
```

The guest accesses the HomelabFS mount, never the raw backing directory. Linux FUSE remains a separate portability target around the shared Go core; it is not required inside Rocky for this deployment. The selected 9p/FUSE path still needs validation for the actual guest kernel, locking, mmap, caching, permissions, and durability semantics.

When a pod moves to another worker, the volume's primary can move with it. Either replica may be promoted, but workload I/O remains blocked until fencing, authoritative recovery, local application, and synchronous-partner readiness complete. Returning nodes catch up as followers rather than automatically reclaiming ownership. Automatic failover means workload recovery with the same volume, not zero interruption or process-memory migration.

## Operating model

- **Normal operation:** at least two ready replicas per volume, with no silent downgrade to local-only writes.
- **Emergency mode:** explicit activation permits one designated node to serve durable local writes. Excluded nodes must be fenced; rejoin and return to normal operation require manual admission and recovery.
- **Coordination:** etcd is planned for ownership and configuration, outside the normal per-operation I/O path.
- **Networking:** persistent replication connections over the existing WireGuard mesh are proposed, with an explicit admitted-node list and per-volume authorization.
- **Storage:** separate data/journal ZFS datasets are recommended, with checkpoint-based journal cleanup. External FreeBSD tools manage periodic snapshots and retention.
- **Compatibility:** traditional Unix ownership/permissions and POSIX-style behavior for the deployed f3s applications are requirements to validate, not current guarantees.

Routine failures should recover automatically when enough valid history and authority remain. Manual intervention is acceptable for emergency operation, ambiguous history, intentional rollback, or unavailable fencing.

## Design documents

Start with the [architecture overview](docs/Architecture.md).

| Document | Topic |
|---|---|
| [f3 / Rocky PoC findings](docs/PoC-f3-rocky.md) | Stock-kernel blocker, working ELRepo/9p experiment, and I/O results |
| [Motivation and goals](docs/Motivation-and-goals.md) | Why the project exists and what it aims to provide |
| [Cluster and ownership](docs/Cluster-and-ownership.md) | Membership, placement, and coordination |
| [Replication](docs/Replication.md) | Write path, journals, and failover continuity |
| [Journal storage](docs/Journal-storage.md) | Dataset layout, checkpoints, and housekeeping |
| [Performance](docs/Performance.md) | Bounded concurrency, connection pools, and measurement |
| [Recovery](docs/Recovery.md) | Failure handling and automatic/manual recovery boundaries |
| [Emergency mode](docs/Emergency-mode.md) | Single-node recovery and controlled rejoin |
| [Kubernetes and migration](docs/Kubernetes-and-migration.md) | Volume publication and NFS retirement |
| [Filesystem semantics](docs/Filesystem-semantics.md) | File operations, permissions, and workload compatibility |
| [Platform interfaces](docs/Platform-interfaces.md) | Shared Go core, FUSE adapters, and virtio-9p |
| [Security](docs/Security.md) | Encryption, admission, and authorization |
| [Snapshots and restore](docs/Snapshots-and-restore.md) | External snapshots and coordinated restore |
| [Integration tests](docs/Integration-tests.md) | Failure scenarios and acceptance gates |

The next steps are transport prototypes, a specified and tested replication/fencing state machine, and the integration tests described in these documents. No benchmark results or successful fault-test runs are claimed.
