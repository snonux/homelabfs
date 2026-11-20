# zq3-r16 filled run inputs (2026-11-20)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r15 fill](../20261119-zq3-r15-filled-plan/README.md) ·
[zq3-r15 record](../20261119-zq3-r15/README.md) ·
[zq3-r16 record](../20261120-zq3-r16/README.md) ·
[r16 candidate](../../../../tools/integration/artifacts/evidence/xz3-20261119-r16/README.md)

These are the exact run inputs for `zq3-r16`, the sixteenth attempt of zq3.
They were generated on the workstation with the committed tools at `main`
`89af1cc` (after ie4). They mirror the
[zq3-r15 fill](../20261119-zq3-r15-filled-plan/README.md), which ran for
real: Phases 0 to 9 passed (Phase 1a, Phase 4 step 1a for the z94 daemon
`8668e5f0…`, the predicted pod manifest `34a71569…`, both mounts, READY,
`usable` L = 2, Phase 9), T1 failed (R15-1: the m0 root OPEN succeeded
after 7.4 s, then the root `fstat` got `ESTALE` from the guest's own
`hfs_mmap_begin` view re-check after a recall, before any GETATTR was
sent), and Phase 11 took the guard-SIGTERM branch, where both unmounts
returned `rc 0` in 1 s. The changes are:
- the run ID (`zq3-r16`) and the native guard root
  `/var/tmp/hfs-20261120-zq3r16-fdq` (fill date 2026-11-20);
- the r16 candidate `a1af0bd7…` with the **new ie4 guest kernel**
  (bzImage `7109ac49…`, GNU build ID `7a5bb8dc…`, System.map `95802ae3…`):
  an observation whose view moved during VIEW_ACQUIRE acquires again
  instead of returning `ESTALE`, and GETATTR/LOOKUP re-issue on a moved
  target. The z94 daemon `8668e5f0…`, the window artifacts
  (`9d9f0f01…`, `d6f2e4d0…`), the pc4 exporter `2fd7e32f…`, the config,
  the modules and the init helper are reused byte for byte. **Phase 4
  step 1a stays required** (cheap; every run repeats it);
- a regenerated Rocky kernel plan, and `rocky_kernel.py` and
  `controller.py` re-pinned (they admit only `a1af0bd7…`);
- `hfs-runtime-prep` and the signer rebuilt at `89af1cc`: **the bytes
  equal zq3-r15's apart from the VCS stamp** (no Go source changed since
  `2f34b60` apart from a README; `-buildvcs=false` builds at `312abdb` and
  `89af1cc` are identical);
