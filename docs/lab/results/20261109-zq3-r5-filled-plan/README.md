# zq3-r5 filled run inputs (2026-11-09)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r4 fill](../20261109-zq3-r4-filled-plan/README.md) ·
[zq3-r4 record](../20261109-zq3-r4/README.md)

These are the exact run inputs for `zq3-r5`, the fifth attempt of zq3. They
were generated on the workstation with the committed tools at `main`
`41cbefd` (after z54 and 664) and mirror the
[zq3-r4 fill](../20261109-zq3-r4-filled-plan/README.md), which ran for real,
including its review-round-1 execution notes. The changes are:
- the run ID;
- the z54 candidate, with the new daemon re-pinned everywhere;
- rebuilt `hfs-runtime-prep` and signer;
- a **new Phase 4 step 1a** that qualifies the new daemon's process observer
  on f3;
- the fresh facts after Rocky restarted;
- the zq3-r4 lessons.

Nothing here used SSH, the network, the lab, a VM, a mount, an installation
or the operator private key. **No authority follows from these files:**
every class A step in the [run sheet](run-sheet.md) still needs Paul's
explicit final GO.

`fdescfs.ko` (id 27) and `fusefs.ko` (id 28) are loaded on f3 again. zq3-r4
left them loaded in the same boot, so decision D1-a applies again as
**Phase 1a**, right after Phase 1 and before staging.

