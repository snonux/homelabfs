# xz3 r16 kernel rebuild (2026-11-19): zq3-r15 observation re-check fix

**Superseded on 2026-11-20** by the [r17 rebuild](../xz3-20261120-r17/README.md)
(`f614d52a…`, source `3430758`) for zq3-r17. Its ie4 guest kernel
`7a5bb8dc…` returns a host BLOCKED observation VIEW_ACQUIRE at once while
still holding its lease, and the shared budget of 8 control tickets was
full, so m1's root OPEN re-acquire was refused and T1 failed (zq3-r16
R16-1, fixed by re4 and the qe4 ControlTickets bound of 24 in the runtime
spec). r17 rebuilt the kernel (re4), the daemon (te4) with the window
artifacts, and the exporter on f3 under the 7e4 guard recipe. The copy
route used here no longer assembles at `3430758`, because `assemble.py`
reads the guard binding from the xd4 native proof records. The tooling
refuses this manifest as superseded. It still passes `package.py check`
and `admit.py` under the `3430758` tooling.

This candidate supersedes the [r15 rebuild](../xz3-20261119-r15/README.md)
(`ac8020aa…`, source `2f34b60`) only because its guest kernel predates ie4.
In zq3-r15 (R15-1) T1's `os.fstat` of the m0 root failed with `ESTALE`
before its GETATTR was sent. `hfs_mmap_begin` (patch 0004) took its DOMAIN
READ lease with VIEW_ACQUIRE, which stayed PENDING while a recall ran. The
recall reinstalled the same cut and the host granted the lease, but the view
revision had moved, so the post-acquire stamp re-check returned `-ESTALE`
without a log line. ie4 (merge `707f3d1`) changed only the guest kernel
(`tools/integration/linux9p` patches 0003/0004 and `source.json`):

- **Patch 0004:** an observation whose view moved past a held lease acquires
  again and releases the old lease only afterwards. It never re-stamps a
  held lease. A host BLOCKED or RETIRED_ID `-EAGAIN` returns at once. A
  FENCED view, a closing or blocked session or `transport_failed` keeps
  `-ESTALE`. The retries share one 30 s deadline (`-ETIMEDOUT`), and each
  failed re-check logs a rate-limited line.
- **Patch 0003:** `hfs_attr_install` re-stamps a cache observation if the
  view still has the request's target. On a moved target, `hfs_getattr`,
  `hfs_refresh` and `hfs_lookup` re-issue their request within 30 s, and a
  moved LOOKUP is balanced with FORGET. READDIR maps a moved target to
  `-ESTALE`.

