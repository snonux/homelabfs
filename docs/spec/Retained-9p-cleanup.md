# Retained-9p parent-funded directory cleanup 1.3

The controlled-only successor uses `9P2000.L.hfs1.3`, frame major1/minor3 and
exact tuple `{body_schema:4,bridge_major:1,bridge_minor:3,features:8191,
msize:65536,normal_major:3,normal_minor:0}`. Feature bit12 selects the complete
contract below. Controlled1.2 allocation, gap, full original retirement and
verification requirements remain mandatory. Closed1.0/1.1/1.2 do not accept this
tuple or READDIR_CLEANUP. No signed enrollment may be translated or downgraded;
this exact tuple remains pinned in the provider ledger and signed view domain.
This supplies component semantics, not native, distributed or normal authority.

READDIR still accepts limit1..256. Msize bounds each 9p frame; the existing
complete body bound is1MiB. Neither claim credit defaults (4MiB retained and
2MiB source) nor aggregate budgets (256MiB/128MiB) increase. Retirement does not
recycle these permanent charges. A full256 page therefore cannot require256
additional ordinary FORGET claims.

The new reserved control command READDIR_CLEANUP has exactly
`{capabilities:[Cap...],claim:Claim,credentials:Credentials,result_id:hash}`.
Claim is the exact original READDIR claim; credentials are its full original
issuer, including process incarnation, groups, namespace, idmap and revision.
The capabilities array contains every full PATH capability in original entry
order, including entries never emitted to the application. Empty pages use an
empty array. Result_id names the exact protected original result. Partial,
reordered, changed, wrong-issuer or wrong-parent requests refuse before changes.
An exited issuer may only close this exact original obligation; this command
cannot create credentials, authority, source mutations or ordinary claims.

Before applying or exposing the READDIR plan, its existing allocated credit
owns the exact cleanup request and immutable receipt. Both are included in the
real retained-byte admission calculation together with proposed result, plan,
outcome and bridge snapshot copies. An unfunded closure refuses before effect.
No per-entry cleanup claims, source credits or lookup pins are created.
READDIR entry grants already own no filesystem lookup pin; independently issued
LOOKUP grants, descriptions, dirty reservations and source pins remain separate.

Success is exactly
`{claim:Claim,request_hash:hash,result_id:hash,status:CLOSED}`. Request_hash is
SHA256 of the complete canonical cleanup payload. Exact retry, including after
reply loss or reopen, returns the same protected receipt. A terminal page
cannot retire before this cleanup is durably closed. Existing ascending exact
result/gap floor coverage still applies afterward; cleanup does not advance a
floor or assert source/physical observation closure.

The host serializes cleanup with private result preparation. It loads the
current protected session snapshot and its exact owner claim, verifies that
the owner's BridgeState bytes equal that snapshot, then changes only the
matching transient grant Released bits. All current metadata, independent
lookup/dirty/description/source ownership and identity records remain. A
false-to-true replacement cannot enlarge the canonical snapshot; the two
existing owner-funded copies are replaced within that owner's retained credit.
The new snapshot sequence, changed owner, parent closure flag, current snapshot
and idempotent receipt are protected in one transaction. Unknown owner or size
mismatch refuses. No old parent snapshot replaces current state, and old result
replay cannot restore a superseded snapshot. In-memory application changes only
the same targeted release bits, preserving newer unprotected credential metadata.
There is no session barrier that could block STOP, recall or drain progress.

The guest preallocates a complete cleanup payload in the parent request before
READDIR CALL, within its existing request reservation. It retains the parent
claim, original full credentials and result while processing the page. After
leaving physical observation, it sends the exact all-entry request using the
reserved control lane. It verifies the full receipt before adopting the parent;
borrowed result users still prevent ordinary retirement. Queue, memory, partial
reply or protocol failure leaves the original parent and cleanup bytes owned,
unadopted and available for inspection/reconciliation. No missing obligation is
inferred from timeout or transport acknowledgement.

The maintained C source includes the minor3 consumer branch. Actual selected
negotiation, forwarding and runtime receipt installation remain the separate
o04/runtime artifact gates. Primitive C transport/allocator substitutes and
real temporary host Service/provider checks do not establish native physical
observation, actual FUSE identity, kernel runtime or distributed acceptance.
