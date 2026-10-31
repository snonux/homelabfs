# HomelabFS snapshots and restore

Status: all-origin backup/restore contract `gz3`, 2026-10-31. Runtime integration
and approved isolated restore evidence remain pending. External FreeBSD tools own
snapshot scheduling/retention; HomelabFS supplies verified recovery manifests,
barriers and pin/release interfaces. Snapshot interval, tool and off-host policy
for the target remain deployment decisions. Existing lab zrepl history is a
periodic VM-disk recovery copy, not proof of this contract or normal replication.

## Complete all-origin recovery sets

A clean recovery set binds an authenticated complete closure manifest to one
ordered selected cut and the complete issued-grant frontier. Inventory all exact
origin streams and durable source cuts, including outside-placement origins,
immutable grants/choices/results/claims, authority/promises, transaction decisions,
homes/handles, allocation/rejection ledgers and all transitive dependencies.
Retain explicit unresolved work until safely resolved or included as uncertain
recovery evidence. Owner RAM, local apply position and a source maximum are not
backup data or complete-history proof. Capture one of these two forms:

- A distributed set containing every required source dataset/control artifact at
  compatible exact cuts, plus a verified baseline and suffix/dependency bytes.
- A selected backup set into which the complete graph and payload bases have
  already been durably materialized and independently verified. It may replace
  original origin datasets only for the explicitly covered obligations.

For either form, pin source segments, baselines, homes/transaction/claim ledgers,
control evidence and every external snapshot/clone needed for replay **before**
capture. Gate the closure and obtain the complete sealed-origin survey or exact
verified durable equivalents and issued frontier for a clean closed cut; preserve
live RAM obligations before any voluntary stop. Coordinate with in-flight trim,
membership, capture and source-release transitions. External tooling acknowledges
holds, exact dataset/GUID/snapshot inventory, manifest/artifact hashes and completed
transfer verification. Publish the recovery set only after all components are
durable and verified. A partially captured/received set remains incomplete and
pinned; timeout never releases source protection or certifies a backup.

After a sealed authority close, normal operation resumes only through an
installed successor-authority transition and fresh view barriers; capture alone
cannot reopen the closed rights. Later work must use a separately retained suffix
whose authority and predecessor link are recorded. Release capture pins only after a durable
backup-ownership/release record proves that the complete recovery set independently
holds all obligations it replaces, and remaining normal/offline-replica/point-in-
time plans stay covered. Ongoing backup retention then pins those artifacts until
explicit retirement; daemon GC cannot infer retirement from age or schedule.
Retain independent configuration, identity/authority verification material and
secure recovery access to encryption keys outside the lost storage. Never place
keys in public manifests or evidence bundles.

In the intended `volumes/<uuid>/{data,journal}` layout, one host's recursive
snapshot captures those siblings together. It is only one component: A may have
published W's write in RAM while its own data/journal snapshots predate that write.
A snapshot of A without W or verified replacement coverage is not a complete
recovery set. Prepared lab/canary layouts require inventorying their actual
run-owned data/journal/control datasets; do not snapshot a shared parent containing
unrelated runs. Independently timed data/journal snapshots are compatible only
with a verified matching baseline and full replay suffix, never by timestamp.

Live crash-recoverable snapshots may supplement clean closed recovery sets if
recovery validates partial appends/apply and uncertain slots under the full source
survey. They must not be advertised as an application-consistent or closed clean
checkpoint. Database quiescing is a separate application contract. Snapshot count
and deletion of live journal files do not bound retained bytes: snapshots/clones
may retain those blocks. Coordinate external holds/retention and capacity admission
without letting daemon housekeeping destroy administrator-owned snapshots.

## Restore and intentional rollback

Restore into new private, gated writable resources; preserve original/failed copies
and received backup sinks. Verify complete set/artifact bytes, exact authority and
source/dependency mapping, checkpoints and required suffixes before replay. Rebuild
only selected effects using stable object/request identities; never lay an old
catalog over payloads that may already contain excluded redo. A missing source,
claim/result ledger, home or transaction decision blocks the dependent closure.

Repairing one stale replica rejoins current history under recovery-only permission,
with checkpoint plus suffix and no workload publication. Whole-closure crash
activation follows [Takeover](spec/Takeover.md#all-origin-transition-state-machine):
positive old-domain exclusion, complete sealed survey/equivalents and issued
frontier, durable full recovery graph at new A and distinct B, then installed
authority and fresh cache/view barriers. A saved manifest does not prove the old
execution domains are currently excluded; verify continued launch inhibition.
Do not activate two copies of the same restored identity/history.

Intentional rollback/loss is a separate explicit user-authorized new recovery
generation and source selection. Keep unwanted suffixes quarantined and prevent
normal replay from reintroducing them. Select a consistent whole closure including
transaction/home dependencies and preserve complete permanent ID-rejection/floor
evidence, or establish a verified disjoint fresh identity namespace; uncertain old
IDs cannot execute as fresh requests. A new generation alone supplies no such
proof and never rewrites an old COMMIT as ABORT. If exclusion or identity safety
cannot be established, keep activation unavailable.

Local snapshots do not survive loss of their pool. Off-host copies must verify
the complete selected recovery set, transfer completion and retention/hold ownership;
no completed subset proves the remainder. Independently captured VM disks may be
crash-recoverable but do not establish a coordinated all-origin cut. The
[f3-to-f2 lab procedure](Lab-replication.md) requires a separate writable recovery
copy and separately approved boot; never boot or modify its received sink.

## Canary policy and restore acceptance

The [disposable canary backup policy](spec/Backup-policy.md) supplies bounded
retention/resource obligations and isolated restore acceptance. Its manifests
must satisfy this all-origin coverage contract before distributed use. Retain
test artifacts, trust material and configuration outside canary storage; use test
keys and separate writable recovery resources. Verify source coverage, exact
claims/results, namespace/data/application integrity and recovery generations in
separately approved isolated runs. Existing lab replication or local checkpoint
tests do not establish distributed restoration. Production backup policy, workload
migration and NFS retirement remain outside scope.
