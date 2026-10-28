# Bounded canary capacity plan

[Deployment readiness](Deployment-readiness.md#capacity-and-resource-admission) ·
[Synthetic patterns](Canary-patterns.md) · [Journal lifecycle](Journal-storage.md) ·
[Performance](Performance.md) · [Lab replication](Lab-replication.md)

## Status and scope

This is a proposed small synthetic profile, prepared on 2026-10-28. It neither
installs limits nor establishes current spare capacity. HomelabFS, its full
runner and resource enforcement remain implementation work. **No profile is
admitted by this document.** Installation and each run need their own explicit
approval under [Test automation](Test-automation.md). Disruptive tests require
approved isolated targets.

Use fresh run-owned identities, datasets and paths. Preserve production PVs,
consumers, NFS, Garage, backups, received sinks and existing power automation.
There is no production copy, migration or hardware-upgrade requirement. If a
profile fails admission, reduce its bounds or select approved isolated placement.
Do not reclaim production or failed-run resources to make it fit.

## Evidence and separate placements

| Evidence | Recorded allocation or observation | What it establishes |
|---|---|---|
| [Lab operations, 2026-10-25](Lab-operations.md#inventory-and-access) | Three native FreeBSD guests, each 2 GiB RAM, two vCPUs, 20 GiB OS disk and 16 GiB data disk; guest ARC capped at 256 MiB | Existing prepared native geometry, not current VM state or available space |
| [Lab preparation](FreeBSD-lab-preparation.md) and [replication](Lab-replication.md) | f3 outer lab quota 120 GiB, 40 GiB per guest; f2 receives the subtree | Existing outer quota boundaries; received history needs separate accounting |
| [Readiness, 2026-10-28](Deployment-readiness.md#observed-baseline-2026-10-28) | f0/f1/f2 zdata available: 1.42 TiB / 1.43 TiB / 670 GiB | Dated rounded availability; no admission or RAM/CPU/network measurement |
| Same readiness observation | f0 NFS referenced 1.97 TiB, used 2.30 TiB | Context only; unrelated to this fixture size |

Keep these placements distinct:

- **N: native integration on f3.** All FUSE, future daemon and dedicated test
  coordination processes run inside the three 2 GiB guests. No 9p in this matrix.
- **R: independent Rocky/9p probe on f3.** Host FUSE and host ZFS, one Rocky guest;
  all three native guests stopped. This is a transport profile, with no extra
  replicated filesystem or etcd implied.
- **S: future shared-host canary on f0/f1/f2.** One replica per host, a host daemon
  and dedicated test coordination member, and small clients inside the existing
  r0/r1/r2 guests. Publication depends on supported guest kernels and later
  placement decisions. It must not use production k3s etcd as test coordination.

N and R are mutually exclusive even if a memory calculation appears to fit.
Never start three native guests alongside Rocky. Inspect actual VM state and
allocations before an approved lifecycle window; pausing Rocky releases no RAM.
Do not start the unrelated development VM or a fourth native guest implicitly.

Local configuration is evidence of intended state, not live state. On 2026-10-28,
`~/git/conf/gonf/cluster/freebsd_bhyve.go` specified four vCPUs and 12G for each
r0–r2 Rocky guest; its older f3 configuration specified 14G. The repository's
lab planning uses a 12 GiB f3 Rocky profile. IQ's hardware and VM leaves also
contain historical 16 GB/N100/four-core/GbE and 14G examples, explicitly not
revalidated during their 2026-10-25 migration. Resolve this discrepancy by
inspection before execution; neither value authorizes resizing. The R arithmetic
below uses the requested 12 GiB profile and also shows the 14 GiB failure case.

## Selected synthetic workload

Use all five [canary patterns](Canary-patterns.md): **4 MiB live regular-file
payload total**, including temporary/state files, at most 64 regular files,
32 directories, eight symlinks, 64 KiB per file, 256 mutations and a five-minute
fixture phase. Aliases and read-only views share the canonical identity and do
not create extra payload or replica histories. The aggregate quota covers the
entire selected volume set, regardless of later volume/group mapping.

A full-file replacement for every mutation is at most
`256 × 64 KiB = 16 MiB` of application write payload. This is not a bound on
physical writes: framing, metadata, checkpoints, retries, etcd, ZFS allocation
and snapshots add costs. The following ceilings bound those separately. No real
application scans, databases, media libraries or benchmark-sized payloads belong
to this profile. The historical 128 MiB PoC probe is a different profile.

## Per-replica storage envelope

All figures below are **aggregate MiB per node for the entire fixture set**,
not a quota to multiply by every claim. Base quotas apply to actual allocated
space, with no assumed compression or snapshot sharing. Logical payload also
has its separate 4 MiB client limit.

| Charge | Ceiling, MiB | Bound and ownership |
|---|---:|---|
| Current data/namespace | 64 | 4 MiB payload plus allocation/namespace allowance; aggregate data `refquota` ceiling |
| Current journal | 128 | All streams, sealed/open segments and retained replay obligations; aggregate journal `refquota` ceiling |
| Current control/checkpoint manifests | 64 | Identities, epochs, recovery and admission evidence; never exported to clients |
| Dedicated test etcd persistent state | 128 | Database, WAL and local coordination artifacts, separate from production etcd |
| Two retained recovery sets | 768 | Each charged as a complete 384 MiB base; no sharing assumed |
| New checkpoint staging | 384 | One complete base while old data/history still exists |
| One replica rebuild/restore staging | 384 | One additional complete base plus its included suffix; old replica remains charged |
| Evidence | 256 | Bounded logs, manifests, hashes and diagnostics, outside exported volumes |
| Failed-resource/cleanup reserve | 256 | Cleanup records and retained partial resources; no second run while resources remain charged |
| Allocation/metadata allowance | 640 | ZFS accounting, staging overhead and rounding; measured use still governs |
| **Maximum planned used** | **3072 (3 GiB)** | Includes one checkpoint and one rebuild simultaneously |
| **Unused emergency headroom** | **1024 (1 GiB)** | Not available for workload admission |
| **Run parent quota / reservation in admission ledger** | **4096 (4 GiB)** | Includes descendants, snapshots and staged copies |

Arithmetic: current base `64 + 128 + 64 + 128 = 384 MiB`;
`384 + 768 + 384 + 384 + 256 + 256 + 640 = 3072 MiB`.
Normal service without either staging copy is at most 2304 MiB under the same
conservative allowances. Worst case adds 768 MiB to reach 3072 MiB. Three nodes
reserve `3 × 4 = 12 GiB`, with at most `3 × 3 = 9 GiB` planned used. A rebuild
on an existing node uses its listed staging allowance, not an unbudgeted fourth
copy. A distinct replacement host needs its own 4 GiB admission and RAM/CPU
budget; this does not authorize booting a fourth lab VM.

The implementation/installation plan must translate these aggregates into
run-parent and child quotas. Data/journal/control/etcd base `refquota` limits do
not bound snapshot bytes. For two fully distinct retained versions, their
base-plus-snapshot allowances are respectively 192/384/192/384 MiB, totalling
1152 MiB. Include both staging trees under the same run parent. An alias cannot
receive another independent allowance. Account evidence on its actual storage
host if it is collected elsewhere; reserve an additional **1 GiB** at the
external evidence destination for the complete run and retained failure report.
These are admission reservations, not a request to set ZFS `reservation` on
shared pools automatically.

Retain at most two completed compatible recovery sets and one checkpoint and
one rebuild in progress. [Snapshot/restore policy](Snapshots-and-restore.md)
and later tooling must define the paired data/journal/control/coordination
manifest and any required holds; independent etcd state is not made consistent
by a recursive data snapshot alone. Use synthetic test keys. A restore uses a
separate writable run-owned destination, never a received sink or a live rollback.

At **2.5 GiB** parent used, or 75% of any base allowance, stop admitting new
workload mutations and assess remaining obligations. The hard planned-use limit
is **3 GiB**, leaving 1 GiB for orderly gating, durable metadata, evidence and
cleanup. Reserve credits before acceptance, including operation framing and
metadata; exceeding a forecast must gate writes before accepting them. If
checkpoint/rebuild output will exceed 384 MiB, reject that operation before it
starts. Do not delete required journal prefixes or pinned snapshots to regain
space. Backpressure and an explicitly incomplete run are correct outcomes.
Snapshot count does not override the byte ceiling; failed resources keep their
charge until inspected and deliberately cleaned up.

## Host and pool arithmetic

For each physical pool, record immediately before approval:

- `A`: usable dataset availability after ancestor quotas/reservations, measured
  on the actual destination pool; do not add `available` from sibling datasets.
- `F`: protected pool-free floor, proposed here as **20% of measured pool size**
  or the existing operational floor if larger. This is a planning gate, not a
  claim that 20% is an established production policy.
- `G`: protected growth during the whole run, evidence collection and cleanup
  window: current services, Garage, backups, replication/receives and other VMs.
  Use a measured conservative bound for the approved window; unknown `G` fails
  admission. Never assume zero because the fixture is small.
- `Q`: new run reservations on that pool, including evidence and retained prior
  failures not already reflected in `A`. Avoid charging existing allocations
  twice, but preserve their future growth in `G`.

Require `A >= F + G + Q` and sufficient space below every ancestor and child
quota. Apply the same rule at the workload/evidence destinations, not just the
largest pool. A quota limits consumption; it does not create free capacity.

| Host / placement | New bounded demand | Required check |
|---|---:|---|
| f0, S | 4 GiB | `A_f0 >= F_f0 + G_f0 + 4 GiB` |
| f1, S | 4 GiB | `A_f1 >= F_f1 + G_f1 + 4 GiB` |
| f2, S, with external evidence here | 5 GiB | `A_f2 >= F_f2 + G_f2 + 5 GiB`; `G_f2` includes f3 receive/history growth |
| f3, N, inner pools | 4 GiB per 16 GiB guest pool | Measured available minus inner floor/growth must fit 4 GiB; disk size alone is insufficient |
| f3, N, outer lab | Existing 40 GiB child / 120 GiB parent quotas | Each existing child used plus forecast growth must stay below 40 GiB; sum below 120 GiB, with host-pool floor intact |
| f3, R | 4 GiB on the actual host backing pool, plus guest logs | Same pool formula; no three-replica charge for this transport-only profile |

For context only, f2's rounded dated 670 GiB availability minus the proposed
5 GiB S/evidence reservation leaves 665 GiB **before** protected growth and the
pool floor. With a rounded 186 GiB floor (20% of the dated 928 GiB pool), that
would leave about **479 GiB minus G**. This demonstrates the size of the proposed
demand, not an admission result. f0/f1's dated 1.42/1.43 TiB similarly cannot
establish current headroom. There is no current f3 pool-availability measurement
in this plan.

For N, each guest's nominal disk sum is `20 + 16 = 36 GiB`, leaving only
`40 - 36 = 4 GiB` under its outer quota if both images become fully allocated.
Across three guests, `3 × 36 = 108 GiB`, leaving `120 - 108 = 12 GiB` for outer
configuration/history at that extreme. Inner free space and the 4 GiB run
quota do not prove that outer snapshots fit. Measure allocated image blocks,
`usedbysnapshots`, OS growth and parent availability independently. Do not count
unused sparse disk capacity as extra space outside the 40/120 GiB quotas.

[Existing f3 → f2 zrepl](Lab-replication.md) takes snapshots every ten minutes,
retaining last ten plus `24x1h | 14x1d`. It owns these snapshots; this plan adds
no competing owner and changes no retention. Its outer VM-image history is
**additional to** the two inner recovery sets above. A source quota of 120 GiB
does not cap receiver history at 120 GiB. Do not claim snapshot counts bound
bytes or that all of f2's 670 GiB is available to the lab.

Before an N run, reserve a proposed **32 GiB incremental run/history allowance
on both f3's actual outer pool and f2's receiver pool**, plus the 1 GiB external
evidence allowance on its chosen host. This is a byte ceiling to verify and
monitor, not measured write amplification or an alteration to the sink quota.
Existing lab bytes are already charged in measured availability; protect all
unrelated VM/zrepl growth separately in `G`. Require an approved run bound on
cumulative churn, OS writes and subsequent receives that fits this allowance;
inner occupancy alone cannot supply that bound. If attribution or receiver
history growth cannot be bounded, N is not admitted. Do not prune snapshots,
modify or boot the received sink to recover the allowance. Restore on f2 would
need a further separately admitted writable copy and host RAM budget; the
recovery copy itself grants no capacity to boot it.

## RAM, ARC and existing guests

All numbers below are proposed admission envelopes, not observations of RSS or
permission to lower production ARC/guest allocations. Use measured physical RAM
in bytes, not the marketing label “16 GB”. Include kernel/wired memory, bhyve
process overhead, ARC, services and a no-swap safety margin; free pages alone are
not available RAM. Existing guests remain charged at their full configured
allocation even when idle. Reclaimable ARC must have a verified safe budget,
not be counted both as free and as allocated.

For each **2 GiB native guest**, the proposed internal split is:

| Guest charge | MiB |
|---|---:|
| Guest OS/kernel/background processes | 384 |
| Guest ARC (existing cap) | 256 |
| HomelabFS daemon, all buffers and runtime | 384 |
| Dedicated test etcd member | 256 |
| Synthetic clients and evidence helpers | 128 |
| Additional kernel/I/O allocation allowance | 128 |
| Unallocated safety margin | 512 |
| **Total inside existing allocation** | **2048** |

The daemon's 384 MiB includes a proposed **64 MiB total queued/in-flight payload
cap** across all peers and volumes; memory-ACK buffers remain charged until safe
to release. The remaining 320 MiB covers runtime, indexes, connection and replay
state. A Go runtime target is not a hard process cap. Measure total resident and
kernel use, enforce byte credits, and gate admission before exhaustion. etcd's
256 MiB is a test allowance to validate, not a demonstrated minimum.

For N, `3 × 2048 = 6144 MiB` guest allocation. On f3, propose additionally
2048 MiB existing host kernel/services, 2048 MiB host ARC, 512 MiB bhyve overhead
and 2048 MiB spare: **12800 MiB (12.5 GiB) total**. Guest daemon/etcd/client RAM
is already inside the 6 GiB, so do not add it again. Compare with actual physical
RAM and measured host high-water use. The existing lifecycle helper's 6 GiB
plus 4 GiB minimum check is necessary but does not implement this complete gate.
No swap use, memory pressure or unexplained extra VM is acceptable for admission.

For R, budget 12 GiB Rocky plus the same 6.5 GiB host protection and **512 MiB
host FUSE/helpers**: **19 GiB required**, above a nominal 16 GiB host. At the
older configured 14 GiB Rocky size this becomes **21 GiB**. Thus **R is not
admitted under this conservative profile**. A historical successful PoC does not
prove these bounds fit. A separately approved smaller guest/profile with
measured host protection, or approved isolated placement, is needed; do not
silently shrink Rocky or host ARC. The three native guests remain stopped for R.

For S, propose **1 GiB additional host RAM per replica**: 512 MiB daemon,
256 MiB dedicated test etcd and 256 MiB collectors/kernel buffers. Preserve the
same **6.5 GiB host kernel/services/ARC/bhyve/spare envelope** used above. With
each recorded r-node at 12 GiB, `12 + 6.5 + 1 = 19.5 GiB` per host: **S is also
not admitted on a nominal 16 GiB host under these proposed ceilings**. This
explicit failure prevents assuming that tiny synthetic payload implies memory
fit. More precise approved measurements may justify a smaller profile; this
document does not require a hardware upgrade or a production inventory.

The general host gate is
`physical RAM >= all configured running guest RAM + protected host envelope + new host processes`.
Record the measured/proposed components explicitly and preserve at least the
2 GiB spare within the host envelope. Existing service peaks above their
allowance increase the required envelope. The sample conservative budgets fail
rather than assuming that ARC, backups or services can be squeezed automatically.

For S clients, additionally require **256 MiB and 0.25 CPU core of available
capacity inside each participating existing guest**. This does not add another
256 MiB to the physical allocation; it is a separate guest admission test.
Do not evict or alter existing workloads. Local chart settings inspected on
2026-10-28 illustrate why requests are not measurements: Player requests 128 MiB
and limits 1 GiB, Navidrome 128 MiB/1 GiB, Syncthing 384 MiB/1536 MiB, and Immich
Postgres 512 MiB/2 GiB. Their request sum is 1152 MiB versus a 5632 MiB limit sum;
this partial example is neither placement nor a complete capacity inventory.
Sources are the corresponding `~/git/conf/f3s/*/helm-chart/templates/` deployment
files and `immich/helm-chart/templates/postgres.yaml`. Existing k3s/etcd and other
services remain part of the guest baseline; do not add chart sums to VM RAM a
second time or infer spare capacity from PVC sizes.

## CPU, storage I/O and network ceilings

These are bounded demand/admission limits for later runner enforcement. They
are not calibrated latency, throughput or recovery targets; those belong to
`jv3` and [performance acceptance](Performance.md#canary-acceptance-profiles).

| Resource | Proposed ceiling | Admission and stopping rule |
|---|---|---|
| Native N physical CPU | 1.5 cores aggregate across all three guests, including daemon/etcd/clients | Each guest averages at most 0.5 core; two configured vCPUs each do not reserve six physical cores |
| S physical CPU, per host | 1 core aggregate: 0.5 daemon, 0.125 test etcd, 0.125 collection, 0.25 guest client | Charge guest client CPU on the same host; no double allocation |
| R physical CPU | 1 core aggregate including host FUSE and incremental guest probe CPU | Independent transport profile only |
| Physical CPU spare | 1 core per host | Require `measured existing peak + canary ceiling + 1 <= actual cores`; on four cores, N allows at most 1.5 existing cores, S/R at most 2 |
| Disk I/O, per physical host | 4 MiB/s aggregate canary traffic and 100 IOPS, including syncs, replay, snapshots and rebuild | Measure device latency and baseline load; if no safe residual can be demonstrated, no admission; preserve production sync semantics |
| Peer/live traffic | 1 MiB/s aggregate per host | Both synchronous and asynchronous copies, retransmission and framing included; not 1 MiB/s per peer |
| Bulk checkpoint/rebuild/catch-up | 2 MiB/s per host | At most one bulk operation cluster-wide; share this budget across peers |
| Control and evidence transfer | 0.5 MiB/s each per host | Includes test coordination, SSH collection and retries |
| Total test network | 4 MiB/s in each direction per physical host | Reserve 25% measured usable link/tunnel capacity: `existing peak + 4 <= 0.75 × measured usable MiB/s`, separately for transmit/receive |

Network demand is `1 + 2 + 0.5 + 0.5 = 4 MiB/s`, approximately 33.6 Mbit/s in
each direction, including overhead in the measured total. Apply the gate to
the shared NIC, WireGuard path and bottleneck link, not the nominal GbE label.
Native guest loopback/bridge transfers still consume host CPU and physical disk
resources even without a wire hop. Existing zrepl and backup traffic are part
of the baseline, never displaced to meet this test budget. The existing zrepl
job may have no test-specific rate control: if its run-attributable transfer
cannot fit the measured residual, schedule a separately approved bounded window
or reject admission; do not change production replication configuration here.

Cap the complete setup/fixture/checkpoint/rebuild/collection window at **20
minutes**, with only five minutes for fixture mutations. At 4 MiB/s the physical
host I/O ceiling allows at most `4 × 1200 = 4800 MiB` (4.6875 GiB) transferred
through that I/O budget during the window. This is not a retained-space estimate:
OS/background writes and later replication/history charges still require the
separate growth budget. No implied catch-up completion time follows from a
rate cap. Finite operation deadlines, cumulative-byte accounting, bounded
retries and resource samplers must exist before running this profile. If a limit
cannot be enforced or measured, report the case not admitted rather than claim
that a time limit alone bounded its use. Incomplete rebuild/checkpoint resources
remain charged and useful evidence is preserved.

## Admission record and cleanup

A later approved run plan must attach a dated, read-only resource inspection:
actual host/guest identities and VM states, pool/ancestor availability and quota
properties, snapshots/clones/holds, physical and guest RAM/ARC high-water marks,
all running guest allocations, CPU/device latency and transmit/receive baselines.
Record the growth window and `A/F/G/Q` arithmetic for every affected pool and the
RAM/CPU/network equations for each host. Existing dated evidence is a starting
point; no fresh infrastructure inspection was performed to prepare this plan.
No full production-data scan is needed to supply these focused measurements.

Record predicted and observed peaks for live data, journal, control/etcd,
snapshots, staging, memory-ACK queues, physical writes, network bytes and retained
outer replication history. If limits or availability changed, stop and replan;
approval for an older plan does not authorize broader targets or higher limits.

On completion, collect evidence outside the tested volume, normally unmount,
and remove only approved run-owned resources after checking mounts and holds.
Keep failed resources and their byte charges in the inventory. Deletion of files
may free no pool space while snapshots/clones retain blocks; do not report the
reservation released until actual charged usage is inspected, including the
later receive/retention lifecycle. Never force-kill Rocky, recursively delete
through a live mount, change snapshot ownership, or clean production data to
make a run pass. The evidence report distinguishes admitted/executed, blocked
by capacity, failed, and not run, independently for N, R and S.
