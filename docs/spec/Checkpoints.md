# Checkpoints, bootstrap and all-origin reclamation

Status: all-origin contract `gz3`, 2026-10-31; distributed publication, bootstrap
and reclamation are **unimplemented**. The existing v1 local implementation is
separately scoped below. It cannot grant all-origin deletion or normal serving.

## Ordered closure checkpoint

A checkpoint binds one complete dependency closure and installed authority lineage
to an immutable selected slot cut K and exact per-volume selected cuts. Gate the
capture against grants, selection/publication, apply, source mutation, membership,
closure joins and concurrent checkpoint/trim. The gate may permit later ingestion
only into a separately pinned suffix with an explicit nonoverlap proof. Finish
or retain every unresolved issued grant/result explicitly; no selected prefix
implies a closed issued frontier. A clean whole-closure backup/drain additionally
closes that frontier through the all-origin sealed survey or verified equivalents.
A sealed old authority cannot resume choices merely because capture ended; reopen
only through the appropriate installed successor authority and view barriers.

The manifest records authenticated baseline/catalog/payload digests, all original
origin stream identities/contiguous source cuts and slot-to-fragment predecessor
mapping, every exact decision/result/claim, dependency edges, complete authority/
promise/issued-frontier evidence and protected source/retention obligations. A
source cut is not a selected cut: one stream may contain unselected/staged and
structural bytes. K may select W1's first fragment, W2's first and W1's second,
including W2 outside placement. A checkpoint at W1's latest local position without
W2's required graph is incomplete. See the
[worked coverage checks](Takeover.md#worked-source-coverage-checks).

Durably capture the complete logical state at K, including namespace, bytes and
holes, metadata/revisions, foreign homes/open-unlinked objects, handles/pins,
allocation/reuse tombstones, claims/anchors/session floors and retired-ID rejection
state, transaction prepares/decisions/INSTALL/COMPLETE and barrier dependencies.
Replay uses the original validated redo/results; it cannot re-resolve paths or
recompute authorization. If a semantic record is still needed to validate a later
suffix or identity, keep its exact bytes in the baseline/archive or a pinned
verified durable dependency. Compacting the payload state is not permission to
erase permanent identity/decision evidence. Semantic compaction needs a separate
specified and proved replacement, including never-reused allocations.

Owner A's committed RAM overlay is capture input, never a durable checkpoint.
Either materialize and verify that complete overlay graph durably in the baseline
or retain exact durable source coverage at every contributing origin. A physical
snapshot of A's old applied tree and journal alone cannot cover its newer RAM K.
The designated distinct memory copy, received maximum, backing-installed maximum,
empty delivery queue or client reply supplies no durable replacement coverage.

## Publication, restart and bootstrap

Prepare immutable baseline/source/selection/dependency artifacts in a private
destination; verify bytes and complete graph, sync files and directories, then
publish an authenticated immutable manifest and atomic root pointer with ordered
syncs. Record the exact required per-stream suffix start and predecessor seals.
A crash must leave either the previous complete baseline+suffix or the new one;
old/orphan artifacts remain until verified retirement. A manifest hash alone does
not prove data durability, authority, source coverage or external pin release.

Before serving after restart, hold recovery/source/visibility gates across pointer
validation, baseline authorization, archive/source verification, suffix open and
full replay. Replay exact selected effects in slot order and reconcile unresolved
grants/results/authority before activation. Check all referenced payload bases,
foreign dependencies and permanent rejection/claim ledgers. Verify every surviving
source byte against its archived counterpart, chain/seal and immutable creator
identity. A missing suffix, interior hole, changed history or unknown authority
blocks bootstrap; never choose the largest applied/checkpoint position.

A node rejoining with no genesis may bootstrap only from this fully verified
closure baseline plus its exact complete suffix, behind workload gates in new
private roots. It is not normal ServingReady until [Takeover](Takeover.md)'s
current authority/incarnation, source and frontend predicates hold. Crash
activation needs the full durable graph at prospective A and distinct B;
cooperative handoff may retain verified origin sources and transfer RAM ahead of
new-owner disk. Neither path accepts an old APPLIED pointer as an independent
baseline. The current genesis-only service has no such bootstrap integration.

## Retention and source release

Compute retention from the complete source/dependency graph and its holders,
independently of local backing apply. A source may be released only when verified
durable replacements preserve every covered selected and unresolved obligation,
all identity/result/dependency evidence, and complete issued-frontier information
needed for later origin retirement/recovery. Persist the exact coverage and
protected release at required participants before unlinking. An outside origin
remains an obligated source until this step; shrinking S or changing authority
cannot erase it.

Acquire a fresh lease excluding membership/closure, checkpoint/backup, transfer,
emergency/promotion, source-release and pin changes through final unlink and
directory sync. Check every category, including offline assigned replicas and
advertised point-in-time recovery/backup sets. Unknown categories/coverage block
reclaim. An offline replica either retains incremental source coverage, or has an
explicit durable policy transition to a pinned verified baseline plus suffix.
That transition preserves resulting state, not literal intermediate replay; do
not silently change its recovery promise. Capacity pressure throttles/stops
admission rather than drops needed origins, claims or pins.

Only delete whole closed source segments whose exact bytes are covered. Persist
reclaim intent and sync each ordered unlink before the next; every restart must
reauthenticate baseline, sources and fresh dependency gates. A stale reclaim file
is evidence, never reusable permission. External ZFS snapshots/clones/holds and
backup archives have independent owners: daemon GC cannot prune them, infer their
capture/release from timestamps or assume unlinked blocks are freed. The
[backup capture contract](../Snapshots-and-restore.md#complete-all-origin-recovery-sets)
requires explicit holds and verified complete recovery sets before releasing
capture pins. No distributed GC, semantic-history compaction or power-loss proof
is supplied by the local implementation or seeded AllOrigin graphs.

## Scoped v1 local implementation and evidence

The remaining sections describe exactly the existing bounded single-source v1
checkpoint package. Their codec and tests preserve their original scope and bytes;
none implements the all-origin manifest, authenticated source-release or bootstrap
contract above.

`internal/checkpoint` implements bounded local checkpoint publication and source
segment reclamation for explicitly admitted disposable single-node callers.
It requires trusted external authority and retention implementations. It adds no
normal replication verifier, ACCEPT record, emergency authority, automatic
single-node write policy or daemon/lab deployment.

The existing `fsservice.New` path still requires genesis. **Never trim its source
stream.** A future service integration must hold the read/apply/ownership gates,
seal and refresh its selected source horizon, call `Result.Capture`, publish the
checkpoint, and use `checkpoint.Recover`, `segment.OpenCheckpointSuffix` and
`apply.Resume` on every later recovery. Its allocation/collision checks must
include archived frames and permanent external reservations, including
unselected creation attempts, and its structural UUID allocator must exclude
archived segment identities. No service permission or retention grant is inferred.

## Consistent immutable cut

A successful apply `Result.Capture` requires its independently selected `Through`
to end on a verified segment SEAL. The caller excludes source mutation, reads,
apply and administrative changes during capture; the authority retains all
commit/closure/serving gates. The selected effects remain the exact complete
protected effect list, beginning with VOLUME_INIT. Through can include trailing
structural and unselected frames; the last applied source effect and the
contiguous backing slot remain separate positions.

`storage.Capture` exports/synchronizes the complete catalog, then makes independent
byte copies of every live regular payload, including unlinked pinned objects and
logical holes. These bytes do not share mutable files or native hard links with
the working store. The image retains raw names/text, exact metadata, namespace
revisions/tombstones, parent/reference ledgers, active/released pins, retired
allocation descriptors and operation receipts. Hash/size verification rejects a
catalog whose bytes do not match its independent payload copy.

The baseline also preserves **every original source frame from genesis through
the seal**, selected or unselected. The segment validator checks exact sequence,
OPEN/SEAL linkage, unique segment IDs, predecessor references, counts and original
byte hashes. This preserves allocation attempts, requests and session claims,
prepares/decisions, operation identity/deduplication, configuration/authority,
uncertainty-resolution and lifetime evidence. Prefix frames are permanent
checkpoint contents and remain available for later dependency resolution. External
referenced artifacts and cross-home closure remain the trusted authority's
responsibility; their mere hashes do not establish recoverability.

This is intentionally bounded segment reclamation, **not semantic-history
compaction**. Archived source bytes are duplicated and retained permanently;
checkpointing can increase total space. All admitted histories and their
permanent evidence still count against apply's default 4096-frame/64 MiB source
limits. Defaults additionally bound payload copies to 64 MiB and the complete
encoded artifact to 192 MiB (hard maximum 1 GiB). Before initial encoding, Publish
also bounds each nested vector count, frame/source bytes, payload bytes, catalog,
proof and aggregate raw input; its exact encoded-size check additionally accounts
for JSON/base64 overhead. Catalog/proof/progress limits
also apply. Callers must admit aggregate storage for every retained baseline,
orphan, reservation, failed workspace and temporary copy. Snapshot copies use
logical bytes, so sparse holes count in the budget. No unlimited-write or net
space-recovery claim is made.

## Authority and publication

The checkpoint root is separate, private, caller-owned and administratively
immutable. The caller admits ancestor/descendant/mount safety and excludes all
other writers. `Authority.Enter` authenticates the exact manifest and opaque
checkpoint evidence and holds ownership, source, selection, membership and
checkpoint gates. It returns a nonnil release. `AuthorizeBaseline` independently
binds the exact baseline SHA-256 to the source digest and selection, and must
establish that these payload/catalog bytes are the corresponding installed state.
On restart it must authenticate the accepted baseline digest; a syntactically
valid local manifest, checksum or APPLIED pointer is insufficient. There is no
production implementation or permissive fallback. Tests use explicit fixture
baseline digests and disposable authority/protection data.

Version 1 artifacts use deterministic canonical Go JSON. Source/catalog/payload
bytes and proof bytes are base64; UUIDs/hashes in checkpoint structures are fixed
byte arrays. The catalog itself uses the backing store's strict versioned codec,
including base64 raw names. Decoding requires exact canonical re-encoding,
rejecting alternate, missing/duplicate/unknown fields and trailing data.

Publication writes these exclusively with mode 0600:

1. `baseline-<uuid>.hfb`: version, complete stream identity, canonical catalog,
   independent payload byte vectors, all exact prefix frames, selection/proof,
   backing slot and applied source reference; file sync then root sync.
2. `manifest-<uuid>.json`: version, fresh checkpoint UUID, volume, generation,
   sealed Through, exact baseline byte length/hash, required suffix sequence
   (Through + 1), external checkpoint evidence; file sync then root sync.
3. `CURRENT.tmp-<uuid>`: version, checkpoint UUID, exact manifest length/hash;
   file sync and root sync, atomic rename over `CURRENT`, then root sync.

A checkpoint UUID is never reused, including after an interrupted attempt.
Older immutable artifacts and all orphans remain. New publication cannot regress
or replace a differing source/selection prefix or change stream identity.
History-generation transitions require a future explicit recovery protocol.
`CURRENT` here belongs to the separate checkpoint root, not backing storage's
local CURRENT or journal CHECKPOINT_ACCEPT. Storage StageCheckpoint and
PublishCheckpoint remain local barrier primitives with mutable-payload catalogs;
they are never used to authorize this reclamation protocol.

## Dependencies, boundary and crash recovery

`Trim` loads CURRENT, enters authority gates, rechecks the exact current pointer,
verifies its complete manifest/baseline and revalidates all surviving source bytes.
It then obtains a fresh `RetentionDependency` lease. A grant must bind the exact
manifest digest and Through, contain nonempty bounded proof and explicitly
establish complete knowledge of assigned replicas, transfer/promotion/emergency/
checkpoint pins, recovery holds and advertised point-in-time recovery plans.
Any unknown category, missing implementation or conflicting binding blocks all
deletions. Replica coverage must be durable; partner memory ACK never suffices.
The dependency implementation must hold its gates through the final unlink sync.
Future multi-node policies remain external; this package supplies no fake
empty-membership or implicit no-holds implementation.

After these checks Trim re-syncs CURRENT and its root, writes/syncs
`reclaim-<uuid>.json` containing the exact manifest and root-syncs it, then removes
only verified closed source segments covered through that seal. It unlinks in
ascending sequence order and directory-syncs **each unlink before starting the
next**. A stale reclaim file is evidence only; every retry/restart reacquires
fresh authority and dependency gates. No timestamp or largest-record heuristic
is used. Open segments and every later frame remain.

`VerifyCheckpointSource` handles obsolete segments that survived a crash. It
locates the earliest remaining segment, derives its predecessor only from the
fully verified immutable prefix, verifies the complete surviving OPEN/SEAL chain,
compares every surviving archived frame byte-for-byte, rejects a suffix OPEN
that reuses any archived segment identity (even after its source file is gone),
and rejects an interior
gap, missing required suffix, changed prefix or torn/corrupt source. Ordered
per-unlink sync permits only a missing leading portion of the archived prefix.
It performs no repair/truncation. The caller must retain its outer recovery,
source-retention and visibility gates across `checkpoint.Recover`, opening the
suffix and the complete `apply.Resume` operation. Recover releases its internal
Authority.Enter lease on return; that lease alone cannot protect the later
recovery sequence. No source append/trim, baseline change or serving may occur
in that interval. `OpenCheckpointSuffix` yields only frames after
the final archived seal, even when obsolete source files remain.

`apply.Resume` validates/authenticates the baseline, reads and authorizes the
complete archive-plus-suffix selection, checks identity, history and backing-slot
correspondence, and then copies the immutable baseline into fresh empty private
data/journal roots. Progress also requires a fresh root with exact mode 0700 and administrative owner
validation before any destination write, preserving all
old attempts. No old APPLIED or dirty working tree becomes a baseline. It applies
only new selected effects, resolving earlier MUTATION/PREPARED dependencies and
exact operation retries from the permanent archive. Resume's Result.Advance takes
a freshly verified checkpoint suffix reader; it combines that suffix with the
retained archive internally. Result.Capture can establish the next sealed cut.

## Local validation

OS temporary-root tests also interrupt every checkpoint/trim hook and recover
using fresh destinations; these establish syscall error outcomes rather than
power-loss behavior. OS temporary-root and VFS tests verify real independent payload restoration and
namespace/metadata outcomes, shrink/extend, old IDs after name reuse, exact retry
slot mapping, PREPARED-before-baseline/INSTALL-after-baseline, live pin release,
reclaim, released pin tombstones and retired allocation persistence across a
second baseline. Corruption and authority failures are rejected before writing
fresh destinations; unknown dependency categories and corrupted CURRENT,
manifest, baseline or source block reclamation.

The simulator crashes at all checkpoint publication, reclaim-boundary and ordered
unlink/barrier hook occurrences under data Lose/Keep/Tear and namespace Lose/Keep.
Additional injected artifact write and file-sync errors cover partial baseline,
manifest and pointer creation. Recovery proves either complete genesis remains
before publication, or authenticated immutable baseline plus required suffix
produces expected bytes, namespace and revisions, including after a second crash
and trim retry. These are local model/syscall tests. Native FUSE, FreeBSD/ZFS power
loss, replica failover and lab VM tests retain their separate gates; none ran here.
