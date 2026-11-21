package controlledvalidation

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

// steppedClock is a settable refusal-trace clock.
type steppedClock struct{ at time.Time }

func (c *steppedClock) now() time.Time { return c.at }

// TestUncertainRefusalIsTraced replays the zq3-r8 shape (a84): an uncertain
// WouldBlock from admission leaves the claim RESERVED/UNPREPARED behind
// ADMITTED/OK, and the claim now records why. The same class is written at
// most once a second, a new class restarts the trace, OBSERVE's claim summary
// shows it, a restart keeps it, and the claim still resumes and keeps the
// trace once admission passes.
func TestUncertainRefusalIsTraced(t *testing.T) {
	r := newRefusalRig(t)
	clock := &steppedClock{at: time.Unix(1_800_000_000, 0)}
	r.f.provider.now = clock.now
	admission := &fixedAdmission{err: fsapi.WouldBlock}
	r.f.provider.admission = admission
	b := r.create("traced")
	r.requireAdmitted(b)
	start := clock.at.Unix()
	requireTrace(t, r.entry(b), refusalTrace{Class: "ADMISSION:WouldBlock", Count: 1, First: start, Last: start})

	r.query(b) // the same second: rate-limited, nothing written
	requireTrace(t, r.entry(b), refusalTrace{Class: "ADMISSION:WouldBlock", Count: 1, First: start, Last: start})
	clock.at = clock.at.Add(2 * time.Second)
	r.query(b)
	requireTrace(t, r.entry(b), refusalTrace{Class: "ADMISSION:WouldBlock", Count: 2, First: start, Last: start + 2})
	if n := admission.calls.Load(); n != 3 {
		t.Fatalf("admission ran %d times, want one per CALL/QUERY", n)
	}

	admission.err = fsapi.NoSpace
	r.query(b)
	requireTrace(t, r.entry(b), refusalTrace{Class: "ADMISSION:NoSpace", Count: 1, First: start + 2, Last: start + 2})
	requireSummaryTrace(t, r.f.provider, b.Call.Claim.Number, "ADMISSION:NoSpace", start+2)

	r.f.provider.admission = nil
	if status := r.query(b); status != "TERMINAL" {
		t.Fatalf("resumed QUERY %s", status)
	}
	// The rig's bridge keeps the old provider, so after the restart only the
	// ledger is read: the trace was protected with the entry.
	r.restart()
	if e := r.entry(b); e.Status != "TERMINAL" || e.Refusal == nil || e.Refusal.Class != "ADMISSION:NoSpace" {
		t.Fatalf("resumed claim %s trace %+v", e.Status, e.Refusal)
	}
}

// query sends one QUERY of b and returns its status.
func (r *refusalRig) query(b guestbridge.Binding) string {
	r.t.Helper()
	var reply struct{ Status string }
	r.decode(r.request("QUERY", map[string]any{"claim": b.Call.Claim}), &reply)
	return reply.Status
}

func requireTrace(t *testing.T, e entry, want refusalTrace) {
	t.Helper()
	if e.Status != "RESERVED" || e.PreparationStage != "UNPREPARED" || e.Refusal == nil || *e.Refusal != want {
		t.Fatalf("claim %s/%s trace %+v, want %+v", e.Status, e.PreparationStage, e.Refusal, want)
	}
}

