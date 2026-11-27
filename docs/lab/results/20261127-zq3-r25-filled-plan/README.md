# zq3-r25 filled run inputs (2026-11-27)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r24 fill](../20261127-zq3-r24-filled-plan/README.md) ·
[zq3-r24 record](../20261127-zq3-r24/README.md) ·
[r25 candidate](../../../../tools/integration/artifacts/evidence/xz3-20261127-r25/README.md) ·
[`hfs_fdattr`](../../../../tools/lab/retained9p/README.md#hfs_fdattr-mounts-bl4-s64)

These are the exact run inputs for `zq3-r25`, the twenty-fifth attempt of
zq3: Phases 0 and 1 of the [run plan](../../zq3-run-plan.md) only. They were
generated on the workstation with the committed tools at `main` `1f43821`
(the r25 candidate merge). They mirror the
[zq3-r24 fill](../20261127-zq3-r24-filled-plan/README.md). zq3-r24 ran on
2026-11-27: Phases 0 to 9 passed, and then the 1024-group access child's
`open` of `group-tail` failed `ENOSPC` again, with m0's **control** lane
98% full (R24-1). Task gn4 proved the cause: 13 released but unretired
VIEW_RELEASE tickets held the lane, the km4 round freed only an
ordinary-lane request, and the failing charge was a release ticket charged
after its grant, which stranded a host lease and blocked every later
VIEW_RETIRE of m0 under the km4 daemon's domain-wide check. Phase 11 and 12
were clean.

**Run timing.** The facts are of **2026-11-27 14:52:14Z**, taken after that
day's nightly dnf run had stamped (00:55:07 EET, transaction #69). **The run
may happen today, 2026-11-27, without a refresh**, provided Phase 5 starts
no later than **21:25 UTC** (the N1 gate's latest start) and the run ends
before **tonight's 22:55 UTC dnf run** (cutoffs: no new case or step after
22:15 UTC, fallback 22:35 UTC, guest poweroff before 22:45 UTC). Phase 0's
recheck and the run's own Phase 1 still come first. **On any later day, a
run-day refresh comes first**, as zq3-r23's (`78397a0`), with Phase 2 no
earlier than 35 minutes after that day's 22:55 UTC dnf run: see
[Run-day refresh](#run-day-refresh-a-later-run-day-only).

The changes are:
- the run ID (`zq3-r25`) and the native guard root
  `/var/tmp/hfs-20261127-zq3r25-fdq` (fill date 2026-11-27);
- the **r25 candidate `a3eac923…`** (built from `e9ae706`, the gn4 merge,
  which is also the repository baseline; no
  `inputs/repository-changes.patch`):
  - **a NEW guest kernel** (build ID `8c70915f…`, bzImage `ee650e63…`,
    System.map `d1ec01e8…`): **gn4**. The lease's control release ticket
    is prepared before the VIEW_ACQUIRE is sent, so a full control lane
    refuses the acquisition before any host lease exists; a charge-pressure
    round also runs `hfs_view_retire` (the round line gains `view retire
    <errno>`); a retry that fails again is logged (`charge still failed
    after the retry: error <e>, <lane> lane used …`);
  - **a NEW CGO daemon** `43414d7f…` (gn4: `checkRetire` refuses a
    VIEW_RETIRE for an active lease only when the lease carries the
    retiring family's own credentials). It has not been checked on f3, so
    **Phase 4 step 1a must qualify it**; the guard's `--runtime-sha256`,
    the bootstrap `runtime.sha256` and `sh/p4-observer.py` pin it;
  - **new window artifacts** built with it from `e9ae706`: test binary
    `29e9e160…`, helper `2202e6b5…` (inspector = the daemon `43414d7f…`);
  - **reused byte for byte from `c9500232…`:** the exporter `8cadae40…`
    (the runtime config's `Exporter.SHA256` stays);
- **`hfs_fdattr` stays enabled** (decision below);
- `hfs-runtime-prep` and the signer rebuilt at `1f43821` with go1.27.4
  (they link `internal/viewcontrol` and `internal/controlledruntime`, which
  gn4 changed);
- a regenerated bundle (new sessions), argv record (the UEFI vars changed
  with zq3-r24's Phase 12 start), Rocky kernel plan, batch plan, bootstrap
  plan, staging tarballs, window pins and helper pins;
- re-pinned tools: `controller.py` `c54a803e…` and `rocky_kernel.py`
  `3e80d1f1…` (admit only `a3eac923…`; `c9500232…` is refused as
  superseded). `guest.py` is unchanged (`e436ae72…`, the batch
  `probe_sha256`). `git diff 87b9bf2 1f43821 -- tools/lab
  docs/lab/zq3-fdescfs-window internal/runtimeprep/testdata` changed
  nothing else that runs (the window `pins.json` and `build-record.json`,
  the window build recipe's default candidate, a README and tests);
- Rocky's new incarnation, from zq3-r24's Phase 12 start: PID `47896`,
  lstart `Fri Nov 27 06:55:27 2026` UTC, boot ID
  `971d3010-d7c6-4b0e-8855-5e0a9b5fb6d9`, in `NG`, `p5-recheck.py` and
  `p5-estimate.py`;
- the f3 modules left loaded by zq3-r24 are now ids 41 (`fdescfs.ko`) and
  42 (`fusefs.ko`): **Phase 1a runs**;
- `sh/n1-gate.py` still expects **#69** (2026-11-27 00:55), the fill's
  facts' newest transaction;
- **gn4 finding rules** (new): every `retired under charge pressure` line
  is recorded with its `view retire` errno, lane usage and outcome; `view
  retire 0` with no later failure is expected (gn4 at work); `view retire
  -11` is recorded and is a finding only if the charge then fails; a
  `charge still failed after the retry` line is always a finding (on the
  control lane during T1, R24-1 again). The view controller's
  `Leases`/`Tickets` around T1 are recorded (zq3-r24's failure OBSERVE:
  8 and 22). The 9n4 rules are kept;
- **the R23-2 wait rule is kept:** no Monitor, timer or task-notification
  wakeup for any wait, gate waits included; bounded foreground loops only
  (`timeout 540 sh -c 'until <check>; do sleep 5; done'`, repeated as
  needed; never `until ! pgrep -f …`). Phase 8 block 2a (R22-2) is kept.

Unchanged: the exporter, the pod template's runtime pins (no dnf run since
#69), both workstation and f3 ssh pins, the spec, the init helper, the
iormesh rule and every be4 bound.

Phase 1 ran for real, read-only, over the pinned host keys only (below).
Nothing else touched a host: no staging, no write on f3 or Rocky, no
kldload or kldunload, no VM lifecycle, no key enrolment, and the operator
private key was never read (the empty `0700` `operator/` directory exists).
**No authority follows from these files:** every class A step in the
[run sheet](run-sheet.md) still needs Paul's explicit final GO, including
the Phase 1a unload and T7 (A8). **zq3 acceptance stays incomplete by
design**, because T5 does not run.

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-xz3-20261127-r25/zq3-r25-fill` (`0700`, under the
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
`a3eac923…`, whose guest kernel, exporter and daemon all carry s64. In
zq3-r21 to zq3-r24 both mountinfo rows showed the option once, right after
`cache=mmap`, and T1's `fchown` and `fchmod` through the open description
returned 0 (in zq3-r24 for `group` and `group-tail`). So **this run keeps
it enabled**:
- **batch template:** `"fd_attr": true` at the top and in each of the four
  requests (release and step requests copy it);
- **bootstrap plan**, all three fields together (nl4):
  - `fd_attr_candidate_sha256` `a3eac923…`;
  - `fd_attr_candidate_manifest`
    `/home/paul/tmp/claude/hfs-xz3-20261127-r25/package/manifest.json`,
    read under that digest: its daemon is `runtime.sha256` (`43414d7f…`)
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
  - the r24 manifest `c9500232…` (superseded);
  - zq3-r24's kernel plan (its bytes differ from `kernel.plan_sha256`);
  - a missing manifest file;
  - the r23/r24 km4 daemon `43238fe9…` and the r21/r22 daemon `c7b89648…`
    as `runtime.sha256` (not the candidate's daemon).
- `controller.py check` passes a zero-ID copy of the batch. It refuses:
  - `fd_attr` false;
  - a request without `fd_attr`;
  - requests with it under a plan without it;
  - the r24, r23, r22, second r21, first r21 and r20 candidates
    (superseded).
- The sheet dry run's `fill_plan.py batch` fills `fd_attr: true`. On a
  synthetic READY run whose mountinfo rows lack `hfs_fdattr` it refuses:
  "bootstrap plan names fd_attr candidate a3eac923… but its mounts do not
  show hfs_fdattr".

## Phase 1 facts (2026-11-27 14:52:14Z)

Read-only, one bounded batch per host, stdout and stderr kept apart:
- `ssh -F /dev/null` with `StrictHostKeyChecking=yes`, no global
  known_hosts, `BatchMode`;
- f3 with `docs/lab/f3-host-known_hosts` (alias `f3`);
- Rocky through f3 with `docs/lab/rocky-known_hosts` (alias `rocky`).

The only `doas` commands were `vm list`, `ls /dev/vmm`, `test -e` and
`host.py facts` from stdin. The Rocky batch also covered `dnf history info
69`, `needs-restarting -r`, `systemctl is-system-running` and the boot list.

**f3:**
- boot `1794574308`, unchanged since Paul's 2026-11-13 reboot;
- `vm list`: `rocky` Running (PID 47896); `freebsd`, `hfs-test0..2` and
  **iom1, iom2, iom3 Stopped** (the iormesh session ran iom1 and iom2
  earlier today and reported them reset and stopped: verified); no
  iormesh k3s VM listed; `/dev/vmm` holds only `rocky`; `vm-public`
  members `tap0`, `re0`. The iormesh session messages before it starts
  one; the sheet never touches them;
- swap 0 used;
- memory (a record only, not a headroom judgement; Rocky up 7.9 hours):

  | Item | Value |
  |---|---|
  | physmem | 16,879,837,184 |
  | free | 1,024,022 pages, 4.19 GB |
  | inactive | 1.71 GB |
  | active | 0.22 GB |
  | wired | 10.33 GB |
  | ARC | 9.38 GB (`c_min` 0.53 GB) |

- `fdescfs.ko` (id 41) and `fusefs.ko` (id 42) still loaded, left by
  zq3-r24, with no fuse/fdesc mount: **Phase 1a runs**;
- the r25 paths are absent (guard root `hfs-20261127-zq3r25-fdq`);
- the PoC share is present, zroot is ONLINE with 341G free, and
  `/usr/bin/ssh` is `6a9e5573…` (unchanged, F1 holds);
- launch facts:
  - **UEFI vars `a0b33064…`** (new: zq3-r24's Phase 12 start changed them;
    `e32f7926…` at the zq3-r24 fill);
  - `rocky.conf` `c101adf1…`, vm-run `43753bc8…` and firmware
    `6b48e967…`, all unchanged;
  - `tap100` is free (interfaces `re0 lo0 vm-public pflog0 vm-hfslab wg0
    tap0`).

**Rocky:**
- **runs the stock `5.14.0-741.27.1.el9_9.x86_64`**, which is also the
  grub default and `saved_entry`; `next_entry` is empty: **no kernel
  split**. Grub entries: 7.0.3 elrepo, 6.1.190 elrepo, 741.27.1, 741.24.1,
  741.22.1, rescue (byte-identical to zq3-r24's facts);
- PID `47896`, lstart `Fri Nov 27 06:55:27 2026` UTC, boot ID
  `971d3010-d7c6-4b0e-8855-5e0a9b5fb6d9` (zq3-r24's Phase 12 incarnation,
  as expected), up 7:56; systemd `running`;
- Enforcing, no `/.autorelabel`, no `hfs-xz3` file in `/boot`, no
  `/etc/homelabfs`;
- `authorized_keys` `e08542dc…` (one line), python3.9 `729ac37f…`;
- the zq3-r1 to zq3-r24 leftovers under `/root` and `/var/tmp` (evidence),
  no zq3-r25 path.

**Rocky's upgrade:**
- clock `Europe/Sofia (EET, +0200)`, synchronized;
- stamp `2026-11-27` (00:55:07 EET); newest dnf transaction still **#69**
  (2026-11-27 00:55, Upgrade, 2 packages: `rsync-3.2.7-4.el9_9` and
  `libgs-9.54.0-20.el9_9.1`); the next run is at 22:55 UTC today;
- `needs-restarting -r` is clean; the hourly ticks only log the deferred
  reboot;
- the pod runtime equals zq3-r24's (podman `8d94a8fb…` `5.8.5-5.el9_9`,
  crun, conmon, seccomp; `rocky-pod-runtime` byte-identical), so **the pod
  template's pins are unchanged**.

The public [facts/](facts/) holds the f3 launch facts, the Phase 1 batch
and network/modules, and Rocky grub, pod runtime, upgrade, dnf #69 (with
`needs-restarting` and the boot list) and uptime. The Rocky Phase 1 batch,
the stderr files and the f3 ssh hash line stay in the private fill.

## Run-day refresh (a later run day only)

**N1:** the gate computes the times on the run day; the bounds are always:

| Bound | Time (UTC, run day) |
|---|---|
| Earliest Phase 2 (35 min after the 22:55 UTC dnf run of the evening before) | 23:30 the evening before |
| Latest Phase 5 start | **21:25** |
| No new case or step after | 22:15 |
| Fallback (observer removal and poweroff at once) | 22:35 |
| Guest poweroff before | 22:45 |

`n1-gate.py phase1` reports `dnf_unchanged: true` on the facts (#69), and
the fixed-time tests show the real 14:52Z records give GO at 15:00Z and
21:25Z, STOP at 21:26Z and STOP on any later day (fresh Phase 1 required).
**A run on 2026-11-27 needs no refresh.**

On a later run day, after that day's nightly dnf run has stamped, before
any class A step:
1. **Phase 0 block** of the sheet (pins, argv verify, `check-argv`,
   `bootstrap.py check`, workstation ssh) and `git diff --stat 1f43821
   <main>` over the tools (must be empty or tests/docs only).
2. **Fresh read-only Phase 1 facts** (the sheet's Phase 1 block, or the
   fill's collector), compared with the 14:52Z facts:
   - **Rocky's running kernel must equal the grub default, both stock.** If
     the nightly dnf installed a new stock kernel as the default while
     Rocky still runs `741.27.1` (the N1 kernel split): **STOP and report.**
     Paul must approve a Rocky maintenance reboot into the new default (as
     on 2026-11-26 06:46Z), and the reboot means a refill (kernel plan stock
     default, UEFI vars, argv record, bundle and sessions, Rocky PID/lstart
     in `NG` and the Phase 5 helpers, staging);
   - a new transaction (#70 or later) that changed nothing pinned: refresh
     `sh/n1-gate.py`'s expected transaction and the sheet's N1 lines (the
     gate does not read it, so this is a record fix). Changing
     `sh/n1-gate.py` also means updating its hpin in `session.sh`, then
     `session.sh`'s pin in the Phase 0 `sha256sum` list and in `pins.json`
     (hpin refuses a stale pin, so a missed step fails closed);
   - a changed pod runtime: refill the pod template;
   - any Rocky restart (new PID, boot ID or UEFI vars) or f3 reboot: refill
     (an f3 reboot also unloads both modules, so Phase 1a changes);
   - **every iormesh VM Stopped and `/dev/vmm` = `rocky`**; if one runs,
     the run waits and the orchestrator coordinates with the iormesh
     session;
   - swap 0 used (no Phase 0b).
3. Paul's final GO on the (refreshed) sheet; the operator key is copied
   then.

## Inputs

| Input | Value |
|---|---|
| Candidate | r25 rebuild: manifest `a3eac923…`, package `/home/paul/tmp/claude/hfs-xz3-20261127-r25/package`, source checkout and repository baseline `e9ae706` (no `inputs/repository-changes.patch`) |
| New bytes | kernel bzImage `ee650e63…`, vmlinux `c416af7e…`, build ID `8c70915f…`, System.map `d1ec01e8…`; CGO daemon `43414d7f…`; window artifacts `29e9e160…`/`2202e6b5…` |
| Same bytes as zq3-r24 | exporter `8cadae40…`; config `692ed3fa…`, Module.symvers `b68cf94a…`, the five modules and `modules.*`; the init helper `8c02b98f…` |
| Lab tools | re-pinned (`git diff 87b9bf2 1f43821 -- tools/lab docs/lab/zq3-fdescfs-window internal/runtimeprep/testdata`): `controller.py` `c54a803e…` and `rocky_kernel.py` `3e80d1f1…` (admit only `a3eac923…`; `FD_ATTR`); the window `pins.json` (the three artifact pins) |
| Unchanged tools | `guest.py` `e436ae72…`, `bootstrap.py` `b8714120…`, `bootstrap_remote.py` `5155ff08…`, `fill_plan.py` `1a32774d…`, `correlate.py` `d20f9753…`, `pod_fixture.py` `7c1e545f…`, `argv.py` `940d4787…`, `guest-lock-probe.py` `fd439897…`, `pod_workload.py` `c714370f…`, `observer.py` `2e1449dd…`, `observer_key.py` `cd7f38c2…`, `host.py` `738e1ca5…`, `host_snapshot.py` `b29ffed2…`, `fdescfs-window.py` `b00b19d1…`, `fdescfs-window-plan.py` `56ef5bb4…`, `runtime-guard.py` `fece96b3…`, native test guard `576d802a…`; every staged copy is re-hashed against main by `build-stage.sh` |
| Runtime spec | `internal/runtimeprep/testdata/zq3-spec.json` at `1f43821`, unchanged since `95f6d84` (exporter `8cadae40…`, `ControlTickets` 24). `make-spec.py` changes only the run ID and the operator key path |
| Facts | 2026-11-27 14:52:14Z (stamp `20261127T145214Z`), above |

## Prep compatibility with the candidate daemon

`hfs-runtime-prep` and the signer are built from `1f43821`; the daemon from
`e9ae706`. `git diff e9ae706 1f43821 -- internal cmd go.mod go.sum` touches
only `internal/guestbridge/README.md` (no Go source), so the prep code is
exactly the candidate daemon's (including the gn4
`controlledruntime`/`viewcontrol` changes both tools link).

The generated `generation.json`, `request-template.json` and
`volume-init.json` have exactly zq3-r24's key structure.
`runtime-config.json` differs only in its session-UUID map keys, and its
`Exporter.SHA256` is `8cadae40…`. `argv.py verify` and
`hfs-runtime-prep check-argv` pass. **No format change: no stop.**

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | **Rebuilt** at `1f43821` with go1.27.4-X:nodwarf5 (`-trimpath`, CGO, VCS stamps, unmodified private clone, `vcs.modified=false`); a second build of each, forced with `-a`, gave the same bytes | prep `12868ff6…`, signer `7315346c…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from the committed spec (`make-spec.py`); the spec equals zq3-r24's renamed | spec `8bbeee4b…`, request template `4653baea…`, runtime config `5f3bdb16…`, volume-init `f3cf6501…`; volume `acbaddf8-…`; sessions hfs0 `6b53dc33-…`, hfs1 `b9133ba8-…` |
| 1. Argv | `argv.py generate` from the 14:52:14Z launch facts (sessions, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `1e69471f…`, args `ed18e97c…`, vars `a0b33064…`, exporter `8cadae40…` |
| 2. Kernel plan | `rocky_kernel.py plan` at main, stock default `/boot/vmlinuz-5.14.0-741.27.1.el9_9.x86_64`. The r24 package `c9500232…` is refused ("superseded xz3 candidate manifest; use the r25 rebuild (a3eac923)") | plan `9dc132af…`, build ID `8c70915f…` |
| 3. Batch template | Derived from zq3-r24's by `derive-templates.py`: run ID, fill path, sessions, candidate `c9500232…` → `a3eac923…`; `probe_sha256` (guest.py `e436ae72…`), ssh `d4c70b09…` and **`fd_attr` true** (plan and requests) kept and checked. be4 seconds 240/255 kept (checked); **T5 stays deferred** with the r9 reason. `check` refuses the template as designed; a zero-ID copy **passes**; the refusals are listed in [pins](pins.json) `controller_batch.synthetic_checks` (the r24, r23, r22, r21, first-r21 and r20 candidates superseded, zq3-r14 seconds, no deferral, `credentials` deferred, the three fd_attr shapes) | template `e0e3ccdc…`, synthetic `76971f40…` |
| 4. Bootstrap plan | Derived from zq3-r24's: new bundle, kernel plan, `rocky_kernel.py`, prep and the three fd_attr fields; **`runtime.sha256` → `43414d7f…`** (checked against the package); `bootstrap_remote.py` and the ssh pins unchanged. The unfilled template refuses (`observer.ssh_sha256: SHA256 hex`); **F1 filled now** with `fill_plan.py bootstrap` from the 14:52Z f3 `sha256 -r /usr/bin/ssh` line (`6a9e5573…`, unchanged), then `bootstrap.py check` **pass** | template `bd9465d1…`, filled plan `f17994e3…` |
| 5. Guard argv | zq3-r24's renamed, with **`--runtime-sha256 43414d7f…`** and `--disposable-validation` once, last; equal to `bootstrap.py`'s own `SshRemote.guard_argv()` (shell string and argv) and passes `runtime-guard.py`'s `parse_args` and `check_command` | `d528095a…` |
| 6. Pod template | zq3-r24's `/2` template, run ID renamed, runtime pins refilled from the facts (equal to zq3-r24's), `fixture_sha256` pl4's `7c1e545f…`. The committed `pod_fixture.py` accepts it and refuses the malformed variants and zq3-r20's fixture pin. **Expected rootfs manifest `34a71569…`** (an expectation, not a pin) | pod template `0dc56c23…` |
| 7. Lock-probe templates | zq3-r24's, run ID renamed (150/240); the filled requests pass `validate_request`, case 151 refuses | m0 `4c05df92…`, m1-T8 `4825ba75…` |
| 8. Staging | `build-stage.sh`: 17 f3 and 18 Rocky files, two deterministic tarballs (a second tar run gave the same hashes); a verification extract matched all 35 hashes; the f3 tarball holds only `0/0` entries, modes `0700`/`0500`/`0400`, no parent entry; extract deleted. Against zq3-r24: f3 daemon, `controlledruntime.test`, `fdescfs-namespace`, argv record, volume-init and runtime config; Rocky bzImage, System.map, plan and `rocky_kernel.py` changed (the exporter is the same bytes) | f3 tar `4e611127…`, Rocky tar `76c4028a…` |
| 9. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` at main (the r25 rebuild moved its three artifact pins) with `zq3-r1` → `zq3-r25`; against zq3-r24's renamed exactly the three artifact pins differ, and they equal the r25 window build and package daemon. Synthetic fills (not pins): zq3-r8's real Phase 4 facts with the r25 hashes **pass**; the r24 (= r23), r22 (= r21), r20 or xc4 hashes **refuse** (`pinned artifact differs or missing`); facts with both modules loaded **refuse** (`fdescfs module present`): hence Phase 1a | r25 pins `2de31788…` |
| 10. Signing watcher | zq3-r24's with `D`, the comments and **the signer pin** changed; tested with a fake signer (wrong hash, command, action and repeat refused; both exact requests signed; the real pin refuses the fake binary) | `665219d7…` |
| 11. Process observer check | `sh/p4-observer.py` (**daemon pin `43414d7f…`**, staged path renamed); zq3-r24's real records (pid 42987), renamed and with `43414d7f…` substituted, **pass**; with their own daemon `43238fe9…` they are refused for the `sha256`, unrenamed for the staged path too; the `$NG` line (Rocky PID `47896`) passes the native guard's `parse_args` and `check_request` | `2d08e4ae…` |
| 12. N1 gate helper | `sh/n1-gate.py` with `FACTS_TOP` **#69 of 2026-11-27 00:55**. Fixed-time tests on 2026-11-27: 12:30Z GO, the real 14:52Z records GO at 15:00Z and 21:25Z and STOP at 21:26Z, 21:20Z and 21:25Z GO, 21:30Z, 22:56Z and 23:10Z STOP, the next day with the old Phase 1 (synthetic or real) STOP, fresh GO, a stale stamp STOP, 01:00Z GO; `phase1` true for the real facts (#69), false for #68 and a synthetic #70 (17 cases) | `bab60184…` |
| 13. Recall helper | `sh/recalls.py` unchanged in code; on zq3-r24's real OBSERVEs it reports 2 recalls (`RELEASE/VIEW_RELEASE`), advances 9, self_admissions 10; an existing output exits 2 | `572fc5d2…` |
| 14. Other helpers | `p5-recheck.py` and `p5-estimate.py` pin Rocky PID 47896 / lstart 06:55:27 (zq3-r24's real Phase 5 inputs: the lstart check refuses 00:25:44 and passes with 06:55:27; the estimate reproduces zq3-r24's `p5-estimate.json` byte for byte, margin +0.17 GB); `p10-requests.py` the three r25 templates; `p9-ready.py` refuses zq3-r24's real READY run (r24 socket); guard budget unchanged and retested on zq3-r24's real journal (its Phase 9 and T1 checks reproduce, 3490 s and 3465 s; every latest start GO, one second later STOP; malformed journals `rc=2`, 23 cases); `p1-f3.sh` checks the r25 paths | [pins](pins.json) `fill_helpers` |
| 15. Run sheet | [run-sheet.md](run-sheet.md): Phases 0-12 with every kept rule, the zq3-r24 run notes (R24-1, R24-2), "New for zq3-r25", the **R23-2 wait rule** (kept: never a Monitor, timer or notification wakeup; bounded foreground loops only), **Phase 8 block 2a** (kept), the run timing (header, N1, Phase 0, Phase 1), BEGIN/END markers in all code blocks, the fd_attr expectations, Phase 4 step 1a required for the new daemon, the **gn4** finding rules (`view retire <errno>`; `charge still failed after the retry` always a finding; the view controller's `Leases`/`Tickets` around T1), the 9n4 rules kept, the stock-kernel rule (Phase 3, 12), the iormesh rule and a run log template. `session.sh` `9eba6624…` (hpin over 19 files) | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-27T00:00:00Z --format=gnu`.
The f3 tarball holds only `hfs-20261127-zq3r25-fdq` and
`hfs-validation-xz3/zq3-r25`, with **no entry for the existing parent**.

## Signing

As in zq3-r24: Paul delegated both signatures (A7) to Claude. In Phase 8 the
driver's stdout goes through `bin/sign_watch.py` (`665219d7…`). The
watcher runs the signer (`7315346c…`) only if:
- the printed command is exactly one of the two below;
- the request file's SHA256 equals the printed `request_sha256`;
- each command runs at most once.

Anything else is refused, and the driver then times out (fails closed):

```
/home/paul/tmp/claude/hfs-xz3-20261127-r25/zq3-r25-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-xz3-20261127-r25/zq3-r25-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-xz3-20261127-r25/zq3-r25-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-xz3-20261127-r25/zq3-r25-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-xz3-20261127-r25/zq3-r25-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-xz3-20261127-r25/zq3-r25-fill/bootstrap-run/work/signer-binding
```

The operator key goes to the bundle's path
`/home/paul/tmp/claude/hfs-xz3-20261127-r25/zq3-r25-fill/operator/operator-signing-key`
before the run (the empty `0700` directory exists; Paul's GO decides the
copy). Nothing in the fill read it.

## Run-time fills

As in the [zq3-r24 fill](../20261127-zq3-r24-filled-plan/README.md#run-time-fills),
with these values changed:

| Fill | When and source | Used by |
|---|---|---|
| N1 | Phase 1 (today, or fresh on a later day): `n1-gate.py phase1` expects **#69** (a newer transaction without drift means refreshing the helper, see above); the running kernel must equal the grub default **`741.27.1`** | Phase 1 Rocky gate, Phase 3 install, Phase 5 go/no-go, T7 |
| D1 | **D1-a, Phase 1a**: both modules are loaded (14:52Z: ids 41 and 42) | Phase 4 step 0 window plan fill |
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 14:52Z fact); Phase 4 step 0 rechecks it | bootstrap `observer.ssh_sha256` (plan `f17994e3…`) |
| F3 pod rootfs manifest digest (`PM`) | Phase 3: expect `34a71569…`; any other digest is explained before `build` | `build`, T7 request |
| F3a process observer record | Phase 4 step 1a; **pass required** for the NEW daemon `43414d7f…` (not yet checked on f3) | go/no-go before setup |
| F11 Rocky PID and lstart | **fixed** from the 14:52Z facts (`47896`, `Fri Nov 27 06:55:27 2026` UTC; boot ID `971d3010-d7c6-4b0e-8855-5e0a9b5fb6d9` copied from the facts file) | native guard host profile, Phase 5 recheck |
| F21 swap | **No Phase 0b**: swap 0 at the facts; Phase 1 requires 0 used (else stop and ask Paul); Phase 6 requires swap unused | Phase 1, 5, 6 |
| F24 `hfs_fdattr` | **filled at fill time** in both plans (above); Phase 8's mountinfo rows and the batch fill check it at run time | Phase 8 mounts, batch fill, T1-T4, T9-T11 |

## Expectations

- **Through Phase 9 as in zq3-r24**, with `hfs_fdattr` on both mounts and
  Phase 9 within 5 minutes of READY (block 2a); **Phase 4 step 1a passes
  for the new daemon `43414d7f…`**; the exporter `8cadae40…` launches
  cleanly.
- **T1 should pass:** with gn4 every lease's release ticket is charged
  before its VIEW_ACQUIRE, a charge-pressure round also retires released
  view tickets, and the daemon admits a VIEW_RETIRE while another
  credential set holds a lease, so m0's control lane no longer fills with
  released tickets and the 1024-group access child reads `group-tail`. The
  view controller's `Leases`/`Tickets` after T1 should not stay at
  zq3-r24's 8/22.
- **Then T2-T4, T6, T7, T9, T10, T11 and T8** within the guard budgets.
  None of T2-T11 has run for real. T3 adds the s64 peer-rename guard, and
  T9-T11 exercise hh4 ABANDON and dh4 for the first time. T5 stays
  deferred.
- **Record:**
  - the admission timings;
  - recalls by trigger, `advances` and `self_admissions`;
  - the view controller's `Leases`/`Tickets` in the OBSERVEs around T1;
  - every guest re4/1h4/4h4/tj4/s64/hh4/6m4/km4/9n4/gn4 log line, in
    particular every `retired under charge pressure` line with its `view
    retire <errno>` and lane usage (`<lane> lane used X of Y bytes, S of L
    slots; <outcome>`), and every `charge still failed after the retry:
    error <e>, <lane> lane used …; the charge fails` line; and any
    `blocked by … failure` line with its errno;
  - any `ENOSPC` (R21-1/R22-1/R23-1/R24-1 again), `EACCES` (R20-1),
    `EAGAIN`, `EOPNOTSUPP`, `EKEYREJECTED`, `ESTALE` or `ETIMEDOUT` from a
    probe syscall, as a finding with its named syscall (for a forked child
    the reply's `"syscall"`); a guest `blocked by uncertain control result
    on VIEW_RETIRE (-110)` line is R22-1 again.
- **What the gn4 lines mean for T1:**
  - `view retire 0` (a family floor advanced, so the round returned
    control-lane bytes) followed by a charge that succeeds: gn4 at work,
    expected, recorded, never a finding on its own;
  - `view retire -11` (nothing retirable, or every VIEW_RETIRE BLOCKED):
    recorded; a finding only if the charge then fails;
  - `charge still failed after the retry …; the charge fails`: always a
    finding. On the control lane during T1's access children it is
    **R24-1 again** (against gn4: record the line, the preceding round
    line and the view controller's `Leases`/`Tickets`); on the ordinary
    lane it is R21-1/R23-1 again;
  - the 9n4 outcomes keep their meaning: `a concurrent round freed
    requests, retrying the charge once` expected, `nothing freed, the
    charge fails` a finding.

Known risks, not fixed:
- **gn4's limits:** `hfs_view_retire` runs on the caller's session only; a
  VIEW_RETIRE for a family whose own credentials still hold an active
  lease is still refused, and an open dirty reservation still refuses
  domain-wide; the 9n4 limits stay (one spurious retry from the global
  generation; `hfs_retire` frees only the oldest run of one credential
  set per round). When nothing is retirable, a call still fails cleanly
  with `ENOSPC`;
- **ue4's other half** (retirement during a recall);
- **the daemon's first f3 run** (Phase 4 step 1a is its first check there);
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

**Sheet dry run** (`dryrun/` in the private fill). The zq3-r24 harness ran
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

**Real-record checks** (zq3-r24's):
- `p9-ready.py` refused zq3-r24's real READY run (its control socket is
  the r24 path);
- `fill_plan.py batch` refused zq3-r24's real run for the r25 plan pin;
- `p10-admission.py` on zq3-r24's real T1 log reports `admission_ms`
  8257.9 and the failure `Refused` "all 1024 groups including final entry
  must survive (child open errno 28)" (`errno` null: the parent made no
  failing syscall; the child's `open` is named, R24-1);
- `p11-fence.py` on zq3-r24's Phase 11 and case-failure evidence reports
  `fenced: false`.

## Findings and open questions

- **Q0 N1 and the run day.** The facts were taken after the fill day's
  nightly dnf: a run today needs no refresh if Phase 5 starts by 21:25 UTC
  and the run ends before 22:55 UTC. On a later day: Phase 0 recheck and
  fresh Phase 1 facts after that day's nightly dnf has stamped, Phase 2 no
  earlier than 35 minutes after it; a kernel split (new stock default,
  Rocky still on `741.27.1`) **stops for Paul's maintenance-reboot
  approval** and a refill.
- **Q1 T5 deferred.** The run cannot complete zq3 acceptance.
- **Q2 the guard budget.** Worst cases 5025 s against 3600 s.
- **Q3 first run:** the kernel `8c70915f…` and the daemon `43414d7f…`
  (gn4). They have only builds, clean repros, the guest suites
  (`controlled_received --scenario both`, 21 scenarios), the Go suites and
  offline admission behind them. The repository's `view --pressure`
  harness does not compile since gn4 (a stale `hfs_lease_own` fixture,
  fixed on main by gp4 `e5362c9`); a private copy passed. The daemon must
  pass Phase 4 step 1a before anything else uses it.
- **Q4 `hfs_fdattr`.** A mountinfo row without the option stops the run
  at Phase 8 (`changed`, mount kept). An errno from an fd-based call is a
  finding against s64.
- **Q5 the iormesh VMs.** The window needs iom1-3 and any iormesh k3s VM
  stopped and `/dev/vmm` = `rocky`. At the facts all were stopped (the
  iormesh session had run iom1 and iom2 earlier today); the iormesh
  session messages before it starts one. The sheet never stops or starts
  them.
- **Q6 memory.** The 14:52Z numbers are a record only. Phase 5's estimate,
  the drain and the Phase 6 bar decide at run time.
- **Q7 UEFI vars drift.** The record pins `a0b33064…`. Any Rocky boot
  before Phase 7 changes it, and `host.py setup` then refuses (a
  maintenance reboot on the run day means a refill).
- **Q8 Phase 1a.** Every D1-a so far unloaded both modules; a refusal stops
  before staging.
- **Q9 the workstation ssh.** `d4c70b09…`. An openssh update before the run
  makes the batch fill refuse after READY: the Phase 0 recheck catches it
  first.
- **Q10 the pod rootfs manifest.** Expected `34a71569…` (zq3-r15's to
  zq3-r24's real digest; no dnf run since #69). Any other digest is
  explained before `build`.
- **Q11 the stock kernel.** Rocky's restored kernel at Phase 12 must be the
  stock default (`5.14.0-741.27.1.el9_9` unless a refreshed refill names a
  newer one). The custom `6.1.190-hfs-xz3` is only ever a one-shot
  `next_entry` (Phase 3 never uses `--make-default`), and U2 removes it.
- **Q12 R23-2.** The operator never arms a Monitor, timer or notification
  wakeup for a wait: bounded foreground loops decide (an N1 earliest time
  is waited out with repeated `timeout 540` loops on the clock), and block
  2a's polls start the after-READY lines at once. zq3-r24 kept to it.
- **Q13 gn4 outcomes.** `view retire 0` is gn4 at work and is recorded,
  never a finding on its own; `view retire -11` is a finding only with a
  failed charge; `charge still failed after the retry` (with its lane
  usage) is always a finding, on the control lane during T1 R24-1 again.

## Local verification

At `1f43821` (a clean worktree of main), all passing:
- retained9p: 445 tests;
- directlaunch: 26 tests;
- `tools/lab` top level: 249 tests (3 skipped);
- `tools/integration/artifacts`: 44 tests;
- `go test -race` for `internal/runtimeprep` and the two tool packages.

The full Go suite and its `-race` pass are the candidate's (its evidence
README). Since `e9ae706` no Go source changed, and no Go code changed in
this commit.

`bash -n` and shellcheck are clean for `build-stage.sh` and (as `-s sh`, the
shell they are fed to) the `sh/` shell helpers. `session.sh` keeps its
intentional word splitting of `$O`,
loads only in bash, and its `hpin` and `okpin` pass.

**Phase 0 block** of the sheet, run for real (extracted between its
BEGIN/END markers): argv verify and `check-argv` pass, `bootstrap.py check`
passes, the synthetic batch check passes, the workstation ssh is
`d4c70b09…`, and `hpin`, `okpin` and the six `sha256sum -c` pins are OK.
