# Isolated retained-9p fixture plan

Task `wz3`, 2026-11-02. **Plan only; no fixture is admitted by this document.**
This identifies a bounded base fixture and later distributed fixtures, their
preparation gates and the evidence needed before execution. The orchestrator's
authorized f3 wake and bounded read-only inventories are recorded below; Rocky
was observed running under the existing host autostart policy. No direct VM
lifecycle command, installation, provisioning, mount, workload or fault
injection was performed for this plan. Capacity and capabilities must be observed again
before execution; unavailable inventory cannot be filled from historical logs.

The selected [authorization decision](Retained-9p-authorization.md) and
[compatibility design](Retained-9p-compatibility.md), adopted in `b4aa7bf`,
control this profile: FreeBSD HomelabFS FUSE → bhyve/lib9p → Rocky kernel 9p.
Guest kernels/root attest complete per-request identities within admitted
exports; host Unix authorization and volume confinement still apply. Every
admitted origin remains eligible to write the same volume. Older single-writer
wording in preparation/kernel-policy documents is superseded by this decision
and the [all-origin contract](../spec/Replication-protocol.md). It must not be
reintroduced as fixture admission. Advisory lock guarantees remain confined to
one guest 9p mount/kernel inode domain.

## Observed availability and admission status

Read [lab operations](../Lab-operations.md),
[test automation](../Test-automation.md) and
[replication safety](../Lab-replication.md), then IQ's progressive
`AGENTS.md → topics/infra.md → infra/f3s.md → hosts/vms/rocky-vm`
navigation before remote inventory. IQ's dated machine tables are routing and
planning information, not live state.

One bounded, pinned, read-only SSH inventory attempt at
`2026-11-02T20:37:47Z–20:37:50Z` failed **before remote dispatch**:

```text
destination: paul@f3, HostName=192.168.1.133, HostKeyAlias=f3
exit: 255
stdout: empty
stderr: ssh: connect to host 192.168.1.133 port 22: No route to host
```

This did not establish that f3 was powered off. The orchestrator subsequently
sent one explicitly authorized f3-only Wake-on-LAN packet; no VM stop/start or
configuration operation was issued. A completed pinned host retry at remote
`20:40:46Z`, followed by completed host/Rocky capability inventory at
`20:41:32Z–20:41:34Z`, established the following actual state. Exact argv,
scripts, separate outputs and return codes are retained unchanged in the
[inventory evidence](results/20261102-wz3-inventory/): `inventory.json`,
`inventory-retry-1.json`, `inventory-capabilities-host.json` and
`inventory-capabilities-rocky.json`; the orchestrator's `f3-power-on.json`
records its power operation. The [digest manifest](results/20261102-wz3-inventory/sha256.json)
binds those exact receipt bytes, also retained on the controller in
`/home/paul/tmp/codex/hfs-wz3-planning-20261102/`.
Remote clock readings and controller timestamps
are separate evidence, not a precision latency measurement.

