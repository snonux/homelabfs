# Why HomelabFS exists

[Project overview](../README.md) · [Architecture](Architecture.md)

The goal is simple: highly available file storage for my home lab, built on the FreeBSD and ZFS foundations I already use.

My f3s setup runs Kubernetes inside Rocky Linux bhyve VMs on FreeBSD hosts. The pod workloads need ordinary persistent files: directories, permissions, locking, renames, and reliable sync behavior. When a workload moves to another node, its storage should follow and recover automatically. HomelabFS is intended to provide that while retiring NFS from f3s.

I have not found an existing solution that fits this combination of platform, filesystem semantics, and operational scope well enough. That is the motivation for exploring HomelabFS.

## Why the existing options do not fit this setup

- **GlusterFS:** FreeBSD maintenance is a concern. The FreeBSD Ports tree records the removal of `net/glusterfs` on 2025-11-06 because the port was broken. That makes it an unsuitable maintained foundation for this project today. [FreeBSD ports removal record](https://github.com/freebsd/freebsd-ports/blob/main/MOVED).
- **Ceph:** CephFS provides POSIX file storage, but the wider Ceph stack is more operational complexity than I want for this home lab. It involves monitors, managers, storage daemons, and metadata servers for CephFS. This is a scope and maintenance choice for f3s. [Ceph components](https://docs.ceph.com/en/latest/start/ceph-cluster-components/).
- **Garage and S3 object storage:** useful for applications that speak S3, but adapting ordinary pod filesystem access to an object API adds too much friction here. Garage explicitly lists POSIX/filesystem compatibility as a non-goal. The workloads need mounted files with the required semantics, without application rewrites around object storage. [Garage goals](https://garagehq.deuxfleurs.fr/documentation/design/goals/).

## Design goals

1. **Build on FreeBSD and ZFS.** Keep local storage, encryption, and external snapshot tooling on the existing hosts.
2. **Provide filesystem-compatible persistent volumes.** Support the POSIX-style operations and traditional Unix ownership/permissions required by the actual Kubernetes workloads.
3. **Keep normal I/O local.** Each workload directory has one active primary; different nodes can serve different workloads simultaneously.
4. **Recover automatically from routine failures.** Replicate continuously, let either replica become primary after safe catch-up, and bring returning nodes up to date without manual repair in the usual case.
5. **Keep operation manageable.** Start with three nodes, allow expansion, reuse WireGuard, and keep coordination out of normal reads and writes.
6. **Make recovery explicit.** Preserve replayable journals, test external snapshot restores, and provide a controlled single-node emergency mode when manual intervention is necessary.
7. **Make tradeoffs visible.** The synchronous partner may acknowledge in memory for speed. That deliberately accepts a documented multi-failure data-loss window; it does not relax conflict prevention or permit silent stale recovery.

## Current scope

HomelabFS is in the concept phase. A focused design for my home lab still requires careful distributed-systems engineering and real application testing. POSIX compatibility, automatic failover, performance, and the selected virtio-9p/FUSE access path are goals to demonstrate before migrating real data.
