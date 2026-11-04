# Controlled terminal guest-session retirement

Task `514` specifies the owned implementation for `614`, following `t04`. Task
`714` corrects its multiple-cell handoff against `5ef6840` and the immutable C6
maintained source. Task `614` implements this contract in the owned Go, exporter
and maintained Linux components; installation and normal publication remain unadmitted. Local
component evidence is recorded under
[614](../../tools/integration/linux9p/evidence/614/evidence.json). It applies only
to disposable `CONTROLLED_LOCAL` sessions with null publication. Production migration, normal publication, distributed authority
stay closed. The narrow [C9 barriered component restart](../../internal/controlledruntime/restart.md)
can retain terminal ownership only under the exact surviving native/exporter/kernel
owners with complete paired-ledger agreement; guest RESUME remains BLOCKED.

## Source-supported boundary

[Transport](../../internal/guestbridge/endpoint.go) accepts and protects a new
cell before dispatch. `RELEASE_CELL` reclaims its exact acknowledged donor and
creates a new `RELEASED` result. It deletes one cell and inserts one, so
repetition cannot even reduce multiple retained cells to one. Ordinary/control
mixtures and concurrent requests routinely leave multiple cells. Its reply,
ordinary `FLOOR`, view `VIEW_FLOOR`, `Transport.Close`, and the existing
`RESUME:BLOCKED` reply supply no terminal session authority.

[Mapping](Retained-9p-mapping.md) authenticates actual original/native command
and response bindings. C8 first protects the candidate and donor deletion;
`finishMappingBase` protects the response-base chain before Producer signs; the
worker protects both full response hashes, frames and coverage before exposure.
None of these existing APIs permanently closes guest-session issuance or
transfers final response ownership. The private Linux `Received` factory
observes a real completed transport response; it is not a public constructor.
In actual `hfs_session_destroy`, uncertain requests and physical retirement are
checked before `provider->retire_session`; only its success permits
`hfs_session_free`. The baseline C6 features16383 provider returns `EOPNOTSUPP` there; task614 adds
the explicitly negotiated features32767 path.

The selected addition is one mapped `SESSION_RETIRE` exchange. Its success
permanently closes one guest scope and transfers its final transport owner to
C8 and the protected exporter ledger. The guest can then have zero transport
cells without claiming zero native cells. One final native cell remains live,
charged and exactly replayable. Other guest sessions continue on the same FUSE
endpoint with the original controller and increasing native allocator.

## Explicit negotiation and closed schemas

The terminal extension keeps token `9P2000.L.hfs1.4`, frame major1/minor4,
body_schema5, bridge1.4, msize65536 and normal3.0. It reserves feature bit14
for terminal retirement: terminal-capable enrollment selects **features32767**
on both legs. The existing exact features16383 tuple remains supported
unchanged; it rejects this opcode and receipt kind. No tuple can change after
enrollment. This root-approved extension remains controlled1.4; it does not
select1.5. The accepted `t04` `VIEW_FLOOR` extension remains separate; ordinary
`FLOOR` continues to mean only original `RETIRE` coverage. Old0..3 remain
unchanged.

This choice and the terminal donor-class exception below were explicitly
approved by the root. Task714 amended the exact features32767 schema before
implementation; it created no new version. The implementation remains
uninstalled. C7 remains on features16383. Existing sole-donor component positives establish only that
limited composition, not full session closure.

All objects below are closed canonical JSON with duplicate/unknown fields
rejected. UUIDs, SHA256 hashes, full Context, full Credentials, Ack and
receipt2 retain their existing validation/encoding. No shortened credentials or
scope.

The ordinary command envelope remains exactly
`{cell,context,delegation,op,payload,protocol,reclaim}`. Guest delegation is
null; actual native delegation uses the existing1.4 inverse whitelist and
poll_index0. `op` is `SESSION_RETIRE`; `payload` has exactly:

```
{retirement_id,credentials,guest_through,observer_revision,closure_hash,
 settled_cells}
```

`retirement_id` is a fresh nonzero UUID retained by the kernel before issuance.
`credentials` is the exact captured full guest authorization snapshot for this
terminal exchange, checked by the existing Attestor and retained through
retries. It does not replace any original issuer credentials in ordinary/view
floors. `guest_through` is the actual guest issuance highwater immediately
before this command, not the count of remaining cells. The final command's cell
is greater; no artificial gap, maximum counter, reset or fresh session is used.
`observer_revision` is the nonzero immutable terminal snapshot revision
described below. `closure_hash` commits that snapshot's exact terminal object.

