# zq3-r23: km4 lets T1's `create(group-tail)` and its fd calls pass, but the 1024-group access child's open of `group-tail` fails ENOSPC after a retire round returns `-11` (2026-11-27)

Run sheet: [filled plan](../20261126-zq3-r23-filled-plan/run-sheet.md)
(run-day refresh merge `664dcb2`; tools snapshot `f584de4`).

Candidate: the r23 rebuild `21c49a55…` ([xz3-20261126-r23](../../../../tools/integration/artifacts/evidence/xz3-20261126-r23/README.md)):
- the go1.27.4 km4 daemon `43238fe9…`, first run;
- the guest kernel, build ID `2435216f…` (km4's `retired under charge pressure` log line), first run;
- the exporter `8cadae40…` (zq3-r21's bytes, third run).

`hfs_fdattr` was enabled on both mounts. Times are UTC. Private evidence:
`/home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/evidence` (run log `RUN-LOG.md`).

## Before the run

- Paul gave the final GO for the sheet at about 22:47Z on 2026-11-26 (A1-A8, including the Phase 1a unload, T7, the Rocky stop, the controlled launch and both signatures by Claude through the watcher), conditional on a clean run-day refresh. The refresh at 22:56:44Z was a record refresh only (dnf #69: `rsync` and `libgs`, nothing pinned changed). T5 stays deferred.
- Rocky ran zq3-r22's Phase 12 incarnation (PID 50982, boot `99de7fb2-…`).
- The iormesh session confirmed iom1-3 Stopped and held them until told the run was over. They stayed stopped through the whole run, and the sheet never touched them.
- f3 swap was 0, so there was no Phase 0b.

## Outcome

Phases 0 to 9 passed. Phase 9 started about 1 minute after READY: block 2a's bounded log poll worked (R22-2 is fixed). T1 (`credentials`) got further than ever before, but failed with a new finding, R23-1. T2 to T11 did not run. Phase 11 took the guard-SIGTERM branch, and restoration was clean. **Rocky was down for 5 min 47 s** (zq3-r22: 25 min 42 s).

| Phase | UTC | Result |
|---|---|---|
| 0 | 23:02-23:03 (2026-11-26) | passed |
| 1 | 00:16 | passed (nightly gate GO at 00:16Z) |
| 1a | 00:17 | passed (fusefs id 40, fdescfs id 39, `rc 0` each) |
| 2 | 00:17 | passed |
| 3 | 00:17-00:18 | passed (non-default kernel entry, PM `34a71569…`, 82 entries) |
| 4 | 00:18-00:19 | passed (step 1a qualified `43238fe9…`) |
| 5 | 00:19:23-00:20:01 | passed (gate GO at 00:19Z, UMA drain, one-shot select, Rocky stopped) |
| 6 | 00:20:09 | passed (margin 0.26 GB) |
| 7 | 00:20:18-00:20:23 | passed |
| 8 | 00:20:38-00:21:54 | passed (READY at 00:21:10Z, `usable`, L = 2) |
| 9 | 00:22:22-00:22:34 | passed |
| T1 `credentials` | 00:22:40-00:23:38 | **fail** (R23-1, ENOSPC in access child 3) |
| T2 `sync`, T3 `metadata` (s64 peer-rename guard), T4 `namespace` | - | unrun (T1 failed) |
| T5 `mapped-pressure` | - | unrun (deferred, re-plan pending) |
| T6, T7 (pod), T9, T10, T11, T8 | - | unrun (T1 failed) |
| 11 | 00:24:05-00:25:39 | completed (guard-SIGTERM branch) |
| 12 | 00:25:43-00:26:23 | completed; Rocky on stock 741.27.1, custom kernel removed |

The run started about 50 minutes later than planned. The operator's timer for the 23:30Z N1 bound fired at 23:27Z, but the operator only resumed when the orchestrator prompted it at 00:15Z. Nothing on any host changed between Phase 0 (23:03Z) and Phase 1 (00:16Z), and Phase 1 was taken fresh at 00:16Z. The delay came before anything changed on a host, so Rocky's downtime did not grow. From then on every wait was a bounded loop in the foreground.

**What held:**
- **Phase 0 recheck:** all pins, `argv.py verify`, `check-argv` and `bootstrap.py check` passed. The workstation ssh was still `d4c70b09…`, and `git diff f584de4 664dcb2` over the tools was empty.
- **Phase 1:** every fact matched the refresh:
  - f3 boot `1794574308`, Rocky PID 50982, boot `99de7fb2-…`, UEFI vars `4f63d71b…`, swap 0;
  - iom1-3 Stopped and `/dev/vmm` = `rocky`;
  - Rocky runs `741.27.1` = grub default = `saved_entry`, with `next_entry` empty;
  - stamp `2026-11-27`, dnf #69 (`dnf_unchanged: true`), the pod runtime unchanged.
- **Phase 3:** the kernel entry `…-6.1.190-hfs-xz3` was added as a non-default entry, and `check --expect stock` passed. The pod rootfs manifest is the predicted `34a71569…`.
- **Phase 4:**
  - step 1a qualified the new km4 daemon `43238fe9…` (uid/gid 0, groups `[0,0,5]`, pid 5973);
  - the window plan `051e8d34…` was set up and qualified, and fdescfs was supported;
  - fusefs was loaded (fdescfs id 40, fusefs id 41);
  - the observer key was enrolled (`EA` `c88c67ee…`).
- **Phase 5:** the estimate margin was +0.09 GB, so the UMA drain ran and freed about 0.24 GB. `select` set only the one-shot `next_entry`, and the stock default stayed.
- **Phase 6:** passed with a 0.26 GB margin (15,022,441,720 bytes).
- **Phase 8:**
  - launch at 00:20:38Z, READY at 00:21:10Z (32.1 s);
  - both signatures by the watcher;
  - mounts 391 and 408;
  - `usable` passed with L=2;
  - the exporter launched cleanly, and `exporter.stderr` stayed empty.
- **R22-2 fixed (block 2a):** the driver ran in the background. The bounded `grep READY` poll returned at 00:21:14Z, the `summary.json` wait at 00:21:58Z, and Phase 9 started at 00:22:22Z, 72 s after READY (`late_after_ready: false`). The operator never waited for a task notification.
- **`hfs_fdattr`, third VM run:** both mountinfo rows read `rw,cache=mmap,hfs_fdattr,msize=65536,trans=virtio,version=9P2000.L.hfs1.4`. T1's `fchown` and `fchmod` through the open description returned 0 for `group`, and for the first time also for `group-tail`.
- **Phase 9:** passed with 3496 s of guard budget left.
  - OBSERVE seq 3, with `Open 0` on both providers;
  - Enforcing; no hfs block line;
  - 984 timing: SSH no-op median 0.206 s, OBSERVE net median 0.442 s, max 0.62 s (3 samples).
- **R22-1 fixed (km4):** T1's `create-exclusive(group-tail)` returned 0 in 5.8 s, then its `fchown 41002:44022`, `fchmod 0640` and 15-byte `pwrite` all returned 0. There was no `VIEW_RETIRE (-110)` line and no block line of any kind during T1. Both sessions stayed usable until the guard stopped the runtime.
- **R20-1 stays fixed (sj4):** access child 1 read `group` below the 0700 root (status 0). Child 2 got EACCES as designed.
- **xf4 and vj4 still work.** `recalls.py` reported:
  - `advances` 9 and `self_admissions` 9;
  - only the 2 bootstrap `RELEASE/VIEW_RELEASE` recalls (DOMAIN, 5.6 s and 4.1 s);
  - no CALL_CONFLICT and no EXPECTED_TARGET recall.
- **T1 admission:** 8.1 s. The largest gap was 2.1 s at `open-root[0] → stat-root[0]`. No deadline was hit and there was no ETIMEDOUT.
- **No guest WARNING or BUG, no AVC, no host fence.** `runtime.stderr` and `exporter.stderr` were empty, and both fence records report `fenced: false`.
- **Unmounts after the guard stop:** both returned `rc 0` in 0 s, with no new block line (R13-1 stays fixed).
- **Phase 12:** Rocky started on stock `741.27.1`. It ran once in vm-bhyve, with no relabel. U2 removed the test kernel.

## R23-1: the 1024-group access child's open of `group-tail` fails ENOSPC

**What happened.** T1 started at 00:22:44Z and ended after 54.4 s with `dispatching-uncertain` ("Refused: required actual syscall case failed: credentials").

The guest.py named events (`ctrl-run/credentials/stdout.log`; guest monotonic seconds), each with errno 0 unless noted:
1. `mkdir-exclusive` (3.8 s), `open-leaf` (4.9 s), `fchmod-leaf 0777`;
2. `create-exclusive group`, `fchown 41002:42002`, `fchmod 0640`, `pwrite` of 12 bytes;
3. access child 1 (uid 41001, groups `[42001,42002,42003]`) read `group-secret` (status 0). Access child 2 was denied with EACCES (status 256), the expected outcome;
4. **`create-exclusive(group-tail)`** (154.81 → 160.64 s), **`fchown 41002:44022`**, **`fchmod 0640`** and the 15-byte **`pwrite`** (done at 162.38 s): **all errno 0**. Earlier runs stopped here: R21-1 failed the `fchmod`, R22-1 the `create`;
5. **access child 3**, the 1024-group credential child, failed with `OSError` **errno 28 (`ENOSPC`)**: `No space left on device: 'group-tail'`. The parent recorded its exit at 166.50 s;
6. the parent refused the case at 169.32 s: "all 1024 groups including final entry must survive" (`errno` null, because the parent itself made no failing syscall). The terminal record came at 173.34 s.

The guest kernel log during T1 (the full `dmesg` has no other hfs line before the guard stop):

```text
[158.148573] 9p-hfs: session a19f095f-… retired under charge pressure: own retire 0 freed 1 requests, 0 of 1 other sessions freed 0; retrying the charge once
[166.546877] 9p-hfs: session a19f095f-… retired under charge pressure: own retire 0 freed 5 requests, 0 of 1 other sessions freed 0; retrying the charge once
[166.546939] 9p-hfs: session a19f095f-… retired under charge pressure: own retire -11 freed 4 requests, 0 of 1 other sessions freed 0; nothing freed, the charge fails
```

Session `a19f095f-…` is m0 (`hfs0`). The only other hfs lines came at 206.2 s: the expected `(-112), exporter ERROR 5` block and worker-stop pairs for both sessions after the guard's SIGTERM.

**Host side (read-only):**
- The failure OBSERVE (seq 5, the yf4 direct form) was OBSERVED, SERVING and complete. It was taken about 13 s after the case ended.
- m0's provider ledger: Allocated 62, **Retired 62**, Open 0.
- m1's: Allocated 13, Retired 10, Open 0.
- The view controller had Leases 0, Reservations 0, Tickets 0, no recall, and `Matches` true.
- `runtime.stderr` and `exporter.stderr` were empty, and `t-fail-fence.json` and `p11-fence.json` report `fenced: false`.

**What the evidence shows:**
- **The `ENOSPC` is guest-local again:** the host recorded no refusal and no fence. By the failure OBSERVE, every m0 claim was retired and the view controller held nothing.
- **km4's 6m4 retry worked twice and then failed.** At 158.15 s (around the `create(group-tail)` sequence) a round freed 1 request and retried the charge. At 166.55 s two rounds came 62 µs apart. The first freed 5 requests and retried. The second got **`own retire -11` (`-EAGAIN`)**, which counts as nothing freed, so its charge failed `ENOSPC`.
- **The two rounds 62 µs apart point at two concurrent charges on m0.** The second `hfs_retire` most likely found `session->retirement` held by the first round, returned `-EAGAIN`, and `hfs_retire_shared` then gave up instead of waiting for that round or retrying behind it. That is the "local retirement mutex busy" case, which 1h4 handles for view acquires but 6m4/km4 does not handle for call charges. This is an inference from the log lines. The code path is not confirmed yet.
- **The `freed 4 requests` next to `-11`** suggests the counter reports requests freed by the concurrent round, or a partial count. Either way, the round still logged `nothing freed`.
- **No session blocked:** there is no `blocked by … (-28)` line and no `VIEW_RETIRE (-110)` line. The case failed cleanly with an errno. The sessions stayed usable, unlike R21-1 and R22-1.

**Timing caveat.** The parent recorded child 3's exit at guest monotonic 166.502 s (`CLOCK_MONOTONIC`), about 45 ms *before* the dmesg timestamp of the failing round (166.547 s, the printk clock). The km4 line is rate-limited, so the round that failed the child's own charge may have gone unlogged. The 166.55 s pair would then come from the cleanup after the child exited, such as its `DESCRIPTION_RELEASE`/`PROCESS_EXIT`. The two clocks are close but not the same. So the evidence does not prove which charge the `-11` round belonged to. It does show that a km4 round can return `-EAGAIN` and fail its charge while another round on the same session is freeing requests.

**Which syscall in the child failed.** guest.py's child report names only the path (`'group-tail'`) and errno 28. From the case logic it is the child's `open` of `group-tail` under the 1024-group credentials. The failing call was not the `create` or the fd-based `fchown`/`fchmod` (those passed in the parent), so this is not a finding against s64.

`credentials-owner-live.tar` and `credentials-owner-after.tar` (BoltDB ledgers for the controller, both providers and the transport) and `f3-evidence.tar` hold the host state. The full guest `dmesg` is in `p11-guest-dmesg.txt`.

Ruled out:
- **R22-1 again:** no `VIEW_RETIRE` block. km4's `BLOCKED` answer kept the cleanup worker moving.
- **A host fence or refusal:** none recorded, both stderr files empty.
- **ETIMEDOUT or a deadline:** `etimedout` false and `deadline_hit` false. The case took 54 s of its 240 s.
- **Recall interference:** only the two bootstrap recalls (00:20:59Z and 00:21:06Z) ran, before T1.

**Effect.** T2-T11 did not run. The T3 s64 peer-rename guard, T7 (pod) and hh4/dh4 in T9-T11 remain without a real run.

**For the fix (task to file):** under charge pressure, `hfs_retire_shared` should treat a busy local retirement (`-EAGAIN` from `hfs_retire`) as "another round is in progress". It should wait for that round, or retry the charge after it, instead of failing `ENOSPC`. Separately, guest.py's child report should name the syscall (yf4 names only parent syscalls), and the guest budget should be recorded, so that the next run can tell which charge and lane ran short.

## R23-2 (operator): the run started about 50 minutes late

- **What happened:** to respect the N1 gate (no GO before 23:30Z), the operator armed a monitor that fired at 23:27Z. It then waited for that notification instead of polling in the foreground, and it did not resume until the orchestrator's message at 00:15Z.
- **Effect:** no host had been touched since Phase 0. Phase 1 was taken fresh at 00:16Z, and the gates passed: nightly gate GO, latest Phase 5 start 21:25Z. Rocky's downtime was not affected.
- **For the next run:** pre-window waits, like the R22-2 rule for the driver, must be bounded loops in the foreground (`timeout 540 sh -c 'until …; do sleep 5; done'`, repeated), never a notification or monitor wakeup.

## km4 log-line observations

- Three `retired under charge pressure` lines, all for m0 (above). Two rounds retired and retried (freed 1, then 5). The third returned `own retire -11` and failed its charge.
- No other session freed anything in any round (`0 of 1 other sessions freed 0`). m1's own claims were not the shortage.
- No `blocked by uncertain control result on VIEW_RETIRE (-110)` line: **R22-1 did not recur**. No `blocked by … (-28)` line, no `view control worker stopped` line before the guard stop, and no `WARNING`/`BUG`.
- None of the other expected lines appeared during T1: no re4/1h4 `local retirement mutex busy` or BLOCKED re-check lines, no ie4 `acquiring again` lines and no `retry limit reached`.

## Host events and facts

- **f3:** boot `1794574308` unchanged. The iormesh VMs iom1-3 were Stopped at Phase 1, at Phase 5 step 1 and at Phase 12. No k3s VM was listed.
- **N1:** stamp `2026-11-27`, dnf #69 unchanged. The nightly gate printed GO at 00:16Z and 00:19Z. The run ended at 00:26Z, far ahead of the 21:25Z bound.
- **Rocky:**
  - before the run: PID 50982 (Thu Nov 26 14:51:24Z), boot `99de7fb2-…`;
  - `stop-rocky` at 00:19:57Z (`Stopped` by 00:20:01Z);
  - started at 00:25:44Z: PID 10896 (lstart Fri Nov 27 00:25:44Z), boot `f39e32c7-0bd1-4fc6-a94e-5c946aed0a3f`;
  - pinned SSH answered at 00:26:01Z on the first try.
  - **Down 5 min 47 s** (6 min 4 s to SSH).
- **Run values:**
  - window plan `051e8d34…`;
  - observer EA `c88c67ee…`, removed (`restored_pre_run: true`, `authorized_keys` back to `e08542dc…`);
  - volume `a9d8b116-…`;
  - B `c26654c5…`, U `1c452625-…`;
  - guest boot `7c796f7c-…`;
  - installed admission manifest `e48a7e66…`.
- **Guard:** one SIGTERM at 00:24:10Z to the root guard PID 8037, gone by 00:24:16Z. `runtime_running_at_guard_exit` False, `runtime_exit` 0, exporter pid 8048.
- **Phase 11:**
  - the guest powered off at 00:25:05Z, and the child was gone at 00:25:17Z;
  - host cleanup destroyed the tap and the vmm, and the UEFI vars stayed unchanged (`4f63d71b…`);
  - the native fusefs mount was unmounted normally;
  - the f3 observer key was deleted;
  - the window was restored (`mount_restored: true`).
- **Restore proof (final state):**
  - Rocky runs the stock `5.14.0-741.27.1.el9_9` = grub default = `saved_entry`, with `next_entry` empty (`check --expect stock` ok);
  - Rocky is Enforcing, with no `/.autorelabel` and systemd `running`, and Rocky swap is unused;
  - no `hfs-xz3` file in `/boot`, no hfs-xz3 BLS entry, and no `/lib/modules/6.1.190-hfs-xz3` or `/etc/homelabfs` (U2);
  - on f3, Rocky is the only running VM, `/dev/vmm` holds only `rocky` and swap is 0;
  - no fdescfs/fusefs mount; `fdescfs.ko` (40) and `fusefs.ko` (41) stay loaded by the reviewed plan, so the next run needs Phase 1a again;
  - the f3 observer directory holds only `rocky-known_hosts`, and Rocky's `authorized_keys` is back to `e08542dc…`.
- **Operator notes:**
  - every line ran through a bash wrapper that sourced `session.sh` and `vars.env`, with stdin from `/dev/null`; zsh never ran a session line;
  - the guard-gone wait ran as a bounded loop (33 × 10 s) and saw `guard-gone` on its first try;
  - Phase 11's ETIMEDOUT scan line found no release or T6-T11 file to scan (the helper printed its usage), as in zq3-r21 and zq3-r22;
  - the Phase 9 `LSM: initializing` grep matched nothing again (sheet wording; `SELinux:  Initializing.` was present).

## Follow-up

- **R23-1:** a km4 6m4 round that meets a busy local retirement (`-EAGAIN`) fails its charge `ENOSPC` instead of waiting. The access child with 1024 groups then fails to open `group-tail`. To do:
  - fix the guest retry;
  - name the child syscall in guest.py;
  - record the guest budget.

  **9n4 (cause proven, fixed in the guest kernel).** See [linux9p README, `9n4`](../../../../tools/integration/linux9p/README.md#a-round-that-waited-for-a-concurrent-retirement-retries-9n4).
  - **The `-11` was not a busy mutex.** `hfs_retire` waits for `session->retirement` with a blocking `mutex_lock`. Its `-EAGAIN` means that nothing was retirable once it held the mutex, and such a round frees nothing.
  - **What happened:** the second round counted `nr_requests` before it waited. It waited out the first round, which freed 5 requests, and then found nothing left. `freed 4` is the first round's 5 frees, less one request admitted meanwhile. `hfs_retire_shared` returned that `-EAGAIN`, so the charge failed with ENOSPC although its bytes had just been returned.
  - **The timing caveat is settled.** The guest `dmesg` line `sched_clock: Marking unstable (15327372088, 46573846)<-(…)` at 15.37 s puts the printk clock at `CLOCK_MONOTONIC` + 46.57 ms. So the pair ran at monotonic 166.5003 s, 1.3 ms before the parent recorded child 3's exit (166.5016 s). The `-11` round was the child's own charge. The rate limit dropped nothing (three lines in all).
  - **The fix:** a retirement generation that `hfs_retire` advances whenever it frees requests. A charge reads it before its attempt. `hfs_retire_shared` returns 0, so the charge retries once, when the generation moved even though its own round freed nothing. The km4 line now names the byte lane and has the outcome `a concurrent round freed requests, retrying the charge once`.
  - **Red first:** controlled_received `TestControlledReceivedActualBusyRetirementRetry` reproduced r23's line (`own retire -11 freed 2 requests … nothing freed, the charge fails`, `-28`).
  - **Done:** guest.py child replies now carry `"syscall"`, and the group refusals name the child's syscall and errno.
  - **Not done, and not possible from existing data:** the guest budget usage. Neither the guest sysfs snapshot nor the host OBSERVE carries guest lane counters. The r23 line did not name the lane either. The ordinary lane is the likely one.
  - **Rebuild for r24:** the guest kernel only. The daemon and the exporter are unchanged.
- **R23-2:** the operator's pre-window wait (above).
- **Validated by this run:**
  - km4 (R22-1 fixed: `create(group-tail)` and its fd calls pass, no `VIEW_RETIRE` block);
  - block 2a (R22-2 fixed: Phase 9 72 s after READY);
  - the km4 daemon's process-observer qualification;
  - sj4 (R20-1);
  - tj4/wj4 (advances 9);
  - vj4 (self_admissions 9, no CALL_CONFLICT);
  - the `8cadae40` exporter launch;
  - `hfs_fdattr` mount options and fd-based `fchown`/`fchmod`;
  - R13-1 (unmounts in 0 s after the guard stop).
- **Still without a real run:** T2-T11 (the s64 peer-rename guard, T7, hh4/dh4 in T9-T11). T5 stays deferred.
- **The iormesh session can be told that f3 is free** (run over at 00:26:23Z).
