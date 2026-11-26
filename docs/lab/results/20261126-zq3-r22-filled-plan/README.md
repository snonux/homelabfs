# zq3-r22 filled run inputs (2026-11-26)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r21 fill](../20261125-zq3-r21-filled-plan/README.md) ·
[zq3-r21 record](../20261126-zq3-r21/README.md) ·
[r22 candidate](../../../../tools/integration/artifacts/evidence/xz3-20261126-r22/README.md) ·
[`hfs_fdattr`](../../../../tools/lab/retained9p/README.md#hfs_fdattr-mounts-bl4-s64)

These are the exact run inputs for `zq3-r22`, the twenty-second attempt of
zq3: Phases 0 and 1 of the [run plan](../../zq3-run-plan.md) only. They were
generated on the workstation with the committed tools at `main` `1ed87bf`
(the r22 candidate merge and its review round 1). They mirror the
[zq3-r21 fill](../20261125-zq3-r21-filled-plan/README.md), which ran on
2026-11-26: Phases 0 to 9 passed, R20-1 was fixed, the xf4 advance and vj4
self admissions worked, both mounts showed `hfs_fdattr`, and then T1's
`fchmod` of a second file failed `ENOSPC` and both sessions blocked with
`-28` (R21-1). Phase 11 and 12 were clean.

The changes are:
- the run ID (`zq3-r22`) and the native guard root
  `/var/tmp/hfs-20261126-zq3r22-fdq` (fill date 2026-11-26);
- the **r22 candidate `2c85b230…`** (source checkout `de9df35`, repository
  baseline `3081d31`). **Only the guest kernel is new** (build ID
  `c34dc6fb…`, 6m4: a call's reservation and completion package, and a view
  ticket's reservation, family credit and view package, are charged as one
  attempt; on `ENOSPC`/`EAGAIN` the guest retires every session of the
  provider and retries once). The daemon `c7b89648…`, the exporter
  `8cadae40…` and the window artifacts (`0129385e…`, `2157b861…`) are the
  second r21 rebuild's bytes. Phase 4 step 1a still runs for the daemon;
- **`hfs_fdattr` stays enabled** (decision below);
- `hfs-runtime-prep` and the signer rebuilt at `1ed87bf` with go1.27.4;
- a regenerated bundle (new sessions), argv record (the UEFI vars changed
  with zq3-r21's Phase 12 start), Rocky kernel plan, batch plan, bootstrap
  plan, staging tarballs and helper pins;
- re-pinned tools: `controller.py` and `rocky_kernel.py` (admit only
  `2c85b230…`; `f21450ec…` is refused as superseded). No other tool
  changed since the zq3-r21 fill;
- Rocky's new incarnation, from zq3-r21's Phase 12 start: PID `93782`,
  lstart `Thu Nov 26 07:09:20 2026` UTC, boot ID
  `c7fd367a-0eac-4514-954c-40be9d3a9804`, in `NG`, `p5-recheck.py` and
  `p5-estimate.py`;
- the f3 modules left loaded by zq3-r21 are now ids 38 (`fdescfs.ko`) and
  39 (`fusefs.ko`): **Phase 1a runs**.

Unchanged: the window pins (the committed `pins.json`), the pod template's
runtime pins (no dnf run since #68), both workstation and f3 ssh pins, the
spec, the init helper, the iormesh rule and every be4 bound.

Phase 1 ran for real, read-only, over the pinned host keys only (below).
Nothing else touched a host: no staging, no write on f3 or Rocky, no
kldload or kldunload, no VM lifecycle, no key enrolment, and the operator
private key was never read (the empty `0700` `operator/` directory exists).
**No authority follows from these files:** every class A step in the
[run sheet](run-sheet.md) still needs Paul's explicit final GO, including
the Phase 1a unload and T7 (A8). **zq3 acceptance stays incomplete by
design**, because T5 does not run.

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-xz3-20261126-r22/zq3-r22-fill` (`0700`, under the
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
`2c85b230…`, whose guest kernel, exporter and daemon all carry s64. In
zq3-r21 both mountinfo rows showed the option once, right after
`cache=mmap`, and T1's `fchown` and `fchmod` of `group` through the open
description returned 0. So **this run keeps it enabled**:
- **batch template:** `"fd_attr": true` at the top and in each of the four
  requests (release and step requests copy it);
- **bootstrap plan**, all three fields together (nl4):
  - `fd_attr_candidate_sha256` `2c85b230…`;
  - `fd_attr_candidate_manifest`
    `/home/paul/tmp/claude/hfs-xz3-20261126-r22/package/manifest.json`,
    read under that digest: its daemon is `runtime.sha256` and its exporter
    the runtime config's `Exporter.SHA256` (`8cadae40…`);
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
  - the r21 manifest `f21450ec…` (superseded);
  - zq3-r21's kernel plan (its bytes differ from `kernel.plan_sha256`);
  - a missing manifest file.
- `controller.py check` passes a zero-ID copy of the batch. It refuses:
  - `fd_attr` false;
  - a request without `fd_attr`;
  - requests with it under a plan without it.
- The sheet dry run's `fill_plan.py batch` fills `fd_attr: true`. On a
  synthetic READY run whose mountinfo rows lack `hfs_fdattr` it refuses:
  "bootstrap plan names fd_attr candidate 2c85b230… but its mounts do not
  show hfs_fdattr".

## Phase 1 facts (2026-11-26 11:48:16Z)

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
- `vm list`: `rocky` Running (PID 93782); `freebsd`, `hfs-test0..2` and
  **iom1, iom2, iom3 Stopped**; no iormesh k3s VM listed; `/dev/vmm` holds
  only `rocky`; `vm-public` members `tap0`, `re0`;
- swap 0 used;
- memory (a record only, not a headroom judgement; Rocky up 4.6 hours):

  | Item | Value |
  |---|---|
  | physmem | 16,879,837,184 |
  | free | 1,279,250 pages, 5.24 GB |
  | inactive | 1.68 GB |
  | active | 0.21 GB |
  | wired | 9.32 GB |
  | ARC | 8.26 GB (`c_min` 0.53 GB) |

- `fdescfs.ko` (id 38) and `fusefs.ko` (id 39) still loaded, left by
  zq3-r21, with no fuse/fdesc mount: **Phase 1a runs**;
- the r22 paths are absent (guard root `hfs-20261126-zq3r22-fdq`);
- the PoC share is present, zroot is ONLINE with 343G free, and
  `/usr/bin/ssh` is `6a9e5573…` (unchanged, F1 holds);
- launch facts:
  - **UEFI vars `0be7dec3…`** (new: zq3-r21's Phase 12 start changed them;
    `883a9566…` at the zq3-r21 refill);
  - `rocky.conf` `c101adf1…`, vm-run `43753bc8…` and firmware
    `6b48e967…`, all unchanged;
  - `tap100` is free (interfaces `re0 lo0 vm-public pflog0 vm-hfslab wg0
    tap0`).

**Rocky:**
- **runs the stock `5.14.0-741.27.1.el9_9.x86_64`**, which is also the
  grub default and `saved_entry`; `next_entry` is empty. Grub entries:
  7.0.3 elrepo, 6.1.190 elrepo, 741.27.1, 741.24.1, 741.22.1, rescue;
- PID `93782`, lstart `Thu Nov 26 07:09:20 2026` UTC, boot ID
  `c7fd367a-0eac-4514-954c-40be9d3a9804`, up 4:38; systemd `running`;
- Enforcing, no `/.autorelabel`, no `hfs-xz3` file in `/boot`, no
  `/etc/homelabfs`;
- `authorized_keys` `e08542dc…` (one line), python3.9 `729ac37f…`;
- the zq3-r1 to zq3-r21 leftovers under `/root` and `/var/tmp` (evidence),
  no zq3-r22 path.

**Rocky's upgrade:**
- clock `Europe/Sofia (EET, +0200)`, synchronized;
- stamp `2026-11-26` (00:59:03 EET); newest dnf transaction still **#68**
  (2026-11-26 00:55 EET, 29 packages: the stock kernel `741.27.1`,
  kernel-tools/-libs/-headers, ruby 3.0.7-171 and its gems, webkit2gtk3-jsc
  2.54.3; the 687.66.1 kernel removed);
- `needs-restarting -r` is clean; the hourly ticks only log the deferred
  reboot;
- the pod runtime equals zq3-r21's (podman `8d94a8fb…` `5.8.5-5.el9_9`,
  crun, conmon, seccomp), so **the pod template's pins are unchanged**.

The public [facts/](facts/) holds the f3 launch facts, the Phase 1 batch
and network/modules, and Rocky grub, pod runtime, upgrade, dnf #68 (with
`needs-restarting` and the boot list) and uptime. The Rocky Phase 1 batch,
the stderr files and the f3 ssh hash line stay in the private fill.

**N1 for 2026-11-26:** the next dnf run is 2026-11-27 00:55 EET =
**2026-11-26 22:55 UTC**:

| Bound | Time (UTC) |
|---|---|
| Latest Phase 5 start | **21:25** |
| No new case or step after | 22:15 |
| Fallback (observer removal and poweroff at once) | 22:35 |
| Guest poweroff before | 22:45 |

`n1-gate.py gate` on the facts printed GO at 11:53Z, and `phase1` reports
`dnf_unchanged: true` (#68). With the real Phase 1 records the gate gives
GO at 21:25Z and STOP at 21:26Z. If Rocky's nightly upgrade runs before
the window (a run on 2026-11-27 or later), the sheet's later-day path
applies: fresh Phase 1 facts, the gate recomputes the times, and any drift
(a new transaction that changes the kernel default or the pod runtime)
means a refill.

## Inputs

| Input | Value |
|---|---|
| Candidate | r22 rebuild: manifest `2c85b230…`, package `/home/paul/tmp/claude/hfs-xz3-20261126-r22/package`, source checkout `de9df35`, repository baseline `3081d31` |
| New bytes | kernel bzImage `f51aff35…`, vmlinux `95a7e7fe…`, build ID `c34dc6fb…`, System.map `c710fd4c…`, Module.symvers `b68cf94a…` |
| Same bytes as zq3-r21 | config `692ed3fa…`, the five modules and `modules.*`; daemon `c7b89648…`; exporter `8cadae40…`; window artifacts `0129385e…`/`2157b861…`; the init helper `8c02b98f…` |
| Lab tools | re-pinned (`git diff 95f6d84 1ed87bf -- tools/lab docs/lab/zq3-fdescfs-window internal/runtimeprep/testdata`): `controller.py` `4f33f9c9…` and `rocky_kernel.py` `67430094…` (admit only `2c85b230…`; `FD_ATTR`). The window `build-record.json` only gained the r22 "not rebuilt" note |
| Unchanged tools | `bootstrap.py` `b8714120…`, `bootstrap_remote.py` `5155ff08…`, `fill_plan.py` `1a32774d…`, `guest.py` `746a67bc…`, `correlate.py` `d20f9753…`, `pod_fixture.py` `7c1e545f…`, `argv.py` `940d4787…`, `guest-lock-probe.py` `fd439897…`, `pod_workload.py` `c714370f…`, `observer.py` `2e1449dd…`, `observer_key.py` `cd7f38c2…`, `host.py` `738e1ca5…`, `host_snapshot.py` `b29ffed2…`, `runtime-guard.py` `fece96b3…`, native test guard `576d802a…`; every staged copy is re-hashed against main by `build-stage.sh` |
| Runtime spec | `internal/runtimeprep/testdata/zq3-spec.json` at `1ed87bf`, unchanged since `95f6d84` (exporter `8cadae40…`, `ControlTickets` 24). `make-spec.py` changes only the run ID and the operator key path |
| Facts | 2026-11-26 11:48:16Z (stamp `20261126T114816Z`), above |

## Prep compatibility with the candidate daemon

`hfs-runtime-prep` and the signer are built from `1ed87bf`; the daemon from
`3081d31`. `git diff 3081d31 1ed87bf -- internal cmd go.mod go.sum` touches
only `internal/runtimeprep/testdata/zq3-spec.json` (the exporter pin) and
`internal/guestbridge/README.md`. So the prep code is the candidate's.

The generated `generation.json`, `request-template.json` and
`volume-init.json` have exactly zq3-r21's key structure.
`runtime-config.json` differs only in its session-UUID map keys, and its
`Exporter.SHA256` is `8cadae40…`. `argv.py verify` and
`hfs-runtime-prep check-argv` pass. **No format change: no stop.**

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | **Rebuilt** at `1ed87bf` with go1.27.4-X:nodwarf5 (`-trimpath`, CGO, VCS stamps, unmodified private clone, `vcs.modified=false`); a second build of each, forced with `-a`, gave the same bytes. No source changed since `95f6d84`; the VCS stamp changed the bytes | prep `e232319a…`, signer `4b8dd6d6…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from the committed spec (`make-spec.py`); the spec equals zq3-r21's renamed | spec `e2b2c33c…`, request template `e3691318…`, runtime config `9c69033b…`, volume-init `d43663e4…`; volume `afc5779b-…`; sessions hfs0 `80a032fa-…`, hfs1 `d23791a0-…` |
| 1. Argv | `argv.py generate` from the 11:48:16Z launch facts (sessions, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `b58144ba…`, args `c67329f0…`, vars `0be7dec3…`, exporter `8cadae40…` |
| 2. Kernel plan | `rocky_kernel.py plan` at main, stock default `/boot/vmlinuz-5.14.0-741.27.1.el9_9.x86_64`. The second r21 package `f21450ec…` is refused ("superseded xz3 candidate manifest; use the r22 rebuild (2c85b230)") | plan `d075b421…`, build ID `c34dc6fb…` |
| 3. Batch template | Derived from zq3-r21's by `derive-templates.py`: run ID, fill path, sessions, candidate `f21450ec…` → `2c85b230…`; `probe_sha256` (guest.py `746a67bc…`), ssh `d4c70b09…` and **`fd_attr` true** (plan and requests) kept and checked. be4 seconds 240/255 kept (checked); **T5 stays deferred** with the r9 reason. `check` refuses the template as designed; a zero-ID copy **passes**; the refusals are listed in [pins](pins.json) `controller_batch.synthetic_checks` (the r21, first-r21 and r20 candidates superseded, zq3-r14 seconds, no deferral, `credentials` deferred, the three fd_attr shapes) | template `8944f624…`, synthetic `de2dc6ed…` |
| 4. Bootstrap plan | Derived from zq3-r21's: new bundle, kernel plan, `rocky_kernel.py`, prep and the three fd_attr fields; daemon `c7b89648…`, `bootstrap_remote.py` and the ssh pins unchanged. The unfilled template refuses (`observer.ssh_sha256: SHA256 hex`); **F1 filled now** with `fill_plan.py bootstrap` from the 11:48Z f3 `sha256 -r /usr/bin/ssh` line (`6a9e5573…`, unchanged), then `bootstrap.py check` **pass** | template `47b4f421…`, filled plan `0eb9909d…` |
| 5. Guard argv | zq3-r21's renamed, with `--runtime-sha256 c7b89648…` and `--disposable-validation` once, last; equal to `bootstrap.py`'s own `SshRemote.guard_argv()` (shell string and argv) and passes `runtime-guard.py`'s `parse_args` and `check_command` | `f45f7b58…` |
| 6. Pod template | zq3-r21's `/2` template, run ID renamed, runtime pins refilled from the facts (equal to zq3-r21's: podman `8d94a8fb…`, `podman-6:5.8.5-5.el9_9.x86_64`), `fixture_sha256` pl4's `7c1e545f…`. The committed `pod_fixture.py` accepts it and refuses the malformed variants and zq3-r20's fixture pin. **Expected rootfs manifest `34a71569…`** (an expectation, not a pin) | pod template `21671789…` |
| 7. Lock-probe templates | zq3-r21's, run ID renamed (150/240); the filled requests pass `validate_request`, case 151 refuses | m0 `27ead6da…`, m1-T8 `d578073d…` |
| 8. Staging | `build-stage.sh`: 17 f3 and 18 Rocky files, two deterministic tarballs (a second tar run gave the same hashes); a verification extract matched all 35 hashes; the f3 tarball holds only `0/0` entries, modes `0700`/`0500`/`0400`, no parent entry; extract deleted. Against zq3-r21: f3 argv record, volume-init and runtime config; Rocky bzImage, System.map, plan and `rocky_kernel.py` changed | f3 tar `747abc9a…`, Rocky tar `5930831b…` |
| 9. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` at main (unchanged since r21) with `zq3-r1` → `zq3-r22`; equal to zq3-r21's renamed. Synthetic fills (not pins): zq3-r8's real Phase 4 facts with the r22 hashes **pass**; the r20 or xc4 hashes **refuse** (`pinned artifact differs or missing`); facts with both modules loaded **refuse** (`fdescfs module present`): hence Phase 1a | r22 pins `ea5eee05…` |
| 10. Signing watcher | zq3-r21's with `D`, the comments and **the signer pin** changed; tested with a fake signer (wrong hash, command, action and repeat refused; both exact requests signed; the real pin refuses the fake binary) | `fd5a4d25…` |
| 11. Process observer check | `sh/p4-observer.py` (daemon pin `c7b89648…` unchanged, staged path renamed); zq3-r21's real records, renamed, **pass**; with the r20 daemon substituted they are refused for the `sha256` only, unrenamed for the staged path; the `$NG` line (Rocky PID `93782`) passes the native guard's `parse_args` and `check_request` | `6f8cd684…` |
| 12. N1 gate helper | `sh/n1-gate.py` with `FACTS_TOP` still **#68 of 2026-11-26 00:55**. Fixed-time tests for 2026-11-26: 12:30Z GO (also with the real records), the real records GO at 21:25Z and STOP at 21:26Z, 21:20Z and 21:25Z GO, 21:30Z, 22:56Z and 23:10Z STOP, the next day with the old Phase 1 (synthetic or real) STOP, fresh GO, a stale stamp STOP, 01:00Z GO; `phase1` true for the real facts (#68), false for #67 and #69 (17 cases) | `b41653a8…` |
| 13. Recall helper | `sh/recalls.py` unchanged in code; on zq3-r21's real OBSERVEs it reports 2 recalls (`RELEASE/VIEW_RELEASE`), advances 8, self_admissions 8; an existing output exits 2 | `3f0894d7…` |
| 14. Other helpers | `p5-recheck.py` and `p5-estimate.py` pin Rocky PID 93782 / lstart 07:09:20 (zq3-r21's real Phase 5 inputs: the lstart check refuses 06:46:29 and passes with 07:09:20; the estimate reproduces zq3-r21's +0.14 GB margin); `p10-requests.py` the three r22 templates; `p9-ready.py` refuses zq3-r21's real READY run (r21 socket); guard budget unchanged and retested on zq3-r21's real journal (its Phase 9 and T1 checks reproduce, every latest start GO, one second later STOP; malformed journals `rc=2`, 22 cases); `p1-f3.sh` checks the r22 paths | [pins](pins.json) `fill_helpers` |
| 15. Run sheet | [run-sheet.md](run-sheet.md): Phases 0-12 with every kept rule, the zq3-r21 run notes, "New for zq3-r22", BEGIN/END markers in all code blocks, the fd_attr expectations (Phase 8 mountinfo, Phase 9 tuple, T3 peer rename), the 6m4 and `ENOSPC` finding rules, the stock-kernel rule (Phase 3, 12), the iormesh rule (Phase 1 and Phase 5 step 1, `/dev/vmm` = `rocky`) and a run log template. `session.sh` `2c252b74…` (hpin over 19 files) | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-26T00:00:00Z --format=gnu`.
The f3 tarball holds only `hfs-20261126-zq3r22-fdq` and
`hfs-validation-xz3/zq3-r22`, with **no entry for the existing parent**.

## Signing

As in zq3-r21: Paul delegated both signatures (A7) to Claude. In Phase 8 the
driver's stdout goes through `bin/sign_watch.py` (`fd5a4d25…`). The
watcher runs the signer (`4b8dd6d6…`) only if:
- the printed command is exactly one of the two below;
- the request file's SHA256 equals the printed `request_sha256`;
- each command runs at most once.

Anything else is refused, and the driver then times out (fails closed):

```
/home/paul/tmp/claude/hfs-xz3-20261126-r22/zq3-r22-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-xz3-20261126-r22/zq3-r22-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-xz3-20261126-r22/zq3-r22-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-xz3-20261126-r22/zq3-r22-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-xz3-20261126-r22/zq3-r22-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-xz3-20261126-r22/zq3-r22-fill/bootstrap-run/work/signer-binding
```

The operator key goes to the bundle's path
`/home/paul/tmp/claude/hfs-xz3-20261126-r22/zq3-r22-fill/operator/operator-signing-key`
before the run (the empty `0700` directory exists). Nothing in the fill
read it.

## Run-time fills

As in the [zq3-r21 fill](../20261125-zq3-r21-filled-plan/README.md#run-time-fills),
with these values changed:

| Fill | When and source | Used by |
|---|---|---|
| N1 | Phase 1: `n1-gate.py phase1` expects **#68**; the running kernel must equal the grub default **`741.27.1`** | Phase 1 Rocky gate, Phase 3 install, Phase 5 go/no-go, T7 |
| D1 | **D1-a, Phase 1a**: both modules are loaded (11:48Z: ids 38 and 39) | Phase 4 step 0 window plan fill |
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 11:48Z fact); Phase 4 step 0 rechecks it | bootstrap `observer.ssh_sha256` (plan `0eb9909d…`) |
| F3 pod rootfs manifest digest (`PM`) | Phase 3: expect `34a71569…`; any other digest is explained before `build` | `build`, T7 request |
| F3a process observer record | Phase 4 step 1a; **pass required** for `c7b89648…` (as in zq3-r21) | go/no-go before setup |
| F11 Rocky PID and lstart | **fixed** from the 11:48Z facts (`93782`, `Thu Nov 26 07:09:20 2026` UTC; boot ID `c7fd367a-0eac-4514-954c-40be9d3a9804` copied from the facts file) | native guard host profile, Phase 5 recheck |
| F21 swap | **No Phase 0b**: swap 0 at the facts; Phase 1 requires 0 used (else stop and ask Paul); Phase 6 requires swap unused | Phase 1, 5, 6 |
| F24 `hfs_fdattr` | **filled at fill time** in both plans (above); Phase 8's mountinfo rows and the batch fill check it at run time | Phase 8 mounts, batch fill, T1-T4, T9-T11 |

## Expectations

- **Through Phase 9 as in zq3-r21**, with `hfs_fdattr` on both mounts;
  Phase 4 step 1a passes for `c7b89648…` again; the exporter `8cadae40…`
  launches cleanly.
- **T1 should pass:** 6m4 retires every session of the provider and
  retries once when a call's or a view ticket's charge fails, so
  `fchmod(group-tail)` and the cleanup calls after it no longer fail
  `ENOSPC`. sj4 (R20-1), tj4/wj4 (advances) and vj4 (self admissions) held
  in zq3-r21 (advances 8, self_admissions 8, only the 2 bootstrap RELEASE
  recalls, admission 8.6 s).
- **Then T2-T4, T6, T7, T9, T10, T11 and T8** within the guard budgets.
  None of T2-T11 has run for real. T3 adds the s64 peer-rename guard, and
  T9-T11 exercise hh4 ABANDON and dh4 for the first time. T5 stays
  deferred.
- **Record:**
  - the admission timings;
  - recalls by trigger, `advances` and `self_admissions`;
  - every guest re4/1h4/4h4/tj4/s64/hh4/6m4 log line, in particular any
    `blocked by … failure` line with its errno;
  - any `ENOSPC` (R21-1 again), `EACCES` (R20-1), `EAGAIN`, `EOPNOTSUPP`,
    `EKEYREJECTED`, `ESTALE` or `ETIMEDOUT` from a probe syscall, as a
    finding with its named syscall.

Known risks, not fixed:
- **6m4's limits:** a retirement returns only the bytes of its own lane,
  and unretired view tickets are still not offered for retirement while no
  recall runs; when nothing on the lane is retirable, a call still fails
  cleanly with `ENOSPC`;
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

**Sheet dry run** (`dryrun/` in the private fill). The zq3-r21 harness ran
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

**Real-record checks** (zq3-r21's):
- `p9-ready.py` refused zq3-r21's real READY run (its control socket is
  the r21 path);
- `fill_plan.py batch` refused zq3-r21's real run for the r22 plan pin;
- `p10-admission.py` on zq3-r21's real T1 log reports `admission_ms`
  8620.6 and the failure `PermissionError` errno 13 (the consequence of the
  blocked sessions after R21-1's `ENOSPC`);
- `p11-fence.py` on zq3-r21's Phase 11 and case-failure evidence reports
  `fenced: false`.

## Findings and open questions

- **Q0 N1.** Start after the run day's upgrade has stamped and finish before
  22:55 UTC: latest Phase 5 start 21:25 UTC, no new case after 22:15 UTC,
  guest off before 22:45 UTC. The gate computes the dates; a later day
  needs fresh Phase 1 facts.
- **Q1 T5 deferred.** The run cannot complete zq3 acceptance.
- **Q2 the guard budget.** Worst cases 5025 s against 3600 s.
- **Q3 first run:** the `c34dc6fb` kernel (6m4). It has only a build, a
  clean repro, the controlled_received and controlled_provider suites and
  offline admission behind it.
- **Q4 `hfs_fdattr`.** A mountinfo row without the option stops the run
  at Phase 8 (`changed`, mount kept). An errno from an fd-based call is a
  finding against s64.
- **Q5 the iormesh VMs.** The window needs iom1-3 and any iormesh k3s VM
  stopped and `/dev/vmm` = `rocky`. At the facts all were stopped and no
  k3s VM was listed. The sheet never stops or starts them: the
  orchestrator schedules the window with the iormesh session and tells it
  afterwards that f3 is free.
- **Q6 memory.** The 11:48Z numbers are a record only. They differ from
  zq3-r21's (Rocky up 4.6 hours: active 0.21 GB, wired 9.32 GB, ARC
  8.26 GB). Phase 5's estimate, the drain and the Phase 6 bar decide at run
  time.
- **Q7 UEFI vars drift.** The record pins `0be7dec3…`. Any Rocky boot
  before Phase 7 changes it, and `host.py setup` then refuses.
- **Q8 Phase 1a.** Every D1-a so far unloaded both modules; a refusal stops
  before staging.
- **Q9 the workstation ssh.** `d4c70b09…`. An openssh update before the run
  makes the batch fill refuse after READY: the Phase 0 recheck catches it
  first.
- **Q10 the pod rootfs manifest.** Expected `34a71569…` (zq3-r21's real
  digest; no dnf run since). Any other digest is explained before `build`.
- **Q11 the stock kernel.** Rocky's restored kernel at Phase 12 must be the
  stock `5.14.0-741.27.1.el9_9`. The custom `6.1.190-hfs-xz3` is only ever
  a one-shot `next_entry` (Phase 3 never uses `--make-default`), and U2
  removes it. The sheet's Phase 3, 12 and Recovery lines say so.

## Local verification

At `1ed87bf` (a clean worktree of main), all passing:
- retained9p: 440 tests;
- directlaunch: 26 tests;
- `tools/lab` top level: 249 tests (3 skipped);
- `tools/integration/artifacts`: 44 tests;
- `go test -race` for `internal/runtimeprep` and the two tool packages.

The full Go suite and its `-race` pass are the candidate's (its evidence
README). Since `3081d31` only the spec testdata and READMEs changed among
the Go sources, and no Go code changed in this commit.

`bash -n` and shellcheck are clean for `build-stage.sh` and the `sh/`
shell helpers. `session.sh` keeps its intentional word splitting of `$O`,
loads only in bash, and its `hpin` and `okpin` pass.

**Phase 0 block** of the sheet, run for real: argv verify and `check-argv`
pass, `bootstrap.py check` passes, the synthetic batch check passes, the
workstation ssh is `d4c70b09…`, and `hpin`, `okpin` and the six
`sha256sum -c` pins are OK.