`reclaim` remains exactly one funding donor Ack, or null. `settled_cells` is
required even when empty: a canonical array of full guest Acks sorted by
strictly increasing guest cell, containing **every other** old retained cell
for this exact scope, at most79 entries. The disjoint union of reclaim and the
array must equal the actual old-cell set, at most80, in the kernel, exporter
and C8. These Acks acknowledge already fully observed immutable replies; they
grant no physical, claim, source or filesystem rights. Reject duplicates, the
donor in the array, future/candidate cells, omissions, extras, wrong
scope/principal, unknown/unread/incomplete cells and
pending/queued/executing/WAIT owners.

With a donor, native translation supplies its exact native Ack as today.
SESSION_RETIRE alone inherits that donor's actual ordinary/control reservation
and uses the existing funded replacement queue, including mixed-class old sets.
Preserve full-read and exact original/native donor rules. The settled cells do
not fund extra candidates; their credits remain owned until protected removal.
No-donor requires `settled_cells:[]`, actual old-cell count0 and already funded
existing control capacity. This changes neither RELEASE_CELL nor ordinary
reclaim/classification semantics; existing commands are not a count-reduction
prerequisite.

The payload, including the guest Ack array, is byte-identical on both legs. The
existing inverse delegation whitelist changes only to admit this exact terminal
schema with poll_index0; it never accepts caller native hashes. For each
settled Ack the actual private mapping owner retains a `MappingDonor` record
with GuestCell, NativeCell, GuestCommandHash, MappingHash, NativeFullHash and
GuestFullHash from that cell's existing protected mapping. Validate the full
guest/native reply hashes, complete coverage, original scope and principal
bindings against actual owners before deletion. Each old cell keeps its own
original credential binding; it need not equal the terminal caller credentials.
Retain actual class membership (a bounded bitset), before/after class counts
and fixed arena-credit accounting with these records, bound to the same
manifest. Never infer a mapping from an Ack, reset issuance or allocate another
cell on uncertainty. Preserve these bounded records in the terminal C8 cell and
exporter ledger after old rows are removed; public JSON cannot create their
provenance.

`settled_hash` is SHA256 of the canonical exact `settled_cells` array
(including `[]`). Command and closure hashes already commit that array; this
additional compact digest lets responses and evidence name it without repeating
it. All three are checked against the actual retained array, never treated as
authority.

The successful response payload is exactly:

```
{status:"RETIRED",retirement_id,guest_through,observer_revision,closure_hash,
 settled_hash,ownership_hash}
```

The response envelope is the existing controlled1.4 envelope with mandatory
receipt2 of kind `SESSION_RETIRED`. Its evidence has exactly:

```
{retirement_id,credentials,guest_through,observer_revision,closure_hash,
 settled_hash,ownership_hash,retained_owner:"C8_TERMINAL_CELL"}
```

Evidence copies the original five scalar/credential request fields and uses
settled_hash instead of the array. All repeated values equal the exact original
request or response. receipt2 already binds full scope, endpoint, actual
controller/serving generation, original guest cell/hash, translated
response-base hash, actual native cell/command/base hashes and protected
mapping chain. Require poll_count1. `SESSION_RETIRED` is valid only for
`SESSION_RETIRE` under the terminal tuple; that operation cannot consume
SOURCE, EVENT, FLOOR or VIEW_FLOOR. No null receipt, legacy receipt1, error
result, BLOCKED result or detached parsed value authorizes retirement. Refusal
retains ownership and returns an error; it cannot undo an accepted terminal
fence or authorize another command.

## Terminal prerequisites and their real owners

The guest must close workload admission first, join all admitted entrants and
poll/control workers, revoke mappings/PTEs/TLB aliases through the existing
physical stop path, complete funded nonempty dirty drain where required,
release leases/descriptions/source/cache pins, and obtain exact ordinary and
view floors. Joining a poll can reveal another unknown reply: recheck after
joining, before publishing readiness. The final transport exchange is a
dedicated exception to closed workload admission; it cannot reopen acquisition,
polling or allocation.

The provider reserves its terminal request/response/parser/Received capacity
before closing. Under existing physical/session/provider locking, it
permanently marks the mount `TERMINAL_PENDING` and publishes a fresh immutable
terminal object through the **existing admitted read-only kernel observer**,
alongside all original local inventory rows. New terminal object fields are
exactly:

```
{version:"homelabfs-guest-terminal/1",scope,controller,retirement_id,
 revision,guest_through,donor,settled_cells,credentials,
 entrants:0,polls:0,unknown:0,claims:0,held_results:0,descriptions:0,
 leases:0,reservations:0,dirty_pins:0,source_pins:0,mapped_pages:0,
 received_external:0,phase:"TERMINAL_PENDING"}
```

