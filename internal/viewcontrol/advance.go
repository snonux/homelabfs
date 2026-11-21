package viewcontrol

import (
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Guest-requested view advance (task xf4 step A, after zq3-r18 R18-1 and
// zq3-r19 R19-1; design docs/design/xf4-view-advance.md).
//
// Every guest observation that follows the guest's own mutation or pinning
// LOOKUP/OPEN expects a cut that is no longer current (the cut binds the
// journal position and state hash, controller.go target). Before xf4 each
// such acquisition hit expectedMismatch: a recall (RECALL, DRAIN,
// INSTALL_DRAIN, WAIT_DRAINED, INSTALL_FINAL, REOPEN; 5-7 s scoped by wf4)
// installed the current cut, BLOCKED answered the stale expectation and the
// guest retried. In zq3-r19 T1 spent most of its 96 s in about 10 of them.
//
// For a guest view that holds no target-bound cache that recall is
// ceremony: guest read, write, GETATTR and LOOKUP are direct RPCs against
// live host state, attributes are refreshed on every permission check and
// dentries never survive a walk. The only target-bound guest state is the
// mmap page cache and its mappings, PAGE grants, dirty pages and
// reservations. Without any of it the recall's whole guest-side effect is
// "target := current cut, revision++" (STOP waits for and zaps nothing,
// INSTALL_FINAL switches the target, REOPEN bumps the revision again).
//
// The guest (step B) therefore sends "advance": true only when its view is
// cacheless under view->lock, and acquire grants such a stale observation an
// ordinary lease on the current cut it just captured, instead of the recall,
// when all of the following hold (advanceGrantable):
//
//  1. the request carries advance, mode READ and access DOMAIN (the bridge
//     schema already refuses advance on anything else; checked again here),
//     and this ticket has not started its mismatch recall
//     (ticketState.ExpectedMismatch). Once that recall ran, the requester's
//     view was reinstalled, so the ticket stays BLOCKED for its old
//     expectation exactly as before xf4;
//  2. no recall is running (expectedMismatch then answers PENDING without
//     spending its recall, and the retry is classified again);
//  3. the expected and current targets are guestbridge.AdvanceCompatible:
//     same enrollment, profile, context, journal incarnation, epoch and
//     visibility generation, and the expected position not ahead of the
//     current one (at an equal position, the same journal hash). A target of
//     another incarnation (after a reopen) or epoch keeps the recall;
//  4. mismatchRecallScope returns the requesting session: wf4 would have
//     answered this mismatch with a DOMAIN recall of the requester alone.
//     That means a READ request, a clean requester (no EXCLUSIVE_DIRTY lease,
//     no open reservation), no open reservation anywhere, and every active
//     peer lease Targeted on a cut observably equal to the current one (or
//     no peer lease at all);
//  5. the requester holds no active PAGE or OBJECT lease (holdsCacheLease):
//     those are target-bound cache by definition, and only the recall's STOP
//     revokes what the guest published under them;
//  6. the grant fits every bound (sourceFits, grantFits): Sources for the
//     current cut, and the lease table, control and identity credits and
//     Revisions for the lease. A grant that would be BLOCKED for capacity
//     falls back to the recall instead. Before xf4 this request started a
//     recall whose STOP deactivated the requester's leases and moved its
//     fence, which relieves exactly that pressure (unpinned cuts, recyclable
//     lineages); answering BLOCKED would make every retry meet the same
//     full bound, the zq3-r19 R19-1 loop. The check has no side effects, so
//     the fallback starts from the unchanged ledger.
//
// Safety. Requester: the advance grant is byte for byte the grant a request
// with a matching expected target would get on this cut (grantLease), given
// to a domain wf4 would have refreshed by a DOMAIN recall anyway. For a view
// the guest confirmed cacheless under the same lock as its switch, that
// recall's only guest-side effect is what the guest's adoption does
// (target := current cut, revision++): nothing is invalidated because
// nothing is cached, and the revision bump fails any stale stamp. The
// requester's own other active DOMAIN leases (observations still in flight
// or whose release is queued) stay active here, where the recall's STOP
// would have deactivated them; a DOMAIN lease authorizes no PTE and the
// guest's cacheless condition includes no active observation tokens, so
// this only keeps them pinning their cuts and conflicting with mutations,
// which is conservative. Peers: condition 4 leaves them exactly as the
// scoped recall would (it sends them no event). No stale data: the guest
// serves nothing under a lease it has not adopted, and adoption requires its
// installed target to equal this request's expected target at an unchanged
// revision, so no rollback is possible. Fences: no fence moves, so the
// lease's fence is the domain's current fence, as for every grant.
//
// Replay. The advance GRANTED is protected like every admission: the exact
// retransmit and VIEW_QUERY return the same bytes. A ticket that was PENDING
// (a recall was running) is readmitted against the then-current cut and may
// be granted by advance then; if that recall reinstalled the requester's
// view, the guest finds its revision moved, refuses the adoption and hands
// the lease back unadopted (-EXDEV), which is safe, merely one wasted grant
// (before xf4 that readmit started a further recall). A controller reopen
// may keep the same journal incarnation, epoch and visibility generation,
// so an expected cut from before the reopen can still pass condition 3 and
// be advanced; that is safe for the same reason as every advance: the guest
// adopts only while its installed target equals the expected target at the
// captured revision, and otherwise hands the lease back unadopted.
//
// Lineages. Each advance grant opens a lease lineage at the domain's current
// fence (a lineage includes the target). Its release is recall-free while
// the domain has lineage headroom (observation_release.go); once a domain
// holds lineageBudget lineages at its fence, a release recalls again, which
// moves the fence and lets compaction recycle them.

// advanceGrantable reports whether acquire may grant request r of ticket t,
// whose expected target is not the current target captured from current, on
// that current target by advance: conditions 1-6 above.
func (c *Controller) advanceGrantable(scope guestbridge.Context, r request, t ticketState, current fsservice.ControlledViewSource, target guestbridge.ViewTarget) bool {
	if !r.Advance || t.ExpectedMismatch || r.Mode != "READ" || r.Access.Kind != "DOMAIN" || r.Target == nil {
		return false
	}
	if c.state.Recall != nil || !guestbridge.AdvanceCompatible(*r.Target, target) {
		return false
	}
	if c.mismatchRecallScope(scope, r, current) != scope.Session || c.holdsCacheLease(scope.Session) {
		return false
	}
	return c.sourceFits(current) && c.grantFits(c.leaseFor(scope, r, target))
}

// holdsCacheLease reports whether session holds an active PAGE or OBJECT
// lease (condition 5): any lease kind other than DOMAIN may have target-bound
// guest cache published under it.
func (c *Controller) holdsCacheLease(session string) bool {
	for _, l := range c.state.Leases {
		if l.Active && l.Lease.Scope.Session == session && l.Lease.Access.Kind != "DOMAIN" {
			return true
		}
	}
	return false
}
