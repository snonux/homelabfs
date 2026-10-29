# Linux FUSE behavioral CI

The `linux-fuse` jobs in [the root workflow](../.github/workflows/ci.yml) run on
separate disposable Ubuntu 24.04 GitHub-hosted VMs as the runner user and as
root. They install `fuse3` on those ephemeral VMs, require accessible `/dev/fuse`,
build `homelabfs`, `hfs-behavior` and a static `fuseadapter.test` binary, then run
`TestLinuxPortableBehavior` and `TestLinuxBehaviorEINTRBoundary` against actual
kernel FUSE mounts. Missing kernel support is a failure, including when a Go
test would otherwise skip.

This uses the same portable behavior library as `hfs-behavior`, through helpers
spawned and credential-enrolled before publication by the owning test fixture.
The standalone CLI cannot enroll new processes into this sealed controlled
mount. CI therefore executes the real kernel fixture binary, not the standalone
CLI against an inaccessible daemon. It proves restricted single-node local
service behavior; normal daemon publication, arbitrary mutable credentials,
distributed durability, failover, native FreeBSD and 9p remain separate gates.

| Profile | Required coverage | Explicit expected skips |
|---|---|---|
| `hfs-direct`, runner and root | P01 append/namespace, P02 metadata, P03 locks, P04 sync, interruption and normal unmount/fallback rejection | mmap; multiuser uses buffered companion |
| `hfs-buffered`, runner | P01 namespace, P02 metadata, P03 locks, P04 shared mmap/MS_SYNC/fsync, interruption and normal unmount/fallback rejection | append; root-only multiuser |
| `hfs-buffered`, root | Same buffered cases plus actual fixed UID/GID/supplementary-group, sticky-directory and permission-cache checks | append |

Direct I/O rejects mmap. Buffered validation rejects append before any effects
and restricts writable descriptions to one fixed credential set per object.
Neither profile enables writeback-cache negotiation, normal publication or
arbitrary credentials. The root job enables `allow_other` only on the synthetic
buffered mount and uses numeric users 20001/20003 and supplementary group 20002;
it creates no system accounts or persistent FUSE configuration. The runner job
records the root-only case as skipped, never as passed.

Each profile has a 30-second fixture deadline; the selected kernel test binary
has a 120-second timeout, and each CI job has a 15-minute limit including setup.
The fixture caps logical synthetic data at 4 MiB. It stops owned helpers before
normal unmount and removes successful fixture contents only. Failed resources
remain under the logged `/tmp/hfs-linux-ci.*` root for that runner's remaining
lifetime. The wrapper never forces/lazily unmounts, aborts connections or removes
backing resources. CI cancellation or an unrecoverable kernel hang can prevent
normal cleanup; the disposable VM is subsequently discarded.

The `linux-fuse-{runner,root}-RUN_ID-ATTEMPT` artifacts retain both profile JSON
and JUnit files, the raw test log, Go test events, binary hash, kernel/Go/FUSE
versions, execution identity, fixture path and post-run mount inventory for
14 days. Failed or timed-out runs still attempt upload; fixture data and live
mount contents are not traversed for collection. The validator requires exact
P01–P04/lifecycle case sets, declared capabilities and mount identity, matching
JSON/JUnit statuses, the expected profile skips only, actual root multiuser
success, all selected kernel tests passing and a successful wrapper exit.
Missing/partial evidence fails verification.

The workflow permits manual dispatch and pushes only to the dedicated
`codex/linux-fuse-ci-20261029` verification branch. The initial remote has no
registered workflows, so the reviewed scoped branch push registers and runs
this workflow without publishing to `main`. A future dispatch needs the workflow
present on the default branch; branch protection changes are unnecessary.
These are routine isolated CI tests authorized for the current implementation
work. This workflow has no lab, VM lifecycle, deployment or production stages;
their separate approval boundaries still apply. A green branch run plus its
artifacts is required before task `zr3` can be completed. Workflow preparation
and local validator checks alone are not a passing CI result.
