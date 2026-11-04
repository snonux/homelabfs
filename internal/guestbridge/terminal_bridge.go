package guestbridge

import (
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// TerminalBridgeStore is the existing protected lifetime owner selected in the
// bridge's actual Claims binding. It cannot be supplied to a terminal call.
type TerminalBridgeStore interface {
	ProtectTerminalBridge(context.Context, *TerminalAttempt, TerminalSeal, []byte) error
	VerifyTerminalBridge(context.Context, *TerminalAttempt, TerminalSeal, []byte) error
}

// SealBridge closes actual capability use and protects the complete lifetime
// snapshot through the bridge's originally bound store. Root/domain identities
// become tombstones only after sole-worker, zero reservation and no-live-use
// checks; other capability kinds must have already been released.
func (a *TerminalAttempt) SealBridge(ctx context.Context, coordinator TerminalCoordinator) (TerminalSeal, error) {
	if err := a.Check(ctx); err != nil {
		return TerminalSeal{}, err
	}
	t := a.transport
	if any(t.service.config.Verification) != any(coordinator) {
		return TerminalSeal{}, fsapi.Permission
	}
	store, ok := t.service.config.Claims.(TerminalBridgeStore)
	if !ok {
		return TerminalSeal{}, fsapi.Permission
	}
	_, scope, err := a.Attempt(ctx)
	if err != nil {
		return TerminalSeal{}, err
	}
	s := t.service
	s.resultMu.Lock()
	defer s.resultMu.Unlock()
	s.mu.Lock()
	ss := s.sessions[scope.Session]
	if ss == nil || ss.Context != scope || ss.Reserved != 0 {
		s.mu.Unlock()
		return TerminalSeal{}, fsapi.WouldBlock
	}
	raw, err := json.Marshal(ss)
	s.mu.Unlock()
	if err != nil {
		return TerminalSeal{}, err
	}
	var proposed session
	if err = json.Unmarshal(raw, &proposed); err != nil {
		return TerminalSeal{}, err
	}
	for token, g := range proposed.Grants {
		if g == nil || token != g.Cap.Token || g.Cap.Scope != scope {
			return TerminalSeal{}, fsapi.Stale
		}
		if !g.Released {
			// These exact stable identities own no lookup/description reference. The
			// sealed provider/controller and private terminal observer close users.
			if !g.NoLookup || g.Handle != 0 || len(g.Ancestors) != 0 || !oneOf(g.Cap.Kind, "ROOT", "DOMAIN") {
				return TerminalSeal{}, fsapi.WouldBlock
			}
			g.Released = true
		}
	}
	// The seal is excluded from its own committed snapshot.
	priorSeal := proposed.Terminal
	proposed.Terminal = nil
	snapshot, err := marshal(proposed)
	if err != nil {
		return TerminalSeal{}, err
	}
	seal, err := a.Seal(ctx, "BRIDGE", digest(snapshot))
	if err != nil {
		return seal, err
	}
	if priorSeal != nil {
		if *priorSeal != seal {
			return seal, fsapi.Stale
		}
		return seal, store.VerifyTerminalBridge(ctx, a, seal, snapshot)
	}
	if err = store.ProtectTerminalBridge(ctx, a, seal, snapshot); err != nil {
		return seal, err
	}
	// resultMu serializes lifetime transitions throughout this bounded commit.
	// Never acquire transport.mu while holding service.mu: native ingress uses
	// the reverse order. Revalidate the complete original state before applying
	// the protected tombstones; uncertainty preserves the durable scope fence.
	proposed.Terminal = &seal
	s.mu.Lock()
	defer s.mu.Unlock()
	ss = s.sessions[scope.Session]
	current, err := json.Marshal(ss)
	if err != nil {
		return seal, err
	}
	if string(current) != string(raw) {
		return seal, fsapi.Stale
	}
	*ss = proposed
	return seal, nil
}
