# zq3-r13 filled run inputs (2026-11-19)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r12 fill](../20261118-zq3-r12-filled-plan/README.md) ·
[zq3-r12 record](../20261118-zq3-r12/README.md) ·
[zq3-r13 record](../20261119-zq3-r13/README.md) ·
[xc4 candidate](../../../../tools/integration/artifacts/evidence/xz3-20261118-r13/README.md)

These are the exact run inputs for `zq3-r13`, the thirteenth attempt of zq3.
They were generated on the workstation with the committed tools at `main`
`afe34fb` (after xc4). They mirror the
[zq3-r12 fill](../20261118-zq3-r12-filled-plan/README.md) with its run-time
revision R1, which ran for real: Phase 1a unloaded both modules, Phase 4
step 1a qualified the 5a4 daemon, Phase 6 passed with a 0.50 GB margin, then
Phase 8 stopped with mount-0 ok and mount-1 `uncertain` (R12-1, a host
transport fence), and Phases 11 and 12 ran cleanly. The changes are:
- the run ID (`zq3-r13`) and the native guard root
  `/var/tmp/hfs-20261119-zq3r13-fdq`;
- the xc4 candidate `e9c77e5b…`, which rebuilt only the CGO daemon
  (`c959b708…`; nc4: a retried mapped cell reuses its recorded base instead
  of fencing the guest transport, and every fence records its reason); the
  1a4 guest kernel (build ID `8c1de1ed…`), its config and modules and the
  exporter `ef8f17de…` are 5a4's bytes; the window artifacts were rebuilt;
- `hfs-runtime-prep` and the signer rebuilt (nc4 changed code both link);
- everything that pins them re-pinned, and the kernel plan regenerated for
  the new manifest digest (`rocky_kernel.py` and `controller.py` changed);
