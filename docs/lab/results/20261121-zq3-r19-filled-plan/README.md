# zq3-r19 filled run inputs (2026-11-21)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r18 fill](../20261121-zq3-r18-filled-plan/README.md) ·
[zq3-r18 record](../20261121-zq3-r18/README.md) ·
[r19 candidate](../../../../tools/integration/artifacts/evidence/xz3-20261121-r19/README.md)

These are the exact run inputs for `zq3-r19`, the nineteenth attempt of zq3.
They were generated on the workstation with the committed tools at `main`
`f2f6c44` (the r19 candidate merge, after vf4, wf4, yf4 and yg4). They mirror
the [zq3-r18 fill](../20261121-zq3-r18-filled-plan/README.md), which ran for
real on 2026-11-21: Phases 0 to 9 passed, and T1 admitted both mounts in
7.3 s for the first time. Then R18-1 ended T1: the 2f4 guest kernel's
`hfs_setattr` refused the `ATTR_KILL_*` bits of an `fchown` on a regular file
(`EOPNOTSUPP`); vf4 fixes it. Phase 11 was clean. The changes are:
- the run ID (`zq3-r19`) and the native guard root
  `/var/tmp/hfs-20261121-zq3r19-fdq` (fill date 2026-11-21);
- the r19 candidate `09acd09d…` with a **new vf4 guest kernel** (bzImage
  `e5add01e…`, GNU build ID `e3d474df…`, System.map `91e970f0…`):
  `hfs_setattr` accepts the kill bits, dentry ops are set once, and link(2)
  and the create `EEXIST` path `d_drop` before the re-lookup (which fixes a
  guest `BUG_ON` on every link(2) that T4 `namespace` would have hit);
- a **new wf4 CGO daemon** `bb4ad73c…` (an expected-target recall is
  DOMAIN-scoped when no peer holds a lease or an open reservation) and the
  window artifacts rebuilt with it (`408cdafd…`, `d2ca3ab6…`). **Phase 4
  step 1a is required for the new daemon bytes.** The exporter `2fd7e32f…`
  is reused byte for byte; the spec (`ControlTickets` 24), the config, the
  modules and the init helper are unchanged;
- `hfs-runtime-prep` and the signer rebuilt at `f2f6c44`, a regenerated
  bundle and Rocky kernel plan, and `rocky_kernel.py`, `controller.py` and
  **`guest.py` (yf4: every case syscall is a named `p.call`, so a failure
  names its syscall)** re-pinned;
- **the case-failure OBSERVE** in the yf4 direct form (`tf3 observe <next>
  …`), with the sequence rule in the line's comment;
