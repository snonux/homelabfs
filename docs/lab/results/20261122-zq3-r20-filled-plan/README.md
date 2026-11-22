# zq3-r20 filled run inputs (2026-11-22)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r19 fill](../20261121-zq3-r19-filled-plan/README.md) ·
[zq3-r19 record](../20261121-zq3-r19/README.md) ·
[r20 candidate](../../../../tools/integration/artifacts/evidence/xz3-20261122-r20/README.md) ·
[xf4 design](../../../design/xf4-view-advance.md)

These are the exact run inputs for `zq3-r20`, the twentieth attempt of zq3.
They were generated on the workstation with the committed tools at `main`
`e2f406a` (after the r20 candidate merge and the later daemon/test-only
merges 2h4, 0a4, a84, l64, 094/2a4, qc4, fh4, nj4 and s54, which change no
candidate byte). They mirror the
[zq3-r19 fill](../20261121-zq3-r19-filled-plan/README.md), which ran for
real on 2026-11-21: Phases 0b to 9 passed, and T1's `fchown` worked for the
first time (R18-1 fixed). Then R19-1 ended T1: the guest's re4 back-off
counted a busy local retirement round against its BLOCKED retry cap, so the
access child's `open` got `EAGAIN`; 1h4 fixes it. Phase 11 was clean. The
changes are:
- the run ID (`zq3-r20`) and the native guard root
  `/var/tmp/hfs-20261122-zq3r20-fdq` (fill date 2026-11-22);
- the r20 candidate `b37bcb06…` (source `d71ae02`) with a **new 1h4 + 4h4
  guest kernel** (bzImage `2b8880a8…`, GNU build ID `b4d6cd69…`, System.map
  `c726e252…`): an expected-target BLOCKED is re-checked and a busy
  retirement round is waited for, not counted (R19-1); a cacheless view
  sends `VIEW_ACQUIRE` `"advance": true` and adopts the host's current
  target (xf4 step B);
- a **new 3h4 CGO daemon** `c04c519f…` (the host side of the view advance,
  xf4 step A) and the window artifacts rebuilt with it (`1b7e68d8…`,
  `edae7dc3…`). **Phase 4 step 1a is required for the new daemon bytes.**
  The exporter `2fd7e32f…` is reused byte for byte; the spec
  (`ControlTickets` 24), the config, the modules and the init helper are
  unchanged;
- `hfs-runtime-prep` and the signer rebuilt at `e2f406a`, a regenerated
  bundle and Rocky kernel plan, and `rocky_kernel.py`, `controller.py`,
  `bootstrap.py` and `correlate.py` re-pinned (`guest.py` unchanged);
- **the N1 kernel split, resolved before the fill:** dnf #66 (2026-11-22
  00:55 EET, 33 packages) installed the stock kernel
  `5.14.0-741.24.1.el9_9` as the grub default. With Paul's approval Claude
  rebooted Rocky at 16:36Z (`vms.sh stop-rocky` / `start-rocky`, clean
  ACPI; Rocky down about 30 s, SSH 16:35:51Z to 16:36:20Z). Rocky now runs
  `741.24.1` = grub default = `saved_entry`, so the kernel plan's stock
  default is `741.24.1` and the committed `rocky_kernel.py` works
  unchanged;
- the 2026-11-22 16:37:03Z facts: Rocky PID `30974` (the maintenance
  reboot), boot ID `23192359-fcf6-4dd8-9e36-a2f9303861dc` (copied from the
  facts file), UEFI vars `5c95aae0…` (new argv record), module ids 36/37,
  dnf #66;
- **f3 swap 0 used: no Phase 0b** this run (Phase 1 requires 0 used, else
  stop and ask Paul);
- `sh/recalls.py` also reports the 3h4 **advance count** and the recalls
  **by trigger**;
