# Stopped-image preparation for native run 20261030-n18

Task `yy3` refreshed stopped-image attestations after n17 passed both root
fixtures and unprivileged/direct on hfs-test0, then stopped in the buffered
unprivileged namespace fixture with EIO and service unavailability. N17 normal
lab stop completed at 16:45:17.057469378 UTC. The complete native gate remains
pending. This preparation gives no native acceptance, pool-health or SSH-cause
claim; `xy3` harness diagnosis and `yq3` full native gate are separate work.

Actual pinned inventory `b2509f` completed at 17:00:40 UTC and selected existing
ordinary snapshot `zrepl_20261030_165854_000`, created at 16:58:54 UTC strictly
after n17's normal lab shutdown. All four source GUID/written0 checks passed.
Rocky remained running as PID 76196, all four FreeBSD VMs were stopped, and
host swap was zero. Fresh pinned Rocky root query `97396` (`1b4f9d0`) completed
at 17:01:57 UTC, rederiving kernel `5.14.0-687.61.1.el9_8.x86_64` and the actual
1109-row RPM manifest, retaining integer UID zero and `package_count`.

Root reviewed six R17-to-R18 helper hashes, normalized syntax trees and exact
allowed literal substitutions. Local verification independently repeated those
checks and verified five actual operation receipts, helper/proposal pins and
captured stream/log hashes before immutable SQLite reads. Actual resource/source
guard passed at 17:01:57 UTC; collector `76875` (`a1d82a0`) exited zero at
17:04:20 UTC, and fixed twenty-receipt harvest `a15b290` passed at 17:06:16 UTC.

Native OS metadata used read-only vnode/UFS attachments one at a time;
development FreeBSD used isolated sole-device userspace `zdb -L`. No guest
code, guest pool import/mount, repair or pool-health claim was involved.
Exact-owned MD/UFS resources and four new source snapshot automounts were
cleaned normally; every baseline host mount path remained. Final source
GUID/written0 and VM-state guards passed. No snapshot creation, hold, retention,
ownership, package, VM configuration or replication-sink change was made.

Four private databases have mode 0600 and verified hashes. Independent immutable
SQLite queries freshly rederived manifests with database hashes unchanged
before and after. Each fits 32 MiB; aggregate 58,458,112 bytes fits 128 MiB.
Host metadata uses 22,908,928 bytes, below 160 MiB, retained for 168 hours.
This directory contains 22 preparation proofs plus this README; no database
payloads, binaries, keys, approval or native run state are included.

Snapshot pruning or source writes can invalidate admission. A future native
plan must retain fail-closed checks and requires its own applicable approval.
No native plan or test was created by this preparation.

## Reviewed replacement run

Canonical plan `62241f759525964371c7c83bc35bb2dc085c14168e3bc32593a258ec4e04ef5c`
uses fresh R18 metadata and new actual clean f5d36fb FreeBSD binaries. The
reviewed harness separates 256 KiB stdout/stderr capture from a 128 MiB child
file soft limit within the existing dataset quota, preserving the inherited
hard limit, 95+10-second supervision, single TERM and live-child preservation.
The native specification records this correction explicitly. Twelve fixtures,
eight-second case bound, 87-minute outer budget, 2.25 GiB disk, 48 MiB logical
I/O and normal restoration remain unchanged. Actual admission and all thirty
artifact checks passed. Native confirmation of the harness correction and the
complete matrix remain pending; no N17 source-size observation is claimed.
