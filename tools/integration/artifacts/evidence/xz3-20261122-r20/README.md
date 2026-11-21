# xz3 r20 rebuild (2026-11-21): zq3-r19 busy retirement round fix and xf4

The candidate was built and admitted on 2026-11-21; the `20261122` in its directory and evidence names was chosen in advance for the planned zq3-r20 run day and is kept because pins and the run plan reference it.

This candidate supersedes the [r19 rebuild](../xz3-20261121-r19/README.md)
(`09acd09d…`, source `e160370`). In zq3-r19 (R19-1) T1 got further than in
any earlier run, then failed in its first access check. The guest's re4
back-off counted a busy local retirement round against its BLOCKED retry
cap, so a BLOCKED for the expected target gave up with `EAGAIN`. T1 also
needs xf4 (recall-free observations after the guest's own changes) to fit
its deadline. Three merges since r19 change package content:

- **1h4** (merge `c45ba9f`, patches 0003/0004 and `source.json`): a BLOCKED
  for the expected target is re-checked after a reinstall instead of being
  counted. A busy retirement round is waited for instead of being counted
  against the retry cap (R19-1). Every holder of the session's retirement
  lock releases it through `hfs_retirement_unlock`.
- **3h4** (merge `249898b`, `internal/viewcontrol` and
  `internal/guestbridge`, xf4 step A): the host side of the guest-requested
  view advance. A `VIEW_ACQUIRE` with `advance: true` gets a targeted
  DOMAIN READ lease on the current cut instead of a recall. That needs
  three things: wf4 would scope the recall to the requester, capacity fits,
  and the requester is clean.
- **4h4** (merge `9ca26e5`, patches 0002-0005 and `source.json`, xf4 step
  B): the guest side. A cacheless observation sends the advance and adopts
  the host's current target under a bounded re-check. If the re-check
  fails, it releases the grant unadopted (`-EXDEV`).

yg4 (a test-only change), the xf4 design-doc commit `d71ae02` and the zq3-r19
run records were merged too, but they change no package file. So the guest
kernel and the CGO daemon were rebuilt, and the window artifacts with the
daemon. The native exporter was reused byte for byte, with the r17 f3 build
records. **Build and offline admission only:** nothing ran on f3, Rocky or
any VM, no SSH was used, and nothing was installed or booted. So
`installed`, `booted` and `native_accepted` stay false, and publication
stays null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261122-r20/package` |
| Manifest SHA256 | `b37bcb06171fbfdf9a2b2cd3ba40711724adf07925c9a3ac167f936fa544fdec` (194 files) |
| Source checkout and repository baseline | `d71ae024bd889d789a37c2d6975fe5468bbfeef5` (the daemon's build revision; there is no `inputs/repository-changes.patch`) |
| Kernel | `6.1.190-hfs-xz3`: `linux/bzImage` `2b8880a8a69be42b938043bff6f02c5c710e41b312ad0523bd81eda962f64205`, `linux/vmlinux` `3d3a9424b92e4b2abb29d4c4c5094ee1b4dccd5efab36ab33673b683d1b324dd` (GNU build ID `b4d6cd69c788e11a050036b958d5bf04f32d2f64`), `linux/config` `28fe4a8e…` (unchanged), `System.map` `c726e252f8fa52695f055f83d00f8fd2640a49f4802183585be868e6eaccbcca` |
| Modules | the five modules are byte-identical to `09acd09d` (same GNU build IDs; `virtio_net` `072995182e1a…`); `Module.symvers` changed (`d7df3acb…`, CRCs only, see below) |
| CGO daemon | `c04c519f8f659daaca45d4fc42fb8e651bdd028eaafa84a184240f5368c2413e` (11,127,728 bytes, GNU build ID `42f2732a1820c173d9b05f1fcd9f8c4f005582f6`) |
| Native exporter | `freebsd/bhyve` `2fd7e32f5cc8762ae0ff4d5ec6c046d7d49b95c97e4b9150dd17b2ac52c39fd3` (reused from `09acd09d`, itself a byte copy of `f614d52a`, with the r17 native f3 records) |
| Window test binary / helper | `1b7e68d84ab29bc6cf1da5ce3b2549c3216b2cba04c7d4ab2530434750661cd4` / `edae7dc3f59392fa46ecc0c7355d87fe860f018d1509a3ed83daaa8b6c683982` (from `d71ae02`) |

[Evidence](evidence.json) records hashes, stage timings, source-suite
results, receipts and control names.
[kernel-clean-repro.json](kernel-clean-repro.json) holds the
`compare_kernel.py` record (also packaged as `linux/clean-repro.json`), the
stage table and the ABI negative-control receipt.
[reused-files.json](reused-files.json) lists the 24 reused Linux files, the
88 reused native files, the 62 files that came out identical and the 20
that changed. The native build facts stay in
[the r17 records](../xz3-20261120-r17/native-build-records.json).

## Kernel rebuild (r19 method)

- **Source:** a fresh `prepare.py` tree from the cached pinned archive
  `4f3c00db…` (`/home/paul/tmp/claude/c64-config/linux-pinned.tar.gz`;
  nothing downloaded), prepared at `d71ae02`.
- **Two builds at the same O path:** every `kernel.py` stage ran under a
  user systemd scope (two CPUs, 2 GiB, no swap, the r17 40-minute bound).
  The first build tree was moved aside before the repeat. `abi.py collect`
  ran on both packages.
- **No timeouts:** `kernel.py` still gives every `make` 1190 s. No stage
  reached it: the first `vmlinux` took 732 s, the repro `vmlinux` 805 s.
  The resume path (wait until no process uses the O path, then resume on
  the same tree, at most twice) never ran.
- **Clean repro:** `compare_kernel.py` found both builds identical (13
  artifacts, the five modules among them, plus the full module ABI).
- **ABI negative controls:** `check_abi_mismatch.py` refused its four
  controls (stock kernel release, different operator config identity,
  missing and different module layout CRC).
- **Unchanged against `09acd09d`:** `config`, `abi-negative.json`,
  `kernel.release`, `modules.*` and the five modules (same GNU build IDs).
  SELinux stays an active LSM (`CONFIG_LSM` unchanged).
- **Changed:** `vmlinux`, `bzImage`, `System.map`, `Module.symvers`,
  `build.json`, `abi.json` and `clean-repro.json`.
- **`Module.symvers` notes:** as in r16 to r18 (but not r19), the CRCs of
  nine hfs exports changed, because the 1h4/4h4 patches changed types
  that their signatures reach (genksyms): `hfs_call`, `hfs_control`, `hfs_control_flagged`,
  `hfs_request_allocate`, `hfs_request_resolve_gap`, `hfs_retire`,
  `hfs_session_destroy`, `v9fs_hfs_register_provider` and
  `v9fs_hfs_unregister_provider`. No export was added or removed, and no
  packaged module imports any of them.
- **Source suites:** these passed against the fresh tree, run alongside the
  first build:
  - `transport`, `mount_hello` (23 cases), `view` (with 4h4's advance
    cases), `view --pressure` (with 1h4's busy-round and BLOCKED re-check
    cases) and `setattr`, each with `--sanitize`;
  - the `controlled_received` scenarios `passed-grant` and
    `terminal-smoke`, and, new in r20, `advance`
    (`TestControlledReceivedActualViewAdvance`: the 4h4 guest against the
    3h4 xf4 host).

## Daemon and window artifacts (xc4/r15/r17/r18/r19 method)

- **Daemon:** `daemon.py` from `d71ae02` with the pinned `freebsd-cc`
  wrapper and sysroot. A forced build into `daemon-first` and a second
  forced build into `daemon-repro` both gave `c04c519f…`.
  `stage-package.py` copied the first build into the package. The module
  cache was seeded from the r19 cache (`go.mod` and `go.sum` unchanged), and
  nothing was downloaded. The four Go licences are unchanged.
- **Window artifacts:** `build-fdescfs-window-artifacts.sh` (recipe
  `d9459a2c…`, the `d71ae02` file) built from `d71ae02`. Both builds were
  byte-identical and reproduced the daemon. `controlledruntime.test` changed
  because `controlledruntime` links `internal/viewcontrol` and
  `internal/guestbridge`, and 3h4 also changed `internal/controlledruntime`
  comments and tests that are compiled into the test binary. `fdescfs-namespace.c` is unchanged. The helper
  differs from the r19 `d2ca3ab6…` only in DWARF (the new output directory
  as `comp_dir`). After `llvm-objcopy --strip-debug` it is `07c9f5a1…`, as
  before. `docs/lab/zq3-fdescfs-window/pins.json` and `build-record.json`
  follow this build, and the recipe's default commit is now `d71ae02`.

## Native exporter (reused)

No exporter input changed since r19. `git diff e160370 d71ae02` is empty
for the native recipes (`native_build.py`, `native_bundle.py`,
`native_guard.py`, `native_source.py`, `elf.py`, `package.py`). For
`tools/integration/lib9p` it touches only `owned-inputs.json`: 3h4
refreshed its digest of `docs/spec/retained9p-fixtures/views.json`. That
file pins the lib9p test inputs. `views.json` feeds only `tests/fixtures.py`,
which writes the `fixtures.h` of the lib9p test binaries. It is not an
exporter build input. The exporter's seed is the 11 owned lib9p sources and
patch 0001, and they equal the repository. `owned-inputs.json` is not a
native recipe file either; `assemble.py` only diffs it against the
repository baseline (`d71ae02`, so no diff). `native_guard.py` is still
`34ddf37c…`, the recipe the r17 native records bind.

So r20 copied every native file of `09acd09d` byte for byte (88 files: the
exporter, libraries, share files, the selected source bundle, inventory and
bundle record and the two FreeBSD notices), each checked against the
`09acd09d` manifest. It also copied the three r17 native proof records byte
for byte from the r19 root, checked equal to both the r19 and the r17
copies (`native-final-source-and-first-seal.json`,
`native-clean-repro-evidence.json`, `native-bundle-helper-transfer.json`).
`assemble.py` accepted them; no f3 action was needed.

## What was reused

- **Linux inputs:** patch 0001 with `series`, and the 22 Linux and
  Monocypher notices (24 files). Each was checked against its current
  source: the repository patches, and the notices in the freshly prepared
  Linux tree.
- **Native:** the 88 files above.
- **What changed in the manifest (20 files, none removed):**
  - the seven kernel files above;
  - the daemon, `daemon-build.json`, `daemon-preparation.json` and
    `evidence/daemon-clean-repro.json`;
  - patches 0002-0005, `inputs/linux-source-manifest.json` and
    `inputs/linux-source-inventory.json`;
  - `inputs/repository-source.json`, `installation-proposal.json` and the
    packaged `inputs/admission-recipes/README.md` (the artifacts README at
    `d71ae02`).
- **Previous candidate:** after the reuse, `09acd09d` still passes
  `package.py check` and a full `admit.py` under the `d71ae02` tooling
  (receipts `r19-container`, `r19-readmit`).
- **G3 init helper:** kept at `8c02b98f…`; it is not in the package.
  `cmd/homelabfs` does not link `internal/viewcontrol` (`go list -deps`).
  It reaches `internal/guestbridge` only through `fuseadapter`, the mount
  path. 3h4 changed guestbridge only for the `VIEW_ACQUIRE` advance field
  and its response, which only a mounted controlled session reaches.
  node-init and volume-init never mount.
- **Workstation tools:** `hfs-runtime-prep` and the signer
  `hfs-retained9p-operator` link `internal/viewcontrol` and
  `internal/guestbridge` (`go list -deps`). They are not package content,
  so the zq3-r20 fill must rebuild both. The runtime spec is unchanged
  since r19 (ControlTickets 24).

The kernel, daemon and window builds and the assembly ran from the r20
worktree at `d71ae02`, with no edits during them. The pin and doc edits
came afterwards.

## Admission and checks

`assemble.py` sealed and admitted the package. An independent `admit.py` run
against the pinned manifest passed. `check_candidate.py` refused all 26
real-copy mutations for their expected reasons, including the real CGO0
daemon `480713c2…`. The original and the restored copy passed again.

The tooling now admits only this manifest:
- refused as superseded: the r19 `09acd09d…`, the r18 `b2a4f69e…`, the r17
  `f614d52a…`, the r16 `a1af0bd7…`, the r15 `ac8020aa…`, the xd4
  `3a7131dd…`, the xc4 `e9c77e5b…`, the 5a4 `150deb29…`, the b94
  `3b128cc6…`, the r84 `4712e479…`, the l84 `e585206a…`, the y64
  `867ede36…`, the c64+d64 `66b5bb2b…`, 964 `e01fab21…`, z54 `2e0412a6…`,
  w54 `89c928d5…`, r54 `5eb42c50…`, f44 `b203799e…` and the earlier
  `3f770a88…`;
- still quarantined: `7e962627…`.

**What the next run must do again:**
- **Kernel plan:** the guest kernel is new, so the Rocky kernel plan must be
  regenerated for this manifest. It needs build ID `b4d6cd69…` and the new
  `bzImage`, `System.map` and `Module.symvers` hashes. `config`,
  `modules.*` and the five modules keep their hashes.
- **Daemon:** the daemon bytes are new, so the zq3-r19 f3 observer check of
  `bb4ad73c…` does **not** carry over. `c04c519f…` needs
  `--check-process-observer` under the bounded host guard on f3 (Phase 4
  step 1a).
- **Window artifacts:** they are new; the fdescfs window pins name
  `1b7e68d8…` and `edae7dc3…`.
- **Workstation tools:** `hfs-runtime-prep` and the signer must be rebuilt
  at the fill commit.
- **Approval steps:** booting the kernel, checking SELinux enforcing, and
  mounting through it remain zq3 steps that need explicit approval.
