package controlledvalidation

import (
	"encoding/json"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func TestRestoredBridgeKeepsExactLargeCounters(t *testing.T) {
	h := newBridgeHarness(t)
	_, created := h.create()
	requireCode(t, created, "OK")
	_, closed := h.call("DESCRIPTION_CLOSE", map[string]any{"description": created.Capabilities[1], "process": h.process, "close_sequence": uint64(1) << 53})
	requireCode(t, closed, "OK")
	scope := h.f.pc.Authorization.Enrollment.Scope
	raw, err := h.f.provider.Restore(t.Context(), scope)
	if err != nil {
		t.Fatal(err)
	}
	var snapshot map[string]json.RawMessage
	if err = json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatal(err)
	}
	var controller fsapi.UUID
	if err = json.Unmarshal(snapshot["Controller"], &controller); err != nil {
		t.Fatal(err)
	}
	delete(snapshot, "Controller")
	delete(snapshot, "CapabilityLifetimes")
	snapshot["Reserved"] = json.RawMessage(`0`)
	snapshot["Hello"] = json.RawMessage(`true`)
	complete, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.f.provider.CheckRestoredBridge(t.Context(), scope, controller, complete); err != nil {
		t.Fatalf("actual protected large counter rejected: %v", err)
	}
	var counters map[string]uint64
	if err = json.Unmarshal(snapshot["CloseSequences"], &counters); err != nil {
		t.Fatal(err)
	}
	if len(counters) != 1 {
		t.Fatalf("actual close counter missing %s", raw)
	}
	for key, value := range counters {
		if value != uint64(1)<<53 {
			t.Fatal("wrong original large counter")
		}
		counters[key] = value + 1
	}
	snapshot["CloseSequences"], err = json.Marshal(counters)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.f.provider.CheckRestoredBridge(t.Context(), scope, controller, changed); err == nil {
		t.Fatal("adjacent uint64 values above float64 precision compared equal")
	}
}
