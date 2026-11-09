# zq3-r3: HELLO fixed; superblock setup fails and its cleanup oopses (2026-11-09)

Run sheet: [filled plan](../20261109-zq3-r3-filled-plan/run-sheet.md). Candidate
`5eb42c50…` (r54 guest kernel, task t54). Times are UTC. Private evidence stays
in `/home/paul/tmp/claude/hfs-zq3-r3-fill/evidence`.

## Outcome

The r54 fix worked. The guest mount got past the HELLO exchange that failed
with EPROTO in zq3-r2 and reached `mount_nodev`. `hfs_fill_super` then
failed. Its error path, `deactivate_locked_super` → `v9fs_hfs_kill_super` →
`hfs_controlled_begin_close` → `hfs_completion_reserve`, dereferenced a NULL
pointer (address `0xbd`, `RBX` 0, `hfs_completion_reserve+0x41`). The guest
kernel oopsed, and the `mount` task died with IRQs disabled (taint 128). The
runtime never left `AWAITING_GUEST_BIND` and exited 1 at its 120 s deadline.
**T1-T11 are unrun.**

Rocky was down from 15:53:49 to about 16:42, roughly 50 minutes. Most of that
was a workstation network outage (R3-2), plus a slow guest shutdown (R3-4).
Everything was then restored:
- Rocky's stock kernel was checked and the test kernel removed (23 files).
- `authorized_keys` is back to `e08542dc…` (`restored_pre_run: true`).
- Host cleanup destroyed the tap and the VM context; the original vars are
  unchanged.
- The native FUSE mount was unmounted normally before the window restore.
- The window restore completed and recorded `restoration-summary.json`, with
  only the declared `fusefs.ko` retained. This is the first real run of the
  j44 path.
- fdescfs and fusefs stay loaded on f3.

| Phase | Result |
|---|---|
| 0-1 | passed; facts equal the 14:11Z baseline (Rocky PID 45407, vars `c71240e9…`) |
| D1-a | fusefs and fdescfs (left from r2) unloaded normally, before staging |
| 2-3 | passed; kernel installed unselected; pod manifest `f7f69339…` (82) |
| 4 | passed: window plan `339468aa…`, owned FSID `5900ff03:00000059`, qualify PASS, observer key enrolled |
| 5 | rechecks passed; estimate margin 0.38 GB, so UMA drain (freed about 0.23 GB); select; clean stop |
| 6 | passed: 15,303,398,240 bytes reclaimable (margin 0.54 GB) |
| 7 | passed (tap100, volume `071f5579-…`, K04 baseline) |
| 8 | attempt 1: host-preflight SSH banner timeout (R3-2), nothing ran; attempt 2: through bind (both watcher signatures exit 0), then mount-0 oops (R3-1) |
| 11-12 | completed in the mandated order (see above) |

## Findings

- **R3-1, `hfs_fill_super` failure, and a NULL dereference on its cleanup
  path (open).** Two kernel defects, or one defect and one hidden failure:
  1. Why `hfs_fill_super` failed after an accepted HELLO is unknown. The
     guest log shows only the oops. The ROOT/attach exchange with the runtime
     is the next suspect.
  2. `v9fs_hfs_kill_super` calls `hfs_controlled_begin_close` on a session
     whose completion state (`RBX`, the object read at offset `0xbd`) was
     never set up, because fill_super failed early. The error path must not
     dereference it.

  Full trace: `p8-guest-oops.txt` and `p11-guest-dmesg-full.txt` in the
  private evidence.
- **R3-2, network outage on the workstation path (uy3 evidence).** Between
  about 15:54 and 16:34, the workstation on Wi-Fi 10.30.0.110 reached f3
  over `wg2` with RTTs of 1.3-3.6 s and packet loss, while the WireGuard
  handshake stayed fresh. f3's SSH banner arrived after 9.4 s, which is over
  `ConnectTimeout=5`, so host-preflight failed. The run paused, as Paul
  instructed for an unreachable f3, and resumed once the RTT held under
  100 ms for a minute.
- **R3-3, D1: modules left loaded by the previous run.** The window plan
  expects fdescfs and fusefs absent, but r2 left both loaded. They were
  unloaded normally before staging (run-sheet execution note).
- **R3-4, slow guest shutdown after the oops.** `systemctl poweroff` took
  about 3.5 minutes; sshd stopped first and ping stayed up. Nothing was
  forced. The Rocky boot afterwards took about 5 minutes to reach SSH.
