# Owned retained-9p guest integration (`tz3`)

This directory maintains the Linux guest component of the selected
[retained-9p bridge](../../../docs/spec/Retained-9p-protocol.md). It contains
five patches against **upstream Linux 6.1.190**, reproducible source/build
inputs, and tests that compile the actual C codec and identity/metadata helpers.
It does not install a kernel, publish a filesystem, or admit a canary.

The fourth patch contains the `vz3` fault/page_mkwrite, metadata-view, physical
recall, frozen-drain and stopped-installation components described in
[MMAP.md](MMAP.md). Its corrected final4 objects and narrow R2 component review
passed after the dirty-aops and clean-release fixes. Earlier development1,
final2 and final3 receipts retain their original source/configuration scope.
The fifth patch adds the controlled-local provider described below.

The builder enables and asserts THP, SHMEM and read-only filesystem THP for
`--mm-objects`; the preserved first final attempt failed that configuration check.
Object checks select `mm/memory.o`, `mm/gup.o`, `mm/mremap.o` and
`mm/huge_memory.o`, alongside guest/network objects. These component inputs do
not supply an installed profile or compatibility acceptance.

`source.json` pins upstream commit
`79807e970724c1f90cee1316361d61b80db4eef6`, annotated tag object
`9ad12a4c0c7a36f365c13ea17d88499618440a7b`, the exact source archive and every
patch/output digest. The tag was independently resolved with `git ls-remote`;
the verified archive's pax commit comment matches that commit. This upstream
source is **not a proven binding to the installed ELRepo kernel source, ABI or
running bytes**. It is not a signed release manifest. Packaging/admission and
actual guest acceptance remain `xz3`/`zq3` work.

## Component behavior

The profile remains Linux kernel 9p → virtio → lib9p → real FreeBSD FUSE. There
is no guest daemon, Linux FUSE replacement, alternate RPC client or stock-fid
fallback. `CONFIG_9P_FS_HOMELABFS` defaults off and depends on tracepoints.
When built, exact `version=9P2000.L.hfs1,trans=virtio,msize=65536` or
`version=9P2000.L.hfs1.1,trans=virtio,msize=65536` selection uses separate inode/file/super operations inside the 9p filesystem.
Since `g64` the hfs mount also requires exactly one `cache=mmap` (see below). The network
client accepts only exact version/msize replies, sends only Tbridge after
Tversion, and reserves independent ordinary/control/wait tag classes. Raw
bridge bytes do not acquire a stock counted-data prefix. The assigned private
message types exceed signed 8-bit range, so size dispatch explicitly treats
them as unsigned wire bytes. No stock Tflush cancels an admitted claim.

Every authorization callback captures an immutable reference to **current VFS
credentials**, maps filesystem UID/GID and every supplementary group to initial
namespace numbers, and calls the admitted mapping provider. Unmapped/reserved
IDs, mapper errors, overridden worker credentials and more than 1024 input groups
are refused. The canonical complete set is sorted and deduplicated without
truncation. Retained kernel TGID objects distinguish process incarnations from
reused PID numbers. Captures with unchanged complete numeric credentials,
namespace and idmap reuse one revision; observed snapshot changes advance it
monotonically, including A→B→A changes. Earlier request bytes never change when a
later capture changes credentials. Process-exit assertions use a typed actual kernel exit
tracepoint and preallocated work, rather than proc polling or inferred host exit.

Each lowered operation owns an immutable claim, operation UUID, original Context,
canonical intent and credentials before dispatch. A lost/uncertain response
retains its exact candidate/donor state and blocks new ordinary work. Reserved
control work remains available for reconciliation and drains. Pending snapshots
are never rewritten: subsequent QUERY uses another cell. RELEASE_CELL selects
its exact same-session response/hash and inherits its existing credit. Definitive
SEND capacity refusal at any chunk yields the framing writer while retrying
the identical encoded frame, cell, chunk and immutable claim/body. Only exact
COPIED advances the chunk. Retries share the original transport deadline,
including time spent yielding or reacquiring the writer; malformed refusals and
AGAIN fail closed.
Both local credit classes and mandatory aggregate credit admission are bounded.
Retained claims have separate ordinary/control ceilings (4096/13312), so ordinary
exhaustion or uncertain work cannot consume the control claim reserve. These
are distinct from the protocol's 64/16 active snapshot-cell credits.

ROOT/PATH/DESCRIPTION grants are checked against exact Context and object IDs.
Dentry capabilities retain their own ancestry; replacing an alias grant and
copying its immutable bytes are serialized by the dentry lock. Path permissions
are rechecked under current credentials, not a grant's old credential hash.
Descriptor rights instead retain their issuing grants across chmod, unlink,
dup/fork and descriptor transfer. Final release and evicted lookup FORGET work
use those issued snapshots, never root writeback or worker credentials. Failed
local grant adoption remains attached to its exact retained result; equal result
hashes cannot alias different claims. Same-mount POSIX/flock behavior uses Linux's
local inode domain; no distributed lock operation is added.

Creates/mkdir/symlink install complete committed attributes without
`inode_init_owner`, create-then-chown, or a later mode repair. Final creator UID,
logical parent GID, full `07777` mode, type and object identities come from one
validated host result. Local inode numbers are never reused and directory
entries use the same object-number ledger. Each directory description preallocates
and charges a bounded 4096-entry cookie ledger before OPEN. Nonzero seek restores
the exact prior raw-name cursor and revision, including older pages; rewind never
evicts or reassigns old positions. The complete outer directory attributes and
every entry's identity, type and metadata are validated before any entry is emitted.
Setattr preserves full supported mode
bits; unsupported combined size/ownership/mode requests fail before CALL.
The guest does not invent host Unix policy or overwrite committed parent GID.

The Linux VFS already applies guest umask/capability restrictions to creation
mode before the filesystem callback, so the lowered CREATE/MKDIR carries that
mode and umask zero. To preserve native DAC/LSM ordering, a fresh negative lookup
uses an exclusive CREATE primitive. A protected Exists result may resolve by a
fresh lookup and the existing-open path; unknown effects are never retried under
another claim. Existing opens return through normal VFS `may_open` and
`security_file_open`; their OPEN omits truncation so normal `handle_truncate`
runs the path LSM hook before the atomic TRUNCATE callback. Claims identify each
lowered bridge operation, including pathname lookup primitives; no promise is
made that an arbitrary multi-callback Linux syscall is a filesystem transaction.
No LSM hooks are disabled and no security policy is enabled or installed.

WRITE additionally captures `max_end`, the minimum of the current RLIMIT_FSIZE,
non-LFS and filesystem end bounds. Native `generic_write_checks` runs before
CALL and the host must cap the prefix at its authoritative ordered append offset
or return FileTooLarge before effects. A permitted short append consumes only
its acknowledged iterator prefix and uses its exact selected Attr.size as the
file position, even if another thread has installed newer cached attributes.
Native synchronous-write checks subsequently invoke the explicit sync callback.
Per-call RWF_APPEND that differs from the opened description's append grant
remains unsupported before CALL; the ordinary O_APPEND contract is supported.

## Mandatory providers and withheld claims

`fs/9p/hfs.h` defines a mandatory, kernel-owned provider interface. There is no
provider implementation in this series. With the guest component enabled,
absence of a complete provider refuses the profile before transport creation.
A missing mandatory callback or a feature mask outside exact `255`, `2047`,
`4095` and `8191` cannot register. The extension masks require target/event/view
retirement callbacks; the legacy mask requires its mmap callback. Controlled
successors additionally require CONTROLLED_LOCAL enrollment.
Providers must implement and actually prove:

- authenticated enrollment/Context and exact immutable namespace mapping;
- all-domain view entry/leave, physical cache/metadata/PTE/TLB/pin quiescence,
  mmap provenance, positive stop/drain/install and drain-progress reservations;
- aggregate memory/control credits across mounts, including retained requests;
- verified publication, original-origin/source/result binding, protected claim
  gap resolution, retirement proofs and reconciled session retirement;
- retained session ownership and recovery, without deleting uncertain state.

The low-level control entry supports the frozen guest operations, including
VIEW_WAIT's reserved tag and operation-selected recall FETCH channel 3/frame
kind 3. Ordinary/control responses, including RELEASE_CELL, use channel 2/kind
2. Providers own the full recall/recovery state machines
and validation of their corresponding unions. Guest H9B flags remain zero.
Native endpoint-global cell translation, native lane/reclaim flags and the
12 MiB replacement workspace belong to the backend/host components.

For the separately controlled base fixture (`f04`), an explicitly admitted
provider may validate a local-only/null Publication under that fixture's policy.
That proves no normal publication or all-origin guarantee and introduces no
normal runtime dependency into the pre-replication `zq3` base gate. A normal
provider still requires the separately versioned public-binding/source proof.
The callback remains mandatory for both scopes; there is no permissive default.

In particular `vz3` must integrate every cached observation and actual physical
MM/VFS hook; returning success from a callback is not that proof. The current
normal 3.0 codec still lacks the public anchored bridge binding. Its separately
versioned successor/proof provider is mandatory; this series does not place
public requests in v1 or the internal normal 3.0 union. Neither provider pointers
nor a build flag certify installed runtime behavior. Normal daemon publication
remains closed. Cached/mmap compatibility, arbitrary mutable workload/LSM
behavior and all-origin durability have **not** been exercised on this stack.

CALL rejection before reservation, or a locally unissued allocated number,
requires protected no-effect gap resolution before a retirement floor can pass
it. The guest retains those bytes. A timeout, CANCEL, exit, close, fresh UUID or
local assertion cannot mint that evidence. Retained session destruction requires
all claims/candidates reconciled and independently verified protected session
retirement; failed resources remain owned.

