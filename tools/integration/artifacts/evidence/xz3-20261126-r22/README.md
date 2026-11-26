# xz3 r22 rebuild (2026-11-26): 6m4 cross-session retire before ENOSPC

**Superseded on 2026-11-26** by the [r23 rebuild](../xz3-20261126-r23/README.md)
(`21c49a55…`, source `21d8478`) for zq3-r23. zq3-r22 ran this candidate on
2026-11-26, and T1 failed with R22-1. Its daemon `c7b89648…` refused a
`VIEW_RETIRE` beside another process's observation lease with a bare
`WouldBlock` that was never answered. The guest timed out (-110) while it
held its retirement, and T1's create failed with ENOSPC. km4 fixed this in
the daemon, which now answers BLOCKED, and added a retire-and-retry log line
to the guest kernel. r23 rebuilt the kernel, the daemon and the window
artifacts, and reused this candidate's exporter `8cadae40…` byte for byte.
The tooling now refuses this manifest as superseded, and `FD_ATTR` holds
`21c49a55…` instead. This manifest still passes `package.py check` and
`admit.py` under the `21d8478` tooling.

This candidate supersedes the [second r21 rebuild](../xz3-20261125-r21/README.md)
(`f21450ec…`, source `3081d31`). zq3-r21 ran it on 2026-11-26.
[R21-1](../../../../../docs/lab/results/20261126-zq3-r21/README.md) stopped
T1: an `fchmod` failed with ENOSPC, and both sessions blocked with a local
`-28`. The guest kernel had two faults:
- it charged a request's completion package and a view ticket's view
  package without retiring first;
- it retired only the caller's own session, so a session with nothing
  retirable failed while the other held the bytes.

6m4 (merge `de9df35`) fixes both, in the guest kernel only (patches 0003 and
0004 and `source.json`):
- `hfs_call_reserve` and the new `hfs_view_ticket_charge` take all parts of
  a charge as one attempt;
- on ENOSPC/EAGAIN, `hfs_retire_shared` retires every session of the
  provider and the charge is retried once.

