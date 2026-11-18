# zq3-r12 filled run inputs (2026-11-18)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r11 fill](../20261118-zq3-r11-filled-plan/README.md) ·
[zq3-r11 record](../20261118-zq3-r11/README.md) ·
[5a4 candidate](../../../../tools/integration/artifacts/evidence/xz3-20261118-r12/README.md)

These are the exact run inputs for `zq3-r12`, the twelfth attempt of zq3.
They were generated on the workstation with the committed tools at `main`
`637dc11` (after 5a4). They mirror the
[zq3-r11 fill](../20261118-zq3-r11-filled-plan/README.md), which ran for
real: Phase 1a unloaded both modules, READY and the `usable` gate passed,
Phase 9 passed, then T1 failed (R11-1), T7 was skipped (pod NEVRA pins), T8's
unmount hung (R11-2), and Phases 11 and 12 ran cleanly. The changes are:
- the run ID (`zq3-r12`) and the native guard root
  `/var/tmp/hfs-20261118-zq3r12-fdq`;
- the 5a4 candidate `150deb29…`, which rebuilt the guest kernel (1a4; build
  ID `8c1de1ed…`) and the daemon (`e0981a0a…`; y94); the config, the five
  modules and the exporter are b94's bytes;
- `hfs-runtime-prep` and the signer rebuilt;
- everything that pins them, re-pinned, and the kernel plan regenerated;
- the bundle regenerated from the unchanged committed spec with the new code;
- **T7 runs again:** the e94 `pod_fixture.py` is re-pinned, and the pod
  template is `homelabfs-retained9p-pod/2` with `runtime_nevra` from the facts;
- **Phase 1a (D1-a) runs again:** zq3-r11 left `fdescfs.ko` and `fusefs.ko`
  loaded;
- a Phase 1 pod-runtime recheck (N1, Rocky's nightly upgrade);
- the R11-1/R11-2 checks: `p10-admission.py` flags `ETIMEDOUT`, every guest
  block-line capture also greps for `view control worker stopped` and
  `timed out behind a recall`, and T8's lines run strictly one at a time.

Nothing here used SSH, the network, the lab, a VM, a mount, an installation
or the operator private key. **No authority follows from these files:**
every class A step in the [run sheet](run-sheet.md) still needs Paul's
explicit final GO, including the Phase 1a unload and T7 (A8). **zq3
acceptance stays incomplete by design**, because T5 does not run.

## N1: Rocky upgrades itself nightly

**Run-time revision R1 (2026-11-18, Phase 1).** The UTC times below are
wrong: Rocky's time zone is `Europe/Sofia (EET, +0200)` (the fill read
`date -u` output), so the nightly upgrade runs at 00:55 EET = **22:55 UTC**,
once per local day (stamp `/var/lib/unattended-upgrade/last-daily`), and
never reboots Rocky. The run revised N1, the Phase 1 expectation and the
Phase 5 step 0 gate to compute in `Europe/Sofia` (latest Phase 5 start
21:25 UTC); [run-sheet.md](run-sheet.md) is the revised sheet the run used.
Outcome: [zq3-r12 record](../20261118-zq3-r12/README.md).

