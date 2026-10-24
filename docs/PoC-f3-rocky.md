# f3 / Rocky compatibility check

Live inspection on 2026-10-24. This is a prerequisite check, not a completed filesystem PoC.

## Result

**The proposed virtio-9p path cannot run with the inspected guest kernel.** Its configuration disables NET_9P, and the required modules are absent. The guest also has no configured virtio-9p share. This blocks full-path permission, locking, mmap, sync, database, and recovery tests.

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

## Next experiment

1. Prepare a disposable Rocky test guest with a kernel that enables the 9p filesystem and virtio transport. Alternatively, explicitly schedule the kernel change and restart of the existing development VM.
2. Configure a dedicated bhyve virtio-9p share backed only by disposable test data. First test ordinary ZFS directory access to isolate transport problems.
3. Build a minimal Go FUSE passthrough daemon on FreeBSD, load fusefs, and export its test mount through that share.
4. Run the [early compatibility gates](Integration-tests.md#early-compatibility-gates): credentials, locks, mmap, sync, cache behavior, daemon restart, and mount disappearance. Then run database recovery tests.

The chosen architecture remains provisional. If maintaining a different Rocky kernel is unacceptable, revisit the guest access interface explicitly. Linux FUSE support in the guest alone does not provide access to host ZFS; an alternative still needs a designed and tested host/guest protocol.

Host-side FUSE functionality, actual bhyve 9p operation, replication, fencing, crash durability, and performance remain unvalidated. A documented device and an installed kernel module are prerequisites, not evidence that the complete stack works.
