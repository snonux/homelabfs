# Local journal segment implementation

Task `mq3` implements `internal/journal/segment` over `internal/vfs` and the
version 1.0 [record codec](../../internal/journal/record/README.md). The normative
bytes, stream roles and canonical paths remain in
[Journal format](Journal-format.md). This package supplies local persistence,
bounded verification and exact-byte iteration. It establishes no distributed
authority, commitment, visibility, fencing, replay selection or retention rights.

## Admission and exclusive ownership

The caller supplies an authorized `Stream` and a `Backend` rooted in a private
admitted backing directory. `Stream.Directory()` derives the complete canonical
owner or reporter path. Every ancestor and the stream directory must already
exist with their names durably published. The package never initializes a
volume, creates missing ancestors, deletes old segments or follows another
history automatically. Root owner/permissions, mount/overlap policy and storage
configuration remain caller admission responsibilities described in
[Local I/O](Local-io.md).

Before opening a writer, the caller must select and reconcile the required
history, including every known durable/accepted/protected prefix and dependency.
A successful local scan, a complete-looking shorter suffix, or an empty stream
directory cannot prove that a previously accepted tail never existed. The
package has no distributed progress registry and must not be used to bypass
those gates merely because no incomplete frame remains on this replica.

The caller must exclude other writers, retention, renames and external actors
throughout recovery and the subsequent writer lifetime. A reader requires its
fixed retained files to remain unchanged for its lifetime; it is not a concurrent
tail follower. Mutexes serialize operations through one writer or reader; there
is no cross-instance/process lock or distributed ownership mechanism. Publication
checks that its destination does not exist before rename; the exclusive caller
gate makes that check reliable. Existing destination files are never replaced.

## New records and historical bytes

`NewWriter` verifies the complete retained stream before opening its final
unsealed segment or creating a successor. `Options.Structural(sequence)` supplies
stable creator-selected operation, incarnation and diagnostic timestamp fields
for new OPEN/SEAL records. The writer fills their history and sequence from its
authorized stream. `NewSegment` supplies fresh nonzero identities; reuse of a
retained segment identity fails. Callbacks run under the writer lock and must not
reenter it. Reporter structural metadata must name the reporter incarnation.

`Append` accepts a typed **new** record with sequence zero. Its other header
metadata must already be selected by its authorized creator and its history must
match the writer. The writer chooses its sequence, reserving complete candidate
and SEAL space; size rotation can consume two intervening structural positions.
Its returned `Receipt.Reference` identifies the final exact bytes and position.
An already positioned record is rejected. The caller must not convert an old
record to sequence zero to resend it.

This local allocation API is deliberately separate from replica import. A future
receiver must retain the original creator's complete bytes, including OPEN and
SEAL positions, and verify its prefix under authorized import semantics. It must
never pass historical records through this writer or replace creator incarnation,
operation, timestamp or sequence. Import/deduplication/replication transport is
outside `mq3`; the iterator supplies owned exact bytes for forwarding.

Sequence arithmetic never wraps. A retained suffix ending at `MaxUint64-1`
remains readable, but writable recovery and sealing reject a next position of
`MaxUint64` before mutation, because advancing past that seal would overflow.

## Durability and grouping

The default `Append` writes the complete frame at its exact offset and fsyncs
before returning success. `Receipt.Wait(ctx)` observes a local covering durable
boundary only. Local durability never authorizes replay or a client ACK by
itself. Context cancellation does not establish that an operation was absent;
an in-flight OS syscall cannot be canceled by this seam.

With `Options.GroupCommit=true`, successful append returns a pending receipt.
The caller schedules explicit `Flush`; there is no timer or background worker.
One flush syncs all preceding appends, releasing their waiters together. Appends
after that barrier need a later covering sync. Rotation/seal also provides a
covering barrier. At most 1024 receipts may remain pending; further admission
returns `ErrLimit` until a flush. Receipt cancellation only stops that wait.
`Close` adds no barrier and releases unsynced waiters with `ErrClosed`.

New OPEN publication uses exclusive create, complete write, file fsync and parent
directory fsync before admitting records. Rotation writes/syncs SEAL, renames to
the closed range name and fsyncs the segment directory, then writes/syncs a new
exclusive OPEN and fsyncs its parent. The closed filename includes the SEAL
sequence. Seal hash/counts include every preceding complete frame, including OPEN
and checksums. Calling `Seal` leaves the writer sealed until a later append
creates its successor. An I/O/failpoint error poisons the writer, fails unresolved
waiters and requires close plus fresh recovery. Bytes may have become durable
despite an error; they remain uncertain candidates. Resources are preserved.

