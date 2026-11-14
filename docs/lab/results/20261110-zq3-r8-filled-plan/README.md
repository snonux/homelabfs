# zq3-r8 filled run inputs (2026-11-14)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r7 fill](../20261110-zq3-r7-filled-plan/README.md) ·
[zq3-r7 record](../20261110-zq3-r7/README.md)

These are the exact run inputs for `zq3-r8`, the eighth attempt of zq3. They
were generated on the workstation with the committed tools at `main`
`5a35150` (after y64 and z64). They mirror the
[zq3-r7 fill](../20261110-zq3-r7-filled-plan/README.md), which ran end to
end. The changes are:
- the run ID;
- the y64 candidate `867ede36…`, which rebuilt all three components: the
  g64+m64 guest kernel, the i64..x64 daemon and the 564/e64/p64/w64 native
  exporter;
- `hfs-runtime-prep` and the signer rebuilt;
- everything that pins them, re-pinned;
- the bundle regenerated from the committed spec (o64/i64 sizing);
- the r7 errata (task h64): mountinfo counts, and Phase 11 step 6 waits for
  `child-gone` only;
- **T5 `mapped-pressure` deferred** with z64's mechanism (q64);
- k64's post-READY `usable` bootstrap step;
- the fresh facts.

Nothing here used SSH, the network, the lab, a VM, a mount, an installation
or the operator private key. **No authority follows from these files:**
every class A step in the [run sheet](run-sheet.md) still needs Paul's
explicit final GO. **zq3 acceptance stays incomplete by design**, because
T5 does not run.

