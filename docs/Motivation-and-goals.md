# Why HomelabFS exists

[Project overview](../README.md) · [Architecture](Architecture.md)

The goal is simple: highly available file storage for my home lab, built on the FreeBSD and ZFS foundations I already use.

I also want to learn about distributed filesystems by building one, and have fun doing it. Exploring replication, consistency, failure recovery, and filesystem interfaces is part of the purpose of HomelabFS, even where an existing product might be the more practical choice.

My f3s setup runs Kubernetes inside Rocky Linux bhyve VMs on FreeBSD hosts. The pod workloads need ordinary persistent files: directories, permissions, locking, renames, and reliable sync behavior. When a workload moves to another node, its storage should follow and recover automatically. HomelabFS explores those capabilities with isolated test workloads. Migrating production workloads and retiring NFS would be a separate future project.

I have not found an existing solution that fits this combination of platform, filesystem semantics, and operational scope well enough. That is the motivation for exploring HomelabFS.

## Lessons from the f3s blog series

[Part 1: Setting the stage](https://foo.zone/gemfeed/2024-11-17-f3s-kubernetes-with-freebsd-part-1.html) explains the foundations: affordable self-hosting, control over the infrastructure, learning, and FreeBSD's ZFS integration. It initially proposed HAST-backed NFS. HomelabFS continues those goals with a different storage design.

[Part 6: Storage](https://foo.zone/gemfeed/2025-07-14-f3s-kubernetes-with-freebsd-part-6.html) records the practical constraints:

- **HAST:** an automatic failover left a ZFS pool unmountable in my setup; I suspected misconfiguration. I chose zrepl for snapshot-based recovery. This experience is not proof that HAST inherently corrupts ZFS.
- **zrepl with CARP/NFS:** minute-interval replication was an acceptable compromise, but automatic takeover was read-only. Enabling writes required manual intervention to avoid divergent datasets. Moving an IP did not solve data ownership.
- **NFS integration:** native FreeBSD/Linux NFS-over-TLS did not work in that setup, leading to stunnel. Stale mounts needed recovery automation; a later incident escaped mountpoint/stat checks and prompted write probes and escalation.
- **Shared storage:** the post avoided a single shared iSCSI dependency as a failure point; this is not a claim that every iSCSI design lacks redundancy.
- **Other candidates:** MinIO and MooseFS were future explorations. MooseFS was described positively for FreeBSD and POSIX support, not rejected. Its suitability remains an open comparison.

These are historical observations, not universal product limitations. HomelabFS uses these lessons to define canary tests for ownership transfer and application recovery. Replacing the existing system is outside the current project.

## Why the existing options do not fit this setup

- **GlusterFS:** FreeBSD maintenance is a concern. The FreeBSD Ports tree records the removal of `net/glusterfs` on 2025-11-06 because the port was broken. That makes it an unsuitable maintained foundation for this project today. [FreeBSD ports removal record](https://github.com/freebsd/freebsd-ports/blob/main/MOVED).
- **Ceph:** CephFS provides POSIX file storage, but the wider Ceph stack is more operational complexity than I want for this home lab. It involves monitors, managers, storage daemons, and metadata servers for CephFS. This is a scope and maintenance choice for f3s. [Ceph components](https://docs.ceph.com/en/latest/start/ceph-cluster-components/).
- **Garage and S3 object storage:** useful for applications that speak S3, but adapting ordinary pod filesystem access to an object API adds too much friction here. Garage explicitly lists POSIX/filesystem compatibility as a non-goal. The workloads need mounted files with the required semantics, without application rewrites around object storage. [Garage goals](https://garagehq.deuxfleurs.fr/documentation/design/goals/).

## Design goals

1. **Build on FreeBSD and ZFS.** Keep local storage, encryption, and external snapshot tooling on the existing hosts.
2. **Provide filesystem-compatible persistent volumes.** Support the POSIX-style operations and traditional Unix ownership/permissions required by the actual Kubernetes workloads.
3. **Keep normal I/O local.** Any admitted host can accept reads and writes for any volume. Periodically place the preferred owner at the FreeBSD host receiving the most requests, while preserving coordinated commits and non-owner access.
4. **Recover automatically from routine failures.** Replicate continuously, let either replica become primary after safe catch-up, and bring returning nodes up to date without manual repair in the usual case.
5. **Keep operation manageable.** Start with three nodes, allow expansion, reuse WireGuard, and keep coordination out of normal reads and writes.
6. **Make recovery explicit.** Preserve replayable journals, test external snapshot restores, and provide a controlled single-node emergency mode when manual intervention is necessary.
7. **Make tradeoffs visible.** The synchronous partner may acknowledge in memory for speed. That deliberately accepts a documented multi-failure data-loss window; it does not relax conflict prevention or permit silent stale recovery.

8. **Learn and enjoy building it.** Use the project to understand distributed storage through implementation, experiments, and failure testing.

## Workload and operational fit

[Part 8: Observability](https://foo.zone/gemfeed/2025-12-07-f3s-kubernetes-with-freebsd-part-8.html) describes persistent filesystem paths for Prometheus, Grafana, Loki, and Tempo. These belong in the application test inventory alongside databases and file services. Mount success alone is insufficient: workloads must recover their stored state.

[Part 9: GitOps with ArgoCD](https://foo.zone/gemfeed/2026-04-02-f3s-kubernetes-with-freebsd-part-9.html) describes ordering persistent volumes before dependent applications. HomelabFS canary publication must preserve that deployment discipline and add actual storage-readiness checks; a Kubernetes object existing does not prove a volume is safe to serve.

[Part 10: A New Home](https://foo.zone/gemfeed/2026-09-01-f3s-kubernetes-with-freebsd-part-10.html) describes separate local, offsite, and encrypted cloud backups. HomelabFS replication complements that recovery strategy. It does not replace backups or external ZFS snapshots.

## Current scope

HomelabFS is in the concept phase. A focused design for my home lab still requires careful distributed-systems engineering and real application testing. POSIX compatibility, automatic failover, performance, and the selected virtio-9p/FUSE access path are goals to demonstrate using synthetic data and disposable canary volumes. The deliverable is tested implementation, canary evidence and documented limitations; no production cutover or NFS retirement is included.
