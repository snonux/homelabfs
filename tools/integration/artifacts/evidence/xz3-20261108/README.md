# xz3 (2026-11-08): rebuilt controlled validation artifacts

This candidate supersedes the quarantined `7e962627…` package, whose
CGO-disabled FreeBSD daemon could never observe native processes (see the
[earlier record](../xz3/README.md)). It contains the current runtime sources
(s24 diagnostics, w24 descriptor admission, u24 lifecycle control, v24 lost
reply, n34 hardening, m34 lib9p socket fix, t24 pod kernel config). **This is a
build and offline-admission result only:** nothing was installed or booted, no
exporter or VM ran, and `installed`, `booted` and `native_accepted` stay false,
with publication null. Native/pod/PTE acceptance remains zq3, inside its
separately approved window.

| Pin | Value |
|---|---|
| Candidate | `/home/paul/tmp/claude/hfs-xz3-20261108/package-final` |
| Manifest SHA256 | `3f770a884978d3a776663a74083e733baf38665b8cba119022dd5432fb3296f1` (194 files) |
| Source checkout | `e0cca0c874f0027f050974ab5a54095d4af1c416` |
| Operator public key | `2f6d909c8bfa020c43dd460dcc03fd9d1fd78e03cdb61f51aa693f77de89a635` |
| Kernel | `6.1.190-hfs-xz3`, config from t24 |
| CGO daemon | `5359873799fa9ca28bb7aebc65449ceb839e07736c7d65eda1d6d6d6af2583a5` |

[Evidence](evidence.json) has the exact hashes, stage timings, recipe pins and
control names.

## Reproducibility

- **Kernel:** `kernel.py` ran its four stages, each in a user systemd scope
  (two CPUs, 2 GiB, no swap) with a 20-minute timeout, followed by
  `abi.py collect`. A repeat at the same canonical output path matched the
  first build exactly: identical compile inputs, all 13 artifacts and the full
  module ABI. The first comparison of the same-path repeat failed only
  because `abi.py collect` had not yet written `linux/kernel.release`. After
  running it on both packages, the comparison passed. An intermediate repeat at
  a *different* output path was refused by `compare_kernel.py`, because the
  output path is a recorded compile input. Its package is retained, not
  counted, under `kernel-second-package-refused-different-O` in the durable
  root; build trees remain in the session scratch directory.
- **Native FreeBSD** (f3, fresh root `/var/tmp/hfs-xz3-20261108-r1`, committed
  guard with the current Rocky PID): the pinned upstream archive re-captured to
  the same `044a42cc…` hash as the earlier TOFU capture. The owned-source
  inventory changed to `524a414a…` because of the m34 lib9p fix. Two clean
  builds matched in all 83 files. One stage, the first `library9p`, lost its
  local SSH session during a network switch. Its remote guard receipt (exit 0)
  was retrieved and recorded; the stage was not rerun.
- **Daemon:** `daemon.py` now builds with CGO against the pinned FreeBSD 15.1
  sysroot and cross wrapper. It records the clang resource headers, the tool
  library closures and the runtime loader closure. Two forced builds
  produced identical binaries.

## Native qualification of the exact daemon bytes

On f3 (FreeBSD 15.1-RELEASE-p2), the exact final binary `53598737…` ran its
read-only checks under the bounded host guard, with Rocky pinned. It was staged
with mode 0500 and one hard link. No configuration, exporter, VM or mount was
involved.

- `--check-process-observer` **passed**. It observed its complete native
  identity (numeric credentials, groups, incarnation, text vnode) and matched
  its own executable SHA256.
- `--check-descriptor-exec` **refused**, as expected, because the f3 host's
  `/dev/fd` is still devfs. Positive FD execution was qualified for the same
  sources on the hfs-test0 guest (w24, m34). Enabling fdescfs on the f3 host
  and rerunning there is a zq3 step that needs explicit approval.

Receipts are under `receipts/observer-*` in the durable root.

## Admission and controls

`assemble.py` sealed and admitted the package. A separate `admit.py` run
against the pinned manifest passed, and so did a re-admission of the durable
copy. `check_candidate.py` refused all 26 real-copy mutations for their
expected reasons, including:

- a real CGO0 daemon (the quarantined `480713c2…` binary);
- a record claiming CGO0, and the nocgo stub in the source closure;
- renamed observer symbols, a foreign loader, and extra or missing DT_NEEDED
  entries;
- sysroot hash drift, unrecorded clang headers or libraries, and a foreign
  wrapper;
- installed or authority claims, and a false rebuild witness.

The original package and the restored copy both passed again afterwards.

## Limits

Host-side clang headers and libraries are recorded by hash but are not
packaged, so admission checks that they were recorded, not their bytes. The
FreeBSD sysroot and runtime libraries remain measured external inputs and are
not redistributed. The proposal stays inert. Installing this candidate on f3
needs zq3's exact installation and critical-window approval, including the f3
host `/dev/fd` window transferred from w24.
