# Real retained-9p base gate preparation

These tools prepare `zq3` against the actual controlled daemon and admitted full
custom kernel/exporter. They do not reuse the loopback PoC. Current local drafts
have **no deployed acceptance result**. No installation, kernel boot, exporter,
VM, mount, device or live workload action is implied by their presence.

**The old xz3 candidate is quarantined.** Its CGO-disabled FreeBSD observer
returns Unsupported after exporter launch, so it cannot reach native READY.
The controller refuses its exact manifest before any SSH. See
[`quarantine.json`](quarantine.json). Tasks r24/s24 and reopened xz3 must provide
qualified corrected artifacts. Existing offline build proofs remain preserved;
neither a successful old metadata admission nor a different digest establishes
native eligibility. These preparation commands never grant deployment eligibility.

**The controller pins exactly one candidate:** the 2026-11-08 xz3 rebuild,
manifest `3f770a884978d3a776663a74083e733baf38665b8cba119022dd5432fb3296f1`
(source `e0cca0c`, CGO daemon `53598737…`; see its
[evidence](../../integration/artifacts/evidence/xz3-20261108/README.md)). Any
other digest refuses before reading inputs or contacting the lab. This pin is a
dispatch precondition only; installed/booted/native acceptance remains zq3.

`guest.py` runs real Linux syscalls only after matching the actual boot/provider,
complete two-domain inventory, installed manifest digest, SELinux enforcing and
both exact `9P2000.L.hfs1.4,cache=mmap,msize=65536` mount IDs. It retains directory
descriptors and verifies independent `st_dev` domains. Every operation stays in
a newly exclusively created case directory. JSONL invocation/reply/errno evidence
leaves through stdout; a controller must preserve it outside the tested volume.

**Successful cases retain their payload** until host correlation is captured.
(The earlier probe deleted it immediately, so the host could never correlate
journal/source state against the files the guest had just written.) The case
records the exact payload inventory (names, types, inodes, ownership, modes,
sizes, links, leaf listing) and the SHA256 of a fresh 256-bit release token in
`/var/tmp/hfs-zq3-RUN/release-CASE.json`: exclusive, `0600`, fsynced, in the
root-private local run directory that holds both mount points and is checked to
be on neither tested device. The token itself appears only in the controller's
private stdout log. A separate `homelabfs-retained9p-release/1` request passes
the same full admission, then must present that token, the unchanged inventory
digest and the host correlation digest. Only then is the record consumed
(renamed to `released-CASE.json`, so a replay refuses) and the exact known
entries unlinked; the journal is preserved. A wrong token, changed inventory,
changed boot/provider, insecure run directory or existing record refuses and
preserves everything. Failed cases preserve their files and write no record.
One release before the next case keeps the full 4MiB logical canary free of
earlier payloads. The inventory covers names, types, inodes, owners, modes,
sizes and links; file **contents are not hashed**, so it detects replaced or
resized entries, not same-size in-place rewrites.

**Mid-release failure.** The record is consumed before the first unlink. If an
unlink/rmdir or the deadline then fails, part of the payload can remain while
`released-CASE.json` already exists; the guest terminal reports failure and the
controller keeps the case at `release-dispatching-uncertain` with `error` set.
Recovery is manual and never a second release: root inspects the leaf against
the inventory stored in `released-CASE.json`, preserves the remaining entries
and evidence, and continues only with a new run ID (the canary may still hold
the leftover bytes, so that volume/run is not reused).

**Release tokens in evidence.** Each case's `stdout.log` contains its
`release_token`. It is single-use: after a successful release the guest record
is consumed and the token is spent. A token of a case that was never released
can still release that payload. Before copying controller evidence into
`docs/lab/results`, redact `release_token` values or record next to them that
they are spent (and which ones are still live). Keep `state.json`'s
`stdout_sha256` with the unredacted original, which stays private.

Implemented probe cases are credentials, sync, metadata, namespace refusal and
full mapped pressure. The namespace refusal case exercises an actual new user
namespace, and requires the operation to refuse. Failure to create a namespace
is not a pass. This establishes a refusal boundary only: **it does not satisfy
the original namespace/pod compatibility requirement**. The C6 implementation
currently supports the initial user namespace and its numeric idmap. Ordinary
mount/PID/network namespaces may share that user namespace; they need separate
actual fixtures. The accepted lock concession is only same-guest/same-mount
advisory locks, including bind-shared pods; use the existing actual
[`guest-lock-probe.py`](../guest-lock-probe.py), never independent-mount shared
lock assertions. Each guest case has at most60 seconds,256 recorded operations,
12MiB issued writes,128 descriptors and one concurrently active credential child.
The full mapped case dirties all4MiB; SIGBUS is a failure.

