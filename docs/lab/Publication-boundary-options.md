# Guest publication boundary: decision proposal

2026-10-30 · task `0z3` · **proposal, no architecture change or admission**

Recommend designing a Linux FUSE frontend beside the guest consumers, with an
explicit authenticated filesystem-service boundary to the FreeBSD storage owner.
This is substantial new protocol/security work, not a ready replacement. If that
cost or trusting an admitted guest frontend is unacceptable, keep guest
publication blocked. Neither the deployed 9p stack nor the maintained alternative
below satisfies the present contract by configuration alone.

The [deployed investigation](Deployed-9p-boundary.md) proves that the installed
lib9p lock callback reports success without locking. It also identifies missing
guest groups/process identity, stripped synchronous-open flags and retained-root
K04 gaps. These are stronger evidence than the historical passthrough PoC.
The actual Rocky boot lacks 9p; the installed candidate was not booted/admitted.
This comparison used local documents/source and public primary sources only;
no installation, live test, VM operation or publication occurred.

## Choices and disposition

| Choice | Implementable starting point | Decision under existing requirements |
|---|---|---|
| A. Keep current 9p for restricted research | Existing bhyve/lib9p and the unadmitted Linux candidate; a separately prepared, explicitly limited synthetic probe | Retain as optional research only. Cannot complete `zq3`, admit canaries or unblock replication. No real-daemon guest client is currently admitted. |
| B. FreeBSD Samba → Linux SMB3 client → consumers | Real maintained software: FreeBSD [net/samba423](https://cgit.freebsd.org/ports/tree/net/samba423/Makefile), upstream [release history](https://www.samba.org/samba/history/), Linux [CIFS/SMB3 implementation](https://docs.kernel.org/admin-guide/cifs/usage.html) | Technically available for directory sharing while preserving FreeBSD host/Rocky guest placement; reject as a transparent HomelabFS publication replacement. Session credentials and host smbd requests do not preserve the required guest request identity/lifetimes. It would require a different security contract and additional integration. |
| C. Linux HomelabFS FUSE frontend near consumers; FreeBSD owns service/journal/ZFS | Existing project Linux adapter and shared semantic API; remote filesystem-service dispatch is **unimplemented** | Recommended direction for a separately approved design prerequisite. Eliminates the lossy host sharing hop but adds a trusted client and remote service protocol. Does not itself pass any deployment gate. |

B is an available implementation, not an assertion that Samba is installed on f3,
that Rocky's exact kernel has usable CIFS modules, or that this composition has
passed tests. Its packages, dependencies, source/configuration and support policy
would need exact artifact admission. Samba's Unix credential switch uses the
authenticated session's Unix token ([4.23.0 uid.c](https://github.com/samba-team/samba/blob/samba-4.23.0/source3/smbd/uid.c),
[token construction](https://github.com/samba-team/samba/blob/samba-4.23.0/source3/auth/auth_util.c)).
Inference: even successful account mapping cannot distinguish two processes with
the same UID/session and different current supplementary groups, or turn smbd's
host FUSE PID into either guest process incarnation. Exporting the FUSE directory
does not add those facts to the request. Enrolling smbd root or guessing groups
would weaken the existing contract.

Virtio-fs is not a selected fourth option: Linux's [virtio-fs documentation](https://docs.kernel.org/filesystems/virtiofs.html)
describes its guest driver and QEMU/virtiofsd setup, while the inspected deployment
uses bhyve/lib9p. No available supported FreeBSD-host/bhyve virtio-fs combination
was established by this research. A Linux guest driver alone is insufficient.
Passing a zvol as a virtual disk likewise changes storage ownership and supplies
neither this shared filesystem nor its journal/replication semantics.

## Required properties across the boundary

“Required” in column C means design and acceptance work, not an implemented claim.
The unchanged authority is [Platform interfaces](../Platform-interfaces.md),
[FUSE adapter](../spec/FUSE-adapter.md), [durability](../spec/Durability.md),
[deployment tests](../Deployment-tests.md) and [kernel policy](../spec/Kernel-policy.md).

| Property | A: deployed 9p | B: Samba/SMB3 over host FUSE | C: proposed guest frontend/remote service |
|---|---|---|---|
| Trust and numeric credentials | Host bhyve credentials/host account lookup lose the complete guest request snapshot. | Authenticated SMB account/session mapped to a host token; host FUSE sees smbd. Numeric ownership display is not credential provenance. | Trust the admitted guest kernel/frontend to attest request UID, primary GID and **complete** groups; authenticate the frontend separately and authorize each volume/session. Host service enforces Unix policy, rather than trusting a supplied UID as authority. |
| Process incarnation, description and exit | Guest PID/exit are not authenticated to the provider; fid/host fd is not guest process or open-description identity. | SMB handles/session cleanup are not the current provider's guest process-exit, close-any-fd POSIX cleanup or dup/fork/descriptor-transfer model. | Namespaced, never-reused frontend/process incarnation and description IDs, explicit close/exit retirement, ordered cancellation and mapping lifetimes. Retain unknown/disconnected owners until safely retired/fenced; transport timeout is not process death. Arbitrary changing credentials/descriptor transfer still need a proven provider. |
| Lock domain | Installed successful no-op replies; guest-local exclusion cannot establish independent-client enforcement. | Real server locking exists, but SMB versus POSIX/flock ownership and host FUSE owner translation require proof. No `nobrl` workaround. | Owner service holds one ObjectID lock domain across sessions/aliases; preserve POSIX process owners separately from flock open descriptions. Deny unauthorized independent sessions and test authorized independent connections. No frontend-only lock manager. |
| Writes, sync and mmap | Stripped O_SYNC/O_DSYNC; fsync path exists but journal correlation unproven; mmap writeback loses original principal. | Client caching and SMB flush/host fsync are additional boundaries; no per-write or mmap guarantee follows from a successful share. | Keep ordinary write completion behind the shared commit boundary. Carry semantic sync modes, dirty-write grants and file/directory barriers end to end; MS_SYNC covers preceding mapped data. No guest journal ACK substitutes for owner commit. |
| Caches, revocation and generations | Existing guest pages and retained bhyve roots/fids are not fenced by a fresh path check. | Leases/oplocks and reconnectable handles add retained state; share disconnect does not prove cached reads/mappings stopped. | Bind requests/handles/grants to frontend session + serving generation; reject stale replies/replays. Fence the old frontend/consumer access including mappings before replacement serving; define mandatory stop/unmount/restart when cache revocation cannot be proven. No transparent reconnect promise. |
| K04 and publication | Pinned native mount check does not guard bhyve's retained root or ordinary-directory fallback. | Both exported host root and guest mount need generation checks; Samba provides no HomelabFS publication guard. | Verify exact native FUSE volume/generation/session through the pinned consumer publication descriptor; gate bind mounts/readiness and reject plain-directory substitution. Retained descriptors and mappings need the separate revocation fence, even if pathname readiness fails. |
| Namespaces and SELinux | Only initial-user-namespace diagnostic observed; pod path unadmitted. | SMB account/ID mapping and guest namespace mapping add distinct policies; label/permission compatibility unproven. | First prototype admits only initial user namespace with identity maps and fixed synthetic clients. Later pods require explicit UID/GID/fsGroup and namespace mappings, SELinux Enforcing policy/digests/audit evidence. Reject unsupported mappings and foreign credentials, never disable SELinux. |

The [project credential contract](../spec/FUSE-adapter.md#credentials-and-advisory-owners)
requires more than FUSE header UID/GID/PID; headers omit complete groups.
The current project provider therefore admits controlled, fixed-credential
children only. Moving it into Rocky improves proximity to those clients, not
arbitrary workload authentication. Linux's [FUSE I/O modes](https://docs.kernel.org/filesystems/fuse/fuse-io.html)
also distinguish writes sent to userspace from writeback cache acknowledgements.
Use the existing conservative buffered profile with writeback disabled and
verified invalidation; do not infer that zero metadata TTL prevents cached reads.

For B, Linux documents byte-range lock requests and fsync-to-SMB-flush behavior;
Samba documents honoring flush with `strict sync` and a separate `sync always`
policy. Its durable-handle settings can disable interoperability with local
POSIX locking. Those are useful controls, not proof of HomelabFS's full barrier,
description or lock-owner contract ([Linux client](https://docs.kernel.org/admin-guide/cifs/usage.html),
[Samba configuration](https://www.samba.org/samba/docs/current/man-html/smb.conf.5.html)).

## Decision requested before implementation

Choose **C: authorize specification of guest-side Linux FUSE plus an authenticated
remote filesystem-service boundary, preserving the present semantics**. This
explicitly revises the selected f3s path in [Architecture](../Architecture.md):
today it says no Linux daemon in that path. It also makes an admitted guest
frontend/kernel a credential-attesting security principal for only its authorized
volumes. A compromised admitted guest could forge those workload credentials;
channel authentication cannot prove the guest's process state independently.
If this trust expansion is unacceptable, choose **hold guest publication**;
optional A research remains incomplete, and B is not a semantics-preserving escape.

Before: Rocky consumers → guest 9p → bhyve/lib9p → FreeBSD FUSE → shared service/ZFS.
Proposed: Rocky consumers → Linux FUSE/frontend → authenticated remote service →
FreeBSD shared service/journal/ZFS. Intended durable owner/partner copies remain
on FreeBSD ZFS; the guest frontend is not another durable replica or journal owner.
The existing local-only daemon and replication transport do not implement this path.

The follow-up design must choose authenticated frontend enrollment/revocation,
volume roles and protected channel keys, binding to boot/client/serving
incarnations, and replay/duplicate-result rules. Existing node replication
transport is not automatically a workload RPC authorization policy. Keep the
FreeBSD service as sole namespace/permission/lock/commit authority and journal
owner; Linux translates kernel requests, attests permitted callers and manages
kernel cache/lifetime events. It must not access raw ZFS paths or create a second
filesystem/replication state machine. Separate request/handle/control messages
with finite sizes, in-flight limits and cancellation; transport loss yields
bounded errors/uncertainty, never acknowledged local-only writes or premature
lock release. This is new design/implementation scope, not a small adapter toggle.

No selection is made by this proposal. After an explicit decision, update the
architecture, platform/deployment/kernel manifests and prerequisite task wording
coherently before implementation. Keep `zq3`/`4r3` blocked until an explicitly
revised equivalent end-to-end acceptance gate passes; do not label Linux results
as a 9p pass. Native FreeBSD acceptance remains separate and unchanged.

## Smallest subsequent disposable prototype

First deliver a reviewed, bounded remote-service protocol/authorization spec and
negative cases. Only then implement a validation-only experiment using the real
shared service; no general RPC framework, vendor patch or normal publication.
Use two separately authenticated frontend **connections** from controlled Linux
processes in one admitted Rocky guest, one owner service on f3 and a fresh
run-owned synthetic volume. Two FUSE connections exercise server lock/session
identity; they are not two kernels or proof of partition/failover fencing.

Proposed ceiling: 4 MiB logical payload, 256 MiB total run storage including
data/journal/control/evidence, two frontend sessions, eight helpers/32 open
descriptions total, 16 in-flight requests and 1 MiB aggregate queued payload,
ten minutes including collection. Enforce actual disk/process/memory bounds;
reduce the fixture or stop on exhaustion. Admit at most 256 MiB additional host
RSS and 256 MiB guest RSS, plus measured existing ARC/VM/headroom. These are
proposal ceilings, not measured spare capacity. Retained failed resources and
snapshots count against admission, not just the logical-data cap.

Before a run, inspect actual f3 state/resources using pinned SSH via f3 and the
infrastructure skill. The recorded Rocky 12 GiB running/all FreeBSD VMs stopped
state is historical. Never boot the three 2 GiB guests alongside Rocky; no new
guest, production r-node, sink, network policy or lifecycle change is implicit.
Rocky's exact FUSE/kernel/module/daemon and host packages, trust enforcement,
SELinux policy, connection keys and rollback artifacts require a complete pinned
[manifest and target record](../spec/Kernel-policy.md). Existing Linux compilation
or native lab results do not admit Rocky FUSE or its pod runtime.

Acceptance proceeds from fixed synthetic clients: same UID with different full
group vectors and denial cases; foreign/session-replayed requests; close-any-fd
POSIX retirement versus dup/mapping flock retention; independent connections
contending on the same object; O_SYNC/O_DSYNC, fsync/fdatasync/directory-fsync and
MS_SYNC with owner journal evidence; lost replies and duplicate requests; owner
restart rejecting old generations; K04 with new opens and retained descriptors;
cached pages and mappings denied/fenced before replacement publication. For a
single-node experiment, owner journal replay proves only local durability;
partner-memory ACK and full normal replication remain unimplemented/unproven.
Failure to implement any needed prerequisite stops that case rather than
replacing it with a mocked pass. A same-guest connection test cannot certify
independent-guest failure isolation; budget an isolated second target later.
Pods, physical-host failure and the complete K01–K05 matrix remain separate gates.

Rollback closes admission first, retires synthetic clients normally, unmounts
both guest FUSE connections normally and stops the run-owned owner service;
collect identities, receipts, journals and residual-resource inventory. Preserve
failures and leave publication closed if cleanup/fencing is uncertain. Restore
the recorded pre-run state and retained package/configuration artifacts through
the reviewed plan; no automatic raw-directory/9p fallback, forced unmount,
production PV/NFS change or deletion of failed resources. No lab recovery copy
or received sink is booted/modified. The exact execution plan remains subject to
the user's applicable run authorization and [runner rules](../Test-automation.md).
