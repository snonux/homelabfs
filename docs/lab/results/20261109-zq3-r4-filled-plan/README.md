# zq3-r4 filled run inputs (2026-11-09)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r3 fill](../20261109-zq3-r3-filled-plan/README.md) ·
[zq3-r3 record](../20261109-zq3-r3/README.md)

The exact run inputs for `zq3-r4`, the fourth attempt of zq3, generated on the
workstation with the committed tools at `main` `8f2a860` (after w54 and y54).
They mirror the [zq3-r3 fill](../20261109-zq3-r3-filled-plan/README.md),
which ran for real, including its review-round-1 execution notes. The changes
are the run ID, the w54 candidate, the w54 guest budget floors with a rebuilt
`hfs-runtime-prep`, the fresh facts, the re-pinned scripts, the zq3-r3
lessons, and a reworked Phase 8 tail and Phases 9-10, which no attempt has
reached yet. Nothing here used SSH, the network, the lab, a VM, a mount, an
installation or the operator private key. **No authority follows from these
files:** every class A step in the [run sheet](run-sheet.md) still needs
Paul's explicit final GO.

`fdescfs.ko` and `fusefs.ko` are loaded on f3 again (left by zq3-r3, same
boot), so the zq3-r3 decision D1-a applies again. The sheet now runs it as
**Phase 1a**, right after Phase 1 and before staging.

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-zq3-r4-fill` (`0700`). This directory holds only
public hashes, paths, argv and the read-only facts. The per-session keys, the
runtime config (it holds host receipt private keys) and the staging tarballs
that contain it stay private and are never committed.

## Inputs

| Input | Value |
|---|---|
| Candidate | w54 kernel rebuild: manifest `89c928d5…`, package `/home/paul/tmp/claude/hfs-xz3-20261109d/package` (all 195 manifest entries checked), source `ff866c5`; kernel `6.1.190-hfs-xz3` bzImage `5c46f218…`, System.map `d544349f…`, build ID `806cfbb2…`. Daemon `174949c1…`, bhyve `36423b6d…`, config, five modules, window artifacts and init helper `8c02b98f…` unchanged |
| Phase 1 facts | [facts/](facts/), collected read-only 2026-11-09 18:27:00Z. f3: boot `1794212992` (unchanged since zq3-r2), `fdescfs.ko` (id 26) and `fusefs.ko` (id 27) loaded with no fuse/fdesc mount, free 5.05 GB + inactive 6.60 GB, ARC 0.81 GB, `/usr/bin/ssh` `6a9e5573…`. Rocky: PID `76128` (UTC lstart `Mon Nov  9 16:46:08 2026`), boot ID `93655c8a-…`, stock `5.14.0-687.66.1.el9_8.x86_64`, `next_entry` empty, `boot_indeterminate=4`, python3.9 `1143733d…`, `authorized_keys` `e08542dc…`. The files hold no key material (grep for key headers and `ssh-*` lines) |
| Launch facts | `f3-launch-facts-20261109T182700Z.json` (`d3730a1c…`): `rocky.conf` `c101adf1…` unchanged, **UEFI vars `0d2d251e…`** (zq3-r3: `c71240e9…`; Rocky has booted since) |
| Tap | `tap100` (bridge members `re0`, `tap0`) |
| Run ID / staging | `zq3-r4`, `/var/tmp/hfs-validation-xz3/zq3-r4` (parent exists, holds zq3-r1 to zq3-r3) |
| Native guard root | `/var/tmp/hfs-20261109-zq3r4-fdq`, host profile `--rocky-pid 76128 --rocky-birth 'Mon Nov  9 16:46:08 2026'`, guard `576d802a…` (unchanged) |
| Window directory | `/var/tmp/hfs-fdescfs-windows/zq3-r4` (parent exists, `root:wheel 0700`) |
| Rocky names | `/root/hfs-zq3-zq3-r4-*`, `/var/tmp/hfs-zq3-zq3-r4` (the zq3-r1 to zq3-r3 ones stay as evidence) |
| Operator public key | `2f6d909c…`; key path `…/hfs-zq3-r4-fill/operator/operator-signing-key` (empty `0700` directory, never read here) |

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | **`hfs-runtime-prep` rebuilt** at `8f2a860` (`-trimpath`, VCS-stamped, unmodified private clone): the r3 binary `22de7965…` predates the w54 floors. **Signer reused**: `go list -deps ./cmd/hfs-retained9p-operator` excludes `internal/runtimeprep`, `git diff ea55eb5 8f2a860 -- internal cmd go.mod go.sum` reaches it only through `internal/guestbridge/README.md` (not embedded), and `-buildvcs=false` builds of both commits are byte-identical (`345a6de8…`), so the signer pins in `hpin` and the watcher stay. G3 init helper: zq3-r1's reproduced build | prep `5612eeea…`, signer `22e5fb90…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from `testdata/zq3-spec.json` at `8f2a860` with only `zq3-r1` → `zq3-r4` and the r4 operator key path; guest budget reservations 1024, received 256, 512/512 MiB (w54). Floors and actual-runtime validation passed | spec `152ceb82…`, generation record in [pins](pins.json), request template `10c1cfa9…`, runtime config `9a917c40…`, volume-init `36ff07ba…`; volume `14112ef2-…`; sessions hfs0 `da6a4df4-…`, hfs1 `70a68170-…` |
| 1a. Floor check | One-off on scratch copies (`floor-check/`): the r4 spec passes; zq3-r3's guest budget is **refused** (`reservations 32/received 64 below the guest slot floor 380/60 for 2 mounts`), and r3's 128/128 MiB with r4's slots is **refused** (`below the measured guest byte floor 293601280/253755392`). The r3 tool accepted r3's budget. No scratch bundle kept | in [pins](pins.json) `guest_floor_check` |
| 1. Argv | `argv.py generate` from the 18:27Z launch facts (sessions, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `dddf5c43…`, args `5343b01a…`, vars `0d2d251e…` |
| 2. Kernel plan | `rocky_kernel.py plan` at main (`15cc9acb…`, y54 admits only `89c928d5…`), stock default `/boot/vmlinuz-5.14.0-687.66.1.el9_8.x86_64`; differs from zq3-r3's in run ID, manifest, bzImage size and hash, System.map hash and build ID | plan `33555f9f…`, build ID `806cfbb2…` |
| 3. Controller batch | template derived from zq3-r3's (run ID, fill path, sessions, candidate `89c928d5…`; `controller.py` `b1384270…` changed only its admitted/superseded sets); `check` refuses as designed (`probe/installed-manifest pin`); a synthetic copy (zero IDs) passes the main `controller.py`, and the same copy with the r54 candidate is refused as superseded | template `d86eb916…` |
| 4. Bootstrap plan | template derived from zq3-r3's (the one that ran). It carries new bundle, kernel plan, `rocky_kernel.py` (`15cc9acb…`) and rebuilt prep (`5612eeea…`) pins; the guard pin `fece96b3…` is the unchanged `runtime-guard.py`. `derive-templates.py` re-hashes every pinned file at main. Unfilled template refuses as designed. **F1 filled now** with `fill_plan.py bootstrap` from the 18:27Z `sha256 -r /usr/bin/ssh` line, then `bootstrap.py check` **pass** | template `054d7bf2…`, filled plan `aaaf0852…` |
| 5. Guard argv | derived from zq3-r3's (already with `--disposable-validation`), then compared with `bootstrap.py`'s own `SshRemote.guard_argv()` for the filled plan: equal, and it matches `runtime-guard.py`'s `check_command` shape | in [pins](pins.json) |
| 6. Observer and staging pins | unchanged script pins (`observer_key.py` `cd7f38c2…`, `observer.py` `2e1449dd…`, `installed.py` `c824b183…`, native guard `576d802a…`, `runtime-guard.py` `fece96b3…`). Re-pinned to main: `rocky_kernel.py` `15cc9acb…`, `argv.py` `83d54db0…` (comment only). All 17 f3 and 18 Rocky staged files went into two deterministic tarballs (a second tar run gave the same hashes), and a verification extract matched every expected hash and the f3 modes | f3 tar `1ac56598…`, Rocky tar `a0b5f487…` |
| 7. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` with `zq3-r1` → `zq3-r4` (equal to the r3 pins renamed); `check_pins` passes. Synthetic fills (not pins): zq3-r3's Phase 4 facts (after its D1-a) renamed **pass**; zq3-r2's post-restore facts (the current f3 module state) **refuse** with `fdescfs module present`, which Phase 1a resolves | r4 pins `d2db5012…` |
| 8. Signing watcher | zq3-r3's `bin/sign_watch.py`: only `D`, the docstring run ID and the reused-signer comment changed. Signer pin `22e5fb90…` in the watcher and in `hpin`. Tested locally with a fake signer: a wrong request hash, a wrong command, an unknown action, a repeat and a wrong signer hash were refused; both exact requests were signed | `2242a5c3…` |
| 9. Run sheet | [run-sheet.md](run-sheet.md): Phases 0-12, 1a and U1-U3. `session.sh` (`d29bdecc…`, new `sha`, `jf`, `obs`) and ten pinned helper scripts, four of them new for Phases 8-10 (`p9-ready.py`, `p9-observe.py`, `p10-argv.py`, `p10-requests.py`), dry-run against the committed correlator and controller (below) | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-09T00:00:00Z --format=gnu`.
The f3 tarball holds `hfs-20261109-zq3r4-fdq` and `hfs-validation-xz3/zq3-r4`
only, **without an entry for the existing parent** `hfs-validation-xz3/`, so
extraction never rewrites the parent's mode or owner. All directories `0700`,
binaries `0500`, other files `0400`, owner `0/0`.

## Signing

Paul delegated both signatures (A7) to Claude. In Phase 8 the driver's stdout
goes through `bin/sign_watch.py` (`2242a5c3…`). It runs the signer
(`22e5fb90…`) only if the printed command is exactly one of the two below and
the request file's SHA256 equals the printed `request_sha256`, each at most
once. Anything else is refused, and the driver then times out (fails closed):

```
/home/paul/tmp/claude/hfs-zq3-r4-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r4-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r4-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r4-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r4-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r4-fill/bootstrap-run/work/signer-binding
```

The operator key is placed at the bundle's path by Claude before the run, on
Paul's instruction. Nothing in the fill reads it.

## Run-time fills

| Fill | When and source | Used by |
|---|---|---|
| D1 | **decided**: D1-a as Phase 1a (normal `kldunload` of fusefs and fdescfs right after Phase 1) | Phase 4 step 0 window plan fill |
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 18:27Z fact); Phase 4 step 0 rechecks it (`F1-unchanged`), a change stops | bootstrap `observer.ssh_sha256` (plan `aaaf0852…`) |
| F2 fdescfs window plan and its hash (`WP`) | Phase 4 step 0: controller `facts`, host hashes, `realpath` library closure, f3 `date -u +%s`, then `fdescfs-window-plan.py --pins` with the r4 pins | setup, qualify, restore (`W`); Phase 5 recheck |
| F3 pod rootfs manifest digest (`PM`) | Phase 3: `pod_fixture.py manifest` on Rocky (zq3-r1 to zq3-r3: `f7f69339…`); `skip` if A8 is declined | `build`, T7 request |
| F4 observer key pair, enrolled line, `after_sha256` (`EA`) | Phase 4 step 5: `ssh-keygen` on f3, `observer_key.py enrol` | runtime observer, Phase 11 `remove --expect-sha256`, banner-timeout check |
| F5 BLS entry ID | Phase 3 install record | `select` (from the state directory) |
| F6 boot ID, provider nonce, admission manifest SHA256, `virtioN` names, proposal, controller, provision hash | Phase 8 admission and runtime records | enrollment and bind requests (bootstrap fills them), controller batch plan |
| F7 m0/m1 mount IDs | Phase 8 mount steps (mountinfo) | batch plan, then `p10-requests.py` for the T6/T7/T8 requests |
| F8 batch plan and its hash (`B`) | Phase 8, after READY: `fill_plan.py batch`, then `check-retained9p.sh check` | every controller and correlator call |
| F9 runtime UUID (`U`), cut, epoch and lifecycle sequences | READY record via `p9-ready.py`; sequences 1 and 2 literal, then each from `correlate.py command` (`obs`, ARM, RESTART) | Phase 9 OBSERVE, T1-T11 |
| F10 exporter child PID (`XP`) | guard `receipt.json` `exporter.pid` (Phase 11 step 0; `--no-child` if null) | `host.py cleanup --child-pid` |
| F11 Rocky PID and lstart | **fixed** from the 18:27Z facts (`76128`, `Mon Nov  9 16:46:08 2026` UTC); rechecked in Phases 1, 4 and 5 | native guard host profile, Phase 5 recheck, `p5-mem.txt` RSS |
| F12 UMA drain decision | Phase 5 step 1a: `p5-estimate.py` `drain_recommended` | Phase 5 step 1b (or once in Phase 6) |
| F13 T6/T7/T8 request files | Phase 8 after the batch check: `p10-requests.py` (exclusive, `0600`, in `$E`) | T6, T7, T8 |
| F14 guard PID | Phase 11, only on the runtime-still-serving path | the one SIGTERM to the guard |

