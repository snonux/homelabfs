package controlledvalidation

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

// familyCall allocates and runs one call of the claim family that differs
// from the harness original only by nonce (a second guest request stream in
// the same session), numbered explicitly, and returns its OK result.
func familyCall(h *allocationHarness, nonce string, number uint64, op string, args any) (guestbridge.Call, wireResult) {
	h.f.t.Helper()
	call := h.original
	call.Claim.Nonce, call.Claim.Number = nonce, number
	raw, err := canonicalJSON(args)
	mustAllocation(h.f.t, err)
	call.Intent = guestbridge.Intent{Opcode: op, Args: raw}
	raw, err = canonicalJSON(call.Intent)
	mustAllocation(h.f.t, err)
	call.Claim.OriginalHash = hash(raw)
	_, err = h.send("ALLOCATE", call)
	mustAllocation(h.f.t, err)
	_, err = h.send("CALL", call)
	mustAllocation(h.f.t, err)
	e, ok := h.stored(call)
	var result wireResult
	if !ok || json.Unmarshal(e.Result, &result) != nil {
		h.f.t.Fatalf("%s claim %s/%d has no result", op, nonce, number)
	}
	requireCode(h.f.t, result, "OK")
	return call, result
}

// retireFamily retires calls (one family, numbers 1..n in order).
func retireFamily(t *testing.T, h *allocationHarness, calls []guestbridge.Call) {
	t.Helper()
	var hashes []string
	for _, call := range calls {
		hashes = append(hashes, resultID(t, h, call))
	}
	last := calls[len(calls)-1]
	raw, err := h.send("RETIRE", map[string]any{"realm": last.Claim.Realm, "subject": last.Claim.Subject, "anchor": last.Claim.Anchor, "nonce": last.Claim.Nonce, "through": last.Claim.Number, "result_hashes": hashes, "credentials": last.Credentials})
	mustAllocation(t, err)
	if allocationPayload(t, raw)["status"] != "RETIRED" {
		t.Fatalf("retirement refused: %s", raw)
	}
}

// twoFamilyRetireWindow builds a two-family session whose provider snapshot
// holds a tombstone at one family's floor while another family retires
// (u64):
//   - family A: ROOT (A1), LOOKUP entry-000 (A2, grant G), FORGET G (A3; G
//     becomes a tombstone owned by A3), retired through A3, which prunes G;
//   - family B: LOOKUP entry-001 (B1, grant H), FORGET H (B2), LOOKUP
//     entry-002 (B3, grant K, stays live);
//   - G is put back into the snapshot (keepTombstone), as if A's prune had
//     been skipped, and B retires through B3, which prunes H but not G.
//
// It returns the snapshot just before B's retirement (the row a bridge
// crashing in B's retire window holds), the snapshot after it and the
// tokens of G and K. The row-only prune (o64) drops G from that row while
// the snapshot keeps it, and so refused the restore as Stale.
func twoFamilyRetireWindow(t *testing.T, h *allocationHarness) (before, after []byte, g, k string) {
	t.Helper()
	seedCleanupDirectory(h, 3)
	a, b := h.original.Claim.Nonce, id(70).String()
	name := func(i int) string { return hex.EncodeToString([]byte(fmt.Sprintf("entry-%03d", i))) }
	a1, root := familyCall(h, a, 1, "ROOT", map[string]any{})
	a2, lookupG := familyCall(h, a, 2, "LOOKUP", map[string]any{"parent": root.Capabilities[0], "name": name(0)})
	a3, _ := familyCall(h, a, 3, "FORGET", map[string]any{"object": lookupG.Capabilities[0], "count": 1})
	tombstone := protectedSnapshot(t, h)
	retireFamily(t, h, []guestbridge.Call{a1, a2, a3})
	b1, lookupH := familyCall(h, b, 1, "LOOKUP", map[string]any{"parent": root.Capabilities[0], "name": name(1)})
	b2, _ := familyCall(h, b, 2, "FORGET", map[string]any{"object": lookupH.Capabilities[0], "count": 1})
	b3, lookupK := familyCall(h, b, 3, "LOOKUP", map[string]any{"parent": root.Capabilities[0], "name": name(2)})
	g, k = lookupG.Capabilities[0].Token, lookupK.Capabilities[0].Token
	keepTombstone(t, h, tombstone, g)
	before = protectedSnapshot(t, h)
	retireFamily(t, h, []guestbridge.Call{b1, b2, b3})
	after = protectedSnapshot(t, h)
	grants := func(raw []byte) map[string]struct{ Released bool } {
		var state struct {
			Grants map[string]struct{ Released bool }
		}
		mustAllocation(t, json.Unmarshal(raw, &state))
		return state.Grants
	}
	if pre, post := grants(before), grants(after); !pre[g].Released || !pre[lookupH.Capabilities[0].Token].Released || !post[g].Released || len(post) != len(pre)-1 {
		t.Fatalf("setup did not keep tombstone G while pruning H: %d -> %d grants", len(pre), len(post))
	}
	return before, after, g, k
}