The guest terminal record always says `acceptance:false` and requires host
correlation. Syscall success cannot certify covering journal barriers, actual
source ownership, funded drain progress, PTE/TLB exclusion or recovery. Missing
correlations, pod cases or other required matrix entries keep `zq3` incomplete.

## Controller evidence and uncertain dispatch

`check-retained9p.sh check --plan ABS` validates an exact syscall batch and emits
its byte digest without contacting the lab. The batch then runs one case at a
time against already installed/admitted mounts:

1. `probe --plan ABS --sha256 SHA --output FRESH_ABS` creates the run and
   dispatches the first case, ending at `payload-retained-awaiting-correlation`.
2. Root captures host correlation for that case while its payload is present and
   writes a `homelabfs-retained9p-host-correlation/1` record: closed keys
   `version, run_id, case, case_stdout_sha256, inventory_sha256, host_observe,
   acceptance:false`, bound to the case's retained stdout and inventory digests;
   `host_observe` is the nonempty raw host evidence (for example the lifecycle
   endpoint OBSERVE reply). The controller binds, but does not interpret, it.
3. `release --output ABS --sha256 SHA --correlation ABS --correlation-sha256 SHA`
   checks that binding and the retained stdout digest, persists the record and
   dispatches the guest release.
4. `next --output ABS --sha256 SHA` dispatches the following case only after the
   previous payload is released. `syscall_batch_success` needs all five released.

`status --output ABS` reads retained state. Refusals before a dispatch change no
state; a failed or uncertain dispatch records `error`, after which every later
command refuses (inspect, never replay). Commands refuse 3600 seconds after the
run started, leaving any retained payload for inspection. The plan's `seconds`
(60-600) caps each single SSH dispatch: its local deadline is
`min(case seconds + 15, plan seconds)`. Each reopen verifies
the owned lock, exact plan digest and every staged input snapshot. These commands
provide no VM lifecycle, package installation, mounting, fencing or normal
publication; the host correlator that produces step 2's record does not exist yet.
Root must complete the separately approved critical setup before a probe batch.
The batch's SSH executable, host/guest public pin files and probe bytes are pinned
and checked before dispatch. The guest connection goes through f3 with strict
explicit host/guest pin policy and no agent forwarding/control master/key update.

**Targets and host-key pins (zq3 G8).** The plan must name exactly f3
`10.20.0.133` (alias `f3`, user `paul`) and Rocky `10.20.0.123` (alias `rocky`,
user `root`), the IQ-verified primaries after the LAN migration. The
pre-migration `192.168.1.133`/`192.168.1.123` stay configured on both machines
as aliases, but a plan naming one refuses with an explicit reason: the pin
lines, the observer key's `from=` and the evidence all name one route, so one
plan digest never means two network paths. The plan's `known_hosts` files must
be byte copies of [`f3-host-known_hosts`](../../../docs/lab/f3-host-known_hosts)
and [`rocky-known_hosts`](../../../docs/lab/rocky-known_hosts) (or comment-only
variants): exactly one `ALIAS,ADDRESS ssh-ed25519 KEY` line whose fingerprint
equals the reviewed one (`SHA256:VZLF…` f3, `SHA256:UCoi…` Rocky). The alias
serves this controller's `HostKeyAlias`, the address the runtime observer's
plain `root@10.20.0.123` lookup. Those keys come read-only from Paul's existing
known_hosts, cross-checked against the earlier lab record; nothing was scanned.
`docs/lab/f3-known_hosts` remains the hfs-test guest pin file.

The controller reads the plan once: those exact raw bytes supply parsing,
validation, the reported digest and the durable `plan.json`. Verified SSH,
public-pin and probe bytes are retained from their checked reads. Execution uses
the retained probe bytes and fresh nonwritable snapshots under the private run's
`inputs` directory, including the nested ProxyCommand executable and host pins.
Original input path replacements cannot change a dispatched batch. Snapshot
storage is bounded to16MiB SSH +512KiB probe +2×64KiB public pins. Existing output
ancestors must exclude untrusted replacement; only root-owned sticky `/tmp` and
`/var/tmp` may be writable temporary ancestors. Administrative modification of
the owned private run directory during execution is outside this contract.

