# Controlled terminal guest-session retirement

Task `514` specifies the owned implementation for `614`, following `t04`.
This is a design against `d51d153` and the immutable C6 maintained source, not
an implemented or installed retirement API. It applies only to disposable
`CONTROLLED_LOCAL` sessions with null publication. Production migration,
normal publication, distributed authority and endpoint restoration stay closed.

## Source-supported boundary

[Transport](../../internal/guestbridge/endpoint.go) accepts and protects a new
cell before dispatch. `RELEASE_CELL` reclaims its exact acknowledged donor and
creates a new `RELEASED` result. Repeating it cannot produce zero cells. Its
reply, ordinary `FLOOR`, view `VIEW_FLOOR`, `Transport.Close`, and the existing
`RESUME:BLOCKED` reply supply no terminal session authority.

[Mapping](Retained-9p-mapping.md) authenticates actual original/native command
and response bindings. C8 first protects the candidate and donor deletion;
`finishMappingBase` protects the response-base chain before Producer signs;
the worker protects both full response hashes, frames and coverage before
exposure. None of these existing APIs permanently closes guest-session issuance
or transfers final response ownership. The private Linux `Received` factory
observes a real completed transport response; it is not a public constructor.
In actual `hfs_session_destroy`, uncertain requests and physical retirement are
checked before `provider->retire_session`; only its success permits
`hfs_session_free`. C6's provider returns `EOPNOTSUPP` there.

The selected addition is one mapped `SESSION_RETIRE` exchange. Its success
permanently closes one guest scope and transfers its final transport owner to
C8 and the protected exporter ledger. The guest can then have zero transport
cells without claiming zero native cells. One final native cell remains live,
charged and exactly replayable. Other guest sessions continue on the same FUSE
endpoint with the original controller and increasing native allocator.

## Explicit negotiation and closed schemas

The terminal extension keeps token `9P2000.L.hfs1.4`, frame major1/minor4,
body_schema5, bridge1.4, msize65536 and normal3.0. It reserves feature bit14 for
terminal retirement: terminal-capable enrollment selects **features32767** on
both legs. The existing exact features16383 tuple remains supported unchanged;
it rejects this opcode and receipt kind. No tuple can change after enrollment.
This root-approved extension remains controlled1.4; it does not select1.5.
The accepted `t04` `VIEW_FLOOR` extension remains separate; ordinary `FLOOR`
continues to mean only original `RETIRE` coverage. Old0..3 remain unchanged.

This choice and the donor-class exception below were explicitly approved by
the root for task614; C7 remains on features16383.

All objects below are closed canonical JSON with duplicate/unknown fields
rejected. UUIDs, SHA256 hashes, full Context, full Credentials, Ack and receipt2
retain their existing validation/encoding. No shortened credentials or scope.

The ordinary command envelope remains exactly
`{cell,context,delegation,op,payload,protocol,reclaim}`. Guest delegation is null;
actual native delegation uses the existing1.4 inverse whitelist and poll_index0.
`op` is `SESSION_RETIRE`; `payload` has exactly:

```
{retirement_id,credentials,guest_through,observer_revision,closure_hash}
```

`retirement_id` is a fresh nonzero UUID retained by the kernel before issuance.
`credentials` is the exact captured full guest authorization snapshot for this
terminal exchange, checked by the existing Attestor and retained through retries.
It does not replace any original issuer credentials in ordinary/view floors.
`guest_through` is the actual guest issuance highwater immediately before this
command, not the count of remaining cells. The final command's cell is greater;
no artificial gap, maximum counter, reset or fresh session is used.
`observer_revision` is the nonzero immutable terminal snapshot revision described
below. `closure_hash` commits that snapshot's exact terminal object.

`reclaim` is null if the session has no existing guest cells; otherwise it is the
exact full Ack of the **sole** remaining fully observed guest cell. Native
translation supplies its corresponding exact native Ack. SESSION_RETIRE has
its own explicit RELEASE-like classification exception: inherit the donor's
actual ordinary/control reservation and dispatch through the existing funded
replacement queue. Preserve exact same-scope, full-read and original/native
donor rules. This does not change `RELEASE_CELL`. A no-donor command requires
already funded existing control capacity. More than one old cell must first be reconciled and
reclaimed with existing commands. No unknown cell may be silently included.

The successful response payload is exactly:

```
{status:"RETIRED",retirement_id,guest_through,observer_revision,closure_hash,
 ownership_hash}
```

