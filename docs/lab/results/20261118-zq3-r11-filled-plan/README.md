# zq3-r11 filled run inputs (2026-11-18)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r10 fill](../20261114-zq3-r10-filled-plan/README.md) ·
[zq3-r10 record](../20261115-zq3-r10/README.md) ·
[b94 candidate](../../../../tools/integration/artifacts/evidence/xz3-20261117-r11/README.md)

These are the exact run inputs for `zq3-r11`, the eleventh attempt of zq3.
They were generated on the workstation with the committed tools at `main`
`e21528c` (after b94). They mirror the
[zq3-r10 fill](../20261114-zq3-r10-filled-plan/README.md), which ran for
real: past `usable` and Phase 9, then T1 failed (R10-1), T8 held, and
Phases 11 and 12 ran cleanly. The changes are:
- the run ID (`zq3-r11`) and the native guard root
  `/var/tmp/hfs-20261118-zq3r11-fdq`;
- the b94 candidate `3b128cc6…`, which rebuilt the guest kernel (x84, z84;
  build ID `c4e08f6f…`) and the daemon (`bf86d805…`; 194); the config, the
  five modules and the exporter are r84's bytes;
- the y84 lab tools `guest.py` and `guest-lock-probe.py`, re-pinned;
- `hfs-runtime-prep` and the signer rebuilt;
- everything that pins them, re-pinned, and the kernel plan regenerated;
- the bundle regenerated from the unchanged committed spec with the new code;
- **Phase 1a (D1-a) runs again:** zq3-r10 left `fdescfs.ko` and `fusefs.ko`
  loaded;
- a new helper, `sh/p10-admission.py`, for the y84 `admission-step` timings;
- R10-1 and R10-2 checks in Phase 10, T8 and Phase 11;
- the fresh facts: **Rocky's nightly upgrade and Paul's S1-a restart** (S1
  below); the fill was refreshed from the 08:21:45Z facts.

Nothing here used SSH, the network, the lab, a VM, a mount, an installation
or the operator private key. **No authority follows from these files:**
every class A step in the [run sheet](run-sheet.md) still needs Paul's
explicit final GO, including the Phase 1a unload. **zq3 acceptance stays
incomplete by design**, because T5 does not run.

## S1: Rocky's stock kernel changed (resolved by S1-a)

Rocky runs a nightly `dnf -y upgrade` at 00:55. On 2026-11-17 it applied a
301-package EL9.9 update ("Reboot is required") without a reboot, so the
first facts of this fill (2026-11-18 07:31:50Z) showed Rocky still running
`5.14.0-687.66.1.el9_8` while `grubby --default-kernel` and `saved_entry`
named `5.14.0-741.22.1.el9_9`. python3.9 (`729ac37f…`), podman `5.8.5`
(`249e918c…`), crun `1.29.1` (`659867c7…`), dracut, systemctl, udevadm and
grub2-editenv changed too. `rocky_kernel.py` refuses `install`, `select` and
`remove` unless the running kernel equals the plan's stock default
(`require_release`), and `install` also refuses unless the grub default
equals it (`preflight_install`), so that state could not pass Phase 3.

**Paul chose S1-a.** Rocky was restarted cleanly with `tools/lab/vms.sh`
`stop-rocky`/`start-rocky` at 08:21 UTC. It now runs `741.22.1.el9_9` = grub
default = the kernel plan's stock default (plan `649cc2ed…`, unchanged),
Enforcing, no `/.autorelabel`: PID `92402`, UTC lstart `Wed Nov 18 08:21:19
2026`, boot ID `be05dd18-…`, UEFI vars `dc5e2923…`. The fill was refreshed
from the 08:21:45Z facts: a new argv record and f3 tarball, `NG` and
`session.sh`, `p5-recheck.py`, `p5-estimate.py`, the sheet, the pins and this
README. The bundle, kernel plan, templates, bootstrap plan, Rocky tarball and
window pins are unchanged. The observer lines pin the updated python3.9, and
the pod template the updated podman and crun.

