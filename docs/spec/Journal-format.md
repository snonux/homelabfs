# Journal records and segments

Status: version 1.0 design contract for task `cq3`, 2026-10-28. No encoder,
replication wire protocol, checkpoint codec or distributed commit implementation
exists as a result of this document. The loopback PoC implements none of it.
Scope is disposable lab/canary volumes. This specifies representation; it does
not establish that a received record is committed, a fence effective, or a
checkpoint eligible for deletion.

Read [Backing store](Backing-store.md), [Transactions](Transactions.md),
[Durability](Durability.md), [Replication](../Replication.md),
[Journal storage](../Journal-storage.md) and [Recovery](../Recovery.md) together.
Their authority, protection, visibility and retention requirements apply to every
record below. Normal success still requires one durable local complete prefix
and the common partner's retained memory, with the required decision/visibility
conditions; no second durable replica or automatic local-only fallback is added.
The ordinary commit/decision-slot model and concrete platform fence remain gates
before implementation can advertise these capabilities.

## Byte conventions and limits

All integers, including lengths and stored checksums, are big endian. `u8`,
`u16`, `u32`, `u64` are unsigned fixed-width integers; `i64` is two's complement.
There are no native structs, pointers, varints, alignment bytes or implicit
terminators. `uuid` is 16 bytes in displayed UUID byte order, never a string or
host-endian GUID. Required identities are nonzero. Recovery generation and
configuration IDs are UUIDs; epochs, terms, revisions and sequence numbers are
u64 and never wrap. Sequence zero denotes an empty prefix only, never a frame.

The following notation is normative: `A || B` concatenates encodings in the
listed order; `bytes[N]` is exactly N raw bytes; `blob` is `u32 length || bytes`;
`array<T>` is `u32 count || T...`; `optional<T>` is `u8 present` followed by T
only when present is 1. Booleans/presence accept only 0 or 1. Every schema lists
fields in wire order. Every byte must be consumed; trailing fields, padding,
unknown enum values, duplicate keys and reserved nonzero bits are errors.
No optional field can be inferred from a zero UUID. Hashes are 32 raw SHA-256
bytes of the exact stored bytes specified, never of a decoded/re-encoded value.
SHA-256 uses its standard byte result; all-zero hashes are not sentinels.

Version 1.0 has these hard bounds; admission profiles may lower them:

| Value | Inclusive limit |
|---|---|
| Complete frame | 8,388,608 bytes (8 MiB), including header/checksum |
| Generic blob; evidence bundle's total embedded frame bytes | 4,194,304 bytes |
| Write data per mutation | 1,048,576 bytes; nonempty |
| Name component | 1..255 bytes, excluding NUL, slash, `.` and `..` |
| Symlink text | 1..4096 bytes, excluding NUL; not normalized or followed by replay |
| Path | 1..256 components, at most 16,384 component bytes |
| Principal subject | 1..1024 bytes |
| Supplementary groups | 1024 distinct u32 values |
| Closure/participant/authority/dependency vector | 64 groups |
| Entry/object/parent/ledger edits per redo | 256 of each |
| Added/removed references per ledger edit | 256 of each |
| Any array without a smaller type-specific limit | 4096 elements |
| Reference count and regular-file nlink | 4096 per object in this initial profile |
| Evidence frames, pin/reference/replacement lists | 4096 per list |
| External artifact | 1,073,741,824 bytes; additional admitted capacity limits apply |
| Regular-file size, offsets and offset+data length | 0..2^63-1 |
| Segment | 268,435,456 bytes including OPEN, SEAL and checksums |

Do checked arithmetic before allocation or offset advancement. Check total
remaining bytes, minimum element sizes and each per-type bound before allocating
an array; a permitted count alone is not an allocation budget. Bound cumulative
nested bytes and counts by the complete enclosing frame too. No compression,
fragmentation, external write-data references or indefinite-length objects exist
in 1.0. Split a large ordinary write into separately acknowledged returned ranges;
an atomic namespace operation that cannot fit is rejected before admission.
Never truncate an admitted redo/evidence set to fit. Full catalogs are external
checkpoint artifacts, rather than one unbounded journal record.

Names, symlink text, principal subjects and paths preserve exact bytes, case and
Unicode spelling. No normalization occurs. Structured IDs and arrays are binary;
the base64 conventions for JSON backing manifests do not apply to these frames.
Arrays that represent sets are strictly sorted by the complete encoded key,
with no duplicates; ordinary positional arrays explicitly say otherwise. Numeric
sorting is equivalent to byte sorting for these big-endian unsigned keys.

## Frame header and checksum

Disk and replication transfer use the identical frame bytes. Transport sessions
must independently authenticate/authorize peers and bind configuration and
incarnation; a checksum is not authentication. A transfer receiver enforces the
same limits before buffering. Network receipt never repairs or reorders a disk
stream implicitly.

| Offset | Bytes | Field |
|---:|---:|---|
| 0 | 8 | Magic `48 46 53 4a 52 45 43 00` (`HFSJREC` then NUL) |
| 8 | 2 | Major = 1 |
| 10 | 2 | Minor = 0 |
| 12 | 2 | Header length = 144 |
| 14 | 2 | Record kind from the tables below |
| 16 | 4 | Flags = 0 |
| 20 | 4 | Total frame length = 148 + payload length |
| 24 | 4 | Payload length |
| 28 | 4 | Reserved = 0 |
| 32 | 16 | Volume UUID (the group owning this stream) |
| 48 | 16 | Recovery-generation UUID |
| 64 | 16 | Configuration UUID |
| 80 | 8 | Ownership epoch |
| 88 | 8 | Sequence, starting at 1 for each generation/configuration/epoch stream |
| 96 | 16 | Operation UUID: stable internal retry identity, chosen once by owner |
| 112 | 16 | Sender daemon incarnation UUID |
| 128 | 8 | Owner-selected observation timestamp, signed Unix seconds |
| 136 | 4 | Observation timestamp nanoseconds, 0..999,999,999 |
| 140 | 4 | Reserved = 0 |
| 144 | payload length | Typed payload |
| 144 + payload length | 4 | CRC32C of bytes `[0, 144 + payload length)` |

CRC32C is Castagnoli, reflected polynomial `0x82f63b78`, initial register
`0xffffffff`, reflected byte input, final XOR `0xffffffff`. Its check value for
ASCII `123456789` is `0xe3069283`. The stored result is a big-endian u32.
Length, magic, identity, kind and all payload bytes are covered; the checksum
field itself is excluded. SHA-256 references include the complete frame,
including this checksum. Validate the fixed prefix/lengths before allocating or
using identities, then checksum, schema, and stream/protocol invariants.

The timestamp is diagnostic, chosen once and retained on retransmission. It is
neither apply time, ordering, lease expiry nor recovery authority. Metadata times
are exact separate after-images below. Operation UUID identifies an internal
command across retries; per-record position and exact frame bytes identify a
record. Several records for one command can share its operation UUID. Anchored
application identity is carried explicitly in transaction payloads, not compressed
into this UUID. Duplicate position requires identical complete bytes. Reuse of
an operation identity with inconsistent command contents is a protocol error.

Unknown major/minor/header length/kind/mandatory feature is an unsupported format:
stop before replay/serving and preserve bytes. There are no skippable unknown
records or extension tags in 1.0. Never rewrite, downgrade, format-on-open or
scan past such a record. A future upgrader must be explicit, gated, retain the
old recovery unit and account for every replica/snapshot reader. Identical
on-wire framing does not make an unknown version negotiable as 1.0.

## Shared schemas

The capitalized types below are reusable exact encodings, not Go serialization.
`Position = volume:uuid || generation:uuid || configuration:uuid || epoch:u64 ||
sequence:u64`. A zero sequence is permitted only for a prefix position with a
known history. `FrameRef = stream_kind:u8 || reporter:optional<uuid> ||
reporter_incarnation:optional<uuid> || position:Position || frame_sha256:bytes[32]`
refers to a nonzero sequence. Stream kind 1 (owner) requires both optionals absent;
kind 2 (reporter evidence) requires both present. References to mutation,
claim/bind/begin, prepare/decision/install/complete, fence, checkpoint and retention
records require kind 1; ACCEPT/PROMISE/TAIL_REPORT require kind 2. Every other
reference list permits the required kind of its referenced record. Sort FrameRef
sets by `(stream kind, reporter, incarnation, Position)` ignoring the hash for
key equality; unequal hashes at one key are corruption. `Vector = array<Position>` sorted by volume UUID, one entry
per group. A vector never collapses separate epochs or configurations into a
single maximum number. Older-history dependencies require separate FrameRefs.

