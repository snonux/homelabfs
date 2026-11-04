//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

// ProviderRouter dispatches complete enrolled scopes to their actual private
// providers. Its copied route table never falls back to another session.
// It owns no provider lifetime and supplies no new publication authority.
type ProviderRouter struct{ routes map[string]providerRoute }
type providerRoute struct {
	scope    guestbridge.Context
	provider *controlledvalidation.Provider
}

var _ guestbridge.Claims = (*ProviderRouter)(nil)
var _ guestbridge.Execution = (*ProviderRouter)(nil)
var _ guestbridge.ValidationEnrollment = (*ProviderRouter)(nil)
var _ guestbridge.ValidationState = (*ProviderRouter)(nil)
var _ guestbridge.ValidationExecution = (*ProviderRouter)(nil)
var _ guestbridge.ValidationContinuation = (*ProviderRouter)(nil)
var _ guestbridge.ControlledAllocation = (*ProviderRouter)(nil)
var _ guestbridge.ControlledRetirement = (*ProviderRouter)(nil)
var _ guestbridge.ReaddirCleanupStore = (*ProviderRouter)(nil)

// NewProviderRouter freezes actual providers sharing one local service, endpoint,
// serving generation and journal. Enrollment scope keys must match in full.
func NewProviderRouter(ctx context.Context, service *fsservice.Service, providers map[guestbridge.Context]*controlledvalidation.Provider) (*ProviderRouter, error) {
	if service == nil || len(providers) == 0 || len(providers) > 64 {
		return nil, fsapi.Permission
	}
	r := &ProviderRouter{routes: make(map[string]providerRoute, len(providers))}
	var first *controlledvalidation.Enrollment
	for scope, provider := range providers {
		if provider == nil || !provider.UsesService(service) {
			return nil, fsapi.Permission
		}
		e := provider.Enrollment()
		if e.Scope != scope || scope.Session == "" || r.routes[scope.Session].provider != nil {
			return nil, fsapi.Permission
		}
		if first != nil && (e.Endpoint != first.Endpoint || e.ServingGeneration != first.ServingGeneration || e.Journal != first.Journal || e.Scope.Volume != first.Scope.Volume || e.Profile != first.Profile) {
			return nil, fsapi.Permission
		}
		if err := provider.Check(ctx, scope); err != nil {
			return nil, err
		}
		first = &e
		r.routes[scope.Session] = providerRoute{scope, provider}
	}
	return r, nil
}

// Resolve forwards unchanged arguments to the exact enrolled provider.
func (r *ProviderRouter) Resolve(ctx context.Context, scope guestbridge.Context, claim guestbridge.Claim) (guestbridge.ClaimState, error) {
	p, err := r.route(scope)
	if err != nil {
		return guestbridge.ClaimState{}, err
	}
	return p.Resolve(ctx, scope, claim)
}

// Reserve forwards unchanged arguments to the exact enrolled provider.
func (r *ProviderRouter) Reserve(ctx context.Context, b guestbridge.Binding) (guestbridge.ClaimState, error) {
	p, err := r.route(b.Context)
	if err != nil {
		return guestbridge.ClaimState{}, err
	}
	return p.Reserve(ctx, b)
}

// ProtectResult forwards unchanged arguments to the exact enrolled provider.
func (r *ProviderRouter) ProtectResult(ctx context.Context, b guestbridge.Binding, result json.RawMessage) error {
	p, err := r.route(b.Context)
	if err != nil {
		return err
	}
	return p.ProtectResult(ctx, b, result)
}

// Cancel forwards unchanged arguments to the exact enrolled provider.
func (r *ProviderRouter) Cancel(ctx context.Context, scope guestbridge.Context, claim guestbridge.Claim) (guestbridge.ClaimState, error) {
	p, err := r.route(scope)
	if err != nil {
		return guestbridge.ClaimState{}, err
	}
	return p.Cancel(ctx, scope, claim)
}

// Retire forwards unchanged arguments to the exact enrolled provider.
func (r *ProviderRouter) Retire(ctx context.Context, scope guestbridge.Context, raw json.RawMessage) (json.RawMessage, error) {
	p, err := r.route(scope)
	if err != nil {
		return nil, err
	}
	return p.Retire(ctx, scope, raw)
}

// Check forwards unchanged arguments to the exact enrolled provider.
func (r *ProviderRouter) Check(ctx context.Context, scope guestbridge.Context) error {
	p, err := r.route(scope)
	if err != nil {
		return err
	}
	return p.Check(ctx, scope)
}

// ServingGeneration forwards unchanged arguments to the exact enrolled provider.
func (r *ProviderRouter) ServingGeneration(ctx context.Context, scope guestbridge.Context) (fsapi.UUID, error) {
	p, err := r.route(scope)
	if err != nil {
		return fsapi.UUID{}, err
	}
	return p.ServingGeneration(ctx, scope)
}

// Execute forwards unchanged arguments to the exact enrolled provider.
func (r *ProviderRouter) Execute(ctx context.Context, b guestbridge.Binding, prepare func(guestbridge.Captured) (guestbridge.Prepared, error)) (guestbridge.Outcome, error) {
	p, err := r.route(b.Context)
	if err != nil {
		return guestbridge.Outcome{}, err
	}
	return p.Execute(ctx, b, prepare)
}

// CheckValidation forwards unchanged arguments to the exact enrolled provider.
func (r *ProviderRouter) CheckValidation(ctx context.Context, scope guestbridge.Context, profile fsservice.ValidationProfile, endpoint guestbridge.Endpoint) error {
	p, err := r.route(scope)
	if err != nil {
		return err
	}
	return p.CheckValidation(ctx, scope, profile, endpoint)
}