- the 2026-11-21 15:14:44Z facts: Rocky PID `59685` (zq3-r18's Phase 12),
  boot ID `c1dc3905-234b-4f83-91a9-4cb5e87b729f` (copied from the facts
  file), UEFI vars `88bbe93f…` (new argv record), modules ids 35/36, dnf
  still #65;
- **three non-HomelabFS f3 VMs, iom1, iom2 and iom3,** now **Stopped by
  default** (Paul's decision): Phases 1 and 5 require all three `Stopped`,
  and the run never starts them (Phase 12 and every abort path only check);
- **f3 swap in use** (891,068 KiB at the facts, from an iormesh OOM incident
  at 14:20Z): a new **Phase 0b** clears it before Phase 1, **only with
  Paul's explicit approval for this run**; Phase 6 still requires swap
  unused;
- **operator-friendliness** (zq3-r18's slips): every code block has BEGIN
  and END marker lines, nothing refers to a sheet line number, the Phase 11
  step 7 `--child-pid` line comes first and the `--no-child` line is
  labelled "ONLY if the exporter is None", and the step 6 wait matches the
  exact line `child-gone` (`grep -qx`);
- **Phase 1a (D1-a) runs again:** zq3-r18 left `fdescfs.ko` and
  `fusefs.ko` loaded.

Nothing here used SSH, the network, the lab, a VM, a mount, an installation
or the operator private key. **No authority follows from these files:**
every class A step in the [run sheet](run-sheet.md) still needs Paul's
explicit final GO, including the Phase 0b swap clearing, the Phase 1a unload
and T7 (A8). **zq3 acceptance stays incomplete by design**, because T5 does
not run.

## N1: Rocky upgrades itself once a day

Rocky's clock is `Europe/Sofia (EET, +0200)`. The daily mode of
`unattended-upgrade-rocky.timer` runs `dnf upgrade` once per local day, at
00:55 EET = **22:55 UTC**, stamps `/var/lib/unattended-upgrade/last-daily`
and never reboots Rocky. The 2026-11-21 15:14:44Z facts
(`rocky-upgrade-…txt`): stamp `2026-11-21` (written 00:55:06 EET); the
newest dnf transaction is still #65 (2026-11-20 00:55, `fd-find` only),
and the hourly ticks since only logged the deferred reboot. The kernel,
python3.9 and the pod runtime are re-verified unchanged.
`sh/n1-gate.py phase1` expects #65. **The run must start after that day's
run has stamped and finish before the next one, 22:55 UTC on the run day
(2026-11-21 22:55 UTC for a run that day); the latest Phase 5 start is
21:25 UTC.** Phase 1 runs `n1-gate.py gate` early and Phase 5 step 0 runs it
again before Rocky is stopped. The gate computes the dates from its own
clock, so a run on a later day needs no edit; the in-run cutoff (no new
case after 22:15 UTC, guest poweroff before 22:45 UTC, the 22:35 UTC
fallback) applies on the run day.

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-zq3-r19-fill` (`0700`). This directory holds only
public hashes, paths, argv and the read-only facts. These stay private and
are never committed:
- the per-session keys;
- the runtime config, which holds host receipt private keys;
- the staging tarballs that contain it.

## Inputs

| Input | Value |
|---|---|
| Candidate | **r19 rebuild** (tasks vf4, wf4): manifest `09acd09d…` (194 files), package `/home/paul/tmp/claude/hfs-xz3-20261121-r19/package`, source checkout and repository baseline `e160370` (no `inputs/repository-changes.patch`). **New:** the guest kernel `6.1.190-hfs-xz3` (bzImage `e5add01e…`, build ID `e3d474df…`, System.map `91e970f0…`), the CGO daemon `bb4ad73c…` (GNU build ID `841fe14d…`), the window artifacts `408cdafd…`/`d2ca3ab6…`. **Same bytes:** the exporter `2fd7e32f…` (reused with the r17 native records), config `28fe4a8e…`, the five modules, `Module.symvers` and `modules.*`, the init helper `8c02b98f…`. **Lab tools re-pinned** (`git diff c9c11f4 f2f6c44 -- tools docs/lab/zq3-fdescfs-window internal/runtimeprep/testdata`): `controller.py` `d70620d3…` and `rocky_kernel.py` `220fc897…` (admit only `09acd09d…`), **`guest.py` `005d9592…`** (yf4: named syscalls; the batch `probe_sha256`), the window `pins.json`, `build-record.json` and the build script default. Unchanged: `bootstrap.py`, `correlate.py`, `guest-lock-probe.py` `fd439897…`, `pod_fixture.py` `07314543…`, `pod_workload.py` `c714370f…`, `argv.py` `78462c2e…`, `bootstrap_remote.py` `0dcb0fe0…`, `fill_plan.py` `893350cf…`, `runtime-guard.py` `fece96b3…`, native test guard `576d802a…` |
| Runtime spec | `internal/runtimeprep/testdata/zq3-spec.json` at `f2f6c44`, unchanged since r17 (`ControlTickets` 24); `make-spec.py` changes only the run ID and the operator key path (the output equals zq3-r18's spec with the run ID renamed) |
| Facts | 2026-11-21 15:14:44Z, read-only ([facts/](facts/)), copied from `/home/paul/tmp/claude/hfs-zq3-r19-facts/` |

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | **Rebuilt** at `f2f6c44`: `hfs-runtime-prep` and the signer `hfs-retained9p-operator`, with `-trimpath`, CGO, VCS stamps and an unmodified private clone (`vcs.modified=false`); a second build of each, forced with `-a`, gave the same bytes. Both link `internal/viewcontrol` (wf4), so zq3-r18's prep `fb6b4816…` and signer `f6d0b8f7…` no longer match the source. G3 init helper: zq3-r1's reproduced build | prep `6220ecae…`, signer `6b723f18…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from the committed spec (`make-spec.py`). `Views.ControlTickets` 24 | spec `ed4d4a43…`, request template `54b24234…`, runtime config `a5bc9fbb…`, volume-init `a363ef1c…`; volume `6f8f656e-…`; sessions hfs0 `dfe0356b-…`, hfs1 `712b271d-…` |
| 1. Argv | `argv.py generate` from the 15:14:44Z launch facts (sessions, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `093378b7…`, args `9b31a04e…`, vars `88bbe93f…`, exporter `2fd7e32f…` |
| 2. Kernel plan | `rocky_kernel.py plan` at main, stock default `/boot/vmlinuz-5.14.0-741.22.1.el9_9.x86_64`. Against zq3-r18's it differs in `run_id`, `manifest_sha256`, the bzImage and System.map entries and the build ID. The r18 package `b2a4f69e…` is refused as superseded ("use the r19 rebuild") | plan `792eadae…`, build ID `e3d474df…` |
| 3. Controller batch | Template derived from zq3-r18's by `derive-templates.py`: run ID, fill path, sessions, candidate `b2a4f69e…` → `09acd09d…`, **`probe_sha256` → guest.py `005d9592…` (yf4)**; be4 seconds 240/255 kept (checked); **T5 stays deferred** with the r9 reason. `check` refuses the template as designed (`probe/installed-manifest pin`). A synthetic copy with zero IDs **passes** (four cases and the deferral). Refused as designed: the r18 candidate (superseded), zq3-r14's seconds 120/60, plan 120 alone, plan 255 with requests 60, no declaration, `credentials` deferred | template `c92a58f6…`, synthetic `49130d3d…` |
| 4. Bootstrap plan | Template derived from zq3-r18's. New pins: bundle, kernel plan, `rocky_kernel.py`, the rebuilt prep and **the new daemon `bb4ad73c…`**. The unfilled template refuses as designed (`observer.ssh_sha256: SHA256 hex`). **F1 filled now** with `fill_plan.py bootstrap` from the 15:14Z `sha256 -r /usr/bin/ssh` line (`6a9e5573…`), then `bootstrap.py check` (journal `/3`) **pass** | template `04ecac9f…`, filled plan `33113699…` |
| 5. Guard argv | Derived from zq3-r18's, with **`--runtime-sha256 bb4ad73c…`** and `--disposable-validation` exactly once, last. It equals `bootstrap.py`'s own `SshRemote.guard_argv()` for the filled plan, in shell string and argv, and passes `runtime-guard.py`'s `parse_args` and `check_command` | `81673798…` |
| 6. Pod template | zq3-r18's `/2` template with the run ID renamed; NEVRAs and hashes match the 15:14Z pod-runtime facts (byte-identical to zq3-r18's facts); the committed `pod_fixture.py` accepts it and refuses the malformed variants. `pod_workload.py`/`pod_fixture.py` unchanged, so **expected rootfs manifest `34a71569…`** (an expectation, not a pin) | pod template `ad682e65…` |
| 7. Lock-probe templates | zq3-r18's with the run ID renamed (150/240); the filled requests pass `validate_request`, case 151 is refused | m0 `63daf7b5…`, m1-T8 `72ed6ed9…` |
| 8. Staging | `build-stage.sh`: 17 f3 and 18 Rocky files, two deterministic tarballs (a second tar run gave the same hashes); a verification extract matched all 35 expected hashes; the f3 tarball holds only `0/0` entries, modes `0700`/`0500`/`0400`, no parent entry; extract deleted. The path lists equal zq3-r18's with the run ID renamed. Against zq3-r18's: the daemon, two window artifacts, argv record, volume-init and runtime config (f3), and bzImage, System.map, plan and `rocky_kernel.py` (Rocky) changed | f3 tar `2693cd82…`, Rocky tar `dd291283…` |
| 9. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` at main with `zq3-r1` → `zq3-r19`. Synthetic fills (not pins): zq3-r8's real Phase 4 facts with the r19 hashes **pass**; the r18 or xc4 hashes **refuse** (`pinned artifact differs or missing`); facts with both modules loaded **refuse** (`fdescfs module present`): hence Phase 1a | r19 pins `4e73f869…` |
| 10. Signing watcher | zq3-r18's with `D`, the docstring and **the signer pin** changed; tested with a fake signer (wrong hash, command, action, repeat and signer hash refused; both exact requests signed) | `b278db65…` |
| 11. Process observer check | `sh/p4-observer.py` with **the daemon pin `bb4ad73c…`**; zq3-r18's real records (renamed, hash substituted) **pass**, unchanged they are refused for the `sha256` only; the `$NG` line (Rocky PID `59685`) passes the native guard's `parse_args` and `check_request` | `bda506b5…` |
| 12. N1 gate helper | `sh/n1-gate.py` (docstring only). Retested at fixed times on 2026-11-21: 13:30Z, 21:20Z and 21:25Z GO; 21:30Z, 22:56Z, 23:10Z STOP; the next day with the old Phase 1 STOP, fresh GO; a stale stamp STOP; 01:00Z GO; `phase1` true for the real 15:14Z facts (#65), false for #64 and #66 (14 cases) | `9a213135…` |
| 13. Fence record helper | `sh/p11-fence.py` (docstring only); zq3-r18's real Phase 11 and case-failure evidence give `fenced` false | `4d63e655…` |
| 14. Guard budget helper | `sh/guard-budget.py` (docstring only; be4 budgets unchanged). On **zq3-r18's real journal** (launch 11:50:58Z): its Phase 9 and T1 checks GO, each latest start GO and one second later STOP (all seven steps); malformed journals refused (`rc=2`); 22 cases | `188e70a2…` |
| 15. Recall helper | `sh/recalls.py` (docstring only; largest-total selection): synthetic OBSERVEs, pre-ce4 input (`history_found` false, exit 0), an existing output and a missing input (exit 2), and zq3-r18's real OBSERVEs (picks the seven-recall `obs-credentials-fail.json`) | `6d226c7e…` |
| 16. Run sheet | [run-sheet.md](run-sheet.md): Phases 0-12 with the kept rules, the zq3-r18 run notes, "New for zq3-r19", **Phase 0b**, the iom1-3 checks, the yf4 case-failure OBSERVE, BEGIN/END markers in all 49 code blocks, and a run log template. `session.sh` (`0fa43208…`, hpin over 19 files) and seventeen helpers; `p10-requests.py` (`4d813002…`) re-pins the three r19 request templates. The Phase 8-10 lines were dry-run end to end (below) | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-21T00:00:00Z --format=gnu`.
The f3 tarball holds only `hfs-20261121-zq3r19-fdq` and
`hfs-validation-xz3/zq3-r19`, with **no entry for the existing parent**.

## Signing

Paul delegated both signatures (A7) to Claude. In Phase 8 the driver's
stdout goes through `bin/sign_watch.py` (`b278db65…`). The watcher runs the
signer (`6b723f18…`) only if:
- the printed command is exactly one of the two below;
- the request file's SHA256 equals the printed `request_sha256`;
- each command runs at most once.

Anything else is refused, and the driver then times out (fails closed):

```
/home/paul/tmp/claude/hfs-zq3-r19-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r19-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r19-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r19-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r19-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r19-fill/bootstrap-run/work/signer-binding
```

On Paul's instruction, Claude places the operator key at the bundle's path
(`/home/paul/tmp/claude/hfs-zq3-r19-fill/operator/operator-signing-key`; the
empty `0700` directory exists) before the run. Nothing in the fill reads it.

## Run-time fills

| Fill | When and source | Used by |
|---|---|---|
| N1 | Phase 1: Rocky's time zone (`timedatectl`), the upgrade stamp and dnf history (`p1-upgrade.txt`, `n1-gate.py phase1`), the early gate (`p1-nightly-gate.txt`), boot ID, running/default kernel, `next_entry`, python3.9, `authorized_keys` and the pod runtime (`p1-pod-runtime.txt`, `pod-runtime-unchanged: True`); any drift stops for a refill. Phase 5 step 0: `n1-gate.py gate` | Phase 1 Rocky gate, Phase 3 install, Phase 5 go/no-go, T7 |
| D1 | **D1-a, Phase 1a**: both modules are loaded (15:14Z: ids 35 and 36); Phase 1 confirms it, Phase 1a unloads them normally (never `-f`), and a refused unload stops before staging | Phase 4 step 0 window plan fill (needs both absent) |
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 15:14Z fact); Phase 4 step 0 rechecks it (`F1-unchanged`), a change stops | bootstrap `observer.ssh_sha256` (plan `33113699…`) |
| F2 fdescfs window plan and its hash (`WP`) | Phase 4 step 0: controller `facts`, host hashes, `realpath` library closure, f3 `date -u +%s`, then `fdescfs-window-plan.py --pins` with the r19 pins | setup, qualify, restore (`W`); Phase 5 recheck |
| F3 pod rootfs manifest digest (`PM`) | Phase 3: `pod_fixture.py manifest` on Rocky (zq3-r12 to zq3-r14: `523485cc…`; zq3-r15: `34a71569…`, as predicted; **expect `34a71569…` again** (zq3-r16 to zq3-r18 got it too): nothing in the rootfs changed; any other digest is explained before `build`); `skip` only if A8 is declined | `build`, T7 request |
| F3a process observer record | Phase 4 step 1a: guard stage `process-observer`, checked by `p4-observer.py` | go/no-go before setup; **pass required** (the NEW wf4 daemon `bb4ad73c…`; zq3-r18 passed for the je4/ce4/e84 `086d7cde…`) |
| F4 observer key pair, enrolled line, `after_sha256` (`EA`) | Phase 4 step 5: `ssh-keygen` on f3, `observer_key.py enrol` | runtime observer, Phase 11 `remove --expect-sha256`, banner-timeout check |
| F5 BLS entry ID | Phase 3 install record | `select` (from the state directory) |
| F6 boot ID, provider nonce, admission manifest SHA256, `virtioN` names, proposal, controller, provision hash | Phase 8 admission and runtime records | enrollment and bind requests (bootstrap fills them), controller batch plan |
| F7 m0/m1 mount IDs | Phase 8 mount steps (mountinfo) | batch plan, then `p10-requests.py` for the T6/T7/T8 requests |
| F8 batch plan and its hash (`B`) | Phase 8, after READY and `usable`, at once: `fill_plan.py batch` (prints `cases_planned` and `deferred`), then `check-retained9p.sh check` | every controller and correlator call |
| F9 runtime UUID (`U`), `L`, cut, epoch and lifecycle sequences | READY record and `summary.json` via `p9-ready.py` (`--runtime`, `--observe-last`); Phase 9 uses L+1 and the T1 baseline L+2, then each from `correlate.py command` (`obs`, ARM, RESTART); expected L+1 to L+14 with T5 deferred | Phase 9 OBSERVE, T1-T4, T9-T11 |
| F10 exporter child PID (`XP`) | guard `receipt.json` `exporter.pid` (Phase 11 step 0; the `--no-child` line if null) | `host.py cleanup --child-pid` |
| F11 Rocky PID and lstart | **fixed** from the 15:14Z facts (`59685`, `Sat Nov 21 11:58:43 2026` UTC, zq3-r18's Phase 12; boot ID `c1dc3905-234b-4f83-91a9-4cb5e87b729f` copied from the facts file); rechecked in Phases 1, 4 (also by every `$NG` stage) and 5 | native guard host profile, Phase 5 recheck, `p5-mem.txt` RSS |
| F12 UMA drain decision | Phase 5 step 1a: `p5-estimate.py` `drain_recommended` | Phase 5 step 1b (or once in Phase 6) |
| F13 T6/T7/T8 request files | Phase 8 after the batch check: `p10-requests.py` (exclusive, `0600`, in `$E`); lock budgets 150/240 (be4); the T7 request is pod `/2` with `runtime_nevra` and the be4 fixture pin | T6, T7, T8 |
| F14 guard PID | Phase 11, only on the runtime-still-serving path (`pgrep -U root`) | the one SIGTERM to the guard |
| F15 SELinux state | Phase 1 (`/.autorelabel` absent, Enforcing), Phase 9 (guest `getenforce` `Enforcing`, enforce `1`), Phase 12 (relabel record) | Phase 1 and Phase 9 gates; Phase 12 finding |
| F16 984 timing | Phases 9-10: `tf3` lines in `$E/obs-timing.tsv` (three `ssh-noop`, each timed OBSERVE), checked with a local `cat` after the Phase 9 OBSERVE; `p9-timing.py` after T4 and before Phase 11 | run log only; never a gate |
| F17 admission-step timings | Phase 10: `p10-admission.py` over the four cases' `stdout.log` after T4 (`p10-admission-steps.jsonl`; `admission_ms`, the largest gap, `deadline_hit`, `etimedout`; with the owner snapshots, any recall during admission), or over a failed case's before Phase 11 | run log only; never a gate |
| F18 transport fence record | `p11-fence.py`: Phase 8 failure block (`p8-fence.json`), the case-failure block (`t-fail-fence.json`) and always Phase 11 step 1a (`p11-fence.json`) over `runtime.stderr`, the owner snapshots/`f3-evidence.tar` (`transport.db`) and every OBSERVE taken | run log; `fenced: true` is a finding, never a retry |
| F19 guard budget | Phases 9-10: `gbudget STEP` (`$E/*-budget.json`) from the journal's `launch` intent: launch time, guard end, remaining seconds, `decision`; **be4 worst-case step seconds** (Phase 10 table) | GO/STOP before Phase 9 and each budgeted step; a STOP goes to Phase 11 |
| F20 unmount timings | Phase 11 step 2 and T8: each `rockyt … umount` line prints `rc` and `seconds` (`T0`) | R13-1 check; run log |
| F21 swap clearing | **Phase 0b** (A, Paul's explicit approval for this run), iom1-3 Stopped: `p0b-swap-before.txt` (free pages × pagesize > swap used), `swapoff -a`, `swapon -a`, `p0b-swap-after.txt` (0 used); without approval the run stops before Phase 1a | Phase 1 swap line, Phase 6 swap-unused bar |
| F22 case-failure OBSERVE sequence | the case-failure block's `tf3 observe <next>` line (yf4 direct form): L+3 after a T1 that failed after its baseline, one higher if its retained OBSERVE was taken, for T2-T4 one above the last accepted sequence | `obs-$CASE-fail.json`, `p11-fence.py`, `recalls.py` |

## Changes from the zq3-r18 sheet

All zq3-r7 to zq3-r18 rules are kept and written into the lines they name
(Phase 1a; Phase 4 step 1a, now for `bb4ad73c…`; the UMA drain decision;
`rockyt` bounded guest unmounts; `pgrep -U root`; the T10 abort paths to
T8; the explicit `--no-child` path for a null exporter; observer key
removal on every abort; the pause rule; the single host-preflight retry; the
workstation `/usr/bin/ssh` recheck `16cb2c6a…`; the Phase 9-10 helpers; the
plain literal f3 key deletion line; `restoration-summary.json`; the rc
capture after `rockyt`; Phase 11 step 4a/4b; the SELinux gates; the h64
errata; the z64 T5 deferral; the persisted variables; the m64 block-line
rule; journal `/3`, evidence `/2`, the L shift, the 984 timing; the N1 gate,
the in-run cutoff, `p11-fence.py`, `rockywait`; bash only; the be4 bounds,
the guard budget and the tool-timeout comments; `recalls.py` and its
largest-total selection). New for zq3-r19:
- **Candidate `09acd09d…`** in the kernel plan, batch template (and the
  yf4 `guest.py` pin), bootstrap plan (prep, kernel plan and daemon pins),
  guard argv, staging and the Phase 3, 4 and 8 expectations; **Phase 4 step
  1a:** `p4-observer.py` pins `bb4ad73c…`.
- **"Execution notes from the zq3-r18 run"** (R18-1, the recalls, the
  refused failure OBSERVE, the boot-ID typo, the operator slips) and **"New
  for zq3-r19"**.
- **Case-failure block:** the `obs obs-$CASE-baseline obs-$CASE-fail …`
  line is replaced by the yf4 direct form `tf3 observe <next> "doas -n $C
  --operation observe --runtime $U --sequence <next>" >
  $E/obs-$CASE-fail.json`, whose comment gives the sequence rule (L+3 after
  a T1 that failed after its baseline, one higher if its retained OBSERVE
  was taken, for later cases one above the last accepted sequence from the
  table); the block now sets `CASE` on its own line; `recalls.py` follows.
  Dry-run in the stub (sequence 17 OBSERVED; `recalls.py` read it).
- **iom1-3:** Phases 1 and 5 step 1 require all three `Stopped` (stop one on
  its own line if it runs, never force); Phase 12 and Recovery only check
  that they are still `Stopped`, never start them, and Claude messages the
  iormesh session after the run.
- **Phase 0b (new):** the pre-run swap clearing, only with Paul's explicit
  approval for this run and iom1-3 Stopped; Phase 1 expects 0 used; Phase 6
  keeps swap unused; the r18 side effect (laundry; the drain and Phase 6
  decide) is written next to the lines.
- **Operator-friendliness:** BEGIN/END marker lines in every code block, no
  sheet line numbers, prose outside code blocks, the Phase 11 step 7 order
  and labels, the step 6 `grep -qx child-gone` loop.
- **Findings:** any `EOPNOTSUPP` from a probe syscall, any guest
  `WARNING`/`BUG` line and the named failing syscall (yf4) join the
  `EAGAIN`/`ESTALE`/`EKEYREJECTED`/`ETIMEDOUT` rule.

## Expectations: what changed since zq3-r18

zq3-r18 passed everything through Phase 9 and admitted both mounts in T1 in
7.3 s, then failed on R18-1. With **vf4** `fchown` of a regular file should
pass, and link(2) no longer hits the `d_splice_alias` `BUG_ON` (T4
`namespace`). With **wf4** about three of the four post-mutation
expected-target recalls should be `DOMAIN`-scoped (about 6 s each instead of
7.5-13 s). **T1 should now pass.** The remaining risk is the recall cost per
guest operation after a mutation against T1's 255 s dispatch bound
(**xf4**, open). Then **T2-T4, T6, T7, T9, T10, T11 and T8** within the guard
budgets (none of T2-T11 has run for real); T5 stays deferred. Record
per-case admission timings, the recalls (`recalls.py`), the named failing
syscall on any failure, and any `EAGAIN`, `ESTALE`, `EKEYREJECTED`,
`ETIMEDOUT` or `EOPNOTSUPP` as findings. Known risks, not fixed: **0a4**,
**ue4**, **xf4**; **tg4** (`ATTR_TOUCH`, not used by zq3).

**Guard budget.** Unchanged: the be4 worst cases (5025 s) do not fit the
3600 s guard, so a slow run STOPs early and records the rest unrun. Latest
starts after the launch: Phase 9 52:00, T1-T4 44:00, T6 50:00, T7 44:45, T9
(with T10) 38:30, T10 48:30, T8 46:00.

## Phases 9-10 recheck

Since zq3-r18, among the tools the sheet uses, `controller.py` and
`rocky_kernel.py` changed (they admit only `09acd09d…`) and `guest.py`
changed (yf4; the controller sends it, pinned as `probe_sha256`). No CLI the
sheet uses changed shape.

**Sheet dry run** (`dryrun/` in the private fill). The zq3-r18 harness,
renamed, extracted 91 command lines of this sheet from Phase 8 "After
READY" to T8, plus the Phase 7 `k04` baseline and the two Phase 11 evidence
lines, and ran them in order under bash with the real `fill_plan.py batch`,
`check-retained9p.sh check`, `correlate.py`, `session.sh` and the `sh/`
helpers, `controller.py` (with the yf4 `guest.py` pin) with faked guest
dispatch, stub `f3`/`rocky` and a synthetic READY run with L = 2. Every
line passed (`HARNESS-DONE lines=91 L=2`). The case-failure block is
skipped by the harness (it names `$CASE`); its new direct OBSERVE line was
run separately against the same stub (sequence 17, `OBSERVED`), and
`recalls.py` read its output.

**Negative and real-record checks** (`helpers-dryrun.py`: 38 cases, all as
expected). Also: `p9-ready.py` refused zq3-r18's real READY run (its
control socket is not the r19 path); `fill_plan.py batch` refused zq3-r18's
real run for the r19 plan pin; `p10-admission.py` on zq3-r18's real T1 log
reports `admission_ms` 7274, `deadline_hit` false and the failure `OSError`
errno 95 (R18-1).

## Findings and open questions

- **Q0 N1.** Start after the run day's upgrade has stamped and finish
  before 22:55 UTC; latest Phase 5 start 21:25 UTC, no new case after
  22:15 UTC, guest off before 22:45 UTC. The gate computes the dates.
- **Q1 T5 deferred.** The run cannot complete zq3 acceptance.
- **Q2 the guard budget.** Worst cases 5025 s against 3600 s; T9 with T10
  must start by launch + 38:30.
- **Q3 the vf4 kernel and the wf4 daemon run for the first time** (builds,
  clean repros and offline admission only).
- **Q4 T2-T4, T6, T7, T9-T11 first real runs** with the be4 deadlines; T4
  exercises link(2) on the fixed kernel.
- **Q5 the pod rootfs manifest.** Expected `34a71569…`.
- **Q6 memory.** At 15:14Z free 10.19 GB, inactive 0.12 GB, ARC 3.14 GB,
  swap 0.91 GB used. Phase 0b's paging-in lands in laundry (zq3-r18:
  `active_based` margin −1.25 GB, drain 0.53 GB, Phase 6 margin 0.26 GB);
  the drain and the Phase 6 bar decide; iom1-3 must stay stopped.
- **Q7 UEFI vars drift.** The record pins `88bbe93f…`; any Rocky boot
  before Phase 7 changes it, and `host.py setup` then refuses.
- **Q8 Phase 1a.** Every D1-a so far unloaded both modules; a refusal stops
  before staging.
- **Q9 window pins file:** the r19 window pins live only in the private
  fill, with their hash recorded here.
- **Q10 the tool timeouts.** At least 300 s for `$CTL` and lock-probe lines,
  420 s for T7's run line, 1200 s for the Phase 11 step 6 loop.
- **Q11 iom1-3.** Stopped by default (Paul's decision); the run never starts
  them; Claude messages the iormesh session after the run.
- **Q12 swap (needs a decision).** Phase 0b needs **Paul's explicit approval
  for this run**; without it the run stops before Phase 1a.
- **Q13 guest.py.** The brief named yf4's `guest.py` as `5ad4191b…` or later;
  main has `005d9592…` (yf4's review round 1, `5ed6176`, changed it again).
  The controller check and the dry run accept it.

## Local verification

At `f2f6c44` (a clean worktree of main), all passing:
- retained9p: 395 tests;
- directlaunch: 26 tests;
- `tools/lab` top level: 249 tests (3 skipped);
- `go test ./internal/runtimeprep`.

`bash -n` is clean for `build-stage.sh` and `session.sh`; `session.sh`
loads only in bash and its `hpin` passes. No code was changed.

## Review round 1

A fresh-context review found nothing blocking (all pins, tarball members,
executable lines, Phase 0b, the sequence rule and the step 6/7 changes
checked; retained9p 395 OK). Applied to the run sheet:
- the case-failure OBSERVE takes its sequence from a separate
  `N=$((L+3))` line (persisted to `vars.env`, edited for other cases)
  instead of a literal `<next>` placeholder;
- the T1 `recalls.py` line says it runs only after a passed T1 (the
  case-failure block runs it after a failure, avoiding an output collision);
- Phase 0b stops and asks Paul if any iom VM runs or `vm list` fails.
Not fixed (prose only, no tool reads it): stale wording in `pins.json`
(`template_derived_from`, the tools note on guest.py, the kernel-plan
r17 references, the daemon described as je4/ce4/e84); the run record
corrects it.