`donor` is the original funding guest Ack or null; `settled_cells` is the exact
request array captured with every known old Ack under retirement/writer
locking. `revision` equals observer_revision. `scope` and controller are the
installed originals. This object excludes only the already funded dedicated
terminal package and the known donor and settled cells' transport/Received
bookkeeping, including the immutable manifest and mapping records; none may
hold physical/source/claim rights. Inventory every excluded owner, its count,
class, bytes and charge. No hidden transport ownership or subtraction of
arbitrary owners to manufacture zero. Every count comes from actual owned
lists, pins, joined workers and completed physical-state checks. Publishing
zeros or signing JSON alone cannot prove those checks. `closure_hash` is SHA256
of this canonical object. No new guest writeable observer, upload or public
evidence factory.

[Admission](Retained-9p-guest-admission.md) retains the installed mount
identity, boot/provider nonce, controller, terminal id/revision/hash and
consumed device/tag after session memory is released. This permanent, budgeted
tombstone refuses all future enrollment, binding, source adoption and admission
for this session. Never remove its inventory row or replace it with apparent
absence. Snapshots must retain the unchanged complete enrollment and other
local sessions. Extend the existing worst-case provision/observer preflight to
include these terminal fields for every local row within the unchanged128KiB
observer bound; inventories that cannot publish them must refuse provisioning
before any admission. C5 refresh must recognize this explicit terminal state
while keeping other domains usable; a terminal row neither grants source rights
nor satisfies unrelated physical ACKs. Later `TERMINAL_RETIRED` observation
retains the same terminal object/commitment and records completion outside that
immutable object.

The host checks this object through the actual live Admission/observer and
supervised exporter/FUSE tuple, tied to the same private `VerifiedRequest` and
C8 worker/mapping owner. A caller's Context, hash, signature or boolean is
never this provenance. The guest kernel remains the adopted physical trust
boundary; the host does not infer a CPU/VM fence from these bytes.

Host closure also checks actual authoritative state, beyond the observer:

* Provider: every principal/family **owned by this scope**, across all full
  original credentials and ordinary/dirty issuers, has Floor==High; no pending
  allocation, claim, gap uncertainty, journal effect or live validation owner.
  All floors were advanced by exact protected RETIRE/GAP evidence. Zero-high
  families are allowed; no invented result id or bump to exhaustion.
* Controller: each scope-owned acquisition/control family has Floor==High;
  leases inactive, reservations CLOSED, no retained physical/dirty/source use,
  no outstanding event/ACK/install/drain duty or recall involving this domain.
  Shared source objects may remain only with other definite owners and no
  references/obligations from this scope. Existing source verification remains
  mandatory. No active shared recall is cut short to retire one participant.
* Bridge/service: no live description/path/READDIR/dirty capability user or
  pending validation/result consumer. Stable DOMAIN/root identities can become
  explicitly terminal tombstones; any residual capability use must refuse.
  Existing lifetime mechanisms close actual rights first. Terminal retirement
  neither forgets held grants nor treats retained identity count as live use.
* Transport/exporter: all original issued cells reconciled; no incomplete
  header/workspace, queued/executing/pending job, unknown native write, WAIT
  continuation or unread old response for this scope. Immediately before
  acceptance the exact old set is the donor plus settled manifest, or empty in
  the no-donor case; after acceptance precisely the terminal candidate. Compare
  complete original scopes and exact highwaters/mappings on both legs, not
  local list emptiness. An incomplete header for this scope blocks retirement
  even if it could be an unused hole.

Shared provider or controller families whose scope ownership cannot be proved
exclusive block terminal success. No sweeping deletion of shared state.

## Protection and handoff sequence

Use monotonic per-scope phases `OPEN -> TERMINAL_PENDING -> TERMINAL_SEALED ->
TERMINAL_RESPONDED`. A pending attempt is irreversible; failures retain the
session and permit only that exact terminal retry/read. No rollback to OPEN.
The final phase is host ownership, not evidence that the guest saw the reply.
The new tuple must be pinned through signed enrollment and every native/
delegated command and source/receipt check; the terminal bit is not a caller
flag. HELLO remains mandatory for every ordinary operation. The Service and
exporter HELLO gates gain one exact exception: features32767 SESSION_RETIRE may
close a clean, never-HELLO host component scope only with no donor, an empty
manifest, actual zero issuance/owners and all private worker/mapping, observer
and seal checks below. It never opens rights or marks HELLO complete. A native
prepared kernel mount already sends HELLO first; its clean case is not a
never-HELLO test.

1. **Guest preflight.** Finish the preceding prerequisites and publish the
   immutable observer object. Retain the exact original terminal command bytes,
   id, cell, complete credentials, donor Ack, full settled array and all
   charges. Serialise against every issuer. Subsequent attempts reuse these
   exact bytes and cell. The ordinary request/cell allocator is never reset.
   Failure before sending still leaves the admitted kernel terminal-pending and
   its ownership retained. **Implement a dedicated retained terminal candidate
   retry path.** Current hfs_control allocates a fresh cell on another call;
   only ALLOCATE has a dedicated uncertain-candidate lookup/retry.
   SESSION_RETIRE must instead find the exact privately owned terminal
   candidate, hold its original slot/donor, command
   bytes/cell/credentials/manifest and funded package, and resume hfs_transport
   on those bytes without incrementing next_cell. A new hfs_control call with
   reconstructed payload is not a retry. Under the retirement/writer/session
   locks, allow hfs_session_destroy to revisit this one typed terminal
   candidate despite its own pending/blocked status; every other uncertain
   candidate remains fatal to closure. Never clear the generic blocked flag or
   unlink an uncertain candidate as evidence of exclusion. On failure restore
   the same candidate owner; on private successful consumption release it
   exactly once.