// requireSummaryTrace checks OBSERVE's claim summary of number carries the
// trace, and that a claim without one (the rig's ROOT, number 1) encodes no
// refusal key at all (omitempty keeps the b84 shape).
func requireSummaryTrace(t *testing.T, p *Provider, number uint64, class string, last int64) {
	t.Helper()
	evidence, err := p.LedgerEvidence(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	c := evidence.Claims
	if len(c.Pending) != 1 || c.Pending[0].Number != number || c.Pending[0].Refusal != class || c.Pending[0].Refusals != 1 || c.Pending[0].RefusedLast != last {
		t.Fatalf("pending %+v", c.Pending)
	}
	for _, s := range c.Recent {
		if s.Number != 1 {
			continue
		}
		raw, err := json.Marshal(s)
		if err != nil || strings.Contains(string(raw), "Refus") {
			t.Fatalf("untraced claim encodes %s %v", raw, err)
		}
		return
	}
	t.Fatalf("ROOT claim missing from recent %+v", c.Recent)
}

// TestRefusalTraceSkipsSettledAndGapClaims: noteRefusal writes nothing to a
// TERMINAL claim or a declined gap, so a gap proof (the hash of the entry)
// cannot change after ResolveGap took it, and a claim the provider is
// working on is never rewritten by a diagnostic.
func TestRefusalTraceSkipsSettledAndGapClaims(t *testing.T) {
	r := newRefusalRig(t)
	r.f.provider.admission = &fixedAdmission{err: fsapi.WouldBlock}
	b := r.create("declined")
	r.requireAdmitted(b)
	proof, err := r.f.provider.ResolveGap(t.Context(), b.Context, b.Call.Claim)
	if err != nil {
		t.Fatal(err)
	}
	before := r.rawEntry(b)
	r.f.provider.now = func() time.Time { return time.Unix(1_900_000_000, 0) }
	r.f.provider.noteRefusal(b, refusalLoad, fsapi.WouldBlock)
	if after := r.rawEntry(b); !bytes.Equal(before, after) || hash(after) != proof {
		t.Fatalf("gap entry changed:\n%s\n%s", before, after)
	}

	r.f.provider.admission = nil
	done := r.create("done")
	r.requireAdmitted(done)
	if e := r.entry(done); e.Status != "TERMINAL" || e.Refusal != nil {
		t.Fatalf("admitted claim %s trace %+v", e.Status, e.Refusal)
	}
	settled := r.rawEntry(done)
	r.f.provider.noteRefusal(done, refusalExecution, fsapi.IO)
	if !bytes.Equal(settled, r.rawEntry(done)) {
		t.Fatal("TERMINAL entry changed")
	}
}

// rawEntry returns b's stored ledger bytes.
func (r *refusalRig) rawEntry(b guestbridge.Binding) []byte {
	r.t.Helper()
	var raw []byte
	if err := r.f.provider.db.View(func(tx *bolt.Tx) error {
		raw = bytes.Clone(r.f.provider.bucket(tx, "claims").Get(claimKey(b.Call.Claim)))
		return nil
	}); err != nil {
		r.t.Fatal(err)
	}
	return raw
}

// TestObserveRefusalRateLimit pins the trace transitions.
func TestObserveRefusalRateLimit(t *testing.T) {
	first, changed := observeRefusal(nil, "LOAD:WouldBlock", 100)
	if !changed || *first != (refusalTrace{Class: "LOAD:WouldBlock", Count: 1, First: 100, Last: 100}) {
		t.Fatalf("first %+v", first)
	}
	if same, changed := observeRefusal(first, "LOAD:WouldBlock", 100); changed || same != first {
		t.Fatal("a refusal within the same second was written")
	}
	next, changed := observeRefusal(first, "LOAD:WouldBlock", 101)
	if !changed || *next != (refusalTrace{Class: "LOAD:WouldBlock", Count: 2, First: 100, Last: 101}) || first.Count != 1 {
		t.Fatalf("next %+v (old %+v)", next, first)
	}
	other, changed := observeRefusal(next, "ADMISSION:WouldBlock", 101)
	if !changed || *other != (refusalTrace{Class: "ADMISSION:WouldBlock", Count: 1, First: 101, Last: 101}) {
		t.Fatalf("new class %+v", other)
	}
	if got := refusalClass(refusalAdmission, fsapi.WouldBlock); got != "ADMISSION:WouldBlock" {
		t.Fatalf("class %q", got)
	}
}

// TestPlanCreditIgnoresRefusalTrace: across the retained-credit boundary a
// traced entry gets exactly the answer of the same entry without a trace,
// however long its class, and the caller's trace is left in place.
func TestPlanCreditIgnoresRefusalTrace(t *testing.T) {
	plan := fsservice.ControlledPlan{Result: fsapi.Result{Code: fsapi.OK}}
	out := guestbridge.Outcome{Publication: json.RawMessage("null")}
	trace := &refusalTrace{Class: strings.Repeat("X", 4096), Count: 9, First: 1, Last: 2}
	fits, short := 0, 0
	for credit := uint64(4096); credit < 8192; credit++ {
		plain := entry{Version: ledgerVersion, Status: "PENDING", RetainedCredit: credit, SourceCredit: 4096}
		traced := plain
		traced.Refusal = trace
		want, got := checkPlanCredit(plain, plan, out), checkPlanCredit(traced, plan, out)
		if want != got {
			t.Fatalf("credit %d: untraced %v, traced %v", credit, want, got)
		}
		if want == nil {
			fits++
		} else {
			short++
		}
		if traced.Refusal != trace {
			t.Fatal("checkPlanCredit cleared the caller's trace")
		}
	}
	if fits == 0 || short == 0 {
		t.Fatalf("the range missed the boundary: %d fit, %d short", fits, short)
	}
}
