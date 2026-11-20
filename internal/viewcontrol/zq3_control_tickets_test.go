package viewcontrol

import (
	"encoding/json"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Control-ticket capacity at the zq3 spec bounds (task qe4, zq3-r16 R16-1).
//
// In zq3-r16 the host answered m1's ie4 re-acquire BLOCKED because
// Bounds.ControlTickets (8, shared by both mounts) was full. controlCharge
// counted 1 + 3 + 4 + 1 = 9 for that request:
//
//   - 3 answered VIEW_RELEASE tickets that were never retired: two of m0
//     from earlier processes (Phase 8) and m1's first observation release.
//     The guest retires only at the end of a cleanup packet and the host
//     refuses retirement while the credentials still hold an active lease or
//     any recall runs (retirementBlocked), so they stay charged;
//   - 4 for m0's T1 lease that four observations shared: one release ticket
//     or one reserved credit per holder (3 holder releases RELEASED and
//     unretired, the last holder still holding);
//   - 1 for m1's second lease, which ie4 holds while it acquires again;
//   - 1 for the re-acquire itself.
//
// t1ControlProfile replays exactly that ledger shape through the real
// bridges. The spec value must grant the re-acquire; the old value 8 must
// reproduce the BLOCKED, which shows the replay matches the incident.

// t1ProfilePeak is controlCharge plus the re-acquire's own charge at the
// moment of m1's ie4 re-acquire in the replay (9 in zq3-r16).
const t1ProfilePeak = 9

// t1ControlProfile replays T1's control-ticket pattern on both mounts and
// returns m1's re-acquire reply, the control charge it met (including its
// own) and the leases still held, for t1Cleanup.
func (r *rig) t1ControlProfile() (viewReply, int, t1Held) {
	r.t.Helper()
	// Phase 8: two earlier m0 processes each observed once; their releases
	// were answered (scoped recall, RELEASED) but never retired.
	for range 2 {
		if reply := r.release(0, r.domainRead(0)); reply.Status != "RELEASED" {
			r.t.Fatalf("earlier m0 release: %+v", reply)
		}
	}
	// T1 on m0: four observations share one DOMAIN lease; three of them
	// end and release their holder reference (RELEASED, unretired).
	shared := r.domainRead(0)
	for range 3 {
		if again := r.domainRead(0); again.ID != shared.ID {
			r.t.Fatalf("m0 observations got distinct leases %s %s", shared.ID, again.ID)
		}
	}
	for range 3 {
		payload := map[string]any{"request": r.controlTicket(0), "lease": shared}
		if reply := r.view(0, "VIEW_RELEASE", payload); reply.Status != "RELEASED" {
			r.t.Fatalf("m0 holder release: %+v", reply)
		}
	}
	// T1 on m1: the open's path observation's lease is released (its
	// scoped recall moves m1's view), the open's own observation acquires a
	// new lease, and ie4 acquires again before releasing it.
	if reply := r.release(1, r.domainRead(1)); reply.Status != "RELEASED" {
		r.t.Fatalf("m1 first release: %+v", reply)
	}
	held := r.domainRead(1)
	r.controller.mu.Lock()
	charge := r.controller.controlCharge() + 1
	r.controller.mu.Unlock()
	_, reply := r.acquire(1, guestbridge.ViewAccess{Kind: "DOMAIN"}, "READ")
	return reply, charge, t1Held{m0: shared, m1: held}
}

// t1Held are the leases the profile leaves held: m0's last holder and m1's
// lease, which the granted re-acquire shares.
type t1Held struct{ m0, m1 *guestbridge.ViewLease }

// t1Cleanup ends the step the way the guest's processes exit: every holder
// releases (the last one of each lease through its scoped recall), then
// both mounts retire every ticket family, so the next step starts empty.
func (r *rig) t1Cleanup(held t1Held) {
	r.t.Helper()
	payload := map[string]any{"request": r.controlTicket(1), "lease": held.m1}
	if reply := r.view(1, "VIEW_RELEASE", payload); reply.Status != "RELEASED" {
		r.t.Fatalf("m1 re-acquire holder release: %+v", reply)
	}
	for i, lease := range []*guestbridge.ViewLease{held.m0, held.m1} {
		if reply := r.release(i, lease); reply.Status != "RELEASED" {
			r.t.Fatalf("last holder release %d: %+v", i, reply)
		}
	}
	for i := range 2 {
		r.retireFamily(i, uid(111).String())
		r.retireFamily(i, uid(112).String())
	}
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	if charge := r.controller.controlCharge(); charge != 0 {
		r.t.Fatalf("control charge after the step's cleanup: %d", charge)
	}
}

// retireFamily retires domain i's ticket family with this nonce (111 the
// acquisition family, 112 the control family) from its floor through its
// highest number, with the protected terminal ids, as hfs_view_retire does.
func (r *rig) retireFamily(i int, nonce string) {
	r.t.Helper()
	ticket := guestbridge.Ticket{Anchor: uid(110).String(), Nonce: nonce, Credentials: r.credentials[i]}
	r.controller.mu.Lock()
	_, fk := ticketKeys(r.scopes[i], ticket)
	f := r.controller.state.Families[fk]
	hashes := []string{}
	for n := f.Floor + 1; n <= f.High; n++ {
		ticket.Number = n
		key, _ := ticketKeys(r.scopes[i], ticket)
		var reply struct{ ID string }
		_ = json.Unmarshal(r.controller.state.Tickets[key].Current, &reply)
		hashes = append(hashes, reply.ID)
	}
	r.controller.mu.Unlock()
	if len(hashes) == 0 {
		return
	}
	payload := map[string]any{"anchor": ticket.Anchor, "nonce": nonce, "credentials": r.credentials[i], "through": f.High, "result_hashes": hashes}
	r.physical("VIEW_RETIRE", payload)
	raw, err := r.execute(i, "VIEW_RETIRE", payload)
	must(r.t, err)
	if status(raw) != "RETIRED" {
		r.t.Fatalf("retire domain %d family %s: %s", i, nonce, raw)
	}
}

// TestZQ3SpecControlTicketsGrantT1Profile: at the committed zq3 spec bounds
// m1's ie4 re-acquire is GRANTED in T1's control-ticket pattern, the charge
// it meets is the incident's 9, and the spec leaves headroom beyond it.
func TestZQ3SpecControlTicketsGrantT1Profile(t *testing.T) {
	b := zq3SpecBounds(t)
	if b.ControlTickets < 2*t1ProfilePeak {
		t.Fatalf("spec ControlTickets %d leave less than twice T1's peak %d", b.ControlTickets, t1ProfilePeak)
	}
	r := newZQ3Rig(t, 0)
	reply, charge, held := r.t1ControlProfile()
	if reply.Status != "GRANTED" || reply.Lease == nil || reply.Lease.ID != held.m1.ID {
		t.Fatalf("m1 re-acquire at spec bounds: %+v", reply)
	}
	if charge != t1ProfilePeak {
		t.Fatalf("T1 profile control charge %d, want %d", charge, t1ProfilePeak)
	}
	r.t1Cleanup(held)
}

// TestZQ3OldControlTicketsReproduceR16: with the zq3-r16 value 8 the same
// replay answers the re-acquire BLOCKED, as the host did in R16-1.
func TestZQ3OldControlTicketsReproduceR16(t *testing.T) {
	b := zq3SpecBounds(t)
	b.ControlTickets = 8
	r := newRig(t, func(c *Config) { c.Authorization.Enrollment.Bounds = b })
	reply, charge, _ := r.t1ControlProfile()
	if reply.Status != "BLOCKED" || charge != t1ProfilePeak {
		t.Fatalf("m1 re-acquire at ControlTickets 8: %+v at charge %d", reply, charge)
	}
}

// TestZQ3SpecControlTicketsSurviveElevenSteps runs the T1 profile once per
// zq3 test step (T1-T11) on one controller, cleaning up and retiring between
// steps as the guest does when a step's processes exit. Every step's
// re-acquire is GRANTED and meets the same peak, so nothing accumulates
// across steps. This bounds the T1 pattern only: T2-T11 have their own
// workloads, which this replay does not model.
func TestZQ3SpecControlTicketsSurviveElevenSteps(t *testing.T) {
	r := newZQ3Rig(t, 0)
	for step := 1; step <= 11; step++ {
		reply, charge, held := r.t1ControlProfile()
		if reply.Status != "GRANTED" || charge != t1ProfilePeak {
			t.Fatalf("step T%d: re-acquire %+v at charge %d", step, reply, charge)
		}
		r.t1Cleanup(held)
	}
}
