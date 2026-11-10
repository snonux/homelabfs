package viewcontrol

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// zq3SpecBounds reads the REAL Views bounds of the committed zq3 run spec, so
// these tests break when the lab spec drifts below what admission needs
// (zq3-r7 R7-2: Sources 2 refused every non-read op with NoSpace).
func zq3SpecBounds(t *testing.T) Bounds {
	t.Helper()
	raw, err := os.ReadFile("../runtimeprep/testdata/zq3-spec.json")
	must(t, err)
	var spec struct{ Views Bounds }
	must(t, json.Unmarshal(raw, &spec))
	return spec.Views
}

// newZQ3Rig is the two-domain local rig enrolled with the zq3 spec bounds,
// optionally with a different Sources value.
func newZQ3Rig(t *testing.T, sources int) *rig {
	b := zq3SpecBounds(t)
	if sources > 0 {
		b.Sources = sources
	}
	return newRig(t, func(c *Config) { c.Authorization.Enrollment.Bounds = b })
}

// exit sends the domain's PROCESS_EXIT (mount(8) exiting after the mount) and
// requires a TERMINAL OK result; it is that process's last call.
func (r *rig) exit(i int) {
	r.t.Helper()
	exited := r.call(i, "PROCESS_EXIT", map[string]any{"process": r.credentials[i].Process, "exit_sequence": 1})
	if exited.Code != "OK" {
		r.t.Fatalf("PROCESS_EXIT %d: %+v", i, exited)
	}
}

// TestZQ3SpecBoundsAdmitBootstrapNonReads replays the zq3-r7 bootstrap: both
// mounts (the rig's ROOT), mount(8)'s PROCESS_EXIT and a CREATE must each
// reach TERMINAL with the spec's bounds instead of parking UNPREPARED.
func TestZQ3SpecBoundsAdmitBootstrapNonReads(t *testing.T) {
	r := newZQ3Rig(t, 0)
	r.exit(1)
	r.create(0, "zq3-m0", false)
	r.exit(0)
}

// dirtyPair gives each domain a CREATEd file, one EXCLUSIVE_DIRTY write lease
// on it and one granted dirty reservation: the steady state a two-domain
// recall has to drain. Both CREATEs come first, because any non-read op
// admitted while a write lease is live starts a recall instead.
func (r *rig) dirtyPair() [2]guestbridge.DirtyReservation {
	r.t.Helper()
	var descriptions [2]guestbridge.Capability
	for i := range 2 {
		_, descriptions[i] = r.create(i, "zq3-dirty-"+string(rune('0'+i)), false)
	}
	var reservations [2]guestbridge.DirtyReservation
	for i, description := range descriptions {
		_, lease := r.acquire(i, page(description, 0), "EXCLUSIVE_DIRTY")
		if lease.Status != "GRANTED" {
			r.t.Fatalf("write lease %d: %+v", i, lease)
		}
		_, payload := r.reserve(i, lease.Lease, description)
		reserved := r.view(i, "DIRTY_RESERVE", payload)
		if reserved.Status != "GRANTED" || reserved.Reservation == nil {
			r.t.Fatalf("dirty reservation %d: %+v", i, reserved)
		}
		reservations[i] = *reserved.Reservation
	}
	return reservations
}

// parkCreate issues a CREATE while both write leases are live. Admission
// starts the two-domain recall and the claim stays non-terminal until REOPEN.
// root is fetched beforehand: even a read conflicts with a dirty lease.
func (r *rig) parkCreate(root guestbridge.Capability, name string) guestbridge.Call {
	r.t.Helper()
	original := r.originalCall(0, "CREATE", map[string]any{"parent": root, "name": hex.EncodeToString([]byte(name)), "mode": 0660, "umask": 0, "flags": map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}})
	_, err := r.execute(0, "CALL", original)
	must(r.t, err)
	if bytes.Contains(r.query(0, original), []byte(`"status":"TERMINAL"`)) || r.controller.state.Recall == nil {
		r.t.Fatal("CREATE planned through live write leases without a recall")
	}
	return original
}

func (r *rig) query(i int, call guestbridge.Call) json.RawMessage {
	r.t.Helper()
	raw, err := r.execute(i, "QUERY", map[string]any{"claim": call.Claim})
	must(r.t, err)
	return raw
}

