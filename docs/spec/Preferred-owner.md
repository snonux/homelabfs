# Periodic preferred ownership and cooperative handoff

Status: `kz3` specification, 2026-10-31. **Unimplemented.** The separate
[bounded model](../../spec/tla/PreferredOwner-results.md) has its own finite
scope. The [9p credential/trust design](../lab/Retained-9p-authorization.md) was
selected separately on 2026-10-31. This specification provides no kernel cache
mechanism, installation/publication authority or change to the narrow
[approved native fence policy](Takeover.md#approved-isolated-fence-policy-native-vm-stop-v1).

The preferred owner A orders and projects the complete committed state in RAM.
Every admitted normal origin O can read/write every volume subject to installed
authority, permissions and readiness. For remote O, M=A supplies the memory copy;
for O=A, M is a distinct configured B. The durable fragment remains at O.
Preference changes locality, never the right to originate operations.

## Original request measurement

At the first authenticated frontend admission, assign an immutable measurement
identity `(frontend domain incarnation, request serial)` and record its original
FreeBSD daemon host. The frontend must authenticate this provenance; a forwarded
message cannot invent or replace it. Count once after validating the admitted
normal domain and volume/view binding, before ordering or permission evaluation.
Completed, queued and permission-denied valid requests all represent demand.
Malformed, unauthenticated, unadmitted or wrong-volume requests do not count.
An interrupted admitted request still counts once. Counter saturation, missing
identity or exhausted deduplication budget marks that summary incomplete; it does
not silently bias a balancing decision.

Count read, lookup, getattr, readdir, access, mutation, explicit transaction and
sync/barrier requests equally, one unit per original daemon-visible request.
Do not count replication, memory receipts, journal/apply/catch-up work, control
traffic, view-recall/drain commands, or synthetic internal requests as demand.
A drain carrying a genuinely new original frontend request counts that request
once; reinjection/forwarding of an existing identity does not count again.
Forwarding to A, retrying a transport message and retrying the same anchored
operation ID contribute no extra units, even through another origin. Such a retry
retains the original measurement binding and durable O; if the original binding
cannot be established, report incomplete statistics rather than guessing a new
original request. A genuinely new application operation ID is a new request,
including a new read invocation of the same path. Guest/kernel cache hits and
CPU mmap stores invisible to the daemon are unobservable and contribute no
count; these statistics are not a syscall or byte-rate meter.

Keep counters for `(volume UUID, original host, time bucket)` locally. For a
multi-volume request, charge one unit to each explicitly bound participant volume
(including its immutable anchor/home dependencies), and store the request's
participant vector. Repeated visits to a volume charge it once. At closure
aggregation, count the union of measurement identities across member volumes,
once per original request and host, rather than summing duplicate transaction
charges. A request touching two unrelated closures before admission records no
new joined closure until the normal affinity protocol binds it. A late binding
updates the exact affected bucket/vector or makes that window incomplete.

Use a rolling five-minute window with one-minute aligned buckets. Deduplicate
identities across forwarding, retries and all buckets for at least the active
window plus the 90-second reporting deadline; durable anchored-request bindings
retain their original charge time, so arbitrarily late retries cannot become new
measurements after dedup GC. Domain incarnation/serials cannot be reused. Closed
bucket digests, completeness flags and participant identity sets (or an exact
bounded union representation) accompany counts; a probabilistic cardinality
estimate does not decide ownership. Bound these structures with explicit credits;
budget overflow suppresses balancing, without weakening normal I/O correctness.

Publish authenticated summaries every minute, off the I/O path. Bind each to
cluster/registry generation, reporter incarnation, monotonically increasing
report serial, exact bucket start/end, volume/closure-manifest version,
completeness and origin identities. Reject old-incarnation, changed-manifest,
non-increasing/replayed, future or malformed summaries. A restarted reporter
must establish a complete window again or supply verified continuous counters;
an empty reset is not low demand. The collector uses its monotonic round/deadline
and validated aligned bucket IDs, not a reporter's unauthenticated wall clock.
Clock misalignment beyond the admitted bucket tolerance makes the round incomplete.
No per-request etcd transaction, remote metric query or distributed counter is
required. Metrics and their deduplication are advisory and separate from the
permanent correctness claims/results/source inventory.

## Decision rule and limits

These are initial policy defaults, not measured performance recommendations:

| Parameter | Selected value |
| --- | --- |
| Request window / evaluation interval | 5 minutes / 1 minute |
| Collection deadline | 90 seconds after the aligned window end |
| Minimum complete closure sample | 128 original requests |
| Advantage over current owner | at least 32 requests **and** at least 25% of its count |
| Confirmation | same eligible winner in 3 successive complete evaluation rounds |
| Minimum owner tenure | 15 minutes since activation |
| Cooldown | 10 minutes after any completed or abandoned transfer attempt |

An evaluation consumes one frozen round, once. Every host in the closure's
installed admitted set S must have a complete current-window summary, or an
explicit verified complete empty summary. A recent but differently ended window
cannot be mixed with it. Reports arriving after the deadline, an omitted outside
origin or a missing participant bucket make that round ineligible. Delayed/replayed
old reports never extend freshness or confirmation. A failed/incomplete round,
tie, insufficient samples, changed winner, eligibility change or insufficient
advantage resets confirmation. Collection is asynchronous: overlapped rounds keep
their own ends/serials; wall clock corrections cannot manufacture tenure/cooldown.
All intervals use nondecreasing controller time with restart-safe persisted
activation/attempt times or a full conservative wait after lost time continuity.

Aggregate the unique counts over the whole grow-only transaction/home/handle
closure. Select the host with the most requests **among eligible installed
placement hosts**, subject to the above conditions. If the maximum is tied, keep the current owner and reset confirmation,
including a tie between two nonowners. The closure's common A/B and compatible R must remain valid for
all members. An eligible target is in every member's installed R, is admitted
normal, has verified source/baseline readiness and required platform barriers,
compatible incarnations/configuration, and can reserve the complete projection,
accepted records, sources/claims/results/handles/pins, queues and transition
reserve. Verify a distinct compatible B in R and reserve its owner-origin
retention capacity. Refuse a transfer on partial closure discovery, incompatible
placement, pending reconfiguration, missing reserve or unknown continuity.

An outside-R host W may be the hottest requester and remains write eligible;
this never promotes it into R. Report that preference is placement-constrained
and expose W's demand. A separate replica-set admission/reconfiguration (`nr3`)
is needed before W can be an owner candidate. No implicit data rebalance, replica
movement, membership change or exclusion of a normal origin is part of this rule.
Tenure/cooldown delay preference only; actual owner failure uses recovery gates.
Safety/authority changes invalidate any metric proposal and require reevaluation.

## One resumable cooperative transition per closure

Persist one attempt identity, predecessor/closure manifest, exact target A'/B',
metrics round, reserved capacity, higher allocated authority and phase. etcd
serializes this intent/allocation and retries; HomelabFS supplies the positive
close, evidence and view gates. A CAS, lease expiry or new epoch is never a
sequencer/data/cache fence. Reports and acknowledgements bind the exact attempt,
predecessor, complete closure, incarnations, terms and source/cut manifest;
replayed acknowledgements from a previous attempt cannot advance it.

1. **Reserve and gate.** Revalidate policy and capacity against installed R/S and
   closure affinity. Close new conflicting frontend admission/dirtying, retaining
   independent drain capacity. Recall writable/read tokens, drain original dirty
   requests through the old sequencer, and obtain the positive stopped-domain
   barriers in [View barriers](View-barriers.md). Drain requests cannot wait behind
   a frozen grant whose caller owns resources needed by those drains. An already
   issued request may finish its exact old-authority durable choice before its
   origin seals. Stop future grants and publication at a named frontier. A gate
   timeout merely reports unavailable/pending; it cannot cancel an issued choice.
2. **Close and survey.** Obtain all-S sealed close reports and every used A/B
   ledger, serialized after each last old choice/sync/receipt. Persist their
   nondecreasing promises and exact frontier/choice/source manifests. Include
   outside origins with no known operation; silence/placement majorities prove
   neither absence nor completeness. Verified equivalent complete durable coverage
   plus positive exclusion may replace a report exactly as in [Takeover](Takeover.md).
   Record old read/choice/sequencer exclusion and continuously retain old A RAM
   until the successor acknowledges owned transfer. Cooperative close invalidates
   old authority rights, without removing any host from successor S.
3. **Reconcile and transfer.** Preserve the entire contiguous committed logical
   projection: data/extents/holes/sizes, namespace/permissions/metadata, aliases,
   foreign homes and open-unlinked objects. Transfer the complete issued-slot
   frontier, immutable requests/claims/floors/results, unknown choices/results,
   source graph and exact original durable locations, transaction prepare/decision/
   install/COMPLETE state, predecessor dependencies, handles/generations and all
   source/backup/snapshot/offline pins. Resolve a durable unknown COMMIT using its
   immutable grant/evidence, never new pathname lookup; retain the original O and
   stable result. A closed unanswered grant needs proven absence, not timeout,
   before NO_EFFECT. No new slot can leap an unresolved old slot. Exact durable
   origin sources may remain at those origins under verified retention promises;
   transfer does not require new A' disk/apply. A'/B' explicitly accept every
   retained-memory obligation that changes holder, with protection of transfer
   evidence and releases; a sender buffer or mere receipt is not owned retention.
4. **Install.** A' verifies the exact full manifest/projection, source accessibility
   and pins, closed frontier, transferred claims/handles/results, all old-domain
   exclusion and matching higher authority. Install the same closure-wide A'/B',
   S/R, per-volume terms/source cuts and fresh frontend generation at all required
   participants. Still-admitted origins receive successor authority and reconcile
   their old immutable claims before new admission. A' can be read ready with its
   own backing at genesis; it cannot serve a speculative or incomplete overlay.
5. **Reopen.** Obtain positive successor view-generation acknowledgements before
   reopening. Route all admitted observations to A'; execute remote operations
   with O durable+A' RAM and owner-origin operations with A' durable+distinct B'
   RAM. Old A is now an ordinary read/write origin, as are outside-R origins.
   Same-ID retry attaches to its transferred claim/result. Source obligations
   remain until protected equivalent durable coverage and explicit release.
   Release old A's transferred RAM only after exact owned acceptance; retaining
   it longer is safe. Record activation and cooldown times.

Quiescing does not require every original caller to receive a reply. A detached
wait or durable-but-unselected COMMIT is carried and conservatively completed
under the exact successor manifest; unresolved choices remain gated. The finite
model explores an interrupted issued request, its durable unknown choice, then
successor result selection before new grants. Transaction graphs use their own
prepare/decision rules; handoff never converts an unknown COMMIT into ABORT or
moves one volume away from its home/anchor closure.

Before close, a canceled attempt may resume the unchanged predecessor only after
all gates and authority are positively reconciled. After any durable close or
allocated successor promise, never roll back rights in place. Resume that same
attempt or allocate a strictly later reconciled transition. A failed/missing
transfer, lost holder continuity or unknown source blocks reopening; persistent
intent alone cannot reconstruct RAM. Preserve all failed evidence/resources and
fall through to the complete durable A'/B' crash-recovery materialization gates
when cooperation/retention continuity is lost. Preference policy cannot authorize
loss recovery, VM stops, force operations or a weaker normal acknowledgement.

## Verification boundary

[PreferredOwner.tla](../../spec/tla/PreferredOwner.tla) separates exact bounded
measurement examples, report/policy decisions and a real operation-to-handoff-
to-operation execution. Frozen source/result/barrier facts are component interfaces
from AllOrigin, ViewBarrier and OriginTransactions, not another kernel, source
codec or full normal-handshake proof. The result document gives executable
profiles, mutations, reachability witnesses, receipts and all finite limits.
The downstream controller (`lz3`) must implement these states and preserve the
normal protocol's result/claim/source/handle boundaries; this prose/model does
not provide runtime integration or any normal publication authority.
