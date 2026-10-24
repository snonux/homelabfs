# HomelabFS Kubernetes integration and NFS migration

[Architecture overview](Architecture.md)

Persistent-volume placement, the selected f3s guest access path, and retirement of the existing NFS storage.

## Kubernetes persistent volumes

The primary use case is Kubernetes pods mounting persistent volumes on one worker, then being recreated on another worker after failure. Map each independent HomelabFS volume to a managed workload directory and replication group. Multiple volumes may be active on different workers simultaneously. Several pods on the same worker may share a volume when the application permits it; cross-worker access requires an explicit policy beyond the baseline.

In f3s, worker r0 is colocated with storage host f0, r1 with f1, and r2 with f2. A pod scheduled on r1 must use the HomelabFS copy on f1 through bhyve virtio-9p and the Rocky kernel 9p client. Volume ownership follows placement dynamically; this does not require permanent node pinning.

Desired recovery flow:

1. Kubernetes recreates the failed workload on a surviving worker. Prefer the current synchronous replica when feasible to minimize catch-up, but coordinate placement with storage readiness.
2. The volume integration requests ownership for the destination's colocated storage daemon. Do not expose a writable mount merely because the old pod was deleted from the API: it may still be running on a partitioned worker.
3. HomelabFS fences the old serving authority, recovers and applies the authoritative prefix, and establishes a synchronous partner for subsequent writes.
4. Only then does volume publication complete and the replacement pod gain access to the same volume identity and contents at its normal mount path.
5. A returning former primary rejoins as a replica and catches up from the current owner without reclaiming the workload.

Recommended integration target: a CSI driver coordinating volume publication with the HomelabFS daemons. The CSI node component would run in the Linux worker, while data stays on its colocated FreeBSD host. The guest filesystem transport is virtio-9p. CSI controller/node responsibilities, volume access mode, and scheduler coordination remain to be designed; the node component publishes the appropriate 9p-backed volume only after storage readiness. Existing hostPath-backed PVs alone do not provide this ownership handoff or fencing.

Failover means automatic workload restart with the same persistent data, not migration of a running process, its memory, or its open file descriptors. Kubernetes detection/rescheduling, storage recovery, and application startup all contribute to interruption.

The asynchronous replica is also eligible to become primary. It enters PROMOTING/CATCHING_UP with workload I/O blocked, obtains the authoritative recovery boundary from surviving replicas, and fetches and applies its missing history. Its local checkpoint alone cannot reveal updates it never received; lack of evidence of a gap is not proof of freshness. If the former primary failed, the surviving synchronous replica normally supplies the latest recoverable tail. After reconciliation, fencing, local application, and synchronous-partner readiness, it enters ACTIVE and releases blocked access. Kubernetes may wait for volume publication rather than start a pod with a usable mount; if a mount already exists, its I/O must remain gated during promotion. Do not return stale reads or acknowledge new writes while catching up.

If no surviving source can provide the required history, promotion remains blocked rather than silently accepting data loss. Fetching recovery data from another storage node is replication; subsequent workload reads and writes still use the new primary's local copy.

The partner roles above are relative to a write's owner. Partner replacement is a fenced configuration change with catch-up, not an arbitrary choice of whichever node answers first.

## FreeBSD host to Linux workload access

The f3s workloads run in Rocky Linux bhyve guests r0/r1/r2 on FreeBSD f0/f1/f2. Existing persistent paths are predominantly hostPath PVs below `/data/nfs/k3svolumes`, backed by NFS. A FreeBSD FUSE mount alone is not visible inside the guests.

Selected transport: share the FreeBSD HomelabFS mount through bhyve virtio-9p and mount it in the colocated Rocky guest using the Linux kernel 9p client. Kubernetes publishes workload directories from that mount after ownership and recovery readiness. The existing CARP-backed remote NFS path is not the intended HomelabFS data path. Exporting raw ZFS backing directories would bypass coordination and is forbidden. Validate the selected 9p-to-FUSE stack before deploying workloads.

Compatibility testing must cover the full path from a Linux pod through the guest transport to HomelabFS and ZFS, including credentials, locks, caching, sync, and workload rescheduling. Existing ReadWriteOnce hostPath declarations are not a replacement for storage-level fencing. Keep k3s and HomelabFS coordination storage independently bootstrappable.

## NFS retirement and migration

The [f3s storage history](Motivation-and-goals.md#lessons-from-the-f3s-blog-series) explains why replacing the existing stack must improve recovery as well as remove NFS. Require an end-to-end write/sync/read probe in a dedicated health-check directory on an active volume, with a deadline and cleanup. Run it through the pod's actual mount path. Report intentional catch-up or fencing stalls separately from unexpected failures; probes must never bypass the ownership gate. A failed probe alone must not authorize promotion or destructive recovery.

Retiring NFS from f3s is an explicit project goal. Existing NFS mounts, hostPath PVs, and paths such as `/data/nfs/k3svolumes` describe the current system, not a requirement of the new architecture. The replacement path is pod → Rocky kernel 9p client → colocated bhyve virtio-9p share → FreeBSD HomelabFS mount → local ZFS and peer replication. NFS is not a fallback in the intended final deployment.

Inventory all f3s NFS consumers and migrate their data and volume mappings. Preserve application-visible paths where useful, but do not infer the transport from a legacy path name. Validate ownership/permissions, application recovery, normal operation, and failover for each workload before retiring its old storage path. Coordinate cutover so old NFS clients and HomelabFS cannot write independent copies of the same workload data.

After migration and restore validation, remove NFS exports and client mounts and retire NFS-specific stunnel services, mount monitors, and storage failover configuration that have no remaining consumers. Review shared infrastructure before removal: CARP and other services may also serve unrelated traffic. Keep external ZFS snapshots and backups. A migration runbook and rollback procedure remain to be written; no deployment or NFS configuration has been changed.

## Post-deployment acceptance

After native FreeBSD integration tests pass and HomelabFS is deployed under the rollout plan, obtain separate approval for [live smoke tests](Live-smoke-tests.md) from the real Rocky VMs. Scripts exercise dedicated test volumes through the deployed end-to-end path before workload migration. No deployment event automatically authorizes smoke execution.
