package viewcontrol

import (
	"encoding/json"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Review round 2 of task 784: the scoped barrier still covers its own
// participant, lock-only reductions keep the cut, and a scoped recall
// survives a controller reopen.

// TestScopedRecallHoldsOwnDomain: during m1's scoped recall, m1 itself stays
// closed: its GETATTR waits, a READ acquisition answers PENDING and a renewal
// of the lease being released answers BLOCKED.
func TestScopedRecallHoldsOwnDomain(t *testing.T) {
	r := newZQ3Rig(t, 0)
	released := r.domainRead(1)
	r.startRelease(1, released)
	if scope, _, _ := r.recallSnapshot(); scope != r.scopes[1].Session {
		t.Fatalf("recall scope %q", scope)
	}
	if _, status := r.pendingCall(1, "GETATTR", map[string]any{"object": r.roots[1]}); status != "INDETERMINATE" {
		t.Fatalf("participant GETATTR during its own scoped recall: %s", status)
	}
	if _, acquired := r.acquire(1, guestbridge.ViewAccess{Kind: "DOMAIN"}, "READ"); acquired.Status != "PENDING" {
		t.Fatalf("participant READ acquisition: %+v", acquired)
	}
	if renewed := r.view(1, "VIEW_RENEW", map[string]any{"request": r.ticket(1), "lease": released}); renewed.Status != "BLOCKED" {
		t.Fatalf("participant renewal: %+v", renewed)
	}
}

// TestLockOnlyReductionsKeepCut: DESCRIPTION_CLOSE, like PROCESS_EXIT, only
// releases fcntl locks. Under a peer DOMAIN READ lease it starts no recall,
// during the peer's scoped recall it is not held, and neither moves the cut.
func TestLockOnlyReductionsKeepCut(t *testing.T) {
	r := newZQ3Rig(t, 0)
	_, description := r.create(0, "closes", false)
	peer := r.domainRead(1)
	steps := []struct {
		op   string
		args map[string]any
	}{
		{"DESCRIPTION_CLOSE", map[string]any{"description": description, "process": r.credentials[0].Process, "close_sequence": 1}},
		{"DESCRIPTION_CLOSE", map[string]any{"description": description, "process": r.credentials[0].Process, "close_sequence": 2}},
		{"PROCESS_EXIT", map[string]any{"process": r.credentials[0].Process, "exit_sequence": 1}},
	}
	for i, step := range steps {
		if i == 1 {
			r.startRelease(1, peer) // the rest runs inside m1's scoped recall
		}
		before, err := r.controller.capture(t.Context())
		must(t, err)
		if _, status := r.pendingCall(0, step.op, step.args); status != "TERMINAL" {
			t.Fatalf("%s #%d: %s", step.op, i, status)
		}
		after, err := r.controller.capture(t.Context())
		must(t, err)
		if sourceKey(before) != sourceKey(after) {
			t.Fatalf("%s #%d moved the cut", step.op, i)
		}
		if scope, _, exists := r.recallSnapshot(); exists != (i > 0) || (exists && scope != r.scopes[1].Session) {
			t.Fatalf("%s #%d recall scope %q exists %v", step.op, i, scope, exists)
		}
	}
}

// TestScopedRecallSurvivesReopen reopens the controller after m1's STOPPED
// in its scoped recall: the scope and barrier survive, the reopen clears the
// learned ACKs, the fresh ACKs complete the recall and only m1's fence
// advances. The old bridges still point at the closed controller, so the
// test talks to the reopened controller directly.
func TestScopedRecallSurvivesReopen(t *testing.T) {
	r := newZQ3Rig(t, 0)
	r.domainRead(0)
	payload := r.startRelease(1, r.domainRead(1))
	r.ack(1, "STOPPED", r.emptyManifest())
	r.reopen()
	if scope, participants, exists := r.recallSnapshot(); !exists || scope != r.scopes[1].Session || len(participants) != 1 {
		t.Fatalf("reopened recall scope %q participants %v", scope, participants)
	}
	if !r.controller.recallHoldsCall(r.scopes[1].Session, "GETATTR") || r.controller.recallHoldsCall(r.scopes[0].Session, "GETATTR") {
		t.Fatal("reopened scoped barrier changed")
	}
	if granted := r.directView(0, "VIEW_ACQUIRE", map[string]any{"request": r.ticket(0), "domain": r.domains[0], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": nil}); status(granted) != "GRANTED" {
		t.Fatalf("peer READ acquisition after reopen: %s", granted)
	}
	for _, phase := range []string{"STOPPED", "INSTALLED_DRAIN", "DRAINED", "INSTALLED_FINAL", "REOPENED"} {
		r.directExchange(1, "VIEW_POLL", map[string]any{"domain": r.domains[1], "after_sequence": 0})
		if reply := r.directExchange(1, "VIEW_ACK", r.ackPayload(1, phase, r.emptyManifest())); status(reply) != "ACCEPTED" {
			t.Fatalf("%s after reopen: %s", phase, reply)
		}
	}
	r.directExchange(1, "VIEW_POLL", map[string]any{"domain": r.domains[1], "after_sequence": 0})
	if _, _, exists := r.recallSnapshot(); exists {
		t.Fatal("scoped recall did not complete after reopen")
	}
	if r.controller.fence(r.scopes[0].Session) != 1 || r.controller.fence(r.scopes[1].Session) != 2 {
		t.Fatalf("fences m0 %d m1 %d", r.controller.fence(r.scopes[0].Session), r.controller.fence(r.scopes[1].Session))
	}
	r.physical("VIEW_RELEASE", payload)
	if released := r.directView(1, "VIEW_QUERY", map[string]any{"request": payload["request"]}); status(released) != "RELEASED" {
		t.Fatalf("scoped release after reopen: %s", released)
	}
}

// directExchange sends a recall-protocol op straight to the controller.
func (r *rig) directExchange(i int, op string, payload any) json.RawMessage {
	r.t.Helper()
	raw, err := canonical(payload)
	must(r.t, err)
	reply, err := r.controller.Exchange(r.t.Context(), r.scopes[i], op, raw)
	must(r.t, err)
	return reply
}

// directView sends a ticket op straight to the controller.
func (r *rig) directView(i int, op string, payload any) json.RawMessage {
	r.t.Helper()
	raw, err := canonical(payload)
	must(r.t, err)
	reply, err := r.controller.ExchangeView(r.t.Context(), r.scopes[i], op, raw)
	must(r.t, err)
	return reply
}
