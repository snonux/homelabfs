# xz3 xc4 daemon rebuild (2026-11-18): zq3-r12 transport-fence fix

This candidate supersedes the [5a4 rebuild](../xz3-20261118-r12/README.md)
(`150deb29…`, source `1b0a306`) only because its CGO daemon predates the
zq3-r12 fix. In zq3-r12 (R12-1) the host runtime's guest transport marked
itself failed while m1 was still mounting, so both sessions' VIEW_WAITs
failed with `-EPROTO` and mount-1 came back `uncertain`; the reason was
never logged. nc4 (merge `b7d107b`) changed only the daemon (task xc4, the
prerequisite for zq3-r13):

- `internal/guestbridge`: a retried mapped cell reuses the base response
  recorded for it, instead of fencing the whole guest transport. Every
  transport fence now records its reason (`failure.go`; `service_execute.go`
  split from `service.go`; `endpoint.go`, `mapping.go`, `restore.go`,
  `terminal.go`, `transport_ledger.go`, `worker.go`).
- `internal/controlledruntime` `lifecycle_evidence.go` carries the fence
  reason into the lifecycle evidence.

The l84 method was used again: rebuild the daemon and the window artifacts,
and reuse everything else, now including the 1a4 guest kernel. **Build and
offline admission only:** nothing ran on f3, Rocky or any VM. Nothing was
installed or booted, so `installed`, `booted` and `native_accepted` stay
false, and publication stays null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261118-r13/package` |
| Manifest SHA256 | `e9c77e5bbe68f565240ef82ef3842ae713cb082e15e593036174ec4e735a1df6` (194 files) |
| Source checkout | `b7d107b073a017280b107168042ad6372c59dbbe` (also the repository baseline and the daemon's build revision) |
| CGO daemon | `c959b708f93876a5a4c620f33e64587bc0235dc4cf994de2154eb26710c4bc9b` (11,001,360 bytes, GNU build ID `eac40082a64bd782ba653ce865af47882423e628`) |
| Kernel | `6.1.190-hfs-xz3`, 5a4, unchanged: `linux/bzImage` `803a35b9…`, `linux/vmlinux` `862b41a9…` (GNU build ID `8c1de1edc01d324e0fc2502e552fd42b709a120a`), `linux/config` `28fe4a8e…`, `System.map` `5989d2a5…` |
| Native exporter | `freebsd/bhyve` `ef8f17de8754f8bdd6d5d517f15a859e87a2f4a8ca1542394f5608a8e40823b1` (y64 native f3 build, unchanged) |
| Window test binary / helper | `8194ebe0…` / `8c43fd06…` (from `b7d107b`) |

[Evidence](evidence.json) records hashes, stage timings, receipts and control
names. [reused-files.json](reused-files.json) lists every reused file, the
files that came out identical, and the seven that changed.
[kernel-recheck.json](kernel-recheck.json) holds the kernel checks rerun
under the `b7d107b` tooling.

## What was rebuilt and what was reused

- **Daemon, rebuilt:** `daemon.py` from `b7d107b` with the pinned
  `freebsd-cc` wrapper and sysroot. A forced build into the package and a
  second forced build into a separate package both gave `c959b708…`. The
  window build reproduced it twice more from a `git archive` export.
  - Against 5a4, the source closure (1297 files) changed in eight files
    (`controlledruntime/lifecycle_evidence.go` and seven `guestbridge`
    files) and gained `guestbridge/failure.go` and
    `guestbridge/service_execute.go`; nothing was removed.
  - `go.mod`, `go.sum`, the Go and cgo toolchain, the sysroot capture, the
    runtime closure and the four Go licence files are unchanged.
  - The module cache was seeded from the 5a4 cache, and nothing was
    downloaded.
- **Window artifacts, rebuilt:** `build-fdescfs-window-artifacts.sh` from
  `b7d107b`. Both builds were byte-identical.
  - `controlledruntime.test` changed because `controlledruntime` changed
    and links `guestbridge`.
  - `fdescfs-namespace.c` is unchanged. The helper `8c43fd06…` differs from
    the 5a4 `c05b2e24…` only in DWARF (the new output directory as
    `comp_dir`). After `llvm-objcopy --strip-debug`, both are `07c9f5a1…`.
- **Guest kernel and modules, reused byte for byte:**
  `git diff 1b0a306 b7d107b -- tools/integration/linux9p` is empty: the
  patches, `series`, `source.json`, `prepare.py`, `export.py` and the tests
  are unchanged. So are `kernel.py`, `abi.py`, `compare_kernel.py`,
  `check_abi_mismatch.py` and `kernel.config`. The kernel, its five modules
  and all of its records come from `150deb29`, with build ID `8c1de1ed…`
  unchanged. The kernel checks were rerun under the `b7d107b` tooling
  (`kernel-recheck.sh`):
  - every `linux/` file equals the `150deb29` one;
  - `compare_kernel.py` of the new package against the 5a4 second build
    (same O path) passed, and the record it wrote is byte-identical to the
    packaged `linux/clean-repro.json` (`bf7f4393…`);
  - `check_abi_mismatch.py` on a copy of the reused `linux/` tree without
    `abi-negative.json` refused its four controls, and the record it wrote
    is byte-identical to the packaged one (`f7c4aa9e…`);
  - `assemble.py` and the independent `admit.py` re-observed the
    kernel and module ABI against `linux/abi.json`.

  The source suites (`mount_hello`, `tag`, `controlled_received`, `view`)
  were not rerun: the prepared 1a4 source is unchanged, and 5a4 ran them
  against it.
- **Native exporter, reused byte for byte:**
  `git diff 1b0a306 b7d107b -- tools/integration/lib9p` is empty.
  `stage-package.py` re-hashed all 11 packaged owned inputs against the
  repository. The three native proof records of the y64 f3 build were copied
  from the 5a4 run root and checked against the y64 originals.
- **136 files reused** from `150deb29`, each checked against its manifest:
  - 83 native FreeBSD files;
  - 5 native source-bundle and notice files;
  - 18 Linux kernel, module and record files;
  - 2 Linux source records;
  - 5 patches with `series`;
  - 22 Linux notices.

  30 of them were also checked against their current source: the
  repository patches and `source.json`, and the source inventory and notices
  in the prepared 5a4 Linux tree. Before the reuse, `150deb29` passed
  `package.py check` and a full `admit.py` under the `b7d107b` tooling.
- **What changed in the manifest:** only seven files. They are the daemon,
  its build and preparation records, `evidence/daemon-clean-repro.json`,
  `inputs/repository-source.json`, `installation-proposal.json` (it pins the
  daemon) and the packaged copy of `tools/integration/artifacts/README.md`
  (`inputs/admission-recipes/README.md`), which 5a4 changed. The other 51
  regenerated files came out identical.
- **Not in the package:** `inputs/repository-changes.patch`. The baseline
  `b7d107b` already contains every source change, so the recorded diff is
  empty.
- **G3 init helper:** kept at `8c02b98f…`; it is not in the package.
  `cmd/homelabfs` does not link `controlledruntime` and reaches
  `guestbridge` only through `fuseadapter`'s mount path; `node-init` and
  `volume-init` never mount.
- **Workstation tools:** `cmd/hfs-runtime-prep` and the signer
  `cmd/hfs-retained9p-operator` both link `guestbridge` and
  `controlledruntime` (`go list -deps`), so nc4 changes their inputs. They
  are not package content: the zq3-r13 fill must rebuild both (the zq3-r12
  builds, prep `61a37af2…` and signer `a7f1f847…`, retire).

The build and the assembly ran from the xc4 worktree at `b7d107b`, which had
no edits before the assembly (the pin and doc edits came afterwards). The
daemon builder bindings name that worktree path. Every other binding is a
hash.

## Admission and checks

`assemble.py` sealed and admitted the package. An independent `admit.py` run
against the pinned manifest passed. `check_candidate.py` refused all 26
real-copy mutations for their expected reasons, including the real CGO0
daemon `480713c2…`. The original and the restored copy passed again.

The tooling now admits only this manifest:
- refused as superseded: the 5a4 `150deb29…`, the b94 `3b128cc6…`, the r84
  `4712e479…`, the l84 `e585206a…`, the y64 `867ede36…`, the c64+d64
  `66b5bb2b…`, 964 `e01fab21…`, z54 `2e0412a6…`, w54 `89c928d5…`, r54
  `5eb42c50…`, f44 `b203799e…` and the earlier `3f770a88…`;
- still quarantined: `7e962627…`.

`tools/lab/directlaunch/argv.py` and the example spec
`internal/runtimeprep/testdata/zq3-spec.json` are unchanged, because the
exporter `ef8f17de…` is unchanged.

**What the next run must do again:**
- The daemon bytes are new, so the f3 observer qualification of the 5a4
  daemon (`e0981a0a…`, passed in zq3-r12) does **not** carry over.
  `c959b708…` needs `--check-process-observer` under the bounded host guard
  on f3.
- The window artifacts are new: the fdescfs window pins name `8194ebe0…`
  and `8c43fd06…`.
- The guest kernel is unchanged, so the zq3-r12 Rocky kernel plan values
  (build ID `8c1de1ed…`, `bzImage`, `System.map`, the modules) still hold;
  the plan must still be regenerated for the new manifest digest.
- `hfs-runtime-prep` and the signer must be rebuilt at the fill commit.
- Booting the kernel, checking SELinux enforcing, and mounting through it
  remain zq3 steps that need explicit approval.