The response envelope is the existing controlled1.4 envelope with mandatory
receipt2 of kind `SESSION_RETIRED`. Its evidence has exactly:

```
{retirement_id,credentials,guest_through,observer_revision,closure_hash,
 ownership_hash,retained_owner:"C8_TERMINAL_CELL"}
```

All repeated values equal the exact original request or response. receipt2
already binds full scope, endpoint, actual controller/serving generation,
original guest cell/hash, translated response-base hash, actual native
cell/command/base hashes and protected mapping chain. Require poll_count1.
`SESSION_RETIRED` is valid only for `SESSION_RETIRE` under the terminal tuple;
that operation cannot consume SOURCE, EVENT, FLOOR or VIEW_FLOOR. No null receipt,
legacy receipt1, error result, BLOCKED result or detached parsed value authorizes
retirement. Refusal retains ownership and returns an error; it cannot undo an
accepted terminal fence or authorize another command.

## Terminal prerequisites and their real owners

The guest must close workload admission first, join all admitted entrants and
poll/control workers, revoke mappings/PTEs/TLB aliases through the existing
physical stop path, complete funded nonempty dirty drain where required, release
leases/descriptions/source/cache pins, and obtain exact ordinary and view floors.
Joining a poll can reveal another unknown reply: recheck after joining, before
publishing readiness. The final transport exchange is a dedicated exception to
closed workload admission; it cannot reopen acquisition, polling or allocation.

The provider reserves its terminal request/response/parser/Received capacity
before closing. Under existing physical/session/provider locking, it permanently
marks the mount `TERMINAL_PENDING` and publishes a fresh immutable terminal
object through the **existing admitted read-only kernel observer**, alongside
all original local inventory rows. New terminal object fields are exactly:

```
{version:"homelabfs-guest-terminal/1",scope,controller,retirement_id,
 revision,guest_through,donor,credentials,
 entrants:0,polls:0,unknown:0,claims:0,held_results:0,descriptions:0,
 leases:0,reservations:0,dirty_pins:0,source_pins:0,mapped_pages:0,
 received_external:0,phase:"TERMINAL_PENDING"}
```

`donor` is the original full guest Ack or null. `revision` equals
observer_revision. `scope` and controller are the installed originals. This
object excludes only the already funded dedicated terminal package and the
known donor's transport/Received bookkeeping; neither may hold physical/source/
claim rights. Explicitly account both exclusions; do not subtract arbitrary
owners to manufacture zero. Every count comes from actual owned lists, pins,
joined workers and completed physical-state checks. Publishing zeros or signing
JSON alone cannot prove those checks. `closure_hash` is SHA256 of this canonical
object. No new guest writeable observer, upload or public evidence factory.

[Admission](Retained-9p-guest-admission.md) retains the installed mount identity,
boot/provider nonce, controller, terminal id/revision/hash and consumed device/tag
after session memory is released. This permanent, budgeted tombstone refuses
all future enrollment, binding, source adoption and admission for this session.
Never remove its inventory row or replace it with apparent absence. Snapshots
must retain the unchanged complete enrollment and other local sessions. Extend
the existing worst-case provision/observer preflight to include these terminal
fields for every local row within the unchanged128KiB observer bound; inventories
that cannot publish them must refuse provisioning before any admission. C5
refresh must recognize this explicit terminal state while keeping other domains
usable; a terminal row neither grants source rights nor satisfies unrelated
physical ACKs. Later `TERMINAL_RETIRED` observation retains the same terminal
object/commitment and records completion outside that immutable object.

The host checks this object through the actual live Admission/observer and
supervised exporter/FUSE tuple, tied to the same private `VerifiedRequest` and
C8 worker/mapping owner. A caller's Context, hash, signature or boolean is never
this provenance. The guest kernel remains the adopted physical trust boundary;
the host does not infer a CPU/VM fence from these bytes.

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
  acceptance there are zero cells or precisely the donor; after acceptance
  precisely the terminal candidate. Compare complete original scopes and exact
  highwaters/mappings on both legs, not local list emptiness. An incomplete
  header for this scope blocks retirement even if it could be an unused hole.

Shared provider or controller families whose scope ownership cannot be proved
exclusive block terminal success. No sweeping deletion of shared state.

## Protection and handoff sequence