`ObjectID = home:uuid || allocation:uuid`. It is never recycled. Parent directories,
source/replacement objects, payload targets and ledger edits use this ID, never
native inodes or resolved paths. All descendants of a moved directory retain
homes. The frame's volume must own every edit in its local Redo; references to
objects in other homes are allowed and require the dependency closure.

`Time = seconds:i64 || nanoseconds:u32`, nanoseconds below 1,000,000,000.
`Principal = realm:uuid || subject:blob`. Realm identifies the authentication
namespace; subject is its immutable canonical principal identifier. The trusted
authenticator defines it before request admission. Numeric execution credentials
are separate: `Credentials = uid:u32 || gid:u32 || groups:array<u32>` (sorted).
A changed credential set never changes a previously claimed request identity.

`RequestID = principal:Principal || anchor:uuid || session_nonce:uuid || number:u64`.
Numbers start at 1; the session key omits number. `Endpoint = namespace_volume:uuid
|| view:uuid || components:array<blob>`, with the path bounds above and ordered
components. View policy determines authorized traversal; it is not a host path.
`OriginalRequest = api_version:u16 (=1) || id:RequestID || operation:u8 (1 rename,
2 link) || source:Endpoint || destination:Endpoint || replacement:u8 (1 replace,
2 no_replace for rename; 2 for link) || expected_source:optional<ObjectID> ||
expected_source_parent_revision:optional<u64> || expected_source_name_revision:
optional<u64> || expected_destination_parent_revision:optional<u64> ||
expected_destination_name_revision:optional<u64>`.
Present parent revisions must be nonzero. Present name revisions may be zero
(the virgin absent EntryState); presence 1 plus zero is distinct from absence 0.
Canonical request bytes are this exact encoding. Digest = SHA-256 of those bytes.
Deadlines, routing hints, newly resolved IDs/revisions and execution credentials
are excluded. Retry compares exact OriginalRequest bytes as well as digest,
before any path resolution. The anchored principal in those bytes must match
the authenticated request identity.

Ordinary operations touching foreign homes use this same protected anchor and
two-phase machinery internally, without extending the public Rename/Link API.
`Intent = mutation_kind:u16 || arguments:<mutation kind schema below> ||
selected_metadata:array<ObjectEdit>` includes every globally selected metadata
after-image, sorted by ObjectID (at most 256), including expected old revision.
It excludes namespace/ledger/parent Redo, authorization and dependencies; it does
not exclude the requested values or metadata needed to reconstruct the command. `InternalRequest = internal_version:u16 (=1)
|| id:RequestID || namespace_volume:uuid || view:uuid || intent:Intent`.
It preserves the service's already resolved stable intent for an ordinary native
request, including requested creation mode/umask, explicitly selected SETATTR
values, chosen append offset/accepted bytes and all resulting metadata. Select
automatic times, ownership inheritance, parent metadata and special-bit effects
once before protecting an internal claim (or an application REQUEST_BIND).
No such value may be invented or resampled while preparing/recovering that claim.
Different requested values remain different internal canonical bytes even when
umask or an induced metadata rule happens to produce the same final mode. The service uses a
protected internal session and stable request number at an immutable anchor;
this grants no exactly-once promise to a new application syscall after restart.
`RequestDocument = class:u8 || original:<class schema>`: class 1 OriginalRequest,
class 2 InternalRequest. Application digest remains SHA-256 of OriginalRequest
alone (the public canonical byte form); internal digest is SHA-256 of the exact
ASCII bytes `HOMELABFS-INTERNAL` then NUL then InternalRequest bytes. The stored
class is immutable and part of claim equality. Internal duplicates compare exact
canonical internal bytes. The same retention, claim-before-effects and anchor
recovery requirements apply. A Binding's class/digest is checked against its
claim; an internal request can never be routed to the public Execute decoder.

`Authority = volume:uuid || generation:uuid || configuration:uuid || epoch:u64 ||
owner:uuid || partner:uuid || members:array<uuid> || coordinator_term:u64 ||
owner_incarnation:uuid || partner_incarnation:uuid`.
Members are sorted admitted storage-node UUIDs, initially exactly three; owner
and partner are different members. Emergency mode is not encoded by a missing
partner. It requires a separately authorized configuration/protocol version.
An authority vector is `array<Authority>` sorted by volume, at most 64. All
members of a transaction closure have compatible membership/owner/partner.
Epochs/configurations may differ across groups. A frame's sender incarnation
must be the authorized sender for its role; a receipt reporter is not the owner.

`AffinityRef = closure:uuid || manifest_version:u64 || manifest_sha256:bytes[32]`.
The hash is of the canonical AFFINITY payload. `Visibility = affinity:AffinityRef
|| generation:u64`. Versions/visibility generations start at 1 and do not wrap.
`Authorization = object:ObjectID || object_revision:u64 || view:uuid ||
policy_revision:u64 || credentials:Credentials` records the fresh permission
inputs used at admission; lists are sorted by `(object, view)`.

`Artifact = kind:u16 || artifact_id:uuid || length:u64 || sha256:bytes[32]`.
Kinds are 1 catalog, 2 data manifest, 3 paired snapshot manifest, 4 checkpoint
replacement manifest, 5 external fence attestation, 6 authority authorization, 7 cache-barrier attestation.
They identify immutable private files fetched by validated identity and hash,
never an arbitrary pathname/URL. Their own versioned schemas must validate;
a hash is not evidence of semantics. Catalog/data/backup file codecs remain
follow-on work under Backing-store; unknown codecs or missing referenced files
block serving/retention. This does not define an opaque transaction blob: all
transaction redo, requests, results and decision acceptance are typed here.

`Evidence = frames:array<blob>` contains complete frames, sorted by the full
stream key just defined, one per key; max 4096 frames and 4 MiB total
frame bytes. Embedded frames validate independently and MUST NOT contain another
Evidence field, even an empty one, or structural OPEN/SEAL records. Evidence
kind is derived from its known record kind; reporter identity is read from its
required payload fields and must match header incarnation and its FrameRef.
Consequently bundles cannot recurse. A record
that requires evidence carries direct known frames plus FrameRefs to any other
transitively required evidence already protected in retained history/checkpoints.
Before protection/replay, resolve the entire referenced graph with a bounded
visited-position set and verify hashes, history and contents. Missing/cyclic,
conflicting or over-budget dependencies block progress; no hash-only ACK.
The producer must reject oversized admission rather than omit required evidence.
Replicas may obtain the graph through bounded catch-up before acknowledging.
An Evidence bundle conveys records, not their commitment automatically.

`DecisionID = coordinator:uuid || coordinator_generation:uuid ||
coordinator_configuration:uuid || coordinator_epoch:u64 || request:RequestID ||
term:u64`. `DecisionRef = id:DecisionID || decision:FrameRef`.
`PrepareRef = participant:uuid || prepare:FrameRef || redo_sha256:bytes[32]`;
prepare vectors sort by participant and cover exactly the bound participant set.
`Binding = request:RequestID || request_digest:bytes[32] || affinity:AffinityRef ||
coordinator:uuid || intent:Intent || participants:array<uuid> || authorities:array<Authority> ||
dependencies:Vector`. Participants are sorted and include anchor, endpoint volumes
and every touched home/closure member; coordinator is the lowest participant UUID
at initial binding and stays immutable across later joins/recovery terms. Intent
is the resolved stable operation selected once before protecting REQUEST_BIND;
it is separate from the original request digest. This preserves exact selected
objects/names/offsets even if a crash occurs after binding but before BEGIN.

`TerminalResult = status:u8 (1 committed, 2 aborted, 3 rejected) || code:u16 ||
source:optional<ObjectID> || replacement:optional<ObjectID> ||
decision:optional<DecisionRef> || committed:Vector || visibility:optional<Visibility>
|| conflicts:array<uuid>` (sorted). Code 0 = success; 1 invalid input,
2 unsupported version, 3 unsupported type/capability, 4 missing object,
5 destination exists, 6 nonempty directory, 7 permission denied, 8 read-only,
9 stale expected revision, 10 cycle/root move, 11 placement conflict,
12 unavailable authority, 13 resource limit, 14 I/O failure, 15 cancelled.
Committed requires code 0, source, decision, full committed vector and visibility;
aborted requires nonzero code and abort decision; rejected has no decision,
visibility or committed vector and proves no admission. Conflict UUIDs occur only
for placement conflict. Replacement is present only for an actual removed object.
The API's pending/indeterminate/retired responses are not terminal decision values.

