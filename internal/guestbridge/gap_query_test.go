package guestbridge

import (
	"encoding/json"
	"testing"
)

func TestProtectedGapQueryEncoding(t *testing.T) {
	for _, test := range []struct {
		name        string
		mappedQuery bool
		state       ClaimState
		allowed     bool
	}{
		{"exact", true, ClaimState{Status: "DECLINED"}, true},
		{"old-or-cancel", false, ClaimState{Status: "DECLINED"}, false},
		{"result", true, ClaimState{Status: "DECLINED", Result: json.RawMessage(`{}`)}, false},
		{"unknown-status", true, ClaimState{Status: "ABSENT"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			value, err := queryPayload(Claim{}, test.state, test.mappedQuery)
			if (err == nil) != test.allowed {
				t.Fatalf("encoding %v/%v", value, err)
			}
			if err == nil {
				object := value.(map[string]any)
				if object["status"] != "DECLINED" || object["result"] != nil {
					t.Fatal(value)
				}
			}
		})
	}
}
