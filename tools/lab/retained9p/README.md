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

**The controller pins exactly one candidate:** the xz3 y64 rebuild
(2026-11-10), manifest `867ede3630e4ee0a609f88b0752670fac5b7a0c0e0f9d2a340031110da32c334`
(source `b869710`; see its
[evidence](../../integration/artifacts/evidence/xz3-20261110h/README.md)). All
three components were rebuilt. Its guest kernel (c64 recipe, so SELinux is an
active LSM) adds g64 (hfs `show_options`, exact `cache=mmap`, one block log per
session) and m64 (the Sources floor, every fault block logged, a clean unmount
silent) to the 964 fixed-tag release wait (zq3-r5 R5-1), the r54 mount HELLO
after enrollment and the w54 failed-bind `kill_sb` fence. Its CGO daemon
`34fb05b2…` carries i64, o64, r64, n64, j64, u64, v64 and x64 on top of d64.
These daemon bytes still need the f3 observer check. Its native exporter
`ef8f17de…` carries the 564, e64, p64 and w64 lib9p fixes and was built
natively on f3 (two clean builds, equal). The modules came out byte-identical;
the window artifacts were rebuilt from `b869710`. Seven earlier candidates are refused as
superseded: `3f770a88…` cannot arm the d44 MKDIR-only lost reply, the f44
`b203799e…` has the pre-r54 guest kernel (every hfs1.4 mount returns EPROTO,
zq3-r2 R2-4), the r54 `5eb42c50…` has the pre-w54 guest kernel (kill_sb oops
after a failed bind, zq3-r3 R3-1), the w54 `89c928d5…` has the pre-z54
daemon `174949c1…` (an unanswered pre-acquire VIEW_POLL blocks the first
mount, zq3-r4 R4-1), the z54 `2e0412a6…` has the pre-964 guest kernel
(fixed tag 65533 reused before release blocks the session, zq3-r5 R5-1),
the 964 `e01fab21…` has a guest kernel that boots without SELinux (c64) and
the pre-d64 daemon `39c92d01…` (zq3-r6 R6-1/R6-2), and the c64+d64
`66b5bb2b…` predates the zq3-r7 kernel, daemon and exporter fixes (g64, m64,
i64 to x64, 564, e64, p64, w64). Any
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
its byte digest without contacting the lab. The zq3 batch plan's Phase 8 values
come from `fill_plan.py batch` (see [run-time plan
fills](#run-time-plan-fills-zq3-f1-f6-f8)). The batch then runs one case at a
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
   previous payload is released. `syscall_batch_success` needs every planned
   case released: all five, or the five minus a declared deferral (below).
5. `lifecycle --output ABS --sha256 SHA --step STEP` (after every planned
   release) dispatches one zq3 T9-T11 guest step with the same intent journal:
   see [lifecycle steps](#guest-lifecycle-steps-zq3-g10-g11).

**Deferred cases (z64).** A plan runs `credentials, sync, metadata,
namespace, mapped-pressure` in that order unless it carries an optional
`deferred` list of `{"case", "reason"}` entries. Its `requests` must then be
exactly the five minus the deferred cases, in canonical order. Only
`mapped-pressure` (zq3 T5) may be deferred: its full 4 MiB canary needs more
view lifetimes than the 4 GiB host budget funds until q64 recycles view
tickets (Paul's 2026-11-10 decision); the other four are cheap and are the
T1-T4 evidence the lifecycle steps build on, so no general rule lets a plan
skip them. Unknown, duplicate or non-deferrable entries, an empty `deferred`
list (omit the key instead), a missing, blank, non-printable or over-512-character
reason, a declared case still present, a case missing without a declaration and
an empty batch all refuse. The declaration is part of the plan bytes and
digest; `check` prints `cases_planned` and `deferred`, the run state (and so
`status` and every command's report) repeats both and lists each deferred
case in `missing`, and `zq3_acceptance_complete` stays false. A plan without
the key is the five-case batch, byte-identical to a pre-z64 plan, so its digest
does not change.

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

After every planned payload is released (all five, or four with T5 deferred),
`controller.py lifecycle --step` sends
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

No guest-side warm-up can make MKDIR the first CALL after the ARM: the
GETATTR (from `may_create`'s permission check) and the LOOKUP of the new
name are issued inside the same `mkdir(2)`, uncached by design (analysis
with patch line references in the
[guestbridge README](../../../internal/guestbridge/README.md#targeting-the-mkdir-reply-d44)).
A runtime built with task d44 instead accepts an intent filter:
`correlate.py command --operation arm-lost-reply --lost-intent MKDIR` prints
the ARM with `--lost-intent MKDIR`, whose fixture lets those CALLs (and
background FORGET/PROCESS_EXIT CALLs) through and drops only the MKDIR reply,
after its effect committed. The expected T9 shape then becomes
`dropped-reply-effect-committed-and-visible` (m1 sees the leaf, m0 still
blocked). An older runtime refuses the flag, so it is opt-in.

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
  above `armed_high`, with claim and hashes; an ARM with the MKDIR filter
  (`intent`) must show `dropped_opcode` MKDIR, and both are copied to the
  record as `armed_intent`/`dropped_opcode`. The leaf state is decided by the
  unarmed m1 (m0 only if m1 is unmounted and m0 answered itself); m0's
  `EHOSTDOWN` is recorded as `effect_seen.armed_domain_blocked`. With whether
  the source advanced since the arm baseline this gives `classification`:
  `dropped-reply-effect-committed-and-visible`, `dropped-before-mutation` (the
  expected T9 shape without the MKDIR filter: m1 ENOENT, m0 blocked, no
  source change),
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
  [--lost-intent MKDIR] --runtime-binary PATH --control SOCK`: prints, never
  runs, the exact client argv: runtime, cut and epoch from the latest OBSERVE
  and sequence = last reply's sequence + 1; `--lost-intent` (ARM only) adds
  the d44 filter flag. ARM refuses unless the batch is released, no lifecycle
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

## Ordinary pod namespace fixture (zq3 G9, T7, decision A8)

[`pod_fixture.py`](pod_fixture.py) runs **on Rocky as root** inside the
approved window, after Phase 8 mounted `m0`. It implements the
[ordinary pod namespace profile](../../../docs/lab/Retained-9p-fixtures.md#ordinary-pod-namespace-profile).
The container-side code is [`pod_workload.py`](pod_workload.py). The driver
pins it by `WORKLOAD_SHA256`, so the staged `pod_fixture.py` hash pins both.
Stage both files root-owned in one directory: the driver reads the workload
from its own directory and refuses other bytes.
Both files are Python 3.9 compatible. Nothing here pulls an image, installs a
package, mounts, unmounts or forces anything, and it never kills a container
or the VM (see the timeout note under **Results**).

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
  the installed package, and that package must carry a signature
  (`RSAHEADER`). An empty or `(none)` signature refuses.
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
  - `/var/log/audit/audit.log` exists and is readable. Admission stats it and
    reads its last byte; the AVC delta is read after the containers exit.

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
`lo`, and its recorded flags must show it UP; a down loopback fails rather than
being inferred from `--network=none`. `/hfs/m0` must be exactly one 9p row with the `m0` tag and device. The
cgroup limits must equal the pins. Every matrix and lock outcome must match.
There may be no AVC denial for the pod in the audit delta. A rotated or
oversized delta is *incomplete*. SELinux must still be enforcing afterwards,
and pinned rootfs entries must be unchanged; the runtime may add only empty
mount points, which are reported.

**Results.** A pass removes the two exited containers with plain `podman rm`
and the exact five payload files and leaf on `m0`, after checking the listing.
A cleanup failure is reported apart from the T7 result. Any failure, timeout or
malformed report preserves the containers, payload and evidence. A driver
timeout waiting for the workloads is recorded as `uncertain` and kills nothing.
A short command that hangs (`rpm`, `podman inspect`, `podman rm`) is bounded:
its timeout kills only that client process, never a container, conmon or the
VM. Such a timeout during the run is a T7 failure, and the verdict and both
workloads' output are still written. Note that `w0` is PID 1 of the shared PID
namespace: if it exits early (its own failure or deadline), the kernel ends
`w1` too, and `w1`'s exit status records that. Evidence goes to
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
an approved bootstrap; `hfs-runtime-prep enroll-request` and `bind-request`
([runtimeprep](../../../internal/runtimeprep/README.md)) fill both requests
from the generated template and the runtime's actual records. The signer emits `enrollment.json` (the real
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
   `python3 -I -B rocky_kernel.py plan --package DIR --manifest-sha256 867ede36… --run-id zq3-r1 --stock-default-kernel /boot/vmlinuz-<Phase 1 default> --output /private/new-plan.json`.
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

## Phase 8 bootstrap orchestrator (zq3 G1)

[`bootstrap.py`](bootstrap.py) drives the run plan's Phase 8 from the
workstation, inside `bootstrap_seconds` (at most 120) from launch to READY,
followed by the post-READY `usable` gate under its own bound.
**It has only run against simulated f3, Rocky, runtime and operator.**
Running it needs Paul's explicit approval of the filled plan (A3, A4, A7);
the plan pin is no authority.

Python, like the other lab orchestration (`controller.py`, `runtime-guard.py`,
`runtime-volume-init.py`, `directlaunch`): it composes existing pieces over
SSH and subprocesses, reuses the controller's reviewed pins and durable-file
primitives, and keeps the Go tools (`hfs-runtime-prep`,
`hfs-retained9p-operator`) and the runtime unchanged. The order is the code's
(`runRuntimeWithOS`/`admitOperatorEnrollment` in
`internal/controlledruntime/driver_phases.go`):

| Step | Kind | What happens |
|---|---|---|
| `host-preflight` | read | f3: guard script and runtime match their pins; guard run dir, OwnerDirectory and EnrollmentFile absent; handoff directory root-private |
| `launch` | launch | `doas -n python3.12 -I -B runtime-guard.py … -- hfs-controlled-runtime --config … --disposable-validation` in one long-lived f3 SSH session, detached from this terminal; the 120 s start here |
| `proposal` | read | runtime stdout's first record is `AWAITING_OPERATOR_ENROLLMENT` naming `OwnerDirectory/proposal.json`; a private copy of its bytes |
| `guest-ready` | read | Rocky answers SSH, runs the plan's test kernel release (GRUB consumed `next_entry`), boot ID |
| `admission` | remote | one SSH round trip: pinned `rocky_kernel.py admission`, then manifest SHA256 readback, boot ID, provider nonce from `admission_v1`, `virtioN` of each share tag |
| `enroll-request` | local | `observer.json` with that manifest SHA256, then pinned `hfs-runtime-prep enroll-request`; the request must carry the measured values |
| `signed-enrollment` | read | prints the `hfs-retained9p-operator enroll` command and waits for its fresh output directory |
| `provision` | remote | one write of `provision.json` to `/sys/kernel/homelabfs/provision_v1` |
| `enrollment-handoff` | remote | `enrollment.json` atomically to the EnrollmentFile (temporary file, rename, one link, `0600`); the runtime then runs `ObserveAdmission` |
| `awaiting-bind` | read | second record `AWAITING_GUEST_BIND` (controller, provision hash) |
| `bind-request` | local | `hfs-runtime-prep bind-request` (recomputes the provision hash) |
| `signed-bind` | read | prints the `hfs-retained9p-operator bind` command and waits |
| `bind` | remote | one write of `bind.json` to `/sys/kernel/homelabfs/bind_v1` |
| `mount-0`, `mount-1` | remote | `mount -t 9p -o trans=virtio,version=9P2000.L.hfs1.4,cache=mmap,msize=65536 hfsN /var/tmp/hfs-zq3-RUN/mN`, checked in `/proc/self/mountinfo` |
| `ready` | read | third record `READY`, `CONTROLLED_LOCAL`, two domains, control binding |
| `usable` | read | post-READY usability gate (k64, b84): `guest-stat` lstats both mount roots as root right away and again once 40 s have passed since `mount-1` ended (the guest's 30 s QUERY timeout plus 10 s), with a host poll and a `host-observe` baseline (no open provider claim) in between; both roots must answer every time, and `host-observe` polls until the final probe's GETATTR and PROCESS_EXIT claims are TERMINAL on both providers. Writes nothing into the mounts, but each lstat adds GETATTR and PROCESS_EXIT claims to the provider ledgers and each OBSERVE consumes a lifecycle sequence |

```
python3 -I -B bootstrap.py check --plan PLAN --plan-sha256 SHA
python3 -I -B bootstrap.py run --plan PLAN --plan-sha256 SHA --output NEW_DIR
python3 -I -B bootstrap.py status --output DIR
```

**Plan.** Closed JSON (`homelabfs-zq3-bootstrap-plan/1`, `PLAN_SHAPE` in the
source): run ID, `bootstrap_seconds` (60-120, and equal to the bundle runtime
config's `BootstrapSeconds`), `remote_sha256` (the reviewed
`bootstrap_remote.py`), the signer path (only printed),
SSH executable and both pin files (the G8 targets and fingerprints, exactly
as the controller checks them), f3 `doas`/python, Rocky python, the guard
script/run dir/VM name/disk roots/skip paths, runtime executable/pin/config/
OwnerDirectory/EnrollmentFile, the staged `rocky_kernel.py` and its plan/
state/test kernel release, `hfs-runtime-prep` and the bundle's
`request-template.json` (pinned; its device tags must be `hfs0`, `hfs1`) and
`runtime-config.json` (pinned; only `BootstrapSeconds`, `OwnerDirectory` and
`EnrollmentFile` are read and compared with the plan, the host receipt keys it
also holds are neither kept nor logged),
the observer fields of `ObserverInput` except the manifest hash, and the two
mounts, which must be `hfsN` on `/var/tmp/hfs-zq3-RUN/mN` (what
`observer_key.py stage` creates). Every value that reaches a remote command
line is a plain path, hash or token (`[A-Za-z0-9_./=:,+-]`), so no shell
quoting is involved. `check` and `run` verify every local pin before any
contact: SSH, both host-key files, `hfs-runtime-prep`, the template and the
runtime config (both private files of this user, as `hfs-runtime-prep`
requires), and `bootstrap_remote.py`. A local misconfiguration therefore
refuses before the launch can consume Rocky's one-shot boot.

**Prerequisite on f3.** The remote half runs as `doas -n
/usr/local/bin/python3.12 -I -B -c "LOADER" …`. A doas rule that permits this
is effectively unrestricted root for `paul` on f3 (Python with `-c` runs any
code), whatever the loader checks. Grant it only for the run window and
remove it afterwards, or accept it explicitly.

**Operator signing (A7).** The orchestrator never reads the operator key
and never runs the signer. At each signing point it prints one JSON line
with the exact command, for example `hfs-retained9p-operator enroll --request
OUT/work/enroll-request.json --output OUT/work/signer-enrollment`, and waits
for that fresh directory to hold exactly the expected files, private to the
same user, with identical bytes on two polls 0.25 s apart. It then checks
the signed files against what it measured: `enrollment.json` is a
`RuntimeEnrollment` whose `Provision` equals `provision.json`, whose
`Observer` equals the request's (so `Observer.ManifestHash` is the measured
manifest SHA256), whose `Authorization.Enrollment` is the runtime's exact
proposal with only the empty `guest_boot` filled, and whose provision payload
names the measured boot ID and provider nonce, and whose single provision
group's local mounts name, in domain order, the request's sessions and tags
and the measured `virtioN` devices; `bind.json`'s payload must be
exactly (boot ID, provider nonce, the runtime's provision hash and
controller, the proposal's group ID). Any extra file or mismatch fails the
step before anything is written to the guest. The operator runs the signer
on the workstation as the same user; both signings count against the 120 s.

**Durability and uncertainty.** `OUT/journal.jsonl` gets a durable intent
before every step and a result after it (`ok`, `failed`, `uncertain`), in the
fixed step order; out-of-order steps, steps after a non-ok result and steps
while an intent is open refuse. A mutating call whose outcome was not observed
(an SSH timeout or lost transport during a sysfs write, a remote exit 1, an
overflow, malformed output) ends `uncertain`. The run stops there; nothing
dependent runs, and nothing is replayed: there is no resume and `run`
refuses an existing output directory. A Ctrl-C or kill leaves an open intent,
which `status` reports as uncertain. Only read-only polls are retried. A
mutating step is not dispatched with less than 3 s left, `admission` not with
less than 40 s (`udevadm settle` may take 30 s) and a mount not with less than
10 s; it fails undispatched instead. A deadline stops dependent steps; the orchestrator never signals, kills,
unmounts or deletes anything remote. After a failure the runtime's own
`BootstrapSeconds` fence it and the guard writes its receipt. Every call's
arguments, exit status and streams are kept in `OUT/calls/`, the signed and
request files in `OUT/work/`, the guard transport's streams beside them, and
`OUT/summary.json` at the end (`acceptance: false`).

**Usability gate (`usable`, k64).** In zq3-r6 and zq3-r7 the driver
reported READY with every step `ok` while the guest had already blocked
both sessions. The runtime answered each mount(8) PROCESS_EXIT claim
`ADMITTED/OK` but left it `RESERVED/UNPREPARED`, so every QUERY returned
`INDETERMINATE`. After 30 s the guest blocked the session, and every later
operation, even `getattr` on the root, returned `EACCES`. The failure
surfaced only in T1/T6. So READY is no longer the last step.

- The `usable` step runs `guest-stat` over both plan mounts in one SSH round
  trip. For each root it checks that the mount point is still exactly one
  `9p` mount of its tag in `/proc/self/mountinfo`; a root that is not is
  never stat'ed. Then it runs one `os.lstat` of the root in a child process
  bounded by 8 s. The child never opens, creates or writes anything in the
  mount, and is never killed if it hangs.
- **The gate is not read-only on the host.** Each lstat is an hfs operation.
  `hfs_begin` → `hfs_capture` registers the child as an hfs process and
  sends a GETATTR claim, and the child's exit queues its own PROCESS_EXIT
  claim (patch 0003). The two probes therefore add about four durable claims
  to each provider ledger before T1, two per probe. In an r7-type failure
  the probes' own claims stay unresolved too, and a failed PROCESS_EXIT
  blocks the session just as the mount's did.
- A first probe runs right after READY, so a root that is already refused
  fails at once. The final probe runs once 40 s have passed since `mount-1`
  ended. Between the two probes the step watches the guard transport, then
  one `host-poll` confirms that the guard reports no breach or exit and that
  READY is still the last runtime record.
- **Why 40 s:** the guest's QUERY deadline is 30 s, but it checks that
  deadline only after each QUERY reply, and each QUERY has its own 30 s
  transport deadline. So the block lands at 30 s plus one QUERY round trip.
  In r7 the round trips took milliseconds, so a 10 s margin is enough. A
  QUERY stalled for longer than 10 s could let the final probe run before
  the block. Such a stall would most likely also hold the probe's own
  GETATTR past its 8 s bound, which fails the gate as a timeout. The gate
  makes the r7 failure very unlikely to pass; it is not a proof.
- READY ends the runtime's launch-to-READY bootstrap timeout, so the step
  gets its own fixed bound in place of the driver's deadline, using the same
  deadline mechanism: the settle point (or now, if READY came later) plus
  30 s for the probes and 30 s for the b84 claim check (below). In practice
  that is at most about 100 s after `mount-1`.
- An errno, a timeout, a lost mount, a non-directory root, a missing root, a
  probe transport failure, a guard or runtime exit, or the gate's own
  deadline ends `usable` as `failed`. Every failure's journal detail carries:
  - `probe`: `first`, `settle` (waiting or the host poll), `baseline` (b84
    OBSERVE before the final probe), `final` or `claims` (b84 OBSERVE after
    it);
  - `roots`: both roots' results, or null when no probe result exists;
  - `errno`: the first failing root's errno (13 for zq3-r7's `EACCES`), or
    null for a timeout, a lost mount or a failure outside the lstat.

  The run exits 1 with `summary.json` `stopped_at: usable` and a
  `usable_note`. Nothing is retried, remounted, written into the mounts or
  unmounted: go to Phase 11 with inspection first. `fill_plan.py batch`
  refuses such a run, because it requires every step in `STEP_NAMES` to be
  `ok`. The step list changed (k64), so the journal version became `/2`;
  b84's claim check made it `homelabfs-zq3-bootstrap-journal/3`, and
  `fill_plan.py` refuses `/1` and `/2` journals by their version.
- Remote child bounds: every `--seconds` must be 1..120, or the remote half
  refuses with exit 2 before running anything.
- **Host-side claim check (b84).** zq3-r8 showed the lstat is not enough:
  m0's first-probe lstat returned, but the lstat child's own PROCESS_EXIT
  claim stayed `RESERVED/UNPREPARED`, and the guest blocked m0 about 30 s
  later ("blocked by QUERY timeout on PROCESS_EXIT"). The gate now also
  reads the runtime's OBSERVE:
  - `host-observe` (read-only remote action on f3, under `doas`) re-checks
    the plan's runtime executable pin and runs it once in client mode,
    `--control SOCKET --operation observe --runtime UUID --sequence N`, with
    the socket and runtime UUID from the READY record (the socket must sit
    directly in the plan's OwnerDirectory). The reply (at most 128 KiB) is
    kept in `calls/`. A client that does not return within its bound is
    left running and reported as a failure.
  - OBSERVE evidence `/2` (b84, `internal/controlledvalidation/evidence_claims.go`)
    gives each provider ledger a bounded `Claims` summary: principals, the
    sums of allocated high-water and retirement floor, present entries, the
    open count (neither TERMINAL nor a declined gap), up to 16 open and the 8
    newest entries as number, opcode, status and stage. No binding,
    credential, name or process identity, and no age (the ledger stores no
    time; the driver records `claims_seconds_after_final` itself).
  - Phase `baseline`, after the settle point and the host poll: OBSERVE
    until neither provider has an open claim (the mounts' and the first
    probe's claims all settled). zq3-r8's stranded first-probe PROCESS_EXIT
    fails here, before the final probe runs.
  - Phase `claims`, after the final probe: OBSERVE every 2 s until, per
    provider, at least two numbers were allocated after the baseline's
    `Allocated`, nothing is open, and a TERMINAL GETATTR and a TERMINAL
    PROCESS_EXIT are among the newer numbers (or every number is already
    retired, which implies TERMINAL). Claim numbers are per principal; the
    guest uses one per session, and the gate requires exactly one.
  - An OBSERVE that is not `complete` and `SERVING`, has the wrong evidence
    version (a runtime without b84), lacks a provider's `Claims` or names
    other sessions fails at once. A transport miss retries with a fresh
    sequence. At the gate's deadline the failure names each open claim
    (`hfs0: 1 claim(s) not TERMINAL: #6 PROCESS_EXIT RESERVED/UNPREPARED`);
    the journal detail adds `claims` (the last per-mount summary) and
    `observe_sequence_last`, and `summary.json`'s `usable_note` says a claim,
    not a root, failed.
  - Bound: the settle point plus 30 s (probes) plus 30 s (claims, the
    guest's own QUERY window: a claim slower than that blocks the session
    anyway), so about 100 s after `mount-1`.
  - Side effects: each OBSERVE refreshes both Admissions through the guest
    observer (a failed refresh fences that Admission, as any refresh does)
    and consumes one lifecycle sequence, even when its reply is lost.
    `summary.json` records `observe_sequence_last`; Phase 9 and any later
    OBSERVE must use a larger sequence.

  Until then the guest-side stat is the only gate. It detects the
  consequence (the blocked session), not the cause.

**Remote half.** [`bootstrap_remote.py`](bootstrap_remote.py) holds the fixed
f3/Rocky actions (Python 3.9 compatible). It is not staged: each SSH command
is `PYTHON -I -B -c "LOADER" ACTION ARGS`, where the loader reads exactly the
helper's length from stdin, compares its SHA256 and only then executes it
(exit 97 otherwise). That hash comes from the orchestrator on the same
command line, so the loader only proves that f3 and Rocky run exactly the
bytes the orchestrator sent, intact; it does not say which bytes are
reviewed. That is the plan's `remote_sha256`: the orchestrator refuses before
any contact unless its `bootstrap_remote.py` has that SHA256, and sends only
those checked bytes. A payload follows on the same stdin and is checked against
`--size`/`--sha256`. Exit 0 means done, 2 a known state (with `changed`), 1
uncertain. Remote children (`rocky_kernel.py`, `mount`) are never killed on a
deadline; the action reports uncertain instead.

**Guard transport.** The guard runs in the foreground of one f3 SSH session
started in its own session with stdin `/dev/null` and streams to files, so a
Ctrl-C of the orchestrator does not reach it and it outlives the
orchestrator (READY returns; Phases 9-11 follow). Losing that SSH session
does **not** stop the guard: the session has no tty (`-T`), so sshd sends no
SIGHUP, and the guard and the runtime keep running on f3 under the guard's
own bounds (3600 s, RSS, disk, memory, VM context). The orchestrator only
loses its view: it stops all dependent steps and records the transport loss
in the step result and in `summary.json` (`guard_transport_note`). The
operator then reconnects to f3 and inspects the guard run directory; the
runtime's own bootstrap timeout or the guard limits end the runtime. The
same message is recorded when the guard itself finished (its `receipt.json`
says why), because the local SSH exit cannot tell the two apart. Keepalive
is 15 s × 8 to ride out short stalls. The orchestrator never signals the
transport; `status` reports whether its local PID still exists.

**Local evidence.** `test_bootstrap.py` runs the whole sequence and its
negatives against a simulated world: guard refusal, guard transport exit,
breach, timeout mid-step (uncertain, dependents not run, no replay), killed
orchestrator, remote exit 1/2/97, wrong manifest hash, changed boot, wrong
kernel, request without the measured manifest, changed prep binary, signer
manifest/provision/proposal/bind mismatches, provision local mounts not
naming the measured devices, a mismatched bind request, extra files, the
deadline during the signer wait, no dispatch close to the deadline,
admission not dispatched without time for `udevadm settle`, the recorded
guard transport loss, plan refusals (remote, prep and config pins, a
non-private template, `BootstrapSeconds` and path mismatches), runtime stage
order violations, journal order, plan refusals, the SSH argv and pins, the
loader through `/bin/sh` and `bash` with tampered bytes, bounded local
children, and the `usable` gate (success only after the settle point, `EACCES`
on the first or the final probe, an lstat timeout, a probe transport timeout
as `failed` not `uncertain`, the gate's own deadline, a lost mount, a missing
root, a runtime exit during the settle, a READY later than the settle point,
each failure's `probe`/`roots`/`errno` detail; b84: the claim baseline and
final check, a stranded final-probe PROCESS_EXIT, zq3-r8's stranded
first-probe PROCESS_EXIT failing at the baseline, a slow PROCESS_EXIT that
settles within the poll, an OBSERVE transport miss retried on a fresh
sequence, an incomplete, `/1` or claim-less OBSERVE, and `claims_problems`
on hand-made summaries; `test_bootstrap_remote.py` covers `host-observe`). The signed files it checks are real signer output from
`internal/runtimeprep/bootstrap_fixture_test.go` (throwaway keys, no key
files exported; regenerate with `HFS_BOOTSTRAP_FIXTURE_DIR=NEW_DIR go test
-run TestExportBootstrapFixture ./internal/runtimeprep` and copy the nine
files over). Its guest budget is the example spec's (reservations 1024,
received 256, 512/512 MiB ordinary/control, as in the zq3-r4 bundle); x54
regenerated it after w54's guest floors refused the original 32 / 128 MiB.
Its view bounds are also the example spec's: `Sources` 10, `LedgerBytes`
188 MiB, `WorkBytes` 736 MiB, `SourceBytes` 6 MiB. i64 regenerated it when
`Sources` rose from zq3-r7's 2, which refused every non-read op, to clear
the `2*domains + 4` = 8 floor (`viewcontrol.MinSources`); o64 regenerated it
when `Sources` went from 12 to 10 to fund the provider window budgets
(`internal/runtimeprep/README.md`, capacity notes).
`TestBootstrapFixtureAboveGuestFloor` and
`TestBootstrapFixtureAboveSourceFloor` in the same Go file check the
committed budget against `checkGuestFloor` (`guestfloor.go`) and
`viewcontrol.CheckSourceFloor`, so the fixture cannot fall below either floor
silently; the Python tests only replay it.
`test_bootstrap_remote.py` covers every remote action against a temporary
root, including the `--seconds` bound, and runs the `guest-stat` lstat
program for real there. Not covered:
real SSH/doas, FreeBSD rename semantics, Rocky's sysfs and 9p mount
behaviour, the guest's real QUERY timing after READY, the runtime itself and
real timing of the 120 s.

## Run-time plan fills (zq3 F1, F6-F8)

Two zq3 plans are written ahead of the run as templates with placeholders
for values that exist only during the run. [`fill_plan.py`](fill_plan.py)
writes the final plans from recorded evidence on the workstation (no SSH, no
lab contact), so nothing is hand-edited under time pressure:

```
python3 -I -B fill_plan.py bootstrap --template TEMPLATE --template-sha256 SHA \
  --ssh-fact P4_F3_SSH_SHA256_FILE --output NEW_PLAN
python3 -I -B fill_plan.py batch --template TEMPLATE --template-sha256 SHA \
  --bootstrap-run BOOTSTRAP_OUT --bootstrap-plan-sha256 SHA --output NEW_PLAN
```

| Command | Placeholder → source |
|---|---|
| `bootstrap` | `observer.ssh_sha256` (`PHASE4_F3_USR_BIN_SSH_SHA256`) ← the Phase 4 fact file, exactly one `sha256 -r` line `HASH PATH` whose path is the plan's `observer.ssh` |
| `batch` | `boot_id` (`PHASE8_BOOT_ID`) ← `guest-ready` result, equal to the `admission` result's boot ID |
| | `provider_nonce` (`PHASE8_PROVIDER_NONCE`) ← `admission` result |
| | `installed_manifest_sha256` and each request's `manifest_sha256` (`PHASE8_ADMISSION_MANIFEST_SHA256`) ← `admission` result |
| | each request's m0/m1 `mount_id` (`PHASE8_M0_MOUNT_ID`, `PHASE8_M1_MOUNT_ID`) ← first field of the `mount-0`/`mount-1` results' `mountinfo` row |

`batch` reads the `bootstrap.py run` output directory as that tool wrote
it, and refuses unless all of these hold: `journal.jsonl` (no torn tail)
starts with the run record for the pinned bootstrap plan SHA256 and holds
exactly one intent and one `ok` result per bootstrap step, in order;
`plan.json` has that SHA256 and the journal's run ID; `summary.json` says
`ready` (a run that reached READY but failed the post-READY `usable` gate
therefore refuses); the boot ID and provider nonce are UUIDs and the manifest is
SHA256 hex; `work/enroll-request.json` still has its journaled SHA256 and
carries the same boot ID, provider nonce and `Observer.ManifestHash`; each
`mountinfo` is one row of a `9p` mount of the plan's tag on the plan's
mount point with a positive decimal mount ID, and the two IDs differ. Each
template mount's path, tag and session must equal the bootstrap plan's
mount and the enrollment request's session for that domain, so a template
from another bundle or run refuses.

For both commands the template is pinned by SHA256; every placeholder must
sit exactly in its field (an already filled or moved value refuses) and any
other `PHASEn_…` string refuses as an unknown run-time value. The filled
plan is written privately (`0600`) to `NEW_PLAN.next`, checked by the
consumer's own validation (`controller.py`'s `plan_snapshot`, the same as
`check-retained9p.sh check`, or `bootstrap.py`'s `load_plan`, the same as
`bootstrap.py check`), and only then linked to `NEW_PLAN`; an existing
output is never replaced. It prints one JSON line with the filled values
and the plan's `sha256` for the next pinned command. Filling grants no
authority.

A case deferral (z64) is declared in the batch template itself: the
`deferred` list with its reasons and only the remaining requests. `batch`
copies it unchanged, so the template pin, the filled plan and its SHA256 all
carry it, the controller validation decides whether it is allowed, and the
report adds `cases_planned` and `deferred` (`[]` for the five-case batch).

**Local evidence.** `test_fill_plan.py` produces its bootstrap evidence by
running the real `bootstrap.run` against `test_bootstrap.py`'s simulated
world (only the mount replies carry realistic `mountinfo` rows), fills the
batch plan and checks it with `plan_snapshot` and `controller.py check` as
a child process, and fills the bootstrap plan and checks it with
`bootstrap.py check`. Negatives: a run stopped before READY, a READY run
whose `usable` gate failed with `EACCES`, a `/1` or `/2` journal, a non-READY summary, another bootstrap plan, a torn journal, malformed, foreign, non-9p
or multi-row `mountinfo`, zero or leading-zero IDs, duplicate IDs,
disagreeing admission/enrollment evidence, a changed enrollment request,
template pin, filled, moved and unknown placeholders, a template of another
bundle or run, consumer validation refusals (no output, no `.next`; including
an undeclared missing case and a deferred `credentials`), an
existing output, and malformed or foreign `sha256 -r` fact files.

## Required work before an executable exact plan

Read [the concrete gap and installation assessment](../../../docs/lab/Real-daemon-9p-base-gate.md).
The immutable195-file xz3 candidate remains unchanged. New operational helpers
need separately pinned source, executable and verification records. If the
runtime needs new control APIs, rebuild/reseal only under a separate explicit
artifact revision and retain the existing candidate/evidence unchanged.