## Exact redo and mutations

`Metadata = revision:u64 || type:u8 (1 regular, 2 directory, 3 symlink) || uid:u32
|| gid:u32 || mode:u16 || size:u64 || nlink:u64 || atime:Time || mtime:Time ||
ctime:Time || symlink_text:blob`. Mode has only 07777 bits. Regular sizes obey
the offset bound; directory size is 0 in this format; symlink size equals its
text length. Text is empty except for symlinks. Revisions start at 1. The service
chooses all automatic times, umask/inheritance and special-bit effects once;
followers apply exact values. Initial atime is noatime except explicit SETATTR.
Object type and symlink text cannot change after allocation.

`EntryState = revision:u64 || target:optional<ObjectID>`; absent virgin entries
have revision 0, and removed names retain a nonzero absent revision.
`EntryEdit = parent:ObjectID || name:blob || before:EntryState || after:EntryState`.
Parent is a directory. After revision = before revision + 1; edits sort by
(parent,name). `ObjectEdit = object:ObjectID || expected_revision:u64 ||
after:Metadata` sorted by ObjectID. Expected 0 means new allocation, after 1;
otherwise after revision is expected+1. A no-op has no edit, never a revision bump.
The complete expected catalog at the selected checkpoint plus ordered redo must
match; a conflicting revision is corruption, not a reason to re-resolve a path.

`Reference = parent:ObjectID || name:blob || entry_revision:u64`, sorted by
(parent,name); revision identifies the exact reference removed/added.
`LedgerEdit = object:ObjectID || expected_revision:u64 || resulting_revision:u64
|| expected_nlink:u64 || resulting_nlink:u64 || remove:array<Reference> ||
add:array<Reference>` sorted by object. Ledger revision advances by 1; 0 creates
it. Check exact removed members, absence of added keys, resulting reference set
and exact resulting nlink, never replay `nlink++`. For regular files the reference
set cardinality is nlink; for directories/symlinks it is their namespace reference
set and directory link accounting is separately reflected in Metadata/ParentEdit.
`ParentState = revision:u64 || parent:ObjectID || parent_entry_revision:u64`;
`ParentEdit = directory:ObjectID || before:optional<ParentState> ||
after:ParentState` sorted by directory. Root parent points to itself with entry
revision 0. New relation revision is 1; existing advances by 1. Directories have
exactly one parent and no namespace hard links. Complete closure ancestry checks
precede preparation; redo preserves their selected result.

`Redo = objects:array<ObjectEdit> || entries:array<EntryEdit> ||
ledgers:array<LedgerEdit> || parents:array<ParentEdit>`. Every changed object's
metadata, including parent mtime/ctime/nlink, appears as its complete after-image.
A ledger's count agrees with its metadata when that count changes. Every entry
addition/removal has a matching home-ledger delta, including foreign references;
these may occur in a different participant's Redo under the same decision.
Directory moves include old/new parent accounting and the moved directory's
ParentEdit. Resulting object/ledger/link states are checked across all participants
before visibility. Admission treats all affected homes as dependencies even for
an ordinary request issued through one mount. Objects are not physically deleted
by logical unlink/replacement. Redo SHA-256 covers the exact encoded Redo.

`Mutation = intent:Intent || redo:Redo ||
authorizations:array<Authorization> || dependencies:Vector`.
All argument ObjectIDs are resolved once under admission locks. The redundant
operation intent and exact redo MUST agree. Its local Redo.objects is exactly
the Intent.selected_metadata subset whose ObjectID home equals header volume;
no omitted, extra or altered after-image is allowed. The full global selection
is retained in each immutable Binding/Intent, even on an anchor-only participant.
Creation has the selected new object revision 1, exact initial owner/mode/times
and required empty size/link/type state. SETATTR carries every masked requested
value below; its metadata must agree with those inputs and the recorded shared
Unix policy, including induced ctime/special-bit changes. Unmasked fields remain
at their expected old values except those explicit induced effects. It is an
error to accept a WRITE
whose target after-image differs from its byte range/size semantics, for example.
The owning group stores only its local Redo. An operation requiring another home
uses the transaction records below instead of independent MUTATION commits.

| Kind | Arguments in wire order; required semantics |
|---:|---|
| 1 CREATE | `parent:ObjectID, name:blob, object:ObjectID, exclusive:u8, requested_mode:u16, umask:u16`. Regular file, absent target, new parent-home allocation, empty payload/size, new entry/ledger and metadata. Exclusive is 0/1; even nonexclusive CREATE never overwrites an existing object's identity: opening an existing object is not a new CREATE. |
| 2 MKDIR | `parent:ObjectID, name:blob, object:ObjectID, requested_mode:u16, umask:u16`. Absent target, new directory/empty entries, initial parent relation and exact parent accounting. |
| 3 SYMLINK | `parent:ObjectID, name:blob, object:ObjectID, text:blob`. Absent target, exact text in new symlink metadata, no native host symlink. |
| 4 LINK | `source:ObjectID, target_parent:ObjectID, target_name:blob`. Regular source only, absent destination, same home/identity; add exact reference/count and parent metadata. |
| 5 UNLINK | `parent:ObjectID, name:blob, removed:ObjectID`. Nondirectory recorded occupant; remove exact entry/reference, preserve payload/pins. |
| 6 RMDIR | `parent:ObjectID, name:blob, removed:ObjectID`. Nonroot empty directory; exact entry/reference/parent accounting, lifetime remains pinned as needed. |
| 7 RENAME | `source_parent:ObjectID, source_name:blob, source:ObjectID, destination_parent:ObjectID, destination_name:blob, policy:u8 (1 replace/2 no_replace), replaced:optional<ObjectID>, noop:u8`. Record both old states, removed replacement and exact link/parent/ancestry changes. A regular same-object replacement or identical name is explicit no-op with empty Redo and empty selected_metadata; no guessed new occupant. Validate type compatibility, empty directory replacement, root/cycle prohibitions. |
| 8 WRITE | `object:ObjectID, offset:u64, data:blob`. Regular file, exact nonempty accepted range; resulting size = max(old size, offset+length), exact metadata including set-ID effects. Append is resolved to this offset at admission, never append during replay. |
| 9 TRUNCATE | `object:ObjectID, size:u64`. Regular file, exact result; extension yields zero bytes, shrink does not change identity or double-decrement anything. Record exact metadata. |
| 10 SETATTR | `object:ObjectID, mask:u16`. Bits 0 mode, 1 uid, 2 gid, 3 atime, 4 mtime; nonzero, other bits zero. After mask, encode exactly the fields whose bits are set, in ascending bit order: `mode:u16`, `uid:u32`, `gid:u32`, `atime:TimeSelection`, `mtime:TimeSelection`. Unset fields occupy no bytes. Requested values and complete selected resulting Metadata are both bound in Intent; the latter includes all induced ctime/special-bit changes. Size changes use TRUNCATE. No host-credential substitution. |

Creation requested_mode permits only 07777 bits; umask permits only 0777.
Compute the selected mode/UID/GID under the shared creation/inheritance rules
from those exact inputs and captured credentials/policy, before immutable claim
or binding. Record it in selected_metadata, never recompute it under replay
credentials. SYMLINK has no syscall-supplied mode; its fixed 0777 mode, exact
text and initial metadata are still selected and bound before preparation.
`TimeSelection = source:u8 || value:Time`: source 1 explicit timestamp, 2 now.
For now, value is the one exact owner-selected time chosen before claim/binding;
replay never samples a clock. An omitted timestamp uses an unset mask bit and
has no value. SETATTR mode/UID/GID and both timestamp source/value choices are
therefore distinguishable in canonical bytes independently of their after-images.

Fallocate, hole punching, zero-range allocation, device/FIFO/socket creation,
xattrs and ACLs are unsupported in 1.0 and fail before admission with an explicit
capability error. There is no fallocate record that a reader may guess is WRITE
or truncate. Adding one requires a new understood version and exact allocation,
zero-read, size, replay and capability semantics. Reads, locks, close/flush and
noatime reads are not mutations; pin/release and explicit sync barriers have
records below. No local metadata path has a weaker acknowledgement rule.

## Record kinds: ordinary state, identity and anchors

Payload fields in each row are concatenated in the printed order. Field types
already defined apply with their bounds. There are no unspecified trailing bytes.

