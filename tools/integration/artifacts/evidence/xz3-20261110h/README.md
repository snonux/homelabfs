# xz3 y64 rebuild (2026-11-10): zq3-r7 kernel, daemon and exporter fixes

This candidate supersedes the [c64+d64 rebuild](../xz3-20261110g/README.md)
(`66b5bb2b…`, source `060dea0`). After zq3-r7, fixes were merged in all three
components, so the guest kernel, the CGO daemon and the native bhyve exporter
were all rebuilt (task y64, the prerequisite for zq3-r8):

- **Guest kernel** (`tools/integration/linux9p` patches 0001, 0003, 0004,
  0005 and `source.json`):
  - g64: hfs `.show_options`, an exact `cache=mmap`, and one block log per session;
  - m64: a Sources floor in `hfs_go_bounds`, a log line for every fault block,
    and a silent clean unmount.
- **CGO daemon:** i64, o64, r64, n64, j64, u64, v64 and x64, in
  `internal/viewcontrol`, `controlledvalidation`, `guestbridge` and
  `controlledruntime`.
- **Native exporter and lib9p:** 564, e64, p64 and w64, in `hfs9p.c` and
  `hfs9p_recovery.inc`. The fixes cover guest-order forwarding and the
  fail-back of permanently refused cells. Every candidate since `3f770a88…`
  had reused the 2026-11-08 native build `36423b6d…`, which lacks them.

