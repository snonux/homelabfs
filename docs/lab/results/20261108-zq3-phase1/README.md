# zq3 Phase 1 read-only facts (2026-11-08, 21:03 UTC)

Collected over pinned SSH (strict host keys, direct LAN path), read-only, after
Paul approved the zq3 plan in principle. Nothing was staged, started, mounted,
loaded or changed. [f3](f3-facts.txt) · [Rocky](rocky-facts.txt) ·
[f3 doas policy](f3-doas.conf.txt)

## Preconditions (all hold)

| Check | Result |
|---|---|
| Rocky incarnation | PID 80203, start 2026-11-08 16:05:53 UTC, `rocky.conf` `c101adf1…` unchanged |
| Other VMs | freebsd and hfs-test0..2 stopped; `/dev/vmm` holds only `rocky` |
| Memory / swap | physmem 16,879,837,184; free+inactive about 12.6 GB with Rocky running; swap unused |
| Storage | zroot ONLINE, 353 GB free; `/var/tmp` 338 GB available; `/var/tmp/hfs-validation-xz3` absent |
| Modules | `fusefs`, `fdescfs` not loaded; module hashes `847cd1f2…` (fdescfs) and `b207adf6…` (fusefs) match the pins |
| `/dev/fd` | devfs, no fdescfs mount |
| Route to Rocky | direct on `re0`, source `10.20.0.133` (observer key `from=` holds) |
| PoC share (R6) | `/var/tmp/homelabfs-9p-poc/share` present |
| python3.12 on f3 | regular file, `a66eb370…` |
| Rocky kernel | running and default `5.14.0-687.66.1.el9_8`; `next_entry` empty; `saved_entry` the stock entry |
| Rocky SELinux / kdump | Enforcing; kdump enabled and active |
| Rocky tools | grubby, grub2-editenv, grub2-reboot, depmod (symlink to kmod, followed by design), dracut, systemctl, udevadm present and hashed |
| Rocky python | `/usr/bin/python3` -> `python3.9`, `1143733d…` |
| Rocky root keys | one line, `e08542dc…`, `ssh_home_t` label |
| Rocky containers | podman 5.8.2-8, crun 1.27-1, conmon 2.2.1-2 (hashes recorded); audit log readable |
| Rocky `/boot` | 353 MB free (test kernel about 6 MB plus initramfs about 40 MB, kdump initramfs about 40 MB) |

## Findings

- **f3 doas:** `permit nopass :wheel` already gives paul passwordless root on
  f3, so the "temporary doas rule" decision is moot: nothing about doas
  changes for the run.
- **UEFI firmware symlink:** `tools/lab/directlaunch/host.py facts` refused
  with ELOOP ([attempt 1](f3-launch-facts-attempt1.stderr)) because
  `/usr/local/share/uefi-firmware/BHYVE_UEFI.fd` is a package-installed
  symlink (`edk2-bhyve-g202508_3`) to `../edk2-bhyve/BHYVE_UEFI.fd`. Task h44
  pins exactly that link and its resolved target; the launch facts are
  collected after it merges.
- **Pod manifest:** `pod_fixture.py manifest` needs its scripts on Rocky, so
  it moves into Phase 3 staging (inside the approved window).
