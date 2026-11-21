# zq3-r18 filled run inputs (2026-11-21)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r17 fill](../20261120-zq3-r17-filled-plan/README.md) ·
[zq3-r17 record](../20261120-zq3-r17/README.md) ·
[r18 candidate](../../../../tools/integration/artifacts/evidence/xz3-20261121-r18/README.md)

These are the exact run inputs for `zq3-r18`, the eighteenth attempt of zq3.
They were generated on the workstation with the committed tools at `main`
`c9c11f4` (the r18 candidate merge plus 6f4). They mirror the
[zq3-r17 fill](../20261120-zq3-r17-filled-plan/README.md), which ran for
real: Phases 0 to 9 passed (Phase 1a, Phase 4 step 1a for the qe4/te4 daemon
`85900893…`, the predicted pod manifest `34a71569…`, both mounts, READY,
`usable` L = 2, Phase 9). In T1, m0 was admitted and m1's root OPEN
succeeded, but m1's root `fstat` failed with `EKEYREJECTED` (R17-1): a
correct GRANTED VIEW_QUERY reply, delayed by a recall, reached the guest
before its REOPENED ACK had raised the fence, and the guest blocked the
session. Phase 11 took the guard-SIGTERM branch and was clean. The changes
are:
- the run ID (`zq3-r18`) and the native guard root
  `/var/tmp/hfs-20261121-zq3r18-fdq` (fill date 2026-11-21);
- the r18 candidate `b2a4f69e…` with a **new 2f4 guest kernel** (bzImage
  `f04c03e0…`, GNU build ID `020acb2e…`, System.map `8088881e…`): a
  verified-but-not-current GRANTED reply waits for the REOPENED-ACK fence
  raise and is re-queried; passed grants are decoded unadopted and only
  released;
- a **new je4/ce4/e84 CGO daemon** `086d7cde…` and the window artifacts
  rebuilt with it (`42def98f…`, `f84bf4cf…`). **Phase 4 step 1a is
  required for the new daemon bytes.** The exporter `2fd7e32f…` is reused
  byte for byte; the spec (`ControlTickets` 24), the config, the modules
  and the init helper are unchanged;
- `hfs-runtime-prep` and the signer rebuilt at `c9c11f4`, a regenerated
  bundle and Rocky kernel plan, and `rocky_kernel.py`, `controller.py`,
  `bootstrap.py` and `correlate.py` re-pinned;
- **ce4 recall timing:** a new helper `sh/recalls.py` records
  `controller.recalls` after T1 and in Phase 11 (findings, never gates);
