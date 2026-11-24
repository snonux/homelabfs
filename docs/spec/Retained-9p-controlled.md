# Retained-9p controlled allocation 1.2

This is the closed controlled-local successor selected by `n04`, not normal
publication or an installed runtime. Both legs must explicitly select version
`9P2000.L.hfs1.2`, frame major1/minor2 and the indivisible
HELLO/body tuple `{body_schema:3,bridge_major:1,bridge_minor:2,features:4095,
msize:65536,normal_major:3,normal_minor:0}`. Bit11 selects this entire controlled
runtime contract; all existing bridge1.1 bits remain required. Missing/unknown
bits, mixed tuples and downgrade refuse. Old1.0/1.1 remain closed. Frame layout,
body/chunk limits, cell counts and replacement workspace remain unchanged.

Every command has the existing extension envelope with this exact `protocol`.
HELLO cannot change installed enrollment, even when repeated. The host requires
an explicitly disposable validation profile and signed provider Enrollment's
optional `Protocol` equal to this tuple. Nil preserves previous enrollment bytes
and cannot select1.2. The tuple is part of the private provider's durable scope
identity and embedded aggregate view enrollment; signing another tuple cannot
reopen an existing ledger. Each scope retains its own exact tuple. A normal
configuration cannot select this successor.

## Ordinary issuance and gaps

ALLOCATE and GAP_RESOLVE each carry **exactly** the existing full Call object:
`{claim:Claim,credentials:Credentials,intent:Intent}`. Claim.original_hash must
match SHA256 of canonical Intent. All original Context fields, full sorted
numeric credential groups, process/revision, namespace/idmap and principal/claim
are retained byte-exact. DRAIN_WRITE is forbidden in these operations. Dirty
future ClaimNumber earmarks have no OriginalHash and retain their existing
controller-authenticated CANCELLED-ticket gap path; ordinary allocation cannot
adopt them.

The host authenticates the actual native callback, enrolled exact scope/version,
completed HELLO, guest-kernel credentials and live controlled profile/endpoint
before invoking the real provider. Original binding and next-number issuance
are one synchronous private transaction. Existing retained/source credits are
preowned at allocation, never charged twice at CALL. No filesystem planning or
effect occurs at ALLOCATE. CALL remains separate execution admission and cannot
allocate a number. Every retry must preserve the exact original request.

The guest retains its tentative request before sending ALLOCATE and serializes
ordinary issuance with dirty earmarks. Lost/uncertain acknowledgement blocks
later issuance until that same exchange reconciles; never decrement, skip or
reuse a number. Host success is `{claim:Claim,status:ALLOCATED}`. After an
acknowledgement loss, exact retry returns the same protected identity. Changed
credentials/intent/scope/claim refuse. ALLOCATE uses ordinary cells/workers, so
it cannot consume the reserved STOP/drain lane.

After PROCESS_EXIT, ALLOCATE accepts the issuer's already retained exact
original credential snapshot only for the cleanup shapes CALL accepts from an
exited issuer (protocol spec, FORGET/DESCRIPTION_RELEASE): FORGET `count=1` of
a still-live PATH grant, or DESCRIPTION_RELEASE of a still-live DESCRIPTION
grant, whose capability hash binds those exact credential bytes in the same
scope. The guest's final release can legitimately follow a completed
PROCESS_EXIT (an inherited descriptor outliving its opener, or the exit racing
the release), so refusing it at ALLOCATE while CALL would accept it blocked the
session. Every other exited-issuer ALLOCATE, including OPEN, READ, any
mutation, DESCRIPTION_CLOSE (the completed PROCESS_EXIT already released the
same per-process lock owner) and any fresh credential revision, refuses STALE
without consuming a number. DRAIN_WRITE is never allocated by ALLOCATE.