Use monotonic per-scope phases `OPEN -> TERMINAL_PENDING -> TERMINAL_SEALED ->
TERMINAL_RESPONDED`. A pending attempt is irreversible; failures retain the
session and permit only that exact terminal retry/read. No rollback to OPEN.
The final phase is host ownership, not evidence that the guest saw the reply.
The new tuple must be pinned through signed enrollment, HELLO and every native/
delegated command and source/receipt check; the terminal bit is not a caller flag.

1. **Guest preflight.** Finish the preceding prerequisites and publish the
   immutable observer object. Retain the exact original terminal command bytes,
   id, cell, complete credentials, donor Ack and all control charges. Serialise
   against every issuer. Subsequent attempts reuse these exact bytes and cell.
   The ordinary request/cell allocator is never reset. Failure before sending
   still leaves the admitted kernel terminal-pending and its ownership retained.
   **Implement a dedicated retained terminal candidate retry path.** Current
   hfs_control allocates a fresh cell on another call; only ALLOCATE has a
   dedicated uncertain-candidate lookup/retry. SESSION_RETIRE must instead find
   the exact privately owned terminal candidate, hold its original slot/donor,
   command bytes/cell/credentials and funded package, and resume hfs_transport
   on those bytes without incrementing next_cell. A new hfs_control call with
   reconstructed payload is not a retry. Under the retirement/writer/session
   locks, allow hfs_session_destroy to revisit this one typed terminal candidate
   despite its own pending/blocked status; every other uncertain candidate
   remains fatal to closure. Never clear the generic blocked flag or unlink an
   uncertain candidate as evidence of exclusion. On failure restore the same
   candidate owner; on private successful consumption release it exactly once.
2. **Exporter pending protection.** Under its existing mutex and sole-writer
   ledger, the actual owned lib9p backend joins/checks all work for this scope,
   compares its actual guest highwater and sole donor, and protects a
   `TERMINAL_PENDING` record containing the exact original/native candidate and
   both donor bindings **before forwarding any terminal byte**. It blocks every
   other new guest header, connection/version/HELLO attempt and WAIT continuation
   for this scope. Existing final-cell retransmit/fetch is the only exception;
   it still verifies the original channel/session/live native tuple. It does
   not close the shared backend, descriptors or global native allocator.
3. **C8 acceptance.** Before complete-command ACK/dispatch, under the transport
   lock, validate sole donor, full coverage, exact guest_through against the
   pre-command watermark, no incomplete scope headers/workspace/jobs and no
   previously pending terminal request. Atomically protect candidate admission,
   donor deletion, normal one-time allocator advancement with all earlier
   issuance history preserved, and the per-scope
   `TERMINAL_PENDING` fence in one C8 transaction. The fence names exact original
   and native command hashes and candidate cells. Subsequent writes may only
   replay identical bytes for that candidate; all other issuance and reclaim
   targeting this scope refuse, including direct Service.Execute. Reads/retries
   of this final cell remain authenticated. Do not wait holding the transport
   lock for source, provider, controller or observer work.
4. **Authoritative seals.** The actual worker performs native verification and
   admits the protected mapping as today. A narrow concrete coordinator on the
   bound Producer consumes this private observation; it refreshes and checks
   the actual terminal observer state. It then durably seals the actual
   Provider, Controller and bridge capability scope in that fixed order.
   Each owner joins/serialises its own work, checks the prerequisites above
   under its own lock, and protects an irreversible scope seal bound to the
   same endpoint/controller/scope/terminal id/original request hash. Each seal
   is idempotent only for that exact identity. Provider sealing prevents
   subsequent trusted local Allocate/earmark/effect as well as remote calls.
   Controller sealing prevents new acquisition, source reservation, poll/event
   issuance or future recall membership for the terminal scope, retaining its
   immutable enrolled domain and terminal identity. Other live domains keep
   operating. The bridge seal refuses all old capability use and new grants
   for the retired scope. No change to another session's identities or rights.
   Each authoritative seal uses the closed common record:

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
   state, not an unprotected flag.
   There is no cross-ledger transaction: any partial seal or uncertain commit
   keeps C8 pending, all guest owners retained and success unavailable. Repeat
   the exact attempt only within the still-authenticated original assembly.
   Use the existing bounded pending worker for retryable pre-response refusals;
   duplicate input ACK alone is not redispatch. One worker owns the candidate.
   An uncertain ledger error that poisons the transport stays fenced: terminal
   retirement cannot clear that failure or recreate its authority.