2. **Exporter pending protection.** Under its existing mutex and sole-writer
   ledger, the actual owned lib9p backend joins/checks all work for this scope,
   compares its actual guest highwater and complete donor/settled old set, and
   protects a `TERMINAL_PENDING` fence containing the exact original/native
   candidate, immutable guest manifest and every private native donor binding
   **before forwarding any terminal byte**. Retain all old entries, class
   credits, counters and candidate ownership until actual full native terminal
   proof; the candidate uses replacement staging and holds the funding credit
   in transition in the existing funded replacement workspace, never an81st
   charged native cell, entry credit or new body arena. Existing old-entry
   bodies remain under their existing owners; protect them without making
   uncharged body copies, and charge exact pending snapshots/manifest staging.
   Retained old snapshots are still owned and budgeted; they are not silently
   discarded or double-counted as new live native credits. An ordinary
   input-write ACK is insufficient. Native write uncertainty never takes the
   ordinary EAGAIN reset/new-candidate path for this terminal attempt. It
   blocks every other new guest header, connection/version/HELLO attempt and
   WAIT continuation for this scope. Existing final-cell retransmit/fetch is
   the only exception; it still verifies the original channel/session/live
   native tuple. It does not close the shared backend, descriptors or global
   native allocator.
3. **C8 acceptance.** Before complete-command ACK/dispatch, under the transport
   lock, validate the complete old set, both-leg full coverage and mappings,
   exact guest_through against the pre-command watermark, no other incomplete
   scope header/workspace/job and no competing pending terminal request (the
   exact retained retry remains allowed). Atomically protect deletion of
   **all** known old rows, insertion of the one funded candidate, its bounded
   native donor records, staged post-commit per-class index counts, normal
   one-time allocator advancement with all earlier issuance history preserved,
   and the per-scope `TERMINAL_PENDING` fence in the existing sole-writer C8
   transaction. Extend the current two-ID delta restriction only for this
   bounded terminal deletion set (at most80 deletes and one candidate, no extra
   row puts or workspace). The fence names exact original/native hashes/cells
   and full manifest commitment. After confirmed commit, install those staged
   runtime counts and release secondary old reservation owners; transfer the
   funding donor's one class credit to the candidate (or retain the preowned
   no-donor control credit). Keep precommit owners until that outcome; an
   uncertain commit poisons/fences and retains ownership, never rolls back to
   OPEN or frees guessed subsets. Subsequent writes may only replay identical
   bytes for that candidate; all other issuance and reclaim targeting this
   scope refuse, including direct Service.Execute. Reads/retries of this final
   cell remain authenticated. Do not wait holding the transport lock for
   source, provider, controller or observer work.
4. **Authoritative seals.** The actual worker performs native verification and
   admits the protected mapping as today. A narrow concrete coordinator on the
   bound Producer consumes this private observation; it refreshes and checks
   the actual terminal observer state. It then durably seals the actual
   Provider, Controller and bridge capability scope in that fixed order. Each
   owner joins/serialises its own work, checks the prerequisites above under
   its own lock, and protects an irreversible scope seal bound to the same
   endpoint/controller/scope/terminal id/original request hash. Each seal is
   idempotent only for that exact identity. Provider sealing prevents
   subsequent trusted local Allocate/earmark/effect as well as remote calls.
   Controller sealing prevents new acquisition, source reservation, poll/event
   issuance or future recall membership for the terminal scope, retaining its
   immutable enrolled domain and terminal identity. Other live domains keep
   operating. The bridge seal refuses all old capability use and new grants for
   the retired scope. No change to another session's identities or rights. Each
   authoritative seal uses the closed common record:

   ```
   {version:"homelabfs-terminal-seal/1",owner,scope,endpoint,controller,
    retirement_id,guest_command_hash,closure_hash,state_hash}
   ```

   `owner` is exactly PROVIDER, CONTROLLER or BRIDGE. `state_hash` commits that
   owner's complete checked scope snapshot immediately before adding its seal;
   the seal itself is excluded to avoid a cycle. Persist the snapshot or retain
   its existing authoritative protected records with the seal, including exact
   families, original credentials and floors. A seal without those records
   cannot be revalidated. The bridge's seal belongs in its protected lifetime
   state, not an unprotected flag. There is no cross-ledger transaction: any
   partial seal or uncertain commit keeps C8 pending, all guest owners retained
   and success unavailable. Repeat the exact attempt only within the
   still-authenticated original assembly. Use the existing bounded pending
   worker for retryable pre-response refusals; duplicate input ACK alone is not
   redispatch. One worker owns the candidate. An uncertain ledger error that
   poisons the transport stays fenced: terminal retirement cannot clear that
   failure or recreate its authority.
