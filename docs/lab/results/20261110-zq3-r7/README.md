# zq3-r7: both mounts and READY with every step ok; guest access refused (2026-11-10)

Run sheet: [filled plan](../20261110-zq3-r7-filled-plan/run-sheet.md).
Candidate `66b5bb2b…` (task f64): the c64 guest kernel with SELinux in
`CONFIG_LSM` (build ID `2ce818d9…`) and the d64 daemon `9b7eed8d…`. Times are
UTC. Private evidence: `/home/paul/tmp/claude/hfs-zq3-r7-fill/evidence`.

## Outcome

**Milestones (all first times):**
- Phase 4 step 1a qualified the d64 daemon on f3: `--check-process-observer`
  as root under the native guard gave the exact `homelabfs-process-observer/1`
  record for `9b7eed8d…`.
- Phase 8 completed with every step `ok`. Both signatures exited 0.
  `mount-0` and `mount-1` both completed, and the runtime reached `READY`
  (`domains: 2`, `CONTROLLED_LOCAL`) 35.3 s after launch. The d64 fixes for
  R6-1 (per-domain fence) and R6-2 (guest-cell reorder) held: no
  `EKEYREJECTED` and no view-control `-110`.
- The batch plan (`ad6ba7cd…`), the READY admission and OBSERVE sequence 1
  (both domains `REOPENED`, fences 2 and 1) all passed.
- The guest ran with **SELinux enforcing** (`getenforce` `Enforcing`, enforce
  `1`, `SELinux: Initializing`). It logged no AVC denials during the whole
  boot.
- After the run, Rocky's stock boot needed **no relabel reboot**: one
  vm-bhyve run, SSH after 26 s, `/.autorelabel` absent. This confirms the
  b64 explanation (R6-3).

**New failures:**
- **T1 refused at admission (R7-1):** `guest.py` refused with "exact
  controlled mount tuple differs" before touching anything. The probe batch
  stopped with `required actual syscall case failed: credentials`, which
  puts the controller in error.
- **T6 failed (R7-2):** root could not open `m0` (`EACCES`). Even `ls -ld` on
  either mount root returns "Permission denied", with no AVC record and no
  guest kernel message.

| Phase | Result |
|---|---|
| 0-7 | passed: D1-a unload, process observer `9b7eed8d…`, window plan `7458a22c…` and FSID `5900ff0b:00000059`, UMA drain (+0.15 GB), Phase 6 margin 0.57 GB, tap100 |
| 8 | **READY, every step ok** (both mounts) |
| 9 | passed: OBSERVE seq 1, two 9p mounts, SELinux enforcing |
| T1 | fail: admission refused (R7-1); controller in error |
| T2-T5, T9, T10, T11 | unrun (controller error) |
| T6 | fail: `EACCES` opening m0 (R7-2) |
| T7 | refused at admission ("exact controlled m0 mount tuple differs", R7-1) |
| T8 | **holds**: m1 unmounted normally, the probe refused with the exact admission error, the directory is empty |
| 11-12 | completed (below) |

Rocky was stopped from 03:32:03 to 03:48:51, about 17 minutes. Phase 11 ran
in the mandated order:
1. One SIGTERM to the guard. The runtime stopped cleanly (`runtime_exit` 0);
   the guard's `breach.json` records only that SIGTERM.
2. Evidence tars, then m0 unmounted normally with `rockyt` (rc 0).
3. Observer line removed (`restored_pre_run: true`, `e08542dc…`).
4. Grub check, and the AVC capture: none.
5. Guest poweroff.
6. Host cleanup (tap and vmm destroyed, original vars unchanged).
7. Native FUSE unmount.
8. f3 observer key deleted with the plain literal command.
9. Window restore (`mount_restored: true`).

The test kernel was then removed. fdescfs and fusefs stay loaded on f3.

## Findings

- **R7-1, the hfs mounts show no options (root cause known).** The patch's
  `hfs_super_ops` (`tools/integration/linux9p/patches/0003-hfs1-guest-vfs.patch`)
  sets no `.show_options`, so mountinfo shows only `rw`. Patch 0001 extends
  `p9_show_client_options`, but nothing calls it for an hfs superblock.
  `guest.py` `check_tuple` and `pod_fixture.py` require `cache=mmap`,
  `version=9P2000.L.hfs1.4` and `msize=65536` there. A kernel or tool fix
  is needed before any `guest.py` case can run.
- **R7-2, the mount roots are inaccessible to root (open, main finding).**
  This happened right after both mounts and READY, under SELinux enforcing
  with no AVC. The evidence is in `p10-owner-live.tar` (after T1, before T6),
  `p10-owner-live-2.tar` (after T6 and T8), `f3-evidence.tar`, `t6-*.txt` and
  `r7-1-mountinfo.txt`. Analysis follows in a task.
- **R7-3, run sheet errata (operational).**
  - The guest's `findmnt -t 9p` exits 0 when nothing matches, so `|| echo
    no-9p` never prints. Count ` 9p ` lines in `/proc/self/mountinfo`
    instead.
  - Phase 11 step 6 waits for `child-gone` **and** `no-vmm`, but the run's
    vmm context stays until step 7's `host.py cleanup` destroys it. Wait for
    `child-gone` only, as zq3-r6 effectively did.
