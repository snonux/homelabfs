//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

var _ guestbridge.TerminalCoordinator = (*Producer)(nil)

// RetireSession coordinates only the originally bound live worker and actual
// observer/provider/controller/bridge. Every partial seal remains irreversible.
func (p *Producer) RetireSession(ctx context.Context, a *guestbridge.TerminalAttempt) (guestbridge.TerminalResponse, error) {
	var empty guestbridge.TerminalResponse
	request, scope, err := a.Attempt(ctx)
	if err != nil {
		return empty, err
	}
	if p.terminalFailpoint != nil {
		if err = p.terminalFailpoint(ctx, a, "terminal-begin"); err != nil {
			return empty, err
		}
	}
	if err = p.terminalObserver(ctx, a); err != nil {
		return empty, err
	}
	provider := p.providers[scope.Session]
	if provider == nil {
		return empty, fsapi.Permission
	}
	providerSeal, err := provider.SealTerminal(ctx, a)
	if err != nil {
		return empty, err
	}
	controllerSeal, err := p.controller.SealTerminal(ctx, a)
	if err != nil {
		return empty, err
	}
	bridgeSeal, err := a.SealBridge(ctx, p)
	if err != nil {
		return empty, err
	}
	if err = p.terminalObserver(ctx, a); err != nil {
		return empty, err
	}
	if err = provider.VerifyTerminalSeal(ctx, a, providerSeal); err != nil {
		return empty, err
	}
	if err = p.controller.VerifyTerminalSeal(ctx, a, controllerSeal); err != nil {
		return empty, err
	}
	owner, err := a.ProtectOwnership(ctx, p, provider.Enrollment().ServingGeneration.String(), []guestbridge.TerminalSeal{providerSeal, controllerSeal, bridgeSeal})
	if err != nil {
		return empty, err
	}
	raw, err := canonicalJSON(owner)
	if err != nil {
		return empty, err
	}
	return guestbridge.TerminalResponse{Status: "RETIRED", RetirementID: request.RetirementID, GuestThrough: request.GuestThrough, ObserverRevision: request.ObserverRevision, ClosureHash: request.ClosureHash, SettledHash: owner.SettledHash, OwnershipHash: hashBytes(raw)}, nil
}

func (p *Producer) terminalObserver(ctx context.Context, a *guestbridge.TerminalAttempt) error {
	_, scope, err := a.Attempt(ctx)
	if err != nil {
		return err
	}
	admission := p.facts.admission(scope)
	observed, ok := guestbridge.VerifiedRequest(ctx)
	p.mu.Lock()
	transport := p.transports[scope.Session]
	controller := p.bindings[scope.Session]
	p.mu.Unlock()
	if admission == nil || !ok || controller != observed.Controller || transport == nil {
		return fsapi.Permission
	}
	actual, err := transport.TerminalAttempt(ctx)
	if err != nil {
		return err
	}
	if err = actual.Check(ctx); err != nil {
		return err
	}
	if err = admission.check(scope); err != nil {
		return err
	}
	admission.mu.Lock()
	defer admission.mu.Unlock()
	for _, row := range admission.installed.Admission.Observations {
		if row.Scope == scope {
			if row.Terminal == nil || row.Phase != "TERMINAL_PENDING" && row.Phase != "TERMINAL_RETIRED" {
				return fsapi.Permission
			}
			return a.CheckSnapshot(ctx, *row.Terminal)
		}
	}
	return fsapi.Permission
}

func (p *Producer) terminalEvidence(ctx context.Context, c guestbridge.Command, payload json.RawMessage) (string, json.RawMessage, error) {
	p.mu.Lock()
	transport := p.transports[c.Context.Session]
	p.mu.Unlock()
	if transport == nil || c.Protocol == nil || *c.Protocol != guestbridge.TerminalProtocol() {
		return "", nil, fsapi.Permission
	}
	a, err := transport.TerminalAttempt(ctx)
	if err != nil {
		return "", nil, err
	}
	if err = p.terminalObserver(ctx, a); err != nil {
		return "", nil, err
	}
	// Reuse the same concrete seals and check every actual live owner immediately
	// before signature. ProtectOwnership validates the exact already protected base.
	response, err := p.RetireSession(ctx, a)
	if err != nil {
		return "", nil, err
	}
	expected, err := canonicalJSON(response)
	if err != nil || string(expected) != string(payload) {
		return "", nil, fsapi.Stale
	}
	owner, err := a.Ownership(ctx, p)
	if err != nil {
		return "", nil, err
	}
	if err = a.VerifyOwnership(ctx, p, owner); err != nil {
		return "", nil, err
	}
	request, _, err := a.Attempt(ctx)
	if err != nil {
		return "", nil, err
	}
	evidence, err := canonicalJSON(guestbridge.TerminalEvidence{RetirementID: request.RetirementID, Credentials: request.Credentials,
		GuestThrough: request.GuestThrough, ObserverRevision: request.ObserverRevision, ClosureHash: request.ClosureHash,
		SettledHash: response.SettledHash, OwnershipHash: response.OwnershipHash, RetainedOwner: "C8_TERMINAL_CELL"})
	return "SESSION_RETIRED", evidence, err
}
