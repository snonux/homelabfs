# Stage 2: live f3s end-to-end smoke tests

[Stage 1 native integration](Integration-tests.md) · [Approval and automation](Test-automation.md) · [Deployment compatibility](Deployment-tests.md)

## Entry conditions

Stage 1 must pass for the candidate release. Deploy the validated HomelabFS artifacts to the real FreeBSD f3s hosts under a separately authorized rollout. Then prepare a live smoke plan and obtain **explicit approval for Stage 2**. Deployment finishing does not start tests automatically. Run this stage through scripts, with no LLM in the execution loop.

Test from each participating Rocky VM using its actual deployed kernel, transport, permissions, and mount configuration:

```text
Rocky test process → deployed 9p mount → bhyve → host HomelabFS FUSE → ZFS + configured replicas
```

This complements the native integration suite: it catches wrong packages, unsupported guest kernels, misconfigured exports, credential issues, and publication/readiness mistakes on the real infrastructure. It does not replace the lab's fault-injection tests.

## Isolation and preflight

Provision dedicated run-owned smoke volumes using the real HomelabFS management path. Do not place temporary files in an application's existing volume. Give each participating host a local-primary test volume so normal test I/O follows the intended locality model. Use unique run IDs, quotas, numeric test users/groups, and bounded byte/operation/time limits declared in the approved plan.

Before writing, scripts verify mount identity end to end, expected volume ID, active owner/epoch, replica readiness, installed versions, kernel capabilities, and space/headroom. A directory existing or `mountpoint` returning true is not sufficient. Fail before writes if the guest path could reach a raw backing tree, empty fallback directory, incorrect volume, or unready owner.

The runner stores its oracle and report outside the smoke volumes. No default reboot, forced unmount, daemon kill, network partition, physical-host shutdown, emergency-mode change, or production-volume ownership transfer is part of live smoke testing.

## Scripted smoke matrix

| ID | Check | Expected result |
|---|---|---|
| L01 | Inventory each real FreeBSD host and Rocky guest; verify the expected FUSE and 9p path | Approved versions and identities match; unavailable mounts block testing |
| L02 | Create, write, fsync, reopen, read and checksum in each dedicated local-primary volume | Correct bytes; bounded completion time; no premature readiness |
| L03 | mkdir/rmdir, rename replacement, truncate, hard link, unlink-open | Required filesystem semantics preserved through the deployed path |
| L04 | UID/GID, mode changes, permitted and denied access using the selected test identities | Credentials and permissions preserved; unauthorized access denied |
| L05 | Two test processes contend on flock/fcntl; writable mmap plus flush/sync | Supported lock domain and mmap behavior hold on the actual guest mount |
| L06 | Small SQLite DELETE/WAL transactions with FULL sync, close/reopen and integrity check | Committed state and integrity checks pass; no production database touched |
| L07 | Observe received/durable/applied positions for smoke-volume writes and wait for async catch-up | Sync acceptance follows the contract; every configured replica reaches the expected checkpoint within the test budget |
| L08 | Verify replica state through a supported inspection/checkpoint mechanism | Expected content and metadata at a common committed point; do not mount passive replicas for ordinary stale reads |
| L09 | Run a small bounded mix of independent file I/O on the smoke volumes and inspect health metrics | No errors, unexpected cluster degradation, unbounded lag, or resource growth; not a saturating benchmark |
| L10 | Remove run-owned smoke objects/volumes through supported APIs and verify cleanup | No smoke residue; production volume state and service health remain unaffected |

For L07/L08, define the necessary administrative observability API during implementation. Missing replication evidence is a test failure or explicit unsupported gate, not permission to assume replicas are current. Poll with deadlines rather than arbitrary sleeps.

Where Kubernetes publication is deployed, an optional explicitly selected smoke profile runs a disposable pod using only a smoke PVC, tests the published path, and removes its own resources. The basic Stage 2 suite runs directly inside the real Rocky VMs and does not depend on changing any application deployment.

## Completion and escalation

Scripts capture a per-guest/per-volume result, versions, checksums, operation history, replication positions, health before/after, and cleanup status. Success requires all selected checks and cleanup to pass. A failure stops migration to that path and preserves evidence; do not automatically repair live storage or restart workloads.

Automatic failover under node crashes, stale-mount recovery, emergency mode, and destructive Kubernetes lifecycle scenarios belong to Stage 1 where applicable or to separately approved isolated deployment exercises. Routine live smoke tests demonstrate that the deployed end-to-end path works without proving every failure mode. A planned owner transfer on a smoke volume can be added later as an explicit profile, not silently included in the baseline.

No live deployment or smoke execution is performed by this plan. See [Packaging and rollout](Packaging-and-rollout.md) for distribution of the artifacts that both stages validate.
