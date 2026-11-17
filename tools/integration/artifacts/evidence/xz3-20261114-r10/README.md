# xz3 r84 rebuild (2026-11-14): zq3-r9 kernel pressure and daemon QUERY fixes

**Superseded on 2026-11-17** by the [b94 rebuild](../xz3-20261117-r11/README.md)
(`3b128cc6…`, source `dfdcdd9`) for zq3-r11. Its guest kernel `d73c85da…`
fails a release with `-EAGAIN` while another allocation of the session is
pending and then blocks the session (zq3-r10 R10-1, fixed by x84), and its
unmount stop check refuses a once-reopened LIVE view (R10-2, fixed by z84).
Its daemon `34a5360b…` (which passed the f3 observer check in zq3-r10)
refuses ALLOCATE of an exited issuer's exact original cleanup (R10-1 host
side, fixed by 194). Its native exporter `ef8f17de…`, the five modules and
the native, notice and unchanged patch files are reused byte for byte. The
tooling refuses this manifest as superseded. It still passes
`package.py check` and `admit.py` under the current tooling.

This candidate supersedes the [l84 rebuild](../xz3-20261114-r9/README.md)
(`e585206a…`, source `e92eaa1`). After zq3-r9, fixes were merged in the
guest kernel and in the CGO daemon, so both were rebuilt (task r84, the
prerequisite for zq3-r10):

- **Guest kernel:** p84 changed `tools/integration/linux9p` patch 0004
  (`fs/9p/hfs_mmap.c`, `fs/9p/hfs_view.h`) and `source.json`. Memory
  pressure never releases the lease of an observation in flight. The
  background pressure pass holds while observations are in flight, bounded
  to 5 s.
- **CGO daemon:**
  - o84: a QUERY may resume any never-prepared claim (zq3-r9 R9-1).
    `controlledvalidation/query_continuation.go` replaces
    `cleanup_continuation.go`.
  - q84: a shared DOMAIN lease is released per holder, and only the last
    holder's release recalls (`viewcontrol/lease_holders.go`).
  - i84: one unexported clock hook in `controlledruntime/guest_observer.go`,
    used only by tests.