Rocky runs `dnf -y upgrade` at 00:55 Rocky-local, which is **00:55 UTC**
(Rocky's clock is UTC; Phase 1 now records `date -u` and `timedatectl`).
**The run must start and finish before the next one.** A go/no-go gate in
Phase 5 step 0, before Rocky is stopped, enforces it: `STOP` if Phase 1
predates the last 00:55 UTC, within 35 minutes after 00:55 UTC, or later
than 23:25 UTC (00:55 minus the 90 minutes Phases 5-12 may take); a `STOP`
aborts cleanly (observer key removed on Rocky and f3, U2, window
restored). If it does not, Phase 1 must recheck the
pod-runtime NEVRAs and SHA256s (`p1-pod-runtime.txt`, compared with the pod
template) and the kernel facts (boot ID, running and default kernel,
`next_entry`), and any drift means a refill. The 13:50:55Z facts match
zq3-r11's on every Rocky pin (kernel `741.22.1.el9_9`, python3.9
`729ac37f…`, podman, crun, conmon, seccomp.json, `authorized_keys`).

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-zq3-r12-fill` (`0700`). This directory holds only
public hashes, paths, argv and the read-only facts. These stay private and
are never committed:
- the per-session keys;
- the runtime config, which holds host receipt private keys;
- the staging tarballs that contain it.

## Inputs

| Input | Value |
|---|---|
| Candidate | **5a4 rebuild** (task 5a4): manifest `150deb29…`, package `/home/paul/tmp/claude/hfs-xz3-20261118-r12/package`, source `1b0a306` (repository baseline). **Guest kernel rebuilt (1a4):** `6.1.190-hfs-xz3`, bzImage `803a35b9…`, GNU build ID `8c1de1ed…`, System.map `5989d2a5…`; config `28fe4a8e…`, the five modules and `modules.*` byte-identical to `3b128cc6`. **Daemon `e0981a0a…`** (GNU build ID `f6538eb3…`; y94 on top of b94). **Reused byte for byte:** the native exporter `ef8f17de…` (`argv.py` `ed63ff97…` unchanged). Window artifacts `dafcc121…` (test) and `c05b2e24…` (helper), rebuilt from `1b0a306`. Init helper `8c02b98f…` unchanged. **Lab tools:** `pod_fixture.py` `01f8e842…` (e94); `guest.py` `d8361fc0…` and `guest-lock-probe.py` `11521fe6…` unchanged (`git diff b60e4ee 637dc11 -- tools/lab` touches only `controller.py`, `rocky_kernel.py`, `pod_fixture.py`, their tests, the window pins/build script and the README) |
| Phase 1 facts | [facts/](facts/), collected read-only 2026-11-18 13:50:55Z; they supersede zq3-r11's 08:21:45Z set. **f3:** boot `1794574308` (unchanged since zq3-r8), **`fdescfs.ko` (id 28) and `fusefs.ko` (id 29) loaded**, no fuse/fdesc mount, free 9.91 GB + inactive 1.23 GB, ARC 4.18 GB, `/usr/bin/ssh` `6a9e5573…`, python3.12 `a66eb370…`, `10.20.0.133` on `vm-public` (members `tap0`, `re0`). **Rocky:** PID `3146` (UTC lstart `Wed Nov 18 09:01:00 2026`, zq3-r11's Phase 12 `start-rocky`), boot ID `c8f80bb2-…`, running stock `5.14.0-741.22.1.el9_9` = grub default, `next_entry` empty, `boot_indeterminate=7`, SELinux Enforcing, `/.autorelabel` absent, no `hfs-xz3` file in `/boot` (zq3-r11's U2), `/boot` 328 MB free, python3.9 `729ac37f…`, `authorized_keys` `e08542dc…`. **Pod runtime** (`rocky-pod-runtime-…txt`): podman `249e918c…` `podman-6:5.8.5-3.el9_9.x86_64`, crun `659867c7…` `crun-1.29.1-1.el9.x86_64`, conmon `1ac2a8db…` (`conmon-3:2.2.1-2.el9_8`), seccomp.json `7dd44119…` (`containers-common-5:5.8-2.el9`). The published files are the f3 Phase 1 batch, the network/modules extract, the launch facts and the pod-runtime batch; they hold no key material (grep for key headers and `ssh-*` lines) |
| Host events since zq3-r11 | zq3-r11's own Rocky stop (08:38:38) and normal start (09:01:00 UTC, no relabel reboot, U2 done). No nightly upgrade since zq3-r11's facts. No f3 reboot since Paul's deliberate 2026-11-13 reboot |
| Launch facts | `f3-launch-facts-20261118T135055Z.json` (`54b91b5e…`): `rocky.conf` `c101adf1…`, vm-run `43753bc8…`, firmware `6b48e967…` unchanged; **UEFI vars `2dd5b1ea…`** (zq3-r11: `dc5e2923…`); launch logged `2026-11-18T11:01:00+02:00` |
| Tap | `tap100` (bridge members `re0`, `tap0`) |
| Run ID / staging | `zq3-r12`, `/var/tmp/hfs-validation-xz3/zq3-r12` (the parent exists and holds zq3-r1 to zq3-r11) |
| Native guard root | `/var/tmp/hfs-20261118-zq3r12-fdq`, host profile `--rocky-pid 3146 --rocky-birth 'Wed Nov 18 09:01:00 2026'`, guard `576d802a…` (unchanged) |
| Window directory | `/var/tmp/hfs-fdescfs-windows/zq3-r12` (the parent exists, `root:wheel 0700`) |
| Rocky names | `/root/hfs-zq3-zq3-r12-*`, `/var/tmp/hfs-zq3-zq3-r12` (the zq3-r1 to zq3-r11 ones stay as evidence) |
| Operator public key | `2f6d909c…`; key path `…/hfs-zq3-r12-fill/operator/operator-signing-key` (empty `0700` directory, never read here) |

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | **Rebuilt** at `637dc11`: `hfs-runtime-prep` and the signer `hfs-retained9p-operator`. The build used `-trimpath`, CGO, VCS stamps and an unmodified private clone (`vcs.modified=false`); a second build of each, forced with `-a`, gave the same bytes. Reason: `git diff e21528c 637dc11` over the `go list -deps` directories of both commands (the same list as r11) lists non-test changes only in `internal/viewcontrol` (`ack_reply.go`, `recall.go`: y94) and a README. `go.mod`/`go.sum` are unchanged. zq3-r11's builds (prep `d6fe51ae…`, signer `fc2ffcf4…`) are retired. G3 init helper: zq3-r1's reproduced build | prep `61a37af2…`, signer `a7f1f847…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from `testdata/zq3-spec.json` at `637dc11` (`make-spec.py`: only `zq3-r1` → `zq3-r12` and the r12 operator key path change; the same method reproduces zq3-r11's spec from the same testdata byte for byte). The committed spec is unchanged since r9 | spec `f1626a98…`, request template `22643e9a…`, runtime config `19f744f2…`, volume-init `e92f6455…`; volume `47566f1d-…`; sessions hfs0 `f12c0721-…`, hfs1 `d508c884-…` |
| 1. Argv | `argv.py generate` from the 13:50:55Z launch facts (sessions, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `fc6df445…`, args `5986ee1c…`, vars `2dd5b1ea…` |
| 2. Kernel plan | `rocky_kernel.py plan` at main (`b69c7457…`; admits only `150deb29…`), stock default `/boot/vmlinuz-5.14.0-741.22.1.el9_9.x86_64` (the fresh grub default, which Rocky runs). Against zq3-r11's it differs only in `run_id`, `manifest_sha256`, the bzImage (hash, size, build ID) and System.map. The b94 package `3b128cc6…` is refused as superseded ("use the 5a4 rebuild") | plan `5fae1a77…`, build ID `8c1de1ed…` |
| 3. Controller batch | Template derived from zq3-r11's by `derive-templates.py`: run ID, fill path, sessions, candidate `3b128cc6…` → `150deb29…`; probe `guest.py` `d8361fc0…` unchanged. **T5 stays deferred** with the r9 reason (unchanged). The four other requests keep canonical order. `controller.py` `989842f9…`. `check` refuses the template as designed (`probe/installed-manifest pin`). A synthetic copy with zero IDs **passes** and prints `cases_planned` (four) and the deferral. The same copy is **refused** with the b94 candidate (superseded), without the declaration ("five-case syscall batch minus declared deferrals") and with `credentials` also deferred ("case may not be deferred") | template `7d3b1c52…` |
| 4. Bootstrap plan | Template derived from zq3-r11's (the one that ran). New pins: bundle, kernel plan, `rocky_kernel.py`, the y94 daemon `e0981a0a…` and the rebuilt prep `61a37af2…`. Unchanged: `bootstrap_remote.py` `0dcb0fe0…` (`remote_sha256`), the guard pin `fece96b3…`. The script re-hashes every pinned file at main and in the package. The unfilled template refuses as designed (`observer.ssh_sha256: SHA256 hex`). **F1 filled now** with `fill_plan.py bootstrap` (`893350cf…`) from the 13:50Z `sha256 -r /usr/bin/ssh` line (`6a9e5573…`), then `bootstrap.py check` (`5e467fe6…`, journal `/3`) **pass** | template `154429fe…`, filled plan `201842ae…` |
| 5. Guard argv | Derived from zq3-r11's, with `--runtime-sha256 e0981a0a…` (checked against the package) and `--disposable-validation` exactly once, last. It equals `bootstrap.py`'s own `SshRemote.guard_argv()` for the filled plan, in shell string and argv, and passes `runtime-guard.py`'s `parse_args` and `check_command` | `90d37d69…`, in [pins](pins.json) |
| 6. Pod template (e94) | `derive-templates.py` moves the pod template to `homelabfs-retained9p-pod/2`, re-pins `fixture_sha256` to `pod_fixture.py` `01f8e842…` and adds **`runtime_nevra`** `{"podman": "podman-6:5.8.5-3.el9_9.x86_64", "crun": "crun-1.29.1-1.el9.x86_64"}`, exactly as `rpm -qf --qf '%{NEVRA}\n'` printed them (the facts' `== pod-runtime` section). The script compares `runtime_nevra` and the four `runtime_sha256` values with that section, cross-checks the hashes with the Phase 1 `sha256sum` lines, and runs the committed `pod_fixture.py`'s own `check_nevra_pins` and `parse_request` (synthetic fill): **both accept** the epoch form. `parse_request` refuses the r11 `pod/1` template (`request lacks runtime_nevra`), version `/1` with `runtime_nevra`, swapped NEVRAs, a `podman-docker` sibling, an extra conmon NEVRA and the stale r11 fixture pin | pod template `b7b6c20f…` |
| 7. Observer and staging pins | Unchanged script pins: `observer_key.py` `cd7f38c2…`, `observer.py` `2e1449dd…`, `installed.py` `c824b183…`, native guard `576d802a…`, `runtime-guard.py` `fece96b3…`, `fdescfs-window.py` `b00b19d1…`, `host.py` `738e1ca5…`, `host_snapshot.py` `b29ffed2…`, `argv.py` `ed63ff97…`, `guest-lock-probe.py` `11521fe6…`. Re-pinned to main: `rocky_kernel.py` `b69c7457…`, **`pod_fixture.py` `01f8e842…` (e94)**. All 17 f3 and 18 Rocky files went into two deterministic tarballs; a second tar run gave the same hashes. A verification extract matched every expected hash (35) and the f3 modes (owner `0/0`, no parent entry) and was then deleted | f3 tar `139af277…`, Rocky tar `b37c5d6f…` |
| 8. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` at main (`90845252…`, as 5a4 left it) with `zq3-r1` → `zq3-r12`; `check_pins` passes. Synthetic fills, which are not pins: zq3-r8's real Phase 4 facts and host hashes (modules absent) **pass**, with paths renamed and the three artifact hashes swapped for the 5a4 ones. The same facts with the b94 artifact hashes zq3-r11 staged **refuse** (`pinned artifact differs or missing`). Post-restore facts with both modules loaded, the state on f3 now, **refuse** (`fdescfs module present`): hence Phase 1a | r12 pins `da22551d…` |
| 9. Signing watcher | zq3-r11's `bin/sign_watch.py` (it signed both requests in zq3-r11) with `D`, the docstring run ID, the signer comment and **the signer pin `a7f1f847…`** changed. Tested locally with a fake signer: a wrong request hash, a wrong command, an unknown action, a repeat and a wrong signer hash were refused; both exact requests were signed | `90bde567…` |
| 10. Process observer check | `sh/p4-observer.py` from zq3-r11 with the run ID and **the daemon pin `e0981a0a…`** changed (Phase 4 step 1a, **required**: new daemon bytes). zq3-r11's real records (renamed) are refused only for the b94 `sha256` and pass with `e0981a0a…` substituted. The `$NG` line parses and passes the native guard's own `parse_args` and `check_request` | `173e4da6…` |
| 11. Admission-step helper | `sh/p10-admission.py` from zq3-r11, with `errno` in `failure` and a new **`etimedout`** flag (1a4): errno 110 in the failure, in any `reply` event or in any `errno`/`*_errno` field (lifecycle observations, T6-T8 probe reports), listed in `etimedout_at` (review round 1). On zq3-r11's real T1 `stdout.log` it reports `deadline_hit` true and the 43.6 s `open-root` → `failure` gap; seven synthetic cases (below) behave as expected | `85c5919f…` |
| 12. Run sheet | [run-sheet.md](run-sheet.md): Phases 0-12, 1a (**runs**), 4/1a and U1-U3, N1, the SELinux gates in Phases 1, 9 and 12, the R11-1/R11-2 checks, T7 back in, and a run log template. `session.sh` (`be80a19e…`, `rockyt` bound 270 s) and thirteen pinned helper scripts (the r11 ones, renamed; `p4-observer.py` and `p10-admission.py` changed); `p10-requests.py` re-pins the three r12 request templates. The Phase 8-10 lines were dry-run end to end (below) | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-18T00:00:00Z --format=gnu`.
The f3 tarball holds only `hfs-20261118-zq3r12-fdq` and
`hfs-validation-xz3/zq3-r12`. It has **no entry for the existing parent**
`hfs-validation-xz3/`, so extraction never rewrites the parent's mode or
owner. All directories are `0700`, binaries `0500`, other files `0400`,
owner `0/0`.

## Signing

Paul delegated both signatures (A7) to Claude. In Phase 8 the driver's
stdout goes through `bin/sign_watch.py` (`90bde567…`). The watcher runs the
signer (`a7f1f847…`) only if:
- the printed command is exactly one of the two below;
- the request file's SHA256 equals the printed `request_sha256`;
- each command runs at most once.

Anything else is refused, and the driver then times out (fails closed):

```
/home/paul/tmp/claude/hfs-zq3-r12-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r12-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r12-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r12-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r12-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r12-fill/bootstrap-run/work/signer-binding
```

On Paul's instruction, Claude places the operator key at the bundle's path
before the run. Nothing in the fill reads it.

## Run-time fills

| Fill | When and source | Used by |
|---|---|---|
| N1 | Phase 1: boot ID, running/default kernel, `next_entry`, python3.9, `authorized_keys` and the pod runtime (`p1-pod-runtime.txt`, `pod-runtime-unchanged: True`); any drift stops for a refill | Phase 1 Rocky gate, Phase 3 install, T7 |
| D1 | **D1-a, Phase 1a**: both modules are loaded (13:50Z); Phase 1 confirms it, Phase 1a unloads them normally (never `-f`), and a refused unload stops before staging | Phase 4 step 0 window plan fill (needs both absent) |
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 13:50Z fact); Phase 4 step 0 rechecks it (`F1-unchanged`), a change stops | bootstrap `observer.ssh_sha256` (plan `201842ae…`) |
| F2 fdescfs window plan and its hash (`WP`) | Phase 4 step 0: controller `facts`, host hashes, `realpath` library closure, f3 `date -u +%s`, then `fdescfs-window-plan.py --pins` with the r12 pins | setup, qualify, restore (`W`); Phase 5 recheck |
| F3 pod rootfs manifest digest (`PM`) | Phase 3: `pod_fixture.py manifest` on Rocky (zq3-r1 to zq3-r10: `f7f69339…`; the EL9.9 python3.9 may change it); `skip` only if A8 is declined | `build`, T7 request |
| F3a process observer record | Phase 4 step 1a: guard stage `process-observer`, checked by `p4-observer.py` | go/no-go before setup; **pass required** (new daemon bytes) |
| F4 observer key pair, enrolled line, `after_sha256` (`EA`) | Phase 4 step 5: `ssh-keygen` on f3, `observer_key.py enrol` | runtime observer, Phase 11 `remove --expect-sha256`, banner-timeout check |
| F5 BLS entry ID | Phase 3 install record | `select` (from the state directory) |
| F6 boot ID, provider nonce, admission manifest SHA256, `virtioN` names, proposal, controller, provision hash | Phase 8 admission and runtime records | enrollment and bind requests (bootstrap fills them), controller batch plan |
| F7 m0/m1 mount IDs | Phase 8 mount steps (mountinfo) | batch plan, then `p10-requests.py` for the T6/T7/T8 requests |
| F8 batch plan and its hash (`B`) | Phase 8, after READY and `usable`: `fill_plan.py batch` (prints `cases_planned` and `deferred`), then `check-retained9p.sh check` | every controller and correlator call |
| F9 runtime UUID (`U`), `L`, cut, epoch and lifecycle sequences | READY record and `summary.json` via `p9-ready.py` (`--runtime`, `--observe-last`); Phase 9 uses L+1 and the T1 baseline L+2, then each from `correlate.py command` (`obs`, ARM, RESTART); expected L+1 to L+14 with T5 deferred | Phase 9 OBSERVE, T1-T4, T9-T11 |
| F10 exporter child PID (`XP`) | guard `receipt.json` `exporter.pid` (Phase 11 step 0; the `--no-child` line if null) | `host.py cleanup --child-pid` |
| F11 Rocky PID and lstart | **fixed** from the 13:50Z facts (`3146`, `Wed Nov 18 09:01:00 2026` UTC, zq3-r11's Phase 12); rechecked in Phases 1, 4 (also by every `$NG` stage) and 5 | native guard host profile, Phase 5 recheck, `p5-mem.txt` RSS |
| F12 UMA drain decision | Phase 5 step 1a: `p5-estimate.py` `drain_recommended` | Phase 5 step 1b (or once in Phase 6) |
| F13 T6/T7/T8 request files | Phase 8 after the batch check: `p10-requests.py` (exclusive, `0600`, in `$E`); the T7 request is pod `/2` with `runtime_nevra` | T6, T7, T8 |
| F14 guard PID | Phase 11, only on the runtime-still-serving path (`pgrep -U root`) | the one SIGTERM to the guard |
| F15 SELinux state | Phase 1 (`/.autorelabel` absent, Enforcing), Phase 9 (guest `getenforce` `Enforcing`, enforce `1`), Phase 12 (relabel record) | Phase 1 and Phase 9 gates; Phase 12 finding |
| F16 984 timing | Phases 9-10: `tf3` lines in `$E/obs-timing.tsv` (three `ssh-noop`, each timed OBSERVE), checked with a local `cat` after the Phase 9 OBSERVE; `p9-timing.py` after T4 and before Phase 11 | run log only; never a gate |
| F17 admission-step timings | Phase 10: `p10-admission.py` over the four cases' `stdout.log` after T4 (`p10-admission-steps.jsonl`; `deadline_hit`, `etimedout`), or over a failed case's before Phase 11 | run log only; never a gate |

## Changes from the zq3-r11 sheet

All zq3-r7 to zq3-r11 rules are kept:
- **Phase 1a (D1-a): runs again**, as in zq3-r3 to zq3-r7 and zq3-r9 to
  zq3-r11: a normal `kldunload fusefs`, then `kldunload fdescfs`, never `-f`;
- Phase 4 step 1a, the process observer check: **required**, because the
  daemon bytes changed (`e0981a0a…`);
- the UMA drain decision in Phase 5;
- `rockyt` bounded guest unmounts (Phase 11 step 2 and T8; now 270 s);
- `pgrep -U root -lf runtime-guard.py` for the guard SIGTERM;
- the T10 abort paths to T8, then the guard-SIGTERM branch;
- the explicit `--no-child` line for a null exporter;
- observer key removal on every abort after Phase 4 step 5;
- the pause rule when f3 is unreachable;
- the single retry after a host-preflight-only failure;
- the Phase 0 recheck of the workstation `/usr/bin/ssh` (`16cb2c6a…`,
  unchanged at fill time);
- the Phase 9-10 helpers.

Kept from the zq3-r5 to zq3-r11 executions and the review decisions:
- **Phase 11 step 9 is one plain line with literal paths**:
  `f3 "doas -n rm -- /var/tmp/hfs-validation-xz3/zq3-r12/observer/id_ed25519 /var/tmp/hfs-validation-xz3/zq3-r12/observer/id_ed25519.pub"`.
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
  on `vm-public`, the persisted-variable list (`XP`, `ARM`, `RST`,
  `GUARD_PID`, `L`, `PM`, every `VAR=` appended to `vars.env` with
  `printf %q`), the m64 block-line rule and the Phase 9 tuple gate.
- From zq3-r9 to zq3-r11: journal `/3` and the b84 `usable` gate, OBSERVE
  evidence `/2`, the L sequence shift, `p9-ready.py`/`p9-observe.py`, the
  `tf3` 984 timing with the `obs-timing.tsv` capture check, the extended
  tool-timeout rule (controller lines, T6/T7/T8, Phase 3, U3 at least
  300 s; Phase 8 in the background), the `observe-p9-2.json` rerun name,
  the `admission-step` timings and the case-failure capture with live owner
  snapshots.

New for zq3-r12:
- **Candidate `150deb29…`** in the kernel plan, batch template, bootstrap
  plan, guard argv, window pins, staging, Phase 3, Phase 4 step 1a and
  Phase 8 expectations.
- **T7 back in** (e94): pod template `/2` with `runtime_nevra`, the Phase 3
  pod lines run, and T7's expectation names the `check_runtime` refusal on
  drift.
- **N1** (section and Phase 1 gate): `p1-pod-runtime.txt` and a local
  `pod-runtime-unchanged` comparison with the pod template.
- **A new "Execution notes from the zq3-r11 run" section** (R11-1, R11-2,
  the operator slip, the milestones and what is kept).
- **The r11 lesson as a rule:** never chain a follow-up after a bounded
  command; T8's lock probe and `ls` run only after the unmount printed
  `rc=0`, otherwise a hang capture (`ps … wchan`, live owner snapshot) and
  T8 unrun.
- **R11-1 check** in Phase 10 (replaces the R10-1 check): z94 recalls show
  as `open-root` gaps; `deadline_hit`, `etimedout`, `view control worker
  stopped` and `dispatching-uncertain` are findings.
- **1a4 findings:** every guest block-line grep (Phase 9, the case-failure
  block, T8, Phase 11 step 2) also matches `view control worker stopped`
  and `timed out behind a recall`.

## Expectations: what changed since zq3-r11

zq3-r11 passed `usable` and Phase 9 again (984 OBSERVE net 0.466 s and
0.483 s); R10-1 and R10-2 did not recur. It then failed twice:
- **R11-1:** T1's read-only root OPEN, under the caller's own observation
  lease, started an all-domain recall (z94); the host answered m0's early
  DRAINED STALE, the guest stopped its view worker and blocked m0, and the
  recall waited in WAIT_DRAINED forever (`dispatching-uncertain` after
  44.7 s).
- **R11-2:** T8's unmount of m1 stayed in D in `hfs_view_enter` until
  `rockyt`'s 150 s bound; the guest poweroff took about 530 s.

5a4 changes:
- **y94 (daemon):** an early DRAINED is answered BLOCKED and ACCEPTED once
  the peer's INSTALLED_DRAIN arrives, so an all-domain recall completes;
- **1a4 (guest kernel):** `hfs_view_enter` gives up after 30 s with
  `ETIMEDOUT`; timed-out or finally refused mmap faults get SIGBUS; a
  stopping view worker logs session, phase, ACK and host status.

So T1-T4 should pass, and T6, T7 and T9-T11 (never run for real) should
run. Remaining known risks, not fixed: **z94** (expect self-started
all-domain recalls during admission, now survivable) and **0a4** (the host
never drops a participant that stops acknowledging). Any `ETIMEDOUT`, any
`view control worker stopped` line and any `initial attributes timed out
behind a recall` line is recorded as a finding.

**Timing check against the 90-120 s getattr worst case**
(`docs/Test-automation.md`; 1a4 view wait, lease exchange, x84 allocation
wait and request window, each up to 30 s):
- guest.py's admission alarm (10 s) and case alarm (the plan's 60 s) are
  shorter. SIGALRM does not interrupt the hfs wait, so a slow syscall ends
  as a `Deadline` once it returns (y84 keeps the fd registered);
- the controller's dispatch bound (min(60 + 15, 120) = 75 s) is shorter
  too, so a long wait ends `dispatching-uncertain`, as in zq3-r10 and
  zq3-r11;
- `rockyt` is raised from 150 s to **270 s** (review round 1; its lines
  keep a tool timeout of at least 300 s), so one worst-case getattr plus the
  unmount's own 30 s view wait fits; an `rc=124` is recorded as a finding,
  not necessarily a 1a4 failure, never retried.

The guest and controller bounds are unchanged (no code changes); the sheet
records each outcome as a finding.

## Phases 9-10 recheck

Since zq3-r11, among the tools the sheet uses, `controller.py`
(`989842f9…`, admits only `150deb29…`), `rocky_kernel.py` (`b69c7457…`) and
`pod_fixture.py` (`01f8e842…`, e94) changed. `bootstrap.py` (`5e467fe6…`),
`bootstrap_remote.py` (`0dcb0fe0…`), `fill_plan.py` (`893350cf…`),
`correlate.py` (`9cd55f2c…`), `guest.py` (`d8361fc0…`) and
`guest-lock-probe.py` (`11521fe6…`) are unchanged. `p10-requests.py` only
fills the pod template's placeholders, so the `/2` request needs no helper
change. No CLI the sheet uses changed shape.

**Sheet dry run** (`dryrun/` in the private fill; scratch output). A
harness extracted the 77 command lines of this sheet from Phase 8 "After
READY" to T8, plus the Phase 7 `k04` baseline line and the two Phase 11
evidence lines (the 984 summary and the review-round-1 ETIMEDOUT scan; 80 in
all), from `run-sheet.md` itself and ran them in order. The scan read the
four release logs, the four lifecycle step logs and the T6/T7/T8 reports
(`etimedout` false for all eleven).
The case-failure block (it names `$CASE`) and the T8 hang capture (a text
block) only run on a failure and are skipped. The harness used:
- the real `fill_plan.py batch`, `check-retained9p.sh check`,
  `correlate.py`, `session.sh` (`tf3`, `obs`, `sha`, `jf`) and the `sh/`
  helpers;
- `controller.py` with faked guest dispatch, whose case output carries the
  y84 `admission-step` events before `admitted`;
- stub `f3`/`rocky` that answer like the lifecycle client (evidence `/2`
  with a claim summary), `host_snapshot.py` and the guest; **new:** the
  Rocky stub parses the uploaded T7 request with the committed
  `pod_fixture.py`'s own `parse_request`;
- a synthetic READY run with journal `/3`, an `ok` `usable` result, L = 5
  and mountinfo rows with the exact g64 options.

Every line passed with the expected results:
- L = 5; Phase 9 at 6 (L+1); case pairs 7/8, 9/10, 11/12, 13/14; T9 15,
  ARM 16, post-CALL 17; T10 18, RESTART 19;
- the capture check printed three `ssh-noop` lines and the OBSERVE at 6;
- the batch fill and check printing `cases_planned` (four) and the deferral;
- T1-T4 correlated and released; `syscall_batch_success` True with the
  deferral in the state;
- `p10-admission.py` summarised all four cases (`admission_ms` 2000,
  `deadline_hit` and `etimedout` false);
- T7: the request written by `p10-requests.py` (`homelabfs-retained9p-pod/2`,
  `runtime_nevra` with the epoch form) passed `parse_request`;
- T9 `dropped-reply-effect-committed-and-visible`;
- T10 FENCED;
- T11 `k04_refusal_observed` true, with `deferred` repeated;
- the T6/T8 lock requests from `p10-requests.py`;
- T8 `rc=0`;
- `p9-timing.py` summarised 12 timed OBSERVEs and 3 no-ops.

**Negative and real-record checks** (`helpers-dryrun.py`: 38 cases, all as
expected):
- `p10-admission.py` (review round 1): `etimedout` false for a clean case
  and for a string `"110"` errno; true for errno 110 in a failure, a
  `reply` event, a `step-observed` `mkdir_errno`, a nested `lstat_errno`
  and a lock-probe report;
- `p9-ready.py` refused a wrong socket, a missing or failed `usable`
  result, a `/2` journal header, a missing `observe_sequence_last`, L = 1,
  and a `usable`/summary L mismatch;
- `p9-observe.py` refused sequence 1, L+2, a READY_JSON without L, a fenced
  admission, a rotated cut, an incomplete OBSERVE, evidence `/1` and a wrong
  plan hash;
- `p9-timing.py` refused a malformed TSV and one without an OBSERVE line;
- `p10-argv.py` and `p10-requests.py` refused as in r11;
- `fill_plan.py batch` refused a synthetic run that stopped at `usable`
  with the b84 claims note (`bootstrap steps did not end ok:
  usable=failed`), a `/2` journal and zq3-r11's real run (`journal is not
  for the pinned bootstrap plan`); `p9-ready.py` refused the failed run and
  zq3-r11's real run directory (its control socket is zq3-r11's).

## Decisions from the zq3-r12 run-sheet review round 1 (2026-11-18)

No MUST-FIX. Applied to this fill and the public copy:
- **SHOULD-FIX 1:** `p10-admission.py` sets `etimedout` for errno 110
  anywhere in a file (failure, `reply` events, `errno`/`*_errno` fields),
  because lifecycle steps record an `OSError` errno as an observation and
  continue; Phase 11 now scans every release log, the T9-T11 step logs and
  the T6/T7/T8 reports (`p11-etimedout-scan.jsonl`).
- **SHOULD-FIX 2:** the nightly-upgrade go/no-go gate (N1 above; Phase 1
  records Rocky's clock, Phase 5 step 0 runs the gate).
- **NITs:** `rockyt` 270 s with `rc=124` worded as a finding, not
  necessarily a 1a4 failure; Phase 11 step 6 waits 90 × 10 s for
  `child-gone` (still no forced power-off).

## Findings and open questions

- **Q0 N1.** The run must start and finish before Rocky's next 00:55 UTC
  nightly upgrade. The Phase 5 gate stops it before Rocky is stopped if it
  would not; Phase 1's pod-runtime and kernel rechecks catch an upgrade that
  already ran, and drift means a refill.
- **Q1 T5 deferred.** The run cannot complete zq3 acceptance. T5 needs a
  re-plan and sizing first.
- **Q2 the 5a4 fixes are untested on f3.** The kernel `8c1de1ed…` and the
  daemon `e0981a0a…` passed offline tests only. The main questions are
  Phase 4 step 1a, T1-T4 admission with z94's self-started recalls, and a
  clean T8 unmount.
- **Q3 admission time vs bounds.** One getattr can take about 90-120 s; the
  10 s admission alarm, the 60 s case alarm and the 75 s dispatch bound are
  all shorter. If z94's recalls still delay a root open beyond 10 s, T1 can
  again end as a `Deadline` or `dispatching-uncertain` (now without a wedged
  recall). Raising these bounds is a code/plan change, not done here.
- **Q4 T7 first real run.** T7 has never run for real. The rootfs manifest
  may differ from `f7f69339…` (python3.9 changed with EL9.9).
  `pod_fixture.py` `parse_request` accepts an epoch-less podman NEVRA that
  `check_runtime` would then refuse; the fill avoids it by copying the
  facts' `%{NEVRA}` output exactly (with the epoch) and comparing.
- **Q5 memory.** At 13:50Z, about five hours after Rocky's start, free was
  9.91 GB, inactive 1.23 GB and ARC 4.18 GB. Phase 5's estimate and the
  Phase 6 bar decide (zq3-r11 margin 0.50 GB after the drain).
- **Q6 UEFI vars drift.** The record pins `2dd5b1ea…`. Any Rocky boot before
  Phase 7 changes it, and `host.py setup` then refuses. Recheck right before
  the run.
- **Q7 Phase 1a.** zq3-r3 to zq3-r7 and zq3-r9 to zq3-r11 unloaded both
  modules without refusal. A refusal now stops before staging and needs a
  plan revision.
- **Q8 window pins file:** the r12 window pins live only in the private fill,
  with their hash recorded here.
- **Q9 T6, T7, T9-T11:** never run for real; the T9 ARM, T10 FENCED step 2
  and the T11 receipt wait (5 minutes) have only been dry-run on the
  current tools.

No committed tool refused real inputs. These intended refusals all
occurred:
- the controller template;
- the unfilled bootstrap template;
- the superseded b94 `3b128cc6` candidate in a synthetic batch plan and in
  `rocky_kernel.py plan`;
- an undeclared four-case batch and a deferred `credentials`;
- the window filler on the b94 artifact hashes and on facts with both
  modules loaded;
- `pod_fixture.py parse_request` on the r11 `pod/1` template and on the
  malformed NEVRA variants above.

## Local verification

At `637dc11` in the main checkout, all passing:
- retained9p: 381 tests;
- directlaunch: 26 tests;
- `tools/lab` top level: 248 tests (3 skipped);
- `go test -count=1 ./internal/runtimeprep`.

`shellcheck` is clean for `build-stage.sh` and `sh/*.sh` (as POSIX sh), and
for `session.sh` apart from SC2034/SC2086: its variables are used by the
sheet, and `$O` is split on purpose. No code was changed.
