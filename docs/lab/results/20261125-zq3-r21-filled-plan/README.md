# zq3-r21 filled run inputs (2026-11-25)

[Run sheet](run-sheet.md) · [Pins](pins.json) · [Phase 1 facts](facts/) ·
[Run plan](../../zq3-run-plan.md) · [zq3-r20 fill](../20261122-zq3-r20-filled-plan/README.md) ·
[zq3-r20 record](../20261122-zq3-r20/README.md) ·
[r21 candidate](../../../../tools/integration/artifacts/evidence/xz3-20261125-r21/README.md) ·
[`hfs_fdattr`](../../../../tools/lab/retained9p/README.md#hfs_fdattr-mounts-bl4-s64)

These are the exact run inputs for `zq3-r21`, the twenty-first attempt of
zq3: Phases 0 and 1 of the [run plan](../../zq3-run-plan.md) only. They were
generated on the workstation with the committed tools at `main` `95f6d84`
(the merge of the second r21 candidate). They mirror the
[zq3-r20 fill](../20261122-zq3-r20-filled-plan/README.md), which ran on
2026-11-22: Phases 0 to 9 passed, 1h4 worked, then T1 failed with R20-1
(the host required search permission on every ancestor, so a non-owner
below the 0700 root got `EACCES`), and the guest never sent the xf4
advance. Phase 11 and 12 were clean.

The changes are:
- the run ID (`zq3-r21`) and the native guard root
  `/var/tmp/hfs-20261125-zq3r21-fdq` (fill date 2026-11-25);
- the **second r21 candidate `f21450ec…`** (source `3081d31`, Fedora 45
  toolchain), with three new parts:
  - the guest kernel (build ID `bd4fa753…`; s64, hh4, dh4, 5f4, kn1, wj4
    item 1, tj4);
  - the go1.27.4 daemon `c7b89648…` (vj4, sj4, dh4, s64 host, hh4 host;
    **Phase 4 step 1a is required for it**);
  - the native exporter `8cadae40…` (s64 `_args`, hh4 ABANDON; never run
    before).

  The window artifacts are new too (`0129385e…`, `2157b861…`);
- **`hfs_fdattr` is enabled** (decision below);
- `hfs-runtime-prep` and the signer rebuilt at `95f6d84` with go1.27.4;
- a regenerated bundle, argv record, Rocky kernel plan, batch plan and
  bootstrap plan;
- re-pinned tools: `controller.py`, `rocky_kernel.py`, `bootstrap.py`,
  `bootstrap_remote.py`, `fill_plan.py`, `guest.py`, `correlate.py`,
  `pod_fixture.py` (pl4) and `argv.py`;
- two facts changed outside the candidate and are refilled from the facts:
  - the workstation `/usr/bin/ssh` (Fedora 45, `d4c70b09…`);
  - Rocky's podman (dnf #67, `5.8.5-5.el9_9`);
- the iormesh VMs: another session runs its own el9 VMs on f3, so the run
  starts only while they are all stopped, and the sheet never stops or
  starts one.

Phase 1 ran for real, read-only, over the pinned host keys only (below).
Nothing else touched a host: no staging, no write on f3 or Rocky, no
kldload, no VM lifecycle, no key enrolment, and the operator private key
was never read. **No authority follows from these files:** every class A
step in the [run sheet](run-sheet.md) still needs Paul's explicit final GO,
including the Phase 1a unload and T7 (A8). **zq3 acceptance stays
incomplete by design**, because T5 does not run.

The generated inputs live in the private directory
`/home/paul/tmp/claude/hfs-xz3-20261125-r21/zq3-r21-fill` (`0700`, under the
candidate's run root). This public directory holds only public hashes,
paths, argv and the read-only facts. These stay private and are never
committed:
- the per-session keys;
- the runtime config, which holds host receipt private keys;
- the staging tarballs that contain it.

An older private directory, `/home/paul/tmp/claude/hfs-zq3-r21-fill`, holds a
partial fill for the superseded first r21 rebuild (`1428d4db…`, never run).
Nothing here uses it.

## The `hfs_fdattr` decision

The retained9p README's
[`hfs_fdattr` section](../../../../tools/lab/retained9p/README.md#hfs_fdattr-mounts-bl4-s64)
sets the rule: the option is never implied, a plan asks for it, and only a
candidate in `controller.FD_ATTR` may. `FD_ATTR` holds exactly
`f21450ec…`, whose guest kernel, exporter and daemon all carry s64. Neither
the run plan (Phase 8 step 5) nor the README names any other
precondition. So **this run enables it** (it is the point of the s64 work):
- **batch template:** `"fd_attr": true` at the top and in each of the four
  requests (release and step requests copy it);
- **bootstrap plan**, all three fields together (nl4):
  - `fd_attr_candidate_sha256` `f21450ec…`;
  - `fd_attr_candidate_manifest`
    `/home/paul/tmp/claude/hfs-xz3-20261125-r21/package/manifest.json`,
    read under that digest: its daemon is `runtime.sha256` and its exporter
    the runtime config's `Exporter.SHA256`;
  - `fd_attr_kernel_plan`, this fill's `kernel/rocky-kernel-plan.json`,
    read under `kernel.plan_sha256`; its `manifest_sha256` is the same
    candidate;
- **mounts:** `cache=mmap,hfs_fdattr`. Each mountinfo row must show
  `hfs_fdattr` once, right after `cache=mmap`;
- **T3** adds the VM-only s64 peer-rename guard: `stat` of the old name
  through m0 must fail `ENOENT`.

Checked locally:
- `bootstrap.py check` passes the filled plan. It refuses:
  - a plan without `fd_attr_kernel_plan` (closed keys);
  - the r20 manifest (superseded);
  - zq3-r20's kernel plan (its bytes differ from `kernel.plan_sha256`);
  - a missing manifest file.
- `controller.py check` passes a zero-ID copy of the batch. It refuses:
  - `fd_attr` false;
  - a request without `fd_attr`;
  - requests with it under a plan without it.
- The sheet dry run's `fill_plan.py batch` fills `fd_attr: true`. On a
  synthetic READY run whose mountinfo rows lack `hfs_fdattr` it refuses:
  "bootstrap plan names fd_attr candidate f21450ec… but its mounts do not
  show hfs_fdattr".

## Phase 1 facts (2026-11-25 09:38:05Z)

Read-only, one bounded batch per host, stdout and stderr kept apart:
- `ssh -F /dev/null` with `StrictHostKeyChecking=yes`, no global
  known_hosts, `BatchMode`;
- f3 with `docs/lab/f3-host-known_hosts` (alias `f3`);
- Rocky through f3 with `docs/lab/rocky-known_hosts` (alias `rocky`).

The only `doas` commands were `vm list`, `ls /dev/vmm`, `test -e` and
`host.py facts` from stdin. A follow-up read
(`dnf history info 67`, `needs-restarting -r`) was added after the first
batch showed a new transaction.

**f3:**
- boot `1794574308`, unchanged since Paul's 2026-11-13 reboot;
- `vm list`: `rocky` Running (PID 40425); `freebsd`, `hfs-test0..2` and
  **iom1, iom2, iom3 Stopped**; no iormesh k3s VM listed; `/dev/vmm`
  holds only `rocky`;
- swap 0 used;
- memory (a record only, not a headroom judgement, since the iormesh VMs
  come and go):

  | Item | Value |
  |---|---|
  | physmem | 16,879,837,184 |
  | free | 1,240,779 pages, 5.08 GB |
  | inactive | 2.67 GB |
  | active | 0.51 GB |
  | wired | 8.19 GB |
  | ARC | 7.22 GB (`c_min` 0.53 GB) |

- `fdescfs.ko` (id 37) and `fusefs.ko` (id 38) loaded, left by zq3-r20,
  with no fuse/fdesc mount: **Phase 1a runs**;
- the r21 paths are absent, and `10.20.0.133` is on `vm-public` (members
  `tap0`, `re0`);
- the PoC share is present, zroot is ONLINE with 344G free, and
  `/usr/bin/ssh` is `6a9e5573…` (unchanged);
- the launch facts equal those after zq3-r20's Phase 12 start: UEFI vars
  `9f4fca82…`, `rocky.conf` `c101adf1…`, vm-run `43753bc8…`, firmware
  `6b48e967…`.

**Rocky:**
- **runs the stock `5.14.0-741.24.1.el9_9.x86_64`**, which is also the
  grub default and `saved_entry`; `next_entry` is empty;
- up 2 days 16:18, PID `40425` (lstart `Sun Nov 22 17:19:43 2026` UTC,
  zq3-r20's Phase 12 start), boot ID
  `9a3774b8-fcb1-471c-9b00-8a1bfb089f57`;
- Enforcing, no `/.autorelabel`, no `hfs-xz3` file in `/boot`, no
  `/etc/homelabfs`;
- `authorized_keys` `e08542dc…` (one line), python3.9 `729ac37f…`.

**Rocky's upgrade:**
- clock `Europe/Sofia (EET, +0200)`, synchronized;
- stamp `2026-11-25`; newest dnf transaction **#67** (2026-11-25 00:55
  EET, 14 packages): openssh 9.9p1-17, flatpak, golang 1.27.4, libgsf,
  **podman and podman-docker `5.8.5-5.el9_9`**, xdg-desktop-portal;
- **no kernel**, and `needs-restarting -r` says no reboot is needed. The
  hourly ticks still log "reboot deferred", as every tick does;
- crun, conmon and seccomp.json are byte-identical; **podman changed**
  (`8d94a8fb…`), so the pod template's podman pins are refilled.

The public [facts/](facts/) holds eight files:
- f3: the launch facts, the Phase 1 batch, network/modules;
- Rocky: grub, pod runtime, upgrade, dnf #67 and uptime.

The Rocky Phase 1 batch, the stderr files and the f3 ssh hash line stay in
the private fill.

**N1:** the next dnf run is 2026-11-26 00:55 EET = **2026-11-25
22:55 UTC**, so a run today must start Phase 5 by **21:25 UTC**. The gate
recomputes this on any later day.

## Inputs

| Input | Value |
|---|---|
| Candidate | second r21 rebuild: manifest `f21450ec…` (194 files), package `/home/paul/tmp/claude/hfs-xz3-20261125-r21/package`, source and repository baseline `3081d31` |
| New bytes | kernel bzImage `74728cac…`, build ID `bd4fa753…`, System.map `debb6bda…`, config `692ed3fa…`; daemon `c7b89648…` (GNU build ID `9694935d…`); exporter `8cadae40…` (GNU build ID `03b06c86…`); window artifacts `0129385e…`/`2157b861…` |
| Same bytes | the five modules and `modules.*`; the init helper `8c02b98f…` |
| Lab tools | re-pinned (`git diff e2f406a 95f6d84 -- tools/lab docs/lab/zq3-fdescfs-window internal/runtimeprep/testdata`): `controller.py` `7b36d974…` and `rocky_kernel.py` `f17963c1…` (admit only `f21450ec…`; `FD_ATTR`), `bootstrap.py` `b8714120…`, `bootstrap_remote.py` `5155ff08…`, `fill_plan.py` `1a32774d…`, `guest.py` `746a67bc…`, `correlate.py` `d20f9753…`, `pod_fixture.py` `7c1e545f…`, `argv.py` `940d4787…`, the window `pins.json` and `build-record.json` |
| Unchanged tools | `guest-lock-probe.py` `fd439897…`, `pod_workload.py` `c714370f…`, `observer.py` `2e1449dd…`, `observer_key.py` `cd7f38c2…`, `host.py` `738e1ca5…`, `host_snapshot.py` `b29ffed2…`, `runtime-guard.py` `fece96b3…`, native test guard `576d802a…`; every staged copy is re-hashed against main by `build-stage.sh` |
| Runtime spec | `internal/runtimeprep/testdata/zq3-spec.json` at `95f6d84`: now pins the exporter `8cadae40…`, otherwise unchanged (`ControlTickets` 24). `make-spec.py` changes only the run ID and the operator key path |
| Facts | 2026-11-25 09:38:05Z (stamp `20261125T093805Z`), above |

## Prep compatibility with the candidate daemon

`hfs-runtime-prep` and the signer are built from `95f6d84`; the daemon from
`3081d31`. `git diff 3081d31 95f6d84 -- internal cmd go.mod go.sum` touches
only `internal/runtimeprep/testdata/zq3-spec.json` (the exporter pin) and
`internal/guestbridge/README.md`. So the prep code is the candidate's.

The generated `generation.json`, `request-template.json` and
`volume-init.json` have exactly zq3-r20's key structure.
`runtime-config.json` differs only in its session-UUID map keys, and its
`Exporter.SHA256` is `8cadae40…`. `argv.py verify` and
`hfs-runtime-prep check-argv` pass. **No format change: no stop.**

## What was generated

| Step | Result | Key hashes |
|---|---|---|
| Tools | **Rebuilt** at `95f6d84` with go1.27.4-X:nodwarf5 (`-trimpath`, CGO, VCS stamps, unmodified private clone, `vcs.modified=false`); a second build of each, forced with `-a`, gave the same bytes | prep `39d97a90…`, signer `56147372…`, helper `8c02b98f…` |
| 1. Bundle | `hfs-runtime-prep generate` from the committed spec (`make-spec.py`); the spec equals zq3-r20's renamed plus the exporter | spec `f3214dda…`, request template `628ea087…`, runtime config `ecf9ce3d…`, volume-init `ce9469ce…`; volume `7c10fb99-…`; sessions hfs0 `a37b9380-…`, hfs1 `460c7ff8-…` |
| 1. Argv | `argv.py generate` from the 09:38:05Z launch facts (sessions, `tap100`), then `argv.py verify` **pass** and `hfs-runtime-prep check-argv` **pass** | record `ceeb792f…`, args `ce46e147…`, vars `9f4fca82…`, exporter `8cadae40…` |
| 2. Kernel plan | `rocky_kernel.py plan` at main, stock default `/boot/vmlinuz-5.14.0-741.24.1.el9_9.x86_64`. The first r21 package `1428d4db…` is refused ("superseded xz3 candidate manifest; use the second r21 rebuild (f21450ec)") | plan `6b42f667…`, build ID `bd4fa753…` |
| 3. Batch template | Derived from zq3-r20's by `derive-templates.py`: run ID, fill path, sessions, candidate `b37bcb06…` → `f21450ec…`, `probe_sha256` guest.py `746a67bc…`, ssh `d4c70b09…`, **`fd_attr` true** (plan and requests). be4 seconds 240/255 kept (checked); **T5 stays deferred** with the r9 reason. `check` refuses the template as designed; a zero-ID copy **passes**; the refusals are listed in [pins](pins.json) `controller_batch.synthetic_checks` (r20 and first-r21 candidates superseded, zq3-r14 seconds, no deferral, `credentials` deferred, the three fd_attr shapes) | template `99ca489d…`, synthetic `bda86daf…` |
| 4. Bootstrap plan | Derived from zq3-r20's: new bundle, kernel plan, `rocky_kernel.py`, prep, daemon `c7b89648…`, `bootstrap_remote.py`, ssh pins and the three fd_attr fields. The unfilled template refuses (`observer.ssh_sha256: SHA256 hex`); **F1 filled now** with `fill_plan.py bootstrap` from the 09:38Z f3 `sha256 -r /usr/bin/ssh` line (`6a9e5573…`, unchanged), then `bootstrap.py check` **pass** | template `5e00ef96…`, filled plan `43181e44…` |
| 5. Guard argv | zq3-r20's with **`--runtime-sha256 c7b89648…`** and `--disposable-validation` once, last; equal to `bootstrap.py`'s own `SshRemote.guard_argv()` (shell string and argv) and passes `runtime-guard.py`'s `parse_args` and `check_command` | `44baa7ac…` |
| 6. Pod template | zq3-r20's `/2` template, run ID renamed, `fixture_sha256` pl4's `7c1e545f…`, **podman pins refilled from the facts** (`8d94a8fb…`, `podman-6:5.8.5-5.el9_9.x86_64`). The committed `pod_fixture.py` accepts it and refuses the malformed variants and zq3-r20's fixture pin. **Expected rootfs manifest `34a71569…`** (an expectation, not a pin) | pod template `d96dc035…` |
| 7. Lock-probe templates | zq3-r20's, run ID renamed (150/240); the filled requests pass `validate_request`, case 151 refuses | m0 `c8ec5bee…`, m1-T8 `6a2d8b19…` |
| 8. Staging | `build-stage.sh`: 17 f3 and 18 Rocky files, two deterministic tarballs (a second tar run gave the same hashes); a verification extract matched all 35 hashes; the f3 tarball holds only `0/0` entries, modes `0700`/`0500`/`0400`, no parent entry; extract deleted. Against zq3-r20: f3 `argv.py`, bhyve, the two window artifacts, the daemon, argv record, volume-init and runtime config; Rocky bzImage, config, System.map, plan, `rocky_kernel.py` and `pod_fixture.py` changed | f3 tar `e8ab0797…`, Rocky tar `d69781f6…` |
| 9. Window pins | `docs/lab/zq3-fdescfs-window/pins.json` at main with `zq3-r1` → `zq3-r21`. Synthetic fills (not pins): zq3-r8's real Phase 4 facts with the r21 hashes **pass**; the r20 or xc4 hashes **refuse** (`pinned artifact differs or missing`); facts with both modules loaded **refuse** (`fdescfs module present`): hence Phase 1a | r21 pins `1ae0860e…` |
| 10. Signing watcher | zq3-r20's with `D`, the comments and **the signer pin** changed; tested with a fake signer (wrong hash, command, action and repeat refused; both exact requests signed; the real pin refuses the fake binary) | `8aeb82fc…` |
| 11. Process observer check | `sh/p4-observer.py` with **the daemon pin `c7b89648…`**; zq3-r20's real records (renamed, hash substituted) **pass**, unchanged they are refused for the `sha256` only; the `$NG` line (Rocky PID `40425`) passes the native guard's `parse_args` and `check_request` | `9c81a284…` |
| 12. N1 gate helper | `sh/n1-gate.py` with `FACTS_TOP` **#67 of 2026-11-25 00:55**. Fixed-time tests for 2026-11-25: 10:30Z (also with the real record), 21:20Z and 21:25Z GO; 21:30Z, 22:56Z, 23:10Z STOP; the next day with the old Phase 1 STOP, fresh GO; a stale stamp STOP; 01:00Z GO; `phase1` true for the real facts (#67), false for #66 and #68 (14 cases) | `78cbcd3f…` |
| 13. Recall helper | `sh/recalls.py` also prints vj4's **`self_admissions`** (null while absent); tested on zq3-r20's real OBSERVEs (10 recalls, advances 0, self_admissions null), a synthetic copy (advances 37, self_admissions 5) and an existing output (exit 2) | `1d39257a…` |
| 14. Other helpers | `p5-recheck.py` and `p5-estimate.py` pin Rocky PID 40425 / lstart 17:19:43; `p10-requests.py` the three r21 templates; `p11-fence.py` (docstring); guard budget unchanged and retested on zq3-r20's real journal (all latest starts GO, one second later STOP; malformed journals `rc=2`); `p1-f3.sh` also lists every bhyve process and zroot | [pins](pins.json) `fill_helpers` |
| 15. Run sheet | [run-sheet.md](run-sheet.md): Phases 0-12 with every kept rule, the zq3-r20 run notes, "New for zq3-r21", BEGIN/END markers in all 48 code blocks, the fd_attr expectations (Phase 8 mountinfo, Phase 9 tuple, T3 peer rename), the stock-kernel rule (Phase 3, 12), the iormesh rule and a run log template. `session.sh` `dff08094…` (hpin over 19 files) | [pins](pins.json) `fill_helpers` |

Staging recipe (both tarballs, deterministic):
`tar --sort=name --owner=0 --group=0 --numeric-owner --mtime=2026-11-25T00:00:00Z --format=gnu`.
The f3 tarball holds only `hfs-20261125-zq3r21-fdq` and
`hfs-validation-xz3/zq3-r21`, with **no entry for the existing parent**.

## Signing

As in zq3-r20: Paul delegated both signatures (A7) to Claude. In Phase 8 the
driver's stdout goes through `bin/sign_watch.py` (`8aeb82fc…`). The
watcher runs the signer (`56147372…`) only if:
- the printed command is exactly one of the two below;
- the request file's SHA256 equals the printed `request_sha256`;
- each command runs at most once.

Anything else is refused, and the driver then times out (fails closed):

```
/home/paul/tmp/claude/hfs-xz3-20261125-r21/zq3-r21-fill/bin/hfs-retained9p-operator enroll --request /home/paul/tmp/claude/hfs-xz3-20261125-r21/zq3-r21-fill/bootstrap-run/work/enroll-request.json --output /home/paul/tmp/claude/hfs-xz3-20261125-r21/zq3-r21-fill/bootstrap-run/work/signer-enrollment
/home/paul/tmp/claude/hfs-xz3-20261125-r21/zq3-r21-fill/bin/hfs-retained9p-operator bind --request /home/paul/tmp/claude/hfs-xz3-20261125-r21/zq3-r21-fill/bootstrap-run/work/bind-request.json --output /home/paul/tmp/claude/hfs-xz3-20261125-r21/zq3-r21-fill/bootstrap-run/work/signer-binding
```

The operator key goes to the bundle's path
`/home/paul/tmp/claude/hfs-xz3-20261125-r21/zq3-r21-fill/operator/operator-signing-key`
before the run (the empty `0700` directory exists). Nothing in the fill
read it.

## Run-time fills

As in the [zq3-r20 fill](../20261122-zq3-r20-filled-plan/README.md#run-time-fills),
with these values changed:

| Fill | When and source | Used by |
|---|---|---|
| N1 | Phase 1: `n1-gate.py phase1` expects **#67**; the running kernel must equal the grub default **`741.24.1`** | Phase 1 Rocky gate, Phase 3 install, Phase 5 go/no-go, T7 |
| D1 | **D1-a, Phase 1a**: both modules are loaded (09:38Z: ids 37 and 38) | Phase 4 step 0 window plan fill |
| F1 f3 `/usr/bin/ssh` SHA256 | **filled at fill time** (`6a9e5573…`, from the 09:38Z fact); Phase 4 step 0 rechecks it | bootstrap `observer.ssh_sha256` (plan `43181e44…`) |
| F3 pod rootfs manifest digest (`PM`) | Phase 3: expect `34a71569…`; any other digest is explained before `build` | `build`, T7 request |
| F3a process observer record | Phase 4 step 1a; **pass required** for the NEW daemon `c7b89648…` | go/no-go before setup |
| F11 Rocky PID and lstart | **fixed** from the 09:38Z facts (`40425`, `Sun Nov 22 17:19:43 2026` UTC; boot ID `9a3774b8-fcb1-471c-9b00-8a1bfb089f57` copied from the facts file) | native guard host profile, Phase 5 recheck |
| F21 swap | **No Phase 0b**: swap 0 at the facts; Phase 1 requires 0 used (else stop and ask Paul); Phase 6 requires swap unused | Phase 1, 5, 6 |
| F24 `hfs_fdattr` (new) | **filled at fill time** in both plans (above); Phase 8's mountinfo rows and the batch fill check it at run time | Phase 8 mounts, batch fill, T1-T4, T9-T11 |

## Expectations

- **Through Phase 9 as in zq3-r20**, now with `hfs_fdattr` on both mounts;
  Phase 4 step 1a passes for `c7b89648…`; the exporter `8cadae40…`
  launches cleanly (first run).
- **T1 should pass:**
  - sj4 fixes R20-1;
  - tj4/wj4 let the guest ask for the xf4 advance;
  - vj4 admits the creating CALLs past their own observation leases.

  The model replay (`TestZQ3T1CredentialsReplay`, vj4 with advance) has 0
  CALL_CONFLICT, 0 EXPECTED_TARGET and at most 2 RELEASE (DOMAIN) recalls,
  7-10 self admissions and 35-37 advances (T1 modelled 22 s, measured
  about 1.5 min).
- **Then T2-T4, T6, T7, T9, T10, T11 and T8** within the guard budgets.
  None of T2-T11 has run for real. T3 adds the s64 peer-rename guard, and
  T9-T11 exercise hh4 ABANDON and dh4 for the first time. T5 stays
  deferred.
- **Record:**
  - the admission timings;
  - recalls by trigger, `advances` and `self_admissions`;
  - every guest re4/1h4/4h4/tj4/s64/hh4 log line;
  - any `EACCES` (R20-1 again), `EAGAIN`, `EOPNOTSUPP`, `EKEYREJECTED`,
    `ESTALE` or `ETIMEDOUT` from a probe syscall, as a finding with its
    named syscall.

Known risks, not fixed:
- **ue4**;
- the s64 description path's first VM run;
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

**Sheet dry run** (`dryrun/` in the private fill). The zq3-r20 harness ran
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

**Real-record checks:**
- `p9-ready.py` refused zq3-r20's real READY run (its control socket is
  the r20 path);
- `fill_plan.py batch` refused zq3-r20's real run for the r21 plan pin;
- `p10-admission.py` on zq3-r20's real T1 log reports `admission_ms`
  7624.9 and the failure `Refused` "supplementary group access" (R20-1).

## Findings and open questions

- **Q0 N1.** Start after the run day's upgrade has stamped and finish before
  22:55 UTC: latest Phase 5 start 21:25 UTC, no new case after 22:15 UTC,
  guest off before 22:45 UTC. The gate computes the dates.
- **Q1 T5 deferred.** The run cannot complete zq3 acceptance.
- **Q2 the guard budget.** Worst cases 5025 s against 3600 s.
- **Q3 first runs:** the bd4fa753 kernel, the go1.27.4 daemon and the
  `8cadae40` exporter. Each has only builds, clean repros, source suites and
  offline admission behind it.
- **Q4 `hfs_fdattr`'s first VM run.** A mountinfo row without the option
  stops the run at Phase 8 (`changed`, mount kept). An errno from an
  fd-based call is a finding against s64.
- **Q5 the iormesh VMs.** The window needs iom1-3 and any iormesh k3s VM
  stopped. At the facts all were stopped and no k3s VM was listed. The
  sheet never stops or starts them: the orchestrator schedules the window
  with the iormesh session and tells it afterwards that f3 is free.
- **Q6 memory.** The 09:38Z numbers are a record only. Phase 5's estimate,
  the drain and the Phase 6 bar decide at run time.
- **Q7 UEFI vars drift.** The record pins `9f4fca82…`. Any Rocky boot before
  Phase 7 changes it, and `host.py setup` then refuses.
- **Q8 Phase 1a.** Every D1-a so far unloaded both modules; a refusal stops
  before staging.
- **Q9 the workstation ssh.** It changed with the Fedora 45 upgrade
  (`d4c70b09…`). Another openssh update before the run makes the batch fill
  refuse after READY: the Phase 0 recheck catches it first.
- **Q10 the pod rootfs manifest.** Expected `34a71569…`. pl4's commits pin
  Rocky's digest as unchanged, and podman is not in the rootfs closure.
  Any other digest is explained before `build`.
- **Q11 pins.json is new.** `make-pins.py` was rewritten. It computes the
  same pinned sections and drops the earlier fills' carried-over prose and
  per-run history (zq3-r20's Q13), which stay in the run sheet.
- **Q12 the stock kernel.** Rocky's restored kernel at Phase 12 must be the
  stock `5.14.0-741.24.1.el9_9`. The custom `6.1.190-hfs-xz3` is only ever
  a one-shot `next_entry` (Phase 3 never uses `--make-default`), and U2
  removes it. The sheet's Phase 3, 12 and Recovery lines say so.

## Local verification

At `95f6d84` (a clean worktree of main), all passing:
- retained9p: 440 tests;
- directlaunch: 26 tests;
- `tools/lab` top level: 249 tests (3 skipped);
- `tools/integration/artifacts`: 44 tests;
- `go test -race` for `internal/runtimeprep` and the two tool packages.

The full Go suite and its `-race` pass are the candidate's (at `3081d31`,
its evidence README). Since then only the spec testdata and READMEs
changed among the Go sources, and no Go code changed in this commit.

`bash -n` and shellcheck are clean for `build-stage.sh` and the `sh/`
shell helpers. `session.sh` keeps its intentional word splitting of `$O`,
loads only in bash, and its `hpin` and `okpin` pass. No code was changed.
