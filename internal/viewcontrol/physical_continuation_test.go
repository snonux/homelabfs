package viewcontrol

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

type continuationCheckingFacts struct {
	Facts
	t        *testing.T
	service  *fsservice.Service
	seen     int
	borrowed context.Context
	scope    guestbridge.Context
	payload  json.RawMessage
}

func (f *continuationCheckingFacts) CheckPhysical(ctx context.Context, scope guestbridge.Context, op string, raw, ledger json.RawMessage) error {
	if op != "VIEW_RELEASE" {
		return f.Facts.CheckPhysical(ctx, scope, op, raw, ledger)
	}
	if err := VerifyPhysicalContinuation(ctx, f.service, f, scope, op, raw); err != nil {
		return err
	}
	f.seen++
	f.borrowed, f.scope, f.payload = ctx, scope, append(json.RawMessage(nil), raw...)
	wrongScope := scope
	wrongScope.Session = uid(99).String()
	for _, check := range []struct {
		ctx     context.Context
		service *fsservice.Service
		facts   Facts
		scope   guestbridge.Context
		op      string
		raw     json.RawMessage
	}{
		{context.Background(), f.service, f, scope, op, raw},
		{ctx, &fsservice.Service{}, f, scope, op, raw},
		{ctx, f.service, f.Facts, scope, op, raw},
		{ctx, f.service, f, wrongScope, op, raw},
		{ctx, f.service, f, scope, "VIEW_ACK", raw},
		{ctx, f.service, f, scope, op, json.RawMessage(`{}`)},
	} {
		if VerifyPhysicalContinuation(check.ctx, check.service, check.facts, check.scope, check.op, check.raw) == nil {
			f.t.Fatal("unbound continuation accepted")
		}
	}
	owned := ctx.Value(physicalContinuationKey{}).(physicalContinuation)
	owned.outer.Controller = uid(98)
	forged := context.WithValue(ctx, physicalContinuationKey{}, owned)
	if VerifyPhysicalContinuation(forged, f.service, f, scope, op, raw) == nil {
		f.t.Fatal("changed outer controller accepted")
	}
	return f.Facts.CheckPhysical(ctx, scope, op, raw, ledger)
}

func TestPhysicalReleaseQueryContinuation(t *testing.T) {
	r := newRig(t)
	facts := &continuationCheckingFacts{Facts: r.facts, t: t, service: r.service}
	r.controller.config.Facts = facts
	_, description := r.create(0, "continued-release", false)
	_, read := r.acquire(0, page(description, 0), "READ")
	ticket := r.controlTicket(0)
	payload := map[string]any{"request": ticket, "lease": read.Lease}
	if pending := r.view(0, "VIEW_RELEASE", payload); pending.Status != "PENDING" {
		t.Fatal(pending.Status)
	}
	r.completeCleanRecall()
	r.physical("VIEW_RELEASE", payload)
	wrong := ticket
	wrong.Number++
	if _, err := r.execute(0, "VIEW_QUERY", map[string]any{"request": wrong}); err == nil {
		t.Fatal("unprotected query accepted")
	}
	wrong = ticket
	wrong.Credentials.Groups = []uint32{999}
	if _, err := r.execute(0, "VIEW_QUERY", map[string]any{"request": wrong}); err == nil {
		t.Fatal("changed issuer accepted")
	}
	if facts.seen != 0 {
		t.Fatal("unrelated query gained continuation")
	}
	if released := r.view(0, "VIEW_QUERY", map[string]any{"request": ticket}); released.Status != "RELEASED" {
		t.Fatal(released.Status)
	}
	if facts.seen != 1 {
		t.Fatalf("physical continuation calls %d", facts.seen)
	}
	if VerifyPhysicalContinuation(facts.borrowed, r.service, facts, facts.scope, "VIEW_RELEASE", facts.payload) == nil {
		t.Fatal("borrowed context survived locked call")
	}
	if err := facts.CheckPhysical(t.Context(), r.scopes[0], "VIEW_RELEASE", facts.payload, []byte(`{}`)); err == nil {
		t.Fatal("direct physical assertion gained continuation")
	}
}
