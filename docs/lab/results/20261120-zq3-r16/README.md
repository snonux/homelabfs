# zq3-r16: READY and Phase 9 again; ie4 lets m0's root fstat succeed, but m1's root OPEN gets EAGAIN when its re-acquire finds the control-ticket bound full (2026-11-20)

Run sheet: [filled plan](../20261120-zq3-r16-filled-plan/run-sheet.md)
(the run log records no run-time revision).
Candidate r16 `a1af0bd7…` ([xz3-20261119-r16](../../../../tools/integration/artifacts/evidence/xz3-20261119-r16/README.md)):
the ie4 guest kernel (build ID `7a5bb8dc…`), running for the first time,
with the z94 daemon `8668e5f0…`, the pc4 exporter `2fd7e32f…` and the
window artifacts reused byte for byte. T5 stayed deferred. Times are UTC.
Private evidence: `/home/paul/tmp/claude/hfs-zq3-r16-fill/evidence` (run log
`RUN-LOG.md`), controller output
`/home/paul/tmp/claude/hfs-zq3-r16-fill/ctrl-run/credentials/`, bootstrap
journal `/home/paul/tmp/claude/hfs-zq3-r16-fill/bootstrap-run/`. The
controller, transport and provider ledgers of the owner snapshots
(`credentials-owner-live.tar`, taken right after T1, and
`credentials-owner-after.tar`) were decoded offline from copies with the
r14/r15 decoders.

## Outcome

Phases 0 to 9 passed. T1 (`credentials`) failed during guest admission
(new finding R16-1), so T2-T11 did not run (the sheet allows no retry).
Phase 11 took the guard-SIGTERM branch, and restoration was clean.

**What held:**
- Phase 1a unloaded `fusefs` and `fdescfs` again (Refs 1/1, no mounts,
  both `rc 0`, neither listed afterwards). Phase 4 step 1a qualified the
  daemon `8668e5f0…` again (uid/gid 0, groups `[0,0,5]`, pid 74605).
- Phase 3: kernel BLS `753531a5…` (build ID `7a5bb8dc…`); the T7 pod
  rootfs manifest is `34a71569…`, as expected.
- Phase 5: the active-based estimate left a 0.37 GB margin and recommended
  the UMA drain; it freed 29,091 pages (about 0.12 GB; `uma_free_bytes`
  155 MB → 40 MB). Rocky stopped at 05:01:38Z. Phase 6 passed with a
  0.41 GB margin (reclaimable 15,177,344,864 bytes against a bar of
  14,763,950,080).
- Phase 8: the watcher made both operator signatures (enroll, then bind
  3.1 s later). mount-0 (mountinfo 436) and mount-1 (454) both came up with
  the exact g64 tuple. READY came 32 s after launch (05:01:53Z →
  05:02:25Z). The `usable` gate passed (claims 2.65 s after the final call,
  `observe_sequence_last` L=2).
- Phase 9 (budget GO at 05:03:22Z, 3510 s left): three SSH no-ops, OBSERVE
  at L+1=3 passed (both domains `REOPENED`, fence 3, claims `Allocated 6,
  Open 0`), mountinfo had exactly the two tuples, `Enforcing`, the SELinux
  `Initializing` line, no hfs block line.
- 984 timing (final): SSH no-op median 0.232 s (3 samples), OBSERVE net
  median 0.675 s, max 0.83 s (2 samples), below the 2.4 s bound.
- **ie4 worked where zq3-r15 failed:** m0's root OPEN (`open-root[0]`)
  and the following `os.fstat` (`stat-root[0]`) both succeeded, and m0 was
  admitted (`root-admitted[0]` 4.6 s after `identity-checked`). m1's
  re-check logged the new ie4 line and acquired again instead of returning
  ESTALE. T1's failure left nothing behind: no guest block line during T1,
  no transport fence (`fenced: false`), `runtime.stderr` empty.
- When the guard stopped the runtime, both sessions blocked at uptime
  142.8 s with `VIEW_WAIT (-112), exporter ERROR 5` (zq3-r15 had ERROR 4
  for m0) and both `umount`s returned `rc 0` at once (0 s; R13-1 stays
  fixed).
