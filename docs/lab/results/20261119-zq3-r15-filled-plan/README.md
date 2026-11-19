# zq3-r15 filled run inputs (2026-11-19)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r14 fill](../20261119-zq3-r14-filled-plan/README.md) ·
[zq3-r14 record](../20261119-zq3-r14/README.md) ·
[r15 candidate](../../../../tools/integration/artifacts/evidence/xz3-20261119-r15/README.md)

These are the exact run inputs for `zq3-r15`, the fifteenth attempt of zq3.
They were generated on the workstation with the committed tools at `main`
`312abdb` (after z94 and be4). They mirror the
[zq3-r14 fill](../20261119-zq3-r14-filled-plan/README.md), which ran for
real: Phases 0 to 9 passed (Phase 1a, Phase 4 step 1a, both mounts, READY,
`usable` L = 2, Phase 9), T1 failed (R14-1: the m0 root OPEN waited about
14 s behind three recalls and outlasted guest.py's 10 s alarm; the
controller recorded `dispatching-uncertain`), and Phase 11 took the
guard-SIGTERM branch, where both sessions blocked `(-112), exporter ERROR 5`
after the host stop and both unmounts returned `rc 0` in 1 s (R13-1 fixed).
The changes are:
- the run ID (`zq3-r15`) and the native guard root
  `/var/tmp/hfs-20261119-zq3r15-fdq`;
- the r15 candidate `ac8020aa…` with the **new z94 daemon `8668e5f0…`** (a
  read-only OPEN conflicts like a read; the volume root's lifetime journals
  no pins) and new window artifacts (`9d9f0f01…`, `d6f2e4d0…`). The oc4
  kernel `9b167690…` and the pc4 exporter `2fd7e32f…` are reused byte for
  byte. **Phase 4 step 1a is required for the new daemon bytes**;
- `hfs-runtime-prep` and the signer rebuilt at `312abdb` (both link the
  z94 packages);
