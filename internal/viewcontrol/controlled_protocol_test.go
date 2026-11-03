package viewcontrol

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

func TestControlledProtocolUsesSignedDomainPin(t *testing.T) {
	r := newRig(t)
	scope := r.scopes[0]
	// A real controller with legacy signed domains cannot admit the successor.
	if err := r.controller.CheckEnrollment(t.Context(), scope, guestbridge.ControlledProtocol()); !errors.Is(err, fsapi.Permission) {
		t.Fatalf("legacy inventory accepted successor: %v", err)
	}
	// The pin is included in the aggregate signed bytes; adding a valid tuple to
	// an old authorization cannot reopen its actual controller ledger.
	raw, err := json.Marshal(r.cc.Authorization)
	must(t, err)
	var changed Authorization
	must(t, json.Unmarshal(raw, &changed))
	protocol := guestbridge.ControlledProtocol()
	changed.Enrollment.Domains[0].Protocol = &protocol
	config := r.cc
	config.Authorization = changed
	if controller, err := New(t.Context(), config); err == nil {
		_ = controller.Close()
		t.Fatal("changed unsigned per-scope pin accepted")
	}
	// The tuple selector keeps independent scopes exact and never rewrites1.2
	// into the legacy envelope at ticket/recall construction.
	c := Controller{config: config}
	selected, ok := c.scopeProtocol(scope)
	if !ok || selected != protocol {
		t.Fatal("lost exact successor tuple")
	}
	selected, ok = c.scopeProtocol(r.scopes[1])
	if !ok || selected != guestbridge.ViewProtocol() {
		t.Fatal("changed another domain tuple")
	}
	foreign := scope
	foreign.Session = uid(99).String()
	if _, ok = c.scopeProtocol(foreign); ok {
		t.Fatal("foreign domain selected tuple")
	}
}
