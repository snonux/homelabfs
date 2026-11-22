package viewcontrol

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Retiring a fenced session (task fh4, fenced_retirement.go).

// TestRetireFencedReleasesChargesAndStaysRefused: m1 is fenced as in
// TestStalledParticipantFencedAfterBound and the recall completes with m0.
// m1 still holds ticket, family, lease, lineage and AckArchive rows. The
// operator's signed retirement drops every one of them and returns the
// tickets' Identities charge, but m1 stays in Fenced (now retired): every
// request of it is still refused, a replay of the retirement answers nil,
// the survivor m0 keeps acquiring, and after a reopen the tombstone, the
// refusal and schema 3 are all still there and a new recall skips m1.
func TestRetireFencedReleasesChargesAndStaysRefused(t *testing.T) {
	r := newZQ3Rig(t, 0)
	r.fenceStalledM1()
	before := r.sessionRows(1)
	if before.tickets == 0 || before.leases == 0 || before.lineages == 0 || before.archive == 0 {
		t.Fatalf("fenced m1 holds too little to test a release: %+v", before)
	}
	identities := r.identities()
	must(t, r.retireFenced(1))
	if after := r.sessionRows(1); after != (sessionRows{}) {
		t.Fatalf("retired m1 still holds %+v", after)
	}
	if got := r.identities(); got != identities-before.tickets {
		t.Fatalf("Identities %d after retiring %d tickets from %d", got, before.tickets, identities)
	}
	r.requireRetired(1)
	r.requireRefusedAfterFence(1)
	must(t, r.retireFenced(1)) // an exact replay is answered nil
	if _, granted := r.acquire(0, guestbridge.ViewAccess{Kind: "DOMAIN"}, "READ"); granted.Status != "GRANTED" {
		t.Fatalf("survivor's acquisition after the retirement: %+v", granted)
	}
	r.reopen()
	r.requireRetired(1)
	if _, err := r.tryExchange(1, "VIEW_POLL", map[string]any{"domain": r.domains[1], "after_sequence": 0}); !errors.Is(err, ErrSessionFenced) {
		t.Fatalf("retired session's poll after reopen: %v, want ErrSessionFenced", err)
	}
	evidence, err := r.controller.LedgerEvidence(t.Context())
	must(t, err)
	if evidence.Fenced != 1 || evidence.RetiredFenced != 1 {
		t.Fatalf("evidence %+v", evidence)
	}
	r.requireNoEventAfterFence(1)
}

// TestRetiredFamiliesKeepNumbersMonotone (review of fh4 round 1): the
// retirement keeps each of m1's families as {High, Floor = High, Scope}.
// An old ticket of m1, resent past a cleared lock-free gate, is refused
// under the controller lock (ErrSessionFenced). Even with the tombstone
// itself gone from memory, the kept counters answer it with a rights-free
// RETIRED_ID: no number m1 ever used can be admitted again.
func TestRetiredFamiliesKeepNumbersMonotone(t *testing.T) {
	r := newZQ3Rig(t, 0)
	r.fenceStalledM1()
	op, payload := r.sessionTicket(1, "VIEW_ACQUIRE")
	must(t, r.retireFenced(1))
	r.requireMinimalFamilies(1)
	r.controller.fencedSessions.Delete(r.scopes[1].Session)
	if _, err := r.controller.ExchangeView(t.Context(), r.scopes[1], op, payload); !errors.Is(err, ErrSessionFenced) {
		t.Fatalf("old ticket past the gate: %v, want ErrSessionFenced", err)
	}
	r.controller.mu.Lock()
	delete(r.controller.state.Fenced, r.scopes[1].Session)
	r.controller.mu.Unlock()
	reply, err := r.controller.ExchangeView(t.Context(), r.scopes[1], op, payload)
	if err != nil || status(reply) != "RETIRED_ID" {
		t.Fatalf("old ticket without the tombstone: %s %v, want RETIRED_ID", reply, err)
	}
}

