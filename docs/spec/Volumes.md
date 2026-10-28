# Volumes and replication-group boundaries

Status: boundary policy for task `9q3`. Paul selected one FUSE mount per volume
and cross-volume transaction design before implementation on 2026-10-28.
After Paul delegated routine choices on the same date, the initial transaction
interface is an explicit application API, retaining those per-volume mounts.
This specifies
intended behavior for disposable synthetic lab/canary volumes; it creates no
volumes and establishes no compatibility result. Production migration and NFS
retirement remain outside this project.

## Identity and ownership boundary

A volume is one configured workload root, one immutable volume UUID and one
replication group. The group has its own owner, ownership epoch, configuration,
ordered journal, commit/apply positions, checkpoints and recovery history.
Changing owner does not change volume identity. Shared daemon processes,
physical storage, a mount tree or colocated owners do not merge groups.

The root is a logical namespace, not permission to export its raw backing
directory. Use the [data/journal sibling layout](../Journal-storage.md), keyed
by volume UUID; neither journal nor control storage is workload-visible.
Backing paths and a human-readable name are administrative mappings to the
UUID, not identities inferred from path text. An object's
[stable identity](Backing-store.md) names its home volume; matching inode numbers
on different mounts prove nothing.
[Cross-volume transactions](Transactions.md) introduce logical namespace references
to home-owned objects, without merging volume UUIDs or linking ZFS datasets.

Each canonical root is disjoint from every other canonical root. Reject duplicate
backing roots, ancestor/descendant canonical roots and aliases disguised as new
volumes before creation or publication. Validate actual configured backing
identity, not just lexical prefixes; symlinks must not evade overlap checks.
A permitted view is instead `(volume UUID, relative subtree, access policy)`:
it shares its volume's owner, history and accounting. A read-only view is not
another replica or another group.

Only the active owner serves workload reads and writes, including read-only
views. Passive replicas do not become usable merely because a directory or
FUSE mount exists locally. Apply the [durability/freshness contract](Durability.md)
and [ownership gates](../Cluster-and-ownership.md) independently to each group.
Ordinary daemon mutations and fsync retain the confirmed durable-local-journal
plus synchronous-partner-memory boundary; this spec adds no stronger guarantee.

## Decision 1: node mount topology (Paul)

| Option | Behavior and benefit | Cost or constraint |
|---|---|---|
| A: one FUSE mount per volume (recommended) | Mount each UUID beneath an administrator-owned directory, e.g. `<run-mount-root>/<uuid>`. Kernel mount boundaries reinforce group isolation; individual mount lifecycle and failure handling are easier to inspect. | More mounts and potentially more per-volume guest exports. A daemon may still manage several mounts; this does not require one process per volume. |
| B: one shared FUSE mount | Mount a namespace with administrator-created UUID children. Fewer host mounts; one guest export could expose the whole namespace. | Daemon must enforce group boundaries inside one filesystem, gate each child independently, and prevent writes to its administrative root. One mount's failure affects every visible child. A shared mount does not supply shared ownership or cross-group atomicity. |

B permits only an administrative container of independent volumes, not an
application-owned parent tree. Neither option changes the one-root/one-group
rule. Under either option, an unavailable child must fail or remain gated, never
expose an empty fallback directory or stale passive data.

bhyve virtio-9p shares directories. Under A, plan a share of each required FUSE
volume directory, or separately validate whether an exported administrative
parent traverses child mounts correctly. Parent export traversal is not assumed.
Under B, a single share of the FUSE root is a possible simplification, but exposes
the administrative namespace and must enforce every child's authority and access
policy. Individual child-directory exports remain possible. An export does not
grant serving authority or bypass readiness checks.

The canary plan must record host mount, share tag, guest mount and published
subtree for each UUID/view. Missing required shares block publication. Dynamic
share addition/removal and guest remount behavior need deployment validation;
this policy promises neither hot-plug nor transparent takeover. Never export raw
ZFS data to avoid a mount/export restriction. Test native FUSE and the separate
Rocky/9p path independently; 9p is outside the native three-guest matrix.

**Decision record:** Paul selected A on 2026-10-28. This is not approval to install,
mount, change bhyve configuration or execute a test.

## Decision 2: cross-group operations (Paul)

