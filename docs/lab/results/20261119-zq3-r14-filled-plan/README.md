# zq3-r14 filled run inputs (2026-11-19)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r13 fill](../20261119-zq3-r13-filled-plan/README.md) ·
[zq3-r13 record](../20261119-zq3-r13/README.md) ·
[zq3-r14 record](../20261119-zq3-r14/README.md) ·
[xd4 candidate](../../../../tools/integration/artifacts/evidence/xz3-20261119-r14/README.md)

These are the exact run inputs for `zq3-r14`, the fourteenth attempt of zq3.
They were generated on the workstation with the committed tools at `main`
`c0e8ea2` (after xd4). They mirror the
[zq3-r13 fill](../20261119-zq3-r13-filled-plan/README.md), which ran for
real: Phases 0 to 9 passed (Phase 1a unloaded both modules, Phase 4 step 1a
qualified the daemon, both mounts, READY, `usable` L = 2, the Phase 9
OBSERVE), T1-T11 went unrun because the session paused about 55 minutes
after READY and the guard's 3600 s bound ran out, and in Phase 11 m0's
unmount hung after the host had stopped (R13-1). The changes are:
- the run ID (`zq3-r14`) and the native guard root
  `/var/tmp/hfs-20261119-zq3r14-fdq`;
- the xd4 candidate `3a7131dd…`, which rebuilt the guest kernel for oc4
  (build ID `9b167690…`, bzImage `e0daa57e…`) and the native exporter for
  pc4 (`2fd7e32f…`, **never run on f3**). The daemon `c959b708…`, the kernel
  config, the modules and the window artifacts (`8194ebe0…`/`8c43fd06…`)
  are reused byte for byte;
- `hfs-runtime-prep` and the signer rebuilt at `c0e8ea2`; no Go source
  changed, so they differ from zq3-r13's builds only in the VCS stamp;
- everything that pins them re-pinned: the argv record (new exporter and
  new UEFI vars), the bundle (the committed spec now pins the exporter), the
  kernel plan (regenerated with `rocky_kernel.py`), the templates, staging,
  the bootstrap plan, the watcher and `hpin`;
- **the zq3-r13 lessons:** a guard time budget (`sh/guard-budget.py`,
  `gbudget`; Phase 9 within 5 minutes of READY, a budget check before each
  step that needs the live runtime), **bash only** (`session.sh` refuses
  zsh; every line runs through a bash wrapper that sources `session.sh` and
  `vars.env`), and the **R13-1 check** (each Phase 11 and T8 unmount prints
  its `rc` and seconds);
- **Phase 1a (D1-a) runs again:** zq3-r13 left `fdescfs.ko` and `fusefs.ko`
  loaded.

Nothing here used SSH, the network, the lab, a VM, a mount, an installation
or the operator private key. **No authority follows from these files:**
every class A step in the [run sheet](run-sheet.md) still needs Paul's
explicit final GO, including the Phase 1a unload and T7 (A8). **zq3
acceptance stays incomplete by design**, because T5 does not run.

## N1: Rocky upgrades itself once a day

