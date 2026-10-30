# Emergency-capable format 2.0

Status: mandatory future representation contract for `qw3`, 2026-10-28.
Unimplemented. [Emergency](Emergency.md) supplies the operational predicates;
this document fixes their representation and compatibility boundary. It grants
no activation, loss selection, installation, SSH, mount, VM or disruptive test
authority. Only disposable lab/canary recovery units and synthetic data are in
scope. `rr3` implements version foundations; `rw3` implements these codecs and
reader gates before `lr3` can implement activation. The shared `fsapi` v1 API,
normal codec, backing-store implementation and models are unchanged here.

## Versions, primitives and bounded objects

Select journal **2.0**, replication protocol **2.0**, emergency object **2.0**,
control **2.0**, and recovery manifest **2.0** as one mandatory capability,
`emergency-2` (numeric feature 1). Public and internal request canonical forms
also gain version 2. Existing v1.0 frames, all nine Journal-format goldens and
immutable creator identities remain byte-for-byte unchanged. A v2 reader has
separate strict v1 and v2 decoders; it never supplies v2 defaults to a v1 decoder.

All [Journal-format](Journal-format.md#byte-conventions-and-limits) integer,
UUID, boolean, array, optional, checked-arithmetic, exact consumption, SHA-256,
CRC32C, ordering and byte-preservation conventions apply. Its limits apply
unless reduced below. A required UUID is nonzero; a hash is not a sentinel.
Every binary schema below lists fields in byte order. There are no implicit
fields, extension maps, opaque authorization bytes or skippable tags. Only the
explicit inherited store markers/catalogs below use their versioned JSON schema.
Unknown versions, kinds, tags, features and nonzero reserved fields fail closed.

A standalone object is `Object = magic:bytes[8] (=ASCII HFS2OBJ then NUL) ||
major:u16 (=2) || minor:u16 (=0) || kind:u16 || reserved:u16 (=0) ||
payload_length:u32 || payload:<kind schema> || crc32c:u32`.
The 20-byte prefix and payload are covered by CRC32C; total length is
`24 + payload_length`, at most 4,194,304 bytes. `ObjectRef = kind:u16 ||
length:u32 || sha256:bytes[32]` identifies those complete immutable bytes.
Length is 24..4,194,304. Its kind must match the referenced object. Files are
private immutable content-addressed artifacts; no reference is a path or URL.
Resolve and validate their full typed contents before using them as evidence.

Set arrays sort by their complete encoded semantic key, ignoring hashes for key
equality where the key already identifies one immutable record/object. Unequal
bytes at one key are corruption. Unless specified otherwise, ObjectRef sets
sort by `(kind,sha256)`; RecordRef sets use the stream key below; volume/member/
closure vectors sort by UUID; grants sort by grant UUID. All sets are unique.
Caps: 64 volumes per closure, 64 closures and 4096 nodes/edges/objects per graph,
4096 refs per list, 64 namespaces, 3 admitted storage members (fixed native
profile), 4096 session entries per bounded checkpoint shard, and at most 64
checkpoint shards per closure. A complete resolved evidence graph is at most
64 MiB of frame/object bytes, 4096 vertices, and depth 128. Payload bytes counted
in an external baseline/archive are separately bounded by the admitted Plan
budget, with at most 4096 files each at most 1 GiB. Exceeding a bound requires
rejecting admission or a new reviewed format, never evidence omission. Limits
are checked before allocation and cumulatively; unknown graph size blocks use.

## Tagged references and authority

Use these types throughout v2; their tag bytes are part of equality and hashes:

```text
RecordRef = encoding:u8 || protection:u8 || frame:FrameRefV1
  encoding: 1 exact v1.0 frame, 2 exact v2.0 frame
  protection: 1 NORMAL, 2 EMERGENCY
  encoding=1 requires protection=1
AuthorityRef = tag:u8 || body:<tag schema>
  tag=1: NormalAuthorityV1
  tag=2: volume:uuid || ObjectRef(kind=1 EmergencyAuthority)
Namespace = registry:uuid || serial:u64
RequestID2 = tag:u8 || body:<tag schema>
  tag=1: RequestIDV1
  tag=2: cluster:uuid || namespace:Namespace || id:RequestIDV1
```

`FrameRefV1`, `NormalAuthorityV1` and `RequestIDV1` are the exact shared schemas
from [Journal-format](Journal-format.md#shared-schemas), without modification.
Normal owner and partner remain distinct admitted members, with their real
incarnations; a normal authority never means one available node. Namespace serial
starts at 1 and never wraps. `RecordRef` retains the complete original stream
identity, reporter/creator incarnation, position and complete-frame hash. Sort
its key by `(encoding,stream_kind,reporter,reporter_incarnation,Position)`; a
changed protection tag at one key is corruption, not a second record. Validate
protection against the decoded target's envelope and authorized history.
Imported v1 normal frames retain `(encoding=1, protection=1)`; imported
normal v2 frames retain `(encoding=2, protection=1)`, including a later
emergency after normal v2 exit. Old emergency frames retain `(2,2)` across
normal exit and later emergencies; importing never retags them NORMAL. Both
authority tags select one explicit volume (the first field of normal authority
or the emergency tag body); authority vectors sort uniquely by that volume.
The selected emergency volume must occur in the referenced closure/history
vector; frame envelope authority volume must equal its header volume.

Standalone kind 1, `EmergencyAuthority`, has payload:

```text
cluster:uuid || run:uuid || generation:uuid || transition:uuid ||
designated:uuid || incarnation:uuid || launch:ObjectRef(8) ||
closure:ClosureKey || histories:Vector || members:array<uuid> ||
predecessors:array<PredecessorAuthority> || control:ObjectRef(4) || plan:ObjectRef(2) ||
prospective:array<ProspectiveRef>
```

`PredecessorAuthority = volume:uuid || authority:AuthorityRef`; predecessor
entries sort by volume, exactly one per volume. ClosureKey is defined below;
its version and member vector avoid a hash cycle with the later AFFINITY
record that references this authority. Its histories contain exactly the closure's volumes and new emergency histories;
epochs are nonzero and ordered within each linked lineage, not across UUIDs.
Members remain the original three; designated is one of them. Predecessors resolve to each volume's exact old authority
entry; their complete immutable histories are retained. Every closure authority
in one serving vector has the same cluster, generation, transition, designated,
incarnation, host control intent and plan. No partner field exists. `control`
is the earlier singleton INTENT, not an ACTIVE record that depends on this
object. `prospective` selects exactly the earlier Plan successor descriptors for this
closure, as defined below; all descriptor fields must match this object.
`launch` binds the actual new incarnation; restart creates a new authority
and higher control revision after reconciliation, never edits this object.

An AuthorityRef is not independently executable: verify current independent
control, whole-closure selection/publication and guard coverage. A generation,
local signature, timestamp, largest epoch or restored control pointer alone
cannot allocate designation or establish history protection.

## Journal 2.0: complete derivation and local proofs

The 144-byte v1 header, lengths, CRC, sequence rules, segment OPEN/SEAL ordering,
rotation and torn/interior-corruption rules stay exact, except major=2, minor=0.
The header's configuration identifies the installed v2 history, and its sender
incarnation remains the immutable creator. All nonstructural frames in one owner stream have the same mode, determined
by the installed history/configuration; mode change creates a fresh authorized
configuration and stream. New segment paths include `/v2/`
between `segments/` and the volume UUID, preventing append to an old stream.
A segment contains one version only; old segments remain at their old paths.

Every nonstructural v2 payload is `mode:u8 || authority:AuthorityRef ||
body:<record schema below>`. Mode 1 requires a normal authority; mode 2 requires
EmergencyAuthority. The authority must cover the header volume/history and
sender role. Structural kinds 67/68 have no prefix; OPEN/SEAL bodies use the
v1 schemas with RecordRef in place of FrameRef. Their previous-seal reference
must select the same version and stream mode; a new version starts a new stream
with no previous seal and an explicit migration root. Structural frames never
establish commitment.

The following **recursive type transformation T** defines each inherited v2
body completely: take the exact v1 body/field order and enum/semantic rules;
replace every `Authority` by `AuthorityRef`, `FrameRef` by `RecordRef`, and
`RequestID` by `RequestID2`, and `RequestDocument` by `RequestDocument2`
everywhere (including BEGIN), including occurrences inside Binding, DecisionID,
DecisionRef, PrepareRef, TerminalResult, Tail, Coverage, Visibility dependencies,
optionals and arrays. Other leaf encodings, Intent/Mutation/Redo, positions and
field ordering remain exact. T(Artifact) retains its exact v1 fields and kinds
1..7 and adds kind 8 (Object kind14 Retention), 9 (Object kind10 Checkpoint),
10 (Object kind11 Archive), 11 (Object kind20 Migration), 12 (Object kind17
ExitCommitted). The Artifact UUID
must equal the typed object's retention/checkpoint/archive/migration/exit UUID; its
length/hash cover that exact complete Object. Every newly required authority
or retention Artifact validates this typed schema, never an opaque blob. `Evidence.frames` still contains bounded exact
complete frames, now individually version-discriminated; no embedded record
that has an Evidence field may itself appear there. Old v1 frames nested in
Evidence are decoded as v1 without T. A v2 reference to one is tagged 1/NORMAL.
T never transforms existing bytes, binding, coordinator, ID or outcome. For a
v2 AFFINITY, AffinityRef.manifest_sha256 hashes its exact T(body), excluding
the per-volume mode/authority envelope, so every member records the same
manifest hash. V1 AffinityRef hashes stay unchanged; resolve the referenced
manifest version/history explicitly, never infer its encoding from the hash.

Inherited kind set is exactly `1..9,16..22,32..38,48..55,64..68` where only the
listed kinds in Journal-format exist (10..15,23..31,39..47,56..63 are invalid).
For kind65 replace checkpoint_accepts by `array<CheckpointAcceptance>` defined
below; this is an explicit exception to T. For kind 17 use the RequestDocument2 definition below rather than recursively
changing bytes within an original v1 document. Kind 16/20/21 prepend
`identity:RequestID2` to T(body), with number=1 as the session-key exemplar;
principal/anchor/nonce in body must equal its inner V1 identity. This avoids
untyped namespace-less registration/floor/close records. Kind 22 retains its
retired anchor semantics and additionally bars every typed namespace using it.
Kind 54 is not executable loss authority in v2: import old evidence only;
new loss selection uses the objects below. Kinds 3 COMMIT, 36 DECISION and
48..51 PROMISE/ACCEPT/TAIL_REPORT/RECOVERY_SELECT require mode NORMAL and keep
the full normal protection/majority predicate. In particular ACCEPT for an
EMERGENCY candidate or reporter=owner is invalid. Normal T(body) authorities
must all be NORMAL except CONFIGURATION predecessors at the explicit exit; old emergency dependencies may be referenced, not promoted
to a normal ACCEPT. Normal v2 cannot weaken any v1 commitment rule. An exit CONFIGURATION
(kind53) may have EMERGENCY predecessors only when its authorization is Artifact
kind12, validating the exact ExitCommitted and successor vector; emergency
predecessors never replace normal owner/partner or majority requirements. Other
normal authorization Artifacts retain the separately strict `rr3` allocation
codec/verifier gate; an opaque kind6 artifact cannot authorize emergency entry,
exit, loss, designation or grant.

The following new kinds have mode EMERGENCY only, except kind 133 as stated:

| Kind | Name | Body in exact field order |
|---:|---|---|
| 128 | E_COMMIT | `mutation:RecordRef, dependencies:Vector, selected:ObjectRef(3)` |
| 129 | E_DECISION | `id:T(DecisionID), digest:bytes[32], begin:RecordRef, outcome:u8, prepares:array<T(PrepareRef)>, result:T(TerminalResult), selected:ObjectRef(3)` |
| 130 | E_PROTECT | `target:RecordRef, prefix:Vector, dependencies:array<RecordRef>, objects:array<ObjectRef>, predecessor:optional<RecordRef>` |
| 131 | E_IMPORT | `original:RecordRef, selection:ObjectRef(3), dependencies:array<RecordRef>` |
| 132 | E_CHECKPOINT | `checkpoint:ObjectRef(10), dependencies:array<RecordRef>, predecessor:optional<RecordRef>` |
| 133 | MODE_INSTALL | `mode:u8 (=1 NORMAL_REQUIRED), exit:ObjectRef(16), authority_vector:array<AuthorityRef>, dependencies:array<RecordRef>` |

128 requires an exact mode-E kind-2 MUTATION in the same history/operation and
verified kind-3 Selection; it is a new emergency candidate only, never an old
normal COMMIT. 129 uses DECISION's exact provisional-result exceptions and
outcome enums; its request/binding/coordinator/term/prepares are immutable, every
participant prepare is complete and protected before proposing COMMIT, and it
cannot change an old or new chosen outcome. An old bound normal request stays
bound to its old DecisionID and outcome: recover it through its real normal
selection evidence, or leave it indeterminate/excluded by explicit loss. Never
mint E_DECISION for the old slot or claim that the old candidate became chosen
by survivor sync. 131 retains an independently verified old normal protected
outcome/selection and its graph; it supplies local durable recovery coverage,
not the missing old ACCEPT or absence proof. Missing old normal proof blocks
non-loss import. Original bytes and bound coordinators/results never change.

All mode-E inherited state records (including SESSION/CLAIM/BIND, PREPARED,
INSTALL, COMPLETE, ordinary/anchor results, barriers, pins and retention) require
a subsequent E_PROTECT. Kind 9 commit points to E_COMMIT for a new emergency operation, with its
E_PROTECT in dependencies. For a recovered normal operation it points to the
original NORMAL COMMIT; resolution points to E_IMPORT and dependencies retain
that import/proof graph, as specified below. Kind 19 terminal result requires its exact protected claim/binding,
chosen decision, all protected installs, COMPLETE and verified shared visibility;
rejection still requires protected anchor claim/result. INSTALL's acceptance
list contains matching E_PROTECT/E_IMPORT proofs, never fake ACCEPT. COMPLETE
retains every bound participant, exact decision and cache barrier. The anchor's
terminal result is protected before reply. Safe ABORT of an emergency request
is available only under the same sole writer/term and proven no prior competing
chosen COMMIT; uncertain old normal slots do not gain that absence proof.

E_PROTECT is an assertion checked against **actual durable local publication**,
not an ACK that can establish its own truth. Its target must be an earlier
mode-E owner frame other than E_PROTECT, E_IMPORT or structural frames. The
complete target bytes, contiguous prefixes, all transitive record/object/payload
bases and predecessor proof must be synced with their names/directories on the
designated node in the named incarnation before E_PROTECT is appended/synced.
The proof itself and its segment/directory are synced before protected progress,
apply authorization or response. E_IMPORT likewise requires actual local sync
of original plus graph and its own record; it cannot protect another import.
A remote transfer of a proof is insufficient until the receiver independently
verifies and syncs the graph. `prefix` has one position per affected volume;
older streams are retained explicitly in dependencies, never collapsed by max.

Resolve dependencies before constructing a proof. Logical progress is:
`selection/control/authority -> SESSION/CLAIM -> BIND/BEGIN -> PREPARED ->
E_DECISION -> INSTALL -> COMPLETE -> REQUEST_RESULT`, with E_PROTECT after
each corresponding target and before its consumers. Ordinary progress is
`MUTATION -> E_COMMIT -> ORDINARY_RESULT`, with the same intervening proofs.
E_PROTECT never references a consumer, itself, or another proof of itself.
Candidates do not reference their later proofs. INSTALL cannot be a dependency
of DECISION; the provisional result has no self DecisionRef/visibility. Node-
local E_PROTECTs can refer to other participants' earlier protected prepares
because all are on the same designated node. Traversal rejects every cycle,
missing vertex/hash, depth/byte excess and same-key conflict, including cycles
through objects or checkpoint replacements. Explicit prefix coverage is expanded
to exact retained frame vertices for validation. No phase label or hash-only
receipt bypasses this graph or actual sync.

## Exact comparison of retained normal bindings and results

There are two distinct uses of v2 fields. A newly admitted emergency request
has a tag-2 RequestID2, RequestDocument2 class 1/2, and a new immutable binding
whose authorities are all EMERGENCY in the installed common designated vector.
Recovery of an **already chosen normal** request retains its old normal binding,
coordinator, slot and result. Its v2 recovery envelope uses current emergency
authority to authorize local installation, never to choose the old candidate.
The binding inside that envelope remains NORMAL. No new REQUEST_BIND, BEGIN,
PREPARED or E_DECISION may be minted for that old slot in emergency mode.

Define the exact lossless **LegacyLift** from a strictly decoded v1 value to a
v2 comparison view: replace RequestIDV1 by `u8(1) || original RequestIDV1`,
NormalAuthorityV1 by `u8(1) || original NormalAuthorityV1`, FrameRefV1 by
`u8(1) || u8(1) || original FrameRefV1`, and RequestDocumentV1 by
`u8(3) || blob<exact original RequestDocumentV1>`. Recursively apply those four
rules to Binding, DecisionID, DecisionRef, PrepareRef and TerminalResult in
original field order; every other scalar, Intent/Redo byte, count, optional
presence, digest and array order is copied unchanged. LegacyUnlift is defined
only when each replacement has those exact tags, each inner source validates
as v1 and the full comparison value consumes exactly. It removes only those
specified wrapper bytes; then strict v1 decode must succeed. Anything else is
not legacy and fails this comparison. Thus
`LegacyUnlift(LegacyLift(original)) == original` byte-for-byte. Lift is a view
in the new record, never an edit/re-hash of a retained original record.

For original v1 normal evidence, compare a recovery INSTALL.binding exactly to
LegacyLift of the source REQUEST_BIND.binding; compare its PrepareRef and
DecisionID exactly to their lifted originals. Its decision frame is the original
NORMAL kind36 DECISION ref `(1,1)`, not the E_IMPORT or a new E_DECISION. The
optional prepare is the lifted original kind35 PREPARED ref, and Redo is exactly
the original local Redo bytes. For original **v2** normal evidence, use its
already typed Binding/DecisionID/PrepareRef/Redo bytes verbatim, with its original
NORMAL `(2,1)` refs; do not add another wrapper or run LegacyLift over it. These
rules distinguish source encoding by RecordRef.encoding, never by a heuristic
on payload bytes. A v2 normal binding that itself uses a legacy tag-1 ID keeps
that already stored representation and original canonical-document source.

Recovered INSTALL (kind37) and newly necessary COMPLETE (kind38), REQUEST_RESULT
(kind19) or ORDINARY_RESULT (kind9) are mode-E records in current emergency
streams, followed by E_PROTECT. Their causal fields keep original claim (kind17),
bind (kind18), prepare (kind35) and decision (kind36) refs with their original
NORMAL encoding/protection tags; new install/complete refs are `(2,2)`.
INSTALL.acceptance must include an earlier E_IMPORT root for the exact normal
COMMIT/DECISION, or a normal RECOVERY_SELECT resolving that same candidate, and
its full actual normal selection/acceptance dependencies. REQUEST_RESULT.binding
is the original bind frame, never a replacement binding. A recovered ordinary
result's mutation/commit point to original normal kind2/kind3 respectively;
its resolution is the earlier E_IMPORT. E_IMPORT may also preserve an original
normal kind9/19/38 completed result with its complete chosen-outcome graph;
its identity and result are checked by the same source-version rule. A lone
prepare, claim, candidate sync or higher emergency authority is insufficient
for every one of these paths. Missing true normal proof remains indeterminate.

If the original terminal result/COMPLETE exists, preserve that exact result and
all its immutable committed positions and visibility; a v2 field is its exact
LegacyLift (v1 source) or original typed bytes (v2 source). Prefer E_IMPORT of
the retained completed record; no duplicate completion may select newer positions
or visibility for that ID. Recover missing original dependencies from exact
retained replacements, else stay gated. If complete verified normal history
proves the chosen decision had no constraining COMPLETE/terminal result, recovery
may create its **first** completion: status/code/source/replacement/conflicts
must equal the source decision's lifted/original provisional result, decision
is that source DecisionRef, and committed/visibility fields are populated exactly
from the new complete install vector/shared barrier under the kind38 rules.
They become immutable when first protected. Uncertain missing old completion
history cannot establish that absence or permit another result. ABORT keeps
its original outcome and unchanged-visibility rule. Copies of old results are
historical facts; current serving/publication requires its separate current
activation/barrier graph. Neither new envelope authority nor LegacyLift changes
the original digest, canonical command, chosen value or protection predicate.

## Singleton control, plans, approvals and admission

The initial control authority is one enrolled host-local registry outside VM
and per-volume rollback. It serializes every operation and guarded launch for
one cluster. A second registry copy/runner, restored snapshot, guest local file
or etcd revision cannot allocate a parallel designation. Enrollment pins registry
UUID, host identity, verifier key and storage identity; replacement after lost
continuity is unsupported by this format and requires an independently reviewed
reconstruction. No missing-file initialization is allowed on boot.

The following shared exact types apply to standalone objects:

```text
History = volume:uuid || predecessors:array<Position> || selected:Position ||
          suffix:array<RecordRef> || baseline:optional<ObjectRef>
ClosureKey = closure:uuid || version:u64 || volumes:array<uuid>
NodeLaunch = node:uuid || incarnation:uuid || launch:uuid
Budget = total_bytes:u64 || per_volume:array<VolumeBudget> ||
         reserve_bytes:u64 || queue_bytes:u64 || graph_bytes:u64 ||
         archive_bytes:u64 || staging_bytes:u64 || deadline:Time
VolumeBudget = volume:uuid || ceiling:u64 || stop_at:u64 || reserve:u64
Scope = closures:array<ClosureKey> || unavailable:array<uuid>
Coordination = lineage:uuid || predecessors:array<uuid> ||
               configuration_sha256:bytes[32] || promises:array<RecordRef>
ProspectiveRef = index:u16 || descriptor_sha256:bytes[32]
EmergencySuccessor = volume:uuid || cluster:uuid || run:uuid || generation:uuid ||
                     transition:uuid || designated:uuid || incarnation:uuid ||
                     launch:uuid || closure:ClosureKey || histories:Vector ||
                     members:array<uuid> || predecessors:array<PredecessorAuthority>
SuccessorDescriptor = tag:u8 || body:<tag schema>
  tag=1: NormalAuthorityV1
  tag=2: EmergencySuccessor
ViewDomain = launch:NodeLaunch || serving_generations:array<uuid> || views:array<uuid>
File = role:u8 || home:uuid || id:uuid || length:u64 || sha256:bytes[32]
  role: 1 catalog,2 payload base,3 exact frame segment,4 semantic replacement,
        5 snapshot manifest,6 encrypted recovery key envelope
```

SuccessorDescriptor sets sort by their explicit volume and are unique per
volume. ProspectiveRef.index is the zero-based index into the exact Plan's
successors array; descriptor_sha256 is SHA-256 of that exact canonical tagged
SuccessorDescriptor. ProspectiveRef sets sort by index, without duplicates.
EmergencySuccessor contains only proposed values and **earlier** predecessor
refs: no current EmergencyAuthority hash, Plan/control hash, launch attestation,
future FENCE or successor self-reference. It is not serving authority. Its
predecessors equal the corresponding closure subset of Plan.fence_predecessors.
A later EmergencyAuthority must exactly match each selected descriptor's cluster,
run/generation/transition/designated/incarnation, launch UUID, closure/histories,
members and predecessors. A changed actual launch/incarnation requires a new
reviewed Plan/descriptor and appropriate approval; preserve prior guards.
ViewDomain entries sort by launch node; they cover every old launch and its
complete workload/cache domain. Plan old_launches is the exact native graceful-
stop target vector under the pinned enrollment digest; missing domain/target
identity blocks the plan. Histories sort by volume; predecessor positions sort by complete Position,
retaining distinct histories. Suffix is ordered per full stream key and then
sequence, without gaps within each selected suffix. Every external baseline
ObjectRef has kind 10/11/12 (checkpoint/archive/snapshot), recursively validated.
Files sort by `(role,home,id)`; catalog home is its volume and id the
checkpoint UUID, payload home is its ObjectID home and id its allocation UUID,
segment home is its volume and id its segment UUID. Shared manifest/key-envelope
files use cluster UUID as home and their immutable artifact UUID as id.
 their lookup is private content-addressed storage.
A key envelope alone is not a key-availability proof. Budget arrays cover every
scope volume; stop_at+reserve <= ceiling, all charges/reserves fit total_bytes,
counts/bytes never overflow and zero never means unlimited. Deadline is an
execution bound, not authority expiry that fences cached access. The canary
thresholds and protected reserves in Emergency remain additional admission caps.

| Object kind | Name | Payload in exact field order |
|---:|---|---|
| 2 | Plan | `cluster:uuid, run:uuid, transition:uuid, action:u8, expected_control:ObjectRef(4), generation:uuid, designated:uuid, scope:Scope, predecessors:array<History>, members:array<uuid>, old_launches:array<NodeLaunch>, old_domains:array<ViewDomain>, policy_sha256:bytes[32], enrollment_sha256:bytes[32], verifier_sha256:bytes[32], proposed_launches:array<NodeLaunch>, fence_predecessors:array<PredecessorAuthority>, successors:array<SuccessorDescriptor>, selected:array<Position>, loss:u8, excluded:array<Position>, unknown_outcomes:u8, peer:optional<NodeLaunch>, coordination:Coordination, budget:Budget, preservation:array<File>, required_features:u64 (=1)` |
| 3 | Selection | `plan:ObjectRef(2), control:ObjectRef(4), closure:ClosureKey, histories:array<History>, normal_proofs:array<RecordRef>, affinity:array<RecordRef>, prepares:array<RecordRef>, anchors:array<RecordRef>, homes:array<uuid>, fences:array<ObjectRef(8)>, loss:optional<ObjectRef(7)>, dependencies:array<RecordRef>` |
| 4 | Control | `registry:uuid, cluster:uuid, run:uuid, revision:u64, previous:optional<ObjectRef(4)>, transition:uuid, generation:uuid, designated:uuid, current:optional<NodeLaunch>, phase:u8, plan:optional<ObjectRef(2)>, approval:optional<ObjectRef(5)>, namespace_serial:u64, namespace_bars:array<Namespace>, deny_v1:u8, grants:array<ObjectRef(6)>, revocations:array<ObjectRef(9)>, scope:Scope, evidence:array<ObjectRef>, coordination:Coordination, normal_required:array<RecordRef>, exit:optional<ObjectRef(17)>, budget:Budget` |
| 5 | Approval | `registry:uuid, cluster:uuid, transition:uuid, plan:ObjectRef(2), action:u8, approver:Principal, admin_host:uuid, admin_boot:uuid, uid:u32, connection:uuid, policy_sha256:bytes[32], nonce:uuid, not_before:Time, not_after:Time, audit_ordinal:u64, key_id:uuid, signature:bytes[64]` |
| 6 | Grant | `registry:uuid, cluster:uuid, generation:uuid, grant:uuid, predecessor:ObjectRef(4), plan:ObjectRef(2), approval:ObjectRef(5), role:u8, launch:NodeLaunch, scope:Scope, source:uuid, capabilities:u64, session_revision:u64, staging:uuid, transfer_pins:array<RecordRef>, budget:Budget` |
| 7 | LossIdentity | `registry:uuid, cluster:uuid, transition:uuid, previous:ObjectRef(4), plan:ObjectRef(2), approval:ObjectRef(5), namespace:Namespace, reject_v1:u8 (=1), excluded_namespaces:array<Namespace>, baseline:array<ObjectRef>, excluded:array<Position>, unknown_outcomes:u8 (=1), preserved:array<File>` |
| 8 | LaunchEvidence | `registry:uuid, cluster:uuid, transition:uuid, plan:ObjectRef(2), policy_sha256:bytes[32], node:NodeLaunch, old_launches:array<NodeLaunch>, serving_generations:array<uuid>, views:array<uuid>, guard_revision:u64, guard_sha256:bytes[32], fence:array<RecordRef>, mechanism:uuid, request_major:u16, request_minor:u16, coverage_form:u8, successor_refs:array<ProspectiveRef>, verifier_version:u16, attestation:ArtifactV1` |
| 9 | Revocation | `registry:uuid, cluster:uuid, generation:uuid, grant:ObjectRef(6), predecessor:ObjectRef(4), plan:ObjectRef(2), approval:ObjectRef(5), session_revision:u64, reason:u8, excluded_launch:NodeLaunch, evidence:ObjectRef(8)` |

Action is 1 activate, 2 activate-with-loss, 3 admit, 4 revoke, 5 exit, 6 resume,
7 supersede-gated-attempt. Plan loss=1 iff action=2; all others require loss=0
and no excluded cut. Restart/resume cannot hide a loss/designation change.
`unknown_outcomes` is 0/1; loss=1 requires 1 even if the known-ID list is empty.
`Scope` includes complete closure keys plus volumes deliberately unavailable;
sets are disjoint. Discovery uncertainty gates all possibly dependent members.
Plan files enumerate preserved original branches including contaminated payloads;
they are never silently rewritten. Future exact fence/launch execution is only
`native-vm-stop-v1` as specified in Takeover, with its exact enrolled targets,
positive exclusion verifier and bounds. Kind 8 requires that policy's pinned
strict artifact codec/verifier from `lv3`, current guard continuity and full
coverage. An unimplemented/unknown verifier blocks use. A raw Artifact hash,
PID absence, signature alone or diagnostic timestamp is insufficient. These
schemas add no force/host-power/Rocky/production action or stop permission.

The native fence's ordinary **flat normal authority** request/attestation form
is unchanged: LaunchEvidence coverage_form=1, request_major/minor=1/0 and empty
successor_refs select that existing strict codec. It cannot encode an emergency
successor. For pre-authority emergency exclusion select coverage_form=2,
request_major/minor=2/0 and verifier_version=2, with successor_refs selecting the exact reviewed Plan
successors and its fence_predecessors. The pinned strict native request and
signed attestation codec version 2.0 must bind that Plan ObjectRef, these exact
prospective indexes/hashes (including the complete descriptor/predecessor bytes
resolved from that Plan), and all existing target/old-domain/guard/nonce facts.
Unknown/unimplemented form/version/verifier blocks use. This is an encoding
adaptation to `native-vm-stop-v1`, not a new fence mechanism or action: exact
allowlist/targets, graceful ACPI-only operation, 120-second total/10-second RPC/
three-attempt bounds, supervised exit/no-domain proof and continuing guard
requirements stay unchanged. No automatic force/host/Rocky/production operation
is added. Codec installation and actual disruptive windows remain separately
approved. A normal successor during emergency exit uses a tag-1 flat normal
SuccessorDescriptor inside form 2, retaining its real distinct owner/partner;
this does not alter form 1 or require an EmergencyAuthority hash in a descriptor.

LaunchEvidence.fence is supplemental retained history only: each ref must name
kind52 FENCE already in the exact verified pre-Plan predecessor graph, with its
original NORMAL/EMERGENCY tag and source encoding. It must not depend, directly
or transitively, on this Plan's proposed successor authority/LaunchEvidence or
any later mode-E FENCE; empty is permitted. These earlier facts never substitute
for the new form-2 attestation's exact prospective coverage and continuing guard
verification. New mode-E FENCE copies may be written **after** EmergencyAuthority
exists, referencing that earlier LaunchEvidence; they are consumers, never
inputs to it. Construction order is therefore:
`old evidence/control -> Plan(descriptors) -> Approval -> INTENT -> native
request/attestation -> LaunchEvidence -> EmergencyAuthority -> new mode-E FENCE
-> Selection -> ACTIVE`. Descriptor hash validation is over its bytes, never
over the later authority/attestation. Resolve all implicit attestation coverage
edges too; a forward edge or cycle blocks construction even with a valid CRC
or signature. No fence proof needs to hash its own downstream authority.

Control phase is 0 ENROLLED, 1 INTENT, 2 EXCLUDING, 3 SELECTING, 4 LOCAL_DURABLE,
5 ACTIVATING, 6 EMERGENCY_ACTIVE, 7 EXIT_INTENT, 8 PAIR_DURABLE,
9 NORMAL_REQUIRED, 10 EXIT_COMMITTED, 11 GATED. Revision starts at 1 and increases
by exactly 1 with an exact previous ObjectRef CAS. ENROLLED has no previous,
plan, approval, launch or exit; generation is the enrolled normal generation,
designated is the enrolled administrative target, scope is complete admitted
inventory and all serving rights still require normal authority. No later record
may look like ENROLLED. Non-genesis records require plan/approval; action/scope
must match. Evidence kinds permitted here are 1,3,7,8,10..16,18..20 only. No control
record references an authority/selection that in turn points to that same record:
INTENT references only earlier control and pre-existing plan/approval; guards
are bound by later evidence;
authority/selection link INTENT; ACTIVE links their later evidence. EXIT_COMMITTED
references an earlier kind-17 exit object, never a self-dependent object. Phase
advancement alone cannot open gates: validate all Emergency predicates and the
complete scope vector. GATED preserves the attempt, bars, grants, obligations
and highest mode; it cannot lower NORMAL_REQUIRED or reset namespace serial.

CAS and namespace allocation run under the same host lock as all guarded launch
paths. Compare registry/cluster/run, complete previous digest/revision, generation,
transition and approved plan; sync immutable record and all referenced evidence,
sync an exclusive temporary CURRENT, atomically replace CURRENT and sync parent
before acknowledging. Retain predecessors and external evidence copy. An
ambiguous result is reconciled against the exact candidate, never retried as a
new transition. Conflicting concurrent plans lose CAS and remain gated. Control
continuity is checked against enrolled host binding and the typed external
RegistryFrontier below before any start; an old locally valid snapshot cannot lower it.
Loss of that frontier/registry continuity blocks all fresh authority, including
new namespace allocation. f2 received replicas are evidence only.

Approval is produced by the **trusted independent host broker administrative
socket**, restricted to the pre-enrolled recovery-admin OS UIDs. Its separate
status privilege cannot approve; a replica or restricted runner credential
cannot approve. The broker obtains OS peer credentials itself and maps them to
the pinned Principal/policy, verifies the exact displayed Plan and action/window,
records unique nonce and monotonic audit ordinal, syncs the external audit, and
signs with the already pinned broker Ed25519 key. Signing input is ASCII
`HFS-APPROVAL-2` then NUL then Object's 20-byte prefix and Approval payload
through key_id (signature excluded); CRC includes signature. Verify signature,
principal/policy privilege, exact plan/action/transition, unused-or-identical
nonce and execution window against independently maintained audit authority.
The signed receipt is the broker's authenticated authorization record, not a
client claim that possession of a plan file grants privilege. Clock uncertainty
prevents a new effect; completed operations remain queryable. Identical retries
resume the same durable operation and cannot expand or renew its window. An
expired disruptive window requires fresh concrete approval without releasing
guards. Broker/admin installation remains a future approved action.

Grant roles are 1 designated-serving and 2 recovery-only. Capabilities are bits
0 recovery receive, 1 recovery report, 2 workload serve; other bits invalid.
Role 2 requires exactly bits 0/1 and never workload/ordinary ACCEPT or normal
replication eligibility. Role 1 requires bit 2 and the singleton's exact current
designated launch plus fully activated closure authority before serving. Only
one role-1 launch may be current across the entire cluster. One role-2 peer at
a time is allowed initially; a different peer or restart requires explicit fresh
grant/CAS, incarnation/staging and catch-up reconciliation. Revocation reasons
1 administrator,2 restart,3 superseded; revision increases, old sessions revoke
before new sessions authorize data. Uncertain termination keeps the old launch
barred and gates any conflicting publication. Disconnect/time expiry alone does
not create a fence. Revocations/tombstones never expire or disappear at exit.

## Loss identities, registration and routing before paths

Choose a **typed new identity namespace**, rather than claiming to enumerate
unknown old IDs. On each loss activation the singleton atomically increments its
permanent namespace_serial and publishes LossIdentity with
`Namespace(registry UUID, new serial)`. It permanently sets deny_v1=1 and bars
**all** earlier namespace serials, whether their requests are known or unknown.
Bars are encoded as the complete previously allocated Namespace set (cap 64;
exhaustion blocks another loss). The serial frontier and all bars survive exit,
checkpoint, snapshot, archive, closed session and anchor retirement. A later
loss never reuses a serial. Registry continuity loss, missing frontier/bar or
serial exhaustion makes loss activation unsupported. A new random registry
UUID cannot bypass that condition. ENROLLED has namespace_serial=0 and no allocated namespaces. The initial
upgrade allocates serial 1 through an ENROLLED -> INTENT CAS; a non-loss upgrade
requires complete old identity authority, while an approved initial loss may
bar all tag-1 IDs without enumerating them. Each later loss allocates the next
serial. Historical v1 IDs have no namespace and are permanently
excluded as a whole after any loss, not just a discovered list.

RequestID2 tag 2 is disjoint from tag 1 by definition of typed identity, even
when Principal, anchor, nonce and number are byte-identical. The namespace is
server-issued from current independent control, never a client-selected UUID
or implicit generation field. Authentication binds the same Principal; a new
namespace grants no new Unix privilege. RegisterSession2 accepts version=2,
namespace, anchor and caller nonce, validates namespace/bar and authoritative
anchor routing before lookup or effects, and protects the tag-2 SESSION. The
response includes the full typed session identity. Repeating a nonce within
that namespace returns the same session or its permanent closed tombstone.
The absence of a claim never creates registration in an excluded namespace.
Clients must explicitly persist/use new typed IDs; retrying an uncertain old
operation under a new ID is a new request whose semantic duplication cannot be
prevented by exactly-once machinery. Status reports that distinction; it never
converts an old retry to a new namespace automatically.

`OriginalRequest2 = api_version:u16 (=2) || id:RequestID2 ||` the exact v1
OriginalRequest fields **after** its id. `InternalRequest2 =
internal_version:u16 (=2) || id:RequestID2 || namespace_volume:uuid || view:uuid
|| intent:Intent`. `RequestDocument2 = class:u8 || original:<class schema>`;
class 1 is OriginalRequest2, class 2 InternalRequest2, class 3 is an exact
`blob<RequestDocumentV1>` imported solely for immutable old claim recovery.
Class 3 cannot authorize a new claim. Its digest dispatches on the **inner**
strict v1 document class: inner class 1 uses SHA-256 of exact OriginalRequestV1
alone; inner class 2 uses SHA-256 of ASCII `HOMELABFS-INTERNAL` then NUL then
exact InternalRequestV1. The outer class-3 byte, blob length and inner document
class byte are not hashed. Compare inner class and exact original canonical
bytes as well as digest; no class-3 wrapper or v2 domain prefix changes it.
For class 1/2 v2 documents, original digest is SHA-256 of exact
OriginalRequest2; internal digest is SHA-256 of ASCII `HOMELABFS-INTERNAL-2`
then NUL then InternalRequest2. The tag and namespace participate in equality
and canonical bytes, not credentials, deadline or routing hint. RequestID2 tag 1
is permitted in v2 recovery/status bindings to preserved old IDs; new v2 Execute
requires tag 2. No v1 bytes are re-encoded as version 2 to change an old digest.

A v2 session-registration wire body is `namespace:Namespace || principal:Principal
|| anchor:uuid || nonce:uuid`; principal must equal authenticated caller. Request,
GetStatus, Cancel, Retire and Close bodies always contain the full RequestID2
(number=1 for session-key operations). Frames/messages carrying class-3 claims
or tag-1 IDs are explicit recovery/status traffic, never new registration. The
future typed fsapi extension is required in `rw3`/integration before activation;
existing v1 Go structs cannot carry tag-2 IDs and must fail capability checks,
not squeeze them into nonce/principal or silently accept loss.

Dispatch order is mandatory: decode version/type/bounds; authenticate principal;
load current control/namespace bars; route the immutable anchor within the
selected lineage; consult rejection/floor/claim/binding/result; compare exact
original canonical bytes; only then consider a new claim and resolve paths.
Barred nodes return unavailability and cannot mint claims or authoritative hints.
Missing route/history/control returns INDETERMINATE; never try a source path,
current lowest coordinator or another anchor. Preserved old claims/results and
DecisionIDs retain their original coordinator/authority/outcome through both
entry and exit; exclusion affects executability, not historical truth.

| Request class/state | Required behavior before any pathname resolution |
|---|---|
| v1 client on complete-history, non-loss v2 unit | Existing claimed retries/status use strict v1 ingress and original tag-1 identity, returning or completing only their already bound original outcome under real normal recovery evidence; an unbound old claim stays gated or reaches a proven original preflight rejection without effects. New v1 registration/unclaimed Execute returns UNSUPPORTED_ID_NAMESPACE; new work requires explicit version-2 registration. No old ID is translated. |
| v1 registration/Execute after loss, including unknown nonce | `UNSUPPORTED_ID_NAMESPACE` for registration/new Execute; an existing exact claimed retry is status-only. Never register or execute it, even with no local match. |
| v1 GetStatus/Cancel/Retire/Close after loss | Route as tag 1; RETIRED_ID only where proved, otherwise INDETERMINATE(EXCLUDED_HISTORY) to the v1 client. To inspect a retained original outcome use explicit v2 status with tag1 and historical/excluded flags. Cancel cannot create an abort; retirement cannot manufacture missing completeness. |
| tag-2 namespace current, existing claim | Compare exact document/class/digest; return original pending/result or ID_CONFLICT; never rebind after merge/mode change. |
| tag-2 namespace current, no claim and complete live anchor/session | Protect claim before paths, then follow normal/emergency mode predicate. GetStatus alone never executes. |
| tag-2 namespace barred or less than durable frontier | Exact preserved result or RETIRED_ID where proved, otherwise INDETERMINATE(EXCLUDED_HISTORY); new registration/Execute rejects ID namespace. All numbers permanently nonexecutable. |
| Unknown registry/future serial, malformed tag or missing frontier/history | Reject unsupported namespace or report INDETERMINATE authority; never allocate a namespace on demand or interpret unknown bytes as v1. |
| New v2 client against v1 server | Negotiation fails UNSUPPORTED_VERSION; client retains typed ID and cannot retry as v1 or a new namespace. |

`UNSUPPORTED_ID_NAMESPACE` is a transport/pre-admission capability error, not a
new v1 TerminalResult numeric code or a claim that an old outcome never occurred.
An explicit v2 status query for an excluded retry with retained result returns
that original result as historical status (explicit generation/mode and excluded flag), never a new terminal
REJECTED/ABORTED value for the old identity. Result status fields on the wire
are defined below. Floors/closed-session/retired-anchor mappings remain mandatory
for live namespaces too; namespace bars do not replace normal dedup retention.

## Checkpoint, coverage, archive, final exit and migration objects

Shared encodings:

```text
CheckpointAcceptance = tag:u8 || volume:uuid || checkpoint:uuid ||
                       manifest:ObjectRef(10) || body:<tag schema>
  tag=1 NORMAL: acceptance:RecordRef
  tag=2 EMERGENCY: acceptance:RecordRef || protection:RecordRef
PairReceipt = node:NodeLaunch || durable:Vector ||
              records:array<RecordRef> || objects:array<ObjectRef> ||
              files:array<File> || checkpoint:ObjectRef(10)
Coverage2 = replica:uuid || incarnation:optional<uuid> || method:u8 ||
            through:Vector || source:ObjectRef || suffix:array<RecordRef> ||
            pins:array<RecordRef>
Obligation = replica:uuid || histories:array<History> ||
             literal:u8 || coverage:array<Coverage2> || pins:array<RecordRef>
```

PairReceipt has exactly one owner/one peer receipt, sorted by node; it asserts
actual complete durable storage, verified again at use against exact prospective
incarnations and publication state. It is not a signature that replaces sync.
Coverage methods 1 local complete checkpoint+suffix (source kind10), 2 archive
(source kind11), 3 snapshot baseline+suffix (source kind12). Every history is
covered, hashes/files retrieved and verified, sources durably pinned for the
named obligation; optional incarnation is absent only for a barred/offline
replica. Replica memory receipt is not a Coverage2 method. All assigned members
remain in Obligation, even when only one serves. `literal=1` requires exact
original frames; zero allows the explicitly plan-approved semantic checkpoint
replacement. Missing coverage preserves local history and stops writes.

| Kind | Name | Payload in exact field order |
|---:|---|---|
| 10 | Checkpoint | `checkpoint:uuid, cluster:uuid, generation:uuid, closure:ClosureKey, authorities:array<AuthorityRef>, roots:array<ObjectID>, committed:Vector, applied:Vector, visibility:Visibility, predecessor:optional<ObjectRef(10)>, catalogs:array<File>, payloads:array<File>, semantics:array<RecordRef>, replacements:array<ObjectRef(18)>, pins:array<RecordRef>, required_suffix:array<RecordRef>, control:ObjectRef(4), namespaces:array<Namespace>, deny_v1:u8` |
| 11 | Archive | `archive:uuid, plan:ObjectRef(2), checkpoint:optional<ObjectRef(10)>, exact_frames:array<File>, payloads:array<File>, dependencies:array<RecordRef>, replacements:array<ObjectRef(18)>, source_histories:array<History>, destination:uuid, destination_binding:bytes[32], holds:array<uuid>, key_ids:array<uuid>, verified:ObjectRef(13), obligations:array<uuid>, retained_bytes:u64` |
| 12 | Snapshot | `snapshot:uuid, plan:ObjectRef(2), checkpoint:ObjectRef(10), datasets:array<SnapshotDataset>, control:ObjectRef(4), coordination:Coordination, dependencies:array<RecordRef>, holds:array<uuid>` |
| 13 | Retrieval | `verification:uuid, plan:ObjectRef(2), destination:uuid, destination_binding:bytes[32], files:array<File>, holds:array<uuid>, key_ids:array<uuid>, retrieved_bytes:u64, verifier:NodeLaunch, completed:Time` |
| 14 | Retention | `retention:uuid, predecessor:optional<ObjectRef(14)>, plan:ObjectRef(2), checkpoint:ObjectRef(10), acceptances:array<CheckpointAcceptance>, published:array<ObjectRef(19)>, reclaim_through:Vector, obligations:array<Obligation>, snapshot_pins:array<RecordRef>, transfer_pins:array<RecordRef>, control:ObjectRef(4), replacements:array<ObjectRef(18)>, budget:Budget` |
| 15 | FinalCut | `exit:uuid, plan:ObjectRef(2), control:ObjectRef(4), cut:Vector, closures:array<ClosureKey>, authorities:array<AuthorityRef>, receipts:array<PairReceipt>, obligations:array<Obligation>, fences:array<ObjectRef(8)>, coordination:Coordination` |
| 16 | ExitPrepare | `exit:uuid, plan:ObjectRef(2), final:ObjectRef(15), successors:array<AuthorityRef>, coordination:Coordination, visibility:array<Visibility>, unavailable:array<uuid>, grants:array<ObjectRef(6)>, revocations:array<ObjectRef(9)>` |
| 17 | ExitCommitted | `exit:uuid, prepared:ObjectRef(16), normal_required:array<RecordRef>, receipts:array<PairReceipt>, normal_protection:array<RecordRef>, publications:array<ObjectRef(19)>, barriers:array<RecordRef>, predecessor_control:ObjectRef(4)` |
| 18 | Replacement | `replacement:uuid, closure:ClosureKey, records:array<blob>, source:array<RecordRef>, dependencies:array<RecordRef>, namespaces:array<Namespace>, deny_v1:u8` |
| 19 | Publication | `publication:uuid, cluster:uuid, closure:ClosureKey, volume:uuid, checkpoint:ObjectRef(10), acceptance:CheckpointAcceptance, data_root:uuid, journal_root:uuid, data_guid:u64, journal_guid:u64, manifest:ObjectRef(10), applied:Position, visibility:Visibility` |
| 20 | Migration | `migration:uuid, cluster:uuid, run:uuid, predecessor:array<File>, scope:Scope, target_major:u16 (=2), target_minor:u16 (=0), required_features:u64 (=1), readers:array<Reader>, control:ObjectRef(4), checkpoint:ObjectRef(10), old_pins:array<RecordRef>, complete:u8 (=1)` |
| 21 | RegistryFrontier | `registry:uuid, cluster:uuid, run:uuid, ordinal:u64, previous:optional<ObjectRef(21)>, control:ObjectRef(4), revision:u64, namespace_serial:u64, namespace_bars:array<Namespace>, deny_v1:u8, guard_revision:u64, guard_sha256:bytes[32], host_boot:uuid, key_id:uuid, signature:bytes[64]` |

RegistryFrontier is the exact independent external audit checkpoint. Ordinal
starts at 1 and increases by one under the singleton lock; control revision,
namespace frontier/bars and guard digest equal the newly published exact control
state. Sign using the pinned broker Ed25519 key over ASCII `HFS-FRONTIER-2`
then NUL then its Object prefix and payload through key_id. Sync the new object
and external pointer (the 50-byte pointer form below, target kind21), then the
host control pointer, before new launch/serving effects. Both predecessor copies
remain until reconciliation. A crash/mismatching pointer keeps effects gated;
reconcile only the identical pending CAS, never choose a lower ordinal/revision.
The external audit storage binding/key is pinned at enrollment and independently
accessible without guest workloads. It serializes one enrolled registry, not
multiple active replicas. Missing/unreachable audit, forked signed frontier,
restored host copy behind the audit or loss of both continuity witnesses blocks
new authority; a freshly generated registry/key cannot reset the serial frontier.
This is required future broker behavior, not an existing lab facility.

`SnapshotDataset = volume:uuid || role:u8 (1 data,2 journal,3 control,
4 dedicated coordination) || dataset_guid:u64 || snapshot_guid:u64 || hold:uuid`;
sort by `(volume,role,dataset_guid)`. GUIDs/holds are nonzero. Actual complete
run-owned datasets must match the admitted mapping; timestamps/common snapshot
names are insufficient. `Reader = component:u8 || identity:uuid ||
incarnation:uuid || major:u16 (=2) || minor:u16 (=0) || features:u64 (=1) ||
implementation_sha256:bytes[32]`; components are 1 daemon/segment reader,
2 receiver/transport,3 store/catalog/manifest,4 archive/retrieval,5 snapshot/
restore,6 control/broker/launcher,7 admin/recovery tool,8 FUSE/publication adapter,
9 dedicated coordination bootstrap,10 fsapi client bridge. Reader sets sort by
(component,identity) and enumerate every future actor in the recovery unit,
including offline peers that remain barred until their upgraded identity is
attested. A capability claim is validated against the installed enrolled binary/
configuration; a self-reported version string does not upgrade a reader.

Checkpoint catalogs/payloads exactly cover each closure volume/root, and include
unlinked pinned objects and clean base hashes, never the current dirty files
under an older catalog. Retained typed frames/replacements preserve full session,
claim/canonical bytes, immutable binding/coordinator, old/new decisions/results,
affinity/predecessors, home ledgers, handles, promises, fences and permanent
rejection state. Replacement records contain exact original complete frames in
stream-key order (same 4 MiB embedded-frame limit); they have no recursively
embedded Evidence fields. Source refs map one-to-one to those exact bytes.
Replacement objects cannot contain themselves, refer to future replacements or
replace a semantic graph with hashes. Shard above bounded size; missing shards
block checkpoint use. The existing catalog metadata/redo semantics stay exact.

CheckpointAcceptance is a **closed** sum, not an arbitrary RecordRef. Tag 1
requires an actual NORMAL `(2,1)` kind64 CHECKPOINT_ACCEPT with its full normal
protection graph/current real owner-partner predicate; its data_manifest must
be Artifact kind9 for exactly manifest, checkpoint and closure_checkpoint UUIDs
must equal the common checkpoint UUID, and header volume/root/catalog, committed/
applied/visibility/closure-members/required-suffix must match that volume and
the complete kind10 object. Tag 2 requires `(2,2)` kind132 E_CHECKPOINT whose
checkpoint is exactly manifest, plus `(2,2)` kind130 E_PROTECT whose target is
exactly that acceptance and whose verified durable prefix/objects/dependencies
cover its entire checkpoint graph. Both frames' header volume/history and
emergency authority must match each other and that checkpoint's volume entry.
No tag permits kind49 ACCEPT, a missing E_PROTECT, mode-E kind64, NORMAL
kind132, a mismatched target/checkpoint/closure/vector, or a hash-only proof.

The common volume belongs to manifest.closure; common checkpoint equals its
checkpoint UUID. Publication.acceptance must match Publication.volume,
checkpoint/manifest, applied position and visibility exactly. The primary
Retention.acceptances contain exactly one matching entry per volume of its
checkpoint's full closure, and journal RETENTION.checkpoint_accepts must equal
that exact typed array byte-for-byte. Additional historical acceptance entries
are allowed only for checkpoints explicitly used by retained Obligation sources;
sort all entries uniquely by `(volume,checkpoint,manifest.sha256)`. Their actual
mode/protection and source history stay unchanged. Every primary Publication
must cite its same acceptance entry; all CURRENT pairs and complete closure
vectors must be durably published/revalidated before retention can advance.

Historical v1 NORMAL kind64 frames stay exact `(1,1)` retained source evidence;
they never claim to accept a later kind10 v2 object. To publish that state as a
v2 checkpoint, verify its old manifest/catalog/suffix under the strict old codec,
construct kind10 and obtain a new matching real NORMAL kind64 or EMERGENCY
kind132+E_PROTECT. Preserve the old proof in the dependency graph. Similarly,
normal v2 retention after exit may depend on historical **EMERGENCY** acceptance
entries or Coverage2 bases: keep tag2/protection `(2,2)`, full original durable
graph and every intervening normal suffix. That coverage is a recovery source,
not permission for new normal operations/checkpoints to use E protection. The
current normal RETENTION itself still requires the real current normal pair;
all assigned replicas' durable coverage/pinned bases, offline obligations,
closure publication and GC/pin conditions remain mandatory. No pair/GC predicate
is weakened or old E evidence relabelled as a normal ACCEPT.

E_CHECKPOINT is synced/protected after kind10 bytes and all payload/directory
barriers; mode-E inherited CHECKPOINT_ACCEPT is forbidden (use E_CHECKPOINT).
V2 normal CHECKPOINT_ACCEPT uses T(body), with data_manifest an Artifact kind9
that binds kind10 and catalog the exact Artifact kind1 for its volume.
Write each immutable manifest/catalog, sync its name, then journal acceptance,
then each CURRENT and parent, then revalidate the entire publication vector.
Publication data_root/journal_root are the enrolled private-root binding UUIDs,
verified with their actual dataset GUIDs and selected manifest mapping, never
inferred from a path. Kind19 is the durable evidence of that actual pointer pair, not permission from
an orphan candidate. No participant alone releases old pins. Inherited v2
RETENTION body must reference kind14 through its published-manifest Artifact
(kind=8, length/hash of exact kind14 object), and keeps all other original fields and
normal/E protection; this is a typed required object, never an opaque bypass.
Reclamation requires both the full protected journal RETENTION and kind14,
serialized with transitions, checkpoint, restore and GC. Extra crash copies are
safe; missing covered histories are not. Preserve all offline obligations,
permanent identity bars and external holds across entry/exit. ENOSPC/reserve or
unknown charge gates admission before accepted completion budget is consumed.

Archive kind11 requires either exact complete history+payloads or kind10 plus
all required suffix/dependencies, as declared by Plan and each Obligation.
Kind13 intentionally references the files/destination, **not** kind11's hash,
so retrieval evidence can precede Archive without a cycle. Revalidate the same
retrieved bytes/hashes, destination/key availability and continuing holds before
retention. A same-disk archive adds no disk-loss redundancy. Unknown codec,
missing key/hold/coverage, failed retrieval or absent archive admission means
keep local data and stop writes. No uploader or snapshot owner is implied.

FinalCut B is finite after closure admission freeze and resolution of all
selected slots. Both actual prospective nodes durably hold/apply its complete
graph, clean bases, emergency/normal streams and semantic state. Pair receipts
reference earlier checkpoints at B; those checkpoints do not reference FinalCut.
ExitPrepare successors are normal, distinct real owner/partner within the
original three members, preserve emergency generation and reference its retained
evidence. Coordination lineage is explicitly rebuilt/reconciled, not the old
etcd revision/quorum; all stale launches stay barred. Kind133 MODE_INSTALL is
mode NORMAL only and must be installed and durably synced at both nodes for every
reopening volume, even when one node was previously designated. After its first
durable install there is **no** fallback local-only completion on that node.

ExitCommitted requires the complete pair's MODE_INSTALL vector (one owner
record per volume, copied byte-identically and verified durable at both nodes
by its two new PairReceipts rather than duplicate RecordRefs), retained true
normal protection evidence for any selected normal candidates, verified view/
cache barriers and publications, all linked to the same ExitPrepare/B.
The exit plan permits the named recovery-only peer to copy/sync these transition
records and report PairReceipts, but grants no ordinary candidate ACCEPT or
workload role before EXIT_COMMITTED. MODE_INSTALL is a transition, not an
ordinary COMMIT/DECISION candidate; no synthetic ACCEPT is invented for it.
The two durable receipts and independent exit CAS establish its installation.
Both nodes persist NORMAL_REQUIRED and their full successor vector; independent
control then CAS-publishes EXIT_COMMITTED referencing this already durable
object and preserving bars. Workload gates open only after all those predicates
and matching current coordination activation are revalidated. Partial exit
stays gated and resumes the same attempt; it cannot resume emergency because
a pair/etcd failed. Excluded volumes remain explicitly unavailable, third-node
automatic rejoin stays barred, and completion thereafter uses ordinary owner-
durable/partner-retained-memory protection. B's two-durable-copy requirement
is an exit barrier, not a changed normal fsync promise or downgrade to v1.

## Reader, transport, boot and whole-unit migration gates

Protocol 2.0 begins with `Hello = ASCII HFSWIRE2 || major:u16 (=2) || minor:u16
(=0) || features:u64 (=1) || cluster:uuid || run:uuid || node:NodeLaunch ||
control:ObjectRef(4) || grant:optional<ObjectRef(6)> || mode:u8 ||
maximum_message:u32` (1..8,388,608). Negotiate exact version/feature intersection
under existing authenticated transport, then authorize current control/grant,
lineage and mode. A v1 peer cannot receive/protect a v2 recovery unit even if
some copied frames are v1. Recovery-only sessions cannot become normal sessions
in place. All messages are `length:u32 || kind:u16 || reserved:u16 (=0) ||
body`, length counts kind/reserved/body and is capped by Hello and 8 MiB;
kind 1 transfers `blob<complete frame>`, 2 transfers `blob<complete Object>`,
3 requests `array<RecordRef> || array<ObjectRef>`, 4 reports
`received:Vector || durable:Vector || protected:Vector || applied:Vector ||
missing:array<RecordRef> || missing_objects:array<ObjectRef>`, and 5 returns
`code:u16 || request:optional<RequestID2> || result:optional<T(TerminalResult)>
|| historical:u8 || excluded:u8 || control:ObjectRef(4)`.
Status code 0 terminal (result required), 1 pending, 2 indeterminate,
3 retired ID, 4 ID conflict, 5 unsupported version, 6 unsupported identity
namespace, 7 unavailable authority, 8 resource limit; other codes invalid.
Nonterminal codes omit result. Historical/excluded are explicit booleans;
excluded implies historical; an old surviving result stays the original value.
Transport transfer/report is not a workload Execute API or proof of protection.
No compression/fragmentation/default mode/fallback decoder is defined. A larger
artifact uses bounded private-file transfer under its typed manifest and admitted
budget; it never becomes a journal ACK without independent full validation.

Every cold start loads enrolled identity/private roots, independent current
control/guard/audit frontier, highest promises/mode, namespace bars, storage
markers, migration vector and complete selected dependency graph **before**
network acceptance, client APIs or mount publication. Only status/diagnostics
under the admin socket may precede readiness; no workload access does. Missing/
unknown/corrupt/outdated control, contradictory higher witness, old schema,
partial migration or rollback of NORMAL_REQUIRED blocks serving/acceptance.
Non-designated/barred nodes remain barred despite old transport keys, etcd grants
or copied normal configurations. RecoveryReady is explicitly role-limited;
ServingReady requires full mode-specific graph, actual current launch, publication
and cache predicates. New incarnation reconciles the full lineage and fresh
launch grants; it cannot inherit volatile proof or cached view rights.

Migration is an explicit whole-recovery-unit operation behind closed gates:

1. Discover/freeze the full dependency closure and transitive predecessor,
   anchor/coordinator/home/handle/pin graph, including partial joins. Inventory
   every serving/receiving, archive/snapshot/restore, admin/client bridge and
   boot/control reader. Reject uncertainty; do not upgrade a convenient subset.
2. Install/verify `emergency-2` support on every reader that can touch the new
   unit. Offline readers stay durably barred and cannot admit/rejoin until this
   check passes. Actual installation and any disruptive window require their
   concrete approvals. Reader codec capability alone supplies no runtime fence.
3. Preserve the old available recovery unit and all original bytes/pins in a
   separate private destination/archive with verified independent retrieval.
   If history was already missing, document the missing old history in the
   separately approved loss plan; preservation cannot claim to repair it.
   Create the v2 unit in a **new private destination**, with exact original v1
   histories imported as immutable NORMAL refs and verified selection/loss
   objects. No in-place normal-frame rewrite or creator-incarnation edit.
4. Data/journal `format.json` retain the exact v1 ordered field schema from
   Backing-store (`magic,major,minor,role,volume_uuid,root_object_id`) with
   major=2/minor=0. Encode compact UTF-8 JSON in that order with no insignificant
   whitespace and exactly one final LF; root_object_id uses ordered home_uuid,
   allocation_uuid. UUID strings are canonical lowercase. Catalog schema and
   deterministic ordering are unchanged from Storage-implementation except
   major=2/minor=0; semantic state is the typed kind10/18 graph, never opaque
   dependencies. A v1 store reader rejects major 2 before interpreting payloads.
5. New checkpoint `manifest.hfs` is the exact kind10 Object; old manifest.json
   is absent. New CURRENT is `ASCII HFS2PTR then NUL || target:ObjectRef(19) ||
   crc32c:u32`, CRC over the prefix and ref (50 bytes total). The target is the
   later verified publication record; its acceptance links the earlier kind10
   manifest. To avoid a publication cycle, kind19 asserts the prepared exact
   CURRENT target/pair; actual pointer/parent durability is checked separately
   before it counts as published. No manifest includes its CURRENT or kind19.
   The control pointer uses the same 50-byte form with target kind4. Snapshot/
   archive tools require the same kind20 migration/reader requirements rather
   than decoding an old JSON manifest by guessing.
6. Persist kind20 Migration and exact new marker/checkpoint/control publication
   vector in independent control evidence (kind20 now permitted there). Every
   volume's data/journal marker agrees, all semantic suffixes/pins and predecessor
   evidence validate, every necessary reader is upgraded or barred. Revalidate
   the whole vector and mode-specific activation before any gate opens. Crash
   or partial pointer fan-out preserves both units and remains gated.

There is no downgrade after v2 writes or emergency selection: even normal exit
references emergency history and namespace bars. v1-only daemon/receiver,
manifest/archive/snapshot reader, control boot or recovery tool must report
UNSUPPORTED_VERSION without rewriting, formatting, skipping or deleting state.
Mixed v1/v2 rolling installation is allowed while operating an untouched v1
unit with emergency capability unpublished; a v2 unit never becomes executable
until the whole affected unit satisfies migration gates. Old peers may be copied
to isolated quarantine by an upgraded tool, but receive no serving or ACK role.

## Golden vectors and negative conformance

The following self-contained Python generator has no file/network I/O and uses
only fixed synthetic bytes. It is a representation check, not a codec/runtime/
lab test. Its printed bytes and SHA-256 values are normative vectors; it also
pins malformed/unknown/mixed tags and the namespace registration boundary.
The larger authority/control graph above must be validated separately by `rw3`;
these isolated fixtures deliberately contain unresolved synthetic references
and cannot authorize protection or serving by themselves. The abort candidate
is an alternative fixture, not a continuation of the commit/protect fixture.

```python
import hashlib
import struct


def u16(n):
    return struct.pack('>H', n)


def u32(n):
    return struct.pack('>I', n)


def u64(n):
    return struct.pack('>Q', n)


def uid(n):
    return bytes([n]) * 16


def array(items):
    return u32(len(items)) + b''.join(items)


def crc32c(data):
    crc = 0xffffffff
    for byte in data:
        crc ^= byte
        for _ in range(8):
            crc = (crc >> 1) ^ (0x82f63b78 if crc & 1 else 0)
    return crc ^ 0xffffffff


def obj(kind, payload):
    prefix = b'HFS2OBJ\0' + u16(2) + u16(0) + u16(kind) + u16(0) + u32(len(payload))
    body = prefix + payload
    return body + u32(crc32c(body))


def ref(kind, value):
    return u16(kind) + u32(len(value)) + hashlib.sha256(value).digest()


# Complete original normal authority, volume 11/gen22/config33, epoch7.
# owner44/inc77, real partner55/inc88, members44/55/66, coordinator term9.
normal = (uid(0x11) + uid(0x22) + uid(0x33) + u64(7) + uid(0x44)
          + uid(0x55) + array([uid(0x44), uid(0x55), uid(0x66)])
          + u64(9) + uid(0x77) + uid(0x88))
normal_authority = b'\x01' + normal
# Synthetic typed ObjectRefs do not assert the referenced object exists.
launch = u16(8) + u32(24) + bytes([0xa8]) * 32
control = u16(4) + u32(24) + bytes([0xa4]) * 32
plan = u16(2) + u32(24) + bytes([0xa2]) * 32
position = uid(0x11) + uid(0x22) + uid(0x33) + u64(7) + u64(1)
closure = uid(0x99) + u64(1) + array([uid(0x11)])
e_position = uid(0x11) + uid(0x23) + uid(0x34) + u64(8) + u64(1)
descriptor = (b'\x02' + uid(0x11) + uid(1) + uid(2) + uid(0x23) + uid(3)
              + uid(0x44) + uid(0x77) + uid(0x78) + closure + array([e_position])
              + array([uid(0x44), uid(0x55), uid(0x66)])
              + array([uid(0x11) + normal_authority]))
prospective_ref = u16(0) + hashlib.sha256(descriptor).digest()
authority_payload = (uid(1) + uid(2) + uid(0x23) + uid(3) + uid(0x44)
                     + uid(0x77) + launch + closure + array([e_position])
                     + array([uid(0x44), uid(0x55), uid(0x66)])
                     + array([uid(0x11) + normal_authority]) + control + plan
                     + array([prospective_ref]))
emergency_object = obj(1, authority_payload)
emergency_authority = b'\x02' + uid(0x11) + ref(1, emergency_object)
# Complete owner FrameRefV1, unchanged old bytes; hash is fixture value aa*32.
frame_ref = b'\x01\x00\x00' + position + bytes([0xaa]) * 32
normal_ref = b'\x01\x01' + frame_ref
normal_v2_ref = b'\x02\x01' + frame_ref
emergency_ref = b'\x02\x02\x01\x00\x00' + e_position + bytes([0xaa]) * 32
principal = uid(1) + u32(1) + b'u'
legacy_id = principal + uid(0x11) + uid(0x33) + u64(1)
old_id = b'\x01' + legacy_id
new_id = b'\x02' + uid(1) + uid(0x70) + u64(2) + legacy_id
later_id = b'\x02' + uid(1) + uid(0x70) + u64(3) + legacy_id
assert len(normal) == 180 and len(frame_ref) == 99
assert old_id != new_id and new_id != later_id
assert normal_ref != emergency_ref


def validate_ref(value):
    if len(value) != 101:
        return 'INVALID_LENGTH'
    if value[0] not in (1, 2) or value[1] not in (1, 2):
        return 'UNSUPPORTED_TAG'
    if value[:2] == b'\x01\x02':
        return 'MODE_CONFUSION'
    return 'DECODED_REQUIRES_GRAPH'


assert validate_ref(normal_ref) == 'DECODED_REQUIRES_GRAPH'
assert validate_ref(normal_v2_ref) == 'DECODED_REQUIRES_GRAPH'
assert validate_ref(emergency_ref) == 'DECODED_REQUIRES_GRAPH'
assert validate_ref(b'\x01\x02' + frame_ref) == 'MODE_CONFUSION'
assert validate_ref(b'\x03\x01' + frame_ref) == 'UNSUPPORTED_TAG'
assert validate_ref(normal_ref + b'\0') == 'INVALID_LENGTH'
# Control lost prior unknown-ID history; all legacy IDs are still nonexecutable.
# No pathname is accepted by this pre-routing registration predicate.

def may_register(tag, registry, serial, frontier, bars, continuity, history):
    return (tag == 2 and registry == uid(0x70) and continuity and history
            and serial == frontier and serial not in bars)


assert not may_register(1, uid(0x70), 0, 3, {1, 2}, True, True)
assert not may_register(2, uid(0x70), 2, 3, {1, 2}, True, True)
assert not may_register(2, uid(0x71), 3, 3, {1, 2}, True, True)
assert not may_register(2, uid(0x70), 3, 3, {1, 2}, False, True)
assert not may_register(2, uid(0x70), 3, 3, {1, 2}, True, False)
assert may_register(2, uid(0x70), 3, 3, {1, 2}, True, True)
assert crc32c(b'123456789') == 0xe3069283


def frame(kind, sequence, body):
    payload = b'\x02' + emergency_authority + body
    header = struct.pack('>8sHHHHIIII16s16s16sQQ16s16sqII',
                         b'HFSJREC\0', 2, 0, 144, kind, 0, 148 + len(payload),
                         len(payload), 0, uid(0x11), uid(0x23), uid(0x34),
                         8, sequence, uid(3), uid(0x77), -1, 123456789, 0)
    return header + payload + u32(crc32c(header + payload))


selection_ref = u16(3) + u32(24) + bytes([0xa3]) * 32
commit = frame(128, 3, emergency_ref + array([e_position]) + selection_ref)
commit_position = e_position[:-8] + u64(3)
commit_ref = (b'\x02\x02\x01\x00\x00' + commit_position
              + hashlib.sha256(commit).digest())
protect = frame(130, 4, commit_ref + array([commit_position])
                + array([emergency_ref])
                + array([ref(1, emergency_object), plan, selection_ref, control])
                + b'\x00')
# Alternative isolated abort candidate with provisional result, not a terminal
# reply or proof of absence of an old normal COMMIT.
decision_id = uid(0x11) + uid(0x23) + uid(0x34) + u64(8) + new_id + u64(9)
provisional_abort = b'\x02' + u16(15) + b'\x00' * 3 + array([]) + b'\x00' + array([])
decision = frame(129, 5, decision_id + bytes([0xbb]) * 32 + emergency_ref
                 + b'\x02' + array([]) + provisional_abort + selection_ref)
# Legacy comparison views: only specified wrapper bytes are added/removed.
object_id = uid(0x11) + bytes.fromhex('77777777777747778777777777777777')
parent_id = uid(0x11) + bytes.fromhex('aaaaaaaaaaaa4aaa8aaaaaaaaaaaaaaa')
intent = u16(4) + object_id + parent_id + u32(1) + b'd' + array([])
legacy_internal = u16(1) + legacy_id + uid(0x11) + uid(0x55) + intent
legacy_digest = hashlib.sha256(b'HOMELABFS-INTERNAL\0' + legacy_internal).digest()
inner_document = b'\x02' + legacy_internal
class3_document = b'\x03' + u32(len(inner_document)) + inner_document
assert class3_document[0] == 3
assert struct.unpack('>I', class3_document[1:5])[0] == len(class3_document[5:])
assert class3_document[5] == 2 and class3_document[6:8] == u16(1)
assert hashlib.sha256(b'HOMELABFS-INTERNAL\0' + class3_document[6:]).digest() == legacy_digest
assert hashlib.sha256(class3_document).digest() != legacy_digest
assert hashlib.sha256(b'HOMELABFS-INTERNAL-2\0' + class3_document[6:]).digest() != legacy_digest
legacy_affinity = uid(0x99) + u64(1) + bytes([0xcc]) * 32
middle = legacy_digest + legacy_affinity + uid(0x11) + intent + array([uid(0x11)])
legacy_binding = legacy_id + middle + array([normal]) + array([position])
lifted_binding = old_id + middle + array([normal_authority]) + array([position])
authority_offset = len(old_id) + len(middle)
assert lifted_binding[0] == 1
assert lifted_binding[authority_offset:authority_offset + 5] == u32(1) + b'\x01'
unlifted_binding = (lifted_binding[1:authority_offset] + u32(1)
                    + lifted_binding[authority_offset + 5:])
assert unlifted_binding == legacy_binding
legacy_decision_prefix = uid(0x11) + uid(0x22) + uid(0x33) + u64(7)
legacy_decision_ref = legacy_decision_prefix + legacy_id + u64(9) + frame_ref
lifted_decision_ref = legacy_decision_prefix + old_id + u64(9) + normal_ref
assert lifted_decision_ref[56] == 1
ref_offset = 56 + len(old_id) + 8
assert lifted_decision_ref[ref_offset:ref_offset + 2] == b'\x01\x01'
assert (lifted_decision_ref[:56] + lifted_decision_ref[57:ref_offset]
        + lifted_decision_ref[ref_offset + 2:]) == legacy_decision_ref
result_prefix = b'\x01' + u16(0) + b'\x01' + object_id + b'\x00\x01'
result_suffix = array([position]) + b'\x01' + legacy_affinity + u64(1) + array([])
legacy_result = result_prefix + legacy_decision_ref + result_suffix
lifted_result = result_prefix + lifted_decision_ref + result_suffix
assert legacy_result[:len(result_prefix)] == lifted_result[:len(result_prefix)]
assert legacy_result[-len(result_suffix):] == lifted_result[-len(result_suffix):]
# Includes implicit native request/signature coverage, not just stored refs.
# These edge fixtures test construction order; they are not full wire objects.
dag_order = [
    ('old_authority', []), ('old_fence', ['old_authority']), ('old_control', []),
    ('descriptor', ['old_authority']),
    ('plan', ['old_control', 'descriptor', 'old_fence']), ('approval', ['plan']),
    ('intent', ['old_control', 'plan', 'approval']),
    ('native_request', ['intent', 'plan', 'descriptor', 'approval']),
    ('attestation', ['native_request', 'plan', 'descriptor', 'old_fence']),
    ('launch_evidence', ['attestation', 'plan', 'descriptor', 'old_fence']),
    ('authority', ['launch_evidence', 'intent', 'plan', 'old_authority', 'descriptor']),
    ('new_e_fence', ['authority', 'launch_evidence']),
    ('selection', ['plan', 'intent', 'launch_evidence', 'new_e_fence']),
    ('local_durable', ['intent', 'authority', 'selection']),
    ('activating', ['local_durable', 'authority', 'selection']),
    ('active', ['activating', 'authority', 'selection'])]


def construct_graph(order):
    hashes = {}
    for name, deps in order:
        if name in hashes or any(dep not in hashes for dep in deps):
            return None  # includes missing, self, forward and cyclic references
        payload = descriptor if name == 'descriptor' else name.encode('ascii')
        hashes[name] = hashlib.sha256(payload + b''.join(hashes[d] for d in deps)).digest()
    return hashes


assert construct_graph(dag_order) is not None
assert prospective_ref[2:] == hashlib.sha256(descriptor).digest()
bad_forward = [(n, d + ['new_e_fence'] if n == 'launch_evidence' else d)
               for n, d in dag_order]
bad_cycle = [(n, d + ['authority'] if n == 'attestation' else d)
             for n, d in dag_order]
assert construct_graph(bad_forward) is None
assert construct_graph(bad_cycle) is None
assert construct_graph([('self', ['self'])]) is None
assert construct_graph([('missing', ['absent'])]) is None
# Isolated canonical checkpoint-acceptance sum fixtures; referenced objects
# remain synthetic. Metadata below stands for independently resolved facts.
checkpoint_ref = u16(10) + u32(24) + bytes([0xca]) * 32
checkpoint_id = uid(0xc1)
checkpoint = frame(132, 6, checkpoint_ref + array([commit_ref]) + b'\x00')
checkpoint_position = e_position[:-8] + u64(6)
e_checkpoint_ref = (b'\x02\x02\x01\x00\x00' + checkpoint_position
                    + hashlib.sha256(checkpoint).digest())
checkpoint_protect = frame(130, 7, e_checkpoint_ref + array([checkpoint_position])
                           + array([commit_ref]) + array([checkpoint_ref]) + b'\x00')
proof_position = e_position[:-8] + u64(7)
checkpoint_proof_ref = (b'\x02\x02\x01\x00\x00' + proof_position
                        + hashlib.sha256(checkpoint_protect).digest())
acceptance_common = uid(0x11) + checkpoint_id + checkpoint_ref
normal_checkpoint_acceptance = b'\x01' + acceptance_common + normal_v2_ref
emergency_checkpoint_acceptance = (b'\x02' + acceptance_common + e_checkpoint_ref
                                  + checkpoint_proof_ref)
identity = (uid(0x11), checkpoint_id, checkpoint_ref, closure, e_position, e_position)
metadata = {
    normal_v2_ref: (64, 2, 1, identity, True),
    e_checkpoint_ref: (132, 2, 2, identity, True),
    checkpoint_proof_ref: (130, 2, 2, e_checkpoint_ref, True)}


def acceptance_ok(value, meta, expected):
    tag = value[0] if value else 0
    if tag not in (1, 2) or len(value) != (172 if tag == 1 else 273):
        return False
    if value[1:71] != expected[0] + expected[1] + expected[2]:
        return False
    source = meta.get(value[71:172])
    if source is None or source != ((64, 2, 1, expected, True) if tag == 1
                                    else (132, 2, 2, expected, True)):
        return False
    return tag == 1 or meta.get(value[172:]) == (130, 2, 2, value[71:172], True)


assert acceptance_ok(normal_checkpoint_acceptance, metadata, identity)
assert acceptance_ok(emergency_checkpoint_acceptance, metadata, identity)
assert not acceptance_ok(b'\x01' + emergency_checkpoint_acceptance[1:172], metadata, identity)
assert not acceptance_ok(emergency_checkpoint_acceptance[:-101], metadata, identity)
wrong_mode = dict(metadata)
wrong_mode[e_checkpoint_ref] = (132, 2, 1, identity, True)
assert not acceptance_ok(emergency_checkpoint_acceptance, wrong_mode, identity)
wrong_target = dict(metadata)
wrong_target[checkpoint_proof_ref] = (130, 2, 2, normal_v2_ref, True)
assert not acceptance_ok(emergency_checkpoint_acceptance, wrong_target, identity)
wrong_cut = identity[:-1] + (position,)
assert not acceptance_ok(emergency_checkpoint_acceptance, metadata, wrong_cut)
wrong_kind = dict(metadata)
wrong_kind[normal_v2_ref] = (49, 2, 1, identity, True)
assert not acceptance_ok(normal_checkpoint_acceptance, wrong_kind, identity)
wrong_e_kind = dict(metadata)
wrong_e_kind[e_checkpoint_ref] = (64, 2, 2, identity, True)
assert not acceptance_ok(emergency_checkpoint_acceptance, wrong_e_kind, identity)
wrong_n_mode = dict(metadata)
wrong_n_mode[normal_v2_ref] = (64, 2, 2, identity, True)
assert not acceptance_ok(normal_checkpoint_acceptance, wrong_n_mode, identity)
# Historic E coverage remains tag 2 after exit; current normal retention still
# needs its normal pair and complete durable obligations/publication.


def can_retain(mode, acceptance, pair, obligations, published):
    return (mode in (1, 2) and acceptance_ok(acceptance, metadata, identity)
            and (mode == 2 or pair) and obligations and published)


assert can_retain(1, emergency_checkpoint_acceptance, True, True, True)
assert can_retain(2, emergency_checkpoint_acceptance, False, True, True)
assert not can_retain(1, emergency_checkpoint_acceptance, False, True, True)
assert not can_retain(1, emergency_checkpoint_acceptance, True, False, True)
assert not can_retain(1, emergency_checkpoint_acceptance, True, True, False)
for name, value in [('normal_authority', normal_authority),
                    ('emergency_object', emergency_object),
                    ('emergency_authority', emergency_authority),
                    ('normal_ref', normal_ref), ('normal_v2_ref', normal_v2_ref),
                    ('emergency_ref', emergency_ref),
                    ('old_id', old_id), ('new_id', new_id), ('later_id', later_id),
                    ('e_commit', commit), ('e_protect', protect),
                    ('e_abort_candidate', decision), ('class3_document', class3_document),
                    ('legacy_binding', legacy_binding), ('lifted_binding', lifted_binding),
                    ('legacy_decision_ref', legacy_decision_ref),
                    ('lifted_decision_ref', lifted_decision_ref),
                    ('legacy_result', legacy_result), ('lifted_result', lifted_result),
                    ('successor_descriptor', descriptor), ('prospective_ref', prospective_ref),
                    ('e_checkpoint', checkpoint), ('e_checkpoint_protect', checkpoint_protect),
                    ('normal_checkpoint_acceptance', normal_checkpoint_acceptance),
                    ('emergency_checkpoint_acceptance', emergency_checkpoint_acceptance)]:
    print(name, len(value), value.hex(), hashlib.sha256(value).hexdigest())
```

Legacy vectors additionally pin exact wrapper reversibility and unchanged
original result positions/visibility. The class-3 fixture checks its semantic
digest using the original internal domain; the table lists SHA-256 of its exact
wrapper bytes, which is deliberately **not** that semantic request digest.

The construction check includes every native request/attestation coverage edge
between pre-Plan history, Plan/Approval/INTENT, launch evidence, authority, new
FENCE, Selection and activation. It hashes nodes in construction order and
rejects the former downstream-authority cycle and a new-FENCE forward dependency.
Those edge fixtures are not complete native wire artifacts/signatures. Separate
checkpoint fragments check the closed sum, exact target/mode/kind/identity/cut,
missing protection and preservation of historical E coverage in normal mode;
actual sync/normal protection and complete replica coverage remain runtime gates.

The independently pinned output lengths/digests below supplement the exact byte
recipe; changing a schema requires changing its version rather than regenerating
accepted old bytes.

| Vector | Bytes | SHA-256 of exact bytes |
|---|---:|---|
| `normal_authority` | 181 | `cb1292eca51ee82e3f5caccdb2b188d56f7c78dcb0d03c9ba170b74cf641c5b8` |
| `emergency_object` | 637 | `d3e0c5b50af57b0713658c1bd2a1aaee8ac432c54c17c00398318aac4fb1b672` |
| `emergency_authority` | 55 | `dfe495631759228330ebd2439b03cfc2c45fc13ba33988fd52a70621560376fe` |
| `normal_ref` | 101 | `9f59fb90ca9e1f100615f9f4dc1f35641c6da8cd4cd88ca704ee2c1abd37f5d4` |
| `normal_v2_ref` | 101 | `913316a8bcdd1071b5a05da120b95cc893c26a3ab6bf44c0fa1393ee28c7a1e1` |
| `emergency_ref` | 101 | `da6f1851596e045afd3314ca2169fed291e02839e4d028f6af43b69db265e394` |
| `old_id` | 62 | `c62ce780e0499fa5d56bbe50703a191b830bfa59120edecebb198966e49dedbc` |
| `new_id` | 102 | `d3713103c4ed5841a2ddeaab15613d8cdcea5473349bc4e035425bf9af36832a` |
| `later_id` | 102 | `1e364413c376b631f60e890e5087d645d0a4707af7415f373d180ae49d615d91` |
| `e_commit` | 411 | `7af638a5a3a07616f7f9c49f9a05f5338a14b6b687a1ffe01b227e5e679a85d0` |
| `e_protect` | 635 | `adf7b57f8d28e0a3a8c946cbddaaec8dbde2c7695f4dfbb56bf228ccf48d25e8` |
| `e_abort_candidate` | 561 | `150ed05ef7350225854c89fe44f1dac7bf0fafbb5f2992dd8c5aabdb0167180d` |
| `class3_document` | 176 | `68cf3aeff77031cd74ebd376534ae9da94239e06548fbe78ad7ed49ecb408ec4` |
| `legacy_binding` | 512 | `0a34c2e480ffad1aee6d2170b21c4ea14fb8ec120885bc006d8b99af9ec048b9` |
| `lifted_binding` | 514 | `6b439a026577ec2da31e1e13a1fb211cb37430de5e7ea4d21c348174f11504f1` |
| `legacy_decision_ref` | 224 | `974d8fe9f5865e68d52278fe97497b2636c860cd888d238a779b115a4a935ef5` |
| `lifted_decision_ref` | 227 | `79fab74d6d1402cf40bdd3c1d12d7272705a4279db5d33a05254b6e065e1161c` |
| `legacy_result` | 399 | `173a160b153ae50492cfdf557166fedb521269d02772046b27a8b49dbee76284` |
| `lifted_result` | 402 | `44ee4d37cdc35d25d5d830d1e22400a8d05ef3e72ee372115719b110074e2637` |
| `successor_descriptor` | 494 | `757776e2bf8d7746520808a8687caba07f38cad6ec70c7bc1bab3cbfb754489f` |
| `prospective_ref` | 34 | `99d2b812e2eb2d636ac72592a48dcb8e734e9b63da400736f5d560784315007f` |
| `e_checkpoint` | 348 | `faf84ec24759a67715d269f867751b5191ec2fd339b66fad7cd11013ef6eeb66` |
| `e_checkpoint_protect` | 521 | `56e9bcedb0b83f86b886351d913fc148f81fda4af3319af41c7ce7ba0c521966` |
| `normal_checkpoint_acceptance` | 172 | `cda5985996d79a0091fc85522a35ae8eea98cff9379b04a553fa3a3ddf28a865` |
| `emergency_checkpoint_acceptance` | 273 | `e648bb4cfe7ed3bfda9f10fb08cac5cd05cd3cd2761fdaa5484eaa8e62579675` |

Required additional parser/graph/compatibility cases for `rw3`:

| Invalid input/window | Required outcome |
|---|---|
| Object length 23 or >4 MiB; frame >8 MiB; list cap+1; nested graph >64 MiB/depth128; integer overflow | Reject before allocation; preserve input. |
| Valid CRC but unknown 2.1, object/record kind, tag, feature bit, nonzero reserved or trailing byte | Unsupported/corrupt, no fallback decoder or skipping. |
| ARef NORMAL with self/missing partner; mode NORMAL containing E_COMMIT; mode E containing ACCEPT/RECOVERY_SELECT | Reject; no fabricated normal evidence. |
| v1 RecordRef marked E; changed mode at same stream key; hash/creator/header/ref disagreement | Reject mode confusion/corruption, retain exact old bytes. |
| Native form-2 successor hashes downstream EmergencyAuthority, LaunchEvidence.fence names a new successor FENCE, descriptor/Plan/actual-authority mismatch or unknown coverage codec | Reject forward/cyclic evidence or unsupported form before launch/activation; retain guards and old proofs. |
| E_PROTECT before target/dependency/own sync; missing payload, prepare, home, anchor or graph cycle | No protection/ACK/apply/serving; uncertain writes stay retained. |
| Old normal candidate lacks true acceptance/selection; survivor syncs it or creates E_IMPORT | Indeterminate or explicit excluded loss lineage; never chosen by emergency predicate. |
| LegacyLift with nonlegacy request/authority/ref tags, double lift of normal v2 evidence, class-3 wrapper or v2 domain hashed as old request digest | Reject comparison/digest; exact source bytes and source-version dispatch remain authoritative. |
| Existing normal/emergency ID changes binding/coordinator/redo/digest/outcome across entry/exit | Reject ID conflict/protocol corruption; preserve immutable old result. |
| Missing old request list; reused caller nonce; old tag1 unknown registration after loss; old tag2 serial/future registry | No path resolution/claim/execution; exact historical result or excluded indeterminate only. |
| Singleton CAS races, registry snapshot rollback, frontier missing, namespace serial exhaustion | One exact successor or gated; no fresh designation/namespace. |
| Expired/wrong-action/replayed approval, valid transport principal, status-only admin, arbitrary Artifact | No action; signed typed approval/audit privilege and exact scope required. |
| Replayed old grant/new incarnation, recovery-only normal ACK, stale revoked session/third node automatic rejoin | Deny before data acceptance/publication; bars survive exit/restart. |
| Checkpoint acceptance wrong kind/mode/checkpoint/closure/vector, absent/mismatched E_PROTECT, tag2 proof relabelled NORMAL or historical E source used to bypass current normal pair | Reject publication/retention; preserve each source's actual proof and enforce complete pair/GC coverage. |
| Orphan checkpoint acceptance, partial CURRENT/migration, archive missing key/retrieval/hold, one participant trim | Preserve predecessor/local history, block retention; stop writes before reserves. |
| One final receipt, receipt for wrong B/incarnation, partial MODE_INSTALL/EXIT_COMMITTED | Keep gates closed; no fallback to E protection after NORMAL_REQUIRED. |
| v1-only reader/receiver/manifest/archive/snapshot/control/bootstrap/client against v2 unit | Explicit unsupported version; no serving, normal ACK, downgrade, repair or format-on-open. |

The local generator checks bytes and specified predicate fragments only. It does
not establish broker/launcher continuity, real sync, request service integration,
cache fencing, pair durability or any Emergency scenario. No runtime, infrastructure
or model evidence is claimed by that generator. The separate
[bounded Emergency model](../../spec/tla/README.md#emergency-activation-and-exit-nx3)
checks abstract singleton/frontier and exit predicates; it does not verify this
byte schema or a broker implementation. Existing `native-vm-stop-v1` limits and
per-action/per-window approval requirements remain unchanged.
