# zq3-r20: 1h4 works and Rocky runs stock 741.24.1, but the host refuses a non-owner's walk below the 0700 root (EACCES), and the guest never asks for xf4's advance (2026-11-22)

Run sheet: [filled plan](../20261122-zq3-r20-filled-plan/run-sheet.md),
with review round 1 applied (merge `1afd968`).

Candidate r20 `b37bcb06…` ([xz3-20261122-r20](../../../../tools/integration/artifacts/evidence/xz3-20261122-r20/README.md)):
- the 1h4 + 4h4 guest kernel, build ID `b4d6cd69…`;
- the 3h4 daemon `c04c519f…`;
- the exporter `2fd7e32f…`, reused.

Times are UTC. Private evidence: `/home/paul/tmp/claude/hfs-zq3-r20-fill/evidence` (run log `RUN-LOG.md`).

## Before the run (Paul's decisions, 2026-11-22)

- **Custom test kernel kept.** It boots only in the throwaway test VM; Rocky stays on stock.
- **Rocky's maintenance reboot.**
  - Rocky's nightly upgrade (dnf #66, 33 packages) installed stock kernel `741.24.1` as the grub default and deferred the reboot ("standalone rocky requires explicit maintenance approval").
  - The committed `rocky_kernel.py` refuses to install while the running kernel differs from the default. A first fill attempt confirmed this in a FakeRocky simulation.
  - With Paul's approval Claude rebooted Rocky into `741.24.1` (`vms.sh stop-rocky` then `start-rocky`, a clean ACPI shutdown). It was down from 16:35:51Z until SSH answered at 16:36:20Z: Enforcing, no relabel, `systemctl` running.
  - Fresh facts were taken at 16:37:03Z.
- **iom VMs and swap.** iom1-3 stayed stopped (iormesh session coordinated), and f3 swap was 0, so no swap clearing was needed.

## Outcome

Phases 0 to 9 passed. T1 (`credentials`) failed in its first access check (new finding R20-1). T2 to T11 did not run. Phase 11 took the guard-SIGTERM branch, and restoration was clean.

**What held:**
- **Phase 3:** the pod rootfs is still `34a71569…`; dnf #66 did not change it.
- **Phase 4 step 1a:** it qualified the new daemon `c04c519f…` (uid/gid 0, pid 35139).
- **Phase 5:** the estimate margin was +0.26 GB and the UMA drain was run.
- **Phase 6:** passed with a 0.37 GB margin.
- **Phase 8:** READY (launch 17:13:51Z), both signatures, mounts 391/408, `usable` with L=2.
- **Phase 9:** passed. OBSERVE net median was 0.56 s.
- **1h4 works.** Five times in T1 the guest logged `observation re-check failed (VIEW_ACQUIRE BLOCKED for the target before a reinstall); acquiring again at once`. There was no EAGAIN and no retry-limit line, so R19-1 is gone.
- **No guest kernel WARNING or BUG**, no hfs block line during the case, no fence, `runtime.stderr` empty, no AVC records.
- **Unmounts:** after the guard stop, both returned `rc 0` within 1 s.
- **Phase 12:** Rocky started on stock `741.24.1` (PID 40425, boot `9a3774b8-…`), with no relabel. U2 removed the test kernel.

| Phase | Result |
|---|---|
| pre | Rocky maintenance reboot into 741.24.1 (approved) |
| 0-1, 1a | passed |
| 2-7 | passed (new daemon qualified, UMA drain, Phase 6 margin 0.37 GB) |
| 8-9 | passed (READY, `usable` L=2) |
| T1 | **fail** (R20-1) |
| T2-T11 | unrun (T5 deferred) |
| 11-12 | completed; Rocky on stock 741.24.1 |

## R20-1: the host requires search permission on every ancestor, so a non-owner below the 0700 root gets EACCES

**What happened.** T1 started at 17:15:53Z and ended after 103.1 s with `dispatching-uncertain`.
- Admission took 7.6 s.
- These steps all returned errno 0: mkdir, open-leaf, fchmod-leaf 0777, create `group`, fchown 41002:42002 (R18-1 stays fixed), fchmod 0640, pwrite.
- Then the access child (uid 41001, gid 42001, groups `[42001,42002,42003]`) failed with `PermissionError` errno 13 (EACCES), and the case was refused ("supplementary group access").

**Analysis** (offline decoding of the f3, live and after snapshots; daemon code at `d71ae02`):
- **The child failed on the leaf, never on `group`.**
  - Its only call (provider-0 claim 31) is a GETATTR with credentials fsuid 41001, fsgid 42001, groups `[42001,42002,42003]`, result `Permission`, refused before planning.
  - The object is the leaf directory, not `group`.
  - During `openat(leaf_fd, "group")`, the guest's path walk calls `hfs_permission(leaf, MAY_EXEC)`, which refreshes the leaf with a GETATTR (`hfs_inode.c:498-541`). The host answers `Permission`, which maps to -EACCES (`hfs_session.c:1269`). It is returned before `generic_permission`, so the guest logs nothing.