## Reproduce without installation

Use a fresh explicitly task-owned root outside the repository. Python 3.12+
(with tar extraction filters), GCC/make/binutils, bison/flex/bc, OpenSSL,
pkg-config, libelf development inputs and Perl must already be available.
No package installation is performed. For example, with absolute owned paths:

```sh
python3 tools/integration/linux9p/prepare.py \
  --destination /owned/run/linux \
  --archive /owned/run/linux-pinned.tar.gz
python3 tools/integration/linux9p/build.py \
  --source /owned/run/linux --output /owned/run/object-check \
  --budget-root /owned/run --module-objects
python3 tools/integration/linux9p/tests/codec/run.py \
  --source /owned/run/linux --output /owned/run/codec-check --sanitize
python3 tools/integration/linux9p/tests/policy/run.py \
  --source /owned/run/linux --output /owned/run/policy-check --sanitize
python3 tools/integration/linux9p/tests/cursor/run.py \
  --source /owned/run/linux --output /owned/run/cursor-check --sanitize
python3 tools/integration/linux9p/tests/transport/run.py \
  --source /owned/run/linux --output /owned/run/transport-check --sanitize
```

Omit `--archive` to fetch only the pinned primary archive. Existing destinations
and build outputs are refused. Preparation checks lengths/digests, extraction
bounds, exact commit comment, fuzz-zero patch application and every resulting
source file/symlink. Each build re-verifies the complete source inventory.
`export.py` is for editing only the55 maintained paths in a separately owned
source checkout; it regenerates patches and `source.json` against the verified
archive, without Git commits or incidental vendor modifications.

The builder uses a fresh x86-64 defconfig, explicit selected symbols, fixed
build identity/timestamp, path normalization and `-j2` on two CPUs. It limits
per-process address space and polls aggregate process-group RSS against 2 GiB,
allocated-root disk against 8 GiB, and each stage against 20 minutes. It only
terminates its own process group on exceedance. Polling can miss transient
overshoot; nominal intervals and actual maximum RSS sampling gaps are recorded.
Failed evidence is preserved. Earlier builder revisions summed logical file
lengths (`st_size`) despite describing the disk measure as allocated space.
Their historical receipts are unchanged and must not be treated as allocation
measurements. The `vz3` development builder instead sums `st_blocks * 512` for
the owned root and every owned file, directory and symlink; filesystem metadata
outside those entries is excluded. The new input receipt records that measure.
The output receipt identifies config, commands,
tool versions and object hashes. It compiles and combines kernel objects;
it does **not** perform complete module modpost/link, build a bootable kernel,
prove an ELRepo ABI, establish a hermetic toolchain, or prove cross-builder
bit-for-bit reproducibility.

The same-source C tests use a userspace allocation/hash shim only at the kernel
primitive boundary. They exercise the production parser/encoder and pure
identity/metadata decoder and directory-cookie state. They cover literal golden frames, malformed framing,
canonical/schema/type limits, deterministic arbitrary/truncated inputs,
allocation failure cleanup, operation-selected recall/result channels and frame
kinds with cross-kind/session/cell/hash negatives, 1024/1025 groups, mapping refusal and full committed
metadata including asymmetric creator UID/parent GID and `07777`, captured
write-end limits, independent ordinary/control admission and exact old directory
cookie restoration. These are the actual pure helpers used by the kernel
callbacks; the VFS wiring is also source-reviewed and compiled. They do not
execute Linux current-credential capture, process scheduling/exit, VFS attribute
application, LSM policy, virtio, lib9p or FreeBSD FUSE.

The transport suite compiles the complete production `hfs_transport` function
verbatim with the real codec/policy helpers and substitutes only p9 I/O, mutex
and clock primitives. It verifies exact retries at chunk 0, 1 and the final
chunk, writer yielding, original deadline expiry (including clock wrap),
malformed replies and transport errors, with result channel 2 and recall channel
3. Its simulated control progress proves the lock is released at that boundary;
it does not execute kernel scheduling or an actual control exchange.

See [i04 evidence](evidence/i04/evidence.json) for the capacity correction and
current source receipts. See [h04 evidence](evidence/h04/evidence.json) for the current recall-channel
correction inputs and receipts. The original [tz3 evidence](evidence.json)
retains its historical source and validation scope. Kernel-object compilation and these regressions are component evidence;
actual source/package/running-byte binding, syscall/guest behavior, physical
recall, native FUSE transport, signing, recovery and release admission remain
separate gates. This work performed no SSH, mount, VM, install, production or
received-sink operation.

## Retained view candidate checks

The owned source contract and required platform gates are described in
[MMAP.md](MMAP.md). Run every command in a separate fresh output directory;
`SOURCE` names an owned prepared kernel tree and `HOST_REPO` a checkout with j04.
These are component checks, not a mounted-filesystem acceptance batch.

```sh
python3 tools/integration/linux9p/tests/capture/run.py \
  --source "$SOURCE" --output "$RUN/capture"
python3 tools/integration/linux9p/tests/view/run.py \
  --source "$SOURCE" --output "$RUN/view" --sanitize
python3 tools/integration/linux9p/tests/view/run.py --pressure \
  --source "$SOURCE" --output "$RUN/pressure" --sanitize
python3 tools/integration/linux9p/tests/dirty/run.py \
  --source "$SOURCE" --output "$RUN/dirty" \
  --fixture "$HOST_REPO/docs/spec/retained9p-fixtures/views.json"
python3 tools/integration/linux9p/tests/dirty/host_codec.py \
  --repo "$HOST_REPO" --vectors "$RUN/dirty" --output "$RUN/dirty-host"
python3 tools/integration/linux9p/tests/requests/run.py \
  --source "$SOURCE" --output "$RUN/requests"
python3 tools/integration/linux9p/tests/root/run.py \
  --source "$SOURCE" --output "$RUN/root"
python3 tools/integration/linux9p/tests/huge/run.py \
  --source "$SOURCE" --output "$RUN/huge"
python3 tools/integration/linux9p/tests/leases/bounds.py --output "$RUN/bounds"
python3 tools/integration/linux9p/tests/dirty/host_codec.py --mode bounds \
  --repo "$HOST_REPO" --vectors "$RUN/bounds" --output "$RUN/bounds-host"
```

The bounded Go overlay checks run only the named interoperability tests with
one package build job and two CPUs. They hash host source and record actual exit,
sampled group RSS, maximum sampling gap and unchanged source. The closed-union
calculator includes full 1024-group credentials, nested Context/capabilities,
maximum page bytes and number widths; it supplies resource arithmetic, not an
actual memory-placement or enrollment proof. Existing manifest/ACK/control
frames keep their one-MiB bound outside the smaller ticket-operation arena.

See [vz3 component evidence](evidence/vz3/evidence.json) for the exact final
source/config/compiler/object bindings, preserved failed batch, current primitive
checks and scope limits. Neither object compilation nor those helpers supplies a
runtime native provider or proves actual physical recall on an installed kernel.

## Controlled successor forwarding (`o04`)

Explicit `9P2000.L.hfs1.2` and `9P2000.L.hfs1.3` selections use their exact
minor2/schema3/features4095 and minor3/schema4/features8191 tuples. Providers
must select the matching complete feature mask and CONTROLLED_LOCAL enrollment;
those older selections do not use the registered minor4 controlled provider.
Closed older selections cannot carry
ALLOCATE/GAP_RESOLVE or the minor3-only READDIR_CLEANUP. The owned codec and
lib9p retain the successor response `verification` field byte for byte.

Before an ordinary CALL, production request code retains the full original
Claim, intent hash, Context and credentials, then sends the exact full Call in
ALLOCATE through the ordinary lane. Only its exact ALLOCATED acknowledgment
permits CALL. An uncertain allocation keeps its original transport cell,
canonical command, donor and local issuance owner. Retry sends those same
bytes; subsequent ordinary/control claims and dirty earmarks cannot issue a
later number. Typed dirty reservations own their original no-hash number and
hold the same issuance gate until the existing reservation decoder verifies
its protected binding or its exact CANCELLED-ticket closure. DRAIN_WRITE never
uses ordinary ALLOCATE. GAP_RESOLVE retains the complete original and its exact
component proof; mandatory runtime retirement verification remains separate.
A lost GAP reply retries that original Call in another reserved control cell;
the unknown earlier transport candidate and blocked session remain owned for
C9 reconciliation. It does not resume ordinary issuance automatically.
The request retains its complete terminal QUERY and GAP response envelopes;
the mandatory retirement callback receives the complete borrowed RETIRE
envelope for exact controlled1.4 private floor-receipt verification. Receipt retention grants no
verification authority. Successor RETIRE sends original full credentials and a contiguous prefix owned
by that issuer, without replacing existing provider floor verification.

The backend translates guest cells, reclaim hashes and VIEW_WAIT poll commands
into native endpoint coordinates. It preserves the host's exact native receipt;
that signature does not authenticate rewritten guest coordinates. The current
`hfs_json.native_cell` field records the guest translated cell, not the
endpoint-global native cell. The separately negotiated1.4 mapping (`y04`)
authenticates the original guest/native coordinates and complete poll chain.
The controlled provider below consumes this through its private received path;
a public transported flag or preserved signature cannot create that authority.
Complete runtime construction and positive terminal session handoff remain
separate work. Primitive tests do not establish native FUSE identity,
physical observation, installed kernel behavior or deployment.

Run the composed controlled fixture with a new output under an explicitly owned
budget root:

```sh
python3 tools/integration/linux9p/tests/controlled/run.py \
  --source "$SOURCE" --output "$RUN/controlled" --budget-root "$RUN"
```

