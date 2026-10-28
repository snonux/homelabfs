# Synthetic canary volumes and placement

[Patterns](Canary-patterns.md) · [Volume policy](spec/Volumes.md) ·
[Capacity](Capacity-plan.md) · [Acceptance profiles](spec/Acceptance-profiles.md) ·
[Kubernetes publication](Kubernetes-and-migration.md)

## Status and boundary

This is the fixture mapping for task `gv3`, prepared on 2026-10-28. It maps
P1–P4 to seven independent canonical volumes and P5 to an explicit unsupported
publication request. These are planned identities and paths, not allocated
volumes, admitted nodes, running mounts or compatibility results. No live
registry/collision inspection, infrastructure operation or workload test was
performed for this document. The Go loopback PoC is not the filesystem described
here. The manifest shape below is a design example, not an implemented management
API or a complete runnable lab plan.

Only deterministic synthetic bytes belong in these volumes. Do not copy
production files, reuse production PV/PVC names, attach existing consumers, or
change production NFS, stunnel, CARP, monitoring or power automation. PVC request
sizes are neither fixture sizes nor capacity measurements. Production migration
and NFS retirement belong to a separate project.

The [explicit application transaction API](spec/Transactions.md)
has a separate design contract and implementation/conformance gates. Initial fixtures
use ordinary operations on per-volume mounts, with cross-volume rename/link
returning `EXDEV`. They do not invoke or validate the transaction API. A future
transaction fixture needs its own operation ledger and approved plan; it does
not turn these independent roots into a federated application parent.

## Identity derivation and durable manifest

For a new run generate a fresh random UUIDv4 `R`; persist it before allocating
anything. Define `K` as its 32 lowercase hexadecimal digits without hyphens and
use `K` as the native runner's `run_id`. For each label in the table below,
derive `V(label) = UUIDv5(namespace=R, name="homelabfs/canary/" + label)`.
Retries of the same approved run reconcile the same manifest; a new run always
gets a new `R`, even if the previous run failed. UUID generation alone does not
prove freshness: registry checks below are mandatory.

| Label | Illustrative `V(label)` for the example `R` below |
|---|---|
| `music` | `6c2bd0f2-37c6-5f56-a32a-b6cbb15d9e7b` |
| `music-state` | `0c2193b6-bdd0-5ee2-bb9d-f9fdf85c09de` |
| `player-private` | `16cfde2c-4342-5781-8e34-bed18ee0b1b2` |
| `player-libraries` | `edf0463e-ff05-57b2-874c-5352d9e31b28` |
| `sync-config` | `84158f9c-3c8b-5943-982d-ead833eed30a` |
| `sync-data` | `7e64c4f5-34d7-5c05-816f-7d462809b709` |
| `video` | `1f80f494-6897-550c-8b98-ecf1dfe9ba9d` |

Example `R = 05f4d88f-e53d-4fce-9f56-fad7ed48e680` is documentary only: none
of these IDs has been admitted or inspected. Do not reuse the example in a run.
The following template shows how a future plan expands its own fresh values:

```yaml
run_uuid: R
run_id: K
status: proposed-not-admitted
namespace: hfs-K
manifest_owner: exact-approved-plan-hash
replicas: [hfs-test0, hfs-test1, hfs-test2] # targets, not admitted node IDs
volumes: # seven entries, one for each label above
  - label: music
    uuid: V(music)
    name: hfs-K-music
    initial_owner_target: hfs-test0
    sync_target: hfs-test1
    async_target: hfs-test2
    per_target: # instantiate for N=0,1,2 on its matching target
      data_dataset: hfsdataN/data/hfs-run-K/V(music)
      journal_dataset: hfsdataN/journal/hfs-run-K/V(music)
      control_record: hfsdataN/control/hfs-run-K/volumes/V(music)
      fuse_mount: /var/tmp/hfs-run-K/mnt/V(music)
    views:
      - {id: music-reader, subtree: ., access: ro, client: p1-reader}
      - {id: music-writer, subtree: ., access: rw, client: p1-writer}
      - {id: player-music, subtree: ., access: ro, client: p2-player}
    data_refquota_mib: 12
    journal_refquota_mib: 24
publication: # S only; N has no Kubernetes objects or 9p shares
  pvc: hfs-K-VIEW
  pv: hfs-K-VIEW
  volume_handle: V(label)
  claim_view: [V(label), RELATIVE_SUBTREE, ro-or-rw]
  share_tag: hfs-FIRST12(K)-FIRST8(V(label))
  guest_mount: /var/tmp/hfs-run-K/volumes/V(label)
```

