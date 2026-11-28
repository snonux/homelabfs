# xz3 r26 rebuild (2026-11-27): hp4 per-ticket VIEW_RETIRE rule

This candidate supersedes the [r25 rebuild](../xz3-20261127-r25/README.md)
(`a3eac923…`, source `e9ae706`). zq3-r25 ran it on 2026-11-27.
[R25-1](../../../../../docs/lab/results/20261127-zq3-r25/README.md) stopped
T1: every probe syscall passed, the 1024-group access child included, but
the retention inventory's `lstat` of `setgid/created` failed with ENOSPC.
m0's charge-pressure round logged `view retire -11` with the control lane at
526509074 of 536870912 bytes, then `charge still failed after the retry:
error -28`. The lane held 13 answered VIEW_RELEASE tickets of the T1
process. The gn4 daemon answered their VIEW_RETIRE BLOCKED, because a lease
of the same credentials was always active: the guest releases an
observation's lease asynchronously, often only after the process's next
observation has acquired its own.

hp4 (merge `9994339`) changes two parts:
- **daemon:** `retirementBlocked` (`internal/viewcontrol/retirement.go`)
  drops gn4's per-credential active-lease clause. A range is refused only
  when one of its tickets is a GRANTED acquisition (`GrantedLeaseID`) of a
  still active lease. `factsLedger.checkRetire`
  (`internal/controlledruntime/facts.go`) applies the same per-ticket rule.
  The recall and open-reservation clauses are unchanged;
- **guest patches 0003 and 0004:** comments only (they now describe the
  host rule), regenerated with `export.py --task hp4`, so `source.json`
  changed too.

The [linux9p README](../../../linux9p/README.md) has the details (hp4
section).

So the CGO daemon (with the window artifacts) and the guest kernel were
rebuilt, as in the [r25 rebuild](../xz3-20261127-r25/README.md). Since
`e9ae706`:
- no owned lib9p input changed;
- `go.mod`, `go.sum`, `daemon.py`, `daemon_cgo.py`, `native_guard.py` and
  `kernel.config` did not change;
- of the Linux patches only 0003 and 0004 changed.

So the native exporter is reused byte for byte from `a3eac923` (the r16
method for the native part). It has run since zq3-r21 with `hfs_fdattr`
mounts.

**The kernel bytes are not identical to r25's.** hp4 changed only comments,
but the changed comment lines shifted the source line of one `WARN`/`BUG`
site, so one `__bug_table` line number differs. Against the r25 `vmlinux`
exactly 21 bytes differ: 20 in `.notes` (the GNU build ID) and 1 in
`__bug_table` (receipts `vmlinux-r25-diff-offsets.txt` and
`vmlinux-r25-diff-sections.txt` in the run root). `bzImage`, `build.json`
and `abi.json` follow. `System.map`, `config`, `Module.symvers`,
`abi-negative.json` and the five modules are byte-identical to
`a3eac923`. **The r26 fill must therefore regenerate
the Rocky kernel plan; it cannot keep the r25 kernel plan values.**

