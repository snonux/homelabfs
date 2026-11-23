package controlledvalidation

import (
	"bytes"
	"context"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

// abandoningAdmission runs a whole ABANDON of its claim inside Admit, that
// is after execute's first loadExact released p.execution and before
// executeAdmitted takes it again, then grants admission without a permit.
type abandoningAdmission struct {
	p       *Provider
	scope   guestbridge.Context
	request abandonRequest
	out     []abandonOutcome
	err     error
}

func (a *abandoningAdmission) Admit(context.Context, guestbridge.Binding) (Admitted, error) {
	a.out, a.err = a.p.abandon(a.scope, a.request)
	return nil, nil
}

// TestAbandonBetweenAdmissionAndPreparation: an ABANDON that lands after the
// CALL's claim was loaded and admitted, but before executeAdmitted's locked
// reload, declines the claim; the reload finds the gap, no preparer runs
// and the file is never created.
func TestAbandonBetweenAdmissionAndPreparation(t *testing.T) {
	r := newAbandonRig(t)
	call := r.allocated(2, "72616365")
	admission := &abandoningAdmission{p: r.f.provider, scope: r.config.Scopes[0], request: abandonRequestOf(call)}
	r.f.provider.admission = admission
	raw, err := r.send("CALL", call)
	r.f.provider.admission = nil
	mustAllocation(t, err)
	if allocationPayload(t, raw)["status"] != "ADMITTED" {
		t.Fatal(string(raw))
	}
	mustAllocation(t, admission.err)
	if len(admission.out) != 1 || admission.out[0].Status != "ABANDONED" {
		t.Fatalf("racing ABANDON %+v", admission.out)
	}
	e, _ := r.stored(call)
	if !e.Gap || !e.Abandoned || e.PreparationStage != "UNPREPARED" || e.Plan != nil || e.Outcome != nil {
		t.Fatalf("claim prepared after the abandon: %+v", e)
	}
	if hash(rawClaim(t, r.f.provider, call.Claim)) != *admission.out[0].ProofHash {
		t.Fatal("the gap changed after the abandon")
	}
	lookup := command(r.f, fsapi.Lookup)
	lookup.Parent = r.f.config.Storage.Root
	lookup.Name = "race"
	request := fsapi.NewRequest(fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}})
	mustAllocation(t, r.f.service.Dispatch(t.Context(), request, lookup))
	if got, err := request.Completion().Wait(t.Context()); err != nil || got.Code != fsapi.NotFound {
		t.Fatalf("racing CREATE has an effect: %v %v", got.Code, err)
	}
}

// requireUnchanged runs an ABANDON of calls and checks it answers want by
// number while the claims, the credit and the counters stay exactly as they
// were.
func (r *abandonRig) requireUnchanged(want map[uint64]string, calls ...guestbridge.Call) map[uint64]string {
	r.t.Helper()
	before := make([][]byte, len(calls))
	for i, call := range calls {
		before[i] = rawClaim(r.t, r.f.provider, call.Claim)
	}
	retained, source := credit(r.t, r.f.provider)
	status, proof, err := r.abandon(calls...)
	mustAllocation(r.t, err)
	for n, s := range want {
		if status[n] != s {
			r.t.Fatalf("number %d %s, want %s", n, status[n], s)
		}
	}
	for i, call := range calls {
		if !bytes.Equal(before[i], rawClaim(r.t, r.f.provider, call.Claim)) {
			r.t.Fatalf("claim %d rewritten", call.Claim.Number)
		}
	}
	if a, b := credit(r.t, r.f.provider); a != retained || b != source {
		r.t.Fatal("credit changed")
	}
	evidence, err := r.f.provider.LedgerEvidence(r.t.Context())
	mustAllocation(r.t, err)
	if evidence.Claims.Abandoned != 0 || evidence.Claims.AbandonedCredit != 0 {
		r.t.Fatal("counted as abandoned")
	}
	return proof
}

// putEarmark stores e at claim's key, as EarmarkNumber or a bind would.
func (r *abandonRig) putEarmark(claim guestbridge.Claim, e Earmark) {
	r.t.Helper()
	mustAllocation(r.t, r.f.provider.db.Update(func(tx *bolt.Tx) error {
		return put(r.f.provider.bucket(tx, "earmarks"), claimKey(claim), e)
	}))
}

// TestAbandonKeepsEarmarkedAndRefusedClaims: a stranded claim bound to an
// open dirty earmark, a number holding only an unbound earmark, and a claim
// whose definite admission refusal is protected as TERMINAL are all KEPT,
// with nothing written.
func TestAbandonKeepsEarmarkedAndRefusedClaims(t *testing.T) {
	r := newAbandonRig(t)
	bound := r.stranded(2, "626f756e64")
	r.putEarmark(bound.Claim, Earmark{Scope: r.config.Scopes[0], Bound: &bound})
	earmarked := r.create(3, "6561726d61726b")
	r.putEarmark(earmarked.Claim, Earmark{Scope: r.config.Scopes[0]})
	r.requireUnchanged(map[uint64]string{2: "KEPT", 3: "KEPT"}, bound, earmarked)

	r2 := newAbandonRig(t)
	refused := r2.allocated(2, "72656675736564")
	r2.f.provider.admission = &fixedAdmission{err: &AdmissionRefusal{Code: fsapi.NoSpace, Reason: "below the source floor"}}
	_, err := r2.send("CALL", refused)
	r2.f.provider.admission = nil
	mustAllocation(t, err)
	if e, _ := r2.stored(refused); e.Status != "TERMINAL" || e.PreparationStage != "ADMISSION_REFUSED" {
		t.Fatalf("definite refusal left %s/%s", e.Status, e.PreparationStage)
	}
	r2.requireUnchanged(map[uint64]string{2: "KEPT"}, refused)
}

// TestAbandonExistingGapAndRetired: a number GAP_RESOLVE already declined is
// answered ABANDONED with that same proof and is not rewritten, recounted or
// released again (its credit returns at recycle); retired numbers answer
// RETIRED_ID.
func TestAbandonExistingGapAndRetired(t *testing.T) {
	r := newAbandonRig(t)
	gap := r.allocated(2, "676170")
	raw, err := r.send("GAP_RESOLVE", gap)
	mustAllocation(t, err)
	proof := allocationPayload(t, raw)["proof_hash"].(string)
	if got := r.requireUnchanged(map[uint64]string{2: "ABANDONED"}, gap); got[2] != proof {
		t.Fatalf("existing gap proof %s, want %s", got[2], proof)
	}
	c := r.original.Claim
	retire := map[string]any{"realm": c.Realm, "subject": c.Subject, "anchor": c.Anchor, "nonce": c.Nonce, "through": uint64(2), "result_hashes": []string{r.rootID, proof}, "credentials": r.original.Credentials}
	raw, err = r.send("RETIRE", retire)
	mustAllocation(t, err)
	if allocationPayload(t, raw)["status"] != "RETIRED" {
		t.Fatalf("retire %s", raw)
	}
	r.requireUnchanged(map[uint64]string{1: "RETIRED_ID", 2: "RETIRED_ID"}, r.original, gap)
}
