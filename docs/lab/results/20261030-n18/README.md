# Native single-node FUSE gate, 20261030-n18

**All twelve native fixtures passed: 159 checks passed, 21 expected skips,
zero failures.** Canonical plan:
`62241f759525964371c7c83bc35bb2dc085c14168e3bc32593a258ec4e04ef5c`.
The standing-authorized invocation (`80175`, terminal `33fda5`) ran
17:20:16.860–17:23:57.152 UTC and exited 0 after 220.292 seconds.
The original report is `complete=true`, `success=true`.

Each of hfs-test0, hfs-test1 and hfs-test2 ran root/direct, root/buffered,
unprivileged/direct and unprivileged/buffered against its own guest FUSE
and ZFS-backed single-node service. Direct profiles passed append and skip mmap;
buffered profiles passed mmap and skip unsupported append. Root/buffered provides
the root-only multiuser companion. The 21 skips match that declared profile
split; none replaces a required check. Every fixture completed fifteen case
results, normal unmount and no-fallback checks. All child exits were zero with
no timeout, TERM, preserved live process, capture overflow or capture error.
Recorded stdout/stderr sizes match retained bytes and stay under 256 KiB each.

Unprivileged append took 4.378, 4.192 and 4.378 seconds across the guests,
inside the unchanged eight-second bound. All twelve process-lock cases passed.
These results retain the full content, identity, permission, cache, mmap, sync,
lock and lifecycle assertions; no timeout or workload was reduced for a pass.
[Offline revalidation](offline-native-acceptance.json) reran the real report
validator for all twelve collected fixtures without another test execution.

The f5d36fb harness separates evidence caps from legitimate journal/data writes.
Its local real-OS regression reproduces EFBIG followed by IO/UNAVAILABLE under
the former 256 KiB process-wide cap and succeeds with the corrected limit.
Independent read-only observation (`c1ad50`) of the exactly enrolled retained
N17 source found its sole open journal at 262,107 bytes, only 37 bytes below
that cap. That corroborates the controlled mechanism; no raw N17 EFBIG syscall
was recorded. The corrected N18 harness completes the formerly failing fourth
fixture and all remaining fixtures. No filesystem semantics or durability
barriers were relaxed to correct the supervising limit.

Every original admission, graceful lifecycle, readiness, setup, workload,
collection, normal cleanup, shutdown and Rocky restoration/readiness step passed.
No ambiguous outcome, reconciliation, forced operation or workload retry occurred.
Lab shutdown finished at 17:23:38.105035226 UTC; restoration readiness finished
at 17:23:57.142096615 UTC. A fresh pinned inventory (`d4c198`) confirmed Rocky
PID 91477 running and all four FreeBSD VMs stopped. Run-owned roots/datasets
remain under the 168-hour evidence policy; all thirty original artifact bytes
are privately preserved with mode 0600 before further source changes.

This is the native half of the single-node gate. Real-daemon 9p deployment
compatibility (`zq3`), replication, distributed ownership/recovery and canary
acceptance remain separate. No production workload migration or NFS change
occurred. The earlier intermittent SSH failure cause remains unresolved (`uy3`).
