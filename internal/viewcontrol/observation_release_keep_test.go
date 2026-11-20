package viewcontrol

import (
	"maps"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Releases that must still recall after task je4 (observation_release.go):
// each test breaks exactly one condition of a recall-free release and
// requires the release to answer PENDING and start the recall it started
// before je4, with that recall's scope.

// requireReleaseRecall sends lease's release on domain i, requires PENDING
// and a recall of the given scope ("" for all-domain), then completes it and
// requires RELEASED.
func (r *rig) requireReleaseRecall(t *testing.T, i int, lease *guestbridge.ViewLease, scope string) {
	t.Helper()
	payload := r.startRelease(i, lease)
	got, _, exists := r.recallSnapshot()
	if !exists || got != scope {
		t.Fatalf("release recall scope %q (exists %v), want %q", got, exists, scope)
	}
	if !r.leaseActive(lease) {
		t.Fatal("lease closed before the recall's STOPPED")
	}
	r.completeCleanRecall()
	r.physical("VIEW_RELEASE", payload)
	if reply := r.view(i, "VIEW_QUERY", map[string]any{"request": payload["request"]}); reply.Status != "RELEASED" {
		t.Fatalf("release after its recall %+v", reply)
	}
}

// TestDiscoveryReleaseKeepsRecall: a target:null DOMAIN READ lease (the
// mount's hfs_mmap_bootstrap discovery) keeps its scoped recall, which the
// bootstrap loop needs to install the current cut.
func TestDiscoveryReleaseKeepsRecall(t *testing.T) {
	r := newZQ3Rig(t, 0)
	r.domainRead(0)
	r.requireReleaseRecall(t, 1, r.domainRead(1), r.scopes[1].Session)
}

// TestDiscoveryHolderKeepsRecall: an observation lease that a discovery
// acquisition also holds keeps the recall for its last holder, whichever
// holder releases last.
func TestDiscoveryHolderKeepsRecall(t *testing.T) {
	r := newZQ3Rig(t, 0)
	observed := r.observeCurrent(1, guestbridge.ViewAccess{Kind: "DOMAIN"})
	discovery := r.domainRead(1)
	if discovery.ID != observed.ID {
		t.Fatalf("discovery did not share the observation lease")
	}
	if reply := r.release(1, observed); reply.Status != "RELEASED" {
		t.Fatalf("holder release %+v", reply)
	}
	r.requireReleaseRecall(t, 1, discovery, r.scopes[1].Session)
}

// TestMovedCutReleaseKeepsRecall: after a peer's authority reductions moved
// the cut under the lease (lifetime state, no conflict), the release keeps
// the scoped recall that installs the moved cut in the releasing domain.
func TestMovedCutReleaseKeepsRecall(t *testing.T) {
	r := newZQ3Rig(t, 0)
	path, description := r.create(0, "moves-cut", false)
	lease := r.observeCurrent(1, guestbridge.ViewAccess{Kind: "DOMAIN"})
	r.call(0, "DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": 1})
	r.call(0, "FORGET", map[string]any{"object": path, "count": 1})
	if equal(r.currentTarget(1), lease.Target) {
		t.Fatal("the reductions did not move the cut")
	}
	r.requireReleaseRecall(t, 1, lease, r.scopes[1].Session)
}

// TestPageObservationReleaseKeepsRecall: a targeted PAGE READ lease keeps
// its recall: the guest publishes cache objects under it until STOP.
func TestPageObservationReleaseKeepsRecall(t *testing.T) {
	r := newZQ3Rig(t, 0)
	_, description := r.create(1, "page", false)
	lease := r.observeCurrent(1, page(description, 0))
	r.requireReleaseRecall(t, 1, lease, r.scopes[1].Session)
}

