# xz3 r24 rebuild (2026-11-27): 9n4 retry after a concurrent retire round

**Superseded on 2026-11-27** by the [r25 rebuild](../xz3-20261127-r25/README.md)
(`a3eac923…`, source `e9ae706`) for zq3-r25. zq3-r24 ran this candidate on
2026-11-27, and T1 failed with
[R24-1](../../../../../docs/lab/results/20261127-zq3-r24/README.md). The
1024-group access child's open failed with ENOSPC again. The 9n4 retry ran
but could not help: m0's control lane was 98% full of released but
unretired VIEW_RELEASE tickets, which no retire round offered. The failing
charge was a release ticket charged after its VIEW_ACQUIRE was granted. It
stranded a host lease, and this candidate's daemon `43238fe9…` then refused
every later VIEW_RETIRE of m0 domain-wide. gn4 fixed both sides, so r25
rebuilt the guest kernel and the daemon with the window artifacts, and
reused only this candidate's exporter `8cadae40…` byte for byte. The
tooling now refuses this manifest as superseded, and `FD_ATTR` holds
`a3eac923…` instead. This manifest still passes `package.py check` and
`admit.py` under the `e9ae706` tooling.

This candidate supersedes the [r23 rebuild](../xz3-20261126-r23/README.md)
(`21c49a55…`, source `21d8478`). zq3-r23 ran it on 2026-11-27.
[R23-1](../../../../../docs/lab/results/20261127-zq3-r23/README.md) stopped
T1: the km4 daemon passed the f3 observer check and the create of R22-1
succeeded, but the 1024-group access child's `open(group-tail)` failed with
ENOSPC. The guest log showed two km4 retire rounds on m0, 62 µs apart.
`hfs_retire` takes the session's retirement mutex with a blocking
`mutex_lock`, so the second round waited out the first, which freed the
requests, and then found nothing left to retire (-11). `hfs_retire_shared`
returned that -11 and failed the charge although its bytes had just come
back.

9n4 (merge `f6119d5`) fixes this in the guest kernel only (patches 0003,
0004 and 0005 and `source.json`):
- `hfs_retire` advances a global retirement generation after each round
  that freed requests;
- `hfs_call_reserve` and `hfs_view_ticket_charge` read it before their
  first attempt and pass it with their lane to `hfs_retire_shared`. A round
  that freed nothing itself but saw the generation move now earns the one
  retry, which stays bounded;
- the km4 line names the lane and, with the controlled provider, its byte
  and slot usage, and has a third outcome `a concurrent round freed
  requests, retrying the charge once`.

