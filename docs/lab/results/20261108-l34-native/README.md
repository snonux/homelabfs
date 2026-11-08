# Native controlledruntime package run on FreeBSD (task l34, 2026-11-08)

Native run of the whole cross-built `controlledruntime` test package in
disposable guest **hfs-test0** (FreeBSD 15.1-RELEASE), with fdescfs `-o nodup`
on `/dev/fd` and the repository source tree staged as the test binary's working
directory by [`tools/lab/native-test-drive.py`](../../../../tools/lab/native-test-drive.py)
`--source-archive`/`--cwd`. This records test portability only; it does not
qualify the real daemon, READY or the 9p base gate (zq3).

[Artifact hashes](artifacts.sha256) · [receipts](receipts/)

## Access and window

The laptop had moved to another network, so f3 was reached through
`paul@f0.wg0` as a ProxyJump to `f3.lan`, keeping the pinned f3 host key
(`HostKeyAlias f3`). The earlier interrupted fdescfs setup was checked first:
nothing had run, so nothing was replayed. Then Rocky was stopped (ACPI) and the
lab started. In hfs-test0 only, fdescfs was loaded and mounted with intent
records at 15:18:20Z, then normally unmounted and unloaded at 15:26:55Z. The
`/dev/fd` devfs identity matched its pre-setup record. Next came `stop-lab` and
`start-rocky`; Rocky answered pinned SSH. See the
[pause/resume record](../20261108-wifi-pause/handoff.json).

## Results

| Run | Source | Pass | Fail | Notes |
|---|---|---:|---:|---|
| [earlier guest0-full](../20261108-native-fd-qualification/receipts/guest0-full/) | 62e0e2b, no staging | 45 | 33 | relocated cwd plus `#!/usr/bin/python3` fixture |
| [full](receipts/full/) | d94198e, staged | 71 | 10 | lib9p fixture compile: POSIX macros hide `u_int` on FreeBSD |
| [full2](receipts/full2/) | b92812a, staged | 71 | 10 | fixture compiles; exporter asserts `!_recovery_socket()` |

All producer and observer tests now pass. The observer runs its pinned
primitive as `/dev/fd/3`, so its production FD-exec path has now executed
successfully on native FreeBSD for the first time.

The 10 remaining failures (`TestRuntimeDriverSharedSequenceRealC` and
`TestBarriered*`) are a **product defect**, tracked as task **m34**. The s24
diagnostics captured the exporter's stderr:
`Assertion failed: (!_recovery_socket())`. A C probe in the guest showed
that on FreeBSD, `getsockname`/`getpeername` on an unnamed `socketpair` return
16 bytes (the `sun_noname` placeholder, all-zero `sun_path`), while Linux
returns 2. lib9p's CONTROL and RECOVERY socket admission (`hfs9p_supervisor.c`
`_socket`, `hfs9p_recovery.inc` `_recovery_socket`) requires at most
`offsetof(sun_path)` bytes, so it rejects legitimate socketpairs on FreeBSD.
That fix changes pinned lib9p inputs, so it is a separate reviewed task.

Guard peaks: full 307 MiB RSS / 51 s; full2 363 MiB RSS / 91 s, within the
guest profile's 512 MiB and 540 s bounds. Preserved resources:
`/var/tmp/hfs-20261108-l34-full{,2}` and `/var/tmp/hfs-20261108-l34-fdescfs`
on hfs-test0.
