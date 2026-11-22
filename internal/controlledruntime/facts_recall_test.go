package controlledruntime

import (
	"encoding/json"
	"testing"

	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
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

// TestFactsVerifyFencedEntries (task 0a4 review round 1): the Facts check
// every fence instead of trusting it. A fence needs a registered domain,
// the archived STOPPED of the recall it names, the domain's fence still at
// the recorded value and no event of the current recall.
func TestFactsVerifyFencedEntries(t *testing.T) {
	var scope guestbridge.Context
	scope.Session = "b"
	stopped := viewcontrol.ArchiveKey(scope, "r1", "STOPPED", 3)
	for _, c := range []struct {
		name    string
		mutate  func(*factsLedger)
		want    bool
		archive map[string]string
	}{
		{"valid", func(*factsLedger) {}, true, map[string]string{stopped: "h"}},
		{"no archived STOPPED", func(*factsLedger) {}, false, map[string]string{}},
		{"STOPPED of another recall", func(*factsLedger) {}, false, map[string]string{viewcontrol.ArchiveKey(scope, "r0", "STOPPED", 3): "h"}},
		{"fence moved", func(l *factsLedger) { l.Fences["b"] = 2 }, false, map[string]string{stopped: "h"}},
		{"still issued an event", func(l *factsLedger) { l.Recall.Events["b"] = json.RawMessage(`{}`) }, false, map[string]string{stopped: "h"}},
		{"unregistered", func(l *factsLedger) { delete(l.Domains, "b") }, false, map[string]string{stopped: "h"}},
	} {
		raw := `{"Domains":{"a":{},"b":{}},"Fences":{},"Fenced":{"b":{"Recall":"r1","Phase":"INSTALL_DRAIN","Sequence":4,"Fence":1}},"Recall":{"ID":"r2","Events":{"a":{}}}}`
		var l factsLedger
		if err := json.Unmarshal([]byte(raw), &l); err != nil {
			t.Fatal(err)
		}
		d := l.Domains["b"]
		d.Domain.Scope = scope
		l.Domains["b"] = d
		l.AckArchive = c.archive
		c.mutate(&l)
		if got := l.validFenced(nil, retirementAuthority{}); got != c.want {
			t.Errorf("%s: got %v", c.name, got)
		}
	}
}