- the bundle regenerated from the unchanged committed spec with the new code;
- **N1 as zq3-r12's R1 left it**, now in a pinned helper `sh/n1-gate.py`
  (Europe/Sofia, R1's three rules plus a stamp rule, tested at fixed times),
  and Phase 1 records the upgrade stamp and the dnf history;
- **the nc4 fence record:** `sh/p11-fence.py` collects `runtime.stderr`,
  `transport.failure` and the `transport.db` failure row in the Phase 8 and
  case failure blocks and in a new Phase 11 step 1a;
- **`rockywait`** in `session.sh`: the Phase 12 (and U3) SSH wait runs in
  the session shell (zq3-r12's `bash -c "rocky true"` slip);
- **Phase 1a (D1-a) runs again:** zq3-r12 left `fdescfs.ko` and `fusefs.ko`
  loaded.

Nothing here used SSH, the network, the lab, a VM, a mount, an installation
or the operator private key. **No authority follows from these files:**
every class A step in the [run sheet](run-sheet.md) still needs Paul's
explicit final GO, including the Phase 1a unload and T7 (A8). **zq3
acceptance stays incomplete by design**, because T5 does not run.

## N1: Rocky upgrades itself once a day

Rocky's clock is `Europe/Sofia (EET, +0200)` (zq3-r12's run-time revision
R1). `unattended-upgrade-rocky.timer` fires hourly at :55 local, but the
daily mode runs `dnf upgrade` only once per local day, at 00:55 EET =
**22:55 UTC**, stamps `/var/lib/unattended-upgrade/last-daily` and never
reboots Rocky. The 2026-11-19 06:16:49Z facts (`rocky-upgrade-…txt`): stamp
`2026-11-19` (written 00:55:04 EET), newest dnf transaction still #64 of
2026-11-18 00:55, so that run changed nothing. **The run must start after
that day's run has stamped and finish before the next one, 2026-11-19
22:55 UTC.** Phase 5 step 0 runs `n1-gate.py gate` before Rocky is stopped:
`STOP` if Phase 1 predates the last 00:55 local, within 35 minutes after
it, after 23:25 local (**21:25 UTC, the latest Phase 5 start on
2026-11-19**), or if the Phase 1 stamp is not that day's; a `STOP` aborts
cleanly (observer key removed on Rocky and f3, U2, window restored). Phase 1
records the stamp and the dnf history (`n1-gate.py phase1`), and any new
transaction lets the pod-runtime and kernel rechecks decide; drift means a
refill. The facts match zq3-r12's on every Rocky pin (kernel
`741.22.1.el9_9`, python3.9 `729ac37f…`, podman, crun, conmon,
seccomp.json, `authorized_keys`).

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-zq3-r13-fill` (`0700`). This directory holds only
public hashes, paths, argv and the read-only facts. These stay private and
are never committed:
- the per-session keys;
- the runtime config, which holds host receipt private keys;
- the staging tarballs that contain it.

## Inputs

| Input | Value |
|---|---|
| Candidate | **xc4 rebuild** (task xc4): manifest `e9c77e5b…`, package `/home/paul/tmp/claude/hfs-xz3-20261118-r13/package`, source `b7d107b` (repository baseline). **Daemon `c959b708…`** (GNU build ID `eac40082…`; nc4 on top of the 5a4 code). **Reused byte for byte from 5a4 `150deb29…`:** the 1a4 guest kernel `6.1.190-hfs-xz3` (bzImage `803a35b9…`, GNU build ID `8c1de1ed…`, System.map `5989d2a5…`), config `28fe4a8e…`, the five modules and `modules.*`, and the native exporter `ef8f17de…` (`argv.py` `ed63ff97…` unchanged). Window artifacts `8194ebe0…` (test) and `8c43fd06…` (helper), rebuilt from `b7d107b`. Init helper `8c02b98f…` unchanged. **Lab tools:** `controller.py` `f6f4d93a…` and `rocky_kernel.py` `14707d33…` (admit only `e9c77e5b…`); `guest.py` `d8361fc0…`, `guest-lock-probe.py` `11521fe6…` and `pod_fixture.py` `01f8e842…` unchanged (`git diff ef92575 afe34fb -- tools/lab` touches only `controller.py`, `rocky_kernel.py`, their tests, the window build script, its plan test and the README) |
| Phase 1 facts | [facts/](facts/), collected read-only 2026-11-19 06:16:49Z; they supersede zq3-r12's 13:50:55Z set. **f3:** boot `1794574308` (unchanged since zq3-r8), **`fdescfs.ko` (id 29) and `fusefs.ko` (id 30) loaded**, no fuse/fdesc mount, free 8.68 GB + inactive 2.07 GB, ARC 4.47 GB, `/usr/bin/ssh` `6a9e5573…`, python3.12 `a66eb370…`, `10.20.0.133` on `vm-public` (members `tap0`, `re0`). **Rocky:** PID `53482` (UTC lstart `Wed Nov 18 17:24:06 2026`, zq3-r12's Phase 12 `start-rocky`), boot ID `6fe8702d-…`, running stock `5.14.0-741.22.1.el9_9` = grub default, `next_entry` empty, `boot_indeterminate=7`, SELinux Enforcing, `/.autorelabel` absent, no `hfs-xz3` file in `/boot` (zq3-r12's U2), `/boot` 328 MB free, python3.9 `729ac37f…`, `authorized_keys` `e08542dc…`. **Clock and upgrade** (`rocky-upgrade-…txt`): `Europe/Sofia (EET, +0200)`, synchronized; timer hourly at :55; stamp `2026-11-19` (00:55:04 EET); newest dnf transaction #64 (2026-11-18 00:55). **Pod runtime** (`rocky-pod-runtime-…txt`, byte-identical to zq3-r12's): podman `249e918c…` `podman-6:5.8.5-3.el9_9.x86_64`, crun `659867c7…` `crun-1.29.1-1.el9.x86_64`, conmon `1ac2a8db…` (`conmon-3:2.2.1-2.el9_8`), seccomp.json `7dd44119…` (`containers-common-5:5.8-2.el9`). The published files are the f3 Phase 1 batch, the network/modules extract, the launch facts, the pod-runtime batch and the Rocky upgrade record; they hold no key material (grep for key headers and `ssh-*` lines) |
| Host events since zq3-r12 | zq3-r12's own Rocky stop (17:19:37, ACPI) and normal start (17:24:06 UTC, vm-bhyve run 1, no relabel reboot, U2 done). Rocky's daily run at 2026-11-19 00:55 EET stamped and changed nothing. No f3 reboot since Paul's deliberate 2026-11-13 reboot |
| Launch facts | `f3-launch-facts-20261119T061649Z.json` (`042b2b55…`): `rocky.conf` `c101adf1…`, vm-run `43753bc8…`, firmware `6b48e967…` unchanged; **UEFI vars `530b541e…`** (zq3-r12: `2dd5b1ea…`); launch logged `2026-11-18T19:24:06+02:00` |
| Tap | `tap100` (bridge members `re0`, `tap0`) |
| Run ID / staging | `zq3-r13`, `/var/tmp/hfs-validation-xz3/zq3-r13` (the parent exists and holds zq3-r1 to zq3-r12) |
| Native guard root | `/var/tmp/hfs-20261119-zq3r13-fdq`, host profile `--rocky-pid 53482 --rocky-birth 'Wed Nov 18 17:24:06 2026'`, guard `576d802a…` (unchanged) |
| Window directory | `/var/tmp/hfs-fdescfs-windows/zq3-r13` (the parent exists, `root:wheel 0700`) |
| Rocky names | `/root/hfs-zq3-zq3-r13-*`, `/var/tmp/hfs-zq3-zq3-r13` (the zq3-r1 to zq3-r12 ones stay as evidence) |
| Operator public key | `2f6d909c…`; key path `…/hfs-zq3-r13-fill/operator/operator-signing-key` (empty `0700` directory, never read here) |

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | **Rebuilt** at `afe34fb`: `hfs-runtime-prep` and the signer `hfs-retained9p-operator`. The build used `-trimpath`, CGO, VCS stamps and an unmodified private clone (`vcs.modified=false`); a second build of each, forced with `-a`, gave the same bytes. Reason: `git diff 637dc11 afe34fb` over the `go list -deps` directories of both commands (the same list as r12) lists non-test changes only in `internal/guestbridge` (nc4: `endpoint.go`, `mapping.go`, `restore.go`, `terminal.go`, `transport_ledger.go`, `worker.go`, `service.go` split into `service_execute.go`, new `failure.go`; plus docs) and `internal/controlledruntime/lifecycle_evidence.go`. `go.mod`/`go.sum` are unchanged. zq3-r12's builds (prep `61a37af2…`, signer `a7f1f847…`) are retired. G3 init helper: zq3-r1's reproduced build | prep `2381f504…`, signer `bba12e52…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from `testdata/zq3-spec.json` at `afe34fb` (`make-spec.py`: only `zq3-r1` → `zq3-r13` and the r13 operator key path change; the same method reproduces zq3-r12's spec from the same testdata byte for byte). The committed spec and `internal/runtimeprep` are unchanged since r12 | spec `c6d94094…`, request template `846fb8b2…`, runtime config `9a2c7fb3…`, volume-init `0434bf2e…`; volume `c2a5c018-…`; sessions hfs0 `791ef945-…`, hfs1 `a28f9517-…` |
| 1. Argv | `argv.py generate` from the 06:16:49Z launch facts (sessions, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `2c66ecaf…`, args `880f08a3…`, vars `530b541e…` |
| 2. Kernel plan | `rocky_kernel.py plan` at main (`14707d33…`; admits only `e9c77e5b…`), stock default `/boot/vmlinuz-5.14.0-741.22.1.el9_9.x86_64` (the fresh grub default, which Rocky runs). Against zq3-r12's it differs only in `run_id` and `manifest_sha256`: the kernel bytes are the same. The 5a4 package `150deb29…` is refused as superseded ("use the xc4 rebuild") | plan `bdae6578…`, build ID `8c1de1ed…` |
| 3. Controller batch | Template derived from zq3-r12's by `derive-templates.py`: run ID, fill path, sessions, candidate `150deb29…` → `e9c77e5b…`; probe `guest.py` `d8361fc0…` unchanged. **T5 stays deferred** with the r9 reason (unchanged). The four other requests keep canonical order. `controller.py` `f6f4d93a…`. `check` refuses the template as designed (`probe/installed-manifest pin`). A synthetic copy with zero IDs **passes** and prints `cases_planned` (four) and the deferral. The same copy is **refused** with the 5a4 candidate (superseded), without the declaration ("five-case syscall batch minus declared deferrals") and with `credentials` also deferred ("case may not be deferred") | template `ba93a244…` |
| 4. Bootstrap plan | Template derived from zq3-r12's (the one that ran). New pins: bundle, kernel plan, `rocky_kernel.py`, the nc4 daemon `c959b708…` and the rebuilt prep `2381f504…`. Unchanged: `bootstrap_remote.py` `0dcb0fe0…` (`remote_sha256`), the guard pin `fece96b3…`. The script re-hashes every pinned file at main and in the package. The unfilled template refuses as designed (`observer.ssh_sha256: SHA256 hex`). **F1 filled now** with `fill_plan.py bootstrap` (`893350cf…`) from the 06:16Z `sha256 -r /usr/bin/ssh` line (`6a9e5573…`), then `bootstrap.py check` (`5e467fe6…`, journal `/3`) **pass** | template `2c84e3e2…`, filled plan `f1909ab1…` |
| 5. Guard argv | Derived from zq3-r12's, with `--runtime-sha256 c959b708…` (checked against the package) and `--disposable-validation` exactly once, last. It equals `bootstrap.py`'s own `SshRemote.guard_argv()` for the filled plan, in shell string and argv, and passes `runtime-guard.py`'s `parse_args` and `check_command` | `216ca2e7…`, in [pins](pins.json) |
| 6. Pod template (e94) | zq3-r12's `homelabfs-retained9p-pod/2` template with the run ID renamed. `derive-templates.py` compares its `runtime_nevra` `{"podman": "podman-6:5.8.5-3.el9_9.x86_64", "crun": "crun-1.29.1-1.el9.x86_64"}` and the four `runtime_sha256` values with the facts' `== pod-runtime` section (byte-identical to zq3-r12's), cross-checks the hashes with the Phase 1 `sha256sum` lines, and runs the committed `pod_fixture.py`'s own `check_nevra_pins` and `parse_request` (synthetic fill): **both accept**. `parse_request` refuses a `pod/1` request without `runtime_nevra`, version `/1` with `runtime_nevra`, swapped NEVRAs, a `podman-docker` sibling, an extra conmon NEVRA and a stale fixture pin | pod template `9fe58dda…` |
| 7. Observer and staging pins | Unchanged script pins: `observer_key.py` `cd7f38c2…`, `observer.py` `2e1449dd…`, `installed.py` `c824b183…`, native guard `576d802a…`, `runtime-guard.py` `fece96b3…`, `fdescfs-window.py` `b00b19d1…`, `host.py` `738e1ca5…`, `host_snapshot.py` `b29ffed2…`, `argv.py` `ed63ff97…`, `guest-lock-probe.py` `11521fe6…`, `pod_fixture.py` `01f8e842…`. Re-pinned to main: `rocky_kernel.py` `14707d33…`. All 17 f3 and 18 Rocky files went into two deterministic tarballs; a second tar run gave the same hashes. A verification extract matched every expected hash (35) and the f3 modes (owner `0/0`, no parent entry) and was then deleted | f3 tar `de03f94c…`, Rocky tar `cb239d70…` |
| 8. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` at main (`750424db…`, as xc4 left it) with `zq3-r1` → `zq3-r13`; `check_pins` passes. Synthetic fills, which are not pins: zq3-r8's real Phase 4 facts and host hashes (modules absent) **pass**, with paths renamed and the three artifact hashes swapped for the xc4 ones. The same facts with the 5a4 artifact hashes zq3-r12 staged **refuse** (`pinned artifact differs or missing`). Post-restore facts with both modules loaded, the state on f3 now, **refuse** (`fdescfs module present`): hence Phase 1a | r13 pins `114b6daa…` |
| 9. Signing watcher | zq3-r12's `bin/sign_watch.py` (it signed both requests in zq3-r12) with `D`, the docstring run ID, the signer comment and **the signer pin `bba12e52…`** changed. Tested locally with a fake signer: a wrong request hash, a wrong command, an unknown action, a repeat and a wrong signer hash were refused; both exact requests were signed | `9d5baaa4…` |
| 10. Process observer check | `sh/p4-observer.py` from zq3-r12 with the run ID and **the daemon pin `c959b708…`** changed (Phase 4 step 1a, **required**: new daemon bytes). zq3-r12's real records (renamed) are refused only for the 5a4 `sha256` and pass with `c959b708…` substituted. The `$NG` line parses and passes the native guard's own `parse_args` and `check_request` | `1739c6a2…` |
| 11. N1 gate helper (new) | `sh/n1-gate.py`: `phase1` parses the Phase 1 upgrade record (stamp, newest dnf transaction, `dnf_unchanged` against #64); `gate` is zq3-r12's R1 gate (Europe/Sofia; Phase 1 before the last 00:55 local, within 35 min after it, after 23:25 local) plus the stamp rule. Tested at fixed times (`--now`, tests only): 08:30Z GO (also with the real upgrade record), 21:20Z and 21:25Z GO, 21:30Z, 22:56Z and 23:10Z STOP, the next day with the old Phase 1 STOP, the next day with a fresh Phase 1 and stamp GO, a stale stamp STOP; `phase1` reports `dnf_unchanged` true for the facts and false for a synthetic #65 | `3ac1eb91…` |
| 12. Fence record helper (new) | `sh/p11-fence.py` (nc4): the `guestbridge: transport failed at …` stderr line, `transport.failure` in any OBSERVE/JSON file and the `failure` row in `transport.db` (raw or inside a tar). Tested on a real nc4 row and stderr line written by the committed `guestbridge` code (a temporary test in the private clone, removed afterwards; `git status` clean), on zq3-r12's real evidence (`fenced` false: pre-nc4, no record) and, since review round 1, on a synthetic `bootstrap-run/calls/*.json` record whose `stdout` holds the OBSERVE reply as a JSON string (found) | `3639529a…` |
| 13. Run sheet | [run-sheet.md](run-sheet.md): Phases 0-12, 1a (**runs**), 4/1a and U1-U3, N1, the SELinux gates in Phases 1, 9 and 12, the R11 and R12-1 checks, the fence record capture, `rockywait`, and a run log template. `session.sh` (`b788efe4…`, `rockyt` bound 270 s, new `rockywait`) and fifteen pinned helper scripts (the r12 ones, renamed; `p4-observer.py` changed; `n1-gate.py` and `p11-fence.py` new); `p10-requests.py` re-pins the three r13 request templates. The Phase 8-10 lines were dry-run end to end (below) | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-19T00:00:00Z --format=gnu`.
The f3 tarball holds only `hfs-20261119-zq3r13-fdq` and
`hfs-validation-xz3/zq3-r13`. It has **no entry for the existing parent**
`hfs-validation-xz3/`, so extraction never rewrites the parent's mode or
owner. All directories are `0700`, binaries `0500`, other files `0400`,
owner `0/0`.

## Signing

Paul delegated both signatures (A7) to Claude. In Phase 8 the driver's
stdout goes through `bin/sign_watch.py` (`9d5baaa4…`). The watcher runs the
signer (`bba12e52…`) only if:
- the printed command is exactly one of the two below;
- the request file's SHA256 equals the printed `request_sha256`;
- each command runs at most once.

Anything else is refused, and the driver then times out (fails closed):

```
/home/paul/tmp/claude/hfs-zq3-r13-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r13-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r13-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r13-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r13-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r13-fill/bootstrap-run/work/signer-binding
```

On Paul's instruction, Claude places the operator key at the bundle's path
before the run. Nothing in the fill reads it.

## Run-time fills

| Fill | When and source | Used by |
|---|---|---|
| N1 | Phase 1: Rocky's time zone (`timedatectl`), the upgrade stamp and dnf history (`p1-upgrade.txt`, `n1-gate.py phase1`), boot ID, running/default kernel, `next_entry`, python3.9, `authorized_keys` and the pod runtime (`p1-pod-runtime.txt`, `pod-runtime-unchanged: True`); any drift stops for a refill. Phase 5 step 0: `n1-gate.py gate` | Phase 1 Rocky gate, Phase 3 install, Phase 5 go/no-go, T7 |
| D1 | **D1-a, Phase 1a**: both modules are loaded (06:16Z); Phase 1 confirms it, Phase 1a unloads them normally (never `-f`), and a refused unload stops before staging | Phase 4 step 0 window plan fill (needs both absent) |
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 06:16Z fact); Phase 4 step 0 rechecks it (`F1-unchanged`), a change stops | bootstrap `observer.ssh_sha256` (plan `f1909ab1…`) |
| F2 fdescfs window plan and its hash (`WP`) | Phase 4 step 0: controller `facts`, host hashes, `realpath` library closure, f3 `date -u +%s`, then `fdescfs-window-plan.py --pins` with the r13 pins | setup, qualify, restore (`W`); Phase 5 recheck |
| F3 pod rootfs manifest digest (`PM`) | Phase 3: `pod_fixture.py manifest` on Rocky (zq3-r12: `523485cc…`; no dnf transaction since, so expected again); `skip` only if A8 is declined | `build`, T7 request |
| F3a process observer record | Phase 4 step 1a: guard stage `process-observer`, checked by `p4-observer.py` | go/no-go before setup; **pass required** (new daemon bytes) |
| F4 observer key pair, enrolled line, `after_sha256` (`EA`) | Phase 4 step 5: `ssh-keygen` on f3, `observer_key.py enrol` | runtime observer, Phase 11 `remove --expect-sha256`, banner-timeout check |
| F5 BLS entry ID | Phase 3 install record | `select` (from the state directory) |
| F6 boot ID, provider nonce, admission manifest SHA256, `virtioN` names, proposal, controller, provision hash | Phase 8 admission and runtime records | enrollment and bind requests (bootstrap fills them), controller batch plan |
| F7 m0/m1 mount IDs | Phase 8 mount steps (mountinfo) | batch plan, then `p10-requests.py` for the T6/T7/T8 requests |
| F8 batch plan and its hash (`B`) | Phase 8, after READY and `usable`: `fill_plan.py batch` (prints `cases_planned` and `deferred`), then `check-retained9p.sh check` | every controller and correlator call |
| F9 runtime UUID (`U`), `L`, cut, epoch and lifecycle sequences | READY record and `summary.json` via `p9-ready.py` (`--runtime`, `--observe-last`); Phase 9 uses L+1 and the T1 baseline L+2, then each from `correlate.py command` (`obs`, ARM, RESTART); expected L+1 to L+14 with T5 deferred | Phase 9 OBSERVE, T1-T4, T9-T11 |
| F10 exporter child PID (`XP`) | guard `receipt.json` `exporter.pid` (Phase 11 step 0; the `--no-child` line if null) | `host.py cleanup --child-pid` |
| F11 Rocky PID and lstart | **fixed** from the 06:16Z facts (`53482`, `Wed Nov 18 17:24:06 2026` UTC, zq3-r12's Phase 12); rechecked in Phases 1, 4 (also by every `$NG` stage) and 5 | native guard host profile, Phase 5 recheck, `p5-mem.txt` RSS |
| F12 UMA drain decision | Phase 5 step 1a: `p5-estimate.py` `drain_recommended` | Phase 5 step 1b (or once in Phase 6) |
| F13 T6/T7/T8 request files | Phase 8 after the batch check: `p10-requests.py` (exclusive, `0600`, in `$E`); the T7 request is pod `/2` with `runtime_nevra` | T6, T7, T8 |
| F14 guard PID | Phase 11, only on the runtime-still-serving path (`pgrep -U root`) | the one SIGTERM to the guard |
| F15 SELinux state | Phase 1 (`/.autorelabel` absent, Enforcing), Phase 9 (guest `getenforce` `Enforcing`, enforce `1`), Phase 12 (relabel record) | Phase 1 and Phase 9 gates; Phase 12 finding |
| F16 984 timing | Phases 9-10: `tf3` lines in `$E/obs-timing.tsv` (three `ssh-noop`, each timed OBSERVE), checked with a local `cat` after the Phase 9 OBSERVE; `p9-timing.py` after T4 and before Phase 11 | run log only; never a gate |
| F17 admission-step timings | Phase 10: `p10-admission.py` over the four cases' `stdout.log` after T4 (`p10-admission-steps.jsonl`; `deadline_hit`, `etimedout`), or over a failed case's before Phase 11 | run log only; never a gate |
| F18 transport fence record (new) | `p11-fence.py`: Phase 8 failure block (`p8-fence.json`), the case-failure block (`t-fail-fence.json`) and always Phase 11 step 1a (`p11-fence.json`) over `runtime.stderr`, the owner snapshots/`f3-evidence.tar` (`transport.db`) and every OBSERVE taken | run log; `fenced: true` is a finding, never a retry |

## Changes from the zq3-r12 sheet

All zq3-r7 to zq3-r12 rules are kept:
- **Phase 1a (D1-a): runs again**, as in zq3-r3 to zq3-r7 and zq3-r9 to
  zq3-r12: a normal `kldunload fusefs`, then `kldunload fdescfs`, never `-f`;
- Phase 4 step 1a, the process observer check: **required**, because the
  daemon bytes changed (`c959b708…`);
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

Kept from the zq3-r5 to zq3-r12 executions and the review decisions:
- **Phase 11 step 9 is one plain line with literal paths**:
  `f3 "doas -n rm -- /var/tmp/hfs-validation-xz3/zq3-r13/observer/id_ed25519 /var/tmp/hfs-validation-xz3/zq3-r13/observer/id_ed25519.pub"`.
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
  snapshots; never chain a follow-up after a bounded command (zq3-r11).
- From zq3-r12: the e94 pod `/2` request with `runtime_nevra`, the
  `etimedout` flag and the Phase 11 ETIMEDOUT scan, every guest block-line
  grep matching `view control worker stopped` and `timed out behind a
  recall`, and run-time revision R1 (Europe/Sofia).

New for zq3-r13:
- **Candidate `e9c77e5b…`** in the kernel plan, batch template, bootstrap
  plan, guard argv, window pins, staging, Phase 3, Phase 4 step 1a and
  Phase 8 expectations.
- **A new "Execution notes from the zq3-r12 run" section** (R12-1, R1, the
  operator slip, what is kept).
- **N1 gate as a helper:** `n1-gate.py` replaces R1's `python3 -c` line;
  Phase 1 adds the upgrade record (`p1-upgrade.txt`/`.json`) and the stamp
  rule; the latest Phase 5 start on 2026-11-19 is 21:25 UTC.
- **R12-1 check and the fence record:** Phase 8's expectations and failure
  block, the case-failure block, a Phase 10 paragraph, Phase 11 step 1a
  (always) and the T10 note that a restore refuses a fenced ledger
  (expected, recorded, never worked around).
- **The r12 lesson as a rule:** never call a session function inside `bash
  -c`; Phase 12 and U3 wait with `rockywait` in the session shell (tool
  timeout at least 900 s).
- **Review round 1:** the early Phase 1 gate and the in-run cutoff (below).

## Decisions from the zq3-r13 run-sheet review round 1 (2026-11-19)

No blocking issues. Applied to this fill and the public copy:
1. **`p11-fence.py` reads nested replies:** `bootstrap-run/calls/*.json`
   keep a remote reply as a JSON string in `stdout` (`bootstrap.py`), so a
   `transport.failure` in a `usable` gate OBSERVE sat one encoding level
   down and was missed. String fields that start with `{` or `[` are now
   decoded and scanned. A synthetic call record with a failure in its
   `stdout` is found; the real nc4 row and zq3-r12's evidence give the same
   results as before. New hash `3639529a…` (`session.sh` hpin, pins).
2. **In-run cutoff:** no new case or lifecycle step after **22:15 UTC on
   2026-11-19**; then Phase 11 at once, with its step 5 guest poweroff
   before **22:45 UTC**, so Rocky's daily `dnf upgrade` at 22:55 UTC never
   runs against Rocky's disk while the test guest boots it. Stated in the
   Phase 5 gate text and at the top of Phases 8, 9 and 10 (with a 22:35 UTC
   fallback: observer removal and poweroff at once, skipped evidence steps
   recorded).
3. **Case-failure fence capture:** the block takes `$E/$CASE-owner-live.tar`
   first and passes it to `p11-fence.py` (the snapshot carries
   `transport.db`).
4. **`rockywait 36`** can take about 720 s: its lines (Phase 12, U3) need a
   tool timeout of at least 900 s; `session.sh` (`b788efe4…`) says so.
5. **Early gate:** Phase 1 runs `n1-gate.py gate` right after the upgrade
   record (`p1-nightly-gate.txt`), so a `STOP` comes before Phase 1a
   changes anything; Phase 5 step 0 still runs it again.

Rechecked: the sheet dry run (80 lines, all as expected), the helper dry
run (38 cases), the fence tests and `shellcheck` of `session.sh`.

## Expectations: what changed since zq3-r12

zq3-r12 reached admission, both signatures and mount-0 with the exact g64
tuple, then failed:
- **R12-1:** mount-1 `uncertain`; at guest 23.5 s both sessions were
  `blocked by uncertain control result on VIEW_WAIT (-71)` with worker lines
  `host status -`. The host runtime's guest transport fenced itself (a
  retried mapped cell fenced the whole transport), the exporter turned the
  failed fetches into ERROR packets and the guest mapped them to `-EPROTO`.
  `runtime.stderr` was empty.

xc4 changes (nc4, daemon only):
- a retried mapped cell reuses the base response recorded for it, so a
  transient WouldBlock while signing no longer fences the transport;
- every fence records its first reason: one `runtime.stderr` line,
  `transport.failure` in the lifecycle evidence and a `failure` row in
  `transport.db`; a guestbridge restore refuses a ledger with that row.

So mount-1 should succeed, and READY, `usable`, Phase 9 and T1-T4 should
proceed; T6, T7 and T9-T11 (never run for real) should run. If any fence
happens, the run records its reason (`p11-fence.py`) and stops the matrix;
a restore refusal after it is expected and recorded. Remaining known risks,
not fixed: **oc4** (the guest's `hfs_transport` retry loops ignore
`session->blocked`; ERROR maps to `-EPROTO`), **z94** (self-started
all-domain recalls during admission, survivable since y94) and **0a4** (the
host never drops a participant that stops acknowledging). Any `ETIMEDOUT`,
any `view control worker stopped` line and any `initial attributes timed
out behind a recall` line is still recorded as a finding.

**Timing check against the 90-120 s getattr worst case** (unchanged since
zq3-r12; `docs/Test-automation.md`): guest.py's 10 s admission alarm, the
plan's 60 s case alarm and the controller's 75 s dispatch bound are all
shorter, so a slow syscall ends as a `Deadline` or `dispatching-uncertain`
(findings); `rockyt`'s 270 s fits one worst-case getattr plus the unmount's
own 30 s view wait; an `rc=124` is a finding, never retried.

## Phases 9-10 recheck

Since zq3-r12, among the tools the sheet uses, only `controller.py`
(`f6f4d93a…`, admits only `e9c77e5b…`) and `rocky_kernel.py` (`14707d33…`)
changed. `bootstrap.py` (`5e467fe6…`), `bootstrap_remote.py` (`0dcb0fe0…`),
`fill_plan.py` (`893350cf…`), `correlate.py` (`9cd55f2c…`), `guest.py`
(`d8361fc0…`), `guest-lock-probe.py` (`11521fe6…`) and `pod_fixture.py`
(`01f8e842…`) are unchanged. No CLI the sheet uses changed shape.

**Sheet dry run** (`dryrun/` in the private fill; scratch output). A
harness extracted the 77 command lines of this sheet from Phase 8 "After
READY" to T8, plus the Phase 7 `k04` baseline line and the two Phase 11
evidence lines (the 984 summary and the ETIMEDOUT scan; 80 in all), from
`run-sheet.md` itself and ran them in order. The case-failure block (it
names `$CASE`), the T8 hang capture (a text block) and the Phase 8 failure
block (before "After READY") only run on a failure and are skipped. The
harness used:
- the real `fill_plan.py batch`, `check-retained9p.sh check`,
  `correlate.py`, `session.sh` (`tf3`, `obs`, `sha`, `jf`) and the `sh/`
  helpers;
- `controller.py` with faked guest dispatch, whose case output carries the
  y84 `admission-step` events before `admitted`;
- stub `f3`/`rocky` that answer like the lifecycle client (evidence `/2`
  with a claim summary), `host_snapshot.py` and the guest; the Rocky stub
  parses the uploaded T7 request with the committed `pod_fixture.py`'s own
  `parse_request`;
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
- `p10-admission.py`: `etimedout` false for a clean case and for a string
  `"110"` errno; true for errno 110 in a failure, a `reply` event, a
  `step-observed` `mkdir_errno`, a nested `lstat_errno` and a lock-probe
  report;
- `p9-ready.py` refused a wrong socket, a missing or failed `usable`
  result, a `/2` journal header, a missing `observe_sequence_last`, L = 1,
  and a `usable`/summary L mismatch;
- `p9-observe.py` refused sequence 1, L+2, a READY_JSON without L, a fenced
  admission, a rotated cut, an incomplete OBSERVE, evidence `/1` and a wrong
  plan hash;
- `p9-timing.py` refused a malformed TSV and one without an OBSERVE line;
- `p10-argv.py` and `p10-requests.py` refused as in r12;
- `fill_plan.py batch` refused a synthetic run that stopped at `usable`
  with the b84 claims note (`bootstrap steps did not end ok:
  usable=failed`), a `/2` journal and zq3-r12's real run (`journal is not
  for the pinned bootstrap plan`); `p9-ready.py` refused the failed run and
  zq3-r12's real run directory (it stopped at mount-1: `summary.json
  outcome is not ready`); `p10-admission.py` on zq3-r11's real T1 log still
  reports `deadline_hit` true and the 43.6 s `open-root` gap.

## Findings and open questions

- **Q0 N1.** The run must start after the 2026-11-19 daily run (stamped
  00:55:04 EET) and finish before 2026-11-19 22:55 UTC; the latest Phase 5
  start is 21:25 UTC, no new case or step starts after 22:15 UTC, and the
  guest is powered off before 22:45 UTC (review round 1). A later attempt needs fresh Phase 1 facts (the gate
  refuses an old Phase 1), and a new dnf transaction lets the pod/kernel
  rechecks decide (drift means a refill).
- **Q1 T5 deferred.** The run cannot complete zq3 acceptance. T5 needs a
  re-plan and sizing first.
- **Q2 the nc4 fix is untested on f3.** The daemon `c959b708…` passed
  offline tests only (xc4 admission). The main questions are Phase 4 step
  1a, mount-1 (R12-1), `usable`, and T1-T4 admission with z94's
  self-started recalls. oc4 is not fixed: a block during a mount can still
  leave it `uncertain` after about 30 s.
- **Q3 admission time vs bounds.** Unchanged from zq3-r12: one getattr can
  take about 90-120 s; the 10 s admission alarm, the 60 s case alarm and
  the 75 s dispatch bound are all shorter. Raising them is a code/plan
  change, not done here.
- **Q4 T7 first real run.** T7 has never run for real. The rootfs manifest
  should again be `523485cc…` (no dnf transaction since zq3-r12); a
  different digest must be explained before `build`.
- **Q5 memory.** At 06:16Z, about 13 hours after Rocky's start, free was
  8.68 GB, inactive 2.07 GB and ARC 4.47 GB (free + inactive 0.39 GB lower
  than at zq3-r12's fill). Phase 5's estimate and the Phase 6 bar decide
  (zq3-r12: active_based margin 0.45 GB, Phase 6 margin 0.50 GB after the
  drain).
- **Q6 UEFI vars drift.** The record pins `530b541e…`. Any Rocky boot before
  Phase 7 changes it, and `host.py setup` then refuses. Recheck right before
  the run.
- **Q7 Phase 1a.** zq3-r3 to zq3-r7 and zq3-r9 to zq3-r12 unloaded both
  modules without refusal. A refusal now stops before staging and needs a
  plan revision.
- **Q8 window pins file:** the r13 window pins live only in the private fill,
  with their hash recorded here.
- **Q9 T6, T7, T9-T11:** never run for real; the T9 ARM, T10 FENCED step 2
  and the T11 receipt wait (5 minutes) have only been dry-run on the
  current tools. After a fence, T10's restore would refuse the ledger (by
  design).
- **Q10 the fence record in practice.** `p11-fence.py` reads the
  `transport.db` row from raw bbolt pages; it was checked on a real row
  written by the committed code, but a fence on f3 has never been recorded.
  Whether a lifecycle FENCED (T10) also leaves a transport failure record is
  not established; the sheet tells the operator to judge such a record by
  its `Time`.

No committed tool refused real inputs. These intended refusals all
occurred:
- the controller template;
- the unfilled bootstrap template;
- the superseded 5a4 `150deb29` candidate in a synthetic batch plan and in
  `rocky_kernel.py plan`;
- an undeclared four-case batch and a deferred `credentials`;
- the window filler on the 5a4 artifact hashes and on facts with both
  modules loaded;
- `pod_fixture.py parse_request` on the malformed pod requests above;
- `n1-gate.py gate` at the STOP times above.

## Local verification

At `afe34fb` in the main checkout, all passing:
- retained9p: 381 tests;
- directlaunch: 26 tests;
- `tools/lab` top level: 248 tests (3 skipped);
- `go test -count=1 ./internal/runtimeprep`.

`shellcheck` is clean for `build-stage.sh` and `sh/*.sh` (as POSIX sh), and
for `session.sh` apart from SC2034/SC2086: its variables are used by the
sheet, and `$O` is split on purpose. No code was changed.
