# f3 / Rocky compatibility check

Live inspection and transport PoC on 2026-10-24. The 9p transport now works on an alternative kernel. Follow-up [FUSE and benchmark results](PoC-fuse-and-benchmarks.md) cover the passthrough layer; distributed HomelabFS remains unimplemented.

## Initial stock-kernel result

**The proposed virtio-9p path cannot run with the inspected guest kernel.** Its configuration disables NET_9P, and the required modules are absent. At initial inspection the guest also had no configured virtio-9p share. The later experiment below overcame these prerequisites with an alternative kernel and an isolated share.

## Evidence

| Check | Observed result |
|---|---|
| FreeBSD f3 | 15.1-RELEASE-p2, amd64 |
| Host bhyve manual | Lists virtio-9p as a supported device |
| Host FUSE | `/boot/kernel/fusefs.ko` exists; module not loaded; no `/dev/fuse` or FUSE mount observed |
| Host development packages | No Go/FUSE package matched the package inventory query |
| Guest | Rocky Linux 9.8 (Blue Onyx) |
| Running guest kernel | `5.14.0-687.59.1.el9_8.x86_64` |
| Guest kernel configuration | `# CONFIG_NET_9P is not set`; `CONFIG_FUSE_FS=m` |
| Module checks | Dry-run modprobe reports both `9p` and `9pnet_virtio` absent |
| Guest virtio devices | Only device type `0x0001` (network) observed |
| VM configuration | NVMe disk and virtio-net; no virtio-9p share |

Inspection used SSH, kernel configuration and module inventory, vm-bhyve configuration, the installed bhyve manual, and mount/device listings. No packages were installed, kernel modules loaded, mounts created, VM configuration edited, or services restarted. No workload data was accessed.

`kernel-modules-extra` is not installed, but the explicit disabled kernel configuration means this must not be treated as merely a missing mount helper. A kernel/module build with 9p support is needed; compatibility and maintenance of any replacement must be evaluated.

## Initial experiment plan

