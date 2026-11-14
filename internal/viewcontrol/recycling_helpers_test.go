package viewcontrol

import (
	"encoding/json"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// recycleRun drives guest lease cycles for the recycling tests (tasks q64,
// f84): DOMAIN READ acquire/release cycles, each with its scoped recall, and
// dirty rounds with an all-domain recall, retiring every terminal ticket the
// way the guest does after each cleanup (hfs_view_retire). Before reopen it
// talks through the real bridges; after reopen (direct) straight to the
// controller, because the bridges keep pointing at the closed controller.
type recycleRun struct {
	r      *rig
	direct bool
	// descriptions are one file per domain for the dirty rounds, created up
	// front: a non-read op admitted while a write lease is live recalls.
	descriptions [2]guestbridge.Capability
	peaks        recyclePeaks
	cycles       int
	recalls      int
}

// recyclePeaks are the high-water marks of every record that used to grow
// for the whole controller lifetime.
type recyclePeaks struct {
	identities, tickets, uuids, archive, revisions, lineages int
	retirements, leases, sources, ledgerBytes                int
}

func newRecycleRun(r *rig) *recycleRun {
	x := &recycleRun{r: r}
	for i := range 2 {
		_, x.descriptions[i] = r.create(i, "q64-dirty-"+string(rune('0'+i)), false)
	}
	return x
}

// view sends one ticket op and returns its reply payload.
func (x *recycleRun) view(i int, op string, payload any) json.RawMessage {
	x.r.t.Helper()
	if x.direct {
		return x.r.directView(i, op, payload)
	}
	raw, err := x.r.execute(i, op, payload)
	must(x.r.t, err)
	return raw
}

// exchange sends one recall-protocol op and returns its reply payload.
func (x *recycleRun) exchange(i int, op string, payload any) json.RawMessage {
	x.r.t.Helper()
	if x.direct {
		return x.r.directExchange(i, op, payload)
	}
	raw, err := x.r.execute(i, op, payload)
	must(x.r.t, err)
	return raw
}

// readRecord is what the guest sent and got in one read cycle.
type readRecord struct {
	acquire, release map[string]any
	// granted is the exact VIEW_ACQUIRE reply; reopened the exact REOPENED
	// ACK of the cycle's scoped recall.
	granted, reopened json.RawMessage
}

// readCycle is one short-lived guest reader on domain i: a DOMAIN READ lease
// and its preowned release, whose scoped recall completes before the
// release is RELEASED; then the guest retires both tickets.
func (x *recycleRun) readCycle(i int) readRecord {
	x.r.t.Helper()
	rec := x.readUnretired(i)
	x.observe() // the peak: both tickets and the archived ACKs are live
	x.retireAll(i)
	x.cycles++
	x.observe()
	return rec
}

// readUnretired is readCycle without the retirement.
func (x *recycleRun) readUnretired(i int) readRecord {
	x.r.t.Helper()
	rec := readRecord{acquire: map[string]any{"request": x.r.ticket(i), "domain": x.r.domains[i], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": nil}}
	rec.granted = x.view(i, "VIEW_ACQUIRE", rec.acquire)
	var granted viewReply
	must(x.r.t, json.Unmarshal(rec.granted, &granted))
	if granted.Status != "GRANTED" || granted.Lease == nil {
		x.r.t.Fatalf("cycle %d: DOMAIN READ %d: %+v", x.cycles, i, granted)
	}
	rec.release = map[string]any{"request": x.r.controlTicket(i), "lease": granted.Lease}
	x.r.physical("VIEW_RELEASE", rec.release)
	if reply := x.view(i, "VIEW_RELEASE", rec.release); status(reply) != "PENDING" {
		x.r.t.Fatalf("cycle %d: release %d: %s", x.cycles, i, reply)
	}
	rec.reopened = x.finishRecall()[i]
	if reply := x.view(i, "VIEW_QUERY", map[string]any{"request": rec.release["request"]}); status(reply) != "RELEASED" {
		x.r.t.Fatalf("cycle %d: release query %d: %s", x.cycles, i, reply)
	}
	return rec
}

// finishRecall acknowledges every phase of the current clean recall for its
// participants, polling first so the recall advances to the phase's event.
// It returns each participant's exact REOPENED ACK.
func (x *recycleRun) finishRecall() map[int]json.RawMessage {
	x.r.t.Helper()
	_, participants, exists := x.r.recallSnapshot()
	if !exists {
		x.r.t.Fatalf("cycle %d: no recall to finish", x.cycles)
	}
	reopened := map[int]json.RawMessage{}
	for _, phase := range []string{"STOPPED", "INSTALLED_DRAIN", "DRAINED", "INSTALLED_FINAL", "REOPENED"} {
		for _, i := range participants {
			x.exchange(i, "VIEW_POLL", map[string]any{"domain": x.r.domains[i], "after_sequence": 0})
			ack := x.r.ackPayload(i, phase, x.r.emptyManifest())
			if reply := x.exchange(i, "VIEW_ACK", ack); status(reply) != "ACCEPTED" {
				x.r.t.Fatalf("cycle %d: %s ACK %d: %s", x.cycles, phase, i, reply)
			}
			reopened[i] = ack
		}
	}
	if _, _, exists = x.r.recallSnapshot(); exists {
		x.r.t.Fatalf("cycle %d: recall did not close", x.cycles)
	}
	x.recalls++
	return reopened
}

// dirtyRound gives each domain an EXCLUSIVE_DIRTY page lease with one dirty
// reservation, releases m0's lease (an all-domain recall that freezes and
// drains both reservations), releases m1's quiesced lease and retires all.
func (x *recycleRun) dirtyRound() {
	r := x.r
	r.t.Helper()
	var reservations [2]guestbridge.DirtyReservation
	var leases [2]*guestbridge.ViewLease
	for i := range 2 {
		_, lease := r.acquire(i, page(x.descriptions[i], 0), "EXCLUSIVE_DIRTY")
		if lease.Status != "GRANTED" {
			r.t.Fatalf("round %d: write lease %d: %+v", x.cycles, i, lease)
		}
		_, payload := r.reserve(i, lease.Lease, x.descriptions[i])
		reserved := r.view(i, "DIRTY_RESERVE", payload)
		if reserved.Status != "GRANTED" || reserved.Reservation == nil {
			r.t.Fatalf("round %d: reservation %d: %+v", x.cycles, i, reserved)
		}
		reservations[i], leases[i] = *reserved.Reservation, lease.Lease
	}
	x.observe() // both write leases and reservations are live
	release := map[string]any{"request": r.controlTicket(0), "lease": leases[0]}
	r.physical("VIEW_RELEASE", release)
	if reply := r.view(0, "VIEW_RELEASE", release); reply.Status != "PENDING" {
		r.t.Fatalf("round %d: dirty release: %+v", x.cycles, reply)
	}
	r.drainPair(reservations)
	x.recalls++
	if reply := r.view(0, "VIEW_QUERY", map[string]any{"request": release["request"]}); reply.Status != "RELEASED" {
		r.t.Fatalf("round %d: dirty release query: %+v", x.cycles, reply)
	}
	if reply := r.release(1, leases[1]); reply.Status != "RELEASED" {
		r.t.Fatalf("round %d: quiesced release: %+v", x.cycles, reply)
	}
	x.observe()
	x.retireAll(0)
	x.retireAll(1)
	x.cycles++
	x.observe()
}

// retireAll retires every ticket of both of domain i's families through its
// High, naming each protected terminal reply id, as the guest does.
func (x *recycleRun) retireAll(i int) {
	r := x.r
	r.t.Helper()
	for _, nonce := range []string{uid(111).String(), uid(112).String()} {
		payload := x.retirement(i, nonce)
		if payload == nil {
			continue
		}
		r.physical("VIEW_RETIRE", payload)
		if reply := x.view(i, "VIEW_RETIRE", payload); status(reply) != "RETIRED" {
			r.t.Fatalf("cycle %d: retire %d: %s", x.cycles, i, reply)
		}
	}
}

// retirement builds the VIEW_RETIRE of family nonce of domain i through its
// High from the ledger's protected terminal replies, or nil if nothing is
// unretired.
func (x *recycleRun) retirement(i int, nonce string) map[string]any {
	r := x.r
	ticket := guestbridge.Ticket{Anchor: uid(110).String(), Nonce: nonce, Credentials: r.credentials[i]}
	_, fk := ticketKeys(r.scopes[i], ticket)
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	f := r.controller.state.Families[fk]
	hashes := []string{}
	for n := f.Floor + 1; n <= f.High; n++ {
		ticket.Number = n
		key, _ := ticketKeys(r.scopes[i], ticket)
		var reply struct{ ID string }
		must(r.t, json.Unmarshal(r.controller.state.Tickets[key].Current, &reply))
		hashes = append(hashes, reply.ID)
	}
	if len(hashes) == 0 {
		return nil
	}
	return map[string]any{"anchor": uid(110).String(), "nonce": nonce, "credentials": r.credentials[i], "through": f.High, "result_hashes": hashes}
}

// observe records the high-water marks of the formerly lifetime records.
func (x *recycleRun) observe() {
	c := x.r.controller
	c.mu.Lock()
	defer c.mu.Unlock()
	raw, err := c.rawState()
	must(x.r.t, err)
	s, p := c.state, &x.peaks
	p.identities = max(p.identities, s.Identities)
	p.tickets = max(p.tickets, len(s.Tickets))
	p.uuids = max(p.uuids, len(s.UUIDs))
	p.archive = max(p.archive, len(s.AckArchive))
	p.revisions = max(p.revisions, len(s.Revisions))
	p.lineages = max(p.lineages, len(s.LineageFences))
	p.retirements = max(p.retirements, len(s.Retirements))
	p.leases = max(p.leases, len(s.Leases))
	p.sources = max(p.sources, len(s.Sources))
	p.ledgerBytes = max(p.ledgerBytes, len(raw))
}

// admitted is the number of view tickets the controller ever admitted: the
// sum of every family's High.
func (x *recycleRun) admitted() uint64 {
	c := x.r.controller
	c.mu.Lock()
	defer c.mu.Unlock()
	total := uint64(0)
	for _, f := range c.state.Families {
		total += f.High
	}
	return total
}

// requireMargins requires each formerly lifetime record to peak at no more
// than half its bound, as the o64 workload model requires of its bounds.
func (x *recycleRun) requireMargins(t *testing.T, b Bounds) {
	t.Helper()
	check := func(name string, peak, bound int) {
		t.Helper()
		if 2*peak > bound {
			t.Errorf("%s: twice the peak %d exceeds the bound %d", name, peak, bound)
		}
	}
	p := x.peaks
	check("ticket identities (Identities)", p.identities, b.Identities)
	check("ticket rows (Identities)", p.tickets, b.Identities)
	check("recall/provenance UUIDs (2*Identities)", p.uuids, 2*b.Identities)
	check("ACK archive (8*Identities)", p.archive, 8*b.Identities)
	check("lineage revisions (Identities)", p.revisions, b.Identities)
	check("leases (Identities)", p.leases, b.Identities)
	check("source cuts (Sources)", p.sources, b.Sources)
	check("ledger bytes (LedgerBytes)", p.ledgerBytes, b.LedgerBytes)
	// Receipts have no bound of their own; one per family (2 per domain).
	check("retirement receipts (latest per family, 4 families)", p.retirements, 8)
}