// TestRetireFencedRefusals: a retirement is refused while the recall that
// fenced m1 still runs, for a forged, foreign or stale statement, for an
// unfenced session, and while m1 holds an active lease or an open
// reservation (both injected: a fence never leaves either behind). Every
// refusal leaves m1's rows in place; the valid retirement then succeeds.
func TestRetireFencedRefusals(t *testing.T) {
	r := newZQ3Rig(t, 0)
	advance := r.testClock()
	open := r.startEarlyDrainRecall()
	r.ack(0, "INSTALLED_DRAIN", nil)
	advance(StalledParticipantBound + time.Millisecond)
	r.poll(0)
	r.requireFenced(1)
	if err := r.retireFenced(1); !errors.Is(err, fsapi.WouldBlock) {
		t.Fatalf("retirement during the recall: %v, want WouldBlock", err)
	}
	r.ack(0, "DRAINED", r.emptyManifest())
	r.finishAfterFence(open)
	before := r.sessionRows(1)
	r.requireForgedRetirementsRefused()
	r.requireInjectedRefusal(func(c *Controller) func() {
		for key, l := range c.state.Leases {
			if l.Lease.Scope.Session == r.scopes[1].Session {
				l.Active = true
				c.state.Leases[key] = l
				return func() { l.Active = false; c.state.Leases[key] = l }
			}
		}
		t.Fatal("m1 has no lease row")
		return nil
	})
	r.requireInjectedRefusal(func(c *Controller) func() {
		c.state.Reservations["injected"] = reservationState{Reservation: guestbridge.DirtyReservation{Scope: r.scopes[1]}, Status: "FROZEN"}
		return func() { delete(c.state.Reservations, "injected") }
	})
	if after := r.sessionRows(1); after != before {
		t.Fatalf("refused retirements changed m1's rows: %+v, was %+v", after, before)
	}
	must(t, r.retireFenced(1))
	r.requireRetired(1)
}

// TestRetireFencedReplayAfterCrash crashes the retirement at both points.
// Before the write nothing changes: the reopened ledger holds the plain
// fence with every row, and the same signed command retires again. After
// the write the reopened ledger holds the tombstone without the rows, the
// same command is a nil replay, m1 is refused and m0 still polls.
func TestRetireFencedReplayAfterCrash(t *testing.T) {
	var armed atomic.Value
	armed.Store("")
	crash := errors.New("crash")
	r := newRig(t, func(c *Config) {
		c.Authorization.Enrollment.Bounds = zq3SpecBounds(t)
		c.Failpoint = func(name string) error {
			if name == armed.Load().(string) {
				return crash
			}
			return nil
		}
	})
	r.fenceStalledM1()
	before := r.sessionRows(1)
	for _, point := range []string{"fenced-retire", "fenced-retired-protected"} {
		armed.Store(point)
		if err := r.retireFenced(1); !errors.Is(err, crash) {
			t.Fatalf("retirement survived failpoint %s: %v", point, err)
		}
		armed.Store("")
		r.reopen()
		if point == "fenced-retire" && (r.sessionRows(1) != before || r.retired(1)) {
			t.Fatalf("crash before the write changed the ledger: %+v retired %v", r.sessionRows(1), r.retired(1))
		}
	}
	r.requireRetired(1)
	if after := r.sessionRows(1); after != (sessionRows{}) {
		t.Fatalf("protected retirement lost rows on reopen: %+v", after)
	}
	must(t, r.retireFenced(1))
	if _, err := r.tryExchange(1, "VIEW_POLL", map[string]any{"domain": r.domains[1], "after_sequence": 0}); !errors.Is(err, ErrSessionFenced) {
		t.Fatalf("retired session's poll: %v, want ErrSessionFenced", err)
	}
	r.directPoll(0)
}

