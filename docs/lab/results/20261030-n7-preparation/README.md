# Stopped-image preparation for native run 20261030-n7

Task `by3` refreshed source attestations after n6 wrote disposable guest
fixtures and the three native guests stopped cleanly. This preparation did
not boot guests, execute a native test, create a canonical run plan or record
approval. The n6 behavior attempt failed at its first fixture mount; these
receipts make no native behavior acceptance claim.

Actual host inventory at 11:28:38 UTC selected the existing ordinary snapshot
`zrepl_20261030_111853_000`. All four snapshots were created after n6's actual
clean guest shutdown at 11:07:32.549 UTC. Their GUIDs and zero-written checks
passed. Rocky remained running as PID 51221, all four FreeBSD VMs were stopped,
and host swap was zero. Fresh pinned Rocky root SSH rederived its kernel and
1109-row RPM manifest with UID recorded as integer zero.

Root reviewed the constant-only R8-to-R9 differences and independently checked
six normalized syntax trees, baseline hashes and the exact diff. The actual
source/resource guard passed at 11:32:43 UTC. The exact released collector
finished successfully (`session 17934`, terminal `442a71`) at 11:33:38 UTC;
the fixed twenty-receipt harvest passed (`f5e598`) at 11:34:59 UTC. Operation
receipts, helper hashes and captured output hashes were checked locally.

Native image inspection used read-only vnode/UFS attachments, one at a time.
Development FreeBSD metadata used isolated sole-device userspace `zdb -L`,
with no guest pool import, guest ZFS mount, repair or pool-health conclusion.
Host source snapshots automounted. Exact-owned MD/UFS resources and four
newly automounted source snapshots were cleaned normally. Final source
GUID/written0 and unchanged VM-state checks passed. No source snapshot creation, hold, retention, ownership, package, VM
configuration, guest permission or replication-sink change was made.

Four private controller databases have mode 0600 and verified hashes. Package
manifests were independently rederived using immutable SQLite queries, with
unchanged database hashes before and after the reads. Each database fits
32 MiB and their aggregate 58,458,112 bytes fits 128 MiB. Host metadata occupies
22,908,928 bytes, below 160 MiB; metadata remains for 168 hours. This directory
contains 21 preparation proofs plus this README. Payload databases, binaries,
private keys, approval and native run state are excluded.

These preparation receipts give no filesystem acceptance result. Source
pruning, writes or another host restart can invalidate admission; the next
native plan must retain its fail-closed source checks.

## Replacement plan

The finalized [plan](reviewed-plan.json) has canonical hash
`8d2b54e5e4935cb4201b4d79c299230ca5a67813169fc71fc6f8d76dda1cb01c`.
Created at 11:44:20 UTC, it expires 2026-10-31 at the same time. The actual
read-only admission survey passed at 11:43:19 UTC and matches the final
policy/targets/budget subset. Thirty artifact hashes, all real offline contexts,
collection dependencies and unchanged scope/deadlines passed verification;
root independently checked them again.

Both new FreeBSD amd64/CGO-disabled binaries record actual clean source
`733d843b95a8680e118330e3ce3eef0b94447747`, including the reviewed platform
mount fix. An initial isolated-checkout build lacked required VCS metadata and
was rejected; it is excluded from this plan. The old `127ecc9` binaries are
also excluded.

The [review](review.txt) retains twelve bounded synthetic fixture calls, the
87-minute maximum, 2.25 GiB disk, 48 MiB logical I/O, normal cleanup and graceful
Rocky restoration. Execution is covered by Paul's recorded standing approval
for this bounded non-production profile. Critical or dangerous scope changes
and production remain excluded. No n7 approval record, controller state or
native execution evidence existed at packet publication; root records the
standing authorization separately before execution. Source drift or pruning
still fails closed at live admission.