| Kind | Name | Payload |
|---:|---|---|
| 1 | VOLUME_INIT | `root:ObjectID, initial:Metadata, root_parent:ParentState, root_ledger:LedgerEdit, authority:Authority, authorization:Artifact`. Root belongs to header volume, is a directory with self-parent; exact shared allocation and initialization, never format-on-open. |
| 2 | MUTATION | `mutation:Mutation`. Candidate redo only; receipt/durability alone does not authorize apply. |
| 3 | COMMIT | `mutation:FrameRef, authority:Authority, dependencies:Vector`. Exact normal commit candidate for the referenced MUTATION (kind 2); acceptance evidence is in later ACCEPT/recovery/result records to avoid a hash-reference cycle. Apply requires verified commit/protection under the future normal commit model; multi-group work uses DECISION. |
| 4 | BARRIER | `scope:u8 (1 file,2 directory,3 volume,4 closure), object:optional<ObjectID>, data_only:u8, covered:Vector, decisions:array<DecisionRef>, visibility:Visibility`. File/directory require object; other scopes forbid it. Covers captured prior admission order; not later concurrent requests or an application transaction. |
| 5 | HANDLE_PIN | `pin:uuid, object:ObjectID, serving_generation:uuid, incarnation:uuid, reasons:u8`. Bits 0 open,1 mapping,2 kernel lookup; nonzero, remaining zero. Home-owned conservative lifetime pin precedes exposing a handle/node. |
| 6 | HANDLE_RELEASE | `pin:uuid, object:ObjectID, serving_generation:uuid, incarnation:uuid, reason:u8 (1 all obligations drained,2 fenced incarnation), fence:optional<FrameRef>`. Reason 2 requires verified FENCE; reason 1 requires service-observed zero open/mapping/lookup obligations. Uncertainty retains pin. |
| 7 | RECLAIM | `object:ObjectID, expected_object_revision:u64, expected_ledger_revision:u64, retention:FrameRef, pins_released:array<FrameRef>`. Home-owned permanent allocation tombstone; only after zero references and all recovery/snapshot/handle pins released. Physical deletion is bounded resumable GC after this protected authorization. |
| 8 | PIN | `pin:uuid, action:u8 (1 acquire,2 release), purpose:u8 (1 snapshot,2 transfer,3 replica baseline,4 recovery,5 transaction,6 session), objects:array<ObjectID>, history:Vector, artifacts:array<Artifact>, predecessor:optional<FrameRef>`. Release matches acquisition contents and references it; no expiry. Lists sorted, bound recovery/data dependencies. |
| 9 | ORDINARY_RESULT | `mutation:FrameRef, commit:optional<FrameRef>, outcome:u8 (1 committed,2 aborted), code:u16, object:optional<ObjectID>, accepted_bytes:u32, resolution:optional<FrameRef>, dependencies:array<FrameRef>, evidence:Evidence`. Stable header operation UUID returns the same internal result. Commit requires COMMIT and complete acceptance graph; abort requires protected safe resolution, never a timeout. accepted_bytes is nonzero only for WRITE and equals its exact data length. Codes use TerminalResult codes. |
| 16 | SESSION | `principal:Principal, anchor:uuid, nonce:uuid, floor:u64 (=0), state:u8 (=1 live)`. At anchor; registration protects identity and lifetime pin before any claim. |
| 17 | REQUEST_CLAIM | `document:RequestDocument, digest:bytes[32]`. At immutable anchor; exact bytes/digest verified and claim/floor consulted before paths. |
| 18 | REQUEST_BIND | `claim:FrameRef, binding:Binding, authorizations:array<Authorization>`. At anchor; immutable binding after compatible affinity join, before BEGIN. |
| 19 | REQUEST_RESULT | `request:RequestID, digest:bytes[32], claim:FrameRef, binding:optional<FrameRef>, result:TerminalResult, dependencies:array<FrameRef>, evidence:Evidence`. Terminal rejection has no binding unless compatible placement preparation created one without admission; committed/aborted require bound decision/visibility dependencies. Protect before terminal reply. |
| 20 | SESSION_RETIRE | `principal:Principal, anchor:uuid, nonce:uuid, previous_floor:u64, new_floor:u64, terminal_results:array<FrameRef>, replacement:optional<Artifact>`. Strict increase, no unresolved claim at/below new floor; only terminal IDs/unused lower numbers covered. Replacements must preserve complete anchor state; all retries below floor are RETIRED_ID. |
| 21 | SESSION_CLOSE | `principal:Principal, anchor:uuid, nonce:uuid, floor:u64, retirement:optional<FrameRef>`. Every claim is terminal AND every claimed receipt is retired by the protected floor; retirement is absent only for a zero-floor session with no claims. Durable permanent tombstone rejects registration and every future number, releases only the live-session pin. |
| 22 | ANCHOR_RETIRE | `anchor:uuid, closed_sessions:array<FrameRef>, permanent_rejection:Artifact, authority:Authority`. Verified permanent retired-anchor UUID rejection mapping retained in registry/control; never reroute IDs to another volume. Does not erase unresolved state or allow UUID reuse. |

FRAME op UUIDs do not give arbitrary application retries exactly-once semantics.
Internal dedup keeps identity, command bytes/result and complete history until a
verified replacement exists. Anchored application sessions have no wall-clock
expiry. Floor/tombstone lookup takes precedence over resolution, including after
checkpoint, session closure or namespace retirement. Retiring a request never
releases home, catch-up or snapshot pins implicitly.

## Affinity and two-phase decisions

| Kind | Name | Payload |
|---:|---|---|
| 32 | AFFINITY | `closure:uuid, manifest_version:u64, members:array<uuid>, authorities:array<Authority>, predecessors:array<AffinityRef>`. Sorted complete member vector and predecessor manifests; hash exact payload. Every member stores identical payload, though headers differ. Gates remain closed until all copies protected. Initial affinity only grows. |
| 33 | BEGIN | `binding:Binding, bind:FrameRef, document:RequestDocument, authorizations:array<Authorization>, term:u64`. At immutable bound coordinator; class/digest match exact original claim. No path re-resolution after binding. |
| 34 | TX_ANCHOR | `request:RequestID, digest:bytes[32], binding:Binding, begin:FrameRef, coordinator_term:u64`. At every participant including anchor and homes; discover partial BEGIN fan-out on restart. |
| 35 | PREPARED | `request:RequestID, digest:bytes[32], begin:FrameRef, binding:Binding, term:u64, operation:Mutation, redo_sha256:bytes[32], retained:array<FrameRef>, artifacts:array<Artifact>`. Participant-local exact redo, affected ID/revision/authorization/lifetime state, complete payload/prior-write/staging dependencies. Mutation kind only LINK or RENAME for the application API; ordinary foreign-home operations may use any supported mutation. All local subsets share the exact global Intent retained in the immutable Binding. Source/destination arguments never vary across participants; only their local Redo/authorization/dependencies differ. |
| 36 | DECISION | `id:DecisionID, digest:bytes[32], begin:FrameRef, outcome:u8 (1 commit,2 abort), prepares:array<PrepareRef>, result:TerminalResult, authority:Authority, recovery:optional<FrameRef>`. One writer/outcome per coordinator term. COMMIT requires every bound participant's protected prepare and dependencies. ABORT may have partial prepares; its safe resolution still needs fencing/acceptance evidence. The embedded result has decision absent (avoids self-reference); final committed/aborted result adds the resulting DecisionRef. Visibility/committed vector are absent here until COMPLETE; other result fields are fixed. |
| 37 | INSTALL | `request:RequestID, digest:bytes[32], binding:Binding, decision:DecisionRef, outcome:u8 (1 commit,2 abort), prepare:optional<PrepareRef>, redo:optional<Redo>, applied:Position, acceptance:array<FrameRef>, dependencies:array<FrameRef>, evidence:Evidence`. Carries/retains complete recoverable decision acceptance and prepare graph. COMMIT requires exact local prepare/redo and applied dependency position; ABORT has no redo and cannot invent a different outcome. Protection and apply are distinct; field applied states completed local redo only. |
| 38 | COMPLETE | `request:RequestID, digest:bytes[32], decision:DecisionRef, installs:array<FrameRef>, installed:Vector, result:TerminalResult, visibility:Visibility, cache_barriers:array<FrameRef>`. At coordinator, exact bound participant install vector; after all local apply, alias invalidation/drain and shared barrier. Result decision/positions/visibility match; cache_barriers references verified VIEW_BARRIER records. Protect, then publish the one closure visibility generation; REQUEST_RESULT at anchor precedes reply. |