**The nightly 00:55 `dnf -y upgrade` must not run between this fill and the
run** (planned for 2026-11-18). Phase 1 rechecks the boot ID, both kernels,
python3.9 and `authorized_keys`; any difference stops the run for a refill.

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-zq3-r11-fill` (`0700`). This directory holds only
public hashes, paths, argv and the read-only facts. These stay private and
are never committed:
- the per-session keys;
- the runtime config, which holds host receipt private keys;
- the staging tarballs that contain it.

## Inputs

| Input | Value |
|---|---|
| Candidate | **b94 rebuild** (task b94): manifest `3b128cc6…`, package `/home/paul/tmp/claude/hfs-xz3-20261117-r11/package`, source `dfdcdd9`. **Guest kernel rebuilt (x84, z84):** `6.1.190-hfs-xz3`, bzImage `5b08b3d2…`, GNU build ID `c4e08f6f…`, System.map `341071f6…`; config `28fe4a8e…`, the five modules and `modules.*` byte-identical to `4712e479`. **Daemon `bf86d805…`** (GNU build ID `d3af6b7c…`; 194 on top of r84). **Reused byte for byte:** the native exporter `ef8f17de…` (`argv.py` `ed63ff97…` unchanged). Window artifacts `8abf53bc…` (test) and `62d0d3fa…` (helper), rebuilt from `dfdcdd9`. Init helper `8c02b98f…` unchanged. **Lab tools (y84):** `guest.py` `d8361fc0…`, `guest-lock-probe.py` `11521fe6…` |
| Phase 1 facts | [facts/](facts/), collected read-only 2026-11-18 08:21:45Z, after the S1-a restart; they supersede the 07:31:50Z set (kept privately) and the 20261114T212428Z set. **f3:** boot `1794574308` (unchanged since zq3-r8), **`fdescfs.ko` (id 27) and `fusefs.ko` (id 28) loaded**, no fuse/fdesc mount, free 10.38 GB + inactive 0.31 GB, ARC 3.98 GB, `/usr/bin/ssh` `6a9e5573…`, python3.12 `a66eb370…`, `10.20.0.133` on `vm-public` (members `tap0`, `re0`). **Rocky:** PID `92402` (UTC lstart `Wed Nov 18 08:21:19 2026`, started by the S1-a `start-rocky`), boot ID `be05dd18-…`, running stock `5.14.0-741.22.1.el9_9` = grub default (S1), `next_entry` empty, `boot_indeterminate=7`, SELinux Enforcing, `/.autorelabel` absent, no `hfs-xz3` file in `/boot` (zq3-r10's U2), python3.9 `729ac37f…` (was `1143733d…`), `authorized_keys` `e08542dc…`. The published files are the f3 Phase 1 batch, the network/modules extract and the launch facts, as for zq3-r10; they hold no key material (grep for key headers and `ssh-*` lines) |
| Host events since zq3-r10 | zq3-r10's own Rocky stop (08:15:20) and normal start (2026-11-15 08:21:01 UTC, no relabel reboot); Rocky's nightly `dnf -y upgrade` (00:55) applied a 301-package EL9.9 update on 2026-11-17 without a reboot; the S1-a clean restart (`vms.sh stop-rocky`/`start-rocky`) on 2026-11-18 at 08:21 UTC booted the new stock kernel. **The 00:55 nightly upgrade must not run between this fill and the run.** No f3 reboot since Paul's deliberate 2026-11-13 reboot |
| Launch facts | `f3-launch-facts-20261118T082145Z.json` (`53783673…`): `rocky.conf` `c101adf1…`, vm-run `43753bc8…`, firmware `6b48e967…` unchanged; **UEFI vars `dc5e2923…`** (zq3-r10: `09f21e31…`; 07:31Z, before the restart: `c19535b3…`); launch logged `2026-11-18T10:21:19+02:00` |
| Tap | `tap100` (bridge members `re0`, `tap0`) |
| Run ID / staging | `zq3-r11`, `/var/tmp/hfs-validation-xz3/zq3-r11` (the parent exists and holds zq3-r1 to zq3-r10) |
| Native guard root | `/var/tmp/hfs-20261118-zq3r11-fdq`, host profile `--rocky-pid 92402 --rocky-birth 'Wed Nov 18 08:21:19 2026'`, guard `576d802a…` (unchanged) |
| Window directory | `/var/tmp/hfs-fdescfs-windows/zq3-r11` (the parent exists, `root:wheel 0700`) |
| Rocky names | `/root/hfs-zq3-zq3-r11-*`, `/var/tmp/hfs-zq3-zq3-r11` (the zq3-r1 to zq3-r10 ones stay as evidence) |
| Operator public key | `2f6d909c…`; key path `…/hfs-zq3-r11-fill/operator/operator-signing-key` (empty `0700` directory, never read here) |

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | **Rebuilt** at `e21528c`: `hfs-runtime-prep` and the signer `hfs-retained9p-operator`. The build used `-trimpath`, CGO, VCS stamps and an unmodified private clone (`vcs.modified=false`); a second build of each, forced with `-a`, gave the same bytes. Reason: `git diff 5a95ad4 e21528c` over the `go list -deps` directories of both commands (the same list as r10) lists non-test changes only in `internal/guestbridge` (`controlled_protocol.go`, `service.go`: 194). `go.mod`/`go.sum` are unchanged. zq3-r10's builds (prep `b522b6cb…`, signer `ac8e32c4…`) are retired. G3 init helper: zq3-r1's reproduced build | prep `d6fe51ae…`, signer `fc2ffcf4…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from `testdata/zq3-spec.json` at `e21528c` (`make-spec.py`: only `zq3-r1` → `zq3-r11` and the r11 operator key path change; the same method reproduces zq3-r10's spec from the same testdata byte for byte). The committed spec is unchanged since r9 | spec `fb07c411…`, request template `408f2969…`, runtime config `f90f5090…`, volume-init `d85f4a95…`; volume `6788b457-…`; sessions hfs0 `a112c1e3-…`, hfs1 `a1f24140-…` |
| 1. Argv | `argv.py generate` from the 08:21:45Z launch facts (sessions, `tap100`; regenerated after the S1-a restart, the 07:31Z record `4b217f49…` is retired), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `88763ba5…`, args `c0a0c2ca…` (unchanged), vars `dc5e2923…` |
| 2. Kernel plan | `rocky_kernel.py plan` at main (`5bef419e…`; admits only `3b128cc6…`), stock default `/boot/vmlinuz-5.14.0-741.22.1.el9_9.x86_64` (the fresh grub default, which Rocky runs since the S1-a restart; unchanged by the refill). Against zq3-r10's it differs in `run_id`, `manifest_sha256`, `stock_default_kernel`, the bzImage (hash, size, build ID) and System.map; config and modules are the same. The r84 package `4712e479…` is refused as superseded ("use the b94 rebuild"). An unstaged alternative with the 687 default (S1-b, not chosen) is `7962f137…` (not a pin) | plan `649cc2ed…`, build ID `c4e08f6f…` |
| 3. Controller batch | Template derived from zq3-r10's by `derive-templates.py`: run ID, fill path, sessions, candidate `4712e479…` → `3b128cc6…`, **probe `guest.py` `c50bb849…` → `d8361fc0…` (y84)**. **T5 stays deferred** with the r9 reason (unchanged). The four other requests keep canonical order. `controller.py` `b1671cf1…`. `check` refuses the template as designed (`probe/installed-manifest pin`). A synthetic copy with zero IDs **passes** and prints `cases_planned` (four) and the deferral. The same copy is **refused** with the r84 candidate (superseded), without the declaration ("five-case syscall batch minus declared deferrals") and with `credentials` also deferred ("case may not be deferred") | template `49664303…` |
| 4. Bootstrap plan | Template derived from zq3-r10's (the one that ran). New pins: bundle, kernel plan, `rocky_kernel.py`, the b94 daemon `bf86d805…` and the rebuilt prep `d6fe51ae…`. Unchanged: `bootstrap_remote.py` `0dcb0fe0…` (`remote_sha256`), the guard pin `fece96b3…`. The script re-hashes every pinned file at main and in the package. The unfilled template refuses as designed (`observer.ssh_sha256: SHA256 hex`). **F1 filled now** with `fill_plan.py bootstrap` (`893350cf…`) from the 07:31Z `sha256 -r /usr/bin/ssh` line (`6a9e5573…`, the same at 08:21Z), then `bootstrap.py check` (`5e467fe6…`, journal `/3`) **pass** | template `0a6062f0…`, filled plan `97c85173…` |
| 5. Guard argv | Derived from zq3-r10's, with `--runtime-sha256 bf86d805…` (checked against the package) and `--disposable-validation` exactly once, last. It equals `bootstrap.py`'s own `SshRemote.guard_argv()` for the filled plan, in shell string and argv, and passes `runtime-guard.py`'s `parse_args` and `check_command` | in [pins](pins.json) |
| 6. Observer, pod and staging pins | Unchanged script pins: `observer_key.py` `cd7f38c2…`, `observer.py` `2e1449dd…`, `installed.py` `c824b183…`, native guard `576d802a…`, `runtime-guard.py` `fece96b3…`, `fdescfs-window.py` `b00b19d1…`, `host.py` `738e1ca5…`, `host_snapshot.py` `b29ffed2…`, `pod_fixture.py` `ab4d269b…`, `argv.py` `ed63ff97…`. Re-pinned to main: `rocky_kernel.py` `5bef419e…`, **`guest-lock-probe.py` `11521fe6…` (y84)**. The observer `stage`/`enrol` lines pin the updated python3.9 `729ac37f…`; the pod template pins podman `249e918c…` and crun `659867c7…` (`derive-templates.py` checks all four runtime hashes against the facts). All 17 f3 and 18 Rocky files went into two deterministic tarballs; a second tar run gave the same hashes. A verification extract matched every expected hash (35) and the f3 modes (owner `0/0`, no parent entry) and was then deleted | f3 tar `cfe91626…` (new argv record; 07:31Z: `52f9ddb8…`), Rocky tar `adbdd63b…` (unchanged), pod template `4b6037d8…` |
| 7. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` at main (`89027fd9…`, as b94 left it) with `zq3-r1` → `zq3-r11`; `check_pins` passes. Synthetic fills, which are not pins: zq3-r8's real Phase 4 facts and host hashes (modules absent) **pass**, with paths renamed and the three artifact hashes swapped for the b94 ones. The same facts with the r84 artifact hashes zq3-r10 staged **refuse** (`pinned artifact differs or missing`). Post-restore facts with both modules loaded, the state on f3 now, **refuse** (`fdescfs module present`): hence Phase 1a | r11 pins `c74e605f…` |
| 8. Signing watcher | zq3-r10's `bin/sign_watch.py` (it signed both requests in zq3-r10) with `D`, the docstring run ID, the signer comment and **the signer pin `fc2ffcf4…`** changed. Tested locally with a fake signer: a wrong request hash, a wrong command, an unknown action, a repeat and a wrong signer hash were refused; both exact requests were signed | `2608ebd8…` |
| 9. Process observer check | `sh/p4-observer.py` from zq3-r10 with the run ID and **the daemon pin `bf86d805…`** changed (Phase 4 step 1a, **required**: new daemon bytes). zq3-r10's real records (renamed) are refused only for the r84 `sha256` and pass with `bf86d805…` substituted. The `$NG` line parses and passes the native guard's own `parse_args` and `check_request` | `fe08f30a…` |
| 10. Admission-step helper | **New** `sh/p10-admission.py`: per-case offsets and gaps of the y84 `admission-step` events in `ctrl-run/<case>/stdout.log`, `admission_ms` and `deadline_hit`. On zq3-r10's real T1 `stdout.log` it reports `identity_checked` false (pre-y84) and `deadline_hit` true; on a synthetic y84 log it reports each stage | `fb8cd99d…` |
| 11. Run sheet | [run-sheet.md](run-sheet.md): Phases 0-12, 1a (**runs**), 4/1a and U1-U3, S1 (resolved), the SELinux gates in Phases 1, 9 and 12, the R10-1/R10-2 checks, and a run log template. `session.sh` (`13c3d202…`) and thirteen pinned helper scripts (the twelve r10 ones, renamed, plus `p10-admission.py`); `p10-requests.py` re-pins the three r11 request templates. The Phase 8-10 lines were dry-run end to end (below) | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-18T00:00:00Z --format=gnu`.
The f3 tarball holds only `hfs-20261118-zq3r11-fdq` and
`hfs-validation-xz3/zq3-r11`. It has **no entry for the existing parent**
`hfs-validation-xz3/`, so extraction never rewrites the parent's mode or
owner. All directories are `0700`, binaries `0500`, other files `0400`,
owner `0/0`.

## Signing

Paul delegated both signatures (A7) to Claude. In Phase 8 the driver's
stdout goes through `bin/sign_watch.py` (`2608ebd8…`). The watcher runs the
signer (`fc2ffcf4…`) only if:
- the printed command is exactly one of the two below;
- the request file's SHA256 equals the printed `request_sha256`;
- each command runs at most once.

Anything else is refused, and the driver then times out (fails closed):

```
/home/paul/tmp/claude/hfs-zq3-r11-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r11-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r11-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r11-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r11-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r11-fill/bootstrap-run/work/signer-binding
```

On Paul's instruction, Claude places the operator key at the bundle's path
before the run. Nothing in the fill reads it.

## Run-time fills

| Fill | When and source | Used by |
|---|---|---|
| S1 | **Done**: S1-a restart at 08:21 UTC and this refill; Phase 1 rechecks that Rocky has not rebooted or upgraded since | Phase 1 Rocky gate, Phase 3 install |
| D1 | **D1-a, Phase 1a**: both modules are loaded (08:21Z); Phase 1 confirms it, Phase 1a unloads them normally (never `-f`), and a refused unload stops before staging | Phase 4 step 0 window plan fill (needs both absent) |
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 07:31Z fact, unchanged at 08:21Z); Phase 4 step 0 rechecks it (`F1-unchanged`), a change stops | bootstrap `observer.ssh_sha256` (plan `97c85173…`) |
| F2 fdescfs window plan and its hash (`WP`) | Phase 4 step 0: controller `facts`, host hashes, `realpath` library closure, f3 `date -u +%s`, then `fdescfs-window-plan.py --pins` with the r11 pins | setup, qualify, restore (`W`); Phase 5 recheck |
| F3 pod rootfs manifest digest (`PM`) | Phase 3: `pod_fixture.py manifest` on Rocky (zq3-r1 to zq3-r10: `f7f69339…`); `skip` if A8 is declined | `build`, T7 request |
| F3a process observer record | Phase 4 step 1a: guard stage `process-observer`, checked by `p4-observer.py` | go/no-go before setup; **pass required** (new daemon bytes) |
| F4 observer key pair, enrolled line, `after_sha256` (`EA`) | Phase 4 step 5: `ssh-keygen` on f3, `observer_key.py enrol` | runtime observer, Phase 11 `remove --expect-sha256`, banner-timeout check |
| F5 BLS entry ID | Phase 3 install record | `select` (from the state directory) |
| F6 boot ID, provider nonce, admission manifest SHA256, `virtioN` names, proposal, controller, provision hash | Phase 8 admission and runtime records | enrollment and bind requests (bootstrap fills them), controller batch plan |
| F7 m0/m1 mount IDs | Phase 8 mount steps (mountinfo) | batch plan, then `p10-requests.py` for the T6/T7/T8 requests |
| F8 batch plan and its hash (`B`) | Phase 8, after READY and `usable`: `fill_plan.py batch` (prints `cases_planned` and `deferred`), then `check-retained9p.sh check` | every controller and correlator call |
| F9 runtime UUID (`U`), `L`, cut, epoch and lifecycle sequences | READY record and `summary.json` via `p9-ready.py` (`--runtime`, `--observe-last`); Phase 9 uses L+1 and the T1 baseline L+2, then each from `correlate.py command` (`obs`, ARM, RESTART); expected L+1 to L+14 with T5 deferred | Phase 9 OBSERVE, T1-T4, T9-T11 |
| F10 exporter child PID (`XP`) | guard `receipt.json` `exporter.pid` (Phase 11 step 0; the `--no-child` line if null) | `host.py cleanup --child-pid` |
| F11 Rocky PID and lstart | **fixed** from the 08:21Z facts (`92402`, `Wed Nov 18 08:21:19 2026` UTC, the S1-a restart); rechecked in Phases 1, 4 (also by every `$NG` stage) and 5 | native guard host profile, Phase 5 recheck, `p5-mem.txt` RSS |
| F12 UMA drain decision | Phase 5 step 1a: `p5-estimate.py` `drain_recommended` | Phase 5 step 1b (or once in Phase 6) |
| F13 T6/T7/T8 request files | Phase 8 after the batch check: `p10-requests.py` (exclusive, `0600`, in `$E`) | T6, T7, T8 |
| F14 guard PID | Phase 11, only on the runtime-still-serving path (`pgrep -U root`) | the one SIGTERM to the guard |
| F15 SELinux state | Phase 1 (`/.autorelabel` absent, Enforcing), Phase 9 (guest `getenforce` `Enforcing`, enforce `1`), Phase 12 (relabel record) | Phase 1 and Phase 9 gates; Phase 12 finding |
| F16 984 timing | Phases 9-10: `tf3` lines in `$E/obs-timing.tsv` (three `ssh-noop`, each timed OBSERVE), checked with a local `cat` after the Phase 9 OBSERVE; `p9-timing.py` after T4 and before Phase 11 | run log only; never a gate |
| F17 admission-step timings | Phase 10: `p10-admission.py` over the four cases' `stdout.log` after T4 (`p10-admission-steps.jsonl`), or over a failed case's before Phase 11 | run log only; never a gate |

