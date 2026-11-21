# xz3 r19 rebuild (2026-11-21): zq3-r18 setattr kill-bit fix

**Superseded on 2026-11-21** by the [r20 rebuild](../xz3-20261122-r20/README.md)
(`b37bcb06…`, source `d71ae02`) for zq3-r20. Its vf4 guest kernel
`e3d474df…` counted a busy local retirement round against the re4 BLOCKED
retry cap, so a BLOCKED for the expected target gave up with `EAGAIN`
(zq3-r19 R19-1, fixed by 1h4). r20 rebuilt the kernel (1h4, 4h4) and the
daemon (3h4) with the window artifacts, and reused the r17 native exporter
and its f3 records byte for byte again. The tooling refuses this manifest
as superseded. It still passes `package.py check` and `admit.py` under the
`d71ae02` tooling.

This candidate supersedes the [r18 rebuild](../xz3-20261121-r18/README.md)
(`b2a4f69e…`, source `633509e`). In zq3-r18 (R18-1) T1 admitted both mounts
in 7 s for the first time, then failed on its first directory: every
chown/fchown of a regular file carries `ATTR_KILL_*` bits, and the 2f4 guest
kernel's `hfs_setattr` refused them with `EOPNOTSUPP` before any RPC. Two
merges since r18 change package content:

- **vf4** (merge `7605efd`, patch 0003 and `source.json`): `hfs_setattr`
  accepts the `ATTR_KILL_*` bits (R18-1); dentry ops are set once, in
  `hfs_dentry_attach`; link and the create EEXIST path `d_drop` before the
  re-lookup, so `d_splice_alias` never sees a hashed dentry.
- **wf4** (merge `e160370`, `internal/viewcontrol`): an expected-target
  recall is scoped to the requesting domain when no peer holds a lease or
  an open reservation; otherwise it stays all-domain.

