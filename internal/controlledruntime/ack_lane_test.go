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
	x := newACKLaneFixture(t)
	// A fresh observation, so the QUERY below needs no probe of its own while
	// the ACK (not recorded in it) must probe.
	require(t, x.r.admission.Refresh(t.Context()))
	x.slow.Store(true)
	sent, pending := x.sendACK(t)
	// call("ROOT") runs ALLOCATE and CALL (ordinary lane) and then QUERY, the
	// only one of the three on the control lane that the ACK used to block.
	quick(t, "QUERY behind an in-flight VIEW_ACK observation", func() { x.r.peers[0].call("ROOT", map[string]any{}) })
	x.finishACK(t, sent, pending)
	if n := x.probes.Load(); n != 1 {
		t.Fatalf("%d slow observations, want 1 (the ACK's)", n)
	}
}

// TestStaleObservationQueryReusesACKProbe is the companion case: the cached
// observation is older than admissionReuse (5 s), so the QUERY's own reuse
// rule needs a new observation. It waits once, on the admission's refresh
// lock, for the probe the ACK already started and reuses it: exactly one
// probe runs, so no second SSH round trip quietly appears. ALLOCATE and CALL
// in call("ROOT") coalesce onto the same probe.
func TestStaleObservationQueryReusesACKProbe(t *testing.T) {
	x := newACKLaneFixture(t)
	require(t, x.r.admission.Refresh(t.Context()))
	a := x.r.admission
	a.mu.Lock()
	// Age the committed observation past admissionReuse but within check's
	// 15 s freshness limit, without sleeping.
	a.refreshed = a.refreshed.Add(-6 * time.Second)
	a.observedAt = a.observedAt.Add(-6 * time.Second)
	a.mu.Unlock()
	x.slow.Store(true)
	sent, pending := x.sendACK(t)
	started := time.Now()
	x.r.peers[0].call("ROOT", map[string]any{})
	// It must have waited for the in-flight probe (else the observation was
	// not stale and this test proves nothing), but only for that one probe.
	waited := time.Since(started)
	if waited < slowObservation/2 {
		t.Fatalf("QUERY took %s: it did not wait for the in-flight probe", waited)
	}
	if waited >= slowObservation+slowObservation/2 {
		t.Fatalf("QUERY waited %s, more than the one in-flight probe", waited)
	}
	x.finishACK(t, sent, pending)
	if n := x.probes.Load(); n != 1 {
		t.Fatalf("%d slow observations, want 1 (the ACK's, reused by the QUERY)", n)
	}
}

// ackLaneFixture is the assembled two-mount runtime with an all-domain recall
// active and mount 0's STOPPED ACK built but not sent. Probes are slow only
// after slow is set.
type ackLaneFixture struct {
	r       *sourceRig
	chain   *r5Chain
	ack     map[string]any
	event   map[string]json.RawMessage
	slow    atomic.Bool
	probes  atomic.Int32
	entered chan struct{}
}

func newACKLaneFixture(t *testing.T) *ackLaneFixture {
	t.Helper()
	r, c := assemblyFixture(t)
	x := &ackLaneFixture{r: r, entered: make(chan struct{}, 4)}
	slowProbe(r.admission, &x.slow, &x.probes, x.entered)
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
	x.chain = &r5Chain{peer: r.peers[0]}
	x.ack = recallACK(t, x.chain, domain, "STOPPED")
	x.event = x.ack["event"].(map[string]json.RawMessage)
	delete(x.ack, "event")
	return x
}

// sendACK writes the ACK, waits until its slow probe has started and then
// publishes the kernel record: the kernel publishes it before the ACK, and
// the slow probe reads the observation only after its delay, so it measures
// this record.
func (x *ackLaneFixture) sendACK(t *testing.T) (time.Time, pendingLeg) {
	t.Helper()
	sent := time.Now()
	pending := x.chain.send(t, "VIEW_ACK", x.ack)
	<-x.entered
	observeMountACK(t, x.r, 0, x.event, x.ack, 2, 2)
	return sent, pending
}

// finishACK requires ACCEPTED, and no earlier than the ACK's probe could end.
func (x *ackLaneFixture) finishACK(t *testing.T, sent time.Time, pending pendingLeg) {
	t.Helper()
	if payload := x.chain.finish(t, pending); !bytes.Equal(payload, []byte(`{"status":"ACCEPTED"}`)) {
		t.Fatalf("VIEW_ACK: %s", payload)
	}
	if elapsed := time.Since(sent); elapsed < slowObservation {
		t.Fatalf("VIEW_ACK answered after %s, before its %s observation", elapsed, slowObservation)
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
