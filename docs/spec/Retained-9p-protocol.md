# Negotiated retained-9p bridge 1.0

Task `rz3`, design only. This freezes the first bounded bridge representation for
the [selected guest trust boundary](../lab/Retained-9p-authorization.md).
The host component in [`internal/guestbridge`](../../internal/guestbridge/README.md)
now implements framing, private raw FUSE callbacks and delegated dispatch seams.
Its disposable and normal execution providers, installed integration, physical
cache barriers and normal publication remain separate gated work. This schema
alone supplies none of those runtime facts. The
[compatibility design](../lab/Retained-9p-compatibility.md) supplies the required
integration points and runtime gates; [View barriers](View-barriers.md) owns the
STOPPED/drain/DRAINED semantics. Existing probe evidence keeps its original scope.

Keep Linux kernel 9p → virtio → owned bhyve/lib9p backend → **actual FreeBSD
HomelabFS FUSE connection** → shared service. There is no guest HfsRPC client or
Linux FUSE replacement. Maintain the guest/backend changes as explicitly owned,
pinned patch series and build inputs; editing an incidental vendor checkout is
not a release artifact. Native FUSE header credentials remain those of the
actual exporter. Delegated guest credentials are a separate assertion.

## Negotiation and limits

All numbers below are assigned within this new version, not stock 9P2000.L.
Normal Tversion/Rversion (100/101, NOTAG=65535) selects exactly the ASCII string
`9P2000.L.hfs1`; returning `9P2000.L`, `unknown`, another version or another msize
does not admit this profile. msize is exactly 65536. After version selection only
Tbridge=240 and Rbridge=241 are permitted, using the ordinary 9p little-endian
`size:u32,type:u8,tag:u16` seven-byte header. Size includes that header. No stock
attach/walk/open/read/write operation can bypass the bridge. These private
numbers claim no allocation in another protocol version.

Tbridge payload is `method:u8,channel:u8,cell:u64,chunk:u32`, little endian,
followed by an H9B frame only for SEND=1. Channel is COMMAND=1, RESULT=2,
RECALL=3. SEND is valid only for COMMAND; cell/chunk must match its H9B header.
FETCH=2 has no trailing bytes and selects one RESULT/RECALL cell/chunk. Rbridge
is `status:u8` followed by: nothing for COPIED=1 or AGAIN=2; one complete H9B
frame for FRAME=3; reason:u16 for ERROR=4 (INVALID=1,SCOPE=2,CAPACITY=3,
RETIRED_CELL=4). SEND returns COPIED or ERROR; FETCH returns FRAME/AGAIN/ERROR.
Size includes the seven-byte 9p header and must be exact. No concatenated frames,
trailing bytes or partial chunks are permitted. The selected H9B cell/chunk and
kind must match FETCH. COPIED is transport receipt, never filesystem success.
A SEND ERROR/CAPACITY definitively refuses **that packet** without copying its
chunk. Retry the identical packet at any chunk index, retaining earlier copied
chunks, the same cells and immutable claim/intent bytes. Do not advance the
chunk or restart/reclassify the claim. This packet refusal says nothing about
semantic no-effect for an earlier admitted claim. A complete command already
owned by the receiver remains COPIED while its native forwarding is capacity
blocked; later FETCH exchanges retry/query that retained mapping. FETCH AGAIN
is bounded readiness backpressure, not an empty or terminal filesystem result.

Responses echo the 9p tag, but tags may be reused only after that response;
they never identify an operation. Tag 65534 is reserved for VIEW_WAIT's
SEND/FETCH loop and 65533 for drain/control exchanges; ordinary traffic uses
0..65532. Large messages use H9B chunks under independently completed tags.
The native FUSE path carries the H9B frame alone at its positional offset.

Both guest↔backend and backend↔FUSE perform HELLO before any capability, attach
or workload request. Every member of the publication manifest must select bridge
1.0, body schema 1, ordinary normal format 3.0, and **exactly** feature mask
`0x000000ff`: bit0 current credentials/groups1024, bit1 scoped capabilities,
bit2 immutable claims/results, bit3 atomic metadata07777, bit4 stopped drains,
bit5 exact install acknowledgements, bit6 original-origin binding, bit7 positional
control cells. Missing or unknown bits/versions, mixed peers, stock endpoints and
unreconciled authority fail closed. A peer cannot negotiate the intersection and
silently omit a required feature. No downgrade/fallback path exists.

| Bound | Bridge 1.0 value |
| --- | ---: |
| Complete canonical message body | 1048576 bytes |
| Chunk body | 32768 bytes |
| Incomplete messages per session | 16 |
| Active native command/result cells per endpoint, aggregate across sessions | 64 ordinary + 16 reserved control/drain |
| Dedicated acknowledgement/replacement parser workspace per endpoint | one, 12 MiB total including bytes, decoded state and reply scratch |
| Guest supplementary groups | 1024, complete, ascending, unique |
| Path components / total component bytes | 256 / 16384 |
| Component / symlink / I/O bytes | 255 / 4096 / 65536 |
| Closure members / inventoried domains / dirty entries per recall | 64 / 64 / 1024 |
| Capability/path-ancestor entries per message | 256 |

Reserve the complete body, immutable reply, retained-result, parser and drain
resources before admission; declared lengths alone never allocate unbounded
memory. Per-page packages must already exist before writable PTE installation.
Additional aggregate memory/journal/source/owner-RAM budgets are mandatory
admission inputs, not consequences of these per-message ceilings. Exhaustion
backpressures before admission/dirtying and never evicts unresolved evidence.

## H9B framing and canonical bodies

Every chunk has this **88-byte**, little-endian header. There is no native struct
padding. UUIDs are raw 16-byte nonzero identities, never host integer layout.