5. **Protected terminal ownership.** After all seals are durably verified from
   their actual owners and the observer still matches, C8 protects a closed
   terminal ownership descriptor in its candidate row and advances to SEALED.
   It has exactly:

   ```
   {version:"homelabfs-terminal-owner/1",scope,endpoint,controller,
    serving_generation,retirement_id,guest_cell,native_cell,
    guest_command_hash,native_command_hash,guest_through,observer_revision,
    closure_hash,settled_hash,provider_seal_hash,controller_seal_hash,
    bridge_seal_hash,retained_owner:"C8_TERMINAL_CELL"}
   ```

   The three seal hashes commit exact protected owner records, read from their
   live bound owners; they are not supplied evidence or substitutes for those
   owners. `ownership_hash` hashes this canonical descriptor. It deliberately
   excludes response hashes and mapping chain: including them would create a
   signature/hash cycle. Protect the original seal records and descriptor
   before constructing the success response. A detached descriptor cannot
   create a private terminal observation. New narrow methods return inert
   descriptors; only the bound coordinator with the original worker observation
   may use them.
6. **Base then full response.** Construct the exact success base containing
   ownership_hash. Keep today's ordering: finishMappingBase durably records the
   actual native/guest response-base hashes and chain; only then may Producer
   sign SESSION_RETIRED. Retrying an already protected base must validate and
   reuse that exact base/chain, never append a second mapping leg. Signing
   additionally requires the same actual C8 transport's protected SEALED
   descriptor and live sealed owners. After attaching receipt2, the worker
   protects immutable full native and translated response hashes,
   frames/coverage, descriptor and RESPONDED phase in one C8 transaction before
   exposure. Do not sign authority based only on accepted candidate or missing
   local work. A crash after signing but before protection exposes no response.
   Full hash covers the signature; it never appears inside its own signed
   descriptor.
7. **Exporter final protection.** The backend obtains the actual full native
   reply through the original authenticated FUSE endpoint, completes all native
   frame reads and exact1.4 translation/mapping checks, and protects that exact
   native/guest response plus terminal final record, exact old-entry removals
   and resulting class counters before delivering any guest reply. This commit
   finally releases the secondary exporter credits while preserving the single
   inherited funding class. Protection failure/uncertainty retains the complete
   pending owner cut and fence; no guest bytes or guessed cleanup are allowed.
   Its native tuple verification is still mandatory. Its terminal fence was
   already permanent before send; unsigned payload parsing cannot reopen it.
   The guest performs pinned-key receipt verification itself. The backend does
   not mint guest provenance or grant retirement from a parse alone. No extra
   native request or RELEASE_CELL is sent to acknowledge this final result.
   Native coverage and exact guest read retries remain protected.
8. **Private guest consumption.** Actual hfs_control completion creates the
   private terminal Received owner, with the original request and full reply.
   Add a dedicated terminal consumption check: installed terminal-capable
   tuple, pinned enrolled key/controller/endpoint, exact original/full
   credentials, donor, every settled Ack and settled_hash against the complete
   original payload, observer id/revision/hash, SESSION_RETIRED/poll_count1 and
   receipt2 mapping. Check immutable physical/session closure again, allowing
   only the known exact donor/settled set, manifest and dedicated terminal
   package as transport bookkeeping, with their charges fully counted. Normal
   `admissible` must stay closed; this narrowly typed verifier cannot authorize
   SOURCE, EVENT, ordinary floor, view floor, map or cache adoption. It marks
   the registry terminal-retired and consumes/releases every guest old cell,
   terminal cell/Received package and session control reservations exactly
   once. No borrowed JSON or Received reference may outlive the session.
   Provider retire_session can now return success; `hfs_session_destroy` frees
   only after all teardown preconditions are settled and drops module/session
   references once. Retain the registry tombstone and its base charge.

The final native cell is **not released**: C8 owns its immutable command,
original/native mapping, response, coverage and terminal descriptor permanently
for this endpoint's retained lifetime. The exporter likewise owns its final
entry and session tombstone in its existing protected ledger. The guest frees
only its own rights and transport memory after the positive handoff. No final
ACK, caller observation of delivery or recursive release chain is needed.