GAP_RESOLVE success is `{claim:Claim,status:GAP,proof_hash:hash}`. The real
provider protects exact no-effect only for a never-prepared ordinary allocation.
Unissued numbers, changed originals, PENDING/planned/effected operations, and
failed preparation with uncertain effects refuse. Exact lost-reply retries
return the same gap proof. The1.2 RETIRE request adds mandatory full `credentials` to the existing
`{realm,subject,anchor,nonce,through,result_hashes}` payload. Old1.0/1.1
continue to reject that extra field. The bridge attests these credentials and
rechecks the live profile; the narrow provider adapter matches every covered
protected original allocation/call or typed dirty earmark to these exact
credentials and scope. A valid current credential assertion cannot replace the
original owner. GAP_RESOLVE and RETIRE may use an already retained exact
original credential snapshot after PROCESS_EXIT solely to close those protected
numbers/floors; live attestation/profile checks remain mandatory. They cannot
admit a fresh revision, allocate a number or execute an unsent CALL. The exact extended request is the protected retirement retry
key. RETIRE retains the existing complete ascending
result/gap coverage, bounded1024 numbers and durable idempotent floor receipt;
no missing/intermediate number can be skipped. Component retirement remains
available without claiming a signed guest floor-verification receipt.

## Session abandon of stranded claims (hh4)

ABANDON is an additive controlled operation, accepted under every controlled
tuple (frame minor 2, 3 and 4) and refused by closed1.0 and1.1. It is the
guest's explicit signal that a **blocked** session gives up on an exact list
of its own uncertain claims: after sending it the guest sends no further
CALL, QUERY or GAP_RESOLVE for any listed claim. The payload is exactly
`{realm:uuid,subject:bytes(1024),anchor:uuid,nonce:uuid,
claims:[{number:n64,original_hash:hash}]}` with 1..64 claims in strictly
ascending number order, all of the one named family. The response payload is
exactly `{claims:[{number:n64,status:ABANDONED|KEPT|RETIRED_ID|UNKNOWN,
proof_hash:hash|null}]}`, one outcome per requested number in request order,
`proof_hash` non-null exactly on ABANDONED. `verification` is null: ABANDON
grants no source, event or floor authority.

The host authenticates the actual native callback, enrolled exact scope and
version, completed HELLO and the live controlled profile and endpoint, as for
RETIRE. It is reserved control work (the STOP/drain lane), like GAP_RESOLVE,
and like GAP_RESOLVE it is not a view operation. In one private ledger
transaction, serialized with every preparation of a never-prepared claim, the
provider answers each number:

* ABANDONED: the claim was provably never prepared (an allocated original
  whose CALL has not reserved it, or RESERVED with no preparation begun, no
  plan, outcome, result or snapshot). It is now a protected no-effect gap,
  its retained and source credit returned to the budgets and its entry credit
  zero; or it already was such a gap (GAP_RESOLVE or an earlier ABANDON),
  which is never rewritten. `proof_hash` is the gap proof RETIRE coverage
  requires for that number;
