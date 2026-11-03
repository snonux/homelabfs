# Retained mapped cache: kernel source contract

This is a vz3 component candidate awaiting root review and coupled integration.
The older development1 patch/object receipts predate the final reservation
integration. The maintained final candidate includes the complete owned paths.
No installed kernel, enabled provider/profile or syscall acceptance is claimed.
The real platform acceptance remains xz3/zq3; helpers exercise production C with
primitive substitutes and cannot establish actual page-table or source facts.

## Description authority and inherited aliases

A mapped page's immutable provenance is the original authorized
mapping/Description/page grant. It is not the identity of the CPU thread that
last executed a store. Existing DESCRIPTION authority survives chmod/unlink,
dup/fork/descriptor transfer and legal inherited mapping aliases until recall or
generation/session revocation. CLONE_VM and later credential changes do not
retroactively replace that original grant. Delayed writes retain its exact full
Credentials, Description, Context, Reservation and ClaimNumber; a writeback
worker never supplies a replacement credential or an unrelated grant.

This follows the selected contract in docs/spec/Retained-9p-protocol.md
DESCRIPTION paragraphs (lines 399–403 on the inspected main source) and
Filesystem-service.md's handle permission contract (215–222), implemented by
internal/fsservice/permissions.go's Handle branch for Read/Write/Truncate.
Pinned Linux6.1.190 mm/mmap.c authorizes shared writable mappings from
file->f_mode (1359 onward) and retains VM_MAYWRITE accordingly. Fork retains the
same file reference; shared PTE copying does not reauthorize each CPU store.
No commit_creds/per-thread revocation hook or shared-mm restriction belongs to
this selected contract.

A new path authorization still checks the current logical path/cut and fresh
full Credentials. A new authorization RPC/lease uses its newly captured full
Credentials and ticket family; it cannot mutate an old reservation or revive an
old PATH capability after denial. Legal inheritance does not permit substituting
a different Description/grant. Physical STOP covers all mapping aliases,
including fork, shared-mm, executable and open-unlinked mappings. Private
anonymous COW bytes are application snapshots and survive file-cache recall.

A proposed clarification of the ambiguous j04 credential sentence is staged in
the owned run root as proposal-j04-inherited-mapping.patch. Shared protocol/docs
are left for root to integrate after k04.

## Physical and wire boundaries

Final PTE admission checks the actual view phase, installed target and captured
local cache revision after parked RPC work. Neither lease acquisition nor
reservation issuance is a writable PTE. Before a writable PTE, the page owns an
immutable protected reservation, an ordinary earmarked number, original full
credentials and preowned freeze/drain/release resources. The body hash is bound
only after frozen bytes and DirtyEntry id are known. All aliases remain under
that one original obligation until its actual drain/recall resolves.

STOP closes local admission before joining final-install/inflight cookies,
revokes file-backed PTEs through unmap_mapping_range(even_cows=0), and checks
mapping/PTE/TLB/MMU completion and writeback/pin hazards. A bare fence does not
prove quiescence. Only after these checks and immutable freeze can the complete
adopted inventory become the STOPPED manifest. The authenticated ACK contract
excludes adoption of every old-epoch late reservation, including host-issued
replies not yet received locally. After reopen, incremented local revision
rejects those late replies even if visibility UUID did not change.

The registered cache aops use the pinned Linux noop_dirty_folio callback. The
real set_page_dirty/folio_mark_dirty path atomically tracks the page dirty bit
without generic writeback tags, accounting, credentials or RPC. Shared writable
PTEs already own adopted immutable provenance before installation. Dirty-PTE
removal during RECALLING can mark the bit again, so this callback has no LIVE
phase gate and takes no sleeping lock. Freeze and reserved drains retain the
original obligation; generic writeback never becomes its authority.

Dirty capacity is independently sixteen pages; clean metadata capacity is128.
Per-lease RELEASE and per-dirty FREEZE have independently preowned control
resources and finite counter slots before cache rights. Tickets reserve capacity
without numbers until activation; activation order is retirement order. An
activated uncertain ticket is never skipped. Pressure uses the original
preowned VIEW_RELEASE outside page/PTE/physical/transition locks to start recall
while acquisition capacity is blocked; the independent recall path must advance
STOP/freeze/drain/install/reopen. This includes ACTIVE clean READ leases and
active renewed siblings of the released lineage: the host protects PENDING and
initiates recall before asking a physical verifier for terminal release. A
129th clean-cache page and a read-first shared-write fault can therefore use
already funded release control while acquisition is blocked. The write fault
unlocks its original page before release and retries after stopped invalidation;
a fresh EXCLUSIVE_DIRTY grant and reservation still precede its writable PTE.
RELEASED requires actual physical/source closure.
No temporary pressure lease may be exposed as cache authority.

Frozen DRAIN_WRITE uses its original prepared ordinary request. A terminal short
or error result retains the exact bytes/provenance and mapping error; it cannot
produce DRAINED or a replacement claim. Successful protection means origin
journal durability plus owner RAM, without owner-disk/backing-apply waiting.
Intermediate installations happen while stopped and independently of the parked
drain caller. Final target/source verification, ordered installation and exact
reopen acknowledgements remain required before new observations.

