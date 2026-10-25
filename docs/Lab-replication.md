# Lab replication copy: f3 → f2

The complete `zroot/bhyve/homelabfs-lab` subtree is included in f3's existing `f3_to_f2_freebsd` zrepl push job. The destination is `zdata/sink/f3/zroot/bhyve/homelabfs-lab` on f2's data pool, alongside the existing development FreeBSD and Rocky copies.

## Policy and contents

- Encrypted raw ZFS sends over the existing WireGuard connection to f2.
- New snapshots every 10 minutes while f3 is online and zrepl is running.
- Sender and receiver retention: last 10 snapshots plus `24x1h | 14x1d`.
- Recursive selection `"zroot/bhyve/homelabfs-lab<": true` includes the parent, all three guest datasets, and future descendants.
- The same subtree is excluded from f3's local snapshot job. Existing local snapshots are preserved; this change does not delete them.
- f2 already excludes `zdata/sink<` from its local snapshot job. Received filesystems remain unmounted and are not registered as runnable VMs.

The copy contains each guest's vm-bhyve configuration, OS and data disk images, plus the lab's manifest and public host-key records. The inner guest ZFS pools are contained in those disk images; zrepl does not connect to the guests. Host-level datastore/switch registration is **not** in the lab dataset and must be reconstructed during recovery.

This is a periodic recovery copy, not synchronous storage replication or automatic failover. Recovery is limited to the latest **completed** received snapshot, potentially older than ten minutes after an outage or stalled transfer. Snapshots of running VMs are not application-quiesced; guest filesystems and application journals may need recovery. Independent guest snapshots do not establish a coordinated application checkpoint across the cluster.

## Inspect replication

```sh
ssh f3 'doas zrepl status --mode dump'
ssh f2 'doas zfs list -r -o name,used,available,mounted zdata/sink/f3/zroot/bhyve/homelabfs-lab'
```

Check the parent and all three children are `DONE`, and both pruning phases complete. Compare the `guid` property of identically named snapshots on source and destination; dataset names have different prefixes, snapshot GUIDs must match. Do not mistake an incomplete receive for a usable snapshot.

Configuration is managed in the conf repository's `gonf/cluster/freebsd_storage.go`. The f3 push job owns replication and snapshots; f2's sink endpoint is unchanged. Sink encryption roots use the existing `/keys/f3.lan.buetow.org:bhyve.key`, with their key locations and boot-time key-loading inventory managed by gonf. Never commit the key itself. Replication can continue with received encryption keys unloaded. The parent and each of the three guest datasets are separate received encryption roots. Future descendants are replicated automatically, but their encryption roots need additional key-loading inventory entries.

Watch destination `available` and `usedbysnapshots`. Snapshot counts do not bound bytes, and guest writes can retain substantial old disk-image blocks. The source lab has a 120 GiB aggregate quota and 40 GiB per guest; received properties and retained history must be checked when planning capacity.

## Recovery procedure — separately approved

1. Fence or otherwise confirm the original three VMs cannot run or rejoin. Recovered disks retain their IPs, MACs, host IDs and SSH keys.
2. Select completed received snapshots with matching run/time evidence. Preserve the received datasets and snapshot history. Do not point vm-bhyve at the live replication sink or modify it for normal use.
3. On suitable replacement FreeBSD hardware, create a separate writable recovery copy using ZFS send/receive from the selected snapshots. On f2 itself, snapshot clones are also possible but depend on retained origins and require a deliberate retention plan. Obtain the f3 bhyve encryption key from the existing secure key storage; never assume the failed disk was its only copy.
4. Reconstruct a vm-bhyve datastore named `hfslab` pointing to the **recovery** dataset, and a switch named `hfslab` with address `172.31.253.1/24`, no physical uplink and no NAT. The three configurations reference that switch. Check subnet conflicts and IPv4 forwarding before use. See [original preparation](FreeBSD-lab-preparation.md).
5. Register the existing guest directories, preserving each guest's configuration and attaching its OS/data disk pair. Do not invoke the preparation script, which creates new disks and identities. Keep automatic startup disabled.
6. Admit memory before booting: the three guests need 6 GiB plus host/hypervisor headroom. f2 already hosts r2; do not stop r2 or overcommit its host implicitly. A received copy on f2 does not imply f2 has capacity to run it.
7. In the approved recovery window, boot and inspect guest pool import, filesystem recovery, pinned SSH identity and peer networking. Run PoC/integration checks only if explicitly included in that approval. Establish a new replication direction deliberately before allowing the old source back.

No restore boot or disaster-recovery drill was performed while enabling this replication. See [lab operations](Lab-operations.md) for the independent PoC results.

## Validation on 2026-10-25

The first scheduled lab snapshot, `zrepl_20261025_095215_000`, completed its full transfer in the existing job at 11:53:21 EET (66 seconds for the job, including Rocky). All four lab datasets completed, and both pruning phases succeeded.

A one-off administrative recursive snapshot, `zrepl_verify_20261025_095352`, then verified incremental transfer without writing guest payload data or waiting for the next ten-minute boundary. It uses the existing `zrepl_` retention policy. Its incremental job completed in two seconds. All **eight snapshot GUIDs** (two snapshots across four datasets) matched between f3 and f2. This verifies incremental transfer mechanics; it is not a guest write/recovery test.

`zfs load-key -n` successfully verified the existing key against each received encryption root, without loading it. All received lab datasets remained unmounted. The live configuration and managed configuration matched after deployment. Rocky remained running and all three lab guests remained stopped throughout this change. No restore boot, integration test, VM power-state change, or guest I/O was performed.

[Recorded replication status, receiver properties, and GUID comparison](lab/results/20261025-replication/).
