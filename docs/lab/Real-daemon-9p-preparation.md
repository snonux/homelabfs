# Real-daemon 9p gate: implementation prerequisites

Assessment at source revision `801a3302659d4d2b92cfc5e55bfa4ed3d56ea01f`,
2026-10-30. This is local source assessment and a proposed implementation order,
not live compatibility evidence, artifact admission, or an execution plan.
Task `zq3` remains incomplete and continues to block `4r3`.

Required contracts: [kernel policy](../spec/Kernel-policy.md),
[deployment gates](../Deployment-tests.md),
[durability](../spec/Durability.md#mmap-and-guest-buffering),
[platform interfaces](../Platform-interfaces.md),
[deployment readiness](../Deployment-readiness.md),
[test automation](../Test-automation.md),
[lab operations](../Lab-operations.md), [replication safety](../Lab-replication.md)
and [historical PoC runbook](../PoC-runbook.md).

## Why the existing daemon cannot run the required profile yet

`internal/daemoncli/cli.go` rejects ordinary `mount`/`serve` publication and
requires `--disposable-validation --self-client`. Its credential provider admits
only its own executable. The behavior entry point uses
`fuseadapter.Participants`, which admits the daemon and fixed-credential helpers
it directly spawns before mounting. FreeBSD participant admission compares the
request PID with the retained child PID and verifies its exact fixed IDs/groups
and start time (`participants_freebsd.go`). An independently managed bhyve
process is not an enrolled helper.

`fuseadapter.Adapter.identity` requires credentials to match the FUSE header
UID/GID/PID. These are host kernel requests; a guest helper PID cannot simply be
inserted into this provider. Enrolling the bhyve PID or guessing groups by guest
UID does not establish request-bound guest credentials, process-exit semantics,
descriptor ownership, or independent lock owners. The actual deployed
bhyve/lib9p credential behavior must be identified before selecting a solution.
This assessment does not assert the behavior of an uninspected deployed release.

`Adapter.Capabilities` explicitly leaves normal publication, arbitrary
credentials and writeback unsupported. Buffered native validation supports
controlled helpers; it does not authorize arbitrary guest callers. Native
`OpenMount` verifies a native FUSE connection and immutable root xattr, then
uses a pinned directory descriptor. Its native statfs check is not a guest 9p
publication verifier. K04 requires a separate verified publication boundary
through the complete sharing path.

The historical `check-9p-poc.sh` uses the passthrough executable, unpinned SSH
aliases, root probes and an existing PoC export. It lacks plan-bound admission,
the complete matrix below, durable reconciliation and a guest publication guard.
Copying it with the daemon filename substituted would fail admission or weaken
the daemon security contract. Keep it as historical PoC tooling.

The IQ infrastructure index records plain `rocky` as a development VM outside
the k3s cluster. This is planning context, not freshly inspected state. Pod
tests need an explicitly selected isolated runtime/publication environment;
existing production r-nodes cannot substitute. A duplicate mount in one guest
is not an independently connected client. Any additional client needs a
separately admitted target and memory budget.

## Small prerequisite task breakdown

These are proposed task descriptions for the project owner to create and order.
They neither change task state nor authorize transport changes.

1. **Specify the deployed 9p credential, lock and sync boundary.** References:
   kernel policy sections Complete artifact manifest and Validation ownership;
   Platform interfaces / Unix permission enforcement boundary; FUSE-adapter /
   Credentials and lock owners; Deployment-tests / Early compatibility gates;
   Durability / mmap and guest buffering. Bind actual f3 base/kernel/FUSE,
   bhyve/lib9p executable/library bytes and matching source to actual Rocky
   kernel/modules. Trace attach/access modes, UID/GID and complete supplementary
   groups, host syscall credentials, guest process/client and lock owner
   identity, release/exit, fsync/fdatasync/directory-fsync and mmap MS_SYNC.
   Acceptance: a reviewed mapping table names what reaches HomelabFS and how
   it is authenticated; identifies unsupported semantics explicitly; selects
   an implementable disposable publication contract without daemon-identity or
   guessed-group fallback. If the selected stack cannot preserve required
   semantics, stop that profile and propose an explicit transport/design task.
   Source review of another release is not deployed proof. Do not patch lib9p,
   vendor a transport, or add a guest protocol under this investigation task.

2. **Implement bounded disposable 9p publication and isolated fixture setup.**
   Depends on the reviewed boundary above. References: Configuration /
   disposable behavior profiles; FUSE-adapter / credential and cached-write
   admission; Kernel-policy / Target records and serving refusal;
   Deployment-tests / K04; Deployment-readiness / scope and completion.
   Acceptance: explicit validation-only CLI/configuration, exact exporter and
   volume/generation/session admission, request-bound credentials and distinct
   lock domains, guest readiness guard and retained-descriptor behavior,
   rejected plain-directory fallback, normal-publication denial preserved.
   Provide non-root UID/GID/supplementary-group and denial fixtures; explicit
   namespace mapping; an isolated pod runtime with pinned image and pod
   securityContext; SELinux enforcing with retained policy and audit evidence.
   Namespaces, mounts, images, devices and synthetic paths are run-owned; no
   production PVs/consumers or existing k3s workload changes. Include local
   fail-closed checks before any separately reviewed live setup.

3. **Implement and execute the complete zq3 compatibility harness.** Depends
   on the first two prerequisites and an exact provisional artifact/target
   record. References: Test-automation / Runner contract; Lab-operations;
   Lab-replication; Deployment-tests / Early compatibility gates and K04;
   Kernel-policy / validation scope; Durability / mmap and guest buffering.
   Acceptance: the bounded matrix below passes against the real daemon,
   with machine-readable results and original logs outside the test target,
   per-case deadlines and I/O budgets, normal cleanup and original-state
   restoration. Missing prerequisites or required cases yield incomplete/
   nonzero status, never skipped-as-passed acceptance. Keep native integration
   and transport results separate. Record follow-up failures; do not complete
   zq3 or release dependent replication work on a partial pass.

## Minimal harness shape after the prerequisites

Use one local controller with explicit plan/execute/status/cleanup operations,
one host driver and one Rocky probe. Reuse existing runner concepts and bounded
behavior helpers where their security/transport contracts actually apply; do
not import `tools/poc/` or build another general framework. Prepare all drivers
and local checks before an actual run. Shell drivers must pass shellcheck.

The controller pins host and guest SSH keys through f3, source/artifact hashes,
VM identity/configuration and process incarnation, exact running kernel and
module set, export tag/root, mount options, namespace/pod configuration and
SELinux policy. It records the selected admission limitations, restoration
state, approval source and exact run-owned resource inventory. Before mutation
it rechecks VM memory/lifecycle constraints and refuses drift. A historical
installed ELRepo kernel is not proof of the current running kernel.

| Case | Required observation |
|---|---|
| Credentials | Numeric UID/GID, complete supplementary groups, same UID with different groups, root/non-root allow and denial, pod securityContext and supported namespace mapping under SELinux enforcing; correlate guest operations with host authorization |
| Sync | Trace ordinary write, O_SYNC/O_DSYNC, fsync, fdatasync, directory fsync, writable mmap MS_SYNC and later fsync to covering daemon journal barriers; state the guest-visible buffering boundary and avoid power-loss claims |
| Locks | Contended POSIX/flock operations in one mount, across pods sharing it, duplicate guest mounts and independently connected clients; positive local exclusion plus refusal of unauthorized concurrent volume serving; include close/exit and descriptor lifetime |
| Cache | Data, attributes, permissions and negative lookup freshness; already-open files and existing mmap pages across daemon restart and generation change; access revocation must not be inferred from readiness alone |
| Restart/recovery | Preserve synthetic journal/oracle; restart daemon; inspect old bhyve handles; determine and automate smallest safe workload restart, normal remount or graceful VM restart; verify committed data and new identity |
| K04 | Lose host FUSE and guest 9p mounts separately; ordinary directory remains; both new and retained consumer access must fail safely with no underlying fallback writes, then recover only after exact mount identity admission |

Each case has an external sequence/reply oracle, bounded data sizes, fixed
seeds and deadlines; preserve raw errno, kernel/module/daemon identities and
timings. Record unsupported/not-run cases explicitly. A lock stub on upstream
main does not establish installed behavior. Current upstream documentation
describes `cache=mmap` as read-ahead plus writeback and `cache=none` as disabling
mmap; inspect the selected 6.1 kernel source and verify its actual behavior
instead of applying current documentation as version-specific proof.
[Linux 9p documentation](https://docs.kernel.org/filesystems/9p.html).

On failure stop dependent probes, collect evidence, close run-owned clients,
unmount normally and preserve busy/failed resources. No forced unmount, forced
Rocky termination, disk overwrite, received-sink modification or production
cleanup is an available recovery step. A lifecycle change, extra VM, package
installation or policy change must be inside the concrete reviewed run plan.