- No AVC records, no relabel. U2 removed the test kernel.

**New finding:**
- **R16-1:** T1's m1 root OPEN (`open-root[1]`, 6.1 s after
  `identity-checked`) failed 8.3 s later with `BlockingIOError` errno 11
  (`EAGAIN`) on `/var/tmp/hfs-zq3-zq3-r16/m1`. The probe ended with
  `terminal` 15.8 s after `identity-checked`, and the controller recorded
  `dispatching-uncertain` (`Refused: required actual syscall case failed:
  credentials`, 16.7 s). See the analysis below.

| Phase | Result |
|---|---|
| 0-1, 1a | passed (both modules unloaded) |
| 2-7 | passed (process observer `8668e5f0…`, nightly gate GO 04:58Z and 05:01Z, UMA drain run, Phase 6 margin 0.41 GB) |
| 8 | passed (mount-0 and mount-1 ok, READY 32 s after launch, `usable` L=2) |
| 9 | passed (OBSERVE L+1=3; 984 timing) |
| T1 | **fail** (R16-1) |
| T2-T11 | unrun (no retry after T1; T5 deferred) |
| 11-12 | completed (guard-SIGTERM branch) |

T1 started at 05:03:41Z with the guard budget GO (3492 s left). The
baseline OBSERVE at L+2=4 passed (both domains `REOPENED`, fence 3). The
probe started at 05:03:42.3Z. Phase 11 ran the guard-SIGTERM branch: the
final 984 sample, then one SIGTERM to guard 76187 at 05:04:22Z. The guard
was gone by 05:04:33Z. Receipt: `runtime_exit 0`, only the
`guard-interrupted SIGTERM` breach (149.5 s), runtime not running at guard
exit, exporter (pid 76198) still alive. Then: evidence tarballs, the fence
record (`fenced: false`), the unmounts (above; no new block lines),
observer removed (`authorized_keys` back to `e08542dc…`,
`restored_pre_run: true`), `next_entry` consumed, no AVC, full dmesg (462
lines), poweroff, the bhyve child gone at 05:05:07Z, host cleanup (tap and
vmm destroyed, UEFI vars `5df71476…` unchanged), native FUSE unmount
(`rc 0`), the observer key deleted on f3 and the window restore
(`mount_restored: true`, `fusefs.ko` kept loaded by the reviewed plan).
Phase 12: `start-rocky` at 05:05:29Z, SSH at the second try (05:05:49Z),
`check --expect stock` ok, `Enforcing`, `ak e08542dc…`, no
`/.autorelabel`, vm-bhyve run 1 only.

## R16-1: m1's re-acquire was refused for control-ticket capacity

### What the evidence shows

1. **Guest timeline** (`stdout.log`, guest monotonic): `identity-checked`
   102.45 s, `open-root` m0 103.62 s, `stat-root` m0 105.75 s,
   `root-admitted` m0 107.08 s, `open-root` m1 108.55 s, `failure`
   (`[Errno 11] Resource temporarily unavailable`) 116.83 s, `terminal`
   118.29 s. The only guest kernel line in T1 is at uptime 115.94 s:
   `9p-hfs: session eb262a2f-… observation re-check failed (view moved past
   the granted lease); acquiring again before releasing it` (eb262a2f is
   m1). There is no block, fence, exhaustion or second re-check line.
