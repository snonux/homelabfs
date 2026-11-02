# Bounded committed owner RAM views

`fz3` implements the local component boundary in `internal/logicalview`,
`storage.Project` and `fsservice.LogicalPlanner`. It supplies no network protocol,
native FUSE cache algorithm, retained 9p implementation or serving grant. Normal
publication remains closed. Paul selected the separate
[retained guest trust design](../lab/Retained-9p-authorization.md) on 2026-10-31;
this component does not implement that delegated frontend.

A view owns complete bounded RAM snapshots for a dependency closure, including
regular-file bytes, zero-filled holes, sizes, namespace tombstones, immutable
home identities, home reference ledgers, directory parents, modes/credentials,
timestamps, symlinks and lifetime pins. Sparse files use explicit zero bytes;
logical size therefore consumes the RAM bound before allocation. This trades
space for a small implementation with the existing backing primitive/catalog
validators. A view neither reads nor writes owner backing storage after its
verified baseline has been copied.

`LogicalPlanner` shares the local service's mutation planning, Unix permission
checks, redo translation and read implementation. Its metadata, append offset,
permission check and payload read use the same captured RAM observation.
Allocations and operation identities are permanently reserved in a bounded RAM
ledger separately from eventual backing file creation. Typed ordinary normal3
grants still reject public transactions and foreign-home mutations. The generic
view can project complete per-home primitive batches and validates every alias
against its actual home ledger; that is a foundation for later transaction
integration, not an implementation of the public transaction protocol.

One staged candidate follows the exact previous selected certificate. Stage
copies the immutable intent, redo and actual dependency bytes, validates a full
projected afterimage, and exposes no changes. Selection requires the explicit
external verifier to establish origin-local durable choice/result and distinct
memory retention, including O outside replica placement. Remote O uses A; O=A
uses B. There is no owner-local durable-write substitution. A failed selection
leaves the candidate and identity unresolved and invisible.

Selection and visibility are separate. The old committed cut remains available
until publication. Publication requires all in-flight component observations to
close, the exact protected result, and matching acknowledgements from the entire
admitted cache-domain inventory. The external verifier must establish actual
STOPPED/DRAINED/installed physical barriers. Empty domains support internal
component use only and confer no mounted frontend capability. No stock FUSE
invalidation, zero TTL, timeout, disconnected session or model result supplies
those physical facts. Open handles preserve object home and granted access
through chmod, rename and unlink; a serving-generation mismatch is stale.

Per-origin received/durable positions, selected cut, visible cut, backing
installation and backing durability remain distinct. Neither receipt nor source
progress selects a candidate, and neither backing mark changes visible RAM.
Source dependencies and unresolved claims stay pinned. Retirement requires an
explicit verified equivalent durable replacement; a client reply or disk apply
alone cannot delete them. Permanent identity/allocation ledgers remain after
claim retirement, and finite exhaustion gates new planning.

Before copying a baseline or admitting a candidate, the component reserves a
fixed memory arena that includes the maximum retained versions, candidate,
observation/planning and verifier copies, claim bytes and identity ledgers. A
separate isolated recovery arena remains available when ordinary capacity is
full. Limits reject version, claim, source, handle and logical-size exhaustion
before effects. Baseline admission counts the exact canonical catalog plus
payload bytes and permanent identities across every home before copying any
snapshot. Candidate admission first validates and counts the complete projected
closure through a metadata-only pass of the shared primitive validator; it does
not copy baseline payloads or allocate holes during that pass. Generic staging
also counts distinct new allocation identities across the whole batch and honors
allocations already reserved for the matching claim. Callers also charge
request-owned input/output buffers and recovery destinations through their
admission packages; the view cannot account for buffers retained by an external
caller.

`WriteRecovery` serializes all retained versions, unresolved candidate,
selected/visible/backing frontiers, claim/source evidence, permanent identities,
allocations and open handles. `Restore` reserves capacity before bounded decode,
validates independently owned snapshot bytes and exact cuts, and requires the
external recovery verifier for complete durable source coverage, issued grants,
old-domain exclusion and any surviving continuity. Component serialization is
not a distributed durable checkpoint. A new daemon UUID cannot erase an old
pin or validate an old handle. Actual journal-driven crash admission, transport
binding, source-release/checkpoint lifecycle and native/guest barrier mechanisms
remain the separately gated protocol/platform work.

The generic default profile admits 128 KiB per closure snapshot, 128 KiB per
claim, four retained versions, eight claims, two simultaneous observations and
512 permanent identities and version-pin tokens. It reserves 5.625 MiB of
ordinary RAM and 15.5 MiB of recovery capacity per affected volume before copying. Larger disposable profiles
must explicitly increase both view and admission limits. Multi-home arenas are
conservatively charged in full to every affected volume; this avoids splitting
one volume's accounting by closure ID. Source/body buffers and canonical transfer
copies are included in these fixed reservations. This is a configured bound,
not a benchmark or a measured native-kernel allocation figure.

Logical description admission and retirement have separate mandatory verifier
phases for protected home pins. Lookup results from this planning/observation
seam are internal values; a mounted adapter must also establish its protected
lookup/mapping lifetime obligations before publishing them. The existing v1
`Service` keeps its original journaled lookup/open pin implementation. No
`LogicalPlanner` constructor or handle map supplies that protocol integration
or opens a native/guest mount.

The 128 KiB snapshot default is a conservative component profile, not a reduction
of the existing 4 MiB canary workload. The local `TestLogicalFourMiBProfileRAMPlanningAndRecoveryBeforeBacking`
uses an explicit 6 MiB snapshot/8 MiB claim profile with sufficient separate
ordinary and recovery reservations. It covers a real 4 MiB logical payload,
ordered remote-origin writes, a patch crossing its write-chunk boundary,
subsequent RAM reads/append planning and serialized restoration while owner
backing remains unchanged. A runtime canary must explicitly admit its full
payload/catalog/claim/version/recovery profile and retain the separate native
and retained-guest platform gates. This component test is not infrastructure
calibration, a latency target or deployment admission.
