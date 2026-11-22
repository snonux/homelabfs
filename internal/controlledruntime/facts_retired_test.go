//go:build linux || freebsd

package controlledruntime

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// Retired fences in the runtime Facts (task fh4).

// TestFactsVerifyRetiredFences: a retired fence no longer needs its archived
// STOPPED, but the Facts verify the operator's signature over its exact row
// and this enrollment themselves, and require that it left no row of its
// scope but minimal family rows (viewcontrol.RetiredScopeClean). The
// domain, fence and event rules still hold, and "Retired":null is an
// unretired fence, as for the controller.
func TestFactsVerifyRetiredFences(t *testing.T) {
	operator := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	authority := retirementAuthority{operator: operator.Public().(ed25519.PublicKey), enrollmentHash: "enrollment"}
	var scope guestbridge.Context
	scope.Session = "b"
	for _, c := range []struct {
		name   string
		mutate func(map[string]any)
		want   bool
	}{
		{"valid", func(map[string]any) {}, true},
		{"minimal family kept", func(l map[string]any) { l["Families"] = retiredFamily(scope, 7, 7, "") }, true},
		{"family below its high", func(l map[string]any) { l["Families"] = retiredFamily(scope, 7, 6, "") }, false},
		{"family receipt kept", func(l map[string]any) { l["Families"] = retiredFamily(scope, 7, 7, "r") }, false},
		{"lineage fence left", func(l map[string]any) {
			l["LineageFences"] = map[string]any{"x": map[string]any{"Session": "b", "Fence": 1}}
		}, false},
		{"archived STOPPED left", func(l map[string]any) {
			l["AckArchive"] = map[string]string{viewcontrol.ArchiveKey(scope, "r1", "STOPPED", 3): "h"}
		}, false},
		{"ticket left", func(l map[string]any) { l["Tickets"] = map[string]any{"x": map[string]any{"Scope": scope}} }, false},
		{"lease left", func(l map[string]any) {
			l["Leases"] = map[string]any{"x": map[string]any{"Lease": map[string]any{"scope": scope}}}
		}, false},
		{"reservation left", func(l map[string]any) {
			l["Reservations"] = map[string]any{"x": map[string]any{"Reservation": map[string]any{"scope": scope}, "Status": "CLOSED"}}
		}, false},
		{"forged signature", func(l map[string]any) { retiredRow(l)["Retired"] = map[string]any{"Signature": []byte("AA")} }, false},
		{"row changed after signing", func(l map[string]any) { retiredRow(l)["Sequence"] = 5 }, false},
		{"null tombstone is unretired", func(l map[string]any) { retiredRow(l)["Retired"] = nil }, false},
		{"fence moved", func(l map[string]any) { l["Fences"] = map[string]uint64{"b": 2} }, false},
		{"still issued an event", func(l map[string]any) {
			l["Recall"] = map[string]any{"ID": "r2", "Events": map[string]any{"a": map[string]any{}, "b": map[string]any{}}}
		}, false},
	} {
		ledger := retiredLedgerMap(t, scope, operator, authority.enrollmentHash)
		c.mutate(ledger)
		raw, l := decodeLedger(t, ledger)
		if got := l.validFenced(raw, authority); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}

// TestFactsRefuseFencedScope: CheckPhysical refuses every check of a
// fenced session, retired or not, right after the ledger check
// (refuseFenced); a live peer's is not refused there. CheckPhysical itself
// runs only with the controller's retained-sources context, which nothing
// outside viewcontrol can build, so the end-to-end path (a survivor's
// release and retirements passing CheckPhysical with a real retired ledger
// and signature) is TestZQ3BlockedDomainFencedPeerReopens.
func TestFactsRefuseFencedScope(t *testing.T) {
	operator := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	var fenced, live guestbridge.Context
	fenced.Session, live.Session = "b", "a"
	_, l := decodeLedger(t, retiredLedgerMap(t, fenced, operator, "enrollment"))
	if err := l.refuseFenced(fenced); !errors.Is(err, viewcontrol.ErrSessionFenced) {
		t.Fatalf("retired scope's physical check: %v", err)
	}
	if err := l.refuseFenced(live); err != nil {
		t.Fatalf("live scope's physical check: %v", err)
	}
}

// retiredLedgerMap is a schema-3 ledger of two domains a and scope.Session
// in which the latter is fenced and retired with operator's real signature.
func retiredLedgerMap(t *testing.T, scope guestbridge.Context, operator ed25519.PrivateKey, enrollment string) map[string]any {
	t.Helper()
	statement := viewcontrol.NewFencedRetirement(enrollment, scope.Session, "r1", "INSTALL_DRAIN", 4, 1)
	signature, err := viewcontrol.SignFencedRetirement(operator, statement)
	require(t, err)
	return map[string]any{
		"Schema":     viewcontrol.LedgerSchemaFenced,
		"Domains":    map[string]any{"a": map[string]any{}, scope.Session: map[string]any{"Domain": map[string]any{"scope": scope}}},
		"Fences":     map[string]uint64{},
		"Fenced":     map[string]any{scope.Session: map[string]any{"Recall": "r1", "Phase": "INSTALL_DRAIN", "Sequence": 4, "Fence": 1, "Retired": map[string]any{"Signature": signature}}},
		"Recall":     map[string]any{"ID": "r2", "Events": map[string]any{"a": map[string]any{}}},
		"AckArchive": map[string]string{},
	}
}

// retiredRow returns session b's Fenced row of a retiredLedgerMap ledger.
func retiredRow(l map[string]any) map[string]any {
	return l["Fenced"].(map[string]any)["b"].(map[string]any)
}

// retiredFamily is a Families map with one family of scope.
func retiredFamily(scope guestbridge.Context, high, floor uint64, receipt string) map[string]any {
	return map[string]any{"f": map[string]any{"High": high, "Floor": floor, "Scope": scope, "Receipt": receipt}}
}

// decodeLedger returns the raw bytes of ledger and their factsLedger.
func decodeLedger(t *testing.T, ledger map[string]any) (json.RawMessage, factsLedger) {
	t.Helper()
	raw, err := json.Marshal(ledger)
	require(t, err)
	var l factsLedger
	require(t, json.Unmarshal(raw, &l))
	return raw, l
}