`VIEW` is a unique view ID from the mapping, not another volume UUID. The
proposed Kubernetes namespace is `hfs-K`; cluster-scoped PV names include the
same full `K`. Names, labels, share tags and object UIDs are recorded explicitly,
including a `homelabfs-run=K` label where supported. Check resulting name/tag
syntax and transport length limits before approval; a truncated share tag must
still be collision-free and map to the full UUID in the manifest. No production
StorageClass or existing claim is substituted. Native N leaves all S-only fields
unallocated; S must resolve actual host/guest export paths before admission.

Store the expanded manifest, approved plan/hash, expected-content/hash ledger,
resolved target identities, quota ledger, view graph and resource inventory in
the external controller's durable `state_dir`/`evidence_dir`, outside the tested
cluster. Record immutable backing dataset identities and actual mountpoints,
not only these template names. Controller paths must be distinct absolute paths
under a dedicated run directory and have the external evidence capacity below.
Do not make the only ownership record a file inside a disposable volume.

## Canonical roots, contents and aggregate allocation

Each row is a separate canonical data root `.../data/hfs-run-K/V(label)` with a
separate sibling journal root `.../journal/hfs-run-K/V(label)`. None is below
another canonical root. `R/music` in the pattern document means the logical
contents of the `music` row, not a shared mounted parent named `R`. Journal and
control storage are never exported. Subtrees and aliases consume their row's
existing history, allocation and replica budget.

The logical limits below include seed data, temporary replacements, state,
permission probes and peak simultaneous old/new files. Counts include each
canonical volume's root directory; publication mountpoint directories outside
the volumes are charged to control/metadata storage. File limits are maxima,
not a request to fill all rows. Every file is at most 64 KiB.

| Label / patterns | Required seed paths relative to its root | Live payload KiB | Files | Dirs | Symlinks | Data MiB | Journal MiB |
|---|---|---:|---:|---:|---:|---:|---:|
| `music` / P1,P2 | `Album A/{track.bin,cover.bin}` | 768 | 12 | 4 | 1 | 12 | 24 |
| `music-state` / P1 | `state.bin` | 256 | 4 | 2 | 0 | 4 | 8 |
| `player-private` / P2 | `data/state.bin`, `media/marker.bin`, `thumbnails/thumb.bin`; empty reserved `media/{music,sync,libraries}` mountpoints | 1024 | 16 | 8 | 2 | 16 | 32 |
| `player-libraries` / P2 | `{audiobooks,podcasts}/sample.bin` | 512 | 8 | 4 | 1 | 8 | 8 |
| `sync-config` / P3 | `state.bin` | 256 | 4 | 2 | 0 | 4 | 8 |
| `sync-data` / P3,P2 | `Camera/Irregular Ninja/{photo-1.bin,photo-2.bin}` | 768 | 12 | 6 | 2 | 12 | 32 |
| `video` / P4 | `clip.bin` | 512 | 8 | 6 | 2 | 8 | 16 |
| **Entire fixture set** | P5 allocates no objects | **4096** | **64** | **32** | **8** | **64** | **128** |

Optional symlink probes stay within already authorized views; none recreates
P5's unsupported aggregate namespace. The root counts leave room for bounded
temporary directories, including a directory rename in `sync-data`. Writers
must acquire aggregate file/byte/operation credits before creating temporary
objects. An alias read consumes no additional payload allocation. Seed all
required subdirectories before publication; the missing-subdirectory negative
case deliberately withholds one in its own planned phase and must not create an
empty fallback mount.

