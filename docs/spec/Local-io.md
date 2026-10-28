# Local I/O and simulated crashes

Tasks `kq3` and `dw3` supply `internal/vfs`, the local persistence seam for
future journal and backing-store code. It is separate from the workload-facing FUSE API and
from the distributed acknowledgement contract in [Durability](Durability.md).
No distributed filesystem, checkpoint publisher or replay engine is implemented
by this package. The [backing-store contract](Backing-store.md) selects logical
metadata and ID-keyed payload files without xattrs, and uses the narrow
discovery/GC capabilities for store implementation.

`FS` opens regular files, renames files or directories, unlinks regular files,
creates directories and syncs directories. `File` offers offset reads and writes,
truncate, fsync, fdatasync and close. Names are relative slash paths, with `.`
accepted for root directory barriers and discovery. `Discovery` adds physical
stat, directory handles and empty-directory removal. File handles from both
backends additionally implement `Statter`; `Dir` includes descriptor stat and
bounded `ReadDir`. Seek position, append mode, synchronous open flags, hard-link
creation, symlinks, host ownership/permission changes, timestamps, mmap and locking
are outside this local seam.
Consumers can request smaller interfaces containing only the methods they need.
On the supported Linux and FreeBSD targets, rename uses a kernel `renameat`
through parent descriptors opened beneath `os.Root`. This preserves atomic
replacement of an empty directory, which Go's `os.Root.Rename` rejects before
calling the kernel. Neither destination removal nor a multi-step replacement is
used. Other platforms inherit Go's more restrictive directory-rename behavior.

The OS backend opens an existing root using Go's `os.Root`; it does not create
or clear the root. It rejects a final symlink at the supplied root and verifies the
opened root's local identity against the initial inspection. All relative path
components are checked without following symlinks, even links pointing inside
the root; special files are rejected before opening or mutation. This confines
path resolution but does not prevent crossing mountpoints. Supply a private
backing directory without externally modified names, symlinks, mounts or device
nodes. The checks do not defend against another actor racing changes in that
tree. Contexts are checked before operations and during enumeration; they cannot
cancel a storage syscall already running.
Close always attempts release without a context. Kernel errors, including a
directory-sync capability failure, propagate to the caller.

## Physical discovery and admission

`Stat` and descriptor `Statter.Stat` return only platform-neutral physical
`Info`: file type/mode, size, numeric UID/GID and local device/file identity.
No native stat structs cross the seam. Use root `Stat(".")` and `OpenDir(".")`
descriptor evidence to validate the admitted owner, private permissions and
physical identity under the caller's root/mount/overlap policy. `NewOS` itself
does not choose an allowed UID, enforce 0700, or claim exclusive ownership.
Admission must establish those policies before store initialization or recovery.
The root and its descendants must belong to one backend owner; exposing these
capabilities is not permission to repair ownership or relax private permissions.
Physical identity can detect local aliases but is reusable after deletion and
must never become replicated ObjectID, logical Unix metadata or serving state.

`OpenDir` accepts only directories. Its handle remains attached to that local
directory through rename or empty-directory removal. `ReadDir(ctx, n)` requires
`1 <= n <= MaxReadDir` (1024), returns at most n immediate children and bounds
allocation by the requested page plus a fixed OS directory buffer. It does not
materialize or sort a complete directory. On Linux/FreeBSD it reads bounded raw
names, then checks context before each no-follow `fstatat` relative to the open
directory descriptor. Identity and cancellation behavior do not depend on a
Go patch release's `DirEntry.Info` behavior. There is no ordering guarantee or
snapshot across pages. Recovery/GC must exclude concurrent mutations for the
complete scan and apply its own total count/byte/time budget; paging alone does
not bound the total work a caller requests. Invalid limits do not advance the
cursor. Context cancellation and entry/stat/type errors may accompany a partial
page, whose contents cannot authorize an incomplete scan. Treat every non-EOF
error as a failed scan; restart discovery with a new handle under the required
gates. EOF may accompany the last page or a later empty page. Symlinks and special
files fail discovery rather than being followed or silently admitted.

`Rmdir` accepts only empty directories and rejects `.`. It never implements
recursive deletion, and regular-file `Unlink` remains separate. Wrong types,
symlinks, invalid/escaping paths and nonempty directories fail without mutation.
Sync the modified parent directory after removal before relying on its
persistence. Descriptor close adds no barrier; closed or crash-stale handles
fail clearly. OS root close releases the root while existing file and directory
descriptors remain independent.

## Assumed storage contract

