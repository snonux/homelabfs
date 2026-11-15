# zq3-r10 filled run inputs (2026-11-14)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r9 fill](../20261114-zq3-r9-filled-plan/README.md) ·
[zq3-r9 record](../20261114-zq3-r9/README.md) ·
[r84 candidate](../../../../tools/integration/artifacts/evidence/xz3-20261114-r10/README.md)

These are the exact run inputs for `zq3-r10`, the tenth attempt of zq3. They
were generated on the workstation with the committed tools at `main`
`5a95ad4` (after r84). They mirror the
[zq3-r9 fill](../20261114-zq3-r9-filled-plan/README.md), which ran for real
up to its `usable` failure at the final probe (R9-1) and through a clean
Phase 11 and 12. The changes are:
- the run ID (`zq3-r10`, one character longer than `zq3-r9`);
- the r84 candidate `4712e479…`, which rebuilt the guest kernel (p84, build
  ID `d73c85da…`) and the daemon (`34a5360b…`; o84, q84, i84); the config,
  the five modules and the exporter are l84's bytes;
- `hfs-runtime-prep` and the signer rebuilt;
- everything that pins them, re-pinned, and the kernel plan regenerated;
- the bundle regenerated from the unchanged committed spec with the new code;
- **Phase 1a (D1-a) runs again:** zq3-r9 left `fdescfs.ko` and `fusefs.ko`
  loaded;
- a local check that the 984 timing file captured its first four lines;
- the fresh facts.

