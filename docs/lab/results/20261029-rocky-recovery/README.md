# Rocky recovery observation — 2026-10-29

Bounded pinned root SSH through f3 succeeded once at 20:03:41 UTC. Rocky reports
Rocky Linux 9.8, kernel `5.14.0-687.61.1.el9_8.x86_64`, UID 0, systemd `running`,
sshd `active`, and no failed units. `rocky-packages.txt` contains 1109 sorted RPM
rows with a final newline; SHA-256: `4b689f7e8bb5d66bdabaffe285c49a87a103325266dda6424f8003ee59dcb4e3`.

At 20:04:17 UTC f3 reports Rocky running as PID 6412 with 12 GiB allocated;
`freebsd` and all three native lab guests are stopped. Host swap use and paging
counters are zero. No md attachments, snapshot automounts, or inspection UFS
mounts remain in the observed inventory.

All four `zrepl_20261029_050719_000` source snapshots remain available with their
previous GUIDs. However, ZFS now reports **8192 bytes written since that snapshot
on each source dataset**, previously zero. The existing immutable snapshot
metadata therefore does not establish that current source images are unchanged.
The cause and changed objects were not investigated. Native planning must retain
this blocker until separately resolved.

The current `prepared-lab-preflight.py` package validator also rejects actual RPM
rows containing `.(none)` and `^` (listed in `report.json`). The inventory digest
above describes the captured canonical data; it does not claim current preflight
admission. No runner was executed and no code was changed.

The Rocky restart's cause and operator are unknown. The earlier stalled Rocky
PID was 6435. The previously approved ACPI stop timed out; this observation does
not establish clean-stop ability. No stop/start/reboot, package or service
changes, mounts, snapshot changes, image extraction, workload I/O, or native/9p
tests were performed. No approval file or existing native plan was changed.

`commands.json` records exact read-only commands and time limits. The Rocky
command used a 25-second guest timeout (2-second termination grace) and a
40-second controller timeout; it returned successfully in 2.6 seconds. f3's final
command used a 20-second remote timeout (2-second grace) and a 30-second
controller timeout, returning successfully in 0.452 seconds. Two earlier f3
commands exposed csh parsing errors; final source was supplied directly to
`/bin/sh -s`. Their diagnostic outputs remain under the local directory recorded
in `report.json`; no repair or Rocky retry followed.

These files contain recovery observations, not a HomelabFS test result. No code
changed and no build or test suite was run for this evidence-only update.

The retained raw guest stdout has SHA-256
`d814a0caf014ca1cea222c4088959fdf6b3f6d611e5b66295ed4fcca4c12dd89`.
Its bytes contain 1120 newline characters and no literal backslash-n sequences.
`rocky-readiness.txt` is the exact prefix before `RPM_MANIFEST_BEGIN\n`;
`rocky-packages.txt` is the exact content between that marker and
`RPM_MANIFEST_END\n`. No bytes were normalized during extraction. The remote
command already sorted the manifest. Concatenating those files and markers
reconstructs the raw stdout exactly.

Task `4x3` records the observed SSH recovery. Verifying a successful graceful
Rocky stop and restoration remains part of task `yq3`'s separately approved
native run; this report does not complete that maintenance check.