For this model, successful fsync makes the file's preceding data and size durable.
fdatasync covers data and retrieval-critical metadata; Linux uses fdatasync and
other platforms, including FreeBSD, conservatively use fsync. Close adds no
durability. Syncing a file does not persist its name. Create, unlink, mkdir,
rename and rmdir need the relevant parent directory barrier; a cross-directory
rename needs both parents. A new nested directory also needs its own parent name persisted.
Syncing a directory does not sync its children’s file data.

These are the explicit assumptions for ZFS-backed operation with normal sync
semantics, not new real-kernel evidence. `sync=disabled` violates the required
contract. Hardware, pool configuration, directory fsync and the complete guest
transport still need separately approved validation on the actual target.
The simulator cannot establish power-loss behavior of ZFS or bhyve.

For checkpoint publication, the caller must sync the checkpoint file before
rename, sync the modified parent directories, then allow retention to rely on
the checkpoint. See [Journal storage](../Journal-storage.md). Backing-file
application and namespace changes must become durable before publishing their
applied checkpoint. No cross-file or cross-dataset transaction is implied.

## Simulator recovery model

The simulator gives each inode live and synced byte images. Write and truncate
change the live image; fsync/fdatasync copy it to the synced image. File handles
retain
identity across rename, replacement and unlink; directory handles do so across
rename and empty-directory removal. Crash invalidates every handle,
so an old descriptor cannot silently start referring to a replacement inode.

Namespace operations are atomic in memory and during recovery. A directory
barrier marks preceding operations on that directory; a rename is directly
durable when both modified parents have barriers. Recovery also retains the
preceding namespace operations on the touched identities needed to replay a
retained operation coherently. This deliberately models a conservative ordered
subset of possible namespace histories, including empty-directory removals,
preventing recovered directory cycles or duplicate directory parents. Dependency retention can keep an earlier
unsynced operation, including one involving another directory; it never syncs
file bytes. A newly created directory's own name is independent of its contents:
syncing the directory does not implicitly persist its name in its parent.

`Config` chooses separate data and namespace policies:

- `Lose` restores synced bytes and discards namespace operations without a
  barrier or retained dependency. It is the default adversarial policy.
- `Keep` retains dirty state, representing completed background writeback.
- `Tear`, for data only, selects a recovered size between old and new sizes and
  copies a seeded prefix of new bytes over old bytes, leaving zeros where no old
  bytes existed. It can produce partial overwrites, extensions and truncations.
- `Random` chooses Lose/Keep/Tear per file and Lose/Keep per namespace operation.
  It retains dependencies after making those choices.

Crash reconstructs state, removes unreachable objects, and treats recovered
state as the next durable baseline. The PRNG stream continues across crashes.
Record the seed, configuration and exact serialized operation schedule; the
same seed does not determine goroutine arrival order. PRNG consumption follows
inode and namespace-operation order, independently of Go map iteration.

This model does not enumerate arbitrary sector reordering, every possible
namespace writeback combination, physical-sector tears, media corruption,
storage errors, short writes or timing failures. A torn write may also recover
entirely old or entirely new bytes. It is not a model of the ZFS transaction
group implementation. Loss/retention/tearing are recovery choices, while runtime
I/O-error injection remains separate. The default per-file allocation bound is
16 MiB; callers must additionally bound file counts and operation histories.
The simulator checks open access flags but does not emulate credentials,
permission enforcement or umask. Physical simulator modes retain creation bits,
its root is 0700, UID/GID are deterministically zero, and local device/file IDs
are simulator-only evidence. Its paged enumeration selects successive names
without copying the directory; this trades scan time for bounded allocation and
checks cancellation during scanning. OS and simulator error details may differ.

The OS backend serializes namespace methods, including type-check/open and
type-check/unlink sequences, directory syncing and the parent-descriptor rename
sequence. Concurrent calls through one backend cannot replace a checked regular
file with a directory before its operation completes. Open handles perform file
I/O independently of that lock and retain their inode identity. This lock does
not coordinate separate OS instances or external actors accessing the same root;
one backend must own each private backing tree.

## Prepared validation

The package tests cover both backends in temporary/in-memory roots: offset and
sparse I/O, truncation, explicit barriers, replacement and open-unlinked handles,
directory moves, descriptor stat/identity, paged discovery/allocation bounds,
private-root evidence, symlink/special-file rejection, empty-directory removal,
ancestor/emptiness checks, invalid paths/flags, canceled contexts, closed handles,
concurrent namespace calls and root escape rejection. Simulator cases cover
independent file/name barriers, checkpoint rename publication, unsynced/synced unlink and truncate,
cross-directory rename barriers, synced/unsynced empty-directory removal, stale
file/directory handles, nested directory durability, allocation limits and seeded loss/retention/tearing reproducibility.

Tests require a fresh explicit user approval before execution, including local
unit tests. Static build, vet, formatting and cross-build results are reported
separately; they do not establish these runtime properties.
