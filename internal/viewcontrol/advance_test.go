package viewcontrol

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Guest-requested view advance (task xf4, advance.go): the grant path, its
// replay, retransmit, readmit and reopen, the recall-free release of an
// Advance lease whose cut moved, holder sharing and the lineage budget. The
// negative per condition is in advance_conditions_test.go.

// advancePayload is domain i's observation acquire on ticket expecting
// expected, with "advance": true.
func (r *rig) advancePayload(i int, ticket guestbridge.Ticket, expected guestbridge.ViewTarget) map[string]any {
	return map[string]any{"request": ticket, "domain": r.domains[i], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": expected, "advance": true}
}

// advanceAcquire sends domain i's advance observation acquire expecting
// expected and returns its ticket and reply.
func (r *rig) advanceAcquire(i int, expected guestbridge.ViewTarget) (guestbridge.Ticket, viewReply) {
	r.t.Helper()
	ticket := r.ticket(i)
	return ticket, r.view(i, "VIEW_ACQUIRE", r.advancePayload(i, ticket, expected))
}

// advances is the controller's advance counter as OBSERVE reports it.
func (r *rig) advances() uint64 {
	r.t.Helper()
	return history(r.t, r.controller).Advances
}

// leaseState returns the live ledger state of lease.
func (r *rig) leaseState(lease *guestbridge.ViewLease) leaseState {
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	return r.controller.state.Leases[lease.ID]
}

// staleView returns domain i's installed view and moves the cut past it by
// a CREATE on m0, which no lease conflicts with: the state every guest
// observation after a mutation finds. m0 then drops the file's lifetimes,
// so a later LOOKUP of name journals a handle pin (pinLookup).
func (r *rig) staleView(i int, name string) guestbridge.ViewTarget {
	r.t.Helper()
	installed := r.currentTarget(i)
	path, description := r.create(0, name, false)
	r.dropLifetimes(0, path, description)
	if equal(installed, r.currentTarget(i)) {
		r.t.Fatal("the CREATE did not move the cut")
	}
	return installed
}

// pinLookup is domain i's pinning LOOKUP of name (a read, so it conflicts
// with no READ lease): it moves the cut, also under i's own lease.
func (r *rig) pinLookup(i int, name string) {
	r.t.Helper()
	before := r.currentTarget(i)
	root := r.call(i, "ROOT", map[string]any{}).Capabilities[0]
	if res := r.call(i, "LOOKUP", map[string]any{"parent": root, "name": hex.EncodeToString([]byte(name))}); res.Code != "OK" {
		r.t.Fatalf("LOOKUP %s: %+v", name, res)
	}
	if equal(before, r.currentTarget(i)) {
		r.t.Fatal("the LOOKUP did not move the cut")
	}
}

// requireAdvanced requires reply to be GRANTED by advance on domain i's
// current cut at its unchanged fence, with no recall started.
func (r *rig) requireAdvanced(t *testing.T, i int, reply viewReply) *guestbridge.ViewLease {
	t.Helper()
	if reply.Status != "GRANTED" || reply.Lease == nil {
		t.Fatalf("advance acquire %d: %+v", i, reply)
	}
	if !equal(reply.Lease.Target, r.currentTarget(i)) || reply.Lease.Fence != r.controller.fence(r.scopes[i].Session) {
		t.Fatalf("advance lease %+v not on the current cut and fence", reply.Lease)
	}
	r.requireNoRecall(t, "advance grant", reply.Lease)
	return reply.Lease
}

// TestAdvanceGrantsCurrentCut: a stale advance observation of a clean domain
// with idle peers is GRANTED on the current cut without a recall, as a
// Targeted Advance lease; the advance counter counts it once, and the exact
// retransmit and the QUERY return the protected bytes without counting
// again.
func TestAdvanceGrantsCurrentCut(t *testing.T) {
	r := newZQ3Rig(t, 0)
	installed := r.staleView(1, "advance-grant")
	ticket, reply := r.advanceAcquire(1, installed)
	lease := r.requireAdvanced(t, 1, reply)
	if state := r.leaseState(lease); !state.Targeted || !state.Advance || state.Sharers != 0 {
		t.Fatalf("advance lease state %+v", state)
	}
	if r.recallTotal() != 0 || r.advances() != 1 {
		t.Fatalf("recalls %d, advances %d", r.recallTotal(), r.advances())
	}
	first := r.directView(1, "VIEW_ACQUIRE", r.advancePayload(1, ticket, installed))
	again := r.directView(1, "VIEW_ACQUIRE", r.advancePayload(1, ticket, installed))
	query := r.directView(1, "VIEW_QUERY", map[string]any{"request": ticket})
	if !bytes.Equal(first, again) || !bytes.Equal(first, query) || status(first) != "GRANTED" {
		t.Fatalf("retransmit %s, again %s, query %s", first, again, query)
	}
	if r.advances() != 1 {
		t.Fatalf("a replay counted as an advance: %d", r.advances())
	}
}

// TestAdvanceLeaseReleaseSkipsRecallAfterMove: the Advance lease's own
// pinning LOOKUP moves the cut under it; its release is still RELEASED
// without a recall (je4's current-cut condition is relaxed for Advance
// leases), and the next stale advance observation is granted again.
func TestAdvanceLeaseReleaseSkipsRecallAfterMove(t *testing.T) {
	r := newZQ3Rig(t, 0)
	installed := r.staleView(1, "advance-release")
	_, reply := r.advanceAcquire(1, installed)
	lease := r.requireAdvanced(t, 1, reply)
	r.pinLookup(1, "advance-release")
	if released := r.release(1, lease); released.Status != "RELEASED" {
		t.Fatalf("advance release %+v", released)
	}
	r.requireNoRecall(t, "advance release")
	if r.leaseActive(lease) || r.recallTotal() != 0 || r.controller.fence(r.scopes[1].Session) != 1 {
		t.Fatalf("lease active %v, recalls %d, fence %d", r.leaseActive(lease), r.recallTotal(), r.controller.fence(r.scopes[1].Session))
	}
	_, next := r.advanceAcquire(1, lease.Target)
	r.requireAdvanced(t, 1, next)
	if r.advances() != 2 {
		t.Fatalf("advances %d", r.advances())
	}
}

// TestAdvanceOnCurrentCutIsAdvanceLease: an advance request whose expected
// target is current is an ordinary grant (no advance counted), but its lease
// is Advance too: every holder is an advancing observation, so its release
// skips the recall after its own pinning LOOKUP as well.
func TestAdvanceOnCurrentCutIsAdvanceLease(t *testing.T) {
	r := newZQ3Rig(t, 0)
	path, description := r.create(1, "advance-current", false)
	r.dropLifetimes(1, path, description)
	_, reply := r.advanceAcquire(1, r.currentTarget(1))
	lease := r.requireAdvanced(t, 1, reply)
	if !r.leaseState(lease).Advance || r.advances() != 0 {
		t.Fatalf("lease %+v, advances %d", r.leaseState(lease), r.advances())
	}
	r.pinLookup(1, "advance-current")
	if released := r.release(1, lease); released.Status != "RELEASED" {
		t.Fatalf("release %+v", released)
	}
	r.requireNoRecall(t, "advance release on a current-cut grant")
}

// TestAdvanceSharedLeaseAndsAdvance: an ordinary observation that shares an
// Advance lease clears Advance for good (shareLease), so after a cut move
// the last holder's release keeps je4's scoped recall; two advancing holders
// keep it.
func TestAdvanceSharedLeaseAndsAdvance(t *testing.T) {
	r := newZQ3Rig(t, 0)
	_, reply := r.advanceAcquire(1, r.staleView(1, "advance-shared"))
	lease := r.requireAdvanced(t, 1, reply)
	_, second := r.advanceAcquire(1, lease.Target)
	if second.Lease == nil || second.Lease.ID != lease.ID || !r.leaseState(lease).Advance {
		t.Fatalf("advancing holder: %+v, state %+v", second, r.leaseState(lease))
	}
	plain := r.observeCurrent(1, guestbridge.ViewAccess{Kind: "DOMAIN"})
	if plain.ID != lease.ID || r.leaseState(lease).Advance || !r.leaseState(lease).Targeted {
		t.Fatalf("ordinary holder: %s, state %+v", plain.ID, r.leaseState(lease))
	}
	for range 2 {
		if released := r.release(1, lease); released.Status != "RELEASED" {
			t.Fatalf("holder release %+v", released)
		}
	}
	r.pinLookup(1, "advance-shared")
	r.requireReleaseRecall(t, 1, lease, r.scopes[1].Session)
}

// TestAdvanceReadmitAfterRunningRecall: an advance acquire that arrives while
// a peer's recall runs (condition 2) is PENDING and starts nothing; once the
// recall ended, its QUERY readmits it and it is granted by advance.
func TestAdvanceReadmitAfterRunningRecall(t *testing.T) {
	r := newZQ3Rig(t, 0)
	installed := r.staleView(1, "advance-readmit")
	r.startRelease(0, r.domainRead(0)) // m0's discovery release recalls m0
	ticket, reply := r.advanceAcquire(1, installed)
	if reply.Status != "PENDING" || r.recallTotal() != 1 || r.advances() != 0 {
		t.Fatalf("advance during a recall: %+v, recalls %d, advances %d", reply, r.recallTotal(), r.advances())
	}
	r.completeCleanRecall()
	readmitted := r.view(1, "VIEW_QUERY", map[string]any{"request": ticket})
	r.requireAdvanced(t, 1, readmitted)
	if r.recallTotal() != 1 || r.advances() != 1 {
		t.Fatalf("recalls %d, advances %d", r.recallTotal(), r.advances())
	}
}

// TestAdvanceLeaseSurvivesReopen: a reopened ledger keeps the Advance flag
// (the counter is memory only and starts at 0), and the release after the
// lease's own pinning LOOKUP moved the cut is still recall-free.
func TestAdvanceLeaseSurvivesReopen(t *testing.T) {
	r := newZQ3Rig(t, 0)
	_, reply := r.advanceAcquire(1, r.staleView(1, "advance-reopen"))
	lease := r.requireAdvanced(t, 1, reply)
	r.pinLookup(1, "advance-reopen")
	r.reopen()
	if !r.leaseState(lease).Advance || r.advances() != 0 {
		t.Fatalf("after reopen: state %+v, advances %d", r.leaseState(lease), r.advances())
	}
	payload := map[string]any{"request": r.controlTicket(1), "lease": lease}
	r.physical("VIEW_RELEASE", payload)
	if released := r.direct(1, "VIEW_RELEASE", payload); released.Status != "RELEASED" {
		t.Fatalf("release after reopen %+v", released)
	}
	r.requireNoRecall(t, "advance release after reopen")
}

// TestAdvanceReleaseLineageBudgetRecalls: once the domain holds lineageBudget
// lineages at its fence, an Advance lease's release keeps its scoped recall
// like every observation release, which moves the fence for compaction.
func TestAdvanceReleaseLineageBudgetRecalls(t *testing.T) {
	r := newZQ3Rig(t, 0)
	_, reply := r.advanceAcquire(1, r.staleView(1, "advance-budget"))
	lease := r.requireAdvanced(t, 1, reply)
	session := r.scopes[1].Session
	r.controller.mu.Lock()
	fillLineages(&r.controller.state, session, r.controller.lineageBudget()-1, 1)
	r.controller.mu.Unlock()
	r.requireReleaseRecall(t, 1, lease, session)
	if r.controller.fence(session) != 2 {
		t.Fatalf("fence %d after the budget recall", r.controller.fence(session))
	}
}
