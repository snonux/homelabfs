package viewcontrol

import (
	"context"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Recall scope (task 784, zq3-r8 R8-1).
//
// Every recall used to be all-domain, and its admission barrier closed every
// domain's CALLs until REOPEN. In zq3-r8 each short-lived guest process took
// a DOMAIN READ lease, and releasing it started a five-phase recall across
// BOTH domains; a PROCESS_EXIT started one too whenever a peer held any
// active lease. A peer's root GETATTR and m0's PROCESS_EXIT then stayed
// RESERVED/UNPREPARED until the guest's 30 s QUERY window blocked both
// sessions. Two rules now keep that traffic out of other domains' way while
// every page/data safety invariant stays as it was:
//
//  1. Authority reductions (PROCESS_EXIT, FORGET, DESCRIPTION_RELEASE) never
//     conflict with a lease, so a peer's lease never makes them start a
//     recall. Only an open dirty reservation of their OWN domain still
//     conflicts: its bytes must drain under the original process and
//     description before either goes away (ownReservationOpen).
//     They only shrink existing authority (Retained-9p-protocol.md: "This
//     exception only reduces existing authority"): no cached page, attribute,
//     directory entry or negative dentry of any domain changes. FORGET and a
//     final DESCRIPTION_RELEASE may reclaim an unlinked object, but only once
//     no reference remains, and every lease or mapping that could observe it
//     holds such a reference. PROCESS_EXIT releases fcntl locks, which hold
//     no cache/data authority (see AdmitNative), and leaves the source cut
//     unchanged, so the recall barrier itself never holds it (only its own
//     domain's open reservation can). FORGET and
//     DESCRIPTION_RELEASE may append lifetime state and so change the cut;
//     while a recall installs a cut they wait (recallHoldsCall), because the
//     guest refuses a REOPEN whose target is not the INSTALL_FINAL cut.
//  2. Releasing a clean READ lease recalls only the releasing domain when
//     that domain holds no EXCLUSIVE_DIRTY lease and no open reservation
//     (releaseRecallScope). Such a recall has nothing to drain: it only stops
//     the domain, reinstalls the current cut and reopens it, so its readable
//     PTEs are closed before RELEASED. Its barrier is per domain: the other
//     domains keep their leases, and their reads, READ acquisitions/renewals
//     and PROCESS_EXITs proceed. Nothing that changes the cut is admitted
//     during any recall (non-read CALLs, native commands, DIRTY_RESERVE), so
//     the scoped recall's install sequence sees one stable cut.
//
// Every other recall (dirty or exclusive release, conflicting admission or
// acquisition, an expected-target mismatch) stays all-domain with the global
// barrier, exactly as before.

// covers reports whether session participates in the recall.
func (r *recallState) covers(session string) bool {
	return r.Domain == "" || r.Domain == session
}

// recallHashes returns the inventory and closure hashes of a new recall. An
// all-domain recall binds the whole domain map, as it always did; a scoped
// recall binds only its domain and names the scope in the closure.
func (c *Controller) recallHashes(id, scope string) (string, string) {
	if scope == "" {
		inventory := digest(c.state.Domains)
		return inventory, digest(struct{ Enrollment, Inventory, Recall string }{c.enrollmentHash, inventory, id})
	}
	inventory := digest(map[string]domainState{scope: c.state.Domains[scope]})
	return inventory, digest(struct{ Enrollment, Inventory, Recall, Domain string }{c.enrollmentHash, inventory, id, scope})
}

// startDomainRecall starts a recall of the single domain session; it is a
// no-op while any recall exists.
func (c *Controller) startDomainRecall(ctx context.Context, session string) error {
	return c.beginRecall(ctx, session)
}

// startReleaseRecall starts the recall a preowned VIEW_RELEASE of lease
// needs: scoped to the releasing domain when releaseRecallScope allows it,
// otherwise all-domain.
func (c *Controller) startReleaseRecall(ctx context.Context, lease guestbridge.ViewLease) error {
	if scope := c.releaseRecallScope(lease); scope != "" {
		return c.startDomainRecall(ctx, scope)
	}
	return c.startRecall(ctx)
}

// releaseRecallScope returns the session whose recall alone closes a release
// of lease, or "" when the release needs the all-domain recall. Only a clean
// READ lease qualifies, and only while its domain holds no EXCLUSIVE_DIRTY
// lease and no open reservation: such a domain has no dirty bytes to freeze
// and drain, so stopping it changes nothing another domain can observe.
func (c *Controller) releaseRecallScope(lease guestbridge.ViewLease) string {
	if lease.Mode != "READ" {
		return ""
	}
	session := lease.Scope.Session
	for _, l := range c.state.Leases {
		if l.Active && l.Lease.Scope.Session == session && l.Lease.Mode != "READ" {
			return ""
		}
	}
	for _, r := range c.state.Reservations {
		if r.Status != "CLOSED" && r.Reservation.Scope.Session == session {
			return ""
		}
	}
	return session
}

// ownReservationOpen reports whether session holds an open (not CLOSED)
// dirty reservation. An authority reduction of that domain waits for such a
// reservation's recall: the exiting process or released description may be
// the origin of dirty bytes that must drain under it first
// (TestTransportLifetimeCannotDiscardDirtyDescriptionOrExitPins). Another
// domain's reservation pins its own description and object, so it is
// unaffected.
func (c *Controller) ownReservationOpen(session string) bool {
	for _, r := range c.state.Reservations {
		if r.Status != "CLOSED" && r.Reservation.Scope.Session == session {
			return true
		}
	}
	return false
}

// anyReservationOpen reports whether any domain holds an open reservation.
func (c *Controller) anyReservationOpen() bool {
	for _, r := range c.state.Reservations {
		if r.Status != "CLOSED" {
			return true
		}
	}
	return false
}

// authorityReduction reports the opcodes that only reduce existing
// authority: they conflict with no lease and only with their own domain's
// open reservations.
func authorityReduction(op string) bool {
	switch op {
	case "PROCESS_EXIT", "FORGET", "DESCRIPTION_RELEASE":
		return true
	}
	return false
}

// recallHoldsCall reports whether the current recall makes a CALL of op from
// session wait (WouldBlock until REOPEN). PROCESS_EXIT is never held by the
// barrier: it keeps the cut and holds no cache/data authority (conflicts with
// its own domain's open reservation are checked separately). Any other op that may change
// the cut waits for every recall, because the recall installs one exact cut.
// A read waits only for a recall its domain participates in.
func (c *Controller) recallHoldsCall(session, op string) bool {
	r := c.state.Recall
	switch {
	case r == nil || op == "PROCESS_EXIT":
		return false
	case !readOperation(op):
		return true
	default:
		return r.covers(session)
	}
}

// recallHoldsLease reports whether the current recall makes a READ/
// EXCLUSIVE_DIRTY acquisition or renewal from session wait. Only a READ lease
// of a domain outside a scoped recall proceeds: it shares the stable current
// cut with every other READ lease and changes nothing the recall installs.
func (c *Controller) recallHoldsLease(session, mode string) bool {
	r := c.state.Recall
	return r != nil && (mode != "READ" || r.covers(session))
}