5. **Protected terminal ownership.** After all seals are durably verified from
   their actual owners and the observer still matches, C8 protects a closed
   terminal ownership descriptor in its candidate row and advances to SEALED.
   It has exactly:

   ```
   {version:"homelabfs-terminal-owner/1",scope,endpoint,controller,
    serving_generation,retirement_id,guest_cell,native_cell,
    guest_command_hash,native_command_hash,guest_through,observer_revision,
    closure_hash,provider_seal_hash,controller_seal_hash,bridge_seal_hash,
    retained_owner:"C8_TERMINAL_CELL"}
   ```

   The three seal hashes commit exact protected owner records, read from their
   live bound owners; they are not supplied evidence or substitutes for those
   owners. `ownership_hash` hashes this canonical descriptor. It deliberately
   excludes response hashes and mapping chain: including them would create a
   signature/hash cycle. Protect the original seal records and descriptor before
   constructing the success response. A detached descriptor cannot create a
   private terminal observation. New narrow methods return inert descriptors;
   only the bound coordinator with the original worker observation may use them.
6. **Base then full response.** Construct the exact success base containing
   ownership_hash. Keep today's ordering: finishMappingBase durably records the
   actual native/guest response-base hashes and chain; only then may Producer
   sign SESSION_RETIRED. Retrying an already protected base must validate and
   reuse that exact base/chain, never append a second mapping leg. Signing
   additionally requires the same actual C8
   transport's protected SEALED descriptor and live sealed owners. After
   attaching receipt2, the worker protects immutable full native and translated
   response hashes, frames/coverage, descriptor and RESPONDED phase in one C8
   transaction before exposure. Do not sign authority based only on accepted
   candidate or missing local work. A crash after signing but before protection
   exposes no response. Full hash covers the signature; it never appears inside
   its own signed descriptor.
7. **Exporter final protection.** The backend obtains the actual full native
   reply through the original authenticated FUSE endpoint, completes all native
   frame reads and exact1.4 translation/mapping checks, and protects that exact
   native/guest response plus terminal final record before delivering any guest
   reply. Its native tuple verification is still mandatory. Its terminal fence
   was already permanent before send; unsigned payload parsing cannot reopen
   it. The guest performs pinned-key receipt verification itself. The backend
   does not mint guest provenance or grant retirement from a parse alone.
   No extra native request or RELEASE_CELL is sent to acknowledge this final
   result. Native coverage and exact guest read retries remain protected.
8. **Private guest consumption.** Actual hfs_control completion creates the
   private terminal Received owner, with the original request and full reply.
   Add a dedicated terminal consumption check: installed terminal-capable tuple,
   pinned enrolled key/controller/endpoint, exact original/full credentials,
   donor, observer id/revision/hash, SESSION_RETIRED/poll_count1 and receipt2
   mapping. Check immutable physical/session closure again, allowing only the
   known donor and dedicated terminal package as transport bookkeeping. Normal
   `admissible` must stay closed; this narrowly typed verifier cannot authorize
   SOURCE, EVENT, ordinary floor, view floor, map or cache adoption. It marks
   the registry terminal-retired and consumes/releases the guest donor, terminal
   cell/Received package and session control reservations exactly once. No
   borrowed JSON or Received reference may outlive the session. Provider
   retire_session can now return success; `hfs_session_destroy` frees only
   after all teardown preconditions are settled and drops module/session
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
| Exporter candidate pending, host has not accepted | Exporter ledger retains exact candidate/allocator/donor; guest retains pending bytes and rights. Retry identical native/guest bytes. No new cell/id or timeout-based success. |
| C8 candidate/fence protected, seals incomplete | C8 owns candidate and permanent fence; earlier seals retain their owners. No success receipt. Original live assembly can retry the same seal sequence; a missing/uncertain owner blocks. |
| Seals and descriptor protected, no base/full reply yet | C8 keeps SEALED and all exact bindings. Same live assembly may finish the same base/signature/full response once, with every owner rechecked. It cannot return a terminal receipt from a detached disk snapshot. |
| Base protected or signature generated, full reply not committed | No bytes exposed. Preserve candidate, base/chain and seals. A live retry must match the protected base exactly; mismatch fences. No re-execution of claims or floor advancement. |
| C8 full response committed; host/exporter reply lost | Original live native endpoint replays the same immutable full response, signature, cells and mapping. Backend pending record identifies the exact native candidate and cannot allocate another. |
| Exporter full final record committed; guest reply lost | Replay identical protected guest frames for the same original cell. The kernel remains terminal-pending and charged until actual private verification; other guest ingress stays refused. |
| Guest consumed success; no further guest message | C8 final cell, exporter final row and kernel registry tombstone remain their definite owners. Duplicate reads return exact bytes; no state can issue fresh rights for the old scope. |
| Exporter dies; original host endpoint/controller survives | Decode/validate the exact protected pending/final exporter rows, phases, counters, fence and original tuple as retained evidence. A replacement exporter remains blocked unless C9 supplies actual supervised continuity and cross-ledger reconciliation. Merely loading valid rows or holding matching hashes never admits a new native owner. |
| Daemon/FUSE/controller or kernel continuity lost | Persisted rows are inert evidence. No new native connection, read-only inspection, host signature or terminal descriptor restores original authority. Keep all uncertain ownership fenced; RESUME remains BLOCKED until C9 supplies full admitted continuity/exclusion. No post-crash success is claimed where that mechanism is absent. |

