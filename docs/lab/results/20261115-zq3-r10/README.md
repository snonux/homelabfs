# zq3-r10: past the usable gate and Phase 9; T1 blocked by a guest-local release refusal (2026-11-15)

Run sheet: [filled plan](../20261114-zq3-r10-filled-plan/run-sheet.md).
Candidate r84 `4712e479…` ([xz3-20261114-r10](../../../../tools/integration/artifacts/evidence/xz3-20261114-r10/README.md)):
the p84 guest kernel (build ID `d73c85da…`), the daemon `34a5360b…` (o84,
q84 on top of the r9 code) and the reused exporter `ef8f17de…`. T5 stayed
deferred. Times are UTC. Private evidence:
`/home/paul/tmp/claude/hfs-zq3-r10-fill/evidence` (run log `RUN-LOG.md`).

## Outcome

**Milestones (first times):**
- The b84 `usable` gate passed: first and final probes on both roots, the
  baseline and the claims phase. zq3-r8 and zq3-r9 failed here, so o84, p84
  and q84 did their job.
- Phase 9 passed: OBSERVE sequence L+1 = 3 with both providers `Open 0`,
  both mounts with the exact g64 tuple, SELinux `Enforcing` on the test
  kernel, no guest block line.
- **First 984 timing samples:** SSH no-op median 0.198 s; OBSERVE (one fresh
  guest observation) net 0.605 s and 0.434 s. The offline model's bound is
  about 2.4 s per observation, so the lab has a wide margin.
- T8 held: m1 unmounted normally, the guest lock probe refused at admission
  ("Refused: exact mountpoint, 9p source and mount ID required", exit 10),
  and the directory was empty.

**New failures:**
- **R10-1:** T1 `credentials` hit the guest's 10 s case deadline during
  admission (`probe` refused after 16.9 s, case `dispatching-uncertain`),
  and the guest blocked m0:
  `[146.714178] 9p-hfs: session 5c69f97f-… blocked by DESCRIPTION_RELEASE failure on DESCRIPTION_RELEASE (-11)`.
- **R10-2:** T8's normal unmount of m1 logged
  `[208.842946] 9p-hfs: session 8f7f94c7-… blocked by failed provider stop on STOP (-16)`
  (the unmount itself returned 0).

| Phase | Result |
|---|---|
| 0-1, 1a | passed; fusefs and fdescfs unloaded normally |
| 2-7 | passed: staging, kernel install, process observer `34a5360b…`, window `5900ff05:00000059`, Phase 6 margin 0.57 GB |
| 8 | **READY and `usable` passed** (L = 2) |
| 9 | **passed** |
| T1 | **fail** (R10-1); controller in error |
| T2-T4, T6, T7, T9-T11 | unrun |
| T5 | unrun (deferred, re-plan pending) |
| T8 | **holds** |
| 11-12 | completed |

Rocky was stopped from 08:15:20 to 08:21:01, about 6 minutes. Phase 11 ran
the guard-SIGTERM branch in the mandated order (guard gone after about 20 s,
`runtime_exit` 0; m0 unmounted normally; observer removed,
`restored_pre_run: true`; no AVC records; host cleanup; native FUSE unmount;
the plain literal key deletion; window restore `mount_restored: true`).
Phase 12: Rocky PID 38959 (UTC lstart `Sun Nov 15 08:21:01 2026`, boot ID
`374eff4c-…`), stock kernel, `Enforcing`, `authorized_keys` `e08542dc…`, no
relabel reboot; test kernel removed (U2). fdescfs and fusefs stay loaded.

## Root cause

Decoded offline from both owner snapshots (`/home/paul/tmp/claude/r10-analysis/`).

**R10-1 was guest-local; the DESCRIPTION_RELEASE never reached the host.**
m0's provider stopped at claim 9 (High 9, Floor 9), with no claim #10. Claim
#9 is T1's OPEN of the m0 root, made during the case's admission: it was
answered ADMITTED, parked behind about three recalls (m0's fence 3→6, the
last one all-domain), resumed by o84's QUERY continuation and ended TERMINAL
OK with a description capability. So the host side worked.
1. guest.py's `signal.alarm(10)` stays pending during the uninterruptible
   hfs wait and raises `Deadline` right after `os.open` returns, before the
   fd is registered (guest.py ~236-237), so the fd leaks and closes only at
   process exit.
2. In `do_exit`, the PROCESS_EXIT work is queued before `exit_files`; its
   `hfs_call` sets `session->allocation_pending`.
3. The final `fput` reaches `hfs_release`, whose `hfs_call` gets an
   immediate local `-EAGAIN` because another allocation is pending (patch
   0003 ~2311/~2372), and `hfs_release` treats every error as uncertain and
   blocks the session.
Confidence: high that the host was not involved, high for the leak and the
exit ordering, medium-high that `allocation_pending` produced the -11.

**R10-2 is independent of R10-1.** `hfs_controlled_stop` (patch 0005)
accepts only an UNBOUND or quiescent view, but after any recall's REOPEN the
view is LIVE and not quiescent, and the check runs before `kill_anon_super`
and the flush. So every clean unmount of a once-reopened mount logs this
line. Earlier runs never showed it because their sessions were already
blocked (one report per session). Functionally harmless (unmount rc 0).
Confidence: high.

**Also found:** any process that exits with an open hfs fd, and any
same-mount concurrency, can hit the same local `-EAGAIN` and block the
session (the pod workload will). Every observation release costs a recall,
sometimes across both domains, so a syscall's first admission can wait
seconds; that made T1's admission slow enough to meet the 10 s alarm.

**Why offline gates missed it:** the 984 model replays only mount, usable
and settle traffic, opens no files and has no guest allocation fence or
SIGALRM; the mount_hello stop fixture only models an UNBOUND quiescent view.

## Follow-up tasks

- x84 (guest 0003): a request refused only because another allocation is
  pending waits (bounded) instead of failing; at least for the cleanup
  opcodes. Kernel rebuild.
- y84 (guest.py): block SIGALRM around `os.open` + registration; emit each
  admission step.
- z84 (guest 0005/0003): run the unmount stop check after
  `kill_anon_super` + flush and accept a LIVE view with zero activity.
  Kernel rebuild.
- 094 (984 model): replay guest.py cases (credentials admission with
  recalls, then exit with a release); measure per-syscall admission latency.