Rocky's clock is `Europe/Sofia (EET, +0200)` (zq3-r12's run-time revision
R1). `unattended-upgrade-rocky.timer` fires hourly at :55 local (and once
about 10 minutes after a boot), but the daily mode runs `dnf upgrade` only
once per local day, at 00:55 EET = **22:55 UTC**, stamps
`/var/lib/unattended-upgrade/last-daily` and never reboots Rocky. The
2026-11-19 11:31:26Z facts (`rocky-upgrade-…txt`): stamp `2026-11-19`
(written 00:55:04 EET), newest dnf transaction still #64 of 2026-11-18
00:55, so that run changed nothing; the run 11 minutes after zq3-r13's
Phase 12 start (10:35 EET) only logged the deferred reboot. **The run must
start after that day's run has stamped and finish before the next one,
22:55 UTC on the run day (2026-11-19 22:55 UTC for a run that day).** Phase 1
runs `n1-gate.py gate` early (before Phase 1a changes anything) and Phase 5
step 0 runs it again before Rocky is stopped: `STOP` if Phase 1 predates the
last 00:55 local, within 35 minutes after it, after 23:25 local (**21:25 UTC,
the latest Phase 5 start**), or if the Phase 1 stamp is not that day's. The
gate computes the dates from its own clock, so a run on a later day needs no
edit; the in-run cutoff (no new case after 22:15 UTC, guest poweroff before
22:45 UTC, the 22:35 UTC fallback) applies on the run day. Any new dnf
transaction lets the pod-runtime and kernel rechecks decide; drift means a
refill. The facts match zq3-r13's on every Rocky pin (kernel
`741.22.1.el9_9`, python3.9 `729ac37f…`, podman, crun, conmon,
seccomp.json, `authorized_keys`).

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-zq3-r14-fill` (`0700`). This directory holds only
public hashes, paths, argv and the read-only facts. These stay private and
are never committed:
- the per-session keys;
- the runtime config, which holds host receipt private keys;
- the staging tarballs that contain it.

## Inputs

| Input | Value |
|---|---|
| Candidate | **xd4 rebuild** (task xd4): manifest `3a7131dd…` (195 files), package `/home/paul/tmp/claude/hfs-xz3-20261119-r14/package`, source checkout `942614a`, repository baseline `b7d107b` plus `inputs/repository-changes.patch`. **New:** the oc4 guest kernel `6.1.190-hfs-xz3` (bzImage `e0daa57e…`, GNU build ID `9b167690…`, System.map `dd88dfcb…`) and the pc4 native exporter bhyve `2fd7e32f…` (GNU build ID `43f2ae41…`; `argv.py` `78462c2e…` pins it). **Reused byte for byte from xc4 `e9c77e5b…`:** the daemon `c959b708…` (GNU build ID `eac40082…`), config `28fe4a8e…`, the five modules and `modules.*`, and the window artifacts `8194ebe0…` (test) and `8c43fd06…` (helper). Init helper `8c02b98f…` unchanged. **Lab tools:** `controller.py` `3b08031f…` and `rocky_kernel.py` `25924379…` (admit only `3a7131dd…`), `argv.py` `78462c2e…`; `guest.py` `d8361fc0…`, `guest-lock-probe.py` `11521fe6…`, `pod_fixture.py` `01f8e842…`, `bootstrap.py` `5e467fe6…`, `bootstrap_remote.py` `0dcb0fe0…`, `correlate.py` `9cd55f2c…`, `fill_plan.py` `893350cf…` and `runtime-guard.py` `fece96b3…` unchanged (`git diff 4a3cf82 c0e8ea2 -- tools/lab tools/integration docs/lab/zq3-fdescfs-window`: in `tools/lab` only `controller.py`, `rocky_kernel.py`, `directlaunch/argv.py`, their tests, `test_fdescfs_window_plan.py` and READMEs; the window `pins.json` is unchanged, `build-record.json` gained a note) |
| Phase 1 facts | [facts/](facts/), collected read-only 2026-11-19 11:31:26Z; they supersede zq3-r13's 06:16:49Z set. **f3:** boot `1794574308` (unchanged since zq3-r8), **`fdescfs.ko` (id 30) and `fusefs.ko` (id 31) loaded**, no fuse/fdesc mount, free 7.59 GB + inactive 1.36 GB, ARC 5.76 GB, `/usr/bin/ssh` `6a9e5573…`, python3.12 `a66eb370…`, `10.20.0.133` on `vm-public` (members `tap0`, `re0`). **Rocky:** PID `46923` (UTC lstart `Thu Nov 19 08:24:51 2026`, zq3-r13's Phase 12 `start-rocky`), boot ID `5d5c32fe-…`, running stock `5.14.0-741.22.1.el9_9` = grub default, `next_entry` empty, `boot_indeterminate=7`, SELinux Enforcing, `/.autorelabel` absent, no `hfs-xz3` file in `/boot` (zq3-r13's U2), `/boot` 328 MB free, python3.9 `729ac37f…`, `authorized_keys` `e08542dc…`. **Clock and upgrade** (`rocky-upgrade-…txt`): `Europe/Sofia (EET, +0200)`, synchronized; timer hourly at :55; stamp `2026-11-19` (00:55:04 EET); newest dnf transaction #64 (2026-11-18 00:55). **Pod runtime** (`rocky-pod-runtime-…txt`, byte-identical to zq3-r13's): podman `249e918c…` `podman-6:5.8.5-3.el9_9.x86_64`, crun `659867c7…` `crun-1.29.1-1.el9.x86_64`, conmon `1ac2a8db…` (`conmon-3:2.2.1-2.el9_8`), seccomp.json `7dd44119…` (`containers-common-5:5.8-2.el9`). The published files are the f3 Phase 1 batch, the network/modules extract, the launch facts, the pod-runtime batch and the Rocky upgrade record; they hold no key material (grep for key headers and `ssh-*` lines) |
| Host events since zq3-r13's fill | zq3-r13's own Rocky stop (07:13:50, ACPI) and normal start (08:24:51 UTC, vm-bhyve run 1, no relabel reboot, U2 done); Rocky's upgrade timer ran once at 10:35 EET and changed nothing; the xd4 native exporter build ran on f3 under `native_guard.py` in `/var/tmp/hfs-xz3-20261119-r14-r1` (left as evidence; Rocky's PID, lstart and `rocky.conf` unchanged after it). No f3 reboot since Paul's deliberate 2026-11-13 reboot |
| Launch facts | `f3-launch-facts-20261119T113126Z.json` (`09269ec8…`): `rocky.conf` `c101adf1…`, vm-run `43753bc8…`, firmware `6b48e967…` unchanged; **UEFI vars `379f79e7…`** (zq3-r13: `530b541e…`); launch logged `2026-11-19T10:24:51+02:00` |
| Tap | `tap100` (bridge members `re0`, `tap0`) |
| Run ID / staging | `zq3-r14`, `/var/tmp/hfs-validation-xz3/zq3-r14` (the parent exists and holds zq3-r1 to zq3-r13) |
| Native guard root | `/var/tmp/hfs-20261119-zq3r14-fdq`, host profile `--rocky-pid 46923 --rocky-birth 'Thu Nov 19 08:24:51 2026'`, guard `576d802a…` (unchanged) |
| Window directory | `/var/tmp/hfs-fdescfs-windows/zq3-r14` (the parent exists, `root:wheel 0700`) |
| Rocky names | `/root/hfs-zq3-zq3-r14-*`, `/var/tmp/hfs-zq3-zq3-r14` (the zq3-r1 to zq3-r13 ones stay as evidence) |
| Operator public key | `2f6d909c…`; key path `…/hfs-zq3-r14-fill/operator/operator-signing-key` (empty `0700` directory, never read here) |

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | **Rebuilt** at `c0e8ea2` (as the brief requires): `hfs-runtime-prep` and the signer `hfs-retained9p-operator`, with `-trimpath`, CGO, VCS stamps and an unmodified private clone (`vcs.modified=false`); a second build of each, forced with `-a`, gave the same bytes. **No Go source changed:** `git diff afe34fb c0e8ea2 -- internal cmd go.mod go.sum` touches only `internal/guestbridge/README.md` and the spec testdata. Builds with `-buildvcs=false` at `afe34fb` and at `c0e8ea2` are byte-identical (prep `034a6f70…`, signer `d01a39cc…`), so the new stamped builds differ from zq3-r13's (prep `2381f504…`, signer `bba12e52…`, retired) only in `vcs.revision`/`vcs.time`. G3 init helper: zq3-r1's reproduced build | prep `25ab9c4f…`, signer `5e284fda…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from `testdata/zq3-spec.json` at `c0e8ea2` (`make-spec.py`: only `zq3-r1` → `zq3-r14` and the r14 operator key path change; the same method reproduces zq3-r13's spec from `afe34fb`'s testdata byte for byte). Since zq3-r13 the committed spec changed in one value, `Exporter.SHA256` `ef8f17de…` → `2fd7e32f…` (xd4) | spec `f99be774…`, request template `24daf961…`, runtime config `ea3d2359…`, volume-init `78b1739e…`; volume `199e26a1-…`; sessions hfs0 `5ab6d77e-…`, hfs1 `4f468364-…` |
| 1. Argv | `argv.py generate` (at `c0e8ea2`, exporter pin `2fd7e32f…`) from the 11:31:26Z launch facts (sessions, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `dc29130d…`, args `0072ffe2…`, vars `379f79e7…`, exporter `2fd7e32f…` |
| 2. Kernel plan | `rocky_kernel.py plan` at main (`25924379…`; admits only `3a7131dd…`), stock default `/boot/vmlinuz-5.14.0-741.22.1.el9_9.x86_64` (the fresh grub default, which Rocky runs). Against zq3-r13's it differs in `run_id`, `manifest_sha256` and the bzImage and System.map entries (the oc4 kernel); config, the five modules, `modules.*` and the stock default are unchanged. The xc4 package `e9c77e5b…` is refused as superseded ("use the xd4 rebuild") | plan `39e9b962…`, build ID `9b167690…` |
| 3. Controller batch | Template derived from zq3-r13's by `derive-templates.py`: run ID, fill path, sessions, candidate `e9c77e5b…` → `3a7131dd…`; probe `guest.py` `d8361fc0…` unchanged. **T5 stays deferred** with the r9 reason (unchanged). The four other requests keep canonical order. `controller.py` `3b08031f…`. `check` refuses the template as designed (`probe/installed-manifest pin`). A synthetic copy with zero IDs **passes** and prints `cases_planned` (four) and the deferral. The same copy is **refused** with the xc4 candidate (superseded, "use the xd4 rebuild"), without the declaration ("five-case syscall batch minus declared deferrals") and with `credentials` also deferred ("case may not be deferred") | template `9763443f…` |
| 4. Bootstrap plan | Template derived from zq3-r13's (the one that ran through READY). New pins: bundle, kernel plan, `rocky_kernel.py` and the rebuilt prep `25ab9c4f…`. Unchanged: the daemon `c959b708…` (`runtime.sha256`), `bootstrap_remote.py` `0dcb0fe0…` (`remote_sha256`), the guard pin `fece96b3…`. The script re-hashes every pinned file at main and in the package. The unfilled template refuses as designed (`observer.ssh_sha256: SHA256 hex`). **F1 filled now** with `fill_plan.py bootstrap` (`893350cf…`) from the 11:31Z `sha256 -r /usr/bin/ssh` line (`6a9e5573…`), then `bootstrap.py check` (`5e467fe6…`, journal `/3`) **pass** | template `abe6904d…`, filled plan `6a88fc8f…` |
| 5. Guard argv | Derived from zq3-r13's, with `--runtime-sha256 c959b708…` (checked against the package) and `--disposable-validation` exactly once, last. It equals `bootstrap.py`'s own `SshRemote.guard_argv()` for the filled plan, in shell string and argv, and passes `runtime-guard.py`'s `parse_args` and `check_command` | `d88c4bf3…`, in [pins](pins.json) |
| 6. Pod template (e94) | zq3-r13's `homelabfs-retained9p-pod/2` template with the run ID renamed. `derive-templates.py` compares its `runtime_nevra` `{"podman": "podman-6:5.8.5-3.el9_9.x86_64", "crun": "crun-1.29.1-1.el9.x86_64"}` and the four `runtime_sha256` values with the facts' `== pod-runtime` section (byte-identical to zq3-r13's), cross-checks the hashes with the Phase 1 `sha256sum` lines, and runs the committed `pod_fixture.py`'s own `check_nevra_pins` and `parse_request` (synthetic fill): **both accept**. `parse_request` refuses a `pod/1` request without `runtime_nevra`, version `/1` with `runtime_nevra`, swapped NEVRAs, a `podman-docker` sibling, an extra conmon NEVRA and a stale fixture pin | pod template `53a2a948…` |
| 7. Observer and staging pins | Unchanged script pins: `observer_key.py` `cd7f38c2…`, `observer.py` `2e1449dd…`, `installed.py` `c824b183…`, native guard `576d802a…`, `runtime-guard.py` `fece96b3…`, `fdescfs-window.py` `b00b19d1…`, `host.py` `738e1ca5…`, `host_snapshot.py` `b29ffed2…`, `guest-lock-probe.py` `11521fe6…`, `pod_fixture.py` `01f8e842…`. Re-pinned to main: `rocky_kernel.py` `25924379…` and `argv.py` `78462c2e…`. New staged bytes: the oc4 kernel (bzImage, System.map) and the pc4 exporter. All 17 f3 and 18 Rocky files went into two deterministic tarballs; a second tar run gave the same hashes. A verification extract matched every expected hash (35) and the f3 modes (owner `0/0`, no parent entry) and was then deleted | f3 tar `a2c73ef8…`, Rocky tar `d130905f…` |
| 8. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` at main (`750424db…`, unchanged since xc4) with `zq3-r1` → `zq3-r14`; `check_pins` passes. Synthetic fills, which are not pins: zq3-r8's real Phase 4 facts and host hashes (modules absent) **pass**, with paths renamed and the three artifact hashes the xc4 ones xd4 reuses. The same facts with the 5a4 artifact hashes zq3-r12 staged **refuse** (`pinned artifact differs or missing`). Post-restore facts with both modules loaded, the state on f3 now, **refuse** (`fdescfs module present`): hence Phase 1a | r14 pins `fc61debe…` |
| 9. Signing watcher | zq3-r13's `bin/sign_watch.py` (it signed both requests in zq3-r12 and zq3-r13) with `D`, the docstring run ID, the signer comment and **the signer pin `5e284fda…`** changed. Tested locally with a fake signer: a wrong request hash, a wrong command, an unknown action, a repeat and a wrong signer hash were refused; both exact requests were signed | `55fbcc15…` |
| 10. Process observer check | `sh/p4-observer.py` from zq3-r13 with the run ID and docstring changed; the daemon pin `c959b708…` is unchanged (Phase 4 step 1a stays **required**). zq3-r13's real records (renamed) **pass** unchanged and are refused only for the `sha256` with the 5a4 daemon substituted. The `$NG` line (Rocky PID `46923`) parses and passes the native guard's own `parse_args` and `check_request` | `f0fc345e…` |
| 11. N1 gate helper | `sh/n1-gate.py` from zq3-r13 (docstring only). Retested at fixed times (`--now`, tests only): 13:30Z GO (also with the real 11:31Z upgrade record), 21:20Z and 21:25Z GO, 21:30Z, 22:56Z and 23:10Z STOP, the next day with the old Phase 1 STOP, the next day with a fresh Phase 1 and stamp GO, a stale stamp STOP; `phase1` reports `dnf_unchanged` true for the facts and false for a synthetic #65 | `0f5f8561…` |
| 12. Fence record helper | `sh/p11-fence.py` from zq3-r13 (docstring only). Retested: the real nc4 row and stderr line (found), zq3-r13's real Phase 11 evidence (`fenced` false) and a synthetic `bootstrap-run/calls/*.json` record with the OBSERVE reply as a JSON string (found) | `05e8fe2c…` |
| 13. Guard budget helper (new) | `sh/guard-budget.py`: the guard's remaining seconds from the bootstrap journal's `launch` intent and the 3600 s bound, and a GO/STOP per step (phase9, T1-T4, T6, T7, T9 with T10, T10, T8) with a 300 s Phase 11 reserve; `session.sh` `gbudget` runs it. Tested at fixed times on zq3-r13's real journal (16 cases): zq3-r13's real Phase 9 start (08:10:55Z) is STOP, 07:19Z GO, each latest start is GO one second before STOP; a `/2` journal, two or no `launch` intents and a missing file are refused (`rc=2`) | `d1c5f6d5…` |
| 14. Run sheet | [run-sheet.md](run-sheet.md): Phases 0-12, 1a (**runs**), 4/1a and U1-U3, N1, the SELinux gates in Phases 1, 9 and 12, the R11, R12-1 and R13-1 checks, the fence record capture, the guard budget, `rockywait`, bash only, and a run log template. `session.sh` (`04942bda…`; bash check, `gbudget`, `rockyt` bound 270 s, `rockywait`) and sixteen pinned helper scripts; `p10-requests.py` re-pins the three r14 request templates. The Phase 8-10 lines were dry-run end to end (below) | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-19T00:00:00Z --format=gnu`.
The f3 tarball holds only `hfs-20261119-zq3r14-fdq` and
`hfs-validation-xz3/zq3-r14`. It has **no entry for the existing parent**
`hfs-validation-xz3/`, so extraction never rewrites the parent's mode or
owner. All directories are `0700`, binaries `0500`, other files `0400`,
owner `0/0`.

## Signing

Paul delegated both signatures (A7) to Claude. In Phase 8 the driver's
stdout goes through `bin/sign_watch.py` (`55fbcc15…`). The watcher runs the
signer (`5e284fda…`) only if:
- the printed command is exactly one of the two below;
- the request file's SHA256 equals the printed `request_sha256`;
- each command runs at most once.

Anything else is refused, and the driver then times out (fails closed):

```
/home/paul/tmp/claude/hfs-zq3-r14-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r14-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r14-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r14-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r14-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r14-fill/bootstrap-run/work/signer-binding
```

On Paul's instruction, Claude places the operator key at the bundle's path
before the run. Nothing in the fill reads it.

## Run-time fills

| Fill | When and source | Used by |
|---|---|---|
| N1 | Phase 1: Rocky's time zone (`timedatectl`), the upgrade stamp and dnf history (`p1-upgrade.txt`, `n1-gate.py phase1`), the early gate (`p1-nightly-gate.txt`), boot ID, running/default kernel, `next_entry`, python3.9, `authorized_keys` and the pod runtime (`p1-pod-runtime.txt`, `pod-runtime-unchanged: True`); any drift stops for a refill. Phase 5 step 0: `n1-gate.py gate` | Phase 1 Rocky gate, Phase 3 install, Phase 5 go/no-go, T7 |
| D1 | **D1-a, Phase 1a**: both modules are loaded (11:31Z); Phase 1 confirms it, Phase 1a unloads them normally (never `-f`), and a refused unload stops before staging | Phase 4 step 0 window plan fill (needs both absent) |
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 11:31Z fact); Phase 4 step 0 rechecks it (`F1-unchanged`), a change stops | bootstrap `observer.ssh_sha256` (plan `6a88fc8f…`) |
| F2 fdescfs window plan and its hash (`WP`) | Phase 4 step 0: controller `facts`, host hashes, `realpath` library closure, f3 `date -u +%s`, then `fdescfs-window-plan.py --pins` with the r14 pins | setup, qualify, restore (`W`); Phase 5 recheck |
| F3 pod rootfs manifest digest (`PM`) | Phase 3: `pod_fixture.py manifest` on Rocky (zq3-r12 and zq3-r13: `523485cc…`; no dnf transaction since, so expected again); `skip` only if A8 is declined | `build`, T7 request |
| F3a process observer record | Phase 4 step 1a: guard stage `process-observer`, checked by `p4-observer.py` | go/no-go before setup; **pass required** (every run) |
| F4 observer key pair, enrolled line, `after_sha256` (`EA`) | Phase 4 step 5: `ssh-keygen` on f3, `observer_key.py enrol` | runtime observer, Phase 11 `remove --expect-sha256`, banner-timeout check |
| F5 BLS entry ID | Phase 3 install record | `select` (from the state directory) |
| F6 boot ID, provider nonce, admission manifest SHA256, `virtioN` names, proposal, controller, provision hash | Phase 8 admission and runtime records | enrollment and bind requests (bootstrap fills them), controller batch plan |
| F7 m0/m1 mount IDs | Phase 8 mount steps (mountinfo) | batch plan, then `p10-requests.py` for the T6/T7/T8 requests |
| F8 batch plan and its hash (`B`) | Phase 8, after READY and `usable`, at once: `fill_plan.py batch` (prints `cases_planned` and `deferred`), then `check-retained9p.sh check` | every controller and correlator call |
| F9 runtime UUID (`U`), `L`, cut, epoch and lifecycle sequences | READY record and `summary.json` via `p9-ready.py` (`--runtime`, `--observe-last`); Phase 9 uses L+1 and the T1 baseline L+2, then each from `correlate.py command` (`obs`, ARM, RESTART); expected L+1 to L+14 with T5 deferred | Phase 9 OBSERVE, T1-T4, T9-T11 |
| F10 exporter child PID (`XP`) | guard `receipt.json` `exporter.pid` (Phase 11 step 0; the `--no-child` line if null) | `host.py cleanup --child-pid` |
| F11 Rocky PID and lstart | **fixed** from the 11:31Z facts (`46923`, `Thu Nov 19 08:24:51 2026` UTC, zq3-r13's Phase 12); rechecked in Phases 1, 4 (also by every `$NG` stage) and 5 | native guard host profile, Phase 5 recheck, `p5-mem.txt` RSS |
| F12 UMA drain decision | Phase 5 step 1a: `p5-estimate.py` `drain_recommended` | Phase 5 step 1b (or once in Phase 6) |
| F13 T6/T7/T8 request files | Phase 8 after the batch check: `p10-requests.py` (exclusive, `0600`, in `$E`); the T7 request is pod `/2` with `runtime_nevra` | T6, T7, T8 |
| F14 guard PID | Phase 11, only on the runtime-still-serving path (`pgrep -U root`) | the one SIGTERM to the guard |
| F15 SELinux state | Phase 1 (`/.autorelabel` absent, Enforcing), Phase 9 (guest `getenforce` `Enforcing`, enforce `1`), Phase 12 (relabel record) | Phase 1 and Phase 9 gates; Phase 12 finding |
| F16 984 timing | Phases 9-10: `tf3` lines in `$E/obs-timing.tsv` (three `ssh-noop`, each timed OBSERVE), checked with a local `cat` after the Phase 9 OBSERVE; `p9-timing.py` after T4 and before Phase 11 | run log only; never a gate |
| F17 admission-step timings | Phase 10: `p10-admission.py` over the four cases' `stdout.log` after T4 (`p10-admission-steps.jsonl`; `deadline_hit`, `etimedout`), or over a failed case's before Phase 11 | run log only; never a gate |
| F18 transport fence record | `p11-fence.py`: Phase 8 failure block (`p8-fence.json`), the case-failure block (`t-fail-fence.json`) and always Phase 11 step 1a (`p11-fence.json`) over `runtime.stderr`, the owner snapshots/`f3-evidence.tar` (`transport.db`) and every OBSERVE taken | run log; `fenced: true` is a finding, never a retry |
| F19 guard budget (new) | Phases 9-10: `gbudget STEP` (`$E/*-budget.json`) from the journal's `launch` intent: launch time, guard end, remaining seconds, `decision` | GO/STOP before Phase 9 and each budgeted step; a STOP goes to Phase 11 |
| F20 unmount timings (new) | Phase 11 step 2 and T8: each `rockyt … umount` line prints `rc` and `seconds` (`T0`) | R13-1 check; run log |

## Changes from the zq3-r13 sheet

All zq3-r7 to zq3-r13 rules are kept:
- **Phase 1a (D1-a): runs again**, as in zq3-r3 to zq3-r7 and zq3-r9 to
  zq3-r13: a normal `kldunload fusefs`, then `kldunload fdescfs`, never `-f`;
- Phase 4 step 1a, the process observer check: **required**, although the
  daemon `c959b708…` is unchanged and passed it in zq3-r13 (cheap, and every
  run's records show it);
- the UMA drain decision in Phase 5;
- `rockyt` bounded guest unmounts (Phase 11 step 2 and T8; 270 s);
- `pgrep -U root -lf runtime-guard.py` for the guard SIGTERM;
- the T10 abort paths to T8, then the guard-SIGTERM branch;
- the explicit `--no-child` line for a null exporter;
- observer key removal on every abort after Phase 4 step 5;
- the pause rule when f3 is unreachable;
- the single retry after a host-preflight-only failure;
- the Phase 0 recheck of the workstation `/usr/bin/ssh` (`16cb2c6a…`,
  unchanged at fill time);
- the Phase 9-10 helpers.

Kept from the zq3-r5 to zq3-r13 executions and the review decisions:
- **Phase 11 step 9 is one plain line with literal paths**:
  `f3 "doas -n rm -- /var/tmp/hfs-validation-xz3/zq3-r14/observer/id_ed25519 /var/tmp/hfs-validation-xz3/zq3-r14/observer/id_ed25519.pub"`.
  Every abort path points at that line.
- **The window restore's outcome is `restoration-summary.json`**, not an
  `rc` reported through an output filter (step 10, `"mount_restored": true`).
- **Every `rockyt … | tee` line ends with `; echo rc=${PIPESTATUS[0]}`**
  (T8, Phase 11 step 2), now followed by the unmount's `seconds`.
- Phase 11 step 4a captures the guest's AVC records, step 4b the full guest
  `dmesg`. The vm-bhyve log decides whether a relabel happened, and the
  SELinux gates in Phases 1, 9 and 12 stay.
- The h64 errata: guest 9p checks count ` 9p ` mountinfo lines; Phase 11
  step 6 waits for `child-gone` only.
- The z64 T5 deferral (r9 reason), the Phase 1 check that `10.20.0.133` is
  on `vm-public`, the persisted-variable list (`XP`, `ARM`, `RST`,
  `GUARD_PID`, `L`, `PM`, now `T0`, every `VAR=` appended to `vars.env` with
  `printf %q`), the m64 block-line rule and the Phase 9 tuple gate.
- From zq3-r9 to zq3-r11: journal `/3` and the b84 `usable` gate, OBSERVE
  evidence `/2`, the L sequence shift, `p9-ready.py`/`p9-observe.py`, the
  `tf3` 984 timing with the `obs-timing.tsv` capture check, the extended
  tool-timeout rule (controller lines, T6/T7/T8, Phase 3, U3 at least
  300 s; Phase 8 in the background), the `observe-p9-2.json` rerun name,
  the `admission-step` timings and the case-failure capture with live owner
  snapshots; never chain a follow-up after a bounded command (zq3-r11).
- From zq3-r12 and zq3-r13: the e94 pod `/2` request with `runtime_nevra`,
  the `etimedout` flag and the Phase 11 ETIMEDOUT scan, every guest
  block-line grep matching `view control worker stopped` and `timed out
  behind a recall`, R1 (Europe/Sofia) with `sh/n1-gate.py` and its stamp
  rule, the early Phase 1 gate, the in-run cutoff (no new case or step after
  22:15 UTC on the run day; Phase 11 step 5 before 22:45 UTC; the 22:35 UTC
  fallback), `p11-fence.py` with JSON-string decoding at every capture
  point, `rockywait` (tool timeout at least 900 s) and never calling session
  functions inside `bash -c` without sourcing.

New for zq3-r14:
- **Candidate `3a7131dd…`** in the kernel plan, batch template, bootstrap
  plan, window pins (renamed only), staging, Phase 2, Phase 3, Phase 8 and
  Phase 10 expectations; the pc4 exporter in the argv record and the bundle.
- **A new "Execution notes from the zq3-r13 run" section** (the operator
  pause, R13-1, the zsh slip, what is kept) and an updated "New for
  zq3-r14" section (the oc4/pc4 behaviour to record, the facts).
- **Guard budget (zq3-r13 lesson):** the header, the rules, Phase 8 "After
  READY" (no pause), Phase 9 (start within 5 minutes of READY; first line
  `gbudget phase9`) and Phase 10 (a budget table with each step's expected
  seconds and latest start after the launch; a `gbudget STEP` line opens
  T1-T4, T6, T7, T9, T10 and, while the runtime serves, T8). STOP: the step
  and the rest unrun, Phase 11 through its guard-SIGTERM branch.
- **Bash only (zq3-r13 lesson):** the rules and Session setup state the
  wrapper (`bash -c 'source …/session.sh && { test ! -e …/vars.env ||
  source …/vars.env; } && <line>'`); `session.sh` refuses other shells.
- **R13-1 check:** Phase 11 step 2 and T8 print each unmount's `rc` and
  `seconds`; after a host stop an unmount should return within about
  30-60 s; `rc=124` is a finding.
- **oc4 behaviour to record:** errnos `-EHOSTDOWN` (-112) and `-ESTALE`
  (-116) where zq3-r12 saw `-EPROTO` (-71) for ERROR 5/4, `exporter ERROR
  <n>` in block and worker-stop lines, and any `transport_failed` sign; the
  R12-1 check now names `(-112), exporter ERROR 5` for a fenced host, and
  after a FENCED T10 such lines are expected.
- **Dates stated generically:** the gate computes the dates; the cutoff
  applies "on the run day" (2026-11-19 22:55 UTC for a run that day).

## Expectations: what changed since zq3-r13

zq3-r13 reached READY with both mounts and passed `usable` and Phase 9
(R12-1 fixed by nc4). Its matrix went unrun after an operator pause, and in
Phase 11 m0's unmount hung after the host had stopped:
- **R13-1:** the exporter's endpoint check failed after the runtime
  stopped and it sent no reply; virtio dropped bhyve's empty completion and
  the guest's hfs 9p wait was unbounded, so m0's view worker held
  `session->writer` and the unmount's `hfs_control` waited in D past 270 s;
  the poweroff then took about 7.5 minutes.

xd4 changes (oc4 guest kernel, pc4 exporter; the daemon is unchanged):
- the exporter answers ERROR 5 UNAVAILABLE for a failed result read or
  endpoint check, so every well-formed exchange gets a reply;
- the guest maps ERROR 5 to `-EHOSTDOWN` and ERROR 4 to `-ESTALE` (others
  stay `-EPROTO`), logs `exporter ERROR <n>` in its block and worker-stop
  lines, stops retries and polls once a session is blocked (teardown
  exempt), bounds every hfs reply wait at 30 s and marks a session whose
  exchange timed out or got ERROR 5 `transport_failed`, which stops even
  its teardown retries.

So everything through Phase 9 should go as in zq3-r13. **T1-T4 should then
run for the first time since zq3-r11's T1** (R11-1, fixed by y94), and T6,
T7 and T9-T11 (never run for real) should run; T5 stays deferred. Phase 11's
unmounts should return promptly even after the host stops (R13-1 check).
Record the new guest errno and log behaviour wherever it shows. Remaining
known risks, not fixed: **z94** (self-started all-domain recalls during
admission, survivable since y94, but slower), **0a4** (the host never drops
a participant that stops acknowledging) and **the new exporter's first
run** (`2fd7e32f…`). Any `ETIMEDOUT`, any `view control worker stopped` line
and any `initial attributes timed out behind a recall` line is still
recorded as a finding.

**Timing check against the 90-120 s getattr worst case** (unchanged since
zq3-r12; `docs/Test-automation.md`): guest.py's 10 s admission alarm, the
plan's 60 s case alarm and the controller's 75 s dispatch bound are all
shorter, so a slow syscall ends as a `Deadline` or `dispatching-uncertain`
(findings); `rockyt`'s 270 s fits one worst-case getattr plus the unmount's
own 30 s view wait; an `rc=124` is a finding, never retried. The guard
budget assumes each case takes at most 300 s at the operator's pace.

## Phases 9-10 recheck

Since zq3-r13, among the tools the sheet uses, only `controller.py`
(`3b08031f…`, admits only `3a7131dd…`), `rocky_kernel.py` (`25924379…`) and
`argv.py` (`78462c2e…`, exporter pin) changed. `bootstrap.py` (`5e467fe6…`),
`bootstrap_remote.py` (`0dcb0fe0…`), `fill_plan.py` (`893350cf…`),
`correlate.py` (`9cd55f2c…`), `guest.py` (`d8361fc0…`),
`guest-lock-probe.py` (`11521fe6…`) and `pod_fixture.py` (`01f8e842…`) are
unchanged. No CLI the sheet uses changed shape.

**Sheet dry run** (`dryrun/` in the private fill; scratch output). A
harness extracted the 87 command lines of this sheet from Phase 8 "After
READY" to T8, plus the Phase 7 `k04` baseline line and the two Phase 11
evidence lines (90 in all; zq3-r13: 80, the difference is the ten new
`gbudget` lines), from `run-sheet.md` itself and ran them in order under
bash. The case-failure block (it names `$CASE`), the T8 hang capture (a
text block) and the Phase 8 failure block (before "After READY") only run
on a failure and are skipped. The harness used:
- the real `fill_plan.py batch`, `check-retained9p.sh check`,
  `correlate.py`, `session.sh` (`tf3`, `obs`, `gbudget`, `sha`, `jf`) and
  the `sh/` helpers;
- `controller.py` with faked guest dispatch, whose case output carries the
  y84 `admission-step` events before `admitted`;
- stub `f3`/`rocky` that answer like the lifecycle client (evidence `/2`
  with a claim summary), `host_snapshot.py` and the guest; the Rocky stub
  parses the uploaded T7 request with the committed `pod_fixture.py`'s own
  `parse_request`;
- a synthetic READY run with journal `/3` (now with `utc` times, the launch
  two minutes back), an `ok` `usable` result, L = 5 and mountinfo rows with
  the exact g64 options.

Every line passed with the expected results:
- all ten `gbudget` lines `GO` with `rc=0`;
- L = 5; Phase 9 at 6 (L+1); case pairs 7/8, 9/10, 11/12, 13/14; T9 15,
  ARM 16, post-CALL 17; T10 18, RESTART 19;
- the batch fill and check printing `cases_planned` (four) and the deferral;
- T1-T4 correlated and released; `syscall_batch_success` True with the
  deferral in the state;
- `p10-admission.py` summarised all four cases (`deadline_hit` and
  `etimedout` false);
- T7: the request written by `p10-requests.py` (`homelabfs-retained9p-pod/2`,
  `runtime_nevra` with the epoch form) passed `parse_request`;
- T9 `dropped-reply-effect-committed-and-visible`;
- T10 FENCED;
- T11 `k04_refusal_observed` true, with `deferred` repeated;
- the T6/T8 lock requests from `p10-requests.py`;
- T8's timed unmount line `rc=0 seconds=0`;
- `p9-timing.py` summarised the timed OBSERVEs and no-ops.

**Negative and real-record checks** (`helpers-dryrun.py`: 38 cases, all as
expected, as in zq3-r13): `p10-admission.py`'s `etimedout` cases,
`p9-ready.py`'s and `p9-observe.py`'s refusals, `p9-timing.py`,
`p10-argv.py` and `p10-requests.py`. Also: `fill_plan.py batch` refused a
synthetic run that stopped at `usable` with the b84 claims note
(`bootstrap steps did not end ok: usable=failed`) and a `/2` journal;
`p9-ready.py` refused the failed run and zq3-r13's real READY run (its
control socket is not the r14 path); `fill_plan.py batch` refused zq3-r13's
real run for the r14 plan pin (`journal is not for the pinned bootstrap
plan`); `guard-budget.py` printed STOP for zq3-r13's real journal at its
real Phase 9 start; `p10-admission.py` on zq3-r11's real T1 log still
reports `deadline_hit` true and the 43.6 s `open-root` gap.

## Findings and open questions

- **Q0 N1.** The run must start after the run day's daily upgrade has
  stamped and finish before 22:55 UTC that day; the latest Phase 5 start is
  21:25 UTC, no new case or step starts after 22:15 UTC, and the guest is
  powered off before 22:45 UTC. A later day needs no refill as long as
  Phase 1's rechecks hold (the gate computes the dates); a new dnf
  transaction lets the pod/kernel rechecks decide.
- **Q1 T5 deferred.** The run cannot complete zq3 acceptance. T5 needs a
  re-plan and sizing first.
- **Q2 the guard budget.** The per-step seconds (300 s per case, 420 s for
  T7, 540 s for T9 with T10) and the 300 s Phase 11 reserve are estimates
  from the bounds, not measurements: no case has run since zq3-r11. A
  healthy matrix should finish about 35 minutes after the launch; a STOP
  ends the matrix early by design.
- **Q3 the new exporter's first run.** `2fd7e32f…` has passed only the
  native lib9p tests. Phase 8 exercises it first (launch, proposal,
  admission, mounts); anything new there is a finding against pc4.
- **Q4 oc4 on the lab.** The bounded 30 s reply wait and
  `transport_failed` have never run on f3. A healthy host should never hit
  them before Phase 11; an `ETIMEDOUT` or `(-112), exporter ERROR 5` before
  the host stops is a finding. A guest client that still owns timed-out
  requests is kept until the guest reboots (by design).
- **Q5 admission time vs bounds.** Unchanged from zq3-r12: one getattr can
  take about 90-120 s; the 10 s admission alarm, the 60 s case alarm and
  the 75 s dispatch bound are all shorter. Raising them is a code/plan
  change, not done here.
- **Q6 T7 first real run.** T7 has never run for real. The rootfs manifest
  should again be `523485cc…` (no dnf transaction since zq3-r12); a
  different digest must be explained before `build`.
- **Q7 memory.** At 11:31Z, about three hours after Rocky's start, free was
  7.59 GB, inactive 1.36 GB and ARC 5.76 GB (free + inactive 1.8 GB lower,
  ARC 1.3 GB higher than at zq3-r13's fill). Phase 5's estimate and the
  Phase 6 bar decide (zq3-r13: active_based margin 0.45 GB, Phase 6 margin
  0.49 GB after the drain).
- **Q8 UEFI vars drift.** The record pins `379f79e7…`. Any Rocky boot
  before Phase 7 changes it, and `host.py setup` then refuses. Recheck right
  before the run.
- **Q9 Phase 1a.** zq3-r3 to zq3-r7 and zq3-r9 to zq3-r13 unloaded both
  modules without refusal. A refusal now stops before staging and needs a
  plan revision.
- **Q10 window pins file:** the r14 window pins live only in the private
  fill, with their hash recorded here.
- **Q11 T6, T7, T9-T11:** never run for real; the T9 ARM, T10 FENCED step 2
  and the T11 receipt wait (5 minutes) have only been dry-run on the
  current tools. After a fence, T10's restore would refuse the ledger (by
  design).
- **Q12 the fence record in practice.** `p11-fence.py` reads the
  `transport.db` row from raw bbolt pages; it was checked on a real row
  written by the committed code, but a fence on f3 has never been recorded
  (zq3-r13: `fenced` false).

No committed tool refused real inputs. These intended refusals all
occurred:
- the controller template;
- the unfilled bootstrap template;
- the superseded xc4 `e9c77e5b` candidate in a synthetic batch plan and in
  `rocky_kernel.py plan`;
- an undeclared four-case batch and a deferred `credentials`;
- the window filler on the 5a4 artifact hashes and on facts with both
  modules loaded;
- `pod_fixture.py parse_request` on the malformed pod requests above;
- `n1-gate.py gate` at the STOP times above;
- `guard-budget.py` after each latest start and on malformed journals.

## Local verification

At `c0e8ea2` in the main checkout, all passing:
- retained9p: 381 tests;
- directlaunch: 26 tests;
- `tools/lab` top level: 248 tests (3 skipped);
- `go test -count=1 ./internal/runtimeprep`.

`shellcheck` is clean for `build-stage.sh` and `sh/*.sh` (as POSIX sh), and
for `session.sh` apart from SC2034/SC2086: its variables are used by the
sheet, and `$O` is split on purpose (which is why it needs bash). No code
was changed.
