# Stopped-image preparation for native run 20261030-n5

Task `hx3` refreshed the image attestations after the n4 guest lifecycle. It did
not execute a native test, create approval or change VM state.

The actual host inventory at 08:47:02 UTC selected existing ordinary source
snapshot `zrepl_20261030_083853_000`. All four source GUIDs and zero-written
checks passed. Rocky remained running as PID 17368, with all four FreeBSD VMs
stopped and zero host swap. Fresh pinned Rocky root SSH returned the expected
kernel and the rederived 1109-row RPM manifest.

Root reviewed the constant-only helper differences and independently compared
their normalized syntax trees and baseline hashes. The resource/source guard
passed (`9f9a33`), then the released collector finished successfully
(`session 98341`, terminal `e3ff1b`). Native images used read-only vnode/UFS
inspection; development FreeBSD metadata used the isolated sole-device userspace
`zdb -L` method. No guest code, pool import, guest ZFS mount, repair or pool-health
conclusion was involved. Host source snapshots did automount during inspection.

The fixed twenty-receipt harvest passed (`474f28`). Receipts establish normal
exact-owned MD/UFS cleanup and normal unmount of four newly automounted source
snapshots. Source GUIDs and zero-written checks remained valid; VM states were
unchanged. Existing snapshots, retention, ownership and replication sink were
not changed.

All four private controller databases have mode 0600, verified hashes and
immutable SQLite package queries. Their aggregate size is 58,458,112 bytes,
below the 128 MiB cap; each fits 32 MiB. Host metadata occupies 22,908,928 bytes,
below 160 MiB. Metadata directories remain for 168 hours. Database payloads,
binaries, keys and approval files are not stored here.

These files preserve actual preparation receipts and metadata. A source snapshot
may later be pruned or changed; execution must still fail closed on drift.
Task `yq3` prepares the replacement plan after the readiness/diagnostic fixes.
Actual native execution requires fresh approval of that exact plan.

## Replacement plan

The finalized [plan](reviewed-plan.json) has canonical hash
`fe727a549b3ddde47e13657ba566babc01be54063daadc5be06f81e86ae509fc`.
It was created at 09:07:47 UTC and expires 2026-10-31 at the same time. All thirty
artifact hashes, policy/readiness/native contexts, collection applicability,
phase deadlines and unchanged scope/budgets passed offline verification. The
actual read-only planning survey passed at 09:07:17 UTC and matches the final
targets/budget/policy subset. Its earlier wrapper assertion failed locally
before transport creation; that log is preserved separately.

The [review](review.txt) describes the clean Rocky downtime, twelve bounded
synthetic calls, normal cleanup/restoration, 87-minute maximum, 2.25 GiB disk
budget and 48 MiB logical I/O limit. It includes reviewed fixes `ex3` and `gx3`.
No n5 approval, controller state, execution evidence or native test was created.
Keep f3 running: a host restart or snapshot pruning/drift can invalidate these
source pins before plan expiry.