Paul rebooted f3 deliberately on 2026-11-13, so `fdescfs.ko` and
`fusefs.ko` are not loaded. **Phase 1a (D1-a) is not needed.** Phase 1
checks that both are still absent, and the run log records why 1a is
skipped.

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-zq3-r8-fill` (`0700`). This directory holds only
public hashes, paths, argv and the read-only facts. These stay private and
are never committed:
- the per-session keys;
- the runtime config, which holds host receipt private keys;
- the staging tarballs that contain it.

## Inputs

| Input | Value |
|---|---|
| Candidate | **y64 rebuild**: manifest `867ede36…`, package `/home/paul/tmp/claude/hfs-xz3-20261110h/package`, source `b869710`. All 194 manifest entries were checked against their files. Guest kernel `6.1.190-hfs-xz3` (g64 `.show_options` with an exact `cache=mmap`; m64 Sources floor and a log line for every fault block): bzImage `40afad22…`, System.map `1ffea83f…`, GNU build ID `870bb9d1…`. Daemon `34fb05b2…` (i64, o64, r64, n64, j64, u64, v64, x64). Native exporter `ef8f17de…` (564, e64, p64, w64), replacing `36423b6d…`. Window artifacts `f71f0bb8…` (test) and `3d992cdf…` (helper), rebuilt from `b869710`. **Unchanged:** config `28fe4a8e…` (SELinux in `CONFIG_LSM`), the five modules and `modules.*`, init helper `8c02b98f…` |
| Phase 1 facts | [facts/](facts/), collected read-only 2026-11-14 07:57:09Z; they supersede the 20261110T201420Z and 20261111T063434Z sets. **f3:** boot `1794574308` (2026-11-13 12:51:48 UTC), neither module loaded, no fuse/fdesc mount, free 12.11 GB + inactive 1.36 GB, ARC 1.17 GB, `/usr/bin/ssh` `6a9e5573…`, python3.12 `a66eb370…` (same bytes, new mtime Nov 10). **Rocky:** PID `6388` (UTC lstart `Fri Nov 13 12:52:05 2026`), boot ID `6f6f61b0-…`, stock `5.14.0-687.66.1.el9_8`, `next_entry` empty, `boot_indeterminate=7`, SELinux Enforcing, `/.autorelabel` absent, python3.9 `1143733d…`, `authorized_keys` `e08542dc…`. The files hold no key material (grep for key headers and `ssh-*` lines) |
| Host events since zq3-r7 | **2026-11-13 12:51:48 UTC: f3 reboot, done deliberately by Paul.** 2026-11-10 about 09:33 UTC: f3 reboot, **cause not established**. 2026-11-10 19:32 UTC: Rocky self-reboot (vm-bhyve exit 0, run 2), **cause not established**. The vm-bhyve log also shows a further Rocky self-reboot (2026-11-11 10:08 UTC) and many Rocky stops and starts on 2026-11-11 to 13 outside the zq3 work, with no recorded cause. None of them came from this fill |
| f3 network (host change, not ours) | `10.20.0.133` is no longer configured on `re0`; `route -n get 10.20.0.123` reports interface `vm-public`, and a read-only check at 2026-11-14 08:36Z (review round 1) confirmed `inet 10.20.0.133` on `vm-public` and none on `re0`. The bridge members are still `re0` and `tap0`. See the [check](#f3-network-change) below |
| Launch facts | `f3-launch-facts-20261114T075709Z.json` (`4b603cdf…`): `rocky.conf` `c101adf1…`, vm-run `43753bc8…`, firmware `6b48e967…` unchanged; **UEFI vars `49bdc1ef…`** (zq3-r7: `00f82a83…`); launch logged `2026-11-13T14:52:06+02:00` |
| Tap | `tap100` (bridge members `re0`, `tap0`) |
| Run ID / staging | `zq3-r8`, `/var/tmp/hfs-validation-xz3/zq3-r8` (the parent exists and holds zq3-r1 to zq3-r7) |
| Native guard root | `/var/tmp/hfs-20261110-zq3r8-fdq`, host profile `--rocky-pid 6388 --rocky-birth 'Fri Nov 13 12:52:05 2026'`, guard `576d802a…` (unchanged) |
| Window directory | `/var/tmp/hfs-fdescfs-windows/zq3-r8` (the parent exists, `root:wheel 0700`) |
| Rocky names | `/root/hfs-zq3-zq3-r8-*`, `/var/tmp/hfs-zq3-zq3-r8` (the zq3-r1 to zq3-r7 ones stay as evidence) |
| Operator public key | `2f6d909c…`; key path `…/hfs-zq3-r8-fill/operator/operator-signing-key` (empty `0700` directory, never read here) |

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | **Rebuilt** at `5a35150`: `hfs-runtime-prep` and the signer `hfs-retained9p-operator`. The build used `-trimpath`, CGO, VCS stamps and an unmodified private clone (`vcs.modified=false`); a second build of each, forced with `-a`, gave the same bytes. Reason: `git diff ba25486 5a35150` over the `go list -deps` directories of both commands lists non-test changes in `internal/controlledruntime`, `controlledvalidation`, `guestbridge`, `viewcontrol` and `runtimeprep/spec.go`. `go.mod`/`go.sum` are unchanged. zq3-r7's builds (prep `dbebb5a8…`, signer `9ca59cee…`) are retired. G3 init helper: zq3-r1's reproduced build, kept by xz3-20261110h | prep `29ce5bdb…`, signer `b6612534…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from `testdata/zq3-spec.json` at `5a35150` (`make-spec.py`: only `zq3-r1` → `zq3-r8` and the r8 operator key path change; the same method reproduces zq3-r7's spec from `ba25486` byte for byte). The spec now carries the o64/i64 sizing (Sources 10, WorkBytes 736 MiB, LedgerBytes 188 MiB, Claims 128, Capabilities 640, CapabilityIdentities 1024, CredentialRevisions 128, ServiceBytes 192 MiB) and the exporter `ef8f17de…`. The guest budget is unchanged | spec `0a0dcda7…`, request template `1c5baf24…`, runtime config `d208bbfe…`, volume-init `33766fc3…`; volume `bde64255-…`; sessions hfs0 `5b822444-…`, hfs1 `11154906-…` |
| 1. Argv | `argv.py generate` from the 07:57Z launch facts (sessions, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `ea34b181…`, args `37f20d9d…`, vars `49bdc1ef…` |
| 2. Kernel plan | `rocky_kernel.py plan` at main (`1e9321fd…`; admits only `867ede36…`), stock default `/boot/vmlinuz-5.14.0-687.66.1.el9_8.x86_64`. It differs from zq3-r7's in run ID, manifest, build ID, bzImage and System.map; config and modules are the same. The c64+d64 package `66b5bb2b…` is refused as superseded | plan `0c761d9d…`, build ID `870bb9d1…` |
| 3. Controller batch | Template derived from zq3-r7's by `derive-templates.py`: run ID, fill path, sessions, candidate `66b5bb2b…` → `867ede36…`, probe `guest.py` `c50bb849…`. **T5 deferred:** the `mapped-pressure` request is dropped and `deferred: [{case: "mapped-pressure", reason: "q64: …"}]` is added; the four other requests keep canonical order. `controller.py` `2ec02d69…`. `check` refuses the template as designed (`probe/installed-manifest pin`). A synthetic copy with zero IDs **passes** and prints `cases_planned` (four) and the deferral. The same copy is **refused** with the `66b5bb2b` candidate (superseded), without the declaration ("five-case syscall batch minus declared deferrals") and with `credentials` also deferred ("case may not be deferred") | template `e3982d13…` |
| 4. Bootstrap plan | Template derived from zq3-r7's (the one that ran). New pins: bundle, kernel plan, `rocky_kernel.py`, the y64 daemon `34fb05b2…`, the rebuilt prep `29ce5bdb…` and the k64 `bootstrap_remote.py` `93563b17…` (`remote_sha256`). The guard pin `fece96b3…` is unchanged. The script re-hashes every pinned file at main and in the package. The unfilled template refuses as designed. **F1 filled now** with `fill_plan.py bootstrap` (`564c2e16…`) from the 07:57Z `sha256 -r /usr/bin/ssh` line (`6a9e5573…`, unchanged), then `bootstrap.py check` (`de2a0054…`, journal `/2`) **pass** | template `0a1255a0…`, filled plan `006b50ed…` |
| 5. Guard argv | Derived from zq3-r7's, with `--runtime-sha256 34fb05b2…` (checked against the package) and `--disposable-validation` exactly once, last. It equals `bootstrap.py`'s own `SshRemote.guard_argv()` for the filled plan, in shell string and argv, and passes `runtime-guard.py`'s `parse_args` and `check_command` | in [pins](pins.json) |
| 6. Observer and staging pins | Unchanged script pins: `observer_key.py` `cd7f38c2…`, `observer.py` `2e1449dd…`, `installed.py` `c824b183…`, native guard `576d802a…`, `runtime-guard.py` `fece96b3…`, `fdescfs-window.py` `b00b19d1…`, `host.py` `738e1ca5…`, `host_snapshot.py` `b29ffed2…`, `guest-lock-probe.py` `630a737d…`, `pod_fixture.py` `ab4d269b…`. Re-pinned to main: `rocky_kernel.py` `1e9321fd…`, `argv.py` `ed63ff97…` (exporter pin). All 17 f3 and 18 Rocky files went into two deterministic tarballs; a second tar run gave the same hashes. A verification extract matched every expected hash and the f3 modes (owner `0/0`, no parent entry) and was then deleted | f3 tar `dce5429a…`, Rocky tar `75bf705a…` |
| 7. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` at main (`ef96c638…`, as y64 left it) with `zq3-r1` → `zq3-r8`; `check_pins` passes. Synthetic fills, which are not pins: zq3-r7's real Phase 4 facts and host hashes **pass**, taken after its D1-a, with paths renamed and the three artifact hashes swapped for the y64 ones. The same facts with zq3-r7's unswapped host hashes **refuse** (`pinned artifact differs or missing`). Post-restore facts with both modules loaded **refuse** (`fdescfs module present`) | r8 pins `bb028d1f…` |
| 8. Signing watcher | zq3-r7's `bin/sign_watch.py` with `D`, the docstring run ID, the signer comment and **the signer pin `b6612534…`** changed. Tested locally with a fake signer: a wrong request hash, a wrong command, an unknown action, a repeat and a wrong signer hash were refused; both exact requests were signed | `4e8e75cf…` |
| 9. Process observer check | `sh/p4-observer.py` from zq3-r7 with the run ID and **the daemon pin `34fb05b2…`** changed (Phase 4 step 1a, **required**: new daemon bytes). zq3-r7's real records (renamed) are refused only for the d64 `sha256` and pass with `34fb05b2…` substituted. The `$NG` line parses and passes the native guard's own `check_request` | `7070f818…` |
| 10. Run sheet | [run-sheet.md](run-sheet.md): Phases 0-12, 1a (skipped), 4/1a and U1-U3, with the SELinux gates in Phases 1, 9 and 12. `session.sh` (`3de08957…`) and eleven pinned helper scripts. `p9-ready.py` now also requires the `usable` result, and `p1-f3.sh` records the route and `vm-public`. The Phase 8-10 lines were dry-run end to end (below) | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-14T00:00:00Z --format=gnu`.
The f3 tarball holds only `hfs-20261110-zq3r8-fdq` and
`hfs-validation-xz3/zq3-r8`. It has **no entry for the existing parent**
`hfs-validation-xz3/`, so extraction never rewrites the parent's mode or
owner. All directories are `0700`, binaries `0500`, other files `0400`,
owner `0/0`.

## Signing

Paul delegated both signatures (A7) to Claude. In Phase 8 the driver's
stdout goes through `bin/sign_watch.py` (`4e8e75cf…`). The watcher runs the
signer (`b6612534…`) only if:
- the printed command is exactly one of the two below;
- the request file's SHA256 equals the printed `request_sha256`;
- each command runs at most once.

Anything else is refused, and the driver then times out (fails closed):

```
/home/paul/tmp/claude/hfs-zq3-r8-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r8-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r8-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r8-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r8-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r8-fill/bootstrap-run/work/signer-binding
```

On Paul's instruction, Claude places the operator key at the bundle's path
before the run. Nothing in the fill reads it.

## Run-time fills

| Fill | When and source | Used by |
|---|---|---|
| D1 | **not needed**: both modules absent after the 2026-11-13 reboot; Phase 1 checks, Phase 1a is skipped with that reason, and a loaded module stops the run | Phase 4 step 0 window plan fill |
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 07:57Z fact); Phase 4 step 0 rechecks it (`F1-unchanged`), a change stops | bootstrap `observer.ssh_sha256` (plan `006b50ed…`) |
| F2 fdescfs window plan and its hash (`WP`) | Phase 4 step 0: controller `facts`, host hashes, `realpath` library closure, f3 `date -u +%s`, then `fdescfs-window-plan.py --pins` with the r8 pins | setup, qualify, restore (`W`); Phase 5 recheck |
| F3 pod rootfs manifest digest (`PM`) | Phase 3: `pod_fixture.py manifest` on Rocky (zq3-r1 to zq3-r7: `f7f69339…`); `skip` if A8 is declined | `build`, T7 request |
| F3a process observer record | Phase 4 step 1a: guard stage `process-observer`, checked by `p4-observer.py` | go/no-go before setup; **pass required** (new daemon bytes) |
| F4 observer key pair, enrolled line, `after_sha256` (`EA`) | Phase 4 step 5: `ssh-keygen` on f3, `observer_key.py enrol` | runtime observer, Phase 11 `remove --expect-sha256`, banner-timeout check |
| F5 BLS entry ID | Phase 3 install record | `select` (from the state directory) |
| F6 boot ID, provider nonce, admission manifest SHA256, `virtioN` names, proposal, controller, provision hash | Phase 8 admission and runtime records | enrollment and bind requests (bootstrap fills them), controller batch plan |
| F7 m0/m1 mount IDs | Phase 8 mount steps (mountinfo) | batch plan, then `p10-requests.py` for the T6/T7/T8 requests |
| F8 batch plan and its hash (`B`) | Phase 8, after READY and `usable`: `fill_plan.py batch` (prints `cases_planned` and `deferred`), then `check-retained9p.sh check` | every controller and correlator call |
| F9 runtime UUID (`U`), cut, epoch and lifecycle sequences | READY record via `p9-ready.py`; sequences 1 and 2 literal, then each from `correlate.py command` (`obs`, ARM, RESTART); expected 1-14 with T5 deferred | Phase 9 OBSERVE, T1-T4, T9-T11 |
| F10 exporter child PID (`XP`) | guard `receipt.json` `exporter.pid` (Phase 11 step 0; the `--no-child` line if null) | `host.py cleanup --child-pid` |
| F11 Rocky PID and lstart | **fixed** from the 07:57Z facts (`6388`, `Fri Nov 13 12:52:05 2026` UTC); rechecked in Phases 1, 4 (also by every `$NG` stage) and 5 | native guard host profile, Phase 5 recheck, `p5-mem.txt` RSS |
| F12 UMA drain decision | Phase 5 step 1a: `p5-estimate.py` `drain_recommended` | Phase 5 step 1b (or once in Phase 6) |
| F13 T6/T7/T8 request files | Phase 8 after the batch check: `p10-requests.py` (exclusive, `0600`, in `$E`) | T6, T7, T8 |
| F14 guard PID | Phase 11, only on the runtime-still-serving path (`pgrep -U root`) | the one SIGTERM to the guard |
| F15 SELinux state | Phase 1 (`/.autorelabel` absent, Enforcing), Phase 9 (guest `getenforce` `Enforcing`, enforce `1`), Phase 12 (relabel record) | Phase 1 and Phase 9 gates; Phase 12 finding |

## Changes from the zq3-r7 sheet

All zq3-r7 rules are kept:
- Phase 4 step 1a, the process observer check: **required again**, because
  the daemon bytes changed;
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

D1-a (Phase 1a) stays as a rule but **does not run** in r8: both modules are
absent. The sheet keeps the Phase 1 check and a skipped Phase 1a section with
its recorded reason. A loaded module stops the run.

Kept from the zq3-r5 to zq3-r7 executions and the r7 review decisions:
- **Phase 11 step 9 is one plain line with literal paths**:
  `f3 "doas -n rm -- /var/tmp/hfs-validation-xz3/zq3-r8/observer/id_ed25519 /var/tmp/hfs-validation-xz3/zq3-r8/observer/id_ed25519.pub"`.
  Every abort path points at that line.
- **The window restore's outcome is `restoration-summary.json`**, not an
  `rc` reported through an output filter (step 10, `"mount_restored": true`).
- **Every `rockyt … | tee` line ends with `; echo rc=${PIPESTATUS[0]}`**
  (T8, Phase 11 step 2).
- Phase 11 step 4a captures the guest's AVC records. The vm-bhyve log decides
  whether a relabel happened, and the SELinux gates in Phases 1, 9 and 12
  stay.

Applied from the r7 errata (task h64):
- **Guest 9p checks count ` 9p ` lines in `/proc/self/mountinfo`**
  (`grep ' 9p ' …; grep -c ' 9p ' …`), replacing every `findmnt -t 9p`
  (Phase 9, T8, Phase 11 step 2 twice), because `findmnt` exits 0 when
  empty.
- **Phase 11 step 6 waits for `child-gone` only**; step 7's `host.py
  cleanup` destroys the vmm context.
- **T5 deferred** (z64, q64): the batch template carries the declaration
  and four requests.
  - Phase 10 order: T1-T4, T6, T7, T9, T10, T11, T8. T5 is recorded "unrun
    (deferred, q64)".
  - T6 needs T5 only for order, so it follows T4.
  - T9-T11 run after the four released cases.
  - Expected sequences: 2/3, 4/5, 6/7, 8/9; T9 10/11/12; T10 13/14.
  - T4 now ends with `status` (`syscall_batch_success`, `cases_planned`,
    `deferred`).
- **k64 `usable` step:** new bootstrap pins (`bootstrap.py` `de2a0054…`,
  `bootstrap_remote.py` `93563b17…`). The Phase 8 budget gains about 70 s.
  The sheet describes the step, its failure path (guard-SIGTERM branch, no
  batch fill) and the `p9-ready.py` check.

New for zq3-r8:
- **Candidate `867ede36…`** in the kernel plan, batch template, bootstrap
  plan, guard argv, window pins, staging, Phase 3, Phase 4 step 1a and
  Phase 8 expectations.
- **Phase 9 gates on the exact mount tuple**
  `rw,cache=mmap,msize=65536,trans=virtio,version=9P2000.L.hfs1.4`, exactly
  two 9p mountinfo lines, and no `hfs … blocked` dmesg line, in addition to
  SELinux.
- **Phase 8 expects** both mounts, `usable` ok and READY with every step
  `ok`. T1-T4 and T6-T11 should get past admission. Before T9, any guest
  `hfs … blocked` line is a finding: m64 logs every fault block, and a
  clean unmount is silent. T9 (m0) and a FENCED T10/T11 (m1) block sessions
  on purpose, so afterwards only unexpected block lines are findings. Phase
  11 step 4b saves the full guest `dmesg`.
- zq3-r7's T10 note about the 564 race is dropped, because the exporter now
  has 564. A "busy" recovery reply would be a finding.
- Phase 1 expectations for the new f3 boot and Rocky incarnation, and for
  the `vm-public` address check.

## Phases 9-10 recheck

Since zq3-r7:
- `controller.py` (`2ec02d69…`), `correlate.py` (`257f44f9…`) and
  `fill_plan.py` (`564c2e16…`) gained z64. A plan may declare a `deferred`
  list, and `check`, the batch fill report, the controller state, `status`,
  `correlate.py command` and the T9/T11 records repeat it.
  `syscall_batch_success` means every planned case released, and the
  lifecycle steps need exactly that.
- `guest.py` (`c50bb849…`) changed only comments.
- `guest-lock-probe.py` (`630a737d…`) is unchanged.
- `bootstrap.py` (`de2a0054…`) gained the `usable` step and journal `/2`;
  `fill_plan.py` refuses `/1` journals.

No CLI the sheet uses changed shape.

**Sheet dry run** (`dryrun/` in the private fill; scratch output). A
harness extracted **all 69 command lines** of this sheet, from Phase 8
"After READY" to T8 (plus the Phase 7 `k04` baseline line), from
`run-sheet.md` itself and ran them in order. It used:
- the real `fill_plan.py batch`, `check-retained9p.sh check`,
  `correlate.py`, `session.sh` (`obs`, `sha`, `jf`) and the `sh/` helpers;
- `controller.py` with faked guest dispatch;
- stub `f3`/`rocky` that answer like the lifecycle client, `host_snapshot.py`
  and the guest;
- a synthetic READY run with journal `/2`, an `ok` `usable` result and
  mountinfo rows with the exact g64 options.

Every line passed with the expected results:
- sequences 1-14 (ARM 11, RESTART 14);
- the batch fill and check printing `cases_planned` (four) and the deferral;
- T1-T4 correlated and released; `syscall_batch_success` True with the
  deferral in the state and in `missing`;
- T9 `dropped-reply-effect-committed-and-visible`;
- T10 FENCED;
- T11 `k04_refusal_observed` true, with `deferred` repeated;
- the T6/T8 lock requests from `p10-requests.py`;
- T8 `rc=0`.

**Negative and real-record checks:**
- `helpers-dryrun.py` refused, as expected: a wrong socket, a missing or
  failed `usable` result, a wrong sequence, a fenced admission, a rotated
  cut, an incomplete OBSERVE, an unfiltered ARM, a wrong operation, existing
  outputs and a wrong plan hash.
- `fill_plan.py batch` refused a run that stopped at `usable` (`bootstrap
  steps did not end ok: usable=failed`), and `p9-ready.py` refused it too.
- `p9-ready.py` refuses zq3-r7's real run directory (journal `/1`, no
  `usable`). It passes the same directory with the socket renamed and an
  `ok` `usable` result appended.
- `p9-observe.py` passes zq3-r7's real OBSERVE sequence 1 against a batch
  plan from the r8 template that carries zq3-r7's real boot ID, nonce,
  admission manifest, sessions and mount IDs. It refuses the same OBSERVE
  against the r8 synthetic plan (boot/provider differs) and against a wrong
  plan hash.
- `guest.py` `check_tuple` passes a mountinfo row with the g64 options and
  refuses zq3-r7's `rw`-only row ("exact controlled mount tuple differs").

## f3 network change

`10.20.0.133` moved from `re0` onto the `vm-public` bridge (host change, not
ours). Nothing in the sheet assumes the address is on `re0`:
- the session reaches f3 by IP;
- `host.py` checks only the `vm-public` bridge and its members;
- `tap100` joins `vm-public`.

One dependency remains. `observer_key.py` enrols the observer key with
`from="10.20.0.133"`, so f3's connection to the guest must leave from that
address. The address is on the bridge that the route uses (confirmed
read-only at 2026-11-14 08:36Z), so this should hold. `p1-f3.sh` now prints `route -n get 10.20.0.123` and `ifconfig
vm-public`, and Phase 1 stops if `10.20.0.133` is not on `vm-public`.

## Decisions from the zq3-r8 run-sheet review round 1 (2026-11-14)

No must-fix items. Applied:
- **f3 address:** the bridge `inet` line was confirmed read-only at 08:36Z
  (`inet 10.20.0.133` on `vm-public`, none on `re0`), so the Phase 1 stop
  should not fire.
- **Persisted variables:** the run-sheet list now includes `XP`, `ARM`,
  `RST` and the guard PID, and says to append each `VAR=` result to
  `vars.env` and not to rely on `pipefail` between lines.
- **Block-line rule:** only lines before T9, or for an unexpected session
  or operation afterwards, are findings. The new Phase 11 step 4b saves the
  full guest `dmesg` before the poweroff.
- **Nits:** "five modules as zq3-r7", the duplicated Phase 11 heading
  removed, and the `session.sh` summary fenced as `text`.

Kept: the "zq3-r8" wording slips in the comments of the pinned
`session.sh` and `sh/p4-observer.py` (fixing them would mean re-pinning).
`tools/lab/vms.sh` still reaches f3 through `~/.ssh/config` (BatchMode,
never accepting new keys), as in r1-r7. T9-T11, `obs` and `p10-argv.py`
have only been dry-run so far.

## Findings and open questions

- **Q1 T5 deferred (q64).** The run cannot complete zq3 acceptance. The
  deferral reason records Paul's pending q64 decision (default option 4).
- **Q2 the y64 fixes are untested on f3.** Only the native exporter build
  ran on f3. The kernel (g64, m64), the daemon (i64..x64) and the exporter
  fixes (564, e64, p64, w64) passed offline tests only. The main questions
  for this run are: the mount tuple in Phase 9, `usable` ok, and T1 past
  admission.
- **Q3 the `usable` step adds claims.** Each `lstat` adds GETATTR and
  PROCESS_EXIT claims to the provider ledgers before T1. The step narrows
  zq3-r7's failure, but it does not prove the mount claims reached TERMINAL
  (k64 follow-up).
- **Q4 memory.** At 07:57Z free was 12.11 GB, inactive 1.36 GB and ARC
  1.17 GB. The facts carry no Active count. Phase 5's estimate and the
  Phase 6 bar decide (zq3-r7 margin 0.57 GB after the drain).
- **Q5 UEFI vars drift.** The record pins `49bdc1ef…`. Any Rocky boot before
  Phase 7 changes it, and `host.py setup` then refuses. Rocky restarted
  often between 2026-11-11 and 13, so recheck right before the run.
- **Q6 host events.** Two events remain unexplained: the 2026-11-10 f3
  reboot and the Rocky self-reboots (2026-11-10 19:32 and 2026-11-11 10:08
  UTC). The 2026-11-13 reboot was Paul's.
- **Q7 window pins file:** the r8 window pins live only in the private fill,
  with their hash recorded here.
- **Q8 T9 ARM refused, T10 FENCED step 2, the T11 receipt wait (5 minutes):**
  unchanged from zq3-r7; the sheet keeps the same choices.

No committed tool refused real inputs. These intended refusals all occurred:
- the controller template;
- the unfilled bootstrap template;
- the superseded `66b5bb2b` candidate in a synthetic batch plan and in
  `rocky_kernel.py plan`;
- an undeclared four-case batch and a deferred `credentials`;
- the window filler on zq3-r7's unswapped artifact hashes and on facts with
  both modules loaded.

## Local verification

At `5a35150` in the main checkout, all passing:
- retained9p: 340 tests;
- directlaunch: 26 tests;
- `tools/lab` top level: 245 tests (3 skipped);
- `go test -count=1 ./internal/runtimeprep`.

`shellcheck` is clean for `build-stage.sh` and `sh/*.sh` (as POSIX sh), and
for `session.sh` apart from SC2034/SC2086: its variables are used by the
sheet, and `$O` is split on purpose. No code was changed.
