# xz3 b94 rebuild (2026-11-17): zq3-r10 allocation-fence, unmount-stop and exited-cleanup fixes

**Superseded on 2026-11-18** by the [5a4 rebuild](../xz3-20261118-r12/README.md)
(`150deb29…`, source `1b0a306`) for zq3-r12. Its guest kernel `c4e08f6f…`
waits in `hfs_view_enter` without a time bound, so a recall wedged in
WAIT_DRAINED kept m1's stat and `umount` in D state (zq3-r11 R11-2, fixed by
1a4). Its daemon `bf86d805…` (which passed the f3 observer check in zq3-r11)
answers an early DRAINED STALE, so the guest stopped its view worker and
blocked the session and T1's OPEN timed out (R11-1, fixed by y94). Its
native exporter `ef8f17de…`, the five modules and the native, notice and
unchanged patch files are reused byte for byte. The tooling refuses this
manifest as superseded. It still passes `package.py check` and `admit.py`
under the current tooling.

This candidate supersedes the [r84 rebuild](../xz3-20261114-r10/README.md)
(`4712e479…`, source `cc6beed`). After zq3-r10, fixes were merged in the
guest kernel and in the CGO daemon, so both were rebuilt (task b94, the
prerequisite for zq3-r11):

- **Guest kernel:** x84+z84 changed `tools/integration/linux9p` patches
  0003, 0004 and 0005 and `source.json`.
  - x84 (zq3-r10 R10-1, guest side): a request refused only because another
    allocation of the session is pending now waits (FIFO, bounded) for the
    allocation fence to clear instead of failing `-EAGAIN`, so a release
    that `do_exit` runs after PROCESS_EXIT no longer blocks the session.
  - z84 (R10-2): the unmount stop check runs after `kill_anon_super`, the
    control flush and a bounded settle (`hfs_mmap_settle`), and accepts a
    once-reopened LIVE view with closed admission and no owner
    (`hfs_controlled_view_idle`).
- **CGO daemon:** 194 (R10-1, host side) changed `internal/guestbridge`
  `controlled_protocol.go` and `service.go`. ALLOCATE now tolerates an
  issuer whose PROCESS_EXIT already completed, exactly for the CALL path's
  original-cleanup shapes (DESCRIPTION_RELEASE of a live DESCRIPTION grant,
  FORGET count 1 of a live PATH grant).

