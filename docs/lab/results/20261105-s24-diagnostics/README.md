# S24 bounded exporter diagnostics evidence

This records the diagnostic transport component only. `native_accepted:false`,
`native_runtime_ready:false`, and Publication remains null. Full native
`Supervisor.Start`, runtime READY, guest/FUSE behavior and the real 9p base gate
are not qualified here. W24, U24, V24, xz3 and zq3 retain those separate gates.

## Current local candidate

[Result](result.json), [source hashes](source-bindings.json) and
[review record](review.json) identify the R1-fixed candidate. All 50 recorded
Go/C/header inputs were compared directly with both current local guard bindings.

| Check | Result | Guard duration | Peak process-tree RSS |
|---|---|---:|---:|
| [Focused race/coverage](receipts/focused-race-r1-fixes-7.json) | PASS; 31.9% affected-package statements | 33.667 s | 212,049,920 bytes |
| [Build/vet/errcheck/format](receipts/static-checks-r1-fixes-7.json) | PASS | 18.166 s | 10,903,552 bytes |

The focused tests include real supervised child output, overflow, management
Close, independent process exit/channel EOF, partial-write/EIO and short/nil
outcomes through real files, continued draining, final status WriteAt/EIO and
successful WriteAt followed by Sync/EIO. They assert exact stored bytes,
incomplete saved status, best-effort invalidation and sticky errors returned
by Close after descriptor closure. The numeric coverage is for these selected
package tests, not the full suite. Exact commands and output are retained under
`receipts/`, `logs/` and [static-checks.py](static-checks.py).

R1 found two blockers: terminal status error retention/invalidation and missing
positive partial-write coverage. R2 confirmed both resolved with no new blocking
finding. Merged checks and a native recheck of the corrected bytes remain for
root; this record does not claim they ran. Root's subsequent read-only tool-closure capture was refused by the native
guard before its command ran because a foreign development VM was running
alongside Rocky. No current-byte diagnostic recheck was attempted. The VM was
left untouched; that recheck remains deferred until a safe profile.

## Historical actual FreeBSD component proof

[Native v5 receipt](receipts/native-parent-exit-v5.json) and
[transcript](logs/native-parent-exit-v5.stdout) show both `default-negative` and
`native` passing on actual FreeBSD. Native execution took 0.331 s with peak RSS
128,008,192 bytes. The native plan bounded CPU to one, RSS to 512 MiB and its
private run root to 128 MiB. No VM, mount, module or installation action ran.

The fixture used actual production log construction, attachment and lifecycle
hooks with test-only private pinned-path C execution. After its real Go owner
was reaped, the C child with default/unblocked SIGPIPE survived actual fd1/fd2
writes returning EPIPE. Its negative control reported its reaped writer's actual
SIGPIPE death. The saved status had to remain incomplete after owner death.
This supplies no `Supervisor.Start`, protected FD-exec or runtime READY proof.

These are **pre-R1-fix Go binary bytes**, preserved in
[artifact bindings](artifact-bindings.json) and
[native-v5 source bindings](native-v5-source-bindings.json). The current source
record explicitly identifies the two changed Go files; the C fixture is
unchanged. Do not reuse the historical native binary qualification for the
corrected or merged daemon. Staged file/library hashes, exact cross-build
commands and native plan are retained alongside the receipts.

## Failures retained

| Attempt | Recorded outcome |
|---|---|
| Cross-build v1 | Fixture POSIX flags hid the actual BSD `SO_NOSIGPIPE` declaration. Corrected flags preserve the real header namespace. |
| Native full launch r1/r2 | FAILED before child startup; r2 exposes `/dev/fd/4: no such file or directory`. Missing fdescfs/FD-exec capability remains W24 qualification work. |
| Native component r3 | Guard refused stale Rocky process admission after an external reboot; no test ran. Collection also failed because test output did not exist. |
| Native component r4 | FAILED: C exit 82. The test wrongly expected normalized `1`; the selected kernel returns the enabled option mask `2048`. No product socket check was weakened. |
| Covered local v6 | FAILED two exact-stderr expectations because covered Go helpers emitted their 54-byte GOCOVERDIR notice. V7 asserts those actual bytes without changing production environment. |

[History index](history/index.json), raw failed receipts, transcripts, compiler
output and read-only r4 resource inspection remain under `history/`. Original
larger working/native directories remain referenced there. Failures are not
reclassified as passes. The selected primary kernel source URLs/SHA256 values
in the artifact record explain socket buffer accounting and the option mask.

## Evidence limits

Byte/memory accounting does not prove physical placement or storage latency.
A storage failure cannot guarantee durable invalidation; live sticky errors and
returned Close errors must disqualify uncertain saved success. Diagnostic
`Complete` describes captured output, not child success or filesystem
acceptance. A nonzero child exit can have complete diagnostics and still fail
its test. Linux signal-ignore fixtures are explicit OS boundaries and cannot
establish FreeBSD behavior. Final source/artifact admission and full native
startup remain separate from this component record.

[Manifest](manifest.json) records the copied evidence hashes. Full guard binding
indexes and original collection wrappers remain in the referenced task root.