Output paths must be canonical absolute ASCII paths containing only
`[A-Za-z0-9_./-]`. Percent tokens, dollar/environment syntax, tildes, whitespace,
quotes, backslashes and all other characters refuse before staging or process
creation. This restriction prevents OpenSSH option/token expansion from routing
the outer or nested command to a different snapshot path. Ordinary hyphens,
underscores and dots are allowed; parent ownership checks still apply.

The controller fsyncs the plan and per-case dispatch intent before starting SSH,
stores separate bounded stdout/stderr outside the lab, and requires a real final
guest terminal plus a zero transport exit. A transport timeout kills only the
local SSH child and retains the dispatch as uncertain; it never kills the VM or
infers remote quiescence from SSH exit. It refuses output-directory reuse and
does not replay a dispatch automatically. Stream overflow makes required evidence
incomplete, rather than silently truncating a receipt. The report separately
states syscall-batch success and `zq3_acceptance_complete:false`, with explicit
remaining original gates. A complete syscall batch is useful evidence, never a
substitute for host correlation or the full gate.

The new output directory is synced together with its parent before dispatch.
Each case directory and its empty log entries are durable before child startup;
final log data and the case directory are synced before recording completed
state. A parent/log/case durability failure prevents dispatch or retains its
uncertain intent, rather than publishing durable completion without reachable
evidence. Ordering/failure regressions exercise these local filesystem boundaries.

Each canonical request is capped at16KiB including its newline, matching the
guest wire bound. The controller rejects oversize input before process creation
and sends stdin nonblocking under the same per-case selector deadline while
draining stdout/stderr. A stalled SSH handshake or child that never reads cannot
hold the controller in a synchronous pipe write beyond that deadline. Local
regressions use an actual4KiB pipe/non-reading child and pre-spawn oversize denial;
these exercise controller transport safety only, not guest filesystem acceptance.

## Run-only observer key (zq3 G6)

[`observer_key.py`](observer_key.py) runs **on Rocky as root** inside the
approved window (A6). It generates no key and opens no connection; the run
generates the key on f3 and transfers only its public half. Python 3.9
compatible, so it can run from stdin as `/usr/bin/python3.9 -I -B - ACTION …`;
Rocky cannot hash stdin, so the filled plan pins this file's SHA256 and the
operator checks it before each invocation.

- `stage`: checks the pinned interpreter (root-owned regular file, no symlink,
  SHA256), `observer.py` against `--observer-sha256` and the installed probe
  against the built-in `c824b183…` (the bytes `hfs-controlled-runtime` embeds
  from `internal/controlledruntime/probe/installed.py`). Creates
  `/var/tmp/hfs-zq3-RUN` (`0700`, empty `m0`, `m1`) if absent and writes
  `observer.py` and `installed-probe.py` exclusively, root-owned `0400`.
- `enrol`: re-verifies those, accepts only one `ssh-ed25519 KEY
  hfs-zq3-RUN-observer` public line, requires the current `authorized_keys`
  SHA256 to equal `--expect-sha256` (the Phase 1 value) and refuses if the key
  or run comment is already present. Writes the receipt
  `observer-key.json` first, then appends exactly one line
  `restrict,from="10.20.0.133",command="PY -I RUN/observer.py --probe
  RUN/installed-probe.py --probe-sha256 … --python PY --python-sha256 …"
  ssh-ed25519 KEY hfs-zq3-RUN-observer` and verifies after = SHA256(before + line).
- `remove --expect-sha256 H`: H must be the current file hash. With the
  receipt's post-enrol hash it removes the exact line and must restore the
  pre-run bytes. With another explicitly given current hash (unrelated keys
  changed meanwhile) it removes only that line, keeps everything else and
  reports `restored_pre_run: false`. A duplicated line refuses. If an enrol was
  interrupted before its line landed and the file is still at the receipt's
  pre-run hash, it only retires the receipt; with the line absent and any other
  hash it refuses, and root retires the receipt manually after inspection. The receipt
  becomes `observer-key-removed.json`, so a repeat refuses.