| Item | Actual observation for this plan | Admission consequence |
|---|---|---|
| f3 | FreeBSD kernel/userland `15.1-RELEASE-p2`, revision `aadd58dddcbc`, 4 CPUs, `hw.physmem=16879837184` bytes (15.721 GiB); host swap 0 | One post-boot sample, no sustained-pressure admission |
| VM allocation | Rocky Running PID 6444, 4 vCPUs/12 GiB; `freebsd` and hfs-test0–2 Stopped | Native trio excluded; Rocky's full allocation must be budgeted even while pages are not resident |
| Host processes | Additional bhyve PIDs 6537/6540 identify as `system.pwd`/`system.grp`; all three executable paths `/usr/sbin/bhyve` | Include exporter/helper processes in host reserve; do not count RSS as VM capacity |
| Host storage | `zroot` ONLINE, 377580130304 bytes pool free; `zroot/bhyve` available 361878601728 bytes; lab subtree available 121966059520 bytes, quota 120 GiB; child quotas 40 GiB | Existing pool capacity, no run reservation; do not place the independent 9p fixture into native guest disks |
| Retained history/sync | Rocky dataset `usedbysnapshots=12540030976` bytes in capability observation; lab parent 24576 bytes; `sync=standard`, reservation/refreservation 0 for observed bhyve parent/Rocky/lab/var-tmp datasets | Counts vary with snapshots; child/subtree retention and final run root still need exact-plan admission |
| Rocky guest | 9.8; running `5.14.0-687.61.1.el9_8.x86_64`; `CONFIG_NET_9P` unset; no 9p mount found | Current boot cannot run B0; installed ELRepo 6.1.190 core/modules/extra are not patched or booted evidence |
| Guest capacity/access | Guest swap 0; root/var-tmp filesystem available 23644803072 bytes; `/boot` available 329064448 bytes; SELinux Enforcing; podman/crun commands present | Candidate/rollback boot-image fit is unresolved; executable presence is not an admitted pod/runtime profile |
| Patched Linux9p/lib9p/FUSE-control/daemon artifact set | No candidate admitted | `rz3`/`sz3`/`tz3`/`uz3`/`vz3`/`xz3` gates remain |
| Independent disposable hosts for distributed fixtures | None identified/admitted here | Distributed runs cannot be scheduled yet |

The live VM configuration confirms 12 GiB, superseding the migrated IQ leaf's
older 14 GiB description for this dated observation. The native guests' FUSE runs
inside each FreeBSD guest on its own pool. They are neither Rocky cache domains
nor independent physical failure domains. Do not run the native trio alongside
the large Rocky VM. Existing f2 received copies are not runnable fixtures.

## B0: base, two guest domains on one origin

The topology reference uses f3 and its existing `rocky` VM, but **that 12 GiB
placement fails memory admission for the current 4 MiB logical component
profile**, as calculated below. It is not an executable candidate under these
limits. Native `hfs-test0`–`2` and unrelated `freebsd` remain stopped during a
9p window. On f3, one real HomelabFS daemon and one
native FUSE connection serve a new synthetic volume `V(RUN)`. One dedicated
patched exporter serves that exact volume through two negotiated guest mount
sessions `C0` and `C1` in Rocky. Each has its own registered mount/cache
incarnation, token/recall state, descriptors and mapped aliases. Both may write.
Two exports, if required by the implementation, still bind to that same service
volume and origin, never two independent copies of its backing tree.

Two mount commands alone are insufficient: runtime evidence must show distinct
9p mount/cache domains and separate recall registrations. Bind mounts, pods
sharing the same mount and a merged kernel superblock do not count as the second
domain. Prove each can hold aliases while the other mutates, and that a paused
recall in either domain blocks the conflicting publication. Inventory every
native host reader too; the daemon's native path is not exempt from barriers.

This uses one native connection, not two `AttachFrontend` calls on the current
single-frontend service. The current restricted CLI/provider is not a delegated
guest exporter profile; enrolling root/bhyve or substituting guest IDs into FUSE
headers is forbidden. Implement and admit the selected integration first.
Neither existing passthrough binaries nor logical/model tests substitute for it.

If the patched guest cannot create two independently registered domains on a
VM, that topology is also not ready. To accommodate the current logical profile,
the required separate preparation proposal is two new disposable Rocky guests
of 2 GiB and two vCPUs each on f3, with the large Rocky and all native guests
stopped. This requires a separate reviewed creation/installation/lifecycle plan,
new identities, source images, disk budgets and pins. Do not resize, clone over,
recreate or repurpose an existing VM to satisfy this fallback. Its 4 GiB guest
budget is a proposed ceiling, not evidence that the required guest image boots
and runs inside 2 GiB.

B0 validates local multiuser/namespace/sync/mmap/K04 behavior and two-domain
recall on one origin. It cannot establish distinct-host retained-memory
protection, remote-origin durability, owner failure or outside-replica history.
Its disposable profile must label this limit and keep normal publication
refused. It supplies no fake partner acknowledgement. Native `n18` results and
historical 9p passthrough checks retain their original, separate scope.

## D0/D1: later independent hosts, same volume