`hfs_session_free` currently has fallible physical checks. Implementation must
make the successful handoff-to-free transition idempotent: perform these checks
before terminal authorization, keep the session permanently closed and charged
if a later failure occurs, and make retry consume the already verified terminal
state without RPC/reopening or double-free. Never set retired and discard the
only ownership reference while teardown can still fail. This is owned lifetime
code, not a generic kernel change.

## Lost replies, process loss and finite ownership

| Window | Required durable/live owner and permitted outcome |
|---|---|
| Exporter candidate pending, host has not accepted | Exporter ledger retains exact candidate/allocator, full old manifest/native owners and class counters; guest retains pending bytes and rights. Retry identical native/guest bytes. No new cell/id or timeout-based success. |
| C8 candidate/fence protected, seals incomplete | C8 owns candidate, full donor cut, resulting counters/history and permanent fence; earlier seals retain their owners. No success receipt. Original live assembly can retry the same seal sequence; a missing/uncertain owner blocks. |
| Seals and descriptor protected, no base/full reply yet | C8 keeps SEALED and all exact bindings. Same live assembly may finish the same base/signature/full response once, with every owner rechecked. It cannot return a terminal receipt from a detached disk snapshot. |
| Base protected or signature generated, full reply not committed | No bytes exposed. Preserve candidate, base/chain and seals. A live retry must match the protected base exactly; mismatch fences. No re-execution of claims or floor advancement. |
| C8 full response committed; host/exporter reply lost | Original live native endpoint replays the same immutable full response, signature, cells and mapping. Backend pending record identifies the exact native candidate and cannot allocate another. |
| Exporter full final record committed; guest reply lost | Replay identical protected guest frames for the same original cell. The kernel remains terminal-pending and charged until actual private verification; other guest ingress stays refused. |
| Guest consumed success; no further guest message | C8 final cell, exporter final row and kernel registry tombstone remain their definite owners. Duplicate reads return exact bytes; no state can issue fresh rights for the old scope. |
| Exporter dies; original host endpoint/controller survives | Decode/validate the exact protected pending/final exporter rows, phases, counters, fence and original tuple as retained evidence. A replacement exporter remains blocked unless C9 supplies actual supervised continuity and cross-ledger reconciliation. Merely loading valid rows or holding matching hashes never admits a new native owner. |
| Daemon/FUSE/controller or kernel continuity lost | Persisted rows are inert evidence. No new native connection, read-only inspection, host signature or terminal descriptor restores original authority. Keep all uncertain ownership fenced; RESUME remains BLOCKED until C9 supplies full admitted continuity/exclusion. No post-crash success is claimed where that mechanism is absent. |

Task614 must implement same-live-owner exact lost-reply retry and durable
pending/final record encoding, reconstruction for validation, and refusal.
Replacement exporter/kernel/endpoint admission remains blocked. The narrow
[C9 same-owner barriered path](../../internal/controlledruntime/restart.md) reopens
host semantic components while the exact native/exporter/kernel owners survive,
requiring fresh continuity and complete cross-ledger reconciliation. C9 depends
on614; these records and refusal checks also work without that coordinator. Decoders must validate the complete pre/post manifest owner cut,
class/counter/ budget accounting, all native mappings and issuance history;
reject a missing fence, partial subset, conflicting old rows, forgotten credit
or inconsistent final removal. Compact hashes alone cannot repair missing owner
records. A valid decoder result is inert evidence, not restored native
ownership. A host response committed before a crash is not permission to run a
replacement controller that merely knows its bytes. No restoration by absence
or guessing.

The kernel registry has at most64 permanently consumed local session rows;
lib9p has at most16 session rows per backend; C8 has its existing at most64
scopes and80 native cells. A terminal session permanently spends one existing
native cell charged to its inherited ordinary/control arena (control for the
no-donor case), one existing exporter entry and one session tombstone. C8
retains at most64 ordinary and16 control cells in total, including active
sessions; there is no guarantee of64 terminal-control cells. A backend's16
session rows can impose an earlier limit. Do not return that final
native/exporter credit after guest delivery or pretend it is free capacity. At
exhaustion, new admission refuses; there is no eviction by age. This bounds
final ownership without a tombstone per historical operation.

Keep the1MiB body,8MiB C8 row,128KiB index, existing C8 memory/ledger ceilings,
exporter arenas and guest signed budgets unchanged. Store the descriptor, seals
and bounded native donor records in the final cell/actual authoritative owners;
the index carries only bounded scalar/hash references, never duplicate full
provider/controller ledgers or manifests for every scope.

Serialization must use actual canonical encoders and source field limits. From
`types.go`/`codec.go`: MaxCell2147483647 gives a full Ack at most102 bytes; 79
Acks plus array separators cost8138 bytes. Full Credentials with1024 sorted
uint32 groups, maximal uint32 IDs and uint64 revision cost at most11542 bytes;
complete Context (three64-hex hashes and fourteen36-byte UUIDs) costs984 bytes.
The exact terminal observer schema above with one donor,79 settled cells and
maximal revision/highwater costs at most21227 bytes, so a16KiB object buffer is
insufficient. Authorize a terminal-only local object limit32768 bytes, charged
to existing registry/base reservations; this raises no signed quota. The exact
terminal-owner descriptor above costs at most2131 bytes, conservatively using
uint64 endpoint nodes and UUID serving_generation. Check actual encoding too.

