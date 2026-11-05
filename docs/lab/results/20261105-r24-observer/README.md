# FreeBSD process observer qualification, 2026-11-05

The actual CGO daemon passed its read-only `--check-process-observer` command
on f3, using the captured FreeBSD base C ABI. It observed its complete native
identity and matched its protected executable bytes. Actual CGO self-observation
and CGO-disabled prelaunch refusal tests also passed. The separate C boundary
fixture passed two positive and 59 negative cases; those injected sysctl values
are boundary tests, not native observation evidence.

[evidence.json](evidence.json) records the exact source, compiler, sysroot,
binary, stage receipts and native output. Full inputs, logs and failed attempts
remain under the recorded retained root. Two initial cross-build attempts failed
on compiler-wrapper flag handling; corrected compilation and native execution
passed. No successful result is attributed to either failed attempt.

Only disposable observer binaries and temporary refusal-test files were staged.
No runtime configuration, exporter, VM lifecycle, FUSE mount, installation or
filesystem workload ran. Rocky PID 6444 and its birth identity were unchanged;
`/dev/vmm` contained only Rocky before and after. Publication remains null.

This qualifies the recorded daemon bytes for the observer capability only.
The quarantined xz3 artifact remains unchanged and ineligible. The final selected
daemon must be rebuilt and qualified again after later runtime changes; this
receipt cannot qualify different bytes or establish native runtime readiness.

The merged source `c520332` also passed the full Go suite, full build, vet,
errcheck, lint, and FreeBSD static/CGO cross-builds. [Merged checks](merged-checks.json)
retain every result. The broad local checks used a separately recorded 10GiB
aggregate workspace allowance; earlier small-cache/storage stops remain failures
of those attempts. Native resource constraints and VM state were unchanged.
Fresh R1 found no blocking or nonblocking issue.