DECISION's provisional result uses the same TerminalResult byte schema with the
explicit exceptions in that row; it is not a terminal API receipt. For abort,
COMPLETE records released gates/staging with an abort result and unchanged
visibility; committed COMPLETE advances visibility once. A failure after protected
COMMIT leaves COMMIT irreversible, possibly gated, pending or indeterminate.
A duplicate INSTALL/COMPLETE must agree exactly with the same decision/revisions;
replay from the checkpoint cannot create another reference, allocation or count.
Participant timeout, missing COMPLETE or coordinator death is never ABORT proof.
The anchor may be the only group touched by a rejected preflight result; that
result consumes its claimed ID without namespace effects.

## Acceptance, fencing and recovery evidence

These fields represent evidence the protocol must verify; mere presence of the
record does not make its statements true. In particular this format cannot select
the physical guest/host fence or substitute for the pending commit model.

`Alias = view:uuid || object:optional<ObjectID> || parent:optional<ObjectID> ||
name:blob || invalidate:u8`. Bits 0 entry, 1 negative entry, 2 attributes, 3 data;
nonzero, others zero. Entry/negative flags require parent/name; attribute/data
flags require object. Name is empty only when no entry/negative flag is set.
Aliases sort by `(view,parent,name,object)` with no duplicates and are bounded by
4096 entries; admission must budget the alias set, never silently omit one.

`Tail = received:Position || durable:Position || accepted:Position || applied:Position
|| incarnation:uuid || continuous:u8 || missing:array<FrameRef>`.
Positions belong to the same reported stream and are contiguous prefixes; each
is tracked separately (applied cannot exceed committed/decided coverage).
Continuous means the reporting incarnation still owns its complete prior accepted
tail, or has verified durable coverage; restart from a durable prefix alone MUST
report false/unknown continuity. Missing references identify known missing data,
never establish absence of an unobserved old candidate.

| Kind | Name | Payload |
|---:|---|---|
| 48 | PROMISE | `recovery_id:uuid, authority:Authority, promised_epoch:u64, promised_term:u64, reporter:uuid, reporter_incarnation:uuid, tails:array<Tail>, prior_acceptances:array<FrameRef>, authorization:Artifact`. Promise persisted before reply; preserve accepted records and memory tail first. Scoped to every authority vector member by separate protected records. Higher promise cannot erase old accepted evidence. |
| 49 | ACCEPT | `slot_kind:u8 (1 ordinary commit,2 transaction decision), candidate:FrameRef, request:optional<RequestID>, term:u64, authority:Authority, reporter:uuid, reporter_incarnation:uuid, retained:Vector, dependencies:array<FrameRef>, durable:Vector`. Reporter-owned retention assertion for exact candidate/full prefix/dependencies; ordinary request absent, transaction present. Memory acceptance is not a durable-tail claim; durable vector may lag retained. |
| 50 | TAIL_REPORT | `recovery_id:uuid, reporter:uuid, reporter_incarnation:uuid, promises:array<FrameRef>, tails:array<Tail>, candidates:array<FrameRef>, dependencies:array<FrameRef>`. Authenticated bounded recovery report; gather actual tails, not just position summaries. |
| 51 | RECOVERY_SELECT | `recovery_id:uuid, slot_kind:u8 (1 ordinary,2 transaction), operation:uuid, binding:optional<Binding>, new_authorities:array<Authority>, promises:array<FrameRef>, reports:array<FrameRef>, selected:optional<FrameRef>, absence_abort:u8, dependencies:array<FrameRef>, fence:array<FrameRef>, evidence:Evidence`. Transaction requires binding and selected DECISION; ordinary forbids binding and selected value is COMMIT. Operation matches candidate's stable header operation identity. Required intersecting storage-majority evidence for every member. Adopt highest accepted term consistent with any protected decision, retaining all prepares; absent dependencies block recovery. Absence abort requires no constraining candidate and common partner continuous accepted tail or equivalent complete durable history. Exactly one of selected or absence_abort=1. Re-protect selected/new abort in the new pair before install/apply. |
| 52 | FENCE | `recovery_id:uuid, authorities:array<Authority>, old_owner:uuid, old_incarnation:uuid, serving_generations:array<uuid>, views:array<uuid>, mechanism:uuid, attestation:Artifact, completed_at:Time, dependencies:array<FrameRef>`. Positive completion evidence covering old serving rights, guest/client cached paths, descriptors/mappings and named views. Mechanism/attestation must have a separately supported authorized verifier; an unknown mechanism, lease timeout, API pod deletion or assertion without verification cannot open gates. Fences are never undone by a volume rollback. |
| 53 | CONFIGURATION | `recovery_id:uuid, predecessors:array<Authority>, successors:array<Authority>, closure:AffinityRef, recovered:Vector, promises:array<FrameRef>, fences:array<FrameRef>, partner_ready:array<FrameRef>, authorization:Artifact`. Fenced configuration transition preserving whole closure history and offline async obligations. Not a per-operation coordination lookup. |
| 54 | LOSS_RECOVERY | `recovery_id:uuid, previous:array<Authority>, selected:Vector, new_generation:uuid, unavailable_requests:array<RequestID>, unavailable_ranges:array<Position>, baselines:array<Artifact>, fences:array<FrameRef>, authorization:Artifact`. Explicit authorized loss selection into a new history; preserve old copies and unknown client outcomes, never automatically fill missing payloads or expose a partial transaction. Ranges are selected cut positions; all excluded suffixes remain uncertain, not an exhaustive claim of known lost requests. |

| 55 | VIEW_BARRIER | `affinity:AffinityRef, old_visibility:Visibility, new_visibility:Visibility, serving_generation:uuid, incarnation:uuid, aliases:array<Alias>, drained:u8 (=1), mechanism:uuid, attestation:Artifact, dependencies:array<FrameRef>`. Positive verified invalidation/update and in-flight-observation drain for every affected alias under the admitted platform profile. New visibility names the same affinity and advances generation by one. This may use supported cache invalidation or a separately authorized view quiesce/revoke/remount mechanism; it does not itself choose one or claim success from zero TTL. Missing verification keeps committed work gated. |

Normal transaction publication uses VIEW_BARRIER; it need not assert the old
owner was fenced. Takeover also requires FENCE covering old cached/client paths.
Attestation codecs and verifiers are profile-specific implementation gates;
unknown mechanisms/artifacts never establish either barrier. Decoding these
records is not a kernel-cache or physical-fence validation result.

Arrays of FrameRefs sort by their complete stream key. Acceptance evidence retains source
node/incarnation, slot, term, exact value and complete dependency coverage.
Conflicting values in one term or incompatible higher-term evidence quarantine
the history; timestamps and largest sequence numbers cannot settle them.
An ACCEPT frame can itself be journaled durably later; partner memory acceptance
may be transmitted as its exact frame before durability. The reporter stream is
still volume-bound, monotonic and reconciled; a reporter cannot append concurrently
to the owner's stream with its own sequence numbers. Evidence frames from a
reporter use a separately owned evidence stream (segment distinction below).
A bundle or fetched graph resolves ACCEPT/PROMISE/TAIL_REPORT references without
fabricating an empty tail for an offline/restarted member.

## Checkpoints, coverage and retirement

`Coverage = replica:uuid || incarnation:uuid || durable:Vector ||
baseline:optional<Artifact> || suffix_start:Vector || dependencies:array<FrameRef>`.
Durable prefix or verified baseline plus every subsequent required record must
cover each obligation. Memory-only receipt is insufficient. All snapshot,
transfer, transaction, session, home-reference and recovery pins are retained
until verified replacement; missing coverage is unknown, never caught up.

| Kind | Name | Payload |
|---:|---|---|
| 64 | CHECKPOINT_ACCEPT | `checkpoint:uuid, root:ObjectID, data_manifest:Artifact, catalog:Artifact, predecessor:optional<uuid>, committed:Vector, applied:Vector, visibility:Visibility, closure_checkpoint:uuid, closure_members:array<uuid>, dependencies:array<FrameRef>, replacements:array<FrameRef>, pins:array<FrameRef>, required_suffix:Vector`. Durable paired data/journal acceptance, same closure checkpoint/vector on all members. Replacement frames preserve complete decision/request/affinity/fence semantics, not just hashes. Full catalog carries exact object/entry/parent/ledger/tombstone/anchor state. |
| 65 | RETENTION | `retention_id:uuid, predecessor:optional<FrameRef>, checkpoint_accepts:array<FrameRef>, published_manifests:array<Artifact>, closure_checkpoint:uuid, reclaim_through:Vector, coverage:array<Coverage>, pins:array<FrameRef>, replacements:array<FrameRef>, authorities:array<Authority>`. Protected, durable boundary only after every member's accepted CURRENT pair is published/revalidated, complete durable replica coverage and pin registry checked under transition/GC serialization. |
| 66 | REPLICA_PROGRESS | `replica:uuid, incarnation:uuid, received:Vector, durable:Vector, decided:Vector, applied:Vector, baseline:optional<Artifact>, required_suffix:Vector, transfer_pins:array<FrameRef>`. Durable tracking of async/offline catch-up obligations; a received or applied position alone cannot advance retention. |