**Rocky restarted at 20:20:05 UTC**, after zq3-r4's restore (Rocky was back
on SSH at 19:29:41). The cause is unknown. Rocky now has a new bhyve PID
(`5694`), a new boot ID, `boot_indeterminate=5` and new UEFI vars
`7b2192fd…`, so the argv record is new. The stock kernel, the empty
`next_entry` and `authorized_keys` `e08542dc…` are unchanged.

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-zq3-r5-fill` (`0700`). This directory holds only
public hashes, paths, argv and the read-only facts. These stay private and
are never committed:
- the per-session keys;
- the runtime config, which holds host receipt private keys;
- the staging tarballs that contain it.

## Inputs

| Input | Value |
|---|---|
| Candidate | z54 daemon rebuild: manifest `2e0412a6…`, package `/home/paul/tmp/claude/hfs-xz3-20261109e/package` (all 194 manifest entries checked), source `a20aca1`. New: CGO daemon `39c92d01…`, window artifacts `controlledruntime.test` `97fb5c07…` and `fdescfs-namespace` `cc92bcde…`. Unchanged: w54 kernel `6.1.190-hfs-xz3` (bzImage `5c46f218…`, System.map `d544349f…`, build ID `806cfbb2…`), config, five modules, bhyve `36423b6d…`, init helper `8c02b98f…` |
| Phase 1 facts | [facts/](facts/), collected read-only 2026-11-09 21:21:09Z. f3: boot `1794212992` (unchanged since zq3-r2), `fdescfs.ko` (id 27) and `fusefs.ko` (id 28) loaded with no fuse/fdesc mount, free 4.96 GB + inactive 7.94 GB, ARC 0.78 GB, `/usr/bin/ssh` `6a9e5573…`. Rocky: PID `5694` (UTC lstart `Mon Nov  9 20:20:05 2026`), boot ID `a12b26fd-…`, stock `5.14.0-687.66.1.el9_8`, `next_entry` empty, `boot_indeterminate=5`, python3.9 `1143733d…`, `authorized_keys` `e08542dc…`. The files hold no key material (grep for key headers and `ssh-*` lines) |
| Launch facts | `f3-launch-facts-20261109T212109Z.json` (`318ac184…`): `rocky.conf` `c101adf1…` unchanged, **UEFI vars `7b2192fd…`** (zq3-r4: `0d2d251e…`; Rocky restarted at 20:20) |
| Tap | `tap100` (bridge members `re0`, `tap0`) |
| Run ID / staging | `zq3-r5`, `/var/tmp/hfs-validation-xz3/zq3-r5` (the parent exists and holds zq3-r1 to zq3-r4) |
| Native guard root | `/var/tmp/hfs-20261109-zq3r5-fdq`, host profile `--rocky-pid 5694 --rocky-birth 'Mon Nov  9 20:20:05 2026'`, guard `576d802a…` (unchanged) |
| Window directory | `/var/tmp/hfs-fdescfs-windows/zq3-r5` (the parent exists, `root:wheel 0700`) |
| Rocky names | `/root/hfs-zq3-zq3-r5-*`, `/var/tmp/hfs-zq3-zq3-r5` (the zq3-r1 to zq3-r4 ones stay as evidence) |
| Operator public key | `2f6d909c…`; key path `…/hfs-zq3-r5-fill/operator/operator-signing-key` (empty `0700` directory, never read here) |

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | **`hfs-runtime-prep` and the signer `hfs-retained9p-operator` rebuilt** at `41cbefd` (`-trimpath`, VCS-stamped, unmodified private clone, `vcs.modified=false`). Both link `internal/guestbridge` and `internal/viewcontrol`, which z54 changed (`go list -deps`), so zq3-r2's signer `22e5fb90…`, used up to zq3-r4, is retired. A second build of each gave the same bytes. G3 init helper: zq3-r1's reproduced build (`cmd/homelabfs` does not reach the z54 code) | prep `2d8d8e88…`, signer `89905914…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from `testdata/zq3-spec.json` at `41cbefd`, changing only `zq3-r1` → `zq3-r5` and the r5 operator key path. The guest budget is unchanged from zq3-r4 (reservations 1024, received 256, 512/512 MiB) and passes the w54 floors and the actual-runtime validation | spec `073c2d9c…`, request template `48cc4d7d…`, runtime config `e200bc44…`, volume-init `3857d297…`; volume `346d2bd4-…`; sessions hfs0 `40ab199d-…`, hfs1 `3679b29d-…` |
| 1. Argv | `argv.py generate` from the 21:21Z launch facts (sessions, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `0382e2f1…`, args `363f7ade…`, vars `7b2192fd…` |
| 2. Kernel plan | `rocky_kernel.py plan` at main (`c949e487…`; 664 admits only `2e0412a6…`), stock default `/boot/vmlinuz-5.14.0-687.66.1.el9_8.x86_64`. It differs from zq3-r4's only in run ID and manifest: same kernel and modules | plan `cbe1799a…`, build ID `806cfbb2…` |
| 3. Controller batch | Template derived from zq3-r4's: run ID, fill path, sessions, candidate `89c928d5…` → `2e0412a6…`. `controller.py` (`b500305d…`) changed only its admitted and superseded sets. `check` refuses the template as designed (`probe/installed-manifest pin`). A synthetic copy with zero IDs passes the main `controller.py`; the same copy with the w54 candidate is refused as superseded | template `d07cd45b…` |
| 4. Bootstrap plan | Template derived from zq3-r4's (the one that ran) by `derive-templates.py`. It carries the new pins: z54 daemon `39c92d01…` (`runtime.sha256`), bundle, kernel plan, `rocky_kernel.py` and rebuilt prep. The guard pin `fece96b3…` is the unchanged `runtime-guard.py`. The script re-hashes every pinned file at main and in the package. The unfilled template refuses as designed. **F1 filled now** with `fill_plan.py bootstrap` from the 21:21Z `sha256 -r /usr/bin/ssh` line, then `bootstrap.py check` **pass** | template `3eae263b…`, filled plan `d14285d1…` |
| 5. Guard argv | Derived from zq3-r4's, with `--runtime-sha256 39c92d01…` (checked against the package) and `--disposable-validation` exactly once, last. It equals `bootstrap.py`'s own `SshRemote.guard_argv()` for the filled plan, in shell string and argv, and matches `runtime-guard.py`'s `check_command` shape | in [pins](pins.json) |
| 6. Observer and staging pins | Unchanged script pins: `observer_key.py` `cd7f38c2…`, `observer.py` `2e1449dd…`, `installed.py` `c824b183…`, native guard `576d802a…`, `runtime-guard.py` `fece96b3…`, `fdescfs-window.py` `b00b19d1…`. Re-pinned to main: `rocky_kernel.py` `c949e487…`, `argv.py` `167d3f46…` (comment only). All 17 f3 and 18 Rocky files went into two deterministic tarballs; a second tar run gave the same hashes. A verification extract matched every expected hash and the f3 modes and was then deleted | f3 tar `c90c50da…`, Rocky tar `d7235246…` |
| 7. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` at main (664: daemon, helper and test re-pinned) with `zq3-r1` → `zq3-r5`; `check_pins` passes. Synthetic fills, which are not pins: zq3-r4's Phase 4 facts (after its D1-a), with the host hashes renamed and the three z54 artifact hashes, **pass**. Post-restore facts with both modules loaded (f3's state now) **refuse** with `fdescfs module present`; Phase 1a resolves that | r5 pins `a59015b9…` |
| 8. Signing watcher | zq3-r4's `bin/sign_watch.py` with `D`, the docstring run ID and the signer pin and its comment changed. The new signer pin `89905914…` is in the watcher and in `hpin`. Tested locally with a fake signer: a wrong request hash, a wrong command, an unknown action, a repeat and a wrong signer hash were refused; both exact requests were signed | `1e8bf643…` |
| 9. Process observer check | New helper `sh/p4-observer.py` for Phase 4 step 1a (below). Tested locally: it passed a synthetic good record and refused nine bad ones | `c8db0160…` |
| 10. Run sheet | [run-sheet.md](run-sheet.md): Phases 0-12, 1a, the new 4/1a and U1-U3. `session.sh` (`923c6539…`) and eleven pinned helper scripts. The Phase 8-10 helpers were dry-run again against the committed correlator and controller (below) | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-09T00:00:00Z --format=gnu`.
The f3 tarball holds only `hfs-20261109-zq3r5-fdq` and
`hfs-validation-xz3/zq3-r5`. It has **no entry for the existing parent**
`hfs-validation-xz3/`, so extraction never rewrites the parent's mode or
owner. All directories are `0700`, binaries `0500`, other files `0400`,
owner `0/0`.

## New: Phase 4 step 1a, process observer qualification

The z54 daemon `39c92d01…` has new bytes. Only the f44 daemon `174949c1…`
passed `hfs-controlled-runtime --check-process-observer` on f3
(xz3-20261108b "Checks"), and the xz3-20261109e evidence states that this
does not carry over. Step 1a runs the staged daemon's read-only check once:
- under the native test guard's host profile (`$NG`: Rocky PID `5694`
  pinned, 60 s, 512 MiB RSS, free-page floors);
