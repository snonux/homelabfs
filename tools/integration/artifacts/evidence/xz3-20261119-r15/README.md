# xz3 r15 daemon rebuild (2026-11-19): zq3-r14 read-only root OPEN fix

**Superseded on 2026-11-19** by the [r16 rebuild](../xz3-20261119-r16/README.md)
(`a1af0bd7…`, source `707f3d1`) for zq3-r16. Its oc4 guest kernel
`9b167690…` fails an observation with `-ESTALE` when a recall moves the view
while the observation's VIEW_ACQUIRE is PENDING, so T1's root `fstat` failed
before its GETATTR was sent (zq3-r15 R15-1, fixed by ie4). Its z94 daemon
`8668e5f0…` (which passed the f3 observer check in zq3-r15), the window
artifacts, its pc4 exporter `2fd7e32f…` and every other non-kernel file are
reused byte for byte. The tooling refuses this manifest as superseded. It
still passes `package.py check` and `admit.py` under the current tooling.

This candidate supersedes the [xd4 rebuild](../xz3-20261119-r14/README.md)
(`3a7131dd…`, source `942614a`) only because its CGO daemon predates the
zq3-r14 fix. In zq3-r14 (R14-1) T1's m0 root OPEN waited about 14 s behind
three recalls, past the 10 s alarm. The daemon treated a read-only OPEN
(`open(O_RDONLY|O_DIRECTORY)` of a mount root) as a non-read for conflicts,
so it recalled every domain against the caller's own READ observation lease.
The root OPEN's HandlePin also moved the source cut, so every domain's next
observation missed its expected target and recalled again. z94 (merge
`2f34b60`) changed only the daemon:

- `internal/viewcontrol`: a read-only CALL OPEN (access READ, no create,
  exclusive, truncate or append flag) conflicts like a read, only with
  EXCLUSIVE_DIRTY leases and open reservations (`read_open.go`, new;
  `admission.go`, `recall_scope.go`).
- `internal/fsservice`: the volume root's lifetime is pinless, so a root
  OPEN/RELEASE cycle journals nothing and keeps the source cut
  (`controlled.go`, `execute.go`, `types.go`).