// keepTombstone copies grant token from the earlier snapshot from into the
// session's protected snapshot and its owner entry (which must stay equal,
// or Retire skips pruning), emulating a retirement whose prune was skipped.
func keepTombstone(t *testing.T, h *allocationHarness, from []byte, token string) {
	t.Helper()
	scope := h.config.Scopes[0]
	mustAllocation(t, h.f.provider.db.Update(func(tx *bolt.Tx) error {
		bridge := h.f.provider.bucket(tx, "bridge")
		state := exactMap(t, bridge.Get([]byte(scope.Session)))
		state["Grants"].(map[string]any)[token] = exactMap(t, from)["Grants"].(map[string]any)[token]
		snapshot, err := json.Marshal(state)
		if err != nil {
			return err
		}
		ownerKey := bridge.Get([]byte(scope.Session + "/owner"))
		var owner entry
		if err = json.Unmarshal(h.f.provider.bucket(tx, "claims").Get(ownerKey), &owner); err != nil {
			return err
		}
		owner.BridgeState = snapshot
		if err = h.f.provider.putClaim(tx, ownerKey, owner); err != nil {
			return err
		}
		return bridge.Put([]byte(scope.Session), snapshot)
	}))
}

// TestRestoreAcceptsTwoFamilyRowSavedBeforeRetirePrune (u64): a row saved in
// the retire window of a two-family session restores, because the restore
// check prunes the snapshot as well as the row at every family floor, and
// every live-grant difference is still refused.
func TestRestoreAcceptsTwoFamilyRowSavedBeforeRetirePrune(t *testing.T) {
	h := newAllocationHarnessProtocol(t, guestbridge.CleanupProtocol())
	before, after, g, k := twoFamilyRetireWindow(t, h)
	var owner struct{ Controller fsapi.UUID }
	mustAllocation(t, json.Unmarshal(after, &owner))
	check := func(row []byte) error {
		return h.f.provider.CheckRestoredBridge(t.Context(), h.config.Scopes[0], owner.Controller, row)
	}
	// The unedited round trip shows that the negatives below fail on their
	// edit alone, not on re-encoding.
	unedited := editGrants(t, before, func(map[string]map[string]any) {})
	for name, row := range map[string][]byte{"pre-prune row": before, "pruned row": after, "unedited round trip": unedited} {
		if err := check(row); err != nil {
			t.Fatalf("%s refused: %v", name, err)
		}
	}
	for name, edit := range twoFamilyNegatives(t, g, k, h.original.Claim) {
		// A negative whose edit leaves the row unchanged would be accepted
		// correctly and so fail spuriously (yg4); refuse such a setup.
		row := editGrants(t, before, edit)
		if bytes.Equal(row, unedited) {
			t.Fatalf("%s edit left the row unchanged", name)
		}
		if err := check(row); !errors.Is(err, fsapi.Stale) {
			t.Fatalf("%s restored: %v", name, err)
		}
	}
}