Nothing here used SSH, the network, the lab, a VM, a mount, an installation
or the operator private key. **No authority follows from these files:**
every class A step in the [run sheet](run-sheet.md) still needs Paul's
explicit final GO, including the Phase 1a unload. **zq3 acceptance stays
incomplete by design**, because T5 does not run.

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-zq3-r10-fill` (`0700`). This directory holds only
public hashes, paths, argv and the read-only facts. These stay private and
are never committed:
- the per-session keys;
- the runtime config, which holds host receipt private keys;
- the staging tarballs that contain it.

## Inputs

| Input | Value |
|---|---|
| Candidate | **r84 rebuild** (task r84): manifest `4712e479…`, package `/home/paul/tmp/claude/hfs-xz3-20261114-r10/package`, source `cc6beed`. **Guest kernel rebuilt (p84):** `6.1.190-hfs-xz3`, bzImage `10f2da29…`, GNU build ID `d73c85da…`, System.map `a720bd4a…`; config `28fe4a8e…`, the five modules and `modules.*` byte-identical to `e585206a`. **Daemon `34a5360b…`** (GNU build ID `b7679dc7…`; o84, q84, i84 on top of l84). **Reused byte for byte:** the native exporter `ef8f17de…` (`argv.py` `ed63ff97…` unchanged) and `guest.py` `c50bb849…`. Window artifacts `72fffd3a…` (test) and `5c709a35…` (helper), rebuilt from `cc6beed`. Init helper `8c02b98f…` unchanged |
| Phase 1 facts | [facts/](facts/), collected read-only 2026-11-14 21:24:28Z; they supersede the 20261114T160408Z set. **f3:** boot `1794574308` (unchanged since zq3-r8), **`fdescfs.ko` (id 26) and `fusefs.ko` (id 27) loaded**, no fuse/fdesc mount, free 12.55 GB + inactive 1.20 GB, ARC 1.57 GB, `/usr/bin/ssh` `6a9e5573…`, python3.12 `a66eb370…`, `10.20.0.133` on `vm-public` (members `tap0`, `re0`). **Rocky:** PID `56842` (UTC lstart `Sat Nov 14 16:55:47 2026`, started by zq3-r9's Phase 12), boot ID `00e96c71-…`, stock `5.14.0-687.66.1.el9_8`, `next_entry` empty, `boot_indeterminate=7`, SELinux Enforcing, `/.autorelabel` absent, no `hfs-xz3` file in `/boot` (zq3-r9's U2), python3.9 `1143733d…`, `authorized_keys` `e08542dc…`. The files hold no key material (grep for key headers and `ssh-*` lines) |
| Host events since zq3-r9 | Only zq3-r9's own Rocky stop (16:51:34) and normal start (16:55:47 UTC, no relabel reboot). No f3 reboot since Paul's deliberate 2026-11-13 reboot |
| Launch facts | `f3-launch-facts-20261114T212428Z.json` (`f43fd124…`): `rocky.conf` `c101adf1…`, vm-run `43753bc8…`, firmware `6b48e967…` unchanged; **UEFI vars `09f21e31…`** (zq3-r9: `6de05da2…`); launch logged `2026-11-14T18:55:47+02:00` |
| Tap | `tap100` (bridge members `re0`, `tap0`) |
| Run ID / staging | `zq3-r10`, `/var/tmp/hfs-validation-xz3/zq3-r10` (the parent exists and holds zq3-r1 to zq3-r9) |
| Native guard root | `/var/tmp/hfs-20261114-zq3r10-fdq`, host profile `--rocky-pid 56842 --rocky-birth 'Sat Nov 14 16:55:47 2026'`, guard `576d802a…` (unchanged) |
| Window directory | `/var/tmp/hfs-fdescfs-windows/zq3-r10` (the parent exists, `root:wheel 0700`) |
| Rocky names | `/root/hfs-zq3-zq3-r10-*`, `/var/tmp/hfs-zq3-zq3-r10` (the zq3-r1 to zq3-r9 ones stay as evidence) |
| Operator public key | `2f6d909c…`; key path `…/hfs-zq3-r10-fill/operator/operator-signing-key` (empty `0700` directory, never read here) |

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | **Rebuilt** at `5a95ad4`: `hfs-runtime-prep` and the signer `hfs-retained9p-operator`. The build used `-trimpath`, CGO, VCS stamps and an unmodified private clone (`vcs.modified=false`); a second build of each, forced with `-a`, gave the same bytes. Reason: `git diff dcf2ed4 5a95ad4` over the `go list -deps` directories of both commands lists non-test changes in `internal/controlledruntime` (3 files), `controlledvalidation` (`execution.go`, `query_continuation.go` added, `cleanup_continuation.go` removed) and `viewcontrol` (`controller.go`, `tickets.go`, `lease_holders.go` added). `go.mod`/`go.sum` are unchanged. zq3-r9's builds (prep `14c2ffca…`, signer `efcf6fbf…`) are retired. G3 init helper: zq3-r1's reproduced build | prep `b522b6cb…`, signer `ac8e32c4…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from `testdata/zq3-spec.json` at `5a95ad4` (`make-spec.py`: only `zq3-r1` → `zq3-r10` and the r10 operator key path change; the same method reproduces zq3-r9's spec from `dcf2ed4` byte for byte). The committed spec is unchanged since r9; the sizing and guest budget are the same. `generate` checked the one character longer lifecycle socket path against the 103-byte `sockaddr_un` limit | spec `c9a15881…`, request template `b8766143…`, runtime config `a551e3f5…`, volume-init `b0af6218…`; volume `e9294bd8-…`; sessions hfs0 `5c69f97f-…`, hfs1 `8f7f94c7-…` |
| 1. Argv | `argv.py generate` from the 21:24Z launch facts (sessions, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `c889b417…`, args `5cc5e679…`, vars `09f21e31…` |
| 2. Kernel plan | `rocky_kernel.py plan` at main (`f32cf7b8…`; admits only `4712e479…`), stock default `/boot/vmlinuz-5.14.0-687.66.1.el9_8.x86_64`. Against zq3-r9's it differs in `run_id`, `manifest_sha256`, the bzImage (hash, size, build ID) and System.map; config and modules are the same. The l84 package `e585206a…` is refused as superseded ("use the r84 rebuild") | plan `dd6d0d88…`, build ID `d73c85da…` |
| 3. Controller batch | Template derived from zq3-r9's by `derive-templates.py`: run ID, fill path, sessions, candidate `e585206a…` → `4712e479…`. **T5 stays deferred** with the r9 reason (unchanged). The four other requests keep canonical order. `controller.py` `53289118…`. `check` refuses the template as designed (`probe/installed-manifest pin`). A synthetic copy with zero IDs **passes** and prints `cases_planned` (four) and the deferral. The same copy is **refused** with the l84 candidate (superseded), without the declaration ("five-case syscall batch minus declared deferrals") and with `credentials` also deferred ("case may not be deferred") | template `877b12bf…` |
| 4. Bootstrap plan | Template derived from zq3-r9's (the one that ran). New pins: bundle, kernel plan, `rocky_kernel.py`, the r84 daemon `34a5360b…` and the rebuilt prep `b522b6cb…`. Unchanged: `bootstrap_remote.py` `0dcb0fe0…` (`remote_sha256`), the guard pin `fece96b3…`. The script re-hashes every pinned file at main and in the package. The unfilled template refuses as designed (`observer.ssh_sha256: SHA256 hex`). **F1 filled now** with `fill_plan.py bootstrap` (`893350cf…`) from the 21:24Z `sha256 -r /usr/bin/ssh` line (`6a9e5573…`, unchanged), then `bootstrap.py check` (`5e467fe6…`, journal `/3`) **pass** | template `9302fbb6…`, filled plan `12cbf0b6…` |
| 5. Guard argv | Derived from zq3-r9's, with `--runtime-sha256 34a5360b…` (checked against the package) and `--disposable-validation` exactly once, last. It equals `bootstrap.py`'s own `SshRemote.guard_argv()` for the filled plan, in shell string and argv, and passes `runtime-guard.py`'s `parse_args` and `check_command` | in [pins](pins.json) |
| 6. Observer and staging pins | Unchanged script pins: `observer_key.py` `cd7f38c2…`, `observer.py` `2e1449dd…`, `installed.py` `c824b183…`, native guard `576d802a…`, `runtime-guard.py` `fece96b3…`, `fdescfs-window.py` `b00b19d1…`, `host.py` `738e1ca5…`, `host_snapshot.py` `b29ffed2…`, `guest-lock-probe.py` `630a737d…`, `pod_fixture.py` `ab4d269b…`, `argv.py` `ed63ff97…`. Re-pinned to main: `rocky_kernel.py` `f32cf7b8…`. All 17 f3 and 18 Rocky files went into two deterministic tarballs; a second tar run gave the same hashes. A verification extract matched every expected hash (35) and the f3 modes (owner `0/0`, no parent entry) and was then deleted | f3 tar `7924a5e4…`, Rocky tar `a8a9d1a0…` |
| 7. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` at main (`06444349…`, as r84 left it) with `zq3-r1` → `zq3-r10`; `check_pins` passes. Synthetic fills, which are not pins: zq3-r8's real Phase 4 facts and host hashes (modules absent) **pass**, with paths renamed and the three artifact hashes swapped for the r84 ones. The same facts with the l84 artifact hashes zq3-r9 staged **refuse** (`pinned artifact differs or missing`). Post-restore facts with both modules loaded, the state on f3 now, **refuse** (`fdescfs module present`): hence Phase 1a | r10 pins `a4553771…` |
| 8. Signing watcher | zq3-r9's `bin/sign_watch.py` (it signed both requests in zq3-r9) with `D`, the docstring run ID, the signer comment and **the signer pin `ac8e32c4…`** changed. Tested locally with a fake signer: a wrong request hash, a wrong command, an unknown action, a repeat and a wrong signer hash were refused; both exact requests were signed | `153be7f5…` |
| 9. Process observer check | `sh/p4-observer.py` from zq3-r9 with the run ID and **the daemon pin `34a5360b…`** changed (Phase 4 step 1a, **required**: new daemon bytes). zq3-r9's real records (renamed) are refused only for the l84 `sha256` and pass with `34a5360b…` substituted. The `$NG` line parses and passes the native guard's own `parse_args` and `check_request` | `1ce3fd57…` |
| 10. Run sheet | [run-sheet.md](run-sheet.md): Phases 0-12, 1a (**runs**), 4/1a and U1-U3, with the SELinux gates in Phases 1, 9 and 12, and a run log template. `session.sh` (`8fe8f74a…`) and the same twelve pinned helper scripts, renamed; `p10-requests.py` re-pins the three r10 request templates. The Phase 8-10 lines were dry-run end to end (below) | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-14T00:00:00Z --format=gnu`.
The f3 tarball holds only `hfs-20261114-zq3r10-fdq` and
`hfs-validation-xz3/zq3-r10`. It has **no entry for the existing parent**
`hfs-validation-xz3/`, so extraction never rewrites the parent's mode or
owner. All directories are `0700`, binaries `0500`, other files `0400`,
owner `0/0`.

## Signing

Paul delegated both signatures (A7) to Claude. In Phase 8 the driver's
stdout goes through `bin/sign_watch.py` (`153be7f5…`). The watcher runs the
signer (`ac8e32c4…`) only if:
- the printed command is exactly one of the two below;
- the request file's SHA256 equals the printed `request_sha256`;
- each command runs at most once.

Anything else is refused, and the driver then times out (fails closed):

```
/home/paul/tmp/claude/hfs-zq3-r10-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r10-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r10-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r10-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r10-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r10-fill/bootstrap-run/work/signer-binding
```

On Paul's instruction, Claude places the operator key at the bundle's path
before the run. Nothing in the fill reads it.

## Run-time fills

| Fill | When and source | Used by |
|---|---|---|
| D1 | **D1-a, Phase 1a**: both modules are loaded (21:24Z); Phase 1 confirms it, Phase 1a unloads them normally (never `-f`), and a refused unload stops before staging | Phase 4 step 0 window plan fill (needs both absent) |
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 21:24Z fact); Phase 4 step 0 rechecks it (`F1-unchanged`), a change stops | bootstrap `observer.ssh_sha256` (plan `12cbf0b6…`) |
| F2 fdescfs window plan and its hash (`WP`) | Phase 4 step 0: controller `facts`, host hashes, `realpath` library closure, f3 `date -u +%s`, then `fdescfs-window-plan.py --pins` with the r10 pins | setup, qualify, restore (`W`); Phase 5 recheck |
| F3 pod rootfs manifest digest (`PM`) | Phase 3: `pod_fixture.py manifest` on Rocky (zq3-r1 to zq3-r9: `f7f69339…`); `skip` if A8 is declined | `build`, T7 request |
| F3a process observer record | Phase 4 step 1a: guard stage `process-observer`, checked by `p4-observer.py` | go/no-go before setup; **pass required** (new daemon bytes) |
| F4 observer key pair, enrolled line, `after_sha256` (`EA`) | Phase 4 step 5: `ssh-keygen` on f3, `observer_key.py enrol` | runtime observer, Phase 11 `remove --expect-sha256`, banner-timeout check |
| F5 BLS entry ID | Phase 3 install record | `select` (from the state directory) |
| F6 boot ID, provider nonce, admission manifest SHA256, `virtioN` names, proposal, controller, provision hash | Phase 8 admission and runtime records | enrollment and bind requests (bootstrap fills them), controller batch plan |
| F7 m0/m1 mount IDs | Phase 8 mount steps (mountinfo) | batch plan, then `p10-requests.py` for the T6/T7/T8 requests |
| F8 batch plan and its hash (`B`) | Phase 8, after READY and `usable`: `fill_plan.py batch` (prints `cases_planned` and `deferred`), then `check-retained9p.sh check` | every controller and correlator call |
| F9 runtime UUID (`U`), `L`, cut, epoch and lifecycle sequences | READY record and `summary.json` via `p9-ready.py` (`--runtime`, `--observe-last`); Phase 9 uses L+1 and the T1 baseline L+2, then each from `correlate.py command` (`obs`, ARM, RESTART); expected L+1 to L+14 with T5 deferred | Phase 9 OBSERVE, T1-T4, T9-T11 |
| F10 exporter child PID (`XP`) | guard `receipt.json` `exporter.pid` (Phase 11 step 0; the `--no-child` line if null) | `host.py cleanup --child-pid` |
| F11 Rocky PID and lstart | **fixed** from the 21:24Z facts (`56842`, `Sat Nov 14 16:55:47 2026` UTC); rechecked in Phases 1, 4 (also by every `$NG` stage) and 5 | native guard host profile, Phase 5 recheck, `p5-mem.txt` RSS |
| F12 UMA drain decision | Phase 5 step 1a: `p5-estimate.py` `drain_recommended` | Phase 5 step 1b (or once in Phase 6) |
| F13 T6/T7/T8 request files | Phase 8 after the batch check: `p10-requests.py` (exclusive, `0600`, in `$E`) | T6, T7, T8 |
| F14 guard PID | Phase 11, only on the runtime-still-serving path (`pgrep -U root`) | the one SIGTERM to the guard |
| F15 SELinux state | Phase 1 (`/.autorelabel` absent, Enforcing), Phase 9 (guest `getenforce` `Enforcing`, enforce `1`), Phase 12 (relabel record) | Phase 1 and Phase 9 gates; Phase 12 finding |
| F16 984 timing | Phases 9-10: `tf3` lines in `$E/obs-timing.tsv` (three `ssh-noop`, each timed OBSERVE), checked with a local `cat` after the Phase 9 OBSERVE; `p9-timing.py` after T4 and before Phase 11 | run log only; never a gate |

## Changes from the zq3-r9 sheet

All zq3-r7 to zq3-r9 rules are kept:
- **Phase 1a (D1-a): runs again**, as in zq3-r3 to zq3-r7 and zq3-r9: a
  normal `kldunload fusefs`, then `kldunload fdescfs`, never `-f`;
- Phase 4 step 1a, the process observer check: **required**, because the
  daemon bytes changed (`34a5360b…`);
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

Kept from the zq3-r5 to zq3-r9 executions and the review decisions:
- **Phase 11 step 9 is one plain line with literal paths**:
  `f3 "doas -n rm -- /var/tmp/hfs-validation-xz3/zq3-r10/observer/id_ed25519 /var/tmp/hfs-validation-xz3/zq3-r10/observer/id_ed25519.pub"`.
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
- From zq3-r9: journal `/3` and the b84 `usable` gate, OBSERVE evidence `/2`,
  the L sequence shift, `p9-ready.py`/`p9-observe.py`, the `tf3` timing,
  the tool-timeout rule and `printf %q` for `vars.env`.

New for zq3-r10:
- **Candidate `4712e479…`** in the kernel plan, batch template, bootstrap
  plan, guard argv, window pins, staging, Phase 3, Phase 4 step 1a and
  Phase 8 expectations.
- **A new "Execution notes from the zq3-r9 run" section** (R9-1 and what is
  kept).
- **Phase 8 expectations:** the `final` probe should now return on both
  roots and no guest `QUERY timeout` line should appear; the R9-1 pattern
  is named as a finding against o84/p84; the live owner snapshot is taken
  before and after any block line, as zq3-r9 did.
- **o84 contention:** each parked claim re-runs `Admit` (`c.mu.TryLock`) on
  every guest QUERY (~100 Hz). The sheet asks to record slow OBSERVEs or
  long-open claims (a run log line).
- **984 capture check:** a local `cat $E/obs-timing.tsv` right after the
  Phase 9 OBSERVE checks that the three `ssh-noop` lines and the first
  OBSERVE were captured (evidence only, never a stop). The run log template
  records the line count, the summary and the contention note.

## Expectations: what changed in the runtime since zq3-r9

zq3-r9's `usable` gate passed `first` and `baseline` (R8-1 fixed) and failed
at `final` (R9-1): each root's GETATTR was answered ADMITTED but its claim
stayed `RESERVED/UNPREPARED`, because the first admission was refused behind
a recall (most likely triggered by the guest's pressure pass releasing the
in-flight observation's lease) and every QUERY resume was refused. r84
changes:
- **o84:** a QUERY may resume any never-prepared claim;
- **p84 (guest kernel):** pressure never releases the lease of an
  observation in flight; the background pass holds, at most 5 s, while
  observations are in flight;
- **q84:** a shared DOMAIN lease is released per holder, and only the last
  holder's release recalls;
- **i84:** a test-only clock hook.

So the `usable` gate's final probe should pass (record it if not), Phase 9
should run, and the 984 timing (`tf3`) should produce its first samples.

## Phases 9-10 recheck

Since zq3-r9 only `controller.py` (`53289118…`, admits only `4712e479…`)
and `rocky_kernel.py` changed among the tools the sheet uses. `bootstrap.py`
(`5e467fe6…`), `bootstrap_remote.py` (`0dcb0fe0…`), `fill_plan.py`
(`893350cf…`), `correlate.py` (`9cd55f2c…`), `guest.py` (`c50bb849…`) and
`guest-lock-probe.py` (`630a737d…`) are unchanged. No CLI the sheet uses
changed shape.

**Sheet dry run** (`dryrun/` in the private fill; scratch output). A
harness extracted the 74 command lines of this sheet from Phase 8 "After
READY" to T8 (including the new `obs-timing.tsv` check), plus the Phase 7
`k04` baseline line and the Phase 11 984 summary line (76 in all), from
`run-sheet.md` itself and ran them in order. It used:
- the real `fill_plan.py batch`, `check-retained9p.sh check`,
  `correlate.py`, `session.sh` (`tf3`, `obs`, `sha`, `jf`) and the `sh/`
  helpers;
- `controller.py` with faked guest dispatch;
- stub `f3`/`rocky` that answer like the lifecycle client (evidence `/2`
  with a claim summary), `host_snapshot.py` and the guest;
- a synthetic READY run with journal `/3`, an `ok` `usable` result, L = 5
  and mountinfo rows with the exact g64 options.

Every line passed with the expected results:
- L = 5; Phase 9 at 6 (L+1); case pairs 7/8, 9/10, 11/12, 13/14; T9 15,
  ARM 16, post-CALL 17; T10 18, RESTART 19;
- the capture check printed three `ssh-noop` lines and the OBSERVE at 6;
- the batch fill and check printing `cases_planned` (four) and the deferral;
- T1-T4 correlated and released; `syscall_batch_success` True with the
  deferral in the state;
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
- `p9-observe.py` refused sequence 1, L+2, a READY_JSON without L, a fenced
  admission, a rotated cut, an incomplete OBSERVE, evidence `/1` and a wrong
  plan hash;
- `p9-timing.py` refused a malformed TSV and one without an OBSERVE line;
- `p10-argv.py` and `p10-requests.py` refused as in r9;
- `fill_plan.py batch` refused a synthetic run that stopped at `usable`
  with the b84 claims note (`bootstrap steps did not end ok:
  usable=failed`) and a `/2` journal; `p9-ready.py` refused the failed run
  and zq3-r9's real run directory (outcome `failed`).

## Decisions from the zq3-r10 run-sheet review round 1 (2026-11-15)

No must-fix. Applied: the illustrative `NG` line now shows Rocky PID
`56842` (the real `session.sh` already did); the tool-timeout rule now also
covers every `$CTL` controller line, the T6/T8 lock-probe lines, T7's
`pod_fixture.py run`, Phase 3's kernel install and pod build, and U3's
`start-rocky` (at least 300 s); a Phase 9 OBSERVE rerun writes
`observe-p9-2.json`. Kept: the dry run's `__pycache__` folders in the
snapshot are harmless (the sheet runs every tool with `-I -B`).

## Findings and open questions

- **Q1 T5 deferred.** The run cannot complete zq3 acceptance. T5 needs a
  re-plan and sizing first.
- **Q2 the r84 fixes are untested on f3.** The kernel `d73c85da…` and the
  daemon `34a5360b…` passed offline tests only. The main questions are
  Phase 4 step 1a, the `usable` final probe, and Phase 9 with T1 past
  admission.
- **Q3 o84 contention.** Each parked claim re-runs `Admit` on every guest
  QUERY (~100 Hz); nothing measured its cost on f3. Slow OBSERVEs or a claim
  open past the `claims` window would point at it.
- **Q4 memory.** At 21:24Z free was 12.55 GB, inactive 1.20 GB and ARC
  1.57 GB. Phase 5's estimate and the Phase 6 bar decide (zq3-r9 margin
  0.58 GB after the drain).
- **Q5 UEFI vars drift.** The record pins `09f21e31…`. Any Rocky boot
  before Phase 7 changes it, and `host.py setup` then refuses. Recheck right
  before the run.
- **Q6 Phase 1a.** zq3-r3 to zq3-r7 and zq3-r9 unloaded both modules without
  refusal. A refusal now stops before staging and needs a plan revision.
- **Q7 window pins file:** the r10 window pins live only in the private fill,
  with their hash recorded here.
- **Q8 T9 ARM refused, T10 FENCED step 2, the T11 receipt wait (5 minutes):**
  unchanged; T9-T11 have only been dry-run so far.

No committed tool refused real inputs. These intended refusals all occurred:
- the controller template;
- the unfilled bootstrap template;
- the superseded l84 `e585206a` candidate in a synthetic batch plan and in
  `rocky_kernel.py plan`;
- an undeclared four-case batch and a deferred `credentials`;
- the window filler on the l84 artifact hashes and on facts with both
  modules loaded.

## Local verification

At `5a95ad4` in the main checkout, all passing:
- retained9p: 357 tests;
- directlaunch: 26 tests;
- `tools/lab` top level: 245 tests (3 skipped);
- `go test -count=1 ./internal/runtimeprep`.

`shellcheck` is clean for `build-stage.sh` and `sh/*.sh` (as POSIX sh), and
for `session.sh` apart from SC2034/SC2086: its variables are used by the
sheet, and `$O` is split on purpose. No code was changed.