- **Phase 1a (D1-a) runs again:** zq3-r19 left `fdescfs.ko` and
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
and never reboots Rocky. The 2026-11-22 16:37:03Z facts
(`rocky-upgrade-…txt`): stamp `2026-11-22` (written 00:59:10 EET); the
newest dnf transaction is #66 (2026-11-22 00:55, 33 packages, among them
the stock kernel). python3.9, `authorized_keys` and the pod runtime are
byte-identical. `sh/n1-gate.py phase1` now expects #66. **The run must
start after that day's run has stamped and finish before the next one,
22:55 UTC on the run day (2026-11-22 22:55 UTC for a run that day); the
latest Phase 5 start is 21:25 UTC.** Phase 1 runs `n1-gate.py gate` early
and Phase 5 step 0 runs it again before Rocky is stopped. The gate computes
the dates from its own clock; the in-run cutoff (no new case after 22:15
UTC, guest poweroff before 22:45 UTC, the 22:35 UTC fallback) applies on
the run day.

**How `rocky_kernel.py` handles the kernel state.** A FakeRocky simulation
with the committed `test_rocky_kernel.py` rig (`scratch/n1split_test.py`,
from the earlier fill attempt, rerun here: 3 tests OK) showed: in the split
state (running `741.22.1`, default `741.24.1`) `install` refuses whichever
stock kernel the plan names ("install runs while Rocky runs its stock
kernel …" or "grub default is not the pinned stock kernel"); in the current
state (running = default = `741.24.1`) `install` and `select` pass.
`rocky_kernel.py` finds this run's grub entry only by the exact path
`/boot/vmlinuz-6.1.190-hfs-xz3`, never the elrepo `6.1.190-1.el9.elrepo`
entry, and `verify_added` requires every other grub entry unchanged.
Phase 12's restart is an ordinary stock boot of `741.24.1`.

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-zq3-r20-fill` (`0700`). This directory holds only
public hashes, paths, argv and the read-only facts. These stay private and
are never committed:
- the per-session keys;
- the runtime config, which holds host receipt private keys;
- the staging tarballs that contain it.

## Inputs

| Input | Value |
|---|---|
| Candidate | **r20 rebuild** (tasks 1h4, 3h4, 4h4): manifest `b37bcb06…` (194 files), package `/home/paul/tmp/claude/hfs-xz3-20261122-r20/package`, source checkout and repository baseline `d71ae02` (no `inputs/repository-changes.patch`). **New:** the guest kernel `6.1.190-hfs-xz3` (bzImage `2b8880a8…`, build ID `b4d6cd69…`, System.map `c726e252…`), the CGO daemon `c04c519f…` (GNU build ID `42f2732a…`), the window artifacts `1b7e68d8…`/`edae7dc3…`. **Same bytes:** the exporter `2fd7e32f…` (reused with the r17 native records), config `28fe4a8e…`, the five modules and `modules.*`, the init helper `8c02b98f…`. **Lab tools re-pinned** (`git diff f2f6c44 e2f406a -- tools docs/lab/zq3-fdescfs-window internal/runtimeprep/testdata`): `controller.py` `d2751223…` and `rocky_kernel.py` `7873da9e…` (admit only `b37bcb06…`), `bootstrap.py` `6272bd9e…` and `correlate.py` `d1c8dfce…` (comments; bootstrap's a84/l64 `claim_text` shows an open claim's age and held refusal), the window `pins.json`, `build-record.json` and the build script default. Unchanged: `guest.py` `005d9592…`, `guest-lock-probe.py` `fd439897…`, `pod_fixture.py` `07314543…`, `pod_workload.py` `c714370f…`, `argv.py` `78462c2e…`, `bootstrap_remote.py` `0dcb0fe0…`, `fill_plan.py` `893350cf…`, `runtime-guard.py` `fece96b3…`, native test guard `576d802a…` (all staged copies re-hashed against main by `build-stage.sh`) |
| Runtime spec | `internal/runtimeprep/testdata/zq3-spec.json` at `e2f406a`, unchanged since r17 (`ControlTickets` 24); `make-spec.py` changes only the run ID and the operator key path (the output equals zq3-r19's spec with the run ID renamed) |
| Facts | 2026-11-22 16:37:03Z, read-only, copied from `/home/paul/tmp/claude/hfs-zq3-r20-facts/` (stamp `20261122T163703Z`; `superseded/` ignored). The public [facts/](facts/) holds six of them, as zq3-r19 did plus the new grub listing: launch facts, f3 Phase 1, network/modules, Rocky grub, pod runtime and upgrade; the Rocky Phase 1 batch, the stderr files and the ssh hash line stay in the private fill |

## Prep compatibility with the candidate daemon

`hfs-runtime-prep` and the signer link main's `internal/viewcontrol`
(3h4 plus 2h4, 0a4, fh4), not the candidate's `d71ae02`. Checked:
`git diff d71ae02 e2f406a` is empty for `internal/runtimeprep`, `cmd/`,
`internal/controlledruntime/runtime_config.go` (the runtime config
validation), `internal/viewcontrol/source_floor.go`, `go.mod` and `go.sum`;
the viewcontrol `Bounds`, `Enrollment` and `TicketFamily` types are
unchanged. The ledger schema 3 (0a4/fh4) applies only to a ledger that
fenced a session; the ledger is written by the daemon at run time, never by
prep. The generated `generation.json`, `request-template.json` and
`volume-init.json` have exactly zq3-r19's key structure, and
`runtime-config.json` differs only in its session-UUID map keys. `argv.py
verify` and `hfs-runtime-prep check-argv` pass. **No format change: no
stop.**

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | **Rebuilt** at `e2f406a`: `hfs-runtime-prep` and the signer `hfs-retained9p-operator`, with `-trimpath`, CGO, VCS stamps and an unmodified private clone (`vcs.modified=false`); a second build of each, forced with `-a`, gave the same bytes. zq3-r19's prep `6220ecae…` and signer `6b723f18…` no longer match the source. G3 init helper: zq3-r1's reproduced build | prep `1028e2aa…`, signer `2a2c7618…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from the committed spec (`make-spec.py`). `Views.ControlTickets` 24 | spec `a788bf88…`, request template `e9689954…`, runtime config `8d29ecc5…`, volume-init `0bcac6b9…`; volume `188f8586-…`; sessions hfs0 `cf92bd03-…`, hfs1 `ff0b014f-…` |
| 1. Argv | `argv.py generate` from the 16:37:03Z launch facts (sessions, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass**. Against zq3-r19's facts only the UEFI vars changed (`rocky.conf`, vm-run, firmware, interfaces, bridge members unchanged) | record `bde99834…`, args `35b69ace…`, vars `5c95aae0…`, exporter `2fd7e32f…` |
| 2. Kernel plan | `rocky_kernel.py plan` at main, stock default **`/boot/vmlinuz-5.14.0-741.24.1.el9_9.x86_64`**. Against zq3-r19's it differs in `run_id`, `manifest_sha256`, the bzImage and System.map entries, the build ID and the stock default. The r19 package `09acd09d…` is refused as superseded ("use the r20 rebuild") | plan `bd69d9cc…`, build ID `b4d6cd69…` |
| 3. Controller batch | Template derived from zq3-r19's by `derive-templates.py`: run ID, fill path, sessions, candidate `09acd09d…` → `b37bcb06…`; `probe_sha256` stays guest.py `005d9592…`; be4 seconds 240/255 kept (checked); **T5 stays deferred** with the r9 reason. `check` refuses the template as designed (`probe/installed-manifest pin`). A synthetic copy with zero IDs **passes** (four cases and the deferral). Refused as designed: the r19 candidate (superseded), zq3-r14's seconds 120/60, plan 120 alone, plan 255 with requests 60, no declaration, `credentials` deferred | template `0ae25037…`, synthetic `13fb7e46…` |
| 4. Bootstrap plan | Template derived from zq3-r19's. New pins: bundle, kernel plan, `rocky_kernel.py`, the rebuilt prep and **the new daemon `c04c519f…`**. The unfilled template refuses as designed (`observer.ssh_sha256: SHA256 hex`). **F1 filled now** with `fill_plan.py bootstrap` from the 16:37Z `sha256 -r /usr/bin/ssh` line (`6a9e5573…`), then `bootstrap.py check` (journal `/3`) **pass** | template `c1d38f64…`, filled plan `7b342bfb…` |
| 5. Guard argv | Derived from zq3-r19's, with **`--runtime-sha256 c04c519f…`** and `--disposable-validation` exactly once, last. It equals `bootstrap.py`'s own `SshRemote.guard_argv()` for the filled plan, in shell string and argv, and passes `runtime-guard.py`'s `parse_args` and `check_command` | `eedfcd77…` |
| 6. Pod template | zq3-r19's `/2` template with the run ID renamed; NEVRAs and hashes match the 16:37Z pod-runtime facts (byte-identical to zq3-r19's facts); the committed `pod_fixture.py` accepts it and refuses the malformed variants. **Expected rootfs manifest `34a71569…`** (an expectation, not a pin; dnf #66 may change it, see Q5) | pod template `30eceb1c…` |
| 7. Lock-probe templates | zq3-r19's with the run ID renamed (150/240); the filled requests pass `validate_request`, case 151 is refused | m0 `e4dd1954…`, m1-T8 `7530e963…` |
| 8. Staging | `build-stage.sh`: 17 f3 and 18 Rocky files, two deterministic tarballs (a second tar run gave the same hashes); a verification extract matched all 35 expected hashes; the f3 tarball holds only `0/0` entries, modes `0700`/`0500`/`0400`, no parent entry; extract deleted. The path lists equal zq3-r19's with the run ID (and the fdq date) renamed. Against zq3-r19's: the daemon, two window artifacts, argv record, volume-init and runtime config (f3), and bzImage, System.map, plan and `rocky_kernel.py` (Rocky) changed | f3 tar `b331f112…`, Rocky tar `f4dac65f…` |
| 9. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` at main with `zq3-r1` → `zq3-r20`. Synthetic fills (not pins): zq3-r8's real Phase 4 facts with the r20 hashes **pass**; the r19 or xc4 hashes **refuse** (`pinned artifact differs or missing`); facts with both modules loaded **refuse** (`fdescfs module present`): hence Phase 1a | r20 pins `c30d4555…` |
| 10. Signing watcher | zq3-r19's with `D`, the comments and **the signer pin** changed; tested with a fake signer (wrong hash, command, action, repeat and signer hash refused; both exact requests signed) | `11f85ebf…` |
| 11. Process observer check | `sh/p4-observer.py` with **the daemon pin `c04c519f…`**; zq3-r19's real records (renamed, hash substituted) **pass**, unchanged they are refused for the `sha256` only; the `$NG` line (Rocky PID `30974`) passes the native guard's `parse_args` and `check_request` | `9620ecc9…` |
| 12. N1 gate helper | `sh/n1-gate.py` with `FACTS_TOP` **#66 of 2026-11-22 00:55**. Retested at fixed times on 2026-11-22: 17:30Z (also with the real 16:37Z record), 21:20Z and 21:25Z GO; 21:30Z, 22:56Z, 23:10Z STOP; the next day with the old Phase 1 STOP, fresh GO; a stale stamp STOP; 01:00Z GO; `phase1` true for the real facts (#66), false for #65 and #67 (14 cases) | `d38ffd1e…` |
| 13. Fence record helper | `sh/p11-fence.py` (docstring only); zq3-r19's real Phase 11 and case-failure evidence give `fenced` false | `ea6eea39…` |
| 14. Guard budget helper | `sh/guard-budget.py` (docstring only; be4 budgets unchanged). On **zq3-r19's real journal** (launch 16:15:05Z): its Phase 9 and T1 checks GO, each latest start GO and one second later STOP (all seven steps); malformed journals refused (`rc=2`); 22 cases | `550f153f…` |
| 15. Recall helper | `sh/recalls.py` (largest-total selection kept) **now also prints `advances` (3h4), `by_trigger` and the optional 2h4 `blocked`**: zq3-r19's real OBSERVEs (10 recalls, `advances` null: the r19 daemon predates the counter in its OBSERVE), a synthetic copy with `advances` 37 (reported), an existing output (exit 2), and the dry run's failure OBSERVE | `6a1e2d71…` |
| 16. Run sheet | [run-sheet.md](run-sheet.md): Phases 0-12 with the kept rules, the zq3-r19 run notes, "New for zq3-r20", **Phase 0b replaced by a no-op note**, the iom1-3 checks, the yf4 case-failure OBSERVE with `N=$((L+3))`, BEGIN/END markers in all 48 code blocks, and a run log template. `session.sh` (`faa2b9ac…`, hpin over 19 files) and seventeen helpers; `p10-requests.py` (`0e1862a8…`) re-pins the three r20 request templates. The Phase 8-10 lines were dry-run end to end (below) | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-22T00:00:00Z --format=gnu`.
The f3 tarball holds only `hfs-20261122-zq3r20-fdq` and
`hfs-validation-xz3/zq3-r20`, with **no entry for the existing parent**.

## Signing

Paul delegated both signatures (A7) to Claude. In Phase 8 the driver's
stdout goes through `bin/sign_watch.py` (`11f85ebf…`). The watcher runs the
signer (`2a2c7618…`) only if:
- the printed command is exactly one of the two below;
- the request file's SHA256 equals the printed `request_sha256`;
- each command runs at most once.

Anything else is refused, and the driver then times out (fails closed):

```
/home/paul/tmp/claude/hfs-zq3-r20-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r20-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r20-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r20-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r20-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r20-fill/bootstrap-run/work/signer-binding
```

On Paul's instruction, Claude places the operator key at the bundle's path
(`/home/paul/tmp/claude/hfs-zq3-r20-fill/operator/operator-signing-key`; the
empty `0700` directory exists) before the run. Nothing in the fill reads it.

## Run-time fills

As in the [zq3-r19 fill](../20261121-zq3-r19-filled-plan/README.md#run-time-fills)
(F1-F20, F22), with these values changed:

| Fill | When and source | Used by |
|---|---|---|
| N1 | Phase 1: as before; `n1-gate.py phase1` expects **#66**; the running kernel must equal the grub default **`741.24.1`** | Phase 1 Rocky gate, Phase 3 install, Phase 5 go/no-go, T7 |
| D1 | **D1-a, Phase 1a**: both modules are loaded (16:37Z: ids 36 and 37) | Phase 4 step 0 window plan fill |
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 16:37Z fact); Phase 4 step 0 rechecks it | bootstrap `observer.ssh_sha256` (plan `7b342bfb…`) |
| F3 pod rootfs manifest digest (`PM`) | Phase 3: expect `34a71569…`; **dnf #66 upgraded 33 packages**, so a different digest is plausible and must be explained (compare with zq3-r19's `p3-pod-manifest.json`) before `build` | `build`, T7 request |
| F3a process observer record | Phase 4 step 1a; **pass required** for the NEW 3h4 daemon `c04c519f…` (zq3-r19 passed for the wf4 `bb4ad73c…`) | go/no-go before setup |
| F11 Rocky PID and lstart | **fixed** from the 16:37Z facts (`30974`, `Sun Nov 22 16:36:05 2026` UTC, the maintenance reboot; boot ID `23192359-fcf6-4dd8-9e36-a2f9303861dc` copied from the facts file) | native guard host profile, Phase 5 recheck |
| F21 swap | **No Phase 0b**: swap 0 at the facts; Phase 1 requires 0 used (else stop and ask Paul); Phase 6 requires swap unused | Phase 1 swap line, Phase 6 |
| F23 recalls by trigger and advances (new) | `recalls.py` after T1 (or in the case-failure block) and in Phase 11 step 1b: `by_trigger`, `advances` | run log only; findings, never a gate |

## Changes from the zq3-r19 sheet

All zq3-r7 to zq3-r19 rules are kept and written into the lines they name
(Phase 1a; Phase 4 step 1a, now for `c04c519f…`; the UMA drain decision;
`rockyt` bounded guest unmounts; `pgrep -U root`; the T10 abort paths to
T8; the explicit `--no-child` path for a null exporter; observer key
removal on every abort; the pause rule; the single host-preflight retry; the
workstation `/usr/bin/ssh` recheck `16cb2c6a…`; the Phase 9-10 helpers; the
plain literal f3 key deletion line naming zq3-r20;
`restoration-summary.json`; the rc capture after `rockyt`; Phase 11 step
4a/4b; the SELinux gates; the h64 errata; the z64 T5 deferral; the
persisted variables; the m64 block-line rule; journal `/3`, evidence `/2`,
the L shift, the 984 timing; the N1 gate, the in-run cutoff,
`p11-fence.py`, `rockywait`; bash only; the be4 bounds, the guard budget
and the tool-timeout comments; `recalls.py` and its largest-total
selection; the BEGIN/END markers, the `N=$((L+3))` failure OBSERVE line, the
step 6 `grep -qx child-gone` loop and the step 7 order; the iom1-3 Stopped
checks; the swap rule). New for zq3-r20:
- **Candidate `b37bcb06…`** in the kernel plan, batch template, bootstrap
  plan (prep, kernel plan and daemon pins), guard argv, staging and the
  Phase 3, 4 and 8 expectations; **Phase 4 step 1a:** `p4-observer.py` pins
  `c04c519f…`.
- **"Execution notes from the zq3-r19 run"** (R19-1, the recalls, the
  case-failure block as written) and **"New for zq3-r20"**.
- **Phase 0b removed** (a short note says why); Phase 1, 5 and 6 swap lines
  require 0 used without a clearing step.
- **Kernel facts:** stock default `741.24.1` everywhere (Phase 1, 3, 12);
  Phase 12's restart is an ordinary stock boot; the run log template
  records swap and the kernel.
- **T1 expectations:** the advance count and the recalls by trigger
  (`recalls.py`), the model replay's 7 CALL_CONFLICT recalls and about 37
  advances, the 1h4/4h4 guest log lines (`local retirement mutex busy`,
  BLOCKED re-check, advance adopted or released unadopted `-EXDEV`), and an
  `EAGAIN` with `retry limit reached` named as R19-1 again.
- **Findings:** any `EAGAIN`, `EOPNOTSUPP`, `EKEYREJECTED`, `ESTALE` or
  `ETIMEDOUT` from a probe syscall, with its named syscall.

## Expectations: what changed since zq3-r19

zq3-r19 passed everything through Phase 9, fixed R18-1, and showed every
EXPECTED_TARGET recall DOMAIN-scoped (wf4), then failed on R19-1. With
**1h4** the guest waits for a busy retirement round and re-checks an
expected-target BLOCKED instead of giving up with `EAGAIN`. With **xf4**
(3h4 daemon, 4h4 kernel) an observation after the guest's own change asks
for an advance instead of starting a recall. **T1 should pass:** the model
replay (094/2a4, `TestZQ3T1CredentialsReplay`) with advance takes about
1 min 26 s modelled (about 2.5 min measured), with only the 7 CALL_CONFLICT
recalls and about 37 advance grants. Then **T2-T4, T6, T7, T9, T10, T11 and
T8** within the guard budgets (none of T2-T11 has run for real); T5 stays
deferred. Record:
- per-case admission timings (`p10-admission.py`);
- recalls by trigger and the advance count (`recalls.py`);
- every guest re4/1h4 log line (`local retirement mutex busy`, the
  `re-check` lines) and every 4h4 advance line;
- any `EAGAIN`, `EOPNOTSUPP`, `EKEYREJECTED`, `ESTALE` or `ETIMEDOUT` as a
  finding with its named syscall.

Known risks, not fixed: **CALL_CONFLICT recalls** (xf4 follow-up F; ALL
scope, 13-15 s each in zq3-r19), **ue4**, **dh4** (blocked-session
`DRAIN_WRITE`/`GAP_RESOLVE`; T9-T11 block sessions on purpose) and
**hh4**. The 0a4 participant fence and the fh4 retirement are on main but
not in this candidate's daemon.

**Guard budget.** Unchanged: the be4 worst cases (5025 s) do not fit the
3600 s guard, so a slow run STOPs early and records the rest unrun. Latest
starts after the launch: Phase 9 52:00, T1-T4 44:00, T6 50:00, T7 44:45, T9
(with T10) 38:30, T10 48:30, T8 46:00.

## Phases 9-10 recheck

Since zq3-r19, among the tools the sheet uses, `controller.py` and
`rocky_kernel.py` changed (they admit only `b37bcb06…`), and `bootstrap.py`
and `correlate.py` changed in comments and bootstrap's `claim_text` run-log
output. No CLI the sheet uses changed shape.

**Sheet dry run** (`dryrun/` in the private fill). The zq3-r19 harness,
renamed, extracted 91 command lines of this sheet from Phase 8 "After
READY" to T8, plus the Phase 7 `k04` baseline and the two Phase 11 evidence
lines, and ran them in order under bash with the real `fill_plan.py batch`,
`check-retained9p.sh check`, `correlate.py`, `session.sh` and the `sh/`
helpers, `controller.py` with faked guest dispatch, stub `f3`/`rocky` and a
synthetic READY run with L = 2. Every line passed (`HARNESS-DONE lines=91
L=2`). The case-failure block is skipped by the harness (it names `$CASE`);
its direct OBSERVE line was run separately against the same stub (sequence
17, `OBSERVED`), and `recalls.py` read its output (`advances` 0,
`by_trigger` printed).

**Negative and real-record checks** (`helpers-dryrun.py`: 38 cases, all as
expected). Also: `p9-ready.py` refused zq3-r19's real READY run (its
control socket is not the r20 path); `fill_plan.py batch` refused zq3-r19's
real run for the r20 plan pin; `p10-admission.py` on zq3-r19's real T1 log
reports `admission_ms` 8139, `deadline_hit` false and the failure `Refused`
"supplementary group access" (R19-1).

## Findings and open questions

- **Q0 N1.** Start after the run day's upgrade has stamped and finish
  before 22:55 UTC; latest Phase 5 start 21:25 UTC, no new case after
  22:15 UTC, guest off before 22:45 UTC. The gate computes the dates.
- **Q1 T5 deferred.** The run cannot complete zq3 acceptance.
- **Q2 the guard budget.** Worst cases 5025 s against 3600 s; T9 with T10
  must start by launch + 38:30.
- **Q3 the 1h4/4h4 kernel and the 3h4 daemon run for the first time**
  (builds, clean repros, source suites and offline admission only).
- **Q4 T2-T4, T6, T7, T9-T11 first real runs** with the be4 deadlines.
- **Q5 the pod rootfs manifest.** Expected `34a71569…`, but dnf #66
  upgraded 33 packages; the fill cannot tell whether any of them is in the
  rootfs, so a different digest must be explained before `build`.
- **Q6 memory.** At 16:37Z free 8.84 GB, inactive 0.14 GB, ARC 5.25 GB,
  swap 0, one minute after Rocky's start. Phase 5's estimate, the drain and
  the Phase 6 bar decide; iom1-3 must stay stopped.
- **Q7 UEFI vars drift.** The record pins `5c95aae0…`; any Rocky boot
  before Phase 7 changes it, and `host.py setup` then refuses.
- **Q8 Phase 1a.** Every D1-a so far unloaded both modules; a refusal stops
  before staging.
- **Q9 window pins file:** the r20 window pins live only in the private
  fill, with their hash recorded here.
- **Q10 the tool timeouts.** At least 300 s for `$CTL` and lock-probe lines,
  420 s for T7's run line, 1200 s for the Phase 11 step 6 loop.
- **Q11 iom1-3.** Stopped by default; the run never starts them; Claude
  messages the iormesh session after the run.
- **Q12 prep at main.** The prep and signer link main's viewcontrol, not
  the candidate's; checked compatible above. If main moves again before the
  run, the Phase 0 `git diff --stat e2f406a $FINAL` rule applies.
- **Q13 stale prose in `pins.json`.** Some carried-over descriptive fields
  (helper provenance notes under `phase9_10`, the `rechecked_against` note)
  still describe earlier fills; every hash is computed from its file, and
  the run sheet and this README are authoritative.

## Local verification

At `e2f406a` (a clean worktree of main), all passing:
- retained9p: 397 tests;
- directlaunch: 26 tests;
- `tools/lab` top level: 249 tests (3 skipped);
- `go test ./internal/runtimeprep`.

`bash -n` is clean for `build-stage.sh` and `session.sh`; `session.sh`
loads only in bash (zsh refuses) and its `hpin` and `okpin` pass. No code
was changed.
