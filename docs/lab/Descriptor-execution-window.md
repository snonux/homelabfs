# Native descriptor execution prerequisite and temporary qualification window

The controlled runtime executes its pinned exporter, mount helper and SSH
observer through inherited `/dev/fd/N` descriptors. FreeBSD requires an actual
`fdescfs` mount at `/dev/fd` with **`nodup`** vnode lookup semantics. A default
fdescfs mount returns descriptor character devices. `rdlnk` pathname symlinks
discard descriptor pinning and are not an acceptable substitute. The preflight
checks the actual filesystem and a held executable's regular vnode identity;
it does not trust a configuration boolean or a caller's mount claim.

The selected FreeBSD source's [lookup implementation](https://github.com/freebsd/freebsd-src/blob/aadd58dddcbc78f4d5594827b46b5633552b15ce/sys/fs/fdescfs/fdesc_vnops.c)
returns the held vnode under `FMNT_NODUP`; its
[mount implementation](https://github.com/freebsd/freebsd-src/blob/aadd58dddcbc78f4d5594827b46b5633552b15ce/sys/fs/fdescfs/fdesc_vfsops.c)
recognizes `nodup`. Actual qualification of a `nodup` mount on FreeBSD 15.1 is
recorded [below](#qualification-result-2026-11-08); the f3 host kernel still
requires its own window. Linux `/dev/fd` magic links have different semantics;
Linux tests cannot establish this FreeBSD prerequisite.

`hfs-controlled-runtime --check-descriptor-exec` is an exclusive, read-only
prerequisite report. It opens its own executable and emits the actual platform,
mount type, FSID, flags, support/failure and null publication, including on
refusal. It creates no owner, log, child, service or mount. A supported report
does not prove a child launch, process observer support, FUSE, READY, bhyve,
guest behavior, or the complete zq3 gate. `--check-process-observer` remains a
separate read-only qualification. Startup checks both prerequisites before
opening the disposable service. Direct Start, mount and observer entry points
also check descriptor support before their executable/device/staging effects.

## Authority and reviewed plan

`/dev/fd` is a SYSTEM namespace outside disposable test directories. The
standing routine verification approval does not itself authorize mounting it or
loading a module. Root obtains any necessary explicit host setup authority only
after reviewing the exact plan, scripts and artifact bytes. No worker performs
host actions. An exact plan hash records the reviewed input; it grants no
authority. Do not edit fstab, rc/loader policy, install packages, unload modules,
force unmount, signal/reset VMs, touch production/NFS/PVs or modify a received
replication sink.

The version-controlled [controller](../../tools/lab/fdescfs-window.py) runs on
the selected FreeBSD host through the pinned `/usr/local/bin/python3.12 -I -B`
interpreter, with user-site/environment imports isolated and bytecode writes
disabled. Actual read-only root facts on 2026-11-05 established
f3 kernel/base/userland `15.1-RELEASE-p2`, with fdescfs absent from both the module
and VFS registries. Its existing root-owned mode 0444 stock module is 47,696 bytes,
SHA256 `847cd1f2dc47db32d56dcf5352b65bad2b174b339e8aa89a8d114299311856d2`.
The read-only facts of 2026-11-08 18:10 UTC confirm the same kernel
(`FreeBSD 15.1-RELEASE-p2 releng/15.1-n283596-aadd58dddcbc GENERIC`), the same
module SHA256, fdescfs and fusefs not loaded and no fuse/fdescfs mounts
(see [filled f3 plan](#filled-f3-plan-for-zq3-r1-2026-11-08)).
The proposed window explicitly includes one pinned
`/sbin/kldload /boot/kernel/fdescfs.ko` only if still absent, and **retains that
new module after normally restoring the mount**. That kernel state change must
be explicit in the reviewed authority; it is not full original-system
restoration. No module is unloaded. Existing modules and supported mounts are
preserved; an existing unsupported mount refuses before module/mount changes.

The selected source's [`mount` command](https://github.com/freebsd/freebsd-src/blob/aadd58dddcbc78f4d5594827b46b5633552b15ce/sbin/mount/mount.c)
signals mountd after successful root mounts. The
[narrow native helper](../../tools/lab/fdescfs-namespace.c) instead invokes only
checked-header `nmount` and normal `unmount` for fixed `/dev/fd`, verifies the
exact FSID and never signals mountd or any other process. It is a host namespace
setup tool, never an exporter exec wrapper. Compile it offline against the
selected actual FreeBSD headers/libc with warnings treated as errors; pin the
compiler, recipe, executable and loader/library closure before host setup.
Its generic `nocover` iovec option selects the kernel's atomic `MNT_NOCOVER`
guard against a concurrently installed foreign mount. The 64-bit flag is not
truncated into nmount's int flags argument. The selected
[kernel parser](https://github.com/freebsd/freebsd-src/blob/aadd58dddcbc78f4d5594827b46b5633552b15ce/sys/kern/vfs_mount.c)
recognizes that option before entering the mount operation.
Normal restoration uses the selected kernel's `MNT_BYFSID` and documented
`FSID:decimal:decimal` selector, with no force flag, so a foreign mount covering
the pathname cannot become the unmount target between observation and syscall.

Preparation must pin the actual root-owned, non-writable native inspector and
test binaries, their loader/shared-library closure, the actual kernel/base and
fdescfs module when present, Python/interpreter closure, every base command,
controller bytes, hostname, boot, loaded module inventory, mount table and
`/dev/fd` directory metadata. Root alone obtains fresh read-only host facts;
historical inventories are not current admission. Include the existing Rocky
and all guest/process/memory/storage inventory in the outer reviewed plan.
This window runs no VM lifecycle, FUSE mount or filesystem workload.

Controller plan schema (values below are explanatory placeholders, **not an
executable exact plan**):

```json
{
  "version": "homelabfs-fdescfs-window/1",
  "run_id": "w24-UNIQUE",
  "expires": 0,
  "directory": "/var/tmp/hfs-fdescfs-windows/w24-UNIQUE",
  "inspector": "/ROOT-PRIVATE-STAGING/hfs-controlled-runtime",
  "namespace_helper": "/ROOT-PRIVATE-STAGING/fdescfs-namespace",
  "module_policy": "preserve-existing-or-retain-new",
  "window_module": {"path": "/boot/kernel/OTHER.ko", "sha256": "ITS_PINNED_SHA256"},
  "sha256": {"/ABSOLUTE/ARTIFACT": "ACTUAL_SHA256"},
  "before": {"ACTUAL_FACTS_OUTPUT": "not invented or historical"},
  "qualification": [["/ROOT-PRIVATE-STAGING/controlledruntime.test",
    "-test.run=^TestDescriptorExecNativeQualification$", "-test.v",
    "-test.timeout=5s"]]
}
```

`window_module` is either `null` or exactly one stock `/boot/kernel/*.ko`
file other than `fdescfs.ko`, whose hash must equal its entry in `sha256`. It
declares a module that the **outer reviewed run** loads itself between setup
and restore (zq3: `fusefs.ko` in Phase 4 step 4). The controller never loads
or unloads it. Because the declaration is part of the hash-bound plan and its
file is an artifact pin, every controller action re-digests that file before
doing anything.

Root prepares `/var/tmp/hfs-fdescfs-windows` as a root-private directory under
the reviewed staging plan, counts all retained/current/cache/tmp/remote storage
in the global 12 GiB ownership envelope and maintains the separate free-space
guard. Bind the 512 MiB native RSS envelope and one CPU through the outer
approved resource guard; Go child GC/parallelism hints are not an RSS limit.
No package/toolchain install is implicit. The controller limits each fixed
command to 10 seconds and combined stdout/stderr to 256 KiB. Qualification has
a five-second Go test deadline. The actual selected binaries and resource guard
must be validated before the host setup request.

## Exact intended sequence

Every controller invocation below has the explicit prefix
`/usr/local/bin/python3.12 -I -B /ROOT-PRIVATE-STAGING/fdescfs-window.py`;
do not select an interpreter from PATH or run an editable workstation copy.

1. Root collects read-only actual facts and pins all selected artifacts. Build
   the source-matching native CGO test and command offline using the reviewed
   native toolchain. On the actual unsupported profile, run only
   `TestDescriptorExecNativeUnsupported`: it must refuse before the child,
   pinned exporter, issuance changes, mount device and observer file staging.
   A skip is unrun negative evidence. Retain stdout, stderr and exit separately.
2. Review the filled plan JSON and its SHA256, complete artifact closure and
   fresh outer resource/VM guard. Obtain required SYSTEM setup authority.
3. `facts --inspector /ABSOLUTE/INSPECTOR` emits fresh controller facts without
   creating a run. `setup --inspector ... --plan /ABSOLUTE/PLAN
   --plan-sha256 HASH --directory /var/tmp/hfs-fdescfs-windows/RUN` refuses
   reused IDs or changed pins/facts. If absent, the explicit stock module load
   has its own intent/result and actual before/after module evidence. Setup then
   invokes the pinned helper's `mount` action: exactly fdescfs at `/dev/fd`,
   `nodup` vnode lookup and `MNT_NOSUID`, without mountd notification. It verifies
   the sole new mount, actual regular FD vnode semantics, and matching helper/
   inspector FSID, then protects `owned.json`. Private `nodup` flags are not
   inferred from mount's printed standard flags. Neither is `MNT_NOSUID`:
   FreeBSD's `fdesc_statfs` reports only `mnt_flag & MNT_IGNORE`, so the new
   `mount -p` row on 15.1 reads exactly `rw` (observed in zq3-r1). Setup
   accepts the option list `rw` or `rw,nosuid` and refuses anything else;
   nosuid rests on the pinned helper bytes. It is defence in depth only: with
   `nodup`, `/dev/fd/N` resolves to the held file's own vnode, so exec applies
   that file's filesystem setuid rules, not fdescfs's. Existing supported mounts
   receive `existing.json` instead.
4. `qualify` with the same arguments checks that exact FSID/profile and runs
   only the pinned bounded native test commands. The positive test starts the
   existing short owned Go helper through its copied, hash-pinned executable
   descriptor, receives management readiness and checks native live executable
   identity. It never starts bhyve. Run the separately reviewed S24 bounded C
   parent-death/SIGPIPE fixtures only when their exact bytes/invocations are
   included in the outer authorized plan. Their result is distinct evidence.
5. `restore` with the same arguments only normally unmounts the exact owned
   FSID/profile via the pinned helper's `unmount EXACT_FSID` action. A preexisting mount is preserved.
   Restore compares final boot/base/module/mount/directory facts with the
   post-module-load baseline facts. Every non-module fact must be equal, and
   every baseline loaded file (fdescfs.ko included) must still be loaded
   unchanged (same Id, address, size, path and contained modules; reference
   counts are ignored because loading the window module raises them). The only tolerated addition is the plan's `window_module` file,
   loaded from its exact declared path, registering its module name, with the
   on-disk file still matching the pinned SHA256 (kldstat cannot hash loaded
   kernel text). It may also be absent if the run stopped before loading it.
   Any other module, a second extra module, a wrong path or hash, or a missing
   baseline module refuses after the normal unmount and retains `owned.json`
   for inspection. `restoration-summary.json` explicitly records mount
   restoration, whether the newly loaded fdescfs module remains and the loaded
   window module; it does not report full original-system restoration when
   either module is retained.
   Restore remains permitted after plan expiry for the same
   pinned bytes and ownership, so expiry does not strand normal restoration.
   Collect all private evidence outside the host and inspect final VM/process,
   memory, module, filesystem and remaining-resource inventory.

Every mutating/qualification dispatch writes exclusive durable intent before
the child and a separate result afterward. Intent without result is uncertain:
do not replay. A timeout/overflow stops dependent actions and preserves state;
only the exact short command child may be stopped, never a VM or process group.
Mount success without completed ownership evidence requires independent root
inspection and explicit reconciliation before any cleanup. Busy unmounts,
changed FSIDs/profiles and restoration mismatches stop and retain resources.
No trap automatically unmounts uncertain resources or removes failed evidence.

This document and script are preparation for the **f3 host** window, not an
approved host setup; no f3 host `/dev/fd` mount has been made. The section
below fills everything that can be pinned offline for the zq3 run.

## Filled f3 plan for zq3-r1 (2026-11-08)

Prepared offline by task v34 (zq3 gap G12) from source `e0cca0c`, then rebuilt
for f44 from `defd974` (d44 MKDIR-targeted lost reply) so the test bytes match
the final daemon source. Nothing used
SSH, the lab, a VM, a mount or a module. It grants no authority: the window is
zq3 approval class A5 and still needs Paul's explicit yes at run time.

**Rebuilt artifacts.** [`build-fdescfs-window-artifacts.sh`](../../tools/lab/build-fdescfs-window-artifacts.sh)
exports the selected commit (now `defd974`) as a source archive and builds each artifact twice with
fresh Go caches, using the pinned clang cross wrapper, the captured f3 sysroot
(`c16fdc1c…`) and the candidate daemon's Go environment. Both builds were
byte-identical. As a toolchain cross-check the same recipe reproduced the
candidate daemon exactly. Exact commands, environment, toolchain hashes and
ELF closure: [build record](zq3-fdescfs-window/build-record.json).

| Artifact (staged under `/var/tmp/hfs-validation-xz3/zq3-r1/bin/`) | SHA256 | Bytes |
|---|---|---|
| `controlledruntime.test` (FreeBSD CGO test binary) | `9e046c2015b2d12c8fc42a89b06e7eaf5bc93876ece018917e3c67db35249cd8` | 13,378,320 |
| `fdescfs-namespace` (helper, `-std=c11 -Wall -Wextra -Werror -O1 -g`) | `a773f3ef506bec572fe777a269cffb958ce5e04925eb22f580bf40b9f434fe98` | 22,576 |
| `hfs-controlled-runtime` (inspector, equals the candidate manifest) | `174949c1ebf97d483d49e41f1ade7bce88f63013cb943be794276bb1ddd417a7` | 10,750,096 |
| `fdescfs-window.py` (controller) | SHA256 of the repository file at the reviewed commit | |

All three binaries load through `/libexec/ld-elf.so.1` and need base
`libc.so.7`, which itself needs `libsys.so.7`; the Go binaries also need
`libthr.so.3`. Those four hashes, taken from the f3 sysroot capture, are pinned
too and re-verified on f3 by the controller at setup.

**Pins and filler.** [`pins.json`](zq3-fdescfs-window/pins.json) holds every
reviewed value: run ID `zq3-r1`, window directory, staged paths, the hashes
above, the stock module `847cd1f2…`, the declared window module
`/boot/kernel/fusefs.ko` (`b207adf6…`, also pinned, from the
[Phase 1 facts](results/20261108-zq3-phase1/README.md); task j44) that Phase 4
step 4 loads after setup and restore therefore tolerates, the expected host identity
(`f3.lan.buetow.org`, `15.1-RELEASE-p2` kernel and userland), fdescfs absent,
`/dev/fd` devfs and unsupported, and which modules must or must not be loaded.
The controller compares the plan's `before` with fresh `facts` output and
refuses an expiry more than one hour ahead, so the exact plan JSON can only be
produced at run time. [`fdescfs-window-plan.py`](../../tools/lab/fdescfs-window-plan.py)
does that on the workstation: it refuses unless the fresh facts and host
hashes match every pin, then writes `plan.json` and `fill-record.json` and
prints the plan SHA256 for review. Its tests feed the filled plan to the
actual controller `validate()`.

**Facts the orchestrator collects read-only on f3** (root over pinned SSH,
after staging, at the start of Phase 4):

1. `sha256 -r` of every path in `pins.json` (`collect`, `pinned` and
   `controller`). The `collect` values (interpreter, kernel, the six base
   commands) are new facts; the others, including `/boot/kernel/fusefs.ko`,
   must equal their pins.
2. `ldd /usr/local/bin/python3.12` and `sha256 -r` of each library it lists
   (optional extra pins; the filler accepts them only under `/lib`, `/usr/lib`,
   `/usr/local/lib` or `/libexec`).
3. `stat -f '%Sp %Su %Sg %N'` of `/usr/local/bin/python3.12` and its
   ancestors. The controller opens every pinned file with `O_NOFOLLOW` and
   requires root-owned, non-writable files under root-controlled directories,
   so a symlinked or writable interpreter path refuses.
4. `/usr/local/bin/python3.12 -I -B STAGED/fdescfs-window.py facts --inspector
   STAGED/hfs-controlled-runtime` stdout (read-only; creates no run).
5. `date -u +%s` on f3, taken with item 4.

Feed items 1, 2, 4 and 5 (`--host-now`) to the filler with an expiry under one
hour. The controller compares the expiry with f3's clock, not the
workstation's, so the filler checks it against both and refuses a skew above
300 s. Review the printed hash, then run `setup`, `qualify` and `restore` with exactly that plan
and hash. `qualify` runs under the bounded native guard's host profile
(512 MiB aggregate RSS, one CPU, Rocky pinned, because Rocky still runs during
Phase 4), staged together with its `runner_process.py` in a fresh 0700 root
as `native-test-drive.py` does. The guard imports that sibling module, so it
runs with `-B` but without `-I` (which would drop the script directory from
the import path); the controller below keeps `-I -B`:

```sh
doas -n /usr/local/bin/python3.12 -B /var/tmp/hfs-20261108-zq3r1-fdq/native-test-guard.py \
  --root /var/tmp/hfs-20261108-zq3r1-fdq --stage qualify \
  --local-reserved-bytes 4294967296 --profile host --rocky-pid PID \
  --rocky-birth 'LSTART' --rocky-config-sha256 HASH --max-seconds 60 -- \
  /usr/local/bin/python3.12 -I -B /var/tmp/hfs-validation-xz3/zq3-r1/bin/fdescfs-window.py \
  qualify --inspector /var/tmp/hfs-validation-xz3/zq3-r1/bin/hfs-controlled-runtime \
  --plan PLAN --plan-sha256 PLAN_SHA256 --directory /var/tmp/hfs-fdescfs-windows/zq3-r1
```

`setup` and `restore` run directly: the controller bounds each of their
commands to 10 s, and the native guard's process-group TERM/KILL must never
interrupt a mount or unmount dispatch. The pre-setup
`TestDescriptorExecNativeUnsupported` negative uses the same guard with the
staged test binary as its command.

## Qualification result (2026-11-08)

W24's FreeBSD qualification ran in the disposable guest **hfs-test0**
(15.1-RELEASE `n283562-96841ea08dcf`), not on the f3 host. Paul allowed
stopping Rocky and starting the test VMs; treating a mount inside that
disposable guest as a lab action rather than a host SYSTEM change is the
agent's interpretation, flagged for Paul's confirmation. The run loaded the guest's stock
module, mounted `fdescfs -o nodup` on `/dev/fd`, and passed
`TestDescriptorExecNativeQualification` plus the full `Supervisor.Start`
parent-exit test `TestActualExporterDiagnosticsParentExit`. Negative controls
refused before setup and after normal `umount`/`kldunload`; the guest's
`/dev/fd` devfs identity matched its pre-setup record. On the f3 host itself,
the unsupported profile refused correctly with the current bytes. See the
[evidence](results/20261108-native-fd-qualification/).

The guest kernel and its module bytes differ from f3's `15.1-RELEASE-p2`. Before
the real daemon runs on the f3 host (zq3, annotated there), this host window still needs its
filled plan, explicit critical authority, rerunning
`TestDescriptorExecNativeQualification` on the host, and normal restoration.