These are gated on core all-origin runtime, real platform barriers and the
`ov3`/`pv3` acceptance harness. They do not gate implementation of the B0 harness.

| Fixture | Placement and roles | Required distinction |
|---|---|---|
| D0 | Two independently admitted physical FreeBSD hosts `HA` and `HB`, each running its own real daemon, ZFS source/journal and patched bhyve/Linux9p guest; owner `A` on HA, configured partner `B` on HB; `S={A,B}` | Both origins write **one volume UUID and closure**; owner-origin completion uses A durable + B retained RAM, remote B-origin completion uses B durable + A retained RAM |
| D1 | D0 plus a third independent admitted host `HO`, with origin `O` outside replica placement `R={A,B}`; `S={A,B,O}` | O writes the same volume; O durable + A retained RAM cannot be replaced by A-local commit or a B disk maximum |

Use controller-held run-owned synthetic IDs, not preexisting replica contents.
On every host, a separate data/journal/control root belongs to that origin;
do not share a backing dataset, NFS export, VM disk or controller directory to
manufacture replication. A delayed owner backing/apply fixture must still read
the committed RAM view, including foreign-origin metadata, through every
admitted client. The external controller holds the invocation/reply/read oracle.
D1 retains original source fragments, uncertainty and claims across source loss;
fencing does not invent missing durable coverage.

No HA/HB/HO allocation beyond the B0 candidate f3 is available from this plan.
Production f0–f2, their r-nodes, production storage and f2's received sink are
excluded. A second or third native VM on f3 cannot satisfy physical-host
independence. Later admission must name real spare hosts, owners, management
paths, independent storage and per-host memory/disk/fence capabilities. Missing
hosts mean “not run”; do not weaken the topology or run a Linux frontend instead.

## Resource and execution envelope

The following are **proposed upper bounds**, not measurements or implemented
limit enforcement. A reviewed exact plan must pin enforceable limits in the
new harness; if it cannot enforce a bound or the workload does not fit, refuse
that plan and revise it before execution. Do not silently increase bounds.

| Resource | B0 ceiling | D0/D1 ceiling |
|---|---|---|
| Guest RAM | Existing Rocky's freshly observed allocation, no increase; fallback 2 × 2 GiB | 1 × 2 GiB new disposable Rocky per host, subject to actual boot/workload admission |
| Host logical-component and exporter credits | 512 MiB ordinary plus **separate 768 MiB recovery** per-volume component caps; 128 MiB exporter/recall budget; 1,408 MiB combined, not a complete runtime RSS bound | Same per host/volume; multiply component arenas for additional authority volumes; separately admit other runtime allocations |
| Host reserve | At least 3 GiB physical RAM outside all configured running VM allocations and component/exporter budgets; separately quantify other service runtime costs; reject swap use or sustained page pressure | Same per host; ARC and unrelated host processes must fit this reserve |
| vCPU/workload concurrency | Existing Rocky vCPU allocation unchanged; at most 4 active probe workers, 16 probe processes, 2 guest mount domains | At most 2 vCPUs/new guest, 4 workers/host, 16 processes/host; 6 guest domains aggregate in D1 |
| Persistent synthetic run data | 2 GiB source/journal/control aggregate per origin, plus 512 MiB guest scratch | Same per origin/guest; include all retained claims/results and delayed-apply/source fragments |
| Artifact staging | 4 GiB separate per host/guest for candidate plus complete rollback set | Same; a larger real artifact closure requires a revised budget |
| Future capacity-profile envelope | 4,096 live objects, 64 open descriptions, 16 mappings and 64 MiB mapped/dirty data aggregate; max 16 MiB/file; 256 MiB distinct payload, 1 GiB cumulative issued writes, 20,000 issued operations; **not admitted by the current 4 MiB profile or its budgets** | Same per origin; 3 GiB cumulative writes and 60,000 operations maximum for D1; separate profile sizing required |
| Time bound | 10-minute admission, 20-minute setup, 30-minute workload, 10-minute collection, 15-minute normal cleanup/restoration | Same per batch; host count does not multiply deadlines |
| Evidence | 256 KiB per diagnostic stream, 64 MiB aggregate/controller run directory; separate complete oracle bounded by the operation cap | Same; truncation of required oracle or admission evidence makes the run incomplete |

