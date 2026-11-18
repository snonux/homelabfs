# zq3-r11: usable and Phase 9 pass again; T1's root OPEN parks behind a recall wedged by an early DRAINED refused as STALE (2026-11-18)

Run sheet: [filled plan](../20261118-zq3-r11-filled-plan/run-sheet.md).
Candidate b94 `3b128cc6…` ([xz3-20261117-r11](../../../../tools/integration/artifacts/evidence/xz3-20261117-r11/README.md)):
the x84/z84 guest kernel (build ID `c4e08f6f…`), the daemon `bf86d805…`
(194 on top of the r10 code) and the reused exporter `ef8f17de…`. T5 stayed
deferred; **T7 was skipped** (`pod_fixture.py` pins the pre-EL9.9
podman/crun package versions; task e94). Times are UTC. Private evidence:
`/home/paul/tmp/claude/hfs-zq3-r11-fill/evidence` (run log `RUN-LOG.md`).

## Before the run

Rocky's nightly `dnf -y upgrade` (00:55 Rocky-local) had installed the EL9.9
update (kernel `5.14.0-741.22.1.el9_9`, new python3.9, podman, crun) on
2026-11-17 without a reboot, so the running kernel (687.66.1) and the grub
default differed and the kernel install would refuse. Rocky was cleanly
restarted with `tools/lab/vms.sh` at 08:21, the facts retaken and the fill
partly regenerated (option S1-a).

## Outcome

**What held:**
- READY, the b84 `usable` gate and Phase 9 passed again (claims `Open 0`,
  exact mount tuple, SELinux `Enforcing`, no block line).
- 984 timing: about 0.47 s per fresh guest observation (bound about 2.4 s).
- zq3-r10's two failures did not recur: no exit-time release refusal (R10-1)
  and no block line on the clean m0 unmount (R10-2).
- The new y84 admission-step events showed exactly where T1 stalled.

**New failures:**
- **R11-1:** T1 `credentials`: `open-root` of m0 at guest 103.6 s, then
  nothing until the 10 s alarm fired at 147.2 s (the OPEN took 43.6 s);
  the guest blocked m0:
  `[147.258912] 9p-hfs: session a112c1e3-… blocked by QUERY timeout on OPEN (-110)`.
  The probe refused after 44.7 s (`dispatching-uncertain`).
- **R11-2:** T8's normal `umount` of m1 never progressed: it stayed in D in
  `hfs_view_enter` (the stat of the mount point) until `rockyt`'s 150 s
  bound; T8 unrun. The guest poweroff later took about 530 s.
- **Operator slip:** my chained T8 command also started the lock probe after
  the unmount had timed out, against the sheet's order; it only stats and
  changed nothing.

| Phase | Result |
|---|---|
| 0-1, 1a | passed |
| 2-7 | passed (process observer `bf86d805…`, Phase 6 margin 0.50 GB) |
| 8, 9 | **passed** (L = 2) |
| T1 | **fail** (R11-1) |
| T2-T4, T6, T9-T11 | unrun |
| T5 | unrun (deferred) |
| T7 | unrun (skipped, e94) |
| T8 | **unrun** (m1 unmount stuck, R11-2) |
| 11-12 | completed |

Rocky was stopped from 08:38:38 to 09:01:00 (about 22 minutes; the stuck
guest shutdown). Phase 11 ran the guard-SIGTERM branch in order (guard gone
after about 20 s, `runtime_exit` 0; m0 unmounted normally; m1 stayed behind
its stuck unmount; observer removed; no AVC records; poweroff; host cleanup;
native FUSE unmount; plain literal key deletion; window restore
`mount_restored: true`). Phase 12: Rocky PID 3146 (UTC lstart `Wed Nov 18
09:01:00 2026`, boot ID `c8f80bb2-…`), kernel 741.22.1, `Enforcing`, no
relabel reboot; test kernel removed (U2).

## Root cause

Decoded offline (`/home/paul/tmp/claude/r11-analysis/`; offline repro
against the real controller in a scratch copy).

1. **guest.py's read-only root OPEN started an all-domain recall itself.**
   The OPEN runs under the caller's own DOMAIN READ observation lease, and
   OPEN is not a `readOperation` (`internal/viewcontrol/admission.go`), so
   `conflicts(false)` matched that lease and started a recall of both
   domains.
2. **The host answered m0's early DRAINED with STALE.** The guest sends
   DRAINED as soon as its own INSTALLED_DRAIN is accepted; the recall was
   still waiting for m1's INSTALLED_DRAIN, and `checkACKPhase`
   (`recall.go`) returns Stale whenever the phase is not WAIT_DRAINED. The
   guest maps that to `-ESTALE` and stops its view worker for good
   (`view control worker stopped, error -116` at 116.7 s), blocking m0. The
   spec requires BLOCKED for an ACK that may become acceptable unchanged;
   the offline repro shows the identical bytes are ACCEPTED later.
3. **The recall then waited forever in WAIT_DRAINED** (controller state
   byte-identical across all four snapshots, about 9 minutes): the OPEN
   stayed RESERVED/UNPREPARED and every o84 QUERY resume was held until the
   guest's deadline (R11-1), and m1's view stayed in DRAINED waiting for an
   INSTALL_FINAL that never came, so `hfs_view_enter`, which has no time
   bound, kept m1's stat and unmount in D (R11-2). Nothing on the host gives
   up on a participant that stops acknowledging.

Confidence: high for the self-conflict, the STALE early DRAINED and the
wedged recall; medium-high that the `-116` is exactly this STALE. Not
explained: why the OPEN took 43.6 s against the 30 s QUERY window (no
timestamps in the ledgers).

**Why offline gates missed it:** the 984 model never sends a non-read CALL
under an observation lease, and both it and the viewcontrol test helper
send every INSTALLED_DRAIN before any DRAINED, which never produces the
early DRAINED.

## Follow-up tasks

- y94 (viewcontrol): answer an early DRAINED with BLOCKED, not STALE; send
  ACKs in the guest's order in tests and the 984 model.
- z94 (viewcontrol): a read-only OPEN conflicts like a read.
- 0a4 (viewcontrol): fence and drop a participant that stops acknowledging
  after STOPPED with nothing left to drain.
- 1a4 (guest 0004): bound `hfs_view_enter`'s wait; log the session in
  `hfs_mmap_fail`.
- 2a4 (984 model): replay the credentials admission, guest ACK order and a
  blocked-domain case.
- e94 (pod_fixture): package pins from the run facts, so T7 can run.
