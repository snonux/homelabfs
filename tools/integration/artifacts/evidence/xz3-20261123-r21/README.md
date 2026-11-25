# xz3 r21 rebuild (2026-11-22): zq3-r20 ancestor walk fix, xf4 advance trigger and exporter EAGAIN retry

**Superseded on 2026-11-24, never run,** by the
[second r21 rebuild](../xz3-20261125-r21/README.md) (`f21450ec…`, source
`3081d31`) for the same run, zq3-r21, now planned on 2026-11-25. Main had
gained s64 (`hfs_fdattr` in guest kernel, exporter and daemon), hh4
(ABANDON of a blocked session's stranded claims), dh4, vj4, 5f4, kn1 and
wj4 item 1, and Paul approved a fresh build. That rebuild is also the first
on the Fedora 45 toolchain (go1.27.4). The tooling refuses this manifest as
superseded. It still passes `package.py check` and `admit.py` under the
`a670592` tooling.

The candidate was built and admitted on 2026-11-22. The `20261123` in its
directory and evidence names is the planned zq3-r21 run day, chosen in
advance as r20 did, because pins and the run plan reference it.

This candidate supersedes the [r20 rebuild](../xz3-20261122-r20/README.md)
(`b37bcb06…`, source `d71ae02`). In zq3-r20
([record](../../../../docs/lab/results/20261122-zq3-r20/README.md)) 1h4
worked, but T1 failed its first access check: the host required search
permission on every ancestor, so a non-owner got `EACCES` below the 0700
root (R20-1). The guest also never sent the xf4 advance, because it counted
every registered inode as cached. Three groups of merges since r20 change
package content:

- **tj4** (merge `91b9ee1`, patches 0003/0004 and `source.json`): the
  cacheless test counts only mmapped inodes. `hfs_mmap_file` marks the inode
  with `hfs_view_mmap_inode` before installing `hfs_mmap_ops`. A mount with
  registered roots can now send the advance.
- **9h4** (merge `4904942`, the owned lib9p sources `hfs9p_bootstrap.inc`,
  `hfs9p_recovery.inc` and `hfs9p_supervisor.c`): the exporter's controlled
  C bootstrap waits out backend-mutex contention (`EAGAIN`, up to 10 s per
  call) instead of fencing itself under CPU load.
- **Daemon merges** (non-test Go files the daemon links):
  - sj4 (`bded988`, `internal/guestbridge/dispatch.go`): revalidatePath keeps
    the ancestor identity re-walk but drops the per-ancestor search check
    (R20-1);
  - 2h4 (`dd051a4`): per-ticket BLOCKED reasons in OBSERVE;
  - 0a4 (`2de4ce5`): a recall participant silent for 60 s after STOPPED is
    fenced so the others REOPEN;
  - a84 part 1 (`f22dc1a`): refusal traces on the claim (also
    `internal/guestbridge/service.go`);
  - l64 (`85e7950`): open claims carry their age;
  - fh4 (`c11ec7c`): operator-signed retirement of a fenced session releases
    its shared charges;
  - nj4 (`e2f406a`): the pinned exporter copy is written under
    `syscall.ForkLock`.

The other merges change no package file: s54, 094+2a4 and qc4 touch only
tests and harnesses, and the r20 candidate merge, the zq3-r20 fill and the
zq3-r20 run record are docs and records. So the guest kernel, the CGO
daemon (with the window artifacts) and the native exporter were rebuilt.
The exporter was rebuilt on f3 with Paul's explicit approval ("Yes, rebuild
on f3").

**Build and offline admission only:** nothing was installed or booted, no
VM was touched, and on f3 nothing ran with privilege. So `installed`,
`booted` and `native_accepted` stay false, and publication stays null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261123-r21/package` |
| Manifest SHA256 | `1428d4dbc149f15866ca6ff1dd259642e2ab34bb5b943c8b1cf1e92306ef4fd9` (194 files) |
| Source checkout and repository baseline | `91b9ee1df2b7f26baae0032d3928286743fe4e57` (the daemon's build revision; there is no `inputs/repository-changes.patch`) |
| Kernel | `6.1.190-hfs-xz3`: `linux/bzImage` `b3f2f72b8ed28a6b6889acbd300674efde9a9ff9b20cbaa622aecefd7802c047`, `linux/vmlinux` `796da1c9936a35e0ca756db486442b6acc4e391a905ee1b851b8b8ac6a709a03` (GNU build ID `f951817780097db10b5f7d68bbe01c80abc225a6`), `linux/config` `28fe4a8e…` (unchanged), `System.map` `35111e72305eed9d1bb246de765451a6d50179d4af7fd28304c7fb68b1e52bdc` |
| Modules | the five modules are byte-identical to `b37bcb06` (same GNU build IDs). `Module.symvers` changed (`28ec9854…`, CRCs only, see below) |
| CGO daemon | `013515a8731cde2bad643230a7822221f2ff086a6d72812778eb78d69c761200` (11,218,992 bytes, GNU build ID `0366a6fbd9065ac5dcaa04bd460cc054df294149`) |
| Native exporter | `freebsd/bhyve` `368a021d2046bf9c2845a4d80345d5665f948bd6136a3a233827505d06a052db` (GNU build ID `d421d44c71427bae7544be98c8b670d9a69ec1d5`; rebuilt on f3, two clean builds equal) |
| Window test binary / helper | `88073c2c86a2d5cec64a82d564f19f6f629ff4a44258ae80a6cc9644972c0bb9` / `c14cc4d21f17f78a88fbe47233dd6123f58be427f3765ab2365ebaddc16edbc9` (from `91b9ee1`) |

What the files hold:
- [Evidence](evidence.json): hashes, stage timings, source-suite results,
  receipts and control names.
- [kernel-clean-repro.json](kernel-clean-repro.json): the `compare_kernel.py`
  record (also packaged as `linux/clean-repro.json`), the stage table and
  the ABI negative-control receipt.
- [reused-files.json](reused-files.json): the 26 reused Linux files, the 139
  files that came out identical and the 29 that changed. No file was added
  or removed.
- [native-build-records.json](native-build-records.json): the f3 build
  facts, guard receipts, comparison and native-verify.

## Kernel rebuild (r20 method)

- **Source:** a fresh `prepare.py` tree from the cached pinned archive
  `4f3c00db…` (`/home/paul/tmp/claude/c64-config/linux-pinned.tar.gz`;
  nothing downloaded), prepared at `91b9ee1`.
- **Two builds at the same O path:** every `kernel.py` stage ran under a
  user systemd scope (two CPUs, 2 GiB, no swap, the r17 40-minute bound).
  The first build tree was moved aside before the repeat. `abi.py collect`
  ran on both packages.
- **No timeouts:** `kernel.py` still gives every `make` 1190 s. No stage
  reached it. The first `vmlinux` took 1114 s while the daemon, window and
  suite jobs shared the host. The repro `vmlinux` took 868 s. The resume
  path never ran.
- **Clean repro:** `compare_kernel.py` found both builds identical (13
  artifacts, the five modules among them, plus the full module ABI).
- **ABI negative controls:** `check_abi_mismatch.py` refused its four
  controls (stock kernel release, different operator config identity,
  missing and different module layout CRC).
- **Unchanged against `b37bcb06`:** `config`, `abi-negative.json`,
  `kernel.release`, `modules.*` and the five modules (same GNU build IDs).
  SELinux stays an active LSM (`CONFIG_LSM` unchanged).
- **Changed:** `vmlinux`, `bzImage`, `System.map`, `Module.symvers`,
  `build.json`, `abi.json` and `clean-repro.json`.
- **`Module.symvers`:** as in r20, the CRCs of the nine hfs exports changed:
  `hfs_call`, `hfs_control`, `hfs_control_flagged`, `hfs_request_allocate`,
  `hfs_request_resolve_gap`, `hfs_retire`, `hfs_session_destroy`,
  `v9fs_hfs_register_provider` and `v9fs_hfs_unregister_provider`. The tj4
  patches change types that their signatures reach (genksyms). No export
  was added or removed, and no packaged module imports any of them.
- **Source suites:** the eight r20 suites passed against the fresh tree,
  run alongside the first build:
  - `transport`, `mount_hello` (23 cases), `view` and `view --pressure`
    (both with tj4's mmapped-inode cases; `--pressure` also runs tj4's
    `hfs_mmap_file` source guard) and `setattr`, each with `--sanitize`;
  - the `controlled_received` scenarios `passed-grant`, `terminal-smoke` and
    `advance` (the tj4 guest against the `91b9ee1` host).

## Daemon and window artifacts (r20 method)

- **Daemon:** `daemon.py` from `91b9ee1` with the pinned `freebsd-cc`
  wrapper and sysroot. A forced build into `daemon-first` and a second
  forced build into `daemon-repro` both gave `013515a8…`.
  `stage-package.py` copied the first build into the package. The module
  cache was seeded from the r20 cache (`go.mod` and `go.sum` unchanged), and
  nothing was downloaded. The four Go licences are unchanged.
- **Window artifacts:** `build-fdescfs-window-artifacts.sh` (recipe
  `97644ea3…`, the `91b9ee1` file) built from `91b9ee1`. Both builds were
  byte-identical and reproduced the daemon. `controlledruntime.test` changed
  with the daemon's packages. `fdescfs-namespace.c` is unchanged. The helper
  differs from the r20 `edae7dc3…` only in DWARF (the new output directory
  as `comp_dir`). After `llvm-objcopy --strip-debug` it is `07c9f5a1…`, as
  before. `docs/lab/zq3-fdescfs-window/pins.json` and `build-record.json`
  follow this build, and the recipe's default commit is now `91b9ee1`.

## Native exporter (r17 method, on f3)

9h4 changed three of the eleven owned exporter inputs, so the exporter had
to be rebuilt. The recipes are unchanged since r17 (`native_guard.py`
`34ddf37c…`). The coordinator scripts are the r17 ones with the repository
set to the r21 worktree, the Rocky PID and start time updated, and two real
changes: `facts-after.sh` now matches every `bhyve: ` process (as the
before-facts do, so both lists use the same filter), and `native-resume.sh`
is new (see the deviation below).

- **Build root:** one fresh 0700 root, `/var/tmp/hfs-xz3-20261123-r21-r1`,
  left in place as evidence.
- **Access:** pinned-key SSH only (`-F /dev/null`,
  `StrictHostKeyChecking=yes`, `docs/lab/f3-host-known_hosts`,
  `HostKeyAlias=f3`, `paul@10.20.0.133`). No privilege, install or mount,
  and no VM action.
- **Guard:** every stage ran under `native_guard.py` (1 CPU, 512 MiB,
  20 minutes per stage, 8 GiB disk), pinned to the running Rocky.
- **Rocky:** bhyve PID 40425, start time `Sun Nov 22 17:19:43 2026` (UTC)
  and `rocky.conf` `c101adf1…` were the same before and after. `/dev/vmm`
  held only `rocky`, so no iom VM was running. Both fact lists use the same
  process filter.
- **Builds:** two clean builds in the one root were equal in all 83 files.
  Against `b37bcb06`, only `bhyve`, `lib9p.a`, `lib9p_pie.a` and
  `freebsd/build.json` differ. `libvmmapi*` and every share file came out
  identical.
- **Source bundle:** inventory `19239259…`, selected archive `5baa8621…`,
  8670 source files.
- **lib9p `native-verify.sh`:** passed under the guard (exit 0, 17 s), with
  a freshly generated `fixtures.h` (`1684ceb1…`). It differs from the
  r17/xd4 `efc26e4b…` only by 3h4's `view_read_domain_advance` fixture.
- **Collection:** `collect-native.py` used read-only `cat` and `tar`. It
  checked every collected file against the remote seal and bundle records,
  and wrote the three native proof records that `assemble.py` reads.
- **Owned sources:** `stage-package.py` checked that all 11 owned lib9p
  inputs in the repository equal the seed staged on f3.

**Deviation: SSH reset.** `native-run.sh` stopped after its
`source-cached` step. Both `source-copy-cached` and `source-extract`
completed under the guard; the remote receipt shows exit 0 after 37.2 s.
But the peer reset the SSH session carrying `source-extract` after the
stage had finished, so the coordinator recorded exit 255. A read-only
inspection found the root complete, no leftover run process and Rocky
unchanged. `native-resume.sh` then ran the remaining steps unchanged on the
same root, from the first build through native-verify. Both builds still
started from the one extracted source, as in r17. The reset's cause is
unknown, and no other SSH failure occurred. The `source-cached` step took
336 s against r17's 40 s while the extract itself took 37 s, so the session
hung for about five minutes before the reset.

## What was reused and what changed

- **Linux inputs (26 files):** patches 0001, 0002 and 0005 with `series`,
  and the 22 Linux and Monocypher notices. Each was checked against its
  current source: the repository patches, and the notices in the freshly
  prepared Linux tree.
- **Native:** nothing reused. The 88 collected files replace the r17-era
  copies.
- **What changed in the manifest (29 files, none added or removed):**
  - the seven kernel files above;
  - the daemon, `daemon-build.json`, `daemon-preparation.json` and
    `evidence/daemon-clean-repro.json`;
  - `bhyve`, `lib9p.a`, `lib9p_pie.a`, `freebsd/build.json`, the selected
    source archive, source inventory and bundle record, and
    `evidence/native-clean-repro.json`;
  - the three 9h4 owned sources under `inputs/freebsd-owned/src/`;
  - patches 0003/0004, `inputs/linux-source-manifest.json` and
    `inputs/linux-source-inventory.json`;
  - `inputs/repository-source.json`, `installation-proposal.json` and the
    packaged `inputs/admission-recipes/README.md` (the artifacts README at
    `91b9ee1`).
- **Previous candidate:** `b37bcb06` still passes `package.py check` and a
  full `admit.py` under the `91b9ee1` tooling (receipts `r20-container`,
  `r20-readmit`).
- **G3 init helper:** kept at `8c02b98f…`; it is not in the package. Of the
  packages `cmd/homelabfs` links (`go list -deps`), only
  `internal/guestbridge` changed (`dispatch.go` for sj4, `service.go` for
  a84). It is reached only through `fuseadapter` on the mount path, and
  node-init and volume-init never mount.
- **Workstation tools:** `hfs-runtime-prep` and the signer
  `hfs-retained9p-operator` link `internal/viewcontrol`,
  `internal/guestbridge`, `internal/controlledruntime` and
  `internal/controlledvalidation`. They are not package content, so the
  zq3-r21 fill must rebuild both. The runtimeprep example spec now pins the
  new exporter.

The kernel, daemon and window builds and the assembly ran from the r21
worktree at `91b9ee1`, with no edits during them. The pin and doc edits
came afterwards.

## Admission and checks

`assemble.py` sealed and admitted the package. An independent `admit.py` run
against the pinned manifest passed. `check_candidate.py` refused all 26
real-copy mutations for their expected reasons, including the real CGO0
daemon `480713c2…`. The original and the restored copy passed again.

The tooling now admits only this manifest:
- refused as superseded: the r20 `b37bcb06…`, the r19 `09acd09d…`, the r18
  `b2a4f69e…`, the r17 `f614d52a…`, the r16 `a1af0bd7…`, the r15
  `ac8020aa…`, the xd4 `3a7131dd…`, the xc4 `e9c77e5b…`, the 5a4
  `150deb29…`, the b94 `3b128cc6…`, the r84 `4712e479…`, the l84
  `e585206a…`, the y64 `867ede36…`, the c64+d64 `66b5bb2b…`, 964
  `e01fab21…`, z54 `2e0412a6…`, w54 `89c928d5…`, r54 `5eb42c50…`, f44
  `b203799e…` and the earlier `3f770a88…`;
- still quarantined: `7e962627…`.

`directlaunch/argv.py`, its README and
`internal/runtimeprep/testdata/zq3-spec.json` pin the new exporter
`368a021d…`, as xd4 did for `2fd7e32f…`.

**What the next run must do again:**
- **Kernel plan:** the guest kernel is new, so the Rocky kernel plan must be
  regenerated for this manifest. It needs build ID `f9518177…` and the new
  `bzImage`, `System.map` and `Module.symvers` hashes. `config`,
  `modules.*` and the five modules keep their hashes.
- **Daemon:** the daemon bytes are new, so the zq3-r20 f3 observer check of
  `c04c519f…` does **not** carry over. `013515a8…` needs
  `--check-process-observer` under the bounded host guard on f3 (Phase 4
  step 1a).
- **Exporter:** `368a021d…` has never run. The fill must pin it in the
  runtime config (`Exporter.SHA256`) and stage it.
- **Window artifacts:** they are new; the fdescfs window pins name
  `88073c2c…` and `c14cc4d2…`.
- **Workstation tools:** `hfs-runtime-prep` and the signer must be rebuilt
  at the fill commit.
- **Approval steps:** booting the kernel, checking SELinux enforcing, and
  mounting through it remain zq3 steps that need explicit approval.
