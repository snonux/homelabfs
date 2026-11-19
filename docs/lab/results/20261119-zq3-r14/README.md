# zq3-r14: READY and Phase 9 again; T1's root OPEN outlasts guest.py's 10 s alarm behind three recalls; the R13-1 unmounts are fixed (2026-11-19)

Run sheet: [filled plan](../20261119-zq3-r14-filled-plan/run-sheet.md)
(no run-time revision).
Candidate xd4 `3a7131dd…` ([xz3-20261119-r14](../../../../tools/integration/artifacts/evidence/xz3-20261119-r14/README.md)):
the oc4 guest kernel (build ID `9b167690…`) and the pc4 exporter
`2fd7e32f…`, both running for the first time, with the nc4 daemon
`c959b708…` reused byte for byte. T5 stayed deferred. Times are UTC.
Private evidence: `/home/paul/tmp/claude/hfs-zq3-r14-fill/evidence` (run log
`RUN-LOG.md`), controller output
`/home/paul/tmp/claude/hfs-zq3-r14-fill/ctrl-run/credentials/`, bootstrap
journal `/home/paul/tmp/claude/hfs-zq3-r14-fill/bootstrap-run/`. Offline
decode: `/home/paul/tmp/claude/r14-analysis/` (copies only).

## Outcome

Phases 0 to 9 passed, and the matrix started this time: Phase 9 finished
about 80 s after READY. T1 (`credentials`) failed during guest admission
(new finding R14-1), so T2-T11 did not run (the sheet allows no retry).
Phase 11 took the guard-SIGTERM branch. With the host gone, both mounts
blocked with the new oc4/pc4 error and unmounted in 1 s each, so **R13-1 is
fixed**. Restoration was clean.

**What held:**
- Phase 1a unloaded `fusefs` and `fdescfs` again (Refs 1/1, no mounts,
  both `rc 0`, neither listed afterwards). Phase 4 step 1a qualified the
  daemon `c959b708…` (uid/gid 0, groups `[0,0,5]`, pid 80757).
- Phase 5: the active-based estimate left a 0.22 GB margin and recommended
  the UMA drain; step 1b freed about 0.28 GB (67,857 pages;
  `uma_free_bytes` 310 MB → 39 MB). Phase 6 passed with a 0.41 GB margin
  (reclaimable 15,175,796,864 bytes against a bar of 14,763,950,080).
- Phase 8: the watcher made both operator signatures (enroll, then bind
  3.2 s later). mount-0 (mountinfo 391) and mount-1 (408) both came up with
  the exact g64 tuple. READY came 32 s after launch (12:37:06Z →
  12:37:38Z). The `usable` gate passed (claims 1.87 s after the final call,
  `observe_sequence_last` L=2).
- Phase 9 (12:38:59Z, inside the new 5-minute budget): three SSH no-ops,
  OBSERVE at L+1=3 passed (both domains `REOPENED`, fence 3, claims
  `Open 0`), mountinfo had exactly the two tuples, `Enforcing`, the SELinux
  `Initializing` line, no hfs block line.
- 984 timing (final): SSH no-op median 0.329 s (3 samples), OBSERVE net
  median 0.493 s, max 0.521 s (2 samples), below the 2.4 s bound.
- T1's admission failure left nothing behind: no guest block line, no
  transport fence (`fenced: false`), `runtime.stderr` empty. Unlike
  zq3-r10, the probe's cleanup and its `PROCESS_EXIT` went through
  (y84, x84).
