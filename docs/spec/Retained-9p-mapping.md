# Retained-9p authenticated original mapping 1.4

Task `114` implements the host foundation for an explicit controlled-local
successor. It does not implement the owned C backend carrier, guest runtime
integration (`y04`), a kernel received-response provider (`s04`), restoration,
or distributed operation. The pure verifier below cannot create kernel receipt
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
not a later worker's watermark. Complete active rows and at most80 aggregate
recent guest issuance markers prevent duplicates, including reuse through an
older still-incomplete reservation after a donor was deleted. Markers are
freshness guards, never evidence that unseen holes retired. A marker is pruned
only when no older incomplete capture can reuse it. New headers reserve capacity
for `recent + incomplete <=80` before their first ACK; completion spends its
already owned slot. A permanently incomplete cell may eventually close fresh
admission. No marker is evicted to make room.

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
