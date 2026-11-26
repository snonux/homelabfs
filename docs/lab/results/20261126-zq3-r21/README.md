# zq3-r21: R20-1 is fixed, the xf4 advance and vj4 self admissions work, and `hfs_fdattr` mounts cleanly, but T1's `fchmod` of a second file fails ENOSPC and both sessions block on `-28` (2026-11-26)

Run sheet: [filled plan](../20261125-zq3-r21-filled-plan/run-sheet.md)
(refill of 2026-11-26 after the approved maintenance reboot, merge `181d195`).

Candidate: the second r21 rebuild `f21450ec…` ([xz3-20261125-r21](../../../../tools/integration/artifacts/evidence/xz3-20261125-r21/README.md)):
- the guest kernel, build ID `bd4fa753…` (s64, hh4, dh4, 5f4, kn1, wj4 item 1, tj4);
- the go1.27.4 daemon `c7b89648…` (vj4, sj4, dh4, s64 host, hh4 host);
- the exporter `8cadae40…` (first run).

`hfs_fdattr` was enabled on both mounts. Times are UTC. Private evidence:
`/home/paul/tmp/claude/hfs-xz3-20261125-r21/zq3-r21-fill/evidence` (run log `RUN-LOG.md`).

## Before the run

- Paul gave the final GO for the sheet (A1-A8, including the Phase 1a unload and T7). T5 stays deferred.
- Rocky's maintenance reboot into stock `741.27.1` (Paul's approval, 06:46Z) and the refill are in the [filled plan](../20261125-zq3-r21-filled-plan/README.md).
- The iormesh VMs iom1-3 were stopped by the iormesh session. They stayed stopped through the whole run and the sheet never touched them.
- f3 swap was 0, so there was no Phase 0b.

## Outcome

Phases 0 to 9 passed. T1 (`credentials`) failed with a new finding, R21-1. T2 to T11 did not run. Phase 11 took the guard-SIGTERM branch, and restoration was clean.

**What held:**
- **Phase 1a:** a normal `kldunload` of fusefs (id 38) and fdescfs (id 37), `rc 0` each.
- **Phase 3:** the pod rootfs manifest is the predicted `34a71569…`. The kernel entry `…-6.1.190-hfs-xz3` was added as a non-default entry.
- **Phase 4 step 1a:** it qualified the new daemon `c7b89648…` (uid/gid 0, groups `[0,0,5]`, pid 89245).
- **Phase 5:** the nightly gate printed GO at 07:03Z. The estimate margin was +0.14 GB, so the UMA drain ran and freed about 0.23 GB.
- **Phase 6:** passed with a 0.29 GB margin (15,056,814,296 bytes).
- **Phase 8:**
  - launch at 07:04:58Z, READY at 07:05:32Z (34 s);
  - both signatures by the watcher;
  - mounts 391 and 408;
  - `usable` passed with L=2;
  - the exporter `8cadae40…` launched cleanly, and `exporter.stderr` stayed empty.
- **`hfs_fdattr` (bl4/s64), first VM run:** both mountinfo rows read `rw,cache=mmap,hfs_fdattr,msize=65536,trans=virtio,version=9P2000.L.hfs1.4`, with `hfs_fdattr` once, right after `cache=mmap`. The batch fill accepted them (`fd_attr: true`).
- **Phase 9:** passed, starting 1 minute after READY with 3506 s of guard budget left.
  - OBSERVE seq 3, with `Open 0` on both providers;
  - Enforcing; no hfs block line;
  - 984 timing: SSH no-op median 0.223 s, OBSERVE net median 0.474 s, max 0.546 s.
- **R20-1 is fixed (sj4).** The access child (uid 41001, groups `[42001,42002,42003]`) read `group` below the 0700 root (`group-secret`, status 0). The second child, which must be denied, got EACCES as designed.
- **xf4 advance and vj4 work.** `recalls.py` reported:
  - `advances` 8 and `self_admissions` 8;
  - only 2 recalls, both the bootstrap `RELEASE/VIEW_RELEASE` (DOMAIN, about 5 s);
  - no CALL_CONFLICT and no EXPECTED_TARGET recall (zq3-r20 had 5 EXPECTED_TARGET and 2 CALL_CONFLICT).
