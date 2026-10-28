# Representative synthetic canary patterns

[Filesystem semantics](Filesystem-semantics.md) · [Kubernetes canaries](Kubernetes-and-migration.md) · [Deployment readiness](Deployment-readiness.md)

These five selected workload shapes inform volume semantics (`9q3`) and later
canary fixture mapping (`gv3`). They are requirements and fixture designs, not
implemented fixtures or compatibility results. No tests, lab operations or
deployment were performed to write this document. Every installation and test
run still needs its own [explicit approval](Test-automation.md).

Use synthetic bytes in disposable, run-owned volumes only. Existing workloads,
PVs, data, NFS, stunnel, CARP, monitoring and power automation remain in place.
This is neither an exhaustive consumer inventory nor a production migration
plan. Remote earth access and NFS retirement belong to a possible future project.

## Read-only source evidence

Selected local files in `~/git/conf` were read on **2026-10-28**, at clean
revision `aa8793fe3743f619f9247754a7e53acec43c1500`. The links below open that
local checkout; paths are under `~/git/conf`. They describe configured
storage shapes, not current mounts, scheduling, permissions or runtime health.
No cluster queries, production directory scans or remote requests were made.
IQ's workload index and Player topic supplied navigation context; the chart
fields below are the configuration evidence.

| Source | Selected evidence |
|---|---|
| S1: [Navidrome PVs](/home/paul/git/conf/f3s/navidrome/helm-chart/templates/persistent-volume.yaml:50), [deployment](/home/paul/git/conf/f3s/navidrome/helm-chart/templates/deployment.yaml:103) | Music claim points to `/data/nfs/k3svolumes/navidrome/music`; Navidrome mounts it at `/music`. PV comments call it a reader, but the application container mount has **no `readOnly: true`**. The synthetic reader role below is an explicit canary policy, not a claim that production enforces it. |
| S2: [beets-art job](/home/paul/git/conf/f3s/beets-art/helm-chart/templates/cronjob.yaml:25), [state PV](/home/paul/git/conf/f3s/beets-art/helm-chart/templates/persistent-volume.yaml:15) | Job uses `navidrome-music-pvc` at `/music`, separate writable `/state`, and `concurrencyPolicy: Forbid`; commands import, fetch and embed artwork. This is configuration intent, not proof that a second writer cannot exist. |
| S3: [Player PVs](/home/paul/git/conf/f3s/player/helm-chart/templates/persistent-volume.yaml:1), [deployment](/home/paul/git/conf/f3s/player/helm-chart/templates/deployment.yaml:219) | Private data/media/thumbnails plus read-only music, Player libraries and Syncthing data. Music aliases S1's path; Syncthing aliases S4's data path. The nested Syncthing `subPath` contains spaces. Read-only mounts have separate writable thumbnails. |
| S4: [Syncthing PVs](/home/paul/git/conf/f3s/syncthing/helm-chart/templates/persistent-volume.yaml:1), [deployment](/home/paul/git/conf/f3s/syncthing/helm-chart/templates/deployment.yaml:90) | Separate config and data roots, mounted writable at `/config` and `/data`. S3 reads a subtree of data. The charts do not establish exact update algorithms or notification guarantees. |
| S5: [Immich video PVs](/home/paul/git/conf/f3s/immich/helm-chart/templates/persistent-volume.yaml:176), [ArgoCD application values](/home/paul/git/conf/f3s/argocd-apps/services/immich.yaml:126) | Distinct RW and RO video PV/PVC names point to the **same** `/data/nfs/k3svolumes/immich/external-library/videos` path. Application values mount `/external-videos-rw` and `/external-videos-ro`, the latter explicitly read-only. |
| S6: [Shuriken parent PV](/home/paul/git/conf/f3s/shuriken/helm-chart/templates/persistent-volumes.yaml:1), [job mount](/home/paul/git/conf/f3s/shuriken/helm-chart/templates/cronjob.yaml:227), [site configuration](/home/paul/git/conf/f3s/shuriken/helm-chart/templates/configmap.yaml:1) | Parent `/data/nfs/k3svolumes` is exposed at `/data` so relative symlinks from `shuriken.sh/incoming` into `syncthing` resolve. Output `dist` is a regular directory to allow staging rename. These comments do not prove actual symlink targets or restrict writes to output. |

