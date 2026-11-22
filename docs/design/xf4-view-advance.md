# xf4: guest-requested view advance (design)

This design comes from the zq3-r18 and zq3-r19 findings:
- docs/lab/results/20261121-zq3-r18/README.md, R18-1 section 3;
- docs/lab/results/20261121-zq3-r19/README.md, R19-1 section 3.

## Problem

Every guest observation (a VIEW_ACQUIRE with an expected target) that follows the guest's own mutation or LOOKUP/OPEN pin mismatches the host's current cut. The cut binds the journal position and the state hash (viewcontrol controller.go ~552).

So the host runs a full recall, then answers BLOCKED for the stale expectation, and the guest retries. The recall goes RECALL, DRAIN, INSTALL_DRAIN, WAIT_DRAINED, INSTALL_FINAL, REOPEN, and costs about 5-7 s when wf4 scopes it to one domain, or 13-15 s for all domains.

In zq3-r19, T1 (credentials) spent most of its 96 s in about 10 such recalls. Its remaining steps would need about 30 more, roughly 340-385 s in total against the 240 s guest deadline.

## Why the recall is mostly ceremony

Guest read, write, GETATTR and LOOKUP calls are direct RPCs that read the host's live state (hfs_io in fs/9p/hfs_file.c).

The only guest state bound to the view target is the mmap page cache and its mappings, PAGE grants, dirty pages and reservations. Attributes are refreshed on every permission check, and dentries never survive a walk (hfs_d_revalidate returns 0).

For a view with none of that state, the recall's guest-side effect reduces to three things:
- STOP waits for nothing and zaps nothing;
- hfs_mmap_install changes the target and bumps the revision;
- REOPEN bumps the revision again.

Each of the six phases costs about one second of guest round trip, about 5.6 s per recall, with no BLOCKED time.

## Rejected alternatives

**Granting a different target as things stand.** The guest refuses it in four places:
- hfs_lease_decode returns -ESTALE on a target mismatch (hfs_lease.c ~1000);
- hfs_received_source_current marks the reply stale (hfs_received.inc ~597-608), and 2f4's settle path can then end in -EPROTO and a blocked session;
- the guest codec policy requires the lease target to equal the request target (patch 0002 ~1493-1506);
- on the host, guestbridge/view_response.go:200 makes the same check.

**An install-only refresh.** hfs_mmap_install(final) only accepts the DRAINED phase, so this still needs a guest change, and it would still cost about 3-4 s per recall.

**Keeping dentries in hfs_d_revalidate.** Returning 1 without an RPC breaks the rule that cached dentries revalidate through an observation. A stamped variant saves nothing, because the LOOKUP's own pin moves the cut anyway.

**Excluding pins from the cut identity.** The target binds the exact journal position and state hash, which source verification and the receipt-evidence chain depend on.

**Hidden blocker.** releaseSkipsRecall (observation_release.go:117-123) keeps a recall whenever the cut moved under the lease, and every pinning LOOKUP observation moves the cut. Today the next mismatch recall mostly absorbs these release recalls. If the mismatch recalls go away, the release recalls become visible, so the same change must deal with them.

## Recommended approach

### Protocol

VIEW_ACQUIRE gains one optional field, `"advance": true`, placed after `access` in sorted key order.
- When the field is absent, the request bytes are exactly today's. `false` is refused, so every request has a single encoding.
- Its meaning: "my view at the expected target holds no target-bound cache; if the target is stale, grant on the current cut and I will adopt it."
- No new message, status or protocol tuple is needed.
- A new daemon accepts both old and new kernels; a new kernel needs the xf4 daemon.

### Host (step A)