## Independent stopped installation and drain completion

The event executor acknowledges INSTALLED_DRAIN after verifying and installing
that exact source target while physically stopped. It does not wait for the
ordinary drain caller, whose protected result can itself require that installed
cut. Each captured and successfully FROZEN obligation retains its immutable
bytes, Claim, Reservation and preowned request resources in RAM even when its
old page is detached. This is the only unresolved-dirty case accepted by the
owned launder/release callbacks; it is not generic writeback. The independent
drain executor obtains exact full-length terminal results in original ordinary
ClaimNumber order. A separate event-queue continuation emits DRAINED only after
all those results and physical cache detachment, using the latest intermediate
event and the unchanged complete manifest. Further intermediate installations
can progress while those results are parked. Final installation requires
DRAINED; reopen requires FINAL, no adopted dirty obligations and no attached
old file-cache pages.

An evicted/open-unlinked inode does not make a dirty obligation disappear. The
retained page registry is checked separately from the inode registry; an orphan
page can detach only with the same actual stopped, unmapped, nonwriteback,
unpinned and frozen conditions. Error reporting obtains a temporary matching
registered inode reference when one still exists, rather than pinning an inode
forever through page-private metadata. Private anonymous COW uses a lifetime-only
cookie and survives even a fenced backing view; it supplies no backing-cache
physical authority and does not prolong STOP's active-token count.

## Owned generic hooks and error exits

The exact export inventory is in export.py and source.json. Generic MM changes
are limited to include/linux/mm.h, include/linux/page-flags.h, mm/memory.c,
mm/gup.c, mm/mremap.c and mm/huge_memory.c. The observer fields default to absent
for every other filesystem; the selected retained profile is x86_64/64BIT,
4096-byte order-zero pages.

* finish_fault allocates page-table resources before entering the observer,
  then brackets its final PTE lock/revalidation/install. Admission failure returns
  NOPAGE; changed-PTE and success branches both release the cookie after unlock.
  An opted-in compound page cannot take the huge-PMD shortcut.
* finish_mkwrite_fault brackets the actual shared writable-PTE path. Both its
  changed-PTE return and successful reuse finish through the wrapper's end.
* wp_page_copy allocates, copies and charges before physical admission. Its
  pre-admission allocation/poison exits own no cookie; admission failure frees
  both temporary references. Once admitted, both PTE branches and notifier
  cleanup reach one end callback. It requests backing read authority, preserving
  legally copied private anonymous data instead of reserving shared dirty data.
* copy_page_range brackets the actual fork copy, including early/error returns
  from its renamed internal body. The wrapper restores its NOFS scope and ends
  exactly once. kernel/fork.c drops mapping i_mmap write lock before this call.
* move_page_tables brackets external moves. move_vma holds one bracket across
  forward move and any rollback; it never reacquires authority to recover a
  partial move. A failed admission moves zero bytes; post-admission error and
  success restore NOFS and end before VMA unmap/accounting cleanup.
* The common hugepage_vma_check refuses observer VMAs before executable-file,
  anonymous, MADV_HUGEPAGE and collapse decisions. VM_NOHUGEPAGE is an additional
  hint, not the exclusion mechanism. The focused helper compiles this actual
  common check; it does not execute MADV_COLLAPSE on a running kernel.
* Slow GUP refuses no_gup VMAs and try_grab_page refuses the dedicated page flag.
  Fast FOLL_GET/FOLL_PIN test the page-owned flag after a speculative physical
  reference; no lockless mapping dereference is introduced. Every owned file
  page gets the marker before attachment/PTE exposure. Migration refuses it.
  Invalidation retains the marker until actual mapping/private detachment and
  free_folio, after PTE removal. A speculative fast-GUP still must revalidate its
  original PTE; clearing after detachment cannot validate a removed PTE, and its
  physical reference prevents reuse meanwhile. Unsupported bypasses remain an
  activation gate requiring installed-kernel/platform evidence.

vm_cache_begin snapshots the end function before invoking a potentially parked
begin callback. End never dereferences a later-destroyed VMA or vm_ops. Owned
callbacks hold actual view and module references through that end. The callback
contract disallows sleeping when a page is supplied; page-less fork/move entry
may wait before admission, with an independent view/module reference already
held. No RPC or barrier wait occurs inside an admitted physical bracket.

## Locks, independent lifetimes and retirement

Session lock protects the view owner pointer; get takes its kref/module lifetime
before releasing the lock, including parked entrants. The view spinlock protects
phase, local revision, target, registered inode/cache/observation inventories and
active admission. transitions serializes STOP, seal, install, final reopen and
retained preallocated inode/page workspaces; fence only closes admission under
the spinlock and cannot reuse those workspaces. STOP closes LIVE before waiting
for active entrants, snapshots referenced objects, then drops the spinlock before
PTE/TLB/MMU invalidation and page locking. Its only inode nesting is view lock to
igrab's inode lock. Eviction unregisters outside inode lock before inode memory
can be freed; temporary iput occurs after dropping view lock.