It builds actual guest issuance/control/framing plus the owned backend and real
local backend ledger, then runs the real host Transport/Service/provider/journal
through a Go test overlay. It does not manually allocate host claims. The kernel
and native fd observations remain primitives; result publication deliberately
refuses without C6. Kernel typed-earmark interleaving is distinct from the
separate production DIRTY_RESERVE decoder check using literal controller bytes,
with binding/copy-failure negatives. It does not instantiate a live host dirty
controller or establish source/physical authority. See [o04 evidence](evidence/o04/evidence.json)
for exact frozen inputs, bounded checks, preserved failed attempts and limits.

The same composed fixture covers the closed minor0/1 selections. Their
Enrollment.Protocol remains nil; the fixture explicitly issues the ROOT claim
with the real legacy Provider.Allocate API because these wire versions have no
ALLOCATE command. This is synthetic fixture issuance, not guest pre-send
allocation or native/source/publication authority. Both ordinary and locally
reserved ROOT requests send ordinary CALLs; QUERY uses control cells and HELLO
uses ordinary cells. DRAIN_WRITE alone uses a control CALL. Existing corpus
arguments with an unissued drain claim reach the real host and are refused.
Result publication still refuses without its mandatory verifier. The backend's
same-class donor restriction is unchanged. [z04 evidence](evidence/z04/evidence.json)
records the original composed failures and the focused checks.

## Controlled guest provider (`s04`)

The fifth patch supplies a default-off, built-in controlled-local provider for
exact1.4/schema5/features16383. Its [local admission artifacts](../../../docs/spec/Retained-9p-guest-admission.md)
install a complete signed grouped local inventory before observer admission,
then bind each actual host controller once. Enrolled device/client/superblock,
initial namespace/idmap and all lane identities stay private and immutable.
Prepared/unbound sessions permit only the exact bootstrap HELLO and grant no
source or ordinary rights. Failed/uncertain sessions retain consumed identities.

`/sys/kernel/homelabfs/admission_v1` is root-readable0400. Each open reserves one
of four128KiB immutable snapshots, including stopped/uncertain domains and exact
protocols/phase/event/ACK hashes. Root-only provision/bind endpoints accept one
complete bounded signed artifact. There is no reset or replacement interface.
The approved configuration delta is `9P_FS=y`, `NET_9P_VIRTIO=y`, `SYSCTL=y`,
`SYSFS=y`, `CC_IS_GCC=y`, `9P_FS_HOMELABFS=y`,
`9P_FS_HOMELABFS_CONTROLLED=y` and a pinned nonempty
`9P_FS_HOMELABFS_OPERATOR_KEY`; legacy profiles are unchanged. Crypto wrappers
require the explicitly pinned GCC whole-program elimination flags.

Actual transport completion alone creates the private received owner. Detached
JSON, copied diagnostic flags, signatures and source hashes cannot create it.
Exact descendant capabilities/targets retain that owner independently of the
existing request lifetime. Nonnull publication refuses. SOURCE/EVENT verifies
actual original request/evidence, signed enrollment and actual physical state;
real host C5 verification continues to own journal/catalog/payload byte checks.
C6 charges retained admission/snapshot/staging ownership against control bytes,
then supplies finite byte/slot/cookie/received caps and conservative retain.
One accepted root administrative open uses actual kernfs preallocation to
serialize same-fd copies; generic pre-admission syscalls/allocator overhead remain
outside the signed requested-allocation budget.
C7 precharges actual request/ticket completion packages and independent session
event/floor lanes before exposure. Native reserved scheduling does not classify
resource ownership: ordinary QUERY/view-query data and descendants stay ordinary,
while exact preowned cleanup can consume its already funded capacity while closing
or globally saturated. The event6/floor2 Received-slot split follows the actual
pending[3]/last-event/reposted-WAIT/ACK and retained-cut lifetimes. HELLO retains its
actual carrier through complete JSON destruction without creating Received
authority. See the [admission contract](../../../docs/spec/Retained-9p-guest-admission.md)
for computed byte/slot bounds and the installer-versus-SB admission distinction.

C7 partitions ordinary and funded control bytes/slots across mounts and retains
per-session ownership. Its typed closing path permits only exact preowned cleanup,
including bound DRAIN_WRITE and parent READDIR cleanup; generic control CALL
refuses. Actual full protected FLOOR/VIEW_FLOOR replies authorize same-issuer
contiguous retirement; actual GAP and QUERY envelopes retain no-effect coverage.
Uncertain candidates and ordinary/per-family retirement attempts keep their exact
original bytes, issuer, cut and any full reply until consumption or definite
BLOCKED. A late result borrower keeps the accepted old cut available for replay.
No timeout, stop fence or empty local list supplies retirement authority.

The C7 component scenarios exercise two-session ordinary-cookie saturation with
real hfs_call refusal while funded STOP/FROZEN and a nonempty4096-byte drain
progress, busy-pin refusal, strict full-credential A/B/A floors, lost GAP/FLOOR
replies, prepared/effected refusal, and later-request/ticket interleavings. The
actual closed1.4 QUERY exposes protected DECLINED/null only under its exact
private request/native binding; direct/CANCEL and older protocols retain the
conservative projection. Final guest/native cells, unknown candidates and
session/module/physical/source ownership remain bounded and retained.
Positive terminal closure belongs to task614 under the separately negotiated
[session contract](../../../docs/spec/Retained-9p-session-retirement.md), a C9
prerequisite; C7 does not reinterpret RELEASE_CELL or implement its new tuple.

The `controlled_crypto` tests run unchanged Monocypher4.0.3 Ed25519 through owned
canonical prime-subgroup checks, real RFC/Go/OpenSSL signatures and encoding
negatives. `controlled_provider` tests run the actual signed transactional
installer and immutable sysfs serializer, including distinct volumes/journals,
allocation failures, exact independent inventory/phase oracles and overflow.
`controlled_received` joins the actual registered provider, private transport
factory, view/lease/STOP/install/ACK helpers, net9p exchange and C carrier to the
real Go Transport/Producer/controller/source. Its actual C snapshot drives the
host observer, including a nonnull original WAIT Ack across multiple polls.
A changed original journal byte suppresses a fresh source response/adoption.
Kernel credentials, MM/PTE primitives, native FUSE identity and virtio device
objects remain explicit OS substitutions in these component tests. Actual
kernel translation-unit/frame/link checks do not prove installed PTE/TLB,
native deployment, terminal closure or normal/distributed publication.

## Mount HELLO ordering regression (`r54`)

The first real lab mount (zq3-r2, R2-4) returned EPROTO: the mount built HELLO
from the still-zero features before enrollment, so it carried the features 16383
default and the controlled pre-send check refused it for a features 32767
enrollment. The reply check also accepted only that default selection.
The `controlled_received` harness builds its own HELLO after enrollment and
uses only the mount's failure tail (now `hfs_mount_fail`), so it could not
see this.

```sh
python3 tools/integration/linux9p/tests/mount_hello/run.py \
  --source "$SOURCE" --output "$RUN/mount-hello" --sanitize
```

It compiles the complete production `v9fs_hfs_mount` with its helpers, the
enrollment validators and `hfs_controlled_before_send` verbatim. Kernel/VFS, p9 client,
transport binding and controlled registry lookups are substituted; the native
exchange echoes the sent HELLO as the runtime does. Cases: features 16383 and
32767 mount; a default echo for 32767 and a terminal echo for 16383 refuse with
EPROTO after one HELLO; an unselected mask refuses before any HELLO or client.
Each case runs in its own child. The pre-r54 source fails the 32767 cases and
crashes on the unselected mask.

## Failed bind_super close and slot floor (`w54`)

The second lab mount (zq3-r3, R3-1) had an accepted HELLO, then failed
`hfs_fill_super` at its first step. The lab provisioned `reservations` 32, so
each partition (control and ordinary) held at most 16 credits, while one
completion package needs 17 (parent plus `HFS_COMPLETION_SLOTS`). bind_super's
first progress lane returned ENOSPC before ROOT. The offline harness provisions
8192 and could not see this. `mount_nodev` then ran kill_sb.
`v9fs_hfs_kill_super` → `hfs_controlled_begin_close` passed the never-created
terminal lane to `hfs_completion_reserve`, which read `->closed` at offset
0xbd and oopsed.

Patch 0005 now refuses that state. `begin_close` returns ESTALE without a
terminal lane, and kill_sb fences the retained session as blocked.
`hfs_completion_reserve` also refuses a NULL lane. The other close and
retirement paths already tolerate absent lanes (`hfs_completion_close`,
`_idle`, `_owns_credit`, the terminal inventory/publish checks and
`hfs_controlled_progress_reserve`). The failed bind keeps `mount->claimed` and
`mount->session` on purpose: the accepted HELLO consumed a native cell under
that enrollment, kill_sb retains the session, and registry rows are never
re-enrolled. Recovery is a fresh boot and provision.

Provisioning now also refuses a slot budget below the mount floor
(`hfs_controlled_slot_floor`, derived in `hfs_controlled_internal.h`). The floor
is what stays charged while every mount is bound, bootstrapped and serving one
first operation. A package is 17 slots; an owner (request or ticket reservation
plus its package) is 18:

| Per mount | Control credits | Ordinary credits |
|---|---|---|
| bind_super progress lanes | 3 × 17 (32767) or 2 × 17 (16383) | |
| process identity, view, mmap actor, bootstrap cleanup | 4 | |
| ROOT request | | 18 |
| discovery lease: ticket owner + family | 19 (release) | 19 (acquire) |
| first `hfs_begin_cap` from a new process | 21: identity, observation cleanup, release ticket owner, family | 38: cookie, acquire ticket owner, family, request owner |
| total | 95 (32767) or 78 (16383) | 75 |

