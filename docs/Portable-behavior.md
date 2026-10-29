# Portable single-node behavior runner

`hfs-behavior` is a static Go binary for Linux and FreeBSD. It runs bounded
synthetic syscall checks inside a fresh run-owned directory beneath an existing
disposable directory or mount. It writes JSON and JUnit XML outside that target.
It creates no mount, starts no VM, changes no accounts or system configuration,
and imports nothing from the throwaway `tools/poc` module.

Build without running tests:

```
go run github.com/magefile/mage behaviorBuild
```

The binaries are `bin/linux-amd64/hfs-behavior` and
`bin/freebsd-amd64/hfs-behavior`, built with `CGO_ENABLED=0`. FreeBSD compilation
is a portability check; it provides no evidence of native FreeBSD behavior.

After obtaining the approval required for the intended test environment, run a
plain-directory baseline using an existing disposable target. Keep the binary
and both new report paths outside the target:

```
bin/linux-amd64/hfs-behavior -path /absolute/disposable/target \
  -json /absolute/evidence/baseline.json \
  -junit /absolute/evidence/baseline.xml
```

Exit 0 means no reported failures. Skips remain explicit in both formats and
never establish full P01–P04 acceptance. Exit 1 means a behavioral or cleanup
failure; exit 2 means CLI, startup or report I/O failure. Existing report files
are never overwritten, and output destinations are reserved before any test I/O.
Successful runs remove only their own test directory. Failed runs preserve their
data; each report records the exact directory. Three helper processes have fixed
credentials, run no further programs and use private pipes. Commands normally
have a five-second deadline and the standalone run has a 45-second deadline.
Cleanup waits are bounded; failure to stop a helper prevents owned unmount.
All mutating peers exit and revoke before the sole remaining main helper closes
its retained lock fd and removes successful run data through a bounded RPC. No
peer can replace names during the idempotent cleanup retries. The main helper
then exits and revokes before normal unmount. Close is never retried.

## Cases and limits

| Family | Observable checks |
|---|---|
| P01 | Create/exclusive-create rejection; two processes append 16 distinct 32-byte records each, checking exact identities/counts/content; rename and replacement preserving old open identities; hard-link identity/count; unlink-open; truncate shrink and zero-filled extension |
| P02 | Umask, chmod/chown read-back, setgid directory inheritance; root-only fixed numeric users, supplementary group access/denial, sticky deletion denial and chmod after a cached denial |
| P03 | Separate-process fcntl and flock contention, interrupted blocking fcntl, lock cleanup on process exit |
| P04 | Ordinary writes and synchronous open, native fsync/fdatasync and directory fsync, shared writable mmap/MS_SYNC followed by fsync and reopen/read-back |
| Lifecycle | Owned normal unmount, exact volume/generation/session rejection after unmount, empty fallback directory inspection |

Every helper opens the root once, then accesses the run directory through pinned
directory fds and `openat`-family syscalls. A declared HomelabFS target additionally
requires native FUSE type and exact identity verification before creating data.
After unmount, the identity gate refuses to supply a writable root, and the
fallback directory must remain empty. Plain-directory baselines do not require
a FUSE identity. Reports include the declared profile and any capabilities
supplied by the owning adapter; the standalone binary does not guess capabilities.

When an additional supplementary group is available, the unprivileged setgid
case assigns that group to the directory so inheritance differs from the
creator's primary group. With only the primary group available this is a weaker
same-group check. Root uses synthetic group 20002. The multiuser case requires
root and pre-spawned users 20001 (supplementary group 20002) and 20003. It skips
with a reason when unprivileged. No real user account is created. The target's
ancestors and helper executable must permit these synthetic users to traverse
and execute; standalone mode never changes ancestors to make that possible.

Standalone mode does not own the target lifecycle and explicitly skips normal
unmount and mount-loss checks. Its interrupted-lock probe observes a bounded
wait under contention, kills only its own helper, and verifies the holder's lock
still conflicts. It cannot observe a FUSE daemon's cancellation callback.

These are live local syscall tests. They do not establish power-loss durability,
replay/failover semantics, cache fencing, multiple independent mounts or guests,
9p compatibility, arbitrary changing credentials, or normal workload publication.
The native integration and deployment gates remain separate.

## Controlled HomelabFS profiles

The implemented adapter has two deliberately different disposable profiles.
`hfs-direct` supports append and rejects mmap. `hfs-buffered` supports mmap and
rejects `O_APPEND` before effects. Each report explicitly skips its complementary
case and names the profile supplying that coverage. Both reports must be assessed
together; a single profile cannot establish combined append/mmap compatibility.

The owned Linux fixture starts every trusted helper **before** mounting through
`Participants.Start`, then seals publication. Mmap touches occur only in helpers,
outside the Go daemon runtime. Actual service `WaitLock` and `CancelLock` events
prove the blocked request was admitted and cancelled when its helper exited.
All helpers stop before normal unmount; failed resources are retained. The
root-only multiuser fixture uses the buffered profile's explicit allow_other
admission, with private backing roots. It skips under an unprivileged UID.