The f64 method (kernel twice at the same O path, daemon twice, window build)
was combined with the 2026-11-08 native method on f3. **Only the native
build ran on a host.** Nothing was installed or booted, and no VM, mount or
module was touched. `installed`, `booted` and `native_accepted` stay false,
and publication stays null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261110h/package` |
| Manifest SHA256 | `867ede3630e4ee0a609f88b0752670fac5b7a0c0e0f9d2a340031110da32c334` (194 files) |
| Source checkout | `b8697101df601d7748bae85721e91c0e01aae474` (also the repository baseline and the daemon's build revision) |
| Kernel | `6.1.190-hfs-xz3`: `linux/bzImage` `40afad22…`, `linux/vmlinux` `d2d3e5ab…` (GNU build ID `870bb9d1d61ca10ecbbdb0a494a2fb5c449b92ee`), `linux/config` `28fe4a8e…` (unchanged from c64), `System.map` `1ffea83f…` |
| Modules | the five modules are byte-identical to `66b5bb2b` (same GNU build IDs; `virtio_net` `07299518…`) |
| CGO daemon | `34fb05b28357b42e37816370f9bb6c0ec046ff2e98763dafff87c3680021694c` (10,878,480 bytes, GNU build ID `30c41c9a…`) |
| Native exporter | `freebsd/bhyve` `ef8f17de8754f8bdd6d5d517f15a859e87a2f4a8ca1542394f5608a8e40823b1` (GNU build ID `6870705e…`), owned-source inventory `8ad5ca2c…` |
| Window test binary / helper | `f71f0bb8…` / `3d992cdf…` (from `b869710`) |

[Evidence](evidence.json) records hashes, stage timings, receipts and control
names. [native-build-records.json](native-build-records.json) has the f3
staging hashes, every guard receipt, the clean-repro comparison and the f3
facts before and after.

## Native build on f3

The build reused the 2026-11-08 coordinator (`native-coord.py`,
`collect-native.py`) with two changes. SSH uses only the repository's pinned
host key (`docs/lab/f3-host-known_hosts`, `-F /dev/null`,
`StrictHostKeyChecking=yes`). The recipes come from the y64 worktree. The
recipes are unchanged since 2026-11-08: `native_build.py` `263775fd…`,
`native_source.py` `31f9ce90…`, `native_guard.py` `228c91f3…`, `elf.py`,
`runner_process.py` and `native_bundle.py` `1a17c33b…`.

- **Root and guard:** one fresh private 0700 root,
  `/var/tmp/hfs-xz3-20261110h-r1`, left in place as evidence. Every stage ran
  under `native_guard.py`: one CPU, 512 MiB aggregate RSS, 20 minutes per
  stage, 8 GiB disk. It was pinned to the current Rocky incarnation (PID
  6472, lstart `Tue Nov 10 09:35:04 2026` UTC, `rocky.conf` `c101adf1…`,
  `memory=12G`, `/dev/vmm` only `rocky`). Rocky was unchanged afterwards. The
  peak guarded RSS was 398 MiB (the lib9p native tests); the build stages
  peaked at 260 MiB.
- **Source:** the pinned archive `044a42cc…` and its capture record were
  copied from the 2026-11-08 root and re-hashed, not downloaded.
  `native_source.py extract` applied the unchanged patch `df00a6b0…` and the
  owned sources (`hfs9p.c` `7e6a1182…` and `hfs9p_recovery.inc` `18afeeb9…`
  changed; the other eight files are unchanged). The new inventory is
  `8ad5ca2c…`, replacing `524a414a…`.
- **Two clean builds in one root:** `preflight`, `library9p`,
  `libraryvmmapi`, `bhyve` and `collect`. After the first build, the first
  package, preflight and completion records were sealed and moved aside
  (seal `038d152b…`), and the objects were removed. The second build ran from
  scratch. The comparison found all 83 files equal, and the preflight records
  equal too.
  - The coordinator had asked for a second root, `-r2`. It was not created.
    The preflight record embeds the root path (object prefix, `CFLAGS`
    prefix maps), and `admit.py` requires equal first and second preflight
    hashes. A second root would therefore produce a proof that admission
    refuses. The 2026-11-08 build used the same single-root method.
- **What changed:** against `66b5bb2b`, only `bhyve`, `lib9p.a`,
  `lib9p_pie.a`, `freebsd/build.json` and the three source-bundle records
  changed. `bhyve-slirp-helper`, the `libvmmapi` archives, the 76
  `share/bhyve` files and both FreeBSD notices came out byte-identical.
- **lib9p native tests:** `native-verify.sh` ran on f3 under the same guard
  (stage `lib9p-native-verify`) and passed: production API, backend, ledger
  and supervisor tests.

## What else was rebuilt and what was reused

- **Kernel, rebuilt:** from a fresh `prepare.py` tree, using the cached pinned
  archive `4f3c00db…`. `config`, `abi-negative.json` and the five modules
  (same GNU build IDs) are byte-identical to `66b5bb2b`. `vmlinux`,
  `bzImage`, `System.map`, `build.json`, `abi.json` and `clean-repro.json`
  changed. `Module.symvers` differs only in the CRCs of the eight hfs
  `EXPORT_SYMBOL_GPL` symbols, because g64 and m64 changed their types. No
  module imports them.
- **Daemon, rebuilt:** `daemon.py` from `b869710` with the pinned
  `freebsd-cc` wrapper and sysroot. Two forced builds both gave `34fb05b2…`,
  and the window build reproduced it twice more. `go.mod`, `go.sum`, the
  toolchain, the sysroot capture, the runtime closure and the four Go licence
  files are unchanged. The module cache was seeded from the f64 cache, and
  nothing was downloaded.
- **Window artifacts, rebuilt:** `build-fdescfs-window-artifacts.sh` from
  `b869710`. Both builds were byte-identical.
  - `controlledruntime.test` changed because `controlledruntime` and the
    packages it links changed.
  - `fdescfs-namespace.c` is unchanged. The helper `3d992cdf…` differs from
    the f64 `45f2d59a…` only in DWARF (the new output directory as
    `comp_dir`). After `llvm-objcopy --strip-debug`, both are `07c9f5a1…`,
    the same as the 664 and z54 builds.
- **Reused byte for byte (24 files):** the 22 Linux notices, patch 0002 and
  `series`. `66b5bb2b` first passed `package.py check` and a full `admit.py`
  under the current tooling. Each reused file was checked against its
  manifest, the patches against the repository, and the notices against the
  freshly prepared source. [reused-files.json](reused-files.json) lists them.
  It also lists the 96 rebuilt files that came out byte-identical to
  `66b5bb2b` and the 23 that changed.
- **Not in the package:** `inputs/repository-changes.patch`. The baseline
  `b869710` already contains every source change, so the recorded diff is
  empty.
- **G3 init helper:** kept at `8c02b98f…`; it is not in the package.
  `cmd/homelabfs` reaches `guestbridge` only through `fuseadapter`'s mount
  path, and `node-init` and `volume-init` never mount.

## Reproducibility and tests

The kernel stages ran twice under a user systemd scope (two CPUs, 2 GiB, no
swap, a 20-minute timeout per stage) at the same canonical output path. The
first build tree was moved aside before the repeat. `abi.py collect` ran on
both packages. `compare_kernel.py` passed on the first attempt (13 artifacts
plus the full module ABI), and `check_abi_mismatch.py` refused its negative
controls.

Against the prepared source, three suites passed:
- `mount_hello --sanitize`: 22 cases, including `enrollment-sources-floor`;
- `tag --sanitize`: 4 cases;
- the `controlled_received` `terminal-smoke` scenario.

## Admission and checks

The build and the assembly ran from the y64 worktree at `b869710`. All 4002
tracked files are byte-identical to the main checkout, which is also at
`b869710`, and the main checkout has no untracked files in the build
directories.

`assemble.py` sealed and admitted the package. An independent `admit.py` run
against the pinned manifest passed. `check_candidate.py` refused all 26
real-copy mutations for their expected reasons, including the real CGO0
daemon `480713c2…`. The original and the restored copy passed again.

The tooling now admits only this manifest. The c64+d64 `66b5bb2b…`, 964
`e01fab21…`, z54 `2e0412a6…`, w54 `89c928d5…`, r54 `5eb42c50…`, f44
`b203799e…` and earlier `3f770a88…` are refused as superseded, and
`7e962627…` stays quarantined.

`tools/lab/directlaunch/argv.py` now pins the exporter `ef8f17de…`. The
example spec `internal/runtimeprep/testdata/zq3-spec.json` follows it, because
`TestCheckArgvRecordAgreesWithG4` requires the generated runtime config's
exporter digest to match the argv record. Only `Exporter.SHA256` changed
there. The i64/o64 view bounds (Sources 12 and the zq3 lifetime budgets) are
untouched. The candidate package and pins embed no view bounds: they come
from the run spec at fill time.

**What the next run must do again:**
- The daemon bytes are new, so the f3 observer qualification does **not**
  carry over. `34fb05b2…` needs `--check-process-observer` under the bounded
  host guard on f3.
- The exporter `ef8f17de…` has never run. The filled zq3-r8 spec must pin it,
  replacing `36423b6d…`.
- Booting this kernel, checking SELinux enforcing, and mounting through it
  remain zq3 steps that need explicit approval.
