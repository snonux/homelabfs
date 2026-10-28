# Handles and local advisory locks

Task `rq3` extends the local service from `pq3` and permission policy from `qq3`.
The code and tests establish a bounded service contract on disposable local
roots. Native FreeBSD FUSE P01/P03, Linux adapters, duplicate mounts and virtio-9p
remain separate acceptance gates; this implementation provides no kernel or
transport compatibility evidence.

## Stable object lifetime

A handle names an exact object allocation, open access mode and serving
generation. Unlink, final hard-link removal and rename replacement never retarget
it. Data stays readable and writable through existing descriptors until final
close; chmod does not revoke descriptor rights. Lookup references and open
references have separate counts. Forget never closes handles, and final open
release never drains outstanding kernel lookup references. Both conservatively
share a protected home HANDLE_PIN. The final combined drain protects a
HANDLE_RELEASE before any reclamation can rely on it. Allocation tombstones and
name revisions survive payload deletion and prevent identity reuse.

`Reap` requests a serialized sweep. Namespace removal/replacement and lifetime
drains also sweep when `Config.Reclamation` supplies a verifier. Only zero-link
objects with empty reference ledgers and no local lookup/open obligations are
candidates. The root is never a candidate. With no verifier, or a verifier
returning Busy/WouldBlock, payloads and pins remain retained. Restart preserves
old pins; an explicit sweep can clean proven crash orphans after recovery.

`ReclamationPolicy.AuthorizeRetirement` receives current exact selected history,
owner authority, object metadata/ledger revisions, all pins and selected prior
release references. It must authenticate authoritative retirement and retention
coverage, including all relevant replicas, snapshots, checkpoints and outstanding
obligations. Every active old-generation pin additionally needs positive fencing
that excludes its cached kernel nodes, open descriptors, mappings and any
transferred client paths. A changed workspace UUID, daemon PID, wall clock,
expired lease or new owner by itself proves none of these facts. No production
verifier is implemented. Synthetic test verifiers only exercise the seam.

An authorization includes a nonnil lease keeping the evidence and exclusive
retirement authority valid through one object cleanup, an exact serving
generation/owner-incarnation/cut/object/revision
binding, a retained evidence reference, and a fence reference covering all active
pins. The service checks these bindings and record shapes; those checks cannot
authenticate evidence contents. It journals and protects fenced HANDLE_RELEASE
records, followed by RECLAIM carrying all selected prior and new release
references. Existing apply removes the payload and permanently retires the ID.
The normal Protocol must independently authenticate lifetime/retention effects
both online and on replay. A verifier error or uncertain append/protection/apply
blocks serving and preserves evidence. The lease is released on all return paths.
Hooks must use bounded deadlines and must not reenter the service.

Crashes before and after either release or reclaim are recovered by selected
journal replay into fresh roots. A completed release without reclaim leaves a
retained orphan eligible for a later proof-authorized sweep. Uncertain cleanup
never fabricates an empty object or reallocates its ID. Service source history
remains genesis-based and must not be trimmed by checkpoint segment GC.

## Lock identities and domains

All commands use the exclusively serving volume worker, which keeps catalog,
lifetime and lock transitions ordered. The lock key is the stable ObjectID, so
hard-link aliases share one lock domain. All duplicate local mounts admitted for
a volume must dispatch through this same Service. Separate services or host
replicas cannot independently implement a shared advisory domain. Foreign-home
aliases and cross-volume aliases are not enabled by this task.

`LockOwner{Session, Generation, Token}` identifies an authenticated adapter process
within a session and serving generation. PID alone is insufficient; the adapter
must prevent token impersonation and token reuse across process incarnations.
Credentials do not authenticate opaque owner tokens. Descriptor transfer changes
neither the issuing handle nor its access mode. The receiving process supplies
its own LockOwner for process locks; an inherited lock owner is never fabricated.

The two explicit namespaces follow the initial BSD-style contract:

