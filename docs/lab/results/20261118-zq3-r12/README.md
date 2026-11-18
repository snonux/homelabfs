# zq3-r12: mount-1 uncertain; the host's guest transport failed and both sessions blocked on VIEW_WAIT (2026-11-18)

Run sheet: [filled plan](../20261118-zq3-r12-filled-plan/run-sheet.md)
(with run-time revision R1).
Candidate 5a4 `150deb29…` ([xz3-20261118-r12](../../../../tools/integration/artifacts/evidence/xz3-20261118-r12/README.md)):
the 1a4 guest kernel (build ID `8c1de1ed…`), the y94 daemon `e0981a0a…` and
the reused exporter `ef8f17de…`. T5 stayed deferred; T7 was back in but
never reached. Times are UTC. Private evidence:
`/home/paul/tmp/claude/hfs-zq3-r12-fill/evidence` (run log `RUN-LOG.md`);
offline analysis in `/home/paul/tmp/claude/r12-analysis/`.

## Outcome

The run stopped in Phase 8, before READY: mount-0 succeeded, mount-1 did
not return before its deadline (`uncertain`), and both guest sessions were
blocked at guest uptime 23.5 s. Phases 9 and 10 never ran, so there is no
984 timing sample and no T1-T11 result. Cleanup and restoration were clean.

**What held:**
- Phase 1a unloaded `fusefs` and `fdescfs` again, and the Phase 4 step 1a
  process observer qualified the new daemon `e0981a0a…`.
- The READY path up to mount-0 worked: admission, signed enrollment and
  bind, mount-0 with the exact g64 tuple (mountinfo 391).
- The blocked m0 unmounted normally in about 1 s (`rc 0`), with no new
  block or worker-stopped line afterwards.
- The guest poweroff took about 11 s (zq3-r11: about 530 s).
- No AVC records, no relabel reboot. Rocky was down from 17:19:37 to
  17:24:06 (about 4.5 minutes).
- The new pod manifest `523485cc…` (zq3-r1..r10: `f7f69339…`) was explained
  before the build: same 82 paths and workload; 14 entries changed content
  and 36 only their origin, all from the EL9.9 glibc
  (`2.34-284.el9_8.4` → `2.34-290.el9_9`) and python3/python3-libs
  (`3.9.25-7.el9_8.6` → `3.9.25-16.el9_9`) updates.

