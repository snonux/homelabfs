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

## Replacement plan

The finalized [plan](reviewed-plan.json) has canonical hash
`7b3076c5025a192f99003a488ad31877d49102dd4c1fcbe00b5e9957cc449a94`.
It was created at 10:31:17 UTC and expires 2026-10-31 at the same time. It pins
reviewed fixes `ix3`, `jx3`, `kx3` and `lx3`. All thirty artifact hashes and the
actual policy/readiness/native contexts passed offline verification. Root
independently confirmed the canonical hash, artifact bytes, unchanged scope and
exact final survey subset.

The actual read-only planning survey passed at 10:30:48 UTC with Rocky running,
all four FreeBSD guests stopped and zero active sessions. Its separate stderr
contains two OpenSSH key-exchange warnings. This survey did not execute native
workloads or change VM state.

The [review](review.txt) describes clean Rocky downtime, twelve sequential
synthetic behavior calls, normal cleanup/restoration, an 87-minute maximum,
2.25 GiB disk and 48 MiB logical I/O. The outer plan has one driver selection
covering all twelve calls. Native binaries retain their actual clean `127ecc9`
build identity; current production Go sources are unchanged apart from test
files. No n6 approval, controller state or native execution evidence exists at
publication. Fresh explicit approval of this exact plan remains required.
Source pruning/drift or another host restart can invalidate admission before
plan expiry; the live execution preflight must fail closed.