- **First run of the oc4 kernel and the pc4 exporter.** When the guard
  stopped the runtime, both sessions logged at uptime 177.7 s
  `blocked by uncertain control result on VIEW_WAIT (-112), exporter ERROR 5`
  (EHOSTDOWN from pc4's host-unavailable reply) and
  `view control worker stopped, error -112, exporter ERROR 5, at VIEW_WAIT`.
  That is the designed path. Both `umount`s returned `rc 0` in 1 s, with
  no new block lines, and the guest poweroff took seconds (zq3-r13:
  about 7.5 minutes).
- No AVC records, no relabel. U2 removed the test kernel.

**New finding:**
- **R14-1:** T1's guest admission opened the m0 root (`open-root[0]`) at
  1.6 s after `identity-checked`, and the open returned only 14.1 s later.
  guest.py's 10 s admission alarm had already expired, so the probe failed
  with `Deadline` (terminal at 17.1 s; `admission_ms` null,
  `deadline_hit` true, `etimedout` false). The controller recorded
  `dispatching-uncertain` (`Refused: required actual syscall case failed:
  credentials`, 18.2 s). See the analysis below.

| Phase | Result |
|---|---|
| 0-1, 1a | passed (both modules unloaded) |
| 2-7 | passed (process observer `c959b708…`, nightly gate GO 12:33Z and 12:36Z, step 1b UMA drain run, Phase 6 margin 0.41 GB) |
| 8 | passed (mount-0 and mount-1 ok, READY 32 s after launch, `usable` L=2) |
| 9 | passed (OBSERVE L+1=3; 984 timing) |
| T1 | **fail** (R14-1) |
| T2-T11 | unrun (no retry after T1; T5 deferred) |
| 11-12 | completed (guard-SIGTERM branch; R13-1 fixed) |

T1 started at 12:39:03Z with the guard budget GO (3486 s left). The
baseline OBSERVE at L+2=4 passed. Phase 11 ran the guard-SIGTERM branch:
the final 984 sample, then one SIGTERM to guard 82283 at about 12:40:10Z.
The guard was gone at the second check (12:40:20Z). Receipt: `runtime_exit
0`, only the `guard-interrupted SIGTERM` breach (183.6 s), runtime not
running at guard exit, exporter (pid 82297) still alive. Then: evidence
tarballs, the fence record (`fenced: false`), the unmounts (above), observer
removed (`authorized_keys` back to `e08542dc…`, `restored_pre_run: true`),
`next_entry` consumed, no AVC, full dmesg (459 lines), poweroff at
12:41:10Z, host cleanup (tap and vmm destroyed, UEFI vars `379f79e7…`
unchanged), native FUSE unmount (`rc 0`), plain literal observer key
deletion on f3 through the bash wrapper (`rc 0`, only `known_hosts` left)
and the window restore (`mount_restored: true`, `fusefs.ko` kept loaded by
the reviewed plan). Phase 12: `start-rocky` at 12:41:44Z, SSH at the second
try (12:42:01Z), `check --expect stock` ok, `Enforcing`, `ak e08542dc…`, no
`/.autorelabel`, vm-bhyve run 1 only.

## R14-1: the m0 root OPEN waited behind three recalls

### What the evidence shows

1. **Guest timeline** (`stdout.log`, guest monotonic): `identity-checked`
   at 112.24 s, `open-root` (m0) at 113.82 s, `failure` (Deadline) at
   127.95 s, `terminal` at 129.36 s. guest.py arms `signal.alarm(10)` at
   process start and masks SIGALRM across `os.open` (y84), so the alarm
   fired about 10 s after start, during the open, and `Deadline` was raised
   only when the open returned. The m0 root open therefore took about
   14.1 s. The guest clock matches f3's to within about a second (the
   block lines at 177.7 s coincide with the guard stop at 183.6 s after
   launch).
2. **Three recalls during T1.** The baseline OBSERVE (12:39:00Z) showed
   both domains at fence 3 with no recall. In the owner snapshots after
   T1, m0 is at fence 6 (domain sequence 20) and m1 at fence 5 (sequence
   16). A fence counts completed recalls, and each recall uses four
   sequence numbers. So m0 took part in three recalls during T1 and m1 in
   two. m1 issued no claim at all (still 6 allocated), so all three were
   started by m0's T1 traffic: **one m0-only (scoped) recall and two
   all-domain recalls**.
3. **The last recall is all-domain and completed.** The ACK archive keeps
   only the last recall, `f5a682d5-…`: m1 sequences 13-16 and m0 17-20, all
   five ACKs per domain, ending with both REOPENED ACKs ACCEPTED (native
   cells 1675 for m0 and 1682 for m1). The controller state is identical in
   all three snapshots (live, after, f3 at guard exit): `Recall` null,
   `Leases`, `Reservations` and `Tickets` empty.
