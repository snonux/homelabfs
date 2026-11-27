# xz3 r25 rebuild (2026-11-27): gn4 release tickets before the grant, view retirement under charge pressure

This candidate supersedes the [r24 rebuild](../xz3-20261127-r24/README.md)
(`c9500232…`, source `f6119d5`). zq3-r24 ran it on 2026-11-27.
[R24-1](../../../../../docs/lab/results/20261127-zq3-r24/README.md) stopped
T1: the 1024-group access child's `open(group-tail)` failed with ENOSPC
again. m0's control lane was 98% full (526456646 of 536870912 bytes), held
by 13 released but unretired VIEW_RELEASE tickets. The one request the 9n4
round freed returned ordinary-lane bytes only, so its retry failed too. The
failing charge was child 3's release ticket, charged after its second
VIEW_ACQUIRE had been GRANTED. Its failure stranded an active host lease,
and the km4 daemon's domain-wide `checkRetire` then answered every later
VIEW_RETIRE of m0 BLOCKED.

gn4 (merge `e9ae706`) changes two parts:
- **guest kernel:** patches 0003 and 0004 and `source.json`.
  `hfs_lease_acquire_ex` prepares the lease's control release ticket before
  the VIEW_ACQUIRE is sent, and `hfs_lease_own` takes it over, so a full
  control lane refuses the acquisition with no host lease.
  `hfs_retire_shared` also runs `hfs_view_retire` on the caller's session
  (the round line gains `view retire <errno>`), and a retry that fails
  again is logged;
- **daemon:** `Facts.checkRetire` (`internal/controlledruntime/facts.go`)
  refuses a VIEW_RETIRE for an active lease only when that lease carries
  the retiring family's credentials (`viewcontrol.CredentialDigest`,
  `internal/viewcontrol/retirement.go`). That matches the controller's
  `retirementBlocked`. Open reservations still refuse domain-wide.