**Build and offline admission only:** nothing ran on f3, Rocky or any VM,
and no SSH was used. Nothing was installed or booted, so `installed`,
`booted` and `native_accepted` stay false, and publication stays null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261127-r26/package` |
| Manifest SHA256 | `80cd01bbfafdbf98361d0103d898c277af48350d9fe291d04da8a8f1f3789e30` (194 files) |
| Source checkout and repository baseline | `99943399396d59039d56d5fec2b2ff296b54e9d4` (main after the hp4 merge). The daemon was rebuilt there, so there is no `inputs/repository-changes.patch` |
| Kernel | `6.1.190-hfs-xz3`: `linux/bzImage` `8515701f78fe45df94015c3d06009c085db4c9ad604fff6423e8f29e1b229645`, `linux/vmlinux` `cefad6e33d8c10801b07d38b3e6d0d7404d37f515a84a447141ba1b6eda6deb9` (GNU build ID `899cc101b367585991867498ab9601001f528e43`), `linux/config` `692ed3fa…`, `System.map` `d1ec01e8b86ef9fe954494095d866eef7e92e192f6e09e5f9a85c9e17ab12cb0`, `Module.symvers` `b68cf94aea6ab1f7f8599a1ba229951d4be1b1c4d8272dc4b9ce20f964828f17` (all three unchanged from `a3eac923`) |
| Modules | the five modules are byte-identical to `a3eac923` (same GNU build IDs; `virtio_net` `072995182e1a…`) |
| CGO daemon | `024528a4bd7fd57d896c4aa6e35f2b274c10782ad5affd569c5cdcdfc858d97c` (from `9994339`, go1.27.4, GNU build ID `42fa592c…`; two forced builds and the window build gave the same bytes; not yet checked on f3) |
| Native exporter | `freebsd/bhyve` `8cadae403dc3f0c7add38f16ec8fae1d00b17c1802f00411896e96fa083aa4d4` (second r21 f3 build, reused) |
| Window test binary / helper | `b0d563df24e026bf8e678af70a91e9a3a396d8ca0d6155fe5b5a0b87f415e19e` / `e47d07f2cbc407e5e4e829ec22b2a5e2f9511c4ef04c6259c5ce067fbd962418` (from `9994339`, rebuilt; helper `llvm-objcopy --strip-debug` `7872b1b4…`, unchanged) |

What the files hold:
- [Evidence](evidence.json): hashes, stage timings, source-suite results,
  receipts and control names.
- [kernel-clean-repro.json](kernel-clean-repro.json): the `compare_kernel.py`
  record (also packaged as `linux/clean-repro.json`), the stage table and
  the ABI negative-control receipt.
- [reused-files.json](reused-files.json): the reused Linux and native files,
  the files that came out identical and the 16 that changed.

## Kernel rebuild (r25 method)

- **Source:** a fresh `prepare.py` tree from the cached pinned archive
  `4f3c00db…` (`/home/paul/tmp/claude/c64-config/linux-pinned.tar.gz`;
  nothing downloaded), prepared at `9994339`.
- **Two builds at the same O path:** every `kernel.py` stage ran under a
  user systemd scope (two CPUs, 2 GiB, no swap, the r17 40-minute bound).
  The first build tree was moved aside before the repeat. `abi.py collect`
  ran on both packages.
- **Timeouts and resumes:** at host load 15-24 (this run's `go test -race`
  and guest suites plus other sessions) both `vmlinux` stages reached
  `kernel.py`'s 1190 s `make` bound (first 1196 s, repro 1208 s). The
  driver resumed each on the same O tree:
  - resume r1 failed both times (68 s and 150 s). The driver's settle step
    looks only for `make` processes with `O=` in their arguments, so it
    missed the orphaned sub-make children of the killed stage, and r1
    raced them (objtool could not open a deleted `cpufreq.o`; fixdep and
    `ar` missed files; objtool found `evgpeblk.o` already processed);
  - resume r2 started after the orphans had finished, and passed (first
    181 s; repro 36317 s of wall clock, because the laptop was suspended
    for about ten hours during it; `kernel.py`'s timeout uses the
    monotonic clock, which does not count suspend). `image-modules` and
    `collect` then passed.

  Both packages are byte-identical, and the first differs from the r25
  `vmlinux` in the 21 bytes above only, so the races left no trace in the
  output. The settle step should also match the sub-make and compiler
  children (a recipe gap for a follow-up).
- **Clean repro:** `compare_kernel.py` found both builds identical (13
  artifacts, the five modules among them, plus the full module ABI).
- **ABI negative controls:** `check_abi_mismatch.py` refused its four
  controls (stock kernel release, different operator config identity,
  missing and different module layout CRC).
- **Unchanged against `a3eac923`:** `config`, `System.map`,
  `Module.symvers`, `abi-negative.json`, `kernel.release`, `modules.*` and
  the five modules (same GNU build IDs). No export CRC changed, and no
  export was added or removed. SELinux stays an active LSM.
- **Changed:** `vmlinux`, `bzImage`, `build.json`, `abi.json` and
  `clean-repro.json`.

The Linux build trees, the prepared source and the daemon and window Go
build caches were deleted afterwards. The two kernel packages, the daemon
outputs, both window builds (`window-build/run-1`, `run-2`), the candidate
package and every receipt stay in the run root.

## Daemon and window rebuild (r25 method)

- **Daemon:** `daemon.py --prepare` and two forced builds ran from the
  worker worktree at `9994339` with no edits. GOMAXPROCS was 2, and the
  private module cache was seeded from the r25 cache, so nothing was
  downloaded. Both builds gave `024528a4…`, and the builder bindings name
  this worktree. The Go licence closure equals `a3eac923`'s.
- **Window artifacts:** the window test binary is `internal/controlledruntime`
  itself, and it links `internal/viewcontrol`. `GOOS=freebsd go list -test
  -deps ./internal/controlledruntime` lists both (receipt
  `window-go-list-test-deps.txt`), and hp4 changed both. So
  `build-fdescfs-window-artifacts.sh` ran with the explicit commit
  `9994339` (recipe `d0cdfa1d…`). The two builds were byte-identical, and
  the cross-built daemon equals `024528a4…`.
  - `fdescfs-namespace.c` and the toolchain are unchanged. The helper
    differs from the `e9ae706` build only in DWARF, which records the new
    out_dir as comp_dir. With `llvm-objcopy --strip-debug` both are the same
    `7872b1b4…` (receipt `helper-stripped.sha256`).
  - Afterwards only the recipe's default `CANDIDATE` and its header
    comment moved to `9994339`.
  - `docs/lab/zq3-fdescfs-window/pins.json` and `build-record.json` name
    the new bytes.

## Guest harnesses

All ran against the fresh tree, alongside the kernel builds and at low
priority. Results are in `suites.tsv` in the run root:
- with `--sanitize`: `transport`, `mount_hello`, `view`, `view --pressure`
  (compiles again since gp4, `66382b5`), `setattr`, `codec`, `cursor`,
  `policy`, `tag`, `root` and `reclaim`;
- without it: `capture`, `requests`, `huge`, and `dirty` (with
  `docs/spec/retained9p-fixtures/views.json`);
- `controlled` (120 s; `build.py`'s 60 s compile bound held, no retry was
  needed);
- `build.py --module-objects` (186 s) and `controlled_provider` against it,
  both with the run root as budget root: 73 PASS lines;
- `controlled_received --scenario both`: 21 scenarios. 16 passed in the
  combined run. Five failed at host load 15-24 and passed on one retry, run
  alone at load about 5 (`suites/cr-retry-*`, rows `cr-retry-*` in
  `suites.tsv`):
  - `interleaving`, `abandon`, `package` and `terminal-owners` hit the
    120 s `cc` bound of `controlled_received/build.py` for the C6 consumer
    (`subprocess.TimeoutExpired`);
  - `provider` failed a timing check ("WAIT did not yield after actual
    backend EAGAIN").

  The `package` retry ran hp4's
  `TestControlledReceivedActualHeldLeaseRetirement` beside gn4's
  `ControlLaneRetirement`, `TicketPackageRetirement`,
  `BusyRetirementRetry` and `PackageRetirement`; all passed.

All other suites passed on the first run, `view --pressure` included.

Not run:
- `controlled_crypto`: it needs prepared kernel headers;
- `mapped`: hp4 did not run it either.

## What was reused

- **Native exporter (88 files):** the native FreeBSD files, the three
  source-bundle inputs and the FreeBSD notices, each checked against the
  `a3eac923` manifest.
  - The three native proof records of the second r21 f3 build
    ([records](../xz3-20261125-r21/native-build-records.json)) were copied
    byte for byte from the r21 run root and compared with the collected
    originals. They bind `native_guard.py` `34ddf37c…`, which is unchanged.
  - The 11 packaged owned lib9p inputs equal the repository's.
- **Linux inputs (26 files):** patches 0001, 0002 and 0005 with
  `series`, and the 22 Linux and Monocypher notices. All 26 were
  checked against their current source.
- **What changed in the manifest (16 files):**
  - the five kernel files above;
  - the daemon, `freebsd/daemon-build.json`, `daemon-preparation.json` and
    `evidence/daemon-clean-repro.json`;
  - patches 0003 and 0004, `inputs/linux-source-manifest.json` and
    `inputs/linux-source-inventory.json`;
  - `inputs/repository-source.json` and `installation-proposal.json`;
  - the packaged copy of `tools/integration/artifacts/README.md`
    (`inputs/admission-recipes/README.md`), which the a3eac923 commit
    changed.

  Nothing was added or removed.
- **Previous candidate:** `a3eac923` still passes `package.py check` and a
  full `admit.py` under the `9994339` tooling (receipts `r25-container`,
  `r25-readmit`).
- **G3 init helper:** kept at `8c02b98f…`. It is not in the package, and
  `cmd/homelabfs` links neither of the two changed Go packages.

The kernel was prepared, and the daemon and window built, from the worker
worktree at `9994339`. The package was assembled there too. There were no
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
- refused as superseded: the r25 `a3eac923…` (R25-1), the r24 `c9500232…`
  (R24-1), the r23 `21c49a55…` (R23-1), the r22 `2c85b230…` (R22-1), the
  second r21 `f21450ec…` (R21-1) and every earlier candidate;
- still quarantined: `7e962627…`.

`directlaunch/argv.py` and `internal/runtimeprep/testdata/zq3-spec.json`
keep the exporter `8cadae40…`.

**Gates** (on go1.27.4; receipts in `receipts/gates/` in the run root):
- **Python suites passed:** `tools/lab` 249 tests (3 skipped; no
  `test_wait_lab_ready` flake this time), retained9p 445, directlaunch 26,
  artifacts 44.
- **lib9p `verify.sh` and `go vet ./...`:** passed.
- **`golangci-lint run ./...`:** the two staticcheck QF1001 (De Morgan)
  notes already on main (`internal/guestbridge/dispatch.go:226`,
  `internal/journal/segment/cursor.go:147`).
- **`go test -race -p 2 -timeout 60m ./...`:** ran once (private
  `GOCACHE`), alongside the kernel builds and guest suites at host load
  15-24. 29 packages passed, and no data race was reported.
  `internal/controlledruntime` hit the 60-minute package timeout while
  `TestZQ3T1CredentialsReplay` (44m33s, in its `advance false, late
  cleanup` subtest) and `TestZQ3T1ReplayDetectsNonCachelessGuest` were
  still running, so that package's remaining tests did not run under
  `-race`. This is the known T1 replay cost under `-race` on this host
  (r25: wall budget missed by 2-4 s at 2102 s for the package; hp4:
  5m26s/5m49s at load 20), made worse by the load.
- **Without `-race`, after the race run:**
  `TestZQ3T1CredentialsReplay` alone passed (334 s; the `advance true`
  subtests measured 1m16s and 1m17s of T1's 4-minute budget, the modelled
  22 s held). The whole `internal/controlledruntime` package then passed without `-race` (542 s, `ok`).
- This rebuild changes no Go file.

**What the r26 fill must do** (against the
[r25 fill](../../../../../docs/lab/results/20261127-zq3-r25-filled-plan/README.md)):
- **Candidate:** `80cd01bbfafdbf98361d0103d898c277af48350d9fe291d04da8a8f1f3789e30` at `/home/paul/tmp/claude/hfs-xz3-20261127-r26/package`
  everywhere the r25 fill named `a3eac923…`, the batch template's
  `candidate_manifest_sha256` and the bootstrap plan's
  `fd_attr_candidate_sha256` / `fd_attr_candidate_manifest` included.
- **Kernel plan:** regenerate the Rocky kernel plan for this manifest
  (`rocky_kernel.py plan`). The kernel is new bytes: build ID `899cc101…`,
  `bzImage` `8515701f…`, `vmlinux` `cefad6e3…`. `System.map` `d1ec01e8…`,
  `config`, `Module.symvers`, `modules.*` and the five modules keep their
  hashes. The bootstrap plan's `kernel_plan_sha256` and
  `fd_attr_kernel_plan` follow.
- **Daemon:** `024528a4…` is new bytes. The zq3-r26 Phase 4 step 1a
  (`--check-process-observer` under the bounded host guard on f3) must
  qualify it before anything else uses it. The fill pins `024528a4…`
  wherever the r25 fill pinned `43414d7f…`: the bootstrap
  `runtime_sha256`, the candidate file list and the guard's
  `--runtime-sha256`.
- **Window artifacts:** the pins in `docs/lab/zq3-fdescfs-window/pins.json`
  changed (`024528a4…`, `e47d07f2…`, `b0d563df…`). The fill regenerates
  its window pins and window plan from them.
- **Exporter:** unchanged. `Exporter.SHA256` and the argv record's exporter
  stay `8cadae40…`.
- **Workstation tools:** `hfs-runtime-prep` and the signer link
  `internal/viewcontrol` and `internal/controlledruntime`, which hp4
  changed. The fill must rebuild both at its own commit with go1.27.4, so
  the prep and signer hashes change.
- **Tool pins:** `controller.py` and `rocky_kernel.py` changed (new
  ADMITTED, FD_ATTR and SUPERSEDED), so their hashes in the fill (the
  bootstrap `kernel_script_sha256` included) change. `guest.py` is
  unchanged since the r25 fill (`e436ae72…`).
- **Facts and run IDs:** fresh read-only facts, run ID `zq3-r26`, a new
  argv record and UEFI vars, as every fill.
- **Findings:** the guest log lines are unchanged (hp4 changed only
  comments in the guest), so the r25 sheet's T1 rules carry over: the round
  line's `view retire <errno>`, and `charge still failed after the retry`
  is always a finding.
- **Approval steps:** booting the kernel, checking SELinux enforcing, and
  mounting through it remain zq3 steps that need explicit approval.
