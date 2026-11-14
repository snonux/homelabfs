# xz3 l84 daemon rebuild (2026-11-14): zq3-r8 recall and usable-gate fixes

**Superseded on 2026-11-14** by the [r84 rebuild](../xz3-20261114-r10/README.md)
(`4712e479…`, source `cc6beed`) for zq3-r10. Its daemon `868dc045…` (which
passed the f3 observer check in zq3-r9) refuses the guest's QUERY resume of
a claim that was never prepared, so zq3-r9's final `usable` probe parked
(R9-1, fixed by o84), and it recalls a shared DOMAIN lease on the first
holder's release (fixed by q84). Its guest kernel `870bb9d1…` predates p84:
its pressure pass can release the lease of an observation in flight. Its
native exporter `ef8f17de…` and the native, notice and unchanged patch files
are reused byte for byte. The tooling refuses this manifest as superseded.
It still passes `package.py check` and `admit.py` under the current tooling.

This candidate supersedes the [y64 rebuild](../xz3-20261110h/README.md)
(`867ede36…`, source `b869710`) only because its CGO daemon predates the
zq3-r8 fixes. In zq3-r8 (R8-1) the y64 daemon `34fb05b2…` parked the
`usable` probe's claims behind recalls. Each clean READ release and each
PROCESS_EXIT recalled both domains, and every VIEW_ACK ran a synchronous SSH
observation on the shared control worker. Both sessions then blocked on the
guest's 30 s QUERY window. These merges, all in the daemon, changed it
(task l84, the prerequisite for zq3-r9):

- 784: recalls are scoped to the releasing domain, and lock-only reductions
  never recall.
- 884: VIEW_ACK reuses a matching observation, and no Admission lock is held
  across SSH.
- d84: VIEW_ACK runs on its own transport worker lane.
- g84: VIEW_RELEASE is certified per lease.
- h84: a retrying cell can no longer starve its lane.
- b84: OBSERVE carries a bounded provider claim summary (evidence `/2`).
- q64/f84: retired view tickets are recycled, and the recall history is
  pruned.