The y64 method was used for the kernel (a fresh prepared source, two builds
at the same O path), the l84 method for the daemon and the window artifacts.
The native exporter was reused. **Build and offline admission only:**
nothing ran on f3, Rocky or any VM. Nothing was installed or booted, so
`installed`, `booted` and `native_accepted` stay false, and publication
stays null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261114-r10/package` |
| Manifest SHA256 | `4712e4793e67007a65b293dd62dceb16d16a2f2322705f7f0a5f81c261442cf0` (194 files) |
| Source checkout | `cc6beedd74c18df7eefbbcc189934035b8267e91` (also the repository baseline and the daemon's build revision) |
| Kernel | `6.1.190-hfs-xz3`: `linux/bzImage` `10f2da29f3a1517c186c339d9defcd6c3ffdee4eeaf9ab00cf7dbf3283f445af`, `linux/vmlinux` `e26684e970a017eb3991f0a8797612edd636a2eb01efef4cc6991dd212d70706` (GNU build ID `d73c85da5f0667e9dc6e4abf6b73e4c84c03b9a2`), `linux/config` `28fe4a8e…` (unchanged), `System.map` `a720bd4abce33f1ebb2216dd0afa06b68bfb125514b836d3c647a67018afaa62` |
| Modules | the five modules are byte-identical to `e585206a` (same GNU build IDs; `virtio_net` `07299518…`) |
| CGO daemon | `34a5360bcb43ff7516062c096ab247b122febe6653d9510277730e968cae5230` (10,963,936 bytes, GNU build ID `b7679dc700e454c7db0bbc73a08d575ac08efff4`) |
| Native exporter | `freebsd/bhyve` `ef8f17de8754f8bdd6d5d517f15a859e87a2f4a8ca1542394f5608a8e40823b1` (y64 native f3 build, unchanged) |
| Window test binary / helper | `72fffd3a…` / `5c709a35…` (from `cc6beed`) |

[Evidence](evidence.json) records hashes, stage timings, receipts and control
names. [reused-files.json](reused-files.json) lists every reused file, the
rebuilt files that came out identical, and the 17 that changed.
[kernel-clean-repro.json](kernel-clean-repro.json) holds the
`compare_kernel.py` record (also packaged as `linux/clean-repro.json`
`ce1b1040…`), the stage table and the ABI negative-control receipt.

## What was rebuilt and what was reused

- **Kernel, rebuilt:** from a fresh `prepare.py` tree, using the cached pinned
  archive `4f3c00db…` (nothing downloaded). The new source inventory is
  `567adc91…`.
  - `config`, `abi-negative.json`, `kernel.release`, `modules.*` and the five
    modules (same GNU build IDs) are byte-identical to `e585206a`.
  - `vmlinux`, `bzImage`, `System.map`, `build.json`, `abi.json` and
    `clean-repro.json` changed.
  - `Module.symvers` differs only in the CRCs of the eight hfs
    `EXPORT_SYMBOL_GPL` symbols, because p84 changed the types they reach
    through `hfs_view.h`. No module imports them.
- **Daemon, rebuilt:** `daemon.py` from `cc6beed` with the pinned `freebsd-cc`
  wrapper and sysroot. A forced build into the package and a second forced
  build into a separate package both gave `34a5360b…`. The window build
  reproduced it twice more from a `git archive` export.
  - Against l84, the source closure (1295 files) changed in six files
    (`controlledruntime` `attestor.go` (comments only), `facts.go` and
    `guest_observer.go`; `controlledvalidation/execution.go`;
    `viewcontrol` `controller.go` and `tickets.go`). It gained
    `controlledvalidation/query_continuation.go` and
    `viewcontrol/lease_holders.go`, and lost
    `controlledvalidation/cleanup_continuation.go`.
  - `go.mod`, `go.sum`, the Go and cgo toolchain, the sysroot capture, the
    runtime closure and the four Go licence files are unchanged.
  - The module cache was seeded from the l84 cache, and nothing was
    downloaded.
- **Window artifacts, rebuilt:** `build-fdescfs-window-artifacts.sh` from
  `cc6beed`. Both builds were byte-identical.
  - `controlledruntime.test` changed because `controlledruntime` and the
    packages it links changed.
  - `fdescfs-namespace.c` is unchanged. The helper `5c709a35…` differs from
    the l84 `50d823be…` only in DWARF (the new output directory as
    `comp_dir`). After `llvm-objcopy --strip-debug`, both are `07c9f5a1…`.
- **Native exporter, reused byte for byte:**
  `git diff e92eaa1 cc6beed -- tools/integration/lib9p` is empty.
  `stage-package.py` re-hashed all 11 packaged owned inputs against the
  repository. The three native proof records of the y64 f3 build were copied
  from the l84 run root and checked against the y64 originals.
- **115 files reused** from `e585206a`, each checked against its manifest:
  - 83 native FreeBSD files;
  - 5 native source-bundle and notice files;
  - patches 0001, 0002, 0003 and 0005 with `series`;
  - 22 Linux notices.

  27 of them were also checked against their current source: the
  repository patches, and the notices in the freshly prepared Linux tree.
  Before the reuse, `e585206a` passed `package.py check` and a full
  `admit.py` under the `cc6beed` tooling.
- **What changed in the manifest (17 files):**
  - the kernel files above;
  - the daemon and its build and preparation records, and
    `daemon-clean-repro.json`;
  - patch 0004, `inputs/linux-source-manifest.json` and
    `inputs/linux-source-inventory.json`;
  - `inputs/repository-source.json` and `installation-proposal.json` (it
    pins the kernel and the daemon);
  - the packaged copy of `tools/integration/artifacts/README.md`.
- **Not in the package:** `inputs/repository-changes.patch`. The baseline
  `cc6beed` already contains every source change, so the recorded diff is
  empty.
- **G3 init helper:** kept at `8c02b98f…`; it is not in the package.
  `cmd/homelabfs` reaches `guestbridge` only through `fuseadapter`'s mount
  path, and o84, q84 and i84 do not touch `guestbridge` runtime code.

The build and the assembly ran from the r84 worktree at `cc6beed`, which had
no edits before assembly. The daemon builder bindings name that worktree
path. Every other binding is a hash.

## Reproducibility and tests

The kernel stages ran twice under a user systemd scope (two CPUs, 2 GiB, no
swap, a 20-minute timeout per stage) at the same canonical output path. The
first build tree was moved aside before the repeat. `vmlinux` took 651 s and
663 s. `abi.py collect` ran on both packages. `compare_kernel.py` passed on
the first attempt (13 artifacts plus the full module ABI), and
`check_abi_mismatch.py` refused its four negative controls.

Against the prepared source, these suites passed:
- `mount_hello --sanitize`: 22 cases, including `enrollment-sources-floor`;
- `tag --sanitize`: 4 cases;
- the `controlled_received` `terminal-smoke` scenario;
- `view` and `view --pressure`, each plain and with `--sanitize`. The
  pressure run reports the p84 checks: an in-flight observation lease is
  never released, and the background pass is held (bounded) while observing.

## Admission and checks

`assemble.py` sealed and admitted the package. An independent `admit.py` run
against the pinned manifest passed. `check_candidate.py` refused all 26
real-copy mutations for their expected reasons, including the real CGO0
daemon `480713c2…`. The original and the restored copy passed again.

The tooling now admits only this manifest:
- refused as superseded: the l84 `e585206a…`, the y64 `867ede36…`, the
  c64+d64 `66b5bb2b…`, 964 `e01fab21…`, z54 `2e0412a6…`, w54 `89c928d5…`,
  r54 `5eb42c50…`, f44 `b203799e…` and the earlier `3f770a88…`;
- still quarantined: `7e962627…`.

`tools/lab/directlaunch/argv.py` and the example spec
`internal/runtimeprep/testdata/zq3-spec.json` are unchanged, because the
exporter `ef8f17de…` is unchanged.

**What the next run must do again:**
- The daemon bytes are new, so the f3 observer qualification of the l84
  daemon does **not** carry over. `34a5360b…` needs
  `--check-process-observer` under the bounded host guard on f3.
- The guest kernel is new: the Rocky kernel plan must be regenerated for
  this manifest. It names build ID `d73c85da…` and the new `bzImage` and
  `System.map` hashes; `config`, `modules.*` and the five modules keep
  their hashes.
- The window artifacts are new: the fdescfs window pins name `72fffd3a…`
  and `5c709a35…`.
- Booting the kernel, checking SELinux enforcing, and mounting through it
  remain zq3 steps that need explicit approval.
