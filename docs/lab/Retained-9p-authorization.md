# Retained 9p authorization proposal, 2026-10-30

**Decision input, not an admitted security model.** Retaining 9p and accepting
[same-mount local advisory locks](../Platform-interfaces.md#accepted-9p-lock-scope-2026-10-30)
does not authorize treating bhyve's host root identity as a guest user. The
selected stack can enforce complete guest group checks in the trusted Linux
kernel, but cannot supply those credentials to the current HomelabFS service.
A separate, explicit VM/export authority model is a possible validation design;
it changes who authorizes guest users. It also leaves lib9p's independent group
checks and metadata mismatches to resolve. It is not a full multiuser solution.
Unchanged lib9p cannot satisfy unrestricted same-UID dynamic-group and arbitrary
Unix-mode semantics: the group-class counterexample and mode masking below are
known incompatibilities. Selecting guest trust or supplying a ceiling manifest
does not fix them or accept additional group/metadata concessions.

Task `3z3` supplies the authorization proposal for `zq3`, following `zy3` and
`1z3`. No runtime, code, task state or normal-publication capability changes here.
`zq3` remains incomplete and `4r3` blocked. The Linux frontend/RPC alternative
was not selected. Durability is a [separate prerequisite](Retained-9p-sync.md).

## Evidence and limits

The [deployed investigation](Deployed-9p-boundary.md#installed-code-and-source-binding)
binds the observed FreeBSD revision, installed bhyve/lib9p bytes and selected
machine-code semantics. Its host observation found bhyve UID/GID zero, groups
`0,0,5`; these are exporter credentials, not guest credentials. Installed
6.1.190 is an unbooted candidate in that observation. This document adds pinned
source analysis, not a new target inspection or a source-to-binary attestation.
All recorded VM state remains historical.

Linux references below pin commit `79807e970724c1f90cee1316361d61b80db4eef6`
(upstream 6.1.190). FreeBSD references pin observed revision
`aadd58dddcbc78f4d5594827b46b5633552b15ce`. The earlier report explains ELRepo
recipe provenance and the limits of matching upstream source to installed RPMs.
Exact booted kernel/modules and the specific permission paths still need live
validation before admission under [Kernel policy](../spec/Kernel-policy.md).

## Where authorization actually happens

| Layer | Actual source path | Security consequence |
|---|---|---|
| Linux pathname/open checks | The dotl inode-operation tables have no `.permission` method. VFS `do_inode_permission` therefore calls `generic_permission`, whose mode check uses current fsuid and `in_group_p` (fsgid plus supplementary groups), followed by Linux capability overrides. Path search, parent mutation and ordinary open checks run in this kernel. | This is the layer with the actual complete guest process group vector. Linux capability/root and LSM rules apply; they are not identical to HomelabFS's UID-zero-only service policy. |
| Access mode | Session initialization defaults 9P2000.L to `access=client`. Explicit `user` and `client` select fids by current fsuid; `any` permits reuse of the session UID's fid; numeric `access=<uid>` restricts attachment to that UID. | In this pinned dotl implementation `user`/`any` do **not** install an alternate permission callback or disable VFS mode checks. Switching access mode cannot forward complete groups. Prefer `client` when retaining per-UID attach metadata; `any` is only a separately declared export-principal experiment, not guest identity preservation. |
| ACLs | POSIX ACL client enforcement requires dotl, `access=client`, `posixacl` and the corresponding compiled support. The ACL loader otherwise installs null ACLs; enabled ACLs fetch/cache `system.posix_acl_access/default` xattrs. | `access=client` alone does not enable ACLs. Proposed initial scope has no ACLs, and refuses ACL-bearing data/ACL mutation. ACL interoperability is unproved and outside the current service policy. |
| Attributes and chmod/chown | Dotl getattr fetches server attributes except the loose/fscache shortcut. Setattr calls `setattr_prepare(&init_user_ns,...)` before TSETATTR; VFS/attr code checks ownership, group membership and capabilities. | Fresh correct numeric metadata is essential to guest decisions. This is guest authorization of chmod/chown, not a request-bound proof delivered to the host. Inode/dentry reuse and permission invalidation still need actual cache tests. |
| lib9p attachment | `fs_attach` accepts numeric `n_uname`, looks up that UID in the **host** passwd database, and captures `l9p_getgrlist` from its host account; an unknown numeric UID can attach with zero groups. Authinfo is retained by fids. | No complete guest group vector, per-operation group change or guest process incarnation enters this authinfo. Numeric UID attachment is a claim from the guest transport, not independently authenticated per-user authority. |
| lib9p secondary checks | `fs_iopen`, walk, parent creation and namespace operations call `check_access` with attach UID and host-derived groups; some operations supply a wire effective GID. Mode fallback allows superuser; available host ACLs can replace mode checks. | The secondary check can deny a guest-authorized action or allow an action forbidden by the guest group set. It cannot replace guest VFS authorization. ACLs can add another mismatch. |
| Host syscalls | lib9p uses ordinary `openat`, `pwritev`, `fchmodat`, `fchownat`, etc.; it does not switch process credentials to attach authinfo. Dotl `fs_setattr` issues chmod/chown syscalls without an attach-owner permission check. | FreeBSD kernel/FUSE sees actual exporter credentials. In the observed deployment these are root. Host `default_permissions` checks that principal; it cannot enforce missing guest credentials or repair a malicious guest's setattr request. |
| HomelabFS | Adapter `identity` insists on exact FUSE UID/GID/PID equality and complete verified host groups. `UnixPermissions` checks fresh logical metadata and records authorization before mutation. | It can authenticate the exporter process, not the guest user. Substituting guest IDs, looking up groups by guest UID, or reporting guest process exit would fabricate provenance. |

Primary Linux source: [dotl inode operations, getattr/setattr and creation](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/fs/9p/vfs_inode_dotl.c),
[VFS mode, capability and security checks](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/fs/namei.c),
[attribute authorization](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/fs/attr.c),
[session/access selection](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/fs/9p/v9fs.c),
[fid selection/writeback](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/fs/9p/fid.c),
[ACL behavior](https://github.com/gregkh/linux/blob/79807e970724c1f90cee1316361d61b80db4eef6/fs/9p/acl.c).
Host paths above are in [pinned lib9p fs.c](https://github.com/freebsd/freebsd-src/blob/aadd58dddcbc78f4d5594827b46b5633552b15ce/contrib/lib9p/backend/fs.c).
These links describe source behavior; the installed-byte proof covers the
particular semantics documented in the earlier report, not every new path here.

### Why mirroring accounts or using one root attach is insufficient

For a non-owner file `0640`, guest UID A with supplementary group G may read,
but lib9p denies if A's host account lacks G. The reverse host membership may
allow at lib9p while the trusted guest kernel denies. Such a host allow does not
bypass an intact guest check; it becomes a false allow if treated as the complete
authorization decision, or if an untrusted guest can issue protocol requests.

Taking a union of all possible groups is not a general remedy: for mode `0004`,
a host group member selects the restrictive group class while a guest nonmember
selects readable other bits. Even a superset can cause false denial. Two guest
processes with the same fsuid and different supplementary groups reuse UID-scoped
fids; changing the host account later does not refresh retained authinfo. Exact
static account mirroring can support a bounded fixture, not arbitrary
`setgroups`, differing pod group sets, or changed groups under one UID.

A shared root attach (`access=any,uname=root` with the initial attachment
verified to resolve to host UID zero) can avoid mode-based secondary group
false-denials while guest VFS still checks each real process. Initial attachment
uses the mount username and an unspecified numeric UID; `any` reuses that fid.
`dfltuid=0` alone does not select root attachment. ACLs must be absent because
lib9p ACL behavior need not give root the same bypass. New objects take attach
UID and wire/default GID: root attachment loses creator ownership. Thus it is a
useful **preseeded data-only** experiment below, not a general numeric-identity
solution. World-writable exports hide the group problem and are not a multiuser
acceptance fixture.

Creation is a separate issue even with per-UID attachment: `fs_icreate` first
creates using host syscalls, then fchmod/fchown repairs ownership from attach UID
and supplied GID. HomelabFS initially sees host exporter ownership and separate
metadata mutations. Intermediate visibility, crash/failure cleanup, special-bit
handling and the service rule of always inheriting parent GID require explicit
compatibility evidence. Dotl `fs_setattr` masks requested mode to `0777`; sticky
and set-ID preservation cannot be presumed. A trust revision cannot fix those
semantic differences. No full ownership/chmod/chown compatibility is claimed.

## Proposed explicit boundary: a trusted VM/export principal

This is a **model revision requiring selection**, not a permission waiver already
implied by retaining 9p. The guest kernel would become responsible for complete
guest-user Unix authorization, namespace/capability checks, guest LSM policy,
and local process/descriptor/lock lifecycle. The host would authenticate and
authorize one VM/export principal for one volume session. It would enforce
volume confinement, serving generation, handle validity, resource bounds,
mutation/durability gates and audit provenance, without claiming to recheck
guests' supplementary groups.

Guest root, the guest kernel and administrators able to replace that kernel,
create privileged mounts, alter guest credentials or issue raw 9p requests are
therefore trusted with that entire export. They can impersonate guest UIDs and
bypass guest DAC; this design offers no host-enforced per-user protection against
a compromised guest kernel/root. Although that trust is already suggested by
lib9p's numeric attach and root syscalls, the existing HomelabFS contract promises
stronger request-bound service enforcement. Making the weaker boundary explicit
is a security change. A compromised exporter or host remains trusted as well.
The authority must not extend to any other volume or private backing/control path.

Proposed narrow contract wording for a later reviewed spec change:

> For the explicitly named disposable 9p export profile, HomelabFS authenticates
> the actual host exporter and authorizes that principal for one immutable
> volume/session/generation. The trusted guest kernel authorizes guest users
> using their complete current numeric credentials. Exporter credentials are
> recorded as exporter credentials and never represented as guest identities.
> Native FUSE and the ordinary service retain request-bound per-user Unix policy.
> No normal publication or compatibility admission follows from this profile.

An implementation proposal after selection must do all of the following:

1. Introduce a separately named, disposable-only export profile and explicit
   authority type, not an ordinary `Credentials` provider pretending to return
   guest users. Preserve exact FUSE header equality with actual host exporter
   UID/GID/PID/groups. Retain an unreusable exporter incarnation and revoke on
   exit; PID/account lookup alone is insufficient. Existing `Participants` is
   not an arbitrary bhyve allowlist: children must start idle before mount and
   cannot exec/change credentials. A real exporter supervisor needs a reviewed
   launch/ready handshake and graceful lifecycle; no retrofit enrollment of the
   running root bhyve or force-kill error path.
2. Bind the authority to immutable run ID, VM/configuration identity, export tag
   and root, volume UUID, native FUSE connection/session, serving generation,
   exporter incarnation and exactly one guest mount/publication. Reject reused
   PIDs, handles, exports and tokens after restart. A pathname or UID is not an
   authority. Never expose backing files, journal or control interfaces.
3. Name the authorization policy separately from native Unix policy, with a
   distinct policy revision and journal/audit provenance identifying the actual
   export authority. A proposal may delegate guest DAC for this one disposable
   volume; it must say so explicitly. Do not feed synthetic guest credentials
   into native `UnixPermissions`, disable it globally, or silently rely on its
   UID-zero bypass. Current construction requires built-in UnixPermissions;
   no existing constructor/CLI admits this alternative. Any new scoped service
   construction must keep authority/apply/completion/handle gates mandatory.
4. Keep native FUSE and shared-service `UnixPermissions` revision 1 unchanged:
   request-bound actual user/groups, fresh logical checks and normal publication
   refusal remain mandatory there. The alternative profile must be impossible
   to select through existing self-client/multiuser/native flags or normal mode.
5. Canonical namespace-inclusive validation prototype: pin `version=9p2000.L,access=client`,
   exact cache options and initial-namespace identity maps. Supply a run manifest
   mapping every admitted guest numeric UID to an explicit host account, primary
   GID and fixed supplementary-group ceiling. Reject missing accounts, failed
   lookup, missing required groups or database/configuration drift; never guess
   an account from a current guest UID. Preserve per-UID attach ownership and
   wire effective GID on creation. Complete **actual** guest groups still
   authorize in guest VFS; the host ceiling is only lib9p's secondary allowance,
   not a request-bound snapshot. Pin fixture credentials and allowed credential
   transitions, including two processes sharing one UID but differing groups.
   Keep root trusted for the admitted export and exclude ACLs initially.
6. Validate the ceiling against actual modes and each namespace/metadata
   operation, not just set inclusion. Group permissions do not monotonically
   increase with group membership (`0004` above). The real validation matrix
   must include this counterexample, same-UID different-group operations,
   sticky/set-ID setattr and split creation ownership, retaining expected
   failures as compatibility blockers. Do not choose a roster or mode mask
   that removes them and then report full acceptance. An optional restricted
   research subset may use group bits containing other bits and a bounded
   mode/ownership roster, but cannot replace the required end state. Resolve
   each required mismatch or obtain a separate explicit contract decision;
   selecting this authority model does not decide those concessions.

The minimal code change is a distinct disposable export configuration plus
export-authority admission/supervision, explicit UID/account/group-ceiling
manifest validation, a separately versioned service policy and explicit
provenance, and an independently reviewed
mount guard. Buffered admission currently requires a sealed `Participants`
provider in `Adapter.New`; introduce an explicit alternative only for this named
profile, with its own exact connection binding and lifecycle checks. Do not
remove the native requirement or reuse `BufferedValidation` to imply support.
Keep host `default_permissions`: it checks actual root exporter credentials,
while the explicit export policy restricts its authority. This is a concrete
namespace-inclusive validation prototype without a new transport or vendor
patch. It exercises the API as an intermediate step with known expected
compatibility failures; it is not a full `zq3` solution. Preserve create/mkdir/link/rename/unlink and
chmod/chown/timestamp cases in the prototype rather than replacing them with a
root-only data probe. Ceiling limitations and split creation metadata remain
required acceptance checks. Unchanged service authorization cannot authenticate
missing guest users.
These limitations must remain visible in results and deployment admission.

### Export confinement is mandatory even for trusted guest root

Guest-root trust grants only the admitted volume, never arbitrary host-root
filesystem access. Pinned [bhyve export initialization](https://github.com/freebsd/freebsd-src/blob/aadd58dddcbc78f4d5594827b46b5633552b15ce/usr.sbin/bhyve/pci_virtio_9p.c)
opens the configured directory, limits its descriptor with Capsicum rights
(including lookup and substantial mutation rights), and passes that descriptor
to lib9p. `fs_walk` rejects slash-bearing components, handles `..` in path
construction and uses no-follow stat; `fs_iopen` rejects a final symlink and
checks device/inode identity after open. Other paths use concatenated relative
names, and setattr truncation explicitly notes a symlink concern in source.
These are mechanisms to assess, not proof that every escape/race is impossible.
Descriptor rights limiting is not an assertion that rootfd is a chroot or that
every process descriptor/global namespace is safely confined. No new binary
confinement proof or actual escape test has been performed here.

The staged prototype starts with a preseeded tree, then tests namespace mutation
and symlink behavior before claiming those operations. The optional root-attach
data-only diagnostic denies namespace mutation. Check guest root `..`, absolute names, symlinks
pointing outside the export, a sibling synthetic-volume sentinel and foreign
object/handle IDs: none may expose or mutate anything outside the admitted
volume. Check host authority object membership even when PID/UID match. Test
second writable publication refusal separately. Any uncertain confinement blocks
publication; trusting the guest kernel does not waive this boundary.

## Handles, namespaces and publication remain independent gates

Host FUSE handles identify lib9p's host open descriptions, not guest processes.
Keep object/volume/generation/session-bound handles and granted descriptor access
across chmod/unlink. A guest final fid clunk closes its host fd; guest process
exit need not produce that event while another guest descriptor retains the fid.
Do not send synthetic per-guest `ProcessExit` or map all guest advisory owners
to bhyve. Under the accepted contract Linux alone supplies POSIX/flock ownership,
close-any-fd/last-description/exit behavior inside the same mount/inode domain;
host server lock success supplies no such proof. Independently mounted clients
and host direct access cannot become concurrent consumers of that writable volume.

Initial research can pin identity maps in the initial user namespace. Pod UID,
GID, `supplementalGroups`, `fsGroup`, capabilities, user-namespace maps and actual
process groups must later be recorded and checked inside the isolated pod.
Container root may map to a non-root initial-namespace UID; names and pod IDs do
not authenticate wire IDs. Bind pods to the same underlying admitted 9p mount,
not fresh mounts. SELinux Enforcing must remain active with pinned labels,
mount/pod policy and allow/deny/audit evidence. It is an additional guest control,
not a host-verifiable group snapshot. Plain Rocky results cannot stand in for
pod or namespace acceptance.

Before publication, a guard must verify the real host FUSE identity through the
export and guest mount, root metadata identity, expected volume/generation/session
and exclusive client assignment. The bhyve retained rootfd and all fids must be
accounted for. Refuse plain-directory fallback on either side (K04); no writable
underlying fallback directory can be exposed during startup/recovery. Exact
service authority must fail closed after host mount/session loss.

Revocation also needs an operational fence on the old guest's published clients,
open files and cached mmap pages before admitting a successor. Replacing an
export path, restarting HomelabFS, or remounting only a new client does not revoke
old cached reads/stores. Guest-root trust does not make cached revocation
instantaneous. Prove the required quiesce, unpublish, normal unmount and, if
necessary, graceful VM shutdown sequence. Preserve failed/busy resources and
refuse successor publication when that fence cannot be established. These are
unimplemented prerequisites, not instructions to alter a running VM here.

## Smallest useful implementation and acceptance sequence

After explicit model selection, implement **only** one run-owned synthetic
volume/export and a fixed fixture roster using the canonical `access=client`
profile. Include two non-root UIDs, distinct primary groups and one
supplementary-only group. Pin actual host exporter credentials/incarnation and
the exact UID/account/group-ceiling manifest; preserve restrictive guest-visible
ownership and modes. Cover ordinary I/O **and** namespace/ownership operations,
including created objects, without normal CLI or arbitrary workloads. Root is
an administrative fixture, not the only caller. No `0777` substitute is allowed.

The smallest staged validation starts with known restrictive objects and extends
to actual create/chmod/chown/rename/unlink in the same declared profile. A
separate `access=any,uname=root` data-only diagnostic can isolate guest group
checks from lib9p ceiling checks; it must refuse creation/metadata mutation and
cannot substitute for the canonical namespace-inclusive validation prototype.
The required end state includes namespace and metadata compatibility, rather
than stopping at preseeded data. Existing startup/
admission, exporter supervision, explicit service-profile construction and
publication guards need implementation and local review before live setup.
No tests were executed for this document.

| Fixture | Required outcome and scope |
|---|---|
| Complete guest groups | Group-only `0640` read / `0660` write and parent search succeed for the supplementary member; same UID without that group fails at guest VFS. Record actual groups and correlate allowed calls with host exporter identity, never a fabricated guest FUSE header. |
| Secondary-check mismatch | Deliberately missing host group causes expected guest-allow/host-deny; excess host group plus `0004` demonstrates possible false denial. Restore exact records in the reviewed fixture sequence. These are incompatibility diagnostics, not acceptance passes for arbitrary groups. |
| Ownership and modes | Owner/non-owner chmod and allowed/denied chown/chgrp; new create ownership, parent GID, sticky directory, set-ID clearing, rename/unlink, timestamp and failed partial create behavior compared with the native service contract. Unsupported or mismatching cases remain blocking for that claimed profile. |
| Credential changes | Two simultaneous processes sharing a UID but differing groups; change groups and reuse an existing fid, then open anew. Guest allow/deny must follow each complete group set despite UID-scoped fids and the fixed host ceiling. Required positive operations denied by the host ceiling or stale authinfo remain full-profile failures; the separate root-attach diagnostic can isolate this cause. |
| Incarnation/authority | Unknown exporter, changed groups, reused PID, wrong VM/export/volume/session/generation and stale handles fail before mutation. Host-root credentials without the enrolled exact export authority do not gain admission. Normal CLI and native behavior retain existing refusal/checks. |
| Local lifecycle | Same-mount POSIX/flock contention, GETLK, blocking/nonblocking, dup/fork/transfer, close and exit, including pods sharing that mount, follow the accepted local contract. No guest process-lifetime claim is inferred from host releases. |
| Publication/fence | Second writable client denied; host or guest mount loss cannot write a fallback; retained fds and cached mappings cannot survive successor admission outside the declared fence. Wrong incarnation cannot reacquire authority. |
| Pod/SELinux | Exact isolated pod namespace/credential/capability and SELinux allow/deny/audit fixtures pass before any pod compatibility claim. Missing runtime/policy fixture is incomplete, not skipped-as-pass. |

The first staged fixed-fixture research run may cover only the first rows after its
specific prerequisites; report its exact subset and remaining failures. It does
not complete `zq3`, admit dynamic multiuser canaries, or release replication work.
Full acceptance also requires [Configuration](../spec/Configuration.md),
[FUSE adapter](../spec/FUSE-adapter.md), [Filesystem service](../spec/Filesystem-service.md),
[Durability](../spec/Durability.md), [Volumes](../spec/Volumes.md),
[Security](../Security.md) and the complete
[real-daemon preparation matrix](Real-daemon-9p-preparation.md), with any selected
9p-specific revisions explicit and reviewed. Native policy is not weakened by
passing the subset.

## Bounded decision for the project owner

- **Select the explicit trusted-guest export model for bounded validation.**
  Trust the guest kernel/root to authorize guest Unix users and let the host
  authorize the exact VM/export principal for one volume/session. Native policy
  remains unchanged. Authorize the separate disposable profile and the
  namespace-inclusive non-root fixture with an explicit host group ceiling.
  Known group/metadata incompatibilities remain mandatory negative evidence;
  this choice accepts no additional group or metadata concessions. No full
  compatibility pass is possible until those semantics are fixed or separately
  decided, and no run or normal publication is admitted by the design choice.
- **Keep the existing per-user host service contract for 9p.** Preserve the
  current refusal of real-daemon guest publication. With this unmodified stack,
  complete request-bound guest groups/process identity are unavailable; there
  is no honest enrollment-only implementation of the required full profile.

Neither choice selects Linux FUSE/RPC or a vendor patch. The first is the
concrete retained-9p research proposal; the second preserves the existing stronger
boundary. The lock-scope decision alone selects neither authorization model.
