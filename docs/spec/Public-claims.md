# Ordinary public bindings and durable claim ledger

Status: `g04` representation/local persistence foundation. Distributed source
retention, all-origin coordination, transaction integration and public serving
remain unimplemented. `internal/normalpublic` supplies no guestbridge provider,
deployment profile, mount, authority installer or publication capability.
Normal3 3.0's INTERNAL ordinary document union and all version-one contracts
remain unchanged. This format is ordinary single-volume work only; public
transaction/home-closure graphs belong to separate work.

The independent public format is **1.0**, magic `HFSPUB01`. Its version is not
the retained bridge version or normal3 version. Negotiation requires exact
major/minor equality. No existing transport advertises support automatically.
The 16-byte envelope is magic (8), big-endian major/minor (2 each), canonical
body length (4), then canonical JSON and a SHA-256 of header plus body (32).
Every field is present, with Go declaration spelling/order, byte slices encoded
as base64, UUIDs and hashes as numeric arrays, and absent pointer/slice values as
JSON null. Readers bound lengths before decoding and require exact re-encoding;
unknown/duplicate fields, alternate whitespace, trailing data and versions fail.
Maximum complete frame is 8 MiB; this checksum is not an authentication proof.

`Binding` keeps the complete anchored Realm/Subject/Anchor/Nonce/Number, exact
canonical original Intent bytes and digest, numeric VFS credentials/full groups,
process/namespace/map/revision and attestation provenance bytes, original export,
session, policy/registry/authority bindings, immutable operation UUID and original
O launch/journal/history. Authority reuses the existing canonical normal3 authority
type/hash, without putting a public request into a normal3 grant. Membership
validation accepts O outside replica placement when O belongs to admitted S.
Intent/provenance bytes have mandatory external closed-schema and semantic
validation; successful byte decoding does not establish their authenticity.

Public source frames form a closed separate union: original Binding (O),
COMMIT/DECLINE Choice (O), ordered Selection (A), and immutable Result (O).
Every reference includes full stream, sequence, creator, kind and exact complete
frame digest. Selection binds the exact authority, sequencer launch, serial and
previous selection; serial one requires an explicit baseline. Graph verification
checks actual creator bytes, complete transitive references, creator/operation/O,
history/authority and ordered predecessor links, conflicting original claims,
operations, source positions, choices and selections. It rejects missing,
duplicated and extraneous public sources. Public dependencies stay within one
volume. Existing normal3 dependencies are separately named in `Binding.Sources`;
their actual bytes and complete `NormalRecovery` graph are checked through the
unchanged `record.VerifyNormalGraph`. This does not invent public normal3 grants
or interpret any source maximum as complete coverage.

The local ledger has mandatory independent Authentication, Admission and
Retention interfaces, plus a permanent enrolled `LedgerID`. There are no default
providers. `Resolve` authenticates anchor access before disclosing any old binding.
The consumer must resolve a known claim before fresh capability/path lookup or
choosing O; a known exact `Reserve` retry returns existing state without fresh
admission. Changes to original credentials, intent, scope, operation or O conflict.
Authenticated forwarding may resolve another current scope to the original
binding, but cannot replace its original context or execute old capabilities as
a fresh operation. No adapter integrating this into normal execution is supplied.

Allocation is an explicit durable transition requiring authenticated serialized
anchor allocation evidence for a contiguous list of exact number/original-intent
digest pairs. The original digest is immutable even before admission, so an
unsent or rejected number cannot later identify a different intent. Every
allocated number starts UNSENT, including
requests never sent or rejected before reservation. Reserve changes only an
allocated UNSENT number into PENDING, after admission and operation collision
checks. Allocation cannot reset, reuse numbers or jump over unrecorded allocated
holes. Every number is retained and charged against a finite limit.

PENDING can become INDETERMINATE or CANCEL_REQUESTED. Cancellation records a
request/detached wait, never absence or NO_EFFECT. An unknown or unallocated
claim stays UNKNOWN; its cancellation fails without creating a record. Only an
exact verified protected result graph makes an admitted claim TERMINAL, including
a selected durable DECLINE. A different terminal graph/result conflicts. A gap
can become PROTECTED_GAP only from UNSENT with positive authenticated serialized
no-effect proof. An admitted uncertain claim cannot become a gap.

Retirement traverses every number from old floor+1 through the requested floor,
at most 1024 per transition. Each must have a complete protected terminal result
or protected no-effect gap. Pending/unknown/unsent entries block. The verifier
receives the exact entire contiguous entry/result/source graph set, previous
floor and supplied evidence; authenticated anchor serialization and permanent
floor protection must be established independently. The floor is permanent and
cannot decrease. Results, gaps and all original operation identities remain
retained after retirement in this version. No timer, timeout, client reply,
missing file, selected-source maximum, clock or TTL permits removal or reuse.
Checkpoint/source-release compaction requires later verified replacement work.

The external Retention provider must establish all facts codec success cannot:
installed authority and exact original-origin choice/result; original O's own
contiguous durable complete fragment; exact older durable dependencies and pins;
distinct M=A for remote O or M=B for owner O with complete retained bytes;
selected outcome and all required visibility barriers. `VerifyRecovery` receives
the enrolled ledger ID and complete journal bytes on every open, including empty
files. It must verify complete historical ownership/floor/result/source coverage
and continuity or exclusion. A new path, truncated complete prefix, new daemon
UUID or empty journal is never evidence that previous allocations did not exist.
These are mandatory provider contracts, with no shipped normal proof provider.

Local persistence uses separate `HFSPLOG1` 1.0 envelopes with ledger ID,
contiguous event number, preceding complete-entry hash and exact transition
bytes. A private 0600, same-owner, single-link regular file is opened without
following its final symlink and exclusively locked. Initial file and parent
directory are synced; each complete transition is appended and synced before
in-memory publication. Failed writes/syncs poison that handle, requiring reopen
and verified recovery; no uncertain error becomes a rejection/no-effect result.
Reopen validates canonical envelopes, chain, transitions and actual source graphs
before the external recovery gate. Torn/corrupt tails block and remain intact;
there is no truncation/repair fallback. Parent directories must be privately owned
and controlled by the caller. Local sync does not prove this file is O's own
journal or supply the distributed durability/retention facts above.

Explicit limits allow 1..4096 anchors, entries and source frames, with a journal
budget of 48 bytes..256 MiB. The combined public/normal source graph is at most
8 MiB. Intent/result bytes are at most 1 MiB, credentials have at most 1024
sorted unique supplementary groups and 64 KiB provenance; subject is 1..1024
bytes. Exhaustion blocks rather than discarding an unresolved/retired identity.
The entire bounded journal is read on recovery; per-entry envelopes remain
bounded separately. The component prioritizes inspectable permanence over
compaction or high-throughput concurrency.

Tests use actual temporary journals/source bytes, exclusive-writer contention,
concurrent duplicate reservations, a child process exiting without Close followed
by reopen, poisoned append failure, immutable retry/result/floor behavior,
missing gaps/results, canonical/version/size refusals and exact normal/public
source graph tampering. Fixture proof providers test mandatory interface wiring
only; they are not retention, no-effect or platform proofs. Process exit and
fsync tests do not establish power-loss behavior, guest/kernel cache barriers,
distributed failover, source-release safety or public runtime acceptance.
