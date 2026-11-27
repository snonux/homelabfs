# zq3-r25: gn4 gets T1's probe syscalls through, including the 1024-group child, but the retention inventory's `lstat` fails ENOSPC with the control lane full again (2026-11-27)

Run sheet: [filled plan](../20261127-zq3-r25-filled-plan/run-sheet.md)
(fill merge `3566524`; tools snapshot `1f43821`).

Candidate: the r25 rebuild `a3eac923…` ([xz3-20261127-r25](../../../../tools/integration/artifacts/evidence/xz3-20261127-r25/README.md)):
- the guest kernel, build ID `8c70915f…` (gn4: the release ticket is charged before the VIEW_ACQUIRE, a charge-pressure round also runs `hfs_view_retire`, and a failed retry logs its own line), first run;
- the go1.27.4 gn4 daemon `43414d7f…` (per-credential `checkRetire`), first run;
- the exporter `8cadae40…` (zq3-r21's bytes, fifth run).

`hfs_fdattr` was enabled on both mounts. Times are UTC. Private evidence:
`/home/paul/tmp/claude/hfs-xz3-20261127-r25/zq3-r25-fill/evidence` (run log `RUN-LOG.md`).

## Before the run

- Paul gave the final GO at about 15:16Z ("GO now + copy key"): run today with no refresh. That covered A1-A8, including the Phase 1a unload, T7, the Rocky stop, the controlled launch and both signatures by Claude through the watcher. The fill's facts are from 14:52Z, after dnf #69. T5 stays deferred.
- The operator key was copied into the fill's `operator/` before the run. Only `sign_watch.py` used it, through the signer. The operator checked only its mode, owner, link count and size, and never read it.
- Rocky ran zq3-r24's Phase 12 incarnation (PID 47896, boot `971d3010-…`).
- iom1-3 were Stopped through the whole run, and the sheet never touched them.
- f3 swap was 0, so there was no Phase 0b.

## Outcome

Phases 0 to 9 passed. Phase 4 step 1a qualified the new daemon, and Phase 9 started about 70 s after READY. **T1 (`credentials`) got much further than in zq3-r23 and zq3-r24.** Every probe syscall returned what the case expects. That includes the 1024-group access child, which read `group-tail` (the R24-1 point). Then guest.py's retention inventory ran an `lstat` of `setgid/created`, and it failed `ENOSPC`. m0's charge-pressure round logged `view retire -11` with the **control lane at 98.1% of its bytes**, followed by gn4's new `charge still failed after the retry: error -28` line (**R25-1**, R24-1 again on the control lane, a finding against gn4). The host's view controller held **0 leases and 0 tickets** at the failure OBSERVE (zq3-r24: 8 and 22), so the daemon side of gn4 left no stranded lease. T2 to T11 did not run. Phase 11 took the guard-SIGTERM branch, and restoration was clean. **Rocky was down for 8 min 48 s** (9 min 8 s to SSH; zq3-r24: 6 min 46 s). The extra time is T1 itself: it ran 183 s here, against 55 s in zq3-r24.

| Phase | UTC | Result |
|---|---|---|
| 0 | 15:35:42-15:36:43 | passed |
| 1 | 15:36:44-15:37:30 | passed (nightly gate GO at 15:37Z) |
| 1a | 15:37:40-15:38:00 | passed (fusefs id 42, fdescfs id 41, `rc 0` each) |
| 2 | 15:38:00-15:38:20 | passed |
| 3 | 15:38:20-15:39:20 | passed (non-default kernel entry, PM `34a71569…`, 82 entries) |
| 4 | 15:39:22-15:40:48 | passed (step 1a qualified `43414d7f…`, pid 91766) |
| 5 | 15:41:00-15:41:53 | passed (gate GO at 15:41Z, UMA drain, one-shot select, Rocky stopped) |
| 6 | 15:41:55-15:42:05 | passed (margin 0.25 GB) |
| 7 | 15:42:05-15:42:20 | passed |
| 8 | 15:42:25-15:43:58 | passed (launch 15:42:33Z, READY 15:43:06Z, `usable`, L = 2) |
| 9 | 15:44:16-15:44:35 | passed |
| T1 `credentials` | 15:44:40-15:47:49 | **fail** (R25-1: ENOSPC in the retention inventory's `lstat`; `charge still failed after the retry` on the control lane) |
| T2 `sync`, T3 `metadata` (s64 peer-rename guard), T4 `namespace` | - | unrun (T1 failed) |
| T5 `mapped-pressure` | - | unrun (deferred, re-plan pending) |
| T6, T7 (pod), T9, T10, T11, T8 | - | unrun (T1 failed) |
| 11 | 15:48:00-15:50:30 | completed (guard-SIGTERM branch) |
| 12 | 15:50:34-15:52:00 | completed; Rocky on stock 741.27.1, custom kernel removed |

The phase times come from the tool output, the run log and the evidence file stamps. Times inside a phase are accurate to about a minute; the start, stop, READY, guard and Rocky times are exact. The run started right after the GO handoff. The gate gave GO at 15:37Z and again at 15:41Z, so no N1 wait was needed. Every wait was a bounded loop in the foreground.

**What held:**
- **Phase 0:** all pins passed, and so did `argv.py verify`, `check-argv`, `bootstrap.py check` and the synthetic batch check (four cases, `mapped-pressure` deferred, `fd_attr` true). The workstation ssh was still `d4c70b09…`. `git diff 1f43821 3566524` over the pinned tool paths was empty.
- **Phase 1:** every fact matched the 14:52Z facts:
  - f3 boot `1794574308`, Rocky PID 47896 (lstart Fri Nov 27 06:55:27 2026), boot `971d3010-…`, UEFI vars `a0b33064…`, swap 0;
  - iom1-3 Stopped, no k3s VM, `/dev/vmm` = `rocky`;
  - Rocky runs `741.27.1` = grub default = `saved_entry`, with `next_entry` empty;
  - stamp `2026-11-27`, dnf #69 (`dnf_unchanged: true`), `pod-runtime-unchanged: True`.
- **Phase 3:** the kernel entry `b26c0ac5…-6.1.190-hfs-xz3` was added as a non-default entry, and `check --expect stock` passed. The pod rootfs manifest is the predicted `34a71569…` (82 entries).
- **Phase 4:**
  - **step 1a qualified the new gn4 daemon `43414d7f…` on its first f3 run** (`p4-observer.py` `pass: true`: uid/gid 0, groups `[0,0,5]`, pid 91766, stderr empty);
  - the window plan `fd9730c0…` was set up and qualified, and fdescfs was supported;
  - fusefs was loaded (fdescfs id 42, fusefs id 43);
  - the observer key was enrolled (`EA` `b5921c68…`).
- **Phase 5:** the estimate margin was +0.17 GB, so the UMA drain ran and freed 0.16 GB. `select` set only the one-shot `next_entry`, and the stock default stayed. `stop-rocky` ran from 15:41:47Z to 15:41:53Z.
- **Phase 6:** passed with a 0.25 GB margin (15,016,175,000 bytes).
- **Phase 8:**
  - launch at 15:42:33Z, READY at 15:43:06Z (33 s);
  - both signatures by the watcher (`"watcher": "signed"` ×2);
  - mounts 391 and 408;
  - `usable` passed with L=2;
  - the exporter launched cleanly, and `exporter.stderr` and `runtime.stderr` stayed empty.
- **Block 2a (R22-2 stays fixed):** the driver started in the background at about 15:42:31Z. The bounded `grep READY` poll returned and logged 15:43:12Z, the `summary.json` wait returned by 15:43:55Z, and Phase 9 started at 15:44:16Z (`late_after_ready: false`). The background task's completion notice was never waited for.
- **`hfs_fdattr`, fifth VM run:** both mountinfo rows read `rw,cache=mmap,hfs_fdattr,msize=65536,trans=virtio,version=9P2000.L.hfs1.4`.
- **Phase 9:** passed, with 3497 s of guard budget left (guard end 16:42:33Z).
  - OBSERVE seq 3, `Open 0` on both providers, view controller Leases 0 and Tickets 0;
  - Enforcing, enforce 1; `SELinux:  Initializing.` present (the `LSM: initializing` grep matched nothing, as before); `no-hfs-block`.
- **T1 got past everything zq3-r23 and zq3-r24 reached** (gn4, km4, sj4, s64). Admission took 7.30 s, with the largest gap 2.67 s at `open-root[1] → stat-root[1]`. In order, all of these returned 0 or the designed refusal:
  - `mkdir`, `open-leaf`, `fchmod-leaf`, then `create-exclusive group`, `fchown`, `fchmod` and the `pwrite`;
  - access child 1 read `group-secret`, and child 2 got `EACCES` (`"syscall": "open"`, designed);
  - `create-exclusive(group-tail)`, then `fchown`, `fchmod` and the 15-byte `pwrite`;
  - **the 1024-group access child (pid 2722, uid 41001, 1024 groups) read `full-group-tail`, status 0. This is the R23-1/R24-1 point, now passed;**
  - the next child got `EACCES` opening `group-tail` (designed);
  - the `setgid` directory: `mkdir`, `open-directory`, `fchown 0:42002` and `fchmod`; its child reported `[41001, 42002, 416]`; then `stat-cross-domain` of `setgid/created`;
  - the `sticky` directory: `mkdir`, `open-directory` and `fchmod`; `create-exclusive owned` and `fchown`; the child's `unlink` of `owned` got `EPERM` (designed);
  - `group`: `open`, `chmod-special-bits`, `fstat`, `chown-clears-special-bits`, `fstat` (s64 fd-based calls, all 0);
  - `open-leaf` for the retention inventory.
- **xf4 and vj4 still work.** `recalls.py` reported:
  - `advances` 30 and `self_admissions` 14 (zq3-r24: 9 and 10). T1 ran much longer here;
  - 4 recalls, all `RELEASE/VIEW_RELEASE` (DOMAIN): the 2 bootstrap recalls (15:42:54Z, 15:43:02Z) and 2 more on m0 during T1 (15:45:44Z, 9.6 s; 15:46:57Z, 6.6 s);
  - no CALL_CONFLICT and no EXPECTED_TARGET recall.
- **No guest WARNING or BUG, no AVC, no host fence.** `t-fail-fence.json` and `p11-fence.json` both report `fenced: false`.
- **Unmounts after the guard stop:** m0 `rc 0` in 0 s, m1 `rc 0` in 1 s, with no new block line (R13-1 stays fixed).
- **Phase 12:** Rocky started on stock `741.27.1`, with one vm-bhyve run and no relabel. U2 removed the test kernel.

## R25-1 (R24-1 again on the control lane): the retention inventory's `lstat` fails ENOSPC; gn4's round frees nothing on the control lane and logs `charge still failed after the retry`

**What happened.** The probe started at 15:44:46Z and ended after 183.1 s with `dispatching-uncertain` ("Refused: required actual syscall case failed: credentials").

The guest.py named events (`ctrl-run/credentials/stdout.log`; guest monotonic seconds):
1. the 1024-group child's exit was recorded at 182.248 s (status 0, `full-group-tail`);
2. all the later probe steps listed above returned 0 or their designed refusal, up to `open-leaf` (reply at 289.530 s);
3. **the failure at 303.676 s:** `{"error": "OSError", "errno": 28, "detail": "[Errno 28] No space left on device: 'setgid/created'"}`. This comes after `open-leaf` and before any `payload-retained` event, so it is inside `Retention.retain()` → `inventory()`. That function runs `os.stat(name, dir_fd=leaf, follow_symlinks=False)` (an `lstat`) on each payload entry, and `setgid/created` is the first file entry of `credentials`. It is the parent's own call, not a child's, so no `"syscall"` field was recorded. The terminal record came at 308.283 s (`payload_retained: false`).

Every `9p-hfs` line in the full guest `dmesg` (`p11-guest-dmesg.txt`). The printk clock is `CLOCK_MONOTONIC` + 46.1 ms (`sched_clock: Marking unstable (14585581781, 46136024)`). All lines are for m0, session `6b53dc33-…` (`hfs0`):

| printk s | Line |
|---|---|
| 193.951329 | `observation re-check failed (view moved past the granted lease); acquiring again before releasing it` (4h4) |
| 246.565076 | own retire 0 freed 2 requests, **view retire -11**, 0 of 1 other sessions freed 0, ordinary lane 303,908,808 / 536,870,912 bytes, 489 / 512 slots; retrying the charge once |
| 248.263877 | own retire 0 freed 1 requests, **view retire 0**, 0 of 1 other sessions freed 0, ordinary lane 9,258,801 / 536,870,912 bytes, 290 / 512 slots; retrying the charge once |
| 294.657042 | own retire 0 freed 2 requests, **view retire -11**, 0 of 1 other sessions freed 0, ordinary lane 449,292,647 / 536,870,912 bytes, 406 / 512 slots; retrying the charge once |
| 302.471238 | own retire 0 freed 3 requests, **view retire -11**, 0 of 1 other sessions freed 0, **control lane 526,509,074 / 536,870,912 bytes, 371 / 512 slots**; retrying the charge once |
| 302.471269 | **`charge still failed after the retry: error -28, control lane used 526509074 of 536870912 bytes, 371 of 512 slots; the charge fails`** |

The two m0/m1 `VIEW_WAIT (-112), exporter ERROR 5` block and worker-stop pairs at 372.26 s came after the guard's SIGTERM and are expected.

**Timing.** The failing round ran at monotonic 302.425 s, during the inventory: after `open-leaf` returned at 289.530 s and 1.25 s before guest.py recorded the failure.

**Host side (read-only):**
- The failure OBSERVE (seq 5, the yf4 direct form) was OBSERVED.
- m0's provider ledger: Allocated 154, Retired 152, Open 0. m1's: Allocated 20, Retired 17, Open 0.
- **The view controller held Leases 0 and Tickets 0**, with Reservations 0, no recall and `Matches` true. The Phase 9 OBSERVE and the T1 baseline (seq 3 and 4) showed the same. zq3-r24's failure OBSERVE showed 8 and 22.
- `runtime.stderr` and `exporter.stderr` were empty. `t-fail-fence.json` and `p11-fence.json` report `fenced: false`.

**What the evidence shows:**
- **gn4 moved the failure, but did not remove it.** The 1024-group child's `open` of `group-tail` now passes, and so does everything after it in the probe. The case then failed on the same lane, with almost the same usage as zq3-r24 (526,509,074 bytes against 526,456,646; 371 slots against 368).
- **gn4's new retry logging works.** Its `view retire <errno>` field and its `charge still failed after the retry` line both fired. By the sheet's rule that line is always a finding, and on the control lane during T1 it is R24-1 again.
- **The view retirement found nothing to retire when it mattered.** All three `view retire -11` rounds came while the lane was full. The one `view retire 0` round (248.26 s) returned bytes, and its charge then succeeded. That `view retire 0` is gn4 at work, and expected. The two ordinary-lane `-11` rounds at 246.57 s and 294.66 s were followed by successful charges, so they are not findings on their own.
- **No host lease was stranded.** Leases and Tickets were 0/0 on the host at the failure OBSERVE, about 25 s after the case. The daemon's per-credential `checkRetire` and the guest's release-ticket-first order left no host state behind.
- **Likely cause (inference, not confirmed in code; hp4 below proved it only in part):** gn4 charges each lease's control release ticket before its VIEW_ACQUIRE is sent. So every live lease now holds its release ticket's control bytes (about 25.5 MiB with its view package, per gn4's zq3-r24 analysis) for as long as the lease lives. About 20 such tickets fill the 512 MiB lane. During the retention inventory the T1 process held several observation leases (the leaf, both directories and the payload entries). Earlier leases may also still have been live, or released but not yet retired. `view retire -11` says that, at that moment, no family's floor could advance: every held ticket belonged to a lease still in use, or its VIEW_RETIRE was BLOCKED. The guest's per-ticket accounting is not visible on the host, so the owner snapshots cannot settle this.
- **No session blocked:** there was no `blocked by … (-28)` line and no `VIEW_RETIRE (-110)` line before the guard stop.

`credentials-owner-live.tar` and `credentials-owner-after-2.tar` (BoltDB ledgers for the controller, both providers and the transport) and `f3-evidence.tar` hold the host state. The full guest `dmesg` is in `p11-guest-dmesg.txt`.

Ruled out:
- **R22-1 again:** no `VIEW_RETIRE` block line.
- **R24-1's stranded host lease:** Leases 0 and Tickets 0 on the host.
- **9n4's race:** no `a concurrent round freed requests` and no `nothing freed` line (`0 of 1 other sessions freed 0` each time).
- **A host fence or refusal:** none recorded, and both stderr files were empty.
- **ETIMEDOUT or a deadline:** `etimedout` false, `deadline_hit` false. The case took 183 s of its 240 s request deadline, so it ended `dispatching-uncertain` on the error, not on a bound.
- **s64:** the fd-based `fstat`, `fchown` and `fchmod` calls all returned 0. The failing call is a path `lstat`.

**Effect.** T2-T11 did not run. The T3 s64 peer-rename guard, T7 (pod) and hh4/dh4 in T9-T11 still have no real run.

**For the fix (task to file):**
- account for m0's control lane during T1 with gn4's charging order: how many release tickets are live at once (per lease, per credential set, per child), and which are released but not retirable;
- check why `hfs_view_retire` returns `-11` at 302.47 s: no floor could advance, or every VIEW_RETIRE was BLOCKED;
- decide between a smaller control charge per release ticket (the view package size), a larger control lane for the probe's lease count, or retiring a lease's ticket as soon as the lease is released within the same process.

**Proven cause (task hp4, from the controller ledger and the code).** The live `viewcontrol-v1.db` in `credentials-owner-live.tar` keeps older copies of the state JSON in its free pages. Four full copies were decoded read-only.
- **What held the control lane:** 13 VIEW_RELEASE tickets of the T1 process (credential hash `a8d02f36…`), numbers 43-55 of its release family, all answered RELEASED. The oldest copy shows both T1 families at floor 42 and high 55, with acquisitions 43-55 GRANTED, 5 lease rows, all inactive, and no recall or reservation. No other family had an unretired ticket.
  - 13 × 26,786,357 bytes (one release ticket with its view package) plus the permanent base of 178,286,433 bytes is exactly the logged 526,509,074. r24's 526,456,646 is also 13 tickets.
  - The lane holds at most 13 such tickets beside the base, so the 14th charge fails.
  - They were view tickets, not requests: requests use the ordinary lane.
- **Why the host showed 0/0:** the next copy has T1's acquisition family at 55 and its release family still at 42, which is between the two VIEW_RETIREs of one cleanup round. The current state has both at 55. Once T1 stopped, the cleanup worker's VIEW_RETIREs were RETIRED and the guest charges were returned (`hfs_view_retire_free`). The guest accounting was right, and nothing leaked.
- **Why `view retire -11`:** `hfs_view_retire` returns -EAGAIN when no floor advanced. It takes the retirement mutex blocking, so -11 is not lock contention, and no family was in flight. T1's tickets 43 were retirable in the guest long before the failure, so the round sent VIEW_RETIREs and the host answered BLOCKED.
  - No recall ran (the last ended at 15:47:04Z, the failure was at about 15:47:44Z), and no reservation existed.
  - That leaves gn4's per-credential clause (`retirementBlocked`, `factsLedger.checkRetire`): a lease of T1's credentials was active.
- **Why one always was:** the guest's cleanup worker releases an observation's lease asynchronously after `hfs_mmap_end`, while the process starts its next syscall. 8 of the 13 acquisitions (47-51, 53-55) were granted the previous observation's still active lease (`shareLease`). So every cleanup VIEW_RETIRE, re4 kick and charge-pressure round met T1's next lease:
  - the ordinary-lane rounds at 246.57 s and 294.66 s ran inside an observation;
  - the failing control round was the `lstat`'s release-ticket charge, made while the previous observation's lease was still active.
- **gn4's prediction (about 20 live leases' up-front release tickets) is refuted.** The 13 were answered releases of inactive leases, with at most the newest one or two still live at the failure. The lane fills at 13, not 20.

**Fix (hp4, host):** `retirementBlocked` and `factsLedger.checkRetire` refuse a range only when one of its tickets is a GRANTED acquisition of a still active lease, no longer for any active lease of the same credentials. Answered releases now retire beside the process's next lease, and acquisitions retire up to the first one whose (shared) lease is still active (at most 6 back in r25). The recall and open-reservation clauses are unchanged.
- The guest patches changed in comments only.
- Tests: controlled_received `TestControlledReceivedActualHeldLeaseRetirement` reproduced r25's two lines red first, and passes now. The Go tests are `TestRetirementIsPerTicket`, `TestFactsRetireIsPerTicket` and `TestZQ3HolderRetiresBesideItsNextObservation`. See the [hp4 section](../../../../tools/integration/linux9p/README.md#answered-releases-retire-beside-the-processs-next-lease-hp4).

## R25-2 (operator, minor): one evidence snapshot truncated; one probe line moved to the background by the tool

- The case-failure block's second owner snapshot (`credentials-owner-after.tar`) came back truncated: tar printed `(null)`, rc 1, 819,200 bytes. One read-only retry, `credentials-owner-after-2.tar`, succeeded. The truncated file is kept as `credentials-owner-after.truncated-tar-null.partial`. Under its original name it matched Phase 11 step 1a's `*owner*.tar` glob and made `p11-fence.py` fail with `unexpected end of data`. After the rename, the same line ran and reported `fenced: false`.
- The shell tool moved the T1 `probe` line to the background after 180 s, even though the line had a 330 s timeout. Its result was read from the line's own output file. The completion notice arrived together with that read, and no wait depended on it.

Nothing on the hosts was affected. Record only.

## gn4 / 9n4 / km4 log-line observations

- Every `retired under charge pressure` line is in the R25-1 table: four lines, three with `view retire -11` and one with `view retire 0`.
- One `charge still failed after the retry` line (control lane, `error -28`): **R25-1**.
- No `a concurrent round freed requests, retrying the charge once` line, and no `nothing freed, the charge fails` line.
- One 4h4 line: `view moved past the granted lease; acquiring again before releasing it` at 193.95 s. Expected, not a finding.
- No `blocked by uncertain control result on VIEW_RETIRE (-110)` line (R22-1 did not recur). No `blocked by … (-28)` line, no `view control worker stopped` line before the guard stop, and no `WARNING`/`BUG`.
- None of the other expected re4/1h4/tj4/s64/hh4/dh4/ie4 lines appeared during T1.

## Host events and facts

- **f3:** boot `1794574308` unchanged. iom1-3 were Stopped at Phase 1, at Phase 5 step 1 and at Phase 12, and no k3s VM was listed.
- **N1:** stamp `2026-11-27`, dnf #69 unchanged. The nightly gate printed GO at 15:37Z and 15:41Z. The run ended at about 15:52Z, far ahead of the 21:25Z bound.
- **Rocky:**
  - before the run: PID 47896 (Fri Nov 27 06:55:27Z), boot `971d3010-…`;
  - `stop-rocky` at 15:41:47Z (`Stopped` by 15:41:53Z);
  - started at 15:50:35Z: PID 97712 (lstart Fri Nov 27 15:50:35 2026 UTC), boot `5f26abab-958b-4701-97ac-8f1772023d15`;
  - pinned SSH answered at 15:50:55Z on the second try (one banner timeout);
  - **down 8 min 48 s** (9 min 8 s to SSH).
- **Run values:**
  - window plan `fd9730c0…`;
  - observer EA `b5921c68…`, removed (`restored_pre_run: true`, `authorized_keys` back to `e08542dc…`);
  - volume `acbaddf8-…`;
  - B `8b9c6c0b…`, U `54d87a31-…`;
  - guest boot `5dd879ce-…`;
  - installed admission manifest `c0b6e568…`.
- **Guard:** one SIGTERM at 15:48:52Z to the root guard PID 93960, gone by 15:48:57Z (first check). `runtime_running_at_guard_exit` False, `runtime_exit` 0, exporter pid 93972, no `stop-uncertain.json`.
- **984 timing:** ssh-noop median 0.216 s; OBSERVE net median 0.498 s, max 0.546 s over 3 samples (`net_max_below_984_bound: true`).
- **Phase 11:**
  - the guest powered off at 15:50:00Z, and the child was gone at 15:50:05Z (first try, pattern `[h]fs-zq3-r25`);
  - host cleanup destroyed the tap and the vmm, and the UEFI vars stayed unchanged (`a0b33064…`);
  - the native fusefs mount was unmounted normally;
  - the f3 observer key was deleted;
  - the window was restored (`mount_restored: true`).
- **Restore proof (final state):**
  - Rocky runs the stock `5.14.0-741.27.1.el9_9` = grub default = `saved_entry`, with `next_entry` empty (`check --expect stock` ok);
  - Rocky is Enforcing, with no `/.autorelabel`, one journal boot and Rocky swap unused;
  - no `hfs-xz3` file in `/boot`, and no `/lib/modules/6.1.190-hfs-xz3` or `/etc/homelabfs` (U2);
  - on f3, Rocky is the only running VM, `/dev/vmm` holds only `rocky` and swap is 0;
  - no fdescfs/fusefs mount; `fdescfs.ko` (42) and `fusefs.ko` (43) stay loaded by the reviewed plan, so the next run needs Phase 1a again;
  - the f3 observer directory holds only `rocky-known_hosts`, and Rocky's `authorized_keys` is back to `e08542dc…`.
- **Operator notes:**
  - every line ran through a bash wrapper that sourced `session.sh` and `vars.env`, with stdin from `/dev/null`; zsh never ran a session line;
  - every wait (READY, `summary.json`, guard-gone, child-gone, `rockywait`) was a bounded foreground loop, and no Monitor, timer or notification wakeup was armed (R23-2 check);
  - some hand-written times inside early run-log lines ran about a minute ahead; the tool-output stamps are authoritative;
  - Phase 11's ETIMEDOUT scan line found no release or T6-T11 file to scan (the helper printed its usage), as in zq3-r21 to zq3-r24;
  - the Phase 9 `LSM: initializing` grep matched nothing again (`SELinux:  Initializing.` was present).

## Follow-up

- **R25-1 (R24-1 again on the control lane):** with gn4, T1's probe syscalls all pass, including the 1024-group child's read of `group-tail`. Then the retention inventory's `lstat` of `setgid/created` fails `ENOSPC`. m0's control lane is at 98.1% of its bytes, the round's `view retire` returns `-11`, and the retry fails (`charge still failed after the retry: error -28`). The host holds no lease or ticket. A follow-up task should account for the guest's live release-ticket charges under gn4's charge-before-acquire order (see the fix list under R25-1).
  - **hp4 (done):** the lane held 13 answered VIEW_RELEASE tickets of the T1 process. Every VIEW_RETIRE of them met T1's next observation lease (the guest releases leases asynchronously), and gn4's per-credential host rule answered BLOCKED. The rule is now per ticket (see "Proven cause" under R25-1).
  - **The r26 candidate needs the daemon rebuilt** (`internal/viewcontrol/retirement.go`, `internal/controlledruntime/facts.go`). The guest patches 0003/0004 changed in comments only, so the kernel is rebuilt only for the source pin, with no change in behaviour. The exporter and the fdescfs window are unchanged.
  - **Still open:** a chain of 16 or more back-to-back observations sharing one lease would meet the host's Tickets bound of 16 (re4's back-off). The 178 MB control base is not decoded.
- **R25-2:** a truncated owner snapshot and a tool-moved probe line; record only. A future sheet could make the Phase 11 `*owner*.tar` glob skip unreadable archives, or check each snapshot's `rc`.
- **Validated by this run:**
  - the gn4 daemon's process-observer qualification (first f3 run) and its launch;
  - gn4's guest logging (`view retire <errno>`; the `charge still failed after the retry` line);
  - gn4's charge order and per-credential `checkRetire` on the host: no stranded lease, Leases and Tickets 0/0 after T1;
  - the 1024-group access child's read of `group-tail`, and every T1 probe step after it (sj4, s64 fd calls, setgid/sticky semantics);
  - km4 (no `VIEW_RETIRE` block), tj4/wj4 (advances 30), vj4 (self_admissions 14);
  - block 2a (Phase 9 about 70 s after READY) and the R23-2 wait rule;
  - the `8cadae40` exporter launch, `hfs_fdattr` mount options, and R13-1 (both unmounts `rc 0` after the guard stop).
- **Still without a real run:** T2-T11 (the s64 peer-rename guard, T7, hh4/dh4 in T9-T11). T5 stays deferred.
- **The iormesh session can be told that f3 is free** (run over at about 15:52Z).
