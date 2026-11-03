package viewcontrol

import (
	"bytes"
	"encoding/json"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"testing"
)

func TestTransportLifetimeCannotDiscardDirtyDescriptionOrExitPins(t *testing.T) {
	for _, op := range []string{"DESCRIPTION_RELEASE", "PROCESS_EXIT"} {
		t.Run(op, func(t *testing.T) {
			r := newRig(t)
			_, description := r.create(0, "held-lifetime", false)
			r.useTransport()
			_, dirty := r.acquire(0, page(description, 0), "EXCLUSIVE_DIRTY")
			_, payload := r.reserve(0, dirty.Lease, description)
			reserved := r.view(0, "DIRTY_RESERVE", payload)
			if reserved.Reservation == nil {
				t.Fatal("dirty reservation absent")
			}
			before, err := r.controller.capture(t.Context())
			must(t, err)
			args := map[string]any{"description": description, "release_sequence": 1}
			if op == "PROCESS_EXIT" {
				args = map[string]any{"process": r.credentials[0].Process, "exit_sequence": 1}
			}
			original := r.originalCall(0, op, args)
			_, err = r.execute(0, "CALL", original)
			must(t, err)
			response, err := r.execute(0, "QUERY", map[string]any{"claim": original.Claim})
			must(t, err)
			if bytes.Contains(response, []byte(`"status":"TERMINAL"`)) {
				t.Fatal("dirty lifetime completed before recall")
			}
			if len(r.controller.state.Reservations) != 1 || r.controller.state.Recall == nil {
				t.Fatal("lifetime dropped dirty reservation/recall")
			}
			after, err := r.controller.capture(t.Context())
			must(t, err)
			if !sameSource(before, after) {
				t.Fatal("pending lifetime dropped source pins")
			}
			r.drainSingle(*reserved.Reservation, "61")
			// Exact original resumes only after source/dirty closure.
			response, err = r.execute(0, "QUERY", map[string]any{"claim": original.Claim})
			must(t, err)
			var query struct {
				Status string
				Result struct{ Code string }
			}
			must(t, json.Unmarshal(response, &query))
			if query.Status != "TERMINAL" || query.Result.Code != "OK" {
				t.Fatalf("lifetime after dirty closure: %s", response)
			}
			if op == "PROCESS_EXIT" {
				released := r.call(0, "DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": 1})
				if released.Code != "OK" {
					t.Fatalf("original issuer cleanup after exit/drain: %+v", released)
				}
			}
		})
	}
}

func TestTransportForgetsExactTransientDirectoryGrants(t *testing.T) {
	r := newRig(t)
	object, _ := r.create(0, "scan-lifetime", false)
	var root guestbridge.Capability
	for _, cap := range r.facts.caps {
		if cap.Kind == "ROOT" && cap.Scope == r.scopes[0] {
			root = cap
			break
		}
	}
	if root.Token == "" {
		t.Fatal("missing issued root")
	}
	r.useTransport()
	for i := 0; i < 3; i++ {
		original := r.originalCall(0, "READDIR", map[string]any{"object": root, "after": "", "revision": 0, "limit": 1})
		_, err := r.execute(0, "CALL", original)
		must(t, err)
		raw, err := r.execute(0, "QUERY", map[string]any{"claim": original.Claim})
		must(t, err)
		var query struct {
			Status string
			Result struct {
				Code    string
				Entries []struct{ Capability guestbridge.Capability }
			}
		}
		must(t, json.Unmarshal(raw, &query))
		if query.Status != "TERMINAL" || query.Result.Code != "OK" || len(query.Result.Entries) != 1 {
			t.Fatalf("directory result: %s", raw)
		}
		cap := query.Result.Entries[0].Capability
		r.facts.mu.Lock()
		r.facts.caps[cap.Token] = cap
		r.facts.mu.Unlock()
		closed := r.call(0, "FORGET", map[string]any{"object": cap, "count": 1})
		if closed.Code != "OK" {
			t.Fatalf("transient forget: %+v", closed)
		}
		duplicate := r.call(0, "FORGET", map[string]any{"object": cap, "count": 1})
		if duplicate.Code != "Permission" {
			t.Fatalf("reused forgotten authority: %+v", duplicate)
		}
		_, err = r.execute(0, "CALL", original)
		must(t, err)
		replay, err := r.execute(0, "QUERY", map[string]any{"claim": original.Claim})
		must(t, err)
		if !bytes.Equal(raw, replay) {
			t.Fatal("transport replay mutated original directory capabilities")
		}
	}
	// A transient FORGET never consumes this separately issued lookup pin.
	forgotten := r.call(0, "FORGET", map[string]any{"object": object, "count": 1})
	if forgotten.Code != "OK" {
		t.Fatalf("transient scan stole lookup pin: %+v", forgotten)
	}
}