Player's init checks also require library subdirectories to exist before
publication ([deployment lines 74–159](/home/paul/git/conf/f3s/player/helm-chart/templates/deployment.yaml:74)).
Its caller identity is configured as UID/GID 65534 ([lines 20–24](/home/paul/git/conf/f3s/player/helm-chart/templates/deployment.yaml:20)).
The fixtures preserve those semantic concerns without reproducing production
credentials, application databases or media.

## Common fixture contract

`R` denotes a fresh, manifest-owned canary namespace, never an existing
production directory. `music`, `sync`, `player`, `video` and `tree` below are
logical fixture names, not assigned dataset roots or volume UUIDs. `gv3` assigns
fresh identities, actual roots and publication mappings after `9q3` defines
canonical volumes, ownership and overlap policy. No fixture may alias a production
path, PV/PVC, mount, replication group or backing directory.

All five fixtures together have a proposed ceiling of 64 regular files, 32
directories and 8 symlinks, at most 64 KiB per file and 4 MiB of live file
payload, including temporary and state files. Use deterministic synthetic
contents containing run ID, fixture ID and revision, with expected lengths and
hashes recorded outside the test volumes. Limit an execution plan to 256
workload mutations and a five-minute fixture phase with finite per-operation
deadlines; stop dependent work when a deadline expires. These are bounds for a
small proposed profile, not measured performance acceptance criteria or resource
admission. `fv3` must separately budget replicas, journal growth, checkpoints,
snapshots, rebuilds, evidence and cleanup reserves; `jv3` owns calibrated numeric
acceptance criteria. Real applications and their unbounded scans/background
tasks are outside this minimal fixture profile.

Start with ordinary file-operation clients using synthetic writer and reader
identities. Include a reader with UID/GID 65534, one shared-group reader, and an
unrelated denied user. The approved plan assigns consistent numeric IDs without
changing host accounts used by production. Use explicit ownership/mode/umask
expectations; do not copy the charts' permissive or privileged setup wholesale.
Verify directory search permission, fresh `stat` metadata, allowed reads, denied
reads and denied writes separately from node ownership.

“Supported” below means a required behavior to implement and demonstrate, not a
current capability. All workload reads, including read-only aliases, use the
active owner. Passive replicas never supply allegedly current workload reads.
Co-located clients must observe the same committed namespace and permission
state, with the [durability and freshness contract](spec/Durability.md).
For guest clients, use the profile's verified sync boundary before asserting
visibility; an ordinary buffered guest write is not evidence that the daemon
has committed it. Publication remains blocked until every required volume and
access path is ready. Record exact rejected operation/status and absence of
side effects; a timeout alone is not evidence of correct rejection.

## P1: Shared music reader and artwork writer

**Sources:** S1, S2. The concern is a reader observing another client's updates
to a shared library, with separate writer state.

**Fixture:** `R/music/Album A/{track.bin,cover.bin}` and
`R/music-writer/state.bin`. Give the two library files small distinct revision
payloads. Publish the same music identity to a read-only library reader and one
writable artwork client on the same owner; publish private writable state to
the writer. Serialize fixture writers explicitly rather than treating a job
concurrency setting as storage fencing.

**Supported behavior and oracle:** Warm the reader's lookup/data caches, then
have the writer replace cover content with temp-file/write/sync/rename and
change track data and metadata. After the verified barrier, a fresh reader
lookup/open must return the committed revision, size, mode and timestamp;
replacement must expose a complete old or complete new file, never a partial
namespace update. Do not require atomicity of concurrent in-place data writes:
assert their contents only after synchronization. A read-only reader's attempted
create, overwrite, rename and unlink must fail without changing shared data.
Reader and writer remain in one serving-owner domain during publication.

