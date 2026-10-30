# HomelabFS

<img align="right" src="docs/homelabfs-logo.png" alt="HomelabFS logo: three rose-colored angular paths interlock around a central hexagonal volume, forming a geometric knot that represents replication." width="180" height="180">

**Concept phase — architecture notes and experimental compatibility probes. No distributed HomelabFS implementation is available yet.**

HomelabFS is a planned distributed filesystem for the f3s homelab. This project implements and validates it using disposable lab and canary volumes only. Production workload migration and NFS retirement are possible future steps in a separate project. It will use local ZFS storage on FreeBSD hosts, with replication and workload failover implemented in Go.

Each workload owns an independent directory/persistent volume. In normal mode, any admitted HomelabFS node may accept reads and writes for any volume, regardless of ownership, subject to permissions, current authority and readiness. Writes originating on different hosts must share a consistent order and commit protocol. A preferred owner receives committed changes in memory and can serve them before its own disk persistence. Ownership is a placement preference, not an exclusive right to write.

Periodically reconsolidate preferred ownership toward the FreeBSD host receiving the most requests for that volume. Pod relocation need not wait for an ownership move: non-owners remain able to accept reads and writes. See the [ownership reconsolidation policy](docs/Cluster-and-ownership.md#periodic-ownership-reconsolidation). These are design requirements; the distributed protocol is not implemented.

## Why HomelabFS?

The goal is highly available file storage for my home lab, built on FreeBSD and ZFS, with the POSIX-style file access my Kubernetes workloads require. Learning about distributed filesystems and having fun building one are also goals in their own right. Read [why HomelabFS exists and its design goals](docs/Motivation-and-goals.md), including why GlusterFS, Ceph, and S3 object storage do not fit this setup.

## Architecture

The root Go module contains the daemon scaffold and implemented local packages:
[`internal/fsapi`](internal/fsapi) supplies shared filesystem types and lifetime
primitives, [`internal/journal/record`](internal/journal/record/README.md) supplies
the versioned journal codec, [`internal/journal/segment`](docs/spec/Journal-segments.md)
persists verified streams, [`internal/storage`](docs/spec/Storage-implementation.md)
supplies stable-ID backing primitives, and [`internal/apply`](docs/spec/Apply-engine.md)
rebuilds an explicitly authorized genesis history into fresh roots.
[`internal/fsservice`](docs/spec/Filesystem-service.md) implements a bounded local
filesystem service with journal-first mutations, gated reads, Unix permissions and
local handles/advisory locks; normal commit authority remains external.
[`internal/checkpoint`](docs/spec/Checkpoints.md) supplies immutable local checkpoint
artifacts, and [`internal/fuseadapter`](docs/spec/FUSE-adapter.md) supplies a restricted
disposable direct-I/O frontend. Their validation profiles supply local evidence only.
`internal/failpoint` provides named
fault injection, `internal/vfs` supplies rooted local I/O and a deterministic
crash simulator, and `internal/labrunner` provides the external plan-bound test
controller. The [local daemon CLI](docs/spec/Configuration.md) implements node/volume
initialization, inspection and restricted foreground FUSE validation with its
controlled self-client. Normal distributed publication and replication/recovery
remain unavailable; starting `homelabfs` without arguments returns a usage error.
The separate `tools/poc/` loopback module is a compatibility probe, not the
distributed core.

With Go 1.26.x and Mage installed, `mage build` writes Linux/amd64 and
FreeBSD/amd64 daemon binaries under `bin/`. Build the controllers separately with
`go build -o bin/hfs-lab ./cmd/hfs-lab` and
`go build -o bin/hfs-smoke-driver ./cmd/hfs-smoke-driver`; see the
[lab runner](docs/Lab-runner.md) and [Stage 2 smoke runner](docs/Live-smoke-runner.md)
for their implemented boundaries and missing drivers/adapters.

`mage vet` and `mage lint` provide root-module static checks (lint reports a skip
if golangci-lint is absent). `mage test` runs the root-module unit tests;
`mage fuzzShort` runs each available fuzz target for five
seconds with one worker, but no root-module fuzz targets exist yet. Each test
invocation requires explicit approval under [AGENTS.md](AGENTS.md). The manually
dispatched CI workflow runs vet, unit tests and both cross builds; it never
starts the lab. Local unit tests and the Linux kernel probes recorded in the
[FUSE binding decision](docs/adr/0001-fuse-library.md) do not establish native
FreeBSD adapter readiness, distributed integration or live smoke success; those
validation gates remain open.

![HomelabFS architecture](docs/HomelabFS-architecture.svg)

[Open the SVG diagram](docs/HomelabFS-architecture.svg).

The default deployment has three storage nodes and three replicas per workload. The cluster can grow beyond three nodes; adding a node does not automatically increase every volume's replica count.

For normal writes, the proposed acknowledgement rule is:

- Persist the originating writer's local journal and transmit the complete ordered operation to the preferred owner in parallel.
- Require originating-writer durability and the owner's retained in-memory copy, plus the protocol's commit/visibility conditions, before acknowledging the workload. An owner-originated write requires a distinct synchronous partner's retained memory; the owner cannot count itself twice.
- Deliver every operation to the asynchronous replica, retaining and replaying backlog across disconnects and ownership changes.

An in-memory peer ACK is an intentional durability tradeoff. Losing the originating writer's durable storage together with the required peer's unpersisted memory can lose acknowledged writes. Receipt alone is not commitment. The multi-origin commit, fencing, read-coherence and recovery protocols still need specification and testing.

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

When a pod moves to another worker, its requests may use that host before preferred ownership moves. Periodic request-based reconsolidation can later move the preferred owner through a coordinated handoff. Crash failover is a separate recovery operation: affected I/O must wait for valid authority, fencing, authoritative recovery and required peer readiness. Returning nodes do not automatically reclaim ownership. Automatic failover means workload recovery with the same volume, not zero interruption or process-memory migration.

## Operating model

- **Normal operation:** at least two ready replicas per volume, with no silent downgrade to local-only writes.
- **Emergency mode:** explicit activation permits one designated node to serve durable local writes. Excluded nodes must be fenced; rejoin and return to normal operation require manual admission and recovery.
- **Coordination:** etcd is planned for ownership and configuration, outside the normal per-operation I/O path.
- **Networking:** persistent replication connections over the existing WireGuard mesh are proposed, with an explicit admitted-node list and per-volume authorization.
- **Storage:** separate data/journal ZFS datasets are recommended, with checkpoint-based journal cleanup. External FreeBSD tools manage periodic snapshots and retention.
- **Compatibility:** traditional Unix ownership/permissions and POSIX-style behavior for the deployed f3s applications are requirements to validate, not current guarantees.

Routine failures should recover automatically when enough valid history and authority remain. Manual intervention is acceptable for emergency operation, ambiguous history, intentional rollback, or unavailable fencing.

## Initial test infrastructure

Three FreeBSD bhyve guests on f3 have passed the native FUSE/ZFS PoC checks. Each runs FUSE inside the guest. The separate Rocky 9p/FUSE regression also passed. See [lab operations, scripts, results, and current state](docs/Lab-operations.md); future test runs still require explicit approval.

The [deployment readiness prerequisites](docs/Deployment-readiness.md) cover canary isolation, bounded resource budgets, fencing, kernel support, recovery and test acceptance. They do not authorize production migration or NFS retirement.

## Design documents

Start with the [architecture overview](docs/Architecture.md).

| Document | Topic |
|---|---|
| [PoC setup runbook](docs/PoC-runbook.md) | Reproduce the kernel, bhyve, FUSE, and test setup |
| [FUSE and benchmark results](docs/PoC-fuse-and-benchmarks.md) | Combined-path checks and fio comparison |
| [Package distribution](docs/Packaging-and-rollout.md) | Build once and serve validated artifacts through pkgrepo |
| [f3 / Rocky PoC findings](docs/PoC-f3-rocky.md) | Stock-kernel blocker, working ELRepo/9p experiment, and I/O results |
| [Motivation and goals](docs/Motivation-and-goals.md) | Why the project exists and what it aims to provide |
| [Cluster and ownership](docs/Cluster-and-ownership.md) | Membership, placement, and coordination |
| [Replication](docs/Replication.md) | Write path, journals, and failover continuity |
| [Journal storage](docs/Journal-storage.md) | Dataset layout, checkpoints, and housekeeping |
| [Performance](docs/Performance.md) | Bounded concurrency, connection pools, and measurement |
| [Recovery](docs/Recovery.md) | Failure handling and automatic/manual recovery boundaries |
| [Emergency mode](docs/Emergency-mode.md) | Single-node recovery and controlled rejoin |
| [Kubernetes canaries](docs/Kubernetes-and-migration.md) | Canary volume publication, placement and scope boundaries |
| [Filesystem semantics](docs/Filesystem-semantics.md) | File operations, permissions, and workload compatibility |
| [Platform interfaces](docs/Platform-interfaces.md) | Shared Go core, FUSE adapters, and virtio-9p |
| [Security](docs/Security.md) | Encryption, admission, and authorization |
| [Snapshots and restore](docs/Snapshots-and-restore.md) | External snapshots and coordinated restore |
| [Test approval and automation](docs/Test-automation.md) | Per-run approval and unattended script execution |
| [Disposable FreeBSD lab](docs/FreeBSD-test-lab.md) | Three 2 GiB VMs, memory admission, cleanup, and Rocky restoration |
| [Live smoke tests](docs/Live-smoke-tests.md) | Stage 2 end-to-end checks from real Rocky guests |
| [Deployment compatibility](docs/Deployment-tests.md) | Separate 9p and Kubernetes lifecycle checks |
| [Integration tests](docs/Integration-tests.md) | Failure scenarios and acceptance gates |

Transport PoCs and initial benchmarks are recorded above. The distributed protocol and its fault-injection suite remain to be implemented. Validation has two separately approved, script-driven stages: native HomelabFS FUSE integration in disposable FreeBSD VMs, followed by live end-to-end smoke tests from Rocky after deployment. No distributed fault-test success is claimed.

## License

HomelabFS is licensed under the [BSD 3-Clause License](LICENSE). See [third-party notices](THIRD_PARTY_NOTICES.md) for dependency licenses.
