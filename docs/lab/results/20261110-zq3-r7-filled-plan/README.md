# zq3-r7 filled run inputs (2026-11-10)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r6 fill](../20261110-zq3-r6-filled-plan/README.md) ·
[zq3-r6 record](../20261110-zq3-r6/README.md)

These are the exact run inputs for `zq3-r7`, the seventh attempt of zq3. They
were generated on the workstation with the committed tools at `main`
`ba25486` (after c64, d64 and f64). They mirror the
[zq3-r6 fill](../20261110-zq3-r6-filled-plan/README.md), which ran for real
and reached READY. The changes are:
- the run ID;
- the c64+d64 candidate `66b5bb2b…`, with a new guest kernel (SELinux in
  `CONFIG_LSM`), the new d64 daemon and rebuilt window artifacts;
- `hfs-runtime-prep` and the signer rebuilt;
- everything that pins them, re-pinned;
- the fresh facts;
- the SELinux gates;
- the zq3-r6 execution notes.

zq3-r6's option (b) is dropped: r7 runs the full plan.

Nothing here used SSH, the network, the lab, a VM, a mount, an installation
or the operator private key. **No authority follows from these files:**
every class A step in the [run sheet](run-sheet.md) still needs Paul's
explicit final GO.

`fdescfs.ko` (id 29) and `fusefs.ko` (id 30) are loaded on f3 again. zq3-r6
left them loaded in the same boot, so decision D1-a applies again as
**Phase 1a**.

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-zq3-r7-fill` (`0700`). This directory holds only
public hashes, paths, argv and the read-only facts. These stay private and
are never committed:
- the per-session keys;
- the runtime config, which holds host receipt private keys;
- the staging tarballs that contain it.

## Inputs

| Input | Value |
|---|---|
| Candidate | c64+d64 rebuild: manifest `66b5bb2b…`, package `/home/paul/tmp/claude/hfs-xz3-20261110g/package` (all 194 manifest entries checked), source `060dea0`. **New:** the c64 guest kernel `6.1.190-hfs-xz3`, with `CONFIG_LSM="lockdown,yama,integrity,selinux,bpf,landlock"`, bzImage `6f59b593…`, config `28fe4a8e…`, System.map `3d11cf22…` and GNU build ID `2ce818d9…`. The d64 CGO daemon `9b7eed8d…` (per-domain view fence, 16-cell guest reorder window). Window artifacts `56e87332…` (test) and `45f2d59a…` (helper), rebuilt from `060dea0`. **Unchanged:** the five modules and `modules.*`, bhyve `36423b6d…`, init helper `8c02b98f…` |
| Phase 1 facts | [facts/](facts/), collected read-only 2026-11-10 02:05:04Z. f3: boot `1794212992` (unchanged since zq3-r2), `fdescfs.ko` (id 29) and `fusefs.ko` (id 30) loaded with no fuse/fdesc mount, free 5.02 GB + inactive 3.25 GB, ARC 0.82 GB, `/usr/bin/ssh` `6a9e5573…`. Rocky: PID `41316` (UTC lstart `Tue Nov 10 00:42:41 2026`), boot ID `2ca051f4-…`, stock `5.14.0-687.66.1.el9_8`, `next_entry` empty, `boot_indeterminate=7`, SELinux Enforcing, `/etc/selinux/config` `SELINUX=enforcing`, **`/.autorelabel` absent**, python3.9 `1143733d…`, `authorized_keys` `e08542dc…`. The files hold no key material (grep for key headers and `ssh-*` lines) |
| Launch facts | `f3-launch-facts-20261110T020504Z.json` (`6796178b…`): `rocky.conf` `c101adf1…` unchanged, **UEFI vars `00f82a83…`** (zq3-r6: `bcc505d5…`). The vm-bhyve log shows zq3-r6's Phase 12 start at 00:37:44 UTC, the relabel reboot (run 1 exit 0) at 00:42:41 and run 2 since |
| Tap | `tap100` (bridge members `re0`, `tap0`) |
| Run ID / staging | `zq3-r7`, `/var/tmp/hfs-validation-xz3/zq3-r7` (the parent exists and holds zq3-r1 to zq3-r6) |
| Native guard root | `/var/tmp/hfs-20261110-zq3r7-fdq`, host profile `--rocky-pid 41316 --rocky-birth 'Tue Nov 10 00:42:41 2026'`, guard `576d802a…` (unchanged) |
| Window directory | `/var/tmp/hfs-fdescfs-windows/zq3-r7` (the parent exists, `root:wheel 0700`) |
| Rocky names | `/root/hfs-zq3-zq3-r7-*`, `/var/tmp/hfs-zq3-zq3-r7` (the zq3-r1 to zq3-r6 ones stay as evidence) |
| Operator public key | `2f6d909c…`; key path `…/hfs-zq3-r7-fill/operator/operator-signing-key` (empty `0700` directory, never read here) |

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | **Rebuilt** at `ba25486`: `hfs-runtime-prep` and the signer `hfs-retained9p-operator`. The build used `-trimpath`, VCS stamps and an unmodified private clone (`vcs.modified=false`); a second build of each gave the same bytes. Reason: `git diff 41cbefd ba25486` over the `go list -deps` directories of both commands lists d64's non-test changes in `internal/controlledruntime` (`facts.go`, `restart.go`), `internal/guestbridge` (`endpoint.go`, `mapping.go`, `restore.go`, `transport_ledger.go`) and `internal/viewcontrol` (`controller.go`, `fence.go`, `recall.go`, `tickets.go`). `go.mod`/`go.sum` are unchanged. zq3-r5's builds (prep `2d8d8e88…`, signer `89905914…`, reused in zq3-r6) are retired. G3 init helper: zq3-r1's reproduced build, kept by xz3-20261110g | prep `dbebb5a8…`, signer `9ca59cee…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from `testdata/zq3-spec.json` at `ba25486`, changing only `zq3-r1` → `zq3-r7` and the r7 operator key path (the same method reproduces zq3-r6's spec byte for byte). The guest budget is unchanged (reservations 1024, received 256, 512/512 MiB) | spec `52999de5…`, request template `b33aa7cf…`, runtime config `40727d33…`, volume-init `22c6c8ef…`; volume `31c9e5b9-…`; sessions hfs0 `34f14c1a-…`, hfs1 `c51e30b9-…` |
| 1. Argv | `argv.py generate` from the 02:05Z launch facts (sessions, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `53e384b9…`, args `ec8ef4cd…`, vars `00f82a83…` |
| 2. Kernel plan | `rocky_kernel.py plan` at main (`792b7aef…`; f64 admits only `66b5bb2b…`), stock default `/boot/vmlinuz-5.14.0-687.66.1.el9_8.x86_64`. It differs from zq3-r6's in run ID, manifest, bzImage, config and System.map; the five modules are the same. The 964 package `e01fab21…` is refused as superseded | plan `4b2e2212…`, build ID `2ce818d9…` |
| 3. Controller batch | Template derived from zq3-r6's: run ID, fill path, sessions, candidate `e01fab21…` → `66b5bb2b…`. `controller.py` (`c60d093d…`) changed only its admitted and superseded sets, comments and the refusal text. `check` refuses the template as designed (`probe/installed-manifest pin`). A synthetic copy with zero IDs passes; the same copy with the 964 candidate is refused as superseded | template `9fbe2c3a…` |
| 4. Bootstrap plan | Template derived from zq3-r6's (the one that ran) by `derive-templates.py`. New pins: bundle, kernel plan, `rocky_kernel.py`, the d64 daemon `9b7eed8d…` and the rebuilt prep `dbebb5a8…`. The guard pin `fece96b3…` is unchanged. The script re-hashes every pinned file at main and in the package. The unfilled template refuses as designed. **F1 filled now** with `fill_plan.py bootstrap` from the 02:05Z `sha256 -r /usr/bin/ssh` line (`6a9e5573…`, unchanged), then `bootstrap.py check` **pass** | template `69e97671…`, filled plan `edf849a8…` |
| 5. Guard argv | Derived from zq3-r6's, with `--runtime-sha256 9b7eed8d…` (checked against the package) and `--disposable-validation` exactly once, last. It equals `bootstrap.py`'s own `SshRemote.guard_argv()` for the filled plan, in shell string and argv, and passes `runtime-guard.py`'s `check_command` | in [pins](pins.json) |
| 6. Observer and staging pins | Unchanged script pins: `observer_key.py` `cd7f38c2…`, `observer.py` `2e1449dd…`, `installed.py` `c824b183…`, native guard `576d802a…`, `runtime-guard.py` `fece96b3…`, `fdescfs-window.py` `b00b19d1…`. Re-pinned to main: `rocky_kernel.py` `792b7aef…`, `argv.py` `5147a871…` (comment only). All 17 f3 and 18 Rocky files went into two deterministic tarballs; a second tar run gave the same hashes. A verification extract matched every expected hash and the f3 modes (owner `0/0`, no parent entry) and was then deleted | f3 tar `16ff1ee3…`, Rocky tar `84960ed5…` |
| 7. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` at main (`4b111318…`, as f64 left it) with `zq3-r1` → `zq3-r7`; `check_pins` passes. Synthetic fills, which are not pins: zq3-r6's real Phase 4 facts and host hashes **pass**, taken after its D1-a, with paths renamed and the three artifact hashes swapped for the new ones. The same facts with zq3-r6's unswapped host hashes **refuse** (`pinned artifact differs or missing`). Post-restore facts with both modules loaded (f3's state now) **refuse** with `fdescfs module present`; Phase 1a resolves that | r7 pins `1947e81c…` |
| 8. Signing watcher | zq3-r6's `bin/sign_watch.py` with `D`, the docstring run ID, the signer comment and **the signer pin `9ca59cee…`** changed. Tested locally with a fake signer: a wrong request hash, a wrong command, an unknown action, a repeat and a wrong signer hash were refused; both exact requests were signed | `0baecfb7…` |
| 9. Process observer check | `sh/p4-observer.py` from zq3-r6 with the run ID and **the daemon pin `9b7eed8d…`** changed (Phase 4 step 1a, **required**: new daemon bytes). The `$NG` line parses and passes the native guard's own `check_request`. It passed two synthetic good records and refused 14 bad ones. zq3-r6's real records (renamed) are refused only for the z54 `sha256`, and pass with the d64 `sha256` substituted | `4ac25114…` |
| 10. Run sheet | [run-sheet.md](run-sheet.md): Phases 0-12, 1a, 4/1a and U1-U3, with the SELinux gates in Phases 1, 9 and 12. `session.sh` (`2bd3f894…`) and eleven pinned helper scripts. The Phase 8-10 lines were dry-run end to end and the Phase 9 helpers were run on zq3-r6's real records (below) | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-10T00:00:00Z --format=gnu`.
The f3 tarball holds only `hfs-20261110-zq3r7-fdq` and
`hfs-validation-xz3/zq3-r7`. It has **no entry for the existing parent**
`hfs-validation-xz3/`, so extraction never rewrites the parent's mode or
owner. All directories are `0700`, binaries `0500`, other files `0400`,
owner `0/0`.

## Signing

Paul delegated both signatures (A7) to Claude. In Phase 8 the driver's
stdout goes through `bin/sign_watch.py` (`0baecfb7…`). The watcher runs the
signer (`9ca59cee…`) only if:
- the printed command is exactly one of the two below;
- the request file's SHA256 equals the printed `request_sha256`;
- each command runs at most once.

Anything else is refused, and the driver then times out (fails closed):

```
/home/paul/tmp/claude/hfs-zq3-r7-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-zq3-r7-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-zq3-r7-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-zq3-r7-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-zq3-r7-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-zq3-r7-fill/bootstrap-run/work/signer-binding
```

On Paul's instruction, Claude places the operator key at the bundle's path
before the run. Nothing in the fill reads it.

## Run-time fills

| Fill | When and source | Used by |
|---|---|---|
| D1 | **decided**: D1-a as Phase 1a (normal `kldunload` of fusefs and fdescfs right after Phase 1) | Phase 4 step 0 window plan fill |
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 02:05Z fact); Phase 4 step 0 rechecks it (`F1-unchanged`), a change stops | bootstrap `observer.ssh_sha256` (plan `edf849a8…`) |
| F2 fdescfs window plan and its hash (`WP`) | Phase 4 step 0: controller `facts`, host hashes, `realpath` library closure, f3 `date -u +%s`, then `fdescfs-window-plan.py --pins` with the r7 pins | setup, qualify, restore (`W`); Phase 5 recheck |
| F3 pod rootfs manifest digest (`PM`) | Phase 3: `pod_fixture.py manifest` on Rocky (zq3-r1 to zq3-r6: `f7f69339…`); `skip` if A8 is declined | `build`, T7 request |
| F3a process observer record | Phase 4 step 1a: guard stage `process-observer`, checked by `p4-observer.py` | go/no-go before setup; **pass required** (new daemon bytes) |
| F4 observer key pair, enrolled line, `after_sha256` (`EA`) | Phase 4 step 5: `ssh-keygen` on f3, `observer_key.py enrol` | runtime observer, Phase 11 `remove --expect-sha256`, banner-timeout check |
| F5 BLS entry ID | Phase 3 install record | `select` (from the state directory) |
| F6 boot ID, provider nonce, admission manifest SHA256, `virtioN` names, proposal, controller, provision hash | Phase 8 admission and runtime records | enrollment and bind requests (bootstrap fills them), controller batch plan |
| F7 m0/m1 mount IDs | Phase 8 mount steps (mountinfo) | batch plan, then `p10-requests.py` for the T6/T7/T8 requests |
| F8 batch plan and its hash (`B`) | Phase 8, after READY: `fill_plan.py batch`, then `check-retained9p.sh check` | every controller and correlator call |
| F9 runtime UUID (`U`), cut, epoch and lifecycle sequences | READY record via `p9-ready.py`; sequences 1 and 2 literal, then each from `correlate.py command` (`obs`, ARM, RESTART) | Phase 9 OBSERVE, T1-T11 |
| F10 exporter child PID (`XP`) | guard `receipt.json` `exporter.pid` (Phase 11 step 0; the `--no-child` line if null) | `host.py cleanup --child-pid` |
| F11 Rocky PID and lstart | **fixed** from the 02:05Z facts (`41316`, `Tue Nov 10 00:42:41 2026` UTC); rechecked in Phases 1, 4 (also by every `$NG` stage) and 5 | native guard host profile, Phase 5 recheck, `p5-mem.txt` RSS |
| F12 UMA drain decision | Phase 5 step 1a: `p5-estimate.py` `drain_recommended` | Phase 5 step 1b (or once in Phase 6) |
| F13 T6/T7/T8 request files | Phase 8 after the batch check: `p10-requests.py` (exclusive, `0600`, in `$E`) | T6, T7, T8 |
| F14 guard PID | Phase 11, only on the runtime-still-serving path (`pgrep -U root`) | the one SIGTERM to the guard |
| F15 SELinux state | Phase 1 (`/.autorelabel` absent, Enforcing), Phase 9 (guest `getenforce` `Enforcing`, enforce `1`), Phase 12 (relabel record) | Phase 1 and Phase 9 gates; Phase 12 finding |

## Changes from the zq3-r6 sheet

All zq3-r6 rules are kept:
- D1-a before staging as **Phase 1a** (ids 30 and 29 now);
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

Kept from the zq3-r5 and zq3-r6 executions:
- **Phase 11 step 9 is one plain line with literal paths**:
  `f3 "doas -n rm -- /var/tmp/hfs-validation-xz3/zq3-r7/observer/id_ed25519 /var/tmp/hfs-validation-xz3/zq3-r7/observer/id_ed25519.pub"`.
  Every abort path points at that line.
- **The window restore's outcome is `restoration-summary.json`**, not an
  `rc` reported through an output filter. Step 10 reads
  `/var/tmp/hfs-fdescfs-windows/zq3-r7/restoration-summary.json` into
  `$E/p11-restoration-summary.json` and judges by `"mount_restored": true`.
- **Every `rockyt … | tee` line ends with `; echo rc=${PIPESTATUS[0]}`**
  (T8 and Phase 11 step 2), so the run log gets rockyt's own status
  (124 = bound expired). Phase 11 step 2 now tees to `p11-umount-m0.txt` and
  `p11-umount-m1.txt`.

Dropped: zq3-r6's option (b) notes (SELinux `Disabled` as a finding, T6/T8
only). r7 runs Phases 9-10 in full.