// Restore forwards unchanged arguments to the exact enrolled provider.
func (r *ProviderRouter) Restore(ctx context.Context, scope guestbridge.Context) (json.RawMessage, error) {
	p, err := r.route(scope)
	if err != nil {
		return nil, err
	}
	return p.Restore(ctx, scope)
}

// Finalize forwards unchanged arguments to the exact enrolled provider.
func (r *ProviderRouter) Finalize(ctx context.Context, b guestbridge.Binding, result, snapshot json.RawMessage) error {
	p, err := r.route(b.Context)
	if err != nil {
		return err
	}
	return p.Finalize(ctx, b, result, snapshot)
}

// RecoverOutcome forwards unchanged arguments to the exact enrolled provider.
func (r *ProviderRouter) RecoverOutcome(ctx context.Context, b guestbridge.Binding) (guestbridge.Outcome, bool, error) {
	p, err := r.route(b.Context)
	if err != nil {
		return guestbridge.Outcome{}, false, err
	}
	return p.RecoverOutcome(ctx, b)
}

// ExecuteValidation forwards unchanged arguments to the exact enrolled provider.
func (r *ProviderRouter) ExecuteValidation(ctx context.Context, b guestbridge.Binding, prepare func(guestbridge.Captured) (guestbridge.Prepared, error), protect func(guestbridge.Outcome) (json.RawMessage, json.RawMessage, error)) (guestbridge.Outcome, error) {
	p, err := r.route(b.Context)
	if err != nil {
		return guestbridge.Outcome{}, err
	}
	return p.ExecuteValidation(ctx, b, prepare, protect)
}

// ResumeValidation forwards unchanged arguments to the exact enrolled provider.
func (r *ProviderRouter) ResumeValidation(ctx context.Context, b guestbridge.Binding, prepare func(guestbridge.Captured) (guestbridge.Prepared, error), protect func(guestbridge.Outcome) (json.RawMessage, json.RawMessage, error)) (guestbridge.Outcome, error) {
	p, err := r.route(b.Context)
	if err != nil {
		return guestbridge.Outcome{}, err
	}
	return p.ResumeValidation(ctx, b, prepare, protect)
}

// AllocateOriginal forwards unchanged arguments to the exact enrolled provider.
func (r *ProviderRouter) AllocateOriginal(ctx context.Context, scope guestbridge.Context, call guestbridge.Call) error {
	p, err := r.route(scope)
	if err != nil {
		return err
	}
	return p.AllocateOriginal(ctx, scope, call)
}

// ResolveOriginalGap forwards unchanged arguments to the exact enrolled provider.
func (r *ProviderRouter) ResolveOriginalGap(ctx context.Context, scope guestbridge.Context, call guestbridge.Call) (string, error) {
	p, err := r.route(scope)
	if err != nil {
		return "", err
	}
	return p.ResolveOriginalGap(ctx, scope, call)
}

// RetireOriginal forwards unchanged arguments to the exact enrolled provider.
func (r *ProviderRouter) RetireOriginal(ctx context.Context, scope guestbridge.Context, raw json.RawMessage) (json.RawMessage, error) {
	p, err := r.route(scope)
	if err != nil {
		return nil, err
	}
	return p.RetireOriginal(ctx, scope, raw)
}

// CleanupReaddir forwards unchanged arguments to the exact enrolled provider.
func (r *ProviderRouter) CleanupReaddir(ctx context.Context, scope guestbridge.Context, raw json.RawMessage) (json.RawMessage, error) {
	p, err := r.route(scope)
	if err != nil {
		return nil, err
	}
	return p.CleanupReaddir(ctx, scope, raw)
}

// CheckBridgeProtocol forwards unchanged arguments to the exact enrolled provider.
func (r *ProviderRouter) CheckBridgeProtocol(ctx context.Context, scope guestbridge.Context, selected *guestbridge.Protocol) error {
	p, err := r.route(scope)
	if err != nil {
		return err
	}
	return p.CheckBridgeProtocol(ctx, scope, selected)
}

// CheckViewProtocol verifies every actual provider's controller binding.
func (r *ProviderRouter) CheckViewProtocol(views guestbridge.Views, protocol guestbridge.ViewProtocolProvider) error {
	if r == nil || len(r.routes) == 0 {
		return fsapi.Permission
	}
	for _, route := range r.routes {
		if err := route.provider.CheckViewProtocol(views, protocol); err != nil {
			return err
		}
	}
	return nil
}

func (r *ProviderRouter) route(scope guestbridge.Context) (*controlledvalidation.Provider, error) {
	if r == nil {
		return nil, fsapi.Permission
	}
	route, ok := r.routes[scope.Session]
	if !ok || route.scope != scope || route.provider == nil {
		return nil, fsapi.Permission
	}
	return route.provider, nil
}

// ProtectTerminalBridge forwards the exact private terminal worker and lifetime
// state to this scope's original protected provider.
func (r *ProviderRouter) ProtectTerminalBridge(ctx context.Context, a *guestbridge.TerminalAttempt, seal guestbridge.TerminalSeal, snapshot []byte) error {
	_, scope, err := a.Attempt(ctx)
	if err != nil {
		return err
	}
	p, err := r.route(scope)
	if err != nil {
		return err
	}
	return p.ProtectTerminalBridge(ctx, a, seal, snapshot)
}

// VerifyTerminalBridge verifies the actual original scope's protected snapshot.
func (r *ProviderRouter) VerifyTerminalBridge(ctx context.Context, a *guestbridge.TerminalAttempt, seal guestbridge.TerminalSeal, snapshot []byte) error {
	_, scope, err := a.Attempt(ctx)
	if err != nil {
		return err
	}
	p, err := r.route(scope)
	if err != nil {
		return err
	}
	return p.VerifyTerminalBridge(ctx, a, seal, snapshot)
}
