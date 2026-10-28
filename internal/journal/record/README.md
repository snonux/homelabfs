# Journal representation codec

`record` implements the complete typed byte schemas in
[`Journal-format.md` 1.0](../../../docs/spec/Journal-format.md). All 36 record
kinds and ten mutation argument kinds have concrete schemas. The closed unions
(`Record.Payload`, `Intent.Arguments`, `RequestDocument`) reject mismatched or
unknown alternatives. SETATTR writes only masked fields, with no presence bytes
between them. Public original bytes agree with `fsapi.OriginalRequest`.

`Encode` returns an owned complete frame; `Decode` accepts exactly one frame.
Integer widths and endianness, optional presence, blob lengths, ordered fields,
and array counts are explicit. The small reflection walker visits these typed
schemas: it never casts Go memory, writes padding, serializes native inodes, or
uses Go struct memory layout. UUID, object, time, principal and request identity
semantics reuse `fsapi`; journal-specific field order remains codec-owned.

Decode validates the fixed prefix and bounded lengths, CRC32C, known schemas,
strict sets and available cross-field invariants. It checks remaining bytes and
minimum element sizes before allocating arrays, per-type limits before copying
blobs, and a cumulative 64 MiB decoded-allocation budget per enclosing frame.
Evidence is additionally capped at 4 MiB of complete embedded bytes and cannot
contain OPEN, SEAL, or any record with another Evidence field, even an empty one.
Evidence frames are independently validated and sorted by their stream key.

`Error.Kind` distinguishes `Torn`, `Corrupt`, and `Unsupported`; use `errors.As`.
Torn describes incomplete outer framing only; incomplete embedded Evidence is
Corrupt because the enclosing frame is complete. The caller must determine
whether this is the final frame of the final unsealed segment; a closed/interior
truncation is a recovery gap. Unsupported versions/kinds, including embedded
ones, require explicit handling that preserves their bytes. This package never
scans ahead or truncates a stream.

Every mutation's local `Redo.Objects` must exactly equal its immutable global
`Intent.SelectedMetadata` subset for the header volume. All requested creation
mode/umask and SETATTR source/value fields remain part of internal canonical
identity. `EncodeInternal` hashes `HOMELABFS-INTERNAL` plus NUL plus those exact
bytes. Request claims verify the correct public or internal digest. PREPARED
verifies its exact redo hash and agreement with the immutable Binding intent.
INSTALL also checks its embedded redo against its PrepareRef hash. BEGIN binds
public operation kind/replacement policy to its resolved intent; INSTALL and
COMPLETE bind their request identity to the selected decision. Local prepare,
decision-slot, frame and authority history identities must agree where repeated.

`ReferenceTo` derives owner/reporter references from validated bytes. A SEAL does
not encode its stream kind; use `SealReference` with the verified OPEN payload.
The segment reader remains responsible for verifying that OPEN and SEAL belong
to the same authorized contiguous stream and for checking hashes/counts over
all preceding frames.

Representation validation cannot establish commitment, source authorization,
old-catalog contents, complete ancestry/namespace/ledger agreement, physical
fencing, receipt retirement, or external artifact semantics. These require
retained state and the later protocol/storage implementations. The codec checks
self-contained invariants and preserves the typed inputs required for those
checks; successful decoding grants no permission to replay or serve.

The nine `.hex` fixtures and `manifest.json` are literal normative outputs from
the committed specification, independent of this encoder. Tests construct each
fixture through typed values, compare exact bytes and hashes, then decode and
re-encode. Additional tests cover every kind/operation, every single-bit change
in frame examples, every truncation, valid-CRC semantic corruption, required
stream roles and minimum actual OPEN capacity. Four fuzz targets exercise raw
frames, valid-CRC mutations, public/internal documents, and preallocation bounds.
The existing Mage `FuzzShort` discovers all four and bounds each to five seconds
with one worker. These are local codec checks; they do not exercise lab volumes,
mounts, guests, storage publication, segment recovery, or replication.
