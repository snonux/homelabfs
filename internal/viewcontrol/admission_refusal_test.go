package viewcontrol

import (
	"bytes"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
)

// TestBelowFloorSourcesRefusalIsTerminal replays zq3-r7 R7-2 end to end:
// Sources 2 (which CheckSourceFloor now refuses at New, so it is forced into
// the live controller here) refuses mount(8)'s PROCESS_EXIT. The refusal is
// definite, so the claim is TERMINAL NoSpace instead of RESERVED/UNPREPARED,
// its QUERY replays the same bytes, and the session keeps serving reads.
func TestBelowFloorSourcesRefusalIsTerminal(t *testing.T) {
	r := newZQ3Rig(t, 0)
	r.forceSources(2)
	call := r.originalCall(1, "PROCESS_EXIT", map[string]any{"process": r.credentials[1].Process, "exit_sequence": 1})
	if exited := r.sendCall(1, call); exited.Code != "NoSpace" || exited.Count != 0 {
		t.Fatalf("PROCESS_EXIT %+v, want a terminal NoSpace", exited)
	}
	first := r.query(1, call)
	if again := r.query(1, call); !bytes.Equal(first, again) {
		t.Fatalf("QUERY replay changed:\n%s\n%s", first, again)
	}
	if root := r.call(1, "ROOT", map[string]any{}); root.Code != "OK" {
		t.Fatalf("session wedged after the refusal: %+v", root)
	}
}

// TestCapacityRefusalClassification separates the structural capacity
// refusal (no ledger state can admit it) from contention.
func TestCapacityRefusalClassification(t *testing.T) {
	r := newZQ3Rig(t, 0)
	var refusal *controlledvalidation.AdmissionRefusal
	for _, c := range []struct {
		sources  int
		drain    bool
		definite bool
	}{
		{admittedSources + sourceReserve - 1, false, true},
		{admittedSources + sourceReserve - 1, true, false},
		{admittedSources + sourceReserve, false, false},
		{MinSources(2), false, false},
	} {
		r.forceSources(c.sources)
		err := r.controller.capacityRefusal(c.drain)
		if !errors.Is(err, fsapi.NoSpace) || errors.As(err, &refusal) != c.definite {
			t.Errorf("Sources %d drain %v: %v", c.sources, c.drain, err)
		}
	}
}

// forceSources changes the live enrollment's Sources bound in the controller
// and in the rig's enrollment facts (which must still match it), bypassing
// the floor New enforces.
func (r *rig) forceSources(sources int) {
	r.controller.config.Authorization.Enrollment.Bounds.Sources = sources
	r.facts.mu.Lock()
	r.facts.enrollment.Bounds.Sources = sources
	r.facts.mu.Unlock()
}
