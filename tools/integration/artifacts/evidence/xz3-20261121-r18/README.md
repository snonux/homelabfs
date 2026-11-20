# xz3 r18 rebuild (2026-11-21): zq3-r17 not-yet-current grant fix

This candidate supersedes the [r17 rebuild](../xz3-20261120-r17/README.md)
(`f614d52a…`, source `3430758`). In zq3-r17 (R17-1) T1's m1 root `fstat`
failed with `EKEYREJECTED`. A correct GRANTED VIEW_QUERY reply, delayed by
a recall, arrived before m1's REOPENED ACK had raised the guest's fence. The
re4 guest kernel checked it against its old state, found it uncertain and
blocked the session. Four merges since r17 change package content:

- **2f4** (merge `41dd0c9`, patches 0004 and 0005 and `source.json`): a
  verified GRANTED reply that is not yet current waits for the REOPENED-ACK
  fence raise and is queried again instead of blocking the session; passed
  grants are decoded unadopted and only released.
- **je4** (merge `b5adfab`): the last holder's release of a clean targeted
  DOMAIN READ observation lease answers RELEASED without a recall.
- **ce4** (merge `7ff04b8`): bounded timestamped recall history in
  `internal/viewcontrol`, reported in OBSERVE as `controller.recalls`.
- **e84** (merge `633509e`): a pin-only target mismatch (equal
  client-visible digest, targeted peer leases) recalls only the requesting
  domain; every other mismatch still recalls all domains.