Reserve control/drain credits separately from foreground data before granting
dirtying. Bound parser frames, full group vectors (1024 maximum, never truncate),
requests/results/claims, aliases, dirty manifests and outstanding recalls in the
artifact configuration. Ordinary and isolated recovery credits coexist and
cannot be counted as the same memory. Neither these accounting reservations nor
an RSS kill threshold establish a complete runtime bound: admission must refuse
new work before losing retained dependencies or dirty-page drain capacity. Map
the workload bounds and all non-arena/runtime costs to `6r3` credits first.
Do not set a file-size limit that accidentally truncates valid journal/data
while intending to cap diagnostic logs.

Disk admission must read pool health/free space, dataset `available`, quotas,
refquotas, reservations and `usedbysnapshots`, plus guest filesystem availability.
Require budgeted run/staging bytes plus at least 8 GiB unreserved host pool
headroom; preserve at least 2 GiB guest filesystem headroom. Recheck after staging.
Refuse a degraded pool, `sync=disabled`, unknown reservation ownership, exhausted
quotas or insufficient space. Historical aggregate lab 120 GiB/per-guest 40 GiB
quotas and ten-minute replication alone do not prove available bytes. Do not delete
snapshots or alter snapshot ownership to obtain room.

Source inspection of the current component's `fourMiBLogicalBudget` in
`internal/fsservice/logical_profile_test.go` and `reserve` in
`internal/logicalview/store.go` gives a concrete minimum for one authority volume.
This is a source calculation, not a new test or a lab runtime result. Its limits
are snapshot `S=6 MiB`, claim `C=8 MiB`, versions `V=8`, claims `Q=6`, readers
`R=1`, identities `I=64`, handles `H=16`, dependencies `D=16`:

```text
overhead = I*1024 + H*256 + D*512 + 2*C = 16,855,040 bytes
ordinary = S*(V+4+2*R) + C*(2*Q+8) + overhead
         = 272,707,584 bytes = 260.07421875 MiB
recovery = 4*(S*(V+2) + C*(2*Q) + overhead)
         = 721,731,584 bytes = 688.296875 MiB
both     = 994,439,168 bytes = 948.37109375 MiB
```

Even these actual component reservations, plus the 128 MiB exporter budget,
12 GiB Rocky allocation and 3 GiB host reserve, exceed observed physical RAM
by **354,947,072 bytes (338.50390625 MiB)** before additional runtime costs.
Planning against the profile's full 512 MiB ordinary + 768 MiB recovery caps
instead gives a **702,685,184-byte (670.1328125 MiB)** deficit. Thus the existing
Rocky placement is not admissible for this profile. Post-boot free pages or
another warm-up sample cannot cure this configured-allocation shortfall.
Do not shrink the existing 4 MiB canary to make it fit.

The separately prepared 2 × 2 GiB guest proposal, with existing Rocky and native
guests stopped, leaves arithmetic room for the current component budget, but
its images, workload RAM, non-arena runtime cost and actual domain capabilities
still require admission. The table's larger 16 MiB files/64 MiB dirty data and
256 MiB payload envelope needs a **future capacity profile**, potentially larger
arenas and a new host calculation; it is not supported by the 6 MiB snapshot,
16-handle current calibration. Neither expanding that envelope nor the small
constructor defaults replace the existing 4 MiB requirement.

Rocky's root filesystem has room for the proposed scratch/staging envelope at
this observation; its 313.82 MiB free `/boot` is a distinct unresolved gate for
the exact candidate plus preserved rollback images. Do not prune its existing
kernels or enlarge a disk merely to make the installation fit.

## Exact artifacts, identities and pinned access

