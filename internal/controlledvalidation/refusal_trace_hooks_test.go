package controlledvalidation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

// TestLoadRefusalIsTraced covers execute's LOAD hook: a reserved claim whose
// QUERY resume finds another operation's PENDING snapshot in its scope gets
// loadExact's WouldBlock recorded as LOAD:WouldBlock.
func TestLoadRefusalIsTraced(t *testing.T) {
	r := newRefusalRig(t)
	clock := &steppedClock{at: time.Unix(1_800_000_000, 0)}
	r.f.provider.now = clock.now
	r.f.provider.admission = &fixedAdmission{err: fsapi.WouldBlock}
	b := r.create("held")
	r.requireAdmitted(b)
	r.f.provider.admission = nil
	blocker := b
	blocker.Call.Claim.Number = 999
	blocker.Operation = id(77)
	pending := entry{Version: ledgerVersion, Claim: blocker.Call.Claim, Binding: blocker, Status: "PENDING", Plan: &fsservice.ControlledPlan{}, BridgeState: json.RawMessage(`{"snapshot":1}`)}
	mustAllocation(t, r.f.provider.db.Update(func(tx *bolt.Tx) error {
		return r.f.provider.putClaim(tx, claimKey(blocker.Call.Claim), pending)
	}))
	clock.at = clock.at.Add(time.Second)
	if status := r.query(b); status != "INDETERMINATE" {
		t.Fatalf("QUERY behind a PENDING snapshot %s", status)
	}
	at := clock.at.Unix()
	requireTrace(t, r.entry(b), refusalTrace{Class: "LOAD:WouldBlock", Count: 1, First: at, Last: at})
}

// failingPermit is an admission permit whose Check refuses the cut, so the
// serialized worker fails before preparation begins.
type failingPermit struct{}

func (failingPermit) Check(context.Context, *fsservice.ControlledCut) error { return fsapi.Busy }
func (failingPermit) CheckSource(context.Context, *fsservice.ControlledCut, []fsservice.ControlledEffect) error {
	return nil
}
func (failingPermit) Close() {}

type permitAdmission struct{}

func (permitAdmission) Admit(context.Context, guestbridge.Binding) (Admitted, error) {
	return failingPermit{}, nil
}

// TestExecutionRefusalIsTraced covers execute's EXECUTION hook: a permit
// whose Check fails leaves the claim never prepared, and the failure is
// recorded as EXECUTION:Busy.
func TestExecutionRefusalIsTraced(t *testing.T) {
	r := newRefusalRig(t)
	r.f.provider.now = func() time.Time { return time.Unix(1_800_000_000, 0) }
	r.f.provider.admission = permitAdmission{}
	b := r.create("busy")
	r.requireAdmitted(b)
	requireTrace(t, r.entry(b), refusalTrace{Class: "EXECUTION:Busy", Count: 1, First: 1_800_000_000, Last: 1_800_000_000})
}

// TestExecutionRefusalSkipsPreparingClaim: a failure after the claim reached
// PREPARING (the prepared-before-plan-protection failpoint) is not traced;
// that claim's own protected path owns it.
func TestExecutionRefusalSkipsPreparingClaim(t *testing.T) {
	r := newRefusalRig(t)
	failed := errors.New("failpoint")
	r.f.provider.config.Failpoint = func(name string) error {
		if name == "prepared-before-plan-protection" {
			return failed
		}
		return nil
	}
	b := r.create("preparing")
	r.requireAdmitted(b)
	r.f.provider.config.Failpoint = nil
	if e := r.entry(b); e.Status != "RESERVED" || e.PreparationStage != "PREPARING" || e.Refusal != nil {
		t.Fatalf("claim %s/%s trace %+v", e.Status, e.PreparationStage, e.Refusal)
	}
}

