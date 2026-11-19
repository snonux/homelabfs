# zq3-r15: READY and Phase 9 again; T1's root OPEN now succeeds, but the root fstat gets ESTALE from a recall that completed during its observation (2026-11-19)

Run sheet: [filled plan](../20261119-zq3-r15-filled-plan/run-sheet.md)
(the run log records no run-time revision).
Candidate r15 `ac8020aa…` ([xz3-20261119-r15](../../../../tools/integration/artifacts/evidence/xz3-20261119-r15/README.md)):
the z94 daemon `8668e5f0…`, running for the first time, with the oc4 guest
kernel (build ID `9b167690…`) and the pc4 exporter `2fd7e32f…` reused byte
for byte. It was also the first run with the be4 guest deadlines (admission
150 s, request 240 s). T5 stayed deferred. Times are UTC.
Private evidence: `/home/paul/tmp/claude/hfs-zq3-r15-fill/evidence` (run log
`RUN-LOG.md`), controller output
`/home/paul/tmp/claude/hfs-zq3-r15-fill/ctrl-run/credentials/`, bootstrap
journal `/home/paul/tmp/claude/hfs-zq3-r15-fill/bootstrap-run/`. Offline
decode: `/home/paul/tmp/claude/r15-analysis/` (copies only; `mine/residue.py`
reads claim records from the freed pages of the provider ledgers).

## Outcome

Phases 0 to 9 passed. T1 (`credentials`) failed during guest admission
(new finding R15-1), so T2-T11 did not run (the sheet allows no retry).
Phase 11 took the guard-SIGTERM branch, and restoration was clean.

**What held:**
- Phase 1a unloaded `fusefs` and `fdescfs` again (Refs 1/1, no mounts,
  both `rc 0`, neither listed afterwards). Phase 4 step 1a qualified the
  **new z94 daemon `8668e5f0…`** (uid/gid 0, groups `[0,0,5]`, pid 7065).
- Phase 3: the T7 pod rootfs manifest is `34a71569…`, as the fill predicted
  from the be4 `pod_workload.py`.
- Phase 5: the active-based estimate left a 0.35 GB margin and recommended
  the UMA drain; step 1b freed 34,523 pages (about 0.14 GB;
  `uma_free_bytes` 176 MB → 40 MB). Rocky stopped at 16:29:40Z. Phase 6
  passed with a 0.42 GB margin (reclaimable 15,182,863,760 bytes against a
  bar of 14,763,950,080).
- Phase 8: the watcher made both operator signatures (enroll, then bind
  3.1 s later). mount-0 (mountinfo 459) and mount-1 (405) both came up with
  the exact g64 tuple. READY came 33 s after launch (16:30:01Z →
  16:30:34Z). The `usable` gate passed (claims 1.78 s after the final call,
  `observe_sequence_last` L=2).
- Phase 9 (budget GO at 16:31:29Z, 3511 s left): three SSH no-ops, OBSERVE
  at L+1=3 passed (both domains `REOPENED`, fence 3, claims `Allocated 6,
  Open 0`), mountinfo had exactly the two tuples, `Enforcing`, the SELinux
  `Initializing` line, no hfs block line.
- 984 timing (final): SSH no-op median 0.219 s (3 samples), OBSERVE net
  median 0.483 s, max 0.491 s (2 samples), below the 2.4 s bound.
- **z94 worked as far as T1 got:** the m0 root OPEN succeeded, in 7.4 s
  instead of zq3-r14's 14.1 s, and the be4 admission alarm did not fire
  (`deadline_hit` false). T1's failure left nothing behind: no guest block
  line during T1, no transport fence (`fenced: false`), `runtime.stderr`
  empty, and the probe's close and `PROCESS_EXIT` went through.
- When the guard stopped the runtime, both sessions blocked at uptime
  152.7 s and both `umount`s returned `rc 0` in 1 s (R13-1 stays fixed).
  m0 logged `VIEW_WAIT (-116), exporter ERROR 4` and m1
  `VIEW_WAIT (-112), exporter ERROR 5`; see the note below.
- No AVC records, no relabel. U2 removed the test kernel.

**New finding:**
- **R15-1:** T1's guest admission opened the m0 root (`open-root[0]`)
  0.87 s after `identity-checked`; the open returned 7.39 s later, and the
  following `os.fstat` of that root fd (`stat-root[0]`) failed after
  7.91 s with `OSError` errno 116 (`ESTALE`). The probe ended with
  `terminal` 23.2 s after `identity-checked`, and the controller recorded
  `dispatching-uncertain` (`Refused: required actual syscall case failed:
  credentials`, 24.1 s). See the analysis below.

