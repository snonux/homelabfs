package controlledvalidation

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

// abandonRig is the controlled allocation harness with an admitted ROOT
// claim (number 1), whose root capability the CREATE claims use.
type abandonRig struct {
	*allocationHarness
	t    *testing.T
	root guestbridge.Capability
	// rootID is the ROOT claim's terminal result id, for RETIRE coverage.
	rootID string
}

func newAbandonRig(t *testing.T) *abandonRig {
	t.Helper()
	h := newAllocationHarness(t)
	_, err := h.send("ALLOCATE", h.original)
	mustAllocation(t, err)
	_, err = h.send("CALL", h.original)
	mustAllocation(t, err)
	stored, _ := h.stored(h.original)
	var result wireResult
	mustAllocation(t, json.Unmarshal(stored.Result, &result))
	r := &abandonRig{allocationHarness: h, t: t, rootID: result.ID}
	for _, cap := range result.Capabilities {
		if cap.Kind == "ROOT" {
			r.root = cap
		}
	}
	if r.root.Token == "" {
		t.Fatal("missing actual root capability")
	}
	return r
}

// create builds the exact CREATE original for number, named name.
func (r *abandonRig) create(number uint64, name string) guestbridge.Call {
	r.t.Helper()
	call := r.original
	call.Claim.Number = number
	flags := map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "truncate": false, "sync": "ORDINARY"}
	call.Intent = guestbridge.Intent{Opcode: "CREATE", Args: mustAllocationJSON(r.t, map[string]any{"parent": r.root, "name": name, "mode": uint64(0660), "umask": uint64(0), "flags": flags})}
	raw, err := canonicalJSON(call.Intent)
	mustAllocation(r.t, err)
	call.Claim.OriginalHash = hash(raw)
	return call
}

// allocated allocates a CREATE original without sending its CALL.
func (r *abandonRig) allocated(number uint64, name string) guestbridge.Call {
	r.t.Helper()
	call := r.create(number, name)
	_, err := r.send("ALLOCATE", call)
	mustAllocation(r.t, err)
	return call
}

// stranded allocates and CALLs a CREATE original while admission refuses
// uncertainly, leaving it RESERVED/UNPREPARED with its credit charged (the
// zq3-r8 shape a84 describes).
func (r *abandonRig) stranded(number uint64, name string) guestbridge.Call {
	r.t.Helper()
	call := r.allocated(number, name)
	r.f.provider.admission = &fixedAdmission{err: fsapi.WouldBlock}
	defer func() { r.f.provider.admission = nil }()
	raw, err := r.send("CALL", call)
	mustAllocation(r.t, err)
	if allocationPayload(r.t, raw)["status"] != "ADMITTED" {
		r.t.Fatal(string(raw))
	}
	if e, _ := r.stored(call); !neverPrepared(e) || e.RetainedCredit == 0 {
		r.t.Fatalf("claim %d not stranded: %s/%s", number, e.Status, e.PreparationStage)
	}
	return call
}

// abandon sends one ABANDON of calls and returns status and proof by number.
func (r *abandonRig) abandon(calls ...guestbridge.Call) (map[uint64]string, map[uint64]string, error) {
	r.t.Helper()
	raw, err := r.send("ABANDON", abandonPayload(calls...))
	if err != nil {
		return nil, nil, err
	}
	var reply struct{ Claims []abandonOutcome }
	mustAllocation(r.t, json.Unmarshal(mustAllocationJSON(r.t, allocationPayload(r.t, raw)), &reply))
	status, proof := map[uint64]string{}, map[uint64]string{}
	for _, o := range reply.Claims {
		status[o.Number] = o.Status
		if o.ProofHash != nil {
			proof[o.Number] = *o.ProofHash
		}
	}
	return status, proof, nil
}

func abandonPayload(calls ...guestbridge.Call) map[string]any {
	c := calls[0].Claim
	claims := []map[string]any{}
	for _, call := range calls {
		claims = append(claims, map[string]any{"number": call.Claim.Number, "original_hash": call.Claim.OriginalHash})
	}
	return map[string]any{"realm": c.Realm, "subject": c.Subject, "anchor": c.Anchor, "nonce": c.Nonce, "claims": claims}
}

func abandonRequestOf(calls ...guestbridge.Call) abandonRequest {
	var request abandonRequest
	raw, _ := json.Marshal(abandonPayload(calls...))
	_ = json.Unmarshal(raw, &request)
	return request
}