- as root through `doas`, the credentials `runtime-guard.py` later starts
  the runtime with;
- after staging, Phase 3 and the step 1 negative, but **before** window
  setup and long before the Rocky stop.

The check reads no config, starts no child and mounts nothing, and it does
not need fdescfs (the f44 run passed on devfs):

```bash
f3 "$NG --stage process-observer $R/bin/hfs-controlled-runtime --check-process-observer" | tee $E/p4-process-observer-guard.json
f3 "doas -n cat /var/tmp/hfs-20261109-zq3r5-fdq/checks/process-observer/stdout" > $E/p4-process-observer.json
f3 "doas -n cat /var/tmp/hfs-20261109-zq3r5-fdq/checks/process-observer/stderr" > $E/p4-process-observer.stderr
python3 -I -B $D/sh/p4-observer.py $E/p4-process-observer-guard.json $E/p4-process-observer.json $E/p4-process-observer.stderr | tee $E/p4-process-observer-check.json
```

`p4-observer.py` passes (exit 0) only if all of these hold:
- the guard ran exactly `[staged daemon, --check-process-observer]` with
  `error` null and child exit 0;
- stderr is empty;
- stdout is one `homelabfs-process-observer/1` record (the fields of
  `qualifyProcessObserver`) with:
  - `platform` `freebsd/amd64`;
  - `executable` `/var/tmp/hfs-validation-xz3/zq3-r5/bin/hfs-controlled-runtime`;
  - `sha256` `39c92d01…`;
  - `publication` null;
  - `uid` 0 and `gid` 0, with the gid among `groups`;
  - a positive `pid`, an `incarnation` `freebsd:<pid>:…` and a positive
    `text_vnode`.

**On failure the run stops before Phase 5.** At that point nothing is
selected, Rocky runs, the window is not set up and there is no observer key.
The guard's `checks/process-observer` stays as evidence; U2 removes the
installed test kernel. Only a guard refusal before the child started may be
retried, once, under a new stage name.

## Signing

Paul delegated both signatures (A7) to Claude. In Phase 8 the driver's
stdout goes through `bin/sign_watch.py` (`1e8bf643…`). The watcher runs the
rebuilt signer (`89905914…`) only if:
- the printed command is exactly one of the two below;
- the request file's SHA256 equals the printed `request_sha256`;
- each command runs at most once.

Anything else is refused, and the driver then times out (fails closed):

```
/home/paul/tmp/claude/hfs-zq3-r5-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r5-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r5-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r5-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r5-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r5-fill/bootstrap-run/work/signer-binding
```

On Paul's instruction, Claude places the operator key at the bundle's path
before the run. Nothing in the fill reads it.

## Run-time fills

