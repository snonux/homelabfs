# xz3 c64+d64 rebuild (2026-11-10): SELinux LSM guest kernel, per-domain fence daemon

This candidate supersedes the [964 kernel rebuild](../xz3-20261110f/README.md)
(`e01fab21…`, source `06c27a8`) for two reasons, so both the guest kernel and
the CGO daemon were rebuilt:

- **c64 (merge `dbce45c`):** every candidate up to `e01fab21` had
  `CONFIG_SECURITY_SELINUX=y` but a `CONFIG_LSM` without `selinux`, so the
  guest booted without SELinux. `kernel.config` now sets
  `CONFIG_LSM="lockdown,yama,integrity,selinux,bpf,landlock"` plus explicit
  SELinux options, and `assert_config` refuses a config without `selinux` in
  `CONFIG_LSM`. Under the current tooling `e01fab21` itself no longer admits:
  `package.py check` still passes on it, but `admit.py` refuses it with
  `CONFIG_LSM must enable selinux`.
- **d64 (merge `060dea0`):** in zq3-r6 the daemon kept one view fence for all
  domains while the guest keeps one per domain (R6-1), and it refused an
  unissued guest cell that arrived out of order (R6-2). d64 keeps the fence
  per domain and admits such a cell once inside a bounded 16-cell window, in
  `internal/viewcontrol`, `internal/guestbridge` and
  `internal/controlledruntime`.