// credit reads the provider's retained and source credit in use.
func credit(t *testing.T, p *Provider) (uint64, uint64) {
	t.Helper()
	var retained, source uint64
	mustAllocation(t, p.db.View(func(tx *bolt.Tx) error {
		root := tx.Bucket([]byte(ledgerVersion))
		if raw := root.Get([]byte("retained-credit")); len(raw) == 8 {
			retained = binary.BigEndian.Uint64(raw)
		}
		if raw := root.Get([]byte("source-credit")); len(raw) == 8 {
			source = binary.BigEndian.Uint64(raw)
		}
		return nil
	}))
	return retained, source
}

func rawClaim(t *testing.T, p *Provider, claim guestbridge.Claim) []byte {
	t.Helper()
	var raw []byte
	mustAllocation(t, p.db.View(func(tx *bolt.Tx) error {
		raw = bytes.Clone(p.bucket(tx, "claims").Get(claimKey(claim)))
		return nil
	}))
	return raw
}

// TestAbandonReturnsCreditOfNeverPreparedClaims: a stranded RESERVED claim
// and an allocated, never CALLed number are declined as gaps and their
// credit returns in the ABANDON's transaction; the terminal ROOT is KEPT and
// an unallocated number UNKNOWN. OBSERVE counts the release, the proofs
// retire the numbers, and the later recycle returns nothing twice.
func TestAbandonReturnsCreditOfNeverPreparedClaims(t *testing.T) {
	r := newAbandonRig(t)
	reserved := r.stranded(2, "6f6e65")
	allocated := r.allocated(3, "74776f")
	unknown := r.create(4, "7468726565")
	beforeRetained, beforeSource := credit(t, r.f.provider)
	status, proof, err := r.abandon(r.original, reserved, allocated, unknown)
	mustAllocation(t, err)
	want := map[uint64]string{1: "KEPT", 2: "ABANDONED", 3: "ABANDONED", 4: "UNKNOWN"}
	for n, s := range want {
		if status[n] != s {
			t.Fatalf("number %d %s, want %s (%v)", n, status[n], s, status)
		}
	}
	for _, call := range []guestbridge.Call{reserved, allocated} {
		e, _ := r.stored(call)
		if !e.Gap || e.Status != "DECLINED" || !e.Abandoned || e.RetainedCredit != 0 || e.SourceCredit != 0 {
			t.Fatalf("claim %d %+v", call.Claim.Number, e)
		}
		if hash(rawClaim(t, r.f.provider, call.Claim)) != proof[call.Claim.Number] {
			t.Fatalf("claim %d proof is not the entry hash", call.Claim.Number)
		}
	}
	perClaim := r.f.provider.config.ClaimCreditBytes
	perSource := r.f.provider.config.SourceCreditBytes
	retained, source := credit(t, r.f.provider)
	if retained != beforeRetained-2*perClaim || source != beforeSource-2*perSource {
		t.Fatalf("credit %d/%d, want %d/%d", retained, source, beforeRetained-2*perClaim, beforeSource-2*perSource)
	}
	evidence, err := r.f.provider.LedgerEvidence(t.Context())
	mustAllocation(t, err)
	if evidence.Claims.Abandoned != 2 || evidence.Claims.AbandonedCredit != 2*(perClaim+perSource) {
		t.Fatalf("OBSERVE counters %d/%d", evidence.Claims.Abandoned, evidence.Claims.AbandonedCredit)
	}
	c := r.original.Claim
	retire := map[string]any{"realm": c.Realm, "subject": c.Subject, "anchor": c.Anchor, "nonce": c.Nonce, "through": uint64(3), "result_hashes": []string{r.rootID, proof[2], proof[3]}, "credentials": r.original.Credentials}
	raw, err := r.send("RETIRE", retire)
	mustAllocation(t, err)
	if allocationPayload(t, raw)["status"] != "RETIRED" {
		t.Fatalf("abandon proofs did not retire: %s", raw)
	}
	// The recycle returns exactly the ROOT's credit when it recycled the
	// ROOT (it may stay as the session's snapshot owner) and nothing for the
	// abandoned numbers: any double release, even one, breaks the equality.
	wantRetained, wantSource := retained, source
	if rawClaim(t, r.f.provider, r.original.Claim) == nil {
		wantRetained, wantSource = retained-perClaim, source-perSource
	}
	if a, b := credit(t, r.f.provider); a != wantRetained || b != wantSource {
		t.Fatalf("after recycle credit %d/%d, want %d/%d", a, b, wantRetained, wantSource)
	}
}