New for zq3-r7:
- **Candidate `66b5bb2b…`** in the kernel plan, batch template, bootstrap
  plan, guard argv, window pins, staging, Phase 3, Phase 4 step 1a and
  Phase 8 expectations.
- **SELinux gates:**
  - Phase 1 stops if `/.autorelabel` exists, because the test boot under the
    exporter would relabel and reboot;
  - Phase 9 requires `getenforce` `Enforcing` and enforce `1` in the test
    guest, else Phase 11 with the runtime serving;
  - Phase 12 records the vm-bhyve runs, `journalctl --list-boots` and
    `/.autorelabel` after the stock boot. No relabel is expected; one is a
    finding, not a failure.
- **Phase 8** expects both mounts and READY with every step `ok`. A
  mount-1 `EKEYREJECTED` or a view-control `-110` would be a finding against
  d64. The failure text now also covers zq3-r6's case, a runtime that reaches
  READY although a mount failed: Phase 11 then takes the guard-SIGTERM
  branch.
- Phase 1 expectations for Rocky's current incarnation: PID, boot ID,
  `boot_indeterminate=7`, vars and `/.autorelabel`. Phase 1a module ids are
  30/29. The zq3-r6 directories are listed among the leftovers, and the
  interface order is `wg0` before `tap0`.

