# xz3 xd4 rebuild (2026-11-19): zq3-r13 guest kernel and exporter fixes

This candidate supersedes the [xc4 rebuild](../xz3-20261118-r13/README.md)
(`e9c77e5b…`, source `b7d107b`). In zq3-r13 (R13-1) m0's `umount` hung in D
after the host runtime had stopped. The exporter's endpoint check failed and
it sent no reply, bhyve completed the request with an empty buffer that Linux
virtio drops, and the guest waited forever for a 9p reply. In zq3-r12 the
guest had also reported every exporter ERROR as `-EPROTO` and kept retrying
on a blocked session. Two fixes were merged (`942614a`), one in the guest
kernel and one in the native exporter, so both were rebuilt (task xd4, the
prerequisite for zq3-r14):

- **Guest kernel:** oc4 changed `tools/integration/linux9p` patches
  0001-0004 and `source.json`.
  - `hfs_bridge_error` maps the exporter's ERROR reason: RETIRED_CELL gives
    `-ESTALE`, the new UNAVAILABLE gives `-EHOSTDOWN`, and everything else
    stays `-EPROTO`. The session logs the raw reason.
  - Transport retries, VIEW_QUERY polls and CALL->QUERY polls stop once the
    session is blocked or its transport has failed. Teardown traffic is
    exempt, unless the transport has failed.
  - `p9_client_rpc_v` waits at most 30 s for an hfs reply. A client that
    still owns timed-out requests is kept rather than freed.
- **Native exporter:** pc4 changed `tools/integration/lib9p` `hfs9p.c` and
  `hfs9p.h`.
  - A failed result read maps EHOSTDOWN to ERROR UNAVAILABLE (5).
  - A well-formed exchange whose endpoint check fails gets ERROR
    UNAVAILABLE instead of no reply.

