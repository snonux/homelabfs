package viewcontrol

import "github.com/snonux/homelabfs/internal/guestbridge"

// Own observation leases do not conflict with their own CALLs (task vj4,
// xf4 follow-up F; design docs/design/vj4-self-conflict.md).
//
// In zq3's T1 the largest recall cost left once advance (xf4) removed the
// EXPECTED_TARGET recalls was CALL_CONFLICT: r20 MKDIR (ALL) 26.6 s and
// SETATTR (ALL) 11.6 s, r19 CREATE 14.7 s. Each was a guest's mutating CALL
// meeting that same guest mount's own observation lease: hfs_mmap_end had
// already marked the lease revoked and queued its VIEW_RELEASE on the
// mount's cleanup worker, but the release had not reached the host yet. So
// conflicts() saw an active lease, the all-domain recall stopped both
// mounts, waited for the queued release and the STOPPED round trips, and
// only REOPEN admitted the CALL.
//
// What that recall bought for the requester's own lease: nothing. An
// observation lease (selfObservation: a Targeted DOMAIN READ lease) authorizes
// no PTE; guest mappings fault only under PAGE leases. Its holder installs
// attributes only under a separately stamped short MM token while its
// observation is in flight, attributes are refreshed by RPC on every
// permission check and never become timeless pathname permission, and
// cached dentries revalidate through an RPC (hfs_d_revalidate returns 0;
// observation_release.go has the full guest-side argument, which je4's
// recall-free release already relies on). Whatever the own observation saw,
// it is the requester's own concurrent operation: the guest already lets
// two of its own concurrent mutations (both lease-less) reach the host in
// either order with no recall between them, so ordering its own in-flight
// read against its own write buys no guarantee the guest does not already
// give up. The recall stopped the requester only to wait for a release the
// guest had committed to already.
//
// So conflicts() skips a lease when all of these hold:
//   - the lease's Scope equals the requesting CALL's full Context (same
//     session, guest boot, mount, FUSE session and generation);
//   - it is a DOMAIN READ lease, i.e. no PAGE or OBJECT lease (cache
//     published under it must be revoked by STOP) and no EXCLUSIVE_DIRTY
//     lease (dirty bytes must drain under their original authority);
//   - it is Targeted: every holder acquired it expecting its own target, so
//     it is a guest observation. A target:null discovery lease
//     (hfs_mmap_bootstrap) or a renewal keeps conflicting.
//
// Everything else is unchanged: every peer lease of any kind conflicts as
// before, so cross-domain conflicts recall exactly as they did, with the
// same trigger and scope; the requester's own PAGE, OBJECT and
// EXCLUSIVE_DIRTY leases conflict; every open reservation of any domain
// conflicts; a running recall still holds the CALL (recallHoldsCall); native
// commands belong to no guest context and get no exemption.
//
// Linearizability. The mutation is linearized at its journal append as
// before. The requester's later reads see it: GETATTR, LOOKUP and READ are
// direct RPCs against live host state, and a later observation of the
// requester expects its installed (pre-mutation) view, so acquire finds the
// mismatch and grants only on the freshly captured current cut, by advance
// or after the expected-target recall; no grant ever names the older cut
// again. The skipped lease stays active, still pins its cut and still makes
// every peer mutation recall the requester, as before. Its release then
// finds the cut moved: an Advance lease releases without a recall
// (observation_release.go), any other starts the scoped RELEASE recall of
// the requester alone, which costs at most what the skipped all-domain
// recall cost and nothing for the peers.
//
// Side effect on peers, conservative only: while the skipped lease is still
// active on the pre-mutation cut, a peer's expected-target mismatch sees an
// active lease on an observably older cut (peerLeasesObservablyCurrent), so
// that mismatch takes the all-domain recall and the peer's advance falls
// back to it (advance condition 4). That window ends with the queued
// release, and it is the state any observation lease is already in after a
// peer's cut move; it narrows the scoping, never widens it.

// selfObservation reports whether l is an active observation lease of the
// requesting context scope itself: a Targeted DOMAIN READ lease whose Scope
// is exactly scope. conflicts() skips such a lease for scope's own CALLs.
func selfObservation(l leaseState, scope guestbridge.Context) bool {
	return l.Active && l.Targeted && l.Lease.Scope == scope &&
		l.Lease.Access.Kind == "DOMAIN" && l.Lease.Mode == "READ"
}

// exemptSelf reports whether conflicts() skips l for a CALL of scope:
// selfObservation, unless the test-only selfConflicts ablation restores the
// pre-vj4 rule.
func (c *Controller) exemptSelf(l leaseState, scope guestbridge.Context) bool {
	return !c.selfConflicts && selfObservation(l, scope)
}

// noteSelfAdmission counts a mutating CALL that passed the conflict check
// only because selfObservation skipped the requester's own active
// observation leases, i.e. a CALL_CONFLICT recall that did not run. Like the
// advance counter it is diagnostic memory only (OBSERVE's
// controller.recalls.self_admissions); it also counts an admission that
// later fails, and a reopen starts it at 0.
func (c *Controller) noteSelfAdmission(scope guestbridge.Context, read bool) {
	if read {
		return // a read never conflicts with a READ lease anyway
	}
	for _, l := range c.state.Leases {
		if c.exemptSelf(l, scope) {
			c.history.selfAdmissions++
			return
		}
	}
}
