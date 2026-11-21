package viewcontrol

import (
	"maps"
	"testing"

	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Negatives of the advance conditions (task xf4, advance.go). Each breaks
// exactly one condition of advanceGrantable on an otherwise grantable stale
// observation; end to end, the request then takes the pre-xf4 path:
// PENDING, the expected-target recall, BLOCKED for the old view. (Condition
// 2, a running recall, instead leaves the request PENDING without a recall
// of its own and grants it by advance on readmit:
// TestAdvanceReadmitAfterRunningRecall.)

// requireAdvanceFallback sends domain i's advance acquire expecting expected
// and requires the pre-xf4 mismatch path: PENDING, an EXPECTED_TARGET recall
// of scope ("" for all-domain) with exactly participants, and BLOCKED once
// that recall completed, with no advance counted.
func (r *rig) requireAdvanceFallback(t *testing.T, i int, expected guestbridge.ViewTarget, scope string, participants ...int) {
	t.Helper()
	ticket, reply := r.advanceAcquire(i, expected)
	if reply.Status != "PENDING" {
		t.Fatalf("advance acquire %d: %+v", i, reply)
	}
	r.requireRecallScope(t, scope, participants...)
	r.finishMismatch(t, i, ticket)
	records := history(t, r.controller).Recalls
	if last := records[len(records)-1]; last.Kind != TriggerExpectedTarget || r.advances() != 0 {
		t.Fatalf("history %+v, advances %d", last, r.advances())
	}
}

// TestAdvanceIncompatibleTargetFallsBack (condition 3): an expected target
// ahead of the current position is no advance; the requester's scoped
// mismatch recall runs.
func TestAdvanceIncompatibleTargetFallsBack(t *testing.T) {
	r := newZQ3Rig(t, 0)
	installed := r.staleView(1, "advance-ahead")
	cut := *r.currentTarget(1).Cut
	cut.JournalPosition += 5
	ahead := guestbridge.ViewTarget{Kind: installed.Kind, Cut: &cut}
	r.requireAdvanceFallback(t, 1, ahead, r.scopes[1].Session, 1)
}

// TestAdvanceUntargetedPeerFallsBack (condition 4): a peer's active
// discovery lease says nothing about its installed view, so wf4 keeps the
// all-domain recall, and so does the advance. The ticket stays BLOCKED after
// that recall although every condition would now hold (condition 1: a
// ticket whose mismatch recall ran is never granted by advance).
func TestAdvanceUntargetedPeerFallsBack(t *testing.T) {
	r := newZQ3Rig(t, 0)
	installed := r.staleView(1, "advance-peer")
	r.domainRead(0)
	r.requireAdvanceFallback(t, 1, installed, "", 0, 1)
}

// TestAdvanceCacheLeaseFallsBack (condition 5): a requester holding a PAGE
// lease may have target-bound cache published under it; its scoped mismatch
// recall runs (mismatchRecallScope alone would allow the advance: a PAGE
// READ lease leaves the domain clean).
func TestAdvanceCacheLeaseFallsBack(t *testing.T) {
	r := newZQ3Rig(t, 0)
	installed := r.staleView(1, "advance-page")
	r.pageRead(1)
	r.requireAdvanceFallback(t, 1, installed, r.scopes[1].Session, 1)
}

// TestAdvanceWithoutFieldFallsBack (condition 1): the same stale observation
// without advance keeps the pre-xf4 path exactly.
func TestAdvanceWithoutFieldFallsBack(t *testing.T) {
	r := newZQ3Rig(t, 0)
	installed := r.staleView(1, "advance-absent")
	ticket := r.staleAcquire(1, installed)
	r.requireRecallScope(t, r.scopes[1].Session, 1)
	r.finishMismatch(t, 1, ticket)
	if r.advances() != 0 {
		t.Fatalf("advances %d", r.advances())
	}
}

// TestAdvanceGrantableConditions evaluates advanceGrantable on one stale
// observation with each condition broken in the ledger or request alone
// (the bridge schema already refuses advance with another mode or access;
// the controller checks them again).
func TestAdvanceGrantableConditions(t *testing.T) {
	r := newZQ3Rig(t, 0)
	installed := r.staleView(1, "advance-table")
	peer := r.observeCurrent(0, guestbridge.ViewAccess{Kind: "DOMAIN"})
	for _, c := range []struct {
		name   string
		change func(*ledger, *request, *ticketState)
		want   bool
	}{
		{"grantable", func(*ledger, *request, *ticketState) {}, true},
		{"no advance", func(_ *ledger, q *request, _ *ticketState) { q.Advance = false }, false},
		{"mismatch recall ran", func(_ *ledger, _ *request, t *ticketState) { t.ExpectedMismatch = true }, false},
		{"EXCLUSIVE_DIRTY", func(_ *ledger, q *request, _ *ticketState) { q.Mode = "EXCLUSIVE_DIRTY" }, false},
		{"OBJECT access", func(_ *ledger, q *request, _ *ticketState) { q.Access.Kind = "OBJECT" }, false},
		{"recall running", func(s *ledger, _ *request, _ *ticketState) { s.Recall = &recallState{ID: "running"} }, false},
		{"other epoch", func(_ *ledger, q *request, _ *ticketState) {
			cut := *q.Target.Cut
			cut.JournalEpoch++
			q.Target = &guestbridge.ViewTarget{Kind: q.Target.Kind, Cut: &cut}
		}, false},
		{"peer lease untargeted", func(s *ledger, _ *request, _ *ticketState) {
			l := s.Leases[peer.ID]
			l.Targeted = false
			s.Leases[peer.ID] = l
		}, false},
		{"requester holds an EXCLUSIVE_DIRTY lease", func(s *ledger, _ *request, _ *ticketState) {
			// DOMAIN access, so only condition 4 (a clean requester) breaks;
			// a PAGE lease would also break condition 5 (round-2 review).
			s.Leases["write"] = leaseState{Active: true, Lease: guestbridge.ViewLease{ID: "write", Scope: r.scopes[1], Access: guestbridge.ViewAccess{Kind: "DOMAIN"}, Mode: "EXCLUSIVE_DIRTY"}}
		}, false},
		{"open reservation in a peer", func(s *ledger, _ *request, _ *ticketState) {
			s.Reservations = maps.Clone(s.Reservations)
			s.Reservations["open"] = reservationState{Reservation: guestbridge.DirtyReservation{ID: "open", Scope: r.scopes[0]}, Status: "RESERVED"}
		}, false},
		{"Revisions full", func(s *ledger, _ *request, _ *ticketState) {
			s.Revisions = maps.Clone(s.Revisions)
			for k := len(s.Revisions); k < r.controller.config.Authorization.Enrollment.Bounds.Identities; k++ {
				s.Revisions["filler/"+string(rune('A'+k))] = 1
			}
		}, false},
		{"Sources full without the current cut", func(s *ledger, _ *request, _ *ticketState) {
			s.Sources = map[string]fsservice.ControlledViewSource{}
			for k := range r.controller.config.Authorization.Enrollment.Bounds.Sources {
				s.Sources["filler/"+string(rune('A'+k))] = fsservice.ControlledViewSource{}
			}
		}, false},
		{"requester holds a PAGE lease", func(s *ledger, _ *request, _ *ticketState) {
			s.Leases["page"] = leaseState{Active: true, Lease: guestbridge.ViewLease{ID: "page", Scope: r.scopes[1], Access: guestbridge.ViewAccess{Kind: "PAGE"}, Mode: "READ"}}
		}, false},
	} {
		if got := grantableAfter(r, installed, c.change); got != c.want {
			t.Errorf("%s: grantable %v, want %v", c.name, got, c.want)
		}
	}
}

// grantableAfter evaluates advanceGrantable for domain 1's advance request
// expecting installed after change edited copies of the ledger, request and
// ticket, and restores the ledger.
func grantableAfter(r *rig, installed guestbridge.ViewTarget, change func(*ledger, *request, *ticketState)) bool {
	source, err := r.controller.capture(r.t.Context())
	must(r.t, err)
	c := r.controller
	c.mu.Lock()
	defer c.mu.Unlock()
	saved := c.state
	c.state.Leases = maps.Clone(saved.Leases)
	q := request{Advance: true, Mode: "READ", Access: guestbridge.ViewAccess{Kind: "DOMAIN"}, Target: &installed}
	var ticket ticketState
	change(&c.state, &q, &ticket)
	got := c.advanceGrantable(r.scopes[1], q, ticket, source, c.target(r.scopes[1], source))
	c.state = saved
	return got
}

// TestAdvanceAtLineageCapacityFallsBack (condition 6): with Revisions at
// Bounds.Identities a new lineage would be BLOCKED. The advance request
// takes the recall path instead, as before xf4, rather than a BLOCKED answer
// that every retry would meet again. (The Sources bound is pinned by
// TestAdvanceGrantableConditions only: with Sources full and the current
// cut unretained, the fallback recall itself cannot retain its install cut
// and refuses with NoSpace, exactly as the same request did before xf4.)
func TestAdvanceAtLineageCapacityFallsBack(t *testing.T) {
	r := newZQ3Rig(t, 0)
	installed := r.staleView(1, "advance-lineages")
	r.fillRevisions(r.revisionsRoom())
	r.requireAdvanceFallback(t, 1, installed, r.scopes[1].Session, 1)
}

// TestAdvanceDivergentJournalFallsBack (condition 3): an expected cut at the
// current position with another journal hash is a divergent history, not
// an older view; the requester's scoped mismatch recall runs.
func TestAdvanceDivergentJournalFallsBack(t *testing.T) {
	r := newZQ3Rig(t, 0)
	cut := *r.currentTarget(1).Cut
	cut.JournalHash = hash([]byte("divergent"))
	divergent := guestbridge.ViewTarget{Kind: "CONTROLLED_LOCAL", Cut: &cut}
	r.requireAdvanceFallback(t, 1, divergent, r.scopes[1].Session, 1)
}

// TestAdvanceOlderPeerCutFallsBack (condition 4): a peer observation lease
// Targeted on an observably older cut keeps wf4's all-domain recall, and so
// the advance. A real peer lease gets there only across a Reclaim (see
// TestOpenUnlinkedFinalReleaseMismatch), so the peer lease's target is set
// to the retained cut before the requester's view moved.
func TestAdvanceOlderPeerCutFallsBack(t *testing.T) {
	r := newZQ3Rig(t, 0)
	older := r.observeReleased(0).Target // retained, digest remembered
	installed := r.staleView(1, "advance-older-peer")
	peer := r.observeCurrent(0, guestbridge.ViewAccess{Kind: "DOMAIN"})
	r.controller.mu.Lock()
	l := r.controller.state.Leases[peer.ID]
	l.Lease.Target = older
	r.controller.state.Leases[peer.ID] = l
	r.controller.mu.Unlock()
	r.requireAdvanceFallback(t, 1, installed, "", 0, 1)
}

// TestAdvanceOpenReservationFallsBack (condition 4): an open reservation in
// any domain keeps the all-domain recall. A reservation lives under an
// EXCLUSIVE_DIRTY lease, which a DOMAIN READ acquisition conflicts with
// before it is classified at all, so it is injected here; the clean test
// helper cannot complete a recall that owes a drain, so the test stops at
// the fallback's PENDING and its all-domain recall.
func TestAdvanceOpenReservationFallsBack(t *testing.T) {
	r := newZQ3Rig(t, 0)
	installed := r.staleView(1, "advance-reservation")
	r.controller.mu.Lock()
	r.controller.state.Reservations["open"] = reservationState{Reservation: guestbridge.DirtyReservation{ID: "open", Scope: r.scopes[0]}, Status: "RESERVED"}
	r.controller.mu.Unlock()
	if _, reply := r.advanceAcquire(1, installed); reply.Status != "PENDING" || r.advances() != 0 {
		t.Fatalf("advance beside an open reservation: %+v, advances %d", reply, r.advances())
	}
	r.requireRecallScope(t, "", 1) // all-domain; m0 holds no view, so gets no event
	records := history(t, r.controller).Recalls
	if last := records[len(records)-1]; last.Kind != TriggerExpectedTarget {
		t.Fatalf("history %+v", last)
	}
}

// TestAdvanceExclusiveRequesterFallsBack (condition 4): a requester holding
// an EXCLUSIVE_DIRTY lease is not clean. Its DOMAIN READ acquisition
// conflicts with that lease and starts the all-domain conflict recall before
// any classification, so it is never advanced. (A readmit after that recall
// is classified afresh; an advance grant there would be refused by the
// guest, whose revision moved, and handed back unadopted.)
func TestAdvanceExclusiveRequesterFallsBack(t *testing.T) {
	r := newZQ3Rig(t, 0)
	_, description := r.create(1, "advance-exclusive", false)
	installed := r.currentTarget(1)
	r.create(0, "advance-exclusive-move", false)
	if _, write := r.acquire(1, page(description, 0), "EXCLUSIVE_DIRTY"); write.Status != "GRANTED" {
		t.Fatalf("EXCLUSIVE_DIRTY lease: %+v", write)
	}
	if _, reply := r.advanceAcquire(1, installed); reply.Status != "PENDING" || r.advances() != 0 {
		t.Fatalf("advance beside an own EXCLUSIVE_DIRTY lease: %+v, advances %d", reply, r.advances())
	}
	r.requireRecallScope(t, "", 1) // all-domain; m0 holds no view, so gets no event
	records := history(t, r.controller).Recalls
	if last := records[len(records)-1]; last.Kind != TriggerAcquireConflict {
		t.Fatalf("history %+v", last)
	}
	r.completeCleanRecall()
}

// TestAdvanceLeaseRecalledByPeerCall: right after an advance grant a peer's
// mutation conflicts with the requester's Advance lease and starts the
// all-domain CALL_CONFLICT recall, which covers the requester exactly as for
// any active lease; the lease is closed by its STOPPED and its later release
// is RELEASED without another recall.
func TestAdvanceLeaseRecalledByPeerCall(t *testing.T) {
	r := newZQ3Rig(t, 0)
	_, reply := r.advanceAcquire(1, r.staleView(1, "advance-peer-call"))
	lease := r.requireAdvanced(t, 1, reply)
	root := r.call(0, "ROOT", map[string]any{}).Capabilities[0]
	create, status := r.pendingCall(0, "MKDIR", map[string]any{"parent": root, "name": name("advance-peer-dir"), "mode": 0777, "umask": 0})
	if status == "TERMINAL" {
		t.Fatal("peer mutation passed an active Advance lease")
	}
	r.requireRecallScope(t, "", 1) // all-domain; m0 holds no view, so gets no event
	records := history(t, r.controller).Recalls
	if last := records[len(records)-1]; last.Kind != TriggerCallConflict {
		t.Fatalf("history %+v", last)
	}
	r.completeCleanRecall()
	if r.leaseActive(lease) || r.queryStatus(0, create) != "TERMINAL" {
		t.Fatalf("lease active %v after the conflict recall", r.leaseActive(lease))
	}
	total := r.recallTotal()
	if released := r.release(1, lease); released.Status != "RELEASED" || r.recallTotal() != total {
		t.Fatalf("release after the conflict recall %+v, recalls %d -> %d", released, total, r.recallTotal())
	}
}
