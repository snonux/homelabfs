//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// TestBarrieredEndpointRestartAfterPrunedRetire (o64 review): a RETIRE that
// prunes released grants from the provider's protected snapshot must leave
// the bridge's persisted session rows equal to it, so the endpoint still
// restores (before the fix the rows kept the tombstones and the restore
// compared Stale). The retirement is re-verified by the real producer, and a
// CALL after the restart still runs.
func TestBarrieredEndpointRestartAfterPrunedRetire(t *testing.T) {
	d := startRestartChild(t, buildRestartChild(t))
	d.autoReclaim = true
	d.hello()
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
	last := calls[len(calls)-1].Claim
	_, wire := d.exchange(0, "RETIRE", map[string]any{"realm": last.Realm, "subject": last.Subject, "anchor": last.Anchor, "nonce": last.Nonce, "through": last.Number, "result_hashes": hashes, "credentials": d.rig.credentials}, nil)
	if !bytes.Contains(wire, []byte(`"status":"RETIRED"`)) {
		t.Fatalf("retirement refused: %s", wire)
	}
	// As in TestBarrieredMutatingEndpointRestartRealC: the controller reopens
	// only when its retained cuts equal the current one, so take the current
	// cut with a DOMAIN lease after the mutations and retire the first one.
	family := d.rig.facts.enrollment.TicketFamilies[0].Acquisition
	acquire := func(number uint64) []byte {
		_, raw := d.exchange(0, "VIEW_ACQUIRE", map[string]any{"request": guestbridge.Ticket{Anchor: family.Anchor, Nonce: family.Nonce, Number: number, Credentials: d.rig.credentials}, "domain": root.Capabilities[1], "access": guestbridge.ViewAccess{Kind: "DOMAIN"}, "mode": "READ", "target": nil}, nil)
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
	d.ordinary(0, "GETATTR", map[string]any{"object": root.Capabilities[0]})
}
