# zq3-r19: fchown now works and every expected-target recall covers one mount, but T1's access child gets EAGAIN because the guest counts a busy local retirement round against its BLOCKED retry cap (2026-11-21)

Run sheet: [filled plan](../20261121-zq3-r19-filled-plan/run-sheet.md),
with review round 1 applied (merge `2a07636`).

Candidate r19 `09acd09d…` ([xz3-20261121-r19](../../../../tools/integration/artifacts/evidence/xz3-20261121-r19/README.md)):
- the vf4 guest kernel, build ID `e3d474df…`;
- the wf4 daemon `bb4ad73c…`;
- the exporter `2fd7e32f…`, reused.

`ControlTickets` stayed at 24 and T5 stayed deferred. Times are UTC.

Private evidence:
- `/home/paul/tmp/claude/hfs-zq3-r19-fill/evidence`, with the run log in
  `RUN-LOG.md`;
- controller output in `/home/paul/tmp/claude/hfs-zq3-r19-fill/ctrl-run/credentials/`.

## Before the run

- iom1, iom2 and iom3 (2 GiB each) were stopped by Paul's decision. The
  iormesh session stopped them after an out-of-memory incident at 14:20Z,
  which a `grep -r` over `/zroot/bhyve` had caused. They stay stopped by
  default.
- With Paul's approval for this run, Phase 0b cleared f3's swap
  (852,220 KiB in use, 10.8 GB free): `swapoff -a`, then `swapon -a`, after
  which 0 was in use.
- The iormesh session held its zg4 test until the run was finished.

## Outcome

Phases 0b to 9 passed. T1 (`credentials`) got further than in any earlier
run, then failed in its first access check (new finding R19-1). T2 to T11
did not run. Phase 11 took the guard-SIGTERM branch, and restoration was
clean.

**What held:**
- **Phase 1a:** both modules unloaded (fusefs id 36, fdescfs id 35).
- **Phase 3:** the pod rootfs is `34a71569…` as predicted.
- **Phase 4 step 1a** qualified the **new daemon `bb4ad73c…`** (uid/gid 0,
  groups `[0,0,5]`, pid 88454).
- **Phase 5:** nightly gate GO at 16:11Z and 16:14Z.
  - The estimate was short by 1.28 GB. As in r18, the swap clearing left
    0.99 GB of laundry pages, which the estimate doesn't count.
  - The UMA drain freed 0.78 GB.
- **Phase 6** passed with a **0.37 GB margin** (15,138,096,336 bytes
  reclaimable) and swap at 0.
- **Phase 8:**
  - READY came 33 s after launch (16:15:05Z → 16:15:38Z).
  - Both signatures were made, mounts 395 and 444 came up with the exact
    tuple, and `usable` passed (L=2).
- **Phase 9:** OBSERVE at L+1=3 passed, with two mounts, `Enforcing` and no
  hfs block line.