// distinctToken returns token with its first hex digit replaced by a
// different one, so the result never equals token. A fixed prefix such as
// "ab"+token[2:] equals token whenever the random token already starts with
// that prefix (1 in 256), which turned the "extra live grant" negative into
// a no-op edit that the restore check rightly accepted (yg4).
func distinctToken(token string) string {
	first := "1"
	if token[0] == '1' {
		first = "2"
	}
	return first + token[1:]
}

// TestTwoFamilyNegativesAlwaysEditTheRow (yg4) pins the token prefixes that
// once made the "extra live grant" negative a no-op: K starting with "ab"
// (the old fixed prefix) and with "1" (distinctToken's own default digit).
// Every negative must change a row holding live K and tombstone G.
func TestTwoFamilyNegativesAlwaysEditTheRow(t *testing.T) {
	tail := strings.Repeat("0", 62)
	g := "cd" + tail
	for _, k := range []string{"ab" + tail, "1" + tail + "0", "2" + tail + "0"} {
		row, err := json.Marshal(map[string]any{"Grants": map[string]any{
			g: map[string]any{"Cap": map[string]any{"token": g}, "Handle": 1, "Released": true},
			k: map[string]any{"Cap": map[string]any{"token": k}, "Handle": 2, "Released": false},
		}})
		mustAllocation(t, err)
		unedited := editGrants(t, row, func(map[string]map[string]any) {})
		for name, edit := range twoFamilyNegatives(t, g, k, guestbridge.Claim{}) {
			if bytes.Equal(editGrants(t, row, edit), unedited) {
				t.Errorf("K=%s...: %s edit left the row unchanged", k[:2], name)
			}
		}
	}
}

// twoFamilyNegatives are row edits that must stay Stale. K is live, so
// marking it released by owner (A1, at or below A's floor) makes it
// prunable on the row side only; G is a tombstone at A's floor, so reviving
// it makes it unprunable on the row side only.
func twoFamilyNegatives(t *testing.T, g, k string, owner guestbridge.Claim) map[string]func(map[string]map[string]any) {
	return map[string]func(map[string]map[string]any){
		"missing live grant": func(grants map[string]map[string]any) { delete(grants, k) },
		"extra live grant": func(grants map[string]map[string]any) {
			raw, err := json.Marshal(grants[k])
			mustAllocation(t, err)
			extra := exactMap(t, raw)
			extra["Cap"].(map[string]any)["token"] = distinctToken(k)
			grants[distinctToken(k)] = extra
		},
		"changed live grant": func(grants map[string]map[string]any) { grants[k]["Handle"] = 7 },
		"live grant released at the floor": func(grants map[string]map[string]any) {
			grants[k]["Released"] = true
			grants[k]["Owner"] = map[string]any{"realm": owner.Realm, "subject": owner.Subject, "anchor": owner.Anchor, "nonce": owner.Nonce, "number": owner.Number}
		},
		"pruned tombstone revived": func(grants map[string]map[string]any) { grants[g]["Released"] = false },
	}
}

// editGrants returns row with edit applied to its decoded Grants. Numbers
// are kept exact, so an unedited round trip compares equal.
func editGrants(t *testing.T, row []byte, edit func(map[string]map[string]any)) []byte {
	t.Helper()
	state := exactMap(t, row)
	grants := map[string]map[string]any{}
	for token, value := range state["Grants"].(map[string]any) {
		grants[token] = value.(map[string]any)
	}
	edit(grants)
	state["Grants"] = grants
	raw, err := json.Marshal(state)
	mustAllocation(t, err)
	return raw
}

func exactMap(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	mustAllocation(t, decoder.Decode(&out))
	return out
}
