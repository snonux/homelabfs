# zq3 proposed run plan: one disposable real-daemon retained-9p run

[Plain-language summary](zq3-plan-summary.md) ·
[Base gate assessment](Real-daemon-9p-base-gate.md) ·
[Descriptor window](Descriptor-execution-window.md) ·
[Deployment gates](../Deployment-tests.md) ·
[Probe tools](../../tools/lab/retained9p/README.md)

**Status: proposal only, not executable yet.** This document gives the exact
shape of one disposable run of the real single-node HomelabFS daemon behind
`Rocky -> virtio-9p -> f3 host FUSE`. It grants no authority. Paul's explicit
approval of a *filled* version of this plan (exact hashes, run ID and fresh
facts) is needed before anything in section (A) happens. Several scripts that
the phases name do not exist yet; they are listed in
[Implementation gaps](#implementation-gaps) and marked **GAP** inline. No phase
may run with a gap substituted by improvised commands.

Prepared 2026-11-08 from `main` `8d2269f` (task zq3). Nothing in this
preparation used SSH, the lab, a VM, a mount or a private key.

## Pinned inputs

| Input | Value |
|---|---|
| Run ID | `zq3-r1` (guest/controller regex `[a-z0-9][a-z0-9-]{0,23}`); a retry gets a new ID |
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261110h/package` (y64 rebuild, 2026-11-10), manifest SHA256 `867ede3630e4ee0a609f88b0752670fac5b7a0c0e0f9d2a340031110da32c334`, source `b8697101df601d7748bae85721e91c0e01aae474`; all three components were rebuilt: the guest kernel (g64 `show_options`, exact `cache=mmap`, once-per-session block log; m64 Sources floor, every fault block logged, clean unmount silent; c64 recipe), the CGO daemon (i64, o64, r64, n64, j64, u64, v64, x64) and the native bhyve exporter with lib9p (564, e64, p64, w64; built natively on f3 and reproduced). The five modules came out byte-identical. The c64+d64 candidate (`66b5bb2b…`, `060dea0`) is now refused as superseded together with 964 (`e01fab21…`), z54 (`2e0412a6…`), w54 (`89c928d5…`), r54 (`5eb42c50…`), f44 (`b203799e…`) and `3f770a88…`. The kernel keeps the c64 SELinux LSM, the 964 fixed-tag release wait (zq3-r5 R5-1), the w54 failed-bind kill_sb fence (zq3-r3 R3-1) and the r54 mount HELLO fix (zq3-r2 R2-4) |
| Quarantined, never installed | `7e962627b1ec9aed913a28ee652bd9ed2f110f3369232c684994c709be2dce5b` (CGO-disabled daemon); the probe controller refuses it |
| Daemon | `freebsd/hfs-controlled-runtime` `34fb05b28357b42e37816370f9bb6c0ec046ff2e98763dafff87c3680021694c` (y64, from `b869710`; two forced builds and the window build gave the same bytes). **New bytes: they have not passed the f3 native observer check yet** (the z54 `39c92d01…` passed it in zq3-r5; the d64 `9b7eed8d…` was never checked); the filled run sheet's Phase 4 step 1a (`--check-process-observer` under the bounded host guard) must run for these bytes |
| Patched bhyve (exporter) | `freebsd/bhyve` `ef8f17de8754f8bdd6d5d517f15a859e87a2f4a8ca1542394f5608a8e40823b1` (GNU build ID `6870705e…`; y64 native f3 build of the 564/e64/p64/w64 lib9p sources, two clean builds equal in all 83 files; every earlier candidate carried `36423b6d…`); the system `/usr/sbin/bhyve` (`c54a6361…`) is never replaced |
| Guest kernel | `6.1.190-hfs-xz3` (c64 recipe, g64+m64 series): `linux/bzImage` `40afad22…`, `linux/vmlinux` `d2d3e5ab…` (GNU build ID `870bb9d1…`), `linux/config` `28fe4a8e…` (unchanged from c64: `CONFIG_LSM="lockdown,yama,integrity,selinux,bpf,landlock"`), `System.map` `1ffea83f…`, five modules (unchanged since w54) (`virtio_net`, `net_failover`, `failover`, `efivarfs`, `x86_pkg_temp_thermal`) with hashes in the package's `installation-proposal.json` |
| Operator public key | `2f6d909c8bfa020c43dd460dcc03fd9d1fd78e03cdb61f51aa693f77de89a635` (built into the guest kernel config) |
| Guest SELinux | The test kernel must boot with SELinux as an active LSM and Rocky's policy enforcing, as the stock kernel does (`guest.py` and `pod_fixture.py` refuse T1-T5, T7 and T9-T11 otherwise). The pre-c64 kernels (through `e01fab21`) had `CONFIG_SECURITY_SELINUX=y` but `CONFIG_LSM="landlock,lockdown,yama,loadpin,safesetid,integrity,bpf"`, and the copied Rocky command line has no `lsm=`/`security=`, so SELinux never initialised (zq3-r5 guest dmesg has no `SELinux:  Initializing`). The pinned `867ede36` kernel is built from the c64 recipe (`CONFIG_LSM` above, RHEL 9 order) and `kernel.py`/`admit.py` refuse a config without `selinux` in `CONFIG_LSM`. The boot itself is still unverified: Phase 9's `getenforce` = `Enforcing` and a dmesg `SELinux:  Initializing` line are real checks again |
| Probe tools | `tools/lab/retained9p/{controller,guest,observer}.py` at the commit that adds this plan; the controller pins only the candidate above |
| Staging prefix (f3) | `/var/tmp/hfs-validation-xz3/zq3-r1` (from the installation proposal) |
| Guest run directory | `/var/tmp/hfs-zq3-zq3-r1` with mount points `m0`, `m1` |
| fdescfs window directory | `/var/tmp/hfs-fdescfs-windows/zq3-r1` |

## Current facts baseline (must be re-collected)

Collected read-only by the orchestrator on **2026-11-08 18:10 UTC**
(`/home/paul/tmp/claude/hfs-xz3-20261108/zq3-facts/{f3.txt,rocky.txt}`).
They are a baseline for review only. Phase 1 re-collects all of them, plus the
missing items marked *new*, immediately before the run; any difference stops
the run and needs a revised plan.

| Target | Fact on 2026-11-08 18:10 UTC |
|---|---|
| f3 kernel | FreeBSD 15.1-RELEASE-p2 `releng/15.1-n283596-aadd58dddcbc` GENERIC |
| f3 VMs | `rocky` Running PID 80203 (uefi, 4 CPU, 12G, AUTO yes); `freebsd`, `hfs-test0..2` Stopped |
| f3 memory | physmem 16,879,837,184 bytes; swap 2 GiB, unused |
| f3 modules | loaded: `vmm`, `nmdm`, `if_bridge`; **not** loaded: `fusefs`, `fdescfs`; no fuse/fdescfs mounts |
| f3 storage | `zroot` 338G available (`zroot/bhyve` 109G used, `zroot/var/tmp` 3.57G) |
| f3 route to Rocky | `route -n get 10.20.0.123`: destination `10.20.0.0` on `re0`, directly connected, no gateway; `re0` carries `10.20.0.133/24` (also `192.168.1.133/24`, `10.98.0.133/24` on other subnets), so f3's source toward Rocky is `10.20.0.133`, matching the observer key's `from=` (`f3-route-to-rocky.txt`) |
| f3 stock modules/binaries | `fdescfs.ko` `847cd1f2…`, `fusefs.ko` `b207adf6…`, `/usr/sbin/bhyve` `c54a6361…` |
| Rocky vm-bhyve config | unchanged; still contains the old PoC line `bhyve_options="-s 0:7:0,virtio-9p,homelabfs_poc=/var/tmp/homelabfs-9p-poc/share"` |
| Rocky OS/kernel | Rocky Linux 9.8; running and grubby default `5.14.0-687.66.1.el9_8.x86_64`; other entries ELRepo 7.0.3, ELRepo 6.1.190, 5.14.0-687.65.1/.64.1, rescue. **The custom `6.1.190-hfs-xz3` is not installed.** |
| Rocky security | SELinux Enforcing; root `authorized_keys` has 1 line |
| Rocky OCI | `podman` 5.8.2 and `crun` 1.27 installed; `runc`, `buildah`, `skopeo` not installed; no image pinned |
| Rocky resources | 11.7 GiB RAM visible, 7 GiB swap unused, 36G free on `/` |

*New facts Phase 1 must add* (not in the baseline): f3 `kern.boottime`
and Rocky's bhyve PID with `TZ=UTC ps -o lstart=` (Phase 5 compares both,
zq3-r1 R5); f3 ARC size/limits and
wired/free/inactive pages; whether the host path of the old PoC line,
`/var/tmp/homelabfs-9p-poc/share` on **f3**, still exists (vm-bhyve's normal
Rocky start in Phase 12 depends on it, see Risk R6); `/usr/local/bin/python3.12`
hash on f3 (the fdescfs controller's pinned interpreter) plus the other host
hashes, `ldd` closure and interpreter ownership listed under
[Filled f3 plan](Descriptor-execution-window.md#filled-f3-plan-for-zq3-r1-2026-11-08); `vm-public` bridge
members and free tap names; current zrepl snapshot name/GUID of
`zroot/bhyve/rocky`; Rocky `/boot` and EFI free bytes, `grub2-editenv list`
(`saved_entry`, `next_entry`), `/etc/kernel/cmdline` or default BLS options,
dracut/depmod/grubby versions and the root-owned tool paths `rocky_kernel.py`
pins (`/usr/sbin/grubby`, `/usr/bin/grub2-editenv`, `/usr/sbin/grub2-reboot`,
`/usr/sbin/depmod`, `/usr/bin/dracut`, `/usr/bin/systemctl`,
`/usr/bin/udevadm`); Rocky kdump state (orchestrator read on 2026-11-08,
`zq3-facts/rocky-kdump.txt`: kdump enabled and active, `crashkernel=1G-2G:192M,2G-64G:256M,64G-:512M`,
one `/boot/initramfs-<release>kdump.img` per installed kernel, so a
test-kernel boot may create `initramfs-6.1.190-hfs-xz3kdump.img`); root
`authorized_keys` SHA256, the real file behind `/usr/bin/python3` (expected
`/usr/bin/python3.9`; the observer pins and opens it with `O_NOFOLLOW`, so a
symlink refuses) and its hash; current authoritative addresses from IQ (f3
`10.20.0.133` and Rocky `10.20.0.123`, now pinned in `controller.py`) and a
check that both hosts still present the keys in
[`f3-host-known_hosts`](f3-host-known_hosts) and
[`rocky-known_hosts`](rocky-known_hosts) (strict checking, never accept-new).

## Approval classes

**(A) Critical: each needs Paul's explicit yes for this filled plan.**

- A1 Stop Rocky for the run window (Phases 5-12); see [downtime](#rocky-downtime).
- A2 Stage the candidate bytes on f3 under the private staging prefix (Phase 2).
- A3 Install the custom guest kernel on Rocky's disk (Phase 3), select it as a
  **one-shot** boot immediately before the stop (Phase 5), stock default kept,
  and remove it afterwards (Phase 12). Includes writing
  `/etc/homelabfs/installed-admission-v1.json` on Rocky's disk in the
  controlled boot (Phase 8 step 0) and the U3 recovery boot: after an
  abort between the stop and the controlled boot, Rocky boots the test kernel
  once under its normal config and is then cleanly rebooted to stock.
- A4 Run Rocky once under the candidate's own bhyve, launched by the runtime,
  with 6 GiB, a transient tap on `vm-public` and its own console (Phases 7-8).
- A5 f3 host SYSTEM changes: `kldload fdescfs` and `kldload fusefs` if absent,
  **both modules stay loaded afterwards** (no unload), and the `fdescfs -o nodup`
  mount on `/dev/fd` for the run, normally unmounted at the end (Phases 4, 11).
- A6 Run-only observer SSH key: generate on f3, enrol one restricted line in
  Rocky root's `authorized_keys`, remove it and destroy the key (Phases 4, 11).
- A7 Use of the operator signing key `2f6d909c…` for the enrollment and bind
  signatures during bootstrap (Phase 8): who holds and runs it.
- A8 Pod fixture inside Rocky (Phase 10, T7), or skip. Runtime/rootfs chosen by
  agent judgement (w34): the installed podman `5.8.2-8.el9_8` and crun
  `1.27-1.el9_7`, rootful in `init_user_ns`, VFS storage in run-owned roots, and a
  `--rootfs` tree built on Rocky from its own RPM-attested files. No image pull,
  install or registry. The yes covers building `RUN/pod/rootfs` and running two
  disposable containers that bind-share `m0`
  ([details](../../tools/lab/retained9p/README.md#ordinary-pod-namespace-fixture-zq3-g9-t7-decision-a8)).

**(B) Routine bounded verification** (standing approval of 2026-11-02, but only
inside an approved (A) window): read-only facts, local builds/tests, read-only
host test binaries under the bounded guard, lifecycle OBSERVE, guest probes in
the run-owned directories, the lock probe, evidence collection, and normal
unmounts of run-owned mounts.

## Limits

| Limit | Value | Source |
|---|---|---|
| Total critical window | 60 minutes from runtime start to runtime exit; the probe controller refuses commands 3600 s after its run started, and the outer runtime guard sends the runtime one SIGTERM (its retained stop, bhyve untouched) at 3600 s | proposal `seconds: 3600`; controller `RUN_WINDOW`; `runtime-guard.py` |
| Restoration budget | 15 minutes after runtime exit (estimate, not enforced) | this plan |
| Host runtime envelope | 6 GiB (signed); admission counts the enforced guard ceilings instead (runtime 4.25 GiB + bhyve 6.5 GiB + 3 GiB host reserve = 13.75 GiB, measured against the reclaimable memory in Phase 6: 14.9 GiB on a fresh f3 boot) | installation proposal, `runtime-guard.py` |
| Runtime admission accounting | `Budget.HostBytes` at most 4 GiB (the admission package's aggregate pool ceiling), inside the 6 GiB physical envelope; the example spec uses 4 GiB with a 192 MiB service share (accounting only, unmeasured; how to revisit it: `internal/runtimeprep/README.md`), so that `Views.Sources` 10 (floor `2*domains + 4` = 8, task i64; zq3-r7's 2 refused every non-read op) and the o64 provider window budgets fit, 38.8 MiB spare. Provider credits recycle at retirement and released grants are pruned (o64); `TestZQ3RunWorkloadFitsLifetimeBounds` holds every provider, bridge and view bound at 2x one run's modeled need, so the spec covers r8 through bootstrap, k64, Phase 9, T1-T4 and T6-T11. T5's mapped dirtying is not covered and needs task q64 (view identity recycling): `Views.Identities` 64 counts every view ticket ever admitted, and its 1024 dirty pages need about 2050 | `admission/budget.go`, `runtimeprep`, `viewcontrol/source_floor.go`, `runtimeprep/README.md` capacity notes |
| Guest workload ceiling | 2 GiB signed provider/workload ceiling; logical canary 4 MiB | proposal |
| Guest kdump reservation | the copied default arguments carry `crashkernel=…,2G-64G:256M,…`, i.e. 256 MiB of the 6 GiB VM under a kernel that honours it. The test kernel's config has `CONFIG_KEXEC`, `CONFIG_KEXEC_FILE` and `CONFIG_CRASH_DUMP` unset, so it should reserve nothing and `kdump.service` should fail in that boot (an expected failed unit); budget the 256 MiB anyway until Phase 8 confirms it | Phase 1 kdump fact, package `linux/config` |
| Disk | host run 4 GiB, guest run 2 GiB; keep 8 GiB unreserved host and 2 GiB guest free | proposal |
| Bootstrap | 120 s from launch to READY | base gate |
| Guest probe case | at most 60 s, 256 operations, 12 MiB issued writes, 128 fds | `guest.py` |
| Controller dispatch | each SSH dispatch bounded by `min(case seconds + 15, plan seconds)`, plan seconds 60-600 (enforced); streams 256 KiB each | `controller.py` |
| fdescfs commands | 10 s each, 256 KiB output; Go test 5 s; native RSS 512 MiB, one CPU (`qualify` under `native-test-guard.py` host profile) | fdescfs window doc |
| Runtime guard | runtime + non-bhyve descendants 4 GiB + 256 MiB RSS (the 4 GiB `Budget.HostBytes` cap plus observer/Go overhead); bhyve 6 GiB + 512 MiB; run disk 4 GiB (mounts below it never touched); 8 GiB free; no swap; 512 MiB available memory; 3600 s | `runtime-guard.py` |
| Lifecycle endpoint | OBSERVE/ARM 30 s, RESTART 90 s, replies 128 KiB | `lifecycle.md` |

## Phases

Every phase writes a durable intent record before a mutating step and a result
after it; an intent without a result is **uncertain** and is never replayed.
Generic failure rule for all phases: stop dependent work, collect evidence,
do not kill or force anything (no `vm poweroff`, no `kill -9` of bhyve, no
forced unmount, no recursive delete through a mount), preserve failed
resources and report. Restart normal Rocky only when no candidate bhyve child
or vmm context could still exist.

### Phase 0: local preparation (workstation, B)

- Actions: the FreeBSD CGO `controlledruntime.test` binary (for the host FD
  rerun) and the `fdescfs-namespace` helper are already rebuilt from `b869710`
  (y64 rebuild) and pinned (`f71f0bb8…`, `3d992cdf…`, reproducible; recipe
  `tools/lab/build-fdescfs-window-artifacts.sh`, record
  `docs/lab/zq3-fdescfs-window/build-record.json`); rebuild only to re-verify.
  The fdescfs window pins are reviewed in `docs/lab/zq3-fdescfs-window/pins.json`;
  the exact plan JSON is filled in Phase 4 from fresh facts with
  `tools/lab/fdescfs-window-plan.py`. Write the run config, the G5 kernel plan
  (`rocky_kernel.py plan`), enrollment request *templates* and the controller
  batch plan (`check-retained9p.sh check --plan` gives its hash). While T5
  stays deferred (q64, below), the batch template declares it: a `deferred`
  entry `{"case": "mapped-pressure", "reason": ...}` and only the four other
  requests (z64); `check` must print `cases_planned` without and `deferred`
  with `mapped-pressure`. The
  enrollment requests cannot be complete here: `BootID`, `ProviderNonce` and
  `Observer.ManifestHash` exist only in the controlled boot and are filled in
  Phase 8 step 0.
  Run all local tests (`python3 -B -m unittest` in `tools/lab/retained9p`,
  `tools/lab/test_fdescfs_window.py`, `test_fdescfs_window_plan.py`,
  `test_runtime_guard.py`, artifact tests, `go test` for touched packages,
  shellcheck).
- Changes: workstation files only.
- No remaining Phase 0 gaps: the run config/enrollment generator (G2, s34)
  and the window plan filler (G12, v34) are implemented.
  Controller target addresses and host-key pins (G8) are implemented: the
  batch plan's `host`/`guest` entries name the pin files above. G2 is
  implemented (s34, pending review/pinning,
  [runtimeprep](../../internal/runtimeprep/README.md)); the G3 helper is built
  with `tools/lab/build-init-helper.sh`. The G2 sessions must exist before the
  G4 argv record, so the order is, on the workstation after the Phase 1 launch
  facts exist and before Phase 2 staging:
  1. `hfs-runtime-prep generate`: its spec `Exporter.Args` is the G4
     controlled argv (Phase 7) with `{{session:0}}`/`{{session:1}}` as the
     hfs0/hfs1 `hfs-controller` values. It draws the sessions and writes the
     run config, keys, G3 plan and request template.
  2. `tools/lab/directlaunch/argv.py generate --session <hfs0> --session
     <hfs1>` with the two sessions from `generation.json` (G4).
  3. `argv.py verify --runtime-config <bundle>/runtime-config.json` and
     `hfs-runtime-prep check-argv --bundle … --record … --record-sha256 …`
     must both pass: same args element by element and in order, same exporter
     path/digest, same run ID and the same session for each share tag. Any
     difference means regenerating from a corrected spec (new sessions, then a
     new record), never editing the config or the record.
- Evidence: hashes of every staged byte, plan files and their SHA256.
- Failure: fix locally; nothing remote has happened.

### Phase 1: fresh read-only facts (f3 and Rocky, B)

- Actions: one bounded read-only batch per host over pinned SSH, stdout and
  stderr kept separate, no retry: everything in the baseline table plus the
  *new* items above. (The fdescfs controller's own `facts` call needs the
  staged inspector, so it runs at the start of Phase 4.) The f3 batch includes
  the G4 launch facts, `doas -n /usr/local/bin/python3.12 -I -B - facts <
  tools/lab/directlaunch/host.py` (read-only, nothing staged): rocky.conf,
  vm-run, firmware (package symlink text and confined target) and UEFI vars
  hashes, the last vm-bhyve launch block (the
  retained one is from 2026-11-02, PID 6444; Rocky has since restarted as PID
  80203), `vm list`, `/dev/vmm`, interfaces, `vm-public` members, nmdm nodes,
  processes. Choose a free tap unit (`tap100`..`tap999`) from it.
- Preconditions to continue: Rocky running PID matches, all four FreeBSD VMs
  stopped, swap unused, pool ONLINE with the disk headroom above, `fusefs` and
  `fdescfs` state as recorded, `/dev/fd` is devfs, f3's route to
  `10.20.0.123` still directly connected on the interface carrying
  `10.20.0.133` (else the observer key's `from=` would refuse), PoC share path on f3 present
  (R6), Rocky default kernel/grubenv unchanged, SELinux enforcing, one-line
  `authorized_keys` hash recorded, and `/.autorelabel` absent on Rocky (R7).
- Evidence: `f3-facts.json`, `rocky-facts.json` with timestamps.
- Failure: any mismatch stops; produce a revised plan.

### Phase 2: stage the candidate on f3 (A2)

- Machine: f3. Actions: create `/var/tmp/hfs-validation-xz3/zq3-r1` root-owned
  `0700`; copy into its `bin/` only `freebsd/hfs-controlled-runtime`, `freebsd/bhyve`,
  `freebsd/bhyve-slirp-helper` (if the argv needs it), the `controlledruntime.test`
  binary, `fdescfs-namespace` helper and the G3 `homelabfs` init helper,
  mode `0500`, plus the controllers `tools/lab/fdescfs-window.py` and
  `tools/lab/runtime-guard.py`, mode `0400`, one hard link each; verify SHA256
  of every staged file against the manifest/Phase 0 pins. Also stage
  (G4) `tools/lab/directlaunch/{host,argv}.py` and the pinned argv record,
  and (G3) `tools/lab/runtime-volume-init.py` with the bundle's
  `volume-init.json`, mode `0400`, with their hashes.
- Changes: new files under the staging prefix only. No system binary,
  library, rc or loader change.
- Evidence: staged-file listing with hashes, owner, mode, link count.
- Failure: hash mismatch stops; staged files are preserved for inspection.
- Rollback: delete the staging directory in Phase 12 (only after evidence copy).

### Phase 3: install the custom kernel on Rocky, not yet selected (A3)

Rocky still runs normally on the stock kernel during this phase. This phase
does **not** set the one-shot `next_entry`: an installed but unselected kernel
cannot be booted by an abort. Selection happens in Phase 5, immediately before
the stop (see [Boot selection and recovery](#boot-selection-and-recovery)).

- Machine: Rocky (root over pinned SSH). G5 implemented (r34):
  [`tools/lab/retained9p/rocky_kernel.py`](../../tools/lab/retained9p/README.md#rocky-test-kernel-zq3-g5).
  Phase 0 runs `rocky_kernel.py plan` on the workstation (package checked
  against the pinned manifest, targets derived, build IDs of `vmlinux` and
  each module recorded); the plan, the script and the package's `linux/`
  files are staged on Rocky, then
  `/usr/bin/python3 -I -B rocky_kernel.py install --plan … --plan-sha256 …
  --state … --stage …` does steps 1-4 with a durable intent/result journal:
  1. Copy `bzImage` to `/boot/vmlinuz-6.1.190-hfs-xz3`, `System.map` and
     `config` beside it; the five modules plus `modules.order`,
     `modules.builtin`, `modules.builtin.modinfo` into
     `/lib/modules/6.1.190-hfs-xz3/`; `depmod 6.1.190-hfs-xz3`.
  2. `dracut --kver 6.1.190-hfs-xz3 /boot/initramfs-6.1.190-hfs-xz3.img`
     (root is LVM `rlm_rocky-root` on NVMe/XFS; NVMe, DM and XFS are built in,
     the initramfs still assembles LVM activation).
  3. `grubby --add-kernel=/boot/vmlinuz-6.1.190-hfs-xz3
     --initrd=/boot/initramfs-6.1.190-hfs-xz3.img --copy-default
     --title='HomelabFS zq3 6.1.190-hfs-xz3'` (never `--make-default`).
  4. Record the new BLS entry ID; verify with `grub2-editenv list` that
     `saved_entry` is unchanged and `next_entry` is still **empty**, and that
     `grubby --default-kernel` is still `5.14.0-687.66.1.el9_8.x86_64` (or the
     fresh Phase 1 value).
  5. *Moved to Phase 8:* `/etc/homelabfs/installed-admission-v1.json` cannot
     be written here. The observer probe requires its module set to **equal**
     `/proc/modules` and compares the running kernel's notes, and which of
     the five modules load is only known in the booted test kernel. So
     `rocky_kernel.py admission` runs as root in the controlled boot as
     Phase 8 step 0, before the enrollment is signed. It writes the closed manifest from the
     installed file hashes and the running build IDs (each checked against
     the plan and the installed `.ko`), and its SHA256 becomes the
     `Observer.ManifestHash` in the enrollment request (G1 orchestrator).
  6. Stage `observer.py` and the run directory `/var/tmp/hfs-zq3-zq3-r1`
     (root, `0700`, with empty `m0`, `m1`) on Rocky's local disk with
     [`observer_key.py`](../../tools/lab/retained9p/observer_key.py) `stage`
     (G6, implemented): copy `observer.py` and
     `internal/controlledruntime/probe/installed.py` (`c824b183…`, the probe the
     runtime embeds) into a root-private incoming directory over the pinned
     channel, then run the tool from stdin, e.g.
     `/usr/bin/python3.9 -I -B - stage --run-id zq3-r1 --observer IN/observer.py
     --observer-sha256 <sha> --probe IN/installed.py --python /usr/bin/python3.9
     --python-sha256 <Phase 1 hash> < SNAPSHOT/observer_key.py`.
     The filled plan pins the SHA256 of `observer_key.py` at the plan commit;
     the operator copies it once into a private snapshot, checks that hash
     immediately before every stdin invocation (stage, enrol, status,
     remove) and records it with each result, since Rocky cannot check
     bytes it reads from stdin.
     It checks both hashes and the interpreter, creates the run directory if
     absent and writes `observer.py` and `installed-probe.py` root-owned `0400`.
- Changes: new files in `/boot`, `/lib/modules/6.1.190-hfs-xz3`, one
  non-default BLS entry, the run directory and the root-private G5 state
  directory (`pre.json`, `install.json`, `journal.jsonl`). grubenv is
  unchanged. `/etc/homelabfs/` is written only in Phase 8 (step 0, the admission manifest).
- Bounds: `/boot` must keep at least the Phase 1 headroom minus kernel+initramfs
  size (expected < 100 MiB).
- Evidence: file hashes, BLS entry text, `grub2-editenv list` before/after,
  `grubby --info=ALL` before/after.
- Failure: stop before Phase 5 and run U2 while Rocky still runs.
- Rollback: U2 (kernel removal), defined under
  [Boot selection and recovery](#boot-selection-and-recovery).

### Phase 4: f3 host fdescfs window and observer key (A5, A6)

Runs while Rocky still runs normally, to keep downtime short. Uses the reviewed
controller `tools/lab/fdescfs-window.py` with the explicit prefix
`/usr/local/bin/python3.12 -I -B` and the pinned `fdescfs-namespace` helper.

0. `facts --inspector /var/tmp/hfs-validation-xz3/zq3-r1/bin/hfs-controlled-runtime`
   (read-only, B) plus the read-only host hashes listed in the
   [filled f3 plan](Descriptor-execution-window.md#filled-f3-plan-for-zq3-r1-2026-11-08);
   on the workstation `fdescfs-window-plan.py --pins
   docs/lab/zq3-fdescfs-window/pins.json --facts … --host-sha256 … --expires …
   --host-now <f3 date -u +%s> --out …` must accept them (any mismatch with the pins or Phase 1 stops) and
   prints the exact plan SHA256, which is reviewed before step 2.
1. Negative before setup (B): run only `TestDescriptorExecNativeUnsupported`
   from the staged test binary under `native-test-guard.py --profile host`; it
   must refuse before child/pin/observer effects. A skip counts as unrun.
2. `setup --plan … --plan-sha256 … --directory /var/tmp/hfs-fdescfs-windows/zq3-r1`:
   loads the stock `fdescfs.ko` (`847cd1f2…`) only if absent, mounts fdescfs
   `nodup,nosuid` on `/dev/fd` via `nmount` with `MNT_NOCOVER`, writes
   `owned.json`. **Disclosed: the module stays loaded after restoration.**
   FreeBSD's `fdesc_statfs` masks `f_flags` to `MNT_IGNORE`, so the new
   `mount -p` row shows only `rw` although `MNT_NOSUID` is set; setup accepts
   exactly `rw` or `rw,nosuid` there and nosuid rests on the pinned helper
   (p44, after zq3-r1's first setup refused the masked row).
3. `qualify` (B, under `native-test-guard.py --profile host`): rerun
   `TestDescriptorExecNativeQualification` on the **f3 host** with the final
   test bytes from source `b869710` (`f71f0bb8…`), plus
   `hfs-controlled-runtime --check-descriptor-exec` (must now report
   `fdescfs`, supported). This closes the host rerun transferred from w24.
4. `kldload /boot/kernel/fusefs.ko` (`b207adf6…`) only if absent, with
   before/after `kldstat`. **Disclosed: stays loaded afterwards.** This exact
   file is the window plan's declared `window_module` (path and pinned hash,
   from `pins.json`; task j44), so Phase 11's restore accepts it. Loading
   anything else here, or from another path, makes restore refuse.
5. Observer key (A6): on f3, `ssh-keygen -t ed25519 -N '' -C
   hfs-zq3-zq3-r1-observer -f /var/tmp/hfs-validation-xz3/zq3-r1/observer/id_ed25519`
   (root `0600`, never copied off f3, never Paul's key). Copy only
   `id_ed25519.pub` to the Rocky incoming directory, then run
   `observer_key.py enrol --run-id zq3-r1 --public-key IN/id_ed25519.pub
   --observer-sha256 <sha> --python /usr/bin/python3.9 --python-sha256 <hash>
   --expect-sha256 <Phase 1 authorized_keys SHA256>` (G6, implemented; same
   stdin form). It re-verifies the staged files, writes a durable receipt, then
   atomically appends exactly one line to `/root/.ssh/authorized_keys`:
   `restrict,from="10.20.0.133",command="/usr/bin/python3.9 -I /var/tmp/hfs-zq3-zq3-r1/observer.py --probe /var/tmp/hfs-zq3-zq3-r1/installed-probe.py --probe-sha256 c824b183… --python /usr/bin/python3.9 --python-sha256 …" ssh-ed25519 AAAA… hfs-zq3-zq3-r1-observer`,
   and prints the before/after SHA256 (after = SHA256(before + line)). The
   runtime's `GuestObserver.Host` is `10.20.0.123` and its `KnownHosts` is
   `rocky-known_hosts`.
- The `/dev/fd` mount stays in place through Phase 10: the runtime execs the
  exporter (`/dev/fd/4`) and the SSH observer (`/dev/fd/3`) through it. The
  observer `/dev/fd/3` production path is exercised for real in Phase 8, the
  first time natively.
- Failure: busy/changed FSID or unexpected mount stops; uncertain mount needs
  root inspection and explicit reconciliation before restore. Nothing is unloaded.
  Rocky's boot state is untouched at this point (no `next_entry`); remove the
  unselected kernel with U2 and restore the window (Phase 11 step 5).

### Phase 5: select the one-shot kernel and stop Rocky (A1, A3)

The one-shot selection is the last action before the stop, so the window in
which an abort leaves an unconsumed `next_entry` is as short as possible.

1. Final pre-stop checks (B): Phase 1 facts unchanged; `sysctl -n
   kern.boottime` equal to the Phase 1 value; `ps -p <Rocky bhyve PID> -o
   lstart=` (with `TZ=UTC`) equal to the Phase 1 value; `doas -n
   /usr/local/bin/python3.12 -I -B fdescfs-window.py facts --inspector …`,
   whose parsed `descriptor` must equal the parsed `owned.json` (zq3-r1, R5: an unnoticed external f3 reboot during a pause
   restarted Rocky and dropped the window, and select/stop then ran on the new
   instance). Any difference stops the run before step 2: nothing is selected
   yet, so U1 is not needed; see the failure rule below. Memory pre-estimate (current free +
   inactive + reclaimable ARC + Rocky's resident bhyve memory must cover the
   Phase 6 requirement); latest zrepl snapshot of
   `zroot/bhyve/rocky` recorded (disk-level reference only; no manual snapshot
   and no automatic rollback).
2. Rocky: `grub2-reboot <entry id recorded in Phase 3>`; verify `next_entry`
   names it, `saved_entry` and `grubby --default-kernel` unchanged. Entry
   point: `rocky_kernel.py select` (refuses unless the installed bytes and
   BLS entry still match the install record; any failure runs U1 at once).
3. f3, immediately after step 2: `tools/lab/vms.sh --approved-action stop-rocky`
   (clean ACPI `vm stop`, waits, never forces). Then verify `vm list` shows
   Rocky Stopped, no `bhyve: rocky` process, no `/dev/vmm/rocky`, its tap gone.
- Failure in step 1: step 2 has not run, so `next_entry` is still empty
  (confirm with `grub2-editenv - list` once Rocky answers, and the
  `rocky_kernel.py` state journal shows no select). Two cases:
  - f3 booted since Phase 1 (R5): the window mount and modules are gone.
    Record the retained window directory by hand (`fdescfs-window.py` has no
    action for a vanished mount). Rocky has AUTO yes, so it normally runs
    again: then U2. If it is down, start it normally (`vms.sh --approved-action
    start-rocky`, after the R6 PoC share check), then U2.
  - Otherwise (same boot, Rocky runs, a PID or mount mismatch): inspect and
    stop; U2, then restore the window (Phase 11 step 5) only if `facts` still
    shows the owned FSID.
- Failure in step 2, or if step 3 refuses before stopping anything: Rocky
  still runs, so run **U1** (cancel the one-shot) and verify, then U2.
- Failure in step 3 (timeout or SSH loss is not success): keep waiting within
  the helper's bound, then stop and inspect; never `vm poweroff`. If Rocky still
  answers pinned SSH, run U1. If it is stopped, `next_entry` is still set:
  recovery is **U3**.
- Restoration: Phase 12, or U3 for any abort before the Phase 8 boot.

### Phase 6: post-stop admission (f3, B)

- Actions: re-check memory now that Rocky's 12 GiB are free: free + inactive +
  reclaimable ARC (above `kstat.zfs.misc.arcstats.c_min`, the effective floor
  `runtime-guard.py` also uses) must cover the enforced guard ceilings,
  bhyve 6 GiB + 512 MiB and runtime 4 GiB + 256 MiB, plus a 3 GiB host
  reserve: 13.75 GiB (14,763,950,080 bytes). The earlier 15 GiB bar counted the
  proposal's 6 GiB runtime envelope, which the 4 GiB `Budget.HostBytes` cap and
  the guard never let the runtime use; on a freshly booted f3 it was
  unreachable (zq3-r1, R6: 16,003,514,952 bytes measured against
  16,106,127,360). The guard still stops the runtime if available memory drops
  below 512 MiB. Swap unused; all FreeBSD VMs still stopped; `/dev/fd` still the
  owned fdescfs FSID.
- Failure: restore the fdescfs window (Phase 11 step 5), then **U3**, not a
  plain normal start: `next_entry` is still set, so a plain start would boot
  the test kernel.

### Phase 7: host setup for the controlled launch (f3, A4)

The 12 GiB vm-bhyve config stays unchanged; the candidate bhyve runs directly
as the runtime's own child. G4 is implemented as preparation (not yet run on
f3): [`tools/lab/directlaunch`](../../tools/lab/directlaunch/README.md).

- `argv.py generate` (after Phase 1, before Phase 2 staging) rebuilds the argv from the Phase 1 launch facts
  and pins it in a canonical record; any drift from the reviewed baseline
  (rocky.conf, vm-run, firmware symlink text `../edk2-bhyve/BHYVE_UEFI.fd`
  and target hash, every logged hardware token) refuses. For
  run `zq3-r1` with tap `tap100` the argv is (vm-run order: devices, options,
  console, name):
  `-s 0,hostbridge -s 31,lpc -s 0:4:0,nvme,/zroot/bhyve/rocky/disk0.img
  -s 0:5:0,virtio-net,tap100,mac=58:9c:fc:4c:38:84
  -c 4 -m 6G -AHPw
  -l bootrom,/usr/local/share/uefi-firmware/BHYVE_UEFI.fd,/var/tmp/hfs-validation-xz3/zq3-r1/launch/uefi-vars.fd
  -s 0:7:0,virtio-9p -o pci.0.7.0.sharename=hfs0 -o pci.0.7.0.hfs-controller=<session0-uuid>
  -s 0:8:0,virtio-9p -o pci.0.8.0.sharename=hfs1 -o pci.0.8.0.hfs-controller=<session1-uuid>
  -U e10b6225-44d0-11f1-882a-e8ff1ed7f3d7 -u -l com1,/dev/nmdm-hfs-zq3-r1.1A hfs-zq3-r1`.
  The earlier sketch here would not have worked: bhyve's virtio-9p `-s`
  parser reads `sharename=hfs0` as share `sharename` with path `hfs0` and
  refuses the second `=`, so the controller is set with `-o` after the
  device's `-s`. The sketch also lacked vm-bhyve's `-w` and `-u`. The PoC
  device and the fbuf/VNC device are dropped; NIC slot and MAC are kept, so
  Rocky keeps its interface name and address. A guest reboot ends the child
  (no vm-run restart loop).
- `host.py setup --record … --record-sha256 …` (f3, root): re-collects the
  facts and refuses on drift (including a changed UEFI vars hash), unless all
  vm-bhyve guests are Stopped, `/dev/vmm` is empty, the tap and nmdm names are
  free and the launch directory is new. Then it copies
  `/zroot/bhyve/rocky/uefi-vars.fd` (`e78b9144…` on 2026-11-05; re-pinned
  from fresh facts) into `<staging>/launch/` (original only read) and creates
  the pinned tap: `create`, description `homelabfs-hfs-zq3-r1`, `vm-public
  addm`, `up`, each with an intent record.
- Initialise the disposable volume and node control directory under the
  staging prefix (G3, implemented in s34, pending review/pinning):
  `/usr/local/bin/python3.12 -I -B tools/lab/runtime-volume-init.py --plan
  <volume-init.json> --plan-sha256 … --record …` drives the staged pinned
  `homelabfs` helper (actual `cmd/homelabfs` from `e0cca0c`, reproduced SHA256
  `8c02b98f…`) through node-init, volume-init and inspect, writes the volume
  config and the empty `0700` mountpoint, and never deletes on failure.
- Stage the private `controlled-runtime/1` config (G2) generated and verified
  in Phase 0: `Exporter` = staged bhyve, `Args` equal to the record element
  by element (`argv.py verify` and `hfs-runtime-prep check-argv` passed).
- Changes: one transient tap in `vm-public`, the vars copy, the volume and
  records under the staging prefix; bhyve creates the nmdm pair and the vmm
  context.
- Rollback: Phase 11 step 4 (`host.py cleanup`).
- Failure (no child started): `host.py cleanup --no-child` (destroys the tap),
  restore the window, then **U3**.

### Phase 8: start the runtime and staged bootstrap (f3 + Rocky, A3, A4, A7)

- Driver (G1, implemented in x34, locally tested only): on the workstation,
  `python3 -I -B tools/lab/retained9p/bootstrap.py run --plan <bootstrap
  plan> --plan-sha256 … --output <new dir>`
  ([README](../../tools/lab/retained9p/README.md#phase-8-bootstrap-orchestrator-zq3-g1)).
  It runs every step below in this order, each with a durable intent and
  result in `<new dir>/journal.jsonl`, refuses out-of-order steps, stops at
  the first failed or uncertain step and never replays one (no resume, no
  reused output directory). It first checks on f3 that the guard script and
  runtime match their pins and that the guard run directory, OwnerDirectory
  and EnrollmentFile do not exist yet, while the EnrollmentFile's parent
  directory does (root, `0700`; Phase 7 must create it).
- Action: in the foreground of one f3 SSH session that the driver starts
  detached from its terminal (no tty), the outer resource guard launches the
  runtime. If that SSH connection is lost, sshd sends no SIGHUP: the guard and
  the runtime keep running under the guard's bounds, the driver stops all
  dependent steps and records the transport loss, and the operator reconnects
  to f3 to inspect; the runtime's bootstrap timeout or the guard limits end it.
  The guard command is:
  `doas -n /usr/local/bin/python3.12 -I -B
  /var/tmp/hfs-validation-xz3/zq3-r1/bin/runtime-guard.py --run-dir
  /var/tmp/hfs-validation-xz3/zq3-r1/guard --runtime-sha256 34fb05b2…
  --vm-name hfs-zq3-r1 --disk-root /var/tmp/hfs-validation-xz3/zq3-r1
  --disk-root /var/tmp/hfs-fdescfs-windows/zq3-r1 --skip-path <native
  mountpoint from the run config> --
  /var/tmp/hfs-validation-xz3/zq3-r1/bin/hfs-controlled-runtime --config
  <private config> --disposable-validation` (the runtime refuses to start
  without that flag; q54, zq3-r2 Phase 8). Runtime stdout (READY) and stderr go to `guard/runtime.stdout`
  and `guard/runtime.stderr`. The guard refuses unless no VM context exists
  and every limit already holds. On the first breach (limits table, a foreign
  VM context such as `rocky`, an unobservable sample, or a SIGHUP/SIGINT/SIGTERM
  to the guard) it writes `breach.json` and sends exactly one SIGTERM to the
  runtime, which handles it as its retained stop. It never signals bhyve,
  Rocky or a process group and never sends SIGKILL. The disk walk never
  touches the native FUSE mount (cached `mount -p` table plus `--skip-path`).
  `--on-breach report` would only record, not even stopping on Ctrl-C or the
  time limit, so it is not used here. Exit 3 means the runtime still ran after
  the grace period: inspect, do not escalate.
- The runtime launches the bhyve child; GRUB consumes the one-shot entry and
  boots `6.1.190-hfs-xz3`. Record the consumption evidence: the observed guest
  kernel release and boot ID (the driver's `guest-ready` result), plus the
  console log (collected separately; the driver does not read it). The runtime emits its
  proposal (`AWAITING_OPERATOR_ENROLLMENT`) and waits for the enrollment
  handoff; it observes the guest only after that handoff, with the handoff's
  `Observer` (`admitOperatorEnrollment` in
  `internal/controlledruntime/driver_phases.go`). Within the 120 s bootstrap,
  counted by the driver from the launch, the following must complete in
  order (G1 driver steps in brackets):
  0. (A3) Root over pinned root SSH to the guest, once it answers with the
     test kernel release [`guest-ready`]: `rocky_kernel.py admission` (G5)
     waits for `udevadm settle`, then writes
     `/etc/homelabfs/installed-admission-v1.json` from the live loaded-module
     set (see Phase 3 step 5). Record its SHA256 and the boot ID it reports,
     and read the provider nonce from `/sys/kernel/homelabfs/admission_v1`
     and the `virtioN` name of each share tag (read-only) [`admission`, one
     SSH round trip with the pinned script]. Fill `BootID`, `ProviderNonce`,
     the device names and `Observer.ManifestHash` into the enrollment request
     with `hfs-runtime-prep enroll-request` [`enroll-request`]. This SSH round
     trip counts against the 120 s.
  1. Signer (`hfs-retained9p-operator enroll`, on the workstation, with the
     operator key under A7) emits `provision.json` and `enrollment.json`
     [`signed-enrollment`]. The driver never reads the key: it prints the
     exact command, waits for its fresh output directory and refuses signed
     files that differ from the measured boot ID, provider nonce, manifest
     hash or exact proposal, or that disagree with each other.
  2. Root writes `provision.json` to `/sys/kernel/homelabfs/provision_v1` in
     one write [`provision`], then transfers `enrollment.json` atomically to
     the host `EnrollmentFile` [`enrollment-handoff`].
  3. The runtime reads the handoff and observes the guest through the
     observer key: actual boot ID, provider nonce, complete module inventory,
     `admission_v1`, and the manifest's SHA256 equal to
     `Observer.ManifestHash`.
  4. At `AWAITING_GUEST_BIND` [`awaiting-bind`]: `hfs-runtime-prep
     bind-request` [`bind-request`], then `hfs-retained9p-operator bind`
     emits `bind.json` [`signed-bind`, checked against the runtime's
     controller and provision hash]; one write to
     `/sys/kernel/homelabfs/bind_v1` [`bind`].
  5. Guest mounts both domains [`mount-0`, `mount-1`]:
     `mount -t 9p -o trans=virtio,version=9P2000.L.hfs1.4,cache=mmap,msize=65536
     hfs0 /var/tmp/hfs-zq3-zq3-r1/m0` and the same for `hfs1` on `m1`; both
     HELLOs; runtime prints READY with the control binding [`ready`].
  6. After READY, under its own bound rather than the 120 s: the usability
     gate (k64) [`usable`]. zq3-r6 and r7 reached READY with every step `ok`,
     but the guest had already blocked both sessions, so every operation
     returned `EACCES`.
     - Root lstats both mount roots in one guest SSH round trip. A root is
       stat'ed only while it is still exactly one `9p` mount of its tag. Each
       lstat runs in a child bounded by 8 s that is never killed.
     - Nothing is written into the mounts, but the gate is **not read-only on
       the host**. Each lstat registers its child as an hfs process and sends
       a GETATTR claim, and the child's exit queues a PROCESS_EXIT claim. The
       two probes add about four durable claims to each provider ledger
       before T1. In an r7-type failure these claims stay unresolved too.
     - The first probe runs right away. The final probe runs once 40 s have
       passed since `mount-1` ended. A host poll in between confirms the
       guard is unbreached and READY is still the last record. The step's
       bound is the settle point plus 30 s.
     - Why 40 s: the guest checks its 30 s QUERY deadline only after each
       QUERY reply, and each QUERY has its own 30 s transport deadline, so
       the block lands at 30 s plus one QUERY round trip. r7's round trips
       took milliseconds, so 10 s of margin is enough. A QUERY stalled
       longer would most likely also stall the probe's GETATTR past 8 s,
       which fails the gate.
     - Both roots must answer every probe. Otherwise `usable` ends `failed`.
       The journal detail names `probe` (`first`, `settle`, `baseline`,
       `final` or `claims`),
       `roots` and `errno` (13 for `EACCES`, null when no lstat reported one).
       The run exits 1 (`stopped_at: usable`), and `fill_plan.py batch`
       refuses it, as it refuses a pre-k64 `/1` or pre-b84 `/2` journal.
     - Nothing is retried, remounted, written or unmounted. Go to Phase 11
       with inspection first.
     - Host-side claim check (b84; zq3-r8 passed m0's lstat while the
       probe child's own PROCESS_EXIT stayed RESERVED and blocked m0 30 s
       later). Root on f3 runs the pinned runtime once per poll in client
       mode (`--operation observe`, the READY socket and runtime UUID,
       sequences 1, 2, …). Before the final probe, neither provider ledger
       may hold an open claim (`baseline`). After it, OBSERVE every 5 s until
       each provider shows a TERMINAL GETATTR and PROCESS_EXIT numbered after
       the baseline (opcode and number range, not process identity; retired
       numbers count as TERMINAL) and nothing open (`claims`), within 30 s
       of the final probe. A failure names each open claim (number, opcode,
       status, stage) and the summary's `usable_note` says a claim failed;
       an OBSERVE that is refused, incomplete or from a pre-b84 runtime fails
       with its own cause and note. Each OBSERVE takes a fresh SSH
       observation of the guest (a failed one fences the Admission) and
       consumes a lifecycle sequence: `summary.json` records
       `observe_sequence_last`, and Phase 9 starts above it. The gate's bound
       grows to about 100 s after `mount-1` (see the
       [README](../../tools/lab/retained9p/README.md#phase-8-bootstrap-orchestrator-zq3-g1)).
- Changes on Rocky's disk (A3): `/etc/homelabfs/installed-admission-v1.json`
  (and `/etc/homelabfs/` if absent), the G5 journal lines; kdump may create
  `/boot/initramfs-6.1.190-hfs-xz3kdump.img`. U2 removes them.
- Evidence: runtime stdout/stderr (bounded drains), READY record,
  `runtime-budget.json`, observer receipts, the signed files, the admission
  command output and manifest SHA256, the `usable` probe results (both
  roots, both probes), the driver's `journal.jsonl`, `calls/`,
  `work/` and `summary.json`, and the guard's `intent.json`,
  `observations.jsonl`, any `breach.json`/`stop-*.json` and `receipt.json`
  (written when the runtime exits).
- Failure: missing/late/changed records refuse and **retain** native and guest
  resources; nothing falls back to stock 9p callbacks. The driver signals,
  kills, unmounts and deletes nothing remote; an `uncertain` step is inspected
  by root, never repeated. Go to Phase 11 with inspection first. If the child exited or failed before GRUB ran (no guest
  kernel observed and no GRUB output on the console), treat `next_entry` as
  unconsumed and restore with **U3** after the child is positively gone.

### Phase 9: READY admission (B)

- `hfs-controlled-runtime --control <owner>/control.sock --operation observe
  --runtime <uuid> --sequence <N>`: every section `complete`, publication null,
  both domains admitted. N is `observe_sequence_last + 1` from the bootstrap's
  `summary.json`: the b84 `usable` gate already consumed sequences 1 to
  `observe_sequence_last`, and the runtime refuses a sequence that is not
  strictly larger (a refusal consumes nothing; retry with the right N).
  Guest: `findmnt` shows exactly the two mounts.
- Every later literal sequence shifts by the same `observe_sequence_last`
  (call it L): the T1 baseline OBSERVE uses L + 2 (it was literal 2), and the
  run sheet's expected-sequence table moves up by L (case baselines/retained
  L+2/L+3 … L+8/L+9, T9 pre-arm L+10, ARM L+11, post-CALL L+12, T10
  post-quiesce L+13, RESTART L+14 with T5 deferred). `correlate.py command`
  already derives its sequence from the latest reply. A number at or below
  the last accepted one is refused without being consumed
  (`lifecycle_control.go` `dispatch`), so a stale literal costs a retry, not
  the sequence.

### Phase 10: gate matrix (B inside the window)

Stop dependent steps on the first failure. Results are pass, fail,
unsupported, unrun or uncertain; a partial result never completes zq3.

| Step | Gate (Deployment-tests) | How |
|---|---|---|
| T1 | UID/GID incl. full 1024-entry groups, setgid/umask create, sticky, special bits, cross-domain metadata | `check-retained9p.sh probe` (case `credentials`), then host correlation (`correlate.py case` from an OBSERVE taken right before the dispatch and one while the payload is retained), then `release`, then `next` |
| T2 | write acknowledgement and sync propagation (O_SYNC/O_DSYNC/fdatasync/fsync, directory fsync, mmap MS_SYNC) | case `sync` + correlation of journal `Through/Applied` via OBSERVE + `release`, `next` |
| T3 | metadata: rename, hardlink, truncate, open-unlinked, cross-domain freshness | case `metadata` + correlation + `release`, `next` |
| T4 | foreign user namespace refusal (boundary only) | case `namespace` + correlation + `release`, then `next` only if T5 is planned (with T5 deferred, T4's release completes the batch and `next` refuses: no remaining case) |
| T5 | `cache=mmap`: full 4 MiB mapped canary, cross-domain hash | **deferred (q64)**: the canary's view lifetimes exceed the 4 GiB budget and running it would block both sessions for T6-T11, so the batch plan declares `mapped-pressure` in `deferred` (z64) and runs T1-T4 only; recorded as deferred, never as a pass. Once q64 is fixed: case `mapped-pressure` + correlation + `release` |
| T6 | locks within one guest mount | `tools/lab/guest-lock-probe.py` on `m0` (POSIX/flock conflicts, blocking waits, GETLK, close/exit cleanup) |
| T6b | locks across clients | same probe across `m0`/`m1`: **recorded as unsupported** per the accepted lock scope, never as a pass |
| T7 | ordinary pod namespace profile: separate mount/PID/net/UTS/IPC namespaces in `init_user_ns`, numeric IDs/full groups, SELinux enforcing, two workloads bind-sharing `m0` | under A8, after T6: `pod_fixture.py run --request ABS` (rootfs built earlier with `build`, see G9); pass = `t7_pass: true`, which also removes the two exited containers and the pod payload normally; any failure or `uncertain` preserves both. The SELinux access of `container_t` to the 9p label is only known now: a denial is a T7 failure, never relabelled or disabled |
| T9 | v24 lost reply | OBSERVE; `correlate.py command --operation arm-lost-reply` prints the ARM argv (m0's session, current cut/epoch); root runs it; `lifecycle --step lost-reply-call` (one mkdir in m0); OBSERVE; `lifecycle --step lost-reply-reread` (m1 then m0, no second mkdir); `correlate.py lost-reply` records the exposure (UNCERTAIN expected), source effect since arming and a classification decided by the unarmed m1 (expected: `dropped-before-mutation` with m0 blocked, `EHOSTDOWN`; with a d44-rebuilt runtime and `--lost-intent MKDIR`: `dropped-reply-effect-committed-and-visible`, see below), never a pass |
| T10 | daemon restart via u24 | `lifecycle --step quiesce` (syncfs, guest `drop_caches` 3 twice: view leases released, lookups FORGET; no VIEW_ACQUIRE refresh; on the blocked m0 the open and FORGETs fail), OBSERVE taken after the quiesce result, then `correlate.py command --operation restart` (refuses unless that OBSERVE is later than every bound one and shows view-controller `Leases`/`Reservations` 0, no `Recall`, `Matches`; then recorded as unrun) and root runs the printed `--operation restart`; RESTARTED or FENCED are both recorded (FENCED, fencing then runtime exit, is Paul's accepted behavior and the expected outcome after T9); **last step that needs the live runtime** |
| T11 | remount / VM restart need, K04 host side | after the runtime exited (FENCED T10), with m1 still mounted: root takes `host_snapshot.py --label before` of the runtime's FUSE mount point on f3, `lifecycle --step after-loss` (lstat, list, exclusive create in **m1**, never retried), `host_snapshot.py --label after`; `correlate.py k04` (with the Phase 7 `baseline` snapshot of the empty mount point, required) records whether guest I/O failed and nothing reached a raw directory. **Limitation:** m0 cannot be probed (T9 blocked its session, so its refusals are local); if m1 also shows the guest's own `EHOSTDOWN` (T9 re-read, T10 quiesce or the first after-loss attempt) the record flags the probe as vacuous and K04 is not observed. If T10 is RESTARTED, the runtime keeps serving, no supported stop exists here and T11 is unrun. Replacement FUSE/exporter and guest RESUME are blocked by design, so the recorded answer is "VM restart required", with the refusals as evidence |
| T8 | K04 guest side: no writes to an empty fallback | **runs last, after T11** (orchestrator decision: T9 before T8; T11 needs m1 mounted): normal `umount m1`; a probe naming `m1` must refuse at admission ("exact mountpoint not present once") before any open; the empty directory stays empty. Needs no live runtime; if the unmount refuses or hangs after host loss, T8 is unrun and Phase 11 handles the mount |

Execution order: **T1-T7, T9, T10, T11, T8** (table order; T5 is skipped while it is deferred, q64).

Mid-release failure (T1-T5): the guest consumes the retention record before
its first unlink. If an unlink or the deadline then fails, part of the payload
can remain, `released-CASE.json` exists and the controller stays at
`release-dispatching-uncertain` with `error`. Do not re-release or replay:
root inspects the leaf against the inventory in `released-CASE.json`, keeps the
leftovers and evidence, marks T1-T5 incomplete, and any repeat uses a new run ID.

T9-T11 guest steps (u34) run only after every planned release (T1-T4 while
T5 is deferred: the controller's `syscall_batch_success` and the
`correlate.py` ARM precondition count the plan's cases, and the run state,
`status` and the T9/T11 records repeat the deferral while
`zq3_acceptance_complete` stays false), once each and in
order, through the same pinned `guest.py`. They admit their own domain (m0 for
T9/T10, m1 for T11) from mountinfo without touching it and record the other
as present or absent. Expect T9 not to show a clean MKDIR lost reply with the
current guest kernel: its path walk sends GETATTR and LOOKUP CALLs before
MKDIR, the fixture drops the first one (a background CALL on m0 could also
take it), and the guest then blocks m0's session (`EHOSTDOWN`) with no
userspace QUERY. The correlator decides the leaf state from the unarmed m1 and
records that as `dropped-before-mutation` (or `fixture-not-fired`), and the
T10 RESTART is then expected to FENCE, which is the host loss T11 probes on
m1.

**MKDIR-targeted variant (d44, needs a rebuilt runtime).** No guest warm-up
can make MKDIR the first CALL: inside the one `mkdir(2)` the guest always
sends GETATTR(parent) for `may_lookup`, LOOKUP(name) because
`hfs_d_revalidate` never keeps a dentry, and GETATTR(parent) again for
`may_create`, all uncached and none affected by `cache=mmap` (the hfs mount
bypasses v9fs caching). Evidence with patch line numbers is in the
[guestbridge analysis](../../internal/guestbridge/README.md#targeting-the-mkdir-reply-d44).
d44 therefore adds per-operation arming to the runtime: ARM operation code 2,
`correlate.py command --operation arm-lost-reply --lost-intent MKDIR`, drops
only a CALL whose guest intent opcode is MKDIR and lets the earlier CALLs
through. The superseded `e0cca0c` candidate refuses the flag; the f44 rebuild
(`b203799e…`, source `defd974`) carries it, with its window pins rebuilt too,
and so do the r54 (`5eb42c50…`, t54) and w54 (`89c928d5…`, y54) candidates,
which reuse that daemon byte for byte, the z54 daemon rebuild (`2e0412a6…`,
daemon `39c92d01…` from `a20aca1`), whose source keeps d44, the 964 kernel
rebuild (`e01fab21…`), which reused that z54 daemon, the c64+d64 rebuild
(`66b5bb2b…`, daemon `9b7eed8d…` from `060dea0`) and the current y64 rebuild
(`867ede36…`, daemon `34fb05b2…` from `b869710`), whose sources keep d44.
With it, the T9 expectation becomes `dropped-reply-effect-committed-and-visible`
(record `armed_intent`/`dropped_opcode` MKDIR, mkdir fails in m0, m1 sees the
directory, m0 still blocked), and T10 is still expected to FENCE because the
blocked guest never re-reads the dropped response. Without the rebuild, run
T9 unfiltered as above.

A step deadline ends the step as failed and uncertain; it is never
recorded as an errno or a refusal. See the
[probe tools](../../tools/lab/retained9p/README.md#guest-lifecycle-steps-zq3-g10-g11).

Not runnable in this run and reported as unrun: PTE/TLB/busy-pin/GUP
exclusion and funded drains (**GAP G14**: no admitted pin primitive),
distributed SAME-volume writers (ov3/pv3).

### Phase 11: cleanup (f3 + Rocky, within A1/A4/A5/A6)

Order matters; each step needs the previous one confirmed.

1. Collect all controller, runtime, observer and guest evidence off the hosts
   into private storage. Each case `stdout.log` contains that case's
   `release_token`: spent once its release succeeded, still live for a case
   never released. Before anything is copied into `docs/lab/results`, redact
   the tokens or record next to each one whether it is spent; the unredacted
   originals (whose hashes `state.json` records) stay private.
2. Guest (still under the custom kernel): close probes; normal `umount` of
   `m0`/`m1` (busy: stop and preserve). Remove the observer line with
   `observer_key.py remove --run-id zq3-r1 --expect-sha256 <enrol after_sha256>`:
   it deletes only that exact line and must report `restored_pre_run: true`
   (file SHA256 equals the pre-run value). If unrelated keys changed meanwhile,
   it refuses; only an explicit rerun with the current hash removes just its
   line and reports `restored_pre_run: false` for Paul to review. Verify
   `grub2-editenv list`: `next_entry` empty (consumed by the Phase 8 boot) and
   `saved_entry` unchanged; if `next_entry` is somehow still set, run U1 now.
3. Guest: `systemctl poweroff`. On f3 confirm the bhyve child exited (runtime
   reports exit; no process; if the runtime already exited, wait for the child).
   Never kill the child.
4. f3: `host.py cleanup --record … --record-sha256 … --child-pid <exporter PID
   from runtime evidence>` (G4): refuses while that PID exists or any process
   carries the name `hfs-zq3-r1`, and never signals anything. Otherwise it
   destroys `/dev/vmm/hfs-zq3-r1` only if `/dev/vmm` was empty before setup
   and the name is no vm-bhyve guest, destroys the pinned tap only if its
   description is the run label, reports the inert nmdm nodes, keeps the vars
   copy with its hash and checks the original vars are unchanged. Then normal
   unmount of the native FUSE mount if the runtime left it (busy: preserve);
   delete the observer private key file.
5. f3: `fdescfs-window.py restore …`: normal `unmount` by exact FSID; compares
   final state with the post-load baseline, accepting only the plan-declared
   `fusefs.ko` (exact path, registered `fusefs` module, on-disk SHA256
   `b207adf6…`) as an extra loaded file (j44). Any other module difference,
   a missing `fdescfs.ko` or a changed mount table refuses and retains
   `owned.json`. `restoration-summary.json` states that `fdescfs` stays loaded
   and records the loaded `fusefs.ko` (`window_module_loaded`), which also
   stays loaded.
- Failure: a busy unmount, surviving child or changed FSID stops cleanup;
  Rocky's normal start waits until the child is positively gone.

### Phase 12: normal restoration and final state (A1, A3)

1. f3: `tools/lab/vms.sh --approved-action start-rocky` (normal vm-bhyve
   12 GiB config, unchanged). GRUB boots the stock default **only because** the
   Phase 8 boot consumed `next_entry` and Phase 11 step 2 verified it empty.
   Without that verification (abort before the Phase 8 boot, or a guest that
   could not be inspected), use U3 instead of this step.
2. Rocky: verify running kernel equals the Phase 1 default, `saved_entry`
   unchanged, `next_entry` empty, SELinux enforcing, `authorized_keys` hash
   equals pre-run, memory as before, `/.autorelabel` absent (R7: a mark here
   means a boot ran without SELinux).
3. Remove the custom kernel (U2), unless Paul chose to keep it.
4. f3: delete the staging prefix after evidence is safe (or keep, if chosen).
5. Final inventory: Rocky running with a new PID, `freebsd` and `hfs-test0..2`
   stopped, no `/dev/vmm/hfs-*`, no extra tap, `/dev/fd` devfs, `fdescfs` and
   `fusefs` loaded (disclosed), no fuse mounts, pool headroom as before.

Expected final state: identical to the baseline except for the two loaded
kernel modules on f3, the new Rocky PID, the consumed one-shot boot record,
and retained evidence files (plus anything Paul chose to keep).

## Boot selection and recovery

Rocky's kernel choice lives in its own disk's grubenv. GRUB clears
`next_entry` while loading its configuration, before it boots anything, so a
one-shot entry is consumed by the first boot that reaches GRUB, whatever kernel
that boot then runs. The 2026-10-25 9p run used the same `grub2-reboot`
mechanism with the ELRepo kernel and Rocky returned to its stock default.

Entry points (G5, implemented in r34): `rocky_kernel.py cancel` (U1),
`remove` (U2), `check --expect test|stock|any` (U3 step 3, Phase 11 step 2,
Phase 12 step 2) and `reboot-to-stock` (U3 step 4). A journal intent without
a result blocks install, select, remove, reboot-to-stock and admission until
root inspects and records it with `reconcile`; `cancel`, `check` and `status`
still run. install, select and remove require exactly the pinned stock
release to be running (another installed kernel, such as ELRepo 7.0.3,
refuses); admission and reboot-to-stock require exactly the test kernel.

- **U1, cancel the one-shot (Rocky running):** `grub2-editenv - unset
  next_entry`; verify `next_entry` empty, `saved_entry` and `grubby
  --default-kernel` unchanged. Mandatory for any failure before Rocky is stopped
  once Phase 5 step 2 has run. `cancel` also runs with a torn journal; if even
  it cannot run, the manual fallback is exactly `grub2-editenv - unset
  next_entry` followed by `grub2-editenv - list` and `grubby --default-kernel`.
- **U2, remove the custom kernel (Rocky running its stock kernel):**
  `grubby --remove-kernel=/boot/vmlinuz-6.1.190-hfs-xz3`; remove the copied
  `/boot` files, the initramfs, the kdump initramfs
  `/boot/initramfs-6.1.190-hfs-xz3kdump.img` if kdump created it in a
  test-kernel boot, `/lib/modules/6.1.190-hfs-xz3` and
  `/etc/homelabfs/installed-admission-v1.json`; the SHA256 of every removed
  file is recorded. Verify `grubby --info=ALL` equals the pre-run listing.
  Never while running the test kernel itself; refused while `next_entry` is
  set (run U1 first) and, before deleting anything, if the module tree holds
  anything the plan or depmod did not create.
- **U3, recovery after Rocky stopped with `next_entry` still set** (aborts in
  Phases 5 step 3 to 8 before the controlled boot reached GRUB). There is no way
  to edit grubenv while Rocky is stopped without touching its disk from f3,
  which this plan does not do. Honest recovery path, approved as part of A3:
  1. Confirm no candidate bhyve child or `/dev/vmm/hfs-zq3-<run>` exists.
  2. `tools/lab/vms.sh --approved-action start-rocky`: the normal 12 GiB
     config. GRUB consumes `next_entry` and boots `6.1.190-hfs-xz3` **once**,
     now under the normal config (with the unused PoC 9p device and normal
     network; no HomelabFS controller exists, so the provider stays unenrolled;
     services that need modules the test kernel lacks may fail for this boot).
  3. Wait (bounded, 10 minutes) for pinned root SSH; record `uname -r`; verify
     `next_entry` empty and `saved_entry` unchanged.
  4. `systemctl reboot` (clean); verify Rocky comes back on the Phase 1 default
     kernel; then U2 and the Phase 12 checks. `reboot-to-stock` requires a
     passing `check --expect test` in the same boot and refuses once the
     admission manifest exists. That is its only marker: it cannot itself tell
     a U3 boot from the controlled Phase 8 boot (which ends with Phase 11's
     poweroff, never a reboot). A Phase 8 boot that failed before admission
     leaves no manifest, and a manifest written by mistake in a U3 boot makes
     it refuse wrongly. **The operator must know which boot they are in.**
  - Alternative U3-b (also needs Paul's yes, a human at the console): during
    step 2, open the Rocky VNC console (`127.0.0.1:5900` on f3) and choose the
    stock entry in the GRUB menu. `next_entry` is already cleared at that point,
    so Rocky boots stock directly, without the test-kernel boot.
  - If the test kernel does not come up in step 3 (hang or panic), a clean
    guest shutdown is impossible; getting out needs a forced power-off
    (`vm poweroff -f`), which this plan never does on its own. It stops and asks
    Paul for that specific action.

Not chosen: booting the controlled launch through `grub-bhyve` with an explicit
kernel would avoid touching Rocky's grubenv, but needs a package installed on
f3 and a different firmware/boot path than Rocky's UEFI setup. It can be
proposed if Paul prefers no grubenv change.

## Rocky downtime

Normal Rocky is unavailable from Phase 5 step 3 (the stop) to Phase 12. The
figures are estimates counted from that stop; no tool enforces them:

| Part | Expected |
|---|---|
| Clean stop | 1-2 min |
| Host setup, runtime start, bootstrap | 5 min (120 s bootstrap bound, then about 100 s for the `usable` gate with its b84 claim check) |
| Gate matrix T1-T11 | 25-35 min |
| Cleanup | 5-10 min |
| Normal start | 2 min |
| **Total** | **about 45-60 min; planning estimate up to 75 min** |
| U3 instead of a normal start (abort before the controlled boot) | about 10 more minutes (one test-kernel boot plus a clean reboot) |

Exception: after an uncertain failure, normal Rocky stays stopped until the
candidate bhyve child and its vmm context are positively gone. That has no
fixed bound; it ends when root reconciles it. A hung test kernel during U3
also stops until Paul decides on a forced power-off.

## Risks

- R1 Memory: the 13.75 GiB bar (enforced guard ceilings plus 3 GiB reserve)
  leaves about 0.7 GB over the zq3-r1 fresh-boot measurement: free +
  inactive 15.31 GB plus about 166 MB of ARC above `arcstats.c_min` (about
  527 MB; `vfs.zfs.arc_min` 0 means the default floor, not none). Phase 6
  measures it and refuses otherwise.
- R2 The custom kernel fails to boot (initramfs/LVM). The one-shot entry was
  consumed when GRUB ran, so the next normal start boots stock. But a hung or
  panicked guest cannot be shut down from inside; ending that child (in Phase 8
  or in U3) needs a forced power-off, which requires Paul's separate explicit
  approval at that time. The runtime bootstrap times out and preserves
  resources meanwhile.
- R2b Abort after the Phase 5 stop but before the controlled boot reaches GRUB:
  `next_entry` is unconsumed and a plain normal start would boot the test
  kernel. Recovery is U3 (one test-kernel boot, then a clean reboot), or U3-b.
- R3 Disk image shared: the direct launch uses Rocky's real disk. vm-bhyve must
  not start `rocky` while the child runs (a concurrent start would corrupt it).
  `vms.sh start-rocky` and `start-lab` refuse, before any `vm start`, while any
  `/dev/vmm/hfs-zq3-*` context or process title token `hfs-zq3-*` exists, and
  never kill anything (b44; offline test `tools/lab/test_vms_sh.py`). A manual
  `vm start rocky` stays unguarded. AUTO=yes only acts on an f3 boot, which also
  ends the child.
- R4 A RESTART FENCE ends the runtime while the bhyve child keeps running;
  Phase 11 shuts the guest down from inside.
- R5 Retained `fdescfs`/`fusefs` modules change f3's kernel state until reboot.
- R6 If the host path of the old PoC line is gone on f3, vm-bhyve cannot start
  Rocky normally in Phase 12. Phase 1 must confirm it exists, or the run waits
  for a separately approved config decision. This plan does not edit
  `rocky.conf`.
- R7 Guest SELinux. Rocky marks `/.autorelabel` on every boot that runs
  without SELinux (`selinux-autorelabel-mark.service`), and the next boot with
  SELinux then relabels the whole root filesystem and reboots. The pre-c64
  kernels (through `e01fab21`) booted without SELinux, so each earlier test
  boot left that mark and forced a relabel plus an extra reboot on the next
  stock start. With the c64 recipe the test kernel initialises SELinux and
  should leave no mark. If a mark is present before the run, the Phase 8 test
  boot itself would relabel and reboot, consuming the one-shot boot: Phase 1
  stops and the relabel needs its own approved stock reboot first. In the
  booted test kernel, `/sys/kernel/security/lsm` must list `selinux` and
  `/sys/fs/selinux/enforce` must read `1`; otherwise the gate cases refuse
  and the run stops with the guest preserved. The 9p mounts carry no
  `security.selinux` xattr support (`CONFIG_9P_FS_SECURITY` off); they get
  their label from the policy's `genfscon 9p` rule, checked only against the
  workstation's Fedora targeted policy, not Rocky's (see T7).

## Implementation gaps

These do not exist yet; the run is not executable until each is implemented,
reviewed and pinned, and this plan is re-filled:

- G1 Bootstrap orchestrator for Phase 8, inside 120 s: the root SSH round
  trip running `rocky_kernel.py admission` (manifest SHA256, boot ID, provider
  nonce), filling the enrollment request, sign, provision, enrollment
  transfer, the runtime's observation, bind, two mounts, then the post-READY
  `usable` gate (k64). **Implemented (x34),
  locally tested only**: `tools/lab/retained9p/bootstrap.py` with its remote
  half `bootstrap_remote.py` ([README](../../tools/lab/retained9p/README.md#phase-8-bootstrap-orchestrator-zq3-g1));
  tests `test_bootstrap.py` (against real signer output) and
  `test_bootstrap_remote.py`. Still needed at run time: the filled bootstrap
  plan and its SHA256 (it pins `bootstrap_remote.py` and the bundle's
  template and runtime config, whose `BootstrapSeconds` must equal the plan's),
  the handoff directory created in Phase 7, a doas rule for `python3.12 -I -B
  -c` on f3 (effectively unrestricted root for `paul`: only for the run window,
  or accepted explicitly), the operator ready to sign twice inside the 120 s
  (A7), and review/pinning of the two scripts.
- G2 Generator for the `controlled-runtime/1` config, enrollment/bind request
  templates (completed in Phase 8 step 0 with `BootID`, `ProviderNonce` and
  `Observer.ManifestHash`) and per-session keys. **Implemented in s34**
  (`cmd/hfs-runtime-prep`, [README](../../internal/runtimeprep/README.md));
  needs review and a pinned build. It validates the config through the actual
  runtime validator, never reads the operator key, and fills the requests with
  `enroll-request`/`bind-request`. `check-argv` binds the config to the G4
  record (order in Phase 0). The admission package caps the runtime's pools
  at 4 GiB, so `Budget.HostBytes` cannot carry the proposal's 6 GiB envelope
  (see Limits).
- G3 Source-matching volume/node initialisation helper. **Implemented in s34**
  as a separate pinned helper, so the xz3 candidate needs no rebuild:
  `tools/lab/build-init-helper.sh` (reproducible FreeBSD build of the actual
  `cmd/homelabfs` from `e0cca0c`) plus `tools/lab/runtime-volume-init.py`;
  needs review, and the Phase 0 rebuild must reproduce the recorded hash.
- G4 Pinned direct bhyve argv reconstructed from the current launch facts, plus
  the tap/nmdm/UEFI-vars setup and post-exit cleanup script. **Prepared** in
  [`tools/lab/directlaunch`](../../tools/lab/directlaunch/README.md) (task t34,
  local simulated tests only). The first Phase 1 `facts` run (2026-11-08)
  refused the package firmware symlink; task h44 pins exactly that link (text,
  target confined to `/usr/local/share`, target hash; every other symlink
  still refuses, setup re-checks). Still needed: fresh `host.py facts` from f3
  for the current Rocky launch block and vars hash, a chosen tap unit, the G2
  session UUIDs, then `argv.py generate` and review of the record.
- G5 **Implemented (r34), locally tested only:** Rocky kernel
  install/one-shot/removal script and the `installed-admission-v1.json`
  generator, `tools/lab/retained9p/rocky_kernel.py` (`plan`, `install`,
  `select`, `cancel`, `remove`, `check`, `reboot-to-stock`, `admission`).
  Still to pin at run time: the script's SHA256, the filled plan and the
  Rocky tool paths it assumes (Phase 1 records them).
- G6 **Implemented (q34)**: [`tools/lab/retained9p/observer_key.py`](../../tools/lab/retained9p/observer_key.py)
  `stage`/`enrol`/`status`/`remove`, run on Rocky as root (Phases 3, 4, 11);
  local tests in `test_observer_key.py`. Still needs, at run time, the Phase 1
  interpreter and `authorized_keys` hashes and Paul's A6 approval.
- G7 **Implemented (u34)**: [`tools/lab/retained9p/correlate.py`](../../tools/lab/retained9p/correlate.py)
  `case` builds `homelabfs-retained9p-host-correlation/1` from two OBSERVE
  replies per case (runtime/cut/epoch, plan boot/provider/sessions,
  journal/ledger/transport identities, advanced applied position, chain to
  the previous release); local tests feed its records to the real `release`
  (`test_correlate.py`). The filled plan pins its SHA256.
- G8 **Implemented (q34)**: `controller.py` accepts only the IQ primaries
  f3 `10.20.0.133` and Rocky `10.20.0.123` (the retained `192.168.1.x` aliases
  refuse with an explicit reason) and checks pin file content against the
  reviewed Ed25519 fingerprints; pins in [`f3-host-known_hosts`](f3-host-known_hosts)
  and [`rocky-known_hosts`](rocky-known_hosts); local tests in
  `test_controller_targets.py`.
- G9 **Implemented (w34)**: [`tools/lab/retained9p/pod_fixture.py`](../../tools/lab/retained9p/pod_fixture.py)
  `manifest`/`build`/`run` with the container side in
  [`pod_workload.py`](../../tools/lab/retained9p/pod_workload.py) (pinned by the
  driver; stage both side by side); local tests in `test_pod_fixture.py`. At run time it still needs,
  from Phase 1: the SHA256 of `pod_fixture.py` (the operator checks it before
  each invocation) and of `/usr/bin/podman`, `/usr/bin/crun`, `/usr/bin/conmon`,
  `/usr/share/containers/seccomp.json`; conmon's NEVRA; a readable
  `/var/log/audit/audit.log`; and the read-only `pod_fixture.py manifest` digest
  for Paul's review. `build` must later reproduce that exact digest (Phase 3 or
  before T7). The request also needs `m0`'s Phase 8 mount ID. Plus Paul's A8
  approval.
- G10 **Implemented (u34)**: `guest.py` lifecycle steps `lost-reply-call`,
  `lost-reply-reread`, `quiesce`, dispatched by `controller.py lifecycle`;
  `correlate.py lost-reply` and `command` (ARM/RESTART argv, RESTART only after
  an observed quiesce); local tests in `test_lifecycle_steps.py`,
  `test_correlate.py`. The guest-kernel limit on T9 above stays unless the
  d44 MKDIR filter is used, which needs a rebuilt runtime candidate.
- G11 **Implemented (u34)**: `guest.py` step `after-loss`, read-only
  [`host_snapshot.py`](../../tools/lab/retained9p/host_snapshot.py) on f3
  (pinned python3.12, root) and `correlate.py k04`, probing m1 (T9 blocks m0)
  and flagging a probe the guest refused locally as vacuous. Needs the
  runtime's FUSE mount point path from the G2 config at fill time.
- G12 *Prepared (v34)*: rebuilt, reproducible `controlledruntime.test`
  `f71f0bb8…` and `fdescfs-namespace` `3d992cdf…` from `b869710`, y64
  rebuild (earlier `56e87332…`/`45f2d59a…` from `060dea0`, c64+d64,
  `97fb5c07…`/`cc92bcde…` from `a20aca1`, z54, reused by 964,
  and `9e046c20…`/`a773f3ef…` from `defd974`, f44; recipe
  cross-checked by reproducing the candidate daemon), reviewed window pins and
  the run-time plan filler. Open until run time: the read-only host facts
  (python3.12 and base command hashes, `ldd` closure, controller `facts`)
  that the filler needs to emit the exact plan hash.
- G13 *Prepared (v34)*: `tools/lab/runtime-guard.py`, outer RSS/time/disk/
  memory/VM-context guard for the runtime and its bhyve child; tested
  offline only, never run on FreeBSD.
- G14 PTE/TLB/GUP exclusion fixtures (no admitted pin primitive): unrun.
- G15 This run is root-orchestrated with intent records, not the full
  plan-bound, crash-resumable runner from `docs/Test-automation.md`.

## Decisions needed from Paul

See the [summary](zq3-plan-summary.md#the-decisions-you-need-to-make) for the
plain-language yes/no list (A1-A8 above, plus: keep or remove the custom kernel
afterwards, and confirm the w24 guest-window interpretation).
