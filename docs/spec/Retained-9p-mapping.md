# Retained-9p authenticated original mapping 1.4

Task `114` implements the host foundation for an explicit controlled-local
successor. Task `y04` adds the owned C carrier and an inert guest receipt
verification primitive. A kernel received-response provider (`s04`), restoration
and distributed operation remain unimplemented. The pure verifier below cannot create kernel receipt
or source authority. Production workloads, PVs and NFS remain outside scope.

Both legs must select `9P2000.L.hfs1.4`, frame major1/minor4 and the exact tuple
`{body_schema:5,bridge_major:1,bridge_minor:4,features:16383,msize:65536,
normal_major:3,normal_minor:0}`. Bit13 selects the complete original/native
binding contract. All cleanup1.3 requirements, including the actual parent-funded
READDIR closure, remain mandatory. Closed1.0/1.1/1.2/1.3 envelopes and receipt1
are unchanged. A selected tuple cannot change after signed enrollment.

## Command and inverse transformation

Every1.4 command has exactly `{cell,context,delegation,op,payload,protocol,reclaim}`.
The guest original requires `delegation:null`. The actual owned backend must
reject guest non-null delegation before issuing any native cell, and attach this
closed object on **every actual native leg**, including each WAIT poll:

```
{version:"homelabfs-native-delegation/1",guest_cell,guest_op,
 guest_reclaim,poll_index}
```

`guest_cell` is1..MaxCell; `guest_reclaim` is the original full Ack or null;
`poll_index` is0..15. No original body or caller-supplied command hash is added.
The native command is otherwise the exact original scope, protocol, payload
and complete credentials. The inverse whitelist restores cell, op and reclaim,
sets delegation null, and restores the mirrored RELEASE_CELL Ack payload.
`guest_op` must equal native op, except VIEW_WAIT maps to native VIEW_POLL.
Only VIEW_WAIT may use an index greater than0. VIEW_WAIT has the VIEW_POLL
payload schema and cannot execute directly on the host.

The host hashes this exact canonical inverse, including delegation null, as G.
Hashing borrows the existing payload bytes; it retains no second original body.
Metadata is only a candidate until actual authenticated Execute observes the
same native command through the private transport worker context. A supplied
Native value, decoded delegation, detached observation or matching hash cannot
register a mapping or bypass that private candidate.

## Actual protected ownership and order

1.4 requires `NewDurableTransport`; the memory-only constructor refuses it.
Bridge construction first binds the actual producer/providers/controller/native
objects. The durable transport then attaches itself to that same configured
producer after its initial ledger commit and before workers become usable.
Concurrent unbound or direct delegated Execute calls refuse. An uncertain
attachment retains the service's nonreusable transport claim.

The mapping belongs to the existing C8 cell credit and protected row. Its key is
actual endpoint/controller/full scope/guest cell. It retains original G/op/Ack,
current native cell/command hash/index, response base/full hashes, rolling chain,
and at most16 compact own-leg records. A first-leg donor snapshot retains only
its guest/native cell, original hash, chain commitment and full response hashes;
it is nonrecursive and does not copy the donor's leg array.

Full command acceptance checks the actual retained donor before deleting it:
same scope, original guest cell and full translated guest response hash must map
to that donor's actual native cell and full native response hash. RELEASE_CELL's
native and original payloads must exactly mirror their respective reclaim Acks.
Existing transport read coverage and credit-class checks still apply. Candidate,
freshness index and donor deletion commit in the **same C8 transaction** before
input ACK or dispatch; there is no claimed cross-ledger atomicity.

Execute creates the existing private authenticated request observation and admits
that exact protected mapping before producer preparation or service/controller
effects. The private worker token and matching actual observation are both
required. After dispatch, the host protects the actual response-base hashes and
chain before receipt signing. The worker protects immutable native response,
translated full response hash, frames and coverage before exposure or reclaim.
Failures retain/fence uncertain ownership. Inert durable inspection does not
restore a controller, authenticate recorded callbacks, or authorize RESUME.