| Offset | Bytes | Field |
| --- | ---: | --- |
| 0 | 8 | ASCII `HFS9BR1` then NUL |
| 8 | 2 | major=1 |
| 10 | 2 | minor=0 |
| 12 | 2 | kind: COMMAND=1, RESULT=2, RECALL=3 |
| 14 | 2 | guest leg: flags=0; native leg: credit/reclaim flags below |
| 16 | 4 | complete body length, 1..1048576 |
| 20 | 4 | chunk offset in complete body |
| 24 | 4 | chunk length, 1..32768 |
| 28 | 4 | reserved=0 |
| 32 | 16 | live bridge session UUID |
| 48 | 8 | message cell, 1..2147483647: guest-session local on 9p; endpoint-global on FUSE |
| 56 | 32 | SHA-256 of complete canonical body |

On the **guest leg** flags remain zero. On the **native leg**, bit0 selects
control/drain credit (zero selects ordinary credit), and bit1 says the complete
command has nonnull reclaim. Other bits fail. The authenticated backend derives
these bits from its complete owned guest command and exact retained reclaim
mapping **before the first native chunk**. They remain identical across every
chunk. Native RESULT/RECALL echoes the issued command flags; the backend verifies
them against its retained mapping and clears them for the guest response.
Ordinary CALL and HELLO use ordinary credit; DRAIN_WRITE and other control
operations use control/drain credit. RELEASE_CELL inherits its acknowledged
target's credit class. Reclaim selects the independent fixed workspace, never
an 81st cell. The host validates both bits against the entire parsed command,
current scope and exact donor snapshot before effects or atomic credit transfer.
This explicit native metadata is required because canonical CALL args can place
opcode after a large data field, beyond the first chunk. Header flags cannot
authenticate the caller or supply normal publication authority.

The body immediately follows the header. Offsets are multiples of 32768;
length is min(32768,total-offset), and offset is strictly below total. Chunk
order is arbitrary, duplicate identical chunks are harmless; overlap, changed
bytes/length/hash at the same message cell, unexpected kind or scope is rejected.
No semantic command executes until every byte is present, its hash matches and
the complete body passes validation. Cancellation of incomplete assembly has no
effect, but cannot free an already admitted claim. SHA-256 detects identity
conflicts; it does not authenticate an untrusted guest or exporter.

Bodies are canonical ASCII JSON: dictionaries recursively sorted by ASCII key,
no insignificant whitespace, duplicate keys, unknown fields, floats, exponent
numbers, NaN, negative zero or trailing bytes. Integers use shortest decimal
spelling. Strings use ordinary printable ASCII, `\"`/`\\` escapes only; bytes are
lowercase even-length hex strings. There is no Unicode normalization or text
filename conversion. UUID strings are canonical lowercase nonzero UUIDs in 8-4-4-4-12 hexadecimal
group spelling; hashes
and opaque capability tokens are exactly 64 lowercase hex digits. Booleans are
JSON booleans, optional values are explicit null, never missing keys. A field
table is closed: it lists **all** fields, and range/type/union violations fail.
Digest equality is followed by comparison of retained exact canonical bytes.

`u32` means integer 0..4294967295, `u64` means 0..18446744073709551615;
`n64` is u64 excluding zero. Offsets/sizes are 0..9223372036854775807.
`bytes(N)` is at most N decoded bytes. `Time={seconds:i64,nanos:u32}` requires
nanos<1000000000. Reserved IDs, all-zero UUIDs and unmapped numeric IDs fail.

## Private FUSE inode transport

Reserve `.homelabfs-control-v1/{command,result,recall}` as protocol-only synthetic
inodes, not journalled workload entries. Do not expose their names, node IDs,
qids, xattrs, root capabilities or walk/readdir results to any guest. These names
are reserved against workload creation. Open authenticates the supervised exact
exporter process incarnation, credentials, real FUSE session and enrollment,
beyond 0600 ownership. Authenticate every native operation again; native
UID/GID/PID/groups must equal the actual exporter snapshot, never guest values.
Only the new delegated entry interprets the separately attested guest context.
No raw-backing path, plain directory or generic POSIX backend fallback exists.

The native **endpoint** is the enrolled tuple `(real fuse_session UUID, command
node ID, result node ID, recall node ID)` on one live FUSE connection. Its inode
set and allocator lifetime belong to the daemon's supervised endpoint enrollment,
not to a guest session, export label, PID, application fd or reused native FUSE
handle. All guest sessions sharing that inode set share one native cell namespace,
including exporter/guest/session replacements. Reopening a control fd does not
create another endpoint or reset the namespace. The backend must verify that
same endpoint identity before every read/write and must never fall through to a
replacement mount or ordinary directory. Its retained schema is
`Endpoint={fuse_session:uuid,command_node:n64,result_node:n64,recall_node:n64}`;
node IDs come from that authenticated FUSE connection, never guest assertions.

Open selects direct I/O, no mmap/append/cache; advertise virtual size **2^52** on
each inode. Native cell c is 1..2147483647, monotonically allocated and **never
reused during that endpoint lifetime**, across all bridge sessions and exports.
Cell0 is invalid. Cell stride S=2097152. H9B chunk j begins at positional offset
`c*S + j*(88+32768)`. Its write is exactly one header plus that chunk's body. A
complete body fits within its cell; padding has no messages. The header chunk
offset is j*32768. Native H9B/header/body cell c and positional cell must match.
The inode plus offset selects the native response without needing its session
header first; the returned session/header are additionally checked against the
forwarding ledger. They cannot resolve a collision after the read.

