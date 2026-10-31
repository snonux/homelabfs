# All-origin normal format 3.0

Status: representation foundation (`ez3`), 2026-10-31. This is **not** a
distributed filesystem, serving grant, upgrade controller, or implementation of
the [normal protocol](Replication-protocol.md). Normal publication remains
closed. Current transport sessions, local fixture configuration, backing apply,
and admin server remain at their existing versions and behavior.

Version 3.0 has its own magic and namespace. Journal/protocol/control/recovery
2.0 and `HFS2OBJ` remain reserved for the exact
[emergency format](Emergency-format.md). Nothing here reinterprets its records,
goldens, protection tags or loss namespaces. A normal3 reader rejects v1/E2
bytes; the existing v1 reader rejects normal3 bytes. No silent legacy lift,
mixed-version negotiation, in-place rewrite or downgrade exists. `rr3` owns
whole-unit upgrade activation and `rw3` owns emergency composition.

## Canonical field encoding

`internal/journal/record/normal_types.go` is the ordered field schema: declaration
order is byte order, including grouped fields from left to right. The following
rules fully define its byte encoding; no Go layout, reflection metadata, JSON or
host byte order occurs in journal bytes.

- Unsigned integers use their declared fixed width, big endian. Signed seconds
  use signed 64-bit two's complement. `Time` is seconds:i64 then nanos:u32,
  nanos below 1,000,000,000. UUIDs are nonzero raw 16 bytes. Hashes are raw 32
  bytes; an all-zero hash is a value, never an absence marker.
- Structs concatenate fields. Pointers use present:u8 (exactly 0 or 1), then the
  value when present. Booleans use u8 exactly 0 or 1.
- Arrays have count:u32 then elements; byte/string fields have length:u32 then
  exact bytes. Hard maxima and sorted-set rules are the `wire` annotations in
  that schema. Before allocation the existing bounded canonical codec checks
  enclosing bytes and a 64 MiB cumulative allocation budget. Complete frame
  bytes are limited to 8,388,608; payload/blob limits still apply independently.
- Sorted sets reject equal keys, even with equal contents. UUID sets sort raw
  UUID bytes. `NormalRef` keys are complete `OriginStream` then local sequence;
  the hash, creator and kind are values at that key, never extra duplicate keys.
  Volume/cut sets key by volume; coverage keys by stream then holder launch;
  graph nodes key by frame ref; result observations key by grant ref; frontiers
  key by stream; close/equivalent reports key by origin/missing launch.
- `RequestDocument`, `Intent`, `Mutation`, credentials, metadata, redo and
  operation arguments reuse their exact existing canonical field schemas and
  validation. A new normal grant requires the old `Mutation.Dependencies`
  scalar vector to be empty; its full normal refs carry dependencies instead.
  No v1 `Commit`, `Accept` or `Authority` rule is relaxed. The immutable internal
  document's intent must equal the grant mutation's intent, selected per-home
  metadata must equal redo, and numeric credentials must match authorizations.

This first grant schema deliberately admits only internal ordinary single-home
documents. Public transaction documents and foreign-home object inputs return
`Unsupported`; clearing the legacy scalar vector does not make transaction
evidence normal3-safe. No new2PC union or implicit legacy/E2 source lift exists.
All such operations remain unavailable through this foundation.

## Envelope and creator identity

Every normal record has a 24-byte envelope, body and CRC32C:u32 over envelope
and body (Castagnoli, same checksum convention as v1):

| Offset | Bytes | Value |
| --- | ---: | --- |
| 0 | 8 | `HFSNREC` followed by NUL |
| 8 | 2 | Major 3 |
| 10 | 2 | Minor 0 |
| 12 | 2 | Closed record kind |
| 14 | 2 | Reserved zero |
| 16 | 4 | Complete length, including checksum |
| 20 | 4 | Body length; complete length = body length + 28 |
| 24 | body length | Creator prefix then typed payload |
| end-4 | 4 | CRC32C |

The exact 172-byte creator prefix is `OriginStream || sequence:u64 ||
operation:uuid || Creator:Launch || observed:Time`.
`OriginStream = cluster || volume || generation || configuration || epoch:u64 ||
origin || journal-incarnation`, all other fields UUIDs. `Launch = node ||
daemon-incarnation`. Sequence is positive, contiguous and nonwrapping; structural
frames consume it. The creator's node equals the stream origin. Journal lifetime
and daemon lifetime are distinct. Reopening a source preserves every old creator
byte; writing under a later daemon lifetime requires external reconciliation and
does not change old refs.

`NormalRef = OriginStream || sequence:u64 || Creator:Launch || kind:u16 ||
sha256:bytes[32]`. The hash covers the complete original frame, including CRC.
A historical transfer never replaces Creator with the forwarding daemon.

