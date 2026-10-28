# Local apply and crash replay

`internal/apply` implements bounded single-home replay using the typed journal
codec, verified segment reader and stable-ID backing store. It is a local
primitive for a trusted recovery/service caller. It supplies no replication,
normal-mode client acknowledgement, fencing, shared visibility, request/session
service or checkpoint acceptance. No lab or native kernel evidence is implied.

## Authoritative input and caller gates

`Replay` requires an `OpenReader` over a fixed retained owner stream beginning at
sequence one, its exact final `Through` reference, and a complete ordered list of
selected effect references. The first selected effect is protected `VOLUME_INIT`.
Subsequent effects are selected ordinary `COMMIT`, committed participant
`INSTALL`, protected `HANDLE_PIN`, `HANDLE_RELEASE` or `RECLAIM`. A mutation,
prepare, abort or other control record never becomes a backing effect by mere
presence. All records remain retained, including unselected proposals, session
claims, decision evidence, pins and acceptance dependencies.

A mandatory trusted `Authorizer` verifies the nonempty bounded proof against the
exact supplied selection and history digest. It must establish the complete
selected commit/decision set through `Through`, protection and recoverability,
all local and foreign dependencies, authority and closure gates. This is an
explicit trust seam for the future replication/recovery implementation, not a
built-in verifier. A no-op test authorizer is only a test fixture; the repository
contains no production authorizer. A local COMMIT, fsync receipt, checksum or
largest sequence does not satisfy the contract. The authority must keep its
external gates held throughout replay and until all participant installations
and the shared visibility/cache barriers are complete.

The digest is SHA-256 over each source frame in order: its exact byte length as a
big-endian u64, its complete original bytes and one byte indicating whether the
frame is selected (0 or 1). A prefix digest stops after a selected frame. This
binds omissions and exact frame order as well as selected contents. Default
admission bounds are 4096 frames, 64 MiB of complete source bytes, 1 MiB proof and
256 MiB of immutable progress files per attempt. Storage catalog/object limits
also apply. A caller may explicitly admit different finite budgets and must
separately budget all retained workspaces/attempts and source dependencies.

Segment continuity, OPEN/SEAL checks, original-byte preservation and torn-tail
reconciliation remain in `internal/journal/segment`. Apply performs no tail
repair, truncation, scan-ahead or source rewrite. A repair requires the segment
caller's separate exact-byte evidence preservation and authority contract; the
apply selection is no substitute for that authorization.

## Fresh rebuild and stable identities

Each attempt uses new empty, distinct, private data and journal roots and a fresh
workspace UUID, plus a separate private progress root. Root overlap, concurrent
administrative changes and multiple callers are excluded by admission. Exact
root aliases are rejected. `Format` initializes this explicit unpublished
reconstruction destination using the selected root identity and metadata. It
never formats or opens an existing dirty store implicitly. Failed, superseded
and interrupted workspaces are preserved for inspection.

Recovery always rebuilds the entire selected history from the empty-payload
protected genesis; it does not skip old effects using APPLIED. This is the
initial clean-baseline strategy. It needs no checkpoint payload copy and never
feeds a dirty payload tree to `storage.Open`. In particular, a partial later
write or shrink cannot contaminate recovery of an older catalog. Selecting a
history incompatible with existing APPLIED fails closed; a separately authorized
loss-generation transition and its progress namespace are future recovery work.
A separate explicit `Capture`/`Resume` path now supports independently preserved
immutable payloads and exact archived source evidence; see [Checkpoints](Checkpoints.md).
Ordinary Replay and current filesystem-service startup still require genesis.
Replay/APPLIED alone never grants compaction or deletion authority.

A selected ordinary COMMIT must resolve the exact earlier MUTATION. INSTALL
resolves the exact earlier local PREPARED and checks its binding, request/digest,
redo and redo hash. Complete graph authorization remains the external verifier's
job. Mutation arguments and namespace edits must agree on exact parent/name and
source/replacement IDs. The adapter converts exact metadata afterimages,
reference removals/additions and parent transitions into backing afterimages;
it never resolves old workload paths or repeats arithmetic link deltas.

The backing sequence is a separate contiguous redo-slot sequence in the same
history. It advances only when a local primitive is installed. An anchor-only
participant with no local redo consumes no backing slot. APPLIED records both
the source effect and the backing slot. Exact operation retries compare complete
normalized selected-record bytes (only its source sequence is normalized),
consume no second slot and cannot recreate an old object over a reused name.
Conflicting operation identity reuse fails closed. Every restart reconstructs
this dedup state from the retained selected history.

Namespace replacement retains the replaced allocation/payload and its handle
pins. Pin release matches the exact object, serving generation and incarnation;
reclaim additionally requires selected protected retention authority and the
backend's zero-reference/pin checks. Directory metadata nlink is distinct from
its own namespace-reference cardinality and parent relation. Adding/removing a
child can change directory metadata without changing that directory's reference
ledger revision. Moved directories retain their object homes and exact parent
relations. Local backend closure checks still refuse foreign effects without
an external coordinated closure implementation.