## Phases 9-10 recheck

The committed `correlate.py` (`82af8060…`), `guest.py` (`65a6a16d…`),
`guest-lock-probe.py` (`630a737d…`), `fill_plan.py` and `bootstrap.py` are
byte-identical to zq3-r6's snapshot. `controller.py` (`c60d093d…`) changed
only its admitted and superseded sets, comments and one refusal text, so no
CLI or record changed.

**Sheet dry run.** A scratch harness extracted **all 71 command lines** of
this sheet, from Phase 8 "After READY" to T8 (plus the Phase 7 `k04`
baseline line), from `run-sheet.md` itself and ran them in order. It used:
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
  `guest-lock-probe.py`;
- the new `; echo rc=${PIPESTATUS[0]}` T8 line printed `rc=0`.

**zq3-r6's real records.**
- `p9-ready.py` refuses zq3-r6's real run directory (`summary.json outcome
  is not ready`: mount-1 failed).
- It passes a scratch run directory that carries zq3-r6's real READY line
  with the socket renamed to zq3-r7 (runtime `efdbcd17-…`, epoch 1), and
  refuses the unrenamed line (wrong socket).
- `p9-observe.py` passes zq3-r6's real OBSERVE sequence 1 (OBSERVED,
  SERVING). It was checked against a batch plan built from the r7 template
  with zq3-r6's real boot ID, nonce, admission manifest and sessions (m1
  mount ID synthetic).
- It refuses the same OBSERVE against the r7 sessions and against a wrong
  plan hash.

The r6 negative dry run was also repeated, and every case was refused:
- a wrong sequence;
- a fenced admission;
- a rotated cut;
- an incomplete OBSERVE;
- a wrong socket;
- an unfiltered ARM;
- a wrong operation;
- a wrong plan hash;
- existing outputs.

## Findings and open questions

- **Q1 memory.** At 02:05Z free + inactive was 8.26 GB (inactive 3.25 GB;
  zq3-r6's facts had 12.89 GB). Inactive fell by about 4.6 GB since then,
  most likely because Rocky's fresh boot after the relabel turned it Active
  (the facts do not record Active). Phase 5's `active_based` estimate
  counts Active, and the Phase 6 bar decides (zq3-r6 margin 0.57 GB after the
  drain). The o54 host-profile floors (free ≥ 1 GiB, free + inactive ≥
  3.5 GiB) hold.
- **Q2 the d64 fixes are untested on f3.** The per-domain fence and the
  16-cell reorder window passed offline tests only. Mount-1 and READY with
  every step `ok` are this run's main question. Exporter-side ordering, and
  not pinning a send on a permanent refusal, belong to task e64 and are not
  in this candidate. A reorder deeper than 16 cells would stall as in zq3-r6.
- **Q3 first guest with SELinux enforcing.** c64 is verified offline only.
  Phase 9 now gates on `Enforcing`. T7 will be the first pod run against
  9p with an enforcing guest; a `container_t` denial is a T7 failure, never
  relabelled or disabled.
- **Q4 UEFI vars drift.** The record pins `00f82a83…`. Any Rocky boot before
  Phase 7 changes it, and `host.py setup` then refuses.
- **Q5 T9 ARM refused, Q6 Phase 11 step 2 after a FENCED T10, Q7 the T11
  receipt wait (5 minutes):** unchanged from zq3-r6; the sheet keeps the same
  choices.
- **Q8 window pins file:** the r7 window pins live only in the private fill,
  with their hash recorded here.
- **Q9 zq3-r5's f3 observer key:** still not visible to the fill (zq3-r6
  deleted its own key with the plain command).

No committed tool refused real inputs. These intended refusals all occurred:
- the controller template;
- the unfilled bootstrap template;
- the window filler on the current module state (Phase 1a resolves it) and on
  zq3-r6's unswapped artifact hashes;
- the superseded 964 candidate in a synthetic batch plan and in
  `rocky_kernel.py plan`.

## Local verification

At `ba25486` in the main checkout, all passing:
- retained9p: 280 tests;
- directlaunch: 26 tests;
- `tools/lab` top level: 245 tests (3 skipped);
- `go test -count=1 ./internal/runtimeprep`.

`shellcheck` is clean for `build-stage.sh` and `sh/*.sh` (as POSIX sh), and
for `session.sh` apart from SC2034/SC2086: its variables are used by the
sheet, and `$O` is split on purpose. No code was changed.