The closed record kinds are:

| Tag | Payload |
| ---: | --- |
| 1 | `OriginOpen` |
| 2 | `OriginSeal` |
| 3 | `NormalGrant` |
| 4 | `OriginChoice` |
| 5 | `MemoryAcceptance` |
| 6 | `SlotSelection` |
| 7 | `NormalResult` |
| 8 | `NormalBarrier` |
| 9 | `NormalFrontier` |
| 10 | `NormalRecovery` |
| 11 | `OriginFragment` |
| 12 | `OriginCloseReport` |
| 13 | `TransitionPromise` |

Unknown magic/version/kind, reserved fields, impossible lengths, invalid
identities, malformed sets and complete CRC failures stop decoding. A short
bounded frame is `Torn`, an observation requiring storage context; it is never
permission to truncate or infer absence.

Installed `NormalAuthority` has a separate object envelope with magic `HFSNOBJ`
plus NUL, version3.0 and object kind1, the same length/CRC layout, and its exact
canonical authority payload. The authority hash in a slot is SHA-256 of that
payload (excluding this object envelope). This definition avoids envelope and
certificate hash cycles.

## Order, origin durability and protection

Authority records complete admitted S separately from per-volume placement R,
immutable registry/recovery/closure identities, per-volume history, sequencer A,
owner-origin partner B and positive-fence lineage hashes. Every replica is
admitted; A and B are distinct admitted replicas. Any admitted O can use any
member volume, including O outside R. Static registry `CheckOrigin` checks this
eligibility without creating a serving grant or altering `CheckAssignment`.

An ordered slot is `authority-payload-hash || sequencer:Launch || serial:u64`.
It is not a journal offset. A `SelectedRef` binds that slot to its exact selection
certificate. Serial1 has no predecessor and an explicit baseline hash; a later
grant names the exact immediately preceding selected certificate under the same
authority and sequencer lifetime. A grant contains the full installed authority,
origin stream/launch, immutable document/credentials/redo, operation UUID,
selected commit output and explicit no-effect output, dependencies and prior
per-volume cuts. The creator of a grant is A, in A's origin stream for the same
volume history. M=A for O!=A, and M=B for O=A. Self counting and substituting B
for A on a remote-origin request are rejected.

O's `OriginFragment` stores the **complete original grant frame bytes**, including
payload, request and creator A. Its outer frame is O's own durable source frame.
Only a grant may be enclosed; nested fragments are rejected before recursive
decode. This composes the already canonical operation representation with
distributed origin fragments and a separate total order; it does not mandate
copying all historical payloads to every new origin.

`OriginChoice.Choice` is COMMIT=1 or DECLINE=2. Only COMMIT has its exact prepared
acceptance ref. The choice names its grant, slot and complete local fragment
refs. Missing choice, source, response or result never means DECLINE.
Acceptance phases are PREPARED=1, CHOICE=2, RESULT=3 and bind grant/slot/O/M and
target ref. Prepared acceptance asserts no durable choice; later phases bind
the exact durable O prefix through their target. This is assertion encoding,
not proof that bytes were synced or RAM still exists after an unknown crash.

Selection outcomes are COMMIT=1 and NO_EFFECT=2. The graph verifier requires the
matching original COMMIT/DECLINE choice, exact phase2 retention evidence and
predecessor. Selection lists affected volume IDs; the resulting cuts are the
new certificate ref, avoiding a self-hashing certificate. Result binds that
exact selection to O's stable output; commit and no-effect results must match
their distinct values fixed in the grant. Result observations distinguish
PENDING=1, INDETERMINATE=2 and TERMINAL=3; only terminal has a result ref. A
missing result beside an intact COMMIT remains unknown, never no effects.

## Sources, recovery and local storage seam

`SourceCoverage` binds actual durable-prefix stream/holder identities and exact
last-frame ref, separately from memory acceptance. `NormalFrontier` separates
received/durable source offsets, selected/visible/backing-installed/checkpoint
cuts, durable sources and retained acceptances. Unknown cuts are explicit
absences. Numeric origin maxima never stand in for global selected history.

Recovery objects retain complete authority S, issued-grant frontier, exact
selected certificate, source coverage, dependency DAG, unresolved grants,
result observations, origin close reports and named verified equivalents.
Close-report states distinguish UNKNOWN=1, SEALED_EMPTY=2 and COMPLETE=3, with
issuer launch, transition/predecessor promise, complete issued frontier,
unresolved slots, received/durable/selected/retained state, continuity/lost RAM,
and source/pin obligations. Equivalent evidence identifies the missing exact
report/lifetime, positive exclusion, complete issued frontier and replacement
sources/obligations. These are evidence fields, not installed recovery decisions.

