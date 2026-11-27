# xz3 r23 rebuild (2026-11-26): km4 BLOCKED answer to a held VIEW_RETIRE

**Superseded on 2026-11-27** by the [r24 rebuild](../xz3-20261127-r24/README.md)
(`c9500232…`, source `f6119d5`) for zq3-r24. zq3-r23 ran this candidate on
2026-11-27, and T1 failed with
[R23-1](../../../../../docs/lab/results/20261127-zq3-r23/README.md). Its
daemon passed the f3 observer check and the create of R22-1 succeeded, but
the 1024-group access child's open failed with ENOSPC. Its kernel's km4
retire round had waited out a concurrent round on the same session, which
freed the requests, then found nothing left to retire (-11) and failed the
charge. 9n4 fixed this in the guest kernel only: such a round now earns the
one retry. r24 rebuilt only the kernel and reused this candidate's daemon
`43238fe9…`, window artifacts and exporter `8cadae40…` byte for byte. The
tooling now refuses this manifest as superseded, and `FD_ATTR` holds
`c9500232…` instead. This manifest still passes `package.py check` and
`admit.py` under the `f6119d5` tooling.

This candidate supersedes the [r22 rebuild](../xz3-20261126-r22/README.md)
(`2c85b230…`, source `de9df35`). zq3-r22 ran it on 2026-11-26.
[R22-1](../../../../../docs/lab/results/20261126-zq3-r22/README.md) stopped
T1: `create(group-tail)` returned ENOSPC after m0 blocked on an uncertain
`VIEW_RETIRE` (-110). The daemon refused a child's `VIEW_RETIRE` because
another process of the domain held an observation lease. It refused with a
bare `WouldBlock`, which the transport retried without ever answering the
cell. So the guest waited out its 30 s bound while it held its retirement,
and the create's 6m4 retire-and-retry failed.

km4 (merge `21d8478`) changes two parts:
- **daemon:** `Facts.checkRetire` (`internal/controlledruntime/facts.go`)
  returns the new `viewcontrol.ErrRetirementHeld`, and `Controller.retire`
  (`internal/viewcontrol/retirement.go`) answers it BLOCKED at the
  unchanged floor. The domain-wide rule is unchanged;
- **guest kernel:** patch 0003 and `source.json`. `hfs_retire_shared` logs
  each 6m4 retire-and-retry round with `pr_info_ratelimited`. The behaviour
  does not change.

So the guest kernel and the CGO daemon (with the window artifacts) were
rebuilt. Since `3081d31`:
- no owned lib9p input changed;
- `go.mod`, `go.sum`, `daemon.py`, `daemon_cgo.py` and `native_guard.py`
  did not change.

So the native exporter is reused byte for byte from `2c85b230` (the r16
method for the native part). It ran in zq3-r21 and zq3-r22 with
`hfs_fdattr` mounts.