2. **m1's acquisition tickets** (controller ledger, live snapshot): the T1
   process's m1 acquisition family has three `VIEW_ACQUIRE` tickets:
   #1 GRANTED at once (lease `4110f6e8…`, fence 4), #2 first PENDING, then
   GRANTED (lease `490ad4b1…`, fence 5), and **#3 answered BLOCKED on its
   first exchange** (Initial and Current BLOCKED, `ExpectedMismatch`
   false, same expected target as #2). The matching release family has #1
   (lease `4110f6e8…`) and #2 (lease `490ad4b1…`), both PENDING, then
   RELEASED. #3 is the ie4 re-acquire; its BLOCKED reached
   `hfs_view_exchange` as `-EAGAIN`, which `hfs_mmap_observe` returns
   without another re-check (ie4 S1), and the OPEN failed with it.
3. **m1's OPEN never reached the host:** m1's provider ledger holds only
   the Phase 8 GETATTR and PROCESS_EXIT claims.
4. **Which BLOCKED.** `acquire` (`internal/viewcontrol/tickets.go`) answers
   BLOCKED on a first exchange only from `remember` (a full Sources bound)
   or from `grantLease`/`shareLease` (lease count, control or identity
   capacity, or revision overflow). The expected-target path (e84) answers
   PENDING first and records `ExpectedMismatch`, which #3 does not have.
   The ledger rules out all but one: Sources 1 (bound 10), 5 lease records
   (bound 64), Identities 17 (bound 64), every revision 1. **The control
   capacity is full.** `controlCapacity(1)` charges 1 for the request, 1
   per unretired `VIEW_RELEASE` ticket, 1 per lease whose release credit is
   reserved but unbound and 1 per further holder of a shared lease, against
   `ControlTickets` 8 (the zq3 spec bound, shared by both domains). When #3
   arrived that was 1 + 3 + 4 + 1 = 9:
   - 3 unretired release tickets: two of m0 from earlier processes
     (leases at fence 2 and 3, most likely Phase 8's GETATTRs; their
     families still at floor 0) and m1's own #1;
   - 4 for m0's T1 lease `cb26e49e…`, which four m0 observations shared
     (one release ticket or reserved credit per holder; 3 holder releases
     RELEASED, the last one PENDING);
   - 1 for lease `490ad4b1…`, which ie4 still held (credit reserved, release
     not yet sent).
   #2 had been granted at exactly 8. The live snapshot, taken after the
   failure, still adds up to 9 (8 unretired release tickets).
5. **Why the release tickets stayed:** a release ticket is charged until
   its family's floor passes it. The guest retires families only at the end
   of a cleanup packet (`hfs_mmap_cleanup` → `hfs_view_retire`) and does not
   retry a BLOCKED retirement; the host refuses retirement while any recall
   runs or while the family's credentials still hold an active lease
   (`retirementBlocked`, `retirement.go`). Of m0's two earlier release
   families one retired only after T1 (the after snapshot); the other and
   m1's release family were still at floor 0 there.

### Where the 8.3 s went (inference)

The re-check line came 7.4 s after `open-root[1]` and 0.9 s before the
failure. The stdout markers and the dmesg stamps are both since guest boot,
so this split is only approximate. The 7.4 s match one scoped release recall
(about 7 s in zq3-r15): m1's #1 lease (the first observation of the open's
path) was released, its last-holder release started a recall of m1 (fence
4 → 5), and #2, taken by the open's observation on the stamp captured
before that recall, waited PENDING behind it (`recallHoldsLease`). It was
granted after the REOPEN, so the re-check saw a moved revision (-EXDEV).
The remaining 0.9 s is ticket #3: one control round trip (OBSERVE net
0.52-0.83 s) answered BLOCKED. The later m1 fence 6 is most likely the
recall that the held lease's own release started after the failure.

### Did ie4's acquire-before-release cause it?

Not the shortage itself. The held lease cost one control slot, but
releasing it first would have cost the same slot as an unretired release
ticket (`ticketCapacity` binds the reserved credit to the release). The
count was already at the bound when #2 was granted. Any further m1
observation would have needed a ninth slot, such as the `os.fstat` after
the open. Only a release-first path that also retired m1's family before
acquiring again could have freed slots. That costs the self-recall ie4
avoids, and retirement is refused while a recall runs. ie4 did make the
failure immediate: its S1 path returns a host BLOCKED as `-EAGAIN` at
once instead of waiting.

**Confidence:**
- high: the `EAGAIN` is the host's BLOCKED answer to m1's ie4 re-acquire
  (ticket #3, BLOCKED on first exchange; no other guest line; the OPEN never
  reached the host);
- high: the BLOCKED came from the control-ticket capacity in
  `grantLease`/`shareLease`, not from e84's expected-target path, the
  Sources bound, the identity bound or a lease conflict (a conflict
  answers PENDING);
