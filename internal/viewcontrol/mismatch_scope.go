package viewcontrol

import (
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Scoped expected-target recalls (task e84, after zq3-r14..r17).
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
//   - A peer WITH an active lease observes that lease's cut. If that cut holds
//     the same client-observable state as the current cut (same objects,
//     attributes, names, tombstones, retired allocations and payload bytes;
//     fsservice ControlledViewSource.ObservableDigest), reinstalling the
//     current cut changes nothing the peer can see, so stopping it buys
//     nothing either.
//
// Cut moves therefore fall in two classes. A pin-only move (HandlePin/
// HandleRelease records: a pinning LOOKUP or OPEN of a non-root object, a
// FORGET or final DESCRIPTION_RELEASE that reclaims nothing) changes only
// the image's position, pins and receipts, which no client operation reads.
// Any mutation (data, attributes, namespace) or Reclaim changes observable
// state; a peer lease on a cut before it may need the reinstall.
//
// The classification compares the endpoint cuts, not the journal records
// between them: the expected target's cut must be observably equal to the
// current cut, and so must the target of every active peer lease. Comparing
// endpoints is at least as strict as classifying the records: an
// intermediate change that a later one undid leaves nothing observable, and
// any surviving change fails the comparison. The comparison needs the
// expected cut's observable digest. The guest retires its observation
// tickets promptly (16 live tickets at the zq3 spec), compaction then drops
// the released leases and pruning drops the cuts only they pinned, so the
// expected cut is often gone from Sources by the time a peer's pin moves
// the cut. The controller therefore remembers, in memory only, the
// observable digest of each cut it retained (observed_cuts.go, the last
// observedCutLimit cuts), keyed by the cut's identity including its state
// hash. Leases pin their cuts, so a peer lease's digest is always
// derivable.
//
// mismatchRecallScope then scopes the recall to the requesting domain when,
// in addition, the request is READ and the requesting domain is clean
// (cleanReadDomain: no EXCLUSIVE_DIRTY lease, no open reservation), so the
// scoped recall has nothing to freeze or drain, exactly like the scoped
// release recall of task 784 (recall_scope.go), whose barrier rules apply
// unchanged: every cut-changing op of every domain still waits for it, so
// it installs one stable cut. A requester never goes without a recall: its
// guest needs the INSTALL_FINAL of the current cut to leave the stale view.
//
// Everything else keeps the all-domain recall: an EXCLUSIVE_DIRTY request,
// a requesting domain with dirty state, an expected cut whose digest is
// unknown (neither retained nor remembered, e.g. after a controller reopen;
// classifying it would need the journal records between the two positions,
// which the controller does not read), a different journal incarnation,
// epoch or serving generation, any observable change between the expected
// cut and the current one, and any active peer lease on an observably
// different cut.

// mismatchRecallScope returns the session whose recall alone answers an
// expected-target mismatch of request r from scope, or "" when the mismatch
// needs the all-domain recall. current is the cut acquire just captured.
func (c *Controller) mismatchRecallScope(scope guestbridge.Context, r request, current fsservice.ControlledViewSource) string {
	session := scope.Session
	if c.allDomainMismatch || r.Mode != "READ" || r.Target == nil || !c.cleanReadDomain(session) {
		return ""
	}
	now, ok := c.observe(current)
	if !ok {
		return ""
	}
	if expected, known := c.observedDigest(scope, *r.Target); !known || expected != now {
		return ""
	}
	if !c.peerLeasesObservablyCurrent(session, now) {
		return ""
	}
	return session
}

// peerLeasesObservablyCurrent reports whether every active lease of a domain
// other than session sits on a cut whose observable digest is now, so
// reinstalling the current cut in that domain would change nothing it can
// observe.
func (c *Controller) peerLeasesObservablyCurrent(session, now string) bool {
	for _, l := range c.state.Leases {
		if !l.Active || l.Lease.Scope.Session == session {
			continue
		}
		if digest, known := c.observedDigest(l.Lease.Scope, l.Lease.Target); !known || digest != now {
			return false
		}
	}
	return true
}
