package guestbridge

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

type callerObservationKey struct{}

type observingViews struct {
	expected Native
	*recordingViewProvider
	seen   []CommandObservation
	reject bool
}

func (p *observingViews) CheckRequest(ctx context.Context, c Context, op string, raw json.RawMessage) error {
	observed, ok := VerifiedRequest(ctx)
	if !ok {
		return fsapi.Permission
	}
	p.seen = append(p.seen, observed)
	// Returned receipt data is detached from the private context value.
	if len(observed.Native.Groups) > 0 {
		observed.Native.Groups[0] ^= 1
	}
	if observed.Protocol != nil {
		observed.Protocol.Features = 0
	}
	again, ok := VerifiedRequest(ctx)
	if !ok || again.PayloadHash != digest(raw) || again.Scope != c || again.Operation != op || !equalWire(again.Native, p.expected) || again.Protocol == nil || *again.Protocol != ViewProtocol() {
		return fsapi.Permission
	}
	if p.reject {
		return fsapi.Permission
	}
	return p.recordingViewProvider.CheckRequest(ctx, c, op, raw)
}

func TestVerifiedRequestComesOnlyFromActualAuthenticatedExecute(t *testing.T) {
	if _, ok := VerifiedRequest(context.Background()); ok {
		t.Fatal("unverified context had observation")
	}
	forged := context.WithValue(context.Background(), callerObservationKey{}, CommandObservation{Operation: "VIEW_ACK"})
	if _, ok := VerifiedRequest(forged); ok {
		t.Fatal("caller-created receipt became verified")
	}
	x := newViewDispatchFixture(t)
	provider := &observingViews{recordingViewProvider: x.provider, expected: x.bridge.native}
	x.bridge.s.config.ViewProtocol = provider
	command := x.acquire(t, ViewAccess{Kind: "DOMAIN"}, "READ", nil)
	x.reply(t, command, "PENDING", nil, nil)
	raw, err := x.bridge.s.Execute(t.Context(), x.bridge.native, command)
	if err != nil || len(raw) == 0 {
		t.Fatalf("actual Execute %v", err)
	}
	if len(provider.seen) != 1 {
		t.Fatal("missing actual callback observation")
	}
	observed := provider.seen[0]
	canonical, err := marshal(command)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Controller != x.bridge.s.controller || observed.Scope != command.Context || observed.Cell != command.Cell || observed.CommandHash != digest(canonical) || observed.PayloadHash != digest(command.Payload) {
		t.Fatal("wrong command/native binding")
	}
	// A changed command gets a distinct exact binding, never the previous receipt.
	changed := command
	value, err := Canonical(command.Payload)
	if err != nil {
		t.Fatal(err)
	}
	payload := value.(map[string]any)
	payload["request"].(map[string]any)["number"] = json.Number("2")
	changed.Payload = mustJSON(t, payload)
	x.reply(t, changed, "PENDING", nil, nil)
	if _, err = x.bridge.s.Execute(t.Context(), x.bridge.native, changed); err != nil {
		t.Fatal(err)
	}
	if len(provider.seen) != 2 || provider.seen[1].CommandHash == observed.CommandHash || provider.seen[1].PayloadHash == observed.PayloadHash {
		t.Fatal("changed binding reused receipt")
	}
	before := len(provider.seen)
	wrong := x.bridge.native
	wrong.UID++
	if _, err = x.bridge.s.Execute(t.Context(), wrong, command); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("unauthenticated callback %v", err)
	}
	wrongScope := x.command(t, "HELLO", ViewProtocol())
	wrongScope.Context.GuestBoot = uid(222).String()
	if _, err = x.bridge.s.Execute(t.Context(), x.bridge.native, wrongScope); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("changed scope %v", err)
	}
	if len(provider.seen) != before {
		t.Fatal("refused native/scope minted observation")
	}
	provider.reject = true
	if _, err = x.bridge.s.Execute(t.Context(), x.bridge.native, command); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("mandatory fact refusal ignored %v", err)
	}
}
