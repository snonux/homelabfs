# zq3-r4: bind_super passes; mount-0 blocks in the transport (2026-11-09)

Run sheet: [filled plan](../20261109-zq3-r4-filled-plan/run-sheet.md).
Candidate `89c928d5…` (w54 kernel, task y54); bundle with reservations 1024,
received 256 and 512/512 MiB. Times are UTC. Private evidence:
`/home/paul/tmp/claude/hfs-zq3-r4-fill/evidence`.

## Outcome

Every phase up to bind passed again, with both watcher signatures exit 0.
The w54 budget fix held:
- no ENOSPC and no oops;
- the guest mount got past bind_super;
- the runtime's transport database grew to 256 KiB (64 KiB in zq3-r3), so
  it received more than HELLO.

`mount -t 9p … hfs0 …/m0` then blocked in D state in `hfs_transport` and
did not return within guest-mount's 30 s bound. The step is recorded as
`uncertain`. The runtime stayed at `AWAITING_GUEST_BIND` and exited 1 at
its 120 s deadline. After it exited, the blocked mount ended: no 9p mount
remained and the guest logged no error. **T1-T11 are unrun.**

Rocky was stopped from 19:20:47 to 19:24:20, about 4 minutes. It was back on
SSH at 19:29:41, on stock 5.14.0-687.66.1.el9_8. Cleanup ran in the
mandated order:
- observer key removed (`restored_pre_run: true`, `e08542dc…`);
- guest poweroff in about 15 s;
- host cleanup (tap and VM context destroyed, original vars unchanged);
- native FUSE mount unmounted normally before the window restore;
- window restore with `restoration-summary.json`;
- test kernel removed (23 files).
fdescfs and fusefs stay loaded on f3.

## Findings

- **R4-1, mount-0 blocks waiting for a transport reply (open, main
  finding).** The guest's mount sleeps in `hfs_transport` after
  bind_super. The runtime never left `AWAITING_GUEST_BIND`, so it either
  did not answer the request the guest is waiting for, or the answer
  never reached the guest. The live transport snapshot
  (`p8-owner-live.tar`, taken before the deadline) and the final
  `f3-evidence.tar` show which requests arrived and how they were
  answered; analysis follows in a task.
- **R4-2, operational.** The steps before the mount all behaved as planned:
  - D1-a unloaded fusefs and fdescfs left from r3;
  - the UMA drain freed about 0.18 GB;
  - Phase 6 margin was 0.54 GB.
  The network stayed stable throughout.
