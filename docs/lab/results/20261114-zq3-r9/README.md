# zq3-r9: R8-1 fixed; the final usable probe parks behind a refused QUERY resume (2026-11-14)

Run sheet: [filled plan](../20261114-zq3-r9-filled-plan/run-sheet.md).
Candidate l84 `e585206a…` ([xz3-20261114-r9](../../../../tools/integration/artifacts/evidence/xz3-20261114-r9/README.md)):
the reused g64+m64 guest kernel (build ID `870bb9d1…`), the daemon
`868dc045…` built at `e92eaa1` (784, 884, d84, g84, h84, b84, q64/f84) and
the reused native exporter `ef8f17de…`. T5 stayed deferred. Times are UTC.
Private evidence: `/home/paul/tmp/claude/hfs-zq3-r9-fill/evidence` (run log
`RUN-LOG.md`).

## Outcome

**Milestones (first times):**
- Phase 1a unloaded the two modules zq3-r8 left loaded (normal `kldunload`,
  both rc 0).
- Phase 4 step 1a qualified the l84 daemon `868dc045…` as root.
- READY with every step `ok`, both signatures by the watcher, both mounts
  with the exact g64 tuple.
- **zq3-r8's failure (R8-1) is fixed.** The b84 `usable` gate's first probe
  passed, and its baseline OBSERVE (sequence 1) showed both providers with
  no open claim: the first probe's GETATTR #3 and PROCESS_EXIT #4 TERMINAL,
  two claims retired.

**New failure (R9-1):** the gate's final probe, about 40 s later, hung on
both roots (`lstat` did not return within 8 s; guest stacks `hfs_getattr` →
`hfs_call` → `hfs_request_run` → `hfs_control` → `hfs_transport`). The guest
then blocked both sessions:

```
[  113.011783] 9p-hfs: session 1c31c3e7-… blocked by QUERY timeout on GETATTR (-110)
[  122.026121] 9p-hfs: session 10203c44-… blocked by QUERY timeout on GETATTR (-110)
```

| Phase | Result |
|---|---|
| 0-1 | passed (facts as at 16:04Z) |
| 1a | passed: fusefs and fdescfs unloaded normally |
| 2-3 | passed: staging, kernel install, observer stage, pod rootfs `f7f69339…` |
| 4 | passed: process observer `868dc045…`, window plan `f0efa50c…`, FSID `5900ff03:00000059`, fusefs, observer key |
| 5-7 | passed: rechecks, UMA drain, Phase 6 margin 0.58 GB, tap100 |
| 8 | READY; `usable` first probe and baseline ok; **final probe failed** (R9-1) |
| 9, T1-T4, T6-T11 | unrun (`fill_plan.py` refuses a failed `usable`); T5 deferred |
| 11-12 | completed |

Rocky was stopped from 16:51:34 to 16:55:47, about 4 minutes. Phase 11 took
the guard-SIGTERM branch in the mandated order: live owner snapshots before
and after the blocks; one SIGTERM to the guard (PID 54577, gone after about
20 s, `runtime_exit` 0); evidence tars; m0 and m1 unmounted normally (rc 0);
observer line removed (`restored_pre_run: true`); `next_entry` empty; no AVC
records; full guest `dmesg` saved; poweroff (child gone after about 10 s);
host cleanup (tap and vmm destroyed, original vars `6de05da2…` unchanged);
the native FUSE mount unmounted normally; the f3 observer key deleted with
the plain literal line; window restore `mount_restored: true`.

Phase 12: Rocky started normally (PID 56842, UTC lstart `Sat Nov 14 16:55:47
2026`, boot ID `00e96c71-…`), SSH after about 20 s, stock kernel,
`Enforcing`, `authorized_keys` `e08542dc…`, no relabel reboot (one vm-bhyve
run). The test kernel was removed (U2). fdescfs and fusefs stay loaded on
f3. The 984 timing measurement (Phase 9 onwards) produced no sample, because
Phase 9 never ran.

## Root cause

Decoded offline from both owner snapshots (decoders in
`/home/paul/tmp/claude/r9-analysis/`; repro test there).

**What the ledgers show (m0; m1 is the same about 8 s later).** At the
baseline OBSERVE the controller had no recall, lease, ticket or source and
the admission was not fenced. The final probe's VIEW_ACQUIRE was GRANTED
(no expected-target mismatch; the lease targets the OBSERVE's cut). Its
GETATTR #5 was answered ADMITTED/OK but provider claim #5 stayed
`RESERVED/UNPREPARED` in both snapshots. While the GETATTR was in flight the
lstat child released that lease: a scoped recall of m0 alone ran STOPPED to
REOPENED and finished well inside the guest's window. Every later QUERY
still answered INDETERMINATE.

**Why (R9-1).**
1. *Trigger (guest, inferred):* when a normal-lane exchange waits more than
   50 ms, `hfs_transport` (patch 0003) calls `hfs_mmap_progress`, which
   queues `hfs_mmap_pressure` (patch 0004). That releases the published
   observation's lease, including the one the waiting GETATTR holds. No
   other release path can drop an in-flight observation.
2. *First admission refused:* the release's recall holds the domain's reads
   (`recall_scope.go` `recallHoldsCall`), or `TryLock` lost to it;
   `Admit` returned WouldBlock and `guestbridge/service.go` answered
   ADMITTED (as designed).
3. *Every resume refused:* each QUERY resumes through
   `recoverValidation` → `Controller.Admit` → `Facts.Authorize`
   (`controlledruntime/facts.go`). The outer request is a QUERY, so it needs
   a continuation token, and `cleanupQueryContext`
   (`controlledvalidation/cleanup_continuation.go`) mints one only for
   DESCRIPTION_RELEASE and FORGET. A GETATTR gets PERMISSION on every QUERY
   until the guest gives up. This contradicts
   `docs/spec/Retained-9p-view-leases.md` ("ordinary QUERY … may resume").

Refuted: an expected-target mismatch (e84), a leftover recall from the first
probe, the b84 OBSERVE fencing the admission or holding `refreshMu`, and a
q64 recycling side effect. zq3-r8's stuck PROCESS_EXIT and GETATTR could
not resume either, so this bug probably also made R8-1 permanent once any
first admission failed.

Confidence: high that the refused QUERY resume parked the claims (reproduced
offline: one WOULD_BLOCK at CALL time, then every resume PERMISSION);
medium-high that guest pressure was the trigger. Unknown: which of the two
causes refused the first admission.

**Why offline gates missed it.** The 984 model releases the probe's lease
only after its GETATTR is TERMINAL, so every modelled CALL was admitted on
the first try and QUERY resume never ran. The viewcontrol resume tests use a
fake `Authorize` that ignores whether the outer request is a QUERY.

Even with the host fixed, the recall moves the view under the GETATTR (fence
and revision 2→3 in the model), so the guest's `hfs_attr_install` would
return ESTALE: the guest's pressure path must not release a lease whose
observation still has a request in flight.

## Follow-up tasks

- o84 (host): let a QUERY resume any UNPREPARED claim, not only
  DESCRIPTION_RELEASE/FORGET; make the viewcontrol fake `Authorize`
  distinguish QUERY from CALL; 984 extension reproducing R9-1.
- p84 (guest kernel): the pressure path must not release a lease whose
  observation still has a request in flight; needs a kernel rebuild and a
  new candidate; model the 50 ms pressure release in the 984 model.
- a84 (raised): record the last swallowed refusal class on the claim.
