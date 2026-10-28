# Local backing-store implementation boundary

`internal/storage` implements the version 1.0 private data format over
`internal/vfs.FS` plus `vfs.Discovery`. It is a local primitive backend for
trusted apply/service callers. It neither chooses commitment nor supplies the
journal replay engine, distributed transaction coordinator, permission checks,
FUSE cache barriers, fencing, replica coverage, snapshot administration or
checkpoint acceptance. Those remain separate implementation gates.

`Format` is explicit and requires two empty, distinct, private roots, the exact
protected root object/metadata and an initial history. It does not upgrade or
repair existing trees. `Open` requires an externally selected `Image`, validates
both role markers and identities, the bounded catalog and private physical tree,
and verifies every checkpoint-base payload's exact length and SHA-256. Neither a
native inode nor a current host pathname can reconstruct logical metadata.

The caller owns both VFS roots and their administrative admission. Linux and
FreeBSD syscall differences stay in `internal/vfs`; storage uses no host syscall,
xattr, native hard link, host symlink or workload namespace mirror. Admission
checks exact 0700 directories, exact 0600 regular files, UID, native link count
one, repeated physical identities and device changes. Discovery refuses special
files and symlinks. Device checks reject foreign-filesystem mounts; externally
managed mounts, including same-device bind mounts, and concurrent administrative
changes inside admitted roots are excluded by the private-root ownership
contract. Kernel mount admission/fencing is outside this local backend.

## Semantic operations

`Operation` carries an immutable operation UUID, complete history identity and a
contiguous backing-apply sequence, primitive kind, exact home ObjectID, complete
object afterimages and exact before/after name states. The journal adapter must
map its complete authoritative committed suffix to these contiguous apply slots;
it cannot skip dependency effects or treat a received journal position as an
applied operation. The initial bounded implementation retains every applied
receipt from slot one; reaching the operation limit stops admission. Compaction
of those receipts requires the later checkpoint/replay protocol.

Object afterimages contain the immutable descriptor, exact logical metadata,
reference ledger revision, complete sorted `(parent ID, raw name, entry revision)`
reference set, and a directory's exact parent relation. Namespace entry revisions
remain after removal. Updates require the exact old object revision and next
result revision, with independent revision checks for ledger and parent changes.
Changing a directory's logical link count alone does not advance its namespace
reference ledger revision: child directory additions/removals change metadata,
while changing the directory's own namespace references requires the next ledger
revision. Regular-file and symlink link counts remain tied to their reference sets.
Directory ancestry, local alias/ledger consistency, logical link accounting,
empty-directory removal, immutable symlink bytes and regular payload sizes are
validated before physical modification. The service chooses exact owner times,
permission/special-bit changes and credentials; storage applies no host-derived
metadata policy. Reads preserve atime.

Create/mkdir/symlink, offset write, exact truncate, link, unlink, rename, setattr,
lookup/read/readdir/stat use these logical identities. Writes and truncates
address a private regular payload by its never-reused home allocation. Logical
unlink and replacement retain the old payload. Same-object rename is an explicit
no-op with no catalog edits. Directory/symlink objects never gain host payloads.

An applied slot retains the operation UUID and SHA-256 of deterministic semantic
JSON, with all raw names converted to canonical padded base64 before hashing.
Exact retries, including older CREATE/WRITE operations after later operations,
return without touching physical bytes or resolving paths. A conflicting digest,
operation-ID reuse, gap, exhausted revision or mismatched expected state fails.
The operation structures have no maps; slice order is part of the protected
semantic encoding. Callers must retain that order on retries.

An operation lists its exact sorted participant-home set, including dependencies
in both old and new reference/parent states. Foreign-home operations require an
external `Closure` implementation; standalone calls fail closed. `Closure.Enter`
must authorize the committed decision and retain closure-wide read/apply gates
until every participant and the shared visibility barrier complete. A home-only
ledger fragment and a directory-home entry fragment can be installed through
this seam. Releasing one local callback is not distributed publication; the
external coordinator must retain its overall gate on error. Storage does not
construct prepares, select COMMIT or infer that another participant installed.

