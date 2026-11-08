# Barriered component restart (C9)

`Supervisor.SuspendEndpoint` and its private, single-use `Restart` reopen
controlled local semantic components under the **same surviving native owner**.
This is a barriered component restart, including replay after a lost reply. It
is not recovery after losing the daemon/FUSE owner, exporter or guest kernel.
Guest-wire `RESUME`, nonzero-digest HSM1 CLAIM/RECONCILE, replacement exporters,
new FUSE connections and ordinary publication remain blocked.

The supported cut has no in-flight command, replacement workspace, pending C
forward, active filesystem handles/lookups/locks or unfinished recall. Every
retained response is complete and immutable. Sources must be reconstructible
from the actual current filesystem bytes and exact journal prefix; historical
cuts refuse even when their hashes or serialized snapshots are available. A
permanent terminal scope retains its exact final cell, original ordinary/control
charge, full donor manifest, mapping and all owner seals and fences.

The coordinator first pauses the actual supervised C backend, including SEND,
FETCH, response coverage and its poll worker. It closes host admission and joins
workers, then compares complete actual C and C8 ledgers: original/native bytes,
responses, protocol, endpoint, controller, counters, classes, coverage, mappings
and terminal ownership. Fully issued/responded exact rows can resolve the C
uncertain-write bit; pending or missing response evidence cannot. It captures
private cuts from the actual old provider, view controller, service and C8 owned
descriptors, then closes those owners. Copied files, caller-selected paths and
decoded controller identifiers supply no restoration authority.

Reopen creates new providers, controller, Facts, service, producer and bridge
from their original protected files. The private applied-store cut reads the
actual APPLIED/catalog, complete journal prefix and payload files into a new
store without replaying effects or changing workspace/generation. Original
Replay's fresh-workspace rule remains unchanged. Root inode inventories and
exclusive writer/final owned-descriptor checks reject replacement, missing or
changed files. Current sources are recaptured by the actual new service worker
and must match the original identity byte for byte. Fresh full admission and
physical observations cover every original domain, including terminal fences.
The original live Supervisor alone consumes the one-use assembly grant and
retains its actual controller incarnation. `Shell.ReplaceEndpoint` switches only
the exact old bound shell to its privately funded same-native successor. Only
then can that same C exporter resume. An error preserves a fenced owner and its
evidence; callers must close the nonnil partial owners returned with an error.

C8 adds complete per-scope bridge rows to the same transaction as each protected
transport transition, before reply exposure. They retain HELLO, credentials,
capability tombstones, close/exit counters and terminal seals. Older C8 files
without these complete rows remain inspectable inert evidence; restoration
refuses them. Snapshots cannot include unsupported EXECUTING cells at a restart
cut. Existing service result serialization protects capability effects before
plan protection and prevents peer mutation from becoming unprotected authority.

A READY runtime exposes this sequence only through its private
[lifecycle endpoint](lifecycle.md): RESTART names the exact server-issued
runtime/cut/epoch, calls these real methods on the retained owners, switches
the runtime's current owners only after Activate and otherwise keeps the
supervisor fenced with all partial owners retained. `Activate` uses the native
shell switch; local tests substitute only that FUSE dispatch primitive. An
armed, unfired lost-reply fixture is expired before suspension. A response whose
exposure was dropped and never re-read leaves its C row without a response, so
the paired exporter/C8 cut refuses until the exporter re-reads it.

## Private C recovery channel and bounds

Start passes one additional unnamed pinned AF_UNIX socket at fd5 to the same
child. HSR1 version1 uses the existing 16-byte framing shape and at most184
payload bytes, with exact typed PAUSE, DATA, BIND and RESUME exchanges. Data
chunks have an eight-byte offset and at most176 bytes of protected ledger data.
The channel has no guest listener/reconnect, accepts no descriptor rights, and
checks its actual peer process. It permits one recovery attempt. Active exchanges
have finite deadlines and the private grant expires; failure leaves forwarding
paused. The ordinary fd3 HSM1 reader and heartbeat continue independently.

The C recovery thread has a fixed128KiB stack. Its paused ledger staging uses
the existing at-most65MiB snapshot and protection buffers, separate from the
64MiB live backend allocation. The Go side streams at most16MiB into an owned
private staging file and parses bounded per-row blobs; it never allocates a frame or
payload of that size. Both the new barrier C snapshot and aggregate encoded C8 cut are capped at16MiB before retained decoding, and the actual mapped C8 file must not exceed768MiB; larger cuts refuse without changing normal transport capacity. The same aggregate task disk/RSS limits still apply.

C8 keeps its8MiB row,128KiB index,768MiB operating file ceiling and1GiB charged
ledger reservation. Complete bridge rows together are limited to8MiB; three
retained/encoded/staged copies spend at most24MiB of that reservation. Ordinary
credential and grant admission additionally reserves their bounded authority-
reducing closure: remaining close identities, maximum uint64 close/exit/release
counters, terminal seal and one maximum-sized terminal credential. Pre-effect
staged results charge their actual capabilities and ancestry plus every other
scope. Capacity refusal becomes a protected no-effect NoSpace plan before plan
protection; requested READDIR limits do not charge hypothetical maximum entries. Terminal-capable sessions reserve one
credential identity within their configured quota as well as its encoded bytes. Rejected identities are rolled back;
funded cleanup and the fenced terminal call can consume their reserved bytes.
The private service cut admits at most65536 inode-inventory entries with512-byte
paths and65536 native IDs. At most64MiB of conservative decoded cut copies,90MiB for two inventories,8MiB of moved IDs,24MiB of session copies and28MiB of parser/transaction workspace fit within the existing256MiB ledger headroom above768MiB. Source/catalog comparisons spend existing view WorkBytes; prior owner graphs and comparison payload
copies are dropped as their new owners are verified. Existing view WorkBytes
charges source/metadata comparison copies; no quota is silently enlarged.

## Evidence boundary

`restart_test.go` runs a real supervised C child/backend/ledger/HSR1 peer and
actual all-owner Go close/reopen. It exercises nonempty current sources,
original capabilities/controller/counters, lost mutating replies, unchanged
payload/journal effects, exact terminal full-set replay and fail-closed missing,
changed, stale, copied, unexcluded, unresolved and historical cuts. Only native
FUSE I/O/INIT and guest physical observer behavior are declared OS primitives in
these local tests. The shell switch has a separate in-package OS-boundary test.
Native mount/INIT/activation, installed kernel physical behavior, lab execution
and distributed failover remain unrun and unclaimed.