**Explicit rejection:** Concurrent serving of this volume on different owners,
or satisfying the reader with a passive replica, is outside the baseline and
must fail publication. Storage ownership does not replace ordinary same-owner
application locks. This fixture does not certify beets, Navidrome or SQLite.

## P2: Player-like multi-volume reader

**Sources:** S3 with S1 and S4. One reader links otherwise separate music and
Syncthing sharing relationships; volume placement constraints are transitive.

**Fixture:** One client has private writable `R/player/{data,media,thumbnails}`,
read-only P1 music under its media view, read-only
`R/sync/data/Camera/Irregular Ninja`, and a small read-only
`R/player-libraries/{audiobooks,podcasts}` view. Private files are synthetic
state/cache markers, not database copies. This reuses P1/P3 data; it does not
make new copies for the additional read-only claims.

**Supported behavior and oracle:** Publish the complete dependency set on one
worker whose colocated host owns every required canonical volume. Read known
hashes from both shared sources while writing a thumbnail and state marker only
to private writable views. After P1/P3 committed updates, reopen through this
client and verify fresh bytes. Deny writes to every read-only source, including
attempts to put thumbnails beside it. A deliberately absent required subdirectory
must prevent successful publication; do not create an empty replacement tree.

**Explicit rejection:** If music can only serve on owner A and sync can only
serve on owner B, reject or keep the whole publication pending, with an explicit
placement conflict. Do not start the reader with a partial or stale mount set.
The dependency includes P1's writer and P3's updater as well as the Player-like
client, so considering each PVC independently is insufficient. `9q3` chooses
whether common placement uses separate co-located volumes or a shared group;
this document assigns neither. Cross-volume rename must fail with `EXDEV`, and
there is no cross-volume transaction or atomic snapshot promise.

## P3: Syncthing-like updates with a subtree reader

**Sources:** S4 and S3. Temporary replacement, rename, deletion and metadata
updates are selected semantic probes, not an assertion about Syncthing's exact
implementation or its deployed settings.

**Fixture:** `R/sync/config/state.bin` and
`R/sync/data/Camera/Irregular Ninja/{photo-1.bin,photo-2.bin}`. One updater has
writable config/data; P2 sees only the selected data subtree read-only on the
same owner. Include a file initially denied to UID 65534, then change its mode
to permit access, and reverse the change for a fresh open.

**Supported behavior and oracle:** Prime a negative lookup, create a file, then
replace an existing file through a synchronized temporary file and same-volume
rename. Set explicit timestamps, rename a directory, and delete a file. At each
verified completion boundary, compare directory entries, hashes, size, mode and
timestamps with the external expected revision. Fresh lookup must find a newly
created name and stop finding a removed one. Recheck permissions with the actual
caller credentials after each change; already-open descriptors are not promised
retroactive revocation. Keep the updater's config mutation distinct from file
data changes; this fixture supplies no atomic transaction between them.

**Explicit rejection or limitation:** Cross-owner current subtree access is
rejected under the baseline. Filesystem notifications are not established by a
scan-based pass: record them as **not tested** unless a separate approved profile
checks the required notification API through the full access path. Do not claim
Syncthing synchronization, conflict resolution or database recovery from these
file-operation probes.

## P4: Aliased writable and read-only video claims

**Source:** S5. Two claim names are two views of one data identity.

**Fixture:** One canonical `R/video` directory containing `clip.bin`, published
through distinct `video-rw` and `video-ro` claim identities to the same owner.
Use separate mount paths in one client, matching the selected Immich shape.
No actual video decoder, Immich server, database or production clip is required.

**Supported behavior and oracle:** Write and sync via RW, then reopen via RO
and compare bytes and metadata. Repeat after same-volume replacement and a
permission change. Warm both views first so this exercises alias cache
freshness. The canonical identity, ownership epoch, replication history and
data accounting remain shared; an alias must not allocate another independent
replica history. Verify every RO mutation fails without affecting RW. Log the
canonical identity behind each view; do not infer alias correctness solely from
matching contents or assume identical device/inode numbers across transports.

