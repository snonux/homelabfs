# zq3-r22: READY, `usable` and Phase 9 pass again with `hfs_fdattr`, but T1's `create` of `group-tail` fails ENOSPC after 33 s, m0 blocks on a `VIEW_RETIRE` timeout and m1 on `PROCESS_EXIT (-28)` (2026-11-26)

Run sheet: [filled plan](../20261126-zq3-r22-filled-plan/run-sheet.md)
(merge `de1c900`; tools snapshot `1ed87bf`).

Candidate: the r22 rebuild `2c85b230…` ([xz3-20261126-r22](../../../../tools/integration/artifacts/evidence/xz3-20261126-r22/README.md)):
- the guest kernel, build ID `c34dc6fb…` (6m4 on top of r21's series), first run;
- the go1.27.4 daemon `c7b89648…` (zq3-r21's bytes);
- the exporter `8cadae40…` (zq3-r21's bytes, second run).

`hfs_fdattr` was enabled on both mounts. Times are UTC. Private evidence:
`/home/paul/tmp/claude/hfs-xz3-20261126-r22/zq3-r22-fill/evidence` (run log `RUN-LOG.md`).

## Before the run

- Paul gave the final GO for the sheet today (A1-A8, including the Phase 1a unload, T7, the Rocky stop, the controlled launch and both signatures by Claude through the watcher). T5 stays deferred.
- No dnf run and no Rocky reboot since the fill: Rocky ran zq3-r21's Phase 12 incarnation (PID 93782, boot `c7fd367a-…`).
- The iormesh VMs iom1-3 were stopped and held by the iormesh session. They stayed stopped through the whole run and the sheet never touched them.
- f3 swap was 0, so there was no Phase 0b.

## Outcome

Phases 0 to 9 passed. T1 (`credentials`) failed with a new finding, R22-1: ENOSPC came back in a new form, so R21-1 is not fixed by 6m4. T2 to T11 did not run. Phase 11 took the guard-SIGTERM branch, and restoration was clean. An operator delay (R22-2) started Phase 9 about 20 minutes after READY. The guard budget still allowed Phase 9 and T1, but Rocky's downtime grew to about 26 minutes.

**What held:**
- **Phase 0 recheck:** all pins, `argv.py verify`, `check-argv` and `bootstrap.py check` passed. The workstation ssh was still `d4c70b09…`, and `git diff 1ed87bf de1c900` over the tools was empty.
- **Phase 1:** every fact matched the 11:48Z fill:
  - f3 boot `1794574308`, Rocky PID 93782, UEFI vars `0be7dec3…`, swap 0;
  - iom1-3 Stopped and `/dev/vmm` = `rocky`;
  - dnf still #68, the pod runtime unchanged;
  - the early nightly gate printed GO at 14:21Z.
- **Phase 1a:** a normal `kldunload` of fusefs (id 39) and fdescfs (id 38), `rc 0` each.
- **Phase 3:** the kernel entry `…-6.1.190-hfs-xz3` was added as a non-default entry, and `check --expect stock` passed. The pod rootfs manifest is the predicted `34a71569…`, and the build made 82 entries.
- **Phase 4:**
  - step 1a qualified the daemon `c7b89648…` again (uid/gid 0, groups `[0,0,5]`, pid 39889);
  - the window plan `ce4cf578…` was set up and qualified, and fdescfs was supported;
  - fusefs was loaded (fdescfs id 39, fusefs id 40);
  - the observer key was enrolled (`EA` `e0831c1f…`).
- **Phase 5:** the nightly gate printed GO at 14:25Z, and the rechecks passed (iom1-3 Stopped, `/dev/vmm` = `rocky`, swap 0). The estimate margin was +0.15 GB, so the UMA drain ran and freed about 0.20 GB. `select` set only the one-shot `next_entry`, and the stock default stayed.
- **Phase 6:** passed with a 0.28 GB margin (15,043,770,824 bytes).
- **Phase 8:**
  - launch at 14:26:46Z, READY at 14:27:19Z (32.8 s);
  - both signatures by the watcher;
  - mounts 391 and 408;
  - `usable` passed with L=2;
  - the exporter launched cleanly, and `exporter.stderr` stayed empty.
- **`hfs_fdattr` (bl4/s64), second VM run:** both mountinfo rows read `rw,cache=mmap,hfs_fdattr,msize=65536,trans=virtio,version=9P2000.L.hfs1.4`, with `hfs_fdattr` once, right after `cache=mmap`. The batch fill accepted them (`fd_attr: true`). T1's `fchown` and `fchmod` of `group` through the open description returned 0 again.
- **Phase 9:** passed with 2362 s of guard budget left.
  - OBSERVE seq 3, with `Open 0` on both providers;
  - Enforcing; no hfs block line;
  - 984 timing: SSH no-op median 0.222 s, OBSERVE net median 0.447 s, max 0.596 s (3 samples).
- **R20-1 stays fixed (sj4).** Access child 1 (uid 41001, groups `[42001,42002,42003]`) read `group` below the 0700 root (`group-secret`, status 0). Child 2 got EACCES as designed.
- **xf4 advance and vj4 still work.** `recalls.py` reported:
  - `advances` 7 and `self_admissions` 6;
  - only the 2 bootstrap `RELEASE/VIEW_RELEASE` recalls (DOMAIN, about 4.8 s each);
  - no CALL_CONFLICT and no EXPECTED_TARGET recall.
- **T1 admission:** 10.0 s. The largest gap was 3.0 s at `open-root[0] → stat-root[0]`. No deadline was hit and there was no ETIMEDOUT in the probe's records.
- **No guest WARNING or BUG, no AVC, no host fence.** `runtime.stderr` was empty, and both fence records report `fenced: false`.
- **Unmounts after the guard stop:** both returned `rc 0` in 0 s, with no new block line (R13-1 stays fixed).
- **Phase 12:** Rocky started on stock `741.27.1`. It ran once in vm-bhyve, with no relabel. U2 removed the test kernel.

| Phase | Result |
|---|---|
| 0-1, 1a | passed |
| 2-7 | passed (daemon qualified, UMA drain, Phase 6 margin 0.28 GB) |
| 8-9 | passed (READY, `usable` with L=2, `hfs_fdattr` on both mounts); Phase 9 started late (R22-2) |
| T1 `credentials` | **fail** (R22-1, ENOSPC). R20-1 passed; advances 7, self_admissions 6 |
| T2 `sync`, T3 `metadata` (s64 peer-rename guard), T4 `namespace` | unrun (T1 failed) |
| T5 `mapped-pressure` | unrun (deferred, re-plan pending) |
| T6, T7 (pod), T9, T10, T11, T8 | unrun (T1 failed) |
| 11-12 | completed; Rocky on stock 741.27.1, custom kernel removed |

The T3 s64 peer-rename guard did not run again. So `hfs_fdattr` was checked only by the mount rows and by T1's fd-based calls on `group`, which returned 0.

## R22-1: T1's `create` of `group-tail` fails ENOSPC after 33 s, and both sessions block

**What happened.** T1 started at 14:47:51Z and ended after 73.9 s with `dispatching-uncertain` ("missing complete guest terminal").

The guest.py named events (`ctrl-run/credentials/stdout.log`), each with errno 0 unless noted:
1. `mkdir-exclusive` (5.0 s), `open-leaf` (4.2 s), `fchmod-leaf 0777`;
2. `create-exclusive group`, `fchown 41002:42002`, `fchmod 0640`, `pwrite` of 12 bytes;
3. access child 1 read `group-secret` (status 0); access child 2 was denied with EACCES (status 256), the expected outcome;
4. **`create-exclusive(group-tail)` returned errno 28 (`ENOSPC`)**. It was invoked at guest monotonic 1298.593 s and returned at 1331.853 s, **33.26 s later**;
5. the case then failed with `PermissionError` errno 13 at 1331.882 s, the consequence of the blocked session.

So r22 got less far than zq3-r21. There, `create(group-tail)` and its `fchown` returned 0 and the `fchmod` after them failed at once. Here the `create` itself failed, and only after waiting about one 30 s reply bound.

The guest kernel log at the same moment (the full `dmesg` holds no other hfs line, and no 6m4, re4 or 1h4 line):

```text
[1331.882756] 9p-hfs: session 80a032fa-… blocked by uncertain control result on VIEW_RETIRE (-110), exporter ERROR -; later operations on this mount fail with EACCES or EHOSTDOWN
[1331.882792] 9p-hfs: session 80a032fa-… view control worker stopped, error -112, exporter ERROR -, at VIEW_WAIT (event -, ack -, host status -, view phase LIVE, recall -); later operations on this mount fail
[1331.966915] 9p-hfs: session d23791a0-… blocked by process-exit failure on PROCESS_EXIT (-28), exporter ERROR -; later operations on this mount fail with EACCES or EHOSTDOWN
[1331.970686] 9p-hfs: session d23791a0-… view control worker stopped, error -112, exporter ERROR -, at VIEW_WAIT (event -, ack -, host status -, view phase LIVE, recall -); later operations on this mount fail
```

**Host side (read-only):**
- The failure OBSERVE (seq 5, the yf4 direct form) was OBSERVED, SERVING and complete, with no fence.
- m0's provider ledger: Allocated 46, Retired 39, Entries 7, Open 0. Claims 40-46 are TERMINAL and unretired: DESCRIPTION_RELEASE, PROCESS_EXIT, GETATTR, FORGET, LOOKUP, GETATTR and PROCESS_EXIT (the last three most likely access child 2's). **There is no CREATE claim for `group-tail`: the call never reached the host.**
- m1's: Allocated 12, Retired 12, Open 0.
- The view controller had Leases 6, Reservations 0, Tickets 14, no recall, and `Matches` true (zq3-r21: Tickets 10).
- The transport had 10 cells: 3 ordinary and 7 control.
- `runtime.stderr` and `exporter.stderr` were empty, and `t-fail-fence.json` and `p11-fence.json` report `fenced: false`.

**What the evidence shows:**
- **The `ENOSPC` is guest-local.** The host has no claim for the `create` and recorded no refusal.
- **6m4's retirement did not move the host's floor.** m0 still had 7 retirable TERMINAL claims at the failure OBSERVE. The host accepts their retirement, as zq3-r21 showed, but none was retired.
- **m0's session blocked on a `VIEW_RETIRE` that timed out (`-110`, the oc4 30 s reply bound).** The block line was logged about 30 ms after the `create` returned. 33.26 s is one 30 s reply wait plus about 3 s.
- **m1 blocked on its `PROCESS_EXIT` with `-28`** 84 ms later: the same pattern as zq3-r21's m1 (6m4's known limit, "a retirement returns only its own lane's bytes").

**Likely sequence (a hypothesis until the owner snapshots are decoded):**
1. The `create`'s charge failed with ENOSPC.
2. 6m4's `hfs_retire_shared` started a retirement round, and its view-ticket retirement sent a `VIEW_RETIRE` on the control lane.
3. That exchange got no reply within 30 s, so the guest blocked m0 as an uncertain control result.
4. The single retry then failed ENOSPC again.

Open questions:
- Why the `VIEW_RETIRE` went unanswered: whether it reached the exporter or the host at all, and whether a control-lane slot, the control byte partition or the ordinary byte partition was full;
- which lane's budget the `create` ran out of.

`credentials-owner-live.tar` and `credentials-owner-after.tar` (BoltDB ledgers for the controller, both providers and the transport) and `f3-evidence.tar` hold the host state. The full guest `dmesg` is in `p11-guest-dmesg.txt`.

Ruled out:
- **A host fence or refusal:** the exporter recorded no refusal and both stderr files were empty.
- **An ETIMEDOUT from a probe syscall:** the `create` failed `ENOSPC`, not `ETIMEDOUT`; only the guest's internal `VIEW_RETIRE` exchange timed out.
- **Recall interference:** no recall ran during T1. The two bootstrap recalls completed at 14:27Z, about 20 minutes earlier.

**Effect.** T2-T11 did not run. In particular, the T3 s64 peer-rename guard, T7 (pod) and the hh4/dh4 paths in T9-T11 remain without a real run.

## R22-2 (operator): Phase 9 started about 20 minutes after READY

- **What happened:** the driver ran in the background as the sheet requires. It reached READY at 14:27:19Z and exited at 14:28:04Z. The operator waited for the background-task completion notification instead of watching `p8-bootstrap.jsonl`. That notification arrived only at 14:47:11Z, so Phase 9 started at 14:47:23Z (`late_after_ready: true`).
- **Effect on the run:** the guard budget printed GO for Phase 9 (2362 s left) and T1 (2341 s left, latest start 15:10:46Z). No step was skipped for budget, and the T1 outcome is not attributed to the delay.
- **Effect on Rocky:** its downtime grew by about 20 minutes.
- **For the next run:** watch the driver's journal or `summary.json` with a bounded wait that cannot match itself, and do not rely on the completion notification alone. The 5-minute rule is unchanged.

## Host events and facts

- **f3:** boot `1794574308` unchanged. The iormesh VMs iom1-3 were Stopped at Phase 1, at Phase 5 step 1 and at Phase 12. No k3s VM was listed.
- **N1:** stamp `2026-11-26`, dnf #68 unchanged. The nightly gate printed GO at 14:21Z and 14:25Z. The run ended before 14:53Z, far ahead of the 21:25Z bound.
- **Rocky:**
  - before the run: PID 93782 (07:09:20Z), boot `c7fd367a-…`;
  - stopped cleanly at 14:26:01Z (`Stopped` by 14:26:05Z);
  - started at 14:51:23Z: PID 50982 (lstart 14:51:24Z), boot `99de7fb2-c8e3-4b60-afef-7b11e77b4142`;
  - pinned SSH answered at 14:51:43Z, after one banner timeout while Rocky was booting.
  - **Down 25 min 42 s for the run** (R22-2 accounts for about 20 minutes of it).
- **Run values:**
  - window plan `ce4cf578…`;
  - observer EA `e0831c1f…`, removed (`restored_pre_run: true`, `authorized_keys` back to `e08542dc…`);
  - volume `afc5779b-…`;
  - B `56dea52d…`, U `83bf3167-…`;
  - guest boot `c56ff671-…`.
- **Guard:** one SIGTERM at 14:49:53Z to the root guard PID 41939, gone by 14:49:56Z. `runtime_running_at_guard_exit` False, exporter pid 41953.
- **Phase 11:**
  - the guest powered off at 14:50:48Z, and the child was gone at 14:50:52Z;
  - host cleanup destroyed the tap and the vmm, and the UEFI vars stayed unchanged (`0be7dec3…`);
  - the native fusefs mount was unmounted normally;
  - the f3 observer key was deleted;
  - the window was restored (`mount_restored: true`).
- **Final state:**
  - Rocky runs the stock `5.14.0-741.27.1.el9_9` = grub default = `saved_entry`, with `next_entry` empty;
  - Rocky is Enforcing, with no `/.autorelabel` and systemd `running`;
  - no `hfs-xz3` file in `/boot`, no hfs-xz3 BLS entry, and no `/lib/modules/6.1.190-hfs-xz3` or `/etc/homelabfs`;
  - on f3, Rocky is the only running VM, `/dev/vmm` holds only `rocky` and swap is 0;
  - no fdescfs/fusefs mount; `fdescfs.ko` (39) and `fusefs.ko` (40) stay loaded by the reviewed plan, so the next run needs Phase 1a again;
  - the f3 observer directory holds only `rocky-known_hosts`.
- **Operator notes:**
  - every line ran through a bash wrapper that sourced `session.sh` and `vars.env`; zsh never ran a session line;
  - two wrapper calls were refused before running (relative line-file paths; nothing reached a host) and were rerun with absolute paths;
  - Phase 11's ETIMEDOUT scan line found no release or T6-T11 file to scan (the helper printed its usage), as in zq3-r21;
  - the Phase 9 `LSM: initializing` grep matched nothing. This kernel prints `LSM: Security Framework initializing` (0.265 s); `SELinux:  Initializing.` was present, so this is a sheet grep wording issue, not a stop.

## Follow-up

- **R22-1:** decode the owner snapshots and the guest budget offline. The questions are why m0's `VIEW_RETIRE` went unanswered for 30 s and which lane's bytes the `create` lacked.
  - The 6m4 retirement round did not retire any of m0's 7 retirable claims on the host.
  - 6m4 does not log its retire-and-retry path, so the guest log cannot show whether it ran. A log line there would help the next run.
  - m1's `PROCESS_EXIT (-28)` repeats 6m4's known limit.
- **R22-2:** the operator's Phase 8 wait (above).
- **Validated again by this run:**
  - sj4 (R20-1);
  - tj4/wj4 (advances fire);
  - vj4 (no CALL_CONFLICT recalls; self admissions);
  - the `8cadae40` exporter launch;
  - `hfs_fdattr` mount options and fd-based `fchown`/`fchmod`;
  - R13-1 (unmounts in 0 s after the guard stop).
- **Still without a real run:** T2-T11 (the s64 peer-rename guard, T7, hh4/dh4 in T9-T11). T5 stays deferred.
- **The iormesh session can be told that f3 is free** (run over at 14:53Z).