The daemon did not change: `git diff b7d107b 942614a -- internal cmd go.mod
go.sum` touches only `internal/guestbridge/README.md`. So the xc4 daemon
`c959b708…` and the window artifacts are reused byte for byte, as 964 did.
The y64 method was used for the kernel (a fresh prepared source, two builds
at the same O path) and for the exporter (a native build on f3, two clean
builds in one root). **Only the native build ran on a host.** Nothing was
installed or booted, and no VM, mount, module or Rocky was touched.
`installed`, `booted` and `native_accepted` stay false, and publication
stays null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261119-r14/package` |
| Manifest SHA256 | `3a7131ddc0bfad96762421122027d63aa5c736ace1ef61f4cb9eff14b5379f6f` (195 files) |
| Source checkout | `942614af75db2f83855ba50b85a67c9c7accf763` |
| Repository baseline | `b7d107b073a017280b107168042ad6372c59dbbe` (the reused daemon's build revision; `inputs/repository-changes.patch` `400c8749…` records the tracked oc4/pc4 diff since then) |
| Kernel | `6.1.190-hfs-xz3`: `linux/bzImage` `e0daa57eaa51d727a86417c8a82b29fc1143f066ca0b806657ae61ffcccc849f`, `linux/vmlinux` `d2690b4257f4454c7b1e81250c9c28bca7763bdb32bdb46b6ae09c35cb6c7f8c` (GNU build ID `9b167690f6cd2f0538ecb6df06ab76121736eacb`), `linux/config` `28fe4a8e…` (unchanged), `System.map` `dd88dfcb2ab0b57a7e4f5cae9cc972460ec1be1b2f6fe8f2b255be39ccffe622` |
| Modules | the five modules are byte-identical to `e9c77e5b` (same GNU build IDs; `virtio_net` `39de7e93…`) |
| Native exporter | `freebsd/bhyve` `2fd7e32f5cc8762ae0ff4d5ec6c046d7d49b95c97e4b9150dd17b2ac52c39fd3` (GNU build ID `43f2ae41…`), owned-source inventory `35714a7f…` |
| CGO daemon | `c959b708f93876a5a4c620f33e64587bc0235dc4cf994de2154eb26710c4bc9b` (xc4, from `b7d107b`, reused; it passed the f3 observer check in zq3-r13) |
| Window test binary / helper | `8194ebe0…` / `8c43fd06…` (xc4, from `b7d107b`, reused) |

[Evidence](evidence.json) records hashes, stage timings, suites and control
names. [native-build-records.json](native-build-records.json) has the f3
staging hashes, every guard receipt, the clean-repro comparison and the f3
facts before and after. [kernel-clean-repro.json](kernel-clean-repro.json)
holds the `compare_kernel.py` record (also packaged as
`linux/clean-repro.json`), the stage table and the ABI negative-control
receipt. [reused-files.json](reused-files.json) lists the 31 reused files,
the files that came out identical, and the 27 that changed.

## Native build on f3

The build reused the y64 coordinator (`native-coord.py`, `collect-native.py`,
`native-verify-run.py`), with only the root, the worktree and the path of the
pinned host key changed. SSH used `-F /dev/null`, `BatchMode=yes`,
`StrictHostKeyChecking=yes`, and the repository's `docs/lab/f3-host-known_hosts`
with `HostKeyAlias=f3`. The native recipes are unchanged since y64.

- **Rocky pin, checked read-only first:** bhyve PID 46923, lstart
  `Thu Nov 19 08:24:51 2026` UTC, `rocky.conf` `c101adf1…`, `memory=12G`,
  `/dev/vmm` only `rocky`. The facts after the build were the same.
- **Root and guard:** one fresh private 0700 root,
  `/var/tmp/hfs-xz3-20261119-r14-r1`, left in place as evidence. Every stage
  ran under `native_guard.py` (one CPU, 512 MiB aggregate RSS, 20 minutes per
  stage, 8 GiB disk), pinned to that Rocky incarnation. The peak guarded RSS
  was 413 MiB (the lib9p native tests); the build stages peaked at 299 MiB.
- **Source:** the pinned archive `044a42cc…` and its capture record were
  copied from the 2026-11-08 root and re-hashed, not downloaded.
  `native_source.py extract` applied the unchanged patch `df00a6b0…` and the
  owned sources. Only `hfs9p.c` (`f823182c…`) and `hfs9p.h` (`d7dedcbf…`)
  changed. The new inventory is `35714a7f…`.
- **Two clean builds in one root:** `preflight`, `library9p`,
  `libraryvmmapi`, `bhyve` and `collect`. After the first build, its package,
  preflight and completion records were sealed (seal `e237833c…`) and moved
  aside, and the objects were removed. The comparison found all 83 files and
  both preflight records equal.
- **One retried second build:** the first attempt at the second build
  stopped in its `bhyve` stage. The cause was the guard, not the build: its
  disk walk `lstat()`ed a make `.tmp` object that make had just renamed, and
  the guard failed closed and stopped the stage. `reset-second` (also under
  the guard) checked that the sealed first build was intact, then removed
  that attempt's objects, preflight and two completion records. The whole
  second build then ran again from scratch as `build-second-retry-*`. Both
  attempts' receipts are kept.
- **What changed:** against `e9c77e5b`, only `bhyve`, `lib9p.a`,
  `lib9p_pie.a`, `freebsd/build.json` and the three source-bundle records
  changed. `bhyve-slirp-helper`, the `libvmmapi` archives, the 76
  `share/bhyve` files and both FreeBSD notices came out byte-identical.
- **lib9p native tests:** `native-verify.sh` ran on f3 under the same guard
  (stage `lib9p-native-verify`) and passed. It covers the production API,
  backend (including the pc4 `unavailable_tests`), ledger and supervisor
  tests.

## What else was rebuilt and what was reused

- **Kernel, rebuilt:** from a fresh `prepare.py` tree, using the cached pinned
  archive `4f3c00db…` (nothing downloaded). The new source inventory is
  `276aba05…`.
  - `config`, `abi-negative.json`, `kernel.release`, `modules.*` and the
    five modules (same GNU build IDs) are byte-identical to `e9c77e5b`.
  - `vmlinux`, `bzImage`, `System.map`, `build.json`, `abi.json` and
    `clean-repro.json` changed.
  - `Module.symvers` changed in the CRCs of seven hfs exports (`hfs_call`,
    `hfs_control`, `hfs_retire`, `hfs_request_allocate`,
    `hfs_request_resolve_gap`, `hfs_session_destroy`,
    `v9fs_hfs_register_provider`). It also gained the export
    `hfs_control_flagged`. No module imports them.
- **Daemon, reused byte for byte:** the binary, `daemon-build.json`,
  `daemon-preparation.json`, the four Go licences and the xc4
  `daemon-clean-repro.json`.
  - The xc4 builder bindings name the xc4 worktree, where the daemon was
    really built. They were re-keyed to the xd4 worktree, but only after
    checking that `daemon.py` and `daemon_cgo.py` there hash to the bound
    values. This is the 964 deviation. The original binding files are kept
    in the run root under `daemon-binding-origin/`.
  - Because `assemble.py` takes the repository baseline from the daemon's
    build record, the baseline stays `b7d107b`. `inputs/repository-changes.patch`
    records the tracked oc4/pc4 diff since then.
- **Window artifacts, reused:** they follow the daemon source, and the daemon
  is the `b7d107b` build. `docs/lab/zq3-fdescfs-window/pins.json` is
  unchanged; the build record gained a note.
- **31 files reused** from `e9c77e5b`, each checked against its manifest:
  - the daemon files above (7);
  - patch 0005 with `series`;
  - the 22 Linux notices.

  24 of them were also checked against their current source: the
  repository patches, and the notices in the freshly prepared Linux tree.
  Before the reuse, `e9c77e5b` passed `package.py check` and a full
  `admit.py` under the `942614a` tooling.
- **What changed in the manifest (27 files):**
  - the seven kernel files above;
  - the seven native files above;
  - patches 0001-0004, `inputs/linux-source-manifest.json` and
    `inputs/linux-source-inventory.json`;
  - the owned `hfs9p.c` and `hfs9p.h`, and `evidence/native-clean-repro.json`;
  - `inputs/repository-source.json`, `inputs/repository-changes.patch` (new)
    and `installation-proposal.json`, which pins the kernel and the exporter;
  - the packaged copy of `tools/integration/artifacts/README.md`
    (`inputs/admission-recipes/README.md`), which xc4 changed.
- **G3 init helper:** kept at `8c02b98f…`. It is not in the package, and no
  Go source changed.
- **Workstation tools:** no Go source changed, so `hfs-runtime-prep` and the
  signer keep their inputs. `tools/lab/directlaunch/argv.py` and the example
  spec `internal/runtimeprep/testdata/zq3-spec.json` now pin the exporter
  `2fd7e32f…`, because `TestCheckArgvRecordAgreesWithG4` requires the
  generated runtime config's exporter digest to match the argv record.

The build and the assembly ran from the xd4 worktree at `942614a`. It had no
edits under `tools/integration` during the builds and the assembly (the pin
and doc edits came afterwards).

## Reproducibility and tests

The kernel stages ran twice under a user systemd scope (two CPUs, 2 GiB, no
swap, a 20-minute timeout per stage) at the same canonical output path. The
first build tree was moved aside before the repeat. `vmlinux` took 1054 s and
962 s, slower than before because the source suites ran alongside the first
build. `abi.py collect` ran on both packages. `compare_kernel.py` passed on
the first attempt (13 artifacts plus the full module ABI), and
`check_abi_mismatch.py` refused its four negative controls.

Against the prepared source, these suites passed:
- `mount_hello --sanitize`: 23 cases;
- `tag --sanitize`: 5 cases;
- `transport --sanitize`;
- `dirty`, with `docs/spec/retained9p-fixtures/views.json`;
- the `controlled_received` scenarios `terminal-smoke`, `cleanup` and
  `exit-cleanup`;
- `view` and `view --pressure`, each plain and with `--sanitize`.

## Admission and checks

`assemble.py` sealed and admitted the package. An independent `admit.py` run
against the pinned manifest passed. `check_candidate.py` refused all 26
real-copy mutations for their expected reasons, including the real CGO0
daemon `480713c2…`. The original and the restored copy passed again.

The tooling now admits only this manifest:
- refused as superseded: the xc4 `e9c77e5b…`, the 5a4 `150deb29…`, the b94
  `3b128cc6…`, the r84 `4712e479…`, the l84 `e585206a…`, the y64
  `867ede36…`, the c64+d64 `66b5bb2b…`, 964 `e01fab21…`, z54 `2e0412a6…`,
  w54 `89c928d5…`, r54 `5eb42c50…`, f44 `b203799e…` and the earlier
  `3f770a88…`;
- still quarantined: `7e962627…`.

**What the next run must do again:**
- The exporter `2fd7e32f…` has never run. The filled zq3-r14 spec must pin
  it, replacing `ef8f17de…`.
- The guest kernel is new. The Rocky kernel plan must be regenerated for
  this manifest: build ID `9b167690…` and the new `bzImage` and `System.map`
  hashes. `config`, `modules.*` and the five modules keep their hashes;
  `Module.symvers` changed.
- The daemon bytes are unchanged and passed the f3 observer check in
  zq3-r13. Every run still repeats Phase 4 step 1a.
- Booting the kernel, checking SELinux enforcing, and mounting through it
  remain zq3 steps that need explicit approval.