// TestRetiredFenceLedgerValidation: a reopened ledger's tombstone must be
// signed by the operator for this enrollment and must leave no row of its
// scope; an unretired fence still needs its archived STOPPED. A tampered
// signature refuses the reopen.
func TestRetiredFenceLedgerValidation(t *testing.T) {
	r := newZQ3Rig(t, 0)
	r.fenceStalledM1()
	must(t, r.retireFenced(1))
	c := r.controller
	session := r.scopes[1].Session
	c.mu.Lock()
	if !c.state.validFenced() || !c.validRetiredFences() {
		t.Fatal("valid retired ledger refused")
	}
	key := ArchiveKey(r.scopes[1], "r", "STOPPED", 1)
	c.state.AckArchive[key] = "h"
	if c.state.validFenced() {
		t.Fatal("retired fence with an AckArchive row accepted")
	}
	delete(c.state.AckArchive, key)
	f := c.state.Fenced[session]
	retired := f.Retired
	f.Retired = nil
	c.state.Fenced[session] = f
	if c.state.validFenced() {
		t.Fatal("unretired fence without its archived STOPPED accepted")
	}
	f.Retired = &retiredFence{Signature: append([]byte{}, retired.Signature...)}
	f.Retired.Signature[0] ^= 1
	c.state.Fenced[session] = f
	if c.validRetiredFences() {
		t.Fatal("tampered retirement signature accepted")
	}
	must(t, c.save())
	raw, err := c.rawState()
	must(t, err)
	c.mu.Unlock()
	r.facts.mu.Lock()
	r.facts.recovery = hash(raw)
	r.facts.mu.Unlock()
	must(t, c.Close())
	if _, err := New(t.Context(), r.cc); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("reopen with a tampered tombstone: %v, want Stale", err)
	}
	r.controller = nil
}

// sessionRows counts the rows one session keeps charged.
type sessionRows struct {
	// families counts the session's families that are not yet reduced to
	// the {High, Floor = High, Scope} row a retirement keeps.
	tickets, families, leases, reservations, lineages, archive int
}

// fenceStalledM1 fences m1 in INSTALL_DRAIN and completes the recall with
// m0 (TestStalledParticipantFencedAfterBound).
func (r *rig) fenceStalledM1() {
	r.t.Helper()
	advance := r.testClock()
	open := r.startEarlyDrainRecall()
	r.ack(0, "INSTALLED_DRAIN", nil)
	advance(StalledParticipantBound + time.Millisecond)
	r.poll(0)
	r.requireFenced(1)
	r.ack(0, "DRAINED", r.emptyManifest())
	r.finishAfterFence(open)
}

// sessionRows counts domain i's rows in the live ledger.
func (r *rig) sessionRows(i int) sessionRows {
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	s, session := r.controller.state, r.scopes[i].Session
	var n sessionRows
	for _, t := range s.Tickets {
		n.tickets += boolCount(t.Scope.Session == session)
	}
	for _, f := range s.Families {
		n.families += boolCount(f.Scope != nil && f.Scope.Session == session && (f.Floor != f.High || f.Receipt != ""))
	}
	for _, l := range s.Leases {
		n.leases += boolCount(l.Lease.Scope.Session == session)
	}
	for _, v := range s.Reservations {
		n.reservations += boolCount(v.Reservation.Scope.Session == session)
	}
	for _, at := range s.LineageFences {
		n.lineages += boolCount(at.Session == session)
	}
	for key := range s.AckArchive {
		n.archive += boolCount(strings.HasPrefix(key, digest(r.scopes[i])+"/"))
	}
	return n
}

func boolCount(b bool) int {
	if b {
		return 1
	}
	return 0
}

// identities returns the ledger's Identities charge.
func (r *rig) identities() int {
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	return r.controller.state.Identities
}

// retireFenced signs domain i's retirement statement with the operator key
// and submits it.
func (r *rig) retireFenced(i int) error {
	r.t.Helper()
	statement, err := r.controller.FencedRetirementStatement(r.scopes[i].Session)
	if err != nil {
		return err
	}
	return r.submitRetirement(statement, r.operator)
}

// submitRetirement signs statement with key and submits it.
func (r *rig) submitRetirement(statement FencedRetirement, key ed25519.PrivateKey) error {
	r.t.Helper()
	signature, err := SignFencedRetirement(key, statement)
	must(r.t, err)
	return r.controller.RetireFenced(r.t.Context(), statement, signature)
}

