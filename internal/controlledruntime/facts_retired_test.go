//go:build linux || freebsd

package controlledruntime

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// TestFactsVerifyRetiredFences (task fh4): a retired fence (the tombstone
// of viewcontrol's operator retirement) no longer needs its archived
// STOPPED, but must leave no ticket, lease, reservation or archived ACK of
// its scope; the domain, fence and event rules still hold.
func TestFactsVerifyRetiredFences(t *testing.T) {
	var scope guestbridge.Context
	scope.Session = "b"
	for _, c := range []struct {
		name   string
		mutate func(*factsLedger)
		want   bool
	}{
		{"valid", func(*factsLedger) {}, true},
		{"archived STOPPED left", func(l *factsLedger) { l.AckArchive[viewcontrol.ArchiveKey(scope, "r1", "STOPPED", 3)] = "h" }, false},
		{"ticket left", func(l *factsLedger) { addTicket(l, scope) }, false},
		{"lease left", func(l *factsLedger) {
			l.Leases = map[string]factsLease{"x": {Lease: guestbridge.ViewLease{Scope: scope}}}
		}, false},
		{"reservation left", func(l *factsLedger) { addRetiredReservation(l, scope) }, false},
		{"fence moved", func(l *factsLedger) { l.Fences["b"] = 2 }, false},
		{"still issued an event", func(l *factsLedger) { l.Recall.Events["b"] = json.RawMessage(`{}`) }, false},
		{"unregistered", func(l *factsLedger) { delete(l.Domains, "b") }, false},
	} {
		l := retiredLedger(t, scope)
		c.mutate(&l)
		if got := l.validFenced(); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

// TestFactsRefuseFencedScope (task fh4): every physical check of a fenced
// session, retired or not, is refused; a live peer's is not.
func TestFactsRefuseFencedScope(t *testing.T) {
	var fenced, live guestbridge.Context
	fenced.Session, live.Session = "b", "a"
	l := retiredLedger(t, fenced)
	if err := l.refuseFenced(fenced); !errors.Is(err, viewcontrol.ErrSessionFenced) {
		t.Fatalf("retired scope's physical check: %v", err)
	}
	if err := l.refuseFenced(live); err != nil {
		t.Fatalf("live scope's physical check: %v", err)
	}
}

// retiredLedger is a two-domain ledger in which b is fenced and retired.
func retiredLedger(t *testing.T, scope guestbridge.Context) factsLedger {
	t.Helper()
	raw := `{"Domains":{"a":{},"b":{}},"Fences":{},"Fenced":{"b":{"Recall":"r1","Phase":"INSTALL_DRAIN","Sequence":4,"Fence":1,"Retired":{"Signature":"AA=="}}},"Recall":{"ID":"r2","Events":{"a":{}}},"AckArchive":{}}`
	var l factsLedger
	if err := json.Unmarshal([]byte(raw), &l); err != nil {
		t.Fatal(err)
	}
	d := l.Domains["b"]
	d.Domain.Scope = scope
	l.Domains["b"] = d
	return l
}

// addTicket adds one ticket row of scope to l.
func addTicket(l *factsLedger, scope guestbridge.Context) {
	raw, _ := json.Marshal(map[string]any{"x": map[string]any{"Scope": scope}})
	_ = json.Unmarshal(raw, &l.Tickets)
}

// addRetiredReservation adds one CLOSED reservation row of scope to l.
func addRetiredReservation(l *factsLedger, scope guestbridge.Context) {
	raw, _ := json.Marshal(map[string]any{"x": map[string]any{"Reservation": guestbridge.DirtyReservation{Scope: scope}, "Status": "CLOSED"}})
	_ = json.Unmarshal(raw, &l.Reservations)
}
