# Local daemon configuration and lifecycle

Task `tq3` implements `cmd/homelabfs` and `internal/daemoncli`. It initializes and
inspects local disposable volumes and runs a restricted FUSE mount in the
foreground. It is not normal distributed serving, emergency mode, a replication
implementation or production publication. No synthetic partner ACK is supplied.
The [FUSE adapter](FUSE-adapter.md) capability restrictions remain in force.

## Configuration version 1

Use a private UTF-8 JSON file, at most 64 KiB, with these required fields and
the optional `enable_test_hooks` boolean (default `false`):

```json
{
  "format_version": 1,
  "node_id": "11111111-1111-4111-8111-111111111111",
  "volume_id": "22222222-2222-4222-8222-222222222222",
  "control_dir": "/disposable/homelabfs/control",
  "volume_dir": "/disposable/homelabfs/volume",
  "mountpoint": "/disposable/homelabfs/mount"
}
```

The UUIDs above are examples. `node_id` must be the UUID returned by `node-init`;
choose a fresh, nonzero canonical lowercase UUID for each new `volume_id`.
All paths must be absolute and clean, have existing real directory parents, and
contain no symlink components. The three configured paths must be disjoint,
including ancestor/descendant relationships. Configuration and identity files
must be regular, mode 0600, singly linked and owned by the daemon's effective UID.
Field names are exact and case-sensitive, including in node/volume identity files.
Unknown/duplicate fields, trailing JSON, malformed UTF-8, unsupported versions,
invalid UUID spelling and identity mismatches are errors. Values are read once
per invocation; no hot reload exists. `enable_test_hooks:true` explicitly enables
the bounded administrative journal-failure toggle; see [local administration](Administration.md).
Static normal-node membership and the durable node-lifetime API use a separate
[static cluster configuration](Static-cluster.md); they are not wired into this
disposable CLI profile.

Control, volume and mount roots are mode 0700 and owned by that UID. The paths,
their ancestors and contents are administratively exclusive, as required by the
storage/service admission contract. They must not be concurrently replaced,
edited, populated with external mounts or exposed to workloads. These checks
are not a defense against a hostile administrator changing the backing tree.
Workloads must never receive the private source, replay or reservation paths.
The current initializer creates ordinary directories; ZFS dataset provisioning,
mounting, capacity admission and snapshots remain administrator responsibilities.
Creation does not import any existing files or initialize an existing dataset root.

## Commands

Build with `go build -o homelabfs ./cmd/homelabfs`. `homelabfs -version` prints
the application version. Each creation command requires a **new** root; its parent
must already exist. For an explicitly authorized disposable run:

```sh
./homelabfs node-init --control /disposable/homelabfs/control
```

This generates a random node UUID and durably writes `control/node.json` with
`format_version` and `node_id`. Record the returned UUID in your configuration,
set a fresh `volume_id`, and create the empty private mountpoint separately.
Keep the configuration file mode 0600. Then:

```sh
./homelabfs volume-init --config /disposable/config.json --disposable-validation
./homelabfs inspect --config /disposable/config.json
./homelabfs status --config /disposable/config.json # while foreground serving
./homelabfs mount --config /disposable/config.json --disposable-validation --self-client
```

`serve` is an alias for `mount`. Both run in the foreground. They install the
service's built-in Unix permission policy and the matching named disposable
profile. Without both explicit validation flags, serving fails closed before
touching persistent recovery state: the real normal `Protocol`, verified native
credential/ancestor admission, caching and mmap publication gates remain open.
There is no raw-backing fallback, implicit local-only normal mode, `allow_other`,
daemonization, shell command client, PID allowlist or account lookup provider.

