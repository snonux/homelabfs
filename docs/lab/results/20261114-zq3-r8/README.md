# zq3-r8: READY with the exact mount tuple; both sessions blocked by unanswered QUERYs (2026-11-14)

Run sheet: [filled plan](../20261110-zq3-r8-filled-plan/run-sheet.md).
Candidate y64 `867ede36…` ([xz3-20261110h](../../../../tools/integration/artifacts/evidence/xz3-20261110h/README.md)):
the g64+m64 guest kernel (build ID `870bb9d1…`), the daemon `34fb05b2…`
(i64, o64, r64, n64, j64, u64, v64, x64) and the native exporter
`ef8f17de…` (564, e64, p64, w64). T5 was deferred before the run (z64,
q64). Times are UTC. Private evidence:
`/home/paul/tmp/claude/hfs-zq3-r8-fill/evidence` (run log `RUN-LOG.md`).

## Outcome

**Milestones (all first times):**
- Phase 4 step 1a qualified the y64 daemon `34fb05b2…` on f3 (root, groups
  `[0, 0, 5]`), and the first-ever run of the native exporter `ef8f17de…`
  launched the guest and served both mounts.
- Both mounts showed the exact g64 tuple
  `rw,cache=mmap,msize=65536,trans=virtio,version=9P2000.L.hfs1.4`, so R7-1
  is fixed.
- m64's diagnostics worked: the guest logged the reason for each blocked
  session, where zq3-r7 blocked silently.
- READY again 35.9 s after launch, with every bootstrap step up to READY
  `ok` and both signatures by the watcher exiting 0.
- The k64 `usable` gate did its job: it stopped the run before the batch
  fill, so no case ran against a broken mount.

**New failure (R8-1):** `usable` failed at its first probe. m0's root
`lstat` succeeded (mode `040700`, uid 0). m1's root `lstat` did not return
within 8 s: the guest process sat in `hfs_request_run` ← `hfs_call` ←
`hfs_getattr`. The guest then blocked both sessions:

```
[   74.977856] 9p-hfs: session 5b822444-… blocked by QUERY timeout on PROCESS_EXIT (-110); later operations on this mount fail with EACCES or EHOSTDOWN
[   85.074189] 9p-hfs: session 11154906-… blocked by QUERY timeout on GETATTR (-110); later operations on this mount fail with EACCES or EHOSTDOWN
```

`5b822444` is hfs0 (m0) and `11154906` is hfs1 (m1). The host never
answered these claims within the guest's 30 s QUERY window. zq3-r7's cause
(Sources=2, NoSpace) is gone, so this is a different wall; see
[Root cause](#root-cause).

| Phase | Result |
|---|---|
| 0-1 | passed (facts unchanged since 07:57Z; `10.20.0.133` on `vm-public`; Phase 1a skipped, modules absent) |
| 2-3 | passed: staging, kernel install, observer stage, pod rootfs `f7f69339…` |
| 4 | passed: process observer `34fb05b2…`, window plan `9b43db0d…`, FSID `5900ff01:00000059`, fusefs, observer key |
| 5-7 | passed: rechecks, UMA drain (+0.19 GB free), Phase 6 margin 0.52 GB, tap100 |
| 8 | READY (35.9 s, both mounts, exact tuple); **`usable` failed** (R8-1) |
| 9, T1-T4, T6-T11 | unrun: `fill_plan.py` refuses a run whose `usable` failed; T5 deferred (q64) |
| 11-12 | completed (below) |

Rocky was stopped from 08:42:23 to 08:47:16, about 5 minutes. Phase 11 took
the guard-SIGTERM branch in the mandated order:
1. Live owner snapshots before and after the blocks
   (`p8-owner-live.tar`, `p8-owner-after-block.tar`), read-only, while the
   runtime served.
2. One SIGTERM to the guard (PID 10903). The guard stopped the runtime
   cleanly (`runtime_exit` 0, guard gone after about 20 s); its breach record
   lists only that SIGTERM.
3. Evidence tars; m0 and m1 unmounted normally with `rockyt` (both rc 0,
   9p count 0).
4. Observer line removed (`restored_pre_run: true`, `e08542dc…`);
   `next_entry` empty; no AVC records; full guest `dmesg` saved (step 4b).
5. Guest poweroff; the child was gone after about 10 s.
6. Host cleanup (tap and vmm destroyed, original vars unchanged
   `49bdc1ef…`); native FUSE mount unmounted normally.
7. f3 observer key deleted with the plain literal command.
8. Window restore (`mount_restored: true`; fusefs kept by the reviewed
   plan).

Phase 12: Rocky started normally (PID 13234, UTC lstart `Sat Nov 14
08:47:16 2026`, boot ID `83c76231-…`), pinned SSH after about 20 s, stock
kernel, `Enforcing`, `authorized_keys` `e08542dc…`, **no relabel reboot**
(one vm-bhyve run, `/.autorelabel` absent). The test kernel was removed
(U2). fdescfs and fusefs stay loaded on f3 until its next boot. Staging on
f3 and the run directories on Rocky are kept as evidence.

## Host events before the run

- 2026-11-13 12:51:48 UTC: f3 reboot, done deliberately by Paul.
- f3's address `10.20.0.133` moved from `re0` to the `vm-public` bridge
  (confirmed read-only at 08:36Z and again in Phase 1). Nothing in the run
  depended on `re0`; the observer key's `from="10.20.0.133"` still matched.
- f3's `libpython3.12.so.1.0` changed (`786c2283…` → `6fc7b9b0…`, a Python
  package update on 2026-11-10); the launcher binary `a66eb370…` is
  unchanged. The window plan takes live hashes, so nothing pinned the old
  one.
