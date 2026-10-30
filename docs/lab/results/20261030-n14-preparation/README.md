# Stopped-image preparation for native run 20261030-n14

Task `ty3` refreshed source attestations after n13's first root/direct fixture
failed its lock check. Original collection, normal cleanup, lab shutdown and
Rocky restoration/readiness all passed. This preparation infers no cause or
resolution of the lock failure and gives no native acceptance result. It did
not boot guests, run native tests, create a canonical plan or record approval.

Actual inventory at 15:36:57 UTC selected existing ordinary snapshot
`zrepl_20261030_152854_000`, created at 15:28:54 UTC strictly after n13's actual
clean lab shutdown at 15:26:50.306388662 UTC. All four GUID/written0 checks passed.
Rocky remained running as PID 37004, all four FreeBSD VMs were stopped, and
host swap was zero. Fresh pinned Rocky root SSH rederived kernel
`5.14.0-687.61.1.el9_8.x86_64` and its 1109-row RPM manifest, with integer UID zero
and the actual `package_count` observation field retained.

Root independently verified six R14-to-R15 baseline/new hashes, normalized
syntax trees and exact allowed literal substitutions. The actual source/resource
guard passed at 15:44:57 UTC. Collector session `25470`, terminal `c97ffd`,
finished successfully at 15:45:35 UTC; fixed twenty-receipt harvest `1c05f6`
passed at 15:46:21 UTC. Five actual operation receipts, helper/proposal hashes
and captured output hashes were verified before immutable SQLite reads.

Native metadata used read-only vnode/UFS attachments one at a time; development
FreeBSD used isolated sole-device userspace `zdb -L`. No guest code, guest pool
import, guest ZFS mount, repair or pool-health conclusion was involved. Exact-owned
MD/UFS resources and four new source snapshot automounts were cleaned normally;
all baseline host mount paths remained. Final source GUID/written0 and VM-state
checks passed. No snapshot creation, hold, retention, ownership, package, VM
configuration, guest permission or replication-sink change was made.

Four private controller databases have mode 0600 and verified hashes. Fresh
immutable SQLite queries independently rederived their manifests; database hashes
were unchanged before and after. Each fits 32 MiB; aggregate 58,458,112 bytes fits
128 MiB. Host metadata uses 22,908,928 bytes, below 160 MiB, retained for 168 hours.
This directory contains 22 preparation proofs plus this README. Payload databases,
binaries, keys, approval and native run state are excluded.

Preparation gives no filesystem acceptance result. Snapshot pruning, writes or
another host restart can invalidate admission; future native plans must retain
fail-closed source checks.
