# Deployment compatibility tests

[Stage 1: native integration](Integration-tests.md) · [Stage 2: live smoke tests](Live-smoke-tests.md)

These checks cover the f3s-specific guest transport and Kubernetes integration. They are separate from the three-FreeBSD-VM native HomelabFS suite. The routine post-deployment live smoke suite is bounded and non-disruptive. Destructive guest, mount, and pod lifecycle tests below require an isolated deployment test environment; do not run them automatically against production workloads.

## Early compatibility gates

Before implementing extensive replication, validate the selected FreeBSD FUSE → bhyve virtio-9p → Rocky → pod path on the actual supported versions.

- Confirm guest 9p modules, protocol dialect, mount lifecycle, UID/GID handling, and application-visible errors.
- Verify write acknowledgement and sync propagation at the syscall boundary. A normal buffered write and a writable mmap store are different operations; define the promised persistence boundary for each. Kernel buffering must not silently contradict the chosen write contract.
- Verify locking in one guest/mount, across pods using that mount, through duplicate guest mounts, and between independently connected clients. Preserve correct local application locking; enforce exclusive volume serving against other clients.
- Verify cache behavior for data, metadata, permissions, negative lookups, and existing mmap pages through failover and daemon restart.
- Verify recovery of the 9p share after FUSE restart. A fresh host mount need not repair already-open bhyve file handles automatically. Determine whether remount, workload restart, or VM restart is required and automate the smallest safe action.

Two concrete findings make these gates essential. Linux's 9p documentation describes the no-cache mode as disabling mmap, while its mmap mode includes writeback behavior; disabling every cache is therefore not automatically a solution for database compatibility. The FreeBSD main-branch lib9p backend inspected during this review returns lock success without acquiring a host lock and reports regions unlocked. This does not establish the behavior of the deployed release or prove same-guest locking fails, since the guest kernel can also maintain locks. It does mean host locking cannot be assumed to protect independent clients. Sources: [Linux 9p cache modes](https://docs.kernel.org/filesystems/9p.html), [FreeBSD lib9p backend](https://github.com/freebsd/freebsd-src/blob/main/contrib/lib9p/backend/fs.c).

If a required property fails, stop acceptance of the affected canary profile and revise or repair the selected transport explicitly. Do not hide missing semantics behind a successful mount or filesystem benchmark.

## Kubernetes lifecycle scenario families

Preserve these IDs for existing references. They are not part of the 36 native Stage 1 families and are not all routine live smoke tests.

| ID | Fault or action | Required result |
|---|---|---|
| K01 | Crash only pod, Rocky guest, bhyve process, or FUSE daemon in separate runs | Use the appropriate restart/remount/recovery path; no unnecessary cluster-wide promotion |
| K02 | Delete/recreate pod while old process survives a partition | New volume publication waits for actual fencing; API deletion is not evidence of old process death |
| K03 | Repeat or interrupt CSI publish/unpublish, including stale requests after promotion | Requests are idempotent and scoped to volume/session/generation; stale unpublish cannot revoke a newer valid mount |
| K04 | Remove FUSE mount or guest 9p mount; leave ordinary directory underneath | No workload writes to an empty/raw fallback directory; readiness fails clearly |
| K05 | Pod uses multiple persistent volumes; destinations reach readiness at different times | Start only when all required mounts are ready; no deadlocked ownership transfer; no implied cross-volume transaction guarantee |

Test isolated application instances with synthetic data through the guest mount and canary Kubernetes publication path. Do not attach existing workload PVs or copy production data. A native FreeBSD database test cannot replace application-specific validation in Rocky. Include metrics/log storage and dependency ordering of PV/PVC creation, mount readiness, and workload startup.

## Deployment and physical-host acceptance

Run K01–K05, cached-descriptor/mmap fencing, physical-host loss, shutdown/cold
start and independent restore only on isolated test infrastructure under an
explicit approved plan. Use disposable volumes, an external oracle, bounded
faults and recorded restoration. Do not extend live shared-host smoke into
production host/guest fault injection or production power-automation changes.

Report these profiles separately from Stage 1 and routine Stage 2. Missing
physical infrastructure means a physical case is not run and the corresponding
claim remains unproven; it does not require disrupting production to finish a
bounded live canary smoke run. All selected cases must pass for their profile.
See [canary readiness](Deployment-readiness.md). No outcome here authorizes
production migration or NFS retirement.
