package controlledvalidation

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	bolt "go.etcd.io/bbolt"
)

// fixedAdmission refuses every Admit with err and counts the attempts.
type fixedAdmission struct {
	err   error
	calls atomic.Int32
}

func (a *fixedAdmission) Admit(context.Context, guestbridge.Binding) (Admitted, error) {
	a.calls.Add(1)
	return nil, a.err
}

// refusalRig is a real bridge over the fixture's real provider and service.
type refusalRig struct {
	t      *testing.T
	f      *fixture
	bridge *guestbridge.Service
	native guestbridge.Native
	cell   uint64
	root   guestbridge.Capability
}

func newRefusalRig(t *testing.T) *refusalRig {
	t.Helper()
	f := newFixture(t)
	scope := f.pc.Authorization.Enrollment.Scope
	native := guestbridge.Native{UID: 1000, GID: 1001, PID: 123, Unique: 1, Groups: []uint32{200}, Process: id(66).String(), Endpoint: f.pc.Authorization.Enrollment.Endpoint}
	template := f.newBinding("ROOT", map[string]any{}, false)
	config := guestbridge.Config{Validation: f.config.Validation, Endpoint: native.Endpoint, Native: &nativeFacts{native: native}, Attestor: credentialFacts{expected: template.Call.Credentials, scope: scope}, Claims: f.provider, Execution: f.provider, Views: noViews{}, Scopes: []guestbridge.Context{scope}}
	bridge, err := guestbridge.New(t.Context(), config)
	if err != nil {
		t.Fatal(err)
	}
	r := &refusalRig{t: t, f: f, bridge: bridge, native: native}
	r.request("HELLO", map[string]any{"body_schema": 1, "bridge_major": 1, "bridge_minor": 0, "features": 255, "msize": 65536, "normal_major": 3, "normal_minor": 0})
	r.request("CALL", template.Call)
	var root struct {
		Status string
		Result wireResult
	}
	r.decode(r.request("QUERY", map[string]any{"claim": template.Call.Claim}), &root)
	if root.Status != "TERMINAL" || len(root.Result.Capabilities) != 2 {
		t.Fatalf("ROOT %+v", root)
	}
	r.root = root.Result.Capabilities[0]
	return r
}

// request sends one command and returns the response's payload.
func (r *refusalRig) request(op string, payload any) json.RawMessage {
	r.t.Helper()
	r.cell++
	raw, err := json.Marshal(payload)
	if err != nil {
		r.t.Fatal(err)
	}
	response, err := r.bridge.Execute(r.t.Context(), r.native, guestbridge.Command{Cell: r.cell, Context: r.f.pc.Authorization.Enrollment.Scope, Op: op, Payload: raw})
	if err != nil {
		r.t.Fatalf("%s: %v", op, err)
	}
	var envelope struct{ Payload json.RawMessage }
	r.decode(response, &envelope)
	return envelope.Payload
}

func (r *refusalRig) decode(raw []byte, v any) {
	r.t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		r.t.Fatal(err)
	}
}

func (r *refusalRig) create(name string) guestbridge.Binding {
	flags := map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}
	return r.f.newBinding("CREATE", map[string]any{"parent": r.root, "name": hex.EncodeToString([]byte(name)), "mode": 0660, "umask": 0, "flags": flags}, false)
}

// requireAdmitted checks the CALL answer: ownership transfer, never REJECTED
// (the claim is already reserved when Admit runs).
func (r *refusalRig) requireAdmitted(b guestbridge.Binding) {
	r.t.Helper()
	var call struct{ Status, Code string }
	r.decode(r.request("CALL", b.Call), &call)
	if call.Status != "ADMITTED" || call.Code != "OK" {
		r.t.Fatalf("CALL answered %+v", call)
	}
}

// entry reads the provider's ledger entry of b.
func (r *refusalRig) entry(b guestbridge.Binding) entry {
	r.t.Helper()
	var e entry
	if err := r.f.provider.db.View(func(tx *bolt.Tx) error {
		var err error
		e, _, err = r.f.provider.load(tx, b.Call.Claim)
		return err
	}); err != nil {
		r.t.Fatal(err)
	}
	return e
}

// requireNoReserved fails when any ledger claim is still RESERVED.
func (r *refusalRig) requireNoReserved() {
	r.t.Helper()
	if err := r.f.provider.db.View(func(tx *bolt.Tx) error {
		return r.f.provider.bucket(tx, "claims").ForEach(func(_, raw []byte) error {
			var e entry
			if err := json.Unmarshal(raw, &e); err != nil {
				return err
			}
			if e.Status == "RESERVED" {
				r.t.Errorf("claim %d left RESERVED/%s", e.Claim.Number, e.PreparationStage)
			}
			return nil
		})
	}); err != nil {
		r.t.Fatal(err)
	}
}

