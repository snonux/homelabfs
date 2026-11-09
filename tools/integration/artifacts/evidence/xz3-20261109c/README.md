# xz3 r54 kernel rebuild (2026-11-09): mount HELLO after enrollment

This candidate supersedes the [f44 candidate](../xz3-20261108b/README.md)
(`b203799e…`, source `defd974`) only because its guest kernel predates r54.
That kernel's `v9fs_hfs_mount` built the HELLO before enrollment, so every
hfs1.4 mount returned EPROTO in lab run zq3-r2 (R2-4). Task r54 (`5e51fe1`)
fixed `0003-hfs1-guest-vfs.patch` (now `e287211a…`, 165910 bytes). **Build and
offline admission only:** nothing was installed or booted, and `installed`,
`booted` and `native_accepted` stay false with publication null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261109c/package` |
| Manifest SHA256 | `5eb42c5066792b55e439373db48e8e8e88532efc61ff7b82f64db22a87798f99` (195 files) |
| Source checkout | `ca920a2ab7346cbcacb4e7bcc9148e213edd7fc4` (repository baseline stays `defd974`, the daemon's build revision) |
| Kernel | `6.1.190-hfs-xz3`: `linux/bzImage` `98cc02d5…`, `linux/vmlinux` `59357102…` (GNU build ID `1f7808b8…`), `linux/config` `d88b4c56…` (unchanged), `System.map` `69b6c7bc…` |
| CGO daemon | `174949c1ebf97d483d49e41f1ade7bce88f63013cb943be794276bb1ddd417a7` (f44, unchanged) |
| Window test binary / helper | `9e046c20…` / `a773f3ef…` (f44, from `defd974`, unchanged) |

No initramfs is part of the package; the recipe builds `bzImage` and modules.
[Evidence](evidence.json) records hashes, stage timings and control names.

## What was rebuilt and what was reused

- **Rebuilt:** the full Linux kernel, from a fresh `prepare.py` source tree
  (the cached pinned archive `4f3c00db…`, nothing downloaded; source inventory
  `1a7dc675…`) with the r54 series. `vmlinux`, `bzImage`, `System.map`,
  `build.json`, `abi.json` and `clean-repro.json` changed. The config, the
  five modules, `Module.symvers`, `modules.*`, `kernel.release` and
  `abi-negative.json` came out byte-identical to f44; the r54 change is in
  built-in 9p code and does not alter the exported symbol CRCs.
- **New Linux source records:** `inputs/linux-source-manifest.json` (the r54
  `source.json`), `inputs/linux-source-inventory.json`, the new 0003 patch and
  `inputs/repository-changes.patch` (the `source.json` diff against the
  `defd974` baseline that `assemble.py` records).
- **Reused byte for byte (122 files):** the 83 native FreeBSD files, five
  native source-bundle and notice files, seven daemon files (binary, build
  record, preparation record, Go licenses), 22 Linux license notices and the
  unchanged patches 0001, 0002, 0004, 0005 and `series`. Each was checked
  against the f44 manifest, and the Linux notices and patches also against
  the current source tree and repository. [reused-files.json](reused-files.json)
  lists them. `git diff defd974 HEAD -- internal cmd tools/integration/lib9p`
  touches only `internal/guestbridge/README.md` and a runtimeprep test file;
  `go.mod` only drops bbolt's `// indirect` marker, which Go build info does
  not record.
- **Window artifacts and G3 init helper:** not in the package and unchanged.
  `internal/controlledruntime` and `cmd/` have no changes since `defd974`, so
  the f44 window bytes still match the daemon source. The f44 window build
  (`window-build/run-{1,2}`) still hashes to `9e046c20…`, `a773f3ef…` and
  `174949c1…`. The init helper stays `8c02b98f…` for the reason recorded in
  the f44 evidence.

## Reproducibility

`kernel.py` ran its four stages twice, each under a user systemd scope (two
CPUs, 2 GiB, no swap) with a 20-minute timeout, at the **same** canonical
output path `…/hfs-xz3-20261109c/kernel-build`. The output path is a recorded
compile input, so the first build tree was moved to `kernel-build-first`
before the repeat. `abi.py collect` ran on both packages before
`compare_kernel.py`, which passed on the first attempt: identical compile
inputs, all 13 artifacts and the full module ABI. `check_abi_mismatch.py`
refused its four negative controls. The host's elfutils moved from 0.196 to
0.197 since f44, so `inputs/toolchains.json` records the new `libelf` and
`libdebuginfod` loader dependencies; both builds used the same toolchain.

`tests/mount_hello/run.py --sanitize` against the prepared source passed all
five cases (16383 and 32767 mount, both wrong echoes and the unselected mask
refuse).

## Admission and checks

`assemble.py` sealed and admitted the package. It ran from the main checkout
(`/home/paul/git/homelabfs`, at `ca920a2`, byte-identical to this branch for
`tools/integration`). The retained daemon builder bindings name that path,
because the reused daemon was built there. An independent `admit.py` run
against the pinned manifest passed. `check_candidate.py` refused all 26
real-copy mutations for their expected reasons, including the real CGO0
daemon `480713c2…`, and the original and restored copies passed again.

The tooling now admits only this manifest. The f44 manifest `b203799e…` and
the earlier `3f770a88…` are refused as superseded, and `7e962627…` stays
quarantined. The f3 observer qualification of the daemon bytes carries over
from f44 because the bytes are identical. Booting this kernel and mounting
through it remain zq3 steps that need explicit approval.