The xc4 method was used again: rebuild the daemon and the window artifacts,
and reuse everything else, now including the oc4 guest kernel and the pc4
exporter. **Build and offline admission only:** nothing ran on f3, Rocky or
any VM, and no SSH was used. Nothing was installed or booted, so
`installed`, `booted` and `native_accepted` stay false, and publication
stays null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261119-r15/package` |
| Manifest SHA256 | `ac8020aa0012550d83b4ab15ffd79900a31338330513e2af7d4f5e58b910c1c6` (194 files) |
| Source checkout | `2f34b60b346aa597134b27ddf9ee29c266a17954` (also the repository baseline and the daemon's build revision) |
| CGO daemon | `8668e5f085e4d53126ac382dd435339e15c8b7a5085d36d953f4432d9c7eb7cf` (11,005,736 bytes, GNU build ID `24a8935e6c91ae731659547a4bebda629fe8ce40`) |
| Kernel | `6.1.190-hfs-xz3`, oc4, unchanged: `linux/bzImage` `e0daa57e…`, `linux/vmlinux` `d2690b42…` (GNU build ID `9b167690f6cd2f0538ecb6df06ab76121736eacb`), `linux/config` `28fe4a8e…`, `System.map` `dd88dfcb…` |
| Native exporter | `freebsd/bhyve` `2fd7e32f5cc8762ae0ff4d5ec6c046d7d49b95c97e4b9150dd17b2ac52c39fd3` (xd4 native f3 build, unchanged) |
| Window test binary / helper | `9d9f0f01e821bdeee101b88b3f0ef20853f339e97ddaff6bbe08984da6157319` / `d6f2e4d0a41073cb151f0f1c312607bc386726f55dac0ca71cf584902e067cd7` (from `2f34b60`) |

[Evidence](evidence.json) records hashes, stage timings, receipts and control
names. [reused-files.json](reused-files.json) lists every reused file, the
files that came out identical, the seven that changed and the one removed.
[kernel-recheck.json](kernel-recheck.json) holds the kernel checks rerun
under the `2f34b60` tooling.

## What was rebuilt and what was reused

- **Daemon, rebuilt:** `daemon.py` from `2f34b60` with the pinned
  `freebsd-cc` wrapper and sysroot. A forced build into the package and a
  second forced build into a separate package both gave `8668e5f0…`. The
  window build reproduced it twice more from a `git archive` export.
  - Against xc4 (`c959b708…`, carried by `e9c77e5b` and `3a7131dd`), the
    source closure (1298 files) changed in five files
    (`fsservice/controlled.go`, `execute.go`, `types.go`,
    `viewcontrol/admission.go`, `recall_scope.go`) and gained
    `viewcontrol/read_open.go`; nothing was removed.
  - `go.mod`, `go.sum`, the Go and cgo toolchain, the sysroot capture, the
    runtime closure and the four Go licence files are unchanged.
  - The module cache was seeded from the xc4 cache, and nothing was
    downloaded.
- **Window artifacts, rebuilt:** `build-fdescfs-window-artifacts.sh` from
  `2f34b60`. Both builds were byte-identical.
  - `controlledruntime.test` changed because `controlledruntime` links
    `viewcontrol` and `fsservice`.
  - `fdescfs-namespace.c` is unchanged. The helper `d6f2e4d0…` differs from
    the xc4 `8c43fd06…` only in DWARF (the new output directory as
    `comp_dir`). After `llvm-objcopy --strip-debug`, both are `07c9f5a1…`.
- **Guest kernel and modules, reused byte for byte:**
  `git diff 942614a 2f34b60 -- tools/integration/linux9p` is empty, and in
  `tools/integration/artifacts` only `README.md` and evidence records
  changed. The kernel, its five modules and all of its records come from
  `3a7131dd`, with build ID `9b167690…` unchanged. The kernel checks were
  rerun under the `2f34b60` tooling (`kernel-recheck.sh`):
  - every `linux/` file equals the `3a7131dd` one;
  - `compare_kernel.py` of the new package against the xd4 second build
    (same O path) passed, and the record it wrote is byte-identical to the
    packaged `linux/clean-repro.json`;
  - `check_abi_mismatch.py` on a copy of the reused `linux/` tree without
    `abi-negative.json` refused its four controls, and the record it wrote
    is byte-identical to the packaged one;
  - `assemble.py` and the independent `admit.py` re-observed the
    kernel and module ABI against `linux/abi.json`.

  The source suites were not rerun: the prepared oc4 source is unchanged,
  and xd4 ran them against it.
- **Native exporter, reused byte for byte:**
  `git diff 942614a 2f34b60 -- tools/integration/lib9p` is empty.
  `stage-package.py` re-hashed all 11 packaged owned inputs against the
  repository. The three native proof records of the xd4 f3 build were copied
  from the xd4 run root; the clean-repro proof was checked against the
  record packaged in `3a7131dd`.
- **136 files reused** from `3a7131dd`, each checked against its manifest:
  the 83 native FreeBSD files, the 5 native source-bundle and notice files,
  the 18 Linux kernel, module and record files, the 2 Linux source records,
  the 5 patches with `series` and the 22 Linux and Monocypher notices. 30 of them were
  also checked against their current source: the repository patches and
  `source.json`, and the source inventory and notices in the xd4 prepared
  Linux tree. Before the reuse, `3a7131dd` passed `package.py check` and a
  full `admit.py` under the `2f34b60` tooling.
- **What changed in the manifest:** seven files. They are the daemon, its
  build and preparation records, `evidence/daemon-clean-repro.json`,
  `inputs/repository-source.json`, `installation-proposal.json` (it pins the
  daemon) and the packaged copy of `tools/integration/artifacts/README.md`
  (`inputs/admission-recipes/README.md`), which xd4 changed. The other 51
  regenerated files came out identical.
- **Removed from the package:** `inputs/repository-changes.patch`. The
  repository baseline moves from `b7d107b` (the reused xc4 daemon's build
  revision) to the new build revision `2f34b60`, which already contains the
  oc4/pc4 sources, so the recorded diff is empty.
- **G3 init helper:** kept at `8c02b98f…`; it is not in the package.
  `cmd/homelabfs` does not link `viewcontrol` and reaches `fsservice` only
  through `fuseadapter`'s mount path; `node-init` and `volume-init` never
  mount.
- **Workstation tools:** `cmd/hfs-runtime-prep` and the signer
  `cmd/hfs-retained9p-operator` both link `viewcontrol` and `fsservice`
  (`go list -deps`), so z94 changes their inputs. They are not package
  content: the zq3-r15 fill must rebuild both.
  `tools/lab/directlaunch/argv.py` and the example spec
  `internal/runtimeprep/testdata/zq3-spec.json` are unchanged, because the
  exporter `2fd7e32f…` is unchanged.

The build and the assembly ran from the r15 worktree at `2f34b60`, which had
no edits before the assembly (the pin and doc edits came afterwards). The
daemon builder bindings name that worktree path. Every other binding is a
hash.

## Admission and checks

`assemble.py` sealed and admitted the package. An independent `admit.py` run
against the pinned manifest passed. `check_candidate.py` refused all 26
real-copy mutations for their expected reasons, including the real CGO0
daemon `480713c2…`. The original and the restored copy passed again.

The tooling now admits only this manifest:
- refused as superseded: the xd4 `3a7131dd…`, the xc4 `e9c77e5b…`, the 5a4
  `150deb29…`, the b94 `3b128cc6…`, the r84 `4712e479…`, the l84
  `e585206a…`, the y64 `867ede36…`, the c64+d64 `66b5bb2b…`, 964
  `e01fab21…`, z54 `2e0412a6…`, w54 `89c928d5…`, r54 `5eb42c50…`, f44
  `b203799e…` and the earlier `3f770a88…`;
- still quarantined: `7e962627…`.

**What the next run must do again:**
- The daemon bytes are new, so the f3 observer qualification of the xc4
  daemon (`c959b708…`, passed in zq3-r13 and zq3-r14) does **not** carry
  over. `8668e5f0…` needs `--check-process-observer` under the bounded host
  guard on f3.
- The window artifacts are new: the fdescfs window pins name `9d9f0f01…`
  and `d6f2e4d0…`.
- The guest kernel is unchanged, so the zq3-r14 Rocky kernel plan values
  (build ID `9b167690…`, `bzImage`, `System.map`, the modules) still hold;
  the plan must still be regenerated for the new manifest digest.
- `hfs-runtime-prep` and the signer must be rebuilt at the fill commit.
- Booting the kernel, checking SELinux enforcing, and mounting through it
  remain zq3 steps that need explicit approval.
