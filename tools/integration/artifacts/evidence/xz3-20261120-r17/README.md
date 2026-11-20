# xz3 r17 rebuild (2026-11-20): zq3-r16 control-ticket fix

**Superseded on 2026-11-21** by the [r18 rebuild](../xz3-20261121-r18/README.md)
(`b2a4f69e…`, source `633509e`) for zq3-r18. Its re4 guest kernel
`360fbc6d…` blocked m1's session with `EKEYREJECTED` when a correct GRANTED
VIEW_QUERY reply arrived before its REOPENED ACK had raised the fence
(zq3-r17 R17-1, fixed by 2f4). r18 rebuilt the kernel (2f4) and the daemon
(je4, ce4, e84) with the window artifacts, and reused this build's native
exporter and its f3 records byte for byte. The tooling refuses this
manifest as superseded. It still passes `package.py check` and `admit.py`
under the `633509e` tooling.

This candidate supersedes the [r16 rebuild](../xz3-20261119-r16/README.md)
(`a1af0bd7…`, source `707f3d1`). In zq3-r16 (R16-1) T1's m1 root OPEN hit a
routine recall. The ie4 guest kernel asked again as designed, but the host
answered BLOCKED: the budget of 8 control tickets, shared by both mounts,
was full. The kernel returned that at once while it still held its lease.
Three merges since r16 change package content or its inputs:

- **re4** (merge `bc38547`, patch 0004 and `source.json`): on a host
  BLOCKED observation VIEW_ACQUIRE the guest releases its held lease and
  retries only after retirement progress (at most 3 retries and 4 tickets
  per syscall; any floor advance counts as progress). Refused retirements
  are re-sent.
- **qe4** (merge `3455fab`): ControlTickets 8 -> 24 in the runtime spec
  `internal/runtimeprep/testdata/zq3-spec.json`, plus the control-ticket
  accounting in `internal/viewcontrol/tickets.go`. The spec is not package
  content: no manifest entry names it and `assemble.py` tracks no such file.
- **te4** (merge `3430758`): incremental journal verification
  (`internal/journal/segment` Cursor), an indexed pending-claims check
  (`internal/fsservice`), and changes in `controlledvalidation` and
  `viewcontrol`.