## Changes from the zq3-r10 sheet

All zq3-r7 to zq3-r10 rules are kept:
- **Phase 1a (D1-a): runs again**, as in zq3-r3 to zq3-r7, zq3-r9 and
  zq3-r10: a normal `kldunload fusefs`, then `kldunload fdescfs`, never `-f`;
- Phase 4 step 1a, the process observer check: **required**, because the
  daemon bytes changed (`bf86d805…`);
- the UMA drain decision in Phase 5;
- `rockyt` bounded guest unmounts (Phase 11 step 2 and T8);
- `pgrep -U root -lf runtime-guard.py` for the guard SIGTERM;
- the T10 abort paths to T8, then the guard-SIGTERM branch;
- the explicit `--no-child` line for a null exporter;
- observer key removal on every abort after Phase 4 step 5;
- the pause rule when f3 is unreachable;
- the single retry after a host-preflight-only failure;
- the Phase 0 recheck of the workstation `/usr/bin/ssh` (`16cb2c6a…`,
  unchanged at fill time);
- the Phase 9-10 helpers.

Kept from the zq3-r5 to zq3-r10 executions and the review decisions:
- **Phase 11 step 9 is one plain line with literal paths**:
  `f3 "doas -n rm -- /var/tmp/hfs-validation-xz3/zq3-r11/observer/id_ed25519 /var/tmp/hfs-validation-xz3/zq3-r11/observer/id_ed25519.pub"`.
  Every abort path points at that line.
