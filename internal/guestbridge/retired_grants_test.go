package guestbridge

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// TestPruneRetiredGrants (o64): only released grants whose owning claim is
// retired in the same principal are dropped, with their close sequences.
func TestPruneRetiredGrants(t *testing.T) {
	scope := Context{Session: "s"}
	family := ClaimNumber{Realm: "r", Subject: "61", Anchor: "a", Nonce: "n"}
	at := func(n uint64) *ClaimNumber { c := family; c.Number = n; return &c }
	other := *at(3)
	other.Nonce = "other"
	snapshot := validationSnapshot{CapabilityLifetimes: 2, Context: scope, Grants: map[string]*grant{
		"retired":      {Released: true, Owner: at(3)},
		"at-floor":     {Released: true, Owner: at(5)},
		"after-floor":  {Released: true, Owner: at(6)},
		"other-family": {Released: true, Owner: &other},
		"no-owner":     {Released: true},
		"live":         {Owner: at(2)},
	}, CloseSequences: map[string]uint64{"retired:p": 1, "live:p": 2}}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	floor := *at(5)
	pruned, n, err := PruneRetiredGrants(scope, raw, floor)
	if err != nil || n != 2 {
		t.Fatalf("pruned %d grants: %v", n, err)
	}
	var out validationSnapshot
	if err = json.Unmarshal(pruned, &out); err != nil {
		t.Fatal(err)
	}
	for _, kept := range []string{"after-floor", "other-family", "no-owner", "live"} {
		if out.Grants[kept] == nil {
			t.Fatalf("grant %q was pruned", kept)
		}
	}
	if len(out.Grants) != 4 || len(out.CloseSequences) != 1 || out.CloseSequences["live:p"] != 2 {
		t.Fatalf("unexpected pruned snapshot: %d grants, closes %v", len(out.Grants), out.CloseSequences)
	}
	if same, n, _ := PruneRetiredGrants(scope, pruned, floor); n != 0 || string(same) != string(pruned) {
		t.Fatal("a second prune changed the snapshot")
	}
	if _, _, err = PruneRetiredGrants(Context{Session: "x"}, raw, floor); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("foreign scope: %v", err)
	}
}
