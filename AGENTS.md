# HomelabFS agent handoff

Current scope: implement and validate HomelabFS using disposable lab and canary
volumes only. Do not migrate production workloads, copy production data into
canaries, retire NFS, or change production PVs/consumers. Production migration
and NFS retirement belong to a separate future project. Existing workload
patterns are compatibility examples, not a migration checklist.

HomelabFS is in the concept phase. The Go FUSE code under `tools/poc/` is a
loopback compatibility probe, not a distributed filesystem.

Before working on the lab, read `docs/Lab-operations.md` and
`docs/Test-automation.md`. Use the user's f3s skill for infrastructure details.

- f3 hosts three FreeBSD guests, `hfs-test0` through `hfs-test2`, each 2 GiB.
  FUSE runs **inside each guest**, backed by that guest's own ZFS pool.
- The independent Rocky/9p probe uses host FUSE on f3. Do not conflate it with
  native three-guest tests or add 9p to the native integration matrix.
- Inspect actual VM state; recorded state is historical. Never run the three
  guests alongside the 12 GiB Rocky VM on this approximately 16 GiB host.
- Routine bounded verification batches are covered by the user's standing
  approval of 2026-11-02: "approve all verification batches automatically from
  now on". Do not ask again for routine verification. Critical or dangerous
  operations, including disruptive VM lifecycle windows, still need explicit
  approval; a `--approved-run` flag alone grants no authority for those actions.
- Existing preparation is complete: do not rerun the creation script or
  overwrite VM disks. Use scripts under `tools/lab/` for approved operations.
- Pin guest SSH host keys using `docs/lab/f3-known_hosts`; access via f3.
- Keep tests bounded to disposable run-owned directories. Collect evidence,
  unmount normally, and preserve failed resources for inspection. Never
  force-kill Rocky or delete production data to make a test pass.
- Initial results and setup limitations: `docs/Lab-operations.md`.
  Full replication/failover harness and HomelabFS itself remain unimplemented.

The lab is replicated from f3 to f2 every 10 minutes. Read
`docs/Lab-replication.md` before recovery or changing snapshot ownership.
Never boot or modify the received sink directly.