- the 2026-11-21 08:54:42Z facts: Rocky PID `46196` (zq3-r17's Phase 12),
  UEFI vars `5ea958b3…` (new argv record), modules ids 34/35, dnf still #65;
- **two non-HomelabFS f3 VMs, iom1 and iom2,** stopped on Paul's
  instruction before the facts: Phases 1 and 5 require them `Stopped`, and
  Phase 12 starts them again;
- **f3 swap in use** (883,332 KiB at the facts): Phase 6 still requires
  swap unused before the launch (see Q12);
- the zq3-r17 lessons (R17-1; every 2f4/ie4/re4 log line and any `EAGAIN`,
  `ESTALE`, `EKEYREJECTED` or `ETIMEDOUT` from a probe syscall are
  findings);
- **Phase 1a (D1-a) runs again:** zq3-r17 left `fdescfs.ko` and
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
and never reboots Rocky. The 2026-11-21 08:54:42Z facts
(`rocky-upgrade-…txt`): stamp `2026-11-21` (written 00:55:06 EET); the
newest dnf transaction is still #65 (2026-11-20 00:55, `fd-find` only),
and the hourly ticks since only logged the deferred reboot. The kernel,
python3.9 and the pod runtime are re-verified unchanged.
`sh/n1-gate.py phase1` expects #65; any transaction newer
than #65 at Phase 1 means the pod-runtime and kernel rechecks decide and
drift means a refill, as before. **The run must start after that day's run
has stamped and finish before the next one, 22:55 UTC on the run day
(2026-11-21 22:55 UTC for a run that day); the latest Phase 5 start is
21:25 UTC.** Phase 1 runs `n1-gate.py gate` early and Phase 5 step 0 runs
it again before Rocky is stopped. The gate computes the dates from its own
clock, so a run on a later day needs no edit; the in-run cutoff (no new
case after 22:15 UTC, guest poweroff before 22:45 UTC, the 22:35 UTC
fallback) applies on the run day.

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-zq3-r18-fill` (`0700`). This directory holds only
public hashes, paths, argv and the read-only facts. These stay private and
are never committed:
- the per-session keys;
- the runtime config, which holds host receipt private keys;
- the staging tarballs that contain it.

## Inputs

| Input | Value |
|---|---|
| Candidate | **r18 rebuild** (tasks 2f4, je4, ce4, e84): manifest `b2a4f69e…` (194 files), package `/home/paul/tmp/claude/hfs-xz3-20261121-r18/package`, source checkout and repository baseline `633509e` (no `inputs/repository-changes.patch`). **New:** the guest kernel `6.1.190-hfs-xz3` (bzImage `f04c03e0…`, build ID `020acb2e…`, System.map `8088881e…`), the CGO daemon `086d7cde…` (GNU build ID `3fb0a610…`), the window artifacts `42def98f…`/`f84bf4cf…`. **Same bytes:** the exporter `2fd7e32f…` (reused with the r17 native records), config `28fe4a8e…`, the five modules and `modules.*`, the init helper `8c02b98f…`. **Lab tools re-pinned** (`git diff 58e04a9 c9c11f4 -- tools docs/lab/zq3-fdescfs-window internal/runtimeprep/testdata`): `controller.py` `5199e63d…` and `rocky_kernel.py` `d1f0ee0b…` (admit only `b2a4f69e…`), `bootstrap.py` `2c9982b4…` and `correlate.py` `f899d9de…` (one comment each: the optional ce4 field is diagnostics). Unchanged: `guest.py` `48d6ae81…`, `guest-lock-probe.py` `fd439897…`, `pod_fixture.py` `07314543…`, `pod_workload.py` `c714370f…`, `argv.py` `78462c2e…`, `bootstrap_remote.py` `0dcb0fe0…`, `fill_plan.py` `893350cf…`, `runtime-guard.py` `fece96b3…`, native test guard `576d802a…` |
| Runtime spec | `internal/runtimeprep/testdata/zq3-spec.json` at `c9c11f4`, unchanged since r17 (`ControlTickets` 24); `make-spec.py` changes only the run ID and the operator key path |
| Facts | 2026-11-21 08:54:42Z, read-only ([facts/](facts/)); the 08:29:38Z set, taken while iom1/iom2 ran, is superseded |

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | **Rebuilt** at `c9c11f4`: `hfs-runtime-prep` and the signer `hfs-retained9p-operator`, with `-trimpath`, CGO, VCS stamps and an unmodified private clone (`vcs.modified=false`); a second build of each, forced with `-a`, gave the same bytes. They link the je4/ce4/e84 packages, so zq3-r17's prep `6a726c13…` and signer `57e9606c…` no longer match the source. G3 init helper: zq3-r1's reproduced build | prep `fb6b4816…`, signer `f6d0b8f7…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from the committed spec (`make-spec.py`; equal to zq3-r17's spec with the run ID renamed). `Views.ControlTickets` 24 | spec `14002561…`, request template `19e33352…`, runtime config `09511c1c…`, volume-init `dc440da5…`; volume `52c8c108-…`; sessions hfs0 `558f85f5-…`, hfs1 `ada03325-…` |
| 1. Argv | `argv.py generate` from the 08:54:42Z launch facts (sessions, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `4a9a0828…`, args `d1030f8b…`, vars `5ea958b3…`, exporter `2fd7e32f…` |
| 2. Kernel plan | `rocky_kernel.py plan` at main (`d1f0ee0b…`), stock default `/boot/vmlinuz-5.14.0-741.22.1.el9_9.x86_64`. Against zq3-r17's it differs in `run_id`, `manifest_sha256`, the bzImage and System.map entries and the build ID. The r17 package `f614d52a…` is refused as superseded ("use the r18 rebuild") | plan `a5e9d6d0…`, build ID `020acb2e…` |
| 3. Controller batch | Template derived from zq3-r17's by `derive-templates.py`: run ID, fill path, sessions, candidate `f614d52a…` → `b2a4f69e…`; be4 seconds 240/255 kept (checked); **T5 stays deferred** with the r9 reason. `check` refuses the template as designed (`probe/installed-manifest pin`). A synthetic copy with zero IDs **passes** (`b185d9b8…`, four cases and the deferral). Refused as designed: the r17 candidate (superseded), zq3-r14's seconds 120/60, plan 120 alone, plan 255 with requests 60, no declaration, `credentials` deferred | template `a6f6f41c…` |
| 4. Bootstrap plan | Template derived from zq3-r17's. New pins: bundle, kernel plan, `rocky_kernel.py`, the rebuilt prep and **the new daemon `086d7cde…`**. The unfilled template refuses as designed (`observer.ssh_sha256: SHA256 hex`). **F1 filled now** with `fill_plan.py bootstrap` from the 08:54Z `sha256 -r /usr/bin/ssh` line (`6a9e5573…`), then `bootstrap.py check` (journal `/3`) **pass** | template `ab2dd4b8…`, filled plan `11a8d70e…` |
| 5. Guard argv | Derived from zq3-r17's, with **`--runtime-sha256 086d7cde…`** and `--disposable-validation` exactly once, last. It equals `bootstrap.py`'s own `SshRemote.guard_argv()` for the filled plan, in shell string and argv, and passes `runtime-guard.py`'s `parse_args` and `check_command` | `b680bb98…` |
| 6. Pod template | zq3-r17's `/2` template with the run ID renamed; NEVRAs and hashes match the 08:54Z pod-runtime facts (byte-identical to zq3-r17's); the committed `pod_fixture.py` accepts it and refuses the malformed variants. **Expected rootfs manifest `34a71569…`** (an expectation, not a pin) | pod template `f643387e…` |
| 7. Lock-probe templates | zq3-r17's with the run ID renamed (150/240); the filled requests pass `validate_request`, case 151 is refused | m0 `3393c67d…`, m1-T8 `aeeabf66…` |
| 8. Staging | `build-stage.sh`: 17 f3 and 18 Rocky files, two deterministic tarballs (a second tar run gave the same hashes); a verification extract matched all 35 expected hashes; the f3 tarball holds only `0/0` entries, modes `0700`/`0500`/`0400`, no parent entry; extract deleted. Against zq3-r17's lists: the daemon, two window artifacts, argv record, volume-init and runtime config (f3), and bzImage, System.map, plan and `rocky_kernel.py` (Rocky) changed | f3 tar `80e64807…`, Rocky tar `3d2b3fa1…` |
| 9. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` at main (`6def5a31…`) with `zq3-r1` → `zq3-r18`. Synthetic fills (not pins): zq3-r8's real Phase 4 facts with the r18 hashes **pass**; the r17 or xc4 hashes **refuse** (`pinned artifact differs or missing`); facts with both modules loaded **refuse** (`fdescfs module present`): hence Phase 1a | r18 pins `73418327…` |
| 10. Signing watcher | zq3-r17's with `D`, the docstring and **the signer pin** changed; tested with a fake signer (wrong hash, command, action, repeat and signer hash refused; both exact requests signed) | `4da3282e…` |
| 11. Process observer check | `sh/p4-observer.py` with **the daemon pin `086d7cde…`**; zq3-r17's real records (renamed, hash substituted) **pass**, unchanged they are refused for the `sha256` only; the `$NG` line (Rocky PID `46196`) passes the native guard's `parse_args` and `check_request` | `0c98eb9d…` |
| 12. N1 gate helper | `sh/n1-gate.py` (docstring only). Retested at fixed times on 2026-11-21: 13:30Z, 21:20Z and 21:25Z GO; 21:30Z, 22:56Z, 23:10Z STOP; the next day with the old Phase 1 STOP, fresh GO; a stale stamp STOP; 01:00Z GO; `phase1` true for the real facts (#65), false for #64 and #66 | `3806a422…` |
| 13. Fence record helper | `sh/p11-fence.py` (docstring only); zq3-r17's real Phase 11 and case-failure evidence give `fenced` false | `ecea88bc…` |
| 14. Guard budget helper | `sh/guard-budget.py` (docstring only; be4 budgets unchanged). On **zq3-r17's real journal** (launch 15:34:25Z): its Phase 9 and T1 starts GO, each latest start GO and one second later STOP (all seven steps); malformed journals refused (`rc=2`) | `737af769…` |
| 15. Recall helper (new, ce4) | `sh/recalls.py`: per recall trigger, scope, outcome, phase durations and ACK waits from `controller.recalls`; tested on synthetic OBSERVEs (direct and JSON-string) and pre-ce4 inputs (`history_found` false, exit 0), an existing output and a missing input (exit 2), and zq3-r17's real `observe-p9.json` (no history) | `d5f614df…` |
| 16. Run sheet | [run-sheet.md](run-sheet.md): Phases 0-12 with the kept rules, the zq3-r17 notes, "New for zq3-r18", iom1/iom2 and swap lines, the recall lines and a run log template. `session.sh` (`d9623792…`, hpin over 19 files) and seventeen helpers; `p10-requests.py` (`72738e2b…`) re-pins the three r18 request templates. The Phase 8-10 lines were dry-run end to end (below) | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-21T00:00:00Z --format=gnu`.
The f3 tarball holds only `hfs-20261121-zq3r18-fdq` and
`hfs-validation-xz3/zq3-r18`, with **no entry for the existing parent**.
## Signing

Paul delegated both signatures (A7) to Claude. In Phase 8 the driver's
stdout goes through `bin/sign_watch.py` (`4da3282e…`). The watcher runs the
signer (`f6d0b8f7…`) only if:
- the printed command is exactly one of the two below;
- the request file's SHA256 equals the printed `request_sha256`;
- each command runs at most once.

Anything else is refused, and the driver then times out (fails closed):

```
/home/paul/tmp/claude/hfs-zq3-r18-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r18-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r18-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r18-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r18-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r18-fill/bootstrap-run/work/signer-binding
```

On Paul's instruction, Claude places the operator key at the bundle's path
before the run. Nothing in the fill reads it.

## Run-time fills

| Fill | When and source | Used by |
|---|---|---|
| N1 | Phase 1: Rocky's time zone (`timedatectl`), the upgrade stamp and dnf history (`p1-upgrade.txt`, `n1-gate.py phase1`), the early gate (`p1-nightly-gate.txt`), boot ID, running/default kernel, `next_entry`, python3.9, `authorized_keys` and the pod runtime (`p1-pod-runtime.txt`, `pod-runtime-unchanged: True`); any drift stops for a refill. Phase 5 step 0: `n1-gate.py gate` | Phase 1 Rocky gate, Phase 3 install, Phase 5 go/no-go, T7 |
| D1 | **D1-a, Phase 1a**: both modules are loaded (08:54Z: ids 34 and 35); Phase 1 confirms it, Phase 1a unloads them normally (never `-f`), and a refused unload stops before staging | Phase 4 step 0 window plan fill (needs both absent) |
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 08:54Z fact); Phase 4 step 0 rechecks it (`F1-unchanged`), a change stops | bootstrap `observer.ssh_sha256` (plan `11a8d70e…`) |
| F2 fdescfs window plan and its hash (`WP`) | Phase 4 step 0: controller `facts`, host hashes, `realpath` library closure, f3 `date -u +%s`, then `fdescfs-window-plan.py --pins` with the r18 pins | setup, qualify, restore (`W`); Phase 5 recheck |
| F3 pod rootfs manifest digest (`PM`) | Phase 3: `pod_fixture.py manifest` on Rocky (zq3-r12 to zq3-r14: `523485cc…`; zq3-r15: `34a71569…`, as predicted; **expect `34a71569…` again** (zq3-r16 and zq3-r17 got it too): nothing in the rootfs changed; any other digest is explained before `build`); `skip` only if A8 is declined | `build`, T7 request |
| F3a process observer record | Phase 4 step 1a: guard stage `process-observer`, checked by `p4-observer.py` | go/no-go before setup; **pass required** (the NEW je4/ce4/e84 daemon `086d7cde…`; zq3-r17 passed for the qe4/te4 `85900893…`) |
| F4 observer key pair, enrolled line, `after_sha256` (`EA`) | Phase 4 step 5: `ssh-keygen` on f3, `observer_key.py enrol` | runtime observer, Phase 11 `remove --expect-sha256`, banner-timeout check |
| F5 BLS entry ID | Phase 3 install record | `select` (from the state directory) |
| F6 boot ID, provider nonce, admission manifest SHA256, `virtioN` names, proposal, controller, provision hash | Phase 8 admission and runtime records | enrollment and bind requests (bootstrap fills them), controller batch plan |
| F7 m0/m1 mount IDs | Phase 8 mount steps (mountinfo) | batch plan, then `p10-requests.py` for the T6/T7/T8 requests |
| F8 batch plan and its hash (`B`) | Phase 8, after READY and `usable`, at once: `fill_plan.py batch` (prints `cases_planned` and `deferred`), then `check-retained9p.sh check` | every controller and correlator call |
| F9 runtime UUID (`U`), `L`, cut, epoch and lifecycle sequences | READY record and `summary.json` via `p9-ready.py` (`--runtime`, `--observe-last`); Phase 9 uses L+1 and the T1 baseline L+2, then each from `correlate.py command` (`obs`, ARM, RESTART); expected L+1 to L+14 with T5 deferred | Phase 9 OBSERVE, T1-T4, T9-T11 |
| F10 exporter child PID (`XP`) | guard `receipt.json` `exporter.pid` (Phase 11 step 0; the `--no-child` line if null) | `host.py cleanup --child-pid` |
| F11 Rocky PID and lstart | **fixed** from the 08:54Z facts (`46196`, `Fri Nov 20 15:37:52 2026` UTC, zq3-r17's Phase 12); rechecked in Phases 1, 4 (also by every `$NG` stage) and 5 | native guard host profile, Phase 5 recheck, `p5-mem.txt` RSS |
| F12 UMA drain decision | Phase 5 step 1a: `p5-estimate.py` `drain_recommended` | Phase 5 step 1b (or once in Phase 6) |
| F13 T6/T7/T8 request files | Phase 8 after the batch check: `p10-requests.py` (exclusive, `0600`, in `$E`); lock budgets 150/240 (be4); the T7 request is pod `/2` with `runtime_nevra` and the be4 fixture pin | T6, T7, T8 |
| F14 guard PID | Phase 11, only on the runtime-still-serving path (`pgrep -U root`) | the one SIGTERM to the guard |
| F15 SELinux state | Phase 1 (`/.autorelabel` absent, Enforcing), Phase 9 (guest `getenforce` `Enforcing`, enforce `1`), Phase 12 (relabel record) | Phase 1 and Phase 9 gates; Phase 12 finding |
| F16 984 timing | Phases 9-10: `tf3` lines in `$E/obs-timing.tsv` (three `ssh-noop`, each timed OBSERVE), checked with a local `cat` after the Phase 9 OBSERVE; `p9-timing.py` after T4 and before Phase 11 | run log only; never a gate |
| F17 admission-step timings | Phase 10: `p10-admission.py` over the four cases' `stdout.log` after T4 (`p10-admission-steps.jsonl`; `admission_ms`, the largest gap, `deadline_hit`, `etimedout`; with the owner snapshots, any recall during admission), or over a failed case's before Phase 11 | run log only; never a gate |
| F18 transport fence record | `p11-fence.py`: Phase 8 failure block (`p8-fence.json`), the case-failure block (`t-fail-fence.json`) and always Phase 11 step 1a (`p11-fence.json`) over `runtime.stderr`, the owner snapshots/`f3-evidence.tar` (`transport.db`) and every OBSERVE taken | run log; `fenced: true` is a finding, never a retry |
| F19 guard budget | Phases 9-10: `gbudget STEP` (`$E/*-budget.json`) from the journal's `launch` intent: launch time, guard end, remaining seconds, `decision`; **be4 worst-case step seconds** (Phase 10 table) | GO/STOP before Phase 9 and each budgeted step; a STOP goes to Phase 11 |
| F20 unmount timings | Phase 11 step 2 and T8: each `rockyt … umount` line prints `rc` and `seconds` (`T0`) | R13-1 check; run log |

## Changes from the zq3-r17 sheet

All zq3-r7 to zq3-r17 rules are kept and written into the lines they name:
- **Phase 1a (D1-a): runs again** (zq3-r17 left both modules loaded): a
  normal `kldunload fusefs`, then `kldunload fdescfs`, never `-f`;
- **Phase 4 step 1a, the process observer check: required**, now for the
  new je4/ce4/e84 daemon bytes `086d7cde…`;
- the UMA drain decision in Phase 5; `rockyt` bounded guest unmounts
  (270 s); `pgrep -U root -lf runtime-guard.py` for the guard SIGTERM; the
  T10 abort paths to T8; the explicit `--no-child` line for a null
  exporter; observer key removal on every abort after Phase 4 step 5; the
  pause rule; the single host-preflight retry; the Phase 0 recheck of the
  workstation `/usr/bin/ssh` (`16cb2c6a…`, unchanged at fill time); the
  Phase 9-10 helpers;
- from the zq3-r5 to zq3-r7 executions and reviews: Phase 11 step 9 is one
  plain line with literal paths,
  `f3 "doas -n rm -- /var/tmp/hfs-validation-xz3/zq3-r18/observer/id_ed25519 /var/tmp/hfs-validation-xz3/zq3-r18/observer/id_ed25519.pub"`,
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
- from zq3-r14 to zq3-r17: the be4 bounds (request `seconds` 240, plan
  255, T6 150/240, T7 workload 240 / run 315), the guard budget helper and
  its GO/STOP lines with the per-step latest start times, the tool-timeout
  comments on every `$CTL` (>= 300 s), lock-probe (>= 300 s) and T7 pod
  run (>= 420 s) line, the timed unmounts, the child-wait line with this
  run's pattern `[h]fs-zq3-r18`.

- **f3 swap and iom1/iom2 (new, from the 2026-11-21 facts):** Phase 1
  records swap instead of requiring it unused and requires iom1/iom2
  `Stopped`; Phase 5 step 1 rechecks both; Phase 6 keeps **swap unused**
  as a launch condition (runtime-guard.py admits no swap) with an explicit
  stop path; Phase 12 starts iom1 and iom2 again.

New for zq3-r18:
- **Candidate `b2a4f69e…`** in the kernel plan, batch template, bootstrap
  plan (prep, kernel plan and daemon pins), guard argv, staging and the
  Phase 3, 4 and 8 expectations; **Phase 4 step 1a:** `p4-observer.py`
  pins `086d7cde…`.
- **"Execution notes from the zq3-r17 run"** (R17-1, R16-1 gone, R13-1
  still fixed) and **"New for zq3-r18"** (2f4, je4, ce4, e84, iom1/iom2,
  swap, the expectations, the risks, the facts).
- **ce4:** `recalls.py` after T1 (pass or fail, also in the case-failure
  block) and in Phase 11 step 1b; run log line for recalls.
- **iom1/iom2:** Stopped in Phase 1 and Phase 5 step 1 (stop on its own
  line if running, never force); started again in Phase 12, each on its
  own line, then checked `Running`.
- **Swap:** recorded in Phases 1 and 5; required unused in Phase 6, with a
  stop path (U3) and a question for Paul if Rocky's stop does not free it.
- **Phase 10 and the run log:** every 2f4/ie4/re4 guest log line and any
  `EAGAIN`, `ESTALE`, `EKEYREJECTED` or `ETIMEDOUT` from a probe syscall is
  a finding, next to the admission timings and the recalls (R17-1 check).
## Expectations: what changed since zq3-r17

zq3-r17 passed everything through Phase 9 and failed T1 on R17-1. The R16-1
shortage was gone (no BLOCKED), m0 was admitted and m1's root OPEN
succeeded, but a recall-delayed GRANTED reply beat m1's fence raise and the
guest blocked the session (`EKEYREJECTED`). With **2f4** such a reply waits
for the fence raise and is re-queried, so **T1 should admit both mounts**.
With **je4** (no recall for the last holder's clean READ release) and
**e84** (pin-only mismatches recall only the requesting domain), admission
should be much faster than zq3-r17's 10-12 s per root. Then **T2-T4, T6,
T7, T9, T10, T11 and T8** within the guard budgets (none of T2-T11 has run
for real); T5 stays deferred. Record per-case admission timings, the
recalls (`recalls.py`: trigger, scope, phase durations, ACK waits), every
2f4/ie4/re4 guest log line and any `EAGAIN`, `ESTALE`, `EKEYREJECTED` or
`ETIMEDOUT` from a probe syscall as findings.

Remaining known risks, not fixed: **0a4** (the host never drops a
participant that stops acknowledging) and **ue4** (release tickets cannot
retire during a recall). A guest that blocks past its alarm can outlive the
controller's dispatch bound (error/uncertain, safe, a finding).

**Guard budget.** Unchanged: the be4 worst cases (5025 s) do not fit the
3600 s guard, so a slow run STOPs early and records the rest unrun. Latest
starts after the launch: Phase 9 52:00, T1-T4 44:00, T6 50:00, T7 44:45, T9
(with T10) 38:30, T10 48:30, T8 46:00.

## Phases 9-10 recheck

Since zq3-r17, among the tools the sheet uses, `controller.py` and
`rocky_kernel.py` changed (they admit only `b2a4f69e…`), and `bootstrap.py`
and `correlate.py` gained one comment each. No CLI the sheet uses changed
shape.

**Sheet dry run** (`dryrun/` in the private fill). The zq3-r17 harness,
renamed, extracted 88 command lines of this sheet from Phase 8 "After
READY" to T8, plus the Phase 7 `k04` baseline and the two Phase 11 evidence
lines (91 in all, one more than r17: T1's `recalls.py` line), and ran them
in order under bash with the real `fill_plan.py batch`, `check-retained9p.sh
check`, `correlate.py`, `session.sh` and the `sh/` helpers, `controller.py`
with faked guest dispatch, stub `f3`/`rocky` and a synthetic READY run.
Every line passed (`HARNESS-DONE lines=91 L=5`).

**Negative and real-record checks** (`helpers-dryrun.py`: 38 cases, all as
expected). Also: `p9-ready.py` refused zq3-r17's real READY run (its
control socket is not the r18 path); `fill_plan.py batch` refused zq3-r17's
real run for the r18 plan pin; `p10-admission.py` on zq3-r17's real T1 log
reports `deadline_hit` false and the failure `OSError` errno 129 (R17-1).

## Findings and open questions

- **Q0 N1.** Start after the run day's upgrade has stamped and finish
  before 22:55 UTC; latest Phase 5 start 21:25 UTC, no new case after
  22:15 UTC, guest off before 22:45 UTC. The gate computes the dates.
- **Q1 T5 deferred.** The run cannot complete zq3 acceptance.
- **Q2 the guard budget.** Worst cases 5025 s against 3600 s; T9 with T10
  must start by launch + 38:30.
- **Q3 the 2f4 kernel and the je4/ce4/e84 daemon run for the first time**
  (builds, clean repros and offline admission only). Phase 4 step 1a
  qualifies the daemon, Phase 3 installs the kernel, Phase 8 is their
  first real run.
- **Q4 T2-T4, T6, T7, T9-T11 first real runs** with the be4 deadlines.
- **Q5 the pod rootfs manifest.** Expected `34a71569…`.
- **Q6 memory.** At 08:54Z free 4.92 GB, inactive 0.22 GB, ARC 4.95 GB
  (zq3-r17's fill: 5.62, 2.12, 6.66 GB; zq3-r17's active-based margin was
  only 0.017 GB before the drain). Phase 5's estimate and the Phase 6 bar
  decide; iom1/iom2 must stay stopped.
- **Q7 UEFI vars drift.** The record pins `5ea958b3…`; any Rocky boot
  before Phase 7 changes it, and `host.py setup` then refuses.
- **Q8 Phase 1a.** Every D1-a so far unloaded both modules; a refusal stops
  before staging.
- **Q9 window pins file:** the r18 window pins live only in the private
  fill, with their hash recorded here.
- **Q10 the tool timeouts.** At least 300 s for `$CTL` and lock-probe lines,
  420 s for T7's run line.
- **Q11 iom1/iom2.** Stopped by Claude on Paul's instruction. Paul's
  iormesh session restarted them at 08:55:14Z; on 2026-11-21 Paul agreed to
  pause iormesh so Claude stops them again for this run. The sheet restarts
  them in Phase 12, and (review round 1) the Recovery section now restarts
  them on every abort path too.
- **Q12 swap (decided).** 883,332 KiB of swap was in use at the facts, while
  `runtime-guard.py` admits no swap use. Paul approved on 2026-11-21
  clearing it once iom1/iom2 are stopped: before Phase 1, Claude checks that
  free memory exceeds the swap in use and runs `doas -n swapoff -a` then
  `doas -n swapon -a` on f3, each on its own line, then records `swapinfo -k`
  showing 0 used. Phase 6 still requires swap unused.

## Review round 1

A fresh-context review found nothing blocking (all hashes, tarballs, tests
and AGENTS.md rules checked). Applied:
- the case-failure block takes one fresh `obs obs-$CASE-baseline
  obs-$CASE-fail` while the runtime still serves, so a failed case's ce4
  recall history is captured before Phase 11's SIGTERM drops it;
- `recalls.py` summarises the history with the largest `total` (a RESTARTED
  T10 starts an empty history that "newest" would have picked); `recalls.py`
  is now `d5f614df…` and `session.sh` `d9623792…`;
- iom1/iom2 restart on every abort path (Recovery);
- cosmetic: seventeen sh/* helpers, a duplicated README paragraph removed.

No committed tool refused real inputs. The intended refusals all occurred
(the controller template, the unfilled bootstrap template, the r17
candidate, zq3-r14's seconds, an undeclared batch, a deferred
`credentials`, the window filler on old hashes and loaded modules, the
malformed pod requests, a lock budget of 151 s, the old daemon hash in
`p4-observer.py`, the watcher negatives, `n1-gate.py` at STOP times,
`guard-budget.py` after each latest start, `p9-ready.py` and `fill_plan.py
batch` on zq3-r17's real run).

## Local verification

At `c9c11f4` (a clean worktree of main), all passing:
- retained9p: 391 tests;
- directlaunch: 26 tests;
- `tools/lab` top level: 249 tests (3 skipped);
- `go test ./internal/runtimeprep`.

`bash -n` is clean for `build-stage.sh` and `session.sh`; `session.sh`
loads only in bash and its `hpin` passes. No code was changed.
