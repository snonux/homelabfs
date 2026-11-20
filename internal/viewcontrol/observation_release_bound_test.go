package viewcontrol

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Lineage bounds of task je4 (review round 1): reopen refuses a ledger whose
// Revisions exceed Bounds.Identities, so grantLease blocks a new lineage at
// that bound, and a recall-free release needs Revisions below half of it.

// fillRevisions records n extra lineage high-waters without LineageFences
// entries (the pre-q64 shape that compaction never recycles), so they stay
// until the test ends.
func (r *rig) fillRevisions(n int) {
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	for k := range n {
		r.controller.state.Revisions[fmt.Sprintf("filler/%d", k)] = 1
	}
}

// direct sends a view op straight to the current controller: after reopen
// the rig's bridges still point at the closed one.
func (r *rig) direct(i int, op string, payload any) viewReply {
	r.t.Helper()
	raw, err := r.controller.ExchangeView(r.t.Context(), r.scopes[i], op, mustRaw(r.t, payload))
	must(r.t, err)
	var reply viewReply
	must(r.t, json.Unmarshal(raw, &reply))
	return reply
}

// revisionsRoom is how many more lineages fit below the Identities bound.
func (r *rig) revisionsRoom() int {
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	return r.controller.config.Authorization.Enrollment.Bounds.Identities - len(r.controller.state.Revisions)
}

// TestNewLineageBlockedAtRevisionsBound: with Revisions at Bounds.Identities a
// new lineage is BLOCKED while an existing active lineage is still shared,
// and a ledger at the bound reopens (and keeps blocking).
func TestNewLineageBlockedAtRevisionsBound(t *testing.T) {
	r := newZQ3Rig(t, 0)
	_, description := r.create(1, "page", false)
	held := r.domainRead(0)
	r.fillRevisions(r.revisionsRoom())
	if shared := r.domainRead(0); shared.ID != held.ID {
		t.Fatalf("existing lineage not shared at the bound: %s %s", shared.ID, held.ID)
	}
	if _, reply := r.acquire(1, page(description, 0), "READ"); reply.Status != "BLOCKED" {
		t.Fatalf("new lineage at the Revisions bound: %+v", reply)
	}
	r.reopen()
	if r.revisionsRoom() != 0 {
		t.Fatalf("reopened ledger room %d", r.revisionsRoom())
	}
	acquire := map[string]any{"request": r.ticket(1), "domain": r.domains[1], "access": page(description, 4096), "mode": "READ", "target": nil}
	if reply := r.direct(1, "VIEW_ACQUIRE", acquire); reply.Status != "BLOCKED" {
		t.Fatalf("new lineage after reopen at the bound: %+v", reply)
	}
}

// TestObservationReleaseRecallsAtHalfRevisions: once Revisions as a whole
// holds half of Bounds.Identities, an observation release keeps its scoped
// recall, although its own domain is far below lineageBudget.
func TestObservationReleaseRecallsAtHalfRevisions(t *testing.T) {
	r := newZQ3Rig(t, 0)
	lease := r.observeCurrent(1, guestbridge.ViewAccess{Kind: "DOMAIN"})
	r.fillRevisions(r.revisionsRoom() - r.controller.config.Authorization.Enrollment.Bounds.Identities/2)
	r.requireReleaseRecall(t, 1, lease, r.scopes[1].Session)
}

// TestTargetedLeaseSurvivesReopen: a reopened ledger keeps a lease's
// Targeted flag, and its release is still recall-free.
func TestTargetedLeaseSurvivesReopen(t *testing.T) {
	r := newZQ3Rig(t, 0)
	lease := r.observeCurrent(1, guestbridge.ViewAccess{Kind: "DOMAIN"})
	r.reopen()
	r.controller.mu.Lock()
	targeted := r.controller.state.Leases[lease.ID].Targeted
	r.controller.mu.Unlock()
	if !targeted {
		t.Fatal("reopen dropped Targeted")
	}
	payload := map[string]any{"request": r.controlTicket(1), "lease": lease}
	r.physical("VIEW_RELEASE", payload)
	if reply := r.direct(1, "VIEW_RELEASE", payload); reply.Status != "RELEASED" {
		t.Fatalf("release after reopen %+v", reply)
	}
	r.requireNoRecall(t, "release after reopen")
	if r.recallTotal() != 0 {
		t.Fatalf("recalls %d", r.recallTotal())
	}
}