One supervised backend allocator/framing writer owns the endpoint-wide native
high-water mark and forwarding ledger. Before a new cell's first write it owns
an immutable mapping from `(endpoint,guest session,guest cell)` to native c and
exact request bytes. First chunks enter FUSE in allocation order. The daemon
accepts a new cell only at its own next high-water mark, records its session and
first-chunk identity before acknowledging transport receipt, and reserves its
aggregate resources. A credit-replacement candidate is the explicit exception:
its chunks remain unissued in the fixed workspace below until complete validation
and atomic target-credit transfer; copying a candidate chunk grants no cell. Later chunks/cells can progress concurrently. A lost receipt
retries that existing cell's exact chunk; a conflicting allocation, nonconsecutive
new cell or session change at an issued cell fails. A second allocator cannot
reset or independently reuse the counter. Backend/daemon high-water marks,
issued/released floor, unresolved holes and exact mapping/response state must
reconcile before another writer/exporter/session is admitted. Missing continuity
blocks; a fresh UUID does not supply it.

Guest-leg cells remain local to each guest bridge session and may both be 1 in
two concurrent sessions. The backend gives them distinct native cells, rewrites
only the transport body's/header's cell, sets native flags as above and recomputes framing hashes, retaining
both exact bodies. Responses reverse that mapping. A retained forwarding entry
is exactly `{endpoint:Endpoint,guest_session:uuid,guest_cell:n64,native_cell:n64,
request_guest_hash:hash,request_native_hash:hash,response_guest_hash:hash|null,
response_native_hash:hash|null,native_flags:u32(0..3),state:ASSEMBLING|ADMITTED|RESPONDED|RELEASED}`;
associated exact byte snapshots remain owned until protected release. Each native
cell also retains its donated credit class (ordinary or control/drain). Every
ordinary entry has matching forward `(endpoint,guest_session,guest_cell)` and
reverse `(endpoint,native_cell)` keys. VIEW_WAIT's forward key owns an ordered
list of at most 16 such native bindings; each distinct native reverse key also
identifies its list position. Responses can never use an unrecorded reverse
lookup or a native-cell number supplied by the guest. Claim/intent/credential,
Context and result identities are unchanged. VIEW_WAIT
is the already specified guest operation translated to native VIEW_POLL;
transport rewriting never changes an admitted filesystem intent. RELEASE_CELL
additionally translates
its target guest cell through that **same session's** mapping; it cannot name a
native cell directly. VIEW_WAIT's bounded VIEW_POLLs each get distinct native
cells, at most 16 per guest wait. Retain their ordered mappings until the guest
snapshot and every native snapshot are acknowledged. No guest request, FETCH,
release or native response may proceed without the exact corresponding mapping. The
backend translates reclaim.cell/hash with the same scope rules as RELEASE_CELL.

Command accepts only positional Write; reads fail EACCES. A write copies,
validates and enqueues within reserved capacity; it never waits for work, a
recall, a grant or another vnode. A short/error write has no semantic success:
retry its exact bytes/cell, then query the original claim if admission is
uncertain. Native write completion is transport completion only.

Result and recall accept only positional Read; writes fail EACCES. Reads at
`c*S+j*(88+32768)+k` select exact immutable response chunk bytes starting at k.
Short native reads are allowed: collect the 88-byte header first, validate it,
then collect exactly chunk-length bytes using successive preads; bound each
pread to that chunk, at most 32768 bytes. Nonfinal inter-chunk padding returns
EINVAL. An absent cell or chunk returns EAGAIN, including before assembly/response
creation; it never
blocks. An in-cell offset at or beyond a completed final chunk returns zero
(EOF); EOF before the declared chunk end is a transport fault. Virtual inode
size prevents ordinary kernel size checks from manufacturing EOF at offset zero.
Reads at or beyond 2^52 return EOF; negative/overflow/cell0 offsets fail EINVAL.
Native kernel read splitting, EOF and size behavior remain actual FreeBSD proof
gates; this arithmetic is not that proof.

Each complete command yields exactly one immutable response snapshot in its cell
on **result**, except VIEW_POLL on **recall**. CALL yields ADMITTED or REJECTED;
terminal status is fetched with a fresh QUERY cell, whose snapshot can be
PENDING/INDETERMINATE/TERMINAL/RETIRED_ID. An old PENDING snapshot never changes
under a multipart reader. Poll another fresh cell to learn later status.
RELEASE_CELL acknowledges an exact response hash and permits freeing its bytes;
it does not make that native cell available for allocation. The daemon verifies
that the target's enrolled endpoint/session and native response hash match the
release command. On the guest leg the backend first verifies the guest response
hash, then maps it to the retained native snapshot/hash. Never translate by
matching the numeric guest/native cell alone. Reads of released native cells
return ESTALE, while another session's distinct cell remains unchanged. An
endpoint-wide issued-cell high-water mark plus released floor/unresolved holes
prevents reuse after payload bytes are freed. RELEASE_CELL itself has a response;
its acknowledgement can be combined with the next release, leaving one final
cell until protected session retirement.

The 64 ordinary/16 reserved active-cell credits are **aggregate per endpoint**,
not multiplied by the number of guest sessions; fairly schedule admitted domains
without consuming recall/drain reserve for ordinary work.

**Full-capacity release progress:** enrollment also reserves exactly one 12 MiB
acknowledgement/replacement workspace and an independent bounded parser/worker
path per endpoint. Its total includes complete request bytes, bounded decoded
objects and reply scratch; it cannot retain an 81st active command/result cell.
Unacknowledged ordinary/VIEW_POLL/drain queues cannot occupy that workspace or
its worker. Only a candidate carrying an exact `reclaim` acknowledgement uses it,
even when all 80 cell credits hold responses. The backend already owns the complete canonical request and
serializes its bounded chunk writes; it cannot hold this workspace while waiting
for a guest, vnode, grant, drain or service credit. An unacknowledged command
blocked on ordinary capacity cannot reserve the next native number or framing
writer ahead of this path. Failed other-resource admission returns immediately
or rejects transport before admission, never waits while holding the workspace.