Task614 must implement same-live-owner exact lost-reply retry and durable
pending/final record encoding, reconstruction for validation, and refusal.
Positive replacement exporter/kernel/endpoint admission may remain blocked until
C9 supplies actual continuity/exclusion and cross-ledger restoration. C9 depends
on614; these records and refusal checks must work without that future coordinator.
A valid decoder result is inert evidence, not restored native ownership. A host
response committed before a crash is not permission to run a replacement
controller that merely knows its bytes. No restoration by absence or guessing.

The kernel registry has at most64 permanently consumed local session rows;
lib9p has at most16 session rows per backend; C8 has its existing at most64
scopes and80 native cells. A terminal session permanently spends one existing
native cell charged to its inherited ordinary/control arena (control for the
no-donor case), one existing exporter entry and one session tombstone. C8
retains at most64 ordinary and16 control cells in total, including active
sessions; there is no guarantee of64 terminal-control cells. A backend's16
session rows can impose an earlier limit. Do not return that final
native/exporter credit after guest delivery or pretend it is free capacity. At exhaustion, new
admission refuses; there is no eviction by age. This bounds final ownership
without a tombstone per historical operation.

Keep the1MiB body,8MiB C8 row,128KiB index, existing C8 memory/ledger ceilings,
exporter arenas and guest signed budgets unchanged. Store the terminal descriptor
and seals by bounded scalar/hash references in the final cell/authoritative
owner, not full duplicate provider/controller ledgers in the index. Preflight
actual maximal credential/scope/descriptor serialization against existing row,
index and receipt unsigned16KiB limits before accepting the terminal candidate.
Charge terminal fields, immutable observer object, backend pending/final staging
and Received/request packages to existing preowned control/base credits. Failure
to fit refuses before send/ACK; ordinary saturation cannot steal that capacity.
No retirement path allocates an81st cell or a second replacement workspace.

## Task614 implementation and acceptance

Implement only these owned seams; small new terminal-specific modules are
preferable to a public general-purpose evidence/restore API:

| Owner | Concrete files/seams |
|---|---|
| Negotiation and exact schemas | `internal/guestbridge/{mapped_protocol,codec,schema,response,verification,verification_v2}.go`; controlledruntime tuple admission; owned lib9p and Linux protocol/enrollment parsers/builders. Add exact dual-tuple checks, not broad minor>=4 acceptance. |
| C8 scope fence and terminal row | `internal/guestbridge/{endpoint,transport_ledger,mapping,service}.go`, private verification worker state and bridge `validation_state.go` capability/lifetime snapshot. Add typed terminal dispatch and persisted phases/descriptor with direct-execution and new-header refusal. Preserve C8 commit/base/full ordering. |
| Real owner seals | `internal/controlledruntime/{producer,producer_mapping,facts,guest_observer,guest_artifacts,state}.go` and `probe/installed.py` observer decoding; narrow scope seal/checks in `internal/controlledvalidation` and `internal/viewcontrol` persistence/dispatch. Provider and controller must themselves enforce seals on local/trusted issuance and future recalls. No success-only verifier callback. |
| Actual native bridge | `tools/integration/lib9p/src/{hfs9p.c,hfs9p.h,hfs9p_ledger.c,hfs9p_ledger.h}` and owned JSON/protocol support. Protect pending/final scope state via the sole-writer ledger; reconstruct/validate exact records but refuse replacement-owner admission without C9 continuity; preserve shared endpoint/global highwater. |
| Actual guest lifetime | Maintained `fs/9p/{hfs_session.c,hfs_received.inc,hfs_received.h,hfs_controlled.c,hfs_controlled_internal.h,hfs_controlled_artifacts.c}` plus owned receipt/attestation/enrollment helpers as needed; reproduce through `tools/integration/linux9p/export.py` and maintained owned patches/manifests. Add retained-candidate exact-byte retry, private terminal Received consumption and immutable observer/tombstone; preserve all generic kernel/vendor code. |