Native cell retries require the exact original canonical bytes. Another native
cell cannot repeat the same guest key/index. A WAIT continuation requires the
same G, exactly prior index+1, an increasing native cell (not necessarily an
adjacent one), the prior actual `{event:null,status:EMPTY}` response, and its
exact full native Ack. EVENT closes polling. At most16 actual legs are admitted.
An authenticated new guest command may instead reclaim an EMPTY donor with the
exact full native/translated hashes, atomically closing that old WAIT chain. This
permits an actual backend deadline at any index without guessing host time or
claiming the guest observed an internal EMPTY. The removed chain cannot later
continue or resurrect. EMPTY itself grants no source/event/floor authority.

Independent control and ordinary workers may execute accepted requests out of
order. Each native cell/workspace captures the scope's guest highwater on its
first accepted header. A fresh original must exceed that captured watermark,
not a later worker's watermark, **or** be an unissued hole of the scope's
reorder window. The exporter used to forward one session's guest cells out of
guest order (zq3-r6 R6-2: VIEW_WAIT guest cell 48 arrived as native 108, after
QUERY guest cell 49 as native 107), so raising the highwater past skipped cells
keeps those inside a 16-cell window below it as holes. A hole is admitted at most
once (admission removes it). A hole that falls out of the window is not admitted
as a hole (a cell can still enter above an older captured watermark, which is
safe). The 16-cell window is a stopgap, not derived from an exporter bound.
Since task e64 the exporter allocates native numbers to one session's originals
in guest order and waits up to one second for a lower cell that is missing or
incomplete. Only a cell it gave up on after that hold, or a complete ordinary
cell that later traffic bypassed because it could not be written (no credit,
EAGAIN on the first chunk of a multi-chunk cell, or an error before its write),
can still arrive below the highwater. A cell is refused as stale once any cell 16 or more guest numbers
above it was admitted (a distance rule). Since task p64 the exporter fails such
a refused cell back to the guest instead of keeping it as its pinned writer.
Because the runtime allocates a non-donor cell's native number at its first
chunk, the exporter fails a non-donor cell back only
when the refusal also shows that the number was spent (ESTALE on a first
attempt, or EINVAL after an accepted earlier chunk) and then treats that number
as spent. Since task w64 any fail-back (donor or not) needs a refusal of the
final chunk, where the runtime validates the complete cell; a refusal of an
earlier chunk comes before validation, leaves the accepted chunks in the
runtime, and keeps the candidate pinned for a replay from the first chunk. Since task n64 the runtime matches that: a complete cell refused
Stale by validation (except a SESSION_RETIRE), or Invalid on a chunk after the
first, is removed with its credit, so it no longer counts as incomplete or
pins markers (see the marker rule below). Its number stays spent, and the C8
index keeps the latest such refusal as `Refused` (omitted until one happens)
so that a resend gets the same errno. A donor send refused Stale, Invalid or
Permission leaves the reclaim workspace free (not a SESSION_RETIRE or a
continuation poll, which the exporter pins). An authentication ESTALE or
credential EACCES of the final chunk comes before validation, yet a first
attempt still fails the cell back. Since task x64 the runtime then replaces
the abandoned, never-completed cell when the exporter's single writer sends a
different frame at the next native number: an incomplete numbered cell at the
high-water is removed with its credit (its number stays spent), and an
incomplete workspace holding another frame is freed (its number stays
unallocated). Identical replays still continue the kept chunks, and a complete
(validated) cell is never replaced. The window is now a bounded fallback whose size
can be revisited once the rebuilt exporter has run (see the [lib9p README](../../tools/integration/lib9p/README.md)).
This bounds retained state and is not evidence that the guest retired
it. A SESSION_RETIRE drops its scope's holes. The C8 index protects holes as
`GuestHoles` (omitted when empty, so an index without it means every cell
through the highwater is spent); restore and inspection validate them as sorted,
in-window, below the highwater and disjoint from markers and active rows.
Complete active rows and at most80 aggregate
recent guest issuance markers prevent duplicates, including reuse through an
older still-incomplete reservation after a donor was deleted. Markers are
freshness guards, never evidence that unseen holes retired. A marker is pruned
only when no older incomplete capture can reuse it. New headers reserve capacity
for `recent + incomplete <=80` before their first ACK; completion spends its
already owned slot. A permanently incomplete cell may eventually close fresh
admission. No marker is evicted to make room. Since task x64 the exporter's
sole writer leaves an incomplete cell only at the highwater, and the next
header replaces it as abandoned, so these rules guard ledgers written before
x64 and do not limit a single-writer exporter.