In that workspace validate the entire schema/current scope and exact existing
fully received snapshot/session/hash, nonreuse/next candidate number, and all
new command/response resource reservations. Then atomically mark the target
snapshot released, transfer **its one existing cell credit** to the new command
and advance the native high-water mark/install the forwarding entry. Its exact
response uses that donated credit. RELEASE_CELL inherits the target's credit
class; another command must match its designated ordinary/control class, so an
ordinary CALL cannot steal drain reserve. No intervening state needs a free 81st
credit. Until this validation succeeds, fragmented bytes/candidate metadata live
only in the fixed workspace, not in the issued-cell ledger. Invalid acknowledgements
leave the target, high-water mark, mappings and credits unchanged and return a
transport refusal. Retry uncertain attempts with identical candidate bytes until
admission or definitive refusal is reconciled; a lost write result cannot justify
reusing an issued cell for another command.

RELEASE_CELL uses its own payload as this same acknowledgement, not two releases;
its `reclaim` must equal that payload. At 80 occupied cells, read an ordinary
snapshot, atomically replace it with a new RELEASE_CELL response (still 80), then
acknowledge that response in the next CALL/QUERY's `reclaim` and atomically
replace it with that new command (still 80). The old offsets remain ESTALE and
the new native cells strictly increase. This provides bounded progress even
when all 16 control credits hold VIEW_POLL responses. Other snapshot/claim/domain
obligations remain retained; only the acknowledged transport snapshot is freed.

Exhausting a guest's 31-bit cell space can use a gated reconciled guest-session replacement, whose
new guest cell1 maps to the next unused native cell. Old native cells/mappings
remain retained until their exact snapshot acknowledgement and protected
obligations permit retirement. Replacing a guest or exporter never resets the
native allocator. Exhausting the endpoint's 31-bit space blocks new admission:
only a separately authorized, drained/reconciled destruction of that old FUSE
endpoint and admission of a distinct real FUSE connection/inode set creates a
fresh native namespace. Old descriptors cannot address the new endpoint; old
claims/results and cache-domain obligations still need recovery, not deletion.

VIEW_WAIT is a guest logical long-lived request using its reserved 9p tag for
sequential SEND/FETCH. The backend may keep its result pending while polling
**nonblocking** FUSE VIEW_POLL cells with bounded backoff; the immutable response
is one exact event or EMPTY on its fixed 1000ms wait deadline. EMPTY is
not an acknowledgement. Repost before acknowledging an event. A recall's event
identity/bytes remain retained independently of polls until exact acknowledgement.
Within **one** logical VIEW_WAIT, the backend may transport-acknowledge a fully
received EMPTY native snapshot by putting its exact same-session cell/hash in
the next generated VIEW_POLL's reclaim. That atomically donates the existing
control credit; it neither acknowledges a recall event nor asserts VIEW_ACK.
Retain the ordered request/response mappings and acknowledged EMPTY tombstones
(up to 16 polls) until the guest's final immutable snapshot is acknowledged.
The guest's RELEASE_CELL/reclaim then translates to the final native snapshot;
earlier EMPTY snapshots have already been transport-acknowledged in that chain.
Never continue/reclaim automatically after EVENT. The 1000ms deadline cannot
manufacture EMPTY while a native poll is unknown or has no complete response;
retain that exact pending mapping until a real EMPTY/EVENT snapshot is received.
Reserve independent FUSE/parser/backend/drain workers and queue credits. Distinct
inodes and nonblocking reads avoid a result/recall vnode lock blocking command
write/drain completion. Do not rely on a FreeBSD FUSE ioctl callback or a unique
FUSE handle per application fd: the kernel may reuse a native vnode/caller handle.
Protocol cells, guest description capabilities and session identities do that work.

## Closed common and capability schemas

Every command body is `{cell:n64,context:Context,op:Operation,payload:Payload,
reclaim:Ack|null}`, where `Ack={cell:n64,response_hash:hash}`. Nonnull reclaim
acknowledges one fully received same-session response and requests atomic credit
replacement as above. It is guest-local on 9p and translated to the exact native
cell/hash by the retained forwarding ledger; unknown mappings never translate.
Every response is `{cell:n64,context:Context,op:Operation,payload:Payload}` with
the command's cell/context/op, even on rejection. Operation is exactly HELLO,
CALL, QUERY, CANCEL, RETIRE, RELEASE_CELL, RESUME, VIEW_WAIT, VIEW_POLL or VIEW_ACK.
VIEW_WAIT is guest/backend only; VIEW_POLL is backend/FUSE only. All other
operations cross both legs, with guest-session cells translated to unique
endpoint-wide native cells by the backend forwarding ledger above. Private RESULT/RECALL bodies never accept incoming commands.
Malformed framing/schema fails transport before operation admission; operation
responses below describe only schema-valid requests. Context contains exactly:

```
authority_hash:hash, cluster:uuid, configuration:uuid, export:uuid,
exporter_incarnation:uuid, fuse_session:uuid, generation:uuid, guest_boot:uuid,
guest_mount:uuid, origin_incarnation:uuid, origin_journal:uuid, origin_node:uuid,
policy_hash:hash, registry_hash:hash, session:uuid, vm:uuid, volume:uuid
```

All values match authenticated enrollment/installed authority. HELLO uses its
fresh proposed context only after the native exporter binding is authenticated;
its admission does not establish serving readiness. VM/export/guest identities
are not inferred from guest-provided display names or addresses. One bridge
session is bound to one volume/export/mount/domain. Different guests are
independently writable admitted domains; no permanent single-writer rule applies.

`Cred` is exactly `{fsuid:u32,fsgid:u32,groups:[u32],process:uuid,
credential_revision:n64,idmap_hash:hash,user_namespace:uuid}`. IDs are mapped
initial-user-namespace numbers; the pinned admitted mapping must cover all IDs.
Capture a complete immutable credential snapshot in the VFS authorization
context before asynchronous dispatch. Changes to fsuid/fsgid/groups increment
the process credential revision; simultaneous same-UID processes remain distinct.
Out-of-order already captured revisions are not overwritten by a newer request;
the same process/revision cannot name different bytes. New path authorization
uses the current captured revision, never an attach fid or host-account groups.
Retain guest capability/LSM checks; host Unix denial wins. Guest root/kernel and
the trusted exporter can forge in-export identities, as explicitly selected;
the host does not claim per-user isolation against that attacker.

