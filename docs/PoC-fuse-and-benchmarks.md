# Go FUSE plus bhyve 9p: initial results

Tested on f3 and its standalone Rocky development VM, 2026-10-24. [Reproduction runbook](PoC-runbook.md) · [Kernel findings](PoC-f3-rocky.md) · [Package plan](Packaging-and-rollout.md)

## Outcome

**The complete Rocky → virtio-9p → FreeBSD Go FUSE passthrough → ZFS path works for the initial functional and I/O tests.** It requires the tested ELRepo kernel and `cache=mmap`; the stock Rocky kernel and no-cache mode do not meet these prerequisites.

This validates a transport prototype. It does not implement or validate HomelabFS replication, journaling, ownership, fencing, automatic failover, or the entire f3s application portfolio.

## Implementation and environment

- Host: Intel N100, FreeBSD 15.1-RELEASE-p2, existing ZFS `zroot/var/tmp` dataset. Properties: `compression=on`, `sync=standard`, `recordsize=128K`, `dedup=off`.
- Guest: Rocky Linux 9.8, four vCPUs and 12 GiB configured RAM, ELRepo kernel `6.1.190-1.el9.elrepo.x86_64`, SELinux enforcing.
- Go probe: [source](../tools/poc/fuse-passthrough/main.go), separate module pinned to go-fuse v2.9.0. Cross-built with Go 1.26.8 for FreeBSD/amd64 without cgo; Linux build and both target vet checks also passed. Linux FUSE runtime behavior was not tested.
- FUSE: allow-other with kernel default permission checks; explicit zero attribute, entry, and negative-entry timeouts. No explicit FUSE writeback-cache optimization enabled by the probe. Guest 9p `cache=mmap` still introduces caching/writeback.
- Host backing directory: `/var/tmp/homelabfs-fuse-poc/backing`. FUSE mount: `/var/tmp/homelabfs-9p-poc/share/fuse`. Guest view: `/mnt/homelabfs-9p-poc/fuse`.
- The existing bhyve share traverses the nested host FUSE mount, avoiding another VM restart. The outer export also contains raw test directories solely for comparison; production must expose only the coordinated filesystem.

## Functional checks

All eight [functional checks](../tools/poc/check-filesystem.py) passed through the combined path:

1. Write/fsync, rename, hard link, unlink-open, and directory fsync.
2. Numeric UID/GID and mode round-trip, with denied access from a different UID.
3. Writable mmap, flush, and fsync.
4. Same-mount flock contention between processes.
5. Same-mount fcntl contention between processes.
6. SQLite DELETE journal with FULL sync and reopen.
7. SQLite WAL with FULL sync and reopen.
8. SQLite process exit without connection cleanup: committed row retained, uncommitted row absent, integrity check passed.

[Raw functional results](poc-results/rocky9-9p-fuse-functional.json). These lock checks cover one guest mount, not independently connected clients. Credentials were tested using root and synthetic UIDs, not the full Kubernetes supplementary-group and security-context matrix.

The [bounded Python I/O test](../tools/poc/check-io.py) also passed: a 128 MiB sequential transfer with checksum verification, 256 random 4 KiB writes with per-write fsync and read-back checks, and four concurrent 16 MiB writers. [Raw results](poc-results/rocky9-9p-fuse-io.json).

An additional 16 MiB file written through the guest's FUSE-backed path produced the same SHA-256 directly in the host backing directory: `1a3ade6af8fff8c3e8087d8d18883302b6fb985fce1612948ec226e31367c81a`. Test payloads were removed after verification.

## fio comparison

Used fio as the benchmark available on both operating systems: FreeBSD **3.43**, Rocky **3.35**. These are different packaged versions, so this is an initial comparison rather than a controlled version-identical benchmark.

All paths used the same [job file](../tools/poc/compare.fio): synchronous engine, one job, queue depth one, buffered I/O, 256 MiB sequential writes in 1 MiB blocks with a final fsync, sequential read-back with CRC32C verification, then ten seconds of random 4 KiB writes in a 64 MiB file with fsync after each write. Buffers are refilled. Paths ran sequentially; three repetitions were collected, rotating path order after the initial run. The FreeBSD → FUSE → ZFS path was added afterward as three consecutive runs with the same job file and host fio version, using `/var/tmp/homelabfs-9p-poc/share/fuse/fio-host`. All 36 jobs across the four paths reported `error=0`.

