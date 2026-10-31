# Retained 9p authorization decision proposal, 2026-10-31

**Unselected proposal for `jz3`; no security policy or publication changes.**
Keep FreeBSD HomelabFS FUSE → bhyve/lib9p → Rocky kernel 9p. The accepted
concession remains advisory locks between cooperating processes in the same
guest, same 9p mount/kernel inode domain. Independent admitted nodes may write
the same volume. The [all-origin contract](../spec/Replication-protocol.md)
requires their common committed view, including owner RAM before owner disk.

The proposed repair is **request-bound guest-kernel credential delegation**:
a negotiated extension of this 9p path plus an exporter-only control endpoint
on the real host FUSE connection. HomelabFS would authenticate the exporter,
validate its explicit guest-credential assertion and enforce shared Unix policy
against current committed logical metadata. This needs an explicit trust
decision and owned integration work; neither the stock stack nor the existing
`Credentials` provider implements it. The [compatibility design](Retained-9p-compatibility.md)
specifies framing, atomic metadata, sync, cache/mmap recalls and work packages.

## Evidence and its limits

The [deployed investigation](Deployed-9p-boundary.md#installed-code-and-source-binding)
records installed bytes and selected machine-code semantics. Recorded exporter
UID/GID zero and groups `0,0,5` identify bhyve, not a guest process. The inspected
ELRepo 6.1.190 candidate was unbooted at that observation. The Linux reference is
upstream `79807e970724c1f90cee1316361d61b80db4eef6`; the FreeBSD reference is
`aadd58dddcbc78f4d5594827b46b5633552b15ce`. Neither is a new runtime observation
or proof that every source path matches deployed compiler inputs.

| Boundary | Source-supported behavior | Required repair |
|---|---|---|
| Linux VFS | Uses current fsuid, fsgid and supplementary groups for Unix access. Fids are selected by UID, not complete changing credentials. | Capture credentials per authorization request, not once per attach; retain guest checks and LSM restrictions. |
| lib9p | Attach captures host-account groups; host syscalls execute as exporter. | Dedicated backend forwards explicit complete guest assertions; no host-account lookup as guest authorization. |
| Native FUSE/service | FUSE header equality authenticates the host caller; `UnixPermissions` checks logical metadata. | Preserve native equality checks and introduce explicitly delegated admission; never substitute guest IDs into host headers. |
| Create/setattr | Stock backend creates then repairs ownership/mode; setattr masks mode to `0777`. | One logical create/metadata command, final creator UID and parent GID, full supported mode, atomic committed visibility. |

Primary sources: [Linux VFS checks](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/fs/namei.c),
[9p fid selection](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/fs/9p/fid.c),
[dotl metadata](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/fs/9p/vfs_inode_dotl.c),
[lib9p backend](https://github.com/freebsd/freebsd-src/blob/aadd58dddcbc78f4d5594827b46b5633552b15ce/contrib/lib9p/backend/fs.c).
These support source assessment within the binding limits above. Current local
rules are in [adapter identity](../../internal/fuseadapter/adapter.go) and
[Unix authorization](../../internal/fsservice/permissions.go).

## Exact choice for project-owner review

**Proposed choice: trust explicitly admitted guest kernels/root to attest guest
credentials for authorized volume sessions; retain host service Unix
authorization of those asserted users.** Authenticate exact VM/exporter
incarnation, FUSE connection, volume/session, policy and serving generation.
The guest kernel supplies complete current numeric credentials and process/
description incarnations. Host service checks use those credentials and fresh
committed metadata. Exporter host-root credentials alone grant no guest operation.

Guest root/kernel, administrators able to replace the kernel and a compromised
exporter can forge identities inside granted exports. Authentication, framing
and a kernel-held secret cannot make a compromised guest kernel truthful.
There is no host-enforced per-user protection against that attacker. Volume
confinement, origin authority, durability, view barriers, limits and cross-volume
isolation remain mandatory; native users retain existing host-user policy.

This differs from `3z3`'s **guest-DAC-only export-principal** proposal: the host
would receive complete per-request assertions and recheck Unix policy. Both
trust the guest kernel as an identity source; neither can derive guest identity
from bhyve's host FUSE header. This choice also entails maintained Linux 9p and
lib9p extensions and an explicit FUSE/service delegation interface. It is a
security and maintenance decision, not implied by retaining 9p or accepting
local advisory locks. Subsequent installation, VM changes, tests and normal
publication retain their own gates.

If declined or deferred, keep current real-daemon publication refusal and
prepare the work packages for later review. That does not select another
transport or narrow the required final workload.

## Why the earlier workaround is superseded

Host-account group ceilings do not fix arbitrary groups. For non-owner mode
`0004`, a guest nonmember can read via other bits while a host member selects
restrictive group bits and is denied. Unioning groups is nonmonotonic. Two
same-UID processes can share fids despite differing groups. Root attach loses
creator identity; create-then-chown exposes intermediate ownership; `0777` loses
sticky/set-ID bits. Fixed rosters, preseeded trees, permissive modes and blanket
single-client publication cannot replace dynamic multiuser, namespace-inclusive,
multi-origin compatibility.

The earlier proposal remains in git history; its group-ceiling mechanism,
research subsets and exclusive-publication rules are superseded here. `zq3`
remains the real-daemon single-node base gate after required repairs. Full
distributed concurrent-client acceptance belongs to `ov3`/`pv3` after the core
protocol, without a dependency cycle through `zq3`. No base-gate pass, trust
selection, runtime compatibility or normal admission is claimed.