Before installation, `xz3` supplies a complete immutable candidate and rollback
manifest under [kernel policy](../spec/Kernel-policy.md): owned Linux9p and
FreeBSD bhyve/lib9p patch/build inputs; full kernels/modules and dependency
closure; FUSE module/control protocol; real daemon and configuration; exact
feature/schema/credential/view-policy revisions; build toolchains, bytes/digests,
signer trust and recovery instructions. `tz3`/`uz3`/`vz3` patches remain explicit
owned integration artifacts; do not make incidental edits to installed upstream
sources or substitute a version string for installed/source/runtime binding.
Booted Linux kernel, loaded modules, actual exporter executable/libraries and
native FUSE connection must all bind to the manifest before mounts are admitted.
The observed unbooted 6.1.190 candidate is not a patched admitted artifact.
Read-only host hashing observed bhyve
`c54a63615edb12eca0a62bdadba97aa39641358302f6cdcee7362a858c709865`,
lib9p.so.1 `589e8d0f9db4401f8a33274a5efe31d3cd742b18cedba87601c39617501c01ed`
and fusefs.ko `b207adf6383bee8e022fa6115044508b7c64da07e73865e5ea71594d524cc4bd`.
The first two match the prior deployed investigation's disk-byte baseline;
neither disk hashes nor executable path observations attest all in-memory bytes,
match future patched compiler inputs or supply recall capability.

Current pinned public identities, independently inherited from the existing lab
records, are:

| SSH target | Ed25519 fingerprint |
|---|---|
| f3 / 192.168.1.133 | `SHA256:VZLFFSJZ9K7iOBQ/cFehASFzCfIOn0TKFD1L89ziL5g` |
| Rocky / 192.168.1.123 | `SHA256:UCoipW2LqGWOpaRnuShloHwgo0w2sHAUthKdvPt5KsA` |

Existing controller pin files are
`/home/paul/tmp/codex/hfs-native-planning-20261029/yq3/host-known_hosts` and
`guest-known_hosts`; native guest public pins remain in
[`f3-known_hosts`](f3-known_hosts). Copy the exact relevant public entries into
the approved plan's immutable inputs, record their digests and compare them at
execution. Fingerprint printing is a local check, not fresh remote authentication.
Use `ssh -F /dev/null`, `BatchMode=yes`, `StrictHostKeyChecking=yes`,
`GlobalKnownHostsFile=/dev/null`, `UpdateHostKeys=no`, `ControlMaster=no`,
`ControlPath=none`, explicit `UserKnownHostsFile`, `ConnectTimeout=5`,
`ServerAliveInterval=3`, `ServerAliveCountMax=1`. Host contact pins
`HostName=192.168.1.133` and `HostKeyAlias=f3`; Rocky and native guests use a
ProxyCommand with the same explicit host pin policy through f3. Bound each
inventory subprocess to 40 seconds and capture stdout/stderr separately.
Never use key scanning or a broad fallback file to accept a changed key.
New hosts/guests need independently enrolled keys and fresh unique UUID/MAC/IP,
boot/export/mount/process identities, not copied native VM identities.

Each exact plan also records synthetic volume/closure and node IDs, full domain
inventory, source placement, export tags, cache/options/`msize`, private real
roots, rootfd/native connection checks, UID/GID/group scenarios, seeds and normal
restoration state. Use a unique `RUN` and descendants of a newly approved private
`/var/tmp/hfs-retained9p-RUN` root, with sibling control/volume/mount/export roots;
never reuse the existing passthrough share as an admitted daemon mount. Define
the guest mount publication verifier and K04 retained-fid behavior first.
Keep SELinux enforcing. Pod claims additionally require an explicitly admitted
isolated runtime, pinned images, numeric security contexts and ID maps; plain
Rocky is not evidence of k3s/pod capability. Preseeded files alone do not replace
dynamic namespace and changing-group workloads.

## Positive cache and CPU fence authority

Ordinary mutation barriers require every exact admitted native/guest domain's
STOPPED → protected drain → DRAINED → new-generation installation receipt,
including CPU PTE/TLB synchronization, in-flight kernel aliases, descriptions,
hard links and open-unlinked mappings. `vz3`/`fz3` must supply and validate these
mechanisms before a run can claim them. TTL zero, successful `fsync`, unmount
attempt, stale registry removal, SSH loss and a recall timeout are not receipts.
A busy/pinned alias or missing acknowledgement leaves the closure gated.