| Fill | When and source | Used by |
|---|---|---|
| D1 | **decided**: D1-a as Phase 1a (normal `kldunload` of fusefs and fdescfs right after Phase 1) | Phase 4 step 0 window plan fill |
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 21:21Z fact); Phase 4 step 0 rechecks it (`F1-unchanged`), a change stops | bootstrap `observer.ssh_sha256` (plan `d14285d1…`) |
| F2 fdescfs window plan and its hash (`WP`) | Phase 4 step 0: controller `facts`, host hashes, `realpath` library closure, f3 `date -u +%s`, then `fdescfs-window-plan.py --pins` with the r5 pins | setup, qualify, restore (`W`); Phase 5 recheck |
| F3 pod rootfs manifest digest (`PM`) | Phase 3: `pod_fixture.py manifest` on Rocky (zq3-r1 to zq3-r4: `f7f69339…`); `skip` if A8 is declined | `build`, T7 request |
| **F3a process observer record** (new) | Phase 4 step 1a: guard stage `process-observer`, checked by `p4-observer.py` | go/no-go before setup; pass required |
| F4 observer key pair, enrolled line, `after_sha256` (`EA`) | Phase 4 step 5: `ssh-keygen` on f3, `observer_key.py enrol` | runtime observer, Phase 11 `remove --expect-sha256`, banner-timeout check |
| F5 BLS entry ID | Phase 3 install record | `select` (from the state directory) |
| F6 boot ID, provider nonce, admission manifest SHA256, `virtioN` names, proposal, controller, provision hash | Phase 8 admission and runtime records | enrollment and bind requests (bootstrap fills them), controller batch plan |
| F7 m0/m1 mount IDs | Phase 8 mount steps (mountinfo) | batch plan, then `p10-requests.py` for the T6/T7/T8 requests |
| F8 batch plan and its hash (`B`) | Phase 8, after READY: `fill_plan.py batch`, then `check-retained9p.sh check` | every controller and correlator call |
| F9 runtime UUID (`U`), cut, epoch and lifecycle sequences | READY record via `p9-ready.py`; sequences 1 and 2 literal, then each from `correlate.py command` (`obs`, ARM, RESTART) | Phase 9 OBSERVE, T1-T11 |
| F10 exporter child PID (`XP`) | guard `receipt.json` `exporter.pid` (Phase 11 step 0; the `--no-child` line if null) | `host.py cleanup --child-pid` |
| F11 Rocky PID and lstart | **fixed** from the 21:21Z facts (`5694`, `Mon Nov  9 20:20:05 2026` UTC); rechecked in Phases 1, 4 (also by every `$NG` stage) and 5 | native guard host profile, Phase 5 recheck, `p5-mem.txt` RSS |
| F12 UMA drain decision | Phase 5 step 1a: `p5-estimate.py` `drain_recommended` | Phase 5 step 1b (or once in Phase 6) |
| F13 T6/T7/T8 request files | Phase 8 after the batch check: `p10-requests.py` (exclusive, `0600`, in `$E`) | T6, T7, T8 |
| F14 guard PID | Phase 11, only on the runtime-still-serving path (`pgrep -U root`) | the one SIGTERM to the guard |

## Changes from the zq3-r4 sheet

All zq3-r4 rules and execution notes are kept:
- D1-a before staging as **Phase 1a**;
- the UMA drain decision in Phase 5;
- the mandatory one-line-per-step Phase 11 order;
- observer key removal on every abort after Phase 4 step 5;
- pause when f3 is unreachable;
- the single retry after a host-preflight-only failure;
- the Phase 0 recheck of the workstation `/usr/bin/ssh` (`16cb2c6a…`,
  unchanged at fill time);
- the Phase 9-10 helpers.

The review-round-1 overrides are now also written into the lines they name:
- `pgrep -U root -lf runtime-guard.py` for the guard SIGTERM;
- `timeout 150` on every guest unmount (Phase 11 step 2 and T8);
- the T10 abort paths: a restart refusal or `REFUSED` leads to T8, then the
  guard-SIGTERM branch;
- an explicit `--no-child` line for a null exporter.

New for zq3-r5:
- **Phase 4 step 1a**, the process observer qualification (above).
- **Daemon re-pinned everywhere:** bootstrap `runtime.sha256`, the guard
  argv `--runtime-sha256`, the f3 staging list, the window pins and the
  bootstrap template, all `39c92d01…`; the batch template and kernel plan
  name candidate `2e0412a6…`.