Protected handle pin/release afterimages preserve the ObjectID, serving generation
and incarnation. Pins remain conservative across uncertain release/restart. The
service must protect them before exposing handles or operation-authorizing kernel
references and prove drainage/revocation before release. `Reclaim` requires a
trusted protected retention decision; storage additionally requires zero links,
references, live entries and active pins. Physical identity descriptors and
catalog allocation tombstones remain permanently, even after payload removal.
Name tombstones inside a reclaimed directory remain validated against its
permanent directory descriptor; that descriptor cannot authorize live entries.

## Allocation and crash behavior

`Allocate` creates UUIDv4 identities from cryptographic randomness by default.
It checks live objects, retired identities, pending reservations and physical
object-directory existence. A reservation is an immutable, synced
`staging/<operation-uuid>/allocation.json` record containing magic
`HOMELABFS-ALLOCATION`, major `1`, operation UUID and object ID. Its directory and
parent are synced. Repeated allocation for the same operation returns the same
identity. Collisions retry within a bounded attempt budget; retained descriptors
and reservations prevent deliberate reuse. A reservation cannot be consumed by
another creation operation.

The in-memory catalog publishes only after successful payload application. The
store serializes reads with application; any physical apply error closes its
read gate and preserves the old catalog and partial private resources. Errors
after commitment do not imply abort. The later recovery engine must reconcile
protected history and provide a clean recovery destination when required.

An `Image` contains all live objects, namespace tombstones, full home ledgers,
directory parents, pins (including releases), retired allocations, applied
receipts and exact clean payload hashes. `Export` syncs payloads, immutable
identity files and directory dependencies, and captures the current installed
cut. The caller must preserve matching payload bytes before allowing later
application. An older catalog alone cannot bless a dirty baseline: `Open`
rejects length/hash mismatch, including retained/torn bytes from a later or
discarded suffix. Missing checkpoint-base payloads are errors. Objects allocated
after the baseline may be reinitialized only by their exact protected creation
descriptor, then replayed through the complete selected ordered suffix.

## Immutable publication primitives

`StageCheckpoint` verifies the supplied image against the current catalog and
payloads, syncs them, and writes/syncs immutable `catalog.json` and `manifest.json`
in private checkpoint staging. A successful staging attempt freezes local apply
at that cut. A failed attempt after installing this gate retains it and evidence
for retry/recovery. The caller must also retain its external closure gate.

`PublishCheckpoint` renames the complete staged directory into `checkpoints/`
and syncs both parents. `PublishCurrent` revalidates the installed cut and exact
manifest, writes/syncs a fresh `CURRENT.tmp-<publication-uuid>`, renames it over
`CURRENT`, and syncs the root before releasing the local apply gate.
`ReadCurrent` validates pointer, manifest and catalog lengths/hashes without
claiming journal acceptance or distributed authority. Old checkpoint directories,
staging and old journal evidence are never deleted by these helpers.

The manifest includes a checkpoint UUID, volume/root IDs, exact cut, catalog
length/SHA-256 and opaque canonical-base64 external dependency bytes. The caller
must durably protect matching journal acceptance and closure evidence before
`PublishCurrent`. No helper advances retention or publishes a closure peer.
The later checkpoint engine must implement acceptance pairing, complete closure
fan-out, predecessor selection and interrupted-publication reconciliation.

`EncodeImage(image, limits)` and `DecodeImage(bytes, limits)` apply the same
configured catalog and byte limits. Zero fields select the default caps; larger
explicitly admitted caps are retained through export, encoding and publication.

Catalog JSON uses magic `HOMELABFS-CATALOG`, major/minor `1/0`, exact canonical
UUID strings, sorted object/entry/pin/allocation/receipt/payload arrays, integer
metadata and positions, lowercase hexadecimal hashes, canonical padded-base64
raw names and symlink text. Every schema field is mandatory (including explicit
null parent/target fields); unknown or wrong-case keys, duplicate keys, invalid
UTF-8 JSON, missing newlines, unsupported versions, duplicate identities and
invalid ranges are rejected. Timestamp fields retain exact signed `Seconds` and
unsigned `Nanoseconds`; no floating-point or apply-time conversion occurs.

Local tests cover Linux OS temp roots and the VFS crash simulator, primitive
retries, replacement lifetime, allocation collisions, raw name bytes, strict
markers/catalogs, unsafe physical trees, foreign dependency gates, dirty baseline
rejection, complete ordered suffixes and immutable publication crash windows.
They establish no lab, FreeBSD kernel, distributed-recovery or FUSE evidence.