| Phase | Result |
|---|---|
| 0-1, 1a | passed (both modules unloaded) |
| 2-7 | passed (process observer `8668e5f0…`, nightly gate GO 16:27Z and 16:29Z, step 1b UMA drain run, Phase 6 margin 0.42 GB) |
| 8 | passed (mount-0 and mount-1 ok, READY 33 s after launch, `usable` L=2) |
| 9 | passed (OBSERVE L+1=3; 984 timing) |
| T1 | **fail** (R15-1) |
| T2-T11 | unrun (no retry after T1; T5 deferred) |
| 11-12 | completed (guard-SIGTERM branch) |

T1 started at 16:31:46Z with the guard budget GO (3495 s left). The
baseline OBSERVE at L+2=4 passed (both domains `REOPENED`, fence 3). The
probe started at 16:31:47.6Z. Phase 11 ran the guard-SIGTERM branch: the
final 984 sample, then one SIGTERM to guard 8610 at about 16:32:40Z. The
guard was gone at the second check (16:32:51Z). Receipt: `runtime_exit 0`,
only the `guard-interrupted SIGTERM` breach (159.4 s), runtime not running
at guard exit, exporter (pid 8621) still alive. Then: evidence tarballs,
the fence record (`fenced: false`), the unmounts (above; mountinfo empty,
no new block lines), observer removed (`authorized_keys` back to
`e08542dc…`, `restored_pre_run: true`), `next_entry` consumed, no AVC, full
dmesg (459 lines), poweroff, the bhyve child gone at the second check
(16:33:32Z), host cleanup (tap and vmm destroyed, UEFI vars `ff1ec37b…`
unchanged), native FUSE unmount (`rc 0`), the observer key deleted on f3
(`rc 0`) and the window restore (`mount_restored: true`, `fusefs.ko` kept
loaded by the reviewed plan). Phase 12: `start-rocky` at 16:33:55Z, SSH at
the second try (16:34:12Z), `check --expect stock` ok, `Enforcing`,
`ak e08542dc…`, no `/.autorelabel`, vm-bhyve run 1 only.

## R15-1: the root fstat lost a race with a recall

### What the evidence shows

1. **Guest timeline** (`stdout.log`, guest monotonic; the guest booted at
   about 16:30:07.8Z by the dmesg `audit(1795105808.144)` line at uptime
   0.385 s, so these are approximate UTC times): `identity-checked` 99.62 s
   (about 16:31:47.4Z), `open-root` m0 100.50 s, `stat-root` m0 107.88 s,
   `failure` (`[Errno 116] Stale file handle`) 115.79 s, `terminal`
   122.79 s. Between the `open-root` and `stat-root` markers guest.py only
   runs `os.open` and a local `/proc` mount check; between `stat-root` and
   the failure only `os.fstat(fd)`.
2. **The host never received the fstat's GETATTR.** The provider ledger of
   m0 keeps only the newest claim (#12), but its freed pages still hold
   older claim records. For T1's process (`31983341…`, m0's fourth
   process) they show #9 OPEN (TERMINAL OK, handle 1), #10
   DESCRIPTION_CLOSE, #11 DESCRIPTION_RELEASE and #12 PROCESS_EXIT, all
   TERMINAL OK. Claims 1-6 were the three earlier guest processes of
   Phase 8 (GETATTR + PROCESS_EXIT each, per the `usable` claim
   summaries; Phase 9 still showed 6 allocated); #7 and #8 are not retained (most likely the realpath
   `lstat` GETATTR and the open's permission refresh). The guest numbers a
   claim only when it admits a CALL (`hfs_call_number_locked`, patch 0003)
   and never reuses a number, so with #9 OPEN followed directly by #10
   DESCRIPTION_CLOSE the fstat's GETATTR was **never numbered or sent**.
   The `ESTALE` came from the guest kernel, before any CALL.
3. **Not a transport or session error.** A refused or failed transport
   exchange blocks the session and logs a `9p-hfs: … blocked` line; there
   was none during T1, and m0's claims #10-#12 succeeded after the
   failure. The only block lines are from the guard stop, 37 s later.
