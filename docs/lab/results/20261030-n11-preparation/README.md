# Stopped-image preparation for native run 20261030-n11

Task `py3` refreshed source attestations after n10 attempted three fixtures on
hfs-test0 and stopped the native guests cleanly. Collection and every normal
cleanup, shutdown and Rocky restoration step passed in n10. This preparation
infers no cause or resolution of n10's failed fixture. It gives no native
acceptance result and did not boot guests, run native tests, create a canonical
plan or record approval.

Actual host inventory at 14:45:30 UTC selected the existing ordinary snapshot
`zrepl_20261030_143854_000`. All four snapshots were created at 14:38:54 UTC,
strictly after n10's actual clean lab shutdown at 13:21:48.985044888 UTC.
Their GUIDs and zero-written checks passed. Rocky remained running as PID 3774,
all four FreeBSD VMs were stopped, and host swap was zero. Fresh pinned Rocky
root SSH rederived kernel `5.14.0-687.61.1.el9_8.x86_64` and its 1109-row RPM
manifest, recording UID as integer zero and retaining the actual `package_count`
observation field.

Root reviewed the constant-only R12-to-R13 differences and independently checked
six normalized syntax trees, baseline hashes and every changed literal. The
actual source/resource guard passed at 14:46:13 UTC. The released collector
finished successfully (`session 3055`, terminal `0fa101`) at 14:46:50 UTC;
the fixed twenty-receipt harvest passed (`8167ef`) at 14:47:16 UTC. Five actual
operation receipts, helper/proposal hashes and captured output hashes were
verified locally before querying the private databases.

Native image inspection used read-only vnode/UFS attachments, one at a time.
Development FreeBSD metadata used isolated sole-device userspace `zdb -L`,
with no guest pool import, guest ZFS mount, repair or pool-health conclusion.
Exact-owned MD/UFS resources and four newly automounted source snapshots were
cleaned normally. Final source GUID/written0 and unchanged VM-state checks
passed; all baseline host mount paths remain. No source snapshot creation,
hold, retention, ownership, package, VM configuration, guest permission or
replication-sink change was made.

Four private controller databases have mode 0600 and verified hashes. Package
manifests were independently rederived using immutable SQLite queries, with
unchanged database hashes before and after the reads. Each database fits
32 MiB and their aggregate 58,458,112 bytes fits 128 MiB. Host metadata occupies
22,908,928 bytes, below 160 MiB; metadata remains for 168 hours. This directory
contains 22 preparation proofs plus this README. Payload databases, binaries,
private keys, approval and native run state are excluded.

Source pruning, writes or another host restart can invalidate admission; the
next native plan must retain its fail-closed source checks. These receipts
are preparation evidence only.

## Replacement plan

The [reviewed plan](reviewed-plan.json) has canonical hash
`1d1463266fa43da06062396d520caf841f6236f814592b5f5558968969b8ff22`.
Created at 14:52:00 UTC, it expires 2026-10-31 at the same time. Actual read-only
admission passed at 14:50:55 UTC and matches the final policy subset. All thirty
artifact hashes and unchanged targets, bounds and graceful lifecycle passed
independent verification.

Both new FreeBSD amd64/CGO-disabled binaries record actual clean source
`2e875e2faf217787cffc39e635e7ac0654070750`. The journal scan uses a bounded
64 KiB window while preserving full validation. Local persisted-byte tests
show fewer read calls with identical bytes read; native latency and acceptance
remain unverified. The twelve synthetic fixtures, exact-content oracle and
eight-second case limit are unchanged, with the same 87-minute maximum,
2.25 GiB disk, 48 MiB logical I/O and normal cleanup/Rocky restoration.

Execution uses Paul's standing bounded non-production test authorization.
Production and critical or dangerous operations remain excluded. Approval,
controller state and native execution evidence were absent at publication.
Paul resumed work on all tasks; the preceding pause is revoked.
