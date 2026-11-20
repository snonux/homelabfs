//go:build linux || freebsd

package controlledruntime

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// TestControllerEvidenceRecallHistoryRoundTrip: the ce4 recall history is an
// additive optional field of evidence /2. A record with it round-trips
// exactly, and a /2 record written before ce4 (no "recalls") still decodes,
// with Recalls nil, and re-encodes to the same bytes.
func TestControllerEvidenceRecallHistoryRoundTrip(t *testing.T) {
	history := &viewcontrol.RecallHistory{Capacity: viewcontrol.RecallHistoryLimit, Total: 3, Omitted: 1, Recalls: []viewcontrol.RecallRecord{{
		ID: "r", Kind: viewcontrol.TriggerRelease, Opcode: "VIEW_RELEASE", Domain: "s0", Scope: "DOMAIN", Participants: []string{"s0"},
		StartedUTC: "2026-11-20T01:02:03.000004Z", EndMicros: 9000, Outcome: viewcontrol.RecallCompleted,
		Phases: []viewcontrol.RecallPhase{{Phase: "RECALL", IssueMicros: 40}, {Phase: "DRAIN", OffsetMicros: 700}},
		Acks:   []viewcontrol.RecallAck{{Domain: "s0", Phase: "STOPPED", Sequence: 1, OffsetMicros: 650, Blocked: 2, BlockedMicros: 600, CheckMicros: 30}},
	}}}
	want := lifecycleEvidence{Version: lifecycleEvidenceVersion, Stage: "SERVING", Controller: controllerEvidence{Error: "closed", Recalls: history}}
	raw, err := json.Marshal(want)
	require(t, err)
	var got lifecycleEvidence
	require(t, json.Unmarshal(raw, &got))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip\n got %+v\nwant %+v", got.Controller, want.Controller)
	}
	legacy := lifecycleEvidence{Version: lifecycleEvidenceVersion, Stage: "SERVING", Controller: controllerEvidence{Error: "closed"}}
	old, err := json.Marshal(legacy)
	require(t, err)
	var decoded lifecycleEvidence
	require(t, json.Unmarshal(old, &decoded))
	again, err := json.Marshal(decoded)
	require(t, err)
	if decoded.Controller.Recalls != nil || string(again) != string(old) {
		t.Fatalf("pre-ce4 evidence changed: %s -> %s", old, again)
	}
}