## Effects and applied progress

Replay holds its partially constructed store privately. For each selected effect
it applies the exact-ID primitive, then calls `Store.Export`, synchronizing payload
bytes/size, identity files and directory dependencies. Only after those barriers
does it encode a complete local image and publish applied progress. On any error
Replay returns no Store; all resources and uncertain effects remain private.
Returning a Store establishes local installation only, while the caller's
external serving/visibility gates remain closed.

Progress uses a separate root, never backing-store `CURRENT` or
`CHECKPOINT_ACCEPT`. Each image is written exclusively as
`image-<workspace-uuid>-<20-digit-effect-index>.json`, fsynced and followed by a
parent sync. A fresh `APPLIED.tmp-<workspace>-<effect-index>` is written and
fsynced, renamed over `APPLIED`, then followed by a parent sync. Older images and
all temporary/orphan files remain. During rebuild, publication waits until the
old applied effect is reached, so APPLIED cannot regress to an earlier prefix.

APPLIED is a 242-byte versioned binary record: magic `HFSAPPL` then NUL (8 bytes),
major u16 (=1), workspace UUID, zero-based selected-effect index u64, backing slot
u64, source Position (three UUIDs, epoch u64 and sequence u64), source frame
SHA-256, selected-prefix SHA-256, image length u64, image SHA-256 and trailing
SHA-256 of all preceding bytes. Integers are big endian. Owner stream identity
is implicit; reporter references are forbidden. Unknown version/length,
corruption, missing/wrong image, conflicting selected prefix or reused current
workspace UUID fails closed before formatting new roots. Image decoding uses
the backing store's strict schema and configured limits.

An image stored here records a local installed cut; its payloads can be changed
by later effects in that workspace. It therefore cannot be reused as a clean
baseline just because its JSON is durable. Recovery rebuilds into new roots.
APPLIED provides neither distributed checkpoint acceptance nor GC permission.
The returned Applied reference conservatively names the last selected backing
effect; trailing control/structural/candidate frames are scanned but do not
advance it. The selected `Through` remains the independently authorized source
horizon. The separate checkpoint controller pairs complete prefix evidence and independent
payload copies with explicit authority/retention gates before advancing retention.
It never treats these progress images as a clean payload baseline.

## Local verification

Temporary-root OS and VFS simulator tests compare namespace revisions, exact
metadata, object contents, ledgers, parent relations and lifetime evidence with
an independent model. Cases cover all primitive kinds, unselected prepare and
mutation staging, replacement with live pins, old-ID writes after name reuse,
shrink/extend, duplicate old COMMIT, strict selection/progress failures and
complete ordered restart replay. Three fixed command seeds generate additional
namespace/content histories.

Every occurrence of the seven apply/publication hooks is crashed under data
Lose/Keep/Tear and namespace Lose/Keep. Additional seeded VFS schedules stop after
mkdir, payload and progress writes, truncate, file sync, directory sync and rename;
recovery uses new roots and compares the complete result with the model. Journal
append crash composition exercises before/after append and sync: surviving
unselected candidates stay unapplied; malformed/torn source suffixes gate without
unauthorized repair. Separate durability checks reopen the exact published image
only at the publication cut after simulated loss. These are local model tests,
not storage hardware power-loss, FUSE, distributed recovery or lab VM tests.

## Incremental service application

`Result.Advance` extends a successful Replay result in the same workspace behind
caller-owned read/apply/authority gates. It rereads the complete bounded genesis
history, rejects any changed old source bytes or selected effect prefix, obtains
fresh exact-history authorization and applies only newly selected effects with
the same deduplication, payload, image and APPLIED barriers. Progress budget is
cumulative across the result lifetime. Source reads must finish before appends
resume. Do not copy or concurrently use the mutable Result/Store.

Any extension failure poisons the result for further Advance calls. The service
closes its serving gate and recovers using Replay into fresh roots. This seam
adds no dirty-baseline reuse, checkpoint acceptance, retention permission,
follower, production authorizer or normal acknowledgement policy. See the
[single-volume service contract](Filesystem-service.md) for command ordering,
proof/permission hooks and the explicit disposable local validation profile.

## Immutable baseline and selected suffix

`Result.Capture` freezes a verified sealed source horizon, independent catalog and
payload bytes, and every exact prefix frame. `Resume` requires an authenticated
baseline, fresh private data/journal/progress roots and an explicitly authorized
archive-plus-suffix selection. It restores the clean baseline before applying only
new selected effects. Permanent source frames retain earlier dependencies and
operation identity deduplication. A resumed Result.Advance accepts a verified
checkpoint suffix reader and combines the archived prefix internally. See the
[checkpoint contract](Checkpoints.md) for publication, bounded storage, lifetime
and allocation evidence, authority/dependency leases and ordered segment trim.