Values below are the median of three runs, with the observed minimum–maximum in parentheses. Random-write IOPS cover the write-plus-sync loop; fsync p95 is the median of the three per-run fsync p95 values and excludes the preceding write syscall.

| Path | Sequential write MiB/s | Read/verify MiB/s | Random write + fsync IOPS | fsync p95 ms |
|---|---:|---:|---:|---:|
| FreeBSD directly on ZFS | 405.7 (265.0–407.0) | 3657.1 (1333.3–3938.5) | 1427.1 (1264.8–1452.7) | 0.668 |
| FreeBSD → FUSE → ZFS | 303.3 (299.1–343.6) | 1168.9 (1032.3–1219.0) | 1278.9 (1267.1–1405.9) | 0.750 |
| Rocky → 9p → ZFS | 190.8 (181.8–291.9) | 1000.0 (588.5–1855.1) | 990.9 (966.7–1089.9) | 0.938 |
| Rocky → 9p → FUSE → ZFS | 241.7 (235.3–246.2) | 755.2 (719.1–775.8) | 867.8 (855.0–933.0) | 0.954 |

The host-only FUSE row isolates the additional filesystem layer without involving bhyve or 9p, although it was measured later rather than interleaved with the original baselines. It uses the same FreeBSD fio binary and ZFS dataset as the direct-host baseline. The layered path had lower random write-and-sync throughput in this sample. Its higher sequential-write result than raw 9p is not evidence that adding FUSE improves performance: the short buffered workload, differing cache behavior, and run-to-run variation prevent that conclusion. Read results include cache effects and must not be described as physical disk throughput. Caches were not globally flushed, the host was not otherwise isolated, and the test set fits in memory. fio's own cache invalidation behavior may also differ by OS.

No replication or application journal was involved. These results are not a latency SLA, power-loss durability test, or estimate of final HomelabFS throughput.

Raw evidence:

| Path | Run 1 | Run 2 | Run 3 |
|---|---|---|---|
| FreeBSD ZFS | [JSON](poc-results/fio-freebsd-zfs.json) | [JSON](poc-results/fio-freebsd-zfs-2.json) | [JSON](poc-results/fio-freebsd-zfs-3.json) |
| FreeBSD FUSE/ZFS | [JSON](poc-results/fio-freebsd-fuse-zfs-1.json) | [JSON](poc-results/fio-freebsd-fuse-zfs-2.json) | [JSON](poc-results/fio-freebsd-fuse-zfs-3.json) |
| Rocky 9p | [JSON](poc-results/fio-rocky-9p.json) | [JSON](poc-results/fio-rocky-9p-2.json) | [JSON](poc-results/fio-rocky-9p-3.json) |
| Rocky 9p/FUSE | [JSON](poc-results/fio-rocky-9p-fuse.json) | [JSON](poc-results/fio-rocky-9p-fuse-2.json) | [JSON](poc-results/fio-rocky-9p-fuse-3.json) |

## Remaining risks and current state

The FUSE daemon logged `Unimplemented opcode BMAP` during testing. Observed file and fio operations still succeeded; the warning is recorded rather than treated as proof of full operation coverage. No panic/Oops was observed in the inspected logs.

Daemon crash/remount recovery, old file handles, cache fencing, actual power loss, cross-client locks, PostgreSQL, and real Kubernetes workloads remain untested. Returning successfully from fsync does not prove that every layer meets the eventual HomelabFS durability contract. The minimal loopback also does not prevent an empty underlying mountpoint becoming visible if the mount disappears; production needs an explicit mount-readiness and access gate.

The FUSE daemon and test mount remain running for inspection, without boot-service configuration. The guest remains on the one-time ELRepo boot, with its stock kernel still the normal default. No production workload mount was changed. fio packages and build/test artifacts remain installed; benchmark data files were removed. The dedicated test export and permissive test-directory modes must not be reused as a production security policy.
