# zq3-r6 filled run inputs (2026-11-10)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r5 fill](../20261109-zq3-r5-filled-plan/README.md) ·
[zq3-r5 record](../20261109-zq3-r5/README.md)

These are the exact run inputs for `zq3-r6`, the sixth attempt of zq3. They
were generated on the workstation with the committed tools at `main`
`ded27b3` (after 964 and a64) and mirror the
[zq3-r5 fill](../20261109-zq3-r5-filled-plan/README.md), which ran for real.
The changes are:
- the run ID;
- the 964 candidate `e01fab21…` with its new guest kernel;
- the re-pinned kernel plan, `rocky_kernel.py` and `controller.py`;
- the fresh facts;
- the zq3-r5 lessons.

The daemon, the window artifacts, `hfs-runtime-prep` and the signer are
unchanged and reused, with proof.

Nothing here used SSH, the network, the lab, a VM, a mount, an installation
or the operator private key. **No authority follows from these files:**
every class A step in the [run sheet](run-sheet.md) still needs Paul's
explicit final GO.

`fdescfs.ko` (id 28) and `fusefs.ko` (id 29) are loaded on f3 again. zq3-r5
left them loaded in the same boot, so decision D1-a applies again as
**Phase 1a**.

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-zq3-r6-fill` (`0700`). This directory holds only
public hashes, paths, argv and the read-only facts. These stay private and
are never committed:
- the per-session keys;
- the runtime config, which holds host receipt private keys;
- the staging tarballs that contain it.

## Inputs

| Input | Value |
|---|---|
| Candidate | 964 kernel rebuild: manifest `e01fab21…`, package `/home/paul/tmp/claude/hfs-xz3-20261110f/package` (all 195 manifest entries checked), source `06c27a8`. New: guest kernel `6.1.190-hfs-xz3` with bzImage `704b17be…`, System.map `efb43b79…`, GNU build ID `a470c7a8…`. Unchanged: z54 CGO daemon `39c92d01…`, window artifacts `97fb5c07…`/`cc92bcde…` (z54 build from `a20aca1`), config, five modules, bhyve `36423b6d…`, init helper `8c02b98f…` |
| Phase 1 facts | [facts/](facts/), collected read-only 2026-11-09 23:56:01Z. f3: boot `1794212992` (unchanged since zq3-r2), `fdescfs.ko` (id 28) and `fusefs.ko` (id 29) loaded with no fuse/fdesc mount, free 5.00 GB + inactive 7.89 GB, ARC 0.82 GB, `/usr/bin/ssh` `6a9e5573…`. Rocky: PID `21428` (UTC lstart `Mon Nov  9 22:04:03 2026`), boot ID `ffa856ca-…`, stock `5.14.0-687.66.1.el9_8`, `next_entry` empty, `boot_indeterminate=6`, python3.9 `1143733d…`, `authorized_keys` `e08542dc…`. The files hold no key material (grep for key headers and `ssh-*` lines) |
| Launch facts | `f3-launch-facts-20261109T235601Z.json` (`0a801be8…`): `rocky.conf` `c101adf1…` unchanged, **UEFI vars `bcc505d5…`** (zq3-r5: `7b2192fd…`) |
| Tap | `tap100` (bridge members `re0`, `tap0`) |
| Run ID / staging | `zq3-r6`, `/var/tmp/hfs-validation-xz3/zq3-r6` (the parent exists and holds zq3-r1 to zq3-r5) |
| Native guard root | `/var/tmp/hfs-20261110-zq3r6-fdq`, host profile `--rocky-pid 21428 --rocky-birth 'Mon Nov  9 22:04:03 2026'`, guard `576d802a…` (unchanged) |
| Window directory | `/var/tmp/hfs-fdescfs-windows/zq3-r6` (the parent exists, `root:wheel 0700`) |
| Rocky names | `/root/hfs-zq3-zq3-r6-*`, `/var/tmp/hfs-zq3-zq3-r6` (the zq3-r1 to zq3-r5 ones stay as evidence) |
| Operator public key | `2f6d909c…`; key path `…/hfs-zq3-r6-fill/operator/operator-signing-key` (empty `0700` directory, never read here) |

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | **Reused, not rebuilt**: zq3-r5's `hfs-runtime-prep` and signer `hfs-retained9p-operator` (built at `41cbefd`). Proof: `git diff 41cbefd ded27b3` over the `go list -deps` directories of both commands lists only `_test.go` files and `internal/guestbridge/README.md`; `go.mod`/`go.sum` are unchanged, and so is the only non-test `go:embed` (`probe/installed.py`). Unstamped `-trimpath` builds at `41cbefd` and `ded27b3` are byte-identical (prep `96aafc46…`, signer `d073b60a…`). G3 init helper: zq3-r1's reproduced build | prep `2d8d8e88…`, signer `89905914…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from `testdata/zq3-spec.json` at `ded27b3`, changing only `zq3-r1` → `zq3-r6` and the r6 operator key path (the same method reproduces zq3-r5's spec). The guest budget is unchanged (reservations 1024, received 256, 512/512 MiB) | spec `45275337…`, request template `2bf5cee4…`, runtime config `00552f03…`, volume-init `3dfe7564…`; volume `41ec7d24-…`; sessions hfs0 `a512e9cc-…`, hfs1 `9c3d6ec6-…` |
| 1. Argv | `argv.py generate` from the 23:56Z launch facts (sessions, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `df169a4c…`, args `2e241dc0…`, vars `bcc505d5…` |
| 2. Kernel plan | `rocky_kernel.py plan` at main (`1e5527c7…`; a64 admits only `e01fab21…`), stock default `/boot/vmlinuz-5.14.0-687.66.1.el9_8.x86_64`. It differs from zq3-r5's in run ID, manifest, bzImage and System.map; the five modules are the same. The z54 package is refused as superseded | plan `4f77092c…`, build ID `a470c7a8…` |
| 3. Controller batch | Template derived from zq3-r5's: run ID, fill path, sessions, candidate `2e0412a6…` → `e01fab21…`. `controller.py` (`fd9e9f5f…`) changed only its admitted and superseded sets, comments and the refusal text. `check` refuses the template as designed (`probe/installed-manifest pin`). A synthetic copy with zero IDs passes; the same copy with the z54 candidate is refused as superseded | template `85f57039…` |
| 4. Bootstrap plan | Template derived from zq3-r5's (the one that ran) by `derive-templates.py`. It carries the new pins: bundle, kernel plan and `rocky_kernel.py`. The daemon pin `39c92d01…`, the guard pin `fece96b3…` and the prep pin `2d8d8e88…` are unchanged. The script re-hashes every pinned file at main and in the package. The unfilled template refuses as designed. **F1 filled now** with `fill_plan.py bootstrap` from the 23:56Z `sha256 -r /usr/bin/ssh` line, then `bootstrap.py check` **pass** | template `927fc5e6…`, filled plan `b989e548…` |
| 5. Guard argv | Derived from zq3-r5's, with `--runtime-sha256 39c92d01…` (checked against the package) and `--disposable-validation` exactly once, last. It equals `bootstrap.py`'s own `SshRemote.guard_argv()` for the filled plan, in shell string and argv, and matches `runtime-guard.py`'s `check_command` shape | in [pins](pins.json) |
| 6. Observer and staging pins | Unchanged script pins: `observer_key.py` `cd7f38c2…`, `observer.py` `2e1449dd…`, `installed.py` `c824b183…`, native guard `576d802a…`, `runtime-guard.py` `fece96b3…`, `fdescfs-window.py` `b00b19d1…`. Re-pinned to main: `rocky_kernel.py` `1e5527c7…`, `argv.py` `e8c60fcf…` (comment only). All 17 f3 and 18 Rocky files went into two deterministic tarballs; a second tar run gave the same hashes. A verification extract matched every expected hash and the f3 modes (owner `0/0`, no parent entry) and was then deleted | f3 tar `a8960961…`, Rocky tar `cdbf66d8…` |
| 7. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` at main (unchanged since 664) with `zq3-r1` → `zq3-r6`; `check_pins` passes. Synthetic fills, which are not pins: zq3-r5's real Phase 4 facts and host hashes (after its D1-a, paths renamed) **pass**. Post-restore facts with both modules loaded (f3's state now) **refuse** with `fdescfs module present`; Phase 1a resolves that | r6 pins `860e6b04…` |
| 8. Signing watcher | zq3-r5's `bin/sign_watch.py` with `D`, the docstring run ID and the signer comment changed; the signer pin `89905914…` is the same. Tested locally with a fake signer: a wrong request hash, a wrong command, an unknown action, a repeat and a wrong signer hash were refused; both exact requests were signed | `68f11620…` |
| 9. Process observer check | `sh/p4-observer.py` from zq3-r5 with the run ID renamed (Phase 4 step 1a, kept). The `$NG` line parses and passes the native guard's own `check_request`. It passed a synthetic good record and zq3-r5's real records (renamed), and refused 14 bad ones | `6612f5cb…` |
| 10. Run sheet | [run-sheet.md](run-sheet.md): Phases 0-12, 1a, 4/1a and U1-U3. `session.sh` (`4a5c4f75…`) and eleven pinned helper scripts. The Phase 8-10 lines were dry-run end to end (below) | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-10T00:00:00Z --format=gnu`.
The f3 tarball holds only `hfs-20261110-zq3r6-fdq` and
`hfs-validation-xz3/zq3-r6`. It has **no entry for the existing parent**
`hfs-validation-xz3/`, so extraction never rewrites the parent's mode or
owner. All directories are `0700`, binaries `0500`, other files `0400`,
owner `0/0`.

## Signing

Paul delegated both signatures (A7) to Claude. In Phase 8 the driver's
stdout goes through `bin/sign_watch.py` (`68f11620…`). The watcher runs the
signer (`89905914…`) only if:
- the printed command is exactly one of the two below;
- the request file's SHA256 equals the printed `request_sha256`;
- each command runs at most once.

Anything else is refused, and the driver then times out (fails closed):

```
/home/paul/tmp/claude/hfs-zq3-r6-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r6-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r6-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r6-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r6-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r6-fill/bootstrap-run/work/signer-binding
```

On Paul's instruction, Claude places the operator key at the bundle's path
before the run. Nothing in the fill reads it.

## Run-time fills

| Fill | When and source | Used by |
|---|---|---|
| D1 | **decided**: D1-a as Phase 1a (normal `kldunload` of fusefs and fdescfs right after Phase 1) | Phase 4 step 0 window plan fill |
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 23:56Z fact); Phase 4 step 0 rechecks it (`F1-unchanged`), a change stops | bootstrap `observer.ssh_sha256` (plan `b989e548…`) |
| F2 fdescfs window plan and its hash (`WP`) | Phase 4 step 0: controller `facts`, host hashes, `realpath` library closure, f3 `date -u +%s`, then `fdescfs-window-plan.py --pins` with the r6 pins | setup, qualify, restore (`W`); Phase 5 recheck |
| F3 pod rootfs manifest digest (`PM`) | Phase 3: `pod_fixture.py manifest` on Rocky (zq3-r1 to zq3-r5: `f7f69339…`); `skip` if A8 is declined | `build`, T7 request |
| F3a process observer record | Phase 4 step 1a: guard stage `process-observer`, checked by `p4-observer.py` | go/no-go before setup; pass required |
| F4 observer key pair, enrolled line, `after_sha256` (`EA`) | Phase 4 step 5: `ssh-keygen` on f3, `observer_key.py enrol` | runtime observer, Phase 11 `remove --expect-sha256`, banner-timeout check |
| F5 BLS entry ID | Phase 3 install record | `select` (from the state directory) |
| F6 boot ID, provider nonce, admission manifest SHA256, `virtioN` names, proposal, controller, provision hash | Phase 8 admission and runtime records | enrollment and bind requests (bootstrap fills them), controller batch plan |
| F7 m0/m1 mount IDs | Phase 8 mount steps (mountinfo) | batch plan, then `p10-requests.py` for the T6/T7/T8 requests |
| F8 batch plan and its hash (`B`) | Phase 8, after READY: `fill_plan.py batch`, then `check-retained9p.sh check` | every controller and correlator call |
| F9 runtime UUID (`U`), cut, epoch and lifecycle sequences | READY record via `p9-ready.py`; sequences 1 and 2 literal, then each from `correlate.py command` (`obs`, ARM, RESTART) | Phase 9 OBSERVE, T1-T11 |
| F10 exporter child PID (`XP`) | guard `receipt.json` `exporter.pid` (Phase 11 step 0; the `--no-child` line if null) | `host.py cleanup --child-pid` |
| F11 Rocky PID and lstart | **fixed** from the 23:56Z facts (`21428`, `Mon Nov  9 22:04:03 2026` UTC); rechecked in Phases 1, 4 (also by every `$NG` stage) and 5 | native guard host profile, Phase 5 recheck, `p5-mem.txt` RSS |
| F12 UMA drain decision | Phase 5 step 1a: `p5-estimate.py` `drain_recommended` | Phase 5 step 1b (or once in Phase 6) |
| F13 T6/T7/T8 request files | Phase 8 after the batch check: `p10-requests.py` (exclusive, `0600`, in `$E`) | T6, T7, T8 |
| F14 guard PID | Phase 11, only on the runtime-still-serving path (`pgrep -U root`) | the one SIGTERM to the guard |

## Changes from the zq3-r5 sheet

All zq3-r5 rules are kept:
- D1-a before staging as **Phase 1a**;
- Phase 4 step 1a, the process observer check: kept because it is cheap,
  although the daemon bytes are unchanged and passed in zq3-r5;
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

New from the zq3-r5 execution:
- **Phase 11 step 9 is one plain line with literal paths**:
  `f3 "doas -n rm -- /var/tmp/hfs-validation-xz3/zq3-r6/observer/id_ed25519 /var/tmp/hfs-validation-xz3/zq3-r6/observer/id_ed25519.pub"`.
  In zq3-r5 an agent safety check refused the deletion inside a `bash -c`
  script. Every abort path points at that line.
- **The window restore's outcome is `restoration-summary.json`.** The
  restore prints nothing on success, and a `restore-rc` reported through an
  output filter (`| tee`) is the filter's status, not the restore's. Step 10
  now reads `/var/tmp/hfs-fdescfs-windows/zq3-r6/restoration-summary.json`
  into `$E/p11-restoration-summary.json` and judges by `"mount_restored": true`.

New for zq3-r6:
- **Candidate `e01fab21…`** in the kernel plan, batch template, Phase 3 and
  Phase 8 expectations. Phase 8 now expects mount-0 to complete, then
  mount-1 and READY. If mount-0 blocks again, the sheet takes the live owner
  snapshot and records the guest's mount process state.
- Phase 1 expectations for Rocky's current incarnation: PID, boot ID,
  `boot_indeterminate=6` and vars. Phase 1a module ids 29/28. The zq3-r5
  directories are listed among the leftovers. The launch facts list `tap0`
  before `wg0` now, which is only an order change.
- Phase 12 and U3 explain the 5-minute first-boot reboot (below).

## Phases 9-10 recheck

These phases have never run for real. The committed `correlate.py`
(`82af8060…`), `guest.py` (`65a6a16d…`), `guest-lock-probe.py`
(`630a737d…`), `fill_plan.py` and `bootstrap.py` are byte-identical to
zq3-r5's snapshot. `controller.py` (`fd9e9f5f…`) changed only its admitted
and superseded sets, comments and one refusal text, so no CLI or record
changed.

The dry run did more than r5's. A scratch harness extracted **all 71
command lines** of this sheet, from Phase 8 "After READY" to T8 (plus the
Phase 7 `k04` baseline line), from `run-sheet.md` itself and ran them in
order. It used:
- the real `fill_plan.py batch`, `check-retained9p.sh check`,
  `correlate.py`, `session.sh` (`obs`, `sha`, `jf`) and the `sh/` helpers;
- `controller.py` with faked guest dispatch;
- stub `f3`/`rocky` that answer like the lifecycle client, `host_snapshot.py`
  and the guest.

Every line passed with the expected results:
- sequences 1-16;
- T1-T5 correlated and released, with `syscall_batch_success` True;
- T9 `dropped-reply-effect-committed-and-visible`;
- T10 FENCED;
- T11 `k04_refusal_observed` true;
- the T6/T8 lock requests from `p10-requests.py` parsed in the real
  `guest-lock-probe.py` and reached its admission check, which is T8's
  expected refusal.

The r5 negative dry run was also repeated, and every case was refused:
- a wrong sequence;
- a fenced admission;
- a rotated cut;
- an incomplete OBSERVE;
- a wrong socket;
- an unfiltered ARM;
- a wrong operation;
- a wrong plan hash;
- existing outputs.

## Rocky's first-boot reboot (observation for a follow-up)

Rocky's bhyve consistently starts about 5 minutes after `start-rocky`, while
`boot_indeterminate` climbs (4, 5, 6 across zq3-r4 to zq3-r6). GRUB may be
showing its menu (`menu_auto_hide=1`, `boot_success=0`). The vm-bhyve log in
the launch facts gives more detail. After each of the five `start-rocky`
calls on 2026-11-09, one bhyve run **exited with status 0, which is a
guest-initiated reboot, about 5 minutes after it started**, and vm-bhyve
started the next run. zq3-r5's example: `start-rocky` at 21:59:09 UTC, run 1
exited 0 at 22:04:03, then run 2 (PID 21428). So the PID in the facts always
belongs to the run after that reboot. zq3-r5's unexplained "restart at
20:20 UTC" was also a bhyve exit 0, so the guest rebooted itself then too.

## Findings and open questions

- **Q1 first-boot reboot under the exporter (risk for Phases 8-10).** The
  cause of the 5-minute guest reboot is unknown. If it also happens when the
  test kernel boots under the controlled exporter, bhyve exits about
  5 minutes after the Phase 8 launch. That would end a matrix that needs
  25-35 minutes. No run has kept the test guest up much past 5 minutes:
  zq3-r5 restarted Rocky 5.6 minutes after stopping it. Before the GO, please
  check Rocky's journal for the reboot at 22:04 UTC (read-only, for example
  `journalctl --list-boots` and `journalctl -b -1 -n 50`).