**Build and offline admission only:** nothing ran on f3, Rocky or any VM,
and no SSH was used. Nothing was installed or booted, so `installed`,
`booted` and `native_accepted` stay false, and publication stays null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261126-r23/package` |
| Manifest SHA256 | `21c49a55de83774c30d89b4ea7b01ddeb318e1eafeb8fd2e53418c941f79faa3` (194 files) |
| Source checkout and repository baseline | `21d84783fccfc85a00a9b3c365b0cff0e73e627b` (main after the km4 merge). The daemon was rebuilt there, so there is no `inputs/repository-changes.patch` |
| Kernel | `6.1.190-hfs-xz3`: `linux/bzImage` `049b1367b69ebc493b90e61688f01c897176c6e8aa2ccacb13d34a35bcd0fb42`, `linux/vmlinux` `1aa83d82f854938106be322e61a9875169158d7aa48616984d65dbf5e1835425` (GNU build ID `2435216f843bc8a821388e9733549f9969492dbb`), `linux/config` `692ed3fa…` (unchanged), `System.map` `1a80593f799f676b40263e0d6c1dd0ded079549f0c75b1572dcede690a05b167`, `Module.symvers` `b68cf94aea6ab1f7f8599a1ba229951d4be1b1c4d8272dc4b9ce20f964828f17` (unchanged from `2c85b230`) |
| Modules | the five modules are byte-identical to `2c85b230` (same GNU build IDs; `virtio_net` `072995182e1a…`) |
| CGO daemon | `43238fe9953dbb3659c18b05b18f04d514d06ed600a47f9247f5e91f24bab255` (from `21d8478`, go1.27.4, GNU build ID `0b1b3d2d…`; two forced builds and the window build gave the same bytes; not yet checked on f3) |
| Native exporter | `freebsd/bhyve` `8cadae403dc3f0c7add38f16ec8fae1d00b17c1802f00411896e96fa083aa4d4` (second r21 f3 build, reused) |
| Window test binary / helper | `d58fedc4f74077269802284567d7867e61b155118f5b3b6128a6600e1840946b` / `6d54858cb71dde6f3aa506e9f00f41178adba69deecf44505e79be6e4181caa2` (from `21d8478`, rebuilt) |

What the files hold:
- [Evidence](evidence.json): hashes, stage timings, source-suite results,
  receipts and control names.
- [kernel-clean-repro.json](kernel-clean-repro.json): the `compare_kernel.py`
  record (also packaged as `linux/clean-repro.json`), the stage table and
  the ABI negative-control receipt.
- [reused-files.json](reused-files.json): the reused Linux and native files,
  the files that came out identical and the 16 that changed. One file was
  removed: `inputs/repository-changes.patch`.

## Interrupted first attempt

The first attempt started at about 17:00Z. An orchestrator restart at about
17:45Z killed it during the first `vmlinux` (`-15` after 560 s), and the
`controlled_received` run was left unfinished. The daemon and window builds
had already finished: two equal builds each, with their hashes recorded.
They were kept. Everything else the first attempt wrote was moved to
`interrupted-1745Z/` in the run root, unused: the prepared tree, both build
trees, `steps.tsv`, the suites and their receipts. Then `prepare.py`, both
kernel builds and every guest suite ran again from scratch. All the kernel
and suite results below are from that rerun.

## Kernel rebuild (r22 method)

- **Source:** a fresh `prepare.py` tree from the cached pinned archive
  `4f3c00db…` (`/home/paul/tmp/claude/c64-config/linux-pinned.tar.gz`;
  nothing downloaded), prepared at `21d8478`.
- **Two builds at the same O path:** every `kernel.py` stage ran under a
  user systemd scope (two CPUs, 2 GiB, no swap, the r17 40-minute bound).
  The first build tree was moved aside before the repeat. `abi.py collect`
  ran on both packages.
- **No timeouts:** `vmlinux` took 898 s and 917 s, within `kernel.py`'s
  1190 s bound per `make`. The resume path never ran.
- **Clean repro:** `compare_kernel.py` found both builds identical (13
  artifacts, the five modules among them, plus the full module ABI).
- **ABI negative controls:** `check_abi_mismatch.py` refused its four
  controls (stock kernel release, different operator config identity,
  missing and different module layout CRC).
- **Unchanged against `2c85b230`:** `config`, `Module.symvers`,
  `abi-negative.json`, `kernel.release`, `modules.*` and the five modules
  (same GNU build IDs). km4 added only a log call, so no export CRC
  changed. SELinux stays an active LSM.
- **Changed:** `vmlinux`, `bzImage`, `System.map`, `build.json`, `abi.json`
  and `clean-repro.json`.

## Daemon and window rebuild (second r21 method)

- **Daemon:** `daemon.py --prepare` and two forced builds ran from the
  worker worktree at `21d8478` with no edits. GOMAXPROCS was 2, and the
  private module cache was seeded from the second r21 cache, so nothing was
  downloaded. Both builds gave `43238fe9…`, and the builder bindings name
  this worktree. The Go licence closure equals `2c85b230`'s.
- **Window artifacts:** the window test binary is `internal/controlledruntime`
  itself, and it links `internal/viewcontrol`. `go list -test -deps` with
  `GOOS=freebsd` lists both, and km4 changed both. So
  `build-fdescfs-window-artifacts.sh` ran with the explicit commit
  `21d8478` (recipe `64a0fcf8…`). The two builds were byte-identical, and
  the cross-built daemon equals `43238fe9…`.
  - `fdescfs-namespace.c` and the toolchain are unchanged. The helper
    differs from the `3081d31` build only in DWARF, which records the new
    out_dir as comp_dir. With `llvm-objcopy --strip-debug` it is the same
    `7872b1b4…`.
  - Afterwards only the recipe's default `CANDIDATE` and its header
    comment moved to `21d8478`.
  - `docs/lab/zq3-fdescfs-window/pins.json` and `build-record.json` name
    the new bytes.

## Guest harnesses

All ran against the fresh tree, alongside the kernel builds and at low
priority. Results are in `suites.tsv` in the run root:
- with `--sanitize`: `transport`, `mount_hello`, `view`, `view --pressure`,
  `setattr`, `codec`, `cursor`, `policy`, `tag`, `root` and `reclaim`;
- without it: `capture`, `requests`, `huge`, and `dirty` (with
  `docs/spec/retained9p-fixtures/views.json`);
- `controlled` (49 s; under this load `build.py`'s 60 s compile bound held
  this time);
- `controlled_received --scenario both`: all 21 scenarios (1842 s). Among
  them is `package`, which now checks km4's retire-and-retry log line;
- `build.py --module-objects` (53 s) and `controlled_provider` against it:
  73 PASS lines.

All passed. The first `object-check`/`controlled-provider` rows failed at
once, because the suite script passed `suites/` as the budget root while
`linux-source` lies beside it ("source/output must be inside the allocated
budget root"). The `-rerun` rows used the run root as the budget root, the
README layout, and passed.

Not run:
- `controlled_crypto`: it needs prepared kernel headers;
- `mapped`: km4 did not run it either.

## What was reused

- **Native exporter (88 files):** the native FreeBSD files, the three
  source-bundle inputs and the FreeBSD notices, each checked against the
  `2c85b230` manifest.
  - The three native proof records of the second r21 f3 build
    ([records](../xz3-20261125-r21/native-build-records.json)) were copied
    byte for byte from the r21 run root and compared with the collected
    originals. They bind `native_guard.py` `34ddf37c…`, which is unchanged.
  - The 11 packaged owned lib9p inputs equal the repository's.
- **Linux inputs (27 files):** patches 0001, 0002, 0004 and 0005 with
  `series`, and the 22 Linux and Monocypher notices. All 27 were checked
  against their current source.
- **What changed in the manifest (16 files):**
  - the six kernel files above;
  - the daemon, `freebsd/daemon-build.json`, `daemon-preparation.json` and
    `evidence/daemon-clean-repro.json`;
  - patch 0003, `inputs/linux-source-manifest.json` and
    `inputs/linux-source-inventory.json`;
  - `inputs/repository-source.json` and `installation-proposal.json`;
  - the packaged copy of `tools/integration/artifacts/README.md`
    (`inputs/admission-recipes/README.md`), which the 2c85b230 commit
    changed.
- **Previous candidate:** `2c85b230` still passes `package.py check` and a
  full `admit.py` under the `21d8478` tooling (receipts `r22-container`,
  `r22-readmit`).
- **G3 init helper:** kept at `8c02b98f…`. It is not in the package, and
  `cmd/homelabfs` links neither of the two changed Go packages.

The kernel was prepared, and the daemon and window built, from the worker
worktree at `21d8478`. The package was assembled there too. There were no
edits during the builds or the assembly, and `assemble-status` is empty.
The pin and doc edits came afterwards.

## Admission and checks

`assemble.py` sealed and admitted the package. An independent `admit.py` run
against the pinned manifest passed. `check_candidate.py` refused all 26
real-copy mutations for their expected reasons, including the real CGO0
daemon `480713c2…`. The original and the restored copy passed again.

`controller.ADMITTED` and `rocky_kernel.py` now name only this manifest,
and `FD_ATTR` holds it. The offline `admit.py` still accepts older
containers.
- refused as superseded: the r22 `2c85b230…` (R22-1), the second r21
  `f21450ec…` (R21-1) and every earlier candidate;
- still quarantined: `7e962627…`.

`directlaunch/argv.py` and `internal/runtimeprep/testdata/zq3-spec.json`
keep the exporter `8cadae40…`.

**Gates** (after the pins moved, on go1.27.4):
- **Python suites passed:**
  - `tools/lab`: 249 tests, 3 skipped. The first run failed once in
    `test_native_guest_capture` (fixture exit 30 at load ~15 while the race
    gate ran). The module passed alone, and the whole suite passed when run
    again;
  - retained9p: 440 tests;
  - directlaunch: 26 tests;
  - artifacts: 44 tests.
- **lib9p `verify.sh` and `go vet ./...`:** passed.
- **`go test -race -p 4 -timeout 4h ./...`:** ran once (3705 s). 29
  packages passed, and no data race was reported. `internal/controlledruntime`
  failed one test, `TestZQ3T1CredentialsReplay`: its two `advance true`
  subtests went over T1's 240 s measured-time budget (modelled 22 s plus
  4-7 min of this host's work; the test expects about 80-120 s of host
  work). Every recall, advance and self-admission check in them held, and
  only that wall-clock budget failed. The host was shared with other
  sessions' builds and tests (load 10-18). A run of that test alone, under
  `-race` but at the same load, failed the same way (4m42s and 5m44s).
  This rebuild changes no Go file, and km4's own final check passed it on
  `21d8478`'s Go sources. Receipts: `receipts/gates/go-test-race.txt` and
  `go-test-race-t1-rerun.txt` in the run root.
- **`golangci-lint run ./...`:** reports the two staticcheck QF1001 (De
  Morgan) notes already on main (`internal/guestbridge/dispatch.go:226`,
  `internal/journal/segment/cursor.go:147`).

**What the r23 fill must do:**
- **Kernel plan:** the guest kernel is new, so the Rocky kernel plan must be
  regenerated for this manifest. It has build ID `2435216f…` and new
  `bzImage` and `System.map` hashes. `config`, `Module.symvers`,
  `modules.*` and the five modules keep their hashes.
- **Daemon:** `43238fe9…` is new bytes. The zq3-r23 Phase 4 step 1a
  (`--check-process-observer` under the bounded host guard on f3) must
  qualify it before anything else uses it. The fill pins `43238fe9…`
  wherever it pinned `c7b89648…`, the guard's `--runtime-sha256` included.
- **Window artifacts:** the pins in `docs/lab/zq3-fdescfs-window/pins.json`
  changed (`43238fe9…`, `6d54858c…`, `d58fedc4…`). The fill regenerates
  its window pins and fills the plan from them.
- **Exporter:** unchanged. `Exporter.SHA256` stays `8cadae40…`.
- **`hfs_fdattr`:** allowed for this candidate, never implied. The fill must
  name `21c49a55…` as `fd_attr_candidate_sha256` and point at this
  package's `manifest.json`.
- **Workstation tools:** `hfs-runtime-prep` and the signer link
  `internal/viewcontrol` and `internal/controlledruntime`, which km4
  changed. The fill must rebuild both at its own commit with go1.27.4.
- **Approval steps:** booting the kernel, checking SELinux enforcing, and
  mounting through it remain zq3 steps that need explicit approval.