`VerifyNormalGraph` requires actual creator bytes at every listed graph ref,
rejects absent/cyclic graph edges and omitted typed dependencies, and checks
grant/choice/recipient/selection/result identity relationships. Passing it proves
only bounded byte-graph consistency. It does not establish sync, retention,
complete admitted-node reports, transition-promise monotonicity against external
history, positive exclusion, publication, checkpoint coverage or safe GC.
Recovery/source-pin/checkpoint integration remains gated downstream.

The exact successor2PC phase/grant/source schemas follow the `hz3` transaction
contract in `8w3` transaction API/shared-home/closure integration, including its
transaction-specific checkpoint/GC extensions. General retention/checkpoint/
resync and source-release lifecycle belong to `8r3`; snapshot/barrier/restore
belongs to `or3`, and canary backup to `nv3`. `rw3` must retain exact emergency
protection/loss namespaces when composing sources, and `rr3` must define explicit
legacy-reader/whole-unit upgrade and source import gates. These tasks may require
additional versioned records rather than casting an existing ref or silently
extending one of the closed3.0 kinds. `4r3` supplies ordinary protocol dispatch,
sync/retention assertions and publication integration; none is active here.

The bounded `segment.OriginSource` seam uses exclusive immutable files at
`origin-v3/<cluster>/<volume>/<generation>/<configuration>/e<epoch20>/
<origin>/<journal-incarnation>/s<sequence20>.hfsn`. It starts at1, checks exact
contiguity and creator bytes, syncs each file and its parent before successful
append, and verifies/resyncs the entire bounded prefix on restart. Identical
delivery is idempotent; changed bytes at one key fail. Limits default and cap
at4096 frames/64MiB per source instance. Private synced parents and exclusive
external access are preconditions. A failed/partial append is preserved,
poisons that writer, and blocks reopen; no automatic repair, deletion, rotation,
retention or production path is implemented. Existing v1 segment files and
readers remain untouched.

## Transport and observation seams

`HFSNCAP` plus NUL, then journal/protocol/control/recovery:u16 each exactly3,
then features:u64 exactly1 forms the exact24-byte normal capability descriptor.
Any missing/unknown version or feature fails. This is an indivisible future
upgrade binding; no current Endpoint advertises, negotiates or installs it.

The opt-in normal transport representation uses the existing144-byte envelope
field layout with protocol3.0 and exact normal creator bytes as its body.
DATA uses live/bulk classes; ACCEPTED/PROGRESS/STATUS_REPLY use control. Other
tags are rejected by this typed-frame seam. Its source/source-incarnation remain
the authenticated forwarder identities; original creator refs stay in the body.
Actual socket authentication, current-incarnation/authority checks, ordered
protocol dispatch and full graph retention remain mandatory runtime gates.

The separate representation-only admin observation has the exact canonical
JSON fields `version:3`, `normal_publication:false`, `frontier:<base64 complete
normal frontier record>` in that order. It has the existing16KiB admin bound and
rejects duplicates, aliases, nulls, unknown keys, trailing values and alternate
noncanonical spellings. It is not emitted by the current admin1.0 server.

## Independent literal vector

`internal/journal/record/testdata/normal3_open.hex` is a225-byte literal produced
independently with Python `struct.pack` and a bitwise CRC32C, without calling the
Go codec. SHA-256 is
`fda4d0f976044fa244467057cd6f40b1cb2bdf4607ebb8a7c77374a46b01777d`.
It uses UUID byte repetitions1..9: cluster1, volume2, generation3,
configuration4, origin5, journal6, operation7, creator incarnation8 and segment9;
epoch7, sequence1, observed time(-1,123456789), absent predecessor and maximum1024.
Its generator is the following pure byte construction:

```python
import struct
u = lambda n: bytes([n]) * 16
stream = u(1)+u(2)+u(3)+u(4)+struct.pack('>Q',7)+u(5)+u(6)
creator = stream+struct.pack('>Q',1)+u(7)+u(5)+u(8)+struct.pack('>qI',-1,123456789)
body = creator+u(9)+b'\0'+struct.pack('>Q',1024)
frame = struct.pack('>8sHHHHII',b'HFSNREC\0',3,0,1,0,28+len(body),len(body))+body
crc = 0xffffffff
for byte in frame:
    crc ^= byte
    for bit in range(8):
        crc = (crc >> 1) ^ (0x82f63b78 if crc & 1 else 0)
frame += struct.pack('>I',crc ^ 0xffffffff)
print(frame.hex())
```