- medium-high: the exact 9 = 3 + 4 + 1 + 1 breakdown at #3's arrival
  (the ledger keeps the end state only, and m0's four holders were all
  acquired before m1's open);
- medium: the 7.4 s + 0.9 s split and the recall sequence.

### Recommended fix

1. **Control capacity that does not run out within a test step (host and
   spec).** The structural fix is in `internal/viewcontrol/retirement.go`
   `retirementBlocked`: a range of terminal tickets should not wait for
   every recall to end or for an unrelated active lease of the same
   credentials. Only a ticket that still binds an active lease or open
   reservation should hold retirement back. Then answered release tickets
   stop counting soon after their release. Until then, raise
   `ControlTickets` in the zq3 spec (`internal/runtimeprep/testdata/zq3-spec.json`
   and the run spec). At the current `LedgerBytes` (`validBounds`) the
   ceiling is 28, and 24 fits. Add a `zq3_bounds_test.go` case with the
   spec bounds that replays T1's profile: two mounts, four shared m0
   observations, m1's first release and a held lease plus re-acquire. It
   must grant at the spec bound.
2. **Guest (patch 0004 `hfs_mmap_begin`/`hfs_mmap_observe`):** treat a
   BLOCKED observation `VIEW_ACQUIRE` as back-pressure. Queue the held
   packet's release, run `hfs_view_retire`, and retry with a short backoff
   within the existing `HFS_VIEW_ENTER_WAIT_MS` deadline. Then return
   `-ETIMEDOUT` with one log line, not an immediate `-EAGAIN`. Have the
   cleanup worker retry a BLOCKED `hfs_view_retire` later instead of
   leaving the family until the next cleanup.
3. **Fewer release recalls** (zq3-r15 fix 2) would also shorten the 7.4 s
   and leave fewer recalls to block retirement.

## Host events and facts

- f3 boot `1794574308` unchanged. N1: Rocky clock `Europe/Sofia`, stamp
  `2026-11-20`, newest dnf transaction #65 (expected); nightly gate GO at
  04:58Z (Phase 1) and 05:01Z (Phase 5 step 0).
- Rocky before: PID 11069 (UTC lstart `Thu Nov 19 16:33:55 2026`), boot
  `53cec809-…`, stock `741.22.1`, pod runtime unchanged. Stopped
  (`stop-rocky` rc 0) at 05:01:38Z, started normally at 05:05:29Z: PID
  78540 (UTC lstart `Fri Nov 20 05:05:29 2026`), boot
  `302a5df1-b65a-4d43-9ac6-efd2cd71d795`. **Rocky was down about 4
  minutes** (05:01:38Z to 05:05:29Z).
- Phase 4: window plan WP=`79143cab…`, descriptor execution through
  fdescfs supported; observer key enrolled (EA `8566e164…`).
- Phase 7: volume `6ef0dfaf-…`, K04 baseline recorded.
- After READY: B=`10614076…` (cases credentials, sync, metadata,
  namespace; T5 deferred), U=`00c53b32…`, L=2. The guest requests were
  written (lock-m0 `b4a623ab…`, lock-m1-t8 `195fd058…`, pod `052b3a40…`)
  but never used.
- Guard peaks: disk 23.6 MB, exporter RSS 302 MB, runtime RSS 38 MB.
- Final f3 state: Rocky running (PID 78540), `/dev/vmm` holds only
  `rocky`, no fuse/fdesc mount, `fusefs.ko` loaded by the reviewed window
  plan. Rocky has no `hfs-xz3` kernel (U2).

## Follow-up tasks

- **R16-1 → control-ticket capacity:** host retirement that does not wait
  for unrelated recalls or leases, a higher zq3 `ControlTickets` bound with
  a T1-profile bounds test, and a guest BLOCKED back-off on observation
  acquires (above). Then a rebuild and **zq3-r17** with a fresh fill to run
  T1-T11.
- Fewer release recalls (lease linger or a recall-free clean DOMAIN
  release), to bring each guest operation back under a second.
- Still open: ce4 (recall history), e84 (target-mismatch recalls are
  all-domain), s64 (fd-based GETATTR/SETATTR carry no description), qc4
  (zq3-r12), 0a4 (zq3-r11).