The [linux9p README](../../../linux9p/README.md#retire-across-mounts-before-failing-on-a-completion-package-6m4)
has the details.

Only the guest kernel was rebuilt. Since `3081d31`:
- no non-test Go source, `go.mod` or `go.sum` changed;
- no owned lib9p input changed;
- `daemon.py`, `daemon_cgo.py` and `native_guard.py` did not change.

So the CGO daemon, the window artifacts and the native exporter are reused
byte for byte from `f21450ec` (the r16 method). The daemon passed the f3
observer check in zq3-r21. The exporter ran there with `hfs_fdattr` mounts.

**Build and offline admission only:** nothing ran on f3, Rocky or any VM,
and no SSH was used. Nothing was installed or booted, so `installed`,
`booted` and `native_accepted` stay false, and publication stays null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261126-r22/package` |
| Manifest SHA256 | `2c85b23049655d0a223a0510dc41d93fc3ec4cc35c213ac24ff05af6f8550f0e` (195 files) |
| Source checkout | `de9df35e7cd4a576337718bdc1f530544e5a603f` (main after the 6m4 merge) |
| Repository baseline | `3081d31cb035aaf72730e0d52d2598602117ebbc` (the reused daemon's build revision). `inputs/repository-changes.patch` `678b8164…` records the 6m4 diff since then over `assemble.py`'s tracked list: patch 0004 and `source.json`. Patch 0003 is pinned elsewhere in the package |
| Kernel | `6.1.190-hfs-xz3`: `linux/bzImage` `f51aff3568b1b989fa08279b6651af27da8ecd0370447e837af6d04daf08f3e5`, `linux/vmlinux` `95a7e7fe99a1ee1a13f4a34f88ee5df58614f2edd9c952862988b8e08cb05926` (GNU build ID `c34dc6fb764e294339dace4ee29672b52bc7ba1c`), `linux/config` `692ed3fa…` (unchanged), `System.map` `c710fd4cb1e83dfcc9d375102285a822ffebe5f0aa1bb1782d54c44a8c80b378`, `Module.symvers` `b68cf94aea6ab1f7f8599a1ba229951d4be1b1c4d8272dc4b9ce20f964828f17` |
| Modules | the five modules are byte-identical to `f21450ec` (same GNU build IDs; `virtio_net` `072995182e1a…`) |
| CGO daemon | `c7b896484c6dcb4fe23b6484fdfc4d71a56a16e9bff4b7410e430a0a3cea667e` (from `3081d31`, go1.27.4, reused; passed the f3 observer check in zq3-r21) |
| Native exporter | `freebsd/bhyve` `8cadae403dc3f0c7add38f16ec8fae1d00b17c1802f00411896e96fa083aa4d4` (second r21 f3 build, reused) |
| Window test binary / helper | `0129385e…` / `2157b861…` (from `3081d31`, reused) |

What the files hold:
- [Evidence](evidence.json): hashes, stage timings, source-suite results,
  receipts and control names.
- [kernel-clean-repro.json](kernel-clean-repro.json): the `compare_kernel.py`
  record (also packaged as `linux/clean-repro.json`), the stage table and
  the ABI negative-control receipt.
- [reused-files.json](reused-files.json): the reused Linux, daemon and native
  files, the files that came out identical and the 14 that changed. One
  file was added: `inputs/repository-changes.patch`.

## Kernel rebuild (r21 method)

- **Source:** a fresh `prepare.py` tree from the cached pinned archive
  `4f3c00db…` (`/home/paul/tmp/claude/c64-config/linux-pinned.tar.gz`;
  nothing downloaded), prepared at `de9df35`.
- **Two builds at the same O path:** every `kernel.py` stage ran under a
  user systemd scope (two CPUs, 2 GiB, no swap, the r17 40-minute bound).
  The first build tree was moved aside before the repeat. `abi.py collect`
  ran on both packages.
- **No timeouts:** `vmlinux` took 785 s and 634 s, within `kernel.py`'s
  1190 s bound per `make`. The resume path never ran.
- **Clean repro:** `compare_kernel.py` found both builds identical (13
  artifacts, the five modules among them, plus the full module ABI).
- **ABI negative controls:** `check_abi_mismatch.py` refused its four
  controls (stock kernel release, different operator config identity,
  missing and different module layout CRC).
- **Unchanged against `f21450ec`:** `config`, `abi-negative.json`,
  `kernel.release`, `modules.*` and the five modules (same GNU build IDs).
  SELinux stays an active LSM.
- **Changed:** `vmlinux`, `bzImage`, `System.map`, `Module.symvers`,
  `build.json`, `abi.json` and `clean-repro.json`.
- **`Module.symvers`:** the CRCs of the same nine hfs exports changed
  (`hfs_call`, `hfs_control`, `hfs_control_flagged`, `hfs_request_allocate`,
  `hfs_request_resolve_gap`, `hfs_retire`, `hfs_session_destroy`,
  `v9fs_hfs_register_provider`, `v9fs_hfs_unregister_provider`). No export
  was added or removed, and no packaged module imports any of them.

## Guest harnesses

All ran against the fresh tree, alongside the kernel builds and at low
priority; results are in `suites.tsv` in the run root:
- with `--sanitize`: `transport`, `mount_hello`, `view`, `view --pressure`,
  `setattr`, `codec`, `cursor`, `policy`, `tag`, `root` and `reclaim`;
- without it: `capture`, `requests`, `huge`, and `dirty` (with
  `docs/spec/retained9p-fixtures/views.json`);
- `controlled`;
- `controlled_received --scenario both`: all 21 scenarios, 6m4's new
  `package` scenario included (1392 s).

All passed. The host `go test -race` stages used go1.27.4.

`controlled` failed once, then passed. Its first run stopped in
`build.py`, whose ASan/UBSan consumer compile has a fixed 60 s
`subprocess` bound. The compile hit that bound while the first `vmlinux`
build loaded the host. `build.log` holds only the `TimeoutExpired`
traceback and no compiler diagnostic. Run again once after the kernel
builds, with the same README invocation (load average 4.3), it passed in
43 s: the two compiles took 9 s and 11 s, and
`TestControlledProductionGuestBackendHost` passed every subtest (`gap`,
`legacy`, `abandon`, `abandon-query`). Both runs are kept under `suites/`
(`controlled`, `controlled-rerun`).

Not run:
- `controlled_provider`: it was being repaired in parallel (im4). After the repair it passed (73 PASS) on main `de9df35`, whose guest sources are the ones this kernel was built from;
- `controlled_crypto`: needs prepared kernel headers;
- `mapped`: 6m4 did not run it either.

## What was reused

- **Daemon (7 files):** the binary, `daemon-build.json`,
  `daemon-preparation.json` and the four Go licences, each checked against
  the `f21450ec` manifest. The r21 `daemon-clean-repro.json` was copied
  unchanged.
  - The r21 builder bindings name the r21 worktree, where the daemon was
    really built. They were re-keyed to the r22 worktree, but only after
    `daemon.py` and `daemon_cgo.py` there hashed to the bound values. The
    originals are kept in the run root under `daemon-binding-origin/`.
  - Because `assemble.py` takes the repository baseline from the daemon's
    build record, the baseline stays `3081d31`, and
    `inputs/repository-changes.patch` is back.
- **Native exporter (88 files):** the native FreeBSD files, the three
  source-bundle inputs and the FreeBSD notices, each checked against the
  `f21450ec` manifest.
  - The three native proof records of the second r21 f3 build
    ([records](../xz3-20261125-r21/native-build-records.json)) were copied
    byte for byte and compared with the collected originals. They bind
    `native_guard.py` `34ddf37c…`, which is unchanged.
  - The 11 packaged owned lib9p inputs equal the repository's.
- **Window artifacts:** not rebuilt. They follow the daemon source and the
  toolchain, and both are unchanged. `pins.json` is unchanged; the build
  record gained a note.
- **Linux inputs (26 files):** patches 0001, 0002 and 0005 with `series`,
  and the 22 Linux and Monocypher notices. All 26 were checked against
  their current source.
- **What changed in the manifest (14 files):**
  - the seven kernel files above;
  - patches 0003 and 0004, `inputs/linux-source-manifest.json` and
    `inputs/linux-source-inventory.json`;
  - `inputs/repository-source.json` and `installation-proposal.json`;
  - the packaged copy of `tools/integration/artifacts/README.md`
    (`inputs/admission-recipes/README.md`), which the f21450ec commit
    changed.
- **Previous candidate:** `f21450ec` still passes `package.py check` and a
  full `admit.py` under the `de9df35` tooling (receipts `r21b-container`,
  `r21b-readmit`).
- **G3 init helper:** kept at `8c02b98f…`. It is not in the package, and no
  Go source changed.

The kernel was prepared and the package assembled from the worker worktree
at `de9df35`, with no edits during the builds and the assembly. The pin and
doc edits came afterwards.

## Admission and checks

`assemble.py` sealed and admitted the package. An independent `admit.py` run
against the pinned manifest passed. `check_candidate.py` refused all 26
real-copy mutations for their expected reasons, including the real CGO0
daemon `480713c2…`. The original and the restored copy passed again.

`controller.ADMITTED` and `rocky_kernel.py` now name only this manifest, and `FD_ATTR` holds it (the offline `admit.py` still accepts older containers):
- refused as superseded: the second r21 `f21450ec…` (R21-1), the first r21
  `1428d4db…` and every earlier candidate;
- still quarantined: `7e962627…`.

`directlaunch/argv.py` and `internal/runtimeprep/testdata/zq3-spec.json`
keep the exporter `8cadae40…`.

**Gates** (after the pins moved, on go1.27.4):
- **Python suites passed:**
  - `tools/lab`: 249 tests, 3 skipped;
  - retained9p: 440 tests, so the pl4 fix ended the earlier pod_fixture
    environmental failure;
  - directlaunch: 26 tests;
  - artifacts: 44 tests.
- **lib9p `verify.sh` and `go vet ./...`:** passed.
- **`go test -race -p 2 -timeout 4h ./...`:** ran once and passed (30
  packages, 3445 s, `internal/viewcontrol` 1419 s). No data race was
  reported.
- **`golangci-lint run ./...`:** reports two staticcheck QF1001 (De Morgan)
  notes in Go files this rebuild does not touch:
  `internal/guestbridge/dispatch.go:226` and
  `internal/journal/segment/cursor.go:147`. They are already on main.

**What the r22 fill must do:**
- **Kernel plan:** the guest kernel is new, so the Rocky kernel plan must be
  regenerated for this manifest. It has build ID `c34dc6fb…` and new
  `bzImage`, `System.map` and `Module.symvers` hashes. `config`,
  `modules.*` and the five modules keep their hashes.
- **Daemon:** `c7b89648…` is unchanged, and the f3 observer check passed in
  zq3-r21. Every run still repeats Phase 4 step 1a.
- **Exporter and window artifacts:** unchanged. `Exporter.SHA256` stays
  `8cadae40…`, and the fdescfs window pins stay as they are.
- **`hfs_fdattr`:** allowed for this candidate, never implied. The fill must
  name `2c85b230…` as `fd_attr_candidate_sha256` and point at this
  package's `manifest.json`.
- **Workstation tools:** `hfs-runtime-prep` and the signer are rebuilt at
  the fill commit as usual. No Go source changed since `3081d31`.
- **Approval steps:** booting the kernel, checking SELinux enforcing, and
  mounting through it remain zq3 steps that need explicit approval.