- **be4:** `guest.py`, `controller.py`, `guest-lock-probe.py`,
  `pod_workload.py` and `pod_fixture.py` re-pinned; request `seconds` 240
  and plan `seconds` 255 (zq3-r14's 60/120 are now refused); lock-probe
  budgets 150/240; the guard budgets in `sh/guard-budget.py` and the
  Phase 10 table recomputed from the run plan's be4 table (3600 s guard
  kept; the worst cases sum to 5025 s, so a slow run STOPs early by
  design); the tool-timeout rule raised (`$CTL` and the T6/T8 lock-probe
  lines at least 300 s, T7's `pod_fixture.py run` at least 420 s);
- the T7 pod rootfs manifest **will differ from `523485cc…`** because of
  the new `pod_workload.py`; the fill predicts `34a71569…`;
- the zq3-r14 lessons: R14-1 (z94, be4, admission timings and recalls
  recorded as findings) and the child-wait slip (Phase 11 step 6 is used
  exactly as written, pattern `[h]fs-zq3-r15`);
- **Phase 1a (D1-a) runs again:** zq3-r14 left `fdescfs.ko` and
  `fusefs.ko` loaded.

Nothing here used SSH, the network, the lab, a VM, a mount, an installation
or the operator private key. **No authority follows from these files:**
every class A step in the [run sheet](run-sheet.md) still needs Paul's
explicit final GO, including the Phase 1a unload and T7 (A8). **zq3
acceptance stays incomplete by design**, because T5 does not run.

## N1: Rocky upgrades itself once a day

Rocky's clock is `Europe/Sofia (EET, +0200)`. The daily mode of
`unattended-upgrade-rocky.timer` runs `dnf upgrade` once per local day, at
00:55 EET = **22:55 UTC**, stamps `/var/lib/unattended-upgrade/last-daily`
and never reboots Rocky. The 2026-11-19 16:01:29Z facts
(`rocky-upgrade-…txt`): stamp `2026-11-19` (written 00:55:04 EET), newest
dnf transaction still #64 of 2026-11-18 00:55; the hourly ticks since
zq3-r14's Phase 12 start only logged the deferred reboot. **The run must
start after that day's run has stamped and finish before the next one,
22:55 UTC on the run day (2026-11-19 22:55 UTC for a run that day); the
latest Phase 5 start is 21:25 UTC.** Phase 1 runs `n1-gate.py gate` early
and Phase 5 step 0 runs it again before Rocky is stopped. The gate
computes the dates from its own clock, so a run on a later day needs no
edit; the in-run cutoff (no new case after 22:15 UTC, guest poweroff before
22:45 UTC, the 22:35 UTC fallback) applies on the run day. Any new dnf
transaction lets the pod-runtime and kernel rechecks decide; drift means a
refill. The facts match zq3-r14's on every Rocky pin (kernel
`741.22.1.el9_9`, python3.9 `729ac37f…`, podman, crun, conmon,
seccomp.json, `authorized_keys`).

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-zq3-r15-fill` (`0700`). This directory holds only
public hashes, paths, argv and the read-only facts. These stay private and
are never committed:
- the per-session keys;
- the runtime config, which holds host receipt private keys;
- the staging tarballs that contain it.

## Inputs

| Input | Value |
|---|---|
| Candidate | **r15 daemon rebuild** (task z94): manifest `ac8020aa…` (194 files), package `/home/paul/tmp/claude/hfs-xz3-20261119-r15/package`, source checkout and repository baseline `2f34b60` (no `repository-changes.patch`). **New:** the CGO daemon `8668e5f0…` (GNU build ID `24a8935e…`) and the window artifacts `9d9f0f01…` (test) and `d6f2e4d0…` (helper), both from `2f34b60`. **Reused byte for byte from xd4 `3a7131dd…`:** the oc4 kernel `6.1.190-hfs-xz3` (bzImage `e0daa57e…`, build ID `9b167690…`, System.map `dd88dfcb…`), config `28fe4a8e…`, the five modules and `modules.*`, and the pc4 exporter `2fd7e32f…`. Init helper `8c02b98f…` unchanged. **Lab tools (be4 and z94):** `controller.py` `628ce17a…` and `rocky_kernel.py` `1bfc35a9…` (admit only `ac8020aa…`), `guest.py` `48d6ae81…`, `guest-lock-probe.py` `fd439897…`, `pod_fixture.py` `07314543…` (pins `pod_workload.py` `c714370f…`); unchanged: `argv.py` `78462c2e…`, `bootstrap.py` `5e467fe6…`, `bootstrap_remote.py` `0dcb0fe0…`, `correlate.py` `9cd55f2c…`, `fill_plan.py` `893350cf…`, `runtime-guard.py` `fece96b3…`, native guard `576d802a…` (`git diff c0e8ea2 312abdb -- tools/lab docs/lab/zq3-fdescfs-window internal/runtimeprep/testdata`) |
| Phase 1 facts | [facts/](facts/), collected read-only 2026-11-19 16:01:29Z; they supersede zq3-r14's 11:31:26Z set. **f3:** boot `1794574308` (unchanged since zq3-r8), **`fdescfs.ko` (id 31) and `fusefs.ko` (id 32) loaded**, no fuse/fdesc mount, free 7.68 GB + inactive 1.40 GB, ARC 6.03 GB, `/usr/bin/ssh` `6a9e5573…`, python3.12 `a66eb370…`, `10.20.0.133` on `vm-public` (members `tap0`, `re0`). **Rocky:** PID `84753` (UTC lstart `Thu Nov 19 12:41:44 2026`, zq3-r14's Phase 12 `start-rocky`), boot ID `b4d9ee31-…`, running stock `5.14.0-741.22.1.el9_9` = grub default, `next_entry` empty, `boot_indeterminate=7`, SELinux Enforcing, `/.autorelabel` absent, no `hfs-xz3` file in `/boot` (zq3-r14's U2), `/boot` 328 MB free, python3.9 `729ac37f…`, `authorized_keys` `e08542dc…`. **Clock and upgrade:** `Europe/Sofia (EET, +0200)`, synchronized; stamp `2026-11-19` (00:55:04 EET); newest dnf transaction #64 (2026-11-18 00:55); next run 22:55 UTC. **Pod runtime** (byte-identical to zq3-r14's): podman `249e918c…` `podman-6:5.8.5-3.el9_9.x86_64`, crun `659867c7…` `crun-1.29.1-1.el9.x86_64`, conmon `1ac2a8db…`, seccomp.json `7dd44119…`. The published files hold no key material (grep for key headers and `ssh-*` lines) |
| Host events since zq3-r14's fill | zq3-r14's own Rocky stop (12:36:30Z, ACPI) and normal start (12:41:44 UTC, vm-bhyve run 1, no relabel reboot, U2 done); Rocky's upgrade timer ticked hourly and changed nothing; the r15 build ran on the workstation only. No f3 reboot since Paul's deliberate 2026-11-13 reboot |
| Launch facts | `f3-launch-facts-20261119T160129Z.json` (`3b27d6aa…`): `rocky.conf` `c101adf1…`, vm-run `43753bc8…`, firmware `6b48e967…` unchanged; **UEFI vars `ff1ec37b…`** (zq3-r14: `379f79e7…`); launch logged `2026-11-19T14:41:44+02:00` |
| Tap | `tap100` (bridge members `re0`, `tap0`) |
| Run ID / staging | `zq3-r15`, `/var/tmp/hfs-validation-xz3/zq3-r15` (the parent exists and holds zq3-r1 to zq3-r14) |
| Native guard root | `/var/tmp/hfs-20261119-zq3r15-fdq`, host profile `--rocky-pid 84753 --rocky-birth 'Thu Nov 19 12:41:44 2026'`, guard `576d802a…` (unchanged) |
| Window directory | `/var/tmp/hfs-fdescfs-windows/zq3-r15` (the parent exists, `root:wheel 0700`) |
| Rocky names | `/root/hfs-zq3-zq3-r15-*`, `/var/tmp/hfs-zq3-zq3-r15` (the zq3-r1 to zq3-r14 ones stay as evidence) |
| Operator public key | `2f6d909c…`; key path `…/hfs-zq3-r15-fill/operator/operator-signing-key` (empty `0700` directory, never read here) |

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | **Rebuilt** at `312abdb`: `hfs-runtime-prep` and the signer `hfs-retained9p-operator`, with `-trimpath`, CGO, VCS stamps and an unmodified private clone (`vcs.modified=false`); a second build of each, forced with `-a`, gave the same bytes. Both link `internal/viewcontrol` and `internal/fsservice`, which z94 changed, so zq3-r14's prep `25ab9c4f…` and signer `5e284fda…` are retired. G3 init helper: zq3-r1's reproduced build | prep `9f5b87c6…`, signer `14d0a362…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from `testdata/zq3-spec.json` at `312abdb` (`make-spec.py`: only `zq3-r1` → `zq3-r15` and the r15 operator key path change; the result equals zq3-r14's spec with the run ID renamed). The committed spec is unchanged since zq3-r14 | spec `cac8dd5f…`, request template `fb7a14c0…`, runtime config `b34a204e…`, volume-init `ada9146d…`; volume `e4a01095-…`; sessions hfs0 `d34e2e0c-…`, hfs1 `df8e1015-…` |
| 1. Argv | `argv.py generate` (unchanged, exporter pin `2fd7e32f…`) from the 16:01:29Z launch facts (sessions, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `03bba97c…`, args `d79441a4…`, vars `ff1ec37b…`, exporter `2fd7e32f…` |
| 2. Kernel plan | `rocky_kernel.py plan` at main (`1bfc35a9…`; admits only `ac8020aa…`), stock default `/boot/vmlinuz-5.14.0-741.22.1.el9_9.x86_64`. Against zq3-r14's it differs only in `run_id` and `manifest_sha256`: every file entry (the oc4 kernel, config, modules) is the same. The xd4 package `3a7131dd…` is refused as superseded ("use the r15 rebuild") | plan `b01e8495…`, build ID `9b167690…` |
| 3. Controller batch | Template derived from zq3-r14's by `derive-templates.py`: run ID, fill path, sessions, candidate `3a7131dd…` → `ac8020aa…`, probe `guest.py` `d8361fc0…` → `48d6ae81…` (be4), **request `seconds` 60 → 240 and plan `seconds` 120 → 255** (be4). **T5 stays deferred** with the r9 reason. `check` refuses the template as designed (`probe/installed-manifest pin`). A synthetic copy with zero IDs **passes** and prints `cases_planned` (four) and the deferral. The same copy is **refused** with the xd4 candidate (superseded, "use the r15 rebuild"), with zq3-r14's seconds 120/60 and with plan 120 alone (`per-dispatch deadline cap 255..600 seconds`), with plan 255 but requests 60 (`case differs from exact batch`), without the declaration and with `credentials` also deferred | template `b556e581…` |
| 4. Bootstrap plan | Template derived from zq3-r14's (the one that ran through READY and T1). New pins: bundle, kernel plan, `rocky_kernel.py`, the rebuilt prep `9f5b87c6…` and **the z94 daemon `8668e5f0…`** (`runtime.sha256`). Unchanged: `bootstrap_remote.py` `0dcb0fe0…` (`remote_sha256`) and the guard pin `fece96b3…`. The script re-hashes every pinned file at main and in the package. The unfilled template refuses as designed (`observer.ssh_sha256: SHA256 hex`). **F1 filled now** with `fill_plan.py bootstrap` from the 16:01Z `sha256 -r /usr/bin/ssh` line (`6a9e5573…`), then `bootstrap.py check` (journal `/3`) **pass** | template `f55d743a…`, filled plan `7522f6e4…` |
| 5. Guard argv | Derived from zq3-r14's, with `--runtime-sha256 8668e5f0…` (checked against the package) and `--disposable-validation` exactly once, last. It equals `bootstrap.py`'s own `SshRemote.guard_argv()` for the filled plan, in shell string and argv, and passes `runtime-guard.py`'s `parse_args` and `check_command` | `d1b5964d…`, in [pins](pins.json) |
| 6. Pod template (e94, be4) | zq3-r14's `homelabfs-retained9p-pod/2` template with the run ID renamed and `fixture_sha256` `01f8e842…` → `07314543…` (be4). `derive-templates.py` compares its `runtime_nevra` and the four `runtime_sha256` values with the facts' `== pod-runtime` section (byte-identical to zq3-r14's), cross-checks the hashes with the Phase 1 `sha256sum` lines, and runs the committed `pod_fixture.py`'s own `check_nevra_pins` and `parse_request` (synthetic fill): **both accept**. `parse_request` refuses a `pod/1` request without `runtime_nevra`, version `/1` with `runtime_nevra`, swapped NEVRAs, a `podman-docker` sibling, an extra conmon NEVRA, **the zq3-r14 fixture pin** and the r11 one. **Predicted rootfs manifest:** zq3-r14's real Phase 3 manifest (recomputed: `523485cc…`) with only the `/hfs/pod_workload.py` entry and `workload_sha256` replaced gives `34a71569…` (82 entries; a prediction, not a pin) | pod template `a1a393b3…` |
| 7. Lock-probe templates (be4) | zq3-r14's with the run ID renamed and `budgets` case 3 → **150**, overall 30 → **240**; the filled dry-run requests pass the committed `guest-lock-probe.py`'s `validate_request`, and case 151 is refused | m0 `9336f19a…`, m1-T8 `578cafab…` |
| 8. Observer and staging pins | Unchanged script pins: `observer_key.py` `cd7f38c2…`, `observer.py` `2e1449dd…`, `installed.py` `c824b183…`, native guard `576d802a…`, `runtime-guard.py` `fece96b3…`, `fdescfs-window.py` `b00b19d1…`, `host.py` `738e1ca5…`, `host_snapshot.py` `b29ffed2…`, `argv.py` `78462c2e…`. Re-pinned to main: `rocky_kernel.py` `1bfc35a9…`, `guest-lock-probe.py` `fd439897…`, `pod_fixture.py` `07314543…`, `pod_workload.py` `c714370f…`. New staged bytes: the z94 daemon and the r15 window artifacts. All 17 f3 and 18 Rocky files went into two deterministic tarballs; a second tar run gave the same hashes. A verification extract matched every expected hash (35), the f3 tarball holds only `0/0` entries with modes `0700`/`0500`/`0400` and no parent entry; the extract was then deleted | f3 tar `2b0cd3ac…`, Rocky tar `dc028b27…` |
| 9. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` at main (`6aef7d3a…`, names `8668e5f0…`, `d6f2e4d0…`, `9d9f0f01…`) with `zq3-r1` → `zq3-r15`. Synthetic fills, which are not pins: zq3-r8's real Phase 4 facts and host hashes (modules absent) with the paths renamed and the three r15 artifact hashes **pass**; the same facts with the xc4 hashes zq3-r14 staged **refuse** (`pinned artifact differs or missing`); post-restore facts with both modules loaded, the state on f3 now, **refuse** (`fdescfs module present`): hence Phase 1a | r15 pins `59d634d9…` |
| 10. Signing watcher | zq3-r14's `bin/sign_watch.py` (it signed both requests in zq3-r12 to zq3-r14) with `D`, the docstring run ID, the signer comment and **the signer pin `14d0a362…`** changed. Tested locally with a fake signer: a wrong request hash, a wrong command, an unknown action, a repeat and a wrong signer hash were refused; both exact requests were signed | `978c4e1a…` |
| 11. Process observer check | `sh/p4-observer.py` with the run ID and **the daemon pin `8668e5f0…`**. zq3-r14's real records (renamed) are now **refused** for the `sha256` only, and pass with the z94 hash substituted. The `$NG` line (Rocky PID `84753`) parses and passes the native guard's own `parse_args` and `check_request` | `e346900f…` |
| 12. N1 gate helper | `sh/n1-gate.py` from zq3-r14 (docstring only). Retested at fixed times: 13:30Z GO (also with the real 16:01Z upgrade record), 21:20Z and 21:25Z GO, 21:30Z, 22:56Z and 23:10Z STOP, the next day with the old Phase 1 STOP, with a fresh Phase 1 and stamp GO, a stale stamp STOP; `phase1` reports `dnf_unchanged` true for the facts and false for a synthetic #65 | `36cc1a5b…` |
| 13. Fence record helper | `sh/p11-fence.py` from zq3-r14 (docstring only). Retested: the real nc4 row and stderr line (found), zq3-r14's real Phase 11 evidence (`fenced` false) and a synthetic calls record (found) | `26df88fb…` |
| 14. Guard budget helper (be4) | `sh/guard-budget.py` with the **be4 worst-case step seconds**: phase9 180, T1-T4 660 each, T6 300, T7 615, T9 (with T10) 990, T10 390, T8 540; 300 s reserve; 3600 s guard. Latest starts after the launch: 3120, 2640, 3000, 2685, 2310, 2910 and 2760 s (the run plan's be4 table). Tested at fixed times on **zq3-r14's real journal** (19 cases): its real Phase 9 (12:38:59Z) and T1 (12:39:03Z) starts GO, every latest start GO one second before STOP, zq3-r14's old T1 latest start (launch + 50:00) now STOP; a `/2` journal, two or no `launch` intents and a missing file refused (`rc=2`) | `d1476d28…` |
| 15. Run sheet | [run-sheet.md](run-sheet.md): Phases 0-12, 1a (**runs**), 4/1a (**required, new daemon**), U1-U3, N1, the SELinux gates, the R11, R12-1 and R13-1 checks, the fence record capture, the be4 guard budget table, the raised tool-timeout rule, `rockywait`, bash only, the zq3-r14 execution notes, "New for zq3-r15" and a run log template. `session.sh` (`83a31b73…`) and sixteen pinned helper scripts; `p10-requests.py` (`df990508…`) re-pins the three r15 request templates. The Phase 8-10 lines were dry-run end to end (below) | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-19T00:00:00Z --format=gnu`.
The f3 tarball holds only `hfs-20261119-zq3r15-fdq` and
`hfs-validation-xz3/zq3-r15`. It has **no entry for the existing parent**
`hfs-validation-xz3/`, so extraction never rewrites the parent's mode or
owner. All directories are `0700`, binaries `0500`, other files `0400`,
owner `0/0`.

## Signing

Paul delegated both signatures (A7) to Claude. In Phase 8 the driver's
stdout goes through `bin/sign_watch.py` (`978c4e1a…`). The watcher runs the
signer (`14d0a362…`) only if:
- the printed command is exactly one of the two below;
- the request file's SHA256 equals the printed `request_sha256`;
- each command runs at most once.

Anything else is refused, and the driver then times out (fails closed):

```
/home/paul/tmp/claude/hfs-zq3-r15-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r15-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r15-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r15-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r15-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r15-fill/bootstrap-run/work/signer-binding
```

On Paul's instruction, Claude places the operator key at the bundle's path
before the run. Nothing in the fill reads it.

## Run-time fills

| Fill | When and source | Used by |
|---|---|---|
| N1 | Phase 1: Rocky's time zone (`timedatectl`), the upgrade stamp and dnf history (`p1-upgrade.txt`, `n1-gate.py phase1`), the early gate (`p1-nightly-gate.txt`), boot ID, running/default kernel, `next_entry`, python3.9, `authorized_keys` and the pod runtime (`p1-pod-runtime.txt`, `pod-runtime-unchanged: True`); any drift stops for a refill. Phase 5 step 0: `n1-gate.py gate` | Phase 1 Rocky gate, Phase 3 install, Phase 5 go/no-go, T7 |
| D1 | **D1-a, Phase 1a**: both modules are loaded (16:01Z); Phase 1 confirms it, Phase 1a unloads them normally (never `-f`), and a refused unload stops before staging | Phase 4 step 0 window plan fill (needs both absent) |
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 16:01Z fact); Phase 4 step 0 rechecks it (`F1-unchanged`), a change stops | bootstrap `observer.ssh_sha256` (plan `7522f6e4…`) |
| F2 fdescfs window plan and its hash (`WP`) | Phase 4 step 0: controller `facts`, host hashes, `realpath` library closure, f3 `date -u +%s`, then `fdescfs-window-plan.py --pins` with the r15 pins | setup, qualify, restore (`W`); Phase 5 recheck |
| F3 pod rootfs manifest digest (`PM`) | Phase 3: `pod_fixture.py manifest` on Rocky (zq3-r12 to zq3-r14: `523485cc…`; **be4 changed `pod_workload.py`, so expect the predicted `34a71569…`**; any other digest is explained before `build`); `skip` only if A8 is declined | `build`, T7 request |
| F3a process observer record | Phase 4 step 1a: guard stage `process-observer`, checked by `p4-observer.py` | go/no-go before setup; **pass required** (new z94 daemon bytes `8668e5f0…`) |
| F4 observer key pair, enrolled line, `after_sha256` (`EA`) | Phase 4 step 5: `ssh-keygen` on f3, `observer_key.py enrol` | runtime observer, Phase 11 `remove --expect-sha256`, banner-timeout check |
| F5 BLS entry ID | Phase 3 install record | `select` (from the state directory) |
| F6 boot ID, provider nonce, admission manifest SHA256, `virtioN` names, proposal, controller, provision hash | Phase 8 admission and runtime records | enrollment and bind requests (bootstrap fills them), controller batch plan |
| F7 m0/m1 mount IDs | Phase 8 mount steps (mountinfo) | batch plan, then `p10-requests.py` for the T6/T7/T8 requests |
| F8 batch plan and its hash (`B`) | Phase 8, after READY and `usable`, at once: `fill_plan.py batch` (prints `cases_planned` and `deferred`), then `check-retained9p.sh check` | every controller and correlator call |
| F9 runtime UUID (`U`), `L`, cut, epoch and lifecycle sequences | READY record and `summary.json` via `p9-ready.py` (`--runtime`, `--observe-last`); Phase 9 uses L+1 and the T1 baseline L+2, then each from `correlate.py command` (`obs`, ARM, RESTART); expected L+1 to L+14 with T5 deferred | Phase 9 OBSERVE, T1-T4, T9-T11 |
| F10 exporter child PID (`XP`) | guard `receipt.json` `exporter.pid` (Phase 11 step 0; the `--no-child` line if null) | `host.py cleanup --child-pid` |
| F11 Rocky PID and lstart | **fixed** from the 16:01Z facts (`84753`, `Thu Nov 19 12:41:44 2026` UTC, zq3-r14's Phase 12); rechecked in Phases 1, 4 (also by every `$NG` stage) and 5 | native guard host profile, Phase 5 recheck, `p5-mem.txt` RSS |
| F12 UMA drain decision | Phase 5 step 1a: `p5-estimate.py` `drain_recommended` | Phase 5 step 1b (or once in Phase 6) |
| F13 T6/T7/T8 request files | Phase 8 after the batch check: `p10-requests.py` (exclusive, `0600`, in `$E`); lock budgets 150/240 (be4); the T7 request is pod `/2` with `runtime_nevra` and the be4 fixture pin | T6, T7, T8 |
| F14 guard PID | Phase 11, only on the runtime-still-serving path (`pgrep -U root`) | the one SIGTERM to the guard |
| F15 SELinux state | Phase 1 (`/.autorelabel` absent, Enforcing), Phase 9 (guest `getenforce` `Enforcing`, enforce `1`), Phase 12 (relabel record) | Phase 1 and Phase 9 gates; Phase 12 finding |
| F16 984 timing | Phases 9-10: `tf3` lines in `$E/obs-timing.tsv` (three `ssh-noop`, each timed OBSERVE), checked with a local `cat` after the Phase 9 OBSERVE; `p9-timing.py` after T4 and before Phase 11 | run log only; never a gate |
| F17 admission-step timings | Phase 10: `p10-admission.py` over the four cases' `stdout.log` after T4 (`p10-admission-steps.jsonl`; `admission_ms`, the largest gap, `deadline_hit`, `etimedout`; with the owner snapshots, any recall during admission), or over a failed case's before Phase 11 | run log only; never a gate |
| F18 transport fence record | `p11-fence.py`: Phase 8 failure block (`p8-fence.json`), the case-failure block (`t-fail-fence.json`) and always Phase 11 step 1a (`p11-fence.json`) over `runtime.stderr`, the owner snapshots/`f3-evidence.tar` (`transport.db`) and every OBSERVE taken | run log; `fenced: true` is a finding, never a retry |
| F19 guard budget | Phases 9-10: `gbudget STEP` (`$E/*-budget.json`) from the journal's `launch` intent: launch time, guard end, remaining seconds, `decision`; **be4 worst-case step seconds** (Phase 10 table) | GO/STOP before Phase 9 and each budgeted step; a STOP goes to Phase 11 |
| F20 unmount timings | Phase 11 step 2 and T8: each `rockyt … umount` line prints `rc` and `seconds` (`T0`) | R13-1 check; run log |

## Changes from the zq3-r14 sheet

All zq3-r7 to zq3-r14 rules are kept and written into the lines they name:
- **Phase 1a (D1-a): runs again** (zq3-r14 left both modules loaded): a
  normal `kldunload fusefs`, then `kldunload fdescfs`, never `-f`;
- **Phase 4 step 1a, the process observer check: required**, now because
  the daemon bytes changed (`8668e5f0…`, z94);
- the UMA drain decision in Phase 5; `rockyt` bounded guest unmounts
  (270 s); `pgrep -U root -lf runtime-guard.py` for the guard SIGTERM; the
  T10 abort paths to T8; the explicit `--no-child` line for a null
  exporter; observer key removal on every abort after Phase 4 step 5; the
  pause rule; the single host-preflight retry; the Phase 0 recheck of the
  workstation `/usr/bin/ssh` (`16cb2c6a…`, unchanged at fill time); the
  Phase 9-10 helpers;
- from the zq3-r5 to zq3-r7 executions and reviews: Phase 11 step 9 is one
  plain line with literal paths,
  `f3 "doas -n rm -- /var/tmp/hfs-validation-xz3/zq3-r15/observer/id_ed25519 /var/tmp/hfs-validation-xz3/zq3-r15/observer/id_ed25519.pub"`,
  and every abort path points at it; the window restore's outcome is
  `restoration-summary.json` (`"mount_restored": true`), not an `rc`
  reported through an output filter; `rc` capture after `rockyt`
  (`; echo rc=${PIPESTATUS[0]}`); Phase 11 step 4a AVC capture; the
  vm-bhyve log decides a relabel; the SELinux gates in Phases 1, 9 and 12;
- from zq3-r8: the h64 errata (` 9p ` mountinfo count; Phase 11 step 6
  waits for `child-gone` only), the z64 T5 deferral (r9 reason), the
  `vm-public` check of `10.20.0.133`, the persisted-variable list (`XP`,
  `ARM`, `RST`, `GUARD_PID`, `L`, `PM`, `T0`; every `VAR=` appended to
  `vars.env` with `printf %q`), Phase 11 step 4b full guest dmesg and the
  m64 block-line rule;
- from zq3-r9 to zq3-r11: journal `/3` and the b84 `usable` gate, evidence
  `/2`, the L sequence shift, `p9-ready.py`/`p9-observe.py`, the `tf3` 984
  timing with the `obs-timing.tsv` capture check, `printf %q`, the
  admission-step events, never chaining a follow-up after a bounded
  command;
- from zq3-r12 and zq3-r13: R1 (Europe/Sofia) with `sh/n1-gate.py` and its
  stamp rule, the early Phase 1 gate, the in-run cutoff, `p11-fence.py`
  with JSON-string decoding at every capture point, `rockywait` (tool
  timeout at least 900 s), never calling session functions inside
  `bash -c` without sourcing, the e94 pod `/2` request;
- from zq3-r14: the guard budget helper and its GO/STOP lines, bash-only
  `session.sh`, the timed unmounts, the N1 gate, the in-run cutoff,
  `p11-fence.py`, `rockywait`.

New for zq3-r15:
- **Candidate `ac8020aa…`** in the kernel plan, batch template, bootstrap
  plan (new daemon pin), guard argv (`--runtime-sha256 8668e5f0…`), window
  pins (new artifacts), staging, `p4-observer.py` and the Phase 2-4 and 8
  expectations.
- **be4 values:** request `seconds` 240, plan `seconds` 255, lock-probe
  budgets 150/240, the re-pinned guest tools, the recomputed guard budget
  (Phase 10 table, `sh/guard-budget.py`) and the raised **tool-timeout
  rule** (Rules): every `$CTL` line and the T6/T8 lock-probe lines at
  least 300 s, T7's `pod_fixture.py run` at least 420 s.
- **"Execution notes from the zq3-r14 run"** (R14-1, R13-1 fixed, the
  child-wait slip) and **"New for zq3-r15"** (z94, be4, the new PM, the
  expectations and remaining risks, the facts).
- **Phase 3:** `PM` is expected to differ from `523485cc…` (the new
  `pod_workload.py`); the predicted `34a71569…` is in the line's comment.
- **Phase 10:** a long admission or any recall during admission is
  recorded as a finding; `dispatching-uncertain` now means the 255 s
  dispatch bound ended before the guest did; the `deadline_hit` note names
  the 150 s alarm.
- **Phase 11 step 6:** the child-wait line names this run's pattern and is
  used exactly as written.

## Expectations: what changed since zq3-r14

zq3-r14 passed everything through Phase 9 and failed T1 on guest.py's 10 s
admission alarm (R14-1): the m0 root OPEN waited about 14 s behind three
recalls, at least one started by the read-only OPEN conflicting with its own
READ observation lease. With **z94**, a read-only OPEN conflicts only with
EXCLUSIVE_DIRTY leases and open reservations, and a root OPEN/RELEASE
journals no pin and keeps the cut, so **T1's credentials admission should
start no recall from the root OPEN or its close**. The root lstat's lease
release may still start one scoped recall per root. With **be4**, a slow
but correct admission has 150 s (each recall cost about 4-5 s in
zq3-r14). So **T1-T4 should pass**, then T6, T7, T9, T10, T11 and T8 (none
has run for real); T5 stays deferred. Record per-case admission timings
(`p10-admission.py`: `admission_ms`, the largest gap) and any recall during
admission (fence or domain-sequence steps in the owner snapshots and
OBSERVEs) as findings.

Remaining known risks, not fixed: **e84** (a non-root pin whose target
mismatches still starts an all-domain recall), **0a4** (the host never
drops a participant that stops acknowledging) and **the per-ACK SSH
observation cost** (about 0.5 s per ACK, five ACK phases per recall). A
guest that blocks past its alarm can outlive the controller's dispatch bound
(error/uncertain, safe and accepted by be4, a finding). Any `ETIMEDOUT`,
`view control worker stopped` line or `initial attributes timed out behind
a recall` line is still a finding.

**Guard budget.** The be4 worst cases (Phase 9 180 + T1-T4 2640 + T6 300 +
T7 615 + T9 with T10 990 + reserve 300 = 5025 s) do not fit the 3600 s
guard; the be4 review kept the guard and the GO/STOP gate, so a run with
several slow steps STOPs early and records the rest unrun. A healthy matrix
(each case well under a minute) ends about 35 minutes after the launch.

## Phases 9-10 recheck

Since zq3-r14, among the tools the sheet uses, be4 changed `guest.py`
(`48d6ae81…`), `controller.py` (`628ce17a…`), `guest-lock-probe.py`
(`fd439897…`), `pod_fixture.py` (`07314543…`) and `pod_workload.py`
(`c714370f…`), and `rocky_kernel.py` (`1bfc35a9…`) now admits only
`ac8020aa…`. `bootstrap.py` (`5e467fe6…`), `bootstrap_remote.py`
(`0dcb0fe0…`), `fill_plan.py` (`893350cf…`), `correlate.py` (`9cd55f2c…`)
and `argv.py` (`78462c2e…`) are unchanged. No CLI the sheet uses changed
shape.

**Sheet dry run** (`dryrun/` in the private fill; scratch output). A
harness extracted the 87 command lines of this sheet from Phase 8 "After
READY" to T8, plus the Phase 7 `k04` baseline line and the two Phase 11
evidence lines (90 in all), from `run-sheet.md` itself and ran them in
order under bash, with the real `fill_plan.py batch`,
`check-retained9p.sh check`, `correlate.py`, `session.sh` (`tf3`, `obs`,
`gbudget`, `sha`, `jf`) and the `sh/` helpers, `controller.py` (be4) with
faked guest dispatch, stub `f3`/`rocky` and a synthetic READY run (journal
`/3`, launch two minutes back, `ok` `usable`, L = 5). Every line passed:
- all ten `gbudget` lines `GO` with `rc=0` (be4 budgets);
- the filled batch plan carries plan `seconds` 255 and request `seconds`
  240 and passed `check` (`cases_planned` four, the deferral);
- L = 5; Phase 9 at 6; case pairs 7/8 to 13/14; T9 15/16/17; T10 18/19;
- T1-T4 correlated and released; `syscall_batch_success` True;
- `p10-admission.py` summarised all four cases (`admission_ms` 2000,
  `deadline_hit` and `etimedout` false);
- the T6/T8 lock requests (budgets 150/240) pass `validate_request`;
- T7: the request written by `p10-requests.py` (pod `/2`, `runtime_nevra`,
  the be4 fixture pin) passed `parse_request` in the Rocky stub;
- T9 `dropped-reply-effect-committed-and-visible`; T10 FENCED; T11
  `k04_refusal_observed` true;
- T8's timed unmount line `rc=0 seconds=0`.

**Negative and real-record checks** (`helpers-dryrun.py`: 38 cases, all as
expected). Also: `p9-ready.py` refused zq3-r14's real READY run (its
control socket is not the r15 path); `fill_plan.py batch` refused zq3-r14's
real run for the r15 plan pin (`journal is not for the pinned bootstrap
plan`); `p10-admission.py` on zq3-r14's real T1 log reports `deadline_hit`
true and the 14135.0 ms `open-root[0] -> failure` gap (R14-1).

## Findings and open questions

- **Q0 N1.** The run must start after the run day's daily upgrade has
  stamped and finish before 22:55 UTC that day; the latest Phase 5 start is
  21:25 UTC, no new case or step starts after 22:15 UTC, and the guest is
  powered off before 22:45 UTC. A later day needs no refill as long as
  Phase 1's rechecks hold.
- **Q1 T5 deferred.** The run cannot complete zq3 acceptance.
- **Q2 the guard budget.** The be4 worst cases sum to 5025 s against the
  3600 s guard; a run with several slow steps STOPs early by design (be4
  review decision). T9 with T10 must start by launch + 38:30.
- **Q3 the z94 daemon's first run.** `8668e5f0…` has passed only its
  build, admission and the offline tests. Phase 4 step 1a qualifies it on
  f3; Phase 8 is its first real launch. A recall during T1's admission
  (beyond one scoped recall per root lstat) is a finding against z94.
- **Q4 T1-T4, T6, T7, T9-T11 first real runs** with the be4 deadlines; T7
  and T9-T11 have never run for real. After a fence, T10's restore would
  refuse the ledger (by design).
- **Q5 the pod rootfs manifest.** Expected `34a71569…` (prediction from
  zq3-r14's real manifest); any other digest must be explained before
  `build`.
- **Q6 memory.** At 16:01Z free was 7.68 GB, inactive 1.40 GB and ARC
  6.03 GB (zq3-r14's fill: 7.59, 1.36, 5.76 GB; its active-based margin was
  0.22 GB and it drained). Phase 5's estimate and the Phase 6 bar decide.
- **Q7 UEFI vars drift.** The record pins `ff1ec37b…`. Any Rocky boot
  before Phase 7 changes it, and `host.py setup` then refuses.
- **Q8 Phase 1a.** zq3-r3 to zq3-r7 and zq3-r9 to zq3-r14 unloaded both
  modules without refusal. A refusal stops before staging.
- **Q9 window pins file:** the r15 window pins live only in the private
  fill, with their hash recorded here.
- **Q10 the tool timeouts.** The executing agent's shell tool must use at
  least 300 s for `$CTL` and lock-probe lines and at least 420 s for T7's
  run line; a tool kill would leave an uncertain dispatch.

No committed tool refused real inputs. These intended refusals all
occurred:
- the controller template; the unfilled bootstrap template;
- the superseded xd4 `3a7131dd` candidate in a synthetic batch plan and in
  `rocky_kernel.py plan`;
- zq3-r14's seconds (plan 120, requests 60) in a synthetic batch plan;
- an undeclared four-case batch and a deferred `credentials`;
- the window filler on the xc4 artifact hashes and on facts with both
  modules loaded;
- `pod_fixture.py parse_request` on the malformed pod requests and the
  zq3-r14 fixture pin;
- `guest-lock-probe.py` on a case budget of 151 s;
- `p4-observer.py` on zq3-r14's real record (xc4 `sha256`);
- `n1-gate.py gate` at the STOP times; `guard-budget.py` after each
  latest start and on malformed journals.

## Local verification

At `312abdb` (a clean worktree of main), all passing:
- retained9p: 391 tests;
- directlaunch: 26 tests;
- `tools/lab` top level: 249 tests (3 skipped);
- `go test ./internal/runtimeprep`.

`shellcheck` is clean for `build-stage.sh` and `sh/*.sh` (as POSIX sh), and
for `session.sh` apart from SC2034/SC2086: its variables are used by the
sheet, and `$O` is split on purpose (which is why it needs bash). No code
was changed.
