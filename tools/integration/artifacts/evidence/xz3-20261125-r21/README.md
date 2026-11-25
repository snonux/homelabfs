# xz3 second r21 rebuild (2026-11-24): s64 hfs_fdattr, hh4 ABANDON and the Fedora 45 toolchain

The candidate was built and admitted on 2026-11-24. The `20261125` in its
directory and evidence names is the planned zq3-r21 run day, chosen in
advance as the earlier rebuilds did, because pins and the run plan
reference it. The run is still zq3-r21: the first r21 rebuild never ran.

This candidate supersedes the [first r21 rebuild](../xz3-20261123-r21/README.md)
(`1428d4db…`, source `91b9ee1`, never run). Paul approved a fresh build from
main on 2026-11-24 ("Build, fill and run"), so that zq3-r21 runs the merges
that landed after `91b9ee1`. Three groups of them change package content:

- **Guest kernel** (patches 0002-0005 and `source.json`): dh4 (`ee14da2`, a
  blocked session's DRAIN_WRITE and GAP_RESOLVE are exempt from the poll
  halt), kn1 (`fee0acc`: tg4 ATTR_TOUCH, ch4 advance error mapping, wj4
  items 2-4), 5f4 (`cb11ecb`, a stop wakes the view waits), s64 guest
  (`7f204dc`, GETATTR/SETATTR through the open description behind the
  `hfs_fdattr` mount option), hh4 guest (`1ee3a09`, a blocked session
  ABANDONs its stranded never-prepared claims), wj4 item 1 (`9917a0a`, the
  mmapped mark ends with the last VMA and an empty page cache) and bl4
  (`ae6c6c9`, comment fixes in patch 0003).
- **lib9p exporter** (owned `hfs9p.c`): s64 `_args` (the described
  GETATTR/SETATTR shapes) and the hh4 ABANDON forwarder. The other ten owned
  inputs are unchanged.
