# Controlled retained-9p runtime integration

Task `m04`, planning only, against `6f9b2b3`. This selects the missing adapters
for disposable single-node validation. Nothing here activates a daemon, mount,
kernel or exporter. Successful execution remains `CONTROLLED_LOCAL` with
`publication:null`; normal publication stays closed. The accepted
[guest-kernel trust](Retained-9p-authorization.md), any-node write eligibility,
original-origin durability plus owner/partner RAM without owner-disk wait, and
same-client advisory-lock scope remain unchanged. Distributed implementation
and acceptance remain later work.

## Existing mechanisms and actual gaps

* [controlledvalidation](../../internal/controlledvalidation/README.md) owns real
  local journal execution, exact results/capability snapshots, typed gaps and
  durable ordinary floors. Its `Provider.Allocate` must run **before sending** an
  ordinary claim. Remote CALL cannot allocate. Tests call Allocate explicitly.
* [viewcontrol](../../internal/viewcontrol/README.md) owns actual sealed source
  snapshots, signed aggregate enrollment, reservations, recall/install, and
  preowned drain/source credits. All five `Facts` methods are mandatory;
  there is no installed/native implementation.
* [guestbridge](../../internal/guestbridge/types.go) requires `NativeVerifier`
  and `Attestor`. `VerifiedRequest` authenticates the observed native command,
  not physical quiescence. [Transport](../../internal/guestbridge/endpoint.go)
  has no durable save/restore API. Provider capability restoration alone cannot
  restore its allocator, cells, response snapshots or controller incarnation.