In acquire, before expectedMismatch, grant an ordinary lease on the just-captured current cut when all of these hold (`advanceGrantable`, internal/viewcontrol/advance.go):
1. The request carries advance, mode READ and access DOMAIN, and its ticket has not started its mismatch recall (`ticketState.ExpectedMismatch`). Once that recall ran, the requester's view was reinstalled, so the ticket stays BLOCKED for its old expectation, exactly as before.
2. No recall is running. The request is then PENDING without spending its recall, and its readmitted VIEW_QUERY may be granted by advance once the recall ended.
3. The expected and current targets match on enrollment, profile, context, journal incarnation, epoch and visibility generation, and the expected position is not ahead of the current one; at an equal position the journal hash must match too (`guestbridge.AdvanceCompatible`, shared with the bridge's reply check).
4. mismatchRecallScope(...) returns the requesting session, i.e. wf4 would have chosen a DOMAIN-scoped recall. That means a clean requester, no open reservation anywhere, and every peer idle or Targeted on an observably current cut.
5. The requester holds no active PAGE or OBJECT lease.
6. The grant fits every bound (Sources for the current cut; lease table, control and identity credits and Revisions for the lease; checked without side effects). Otherwise the request takes the recall path: before xf4 that recall's STOP unpinned the requester's leases and moved its fence, relieving the pressure, whereas a BLOCKED answer would repeat on every retry (the R19-1 pattern).

The granted lease is Targeted. A new Advance flag (omitempty) records that every holder requested advance: it is set for any grant to an advance request, also one whose expected target was already current, because such a holder's own pinning LOOKUP moves the cut under it just the same. Releasing an Advance lease may skip the recall even when the cut has moved; je4's own argument covers this, because a DOMAIN lease authorises no PTE. The current-cut condition was only a cost condition: it kept the next observation from paying an expected-target recall, and an advancing guest's next observation is an advance grant instead.

The requester's own other active DOMAIN leases (an observation still in flight, or one whose release is queued) stay active, where the scoped recall's STOP would have deactivated them. A DOMAIN lease authorises no PTE and the guest's cacheless condition includes no active observation tokens, so they only keep pinning their cuts and conflicting with mutations, which is conservative.

Other host changes:
- shareLease ANDs Advance together, as it does Targeted;
- leaseState.Advance is stored omitempty;
- an advance counter (advance grants on a different target since the controller opened, memory only) is reported in OBSERVE as `controller.recalls.advances`, next to the ce4 recall history;
- (task 2h4, later) every VIEW_ACQUIRE BLOCKED decision is recorded in OBSERVE's `controller.recalls.blocked` with its reason: `EXPECTED_MISMATCH` (the old expectation after its recall) or `CAPACITY` with the bound condition 6 checks (`SOURCES`, `CONTROL`, `IDENTITY`, `LEASES`, `REVISIONS`, `LINEAGES`), so a guest BLOCKED can be classified without decoding the ledger (see internal/viewcontrol/README.md);
- guestbridge: view_schema.go accepts the optional field (view_advance.go), and view_response.go allows a different target only when advance was requested and the cuts are AdvanceCompatible;
- the views.json corpus gains the literal command `read_domain_advance` and its GRANTED reply on an advanced cut (the lib9p owned-inputs digest of views.json is refreshed).

### Guest (step B)

- hfs_view_capture also returns a "cacheless" hint under view->lock: no cached pages, mmapped inodes, dirty pages, dirty packages, grants, active tokens and no published observations. The view's own memory credit (view->reservation) is not counted; dirty reservations show as dirty pages/packages.
  - Mappings means inodes that were mmapped (view->nr_mmapped, task tj4), not every registered inode. Before tj4 the test counted view->mappings, which hfs_inode_get fills with every inode, so the mount root alone kept every live view non-cacheless and zq3-r20 never sent advance. view->mappings stays the STOP/recall inventory. hfs_mmap_observe sends advance only when the hint is true, at most once per syscall after a refusal.
- A GRANTED reply whose target differs from the installed one is an advance candidate, not stale, provided it has the same fence, is LIVE at the captured revision, and the installed target equals the request's expected target.
- A new hfs_view_advance (as implemented in 4h4):
  - verifies the new target natively first, outside every view lock (the provider sleeps), as hfs_mmap_install does;
  - takes view->transitions through a bounded, killable wait (hfs_view_transitions_bounded): -EXDEV at once if the view left LIVE or is refused, otherwise -ETIMEDOUT at the syscall deadline or -EINTR on a fatal signal, never an unbounded wait behind a STOP;
  - under view->lock (hfs_view_advance_switch) re-checks LIVE, revision, generation, the expected target's bytes, the visibility generation, the verified hash, the cacheless condition and that no observations are published, then switches the target and bumps the revision;
  - re-stamps the observation before hfs_mmap_publish.
- A failed adoption hands the lease back unadopted with -EXDEV, never -EPROTO.

## Safety

**Requester.** The advance grant is exactly the ordinary grant that a request with a matching expected target would get. It goes to a domain that wf4 would have refreshed with a DOMAIN recall anyway. For a view the guest has confirmed is cacheless, under the same lock as the switch, that recall's only guest-side effect is "target := current cut, revision++", and the advance does precisely that. Nothing is invalidated because nothing is cached, and the revision bump makes any stale stamp fail its re-check.

**Peers.** Condition 4 leaves the peer side exactly as in the scoped recall.

**No stale data.** The guest serves nothing under an unadopted lease. Rollback is impossible, because adoption requires the installed target to equal the request's expected target and the revision to be unchanged.

**Fences.** No fence moves, so the lease fence equals the mount's current fence.

## Crash and replay

**Host.** The advance GRANTED is protected like any other grant, and retransmits and VIEW_QUERY return the same bytes. The new fields are omitempty, so schema 2 is unchanged; downgrading after advances is unsupported. After a reopen the digest cache is empty, and an unknown digest falls back to the recall path; a reopened ledger keeps the Advance flag. A reopen may keep the journal incarnation, so a pre-reopen expected cut can still be advanced; that is safe because the guest adopts only while its installed target equals the expected one at the captured revision. A PENDING ticket readmitted after a recall that reinstalled the requester's view may be granted by advance; the guest then finds its revision moved and returns the lease unadopted (-EXDEV), which is safe.

**Guest.** A replay after adoption is current (accepted as an ordinary verified lease; publish refuses it because the observation's stamp predates the move, so it is only released). A replay after a further move skips settle (the exchange hands a moved advance grant out) and is built unadopted with -EXDEV and released; it never blocks the session. An unparseable payload is handed out the same way. view->transitions serialises a STOP that races an adoption.

**Lineages.** Each advance opens a new lease lineage at the same fence. Once a domain reaches lineageBudget (16) lineages at its current fence, a release recalls again. Expect 0-1 of these in T1; optional step D removes them.

## Test plan

**Host:**
- the grant path;
- one negative per condition, each falling back to PENDING, recall, BLOCKED;
- replay, retransmit and readmit;
- an Advance release without a recall;
- the lineage budget;
- schema refusals;
- a Facts release case;
- a new zq3_advance_replay_test.go that replays T1 credentials. The baseline should show at least 10 EXPECTED_TARGET recalls; with advance, 0 EXPECTED_TARGET and 0 RELEASE recalls, and the same CALL_CONFLICT count.

Step A result (TestZQ3AdvanceRemovesExpectedTargetRecalls, the T1 case body with dentry eviction FORGETs, pinning LOOKUPs and the creating CALLs meeting a still-queued observation release): without advance 7 CALL_CONFLICT + 28 EXPECTED_TARGET + 9 RELEASE recalls; with advance 7 CALL_CONFLICT recalls and 37 advance grants. Without the Advance release relaxation the advance run still shows the 9 RELEASE recalls. The post-case inventory and release process are not replayed; their lineage budget recalls are left to zq3-r20.

**Guest (tools/integration/linux9p/tests):**
- view/, leases/, controlled_received/;
- policy_cases.py;
- mapped/: a view with mmap pages never sends advance.

**Lab (zq3-r20):** EXPECTED_TARGET 0, RELEASE recalls 0-1, CALL_CONFLICT about 7, and an advance counter of about 30-40.

## Expected T1 time

About 130-150 s against the 240 s deadline (pessimistic 200 s):
- admission, about 8 s;
- about 7 CALL_CONFLICT recalls at about 14 s each, about 98 s;
- about 37 parent syscalls and 6 children without recalls, about 25-35 s.

## Steps

- **A (daemon), task 3h4:** host advance grant, release relaxation, schema, docs and tests. Safe to land alone, because old kernels never send advance.
- **B (kernel), task 4h4:** the guest side. It lands after 1h4, which edits the same hfs_mmap.c region.
- **C:** zq3-r20 with A and B, plus 1h4 and 2h4.
- **Optional D:** recycle lineages whose target is below the current cut without a fence move.
- **Optional E:** grant on a retained expected target when it is observably equal to the current cut.
- **Follow-up F:** CALL_CONFLICT against the domain's own observation leases whose release is still pending, e.g. the guest flushes pending releases before a mutation.
