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
The VFS's ATTR_KILL_* bits are consumed first (`vf4`, below): the host clears
the setuid/setgid bits on ownership changes and non-root writes/truncates.
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
python3 tools/integration/linux9p/tests/setattr/run.py \
  --source /owned/run/linux --output /owned/run/setattr-check --sanitize
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
  PROCESS_EXIT, and since `dh4` the reserved drain and reconciliation work
  DRAIN_WRITE and GAP_RESOLVE. Polls inherit the exemption of the operation they serve
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
  most one deadline. Retries that reach the deadline (30 s of CAPACITY
  or FETCH AGAIN) set it too, also against a live but refusing exporter.
  That is acceptable: after host loss the exporter pins the refused native
  write and answers AGAIN forever, so the deadline is the only signal of
  that loss, and a session that waited 30 s is blocked by `hfs_control`
  anyway. Only a refusal whose own reply crosses the deadline does not set
  it. ALLOCATE's deadline never does (review round 2,
  `HFS_TRANSPORT_ALLOCATE`): it only sets `allocation_uncertain`, and a
  healthy session must keep working after it. ERROR 5, a 9p reply-wait
  timeout and a failed endpoint still set it for ALLOCATE. The flag is
  never cleared within a session, by design: a session that lost its host
  is blocked and retained, not resumed; a new mount gets a new session.
- **Bounded reply wait (zq3-r13 R13-1).** The hfs wait in
  `p9_client_rpc_v` (0001, `net/9p/client.c`) was an unbounded
  `wait_event`: when the exporter sent no reply, virtio dropped bhyve's
  empty completion and the sender hung while holding `session->writer`, so
  umount's `hfs_control` hung behind it in D state. The wait is now
  `wait_event_timeout(..., 30 * HZ)` (still not killable: no Tflush may
  cancel an admitted claim). On expiry the exchange returns `-ETIMEDOUT`,
  the request stays allocated (a fixed tag then stays busy and later
  exchanges on it get the bounded `-EBUSY`), and `hfs_transport` sets
  `transport_failed`. The transport still owns that request's reply
  buffer, and its callback reference is dropped only if a late reply
  arrives. So `p9_client_destroy` keeps an hfs client whose idr still holds
  requests (`p9_client_hfs_owned`, review round 2): it neither frees them
  nor closes the transport (virtio's channel stays `inuse`, so no later
  mount takes it), and leaks the client until reboot. Freeing them let a
  late reply write freed memory and run `p9_client_cb` on a freed request. The exporter (pc4) now always replies to a
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
  its first retry. A refusal whose own reply crosses the deadline and an
  ALLOCATE that reaches it do not set it.
- `tag`: after the silent request times out, `p9_client_hfs_owned` keeps
  the client; a late reply then completes and frees the request, and the
  client is no longer owned.
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

## Observation re-check retry (`ie4`)

zq3-r15 R15-1: T1's `os.fstat` of the m0 root failed with `ESTALE` before
its GETATTR was numbered. `hfs_mmap_begin` (patch 0004) captured the view
stamp, took its DOMAIN READ lease with VIEW_ACQUIRE (PENDING while a recall
ran, most likely the scoped recall started by the open's own lease release),
and then re-checked the stamp. The recall had reinstalled the same cut, so
the host granted the lease at the expected target, but the view revision
had moved and the re-check returned `-ESTALE` without a log line.

Patch 0004, `hfs_mmap_begin`, now split into `hfs_mmap_observation_new`,
`hfs_mmap_observe`, `hfs_mmap_publish` and `hfs_mmap_retry_allowed`
(review round 1 applied):

- **View moved past a held lease** (revision or generation changed, not
  LIVE, or another target): acquire again, and release the old lease only
  after that acquisition (`-EXDEV` path). The lease is never re-stamped
  (review round 1): the host deactivates every lease of a recall
  participant at STOPPED, published or not (`quiesceParticipants`,
  `internal/viewcontrol/recall.go`), the guest's STOPPED revokes only
  published ones, and an equal target cannot tell a grant after the recall
  from one before it. Publishing under a quiesced lease would let a peer's
  write pass the conflict check without a recall.
- **No self-recall loop:** if the old lease is still active (granted after
  the recall, the R15-1 case), the new acquisition shares it
  (`grantLease` → `shareLease`) and the old release only returns a holder
  (`releaseLease` → `releaseHolder`). If a recall quiesced it, the new
  lease has the advanced fence, another lineage (`sameLineage` compares
  `Fence`), so the old release finds no active sibling
  (`releaseNeedsRecall`) and is RELEASED without a recall. Every packet is
  queued exactly once, so nothing leaks and nothing is released twice. A
  held lease is never published, so recall ACKs neither wait for nor revoke
  it.
- **Mutation** (no lease): a moved stamp or a view that is not LIVE captures
  the view again.
- **Host BLOCKED** (or RETIRED_ID): the acquisition's own `-EAGAIN`, without
  a lease, is returned at once as before ie4; it is not a re-check and is
  never retried back to back (final review). Since re4 a BLOCKED is retried
  after retirement progress instead (see below); RETIRED_ID still returns
  at once.
- **Refused:** a FENCED view or a closing or blocked hfs1.4 session
  (`hfs_view_refused`, now shared with `hfs_view_try_enter_locked`) keeps
  `-ESTALE`. A retry also stops on `transport_failed` (oc4; `-ESTALE`) or a
  fatal signal (`-EINTR`).
- **Bound:** the retries share one `HFS_VIEW_ENTER_WAIT_MS` (30 s) deadline
  from the call's start (`hfs_view_enter_until` takes it; 1a4's
  `hfs_view_enter` is now its wrapper). Exhaustion is `-ETIMEDOUT`.
- **Logs:** each failed re-check logs a rate-limited `pr_info` line
  (`observation re-check failed (<reason>); <action>`), and exhaustion a
  rate-limited `pr_warn` (`observation retries exhausted after <n> failed
  re-checks (-ETIMEDOUT)`).

Patch 0003, `hfs_attr_install`: a cache observation (GETATTR, LOOKUP,
READDIR, READLINK, STATFS) whose request stamp no longer admits it, on a view
that is not refused, waits for a LIVE view (`hfs_attr_restamp`, its own
30 s: it has no handle on the observation's deadline, see `hfs_view.h`) and
installs if the view still has the target the request was stamped at
(`hfs_request.view_target`, taken by `hfs_view_snapshot_target` in
`hfs_call_prepare`). A moved target (a peer's pinning LOOKUP, FORGET or
DESCRIPTION_RELEASE moves the cut) answers the kernel-internal
`-HFS_ETARGET_MOVED`: `hfs_getattr`, `hfs_refresh` and `hfs_lookup` re-issue
their GETATTR or LOOKUP (`hfs_reissue`) within a 30 s deadline from the
syscall's start, stopping on a fatal signal, `transport_failed` or the
deadline (`-ETIMEDOUT`, one line). READDIR, whose cursor positions are
already recorded, turns it into `-ESTALE`. Each failed check logs a
rate-limited line. A moved LOOKUP's first result is not dropped (final
review): `hfs_inode_get_fail` links its forget record (count 1) like a
successful lookup's, so eviction sends its FORGET (at once for a new inode
that `iget_failed` evicts, else with the existing inode), and
`hfs_lookup_once` adopts the result; the host lookup count stays balanced
and the request pins no retirement floor. `hfs_attr_install`'s view entry
moved to `hfs_attr_enter`.

Tests (`view --pressure`, which now extracts `hfs_mmap_begin` through
`hfs_mmap_end`, `struct hfs_cache_cleanup`, `hfs_attr_restamp`,
`hfs_reissue`, `hfs_attr_enter`, `HFS_ETARGET_MOVED`, `struct hfs_forget`,
`hfs_inode_link_forget`, `hfs_inode_get_fail` and `hfs_evict_inode`). The acquisition and release follow a
model of the host controller: a recall deactivates the domain's leases and
advances its fence, a grant shares the active lease of the same lineage (fence
and target), a
non-last holder's release returns a reference, any other release of an
active lineage starts a recall that runs during the next VIEW_ACQUIRE:

- a recall completes while VIEW_ACQUIRE is PENDING (R15-1): a second
  acquisition shares the lease, the published lease is active, and the old
  release starts no recall;