The y64 method was used for the kernel (a fresh prepared source, two builds
at the same O path), the l84 method for the daemon and the window artifacts,
exactly as r84 did. The native exporter was reused. **Build and offline
admission only:** nothing ran on f3, Rocky or any VM. Nothing was installed
or booted, so `installed`, `booted` and `native_accepted` stay false, and
publication stays null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261117-r11/package` |
| Manifest SHA256 | `3b128cc68e4a4e6c43c8411437df5b7671c003a5f9e7ae6c11700f918440a47a` (194 files) |
| Source checkout | `dfdcdd9635b9bae91b9d074eb86227ca85395b75` (also the repository baseline and the daemon's build revision) |
| Kernel | `6.1.190-hfs-xz3`: `linux/bzImage` `5b08b3d2cb7e7a23d6bb9c0e9f79ab45fcdc3110e0d6d7e19ed2e2287ac8aa3b`, `linux/vmlinux` `6e489f3860c24d004c2df5f0fdd6e8623d7541ed0b36af15b24c42c14e5da282` (GNU build ID `c4e08f6f57c647331cd42541106dcbea9a7b9490`), `linux/config` `28fe4a8e…` (unchanged), `System.map` `341071f62c4173bcf7dc121eef206812371be8cd4b5782b2ef85ead6122a4882` |
| Modules | the five modules are byte-identical to `4712e479` (same GNU build IDs; `virtio_net` `07299518…`) |
| CGO daemon | `bf86d805732cc79a94f8d5023729396d48343d9dd7102be43598ee8992be232c` (10,965,672 bytes, GNU build ID `d3af6b7c1a38485690e3403dbe09d44ae5a0ea7a`) |
| Native exporter | `freebsd/bhyve` `ef8f17de8754f8bdd6d5d517f15a859e87a2f4a8ca1542394f5608a8e40823b1` (y64 native f3 build, unchanged) |
| Window test binary / helper | `8abf53bc…` / `62d0d3fa…` (from `dfdcdd9`) |

[Evidence](evidence.json) records hashes, stage timings, receipts and control
names. [reused-files.json](reused-files.json) lists every reused file, the
rebuilt files that came out identical, and the 19 that changed.
[kernel-clean-repro.json](kernel-clean-repro.json) holds the
`compare_kernel.py` record (also packaged as `linux/clean-repro.json`
`ef7014d1…`), the stage table and the ABI negative-control receipt.

## What was rebuilt and what was reused

- **Kernel, rebuilt:** from a fresh `prepare.py` tree, using the cached pinned
  archive `4f3c00db…` (nothing downloaded). The new source inventory is
  `9ec64777…`.
  - `config`, `abi-negative.json`, `kernel.release`, `modules.*` and the five
    modules (same GNU build IDs) are byte-identical to `4712e479`.
  - `vmlinux`, `bzImage`, `System.map`, `build.json`, `abi.json` and
    `clean-repro.json` changed.
  - `Module.symvers` differs only in the CRCs of the eight hfs
    `EXPORT_SYMBOL_GPL` symbols (`hfs_call`, `hfs_control`,
    `hfs_request_allocate`, `hfs_request_resolve_gap`, `hfs_retire`,
    `hfs_session_destroy`, `v9fs_hfs_register_provider`,
    `v9fs_hfs_unregister_provider`), because x84 and z84 changed the types
    they reach. No module imports them.
- **Daemon, rebuilt:** `daemon.py` from `dfdcdd9` with the pinned
  `freebsd-cc` wrapper and sysroot. A forced build into the package and a
  second forced build into a separate package both gave `bf86d805…`. The
  window build reproduced it twice more from a `git archive` export.
  - Against r84, the source closure (1295 files) changed in exactly two
    files, `guestbridge/controlled_protocol.go` and `guestbridge/service.go`;
    nothing was added or removed.
  - `go.mod`, `go.sum`, the Go and cgo toolchain, the sysroot capture, the
    runtime closure and the four Go licence files are unchanged.
  - The module cache was seeded from the r84 cache, and nothing was
    downloaded.
- **Window artifacts, rebuilt:** `build-fdescfs-window-artifacts.sh` from
  `dfdcdd9`. Both builds were byte-identical.
  - `controlledruntime.test` changed because `controlledruntime` links
    `guestbridge`, which 194 changed.
  - `fdescfs-namespace.c` is unchanged. The helper `62d0d3fa…` differs from
    the r84 `5c709a35…` only in DWARF (the new output directory as
    `comp_dir`). After `llvm-objcopy --strip-debug`, both are `07c9f5a1…`.
- **Native exporter, reused byte for byte:**
  `git diff cc6beed dfdcdd9 -- tools/integration/lib9p` is empty.
  `stage-package.py` re-hashed all 11 packaged owned inputs against the
  repository. The three native proof records of the y64 f3 build were copied
  from the r84 run root and checked against the y64 originals.
- **113 files reused** from `4712e479`, each checked against its manifest:
  - 83 native FreeBSD files;
  - 5 native source-bundle and notice files;
  - patches 0001 and 0002 with `series`;
  - 22 Linux notices.

  25 of them were also checked against their current source: the
  repository patches, and the notices in the freshly prepared Linux tree.
  Before the reuse, `4712e479` passed `package.py check` and a full
  `admit.py` under the `dfdcdd9` tooling.
- **What changed in the manifest (19 files):**
  - the kernel files above;
  - the daemon, its build and preparation records, and
    `evidence/daemon-clean-repro.json`;
  - patches 0003, 0004 and 0005, `inputs/linux-source-manifest.json` and
    `inputs/linux-source-inventory.json`;
  - `inputs/repository-source.json` and `installation-proposal.json` (it
    pins the kernel and the daemon);
  - the packaged copy of `tools/integration/artifacts/README.md`
    (`inputs/admission-recipes/README.md`).
- **Not in the package:** `inputs/repository-changes.patch`. The baseline
  `dfdcdd9` already contains every source change, so the recorded diff is
  empty.
- **G3 init helper:** kept at `8c02b98f…`; it is not in the package.
  `cmd/homelabfs` reaches `guestbridge` only through `fuseadapter`'s mount
  path, and `node-init` and `volume-init` never mount. 194 does change
  `guestbridge` runtime code, but only the controlled-protocol ALLOCATE and
  CALL credential tolerance, which only a mounted controlled session
  reaches.

The build and the assembly ran from the b94 worktree at `dfdcdd9`, which had
no edits during the daemon build and the assembly (the pin edits were set
aside and re-applied afterwards). The daemon builder bindings name that
worktree path. Every other binding is a hash.

## Reproducibility and tests

The kernel stages ran twice under a user systemd scope (two CPUs, 2 GiB, no
swap, a 20-minute timeout per stage) at the same canonical output path. The
first build tree was moved aside before the repeat. `vmlinux` took 741 s and
721 s. `abi.py collect` ran on both packages. `compare_kernel.py` passed on
the first attempt (13 artifacts plus the full module ABI), and
`check_abi_mismatch.py` refused its four negative controls.

Against the prepared source, these suites passed:
- `mount_hello --sanitize`: 23 cases (z84 adds the
  `hfs_controlled_view_idle` copy), including `enrollment-sources-floor`;
- `tag --sanitize`: 4 cases;
- the `controlled_received` scenarios `terminal-smoke`, `cleanup` and
  `exit-cleanup` (the x84 fence wait and the 194 exited-issuer cleanup;
  `run.py` gives the last two a 900 s Go test timeout);
- `view` and `view --pressure`, each plain and with `--sanitize`.

## Admission and checks

`assemble.py` sealed and admitted the package. An independent `admit.py` run
against the pinned manifest passed. `check_candidate.py` refused all 26
real-copy mutations for their expected reasons, including the real CGO0
daemon `480713c2…`. The original and the restored copy passed again.

The tooling now admits only this manifest:
- refused as superseded: the r84 `4712e479…`, the l84 `e585206a…`, the y64
  `867ede36…`, the c64+d64 `66b5bb2b…`, 964 `e01fab21…`, z54 `2e0412a6…`,
  w54 `89c928d5…`, r54 `5eb42c50…`, f44 `b203799e…` and the earlier
  `3f770a88…`;
- still quarantined: `7e962627…`.

`tools/lab/directlaunch/argv.py` and the example spec
`internal/runtimeprep/testdata/zq3-spec.json` are unchanged, because the
exporter `ef8f17de…` is unchanged.

**What the next run must do again:**
- The daemon bytes are new, so the f3 observer qualification of the r84
  daemon (`34a5360b…`, passed in zq3-r10) does **not** carry over.
  `bf86d805…` needs `--check-process-observer` under the bounded host guard
  on f3.
- The guest kernel is new: the Rocky kernel plan must be regenerated for
  this manifest. It names build ID `c4e08f6f…` and the new `bzImage` and
  `System.map` hashes; `config`, `modules.*` and the five modules keep
  their hashes.
- The window artifacts are new: the fdescfs window pins name `8abf53bc…`
  and `62d0d3fa…`.
- y84 changed the lab tools `tools/lab/retained9p/guest.py` and
  `tools/lab/guest-lock-probe.py`. They are not package contents, but the
  zq3-r11 fill must re-pin them.
- Booting the kernel, checking SELinux enforcing, and mounting through it
  remain zq3 steps that need explicit approval.