Received slots per mount are 15 (or 14) control and 12 ordinary: event lane 6,
retirement 2, terminal 1 and 3 per ticket or request package. Both partition
counters are global, so `reservations / 2` must cover the larger summed
partition, and `received / 2` likewise. The lab's two 32767 mounts therefore
need **reservations ≥ 2 × 2 × 95 = 380** and **received ≥ 2 × 2 × 15 = 60**.
The HELLO carrier is freed before bind_super and does not add to the peak.
More concurrent operations, open descriptions, mapped pages and discovery
retries (18 + 18 per retry until the bootstrap cleanup retires them) are not
in the floor; budgets need a margin. Since `reservations` is at most 8192,
a registry with more than 43 terminal mounts can no longer be provisioned.

Bytes are not part of the provisioning floor; they depend on kernel struct
sizes and remain checked at reserve time. The actual accounting in the
`controlled_received` harness charged 80,962,137 control bytes per 32767
bind_super, 36,559,105 ordinary bytes per request owner and 26,802,997 bytes
per lease ticket owner in each partition. That is about 132 MiB control and
121 MiB ordinary per mount, plus a 9 MiB control base for two mounts. zq3-r3's
128 MiB control budget could not even bind both mounts.
`hfs-runtime-prep generate` refuses specs below both floors
([`internal/runtimeprep/guestfloor.go`](../../../internal/runtimeprep/guestfloor.go)).

The `controlled_received` startup runs the regressions in disposable children
after the actual accepted HELLO. Any scenario runs them, and a 32767 scenario
reaches the terminal close path:

```sh
python3 tools/integration/linux9p/tests/controlled_received/run.py \
  --source "$SOURCE" --output "$RUN/w54" --budget-root "$RUN" \
  --host-tree "$PWD" --scenario terminal-smoke
```

`failed-bind-kill-sb` runs the actual `hfs_fill_super` with reservations 32,
which returns ENOSPC with every lane credit returned. It then runs the actual
`v9fs_hfs_kill_super` and checks the retained, fenced and still-claimed
session. `provision-slot-floor` runs `hfs_controlled_base_charge` with an
independent oracle of the floor: 32 and floor − 1 are refused, the exact floor
is accepted. The startup also checks the floor's units against the actual
accounting: bind_super charges exactly its lane packages, a new process one
credit, each `hfs_call` request and each lease exactly one owner per class
plus new families. On the pre-w54 source the terminal scenario reproduces
the lab oops (ASan SEGV at 0xbd in `hfs_completion_reserve` ←
`hfs_controlled_begin_close` ← `v9fs_hfs_kill_super`), and the provisioning
case accepts 32.

## Fixed-tag reuse (`964`)

In the fifth lab mount (zq3-r5, R5-1) the guest accepted the RECALL. It sent
STOPPED and INSTALLED_DRAIN, and the daemon ACCEPTED both. The guest then
never fetched the INSTALLED_DRAIN reply and never sent DRAINED, while the
release loop kept polling VIEW_QUERY (PENDING). Both exchanges use fixed tag
65533. `p9_client_hfs_exchange` released the per-tag mutex before its
`p9_req_put`, and `p9_client_cb` drops the receive reference only after
`wake_up`. The next owner of the tag could therefore call `p9_tag_alloc` while
the old request still held the idr slot. `idr_alloc` then returned ENOSPC,
which became ENOMEM. `hfs_control` treated that as a transport fault and marked
the session blocked for good, so capture, view entry and new mmaps refuse from
then on. The event worker retries only EAGAIN/EBUSY, so it stopped silently,
and DRAINED was never sent. Reserved VIEW_QUERY bypasses the blocked check, so
the release loop kept running.

Patch 0001 now waits, bounded at one second and under the per-tag mutex, until
the fixed tag's previous request is fully released (`p9_client_hfs_tag_idle`).
It drops its own reference before unlocking. Nothing is sent while it waits.
Expiry returns EBUSY, and a disconnecting client returns EIO first. EBUSY from
the exchange always means "not sent": with the tag idle under its mutex,
`p9_tag_alloc` cannot find it busy, and any other EBUSY from the RPC is
reported as EIO. Dynamic tags stay in 0..65532, so only the mutex holder can
take a free fixed slot. The fix is in the exchange, so it covers every
fixed-tag user: all reserved/RELEASE_CELL control traffic on 65533, the
VIEW_WAIT poll worker on 65534, and each SEND followed at once by its FETCH on
the same tag.

Patch 0003's `hfs_transport` treats that EBUSY like a refused packet. A SEND is
retried with the exact frame of the same cell and chunk after yielding the
writer, and a FETCH polls again. Both stay under the command's existing
30-second deadline, which ends in ETIMEDOUT. The error never reaches the
`hfs_control` fault path, so no uncertain candidate is kept and the session is
not blocked. The view event worker needs no resend of its own. Patch 0004 only
makes `hfs_mmap_fail` log the errno (`pr_warn_ratelimited`) whenever the view
control worker stops.

```sh
python3 tools/integration/linux9p/tests/tag/run.py \
  --source "$SOURCE" --output "$RUN/tag" --sanitize
```

It compiles the tag allocator, request lifetime, `p9_client_cb`, the RPC wait
and the hfs exchange verbatim from `net/9p/client.c`. Only the idr, slab, wait
queue, locks, PDU marshalling and a virtio-like receive thread are substituted.
The receive thread holds its last reference after `wake_up` until another owner
probes a tag, as a preempted vCPU would. The cases are:

* a VIEW_QUERY loop and a VIEW_ACK worker on 65533;
* back-to-back VIEW_WAIT on 65534;
* a never-released reference, which must return EBUSY after about one second
  without sending;
* the same stuck tag on a disconnected client, which must return EIO at once.

On the pre-964 source the first three fail: both owners and the poll get
ENOMEM at once, and the stuck tag gets ENOMEM after 0 ms.

The transport suite also returns EBUSY at a SEND (the retry must carry the
identical frame) and at two FETCHes; pre-964 `hfs_transport` returns EBUSY
instead. The `controlled_received` terminal-smoke scenario arms `BUSY_TAG` on
the second session. The next QUERY's SEND and FETCH each first get the bounded
EBUSY from the substituted idle wait, before any native byte. The actual
`hfs_control`/`hfs_transport` then complete the ROOT request on the same native
QUERY cells as a plain request. `BUSY_TAG_CHECK` asserts that the session is
not blocked and has no uncertain candidate, and a further request succeeds.

The same task fixed the GCC 16 `-Wunused-parameter` error in the
`hfs_received_exclusive` stub of `hfs_received.h`, which stopped the codec,
policy, transport and other harnesses from compiling (part of s54). The codec
harness still fails its pre-existing `test.c:366` assertion (s54).

## Explicit terminal tuple (`614`)

The maintained provider also supports explicitly enrolled features32767 with
the same hfs1.4 token/schema. Features16383 remains unchanged.
`v9fs_hfs_mount` builds its HELLO from the enrolled features only after
`provider->enroll`, `hfs_enrollment_validate` and `hfs_enrollment_lanes`
succeed, and accepts only a selection equal to that enrolled tuple. An
enrolled mask without a selected tuple refuses before any native bytes. SESSION_RETIRE
uses a preowned completion package, the actual complete known-Ack set, immutable
read-only terminal observation, and private SESSION_RETIRED receipt validation.
The exact candidate/cell/full credentials survive pre-send uncertainty and lost
replies. Physical/claim/view obligations must finish before acceptance; no
fresh ordinary CALL is admitted after closing.

The32767 cleanup admission slots for DESCRIPTION_RELEASE and FORGET retain
control scheduling while spending ordinary bytes for their full normal response
profiles. An already admitted, never-planned original cleanup may resume through
its authenticated QUERY using an expiring provider-owned continuation; full
original credentials, binding, scope and live native identity remain checked.
Other operations and old tuples receive no such exception. Completed consumed
dirty earmarks close through their exact terminal drain claim and protected
floor; the unused-earmark cancellation flag is not fabricated for them.

For this tuple the control actor charges its actual persistent poll/ACK buffers
in addition to the provider's already charged event/transport/Received parents,
instead of duplicating the legacy32MiB envelope reservation. Final inventory
enumerates completed process/family identity credits, reconciles real class and
Received counters, and retains them until their normal destructors run. An
unknown reservation or external Received alias still refuses closure.

Successful private consumption frees guest owners and leaves the original
charged kernel registry/device tombstone, plus one exporter/C8 native cell.
Other sessions remain usable. The [614 evidence](evidence/614/evidence.json)
separates the real dirty-session lifetime test from the80-cell exporter/host
composition with a substituted OS observer. Neither establishes an installed
kernel, native PTE/TLB exclusion, deployed ABI acceptance or C9 restoration.

## Mount options, cache mode and blocked-session log (`g64`)

zq3-r7 showed two silent guest gaps. `hfs_super_ops` had no `.show_options`,
so mountinfo showed only `rw` and `guest.py` refused the mount tuple (R7-1).
A session marked blocked after a QUERY timeout then refused every operation
with EACCES, and dmesg said nothing (R7-2).

- **Super options.** Patch 0003 adds `hfs_show_options`. It prints
  `,cache=mmap` and then calls `p9_show_client_options` on the live client,
  which prints the transport and the negotiated version. Patch 0001 now
  prints msize for hfs clients even when it equals `DEFAULT_MSIZE`; stock 9p
  still omits it. For the zq3 options
  `trans=virtio,version=9P2000.L.hfs1.4,cache=mmap,msize=65536` the super
  options are
  `rw,cache=mmap,msize=65536,trans=virtio,version=9P2000.L.hfs1.4`.