A native `MappingDonor` record's current JSON field names and type maxima cost
382 bytes;79 records cost30258 bytes as an array (all80 including the funding
record cost30641). Reserve a terminal-only32768-byte native-record arena in
addition to the existing normal16KiB MappingMaxBytes allowance, **within** the
existing charged C8 row/memory and exporter staging budgets. This is retained
ownership, not free extra memory. Include keys, enclosing fields, descriptor,
seals and every staging/transaction copy in actual preflight. The existing row
formula (`mappedCellFunding`) is1MiB RawMessage + four base64 arenas of
ceil((1MiB+32*88)/3)*4+128 +33*16KiB native observations +32KiB fixed JSON
+16KiB normal mapping; add terminal records/descriptor/seal storage and prove
it still fits8MiB. The existing formula is7246336 bytes; adding32768 for
terminal native records,2131 for the descriptor and three at-most1583-byte
common seal records gives7285984, leaving over1MiB for enclosing terminal
fields, class bitset/counters and charged bounded metadata. Actual encoding and
every simultaneously retained allocation must still pass preflight. Do not
embed a full provider/controller snapshot in that row as a shortcut; their
protected ledgers retain it.

Response/evidence use settled_hash, keeping the full Ack array out of the
unchanged unsigned16KiB receipt arena. With full1024-group credentials, maximal
numeric values, complete scope/endpoint, poll_count1 and the exact evidence
schema, unsigned receipt2 is at most13810 bytes (evidence11965); the128-hex
signature makes the full receipt13953 bytes. Preflight actual maximal full
credentials, scope, mapping, descriptor and envelope through that encoder.
Before any provision/admission, worst-case all local registry rows (at most64)
must fit the unchanged128KiB observer with their terminal fields/tombstones;
reject an inventory that cannot publish it. Before
publication/send/complete-command ACK, preflight actual32KiB terminal
object/native arena,16KiB unsigned receipt and normal mapping,128KiB
observer/index,8MiB row and1MiB body limits, plus every preowned
request/parser/Received/control/base charge. No truncation, reduced
group/scope/body cap, signed-budget increase or deferred overbudget
publication. Ordinary saturation cannot steal terminal preowned capacity.
Permanent final native/exporter credits remain charged. No81st cell or second
replacement workspace is allocated; inability to fit refuses while retaining
ownership.

## Task614 implementation and acceptance

Implement only these owned seams; small new terminal-specific modules are
preferable to a public general-purpose evidence/restore API:

| Owner | Concrete files/seams |
|---|---|
| Negotiation and exact schemas | `internal/guestbridge/{mapped_protocol,codec,schema,response,verification,verification_v2}.go`; controlledruntime tuple admission; owned lib9p and Linux protocol/enrollment parsers/builders. Add exact dual-tuple checks, not broad minor>=4 acceptance. |
| C8 scope fence and terminal row | `internal/guestbridge/{endpoint,transport_ledger,mapping,service}.go`, private verification worker state and bridge `validation_state.go` capability/lifetime snapshot. Add typed terminal dispatch, bounded atomic multi-delete, retained native manifest owners and persisted phases/descriptor with direct-execution and new-header refusal. Preserve C8 commit/base/full ordering. |
| Real owner seals | `internal/controlledruntime/{producer,producer_mapping,facts,guest_observer,guest_artifacts,state}.go` and `probe/installed.py` observer decoding; narrow scope seal/checks in `internal/controlledvalidation` and `internal/viewcontrol` persistence/dispatch. Provider and controller must themselves enforce seals on local/trusted issuance and future recalls. No success-only verifier callback. |
| Actual native bridge | `tools/integration/lib9p/src/{hfs9p.c,hfs9p.h,hfs9p_ledger.c,hfs9p_ledger.h}` and owned JSON/protocol support. Protect complete pending/final manifest/counter scope state via the sole-writer ledger; reconstruct/validate exact records but refuse replacement-owner admission without C9 continuity; preserve shared endpoint/global highwater. |
| Actual guest lifetime | Maintained `fs/9p/{hfs_session.c,hfs_received.inc,hfs_received.h,hfs_controlled.c,hfs_controlled_internal.h,hfs_controlled_artifacts.c}` plus owned receipt/attestation/enrollment helpers as needed; reproduce through `tools/integration/linux9p/export.py` and maintained owned patches/manifests. Add complete known-Ack capture, retained-candidate exact-byte retry, private terminal Received consumption and immutable observer/tombstone; preserve all generic kernel/vendor code. |