* [Linux's owned provider interface](../../tools/integration/linux9p/patches/0003-hfs1-guest-vfs.patch)
  now has a default-off built-in1.4 controlled-local implementation (`s04`),
  described in [guest admission](../spec/Retained-9p-guest-admission.md).
  Successor ordinary claims retain their full original ALLOCATE before CALL.
  The closed older semantics remain unchanged; installed/native admission and
  the complete C7-C10 lifetime/runtime composition remain separate.
* [lib9p](../../tools/integration/lib9p/src/hfs9p.h) has a real durable transport
  ledger and real positional FUSE forwarding. Its weak
  `hfs9p_controller_claim` returns ENOTSUP. No actual supervisor supplies its
  mandatory claim/protect/verify callbacks or preopened authenticated FUSE tuple.
* [daemoncli](../../internal/daemoncli/lifecycle.go) constructs a restricted
  self-client adapter, not these components. Its fresh workspace/serving
  generation is not continuity for a retained guest endpoint.

The missing glue is new implementation. Neither installing the present sources
nor replacing verifiers with success callbacks can supply it. `l04` separately
owns capability reclamation; runtime integration consumes that lifetime contract
without changing grant/description/dirty/source pin semantics.

## Selected control carrier and ownership

Keep Linux 9p → virtio-9p → owned lib9p → actual FreeBSD FUSE → shared service.
Use an **explicitly negotiated controlled-only successor** of the maintained
H9B protocol for allocation/gap control and authenticated verification receipts.
Do not add opcodes/fields to closed 1.0/1.1 or reinterpret their null publication.
The first implementation below assigns the exact token/minor/schema/features,
closed unions and literal vectors on both legs. This is an implementation of a
selected carrier, not another transport-selection task. Old versions remain
exactly supported/refused as today; none can select successor operations.

The successor adds a kernel pre-send allocation hook and typed ALLOCATE and
GAP_RESOLVE exchanges. Allocation binds full original Context, principal,
Credentials and exact Claim including intent hash. The authenticated host driver
checks the admitted session and invokes the existing local `Provider.Allocate`.
Only its durably protected acknowledgement permits CALL. CALL remains a separate
execution admission and never allocates. No filesystem operation is implemented
by the transport/backend.

Serialize ordinary-number issuance with dirty earmarks. Retain the exact tentative
request before the allocation exchange; an uncertain allocation blocks later
number issuance until the original exchange is reconciled. Never roll back,
skip or reuse it. Allocation acknowledgement loss retries the same Claim.
Unsent/rejected allocated claims use authenticated GAP_RESOLVE backed by
`ResolveGap`; PENDING/planned/effected claims cannot become gaps. Typed dirty
earmarks retain their existing no-OriginalHash representation and exact
CANCELLED-ticket-id closure. They must not pass through ordinary Allocate.

Source/event/floor verification receipts travel in explicitly versioned response
unions and are bound to exact original requests, response bytes, session,
endpoint, controller and serving generation. The guest transport privately marks
an actually received/verified response; a caller-created JSON object cannot mint
that observation. The host source verifier must first verify the actual complete
sealed source: original journal frames/reference, catalog and payload snapshot,
applied position and visibility generation. Its bounded authenticated receipt
identifies that verified source and retained owner. Sign it with the pinned
enrolled host endpoint key and verify it in the guest provider; hashes/signatures authenticate
the binding, **not** the source or physical facts by themselves. No guest callback
performs a recursive source RPC while holding a view/physical/service lock.
Prepare needed source/event receipts before publishing the response; absent or
expired continuity blocks validation rather than waiting inside a verifier.

The host supervisor owns private ledgers, a pinned operator key, endpoint keys,
exclusive source roots and process/connection lifetime records. It launches the
exact exporter executable from verified owned bytes, retains its process
incarnation/exit observation and fixed native credentials/full groups, and binds
the exact VM/export and guest boot/provider enrollment. Actual booted kernel,
modules and MM ABI must be checked by artifact admission, not inferred from a
guest-selected UUID or reported version string. These remain the adopted trust
boundaries; no new per-user trust claim or guest HfsRPC frontend is introduced.

For host-local supervisor ↔ C backend management, select an inherited preopened
AF_UNIX socketpair created before launching the exact bhyve/exporter child. Use
finite versioned, length-bounded typed frames for endpoint claim, protected-ledger
digest reconciliation and live identity verification. Bind each socket end to
that supervised child incarnation and the admitted FUSE fd tuple; transfer only
the prevalidated descriptors, then restrict their Capsicum rights. No filesystem
socket name or guest-selected reconnect path is accepted. A replacement child
gets a new socketpair only after the supervisor reconciles the old ownership;
socket loss closes admission and preserves obligations. Guest wire bytes cannot
provide native facts, select descriptors or turn an asserted boolean into a
successful supervisor response. This is new host-local adapter implementation,
separate from the existing guest 9p application/control carrier.

Bootstrap is two-phase: mount an unpublished raw FUSE shell that refuses workload
and private traffic, observe its actual kernel connection/inode tuple, construct
the verified provider/controller/bridge/transport, then atomically bind them to
that shell. This avoids claiming a real FUSE connection before it exists:
`guestbridge.New` calls `Native.Enroll`, whereas `NewPrivateControl` currently
requires an already constructed Transport. Only the supervised exporter receives
the preopened direct-I/O fd tuple. The dedicated backend never walks a guest path
to discover it. Bootstrap failure preserves the unadmitted shell/resources.

## Mandatory callback and driver map

Each row names the new runtime implementation, its real inputs and its minimum
executable check. The component checks below are proposed checks, not reported
passes. All checks use disposable local files/processes or maintained-source
primitive harnesses; actual FreeBSD/guest checks remain deployment gates.

| Boundary | Authority and bytes/state consumed | Continuity, owner and smallest check |
|---|---|---|
| controlledvalidation `Facts.Check`; viewcontrol `CheckEnrollment` | Supervisor's operator/host/guest-signed complete enrollment; actual service journal/serving generation; live supervised exporter, FUSE connection, admitted guest boot/provider and full domain inventory. | Supervisor retains exclusive ledger/root ownership. Changed process, boot, source, tuple or bounds closes admission. Driver test opens real provider/controller ledgers and rejects changed/missing live enrollment. |
| guestbridge `NativeVerifier.Enroll` | Actual mounted shell connection plus exact private inode IDs; permanent endpoint issuance ledger and exclusive supervisor ownership; controller UUID from actual bridge construction. | Enroll once; no new Go object can claim a previously issued endpoint. Process test rejects second owner, missing old ledger and replacement connection. |
| `NativeVerifier.Verify`; fuseadapter `NativeCredentials.Snapshot` | Actual per-request FUSE UID/GID/PID/unique plus supervisor-launched exporter incarnation and captured full native groups; exact still-live connection/controller. | Check every callback, including old-result reads. Exporter death/replacement revokes admission, retaining cells. Native component test changes PID/incarnation/groups/tuple and rejects all. Native headers remain exporter credentials. |
| guestbridge `Attestor.Verify` | Admitted VM/boot/kernel-provider session and immutable current numeric fsuid/fsgid/full sorted groups/process/revision/namespace/idmap from the authenticated delegated request. | No host account lookup; guest provider rejects unadmitted namespaces/maps. Credential test changes groups with same UID and rejects truncated/unmapped/foreign-session assertions. |
| viewcontrol `Authorize` | Private `VerifiedRequest(ctx)` observation, exact canonical payload and operation, pinned scope and original principal/revision; actual bridge/service capability and Unix checks remain authoritative. | No observation or wrong native/controller/op/hash denies even QUERY/ACK/replay. Real Execute test rejects externally constructed receipt values. |
| viewcontrol `CheckPhysical` | Authenticated actual guest ACK through that native endpoint; admitted running kernel's physical contract and exact complete event/domain/phase/manifest; actual complete retained source/reservation ledger. Release/cancel/retire check their actual outstanding obligations. | Trust ACK only from the admitted kernel whose STOP closes admission, joins entrants, revokes PTE/TLB/aliases and freezes complete adopted state. Busy/pin/unsupported paths cannot ACK. Host owns retained evidence. Real bridge/controller test rejects wrong/absent ACK provenance; installed-kernel adversarial checks later prove physical behavior. |
| viewcontrol `CheckUnadopted` | Same authenticated STOPPED command, exact issued STOP event, signed admitted kernel contract, old fence and **all** omitted reservations from the supplied complete ledger. | Verify old-revision replies cannot adopt during STOP or after reopen: capture revision before RPC, atomically require LIVE+unchanged revision/target at adoption/final PTE, close admission before freeze, increment revision before reopen. Omitted list alone proves nothing; bound/frozen omissions always block. Delayed-reserve component regression plus actual delayed-reply platform test. |
| viewcontrol `CheckRecovery` | Protected full controller ledger, actual service/source coverage, supervisor's complete old endpoint/boot/domain inventory, plus surviving physical continuity or positive exclusion for every old right. | Never recover from absent maps/timeouts. Disconnect clears learned ACKs; demand fresh exact ACKs. Supervisor preserves uncertainty. Restart test with actual files rejects one missing old domain/source/endpoint, succeeds only with complete admitted evidence. |
| trusted ordinary allocator/gap/floor driver | Successor ALLOCATE/GAP_RESOLVE under real native authentication; existing Provider.Allocate/ResolveGap/Retire and exact protected result/gap coverage. | Pre-send ordering, exact retries and all intermediate numbers retained. Provider owns permanent High/Floor; guest owns unresolved request bytes. Real-journal test loses allocation/gap/retirement replies and verifies one identity, no effects for gap, no floor across a pending claim. |
| Linux `enroll`, `map` | Pinned signed enrollment installed for the exact device/boot/mount/export; current kernel cred and actual mount user namespace/idmap, including every numeric ID. | Kernel provider holds module/session references and immutable enrollment. Repeated device name is no new incarnation. Production-helper check refuses wrong device/session/map and captures full changing groups. |
| Linux `enter`, `leave` | Exact live ROOT/PATH or DESCRIPTION capability, current enrolled domain/generation and native admissibility facts. | Use existing bridge1.1 view/MM gates; no physical token spans RPC. Descriptor I/O remains description-based. Provider owns only its balanced admissibility cookie. Real helper test revokes scope and balances cookies on failed/parked operations; consume `l04` lifetime semantics. |
| Linux `publication`, `target`, `view_event` | Controlled result must have null Publication. Target/event require privately verified transported response plus host's actual-source/event receipt, exact source cut and signed inventory/authority/closure/domain/session/sequence/phase. | Reject NORMAL_PUBLIC, fabricated JSON, wrong channel/cell/controller, stale source or mismatched event. Retain receipt with its owning source/event, bounded by enrollment. Same-source production callback test mutates each binding; full source verifier uses real journal/catalog/payload bytes, not a supplied digest. |
| Linux `reserve`, `unreserve` | Signed finite aggregate host+guest budget, all mounts/sessions, actual request/process/description/view/dirty/control resources and preowned drain/source packages. | One provider-wide accountant, with independent control progress; uncertainty retains charges. Free only actual retired ownership. Pressure helper test fills ordinary capacity across two sessions while funded STOP/drain/retirement still progress. |
| Linux `stop`, `retain` | Existing physical stop/session-retirement machinery and complete uncertain request/candidate/view/page/description ownership. | Stop gates first; no timeout is retirement. Retain takes durable supervisor inventory plus live kernel session/module ownership until reconciliation/exclusion; it cannot serialize dirty RAM as a replacement for preserving it. Helper test failed stop/unmount keeps fenced session and credits alive. |
| Linux `retirement`, `view_retirement`, `resolve_gap`, `retire_session` | Exact protected host floor/gap/retirement receipts bound to original family/credentials/through/request and current endpoint. Session retirement additionally requires empty physical/cache/source ownership and complete transport reconciliation. | Reclaim only under existing `l04`/view/request lifetime rules. Lost/incorrect receipt refuses destruction. Real guest decoder/helper tests cover gaps, duplicate proof, changed credentials, held result/dirty pins and unknown final cell. |
| Linux `mmap` | For the selected successor of bridge1.1, existing owned `hfs_mmap_file`/physical callbacks supply mmap; provider mmap is required only for bridge1.0. | Do not select bridge1.0 as a fallback. An unused legacy callback may refuse EOPNOTSUPP; it cannot claim physical compatibility. Full custom-MM ABI remains an artifact gate. |
| lib9p `hfs9p_controller_claim`, verifier `claim_cb` | Supervisor-owned endpoint/session registry, sole writer lock; genuinely fresh kernel connection with no old issuance, or exact protected transport snapshot digest **and** matching surviving/reconstructed host ledger. | Strong linked C integration replaces weak refusal. No Go runtime in bhyve. Reopen fds is not freshness. C process/ledger test rejects second writer, stale endpoint and unknown host high-water. |
| lib9p `verify_cb` | Real FreeBSD fusefs descriptors, native access/direct-I/O/size/node IDs plus supervised exporter and actual FUSE connection/controller enrollment on every I/O. | Supervisor owns preopened fds; Capsicum confinement remains. Raw files/replacement mount/dead exporter refuse. Native FreeBSD test is separate from portable fd-primitive fixtures. |
| lib9p `protect_cb` | Existing `hfs9p_ledger` exact bounded candidate/session/allocator/poll-trail bytes and authorized directory fd. | Exclusive flock; fsync/rename/fsync before forwarding, failures retain old/next bytes. Existing real-file ledger tests plus adapter test inject sync failure before native write. |
| host transport persistence/restoration | Actual endpoint/controller high-water, released floors/holes, cell headers/chunks/class, complete commands/native observations, immutable responses/read coverage, replacement candidate and pending jobs; provider/capability/view snapshots remain separate authoritative ledgers. | Protect transitions before acknowledging input/exposing response/releasing ownership. Restore exact endpoint only after all ledgers reconcile; reauthenticate preserved jobs, never reprepare protected effects. Failpoints lose process after accepted chunk/response/reclaim and compare exact original bytes and counters. |

## Recovery and bounded admission

Keep the host FUSE daemon alive across an exporter replacement where possible;
then the actual surviving bridge/Transport supplies its original controller and
cell state, and lib9p restores only its own protected forwarding ledger. A dead
daemon/connection is a different case. Preserve its ledger and fence successors
until every old guest domain is positively stopped/drained or excluded by a
separately authorized, verified CPU/VM fence. A new FUSE session does not inherit
old physical rights merely by loading capability snapshots. Do not enable RESUME
until complete endpoint/controller reconstruction and exact physical continuity
or exclusion have executable verification; partial restoration stays BLOCKED.

Use existing bounded serializers and fixed ordinary/control/replacement workers.
New control messages require explicit admission/classification on both legs and
must not spend all funded STOP/drain capacity on allocation/source lookups.
Never allocate a second 12 MiB replacement workspace or an 81st cell. Account
all copies: provider retained/source credits, controller WorkBytes/source/ledger
peak, kernel 16 MiB request packages, exporter ledger/serialization, host
Transport's 512 MiB ordinary + 128 MiB control + 12 MiB workspace reservations,
and permanent identities/tombstones. Defaults are not a runtime profile.
Configure `l04`'s live `MaxCapabilities` and separate finite
`MaxCapabilityIdentities` issuance/tombstone budget explicitly. Consume its typed
transient-READDIR FORGET and description/view/dirty pin lifetimes; do not add a
guest-wire or public-fsapi boolean to duplicate that implementation.
Exhausted identity/source/ledger bounds close admission; retirement does not
imply those implementations recycle every permanent charge.

The [fixture plan](Retained-9p-fixtures.md) already rejects its large-Rocky
placement and preserves a full 4 MiB logical canary. Recalculate against these
actual adapters and signed bounds in `xz3`; never shrink the canary or silently
omit restoration/retained-state budgets to fit. Do not run the three native
guests alongside the 12 GiB Rocky VM. No extra VM, kernel install, power or
mount operation is authorized here.

## Proposed implementation tasks

C1–C10 are dependency labels for the implementation tasks created from this
reviewed plan: C1 `n04`, C2 `o04`, C3 `p04`, C4 `q04`, C5 `r04`, C6 `s04`,
C7 `t04`, C8 `u04`, C9 `v04`, C10 `w04`. Each task reads this plan plus the source links above,
stays repository-only, preserves null Publication and closed old schemas, and
reports component evidence separately from platform acceptance. New packages
and test names below are proposed owned paths/targets. No task installs or
runs a lab merely because its dependencies are complete.

| Label and task payload | Exact source scope and dependencies | One observable outcome / minimum verification |
|---|---|---|
| C1: Implement the controlled successor's authenticated allocation/gap exchange | `docs/spec/Retained-9p-*`, `internal/guestbridge` protocol/codec/dispatch, narrow `controlledvalidation` adapter; new literal fixtures. Depends `m04`, `k04`, `l04`. | Exact negotiated new union invokes real Allocate/ResolveGap through actual authenticated Execute; old1.0/1.1 reject it. Real temporary-service test proves ordinary CALL still cannot allocate, lost allocation replies are idempotent, pending/effected gap refusal and complete floor coverage. |
| C2: Integrate successor forwarding and pre-send kernel allocation | Owned Linux patch/export/source/test inputs and lib9p owned parser/version inputs only. Depends C1. | A production guest request cannot send CALL before protected allocation; uncertainty retains original Claim and bars later numbering. Same-source guest/backend tests exchange literal C1 frames, lose each acknowledgement, interleave typed dirty earmarks and reject mixed versions; no manual host Allocate in this integration fixture. |
| C3: Implement supervised native identity and two-phase endpoint bootstrap | New `internal/controlledruntime` FreeBSD supervisor/credentials/enrollment adapter, narrow staged `fuseadapter` binding; standalone owned lib9p supervisor adapter interface. Depends `m04`. | One real native endpoint can bind one live exporter/controller; before binding the shell refuses all traffic. Local child-process/exclusive-ledger tests verify real process ownership/replacement refusal and startup failure preservation; FreeBSD-specific runtime calls compile, portable shims explicitly stop at OS boundary. |
| C4: Link the concrete lib9p controller adapter | New `tools/integration/lib9p/src` C supervisor adapter plus owned build/patch/tests. Depends C2, C3. | Strong controller claim uses the inherited bounded socketpair and only its enrolled fd tuple, uses existing durable ledger, and restores against a surviving host endpoint witness. Strict C production link/real-file lost-write/second-owner/sync-failure tests include wrong child/socket ownership and malformed/oversized management frames; raw-file production construction still refuses. No embedded Go runtime. |
| C5: Implement live controlled Facts and source/event receipt production | New `internal/controlledruntime` facts/source adapter; narrow sealed service/controller access for actual snapshots; successor response fixtures/codec. Depends C1, C3. | Every mandatory Facts method uses authenticated live observations and actual retained sources. Real Service.Execute/controller/journal tests accept full original provenance and reject missing old domain, false omitted reservation, altered source bytes or forged ACK context. Receipts are minted only after real source/event verification; primitive kernel observations never count as deployment proof. |
| C6: Implement guest enrollment/credential and source/event verification provider | New owned Linux controlled-provider source plus patch/export/manifests/tests; successor receipt consumer. Depends C2, C5. | Registered provider supplies enroll/map/enter/leave/publication/target/view_event from exact admitted state and privately received receipts. Same production helpers reject forged/stale/wrong-channel source/event, changed idmap, wrong device and nonnull publication; balance admissibility lifetime cookies. |
| C7: Implement guest aggregate credits and retained retirement owner | Same owned Linux controlled-provider module's resource/lifetime file and tests; narrow host receipt handling if required. Depends C6, `l04`. | reserve/unreserve/stop/retain/retirement/view_retirement/resolve_gap/retire_session have an actual bounded owner; uncertain sessions remain owned. Two-session production-helper pressure test proves ordinary saturation preserves funded control progress and failed retirement cannot release held results, dirty pins, cells or module/session ownership. |
| C8: Protect host endpoint transport state before external observations | `internal/guestbridge/endpoint.go`, new private transport-ledger adapter/tests; same-generation controller identity handoff only where required. Depends C3, `l04`. | Exact accepted chunks/responses/reclaim transitions survive process failure in real private files. Failpoint tests prove no acknowledged input or exposed immutable response disappears, no cell reuse, bounded serialization, and exclusive writer ownership. Does not itself enable RESUME. |
| C9: Reconcile and restore the complete controlled endpoint | `internal/controlledruntime` restore coordinator, narrow guestbridge restore API plus provider/view restoration integration; lib9p adapter recovery tests. Depends C4, C5, C7, C8 and terminal implementation614. | Restore original bytes/counters/controller only after exact full cross-ledger agreement and fresh physical continuity/exclusion. Real restart/lost-reply tests compare response/capability/source identities, reject missing ledger/stale controller/one unexcluded domain, and never replan protected effects. Replacement FUSE connection remains fenced; RESUME stays BLOCKED in every unimplemented case. |
| C10: Compose a repository-native disposable runtime driver | New explicit controlled runtime command/config under `cmd`/`internal/controlledruntime`; narrow daemoncli lifecycle reuse, no change to self-client/default normal serving. Depends C2, C4, C5, C7, C9. | Driver constructs actual service/providers/controller/bridge/Transport/private shell and emits readiness only after all verifiers/budgets/enrollment bind. Local no-mount assembly test uses real service/files/ledgers and production framing, verifies null publication and refusal with each missing adapter; native mount/guest behavior is explicitly unrun. |

C7 core preserves final guest/native cell and session ownership. Task514 fixes
its [separate terminal handoff contract](../spec/Retained-9p-session-retirement.md);
task614 implements and validates it before C9. Existing RELEASE_CELL necessarily
leaves a replacement cell and cannot provide whole-session retirement. C7 does
not claim positive terminal destruction from local absence or a protected host
cell alone. Its floor proof for existing VIEW_RETIRE is the distinct1.4
VIEW_FLOOR kind; the terminal opcode/feature tuple belongs only to614.

A saved PENDING VIEW_RELEASE resumed by authenticated VIEW_QUERY retains that
actual outer request. The locked controller may attach a private, synchronous
continuation for its exact protected original ticket/operation/payload. Facts
checks the actual Controller/Facts/Service assembly, full outer observation,
expiry and current native/scope/source/physical state; it never overwrites
VerifiedRequest or accepts a caller continuation flag. This allows real pending
release completion without treating QUERY as a new source or physical proof.

The C10 provider dispatch prerequisite is implemented separately by task `014`.
`controlledruntime.NewProviderRouter` freezes full enrolled scopes and their actual
private providers for one service, endpoint, serving journal and volume. Session
lookup must also match the complete original scope. It forwards the existing
claims, execution, private continuation, allocation, retirement and cleanup
contracts without changing the authenticated request context or credentials.
`Producer.Bind` accepts a direct provider or the same concrete router in both
Claims and Execution, and verifies each actual route against its own provider.
Caller assertions cannot establish this binding.

Portable component tests use two distinct GuestMount/session providers under one
admitted GuestBoot/VM, supervisor, endpoint, service, controller and producer.
The service, journal, private ledgers, source capture and signed receipts are real;
the native mount/INIT headers, guest credentials and observed kernel inventory
remain explicitly substituted operating-system boundaries. This does not prove
native guest behavior, normal authority, C6 readiness or C10 runtime assembly.
The later C10 driver must compose this router rather than substitute a generic
Claims adapter. Native transcript mapping and guest receipt verification remain
separate tasks `114` and `y04`.

`xz3` then consumes C10 and all exact adapter inputs, building the **complete**
custom Linux kernel/modules (including generic MM ABI), complete matching
FreeBSD bhyve/lib9p artifact, actual daemon and provider, with full source/config/
toolchain/artifact and installed/running-byte binding. Its concrete resource and
installation proposal must cover the unchanged 4 MiB canary, recovery workspace,
retained credits and native endpoint admission. Object compilation alone cannot
pass it. Build/admission and approved installation are separate reported stages.

`zq3` consumes those admitted artifacts for actual single-node deployed acceptance:
native FUSE fd/EOF/direct-I/O and private-inode hiding; exporter/header versus
dynamic guest credentials; two independent mounts/domains; actual STOP/PTE/TLB/
busy-pin/old-revision exclusion; full 4 MiB mapped pressure with funded drains;
same-client locks; lost replies/recovery/dead-FUSE refusals and external reply
oracle. These component tasks cannot certify those platform outcomes. Keep
`4r3`/`tr3` outside this prerequisite chain; later distributed acceptance still
requires the original any-node/origin-D plus owner-RAM contract.

## Planning verification

This document was checked against the actual provider/Facts/Config interfaces,
Linux registration and request/retirement/MM call sites, lib9p production claim
and verifier interfaces, host transport and current daemon lifecycle. Relative
source links are checked locally. No implementation test, build, SSH, VM, mount,
installation, lab recovery or workload operation is claimed by this plan.

## Host original/native mapping foundation (task114)

The host offers the explicitly negotiated
[1.4 authenticated mapping foundation](../spec/Retained-9p-mapping.md): durable
C8 candidates/leg-chain protection, staged actual Transport/Producer attachment,
receipt2 and a pure pinned-host-attestation verifier. The owned C carrier and
pure guest attestation verifier (`y04`) now exercise
that foundation with real Producer signatures. They do not create the kernel
received-response/provider authority by themselves. The `s04` controlled
provider consumes them only at the actual private transport-completion boundary
and validates exact immutable enrollment, live device/idmap and physical state.
Its component tests join the real C carrier/Go Producer/source/observer and
physical helpers, with OS/MM/native primitives explicitly substituted.
Older1.2/1.3 receipts still bind native bytes only. No decoded metadata, foundation
verifier, component result or object build establishes installed guest readiness,
protected floor/retirement closure or C10 runtime assembly.