4. **Recalls during T1.** At the T1 baseline both domains were at fence 3.
   In the owner snapshots after T1 the controller has m0 at fence 7
   (domain sequence 24) and m1 at fence 4 (sequence 12): m0 took part in
   four recalls during T1 and m1 in one. m1 issued no claim during T1
   (still 6 allocated), so its recall was an all-domain recall started by
   m0's traffic, and **three m0 recalls were m0-only (scoped)**. The ACK
   archive keeps the last recall per domain: m0's (`fb0f98d7-…`, sequences
   21-24) was scoped, m1's (`1516b211-…`, 9-12) the all-domain one. All
   completed: `Recall` null, `Leases`, `Reservations` and `Tickets` empty,
   both mounts back to idle `VIEW_WAIT` polling until the guard stop.

### Where the ESTALE came from (inference from the code)

- `hfs_getattr` (patch 0003) first runs `hfs_begin(…, mutation=false)`.
  Under the controlled provider that is `hfs_mmap_begin` (patch 0004): wait
  for a LIVE view, capture the view stamp (generation, revision) and the
  installed target, take a DOMAIN READ observation lease with
  `VIEW_ACQUIRE` (`expected_target` = that target), and then **re-check
  that the view is still LIVE at the captured revision and generation and
  that its target hash equals the lease's**. If not, it returns `-ESTALE`
  without a log line and without retrying, and `hfs_getattr` returns it to
  userspace. Every recall bumps the view revision (each INSTALL and the
  REOPEN, `hfs_view_reopen`).
- On the host, `VIEW_ACQUIRE` answers PENDING while a recall covers the
  domain (`recallHoldsLease`, `internal/viewcontrol/tickets.go`), and the
  guest polls `VIEW_QUERY` until the recall ends. A recall that reinstalls
  the same cut then grants the lease at the expected target, so
  `hfs_lease_decode` accepts it (a target mismatch there would have blocked
  the session with a log line). Back in `hfs_mmap_begin` the view revision
  has moved, and the observation fails with `-ESTALE`. The fstat's 7.91 s
  is one recall, the same length as the open's 7.39 s.
- **Which recall:** an observation lease is released asynchronously when
  its operation ends (`hfs_mmap_end` → cleanup worker → `VIEW_RELEASE`), and
  releasing the last holder of a clean READ lease starts a recall of that
  domain only (task 784, `releaseNeedsRecall`/`releaseRecallScope`). The
  open's lease release most likely started a scoped m0 recall just after
  `os.open` returned, while `os.fstat` had already captured its stamp on
  the still-LIVE view; its `VIEW_ACQUIRE` then waited for that recall and
  failed the re-check. The open itself most likely waited the same way for
  the recall started by the release of the realpath `lstat`'s lease, but
  reached `hfs_view_enter` after that recall had stopped the view, so it
  parked until REOPEN and captured a fresh stamp. The fstat's own failed
  observation releases its lease as well (a further scoped recall, during
  the close and exit). That gives the three scoped m0 recalls. The trigger
  of the one all-domain recall cannot be read from the retained evidence
  (an expected-target mismatch, task e84, is a candidate).