## Recovery and repair gates

`Recover` discovers only the selected stream directory in pages of 64 entries.
It rejects unexpected names/types, hard-linked files, gaps, overlapping ranges,
duplicate segment identities, interior open files, role/history mismatches,
noncontiguous positions, invalid seals and bytes after a seal. The codec checks
bounded prefix lengths, versions, schemas and checksums. Recovery never searches
ahead for magic. OPEN/predecessor references must agree byte-for-byte in stream
key and hash; nil `PreviousSeal` requires sequence 1. A caller-authorized
`RecoveryOptions.PreviousSeal` can anchor a retained suffix, but does not itself
prove a checkpoint or retention right.

`Limits` bounds the whole directory and scan: defaults are 1024 segments and
1 GiB total physical bytes, with the format's 256 MiB per-segment ceiling.
The writer enforces the same retained count/byte budget, reserving SEAL space,
so it does not create a stream its configured recovery cannot admit. Increase
limits explicitly only after capacity admission. No garbage collector exists.
Frame reads allocate one bounded frame at a time; recovery retains summaries and
incremental hash state rather than buffering entire segments.

Only incomplete outer framing at the last verified boundary of the final open
segment, after a complete OPEN, can return `*TornTail`. A complete fixed header
must pass bounded framing and stream/sequence/role/capacity checks. Fewer than
144 final header bytes are also incomplete framing. A physically complete outer
frame containing malformed/incomplete embedded evidence is corruption, never a
repairable disk tail. A short or missing OPEN is interrupted creation and gates
recovery; it is never an initialized empty volume. Closed/interior truncations,
complete bad checksums, invalid schemas and unsupported versions stay untouched.

The default preserves all torn bytes and returns the tail error.
`RecoveryOptions.Repair(ctx, Tail)` is an explicit caller authority/evidence gate,
not a policy implemented by this package. Before returning nil it **must**:

- Persist an exact original copy outside the scanned stream directory, including
  the incomplete bytes and the stream identity/offset needed for reconciliation.
- Reconcile required owner/partner accepted tails and establish that truncating
  at `Tail.Boundary` discards no known durable, accepted, protected, checkpoint or
  required dependency prefix, under the authorized history selection.
- Keep serving and mutation gates closed until the complete selected history and
  dependency closure have been verified.

The callback must not modify scanned files. After it approves, recovery truncates
only the described final suffix and fsyncs the file. Failure keeps resources and
the caller's retained original available for inspection. A torn frame is never
an absence proof. Recovery does not infer an abort or select commitment.

A verified SEAL under an open filename is fsynced before recovery finishes its
rename and parent barrier. This also handles recovery invoked after an uncertain
runtime error without an intervening simulated crash. A resumed unsealed writer
syncs its observed complete prefix and parent name before admitting new work.

## Sequential reads and validation scope

`OpenReader` verifies the entire bounded retained suffix before returning an
iterator. Its inclusive starting `Position` must belong to the supplied stream
and retained range; reporter identity comes from the full `Stream`, because a
bare Position cannot distinguish owner and reporter sequences. `Next` includes
OPEN/SEAL and returns owned exact frame bytes, typed records and stream-aware
references. Returned buffers and pointer fields are independent of the reader's
verification state. It revalidates incrementally while reading, stops on errors and
returns `io.EOF` at the selected end. Seeking skips verified earlier frames;
there is no unverified random access or infinite tailing.

Focused temporary-root OS tests and simulator tests cover owner and reporter
rotation, restart, byte/reference preservation, every final-frame truncation,
every closed-file truncation, bit flips, valid-CRC seal/evidence corruption,
discovery/range/role/budget failures, grouping and concurrent covering waits.
Crash schedules stop at every occurrence of before/after create, append, file
sync, rename and directory sync under data Lose/Keep/Tear and namespace Lose/Keep.
Separate repair/publication cases cover truncate and recovery barriers. These
checks exercise the local model, not ZFS power loss, bhyve, FUSE, lab VMs or
distributed failover. Those retain the explicit approval requirements in
[Test automation](../Test-automation.md).