- **Q2 the mount-0 outcome is the main unknown.** If 964 does not cure R5-1,
  the run ends as zq3-r5 did, with T1-T11 unrun. The live snapshot and the
  mount process state are the only additions to that path.
- **Q3 T9 ARM refused, Q4 Phase 11 step 2 after a FENCED T10, Q5 the T11
  receipt wait (5 minutes):** unchanged from zq3-r5; the sheet keeps the
  same choices.
- **Q6 UEFI vars drift.** The record pins `bcc505d5…`. Any Rocky boot
  before Phase 7 changes it, and `host.py setup` then refuses. The two boots
  per `start-rocky` make this likelier.
- **Q7 memory.** At 23:56Z free + inactive was 12.89 GB with Rocky running
  (zq3-r5 at 21:21Z: 12.9 GB). zq3-r5's Phase 6 margin after the drain was
  0.53 GB.
- **Q8 window pins file:** the r6 window pins live only in the private fill,
  with their hash recorded here.
- **Q9 zq3-r5's f3 observer key:** the zq3-r5 record left its deletion to
  Paul. The fill cannot see whether that happened. The r6 Phase 1 facts
  list the zq3-r5 directories only.

No committed tool refused real inputs. These intended refusals all occurred:
- the controller template;
- the unfilled bootstrap template;
- the window filler on the current module state (Phase 1a resolves it);
- the superseded z54 candidate in a synthetic batch plan and in `rocky_kernel.py plan`.

## Local verification

At `ded27b3` in the main checkout, all passing:
- retained9p: 280 tests;
- directlaunch: 26 tests;
- `tools/lab` top level: 245 tests (3 skipped);
- `go test -count=1 ./internal/runtimeprep`.

`shellcheck` is clean for `build-stage.sh` and `sh/*.sh` (as POSIX sh), and
for `session.sh` apart from SC2034/SC2086: its variables are used by the
sheet, and `$O` is split on purpose. No code was changed.