- a recall between the grant and the re-check quiesces the unpublished
  lease: it is not published, a new lease at the next fence is, and the old
  release starts no recall (fails on round 0's re-stamp);
- self-recall loop: each operation's release recall runs during the next
  operation's acquisition; three operations, no release recall inside any
  of them;
- a grant at another target: a second acquisition, a new lineage, while the
  first lease is still held; the stale lease's release comes after the
  publication;
- a fence, a blocked hfs1.4 session and `transport_failed`: `-ESTALE`, one
  acquisition, its lease released once;
- the host answers BLOCKED: `-EAGAIN` after one acquisition, no re-check
  line (fails when the acquisition's `-EAGAIN` is retried);
- a moved LOOKUP's forget record: a new inode's eviction sends its FORGET,
  an existing inode keeps it and its eviction sends both, other failures
  free the unlinked seed (fails when the moved record is dropped);
  `hfs_attr_enter` skips a mutation's stale delayed result;
- deadline: three stale grants 10 s apart, and a recall that never ends,
  give `-ETIMEDOUT` with the exhaustion line and every packet queued once;
- every case ends with no lease, packet, credit or control reference left;
- `hfs_attr_restamp`: same target re-stamps, a moved target asks for a
  re-issue with no entry held, a fenced view is `-ESTALE` without a line;
  `hfs_reissue` re-issues only `-HFS_ETARGET_MOVED` and stops on
  `transport_failed`, a fatal signal and the deadline.

On the pre-ie4 re-check (a moved stamp is `-ESTALE`) the first case fails;
with round 0's re-stamp the published lease is not the one the re-check
should have demanded, and releasing the stale lease before the retry's
acquisition starts a release recall; both fail.

The series was regenerated with `export.py --task ie4` (0003: `hfs.h`,
`hfs_session.c`, `hfs_inode.c`; 0004: `hfs_view.[ch]`, `hfs_mmap.c`).
A fresh `prepare.py` tree applies it with fuzz zero and `fs/9p/`, `net/9p/`
and `random.o` compile without warnings under `kernel.py configure`.
transport, policy, cursor, tag, mount_hello, view and view --pressure
(`--sanitize`), requests, huge, dirty and controlled_received `terminal-smoke`,
`cleanup` and `exit-cleanup` pass. codec, capture, root and reclaim were not
run (broken before, s54). No candidate was rebuilt or admitted.

## BLOCKED observation back-off (`re4`)

zq3-r16 R16-1: T1's m1 root OPEN failed with `EAGAIN`. The host answered
BLOCKED to m1's ie4 re-acquire because the shared `ControlTickets` budget (8)
was full: unretired VIEW_RELEASE tickets (the guest offered retirement only at
the end of a cleanup packet and dropped a BLOCKED VIEW_RETIRE), the holders of
m0's shared lease and the lease ie4 still held. ie4 returned that BLOCKED as
`-EAGAIN` at once.

Patch 0004:

- `hfs_lease_acquire_ex` (`hfs_lease_acquire` is its wrapper) reports the
  host's terminal BLOCKED (`*blocked`) apart from RETIRED_ID and local
  `-EAGAIN`. The host gives no reason with BLOCKED.
- `hfs_mmap_begin` (split into `hfs_mmap_attempt`, `hfs_mmap_moved` and
  `hfs_mmap_release_held`): on a BLOCKED observation acquisition the held
  lease (ie4's stale lease, if any) is released at once, before the next
  try, and its packet queued. `hfs_mmap_blocked_backoff` pauses 50 ms
  (doubling), runs `hfs_view_retire_try` (a `mutex_trylock`) and retries
  **only after progress**: the held release completed, or any floor advanced
  since before the acquisition. Final review: the BLOCKED attempt's own
  packet reaches the cleanup worker during the pause and its retirement
  usually frees the tickets first, so the back-off's own try finds nothing
  left or the mutex busy. `view->retire_progress` counts every floor
  advance (`hfs_view_retire`, under `session->retirement`) and is read before
  each acquisition; a changed counter is progress, and a busy try without it
  pauses again (against the cap). Several threads may retry on one advance;
  each stays within its own cap (since 1h4 a busy mutex is waited for
  instead; see below). Review round 1:
  every BLOCKED acquisition still takes a host ticket number and an identity
  until it is retired, and retirement is refused while a recall runs or the
  same credentials hold an active lease, so a blind 30 s loop could spend
  the 16-ticket budget, end in an uncertain NoSpace ticket and stall the
  family. Retries are therefore capped at `HFS_BLOCKED_RETRIES_MAX` (3; at
  most 4 ticket numbers per syscall). No progress, the cap or an exhausted
  deadline (also when the retirement round ran past it) return the host's
  `-EAGAIN` at once with one rate-limited `pr_warn` (`observation
  VIEW_ACQUIRE BLOCKED, <reason> after <n> attempts (-EAGAIN)`); each pause
  logs a rate-limited `pr_info`. It stops on a refused view (FENCED, a
  closing or blocked hfs1.4 session) or `transport_failed` (`-ESTALE`) and a
  fatal signal (`-EINTR`). RETIRED_ID still returns `-EAGAIN` at once;
  without BLOCKED, ie4's acquire-before-release stays.
- `hfs_view_retire` records whether its pass saw a BLOCKED VIEW_RETIRE
  (`view->retire_blocked`, under `session->retirement`; a complete pass
  replaces it). `hfs_mmap_retire_kick` queues `control->retire`
  (`hfs_mmap_retire_work` on `cleanup_queue`, holding a control reference)
  when it is set: forced after a recall's REOPENED ACK (`hfs_mmap_process`;
  the host ends the recall at that ACK), and at the start of an observation
  at most once per `HFS_RETIRE_KICK_MS` (1 s). `hfs_mmap_quiesce_control`
  cancels a queued work and drops its reference.

The fault path (`hfs_mmap_fault`) is unchanged: its `-EAGAIN` already
re-faults (`VM_FAULT_NOPAGE`), and its packet's cleanup retires.

Tests (`view --pressure`). The fixture host charges every acquisition,
BLOCKED included, one of 16 ticket numbers until a retirement passes it
(asserted at every acquisition), answers BLOCKED while its tickets are full,
and refuses VIEW_RETIRE while a recall runs or is pending or a lease with
holders is active, as `retirementBlocked`:

- BLOCKED once for tickets: one 50 ms pause, one retirement, the second
  acquisition is published (fails pre-re4: `-EAGAIN`);
- R16-1's shape: the held stale lease is released at once (its release
  recall refuses the first retirement; the completed release is the
  progress), its packet is queued before the next acquisition, and the
  retirement after the recall lets the fourth acquisition through;
- persistent BLOCKED with advancing retirements: 4 acquisitions, 3 pauses
  (350 ms), `-EAGAIN` with the retry-limit line, every packet queued once,
  nothing leaked;
- no progress: another observation holds an active lease, or the
  retirement round runs past the deadline: `-EAGAIN` after one acquisition
  with the matching line; a mutex that stays busy pauses three times, then
  the retry-limit line, still one acquisition;
- the cleanup worker (which retires after every packet, as
  `hfs_mmap_cleanup`) retires during the pause; the back-off's own try then
  gets `-EAGAIN` or `-EBUSY`, but the advanced counter lets the retry
  through; a busy try before any progress pauses again and the next retires;
- a fatal signal, `transport_failed` and a blocked hfs1.4 session end the
  back-off before any pause or retirement;
- a BLOCKED retirement during a recall is re-sent by the forced REOPENED
  kick (a second kick drops its reference), after which the next observation
  is granted; unforced kicks are rate-limited; a stopping control drops the
  work's reference without an RPC; the next observation kicks it first;
- RETIRED_ID returns at once; the ie4 cases are unchanged.

Mutations (no progress check, no cap, a blocking retirement, no kick rate
limit, no early held release, no progress counter, `-EBUSY` as a give-up, no
counter snapshot) each fail a case. The flag itself is set by
the production `hfs_view_retire`, which the pressure harness replaces with a
host model; that wiring and the REOPENED kick in `hfs_mmap_process` are
compiled by controlled_received (`build.py` extracts the two new functions;
its `compat.h` gains `mutex_trylock`) and source-reviewed, not executed against a host recall.

The series was regenerated with `export.py --task re4` (0004:
`hfs_view.h`, `hfs_lease.[ch]`, `hfs_mmap.c`). A fresh `prepare.py` tree
applies it with fuzz zero and `fs/9p/`, `net/9p/` and `random.o` compile
without warnings under `kernel.py configure`. transport, policy, cursor, tag,
mount_hello, view and view --pressure (`--sanitize`), requests, huge, dirty
and controlled_received `terminal-smoke`, `cleanup` and `exit-cleanup` pass.
No candidate was rebuilt or admitted.

## Lease grant before the fence raise (`2f4`)

zq3-r17 R17-1: T1's m1 root `fstat` failed with `EKEYREJECTED` and the guest
blocked the session (`uncertain view ticket on VIEW_QUERY (-129)`). The host
grants a PENDING VIEW_ACQUIRE lazily, on the first VIEW_QUERY after the recall
(`queryOrCancelTicket` → `readmit`, `internal/viewcontrol/tickets.go`), right
after it accepted the mount's REOPENED ACK. The guest raises its
`current_fence` only when that ACK's reply arrives
(`hfs_controlled_after_ack`), so the host's correct, verified GRANTED at the
new fence (or one captured at the revision before the REOPEN) was not current
at receipt. `hfs_received_new` kept it as a raw outcome and
`hfs_received_source` reported it as unverified.

Patch 0005:

- A verified SOURCE that is not current at receipt is kept as its own state
  (`owner->stale`, only when its lease target matches the receipt evidence);
  its kind stays raw and its target unset, so no other gate adopts it.
- `hfs_received_source_ex(..., &not_current)` (`hfs_received_source` is its
  wrapper) returns `-ESTALE` with `not_current` for a verified SOURCE that is
  not current at receipt or at consumption; `-EKEYREJECTED` again means only
  "not verified" (no owner, another session/channel/frame, another token, never
  a verified SOURCE). `hfs_controlled.h` gains the uncontrolled
  `hfs_controlled_current_fence` stub.

Patch 0004:

- `hfs_view_exchange` (helpers `hfs_view_exchange_source`,
  `hfs_view_grant_settle`, `hfs_view_grant_look`): only a lease VIEW_ACQUIRE
  GRANTED that is verified but not current waits. Within the exchange's 30 s
  deadline it waits event-driven (`hfs_view_reopen_wait` on `view->wait`,
  woken by `hfs_view_reopen_acked` after each accepted REOPENED ACK in
  `hfs_mmap_ack`; `view->reopen_acks` is read before each look) until the view
  is LIVE (or UNBOUND) and the mount's fence equals the lease's, then sends
  VIEW_QUERY again. The host returns the stored GRANTED (no new ticket,
  identity or lease) and the fresh capture validates it. A re-query is sent
  only after progress (another REOPENED ACK or a new view revision); a reply
  that stays not current at a settled view is `-EPROTO` and blocks as before.
- A lease fence behind the mount's (a later recall passed it) is `-EXDEV`;
  the wait's stops are the deadline (`-ETIMEDOUT`), a fatal signal
  (`-EINTR`), a refused view (FENCED, closing or blocked), a halted session or
  a mount no longer admissible (all `-EHOSTDOWN`; not `-ESTALE`, which
  `hfs_lease_acquire_ex` blocks on as a stale grant). None blocks. In every
  one the exchange returns the GRANTED as its response with the ticket
  terminal, not uncertain, because the host holds that lease and its
  reserved release credit (review 1). `hfs_lease_acquire_ex` then builds it
  **unadopted** (`lease->unadopted`): decoded without the provider's target
  check (`hfs_controlled_target` → `hfs_received_selected` refuses a
  not-current reply by design; before review 1 that made the `-EXDEV` path
  fail with `EKEYREJECTED` and leak the lease), no verified visibility, and
  refused by `hfs_view_bind_initial`, `hfs_view_page_lease`,
  `hfs_view_page_grant` and `hfs_mmap_publish`, so it can only be released.
  It is returned in `*out` with the error. The ie4 observation path
  (`hfs_mmap_observe`) holds an `-EXDEV` lease unpublished and acquires again
  (ie4's acquire-before-release, so its release starts no recall); any other
  error's lease goes to the attempt's cleanup packet, as in the fault path
  and the bootstrap discovery loop (which releases an `-EXDEV` lease and
  retries). `hfs_lease_acquire_ex` blocks on `-ESTALE` only from decoding the
  grant itself.
- DIRTY_RESERVE/DIRTY_FREEZE replies (current only while QUIESCENT/STOPPED)
  and unverified replies still block the session at once; the not-current
  DIRTY case now logs `-ESTALE` instead of `-EKEYREJECTED`.
- Rate-limited lines: `VIEW_ACQUIRE GRANTED at fence F is not current (mount
  fence M, view P); querying again once the view settles`, `... was passed by
  a later recall; releasing it and acquiring again (-EXDEV)` and `... view did
  not settle before the exchange deadline (-ETIMEDOUT)`.

ie4/re4/oc4/1a4 invariants hold: no held lease is re-stamped, every wait is
bounded by the exchange deadline, and no lease, packet, credit or control
reference leaks.

Tests:

- `view --pressure` compiles the actual `hfs_view_exchange` with its helpers
  (transport, ticket reply, session block/poll halt, fence and a model of
  0005's receipt are fixtures; `view_wait_hook` plays the REOPENED ACK reply):
  R17-1's shape (PENDING, GRANTED at fence 6 while the mount is at 5) waits
  for the ACK and re-queries to success (pre-2f4: blocked); a GRANTED
  captured before the REOPEN re-queries at once; a reply that a recall made
  not current at consumption and whose ACK passed the lease returns `-EXDEV`
  (also without waiting when the mount is already past it); deadline,
  signal, blocked session and failed transport stop without a block and
  still hand out the GRANTED (ticket terminal); an unverified, a
  never-current and a not-current DIRTY_RESERVE reply block, a current
  DIRTY_RESERVE passes; an unadopted lease is refused by the initial bind and
  the page lease. `retry_grant_passed`: an observation whose acquisition
  returns `-EXDEV` with an unadopted lease re-acquires at the new fence, then
  releases it without a recall. Twelve mutants (no settle, re-query before
  the fence, DIRTY waits, no `-EXDEV`, no progress check, no ACK count, stops
  block, stops left uncertain, refusal as `-ESTALE`, refusal ignored,
  unadopted bindable, unadopted page lease) each fail a case. dirty's harness extracts the new helpers with aborting
  view fixtures (its minor-0 cases never reach them).
- controlled_received: the actual 0005 receipt on a real host GRANTED: the
  unverified cases keep `-EKEYREJECTED` without `not_current`; after the
  initial bind the lease is `-ESTALE` with `not_current` at consumption; a
  VIEW_QUERY answered while the mount's fence is raised is stale at receipt
  (`-ESTALE`, `not_current`; pre-2f4 `-EKEYREJECTED`), is adopted by no target
  gate and stays stale. These run in the `provider` and `corruption`
  scenarios, which still fail later at their own pre-existing points
  (multipoll EVENT receipt; corrupted-source `IO`), as on the unchanged
  baseline; a mutant that never marks a stale receipt aborts at the ACQUIRE.
- controlled_received `passed-grant` (`TestControlledReceivedActualPassedGrant`,
  review 1): the actual `hfs_lease_acquire_ex` with the real 0005 provider.
  With the mount's fence raised past the held lease's, a second DOMAIN READ
  acquisition (shared by the host) returns `-EXDEV` with an unadopted lease,
  blocks nothing, is refused by the initial bind, and its release completes.
  Decoding it through the provider target instead (the pre-review code)
  returns `-129` there.
- `internal/viewcontrol` `TestPendingAcquireGrantedLazilyAndStable`: a
  PENDING acquisition is still PENDING after its recall completed (fence
  advanced), the first VIEW_QUERY grants at the new fence, and repeated
  queries return the stored GRANTED byte for byte without changing the
  active leases.

The series was regenerated with `export.py --task 2f4` (0004: `hfs_view.[ch]`,
`hfs_lease.[ch]`, `hfs_mmap.c`; 0005: `hfs_received.{h,inc}`,
`hfs_controlled.h`). A fresh `prepare.py` tree applies it with fuzz zero and
`fs/9p/`, `net/9p/` and `random.o` compile without warnings under `kernel.py
configure`. transport, policy, cursor, tag, mount_hello, view and view
--pressure (`--sanitize`), requests, huge, dirty and controlled_received
`terminal-smoke`, `cleanup`, `exit-cleanup` and `passed-grant` pass. No
candidate was rebuilt or admitted.

## Setattr kill bits and dentry operations set once (`vf4`)

zq3-r18 R18-1: T1's `fchown(fd, 41002, 42002)` of a fresh regular file failed
with `EOPNOTSUPP` before any CALL. `chown_common` (`fs/open.c`) adds
`ATTR_KILL_SUID | ATTR_KILL_PRIV` (and `ATTR_KILL_SGID` when needed) for every
non-directory, `notify_change` (`fs/attr.c`) passes them down, and
`hfs_setattr` refused every bit outside its supported set.

Patch 0003 (`hfs_setattr` split into `hfs_setattr_sent`,
`hfs_setattr_supported`, `hfs_truncate_args` and `hfs_setattr_args`; the
request bytes are unchanged):

- `hfs_setattr_sent` computes what is sent: without the kill bits and without
  the MODE `notify_change` derived from them. `notify_change` turns a kill
  bit whose mode bit is set into `ATTR_MODE` (the cached mode without it) and
  BUG()s on a caller's own MODE next to `ATTR_KILL_S*ID`, so that MODE is
  always derived. It is never sent: the host clears 06000 itself where Linux
  asks for it, and the cached mode may be stale (nothing refreshes it before
  fchown), so sending it could overwrite the host's permission bits.
  `ATTR_KILL_PRIV` needs nothing: hfs has no xattr handlers, so
  `security_inode_need_killpriv` is 0 and `notify_change` drops it anyway.
- The support check runs on the sent request. `setattr_prepare` still checks
  the request as the VFS built it, derived MODE included (review 1), so a
  chown of a setuid/setgid file still needs `inode_owner_or_capable`: a
  non-owner with `CAP_CHOWN` but without `CAP_FOWNER` gets `EPERM`, as in
  Linux.
- chown/fchown: only UID/GID are sent; the host clears 06000 on any UID or GID
  change (`attributes`, `internal/fsservice/mutation.go`). An explicit MODE
  with setuid/setgid next to UID/GID stays refused, as the host refuses it.
- `chown(-1, -1)`: a setuid/setgid file is left with only `ATTR_CTIME`, like
  any other file, and gets `EOPNOTSUPP` (there is no ctime-only host
  SETATTR, also before vf4). Linux would clear the bits there; review 1
  chose this over sending a possibly stale cached mode.
- `do_truncate` by a caller without `CAP_FSETID` (truncate, ftruncate,
  open with `O_TRUNC`) of a setuid/setgid file: TRUNCATE clears 06000 for a
  non-root caller (`mutation.go`). An explicit MODE next to SIZE stays refused.
- `__remove_privs` (`file_remove_privs`, `file_modified`) leaves only
  `ATTR_FORCE` (never `ATTR_CTIME`): a local no-op, because WRITE clears
  06000 for a non-root caller (`mutation.go`). `hfs_write_iter` does not call
  it today; this keeps a future caller from failing a write. Before vf4 a lone
  `ATTR_FORCE` sent an empty SETATTR that the host refuses as invalid.
- The host checks uid 0 where Linux checks `CAP_FSETID`. A root caller
  without `CAP_FSETID` therefore keeps its 06000 bits on the host on
  truncate/write. The host's result is the mode the inode installs.

G2 (same rebuild): `hfs_dentry_attach` sets `hfs_dops` only on a dentry
without operations (other operations `WARN_ON_ONCE`, review 1) and replaces
its path grant under `d_lock`, freeing any grant it replaces. `hfs_lookup`,
`hfs_create_install` (CREATE) and `hfs_namespace_install` (MKDIR/SYMLINK) use
it. Before, CREATE/MKDIR/SYMLINK set the operations a second time on the
negative dentry `hfs_lookup` had already set up, and `d_set_d_op` WARNed
(`fs/dcache.c:1924/1925`, a `W` taint since tz3). The replaced grant is NULL
on those paths, because the dentry was negative and `hfs_d_revalidate` makes
every walk look up a fresh dentry, so `d_fsdata` ownership is unchanged.

Review 1, `d_splice_alias` BUG: `d_splice_alias` starts with
`BUG_ON(!d_unhashed(dentry))` (`fs/dcache.c:3150`), and `__d_add` hashes the
dentry of every successful `hfs_lookup`. Two paths looked up an
already-hashed negative dentry a second time:

- `hfs_link`: `filename_create` → `lookup_one_qstr_excl` → `hfs_lookup`
  hashes the target before `vfs_link`, so every successful link(2) would have
  BUGged the guest (T4 `namespace` calls `os.link`). This has existed since
  tz3; no run reached it.
- `hfs_atomic_open`'s CREATE that lost to EEXIST without `O_EXCL`: its first
  `hfs_lookup` already hashed the in-lookup dentry.

Both now use `hfs_relookup`, which `d_drop`s the dentry first, as
`nfs_add_or_obtain` does. The parent's `i_rwsem` is held exclusively
(`filename_create`; `open_last_lookups` for `O_CREAT`), so no parallel lookup
can add another dentry for the name meanwhile, and `d_splice_alias` hashes
it again on success. `finish_no_open` already passes a lookup error through.

Review 1 also splits `hfs_atomic_open` into `hfs_atomic_open`, `hfs_create`,
`hfs_create_args`, `hfs_create_exists`, `hfs_create_inode` and
`hfs_create_install`, and `hfs_namespace` into `hfs_namespace`,
`hfs_namespace_args` and `hfs_namespace_install`. Behaviour and request bytes
are unchanged.

Tests (`tests/setattr`, new): the actual setattr functions, `hfs_setattr`,
`hfs_dentry_attach` with `hfs_dops` and its callbacks, `hfs_lookup`,
`hfs_relookup`, `hfs_link`, `hfs_create_exists`, `hfs_dcap_write`,
`hfs_cap_write`, `hfs_cap_free`, `hfs_args_init` and the kernel's `ATTR_*`
values, with the real `hfs_protocol.c` buffer and parser. `test.c` models
`chown_common`, `do_truncate`, `__remove_privs` and `notify_change`'s
kill-bit conversion, and `setattr_prepare`'s owner check for ATTR_MODE;
`hfs_call` records the exact request bytes; `d_splice_alias` fails the test
where the kernel BUGs:

- chown/fchown of a 0640 file (R18-1's call; pre-vf4 `EOPNOTSUPP`) and of
  04755, 02755, 06755, 06740 and 02740 files sends SETATTR with uid/gid and
  `"mode":null`, after `setattr_prepare` saw the derived MODE; a surviving
  `ATTR_KILL_PRIV` is accepted; chgrp from outside the group of a 02740 file
  likewise;
- a non-owner (no `CAP_FOWNER`) chown of a 04755 file is `EPERM` with no CALL;
  of a 0755 file it succeeds;
- `chown(-1, -1)` of 04755 and 0644 files: `EOPNOTSUPP` without
  `setattr_prepare`;
- unchanged: directory chown, chmod 04755, utimensat bytes;
- kept refusals: explicit MODE 04755 with UID, MODE with SIZE;
- ftruncate of 0644/04755/06755/02750 (with and without `CAP_FSETID`),
  truncate(path) and open(`O_TRUNC`) send the exact TRUNCATE bytes;
- `file_remove_privs` of 04755/02755/06755 files, a lone `ATTR_KILL_PRIV` and
  `ATTR_FORCE` alone: no `setattr_prepare`, no CALL;
- `hfs_dentry_attach`: one `d_set_d_op` (the substitute fails where the
  kernel would WARN), each replaced grant freed (ASan leak check);
- link(2) after the negative lookup: LINK bytes, the target relooked up and
  instantiated with the source inode, operations set once; a name resolving
  to another object is `ESTALE`; the EEXIST open relooks up the hashed
  dentry and returns a lookup error (`EAGAIN`) as is;
- `run.py` requires that `hfs_inode.c` calls `d_set_d_op` only inside
  `hfs_dentry_attach`, that the three creators attach through it, and that
  `hfs_lookup` is called only for the in-lookup open and by `hfs_relookup`.

Mutants each fail a case: the derived MODE kept, `setattr_prepare` after
dropping it, kill bits left in the request, no `ATTR_FORCE` no-op,
unconditional `d_set_d_op`, and `hfs_relookup` without `d_drop` (the source
check, and with it disabled the `d_splice_alias` BUG model).

The series was regenerated with `export.py --task vf4` (0003: `hfs_inode.c`).
A fresh `prepare.py` tree applies it with fuzz zero and `fs/9p/`, `net/9p/`
and `random.o` compile without warnings under `kernel.py configure`.
transport, policy, cursor, tag, mount_hello, view and view --pressure,
setattr (`--sanitize`), requests, huge, dirty and controlled_received
`terminal-smoke`, `cleanup` and `exit-cleanup` pass. No candidate was
rebuilt, booted or admitted, so the WARN's absence, link(2) and T1's fchown
are not yet observed in a guest (zq3-r19).

## BLOCKED re-check after a reinstall, busy retirement waited for (`1h4`)

zq3-r19 R19-1: T1's `credentials_groups` access child got `EAGAIN` on its
open of `group`. The host answered BLOCKED as designed: the child's ticket
carried the target from before the fchown, the host started an
EXPECTED_TARGET recall and answered PENDING, and after the REOPENED ACK
re-admission still mismatched the old expectation (`tickets.go`
`expectedMismatch`; "BLOCKED for the old expectation"). re4's back-off then
retried only after retirement progress. Its retirement try found
`session->retirement` held by the round the REOPENED ACK forces
(`hfs_mmap_retire_kick(view, true)`), counted the `-EBUSY` as an attempt and
paused again. After 4 local loops in about 0.43 s it returned `-EAGAIN`,
with no host round trip after the first answer.

Patch 0004:

- `hfs_mmap_observe` reads `view->reopen_acks` into `attempts->acks` just
  before each capture. On a host BLOCKED, `hfs_mmap_attempt` asks
  `hfs_mmap_reinstalled` (under `view->lock`) whether an ACK arrived or the
  stamp moved since then; a reinstall raises the revision, so a new target
  always counts. If so, `hfs_mmap_blocked_recheck` treats the answer as a
  failed re-check (`hfs_mmap_recheck_note`, `attempts->rechecks`) and
  acquires again at once. There is no pause, no retirement try and no
  progress requirement, and the answer is not counted in
  `attempts->blocked`. It is bounded by the deadline, the stop checks and
  the ticket cap. A BLOCKED on an unchanged view keeps re4's progress rule.
- The ticket bound is unchanged: `attempts->blocked + attempts->reinstalled`
  (the BLOCKED tickets of the call) is capped by `HFS_BLOCKED_RETRIES_MAX`,
  so at most 4 host tickets go to BLOCKED answers per syscall.
- A retirement mutex held by another holder waits instead of counting.
  `hfs_view_retire_try(session, &ran)` now says whether a round ran:
  `-EBUSY` with `!ran` is the held mutex. `hfs_mmap_retire_offer` then calls
  `hfs_mmap_retire_busy`, which calls `hfs_view_retire_wait` (new,
  `hfs_view.c`). That wait is killable, event-driven on `view->wait` and
  bounded by one `HFS_RETIRE_BUSY_SLICE_MS` (200 ms) slice within the
  deadline. It ends when `view->retirement_releases` changes (read before
  the try), when `view->retire_progress` changes, or when the view refuses
  or the transport fails. An expired slice is no host answer either: the
  stops are checked and the offer tries again, uncounted, and only the first
  wait of an offer logs. Before 5f4 a session block or `transport_failed`
  alone did not wake it; those were seen at the next wake-up or the
  deadline, as for `hfs_view_reopen_wait`. Since 5f4 both wake
  `session->stop_wait`, which this wait watches too. After a wake the stop checks run
  first (a refused view or failed transport returns `-ESTALE` without
  another round), then progress retries the acquisition. Otherwise this
  call tries again: a round that freed nothing is followed by its own try,
  and a still-held mutex waits again.
- Every holder of `session->retirement` releases it through
  `hfs_retirement_unlock` (`hfs_view.c`). The holders are the view rounds
  (`hfs_view_retire_mode`), ordinary request retirement (`hfs_retire`, e.g.
  from `hfs_call_reserve` on `-ENOSPC`/`-EAGAIN`) and `hfs_session_destroy`,
  at all 8 of its exits. The helper does `mutex_unlock`, then takes the
  session's view (`hfs_view_get`; no view, no waiter), increments
  `atomic_long_t view->retirement_releases`, wakes `view->wait` and puts
  the view.
- How a waiter sees the release: an observation reads the count before its
  trylock. A failed trylock means the holder had not unlocked yet, so had
  not counted yet, and its count changes the value the observation waits
  on. This holds only for holders that release through the helper and
  while the view is still `session->view`. A view leaving it is fenced
  first, which is a stop. The counter is atomic because the increments run
  outside the mutex.
- Review round 2 found that `hfs_retire` and `hfs_session_destroy` used a
  plain `mutex_unlock`. A waiter whose trylock lost against them slept to
  its 30 s deadline and returned `-EAGAIN`.
- Two guards against a future plain unlock: `view/run.py --pressure`
  refuses a source tree where any `mutex_unlock` of a `->retirement` mutex
  in `fs/9p` is outside `hfs_retirement_unlock`, and the wait slice limits
  a missed release to 200 ms. `hfs_view_retire_mode` also wakes
  `view->wait` at each floor advance.
- A round of this call that ran and itself returned `-EBUSY` (the hfs1.4
  in-use check, or the WARN path) is counted and paused again, as in re4.
  Only a host BLOCKED answer or such a round pauses, and each pauses once.
- Separate rate-limited lines: `observation VIEW_ACQUIRE BLOCKED by the
  host (attempt N); retiring, then retrying after M ms ...` (host answer),
  `observation VIEW_ACQUIRE BLOCKED (attempt N): local retirement mutex
  busy; waiting for its holder (up to M ms)` (local wait) and `observation
  re-check failed (VIEW_ACQUIRE BLOCKED for the target before a reinstall);
  acquiring again at once`. The give-up line counts the capped attempts.
- A REOPENED ACK that lands between the read and the capture costs at most
  one spare re-check ticket, within the cap. Without the host's BLOCKED
  reason (2h4), a capacity BLOCKED right after a recall is also re-checked
  once.

Tests (`view --pressure`, `--sanitize`). The fixture's busy retirement is
now another thread's rounds (`retire_busy`, `busy_start`), which hold the
real `session->retirement` mutex. The try's trylock fails while they run,
`busy_round_end` (as `view_wait_hook`, one per 100 ms wake-up) ends one,
and a blocking `hfs_view_retire` waits them out. Every round ends through
the actual `hfs_retirement_unlock` (`hfs_view.c` is compiled whole). The
rest of `hfs_view_retire_mode` (the exchanges, the `ran`
flag and the in-use `-EBUSY`) is still the host model (`retire_ran_busy`
plays the in-use case), so its wiring is compiled by the kernel build and
controlled_received and reviewed in the source, not executed here.
`hfs_retire` and `hfs_session_destroy` are not executed here either; the
source guard checks that they release through the helper. The
fixture asserts fewer than 1000 retirement calls per case, so a back-off
that loops on rounds fails instead of spinning.

- `retry_blocked_reinstalled`: R19-1's shape (`HOOK_REINSTALL_BLOCKED`: a
  recall reinstalls another target with its REOPENED ACK, the answer is
  BLOCKED). Retirement stays busy for 500 ms, yet the call succeeds with
  exactly 2 host tickets, no pause and no retirement try. A view reinstalled
  before every answer stops at 4 BLOCKED tickets.
- `retry_blocked_busy_capacity`: a capacity BLOCKED on an unchanged view
  still needs progress. Two busy rounds free nothing and the call's own try
  is refused, so it returns `-EAGAIN` "no retirement progress" after one
  acquisition and one pause, with the local-busy line apart from the host
  line and no re-check.
- `retry_blocked_busy_rounds`: 5 busy rounds, the fifth advancing a floor,
  end granted on the second acquisition after one pause (550 ms).
- `retry_blocked_busy_deadline`: a round that never ends returns `-EAGAIN`
  "deadline exhausted" at the 30 s deadline, after one acquisition. It
  makes one uncounted try per 200 ms slice (150 tries) and logs one
  local-busy line.
- Review round 1: `retire_unlock_order` checks, in the actual
  `hfs_retirement_unlock`, that a thread whose trylock fails just before
  the unlock (`test_mutex_unlocking` probe) read the old count and the count
  then changes. `retry_blocked_ran_busy`: a round that ran and returned
  `-EBUSY` pauses 3 times and stops at the retry limit, with no local-busy
  line. `retry_blocked_busy_stop`: the busy round ends while the transport
  fails, so the result is `-ESTALE` after one retirement try.
- Review round 2: `retry_blocked_foreign_holder`. The mutex is held by a
  holder that is not a view round, standing in for `hfs_retire`; it releases
  through `hfs_retirement_unlock` 100 ms in. The waiter wakes then (150 ms
  after the call's start, not at its slice end), and its own round frees
  the tickets. A holder that releases with a plain unlock is picked up at
  the slice end (250 ms), not at the deadline.
- Updated: the R16-1 held case (the recall its release starts runs during
  the third acquisition, which is now re-checked: 5 acquisitions, 2 pauses),
  and the worker case's busy try (waited for, 1 pause instead of 2). The old
  "busy mutex pauses three times" assertion became
  `retry_blocked_busy_deadline`.

Each of the first four new cases and both updated ones failed on the re4
`hfs_mmap.c` (run alone, with 1h4's `hfs_view.h` for the new field, before
review round 1 changed the fixture's signatures). Mutants each fail a case:
always re-check, never re-check, a re-check without the ticket cap, a held
mutex counted (re4), a held mutex as a give-up, a re-check on the ACK count
only, and from review round 1: the count incremented before the unlock, a
round that ran with `-EBUSY` treated as a held mutex (the round-0 loop), no
stop check after a wake, and no progress check after a wake. From review
round 2: the helper without the count (no wake for the waiter), no wait
slice, and a plain `mutex_unlock` in `hfs_retire`; the last is refused by
the source guard.

The series was regenerated with `export.py --task 1h4` (0004: `hfs_view.[ch]`,
`hfs_lease.[ch]`, `hfs_mmap.c`; 0003 since review round 2: `hfs_session.c`).
A fresh `prepare.py` tree applies it with
fuzz zero, and `fs/9p/`, `net/9p/` and `random.o` compile without warnings
under `kernel.py configure`. transport, policy, cursor, tag, mount_hello,
view and view --pressure, setattr (`--sanitize`), requests, huge, dirty and
controlled_received `terminal-smoke`, `cleanup` and `exit-cleanup` pass.
requests' and controlled_received's `compat.h` gain `atomic_long_t`.
requests compiles `hfs_retire` without `hfs_view.c`, so its `test.c` supplies
`hfs_retirement_unlock` and asserts that a retirement releases through it. reclaim
still fails at its pre-existing compile errors, as on the unchanged tree. No
candidate was rebuilt, booted or admitted.

## Guest-requested view advance (`xf4` step B, `4h4`)

zq3-r18 R18-1 and zq3-r19 R19-1: nearly every guest observation after the
guest's own mutation or pinning LOOKUP named a stale expected target, so the
host ran a full EXPECTED_TARGET recall (about 5-7 s scoped, 13-15 s
all-domain) and T1 ran out of its 240 s deadline. For a view with no
target-bound state that recall's only guest-side effect is "target :=
current cut, revision++". Design: `docs/design/xf4-view-advance.md`; the
host side is task 3h4, which grants a Targeted DOMAIN READ lease on its
current cut to a request carrying `"advance": true`.

Patch 0002 (codec policy):

- `hfs_view_advance_field`: the optional `advance` field of a VIEW_ACQUIRE
  payload. Absent means today's bytes; present it must be the literal `true`
  on a READ DOMAIN request with a CONTROLLED_LOCAL expected target, so
  `false`, `"true"` or an EXCLUSIVE_DIRTY/null-target request is `-EPROTO`
  (as guestbridge `validateAdvance`).
- `hfs_view_advance_compatible`: guestbridge `AdvanceCompatible`. Both
  targets are CONTROLLED_LOCAL with the same enrollment, profile and context
  hashes, journal incarnation, epoch and visibility generation; the granted
  position is not behind; an equal position needs the same journal hash.
- `hfs_mapped_acquire_original` (split out of `hfs_mapped_source`): a lease
  target that differs from the expected one is accepted only with advance
  and only when compatible; without advance it stays `-ESTALE`. A
  VIEW_QUERY reply has no original request here (and the bridge does not
  check its target either), so the kernel's own classification below is
  its only gate.

Patch 0003: `struct hfs_provider` gains `advance_target`. NULL means the
provider cannot verify an advance, and such a session never requests one.

Patch 0005: `hfs_received_advance` is the receipt gate of an advance
candidate. The token must be exactly the lease target of a verified SOURCE
reply that was not current at receipt (`owner->stale`, lease as the source,
so its target equals the receipt evidence's), on the result channel of this
session. The lease must be at the mount's current fence, the receipt must
have captured the LIVE view at its current revision, and the view must
still be LIVE and installed at exactly the request's expected target, which
differs from the granted one. It never makes the target current.
`hfs_controlled_advance_target` adds the enrollment metadata checks
(`hfs_controlled_target_metadata`).

Patch 0004:

- `hfs_view_capture_ex` returns a cacheless hint under `view->lock`
  (`hfs_view_cacheless_locked`): no cached page, mapping, dirty page or dirty
  package (DIRTY_RESERVE reservations), PAGE grant, admitted token, and no
  published observation. The last is stricter than the design's send
  condition: an advance with another observation published can never be
  adopted, so such a capture keeps today's request. `view->reservation` is
  the view's own memory credit, not a dirty reservation.
- `hfs_mmap_observe_lease` sends advance only when the capture was
  cacheless, `hfs_view_advance_supported` holds (hfs1.4 and
  `provider->advance_target`) and no earlier acquisition of the same
  `hfs_mmap_begin` refused one (`attempts->no_advance`). A view with mmap
  pages or mappings never sends it.
- `hfs_lease_acquire_ex` takes `advance` (only for READ DOMAIN with an
  expected target, else `-EINVAL`); `hfs_lease_acquire_payload` puts
  `"advance":true` right after `access`, and never sends `false`. The ticket
  records it (`ticket->advance`).
- `hfs_view_exchange_source`: an advance ticket's not-current GRANTED whose
  lease target differs from the bound payload's expected target
  (`hfs_view_grant_moved`) is handed out at once, without the 2f4 settle
  wait (which could only end in `-EPROTO` and a blocked session). If the
  bound payload cannot be parsed (memory), it is handed out with `-EXDEV`,
  built unadopted and released (review round 1: never dropped unreleased).
  Without advance such a reply still settles and fails closed.
- `hfs_lease_decode_target` (split out of `hfs_lease_decode`) classifies a
  moved lease target. Without advance it is `-ESTALE`, as before. With
  advance it is an advance candidate when `hfs_view_advance_target` verifies
  it (codec, compatibility, `provider->advance_target`):
  `lease->advance`. Failing that it is an ordinary verified lease when the
  view already holds exactly that target and the reply is current for it
  (`hfs_view_target`). That is a replay after the adoption, or annotation
  (b): a readmitted PENDING ticket whose recall reinstalled the view at the
  granted cut. The observation's stamp predates that move, so
  `hfs_mmap_publish` refuses it and it is held and released after the next
  acquisition, as ie4 does for any grant the view moved past. Otherwise it is
  built unadopted and `hfs_lease_acquire_ex` returns `-EXDEV`; nothing blocks.
- `hfs_view_advance` verifies the target natively again first, outside every
  view lock (the provider sleeps), as `hfs_mmap_install` verifies an event's
  target. It then takes `view->transitions`, so no STOP or install runs
  between re-check and switch, but bounded (`hfs_view_transitions_bounded`,
  review round 1): a view that left LIVE or is refused ends the wait at once
  with `-EXDEV` (a STOP sets RECALLING before it joins tokens without a
  deadline), and any other holder is waited for one killable jiffy at a time
  until the observation's deadline (`-ETIMEDOUT`) or a fatal signal
  (`-EINTR`); the lease is then unadopted and the cleanup packet releases
  it. Under `view->lock`,
  `hfs_view_advance_allowed` re-checks that the view is unrefused and LIVE at
  the capture's revision and generation, still holds the captured expected
  target, is cacheless with our observation not yet published, keeps the
  visibility generation, and that the lease is an unpublished candidate with
  the verified hash. Then the target switches, `revision++` (no fence or
  sequence moves), and the observation's stamp and target are re-stamped
  before `hfs_mmap_publish`. Any refusal marks the lease unadopted and returns
  `-EXDEV`: the observation holds it, acquires again without advance and then
  releases it. `hfs_mmap_publish` also refuses a lease still marked
  `advance`.
- Annotation (a): nothing is served under an unadopted or not yet adopted
  lease. It is never published, bound or page-leased, on the `-EXDEV` path
  too, and the view it leaves behind is the cacheless one it captured.

Tests (each fails on the pre-4h4 tree: the new API does not compile there,
and a mutation run of 13 guards killed every mutant):

- `view/test.c` `advance_checks` (actual `hfs_view.c`): the adoption
  (target switch, revision + 1, re-stamp, the old stamp refused at entry);
  the cacheless hint and the re-check refuse a cached page, a mapping, dirty
  pages, dirty packages, grants, an admitted token and a published
  observation; a moved revision, another generation, another expected
  target, another visibility generation, a refused native check, a hash
  mismatch, a non-candidate, RECALLING and FENCED are refused with the view
  untouched and the lease unadopted. Ordering against STOP: a STOP that
  completes during the native check refuses the adoption; a STOP that holds
  `transitions` first (RECALLING, joining an admitted token) makes the
  adoption return `-EXDEV` at once while the STOP still waits
  (`advance_stop_order_checks`). Another holder of `transitions` on a LIVE
  view ends the adoption at its deadline (`-ETIMEDOUT`, exactly 25 jiffies)
  or at a fatal signal (`-EINTR`) (`advance_bounded_checks`).
- `view --pressure` (actual `hfs_mmap.c` observation path, `hfs_view_exchange`
  and `hfs_lease_decode_target`): a cacheless view requests advance and
  adopts it in one acquisition (`retry_advance_adopted`); a readmitted grant
  after a recall to another target is unadopted, `-EXDEV`, then no advance;
  after a recall to the same target the stamp check refuses it; after a
  recall to the granted cut it is a current ordinary lease, held
  (`retry_advance_refused`). A mapped view, a mapped cached page and an
  unsupported provider never request advance (`retry_advance_never`; this is
  the mapped/ case of the design, which the receipt-only mapped/ harness
  does not compile). Exchange: a moved advance grant is handed out without
  settle (`xq_advance_moved`), an unreadable expected target hands it out
  with `-EXDEV` (`xq_advance_unreadable`), an advance ticket granted its expected target
  still settles, and a moved target without advance still blocks.
  `decode_checks`: the classification including a replay after the adoption
  (accepted) and after a further move (unadopted), and a candidate never
  published before adoption.
- `controlled_received/policy_cases.py`: 13 advance codec cases (3 accepted,
  10 refused).
- `controlled_received --scenario advance`
  (`TestControlledReceivedActualViewAdvance`, consumer `ADVANCE`,
  `ADVANCE_REPLAY`, `ADVANCE_RELEASE`, `ADVANCE_STALE`): with the real 0005
  provider and the xf4 host (task 3h4) after a CREATE moved the cut, the
  advance grant is classified and adopted, its VIEW_QUERY replay is an
  accepted ordinary lease, and after a release (no recall), a further CREATE
  and a second advance the first stored reply is unadopted (`-EXDEV`) with no
  block, while the same reply without advance is the `-ESTALE` that blocks.
  It needs 3h4's host, merged on main. `--scenario both` selects it too
  (its pattern is `^TestControlledReceivedActual`), but `both` currently
  fails on main for other scenarios (task bh4: ProviderProducer,
  SourceCorruption, FundedDirty and the 180 s run timeout), so run
  `--scenario advance` on its own.

Review round 1 (4h4) found no safety issue. It led to the bounded
`transitions` wait, the unreadable-payload release and the comment fix
described above. Two functions were split without a behaviour change:
`hfs_lease_acquire_ex` (now also `hfs_lease_acquire_valid` and
`hfs_lease_acquire_grant`) and `hfs_view_target_decode` (now also
`hfs_view_local_target` and `hfs_view_target_journal`).

## Cacheless means no mmapped inode, not no registered inode (`tj4`)

zq3-r20 (`docs/lab/results/20261122-zq3-r20/README.md`, "xf4's advance never
fired"): the guest never sent `"advance": true`. `hfs_view_cacheless_locked`
required `nr_mappings == 0` and an empty `view->mappings`, but
`hfs_inode_get` registers every inode of a bridge session there
(`hfs_view_register_inode`), so the mount root alone kept every live view
non-cacheless.

Patch 0003 (`hfs.h`): `struct hfs_inode` gains `view_mmapped`, set and
cleared under `view->lock`.

Patch 0004:

- `hfs_view.h`: `view->nr_mmapped` counts registered inodes with
  `view_mmapped`. `nr_mappings` and `view->mappings` stay the STOP/recall
  inventory (STOP zap, install invalidation, dirty mapping errors, retire and
  unmount settle), exactly as before.
- `hfs_view.c`: `hfs_view_register` (via `hfs_view_link_locked`) registers
  an inode and, for `hfs_view_mmap_inode`, marks it mmapped once, in the same
  critical section. `hfs_view_unregister_inode` (eviction, the only path that
  ends a registration) clears the mark and decrements `nr_mmapped` with
  `nr_mappings`. The mark is only set on a registered inode, so
  `nr_mmapped <= nr_mappings` and the count cannot be unbalanced. It is
  sticky until eviction; a munmap keeps it, which only withholds advance.
- `hfs_view_cacheless_locked` uses `!nr_mmapped` instead of the
  `nr_mappings`/`mappings` terms. `hfs_view_advance_allowed` re-checks it
  under `view->lock`, so it covers `nr_mmapped` too.
- `hfs_mmap.c`: `hfs_mmap_file` calls `hfs_view_mmap_inode` before it
  installs `hfs_mmap_ops`.

Safety: a registered, never-mmapped inode holds no state bound to the view
target. `read_iter`/`write_iter` go over the bridge (`hfs_io`, `hfs_file.c`);
`hfs_cache_aops` has no `read_folio`, and `hfs_file_fops` has no
`splice_read`, so no generic path fills its page cache. The only page-cache
insertion (`add_to_page_cache_lru` in the fault path), every PTE and every
PAGE grant (`hfs_view_page_grant`) come from `hfs_mmap_ops`, which only
`hfs_mmap_file` installs, after the mark. No generic path can put a page into
an hfs inode's page cache (no `read_folio`, readahead, splice,
`copy_file_range` or `direct_IO`), so the fault path is the only source of
cached pages. An mmap racing an adoption is serialised by
`view->lock`: either the mark is first, and the adoption's re-check refuses
(`-EXDEV`, lease unadopted), or the switch is first, and the new VMA's faults
are stamped with the new revision and target. A live VMA pins its inode, so
a mapped inode is evicted, and its mark cleared, only once no mapping remains.

Tests:

- `view/test.c` `advance_mapping_checks`: a registered, never-mmapped inode
  keeps the capture hint cacheless and is adopted (revision + 1); once
  mmapped (twice: counted once) both the hint and the adoption refuse;
  unregistering clears the mark and the count; an mmap of an unregistered
  inode registers and marks it. This replaces the old assertion that a
  registered inode means "not cacheless".
- `view --pressure`: the main inode now stays registered, never mmapped,
  throughout the advance cases (as the mount root does), and
  `retry_advance_adopted` sends advance and adopts it; `decode_checks` runs
  with it registered. `retry_advance_never` marks it mmapped
  (`hfs_view_mmap_inode`), which alone withholds advance, then evicts it.
- `view/run.py` `mmap_mark_guard`: `hfs_mmap_file` (not compiled by the
  harness) must call `hfs_view_mmap_inode` before installing `hfs_mmap_ops`,
  and `hfs_mmap.c` must not call `hfs_view_register_inode`.
- Mutants, each killed: the old `nr_mappings` term, no mmapped term, no clear
  at unregister, a double count of a repeated mmap, and `hfs_mmap_file`
  registering without the mark (refused by the guard).

The series was regenerated with `export.py --task tj4` (0003: `hfs.h`; 0004:
`hfs_view.[ch]`, `hfs_mmap.c`). No candidate was rebuilt, booted or admitted.

## Blocked sessions keep their drains and gap reconciliation (`dh4`)

s54 (2026-11-21) found `tests/controlled` failing by timing alone:
`consumer.c` expected the legacy control DRAIN_WRITE to end in the host's
`-EACCES` (unissued claim) and got `-EHOSTDOWN` (-112), and the `gap=true`
case's second `hfs_request_resolve_gap` failed the same way. The first
refusal (legacy ROOT, or the lost first GAP reply) blocks the session.
DRAIN_WRITE and GAP_RESOLVE are reserved control work, so a blocked session
still admits and sends them (`hfs_request_admit`, reserved cells), but since
`oc4` the FETCH AGAIN loop (`hfs_transport_stop`) and the CALL->QUERY poll of
`hfs_request_run` halted them with `-EHOSTDOWN` (`hfs_session_poll_halt`):
neither was in `hfs_after_block_operation`. Whether the reply came on the
first FETCH decided the result.

Decision: exempt exactly DRAIN_WRITE and GAP_RESOLVE; every other operation,
including ordinary CALL opcodes, ALLOCATE, QUERY, VIEW_QUERY, VIEW_WAIT and
DIRTY_FREEZE, still halts at its first retry or poll. This is what the
blocked-session rules above already promise: a lost or uncertain response
"blocks new ordinary work", while "reserved control work remains available
for reconciliation and drains"; a bound DRAIN_WRITE is an immutable byte
obligation (`hfs_request_bind_drain`, the minor >= 2 earmarked drain); and a lost GAP reply "retries that
original Call in another reserved control cell" on the blocked session
(`o04`). Admitting them and then killing them at the first poll kept
neither promise. The exemption covers both forms of DRAIN_WRITE: the legacy
opcode-classified control CALL and the minor >= 2 earmarked dirty drain
(`hfs_request_drain`), whose CALL and QUERY polls pass the opcode. The
byte-obligation argument covers only the earmarked drain; the exemption is
also safe for the legacy control DRAIN_WRITE because exempting it reissues
nothing (it only lets the one admitted exchange finish polling), and it has
no kernel caller apart from the test (`tests/controlled`). Exempt traffic is
still bounded: it runs to its reply or its 30 s deadline, and
`transport_failed` (host gone: a deadline or ERROR UNAVAILABLE) halts it at
its first retry like every exempt exchange (`zq3-r13`). An exempt
DRAIN_WRITE that reaches its 30 s deadline on a stalled exporter therefore
sets `transport_failed`, and later retirement traffic on that session halts
at its first retry, as on a non-blocked session and as before `oc4`.

The QUERY that `hfs_controlled_resolve_gap` (0005,
`hfs_controlled_lifetime.inc`) sends right after GAP_RESOLVE serves that
GAP_RESOLVE, so it inherits its exemption at the call site
(`hfs_control_flagged(..., "QUERY", ..., true,
hfs_after_block_operation("GAP_RESOLVE"), ...)`), the way other polls
inherit theirs. QUERY is not exempt by name. Without it the QUERY still
halted at its first FETCH AGAIN on a blocked session outside teardown, then
blocked the session again and kept another uncertain candidate; `hfs_retire`
reaches it outside teardown (from `hfs_call_reserve`'s exhaustion path and from
`hfs_mmap.c`).

- Patch 0002 (`hfs_policy.c`): `hfs_after_block_operation` lists
  DRAIN_WRITE and GAP_RESOLVE; the `hfs_policy.h` comment says why.
- Patch 0005 (`hfs_controlled_lifetime.inc`): the gap QUERY passes the
  GAP_RESOLVE exemption.
- Patch 0003 (`hfs_session.c`): the `hfs_session_poll_halt` comment names
  the new exemptions. `hfs_terminal_candidate` called `hfs_controlled_owns`
  outside `#ifdef CONFIG_9P_FS_HOMELABFS_CONTROLLED`, but
  `hfs_controlled.h` declares it only under that option, so a build with
  the option off failed (implicit declaration, since `614` (e199627)). The check moved
  into `hfs_terminal_controlled_mismatch`, which is false without the
  option (no session is controlled then). A header stub was not used: the
  controlled fixtures define their own `hfs_controlled_owns`.

Tests:

- `transport`: `hfs_after_block_operation` accepts DRAIN_WRITE and
  GAP_RESOLVE and still refuses ROOT, WRITE and ALLOCATE.
- `controlled`: all six cases (gap=false, gap=true, legacy0/1 and their
  control variants) pass on five consecutive runs; before, every case but
  gap=false failed with -112.
- `controlled_received/build.py` extracts the new helper with
  `hfs_terminal_candidate`.
- `controlled_received gap` (`TestControlledReceivedActualGapLostReplies`):
  after the lost GAP reply has blocked the session, the new consumer action
  `AGAIN GAP_RESOLVE,QUERY 3` answers three local AGAINs to the FETCHes of
  the retried GAP_RESOLVE and of its QUERY. `RESOLVE_GAP` must succeed,
  `AGAIN_CHECK` sees all six served with the session still blocked and not
  `transport_failed`, and the unknown-candidate count stays 2.
- `fs/9p`, `net/9p` and `random.o` build with 0 warnings with
  `CONFIG_9P_FS_HOMELABFS_CONTROLLED` on and off.

The series was regenerated with `export.py --task dh4` (0002:
`hfs_policy.[ch]`; 0003: `hfs_session.c`; 0005:
`hfs_controlled_lifetime.inc`). No candidate was rebuilt, booted
or admitted.

## Touch, advance error mapping and the mmapped-mark guards (`tg4`, `ch4`, `wj4`)

Three small follow-ups, done together (worktree `kn1`).

`tg4` (vf4 round-2 review), patch 0003 `hfs_inode.c`:

- `hfs_setattr_supported` accepts `ATTR_TOUCH`. `utimensat(NULL)`,
  `futimens(fd, NULL)` and a pair of `UTIME_NOW` (plain `touch`) all reach
  `vfs_utimes` as `ATIME|MTIME|CTIME|TOUCH`, which got `EOPNOTSUPP` before any
  CALL. `ATTR_TOUCH` is only a VFS permission hint, already enforced by
  `may_setattr` in `notify_change` (owner, or write permission). The request
  is sent as host-current atime and mtime (`atime_now`/`mtime_now`).
- The `hfs_relookup` comment now names the two outcomes besides a positive
  dentry. A directory alias makes `hfs_create_exists`' O_CREAT open fail with
  `EISDIR`, and `hfs_link` returns `ESTALE` and drops the alias. A negative
  race (the name is gone again) makes `atomic_open` fail with `ENOENT`, and
  `hfs_link` returns `ESTALE`.
- `hfs_atomic_open` moved below `hfs_create`, so the forward declaration is
  gone. The missing blank lines after `hfs_create` and `hfs_namespace` are
  back.
- `tests/setattr`: `model.inc` adds `vfs_utimes` (fs/utimes.c), and
  `test_touch` covers NULL times, both `UTIME_NOW` (`ATTR_TOUCH`, host-now
  times) and an explicit pair (unchanged). On the pre-tg4 source the NULL
  case fails (`vfs_utimes(&f, NULL) == 0`).

`ch4` (4h4 round-2 review), patch 0004 `hfs_view.c`:

- `hfs_view_advance` passes `-ETIMEDOUT`/`-EINTR` through only from
  `hfs_view_transitions_bounded`. Any failure of the native target check is
  a refusal (`-EXDEV`), even an `-ETIMEDOUT` or `-EINTR` from the provider.
  The lease is unadopted either way.
- The `hfs_view_transitions_bounded` comment lists the actual LIVE-phase
  holders of `view->transitions`. Each one holds it only for a check under
  `view->lock`: the REOPENED ACK check (`hfs_controlled_before_ack`, which
  also takes `hfs_controlled_lock`), a STOP until it sets RECALLING, a seal,
  `finish_drained` or install that refuses LIVE, the initial bind of a LIVE
  view, an exact REOPEN replay, and a concurrent adoption's switch.
- `view/test.c` `advance_release_checks`:
  - a holder releases `transitions` after 7 jiffies and the adoption then
    switches the target (revision + 1);
  - the same with jiffies wrapping during the wait;
  - a holder that never releases, with the deadline past a jiffies wrap,
    ends with `-ETIMEDOUT` exactly 25 jiffies later;
  - the provider check returning `-ETIMEDOUT` or `-EINTR` gives `-EXDEV`.
    On the pre-ch4 source this case fails.
- `view --pressure` `xq_advance_unreadable_acquire` runs the unreadable
  expected target through the actual `hfs_lease_acquire_ex`. The harness now
  compiles `hfs_lease.c` from `hfs_json_hash_fields` up to
  `hfs_lease_acquire_ex`, renamed `actual_hfs_lease_acquire_ex` because the
  observation cases keep the host-model fixture. It also compiles the actual
  `hfs_cap_write`. `hfs_lease_own` and the ticket prepare/activate are
  fixtures. The host answers a complete GRANTED lease (its id is the actual
  `hfs_json_hash_fields`) on another target, and the bound payload is
  truncated after binding. Result: `-EXDEV` with the lease decoded,
  unadopted, not a candidate and not verified by the provider, owned for
  release and holding the terminal ticket; no settle and no block. A mutant
  that does not pass `unadopted` to `hfs_lease_decode` is killed. The
  compat `struct hfs_cap` gains `json`/`rights` and `struct hfs_session`
  gains `domain`/`enrollment.context`.
- README nits: the `controlled_received/README.md` advance paragraph is
  rewrapped. The 4h4 section no longer says `--scenario both` simply
  includes the advance scenario: `both` fails on main (bh4). The review-round
  sentence is reworded.

`wj4` (tj4 review follow-ups), partly done:

- (2) `hfs_view_release` also warns on `view->nr_mmapped` (a tripwire:
  `nr_mmapped <= nr_mappings` and both drop together on unregister).
- (3) `view/run.py` `mmap_mark_guard` now runs with and without
  `--pressure`. It requires:
  - in `hfs_mmap_file`, the `if (!err) {` that directly follows the
    `hfs_view_mmap_inode(inode)` result, with `hfs_mmap_ops` installed once
    and only inside that block;
  - exactly one `vm_ops = &hfs_mmap_ops` in `fs/9p`, with `hfs_mmap_ops`
    static to `hfs_mmap.c`;
  - `hfs_file_fops`' `.mmap` is `hfs_mmap`, which sends a bridge session to
    `hfs_mmap_file`.

  Each of these mutants is refused: ops before the mark, ops unconditional,
  no mark, an extra install in `hfs_file.c`, a route to `generic_file_mmap`,
  another `.mmap`, and registration without the mark.
- (4) `hfs_inode_layout_guard` (both modes) compares the members of
  `tests/view/compat.h`'s hand-copied `struct hfs_inode` with
  `fs/9p/hfs.h`, by name and order. A swapped pair is refused.
- (1) Clearing the mark when the last VMA closes and no pages remain is
  **deferred**. It stands on two points:
  - A new lock order. A per-inode VMA count means adding `.open` and
    `.close` to `hfs_mmap_ops`, and `->close` runs under `mmap_write_lock`.
    To reach the view it would take `session->lock` (`hfs_view_get`) and
    then `view->lock`, so `mmap_lock -> session->lock` would be new; all 50
    `session->lock` sites would first need an audit showing that none
    touches user memory or takes `mmap_lock`. Pinning the view from the VMA
    instead changes view lifetime across retire and unmount.
  - The VMA lifecycle is untestable here. The count is only as good as the
    pairing of `->open` and `->close` on every duplication and removal path
    (`dup_mmap`, `__split_vma` and its failure path, `copy_vma`,
    `remove_vma`). Once `->close` exists, `vma_merge` refuses such
    neighbours, so the merge paths that free a VMA without `->close`
    (including `__mmap_region`'s post-`->mmap` merge) do not apply. But the
    harness compiles neither `hfs_mmap_file` nor any VMA lifecycle, and no
    guest kernel was booted, so none of this can be exercised.

  The sticky mark only withholds advance (it never permits a wrong one), so
  leaving it is the safe default. wj4 stays open for (1).

The series was regenerated with `export.py --task kn1` (0003:
`hfs_inode.c`; 0004: `hfs_view.c`). No candidate was rebuilt, booted or
admitted.

## Split view/lease functions and stop wake-ups (`5f4`)

Follow-ups from the 2f4 and 1h4 reviews (worktree `5f4`).

The comments on `hfs_lease_acquire_ex`, `hfs_lease_acquire_grant`,
`lease->unadopted` (`hfs_lease.h`) and the two mmap callers name one more
case of an error with `*out` set. `hfs_view_exchange` can copy a GRANTED into
the caller's response and then fail to keep it as the ticket's Current
(`-ENOMEM`; the ticket is then not terminal). `hfs_lease_acquire_grant`
already built that lease unadopted for release; only the comments missed it.

Functions split per the 50-line rule, without behaviour changes (same lock
order, error paths, unlocks and frees; the original control flow is kept
call for call):

| Before | After (lines) |
| --- | --- |
| `hfs_view_exchange` (174) | `hfs_view_exchange` (38), `hfs_view_round_query` (7), `hfs_view_exchange_bind` (39), `hfs_view_exchange_send` (34), `hfs_view_terminal_status` (8), `hfs_view_exchange_terminal` (35), `hfs_view_exchange_pending` (20), `hfs_view_exchange_poll` (22), `hfs_view_exchange_round` (33); state in `struct hfs_view_round` |
| `hfs_lease_acquire_ex` (49; 105 before ch4) | `hfs_lease_acquire_ex` (42), `hfs_lease_acquire_request` (19) |
| `hfs_mmap_bootstrap` (83) | `hfs_mmap_bootstrap` (38), `hfs_mmap_bootstrap_packet` (26), `hfs_mmap_bootstrap_bind` (41) |
| `hfs_view_retire_mode` (208, the 1h4 round-2 note) | `hfs_view_retire_mode` (38), `hfs_view_retire_families` (11), `hfs_view_retire_payload` (35), `hfs_view_retire_send` (14), `hfs_view_retire_verify` (24), `hfs_view_retire_busy` (14), `hfs_view_retire_take` (21), `hfs_view_retire_free` (20), `hfs_view_retire_advance` (27), `hfs_view_retire_family` (44); state in `struct hfs_retire_round` |

`view/run.py --pressure` keeps its ranges: the exchange helpers sit between
`struct hfs_grant_settle` and `hfs_lease_own`, and
`hfs_lease_acquire_request` between the `hfs_lease_acquire_ex` arguments and
`hfs_lease_acquire`, so both extractions compile them unchanged.

Stop wake-ups (patches 0003 and 0004):

- `hfs_view_reopen_wait` sleeps on `view->wait` until a REOPENED ACK, a
  refused view (`hfs_view_refused`: FENCED, or a closing or blocked hfs1.4
  session) or a failed transport. Only the ACK and the view's own stop and
  fence paths woke `view->wait`. A session block, a close or a failed
  transport is set by code that has no view, so the settle wait slept on to
  the exchange's 30 s deadline.
- `hfs_session_block` cannot take the view itself: it runs under
  `session->lock` (`hfs_view_retire_take`'s WARN path), and `hfs_view_get`
  takes that mutex.
- `struct hfs_session` gains `stop_wait`. Every write of `blocked`,
  `closing` or `transport_failed` is followed by `hfs_session_stop_wake`:
  `hfs_session_mark_blocked` (`hfs_session_block`, the silent
  `hfs_session_fence`, `hfs_mmap_fail`, the close/release sequence guards),
  `hfs_transport_failed` (all four transport sites), and explicit wakes in
  `hfs_mount_fail` and kill_sb. `wake_up_all` is safe under any lock but
  the queue's own.
- `hfs_view_watch_stops` adds a wait entry for the current task to
  `session->stop_wait` around the `wait_event_killable_timeout` on
  `view->wait`. A stop wakes the task; the `wait_event` loop re-checks its
  condition and returns. The entry is removed before the waiter returns.
  `hfs_view_retire_wait` and `hfs_view_enter_until` have the same stops in
  their conditions and now watch the queue too (the 1h4 section said a
  block did not wake the retire wait).
- Conditions checked: the reopen wait's ACK count is woken by
  `hfs_view_reopen_acked`; FENCED by the view's fence path on `view->wait`;
  blocked, closing and transport_failed by `stop_wait`. The settle look
  (`hfs_view_grant_look`) stops on exactly those (refused, then the poll
  halt), so a wake-up cannot spin: a block during teardown is still a
  refusal.

Tests (`view`, `view --pressure`, with and without `--sanitize`):

- `view/compat.h` counts wake-ups per queue and models watched queues
  (`add_wait_queue`). With `view_wait_wakes` set, a simulated sleep that
  no hook woke runs to its timeout, as in the kernel. Without it, each
  step re-checks the condition, as before.
- `xq_stop_wakes`: an admitted VIEW_ACQUIRE gets a GRANTED at fence 6
  while the mount is at 5 and parks in the settle wait. During the first
  sleep another path blocks the session through the actual
  `hfs_session_block` (compiled from `hfs_session.c` as
  `actual_hfs_session_block`, with `hfs_session_mark_blocked` and
  `hfs_session_stop_wake`). The wait ends after that one 10-jiffy step, and
  the GRANTED is handed out with `-EHOSTDOWN`, the ticket terminal, no block
  by the exchange and no watch left behind. A failed transport wakes it
  the same way. A silent write of `blocked` (no wake-up) sleeps to the
  30 s deadline, so the model catches a missing wake-up. A blocked session
  also wakes a parked `hfs_view_enter_until` (`-ESTALE`) and
  `hfs_view_retire_wait`.
- Mutants killed: a `hfs_view_watch_stops` that adds no entry fails
  `xq_stop_wake_case` (`jiffies-start==10`). A `hfs_session_block` that only
  writes `blocked` is refused by the new `stop_wake_guard`, which runs in
  both modes: every write of true to `session->blocked`, `->closing` or
  `->transport_failed` in `fs/9p` must be followed by
  `hfs_session_stop_wake` within four lines.

Other harnesses that compile slices of the changed files follow the new
helpers:

- `transport` starts its slice at `hfs_transport_failed`, and a counting
  `hfs_session_stop_wake` fixture checks that every exchange that set
  `transport_failed` woke the queue.
- `mount_hello` compiles the actual `hfs_session_stop_wake` and
  `hfs_session_mark_blocked`. Its wait queues count wake-ups: every
  `hfs_session_block` (logged or not) and kill_sb wake `stop_wait`.
- `controlled_received` selects the new `hfs_session.c` helpers. Its
  `release_prepare.inc` prefix of `hfs_view_exchange` now ends before the
  first round (`if (ticket->admitted)`). It initializes `stop_wait` and has
  inert `add_wait_queue`/`remove_wait_queue`, since its waits poll.
- `controlled` compiles `hfs_transport_failed` (inside its
  `hfs_reply_scope` span) and gains an `hfs_session_stop_wake` fixture,
  which its `hfs_session_block` fixture also calls.

`hfs_view_query` (60 lines) and other long functions outside this task's
list are unchanged.

The series was regenerated with `export.py --task 5f4` (0003: `hfs.h`,
`hfs_session.c`, `hfs_file.c`; 0004: `hfs_lease.h`, `hfs_lease.c`,
`hfs_view.c`, `hfs_mmap.c`). No candidate was rebuilt, booted or admitted.
