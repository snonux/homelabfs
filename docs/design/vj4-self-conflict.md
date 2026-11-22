# vj4: a CALL does not conflict with its own observation leases (design)

This design is xf4's follow-up F ([xf4-view-advance.md](xf4-view-advance.md)).
It comes from these findings:
- zq3-r19 ([README](../lab/results/20261121-zq3-r19/README.md)): CREATE, CALL_CONFLICT, ALL, 14.7 s;
- zq3-r20 ([README](../lab/results/20261122-zq3-r20/README.md)): MKDIR, CALL_CONFLICT, ALL, 26.6 s, and SETATTR, CALL_CONFLICT, ALL, 11.6 s.

## Problem

After xf4 (and tj4, which lets the guest actually send advance), the
EXPECTED_TARGET recalls are gone from T1. The largest cost left is the
CALL_CONFLICT recalls.

Each one has the same shape:
- a guest mount sends a mutating CALL (MKDIR, CREATE, SETATTR);
- the host still sees an active observation lease of that **same** mount;
- the guest has already ended that observation, but its VIEW_RELEASE is still queued behind the mount's cleanup worker.

So the host recalls all domains. It waits for the queued release and for every participant's STOPPED round trip, and only REOPEN admits the CALL. With T1's 240 s deadline this is about 7 recalls at 12-27 s each.

## The code paths

### Host: where a CALL starts a CALL_CONFLICT recall

1. guestbridge dispatches the guest's CALL to the provider, and `controlledvalidation.Provider.admit` (admission_refusal.go) calls `viewcontrol.Controller.Admit`.
2. `Admit` → `admissionBarrier` → `recallBarrier` (internal/viewcontrol/admission.go).
3. `recallBarrier` exempts authority reductions. For every other op it asks `conflicts(read)`. Before vj4 a non-read op conflicted with **every** active lease, whatever its domain. Then `startRecall(trigger{Kind: CALL_CONFLICT, Opcode, Domain})` began the all-domain recall (recall.go `beginRecall`), and the CALL got WouldBlock until REOPEN; the guest's QUERY resumes it.
4. internal/controlledruntime only assembles this. Its Facts certify the recall's ACKs and releases (facts.go `checkRelease`, which never compares a lease target with the current cut), but they make no conflict decision of their own.

### Guest: where observation releases are queued (patches 0004/0005)

- Every VFS read runs `hfs_begin(mutation=false)` → `hfs_mmap_begin` → `hfs_mmap_observe_lease`, which takes a DOMAIN READ lease expecting the installed view.
- Mutations run `hfs_begin(mutation=true)`, a lease-less observation: their CALL goes out under no lease at all.
- `hfs_mmap_end` (0004, ~line 7163) does three things in order:
  - under `view->lock` it unlinks the observation, decrements `nr_observations` and sets `lease->revoked`;
  - it leaves the provider;
  - it calls `queue_delayed_work(cleanup_queue, &cleanup->work, 0)`.
- `hfs_mmap_cleanup` (~line 5686) runs on the `9p-hfs-cleanup` workqueue (max_active 1). It sends the VIEW_RELEASE (`hfs_lease_release`) and retires tickets.
- So when the syscall returns, the lease is revoked in the guest, but the host still has it active until the worker reaches it. The next syscall's mutating CALL can arrive first, and in the lab it usually did.

## Candidates

**(a) The guest flushes its pending releases before a mutating CALL.**
- This needs a guest kernel change.
- Each mutation pays an extra synchronous VIEW_RELEASE exchange, about 0.5-1 s in the lab.
- It does not cover an observation that another thread of the same mount still has **in flight**: that lease is still needed, so it cannot be flushed, and the recall stays.
- It also serializes mutations behind the cleanup worker, which may be blocked behind a recall-bound release, so it adds a new wait edge.

**(b) The host treats the requester's own observation leases as non-conflicting for the requester's own CALL.**
- Host-only: no new message and no guest change.
- No round trip at all, and it also covers own observations still in flight.

**(c) Alternatives considered:**
- *The guest marks its CALL with "my observations are revoked".* That needs a protocol field and a guest change, and it gives the host nothing it can verify better than (b)'s structural rule.
- *The host defers the CALL briefly, waiting for the release, without a recall.* That still waits for the cleanup worker, and the host still needs a timeout and fallback.
- *Scope the CALL_CONFLICT recall to the requester.* That still costs a 5-6 s DOMAIN recall.

**Choice: (b), narrowly.** A CALL skips a lease in its conflict check only when all of these hold (`selfObservation`, internal/viewcontrol/self_conflict.go):

1. The lease is active, and its `Scope` equals the CALL's **full** `guestbridge.Context`: same session, guest boot, guest mount, FUSE session, generation and the rest. So "same domain" means the same mount and view, not merely the same session ID.
2. It is a **DOMAIN READ** lease, so neither PAGE nor OBJECT access and not EXCLUSIVE_DIRTY.
3. It is **Targeted**: every holder acquired it expecting its own target, so it is a guest observation. A `target:null` discovery lease (`hfs_mmap_bootstrap`) or a renewal still conflicts.