- The 2026-11-10 f3 reboot, Rocky's self-reboots (2026-11-10 19:32 and
  2026-11-11 10:08 UTC) and Rocky's stops and starts on 2026-11-11 to 13
  have no recorded cause.

## Root cause

Decoded offline from both owner snapshots (scratch decoders in
`/home/paul/tmp/claude/r8-analysis/`). It is not a capacity wall (1 of 10
`Sources`, 2 of 128 claims per provider, credits fine), not lost forwarding
(the host answered every forwarded cell) and not a guest-kernel bug.

**What the ledgers show.** Both blocked claims are `RESERVED` /
`UNPREPARED` with credit charged. Their CALLs were answered ADMITTED/OK and
every later QUERY INDETERMINATE:
- m0: the PROCESS_EXIT of the `usable` probe's own `lstat` child
  (provider-0 claim #4). That child's GETATTR (claim #3) is TERMINAL OK, so
  the probe's "m0 ok" was true for the `lstat` but not for the process exit,
  and the exit is what blocked m0.
- m1: the GETATTR on the root (provider-1 claim #3).

**Why they never moved (R8-1).** A view recall (`62f76f3c`, phase REOPEN,
m0's REOPENED ack still executing) was active while both claims arrived.
- `internal/viewcontrol/admission.go:87` returns WouldBlock for every CALL
  while any recall exists. `internal/guestbridge/service.go:513` swallows
  that and answers ADMITTED/OK, and the claim only advances on a later QUERY.
- Recalls are far too frequent. The fence counters (m0 5, m1 4) and
  AckArchive (34, then 35) match four full recalls between the mounts and
  the probe:
  - every short-lived process that touches a mount takes a DOMAIN READ lease,
    and releasing it starts a full five-phase recall across **both** domains
    (`tickets.go:364-373`): m0's and m1's `mount(8)` and the probe's m0
    child;
  - a PROCESS_EXIT also starts one whenever a peer domain holds an active
    lease, because it is not a `readOperation` (`admission.go:90`, `:292`).
- Each recall is slow. Every VIEW_ACK runs a synchronous SSH observation of
  the guest (`internal/controlledruntime/producer.go:93`) on the single
  shared control worker (`internal/guestbridge/endpoint.go:216`), and
  `Admission.Refresh` holds its lock across the SSH call
  (`guest_observer.go:148-176`). One recall needs 10 acks, all serialized
  against the guests' QUERYs.

The guest's fixed 30 s QUERY window ran out first (m0 at about 08:44:24,
m1 at about 08:44:34). After that nothing resumes a RESERVED claim, so both
stayed stranded with their credit charged. mount-1 taking 9.4 s against
mount-0's 4.6 s fits it being parked behind the recall that m0's mount
process started.

Confidence: high for the claim states, the active recall, and both recall
triggers (each reproduced offline in a scratch copy: the claim stays
INDETERMINATE). Medium that this one recall outlasted both deadlines: the
ledgers cannot date its end, and a swallowed admission refusal leaves no
trace. Unknown: the cost of one SSH observation.

**Why offline gates missed it.** o64's zq3 workload model drives no
VIEW_ACQUIRE/VIEW_RELEASE, recalls or observation latency.

## Follow-up tasks

- 784, recall scope: PROCESS_EXIT (and the other authority reductions) must not
  conflict; releasing a clean READ lease recalls only that domain, with a
  per-domain admission barrier for READ recalls.
- 884, recall latency: VIEW_ACK uses the cached observation first, SSH only on
  `ErrNotObserved`, and never under `Admission.mu`.
- 984, workload model: count recalls, acks and observation latency per `usable`
  probe against the 30 s window.
- a84, diagnostics: record the last swallowed refusal class on the claim; release
  the credit of RESERVED claims the guest has given up on.
- b84, `usable` (k64): also require each probe child's PROCESS_EXIT to reach
  TERMINAL before declaring a root usable.
