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

This foundation supplies the strict receipt shape decoder, **no producer** or
success-returning fact verifier. Its responses carry verification:null.
1.2 view/source exchanges (VIEW_ACQUIRE/RELEASE/RENEW/CANCEL/QUERY/RETIRE,
DIRTY_RESERVE/FREEZE, VIEW_POLL/ACK) fail before controller mutation until the
mandatory producer is implemented. `r04` and `s04` own actual producer/consumer
verification; `t04` owns guest floor/resource retirement. Native/platform/kernel
and complete runtime acceptance remain separate gates.

Literal commands, frames and component responses are in
[controlled.json](retained9p-fixtures/controlled.json). They are portable codec
vectors, not physical/native/source evidence.