- **Cache mode.** hfs implements the stock `cache=mmap` contract, and only
  that. read/write always go over the bridge, dentries are never kept,
  attributes are refreshed for every permission check, and the only guest
  page cache is the retained-view mmap cache. `v9fs_hfs_mount` now requires
  exactly one `cache=mmap` and refuses, with EINVAL before enrollment, a
  missing or repeated `cache=`, any other mode, and the legacy cache options
  `loose`, `fscache`, `mmap` and `cachetag=`. Before g64 the options went
  unparsed to `p9_client_create`, which ignores `cache=`. The lab tools
  already pass `cache=mmap`, so they need no change.
- **Blocked log.** `hfs_session_block` sets `blocked` and logs the first
  block of each session once (`pr_warn_ratelimited` behind a per-session
  bit). The log gives the session UUID, the reason, the operation and the
  errno, for example `blocked by QUERY timeout on GETATTR (-110)`. It covers
  the core paths in patch 0003: the QUERY/CALL `retained:` path, the
  control-transport fault, PROCESS_EXIT, FORGET, DESCRIPTION_RELEASE, and the
  CREATE, namespace and READDIR result checks. Teardown fences (kill_sb, a
  failed mount), the sequence-wrap guards and the 0004/0005 view, lease and
  controlled-stop paths still set the flag directly. `hfs_mmap_fail` already
  logs its errno. (m64 routes the view, lease and controlled-stop paths too
  and drops the ratelimit; see below.)
- **Sources floor hook.** Patch 0005's `hfs_go_bounds` now names its
  `Sources` lower bound `HFS_GO_SOURCES_MIN`. The value is still 2, with a
  `TODO(i64)`: it must mirror the derived floor that viewcontrol validation
  defines. Choosing a different number here alone would let the guest and
  the host validators disagree. (m64 replaced it with that floor.)
- **Mount split.** `v9fs_hfs_mount` is split into `hfs_session_new`,
  `hfs_mount_enroll`, `hfs_trace_register`, `hfs_mount_client`,
  `hfs_mount_hello` and `hfs_mount_fail`, in the same order. The
  `controlled_received` failure regression now compiles `hfs_mount_fail`
  directly.

The `mount_hello` harness (command above) now also covers this work. Mounts
use the zq3 option string. After each successful mount it compares the
verbatim `hfs_show_options`/`p9_show_client_options` output exactly. It also
checks that the guest.py `check_tuple` set is a subset of `rw` plus that
output, and that msize 131072 (the default) is still printed. Eleven
refusal cases must fail with EINVAL before enroll, with no session, HELLO or
client. A stock-client case checks that the default msize is still omitted
there, and a direct `hfs_session_block` case checks one log line per
session. The pre-g64 source fails extraction (`hfs_session_block`,
`hfs_cache_option` and `hfs_show_options` are absent).

The series was regenerated with `export.py --task g64`; 0002 and 0004 are
byte-identical. A fresh `prepare.py` tree applies it with fuzz zero. The
touched objects compile without warnings under the candidate configuration
(`tools/integration/artifacts/kernel.py configure`, which includes
`HOMELABFS_CONTROLLED`, plus `make fs/9p/ net/9p/ drivers/char/random.o`).
`build.py --module-objects` fails in `hfs_session.c` on its non-controlled
configuration (`hfs_controlled_owns` undeclared), the same as before g64.
On GCC 16, policy, cursor, transport, tag, mount_hello, requests,
readdir_cleanup, huge and controlled_received `terminal-smoke` pass. codec
(`test.c:366`), capture, root, reclaim and view fail exactly as they did
before g64 (s54). No candidate was rebuilt or admitted.

## Sources floor and the remaining blocked sites (`m64`)

- **Sources floor.** Patch 0005's `hfs_go_bounds` now takes the enrolled
  domain count and refuses `Bounds.Sources` outside
  `[2 * domains + 4, 128]`, exactly `viewcontrol.CheckSourceFloor`
  (`MinSources` in `internal/viewcontrol/source_floor.go`, i64). The guest
  previously accepted any Sources from 2, so a signed enrollment the daemon
  refuses could still provision. `hfs_signed_enrollment_decode` therefore
  counts Domains before it checks Bounds; the decode is split into
  `hfs_go_header`, `hfs_go_domains` and `hfs_go_profile_hash`. The other
  `hfs_go_bounds` limits already matched `viewcontrol.validBounds`; o64
  changed spec values, not bound rules. The zq3 spec (2 sessions, Sources 10)
  and the controlledruntime guest artifacts (3-domain groups, Sources 10, the
  exact floor) are accepted.
- **Blocked sites.** The remaining silent `session->blocked` sites now go
  through `hfs_session_block` with a reason: in patch 0004 the VIEW_QUERY /
  VIEW_CANCEL reply checks, the uncertain view ticket in
  `hfs_view_exchange` (the operation is VIEW_QUERY once the initial command
  was admitted), VIEW_ACQUIRE, both VIEW_RELEASE checks, a busy ticket at
  VIEW_RETIRE, DIRTY_RESERVE and DIRTY_FREEZE. In patch 0005 the controlled
  stop and retain fences use `hfs_session_fence`. kill_sb sets
  `session->unmounting` first, so a clean unmount fences silently (zq3 run
  sheets collect dmesg as evidence, and a warn line there would read as a
  fault). kill_sb itself logs a failed controlled close
  (`failed controlled close on UNMOUNT`) or a failed provider stop
  (`failed provider stop on STOP`); a retain after a failed mount logs
  `controlled retain on RETAIN (0)`. Blocking semantics are unchanged. Only
  a failed mount and the close/release sequence-wrap guards still set the
  flag directly.
- **Log reliability.** The once-per-session line is a plain `pr_warn`. With
  `pr_warn_ratelimited`, a drop inside a printk burst still set the once
  bit, so that session could never explain itself. The bit bounds the output
  to one line per session, and only root mounts create sessions.

`mount_hello` adds three cases. `enrollment-sources-floor` runs the verbatim
`hfs_go_bounds` against the daemon's constants, which `run.py` reads from
`source_floor.go` (it fails if the `MinSources` formula changes), and the real
Bounds of the zq3 spec and guest artifacts. It accepts each signed Bounds,
then checks floor - 1 refused, floor, 128 accepted and 129 refused for 1, 2,
3, 62 and 63 domains. `session-block-ratelimit` blocks while the ratelimit
substitute drops every line and requires the line anyway.
`unmount-fence-log` runs the verbatim `v9fs_hfs_kill_super` with the real
`hfs_controlled_stop` and `hfs_controlled_retain`: a clean unmount logs
nothing, a failed stop or close logs one warn line, and a retain outside
kill_sb logs. The pre-m64 source fails to compile the
harness (`hfs_go_bounds` takes two arguments). `controlled_received` gains a `pr_warn`
substitute and compiles `hfs_session_fence`.

The series was regenerated with `export.py --task m64` (0001 and 0002
unchanged). A fresh `prepare.py` tree applies it with fuzz zero and the
touched objects compile without warnings under `kernel.py configure`
(`make fs/9p/ net/9p/ drivers/char/random.o`). On GCC 16 policy, cursor,
transport, tag, mount_hello, requests, readdir_cleanup, huge and
controlled_received `terminal-smoke` pass. codec (`test.c:366`), capture,
root, reclaim and view fail with the same errors on the pre-m64 tree (s54);
`p84` later repaired view (both modes).
controlled_provider also fails before running, on both trees, in its own
struct extraction (`struct hfs_terminal_ack` incomplete in
`session_struct.inc`). No candidate was rebuilt or admitted.

## Pressure and in-flight requests (`p84`)

zq3-r9 R9-1: when an ordinary-lane exchange waits more than 50 ms,
`hfs_transport` (patch 0003) calls `hfs_mmap_progress`, which queues
`hfs_mmap_pressure` (patch 0004). Pressure releases one lease, and any release
recalls the domain. The recall moves the view under every request still in
flight: that request's first host admission can be refused behind the recall,
and its stamped `hfs_view_enter` in `hfs_attr_install` can return ESTALE (the
r9 refusal cause is not proven; the ESTALE comes from the 984 model). R9 most
likely released the GETATTR's own DOMAIN observation lease.

- **Observation leases are never selected.** Every entry on
  `view->observations` belongs to a request still in flight;
  `hfs_mmap_end` removes it and its cleanup packet releases the lease, as
  before.
- **Background passes hold while observing, for at most 5 s.**
  `hfs_mmap_pressure` takes a `background` flag. The transport-triggered pass
  (`hfs_mmap_pressure_work`) releases no cache lease while
  `view->nr_observations` is nonzero, for at most `HFS_PRESSURE_HOLD_MS`
  (5000, `hfs_view.h`) since the first held pass. A cache-page release there
  would move the view under the in-flight requests just like an observation
  release, and a 50 ms wait does not show that it would help the waiting
  request. After the bound one cache lease is released and the hold restarts,
  so a busy view pays at most one such recall per 5 s; the hold also ends when
  the last observation does. With no observation in flight the background
  pass releases at once. A held pass still advances `pressure_after`, which
  only rate-limits the next pass.
- **Capacity callers are unchanged.** The 129th clean page
  (`hfs_mmap_fault_error` on ENOSPC), read-first `page_mkwrite` and dirty
  `hfs_mmap_sync` pass `background == false` and release a cache lease even
  while a request is in flight; they cannot progress otherwise. That
  residual cost is accepted and documented in `hfs_mmap.c`.

Limiting the transport pass to acquisition-lane waits instead was rejected:
an observation's own VIEW_ACQUIRE wait would still release a cache lease
under another thread's in-flight request.

