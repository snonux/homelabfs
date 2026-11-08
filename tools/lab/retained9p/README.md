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
   `host_observe` is the nonempty raw host evidence. The controller binds, but
   does not interpret, it; [`correlate.py case`](#host-correlator-zq3-g7-g10-g11)
   produces it from two lifecycle OBSERVE replies.
3. `release --output ABS --sha256 SHA --correlation ABS --correlation-sha256 SHA`
   checks that binding and the retained stdout digest, persists the record and
   dispatches the guest release.
4. `next --output ABS --sha256 SHA` dispatches the following case only after the
   previous payload is released. `syscall_batch_success` needs all five released.
5. `lifecycle --output ABS --sha256 SHA --step STEP` (after all five releases)
   dispatches one zq3 T9-T11 guest step with the same intent journal: see
   [lifecycle steps](#guest-lifecycle-steps-zq3-g10-g11).

`status --output ABS` reads retained state. Refusals before a dispatch change no
state; a failed or uncertain dispatch records `error`, after which every later
command refuses (inspect, never replay). Commands refuse 3600 seconds after the
run started, leaving any retained payload for inspection. The plan's `seconds`
(60-600) caps each single SSH dispatch: its local deadline is
`min(case seconds + 15, plan seconds)`. Each reopen verifies
the owned lock, exact plan digest and every staged input snapshot. These commands
provide no VM lifecycle, package installation, mounting, fencing or normal
publication.
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

## Guest lifecycle steps (zq3 G10, G11)

After all five payloads are released, `controller.py lifecycle --step` sends
`homelabfs-retained9p-lifecycle-step/1` requests to the same pinned `guest.py`:
the plan's boot/provider/manifest/mount identity plus `step` and a 30 s alarm.
Steps run at most once, in the order below (skipping is allowed, going back
is not; the re-read needs the CALL directly before it). Each records every
errno as data in one `step-observed` event; the terminal's `syscall_success`
only says the step ran to completion. Nothing is retried or deleted. The
controller records each step's `completed_utc` (workstation clock).

**Gate order: T1-T7, T9, T10, T11, then T8.** T9 blocks m0's guest session
(below), so the T11 K04 probe uses m1, which must still be mounted and
unblocked when host loss happens; T8's normal unmount of m1 therefore comes
last. T8's guest-side assertions (admission refuses `m1`, the empty directory
stays empty) need no live runtime; if the unmount refuses or hangs after host
loss, T8 is recorded unrun and Phase 11 handles the mount. Host loss for T11 is
the runtime exit after a FENCED T10. If T10 is RESTARTED the runtime keeps
serving, this plan has no supported way to stop it with the guest still up,
and T11 is recorded unrun.

**Deadlines are never errno values.** The guest's SIGALRM handler raises
`Deadline`, which is not an `OSError`: an interrupted call is logged as a
`reply` with `timeout: true` and ends the step as failed, so the controller
keeps it uncertain (never a refusal, never a pass). An `OSError` without an
errno also ends the step instead of being recorded as EIO.

Step admission checks the full guest identity but reads the mount tuples from
mountinfo only, without opening or resolving anything inside a domain. The
step's own domain (m0 for T9/T10, m1 for T11) must be the exact admitted
mount; the other may be absent, which is recorded (the admitted provider
inventory must still list both sessions, otherwise the step refuses).

| Step | Gate | Guest action |
|---|---|---|
| `lost-reply-call` | T9 | One `mkdir m0/zq3-RUN-lost-reply` immediately after the host ARM of m0's session; nothing else touches a domain. |
| `lost-reply-reread` | T9 | `lstat` and a bounded root listing of the leaf, m1 first, then m0; no second mkdir. |
| `quiesce` | T10 | Open, `syncfs` and close each present root; write `3` to `/proc/sys/vm/drop_caches` (guest-wide: page cache releases view leases, dentry/inode eviction sends FORGET), settle 2 s, drop again. |
| `after-loss` | T11 | After runtime exit: `lstat` m1, list m1, exclusive create of `m1/zq3-RUN-after-loss`; a create that succeeds is kept as evidence. |

**What the guest kernel actually does with T9.** The patched guest
(`tools/integration/linux9p/patches/0003`) issues a GETATTR for the parent's
permission check and a LOOKUP before MKDIR, all as CALLs, and never trusts a
cached dentry (`hfs_d_revalidate` returns 0). The armed fixture drops the
*first* matching CALL response, so it can hit one of those instead of MKDIR;
background CALLs on m0's session after the ARM (deferred FORGET, PROCESS_EXIT
of an earlier probe process, a GETATTR) can take the drop too. After any
dropped response the guest marks the session blocked (`EHOSTDOWN` for later
ordinary CALLs) and has no userspace-visible QUERY; the m0 re-read then fails
and only m1 shows whether the effect committed. The C9 restart that follows is
then expected to refuse at the paired cut and FENCE (lifecycle.md). The
correlator therefore classifies instead of judging; see below.

