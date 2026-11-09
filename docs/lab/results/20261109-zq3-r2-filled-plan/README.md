# zq3-r2 filled run inputs (2026-11-09)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r1 fill](../20261108-zq3-filled-plan/README.md) ·
[zq3-r1 record](../20261109-zq3-r1/README.md)

The exact run inputs for `zq3-r2`, the second attempt of zq3, generated on the
workstation with the committed tools at `main` `ea55eb5` (after g54). They
mirror the [zq3-r1 fill](../20261108-zq3-filled-plan/README.md) step by step;
only run-ID-dependent values, the fresh facts and the g54 revisions change.
Nothing here used SSH, the network, the lab, a VM, a mount, an installation or
the operator private key. **No authority follows from these files:** every
class A step in the [run sheet](run-sheet.md) still needs Paul's explicit
final GO.

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-zq3-r2-fill` (`0700`). This directory holds only
public hashes, paths, argv and the read-only facts. The per-session keys, the
runtime config (it holds host receipt private keys) and the staging tarballs
that contain it stay private and are never committed.

## Inputs

| Input | Value |
|---|---|
| Candidate | unchanged: manifest `b203799e…`, source `defd974`, daemon `174949c1…`, bhyve `36423b6d…` |
| Phase 1 facts | [facts/](facts/), collected read-only 2026-11-09 11:57Z: f3 boot `1794212992` (10:29:52 EET), Rocky PID `31625` (UTC lstart `Mon Nov  9 11:42:19 2026`), Rocky boot ID `a6a64434-…`, stock `5.14.0-687.66.1.el9_8.x86_64`, `next_entry` empty, python3.9 `1143733d…`, `authorized_keys` `e08542dc…`, f3 `/usr/bin/ssh` `6a9e5573…`, `vfs.zfs.arc_min` 0 |
| Launch facts | `f3-launch-facts-20261109T115732Z.json` (`cca11b18…`): `rocky.conf` `c101adf1…` unchanged, **UEFI vars `cab0ac2c…`** (zq3-r1: `3c3d8783…`; Rocky booted since) |
| Tap | `tap100` (bridge members `re0`, `tap0`) |
| Run ID / staging | `zq3-r2`, `/var/tmp/hfs-validation-xz3/zq3-r2` (parent exists, holds zq3-r1) |
| Native guard root | `/var/tmp/hfs-20261109-zq3r2-fdq`, host profile `--rocky-pid 31625 --rocky-birth 'Mon Nov  9 11:42:19 2026'` |
| Window directory | `/var/tmp/hfs-fdescfs-windows/zq3-r2` (parent exists, `root:wheel 0700`) |
| Rocky names | `/root/hfs-zq3-zq3-r2-*`, `/var/tmp/hfs-zq3-zq3-r2` (zq3-r1's stay as evidence) |
| Operator public key | `2f6d909c…`; key path `…/hfs-zq3-r2-fill/operator/operator-signing-key` (empty `0700` directory, never read here) |

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | `hfs-runtime-prep`, `hfs-retained9p-operator` built (`-trimpath`, VCS-stamped, unmodified clone) from `ea55eb5`; `git diff defd974 ea55eb5 -- internal cmd` touches only `internal/runtimeprep/bootstrap_fixture_test.go`, so non-test code equals `defd974`. G3 init helper: zq3-r1's reproduced build reused (`build-init-helper.sh` and its source `e0cca0c` unchanged since `08831e7`) | prep `22de7965…`, signer `22e5fb90…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from zq3-r1's spec with `zq3-r1` → `zq3-r2` and the r2 operator key path (equal to `testdata/zq3-spec.json` apart from run ID and key path); actual-runtime validation passed | spec `e91125ac…`, generation `badf334f…`, request template `9df3c9a8…`, runtime config `0d17b186…`, volume-init `12f5cd32…`; volume `a6e4d458-…`; sessions hfs0 `efaaa84a-…`, hfs1 `d75b87b9-…` |
| 1. Argv | `argv.py generate` from the 11:57Z launch facts (sessions, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `0a7e36f3…`, args `f8d43b3c…`, vars `cab0ac2c…` |
| 2. Kernel plan | `rocky_kernel.py plan`, stock default `/boot/vmlinuz-5.14.0-687.66.1.el9_8.x86_64`; differs from zq3-r1's only in `run_id` | plan `adf62fc8…`, kernel build ID `acc77b5e…` |
| 3. Controller batch | template derived from zq3-r1's (run ID, fill path, sessions); `check` refuses as designed (`probe/installed-manifest pin`); a synthetic copy (zero IDs) passes | template `d6923aa2…` |
| 4. Bootstrap plan | template derived likewise (new prep, config, request template and kernel plan pins); **F1 filled now** with `fill_plan.py bootstrap` from the 11:57Z `sha256 -r /usr/bin/ssh` line, then `bootstrap.py check` **pass** | template `631b0aa9…`, filled plan `eabd2800…` |
| 5. Guard argv | derived, then compared with `bootstrap.py`'s own `SshRemote.guard_argv()` for the filled plan: equal | in [pins](pins.json) |
| 6. Observer and staging pins | unchanged script pins (`observer_key.py` `cd7f38c2…`, `observer.py` `2e1449dd…`, `installed.py` `c824b183…`); all 17 f3 and 18 Rocky staged files, two tarballs (recipe below), a verification extract matched every expected hash | f3 tar `87ba6b4a…`, Rocky tar `bd7de8b8…` |
| 7. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` with `zq3-r1` → `zq3-r2` (run ID and five paths); `fdescfs-window-plan.py`'s `check_pins` passes, and a synthetic fill from zq3-r1's renamed facts passes (not a pin) | r2 pins `d64487fa…` |
| 8. Signing watcher | Claude's zq3-r1 watcher copied to `bin/sign_watch.py`, only `D` and the docstring run ID changed; tested locally with a fake signer (wrong hash, wrong command and a repeat refused; both exact requests signed); after review it also re-hashes the signer binary (`22e5fb90…`) and logs any watcher error as a refusal while relaying | `1eba4df3…` |
| 9. Run sheet | [run-sheet.md](run-sheet.md), Phases 0-12 plus U1-U3; session helpers `session.sh` (`b4eccf6c…`) and four pinned helper scripts | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-09T00:00:00Z --format=gnu`.
The f3 tarball holds `hfs-20261109-zq3r2-fdq` and `hfs-validation-xz3/zq3-r2`
only, **without an entry for the existing parent** `hfs-validation-xz3/`, so
extraction never rewrites the parent's mode or owner. All directories `0700`,
binaries `0500`, other files `0400`, owner `0/0`.

## Signing

Paul delegated both signatures (A7) to Claude. In Phase 8 the driver's stdout
goes through `bin/sign_watch.py` (`1eba4df3…`), which runs the signer only if
the printed command is exactly one of these and the request file's SHA256
equals the printed `request_sha256`, each at most once; anything else is
refused and the driver times out (fails closed):

```
/home/paul/tmp/claude/hfs-zq3-r2-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r2-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r2-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r2-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r2-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r2-fill/bootstrap-run/work/signer-binding
```

The operator key is placed at the bundle's path by Claude before the run, on
Paul's instruction. Nothing in the fill reads it.

## Run-time fills

| Fill | When and source | Used by |
|---|---|---|
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 11:57Z fact); Phase 4 step 0 rechecks it (`F1-unchanged`), a change stops | bootstrap `observer.ssh_sha256` (plan `eabd2800…`) |
| F2 fdescfs window plan and its hash | Phase 4 step 0: controller `facts` (needs staged inspector), host hashes, `realpath` library closure, f3 `date -u +%s`, then `fdescfs-window-plan.py --pins` r2 pins | setup, qualify, restore; Phase 5 recheck (`owned.json`) |
| F3 pod rootfs manifest digest | Phase 3: `pod_fixture.py manifest` on Rocky | `build`, pod request |
| F4 observer key pair, enrolled line, `after_sha256` | Phase 4 step 5: `ssh-keygen` on f3, `observer_key.py enrol` | runtime observer, Phase 11 `remove --expect-sha256` |
| F5 BLS entry ID | Phase 3 install record | `select` (from the state directory) |
| F6 boot ID, provider nonce, admission manifest SHA256, `virtioN` names, proposal, controller, provision hash | Phase 8 admission and runtime records | enrollment and bind requests (bootstrap fills them), controller batch plan |
| F7 m0/m1 mount IDs | Phase 8 mount steps (mountinfo) | batch plan, lock-probe requests, pod request |
| F8 batch plan and its hash | Phase 8, after READY: `fill_plan.py batch`, then `check-retained9p.sh check` | every controller and correlator call |
| F9 runtime UUID and lifecycle sequences | READY record, then each reply | OBSERVE, ARM, RESTART |
| F10 exporter child PID | runtime and guard evidence | `host.py cleanup --child-pid` |
| F11 Rocky PID and lstart | **fixed** from the 11:57Z facts (`31625`, `Mon Nov  9 11:42:19 2026` UTC); rechecked in Phases 1, 4 and 5 | native guard host profile, Phase 5 recheck |

