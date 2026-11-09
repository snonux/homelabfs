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
`version=9P2000.L.hfs1.1,trans=virtio,msize=65536` selection uses separate inode/file/super operations inside the 9p filesystem. The network
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
copies only the mount's `fail:` tail, so it could not see this.

```sh
python3 tools/integration/linux9p/tests/mount_hello/run.py \
  --source "$SOURCE" --output "$RUN/mount-hello" --sanitize
```

It compiles the complete production `v9fs_hfs_mount`, the enrollment
validators and `hfs_controlled_before_send` verbatim. Kernel/VFS, p9 client,
transport binding and controlled registry lookups are substituted; the native
exchange echoes the sent HELLO as the runtime does. Cases: features 16383 and
32767 mount; a default echo for 32767 and a terminal echo for 16383 refuse with
EPROTO after one HELLO; an unselected mask refuses before any HELLO or client.
Each case runs in its own child. The pre-r54 source fails the 32767 cases and
crashes on the unselected mask.

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
