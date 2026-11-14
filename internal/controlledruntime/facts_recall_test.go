package controlledruntime

import (
	"encoding/json"
	"testing"
)

// TestFactsRecallParticipants: an all-domain recall needs an event for every
// registered domain, a scoped READ recall (task 784) for exactly its domain.
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
