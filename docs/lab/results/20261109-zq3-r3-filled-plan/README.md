# zq3-r3 filled run inputs (2026-11-09)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r2 fill](../20261109-zq3-r2-filled-plan/README.md) ·
[zq3-r2 record](../20261109-zq3-r2/README.md)

The exact run inputs for `zq3-r3`, the third attempt of zq3, generated on the
workstation with the committed tools at `main` `1eba12d` (after o54, p54,
q54, r54 and t54). They mirror the
[zq3-r2 fill](../20261109-zq3-r2-filled-plan/README.md), which ran for real.
The changes are the run ID, the r54 candidate, the fresh facts, the main
bytes of every re-pinned script, and the zq3-r2 lessons R2-1 to R2-6. Nothing
here used SSH, the network, the lab, a VM, a mount, an installation or the
operator private key. **No authority follows from these files:** every class
A step in the [run sheet](run-sheet.md) still needs Paul's explicit final GO.

**Open decision D1 (blocks Phase 4).** `fdescfs.ko` and `fusefs.ko` are still
loaded on f3, left there by zq3-r2 (same boot). `fdescfs-window-plan.py`
requires both absent, and against that state it refuses with `fdescfs module
present; plan is for absent`. The fill reproduced this locally with zq3-r2's
post-restore window facts. The [run sheet](run-sheet.md#open-decision-d1-f3-module-state-paul-before-the-go)
lists three options: unload both modules first (D1-a, the sheet's default,
needs explicit approval), a reviewed tool change (D1-b), or an f3 reboot
(D1-c). D1-b and D1-c are plan revisions.

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-zq3-r3-fill` (`0700`). This directory holds only
public hashes, paths, argv and the read-only facts. The per-session keys, the
runtime config (it holds host receipt private keys) and the staging tarballs
that contain it stay private and are never committed.

## Inputs

| Input | Value |
|---|---|
| Candidate | r54 kernel rebuild: manifest `5eb42c50…`, package `/home/paul/tmp/claude/hfs-xz3-20261109c/package`, source `ca920a2`; kernel `6.1.190-hfs-xz3` bzImage `98cc02d5…`, System.map `69b6c7bc…`, build ID `1f7808b8…`. Daemon `174949c1…`, bhyve `36423b6d…`, config, modules, window artifacts and init helper `8c02b98f…` unchanged |
| Phase 1 facts | [facts/](facts/), collected read-only 2026-11-09 14:11:51Z. f3: boot `1794212992` (unchanged since zq3-r2), `fdescfs.ko` and `fusefs.ko` loaded with no fuse/fdesc mount, free 4.73 GB + inactive 2.93 GB, `/usr/bin/ssh` `6a9e5573…`. Rocky: PID `45407` (UTC lstart `Mon Nov  9 13:00:32 2026`), boot ID `4568a037-…`, stock `5.14.0-687.66.1.el9_8.x86_64`, `next_entry` empty, `boot_indeterminate=3`, python3.9 `1143733d…`, `authorized_keys` `e08542dc…`. The files hold no key material (grep for key headers and `ssh-*` lines) |
| Launch facts | `f3-launch-facts-20261109T141151Z.json` (`2a9c3c4c…`): `rocky.conf` `c101adf1…` unchanged, **UEFI vars `c71240e9…`** (zq3-r2: `cab0ac2c…`; Rocky has booted since) |
| Tap | `tap100` (bridge members `re0`, `tap0`) |
| Run ID / staging | `zq3-r3`, `/var/tmp/hfs-validation-xz3/zq3-r3` (parent exists, holds zq3-r1 and zq3-r2) |
| Native guard root | `/var/tmp/hfs-20261109-zq3r3-fdq`, host profile `--rocky-pid 45407 --rocky-birth 'Mon Nov  9 13:00:32 2026'`, guard `576d802a…` (o54 floors) |
| Window directory | `/var/tmp/hfs-fdescfs-windows/zq3-r3` (parent exists, `root:wheel 0700`) |
| Rocky names | `/root/hfs-zq3-zq3-r3-*`, `/var/tmp/hfs-zq3-zq3-r3` (zq3-r1's and zq3-r2's stay as evidence) |
| Operator public key | `2f6d909c…`; key path `…/hfs-zq3-r3-fill/operator/operator-signing-key` (empty `0700` directory, never read here) |

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | zq3-r2's `hfs-runtime-prep` and `hfs-retained9p-operator` reused: they were built from `ea55eb5`, and `git diff ea55eb5 1eba12d -- internal cmd go.mod go.sum` touches only `internal/guestbridge/README.md`, which is not embedded. G3 init helper: zq3-r1's reproduced build (`build-init-helper.sh` unchanged since `08831e7`) | prep `22de7965…`, signer `22e5fb90…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from zq3-r2's spec with `zq3-r2` → `zq3-r3` and the r3 operator key path (equal to `testdata/zq3-spec.json` apart from run ID and key path); actual-runtime validation passed | spec `e8ace617…`, generation `def84cc2…`, request template `cd031c4d…`, runtime config `97b3639a…`, volume-init `a2dc2c8c…`; volume `071f5579-…`; sessions hfs0 `e53114c2-…`, hfs1 `d1694f05-…` |
| 1. Argv | `argv.py generate` from the 14:11Z launch facts (sessions, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `51117909…`, args `ba338e90…`, vars `c71240e9…` |
| 2. Kernel plan | `rocky_kernel.py plan` (main, t54 admits only `5eb42c50…`), stock default `/boot/vmlinuz-5.14.0-687.66.1.el9_8.x86_64`; differs from zq3-r2's in run ID, manifest, bzImage and System.map hashes and build ID | plan `a3f5feea…`, build ID `1f7808b8…` |
| 3. Controller batch | template derived from zq3-r2's (run ID, fill path, sessions, candidate `5eb42c50…`); `check` refuses as designed (`probe/installed-manifest pin`); a synthetic copy (zero IDs) passes the main `controller.py` | template `a51e10c1…` |
| 4. Bootstrap plan | template derived from zq3-r2's q54 template, the one that ran. It carries new bundle, kernel plan and `rocky_kernel.py` (`35ef1f1c…`) pins; the guard pin `fece96b3…` is the main `runtime-guard.py`. `derive-templates.py` re-hashes every pinned file at main. Unfilled template refuses as designed. **F1 filled now** with `fill_plan.py bootstrap` from the 14:11Z `sha256 -r /usr/bin/ssh` line, then `bootstrap.py check` **pass** | template `ce62c979…`, filled plan `077cfe66…` |
| 5. Guard argv | derived (with `--disposable-validation`, q54), then compared with `bootstrap.py`'s own `SshRemote.guard_argv()` for the filled plan: equal, and it matches `runtime-guard.py`'s `check_command` shape | in [pins](pins.json) |
| 6. Observer and staging pins | unchanged script pins (`observer_key.py` `cd7f38c2…`, `observer.py` `2e1449dd…`, `installed.py` `c824b183…`). Re-pinned to main: `native-test-guard.py` `576d802a…`, `runtime-guard.py` `fece96b3…`, `rocky_kernel.py` `35ef1f1c…`, `argv.py` `73520362…`. All 17 f3 and 18 Rocky staged files went into two deterministic tarballs (a rebuild gave the same hashes), and a verification extract matched every expected hash. Package files were also checked against the manifest | f3 tar `982c27b3…`, Rocky tar `87b7c8d6…` |
| 7. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` with `zq3-r1` → `zq3-r3` (equal to the r2 pins renamed); `check_pins` passes. Synthetic fills (not pins): zq3-r2's Phase 4 facts renamed **pass**; zq3-r2's post-restore facts (the current f3 module state) **refuse**, see D1 | r3 pins `1e3305fb…` |
| 8. Signing watcher | zq3-r2's `bin/sign_watch.py`: only `D`, the docstring run ID and a comment on the reused signer changed. Signer pin `22e5fb90…` in the watcher and in `hpin`. Tested locally with a fake signer: a wrong request hash, a wrong command, a repeat, an unknown action and a wrong signer hash were refused; both exact requests were signed | `03be0b7e…` |
| 9. Run sheet | [run-sheet.md](run-sheet.md), D1 plus Phases 0-12 and U1-U3. `session.sh` (`85e6bfee…`) and six pinned helper scripts, two of them new (`p5-estimate.py`, `p5-uma-stat.sh`) | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-09T00:00:00Z --format=gnu`.
The f3 tarball holds `hfs-20261109-zq3r3-fdq` and `hfs-validation-xz3/zq3-r3`
only, **without an entry for the existing parent** `hfs-validation-xz3/`, so
extraction never rewrites the parent's mode or owner. All directories `0700`,
binaries `0500`, other files `0400`, owner `0/0`.

## Signing

Paul delegated both signatures (A7) to Claude. In Phase 8 the driver's stdout
goes through `bin/sign_watch.py` (`03be0b7e…`). It runs the signer
(`22e5fb90…`) only if the printed command is exactly one of the two below and
the request file's SHA256 equals the printed `request_sha256`, each at most
once. Anything else is refused, and the driver then times out (fails closed):

```
/home/paul/tmp/claude/hfs-zq3-r3-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r3-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r3-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r3-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r3-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r3-fill/bootstrap-run/work/signer-binding
```

The operator key is placed at the bundle's path by Claude before the run, on
Paul's instruction. Nothing in the fill reads it.

## Run-time fills

| Fill | When and source | Used by |
|---|---|---|
| D1 | **before the GO**: Paul chooses D1-a (unload, Phase 4 step 0a), D1-b or D1-c | Phase 4 step 0 window plan fill |
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 14:11Z fact); Phase 4 step 0 rechecks it (`F1-unchanged`), a change stops | bootstrap `observer.ssh_sha256` (plan `077cfe66…`) |
| F2 fdescfs window plan and its hash | Phase 4 step 0: controller `facts` (needs the staged inspector), host hashes, `realpath` library closure, f3 `date -u +%s`, then `fdescfs-window-plan.py --pins` with the r3 pins. **Refuses unless D1 is settled** | setup, qualify, restore; Phase 5 recheck (`owned.json`) |
| F3 pod rootfs manifest digest | Phase 3: `pod_fixture.py manifest` on Rocky (zq3-r1 and zq3-r2: `f7f69339…`) | `build`, pod request |
| F4 observer key pair, enrolled line, `after_sha256` | Phase 4 step 5: `ssh-keygen` on f3, `observer_key.py enrol` | runtime observer, Phase 11 `remove --expect-sha256`, R2-5 retry check |
| F5 BLS entry ID | Phase 3 install record | `select` (from the state directory) |
| F6 boot ID, provider nonce, admission manifest SHA256, `virtioN` names, proposal, controller, provision hash | Phase 8 admission and runtime records | enrollment and bind requests (bootstrap fills them), controller batch plan |
| F7 m0/m1 mount IDs | Phase 8 mount steps (mountinfo) | batch plan, lock-probe requests, pod request |
| F8 batch plan and its hash | Phase 8, after READY: `fill_plan.py batch`, then `check-retained9p.sh check` | every controller and correlator call |
| F9 runtime UUID and lifecycle sequences | READY record, then each reply | OBSERVE, ARM, RESTART |
| F10 exporter child PID | runtime and guard evidence | `host.py cleanup --child-pid` |
| F11 Rocky PID and lstart | **fixed** from the 14:11Z facts (`45407`, `Mon Nov  9 13:00:32 2026` UTC); rechecked in Phases 1, 4 and 5 | native guard host profile, Phase 5 recheck, `p5-mem.txt` RSS |
| F12 UMA drain decision | Phase 5 step 1a: `p5-estimate.py` `drain_recommended` | Phase 5 step 1b (or once in Phase 6) |

## Changes from the zq3-r2 sheet

All zq3-r2 rules are kept. Added from the zq3-r2 execution:

- **(a) Memory, R2-1.** Phase 5 step 1a records free, inactive, Active,
  laundry and wired pages, ARC, `c_min` and Rocky's RSS. `p5-estimate.py`
  prints the old RSS-based estimate, which is pessimistic because Rocky's
  guest memory is Active on f3 and not in bhyve RSS, and the better
  Active-based one. Step 1b drains the UMA caches
  (`printf 'sysctl debug.uma_reclaim=2\n' | f3 doas -n sh -s`) when that
  estimate is short of the bar or within 1 GiB of it. `p5-uma-stat.sh`
  records free/wired pages and `vmstat -z` free bytes before and after. The
  sheet says that capping `vfs.zfs.arc_max` does not help and must not be
  used. Phase 6 allows the drain once if Phase 5 skipped it and the bar
  fails.
- **(b) Phase 8** runs the driver with the re-derived bootstrap plan and the
  watcher. The signer pin sits in both `hpin` and the watcher, and the sheet
  says what a signer rebuild changes.
- **(c) Phase 11** is one command per line in the mandatory order. It covers
  three cases: the runtime exited at its bootstrap deadline (native mount
  left behind, R2-6), T10 ended RESTARTED, or the runtime FENCED. A restore
  refusal is first checked against a leftover mount.
- **(d) SSH banner timeouts (R2-5):** a retry only after a read-only check
  proves nothing ran, once.
- **(e) Mount failures:** never retried in the same guest boot (the failed
  mount keeps its identity claimed, so a retry returns `EACCES`); the run
  goes to Phase 11.

Beyond those:

- **D1** (see above), and Phase 1 now expects both modules loaded.
- The **guest unmount in Phase 11 step 2** is split into one line per
  mount, each run only if `findmnt` lists it.
- `p1-f3.sh` also records Active and wired pages, `c_min` and `arc_max`.
- `boot_indeterminate=3` is expected (2 at zq3-r2).

## Findings and open questions

- **Q1 D1** (above) must be decided before the GO. The sheet assumes D1-a.
- **Q2 brief pointer.** The candidate evidence
  `tools/integration/artifacts/evidence/xz3-20261109c/README.md` has no
  section on what a zq3-r3 fill must change. Its last section is "Admission
  and checks". This fill took the changes from the candidate table, the t54
  diff and the zq3-r2 record.
- **Q3 UEFI vars drift (still open since zq3-r1 F4).** The record pins
  `c71240e9…`. Any Rocky boot before Phase 7 changes it, and `host.py setup`
  then refuses.
- **Q4 memory.** At 14:11Z free + inactive + (ARC − `c_min`) was about
  7.95 GB with Rocky running (using zq3-r2's `c_min` of 527,494,912 bytes, same
  f3 boot). The 14:11Z facts did not record Active pages,
  so the Active-based estimate is first measured in Phase 5. Phase 6 decides.
- **Q5 window pins file** (zq3-r2 Q1): the r3 window pins live only in the
  private fill, with their hash recorded here.
- F3, F5 and F6 of zq3-r1 (120 s bootstrap, RESTARTED T10 leaves the runtime
  serving, `test_bootstrap` reads the run plan) still apply unchanged.

No committed tool refused real inputs, except the D1 window filler refusal,
which was reproduced against facts equal to the current f3 state. The
intended template refusals (controller template, unfilled bootstrap
template) also occurred as designed.

## Local verification

At `1eba12d` in the main checkout: retained9p 280 tests, directlaunch 26,
`test_fdescfs_window` + `test_fdescfs_window_plan` + `test_runtime_guard` +
`test_native_test_drive` 85, `go test ./internal/runtimeprep`, all passing.
`shellcheck` is clean for `build-stage.sh` and `sh/*.sh` (as POSIX sh), and
for `session.sh` apart from SC2034/SC2086 (its variables are used by the
sheet, and `$O` is split on purpose). No code was changed.