// TestObservationReleasableConditions breaks each ledger condition of a
// recall-free release on one granted observation lease. A DOMAIN READ lease
// conflicts with every EXCLUSIVE_DIRTY lease, so a domain cannot normally
// hold both, and an open reservation needs an EXCLUSIVE_DIRTY lease; those
// states are injected to pin the defensive checks.
func TestObservationReleasableConditions(t *testing.T) {
	r := newZQ3Rig(t, 0)
	lease := r.observeCurrent(1, guestbridge.ViewAccess{Kind: "DOMAIN"})
	session := r.scopes[1].Session
	for _, c := range []struct {
		name   string
		change func(s *ledger, l *leaseState)
		want   bool
	}{
		{"clean observation lease", func(*ledger, *leaseState) {}, true},
		{"inactive", func(_ *ledger, l *leaseState) { l.Active = false }, false},
		{"shared", func(_ *ledger, l *leaseState) { l.Sharers = 1 }, false},
		{"untargeted", func(_ *ledger, l *leaseState) { l.Targeted = false }, false},
		{"OBJECT access", func(_ *ledger, l *leaseState) { l.Lease.Access.Kind = "OBJECT" }, false},
		{"EXCLUSIVE_DIRTY mode", func(_ *ledger, l *leaseState) { l.Lease.Mode = "EXCLUSIVE_DIRTY" }, false},
		{"renewed sibling active", func(s *ledger, l *leaseState) {
			sibling := *l
			sibling.Lease.ID, sibling.Lease.Revision = "sibling", l.Lease.Revision+1
			s.Leases["sibling"] = sibling
		}, false},
		{"domain holds an EXCLUSIVE_DIRTY lease", func(s *ledger, l *leaseState) {
			write := *l
			write.Lease.ID, write.Lease.Mode = "write", "EXCLUSIVE_DIRTY"
			s.Leases["write"] = write
		}, false},
		{"domain holds an open reservation", func(s *ledger, _ *leaseState) {
			s.Reservations["open"] = reservationState{Reservation: guestbridge.DirtyReservation{ID: "open", Scope: guestbridge.Context{Session: session}}, Status: "RESERVED"}
		}, false},
		{"domain at its lineage budget", func(s *ledger, _ *leaseState) {
			fillLineages(s, session, r.controller.lineageBudget()-1, 1)
		}, false},
		{"older-fence lineages wait for compaction only", func(s *ledger, _ *leaseState) {
			fillLineages(s, session, r.controller.lineageBudget(), 0)
		}, true},
	} {
		if got := releasableAfter(r, lease, c.change); got != c.want {
			t.Errorf("%s: releasable %v, want %v", c.name, got, c.want)
		}
	}
}

// releasableAfter applies change to a copy of the controller's ledger and
// lease, evaluates observationReleasable and restores the ledger.
func releasableAfter(r *rig, lease *guestbridge.ViewLease, change func(*ledger, *leaseState)) bool {
	c := r.controller
	c.mu.Lock()
	defer c.mu.Unlock()
	saved := c.state
	c.state.Leases = map[string]leaseState{}
	for id, l := range saved.Leases {
		c.state.Leases[id] = l
	}
	c.state.Reservations = map[string]reservationState{}
	c.state.Revisions = maps.Clone(saved.Revisions)
	c.state.LineageFences = maps.Clone(saved.LineageFences)
	l := c.state.Leases[lease.ID]
	change(&c.state, &l)
	c.state.Leases[lease.ID] = l
	got := c.observationReleasable(l)
	c.state = saved
	return got
}

// fillLineages records n further lease lineages of session at fence, as n
// earlier guest processes' observations leave them until a recall moves the
// domain fence past them.
func fillLineages(s *ledger, session string, n int, fence uint64) {
	for k := range n {
		id := session + "/lineage/" + string(rune('a'+k))
		s.Revisions[id] = 1
		s.LineageFences[id] = lineageFence{Session: session, Fence: fence}
	}
}

// TestObservationReleaseLineageBudgetRecalls: once the releasing domain holds
// lineageBudget lineages at its fence (one per earlier guest process), an
// observation release keeps its scoped recall, which moves the fence so that
// compaction can recycle them; afterwards the domain has headroom again.
func TestObservationReleaseLineageBudgetRecalls(t *testing.T) {
	r := newZQ3Rig(t, 0)
	lease := r.observeCurrent(1, guestbridge.ViewAccess{Kind: "DOMAIN"})
	session := r.scopes[1].Session
	r.controller.mu.Lock()
	fillLineages(&r.controller.state, session, r.controller.lineageBudget()-1, 1)
	r.controller.mu.Unlock()
	r.requireReleaseRecall(t, 1, lease, session)
	r.controller.mu.Lock()
	headroom := r.controller.lineageHeadroom(session)
	r.controller.mu.Unlock()
	if !headroom {
		t.Fatal("no lineage headroom after the recall moved the fence")
	}
}