Dirty adoption takes view lock then the exact reservation ticket admission lock;
cancel closes that ticket under its admission lock before making a parked RPC,
without nesting a reverse view lock. Captured epoch/target checks and actual
locked-page/dirty-slot ownership precede publication. Both delayed reserve
arrival during STOP and arrival after same-generation reopen are rejected. The
native authenticated STOPPED statement must verify this exact admitted source
contract and the complete host ledger/omitted IDs; no arbitrary callback success
or omitted-list inference proves no effect.

Control owners, parked pressure callers, cleanup packets and retained view
cookies have independent references. Whole-session retirement first prevents new
control generation and joins workers while preserving complete event/ACK/unknown
request evidence. It then rechecks outstanding requests before the mandatory
native retirement proof. Unknown/failed retirement stays fenced and retained.
Only positive whole-session retirement plus empty physical/cache/source ownership
permits controller and session destruction. Canonical result JSON returned to a
caller pins its actual ordinary request until JSON free, so concurrent verified
RETIRE cannot free a caller's borrowed result. Held dirty records independently
prevent ordinary retirement.

## Verification and remaining integration gates

Maintained helpers compile actual owned production C at primitive boundaries:
view covers physical admission/lifetime/revision/description/cancel/detach and
the actual Linux set_page_dirty/folio_mark_dirty/registered aops chain;
view --pressure additionally compiles actual mmap pressure/mkwrite, stopped
installation and drain-result validation, covering128-clean capacity/129th retry,
read-first upgrade, source denial and short/error results never reaching DRAINED;
dirty covers freeze, immutable claim construction, deferred activation and
PENDING/QUERY lineage; requests covers returned-result pins and protected floor
retirement; root consumes an actual production-host ROOT literal; transport and
codec retain response3 and any-chunk capacity regressions; huge covers the
actual common THP refusal. dirty/host_codec.py feeds the C-emitted DIRTY_FREEZE
request and corresponding response through the real Go closed codec, using an
owned test overlay without changing host source. These checks supply no native
provider, actual page tables, source authority or syscall acceptance.

The development1 exported snapshot was compiled before the final integration.
Later syntax-only checks used its immutable generated headers/recorded compiler
flags. The final candidate now has complete prepared-source verification before
and after successful owned-object compilation, including mm/huge_memory.o with
THP/SHMEM/read-only filesystem THP enabled. The earlier disabled-THP configuration
failure and all reference artifacts remain preserved. Independent root review and
actual platform/syscall acceptance remain required. Existing normal public source
proofs remain unavailable; controlled local targets stay separately typed with
null Publication and mandatory actual journal/state evidence.

Two concrete host progress gaps were confirmed against the earlier host lane:
the Transport worker drops Execute WouldBlock/NoSpace without rescheduling the
accepted original cell; and an ordinary CALL paused before planning remains
RESERVED/Plan=nil because QUERY/repeated CALL only enter outcome recovery. The
physical component cannot repair these by fabricating a result or dropping an
uncertain original. Root has assigned exact-original resumable transport/ordinary admission fixes
to the separate host lane; its reviewed integration is still required before
end-to-end acceptance. Capability/FORGET lifetime work remains
in the separately scoped l04 lane.

The final self-review identified per-capture credential revision churn that
could exhaust the 4096 historical view-family ceiling under unchanged sequential
operations. The correction compares the complete mapped fsuid/fsgid, normalized
full groups, namespace UUID and idmap hash under the process lock. Unchanged
snapshots reuse their epoch; observed changes increment it, including A→B→A
transitions. Concurrent equal captures share one revision, while differing
captures linearize under that lock. Historical returned snapshots, grants,
tickets, counters and floors are never rewritten or reset. A per-process
reservation accounts its cached numeric snapshot and bounded native reference
allowance; unchanged captures need no second standing cache admission. New cache
installation swaps under the spinlock and releases displaced native references
outside it. The maintained capture helper runs actual production capture and
family preparation for4100 equal captures, change/reversion/error cases and
concurrent equal/mixed captures with explicit primitive credential/map/pid
fixtures. It supplies no running-kernel identity or actual namespace proof.
Final2 objects remain evidence for the prior candidate. The re-exported stable
capture source passed final3 complete source pre/post-verification and all
requested object compilation, with earlier reference files unchanged. Root
independent review and actual kernel/platform acceptance remain required.

The R1 dirty-aops and ACTIVE clean-release corrections have current production-C
marking/pressure/mkwrite/drain checks and real host framed recall regressions,
including128 clean grants with a blocked129th request. Focused race/static checks
pass. Final3 is preserved prior-source object evidence. Corrected final4 passed
all requested objects and complete source/reference verification; narrow root R2
is clean. The maintained final4 receipts retain source/config/compiler/object
hashes and sampled resource limits. Runtime kernel/platform acceptance remains
a separate gate.