// TestAbandonedClaimRefusesLaterCallQueryAndGap: after the abandon, a
// delayed QUERY (whose resume would prepare the claim), a replayed CALL and
// a replayed ALLOCATE with its CALL are refused or answer without effect: the gap entry
// is byte-identical and the file was never created. GAP_RESOLVE answers the
// same proof the ABANDON returned.
func TestAbandonedClaimRefusesLaterCallQueryAndGap(t *testing.T) {
	r := newAbandonRig(t)
	reserved := r.stranded(2, "6c617465")
	allocated := r.allocated(3, "6c61746572")
	_, proof, err := r.abandon(reserved, allocated)
	mustAllocation(t, err)
	gap2, gap3 := rawClaim(t, r.f.provider, reserved.Claim), rawClaim(t, r.f.provider, allocated.Claim)
	// Each later request must answer (an unexpected error fails the test)
	// with exactly the no-effect reply below.
	expect := func(op string, payload any, key, want string) {
		t.Helper()
		raw, err := r.send(op, payload)
		mustAllocation(t, err)
		if got := allocationPayload(t, raw)[key]; got != want {
			t.Fatalf("%s %s=%v, want %s", op, key, got, want)
		}
	}
	// The delayed QUERY sees the gap as INDETERMINATE (1.2) and never resumes.
	expect("QUERY", map[string]any{"claim": reserved.Claim}, "status", "INDETERMINATE")
	// The replayed CALL repeats its earlier ownership acknowledgement only.
	expect("CALL", reserved, "status", "ADMITTED")
	// A replayed ALLOCATE answers its protected acknowledgement again (as
	// after GAP_RESOLVE) and writes nothing; the CALL behind it is refused.
	expect("ALLOCATE", allocated, "status", "ALLOCATED")
	expect("CALL", allocated, "status", "REJECTED")
	if !bytes.Equal(gap2, rawClaim(t, r.f.provider, reserved.Claim)) || !bytes.Equal(gap3, rawClaim(t, r.f.provider, allocated.Claim)) {
		t.Fatal("a later request rewrote an abandoned gap")
	}
	raw, err := r.send("GAP_RESOLVE", reserved)
	mustAllocation(t, err)
	if allocationPayload(t, raw)["proof_hash"] != proof[2] {
		t.Fatal("GAP_RESOLVE changed the abandon proof")
	}
	lookup := command(r.f, fsapi.Lookup)
	lookup.Parent = r.f.config.Storage.Root
	lookup.Name = "late"
	request := fsapi.NewRequest(fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200}})
	mustAllocation(t, r.f.service.Dispatch(t.Context(), request, lookup))
	if got, err := request.Completion().Wait(t.Context()); err != nil || got.Code != fsapi.NotFound {
		t.Fatalf("abandoned CREATE has an effect: %v %v", got.Code, err)
	}
}

// TestAbandonKeepsPreparedClaims: a claim interrupted after PREPARING (the
// preparer may have run) or with a protected PENDING plan is KEPT: its
// entry, the credit and the counters stay exactly as they were.
func TestAbandonKeepsPreparedClaims(t *testing.T) {
	for _, failpoint := range []string{"prepared-before-plan-protection", "plan-protected"} {
		t.Run(failpoint, func(t *testing.T) {
			r := newAbandonRig(t)
			call := r.allocated(2, "70726570")
			r.f.provider.config.Failpoint = func(name string) error {
				if name == failpoint {
					return errors.New("intentional abandon fixture interruption")
				}
				return nil
			}
			_, err := r.send("CALL", call)
			mustAllocation(t, err)
			r.f.provider.config.Failpoint = nil
			before := rawClaim(t, r.f.provider, call.Claim)
			if e, _ := r.stored(call); e.PreparationStage == "UNPREPARED" || e.Status == "TERMINAL" {
				t.Fatalf("failpoint did not interrupt: %s/%s", e.Status, e.PreparationStage)
			}
			retained, source := credit(t, r.f.provider)
			status, proof, err := r.abandon(call)
			mustAllocation(t, err)
			if status[2] != "KEPT" || proof[2] != "" {
				t.Fatalf("prepared claim %s %q", status[2], proof[2])
			}
			if !bytes.Equal(before, rawClaim(t, r.f.provider, call.Claim)) {
				t.Fatal("ABANDON rewrote a prepared claim")
			}
			if a, b := credit(t, r.f.provider); a != retained || b != source {
				t.Fatal("ABANDON released a prepared claim's credit")
			}
			evidence, err := r.f.provider.LedgerEvidence(t.Context())
			mustAllocation(t, err)
			if evidence.Claims.Abandoned != 0 || evidence.Claims.AbandonedCredit != 0 {
				t.Fatal("KEPT claim counted as abandoned")
			}
		})
	}
}

