package viewcontrol

import (
	"maps"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// One negative per advance condition (task xf4, advance.go). Each breaks
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