For a failed domain, a separately authorized normal VM shutdown may establish
CPU exclusion only when the external controller positively verifies the exact
VM lifetime is stopped, its matching bhyve process has exited and all enrolled
guest domains for that incarnation are excluded from rejoin. Persist the
revocation/fence lineage before admitting a successor. Normal unmount plus
callback/alias drain can retire a cooperative mount, but killing a daemon or
detaching networking cannot prove stale guest mappings stopped executing.

For B0 the proposed operator is Paul, through the pinned `paul@f3` management
connection and the existing lab helper's normal `stop-rocky` action under its
concrete approved window. The runner must verify the recorded VM/process
incarnation before that action and bind its completed observation to every old
boot/export/cache incarnation. No stop is authorized by this proposal. Later
hosts require their own named operators and scoped management principals; no
new delegated fence credential or crash-fence implementation is provisioned
here. f3 itself is not disposable: host power-off/reset is outside B0. When the host is
unreachable, a VM-list absence cannot be observed; keep successor serving gated.
Later physical-host failure tests need separately approved positive host fence
authority, independent management and durable receipts for every affected domain.
An etcd lease, network partition or elapsed deadline never substitutes. Do not
grant canary credentials production host/VM fencing power.

## Execution gates, evidence and normal restoration

1. Refresh the completed read-only inventory and obtain artifact/build and
   remaining host/guest capability records. Resolve every unknown above.
   Identify actual spare distributed hosts
   before D0/D1. Review the complete resource mapping and runtime/refusal checks.
2. Prepare a concrete installation/lifecycle proposal with exact target, bytes,
   temporary candidate boot, retained stock default, console/management access,
   impact, rollback and the bounded profile. Paul's instruction allowing f3
   startup does not authorize stopping Rocky, changing its kernel, installing
   patches, mounting, extra VM creation or a disruptive test. Critical windows
   retain their separate concrete decision; routine local verification follows
   the current session authorization. This document is not a run approval token.
3. The approved runner records the observed initial state before mutation,
   reserves only new run-owned resources, proves exact running artifacts and
   mounts, then invokes the bounded synthetic oracle. Changed artifacts,
   topology, limits or disruption scope need a revised plan. No implementation
   or approval is inferred from an `--approved-run` flag.
4. Collect machine-readable case results, external invocation/reply/read oracle,
   kernel/module/exporter/daemon identities, credentials/SELinux records, source
   and claim coverage, recall/fence/install receipts, quotas, mount/process/VM
   inventory, errno and timings outside the lab. Keep passes, unsupported,
   unrun, failed and uncertain cases distinct. A missing prerequisite or required
   case keeps acceptance incomplete; model/local results are not runtime proof.
5. Stop dependent probes on failure. Close run-owned applications/mappings and
   drain accepted operations; normally unmount guest 9p before host FUSE; verify
   exact owned mounts/processes are gone before removing empty mount directories.
   Preserve journal/control/source/oracle evidence and failed/busy resources.
   Never recurse through a live mount, force unmount/kill, overwrite disks or
   discard an uncertain accepted operation to finish cleanup.
6. Restore only the explicitly recorded approved state with normal VM stop/start
   and boot configuration restoration. Verify pinned SSH, actual default/running
   kernel, VM allocation, processes, mounts, space and swap afterward. Do not
   boot native guests alongside Rocky. An ambiguous SSH/cleanup result remains
   ambiguous until completed observation/reconciliation; do not retry mutations
   blindly. Report unsuccessful/incomplete cleanup separately from test failure.

The present deliverable checks planning consistency and public-key fingerprints
locally, and records completed pinned read-only host and Rocky inventory after
the initial failed connection and orchestrator's authorized f3 wake. No build,
integration test, provisioning, patch installation, mount, CPU/cache fence or
HomelabFS acceptance run is claimed.
