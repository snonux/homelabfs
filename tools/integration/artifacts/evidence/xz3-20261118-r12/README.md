# xz3 5a4 rebuild (2026-11-18): zq3-r11 bounded view wait and early-DRAINED fixes

**Superseded on 2026-11-18** by the [xc4 rebuild](../xz3-20261118-r13/README.md)
(`e9c77e5b…`, source `b7d107b`) for zq3-r13. Its daemon `e0981a0a…` (which
passed the f3 observer check in zq3-r12) fences the whole guest transport
when a mapped cell is retried, instead of reusing the base it recorded for
that cell, and the fence names no reason (zq3-r12 R12-1, fixed by nc4). Its
1a4 guest kernel `8c1de1ed…`, its native exporter `ef8f17de…` and every
other non-daemon file are reused byte for byte. The tooling refuses this
manifest as superseded. It still passes `package.py check` and `admit.py`
under the current tooling.

This candidate supersedes the [b94 rebuild](../xz3-20261117-r11/README.md)
(`3b128cc6…`, source `dfdcdd9`). After zq3-r11, fixes were merged in the
guest kernel and in the CGO daemon, so both were rebuilt (task 5a4, the
prerequisite for zq3-r12):

- **Guest kernel:** 1a4 changed `tools/integration/linux9p` patches 0003 and
  0004 and `source.json` (zq3-r11 R11-2, guest side).
  - `hfs_view_enter` waits at most `HFS_VIEW_ENTER_WAIT_MS` (30 s) and then
    returns `-ETIMEDOUT`. Before, a recall that never left WAIT_DRAINED kept
    m1's stat and `umount` in D state.
  - An mmap fault that times out gets SIGBUS. So does a fault after a final
    refusal (view FENCED, session closing or blocked, fatal signal), which
    used to retry forever.
  - When the view worker stops, the guest logs one line per session naming
    the session, the worker step, the host event and ACK phase, the host
    status, the view phase and the recall.
  - An initial attribute install of a committed CREATE, MKDIR or SYMLINK
    that times out stays terminal-unadopted and logs a ratelimited warning.
- **CGO daemon:** y94 (zq3-r11 R11-1/R11-2, host side) changed
  `internal/viewcontrol` `ack_reply.go` and `recall.go`.
  - A guest sends DRAINED as soon as its own INSTALLED_DRAIN is accepted,
    even while an all-domain recall still waits for a peer in INSTALL_DRAIN.
  - The daemon used to answer that early DRAINED STALE. The guest then
    stopped its view worker and blocked the session, and the recall waited
    in WAIT_DRAINED forever.
  - Now the daemon answers BLOCKED, if the guest's own INSTALLED_DRAIN of
    that recall and sequence is in the protected ACK archive. The BLOCKED
    bound runs from the latest first BLOCKED of a peer that still owes
    INSTALLED_DRAIN.

