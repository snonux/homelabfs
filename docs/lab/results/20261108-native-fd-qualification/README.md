# Native descriptor-execution qualification and merged gates (2026-11-08)

Resumed after the homelab IP migration. This record closes the remaining
verification of tasks **s24** (bounded exporter diagnostics) and **w24**
(descriptor-execution admission). It does **not** qualify the real daemon,
runtime READY, FUSE, bhyve or the 9p base gate (zq3), and it supplies no final
xz3 artifact. `native_runtime_ready:false`, publication `null`.

[Summary](summary.json) · [merged gate steps](gates/steps.tsv) ·
[artifact hashes](gates/artifacts.sha256) · [guest fdescfs window](gates/fdescfs-guest-window.txt) · [VM lifecycle](gates/vm-lifecycle.txt) ·
[receipts](receipts/) · [guard and driver](tools/)

## Post-migration access

IQ lists f3 at `10.20.0.133` (the old `192.168.1.133` is retained). The `f3`
name pin (ED25519 `SHA256:VZLFFSJZ9K7iOBQ/cFehASFzCfIOn0TKFD1L89ziL5g`) is the
same key as the pre-migration address entry; every connection used
`StrictHostKeyChecking=yes`, nothing was scanned or newly accepted. The guests
sit on the f3-internal `172.31.253.0/24` bridge, unchanged by the migration,
and use the repository's pinned [known_hosts](../../f3-known_hosts).

## Merged local gates on `c216cae`

`c216cae` differs from `d417bb3` only in the pause handoff JSON, so the
`d417bb3` full Go suite pass (452.953 s) still covers the code and was not
repeated. The previously missing gates all passed sequentially with
`-p=1 GOMAXPROCS=2 GOGC=50` (2 min 15 s, peak RSS 316 MiB): gofmt, Linux build
and vet, FreeBSD vet with CGO off and on, golangci-lint (0 issues), errcheck,
`mage build` (static Linux/FreeBSD), and FreeBSD CGO cross-builds of the C
diagnostic child, the controlledruntime test binary, `hfs-controlled-runtime`
and the `fdescfs-namespace` helper. The worktree stayed clean
([before](gates/status) and [after](gates/status-after) are empty).

## Native stages

All stages ran the same cross-built test binary
(`c441f772…`) under the bounded [guard](tools/native_guard.py): one CPU,
512 MiB aggregate RSS, a fresh 0700 `/var/tmp/hfs-2026…` root, no install,
boot or VM action. The guard is derived from the reviewed S24 guard; its host
profile takes the current Rocky PID (6826 became historical with the migration
reboot) and its guest profile pins the hfs-test hostname. The host stage ran
an earlier copy of the guard (staged SHA256 `3e1c325e…`, see
`receipts/host-r1/staging.json`) that lacked only the later `--max-seconds`
option (fixed 90 s bound); the committed copy is the one used for guest stages.

| Stage | Where | Result |
|---|---|---|
| host-r1 | f3 host, Rocky pinned (pid 6473) | PASS: `TestDiagnosticComponentParentExit` (default-negative + native), `TestDescriptorExecNativeUnsupported` (no skip), `TestFreeBSDObserverReadOnly` |
| guest0-pre-negative | hfs-test0, devfs `/dev/fd` | PASS: same three tests; refusal before child/pin/observer effects |
| guest0-positive | hfs-test0, fdescfs `nodup` | PASS: `TestDescriptorExecNativeQualification`, `TestActualExporterDiagnosticsParentExit` (full `Supervisor.Start` with actual FD exec, default-negative + native), component and observer tests; unsupported test skips by design |
| guest0-full | hfs-test0, fdescfs `nodup` | FAIL (45 pass): environment-only, see below |
| guest0-post-negative | hfs-test0 after restore | Unsupported PASS; qualification refuses with `devfs`/UNSUPPORTED, as intended |

The full-launch path that failed as S24 r1/r2 on 2026-11-05
(`fork/exec /dev/fd/4: no such file or directory`) now passes on actual
FreeBSD once a legitimate fdescfs `nodup` mount exists. The admitted report
was `fdescfs`, FSID `5900ff01:00000059`, flags `0`, supported.

### Guest window

Paul allowed stopping Rocky and starting the test VMs. Treating a `kldload` and
`/dev/fd` mount inside that disposable guest as covered by this, rather than as
a SYSTEM host action, is the agent's interpretation and was flagged for Paul's
confirmation. The window was run by hand with intent/state records, not through
`tools/lab/fdescfs-window.py` (which targets the f3 host). `vms.sh stop-rocky`
(ACPI) and `start-lab` ran; inside the disposable **hfs-test0** only
(15.1-RELEASE `n283562-96841ea08dcf`) the stock module
(`4d9dfea0…`) was loaded and `mount -t fdescfs -o nodup fdescfs /dev/fd` made.
Intent records were written before the module load and the mount, and
pre/post/restored state records around them; the restore line records the
result (no separate intent before unmount). Restore
was a normal `umount /dev/fd` and `kldunload fdescfs` (the run loaded it);
the devfs identity of `/dev/fd` matched the pre-setup record. Then
`stop-lab` and `start-rocky`; Rocky answered pinned SSH on its stock kernel.
**The f3 host `/dev/fd` was never mounted and no f3 module was loaded.**

### Full-package failures (not product failures)

1. The relocated test binary runs from its run root, so cwd-relative
   repository inputs (`../../tools/integration/lib9p/...` C sources,
   `testdata/guest-artifacts.json`, `process.go`) do not exist.
2. The producer observer fixture writes its fake SSH primitive with a
   Linux-only `#!/usr/bin/python3` shebang. FreeBSD has no such interpreter
   (the same run's `guest_observer_test.go` fixtures skip with "needs python3"
   for the same path), so `execve(/dev/fd/3)` returns ENOENT for the missing
   interpreter. Only the `Supervisor.Start` `/dev/fd/4` exec is qualified above;
   the observer's `/dev/fd/3` path uses the same kernel mechanism and admission
   check but has not yet run successfully on native FreeBSD.

Task **l34** covers a faithful native full-package run (staged source tree and
portable fixture interpreter).

## Limits

Qualification used the guest's 15.1-RELEASE kernel, not f3's
`15.1-RELEASE-p2` host kernel (different stock module bytes). Running the real
daemon on the f3 host still needs the reviewed
[host window](../../Descriptor-execution-window.md), with explicit critical
authority, inside the zq3 window once Paul approves it (annotated on zq3),
followed by rerunning
`TestDescriptorExecNativeQualification` there. Preserved resources:
`/var/tmp/hfs-20261108-host-r1` on f3 and `/var/tmp/hfs-20261108-guest0-r1`
on hfs-test0.