**What the quiesce does not do.** It performs no explicit VIEW_ACQUIRE
refresh: it can only release views (page cache drop) and leave none
historical. On a blocked m0, opening the root and the evicted lookups' FORGET
CALLs fail with `EHOSTDOWN`, so m0's capabilities stay held; the host OBSERVE
gate then shows whether the C9 cut precondition holds anyway.

## Host correlator (zq3 G7, G10, G11)

[`correlate.py`](correlate.py) runs offline on the workstation. Its inputs are
the controller run directory (verified plan digest, state without `error`,
re-hashed guest logs) and files root copied off f3, each given with its
SHA256: raw `hfs-controlled-runtime --control SOCK --operation …` client output
and host snapshots. It writes new exclusive `0600` records, never overwrites,
and every record says `acceptance:false`. Binding problems refuse and write
nothing.

- `case --case NAME --baseline R --retained R --record NEW`: OBSERVE replies
  taken right before the case dispatch and while its payload is retained.
  Both must be `OBSERVED`, `complete`, `SERVING`, without publication, RESTART
  or lost-reply section; strictly increasing sequence; same runtime, cut and
  epoch (a stale or rotated cut refuses); one unfenced admission with the
  plan's boot ID, provider nonce and both sessions, unchanged domain phase/fence;
  unchanged journal/owner/generation/configuration, provider ledger
  (UUID/device/inode), view-controller ledger and transport controller/FUSE
  session; a non-decreasing transport high-water and a strictly advanced
  applied position. The guest's own admission event must match. From the
  second case on, the baseline must follow the previous case's bound retained
  OBSERVE in the same incarnation and show the release's applied effect. The
  record (`host_observe` holds both raw replies, a summary and limits) is what
  `controller.py release --correlation` binds.
- `lost-reply --before R --armed R --after R --record NEW` (T9): the ARM used
  the pre-arm OBSERVE's cut/epoch and m0's session; the post-CALL OBSERVE has
  the same binding and fixture. A `FIRED` exposure must be `UNCERTAIN`, `CALL`,
  above `armed_high`, with claim and hashes. The leaf state is decided by the
  unarmed m1 (m0 only if m1 is unmounted and m0 answered itself); m0's
  `EHOSTDOWN` is recorded as `effect_seen.armed_domain_blocked`. With whether
  the source advanced since the arm baseline this gives `classification`:
  `dropped-reply-effect-committed-and-visible`, `dropped-before-mutation` (the
  expected T9 shape: m1 ENOENT, m0 blocked, no source change),
  `fixture-not-fired`, `effect-undetermined` (no domain could answer) or
  `inconsistent`. None of these is a pass.