The y64 method was used for the kernel (a fresh prepared source, two builds
at the same O path), the l84 method for the daemon and the window artifacts,
exactly as b94 did. The native exporter was reused. **Build and offline
admission only:** nothing ran on f3, Rocky or any VM. Nothing was installed
or booted, so `installed`, `booted` and `native_accepted` stay false, and
publication stays null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261118-r12/package` |
| Manifest SHA256 | `150deb2993cda12cc2ad6cee2f118fe0f2e334b0ddb9afaf09a8e22f27b0b88a` (194 files) |
| Source checkout | `1b0a306bde2831d487f287af5fde82e80f7cedc7` (also the repository baseline and the daemon's build revision) |
| Kernel | `6.1.190-hfs-xz3`: `linux/bzImage` `803a35b9ca8b1e3ab9554f8ef372ad0d66bacc3d7956f6de84263c5551c19fa5`, `linux/vmlinux` `862b41a921da455682c063775632ac5862ef6f6e2dc3cafd631e56ebd5b3f444` (GNU build ID `8c1de1edc01d324e0fc2502e552fd42b709a120a`), `linux/config` `28fe4a8e…` (unchanged), `System.map` `5989d2a5590b226db905a5672389d84eb82aa9d116483aa870ba2b2c2b21c184` |
| Modules | the five modules are byte-identical to `3b128cc6` (same GNU build IDs; `virtio_net` `07299518…`) |
| CGO daemon | `e0981a0a1c8d64fb6e017481444f1a8f6d8316dbb094fc788fd4d124e7b1e006` (10,970,432 bytes, GNU build ID `f6538eb3defdbb79cc78efdd598afd3d66c0bbe7`) |
| Native exporter | `freebsd/bhyve` `ef8f17de8754f8bdd6d5d517f15a859e87a2f4a8ca1542394f5608a8e40823b1` (y64 native f3 build, unchanged) |
| Window test binary / helper | `dafcc121…` / `c05b2e24…` (from `1b0a306`) |

[Evidence](evidence.json) records hashes, stage timings, receipts and control
names. [reused-files.json](reused-files.json) lists every reused file, the
rebuilt files that came out identical, and the 17 that changed.
[kernel-clean-repro.json](kernel-clean-repro.json) holds the
`compare_kernel.py` record (also packaged as `linux/clean-repro.json`
`bf7f4393…`), the stage table and the ABI negative-control receipt.

## What was rebuilt and what was reused

- **Kernel, rebuilt:** from a fresh `prepare.py` tree, using the cached pinned
  archive `4f3c00db…` (nothing downloaded). The new source inventory is
  `dfbb16bd…`.
  - `config`, `abi-negative.json`, `kernel.release`, `modules.*`,
    `Module.symvers` and the five modules (same GNU build IDs) are
    byte-identical to `3b128cc6`. Unlike b94, `Module.symvers` kept every
    CRC: 1a4 changed no type that the hfs exports reach.
  - `vmlinux`, `bzImage`, `System.map`, `build.json`, `abi.json` and
    `clean-repro.json` changed.
- **Daemon, rebuilt:** `daemon.py` from `1b0a306` with the pinned
  `freebsd-cc` wrapper and sysroot. A forced build into the package and a
  second forced build into a separate package both gave `e0981a0a…`. The
  window build reproduced it twice more from a `git archive` export.
  - Against b94, the source closure (1295 files) changed in exactly two
    files, `viewcontrol/ack_reply.go` and `viewcontrol/recall.go`; nothing
    was added or removed.
  - `go.mod`, `go.sum`, the Go and cgo toolchain, the sysroot capture, the
    runtime closure and the four Go licence files are unchanged.
  - The module cache was seeded from the b94 cache, and nothing was
    downloaded.
- **Window artifacts, rebuilt:** `build-fdescfs-window-artifacts.sh` from
  `1b0a306`. Both builds were byte-identical.
  - `controlledruntime.test` changed because `controlledruntime` links
    `viewcontrol`, which y94 changed.
  - `fdescfs-namespace.c` is unchanged. The helper `c05b2e24…` differs from
    the b94 `62d0d3fa…` only in DWARF (the new output directory as
    `comp_dir`). After `llvm-objcopy --strip-debug`, both are `07c9f5a1…`.
- **Native exporter, reused byte for byte:**
  `git diff dfdcdd9 1b0a306 -- tools/integration/lib9p` is empty.
  `stage-package.py` re-hashed all 11 packaged owned inputs against the
  repository. The three native proof records of the y64 f3 build were copied
  from the b94 run root and checked against the y64 originals.
- **114 files reused** from `3b128cc6`, each checked against its manifest:
  - 83 native FreeBSD files;
  - 5 native source-bundle and notice files;
  - patches 0001, 0002 and 0005 with `series` (0005 is unchanged since b94);
  - 22 Linux notices.

  26 of them were also checked against their current source: the
  repository patches, and the notices in the freshly prepared Linux tree.
  Before the reuse, `3b128cc6` passed `package.py check` and a full
  `admit.py` under the `1b0a306` tooling.
- **What changed in the manifest (17 files):**
  - the six kernel files above;
  - the daemon, its build and preparation records, and
    `evidence/daemon-clean-repro.json`;
  - patches 0003 and 0004, `inputs/linux-source-manifest.json` and
    `inputs/linux-source-inventory.json`;
  - `inputs/repository-source.json` and `installation-proposal.json` (it
    pins the kernel and the daemon);
  - the packaged copy of `tools/integration/artifacts/README.md`
    (`inputs/admission-recipes/README.md`), which b94 changed.
- **Not in the package:** `inputs/repository-changes.patch`. The baseline
  `1b0a306` already contains every source change, so the recorded diff is
  empty.
- **G3 init helper:** kept at `8c02b98f…`; it is not in the package.
  `cmd/homelabfs` does not import `viewcontrol` (`go list -deps`), the only
  runtime package y94 changed.
- **Lab tools:** e94 changed `tools/lab/retained9p/pod_fixture.py` (the pod
  request now carries the podman/crun NEVRA pins). It is not package content
  and no candidate pin names it: the zq3-r12 fill pins its SHA256 as the
  request's `fixture_sha256`, as every fill does.

The build and the assembly ran from the 5a4 worktree at `1b0a306`, which had
no edits during the builds and the assembly (the pin and doc edits came
afterwards). The daemon builder bindings name that worktree path. Every
other binding is a hash.

## Reproducibility and tests

The kernel stages ran twice under a user systemd scope (two CPUs, 2 GiB, no
swap, a 20-minute timeout per stage) at the same canonical output path. The
first build tree was moved aside before the repeat. `vmlinux` took 714 s and
694 s. `abi.py collect` ran on both packages. `compare_kernel.py` passed on
the first attempt (13 artifacts plus the full module ABI), and
`check_abi_mismatch.py` refused its four negative controls.

Against the prepared source, these suites passed:
- `mount_hello --sanitize`: 23 cases;
- `tag --sanitize`: 4 cases;
- the `controlled_received` scenarios `terminal-smoke`, `cleanup` and
  `exit-cleanup`;
- `view` and `view --pressure`, each plain and with `--sanitize`. They carry
  the 1a4 cases: the bounded `hfs_view_enter` wait, the fault SIGBUS and the
  view-worker stop log line.

## Admission and checks

`assemble.py` sealed and admitted the package. An independent `admit.py` run
against the pinned manifest passed. `check_candidate.py` refused all 26
real-copy mutations for their expected reasons, including the real CGO0
daemon `480713c2…`. The original and the restored copy passed again.

The tooling now admits only this manifest:
- refused as superseded: the b94 `3b128cc6…`, the r84 `4712e479…`, the l84
  `e585206a…`, the y64 `867ede36…`, the c64+d64 `66b5bb2b…`, 964
  `e01fab21…`, z54 `2e0412a6…`, w54 `89c928d5…`, r54 `5eb42c50…`, f44
  `b203799e…` and the earlier `3f770a88…`;
- still quarantined: `7e962627…`.

`tools/lab/directlaunch/argv.py` and the example spec
`internal/runtimeprep/testdata/zq3-spec.json` are unchanged, because the
exporter `ef8f17de…` is unchanged.

**What the next run must do again:**
- The daemon bytes are new, so the f3 observer qualification of the b94
  daemon (`bf86d805…`, passed in zq3-r11) does **not** carry over.
  `e0981a0a…` needs `--check-process-observer` under the bounded host guard
  on f3.
- The guest kernel is new: the Rocky kernel plan must be regenerated for
  this manifest. It names build ID `8c1de1ed…` and the new `bzImage` and
  `System.map` hashes; `config`, `modules.*`, `Module.symvers` and the five
  modules keep their hashes.
- The window artifacts are new: the fdescfs window pins name `dafcc121…`
  and `c05b2e24…`.
- The zq3-r12 fill must re-pin `pod_fixture.py` (e94).
- Booting the kernel, checking SELinux enforcing, and mounting through it
  remain zq3 steps that need explicit approval.