The z54 method was used again: rebuild the daemon and the window artifacts,
and reuse everything else. **Build and offline admission only:** nothing ran
on f3, Rocky or any VM. Nothing was installed or booted, so `installed`,
`booted` and `native_accepted` stay false, and publication stays null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261114-r9/package` |
| Manifest SHA256 | `e585206aa26dcd5724065467ebb716ced7c421db0979c25290fed8c37551b213` (194 files) |
| Source checkout | `e92eaa1d1fbbe387c20dc063afc0d6655560cd9c` (also the repository baseline and the daemon's build revision) |
| CGO daemon | `868dc045907d5979c56b090b76b5b03515b1e149e7c7273bf50505f06e4627a6` (10,953,416 bytes, GNU build ID `64858f7717a221295845f3e9d5d87e2fd1f0aed7`) |
| Kernel | `6.1.190-hfs-xz3`, y64, unchanged: `linux/bzImage` `40afad22…`, `linux/vmlinux` `d2d3e5ab…` (GNU build ID `870bb9d1…`), `linux/config` `28fe4a8e…`, `System.map` `1ffea83f…` |
| Native exporter | `freebsd/bhyve` `ef8f17de8754f8bdd6d5d517f15a859e87a2f4a8ca1542394f5608a8e40823b1` (y64 native f3 build, unchanged) |
| Window test binary / helper | `30ba40be…` / `50d823be…` (from `e92eaa1`) |

[Evidence](evidence.json) records hashes, stage timings, receipts and control
names. [reused-files.json](reused-files.json) lists every reused file, the
files that came out identical, and the seven that changed.

## What was rebuilt and what was reused

- **Daemon, rebuilt:** `daemon.py` from `e92eaa1` with the pinned
  `freebsd-cc` wrapper and sysroot. A forced build into the package and a
  second forced build into a separate package both gave `868dc045…`. The
  window build reproduced it twice more from a `git archive` export.
  - Against y64, the source closure (1294 files) changed in 17 files of
    `controlledruntime`, `controlledvalidation`, `guestbridge` and
    `viewcontrol`. It gained `controlledvalidation/evidence_claims.go`,
    `guestbridge/worker.go`, `viewcontrol/recall_scope.go` and
    `viewcontrol/recycling.go`.
  - `go.mod`, `go.sum`, the Go and cgo toolchain, the sysroot capture, the
    runtime closure and the four Go licence files are unchanged.
  - The module cache was seeded from the y64 cache, and nothing was
    downloaded.
- **Window artifacts, rebuilt:** `build-fdescfs-window-artifacts.sh` from
  `e92eaa1`. Both builds were byte-identical.
  - `controlledruntime.test` changed because `controlledruntime` and the
    packages it links changed.
  - `fdescfs-namespace.c` is unchanged. The helper `50d823be…` differs from
    the y64 `3d992cdf…` only in DWARF (the new output directory as
    `comp_dir`). After `llvm-objcopy --strip-debug`, both are `07c9f5a1…`.
- **Guest kernel and modules, reused byte for byte:**
  `git diff b869710 e92eaa1 -- tools/integration/linux9p` touches only the
  `tests/controlled_received` harness (`README.md`, `host_test.go.txt`). The
  patches, `series`, `source.json`, `prepare.py`, `export.py`, the kernel
  recipe and `kernel.config` are unchanged. The kernel, its five modules and
  all of its records come from `867ede36`, with build ID `870bb9d1…`
  unchanged.
- **Native exporter, reused byte for byte:**
  `git diff b869710 e92eaa1 -- tools/integration/lib9p` is empty. `hfs9p.c`,
  `hfs9p_recovery.inc`, the other owned sources and the dedicated-backend
  patch are unchanged, and so are the native recipes. `stage-package.py`
  re-hashed all 11 packaged owned inputs against the repository. The three
  native proof records of the y64 f3 build were copied from the y64 run root
  and re-hashed.
- **136 files reused** from `867ede36`, each checked against its manifest:
  - 83 native FreeBSD files;
  - 5 native source-bundle and notice files;
  - 18 Linux kernel, module and record files;
  - 2 Linux source records;
  - 6 patches with `series`;
  - 22 Linux notices.

  30 of them were also checked against their current source: the
  repository patches and `source.json`, and the source inventory and notices
  in the prepared y64 Linux tree. Before the reuse, `867ede36` passed
  `package.py check` and a full `admit.py` under the `e92eaa1` tooling.
- **What changed in the manifest:** only seven files. They are the daemon,
  its build and preparation records, `daemon-clean-repro.json`,
  `inputs/repository-source.json`, `installation-proposal.json` (it pins the
  daemon) and the packaged copy of `tools/integration/artifacts/README.md`.
  The other 51 regenerated files (recipes, bindings, toolchain and sysroot
  records, owned sources, Go licences) came out identical.
- **Not in the package:** `inputs/repository-changes.patch`. The baseline
  `e92eaa1` already contains every source change, so the recorded diff is
  empty.
- **G3 init helper:** kept at `8c02b98f…`; it is not in the package.
  `cmd/homelabfs` reaches `guestbridge` only through `fuseadapter`'s mount
  path, and `node-init` and `volume-init` never mount.

The build and the assembly ran from the l84 worktree at `e92eaa1`, which had
no edits before assembly (4036 tracked files). The daemon builder bindings
name that worktree path. Every other binding is a hash.

## Admission and checks

`assemble.py` sealed and admitted the package. An independent `admit.py` run
against the pinned manifest passed. `check_candidate.py` refused all 26
real-copy mutations for their expected reasons, including the real CGO0
daemon `480713c2…`. The original and the restored copy passed again.

The tooling now admits only this manifest:
- refused as superseded: the y64 `867ede36…`, the c64+d64 `66b5bb2b…`, 964
  `e01fab21…`, z54 `2e0412a6…`, w54 `89c928d5…`, r54 `5eb42c50…`, f44
  `b203799e…` and the earlier `3f770a88…`;
- still quarantined: `7e962627…`.

`tools/lab/directlaunch/argv.py` and the example spec
`internal/runtimeprep/testdata/zq3-spec.json` are unchanged, because the
exporter `ef8f17de…` is unchanged.

**What the next run must do again:**
- The daemon bytes are new, so the f3 observer qualification of the y64
  daemon (zq3-r8 Phase 4 step 1a) does **not** carry over. `868dc045…` needs
  `--check-process-observer` under the bounded host guard on f3.
- The window artifacts are new: the fdescfs window pins name `30ba40be…`
  and `50d823be…`.
- The b84 `usable` gate reads OBSERVE through `host-observe` and needs this
  daemon's evidence `/2` claim summary. It writes journal `/3`, and the
  OBSERVE sequences it consumes shift Phase 9 and every later literal
  sequence by `observe_sequence_last`.
- Booting the kernel, checking SELinux enforcing, and mounting through it
  remain zq3 steps that need explicit approval.