// retired reports whether domain i carries a retirement tombstone.
func (r *rig) retired(i int) bool {
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	f, ok := r.controller.state.Fenced[r.scopes[i].Session]
	return ok && f.Retired != nil
}

// requireRetired requires domain i fenced (requireFenced) with a tombstone
// in a schema-3 ledger.
func (r *rig) requireRetired(i int) {
	r.t.Helper()
	r.requireFenced(i)
	if !r.retired(i) {
		r.t.Fatalf("m%d not retired", i)
	}
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	if r.controller.state.Schema != LedgerSchemaFenced {
		r.t.Fatalf("schema %d after retirement", r.controller.state.Schema)
	}
}

// requireForgedRetirementsRefused submits m1's statement signed by another
// key, for another enrollment and with another sequence, and a statement
// for the unfenced m0: Permission, Permission, Stale and Permission.
func (r *rig) requireForgedRetirementsRefused() {
	r.t.Helper()
	statement, err := r.controller.FencedRetirementStatement(r.scopes[1].Session)
	must(r.t, err)
	if _, err = r.controller.FencedRetirementStatement(r.scopes[0].Session); !errors.Is(err, fsapi.Permission) {
		r.t.Fatalf("statement for unfenced m0: %v", err)
	}
	foreign, stale, unfenced := statement, statement, statement
	foreign.EnrollmentHash = hash([]byte("another enrollment"))
	stale.Sequence++
	unfenced.Session = r.scopes[0].Session
	for _, c := range []struct {
		name      string
		statement FencedRetirement
		key       ed25519.PrivateKey
		want      error
	}{
		{"other key", statement, keys(r.t), fsapi.Permission},
		{"other enrollment", foreign, r.operator, fsapi.Permission},
		{"stale row", stale, r.operator, fsapi.Stale},
		{"unfenced m0", unfenced, r.operator, fsapi.Permission},
	} {
		if err := r.submitRetirement(c.statement, c.key); !errors.Is(err, c.want) {
			r.t.Fatalf("%s: %v, want %v", c.name, err, c.want)
		}
	}
}

// requireInjectedRefusal applies inject to the live ledger, requires the
// retirement of m1 to be refused WouldBlock and undoes the injection.
func (r *rig) requireInjectedRefusal(inject func(*Controller) func()) {
	r.t.Helper()
	r.controller.mu.Lock()
	undo := inject(r.controller)
	r.controller.mu.Unlock()
	err := r.retireFenced(1)
	r.controller.mu.Lock()
	undo()
	r.controller.mu.Unlock()
	if !errors.Is(err, fsapi.WouldBlock) {
		r.t.Fatalf("retirement with injected state: %v, want WouldBlock", err)
	}
}

// sessionTicket returns the operation and payload of one of domain i's
// ticket rows of op.
func (r *rig) sessionTicket(i int, op string) (string, json.RawMessage) {
	r.t.Helper()
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	for _, t := range r.controller.state.Tickets {
		if t.Scope.Session == r.scopes[i].Session && t.Operation == op {
			return t.Operation, append(json.RawMessage(nil), t.Payload...)
		}
	}
	r.t.Fatalf("m%d has no %s ticket", i, op)
	return "", nil
}

// requireMinimalFamilies requires domain i to own at least one family and
// every one of them reduced to {High, Floor = High, Scope}.
func (r *rig) requireMinimalFamilies(i int) {
	r.t.Helper()
	r.controller.mu.Lock()
	defer r.controller.mu.Unlock()
	owned := 0
	for _, f := range r.controller.state.Families {
		if f.Scope == nil || f.Scope.Session != r.scopes[i].Session {
			continue
		}
		owned++
		if f.Floor != f.High || f.Receipt != "" || f.High == 0 {
			r.t.Fatalf("m%d family after retirement %+v", i, f)
		}
	}
	if owned == 0 {
		r.t.Fatalf("m%d kept no family", i)
	}
}