If implementation requires changing generic kernel/PTE/vendor code, stop and
raise the concrete need to the root; this design authorizes only owned seams.

Minimum real component tests for614 (new tests, not claimed results):

1. Extend the actual C lib9p carrier + real Go durable Transport/Producer/
   controller/provider/service harness, using genuine disposable journals and
   protected ledgers. Two guest sessions share one endpoint and reuse guest
   cell numbers while native cells remain distinct. Fully reconcile one
   session, finish exact terminal exchange through actual C, then prove guest
   cell/Received/session ownership is released while one exact native C8 final
   cell and exporter final entry remain charged. Other session must allocate,
   execute, view/control and reclaim normally on the same endpoint/controller;
   global native highwater increases rather than resets.
2. Use actual Linux maintained `hfs_session_destroy`, hfs_control/private
   Received and controlled provider helpers in the portable production-source
   harness. Exercise sole-donor and no-donor terminal cases; observer must come
   from the actual provider lists/physical hooks. Exercise full original groups,
   process/revision/namespace/idmap and separate issuer families. No test-created
   private token or unconditional physical/source success shim counts as proof
   of the missing boundary. Portable MM/native hooks remain explicitly labeled.
3. Fail at each protected boundary above, including uncertain ledger commit,
   candidate before host write, partial owner seals, base before signature,
   full response before exposure, exporter final protection and guest read loss.
   In the same live guest/backend/host assembly, lose input ACK/full or partial
   reply and retry the actual retained candidate: identical original/native
   bytes/cells, final frames/hashes, one terminal cell, monotonic counters and
   no repeated effects. Verify next_cell does not advance on terminal retry.
   Separately reopen the actual backend ledger to validate its pending/final
   records and require replacement-owner refusal without C9. Kill/replace the
   host connection and require fenced refusal, not a simulated positive restore.
   Verify pending/final binary records cannot omit the fence.
4. Reject changed id/scope/controller/tuple/key, donor/full hash, original
   credentials (including groups with same UID), guest_through/revision/hash,
   native mapping/poll index, response/kind/status and unknown fields. Reject
   detached/signed public evidence without actual worker/Received/observer
   provenance. Existing16383 and0..3 must reject terminal op/kind while retaining
   their existing allowed wire bytes/semantics. VIEW_FLOOR cannot satisfy this
   exchange; SESSION_RETIRED cannot advance either floor.
5. Each actual held result/user, uncertain/pending claim, unfunded/nonempty dirty
   pin, active lease/reservation/source, mapped alias/PTE refusal, external
   Received owner, in-flight poll/header/WAIT and unknown native write must
   refuse terminal success and preserve charges/module/session. Missing family
   or one floor below High refuses. Race another issuer/header/recall against
   the fence and verify either serialized prior obligation blocks success or
   subsequent issuance refuses. Replays with a fresh cell/id and reopening the
   old device/tag/session refuse permanently.
6. Pressure tests account permanent final cells/rows, descriptor/index maxima
   and signed guest budget. Saturate ordinary capacity across both sessions;
   preowned funded control can still reach terminal handoff. Exhaust permanent
   terminal identities/cell credits and require bounded refusal, never eviction.
   Force post-handoff guest teardown failure and verify idempotent retained
   cleanup without double unreserve/module_put or a new RPC.

`t04` still owns the funded real nonempty DIRTY_RESERVE/FROZEN/drain and exact
ordinary/view/GAP/held/unknown resource foundation; task614 must consume those
paths, not substitute clean-only fixtures. Portable Go/C success establishes
owned component composition, not installed native FUSE authentication, real
kernel PTE/TLB exclusion or deployed ABI/resource acceptance. Full custom kernel,
unchanged4MiB canary and platform/native adversarial checks remain `xz3`/`zq3`;
C9 must reconcile these terminal records before any positive restoration claim.