| Domain | Ownership and conflict |
| --- | --- |
| POSIXLock / fcntl | Process/session owner, per-object byte ranges. Shared ranges coexist; an overlapping exclusive range conflicts with another owner. Same-owner replacement splits and coalesces ranges. Failed conversion preserves existing ranges. |
| FlockLock / flock | Whole-file open description represented by Handle. Shared descriptions coexist; exclusive conflicts with other descriptions, including another open by the same process. Duplicates of the same description share its lock. Conversion relinquishes the old flock before contending for the new mode. |

The domains do not conflict with each other; advisory locks do not reject
ordinary reads/writes. This is an explicit supported policy, not a universal
claim across operating systems or transports. fcntl shared acquisition requires
read access and exclusive acquisition write access on the descriptor. flock can
be acquired through a read-only descriptor. Existing descriptor permissions
survive chmod, while handles, generations and serving authority are rechecked.

Ranges use nonnegative SEEK_SET Start/Length with checked signed arithmetic.
Length zero covers all current and future bytes, including offsets at the signed
limit. Adapters normalize SEEK_CUR, SEEK_END and negative native lengths before
dispatch. flock requires Start/Length zero. GetLock returns one conflicting
holder (or nil); it does not acquire a lock and does not report the caller's own
ranges as conflicts. Unlock removes the specified process range or description's
whole-file lock. Its Kind may be zero because it selects no new lock type.

## Blocking, cancellation and cleanup

TryLock returns WouldBlock on conflict. WaitLock uses the same request/completion
API but parks a bounded request outside the volume queue when it conflicts. The
worker continues processing commands and retries pending acquisitions after
transitions. A blocked request does not own a goroutine or hold the worker.
There is no FIFO fairness or deadlock detection guarantee. No deadlock error is
fabricated; callers can cancel cyclic waits explicitly.

Each WaitLock requires a generation-unique WaitID and a valid LockOwner. The
service retains used tokens for that generation, including completed and
canceled attempts, and rejects reuse. CancelLock with the same owner/token is
ordered against granting. It interrupts a pending original completion, or leaves
an already completed original result unchanged. The adapter must await both the
cancel result and the original completion: if acquisition won, it owns the lock
and must unlock or close it. An abandoned Completion.Wait context alone does
not cancel acquisition. This explicit cancellable wait lifecycle is distinct
from admitted journaled mutations, which continue despite abandoned waits.

DescriptorClose is the per-fd close/flush event: closing any fd for an inode
drops that process's fcntl locks on that inode, even if another fd/dup/fork keeps
the shared description alive. It cancels that owner's pending fcntl requests for
the inode and preserves the handle, lookup counts and flock. Adapters later map
this event from their native close/flush owner notification. Final Release ends
the shared open description, releases its flock, cancels its pending requests,
and also applies close-any-fd fcntl cleanup for the supplied owner. Release must
supply the caller's LockOwner when process locks exist on the inode; missing
identity fails explicitly (including when only fcntl waiters exist). The adapter tracks native duplicate/fork reference
counts and sends Release only when the last description reference is closed.

ProcessExit retires one process identity, releases all its fcntl ranges and
cancels its pending requests. SessionExit does this for every process in that
session and retires the session identity. Neither event alone closes a shared
open description: adapters send DescriptorClose/Release for descriptors actually
closed, preserving descriptions inherited or transferred to surviving processes.
Retired process/session tokens cannot acquire again. Service.Close resolves all
pending lock requests as Unavailable and empties the volatile lock table.

Lock ranges, wait attempts retained for cancellation, process identities and
retired sessions are each bounded by Limits.Locks (default 4096). Capacity
exhaustion returns NoSpace; it never silently evicts locks or reuses tokens.
Recovery starts a new generation with new resource budgets.

## Restart and owner change

Locks and waits are local volatile state and are never replayed or handed to a
new owner. Old handle generations and LockOwner generations fail as Stale. The
external ownership protocol must exclude old serving and cached access paths
before a new owner opens service; losing a daemon does not establish that fence.
Applications must observe session/descriptor loss, reopen and recover/reacquire
locks explicitly. The service never reports an old lock transferred successfully,
never accepts an old process token in the new generation, and never promises
that a database transaction survived owner change merely because a new lock can
be acquired. Retained old object pins require the independent proof described
above before their payloads can be removed.