| Option | Contract | Tradeoff |
|---|---|---|
| A: reject with `EXDEV` (recommended) | Cross-volume rename, replacement rename and hard-link creation fail without effects, even when both groups have the same owner or share one FUSE mount. | Matches independent history/recovery and the existing durability/canary contracts. Applications needing atomic rename or hard links must keep those objects in one volume. |
| B: design cross-group operations | Add a durable transaction/recovery protocol spanning the involved groups, including ownership changes, atomic visibility, failure and link lifetime. | Substantial new scope and coupling. Separate kernel mounts under topology A reject cross-mount rename/link before the daemon can coordinate them; POSIX cross-group operations would therefore also require topology B (and transport support), or a separately designed application API rather than ordinary rename/link. This changes the existing `EXDEV` expectation and requires revising durability, recovery and canary contracts before implementation; selecting it cannot imply support already exists. |

Under recommended A, check canonical volume identity before mutating either
side. Do not silently convert rename into copy-and-delete or hard links into
copies/symlinks. A user tool may explicitly copy then remove, with independent
operations and partial-failure handling; that is not an atomic move. Same-volume
cross-directory rename and regular-file hard links preserve one object/history
and use the ordinary durability and permission rules. Directory hard links
remain prohibited. Cross-mount alias paths may receive kernel/transport `EXDEV`
even when they name the same canonical volume; they are not promised to support
rename or linking across those separate mounts.

Symlinks are ordinary stored link text, not cross-group transactions or serving
authority. Traversal succeeds only through already published authorized views
on the active owner. Reject publication that requires cross-owner traversal;
never fetch workload reads from a passive replica to resolve it. Ordinary symlink
traversal supplies no transaction or atomic snapshot across separate volumes.

**Decision record:** Paul selected B on 2026-10-28: add cross-volume transaction
design before implementation. Paul subsequently delegated routine decisions;
the selected interface is an explicit application transaction API, retaining
the per-volume mounts he chose. Ordinary POSIX rename/link across these mounts
still returns `EXDEV`; those syscalls cannot invoke the transaction API.
The [transaction specification](Transactions.md) supplies coordinator authority,
participant fencing, decisions, visibility, object lifetime and recovery semantics
before filesystem API, journal, commit and service implementation are frozen.
Its protocol still requires modelling and implementation evidence before support
can be advertised. Do not silently omit transactions or substitute copy-and-delete.

Existing [Durability.md](Durability.md) and
[canary patterns](../Canary-patterns.md) already require cross-volume rename
`EXDEV`; those statements describe ordinary operations on separate mounts.
They do not satisfy the separate explicit-API transaction-support requirement.
The [transaction contract](Transactions.md) now defines the application API,
regular-file/symlink/directory rename, regular-file hard links, replicated decision
slot, shared visibility barrier and recovery obligations. Its initial placement
rule requires a common owner, replica membership and synchronous partner across
the dependency closure. Foreign object references retain their home groups and
couple placement, recovery and retirement. Initially affinity only grows, so a
transaction may permanently enlarge the availability unit; per-group journals
and identities remain distinct. Incompatible placement is explicitly rejected
before admission. This is specified behavior, not implemented support or proof.
Initial parent aggregation remains proposed as unsupported; this does not
authorize bypassing group boundaries to emulate transactions.

## Administrative namespace, naming and lifecycle

An operator-controlled volume registry manages the parent namespace and maps
UUIDs to names, roots, replica membership, quotas and permitted views. Workloads
cannot create, rename, remove or change permissions on the shared parent or its
volume entries. These are configuration operations, not ordinary replicated
file operations. Coordination is for infrequent configuration/ownership changes,
not per-file access. An initial interface may be configuration-driven; this spec
does not presume an implemented controller or CLI.

Use a stable UUID component in mount/export identity. Human names are unique
within the canary namespace and may change administratively without changing
data identity; renaming must update mappings before publication resumes. Names
must be single components excluding `.`, `..`, separators and reserved control
entries. A new run gets fresh UUIDs and paths even if it reuses a descriptive
fixture label. Record aliases explicitly; never infer them from PVC names.

1. **Create:** validate run ownership, fresh UUID/path, non-overlap, admission,
   quota/headroom and the proposed replica configuration. Prepare empty synthetic
   data/journal storage on the assigned replicas and persist identity/configuration.
   Establish authority, required recovery state and a ready synchronous partner
   before publishing. Mount presence alone does not complete creation.
2. **Publish/unpublish:** resolve the complete view/placement dependency set,
   validate credentials, subtree existence and readiness, then expose it to the
   intended clients. Unpublish closes admission and revokes the relevant access
   path; cached guest access, descriptors and mappings require the deployment
   fencing contract. Removing a PVC/API object alone is not revocation evidence.
