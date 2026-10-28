# Synthetic canary acceptance profiles

[Performance](../Performance.md#canary-acceptance-profiles) ·
[Durability](Durability.md) · [Capacity](../Capacity-plan.md) ·
[Run approval](../Test-automation.md)

## Status and decisions

**Paul approved these numeric calibration criteria on 2026-10-28. They are not
calibrated, admitted or executed.** These deliberately small targets make a
concrete calibration baseline. No distributed HomelabFS measurements support
them yet. Historical loopback/transport PoCs do not satisfy any row here.
Approved numeric profiles and matching measured evidence are required by
[deployment readiness](../Deployment-readiness.md#release-acceptance-and-independent-recovery).
An approved criterion is still neither permission to install nor approval for
a test run. Calibration and acceptance each require an exact, separately
approved plan. A failed calibration proposes a reviewed revision; it must not
silently lower a threshold until a run passes.

These are canary criteria at the stated load, not production SLOs, RPOs or RTOs.
They do not certify real applications, the entire integration matrix or future
production migration. Preserve production PVs, consumers, data, NFS, backups,
received sinks, fencing and power automation. Use only deterministic synthetic
bytes in fresh run-owned identities/paths. Alias and parent/child fixture mapping
remains subject to the volume-policy decision and later fixture specification;
this proposal assigns no roots, groups or transaction semantics.

## Evidence profiles

| ID | Path and selected scenarios | Evidence boundary / present status |
|---|---|---|
| N-H | Native FreeBSD FUSE inside each of three 2 GiB lab guests; three healthy replicas | Healthy latency, throughput, lag and P1–P5 semantics, subject to specified supported mappings; not run |
| N-D | Same native path, third replica deliberately unavailable; owner and ready synchronous partner remain | Degraded latency/throughput and bounded reconnect/replay; separate approved native fault plan; not run |
| N-R | Native daemon/replication-link/individual test-VM failure and recovery with surviving authoritative history | Recovery stages, replay and rebuild inside the single-host lab; not independent host/power evidence; not run |
| S-H | Non-disruptive Stage 2: Rocky process → deployed 9p → bhyve → host FUSE → replicated ZFS | Healthy end-to-end smoke only; S placement is **not admitted** by the conservative capacity plan; not run |
| I-H / I-D / I-R | Isolated deployment path: healthy baseline, deliberately degraded replicas, K01–K05, cached descriptors/mmap fencing and recovery | Requires separately admitted isolated targets and their own plan; not run |
| I-P | Isolated independent physical hosts: host loss, coordinated shutdown/cold start and independent restore | Physical fixtures not established here: **not run**, physical reliability claims withheld |
| R-T | Separate f3 Rocky/9p loopback transport probe, without distributed journal/replicas | Comparison evidence only, never acceptance of HomelabFS replication; R placement is **not admitted** by the conservative capacity plan |

Keep reports separate by profile, path, host/guest, volume, role and artifact
set. N includes no 9p. N and R never run together; inspect actual VM state in
the later approved lifecycle window. Three guests sharing f3 cannot establish
independent physical-host failure. Unavailable physical fixtures do not justify
production disruption or prevent reporting the narrower smoke result. S-H
injects no faults, transfers no owner and restarts no host/guest/daemon. Its
degraded and recovery rows remain not run; I supplies those separately.

## Common workload, version and cache parameters

All limits apply to the **whole selected fixture set**, not per PVC or alias:
4 MiB maximum live regular-file payload including temporary/state files,
64 regular files, 32 directories, eight symlinks, 64 KiB maximum per file,
256 workload mutations and a five-minute fixture phase. The 256 includes
seeding, temporary files, replacements and semantic probes. A short write's
remainder is another invocation and consumes the remaining byte/mutation budget.
At most 16 MiB total application write payload is available under the
`256 × 64 KiB` envelope. Metadata, barriers, retries and physical amplification
are charged separately. No unbounded application scans or real databases are
included. P1–P5 refer to [synthetic patterns](../Canary-patterns.md).

Select one workload or recovery scenario per run unless a concrete operation
ledger demonstrates the combined cases fit all limits. A proposed allocation
for a latency run is at most 64 seeding mutations, 64 measured mutations and
128 mutations for P1–P5/permission/alias probes; a recovery run instead reserves
16 of its 256 mutations for backlog construction. Do not assume a semantic
suite fits by multiplying these allowances. Count actual daemon requests too,
since one syscall may split into several. Explicit cleanup is separately
bounded to the manifest's own objects and must fit the overall window.

Use one active workload stream (one outstanding mutation/barrier chain), one
same-owner reader stream, and at most one bulk operation cluster-wide. Issue at
most four measured mutation chains per second, with no concurrency increase to
meet a throughput target. Pacing is part of elapsed throughput time. A chain is
one write followed by the verified file-sync barrier; its latency includes both.
Native metadata latency covers the named syscall through its committed reply.
Guest metadata timing includes the verified file/directory barrier required by
that operation's deployment contract; an early guest reply alone is insufficient.
Reads include the freshness wait through return. No overlapping phase gets a
second resource allowance.

Every plan pins source revision and dirty-tree state, daemon/runner/library
artifacts and digests, wire/journal/checkpoint formats, build Go version/options,
FreeBSD kernel/userland and ZFS versions, FUSE adapter flags, bhyve/lib9p version,
and (where applicable) Rocky kernel/modules, 9p dialect/mount flags, SELinux and
CSI/container artifacts. Unknown required versions block execution. Different
artifact sets and cache settings are separate result series.

The proposed native cache profile follows [Durability](Durability.md): FUSE
writeback disabled, entry/attribute/negative timeouts zero, buffered reads with
verified invalidation, and no keep-cache across recovery generations. Run cold
and warm series separately. Cold means a newly published mount and names not yet
read by the client, **not** a claim that ZFS ARC or device caches are cold. Warm
means one bounded read/lookup pass over the same fixture before measurement;
its bytes and time remain charged. Do not flush global caches on shared hosts.
Guest tests propose 9p `cache=mmap` only after the actual sync/invalidation gate
passes. A restricted direct-I/O/no-mmap result cannot pass the required mmap
profile. Record actual negotiated flags, not just requested settings.

Normal daemon completion uses local durable journal plus the synchronous
partner's retained complete prefix in memory, with commit/visibility conditions;
fsync adds no second durable copy. No local-only downgrade or `sync=disabled`.
Guest buffered write latency alone is not that acknowledgement boundary: S/I
measure write **plus the verified guest-visible sync barrier**, recording the
trace to the daemon and journal. Ordinary guest writes, mmap MS_SYNC, fdatasync,
directory fsync and synchronous opens need their own semantic evidence; passing
the latency chain does not establish all of them. Rejected capabilities remain
explicitly unsupported and block acceptance where required.

## Latency and useful throughput proposal

Use these rows independently for cold and warm series. H means three ready
replicas; D means an established two-replica synchronous configuration with the
third unavailable before the timed phase. D is N-D or I-D only, not S-H. There
is no deliberately slowed synchronous peer in this initial profile. Other
latency/overload scenarios need a separate bounded numeric profile.

| ID: measured operation and sample | N H p95 / p99 | N D p95 / p99 | S/I H p95 / p99 | I D p95 / p99 | Minimum useful throughput H / D |
|---|---:|---:|---:|---:|---|
| W4: 64 overwrite+fsync chains, 4 KiB each, fixed offsets in preseeded files | 100 / 250 ms | 200 / 500 ms | 150 / 500 ms | 300 / 750 ms | 2 / 1 completed chains/s (8 / 4 KiB/s payload) |
| M: 64 metadata mutations, 16 each create/rename replacement/chmod/unlink, bounded planned dependencies | 100 / 250 ms | 200 / 500 ms | 150 / 500 ms | 300 / 750 ms | 2 / 1 completed mutations/s |
| R4: 128 same-owner reads, 4 KiB each, split evenly fresh-open and already-open with synchronized updates | 20 / 50 ms | 40 / 100 ms | 50 / 100 ms | 100 / 250 ms | 4 / 2 completed reads/s (16 / 8 KiB/s payload) |
| W64: 16 ordered 64 KiB overwrite+fsync chains, at most 1 MiB payload total | 250 / 500 ms | 500 / 1000 ms | 500 / 1000 ms | 750 / 1500 ms | 0.125 / 0.0625 MiB/s payload (2 / 1 chains/s) |

Metadata allocation must provide the exact 64-operation ledger and seed objects
without exceeding live object/mutation bounds; table counts are measured calls,
not permission for arbitrary extra setup. Reuse files in the transfer row; never
grow a benchmark-sized sequential file beyond 64 KiB. The read row permits at
most eight issued reads/s, with its updater's mutations counted in the ledger.
Throughput is successful useful count or payload divided by the interval from
first invocation to final completion, including waits, pacing and retries.
Retries consume limits and never add duplicate useful throughput. The minimum
must pass together with both latency thresholds, semantic checks and resources.

Report p50/p95/p99 and sample count for each operation/path/state, using
nearest-rank `ceil(p × n)` over per-invocation monotonic durations. These small
samples are bounded smoke criteria, not statistically reliable population tail
estimates: p99 of 64 or 16 samples is the maximum. Preserve all raw durations;
never average percentiles across volumes or runs. Do not omit errors/timeouts
from the verdict: any unexpected error or timed-out required operation fails the
row, even if remaining samples meet the thresholds. Give native calls a 2 s
deadline and guest chains a 3 s deadline, including barriers. These guardrails
are longer than acceptance thresholds and establish no performance promise.

## Lag, reconnect and recovery proposal

Lag is measured against the owner's committed position in the **same history,
epoch and configuration**. Record async received, durable and applied contiguous
positions separately; an arbitrary sequence subtraction across epochs is invalid.
Lag bytes count missing encoded journal bytes; operation count counts missing
mutations; age is monotonic elapsed time since owner commitment of the oldest
missing operation. Use acknowledged source positions and external timestamps,
not unsynchronized peer wall clocks. Sample at least once/s with event traces
for intervening transitions. Never serve workload reads from passive replicas.

| ID: scenario | Proposed threshold | Required evidence |
|---|---|---|
| L-H: healthy N-H/S-H/I-H async replica during the selected load | Each of received/durable/applied lag ≤2 MiB, ≤32 mutations and oldest age ≤2 s; all three limits apply | Position events, encoded byte ledger and oldest-commit timestamp; every gap closes within 5 s after last committed mutation |
| L-D: third replica unavailable (N-D/I-D) | Owner retains every obligation; no finite lag-age target for the offline replica; encoded backlog ≤2 MiB and ≤128 mutations | Exact cut position after seeding; pause only this test replica/link under its approved plan; stop generation at either cap. This admits a 64-mutation degraded latency series without dropping obligations |
| C-R: replay-only reconnect, source ready, target recovery-only, matching retained history, ≤2 MiB encoded / ≤16 mutations | First validated progress ≤5 s after link and target ready; complete durable+applied catch-up ≤45 s; ≥0.05 MiB/s useful encoded progress in each complete 10 s interval with ≥0.5 MiB remaining at interval start | Start/readiness/progress/end events; no gaps or duplicate effects; all positions match at a common checkpoint. Demand cap is separate from this rate floor |
| C-B: one checkpoint/rebuild, source ready, recovery set including suffix ≤384 MiB, retained old copy still charged | First validated chunk ≤10 s; transfer/install ≤540 s; validation+suffix/application ≤60 s; total ≤600 s from ready-to-transfer | Exact manifest/charged bytes, chunk verification and resumable progress; 2 MiB/s bulk cap and 4 MiB/s physical I/O cap remain aggregate |

The C-R replay and N-R/I-R small-suffix recovery cases have their own stricter
16-mutation backlog bound; they are separate runs from the degraded latency
series, which may retain up to 128 mutations under L-D. A larger L-D backlog
cannot claim the small-suffix recovery deadlines without a separately specified
profile. No earlier obligations may be discarded to meet the smaller bound.

The replay backlog is produced within the fixture's 256 mutations, for example
at most 16 rewrites of existing 64 KiB files while the async replica is absent.
Encoded journal overhead must fit 2 MiB as well; fewer operations may be needed.
No extra backlog file bypasses the payload/file bounds. The rebuild size is an
upper bound on a complete recovery set, not a request to fill 384 MiB with client
data. Record actual content, checkpoint format and transferred bytes. A recovery
set exceeding the cap is rejected before staging. The transfer deadline is a
proposal to calibrate with real overhead; the 2 MiB/s cap alone proves no minimum
rate. Reconnect/rebuild use a quiesced workload in this initial proposal. Live
traffic during bulk transfer needs a later explicit mixed-load profile.

For N-R/I-R single-owner loss, require surviving authoritative history, working
coordination and independent fence path, and a ≤2 MiB encoded/≤16-mutation suffix.
Use this sequential stage budget; record both stage and cumulative monotonic
times from externally observed fault injection. Waiting for a missing prerequisite
does not disappear from the total; such a run cannot pass this automatic-recovery
row. Safety always takes precedence over meeting the deadline.

| Stage | Start → completion evidence | Maximum |
|---|---|---:|
| Detection/admission gate | Fault injection → affected I/O gated and recovery reason emitted | 15 s |
| Authority and old-path fence | Gate → positive proof old owner/guest cached access is excluded and authority reconciled | 30 s |
| Authoritative-prefix recovery | Fence proof → surviving tail/commit evidence reconciled and required recovered prefix durably preserved | 45 s |
| Local replay/application | Recovered prefix → data, metadata and freshness barrier ready | 60 s |
| Synchronous pair readiness | Applied state → second ready replica and valid configuration established | 30 s |
| Publication and client recovery | Pair ready → verified mount/volume/epoch and first correct synchronized write+read | 30 s |
| **Total for this suffix and failure mode** | **Fault injection → successful client recovery** | **210 s** |

If a full checkpoint install is necessary, select that separate scenario in
advance: replace the 60 s replay stage by the 600 s checkpoint stage, yielding
750 s maximum total, not a silent extension of the replay row. For isolated
cold start, with all required storage/keys and management available, propose
≤120 s from explicit start to local storage, identity and coordination readiness,
then the 210 s small-suffix recovery sequence: ≤330 s overall. Record shutdown
quiesce/persist/stop-guest-access completion separately, proposed ≤60 s from
the approved isolated shutdown request. Missing fencing, required history or
keys blocks serving; a timer is never permission to activate. Manual emergency,
ambiguous-history, intentional rollback and accepted-loss decisions have no
automatic recovery deadline here. Their safe blocked state is required evidence.

## Resource and overall-window assertions

The entire setup, fixture, checkpoint/rebuild, collection and cleanup/restoration
window is ≤20 minutes, including at most five minutes of fixture mutations.
Select scenarios that fit; a 750 s recovery row and a 330 s cold-start row are
separate runs. A deadline stops dependent work and enters approved safe cleanup;
it never forces unmount, kills Rocky or discards retained history. Record incomplete
cleanup/resources even if preservation must continue beyond the window.

Apply [Capacity-plan](../Capacity-plan.md) admission equations and all of its
ceilings; this proposal supplies no new capacity admission. In particular:

| Resource | Acceptance ceiling / observation required |
|---|---|
| Per replica storage | 4 GiB run-parent quota, ≤3 GiB planned used; stop new mutations at 2.5 GiB parent use or 75% of any base allowance. Base data/journal/control/test-etcd ceilings 64/128/64/128 MiB; checkpoint and rebuild each ≤384 MiB, at most one each; two retained recovery sets; failed resources retain their charge |
| Native guest RAM | Within existing 2 GiB: daemon ≤384 MiB, test etcd ≤256 MiB, clients/helpers ≤128 MiB, ARC ≤256 MiB; preserve the full OS/kernel/allowance/512 MiB spare split from Capacity-plan |
| S host additions / clients | ≤1 GiB per host (512 MiB daemon, 256 MiB test etcd, 256 MiB collection/kernel); client ≤256 MiB and 0.25 core inside its existing guest; physical admission still fails under the current conservative envelope |
| Queued/in-flight payload | ≤64 MiB total per daemon, all peers/volumes, including retained memory ACKs; credits reserved before admission; observed RSS is additional evidence, not a substitute for byte accounting |
| Physical CPU | N ≤1.5 cores aggregate and ≤0.5 per guest; S ≤1 core per host including guest client; R ≤1 core. Preserve ≥1 core spare after existing measured peak |
| Physical disk I/O | ≤4 MiB/s and ≤100 IOPS per physical host, including all three native guests, journal sync/replay/checkpoint/rebuild and run-attributable outer traffic; preserve measured residual capacity and baseline device latency |
| Per-host network | Live ≤1 MiB/s; bulk ≤2 MiB/s; control ≤0.5 MiB/s; evidence ≤0.5 MiB/s; total ≤4 MiB/s in each direction including framing/retries; apply to actual bottleneck and outer receives too |
| External/outer charges | 1 GiB external evidence reserve; N adds the capacity plan's 32 GiB incremental history allowance at each outer source/receiver pool; no sink changes or snapshot pruning |

Define CPU and rate ceilings over each complete 10 s monotonic interval and
report shorter bursts and maxima too; enforce separate finite byte/operation
credits so averaging cannot permit unbounded bursts. Measure RSS/queue occupancy
at least once/s plus allocation high-water counters; storage checks before each
phase and continuously charged allocation counters must prevent overshoot between
samples. Require no swap use or memory pressure. Cumulative physical I/O is at
most 4800 MiB per host over 1200 s; this is transferred I/O, not retained bytes.
Latency targets must pass while these caps hold. If enforcement or attribution
cannot bound the canary, report not admitted; do not displace production traffic,
shrink existing guests/ARC or infer resource fit from the small data payload.

## Durability verdicts remain separate

For every selected failure, keep an external ordered invocation/reply oracle and
compare recovered effects, metadata and history against permitted outcomes.
Preserve acknowledged effects where their required copies survive. A timeout or
lost reply is uncertain, not definite rollback; replica equality alone is not an
oracle. Owner crash with partner memory surviving must persist/recover that tail
before activation. Reboot tests must account for the outer host's buffering and
cannot be called physical power-loss proof.

Owner storage loss plus loss of the partner's not-yet-persisted volatile prefix
is the accepted gap. **There is no finite RPO or maximum loss window**, even if
healthy measured lag is small or this tiny run loses no bytes. Report the exact
oracle-known missing/uncertain operations and bytes for that run, the surviving
copies and the safe blocked/manual-decision outcome. Neither the 2 s lag target,
five-minute phase nor recovery deadline bounds this failure mode's possible loss.
Emergency local-only mode is not selected here and receives no inferred guarantee.

## Calibration plan and acceptance evidence

This is a non-executable plan template. No actual targets, artifact digests,
admission record or approval are supplied. To request calibration, fill every
required field below and review the immutable plan; missing values block execution.
Do not fabricate artifact hashes or treat an approval field as a generated token.

- Run ID, selected profile/row and source/spec revision; deterministic seed,
  operation ledger, synthetic expected lengths/hashes and exact backlog construction.
- Actual isolated or live target identities, volume/view mappings, owner/partner
  roles and epochs, caller UID/GIDs, unique paths, exact allowed disruptions and
  narrow fence credentials/positive completion checks where applicable.
- Immutable artifact/version/cache manifest above, capability/guest-sync evidence,
  prerequisite Stage 1/deployment evidence and required physical fixture inventory.
- Fresh resource inspection and pool `A/F/G/Q`, physical RAM/guest allocation,
  CPU/I/O/network arithmetic; quota/credit enforcement and sampler versions;
  all prior failed resources charged; setup/run/restoration and later receive growth.
- Selected numeric threshold revision and warm/cold series; per-operation and
  stage deadlines, retries, byte/operation/time caps, failpoints and fault schedule.
- Independent oracle, raw durations/position events/resource counters, evidence
  destination, bounded logs and external reachability during the injected fault.
- Safe cleanup/restoration state machine, normal unmount proof, retained-resource
  inventory, and Paul's explicit approval tied to this exact plan and window.

Calibration proposes at least three independent bounded runs per selected
row/state/cache/path to expose variability, each separately approved. Report
each run; do not pool results to hide a failure. Their purpose is to assess
feasibility and revise the proposal for review, not produce an approved baseline
automatically. After numeric approval and implementation of assertions/alerts,
acceptance requires matching approved-plan evidence with all selected criteria
passing. Three small calibration samples do not establish production tail behavior.

Each result must identify criterion ID, units, workload/cache/durability/backlog,
version manifest, threshold revision, exact approval/plan reference, actual sample
count/times and raw evidence location. Report **passed**, **failed**,
**unsupported/rejected as specified**, **not admitted** or **not run** per case,
with infrastructure, assertion, timeout, cleanup and restoration failures
distinguished. Required unsupported capabilities block the relevant positive
profile; correct negative rejection is a separate result. Include before/after
resource and retained-copy inventories, including received history charges.

The runner assertions and canary alerts must consume the same approved numeric
profile; alert wiring must distinguish healthy lag from intentionally offline
replicas and recovery from serving readiness. Although Paul approved the numeric
calibration criteria, enforcement, placement admission and matching runs remain
required. These tables provide **no acceptance evidence**.
