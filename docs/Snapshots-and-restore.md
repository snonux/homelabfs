# HomelabFS snapshots and restore

[Architecture overview](Architecture.md)

External FreeBSD tools own snapshot scheduling and retention; restore must coordinate with HomelabFS authority and replication.

## External ZFS snapshots and restore

Configure periodic snapshots of HomelabFS backing datasets using external tools on the FreeBSD hosts. This is a deployment requirement, especially while the filesystem is new. HomelabFS does not own snapshot scheduling or retention. The external tool, interval, retention periods, and any off-host snapshot replication remain to be chosen; nothing has been configured by this architecture work.

Snapshot coverage must include data, namespace metadata, journal, and recovery checkpoints in a mutually recoverable state. The recommended layout uses sibling data/journal datasets under each volume parent, captured together by external recursive snapshots; arbitrary independently timed snapshots of separate datasets are not a recovery contract. HomelabFS must document the required dataset coverage and replay procedure, and may expose a checkpoint/quiesce interface for external tooling if needed. A storage snapshot alone does not promise application-consistent database contents; validate crash recovery or use application-aware quiescing where required.

Restore procedure: stop or fence access to the affected volume, preserve the current state, restore or clone a selected snapshot, replay only journal history appropriate to the chosen recovery point, and reconcile authority before publishing the volume. Distinguish repairing one stale replica (which catches up to the current primary) from intentionally restoring a whole workload to an earlier point. For intentional rollback, prevent live replication from immediately reapplying the unwanted changes, establish a new authoritative recovery generation, and resynchronize the other replicas from that history. Never roll back a live serving dataset behind HomelabFS's in-memory state.

Periodically test snapshot restoration and application recovery. Local snapshots provide historical recovery points but do not survive loss of their containing pool; off-host backups or snapshot copies remain a separate recovery layer.

## Journal dataset coordination

[Journal-storage.md](Journal-storage.md) defines the recommended dataset hierarchy, recovery manifests, checkpoint barriers, and segment retention. Independent snapshot schedules may supplement paired recovery sets, but independently timed data and journal snapshots are not automatically compatible. External retention must account for snapshots retaining deleted journal blocks and for baselines pinned by active transfers.
