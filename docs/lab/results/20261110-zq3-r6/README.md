# zq3-r6: first real mount and first READY; mount-1 rejected (2026-11-10)

Run sheet: [filled plan](../20261110-zq3-r6-filled-plan/run-sheet.md), run as
option (b) because of the guest SELinux gap (task c64). Candidate `e01fab21…`
(task-964 kernel, z54 daemon `39c92d01…`). Times are UTC. Private evidence:
`/home/paul/tmp/claude/hfs-zq3-r6-fill/evidence`.

## Outcome

**Milestones:**
- `mount -t 9p … hfs0 …/m0` completed in the guest. This is the first real
  HomelabFS 9p mount, and it confirms task 964.
- The runtime reached `READY` (`domains: 2`, profile `CONTROLLED_LOCAL`). A
  read-only OBSERVE (sequence 1) returned `OBSERVED`, with stage `SERVING`,
  complete evidence and a live supervisor.

**New failure:** mount-1 (`hfs1`) failed with `mount(2) system call failed:
Key was rejected by service` (EKEYREJECTED). It got far enough to send the
HELLO that let the runtime reach READY. The guest logged `9p hfs: view
control worker stopped, error -110` (ETIMEDOUT) twice, at about 62 s after
boot. With mount-1 failed, the bootstrap journal is not "READY with every
step ok", so the batch plan cannot be filled and **T1-T11 are unrun**. Most
of them are also blocked by the guest SELinux gap, `getenforce` `Disabled`
(task c64).

Rocky was stopped from 00:33:40 to 00:37:44 (about 4 minutes). It was back
on SSH at 00:43:20 after the expected relabel reboot (vm-bhyve "run 2" at
00:42:41), on stock with SELinux enforcing. Phase 11 ran in the mandated
order:
1. guard SIGTERM (the runtime stopped cleanly, `runtime_exit` 0);
2. m0 unmounted normally with `rockyt` (rc 0);
3. observer line removed (`restored_pre_run: true`, `e08542dc…`);
4. guest poweroff in 10 s;
5. host cleanup;
6. native FUSE unmount;
7. the f3 observer key deleted with the plain literal-path command, which
   the agent safety check accepted;
8. window restore (exit 0, `mount_restored: true`).
The test kernel was then removed (23 files).

| Phase | Result |
|---|---|
| 0-7 | passed: process-observer check, window plan `33205dd5…` and FSID `5900ff09:00000059`, UMA drain, Phase 6 margin 0.57 GB, tap100 |
| 8 | through bind, mount-0 **ok**, mount-1 EKEYREJECTED; runtime READY |
| 9 | OBSERVE seq 1 OBSERVED/SERVING; `getenforce` Disabled (finding) |
| 10 | not run (batch plan unfillable; SELinux gate) |
| 11-12 | completed (above) |

## Findings

- **R6-1, mount-1 EKEYREJECTED (open, main finding).** The second domain's
  mount fails after its HELLO. A credential, enrollment or bind check for the
  second scope is the likely area. The evidence is in `p8-owner-live.tar`,
  `f3-evidence.tar` and `p11-guest-dmesg-full.txt`.
- **R6-2, view control worker ETIMEDOUT (open).** Two `view control worker
  stopped, error -110` lines at about 62 s. These are the 964 diagnostics,
  showing a 30 s view-control exchange timing out, possibly for m1, or for
  m0 after mount-1 failed.
- **R6-3, guest SELinux disabled (task c64, fixed in the recipe, pending a
  rebuild).** Also explains Rocky's relabel reboot after each test boot (task
  b64).
