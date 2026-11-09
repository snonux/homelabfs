# xz3 z54 daemon rebuild (2026-11-09): every VIEW_POLL answered

This candidate supersedes the [w54 candidate](../xz3-20261109d/README.md)
(`89c928d5…`, source `ff866c5`) only because its CGO daemon predates z54. In
lab run zq3-r4 (R4-1) the f44 daemon `174949c1…` left a VIEW_POLL that arrived
before the domain was acquired unanswered, so the first mount blocked in the
transport. Task z54 (merge `a20aca1`) answers every VIEW_POLL and VIEW_ACK
(EMPTY before acquire, ACCEPTED, STALE or BLOCKED for an ack) and never
strands a transport cell, in `internal/viewcontrol` and `internal/guestbridge`.
The f44 method was used again: rebuild the daemon and the window artifacts,
reuse everything else. **Build and offline admission only:** nothing was
installed or booted, and `installed`, `booted` and `native_accepted` stay
false with publication null.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261109e/package` |
| Manifest SHA256 | `2e0412a62d1c1f0aa0b29865c521bf3df7a925ea5ef293428fa9b38b647d8f2f` (194 files) |
| Source checkout | `a20aca192911e380f3863897e1e6774b1fc46e0e` (also the repository baseline, the daemon's build revision) |
| CGO daemon | `39c92d016f17b4c480c3c3bfac266f284123413ea4d95697b935d80bdf98a521` (10,768,088 bytes, GNU build ID `5d99b0da…`) |
| Kernel | `6.1.190-hfs-xz3`, w54, unchanged: `linux/bzImage` `5c46f218…`, `linux/vmlinux` `086f6b90…` |
| Window test binary / helper | `97fb5c07…` / `cc92bcde…` (from `a20aca1`) |

[Evidence](evidence.json) records hashes, receipts and control names.

## What was rebuilt and what was reused

- **Rebuilt:** the CGO daemon with `daemon.py` from `a20aca1`, a forced build
  into the package and a second forced build into a separate package. Both
  gave the same bytes, and the window build reproduced them a third and
  fourth time from a `git archive` export. Against y54 the daemon's source
  closure changed in `controlledruntime/facts.go`, `guestbridge/endpoint.go`
  and `transport_ledger.go` and `viewcontrol/{admission,controller,recall}.go`,
  and gained `viewcontrol/ack_reply.go`. The Go and cgo toolchain, sysroot
  capture, runtime closure, `go.sum` and the four Go licence files are
  unchanged.
- **Reused byte for byte (136 files):** the 83 native FreeBSD files, five
  native source-bundle and notice files, the w54 kernel, modules and their
  records (18 files, including `build.json`, `abi.json` and
  `clean-repro.json`), the two Linux source records, patches 0001–0005 with
  `series`, and 22 Linux notices. `git diff ff866c5 a20aca1` touches nothing in
  `tools/integration` except evidence documents. Each file was checked against
  the y54 manifest. The patches, `source.json`, the source inventory and the
  notices were also checked against the current repository and the prepared
  w54 source. [reused-files.json](reused-files.json) lists them.
- **Dropped:** `inputs/repository-changes.patch`. The baseline is now
  `a20aca1`, which already contains the w54 `source.json`, so the diff
  `assemble.py` records is empty. y54 recorded that diff against `defd974`.
- **Window artifacts:** rebuilt with `build-fdescfs-window-artifacts.sh` from
  `a20aca1`. Both builds were byte-identical. `controlledruntime.test` changed
  because `controlledruntime` and the z54 packages it links changed.
  `fdescfs-namespace.c` is unchanged, and the helper differs from the f44
  `a773f3ef…` only in DWARF. `.debug_str` holds the new output directory as
  `comp_dir`, which reorders the merged string pool, and `.debug_info` string
  offsets follow. After `llvm-objcopy --strip-debug` both builds are identical
  (`07c9f5a1…`).
- **G3 init helper:** kept at `8c02b98f…`. It is not in the package.
  `cmd/homelabfs` imports `guestbridge` but not `viewcontrol`, and
  `node-init`/`volume-init` never reach the changed endpoint and ledger code.

The build and assembly ran from the worker worktree at `a20aca1`. All 3937
tracked files are byte-identical to the main checkout, and the main checkout
has no untracked files under `cmd`, `internal`, `pkg`, `tools/integration` or
`tools/lab`. The worker sandbox refuses git in the main checkout. Nothing
reused depends on a path. The new daemon builder bindings name the worktree
path. Every other binding is a hash.

## Admission and checks

`assemble.py` sealed and admitted the package, and an independent `admit.py`
run against the pinned manifest passed. `check_candidate.py` refused all 26
real-copy mutations for their expected reasons, including the real CGO0
daemon `480713c2…`. The original and the restored copy passed again.

The tooling now admits only this manifest. The w54 `89c928d5…`, r54
`5eb42c50…`, f44 `b203799e…` and earlier `3f770a88…` are refused as
superseded, and `7e962627…` stays quarantined. **The f3 observer
qualification does not carry over:** these daemon bytes are new and still
need `--check-process-observer` under the bounded host guard on f3.
