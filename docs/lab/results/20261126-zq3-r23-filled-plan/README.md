# zq3-r23 filled run inputs (2026-11-26)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r22 fill](../20261126-zq3-r22-filled-plan/README.md) ·
[zq3-r22 record](../20261126-zq3-r22/README.md) ·
[r23 candidate](../../../../tools/integration/artifacts/evidence/xz3-20261126-r23/README.md) ·
[`hfs_fdattr`](../../../../tools/lab/retained9p/README.md#hfs_fdattr-mounts-bl4-s64)

These are the exact run inputs for `zq3-r23`, the twenty-third attempt of
zq3: Phases 0 and 1 of the [run plan](../../zq3-run-plan.md) only. They were
generated on the workstation with the committed tools at `main` `f584de4`
(the r23 candidate merge and its review round 1). They mirror the
[zq3-r22 fill](../20261126-zq3-r22-filled-plan/README.md), which ran on
2026-11-26: Phases 0 to 9 passed with `hfs_fdattr` on both mounts, then
T1's `create(group-tail)` failed `ENOSPC` after 33 s while m0 blocked on an
unanswered `VIEW_RETIRE` (`-110`) and m1 on `PROCESS_EXIT (-28)` (R22-1).
The operator also started Phase 9 about 20 minutes late, because it waited
for a task notification instead of the driver log (R22-2). Phase 11 and 12
were clean.

**This run does not happen on the fill day.** The facts are of 2026-11-26
20:15:19Z; that day's latest Phase 5 start (21:25 UTC) was too close. **On
the run day, after Rocky's nightly dnf run (00:55 EET = 22:55 UTC) has
stamped, Phase 0's recheck and fresh Phase 1 facts come first**, as the
zq3-r21 later-day refresh did (commits `9b28254`, `18f268d`); see
[Run-day refresh](#run-day-refresh). **The refresh ran at 2026-11-26
22:56:44Z (run day 2026-11-27 EET): a record refresh** (dnf #69, nothing
pinned changed); see [the result](#run-day-refresh-result-2026-11-26-225644z).

The changes are:
- the run ID (`zq3-r23`) and the native guard root
  `/var/tmp/hfs-20261126-zq3r23-fdq` (fill date 2026-11-26);
- the **r23 candidate `21c49a55…`** (source checkout and repository
  baseline `21d8478`, the km4 merge):
  - **a NEW CGO daemon** `43238fe9…` (go1.27.4, km4: a domain-wide
    `VIEW_RETIRE` refusal is answered `BLOCKED` at the unchanged floor,
    instead of a `WouldBlock` the transport never answered). **Phase 4 step
    1a is required for it**, with the guard's `--runtime-sha256
    43238fe9…`;
  - **a NEW guest kernel** (build ID `2435216f…`): km4's log line for each
    6m4 retire-and-retry round (`retired under charge pressure: …`);
  - **NEW window artifacts** built with the daemon: `controlledruntime.test`
    `d58fedc4…`, `fdescfs-namespace` `6d54858c…` (new window pins);
  - the exporter `8cadae40…` is unchanged (ran in zq3-r21 and zq3-r22), so
    the runtime config's `Exporter.SHA256` stays;
- **`hfs_fdattr` stays enabled** (decision below);
- `hfs-runtime-prep` and the signer rebuilt at `f584de4` with go1.27.4 (both
  link the km4-changed `controlledruntime` and `viewcontrol` packages);
- a regenerated bundle (new sessions), argv record (the UEFI vars changed
  with zq3-r22's Phase 12 start), Rocky kernel plan, batch plan, bootstrap
  plan, staging tarballs and helper pins;
- re-pinned tools: `controller.py` and `rocky_kernel.py` (admit only
  `21c49a55…`; `2c85b230…` is refused as superseded) and the window
  `pins.json`. `git diff 1ed87bf f584de4 -- tools/lab` changed nothing else
  that runs (only `build-fdescfs-window-artifacts.sh`, a README and tests);
- Rocky's new incarnation, from zq3-r22's Phase 12 start: PID `50982`,
  lstart `Thu Nov 26 14:51:24 2026` UTC, boot ID
  `99de7fb2-c8e3-4b60-afef-7b11e77b4142`, in `NG`, `p5-recheck.py` and
  `p5-estimate.py`;
- the f3 modules left loaded by zq3-r22 are now ids 39 (`fdescfs.ko`) and
  40 (`fusefs.ko`): **Phase 1a runs**;
- **R22-2: Phase 8 block 2a** (new). The driver line runs in the
  background, and the operator at once polls the driver log with bounded
  loops that read files and so cannot match themselves:
  `timeout 540 sh -c 'until grep -q READY "$1" || test -s "$2"; do sleep 5;
  done' sh $E/p8-bootstrap.jsonl $D/bootstrap-run/summary.json`, then
  `timeout 300 sh -c 'until test -s "$1"; …' sh …/summary.json`, and goes
  straight on to the after-READY lines. A task notification is never what
  the operator waits for. The rules section says the same.

Unchanged: the exporter, the pod template's runtime pins (no dnf run since
#68 at the facts), both workstation and f3 ssh pins, the spec, the init
helper, the iormesh rule and every be4 bound.

Phase 1 ran for real, read-only, over the pinned host keys only (below).
Nothing else touched a host: no staging, no write on f3 or Rocky, no
kldload or kldunload, no VM lifecycle, no key enrolment, and the operator
private key was never read (the empty `0700` `operator/` directory exists).
**No authority follows from these files:** every class A step in the
[run sheet](run-sheet.md) still needs Paul's explicit final GO, including
the Phase 1a unload and T7 (A8). **zq3 acceptance stays incomplete by
design**, because T5 does not run.

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill` (`0700`, under the
candidate's run root). This public directory holds only public hashes,
paths, argv and the read-only facts. These stay private and are never
committed:
- the per-session keys;
- the runtime config, which holds host receipt private keys;
- the staging tarballs that contain it.

## The `hfs_fdattr` decision

The retained9p README's
[`hfs_fdattr` section](../../../../tools/lab/retained9p/README.md#hfs_fdattr-mounts-bl4-s64)
sets the rule: the option is never implied, a plan asks for it, and only a
candidate in `controller.FD_ATTR` may. `FD_ATTR` now holds exactly
`21c49a55…`, whose guest kernel, exporter and daemon all carry s64. In
zq3-r21 and zq3-r22 both mountinfo rows showed the option once, right after
`cache=mmap`, and T1's `fchown` and `fchmod` of `group` through the open
description returned 0. So **this run keeps it enabled**:
- **batch template:** `"fd_attr": true` at the top and in each of the four
  requests (release and step requests copy it);
- **bootstrap plan**, all three fields together (nl4):
  - `fd_attr_candidate_sha256` `21c49a55…`;
  - `fd_attr_candidate_manifest`
    `/home/paul/tmp/claude/hfs-xz3-20261126-r23/package/manifest.json`,
    read under that digest: its daemon is `runtime.sha256` (`43238fe9…`)
    and its exporter the runtime config's `Exporter.SHA256` (`8cadae40…`);
  - `fd_attr_kernel_plan`, this fill's `kernel/rocky-kernel-plan.json`,
    read under `kernel.plan_sha256`; its `manifest_sha256` is the same
    candidate;
- **mounts:** `cache=mmap,hfs_fdattr`. Each mountinfo row must show
  `hfs_fdattr` once, right after `cache=mmap`;
- **T3** adds the VM-only s64 peer-rename guard: `stat` of the old name
  through m0 must fail `ENOENT`. It has not run for real yet.

Checked locally:
- `bootstrap.py check` passes the filled plan. It refuses:
  - a plan without `fd_attr_kernel_plan` (closed keys);
  - the r22 manifest `2c85b230…` (superseded);
  - zq3-r22's kernel plan (its bytes differ from `kernel.plan_sha256`);
  - a missing manifest file;
  - zq3-r22's daemon `c7b89648…` as `runtime.sha256` (not the candidate's
    daemon).
- `controller.py check` passes a zero-ID copy of the batch. It refuses:
  - `fd_attr` false;
  - a request without `fd_attr`;
  - requests with it under a plan without it;
  - the r22, second r21, first r21 and r20 candidates (superseded).
- The sheet dry run's `fill_plan.py batch` fills `fd_attr: true`. On a
  synthetic READY run whose mountinfo rows lack `hfs_fdattr` it refuses:
  "bootstrap plan names fd_attr candidate 21c49a55… but its mounts do not
  show hfs_fdattr".

## Phase 1 facts (2026-11-26 20:15:19Z)

Read-only, one bounded batch per host, stdout and stderr kept apart:
- `ssh -F /dev/null` with `StrictHostKeyChecking=yes`, no global
  known_hosts, `BatchMode`;
- f3 with `docs/lab/f3-host-known_hosts` (alias `f3`);
- Rocky through f3 with `docs/lab/rocky-known_hosts` (alias `rocky`).

The only `doas` commands were `vm list`, `ls /dev/vmm`, `test -e` and
`host.py facts` from stdin. The Rocky batch also covered `dnf history info
68`, `needs-restarting -r`, `systemctl is-system-running` and the boot list.

**f3:**
- boot `1794574308`, unchanged since Paul's 2026-11-13 reboot;
- `vm list`: `rocky` Running (PID 50982); `freebsd`, `hfs-test0..2` and
  **iom1, iom2, iom3 Stopped**; no iormesh k3s VM listed; `/dev/vmm` holds
  only `rocky`; `vm-public` members `tap0`, `re0`. **After the facts, the
  iormesh session started iom1 and iom2 (2 GiB each, for up to about 5
  hours) with the orchestrator's agreement**, since this run is not before
  the next day. The run-day refresh must find every iormesh VM Stopped
  again; the sheet never touches them;
- swap 0 used;
- memory (a record only, not a headroom judgement; Rocky up 5.4 hours):

  | Item | Value |
  |---|---|
  | physmem | 16,879,837,184 |
  | free | 1,324,113 pages, 5.42 GB |
  | inactive | 1.55 GB |
  | active | 0.24 GB |
  | wired | 9.24 GB |
  | ARC | 8.34 GB (`c_min` 0.53 GB) |

- `fdescfs.ko` (id 39) and `fusefs.ko` (id 40) still loaded, left by
  zq3-r22, with no fuse/fdesc mount: **Phase 1a runs**;
- the r23 paths are absent (guard root `hfs-20261126-zq3r23-fdq`);
- the PoC share is present, zroot is ONLINE with 343G free, and
  `/usr/bin/ssh` is `6a9e5573…` (unchanged, F1 holds);
- launch facts:
  - **UEFI vars `4f63d71b…`** (new: zq3-r22's Phase 12 start changed them;
    `0be7dec3…` at the zq3-r22 fill);
  - `rocky.conf` `c101adf1…`, vm-run `43753bc8…` and firmware
    `6b48e967…`, all unchanged;
  - `tap100` is free (interfaces `re0 lo0 vm-public pflog0 vm-hfslab wg0
    tap0`).

**Rocky:**
- **runs the stock `5.14.0-741.27.1.el9_9.x86_64`**, which is also the
  grub default and `saved_entry`; `next_entry` is empty: **no kernel
  split**. Grub entries: 7.0.3 elrepo, 6.1.190 elrepo, 741.27.1, 741.24.1,
  741.22.1, rescue;
- PID `50982`, lstart `Thu Nov 26 14:51:24 2026` UTC, boot ID
  `99de7fb2-c8e3-4b60-afef-7b11e77b4142` (zq3-r22's Phase 12 incarnation,
  as expected), up 5:23; systemd `running`;
- Enforcing, no `/.autorelabel`, no `hfs-xz3` file in `/boot`, no
  `/etc/homelabfs`;
- `authorized_keys` `e08542dc…` (one line), python3.9 `729ac37f…`;
- the zq3-r1 to zq3-r22 leftovers under `/root` and `/var/tmp` (evidence),
  no zq3-r23 path.

**Rocky's upgrade:**
- clock `Europe/Sofia (EET, +0200)`, synchronized;
- stamp `2026-11-26` (00:59:03 EET); newest dnf transaction still **#68**
  (2026-11-26 00:55 EET, 29 packages: the stock kernel `741.27.1`,
  kernel-tools/-libs/-headers, ruby 3.0.7-171 and its gems, webkit2gtk3-jsc
  2.54.3; the 687.66.1 kernel removed); the next run is at 22:55 UTC;
- `needs-restarting -r` is clean; the hourly ticks only log the deferred
  reboot;
- the pod runtime equals zq3-r22's (podman `8d94a8fb…` `5.8.5-5.el9_9`,
  crun, conmon, seccomp), so **the pod template's pins are unchanged**.

The public [facts/](facts/) holds the f3 launch facts, the Phase 1 batch
and network/modules, and Rocky grub, pod runtime, upgrade, dnf #68 (with
`needs-restarting` and the boot list) and uptime. The Rocky Phase 1 batch,
the stderr files and the f3 ssh hash line stay in the private fill.

## Run-day refresh

**N1:** the gate computes the times on the run day; the bounds are always:

| Bound | Time (UTC, run day) |
|---|---|
| Latest Phase 5 start | **21:25** |
| No new case or step after | 22:15 |
| Fallback (observer removal and poweroff at once) | 22:35 |
| Guest poweroff before | 22:45 |

On 2026-11-26 the latest Phase 5 start was 21:25Z, about an hour after the
facts: too close for Phases 0 to 4, so the run is on a later day.
`n1-gate.py phase1` reports `dnf_unchanged: true` on the facts (#68), and
the fixed-time tests show the real 20:15Z records give GO at 21:25Z, STOP
at 21:26Z and STOP on any later day (fresh Phase 1 required).

On the run day, after that day's nightly dnf run has stamped, before any
class A step:
1. **Phase 0 block** of the sheet (pins, argv verify, `check-argv`,
   `bootstrap.py check`, workstation ssh) and `git diff --stat f584de4
   <main>` over the tools (must be empty or tests/docs only).
2. **Fresh read-only Phase 1 facts** (the sheet's Phase 1 block, or the
   fill's collector), compared with the 20:15Z facts:
   - **Rocky's running kernel must equal the grub default, both stock.** If
     the nightly dnf installed a new stock kernel as the default while
     Rocky still runs `741.27.1` (the N1 kernel split): **STOP and report.**
     Paul must approve a Rocky maintenance reboot into the new default (as
     on 2026-11-26 06:46Z), and the reboot means a refill (kernel plan stock
     default, UEFI vars, argv record, bundle and sessions, Rocky PID/lstart
     in `NG` and the Phase 5 helpers, staging);
   - a new transaction (#69 or later) that changed nothing pinned: refresh
     `sh/n1-gate.py`'s expected transaction and the sheet's N1 lines (the
     gate does not read it, so this is a record fix). Changing
     `sh/n1-gate.py` also means updating its hpin in `session.sh`, then
     `session.sh`'s pin in the Phase 0 `sha256sum` list and in `pins.json`
     (hpin refuses a stale pin, so a missed step fails closed);
   - a changed pod runtime: refill the pod template;
   - any Rocky restart (new PID, boot ID or UEFI vars) or f3 reboot: refill
     (an f3 reboot also unloads both modules, so Phase 1a changes);
   - **every iormesh VM Stopped and `/dev/vmm` = `rocky`** (iom1 and iom2
     were started after these facts); if one still runs, the run waits and
     the orchestrator coordinates with the iormesh session;
   - swap 0 used (no Phase 0b).
3. Paul's final GO on the (refreshed) sheet; the operator key is copied
   then.

### Run-day refresh result (2026-11-26 22:56:44Z)

Rocky's nightly run stamped `2026-11-27` at 00:55:07 EET (22:55:07Z) and
wrote **dnf transaction #69** (2026-11-27 00:55, Upgrade, 2 packages:
`rsync-3.2.7-4.el9_9`, `libgs-9.54.0-20.el9_9.1`); `needs-restarting -r`
clean. The fill's own collector (`scratch/take-facts.sh`, with the newest
transaction instead of #68 and a separate refresh stamp so the 20:15Z
`STAMP` stays) then took read-only facts over the pinned host keys at
22:56:44Z. Against 20:15Z:
- **unchanged:** f3 boot `1794574308`; Rocky's bhyve PID `50982` and UTC
  lstart `Thu Nov 26 14:51:24 2026`; boot ID `99de7fb2…`; running kernel
  `5.14.0-741.27.1.el9_9` = grub default = `saved_entry`, `next_entry`
  empty (no kernel split); Enforcing, no `/.autorelabel`; python3.9,
  `authorized_keys` and the pod runtime (hashes and NEVRAs; `rocky-grub`
  and `rocky-pod-runtime` byte-identical); UEFI vars `4f63d71b…`,
  `rocky.conf`, vm-run, firmware, bridge members `re0 tap0`; f3
  `/usr/bin/ssh`; `fdescfs.ko` (39) and `fusefs.ko` (40) loaded with no
  fuse/fdesc mount (Phase 1a still runs); the r23 paths absent;
- **iormesh:** every iormesh VM (iom1, iom2, iom3) Stopped, `/dev/vmm`
  holds only `rocky`;
- swap 0 used; memory (a record only): free 968,372 pages (3.97 GB),
  inactive 1.71 GB, active 0.44 GB, wired 10.34 GB, ARC 9.04 GB;
- launch facts differ only in the collection time and the interface
  order (`tap0 wg0` swapped; only the set counts).

Refilled (a record fix): `sh/n1-gate.py` `FACTS_TOP` #69 of 2026-11-27
00:55 (`c354f923…`, was `710b4ae2…`), its hpin in `session.sh`, and
`session.sh` `3e92efb9…` (was `ed65bbf4…`) in the sheet's Phase 0 list and
[pins](pins.json) `fill_helpers`; `pins.json` `phase1_facts.run_day_refresh`
records the facts and their hashes. `n1-gate.py phase1` reports
`dnf_unchanged: true` for the 22:56:44Z record and false for the 20:15Z
one; `gate` on the new records gives STOP at 22:57Z (within 35 min of
00:55, expected), GO at 2026-11-27 12:00Z and STOP at 21:26Z. **Phase 0
passes** with the new pins (argv verify, `check-argv`, `bootstrap.py
check`, `check-retained9p.sh`, workstation ssh `d4c70b09…`, hpin, okpin,
the six-file `sha256sum -c`), and `git diff --stat f584de4 main` over the
tools is empty. The run's own Phase 1 is still taken on the run day.

## Inputs

| Input | Value |
|---|---|
| Candidate | r23 rebuild: manifest `21c49a55…`, package `/home/paul/tmp/claude/hfs-xz3-20261126-r23/package`, source checkout and repository baseline `21d8478` |
| New bytes | CGO daemon `43238fe9…`; kernel bzImage `049b1367…`, vmlinux `1aa83d82…`, build ID `2435216f…`, System.map `1a80593f…`; window artifacts `d58fedc4…`/`6d54858c…` |
| Same bytes as zq3-r22 | config `692ed3fa…`, Module.symvers `b68cf94a…`, the five modules and `modules.*`; exporter `8cadae40…`; the init helper `8c02b98f…` |
| Lab tools | re-pinned (`git diff 1ed87bf f584de4 -- tools/lab docs/lab/zq3-fdescfs-window internal/runtimeprep/testdata`): `controller.py` `167f8363…` and `rocky_kernel.py` `e99d91b8…` (admit only `21c49a55…`; `FD_ATTR`); the window `pins.json` (three artifact pins) and `build-record.json` |
| Unchanged tools | `bootstrap.py` `b8714120…`, `bootstrap_remote.py` `5155ff08…`, `fill_plan.py` `1a32774d…`, `guest.py` `746a67bc…`, `correlate.py` `d20f9753…`, `pod_fixture.py` `7c1e545f…`, `argv.py` `940d4787…`, `guest-lock-probe.py` `fd439897…`, `pod_workload.py` `c714370f…`, `observer.py` `2e1449dd…`, `observer_key.py` `cd7f38c2…`, `host.py` `738e1ca5…`, `host_snapshot.py` `b29ffed2…`, `fdescfs-window.py` `b00b19d1…`, `fdescfs-window-plan.py` `56ef5bb4…`, `runtime-guard.py` `fece96b3…`, native test guard `576d802a…`; every staged copy is re-hashed against main by `build-stage.sh` |
| Runtime spec | `internal/runtimeprep/testdata/zq3-spec.json` at `f584de4`, unchanged since `95f6d84` (exporter `8cadae40…`, `ControlTickets` 24). `make-spec.py` changes only the run ID and the operator key path |
| Facts | 2026-11-26 20:15:19Z (stamp `20261126T201519Z`), above |

## Prep compatibility with the candidate daemon

`hfs-runtime-prep` and the signer are built from `f584de4`; the daemon from
`21d8478`. `git diff 21d8478 f584de4 -- internal cmd go.mod go.sum` touches only
`internal/guestbridge/README.md` (no Go source), so the prep code is exactly
the candidate's (including the km4 `controlledruntime`/`viewcontrol` changes
both tools link).

The generated `generation.json`, `request-template.json` and
`volume-init.json` have exactly zq3-r22's key structure.
`runtime-config.json` differs only in its session-UUID map keys, and its
`Exporter.SHA256` is `8cadae40…`. `argv.py verify` and
`hfs-runtime-prep check-argv` pass. **No format change: no stop.**

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | **Rebuilt** at `f584de4` with go1.27.4-X:nodwarf5 (`-trimpath`, CGO, VCS stamps, unmodified private clone, `vcs.modified=false`); a second build of each, forced with `-a`, gave the same bytes | prep `63fa89c6…`, signer `d606a1cd…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from the committed spec (`make-spec.py`); the spec equals zq3-r22's renamed | spec `55b9eab5…`, request template `d1e03061…`, runtime config `80188fb7…`, volume-init `b30ac43a…`; volume `a9d8b116-…`; sessions hfs0 `a19f095f-…`, hfs1 `227cf32d-…` |
| 1. Argv | `argv.py generate` from the 20:15:19Z launch facts (sessions, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `d19bc902…`, args `78b9f97c…`, vars `4f63d71b…`, exporter `8cadae40…` |
| 2. Kernel plan | `rocky_kernel.py plan` at main, stock default `/boot/vmlinuz-5.14.0-741.27.1.el9_9.x86_64`. The r22 package `2c85b230…` is refused ("superseded xz3 candidate manifest; use the r23 rebuild (21c49a55)") | plan `dda355cf…`, build ID `2435216f…` |
| 3. Batch template | Derived from zq3-r22's by `derive-templates.py`: run ID, fill path, sessions, candidate `2c85b230…` → `21c49a55…`; `probe_sha256` (guest.py `746a67bc…`), ssh `d4c70b09…` and **`fd_attr` true** (plan and requests) kept and checked. be4 seconds 240/255 kept (checked); **T5 stays deferred** with the r9 reason. `check` refuses the template as designed; a zero-ID copy **passes**; the refusals are listed in [pins](pins.json) `controller_batch.synthetic_checks` (the r22, r21, first-r21 and r20 candidates superseded, zq3-r14 seconds, no deferral, `credentials` deferred, the three fd_attr shapes) | template `aa1ab40f…`, synthetic `7076ce2c…` |
| 4. Bootstrap plan | Derived from zq3-r22's: new bundle, kernel plan, `rocky_kernel.py`, prep, **daemon `c7b89648…` → `43238fe9…`** and the three fd_attr fields; `bootstrap_remote.py` and the ssh pins unchanged. The unfilled template refuses (`observer.ssh_sha256: SHA256 hex`); **F1 filled now** with `fill_plan.py bootstrap` from the 20:15Z f3 `sha256 -r /usr/bin/ssh` line (`6a9e5573…`, unchanged), then `bootstrap.py check` **pass** | template `535b0f84…`, filled plan `67c551f3…` |
| 5. Guard argv | zq3-r22's renamed, with **`--runtime-sha256 43238fe9…`** and `--disposable-validation` once, last; equal to `bootstrap.py`'s own `SshRemote.guard_argv()` (shell string and argv) and passes `runtime-guard.py`'s `parse_args` and `check_command` | `19de205a…` |
| 6. Pod template | zq3-r22's `/2` template, run ID renamed, runtime pins refilled from the facts (equal to zq3-r22's), `fixture_sha256` pl4's `7c1e545f…`. The committed `pod_fixture.py` accepts it and refuses the malformed variants and zq3-r20's fixture pin. **Expected rootfs manifest `34a71569…`** (an expectation, not a pin) | pod template `bfc3ac25…` |
| 7. Lock-probe templates | zq3-r22's, run ID renamed (150/240); the filled requests pass `validate_request`, case 151 refuses | m0 `302c6f48…`, m1-T8 `850f498c…` |
| 8. Staging | `build-stage.sh`: 17 f3 and 18 Rocky files, two deterministic tarballs (a second tar run gave the same hashes); a verification extract matched all 35 hashes; the f3 tarball holds only `0/0` entries, modes `0700`/`0500`/`0400`, no parent entry; extract deleted. Against zq3-r22: f3 daemon, `controlledruntime.test`, `fdescfs-namespace`, argv record, volume-init and runtime config; Rocky bzImage, System.map, plan and `rocky_kernel.py` changed | f3 tar `be6585e7…`, Rocky tar `29588b14…` |
| 9. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` at main (moved to the three r23 artifact pins) with `zq3-r1` → `zq3-r23`; equal to zq3-r22's renamed with the three new hashes. Synthetic fills (not pins): zq3-r8's real Phase 4 facts with the r23 hashes **pass**; zq3-r22's (= r21) hashes, the r20 or xc4 hashes **refuse** (`pinned artifact differs or missing`); facts with both modules loaded **refuse** (`fdescfs module present`): hence Phase 1a | r23 pins `22b2c45b…` |
| 10. Signing watcher | zq3-r22's with `D`, the comments and **the signer pin** changed; tested with a fake signer (wrong hash, command, action and repeat refused; both exact requests signed; the real pin refuses the fake binary) | `11182a80…` |
| 11. Process observer check | `sh/p4-observer.py` (**daemon pin `43238fe9…`**, staged path renamed); zq3-r22's real records, renamed and with `43238fe9…` substituted, **pass**; with their own daemon `c7b89648…` they are refused for the `sha256`, unrenamed also for the staged path; the `$NG` line (Rocky PID `50982`) passes the native guard's `parse_args` and `check_request` | `fa0d2c74…` |
| 12. N1 gate helper | `sh/n1-gate.py` with `FACTS_TOP` **#68 of 2026-11-26 00:55** at the fill, **#69 of 2026-11-27 00:55 since the run-day refresh** (`c354f923…`). Fixed-time tests: 12:30Z GO, the real 20:15Z records GO at 20:30Z and 21:25Z and STOP at 21:26Z, 21:20Z and 21:25Z GO, 21:30Z, 22:56Z and 23:10Z STOP, the next day with the old Phase 1 (synthetic or real) STOP, fresh GO, a stale stamp STOP, 01:00Z GO; `phase1` true for the real facts (#68), false for #67 and #69 (17 cases) | `710b4ae2…` (fill), `c354f923…` (refresh) |
| 13. Recall helper | `sh/recalls.py` unchanged in code; on zq3-r22's real OBSERVEs it reports 2 recalls (`RELEASE/VIEW_RELEASE`), advances 7, self_admissions 6; an existing output exits 2 | `acaaf186…` |
| 14. Other helpers | `p5-recheck.py` and `p5-estimate.py` pin Rocky PID 50982 / lstart 14:51:24 (zq3-r22's real Phase 5 inputs: the lstart check refuses 07:09:20 and passes with 14:51:24; the estimate reproduces zq3-r22's `p5-estimate.json` byte for byte, margin +0.15 GB); `p10-requests.py` the three r23 templates; `p9-ready.py` refuses zq3-r22's real READY run (r22 socket); guard budget unchanged and retested on zq3-r22's real journal (its Phase 9 and T1 checks reproduce, one second more with a whole-second `--now`; every latest start GO, one second later STOP; malformed journals `rc=2`, 22 cases); `p1-f3.sh` checks the r23 paths | [pins](pins.json) `fill_helpers` |
| 15. Run sheet | [run-sheet.md](run-sheet.md): Phases 0-12 with every kept rule, the zq3-r22 run notes (R22-1, R22-2), "New for zq3-r23", **Phase 8 block 2a** (bounded log poll, R22-2), the run-day refresh (header, Phase 0, Phase 1), BEGIN/END markers in all code blocks, the fd_attr expectations, the km4/6m4 and `ENOSPC`/`VIEW_RETIRE (-110)` finding rules, the stock-kernel rule (Phase 3, 12), the iormesh rule and a run log template. `session.sh` `ed65bbf4…` at the fill, `3e92efb9…` since the run-day refresh (hpin over 19 files) | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-26T00:00:00Z --format=gnu`.
The f3 tarball holds only `hfs-20261126-zq3r23-fdq` and
`hfs-validation-xz3/zq3-r23`, with **no entry for the existing parent**.

## Signing

As in zq3-r22: Paul delegated both signatures (A7) to Claude. In Phase 8 the
driver's stdout goes through `bin/sign_watch.py` (`11182a80…`). The
watcher runs the signer (`d606a1cd…`) only if:
- the printed command is exactly one of the two below;
- the request file's SHA256 equals the printed `request_sha256`;
- each command runs at most once.

Anything else is refused, and the driver then times out (fails closed):

```
/home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/bootstrap-run/work/signer-binding
```

The operator key goes to the bundle's path
`/home/paul/tmp/claude/hfs-xz3-20261126-r23/zq3-r23-fill/operator/operator-signing-key`
before the run (the empty `0700` directory exists; Paul's GO decides the
copy). Nothing in the fill read it.

## Run-time fills

As in the [zq3-r22 fill](../20261126-zq3-r22-filled-plan/README.md#run-time-fills),
with these values changed:

| Fill | When and source | Used by |
|---|---|---|
| N1 | Phase 1 (run day, fresh): `n1-gate.py phase1` expects **#68** (a newer transaction without drift means refreshing the helper, see above); the running kernel must equal the grub default **`741.27.1`** | Phase 1 Rocky gate, Phase 3 install, Phase 5 go/no-go, T7 |
| D1 | **D1-a, Phase 1a**: both modules are loaded (20:15Z: ids 39 and 40) | Phase 4 step 0 window plan fill |
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 20:15Z fact); Phase 4 step 0 rechecks it | bootstrap `observer.ssh_sha256` (plan `67c551f3…`) |
| F3 pod rootfs manifest digest (`PM`) | Phase 3: expect `34a71569…`; any other digest is explained before `build` | `build`, T7 request |
| F3a process observer record | Phase 4 step 1a; **pass required** for the NEW daemon `43238fe9…` | go/no-go before setup |
| F11 Rocky PID and lstart | **fixed** from the 20:15Z facts (`50982`, `Thu Nov 26 14:51:24 2026` UTC; boot ID `99de7fb2-c8e3-4b60-afef-7b11e77b4142` copied from the facts file) | native guard host profile, Phase 5 recheck |
| F21 swap | **No Phase 0b**: swap 0 at the facts; Phase 1 requires 0 used (else stop and ask Paul); Phase 6 requires swap unused | Phase 1, 5, 6 |
| F24 `hfs_fdattr` | **filled at fill time** in both plans (above); Phase 8's mountinfo rows and the batch fill check it at run time | Phase 8 mounts, batch fill, T1-T4, T9-T11 |

## Expectations

- **Through Phase 9 as in zq3-r22**, with `hfs_fdattr` on both mounts and
  Phase 9 within 5 minutes of READY (block 2a); Phase 4 step 1a passes for
  the new daemon `43238fe9…`; the exporter `8cadae40…` launches cleanly.
- **T1 should pass:** with km4 a child's `VIEW_RETIRE` refused while
  another process holds a lease comes back `BLOCKED`, so m0's cleanup
  worker moves on, releases the leases and offers the retirement again
  (re4), and the CREATE's 6m4 retire-and-retry no longer waits behind a
  30 s bound.
- **Then T2-T4, T6, T7, T9, T10, T11 and T8** within the guard budgets.
  None of T2-T11 has run for real. T3 adds the s64 peer-rename guard, and
  T9-T11 exercise hh4 ABANDON and dh4 for the first time. T5 stays
  deferred.
- **Record:**
  - the admission timings;
  - recalls by trigger, `advances` and `self_admissions`;
  - every guest re4/1h4/4h4/tj4/s64/hh4/6m4/km4 log line, in particular
    every `retired under charge pressure` line (what was freed, whether the
    charge was retried) and any `blocked by … failure` line with its errno;
  - any `ENOSPC` (R21-1/R22-1 again), `EACCES` (R20-1), `EAGAIN`,
    `EOPNOTSUPP`, `EKEYREJECTED`, `ESTALE` or `ETIMEDOUT` from a probe
    syscall, as a finding with its named syscall; a guest `blocked by
    uncertain control result on VIEW_RETIRE (-110)` line is R22-1 again.

Known risks, not fixed:
- **km4's limits:** `hfs_retire` retires only the oldest run of claims with
  one set of credentials per round; `hfs_retire_shared` does not offer view
  tickets; a family stays charged while another process of the mount holds
  a lease (offered again only through re4's kicks and the next cleanup
  packet); when nothing is retirable, a call still fails cleanly with
  `ENOSPC`;
- **ue4**;
- the rest of the s64 description path (T3's peer rename runs for the first
  time);
- a peer's observation in flight across a creating CALL, which still
  starts an all-domain CALL_CONFLICT recall.

**Guard budget.** Unchanged: the be4 worst cases (5025 s) do not fit the
3600 s guard, so a slow run STOPs early. Latest starts after the launch:

| Step | Latest start |
|---|---|
| Phase 9 | 52:00 |
| T1-T4 | 44:00 |
| T6 | 50:00 |
| T7 | 44:45 |
| T9 (with T10) | 38:30 |
| T10 | 48:30 |
| T8 | 46:00 |

## Phases 9-10 recheck

**Sheet dry run** (`dryrun/` in the private fill). The zq3-r22 harness ran
with the run ID renamed and the synthetic READY run's mountinfo showing
`hfs_fdattr`. It extracted 91 command lines of this sheet, from Phase 8
"After READY" to T8, plus the Phase 7 `k04` baseline and the two Phase 11
evidence lines. They ran in order under bash with:
- the real `fill_plan.py batch`, `check-retained9p.sh check`,
  `correlate.py`, `session.sh` and the `sh/` helpers;
- `controller.py` with faked guest dispatch;
- stub `f3`/`rocky` and a synthetic READY run with L = 2.

Every line passed (`HARNESS-DONE lines=91 L=2`). The filled batch plan
carries `fd_attr: true` in the plan and all four requests. The
case-failure block's direct OBSERVE line was run separately against the
same stub and `recalls.py` read its output. The helper dry run had 38
cases, all as expected.

**Real-record checks** (zq3-r22's):
- `p9-ready.py` refused zq3-r22's real READY run (its control socket is
  the r22 path);
- `fill_plan.py batch` refused zq3-r22's real run for the r23 plan pin;
- `p10-admission.py` on zq3-r22's real T1 log reports `admission_ms`
  10048.0 and the failure `PermissionError` errno 13 (the consequence of
  the blocked sessions after R22-1);
- `p11-fence.py` on zq3-r22's Phase 11 and case-failure evidence reports
  `fenced: false`.

## Findings and open questions

- **Q0 N1 and the run day.** The fill day's window had passed. On the run
  day: Phase 0 recheck and fresh Phase 1 facts after the nightly dnf has
  stamped; a kernel split (new stock default, Rocky still on `741.27.1`)
  **stops for Paul's maintenance-reboot approval** and a refill. Then
  start before 21:25 UTC and finish before 22:55 UTC.
- **Q1 T5 deferred.** The run cannot complete zq3 acceptance.
- **Q2 the guard budget.** Worst cases 5025 s against 3600 s.
- **Q3 first run:** the daemon `43238fe9…` (km4; Phase 4 step 1a required)
  and the kernel `2435216f…` (km4 log line). They have only builds, clean
  repros, the recall-model and guest suites and offline admission behind
  them.
- **Q4 `hfs_fdattr`.** A mountinfo row without the option stops the run
  at Phase 8 (`changed`, mount kept). An errno from an fd-based call is a
  finding against s64.
- **Q5 the iormesh VMs.** The window needs iom1-3 and any iormesh k3s VM
  stopped and `/dev/vmm` = `rocky`. At the facts all were stopped; iom1 and
  iom2 were started afterwards with the orchestrator's agreement (up to
  about 5 hours). The run-day refresh must find them all Stopped; the sheet
  never stops or starts them.
- **Q6 memory.** The 20:15Z numbers are a record only. Phase 5's estimate,
  the drain and the Phase 6 bar decide at run time.
- **Q7 UEFI vars drift.** The record pins `4f63d71b…`. Any Rocky boot
  before Phase 7 changes it, and `host.py setup` then refuses (a
  maintenance reboot on the run day means a refill).
- **Q8 Phase 1a.** Every D1-a so far unloaded both modules; a refusal stops
  before staging.
- **Q9 the workstation ssh.** `d4c70b09…`. An openssh update before the run
  makes the batch fill refuse after READY: the Phase 0 recheck catches it
  first.
- **Q10 the pod rootfs manifest.** Expected `34a71569…` (zq3-r21's and
  zq3-r22's real digest; no dnf run since #68). Any other digest is
  explained before `build`.
- **Q11 the stock kernel.** Rocky's restored kernel at Phase 12 must be the
  stock default (`5.14.0-741.27.1.el9_9` unless a refreshed refill names a
  newer one). The custom `6.1.190-hfs-xz3` is only ever a one-shot
  `next_entry` (Phase 3 never uses `--make-default`), and U2 removes it.
- **Q12 R22-2.** The operator must not wait for a task notification: block
  2a's bounded polls decide, and the after-READY lines start at once.

## Local verification

At `f584de4` (a clean worktree of main), all passing:
- retained9p: 440 tests;
- directlaunch: 26 tests;
- `tools/lab` top level: 249 tests (3 skipped);
- `tools/integration/artifacts`: 44 tests;
- `go test -race` for `internal/runtimeprep` and the two tool packages.

The full Go suite and its `-race` pass are the candidate's (its evidence
README). Since `21d8478` no Go source changed, and no Go code changed in
this commit.

`bash -n` and shellcheck are clean for `build-stage.sh` and (as `-s sh`, the
shell they are fed to) the `sh/` shell helpers. `session.sh` keeps its
intentional word splitting of `$O`,
loads only in bash, and its `hpin` and `okpin` pass.

**Phase 0 block** of the sheet, run for real (extracted between its
BEGIN/END markers): argv verify and `check-argv` pass, `bootstrap.py check`
passes, the synthetic batch check passes, the workstation ssh is
`d4c70b09…`, and `hpin`, `okpin` and the six `sha256sum -c` pins are OK.
