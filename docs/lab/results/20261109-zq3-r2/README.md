# zq3-r2: launch, admission and signing reached; first 9p mount fails with EPROTO (2026-11-09)

Run sheet: [filled plan](../20261109-zq3-r2-filled-plan/run-sheet.md), with
[pins](../20261109-zq3-r2-filled-plan/pins.json). Times are EET (UTC+2).
Private evidence stays in the fill directory; only hashes and outcomes are
recorded here.

## Outcome

This is the first time the full bootstrap ran against the real hosts, after
three in-run tool fixes (o54, p54, q54, each reviewed, merged and restaged).
Results of attempt 3:

- The test kernel booted on Rocky's disk under the daemon-launched bhyve.
- Guest admission passed.
- Both operator signatures were made by the hash-checking watcher (exit 0,
  about 100 s left each).
- Provision and bind succeeded.

The first guest 9p mount (`mount -t 9p -o
trans=virtio,version=9P2000.L.hfs1.4,cache=mmap,msize=65536 hfs0 …/m0`)
then failed with `mount(2) system call failed: Protocol error` (EPROTO). The
guest kernel logged nothing. The runtime never left `AWAITING_GUEST_BIND`
and exited 1 at its 120 s bootstrap deadline (`context deadline exceeded`).
**T1-T11 are unrun.**

Rocky was down from 14:41:50 to 14:55:56 (about 15 minutes), then returned
to stock. Cleanup, all verified:

- The guest's observer key was removed (`restored_pre_run: true`) and the
  f3 key deleted.
- `host.py cleanup` destroyed the tap and the VM context, and the original
  UEFI vars are unchanged.
- The leftover native FUSE mount was unmounted normally.
- The window unmounted `/dev/fd`.
- Rocky's stock kernel was checked, and the test kernel removed (23 files).
- fdescfs and fusefs stay loaded on f3, as disclosed.

| Phase | Result |
|---|---|
| 0-3 | passed. Kernel installed unselected; pod manifest `f7f69339…` (82 entries), identical to zq3-r1 |
| 4 | passed after o54: negative test, window setup (FSID `5900ff01:00000059`), native qualification PASS, descriptor exec supported, fusefs loaded, observer key enrolled |
| 5 | rechecks passed (same f3 boot, same Rocky PID/lstart, owned mount); one-shot select and clean stop |
| 6 | passed: 15,457,831,824 bytes reclaimable against the 14,763,950,080 bar (after the UMA drain below) |
| 7 | passed: tap100, vars copy, volume `a6e4d458-…` initialised, K04 baseline |
| 8 | attempt 1 refused by the guard (R2-2), attempt 2 runtime exit 1 (R2-3), attempt 3 reached bind, then mount-0 EPROTO (R2-4) |
| 11-12 | completed (see above); window restore refused only because the native FUSE mount was still present, see R2-6 |

## Findings

- **R2-1, native guard free-page floor (fixed by o54).** The host profile
  needed 3.5 GiB of free pages. With Rocky's guest memory counted as Active,
  f3 had 2.87 GB free and 2.6 GB inactive. The guard now keeps a hard 1 GiB
  free floor plus 3.5 GiB free+inactive.
  - A temporary `vfs.zfs.arc_max` cap did not raise free pages, because the
    freed ARC stayed in UMA caches; it was reverted at once.
  - Before the stop, `sysctl debug.uma_reclaim=2` returned 2.2 GB of cached
    UMA items, which the pagedaemon would otherwise reclaim only under
    pressure. Without that drain, Phase 6 would likely have failed.
- **R2-2, runtime guard refused itself (fixed by p54).** Before launch its
  tree is empty, and its own argv and the remote shell carry `--vm-name
  hfs-zq3-r2`, so both counted as `foreign-bhyve`. The guard's own lineage
  is now excluded.
- **R2-3, missing `--disposable-validation` (fixed by q54).** The runtime
  requires the flag. `bootstrap.py` never passed it, and `runtime-guard.py`
  accepted only `RUNTIME --config CONFIG`. Both now use exactly `RUNTIME
  --config CONFIG --disposable-validation`.
- **R2-4, first real 9p mount returns EPROTO (open, the main finding).**
  - Admission advertised bridge protocol 1.4, normal 3.0 and msize 65536 for
    both domains, matching `version=9P2000.L.hfs1.4`.
  - `bind_v1` was written about 4 ms before the mount.
  - The kernel printed no 9p message, and the exporter diagnostics show no
    exporter output.
  - Whether the guest kernel's bind/version path or the exporter's reply
    produced the EPROTO needs source analysis and, if needed, a probe with
    more logging; tracked as a new task.
- **R2-5, SSH banner timeout (uy3 candidate).** At about 12:54:1x UTC, one
  ProxyCommand SSH to the test guest (10.20.0.123 through f3) failed with
  "Connection timed out during banner exchange". The guest had been up about
  1.5 minutes. The calls just before and after succeeded, and nothing ran, so
  the single retry was safe.
- **R2-6, run-sheet cleanup order.** Phase 11's window restore must run
  after the native FUSE unmount. A runtime that exits at its deadline leaves
  its `fusefs.homelabfs` mount, and the restore then refuses its final
  comparison (the `/dev/fd` unmount itself succeeded). Checked by hand: the
  facts equal the post-load baseline except for the declared `fusefs.ko`.

## Re-pinned during the run

| Item | Fill | Used |
|---|---|---|
| native guard (f3 `/var/tmp/hfs-20261109-zq3r2-fdq`) | from `ea55eb5` | `576d802a…` (o54) |
| runtime guard (`$R/bin/runtime-guard.py`) | `b223b35d…` | `4442b772…` (p54), then `fece96b3…` (q54) |
| bootstrap plan | `eabd2800…` | `89becfe9…` (p54), then `468fcd31…` (q54); only `guard.script_sha256` differs |
| tools snapshot | `ea55eb5` | `76a0327` (q54 merge; `bootstrap_remote.py` unchanged, `121ee0cc…`) |

Refused attempts are kept as `$R/guard.refused-{1,2}`, the local
`bootstrap-run.refused-{1,2}` and the native stage `checks/unsupported-before`.