- **Signer rebuilt**, so `hpin` and the watcher carry `89905914…`.
- **Phase 8 expectations for z54.** zq3-r4's mount-0 passed `bind_super`
  but blocked in `hfs_transport` on an unanswered pre-acquire VIEW_POLL
  (R4-1). z54 answers every VIEW_POLL and VIEW_ACK. A failing request is now
  a `blocked` cell that returns EIO to the guest at once, with a
  `BlockedError` class in `transport.db`. mount-0 should complete, mount-1
  follow, and READY arrive. If mount-0 blocks again, the sheet takes the
  live owner snapshot (`p8-owner-live.tar`), as zq3-r4 did.
- Phase 1 expectations for the restarted Rocky: PID, boot ID,
  `boot_indeterminate=5` and vars. Phase 1a module ids 28/27. The zq3-r4
  directories are listed among the leftovers.

**Phases 9-10, rechecked against `controller.py` (`b500305d…`) and
`correlate.py` (`82af8060…`) at `41cbefd`:**
- `correlate.py`, `fill_plan.py`, `bootstrap.py`, `guest.py` and
  `fdescfs-window-plan.py` are byte-identical to zq3-r4's snapshot.
- `controller.py` changed only `ADMITTED`/`SUPERSEDED`.
- z54 changed no lifecycle OBSERVE evidence field.

The Phase 9-10 commands are therefore unchanged apart from names and pins.
The four helpers and `obs` were dry-run again in scratch against
`test_correlate.py`'s fixtures, with the real correlator and controller and
a stub `f3`. Every accepted shape passed. These were refused:
- a wrong sequence;
- a fenced admission;
- a rotated cut;
- an incomplete OBSERVE;
- an unfiltered ARM;
- a wrong operation;
- a wrong socket;
- a wrong plan hash;
- existing outputs.

**`transport.db` `BlockedError`:** z54 adds this `omitempty` field to rows of
blocked cells. Only the daemon itself decodes those rows (`restart.go`, the
same new binary). No tool the sheet runs reads `transport.db`: Phase 11 and
the Phase 8 snapshot only tar `runtime/owner`. Readers built before the
field existed would refuse such rows, but none runs here.

## Findings and open questions

- **Q1 Rocky restart at 20:20 UTC (cause unknown).** It happened about 50
  minutes after zq3-r4's Rocky came back and was not part of any run step.
  Please check Rocky's journal for the cause before the GO. Another restart
  before Phase 1 changes the PID, lstart and UEFI vars; that is a plan
  revision (new argv record, `NG`, `p5-recheck.py`).
- **Q2 observer check credentials.** Step 1a runs as root (`$NG` uses
  `doas`), the runtime's real credentials. The f44 qualification ran as
  `paul` (uid 1001). The pass criteria require uid/gid 0. Please confirm that
  root is the intended qualification identity.
- **Q3 a blocked mount-0 again.** If z54 does not cure R4-1, the run ends as
  zq3-r4 did, with T1-T11 unrun. The extra `p8-owner-live.tar` snapshot is
  the only addition to that path.
- **Q4 T9 ARM refused, Q5 Phase 11 step 2 after a FENCED T10, Q6 the T11
  receipt wait (5 minutes):** unchanged from zq3-r4 (its Q1-Q3); the sheet
  keeps the same choices.
- **Q7 UEFI vars drift (open since zq3-r1 F4).** The record pins `7b2192fd…`.
  Any Rocky boot before Phase 7 changes it, and `host.py setup` then refuses.
- **Q8 memory.** At 21:21Z free + inactive was 12.9 GB with Rocky running
  (zq3-r4 at 18:27Z: 11.65 GB). zq3-r4's Phase 6 margin after the drain was
  0.54 GB.
- **Q9 window pins file:** the r5 window pins live only in the private fill,
  with their hash recorded here.

No committed tool refused real inputs. The intended refusals all occurred:
- the controller template;
- the unfilled bootstrap template;
- the window filler on the current module state (Phase 1a resolves it);
- the superseded w54 candidate in a synthetic batch plan.

## Local verification

At `41cbefd` in the main checkout, all passing:
- retained9p: 280 tests;
- directlaunch: 26 tests;
- `tools/lab` top level: 245 tests (3 skipped);
- `go test -count=1 ./internal/runtimeprep`.

`shellcheck` is clean for `build-stage.sh` and `sh/*.sh` (as POSIX sh), and
for `session.sh` apart from SC2034/SC2086: its variables are used by the
sheet, and `$O` is split on purpose. No code was changed.