// drainPair freezes, drains and reopens both domains' reservations.
func (r *rig) drainPair(reservations [2]guestbridge.DirtyReservation) {
	r.t.Helper()
	recall := r.controller.state.Recall.ID
	var manifests []json.RawMessage
	var calls []guestbridge.Call
	for i, reservation := range reservations {
		payload := freezeRequest(r.controlTicket(i), reservation, recall, "61")
		frozenReply := r.view(i, "DIRTY_FREEZE", payload)
		if frozenReply.Status != "FROZEN" {
			r.t.Fatalf("freeze %d: %+v", i, frozenReply)
		}
		manifest, _ := canonical(map[string]any{"entries": []json.RawMessage{frozenReply.Entry}, "hash": digest([]json.RawMessage{frozenReply.Entry})})
		manifests = append(manifests, manifest)
		raw, _ := canonical(payload)
		var req request
		must(r.t, json.Unmarshal(raw, &req))
		_, call, err := frozen(req)
		must(r.t, err)
		calls = append(calls, call)
	}
	for i := range reservations {
		r.ack(i, "STOPPED", manifests[i])
	}
	for i, call := range calls {
		if drained := r.sendCall(i, call); drained.Code != "OK" {
			r.t.Fatalf("drain %d refused: %+v", i, drained)
		}
	}
	r.completeRecall(manifests)
}

// TestZQ3SpecBoundsAdmitAfterTwoDomainRecall drives the spec bounds from READY
// through one write lease and dirty reservation per domain, the recall a
// non-read CREATE starts, both drains and REOPEN; the parked CREATE, then a
// PROCESS_EXIT, must reach TERMINAL OK afterwards.
func TestZQ3SpecBoundsAdmitAfterTwoDomainRecall(t *testing.T) {
	r := newZQ3Rig(t, 0)
	r.requireTerminal(r.parkCreateAfterDirtyPair())
	r.exit(1)
	r.exit(0)
}

// TestZQ3SourceFloorIsExact runs the MinSources sequence at exactly
// MinSources(2): the parked CREATE's admission then charges every source
// cut of the floor, so one Sources fewer would have refused it with NoSpace.
func TestZQ3SourceFloorIsExact(t *testing.T) {
	floor := MinSources(2)
	r := newZQ3Rig(t, floor)
	parked := r.parkCreateAfterDirtyPair()
	if charged := len(r.controller.state.Sources) + admittedSources + sourceReserve; charged != floor {
		t.Fatalf("post-recall admission charges %d source cuts, floor %d", charged, floor)
	}
	r.requireTerminal(parked)
}

func (r *rig) requireTerminal(call guestbridge.Call) {
	r.t.Helper()
	terminal := r.query(0, call)
	if !bytes.Contains(terminal, []byte(`"status":"TERMINAL"`)) || !bytes.Contains(terminal, []byte(`"code":"OK"`)) {
		r.t.Fatalf("parked CREATE after the two-domain recall: %s", terminal)
	}
}

// TestSourceFloorRefusesEnrollment checks that New names a below-floor
// Sources bound before any signature or ledger work, and that validBounds
// applies the same floor per domain count.
func TestSourceFloorRefusesEnrollment(t *testing.T) {
	if MinSources(1) != 6 || MinSources(2) != 8 {
		t.Fatalf("MinSources(1)=%d MinSources(2)=%d, want 6 and 8", MinSources(1), MinSources(2))
	}
	r := newZQ3Rig(t, 0)
	must(t, r.controller.Close())
	for _, sources := range []int{2, MinSources(2) - 1, MaxSources + 1} {
		c := r.cc
		c.Authorization.Enrollment.Bounds.Sources = sources
		if _, err := New(t.Context(), c); !errors.Is(err, fsapi.Invalid) || !strings.Contains(err.Error(), "Bounds.Sources") {
			t.Fatalf("Sources %d: %v", sources, err)
		}
	}
	b := zq3SpecBounds(t)
	if !validBounds(b, 2) {
		t.Fatal("zq3 spec bounds refused")
	}
	b.Sources = MinSources(3) - 1
	if !validBounds(b, 2) || validBounds(b, 3) {
		t.Fatal("source floor does not scale with the domain count")
	}
}

func (r *rig) parkCreateAfterDirtyPair() guestbridge.Call {
	r.t.Helper()
	root := r.call(0, "ROOT", map[string]any{}).Capabilities[0]
	reservations := r.dirtyPair()
	parked := r.parkCreate(root, "zq3-after-recall")
	r.drainPair(reservations)
	return parked
}