- `status`: read-only hash, line count and enrolled-line count.

Every change replaces `authorized_keys` atomically (exclusive temporary file,
original owner, mode and SELinux label, fsync, rename, directory fsync) after
re-reading it, so a crash leaves a complete old or new file. Other tools take
no lock: an edit landing between that last re-read and the rename (an append
to the old file or another tool's rename) is **silently lost and not
detected**; the post-rename hash check only catches writes after our rename.
Nothing else may edit root's `authorized_keys` during the window. A kill
between writing the temporary file and the rename leaves a stale
`.authorized_keys.hfs-observer-next` beside it; `status` reports it
(`stale_pending`), and the next replacement refuses until root inspects and
deletes it. One flock per run directory serialises the tool.
The interpreter pin covers the executable file only, not the shared libraries
it loads (the same limit as `observer.py`'s own `fexecve` pin).

## Ordinary pod namespace fixture (zq3 G9, T7, decision A8)

[`pod_fixture.py`](pod_fixture.py) runs **on Rocky as root** inside the
approved window, after Phase 8 mounted `m0`. It implements the
[ordinary pod namespace profile](../../../docs/lab/Retained-9p-fixtures.md#ordinary-pod-namespace-profile).
The container-side code is [`pod_workload.py`](pod_workload.py). The driver
pins it by `WORKLOAD_SHA256`, so the staged `pod_fixture.py` hash pins both.
Stage both files root-owned in one directory: the driver reads the workload
from its own directory and refuses other bytes.
Both files are Python 3.9 compatible. Nothing here pulls an image, installs a
package, mounts, unmounts, kills or forces anything.

**Runtime choice (A8, agent judgement under Paul's 2026-11-08 delegation).**
Use the podman `5.8.2-8.el9_8` and crun `1.27-1.el9_7` already installed on
Rocky. Both NEVRAs are pinned in the script. The SHA256 of podman, crun,
conmon and `/usr/share/containers/seccomp.json` comes from Phase 1 in the
request. Containers are rootful, use `--userns=host` (`init_user_ns`), VFS
storage in fresh run-owned roots, and run-owned `containers.conf`,
`storage.conf` and an empty `registries.conf`. The rootfs is **not an image**.
It is a directory built from Rocky's own installed files and started with
`--rootfs --read-only`. Why this choice:

- `buildah` and `skopeo` are absent. Any registry pull would add a network
  dependency and bytes nobody reviewed.
- Every rootfs file is attested against the RPM database's SHA256 digest of
  the installed, signed package.
- The exact tree can be re-verified in place before and after the run.

`runc` is not installed and is not needed.

- `manifest` (read-only, usable for Phase 1 facts) runs the workload's own
  `closure` mode under the pinned `/usr/bin/python3.9 -I -B -S` with
  `LC_ALL=C`. That mode exercises the workload's code paths and lists every
  module file and `/proc/self/maps` file the interpreter loaded. The manifest
  then adds the usrmerge links (`/lib64` for PT_INTERP) and versioned SONAME
  links. It requires every file to be owned by a package whose
  `FILEDIGESTALGO` is SHA256, with mode, digest and link target equal to the
  RPM database row. Fixture-made entries are labelled `origin: fixture`: the
  directories and mount points, a minimal numeric `/etc/passwd`/`group`, empty
  `/etc/hosts`/`hostname`/`resolv.conf` and the workload. The command prints
  the canonical manifest and its SHA256. With `TZ=UTC` and `LC_ALL=C` the same
  install always gives the same digest. The workload deliberately avoids
  `hashlib`/`ssl`, so OpenSSL and its configuration files stay out of the
  container.
- `build --run-id R --expect-manifest-sha256 H` recomputes the manifest and
  refuses unless it equals the pinned `H`, then writes `RUN/pod/rootfs`
  exclusively. Files are root-owned and labelled
  `system_u:object_r:container_file_t:s0`, the label podman documents for
  `--rootfs`. The command also writes `RUN/pod/rootfs-manifest.json` and
  re-verifies the whole tree.
- `run --request ABS` takes a closed request:
  `{version, run_id, fixture_sha256, rootfs_manifest_sha256, runtime_sha256{podman,crun,conmon,seccomp}, m0{tag:"hfs0", mount_id}}`.
  It admits only if all of these hold:
  - it runs as root on Linux;
  - SELinux is enforcing;
  - the driver is in guest init's mount/PID/net/UTS/IPC/user/cgroup namespaces;
  - init has the identity uid/gid map;
  - `m0` has the exact controlled 9p tuple, as `guest.py` admits it;
  - the run directory is root-private and not on `m0`;
  - the runtime hashes and NEVRAs and the rootfs equal their pins;
  - the audit log is readable.

  Then it starts `w0` (new mount, PID, net (`--network=none`, loopback only),
  UTS and IPC namespaces, private cgroupns, hostname `hfs-zq3-RUN-pod`). It
  starts `w1` with its own mount namespace, joined to w0's PID, net, UTS and
  IPC namespaces, like a pod. Both bind-mount the same `RUN/m0` at `/hfs/m0`
  with `rprivate` propagation. Both use `--cap-drop=all` plus only
  CHOWN/FOWNER/SETGID/SETUID, `no-new-privileges`, the pinned seccomp profile
  and MCS level `s0:c731,c914`. Limits: 256 MiB memory, no swap, 64 PIDs.
  Each workload prints its identity and then waits on a stdin barrier, so the
  driver inspects `/proc/PID/{ns,status,uid_map,gid_map,attr/current,cgroup}`
  and the effective cgroup limits **from outside** while it is alive.

The workloads then run:

- **Numeric permission matrix in `w0`:** two UIDs; supplementary group only;
  the same UID with a full 1024-entry group vector, allowed only through its
  final entry, and denied after removing just that entry; changing groups
  inside one process under `setfsuid(41001)`/`setfsgid(42001)`.
- **Lock matrix across `w0`/`w1` through their separate binds of one mount:**
  - same device/inode;
  - nonblocking POSIX and flock conflicts;
  - `F_GETLK` reports w0's PID in the shared PID namespace;
  - `/proc/locks` blocked-waiter evidence;
  - acquisition only after release;
  - w1's locks released by its exit.

They coordinate over an abstract UNIX socket. Abstract sockets are per network
namespace, so reaching it shows w1 really joined w0's netns.

**Judgement.** Self-reported and outside namespace inodes must agree. Mount,
PID, net, UTS and IPC must differ from the guest's. The user namespace must
equal the guest's initial one, with the `0 0 4294967295` maps. w1 must share
w0's PID, net, UTS and IPC namespaces and have its own mount namespace.
Container root must be `0/0` with no group other than 0, the exact capability
mask in effective, permitted and bounding sets, empty ambient, NNP=1 and
seccomp=2. The label must be `container_t:s0:c731,c914`. The only link may be
`lo`. `/hfs/m0` must be exactly one 9p row with the `m0` tag and device. The
cgroup limits must equal the pins. Every matrix and lock outcome must match.
There may be no AVC denial for the pod in the audit delta. A rotated or
oversized delta is *incomplete*. SELinux must still be enforcing afterwards,
and pinned rootfs entries must be unchanged; the runtime may add only empty
mount points, which are reported.

**Results.** A pass removes the two exited containers with plain `podman rm`
and the exact five payload files and leaf on `m0`, after checking the listing.
A cleanup failure is reported apart from the T7 result. Any failure, timeout or
malformed report preserves the containers, payload and evidence. A timeout is
recorded as `uncertain`, and nothing is killed. Evidence goes to
`RUN/pod/evidence/`: `request.json`, both workloads' stdout and stderr,
`verdict.json` and `cleanup.json`. The pod directory is single-use: a second
`run` refuses. The verdict always says `acceptance: false`. A T7 pass is one
gate of the matrix, not zq3 acceptance.

**Not known before the run (and recorded rather than worked around).**

- Whether `container_t` may use the label Rocky's policy gives the
  candidate's 9p mount. The fixture records `m0_label` and never uses `:z`/`:Z`
  (which would write xattrs into the tested volume) or `label=disable`. A
  denial fails T7 with its AVC lines. Fixing it (for example a `context=`
  option in Phase 8's mount, or a boolean) is a decision for Paul.
- Whether podman relabels or adds files in a `--rootfs` tree. A relabel shows
  as changed pinned entries and fails T7. Added empty mount points are
  tolerated and reported.
- The container root's exact supplementary group list, `[]` or `[0]`. Both are
  accepted; anything else fails.

**Local tests** (`test_pod_fixture.py`) simulate rpm, podman inspect/rm,
`/proc`, `/sys` and labels explicitly with a FakeHost and a fake installed-file
mirror. They drive the whole `run` through real pipes with fake container
processes. Negative cases include:

- a namespace not separated;
- a foreign user namespace or map;
- SELinux not enforcing, a wrong context or AVC denials;
- wrong UID/GID, groups, capabilities or matrix outcomes;
- a missing shared bind or different inodes;
- an unshared pod namespace, extra links, missing limits or rootfs changes;
- unattested, unowned, non-SHA256 or out-of-tree closure files;
- admission refusals;
- runtime pin mismatches;
- request shape errors;
- a timeout that must not kill;
- single-use evidence.

The lock protocol runs for real between two local processes on tmpfs, with
real kernel locks and `/proc/locks` waiters. It uses tmpfs, not `$TMPDIR`,
because on btrfs `st_dev` differs from the superblock device that `/proc/locks`
prints. Linux 9p reports one device in both places. An optional test reads the
workstation's real RPM database read-only, builds the rootfs and starts the
interpreter and all workload imports from it alone in an unprivileged
user-namespace chroot. That checks closure completeness on Fedora, **not on
Rocky**. No test runs podman, crun, SELinux policy or 9p.

## External staged signer

`cmd/hfs-retained9p-operator` runs on the external controller. It never executes
SSH, installs a sysfs artifact, initializes a volume or starts the runtime. Its
signatures are inert administrative consent; actual runtime/Facts/guest checks
remain mandatory. The xz3 candidate's operator key stays outside repository,
runtime and guest. Do not read or use that actual private key during preparation.
Test with fresh throwaway keys only.

The two commands are:

```
hfs-retained9p-operator enroll --request /private/enroll-request.json --output /private/new-enrollment
hfs-retained9p-operator bind --request /private/bind-request.json --output /private/new-binding
```

Requests and proposal/key files must be clean absolute, bounded, singly linked,
private regular files owned by the calling UID. Keys accept one unencrypted
Ed25519 PKCS#8 PEM, or exact lowercase hex seed/private key without a newline.
No key bytes appear in output. Fresh0700 output directories contain0600 files;
failure preserves partial outputs and reuse refuses. Original proposal struct
bytes and domain order remain exact. Only an empty GuestBoot may be filled from
the actual observed boot. Signed provision validation checks original keys,
complete aggregate and per-domain signatures, inventory, protocol and budgets.

`EnrollmentRequest` uses exported Go JSON field names:

```
{
  "Proposal": "/private/copied-actual-proposal.json",
  "OperatorKey": "/private/operator.pem",
  "BootID": "actual-observed-guest-boot-UUID",
  "ProviderNonce": "actual-observed-provider-UUID",
  "Keys": [
    {"Session":"C0-UUID","Host":"/private/host0.pem","Guest":"/private/guest0.pem"},
    {"Session":"C1-UUID","Host":"/private/host1.pem","Guest":"/private/guest1.pem"}
  ],
  "Mounts": ["two exact controlledruntime.GuestArtifactMount records"],
  "Budget": "exact controlledruntime.GuestArtifactBudget",
  "Observer": "exact controlledruntime.GuestObserver"
}
```

The quoted record descriptions above are schema explanations, not runnable
request values. Populate actual exported structs and validate locally before
an approved bootstrap. The signer emits `enrollment.json` (the real
RuntimeEnrollment handoff) and `provision.json` (one complete canonical sysfs
write). Install the guest provision first, then atomically transfer enrollment
to the private host EnrollmentFile. At `AWAITING_GUEST_BIND`, the bind request
names that exact provision, original group ID, actual emitted controller and
same ordered key files. It emits `bind.json` for one complete guest sysfs write.
Neither file is a native readiness receipt.

## Required work before an executable exact plan

Read [the concrete gap and installation assessment](../../../docs/lab/Real-daemon-9p-base-gate.md).
The immutable195-file xz3 candidate remains unchanged. New operational helpers
need separately pinned source, executable and verification records. If the
runtime needs new control APIs, rebuild/reseal only under a separate explicit
artifact revision and retain the existing candidate/evidence unchanged.