The [linux9p README](../../../linux9p/README.md#a-round-that-waited-for-a-concurrent-retirement-retries-9n4)
has the details.

Only the guest kernel was rebuilt. Since `21d8478`:
- no Go file, `go.mod` or `go.sum` changed (`git diff 21d8478 f6119d5 --
  internal cmd go.mod go.sum` touches only `internal/guestbridge/README.md`);
- no owned lib9p input changed (`tools/integration/lib9p` is unchanged);
- `daemon.py`, `daemon_cgo.py`, `native_guard.py` and the window recipe's
  build steps did not change.

So the CGO daemon, the window artifacts and the native exporter are reused
byte for byte from `21c49a55` (the r16 method, as in the
[r22 rebuild](../xz3-20261126-r22/README.md)). The daemon passed the f3
observer check in zq3-r23. The exporter has run since zq3-r21 with
`hfs_fdattr` mounts. 9n4 also changed `tools/lab/retained9p/guest.py` (the
child actions name their syscall steps). That is a lab tool, not package
content.

**Build and offline admission only:** nothing ran on f3, Rocky or any VM,
and no SSH was used. Nothing was installed or booted, so `installed`,
`booted` and `native_accepted` stay false, and publication stays null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261127-r24/package` |
| Manifest SHA256 | `c9500232792012a2131063fea7ad992c1115a5904a83aa0ba4c4815acce7670c` (195 files) |
| Source checkout | `f6119d5929d35e70cdcbcf91fa096ec6403c0bcd` (main after the 9n4 merge) |
| Repository baseline | `21d84783fccfc85a00a9b3c365b0cff0e73e627b` (the reused daemon's build revision). `inputs/repository-changes.patch` `008364d6…` records the 9n4 diff since then over `assemble.py`'s tracked list: patch 0004 and `source.json`. Patches 0003 and 0005 are pinned elsewhere in the package |
| Kernel | `6.1.190-hfs-xz3`: `linux/bzImage` `0a384db7fb48e4212384b3dfeae2f7e23dde542805aac312b6c45026d3c4af9a`, `linux/vmlinux` `595941c69bd38b372a6a7bbd8a804492281a117a2219f6ae0db9c4270b6dfe33` (GNU build ID `8ce8bd6e4c69794f9fb1264b75d59b8519366200`), `linux/config` `692ed3fa…` (unchanged), `System.map` `5a455b45ec5957a55488e58d946cd92cfae86db7d04e8a1beed304070a5fc65a`, `Module.symvers` `b68cf94aea6ab1f7f8599a1ba229951d4be1b1c4d8272dc4b9ce20f964828f17` (unchanged from `21c49a55` and `2c85b230`) |
| Modules | the five modules are byte-identical to `21c49a55` (same GNU build IDs; `virtio_net` `072995182e1a…`) |
| CGO daemon | `43238fe9953dbb3659c18b05b18f04d514d06ed600a47f9247f5e91f24bab255` (from `21d8478`, go1.27.4, GNU build ID `0b1b3d2d…`, reused; passed the f3 observer check in zq3-r23, Phase 4 step 1a, pid 5973) |
| Native exporter | `freebsd/bhyve` `8cadae403dc3f0c7add38f16ec8fae1d00b17c1802f00411896e96fa083aa4d4` (second r21 f3 build, reused) |
| Window test binary / helper | `d58fedc4…` / `6d54858c…` (from `21d8478`, reused; `pins.json` unchanged) |

What the files hold:
- [Evidence](evidence.json): hashes, stage timings, source-suite results,
  receipts and control names.
- [kernel-clean-repro.json](kernel-clean-repro.json): the `compare_kernel.py`
  record (also packaged as `linux/clean-repro.json`), the stage table and
  the ABI negative-control receipt.
- [reused-files.json](reused-files.json): the reused Linux, daemon and native
  files, the files that came out identical and the 14 that changed. One
  file was added: `inputs/repository-changes.patch`.

## Kernel rebuild (r23 method)

- **Source:** a fresh `prepare.py` tree from the cached pinned archive
  `4f3c00db…` (`/home/paul/tmp/claude/c64-config/linux-pinned.tar.gz`;
  nothing downloaded), prepared at `f6119d5`.
- **Two builds at the same O path:** every `kernel.py` stage ran under a
  user systemd scope (two CPUs, 2 GiB, no swap, the r17 40-minute bound).
  The first build tree was moved aside before the repeat. `abi.py collect`
  ran on both packages.
- **No timeouts:** `vmlinux` took 619 s and 610 s, within `kernel.py`'s
  1190 s bound per `make`. The resume path never ran.
- **Clean repro:** `compare_kernel.py` found both builds identical (13
  artifacts, the five modules among them, plus the full module ABI).
- **ABI negative controls:** `check_abi_mismatch.py` refused its four
  controls (stock kernel release, different operator config identity,
  missing and different module layout CRC).
- **Unchanged against `21c49a55`:** `config`, `Module.symvers`,
  `abi-negative.json`, `kernel.release`, `modules.*` and the five modules
  (same GNU build IDs). No export CRC changed, and no export was added or
  removed (`Module.symvers` is byte-identical). SELinux stays an active
  LSM.
- **Changed:** `vmlinux`, `bzImage`, `System.map`, `build.json`, `abi.json`
  and `clean-repro.json`.

The Linux build trees and the prepared source were deleted afterwards. The
two kernel packages, the candidate package and every receipt stay in the
run root.

## Guest harnesses

All ran against the fresh tree, alongside the kernel builds and at low
priority. Results are in `suites.tsv` in the run root:
- with `--sanitize`: `transport`, `mount_hello`, `view`, `view --pressure`,
  `setattr`, `codec`, `cursor`, `policy`, `tag`, `root` and `reclaim`;
- without it: `capture`, `requests` and `huge`;
- `controlled` (44 s; `build.py`'s 60 s compile bound held);
- `build.py --module-objects` (39 s) and `controlled_provider` against it,
  both with the run root as budget root: 73 PASS lines;
- `controlled_received --scenario both`: all 21 scenarios (1250 s). Among
  them is `package`, which now runs 9n4's
  `TestControlledReceivedActualBusyRetirementRetry` and its negative case,
  and checks the lane and usage in every retire line.

All of them passed except `dirty`:
- **`dirty` did not compile.** It builds its `compat.h` from
  `tests/requests/compat.h`, where 9n4 added `static atomic_int
  hfs_retired_rounds;`. `dirty`'s `test.c` and its compiled sources
  (`hfs_protocol.c`, `hfs_policy.c`) never use it, so
  `-Werror=unused-variable` stops the compile. This is a harness defect on
  main since 9n4, which did not run `dirty`. It is not a kernel defect.
- **A private copy passed.** The `dirty`, `requests`, `codec`, `policy` and
  `common` harnesses were copied to `dirty-probe/` in the run root, with
  only that one line marked `__attribute__((unused))`. That copy compiled
  and passed against the same prepared tree: both PASS lines, exit 0. The
  repository harness is unchanged; it needs a follow-up fix.

Not run:
- `controlled_crypto`: it needs prepared kernel headers;
- `mapped`: 9n4 did not run it either.

## What was reused

- **Daemon (7 files):** the binary, `daemon-build.json`,
  `daemon-preparation.json` and the four Go licences, each checked against
  the `21c49a55` manifest. The r23 `daemon-clean-repro.json` was copied
  unchanged.
  - The r23 builder bindings name the r23 worktree, where the daemon was
    really built. They were re-keyed to the r24 worktree, but only after
    `daemon.py` and `daemon_cgo.py` there hashed to the bound values. The
    originals are kept in the run root under `daemon-binding-origin/`.
  - Because `assemble.py` takes the repository baseline from the daemon's
    build record, the baseline is `21d8478`, and
    `inputs/repository-changes.patch` is back.
- **Native exporter (88 files):** the native FreeBSD files, the three
  source-bundle inputs and the FreeBSD notices, each checked against the
  `21c49a55` manifest.
  - The three native proof records of the second r21 f3 build
    ([records](../xz3-20261125-r21/native-build-records.json)) were copied
    byte for byte from the r21 run root and compared with the collected
    originals. They bind `native_guard.py` `34ddf37c…`, which is unchanged.
  - The 11 packaged owned lib9p inputs equal the repository's.
- **Window artifacts:** not rebuilt. They follow the daemon source and the
  toolchain, and both are unchanged. `pins.json` is unchanged; the build
  record gained a note.
- **Linux inputs (25 files):** patches 0001 and 0002 with `series`, and the
  22 Linux and Monocypher notices. All 25 were checked against their
  current source.
- **What changed in the manifest (14 files):**
  - the six kernel files above;
  - patches 0003, 0004 and 0005, `inputs/linux-source-manifest.json` and
    `inputs/linux-source-inventory.json`;
  - `inputs/repository-source.json` and `installation-proposal.json`;
  - the packaged copy of `tools/integration/artifacts/README.md`
    (`inputs/admission-recipes/README.md`), which the 21c49a55 commit
    changed.
- **Previous candidate:** `21c49a55` still passes `package.py check` and a
  full `admit.py` under the `f6119d5` tooling (receipts `r23-container`,
  `r23-readmit`).
- **G3 init helper:** kept at `8c02b98f…`. It is not in the package, and no
  Go source changed.

The kernel was prepared and the package assembled from the worker worktree
at `f6119d5`, with no edits during the builds and the assembly
(`assemble-status` is empty). The pin and doc edits came afterwards.

## Admission and checks

`assemble.py` sealed and admitted the package. An independent `admit.py` run
against the pinned manifest passed. `check_candidate.py` refused all 26
real-copy mutations for their expected reasons, including the real CGO0
daemon `480713c2…`. The original and the restored copy passed again.

`controller.ADMITTED` and `rocky_kernel.py` now name only this manifest,
and `FD_ATTR` holds it. The offline `admit.py` still accepts older
containers.
- refused as superseded: the r23 `21c49a55…` (R23-1), the r22 `2c85b230…`
  (R22-1), the second r21 `f21450ec…` (R21-1) and every earlier candidate;
- still quarantined: `7e962627…`.

`directlaunch/argv.py` and `internal/runtimeprep/testdata/zq3-spec.json`
keep the exporter `8cadae40…`.

**Gates** (after the pins moved, on go1.27.4):
- **Python suites passed:**
  - `tools/lab`: 249 tests, 3 skipped;
  - retained9p: 445 tests;
  - directlaunch: 26 tests;
  - artifacts: 44 tests.
- **lib9p `verify.sh` and `go vet ./...`:** passed.
- **`go test -race -p 2 -timeout 4h ./...`:** ran once and passed (30
  packages, 3381 s, `internal/controlledruntime` 1874 s with
  `TestZQ3T1CredentialsReplay` inside its budget, `internal/viewcontrol`
  1351 s). No data race was reported. Receipt:
  `receipts/gates/go-test-race.txt` in the run root.
- **`golangci-lint run ./...`:** reports the two staticcheck QF1001 (De
  Morgan) notes already on main (`internal/guestbridge/dispatch.go:226`,
  `internal/journal/segment/cursor.go:147`).

**What the r24 fill must do:**
- **Kernel plan:** the guest kernel is new, so the Rocky kernel plan must be
  regenerated for this manifest. It has build ID `8ce8bd6e…` and new
  `bzImage` and `System.map` hashes. `config`, `Module.symvers`,
  `modules.*` and the five modules keep their hashes.
- **Daemon:** `43238fe9…` is unchanged, and the f3 observer check passed in
  zq3-r23. Every run still repeats Phase 4 step 1a; the guard's
  `--runtime-sha256` stays `43238fe9…`.
- **Exporter and window artifacts:** unchanged. `Exporter.SHA256` stays
  `8cadae40…`, and the fdescfs window pins stay as they are.
- **`hfs_fdattr`:** allowed for this candidate, never implied. The fill must
  name `c9500232…` as `fd_attr_candidate_sha256` and point at this
  package's `manifest.json`.
- **Workstation tools:** `hfs-runtime-prep` and the signer are rebuilt at
  the fill commit as usual. No Go source changed since `21d8478`.
- **Guest tool:** the fill stages `guest.py` from its own commit, which
  includes 9n4's syscall-step names in child failure replies.
- **Approval steps:** booting the kernel, checking SELinux enforcing, and
  mounting through it remain zq3 steps that need explicit approval.
