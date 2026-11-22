package controlledvalidation

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

// ageEpoch is the pinned clock reading the l64 age tests allocate at.
var ageEpoch = time.Unix(1_800_000_000, 0)

// TestLedgerEvidencePendingAge (l64): a RESERVED/UNPREPARED claim's entry
// stores the allocation time, and OBSERVE reports its Age as the seconds
// from that time to the OBSERVE's own clock reading, on Pending only.
func TestLedgerEvidencePendingAge(t *testing.T) {
	f := newFixture(t)
	clock := &steppedClock{at: ageEpoch}
	f.provider.now = clock.now
	b := f.binding("GETATTR", map[string]any{})
	if got := loadEntry(t, f, b).Opened; got != ageEpoch.Unix() {
		t.Fatalf("Opened %d, want %d", got, ageEpoch.Unix())
	}
	clock.at = ageEpoch.Add(42 * time.Second)
	c := ledgerClaims(t, f)
	if len(c.Pending) != 1 || c.Pending[0].Age != 42 {
		t.Fatalf("pending %+v", c.Pending)
	}
	if len(c.Recent) != 1 || c.Recent[0].Age != 0 {
		t.Fatalf("recent carries an age %+v", c.Recent)
	}
	pending, _ := json.Marshal(c.Pending)
	recent, _ := json.Marshal(c.Recent)
	if !bytes.Contains(pending, []byte(`"Age":42`)) || bytes.Contains(recent, []byte(`"Age"`)) {
		t.Fatalf("json pending %s recent %s", pending, recent)
	}
	// The same stored entry and clock give the same age: replay-stable.
	if again := ledgerClaims(t, f); again.Pending[0] != c.Pending[0] {
		t.Fatalf("age not deterministic %+v %+v", again.Pending[0], c.Pending[0])
	}
}

// TestLedgerEvidenceAgeOmitted (l64): no Age, and no "Age" key, when the
// clock steps back before Opened or the entry predates l64 (no Opened).
func TestLedgerEvidenceAgeOmitted(t *testing.T) {
	f := newFixture(t)
	clock := &steppedClock{at: ageEpoch}
	f.provider.now = clock.now
	b := f.binding("GETATTR", map[string]any{})
	clock.at = ageEpoch.Add(-5 * time.Second)
	requireNoAge(t, ledgerClaims(t, f))

	clock.at = ageEpoch.Add(time.Hour)
	old := loadEntry(t, f, b)
	old.Opened = 0
	mustAllocation(t, f.provider.db.Update(func(tx *bolt.Tx) error {
		return f.provider.putClaim(tx, claimKey(old.Claim), old)
	}))
	requireNoAge(t, ledgerClaims(t, f))
}

// TestEntryFootprintOmitsOpened (l64): Opened never changes a credit measure.
func TestEntryFootprintOmitsOpened(t *testing.T) {
	e := entry{Version: ledgerVersion, Status: "ALLOCATED"}
	without, err := entryFootprint(e)
	mustAllocation(t, err)
	e.Opened = 1<<63 - 1
	with, err := entryFootprint(e)
	mustAllocation(t, err)
	if !bytes.Equal(with, without) {
		t.Fatalf("footprint %s != %s", with, without)
	}
}

func requireNoAge(t *testing.T, c ClaimEvidence) {
	t.Helper()
	raw, _ := json.Marshal(c)
	if len(c.Pending) != 1 || c.Pending[0].Age != 0 || bytes.Contains(raw, []byte(`"Age"`)) {
		t.Fatalf("unexpected age %s", raw)
	}
}

func ledgerClaims(t *testing.T, f *fixture) ClaimEvidence {
	t.Helper()
	evidence, err := f.provider.LedgerEvidence(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	return evidence.Claims
}

func loadEntry(t *testing.T, f *fixture, b guestbridge.Binding) entry {
	t.Helper()
	var e entry
	mustAllocation(t, f.provider.db.View(func(tx *bolt.Tx) error {
		var err error
		e, _, err = f.provider.load(tx, b.Call.Claim)
		return err
	}))
	return e
}
