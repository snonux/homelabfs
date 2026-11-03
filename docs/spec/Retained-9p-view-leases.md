# Retained-9p bridge 1.1 view leases and controlled local cuts

This is the negotiated representation and dispatch foundation for `j04`, not a
host token engine, Linux MM implementation, public serving grant, or lab
activation. `k04` must implement the durable controlled controller and prove its
actual sources; `vz3` owns physical Linux cache/PTE barriers. All admitted origins
remain eligible to read and write. A conflicting lease serializes cache access,
not write origin placement. Normal success still requires original O-local durable
journal/result plus M's complete RAM receipt (M=A for O!=A, M=B for O=A), without
owner-disk wait. Controlled local validation grants no normal replication proof.

## Exact negotiation and immutable transport binding

The exact1.1 9p Tversion/mount token is `9P2000.L.hfs1.1`; minor0 retains
`9P2000.L.hfs1`. Each connection copies its enrolled immutable minor on open,
then negotiates only that exact token without the endpoint lock. Mixed endpoint
sessions cannot negotiate each other's version. The indivisible bridge tuple is `{body_schema:2,bridge_major:1,bridge_minor:1,
features:2047,msize:65536,normal_major:3,normal_minor:0}`. Bits 0..7 retain
bridge1.0 features255; bit8=VIEW_LEASE, bit9=DIRTY_RESERVATION,
bit10=CONTROLLED_TARGET. Partial combinations, future bits, alternate versions,
missing fields and automatic downgrade are refused. The 88-byte HFS9BR1 frame
uses major1/minor1 at offsets8/10; all other framing/canonical/body bounds remain.
Every command has exactly `{cell,context,op,payload,protocol,reclaim}` and every
response exactly `{cell,context,op,payload,protocol}`; `protocol` is the exact
complete tuple. HELLO payload and ACCEPTED selection echo that same tuple.

Closed1.0 remains exact schema1/features255, minor0 and its old envelope. Its
reader rejects1.1 frames/bodies; new readers dispatch only under the explicitly
enrolled version, never infer support from a frame or HELLO proposal. The Go
Config.Protocol (all enrolled scopes) or mutually exclusive SessionProtocols
map (explicit1.1 sessions, unlisted enrolled sessions1.0), and lib9p `hfs9p_add_session_protocol(...,1)` are privileged
component enrollment seams, not default or runtime enablement. Existing APIs
select1.0. Enrollment cannot be changed by repeating HELLO. Every retained cell
and immutable reply binds frame minor, full body protocol, complete Context and
session. lib9p H9LEDG2 stores the installed minor and HELLO per session together
with existing exact byte/mapping/high-water/receipt ledgers; it is used whenever
any1.1 session exists, and old readers reject its magic. Pure1.0 ledgers retain
H9LEDG1. No host transport restore API exists yet: RESUME remains BLOCKED, and a
new Go Service must not reset or replace an issued endpoint. The mandatory
controller enrollment provider must verify prior complete ledgers or positive
exclusion before accepting any replacement. Tuple/hash equality alone does not
establish that continuity.

## Closed wire types

Types use the canonical sorted ASCII JSON, UUID/hash/Cap/Credentials rules of
[bridge1.0](Retained-9p-protocol.md). Every object has exactly the listed keys;
null and omitted fields are distinct. Types in `internal/guestbridge/view_protocol.go`
and validators in `view_schema.go` are the executable closed schema.

`Ticket={anchor:uuid,nonce:uuid,number:n64,credentials:Credentials}`. Its protected
key includes **complete original Context and full canonical Credentials**, with
process, credential revision, namespace/map and all groups, plus anchor/nonce/
number. It is neither secret authority nor an ordinary Claim. Every QUERY,
CANCEL, RELEASE, RENEW and retirement attests and authorizes that original
principal; guessing another process's anchor/nonce cannot disclose or mutate
its state. Changing credentials requires a new grant/ticket; renewal never
replaces original page provenance. Every ticket number is permanently allocated,
monotone and nonreused within its key family. The provider binds the original
operation and all canonical argument bytes before effects. Exact retries return
the original protected response; changed operation/intent is a conflict, even
if its hash/ID is internally consistent. QUERY and CANCEL address that binding;
they are not new admissions with the old number.

`Access` is exactly one of:

| kind | Remaining exact fields |
| --- | --- |
| DOMAIN | none |
| OBJECT | object:ROOT-or-PATH Cap |
| PAGE | description:DESCRIPTION Cap, offset:size, length:4096 |

PAGE offsets are 4096-aligned and <=S64MAX-4096 **before addition**. The profile
supports exactly kernel PAGE_SIZE4096; other page sizes must refuse enrollment.
This is a selected profile bound, not a statement about every Linux kernel.
DOMAIN covers pathname traversal, negative dentries, directory entries,
symlinks and ancestor search/capability dependencies. OBJECT covers metadata,
EOF and permission observations. PAGE covers the physical object and page range
resolved from the description, independent of path, aliases, description token,
process or origin. Metadata/EOF/truncate/permissions/OPEN-grant and namespace
transitions recall all affected page/object/domain observations, including
negative entry and ancestor dependencies. Conflict identity cannot be merely a
description or origin. Concurrent READ leases may share a cut. EXCLUSIVE_DIRTY
conflicts with every overlapping page read/dirty lease across every domain and
origin. DOMAIN observations may conservatively conflict with all mutations of
the domain. These rules close stale metadata/EOF bypasses without restricting
publication to a preferred writer.

`Target` is exactly one of:

* `{kind:NORMAL_PUBLIC,publication:Publication}` uses the existing genuine normal
  authority/selection/source proof, without any loosening of Publication rules.
* `{kind:CONTROLLED_LOCAL,cut:LocalCut}` where LocalCut is exactly
  `{enrollment_hash:hash,profile_hash:hash,context_hash:hash,
  journal_incarnation:uuid,journal_epoch:n64,journal_position:u64,journal_hash:hash,state_hash:hash,
  visibility_generation:uuid}`.

context_hash is SHA-256 of complete canonical Context, journal_incarnation must
match its actual origin_journal. journal_epoch/position/hash name the exact actual
local journal durable position and its complete original frame hash (position0
requires the verified actual enrolled baseline, not invented empty evidence).
state_hash names exact sealed service state bytes at that serialized cut;
visibility_generation is the actual local serving generation. enrollment_hash
binds the installed signed disposable enrollment, profile_hash its exact policy
and source identity. Context volume/generation/configuration plus journal_epoch
and journal_position identify the full actual local stream position. These are structural identities only. The mandatory
CheckEnrollment/CheckTarget provider must obtain and verify actual journal,
sealed service/store cut and signed enrollment through the local service seam.
Caller assertions, normal3-shaped local refs, baseline serial0 and hash-only
records cannot supply proof. Controlled Result.Publication remains null. No
v1/normal3.0 journal kind or closed fsapi enum is extended. Only VIEW_ACQUIRE may carry target:null for verified bootstrap/current-cut
discovery. The issuer must return an actual nonnull verified Lease.target; a
fresh null-target ticket cannot bypass conflict recall or all-domain/source
closure. A supplied nonnull acquire target is an **expected** cut: the issuer selects/verifies the actual current
cut, grants only if equal, otherwise returns BLOCKED or a fresh caller retry. The controlled expected
cut is structurally/scope validated before dispatch, but is not source authority
and need not remain in the issuer archive. It reaches the issuer even when stale
or pruned so the issuer can drive a bounded stopped install. Every actual returned
Lease.target still requires mandatory full-source CheckTarget verification; an
arbitrary expected hash can never issue a lease.

`Lease={id:hash,scope:Context,domain:DOMAIN Cap,access:Access,
credential_hash:hash,mode:READ|EXCLUSIVE_DIRTY,target:Target,fence:n64,
revision:n64}`. id hashes the canonical object with id removed. credential_hash
hashes original full Credentials. READ accepts any Access; EXCLUSIVE_DIRTY
requires PAGE with write right2. PAGE READ requires read right1. Live issuance
and Unix rights/ancestor authorization must be verified before grant, separately
from syntactic capability validation. A pending response is never a lease.
Fence identifies the controller's monotone physical cache lifetime; revision
advances exactly once per successful renewal, without wrap, with scope/domain/
access/credentials/mode/target/fence unchanged. Exact replay returns the same
revision bytes. A new revision has a new content id, while provider lineage
retains all old reservations and versions. No timer expires physical rights.

`ClaimNumber={realm:uuid,subject:bytes(1024),anchor:uuid,nonce:uuid,number:n64}`
permanently earmarks a future ordinary request number before dirtying. It has no
OriginalHash. The existing controlledvalidation Allocate/Claims.Reserve API
requires a complete immutable Claim and **cannot** perform this earmarking.
k04 needs a separate protected number/resource API. It must retain earmarked,
unsent and rejected gaps; no placeholder hash, counter reuse or mutation of an
ordinary bound Claim is allowed.

`Reservation={id:hash,scope:Context,lease_id:hash,description:DESCRIPTION Cap,
credentials:Credentials,offset:size,length:4096,claim_number:ClaimNumber,
provenance:uuid,drain_credit:hash}`. id hashes the canonical object minus id.
The complete original origin Context/credentials/description/rights and exact
page range are immutable. provenance is permanently nonreused; drain_credit
identifies already reserved capacities, not caller-created permission. The
provider pins descriptions even after close/process exit for the original
verified dirty obligation. It preowns freeze request/reply, frozen bytes,
manifest entry, final immutable claim/result/source and DRAIN_WRITE transport
and execution resources **before** writable PTE. This schema issues no such
resources without that provider.

## Operation union and protected result union

Existing1.0 operations persist. VIEW_ACK and VIEW_POLL/event bodies retain their
exact fields/phases, except nonnull targets use tagged Target in1.1. Frozen
Manifest and DirtyEntry retain their original construction. No untagged normal
Publication is accepted as a1.1 install target.

| New operation | Exact payload fields |
| --- | --- |
| VIEW_ACQUIRE | request:Ticket, domain:DOMAIN Cap, access:Access, mode:READ\|EXCLUSIVE_DIRTY, target:Target|null |
| VIEW_RELEASE, VIEW_RENEW | request:Ticket, lease:Lease |
| VIEW_QUERY, VIEW_CANCEL | request:Ticket |
| DIRTY_RESERVE | request:Ticket, lease:Lease, description:DESCRIPTION Cap, offset:size, length:4096, claim_number:ClaimNumber |
| DIRTY_FREEZE | request:Ticket, reservation:Reservation, recall:uuid, data:bytes(4096), claim:Claim |
| VIEW_RETIRE | anchor:uuid, nonce:uuid, credentials:Credentials, through:n64, result_hashes:[hash] |

Each new admission uses a fresh ticket; query/cancel use the target ticket.
RENEW/RELEASE authenticate the same original credential hash as the lease.
DIRTY_RESERVE additionally requires EXCLUSIVE_DIRTY PAGE, exact description,
range and original credentials. Exclusive lease alone is insufficient for a
writable PTE; a protected GRANTED Reservation is required. Unknown reserve or
acquire outcome holds the request pending without physical authority, and
must be queried/cancelled under the original key before retiring its number.
Cancellation yields CANCELLED only after verified absence or physical lease
release/fence plus source closure, never timeout or disconnect. A lost grant
reply remains a live controller obligation and cannot be erased or reassigned
without resolving possible guest receipt/physical access.

DIRTY_FREEZE consumes existing reservation credit, not fresh ordinary admission.
It binds nonempty exact frozen data <=4096, the matching reserved ClaimNumber,
original credentials and recall, once. Compute DirtyEntry with
`description,offset,length=len(data),payload_hash=SHA256(data),reservation=id`;
its id hashes those fields with id/claim removed. Compute DRAIN_WRITE intent
with that id, reservation id, description, offset, data and recall; only now
set Claim.original_hash=SHA256(canonical Intent). Freeze stores the complete
immutable call/bytes before its FROZEN reply, including original Credentials.
The ordinary DRAIN_WRITE call must exactly equal this frozen record. The
provider's ValidateDrain must reject any changed byte/claim/credential/recall.
This order has neither self-hash cycle nor mutation of unknown future bytes.
Overlap between different description aliases cannot merge away provenance.

Ticket responses have exactly `{id:hash,status,request:Ticket,operation,
request_hash:hash,lease:Lease|null,reservation:Reservation|null,entry:DirtyEntry|null}`;
id hashes all other fields. operation is the original admission operation;
request_hash hashes canonical `{op:originalOp,payload:originalPayload}`.
QUERY/CANCEL preserve this original lineage. Direct admissions check the exact
operation/hash locally; the mandatory CheckResponse provider verifies actual
protected original admission bytes and current outcome for every reply, including
QUERY/CANCEL and retirement. An original admission retry always replays its
byte-identical initial reply, including PENDING; QUERY may return a separately
protected current/terminal observation. Cancellation keeps the original admission
hash, protects one deterministic cancellation outcome and replays that exact
outcome on retry. Retirement uses the protected **terminal outcome id**, never
the initial PENDING reply id. QUERY validates GRANTED/FROZEN using the stored
original operation, not QUERY as an invented original admission.
Status-specific closed union:

| status | Authority fields and operations |
| --- | --- |
| PENDING, INDETERMINATE, BLOCKED, RETIRED_ID | all three null; no cache/PTE rights |
| GRANTED | exactly lease for ACQUIRE/RENEW, or reservation for DIRTY_RESERVE; QUERY returns that protected kind |
| RELEASED, CANCELLED | all three null, matching RELEASE/CANCEL or QUERY |
| FROZEN | entry only, exact frozen DIRTY_FREEZE binding; QUERY may replay it |

A GRANTED READ never carries a Reservation. RENEW returns exact prior identity
fields with revision+1. QUERY is authorized against the original complete
binding; changing credentials or session cannot adopt old tickets. VIEW_RETIRE
returns `{status:RETIRED|BLOCKED,floor:u64,proof_hash:hash|null}`, proof only for
RETIRED. result_hashes lists exact protected final reply ids for every number
from previous floor+1 through through, ascending, <=1024. The provider must also
close every allocated gap, lost acquisition result, lease, dirty reservation,
frozen claim, source pin and recall obligation before a permanent protected
floor/tombstone advances. Replies/cell release/close/disconnect/TTL do not retire
anything. Finite number/identity/source exhaustion blocks new admissions.

## Physical ordering, recovery and bounded progress

Acquisition, renew and reserve must not wait for recall completion while holding
a guest physical inflight cookie, global RPC worker, vnode lock, drain credit or
a service serialization lock. The Go transport reserves 64 ordinary/16 control
cells and a separate bounded acquisition queue/worker, in addition to existing
ordinary, control, release and drain workers. VIEW_ACQUIRE/DIRTY_RESERVE route as
ordinary; freeze, poll, ack, query and release use control. Provider admission
must reserve separate bounded ticket/lease/frozen-manifest/source arenas and
return PENDING/BLOCKED rather than accumulating waiters. For1.1 incomplete assemblies, at most12 ordinary candidates per session leave
four of the16 per-session assembly slots for control; the endpoint also retains
its16 global control credits independently of64 ordinary credits. A caller must
receive and RELEASE_CELL/reclaim its PENDING transport reply promptly while the
provider retains separately budgeted ticket state. DIRTY_FREEZE must use
preowned control capacity; never spend the last drain reserve waiting on recall.
One envelope still has the existing1MiB/32768-node/24-depth limits; the controller
must cap reservations and complete frozen manifests before PTEs, including all
maximum credential/capability/claim bytes. The Go queues alone do not prove that
controller budget.

An accepted transport cell whose service dispatch returns temporary WouldBlock
or NoSpace retains its exact native identity, command and body in the existing
worker's bounded pending ring. A timed FIFO retry gets priority when due; it
allocates no new cell, claim, counter or capability credit. Independent control,
drain and replacement workers remain available. Only the first published
response becomes immutable; logical filesystem error results are not retried.
Close stops local retry work without claiming endpoint or physical retirement.

In the private controlled provider, ordinary QUERY and repeated CALL may resume
the sealed original binding only while its durable preparation stage is exactly
UNPREPARED and no protected plan, outcome or bridge snapshot exists. Reserve
records UNPREPARED; execution records PREPARING before invoking preparation.
Both admission and the subsequent execution-lock stage check precede the
callback. A missing/ambiguous historical stage or a failure between preparation
and plan protection stays closed across reopen. Existing protected plans use
original-outcome recovery, never fresh preparation. This marker alone proves
no cancellation or no-effect closure and changes no claim identity, wire
status, pending snapshot version or historical credentials.

The guest receives an actual GRANTED lease, then joins physical access only
after rechecking exact target visibility_generation/fence/revision locally. The guest local cache revision
increments on every verified stopped install, even if visibility_generation
stays unchanged; that local stamp is separate from the host lease fence/revision.
Fetched data, attributes and PTEs require that same stamp recheck immediately
before installation. An outstanding RPC is outside physical inflight: its own
request may cause a recall. Recall closes new access and acquisition first,
joins existing physical accesses, revokes all writable aliases/PTEs and proves
TLB/MMU/pin/busy-page quiescence, then freezes exact bytes/provenance and emits
STOPPED. Intermediate protected local or normal cuts install while stopped;
drain completion does not wait for DRAINED. All exact original frozen entries
resolve and all intermediate installs complete before DRAINED; only then final
selection/install/reopen completes. Metadata conflicts follow the same closure.

Every event/ack binds complete domain, session, sequence, recall, authority,
inventory, closure, frozen manifest and exact tagged target. Duplicate sequence
must replay byte-identical data; stale/reordered/conflicting phase, changed
manifest/target or scope cannot advance state. Disconnect keeps pending tickets,
physical rights, dirty bytes, claims, pins and stopped domains; it invalidates
learned session acknowledgements. Reconnect requires actual reconstructed full
ledgers and original source closure or positive exclusion, then fresh exact
acks. Restart/new UUID/hash-only manifest is not continuity or exclusion. A
continuously surviving stopped domain may reack only under verified continuity.
No runtime/source controller or host restore is supplied here; missing evidence
keeps BLOCKED. No installation, mounts, SSH, VM or production action is authorized.


## Local evidence and remaining boundary

`python3 tools/spec/check_retained9p.py` checks both the unchanged1.0 corpus and
literal1.1 command/frame/digest/reply/freeze vectors from
[views.json](retained9p-fixtures/views.json). Its bounded ticket histories are a
design oracle only, including conflicting intent/principal, stable PENDING versus
terminal lineage, retained unknown gaps and resource partition. Go tests consume
those same literal bytes through actual host canonical/frame/closed-union code,
with malformed version/features/scope/rights/epoch/credentials/range/freeze,
untagged/null-target and response-lineage negatives, and mandatory-provider
refusal. The C suite exercises actual parser/routing, mixed session snapshots,
wrong Tversion, persisted minor flips with recomputed checksum, and actual local
fsync/reopen/lock/failure/downgrade refusal. None proves a durable lease engine,
actual signed enrollment, normal source receipts, physical MM or runtime cache
barriers. Source-byte receipts and command logs are retained in the isolated
j04 evidence directory; root runs complete merged gates separately.

## Controlled controller refinement (k04)

The explicit local controller additionally enrolls distinct acquisition and
preowned-control Ticket anchor/nonce families per full original Credentials.
ACQUIRE/RESERVE/RENEW use acquisition identities, FREEZE/RELEASE use controls;
QUERY/CANCEL keep original identities and RETIRE closes each exact family.
Transport class and identity family are separate. Refused/unknown ordinary
numbers remain retained for exact retry/closure and cannot strand preowned
control counters by sharing their family.

STOPPED's complete adopted manifest and authenticated kernel ACK universally
exclude future adoption of all old local revisions. Host-issued but unadopted
RESERVED/RESERVING entries can close only through mandatory typed physical Facts
binding that exact running-kernel/source contract and ACK provenance, exact STOP
event/domain/session/old fence, full issued ledger and exact omitted IDs. The
admitted running kernel captures revision before dispatch, requires LIVE and the
same revision/target at adoption and final writable PTE, closes before taking its
complete adopted snapshot, and increments revision before reopening. Missing
actual provenance/installation/barrier evidence blocks; omission, UUID or digest
alone proves nothing. FROZEN_PENDING/FROZEN/bound calls may never be omitted.

For positively excluded unbound reservations the host protects CANCELLING and
an exact proposed CANCELLED ticket reply, protects its ClaimNumber as a typed
no-effect gap in the ordinary provider (including an exact next-High allocation
if no earmark survived), and then publishes that reply/accepts STOPPED. Its exact
protected CANCELLED response id is the ordinary RETIRE.result_hashes gap id;
VIEW_QUERY/CANCEL of the original DIRTY_RESERVE retrieves it. This creates no
ordinary Claim, Result or OriginalHash. The immutable initial admission reply is
preserved. Partial two-ledger closure replays the same original id after reopen.

The host verified-command context accessor exposes detached native/controller/
scope/protocol/header and canonical command/payload hashes minted only after
actual Execute authentication/scope/version checks. It permits runtime Facts to
bind the actual ACK provenance without widening1.1. This observation alone is
neither kernel execution nor source/barrier proof. Native/platform verifiers
must use actual live supervised endpoint and admitted running component evidence;
local test shims and source equality do not grant runtime acceptance.