3. **Retire:** stop new publication, quiesce clients and revoke every view/export,
   preserve the acknowledged prefix and required recovery evidence, and record a
   retired identity in configuration so returning replicas cannot republish it.
   If any required step is uncertain, keep it unavailable and preserve resources.
4. **Reclaim:** deletion is a separate authorized manifest-bound operation after
   retention, snapshots, catch-up pins and diagnostics permit it. Normal unmount
   must complete; failure preserves the resource for inspection. Never recycle a
   retired UUID, delete unknown resources or alter received backup sinks.

Lifecycle crash reconciliation, persistence format and exact API/error encoding
belong to implementation specs. Repeating a lifecycle request must reconcile
the same UUID rather than allocate a second history. Snapshot ownership and
recovery follow the existing [lab replication rules](../Lab-replication.md);
retirement does not grant permission to boot or modify a received sink.

## Proposed initial sharing policy and the five patterns

Support same-owner RW/RO and subtree views of one canonical volume with fresh
permissions and cache coherence. Reject malformed/escaping subtree paths, absent
required subdirectories, conflicting UUID mappings and independent owners before
publication, with an explicit reason and no usable partial mount set. Read-only
policy must cover all mutations through that view; it does not replace Unix
permissions. Numeric credentials must survive the actual access path. Required
adapter/export enforcement that is unavailable blocks that profile.

Keep separate canonical volumes for independent private/shared roots and
coordinate placement of the complete client dependency graph. If client A
needs music plus sync data, and those volumes have other writers/readers, the
same-owner requirement applies transitively to every dependent client. Keep
publication pending or reject with a placement-conflict reason until the whole
set can serve on one owner. Do not silently merge UUIDs, copy shared bytes,
start a partial client, or move only one member while incompatible clients
remain active. This adds coordinated placement, not a cross-volume transaction
or a shared failover epoch. The later fixture/CSI specs define its orchestration.

| Canary pattern | Proposed mapping and publication policy |
|---|---|
| P1: music reader/artwork writer | One music UUID with RO/RW views on one owner; separate private writer-state volume colocated there. Same-owner sharing is the explicit shared-volume exception. |
| P2: multi-volume reader | Separate music, sync-data and private/library volumes, all required by this client on one owner. Include P1/P3 clients in transitive placement; reject split-owner or missing-subtree publication. |
| P3: updater/subtree reader | Separate config and data volumes; publish the permitted subtree of the data UUID to P2, never as another canonical root. Notifications and application recovery remain unproven. |
| P4: RW/RO aliases | Both claims resolve to one video UUID/history and owner. Support the two access policies; reject a request assigning either alias another identity/owner. If the transport cannot enforce them, block the alias profile rather than copy it. |
| P5: parent/child relative layout | Proposed initial limitation: reject the complete parent-aggregation/overlapping-root publication as unsupported. Do not construct a parent view spanning P3's independent root and a generator-output root, or count separate source/output mounts as a pass. |

P3's restricted subtree view is an alias within one configured root, not a
parent namespace aggregating configured roots. Supporting P5 later would need
an explicitly designed coherent parent layout, authorization and placement,
potentially one larger canonical root for that whole pattern. That trades away
independent group lifecycle/placement; merely sharing a FUSE mount under option B
does not implement it. Initial P5 results must say **unsupported/rejected as
specified**, not passed. These are proposed initial scope choices for review,
implementation scope choices under Paul's delegation, not claims about the
current PoC. P5 remains an explicitly unsupported initial publication pattern.

Cross-node shared workload access is outside the baseline. Supporting it would
need a separate read-freshness, locks, cache and fencing design; read-only flags
alone do not supply that design. Current ownership transfer restarts workloads
after safe recovery, rather than migrating open handles.

## Review and later validation

Review against [cluster boundaries](../Cluster-and-ownership.md),
[Kubernetes canaries](../Kubernetes-and-migration.md),
[filesystem semantics](../Filesystem-semantics.md) and all five
[synthetic patterns](../Canary-patterns.md). `gv3` maps approved policy to fresh
fixtures only after the [resource budget](../Deployment-readiness.md) is ready.
Later approved runs must check UUID/view identity, denied parent modifications,
non-overlap, complete placement, alias RO enforcement, `EXDEV` without effects,
same-volume rename/link semantics, stale-owner blocking and lifecycle interruption.
Record native and 9p evidence separately, including unsupported and not-run cases.
No tests, builds, deployment or infrastructure operations established this draft;
each execution retains its own [explicit approval](../Test-automation.md).
