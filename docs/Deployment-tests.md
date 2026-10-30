# Deployment compatibility tests

[Stage 1: native integration](Integration-tests.md) · [Stage 2: live smoke tests](Live-smoke-tests.md)

These checks cover the f3s-specific guest transport and Kubernetes integration. They are separate from the three-FreeBSD-VM native HomelabFS suite. The routine post-deployment live smoke suite is bounded and non-disruptive. Destructive guest, mount, and pod lifecycle tests below require an isolated deployment test environment; do not run them automatically against production workloads.

## Early compatibility gates

Before implementing extensive replication, validate the selected FreeBSD FUSE → bhyve virtio-9p → Rocky → pod path on the actual supported versions.

- Confirm guest 9p modules, protocol dialect, mount lifecycle, UID/GID handling, and application-visible errors.
- Verify write acknowledgement and sync propagation at the syscall boundary. A normal buffered write and a writable mmap store are different operations; define the promised persistence boundary for each. Kernel buffering must not silently contradict the chosen write contract.
- Apply the [2026-10-30 accepted lock scope](Platform-interfaces.md#accepted-9p-lock-scope-2026-10-30): prove real POSIX byte-range locks and flock contention between cooperating processes in the same guest 9p mount/kernel inode domain, including pods sharing that mount. Check blocking waits and wakeups, nonblocking conflicts, POSIX GETLK results, close/exit cleanup and the distinct POSIX process/flock open-description lifetimes.
- Record duplicate/independent mounts (including within one guest), separate guests and direct host access as unsupported shared-lock domains, never as locking passes. Separately prove publication denies a second independent writable mount/client and fences old consumers, including cached access, before replacement publication.
- Verify cache behavior for data, metadata, permissions, negative lookups, and existing mmap pages through failover and daemon restart.
- Verify recovery of the 9p share after FUSE restart. A fresh host mount need not repair already-open bhyve file handles automatically. Determine whether remount, workload restart, or VM restart is required and automate the smallest safe action.

The [installed lib9p machine-code investigation](lab/Deployed-9p-boundary.md#installed-code-and-source-binding)
proves lock success without host locks and getlock reporting unlocked. Pinned
Linux candidate source supports local conflict handling; it is not a live
exact-target pass. Paul retained 9p on 2026-10-30 with the single-mount advisory
lock limitation above. This does not waive credentials/groups/process identity,
O_SYNC/O_DSYNC and other sync propagation, cache/generation recovery, K04,
fencing or normal-publication restrictions. `zq3` remains incomplete and `4r3`
blocked. Linux's [9p cache documentation](https://docs.kernel.org/filesystems/9p.html)
also makes clear that disabling caches is not automatically a solution for mmap;
validate the selected version and its actual daemon barriers.

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
