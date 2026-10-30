# Stopped-image preparation for native run 20261030-n6

Task `mx3` refreshed source attestations after n5's guest lifecycle. This
preparation did not boot guests, execute a native test, create a canonical run
plan or record approval.

Actual host inventory at 09:39:34 UTC selected the existing ordinary snapshot
`zrepl_20261030_093853_000`. All four snapshots were created after n5's clean
guest shutdown. Their GUIDs and zero-written checks passed. Rocky remained
running as PID 31349, all four FreeBSD VMs were stopped, and host swap was zero.
Fresh pinned Rocky root SSH rederived its kernel and 1109-row RPM manifest.

Root reviewed constant-only R7-to-R8 differences and independently verified six
normalized syntax trees and baseline hashes. The source/resource guard passed
(`df38a7`). The exact released collector finished successfully (`session 38387`,
terminal `bf0739`); the fixed twenty-receipt harvest also passed (`1a9e11`).

Native image inspection used read-only vnode/UFS attachments, one at a time.
Development FreeBSD metadata used isolated sole-device userspace `zdb -L`, with
no guest pool import, guest ZFS mount, repair or pool-health conclusion. Host
source snapshots did automount. Exact-owned MD/UFS resources and four newly
automounted source snapshots were cleaned normally. Final source GUID/written0
and unchanged VM-state checks passed. No snapshot, hold, retention, ownership,
package, configuration, permission or replication-sink change was made.

Four private controller databases have mode 0600 and verified hashes. Package
manifests were independently rederived using immutable SQLite queries. Each
database fits 32 MiB and their aggregate 58,458,112 bytes fits 128 MiB. Host
metadata occupies 22,908,928 bytes, below 160 MiB; metadata remains for 168 hours.
Payload databases, binaries, private keys and approval files are not stored here.

These are preparation receipts, not a filesystem acceptance result. Source
pruning, writes or another host restart can invalidate admission. Task `yq3`
will prepare the replacement run after the reviewed fixes and local gates.
Actual native execution still requires fresh approval of its exact plan.