The [linux9p README](../../../linux9p/README.md#release-tickets-charged-before-the-grant-view-tickets-retired-under-charge-pressure-gn4)
has the details.

So the guest kernel and the CGO daemon (with the window artifacts) were
rebuilt, as in the [r23 rebuild](../xz3-20261126-r23/README.md). Since
`21d8478`:
- no owned lib9p input changed;
- `go.mod`, `go.sum`, `daemon.py`, `daemon_cgo.py` and `native_guard.py`
  did not change.

So the native exporter is reused byte for byte from `c9500232` (the r16
method for the native part). It has run since zq3-r21 with `hfs_fdattr`
mounts.

**Build and offline admission only:** nothing ran on f3, Rocky or any VM,
and no SSH was used. Nothing was installed or booted, so `installed`,
`booted` and `native_accepted` stay false, and publication stays null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261127-r25/package` |
| Manifest SHA256 | `a3eac923d2b8713a3252b1b95cfe0294a6326bbc298de9d67e81b7ed8ed5e5b9` (194 files) |
| Source checkout and repository baseline | `e9ae70640a3de428c1626886469f6f2e0fc69d61` (main after the gn4 merge). The daemon was rebuilt there, so there is no `inputs/repository-changes.patch` |
| Kernel | `6.1.190-hfs-xz3`: `linux/bzImage` `ee650e63a86ac0b60ca4fd564a095cf61ba333adbd773a91c0471e296d37cd56`, `linux/vmlinux` `c416af7e45f64fef661e736d87258dd9531e2012574e5023664129280d726731` (GNU build ID `8c70915f500e10b7e15f87f0d47e03470bd72bfa`), `linux/config` `692ed3fa…` (unchanged), `System.map` `d1ec01e8b86ef9fe954494095d866eef7e92e192f6e09e5f9a85c9e17ab12cb0`, `Module.symvers` `b68cf94aea6ab1f7f8599a1ba229951d4be1b1c4d8272dc4b9ce20f964828f17` (unchanged from `c9500232`) |
| Modules | the five modules are byte-identical to `c9500232` (same GNU build IDs; `virtio_net` `072995182e1a…`) |
| CGO daemon | `43414d7fff7e1ee396d1e7ff7dfb27be7b4586e6a6270850ad20e106344ace17` (from `e9ae706`, go1.27.4, GNU build ID `d8289ecb…`; two forced builds and the window build gave the same bytes; not yet checked on f3) |
| Native exporter | `freebsd/bhyve` `8cadae403dc3f0c7add38f16ec8fae1d00b17c1802f00411896e96fa083aa4d4` (second r21 f3 build, reused) |
| Window test binary / helper | `29e9e160096c2dd422559105c0f6e8e11511d334e514d340a36948f39f037ebe` / `2202e6b5a7977889ba8dcc31769ae273bb99506a2052a4edb33fd3a78c9d66cb` (from `e9ae706`, rebuilt) |

What the files hold:
- [Evidence](evidence.json): hashes, stage timings, source-suite results,
  receipts and control names.
- [kernel-clean-repro.json](kernel-clean-repro.json): the `compare_kernel.py`
  record (also packaged as `linux/clean-repro.json`), the stage table and
  the ABI negative-control receipt.
- [reused-files.json](reused-files.json): the reused Linux and native files,
  the files that came out identical and the 17 that changed. One file was
  removed: `inputs/repository-changes.patch`.

## Kernel rebuild (r24 method)

- **Source:** a fresh `prepare.py` tree from the cached pinned archive
  `4f3c00db…` (`/home/paul/tmp/claude/c64-config/linux-pinned.tar.gz`;
  nothing downloaded), prepared at `e9ae706`.
- **Two builds at the same O path:** every `kernel.py` stage ran under a
  user systemd scope (two CPUs, 2 GiB, no swap, the r17 40-minute bound).
  The first build tree was moved aside before the repeat. `abi.py collect`
  ran on both packages.
- **No timeouts:** `vmlinux` took 990 s and 986 s, within `kernel.py`'s
  1190 s bound per `make`. The resume path never ran.
- **Clean repro:** `compare_kernel.py` found both builds identical (13
  artifacts, the five modules among them, plus the full module ABI).
- **ABI negative controls:** `check_abi_mismatch.py` refused its four
  controls (stock kernel release, different operator config identity,
  missing and different module layout CRC).
- **Unchanged against `c9500232`:** `config`, `Module.symvers`,
  `abi-negative.json`, `kernel.release`, `modules.*` and the five modules
  (same GNU build IDs). No export CRC changed, and no export was added or
  removed (`Module.symvers` is byte-identical). SELinux stays an active
  LSM.
- **Changed:** `vmlinux`, `bzImage`, `System.map`, `build.json`, `abi.json`
  and `clean-repro.json`.

The Linux build trees, the prepared source and the daemon and window Go
build caches were deleted afterwards. The two kernel packages, the daemon
outputs, both window builds (`window-build/run-1`, `run-2`), the candidate
package and every receipt stay in the run root.

## Daemon and window rebuild (r23 method)

- **Daemon:** `daemon.py --prepare` and two forced builds ran from the
  worker worktree at `e9ae706` with no edits. GOMAXPROCS was 2, and the
  private module cache was seeded from the r23 cache, so nothing was
  downloaded. Both builds gave `43414d7f…`, and the builder bindings name
  this worktree. The Go licence closure equals `c9500232`'s.
- **Window artifacts:** the window test binary is `internal/controlledruntime`
  itself, and it links `internal/viewcontrol`. `go list -test -deps` with
  `GOOS=freebsd` lists both, and gn4 changed both. So
  `build-fdescfs-window-artifacts.sh` ran with the explicit commit
  `e9ae706` (recipe `2b1bd4a9…`). The two builds were byte-identical, and
  the cross-built daemon equals `43414d7f…`.
  - `fdescfs-namespace.c` and the toolchain are unchanged. The helper
    differs from the `21d8478` build only in DWARF, which records the new
    out_dir as comp_dir. With `llvm-objcopy --strip-debug` it is the same
    `7872b1b4…`.
  - Afterwards only the recipe's default `CANDIDATE` and its header
    comment moved to `e9ae706`.
  - `docs/lab/zq3-fdescfs-window/pins.json` and `build-record.json` name
    the new bytes.

## Guest harnesses

All ran against the fresh tree, alongside the kernel builds and at low
priority. Results are in `suites.tsv` in the run root:
- with `--sanitize`: `transport`, `mount_hello`, `view`, `setattr`,
  `codec`, `cursor`, `policy`, `tag`, `root` and `reclaim`;
- without it: `capture`, `requests`, `huge`, and `dirty` (with
  `docs/spec/retained9p-fixtures/views.json`; it compiles again since
  `6490546`);
- `controlled` (94 s; `build.py`'s 60 s compile bound held, no retry was needed);
- `build.py --module-objects` (136 s) and `controlled_provider` against it,
  both with the run root as budget root: 73 PASS lines;
- `controlled_received --scenario both`: all 21 scenarios (2036 s). Among
  them is `package`, which now runs gn4's
  `TestControlledReceivedActualControlLaneRetirement` and the r24 round in
  `TicketPackageRetirement`.

All of them passed except `view --pressure`:
- **`view --pressure` did not compile.** It compiles the actual
  `hfs_lease_acquire_ex` and keeps `hfs_lease_own` as a fixture
  (`tests/pressure/test.c` and `compat_extra.h`). gn4 changed
  `hfs_lease_own` to `(cred, description, release, lease)`: it takes over
  the release ticket that `hfs_lease_acquire_ex` now prepares (`control`)
  before the VIEW_ACQUIRE. The fixture still has the old
  `(session, cred, description, lease)` signature, so
  `-Werror=incompatible-pointer-types` stops the compile. This is a
  harness defect on main since gn4, which ran `view` but not
  `view --pressure`. It is not a kernel defect.
- **A private copy passed.** The `view`, `pressure`, `codec` and `common`
  harnesses were copied to `pressure-probe/` in the run root. Three
  fixtures changed there: `hfs_lease_own` takes the new signature and
  takes over `*release`; `hfs_view_ticket_prepare` hands out the release
  credit for a `control` ticket; `hfs_view_ticket_put` accepts the `NULL`
  that `hfs_lease_acquire_ex` puts once a lease took the ticket. That copy
  compiled and passed against the same prepared tree with `--sanitize`:
  the PASS line, exit 0. The repository harness is unchanged; it needs a
  follow-up fix.

Not run:
- `controlled_crypto`: it needs prepared kernel headers;
- `mapped`: gn4 did not run it either.

## What was reused

- **Native exporter (88 files):** the native FreeBSD files, the three
  source-bundle inputs and the FreeBSD notices, each checked against the
  `c9500232` manifest.
  - The three native proof records of the second r21 f3 build
    ([records](../xz3-20261125-r21/native-build-records.json)) were copied
    byte for byte from the r21 run root and compared with the collected
    originals. They bind `native_guard.py` `34ddf37c…`, which is unchanged.
  - The 11 packaged owned lib9p inputs equal the repository's.
- **Linux inputs (26 files):** patches 0001, 0002 and 0005 with
  `series`, and the 22 Linux and Monocypher notices. All 26 were
  checked against their current source.
- **What changed in the manifest (17 files):**
  - the six kernel files above;
  - the daemon, `freebsd/daemon-build.json`, `daemon-preparation.json` and
    `evidence/daemon-clean-repro.json`;
  - patches 0003 and 0004, `inputs/linux-source-manifest.json` and
    `inputs/linux-source-inventory.json`;
  - `inputs/repository-source.json` and `installation-proposal.json`;
  - the packaged copy of `tools/integration/artifacts/README.md`
    (`inputs/admission-recipes/README.md`), which the c9500232 commit
    changed.
- **Previous candidate:** `c9500232` still passes `package.py check` and a
  full `admit.py` under the `e9ae706` tooling (receipts `r24-container`,
  `r24-readmit`).
- **G3 init helper:** kept at `8c02b98f…`. It is not in the package, and
  `cmd/homelabfs` links neither of the two changed Go packages.

The kernel was prepared, and the daemon and window built, from the worker
worktree at `e9ae706`. The package was assembled there too. There were no
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
- refused as superseded: the r24 `c9500232…` (R24-1), the r23 `21c49a55…`
  (R23-1), the r22 `2c85b230…` (R22-1), the second r21 `f21450ec…` (R21-1)
  and every earlier candidate;
- still quarantined: `7e962627…`.

`directlaunch/argv.py` and `internal/runtimeprep/testdata/zq3-spec.json`
keep the exporter `8cadae40…`.

**Gates** (on go1.27.4):
- **Python suites passed:**
  - `tools/lab`: 249 tests, 3 skipped. The first run failed once in
    `test_wait_lab_ready` (a fake-SSH timeout path answered `uncertain`
    while `golangci-lint` ran beside it). The module passed alone, and the
    whole suite passed when run again;
  - retained9p: 445 tests;
  - directlaunch: 26 tests;
  - artifacts: 44 tests.
- **lib9p `verify.sh` and `go vet ./...`:** passed.
- **`go test -race -p 2 -timeout 4h ./...`:** ran once (3796 s, private
  `GOCACHE`). 29 packages passed, and no data race was reported.
  `internal/controlledruntime` (2102 s) failed one test,
  `TestZQ3T1CredentialsReplay`: its two `advance true` subtests went over
  T1's 240 s measured-time budget by 2 s and 4 s (modelled 22 s plus 3m40s
  and 3m42s of this host's work under `-race`). Every recall, advance and
  self-admission check in them held; only that wall-clock budget failed.
  This is known: gn4's own final check saw it, and base `ac8127f` fails it
  the same way under `-race`. That test alone without `-race` passed (260
  s; the advance subtests measured 1m4s and 1m7s). This rebuild changes no
  Go file. Receipts: `receipts/gates/go-test-race.txt` and
  `go-test-t1-norace.txt` in the run root.
- **`golangci-lint run ./...`:** reports the two staticcheck QF1001 (De
  Morgan) notes already on main (`internal/guestbridge/dispatch.go:226`,
  `internal/journal/segment/cursor.go:147`).

**What the r25 fill must do:**
- **Kernel plan:** the guest kernel is new, so the Rocky kernel plan must be
  regenerated for this manifest. It has build ID `8c70915f…` and new
  `bzImage` and `System.map` hashes. `config`, `Module.symvers`,
  `modules.*` and the five modules keep their hashes.
- **Daemon:** `43414d7f…` is new bytes. The zq3-r25 Phase 4 step 1a
  (`--check-process-observer` under the bounded host guard on f3) must
  qualify it before anything else uses it. The fill pins `43414d7f…`
  wherever the r24 fill pinned `43238fe9…`, the guard's `--runtime-sha256`
  included.
- **Window artifacts:** the pins in `docs/lab/zq3-fdescfs-window/pins.json`
  changed (`43414d7f…`, `2202e6b5…`, `29e9e160…`). The fill regenerates
  its window pins and fills the plan from them.
- **Exporter:** unchanged. `Exporter.SHA256` stays `8cadae40…`.
- **`hfs_fdattr`:** allowed for this candidate, never implied. The fill must
  name `a3eac923…` as `fd_attr_candidate_sha256` and point at this
  package's `manifest.json`.
- **Workstation tools:** `hfs-runtime-prep` and the signer link
  `internal/viewcontrol` and `internal/controlledruntime`, which gn4
  changed. The fill must rebuild both at its own commit with go1.27.4.
- **Guest tool and findings:** the fill stages `guest.py` from its own
  commit. The new round line (`view retire <errno>`) and the
  `charge still failed after the retry` line are what T1's finding rules
  should name.
- **Approval steps:** booting the kernel, checking SELinux enforcing, and
  mounting through it remain zq3 steps that need explicit approval.