`Claim` is exactly `{realm:uuid,subject:bytes(1024),anchor:uuid,nonce:uuid,
number:n64,original_hash:hash}`; subject is nonempty. It maps to stable
Principal(Realm,Subject), SessionID(Principal,Anchor,Nonce), request Number, plus
exact immutable original bytes. Resolve this anchor **before** name lookup,
owner choice or origin choice. An ordinary syscall receives one kernel-owned
claim before dispatch; its number is never reassigned. Reconnect/reroute carries
that claim; a fresh syscall is not an exactly-once retry.

`Cap` is exactly `{token:hash,kind:Kind,scope:Context,object_home:uuid,
object_allocation:uuid,description:uuid|null,rights:u32,revision:n64,
credential_hash:hash,ancestors_hash:hash}`. Kinds are ROOT,PATH,DESCRIPTION,DOMAIN.
The service keeps the matching issuance ledger (or verifies an equivalent scoped
authenticator) and compares the whole grant; token is unpredictable, never a
guest allocation or native FUSE fh. Rights bits are read1/write2/search4/namespace8/
metadata16/sync32; unknown bits fail. DESCRIPTION requires nonnull description;
other kinds require null. Object IDs remain home+allocation. Scope must match
the command context exactly. Alias/view ancestors are a separately retained
ordered list of `(object ID,object revision,entry revision,raw component)` whose
canonical hash is ancestors_hash. A root has the empty list. Revalidate cached
search/path capabilities and current logical metadata at the captured committed
cut; possession is not timeless pathname permission. DESCRIPTION grants retain
issued rights across chmod/unlink, not across generation/session revocation.
dup/fork/transfer retain the description; final reference retirement closes it.
Delayed mapped writes retain that exact grant and per-page provenance, never a
root writeback fid's rights. Process-exit/close are sequenced kernel assertions,
not inferred from host close, missing PID or a reused fid.

Each issued PATH token owns one reference. `FORGET` closes that exact token with
`count=1`; it never aggregates other tokens for the same object. A LOOKUP or
CREATE/MKDIR/SYMLINK path owns its own filesystem lookup pin. A READDIR entry is
a transient path grant without such a pin: its protected FORGET closes only the
bridge authority. Forget verifies the full live scoped grant and current
credentials, but does not rewalk an ancestor that may already have been removed.
It cannot close DESCRIPTION, DOMAIN or ROOT. After PROCESS_EXIT, only the exact
retained original credential bytes/revision whose hash bound the still-live
grant may submit FORGET or final DESCRIPTION_RELEASE as cleanup. The full grant
and scope must match; a changed credential, unknown/released token or ordinary
path/data/issuance request remains refused. This exception only reduces existing
authority and never removes the exited marker or revives a process. Existing
original terminal claims remain replayable. Inode aliases retain each issued
path token separately until their last local reference disappears. READDIR
consumers close every returned transient token, including entries not emitted
because a userspace buffer filled. Failure preserves the original result and
uncertain lifetime work; transport ACK alone never closes a grant.

Successful final DESCRIPTION_RELEASE closes its grant after the execution
provider has verified description/view/dirty/source obligations. DESCRIPTION_CLOSE
and PROCESS_EXIT do not themselves close descriptions that remain shared or have
retained mapped obligations. Released grants remain nonreused identity tombstones;
original terminal results and claim retirement evidence remain independently
protected. Live capability admission excludes those tombstones, while a separate
finite issuance identity budget still refuses further grants when exhausted.

## Operation and result unions

HELLO payload is `{body_schema:1,bridge_major:1,bridge_minor:0,features:255,
msize:65536,normal_major:3,normal_minor:0}`. Its response payload is
`{status:ACCEPTED|REJECTED,selection:<exact HELLO payload>}`; reject without
partial attach if any field differs. Successful HELLO attaches only the exact
enrolled export/domain; there is no guest-selected stock Tattach. Session
replacement uses HELLO followed by
RESUME, not an implicit new lifetime for pending work.

CALL payload is exactly `{claim:Claim,credentials:Cred,intent:Intent}`.
Intent is `{opcode:Opcode,args:Args}`. Claim.original_hash is SHA-256 of canonical
Intent bytes; the immutable admitted record also binds exact Cred bytes and the
initial Context. A replay with different credentials, args or payload is
ID_CONFLICT. Only authenticated forwarding can carry an existing original binding
through a new bridge/origin context; RESUME reconciles it before retry, retaining
the original Context/Cred/intent and original durable origin. Such a recognized
exact replay queries its existing binding before fresh capability validation;
it never executes old-scope capabilities as a new command. New operations need
new capabilities issued for their current context. Unauthenticated
cross-session claim presentation does not confer lookup authority.

In this table Cap means a complete capability as above; names are nonempty raw
bytes excluding NUL, slash, dot and dotdot; paths cannot escape via symlinks.
Every Args object has exactly the listed fields. File-type enum is REGULAR,
DIRECTORY or SYMLINK, separate from mode bits. Flags is
`{access:READ|WRITE|READWRITE,append:bool,create:bool,exclusive:bool,sync:ORDINARY|DATA|FILE,truncate:bool}`.