- **Daemon** (non-test Go files the daemon links): vj4 (`179deee`, a
  mutating CALL no longer recalls the requester's own observation leases),
  dh4, s64 host (`157a132`, GETATTR/SETATTR may carry the open DESCRIPTION)
  and hh4 host (`33a7dfa`, session-bound ABANDON returns never-prepared
  claims' credit as gaps).

The other merges since `91b9ee1` change no package file: bh4, fl4, k84 and
ml4 touch tests and harnesses, nl4 and ol4 the lab tooling, and the rest are
docs and records. So the guest kernel, the CGO daemon (with the window
artifacts) and the native exporter were rebuilt. With s64 in all three
parts, this is the first candidate in `controller.FD_ATTR`.

**New toolchain.** The workstation was upgraded from Fedora 44 to Fedora 45
on 2026-11-24 (about 15:08 UTC): golang 1.27.4, binutils 2.47, clang 23.1.5,
gcc 16.2.1 (fc45 build). `daemon.py` required go1.26.10 and refused, so
Paul decided to re-pin instead of downgrading: `3081d31` names `go1.27.4`
once (`GO_RELEASE`). Every earlier candidate was built on Fedora 44 with
go1.26.10. `admit.py` checks a daemon against the Go version its own build
record names, so those candidates still verify. What the new toolchain
changed:

- **Daemon and window tools:** built with `go1.27.4-X:nodwarf5`; the window
  helper's code changed with clang 23 (stripped `7872b1b4…`; every Fedora 44
  build stripped to `07c9f5a1…`).
- **Kernel:** `linux/config` differs from `1428d4db` only in
  `CONFIG_AS_VERSION` and `CONFIG_LD_VERSION` (Kconfig records the binutils
  version; the operator config fragment is unchanged). The five modules,
  `abi-negative.json`, `kernel.release` and `modules.*` still came out
  byte-identical to `1428d4db`, with the same GNU build IDs.
- **Native exporter:** built on f3 with the FreeBSD toolchain as before;
  the workstation change does not reach it.

**Build and offline admission only:** nothing was installed or booted, no
VM was touched, and on f3 nothing ran with privilege. So `installed`,
`booted` and `native_accepted` stay false, and publication stays null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261125-r21/package` |
| Manifest SHA256 | `f21450ec7597291f2353f48b1d184c6397324783ebbcf73aa73d0b60ebc2a106` (194 files) |
| Source checkout and repository baseline | `3081d31cb035aaf72730e0d52d2598602117ebbc` (main `a670592` plus the Go re-pin; the daemon's build revision; there is no `inputs/repository-changes.patch`) |
| Kernel | `6.1.190-hfs-xz3`: `linux/bzImage` `74728cac0331acb4b46e857137dcec190892655cd9e7b30cdea37f5cc5c98066`, `linux/vmlinux` `5cb3ef3abbf02c241dd3f15b483d8a6dab9d776e42dc4fcd1b441dbc144be6f3` (GNU build ID `bd4fa7530108bd1bbafec82ba2d6d810507baf81`), `linux/config` `692ed3fa048ec59704f554e3ddcea11b66e3f420e23489022b25387302fc1f6c`, `System.map` `debb6bda8dcfc1d1e2bb20a22d2eca2a71084c76fb5aa561571ca7838a7f047a` |
| Modules | the five modules are byte-identical to `1428d4db` (same GNU build IDs). `Module.symvers` changed (`45fef3f6…`, CRCs only, see below) |
| CGO daemon | `c7b896484c6dcb4fe23b6484fdfc4d71a56a16e9bff4b7410e430a0a3cea667e` (11,935,184 bytes, GNU build ID `9694935d7eb301976eaf3fa8135b4295814004ea`, go1.27.4) |
| Native exporter | `freebsd/bhyve` `8cadae403dc3f0c7add38f16ec8fae1d00b17c1802f00411896e96fa083aa4d4` (GNU build ID `03b06c86e966eed086f0196eb66c153648dc6acf`; rebuilt on f3, two clean builds equal) |
| Window test binary / helper | `0129385e23bb5da09bbd6e6a8da4a46caf7a6cfea1fc81c55a31c96318b23a46` / `2157b861b6c8515ee91e40047fab9feb6b16213e9ebd41628c6743dccdb61e5c` (from `3081d31`) |

What the files hold:
- [Evidence](evidence.json): hashes, stage timings, source-suite results,
  receipts, control names, the toolchain and the config difference.
- [kernel-clean-repro.json](kernel-clean-repro.json): the `compare_kernel.py`
  record (also packaged as `linux/clean-repro.json`), the stage table and
  the ABI negative-control receipt.
- [reused-files.json](reused-files.json): the 24 reused Linux files, the 135
  files that came out identical and the 35 that changed. No file was added
  or removed.
- [native-build-records.json](native-build-records.json): the f3 build
  facts, guard receipts, comparison and native-verify.

## Kernel rebuild (r21 method)

- **Source:** a fresh `prepare.py` tree from the cached pinned archive
  `4f3c00db…` (`/home/paul/tmp/claude/c64-config/linux-pinned.tar.gz`;
  nothing downloaded), prepared at `a670592`. The re-pin commit changes no
  Linux input.
- **Two builds at the same O path:** every `kernel.py` stage ran under a
  user systemd scope (two CPUs, 2 GiB, no swap, the r17 40-minute bound).
  The first build tree was moved aside before the repeat. `abi.py collect`
  ran on both packages.
- **No timeouts:** `kernel.py` still gives every `make` 1190 s. The first
  `vmlinux` took 665 s and the repro 660 s; the resume path never ran.
- **Clean repro:** `compare_kernel.py` found both builds identical (13
  artifacts, the five modules among them, plus the full module ABI).
- **ABI negative controls:** `check_abi_mismatch.py` refused its four
  controls (stock kernel release, different operator config identity,
  missing and different module layout CRC).
- **Unchanged against `1428d4db`:** `abi-negative.json`, `kernel.release`,
  `modules.*` and the five modules (same GNU build IDs). SELinux stays an
  active LSM (`CONFIG_LSM` unchanged).
- **Changed:** `vmlinux`, `bzImage`, `System.map`, `Module.symvers`,
  `config` (the two version stamps), `build.json`, `abi.json` and
  `clean-repro.json`.
- **`Module.symvers`:** as in r20 and r21, the CRCs of the nine hfs exports
  changed (`hfs_call`, `hfs_control`, `hfs_control_flagged`,
  `hfs_request_allocate`, `hfs_request_resolve_gap`, `hfs_retire`,
  `hfs_session_destroy`, `v9fs_hfs_register_provider`,
  `v9fs_hfs_unregister_provider`). No export was added or removed, and no
  packaged module imports any of them.
- **Source suites:** nine suites passed against the fresh tree, run
  alongside the kernel builds:
  - `transport`, `mount_hello`, `view`, `view --pressure` and `setattr`,
    each with `--sanitize`;
  - the `controlled_received` scenarios `passed-grant`, `terminal-smoke`
    and `advance`;
  - new for this rebuild, `controlled_received --scenario both` (all 20
    scenarios, `abandon` included; 1131 s), because a guest-patch merge
    earlier on 2026-11-24 had broken that harness unnoticed (ml4 fixed it).
  The host `go test -race` stages used go1.27.4.

## Daemon and window artifacts (r21 method, new toolchain)

- **Daemon:** `daemon.py` from `3081d31` with the pinned `freebsd-cc`
  wrapper and sysroot. A forced build into `daemon-first` and a second
  forced build into `daemon-repro` both gave `c7b89648…`.
  `stage-package.py` copied the first build into the package. The module
  cache was seeded from the `1428d4db` cache (`go.mod` and `go.sum`
  unchanged), and nothing was downloaded. The Go licence closure is
  unchanged.
- **Window artifacts:** `build-fdescfs-window-artifacts.sh` (recipe
  `c778b058…`, the `3081d31` file) built from `3081d31`. Both builds were
  byte-identical and reproduced the daemon. `fdescfs-namespace.c` is
  unchanged, but the helper's code changed with clang 23 (see above).
  `docs/lab/zq3-fdescfs-window/pins.json` and `build-record.json` follow
  this build, and the recipe's default commit is now `3081d31`.

## Native exporter (r17 method, on f3)

s64 and hh4 changed the owned `hfs9p.c`, so the exporter had to be
rebuilt. The recipes are unchanged since r17 (`native_guard.py`
`34ddf37c…`). The coordinator scripts are the first r21 ones with the
repository set to the worker worktree at `a670592` (the re-pin commit
changes no native input); `native-resume.sh` was not needed.

- **Build root:** one fresh 0700 root, `/var/tmp/hfs-xz3-20261125-r21-r1`,
  left in place as evidence.
- **Access:** pinned-key SSH only (`-F /dev/null`,
  `StrictHostKeyChecking=yes`, `docs/lab/f3-host-known_hosts`,
  `HostKeyAlias=f3`, `paul@10.20.0.133`). No privilege, install or mount,
  and no VM action.
- **Guard:** every stage ran under `native_guard.py` (1 CPU, 512 MiB,
  20 minutes per stage, 8 GiB disk), pinned to the running Rocky.
- **Rocky:** bhyve PID 40425, start time `Sun Nov 22 17:19:43 2026` (UTC)
  and `rocky.conf` `c101adf1…` were the same before and after. `/dev/vmm`
  held only `rocky`, so no iom VM was running.
- **Builds:** two clean builds in the one root were equal in all 83 files.
  Against `1428d4db`, only `bhyve`, `lib9p.a`, `lib9p_pie.a` and
  `freebsd/build.json` differ. `libvmmapi*` and every share file came out
  identical.
- **Source bundle:** inventory `3909c5bf…`, selected archive `52edd28f…`,
  8670 source files.
- **lib9p `native-verify.sh`:** passed under the guard (exit 0, 20 s), with
  a freshly generated `fixtures.h` (`679eba06…`). It differs from the
  `1428d4db` `1684ceb1…` only by hh4's `controlled_abandon` and
  `controlled_abandon3` fixtures.
- **Collection:** `collect-native.py` used read-only `cat` and `tar`, checked
  every collected file against the remote seal and bundle records, and wrote
  the three native proof records that `assemble.py` reads.
- **Owned sources:** `stage-package.py` checked that all 11 owned lib9p
  inputs in the repository equal the seed staged on f3; only `hfs9p.c`
  differs from `1428d4db`.
- No SSH reset this time: `native-run.sh` ran every step in one pass.

## What was reused and what changed

- **Linux inputs (24 files):** patch 0001 with `series`, and the 22 Linux
  and Monocypher notices. Each was checked against its current source.
- **Native:** nothing reused. The 88 collected files replace the
  `1428d4db` copies.
- **What changed in the manifest (35 files, none added or removed):**
  - the eight kernel files above;
  - the daemon, `daemon-build.json`, `daemon-preparation.json` and
    `evidence/daemon-clean-repro.json`;
  - `bhyve`, `lib9p.a`, `lib9p_pie.a`, `freebsd/build.json`, the selected
    source archive, source inventory and bundle record, and
    `evidence/native-clean-repro.json`;
  - `inputs/freebsd-owned/src/hfs9p.c`;
  - patches 0002-0005, `inputs/linux-source-manifest.json` and
    `inputs/linux-source-inventory.json`;
  - the re-pinned recipes `daemon.py` (build and admission copies) and
    `test_artifacts.py`, `inputs/toolchains.json`,
    `evidence/actual-builder-bindings.json`, `inputs/repository-source.json`,
    `installation-proposal.json` and the packaged
    `inputs/admission-recipes/README.md`.
- **Previous candidate:** `1428d4db` still passes `package.py check` and a
  full `admit.py` under the `a670592` tooling (receipts `r21-container`,
  `r21-readmit`).
- **G3 init helper:** kept at `8c02b98f…` (built with go1.26.10); it is not
  in the package. Of the packages `cmd/homelabfs` links, only
  `internal/guestbridge` changed (hh4, s64, dh4). It is reached only through
  `fuseadapter` on the mount path, and node-init and volume-init never
  mount.
- **Workstation tools:** `hfs-runtime-prep` and the signer
  `hfs-retained9p-operator` are not package content; the fill must rebuild
  both at the fill commit, now with go1.27.4. The runtimeprep example spec
  pins the new exporter.

The kernel was prepared and the native inputs staged at `a670592`; the
daemon, window builds and the assembly ran from `3081d31`, with no edits
during them. The pin and doc edits came afterwards.

## Admission and checks

`assemble.py` sealed and admitted the package. An independent `admit.py` run
against the pinned manifest passed. `check_candidate.py` refused all 26
real-copy mutations for their expected reasons, including the real CGO0
daemon `480713c2…`. The original and the restored copy passed again.

The tooling now admits only this manifest, and `FD_ATTR` holds it:
- refused as superseded: the first r21 `1428d4db…` and every earlier
  candidate listed in the [first r21 README](../xz3-20261123-r21/README.md);
- still quarantined: `7e962627…`.

`directlaunch/argv.py`, its README and
`internal/runtimeprep/testdata/zq3-spec.json` pin the new exporter
`8cadae40…`.

**Known environmental failure:** in the retained9p suite 434 of 435 tests
pass. `test_pod_fixture.RealRpmTests.test_manifest_build_and_chroot_start`
refuses with "mapped file outside /usr: /etc/ld.so.cache" on the Fedora 45
workstation. It fails the same way on main `a670592` without these
changes, so it comes from the upgrade, not from this candidate; it is
tracked as a separate task.

**What the next run must do again:**
- **Kernel plan:** the guest kernel is new, so the Rocky kernel plan must be
  regenerated for this manifest: build ID `bd4fa753…`, and new `bzImage`,
  `System.map`, `Module.symvers` and `config` hashes. `modules.*` and the
  five modules keep their hashes.
- **Daemon:** `c7b89648…` is new bytes from a new Go release and needs
  `--check-process-observer` under the bounded host guard on f3 (Phase 4
  step 1a).
- **Exporter:** `8cadae40…` has never run. The fill must pin it in the
  runtime config (`Exporter.SHA256`) and stage it.
- **Window artifacts:** they are new; the fdescfs window pins name
  `0129385e…` and `2157b861…`.
- **Workstation tools:** `hfs-runtime-prep` and the signer must be rebuilt
  at the fill commit.
- **`hfs_fdattr`:** allowed for this candidate, never implied. A plan that
  wants it sets `fd_attr` and the three bootstrap fields (see the
  retained9p README).
- **Approval steps:** booting the kernel, checking SELinux enforcing, and
  mounting through it remain zq3 steps that need explicit approval.
