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
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261108/package-final`, manifest SHA256 `3f770a884978d3a776663a74083e733baf38665b8cba119022dd5432fb3296f1`, source `e0cca0c874f0027f050974ab5a54095d4af1c416` |
| Quarantined, never installed | `7e962627b1ec9aed913a28ee652bd9ed2f110f3369232c684994c709be2dce5b` (CGO-disabled daemon); the probe controller refuses it |
| Daemon | `freebsd/hfs-controlled-runtime` `5359873799fa9ca28bb7aebc65449ceb839e07736c7d65eda1d6d6d6af2583a5` (native observer check passed on f3) |
| Patched bhyve (exporter) | `freebsd/bhyve` `36423b6d4f68dc1b05299b1c379a164035e7db5b06ca64b47f2a8571d823fb5c`; the system `/usr/sbin/bhyve` (`c54a6361…`) is never replaced |
| Guest kernel | `6.1.190-hfs-xz3`: `linux/bzImage` `f40ee561…`, `linux/config` `d88b4c56…`, `System.map` `d5e915c5…`, five modules (`virtio_net`, `net_failover`, `failover`, `efivarfs`, `x86_pkg_temp_thermal`) with hashes in the package's `installation-proposal.json` |
| Operator public key | `2f6d909c8bfa020c43dd460dcc03fd9d1fd78e03cdb61f51aa693f77de89a635` (built into the guest kernel config) |
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
| f3 stock modules/binaries | `fdescfs.ko` `847cd1f2…`, `fusefs.ko` `b207adf6…`, `/usr/sbin/bhyve` `c54a6361…` |
| Rocky vm-bhyve config | unchanged; still contains the old PoC line `bhyve_options="-s 0:7:0,virtio-9p,homelabfs_poc=/var/tmp/homelabfs-9p-poc/share"` |
| Rocky OS/kernel | Rocky Linux 9.8; running and grubby default `5.14.0-687.66.1.el9_8.x86_64`; other entries ELRepo 7.0.3, ELRepo 6.1.190, 5.14.0-687.65.1/.64.1, rescue. **The custom `6.1.190-hfs-xz3` is not installed.** |
| Rocky security | SELinux Enforcing; root `authorized_keys` has 1 line |
| Rocky OCI | `podman` 5.8.2 and `crun` 1.27 installed; `runc`, `buildah`, `skopeo` not installed; no image pinned |
| Rocky resources | 11.7 GiB RAM visible, 7 GiB swap unused, 36G free on `/` |

*New facts Phase 1 must add* (not in the baseline): f3 ARC size/limits and
wired/free/inactive pages; whether the host path of the old PoC line,
`/var/tmp/homelabfs-9p-poc/share` on **f3**, still exists (vm-bhyve's normal
Rocky start in Phase 12 depends on it, see Risk R6); `/usr/local/bin/python3.12`
hash on f3 (the fdescfs controller's pinned interpreter); `vm-public` bridge
members and free tap names; current zrepl snapshot name/GUID of
`zroot/bhyve/rocky`; Rocky `/boot` and EFI free bytes, `grub2-editenv list`
(`saved_entry`, `next_entry`), `/etc/kernel/cmdline` or default BLS options,
dracut/depmod/grubby versions, root `authorized_keys` SHA256, `/usr/bin/python3`
hash; current authoritative addresses from IQ (f3 `10.20.0.133` and Rocky
`10.20.0.123` were used on 2026-11-08) with independently verified host-key pins.

## Approval classes

**(A) Critical: each needs Paul's explicit yes for this filled plan.**

- A1 Stop Rocky for the run window (Phases 5-12); see [downtime](#rocky-downtime).
- A2 Stage the candidate bytes on f3 under the private staging prefix (Phase 2).
- A3 Install the custom guest kernel on Rocky's disk (Phase 3), select it as a
  **one-shot** boot immediately before the stop (Phase 5), stock default kept,
  and remove it afterwards (Phase 12). Includes the U3 recovery boot: after an
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
- A8 Pod fixture inside Rocky: which OCI runtime/rootfs (Phase 10, T7), or skip.

**(B) Routine bounded verification** (standing approval of 2026-11-02, but only
inside an approved (A) window): read-only facts, local builds/tests, read-only
host test binaries under the bounded guard, lifecycle OBSERVE, guest probes in
the run-owned directories, the lock probe, evidence collection, and normal
unmounts of run-owned mounts.

## Limits

| Limit | Value | Source |
|---|---|---|
| Total critical window | 60 minutes from runtime start to runtime exit; only the probe controller enforces it (refuses commands 3600 s after its run started); the runtime has no enforced total deadline yet (G13) | proposal `seconds: 3600`; controller `RUN_WINDOW` |
| Restoration budget | 15 minutes after runtime exit (estimate, not enforced) | this plan |
| Host runtime envelope | 6 GiB (signed), plus 6 GiB guest and 3 GiB host reserve = 15 GiB of 15.7 GiB physical | installation proposal |
| Guest workload ceiling | 2 GiB signed provider/workload ceiling; logical canary 4 MiB | proposal |
| Disk | host run 4 GiB, guest run 2 GiB; keep 8 GiB unreserved host and 2 GiB guest free | proposal |
| Bootstrap | 120 s from launch to READY | base gate |
| Guest probe case | at most 60 s, 256 operations, 12 MiB issued writes, 128 fds | `guest.py` |
| Controller dispatch | each SSH dispatch bounded by `min(case seconds + 15, plan seconds)`, plan seconds 60-600 (enforced); streams 256 KiB each | `controller.py` |
| fdescfs commands | 10 s each, 256 KiB output; Go test 5 s; native RSS 512 MiB, one CPU | fdescfs window doc |
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

- Actions: build from source `e0cca0c` with the reviewed recipes: the FreeBSD
  CGO `controlledruntime.test` binary (for the host FD rerun), the
  `fdescfs-namespace` helper; pin them with hashes. Fill the fdescfs window plan
  JSON (schema in the window doc). Write the run config, enrollment requests,
  controller batch plan (`check-retained9p.sh check --plan` gives its hash).
  Run all local tests (`python3 -B -m unittest` in `tools/lab/retained9p`,
  `tools/lab/test_fdescfs_window.py`, artifact tests, `go test` for touched
  packages, shellcheck).
- Changes: workstation files only.
- **GAP**: run config/enrollment generator (G2), filled window plan (G12),
  controller target addresses (G8). The bhyve argv record comes from
  `tools/lab/directlaunch/argv.py generate` on the Phase 1 launch facts (G4);
  G2 must copy its `args` verbatim, and `argv.py verify` checks the config.
- Evidence: hashes of every staged byte, plan files and their SHA256.
- Failure: fix locally; nothing remote has happened.

### Phase 1: fresh read-only facts (f3 and Rocky, B)

- Actions: one bounded read-only batch per host over pinned SSH, stdout and
  stderr kept separate, no retry: everything in the baseline table plus the
  *new* items above. (The fdescfs controller's own `facts` call needs the
  staged inspector, so it runs at the start of Phase 4.) The f3 batch includes
  the G4 launch facts, `doas -n /usr/local/bin/python3.12 -I -B - facts <
  tools/lab/directlaunch/host.py` (read-only, nothing staged): rocky.conf,
  vm-run, firmware and UEFI vars hashes, the last vm-bhyve launch block (the
  retained one is from 2026-11-02, PID 6444; Rocky has since restarted as PID
  80203), `vm list`, `/dev/vmm`, interfaces, `vm-public` members, nmdm nodes,
  processes. Choose a free tap unit (`tap100`..`tap999`) from it.
- Preconditions to continue: Rocky running PID matches, all four FreeBSD VMs
  stopped, swap unused, pool ONLINE with the disk headroom above, `fusefs` and
  `fdescfs` state as recorded, `/dev/fd` is devfs, PoC share path on f3 present
  (R6), Rocky default kernel/grubenv unchanged, SELinux enforcing, one-line
  `authorized_keys` hash recorded.
- Evidence: `f3-facts.json`, `rocky-facts.json` with timestamps.
- Failure: any mismatch stops; produce a revised plan.

### Phase 2: stage the candidate on f3 (A2)

- Machine: f3. Actions: create `/var/tmp/hfs-validation-xz3/zq3-r1` root-owned
  `0700`; copy into its `bin/` only `freebsd/hfs-controlled-runtime`, `freebsd/bhyve`,
  `freebsd/bhyve-slirp-helper` (if the argv needs it), the `controlledruntime.test`
  binary and `fdescfs-namespace` helper, mode `0500`, one hard link each; verify
  SHA256 of every staged file against the manifest/Phase 0 pins. Also stage
  (G4) `tools/lab/directlaunch/{host,argv}.py` and the pinned argv record,
  mode `0400`, with their hashes.
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

- Machine: Rocky (root over pinned SSH). **GAP G5** (no script yet). Intended steps:
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
  5. Write `/etc/homelabfs/installed-admission-v1.json` from the actual
     installed kernel/module hashes and build IDs (closed schema in
     `internal/controlledruntime/README.md`); record its SHA256. **GAP G5**: no
     generator exists.
  6. Stage `observer.py` and the run directory `/var/tmp/hfs-zq3-zq3-r1`
     (root, `0700`, with empty `m0`, `m1`) on Rocky's local disk.
- Changes: new files in `/boot`, `/lib/modules/6.1.190-hfs-xz3`, one
  non-default BLS entry, `/etc/homelabfs/`, the run directory. grubenv is
  unchanged.
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
   (read-only, B); must match Phase 1 and the filled window plan.
1. Negative before setup (B): run only `TestDescriptorExecNativeUnsupported`
   from the staged test binary under the bounded guard; it must refuse before
   child/pin/observer effects. A skip counts as unrun.
2. `setup --plan … --plan-sha256 … --directory /var/tmp/hfs-fdescfs-windows/zq3-r1`:
   loads the stock `fdescfs.ko` (`847cd1f2…`) only if absent, mounts fdescfs
   `nodup,nosuid` on `/dev/fd` via `nmount` with `MNT_NOCOVER`, writes
   `owned.json`. **Disclosed: the module stays loaded after restoration.**
3. `qualify` (B): rerun `TestDescriptorExecNativeQualification` on the **f3
   host** with the final test bytes from source `e0cca0c`, plus
   `hfs-controlled-runtime --check-descriptor-exec` (must now report
   `fdescfs`, supported). This closes the host rerun transferred from w24.
4. `kldload /boot/kernel/fusefs.ko` (`b207adf6…`) only if absent, with
   before/after `kldstat`. **Disclosed: stays loaded afterwards.**
5. Observer key (A6): on f3, `ssh-keygen -t ed25519 -N '' -C
   hfs-zq3-zq3-r1-observer -f /var/tmp/hfs-validation-xz3/zq3-r1/observer/id_ed25519`
   (root `0600`, never copied off f3, never Paul's key). On Rocky append exactly
   one line to `/root/.ssh/authorized_keys`:
   `restrict,from="<f3 address>",command="/usr/bin/python3 -I /var/tmp/hfs-zq3-zq3-r1/observer.py --probe … --probe-sha256 … --python /usr/bin/python3.9 --python-sha256 …" ssh-ed25519 AAAA… hfs-zq3-zq3-r1-observer`;
   record file SHA256 before and after. **GAP G6**: enrol/remove script.
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

1. Final pre-stop checks (B): Phase 1 facts unchanged; memory pre-estimate
   (current free + inactive + reclaimable ARC + Rocky's resident bhyve memory
   must cover the 15 GiB requirement); latest zrepl snapshot of
   `zroot/bhyve/rocky` recorded (disk-level reference only; no manual snapshot
   and no automatic rollback).
2. Rocky: `grub2-reboot <entry id recorded in Phase 3>`; verify `next_entry`
   names it, `saved_entry` and `grubby --default-kernel` unchanged.
3. f3, immediately after step 2: `tools/lab/vms.sh --approved-action stop-rocky`
   (clean ACPI `vm stop`, waits, never forces). Then verify `vm list` shows
   Rocky Stopped, no `bhyve: rocky` process, no `/dev/vmm/rocky`, its tap gone.
- Failure in step 1 or 2, or if step 3 refuses before stopping anything: Rocky
  still runs, so run **U1** (cancel the one-shot) and verify, then U2.
- Failure in step 3 (timeout or SSH loss is not success): keep waiting within
  the helper's bound, then stop and inspect; never `vm poweroff`. If Rocky still
  answers pinned SSH, run U1. If it is stopped, `next_entry` is still set:
  recovery is **U3**.
- Restoration: Phase 12, or U3 for any abort before the Phase 8 boot.

### Phase 6: post-stop admission (f3, B)

- Actions: re-check memory now that Rocky's 12 GiB are free: free + inactive +
  reclaimable ARC (above `arc_min`) must cover 6 GiB guest + 6 GiB runtime +
  3 GiB reserve; swap unused; all FreeBSD VMs still stopped; `/dev/fd` still the
  owned fdescfs FSID.
- Failure: restore the fdescfs window (Phase 11 step 5), then **U3**, not a
  plain normal start: `next_entry` is still set, so a plain start would boot
  the test kernel.

### Phase 7: host setup for the controlled launch (f3, A4)

The 12 GiB vm-bhyve config stays unchanged; the candidate bhyve runs directly
as the runtime's own child. G4 is implemented as preparation (not yet run on
f3): [`tools/lab/directlaunch`](../../tools/lab/directlaunch/README.md).

- `argv.py generate` (Phase 0) rebuilds the argv from the Phase 1 launch facts
  and pins it in a canonical record; any drift from the reviewed baseline
  (rocky.conf, vm-run, firmware, every logged hardware token) refuses. For
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
  staging prefix. **GAP G3**: the candidate has no `homelabfs node-init /
  volume-init` helper.
- Write the private `controlled-runtime/1` config (G2) with `Exporter` = staged
  bhyve and `Args` copied verbatim from the record; `argv.py verify` must pass.
- Changes: one transient tap in `vm-public`, the vars copy and records under
  the staging prefix; bhyve creates the nmdm pair and the vmm context.
- Rollback: Phase 11 step 4 (`host.py cleanup`).
- Failure (no child started): `host.py cleanup --no-child` (destroys the tap),
  restore the window, then **U3**.

### Phase 8: start the runtime and staged bootstrap (f3 + Rocky, A4, A7)

- Action: `doas -n /var/tmp/hfs-validation-xz3/zq3-r1/bin/hfs-controlled-runtime
  --config <private config>` in the foreground under an outer resource guard
  (**GAP G13**: no guard profile for this long-running envelope yet).
- The runtime launches the bhyve child; GRUB consumes the one-shot entry and
  boots `6.1.190-hfs-xz3`. Record the consumption evidence: the observed guest
  kernel release and boot ID, plus the console log. Within the 120 s bootstrap the following must
  complete in order (**GAP G1**: no orchestrator script):
  1. Runtime observes the guest through the observer key (actual boot ID,
     provider nonce, complete module inventory, `admission_v1`).
  2. Signer (`hfs-retained9p-operator enroll`, on the workstation, with the
     operator key under A7) emits `provision.json` and `enrollment.json`.
  3. Root writes `provision.json` to `/sys/kernel/homelabfs/provision_v1` in
     one write, then transfers `enrollment.json` atomically to the host
     `EnrollmentFile`.
  4. At `AWAITING_GUEST_BIND`: `hfs-retained9p-operator bind` emits
     `bind.json`; one write to `/sys/kernel/homelabfs/bind_v1`.
  5. Guest mounts both domains:
     `mount -t 9p -o trans=virtio,version=9P2000.L.hfs1.4,cache=mmap,msize=65536
     hfs0 /var/tmp/hfs-zq3-zq3-r1/m0` and the same for `hfs1` on `m1`; both
     HELLOs; runtime prints READY with the control binding.
- Evidence: runtime stdout/stderr (bounded drains), READY record,
  `runtime-budget.json`, observer receipts, the signed files.
- Failure: missing/late/changed records refuse and **retain** native and guest
  resources; nothing falls back to stock 9p callbacks. Go to Phase 11 with
  inspection first. If the child exited or failed before GRUB ran (no guest
  kernel observed and no GRUB output on the console), treat `next_entry` as
  unconsumed and restore with **U3** after the child is positively gone.

### Phase 9: READY admission (B)

- `hfs-controlled-runtime --control <owner>/control.sock --operation observe
  --runtime <uuid> --sequence 1`: every section `complete`, publication null,
  both domains admitted. Guest: `findmnt` shows exactly the two mounts.

### Phase 10: gate matrix (B inside the window)

Stop dependent steps on the first failure. Results are pass, fail,
unsupported, unrun or uncertain; a partial result never completes zq3.

| Step | Gate (Deployment-tests) | How |
|---|---|---|
| T1 | UID/GID incl. full 1024-entry groups, setgid/umask create, sticky, special bits, cross-domain metadata | `check-retained9p.sh probe` (case `credentials`), then host correlation (**GAP G7**), then `release`, then `next` |
| T2 | write acknowledgement and sync propagation (O_SYNC/O_DSYNC/fdatasync/fsync, directory fsync, mmap MS_SYNC) | case `sync` + correlation of journal `Through/Applied` via OBSERVE + `release`, `next` |
| T3 | metadata: rename, hardlink, truncate, open-unlinked, cross-domain freshness | case `metadata` + correlation + `release`, `next` |
| T4 | foreign user namespace refusal (boundary only) | case `namespace` + correlation + `release`, `next` |
| T5 | `cache=mmap`: full 4 MiB mapped canary, cross-domain hash | case `mapped-pressure` + correlation + `release` |
| T6 | locks within one guest mount | `tools/lab/guest-lock-probe.py` on `m0` (POSIX/flock conflicts, blocking waits, GETLK, close/exit cleanup) |
| T6b | locks across clients | same probe across `m0`/`m1`: **recorded as unsupported** per the accepted lock scope, never as a pass |
| T7 | ordinary pod namespace profile | **GAP G9** and decision A8: separate mount/PID/net/UTS/IPC namespaces in `init_user_ns`, numeric IDs/full groups, SELinux enforcing, two workloads bind-sharing `m0` |
| T8 | K04 guest side: no writes to an empty fallback | normal `umount m1`; a probe naming `m1` must refuse at admission ("exact mountpoint not present once") before any open; the empty directory stays empty |
| T9 | v24 lost reply | `--operation arm-lost-reply` (CALL), one guest mkdir CALL, OBSERVE `lost_reply.exposure` = UNCERTAIN, guest re-read/QUERY returns the original result. **GAP G10** (guest step) |
| T10 | daemon restart via u24 | guest quiesce (FORGET, view refresh), then `--operation restart`; RESTARTED or FENCED are both recorded (FENCED, fencing then runtime exit, is Paul's accepted behavior); **always the last step**. **GAP G10** (quiesce step) |
| T11 | remount / VM restart need, K04 host side | after T10 or runtime exit: guest I/O on `m0` must fail, not reach a raw directory; replacement FUSE/exporter and guest RESUME are blocked by design, so the recorded answer is "VM restart required", with the refusals as evidence. **GAP G11** (host-side check) |

Mid-release failure (T1-T5): the guest consumes the retention record before
its first unlink. If an unlink or the deadline then fails, part of the payload
can remain, `released-CASE.json` exists and the controller stays at
`release-dispatching-uncertain` with `error`. Do not re-release or replay:
root inspects the leaf against the inventory in `released-CASE.json`, keeps the
leftovers and evidence, marks T1-T5 incomplete, and any repeat uses a new run ID.

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
   `m0`/`m1` (busy: stop and preserve). Remove the observer line from
   `authorized_keys`; verify the file SHA256 equals the pre-run value. Verify
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
   final state with the post-load baseline; `restoration-summary.json` states
   that `fdescfs` stays loaded. `fusefs` also stays loaded.
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
   equals pre-run, memory as before.
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

- **U1, cancel the one-shot (Rocky running):** `grub2-editenv - unset
  next_entry`; verify `next_entry` empty, `saved_entry` and `grubby
  --default-kernel` unchanged. Mandatory for any failure before Rocky is stopped
  once Phase 5 step 2 has run.
- **U2, remove the custom kernel (Rocky running, stock kernel or test kernel):**
  `grubby --remove-kernel=/boot/vmlinuz-6.1.190-hfs-xz3`; remove the copied
  `/boot` files, the initramfs, `/lib/modules/6.1.190-hfs-xz3` and
  `/etc/homelabfs/installed-admission-v1.json`; verify `grubby --info=ALL`
  equals the pre-run listing. Never while running the test kernel itself.
- **U3, recovery after Rocky stopped with `next_entry` still set** (aborts in
  Phases 5 step 3 to 8 before the controlled boot reached GRUB). There is no way
  to edit grubenv while Rocky is stopped without touching its disk from f3,
  which this plan does not do. Honest recovery path, approved as part of A3:
  1. Confirm no candidate bhyve child or `/dev/vmm/hfs-zq3-r1` exists.
  2. `tools/lab/vms.sh --approved-action start-rocky`: the normal 12 GiB
     config. GRUB consumes `next_entry` and boots `6.1.190-hfs-xz3` **once**,
     now under the normal config (with the unused PoC 9p device and normal
     network; no HomelabFS controller exists, so the provider stays unenrolled;
     services that need modules the test kernel lacks may fail for this boot).
  3. Wait (bounded, 10 minutes) for pinned root SSH; record `uname -r`; verify
     `next_entry` empty and `saved_entry` unchanged.
  4. `systemctl reboot` (clean); verify Rocky comes back on the Phase 1 default
     kernel; then U2 and the Phase 12 checks.
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
| Host setup, runtime start, bootstrap | 5 min (120 s bootstrap bound) |
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

- R1 Memory: the 15 GiB arithmetic leaves about 0.7 GiB physical headroom; ARC
  and kernel use must fit. Phase 6 measures it and refuses otherwise.
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
  not start `rocky` while the child runs (`vms.sh` and a manual `vm start` would
  both corrupt it). AUTO=yes only acts on an f3 boot, which also ends the child.
- R4 A RESTART FENCE ends the runtime while the bhyve child keeps running;
  Phase 11 shuts the guest down from inside.
- R5 Retained `fdescfs`/`fusefs` modules change f3's kernel state until reboot.
- R6 If the host path of the old PoC line is gone on f3, vm-bhyve cannot start
  Rocky normally in Phase 12. Phase 1 must confirm it exists, or the run waits
  for a separately approved config decision. This plan does not edit
  `rocky.conf`.

## Implementation gaps

These do not exist yet; the run is not executable until each is implemented,
reviewed and pinned, and this plan is re-filled:

- G1 Bootstrap orchestrator for Phase 8 (observe, sign, provision, enrollment
  transfer, bind, two mounts) inside 120 s.
- G2 Generator for the `controlled-runtime/1` config, enrollment/bind requests
  and per-session keys.
- G3 Source-matching volume/node initialisation helper; the candidate package
  contains only `hfs-controlled-runtime` (no `homelabfs node-init/volume-init`).
- G4 Pinned direct bhyve argv reconstructed from the current launch facts, plus
  the tap/nmdm/UEFI-vars setup and post-exit cleanup script. **Prepared** in
  [`tools/lab/directlaunch`](../../tools/lab/directlaunch/README.md) (task t34,
  local simulated tests only). Still needed: fresh `host.py facts` from f3 for
  the current Rocky launch block and vars hash, a chosen tap unit, the G2
  session UUIDs, then `argv.py generate` and review of the record.
- G5 Rocky kernel install/one-shot/removal script and the
  `installed-admission-v1.json` generator.
- G6 Observer key enrol/remove script (one line, hash-checked), and staging of
  `observer.py` and its pinned interpreter hash.
- G7 Host correlator producing `homelabfs-retained9p-host-correlation/1`
  records from lifecycle OBSERVE (journal/source/ledger identities) for each
  retained case; the controller binds but does not interpret them.
- G8 `controller.py` still accepts only the pre-migration addresses
  `192.168.1.133` (f3) and `192.168.1.123` (Rocky); update to IQ-verified
  addresses and add a pinned Rocky host-key file (`docs/lab/f3-known_hosts`
  lists only the hfs-test guests).
- G9 Pod/namespace fixture script and pinned runtime/rootfs (depends on A8).
- G10 Guest steps for the v24 lost-reply CALL and the C9 quiesce before RESTART.
- G11 Host-side K04/after-loss check.
- G12 Filled fdescfs window plan; rebuilt, pinned `controlledruntime.test` and
  `fdescfs-namespace` from `e0cca0c`; python3.12 hash on f3.
- G13 Outer resource guard for the long-running runtime and its child.
- G14 PTE/TLB/GUP exclusion fixtures (no admitted pin primitive): unrun.
- G15 This run is root-orchestrated with intent records, not the full
  plan-bound, crash-resumable runner from `docs/Test-automation.md`.

## Decisions needed from Paul

See the [summary](zq3-plan-summary.md#the-decisions-you-need-to-make) for the
plain-language yes/no list (A1-A8 above, plus: keep or remove the custom kernel
afterwards, and confirm the w24 guest-window interpretation).
