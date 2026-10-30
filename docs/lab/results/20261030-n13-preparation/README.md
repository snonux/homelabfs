# Stopped-image preparation for native run 20261030-n13

Task `ry3` refreshed source attestations after n12's first root/direct fixture
failed its lock check. Original collection, normal cleanup, lab shutdown and
Rocky restoration/readiness all passed. This preparation infers no cause or
resolution of the lock failure and gives no native acceptance result. It did
not boot guests, run native tests, create a canonical plan or record approval.

Actual inventory at 15:10:43 UTC selected existing ordinary snapshot
`zrepl_20261030_150854_000`, created at 15:08:54 UTC strictly after n12's actual
clean lab shutdown at 15:00:23.429555283 UTC. All four GUID/written0 checks passed.
Rocky remained running as PID 24786, all four FreeBSD VMs were stopped, and
host swap was zero. Fresh pinned Rocky root SSH rederived kernel
`5.14.0-687.61.1.el9_8.x86_64` and its 1109-row RPM manifest, with integer UID zero
and the actual `package_count` observation field retained.

Root independently verified six R13-to-R14 baseline/new hashes, normalized
syntax trees and exact allowed literal substitutions. The actual source/resource
guard passed at 15:12:19 UTC. Collector session `92225`, terminal `2c7d2d`,
finished successfully at 15:12:58 UTC; fixed twenty-receipt harvest `20962e`
passed at 15:13:52 UTC. Five actual operation receipts, helper/proposal hashes
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

## Replacement plan

Canonical hash: `8615d1456af119f0e4d7c7e4a62deea577f3d7a722852080fb4ed770c3087c90`.
Created 15:24:14 UTC; expires 2026-10-31 at the same time. Actual fresh read-only
admission passed at 15:22:40 UTC and matches the final policy subset. Thirty
artifact hashes and unchanged targets, bounds and lifecycle passed independent
verification. New FreeBSD amd64/CGO-disabled binaries record actual clean source
`a296dd8789e90d394cd1ca765b7f5a934710048f` and use the fresh R14 metadata.

The lock changes add stage/helper context and one read-only holder snapshot only
after a completed unexpected fcntl conflict. Original errors remain failures;
no acquisition is retried and unknown pipe outcomes trigger no further operation.
N12's lock cause remains unresolved. Local reviews and all nine Go gates passed;
no native acceptance or process-exit lifecycle fix is claimed.

The same twelve synthetic fixtures retain the exact-content oracle, eight-second
case bound, 87-minute maximum, 2.25 GiB disk, 48 MiB logical I/O and normal cleanup
with graceful Rocky restoration. Paul's standing bounded non-production test
authorization applies; production and critical or dangerous operations remain
excluded. Approval records, controller state and execution evidence were absent
at publication. Work continues on all tasks; the prior pause is revoked.