| Opcode | Exact Args fields |
| --- | --- |
| ROOT | (empty Args object); issue this enrolled volume's root identity capability |
| LOOKUP | parent:Cap, name:bytes(255) |
| GETATTR, READLINK | object:Cap |
| CREATE | parent:Cap, name:bytes(255), mode:u32, umask:u32, flags:Flags |
| MKDIR | parent:Cap, name:bytes(255), mode:u32, umask:u32 |
| SYMLINK | parent:Cap, name:bytes(255), target:bytes(4096) |
| OPEN | object:Cap, flags:Flags |
| READ | description:Cap, offset:size, count:u32 (<=65536) |
| WRITE | description:Cap, offset:size, data:bytes(65536), max_end:size |
| TRUNCATE | object:Cap, description:Cap|null, size:size, times:Times |
| SETATTR | object:Cap, attributes:Attributes |
| RENAME | source_parent:Cap, source_name:bytes(255), destination_parent:Cap, destination_name:bytes(255), no_replace:bool |
| LINK | object:Cap, parent:Cap, name:bytes(255) |
| UNLINK, RMDIR | parent:Cap, name:bytes(255) |
| READDIR | object:Cap, after:bytes(255), revision:u64, limit:u32 (1..256) |
| STATFS | root:Cap |
| SYNC | object:Cap, description:Cap|null, scope:DATA|FILE|DIRECTORY|VOLUME |
| DESCRIPTION_CLOSE | description:Cap, process:uuid, close_sequence:n64 |
| DESCRIPTION_RELEASE | description:Cap, release_sequence:n64 |
| FORGET | object:Cap, count:n64 |
| PROCESS_EXIT | process:uuid, exit_sequence:n64 |
| DRAIN_WRITE | description:Cap, offset:size, data:bytes(65536), recall:uuid, dirty_entry:hash, reservation:hash |

WRITE captures `max_end` in 0..S64MAX as the minimum of the submitting
process's RLIMIT_FSIZE, its non-LFS bound and the filesystem bound, before any
transport retry. This field and the original full data remain immutable claim
intent. Under the same all-origin serialization as publication, the host selects
the actual append offset from authoritative logical metadata, then caps the
permitted prefix to `max_end - offset`. It returns the short count, or the
delegated wire code `FileTooLarge` with no effects when no byte is permitted.
This code is outside the closed fsapi/v1 errno and journal-record enums.
DRAIN_WRITE retains its separately verified frozen dirty-page obligation.

`Times={atime:Time|null,mtime:Time|null,ctime:Time|null,atime_now:bool,
mtime_now:bool}`; explicit time and corresponding now are mutually exclusive.
`Attributes={mode:u32|null,uid:u32|null,gid:u32|null,atime:Time|null,
mtime:Time|null,atime_now:bool,mtime_now:bool}`; the union must select a field.
Modes are 0..4095 (07777) and umasks 0..511 (0777), never silently masked; semantic policy chooses
supported bits, sticky/set-ID clearing and authorization. CREATE/MKDIR are one
logical operation: final UID=creator fsuid, GID=logical parent GID unconditionally,
mode/umask/special-bit policy applied once. Return the authoritative committed
attributes; patched guest initialization cannot overwrite parent-GID inheritance.
No observable create-then-chown/chmod repair exists. Combined unsupported setattr
requests fail before effects. Unsupported operation values fail before admission.
Advisory lock behavior remains same guest/mount/kernel inode domain; no distributed
lock wire operation is added. Ordinary cross-mount LINK/RENAME returns CrossDevice;
future public multi-volume transactions need separately negotiated representation.

CALL response payload is `{status:ADMITTED|REJECTED,claim:Claim,code:Code}`.
ADMITTED uses OK and transfers immutable ownership, not success of the syscall.
REJECTED is before claim reservation and has no effects; after reservation all
failures resolve through the protected claim/result path and QUERY.
QUERY and CANCEL payloads are `{claim:Claim}`. QUERY response payload is
`{status:PENDING|INDETERMINATE|TERMINAL|RETIRED_ID,claim:Claim,result:Result|null}`;
only TERMINAL has Result. CANCEL response has the same shape: cancellation
detaches waiting only, or requests the ordinary protocol's protected durable
DECLINE before COMMIT. It never turns unknown into NO_EFFECT. INDETERMINATE
retains/gates the original choice; a timeout never fabricates a rejection.

`Result={id:hash,code:Code,count:u64,data:bytes(65536),attributes:Attr|null,
capabilities:[Cap],entries:[Entry],capacity:Capacity|null,publication:Publication|null}`.
id is SHA-256 of the canonical Result **with id removed**, including publication.
Publication.result_ref identifies the normal source result (semantic output),
not this bridge response wrapper; that source does not contain its own frame
reference or this wrapper hash, so the construction has no self-hash cycle.
Code is one of the exact case-sensitive strings OK, Invalid, NotFound, Exists,
Permission, ReadOnlyFS, NotDirectory, IsDirectory, NotEmpty, CrossDevice,
Unsupported, NoSpace, Quota, IO, Unavailable, Stale, Interrupted, Busy, WouldBlock,
TooManyLinks, NameTooLong (the semantic enum in `internal/fsapi/errno.go`), plus
bridge ID_CONFLICT, RETIRED_ID, PROTOCOL_ERROR and delegated FileTooLarge;
never native errno integers.
Count is exact successful bytes, not transport bytes; failed results carry zero
count, empty data/entries/capabilities and null attributes/capacity. An admitted
error does not imply rollback. Attr is `{object_home:uuid,object_allocation:uuid,
kind:REGULAR|DIRECTORY|SYMLINK,uid:u32,gid:u32,mode:u32,size:size,nlink:u64,
revision:n64,atime:Time,mtime:Time,ctime:Time}`. Entry is
`{name:bytes(255),capability:Cap,attributes:Attr}`; entries are sorted by raw name.
Capacity is `{objects:u64,objects_free:u64,bytes:u64,bytes_free:u64}` with free<=total.