- **The window restore's outcome is `restoration-summary.json`**, not an
  `rc` reported through an output filter (step 10, `"mount_restored": true`).
- **Every `rockyt … | tee` line ends with `; echo rc=${PIPESTATUS[0]}`**
  (T8, Phase 11 step 2).
- Phase 11 step 4a captures the guest's AVC records, step 4b the full guest
  `dmesg`. The vm-bhyve log decides whether a relabel happened, and the
  SELinux gates in Phases 1, 9 and 12 stay.
- The h64 errata: guest 9p checks count ` 9p ` mountinfo lines; Phase 11
  step 6 waits for `child-gone` only.
- The z64 T5 deferral (r9 reason), the Phase 1 check that `10.20.0.133` is
  on `vm-public`, the persisted-variable list (with `L`), the m64 block-line
  rule and the Phase 9 tuple gate.
- From zq3-r9 and zq3-r10: journal `/3` and the b84 `usable` gate, OBSERVE
  evidence `/2`, the L sequence shift, `p9-ready.py`/`p9-observe.py`, the
  `tf3` timing with the `obs-timing.tsv` capture check, the extended
  tool-timeout rule (controller lines, T6/T7/T8, Phase 3, U3 at least
  300 s; Phase 8 in the background), `printf %q` for `vars.env` and the
  `observe-p9-2.json` rerun name.