So the guest kernel and the CGO daemon were rebuilt, and the window
artifacts with the daemon. The native exporter was rebuilt too, on f3, for
a tooling reason (see below). **Build and offline admission only:** nothing
ran on Rocky or any VM, and nothing was installed or booted, so
`installed`, `booted` and `native_accepted` stay false, and publication
stays null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261120-r17/package` |
| Manifest SHA256 | `f614d52a84b43d455a20f2ee973f9e9346a00e26a25019e33a28ec7b8c14e313` (194 files) |
| Source checkout and repository baseline | `3430758f639365b037b79177ea290b4391feeee5` (the daemon's build revision; there is no `inputs/repository-changes.patch`) |
| Kernel | `6.1.190-hfs-xz3`: `linux/bzImage` `bf789b065bc708aa74312bc1786be407a54c81d986544aeeb7212e19bc964d15`, `linux/vmlinux` `0c3a4d14117ad624c1dc4686af9b69350db628779f513e6d5fa27779d108579d` (GNU build ID `360fbc6d1608c03c6ff3d41ffb0dc7fadfccaee9`), `linux/config` `28fe4a8e…` (unchanged), `System.map` `fc2565c4ea13de095ebdf71fe094b3acef52041b3d9b65980eef17e0087d1192` |
| Modules | the five modules are byte-identical to `a1af0bd7` (same GNU build IDs; `virtio_net` `072995182e1a…`) |
| CGO daemon | `85900893e07be37b363b9758fa5ae41184a34b6f45ef2218e26f191918c6733d` (11,055,736 bytes, GNU build ID `1809f39916eb792817f7536a66ca938394d12b36`) |
| Native exporter | `freebsd/bhyve` `2fd7e32f5cc8762ae0ff4d5ec6c046d7d49b95c97e4b9150dd17b2ac52c39fd3` (r17 native f3 build under the 7e4 guard; byte-identical to xd4's) |
| Window test binary / helper | `1f042fd682cb1668a8cbcb7bafe7848fa0a02ffea95752c02e324307e151705d` / `6d78b1debef478c5e29c4052888f36f5b689e1d09db7d59e7787ebc118fbc741` (from `3430758`) |

[Evidence](evidence.json) records hashes, stage timings, receipts and
control names. [kernel-clean-repro.json](kernel-clean-repro.json) holds the
`compare_kernel.py` record (also packaged as `linux/clean-repro.json`), the
stage table and the ABI negative-control receipt.
[native-build-records.json](native-build-records.json) holds the f3 facts,
the guard receipt of every native stage and the native comparison.
[reused-files.json](reused-files.json) lists the 27 reused files, the 144
files that came out identical and the 23 that changed.

## Kernel rebuild (xd4/r16 method)

- **Source:** a fresh `prepare.py` tree from the cached pinned archive
  `4f3c00db…` (`/home/paul/tmp/claude/c64-config/linux-pinned.tar.gz`;
  nothing downloaded). The new source inventory is `4d480854…`.
- **Two builds at the same O path:** every `kernel.py` stage ran under a user
  systemd scope (two CPUs, 2 GiB, no swap). The first build tree was moved
  aside before the repeat. `abi.py collect` ran on both packages.
- **Timeouts and resumes:** the host ran at load 14-16 from other sessions,
  and `kernel.py` gives every `make` 1190 s:
  - The first build timed out in `vmlinux` twice. Attempt 1 is kept in the
    run root's `timeout1/`. Attempt 2 timed out at 1204 s, after the final
    link. Its `vmlinux` stage was then rerun on the same O tree (53 s), and
    the remaining stages followed. That resume did **not** wait for `make`
    children left running by the timeout; it started as soon as `kernel.py`
    returned. The equal fresh repro build below covers the resulting bytes.
  - The builder raised the local systemd scope bound per stage, the wrapper
    around this local kernel build, from 20 to 40 minutes because of the
    host load. `kernel.py`'s own 1190 s limit per `make` still applied and
    caused every timeout. This is not the f3 `native_guard.py` bound, which
    stays 20 minutes per stage (see the native section and
    [the artifacts README](../../README.md)).
  - The first repro resume raced `make` children that the timeout had left
    running, and `fixdep` lost `net/ipv4/.datagram.o.d`. That tree is kept
    in `timeout2/`. The repro build then ran from a fresh O tree in one pass
    (`vmlinux` 922 s), after waiting until no process used the O path.
- **Clean repro:** `compare_kernel.py` found the resumed first build and the
  one-pass repro identical (13 artifacts plus the full module ABI).
- **ABI negative controls:** `check_abi_mismatch.py` refused its four
  controls (stock kernel release, different operator config identity,
  missing and different module layout CRC).
- **Unchanged against `a1af0bd7`:** `config`, `abi-negative.json`,
  `kernel.release`, `modules.*` and the five modules (same GNU build IDs).
  SELinux stays an active LSM (`CONFIG_LSM` unchanged).
- **Changed:** `vmlinux`, `bzImage`, `System.map`, `build.json`, `abi.json`,
  `clean-repro.json` and `Module.symvers`.
- **`Module.symvers` notes:** the CRCs of the same nine hfs exports as in r16
  changed (`hfs_call`, `hfs_control`, `hfs_control_flagged`,
  `hfs_request_allocate`, `hfs_request_resolve_gap`, `hfs_retire`,
  `hfs_session_destroy`, `v9fs_hfs_register_provider`,
  `v9fs_hfs_unregister_provider`). No export was added or removed, and no
  packaged module imports any of them.
- **Source suites:** not rerun for this rebuild; re4 was reviewed and merged
  with its own tests.

## Daemon and window artifacts (xc4/r15 method)

- **Daemon:** `daemon.py` from `3430758` with the pinned `freebsd-cc` wrapper
  and sysroot. A forced build into the package and a second forced build
  into a separate package both gave `85900893…`. The module cache was seeded
  from the r15 cache (`go.mod` and `go.sum` unchanged), and nothing was
  downloaded. The four Go licences are unchanged.
- **Window artifacts:** `build-fdescfs-window-artifacts.sh` from `3430758`
  (recipe `c6ecd100…`). Both builds were byte-identical and reproduced the
  daemon. `controlledruntime.test` changed because `controlledruntime` links
  the changed packages. `fdescfs-namespace.c` is unchanged. The helper
  differs from the r15 `d6f2e4d0…` only in DWARF (the new output directory
  as `comp_dir`). After `llvm-objcopy --strip-debug`, both are `07c9f5a1…`.
  `docs/lab/zq3-fdescfs-window/pins.json` and `build-record.json` follow this
  build, and the recipe's default commit is now `3430758`.

## Native exporter (xd4 method, on f3)

**Attempt 1 was refused.** Following the r15/r16 route, attempt 1 staged
the `a1af0bd7` exporter files byte for byte with the xd4 native proof
records. `assemble.py` at `3430758` refused it in `copy_native_inputs`
("actual native recipe differs from candidate recipe"). Since 7e4,
`native_guard.py` is `34ddf37c…`, but the xd4 records bind `228c91f3…`.
Copying the packaged files does not help. The refused package, its evidence
root and receipts are kept in the run root's `refused-attempt/`, and no
manifest was written.

**The native rebuild:** with Paul's direct approval, the coordinator ran the
xd4 native build on f3 (`native-coord.py`, repository `3430758`), and the
build agent did no SSH.
- **Build root:** one fresh 0700 root, `/var/tmp/hfs-xz3-20261120-r17-r1`,
  left in place as evidence.
- **Guard:** every stage ran under `native_guard.py` `34ddf37c…` (1 CPU,
  512 MiB, 20 minutes per stage, 8 GiB disk), pinned to the running Rocky.
  Rocky's bhyve PID 78540, start time `Fri Nov 20 05:05:29 2026` and
  `rocky.conf` `c101adf1…` were the same before and after, and `/dev/vmm`
  held only `rocky`. The after-facts process list uses a narrower filter
  than the before-facts: it shows only the `rocky` bhyve line and omits the
  `system.pwd` and `system.grp` helper lines.
- **Builds:** two clean builds in the one root were equal in all 83 files,
  and `bhyve` reproduced `2fd7e32f…` byte for byte.
- **Only file that differs:** `freebsd/build.json` differs from `a1af0bd7`
  only in the build root path.
- **Source bundle:** inventory `35714a7f…`, selected archive `ed4bd55a…`,
  8670 source files.
- **lib9p `native-verify.sh`:** passed under the guard (exit 0).
- **Fixtures slip:** the first `native-verify` attempt failed locally,
  before any remote action, because `lib9p-fixtures/fixtures.h` had not been
  generated. It was regenerated with `tests/fixtures.py`, byte-identical to
  xd4's `efc26e4b…`, and the second attempt passed. That attempt's row in
  `steps.tsv` is malformed (a lone `18`), so the guard receipt is the
  authority.
- **Collection:** `collect-native.py`, run by the coordinator as read-only
  `cat` and `tar`, checked every collected file against the remote seal and
  bundle records and wrote the three native proof records that
  `assemble.py` reads.
- **Owned sources:** `stage-package.py` checked all 11 packaged owned lib9p
  inputs against the repository.

## What was reused

- **Linux inputs:** patches 0001, 0002, 0003 and 0005 with `series`, and the
  22 Linux and Monocypher notices (27 files). Each was checked against its
  current source: the repository patches, and the notices in the freshly
  prepared Linux tree.
- **What changed in the manifest (23 files, one removed):**
  - the seven kernel files above;
  - the daemon, `daemon-build.json`, `daemon-preparation.json` and
    `evidence/daemon-clean-repro.json`;
  - `freebsd/build.json` (root path only), `evidence/native-clean-repro.json`;
  - patch 0004, `inputs/linux-source-manifest.json` and
    `inputs/linux-source-inventory.json`;
  - the guard recipe in `inputs/build-recipes/` and
    `inputs/admission-recipes/` (with `test_native_guard.py` and the
    packaged `README.md`), and `evidence/actual-builder-bindings.json`;
  - `inputs/repository-source.json` and `installation-proposal.json`;
  - removed: `inputs/repository-changes.patch` (the baseline is now the
    build revision).
- **Previous candidate:** before the reuse, `a1af0bd7` passed `package.py
  check` and a full `admit.py` under the `3430758` tooling.
- **G3 init helper:** kept at `8c02b98f…`; it is not in the package.
  `cmd/homelabfs` links `internal/journal/segment` and `internal/fsservice`,
  but te4 only added `segment.Cursor`, which the mount path uses.
  `node-init` and `volume-init` use the unchanged `segment.Writer` and never
  mount.
- **Workstation tools:** `hfs-runtime-prep` and the signer
  `hfs-retained9p-operator` link the te4 and qe4 packages (`go list -deps`).
  They are not package content: the zq3-r17 fill must rebuild both and use
  the qe4 spec (ControlTickets 24). `tools/lab/directlaunch/argv.py` is
  unchanged, and so are the exporter bytes.

The kernel, daemon and window builds and the assembly ran from the r17
worktree at `3430758`. It had no edits during the builds or the assembly;
the pin and doc edits came afterwards.

## Admission and checks

`assemble.py` sealed and admitted the package. An independent `admit.py` run
against the pinned manifest passed. `check_candidate.py` refused all 26
real-copy mutations for their expected reasons, including the real CGO0
daemon `480713c2…`. The original and the restored copy passed again.

The tooling now admits only this manifest:
- refused as superseded: the r16 `a1af0bd7…`, the r15 `ac8020aa…`, the xd4
  `3a7131dd…`, the xc4 `e9c77e5b…`, the 5a4 `150deb29…`, the b94
  `3b128cc6…`, the r84 `4712e479…`, the l84 `e585206a…`, the y64
  `867ede36…`, the c64+d64 `66b5bb2b…`, 964 `e01fab21…`, z54 `2e0412a6…`,
  w54 `89c928d5…`, r54 `5eb42c50…`, f44 `b203799e…` and the earlier
  `3f770a88…`;
- still quarantined: `7e962627…`.

**What the next run must do again:**
- **Kernel plan:** the guest kernel is new, so the Rocky kernel plan must be
  regenerated for this manifest: build ID `360fbc6d…` and the new `bzImage`
  and `System.map` hashes. `config`, `modules.*` and the five modules keep
  their hashes; `Module.symvers` changed.
- **Daemon:** the daemon bytes are new, so the f3 observer check of
  `8668e5f0…` does **not** carry over. `85900893…` needs
  `--check-process-observer` under the bounded host guard on f3 (Phase 4
  step 1a).
- **Window artifacts:** they are new; the fdescfs window pins name
  `1f042fd6…` and `6d78b1de…`.
- **Workstation tools:** `hfs-runtime-prep` and the signer must be rebuilt
  at the fill commit, and the fill must use the qe4 spec.
- **Approval steps:** booting the kernel, checking SELinux enforcing, and
  mounting through it remain zq3 steps that need explicit approval.