## Changes from the zq3-r3 sheet

All zq3-r3 rules and its review-round-1 execution notes are kept: D1-a
before staging (now its own **Phase 1a**), the UMA drain decision in Phase 5,
the mandatory one-line-per-step Phase 11 order, observer key removal on every
abort path after Phase 4 step 5, the guard status read from
`$R/guard/receipt.json`, and no retry of a failed mount in the same guest
boot. Added from the zq3-r3 run:

- **Pause when f3 is unreachable (R3-2)**: record the time and a ping, resume
  after a minute under 100 ms RTT, retry once only after proving nothing ran.
  A host-preflight-only failure in Phase 8 is retried once after moving the
  output to `bootstrap-run.preflight-timeout-1` (what zq3-r3 did).
- **Slow guest shutdown (R3-4)**: Phase 11 step 6 waits up to 10 minutes;
  Phase 12 waits up to 10 minutes for Rocky's SSH.
- Phase 8 expectations for the w54 kernel: mount-0 should pass `bind_super`
  and reach ROOT, then mount-1 and READY; a too-small budget would now fail at
  `provision`; a mount failure should return an error instead of oopsing.
- Run-time values are taken from the recorded JSON with `jf`
  (`PM`, `EA`, `B`, `U`, `XP`) instead of being typed.

