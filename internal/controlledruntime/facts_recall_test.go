package controlledruntime

import (
	"encoding/json"
	"testing"
)

// TestFactsRecallParticipants: an all-domain recall needs an event for every
// registered domain, a scoped READ recall (task 784) for exactly its domain,
// and neither expects one for a session fenced as a stalled participant
// (task 0a4), which must not have one either.
func TestFactsRecallParticipants(t *testing.T) {
	for _, c := range []struct {
		name, ledger string
		want         bool
	}{
		{"all-domain complete", `{"Domains":{"a":{},"b":{}},"Recall":{"Events":{"a":{},"b":{}}}}`, true},
		{"all-domain missing", `{"Domains":{"a":{},"b":{}},"Recall":{"Events":{"a":{}}}}`, false},
		{"scoped exact", `{"Domains":{"a":{},"b":{}},"Recall":{"Domain":"b","Events":{"b":{}}}}`, true},
		{"scoped extra event", `{"Domains":{"a":{},"b":{}},"Recall":{"Domain":"b","Events":{"a":{},"b":{}}}}`, false},
		{"scoped wrong domain", `{"Domains":{"a":{},"b":{}},"Recall":{"Domain":"b","Events":{"a":{}}}}`, false},
		{"scoped unregistered", `{"Domains":{"a":{}},"Recall":{"Domain":"b","Events":{}}}`, false},
		{"all-domain fenced dropped", `{"Domains":{"a":{},"b":{}},"Fenced":{"b":{}},"Recall":{"Events":{"a":{}}}}`, true},
		{"all-domain fenced still issued", `{"Domains":{"a":{},"b":{}},"Fenced":{"b":{}},"Recall":{"Events":{"a":{},"b":{}}}}`, false},
		{"all-domain fenced peer missing", `{"Domains":{"a":{},"b":{}},"Fenced":{"b":{}},"Recall":{"Events":{}}}`, false},
	} {
		var l factsLedger
		if err := json.Unmarshal([]byte(c.ledger), &l); err != nil {
			t.Fatal(err)
		}
		if got := l.recallParticipantsComplete(); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}