The external checkpoint codecs must preserve exact SESSION/CLAIM/BIND/RESULT,
floor/closed-session and retired-anchor rejection state, prepare/decision/install
records, authority/incarnation/fence history, affinity manifests, foreign ledger
and data dependencies. Until those strict codecs exist, keep the journal evidence;
CHECKPOINT_ACCEPT alone is not semantic compaction. `replacements` points to typed
retained records (or exact typed records retained by the verified replacement
manifest), so recovery resolves their full closure before pruning. A missing
manifest schema or unresolved reference blocks cleanup rather than weakening it.

Use Backing-store's payload and directory sync, immutable catalog/manifest,
journal acceptance then CURRENT publication protocol. A checksummed candidate
is not durable publication. An orphan acceptance is retained; partial closure
publication stays gated and preserves the complete predecessor. Physical segment
unlink occurs only for closed segments wholly covered by RETENTION, followed by
parent sync. Crash-safe extra copies are acceptable. Control/fence reconciliation
stays outside per-volume rollback. No wall-clock age, snapshot name, applied-file
size or highest frame number establishes checkpoint pairing or deletion safety.

## Segment representation and rotation

Each node has one owner journal stream per `(volume,generation,configuration,epoch)`.
It also has separately owned reporter evidence streams for persistent acceptance/
promise/tail records. Segment files use validated canonical lowercase UUIDs with
hyphens, and zero-padded 20-digit decimal u64 epochs/sequences:

```text
segments/<volume>/<generation>/<configuration>/e<epoch>/owner/
  <first>-open.hfj
  <first>-<last>.hfj
segments/<volume>/<generation>/<configuration>/e<epoch>/evidence/<reporter>/<incarnation>/
  <first>-open.hfj
  <first>-<last>.hfj
```

An evidence stream has its own sequence space, never mixed with owner positions.
FrameRef's stream kind/reporter/incarnation identifies it unambiguously; reporter
sequence 1 never aliases owner sequence 1. PROMISE/ACCEPT/TAIL_REPORT are the only
nonstructural records allowed in reporter streams. Their header incarnation must
match path and reporter fields. FENCE/CONFIGURATION recovery records have authorized
replicated owner-stream copies; their outside-volume control evidence is not
rolled back or made authoritative by those copies. Evidence references to prior
owner streams retain their complete distinct histories.

| Kind | Name | Payload |
|---:|---|---|
| 67 | SEGMENT_OPEN | `segment:uuid, stream_kind:u8 (1 owner,2 evidence), reporter:optional<uuid>, reporter_incarnation:optional<uuid>, first:u64, previous_seal:optional<FrameRef>, maximum_bytes:u64`. Header sequence = first. Evidence requires reporter/incarnation; owner forbids both. Maximum is profile-selected, at most 268435456 and at least this OPEN plus SEAL; first segment starts at 1, later starts previous seal sequence+1. |
| 68 | SEGMENT_SEAL | `segment:uuid, first:u64, last_before_seal:u64, frame_count:u64, byte_count:u64, content_sha256:bytes[32]`. Hash/count cover every complete frame from OPEN through the frame immediately preceding SEAL, including their CRCs; byte_count excludes SEAL. Seal header sequence = last_before_seal+1; count includes OPEN; closed filename last includes SEAL. |

There is no segment superblock, inter-record padding or footer beyond SEAL.
OPEN is the first frame, SEAL the last. Both consume stream sequence positions;
commit/applied vectors can cross structural frames without a filesystem effect.
Header identities, filename/directory, OPEN and seal all agree. The linked prior
seal and contiguous sequence verify segment boundaries; no overlap, gap,
duplicate filenames, unordered range or changed bytes is ignored. Sequence/config/
epoch rollover requires an authorized transition, not a new filename guessing
that an old tail was committed. Pin still-required prior histories explicitly.

Before append, reserve room for the complete candidate frame and seal (including
its fixed 80-byte payload, 228-byte total frame). If it will not fit, seal first.
Optional age rotation happens only between complete frames. Write/sync SEAL,
rename `first-open.hfj` to `first-last.hfj`, sync segment directory, then create
next OPEN with exclusive creation, write/sync it and sync its parent before using
it. A crash after seal sync but before rename leaves a verifiable sealed open-name
file that recovery can finish renaming without overwriting another file. A crash
before new OPEN leaves the old sealed stream usable, not a missing mutation.
Never append to a sealed file. Failed resources stay available for inspection;
a new history/configuration never overwrites old segment paths.

A receiver accepts duplicate delivery only at the same complete stream key with
identical bytes/hash. Missing sequence n blocks n+1's contiguous received/retained
ACK, even if n+1 is buffered within capacity. Same operation UUID at a different
position is reconciled by command identity, not applied as another allocation or
append; inconsistent redo/result is quarantined. Headers alone never authorize
a delayed old incarnation or fence violation.

## Torn tails, corruption and recovery

Start with a verified checkpoint/baseline and authorized history; verify every
required segment and ordered suffix behind closed serving gates. Do not select
a history by filename, timestamp, highest sequence, or valid CRC alone.

- An unsealed last open segment ending with fewer than 144 header bytes, or a
  structurally valid bounded header whose complete claimed frame is not present,
  has an incomplete tail at the last verified boundary. Preserve its bytes and
  reconcile owner/partner accepted tails. Ignore/truncate it only after authority
  proves no protected/accepted required record is being discarded, with the
  original retained as evidence. An incomplete final frame is never an absence
  proof. A file shorter than its OPEN is an interrupted creation, not an empty
  initialized volume.
- A fully present frame with invalid CRC, impossible length, invalid schema,
  unknown format or mismatched identity is corruption/unsupported data, even at
  EOF. Do not call it a torn write and erase it. A verified peer may rebuild a
  quarantined replica into a new private destination after history reconciliation.
- Any missing/truncated/bad frame before a later frame, in a closed segment, or
  before a required checkpoint/dependency boundary is interior corruption/gap.
  Stop; never search for the next magic, skip padding, drop a conflicting frame,
  infer ABORT or synthesize zeros. Bytes after SEAL are corruption.
- A valid complete tail frame without commit/decision evidence remains a candidate;
  it cannot become visible merely because sync or receipt succeeded. Preserve
  uncertain proposals until the fenced decision-resolution rule resolves them.
  Partner restart with an incomplete volatile tail blocks absence-based abort.
- Load metadata at C and replay the complete selected committed suffix in order.
  Partial in-place payload apply is reconstructed using its exact-ID redo and
  clean baseline. Selecting a cut excluding potentially applied bytes needs a
  verified uncontaminated baseline in a new destination. A missing home/payload,
  ledger, anchor, prepare or fence dependency gates the entire affected closure.

## Reproducible byte vectors and verification

The following pure Python snippet is a local format-generation check, not a daemon
codec, filesystem exercise, lab approval or protocol test. It has no file/network
I/O. It generates the exact complete 184-byte OPEN and 228-byte SEAL examples below
and WRITE argument/full candidate plus original-request vectors. The WRITE
candidate is an alternative to SEAL at sequence 2; do not concatenate both into
one history. It is an isolated schema fixture requiring a verified prior catalog
with its expected object/revisions; it is not a committed/recoverable history by
itself. UUID byte fixtures
are nonzero recognizable test values, not actual node/volume identities.