Everything else conflicts exactly as before:
- every peer lease of any kind, so a cross-domain conflict starts the same recall, with the same trigger, scope and participants;
- the requester's own PAGE, OBJECT, EXCLUSIVE_DIRTY and untargeted leases;
- every open reservation of any domain;
- a running recall still holds the CALL (`recallHoldsCall`);
- native commands, which belong to no guest Context (the zero Context matches no lease).

## Why it is safe

**What the skipped recall protected for the requester's own lease: nothing.**
- A DOMAIN lease authorizes no PTE. Guest mappings fault only under PAGE leases, which still conflict.
- A DOMAIN observation installs attributes only under a separately stamped short MM token while it is in flight (`hfs_attr_install`, `hfs_attr_restamp`).
- Attributes are refreshed by RPC on every permission check and never become timeless pathname permission.
- Dentries never survive a walk (`hfs_d_revalidate` returns 0).
- This is the argument je4's recall-free release already relies on (observation_release.go): a DOMAIN lease's recall stops and reopens a view with nothing under the lease to stop.
- Dirty bytes are covered by EXCLUSIVE_DIRTY leases and reservations, which still conflict.

**Linearizability.**
- *Linearization point.* The mutation is linearized at its journal append, exactly as before. Only the admission wait changes, not what is applied or where.
- *The requester's later operations see the write.*
  - Its later reads (GETATTR, LOOKUP, READ) are direct RPCs against live host state (`hfs_io`), so they see it.
  - Its later observations expect its installed view, which is the pre-mutation cut, because the guest never moves its view on its own mutation.
  - acquire therefore finds `expectedMismatch` and grants only on the freshly captured current cut, either by advance or after the expected-target recall.
  - No grant names the pre-mutation cut again. The still-active old lease is never shared with a new request, because `grantLease` shares only a lease of the same lineage, and a lineage includes the target.
  - A self admission does not move the guest's view (no revision bump, no target switch), so the view stamp check in `hfs_mmap_publish` plays no part here. The view moves only later, by advance or recall, at the next observation.
- *Own observations still in flight are concurrent with the mutation.* So either order is linearizable.
  - The only thing they could leave behind is attributes installed after the mutation's own reply.
  - The real guard is the guest's per-object revision check: `hfs_attr_install` (patch 0003, ~line 4415) installs attributes only when `attr.revision >= inode_peek_iversion_raw(inode)` and then stores that revision. A reply carrying the object's pre-mutation revision that arrives after the mutation's reply installed the newer one is dropped, so no object's cached attributes go backwards.
  - An observation is not a multi-object snapshot: each object's attributes are ordered by that object's own revision alone, and two objects read in one observation can straddle the mutation. That was already true of the guest's own lease-less reads and mutations.
  - On top of that, attributes are refreshed by RPC on every permission check, as shown above.
  - The guest already accepts this for two of its own concurrent mutations: both are lease-less, so the host has always admitted them in either order with no recall between them, and the same revision check orders their attribute installs.
  - Before vj4, the recall's STOP made the mutation wait for the own observation. That ordered two concurrent operations of one client, which linearizability does not require.

**Peers: cross-domain behaviour is unchanged.**
- The rule skips only leases whose Scope is the requester's Context, so every peer lease is checked exactly as before.
- The skipped lease stays **active** in the ledger. It keeps pinning its cut, since source pinning is by ledger membership in any case. A peer's later mutation still conflicts with it and recalls the requester, as before.
- One conservative side effect: while that lease is still active on the pre-mutation cut, a **peer's** expected-target mismatch sees an active lease on an observably older cut (`peerLeasesObservablyCurrent`). So that mismatch takes the all-domain recall, and the peer's advance falls back to it (xf4 condition 4).
  - This can only narrow the scoping, never widen any authority.
  - It is the state every observation lease is already in after a peer moves the cut, and it ends with the queued release.

**Release of the skipped lease.** When the queued VIEW_RELEASE arrives, the cut has moved under the lease because of the requester's own mutation.
- An Advance lease, which the guest asked for because its view was cacheless, is released without a recall: xf4's relaxation in `releaseSkipsRecall`.
- Any other observation lease takes the existing scoped RELEASE recall of the requester alone. That recall has nothing to drain, never touches peers, and costs less than the skipped all-domain recall.
- Facts `checkRelease` certifies a Targeted DOMAIN READ release without a fence advance and never compares the target with the current cut, so no Facts change is needed.

**Crash, replay and reopen.**
- The rule is a pure function of the ledger and the request's Context, with nothing persisted. So a retransmitted CALL, a QUERY resume or a controller reopen decides the same way on the same ledger.
- The only new state is the `self_admissions` counter in memory. It is reported in OBSERVE's `controller.recalls` with `omitempty`, so the shape is unchanged while it is 0, and a reopen starts it at 0.