- **T1 admission:** 8.6 s. The largest gap was 2.1 s at `open-root[0] → stat-root[0]`. No deadline was hit and there was no ETIMEDOUT.
- **No guest kernel WARNING or BUG, no AVC, no fence.** `runtime.stderr` was empty.
- **Unmounts after the guard stop:** both returned `rc 0`, in 0 s and 1 s. The guard stop and the unmounts added no new block line.
- **Phase 12:** Rocky started on stock `741.27.1`. It ran once in vm-bhyve, with no relabel. U2 removed the test kernel.

| Phase | Result |
|---|---|
| 0-1, 1a | passed |
| 2-7 | passed (daemon qualified, UMA drain, Phase 6 margin 0.29 GB) |
| 8-9 | passed (READY, `usable` with L=2, `hfs_fdattr` on both mounts) |
| T1 `credentials` | **fail** (R21-1). R20-1 passed; advances 8, self_admissions 8 |
| T2 `sync`, T3 `metadata` (s64 peer-rename guard), T4 `namespace` | unrun (T1 failed) |
| T5 `mapped-pressure` | unrun (deferred, re-plan pending) |
| T6, T7 (pod), T9, T10, T11, T8 | unrun (T1 failed) |
| 11-12 | completed; Rocky on stock 741.27.1, custom kernel removed |

The T3 s64 peer-rename guard did not run, so `hfs_fdattr` was checked only by the mount rows and by T1's fd-based calls. T1's `fchown` and `fchmod` through the open file description returned 0 for `group`.

## R21-1: T1's `fchmod` of a second file fails ENOSPC, and both sessions block with `-28`

**What happened.** T1 started at 07:06:50Z and ended after 44.1 s with `dispatching-uncertain` ("missing complete guest terminal").

The guest.py named events (`ctrl-run/credentials/stdout.log`), each with errno 0 unless noted:
1. `mkdir-exclusive`, `open-leaf`, `fchmod-leaf 0777`;
2. `create-exclusive group`, `fchown 41002:42002`, `fchmod 0640`, `pwrite` of 12 bytes;
3. access child 1 read `group-secret` (status 0); access child 2 was denied with EACCES (status 256), the expected outcome;
4. `create-exclusive group-tail` 0, `fchown group-tail 41002:44022` 0;
5. **`fchmod(group-tail, 0640)` returned errno 28 (`ENOSPC`)** at guest monotonic 148.475 s, about 0.2 ms after it was invoked. So it failed locally, before any reply wait;
6. the case then failed with `PermissionError` errno 13. This is the consequence of the blocked session ("later operations on this mount fail with EACCES or EHOSTDOWN").

The guest kernel log at the same moment:

```text
[148.522092] 9p-hfs: session 05e05279-… blocked by DESCRIPTION_RELEASE failure on DESCRIPTION_RELEASE (-28), exporter ERROR -
[148.529747] 9p-hfs: session 6a28e672-… blocked by process-exit failure on PROCESS_EXIT (-28), exporter ERROR -
[148.531424] 9p-hfs: session 05e05279-… view control worker stopped, error -112, exporter ERROR -, at VIEW_WAIT (… view phase LIVE, recall -)
[148.534112] 9p-hfs: session 6a28e672-… view control worker stopped, error -112, exporter ERROR -, at VIEW_WAIT (… view phase LIVE, recall -)
```

**Host side (read-only):**
- The failure OBSERVE (seq 5) was OBSERVED, SERVING and complete, with no fence.
- m0's provider ledger: Allocated 50, Retired 41, Entries 9, Open 0. The newest claim is #50 SETATTR, TERMINAL.
- m1's: Allocated 10, Retired 10, Open 0.
- The view controller had Reservations 0 and Tickets 10.
- `runtime.stderr` and `exporter.stderr` were empty, and `p11-fence.json` reports `fenced: false`.

So the host recorded no refusal. The `-28` came from the guest, and both sessions hit it within about 8 ms:
- m0 on its `fchmod` and the following DESCRIPTION_RELEASE;
- m1 on a PROCESS_EXIT, the child's exit.

**Root cause (task 6m4).** The guest's global ordinary byte partition (512 MiB, shared by both mounts) was full of retained but retirable owners. `hfs_call` funded the next request's 4 MiB transport reservation and then failed on its completion package, a step that never retired anything.

