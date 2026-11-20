package viewcontrol

import (
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Recall-free observation releases (task je4, observation_release.go). The
// first tests pin the skip; the keep tests pin every condition under which a
// release must still recall; the replay counts the recalls of guest.py's
// credentials admission before and after je4.

// currentTarget is the target the controller would grant domain i now: the
// view a guest installed at the current cut expects.
func (r *rig) currentTarget(i int) guestbridge.ViewTarget {
	r.t.Helper()
	source, err := r.controller.capture(r.t.Context())
	must(r.t, err)
	return r.controller.target(r.scopes[i], source)
}

// observeCurrent is a guest observation acquire of domain i on its current view
// (expected_target = the current cut); it must be GRANTED.
func (r *rig) observeCurrent(i int, access guestbridge.ViewAccess) *guestbridge.ViewLease {
	r.t.Helper()
	reply := r.view(i, "VIEW_ACQUIRE", map[string]any{"request": r.ticket(i), "domain": r.domains[i], "access": access, "mode": "READ", "target": r.currentTarget(i)})
	if reply.Status != "GRANTED" || reply.Lease == nil {
		r.t.Fatalf("observation %d: %+v", i, reply)
	}
	return reply.Lease
}

// recallTotal is the number of recalls the controller started.
func (r *rig) recallTotal() uint64 {
	r.t.Helper()
	return history(r.t, r.controller).Total
}

// TestObservationReleaseSkipsRecall: the last holder's release of a clean
// DOMAIN READ observation lease on the current cut is RELEASED at once,
// starts no recall, deactivates the lease and moves no fence; the next
// observation is granted on the same installed view.
func TestObservationReleaseSkipsRecall(t *testing.T) {
	r := newZQ3Rig(t, 0)
	peer := r.observeCurrent(0, guestbridge.ViewAccess{Kind: "DOMAIN"})
	lease := r.observeCurrent(1, guestbridge.ViewAccess{Kind: "DOMAIN"})
	if reply := r.release(1, lease); reply.Status != "RELEASED" {
		t.Fatalf("observation release %+v", reply)
	}
	r.requireNoRecall(t, "observation release", peer)
	if r.leaseActive(lease) || r.recallTotal() != 0 {
		t.Fatalf("lease active %v, recalls %d", r.leaseActive(lease), r.recallTotal())
	}
	if r.controller.fence(r.scopes[1].Session) != 1 {
		t.Fatalf("fence moved to %d", r.controller.fence(r.scopes[1].Session))
	}
	next := r.observeCurrent(1, guestbridge.ViewAccess{Kind: "DOMAIN"})
	if next.ID == lease.ID || next.Fence != lease.Fence || !equal(next.Target, lease.Target) {
		t.Fatalf("next observation %+v after %+v", next, lease)
	}
}

// TestObservationReleaseRefusedByFactsKeepsLease: without the Facts'
// certification the recall-free release fails and leaves the lease active
// and its release credit reserved, so the guest's retry finds the same state.
func TestObservationReleaseRefusedByFactsKeepsLease(t *testing.T) {
	r := newZQ3Rig(t, 0)
	lease := r.observeCurrent(1, guestbridge.ViewAccess{Kind: "DOMAIN"})
	payload := map[string]any{"request": r.controlTicket(1), "lease": lease}
	if _, err := r.execute(1, "VIEW_RELEASE", payload); !errors.Is(err, fsapi.Unavailable) {
		t.Fatalf("uncertified release: %v", err)
	}
	r.controller.mu.Lock()
	state := r.controller.state.Leases[lease.ID]
	r.controller.mu.Unlock()
	if !state.Active || !state.ReleaseCredit {
		t.Fatalf("refused release changed the lease: %+v", state)
	}
	r.physical("VIEW_RELEASE", payload)
	if reply := r.view(1, "VIEW_RELEASE", payload); reply.Status != "RELEASED" {
		t.Fatalf("retried release %+v", reply)
	}
	r.requireNoRecall(t, "retried release")
}

// TestObservationReleaseWaitsForRunningRecall: a release that arrives while
// another domain's scoped recall runs is PENDING, as every release is; once
// that recall ended, its QUERY is RELEASED without a recall of its own.
func TestObservationReleaseWaitsForRunningRecall(t *testing.T) {
	r := newZQ3Rig(t, 0)
	lease := r.observeCurrent(1, guestbridge.ViewAccess{Kind: "DOMAIN"})
	r.startRelease(0, r.domainRead(0)) // m0's discovery release recalls m0
	payload := r.startRelease(1, lease)
	r.completeCleanRecall()
	r.physical("VIEW_RELEASE", payload)
	if reply := r.view(1, "VIEW_QUERY", map[string]any{"request": payload["request"]}); reply.Status != "RELEASED" {
		t.Fatalf("observation release after the peer recall %+v", reply)
	}
	r.requireNoRecall(t, "observation release after the peer recall")
	if r.recallTotal() != 1 || r.controller.fence(r.scopes[1].Session) != 1 {
		t.Fatalf("recalls %d, m1 fence %d", r.recallTotal(), r.controller.fence(r.scopes[1].Session))
	}
}

// TestSharedObservationReleaseSkipsRecall: two observations share one lease
// (q84); the first release returns its holder reference, the last one is
// recall-free too.
func TestSharedObservationReleaseSkipsRecall(t *testing.T) {
	r := newZQ3Rig(t, 0)
	first := r.observeCurrent(1, guestbridge.ViewAccess{Kind: "DOMAIN"})
	second := r.observeCurrent(1, guestbridge.ViewAccess{Kind: "DOMAIN"})
	if first.ID != second.ID {
		t.Fatalf("observations did not share the lease: %s %s", first.ID, second.ID)
	}
	for _, lease := range []*guestbridge.ViewLease{first, second} {
		if reply := r.release(1, lease); reply.Status != "RELEASED" {
			t.Fatalf("shared release %+v", reply)
		}
	}
	r.requireNoRecall(t, "shared observation releases")
	if r.leaseActive(first) || r.recallTotal() != 0 {
		t.Fatalf("lease active %v, recalls %d", r.leaseActive(first), r.recallTotal())
	}
}