**Lineages.** The CALL_CONFLICT recalls used to move both domains' fences, and compaction then recycled their lease lineages.
- Without those recalls, an advancing domain reaches `lineageBudget` lineages at one fence about twice in T1, and each time one observation release takes a scoped RELEASE recall (xf4 "Lineages").
- These are DOMAIN-scoped. xf4's optional step D (recycling lineages below the current cut without a fence move) would remove them.

## Implementation (host only)

- `internal/viewcontrol/self_conflict.go`:
  - `selfObservation` (the rule);
  - `exemptSelf`, which applies the rule unless the test-only `selfConflicts` ablation restores the pre-vj4 rule;
  - `noteSelfAdmission`, which increments the counter.
- `admission.go`:
  - `recallBarrier` passes the CALL's full Context;
  - `conflicts(scope, read)` skips `exemptSelf` leases;
  - native commands pass the zero Context.
- `recall_history.go`: `RecallHistory.SelfAdmissions` (`self_admissions`, omitempty).
- Documentation: internal/viewcontrol/README.md ("Own observation leases") and docs/spec/Retained-9p-view-leases.md (the conflict rules).

**No guest kernel change is needed.** The guest already revokes an observation lease under `view->lock` before it queues the release, and it never publishes under a lease its view moved past. vj4 relies only on that existing behaviour. Option (a) is therefore not needed. The guest patches and `internal/guestviewmodel` are unchanged; the model still decides advance, and the vj4 rule is entirely host-side.

## Tests

**internal/viewcontrol, self_conflict_test.go:**
- the own observation lease does not recall the own MKDIR (lease stays active, no fence move, one self admission), and its later non-Advance release recalls only the requester;
- an own Advance observation is released without any recall after the MKDIR;
- the requester's next observation is never granted the pre-mutation cut (expected-target recall, or an advance onto the current cut);
- a peer lease, alone or with the requester's own, starts the all-domain CALL_CONFLICT recall with exactly the scope and participants of the pre-vj4 ablation;
- an own untargeted lease, an own PAGE lease and the ablation still recall;
- the predicate, field by field: another session, the same session with another guest mount, inactive, untargeted, PAGE, EXCLUSIVE_DIRTY, and the zero Context;
- the OBSERVE shape.

**internal/viewcontrol, zq3_self_conflict_replay_test.go (T1 at the observation level):**
- with advance: pre-vj4, 7 CALL_CONFLICT recalls; vj4, 0 CALL_CONFLICT, 0 EXPECTED_TARGET and 2 RELEASE (lineage budget, DOMAIN) recalls, with 7 self admissions and 35 advances;
- without advance, vj4: 0 CALL_CONFLICT, 21 EXPECTED_TARGET and 16 RELEASE recalls, all DOMAIN, against 7 CALL_CONFLICT (ALL) + 28 EXPECTED_TARGET + 9 RELEASE before;
- with a peer observation in flight across each creating CALL: 7 all-domain CALL_CONFLICT recalls under both rules and 0 self admissions.
- The xf4 replays (zq3_advance_replay_test.go) now run under the ablation, so they keep measuring xf4 alone.

**internal/controlledruntime (T1 on the assembled runtime with the slow guest observer):**
- `TestZQ3T1CredentialsReplay` requires 0 CALL_CONFLICT recalls and at least one self admission per creating CALL in all four runs. With advance it allows at most 2 RELEASE recalls.
- The new `TestZQ3T1PeerConflictReplay` keeps m1's observation in flight across each creating CALL and requires:
  - one all-domain CALL_CONFLICT recall per creating CALL, with both mounts;
  - m0's guest-order early DRAINED to have been BLOCKED at least once, the path the self conflicts used to exercise;
  - no self admission.
- Results on this laptop (model latency 2 s):
  - with advance, both schedules: 0 CALL_CONFLICT, 2 RELEASE recalls, 7-10 self admissions, 35-37 advance grants; T1 modelled 22 s, measured about 1.5 min with this host's work;
  - without advance: 0 CALL_CONFLICT, 23 EXPECTED_TARGET, 13-14 RELEASE recalls and 14 self admissions;
  - peer variant: 7 all-domain CALL_CONFLICT recalls with 7 BLOCKED early DRAINEDs, and T1 modelled 1 min 26 s.

## Expected lab effect (zq3-r21)

- `controller.recalls`:
  - no CALL_CONFLICT record from a domain's own queued release;
  - `self_admissions` about 7 or more in T1;
  - 0-2 DOMAIN RELEASE recalls from the lineage budget.
- T1 then costs about admission (~8 s) plus the syscalls without recalls (~25-35 s) plus at most two scoped recalls (~10-12 s), roughly 45-60 s against the 240 s deadline.
- A CALL_CONFLICT that still appears names a real cross-domain conflict, or one of the requester's own PAGE, OBJECT or EXCLUSIVE_DIRTY leases or a reservation.
