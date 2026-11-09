# zq3-r5: new daemon qualifies on f3; mount-0 still does not complete (2026-11-09)

Run sheet: [filled plan](../20261109-zq3-r5-filled-plan/run-sheet.md).
Candidate `2e0412a6…` with the z54 daemon `39c92d01…` (task 664) and the
w54 kernel. Times are UTC. Private evidence:
`/home/paul/tmp/claude/hfs-zq3-r5-fill/evidence`.

## Outcome

Phases 0-7 passed. Notable results:
- The new Phase 4 step 1a qualified the new daemon on the f3 host:
  `--check-process-observer` as root under the native guard returned the
  exact `homelabfs-process-observer/1` record for `39c92d01…`, with
  `p4-observer.py` all true.
- The window qualify passed.
- Phase 6 had a 0.53 GB margin after the UMA drain.

Phase 8 again reached bind (both watcher signatures exit 0), and again
`mount -t 9p … hfs0 …/m0` did not return within guest-mount's 30 s
(`uncertain`). Unlike zq3-r4, the mount process was running (`R`), not
asleep in `hfs_transport`, and the live `transport.db` was 512 KiB (256 KiB
in r4), so the guest kept exchanging requests. The runtime stayed at
`AWAITING_GUEST_BIND` and exited 1 at its 120 s deadline. The mount then
ended, with no 9p mount and no oops. **T1-T11 are unrun.**

Rocky was stopped from 21:53:33 to 21:59:09 (about 5.5 minutes) and was back
on SSH at 22:04 on stock 5.14.0-687.66.1.el9_8. The cleanup steps:
- observer line removed (`restored_pre_run: true`, `e08542dc…`);
- guest poweroff in about 15 s;
- host cleanup;
- native FUSE unmount before the window restore;
- window restore with `restoration-summary.json`;
- test kernel removed (23 files);
- fdescfs and fusefs stay loaded.

**One cleanup step is open:** deleting the run-only observer private key
on f3 (`/var/tmp/hfs-validation-xz3/zq3-r5/observer/id_ed25519{,.pub}`). The
agent's safety check refused the scripted `rm` and left it to Paul. The key
can no longer log in anywhere: its public line was removed from Rocky
first. It sits in a root-only 0700 directory.

## Findings

- **R5-1, mount-0 still not completing (open, main finding).** Analysis of
  `p8-owner-live.tar` (live, 512 KiB) and `f3-evidence.tar` follows in a
  task. With z54 every refusal now carries a `BlockedError` class, so the
  cause should be readable directly.
- **R5-2, new-daemon host qualification passed** (step 1a).
- **R5-3, Rocky restarted outside the run** at 20:20 UTC, between r4 and
  r5. The cause is unknown; the r5 facts were taken after it.