1. Prepare a disposable Rocky test guest with a kernel that enables the 9p filesystem and virtio transport. Alternatively, explicitly schedule the kernel change and restart of the existing development VM.
2. Configure a dedicated bhyve virtio-9p share backed only by disposable test data. First test ordinary ZFS directory access to isolate transport problems.
3. Build a minimal Go FUSE passthrough daemon on FreeBSD, load fusefs, and export its test mount through that share.
4. Run the [early compatibility gates](Integration-tests.md#early-compatibility-gates): credentials, locks, mmap, sync, cache behavior, daemon restart, and mount disappearance. Then run database recovery tests.

The chosen architecture remains provisional. If maintaining a different Rocky kernel is unacceptable, revisit the guest access interface explicitly. Linux FUSE support in the guest alone does not provide access to host ZFS; an alternative still needs a designed and tested host/guest protocol.

Host-side FUSE functionality, actual bhyve 9p operation, replication, fencing, crash durability, and performance remain unvalidated. A documented device and an installed kernel module are prerequisites, not evidence that the complete stack works.

## Module build and working alternative

A build using the exact signed Rocky source RPM and matching installed `kernel-devel` failed. The disabled 9p code still uses interfaces removed or changed by the distribution's backports: page-based address-space callbacks and the old `iov_iter_get_pages_alloc` signature. See the [build log](poc-results/rocky9-stock-module-build.log). No custom modules were installed or loaded. Merely enabling the configuration in a stock-source rebuild is therefore not a demonstrated solution either; the code needs porting.

Source RPM: `kernel-5.14.0-687.59.1.el9_8.src.rpm`, SHA-256 `2f9323a1aa92e61a5ecac7e47df9a8bd81338601a0ac0e1d76452e54bd030577`. The source archive already contains distribution changes; `patch-5.14-redhat.patch` was empty. The build used only `net/9p` and `fs/9p` as external modules against the installed build tree.

Installed ELRepo packages `kernel-lt`, `kernel-lt-core`, `kernel-lt-modules`, and `kernel-lt-modules-extra`, version `6.1.190-1.el9.elrepo`. The existing ELRepo repository configuration was used. This kernel enables `NET_9P`, `NET_9P_VIRTIO`, and `9P_FS` as modules. Secure Boot is disabled; SELinux remained enforcing throughout testing. [ELRepo kernel-lt documentation](https://elrepo.org/wiki/doku.php?id=kernel-lt).

The VM was shut down cleanly and restarted with a one-time GRUB selection for ELRepo. The stock `5.14.0-687.59.1.el9_8.x86_64` kernel remains the default. This is a transport experiment, not a decision to deploy alternative kernels across f3s. ELRepo kernel updates and rollback would need an explicit maintenance procedure before adoption.

## Working test configuration

Host backing directory: `/var/tmp/homelabfs-9p-poc/share`, on f3's `zroot/var/tmp` dataset. It is a disposable, world-writable test directory, not a production permission model. The original VM configuration is preserved at `/var/tmp/homelabfs-9p-poc/rocky.conf.original`.

Added to `/zroot/bhyve/rocky/rocky.conf`:

```text
bhyve_options="-s 0:7:0,virtio-9p,homelabfs_poc=/var/tmp/homelabfs-9p-poc/share"
```

On the running ELRepo kernel:

```sh
modprobe 9pnet_virtio
modprobe 9p
mount -t 9p -o trans=virtio,version=9p2000.L,cache=mmap,msize=262144 \
  homelabfs_poc /mnt/homelabfs-9p-poc
```

There is no fstab entry. The mount was left available because Paul's interactive shell was using it; no forced unmount was attempted. All test payloads were removed. On the next normal boot, the stock kernel will not provide this mount. Repeating the experiment requires explicitly selecting the ELRepo kernel again.

## Functional results

The [test script](../tools/poc/check-filesystem.py) uses a fresh temporary directory. The original seven checks first passed on local XFS as a baseline.

| Check | 9p cache=none | 9p cache=mmap |
|---|---|---|
| Write, fsync, rename, hard link, unlink-open, directory fsync | Pass | Pass |
| UID/GID/mode and denied read by another UID | Pass | Pass |
| Writable mmap, flush and fsync | Fails: EINVAL | Pass |
| Same-mount flock contention | Pass | Pass |
| Same-mount fcntl contention | Pass | Pass |
| SQLite DELETE journal, FULL sync, reopen | Pass | Pass |
| SQLite WAL, FULL sync, reopen | Fails: disk I/O error | Pass |
| SQLite WAL process exits without closing, committed row survives and uncommitted row is absent | Not run | Pass |

Raw results: [cache=none](poc-results/rocky9-9p-cache-none.json), [cache=mmap](poc-results/rocky9-9p-cache-mmap.json).

A second independent mount of the same virtio share was refused (`no channels available`). Cross-mount locking therefore remains untested; it requires a second independently attached share or client. The same-mount results do not establish host-side or multi-client lock enforcement.

## Test I/O

The [bounded I/O script](../tools/poc/check-io.py) ran through the cache=mmap mount, using random payloads and checking every result. [Raw measurements](poc-results/rocky9-9p-io.json).

- 128 MiB sequential write with final fsync: **132.34 MiB/s**; read and SHA-256 verification: **299.01 MiB/s**.
- 256 random 4 KiB writes, each followed by fsync: **0.977 ms median**, **1.048 ms p95**, **2.989 ms maximum**. All 251 distinct final block locations verified.
- Four simultaneous writers, each writing and verifying 16 MiB: all passed.
- A separate 16 MiB file had the same SHA-256 in the guest and directly on f3: `c0bb977c1ad1bb6abbd523f796a45f233968ffdbe1fcf64aac6ee4f041ce5f1b`.

These are short buffered smoke measurements, including Python, data-generation and hashing overhead, with warm caches possible. They are not cold-disk throughput, latency guarantees, or HomelabFS replication benchmarks. The attempted final remount was blocked by an interactive shell's working directory; no cache-cold remount claim is made. Kernel logs showed no panic/Oops during these checks.

## Remaining gates

The working path is Rocky → virtio-9p → ordinary host ZFS files. It does **not** yet include the Go FUSE daemon. No host power-loss, bhyve crash, replication, fencing, PostgreSQL, or Kubernetes test has run. A successful fsync syscall and a process-crash SQLite check do not prove power-loss durability. The subsequent [FUSE experiment](PoC-fuse-and-benchmarks.md) inserts a Go passthrough and runs functional/I/O checks. Propagation under failure, cache fencing, and full workload compatibility remain acceptance gates. See the [reproduction runbook](PoC-runbook.md) and [package-distribution plan](Packaging-and-rollout.md).