- **984 timing:** OBSERVE net median 0.548 s, max 0.674 s.
- **R18-1 is fixed (vf4).** yf4's named events show `fchown(group,
  41002:42002)` returning errno 0, followed by `fchmod 0640` and the 12-byte
  `pwrite`. No `d_set_d_op` WARNING appeared either: the guest log has no
  WARNING or BUG line at all.
- **wf4 works.** Every EXPECTED_TARGET recall in T1 was **DOMAIN** (5.2 to
  6.7 s); only the two CALL_CONFLICT recalls (MKDIR and CREATE) were ALL.
- **yf4 works:**
  - The failure names its syscall.
  - The case-failure block ran exactly as written: `N=5`, then a direct
    OBSERVE at sequence 5, which was OBSERVED.
  - So the T1 recall history was captured without any deviation from the
    sheet.
- **R13-1 stays fixed:** both unmounts returned `rc 0` immediately after
  the guard stop.
- **Cleanup:** no fence, `runtime.stderr` empty, no AVC records, no
  relabel. U2 removed the test kernel.

| Phase | Result |
|---|---|
| 0b | swap cleared (Paul's approval) |
| 0-1, 1a | passed |
| 2-7 | passed (new daemon qualified, UMA drain run, Phase 6 margin 0.37 GB) |
| 8 | passed (READY 33 s after launch, `usable` L=2) |
| 9 | passed |
| T1 | **fail** (R19-1) |
| T2-T11 | unrun (T5 deferred) |
| 11-12 | completed (guard-SIGTERM branch) |

## R19-1: the access child's open gets EAGAIN from the guest's BLOCKED retry limit

### What the evidence shows

**T1:** budget GO, baseline OBSERVE at sequence 4. `probe` started at
16:17:07Z and ended after 96.0 s with `dispatching-uncertain`.

**The probe's events, in order:**
1. `admitted` at +8.1 s;
2. `mkdir-exclusive`, `open-leaf` and `fchmod-leaf` 0777;
3. `create-exclusive group`, `fchown 41002:42002`, `fchmod 0640` and
   `pwrite 12`, all errno 0;
4. then the `credentials_groups` access child (uid 41001, groups
   `[42001, 42002, 42003]`), which opens `group` read-only through the leaf
   descriptor. It exited with status 256: `BlockingIOError` errno 11
   (EAGAIN) on `group`;
5. the parent then refused the case: `supplementary group access`.

**Guest log:**
- Several bursts of `observation VIEW_ACQUIRE BLOCKED by the host (attempt
  N); retiring, then retrying after 52/100/200 ms` for m0.
- At uptime 207.45 s, `VIEW_ACQUIRE BLOCKED, retry limit reached after 4
  attempts (-EAGAIN)`. Its four attempts took about 0.43 s in all.
- Earlier bursts got their grant on attempt 1 or 2.

**Host recall history** (ce4, `recalls-t1.json`). All ten completed, and
the longest ACK block was 0.17 s. The first two are the bootstrap releases;
these are T1's:

| Start (Z) | Trigger | Scope | Duration |
|---|---|---|---|
| 16:17:19.1 | CALL_CONFLICT/MKDIR | ALL | 13.1 s |
| 16:17:32.9 | EXPECTED_TARGET/VIEW_ACQUIRE | DOMAIN | 6.7 s |
| 16:17:42.9 | RELEASE/VIEW_RELEASE | DOMAIN | 7.0 s |
| 16:17:50.2 | EXPECTED_TARGET/VIEW_ACQUIRE | DOMAIN | 6.6 s |
| 16:18:00.2 | EXPECTED_TARGET/VIEW_ACQUIRE | DOMAIN | 5.2 s |
| 16:18:08.3 | CALL_CONFLICT/CREATE | ALL | 14.7 s (WAIT_DRAINED 6.0 s) |
| 16:18:23.8 | EXPECTED_TARGET/VIEW_ACQUIRE | DOMAIN | 6.2 s |
| 16:18:33.7 | EXPECTED_TARGET/VIEW_ACQUIRE | DOMAIN | 5.6 s |

### Analysis

The analysis was done offline against the r19 kernel source and the
decoded `viewcontrol-v1.db` and forwarder ledgers. The guest clock is UTC
minus 16:15:12.27: every first BLOCKED line falls 30-70 ms after an
EXPECTED_TARGET recall ends.

**1. The host answered BLOCKED as designed: an expected-target mismatch,
not a shortage of tickets.**
1. The child's ticket #1 carried the target from before the fchown.
2. `acquire` (`tickets.go:366-368`) → `expectedMismatch` started a DOMAIN
   recall and answered PENDING (`:393-400`).
3. The guest polled VIEW_QUERY inside its 30 s exchange budget for the
   recall's 5.6 s.
4. After the REOPENED ACK, re-admission still mismatched the old
   expectation, so the answer was BLOCKED (`:387-389`). This is the
   documented "BLOCKED for the old expectation", and
   `zq3_mismatch_replay_test.go:52-64` encodes it.

Capacity is ruled out:
- Only about 5 of the 16/24 tickets were unretired.
- A capacity BLOCKED comes from `grantLease` on first admission, never
  after a recall.
- The ledger shows the child's family (`f8e8b0915efb`) with only ticket #1
  ever sent. Its BLOCKED is forwarder cell 2880 (fsuid 41001, groups
  `[42001,42002,42003]`).

**2. The guest gave up on its own retirement lock, not on the host.**
- re4's `hfs_mmap_blocked_backoff` (patch 0004 ~6134-6140,
  `fs/9p/hfs_mmap.c:2186-2200`) retries a BLOCKED only after retirement
  progress.
- When `hfs_view_retire_try` finds the retirement mutex busy (`-EBUSY`) and
  no floor has advanced yet, it does `attempts->blocked++` and pauses again.
  So "attempt 2/3/4" were local loops: each followed its 100/200 ms pause
  by only about 3 ms, with no host round trip.
- The mutex was held by the retirement round that every REOPENED ACK
  forces (`hfs_mmap_retire_kick(view, true)`). That round was armed by an
  earlier VIEW_RETIRE the host had refused during a recall (cell 2671,
  `through` 17 → BLOCKED, floor 14; the ue4 condition).
- In the bursts at guest times 147, 173 and 197 s the round finished
  within 52 ms. At 164 s it took about 400 ms and got through on the last
  allowed pause. At 207 s no floor advanced within the 352 ms total pause,
  so the guest returned `-EAGAIN`. The final ledger has every floor equal
  to High: the progress came, just too late.
- So the 5-15 s recalls themselves were not the problem; they are spent
  inside the PENDING poll. The bugs:
  - the progress gate is right for a capacity BLOCKED but wrong for an
    expected-target BLOCKED, where the view has just been reinstalled;
  - a running round counts as a failure;
  - the host's BLOCKED carries no reason;
  - the guest's log line names every local pause "BLOCKED by the host".

**3. Even with that fixed, T1 does not fit its deadline yet.** Measured in
this run:
- an observation after a mutation costs about 6 s (one DOMAIN recall);
- mkdir plus open of a directory about 43 s, a file create about 31 s.

T1's remaining steps work out as follows:
- the group-tail case, setgid, sticky, special bits and the `retain()`
  inventory add about 245-295 s;
- T1 had already used 92 s;
- together that is about 340-385 s against the guest's 240 s deadline
  (255 s dispatch).

### Fix

- **1h4 (required, guest kernel): R19-1 fix.**
  - An expected-target BLOCKED, where the view reopened or changed target
    since capture, is retried at once as a re-check, bounded by the
    deadline and not counted against the cap.
  - `-EBUSY` waits for the running round (killable, bounded) instead of
    counting.
  - Separate log lines for a host BLOCKED and a local busy pause.
  - Tests in `tests/pressure`.
- **2h4 (daemon diagnostic):** a per-ticket BLOCKED reason (expected
  mismatch vs capacity) in OBSERVE or the recall history.
- **xf4 (annotated):** grant without a recall when only the requester's own
  calls moved the cut. Without it, or a larger or split T1 deadline, T1
  cannot finish even after the guest fix.

zq3-r20 needs a kernel rebuild. The daemon `bb4ad73c…` and exporter
`2fd7e32f…` can be reused unless the diagnostic or xf4's host side lands
first.

## Host events and facts

- **f3:** boot `1794574308` unchanged.
- **N1:** stamp `2026-11-21`, newest dnf transaction #65.
- **Rocky before the run:**
  - PID 59685 (UTC lstart `Sat Nov 21 11:58:43 2026`), boot `c1dc3905-…`;
  - stopped cleanly at about 16:14Z.
- **Rocky after the run:**
  - started at 16:21:04Z, SSH answered at 16:21:20Z;
  - PID 93547, boot `8360a500-f078-4607-8fa5-bd9a10e84a4a`;
  - down about 7 minutes, no relabel.
- **Run values:**
  - Phase 7: volume `6f8f656e-…`.
  - After READY: B is `cc0c5953…`, U is `a08e97f0…`, L=2.
  - Observer EA `e1c9a865…`, removed (`restored_pre_run: true`).
- **Guard:**
  - one SIGTERM at 16:19:44Z; the guard was gone by 16:19:54Z;
  - `runtime_exit 0`; exporter pid 90399;
  - peaks: disk 24.0 MB, exporter RSS 237 MB, runtime RSS 44 MB.
- **Final f3 state:**
  - Rocky running, `/dev/vmm` holds only `rocky`;
  - no fuse or fdesc mount; `fdescfs.ko` and `fusefs.ko` loaded, as the
    reviewed plan allows;
  - iom1 to iom3 stopped.
- **Afterwards:** the iormesh session was told that f3 is free, and
  started zg4 on iom1 with memory guards.
- **Fill-record note** (prose only, no tool reads it): the r19 `pins.json`
  wording about `template_derived_from` and guest.py being "unchanged" is
  wrong. guest.py changed `48d6ae81` → `005d9592` (yf4) since the r18 fill,
  and the template's previous candidate was `b2a4f69e`.

## Follow-up tasks

- **1h4:** the R19-1 guest kernel fix (expected-target BLOCKED re-check, and
  busy no longer counting as a failure). Then a kernel rebuild and zq3-r20.
- **2h4:** a per-ticket BLOCKED reason in OBSERVE (daemon diagnostic).
- xf4: recall-free own cut moves. Required for T1 to finish inside its
  deadline (estimate above).
- Still open: tg4, 5f4, ue4, 0a4, 2a4, 094, a84, l64, s54, s64, k84, qc4.