// TestDefiniteAdmissionRefusalIsTerminal replays zq3-r7 R7-2 with a definite
// refusal: CALL stays ADMITTED/OK, but the claim is TERMINAL with a protected
// NoSpace failed result instead of RESERVED/UNPREPARED, QUERY returns it, and
// a CALL replay, a QUERY replay and a provider restart return the same bytes
// without re-running admission.
func TestDefiniteAdmissionRefusalIsTerminal(t *testing.T) {
	r := newRefusalRig(t)
	admission := &fixedAdmission{err: &AdmissionRefusal{Code: fsapi.NoSpace, Reason: "below the source floor"}}
	r.f.provider.admission = admission
	b := r.create("refused")
	r.requireAdmitted(b)
	e := r.entry(b)
	if e.Status != "TERMINAL" || e.PreparationStage != "ADMISSION_REFUSED" || e.Outcome == nil || e.Outcome.Result.Code != fsapi.NoSpace {
		t.Fatalf("definite refusal left %s/%s", e.Status, e.PreparationStage)
	}
	first := r.request("QUERY", map[string]any{"claim": b.Call.Claim})
	var query struct {
		Status string
		Result wireResult
	}
	r.decode(first, &query)
	if query.Status != "TERMINAL" || query.Result.Code != "NoSpace" || query.Result.Count != 0 || len(query.Result.Capabilities) != 0 || string(query.Result.Publication) != "null" {
		t.Fatalf("QUERY %s", first)
	}
	r.requireAdmitted(b)
	if again := r.request("QUERY", map[string]any{"claim": b.Call.Claim}); !bytes.Equal(again, first) {
		t.Fatalf("replayed QUERY changed:\n%s\n%s", first, again)
	}
	if n := admission.calls.Load(); n != 1 {
		t.Fatalf("admission ran %d times; a terminal refusal must replay, not re-admit", n)
	}
	r.requireNoReserved()
	r.restart()
	state, err := r.f.provider.Resolve(t.Context(), b.Context, b.Call.Claim)
	if err != nil || state.Status != "TERMINAL" || !bytes.Equal(state.Result, e.Result) {
		t.Fatalf("restart lost the refusal: %s %v", state.Status, err)
	}
	if out, ready, err := r.f.provider.RecoverOutcome(t.Context(), state.Binding); err != nil || !ready || out.Result.Code != fsapi.NoSpace {
		t.Fatalf("recovered %+v ready=%v %v", out.Result, ready, err)
	}
	r.requireNoReserved()
}

// restart closes and reopens the provider and service on the same ledger.
func (r *refusalRig) restart() {
	r.t.Helper()
	f := r.f
	if err := f.provider.Close(); err != nil {
		r.t.Fatal(err)
	}
	f.provider = nil
	if err := f.service.Close(); err != nil {
		r.t.Fatal(err)
	}
	f.service = nil
	f.workspace(id(32))
	f.start()
	f.enroll()
	f.open()
}

// TestUncertainAdmissionErrorStaysIndeterminate keeps today's behaviour for
// every Admit error that is not a definite refusal (contention NoSpace from a
// full service queue, I/O): ADMITTED/OK, the claim RESERVED/UNPREPARED and
// QUERY INDETERMINATE, retried by each QUERY until admission passes.
func TestUncertainAdmissionErrorStaysIndeterminate(t *testing.T) {
	for _, refusal := range []error{fsapi.NoSpace, fsapi.IO, fsapi.WouldBlock} {
		t.Run(refusal.Error(), func(t *testing.T) {
			r := newRefusalRig(t)
			admission := &fixedAdmission{err: refusal}
			r.f.provider.admission = admission
			b := r.create("uncertain")
			r.requireAdmitted(b)
			if e := r.entry(b); e.Status != "RESERVED" || e.PreparationStage != "UNPREPARED" || e.Plan != nil {
				t.Fatalf("uncertain refusal became %s/%s", e.Status, e.PreparationStage)
			}
			var query struct{ Status string }
			r.decode(r.request("QUERY", map[string]any{"claim": b.Call.Claim}), &query)
			if query.Status != "INDETERMINATE" || admission.calls.Load() < 2 {
				t.Fatalf("QUERY %s after %d admissions", query.Status, admission.calls.Load())
			}
			// Admission passes again (no controller bound): QUERY resumes it.
			r.f.provider.admission = nil
			var done struct {
				Status string
				Result wireResult
			}
			r.decode(r.request("QUERY", map[string]any{"claim": b.Call.Claim}), &done)
			if done.Status != "TERMINAL" || done.Result.Code != "OK" {
				t.Fatalf("resumed QUERY %+v", done)
			}
		})
	}
}

// TestDefiniteRefusalClassification pins which errors count as definite.
func TestDefiniteRefusalClassification(t *testing.T) {
	for _, c := range []struct {
		err  error
		want bool
	}{
		{&AdmissionRefusal{Code: fsapi.NoSpace}, true},
		{&AdmissionRefusal{Code: fsapi.OK}, false},
		{fsapi.NoSpace, false},
		{fsapi.Permission, false},
		{fsapi.Invalid, false},
	} {
		if _, ok := definiteRefusal(c.err); ok != c.want {
			t.Errorf("%v: definite=%v", c.err, ok)
		}
	}
	if err := error(&AdmissionRefusal{Code: fsapi.NoSpace}); !errors.Is(err, fsapi.NoSpace) {
		t.Error("AdmissionRefusal does not unwrap to its code")
	}
}