- the 2026-11-20 04:25:30Z facts: Rocky PID `11069` (zq3-r15's Phase 12),
  UEFI vars `5df71476…` (new argv record), modules ids 32/33, and **a new
  dnf transaction #65** (fd-find only): the N1 gate now expects #65;
- the zq3-r15 lessons (R15-1; ie4 log lines and any `ESTALE` from a probe
  syscall are findings);
- **Phase 1a (D1-a) runs again:** zq3-r15 left `fdescfs.ko` and
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
and never reboots Rocky. The 2026-11-20 04:25:30Z facts
(`rocky-upgrade-…txt`, `rocky-dnf65.txt`): stamp `2026-11-20` (written
00:55:07 EET); **a new dnf transaction #65** (2026-11-20 00:55) upgraded
only `fd-find` (10.4.2-6.el9 → 10.5.2-1.el9, `@epel`). It touches neither
the kernel, python3.9 nor the pod runtime, which the same facts re-verify
unchanged. `sh/n1-gate.py phase1` now expects #65; any transaction newer
than #65 at Phase 1 means the pod-runtime and kernel rechecks decide and
drift means a refill, as before. **The run must start after that day's run
has stamped and finish before the next one, 22:55 UTC on the run day
(2026-11-20 22:55 UTC for a run that day); the latest Phase 5 start is
21:25 UTC.** Phase 1 runs `n1-gate.py gate` early and Phase 5 step 0 runs
it again before Rocky is stopped. The gate computes the dates from its own
clock, so a run on a later day needs no edit; the in-run cutoff (no new
case after 22:15 UTC, guest poweroff before 22:45 UTC, the 22:35 UTC
fallback) applies on the run day.

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-zq3-r16-fill` (`0700`). This directory holds only
public hashes, paths, argv and the read-only facts. These stay private and
are never committed:
- the per-session keys;
- the runtime config, which holds host receipt private keys;
- the staging tarballs that contain it.

## Inputs

| Input | Value |
|---|---|
| Candidate | **r16 kernel rebuild** (task ie4): manifest `a1af0bd7…` (195 files), package `/home/paul/tmp/claude/hfs-xz3-20261119-r16/package`, source checkout `707f3d1`, repository baseline `2f34b60` with `inputs/repository-changes.patch` `60a66f05…`. **New:** the guest kernel `6.1.190-hfs-xz3` (bzImage `7109ac49…`, build ID `7a5bb8dc…`, System.map `95802ae3…`). **Reused byte for byte:** the z94 daemon `8668e5f0…`, the window artifacts `9d9f0f01…`/`d6f2e4d0…`, config `28fe4a8e…`, the five modules and `modules.*`, the pc4 exporter `2fd7e32f…`, the init helper `8c02b98f…`. **Lab tools:** `controller.py` `dfda3889…` and `rocky_kernel.py` `695358cf…` (admit only `a1af0bd7…`); unchanged since the zq3-r15 fill (`git diff 644e9fa 89af1cc -- tools docs/lab/zq3-fdescfs-window`): `guest.py` `48d6ae81…`, `guest-lock-probe.py` `fd439897…`, `pod_fixture.py` `07314543…`, `pod_workload.py` `c714370f…`, `argv.py` `78462c2e…`, `bootstrap.py` `5e467fe6…`, `bootstrap_remote.py` `0dcb0fe0…`, `correlate.py` `9cd55f2c…`, `fill_plan.py` `893350cf…`, `runtime-guard.py` `fece96b3…`, native guard `576d802a…` |
| Phase 1 facts | [facts/](facts/), collected read-only 2026-11-20 04:25:30Z; they supersede zq3-r15's 16:01:29Z set. **f3:** boot `1794574308` (unchanged since zq3-r8), **`fdescfs.ko` (id 32) and `fusefs.ko` (id 33) loaded**, no fuse/fdesc mount, free 7.42 GB + inactive 1.64 GB, ARC 6.09 GB, `/usr/bin/ssh` `6a9e5573…`, python3.12 `a66eb370…`, `10.20.0.133` on `vm-public` (members `tap0`, `re0`). **Rocky:** PID `11069` (UTC lstart `Thu Nov 19 16:33:55 2026`, zq3-r15's Phase 12 `start-rocky`), boot ID `53cec809-…`, running stock `5.14.0-741.22.1.el9_9` = grub default, `next_entry` empty, `boot_indeterminate=7`, SELinux Enforcing, `/.autorelabel` absent, no `hfs-xz3` file in `/boot` (zq3-r15's U2, verified: count 0), `/boot` 328 MB free, python3.9 `729ac37f…`, `authorized_keys` `e08542dc…`. **Clock and upgrade:** `Europe/Sofia (EET, +0200)`, synchronized; stamp `2026-11-20` (00:55:07 EET); newest dnf transaction #65 (2026-11-20 00:55, fd-find only); next run 22:55 UTC. **Pod runtime** (byte-identical to zq3-r15's): podman `249e918c…` `podman-6:5.8.5-3.el9_9.x86_64`, crun `659867c7…` `crun-1.29.1-1.el9.x86_64`, conmon `1ac2a8db…`, seccomp.json `7dd44119…`. The published files hold no key material (grep for key headers and `ssh-*` lines) |
| Host events since zq3-r15's fill | zq3-r15's own Rocky stop (16:29:40Z, ACPI) and normal start (16:33:55 UTC, vm-bhyve run 1, no relabel reboot, U2 done); Rocky's daily run #65 (fd-find only); the hourly timer ticks only logged the deferred reboot; the r16 build ran on the workstation only. No f3 reboot since Paul's deliberate 2026-11-13 reboot. The f3 sshd listener and node_exporter PIDs differ from the 2026-11-19 facts (service restarts, not a reboot; see Q8) |
| Launch facts | `f3-launch-facts-20261120T042530Z.json` (`a05fe2a6…`): `rocky.conf` `c101adf1…`, vm-run `43753bc8…`, firmware `6b48e967…` unchanged; **UEFI vars `5df71476…`** (zq3-r15: `ff1ec37b…`); launch logged `2026-11-19T18:33:55+02:00`; the interface list order changed (`tap0` before `wg0`), the set did not |
| Tap | `tap100` (bridge members `re0`, `tap0`) |
| Run ID / staging | `zq3-r16`, `/var/tmp/hfs-validation-xz3/zq3-r16` (the parent exists and holds zq3-r1 to zq3-r15) |
| Native guard root | `/var/tmp/hfs-20261120-zq3r16-fdq`, host profile `--rocky-pid 11069 --rocky-birth 'Thu Nov 19 16:33:55 2026'`, guard `576d802a…` (unchanged) |
| Window directory | `/var/tmp/hfs-fdescfs-windows/zq3-r16` (the parent exists, `root:wheel 0700`) |
| Rocky names | `/root/hfs-zq3-zq3-r16-*`, `/var/tmp/hfs-zq3-zq3-r16` (the zq3-r1 to zq3-r15 ones stay as evidence) |
| Operator public key | `2f6d909c…`; key path `…/hfs-zq3-r16-fill/operator/operator-signing-key` (empty `0700` directory, never read here) |

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | **Rebuilt** at `89af1cc`: `hfs-runtime-prep` and the signer `hfs-retained9p-operator`, with `-trimpath`, CGO, VCS stamps and an unmodified private clone (`vcs.modified=false`); a second build of each, forced with `-a`, gave the same bytes. **Byte comparison with zq3-r15:** builds with `-buildvcs=false` at `312abdb` and at `89af1cc` are identical (prep `cfdac74b…`, signer `eaec1bf6…`), so the new bytes differ from zq3-r15's (prep `9f5b87c6…`, signer `14d0a362…`) only in the version stamp (`git diff 312abdb 89af1cc -- cmd internal go.mod go.sum` touches only `internal/guestbridge/README.md`). G3 init helper: zq3-r1's reproduced build | prep `4c719e07…`, signer `fc576787…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from `testdata/zq3-spec.json` at `89af1cc` (`make-spec.py`: only `zq3-r1` → `zq3-r16` and the r16 operator key path change; the result equals zq3-r15's spec with the run ID renamed). The committed spec is unchanged since zq3-r15 | spec `3b84e980…`, request template `c11a9483…`, runtime config `e42f7a3b…`, volume-init `5ff311ef…`; volume `6ef0dfaf-…`; sessions hfs0 `ab3894b9-…`, hfs1 `eb262a2f-…` |
| 1. Argv | `argv.py generate` (unchanged, exporter pin `2fd7e32f…`) from the 04:25:30Z launch facts (sessions, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `0a2fa323…`, args `5d5626c1…`, vars `5df71476…`, exporter `2fd7e32f…` |
| 2. Kernel plan | `rocky_kernel.py plan` at main (`695358cf…`; admits only `a1af0bd7…`), stock default `/boot/vmlinuz-5.14.0-741.22.1.el9_9.x86_64`. Against zq3-r15's it differs in `run_id`, `manifest_sha256`, the bzImage and System.map entries and the build ID; config, modules and `modules.*` are the same. The r15 package `ac8020aa…` is refused as superseded ("use the r16 rebuild") | plan `a339da2e…`, build ID `7a5bb8dc…` |
| 3. Controller batch | Template derived from zq3-r15's by `derive-templates.py`: run ID, fill path, sessions, candidate `ac8020aa…` → `a1af0bd7…`; probe `guest.py` `48d6ae81…` unchanged; be4 request `seconds` 240 and plan `seconds` 255 kept (checked). **T5 stays deferred** with the r9 reason. `check` refuses the template as designed (`probe/installed-manifest pin`). A synthetic copy with zero IDs **passes** and prints `cases_planned` (four) and the deferral. The same copy is **refused** with the r15 candidate (superseded, "use the r16 rebuild"), with zq3-r14's seconds 120/60 and with plan 120 alone (`per-dispatch deadline cap 255..600 seconds`), with plan 255 but requests 60 (`case differs from exact batch`), without the declaration and with `credentials` also deferred | template `2a8f1129…` |
| 4. Bootstrap plan | Template derived from zq3-r15's (the one that ran through READY and T1). New pins: bundle, kernel plan, `rocky_kernel.py` and the rebuilt prep `4c719e07…`. Unchanged: the daemon `8668e5f0…` (`runtime.sha256`), `bootstrap_remote.py` `0dcb0fe0…` (`remote_sha256`) and the guard pin `fece96b3…`. The script re-hashes every pinned file at main and in the package. The unfilled template refuses as designed (`observer.ssh_sha256: SHA256 hex`). **F1 filled now** with `fill_plan.py bootstrap` from the 04:25Z `sha256 -r /usr/bin/ssh` line (`6a9e5573…`), then `bootstrap.py check` (journal `/3`) **pass** | template `1d3e835f…`, filled plan `a60f4ac3…` |
| 5. Guard argv | Derived from zq3-r15's, with `--runtime-sha256 8668e5f0…` (checked against the package) and `--disposable-validation` exactly once, last. It equals `bootstrap.py`'s own `SshRemote.guard_argv()` for the filled plan, in shell string and argv, and passes `runtime-guard.py`'s `parse_args` and `check_command` | `c31b2be5…`, in [pins](pins.json) |
| 6. Pod template (e94, be4) | zq3-r15's `homelabfs-retained9p-pod/2` template with the run ID renamed (`fixture_sha256` `07314543…` unchanged). `derive-templates.py` compares its `runtime_nevra` and the four `runtime_sha256` values with the facts' `== pod-runtime` section (byte-identical to zq3-r15's), cross-checks the hashes with the Phase 1 `sha256sum` lines, and runs the committed `pod_fixture.py`'s own `check_nevra_pins` and `parse_request` (synthetic fill): **both accept**. `parse_request` refuses a `pod/1` request without `runtime_nevra`, version `/1` with `runtime_nevra`, swapped NEVRAs, a `podman-docker` sibling, an extra conmon NEVRA, the zq3-r14 fixture pin and the r11 one. **Expected rootfs manifest:** zq3-r15's real `34a71569…` (nothing in the rootfs changed; an expectation, not a pin) | pod template `2aa657af…` |
| 7. Lock-probe templates (be4) | zq3-r15's with the run ID renamed (budgets 150/240 kept); the filled requests pass the committed `guest-lock-probe.py`'s `validate_request`, and case 151 is refused | m0 `00e326eb…`, m1-T8 `7a4c8c6a…` |
| 8. Observer and staging pins | Unchanged script pins: `observer_key.py` `cd7f38c2…`, `observer.py` `2e1449dd…`, `installed.py` `c824b183…`, native guard `576d802a…`, `runtime-guard.py` `fece96b3…`, `fdescfs-window.py` `b00b19d1…`, `host.py` `738e1ca5…`, `host_snapshot.py` `b29ffed2…`, `argv.py` `78462c2e…`, `guest-lock-probe.py` `fd439897…`, `pod_fixture.py` `07314543…`, `pod_workload.py` `c714370f…`. Re-pinned to main: `rocky_kernel.py` `695358cf…`. New staged bytes: the ie4 bzImage and System.map, the r16 argv record, bundle and kernel plan. All 17 f3 and 18 Rocky files went into two deterministic tarballs; a second tar run gave the same hashes. A verification extract matched every expected hash (35), the f3 tarball holds only `0/0` entries with modes `0700`/`0500`/`0400` and no parent entry; the extract was then deleted. Against zq3-r15's expected lists only the three f3 records and the four Rocky kernel-stage files (bzImage, System.map, plan, `rocky_kernel.py`) changed | f3 tar `4fc64ca7…`, Rocky tar `5ace86ff…` |
| 9. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` at main (`6aef7d3a…`, unchanged since zq3-r15; names `8668e5f0…`, `d6f2e4d0…`, `9d9f0f01…`) with `zq3-r1` → `zq3-r16`. Synthetic fills, which are not pins: zq3-r8's real Phase 4 facts and host hashes (modules absent) with the paths renamed and the three artifact hashes **pass**; the same facts with the xc4 hashes **refuse** (`pinned artifact differs or missing`); post-restore facts with both modules loaded, the state on f3 now, **refuse** (`fdescfs module present`): hence Phase 1a | r16 pins `82109b1f…` |
| 10. Signing watcher | zq3-r15's `bin/sign_watch.py` (it signed both requests in zq3-r12 to zq3-r15) with `D`, the docstring run ID, the signer comment and **the signer pin `fc576787…`** changed. Tested locally with a fake signer: a wrong request hash, a wrong command, an unknown action, a repeat and a wrong signer hash were refused; both exact requests were signed | `1f2eeab0…` |
| 11. Process observer check | `sh/p4-observer.py` with the run ID renamed (daemon pin `8668e5f0…` unchanged). zq3-r15's real records (renamed) **pass**; with the xc4 hash substituted they are refused for the `sha256` only. The `$NG` line (Rocky PID `11069`) parses and passes the native guard's own `parse_args` and `check_request` | `b72e2d8f…` |
| 12. N1 gate helper | `sh/n1-gate.py` from zq3-r15 with **the facts' newest transaction moved to #65** (`FACTS_TOP`). Retested at fixed times on 2026-11-20: 13:30Z GO (also with the real 04:25Z upgrade record), 21:20Z and 21:25Z GO, 21:30Z, 22:56Z and 23:10Z STOP, the next day with the old Phase 1 STOP, with a fresh Phase 1 and stamp GO, a stale stamp STOP, 01:00Z GO; `phase1` reports `dnf_unchanged` true for the facts (#65) and false for #64 and a synthetic #66 | `e9e04a84…` |
| 13. Fence record helper | `sh/p11-fence.py` from zq3-r15 (docstring only). Retested: the real nc4 row and stderr line (found), zq3-r15's real Phase 11 and case-failure evidence (`fenced` false) | `e727a6a8…` |
| 14. Guard budget helper (be4) | `sh/guard-budget.py` from zq3-r15 (docstring only; be4 budgets unchanged: phase9 180, T1-T4 660 each, T6 300, T7 615, T9 with T10 990, T10 390, T8 540; 300 s reserve; 3600 s guard; latest starts after the launch 3120, 2640, 3000, 2685, 2310, 2910 and 2760 s). Tested at fixed times on **zq3-r15's real journal** (21 cases): its real Phase 9 (16:31:29Z) and T1 (16:31:46Z) starts GO, each latest start GO and one second later STOP (the +1 s STOP case was not run for T6 and T10); a `/2` journal, two or no `launch` intents, an unknown step and a missing file refused (`rc=2`) | `0f4a271e…` |
| 15. Run sheet | [run-sheet.md](run-sheet.md): Phases 0-12, 1a (**runs**), 4/1a (**required**), U1-U3, N1 (#65), the SELinux gates, the R11, R12-1 and R13-1 checks, the fence record capture, the be4 guard budget table, the tool-timeout rule with its review-round-1 comments on every `$CTL`, lock-probe and T7 line, `rockywait`, bash only, the zq3-r15 execution notes, "New for zq3-r16" and a run log template with an ie4 line. `session.sh` (`d78c53d1…`) and sixteen pinned helper scripts; `p10-requests.py` (`323d13a5…`) re-pins the three r16 request templates. The Phase 8-10 lines were dry-run end to end (below) | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-20T00:00:00Z --format=gnu`.
The f3 tarball holds only `hfs-20261120-zq3r16-fdq` and
`hfs-validation-xz3/zq3-r16`. It has **no entry for the existing parent**
`hfs-validation-xz3/`, so extraction never rewrites the parent's mode or
owner. All directories are `0700`, binaries `0500`, other files `0400`,
owner `0/0`.

## Signing

Paul delegated both signatures (A7) to Claude. In Phase 8 the driver's
stdout goes through `bin/sign_watch.py` (`1f2eeab0…`). The watcher runs the
signer (`fc576787…`) only if:
- the printed command is exactly one of the two below;
- the request file's SHA256 equals the printed `request_sha256`;
- each command runs at most once.

Anything else is refused, and the driver then times out (fails closed):

```
/home/paul/tmp/claude/hfs-zq3-r16-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r16-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r16-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r16-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r16-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r16-fill/bootstrap-run/work/signer-binding
```

On Paul's instruction, Claude places the operator key at the bundle's path
before the run. Nothing in the fill reads it.

## Run-time fills

| Fill | When and source | Used by |
|---|---|---|
| N1 | Phase 1: Rocky's time zone (`timedatectl`), the upgrade stamp and dnf history (`p1-upgrade.txt`, `n1-gate.py phase1`), the early gate (`p1-nightly-gate.txt`), boot ID, running/default kernel, `next_entry`, python3.9, `authorized_keys` and the pod runtime (`p1-pod-runtime.txt`, `pod-runtime-unchanged: True`); any drift stops for a refill. Phase 5 step 0: `n1-gate.py gate` | Phase 1 Rocky gate, Phase 3 install, Phase 5 go/no-go, T7 |
| D1 | **D1-a, Phase 1a**: both modules are loaded (04:25Z: ids 32 and 33); Phase 1 confirms it, Phase 1a unloads them normally (never `-f`), and a refused unload stops before staging | Phase 4 step 0 window plan fill (needs both absent) |
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 04:25Z fact); Phase 4 step 0 rechecks it (`F1-unchanged`), a change stops | bootstrap `observer.ssh_sha256` (plan `a60f4ac3…`) |
| F2 fdescfs window plan and its hash (`WP`) | Phase 4 step 0: controller `facts`, host hashes, `realpath` library closure, f3 `date -u +%s`, then `fdescfs-window-plan.py --pins` with the r16 pins | setup, qualify, restore (`W`); Phase 5 recheck |
| F3 pod rootfs manifest digest (`PM`) | Phase 3: `pod_fixture.py manifest` on Rocky (zq3-r12 to zq3-r14: `523485cc…`; zq3-r15: `34a71569…`, as predicted; **expect `34a71569…` again**: nothing in the rootfs changed; any other digest is explained before `build`); `skip` only if A8 is declined | `build`, T7 request |
| F3a process observer record | Phase 4 step 1a: guard stage `process-observer`, checked by `p4-observer.py` | go/no-go before setup; **pass required** (the reused z94 daemon `8668e5f0…`; zq3-r15 passed) |
| F4 observer key pair, enrolled line, `after_sha256` (`EA`) | Phase 4 step 5: `ssh-keygen` on f3, `observer_key.py enrol` | runtime observer, Phase 11 `remove --expect-sha256`, banner-timeout check |
| F5 BLS entry ID | Phase 3 install record | `select` (from the state directory) |
| F6 boot ID, provider nonce, admission manifest SHA256, `virtioN` names, proposal, controller, provision hash | Phase 8 admission and runtime records | enrollment and bind requests (bootstrap fills them), controller batch plan |
| F7 m0/m1 mount IDs | Phase 8 mount steps (mountinfo) | batch plan, then `p10-requests.py` for the T6/T7/T8 requests |
| F8 batch plan and its hash (`B`) | Phase 8, after READY and `usable`, at once: `fill_plan.py batch` (prints `cases_planned` and `deferred`), then `check-retained9p.sh check` | every controller and correlator call |
| F9 runtime UUID (`U`), `L`, cut, epoch and lifecycle sequences | READY record and `summary.json` via `p9-ready.py` (`--runtime`, `--observe-last`); Phase 9 uses L+1 and the T1 baseline L+2, then each from `correlate.py command` (`obs`, ARM, RESTART); expected L+1 to L+14 with T5 deferred | Phase 9 OBSERVE, T1-T4, T9-T11 |
| F10 exporter child PID (`XP`) | guard `receipt.json` `exporter.pid` (Phase 11 step 0; the `--no-child` line if null) | `host.py cleanup --child-pid` |
| F11 Rocky PID and lstart | **fixed** from the 04:25Z facts (`11069`, `Thu Nov 19 16:33:55 2026` UTC, zq3-r15's Phase 12); rechecked in Phases 1, 4 (also by every `$NG` stage) and 5 | native guard host profile, Phase 5 recheck, `p5-mem.txt` RSS |
| F12 UMA drain decision | Phase 5 step 1a: `p5-estimate.py` `drain_recommended` | Phase 5 step 1b (or once in Phase 6) |
| F13 T6/T7/T8 request files | Phase 8 after the batch check: `p10-requests.py` (exclusive, `0600`, in `$E`); lock budgets 150/240 (be4); the T7 request is pod `/2` with `runtime_nevra` and the be4 fixture pin | T6, T7, T8 |
| F14 guard PID | Phase 11, only on the runtime-still-serving path (`pgrep -U root`) | the one SIGTERM to the guard |
| F15 SELinux state | Phase 1 (`/.autorelabel` absent, Enforcing), Phase 9 (guest `getenforce` `Enforcing`, enforce `1`), Phase 12 (relabel record) | Phase 1 and Phase 9 gates; Phase 12 finding |
| F16 984 timing | Phases 9-10: `tf3` lines in `$E/obs-timing.tsv` (three `ssh-noop`, each timed OBSERVE), checked with a local `cat` after the Phase 9 OBSERVE; `p9-timing.py` after T4 and before Phase 11 | run log only; never a gate |
| F17 admission-step timings | Phase 10: `p10-admission.py` over the four cases' `stdout.log` after T4 (`p10-admission-steps.jsonl`; `admission_ms`, the largest gap, `deadline_hit`, `etimedout`; with the owner snapshots, any recall during admission), or over a failed case's before Phase 11 | run log only; never a gate |
| F18 transport fence record | `p11-fence.py`: Phase 8 failure block (`p8-fence.json`), the case-failure block (`t-fail-fence.json`) and always Phase 11 step 1a (`p11-fence.json`) over `runtime.stderr`, the owner snapshots/`f3-evidence.tar` (`transport.db`) and every OBSERVE taken | run log; `fenced: true` is a finding, never a retry |
| F19 guard budget | Phases 9-10: `gbudget STEP` (`$E/*-budget.json`) from the journal's `launch` intent: launch time, guard end, remaining seconds, `decision`; **be4 worst-case step seconds** (Phase 10 table) | GO/STOP before Phase 9 and each budgeted step; a STOP goes to Phase 11 |
| F20 unmount timings | Phase 11 step 2 and T8: each `rockyt … umount` line prints `rc` and `seconds` (`T0`) | R13-1 check; run log |

## Changes from the zq3-r15 sheet

All zq3-r7 to zq3-r15 rules are kept and written into the lines they name:
- **Phase 1a (D1-a): runs again** (zq3-r15 left both modules loaded): a
  normal `kldunload fusefs`, then `kldunload fdescfs`, never `-f`;
- **Phase 4 step 1a, the process observer check: required**, although the
  daemon bytes are unchanged (cheap; every run repeats it);
- the UMA drain decision in Phase 5; `rockyt` bounded guest unmounts
  (270 s); `pgrep -U root -lf runtime-guard.py` for the guard SIGTERM; the
  T10 abort paths to T8; the explicit `--no-child` line for a null
  exporter; observer key removal on every abort after Phase 4 step 5; the
  pause rule; the single host-preflight retry; the Phase 0 recheck of the
  workstation `/usr/bin/ssh` (`16cb2c6a…`, unchanged at fill time); the
  Phase 9-10 helpers;
- from the zq3-r5 to zq3-r7 executions and reviews: Phase 11 step 9 is one
  plain line with literal paths,
  `f3 "doas -n rm -- /var/tmp/hfs-validation-xz3/zq3-r16/observer/id_ed25519 /var/tmp/hfs-validation-xz3/zq3-r16/observer/id_ed25519.pub"`,
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
  `bash -c` without sourcing, the e94 pod `/2` request; the no-pause rule
  (Phase 9 within 5 minutes of READY), every session line in bash, the
  R13-1 unmount timing;
- from zq3-r14 and zq3-r15: the be4 bounds (request `seconds` 240, plan
  255, T6 150/240, T7 workload 240 / run 315), the guard budget helper and
  its GO/STOP lines with the per-step latest start times, the tool-timeout
  comments on every `$CTL` (>= 300 s), lock-probe (>= 300 s) and T7 pod
  run (>= 420 s) line, the timed unmounts, the child-wait line with this
  run's pattern `[h]fs-zq3-r16`.

New for zq3-r16:
- **Candidate `a1af0bd7…`** in the kernel plan, batch template, bootstrap
  plan (prep and kernel plan pins; daemon pin unchanged), staging (new
  bzImage and System.map) and the Phase 3 and 8 expectations.
- **N1:** the facts' newest dnf transaction is #65 (`n1-gate.py`
  `FACTS_TOP`, the Phase 1 lines and text).
- **"Execution notes from the zq3-r15 run"** (R15-1, R13-1 still fixed,
  the child-wait line used as written) and **"New for zq3-r16"** (ie4, the
  expectations, the remaining risks, the facts).
- **Phase 3:** `PM` is expected to be zq3-r15's `34a71569…` again.
- **Phase 10 and the run log:** any ie4 guest log line (`view not LIVE or
  moved`, a re-check retry, `retry exhausted`, a re-issued GETATTR/LOOKUP)
  and any `ESTALE` or `ETIMEDOUT` from a probe syscall is recorded as a
  finding, next to the admission timings and recalls during admission.

## Expectations: what changed since zq3-r15

zq3-r15 passed everything through Phase 9 and failed T1 on R15-1: z94
worked (the m0 root OPEN succeeded in 7.4 s, no alarm), but the root fd's
`fstat` failed with `ESTALE` because `hfs_mmap_begin`'s VIEW_ACQUIRE
stayed PENDING across a recall and the post-acquire re-check saw the moved
view revision. With **ie4**, that observation acquires again (and releases
the old lease only afterwards), so **T1's `fstat` after a recall should
succeed**, with at most one re-acquire logged (rate-limited). **T1-T4
should pass**, then T6, T7, T9, T10, T11 and T8 within the guard budgets
(none of T6-T11 has run for real); T5 stays deferred. Record per-case
admission timings (`p10-admission.py`: `admission_ms`, the largest gap),
any recall during admission (fence or domain-sequence steps in the owner
snapshots and OBSERVEs) and any ie4 log line as findings.

Remaining known risks, not fixed: **je4** (each clean READ lease release
still costs a recall of about 4-7 s on the lab, so admission stays slow;
the guard budgets may STOP later cases, which is the designed behaviour),
**e84** (a non-root pin whose target mismatches still starts an all-domain
recall) and **0a4** (the host never drops a participant that stops
acknowledging). A guest that blocks past its alarm can outlive the
controller's dispatch bound (error/uncertain, safe and accepted by be4, a
finding). An ie4 retry loop that runs out of its 30 s deadline returns
`ETIMEDOUT`: a finding, never a retry.

**Guard budget.** Unchanged from zq3-r15: the be4 worst cases (Phase 9
180 + T1-T4 2640 + T6 300 + T7 615 + T9 with T10 990 + reserve 300 =
5025 s) do not fit the 3600 s guard, so a run with several slow steps
STOPs early and records the rest unrun. Latest starts after the launch:
Phase 9 52:00, T1-T4 44:00, T6 50:00, T7 44:45, T9 (with T10) 38:30, T10
48:30, T8 46:00. A healthy matrix (each case well under a minute) ends
about 35 minutes after the launch.

## Phases 9-10 recheck

Since zq3-r15, among the tools the sheet uses, only `controller.py`
(`dfda3889…`) and `rocky_kernel.py` (`695358cf…`) changed (they admit only
`a1af0bd7…`). `guest.py`, `guest-lock-probe.py`, `pod_fixture.py`,
`pod_workload.py`, `bootstrap.py` (`5e467fe6…`), `bootstrap_remote.py`
(`0dcb0fe0…`), `fill_plan.py` (`893350cf…`), `correlate.py` (`9cd55f2c…`)
and `argv.py` (`78462c2e…`) are unchanged. No CLI the sheet uses changed
shape.

**Sheet dry run** (`dryrun/` in the private fill; scratch output). The
zq3-r15 harness, renamed, extracted the 87 command lines of this sheet
from Phase 8 "After READY" to T8, plus the Phase 7 `k04` baseline line and
the two Phase 11 evidence lines (90 in all), from `run-sheet.md` itself and
ran them in order under bash, with the real `fill_plan.py batch`,
`check-retained9p.sh check`, `correlate.py`, `session.sh` (`tf3`, `obs`,
`gbudget`, `sha`, `jf`) and the `sh/` helpers, `controller.py` (r16) with
faked guest dispatch, stub `f3`/`rocky` and a synthetic READY run (journal
`/3`, launch two minutes back, `ok` `usable`, L = 5). Every line passed
(`HARNESS-DONE lines=90 L=5`): all ten `gbudget` lines `GO`; the filled
batch plan passed `check`; T1-T4 correlated and released; T10 `FENCED`;
T8's timed unmount line `rc=0 seconds=0`.

**Negative and real-record checks** (`helpers-dryrun.py`: 38 cases, all as
expected). Also: `p9-ready.py` refused zq3-r15's real READY run (its
control socket is not the r16 path); `fill_plan.py batch` refused zq3-r15's
real run for the r16 plan pin (`journal is not for the pinned bootstrap
plan`); `p10-admission.py` on zq3-r15's real T1 log reports
`deadline_hit` false, the failure `OSError` errno 116 and the gaps
`open-root[0] -> stat-root[0]` 7388.3 ms and `stat-root[0] -> failure`
7909.3 ms (R15-1).

## Findings and open questions

- **Q0 N1.** The run must start after the run day's daily upgrade has
  stamped and finish before 22:55 UTC that day; the latest Phase 5 start is
  21:25 UTC, no new case or step starts after 22:15 UTC, and the guest is
  powered off before 22:45 UTC. A later day needs no refill as long as
  Phase 1's rechecks hold; a dnf transaction newer than #65 lets the
  pod-runtime and kernel rechecks decide.
- **Q1 T5 deferred.** The run cannot complete zq3 acceptance.
- **Q2 the guard budget and je4.** The be4 worst cases sum to 5025 s
  against the 3600 s guard; with each lease release still costing a 4-7 s
  recall (je4), a slow admission can make later cases STOP (by design). T9
  with T10 must start by launch + 38:30.
- **Q3 the ie4 kernel's first run.** `7a5bb8dc…` has passed only its
  builds, the clean repro, the source suites and offline admission. Phase 3
  installs it and Phase 8 is its first real boot; any ie4 log line, a
  re-acquire that runs out of its 30 s deadline or an `ESTALE` from a probe
  syscall is a finding.
- **Q4 T1-T4, T6, T7, T9-T11 first real runs** with the be4 deadlines; T6,
  T7 and T9-T11 have never run for real. After a fence, T10's restore would
  refuse the ledger (by design).
- **Q5 the pod rootfs manifest.** Expected `34a71569…` (zq3-r15's real
  digest); any other digest must be explained before `build`.
- **Q6 memory.** At 04:25Z free was 7.42 GB, inactive 1.64 GB and ARC
  6.09 GB (zq3-r15's fill: 7.68, 1.40, 6.03 GB; its active-based margin was
  0.35 GB and it drained). Phase 5's estimate and the Phase 6 bar decide.
- **Q7 UEFI vars drift.** The record pins `5df71476…`. Any Rocky boot
  before Phase 7 changes it, and `host.py setup` then refuses.
- **Q8 f3 service restarts.** The f3 sshd listener (PID 78910 → 42115) and
  `node_exporter` changed PIDs between the 2026-11-19 and 2026-11-20 facts
  without an f3 reboot (boot time unchanged); `/usr/bin/ssh` is unchanged
  (`6a9e5573…`). Not a blocker; Phase 1 rechecks the pinned items.
- **Q9 Phase 1a.** zq3-r3 to zq3-r7 and zq3-r9 to zq3-r15 unloaded both
  modules without refusal. A refusal stops before staging.
- **Q10 window pins file:** the r16 window pins live only in the private
  fill, with their hash recorded here.
- **Q11 the tool timeouts.** The executing agent's shell tool must use at
  least 300 s for `$CTL` and lock-probe lines and at least 420 s for T7's
  run line; a tool kill would leave an uncertain dispatch.

No committed tool refused real inputs. These intended refusals all
occurred:
- the controller template; the unfilled bootstrap template;
- the superseded r15 `ac8020aa` candidate in a synthetic batch plan and in
  `rocky_kernel.py plan`;
- zq3-r14's seconds (plan 120, requests 60; plan 255 with requests 60) in a
  synthetic batch plan;
- an undeclared four-case batch and a deferred `credentials`;
- the window filler on the xc4 artifact hashes and on facts with both
  modules loaded;
- `pod_fixture.py parse_request` on the malformed pod requests and the
  zq3-r14 and r11 fixture pins;
- `guest-lock-probe.py` on a case budget of 151 s;
- `p4-observer.py` on zq3-r15's real record with the xc4 `sha256`;
- `n1-gate.py gate` at the STOP times; `guard-budget.py` after each
  latest start and on malformed journals;
- `p9-ready.py` and `fill_plan.py batch` on zq3-r15's real run.

## Local verification

At `89af1cc` (a clean worktree of main), all passing:
- retained9p: 391 tests;
- directlaunch: 26 tests;
- `tools/lab` top level: 249 tests (3 skipped);
- `go test ./internal/runtimeprep`.

`shellcheck` is clean for `build-stage.sh` and `sh/*.sh` (as POSIX sh), and
for `session.sh` (as bash) apart from SC2034/SC2086: its variables are used by
the sheet, and `$O` is split on purpose (which is why it needs bash). No code
was changed.