4. **The claims completed.** m0 allocated claims 7-12 during T1. The guest
   retired through 11 (RETIRE at native cell 1742) after claim 12, T1's
   `PROCESS_EXIT`, came back TERMINAL OK. No description grant is left on
   m0. Both mounts then went back to idle `VIEW_WAIT` polling (`EMPTY`)
   until the guard stop. m0's first retained `VIEW_WAIT` after sequence 20
   started at about 12:39:22Z (from the exporter's 1000 ms wait deadline
   in f3 monotonic time), right after the guest's terminal.
5. **Nothing else failed.** No guest block line, no transport fence, no
   `etimedout`, `runtime.stderr` and `exporter.stderr` empty.

### What the OPEN waited on (inference from the code)

- `hfs_open` (patch 0003) runs a read-only open with `hfs_begin(…,
  mutation=false)`, so it takes a DOMAIN READ observation lease
  (`hfs_mmap_begin`, patch 0004) and sends OPEN under it. On the host,
  OPEN is not a `readOperation` (`internal/viewcontrol/admission.go`), so
  `conflicts(false)` matches the caller's own active lease and
  `recallBarrier` starts an **all-domain recall** and answers WouldBlock.
  The guest's QUERY then waits until REOPEN. This is the zq3-r11
  self-conflict (task **z94**), and it is deterministic: every read-only
  OPEN pays it.
- guest.py's `realpath` lstat of the m0 root is a GETATTR under its own
  observation lease. Releasing a clean READ lease recalls its domain
  (784's scoped recall, `releaseRecallScope`). That matches the one
  m0-only recall, and the same pattern gave both domains their two recalls
  during mount and `usable` (fence 1 → 3). A release that arrives while
  another recall runs needs none once that recall has quiesced the lease,
  so this scoped recall most likely ran first, overlapping the start of
  the OPEN (whose acquisition waits for it).
- The trigger of the **second all-domain recall** cannot be read from the
  retained evidence. Two candidates fit: the OPEN conflicting a second time
  (for example a VFS `ESTALE` retry of the open, which takes a new
  observation lease and conflicts again), or an expected-target mismatch on
  an acquisition (the all-domain recall of `expectedMismatch`, task e84).
  zq3-r10 showed the same shape (m0 fence 3 → 6, the last recall
  all-domain).
- **Why 14 s.** Three recalls plus the lease and claim exchanges took about
  15.7 s in total, roughly 4-5 s per recall. patch 0004's comments assume
  "a clean recall takes well under a second". On the lab, each recall
  phase's first VIEW_ACK needs a fresh SSH observation of the guest before
  the host accepts it (`internal/controlledruntime/README.md`), and the 984
  timing puts one at about 0.5 s. With five ACK phases per recall that is
  about 2.5 s per recall before event polling, QUERY turnaround and the
  lease acquire/release exchanges. Every idle native exchange took about
  25-40 ms beyond the 67 ms poll pacing (about 15 native cells/s for both
  domains, during and after T1).

The forwarder ring and the transport ledger had already pruned everything
but the last few cells, the provider ledgers keep only the last claims, and
the ACK archive has no timestamps. So **the order of the first two
recalls, each phase's duration and which ACK was slow cannot be
recovered**.

**Confidence:**
- high: the root OPEN itself took about 14 s and outlasted the 10 s alarm;
  the host was not stuck (all three recalls completed, claims 7-12
  terminal and retired, no fence, no block);
- high: three recalls ran during T1, one m0-only and two all-domain, all
  started by m0's traffic;
- high: a read-only OPEN under its own observation lease starts an
  all-domain recall (code, and the zq3-r11 offline repro), so at least one
  all-domain recall is z94's;
- medium-high: the scoped recall is the release of the lstat's observation
  lease;
- medium: the per-ACK SSH observation makes each recall cost seconds and
  is the main reason the total reached 14 s;
- low: the trigger of the second all-domain recall and the recall order.

**Not contributing, as far as the evidence shows:** o84 TryLock contention
(no refusal or retry is visible, though the pruned cells could hide short
WouldBlock retries; it is not needed to explain the time) and observation
latency beyond its multiplier (the 984 samples are 0.46-0.52 s, well inside
the bound).

### Recommended fix

1. **z94** (viewcontrol): a read-only OPEN (READ access, no create,
   truncate or append) conflicts like a read, only with EXCLUSIVE_DIRTY
   leases and open reservations. It must still wait for an in-progress
   recall, because OPEN pins and so moves the cut. This removes the OPEN's
   all-domain recall(s), the largest and both-domain part of R14-1. Tests
   with 2a4's credentials-admission replay.
2. **guest.py's 10 s admission alarm is not realistic.** Even with z94,
   each root's lstat release still costs one scoped recall (about 3-5 s on
   the lab), and admission does that for both roots. patch 0004 documents
   about 90-120 s as one syscall's worst case (view wait, VIEW_ACQUIRE,
   allocation fence and request window, each 30 s). The kernel already
   bounds every hfs syscall, so the alarm only has to catch a hang: set the
   admission alarm to at least one worst-case syscall plus margin (for
   example 150 s), below the controller's dispatch bound, and raise that
   bound (75 s today) and the case alarm (60 s) to match. Keep
   `admission_ms` and the step markers as evidence of the real latency.
