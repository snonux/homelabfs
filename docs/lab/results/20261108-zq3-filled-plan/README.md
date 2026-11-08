# zq3-r1 filled run inputs (2026-11-08)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Run plan](../../zq3-run-plan.md) ·
[Plain-language summary](../../zq3-plan-summary.md) ·
[Phase 1 facts](../20261108-zq3-phase1/README.md)

The exact run inputs for `zq3-r1`, generated on the workstation with the
committed tools at `main` `08831e7`. Nothing here used SSH, the network, the
lab, a VM, a mount, an installation or Paul's operator private key. **No
authority follows from these files:** every class A step in the
[run sheet](run-sheet.md) still needs Paul's explicit final GO.

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-zq3-r1-fill` (`0700`). This directory holds only
public hashes, paths and argv. The per-session keys, the runtime config (it
holds host receipt private keys) and the staging tarballs that contain it stay
private and are never committed.

## Inputs

| Input | Value |
|---|---|
| Candidate | manifest `b203799e…`, source `defd974`, daemon `174949c1…`, bhyve `36423b6d…` |
| Launch facts | `f3-launch-facts-20261108T210707Z.json` (`b205ba01…`, h44-fixed `host.py`) |
| Phase 1 facts | [20261108-zq3-phase1](../20261108-zq3-phase1/README.md): Rocky `5.14.0-687.66.1.el9_8.x86_64`, python3.9 `1143733d…`, `authorized_keys` `e08542dc…` |
| Tap | `tap100` (bridge members were `re0`, `tap0`) |
| Run ID / staging | `zq3-r1`, `/var/tmp/hfs-validation-xz3/zq3-r1` |
| Operator public key | `2f6d909c…` |

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | `hfs-runtime-prep`, `hfs-retained9p-operator` built from `08831e7` (non-test `internal/`+`cmd/` equal to `defd974`); G3 init helper rebuilt with `build-init-helper.sh`, **reproduced** `8c02b98f…` | prep `5bdf2044…`, signer `6f51a325…` |
| 1. Bundle | `hfs-runtime-prep generate` from the zq3 example spec, changed only in `OperatorKey` (path below); actual-runtime validation passed | generation `d672011e…`, request template `5224b642…`, runtime config `409bef90…`, volume-init `747b12b3…`; volume `30ab1d1a-…`; sessions hfs0 `313279d4-…`, hfs1 `9cc910b4-…` |
| 1. Argv | `argv.py generate` (sessions, facts, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `ba493bf6…`, args `a4096e7b…` |
| 2. Kernel plan | `rocky_kernel.py plan`, stock default `/boot/vmlinuz-5.14.0-687.66.1.el9_8.x86_64` | plan `ac3a0e75…`, kernel build ID `acc77b5e…` |
| 3. Controller batch | template with all fixed fields; the real plan needs Phase 8 values, so `check` on the template refuses as designed (`probe/installed-manifest pin`); a synthetic copy (zero IDs) passes `check` | template `24f7116e…` |
| 4. Bootstrap plan | template with every pin (`remote_sha256` `121ee0cc…`, prep, template, runtime config, `bootstrap_seconds` 120 = `BootstrapSeconds`); `check` refuses only `observer.ssh_sha256` (f3 `/usr/bin/ssh` hash, a Phase 4 fact); a synthetic copy passes `check` | template `8996f535…` |
| 5. Guard argv | exact argv as `bootstrap.py` builds it; limits are the guard defaults, which equal the plan ceilings | in [pins](pins.json) |
| 6. Observer and staging pins | interpreter, pre-run `authorized_keys`, `observer_key.py` `cd7f38c2…`, and SHA256 of all 17 f3 and 18 Rocky staged files, plus two staging tarballs | f3 tar `51c52c5b…`, Rocky tar `2d7fd06c…` |
| 8. Run sheet | [run-sheet.md](run-sheet.md), Phases 0-12 plus U1-U3 | |

The synthetic copies only prove that the fixed fields pass the real
validators. Their hashes are **not** pins.

Signing (Paul, inside the 120 s bootstrap, both as user `paul` on the
workstation):

```
/home/paul/tmp/claude/hfs-zq3-r1-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r1-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r1-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r1-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r1-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r1-fill/bootstrap-run/work/signer-binding
```

The bundle names the operator key path
`/home/paul/tmp/claude/hfs-zq3-r1-fill/operator/operator-signing-key` (empty
`0700` directory). Paul copies his key there before Phase 8. Nothing here
reads it.

## Run-time fills

These values cannot exist before the run. Each is listed with its source and
the step that consumes it:

| Fill | When and source | Used by |
|---|---|---|
| F1 f3 `/usr/bin/ssh` SHA256 | Phase 4 step 0, read-only `sha256 -r` on f3 | bootstrap `observer.ssh_sha256`, then the final bootstrap plan hash and `bootstrap.py check` |
| F2 fdescfs window plan and its hash | Phase 4 step 0: controller `facts` (needs staged inspector), host hashes, `ldd` closure, f3 `date -u +%s`, then `fdescfs-window-plan.py` | setup, qualify, restore |
| F3 pod rootfs manifest digest | Phase 3: `pod_fixture.py manifest` on Rocky (needs the staged scripts) | `build`, pod request |
| F4 observer key pair, enrolled line, `after_sha256` | Phase 4 step 5: `ssh-keygen` on f3, `observer_key.py enrol` | runtime observer, Phase 11 `remove --expect-sha256` |
| F5 BLS entry ID | Phase 3 install record | `select` (from the state directory) |
| F6 boot ID, provider nonce, admission manifest SHA256, `virtioN` names, proposal, controller, provision hash | Phase 8 admission and runtime records | enrollment and bind requests (bootstrap fills them), controller batch plan |
| F7 m0/m1 mount IDs | Phase 8 mount steps (mountinfo) | batch plan, lock-probe requests, pod request |
| F8 batch plan and its hash | Phase 8, after READY: template plus F6/F7, then `check-retained9p.sh check` | every controller and correlator call |
| F9 runtime UUID and lifecycle sequences | READY record, then each reply | OBSERVE, ARM, RESTART (`correlate.py command` prints them) |
| F10 exporter child PID | runtime and guard evidence | `host.py cleanup --child-pid` |
| F11 Rocky PID and lstart recheck | Phase 1 recheck and Phase 4 (`80203`, `Sun Nov  8 16:05:53 2026` now) | native guard host profile |

## Findings

- **F1, window restore against the post-load baseline (expected failure,
  needs a decision).** `fdescfs-window.py restore` unmounts `/dev/fd` and then
  requires the full host `facts`, including `kldstat -v` and `mount -p`, to
  equal the baseline recorded right after loading fdescfs. The plan loads
  `fusefs.ko` after that baseline (Phase 4 step 4). The window pins forbid
  `fusefs.ko` at fill time, so it cannot be loaded earlier. With the committed
  tools, Phase 11's restore therefore unmounts normally, then reports `normal
  mount restore incomplete; retain resources` and writes no
  `restoration-summary.json`. Options: a reviewed code change before the run
  (let restore accept the plan-declared fusefs load), or Paul accepts this
  failure and the manual review of `restored.json`. Nothing was changed here.
  **Resolved by task j44 (after this fill):** the window pins and plan now
  declare `window_module` `/boot/kernel/fusefs.ko` `b207adf6…`, and restore
  accepts the post-load baseline plus exactly that file (any other
  difference still refuses). This changes the bytes of `fdescfs-window.py`
  and `fdescfs-window-plan.py`, so their hashes in [pins](pins.json)
  (`20e0e2ea…`, `07791469…`) and the f3 staging tarball `51c52c5b…` are
  stale: re-pin them from the commit used for the run before staging.
- **F2, manual steps without a dedicated tool.** No reviewed tool fills the
  controller batch plan after READY: it is a manual edit of four fields under
  time pressure, followed by `check`. No staging tool exists either: this fill
  defines tar-based staging with exact expected hash and stat lists. Both
  steps are root-orchestrated, as plan limitation G15 already states.
- **F3, 120 s bootstrap.** UEFI, GRUB, the kernel boot, SSH, admission (which
  refuses to start with less than 40 s left) and two human signatures must
  all fit inside 120 s, the base-gate maximum. Paul should keep both signer
  commands ready in a second terminal.
- **F4, UEFI vars drift.** The argv record pins vars `3c3d8783…`, and `host.py
  setup` refuses if the hash changed by Phase 7. Regenerating the record from
  fresh facts (same sessions and args) is then a plan revision.
- **F5, a RESTARTED T10 leaves the runtime serving.** The sheet has no
  supported stop for that case. The guard's 3600 s bound, or a signal to the
  guard (a retained stop), ends it.
- **F6, test harness detail.** `test_bootstrap.ConstantTests` reads
  `docs/lab/zq3-run-plan.md`, so it errors inside the tools snapshot; it
  passes in the checkout (259/259).

No committed tool refused real inputs. The only refusals were the two
intended template refusals above.

## Local verification

retained9p 259 tests, directlaunch 26, `test_fdescfs_window`,
`test_fdescfs_window_plan`, `test_runtime_guard`, `test_runtime_volume_init`
(1 skip), `test_vms_sh`, and `go test ./internal/runtimeprep`, all passing.
No code was changed.
