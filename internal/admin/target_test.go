package admin

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
)

func TestExpectedTargetRefusesReplacementAfterStatus(t *testing.T) {
	c := testConfig(t)
	c.EnableTestHooks = true
	c.Status = func(context.Context) (Status, error) {
		return Status{NodeID: c.NodeID, Volumes: []VolumeStatus{{VolumeID: c.VolumeID, Generation: "old-generation"}}}, nil
	}
	original := testServer(t, c)
	observed, err := Call(t.Context(), c.SocketPath, Request{Version: 1, Operation: "status"})
	if err != nil {
		t.Fatal(err)
	}
	if err := original.Close(); err != nil {
		t.Fatal(err)
	}
	var controls atomic.Int32
	events := make(chan Event, 4)
	c.Generation = "new-generation"
	c.ToggleFault = func(context.Context, string, bool) error { controls.Add(1); return nil }
	c.Audit = func(_ context.Context, event Event) error { events <- event; return nil }
	testServer(t, c)
	request := Request{Version: 1, Operation: "test-fault", Fault: "journal-before-sync", Enabled: true, ExpectedNode: observed.Status.NodeID, ExpectedVolume: observed.Status.Volumes[0].VolumeID, ExpectedGeneration: observed.Status.Volumes[0].Generation}
	if _, err := Call(t.Context(), c.SocketPath, request); err == nil {
		t.Fatal("stale status authorized replacement control")
	}
	denied := <-events
	if controls.Load() != 0 || denied.Outcome != "denied" || denied.Generation != "new-generation" || denied.ExpectedGeneration != "old-generation" || denied.ExpectedNode != c.NodeID || denied.ExpectedVolume != c.VolumeID {
		t.Fatalf("target mismatch was not denied/audited: %+v", denied)
	}
	request.ExpectedGeneration = "new-generation"
	if _, err := Call(t.Context(), c.SocketPath, request); err != nil {
		t.Fatal(err)
	}
	if controls.Load() != 1 {
		t.Fatal("matching target refused")
	}
	if (<-events).Outcome != "requested" || (<-events).Outcome != "applied" {
		t.Fatal("matching control not audited")
	}
	request.ExpectedNode = ""
	request.ExpectedVolume = ""
	request.ExpectedGeneration = ""
	if _, err := Call(t.Context(), c.SocketPath, request); err != nil {
		t.Fatal("legacy control refused", err)
	}
}
func TestExpectedTargetWireIsAllOrNoneStrictAndBounded(t *testing.T) {
	base := `{"version":1,"operation":"test-fault","fault":"x","enabled":false,`
	for _, suffix := range []string{
		`"expected_node":"node"}`,
		`"expected_node":"node","expected_volume":"volume"}`,
		`"expected_node":null,"expected_volume":"volume","expected_generation":"g"}`,
		`"expected_node":"","expected_volume":"volume","expected_generation":"g"}`,
		`"Expected_node":"node","expected_volume":"volume","expected_generation":"g"}`,
		`"expected_node":"node","expected_node":"other","expected_volume":"volume","expected_generation":"g"}`,
		`"expected_node":"node","expected_volume":"volume","expected_generation":"` + strings.Repeat("x", 65) + `"}`,
		`"expected_node":"node\n","expected_volume":"volume","expected_generation":"g"}`,
	} {
		if _, err := decodeRequest([]byte(base + suffix)); err == nil {
			t.Fatalf("invalid expected target accepted: %s", suffix)
		}
	}
	good := Request{Version: 1, Operation: "test-fault", Fault: "x", Enabled: false, ExpectedNode: "node", ExpectedVolume: "volume", ExpectedGeneration: "g"}
	raw, err := json.Marshal(good)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeRequest(raw)
	if err != nil || decoded != good {
		t.Fatalf("roundtrip: %+v %v", decoded, err)
	}
}