## Changes from the zq3-r1 sheet

Required by the brief (a)-(i): no `--` after `--stage` and single-use stage
names; csh-safe f3 commands; `realpath` library pins; the g54 Phase 5
rechecks with exact expected values and the step-1 failure path; the 13.75 GiB
Phase 6 bar with its arithmetic; the r2 Rocky pin in the native guard
profile; the watcher in Phase 8; `session.sh`-style helpers and a `git
archive` snapshot of `ea55eb5`.

Beyond those:

- **Existing parents.** Phase 2 checks `/var/tmp/hfs-validation-xz3` and
  `/var/tmp/hfs-fdescfs-windows` are `drwx------ root wheel` and tests the r2
  paths absent **through `doas`**: as `paul`, `test ! -e` inside a root `0700`
  directory is true even when the path exists. The f3 tarball has no parent
  entry (see above). Phase 4 checks the window parent instead of creating it.
- **csh fixes in r1 lines.** zq3-r1's `doas -n ls /dev/vmm 2>/dev/null || echo
  no-vmm` (Phases 5, 6, 11) passed `2` to `ls` under csh; replaced by `test -e
  /dev/vmm && doas -n ls /dev/vmm || echo no-vmm`. Phase 1's `ps -o lstart`
  now runs under `env TZ=UTC`, so it compares with the guard's UTC birth
  (zq3-r1's Phase 4 pin file recorded local time).
- **Pinned helper scripts** (`sh/`, checked by `hpin`): `p1-f3.sh` (Phase 1
  batch, POSIX sh on stdin), `p4-libs.sh` (realpath closure, skips the base
  libraries the filler would refuse as duplicates), `p5-recheck.py` (the three
  g54 rechecks, exit 0 only if all hold), `p6-admission.py` (the bar). Phase 6
  also compares `--check-descriptor-exec` with `owned.json`.
- **r2 window pins** are a derived copy, not the committed file: the
  committed `pins.json` names `zq3-r1` paths and the filler takes the run
  directory from it. The derivation is a pure `zq3-r1` → `zq3-r2` rename.
- **No snapshot refresh** step: `$S` is the current main HEAD; Phase 0 states
  which diff makes a newer main a plan revision.
- Removed: the zq3-r1 p44 resume block; the window parent creation line.

## Findings and open questions

- **Q1 window pins file.** Should the r2 window pins be committed beside
  `docs/lab/zq3-fdescfs-window/pins.json` (for example as a per-run file),
  rather than living only in the private fill with its hash recorded here?
- **Q2 UEFI vars drift (zq3-r1 F4, still open).** The record pins
  `cab0ac2c…`. Any Rocky boot before Phase 7 (an f3 reboot with Rocky's AUTO
  start, or a Rocky reboot) changes it, and `host.py setup` then refuses. The
  Phase 5 rechecks catch an f3 reboot before select; a Rocky-only reboot is
  caught by the PID/lstart recheck.
- **Q3 memory with Rocky running.** At 11:57Z free + inactive + ARC was
  4,985,882,880 bytes with Rocky up (zq3-r1's Phase 5 saw about 14 GB free),
  so Rocky's bhyve now appears nearly fully resident. It should be freed at
  the stop; Phase 6 decides. A miss there is restore + U3.
- **Q4 Phase 4 → Phase 8 time.** The window plan expires 55 minutes after
  step 0; restore ignores expiry, setup and qualify do not. A pause between
  step 0 and qualify needs the `-resume` refill described in the sheet.
- F3, F5 and F6 of zq3-r1 (120 s bootstrap, RESTARTED T10 leaves the runtime
  serving, `test_bootstrap` reads the run plan) still apply unchanged.

No committed tool refused real inputs. The only refusals were the two
intended template refusals (controller template, unfilled bootstrap
template).

## Local verification

At `ea55eb5` in a clean clone: retained9p 280 tests, directlaunch 26,
`test_fdescfs_window` + `test_fdescfs_window_plan` + `test_runtime_guard`
66, `test_runtime_volume_init` + `test_vms_sh` 21 (1 skip), `go test
./internal/runtimeprep`, all passing. `shellcheck` is clean for `build-stage.sh`
and `sh/*.sh`, and for `session.sh` apart from SC2034/SC2086 (its variables
are used by the sheet, and `$O` is split on purpose). No code was changed.

## Round-1 review fixes (2026-11-09)

- Phase 5 and 6 subtract `kstat.zfs.misc.arcstats.c_min` (the effective ARC
  floor the runtime guard also uses, about 527 MB on f3), not
  `vfs.zfs.arc_min`, whose 0 means "default floor".
- `hpin` (run in Phase 0 and Phase 8) also pins the signer binary
  `22e5fb90…`; the watcher re-hashes it before each signature.
- The watcher catches its own errors, logs them as refusals and keeps
  relaying, so a watcher failure cannot end the driver mid-bootstrap.
- Phase 11 splits the native unmount check from the action and states how
  to end a runtime left serving after a RESTARTED T10 (r1 finding F5).
