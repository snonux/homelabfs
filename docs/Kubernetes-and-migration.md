# HomelabFS Kubernetes integration and canary volumes

[Architecture overview](Architecture.md)

Canary persistent-volume placement and the selected f3s guest access path. The filename is retained for existing links; production migration and NFS retirement are outside this project.

## Kubernetes persistent volumes

The validation use case is disposable Kubernetes test pods mounting dedicated canary persistent volumes on one worker, then being recreated on another worker after failure. Map each independent HomelabFS volume to a managed workload directory and replication group. Multiple volumes may be active on different workers simultaneously. Several pods on the same worker may share a volume when the application permits it; cross-worker access requires an explicit policy beyond the baseline.

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

## Representative patterns and isolation

The [canary volume mapping](Canary-volumes.md) assigns seven bounded canonical
volumes, their RW/RO views and complete placement sets to P1–P4. It records P5
parent aggregation as unsupported and defines fresh run identities, collision
preflight and manifest-bound cleanup. It creates no volumes and admits no profile.

Use selected f3s workload patterns to design synthetic fixtures, not to map or
move production data. Examples include a shared music reader/writer group,
Player-like multi-volume readers, aliases to one video directory, and nested
parent/child namespaces. Define supported placement or explicit rejection for
each fixture. Test same-owner co-location and multi-volume publication without
exposing passive replicas as current read-only copies. Remote earth access is
context for future design, not a required replacement service in this project.

Create fresh canary PVs/PVCs, test pods, namespaces or labels, volume UUIDs,
datasets and mount/export paths. None may alias `/data/nfs/k3svolumes`, an
existing workload PV, backing path or replication identity. Use synthetic test
data only. Cleanup acts on a durable run-owned manifest and checks identities.
Keep existing NFS, stunnel, CARP, mount monitors and workload mappings unchanged.
Canary installation on shared hosts needs a separately approved plan that
accounts for resources, kernel changes and possible effects on existing services.

## Canary acceptance

After native FreeBSD gates pass, deploy the selected canary artifacts under a
separately authorized plan and obtain approval for [Stage 2 smoke tests](Live-smoke-tests.md).
The baseline is bounded I/O on fresh canary volumes through the real Rocky path.
Exercise CSI with disposable pods/PVCs when that profile is selected. Record
versions, data integrity, replication progress, limits and cleanup results.
Disruptive guest/host fencing, power and restore exercises belong to separately
approved isolated infrastructure, not the live production workload hosts.
Document untested properties honestly; a canary pass is not production readiness.

## Possible future project

Production consumer inventory, data sizing, hardware expansion for full data,
application changes, remote-client replacement, migration/cutover/rollback and
NFS retirement belong to a separate project. Current evidence may inform it,
but no canary result authorizes those actions. Existing NFS remains operational.