If implementation requires changing generic kernel/PTE/vendor code, stop and
raise the concrete need to the root; this design authorizes only owned seams.

Minimum real component tests for614 (new tests, not claimed results):

1. Extend the actual C lib9p carrier + real Go durable Transport/Producer/
   controller/provider/service harness, using genuine disposable journals and
   protected ledgers. Two guest sessions share one endpoint and reuse guest
   cell numbers while native cells remain distinct. Create multiple ordinary
   cells, concurrent requests and mixed ordinary/control old cells. Fully
   reconcile one session, finish exact terminal exchange through actual C, then
   prove guest cell/Received/session ownership is released while one exact
   native C8 final cell and exporter final entry remain charged. Other session
   must allocate, execute, view/control and reclaim normally on the same
   endpoint/controller; global native highwater increases rather than resets.
2. Use actual Linux maintained `hfs_session_destroy`, hfs_control/private
   Received and controlled provider helpers in the portable production-source
   harness. Exercise empty, sole-donor and many-cell terminal cases (including
   an actual nonempty dirty session using the actual C7/t04 dirty
   reserve/freeze/drain paths with an explicitly enrolled32767 tuple and new
   terminal exchange); existing16383 C7 wire fixtures remain unchanged. The
   observer must come from the actual provider lists/physical hooks. Exercise
   full original groups, process/revision/namespace/idmap and separate issuer
   families. No test-created private token or unconditional physical/source
   success shim counts as proof of the missing boundary. Portable MM/native
   hooks remain explicitly labeled.
3. Fail at each protected boundary above, including multi-delete/counter and
   uncertain ledger commit, pending protection before host write, lost/partial
   native write, partial owner seals, base before signature, full response
   before exposure, exporter final protection and guest read loss. In the same
   live guest/backend/host assembly, lose input ACK/full or partial reply and
   retry the actual retained candidate: identical original/native bytes/cells,
   final frames/hashes, one terminal cell, monotonic counters and no repeated
   effects. Verify next_cell does not advance on terminal retry. Separately
   reopen the actual backend ledger to validate its pending/final records and
   require replacement-owner refusal without C9. Kill/replace the host
   connection and require fenced refusal, not a simulated positive restore.
   Verify pending/final binary records cannot omit the fence, any manifest
   member/native mapping, original class charge or pre/post owner accounting.
4. Reject changed id/scope/controller/tuple/key, donor/full hash, original
   credentials (including groups with same UID), guest_through/revision/hash,
   native mapping/poll index, response/kind/status and unknown fields. Reject
   missing/noncanonical/duplicate/omitted/extra manifest members, donor
   repeated in the set, wrong/other-session/future/unread Ack and altered full
   hashes. Reject detached/signed public evidence without actual
   worker/Received/observer provenance. Existing16383 and0..3 must reject
   terminal op/kind while retaining their existing allowed wire
   bytes/semantics. VIEW_FLOOR cannot satisfy this exchange; SESSION_RETIRED
   cannot advance either floor.
5. Each actual held result/user, uncertain/pending claim, unfunded/nonempty
   dirty pin, active lease/reservation/source, mapped alias/PTE refusal,
   external Received owner, in-flight poll/header/WAIT and unknown native write
   must refuse terminal success and preserve charges/module/session. Missing
   family or one floor below High refuses. Race another issuer/header/recall
   against the fence and verify either serialized prior obligation blocks
   success or subsequent issuance refuses. Replays with a fresh cell/id and
   reopening the old device/tag/session refuse permanently.
6. Pressure tests account permanent final cells/rows, descriptor/index maxima
   and signed guest budget. Exercise all80 old cells (64ordinary+16control),
   the79-member bound and overflow refusal without an81st cell; count every
   released secondary charge and the permanently charged final class on both
   legs and in the guest. Saturate ordinary capacity across both sessions;
   preowned funded control can still reach terminal handoff. Exhaust permanent
   terminal identities/cell credits and require bounded refusal, never
   eviction. Force post-handoff guest teardown failure and verify idempotent
   retained cleanup without double unreserve/module_put or a new RPC. Cover the
   clean never-HELLO host-component exception with actual private
   mappings/observer/ seals; separately label the prepared native mount case as
   HELLO-first.

`t04` still owns the funded real nonempty DIRTY_RESERVE/FROZEN/drain and exact
ordinary/view/GAP/held/unknown resource foundation; task614 must consume those
paths, not substitute clean-only fixtures. Portable Go/C success establishes
owned component composition, not installed native FUSE authentication, real
kernel PTE/TLB exclusion or deployed ABI/resource acceptance. Full custom
kernel, unchanged4MiB canary and platform/native adversarial checks remain
`xz3`/`zq3`; C9 must reconcile these terminal records before any positive
restoration claim.