All rows together retain the [common profile](spec/Acceptance-profiles.md#common-workload-version-and-cache-parameters):
4 MiB live payload, 64 files, 32 directories, eight symlinks, 256 mutations
including seeding and probes, and at most five minutes of fixture mutations.
One active mutation/barrier chain and one reader stream run at a time, at most
four measured chains/s. The maximum application write payload is
`256 × 64 KiB = 16 MiB`; encoded records and physical writes have separate caps.
Reserve at most 64 mutations for seeding, 64 for a selected measured series and
128 for semantic/permission/alias probes. A concrete syscall/daemon-request
ledger must fit these limits before execution; this mapping does not assert
that every oracle fits one run. Select separate runs for additional cases or
recovery, each with fresh identities. Cleanup has its own bounded ledger.

## Views and complete placement constraints

Paths below are client-visible fixture paths under a private client mount
namespace. They are not host backing paths. Every view records its full
canonical UUID, relative subtree and enforced access policy. RW still requires
Unix permissions. RO is enforced at publication and for every mutation; it is
not inferred from the fixture client's intended behavior.

| Pattern / client | View IDs → `(label, subtree, access)` → client path | Placement and verdict |
|---|---|---|
| P1 reader | `music-reader` → `(music, ., RO)` → `/music` | Same active owner as P1 writer; current reads never use a passive replica |
| P1 artwork writer | `music-writer` → `(music, ., RW)` → `/music`; `music-state` → `(music-state, ., RW)` → `/state` | Serialize writers; music and state must both be ready before publication |
| P2 Player-like reader/writer | `player-data` → `(player-private, data, RW)` → `/data`; `player-media` → `(player-private, media, RW)` → `/media`; `player-thumbnails` → `(player-private, thumbnails, RW)` → `/thumbnails`; `player-music` → `(music, ., RO)` → `/media/music`; `player-sync` → `(sync-data, Camera/Irregular Ninja, RO)` → `/media/sync`; `player-libraries` → `(player-libraries, ., RO)` → `/media/libraries` | Publish the entire set together, including the nested RO mounts over reserved empty mountpoints; no access to an empty underlying mountpoint while gated |
| P3 updater | `sync-config` → `(sync-config, ., RW)` → `/config`; `sync-data` → `(sync-data, ., RW)` → `/data` | Same owner as P2; the P2 subtree is a view of this UUID, never another data root |
| P4 alias client | `video-rw` → `(video, ., RW)` → `/external-videos-rw`; `video-ro` → `(video, ., RO)` → `/external-videos-ro` | Both claims use the same UUID, history, owner and epoch; one data charge, no copies |
| P5 generator/updater request | Rejected request ID `hfs-K-p5-parent`; requested parent `/tree` with source `syncthing/data/Camera/Irregular Ninja`, `shuriken.sh/incoming/pictures -> ../../syncthing/data/Camera/Irregular Ninja`, and output `shuriken.sh/site/dist` | **Unsupported/rejected as specified**: no canonical tree/output UUID, backing root, share, mount, PV or PVC is allocated or published |

P2's nested client mounts are a complete mount set over explicitly reserved
paths. They do not expose an administrative parent or permit an application to
manufacture overlapping canonical roots. Block this profile if the selected
adapter/transport cannot enforce nested RO views and complete gating. In
particular, separate independent source/output mounts cannot pass P5's relative
parent-layout oracle, even if a client can manually access both.

The transitive placement closure **A** contains six UUIDs: `music`,
`music-state`, `player-private`, `player-libraries`, `sync-config`, `sync-data`,
and all P1/P2/P3 clients. Its initial native owner target is `hfs-test0`, with
`hfs-test1` synchronous and `hfs-test2` asynchronous. All six must have recovered
authoritative prefixes, valid individual ownership/epoch, fresh local state and
a ready synchronous partner before any dependent client is published. The
pin is an initial fixture choice, not a permanent topology requirement or one
merged replication group. A later approved owner transfer gates all affected
clients and coordinates the complete closure; moving one volume and leaving
incompatible active clients is forbidden.

Closure **B** is the video UUID and both P4 aliases. Its initial native owner is
`hfs-test1`, synchronous `hfs-test2`, asynchronous `hfs-test0`. It can be placed
independently of A. Every UUID has a replica on all three targets; neither
nonowner supplies current workload reads. Target names must be resolved to
actual admitted node identities before execution; this document invents none.

N uses direct FUSE inside the three prepared guests, with clients on each
closure's owner. No Kubernetes, Rocky or 9p is part of N. The prospective S
placement is A on f0/r0 (sync f1, async f2), B on f1/r1 (sync f2, async f0),
subject to a later exact export/share/guest path manifest and admission. **S
remains not admitted** under the conservative RAM budget, and its recorded
guest kernels lack required 9p support. R is a separate, presently not-admitted
loopback transport profile; never run it alongside the native guests. This
mapping authorizes no boot, installation, kernel change or allocation.

Use the following planned synthetic credentials consistently across replicas;
record their actual representation through every selected path. These are
fixture caller IDs, not requests to create or change host accounts. If the
isolated client cannot represent them without changing production identities,
block publication and revise the plan rather than remap them silently.

| Caller / ownership | UID:GID | Initial permissions and purpose |
|---|---|---|
| P1 writer, P3 updater, P4 alias client and seed owner | `61001:61000` | Shared roots/directories `0755`, ordinary readable files `0644`; private music-state/sync-config roots `0700`, files `0600` |
| P1 reader and P2 client | `65534:65534`, no supplementary groups | RO shared views; owns player-private directories `0700` and files `0600`, preserving writable private state |
| Shared-group probe reader | `61002:61000` | Reads a writer-owned `0640` protected probe through searchable directories |
| Unrelated denied probe caller | `61003:61003`, no supplementary groups | A writer-owned `0640` protected probe rejects reads; RO views reject writes independently of Unix mode |

Set umask `0077` and explicitly set final seed modes; do not depend on ambient
umask or chart defaults. Library seeds are writer-owned `0644` under `0755`
directories and published RO. Video starts with searchable `0755` directories
and writer-owned `0644` files; its RO alias still rejects all mutations for
UID 61001. P3's selected protected photo starts `0640`, changes to `0644` to
grant UID 65534 a fresh read, then returns to `0640` to deny a fresh open.
The shared-group probe remains allowed in both modes. Include explicit denied
directory-search probes within the existing directory/mutation budgets.
Existing open descriptors do not establish retroactive revocation. Every
ownership/mode mutation and its expected result belongs in the external ledger.

## Rejection and oracle matrix

| Case | Required result before/after operation |
|---|---|
| P1/P3/P2 split owners, stale replica, missing share/subdirectory or incomplete view set | Keep the whole affected publication unavailable, with the exact placement/readiness reason; no partial client or fallback data |
| P4 separate UUIDs or owners for RW/RO aliases | Reject configuration/publication without allocating another history; unsupported enforcement blocks the full alias fixture |
| P5 parent aggregation, independent overlapping roots or cross-owner symlink traversal | Explicit unsupported-publication result before allocation; no usable parent/child mount set |
| Malformed/escaping subtree, duplicate/ancestor root, registry collision or unknown ownership | Reject admission/publication without effects; do not adopt the existing resource |
| RO create/write/rename/unlink and denied Unix traversal/read/write | Record exact permission/RO error and unchanged namespace/content; a timeout is not proof of correct rejection |
| Ordinary cross-volume rename/replacement/hard link | `EXDEV`, with both namespaces unchanged; no copy-and-delete fallback |
| Rename/link across separately mounted aliases of the same UUID | `EXDEV` is permitted; not evidence of a second canonical identity |

Positive checks retain the [pattern oracles](Canary-patterns.md): P1 synchronized
replacement/metadata freshness, P2 complete multi-volume reads with private-only
writes, P3 names/content/timestamps/permission changes, and P4 coherent RW/RO
aliases. Warm and cold paths, explicit denied calls and exact sync boundaries
are recorded separately. Notifications, application/database recovery,
cross-mount locks, transaction API behavior and independent physical-host
reliability remain unproven. Reports distinguish passed, failed,
unsupported/rejected as specified, not run and not admitted; P5 is never a
positive pass obtained by narrowing its layout.

## Storage quota translation and readiness

The 64 MiB data and 128 MiB journal totals above are base **allocated-space**
ceilings per replica across seven volumes. Parent base limits enforce the
aggregate as well as individual child limits. Control uses 64 MiB and dedicated
test etcd 128 MiB per replica; production k3s etcd is never used. Per-volume
limits are not multiplied for each alias, stream, mount or PV.

The native runner admits resources below
`hfsdataN/{data,journal,control}/hfs-run-K` on matching `hfs-testN`, and temporary
directories below `/var/tmp/hfs-run-K`. These three sibling run trees are one
logical run allocation, not three independent 4 GiB allowances. A possible
quota split below translates the capacity plan to this typed path layout.
Paired checkpoints/recovery sets span the sibling trees and carry one durable
manifest. Control/coordination staging stays outside per-volume rollback scope.
For this native mapping, set the run's control dataset mountpoint to
`/var/tmp/hfs-run-K`; its private `mnt`, helper and evidence directories then
consume the control-tree budget. Verify that exact dataset/mount identity before
writing there; never fall back to the OS filesystem when it is absent. Expose
only each FUSE child mount to clients, never this control parent. Data/journal
dataset mountpoints remain separately resolved administrative paths. The plan
declares the three parent dataset resources; directories inside their mounted
trees are owned subpaths, not additional independent capacity reservations.

| MiB per replica, whole fixture set | Data run tree | Journal run tree | Control run tree | Total |
|---|---:|---:|---:|---:|
| Current base | 64 | 128 | 192 | 384 |
| Two retained complete recovery sets | 128 | 256 | 384 | 768 |
| One checkpoint staging | 64 | 128 | 192 | 384 |
| One rebuild/restore staging | 64 | 128 | 192 | 384 |
| Evidence | 0 | 0 | 256 | 256 |
| Failed-resource/cleanup reserve | 0 | 0 | 256 | 256 |
| Allocation/metadata allowance | 128 | 256 | 256 | 640 |
| **Maximum planned used** | **448** | **896** | **1728** | **3072** |
| Unused emergency headroom | 256 | 256 | 512 | 1024 |
| **Run-tree quota / admission reservation** | **704** | **1152** | **2240** | **4096** |

All rows count actual allocated bytes, including snapshots/clones/holds and
staged copies, without assuming compression or sharing. Current data/journal
trees have separate child `refquota` ceilings of 64/128 MiB; control and etcd
have 64/128 MiB base child ceilings. The top three tree quotas include snapshots
and staging. Across base plus two retained versions, data/journal/control/etcd
remain 192/384/192/384 MiB respectively. Metadata allowances are additional
overhead, not extra workload files or journal obligations. Failed resources
retain their original charges; the reserve adds cleanup/partial-resource space,
not permission to double the same bytes or start another run.

Thus `384 + 768 + 384 + 384 + 256 + 256 + 640 = 3072 MiB`, plus 1024 MiB
protected headroom = 4 GiB per node. Across three replicas reserve 12 GiB, with
at most 9 GiB planned used. Declare non-overlapping parent resources in the
typed runner plan; do not sum a parent cap and its children's caps twice.
Temporary mount/helper resources must fit their declared control/evidence
charges, not gain another allowance. If physical storage differs from this
split, revise the exact plan and retain the same aggregate ceiling before
approval. The current runner's path checks do not implement quotas or prove
this accounting; reviewed installation/drivers must enforce it before admission.

Stop new mutations at 2.5 GiB aggregate use or 75% of any base allowance;
3 GiB is the hard planned-use ceiling. Forecast/credit accounting must also
prevent an individual child cap or staging limit from being exceeded. At most
one bulk operation runs cluster-wide; one checkpoint and one rebuild can remain
charged concurrently. A recovery set including its suffix must fit 384 MiB.
Never delete needed history or pinned snapshots to fit a limit.

Readiness additionally requires every [capacity equation](Capacity-plan.md),
not just these quotas: measured pool `A >= F + G + Q`, inner and outer quotas,
RAM/ARC/guest allocations, CPU/IO/network residuals and rate enforcement. N
reserves 32 GiB incremental outer history on both source and receiver pools;
external evidence reserves a further 1 GiB at its actual destination. Existing
outer zrepl history is additional to the two inner recovery sets; change no
snapshot ownership or received sink. N's existing 2 GiB guest and full host
memory envelope, 64 MiB aggregate daemon payload queue, 1.5-core aggregate CPU,
4 MiB/s and 100 IOPS physical-host I/O and 4 MiB/s network-direction caps remain
binding. Setup, fixture, recovery, collection and cleanup/restoration together
fit 20 minutes; incomplete preservation may outlast the window and is reported.
These are readiness candidates, not measured fit. No N/S/R profile is admitted
by this mapping or by a historical successful probe.

## Preflight proof of production disjointness

Fresh names and a `/var/tmp` prefix alone do not prove isolation. Before a
future approved execution allocates or publishes anything, a pinned preflight
must compare its expanded manifest against the **actual** authoritative volume
registry, admitted/retired identity ledger, existing run manifests, configured
backing dataset/export inventory and (when used) Kubernetes API objects and
mount/share inventory. Use focused metadata inspection, not a production-data
copy or an exhaustive file scan. If the registry/ownership inventory is missing,
incomplete, stale or cannot represent a proposed alias, fail closed.

1. Resolve actual target identities and approved empty lab roots. Compare every
   run/volume UUID, group/history identity, name, view, share tag, namespace,
   PV/PVC name/UID and volume handle with production, retained and other-run
   entries. Reject collision with any existing or retired identity. For an
   idempotent same-run retry, require the same approved plan/hash and exact
   immutable resource identity/ownership; matching names alone cannot adopt it.
2. Inspect the actual backing identity and canonical ancestor chain on every
   replica: dataset GUID, pool, filesystem/mount identities, mountpoint/export
   source and resolved parent directories. Proposed leaves must be absent for
   initial allocation; existing parents must be approved run-container roots.
   Resolve symlinks and mount crossings without following unknown paths for
   mutation. Reject equal or ancestor/descendant backing roots, bind/nullfs/9p
   aliases, unexpected mounts, clones or references that could alias production.
   Compare identities as well as normalized path components; string prefixes
   alone cannot establish non-overlap.
3. Exclude `/data/nfs/k3svolumes`, every registered production backing/export
   path and all received sink datasets. Require no proposed PV to refer to an
   existing production claim, hostPath, NFS endpoint, CSI handle or backing
   identity. Inspect actual registry/API mappings rather than assuming distinct
   PV names mean distinct storage. Do not modify these existing objects.
4. Check that the seven canonical roots are mutually disjoint. The explicit
   views are the only sharing: music has three claims, player-private three
   subtrees, sync-data a full RW and restricted RO view, video two aliases.
   Validate subtree containment, required directories, caller credentials and
   the complete placement graph. Reject P5 before creating any parent resources.
5. Persist the observed registry revision, object/backing identities, inspection
   time, results and exact plan/hash outside the cluster. Reserve the names and
   identities atomically against that registry revision before creation; verify
   ownership again at allocation/publication to prevent a check/use race. If
   the implementation cannot provide equivalent exclusive admission, remain
   blocked rather than rely on a dated scan. Recheck actual VM/resource state
   inside the approved lifecycle window; only then apply the readiness gates.

This is the required future proof procedure. It is **not a claim that live
collision inspection has been completed**. Every allocation, installation,
mount and test retains its own [approval boundary](Test-automation.md); this
document supplies no executable approval or inferred authority.

## Unpublish, preservation and cleanup ownership

The approved plan owns exactly the expanded manifest and its immutable resource
identities, including all seven volumes, each view/share/mount, snapshots/holds,
staging objects, temporary directories and controller evidence. Persist creation
intent before each operation and terminal results afterward, so interrupted
setup is reconciled without creating another history. Keep the runner's durable
checkpoint and external manifest after a failed guest or controller.

1. Stop new publication and quiesce synthetic clients under bounded deadlines.
   Record final acknowledged positions and collect the external oracle, actual
   owner/epoch, high-water usage, denial results and remaining-resource inventory.
2. Unpublish every alias and dependent client set; prove guest descriptors,
   mappings and exports are revoked under the selected deployment contract.
   Removing a PVC alone is insufficient. Normally unmount client/guest views,
   then exports/FUSE, and record each result. Never force unmount, force-kill
   Rocky, or delete through a live mount to pass cleanup.
3. Preserve failed data, journals, compatible recovery manifests and diagnostics
   within their charged bounds. Record exact retained paths/UUIDs, bytes,
   snapshots/holds, reason and owner. Unknown or uncertain operations block
   dependent cleanup; do not infer success from a missing SSH connection.
4. Retire identities only after authority and all access paths are safely
   closed. Reclamation is a separate authorized manifest-bound operation after
   retention, recovery/catch-up pins, snapshot ownership and diagnostics permit
   it. Verify immutable identity and run ownership immediately before deletion;
   never recursively clean an unknown directory, another run or a received sink.
5. Release capacity only after inspecting actual remaining charges, including
   snapshots, clones and subsequent outer receives. A failed/uncertain unmount
   preserves the resource and failure report for inspection. A fresh run cannot
   silently consume its budget. Restore reusable VM state only via its approved
   lifecycle steps; retained data does not authorize new VM operations.

The final report links each selected pattern/view to passed, failed,
unsupported/rejected, not admitted or not run evidence, and separately records
cleanup/restoration failures and retained resources. Native N evidence and any
later guest/9p evidence remain separate. No result authorizes production changes.