3. **Evidence:** keep a small timestamped recall history that pruning does
   not drop: the trigger (op, session, reason: conflict, release or
   mismatch), each phase start and each ACK's accept time, and the guest
   observation probe time. With it a failure like R14-1 can be timed
   instead of inferred.
4. **094/2a4** (984 model): replay the credentials admission and report its
   latency against guest.py's alarm, with the per-ACK observation cost
   modelled, so the next admission budget is set offline.

## Host events and facts

- f3 boot `1794574308` unchanged. N1: Rocky clock `Europe/Sofia`, stamp
  `2026-11-19`, newest dnf transaction still #64 (unchanged); nightly gate
  GO at 12:33Z (Phase 1) and 12:36Z (Phase 5 step 0).
- Rocky before: PID 46923 (UTC lstart `Thu Nov 19 08:24:51 2026`), boot
  `5d5c32fe-…`, stock `741.22.1`, pod runtime unchanged, pod manifest
  `523485cc…` (same as zq3-r13). Stopped by ACPI at 12:36:30Z, started
  normally at 12:41:44Z: PID 84753 (UTC lstart `Thu Nov 19 12:41:44 2026`),
  boot `b4d9ee31-d25e-403b-b217-8a166368e0fd`. **Rocky was down about 5
  minutes** (12:36:30Z to 12:41:44Z).
- Phase 4: window plan WP=`a6d3f086…` (F3_NOW 12:35:22Z, expires
  13:30:22Z), descriptor execution through fdescfs supported
  (`5900ff0d`); observer key enrolled (EA `75427aaa…`).
- After READY: B=`2c4e5cc9…` (cases credentials, sync, metadata,
  namespace; T5 deferred), U=`d9ed1aa5…`, L=2. The guest requests were
  written (lock-m0 `680228cb…`, lock-m1-t8 `aaa158dd…`, pod `4cad3337…`)
  but never used.
- Guard peaks: disk 23.6 MB, exporter RSS 303 MB, runtime RSS 40 MB.
- **Operator slip (child wait):** after the poweroff, the operator's
  wait helper for the bhyve child first used zq3-r13's stale
  `hfs-zq3-r13` pattern. The child was then checked directly with the
  `[h]fs-zq3-r14` pattern and was gone by 12:41:3xZ.
- Final f3 state: Rocky running, `/dev/vmm` holds only `rocky`, `tap100`
  gone, no fuse/fdesc mount, `fdescfs.ko` (31) and `fusefs.ko` (32)
  loaded, zroot ONLINE. Rocky has no `hfs-xz3` kernel (U2).

## Follow-up tasks

- **R14-1 → z94** (viewcontrol, above), then **zq3-r15** with a fresh fill
  to run T1-T11.
- **guest.py admission alarm** (and the matching case alarm and controller
  dispatch bound): sized from the documented worst case, not 10 s.
- **Recall history with timestamps** in the controller ledger or a bounded
  runtime event log.
- 094/2a4: credentials-admission replay with latency against the alarm.
- Still open: e84 (target-mismatch recalls are all-domain), qc4 (zq3-r12),
  0a4 (zq3-r11).
