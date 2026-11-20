package controlledvalidation

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

// fullPendingStage is checkPendingStage as it was before te4: a scan of
// every claims entry. The indexed version must always agree with it.
func fullPendingStage(p *Provider, tx *bolt.Tx, scope guestbridge.Context, except fsapi.UUID) error {
	return p.bucket(tx, "claims").ForEach(func(_, raw []byte) error {
		var e entry
		if err := json.Unmarshal(raw, &e); err != nil {
			return err
		}
		if e.Binding.Context == scope && e.Binding.Operation != except && e.Status == "PENDING" && e.Plan != nil && len(e.BridgeState) > 0 {
			return fsapi.WouldBlock
		}
		return nil
	})
}

// requirePendingAgrees compares the indexed checkPendingStage with the full
// scan and the expected verdict. CheckViewReady reads the same candidates,
// so the candidate set is what this pins down.
func requirePendingAgrees(t *testing.T, f *fixture, scope guestbridge.Context, blocked bool) {
	t.Helper()
	if err := f.provider.db.View(func(tx *bolt.Tx) error {
		indexed, full := f.provider.checkPendingStage(tx, scope, fsapi.UUID{}), fullPendingStage(f.provider, tx, scope, fsapi.UUID{})
		if !errors.Is(indexed, full) || errors.Is(indexed, fsapi.WouldBlock) != blocked {
			t.Fatalf("indexed %v, full scan %v, want blocked=%t", indexed, full, blocked)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// TestPendingIndexEqualsFullScan drives one claim through PENDING, a rolled
// back settle, a same-transaction settle-and-repend (the generation check),
// a provider reopen (the index is reseeded from disk) and a committed
// settle, comparing the indexed admission exclusion with the former full
// scan at each step.
func TestPendingIndexEqualsFullScan(t *testing.T) {
	f := newFixture(t)
	b := f.binding("DRAIN_WRITE", map[string]any{})
	key := claimKey(b.Call.Claim)
	pending := entry{Version: ledgerVersion, Claim: b.Call.Claim, Binding: b, Status: "PENDING", Plan: &fsservice.ControlledPlan{}, BridgeState: json.RawMessage(`{"snapshot":1}`)}
	settled := pending
	settled.Status = "TERMINAL"
	write := func(fn func(tx *bolt.Tx) error) error { return f.provider.db.Update(fn) }
	requirePendingAgrees(t, f, b.Context, false)
	mustAllocation(t, write(func(tx *bolt.Tx) error { return f.provider.putClaim(tx, key, pending) }))
	requirePendingAgrees(t, f, b.Context, true)
	rollback := errors.New("rolled back")
	if err := write(func(tx *bolt.Tx) error {
		if err := f.provider.putClaim(tx, key, settled); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	requirePendingAgrees(t, f, b.Context, true)
	mustAllocation(t, write(func(tx *bolt.Tx) error {
		if err := f.provider.putClaim(tx, key, settled); err != nil {
			return err
		}
		return f.provider.putClaim(tx, key, pending)
	}))
	requirePendingAgrees(t, f, b.Context, true)
	if err := f.provider.Close(); err != nil {
		t.Fatal(err)
	}
	f.open()
	requirePendingAgrees(t, f, b.Context, true)
	mustAllocation(t, write(func(tx *bolt.Tx) error { return f.provider.putClaim(tx, key, settled) }))
	requirePendingAgrees(t, f, b.Context, false)
	if n := len(f.provider.pending.candidates); n != 0 {
		t.Fatalf("settled claim still indexed (%d candidates)", n)
	}
}
