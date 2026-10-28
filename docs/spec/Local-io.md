# Local I/O and simulated crashes

Task `kq3` supplies `internal/vfs`, the local persistence seam for future journal
and backing-store code. It is separate from the workload-facing FUSE API and
from the distributed acknowledgement contract in [Durability](Durability.md).
No distributed filesystem, checkpoint publisher or replay engine is implemented
by this package. `Backing-store.md` did not exist when this seam was added;
xattrs have therefore not been added speculatively.

`FS` opens regular files, renames files or directories, unlinks regular files,
creates directories and syncs directories. `File` offers offset reads and writes,
truncate, fsync, fdatasync and close. Names are relative slash paths, with `.`
reserved for the root directory barrier. Seek position, append mode, synchronous
open flags, hard-link creation, symlinks, directory removal, readdir, ownership,
permission changes, timestamps, mmap and locking are outside this local seam.
Consumers can request smaller interfaces containing only the methods they need.
On the supported Linux and FreeBSD targets, rename uses a kernel `renameat`
through parent descriptors opened beneath `os.Root`. This preserves atomic
replacement of an empty directory, which Go's `os.Root.Rename` rejects before
calling the kernel. Neither destination removal nor a multi-step replacement is
used. Other platforms inherit Go's more restrictive directory-rename behavior.

The OS backend opens an existing private root using Go's `os.Root`; it does not
create or clear the root. This confines path resolution but does not prevent
crossing mountpoints or accessing externally introduced special files. Supply a
private backing directory without externally modified symlinks, mounts or device
nodes. `Open` checks existing file type before opening; this is not a defense
against another actor racing changes inside the private root. Contexts are
checked before operations; they cannot cancel a storage syscall already running.
Close always attempts release without a context. Kernel errors, including a
directory-sync capability failure, propagate to the caller.

## Assumed storage contract

For this model, successful fsync makes the file's preceding data and size durable.
fdatasync covers data and retrieval-critical metadata; Linux uses fdatasync and
other platforms, including FreeBSD, conservatively use fsync. Close adds no
durability. Syncing a file does not persist its name. Create, unlink, mkdir and
rename need the relevant parent directory barrier; a cross-directory rename needs
both parents. A new nested directory also needs its own parent name persisted.
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
change the live image; fsync/fdatasync copy it to the synced image. Handles retain
identity across rename, replacement and unlink. Crash invalidates every handle,
so an old descriptor cannot silently start referring to a replacement inode.

Namespace operations are atomic in memory and during recovery. A directory
barrier marks preceding operations on that directory; a rename is directly
durable when both modified parents have barriers. Recovery also retains the
preceding namespace operations on the touched identities needed to replay a
retained operation coherently. This deliberately models a conservative ordered
subset of possible namespace histories, preventing recovered directory cycles
or duplicate directory parents. Dependency retention can keep an earlier
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
permission enforcement or umask. OS and simulator error details may differ.

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
directory moves, ancestor/emptiness checks, invalid paths/flags, canceled
contexts and root escape rejection. Simulator cases cover independent file/name
barriers, checkpoint rename publication, unsynced/synced unlink and truncate,
cross-directory rename barriers, stale handles, nested directory durability,
allocation limits and seeded loss/retention/tearing reproducibility.

Tests require a fresh explicit user approval before execution, including local
unit tests. Static build, vet, formatting and cross-build results are reported
separately; they do not establish these runtime properties.