**Phases 8-10, reviewed against `controller.py`, `correlate.py`,
`fill_plan.py`, `guest-lock-probe.py` and `pod_fixture.py` at `8f2a860`:**

- After READY: batch fill and independent check (`B` from the fill record),
  `p9-ready.py` (READY record, socket `$R/runtime/owner/control.sock`,
  runtime/cut/epoch), `p10-requests.py` (T6/T7/T8 requests from the checked
  plan's mount IDs; zq3-r1 to zq3-r3 left these as hand edits).
- Phase 9: the first OBSERVE uses sequence 1, and `p9-observe.py` runs the
  correlator's own checks on it (complete, SERVING, no publication, one
  unfenced admission with the plan's boot/provider/sessions, READY's
  runtime/cut/epoch).
- Phase 10 is written out line by line for every case. T1's baseline uses the
  literal sequence 2, because `correlate.py command` needs the controller run
  that `probe` creates. Every later OBSERVE goes through `obs`
  (`correlate.py command` → `p10-argv.py` → one f3 call); ARM and RESTART go
  through `correlate.py command` and `p10-argv.py` directly. The expected
  sequences are listed (case pairs 2/3 to 10/11, ARM 13, RESTART 16).
- Fixes against the zq3-r3 sheet: the r3 Phase 10 text gave the release
  `--correlation-sha256 <h>` and the OBSERVE sequences as placeholders and had
  no command for running the printed ARM/RESTART argv. It also had no check
  that the batch was released before T9 (`syscall_batch_success`) and no
  receipt wait before T11. T9 now uses `--lost-intent MKDIR` (the daemon
  carries d44), with its expected classification; the post-CALL OBSERVE
  passes the ARMED reply as `--last`. T10's restart refusal now leads to
  T10/T11 unrun and the runtime-serving cleanup path; T11 waits for the guard
  receipt (`runtime_running_at_guard_exit` false). T8's expected refusal text
  is named.