- `k04 --baseline S --before S --after S --record NEW` (T11): snapshots from
  [`host_snapshot.py`](host_snapshot.py) of one run-owned host path below
  `/var/tmp/hfs-validation-xz3/RUN/` (for example the runtime's FUSE mount
  point): `baseline` before runtime start, then `before` and `after` around the
  guest after-loss step, all three required, the same path and taken in that
  order (f3 clock only). `k04_refusal_observed` needs every guest attempt on m1
  to fail, the marker absent on the host, a raw directory (if the path is no
  longer a mount point) equal to the baseline, no host change during the
  attempt, and `probe.probe_reached_host`: m1 must not have shown the guest's
  own `EHOSTDOWN` in the T9 re-read, the T10 quiesce or the first after-loss
  attempt, otherwise the refusals were local and K04 is flagged vacuous. A dead
  FUSE mount still covering the path reports its errno instead of a listing.
  `host_snapshot.py` is read-only (lstat, at most 256 names through an
  `O_NOFOLLOW` descriptor) and runs on f3 as root with the pinned interpreter;
  its 10 s alarm refuses the whole snapshot (a hang is uncertain, never a dead
  mount's errno).
- `command --operation observe|arm-lost-reply|restart --observe R [--last R]
  --runtime-binary PATH --control SOCK`: prints, never runs, the exact client
  argv: runtime, cut and epoch from the latest OBSERVE and sequence = last
  reply's sequence + 1. ARM refuses unless the batch is released, no lifecycle
  step ran and nothing is armed. RESTART refuses unless the last step was
  `quiesce`, the OBSERVE is later than every OBSERVE bound by a case
  correlation, and it shows the C9 cut precondition: view-controller `Leases`
  and `Reservations` 0, no `Recall`, `Matches` true. OBSERVE carries no
  timestamp, so that it was taken after the quiesce is checkable only in part;
  the output repeats the quiesce's `completed_utc` the operator must follow.

Local tests: `test_lifecycle_steps.py` (step shapes, mountinfo-only admission,
real local syscalls for each step with the kernel's errno substituted,
deadlines that end a step instead of becoming an errno, per-step required
domain, controller ordering/no-replay) and `test_correlate.py` (every case
correlated and accepted by the real `release`, stale cut/epoch, rotated
runtime, wrong case, changed log, broken chain, lost-reply classifications
including the realistic blocked-m0 shape, binding refusals, K04 with real
snapshots of temporary directories including a vacuous blocked-m1 probe and
snapshot order/path binding, argv builder, snapshot deadlines).
These are local logic tests only, never 9p, runtime or lab evidence.

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

## Rocky test kernel (zq3 G5)

`rocky_kernel.py` installs, selects once and removes the candidate guest kernel
on Rocky, and generates `/etc/homelabfs/installed-admission-v1.json`. It
implements the [run plan](../../../docs/lab/zq3-run-plan.md)'s Phase 3, Phase 5
step 2 and "Boot selection and recovery" (U1-U3). **It has only been tested
locally against a simulated Rocky** (temporary root, emulated grubby, grubenv,
depmod, dracut, systemctl and GRUB's one-shot consumption at boot). Every
Rocky-side command still needs Paul's explicit A3 approval of the filled plan.

1. Workstation, Phase 0:
   `python3 -I -B rocky_kernel.py plan --package DIR --manifest-sha256 3f770a88… --run-id zq3-r1 --stock-default-kernel /boot/vmlinuz-<Phase 1 default> --output /private/new-plan.json`.
   It refuses any manifest other than the pinned candidate, checks every
   Linux file's size and SHA256 against the manifest and the installation
   proposal, requires `modules.order` to name exactly the manifest's modules,
   and records the GNU build IDs of `vmlinux` and every `.ko`. Targets are
   derived from the source names (`/boot/vmlinuz-R`, `/boot/System.map-R`,
   `/boot/config-R`, `/lib/modules/R/…`); the Rocky side recomputes them, so a
   plan cannot direct a write anywhere else.
2. Stage the script, the plan and the package's `linux/` files on Rocky (for
   example `/root/hfs-zq3-zq3-r1-kernel-stage/`). Every Rocky command is
   `/usr/bin/python3 -I -B rocky_kernel.py CMD --plan P --plan-sha256 S --state D`
   as root, with `D` a new path in a root-only parent (for example
   `/root/hfs-zq3-zq3-r1-kernel-state`).

| Command | Plan step | What it does and refuses |
|---|---|---|
| `install --stage DIR` | Phase 3 steps 1-4 | Needs exactly the plan's stock release running (any other installed kernel, such as ELRepo 7.0.3, refuses), the pinned default, empty `next_entry`, no existing targets or test-kernel entry, `/boot` headroom. Then it records `pre.json` (grubenv, default, raw `grubby --info=ALL`), copies the verified bytes (write beside, link, never clobber), runs `depmod`, `dracut --kver`, `grubby --add-kernel … --copy-default --title …`, and checks for exactly one new entry, all old entries unchanged, the default and `saved_entry` unchanged and `next_entry` still empty. |
| `select` | Phase 5 step 2 | Last Rocky action before the f3 stop; exactly the stock release. Rechecks installed bytes, initramfs and BLS entry, runs `grub2-reboot <id>`, verifies `next_entry` = id and the default unchanged. Any failure runs U1 at once. Runs once per state directory. |
| `cancel` | U1 | `grub2-editenv - unset next_entry` and verify. Allowed even with an uncertain or torn journal. Manual fallback if even this cannot run: `grub2-editenv - unset next_entry`, then `grub2-editenv - list` and `grubby --default-kernel`. |
| `remove` | U2 | Exactly the stock release, `next_entry` empty. Scans first and refuses, deleting nothing, if anything the plan or depmod did not create is present (including symlinks). Then `grubby --remove-kernel`, deletes the files (including `/boot/initramfs-Rkdump.img` if kdump built it in a test-kernel boot) and the admission manifest, recording each removed file's SHA256, and requires `grubby --info=ALL` to equal the pre-run listing byte for byte. |
| `check --expect test\|stock\|any` | U3 step 3, Phase 11 step 2, Phase 12 step 2 | Read-only: running release, boot ID, `next_entry` empty, `saved_entry` and default unchanged. Exit 1 on any problem. The result is journaled. |
| `reboot-to-stock` | U3 step 4 | Test kernel only, a passing `check --expect test` in this boot, and refuses once the admission manifest exists. That is its only marker: it cannot itself tell a U3 boot from the controlled boot (which ends with a poweroff). A Phase 8 boot that failed before admission leaves no manifest, and a manifest written by mistake in a U3 boot makes it refuse wrongly, so the operator must know which boot they are in. Runs `systemctl reboot`. A new boot ID resolves the dangling intent if the shutdown wins the race. |
| `admission` | Phase 8 step 0, before the enrollment is signed | Exactly the test kernel. See below. |
| `status`, `reconcile --seq N --note TEXT` | | Show the journal. Record root's inspected outcome of an uncertain intent. It counts as failed and is never repeated. |

**Why the admission manifest is generated in the controlled boot.**
`internal/controlledruntime/probe/installed.py` refuses unless the manifest's
modules are exactly the loaded `/proc/modules` set and the running
`/sys/kernel/notes` build ID matches. Which of the five planned modules load
(for example `x86_pkg_temp_thermal` under bhyve) is only known once the test
kernel runs. The generator therefore runs as root in the Phase 8 boot, after
`udevadm settle` (refused if the udev queue does not settle). It
refuses any loaded module the plan did not install, checks each loaded
module's installed hash and `.ko` build ID and its running sysfs build ID
against the plan, and checks the kernel image hash and the running kernel's
build ID (from `vmlinux`). It then repeats the boot/module observation, as
the probe does, and writes the closed
`homelabfs-installed-artifacts/1` manifest (canonical JSON, `0644`, written beside and linked
into place, never overwritten). Its SHA256 is the `Observer.ManifestHash` of the
enrollment request. A local test runs the actual `installed.py` probe against
the generated manifest in the simulated root.

Every mutating step writes a durable intent and a durable result to
`journal.jsonl` under an exclusive lock. An intent without a result (the
process was killed) blocks install, select, remove, reboot-to-stock and
admission until root inspects it and records the outcome with `reconcile`;
`cancel`, `check`, `status` and `reconcile` still run. A torn journal line
(an interrupted write) blocks everything except `cancel`, `status` and the
read-only `check` until root repairs `journal.jsonl`. The tool
never runs `grubby --set-default`/`--make-default` and has no code path for
them. Tools run by absolute path (root-owned and not group/world-writable,
symlinks followed), with a fixed environment, no shell and bounded output.
If `grubby --add-kernel` itself changed the default, `install` fails and
nothing tries to restore it. That needs Paul.

Not covered locally: real grubby/BLS/grubenv/dracut behaviour on Rocky 9.8,
SELinux labels of the copied files (they inherit the `/boot` and
`/lib/modules` defaults; Phase 3 evidence should include `ls -Z`), and
whether the hostonly initramfs boots under bhyve (risk R2).

## Required work before an executable exact plan

Read [the concrete gap and installation assessment](../../../docs/lab/Real-daemon-9p-base-gate.md).
The immutable195-file xz3 candidate remains unchanged. New operational helpers
need separately pinned source, executable and verification records. If the
runtime needs new control APIs, rebuild/reseal only under a separate explicit
artifact revision and retain the existing candidate/evidence unchanged.