How the guest charges ordinary bytes (`hfs_controlled_reserve_owned`; the counters are global across mounts):
- Every `hfs_call` takes a 4 MiB transport reservation plus a completion package of about 30.9 MiB (`hfs_completion_capacity(false, HFS_BODY_MAX)`, struct sizes of a few KiB left out). That is about 34.9 MiB per request. The request keeps both until `hfs_retire` frees it after the host's RETIRED.
- Every VIEW_ACQUIRE ticket takes 1 MiB plus a view package of about 24.5 MiB, and keeps both until VIEW_RETIRE.
- Since the controlled cleanup calls (DESCRIPTION_RELEASE, PROCESS_EXIT, ...) send on the ordinary lane, they use the same bytes (`hfs_call_control_bytes`).

What the owner snapshot shows (`credentials-owner-live.tar`, decoded offline):
- **m0's claims:** floor 41, high 50. Claims 42 to 50 are TERMINAL and unretired:
  - 42 LOOKUP `group`, 43 GETATTR and 44 PROCESS_EXIT, all from access child 2 (uid 41001, groups `[42001,42003]`, an exited process);
  - 45 to 50 from the T1 parent (uid 0): GETATTR, GETATTR, LOOKUP (NotFound), CREATE `group-tail`, GETATTR, and SETATTR (the `fchown`, gid 44022).
- **The controller:** 7 unretired m0 VIEW_ACQUIRE tickets, families `a2992a…` 1-3 and `c422868f…` 16-19, plus 3 VIEW_RELEASE tickets on the control side. m1 had no unretired claim or ticket.

The guest frees a request only after the host retires it, and a ticket only after VIEW_RETIRE, so the guest still held all of these. That comes to at least 9 × 34.9 + 7 × 25.5 ≈ 492.6 MiB of the 512 MiB. Less than 19.4 MiB was left, before descriptions, processes, cache pages and m1's owners.

How that produced each failure:
- **`fchmod`:** its 4 MiB reservation fit and its 30.9 MiB package did not. `hfs_call_reserve` retired and retried only when the reservation itself failed. The package was taken afterwards in `hfs_call_prepare` (`hfs_completion_new`), with no retirement. So the call returned ENOSPC locally, about 0.2 ms after it was invoked, and sent nothing.
- **The two cleanup calls:** the following DESCRIPTION_RELEASE (m0) and PROCESS_EXIT (m1) failed the same way and blocked both sessions. The view workers then stopped with -112.
  - m1's PROCESS_EXIT failed for want of m0 retiring. m1 had nothing of its own to retire (floor = high = 10). The bytes were held by m0's retirable requests, and m0 retires only under its own allocation pressure or in recall cleanup, and no recall ran. Even with m0's `fchmod` fixed alone, a later m1 call could still hit this.
- **The host floor:** it stayed at 41 (the failure OBSERVE and both snapshots). The host accepts RETIRE with an exited issuer's credentials (`retireControlled`, `allowExited`). So a retirement attempt would have retired claims 42-44 (one credential run) and returned about 105 MiB, enough for the `fchmod`.

Ruled out:
- **The host:** the exporter recorded no refusal, fence or stderr.
- **s64's description path:** it failed before any argument was built. The SETATTR args are only built after the charge.
- **Slot or received exhaustion:** about 290 of 512 ordinary slots and 48 of 128 received slots were in use.
- **The tag pool.**

Contributing, not a bug proven here:
- The 7 acquisition tickets (about 179 MiB) stayed charged. Before this run, recalls disposed cache pages, and the cleanup work ran `hfs_retire` and `hfs_view_retire` along the way (`hfs_mmap.c`). With xf4 advances and vj4 self admissions, no recall ran during T1. Retirement then happened only under allocation pressure.
- Group-tail is also the deepest point T1 has reached in any run.

Fix: task 6m4 (series regenerated: patch 0003 `hfs.h` and `hfs_session.c`, patch 0004 `hfs_lease.c`).
- **Requests:** `hfs_call_reserve` takes the reservation and the package together. On ENOSPC/EAGAIN from either it releases both, runs `hfs_retire_shared`, and retries once.
- **`hfs_retire_shared`:** retires the caller's session and every other listed session of the same provider, since the budget is global. The other sessions are pinned under `hfs_sessions_lock` and retired with no lock held; kill_sb and a failed mount wait for those pins before retaining or freeing a session.
- **Lease tickets:** `hfs_view_ticket_prepare` gets the same treatment for its reservation, family credit and view package.
- **What it cannot fix:** retirement returns only the bytes of the retired requests' own lane. When nothing on that lane is retirable anywhere, the call still fails cleanly with ENOSPC.
- **Not covered:** unretired view tickets are still not offered for retirement while no recall runs.

