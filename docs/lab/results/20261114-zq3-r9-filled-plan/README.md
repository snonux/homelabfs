# zq3-r9 filled run inputs (2026-11-14)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r8 fill](../20261110-zq3-r8-filled-plan/README.md) ·
[zq3-r8 record](../20261114-zq3-r8/README.md) ·
[l84 candidate](../../../../tools/integration/artifacts/evidence/xz3-20261114-r9/README.md)

These are the exact run inputs for `zq3-r9`, the ninth attempt of zq3. They
were generated on the workstation with the committed tools at `main`
`dcf2ed4` (after l84). They mirror the
[zq3-r8 fill](../20261110-zq3-r8-filled-plan/README.md), which ran for real
up to its `usable` failure (R8-1) and through a clean Phase 11 and 12. The
changes are:
- the run ID;
- the l84 candidate `e585206a…`, which rebuilt only the daemon (`868dc045…`);
  the kernel, its modules and the exporter are y64's bytes;
- `hfs-runtime-prep` and the signer rebuilt;
- everything that pins them, re-pinned;
- the bundle regenerated from the unchanged committed spec with the new code;
- the b84 `usable` gate (bootstrap journal `/3`) and the lifecycle sequences
  shifted by its `observe_sequence_last` (L);
- OBSERVE evidence `/2` in the Phase 9 helper;
- **Phase 1a (D1-a) runs again:** zq3-r8 left `fdescfs.ko` and `fusefs.ko`
  loaded;
- a read-only 984 measurement of OBSERVE wall time;
- the fresh facts.