* KEPT: preparation began, a plan is protected, the claim is TERMINAL, it
  is bound to an open dirty earmark, or the number holds only a dirty earmark
  (allocated by the earmark, holding earmark credit, closed only by the
  controller's CANCELLED-ticket path). It may have had an effect or carries
  an obligation, and stays retained for the guest's normal reconciliation;
* RETIRED_ID: the number is at or below the floor;
* UNKNOWN: the number has neither a claim entry nor an earmark: it was never
  allocated and nothing is charged for it.

For a number with a claim entry, the entry's identity is checked first: a
protected original that names another session scope, or a wrong original
hash, refuses the whole command (Permission, Stale) with no ledger change. A
retired number (whose entry may be recycled) and an earmark-only number have
no entry to check and answer RETIRED_ID and KEPT. An exact replay changes
nothing and returns the same outcomes and proofs. After ABANDONED, a delayed
or replayed CALL executes nothing, a QUERY answers without effect
(INDETERMINATE, or the protected DECLINED of a mapped QUERY), a replayed
ALLOCATE writes nothing and GAP_RESOLVE returns the same proof.

A forwarder or host without ABANDON refuses it before any host state is
touched. The first refusal is the lib9p exporter's command classifier
(`_command` in `tools/integration/lib9p/src/hfs9p.c`): an exporter older
than hh4 answers EINVAL for an operation it does not know when the guest's
last chunk completes the cell, so the guest's send fails (ERROR INVALID) and
nothing is forwarded to the host. Since hh4 the exporter routes ABANDON on
minor 2, 3 and 4 as reserved control work, after checking the closed payload
shape above. A Go host reached directly without ABANDON refuses it in its
closed schema. Either way the guest keeps its claims retained exactly as
before and does not resend the refusal. The design and safety argument are in
[hh4-abandon.md](../design/hh4-abandon.md).

## Response verification union

Every1.2 response is exactly
`{cell,context,op,payload,protocol,verification}`. The first five fields retain
their existing meanings. `verification` is either null or the following closed
receipt;1.0/1.1 response schemas do not gain this field. Null supplies **no guest
source/event/floor authority**, including when the payload contains an existing
protected component gap or retirement proof. A runtime cannot advertise READY
with mandatory receipt production/consumption missing.

The closed receipt is
`{version,kind,scope,endpoint,controller,serving_generation,cell,command_hash,
response_hash,evidence,signature}`:

* version is exactly `homelabfs-controlled-verification/1`;
* kind is SOURCE, EVENT or FLOOR;
* scope is the full original Context; endpoint is exactly
  `{fuse_session,command_node,result_node,recall_node}`;
* controller and serving_generation are UUIDs, cell is the original cell;
* command_hash hashes the complete canonical original command;
* response_hash hashes the canonical response **with verification removed**,
  avoiding self-reference while binding exact payload bytes and tuple;
* signature is64 bytes, lowercase hex. The pinned enrolled host endpoint key
  signs canonical receipt bytes with signature removed. There is no supplied-key
  selector or caller boolean. Consumer checks the actual enrolled signer and all
  request/channel/cell/controller/generation bindings independently.

Evidence is a closed tagged union selected solely by kind:

| kind | Exact evidence fields |
| --- | --- |
| SOURCE | `{target:CONTROLLED_LOCAL ViewTarget,retained_owner:uuid}` |
| EVENT | `{domain_token:hash,sequence:n64,recall:uuid,phase,manifest_hash,target,retained_owner:uuid}` |
| FLOOR | `{realm:uuid,subject:bytes1024,anchor:uuid,nonce:uuid,through:n64,proof_hash:hash,credentials:Credentials}` |

EVENT phases are RECALL, INSTALL_DRAIN, INSTALL_FINAL, REOPEN. RECALL has null
manifest_hash and target; later phases require a hash and CONTROLLED_LOCAL
ViewTarget. FLOOR credentials and family are exact originals, not a new caller
credential snapshot. Successful controlled filesystem results always retain
`publication:null`; NORMAL_PUBLIC targets are forbidden in these receipts.

Structural decoding authenticates nothing. A future host producer must verify
actual complete sealed journal/catalog/payload/source/visibility bytes and
retained ownership before SOURCE; exact authenticated admitted-kernel event and
complete physical/source obligations before EVENT; actual complete protected
coverage before FLOOR. Signatures/hashes cannot replace those facts. The guest
must privately associate receipt verification with an actually received response,
not a caller-created JSON object. No recursive source RPC may run under
view/physical/service locks. Receipt lifetime and copies consume the retained
source/event/floor owner's existing finite credits.

The strict decoder supplies no authority by itself. The explicit C5
[controlled runtime producer](../../internal/controlledruntime/README.md) binds
actual native/provider/controller objects, live admitted-kernel observations and
complete sealed source or protected floor evidence before signing. The default
component configuration still carries verification:null and refuses1.2 view/source
exchanges before controller mutation without that producer. Missing installed
kernel/sysfs observations refuse production construction; primitive component
fixtures establish no installed/native readiness. `s04` owns actual receipt
consumption and the kernel observation carrier; `t04` owns guest floor/resource
retirement. Native/platform/kernel and complete runtime acceptance remain
separate gates.

Literal commands, frames and component responses are in
[controlled.json](retained9p-fixtures/controlled.json). They are portable codec
vectors, not physical/native/source evidence.
