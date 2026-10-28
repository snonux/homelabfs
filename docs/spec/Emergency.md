# Emergency activation, admission and exit

Status: design contract for `kr3`, 2026-10-28. This protocol, its administrative
interface and its emergency journal encoding are **unimplemented**. It applies
only to disposable lab/canary volumes and synthetic data. It establishes no
runtime evidence, production authority or permission to execute an emergency.

Paul delegated routine design choices, including operator UX. The concrete UX
below uses that delegation. The acceptable initial fence is the already approved
[native-vm-stop-v1](Takeover.md#approved-isolated-fence-policy-native-vm-stop-v1),
without expanding its action set. Each actual activation, loss selection,
infrastructure installation and disruptive VM window retains its explicit approval
requirement. Approval of this design is not approval of those actions.

Read [Takeover](Takeover.md), [Power lifecycle](Power-lifecycle.md),
[Durability](Durability.md), [Transactions](Transactions.md),
[Journal format](Journal-format.md), [Backing store](Backing-store.md) and
[Security](../Security.md) together. Normal recovery uses Takeover; losing a
partner, etcd quorum or a shutdown deadline never enters this protocol.

## Scope, authority and irreversible outcomes

Emergency mode is one cluster-wide administrative state with one designated
serving node. Every other node is barred from workload reads/writes, ownership,
automatic rejoin and normal replication sessions. The survivor may publish only
explicitly recovered closures. Other closures stay unavailable; declaring a
cluster emergency does not certify all its data recovered. There is no claim
that Kubernetes control-plane quorum survives a one-node emergency.

The recovery unit is Takeover's complete dependency closure: growing affinity
and partial joins, predecessor manifests, outstanding prepares/decisions,
immutable session/request anchors and bound coordinators, foreign object homes,
moved directory descendants, lifetime ledgers, handles, checkpoints and recovery
pins. An empty source root or terminal transaction does not release a home or
anchor. If discovery is incomplete, gate every possibly dependent member and
report uncertainty; do not recover a convenient subset. Independent fully
discovered closures can remain unavailable without blocking a recovered closure.

Allocate a fresh random emergency recovery-generation UUID and transition UUID,
linked to the exact prior generation/configuration/history vectors. UUIDs do not
order competing generations. A larger epoch, newer timestamp, administrator's
local record or restored etcd revision never establishes exclusion. Generation
selection is serialized by the authenticated administrative workflow and exact
external fence/launch guards. Concurrent incompatible plans must conflict and
remain gated, rather than each declare itself the survivor.

For the initial single-host native profile, the independent host-side control
registry used with the guarded launcher must CAS one cluster-wide emergency
intent against its exact predecessor, binding the generation, designated node
and approved-plan digest before any fresh launch or serving authorization. All
enrolled launch paths consult that same durable registry; grants distinguish
the sole designated serving launch from recovery-only launches. A second runner
or survivor-local file cannot allocate another designation. Registering a new
intent cannot release old guards. This is a required broker/control extension,
not a claim existing lab scripts supply it, and adds no stop/power action to the
approved fence policy. Loss of this registry's continuity blocks activation;
replicating or restoring an old copy does not create a second authority.

Preserve protected ordinary COMMITs, transaction decisions, exact redo,
claim/binding/result identities and retirement/rejection state wherever their
evidence survives. A decision already COMMIT cannot become ABORT because the
partner is absent, INSTALL failed or a client timed out. A prepared transaction
without sufficient decision evidence waits; a lone survivor cannot invent the
old majority or prove absence from its missing local record. Reuse Takeover's
semantic selection rules wherever their real evidence is available. Without
that evidence, wait/recover another source or present a separate explicit loss
plan; the emergency flag alone supplies neither bytes nor absence proof.

A loss plan names a consistent whole-closure baseline/cut, excluded old suffixes,
known missing dependencies and uncertain client outcomes. Preserve the old
recovery unit and quarantine branches. Select all effects of a transaction or
none in the new loss lineage; never expose a dangling home or half a decision.
This can deliberately omit an old committed outcome, but cannot rewrite its old
decision as ABORT or certify it never occurred. Old anchored IDs from excluded
or uncertain history remain rejected/indeterminate through durable generation
rejection mappings, never executable as fresh requests after an earlier baseline
is restored. A missing request list is not proof the list is exhaustive. Loss
approval must cover that uncertainty and the whole dependency closure.

When old history is missing, absence of a claim cannot authorize registration
of an unknown caller-provided session nonce. The current `RequestID` contains no
recovery generation: a new generation flag, a list of discovered old IDs or
randomness alone cannot prove that new registration excludes all old IDs. Loss
activation must preserve complete permanent rejection authority, or establish
a genuinely fresh session/anchor/principal identity namespace that cannot alias
any old `RequestID`; old IDs remain rejected/indeterminate. The follow-on schema
task must specify and verify that versioned mechanism, including registration
and routing before pathname resolution. If neither exclusion proof is available,
loss activation remains unsupported and gated. This adds no restriction to
non-loss activation with complete verified identity history.

If excluded redo might already have modified payloads, install the selected
paired baseline plus complete selected suffix into a new private destination.
An older catalog on top of contaminated live payloads is not rollback. Missing
clean bases, homes, anchors or permanent rejection evidence blocks activation.

## Concrete operator interface

Specify a future `hfsctl emergency` client over the restricted local Unix-domain
administrative socket described by Security. Commands below are interface
contracts, **not installed commands**. Read-only status/preview requires status
privilege; effects require a separate recovery-administrator privilege with OS
peer credentials. A transport or replica credential cannot invoke them. Remote
use goes through authenticated administration, not an unrestricted replica API.

| Command contract | Result and limits |
|---|---|
| `status` | Mode, generation/designated node, phase/revision, exact closure/volume gates, missing evidence, fence/guard coverage, admitted recovery peer, retained bytes/budget and last durable progress. No file contents, keys or credentials. |
| `preview activate --survivor NODE --scope MANIFEST --out PLAN` | Inspect available local evidence without executing fences, opening serving gates or installing policy. Produce immutable canonical plan bytes, digest and a human-readable comparison. Missing remote evidence is reported, not obtained through an unapproved lab operation. |
| `preview admit --peer NODE --closures MANIFEST --out PLAN` | Bind one named existing member's identity/new incarnation, recovery-only scopes, staging, source generation, catch-up pins, launch constraints and budget. No normal role is granted. |
| `preview exit --peer NODE --out PLAN` | Enumerate every closure to reopen, recovered pair, final barrier procedure, coordination lineage, offline obligations, authority/cache transitions, prerequisites and exact effects. |
| `apply --plan PLAN --approval APPROVAL` | Verify the exact digest and authorized action/scope, CAS the expected durable predecessor revision, persist intent, then execute only that plan. Identical retries query/resume its transition UUID. A changed target, cut, loss policy, incarnation, fence domain or budget requires a replacement plan and appropriate approval. |
| `status --transition UUID` / `resume --transition UUID` | Reconcile the same durable attempt and evidence. Resume cannot enlarge its authority; expiry of execution authorization or a new disruptive window leaves it waiting for approval. |

Plans include cluster/run identity, expected control revision and full predecessor
lineage, proposed generation/designated node, sorted closures and dependency graph,
selected histories/baselines, unavailable volumes/outcomes, all old serving/cache
domains, enrollment/policy/verifier digests, exact fence/launch operations,
coordination handling, format capability, bounded resources/deadlines,
preservation and restoration inventory. The preview distinguishes normal
owner-durable/partner-memory guarantees from emergency local-only guarantees.

The operator explicitly approves actual activation and any known/uncertain loss;
a normal-mode approval or generic `--yes`/`--approved-run` cannot stand in for
either. Named admission and exit are manual administrative actions, not automatic
health transitions. Approval binds the reviewed plan; it is not a claim that a
client-side file is a cryptographic credential. The future authorization codec
must bind authenticated approver identity, action, plan digest and permitted
execution window, audit it independently, and reject replay/conflicts. No new
signing service or broad fence authority is presumed by this UX.

Local document/format review, unit tests and simulator checks require no emergency
approval ceremony under the routine-work delegation. Infrastructure installation
and each critical runtime exercise follow
[Test automation](../Test-automation.md); do not use preview to trigger SSH,
mounts, discovery workloads or disruptive infrastructure checks without their
own applicable authorization.

## Independent durable control and restart

Store emergency control on independent local node/cluster storage, outside every
per-volume rollback and outside HomelabFS workload paths. Use the approved run's
control binding in the native lab; verify its actual dataset/mount identity before
access, never fall back to an OS directory if missing. Keep the plan, approvals
and exact control/fence evidence externally as well. Broker launch guards remain
on host-local storage outside VM disks, with the external copy Takeover requires.
Boot disks, local keys, admin endpoint and control must work without workload
publication or etcd quorum. The periodic f2 lab receiver is not live authority,
a launchable replacement or a self-contained application recovery checkpoint.

The future strict versioned control record must retain at least:

| Field group | Required semantic content |
|---|---|
| Identity/lineage | Cluster/run UUIDs, record version, transition UUID, previous record digest/revision, emergency generation, predecessor normal/emergency and coordination lineage, designated node UUID, authorized current incarnation and launch binding. |
| Policy | Cluster-wide deny-default emergency override, complete barred member list, allowed recovered closures/views, named recovery-only grants/revocations, normal-member obligations, format requirements and approved-plan/authorization references. |
| Recovery | Complete closure/predecessor manifests, selected cut/history and baseline references, unavailable/excluded ranges and request uncertainty, loss authorization/rejection mapping, applied/visibility state, durable source/payload/decision/anchor graph and pins. |
| Execution | Phase, per-step intent/completion, wait reason, exact fence/guard/artifact bindings, current capability/session revisions, coordination reconstruction, final pair barrier and exit readiness/publication vector. |
| Retention | Per-volume and aggregate budgets, reserved completion/recovery space, durable coverage/archive manifests, snapshot/transfer/offline pins and last verified progress. |

Publish immutable records with exclusive creation, file sync, atomic pointer
replacement and parent-directory sync; retain the predecessor and referenced
artifacts. Serialize transitions and GC. An ambiguous sync result keeps gates
closed and the candidate/predecessor preserved. Partial closure fan-out cannot
open one member; the complete published vector and independent control decide
readiness. A phase label alone is never proof of a protocol predicate.

Every start first loads identity, highest promises, emergency restrictions and
current guarded-launch evidence **before** network acceptance or mount/workload
publication. The designated node starts `RECOVERING_EMERGENCY`, replays the
complete selected durable suffix and reconciles its current incarnation. Other
members start barred even with old normal registry entries or valid transport
keys. Unknown/corrupt/missing control or conflicting lineage fails closed; no
“normal by default” interpretation or formatting of a replacement control file.

A survivor restart preserves emergency generation, local-only durability mode,
barred peers and pending transitions; it never exits or loses offline obligations.
Its new incarnation cannot inherit old receipts or cached execution rights.
Verify continued exclusion of every old serving domain, including the survivor's
own previous descriptors/mappings, before publishing a new serving generation.
For the initial native policy this can require positively stopping the old
survivor VM launch and a separately authorized fresh recovery-only boot. Sync or
transfer every accessible unique volatile tail before a voluntary stop. A daemon
restart, guest reboot report or apparent empty process list is insufficient proof.
If that disruptive window is not approved, remain gated; do not silently request
a new stop. A crash-completed old stop can be reverified under the same operation
and guard, but cannot be invented after supervisor continuity is lost.

Restoring an older control snapshot cannot lower promises or remove an external
bar. Reconcile against the current external plan, generation and guard registry.
Loss/rollback of all independent witnesses is an authority-reconstruction incident
requiring a fresh reviewed plan, not automatic emergency restart.

## Activation sequence and fencing evidence

| Phase | Preconditions, effects and completion |
|---|---|
| `PLANNED` | Validate current plan/approval, independent control/key roots, supported future format and exact scope. Gate all cluster workload publication and normal acceptance at cooperating nodes. Preserve accepted/uncertain work; unreachable nodes are still potentially serving. |
| `EXCLUDING` | Durably register transition intent and deny-default admission externally and locally. Revoke old sessions at reachable members. Obtain positive exclusion and durable restart inhibition for every barred node and every prior conflicting serving domain, including survivor old views. Preserve unique volatile collateral tails before any voluntary stop. |
| `SELECTING` | Recover/discover full closures and semantic old histories using actual evidence. A missing dependency/absence proof leaves that closure unavailable. Obtain separate explicit loss authority if needed; preserve original copies and select a consistent clean replacement recovery unit. |
| `LOCAL_DURABLE` | Sync complete selected closure baselines, suffixes, requests/results, decisions/prepares, homes/ledgers, affinity, pins and fence/control evidence on the designated node. Persist new generation/authority and exact available/unavailable scope. No partner or majority is asserted. |
| `ACTIVATING` | Replay behind gates, finish all installs and shared visibility/cache barriers, verify mount/view identities and the full activation vector. Publish emergency activation in durable control and each included closure before any ServingReady assertion. Partial installation remains gated. |
| `EMERGENCY_ACTIVE` | Only designated/current incarnation and explicitly ready closures serve local-durable operations. All other nodes and unrecovered closures remain barred. Monitor guard continuity, authority, retention and local sync failures. |

The initial acceptable external proof is exactly Takeover's approved
`native-vm-stop-v1`: enrolled `hfs-test0`–`hfs-test2` only, exact host/VM/dataset/
config/launch/incarnation/view bindings, independent host management and restricted
broker principal, durable launch inhibition before stop, bounded graceful ACPI
stop (120 seconds total, 10-second RPC bound, at most three transport attempts
inside that window), positively supervised exact-launch exit/no execution domain,
and verified signed canonical attestation plus continuing guard coverage. Bind
the proof to this emergency transition, predecessor/successor scope and approved
plan; an artifact from an unrelated attempt cannot be relabelled. Reattestation
may reuse an already revoked launch only after verifying current inhibition and
exact new vector coverage. Excluded stale etcd/client launches stay inhibited.

Network silence, isolation of replication alone, guest SSH, a stop-command return,
clock expiry, an etcd majority or a survivor declaration cannot substitute for
that proof. Losing management, a hung/paused guest or an unknown guard blocks
activation, with inhibition/evidence preserved. No automatic force-kill, reset,
physical-host action, Rocky/host-FUSE/9p or production action is included. A
different fence profile would need concrete reviewed options and Paul's separate
critical decision; it is not selected here. Native tests must not run alongside
the 12 GiB Rocky VM. No infrastructure inspection was performed for this design.

Inventory every role/workload in a stopped VM, including unrelated closures and
isolated etcd. Whole-VM fencing is not per-volume revocation. If any required
domain is outside enrollment or any only surviving tail would be destroyed,
refuse the plan. Existing helpers do not yet establish these broker proofs.

## Local-only commit and visibility

Emergency serving changes the protection predicate deliberately: the designated
node must durably sync the complete ordered operation, commit/decision and all
transitive recovery dependencies locally before protected completion. No partner
receipt or second durable copy is promised. Local sync failure/ENOSPC gates
admission/completion and preserves uncertain operations; it cannot return success
from buffered receipt or backing-file apply. Loss of this node/storage can lose
every emergency change absent from verified external recovery sets, with no
finite loss-window or redundancy guarantee.

Preserve the normal exact-ID redo, contiguous distinct histories, request-claim
before resolution, immutable binding/coordinator and terminal result rules.
Emergency transactions still use prepare, one irreversible decision, all installs
and one closure visibility barrier; local durability of **every** participant and
decision dependency replaces the normal partner protection predicate. Local
durability of one participant cannot expose a partial transaction. No old
normal-mode outcome can be resolved by retroactively applying this new predicate.

Reads/stat/permission checks wait for selected committed/applied dependencies.
Sync barriers capture prior admission order and cover homes/decisions as Durability
requires; close is not fsync, guest-buffered stores are not daemon writes, and
fsync does not create an application transaction. Cache/mmap/alias and mount
publication requirements remain. A committed operation stays gated if visibility
cannot be completed. Emergency mode does not grant unsupported platform features.

## Recovery-only admission of one named peer

Automatic join, failback, owner election, normal replica handshake and unsolicited
snapshot/journal requests are denied throughout emergency and pending exit. Close
old established sessions; checking only new connections is insufficient. Normal
registry/WireGuard membership, old UUID/epoch or a peer claiming a higher value
does not bypass the emergency override.

Manual `admit` durably grants a single named existing member recovery-only access
to explicitly enumerated complete closures under this generation. Bind cluster,
node and transport identity, new launch/incarnation, grant UUID/revision, permitted
source/destination and history, limits and plan. A fresh restricted launch must
boot with workload/etcd-old-cluster startup inhibited. Its old-launch revocation
remains permanent. A stale live-memory restore is never that launch. An arbitrary
fourth node remains unsupported as a replacement under Takeover's initial fixed
membership; this workflow does not invent a membership-reconfiguration protocol.

The receiver may authenticate, report/preserve old evidence and receive verified
checkpoints/journals into private gated staging. It may not originate mutations,
serve workload reads, advertise readiness, vote normal authority, ACK normal
client work or activate old etcd membership. A recovery grant is not a normal
synchronous role. Reconnect/restart must revalidate exact grant/current incarnation
and fence state; a changed incarnation needs reconciled grant authorization,
never a handshake edit. Revocation persists first, closes sessions and preserves
transfer pins until ownership/coverage is reconciled; a timeout never releases
them automatically.

Compare semantic histories and quarantine divergent old suffixes before installing
new state. Old normal or excluded emergency suffixes cannot flow back into the
survivor as fresh commits. Fetch a complete paired clean baseline and ordered
suffix when replay lacks a required graph or payload; verify hashes, identities,
lineage, object homes, anchors/floors, promises, decisions and closure manifests.
No skipped gap, zero payload or timestamp-based merge. Keep source and destination
transfer pins until verified durable replacement. Interrupted copy stays private
and resumes from verified durable coverage, never a guessed received maximum.
The survivor can continue emergency writes while bounded streaming catches up;
no receiver progress alone authorizes exit.

## Final barrier, coordination and explicit exit

Exit is another manual plan-bound transition; no peer arrival, healthy etcd or
expired emergency timer performs it. Require every cluster volume intended to
return to service in the exit manifest, including full dependencies. Volumes not
ready stay explicitly unavailable under durable configuration/publication gates;
exit cannot quietly restore their old normal authority. Every reopened closure
must have the same ready owner/partner pair throughout its members.

1. Persist exit intent, complete closure reservation and deny-default policy.
   Close new mutation/transaction admissions on the survivor for all reopening
   closures; bound and resolve accepted work without inventing aborts. Reconcile
   partial joins and freezes against the immutable predecessor vector. Preserve
   old serving/cache domains and positively exclude them wherever the planned
   configuration/view transition requires, even if retaining this owner.
2. Capture a finite final cut vector B covering all normal selected history and
   all committed emergency operations, session results, prepares/decisions,
   home/ledger state, handles, affinity, control/fence and offline obligations.
   Resolve every slot affecting B; unavailable decisions keep exit blocked.
   Keep client publication gated while replay/cache transition is incomplete.
3. Require both designated owner and named peer in their actual prospective
   incarnations to **durably hold** B's complete recovery graph and payload bases.
   Sync owner/reporter/emergency evidence and decision replacements; verify
   both durable receipts and complete contiguous coverage, not received/applied
   maxima or RAM ACKs. Install/apply B and shared closure visibility behind gates
   on both. A restart invalidates fresh readiness, not earlier durable decisions.
4. Reconcile/rebuild dedicated coordination from independent local roots. Bind
   its new logical lineage to this generation, exact predecessors, promises,
   guards and approved exit plan. Reconcile or supersede stale allocations with
   fresh configurations/epochs/terms; a reset etcd revision is not a higher
   authority. Never merge old etcd members/quorum into the new lineage. Use only
   controlled bootstrap/recovery procedures; coordination quorum and storage
   pair are independent prerequisites.
5. Durably install successor normal authorities for every reopening member on
   both nodes, with real distinct owner/partner, exact incarnations, retained
   emergency predecessor evidence, B and new coordination authorization. Bind
   one exit/closure visibility vector and positive old-path fence evidence.
   Reject early normal acceptance before that vector is complete. Keep all
   excluded members barred and their async/offline coverage obligations pinned.
6. At both nodes persist `NORMAL_REQUIRED` for the successor vector: every new
   protected operation/completion requires current partner retention and local
   durable evidence under the ordinary Durability contract. Finish and verify
   cache/mount publication barriers and matching activation authorization.
   Persist the matching independent `EXIT_COMMITTED` record and admission policy
   **before** releasing any workload gates. Only now assert ServingReady. The
   stronger two-durable-copy B barrier does not change ordinary fsync afterward.

The successor normal authorities retain the emergency recovery generation and
link its exact history; exit is not a return to the pre-emergency generation.
Coordination reconstruction uses that already explicitly authorized new recovery
lineage. A further loss selection or designation change requires its own new
generation/approval; it cannot be hidden inside an exit or bootstrap retry.

There is no local-only completion after a node has installed `NORMAL_REQUIRED`.
An interruption from exit intent through gate release resumes the same attempt
and keeps gates closed until the complete vector is revalidated. Do not erase
intent, roll back modes independently or automatically resume emergency writes
because coordination/pair readiness failed. Abandoning/superseding exit needs a
new reviewed administrative transition that reconciles all partial authorities,
pins and fences. Late messages cannot revive either predecessor serving mode.

Two ready replicas permit degraded normal operation within the admitted
three-member set; they are not a newly invented two-member configuration. The
third remains barred until a later manual recovery-only admission, full catch-up
and explicit role authorization. Completed exit does not make a formerly barred
member eligible for automatic normal rejoin. Persist those exclusions in normal
admission authority until individually cleared. Subsequently an unbarred known
normal member can use ordinary automatic recovery (M02), without automatic
former-owner failback. Extra membership replacement still needs its own protocol.

Stale etcd launched from old disk/snapshot is barred by launch guards, transport/
credential restrictions and lineage validation; quarantine it rather than trust
an old quorum, watch event or lease. All nodes check local emergency/exclusion
state before consulting normal coordination. Old etcd data is evidence to
reconcile, not authority to remove the bar. If a new coordination lineage cannot
be established safely, exit remains blocked while the current safe mode/gates
persist; etcd recovery alone cannot restore missing storage evidence.

## Retention, archive and stop-writes policy

Retain the complete emergency graph for every offline/barred assigned replica.
The obligation persists across generation and configuration changes; do not drop
membership to free space. It includes payload bases, redo/decision/request/session
evidence, permanent floors/tombstones/rejection mappings, affinity/homes, promises,
fences, pins and control lineage. Checkpointing one participant or a receiver's
memory ACK cannot release it. GC/retention and closure transitions serialize.

Select **stop writes by default**. A concrete run declares per-volume and aggregate
allocated-byte ceilings, protected completion/recovery reserves, memory queues,
retained archive/snapshot/staging charges and maximum mutation/backlog bytes.
Admission forecasts the entire closure's worst-case redo, result, checkpoint and
decision completion before accepting work; reserve emergency control/exit space
separately from workload credits. Reuse the admitted canary capacity profile,
never assume emergency mode adds capacity. Its initial thresholds are
[2.5 GiB aggregate or 75% of a base allowance](../Canary-volumes.md#storage-quota-translation-and-readiness),
whichever is reached first, with a 3 GiB hard planned-use ceiling and 1 GiB
protected headroom inside the 4 GiB per-node reservation. Individual child caps,
all other resource equations and bounded fixture limits remain binding. These
are design candidates requiring actual admission, not measured available space.

Before a threshold/reserve violation, stop new mutations and return explicit
capacity/backpressure errors before admission. Finish or reconcile accepted work
within reserved resources, preserving committed outcomes; never abort COMMIT to
free space. Reads may continue only with valid authority/freshness and adequate
resources. Threshold uncertainty, sync failure or underestimated completion cost
gates the affected closure and reports required bytes. Snapshot holds, failed
resources, encrypted export staging and outer lab-replication growth remain
charged; do not prune others' snapshots or change snapshot ownership.

Archiving is optional only under an explicitly configured, bounded recovery
profile. Before deleting a local segment, durably publish and verify a complete
immutable archive recovery set: exact typed frames or a compatible paired closure
checkpoint plus every needed suffix, payload base, dependency and rejection
mapping, hashes/lengths, lineage, holds, destination identity, available independent
keys and durable retrieval/coverage manifest. Verify retrieval/integrity within
the declared budget and pin that source for each offline obligation before
retention advancement. A copy command, timestamp, catalog alone, best-effort
upload or snapshot of only one home is insufficient. External tooling owns
snapshot/backup scheduling; archival cannot imply an unimplemented uploader.

Replacing literal replay with a checkpoint/baseline changes the peer's recovery
method and must be declared in the plan. If literal history is promised, retain
its exact records in the archive. No admitted archive destination/capacity or
failed verification means keep the local history and stop writes. Copies on
the survivor's same disk can relieve a layout quota only if separately budgeted;
they add no disk-loss protection. Even an independent archive protects only its
verified cut; later emergency writes still have one-copy exposure. Disk loss
without a complete external recovery set leaves the closure unavailable and
requires a new explicit recovery/loss plan, never fictitious redundancy.

## Format and implementation gates

**Version 1.0 cannot implement this design.** Its `Authority` requires a distinct
partner, `Binding`/configuration vectors contain normal Authority, and normal
COMMIT/DECISION/RESULT, RECOVERY_SELECT, retention and checkpoint validation depend
on normal partner ACCEPT/majority evidence. A missing partner, self-partner,
fabricated ACCEPT, opaque authorization blob or relaxed v1 validation is invalid.
This specification changes no shared codec, record kind, storage format or model.

Require a separately specified mandatory format/protocol upgrade before any
emergency activation. The follow-on schema task must define a typed
`EmergencyAuthority` (cluster/generation/transition, designated node/incarnation,
closure/history vector and authorization lineage), a tagged normal/emergency
authority reference, and typed activation, local commit/decision proof,
admission/revocation, checkpoint/retention and final-exit evidence. Emergency
protection must have its own locally durable dependency predicate; v1 normal
ACCEPT keeps its current meaning. All authority-bearing bindings, anchors,
results, recovery selections, manifests, segment readers and receivers must
validate the tag and exact predecessor graph. Existing frames retain original
bytes/creator identities; copying never rewrites an old normal proof as emergency.

Use a new mandatory journal/protocol version with explicit migration markers and
feature negotiation; assign its exact byte schema and version in that task,
with strict bounded parsing and positive/negative vectors. No skippable extension
or silent downgrade is allowed. Upgrade an entire dependency/recovery unit behind
closed gates, retain an independently recoverable old unit, and confirm every
future serving/receiving replica, checkpoint/archive/snapshot reader and recovery
tool understands the new version. Exit does not downgrade history to v1: the
new normal stream still references emergency evidence that only upgraded readers
can interpret. An old barred peer must upgrade before recovery-only admission.

Keep responsibilities separate: the admin/runner records approved intent; the
independent control registry enforces generations/admission; the broker proves
execution exclusion; replication/recovery verifies selection/commitment; storage
persists exact bytes; adapters enforce view/cache gates. No layer may mint another
layer's proof from a successful RPC or status bit. Implementation tasks must cover
the versioned schema/upgrade, emergency controller/admin API, broker integration,
retention/archive verification and approved scenario evidence before publication.

Task `qw3` specifies those exact schemas and loss-identity rules. Task `rr3`
supplies version/compatibility foundations; `rw3` implements the emergency codecs
and reader/upgrade gates before `lr3` activation can be implemented. Existing
`mr3` covers named admission, exit and archive/stop-writes behavior, and `sr3`
retains the separately approved infrastructure evidence gate.

## Planned validation and interruption outcomes

| Scenario | Required safety evidence; no runtime pass claimed |
|---|---|
| E01: activation without positive exclusion | Refuse despite quorum loss, valid admin identity, old etcd majority or higher claimed epoch. Wrong/partial/replayed proof, hung stop, unavailable management or missing guard cannot activate. |
| E02: survivor restart; old peers boot | Persisted generation/designated node/deny policy wins before networking. Replay exact durable emergency outcomes; new incarnation/cache proof is required. Barred old launches/etcd cannot resume, and no old peer autojoins. |
| E03: named admission and every exit edge | Peer remains recovery-only through partial baseline/suffix, grant restart, lost replies and final-barrier crash. Both durable B graphs precede successor installation; NORMAL_REQUIRED on both and complete exit publication precede workload release. No fallback local-only completion. |
| E04: capacity or survivor disk loss | Reject/backpressure before reserves; preserve accepted outcomes/pins. Verify any archive completely before trim. Disk loss recovers only verified external cuts or stays unavailable for explicit loss choice. |
| M02 / SEC05 | A known unbarred normal peer can recover automatically after normal reconciliation. An emergency-barred peer with valid transport/old configuration cannot; recovery-only grant never permits serving or ordinary ACKs. |
| Multi-group dependency and loss | Partial joins, moved directories, foreign homes, immutable anchors, protected COMMIT with incomplete INSTALL and unknown requests all gate the complete closure. No half-transaction, duplicate retry or home reclamation. Explicit loss preserves old outcomes as evidence and excludes their IDs from reexecution. |
| Stale lineage and upgrade | Old etcd snapshots/quorums, restored control, stale broker evidence, unsupported emergency version and v1-only receivers fail closed without rewriting/deleting bytes or lowering promises. |
| Interrupted transitions | Crash before/after every intent, sync, pointer publication, fence, grant/revoke, barrier receipt, normal-mode install and gate release reconciles the same transition. Partial publication cannot serve; failed resources and all required histories remain available for inspection. |

Collect approved-plan/authorization digests, real identities and resource bounds,
control revisions/lineage, exact fence/guard artifacts, selected closure graph and
known/uncertain outcomes, durable emergency proofs, grants and denied attempts,
final pair receipts, coordination/successor vectors, mode/admission/ServingReady
changes, retention/archive coverage and remaining resources. Compare with an
independent acknowledged-work/request oracle, including uncertain replies,
object-home integrity and shared visibility. Native cases use guest-local FUSE
only; Rocky/9p and physical-host power remain separately unsupported profiles.

Extending the existing normal-commit TLA+ model is optional for this specification;
that model does not prove emergency activation, local commit, exclusion, admission
or exit. A future extension should model failed/unknown fences, durable control
restart/rollback, loss-lineage boundaries, full dependencies, partial exit and
retention. No model/checker or daemon/kernel/VM test was run to write this design.
Every future critical infrastructure exercise or disruptive window needs its own approval.