Why the hold must be bounded: recalls never depend on a guest release (the
host starts the recall a conflict needs itself in `viewcontrol` admission
and acquisition, `hfs_view_stop` revokes observation leases without waiting
for their requests, and the host deactivates leases at STOPPED). Host
acquisition capacity does depend on it. A held cache lease pins its ticket
family's retirement floor (`hfs_lease.c`, `viewcontrol` `identityCapacity`),
so once identities are exhausted `grantLease` answers BLOCKED until a lease
is released, and the background pass may be the only caller. `read()` and
`write()` copy user buffers inside their own observation, so a fault on an
uncached HFS `MAP_SHARED` buffer acquires while that observation is in
flight. An unbounded hold could outlast the guest's 30 s acquisition
window, and that timeout leaves an uncertain ticket that pins the floor for
good. Five seconds covers a slow in-flight request (ordinary requests take
milliseconds, a scoped clean recall well under a second) and leaves 25 s of
the window for the release, its recall and a retried acquisition.

The fault's own BLOCKED (`-EAGAIN`) is deliberately not made a capacity
caller. BLOCKED also answers an expected-target mismatch after its recall and
RETIRED_ID, so releasing a cache lease on every such retry would add a recall
per stale fault; that path stays as before p84 and relies on the bounded
background pass.

Residual: two observations of the same lineage (domain, access, credentials,
mode) share one host lease, so the normal end-path release of one still
recalls under a concurrent one. A background pass that found no observation
is exposed to one that begins after that check, until the guest processes
the resulting STOP. The capacity callers and the bounded release can still
move the view under an in-flight request.

`view --pressure` adds the cases: an in-flight observation with no cache
lease is never released (fails on the pre-p84 source); with 128 clean pages
the background pass releases nothing while the observation is in flight,
still one jiffy before the bound (fails on the first p84 commit); ENOSPC
pressure still releases a cache lease and the recall revokes, but does not
release or wait for, the observation; at the bound the held pass releases one
cache lease, never the observation lease; read-first mkwrite runs with the
observation in flight, so a background flag there would fail; with no
observation the background pass releases a cache lease. The `view`
session shim gained `bridge_minor` (u16), `closing` and `blocked`, and the
pressure build includes `hfs_received.h`, so both modes compile again; the
fixtures keep `bridge_minor == 0`, so the minor4 gate is not exercised.

The series was regenerated with `export.py --task p84` (only 0004 changes;
round 2 adds two `struct hfs_view` fields and `HFS_PRESSURE_HOLD_MS` in
`hfs_view.h`).
A fresh `prepare.py` tree applies it with fuzz zero and `fs/9p/`, `net/9p/`
and `random.o` compile without warnings under `kernel.py configure`. view,
view --pressure, mount_hello, tag (all `--sanitize`) and controlled_received
`terminal-smoke` pass. No candidate was rebuilt or admitted.

## Pending-allocation wait and the unmount stop (`x84`, `z84`)

zq3-r10 R10-1 (x84, patch 0003): a request may not be numbered while
another request's ALLOCATE is unacknowledged (`session->allocation_pending`).
`hfs_call` used to answer that with an immediate local `-EAGAIN`. No VFS
caller retries it: userspace saw a spurious EAGAIN, and `hfs_release`,
`hfs_flush` and the FORGET and PROCESS_EXIT workers treat any error as
uncertain and block the session. At process exit the PROCESS_EXIT work
(queued from `do_exit` before `exit_files`) holds the fence while the
exiting task's final `fput` sends DESCRIPTION_RELEASE, which is how r10's T1
blocked its session without the release ever reaching the host.