The self-client profile admits only this still-running executable, whose
credentials/groups remain fixed. Linux verifies request TIDs inside
`/proc/self/task` against the captured process and filesystem IDs/groups. Foreign
processes, including other processes with the same UID, are rejected. On FreeBSD,
the fixture requires the exact own process PID, UID/GID and captured own groups.
The pinned [FreeBSD request construction](https://raw.githubusercontent.com/freebsd/freebsd-src/96841ea08dcf/sys/fs/fuse/fuse_ipc.c)
passes `td->td_proc->p_pid`, `cr_uid` and `cr_gid` into the header. No arbitrary
foreign process lookup authenticates clients. These deliberately closed fixtures
do not implement changing workload credentials, descriptor transfer or arbitrary
application admission. FreeBSD compilation is not native mount evidence.

`--self-test` additionally runs a bounded built-in client, creating a uniquely
named synthetic file, writing 35 bytes, syncing, reading, renaming and syncing
the root directory, then normally unmounting and exiting. The verified file
remains in the disposable logical volume for later inspection/replay. Each
volume admits at most 4 MiB of logical data; source replay, catalog, handles and
reservations retain the service's independent finite limits. Without
`--self-test`, the process waits for SIGINT/SIGTERM, normally unmounts, drains
adapter references and closes the service and roots. It never force/lazy unmounts
or removes resources after a failure. An uncertain native mount or failed normal unmount returns an error, blocks
workload admission and retains admin status, mount/backing resources and source
writer ownership for inspection until process exit. This also applies when
`daemoncli.Run` is embedded in a longer-lived process; no forced cleanup follows.

Before reporting readiness, mounting requires an empty private mountpoint and
successful native `CheckMount`: filesystem type and exact volume/generation/session
xattr are checked through the same opened directory. Standard output contains
one JSON readiness record with `profile`, `mountpoint`, canonical `volume_id`,
`generation`, `session` and `normal_publication:false`. Logs go to standard error
using `slog` JSON. Readiness describes this restricted connection only; it does
not certify normal workloads or authorize consumer/9p/Kubernetes publication.

`inspect` validates the configured node/volume manifests and private root layout
and prints the volume manifest. It is a metadata inspection, not journal replay,
source repair, current serving-authority evidence or a mount-identity check.

`status` uses the authenticated private Unix socket and an immutable service
snapshot. It checks administrative config/control identity without traversing the
active FUSE mountpoint or backing storage. It reports local state/wait reason,
source/apply positions, separate backing slots and unknown replica positions;
`normal_publication` remains false. Read/control authorization, bounded protocol,
audit behavior and default-disabled test hooks are specified in
[local administration](Administration.md).

## Persistent layout and restart

```text
control/node.json
control/admin-<volume_id>.sock
control/admin-<volume_id>.audit.jsonl
volume/volume.json
volume/source/segments/<volume>/<history-generation>/<configuration>/e.../owner/...
volume/reservations/allocation-<uuid>.hfr
volume/workspaces/<fresh-serving-generation>/data/
volume/workspaces/<fresh-serving-generation>/journal/
volume/workspaces/<fresh-serving-generation>/progress/
```

The immutable versioned `volume.json` binds node, volume, logical root, history
generation/configuration and explicitly labelled fixture peer/incarnation IDs.
The fixture group supplies required journal record shape only; it names no real
partners and grants no replication, fencing or emergency authority. Volume
initialization durably creates the source directory ancestry and a real
`VOLUME_INIT` record before publishing the manifest. Files are created exclusively
and their contents and parent directories are synced. Interrupted attempts remain
preserved; creation never replaces an existing root or retries it as a fresh volume.

A nonblocking exclusive file lock on the volume manifest prevents two compliant
daemons from opening the same source writer. It remains held through service
close. Each recovery creates a new UUID-named private workspace and replays the
verified genesis history. The logical root/allocation identities survive; old
serving handles and generations do not. All prior workspaces remain, including
failed attempts. Permanent allocation reservations live outside these fresh
workspaces and are rediscovered on every start. No cleanup/GC is implicit.

Startup refuses corrupt or torn source history and preserves the original
bytes; no repair, source reset, checkpoint selection or automatic truncation is
configured. The [checkpoint package](Checkpoints.md) is not integrated into this
startup path. **Never trim the CLI volume's genesis source stream.** Aggregate
disk capacity must account for all retained workspaces, source and reservations;
the 4 MiB logical limit is not a total disk-space limit. Node/volume identity is
local admission metadata, not cryptographic cluster authentication or a fence.

## Evidence and open gates

Focused tests cover configuration/identity refusals, exclusive initialization,
interrupted-root preservation, exclusive writer admission, persistent reservation
discovery, fresh generation replay and acknowledged bytes after abrupt process
loss. Torn history is refused without modification. These are local syscall and
process-crash results, not power-loss, replica or failover evidence.

The opt-in Linux command is
`GOMAXPROCS=2 HOMELABFS_CLI_INTEGRATION=1 go test ./internal/daemoncli -run '^TestLinuxCLIIntegration$' -v -count=1 -timeout 120s`.
Run it only with explicit authorization. It builds the real binary, initializes
new temporary roots, performs bounded self-client I/O, rejects an external client,
restarts, signals foreground serving with SIGTERM, and checks normal unmount.
Successful resources are removed; failures are preserved. On 2026-10-29 this
passed in 1.38 seconds on local Linux; a final run with an explicit post-shutdown
native non-FUSE statfs assertion passed in 0.98 seconds. Native FreeBSD/ZFS, mmap, 9p, arbitrary
credentials, replication/failover and real consumer publication remain separate
unverified gates. No lab, VM, production or host lifecycle operation ran here.


The embedded `internal/fuseadapter` API separately admits an explicit controlled
buffered disposable profile; see [FUSE adapter](FUSE-adapter.md#controlled-buffered-validation-xw3).
It does not add a CLI flag or broaden the self-client daemon profile. Helpers
are enrolled before publication, bound to the exact connection, and access only
the identity-verified mount. Direct I/O and normal-publication denial remain the
defaults. Root-only synthetic multiuser validation is separately opt-in and
unprivileged runs report a skip.
