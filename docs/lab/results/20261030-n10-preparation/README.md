# Stopped-image preparation for native run 20261030-n10

Task `jy3` refreshed source attestations after n9 attempted three fixtures on
hfs-test0 and stopped the native guests cleanly. N9 failed its third fixture;
collection was explicitly reconciled failed after positive inspection. Normal
cleanup, guest shutdown and Rocky restoration/readiness passed. No cause or
resolution of the fixture or collection failures is inferred here. This
preparation gives no native acceptance result and did not boot guests, run
native tests, create a canonical plan or record approval.

Actual host inventory at 13:09:56 UTC selected the existing ordinary snapshot
`zrepl_20261030_130854_000`. All four snapshots were created at 13:08:54 UTC,
strictly after n9's actual clean lab shutdown at 12:59:33.384134734 UTC.
Their GUIDs and zero-written checks passed. Rocky remained running as PID 91299,
all four FreeBSD VMs were stopped, and host swap was zero. Fresh pinned Rocky
root SSH rederived kernel `5.14.0-687.61.1.el9_8.x86_64` and its 1109-row RPM
manifest, recording UID as integer zero and retaining the actual `package_count`
observation field.

Root reviewed the constant-only R11-to-R12 differences and independently checked
six normalized syntax trees, baseline hashes and every changed literal. The
actual source/resource guard passed at 13:10:43 UTC. The released collector
finished successfully (`session 45508`, terminal `6c4871`) at 13:11:16 UTC;
the fixed twenty-receipt harvest passed (`ed8c67`) at 13:12:17 UTC. Five actual
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
