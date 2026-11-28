# zq3-r26: hp4 gets T1 past the retention `lstat`s, then the inventory's READDIR on m0 comes back "malformed" and blocks the session (2026-11-28)

Run sheet: [filled plan](../20261128-zq3-r26-filled-plan/run-sheet.md)
(fill merge `7eeaeb6`; tools snapshot `31d08d9`).

Candidate: the r26 rebuild `80cd01bb…` ([xz3-20261127-r26](../../../../tools/integration/artifacts/evidence/xz3-20261127-r26/README.md)):
- the go1.27.4 hp4 daemon `024528a4…` (per-ticket `retirementBlocked` / `checkRetire`), first run;
- the guest kernel, build ID `899cc101…` (hp4's comment-only patch changes; same behaviour as r25's `8c70915f…`);
- the exporter `8cadae40…` (zq3-r21's bytes, sixth run).

`hfs_fdattr` was enabled on both mounts. Times are UTC. Private evidence:
`/home/paul/tmp/claude/hfs-xz3-20261127-r26/zq3-r26-fill/evidence` (run log `RUN-LOG.md`).

## Before the run

- Paul gave the final GO at about 10:21Z ("GO now + copy key"): run today with no refresh. That covered A1-A8, including the Phase 1a unload, T7, the Rocky stop, the controlled launch and both signatures by Claude through the watcher. The fill's facts are from 08:47Z, after the 2026-11-27 22:55Z nightly dnf run (no new transaction, #69). T5 stays deferred.
- The operator key was copied into the fill's `operator/` before the run. Only `sign_watch.py` used it, through the signer. The operator checked only its mode, owner, link count and size (`600 paul 1 119`), and never read it.
- Rocky ran zq3-r25's Phase 12 incarnation (PID 97712, boot `5f26abab-…`).
- iom1-3 were Stopped through the whole run, and the sheet never touched them.
- f3 swap was 0, so there was no Phase 0b.

## Outcome

Phases 0 to 9 passed. Phase 4 step 1a qualified the new hp4 daemon, and Phase 9 started about 72 s after READY. **T1 (`credentials`) got further than in zq3-r25.** Every probe syscall returned what the case expects, the 1024-group access child's read of `group-tail` included. Then guest.py's retention inventory ran, and every payload `lstat` passed (zq3-r25 failed on the first one, R25-1). **No `retired under charge pressure` line, no `charge still failed after the retry` line and no ENOSPC appeared anywhere in the run.** The inventory's last call, `os.listdir` of the leaf, then failed `EACCES`. m0's guest session logged **`blocked by malformed READDIR page on READDIR (-71), exporter ERROR -`** and stopped its view worker (**R26-1**, new: a guest-side protocol rejection of a READDIR reply, not a budget failure). The host finished that READDIR claim (TERMINAL). It recorded no fence, no refusal and no stderr line. T2 to T11 did not run. Phase 11 took the guard-SIGTERM branch, and restoration was clean. **Rocky was down for 9 min 19 s** (9 min 39 s to SSH; zq3-r25: 8 min 48 s). As in zq3-r25, T1 itself accounts for about 3 minutes of that: it ran 180 s.

| Phase | UTC | Result |
|---|---|---|
| 0 | 10:23:09-10:23:54 | passed |
| 1 | 10:23:54-10:24:30 | passed (nightly gate GO at 10:24Z) |
| 1a | 10:24:30-10:24:49 | passed (fusefs id 43, fdescfs id 42, `rc 0` each) |
| 2 | 10:24:50-10:25:05 | passed |
| 3 | 10:25:05-10:26:27 | passed (non-default kernel entry, PM `34a71569…`, 82 entries) |
| 4 | 10:26:27-10:28:00 | passed (step 1a qualified `024528a4…`, pid 92299) |
| 5 | 10:28:00-10:28:53 | passed (gate GO at 10:28Z, UMA drain, one-shot select, Rocky stopped) |
| 6 | 10:28:55-10:29:11 | passed (margin 0.25 GB) |
| 7 | 10:29:11-10:29:22 | passed |
| 8 | 10:29:35-10:31:00 | passed (launch 10:29:36Z, READY 10:30:09Z, `usable`, L = 2) |
| 9 | 10:31:21-10:31:38 | passed |
| T1 `credentials` | 10:31:54-10:34:59 | **fail** (R26-1: EACCES from the retention inventory's `listdir`; m0 blocked by a malformed READDIR page) |
| T1 case-failure block | 10:35:05-10:36:08 | completed (failure OBSERVE seq 5, `fenced: false`) |
| T2 `sync`, T3 `metadata` (s64 peer-rename guard), T4 `namespace` | - | unrun (T1 failed) |
| T5 `mapped-pressure` | - | unrun (deferred, re-plan pending) |
| T6, T7 (pod), T9, T10, T11, T8 | - | unrun (T1 failed) |
| 11 | 10:36:08-10:37:54 | completed (guard-SIGTERM branch) |
| 12 | 10:38:07-10:38:55 | completed; Rocky on stock 741.27.1, custom kernel removed |

The phase times come from the tool output, the run log and the evidence file stamps. Times inside a phase are accurate to about a minute; the start, stop, READY, guard and Rocky times are exact. The run started right after the GO handoff. The gate gave GO at 10:24Z and again at 10:28Z, so no N1 wait was needed. Every wait was a bounded loop in the foreground.

**What held:**
- **Phase 0:** all pins passed, and so did `argv.py verify`, `check-argv`, `bootstrap.py check` and the synthetic batch check (four cases, `mapped-pressure` deferred, `fd_attr` true). The workstation ssh was still `d4c70b09…`. The diff `31d08d9..7eeaeb6` over the pinned tool paths was empty.
- **Phase 1:** every fact matched the 08:47Z facts:
  - f3 boot `1794574308`, Rocky PID 97712 (lstart Fri Nov 27 15:50:35 2026), boot `5f26abab-…`, UEFI vars `44770067…`, swap 0;
  - iom1-3 Stopped, no k3s VM, `/dev/vmm` = `rocky`;
  - Rocky runs `741.27.1` = grub default = `saved_entry`, with `next_entry` empty;
  - stamp `2026-11-28`, dnf #69 (`dnf_unchanged: true`), `pod-runtime-unchanged: True`.
- **Phase 3:** the kernel entry `b26c0ac5…-6.1.190-hfs-xz3` was added as a non-default entry, and `check --expect stock` passed. The pod rootfs manifest is the predicted `34a71569…` (82 entries).
- **Phase 4:**
  - **step 1a qualified the new hp4 daemon `024528a4…` on its first f3 run** (`p4-observer.py` `pass: true`: uid/gid 0, groups `[0,0,5]`, pid 92299, stderr empty);
  - the window plan `2edfca02…` was set up and qualified, and fdescfs was supported;
  - fusefs was loaded (fdescfs id 43, fusefs id 44);
  - the observer key was enrolled (`EA` `96c26008…`).
- **Phase 5:** the estimate margin was +0.18 GB, so the UMA drain ran and freed about 0.14 GB. `select` set only the one-shot `next_entry`, and the stock default stayed. `stop-rocky` ran from 10:28:49Z to 10:28:53Z.
- **Phase 6:** passed with a 0.25 GB margin (15,010,293,552 bytes).
- **Phase 8:**
  - launch at 10:29:36Z, READY at 10:30:09Z (33 s);
  - both signatures by the watcher (`"watcher": "signed"` ×2);
  - mounts 434 and 454;
  - `usable` passed with L=2;
  - the exporter launched cleanly, and `exporter.stderr` and `runtime.stderr` stayed empty.
- **Block 2a (R22-2 stays fixed):** the driver started in the background at 10:29:35Z. The bounded `grep READY` poll returned, and READY was logged as seen at 10:30:17Z. The `summary.json` wait returned by 10:30:57Z, and Phase 9 started at 10:31:21Z (`late_after_ready: false`). The background task's completion notice was never waited for.
- **`hfs_fdattr`, sixth VM run:** both mountinfo rows read `rw,cache=mmap,hfs_fdattr,msize=65536,trans=virtio,version=9P2000.L.hfs1.4`.
- **Phase 9:** passed, with 3494 s of guard budget left (guard end 11:29:36Z).
  - OBSERVE seq 3, `Open 0` on both providers, view controller Leases 0 and Tickets 0;
  - Enforcing, enforce 1; `SELinux:  Initializing.` present (the `LSM: initializing` grep matched nothing, as before); `no-hfs-block`.
- **T1 passed every probe step** (hp4, gn4, km4, sj4, s64). Admission took 6.63 s, with the largest gap 1.69 s at `open-root[1] → stat-root[1]`. In order, all of these returned 0 or the designed refusal:
  - `mkdir-exclusive`, `open-leaf`, `fchmod-leaf`, then `create-exclusive group`, `fchown`, `fchmod` and the `pwrite`;
  - access child 1 read `group-secret`, and child 2 got `EACCES` (`"syscall": "open"`, designed);
  - `create-exclusive(group-tail)`, then `fchown`, `fchmod` and the 15-byte `pwrite`;
  - **the 1024-group access child (pid 2850, uid 41001, 1024 groups) read `full-group-tail`, status 0** (the R23-1/R24-1 point, passed again);
  - the next child got `EACCES` opening `group-tail` (designed);
  - the `setgid` directory: `mkdir`, `open-directory`, `fchown 0:42002` and `fchmod`; its child reported `[41001, 42002, 416]`; then `stat-cross-domain` of `setgid/created`;
  - the `sticky` directory: `mkdir`, `open-directory` and `fchmod`; `create-exclusive owned` and `fchown`; the child's `unlink` of `owned` got `EPERM` (designed);
  - `group`: `open`, `chmod-special-bits`, `fstat`, `chown-clears-special-bits`, `fstat` (s64 fd-based calls, all 0);
  - `open-leaf` for the retention inventory (reply at guest +147.5 s);
  - **every payload `lstat` of the inventory** (zq3-r25 failed `ENOSPC` on the first, `setgid/created`), and the leaf `fstat`.
- **hp4 did what it was built for.** No charge-pressure round ran at all in T1: no `retired under charge pressure` line, so no `view retire` errno of any kind, and no `charge still failed after the retry` line. The view controller held Leases 0 and Tickets 0 at the Phase 9 OBSERVE and at the T1 baseline (seq 3 and 4). It held **Leases 1 and Tickets 4** at the failure OBSERVE (seq 5), about 25 s after the case. That is m0's blocked session: its view worker stopped with a lease still held. zq3-r25 showed 0/0 at that point, and zq3-r24 8/22.
- **xf4 and vj4 still work.** `recalls.py` reported:
  - `advances` 33 and `self_admissions` 4 (zq3-r25: 30 and 14);
  - 4 recalls, all `RELEASE/VIEW_RELEASE` (DOMAIN): the 2 bootstrap recalls (10:29:56Z on m0, 10:30:04Z on m1) and 2 more on m0 during T1 (10:32:52Z, 7.4 s; 10:34:04Z, 8.0 s);
  - no CALL_CONFLICT and no EXPECTED_TARGET recall.
- **No guest WARNING or BUG, no AVC, no host fence.** `t-fail-fence.json` and `p11-fence.json` both report `fenced: false`; `runtime.stderr` and `exporter.stderr` are empty.
- **Unmounts after the guard stop:** m0 `rc 0` in 0 s, m1 `rc 0` in 0 s, with no new block line (R13-1 stays fixed).
- **Phase 12:** Rocky started on stock `741.27.1`, with one vm-bhyve run and no relabel. U2 removed the test kernel.

## R26-1 (new): the retention inventory's `listdir` fails EACCES; m0's session is blocked by a "malformed READDIR page"

**What happened.** The probe started at 10:31:59Z and ended after 180.05 s with `dispatching-uncertain` ("Refused: missing complete guest terminal; remote state uncertain"). This time guest.py did not catch the error as a case failure. The parent process exited with a `PermissionError` traceback (`main`, line 1320 of the shipped script), so no terminal record reached the controller.

The guest.py named events (`ctrl-run/credentials/stdout.log`; guest monotonic seconds):
1. the 1024-group child's exit was recorded at 196.68 s (status 0, `full-group-tail`);
2. all the later probe steps listed above returned 0 or their designed refusal, up to `open-leaf` (reply at 284.466 s);
3. **the failure at 315.168 s:** `{"error": "PermissionError", "errno": 13, "detail": "[Errno 13] Permission denied"}`. It comes before any `payload-retained` event, so it is inside `Retention.retain()` → `inventory()`. That function runs `os.stat(name, dir_fd=leaf, follow_symlinks=False)` on each payload entry, then `os.fstat(leaf)`, then `os.listdir(leaf)`. The detail carries **no file name**. A failing `os.stat(name, dir_fd=…)` names its path, as zq3-r25's `'setgid/created'` did, while `os.listdir(fd)` has none. So the failing call is the `listdir` (`getdents64`, a 9p READDIR), after every payload `lstat` passed. The error is the parent's own, so no `"syscall"` field was recorded.

Every `9p-hfs` line in the full guest `dmesg` (`p11-guest-dmesg.txt`):

| printk s | Line |
|---|---|
| 315.132750 | **m0 session `f0f6cc78-…` (`hfs0`): `blocked by malformed READDIR page on READDIR (-71), exporter ERROR -; later operations on this mount fail with EACCES or EHOSTDOWN`** |
| 315.264735 | m0: `view control worker stopped, error -112, exporter ERROR -, at VIEW_WAIT (event -, ack -, host status -, view phase LIVE, recall -)` |
| 406.355903 | m1 session `255e34fc-…`: `blocked by uncertain control result on VIEW_WAIT (-112), exporter ERROR 5` (after the guard's SIGTERM at 10:36:29Z; expected) |
| 406.355918 | m1: `view control worker stopped, error -112, exporter ERROR 5, at VIEW_WAIT …` (same; expected) |

There is no other `9p-hfs` line in the boot: no charge-pressure round, no 4h4/re4/1h4 line and no km4 `VIEW_RETIRE` block line.

**Timing.** The block line (printk 315.133 s) and guest.py's failure event (monotonic 315.168 s) are 35 ms apart. The failure came 30.7 s after `open-leaf` returned. guest.py logs no events inside the inventory, so this gap cannot be split between the `lstat`s and the READDIR.

**Host side (read-only):**
- The failure OBSERVE (seq 5, the yf4 direct form) was OBSERVED, `complete`, `SERVING`, with `transport.failure` null.
- m0's provider ledger (`f0f6cc78`): Allocated 166, Retired 165, Open 0. **Its last claim, #166, is a READDIR, `TERMINAL`** (stage `PREPARING`); #165 is a GETATTR. m1's: Allocated 19, Retired 17, Open 0.
- **The view controller held Leases 1 and Tickets 4**, with Reservations 0, no recall and `Matches` true.
- `runtime.stderr` and `exporter.stderr` (`runtime/owner/exporter.stderr`) were empty. `t-fail-fence.json` and `p11-fence.json` report `fenced: false`.

**What the evidence shows:**
- **R25-1 is fixed.** The `lstat` that failed in zq3-r25, and every payload `lstat` after it, now passes, and no charge-pressure round ran. hp4's per-ticket rule removed the control-lane pressure that had stopped T1 in zq3-r24 and zq3-r25.
- **The new failure is a guest-side protocol rejection, not a budget failure.** The guest says `exporter ERROR -`, so the exporter sent no ERROR: the guest kernel's READDIR reply check refused the page itself (`-71`, `-EPROTO`). It then blocked the session, and later calls return `EACCES` (the m64/oc4 block rule). This is the first READDIR through an hfs mount in any zq3 run: no earlier run got past the inventory `lstat`s, and the `usable` gate only runs `lstat`.
- **The host completed the READDIR claim.** It recorded no refusal and no fence, so whatever the host and exporter returned, the host considered the request done. Whether the page is truly malformed (exporter, or the daemon's READDIR reply), or the guest's check is wrong (for example for a page under `hfs_fdattr`, or with the payload's names and types), cannot be told from this evidence. The guest does not log the page.
- **Leases 1 and Tickets 4** at the failure OBSERVE come from the blocked m0 session (its view worker stopped with a lease live), not from hp4. They are recorded, not a finding against hp4.

**hp4 rules (the sheet's finding rules):**
- `view retire 0`: none (no round ran);
- `view retire -11` followed by `charge still failed after the retry` on the control lane in T1 (R25-1 again): **none**;
- any other `view retire` errno: **none**;
- `charge still failed after the retry`: **none**;
- 9n4 `nothing freed, the charge fails`: none. Any ENOSPC: none.

Ruled out:
- **R25-1/R24-1/R23-1/R21-1 again:** no ENOSPC, no round line, no charge failure.
- **R22-1 again:** no `VIEW_RETIRE` block line.
- **R20-1 (sj4):** the EACCES does not come from a permission check below the 0700 root. It follows m0's block line, and the parent runs as root.
- **A host fence or refusal:** none recorded, and both stderr files were empty.
- **ETIMEDOUT or a deadline:** `etimedout` false, `deadline_hit` false. The case took 180 s of its 240 s request deadline.
- **s64's fd-based calls:** `fstat`, `fchown` and `fchmod` all returned 0.
- **The shell tool's move to the background:** the tool moved the `probe` line to the background at 180 s, the same second the case ended (R26-2). The tool does not signal or kill a moved command: the line went on, printed `rc=0` and its closing timestamp, and `state.json` was written. The guest's own clock puts the failure and the block line at 315.13-315.17 s. Workstation process handling cannot produce a guest 9p `-EPROTO`.

`credentials-owner-live.tar` and `credentials-owner-after.tar` (BoltDB ledgers for the controller, both providers and the transport) and `f3-evidence.tar` hold the host state. The full guest `dmesg` is in `p11-guest-dmesg.txt`.

**Effect.** T2-T11 did not run. The T3 s64 peer-rename guard, T4's link(2), T7 (pod) and hh4/dh4 in T9-T11 still have no real run.

**For the fix (task to file):**
- find the guest check that logs `malformed READDIR page` and what it requires of the page (entry framing, offsets, name length, qid type, the hfs1.4 extensions, `hfs_fdattr`);
- decode the READDIR reply for claim #166 from the m0 provider ledger and transport records in `credentials-owner-*.tar`, if they hold the page or its size;
- replay a READDIR of a directory with T1's payload (`group`, `group-tail`, `full-group-tail`, the `setgid` and `sticky` directories, the files in them) through the exporter `8cadae40…` and the r26 daemon in a guest test (controlled_received), with and without `hfs_fdattr`;
- consider having guest.py log the inventory's steps, so a failure there names its call.

## R26-2 (operator, minor): the T1 probe line moved to the background again

The shell tool moved the T1 `probe` line to the background after 180 s, even though the line had a 330 s timeout (as in zq3-r25). Its result was read from the line's own output file through a bounded foreground `grep` loop. The completion notice arrived after that read, and no wait depended on it. The case-failure block's second owner snapshot was complete (`rc 0`, `tar -tf` ok), so R25-2's retry was not needed. Nothing on the hosts was affected. Record only.

## gn4 / 9n4 / km4 / hp4 log-line observations

- No `retired under charge pressure` line, so no `view retire <errno>` at all.
- No `charge still failed after the retry` line.
- No `a concurrent round freed requests, retrying the charge once` line, and no `nothing freed, the charge fails` line.
- No 4h4 `view moved` line (zq3-r25 had one), and none of the other expected re4/1h4/tj4/s64/hh4/dh4/ie4 lines.
- No `blocked by uncertain control result on VIEW_RETIRE (-110)` line (R22-1 did not recur). No `blocked by … (-28)` line and no `WARNING`/`BUG`.
- The only block lines are R26-1's m0 pair and the expected m1 pair after the guard stop.

## Host events and facts

- **f3:** boot `1794574308` unchanged. iom1-3 were Stopped at Phase 1, at Phase 5 step 1 and at Phase 12, and no k3s VM was listed.
- **N1:** stamp `2026-11-28`, dnf #69 unchanged. The nightly gate printed GO at 10:24Z and 10:28Z. The run ended at about 10:39Z, far ahead of the 21:25Z bound.
- **Rocky:**
  - before the run: PID 97712 (Fri Nov 27 15:50:35Z), boot `5f26abab-…`;
  - `stop-rocky` at 10:28:49Z (`Stopped` by 10:28:53Z);
  - started at 10:38:08Z: **PID 98423** (lstart Sat Nov 28 10:38:08 2026 UTC), **boot `38450180-2bc4-432c-b17f-bc507d1f543e`**;
  - pinned SSH answered at 10:38:28Z on the second try (one banner timeout);
  - **down 9 min 19 s** (9 min 39 s to SSH).
- **Run values:**
  - window plan `2edfca02…`;
  - observer EA `96c26008…`, removed (`restored_pre_run: true`, `authorized_keys` back to `e08542dc…`);
  - volume `34e4ab54-…`;
  - B `08c9908b…`, U `8cd2343c-…`;
  - guest boot `c4f5b5db-…`;
  - installed admission manifest `9fe5a247…`.
- **Guard:** one SIGTERM at 10:36:29Z to the root guard PID 94350, gone by 10:36:31Z (first check). `runtime_running_at_guard_exit` False, exporter pid 94361, no `stop-uncertain.json`.
- **984 timing:** ssh-noop median 0.19 s; OBSERVE net median 0.495 s, max 0.614 s over 3 samples (`net_max_below_984_bound: true`).
- **Phase 11:**
  - the guest powered off at 10:37:22Z, and the child was gone at 10:37:34Z (second try, pattern `[h]fs-zq3-r26`);
  - host cleanup destroyed the tap and the vmm, and the UEFI vars stayed unchanged (`44770067…`);
  - the native fusefs mount was unmounted normally;
  - the f3 observer key was deleted;
  - the window was restored (`mount_restored: true`).
- **Restore proof (final state):**
  - Rocky runs the stock `5.14.0-741.27.1.el9_9` = grub default = `saved_entry`, with `next_entry` empty (`check --expect stock` ok);
  - Rocky is Enforcing, with no `/.autorelabel`, one journal boot and Rocky swap unused;
  - no `hfs-xz3` file in `/boot`, and no `/lib/modules/6.1.190-hfs-xz3` or `/etc/homelabfs` (U2);
  - on f3, Rocky is the only running VM, `/dev/vmm` holds only `rocky` and swap is 0;
  - no fdescfs/fusefs mount; `fdescfs.ko` (43) and `fusefs.ko` (44) stay loaded by the reviewed plan, so the next run needs Phase 1a again;
  - the f3 observer directory holds only `rocky-known_hosts`, and Rocky's `authorized_keys` is back to `e08542dc…`.
- **Operator notes:**
  - every line ran through a bash wrapper that sourced `session.sh` and `vars.env`, with stdin from `/dev/null`; zsh never ran a session line;
  - every wait (READY, `summary.json`, the T1 output file, guard-gone, child-gone, `rockywait`) was a bounded foreground loop, and no Monitor, timer or notification wakeup was armed (R23-2 check);
  - some hand-written times inside early run-log lines (Phase 5) ran up to about a minute ahead; the tool-output stamps are authoritative;
  - Phase 11's ETIMEDOUT scan line found no release or T6-T11 file to scan (the helper printed its usage), as in zq3-r21 to zq3-r25;
  - the Phase 9 `LSM: initializing` grep matched nothing again (`SELinux:  Initializing.` was present).

## Follow-up

- **R26-1 (new):** with hp4, T1 passes every probe syscall and every retention `lstat`. Then the inventory's `listdir` of the leaf fails `EACCES`, because the guest refused m0's READDIR reply as a `malformed READDIR page` (`-71`, no exporter ERROR) and blocked the session. The host finished the claim and recorded nothing wrong. A follow-up task should find which side is wrong: the exporter's or daemon's READDIR page, or the guest's check. See the fix list under R26-1. This is the first hfs READDIR in a zq3 run; T4 and T11 will also list directories.
- **R26-2:** a tool-moved probe line; record only.
- **Validated by this run:**
  - the hp4 daemon's process-observer qualification (first f3 run) and its launch;
  - **hp4's per-ticket VIEW_RETIRE rule:** no charge-pressure round, no control-lane failure, and the R25-1 `lstat` passed;
  - the 1024-group access child's read of `group-tail`, and every T1 probe step after it (sj4, s64 fd calls, setgid/sticky semantics);
  - km4 (no `VIEW_RETIRE` block), tj4/wj4 (advances 33), vj4 (self_admissions 4);
  - block 2a (Phase 9 about 72 s after READY) and the R23-2 wait rule;
  - the `8cadae40` exporter launch, `hfs_fdattr` mount options, and R13-1 (both unmounts `rc 0` after the guard stop).
- **Still without a real run:** T2-T11 (the s64 peer-rename guard, T4 link(2), T7, hh4/dh4 in T9-T11). T5 stays deferred.
- **The iormesh session can be told that f3 is free** (run over at about 10:39Z).
