# zq3-r24: 9n4's retry runs once but the 1024-group access child's open of `group-tail` still fails ENOSPC, with the guest control lane 98% full (2026-11-27)

Run sheet: [filled plan](../20261127-zq3-r24-filled-plan/run-sheet.md)
(fill merge `404995d`; tools snapshot `87b9bf2`).

Candidate: the r24 rebuild `c9500232…` ([xz3-20261127-r24](../../../../tools/integration/artifacts/evidence/xz3-20261127-r24/README.md)):
- the guest kernel, build ID `8ce8bd6e…` (9n4: one retry when a concurrent round freed requests; the km4 line names the charged lane and its usage), first run;
- the go1.27.4 km4 daemon `43238fe9…` (zq3-r23's bytes, second run);
- the exporter `8cadae40…` (zq3-r21's bytes, fourth run).

`hfs_fdattr` was enabled on both mounts. Times are UTC. Private evidence:
`/home/paul/tmp/claude/hfs-xz3-20261127-r24/zq3-r24-fill/evidence` (run log `RUN-LOG.md`).

## Before the run

- Paul gave the final GO at about 03:52Z ("GO now + copy key"): run today with no refresh. That covered A1-A8, including the Phase 1a unload, T7, the Rocky stop, the controlled launch and both signatures by Claude through the watcher. The fill's facts are from 03:23Z, after dnf #69. T5 stays deferred.
- The operator key was copied into the fill's `operator/` before the run. Only `sign_watch.py` used it, through the signer. The operator never read it.
- Rocky ran zq3-r23's Phase 12 incarnation (PID 10896, boot `f39e32c7-…`).
- The iormesh session had been asked to keep iom1-3 Stopped until told r24 was over. They stayed Stopped through the whole run, and the sheet never touched them.
- f3 swap was 0, so there was no Phase 0b.

## Outcome

Phases 0 to 9 passed again. Phase 9 started 63 s after READY. T1 (`credentials`) failed exactly where zq3-r23's did: access child 3, which holds all 1024 groups, failed its `open` of `group-tail` with `ENOSPC` (**R23-1 recurred**). 9n4's new path never fired. The only km4/9n4 line was an ordinary retry: one request freed, with the guest's **control lane at 98.1% of its bytes** (R24-1). T2 to T11 did not run. Phase 11 took the guard-SIGTERM branch, and restoration was clean. **Rocky was down for 6 min 46 s** (7 min 5 s to SSH; zq3-r23: 5 min 47 s).

| Phase | UTC | Result |
|---|---|---|
| 0 | 06:41:58-06:42:42 | passed |
| 1 | 06:42:52-06:43:26 | passed (nightly gate GO at 06:43Z) |
| 1a | 06:43:30-06:43:48 | passed (fusefs id 41, fdescfs id 40, `rc 0` each) |
| 2 | 06:43:48-06:44:55 | passed |
| 3 | 06:44:55-06:46:46 | passed (non-default kernel entry, PM `34a71569…`, 82 entries) |
| 4 | 06:46:46-06:48:04 | passed (step 1a qualified `43238fe9…`, pid 42987) |
| 5 | 06:48:04-06:48:47 | passed (gate GO at 06:48Z, UMA drain, one-shot select, Rocky stopped) |
| 6 | 06:48:50-06:49:11 | passed (margin 0.26 GB) |
| 7 | 06:49:11-06:49:24 | passed |
| 8 | 06:49:29-06:51:15 | passed (launch 06:49:30Z, READY 06:50:16Z, `usable`, L = 2) |
| 9 | 06:51:19-06:51:44 | passed |
| T1 `credentials` | 06:51:45-06:52:45 | **fail** (R23-1 again: ENOSPC in access child 3; R24-1) |
| T2 `sync`, T3 `metadata` (s64 peer-rename guard), T4 `namespace` | - | unrun (T1 failed) |
| T5 `mapped-pressure` | - | unrun (deferred, re-plan pending) |
| T6, T7 (pod), T9, T10, T11, T8 | - | unrun (T1 failed) |
| 11 | 06:52:50-06:55:25 | completed (guard-SIGTERM branch) |
| 12 | 06:55:26-06:56:23 | completed; Rocky on stock 741.27.1, custom kernel removed |

The run started at once after the GO handoff. It needed no N1 earliest-time wait, because the gate gave GO at 06:43Z. Every wait was a bounded loop in the foreground (R23-2 did not recur).

**What held:**
- **Phase 0:** all pins, `argv.py verify`, `check-argv`, `bootstrap.py check` and the synthetic batch check passed. The workstation ssh was still `d4c70b09…`, and `git diff 87b9bf2 404995d` over the tools was empty.
- **Phase 1:** every fact matched the 03:23Z facts:
  - f3 boot `1794574308`, Rocky PID 10896 (lstart Fri Nov 27 00:25:44 2026), boot `f39e32c7-…`, UEFI vars `e32f7926…`, swap 0;
  - iom1-3 Stopped, no k3s VM, `/dev/vmm` = `rocky`;
  - Rocky runs `741.27.1` = grub default = `saved_entry`, with `next_entry` empty;
  - stamp `2026-11-27`, dnf #69 (`dnf_unchanged: true`), `pod-runtime-unchanged: True`.
- **Phase 3:** the kernel entry `b26c0ac5…-6.1.190-hfs-xz3` was added as a non-default entry, and `check --expect stock` passed. The pod rootfs manifest is the predicted `34a71569…` (82 entries).
- **Phase 4:**
  - step 1a qualified the daemon `43238fe9…` again (uid/gid 0, groups `[0,0,5]`, pid 42987);
  - the window plan `ac356cb4…` was set up and qualified, and fdescfs was supported;
  - fusefs was loaded (fdescfs id 41, fusefs id 42);
  - the observer key was enrolled (`EA` `9b620f06…`).
- **Phase 5:** the estimate margin was +0.17 GB, so the UMA drain ran and freed 0.16 GB. `select` set only the one-shot `next_entry`, and the stock default stayed. `stop-rocky` ran from 06:48:41Z to 06:48:47Z.
- **Phase 6:** passed with a 0.26 GB margin (15,021,433,264 bytes).
- **Phase 8:**
  - launch at 06:49:30Z, READY at 06:50:16Z (46 s);
  - both signatures by the watcher (`"watcher": "signed"` ×2);
  - mounts 391 and 408;
  - `usable` passed with L=2;
  - the exporter launched cleanly, and `exporter.stderr` and `runtime.stderr` stayed empty.
- **Block 2a (R22-2 stays fixed):** the driver started in the background at 06:49:29Z. The bounded `grep READY` poll returned at 06:50:21Z, the `summary.json` wait at 06:51:04Z, and Phase 9 started at 06:51:19Z (`late_after_ready: false`). The background task's completion notice was never waited for.
- **`hfs_fdattr`, fourth VM run:** both mountinfo rows read `rw,cache=mmap,hfs_fdattr,msize=65536,trans=virtio,version=9P2000.L.hfs1.4`.
- **Phase 9:** passed, with 3490 s of guard budget left (guard end 07:49:30Z).
  - OBSERVE seq 3, with `Open 0` on both providers;
  - Enforcing, enforce 1; `SELinux:  Initializing.` present; `no-hfs-block`.
- **T1, as far as zq3-r23 got (km4, sj4, s64):** admission took 8.26 s, with the largest gap 2.27 s at `open-root[0] → stat-root[0]`. `mkdir`, `open-leaf`, `fchmod 0777`, `create-exclusive group`, `fchown 41002:42002`, `fchmod 0640` and the 12-byte `pwrite` returned 0. Access child 1 read `group-secret` (status 0), and child 2 got `EACCES` as designed (now named: `"syscall": "open"`). `create-exclusive(group-tail)` returned 0 in 4.76 s, then `fchown 41002:44022`, `fchmod 0640` and the 15-byte `pwrite` all returned 0. There was no `VIEW_RETIRE (-110)` line and no block line during T1.
- **xf4 and vj4 still work.** `recalls.py` reported:
  - `advances` 9 and `self_admissions` 10;
  - only the 2 bootstrap `RELEASE/VIEW_RELEASE` recalls (DOMAIN, 4.6 s and 5.4 s);
  - no CALL_CONFLICT and no EXPECTED_TARGET recall.
- **No guest WARNING or BUG, no AVC, no host fence.** `t-fail-fence.json` and `p11-fence.json` both report `fenced: false`.
- **Unmounts after the guard stop:** both returned `rc 0` within 2 s of wall time (SSH included), with no new block line (R13-1 stays fixed).
- **Phase 12:** Rocky started on stock `741.27.1`, with one vm-bhyve run and no relabel. U2 removed the test kernel.

## R24-1 (R23-1 again): the 1024-group child's open fails ENOSPC while the control lane is 98% full; 9n4's retry path never fires

**What happened.** The probe started at 06:51:50Z and ended after 55.1 s with `dispatching-uncertain` ("Refused: required actual syscall case failed: credentials").

The guest.py named events (`ctrl-run/credentials/stdout.log`; guest monotonic seconds):
1. `create-exclusive(group-tail)` 170.263 → 175.025 s, then `fchown`, `fchmod` and `pwrite` (done at 176.757 s): all errno 0;
2. **access child 3** (uid 41001, all 1024 groups), recorded at 181.083 s: `{"syscall": "open", "errno": 28, "error": "OSError", "detail": "[Errno 28] No space left on device: 'group-tail'"}`. guest.py's 9n4 child syscall naming worked as designed;
3. the parent's refusal at 183.276 s: "all 1024 groups including final entry must survive (child open errno 28)". The terminal record came at 187.550 s.

The only km4/9n4 line in the full guest `dmesg` (`p11-guest-dmesg.txt`, line 450):

```text
[181.123684] 9p-hfs: session f8b0bf14-… retired under charge pressure: own retire 0 freed 1 requests, 0 of 1 other sessions freed 0, control lane used 526456646 of 536870912 bytes, 368 of 512 slots; retrying the charge once
```

Session `f8b0bf14-…` is m0 (`hfs0`). The other hfs lines all came at 231.0 s: the expected block and worker-stop pairs after the guard's SIGTERM (m0 `VIEW_WAIT (-116), exporter ERROR 4`; m1 `VIEW_WAIT (-112), exporter ERROR 5`).

**Timing.** The guest `sched_clock: Marking unstable (23732462540, 42207415)` line puts the printk clock at `CLOCK_MONOTONIC` + 42.21 ms. So the round ran at monotonic 181.0815 s, 1.4 ms before the parent recorded child 3's exit (181.0829 s). This was the child's own `open` charge. The round freed 1 request and retried once. The retry failed `ENOSPC`. A failed retry logs no line of its own, so the line's outcome is the last word.

**Host side (read-only):**
- The failure OBSERVE (seq 5, the yf4 direct form) was OBSERVED.
- m0's provider ledger: Allocated 62, **Retired 62**, Open 0 (as in zq3-r23).
- m1's: Allocated 13, Retired 10, Open 0.
- **The view controller held Leases 8 and Tickets 22**, with Reservations 0, no recall and `Matches` true. zq3-r23's failure OBSERVE showed 0/0/0. Phase 9 and the T1 baseline (seq 3 and 4) showed 0 leases and 0 tickets.
- `runtime.stderr` and `exporter.stderr` were empty. `t-fail-fence.json` and `p11-fence.json` report `fenced: false`.

**What the evidence shows:**
- **R23-1 recurred** with the same symptom: the same child, the same `open`, the same errno, and m0's ledger fully retired. The `ENOSPC` is guest-local again: the host recorded no refusal and no fence.
- **The charged lane is the *control* lane, not the ordinary lane** that the zq3-r23 record guessed. It held 526,456,646 of 536,870,912 bytes (98.1%, about 10.4 MB free) and 368 of 512 slots, so it ran short on bytes, not slots. That is the new 9n4 lane-usage field at work.
- **9n4's new path did not apply.** There was no concurrent round (`0 of 1 other sessions freed 0`, and no second line), so there was no `a concurrent round freed requests` retry, and there was no `nothing freed` line either. The ordinary 6m4/km4 path ran: the round freed 1 request, retried once, and the retry still did not fit. The 9n4 fix was aimed at a race (zq3-r23's `own retire -11`) that did not occur here. The underlying shortage remains.
- **Likely cause (inference, not confirmed in code):** the control lane is close to full of charges that one round cannot retire. `hfs_retire` retires only the oldest run of claims with one set of credentials per round (a 9n4 known limit), so a round freed one small request against a lane short by more than that. The 8 leases and 22 view tickets the host still held at the failure OBSERVE (of `ControlTickets` 24) fit a control lane full of held view state from T1's several credential sets (the T1 process, children 1-3). That correlation is from one OBSERVE about 25 s after the case. The guest's own per-charge accounting is not visible on the host.
- **No session blocked:** there was no `blocked by … (-28)` line and no `VIEW_RETIRE (-110)` line before the guard stop. The sessions stayed usable until the runtime stopped.

`credentials-owner-live.tar` and `credentials-owner-after.tar` (BoltDB ledgers for the controller, both providers and the transport) and `f3-evidence.tar` hold the host state. The full guest `dmesg` is in `p11-guest-dmesg.txt`.

Ruled out:
- **R22-1 again:** no `VIEW_RETIRE` block line.
- **zq3-r23's mechanism (a round waiting out a concurrent round):** only one round was logged, it reported `own retire 0`, and no other session freed anything.
- **A host fence or refusal:** none recorded, and both stderr files were empty.
- **ETIMEDOUT or a deadline:** `etimedout` false, `deadline_hit` false. The case took 55 s of its 240 s.
- **Recall interference:** only the two bootstrap recalls ran (06:50:00Z and 06:50:07Z), before T1.
- **s64:** the fd-based `fchown` and `fchmod` of `group-tail` passed in the parent. The failing call is the child's path `open`.

**Effect.** T2-T11 did not run. The T3 s64 peer-rename guard, T7 (pod) and hh4/dh4 in T9-T11 remain without a real run.

**For the fix (task to file):** the control-lane budget, rather than the retry race, is what this run measured. To do:
- find what fills m0's 512 MiB control lane during T1 (view leases, tickets or other control charges per credential set) and why one round frees only one request;
- either retire more per round under charge pressure (all retirable runs, or every credential set) or keep retrying while rounds make progress;
- check whether the held view tickets (22 of 24 on the host) are part of that charge.

**Proven cause (task gn4, after the run):** the inference above was right about the lane, but not about the remedy. The control bytes were held by released but unretired VIEW_RELEASE tickets. Retiring more requests per round could not have helped, because requests use the ordinary lane. The failing charge was child 3's release ticket, charged after a grant. Its failure stranded a host lease, which then blocked every later VIEW_RETIRE of m0. Details and the fix are under [Follow-up](#follow-up).

## R24-2 (environment, minor): slow SSH round trips

The three Phase 9 `ssh-noop` samples took 2.02, 0.45 and 2.29 s (median 2.016 s; earlier runs had about 0.2 s). The OBSERVE wall times were 1.39-2.11 s, so the net OBSERVE times came out negative or near 0 (net max 0.096 s), which makes the 984 sample uninformative this time. Rocky's first SSH try in Phase 12 had one banner timeout (try 2 answered at 06:55:46Z, 19 s after the start). Nothing failed because of it. Record only.

## 9n4 / km4 log-line observations

Every `retired under charge pressure` line, with its lane usage:

| printk s | monotonic s | Session | Own retire | Freed | Others | Lane usage | Outcome |
|---|---|---|---|---|---|---|---|
| 181.123684 | 181.0815 | m0 `f8b0bf14-…` | 0 | 1 request | 0 of 1 freed 0 | control lane 526,456,646 / 536,870,912 bytes, 368 / 512 slots | `retrying the charge once` (the retry then failed ENOSPC) |

- No `a concurrent round freed requests, retrying the charge once` line: 9n4's retry path did not fire.
- No `nothing freed, the charge fails` line. The R23-1 pattern recurred without it (R24-1).
- No `blocked by uncertain control result on VIEW_RETIRE (-110)` line (R22-1 did not recur). No `blocked by … (-28)` line, no `view control worker stopped` line before the guard stop, and no `WARNING`/`BUG`.
- None of the other expected re4/1h4/4h4/tj4/s64/hh4/ie4 lines appeared during T1.

## Host events and facts

- **f3:** boot `1794574308` unchanged. iom1-3 were Stopped at Phase 1, at Phase 5 step 1 and at Phase 12, and no k3s VM was listed.
- **N1:** stamp `2026-11-27`, dnf #69 unchanged. The nightly gate printed GO at 06:43Z and 06:48Z. The run ended at 06:56Z, far ahead of the 21:25Z bound.
- **Rocky:**
  - before the run: PID 10896 (Fri Nov 27 00:25:44Z), boot `f39e32c7-…`;
  - `stop-rocky` at 06:48:41Z (`Stopped` by 06:48:47Z);
  - started at 06:55:27Z: PID 47896 (lstart Fri Nov 27 06:55:27 2026 UTC), boot `971d3010-d7c6-4b0e-8855-5e0a9b5fb6d9`;
  - pinned SSH answered at 06:55:46Z on the second try;
  - **down 6 min 46 s** (7 min 5 s to SSH).
- **Run values:**
  - window plan `ac356cb4…`;
  - observer EA `9b620f06…`, removed (`restored_pre_run: true`, `authorized_keys` back to `e08542dc…`);
  - volume `0e900a6b-…`;
  - B `42c0650a…`, U `15a5247e-…`;
  - guest boot `ef38a04f-…`;
  - installed admission manifest `7116f17b…`.
- **Guard:** one SIGTERM at 06:53:27Z to the root guard PID 44891, gone by 06:53:33Z (first check). `runtime_running_at_guard_exit` False, `runtime_exit` 0, exporter pid 44902, no `stop-uncertain.json`.
- **Phase 11:**
  - the guest powered off at 06:54:45Z, and the child was gone at 06:54:53Z (first try, pattern `[h]fs-zq3-r24`);
  - host cleanup destroyed the tap and the vmm, and the UEFI vars stayed unchanged (`e32f7926…`);
  - the native fusefs mount was unmounted normally;
  - the f3 observer key was deleted;
  - the window was restored (`mount_restored: true`).
- **Restore proof (final state):**
  - Rocky runs the stock `5.14.0-741.27.1.el9_9` = grub default = `saved_entry`, with `next_entry` empty (`check --expect stock` ok);
  - Rocky is Enforcing, with no `/.autorelabel` and systemd `running`, and Rocky swap is unused;
  - no `hfs-xz3` file in `/boot`, no hfs-xz3 BLS entry, and no `/lib/modules/6.1.190-hfs-xz3` or `/etc/homelabfs` (U2);
  - on f3, Rocky is the only running VM, `/dev/vmm` holds only `rocky` and swap is 0;
  - no fdescfs/fusefs mount; `fdescfs.ko` (41) and `fusefs.ko` (42) stay loaded by the reviewed plan, so the next run needs Phase 1a again;
  - the f3 observer directory holds only `rocky-known_hosts`, and Rocky's `authorized_keys` is back to `e08542dc…`.
- **Operator notes:**
  - every line ran through a bash wrapper that sourced `session.sh` and `vars.env`, with stdin from `/dev/null`; zsh never ran a session line;
  - every wait (READY, `summary.json`, guard-gone, child-gone, `rockywait`) was a bounded foreground loop, and no Monitor, timer or notification wakeup was armed (R23-2 check);
  - some hand-written times inside early run-log lines ran 1-2 minutes ahead; the line-prefix stamps are authoritative (noted in the log);
  - Phase 11's ETIMEDOUT scan line found no release or T6-T11 file to scan (the helper printed its usage), as in zq3-r21 to zq3-r23;
  - the Phase 9 `LSM: initializing` grep matched nothing again (`SELinux:  Initializing.` was present).

## Follow-up

- **R24-1 (R23-1 again):** the 1024-group access child's `open` of `group-tail` fails `ENOSPC` with m0's control lane at 98.1% of its bytes. One km4 round freed 1 request, and the single retry did not fit. 9n4 fixed a race this run did not hit. Task gn4 proved the cause from the controller ledger in `credentials-owner-live.tar` and the code ([linux9p README, gn4](../../../../tools/integration/linux9p/README.md#release-tickets-charged-before-the-grant-view-tickets-retired-under-charge-pressure-gn4)):
  - **The control lane was full of released but unretired VIEW_RELEASE tickets.** All 22 host tickets were m0's. 13 were RELEASED release tickets: child 1's 1-5, child 2's 1-3, the T1 process's 16-19 and child 3's 1. Each holds about 25.5 MiB of guest control bytes (with its view package) until a VIEW_RETIRE, about 348 MB together. The other 9 were GRANTED acquire tickets (ordinary lane).
  - **The round freed 1 request, which returned none of those bytes.** In both controlled tuples every request is charged to the ordinary lane, and `hfs_retire_shared` offered no view tickets.
  - **The failing charge was child 3's second lease's release ticket.** Its two VIEW_ACQUIREs were granted the same lease (`c5110c37…`, revision 1), which is still Active with no release ticket. `hfs_lease_own` charged that ticket after the GRANTED. The charge failed, the guest dropped the lease locally, and the host lease was stranded.
  - **Why 22 tickets and 8 leases stayed on the host.** The runtime's physical check (`factsLedger.checkRetire`) refused every VIEW_RETIRE of m0 while any m0 lease was active, so the stranded lease blocked all later retirements. The same domain-wide rule had kept the earlier released tickets charged whenever another T1 process held an observation lease. It would also have refused a retirement from the failing charge itself, because child 3 still held its first lease.
  - **Fix (gn4):** the guest charges the release ticket before the VIEW_ACQUIRE is sent. Its charge-pressure round also runs `hfs_view_retire`, and a failed retry is now logged. On the host, `checkRetire` refuses only for an active lease of the retiring family's own credentials (as the controller's `retirementBlocked` already did); an open reservation still refuses domain-wide.
  - **An r25 candidate needs the guest kernel and the daemon rebuilt.** The exporter is unchanged.
- **R24-2:** slow SSH round trips (about 2 s) during Phase 9; record only.
- **Validated by this run:**
  - 9n4's lane-usage field (it named the control lane and its byte and slot usage);
  - guest.py's child syscall naming (`"syscall": "open"`, `(child open errno 28)`);
  - km4 (no `VIEW_RETIRE` block; `create(group-tail)` and its fd calls pass);
  - block 2a (Phase 9 63 s after READY) and the R23-2 wait rule;
  - the km4 daemon's process-observer qualification;
  - sj4, tj4/wj4 (advances 9) and vj4 (self_admissions 10);
  - the `8cadae40` exporter launch;
  - `hfs_fdattr` mount options;
  - R13-1 (both unmounts `rc 0` after the guard stop).
- **Still without a real run:** T2-T11 (the s64 peer-rename guard, T7, hh4/dh4 in T9-T11). T5 stays deferred.
- **The iormesh session can be told that f3 is free** (run over at 06:56:23Z).