// TestReaddirCleanupIgnoresRefusalTrace: the snapshot owner's absolute
// credit check gives a traced owner exactly the answer of the same owner
// without a trace, across the boundary, while the growth check still
// refuses an owner that grew.
func TestReaddirCleanupIgnoresRefusalTrace(t *testing.T) {
	updated := []byte(`{"snapshot":2}`)
	trace := &refusalTrace{Class: strings.Repeat("X", 64), Count: 1 << 40, First: 1, Last: 2}
	fits, short := 0, 0
	for credit := uint64(4096); credit < 8192; credit++ {
		plain := entry{Version: ledgerVersion, Status: "TERMINAL", BridgeState: updated, RetainedCredit: credit}
		traced := plain
		traced.Refusal = trace
		plainRaw, _ := json.Marshal(plain)
		tracedRaw, _ := json.Marshal(traced)
		want, got := readdirOwnerFits(plain, plainRaw, updated), readdirOwnerFits(traced, tracedRaw, updated)
		if want != got {
			t.Fatalf("credit %d: untraced %v, traced %v", credit, want, got)
		}
		if want == nil {
			fits++
		} else {
			short++
		}
	}
	if fits == 0 || short == 0 {
		t.Fatalf("the range missed the boundary: %d fit, %d short", fits, short)
	}
	grown := entry{Version: ledgerVersion, Status: "TERMINAL", BridgeState: updated, RetainedCredit: 1 << 20, Refusal: trace}
	before := grown
	before.Refusal = nil
	beforeRaw, _ := json.Marshal(before)
	if err := readdirOwnerFits(grown, beforeRaw, updated); !errors.Is(err, fsapi.NoSpace) {
		t.Fatalf("an owner that grew by a trace passed: %v", err)
	}
}

// TestClaimEvidenceWorstCaseBound fills the summary lists with open claims at
// the maximum number, oversized strings, maximal traces and the largest l64
// Age (Opened 1, the clock at the maximum Unix second) and the hh4 abandon
// counters at their maximum, and checks one
// provider's encoded ClaimEvidence stays within MaxClaimEvidenceBytes, with
// traces only on the first claimEvidenceTraced Pending summaries and ages on
// Pending only.
func TestClaimEvidenceWorstCaseBound(t *testing.T) {
	f := newFixture(t)
	const maxSecond = 1<<63 - 1
	f.provider.now = func() time.Time { return time.Unix(maxSecond, 0) }
	template := f.newBinding("GETATTR", map[string]any{}, false)
	long := strings.Repeat("Z", 200)
	mustAllocation(t, f.provider.db.Update(func(tx *bolt.Tx) error {
		top := template.Call.Claim
		// Only the crafted worst-case entries are listed: drop the
		// template's own ALLOCATED number.
		if err := f.provider.deleteClaim(tx, claimKey(top)); err != nil {
			return err
		}
		if err := f.provider.putPrincipal(tx, top, principal{High: ^uint64(0), Floor: ^uint64(0)}); err != nil {
			return err
		}
		if err := put(tx.Bucket([]byte(ledgerVersion)), []byte(abandonedCounters), abandonTotals{Claims: ^uint64(0), Credit: ^uint64(0)}); err != nil {
			return err
		}
		for i := uint64(0); i < claimEvidencePending+claimEvidenceRecent; i++ {
			b := template
			b.Call.Claim.Number = ^uint64(0) - i
			b.Call.Intent.Opcode = long
			e := entry{Version: ledgerVersion, Claim: b.Call.Claim, Binding: b, Status: long, PreparationStage: long,
				Refusal: &refusalTrace{Class: long, Count: ^uint64(0), First: -1 << 63, Last: -1 << 63}, Opened: 1}
			if err := f.provider.putClaim(tx, claimKey(b.Call.Claim), e); err != nil {
				return err
			}
		}
		return nil
	}))
	evidence, err := f.provider.LedgerEvidence(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(evidence.Claims)
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > MaxClaimEvidenceBytes {
		t.Fatalf("claim evidence %d bytes > %d", len(raw), MaxClaimEvidenceBytes)
	}
	c := evidence.Claims
	if c.Abandoned != ^uint64(0) || c.AbandonedCredit != ^uint64(0) {
		t.Fatalf("abandon counters %d/%d", c.Abandoned, c.AbandonedCredit)
	}
	if len(c.Pending) != claimEvidencePending || len(c.Recent) != claimEvidenceRecent {
		t.Fatalf("lists %d/%d", len(c.Pending), len(c.Recent))
	}
	for i, s := range c.Pending {
		if (s.Refusal != "") != (i < claimEvidenceTraced) || len(s.Refusal) > claimSummaryText || len(s.Opcode) > claimSummaryText || s.Age != maxSecond-1 {
			t.Fatalf("pending %d %+v", i, s)
		}
	}
	for _, s := range c.Recent {
		if s.Refusal != "" || s.Refusals != 0 || s.Age != 0 {
			t.Fatalf("recent carries a trace or an age %+v", s)
		}
	}
	t.Logf("worst-case claim evidence: %d bytes", len(raw))
}