## Hashes and receipt2

All hashes are SHA256 of canonical JSON. Native command hashes bind actual
native bytes. Native response base excludes verification. Guest response base
excludes verification and restores original guest cell/op. Full hashes include
the actual receipt (or verification null), after the same scalar restoration on
the guest side. Full hashes fund exact reclaim, not the recursive signed chain.

The chain seed is the hash of
`{domain:"homelabfs-mapping-chain/1",endpoint,controller,scope,guest_cell,
guest_command_hash:G}`. Each actual leg updates it with the hash of
`{domain:"homelabfs-mapping-leg/1",previous,index,native_cell,
native_command_hash,native_response_hash}`. The last member is the actual
response **base** hash. Thus neither signature nor full response hashing refers
to itself. Retained own-leg records allow inert inspection to recompute it.

The distinct closed receipt2 is exactly receipt1's fields plus
`{guest_cell,guest_command_hash,guest_response_hash,poll_count,mapping_hash}`.
Its version is `homelabfs-controlled-verification/2`. Existing `cell`,
`command_hash`, `response_hash` continue to bind the actual final native leg;
the three guest fields bind the original canonical guest command and translated
response base. `poll_count` is1..16, and must equal1 except for VIEW_WAIT.
`mapping_hash` binds the complete protected current leg chain. The original
command hash includes the exact original donor Ack, not an inferred EMPTY trail.
The enrolled host key signs the canonical entire receipt with signature removed.
Receipt1 and receipt2 use separate structs and strict decoders.

`VerifyMappedReceipt` checks a caller-pinned enrolled key and exact scope,
endpoint, controller, serving generation, canonical guest original, translated
response, receipt shape/signature, operation and poll bounds. SOURCE requires a
view operation; EVENT requires VIEW_POLL/VIEW_WAIT and an actual EVENT-shaped
response; FLOOR requires RETIRE and exact original full credentials/family/
through/proof matching. Its result is an inert verified **host attestation**.
It does not independently observe the native legs, validate source bytes or
create a KernelReceived, installed object, event or retirement permission.

## View retirement receipt

Controlled1.4 receipt2 additionally admits `kind:VIEW_FLOOR`, only for the
already existing VIEW_RETIRE operation. Its exact evidence is
`{anchor:uuid,nonce:uuid,through:n64,proof_hash:hash,credentials:Credentials}`.
Anchor, nonce, through and the complete original credentials must equal the
actual original view retirement request; the response must be RETIRED with
`floor == through` and the same nonnull proof hash. Scope, endpoint, controller,
serving generation, original/native mapping, channel, signature and existing
receipt-size limits remain mandatory. Unknown fields refuse.

The producer verifies the actual controller's already protected exact
VIEW_RETIRE receipt before minting this kind. A decoded attestation cannot
create private guest receipt provenance or permit ticket destruction. The
provider additionally matches its still-owned contiguous family prefix and
borrowed complete transported response before the caller frees any ticket.
Ordinary FLOOR remains RETIRE-only and retains its realm/subject/full-original
rules. Receipt1 and protocols0..3 refuse VIEW_FLOOR. This uses the existing
controlled1.4/features16383 tuple; it does not select or implement the separate
[terminal-session extension](Retained-9p-session-retirement.md).

## Fixed bounds