RETIRE payload is `{realm:uuid,subject:bytes(1024),anchor:uuid,nonce:uuid,
through:n64,result_hashes:[hash]}`; response is `{status:RETIRED|BLOCKED,floor:u64,
proof_hash:hash|null}`. A protected permanent floor/tombstone and exact terminal
results are required, never a TTL. result_hashes lists one result per number
from the previous floor+1 through through, ascending, at most 1024 per request;
allocated-number gaps need protected no-effect tombstones. Pending claims prevent
passing their numbers. proof_hash is nonnull only for RETIRED.
Cell release, clunk, process exit, descriptor close and transport disconnect do
not retire claims. RELEASE_CELL payload is Ack and must exactly equal reclaim;
response is `{status:RELEASED}`. Here cell is guest-local on the guest leg and
endpoint-global on the native leg; the backend translates both target cell and
response_hash through its exact retained forwarding entry. Release only a fully
received response from the same live session; never release this command's own
cell, another session's target or an incomplete cell. Replacement sessions cannot
use RELEASE_CELL to adopt old-session cells; protected normal recovery/session
retirement handles those obligations separately.
RESUME payload is
`{previous_session:uuid,ledger_hash:hash,claims_hash:hash,domains_hash:hash}`;
response is `{status:RECONCILED|BLOCKED,ledger_hash:hash}`. The hashes identify
exact recovery manifests already reconstructed under the separately versioned
normal recovery/control protocol, not caller assertions of continuity. This
bridge version provides no arbitrary recovery-manifest upload operation. The
admission controller must install complete verified old cell/claim/domain ledgers
before RESUME, including the endpoint-wide allocator high-water mark, released
floor, unresolved native-cell holes and every retained guest↔native mapping.
A replacement session continues the same native allocator after reconciliation;
it cannot reset it or discard another session's mappings. Hashes alone do not
establish continuity or cover omitted entries.
Missing ledger bytes, old-domain exclusion where a lifetime was lost, or fresh
new-session acknowledgements keep BLOCKED. A continuously surviving stopped domain
can reconcile/re-ack without a physical fence; restart never claims that continuity.

## Exact mapping to all-origin authority and sources

`Publication={authority_hash:hash,sequencer_node:uuid,sequencer_incarnation:uuid,
serial:u64,selection_hash:hash,result_ref:NormalRef|null,source_manifest_hash:hash,
visibility_generation:uuid}`. NormalRef is exactly
`{cluster:uuid,volume:uuid,generation:uuid,configuration:uuid,epoch:n64,
origin_node:uuid,journal_incarnation:uuid,sequence:n64,frame_hash:hash,
creator_node:uuid,creator_incarnation:uuid,kind:u32}`. These names map one-for-one
to normal3 OriginStream, local Sequence, Hash, Creator Launch and record Kind;
use the normal3 closed kind set, not a v1 ref or bridge cell/tag as a substitute.
Authority hash identifies the exact canonical installed normal3 authority payload.
Serial zero denotes an observation of the verified baseline, with its exact
baseline hash in selection_hash and null result_ref; it is not a selected slot.
Nonzero serial names an exact selected certificate and requires result_ref for
a protected operation result. A read/lifetime-only observation may use null
result_ref and still names its exact committed cut/source manifest.
Bridge generation is the per-volume normal recovery generation; live frontend
visibility_generation is separate. Configuration, registry/policy hashes and
sequencer launch must match that authority. Epoch is obtained from its volume
member, never guessed from the bridge version or serial.

origin_node/incarnation/journal in the initial admitted Context maps to O's
Launch and OriginStream. It can be any admitted normal node, including outside
replica placement. The backend is an authenticated forwarder, not implicitly A
or a replica; VM UUID is not O's node ID. The ordinary syscall's immutable
operation UUID is allocated once and retained alongside the anchored claim.
Normal3 grant document/intent/credential/redo must agree exactly with that
binding. A grants/orders, O durably owns its complete original fragment/result,
M=A for O!=A and M=B for O=A. Owner complete logical RAM visibility, source pins,
distinct-host retention and original durable-origin identity survive retries
through another exporter/origin. An O2 retry queries O1's binding; it cannot
mint an O2 fragment as a second choice or route every write to A's disk.

**Representation frontier:** [normal3 3.0](Normal-format.md) admits internal
ordinary single-home documents only. Its current codec does not encode the
public anchored bridge binding, delegated provenance, capabilities or frontend
barrier evidence in this document. The future integration needs a separately
versioned binding/proof representation and verified mapping before serving;
it must not put public requests in the internal union, extend closed3.0 kinds
silently or relax v1 owner/reporter rules. Ordinary canonical intent/credentials
can reuse their normal3 schemas only where the semantics match. The reference
checker below checks bridge examples, not that missing journal/protocol work.

## Recall, stopped drains and install unions

The DOMAIN capability identifies one inventoried guest mount/kernel inode/cache
domain; native reader domains have their own bindings and remain in the same
closure inventory. Delivery uses VIEW_WAIT guest requests and VIEW_POLL native
commands. Their payload is `{domain:Cap,after_sequence:u64}`. The response is
`{status:EMPTY|EVENT,event:Event|null}` with Event only for EVENT.

`Event={sequence:n64,recall:uuid,phase:RECALL|INSTALL_DRAIN|INSTALL_FINAL|REOPEN,
domain:Cap,authority_hash:hash,closure_hash:hash,inventory_hash:hash,
old_visibility:uuid,target:Publication|null,manifest_hash:hash|null}`.
Sequences strictly increase per domain; duplicates replay the same bytes.
RECALL uses null target/manifest_hash and identifies the exact frozen authority
and complete domain inventory,
not an already reserved final mutating slot. INSTALL phases carry the selected
protected target; INSTALL_DRAIN is allowed while stopped before DRAINED.
INSTALL/REOPEN use the frozen manifest_hash; REOPEN names the installed final
target. No disconnect is implicit delivery.