**Explicit rejection:** Assigning separate owners or replication identities to
the two aliases must fail before publication. If alias publication is not
implemented, reject the complete alias fixture explicitly rather than silently
copying data into two volumes. Rename within the canonical volume is covered;
rename across separately mounted alias paths may return `EXDEV` and is not
required to work. This fixture does not establish cross-mount advisory locking.

## P5: Parent/child namespace and relative symlink access

**Sources:** S6 with S4. A parent view reaches a child source and a sibling
output through one layout; separate names or nested paths do not establish
independent ownership boundaries.

**Fixture:** A synthetic `R/tree` contains
`syncthing/data/Camera/Irregular Ninja/photo.bin`,
`shuriken.sh/incoming/pictures -> ../../syncthing/data/Camera/Irregular Ninja`,
and `shuriken.sh/site/dist/index.bin`. Expose the parent view to a generator-like
client and the child data view to an updater on the same owner. The fixture is
entirely synthetic; it never mounts the real parent tree. Use a synthetic
identity authorized for the planned traversal and a second denied identity.

**Supported behavior and oracle:** For a supported parent/child policy, resolve
the relative symlink, read the same committed source revision through parent
and child views, then update it through the child and verify a fresh parent
read. Stage output in a regular sibling directory and rename it to an absent
`dist` name after moving the old `dist` aside. Each rename must be atomic within
the canonical namespace; this two-rename sequence has a gap and is not an atomic
directory exchange. Check traversal permissions with both identities. A parent
view must not bypass child authorization or create a second mutation path around
HomelabFS; symbolic links confer no extra rights.

**Explicit rejection:** `9q3` must either support these overlapping views as one
coherently authorized ownership domain or reject the parent/child publication.
Reject independently owned overlapping roots, cross-owner symlink access and
any overlap that cannot enforce the selected credentials and visibility rules.
Do not claim a general federated parent namespace. A narrower fixture containing
only independent source/output mounts does not pass this relative-layout case;
record the parent pattern as unsupported if that is the chosen initial policy.

## Evidence and handoff

For a future approved run, record the fixture IDs and source revision, run-owned
manifest, actual canonical identities and view mappings, owner/epoch, caller
IDs, platform/kernel/daemon/transport versions and cache settings. Preserve the
external expected revision/hash ledger and operation results, sync boundary,
publication decisions, denied-operation errors, observed contents/metadata,
resource high-water marks and cleanup or retained-resource inventory.

| Pattern | Positive fixture | Required rejection or explicit limitation |
|---|---|---|
| P1 (S1/S2) | Same-owner reader sees synchronized shared music changes | Cross-owner reader/writer or passive-reader publication rejected |
| P2 (S3/S1/S4) | Complete same-owner multi-volume mount set; private writes | Split-owner and missing-subdirectory publication rejected; cross-volume rename `EXDEV` |
| P3 (S4/S3) | Fresh subtree contents, names, timestamps and permissions after updates | Cross-owner current access rejected; notifications and application recovery unproven |
| P4 (S5) | RW/RO aliases use one canonical data/history identity | Separate alias owners/identities rejected, or alias support explicitly rejected in full |
| P5 (S6/S4) | Coherent parent/child view, relative traversal and local output rename | Unsupported overlaps rejected explicitly; no cross-owner parent federation |

`9q3` must turn each conditional alias/overlap case into a specified supported
mapping or explicit rejection, including error/publication behavior. `gv3` then
maps only those decisions to fresh canary roots and identities, with `fv3`'s
resource budget. This document creates no volumes or replication groups and
does not authorize installation or execution. A later run must distinguish
**passed**, **failed**, **unsupported/rejected as specified** and **not run** for
every case and access path. Native FUSE evidence and the separate Rocky/9p
deployment evidence stay separate; 9p is not part of the native three-guest
integration matrix. Failed-resource cleanup follows the approved manifest and
preserves useful diagnostics; it never expands into production cleanup.