After authorization for a bounded local synthetic run:

```
GOMAXPROCS=2 HOMELABFS_FUSE_INTEGRATION=1 \
  go test ./internal/fuseadapter -run '^TestLinuxPortableBehavior$' \
  -v -count=1 -timeout 45s
```

The fixture logs a retained evidence directory containing separate
`hfs-direct.json`/`.xml` and `hfs-buffered.json`/`.xml` reports. Set
`HOMELABFS_BEHAVIOR_REPORTS` to an existing external evidence directory to choose
the destination; filenames must be unused. Each profile has a 30-second overall
deadline, eight-second case deadlines and at most 4 MiB logical synthetic data.
No lab, SSH, VM, installation or production action is part of this command.

The standalone binary can also declare `-profile hfs-direct` or `hfs-buffered`
with `-volume UUID -generation UUID -session UUID`. This verifies identity and
reports real failures; it does not enroll itself in an already sealed controlled
adapter. Such a mount normally denies external helpers. Use the owned fixture
for the current adapter's controlled-client validation.

During development the first direct append probe used 128 records and exceeded
its five-second driver deadline. Its retained payload contained 45 complete
records, showing forward progress. The probe was reduced to 32 records/1 KiB
with an eight-second fixture budget; this is a bounded semantics check, without
a latency acceptance target. That failed fixture and reports remain at
`/home/paul/tmp/codex/hfs-behavior-fixture-291995611` and
`/home/paul/tmp/codex/hfs-behavior-reports-4265753933`. A subsequent two-profile
run passed in 12.78 seconds, with evidence at
`/home/paul/tmp/codex/hfs-behavior-reports-426310589`; later verification strengthens
the append oracle with distinct writer/sequence identities.

Go runtime preemption also exposed native `EINTR` during ordinary direct-profile
calls. The focused `TestLinuxBehaviorEINTRBoundary` probe sends SIGURG to the
exact helper syscall thread and holds the adapter response. Before service
admission, the raw append returns EINTR and the mounted file remains empty.
After admission, cancellation still yields an OK service completion, a successful
raw write, and exactly one record read through the verified mount. The probe
passed in 0.23 seconds. Adapter source likewise waits admitted completions and
rejects cancelled contexts before enqueue; ordinary mutations never manufacture
an interrupted completion after applying effects. This supports three-attempt
EINTR retries for ordinary no-effect syscalls; the unique-record oracle still
detects any duplicate/drop. Close is never retried. Raw probe writes intentionally
perform no retry.

The final two-profile run passed in 8.62 seconds (direct 5.73 seconds, buffered
2.89 seconds) with distinct writer/sequence records, distinct supplementary-group
setgid inheritance, both fcntl/flock released by peer exit and bounded sole-helper
cleanup after peer revocation. Exact identity and
adapter-declared capabilities appear in both report formats. Final evidence is
`/home/paul/tmp/codex/hfs-behavior-reports-3131739363`; successful fixture data was
removed after normal unmount. Root-only multiuser cases skipped under UID 1001.
Intermediate EINTR failures remain preserved in fixture directories ending
`3556459090`, `3413783378`, and `1351975484`, with report directories ending
`1596023622`, `1427602864`, and `976951173` respectively. The failed mounts were
normally unmounted in every run; no abort, force or lazy unmount was used.
Two early plain-directory runs also retained their data after cleanup reported
ENOTEMPTY: `/home/paul/tmp/codex/hfs-behavior-baseline-2408481637` and
`/home/paul/tmp/codex/hfs-behavior-baseline-3973454659`. Cleanup now explicitly
rewinds the pinned directory before enumeration; subsequent baseline runs pass.

The static standalone Linux binary also passed against a fresh directory on the
existing `/dev/shm` tmpfs, with JSON/JUnit parsed independently and its test
directory removed. Evidence remains at
`/home/paul/tmp/codex/hfs-behavior-cli-jcr3eh4q`. Its root-only multiuser and
externally owned lifecycle checks explicitly skipped. Scoped race tests, vet,
errcheck and static FreeBSD amd64 production/test-binary cross-compilation passed.
These checks do not substitute for running the binary on a FreeBSD kernel.

Review found and corrected two gaps in the original oracles. After each rename,
the runner now verifies source pathname absence and opens the destination to
check the source's device/inode identity and expected bytes; replacement also
keeps the old destination fd's contents intact. Real-directory regressions omit
the replacement or substitute a different inode carrying identical bytes and
require the actual verifier to reject both. Append verification now consumes
complete 32-byte records and checks every newline, including the final byte.
Real-file regressions reject final/interior delimiter corruption, changed payload
bytes and a duplicate record paired with a dropped record at the same total size.