- **The host refuses in `revalidatePath`** (`internal/guestbridge/dispatch.go:314-325`).
  - It re-walks the leaf's recorded ancestors and requires `search(cred, parent.Attr)` on each one.
  - The volume root is 0700 owned by uid 0 (cell 1568: DIRECTORY, mode 448, revision 1, never changed), so uid 41001 cannot search it.
  - The spec requires this today (`docs/spec/Retained-9p-protocol.md:424-427`). It is stricter than POSIX: on Linux, an `openat` through a directory descriptor never re-checks the directories above it.
- **Ruled out:**
  - guest credential capture: groups reach the host intact;
  - guestbridge `guestCred`;
  - fsservice's own group check (`permissions.go:176` checks supplementary groups correctly);
  - stale guest inode attributes.
- **Effect:** every T1 child action through the leaf fails the same way. The "different groups must deny" check would currently pass for the wrong reason.

**Fix (task sj4, daemon and spec):** keep the identity re-walk (a renamed or removed ancestor is still Stale) but drop the per-ancestor search check. The guest VFS already checks walk permission on the directories a process actually walks, and fsservice still checks the immediate parent per operation.

## xf4's advance never fired: the guest's cacheless test is never true

`controller.recalls.advances` was 0, and T1 still paid five EXPECTED_TARGET recalls (all DOMAIN-scoped, 4.4-6.0 s), plus CALL_CONFLICT recalls for MKDIR (ALL, 26.6 s) and SETATTR (ALL, 11.6 s).

- **The guest never asked.** None of the recovered VIEW_ACQUIRE payloads carries `"advance"`, and no "advance not adopted" line was logged. So the host's conditions were never reached.
- **Cause.**
  - The guest sends advance only when `hfs_view_cacheless_locked` is true (patch 0004). That test requires `nr_mappings == 0` and an empty `view->mappings`.
  - But `hfs_view_register_inode`, called from `hfs_inode_get`, registers every inode there, not only mmapped ones.
  - The mount root inode lives as long as the mount, so the view is never "cacheless".
- **Why the tests missed it.**
  - The guest unit tests made the view cacheless by unregistering the only inode.
  - The T1 model replay sends advance whenever its flag is on, without modelling the guest's cacheless state.
- **Fixes:**
  - **tj4 (guest kernel):** count only mmapped inodes (a per-inode flag plus `view->nr_mmapped`), keeping `view->mappings` as the STOP inventory.
  - **uj4 (tests):** feed the model replay the guest's real cacheless state.
- **Still open:** the CALL_CONFLICT recalls (task vj4, xf4 follow-up F) remain the largest cost once advance works.

## Next candidate

Rebuild both the guest kernel (tj4) and the daemon (sj4). The daemon rebuild also picks up the merges since `d71ae02`: 2h4, 0a4, fh4, a84, l64, nj4. Paul approved a native exporter rebuild on f3 for 9h4, so the exporter is rebuilt rather than reused.

## Host events and facts

- **f3:** boot `1794574308` unchanged.
- **N1:** stamp `2026-11-22`, dnf #66. Nightly gate GO at 17:10Z and 17:13Z.
- **Rocky:**
  - after the maintenance reboot: PID 30974 (16:36:05Z), boot `23192359-…`;
  - stopped cleanly at about 17:13Z;
  - started at 17:19:43Z: PID 40425, boot `9a3774b8-fcb1-471c-9b00-8a1bfb089f57`;
  - down about 6 minutes for the run.
- **Run values:**
  - UEFI vars `5c95aae0…` at the facts;
  - window plan `9dd09dd0…`;
  - observer EA `62b603d9…`, removed (`restored_pre_run: true`);
  - volume `188f8586-…`;
  - B `89cf99ad…`, U `9f4d6746…`.
- **Guard:** SIGTERM at 17:18:25Z, gone at 17:18:35Z. Exporter pid 37268.
- **Final f3 state:** Rocky running, iom1-3 stopped, swap 0. The iormesh session was told that f3 is free.

## Follow-up tasks

- **sj4:** drop the per-ancestor search check in `revalidatePath` (R20-1).
- **tj4:** the guest's cacheless test counts mmapped inodes only, so advance can fire.
- **uj4:** the model replay models the guest's cacheless state.
- **vj4:** CALL_CONFLICT recalls against the domain's own queued releases (xf4 follow-up F).
- **9h4:** merge, then the native exporter rebuild on f3 (approved).
- **Still open:** tg4, ch4, dh4, 5f4, hh4, bh4, k84, ue4, s64.
