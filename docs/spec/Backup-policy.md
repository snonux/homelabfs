# Disposable canary backup and restore policy

[Snapshots and restore](../Snapshots-and-restore.md) ·
[Transactions](Transactions.md#durability-retention-and-snapshots) ·
[Canary volumes](../Canary-volumes.md) · [Capacity](../Capacity-plan.md) ·
[Acceptance profiles](Acceptance-profiles.md) ·
[Run approval](../Test-automation.md)

## Status and ownership boundary

This is the design contract for `kv3`, prepared on 2026-10-28. It specifies
future tooling and an isolated restore plan; it creates no snapshot, key, hold,
backup destination or restore evidence. HomelabFS and its checkpoint/restore
interfaces remain unimplemented. No canary restore has passed. The
[existing lab replication](../Lab-replication.md) verified completed transfers,
snapshot GUIDs and key compatibility, without a restore boot or application
recovery. Those results cannot satisfy this contract.

Only bounded synthetic lab/canary volumes are covered. Production data, PVs,
consumers, NFS, backup policies, package service, registry and Forgejo remain
outside this project. External FreeBSD tooling owns snapshot creation, transfer,
holds and pruning. HomelabFS owns checkpoint/dependency declarations and journal
GC; neither owner may delete the other's resources. An external run controller
owns the durable inventory and approval/evidence records outside the test cluster.
Every execution, including snapshot/restore exercises, requires Paul's explicit
approval of its exact immutable plan. This document supplies no execution token.

The existing f3 `f3_to_f2_freebsd` zrepl job continues to own the outer lab VM-image
snapshots and retention: ten-minute schedule, last ten plus `24x1h | 14x1d` on
sender and receiver. The f2 received tree remains unmounted and unchanged.
Canary tooling must not create holds on, prune, rename, mount, boot, change keys
or receive properties of that sink, or compete for the `zrepl_` namespace. An
outer image copy and the inner canary recovery sets are different layers, with
separate byte accounting. No production backup schedule is changed here.

## Recovery unit and consistent cut

Use the seven canonical UUIDs and typed sibling paths from
[Canary volumes](../Canary-volumes.md#identity-derivation-and-durable-manifest).
For the full fixture, inventory all three replicas of all seven volumes: 21
data and 21 journal dataset entries, three run control trees, and the three
members' dedicated test-coordination state. P5 has no allocated volume. RO/RW
aliases are view records of the same UUID, never extra snapshot histories.
A complete set carries actual available copies and their positions; merely
listing an unavailable replica is not a copy or a reason to fabricate its GUID.
The initial clean-set profile requires all three replicas ready at the cut.

Fixture placement closure A contains six volumes; B contains video. Their
publication dependencies must survive recovery. Additionally, expand the
[transaction dependency closure](Transactions.md#placement-and-the-unit-of-availability)
through outstanding claims/bindings, coordinators, foreign object homes, session
anchors, affinity predecessors and live cross-volume handles. Include every
transitive member, even an anchor outside the requested path pair. For a bounded
initial fixture, reject checkpoint admission if that expansion exceeds the seven
planned UUIDs or its quotas; do not silently omit a dependency. Initial ordinary
fixtures do not invoke the transaction API. Transaction-specific restore evidence
needs its own approved operation ledger and implemented conformance gates.

To prepare one clean recovery set, future tooling must:

1. Gate the complete affected publication/dependency graph, drain admitted work
   and verify that client/guest caches cannot bypass the gate. Stop new mutations
   and transaction admissions. Resolve pending decisions or retain all prepare,
   accepted-decision and resolution dependencies; uncertainty blocks a clean set.
2. Establish a durable closure checkpoint barrier and committed-cut vector. Each
   member retains its own generation/configuration/epoch and position; there is
   no invented shared sequence number. Persist data/namespace application and
   complete journal/checkpoint dependencies, including decision/result records.
3. While the barrier holds, capture paired data/journal snapshots and matching
   run control evidence on each participant, then a verified logical snapshot
   of the dedicated test etcd database at a recorded revision. Bind its relevant
   key/value inventory and storage-cut references to the same set manifest.
   Dedicated coordination has no unrelated clients; gate its relevant changes
   through this handshake. A recursive ZFS snapshot alone cannot establish
   etcd/application consistency, and these sibling trees have no single safe
   recursive parent that excludes unrelated runs. Enumerate exact run-owned
   datasets; never recursively snapshot the whole guest pool implicitly.
4. Seal the manifest only after every component, hash, dependency and snapshot
   identity is verified and durable externally. Release the barrier only after
   a recorded successful or safely aborted capture. Partial capture stays
   incomplete and charged; no timestamp-based pairing or serving publication.

Record barrier acquisition/release and failure deadlines in the approved plan;
no unbounded pause is permitted. Live crash-image capture is a separate future
profile requiring decision recovery and compatible suffix proof. It cannot be
labelled a clean checkpoint by reusing this manifest type. Database consistency
requires its own quiesce/recovery oracle; these tiny fixtures certify no real
application database.

## Manifest completeness and trust

Use a versioned, canonically encoded manifest, signed by the run's approved test
backup identity and hashed externally. The approved plan pins the encoding,
hash/signature algorithms, signer public-key fingerprint and verifier version.
Unknown versions, missing required fields, duplicate identities and ambiguous
encodings fail closed. Hashes detect damage; provenance also requires a trusted
signer and independently pinned expected values. A signature supplied only by
the same untrusted backup is insufficient. No digest, key or target ID in this
policy stands for an actual measurement.

A set is complete only if every required entry below is present, resolves to
actual admitted resources, and verifies against independently retained records:

| Manifest section | Required fields and checks |
|---|---|
| Envelope and provenance | Schema/version, unique set ID, source run UUID/K, approved plan digest/reference, spec/source revision and dirty-tree state, controller identity, creation/seal times, clean/crash type, status, manifest digest/signature and external durable location |
| Cluster and scope | Test cluster identity, actual node UUIDs/incarnations, admitted membership, seven canonical volume UUID/label mappings, closure UUID/version/member vector and predecessors, fixture publication graph, view/subtree/access policy and UID/GID/mode mapping |
| Storage inventory | Each data/journal/control role, run owner, exact source dataset name plus dataset and snapshot GUIDs, encryption root/key ID, snapshot name, completion state, actual bytes, paired set ID, stream/chunk hashes and lengths, destination inventory and receive completion. Names alone do not prove identity; incomplete receives are not copies |
| Recoverable history | Per-group recovery generation/configuration/epoch, committed/durable/applied checkpoint vector, replay start/end and contiguous suffix hashes/lengths, required baselines, object/namespace/lifetime state and foreign-home edges. Preserve ordered dependencies, not just final file bytes |
| Transactions and deduplication | Canonical request/claim digests and bytes, immutable session anchor/principal/nonce/request identity, REQUEST_BIND participant vector and bound coordinator, prepare/decision/install/COMPLETE/result references, affinity/visibility generation, unresolved obligations, retirement floors and closed-session/retired-anchor tombstones |
| Control and coordination | Source identity/admission/emergency policy and fence evidence as historical input; verified test-etcd logical snapshot digest, format/version, revision, source member/cluster inventory and relevant key inventory, plus storage-cut binding. Restored leases or epochs are not current authority |
| Keys and artifacts | Test encryption/signing/TLS key identifiers and secure retrieval references, pinned trust roots and revocation status, complete independently stored artifact-bundle manifest/digest, compatibility matrix and verifier/replay versions |
| Oracle and resources | Deterministic seed, ordered invocation/reply ledger with sync boundaries, expected paths/lengths/hashes/metadata/object IDs/alias and transaction outcomes, permitted uncertainty/rollback cutoff, quota/credit ledger, transfer/restore pins, byte inventory and cleanup owner |

The immutable set seal references all component digests; mutable transfer/hold
status belongs to an append-only external inventory linked to that seal. Check
that every listed dependency is present or has a verified durable replacement
inside the set. Reject foreign run/cluster UUIDs, substituted dataset/snapshot
GUIDs, missing journal ranges, dangling homes, absent anchor/floor/tombstone
history, incompatible generation vectors, contradictory decisions, checksum or
signature failure, unsupported formats and unexplained extra components.
Quarantine the set and retain diagnostics; do not select a maximum sequence or
newest timestamp, invent an abort, create empty replacement files, or regenerate
missing keys. Decryption success alone does not establish completeness.

The physical layout implementation must map each role to the
[Volumes](Volumes.md) identity and [Transactions](Transactions.md) dependency
contracts: stable UUID/object-home identity, sibling data/journal coverage,
control outside volume rollback, barrier/cut and suffix references, anchor and
retirement state, and retention pins. Its manifest adapter must supply these
fields before backup tooling can be accepted. A different backing layout cannot
weaken the recovery unit. No API or schema implementation is claimed here.

## Test keys and independent recovery bundle

Use a fresh test-only encryption root and narrowly scoped credentials for the
run-owned resources. Record the responsible operator, key IDs/fingerprints,
encryption-root mapping, escrow access method and lifecycle in the approved plan.
Never reuse production secrets or the outer lab bhyve key for these inner tests.
The existing outer lab key ownership/loading inventory stays unchanged. Key bytes,
private signing keys and credentials belong in independent protected storage,
never Git, public evidence, manifests, images or exported data views. Test that
an authorized recovery operator can retrieve the correct test key without the
source cluster; missing, foreign or revoked keys block restoration.

Retain a separately hashed/signed, independently readable recovery bundle with:

- Exact FreeBSD daemon/packages and dependencies; runner, replay/verification
  tools and source/dependency locks/build provenance; OS/kernel/ZFS/FUSE versions
  and supported journal/checkpoint/wire/manifest formats.
- For a guest/deployment profile, the matching pinned Rocky kernel/module RPMs,
  dependencies, boot/rollback entries, bhyve/lib9p configuration, negotiated 9p
  flags and SELinux/credential policy. Apply [Kernel policy](Kernel-policy.md).
- Immutable container images exported by digest, including required platform
  variants, chart/manifests and configuration for the selected test clients,
  daemon, test etcd and helpers. Native profiles mark container/9p items explicitly
  inapplicable; a mutable image tag or a registry URL alone is insufficient.
- Configuration templates and resolved run mapping, independent public trust
  anchors/signature policy, key-retrieval instructions, host-key pins, fence
  scope/management mapping and recovery/normal-unmount instructions. Keep secret
  retrieval separate from public configuration and newly issued test identities.
- At least the selected validated candidate and its compatible known-good rollback
  artifacts/configuration/trust references. Record compatibility and revalidation
  triggers; an older package unable to read the selected format is no rollback.

Pin full versions/ABIs, artifact lengths/digests, signatures, source/build origins
and verification results using [Packaging and rollout](../Packaging-and-rollout.md).
Do not assume current repository signature enforcement. Reject untrusted,
revoked, mismatched or unavailable artifacts before boot/replay. Recovery must
work without HomelabFS, canary etcd, the source pool, package-service PV, registry
or Forgejo availability. The approval plan names an administrative/offline store,
its owner and protected capacity, and demonstrates independent retrieval and
verification; a second path on the failed pool is insufficient. This neither
moves those services nor changes their production storage. Bundle size is
measured and separately reserved, not assumed to fit the 1 GiB evidence reserve.

## Retention, holds and bounded admission

A future external canary snapshot job operates only within its approved run
window and explicit dataset allowlist. Use names `hfs_canary_K_S`, where `S` is
a unique set identifier, with run/plan ownership metadata. Capture one clean
baseline after seeding and at most one later complete checkpoint per selected
run; there is no unattended periodic canary schedule implied here. Keep **at
most two completed compatible recovery sets**, one checkpoint in progress and
one rebuild/restore in progress. These are aggregate fixture limits per replica.
A failed/partial set consumes its staging allowance until inspected; it does
not disappear from accounting because it lacks a seal.

For every baseline/suffix transfer, restore, checkpoint and replica catch-up,
record a durable pin: run/set/component IDs, holder identity, purpose, exact
snapshot/journal dependencies, transfer/resume token reference, charged bytes,
start/progress times and release condition. External tooling acquires a run-owned
hold on the exact test snapshots before releasing the checkpoint barrier or
starting dependent transfer. HomelabFS retains corresponding journal/dependency
pins. Do not advertise a protected set until both inventories agree. Interrupted
transfers keep their pins and their partial destination charge; a timeout alerts
and gates further allocation, rather than expiring the hold automatically.

Pruning requires verified replacement coverage, no transfer/restore/catch-up or
transaction/foreign-home pins, and a recorded operator-authorized cleanup plan.
Anchor retirement does not release snapshot/object/catch-up pins. Unretired
request outcomes, monotonic retirement floors, session tombstones and retired
anchor rejection mappings have no wall-clock expiry. GC and closure transitions
must serialize under the transaction contract. When a third set would be needed
while two are pinned, stop capture/admission; never delete a required prefix to
meet a retention count. Retain the independent bundle/trust/key access and oracle
for every advertised recovery set, plus its compatible rollback artifacts.
Retire test keys only after all dependent sets, partial transfers, restores and
inspection obligations are deliberately retired; record that such sets then
cease to be recoverable. No production key or snapshot retirement is authorized.

Apply [Capacity plan](../Capacity-plan.md) and the typed quota split in
[Canary volumes](../Canary-volumes.md#storage-quota-translation-and-readiness):

| Per replica, whole seven-volume fixture | MiB |
|---|---:|
| Current data/journal/control/test-etcd base | 64 + 128 + 64 + 128 = 384 |
| Two complete retained sets | 768 |
| One checkpoint staging and one additional writable restore/rebuild copy | 384 + 384 = 768 |
| Evidence, failed-resource reserve, allocation/metadata allowance | 256 + 256 + 640 = 1152 |
| Maximum planned used / run quota | 3072 / 4096 |

Each complete set, including required suffix and coordination export, fits
384 MiB per replica; metadata/chunk staging still consumes the declared
allowances. Stop new mutations at 2.5 GiB parent use or 75% of any base allowance;
3 GiB is the hard planned-use ceiling, with 1 GiB emergency headroom reserved.
Data/journal/control typed tree quotas remain 704/1152/2240 MiB per replica.
Only one aggregate bulk operation is allowed. Retained copies count allocated
bytes without assuming compression, clone sharing or deduplication. Holds and
snapshots may retain deleted blocks; refquota and count limits alone are
insufficient. Unexpected growth gates work before consuming recovery capacity.

An existing-node restore consumes the **additional** 384 MiB staging allowance
while its current replica and retained sets stay charged. Restoring all three
replicas needs that allowance on each; a distinct replacement host needs a new
4 GiB reservation and full RAM/CPU admission. An outer VM-image recovery needs
its separately measured writable image copy, not this 384 MiB allowance. Never
infer space/RAM to boot from a received copy's existence. Record fresh pool
`A >= F + G + Q`, ancestors/quotas and measured bytes at every source, writable
destination, bundle and evidence store. Native N additionally reserves the
32 GiB incremental outer-history allowance at both f3 and f2 and 1 GiB external
evidence; bundle and any new writable outer copy are additional demand. Preserve
existing zrepl history and future receive growth, without sink changes.

All existing resource ceilings apply: N stays inside each 2 GiB guest with
384 MiB daemon, 256 MiB test etcd, 128 MiB clients/helpers and 256 MiB ARC plus
the protected OS/spare split; N physical CPU ≤1.5 cores aggregate. Physical I/O
≤4 MiB/s and ≤100 IOPS, bulk ≤2 MiB/s, total network ≤4 MiB/s per direction,
including verification/retries/outer traffic. Preserve measured service headroom,
no swap/pressure, bounded evidence and failed resources. Independent bundle
retrieval and restore verification consume these caps too. Unknown capacity or
unbounded attribution means not admitted. N never runs alongside Rocky; S and R
remain not admitted under the current conservative budgets.

## Restore authority and isolated destinations

Restore into a distinct, empty, run-owned writable destination. A proposed inner
staging mapping is `hfsdataN/ROLE/hfs-run-K/restore-S/COMPONENT`, where ROLE is
`data`, `journal` or `control`; COMPONENT distinguishes volume UUIDs and the
coordination export. These are placeholders to resolve and collision-check in
the exact plan, not existing targets. Charge them under the matching typed run
tree. Their private administrative mountpoints must differ from all source,
received, serving, export and evidence paths. Keep publication disabled, no
autostart, no production credentials/routes and no guest/CSI registration until
validation and authority reconciliation pass. Never write through a missing
mount into an OS-directory fallback. A target below the f2 received sink or its
live mount is always rejected. A separate outer VM recovery follows
[Lab replication](../Lab-replication.md#recovery-procedure--separately-approved)
and requires its own approved target and lifecycle plan.

Preserve canonical volume/object IDs and source history for verification;
new test node/incarnation/coordination identities must be explicitly bound to
the isolated recovery environment. Do not admit duplicate source volume IDs to
a live registry or reuse serving endpoints. Restored control is historical
reconciliation input, never a command to resurrect old owners, leases or keys.
A raw etcd data-directory rollback is not authority recovery. Restore the
verified logical snapshot with compatible tools into a new isolated test-etcd
cluster with new member identities; audit the recovered relevant keys against
the manifest/cut. The restored database may contain historical live leases;
keep services disabled while removing/reconciling those under the new isolated
cluster bootstrap protocol. Reconcile membership, epochs, admission/emergency restrictions,
request-anchor routing, fence records and generations with surviving authoritative
evidence under the takeover protocol. Unknown or contradictory authority blocks
all dependent views. Never connect that etcd to production k3s or let stale leases
start serving. A storage majority, valid history and positive old-path fence are
separate requirements from a successful etcd restore.

Distinguish three operations explicitly in the approved plan:

- **Stale replica repair:** remain recovery-only, verify current authority and
  compatible baseline/suffix, catch up the complete dependency closure, and join
  the current generation only after fencing/readiness proof. An old backup does
  not become authoritative by being restored successfully.
- **Offline restore verification:** keep all endpoints isolated and serving
  disabled while comparing the recovered cut to its oracle. Passing integrity
  checks alone grants no new serving authority or permission to boot/publish.
- **Intentional rollback/accepted loss:** preserve the current branch and oracle,
  state the exact consistent closure cut and known lost/uncertain client outcomes,
  obtain explicit approval for that loss/rollback and positive old-path exclusion,
  and establish a fresh authoritative recovery generation through the fenced
  recovery protocol. Old replica/guest paths remain excluded; reseed peers from
  this history, not an automatic suffix that reapplies rejected changes. No
  partial-closure rollback, generation reuse or automatic rejoin is allowed.

In every serving transition, durably retain deduplication claims/results,
retirement floors and closed/retired anchor mappings; if post-cut request IDs
could be retried, prove they cannot be re-executed by preserving valid rejection
history or keeping former sessions permanently isolated/retired under the
approved recovery protocol. A missing backup record cannot mean an unused ID.
Do not alter an old decision or return success by dropping dependencies. Gate
publication until complete consistent replay, cache invalidation/remount,
authority/fence reconciliation and a ready normal synchronous pair are proven.
The accepted owner-storage plus volatile-partner loss gap has no finite RPO;
this snapshot schedule cannot create one.

## Non-executable isolated restore plan and oracle

The following is a reviewable plan shape, not permission or a runnable script.
Future tooling must first resolve actual target identities, set/dataset/snapshot
GUIDs, paths, artifacts/digests, signer/key references, fresh admission, budgets,
fault/rollback scope, fence proof, retry/deadline and normal cleanup transitions.
Missing values block execution. Paul's approval must reference that exact plan,
its digest and window. Critical restore, fencing and VM lifecycle actions wait
for this approval; a previous run approval or `--approved-run` flag is insufficient.

1. Select one scenario and evidence profile: N offline restore verification
   first, I for guest/cache/lifecycle recovery, I-P only with independently
   admitted physical fixtures. Select one sealed clean set and its external
   oracle/bundle. Inspect the planned resource/collision inventory locally;
   actual resource inspection belongs to the separately authorized execution.
2. Prepare at most 4 MiB deterministic fixture payload across the seven UUIDs,
   64 files, 32 directories, eight symlinks, files ≤64 KiB, ≤256 mutations and
   ≤five minutes of fixture mutations. Record seed, each invocation/reply and
   sync boundary outside the cluster. Select a smaller concrete ledger that
   fits seeding, snapshot cut and chosen recovery case; reserve up to 16 mutations
   and ≤2 MiB encoded bytes only if testing the small-suffix case. No transaction
   mutations are silently added to ordinary fixtures.
3. Gate/quiesce, create the complete set with the barrier above, transfer and
   independently verify its seal/components, and demonstrate bundle/key access
   with source service paths unavailable in the approved isolated environment.
   Preserve sources, snapshots, incomplete outputs and current branch. Disabling
   production services to simulate independence is forbidden.
4. Receive/copy into the separately admitted writable staging paths, preserving
   received sinks. Verify all chunks, dataset/snapshot identity and encryption
   mapping before opening files. Restore logical test etcd and reconcile its
   historical records offline. Validate consistent vector/suffix and replay
   idempotently behind gates, including a planned interrupted-resume case only
   if that case and its extra operations fit the same limits.
5. Compare to the external oracle using the matrix below. For optional serving
   recovery, execute only the explicitly approved fence/authority/generation
   transition and ready-pair/cache proof. Verify first synchronized write/read
   on the admitted path with fresh external receipts. Offline integrity is
   reported separately from serving, guest and physical-host evidence.
6. Collect manifest, raw hashes/metadata, decision and position traces, timing,
   counters, key/artifact verification results without secrets, fence and
   publication evidence, plus remaining-resource inventory. Normally unmount
   all run-owned views in the approved order. Preserve failures and pins; no
   forced unmount/kill, recursive deletion through a mount, sink cleanup or
   unapproved repair. Cleanup only listed run-owned resources under its
   authorized state machine; unresolved cleanup/restoration fails the run.

| Synthetic case | Required integrity outcome |
|---|---|
| Exact clean cut | Every expected name, byte length/hash, UID/GID/mode, timestamp contract, symlink target and stable object/home/link relation matches the cut; RO/RW aliases resolve to the same identity and denied probes leave state unchanged; P5 remains rejected with no allocation |
| Acknowledged effects after replay | Every effect whose required copies survive appears exactly once with its dependencies, ordered namespace/metadata and recorded decision; no gaps, stale reads or duplicate execution. Replica equality without the oracle is insufficient |
| Lost reply/pending transaction | Query/retry the same anchored ID/digest returns its preserved result or stays explicitly indeterminate/gated until valid resolution; no independent abort or re-resolution of a vanished source. Partial install is never published |
| Retired/closed session or retired anchor | Retry is rejected by the retained floor/tombstone/permanent mapping before path resolution; IDs cannot become new requests after restore or rollback |
| Post-cut changes/rollback | Offline verification matches the chosen cut only. An approved serving rollback uses a new generation, reports each known lost/uncertain operation and excludes old history/session resurrection; no finite RPO claim |
| Missing/foreign key, incomplete/foreign set, absent anchor/home/suffix, corrupted chunk/manifest or incompatible artifact | Refuse before serving or destructive replacement, record exact failure and unchanged protected resources, keep diagnostics/holds. Correct refusal is a negative-case result, not a successful restore |
| Missing fence, uncertain coordination/history or insufficient capacity | Remain gated/not admitted with explicit reason. A timer, decryption, restored etcd quorum or matching payload hashes cannot authorize serving |

Use the approved numeric rows in
[Acceptance profiles](Acceptance-profiles.md#lag-reconnect-and-recovery-proposal)
without inventing a backup RTO: C-R applies only to matching retained history
with ≤2 MiB/≤16-mutation suffix (first progress ≤5 s, completion ≤45 s);
C-B permits a ≤384 MiB recovery set (first chunk ≤10 s, transfer/install ≤540 s,
validation/suffix/application ≤60 s, total ≤600 s). C-R's conditional useful-rate
floor remains ≥0.05 MiB/s per complete 10 s interval starting with ≥0.5 MiB left.
N-R/I-R automatic single-owner recovery requires its prerequisites and stage
limits 15/30/45/60/30/30 s, ≤210 s total; checkpoint substitution yields ≤750 s,
and selected cold start ≤120 + 210 = 330 s. Quiesce/shutdown, if selected,
remains ≤60 s. These are separate selected scenarios, not additive allowances.
Manual intentional rollback/accepted-loss has no automatic recovery deadline.
All setup, transfer, validation, collection and cleanup/restoration must fit the
≤20-minute run window and resource caps; exceeding a deadline preserves safety
and reports an incomplete/failed run, never forces activation or deletion.

Report each selected case as **passed**, **failed**, **unsupported/rejected as
specified**, **not admitted** or **not run**, with exact profile, threshold/spec
revision, approved plan reference, source/cut/destination identities, verified
artifact manifest, oracle and raw evidence links. Separate assertion, resource,
infrastructure, timeout, cleanup and restoration failures. Native guests share
f3, so N cannot establish independent pool/host/power survival. Guest cached
access/fencing and application database recovery require their own evidence.
Without isolated independent physical hosts, I-P independent restore, physical
loss/cold-start and power-loss reliability remain **not run**. A passing synthetic
offline restore proves only that selected set/path/artifact combination and
oracle; it neither validates production workloads nor retires NFS.
