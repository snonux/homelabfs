//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// TestBarrieredEndpointRestartAfterPrunedRetire (o64 review): a RETIRE that
// prunes released grants from the provider's protected snapshot must leave
// the bridge's persisted session rows equal to it, so the endpoint still
// restores (before the fix the rows kept the tombstones and the restore
// compared Stale). The retirement is re-verified by the real producer, and a
// CALL after the restart still runs.
//
// u64 pins fix 1 (Service.retire reloading the pruned snapshot) on its own:
// right after the RETIRE the bridge's in-memory and committed session rows
// must equal the provider snapshot exactly. The restart alone cannot show
// that, because CheckRestoredBridge also prunes a stale row (fix 2) and so
// accepts the endpoint even when the reload is missing.
func TestBarrieredEndpointRestartAfterPrunedRetire(t *testing.T) {
	d := startRestartChild(t, buildRestartChild(t))
	d.autoReclaim = true
	d.hello()
	root, calls, hashes := releaseCreatedFile(t, d)
	last := calls[len(calls)-1].Claim
	before := providerGrants(t, d, 0)
	_, wire := d.exchange(0, "RETIRE", map[string]any{"realm": last.Realm, "subject": last.Subject, "anchor": last.Anchor, "nonce": last.Nonce, "through": last.Number, "result_hashes": hashes, "credentials": d.rig.credentials}, nil)
	if !bytes.Contains(wire, []byte(`"status":"RETIRED"`)) {
		t.Fatalf("retirement refused: %s", wire)
	}
	if after := providerGrants(t, d, 0); after >= before {
		t.Fatalf("retirement pruned no grant (%d -> %d), so the comparison below proves nothing", before, after)
	}
	assertSessionEqualsProvider(t, d, 0)
	restartAfterRetire(t, d, root)
}

// releaseCreatedFile runs ROOT, then CREATE of one file whose description
// and object grants are closed, released and forgotten, so a RETIRE through
// the last claim prunes both tombstones. It returns the root capabilities,
// the calls and their result ids in claim order.
func releaseCreatedFile(t *testing.T, d *restartChild) ([]guestbridge.Capability, []guestbridge.Call, []string) {
	t.Helper()
	rootCall, rootRaw := d.root(0)
	var root struct{ Capabilities []guestbridge.Capability }
	require(t, json.Unmarshal(rootRaw, &root))
	flags := map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}
	createCall, created := d.ordinary(0, "CREATE", map[string]any{"parent": root.Capabilities[0], "name": "7072756e6564", "mode": 0660, "umask": 0, "flags": flags})
	var file struct{ Capabilities []guestbridge.Capability }
	require(t, json.Unmarshal(created, &file))
	calls := []guestbridge.Call{rootCall, createCall}
	results := [][]byte{rootRaw, created}
	for _, step := range []struct {
		op   string
		args map[string]any
	}{
		{"DESCRIPTION_CLOSE", map[string]any{"description": file.Capabilities[1], "process": d.rig.credentials.Process, "close_sequence": 1}},
		{"DESCRIPTION_RELEASE", map[string]any{"description": file.Capabilities[1], "release_sequence": 1}},
		{"FORGET", map[string]any{"object": file.Capabilities[0], "count": 1}},
	} {
		call, result := d.ordinary(0, step.op, step.args)
		calls, results = append(calls, call), append(results, result)
	}
	var hashes []string
	for _, result := range results {
		var r struct{ ID string }
		require(t, json.Unmarshal(result, &r))
		hashes = append(hashes, r.ID)
	}
	return root.Capabilities, calls, hashes
}

// restartAfterRetire suspends and reopens the endpoint and runs a CALL in the
// restored one. As in TestBarrieredMutatingEndpointRestartRealC: the
// controller reopens only when its retained cuts equal the current one, so
// it first takes the current cut with a DOMAIN lease after the mutations and
// retires the first one.
func restartAfterRetire(t *testing.T, d *restartChild, root []guestbridge.Capability) {
	t.Helper()
	family := d.rig.facts.enrollment.TicketFamilies[0].Acquisition
	acquire := func(number uint64) []byte {
		_, raw := d.exchange(0, "VIEW_ACQUIRE", map[string]any{"request": guestbridge.Ticket{Anchor: family.Anchor, Nonce: family.Nonce, Number: number, Credentials: d.rig.credentials}, "domain": root[1], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": nil}, nil)
		return raw
	}
	var first struct {
		ID    string
		Lease guestbridge.ViewLease
	}
	require(t, json.Unmarshal(restartPayload(t, acquire(1)), &first))
	d.releaseAndRetireRead(first.Lease, first.ID)
	acquire(2)
	restart, err := d.rig.supervisor.SuspendEndpoint(t.Context(), d.rig.producer, d.transport)
	require(t, err)
	assembly, err := restart.Reopen(t.Context())
	cleanupRestored(t, assembly)
	require(t, err)
	d.install(assembly)
	require(t, restart.resumeExporter(t.Context()))
	d.ordinary(0, "GETATTR", map[string]any{"object": root[0]})
}

// providerGrants counts the grants in the provider's protected bridge
// snapshot of session's scope.
func providerGrants(t *testing.T, d *restartChild, session int) int {
	t.Helper()
	var state struct{ Grants map[string]json.RawMessage }
	require(t, json.Unmarshal(providerSnapshot(t, d, session), &state))
	return len(state.Grants)
}

func providerSnapshot(t *testing.T, d *restartChild, session int) []byte {
	t.Helper()
	peer := d.rig.peers[session]
	raw, err := peer.provider.Restore(t.Context(), peer.scope)
	require(t, err)
	return raw
}

// assertSessionEqualsProvider (u64) requires the bridge's live in-memory
// session and the row committed in the transport ledger to equal the
// provider's protected snapshot in every field both carry, with exact
// integers. Without Service.retire's reload the bridge would still hold the
// pruned tombstones here, until the next CALL.
func assertSessionEqualsProvider(t *testing.T, d *restartChild, session int) {
	t.Helper()
	peer := d.rig.peers[session]
	var snapshot map[string]json.RawMessage
	require(t, json.Unmarshal(providerSnapshot(t, d, session), &snapshot))
	live, committed, err := d.transport.SessionEvidence()
	require(t, err)
	for name, rows := range map[string]map[string]guestbridge.ProtectedBridgeSession{"in-memory": live, "committed": committed} {
		row, ok := rows[peer.scope.Session]
		if !ok {
			t.Fatalf("%s bridge session missing", name)
		}
		var state map[string]json.RawMessage
		require(t, json.Unmarshal(row.State, &state))
		state["Controller"], err = json.Marshal(row.Controller)
		require(t, err)
		for _, key := range []string{"Controller", "Context", "Generation", "Credentials", "Exited", "Grants", "CloseSequences"} {
			if !exactJSON(t, state[key], snapshot[key]) {
				t.Fatalf("%s bridge session %s differs from the provider snapshot after RETIRE (%d vs %d bytes)", name, key, len(state[key]), len(snapshot[key]))
			}
		}
	}
}

// exactJSON compares two JSON values semantically, keeping numbers exact.
func exactJSON(t *testing.T, a, b []byte) bool {
	t.Helper()
	decode := func(raw []byte) any {
		var v any
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		require(t, decoder.Decode(&v))
		return v
	}
	return reflect.DeepEqual(decode(a), decode(b))
}