New for zq3-r11:
- **Candidate `3b128cc6…`** in the kernel plan, batch template, bootstrap
  plan, guard argv, window pins, staging, Phase 3, Phase 4 step 1a and
  Phase 8 expectations; the y84 `guest.py` and `guest-lock-probe.py`.
- **S1** (section and Phase 1 gate): the running Rocky kernel must equal the
  grub default and the plan's stock default (resolved by the S1-a restart;
  the nightly 00:55 upgrade must not run before the run).
- **A new "Execution notes from the zq3-r10 run" section** (R10-1, R10-2,
  the milestones and what is kept).
- **Phase 10 R10-1 check:** `p10-admission.py` after T4 records each case's
  admission time and gaps; on a case failure the sheet captures the
  admission steps, the guest block lines and the live owner snapshots before
  Phase 11. A `Deadline` in admission or a `DESCRIPTION_RELEASE … (-11)`
  block line is a finding against y84/x84.
- **R10-2 check:** T8 records the guest block lines before and after its
  unmount; a new `failed provider stop` line is a finding against z84. The
  same holds for a Phase 11 step 2 unmount of an unblocked session (step
  4b's `dmesg`).
- **Timing note:** one hfs syscall can now take about 60 s (30 s fence wait
  plus the 30 s request window) and an unmount about 2 s more; guest.py's
  10 s admission alarm still bounds admission (zq3-r10's T1 admission took
  16.9 s), so a slow admission ends as a clean `Deadline` and is recorded.

## Expectations: what changed since zq3-r10

zq3-r10 passed the `usable` gate and Phase 9 for the first time, gave the
first 984 samples (SSH no-op median 0.198 s, OBSERVE net 0.605 s and
0.434 s) and held T8. It then failed twice:
- **R10-1:** T1's admission exceeded guest.py's 10 s alarm; the fd leaked
  past the `Deadline`, and the exit-time release met a pending allocation,
  failed `-EAGAIN` locally and blocked m0.
- **R10-2:** T8's clean unmount logged `failed provider stop on STOP (-16)`.

b94 changes:
- **y84 (guest.py):** SIGALRM is blocked across every open and its
  registration, so no fd outlives a deadline; admission emits
  `admission-step` events;
- **x84 (guest kernel):** a request refused only by a pending allocation
  waits (FIFO, bounded) for the fence;
- **194 (daemon):** ALLOCATE accepts an exited issuer's exact original
  cleanup;
- **z84 (guest kernel):** the unmount stop check runs after teardown and a
  bounded settle and accepts an idle once-reopened LIVE view.

So T1-T4 should pass, a normal unmount should log no block line, and T6,
T7 and T9-T11 (unrun in zq3-r8 to zq3-r10) should run.

## Phases 9-10 recheck

Since zq3-r10, among the tools the sheet uses, `controller.py`
(`b1671cf1…`, admits only `3b128cc6…`), `rocky_kernel.py` (`5bef419e…`),
`guest.py` (`d8361fc0…`) and `guest-lock-probe.py` (`11521fe6…`) changed.
`guest.py` keeps its request and `stdout.log` contract and only adds
`admission-step` events, which the controller and correlator ignore.
`bootstrap.py` (`5e467fe6…`), `bootstrap_remote.py` (`0dcb0fe0…`),
`fill_plan.py` (`893350cf…`) and `correlate.py` (`9cd55f2c…`) are
unchanged. No CLI the sheet uses changed shape.

**Sheet dry run** (`dryrun/` in the private fill; scratch output). A
harness extracted the 77 command lines of this sheet from Phase 8 "After
READY" to T8, plus the Phase 7 `k04` baseline line and the Phase 11 984
summary line (79 in all), from `run-sheet.md` itself and ran them in order.
The new lines are the `p10-admission.py` line after T4 and the two T8 block
line captures. The case-failure block, which names `<case>`, only runs on a
failure and is skipped. The harness used:
- the real `fill_plan.py batch`, `check-retained9p.sh check`,
  `correlate.py`, `session.sh` (`tf3`, `obs`, `sha`, `jf`) and the `sh/`
  helpers;
- `controller.py` with faked guest dispatch, whose case output now carries
  y84 `admission-step` events before `admitted`;
- stub `f3`/`rocky` that answer like the lifecycle client (evidence `/2`
  with a claim summary), `host_snapshot.py` and the guest;
- a synthetic READY run with journal `/3`, an `ok` `usable` result, L = 5
  and mountinfo rows with the exact g64 options.

Every line passed with the expected results:
- L = 5; Phase 9 at 6 (L+1); case pairs 7/8, 9/10, 11/12, 13/14; T9 15,
  ARM 16, post-CALL 17; T10 18, RESTART 19;
- the capture check printed three `ssh-noop` lines and the OBSERVE at 6;
- the batch fill and check printing `cases_planned` (four) and the deferral;
- T1-T4 correlated and released (the controller accepted the
  `admission-step` events); `syscall_batch_success` True with the deferral
  in the state;
- `p10-admission.py` summarised all four cases (`admission_ms` 2000,
  `deadline_hit` false);
- T9 `dropped-reply-effect-committed-and-visible`;
- T10 FENCED;
- T11 `k04_refusal_observed` true, with `deferred` repeated;
- the T6/T8 lock requests from `p10-requests.py`;
- T8 `rc=0`;
- `p9-timing.py` summarised 12 timed OBSERVEs and 3 no-ops.

**Negative and real-record checks** (`helpers-dryrun.py`: 31 cases, all as
expected):
- `p9-ready.py` refused a wrong socket, a missing or failed `usable`
  result, a `/2` journal header, a missing `observe_sequence_last`, L = 1,
  and a `usable`/summary L mismatch;
- `p9-observe.py` refused sequence 1, L+2, a READY_JSON without L, a fenced
  admission, a rotated cut, an incomplete OBSERVE, evidence `/1` and a wrong
  plan hash;
- `p9-timing.py` refused a malformed TSV and one without an OBSERVE line;
- `p10-argv.py` and `p10-requests.py` refused as in r10;
- `fill_plan.py batch` refused a synthetic run that stopped at `usable`
  with the b84 claims note (`bootstrap steps did not end ok:
  usable=failed`), a `/2` journal and zq3-r10's real run (`journal is not
  for the pinned bootstrap plan`); `p9-ready.py` refused the failed run and
  zq3-r10's real run directory (its control socket is zq3-r10's).

## Findings and open questions

- **Q0 S1 (resolved by S1-a).** The nightly 00:55 `dnf -y upgrade` must
  not run between this fill and the run; if it does (or Rocky reboots), the
  Phase 1 gate stops and the Rocky-dependent pins need another refill.
  Rocky's first boot of `741.22.1.el9_9` was the S1-a restart; Phase 12
  boots it again.
- **Q1 T5 deferred.** The run cannot complete zq3 acceptance. T5 needs a
  re-plan and sizing first.
- **Q2 the b94 fixes are untested on f3.** The kernel `c4e08f6f…` and the
  daemon `bf86d805…` passed offline tests only. The main questions are
  Phase 4 step 1a, T1-T4 admission inside the 10 s alarm, and a clean T8
  unmount.
- **Q3 admission time.** zq3-r10's T1 admission took 16.9 s, over the 10 s
  alarm. b94 makes that end cleanly, but does not shorten it: if recalls
  still delay a root open by several seconds, T1 can still hit the alarm
  (now as a `Deadline` failure without a blocked session). `p10-admission.py`
  will show where the time goes; the 094 model task has no lab number yet.
- **Q4 memory.** At 08:21Z, a minute after Rocky's start, free was 10.38 GB,
  inactive 0.31 GB and ARC 3.98 GB. Phase 5's estimate and the Phase 6 bar
  decide (zq3-r10 margin 0.57 GB after the drain).
- **Q5 UEFI vars drift.** The record pins `dc5e2923…`. Any Rocky boot before
  Phase 7 changes it, and `host.py setup` then refuses. Recheck right before
  the run.
- **Q6 Phase 1a.** zq3-r3 to zq3-r7, zq3-r9 and zq3-r10 unloaded both
  modules without refusal. A refusal now stops before staging and needs a
  plan revision.
- **Q7 window pins file:** the r11 window pins live only in the private fill,
  with their hash recorded here.
- **Q8 T6, T7, T9-T11:** unrun in zq3-r8 to zq3-r10; the T9 ARM, T10
  FENCED step 2 and the T11 receipt wait (5 minutes) have only been dry-run
  on the current tools.

No committed tool refused real inputs. These intended refusals all
occurred:
- the controller template;
- the unfilled bootstrap template;
- the superseded r84 `4712e479` candidate in a synthetic batch plan and in
  `rocky_kernel.py plan`;
- an undeclared four-case batch and a deferred `credentials`;
- the window filler on the r84 artifact hashes and on facts with both
  modules loaded.

## Local verification

At `e21528c` in the main checkout, all passing:
- retained9p: 372 tests;
- directlaunch: 26 tests;
- `tools/lab` top level: 248 tests (3 skipped);
- `go test -count=1 ./internal/runtimeprep`.

`shellcheck` is clean for `build-stage.sh` and `sh/*.sh` (as POSIX sh), and
for `session.sh` apart from SC2034/SC2086: its variables are used by the
sheet, and `$O` is split on purpose. No code was changed.
