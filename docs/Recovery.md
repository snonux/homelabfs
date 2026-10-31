# HomelabFS failure handling and recovery

Status: all-origin recovery contract, `gz3`, 2026-10-31. **Unimplemented.**
Use [normal replication](spec/Replication-protocol.md), [Durability](spec/Durability.md)
and the [all-origin takeover state machine](spec/Takeover.md#all-origin-transition-state-machine)
together. Fixed-owner v1 model/codec results and local loopback checks establish
none of the distributed behavior here. Scope remains synthetic disposable
lab/canary volumes; production migration and NFS retirement are separate work.

Ordinary failures should recover automatically when authority, positive exclusion
and complete history permit. Waiting for a needed source is a recoverable waiting
state, not permission to select a stale copy. Manual decisions apply to emergency
activation/admission/exit, intentional loss/rollback, irreconcilable authority or
history, and unsupported fencing. The [integration plan](Integration-tests.md)
describes planned acceptance; no automatic takeover has been implemented.

## Recovery inventory and operating states

Recover a complete dependency closure, including affinity/partial joins,
transactions, anchored claims/results, foreign homes and live handles, checkpoints
and snapshot/transfer pins. Preserve all members until transitive discovery is
complete. Persist the attempt identity, predecessor authority, nondecreasing
promises, exact selected/source/dependency manifests, step progress, exclusion
proofs and missing evidence. Serialize overlapping closures and resume the same
attempt after controller restart. New etcd allocation authorizes transition only.

Inventory the complete admitted origin set S and authority lineage, independently
of replica placement R. Every admitted node may originate every volume. A remote
origin W, including W outside R, durably holds its own complete operation fragment;
A retains and serves the complete committed RAM view ahead of its own disk/apply.
Owner-origin work uses A durable plus distinct B RAM. There is no fixed-partner
requirement on W+A remote completion and no placement-majority recovery proof.

Keep `Alive`, `RecoveryReady` and `ServingReady` distinct. RecoveryReady permits
bounded authenticated recovery traffic with workload access closed. ServingReady
requires one exact authority and recovered view for the full closure. CSI and
workload publication consume that final predicate. Expose source vectors, missing
slots/choices/results/dependencies, wait reason, continuity/fence state, bytes and
progress; a long recovery must not cause liveness kills that destroy useful RAM.

## Takeover sequence

The [detailed specification](spec/Takeover.md#all-origin-transition-state-machine)
defines the following seven resumable phases:

1. Discover/gate the entire closure and allocate a serialized transition against
   exact predecessors. Preserve all owned volatile obligations before a voluntary
   process or VM stop; promises and attempt records survive interruption.
2. Positively close old sequencer, all origin-choice, used partner and every
   reader/writer/cache domain, including descriptors, aliases, mmap and dirty
   buffers. Persist nondecreasing promises before close acknowledgements.
   Deadlines, etcd lease expiry and daemon gating are not positive exclusion.
3. Obtain durable sealed reports from every old S member and used A/B ledger,
   serialized after its last possible append/sync/choice. A no-work report is
   explicitly empty/closed. Replace a missing report only with positive exclusion
   **and** independently verified complete durable choice/result/dependency
   coverage **and** proof of the complete issued-grant frontier. A selected
   maximum, two replica disks, fresh epoch or fencing alone cannot supply it.
4. Reconcile the ordered slot/predecessor chain across all source streams.
   Preserve selected COMMITs; conservatively adopt exact durable COMMITs whose
   selection or result is unknown. Resolve ordinary NO_EFFECT only from exact
   durable DECLINE or complete sealed proof of closed absence. Preserve protected
   transaction decisions and unknown results; never abort on timeout/cancel.
   Missing evidence or contradictory bytes gates the whole dependent closure.
5. For crash recovery, durably materialize the full selected graph, payload bases,
   claims/results, closed frontier and source/retention obligations on prospective
   A and distinct B. Verify both current-incarnation durable receipts. Rebuild or
   apply a complete logical view behind gates; applied maxima are insufficient.
6. Install the exact new authority/actual incarnations and source manifest;
   verify old-domain exclusion, fresh handle/view generation and positive frontend
   cache barriers before publication. Only then open observations and new grants.
7. Recover remaining placement replicas from exact source replay or a pinned
   verified checkpoint plus suffix. Preserve offline and backup obligations;
   returning nodes remain recovering until reconciled, without automatic failback.

A close is not instantaneous remote crash knowledge. An origin not yet closed may
still durably choose an already granted operation; late acceptances and PUBLISHED
replies survive a sequencer crash. The survey must cover those phases, not infer
absence from new empty RAM. Unknown status remains PENDING/INDETERMINATE; same-ID
retry uses immutable claims/results before path resolution. Reusing an uncertain
ID as a fresh append/create is forbidden.

Cooperative preferred-owner handoff uses complete closed-origin/source evidence,
positive cooperative domain closure and transfer of the full committed projection,
claims/results/handles and pins. Already verified durable fragments can remain at
origins, so the new owner can serve before local persistence/apply. It does not
need a routine VM stop when cooperation proves closure. A failed RAM transfer or
crash uses the stronger durable A/B gate above. Demand policy remains separate
work; handoff never moves one member out of an affinity closure.

## Failure policy

| Situation | Required behavior |
| --- | --- |
| B fails while A and remote W are healthy | Gate new owner-origin completion. Eligible W+A operations may continue under unchanged installed authority and complete dependency/barrier coverage; changing B incarnation requires a closure transition. |
| A crashes with interleaved origin fragments | Collect all S and used ledgers or proven equivalents, including outside R. Recover the exact ordered chain; A's old disk need not contain its published suffix. |
| One origin restarts with surviving disk and continuously valid A | Positively exclude its old lifetime/cache rights, reconcile issued grants/choices/results/source pins with A's complete retained ledger and explicitly install its new incarnation. A new UUID or shorter disk does not establish readiness. |
| Transport reconnect without restart | Reconcile exact retained ledger/source/session continuity. Socket loss releases no candidate, source or result obligation. |
| Offline origin lacks report or equivalent complete frontier/coverage | Wait, even if no selected write from it is known. A potentially unique unresolved choice cannot be excluded by silence. A fully covered dead origin may retire after protected release. |
| A RAM is lost and W's unique durable fragment is also lost | If no verified durable equivalent remains, the affected chosen/replied history is unavailable under the accepted durability gap. Report it explicitly and preserve evidence; wait/recover data or request separate whole-closure loss recovery. |
| All daemons reboot, disks intact | Survey every origin and lineage, resolve durable choices and results, then durably recover A/B. Largest sequence or first two available placement disks cannot choose history. |
| Required transaction/home/anchor or result bytes missing | Gate the full dependency closure. Matching payload bytes alone do not establish replay/identity or atomic visibility. |
| Corrupt or conflicting history | Quarantine and preserve all copies; verify surviving coverage or use explicit loss recovery. Never merge by timestamp. |
| Journal sync error, disk/memory pressure or stalled delivery | No invented success/rollback. Preserve uncertain work and throttle before recovery/result reserves are exhausted. |
| Authority or cache exclusion cannot be proven | Keep workload gates closed. Coordination recovery alone never revokes old cached access. |
| Replica older than retained journal suffix | Install an authenticated pinned closure checkpoint plus complete suffix in fresh private roots; preserve old/failed resources. |

The accepted loss window includes a delayed pre-crash memory receipt delivered
before the origin learns of RAM loss. It certifies retention when emitted, not
current remote physical memory. There is no finite RPO or unconditional single-
or multi-failure availability claim. A normal remote operation needs distinct
W durable + A RAM, whereas full recovery can wait on many admitted sources.

## Bootstrap, checkpoints and safe publication

Boot independent roots first: management/guards and keys, guest OS/local ZFS,
durable identity/promises/emergency policy, and isolated coordination. Reconcile
authority and full source/dependency coverage before serving. A verified complete
checkpoint plus required suffix may replace genesis; its authenticity, exact cut,
closure and permanent identity evidence are mandatory. Existing `fsservice.New`
is genesis-only and must never have its source trimmed; successor bootstrap is
an unimplemented integration gate, not inferred from the local checkpoint codec.

Verify actual mount/session and volume identity before any workload path, share
or CSI publication. A directory, daemon heartbeat or old mountpoint is insufficient;
a missing FUSE/9p session must never expose writable raw/empty backing paths.
Nonowners route reads to A's committed logical cut, never their stale local tree.
The [checkpoint contract](spec/Checkpoints.md) and [journal storage](Journal-storage.md)
keep all-origin durable sources and immutable claims/results/IDs, homes, transaction
and snapshot pins until verified replacements and protected releases exist.

The approved disruptive fence remains only enrolled `hfs-test0`–`hfs-test2`,
graceful ACPI stop within 120 seconds, positive exact-launch stop proof and durable
restart inhibition. It excludes force/reset, f3 host, Rocky/9p and production.
Additional targets/mechanisms require a separate user decision. Cooperative
barriers also remain unimplemented. See the exact [policy](spec/Takeover.md#approved-isolated-fence-policy-native-vm-stop-v1).

Coordinated shutdown must durably drain the complete all-origin graph before
stopping any holder, then cold start through these same recovery gates; see
[power lifecycle](spec/Power-lifecycle.md). Existing f3sctl/NFS/CARP/monitor/UPS
policy stays outside scope. [Emergency mode](Emergency-mode.md) is an explicitly
authorized local-only exception with independent durable generation/admission,
not an automatic downgrade, normal ServingGrant or permission to forget sources.

## Evidence and remaining acceptance

When blocked, report exact closure/authority, known selected and issued cuts,
per-source durable coverage, missing/uncertain slots/results, old-domain fence
coverage, retained snapshots/pins and possible recovery choices/loss. Preserve
copies before changing history. Loss/rollback uses an explicitly authorized new
recovery generation, never rewritten old COMMITs, and permanently excludes old
uncertain anchored IDs from fresh execution.

The [AllOrigin results](../spec/tla/AllOrigin-results.md) include six seeded
recovery component graphs; they do not prove full ordinary-to-recovery composition
or checkpoint/trim/runtime/kernel correctness. The [boundary mapping](spec/Takeover.md#loss-restart-and-composition-boundaries)
records incoming phase classes and remaining proof/refinement obligations. Every
lab/test execution retains [per-run approval](Test-automation.md); this contract
records no new runtime or VM evidence.