- Phase 11: the guard-SIGTERM branch now covers any stop while the runtime
  still serves (not only a RESTARTED T10); a new step 0 reads the guard
  receipt for `XP`; step 2 says what to do when the blocked m0 does not
  unmount after a FENCED T10 (record, never force, continue to the poweroff).

## Findings and open questions

- **Q1 T9 ARM refused.** The sheet then records T9 failed and T10/T11 unrun
  (their expected outcomes depend on T9), runs T8 and cleans up with the
  runtime serving. Running T10 anyway would also be defensible; please
  confirm.
- **Q2 Phase 11 step 2 after a FENCED T10.** m0's session is blocked and
  the host is gone, so a normal unmount may refuse or hang. The sheet records
  that and continues to the poweroff (never `umount -f`/`-l`). Please confirm.
- **Q3 T11 receipt wait** is bounded at 5 minutes (30 × 10 s), my choice from
  the guard's own 30 s grace and 300 s observation.
- **Q4 brief pointer.** `tools/integration/artifacts/evidence/xz3-20261109d/README.md`
  has no section on what a zq3-r4 fill must change (as for zq3-r3, Q2). This
  fill took the changes from the candidate table, the w54/y54 diffs and the
  zq3-r3 record.
- **Q5 UEFI vars drift (open since zq3-r1 F4).** The record pins
  `0d2d251e…`. Any Rocky boot before Phase 7 changes it, and `host.py setup`
  then refuses.
