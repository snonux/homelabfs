package viewcontrol

import (
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Scoped expected-target recalls (task e84, after zq3-r14..r17; widened by
// task wf4, after zq3-r18).
//
// The guest takes every observation lease with expected_target = its
// installed view. When the cut moved since that view was installed, the
// acquisition hits expectedMismatch, which starts the recall that installs
// the current cut in the requesting domain and then answers BLOCKED for the
// old expectation. Before e84 that recall was always all-domain.
//
// What the all-domain recall protects. The requester needs a fresh view of
// the current cut; any recall covering its domain gives it one (STOP,
// INSTALL_FINAL of the current cut, REOPEN). For a peer the recall does two
// things: it stops the peer's mappings and reinstalls the current cut in the
// peer's view. That matters only if the peer's view could show something
// the current cut no longer holds:
//
//   - A peer WITHOUT an active lease has no host authority to serve from its
//     view: each of its VFS reads first takes its own observation lease with
//     expected_target = its installed view and so hits its own mismatch, and
//     its mappings fault under PAGE leases (see observation_release.go for the
//     guest-side reasons). It needs nothing from this recall.
//   - A peer WITH an active observation lease (leaseState.Targeted: every
//     holder acquired it with expected_target equal to its target) has that
//     target installed as its view. If that cut holds the same
//     client-observable state as the current cut (same objects, attributes,
//     names, tombstones, retired allocations and payload bytes; fsservice
//     ControlledViewSource.ObservableDigest), reinstalling the current cut
//     changes nothing the peer can see, so stopping it buys nothing either.
//     An untargeted (discovery or renewed) lease says nothing about the
//     installed view: in the hfs_mmap_bootstrap loop the view can be older
//     than the lease target. Any such active peer lease keeps the
//     all-domain recall.
//
// Only the peers' state matters, not how the cut moved (task wf4, zq3-r18
// H1). Until wf4 the scope also required the expected target's cut to be
// observably equal to the current cut, i.e. a pin-only move (HandlePin/
// HandleRelease records), and every mutation or Reclaim between the two kept
// the all-domain recall. That condition describes only the requester's view,
// and the requester is recalled either way. For the peers it added nothing
// that the checks below do not already decide:
//
//   - A peer lease on a cut before the mutation is on an observably
//     different cut, so the lease check keeps the all-domain recall.
//   - A peer with no active lease and no open reservation is in exactly the
//     state the mutating CALL already left it in: the CALL's conflict check
//     looks only at active leases, so it recalled nothing in that domain, and
//     je4's recall-free release (observation_release.go) leaves idle peers
//     with stale views as a matter of course. That stale view is therefore
//     an already reachable, safe state that nothing promises to end early.
//     It ends at the peer's own next VFS read, whose observation acquisition
//     mismatches and recalls the peer itself. The guest serves nothing from
//     the view in between (the guest-side argument above and in
//     observation_release.go): no PTE without a PAGE lease, which is an
//     active host lease; cached dentries revalidate through an RPC that
//     takes an observation lease; attributes never become timeless pathname
//     permission. The peer's pins (LOOKUP/OPEN references, descriptions) are
//     host-side lifetime references, not cache authority: they keep objects
//     alive and let the peer read nothing without a lease. Recalling an idle
//     peer only moves its refresh earlier, at the cost of the global barrier.
//     In zq3-r18 T1 m1 was idle, and three of four EXPECTED_TARGET recalls
//     were all-domain at 7.5-9.7 s each, where the DOMAIN one took 6.3 s.
//   - A peer with an open (not CLOSED) reservation may still have bytes to
//     freeze and drain, and the all-domain recall drains them before its
//     install. A reservation is taken under an active lease, so the lease
//     check normally catches it, but the scope also refuses explicitly while
//     any domain holds an open reservation (anyReservationOpen). Side
//     effect: a pin-only mismatch, which e84 scoped regardless of peer
//     reservations, now also recalls all domains while any domain holds an
//     open reservation. That only narrows the scoping, so it is safe; it
//     adds a global barrier only when a peer's reservation is open while
//     its active leases (if any) would pass the lease check.
//
// The lease check needs each active peer lease's observable digest. The
// guest retires its observation tickets promptly (16 live tickets at the
// zq3 spec), compaction then drops the released leases and pruning drops
// the cuts only they pinned. The controller therefore remembers, in memory
// only, the observable digest of each cut it retained (observed_cuts.go,
// the last observedCutLimit cuts), keyed by the cut's identity including
// its state hash. Leases pin their cuts, so an active peer lease's digest
// is always derivable.
//
// mismatchRecallScope scopes the recall to the requesting domain when the
// request is READ, the requesting domain is clean (cleanReadDomain: no
// EXCLUSIVE_DIRTY lease, no open reservation), no domain holds an open
// reservation, and every active peer lease is Targeted on a cut observably
// equal to the current one (trivially true when the peers are idle). The
// scoped recall then has nothing to freeze or drain, exactly like the scoped
// release recall of task 784 (recall_scope.go), whose barrier rules apply
// unchanged: every cut-changing op of every domain still waits for it, so it
// installs one stable cut. A requester never goes without a recall: its
// guest needs the INSTALL_FINAL of the current cut to leave the stale view.
// The expected cut may be of another journal incarnation, epoch or serving
// generation (e.g. after a controller reopen) or have an unknown digest: that
// again concerns only the requester's view, which the scoped recall
// replaces, so the expected cut is not classified at all.
//
// Everything else keeps the all-domain recall: an EXCLUSIVE_DIRTY request,
// a requesting domain with dirty state, an open reservation in any domain,
// a current cut whose observable digest cannot be computed, and any active
// peer lease that is untargeted or on an observably different or unknown
// cut.

// mismatchRecallScope returns the session whose recall alone answers an
// expected-target mismatch of request r from scope, or "" when the mismatch
// needs the all-domain recall. current is the cut acquire just captured.
func (c *Controller) mismatchRecallScope(scope guestbridge.Context, r request, current fsservice.ControlledViewSource) string {
	session := scope.Session
	if c.allDomainMismatch || r.Mode != "READ" || r.Target == nil || !c.cleanReadDomain(session) || c.anyReservationOpen() {
		return ""
	}
	now, ok := c.digestOf(current)
	if !ok || !c.peerLeasesObservablyCurrent(session, now) {
		return ""
	}
	return session
}

// peerLeasesObservablyCurrent reports whether every active lease of a domain
// other than session is a Targeted observation lease, so its target is the
// peer's installed view, on a cut whose observable digest is now: then
// reinstalling the current cut in that domain would change nothing it can
// observe. An untargeted lease's target need not be the installed view. A
// peer without any active lease passes: it serves nothing from its view
// until its own observation mismatches (see above).
func (c *Controller) peerLeasesObservablyCurrent(session, now string) bool {
	for _, l := range c.state.Leases {
		if !l.Active || l.Lease.Scope.Session == session {
			continue
		}
		if !l.Targeted {
			return false
		}
		if digest, known := c.observedDigest(l.Lease.Scope, l.Lease.Target); !known || digest != now {
			return false
		}
	}
	return true
}
