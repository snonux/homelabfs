//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// TestQueryAnsweredWhileACKObservationInFlight is the d84 regression for
// zq3-r8 R8-1. A VIEW_ACK whose bytes no observation records yet needs a
// guest probe (2 s here) before the host may accept it. That probe used to
// run on the transport's one shared control worker, so a guest QUERY queued
// behind the ACK waited for the whole probe. With the VIEW_ACK lane the
// QUERY, in the same session, is answered while the probe is still running,
// and the ACK is still accepted only after the probe measured it.
func TestQueryAnsweredWhileACKObservationInFlight(t *testing.T) {
	r, c := assemblyFixture(t)
	var slow atomic.Bool
	var probes atomic.Int32
	entered := make(chan struct{}, 4)
	slowProbe(r.admission, &slow, &probes, entered)
	a, err := Assemble(t.Context(), c)
	t.Cleanup(func() { require(t, a.Close()) })
	require(t, err)
	r.supervisor.record.Stage = "claimed" // declared native-export/INIT primitive
	counter := uint64(0)
	for _, peer := range r.peers {
		attachAssembly(peer, a, &counter)
	}
	// The all-domain recall comes from mount 1's CREATE conflicting with both
	// active leases, as in TestRecallAcksReuseObservationWithSlowObserver.
	domain, mount1 := mountBoth(t, r, c.Authorization.Enrollment.TicketFamilies)
	r.peers[1].call("CREATE", map[string]any{"parent": mount1[0], "name": "64383461636b6c616e65", "mode": 0o660, "umask": 0, "flags": map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}})

	chain := &r5Chain{peer: r.peers[0]}
	ack := recallACK(t, chain, domain, "STOPPED")
	event := ack["event"].(map[string]json.RawMessage)
	delete(ack, "event")
	// A fresh observation, so the QUERY below needs no probe of its own while
	// the ACK (not recorded in it) must probe.
	require(t, r.admission.Refresh(t.Context()))
	slow.Store(true)

	sent := time.Now()
	pending := chain.send(t, "VIEW_ACK", ack)
	<-entered
	// The kernel publishes the phase record before the ACK; the slow probe
	// reads the observation only after its delay, so it measures this record.
	observeMountACK(t, r, 0, event, ack, 2, 2)
	quick(t, "QUERY behind an in-flight VIEW_ACK observation", func() { r.peers[0].call("ROOT", map[string]any{}) })
	if payload := chain.finish(t, pending); !bytes.Equal(payload, []byte(`{"status":"ACCEPTED"}`)) {
		t.Fatalf("VIEW_ACK: %s", payload)
	}
	if elapsed := time.Since(sent); elapsed < slowObservation {
		t.Fatalf("VIEW_ACK answered after %s, before its %s observation", elapsed, slowObservation)
	}
	if n := probes.Load(); n != 1 {
		t.Fatalf("%d slow observations, want 1 (the ACK's)", n)
	}
}

// pendingLeg is one native command written to the endpoint whose response
// has not been read yet.
type pendingLeg struct {
	original, native guestbridge.Command
	guestRaw         []byte
}

// send writes one control command of the chain (reclaiming the previous
// response, as exec does) without waiting for its response. It is mappedLeg's
// write half; finish is its read half.
func (c *r5Chain) send(t *testing.T, op string, value any) pendingLeg {
	t.Helper()
	r := c.peer
	r.cells++
	*r.mappingNativeCell++
	p := r.installed.Admission.Protocols[r.scope.Session]
	payload, err := canonicalJSON(value)
	require(t, err)
	original := guestbridge.Command{Cell: r.cells, Context: r.scope, Protocol: &p, Op: op, Payload: payload, Reclaim: c.guest}
	guestRaw, err := canonicalJSON(original)
	require(t, err)
	native := original
	native.Cell = *r.mappingNativeCell
	native.Reclaim = c.native
	native.Delegation = &guestbridge.Delegation{Version: guestbridge.DelegationVersion, GuestCell: original.Cell, GuestOp: op, GuestReclaim: c.guest}
	require(t, r.mappedWriteNative(native, mappedFlags(op, c.native, nil)))
	return pendingLeg{original: original, native: native, guestRaw: guestRaw}
}

// finish reads and translates the response of a leg written by send and
// advances the chain's reclaim state to it.
func (c *r5Chain) finish(t *testing.T, leg pendingLeg) json.RawMessage {
	t.Helper()
	raw, err := c.peer.mappedReadNative(leg.native)
	require(t, err)
	payload, _, _, translated, err := c.peer.mappedTranslate(leg.original, leg.guestRaw, raw)
	require(t, err)
	c.guest = &guestbridge.Ack{Cell: leg.original.Cell, ResponseHash: hashBytes(translated)}
	c.native = &guestbridge.Ack{Cell: leg.native.Cell, ResponseHash: hashBytes(raw)}
	c.peer.mappingLastNative = c.native
	return payload
}