- **Q6 memory.** At 18:27Z free + inactive was 11.65 GB with Rocky running
  (zq3-r3 at 14:11Z: 7.66 GB). The Active-based estimate is first measured in
  Phase 5; Phase 6 decides.
- **Q7 window pins file**: the r4 window pins live only in the private fill,
  with their hash recorded here.
- F3, F5 and F6 of zq3-r1 (120 s bootstrap, RESTARTED T10 leaves the runtime
  serving, `test_bootstrap` reads the run plan) still apply unchanged.

No committed tool refused real inputs. The intended refusals occurred as
designed: the controller template, the unfilled bootstrap template, the
window filler on the current module state (resolved by Phase 1a), the
superseded r54 candidate in a synthetic batch plan, and the w54 floors on
zq3-r3's guest budget.

## Local verification

At `8f2a860` in the main checkout: retained9p 280 tests, directlaunch 26,
`tools/lab` top-level 245 (3 skipped), `go test ./internal/runtimeprep`, all
passing. The four new helpers and `obs` were dry-run in scratch against
`test_correlate.py`'s fixtures with the real correlator and controller and a
stub `f3`: each accepted the expected shapes, and each refused a wrong
sequence, fenced admission, rotated cut, incomplete OBSERVE, unfiltered ARM,
wrong operation, failed summary and existing output. `shellcheck` is clean
for `build-stage.sh` and `sh/*.sh` (as POSIX sh), and for `session.sh` apart
from SC2034/SC2086 (its variables are used by the sheet, and `$O` is split on
purpose). No code was changed.