Nothing here used SSH, the network, the lab, a VM, a mount, an installation
or the operator private key. **No authority follows from these files:**
every class A step in the [run sheet](run-sheet.md) still needs Paul's
explicit final GO, including the Phase 1a unload. **zq3 acceptance stays
incomplete by design**, because T5 does not run.

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-zq3-r9-fill` (`0700`). This directory holds only
public hashes, paths, argv and the read-only facts. These stay private and
are never committed:
- the per-session keys;
- the runtime config, which holds host receipt private keys;
- the staging tarballs that contain it.

## Inputs

| Input | Value |
|---|---|
| Candidate | **l84 rebuild** (task l84): manifest `e585206a…`, package `/home/paul/tmp/claude/hfs-xz3-20261114-r9/package`, source `e92eaa1`. **Daemon `868dc045…`** (GNU build ID `64858f77…`; 784, 884, d84, g84, h84, b84, q64/f84). **Reused byte for byte from y64 `867ede36…`:** guest kernel `6.1.190-hfs-xz3` (bzImage `40afad22…`, build ID `870bb9d1…`, config `28fe4a8e…`, System.map `1ffea83f…`), the five modules and `modules.*`, the native exporter `ef8f17de…` (`argv.py` `ed63ff97…` unchanged) and `guest.py` `c50bb849…`. Window artifacts `30ba40be…` (test) and `50d823be…` (helper), rebuilt from `e92eaa1`. Init helper `8c02b98f…` unchanged |
| Phase 1 facts | [facts/](facts/), collected read-only 2026-11-14 16:04:08Z; they supersede the 20261114T075709Z set. **f3:** boot `1794574308` (unchanged since zq3-r8), **`fdescfs.ko` (id 25) and `fusefs.ko` (id 26) loaded**, no fuse/fdesc mount, free 12.68 GB + inactive 1.22 GB, ARC 1.47 GB, `/usr/bin/ssh` `6a9e5573…`, python3.12 `a66eb370…`, `10.20.0.133` on `vm-public` (members `tap0`, `re0`). **Rocky:** PID `13234` (UTC lstart `Sat Nov 14 08:47:16 2026`, started by zq3-r8's Phase 12), boot ID `83c76231-…`, stock `5.14.0-687.66.1.el9_8`, `next_entry` empty, `boot_indeterminate=7`, SELinux Enforcing, `/.autorelabel` absent, no `hfs-xz3` file in `/boot` (zq3-r8's U2), python3.9 `1143733d…`, `authorized_keys` `e08542dc…`. The files hold no key material (grep for key headers and `ssh-*` lines) |
| Host events since zq3-r8 | Only zq3-r8's own Rocky stop (08:42:23) and normal start (08:47:16 UTC, no relabel reboot). No f3 reboot since Paul's deliberate 2026-11-13 reboot. The 2026-11-10 f3 reboot and Rocky's self-reboots (2026-11-10 19:32, 2026-11-11 10:08 UTC) still have no recorded cause |
| Launch facts | `f3-launch-facts-20261114T160408Z.json` (`24443ae1…`): `rocky.conf` `c101adf1…`, vm-run `43753bc8…`, firmware `6b48e967…` unchanged; **UEFI vars `6de05da2…`** (zq3-r8: `49bdc1ef…`); launch logged `2026-11-14T10:47:16+02:00` |
| Tap | `tap100` (bridge members `re0`, `tap0`) |
| Run ID / staging | `zq3-r9`, `/var/tmp/hfs-validation-xz3/zq3-r9` (the parent exists and holds zq3-r1 to zq3-r8) |
| Native guard root | `/var/tmp/hfs-20261114-zq3r9-fdq`, host profile `--rocky-pid 13234 --rocky-birth 'Sat Nov 14 08:47:16 2026'`, guard `576d802a…` (unchanged) |
| Window directory | `/var/tmp/hfs-fdescfs-windows/zq3-r9` (the parent exists, `root:wheel 0700`) |
| Rocky names | `/root/hfs-zq3-zq3-r9-*`, `/var/tmp/hfs-zq3-zq3-r9` (the zq3-r1 to zq3-r8 ones stay as evidence) |
| Operator public key | `2f6d909c…`; key path `…/hfs-zq3-r9-fill/operator/operator-signing-key` (empty `0700` directory, never read here) |

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | **Rebuilt** at `dcf2ed4`: `hfs-runtime-prep` and the signer `hfs-retained9p-operator`. The build used `-trimpath`, CGO, VCS stamps and an unmodified private clone (`vcs.modified=false`); a second build of each, forced with `-a`, gave the same bytes. Reason: `git diff 5a35150 dcf2ed4` over the `go list -deps` directories of both commands lists non-test changes in `internal/controlledruntime` (5 files), `controlledvalidation` (3), `guestbridge` (3) and `viewcontrol` (10). `go.mod`/`go.sum` are unchanged. zq3-r8's builds (prep `29ce5bdb…`, signer `b6612534…`) are retired. G3 init helper: zq3-r1's reproduced build | prep `14c2ffca…`, signer `efcf6fbf…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from `testdata/zq3-spec.json` at `dcf2ed4` (`make-spec.py`: only `zq3-r1` → `zq3-r9` and the r9 operator key path change; the same method reproduces zq3-r8's spec from `5a35150` byte for byte). The committed spec is unchanged since r8, so the r9 spec is r8's with the run ID renamed; the sizing and guest budget are the same | spec `2a83776c…`, request template `e8c3da67…`, runtime config `a9fa59c3…`, volume-init `0116e35f…`; volume `33e0619d-…`; sessions hfs0 `1c31c3e7-…`, hfs1 `10203c44-…` |
| 1. Argv | `argv.py generate` from the 16:04Z launch facts (sessions, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `04da686a…`, args `55a91eda…`, vars `6de05da2…` |
| 2. Kernel plan | `rocky_kernel.py plan` at main (`74309586…`; admits only `e585206a…`), stock default `/boot/vmlinuz-5.14.0-687.66.1.el9_8.x86_64`. It differs from zq3-r8's only in `run_id` and `manifest_sha256`. The y64 package `867ede36…` is refused as superseded ("use the l84 rebuild") | plan `baa47e01…`, build ID `870bb9d1…` |
| 3. Controller batch | Template derived from zq3-r8's by `derive-templates.py`: run ID, fill path, sessions, candidate `867ede36…` → `e585206a…`. **T5 stays deferred**, with a new reason: q64/f84 fixed the view-lifetime wall, but T5 is not yet re-planned or sized. The four other requests keep canonical order. `controller.py` `4eb3b0b8…`. `check` refuses the template as designed (`probe/installed-manifest pin`). A synthetic copy with zero IDs **passes** and prints `cases_planned` (four) and the deferral. The same copy is **refused** with the y64 candidate (superseded), without the declaration ("five-case syscall batch minus declared deferrals") and with `credentials` also deferred ("case may not be deferred") | template `f035e4c0…` |
| 4. Bootstrap plan | Template derived from zq3-r8's (the one that ran). New pins: bundle, kernel plan, `rocky_kernel.py`, the l84 daemon `868dc045…`, the rebuilt prep `14c2ffca…` and the b84 `bootstrap_remote.py` `0dcb0fe0…` (`remote_sha256`, with `host-observe`). The guard pin `fece96b3…` is unchanged. The script re-hashes every pinned file at main and in the package. The unfilled template refuses as designed (`observer.ssh_sha256: SHA256 hex`). **F1 filled now** with `fill_plan.py bootstrap` (`893350cf…`) from the 16:04Z `sha256 -r /usr/bin/ssh` line (`6a9e5573…`, unchanged), then `bootstrap.py check` (`5e467fe6…`, journal `/3`) **pass** | template `d044fe95…`, filled plan `5751da47…` |
| 5. Guard argv | Derived from zq3-r8's, with `--runtime-sha256 868dc045…` (checked against the package) and `--disposable-validation` exactly once, last. It equals `bootstrap.py`'s own `SshRemote.guard_argv()` for the filled plan, in shell string and argv, and passes `runtime-guard.py`'s `parse_args` and `check_command` | in [pins](pins.json) |
| 6. Observer and staging pins | Unchanged script pins: `observer_key.py` `cd7f38c2…`, `observer.py` `2e1449dd…`, `installed.py` `c824b183…`, native guard `576d802a…`, `runtime-guard.py` `fece96b3…`, `fdescfs-window.py` `b00b19d1…`, `host.py` `738e1ca5…`, `host_snapshot.py` `b29ffed2…`, `guest-lock-probe.py` `630a737d…`, `pod_fixture.py` `ab4d269b…`, `argv.py` `ed63ff97…`. Re-pinned to main: `rocky_kernel.py` `74309586…`. All 17 f3 and 18 Rocky files went into two deterministic tarballs; a second tar run gave the same hashes. A verification extract matched every expected hash and the f3 modes (owner `0/0`, no parent entry) and was then deleted | f3 tar `3ed93f63…`, Rocky tar `67c96cbb…` |
| 7. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` at main (`d1b75dcb…`, as l84 left it) with `zq3-r1` → `zq3-r9`; `check_pins` passes. Synthetic fills, which are not pins: zq3-r8's real Phase 4 facts and host hashes (modules absent) **pass**, with paths renamed and the three artifact hashes swapped for the l84 ones. The same facts with zq3-r8's unswapped host hashes **refuse** (`pinned artifact differs or missing`). Post-restore facts with both modules loaded, the state on f3 now, **refuse** (`fdescfs module present`): hence Phase 1a | r9 pins `142dc162…` |
| 8. Signing watcher | zq3-r8's `bin/sign_watch.py` (it signed both requests in zq3-r8) with `D`, the docstring run ID, the signer comment and **the signer pin `efcf6fbf…`** changed. Tested locally with a fake signer: a wrong request hash, a wrong command, an unknown action, a repeat and a wrong signer hash were refused; both exact requests were signed | `9d1ed0df…` |
| 9. Process observer check | `sh/p4-observer.py` from zq3-r8 with the run ID and **the daemon pin `868dc045…`** changed (Phase 4 step 1a, **required**: new daemon bytes). zq3-r8's real records (renamed) are refused only for the y64 `sha256` and pass with `868dc045…` substituted. The `$NG` line parses and passes the native guard's own `check_request` | `c3894dd1…` |
| 10. Run sheet | [run-sheet.md](run-sheet.md): Phases 0-12, 1a (**runs**), 4/1a and U1-U3, with the SELinux gates in Phases 1, 9 and 12, and a run log template. `session.sh` (`9273f85e…`, with the new `tf3`) and twelve pinned helper scripts. `p9-ready.py` (`c32bfc90…`) requires journal `/3` and prints L; `p9-observe.py` (`16182bb4…`) requires sequence L+1 and evidence `/2`; `p9-timing.py` (`83938676…`) is new. The Phase 8-10 lines were dry-run end to end (below) | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-14T00:00:00Z --format=gnu`.
The f3 tarball holds only `hfs-20261114-zq3r9-fdq` and
`hfs-validation-xz3/zq3-r9`. It has **no entry for the existing parent**
`hfs-validation-xz3/`, so extraction never rewrites the parent's mode or
owner. All directories are `0700`, binaries `0500`, other files `0400`,
owner `0/0`.

## Signing

Paul delegated both signatures (A7) to Claude. In Phase 8 the driver's
stdout goes through `bin/sign_watch.py` (`9d1ed0df…`). The watcher runs the
signer (`efcf6fbf…`) only if:
- the printed command is exactly one of the two below;
- the request file's SHA256 equals the printed `request_sha256`;
- each command runs at most once.

Anything else is refused, and the driver then times out (fails closed):

```
/home/paul/tmp/claude/hfs-zq3-r9-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r9-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r9-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r9-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r9-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r9-fill/bootstrap-run/work/signer-binding
```

On Paul's instruction, Claude places the operator key at the bundle's path
before the run. Nothing in the fill reads it.

## Run-time fills

| Fill | When and source | Used by |
|---|---|---|
| D1 | **D1-a, Phase 1a**: both modules are loaded (16:04Z); Phase 1 confirms it, Phase 1a unloads them normally (never `-f`), and a refused unload stops before staging | Phase 4 step 0 window plan fill (needs both absent) |
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 16:04Z fact); Phase 4 step 0 rechecks it (`F1-unchanged`), a change stops | bootstrap `observer.ssh_sha256` (plan `5751da47…`) |
| F2 fdescfs window plan and its hash (`WP`) | Phase 4 step 0: controller `facts`, host hashes, `realpath` library closure, f3 `date -u +%s`, then `fdescfs-window-plan.py --pins` with the r9 pins | setup, qualify, restore (`W`); Phase 5 recheck |
| F3 pod rootfs manifest digest (`PM`) | Phase 3: `pod_fixture.py manifest` on Rocky (zq3-r1 to zq3-r8: `f7f69339…`); `skip` if A8 is declined | `build`, T7 request |
| F3a process observer record | Phase 4 step 1a: guard stage `process-observer`, checked by `p4-observer.py` | go/no-go before setup; **pass required** (new daemon bytes) |
| F4 observer key pair, enrolled line, `after_sha256` (`EA`) | Phase 4 step 5: `ssh-keygen` on f3, `observer_key.py enrol` | runtime observer, Phase 11 `remove --expect-sha256`, banner-timeout check |
| F5 BLS entry ID | Phase 3 install record | `select` (from the state directory) |
| F6 boot ID, provider nonce, admission manifest SHA256, `virtioN` names, proposal, controller, provision hash | Phase 8 admission and runtime records | enrollment and bind requests (bootstrap fills them), controller batch plan |
| F7 m0/m1 mount IDs | Phase 8 mount steps (mountinfo) | batch plan, then `p10-requests.py` for the T6/T7/T8 requests |
| F8 batch plan and its hash (`B`) | Phase 8, after READY and `usable`: `fill_plan.py batch` (prints `cases_planned` and `deferred`), then `check-retained9p.sh check` | every controller and correlator call |
| F9 runtime UUID (`U`), **`L`**, cut, epoch and lifecycle sequences | READY record and `summary.json` via `p9-ready.py` (`--runtime`, `--observe-last`); Phase 9 uses L+1 and the T1 baseline L+2, then each from `correlate.py command` (`obs`, ARM, RESTART); expected L+1 to L+14 with T5 deferred | Phase 9 OBSERVE, T1-T4, T9-T11 |
| F10 exporter child PID (`XP`) | guard `receipt.json` `exporter.pid` (Phase 11 step 0; the `--no-child` line if null) | `host.py cleanup --child-pid` |
| F11 Rocky PID and lstart | **fixed** from the 16:04Z facts (`13234`, `Sat Nov 14 08:47:16 2026` UTC); rechecked in Phases 1, 4 (also by every `$NG` stage) and 5 | native guard host profile, Phase 5 recheck, `p5-mem.txt` RSS |
| F12 UMA drain decision | Phase 5 step 1a: `p5-estimate.py` `drain_recommended` | Phase 5 step 1b (or once in Phase 6) |
| F13 T6/T7/T8 request files | Phase 8 after the batch check: `p10-requests.py` (exclusive, `0600`, in `$E`) | T6, T7, T8 |
| F14 guard PID | Phase 11, only on the runtime-still-serving path (`pgrep -U root`) | the one SIGTERM to the guard |
| F15 SELinux state | Phase 1 (`/.autorelabel` absent, Enforcing), Phase 9 (guest `getenforce` `Enforcing`, enforce `1`), Phase 12 (relabel record) | Phase 1 and Phase 9 gates; Phase 12 finding |
| F16 984 timing | Phases 9-10: `tf3` lines in `$E/obs-timing.tsv` (three `ssh-noop`, each timed OBSERVE); `p9-timing.py` after T4 and before Phase 11 | run log only; never a gate |

## Changes from the zq3-r8 sheet

All zq3-r7 and zq3-r8 rules are kept:
- **Phase 1a (D1-a): runs again**, as in zq3-r3 to zq3-r7: a normal
  `kldunload fusefs`, then `kldunload fdescfs`, never `-f`;
- Phase 4 step 1a, the process observer check: **required**, because the
  daemon bytes changed (`868dc045…`);
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

Kept from the zq3-r5 to zq3-r8 executions and the review decisions:
- **Phase 11 step 9 is one plain line with literal paths**:
  `f3 "doas -n rm -- /var/tmp/hfs-validation-xz3/zq3-r9/observer/id_ed25519 /var/tmp/hfs-validation-xz3/zq3-r9/observer/id_ed25519.pub"`.
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
- The z64 T5 deferral (reason updated, below), the Phase 1 check that
  `10.20.0.133` is on `vm-public`, the persisted-variable list (now with
  `L`), the m64 block-line rule and the Phase 9 tuple gate.

New for zq3-r9:
- **Candidate `e585206a…`** in the kernel plan, batch template, bootstrap
  plan, guard argv, window pins, staging, Phase 3, Phase 4 step 1a and
  Phase 8 expectations.
- **b84 `usable` gate (journal `/3`):** first probe; settle until mount-1's
  end + 40 s; a `baseline` OBSERVE with no open provider claim; the final
  probe; then a `claims` phase polling OBSERVE every 5 s for up to 30 s
  until each provider shows a TERMINAL GETATTR and PROCESS_EXIT numbered
  after the baseline. It ends at most about 100 s after mount-1 (a healthy
  gate ends after about 45-55 s), so the Phase 8 budget grows from about
  70 s to about 100 s for the gate. The sheet
  describes the three failure notes (a root `lstat`, `USABLE_CLAIMS_FAILED`
  for claims, `USABLE_OBSERVE_FAILED` for OBSERVE) and one path for all:
  inspect with the live owner snapshot, never retry, Phase 11 through one
  guard SIGTERM.
- **Sequences shift by L.** Phase 9 OBSERVE L+1; case pairs (L+2, L+3) …
  (L+8, L+9); T9 pre-arm L+10, ARM L+11, post-CALL L+12; T10 post-quiesce
  L+13, RESTART L+14. These match `docs/lab/zq3-run-plan.md` (b84),
  `correlate.py command` (latest reply + 1) and `controller.py`.
  `p9-ready.py --observe-last` prints L, and `L` is persisted in `vars.env`.
- **OBSERVE evidence `/2`:** `p9-observe.py` requires sequence L+1 and
  reports each provider's claim counts (`Allocated`, `Retired`, `Open`)
  without gating.
- **T5 deferral reason:** q64/f84 fixed the lifetime wall (merged), but T5
  is not yet re-planned or sized, so it stays deferred.
- **984 measurement:** `tf3` times three `f3 'doas -n true'` no-ops, the
  Phase 9 OBSERVE, the T1 baseline and every `obs` OBSERVE;
  `p9-timing.py` reports net OBSERVE seconds against the 984 model's bound
  of about 2.4 s per fresh guest observation. Evidence only. The sheet adds
  a run log template with the lines to record.

## Expectations: what changed in the runtime since zq3-r8

zq3-r8's `usable` probe claims stayed RESERVED behind a view recall (R8-1):
the y64 daemon recalled both domains on every clean READ release and every
PROCESS_EXIT, and each VIEW_ACK ran a synchronous guest SSH observation on
the shared control worker under the Admission lock. The l84 daemon changes:
- **784:** a clean READ release recalls only its own domain;
  PROCESS_EXIT/DESCRIPTION_CLOSE never recall;
- **884:** VIEW_ACK reuses a matching observation (SSH only when none is
  cached) and holds no Admission lock across SSH;
- **d84:** VIEW_ACK runs on its own transport worker lane;
- **g84:** VIEW_RELEASE is certified per lease;
- **h84:** a retrying cell can no longer starve its lane's queue;
- **q64/f84:** retired view tickets are recycled, and the ACK archive and
  recall UUIDs are pruned;
- **b84:** OBSERVE carries a bounded per-provider claim summary, which the
  `usable` gate reads.

So Phase 8 should reach `usable` ok with both providers' claims TERMINAL,
and T1-T4 and T6-T11 should get past admission without a guest `hfs …
blocked` line before T9. The offline 984 model says the lab passes only if
one guest SSH observation costs less than about 2.4 s; r9 measures an upper
bound of that cost for the first time.

## Phases 9-10 recheck

Since zq3-r8:
- `bootstrap.py` (`5e467fe6…`) writes journal `/3`, runs the b84 claim
  check through `bootstrap_remote.py` `host-observe` (`0dcb0fe0…`), and
  records `observe_sequence_last` in `summary.json` and the `usable` result.
- `fill_plan.py` (`893350cf…`) refuses `/1` and `/2` journals.
- `correlate.py` (`9cd55f2c…`) expects evidence `/2`.
- `controller.py` (`4eb3b0b8…`) admits only `e585206a…`.
- `guest.py` (`c50bb849…`) and `guest-lock-probe.py` (`630a737d…`) are
  unchanged.

No CLI the sheet uses changed shape.

**Sheet dry run** (`dryrun/` in the private fill; scratch output). A
harness extracted **all 76 command lines** of this sheet, from Phase 8
"After READY" to T8, plus the Phase 7 `k04` baseline line and the Phase 11
984 summary line, from `run-sheet.md` itself and ran them in order. It used:
- the real `fill_plan.py batch`, `check-retained9p.sh check`,
  `correlate.py`, `session.sh` (`tf3`, `obs`, `sha`, `jf`) and the `sh/`
  helpers;
- `controller.py` with faked guest dispatch;
- stub `f3`/`rocky` that answer like the lifecycle client (evidence `/2`
  with a claim summary), `host_snapshot.py` and the guest;
- a synthetic READY run with journal `/3`, an `ok` `usable` result, L = 5
  (the stub's last accepted sequence primed with 5) and mountinfo rows with
  the exact g64 options.

Every line passed with the expected results:
- L = 5; Phase 9 at 6 (L+1); case pairs 7/8, 9/10, 11/12, 13/14; T9 15,
  ARM 16, post-CALL 17; T10 18, RESTART 19;
- the batch fill and check printing `cases_planned` (four) and the deferral;
- T1-T4 correlated and released; `syscall_batch_success` True with the
  deferral in the state and in `missing`;
- T9 `dropped-reply-effect-committed-and-visible`;
- T10 FENCED;
- T11 `k04_refusal_observed` true, with `deferred` repeated;
- the T6/T8 lock requests from `p10-requests.py`;
- T8 `rc=0`;
- `p9-timing.py` summarised 12 timed OBSERVEs and 3 no-ops.

**Negative and real-record checks** (`helpers-dryrun.py`, all as expected):
- `p9-ready.py` refused a wrong socket, a missing or failed `usable`
  result, a `/2` journal header, a missing `observe_sequence_last`, L = 1,
  and a `usable`/summary L mismatch;
- `p9-observe.py` refused sequence 1 (the r8 literal), L+2, a READY_JSON
  without L, a fenced admission, a rotated cut, an incomplete OBSERVE,
  evidence `/1` and a wrong plan hash;
- `p9-timing.py` refused a malformed TSV and one without an OBSERVE line;
- `p10-argv.py` and `p10-requests.py` refused as in r8;
- `fill_plan.py batch` refused a synthetic run that stopped at `usable`
  with the b84 claims note (`bootstrap steps did not end ok:
  usable=failed`) and a `/2` journal ("is not
  homelabfs-zq3-bootstrap-journal/3"); `p9-ready.py` refused the failed run
  and zq3-r8's real run directory (outcome `failed`).

## Decisions from the zq3-r9 run-sheet review round 1 (2026-11-14)

Verdict GO. Applied: a "tool timeouts for long lines" rule (Phase 8 in the
background or with a tool timeout of at least 600 s; every `rockyt` and the
Rocky stop/start lines with at least 300 s), `printf %q` quoting for
`vars.env`, the Phase 11 timing summary shown as reference text in Phase 9,
the `usable` timing stated as a deadline (healthy gate about 45-55 s), and
`set -o pipefail` dropped from the Prepare block. Kept: Phase 1 infers
`/dev/fd` on devfs from the absence of an fdescfs mount (as in r8); the
Phase 7 failure path relies on the general abort rule for the observer key.

## Findings and open questions

- **Q1 T5 deferred.** The run cannot complete zq3 acceptance. The lifetime
  wall is fixed, but T5 needs a re-plan and sizing first.
- **Q2 the l84 fixes are untested on f3.** The daemon `868dc045…` passed
  offline tests only. The main questions for this run are Phase 4 step 1a,
  `usable` ok with both claim checks, and T1 past admission.
- **Q3 OBSERVE cost.** Each `usable` OBSERVE takes a fresh guest SSH
  observation and competes with the VIEW_ACK observations; the 984 model
  does not count them yet. A slow observation could fail the `claims`
  phase's 30 s window. The new timing lines measure it from Phase 9 on, not
  inside the gate.
- **Q4 memory.** At 16:04Z free was 12.68 GB, inactive 1.22 GB and ARC
  1.47 GB. The facts carry no Active count. Phase 5's estimate and the
  Phase 6 bar decide (zq3-r8 margin 0.52 GB after the drain).
- **Q5 UEFI vars drift.** The record pins `6de05da2…`. Any Rocky boot
  before Phase 7 changes it, and `host.py setup` then refuses. Recheck right
  before the run.
- **Q6 Phase 1a.** zq3-r3 to zq3-r7 unloaded both modules without refusal.
  A refusal now stops before staging and needs a plan revision.
- **Q7 window pins file:** the r9 window pins live only in the private fill,
  with their hash recorded here.
- **Q8 T9 ARM refused, T10 FENCED step 2, the T11 receipt wait (5 minutes):**
  unchanged from zq3-r8; T9-T11 have only been dry-run so far.

No committed tool refused real inputs. These intended refusals all occurred:
- the controller template;
- the unfilled bootstrap template;
- the superseded y64 `867ede36` candidate in a synthetic batch plan and in
  `rocky_kernel.py plan`;
- an undeclared four-case batch and a deferred `credentials`;
- the window filler on zq3-r8's unswapped artifact hashes and on facts with
  both modules loaded.

## Local verification

At `dcf2ed4` in the main checkout, all passing:
- retained9p: 357 tests;
- directlaunch: 26 tests;
- `tools/lab` top level: 245 tests (3 skipped);
- `go test -count=1 ./internal/runtimeprep`.

`shellcheck` is clean for `build-stage.sh` and `sh/*.sh` (as POSIX sh), and
for `session.sh` apart from SC2034/SC2086: its variables are used by the
sheet, and `$O` is split on purpose. No code was changed.