- **Not s64.** GETATTR's path re-check (`revalidatePath`,
  `internal/guestbridge/dispatch.go`) walks the grant's recorded ancestry,
  and the ROOT grant has none (`Ancestors: null` in m0's bridge state), so
  it cannot return Stale for the root, and in any case the host never saw
  the GETATTR.
- **Not the oc4 ERROR 4 mapping.** Exporter ERROR 4 (RETIRED_CELL) becomes
  `-ESTALE` only on a failed transport exchange, which blocks the session
  and logs it; nothing like that happened during T1.
- **Not caused by z94.** z94 changed only host planning (a read-only OPEN
  conflicts like a read; the volume root's lifetime journals no pin). The
  root OPEN, close and release all completed OK, the failing GETATTR never
  reached the host, and the scoped release recalls and the
  `hfs_mmap_begin` re-check predate z94 (task 784; patch 0004). z94 made
  the open fast enough to reach the fstat, which exposed this older race.

**Confidence:**
- high: the `ESTALE` was produced in the guest before the GETATTR was
  numbered; the host returned no Stale (claim sequence #9 → #10, no block
  line, later claims OK);
- high: z94 and the pinless root are not implicated; s64 and the oc4
  ERROR 4 mapping are not the cause;
- medium-high: the site is the `hfs_mmap_begin` re-check after
  `VIEW_ACQUIRE` (the only `-ESTALE` on this path that is silent, leaves the
  session usable and follows a recall-length wait; the other candidates,
  a FENCED or blocked view, a missing capability and a stale lease target,
  either block or log or cannot wait seconds);
- medium: the recall during the fstat was the scoped release recall of the
  open's own observation lease;
- low: the trigger of the all-domain recall and the exact recall order.

**Why each recall takes about 7 s** (about 4-5 s in zq3-r14) cannot be
timed from the retained evidence; the ACK archive has no timestamps (task
ce4). Each recall phase's first VIEW_ACK still needs a fresh guest
observation (about 0.5 s by the 984 samples).

**The stop-time ERROR 4 (not T1):** at the guard stop m0's in-flight
`VIEW_WAIT` read got ESTALE from the transport (a released cell, mapped to
ERROR 4 and `-116`), while m1's came after the transport closed
(Unavailable, ERROR 5, `-112`). Both are the designed block on a lost
host; both mounts unmounted in 1 s. Most likely just shutdown ordering
(low confidence).

### Recommended fix

1. **Guest kernel, patch 0004 `hfs_mmap_begin`** (oc4 follow-up, needs a
   kernel rebuild): when the re-check after `hfs_lease_acquire` fails only
   because the view moved on (a recall completed: revision, generation or
   target hash changed, or the phase is not LIVE), release the just-granted
   lease through the existing cleanup path and retry the observation from
   `hfs_view_enter`, bounded by the existing `HFS_VIEW_ENTER_WAIT_MS` (30 s)
   deadline, then `-ETIMEDOUT`. Keep `-ESTALE` for a FENCED view or a
   closing or blocked session. Apply the same rule to `hfs_attr_install`'s
   stamp check for a cache observation (GETATTR, LOOKUP and the others):
   retry the observation instead of returning `-ESTALE`. A recall is
   routine, so no syscall may fail because one completed under it. Add a
   view test (`tools/integration/linux9p/tests/view`) in which a recall
   completes while `VIEW_ACQUIRE` is PENDING.
2. **Fewer recalls (performance, host):** every short-lived guest operation
   ends with a scoped release recall of about 7 s. Either the guest keeps
   its DOMAIN READ lease across a process's consecutive operations
   (linger), or the controller lets a release of a clean DOMAIN READ lease
   that owns no mapping finish without a recall. Fix 1 is needed anyway,
   because any recall (a peer's, e84's) can land in the same window.
3. **ce4** (timestamped recall history) would turn the inferences above into
   timings.

## Host events and facts

- f3 boot `1794574308` unchanged. N1: Rocky clock `Europe/Sofia`, stamp
  `2026-11-19`, newest dnf transaction still #64 (unchanged); nightly gate
  GO at 16:27Z (Phase 1) and 16:29Z (Phase 5 step 0).
- Rocky before: PID 84753 (UTC lstart `Thu Nov 19 12:41:44 2026`), boot
  `b4d9ee31-…`, stock `741.22.1`, pod runtime unchanged. Stopped
  (`stop-rocky` rc 0) at 16:29:40Z, started normally at 16:33:55Z: PID 11069 (UTC lstart
  `Thu Nov 19 16:33:55 2026`), boot `53cec809-b5be-4408-a03b-d348787c196b`.
  **Rocky was down about 4 minutes** (16:29:40Z to 16:33:55Z).
- Phase 3: kernel BLS `35cb6698…` (build ID `9b167690…`), stock check ok.
- Phase 4: window plan WP=`b0e837a1…`, descriptor execution through
  fdescfs supported; observer key enrolled (EA `ec0bedef…`).
- Phase 7: volume `e4a01095…`, K04 baseline recorded.
- After READY: B=`fd158cc3…` (cases credentials, sync, metadata,
  namespace; T5 deferred), U=`fcc6d848…`, L=2. The guest requests were
  written (lock-m0 `f410ee4c…`, lock-m1-t8 `62bf8c68…`, pod `34879085…`)
  but never used.
- Guard peaks: disk 23.6 MB, exporter RSS 327 MB, runtime RSS 37 MB.
- Final f3 state: Rocky running (PID 11069), `/dev/vmm` holds only
  `rocky`, `tap100` gone, no fuse/fdesc mount, `fdescfs.ko` (32) and
  `fusefs.ko` (33) loaded, zroot ONLINE. Rocky has no `hfs-xz3` kernel
  (U2).

## Follow-up tasks

- **R15-1 → guest observation retry** (patch 0004 `hfs_mmap_begin` and
  `hfs_attr_install`, above), then a kernel rebuild and **zq3-r16** with a
  fresh fill to run T1-T11.
- Fewer release recalls (lease linger or a recall-free clean DOMAIN
  release), to bring each guest operation back under a second.
- Still open: ce4 (recall history), e84 (target-mismatch recalls are
  all-domain), s64 (fd-based GETATTR/SETATTR carry no description), qc4
  (zq3-r12), 0a4 (zq3-r11).