// TestAbandonIsBoundToSession: another session's scope cannot abandon (the
// bridge and the provider refuse it); a claim whose protected original
// names another session, or a wrong original hash, refuses the whole
// command and writes nothing, even for the session's own listed claims.
func TestAbandonIsBoundToSession(t *testing.T) {
	r := newAbandonRig(t)
	own := r.stranded(2, "6f776e")
	foreign := r.stranded(3, "666f726569676e")
	other := r.config.Scopes[0]
	other.Session = id(99).String()
	c := r.command("ABANDON", abandonPayload(own))
	c.Context = other
	if _, err := r.bridge.Execute(t.Context(), r.native, c); err == nil {
		t.Fatal("bridge admitted another session's ABANDON")
	}
	if _, err := r.f.provider.AbandonOriginal(t.Context(), other, mustAllocationJSON(t, abandonPayload(own))); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("provider admitted another session's ABANDON: %v", err)
	}
	// Re-home the foreign claim's protected binding to another session.
	mustAllocation(t, r.f.provider.db.Update(func(tx *bolt.Tx) error {
		e, _, err := r.f.provider.load(tx, foreign.Claim)
		if err != nil {
			return err
		}
		e.Original.Context.Session = other.Session
		e.Binding.Context.Session = other.Session
		return r.f.provider.putClaim(tx, claimKey(e.Claim), e)
	}))
	ownBefore, foreignBefore := rawClaim(t, r.f.provider, own.Claim), rawClaim(t, r.f.provider, foreign.Claim)
	retained, source := credit(t, r.f.provider)
	if _, _, err := r.abandon(own, foreign); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("abandoned another session's claim: %v", err)
	}
	wrong := own
	wrong.Claim.OriginalHash = hash([]byte("other intent"))
	if _, _, err := r.abandon(wrong); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("abandoned a claim by a wrong original hash: %v", err)
	}
	if !bytes.Equal(ownBefore, rawClaim(t, r.f.provider, own.Claim)) || !bytes.Equal(foreignBefore, rawClaim(t, r.f.provider, foreign.Claim)) {
		t.Fatal("a refused ABANDON wrote a claim")
	}
	if a, b := credit(t, r.f.provider); a != retained || b != source {
		t.Fatal("a refused ABANDON released credit")
	}
}

// TestAbandonCrashReplay: a crash inside the transaction (the failpoint
// aborts it before commit) leaves the claim and the credit untouched; after
// a reopen the abandon commits, and its replay, also after another reopen,
// answers the same proof, releases nothing twice and keeps the counters.
func TestAbandonCrashReplay(t *testing.T) {
	r := newAbandonRig(t)
	call := r.stranded(2, "6372617368")
	before := rawClaim(t, r.f.provider, call.Claim)
	retained, source := credit(t, r.f.provider)
	r.f.provider.config.Failpoint = func(name string) error {
		if name == abandonFailpoint {
			return errors.New("intentional crash before the abandon commits")
		}
		return nil
	}
	if _, _, err := r.abandon(call); err == nil {
		t.Fatal("interrupted ABANDON answered")
	}
	if !bytes.Equal(before, rawClaim(t, r.f.provider, call.Claim)) {
		t.Fatal("an aborted ABANDON left a partial write")
	}
	if a, b := credit(t, r.f.provider); a != retained || b != source {
		t.Fatal("an aborted ABANDON released credit")
	}
	reopen := func() {
		mustAllocation(t, r.f.provider.Close())
		r.f.provider = nil
		r.f.open()
	}
	reopen()
	scope := r.config.Scopes[0]
	first, err := r.f.provider.abandon(scope, abandonRequestOf(call))
	mustAllocation(t, err)
	released, releasedSource := credit(t, r.f.provider)
	reopen()
	replay, err := r.f.provider.abandon(scope, abandonRequestOf(call))
	mustAllocation(t, err)
	if first[0].Status != "ABANDONED" || replay[0].Status != "ABANDONED" || *first[0].ProofHash != *replay[0].ProofHash {
		t.Fatalf("replay changed the answer: %+v %+v", first[0], replay[0])
	}
	if a, b := credit(t, r.f.provider); a != released || b != releasedSource || released != retained-r.f.provider.config.ClaimCreditBytes {
		t.Fatalf("replay released credit twice: %d/%d", a, b)
	}
	evidence, err := r.f.provider.LedgerEvidence(t.Context())
	mustAllocation(t, err)
	if evidence.Claims.Abandoned != 1 {
		t.Fatalf("replay counted again: %d", evidence.Claims.Abandoned)
	}
}