**New failure:**
- **R12-1:** mount-1 `uncertain` ("guest-mount: exit 1 mount did not return
  before its deadline; left running"). The guest log:
  `[23.528534] 9p-hfs: session d508c884-… blocked by uncertain control result on VIEW_WAIT (-71)`
  (hfs1/m1, view phase STOPPED) and the same for `f12c0721-…` (hfs0/m0,
  view phase LIVE) at 23.556 s; both 1a4 worker lines read
  `(event -, ack -, host status -, …)`. `runtime.stderr` and
  `exporter.stderr` were empty; no oops. See the root cause below.
- **Operator slip:** my Phase 12 SSH wait loop ran `bash -c "rocky true"`,
  but the session function is not exported ("command not found"), so it
  waited 10 minutes for nothing. Rocky was up all along (journal from
  19:24:14 local); nothing changed on any host because of it.

| Phase | Result |
|---|---|
| 0-1, 1a | passed (R1 found in Phase 1, below) |
| 2-7 | passed (process observer `e0981a0a…`, Phase 5 nightly gate GO 17:18Z, step 1b UMA drain run, Phase 6 margin 0.50 GB) |
| 8 | **fail** (R12-1: mount-0 ok, mount-1 `uncertain`) |
| 9, T1-T11 | unrun (T5 deferred) |
| 11-12 | completed (guard-SIGTERM branch) |

Phase 11 ran the guard-SIGTERM branch, because the runtime was still
serving (`stopped_at mount-1`): guard gone at the first check, receipt
`runtime_exit 0` with only the `guard-interrupted SIGTERM` breach (119.2 s),
exporter alive at guard exit; evidence tarballs; m0 unmounted; observer
removed (`authorized_keys` back to `e08542dc…`); `next_entry` consumed; no
AVC; poweroff; host cleanup (tap and vmm destroyed, UEFI vars original
unchanged); native FUSE unmount (`rc 0`); plain literal observer key
deletion on f3; window restore `mount_restored: true` (`fusefs.ko` kept
loaded by the reviewed plan). Phase 12: Rocky PID 53482 (UTC lstart `Wed
Nov 18 17:24:06 2026`), kernel `741.22.1.el9_9`, `Enforcing`, one journal
boot, no relabel; U2 removed the test kernel, its modules and the
installed admission record. Mount-1 was not retried: a failed mount needs a
new guest boot.

## Root cause (R12-1)

Decoded offline from the live, after and f3 owner snapshots
(`/home/paul/tmp/claude/r12-analysis/dumps/`).

The run log's first reading, a 1a4 guest regression in the VIEW_WAIT reply
handling, is **wrong**. 1a4's capture only runs when a reply arrives, and
`host status -` means there was none. The chain:

1. **The host runtime's guest transport marked itself failed** at about
   guest 23.5 s. After that `Transport.Read` and `Transport.Write` return
   Unavailable (`internal/guestbridge/endpoint.go` around 320 and 713).
2. The exporter turns any fetch error into an ERROR packet
   (`tools/integration/lib9p/src/hfs9p.c` around 3028), and the guest maps
   any non-data fetch reply to `-EPROTO` (`-71`; patch 0003 around 1046).
   Both sessions' outstanding VIEW_WAITs failed this way, 28 ms apart.
3. Snapshot evidence: the transport database stops at native cell 169
   (m1's ninth VIEW_WAIT poll; base response recorded, never answered).
   Responses 156 (m1's STOPPED ACK, accepted) and 166 (an m0 poll, EMPTY)
   were never read by the exporter. The exporter's last state shows m1's
   next mount-time VIEW_QUERY (guest cell 14, native 170) refused and held
   for retry.
4. **Mount-1 hung for 30 s** instead of failing at once because
   `hfs_transport`'s retry loops ignore `session->blocked`.

**Trigger: unknown.** Most likely the save of native write 170
(`commitWrite`, `endpoint.go` around 384, which releases cell 168) or
publishing 169 (`worker.go` `publishResponse` around 180). The reason is
never logged (`runtime.stderr` empty).

Also unexplained: after the accepted STOPPED ACK, m1's recall stayed in
INSTALL_DRAIN with EMPTY polls for about 600 ms.

The guest bridge, controlled runtime, FUSE and exporter code is identical
between zq3-r11 (`dfdcdd9`) and zq3-r12 (`1b0a306`), so the failing host
path already existed in zq3-r11; this run is the first to have a recall in
flight while m1 was still mounting (qc4).

Confidence: high that it is not 1a4 and that one host cause failed both
sessions; medium-high that the transport marked itself failed; low on the
trigger.

## Run-time revision R1

Phase 1 found that Rocky's time zone is `Europe/Sofia (EET, +0200)`, not
UTC as the fill assumed: the fill read `date -u` output. Read-only checks
showed the nightly mechanism: `unattended-upgrade-rocky.timer` fires hourly
at :55 local, but `/usr/local/sbin/unattended-upgrade-rocky daily` runs
`dnf upgrade` once per local day (stamp
`/var/lib/unattended-upgrade/last-daily`, `2026-11-18`), i.e. at 00:55 EET =
**22:55 UTC**, and it never reboots Rocky. N1, the Phase 1 expectation and
the Phase 5 step 0 gate were revised to compute in `Europe/Sofia` (latest
Phase 5 start 21:25 UTC). The gate was tested at fixed times before use and
printed `GO 19:18 EET 17:18Z` for the real step 0. The committed
[run sheet](../20261118-zq3-r12-filled-plan/run-sheet.md) now carries R1;
the [run plan](../../zq3-run-plan.md) Phase 1 now asks future fills to
record `timedatectl` and the upgrade timer and stamp.

## Host events and facts

- f3 boot `1794574308` unchanged (no f3 reboot since 2026-11-13).
- Rocky before: PID 3146 (UTC lstart `Wed Nov 18 09:01:00 2026`), boot
  `c8f80bb2-…`, stock `741.22.1.el9_9`, pod runtime unchanged
  (`podman-6:5.8.5-3.el9_9`, `crun-1.29.1-1.el9`). Stopped by ACPI at
  17:19:37, started normally at 17:24:06 (vm-bhyve run 1, boot
  `6fe8702d-…`, PID 53482).
- Phase 5 step 1a recommended the UMA drain (margin 0.45 GB); step 1b freed
  about 0.12 GB (`uma_free_bytes` 159.2 MB → 45.0 MB). Last Rocky zrepl
  snapshot before the stop: `zrepl_20261118_171220_000`.
- Guard peaks: disk 23.5 MB, exporter RSS 278 MB, runtime RSS 36 MB.
- Final f3 state: Rocky running, `/dev/vmm` holds only `rocky`, `tap100`
  gone, no fuse/fdesc mount, `fdescfs.ko` and `fusefs.ko` loaded, zroot
  ONLINE. Rocky has no `hfs-xz3` kernel and no `/etc/homelabfs` admission
  record.

## Follow-up tasks

- **nc4** (primary; guestbridge): find why the transport failed (log the
  reason), add a replay test from the r12 snapshots, fix.
- **oc4** (guest 0003): stop the transport retry loops once the session is
  blocked; map errors precisely; log the raw ERROR code.
- **pc4** (exporter): a distinct ERROR code for a host transport failure.
- **qc4** (tests): a two-mount recall-during-mount workload test; explain
  the INSTALL_DRAIN stall.
- Still open from zq3-r11: z94, 0a4, 2a4.
