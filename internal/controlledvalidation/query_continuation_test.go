package controlledvalidation

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"

	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

type queryCapture struct {
	allocationViews
	borrowed context.Context
}

func (v *queryCapture) CheckRequest(ctx context.Context, scope guestbridge.Context, operation string, raw json.RawMessage) error {
	if operation == "QUERY" {
		v.borrowed = ctx
	}
	return v.allocationViews.CheckRequest(ctx, scope, operation, raw)
}

// Unit-check the borrowed-token validator with a real authenticated outer QUERY.
// The in-package token below isolates its binding/lifetime checks; it cannot
// establish provider closure or source authority. The original is a GETATTR:
// since task o84 the token is not limited to cleanup opcodes. Actual 32767
// token creation is exercised end to end by the controlled runtime's recall
// workload model (TestZQ3R9PressureReleaseParksGetattr) and, for cleanup, by
// the C/Go dirty-session integration.
func TestQueryContinuationBindingAndExpiry(t *testing.T) {
	h := newAllocationHarness(t)
	capture := &queryCapture{allocationViews: h.config.ViewProtocol.(allocationViews)}
	h.config.ViewProtocol = capture
	h.config.Native = &nativeFacts{native: h.native}
	h.original.Intent = guestbridge.Intent{Opcode: "GETATTR", Args: json.RawMessage(`{}`)}
	intent, encodeErr := canonicalJSON(h.original.Intent)
	mustAllocation(t, encodeErr)
	h.original.Claim.OriginalHash = hash(intent)
	var err error
	h.bridge, err = guestbridge.New(t.Context(), h.config)
	mustAllocation(t, err)
	_, err = h.send("HELLO", h.config.Protocol)
	mustAllocation(t, err)
	_, err = h.send("QUERY", map[string]any{"claim": h.original.Claim})
	mustAllocation(t, err)
	outer, ok := guestbridge.VerifiedRequest(capture.borrowed)
	if !ok || outer.Operation != "QUERY" {
		t.Fatal("missing real outer QUERY")
	}
	p := h.f.provider
	verifier := &facts{enrollment: p.Enrollment()}
	p.config.Facts = verifier
	b := guestbridge.Binding{Context: h.config.Scopes[0], Call: h.original}
	b.Canonical, err = canonicalCall(b.Call)
	mustAllocation(t, err)
	live := new(atomic.Bool)
	live.Store(true)
	owned := queryContinuation{p, outer, b, live}
	ctx := context.WithValue(capture.borrowed, queryContinuationKey{}, owned)
	if err = VerifyQueryContinuation(ctx, h.f.service, verifier, b.Context, "CALL", b.Canonical); err != nil {
		t.Fatal(err)
	}
	wrongScope := b.Context
	wrongScope.Session = id(98).String()
	for _, check := range []struct {
		name    string
		ctx     context.Context
		service *fsservice.Service
		facts   Facts
		scope   guestbridge.Context
		op      string
		raw     json.RawMessage
	}{
		{"public", context.Background(), h.f.service, verifier, b.Context, "CALL", b.Canonical},
		{"unissued", capture.borrowed, h.f.service, verifier, b.Context, "CALL", b.Canonical},
		{"service", ctx, &fsservice.Service{}, verifier, b.Context, "CALL", b.Canonical},
		{"facts", ctx, h.f.service, &facts{enrollment: p.Enrollment()}, b.Context, "CALL", b.Canonical},
		{"scope", ctx, h.f.service, verifier, wrongScope, "CALL", b.Canonical},
		{"operation", ctx, h.f.service, verifier, b.Context, "VIEW_ACK", b.Canonical},
		{"original", ctx, h.f.service, verifier, b.Context, "CALL", json.RawMessage(`{}`)},
	} {
		t.Run(check.name, func(t *testing.T) {
			if VerifyQueryContinuation(check.ctx, check.service, check.facts, check.scope, check.op, check.raw) == nil {
				t.Fatal("unbound continuation accepted")
			}
		})
	}
	changed := owned
	changed.outer.Controller = id(99)
	if VerifyQueryContinuation(context.WithValue(ctx, queryContinuationKey{}, changed), h.f.service, verifier, b.Context, "CALL", b.Canonical) == nil {
		t.Fatal("changed actual outer controller accepted")
	}
	live.Store(false)
	if VerifyQueryContinuation(ctx, h.f.service, verifier, b.Context, "CALL", b.Canonical) == nil {
		t.Fatal("expired invocation accepted")
	}
	// The actual factory must not widen old tuples, even with a private QUERY.
	live.Store(true)
	saved := entry{Binding: b, Original: &b, Status: "RESERVED", PreparationStage: "UNPREPARED"}
	original := p.queryContinuationContext(capture.borrowed, b, saved, live)
	if original.Value(queryContinuationKey{}) != nil {
		t.Fatal("old tuple minted QUERY continuation")
	}
}

// exactUnprepared is the entry half of the mint: only the exact original,
// still RESERVED and never prepared, may borrow the inner CALL check.
func TestQueryContinuationNeedsExactUnpreparedOriginal(t *testing.T) {
	intent := guestbridge.Intent{Opcode: "GETATTR", Args: json.RawMessage(`{}`)}
	b := guestbridge.Binding{Context: guestbridge.Context{Session: id(1).String()}, Call: guestbridge.Call{Intent: intent}}
	var err error
	b.Canonical, err = canonicalCall(b.Call)
	mustAllocation(t, err)
	other := b
	other.Canonical = json.RawMessage(`{}`)
	exact := entry{Binding: b, Original: &b, Status: "RESERVED", PreparationStage: "UNPREPARED"}
	if !exactUnprepared(exact, b) {
		t.Fatal("exact never-prepared original refused")
	}
	for name, change := range map[string]func(*entry){
		"no original":       func(e *entry) { e.Original = nil },
		"other original":    func(e *entry) { e.Original = &other },
		"pending":           func(e *entry) { e.Status = "PENDING" },
		"preparing":         func(e *entry) { e.PreparationStage = "PREPARING" },
		"admission refused": func(e *entry) { e.PreparationStage = "ADMISSION_REFUSED" },
		"other binding":     func(e *entry) { e.Binding = other },
	} {
		changed := exact
		change(&changed)
		if exactUnprepared(changed, b) {
			t.Errorf("%s: minted a QUERY continuation", name)
		}
	}
}