yf4 (`guest.py` names each failing syscall) and the zq3-r18 run records were
merged too, but change no package file (`guest.py` is a run tool the fill
pins). So the guest kernel and the CGO daemon were rebuilt, and the window
artifacts with the daemon. The native exporter was reused byte for byte,
with the r17 f3 build records. **Build and offline admission only:**
nothing ran on f3, Rocky or any VM, no SSH was used, and nothing was
installed or booted. So `installed`, `booted` and `native_accepted` stay
false, and publication stays null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261121-r19/package` |
| Manifest SHA256 | `09acd09d66934073e0165a60ac0094e280889cc7204d95061df27072d7de70a5` (194 files) |
| Source checkout and repository baseline | `e16037027633e5fcad8d4dc11f34788d4396c8fa` (the daemon's build revision; there is no `inputs/repository-changes.patch`) |
| Kernel | `6.1.190-hfs-xz3`: `linux/bzImage` `e5add01ee7e5b7034c5b721b994b594ae3d3946258b1918ae571f19cadb0c54f`, `linux/vmlinux` `2223612b65995bd18aba024563861923c8cc6dfd8d9c45db9de236fcaa570bc0` (GNU build ID `e3d474dfdfe600d474fdf8a3a5c72cccc9b1de90`), `linux/config` `28fe4a8e…` (unchanged), `System.map` `91e970f0db05f2e9c7946ed119d84ab121b77b232cd640614030ae9b332e10be` |
| Modules | the five modules and `Module.symvers` are byte-identical to `b2a4f69e` (same GNU build IDs; `virtio_net` `072995182e1a…`) |
| CGO daemon | `bb4ad73ca4cac3906065933dfb39ff98f0e75376618f7c200168679101efa579` (11,111,144 bytes, GNU build ID `841fe14d29d7afcbc9a9c46124355d29440c75c8`) |
| Native exporter | `freebsd/bhyve` `2fd7e32f5cc8762ae0ff4d5ec6c046d7d49b95c97e4b9150dd17b2ac52c39fd3` (reused from `b2a4f69e`, itself a byte copy of `f614d52a`, with the r17 native f3 records) |
| Window test binary / helper | `408cdafdf2ec92b536bc3dc671c2798bf637897e7c36210c928463be7cf690d9` / `d2ca3ab6c767c4e3a4017b9d30671b877fe878c5ecf8ebb3f0a0d2852d9619d1` (from `e160370`) |

[Evidence](evidence.json) records hashes, stage timings, source-suite
results, receipts and control names.
[kernel-clean-repro.json](kernel-clean-repro.json) holds the
`compare_kernel.py` record (also packaged as `linux/clean-repro.json`), the
stage table and the ABI negative-control receipt.
[reused-files.json](reused-files.json) lists the 27 reused Linux files, the
88 reused native files, the 63 files that came out identical and the 16
that changed. The native build facts stay in
[the r17 records](../xz3-20261120-r17/native-build-records.json).

## Kernel rebuild (r18 method)

- **Source:** a fresh `prepare.py` tree from the cached pinned archive
  `4f3c00db…` (`/home/paul/tmp/claude/c64-config/linux-pinned.tar.gz`;
  nothing downloaded), prepared at `e160370`.
- **Two builds at the same O path:** every `kernel.py` stage ran under a
  user systemd scope (two CPUs, 2 GiB, no swap, the r17 40-minute bound).
  The first build tree was moved aside before the repeat. `abi.py collect`
  ran on both packages.
- **No timeouts:** `kernel.py` still gives every `make` 1190 s. No stage
  reached it: the first `vmlinux` took 816 s, the repro `vmlinux` 825 s.
  The resume path (wait until no process uses the O path, then resume on
  the same tree, at most twice) never ran.
- **Clean repro:** `compare_kernel.py` found both builds identical (13
  artifacts, the five modules among them, plus the full module ABI).
- **ABI negative controls:** `check_abi_mismatch.py` refused its four
  controls (stock kernel release, different operator config identity,
  missing and different module layout CRC).
- **Unchanged against `b2a4f69e`:** `config`, `abi-negative.json`,
  `kernel.release`, `modules.*`, `Module.symvers` and the five modules
  (same GNU build IDs). SELinux stays an active LSM (`CONFIG_LSM`
  unchanged).
- **Changed:** `vmlinux`, `bzImage`, `System.map`, `build.json`, `abi.json`
  and `clean-repro.json`.
- **`Module.symvers` notes:** unlike r16 to r18, no CRC changed: patch 0003
  touches no exported interface. No export was added or removed.
- **Source suites:** these passed against the fresh tree, run alongside the
  first build:
  - `transport`, `mount_hello` (23 cases), `view` and `view --pressure`,
    each with `--sanitize`;
  - new in r19, vf4's own `setattr` suite with `--sanitize` (the real
    `hfs_setattr` and dentry attach code against the VFS callers);
  - the `controlled_received` scenarios `passed-grant` and
    `terminal-smoke`.

## Daemon and window artifacts (xc4/r15/r17/r18 method)

- **Daemon:** `daemon.py` from `e160370` with the pinned `freebsd-cc`
  wrapper and sysroot. A forced build into `daemon-first` and a second
  forced build into `daemon-repro` both gave `bb4ad73c…`.
  `stage-package.py` copied the first build into the package. The module
  cache was seeded from the r18 cache (`go.mod` and `go.sum` unchanged), and
  nothing was downloaded. The four Go licences are unchanged.
- **Window artifacts:** `build-fdescfs-window-artifacts.sh` (recipe
  `4f319ffd…`, the `e160370` file) built from `e160370`. Both builds were
  byte-identical and reproduced the daemon. `controlledruntime.test` changed
  because `controlledruntime` links `internal/viewcontrol`.
  `fdescfs-namespace.c` is unchanged. The helper differs from the r18
  `f84bf4cf…` only in DWARF (the new output directory as `comp_dir`). After
  `llvm-objcopy --strip-debug` it is `07c9f5a1…`, as before.
  `docs/lab/zq3-fdescfs-window/pins.json` and `build-record.json` follow this
  build, and the recipe's default commit is now `e160370`.

## Native exporter (reused)

No native source changed since r18: `git diff 633509e e160370` is empty for
`tools/integration/lib9p` and the native recipes (`native_build.py`,
`native_bundle.py`, `native_guard.py`, `native_source.py`, `elf.py`,
`package.py`). The 11 owned lib9p inputs equal the repository, and
`native_guard.py` is still `34ddf37c…`, the recipe the r17 native records
bind. So r19 copied every native file of `b2a4f69e` byte for byte (88
files: the exporter, libraries, share files, the selected source bundle,
inventory and bundle record and the two FreeBSD notices), each checked
against the `b2a4f69e` manifest. It also copied the three r17 native proof
records byte for byte from the r18 root, checked equal to both the r18 and
the r17 copies (`native-final-source-and-first-seal.json`,
`native-clean-repro-evidence.json`, `native-bundle-helper-transfer.json`).
`assemble.py` accepted them; no f3 action was needed.

## What was reused

- **Linux inputs:** patches 0001, 0002, 0004 and 0005 with `series`, and the
  22 Linux and Monocypher notices (27 files). Each was checked against its
  current source: the repository patches, and the notices in the freshly
  prepared Linux tree.
- **Native:** the 88 files above.
- **What changed in the manifest (16 files, none removed):**
  - the six kernel files above;
  - the daemon, `daemon-build.json`, `daemon-preparation.json` and
    `evidence/daemon-clean-repro.json`;
  - patch 0003, `inputs/linux-source-manifest.json` and
    `inputs/linux-source-inventory.json`;
  - `inputs/repository-source.json`, `installation-proposal.json` and the
    packaged `inputs/admission-recipes/README.md` (the artifacts README at
    `e160370`).
- **Previous candidate:** after the reuse, `b2a4f69e` still passes
  `package.py check` and a full `admit.py` under the `e160370` tooling
  (receipts `r18-container`, `r18-readmit`).
- **G3 init helper:** kept at `8c02b98f…`; it is not in the package. wf4
  changed only `internal/viewcontrol`, which `cmd/homelabfs` does not link
  (`go list -deps`).
- **Workstation tools:** `hfs-runtime-prep` and the signer
  `hfs-retained9p-operator` link `internal/viewcontrol` (`go list -deps`).
  They are not package content, so the zq3-r19 fill must rebuild both. The
  runtime spec is unchanged since r18 (ControlTickets 24).

The kernel, daemon and window builds and the assembly ran from the r19
worktree at `e160370`, with no edits during them. The pin and doc edits
came afterwards.

## Admission and checks

`assemble.py` sealed and admitted the package. An independent `admit.py` run
against the pinned manifest passed. `check_candidate.py` refused all 26
real-copy mutations for their expected reasons, including the real CGO0
daemon `480713c2…`. The original and the restored copy passed again.

The tooling now admits only this manifest:
- refused as superseded: the r18 `b2a4f69e…`, the r17 `f614d52a…`, the r16
  `a1af0bd7…`, the r15 `ac8020aa…`, the xd4 `3a7131dd…`, the xc4
  `e9c77e5b…`, the 5a4 `150deb29…`, the b94 `3b128cc6…`, the r84
  `4712e479…`, the l84 `e585206a…`, the y64 `867ede36…`, the c64+d64
  `66b5bb2b…`, 964 `e01fab21…`, z54 `2e0412a6…`, w54 `89c928d5…`, r54
  `5eb42c50…`, f44 `b203799e…` and the earlier `3f770a88…`;
- still quarantined: `7e962627…`.

**What the next run must do again:**
- **Kernel plan:** the guest kernel is new, so the Rocky kernel plan must be
  regenerated for this manifest: build ID `e3d474df…` and the new `bzImage`
  and `System.map` hashes. `config`, `modules.*`, `Module.symvers` and the
  five modules keep their hashes.
- **Daemon:** the daemon bytes are new, so the zq3-r18 f3 observer check of
  `086d7cde…` does **not** carry over. `bb4ad73c…` needs
  `--check-process-observer` under the bounded host guard on f3 (Phase 4
  step 1a).
- **Window artifacts:** they are new; the fdescfs window pins name
  `408cdafd…` and `d2ca3ab6…`.
- **Workstation tools:** `hfs-runtime-prep` and the signer must be rebuilt
  at the fill commit.
- **Approval steps:** booting the kernel, checking SELinux enforcing, and
  mounting through it remain zq3 steps that need explicit approval.