The xd4 kernel method was used again: a fresh prepared source and two builds
at the same O path. The daemon, the window artifacts and the native exporter
are reused byte for byte. **Build and offline admission only:** nothing ran
on f3, Rocky or any VM, and no SSH was used. Nothing was installed or
booted, so `installed`, `booted` and `native_accepted` stay false, and
publication stays null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261119-r16/package` |
| Manifest SHA256 | `a1af0bd700396a44edbd631087d1303d85a390008771903e13937309a65c2e0d` (195 files) |
| Source checkout | `707f3d18699e08de784f83dda8eaea30f32dc909` |
| Repository baseline | `2f34b60b346aa597134b27ddf9ee29c266a17954` (the reused daemon's build revision). `inputs/repository-changes.patch` `60a66f05…` records the ie4 diff since then over `assemble.py`'s tracked list: patch 0004 and `source.json`. Patch 0003 is pinned elsewhere in the package |
| Kernel | `6.1.190-hfs-xz3`: `linux/bzImage` `7109ac494eb924408173251cb79bbceb4a570006bfe1fdfe6c45e5ec7864ebf5`, `linux/vmlinux` `3979898b83e478bb0e4b496fb577e280f9cb2d19e0a26f99675a9d27e33c488a` (GNU build ID `7a5bb8dccbc1c89ed44c221e9b80deaf9baf70fe`), `linux/config` `28fe4a8e…` (unchanged), `System.map` `95802ae3d5752d49342103a32bb80281ca072f2ac57cba5589ab02c7dec6f17e` |
| Modules | the five modules are byte-identical to `ac8020aa` (same GNU build IDs; `virtio_net` `072995182e1a…`) |
| CGO daemon | `8668e5f085e4d53126ac382dd435339e15c8b7a5085d36d953f4432d9c7eb7cf` (r15, from `2f34b60`, reused; it passed the f3 observer check in zq3-r15) |
| Native exporter | `freebsd/bhyve` `2fd7e32f5cc8762ae0ff4d5ec6c046d7d49b95c97e4b9150dd17b2ac52c39fd3` (xd4 native f3 build, reused by r15 and here) |
| Window test binary / helper | `9d9f0f01…` / `d6f2e4d0…` (r15, from `2f34b60`, reused) |

[Evidence](evidence.json) records hashes, stage timings, suites, receipts
and control names. [kernel-clean-repro.json](kernel-clean-repro.json) holds
the `compare_kernel.py` record (also packaged as `linux/clean-repro.json`),
the stage table and the ABI negative-control receipt.
[reused-files.json](reused-files.json) lists the 121 reused files, the files
that came out identical and the 15 that changed.

## Kernel rebuild

- **Source:** a fresh `prepare.py` tree from the cached pinned archive
  `4f3c00db…` (`/home/paul/tmp/claude/c64-config/linux-pinned.tar.gz`, a link
  to the archive xd4 used; nothing downloaded). The new source inventory is
  `b59675cf…`.
- **Two builds at the same O path:** every `kernel.py` stage ran under a user
  systemd scope (two CPUs, 2 GiB, no swap, a 20-minute timeout per stage).
  The first build tree was moved aside before the repeat. `vmlinux` took
  778 s and 723 s. `abi.py collect` ran on both packages.
- **Clean repro:** `compare_kernel.py` passed on the first attempt (13
  artifacts plus the full module ABI).
- **ABI negative controls:** `check_abi_mismatch.py` refused its four
  controls (stock kernel release, different operator config identity,
  missing and different module layout CRC).
- **Unchanged against `ac8020aa`:** `config`, `abi-negative.json`,
  `kernel.release`, `modules.*` and the five modules (same GNU build IDs).
  SELinux stays an active LSM (`CONFIG_LSM` unchanged).
- **Changed:** `vmlinux`, `bzImage`, `System.map`, `build.json`, `abi.json`,
  `clean-repro.json` and `Module.symvers`.
- **`Module.symvers` notes:** the CRCs of nine hfs exports changed
  (`hfs_call`, `hfs_control`, `hfs_control_flagged`, `hfs_request_allocate`,
  `hfs_request_resolve_gap`, `hfs_retire`, `hfs_session_destroy`,
  `v9fs_hfs_register_provider`, `v9fs_hfs_unregister_provider`). No export was
  added or removed, and no packaged module imports any of them.
- **Source suites:** against the prepared ie4 source, alongside the first
  build, these passed:
  - `mount_hello`, `tag` and `transport` with `--sanitize`;
  - `dirty`, with `docs/spec/retained9p-fixtures/views.json`;
  - the `controlled_received` scenarios `terminal-smoke`, `cleanup` and
    `exit-cleanup`;
  - `view` and `view --pressure`, each plain and with `--sanitize`.

## What was reused

`git diff 2f34b60 707f3d1 -- internal cmd go.mod go.sum
tools/integration/lib9p` touches only `internal/guestbridge/README.md`. In
`tools/integration/artifacts` only `README.md` and evidence records changed.
Before the reuse, `ac8020aa` passed `package.py check` and a full `admit.py`
under the `707f3d1` tooling.

- **Daemon, reused byte for byte:** the binary, `daemon-build.json`,
  `daemon-preparation.json` and the four Go licences (7 files), plus the r15
  `daemon-clean-repro.json`, which came out identical.
  - The r15 builder bindings name the r15 worktree, where the daemon was
    really built. They were re-keyed to the r16 worktree, but only after
    checking that `daemon.py` and `daemon_cgo.py` there hash to the bound
    values (the 964/xd4 deviation). The original binding files are kept in
    the run root under `daemon-binding-origin/`.
  - Because `assemble.py` takes the repository baseline from the daemon's
    build record, the baseline stays `2f34b60`, and
    `inputs/repository-changes.patch` is back (see the table).
- **Window artifacts, reused:** they follow the daemon source, and the daemon
  is the `2f34b60` build. `docs/lab/zq3-fdescfs-window/pins.json` is
  unchanged; the build record gained a note.
- **Native exporter, reused byte for byte:** the 83 native FreeBSD files, the
  three source-bundle inputs and the two FreeBSD notices. `stage-package.py`
  re-hashed all 11 packaged owned inputs against the repository. The three
  native proof records of the xd4 f3 build were copied from the r15 run root
  after checking that they equal the xd4 originals; the clean-repro proof
  was checked against the record packaged in `ac8020aa`.
- **Linux inputs reused:** patches 0001, 0002 and 0005 with `series`, and the
  22 Linux and Monocypher notices. All 26 were also checked against their
  current source: the repository patches, and the notices in the freshly
  prepared Linux tree.
- **What changed in the manifest (15 files):**
  - the seven kernel files above;
  - patches 0003 and 0004, `inputs/linux-source-manifest.json` and
    `inputs/linux-source-inventory.json`;
  - `inputs/repository-source.json`, `inputs/repository-changes.patch` (new
    again) and `installation-proposal.json`, which pins the kernel;
  - the packaged copy of `tools/integration/artifacts/README.md`
    (`inputs/admission-recipes/README.md`), which r15 changed.
- **G3 init helper:** kept at `8c02b98f…`. It is not in the package, and no
  Go source changed.
- **Workstation tools:** no Go source changed, so `hfs-runtime-prep` and the
  signer keep their zq3-r15 inputs. `tools/lab/directlaunch/argv.py` and the
  example spec `internal/runtimeprep/testdata/zq3-spec.json` are unchanged,
  because the exporter is unchanged.

The build and the assembly ran from the r16 worktree at `707f3d1`. It had no
edits during the builds and the assembly (the pin and doc edits came
afterwards).

## Admission and checks

`assemble.py` sealed and admitted the package. An independent `admit.py` run
against the pinned manifest passed. `check_candidate.py` refused all 26
real-copy mutations for their expected reasons, including the real CGO0
daemon `480713c2…`. The original and the restored copy passed again.

The tooling now admits only this manifest:
- refused as superseded: the r15 `ac8020aa…`, the xd4 `3a7131dd…`, the xc4
  `e9c77e5b…`, the 5a4 `150deb29…`, the b94 `3b128cc6…`, the r84
  `4712e479…`, the l84 `e585206a…`, the y64 `867ede36…`, the c64+d64
  `66b5bb2b…`, 964 `e01fab21…`, z54 `2e0412a6…`, w54 `89c928d5…`, r54
  `5eb42c50…`, f44 `b203799e…` and the earlier `3f770a88…`;
- still quarantined: `7e962627…`.

**What the next run must do again:**
- The guest kernel is new. The Rocky kernel plan must be regenerated for this
  manifest: build ID `7a5bb8dc…` and the new `bzImage` and `System.map`
  hashes. `config`, `modules.*` and the five modules keep their hashes;
  `Module.symvers` changed.
- The daemon bytes are unchanged and passed the f3 observer check in
  zq3-r15. Every run still repeats Phase 4 step 1a.
- The window artifacts and the exporter are unchanged.
- Booting the kernel, checking SELinux enforcing, and mounting through it
  remain zq3 steps that need explicit approval.
