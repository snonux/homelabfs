# zq3-r1: first attempt, stopped before launch (2026-11-09)

Run sheet: [filled plan](../20261108-zq3-filled-plan/run-sheet.md), pins
[pins.json](../20261108-zq3-filled-plan/pins.json) (after the p44 refresh).
Times are EET (UTC+2). Private evidence stays in the fill directory; only
hashes and outcomes are recorded here.

## Outcome

Stopped after Phase 5 at the Phase 6 admission. No HomelabFS runtime, guest
or 9p mount was started, so T1-T11 are **unrun**. Every change was undone
through the planned recovery paths (U3, U2, observer key removal), and the
hosts ended in their pre-run state.

| Phase | Result |
|---|---|
| 0-2 | passed (argv/pins re-verified, f3 staging 17 files hash and stat match) |
| 3 | passed: kernel `6.1.190-hfs-xz3` installed unselected (initramfs `ede35b4b…`), observer staged, pod rootfs built (82 entries, manifest `f7f69339…`) |
| 4 | passed after p44: negative test PASS, window setup owned `/dev/fd` (FSID `5900ff02:00000059`), **native qualification PASS on the f3 host** (closes the w24 host rerun), `--check-descriptor-exec` supported, fusefs loaded, run-only observer key enrolled (`authorized_keys` `e08542dc…` to `63822ecf…`) |
| 5 | one-shot select and clean Rocky stop worked, but on a Rocky instance restarted by an external f3 reboot (see R5) |
| 6 | **failed**: memory below the bar, `/dev/fd` no longer mounted |
| U3, U2, cleanup | test kernel booted once (16 s to SSH), reboot-to-stock, `check --expect stock` ok, kernel and modules removed, observer key removed (`restored_pre_run: true`, `e08542dc…`), f3 key deleted, no fdescfs/fusefs mount or module left |

## Findings

- **R1, guard `--` separator (fixed by p44).** `native-test-guard.py` uses
  `argparse.REMAINDER`; under Python 3.12 a leading `--` stays as `argv[0]`,
  so the guard tried to exec `--`. The refused stage directory
  `checks/unsupported-before` is kept; the retry used `unsupported-before-r2`.
- **R2, window parent missing (fixed by p44).** The run sheet never created
  the root-private `/var/tmp/hfs-fdescfs-windows` the controller requires.
- **R3, nosuid invisible on fdescfs (fixed by p44).** FreeBSD's
  `fdesc_statfs` reports only `mnt_flag & MNT_IGNORE`, so the `/dev/fd` row is
  `rw` although the helper set `MNT_NOSUID`; setup refused it. The mount was
  undone by exact FSID and fdescfs unloaded normally; facts then equalled the
  original. The refused directory is kept as `zq3-r1.refused-setup-1`.
- **R4, library pins by symlink path (open).** Step 0 adds python3.12's
  `ldd` libraries to the host hash list. `/usr/local/lib/libintl.so.8` is a
  symlink, and the controller hashes with `O_NOFOLLOW`, so validation refused
  (before any change). Pinning the real path `libintl.so.8.4.6` worked. The
  sheet should say "pin `realpath` of each library".
- **R5, external f3 reboot during the pause (open).** f3 shut down at 09:19
  and booted at 10:30 while the run waited for operator decisions; the run had
  sent nothing then. Cause (f3 `/var/log/auth.log`, `messages`): at 09:19:30
  user `f3sctl` from 10.20.0.125 ran `/usr/local/bin/f3sctl agent-root
  poweroff` via doas, and `shutdown` logged `power-down by f3sctl` at 09:19:37:
  a deliberate power-off through the homelab control tool, not a crash. Rocky
  auto-started (new process), the
  window mount and modules vanished. Phase 5 has no recheck of the f3 boot
  time, the Rocky PID/lstart pin or the owned `/dev/fd` mount, so select and
  stop ran on the new Rocky before Phase 6 noticed. Phase 5 must start with
  those rechecks (`sysctl kern.boottime`, `ps -p PID -o lstart=`,
  `inspect == owned.json`).
- **R6, Phase 6 memory bar not reachable on a fresh boot (open).** With all
  VMs stopped: free 3,708,899 + inactive 28,967 pages and ARC 693 MB give
  16,003,514,952 bytes; the bar is 15 GiB (16,106,127,360). `hw.physmem` is
  16,879,837,184, so the bar leaves under 0.8 GiB for the host kernel and
  base system. The 6 GiB guest + 6 GiB runtime envelope + 3 GiB reserve split
  needs a plan revision before zq3-r2.
- **R7, GRUB boot counter.** After the U3 reboots Rocky shows
  `boot_indeterminate=2` (was 0); it is GRUB's boot-success counter and
  resets on the next recorded successful boot.

## Exercised for the first time on the real hosts

- Kernel lifecycle on Rocky end to end: install unselected, one-shot select,
  clean stop, boot once on the test kernel, `reboot-to-stock`, `remove`.
- fdescfs window setup, qualify and the w24 native qualification on f3.
- Observer key stage, enrol and remove with exact `authorized_keys` hashes.

## Left in place

f3: staging under `/var/tmp/hfs-validation-xz3/zq3-r1` and
`/var/tmp/hfs-20261108-zq3r1-fdq`, window directories
`/var/tmp/hfs-fdescfs-windows/zq3-r1{,.refused-setup-1}` (no mounts).
Rocky: `/root/hfs-zq3-zq3-r1-*`, `/var/tmp/hfs-zq3-zq3-r1` (pod rootfs,
observer receipts), kernel state journal.