```python
import hashlib
import struct


def crc32c(data):
    crc = 0xffffffff
    for byte in data:
        crc ^= byte
        for _ in range(8):
            crc = (crc >> 1) ^ (0x82f63b78 if crc & 1 else 0)
    return crc ^ 0xffffffff


def frame(kind, seq, payload):
    header = struct.pack(
        '>8sHHHHIIII16s16s16sQQ16s16sqII',
        b'HFSJREC\0', 1, 0, 144, kind, 0, 148 + len(payload),
        len(payload), 0, b'\x11' * 16, b'\x22' * 16, b'\x33' * 16,
        7, seq, b'\x44' * 16, b'\x55' * 16, -1, 123456789, 0)
    assert len(header) == 144
    body = header + payload
    return body + struct.pack('>I', crc32c(body))


assert crc32c(b'123456789') == 0xe3069283
# OPEN: segment UUID, owner kind, absent reporter/incarnation, first=1,
# absent previous seal, 256 MiB maximum.
opened = frame(67, 1, b'\x66' * 16 + b'\x01\x00\x00'
               + struct.pack('>Q', 1) + b'\x00'
               + struct.pack('>Q', 268435456))
seal_payload = (b'\x66' * 16 + struct.pack('>QQQQ', 1, 1, 1, len(opened))
                + hashlib.sha256(opened).digest())
sealed = frame(68, 2, seal_payload)
# WRITE arguments: ObjectID(home=11..., allocation UUIDv4=77..47..87..), offset=2,
# blob length=3, exact bytes 00 ff 41.
allocation = bytes.fromhex('77777777777747778777777777777777')
write = b'\x11' * 16 + allocation + struct.pack('>QI', 2, 3) + b'\x00\xffA'
# Alternative uncommitted WRITE candidate in the same fixture identities.
# Existing regular object revision 3 -> 4; uid/gid1000, mode0644, size5,
# nlink1; all three metadata times exactly (-1,123456789).
object_id = b'\x11' * 16 + allocation
metadata = (struct.pack('>QBIIHQQ', 4, 1, 1000, 1000, 0o644, 5, 1)
            + struct.pack('>qI', -1, 123456789) * 3 + struct.pack('>I', 0))
selected = struct.pack('>I', 1) + object_id + struct.pack('>Q', 3) + metadata
redo = selected + struct.pack('>III', 0, 0, 0)
authorization = (object_id + struct.pack('>Q', 3) + b'\x88' * 16
                 + struct.pack('>QIIII', 1, 1000, 1000, 1, 1000))
dependency = b'\x11' * 16 + b'\x22' * 16 + b'\x33' * 16 + struct.pack('>QQ', 7, 1)
write_payload = (struct.pack('>H', 8) + write + selected + redo + struct.pack('>I', 1)
                 + authorization + struct.pack('>I', 1) + dependency)
write_frame = frame(2, 2, write_payload)
# Canonical LINK request; principal realm 11..., subject b'u'; anchor 22...,
# nonce 33..., number 1. Source volume/view 44.../55..., name b'a\xff';
# destination volume/view 66.../77..., name b'b'; NoReplace; no expectations.
def blob(value):
    return struct.pack('>I', len(value)) + value


original = (struct.pack('>H', 1) + b'\x11' * 16 + blob(b'u')
            + b'\x22' * 16 + b'\x33' * 16 + struct.pack('>Q', 1) + b'\x02'
            + b'\x44' * 16 + b'\x55' * 16 + struct.pack('>I', 1) + blob(b'a\xff')
            + b'\x66' * 16 + b'\x77' * 16 + struct.pack('>I', 1) + blob(b'b')
            + b'\x02' + b'\x00' * 5)
# Internal claim vectors intentionally reuse one RequestID. Different canonical
# requests at that ID must conflict, never adopt the other request's Redo.
request_id = (b'\x11' * 16 + blob(b'u') + b'\x22' * 16 + b'\x33' * 16
              + struct.pack('>Q', 1))
internal_prefix = struct.pack('>H', 1) + request_id + b'\x44' * 16 + b'\x55' * 16
parent_id = b'\x11' * 16 + bytes.fromhex('aaaaaaaaaaaa4aaa8aaaaaaaaaaaaaaa')


def meta(revision, kind, uid, mode, size, nlink):
    return (struct.pack('>QBIIHQQ', revision, kind, uid, 1000, mode, size, nlink)
            + struct.pack('>qI', -1, 123456789) * 3 + struct.pack('>I', 0))


def edit(object_value, expected, after):
    return object_value + struct.pack('>Q', expected) + after


def create_intent(mode):
    # umask0022, inherited uid/gid1000; exact selected parent/new metadata.
    args = parent_id + blob(b'new') + object_id + struct.pack('>BHH', 1, mode, 0o022)
    chosen = (struct.pack('>I', 2)
              + edit(object_id, 0, meta(1, 1, 1000, mode & ~0o022, 0, 1))
              + edit(parent_id, 2, meta(3, 2, 1000, 0o755, 0, 2)))
    return struct.pack('>H', 1) + args + chosen


def setattr_intent(uid):
    args = object_id + struct.pack('>HI', 2, uid)  # bit1 UID, explicit value
    chosen = struct.pack('>I', 1) + edit(object_id, 3, meta(4, 1, uid, 0o644, 5, 1))
    return struct.pack('>H', 10) + args + chosen


create_0600 = internal_prefix + create_intent(0o600)
create_0644 = internal_prefix + create_intent(0o644)
setattr_1000 = internal_prefix + setattr_intent(1000)
setattr_1001 = internal_prefix + setattr_intent(1001)
internal_domain = b'HOMELABFS-INTERNAL\0'
assert create_0600 != create_0644 and setattr_1000 != setattr_1001
assert hashlib.sha256(internal_domain + create_0600).digest() != hashlib.sha256(
    internal_domain + create_0644).digest()
assert hashlib.sha256(internal_domain + setattr_1000).digest() != hashlib.sha256(
    internal_domain + setattr_1001).digest()
# Even two requested modes producing the same 0644 after-image remain distinct.
assert create_intent(0o644) != create_intent(0o666)
# Negative Redo vectors: equal-length alternate metadata is not an allowed replay
# of the claim/binding. Compare the exact selected ObjectEdit subset, not only ID.
assert len(meta(1, 1, 1000, 0o600, 0, 1)) == 75
selected_create_0600 = create_intent(0o600)[-234:]
selected_create_0644 = create_intent(0o644)[-234:]
selected_setattr_1000 = setattr_intent(1000)[-119:]
selected_setattr_1001 = setattr_intent(1001)[-119:]
def metadata_agrees(selected_objects, local_redo_objects):
    # These fixtures have one home; the production rule selects that home's
    # ObjectEdit subset before this exact-byte comparison.
    return selected_objects == local_redo_objects


assert metadata_agrees(selected_create_0600, selected_create_0600)
assert not metadata_agrees(selected_create_0600, selected_create_0644)
assert metadata_agrees(selected_setattr_1000, selected_setattr_1000)
assert not metadata_agrees(selected_setattr_1000, selected_setattr_1001)
assert len(opened) == 184 and len(sealed) == 228 and len(write) == 47
for name, value in [('open', opened), ('seal', sealed), ('write_args', write),
                    ('write_candidate', write_frame), ('original_link', original),
                    ('internal_create_0600', create_0600),
                    ('internal_create_0644', create_0644),
                    ('internal_setattr_uid1000', setattr_1000),
                    ('internal_setattr_uid1001', setattr_1001)]:
    hashed = internal_domain + value if name.startswith('internal_') else value
    print(name, value.hex(), hashlib.sha256(hashed).hexdigest())
```

Internal vectors display the domain-prefixed request digest; other vectors
display SHA-256 of their exact bytes. For each internal pair, admitting the first
at this shared RequestID makes the second an ID_CONFLICT before effects. An
identical retry remains identical. A claimed 0600/UID1000 Intent with the alternate
0644/UID1001 Redo is rejected even if the enclosing CRC is recomputed; metadata
agreement assertions above represent these negative conformance pairs.

Golden output is printed below after the generation check. A future codec must
produce these exact bytes; no Go memory layout or JSON re-encoding is involved.

`open` (184 bytes), SHA-256 `34086338f38533b6d9ccf1fb9a63d3a5e8ae58f6e7975e9eb66db356ee71af54`:

```text
4846534a52454300000100000090004300000000000000b80000002400000000
1111111111111111111111111111111122222222222222222222222222222222
3333333333333333333333333333333300000000000000070000000000000001
4444444444444444444444444444444455555555555555555555555555555555
ffffffffffffffff075bcd150000000066666666666666666666666666666666
0100000000000000000001000000000010000000a767cf05
```

`seal` (228 bytes), SHA-256 `d36d3b3c12a92db25ac5aa726cf02ef48fdb95e1c2ce75044e86ff6bdbb8dfc5`:

```text
4846534a52454300000100000090004400000000000000e40000005000000000
1111111111111111111111111111111122222222222222222222222222222222
3333333333333333333333333333333300000000000000070000000000000002
4444444444444444444444444444444455555555555555555555555555555555
ffffffffffffffff075bcd150000000066666666666666666666666666666666
00000000000000010000000000000001000000000000000100000000000000b8
34086338f38533b6d9ccf1fb9a63d3a5e8ae58f6e7975e9eb66db356ee71af54
4a934d8e
```