The a64 kernel method (y54) and the 664 daemon and window method (f44) were
combined. **Build and offline admission only:** nothing was installed or
booted, and `installed`, `booted` and `native_accepted` stay false with
publication null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261110g/package` |
| Manifest SHA256 | `66b5bb2b6c1dd1a663f26c50d0d9b3bfeebd620127c4a5859ca146074f2c2e2d` (194 files) |
| Source checkout | `060dea03bb6fffad2353dfa70461dd860e5b1db4` (also the repository baseline, the daemon's build revision) |
| Kernel | `6.1.190-hfs-xz3`: `linux/bzImage` `6f59b593…`, `linux/vmlinux` `cef27ad7…` (GNU build ID `2ce818d94ce5990da73dec32a1f60190c24107e7`), `linux/config` `28fe4a8e…`, `System.map` `3d11cf22…` |
| Resolved `CONFIG_LSM` | `"lockdown,yama,integrity,selinux,bpf,landlock"` (the only line that differs from the 964 config `d88b4c56…`) |
| CGO daemon | `9b7eed8dc9b98f4a61af000bb17b5008672a7083075e206951fc96b53577088b` (10,781,432 bytes, GNU build ID `17d8621d…`) |
| Window test binary / helper | `56e87332…` / `45f2d59a…` (from `060dea0`) |

No initramfs is part of the package; the recipe builds `bzImage` and modules.
[Evidence](evidence.json) records hashes, stage timings, receipts and control
names.

## What was rebuilt and what was reused

- **Kernel, rebuilt:** from a fresh `prepare.py` source tree (the cached
  pinned archive `4f3c00db…`, nothing downloaded). Its source inventory
  `f9e788b0…` is identical to a64's, because the 964 series and `source.json`
  did not change. `config`, `vmlinux`, `bzImage`, `System.map`, `build.json`,
  `abi.json` and `clean-repro.json` changed. The resolved config is the
  `28fe4a8e…` of the c64 config-only run. **The five modules (same GNU build
  IDs), `Module.symvers`, `modules.*`, `kernel.release` and
  `abi-negative.json` came out byte-identical to a64**, as c64 predicted:
  only built-in `security/security.c` reads `CONFIG_LSM`.
- **Daemon, rebuilt:** with `daemon.py` from `060dea0`, using the pinned
  `freebsd-cc` wrapper and sysroot capture. One forced build went into the
  package and a second into a separate package. Both gave `9b7eed8d…`, and the
  window build reproduced it twice more from a `git archive` export. Against
  z54 the source closure changed in `controlledruntime/{facts,restart}.go`,
  `guestbridge/{endpoint,mapping,restore,transport_ledger}.go` and
  `viewcontrol/{controller,recall,tickets}.go`, and gained
  `viewcontrol/fence.go`. The Go and cgo toolchain, sysroot capture, runtime
  closure, `go.mod`, `go.sum` and the four Go licence files are unchanged. The
  private module cache was seeded from the z54 cache, and nothing was
  downloaded.
- **Window artifacts, rebuilt:** with `build-fdescfs-window-artifacts.sh`
  from `060dea0`. Both builds were byte-identical. `controlledruntime.test`
  changed because `controlledruntime` and the d64 packages it links changed.
  `fdescfs-namespace.c` is unchanged. The helper differs from z54's
  `cc92bcde…` only in DWARF (the new output directory as `comp_dir`). After
  `llvm-objcopy --strip-debug` it equals the 664 and a64 builds (`07c9f5a1…`).
- **Reused byte for byte (118 files):** the 83 native FreeBSD files, five
  native source-bundle and notice files, the two Linux source records,
  patches 0001–0005 with `series`, and 22 Linux notices. Each file was checked
  against the e01fab21 manifest (whose container check passed first). The
  patches and `source.json` were also checked against the repository, and the
  source inventory and notices against the freshly prepared source.
  [reused-files.json](reused-files.json) lists them, and also lists the
  rebuilt files that came out identical (kernel modules and records, Go
  licences).
- **Dropped:** `inputs/repository-changes.patch`. The baseline is now
  `060dea0`, which already contains the 964 `source.json` and patches and the
  c64 `kernel.config`, so the diff that `assemble.py` records is empty.
- **G3 init helper:** kept at `8c02b98f…`. It is not in the package.
  `cmd/homelabfs` imports `guestbridge` but not `viewcontrol`, and the d64
  guestbridge changes are guest-transport paths that `node-init` and
  `volume-init` never reach.

## Reproducibility

`kernel.py` ran its four stages twice, each under a user systemd scope (two
CPUs, 2 GiB, no swap) with a 20-minute timeout, at the **same** canonical
output path `…/hfs-xz3-20261110g/kernel-build`. The first build tree was moved
to `kernel-build-first` before the repeat. `abi.py collect` ran on both
packages before `compare_kernel.py`, which passed on the first attempt:
identical compile inputs, all 13 artifacts and the full module ABI.
`check_abi_mismatch.py` refused its four negative controls.

Against the prepared source, three suites passed:
- `tests/mount_hello/run.py --sanitize`: all five cases;
- `tests/tag/run.py --sanitize`: all four 964 fixed-tag cases;
- the `controlled_received` `terminal-smoke` scenario
  (`TestControlledReceivedActualTerminalClean`).

## Admission and checks

The worker sandbox refuses git in the main checkout, so the builds and the
assembly ran from the worker worktree at `060dea0`. All 3969 tracked files are
byte-identical to the main checkout, which is also at `060dea0`. The main
checkout has no untracked files under `cmd`, `internal`, `pkg`,
`tools/integration` or `tools/lab`. Unlike a64, nothing was re-keyed: the
daemon was built in this worktree, so the new daemon builder bindings name the
real build path. The three native proof records are byte copies.

`assemble.py` sealed and admitted the package. An independent `admit.py` run
against the pinned manifest passed. `check_candidate.py` refused all 26
real-copy mutations for their expected reasons, including the real CGO0
daemon `480713c2…`. The original and the restored copy passed again.

The tooling now admits only this manifest. The 964 `e01fab21…`, z54
`2e0412a6…`, w54 `89c928d5…`, r54 `5eb42c50…`, f44 `b203799e…` and earlier
`3f770a88…` are refused as superseded, and `7e962627…` stays quarantined.

**What the next run must do again:** the f3 observer qualification does
**not** carry over, because the daemon bytes are new. They still need
`--check-process-observer` under the bounded host guard on f3. The guest
should now boot with SELinux initialised and Rocky's policy enforcing. That is
expected but not verified, so Phase 9's `getenforce` = `Enforcing` (and a
dmesg `SELinux:  Initializing` line) is a real check again. Booting this
kernel and mounting through it remain zq3 steps that need explicit approval.