- **Wait while the fence is still clearing.** `hfs_call` sleeps on
  `session->allocation_wait` for at most 30 s (`HFS_ALLOCATION_WAIT_MS` in
  `hfs.h`, the guest's request window) and then returns the old `-EAGAIN`.
  Every place the fence clears or becomes non-clearing bumps
  `allocation_epoch` under `session->lock` and wakes the queue: the end of
  each ALLOCATE attempt (acknowledged, failed or uncertain), earmark
  admission and cancellation, retirement of the owner (defensive, for
  minor < 4), a waiter leaving the queue, and kill_sb's closing.
- **FIFO.** Waiters queue on `session->allocation_waiters`
  (`struct hfs_allocation_waiter` on the caller's stack); with the fence free
  only the queue head may number, and new earmarks also yield to a non-empty
  queue (the fault path retries their `-EAGAIN`). A stream of new callers
  therefore cannot starve an `hfs_call` waiter. Only `hfs_call` waiters are
  starvation-free: under sustained ordinary `hfs_call` load an earmark can
  keep losing, and its fault retries via `VM_FAULT_NOPAGE` indefinitely
  (slow, not a deadlock). The head keeps its place through request
  preparation. Two callers that both found the fence free and the queue
  empty prepare concurrently, and the second's re-check routinely loses to
  the first's numbering; it then drops the reservation and view stamp,
  takes its queue place and waits again.
- **Not for an allocation nothing will clear.** An uncertain ALLOCATE
  (`allocation_uncertain`) still refuses at once, so cleanup callers still
  block the session on it. A failed ALLOCATE (new
  `hfs_request.allocation_failed`; nobody retries it) and a closing session
  also refuse at once.
- **Ordinary opcodes wait only for an ordinary owner** (or a free fence with
  an earlier queued caller). That owner's window is the ALLOCATE exchange
  `hfs_request_run` starts at once: one host round trip that takes no VFS,
  page or mmap lock, waits on no workqueue and is not parked behind recalls
  (`guestbridge` skips `ViewProtocol.CheckRequest` for ALLOCATE), so a
  waiter holding `i_rwsem` or `mmap_lock` cannot stall it. An earmark
  (`hfs_dirty_reserve`) holds the fence across DIRTY_RESERVE and, after a
  refusal, until the mmap cleanup worker resumes or cancels it, which
  depends on recalls and view STOP; ordinary callers keep the immediate
  `-EAGAIN` there. The four cleanup opcodes also wait for an earmark: their
  callers (`__fput`, `filp_close`, the control workqueue; READDIR's FORGET
  holds only the directory's shared `i_rwsem`) hold nothing an earmark
  needs.
- **Signals.** Ordinary callers sleep killable and return the old `-EAGAIN`
  on a fatal signal; cleanup callers sleep uninterruptibly (they cannot
  retry). Neither sleep is freezable and the freezer does not wake them,
  so a suspend during such a wait can fail (the default 20 s freeze timeout
  is below the 30 s bound; irrelevant on the lab guests). A syscall can now
  take up to 30 s of
  fence wait plus its own 30 s request window (docs/Test-automation.md).
- **No lasting deadlock.** The wait is bounded and then fails as before.
  An ordinary owner is a running thread past its admission check, never a
  work item queued behind the waiter, so a waiter on the control workqueue
  (max_active 2) cannot hold back its own owner; earmarks are resolved by
  the faulting thread or `cleanup_queue`, never `session->control`.
  `hfs_call` is never entered with `session->lock` or `session->writer`
  held, and the waiter holds neither while it sleeps.

`hfs_call` was split into `hfs_call_admit[_locked]`, `hfs_call_sleep`,
`hfs_call_dequeue`, `hfs_call_reserve`, `hfs_call_prepare`,
`hfs_call_number_locked` and `hfs_call_once`; the request setup itself is
unchanged. The CALLERS_UNFUNDED/ENOSPC behaviour is unchanged: reservation
failures are not fence waits. Waiting widens a pre-existing race:
`hfs_flush` takes its `close_sequence` before `hfs_call`, so two concurrent
closes of one description can reach the host out of sequence order.

Host dependency (task 194): the daemon's ALLOCATE admitted credentials with
`allowExited` only for GAP_RESOLVE (`internal/guestbridge/controlled_protocol.go`
`allocation`), while the CALL accepts an exited issuer for
DESCRIPTION_RELEASE and FORGET (`originalCleanup`). At exit the waiting
release normally *loses* that race: PROCESS_EXIT's CALL runs first on the
host's single ordinary-lane worker, `finishLifetime` records the process as
exited, and the release's ALLOCATE then gets Stale. The same holds for a
FORGET queued later from `hfs_evict_inode` and for an fd inherited by a
child that outlives its opener. Without 194 such a release or FORGET still
blocks the session; x84 only removes the guest-local refusal.
controlled_received `--scenario exit-cleanup`
(`TestControlledReceivedActualExitThenCleanup`) runs PROCESS_EXIT, then
the same process's DESCRIPTION_RELEASE and FORGET through the production
callers against the real host, and requires both to succeed without
blocking. It requires host task 194 (merged in 10b76df) and passes on
main.

DESCRIPTION_CLOSE after PROCESS_EXIT (194 refuses it with STALE at
ALLOCATE): `hfs_process_exit` marks the process exited before queueing
PROCESS_EXIT (`hfs_session.c` sched_process_exit hook, `atomic_cmpxchg` on
`process->exited`), and `hfs_capture_owned` then refuses that process with
`-ESTALE`, so a later `hfs_flush` sends nothing (`hfs_file.c`, the
`hfs_capture` call); the last exiting thread skips the close (`PF_EXITING`
with `signal->live == 0`), and a child closing an inherited fd uses its own
live process. A thread in an uninterruptible cleanup wait cannot let its
process's `live` count reach zero. Residual: a non-last thread with its own
file table (CLONE_THREAD without CLONE_FILES, or `unshare(CLONE_FILES)`)
that captured before the exit mark can send its close after PROCESS_EXIT.
The host then refuses its ALLOCATE; like any refused ALLOCATE today that
leaves the fence held (failed or uncertain): close(2) gets the error and
later requests on the mount refuse with `-EAGAIN`. Not exercised here.

zq3-r10 R10-2 (z84, patches 0003, 0004 and 0005): `hfs_controlled_stop`
accepted only an UNBOUND or quiescent view, but a recall's REOPEN leaves the
view LIVE with `quiescent` false, and `v9fs_hfs_kill_super` ran the stop
before `kill_anon_super` and the control flush. Every clean unmount of a
once-reopened mount logged `blocked by failed provider stop on STOP (-16)`.

- kill_sb now sets closing/blocked (waking fence waiters), tears the
  superblock down, flushes the control workqueue, settles the view
  (`hfs_mmap_settle`, patch 0004) and only then stops.
- `hfs_mmap_settle` is best-effort. It flushes the mmap `cleanup_queue`
  once, which sends the lease releases the last I/O queued (observation
  ends are released asynchronously there), then waits at most
  `HFS_UNMOUNT_SETTLE_MS` (2 s) while a recall or remaining owner is
  visible. The host may start a release's recall only after it returns;
  that is harmless, since no physical owner remains and the stop's verdict
  only decides whether kill_sb logs. A FENCED view ends the wait at once
  (it never settles); the recall phases (STOPPED, drains, FINAL) still
  progress to LIVE and are waited for. Cleanup work takes no superblock or
  inode lock and the flush is bounded by the releases' own exchange
  timeouts. A cleanup that requeues itself (a refused release retried
  every 10 ms) is not chased.
- `hfs_controlled_view_idle` accepts, besides the old cases, a LIVE hfs1.4
  view with zero active, dirty, cached, observation and mapping counts and
  no cookies. Zero is final because no inode, file, mapping or page of the
  superblock survives `kill_anon_super` to take a new token, page or
  observation. Any remaining owner, RECALLING (a recall still running after
  the settle bound) and the drain phases still refuse and log.

Tests:

- controlled_received `x84_test.inc` (every scenario): the decision matrix
  including FIFO order; the bounded wait (admits after a concurrent clear,
  leaves the queue at its deadline, refuses uncertainty at once); a failed
  ALLOCATE through the actual `hfs_request_allocate` wakes a waiter, which
  returns at once (removing `allocation_failed = true` makes it sleep to its
  deadline and fail); the unchanged `hfs_release` against a concurrent
  owner (waits, then numbers), a fence injected between `hfs_call`'s two
  checks (retries once and numbers after the clear; removing the retry
  fails it) and an uncertain owner (refuses at once, blocks); after the
  process is marked exited `hfs_flush` sends nothing (-ESTALE, or 0 for the
  last thread) and the session stays usable. A no-wait mutant fails the
  matrix.
- `--scenario cleanup`: every production cleanup caller waits for a pending
  ALLOCATE and still reaches the host exactly once (run with a 900 s
  timeout, see its README). `--scenario exit-cleanup`: see above.
- mount_hello `unmount-reopened-view`: an idle LIVE view and one whose clean
  pages the teardown releases stop silently, a surviving cached page, an
  active token and RECALLING still log, and kill_sb must stop after the
  teardown, flush and settle; reverting the order, the LIVE acceptance or
  the `nr_cached` check each fails it.
- The rotted tests/controlled harness (s54) gained the extraction span and
  wait stubs; it still fails to build on its pre-existing errors.

The series was regenerated with `export.py --task x84-z84` (0003, 0004 and
0005 change). A fresh `prepare.py` tree applies it with fuzz zero and
`fs/9p/`, `net/9p/` and `random.o` compile without warnings under
`kernel.py configure`. mount_hello, tag, view and view --pressure (plain and
`--sanitize`), controlled_received `terminal-smoke` and `cleanup`, requests,
readdir_cleanup, transport, huge, policy and cursor pass; root, capture,
reclaim and codec fail as on the unchanged tree (s54). No candidate was
rebuilt or admitted.

## Bounded view entry and the worker-stop log (`1a4`)

zq3-r11 R11-2 (patch 0004, `hfs_view.c`): `hfs_view_enter(wait=true)` slept
in `wait_event_killable` until the view was LIVE, FENCED or its hfs1.4
session closing or blocked, with no time bound. In r11 the host's recall
stayed in WAIT_DRAINED for about nine minutes and never finished, so m1's
stat stayed in D and the umount of m1 hung (umount(8) stats the mount point
first); the guest poweroff took about 530 s.

- **Bounded wait.** The wait now ends after `HFS_VIEW_ENTER_WAIT_MS` (30 s,
  `hfs_view.h`; the guest's request window and x84's
  `HFS_ALLOCATION_WAIT_MS`) with `-ETIMEDOUT`. The deadline is fixed at
  entry, so intermediate recall phases that wake the waiter do not extend
  it; a view that settles exactly at the deadline is still admitted. A
  fatal signal still ends the wait with `-ERESTARTSYS`. A clean recall
  takes well under a second, so the bound only fires on a stuck one.
- **Callers.** No caller blocks the session on the timeout. Observation
  begin (`hfs_mmap_begin`, i.e. getattr, lookup, readdir, open, read/write
  and the other requests through `hfs_begin`) returns it to userspace
  before anything is sent; a page fault raises SIGBUS
  (`hfs_mmap_fault_error`; retrying it with `VM_FAULT_NOPAGE` would
  restore the unbounded wait); page-less fork and mremap entry
  (`hfs_view_page_enter`) fail the fork or the move.
- **Committed but not adopted.** One waiting caller runs after the host
  committed: `hfs_attr_install`'s initial install of a new inode from
  CREATE, MKDIR or SYMLINK. On a timeout the object exists on the host but
  the guest neither installs nor adopts it: `iget_failed` sends no FORGET,
  `hfs_create` discards its description locally and `hfs_result_adopt`
  never runs, so the request stays terminal and unadopted, its result is
  retained and it pins its retirement floor; userspace gets ETIMEDOUT
  although the object exists. The session is not blocked. That path logs
  (ratelimited) `9p-hfs: session <uuid> MKDIR committed but its initial
  attributes timed out behind a recall (-ETIMEDOUT); result retained
  unadopted, retirement floor pinned`, the opcode taken from the request's
  intent (`hfs_attr_timeout_warn`).
- **Total syscall bound.** The view wait is one of several 30 s windows: a
  getattr can pay the view wait, its observation's VIEW_ACQUIRE exchange
  (`hfs_view_exchange`), x84's allocation wait and its own request window,
  about 90-120 s in the worst case. read()/write() into an hfs `MAP_SHARED`
  buffer pays more: the fault inside its observation pays another view wait
  and its exchanges.
- **Faults that cannot succeed (pre-existing, same liveness class).**
  `hfs_mmap_fault_error` retried -ESTALE, -EAGAIN and -ERESTARTSYS with
  `VM_FAULT_NOPAGE` even when no retry could succeed. After `hfs_mmap_fail`
  fenced the view, every fault on an uncached page retried forever, and a
  kernel uaccess fault (read(2) into an hfs mapping) looped unkillably
  (x86 checks signals only on `VM_FAULT_RETRY`). It now returns SIGBUS when
  the view is FENCED, the session closing or blocked, or a fatal signal is
  pending (`hfs_mmap_fault_final`); transient refusals still retry.
- **Unmount.** umount(2) enters no view (`hfs_d_revalidate` is local and the
  mount point walk calls no hfs operation); umount(8)'s stat of the mount
  point now returns ETIMEDOUT after 30 s instead of hanging (not verified:
  what util-linux umount does after such a stat; `umount -c` skips the
  canonicalizing stat). kill_sb is
  unchanged and bounded as before (z84's settle waits at most 2 s); the
  settle now also wakes any parked entrant, which then sees `closing` and
  refuses at once instead of at its bound. Normally none is parked, since a
  syscall holds the mount busy until it returns.

`hfs_mmap_fail` (patch 0004): the ratelimited `view control worker stopped,
error -116` named neither the session nor what failed. It now logs once per
session, as m64's `hfs_session_block` (plain `pr_warn`, own once bit
`HFS_REPORTED_VIEW_STOP` in `hfs.h`):

```
9p-hfs: session <uuid> view control worker stopped, error -116, at DRAINED (event INSTALL_DRAIN, ack DRAINED, host status STALE, view phase INSTALLED_DRAIN, recall <uuid>); later operations on this mount fail
```

The step is VIEW_WAIT, EVENT, DRAINED, DRAIN_WRITE or CLEANUP. The event
executor (`hfs_mmap_events`, `hfs_mmap_drained`; one item at a time on its
max_active-1 queue) records the event phase it is processing, the ACK it
sent and the host's reply status in `struct hfs_mmap_control`; "-" means
none (an ACK that got no reply, e.g. a transport timeout, has status "-").
Only `[A-Z_]` words are copied from host replies (`hfs_mmap_word`). The once
bit is independent of `hfs_session_block`'s: a later independent block of
the same session (a QUERY timeout, a failed FORGET) still logs its own line,
and a session that already logged a block still logs the stop. The recall
is printed for event executor failures only (other workers would race with
its writer); otherwise it is "-".

Tests:

- `view`: a STOPPED view that never reopens times out after exactly
  `HFS_VIEW_ENTER_WAIT_MS` (also across a jiffies wrap, holding no module,
  kref or active token afterwards); intermediate phase changes do not extend
  the deadline; a reopen one jiffy before and exactly at the bound is
  admitted; FENCED during the wait gives `-ESTALE`; a fatal signal gives
  `-ERESTARTSYS` at once. The view compat gained a settable `jiffies`
  (moved from the pressure harness) and a `wait_event_killable_timeout`
  substitute that advances it. The pre-1a4 untimed wait aborts the case
  (`untimed wait_event_killable`), and so does a deadline restarted on
  every wake-up (`unbounded wait`).
- `view --pressure`: `hfs_mmap_fault_error(-ETIMEDOUT)` is SIGBUS (fails if
  the fault retries it); the worker-stop line for r11's case is checked
  byte for byte, with the actual `hfs_mmap_note_event`/`hfs_mmap_word`
  filling the record, once per session, bit 0 untouched, a VIEW_WAIT stop
  without a reply printing "-" (also for the recall), and non-word host
  text replaced by "?". Dropping the once bit or the word filter fails it.
  Review round 1 adds: the committed-result line names the session and
  MKDIR from the intent and does not block the session ("?" for a non-word
  opcode); `hfs_mmap_fault_error` returns SIGBUS for -ESTALE/-EAGAIN/
  -ERESTARTSYS/-ENOSPC once the view is FENCED, the session closing or
  blocked, or a fatal signal is pending (no pressure release then), and
  still NOPAGE for a transient -ESTALE/-EAGAIN.
- controlled_received `build.py` extracts the new helpers and struct.

The series was regenerated with `export.py --task 1a4` (0003 and 0004
change: `hfs.h`, `hfs_session.c` comment, `hfs_inode.c`; `hfs_view.[ch]`,
`hfs_mmap.c`); review round 1 regenerated it again.
A fresh `prepare.py` tree applies it with fuzz zero and `fs/9p/`, `net/9p/`
and `random.o` compile without warnings under `kernel.py configure`.
view and view --pressure (plain and `--sanitize`), mount_hello and tag
(`--sanitize`), controlled_received `terminal-smoke`, `cleanup` and
`exit-cleanup`, requests, huge, transport, policy and cursor pass. No
candidate was rebuilt or admitted.

## Exporter ERROR reasons and blocked retries (`oc4`)

zq3-r12 R12-1: the host runtime's guest transport failed, the exporter
answered both sessions' outstanding VIEW_WAIT FETCHes with ERROR, and the
guest mapped that, like every non-FRAME reply, to `-EPROTO` (-71). The
session was then blocked, but mount-1 hung for the full 30 s, because
`hfs_transport`'s retry loops ignored `session->blocked`.

- **ERROR reasons.** `hfs_bridge_error_reason` (0002, `hfs_policy.c`) reads
  the u16 reason of a well-formed ERROR reply (0 otherwise), and
  `hfs_bridge_error` maps it: RETIRED_CELL (4) is `-ESTALE`, the new
  UNAVAILABLE (5, the exporter's EHOSTDOWN, task `pc4`) is `-EHOSTDOWN`,
  everything else stays `-EPROTO`. Both SEND (`hfs_send_reply`, CAPACITY is
  still the only retried reason) and FETCH use it. The values are
  `HFS_ERROR_*` in `hfs_policy.h`, matching the exporter's `HFS9P_ERROR_*`
  (the trees share no header). Callers that block only on `-EPROTO` (view
  ticket and dirty reservation replies) see no change in practice: a failed
  transport already blocks the session in `hfs_control`.
- **Raw reason in the log.** A failed exchange records its reason in
  `session->exporter_error` (`hfs_transport_note`). `hfs_session_block`
  now prints `blocked by <reason> on <op> (<errno>), exporter ERROR <n>;`
  and `hfs_mmap_fail` `error <errno>, exporter ERROR <n>, at <step>`, with
  `-` when the session never got an ERROR. It is the session's latest
  ERROR, normally the exchange that caused the block.
- **Blocked retries.** `hfs_transport` was split into
  `hfs_transport_send`, `hfs_transport_fetch`, `hfs_transport_chunk` and
  `hfs_transport_collect` (plus `hfs_transport_note` and
  `hfs_transport_stop`). Both retry loops, the VIEW_QUERY polls of
  `hfs_view_exchange` and the CALL->QUERY polls of `hfs_request_run` stop
  with `-EHOSTDOWN` once the session is blocked (`hfs_session_poll_halt`).
  Exempt is only traffic that a blocked session still needs:
  `hfs_after_block_operation` lists RETIRE, CANCEL, RELEASE_CELL,
  SESSION_RETIRE, VIEW_RELEASE, VIEW_CANCEL, VIEW_RETIRE, READDIR_CLEANUP and
  the cleanup CALL opcodes FORGET, DESCRIPTION_CLOSE, DESCRIPTION_RELEASE and
  PROCESS_EXIT. Polls inherit the exemption of the operation they serve
  (`hfs_control_flagged`; plain `hfs_control` passes the operation's own).
  `hfs_control_flagged` decides the exemption under `session->lock` at
  admission, also exempting a session in teardown (`closing`,
  `unmounting`). A session in teardown is exempt at every check too:
  kill_sb and `hfs_mount_fail` order `unmounting`/`closing` before
  `blocked` with `smp_wmb()`, paired with `smp_rmb()` in
  `hfs_session_poll_halt`. Review round 1 replaced the first version, which
  exempted any exchange that started on an already blocked session: a
  session blocked between two polls still ran the full 30 s, and a block
  between admission and that read was mistaken for an exemption. (Halting
  every exchange broke `controlled_received terminal-smoke` with -112, which
  is why retirement traffic is exempt.) The FETCH loop checks after its
  sleep, so a block raised then sends no further FETCH.
- **ALLOCATE keeps -EPROTO** for RETIRED_CELL: its session is not blocked
  (only `allocation_uncertain`), and a VFS caller seeing `-ESTALE` could
  retry once and replay the uncertain allocation.
- **Unmount stays bounded when the host is gone (zq3-r13).** After the
  host runtime was stopped, m0's umount (its session not blocked) hung
  > 270 s in `hfs_control`: each exempt teardown exchange retried to its
  own 30 s deadline. An exchange that hits its deadline without an answer
  (`hfs_transport_stop`) or gets ERROR UNAVAILABLE (`hfs_transport_note`)
  now sets `session->transport_failed`, and `hfs_session_poll_halt` then
  stops every later retry or poll at its first retry, exempt and teardown
  traffic included. An unmount after the host is gone therefore waits at
  most one deadline. A CAPACITY refusal at the deadline does not set it
  (the exporter answered).
- **Bounded reply wait (zq3-r13 R13-1).** The hfs wait in
  `p9_client_rpc_v` (0001, `net/9p/client.c`) was an unbounded
  `wait_event`: when the exporter sent no reply, virtio dropped bhyve's
  empty completion and the sender hung while holding `session->writer`, so
  umount's `hfs_control` hung behind it in D state. The wait is now
  `wait_event_timeout(..., 30 * HZ)` (still not killable: no Tflush may
  cancel an admitted claim). On expiry the exchange returns `-ETIMEDOUT`,
  the request stays allocated (a fixed tag then stays busy and later
  exchanges on it get the bounded `-EBUSY`), and `hfs_transport` sets
  `transport_failed`. The exporter (pc4) now always replies to a
  well-formed exchange, so this is the backstop.

Tests:

- `transport`: an injected ERROR reply to the FETCH of a VIEW_WAIT (tag
  65534, recall channel 3) and of a VIEW_QUERY (tag 65533, result channel 2)
  gives `-EHOSTDOWN` (5), `-ESTALE` (4) or `-EPROTO` (1, 2, 3, a high byte,
  malformed lengths), records the raw reason (none for malformed replies)
  and sends no further FETCH; a SEND ERROR 5 or 4 likewise. A block raised
  in the third SEND CAPACITY retry or the fourth FETCH AGAIN poll stops the
  exchange there with `-EHOSTDOWN`, and an exchange started on an already
  blocked session stops at its first retry; unmounting, closing or exempt
  traffic keeps retrying (to the deadline, or to the reply), unless an
  earlier exchange timed out or got UNAVAILABLE (`transport_failed`): then
  even exempt unmount traffic stops at its first retry. Making
  `hfs_transport_halt` never halt fails it. `run.py` now extracts from
  `hfs_transport_note` to `hfs_transport`.
- `mount_hello`: the block line carries `exporter ERROR -` and, for a
  session whose last ERROR was UNAVAILABLE, `(-112), exporter ERROR 5`.
- `view --pressure`: the worker-stop lines carry `exporter ERROR -`, and an
  UNAVAILABLE session prints `error -112, exporter ERROR 5`.
- `dirty`: a session blocked while the first DIRTY_FREEZE reply (PENDING)
  is out sends no VIEW_QUERY round and returns `-EHOSTDOWN` with the ticket
  uncertain; disabling the poll check fails it. The harness did not
  compile before (missing `hfs_received_copy`, `hfs_session_block` and
  policy compat) and its fixture never set `family.anchor`; both are fixed,
  and its earlier cases pass again.
- `tag`: a request whose reply never arrives (`drop_next`, as virtio drops
  an empty completion) returns `-ETIMEDOUT` after the bounded wait (300 ms
  in the harness, which defines `P9_HFS_REPLY_WAIT` itself).
- `transport`: an exchange that times out, gets UNAVAILABLE or no 9p reply
  sets `transport_failed`; afterwards exempt unmounting traffic stops at
  its first retry. A CAPACITY refusal at the deadline does not set it.
- controlled_received `build.py` extracts the new helpers and
  `hfs_control_flagged`.

The series was regenerated with `export.py --task oc4` (0002, 0003 and
0004 change: `hfs_policy.[ch]`; `hfs.h`, `hfs_session.c`; `hfs_mmap.c`, and since review round 1 `hfs_lease.c`;
the zq3-r13 fix also changes 0001, `net/9p/client.c`).
A fresh `prepare.py` tree applies it with fuzz zero and `fs/9p/`, `net/9p/`
and `random.o` compile without warnings under `kernel.py configure`.
transport, policy, cursor, tag, mount_hello, view and view --pressure
(`--sanitize`), requests, huge, dirty and controlled_received `terminal-smoke`,
`cleanup` and `exit-cleanup` pass. codec (`test.c:366`), capture, root and
reclaim fail as before (s54). No candidate was rebuilt or admitted.
