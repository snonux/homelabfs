# Stopped-image preparation for native run 20261030-n8

Task `dy3` refreshed source attestations after n7 wrote disposable guest
fixtures and the three native guests stopped cleanly. The n7 behavior attempt
failed on `ftruncate`; collection, normal cleanup, lab shutdown and Rocky
restoration passed. This preparation gives no native behavior acceptance result.
It did not boot guests, run native tests, create a canonical plan or record approval.

Actual host inventory at 12:04:50 UTC selected the existing ordinary snapshot
`zrepl_20261030_115854_000`. All four snapshots were created at 11:58:54 UTC,
strictly after n7's actual clean lab shutdown at 11:48:51.877729154 UTC.
Their GUIDs and zero-written checks passed. Rocky remained running as PID 64854,
all four FreeBSD VMs were stopped, and host swap was zero. Fresh pinned Rocky
root SSH rederived kernel `5.14.0-687.61.1.el9_8.x86_64` and its 1109-row RPM
manifest, recording UID as integer zero.

Root reviewed the constant-only R9-to-R10 differences and independently checked
six normalized syntax trees, baseline hashes and every changed literal. The
actual source/resource guard passed at 12:17:07 UTC. The released collector
finished successfully (`session 61580`, terminal `192d28`) at 12:18:35 UTC;
the fixed twenty-receipt harvest passed (`34499e`) at 12:19:52 UTC. Five actual
operation receipts, helper hashes and captured output hashes were verified
locally before querying the private databases.

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