VIEW_ACK payload is `{domain:Cap,sequence:n64,recall:uuid,
phase:STOPPED|DRAINED|INSTALLED_DRAIN|INSTALLED_FINAL|REOPENED,
authority_hash:hash,closure_hash:hash,inventory_hash:hash,session:uuid,
manifest:Manifest|null,target:Publication|null}`; response is
`{status:ACCEPTED|STALE|BLOCKED}`. It must match the live Context/session, event,
domain, phase and exact hashes/cut. Manifest is mandatory for STOPPED/DRAINED,
target for installation/reopen. STOPPED uses null target; DRAINED uses the last
intermediate installed target, or null when its manifest needed no drains. The
other acknowledgement phases use null manifest. Manifest is
`{hash:hash,entries:[DirtyEntry]}`; hash is SHA-256 of canonical entries.
DirtyEntry is `{id:hash,description:Cap,offset:size,length:u32,payload_hash:hash,
claim:Claim,reservation:hash}` with length 1..65536. Entries sort by id, with no
duplicate provenance IDs; id hashes the entry with **id and claim removed**.
The claim intent includes that independently computed dirty_entry ID, avoiding
a hash cycle. Overlapping byte ranges retain distinct exact provenance; merges
are allowed only if they preserve each original grant and reserved obligation.
STOPPED and DRAINED name byte-identical frozen manifests. Empty is explicit.
The coordinator owns complete manifest bytes, not only a hash. Large manifests
must fit the message bound; admission throttles before PTEs, not after recall.

Close new conflicting observations/dirtying and token acquisition **before**
reserving the final grant. Guest gates faults/maps/writeback/splice and drains
already admitted reads, revokes writable PTEs, completes physical PTE/TLB/MMU and
busy/pinned alias quiescence, then freezes dirty bytes/provenance and emits
STOPPED. A kernel unable to prove this stays recalling. Submit each frozen
entry as the reserved DRAIN_WRITE with its original description/claim and exact
bytes; it follows O-durable/M-RAM protection. Install each intermediate protected
cut in all stopped domains and return its drain result **without waiting for
DRAINED**. User observations stay closed. Only after all frozen entries resolve,
intermediate installs finish and cleanup/aliases are quiescent may DRAINED be
accepted. All inventoried domains need exact STOPPED+DRAINED before the final
grant. Protect/select that operation, obtain every INSTALLED_FINAL, then REOPEN
and return success. No backing-apply wait supplies the owner-RAM view barrier.

Duplicate acks do not advance another phase. Wrong recall/session/generation,
inventory, manifest, slot or target cannot acknowledge current work. Disconnect
invalidates learned session acks, retains stopped state, dirty packages/events
and claims, and requires fresh acknowledgements after reconciled RESUME.
Kernel/daemon restart additionally needs reconstruction/exclusion under
[Takeover](Takeover.md), not a fresh UUID or heartbeat timeout. A missing domain,
drain error, unresolved claim or lost retention gates the closure indefinitely;
it is not consent to drop a guest, force-stop a VM or narrow to one writer.

## Reviewable examples and remaining evidence

The [fixture corpus](retained9p-fixtures/cases.json) and
[reference checker](../../tools/spec/check_retained9p.py) deliberately cover the
common envelope, CREATE/WRITE delegation examples and boundary histories. Run
`python3 tools/spec/check_retained9p.py` from the repository root. It compares
literal golden frame hex/digests, rejects malformed canonical/frame/scope inputs,
and checks retained-claim replay, retirement and old-session acknowledgement
examples. Native-cell histories include two sessions sharing one endpoint with
guest cell1, distinct positional selection and release, retained old cells across
session replacement, and refusal of wrong scope, missing ledgers, allocation
reset or duplicate allocation. A separate 80-cell-full history checks atomic
release/next-command replacement and unchanged state on invalid full-capacity
acknowledgements. It is not a complete implementation of every operation union, Unix
policy, normal3 mapping, durable storage or kernel barrier. No fixture authenticates
a VM or proves an actual FUSE read, writable PTE or disk sync.

Examples include full-group (1024) acceptance/1025 rejection, creator UID and
parent-GID final metadata, preserved 07777, changed same-UID groups, immutable
payload replay, cross-volume cap denial, cancel/unknown retention and replayed
STOPPED/session/manifest rejection. Two differently scoped writable guest domains
are required in future runtime tests, together with native root observations
while the owner disk/apply is behind its committed RAM cut.

Before normal publication, separately prove exact owned Linux9p/lib9p patch and
installed/running-byte bindings; real native FUSE exporter credential equality;
private-inode nonexposure, direct-I/O size/EOF/chunk behavior and native handle
reuse; nonblocking drains under vnode/worker pressure; complete per-request
identity/groups and namespace mapping; atomic dynamic multiuser metadata;
description-transfer/page provenance; physical cache/mmap quiescence; all-origin
normal representation and original-origin durability; reconnect/restart/result
retirement and all-domain barriers on the actual stack. Stock limitations remain
historical evidence. Configuration and readiness stay fail closed. This schema
authorizes no install, mount, SSH, VM lifecycle, security enablement or lab run.


## Separately negotiated view-lease extension

[Bridge1.1 view leases and controlled cuts](Retained-9p-view-leases.md) defines
an exact schema2/features2047/minor1 extension. Its additional protocol envelope
key, resource tickets and tagged install targets are rejected by closed1.0;
there is no automatic downgrade. It preserves genuine normal Publication proof
and uses a separate verified CONTROLLED_LOCAL cut with null Result.Publication.
The foundation supplies codecs and mandatory controller seams, not a durable
token engine, physical guest barrier, recovery activation or runtime serving.

The separately selected controlled-only successor is specified in
[Retained-9p-controlled.md](Retained-9p-controlled.md). Its minor2/schema3
allocation/gap operations and response verification union do not extend either
closed1.0 or1.1 schema and supply no normal publication authority.

The separately selected controlled-only1.3 parent-funded READDIR cleanup tuple
is specified in [Retained-9p-cleanup.md](Retained-9p-cleanup.md). It preserves the
closed1.0/1.1/1.2 schemas and adds no normal publication authority.