`write_args` (47 bytes), SHA-256 `4a65d955f43e0e28259fe9754dda8cdbaba378b63a168c9d38d9d8092da9052a`:

```text
1111111111111111111111111111111177777777777747778777777777777777
00000000000000020000000300ff41
```

`write_candidate` (599 bytes), SHA-256 `ad305048c619b7bb8323e0b5fb2dbaca281532dbbe3adf92687ed0bb255d3204`:

```text
4846534a5245430000010000009000020000000000000257000001c300000000
1111111111111111111111111111111122222222222222222222222222222222
3333333333333333333333333333333300000000000000070000000000000002
4444444444444444444444444444444455555555555555555555555555555555
ffffffffffffffff075bcd150000000000081111111111111111111111111111
11117777777777774777877777777777777700000000000000020000000300ff
4100000001111111111111111111111111111111117777777777774777877777
77777777770000000000000003000000000000000401000003e8000003e801a4
00000000000000050000000000000001ffffffffffffffff075bcd15ffffffff
ffffffff075bcd15ffffffffffffffff075bcd15000000000000000111111111
1111111111111111111111117777777777774777877777777777777700000000
00000003000000000000000401000003e8000003e801a4000000000000000500
00000000000001ffffffffffffffff075bcd15ffffffffffffffff075bcd15ff
ffffffffffffff075bcd15000000000000000000000000000000000000000111
1111111111111111111111111111117777777777774777877777777777777700
0000000000000388888888888888888888888888888888000000000000000100
0003e8000003e800000001000003e80000000111111111111111111111111111
1111112222222222222222222222222222222233333333333333333333333333
333333000000000000000700000000000000017b043c9d
```

`original_link` (153 bytes), SHA-256 `93d01dd563af238c52b3a3aadfd52d3611ad939505f5fc6b275a7389f1832cc7`:

```text
0001111111111111111111111111111111110000000175222222222222222222
2222222222222233333333333333333333333333333333000000000000000102
4444444444444444444444444444444455555555555555555555555555555555
000000010000000261ff66666666666666666666666666666666777777777777
77777777777777777777000000010000000162020000000000
```

`internal_create_0600` (407 bytes), domain-prefixed request digest `151e3b8a14be12c37043564e35ec04e86f5ba4934cb29a2767882be62ad95985`:

```text
0001111111111111111111111111111111110000000175222222222222222222
2222222222222233333333333333333333333333333333000000000000000144
4444444444444444444444444444445555555555555555555555555555555500
0111111111111111111111111111111111aaaaaaaaaaaa4aaa8aaaaaaaaaaaaa
aa000000036e6577111111111111111111111111111111117777777777774777
8777777777777777010180001200000002111111111111111111111111111111
1177777777777747778777777777777777000000000000000000000000000000
0101000003e8000003e8018000000000000000000000000000000001ffffffff
ffffffff075bcd15ffffffffffffffff075bcd15ffffffffffffffff075bcd15
0000000011111111111111111111111111111111aaaaaaaaaaaa4aaa8aaaaaaa
aaaaaaaa0000000000000002000000000000000302000003e8000003e801ed00
000000000000000000000000000002ffffffffffffffff075bcd15ffffffffff
ffffff075bcd15ffffffffffffffff075bcd1500000000
```

`internal_create_0644` (407 bytes), domain-prefixed request digest `90ef217b2a173aae2703a1c2b24248ea56a7f5cdb242642e623b7c2db571d69e`:

```text
0001111111111111111111111111111111110000000175222222222222222222
2222222222222233333333333333333333333333333333000000000000000144
4444444444444444444444444444445555555555555555555555555555555500
0111111111111111111111111111111111aaaaaaaaaaaa4aaa8aaaaaaaaaaaaa
aa000000036e6577111111111111111111111111111111117777777777774777
87777777777777770101a4001200000002111111111111111111111111111111
1177777777777747778777777777777777000000000000000000000000000000
0101000003e8000003e801a400000000000000000000000000000001ffffffff
ffffffff075bcd15ffffffffffffffff075bcd15ffffffffffffffff075bcd15
0000000011111111111111111111111111111111aaaaaaaaaaaa4aaa8aaaaaaa
aaaaaaaa0000000000000002000000000000000302000003e8000003e801ed00
000000000000000000000000000002ffffffffffffffff075bcd15ffffffffff
ffffff075bcd15ffffffffffffffff075bcd1500000000
```

`internal_setattr_uid1000` (254 bytes), domain-prefixed request digest `0d201801c11837a3e9f7c2cfaf3d5806cef5715a2c90b5354a972e88cf7ee8f1`:

```text
0001111111111111111111111111111111110000000175222222222222222222
2222222222222233333333333333333333333333333333000000000000000144
4444444444444444444444444444445555555555555555555555555555555500
0a11111111111111111111111111111111777777777777477787777777777777
770002000003e800000001111111111111111111111111111111117777777777
77477787777777777777770000000000000003000000000000000401000003e8
000003e801a400000000000000050000000000000001ffffffffffffffff075b
cd15ffffffffffffffff075bcd15ffffffffffffffff075bcd1500000000
```

`internal_setattr_uid1001` (254 bytes), domain-prefixed request digest `59965fac92dc63ed8370df4d92b85ac11082af0d84fd477688f75945722ab282`:

```text
0001111111111111111111111111111111110000000175222222222222222222
2222222222222233333333333333333333333333333333000000000000000144
4444444444444444444444444444445555555555555555555555555555555500
0a11111111111111111111111111111111777777777777477787777777777777
770002000003e900000001111111111111111111111111111111117777777777
77477787777777777777770000000000000003000000000000000401000003e9
000003e801a400000000000000050000000000000001ffffffffffffffff075b
cd15ffffffffffffffff075bcd15ffffffffffffffff075bcd1500000000
```

A format validator must reject these negative cases before replay. These are
specified conformance cases, not claims that an implementation has passed them:

| Input/interleaving | Required check/result |
|---|---|
| Flip any one bit in covered header/payload or checksum | CRC mismatch (or bounded prefix rejection before allocation); do not replay. |
| Total 147, total >8 MiB, payload mismatch, count/blob limit+1, arithmetic overflow | Reject before allocation, including when attacker recomputes CRC. |
| Major/minor/kind/header length/flags/reserved unknown; trailing payload byte | Explicit unsupported/corrupt format, preserve bytes, no skipping. |
| Nanos = 1e9, mode outside 07777, size/offset sum >2^63-1, invalid name or bool | Reject valid-CRC semantic corruption. |
| Same internal ID changes CREATE mode/umask, SETATTR value/time source, or selected metadata; Redo disagrees with immutable metadata selection | Exact canonical retry comparison returns ID_CONFLICT; disagreeing Redo is semantic corruption even with a valid CRC. Never invent omitted values after claim/binding. |
| SESSION_CLOSE while any claim is unresolved or any claimed receipt is unretired | Reject closure; every claim must be terminal AND every receipt covered by the protected retirement floor. |
| Duplicate entry/object/group key, unsorted set, unequal duplicate position | Reject; identical network retransmission is the only duplicate-position exception. |
| Sequence 2 before 1; overlapping ranges, seal wrong hash/count, bytes after seal | Hold gap or quarantine corruption, no contiguous ACK/serving. |
| Cut final open frame at every byte; same truncation in a closed/interior frame | Preserve incomplete open tail for reconciliation; closed/interior case blocks recovery. |
| Missing prepare/home/claim/fence graph; graph cycle or evidence recursion | Block closure recovery/protection; never acknowledge only a digest. |
| Decision wrong digest/participant vector, stale term/incarnation, two outcomes | Quarantine/reject authority; never choose by timestamp. |
| Duplicate write/rename/ledger/install from C; old name now names another ID | Exact-ID ordered replay produces one result; revision conflict blocks, no path re-resolution. |
| Retire floor past pending claim; retry after retirement/checkpoint/source removal | Reject unsafe retirement; floor/tombstone/original result wins before paths. |
| CHECKPOINT_ACCEPT without published matching CURRENT/closure peer/dependency | Preserve predecessor and gate; RETENTION must not advance. |

Local review should check every mutation and every Transactions required record
against these tables and run the pure generation snippet. Future implementation
must add parser/fuzz/corruption/limit vectors and model delayed proposals racing
promises, majority acceptance/tail loss, closure visibility, partial checkpoint
publication and GC. Native kernel/FUSE, crash, storage sync and guest-cache tests
need separately explicit approval under [Test automation](../Test-automation.md).