The Red-first regressions are controlled_received `--scenario package` (`TestControlledReceivedActualPackageRetirement`, `TestControlledReceivedActualTicketPackageRetirement`).

**Effect.** T2-T11 did not run. In particular, the T3 s64 peer-rename guard, T7 (pod) and the hh4/dh4 paths in T9-T11 remain without a real run.

## Host events and facts

- **f3:** boot `1794574308` unchanged. The iormesh VMs iom1-3 were Stopped at Phase 1, at Phase 5 step 1 and at Phase 12. No k3s VM was listed.
- **N1:** stamp `2026-11-26`, dnf #68 unchanged. The nightly gate printed GO at 07:00Z and 07:03Z. The run ended before 07:11Z, far ahead of the 21:25Z bound.
- **Rocky:**
  - before the run: PID 86754 (06:46:29Z), boot `69aefe1f-…`;
  - stopped cleanly at 07:04:19Z (`Stopped` by 07:04:23Z);
  - started at 07:09:18Z: PID 93782 (lstart 07:09:20Z), boot `c7fd367a-0eac-4514-954c-40be9d3a9804`;
  - pinned SSH answered at 07:09:37Z.
  - **Down 5 min 18 s for the run.**
- **Run values:**
  - window plan `3ca6dcd6…`;
  - observer EA `536c106f…`, removed (`restored_pre_run: true`, `authorized_keys` back to `e08542dc…`);
  - volume `ec009977-…`;
  - B `bd2b4518…`, U `9d616812-…`;
  - guest boot `78896e37-…`.
- **Guard:** one SIGTERM at 07:08:09Z, gone by 07:08:12Z. `runtime_running_at_guard_exit` False, exporter pid 91092.
- **Phase 11:**
  - the guest powered off at 07:08:53Z, and the child was gone at 07:08:57Z;
  - host cleanup destroyed the tap and the vmm, and the UEFI vars stayed unchanged;
  - the native fusefs mount was unmounted normally;
  - the f3 observer key was deleted;
  - the window was restored (`mount_restored: true`).
- **Final state:**
  - Rocky runs the stock `5.14.0-741.27.1.el9_9` = grub default = `saved_entry`, with `next_entry` empty;
  - Rocky is Enforcing, with no `/.autorelabel` and systemd `running`;
  - no `hfs-xz3` file in `/boot`, no hfs-xz3 BLS entry, and no `/lib/modules/6.1.190-hfs-xz3` or `/etc/homelabfs`;
  - on f3, Rocky is the only running VM, `/dev/vmm` holds only `rocky` and swap is 0;
  - no fdescfs/fusefs mount; `fdescfs.ko` (38) and `fusefs.ko` (39) stay loaded by the reviewed plan, so the next run needs Phase 1a again;
  - the f3 observer directory holds only `rocky-known_hosts`.
- **Operator notes:**
  - every line ran through a bash wrapper that sourced `session.sh` and `vars.env`;
  - Phase 11's ETIMEDOUT scan line found no release or T6-T11 file to scan, because none ran (the helper printed its usage);
  - `rockywait`'s first try hit one banner timeout while Rocky was booting.

## Follow-up

- **R21-1 (task 6m4):** decoded and fixed in the guest series (patches 0003 and 0004); see the root cause above.
  - r22 needs a new candidate build: the guest kernel changes, and the daemon and exporter do not.
  - Not covered by 6m4: acquisition tickets stay charged while no recall runs, and a retirement returns only its own lane's bytes.
- **Validated by this run:**
  - sj4 (R20-1);
  - tj4/wj4 (advances fire);
  - vj4 (no CALL_CONFLICT recalls; self admissions);
  - the `8cadae40` exporter launch;
  - `hfs_fdattr` mount options.
- **Still without a real run:** T2-T11 (the s64 peer-rename guard, T7, hh4/dh4 in T9-T11). T5 stays deferred.