No1MiB body, 4MiB canary, cell count, 8MiB protected-row, 128KiB index or existing
arena budget increases. Each mapping has a fixed16KiB scalar/hash allowance,
including16 own-leg records and its compact donor commitment. Original and
translated hashing borrow payload slices rather than retaining body copies.
The row bound is checked before constructing a1.4 owner: one1MiB raw command,
four base64 arenas each conservatively including32 frame headers,33 full native
observations each bounded by16KiB (1024 groups plus all scalar fields),32KiB of
fixed metadata and the16KiB mapping allowance fit below8MiB. The private ledger
encoder disables HTML escaping, including the Command marshaler, so a raw
`<>&` payload cannot expand sixfold. One encoder buffer is returned without a
second row clone; existing bounded ledger serialization/transaction ownership
funds it. A64-scope/80-marker maximal index remains below128KiB. Existing ledger
1GiB admission is unchanged; deployment sizing remains a separate canary gate.

## Owned C carrier and inert guest primitive (task y04)

The maintained lib9p backend selects the exact1.4 token/tuple. Its guest ingress
requires delegation:null before it prepares a native candidate. Every native
leg reconstructs the compact delegation from the immutable guest original;
internal WAIT Ack substitution never changes guest_reclaim. Candidate-number
selection does not advance the native allocator. The actual rewritten bytes and
existing32768-token arena are preflighted before issuance and complete-command
acknowledgement. Metadata, digit expansion and both mirrored RELEASE Ack fields
must fit the unchanged1MiB body. Refusal retains the original guest owner and
any allocated/uncertain request. Earlier0..3 admission allowances stay unchanged.
Replies restore only the guest cell/op and preserve the entire V2 receipt.

The maintained Linux guest command builder emits the mandatory null delegation
for1.4 while retaining the closed older encodings. The pure
`hfs_mapped_receipt_verify` API takes borrowed canonical original/response JSON,
an expected context and pinned key, and a required Ed25519 operation. It checks
host-attested bindings and receipt/kind policy, returning only inert attestation
scalars. Selected-field SHA256 uses fixed state and borrowed spans; it does not
clone the response body. The unsigned Ed25519 message is separately bounded to
16KiB. Kernel SHA256 uses the actual kernel implementation; portable tests use
real OpenSSL SHA256 and Ed25519 with the real enrolled Producer key.

This API does not enroll the supplied key, independently observe native legs,
verify source bytes, install a cache object or create KernelReceived/event/floor
authority. Missing Ed25519 support refuses with EOPNOTSUPP. No production kernel
Ed25519 adapter or private received-response provider is installed here. C6 must
obtain immutable key/context/receipt provenance from its private admitted state
and complete the existing source and physical validation paths. A caller JSON,
hash, boolean, parsed receipt or successful pure check cannot replace that work.
The C primitive does not reproduce every nested Go response check: C6 must
complete original acquire/renew comparisons and frozen-entry schema/request
validation before using a source. Its successful result remains an inert host
attestation.

The [portable carrier harness](../../tools/integration/linux9p/tests/mapped/run.py)
uses actual owned C translation and receipt verification with the real durable
Transport, concrete provider router, Producer, controller, journals and protected
providers. It covers two interleaved scopes with reused guest cells and distinct
native globals, original donor reclaim, mirrored RELEASE payload, exact native
and guest lost-reply retries, a signed multi-leg WAIT EVENT, early-deadline EMPTY
reclaim and late continuation refusal, closed guest-builder bytes, mutation/key/
context/credential negatives, and byte/token/allocation bounds. Native fd,
mount/INIT, guest credential observation and scheduling are explicit portable
primitives. Compile-only kernel/FreeBSD checks do not establish an installed
runtime, source-byte verification, C6 readiness, restoration or deployment.

The [C9 barriered same-owner coordinator](../../internal/controlledruntime/restart.md) now reconciles actual complete paired ledgers and fresh physical continuity before host component reopen. Mapping hashes alone remain inert; guest RESUME and replacement native/exporter owners stay blocked.