So the guest kernel and the CGO daemon were rebuilt, and the window
artifacts with the daemon. The native exporter was reused byte for byte,
with the r17 f3 build records. **Build and offline admission only:**
nothing ran on Rocky or any VM, and nothing was installed or booted. So
`installed`, `booted` and `native_accepted` stay false, and publication
stays null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261121-r18/package` |
| Manifest SHA256 | `b2a4f69e78190591bec038e269a73f93a6b39bc9e9091c7f6d7c1d5ab73ea6d7` (194 files) |
| Source checkout and repository baseline | `633509e0e5893d6a1afe6600cd128f00c66138c3` (the daemon's build revision; there is no `inputs/repository-changes.patch`) |
| Kernel | `6.1.190-hfs-xz3`: `linux/bzImage` `f04c03e07ebd3afde89ad154a8b25391e70c895a334f77b03e14d2ec52127002`, `linux/vmlinux` `a5d320c7e81eb2de1fa9682dabe56fa72d9ca98fad1cd62b15f8c53d2094ec3d` (GNU build ID `020acb2e2d6f61ffbaccc5b7f36b160107584e59`), `linux/config` `28fe4a8e…` (unchanged), `System.map` `8088881ec2b2e4d458cefb5d36bc59cff3cfd1188151654f510b6fb268ecfaf3` |
| Modules | the five modules are byte-identical to `f614d52a` (same GNU build IDs; `virtio_net` `072995182e1a…`) |
| CGO daemon | `086d7cde0e2bd2decc3b9527d0fd76ad62722430b70f74d6367e9a108a40a3df` (11,111,304 bytes, GNU build ID `3fb0a61065ebc2595fd62071f2aee6b56a188181`) |
| Native exporter | `freebsd/bhyve` `2fd7e32f5cc8762ae0ff4d5ec6c046d7d49b95c97e4b9150dd17b2ac52c39fd3` (reused from `f614d52a` with the r17 native f3 records) |
| Window test binary / helper | `42def98ffd76328154a93804df69c9781384490af4b165d07e67cf8d55e1274b` / `f84bf4cfb6d1ab8cfe4f29e92afde3c939dfbaf45a26a7a80833e4c1a024ac28` (from `633509e`) |

[Evidence](evidence.json) records hashes, stage timings, source-suite
results, receipts and control names.
[kernel-clean-repro.json](kernel-clean-repro.json) holds the
`compare_kernel.py` record (also packaged as `linux/clean-repro.json`), the
stage table and the ABI negative-control receipt.
[reused-files.json](reused-files.json) lists the 26 reused Linux files, the
88 reused native files, the 62 files that came out identical and the 18
that changed. The native build facts stay in
[the r17 records](../xz3-20261120-r17/native-build-records.json).

## Kernel rebuild (r17 method)

- **Source:** a fresh `prepare.py` tree from the cached pinned archive
  `4f3c00db…` (`/home/paul/tmp/claude/c64-config/linux-pinned.tar.gz`;
  nothing downloaded), prepared at `41dd0c9`. Patches, `source.json` and
  `tools/integration` are unchanged at `633509e`.
- **Two builds at the same O path:** every `kernel.py` stage ran under a
  user systemd scope (two CPUs, 2 GiB, no swap, the r17 40-minute bound).
  The first build tree was moved aside before the repeat. `abi.py collect`
  ran on both packages.
- **No timeouts:** `kernel.py` still gives every `make` 1190 s. The host
  load stayed lower than in r17, so no stage reached that limit: the first
  `vmlinux` took 1064 s, the repro `vmlinux` 1142 s. The driver was ready to
  wait until no process used the O path and then resume a timed-out stage
  on the same tree (at most twice), but that path never ran.
- **Clean repro:** `compare_kernel.py` found both builds identical (the
  kernel artifacts plus the full module ABI).
- **ABI negative controls:** `check_abi_mismatch.py` refused its four
  controls (stock kernel release, different operator config identity,
  missing and different module layout CRC).
- **Unchanged against `f614d52a`:** `config`, `abi-negative.json`,
  `kernel.release`, `modules.*` and the five modules (same GNU build IDs).
  SELinux stays an active LSM (`CONFIG_LSM` unchanged).
- **Changed:** `vmlinux`, `bzImage`, `System.map`, `build.json`, `abi.json`,
  `clean-repro.json` and `Module.symvers`.
- **`Module.symvers` notes:** the CRCs of the same nine hfs exports as in
  r16 and r17 changed (`hfs_call`, `hfs_control`, `hfs_control_flagged`,
  `hfs_request_allocate`, `hfs_request_resolve_gap`, `hfs_retire`,
  `hfs_session_destroy`, `v9fs_hfs_register_provider`,
  `v9fs_hfs_unregister_provider`). No export was added or removed, and no
  packaged module imports any of them.
- **Source suites:** these passed against the fresh tree, run alongside the
  first build:
  - `transport`, `mount_hello` (23 cases), `view` and `view --pressure`,
    each with `--sanitize`;
  - the `controlled_received` scenarios `passed-grant` and
    `terminal-smoke`.

## Daemon and window artifacts (xc4/r15/r17 method)

- **Daemon:** `daemon.py` from `633509e` with the pinned `freebsd-cc`
  wrapper and sysroot. A forced build into `daemon-first` and a second
  forced build into `daemon-repro` both gave `086d7cde…`.
  `stage-package.py` copied the first build into the package. The module
  cache was seeded from the r17 cache (`go.mod` and `go.sum` unchanged), and
  nothing was downloaded. The four Go licences are unchanged.
- **Window artifacts:** `build-fdescfs-window-artifacts.sh` (recipe
  `0bbfd482…`, the `633509e` file) built from `633509e`. Both builds were
  byte-identical and reproduced the daemon. `controlledruntime.test` changed
  because `controlledruntime` links the changed packages.
  `fdescfs-namespace.c` is unchanged. The helper differs from the r17
  `6d78b1de…` only in DWARF (the new output directory as `comp_dir`). After
  `llvm-objcopy --strip-debug` it is `07c9f5a1…`, as before.
  `docs/lab/zq3-fdescfs-window/pins.json` and `build-record.json` follow this
  build, and the recipe's default commit is now `633509e`.

## Native exporter (reused)

No native source changed since r17: the 11 owned lib9p inputs equal the
repository, and `native_guard.py` at `633509e` is still `34ddf37c…`, the
recipe the r17 native records bind. So r18 copied every native file of
`f614d52a` byte for byte (88 files: the exporter, libraries, share files,
the selected source bundle, inventory and bundle record and the two
FreeBSD notices), each checked against the `f614d52a` manifest. It also
copied the three r17 native proof records byte for byte
(`native-final-source-and-first-seal.json`,
`native-clean-repro-evidence.json`, `native-bundle-helper-transfer.json`).
`assemble.py` accepted them; no f3 action was needed.

## What was reused

- **Linux inputs:** patches 0001, 0002 and 0003 with `series`, and the 22
  Linux and Monocypher notices (26 files). Each was checked against its
  current source: the repository patches, and the notices in the freshly
  prepared Linux tree.
- **Native:** the 88 files above.
- **What changed in the manifest (18 files, none removed):**
  - the seven kernel files above;
  - the daemon, `daemon-build.json`, `daemon-preparation.json` and
    `evidence/daemon-clean-repro.json`;
  - patches 0004 and 0005, `inputs/linux-source-manifest.json` and
    `inputs/linux-source-inventory.json`;
  - `inputs/repository-source.json`, `installation-proposal.json` and the
    packaged `inputs/admission-recipes/README.md` (the artifacts README at
    `633509e`).
- **Previous candidate:** after the reuse, `f614d52a` still passes
  `package.py check` and a full `admit.py` under the `633509e` tooling.
- **G3 init helper:** kept at `8c02b98f…`; it is not in the package.
  `cmd/homelabfs` links `internal/storage` and `internal/fsservice`, but e84
  only added `storage.ObservableCatalog` and
  `ControlledViewSource.ObservableDigest`, which `node-init` and
  `volume-init` never call.
- **Workstation tools:** `hfs-runtime-prep` and the signer
  `hfs-retained9p-operator` link the changed packages (`go list -deps`).
  They are not package content, so the zq3-r18 fill must rebuild both. The
  runtime spec is unchanged since r17 (ControlTickets 24).

The kernel, daemon and window builds and the assembly ran from the r18
worktree, with no edits during them. The kernel tree was prepared and both
kernels were built at `41dd0c9`. The fast-forward to `633509e` changed no
guest patch, `source.json` or `tools/integration` file. The daemon, window
build and assembly ran at `633509e`. The pin and doc edits came afterwards.

## Admission and checks

`assemble.py` sealed and admitted the package. An independent `admit.py` run
against the pinned manifest passed. `check_candidate.py` refused all 26
real-copy mutations for their expected reasons, including the real CGO0
daemon `480713c2…`. The original and the restored copy passed again.

The tooling now admits only this manifest:
- refused as superseded: the r17 `f614d52a…`, the r16 `a1af0bd7…`, the r15
  `ac8020aa…`, the xd4 `3a7131dd…`, the xc4 `e9c77e5b…`, the 5a4
  `150deb29…`, the b94 `3b128cc6…`, the r84 `4712e479…`, the l84
  `e585206a…`, the y64 `867ede36…`, the c64+d64 `66b5bb2b…`, 964
  `e01fab21…`, z54 `2e0412a6…`, w54 `89c928d5…`, r54 `5eb42c50…`, f44
  `b203799e…` and the earlier `3f770a88…`;
- still quarantined: `7e962627…`.

**What the next run must do again:**
- **Kernel plan:** the guest kernel is new, so the Rocky kernel plan must be
  regenerated for this manifest: build ID `020acb2e…` and the new `bzImage`
  and `System.map` hashes. `config`, `modules.*` and the five modules keep
  their hashes; `Module.symvers` changed.
- **Daemon:** the daemon bytes are new, so the zq3-r17 f3 observer check of
  `85900893…` does **not** carry over. `086d7cde…` needs
  `--check-process-observer` under the bounded host guard on f3 (Phase 4
  step 1a).
- **Window artifacts:** they are new; the fdescfs window pins name
  `42def98f…` and `f84bf4cf…`.
- **Workstation tools:** `hfs-runtime-prep` and the signer must be rebuilt
  at the fill commit.
- **Approval steps:** booting the kernel, checking SELinux enforcing, and
  mounting through it remain zq3 steps that need explicit approval.
