# HomelabFS snapshots and restore

[Architecture overview](Architecture.md)

External FreeBSD tools own snapshot scheduling and retention; restore must coordinate with HomelabFS authority and replication.

## External ZFS snapshots and restore

Configure periodic snapshots of HomelabFS backing datasets using external tools on the FreeBSD hosts. This is a deployment requirement, especially while the filesystem is new. HomelabFS does not own snapshot scheduling or retention. The external tool, interval, retention periods, and any off-host snapshot replication remain to be chosen; nothing has been configured by this architecture work.

Snapshot coverage must include data, namespace metadata, journal, and recovery checkpoints in a mutually recoverable state. In the intended `volumes/<volume-uuid>/{data,journal}` layout, external recursive snapshots of each volume parent capture its siblings together. The prepared lab and typed canary layouts instead require an explicit inventory of the actual run-owned data/journal/control datasets and coordinated capture under their checkpoint barrier; do not snapshot a shared parent containing unrelated runs. Arbitrary independently timed snapshots of separate datasets are not a recovery contract. HomelabFS must document the required dataset coverage and replay procedure, and may expose a checkpoint/quiesce interface for external tooling if needed. A storage snapshot alone does not promise application-consistent database contents; validate crash recovery or use application-aware quiescing where required.

Restore procedure: stop or fence access to the affected volume, preserve the current state, restore or clone a selected snapshot, replay only journal history appropriate to the chosen recovery point, and reconcile authority before publishing the volume. Distinguish repairing one stale replica (which catches up to the current primary) from intentionally restoring a whole workload to an earlier point. For intentional rollback, prevent live replication from immediately reapplying the unwanted changes, establish a new authoritative recovery generation, and resynchronize the other replicas from that history. Never roll back a live serving dataset behind HomelabFS's in-memory state.

Periodically test snapshot restoration and application recovery. Local snapshots provide historical recovery points but do not survive loss of their containing pool; off-host backups or snapshot copies remain a separate recovery layer.

## Journal dataset coordination

[Journal-storage.md](Journal-storage.md) defines the recommended dataset hierarchy, recovery manifests, checkpoint barriers, and segment retention. Independent snapshot schedules may supplement paired recovery sets, but independently timed data and journal snapshots are not automatically compatible. External retention must account for snapshots retaining deleted journal blocks and for baselines pinned by active transfers.

## Canary policy and restore acceptance

The [disposable canary backup policy](spec/Backup-policy.md) specifies bounded
snapshot/retention, complete recovery manifests, independent artifacts and
isolated restore acceptance for canary data/journal/control/test-coordination
state. Use test keys and separate writable recovery
resources; never modify existing backup policies or received sinks. Retain test
artifacts, trust material and configuration outside the canary storage, and verify
checksums, application integrity and recovery generations in approved isolated
restore runs. Existing lab replication is not evidence that these canary restores
passed. Production backup policy is outside this project. See [canary readiness](Deployment-readiness.md#release-acceptance-and-independent-recovery).
