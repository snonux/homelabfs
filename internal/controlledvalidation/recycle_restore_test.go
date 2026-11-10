package controlledvalidation

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// TestRestoreAcceptsRowSavedBeforeRetirePrune (o64 review): a bridge session
// row persisted before a pruning Retire (a crash between the provider commit
// and the bridge persist) still restores, the pruned row restores, and a row
// missing a live grant does not.
func TestRestoreAcceptsRowSavedBeforeRetirePrune(t *testing.T) {
	h := newAllocationHarnessProtocol(t, guestbridge.CleanupProtocol())
	seedCleanupDirectory(h, 2)
	root := cleanupRoot(h)
	page := cleanupCall(h, "READDIR", map[string]any{"object": root, "after": "", "revision": 0, "limit": 256})
	_, err := h.send("READDIR_CLEANUP", cleanupRequest(t, func() entry { e, _ := h.stored(page); return e }()))
	mustAllocation(t, err)
	cleanupCall(h, "GETATTR", map[string]any{"object": root})
	scope := h.config.Scopes[0]
	before := protectedSnapshot(t, h)
	rootCall := h.original
	rootCall.Claim.Number = 1
	retire := map[string]any{"realm": page.Claim.Realm, "subject": page.Claim.Subject, "anchor": page.Claim.Anchor, "nonce": page.Claim.Nonce, "through": page.Claim.Number, "result_hashes": []string{resultID(t, h, rootCall), resultID(t, h, page)}, "credentials": page.Credentials}
	_, err = h.send("RETIRE", retire)
	mustAllocation(t, err)
	after := protectedSnapshot(t, h)
	if grantCount(t, before) != 4 || grantCount(t, after) != 2 {
		t.Fatalf("retirement pruned %d -> %d grants, want 4 -> 2", grantCount(t, before), grantCount(t, after))
	}
	var owner struct{ Controller fsapi.UUID }
	mustAllocation(t, json.Unmarshal(after, &owner))
	for name, row := range map[string][]byte{"pre-prune row": before, "pruned row": after} {
		if err = h.f.provider.CheckRestoredBridge(t.Context(), scope, owner.Controller, row); err != nil {
			t.Fatalf("%s refused: %v", name, err)
		}
	}
	if err = h.f.provider.CheckRestoredBridge(t.Context(), scope, owner.Controller, withoutOneGrant(t, after)); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("row missing a live grant restored: %v", err)
	}
}

func withoutOneGrant(t *testing.T, snapshot []byte) []byte {
	t.Helper()
	var state map[string]any
	mustAllocation(t, json.Unmarshal(snapshot, &state))
	grants := state["Grants"].(map[string]any)
	for token := range grants {
		delete(grants, token)
		break
	}
	raw, err := json.Marshal(state)
	mustAllocation(t, err)
	return raw
}

func protectedSnapshot(t *testing.T, h *allocationHarness) []byte {
	t.Helper()
	raw, err := h.f.provider.Restore(t.Context(), h.config.Scopes[0])
	mustAllocation(t, err)
	return raw
}

func grantCount(t *testing.T, snapshot []byte) int {
	t.Helper()
	var state struct{ Grants map[string]json.RawMessage }
	mustAllocation(t, json.Unmarshal(snapshot, &state))
	return len(state.Grants)
}
