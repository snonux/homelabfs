package viewcontrol

import (
	"context"
	"encoding/json"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
)

type terminalScope struct {
	Seal guestbridge.TerminalSeal
	// Snapshot contains only this scope's immutable checked records. Unrelated
	// domains and shared sources continue to change without changing this seal.
	Snapshot json.RawMessage
}

type terminalSnapshot struct {
	Scope        guestbridge.Context
	Families     map[string]family
	Tickets      map[string]ticketState
	Leases       map[string]leaseState
	Reservations map[string]reservationState
	Domain       *domainState
}

// SealTerminal checks all real scope owners under the controller lock and
// protects the irreversible scope seal with the complete original ledger.
func (c *Controller) SealTerminal(ctx context.Context, a *guestbridge.TerminalAttempt) (guestbridge.TerminalSeal, error) {
	_, scope, err := a.Attempt(ctx)
	if err != nil {
		return guestbridge.TerminalSeal{}, err
	}
	if err = c.check(ctx, scope); err != nil {
		return guestbridge.TerminalSeal{}, err
	}
	if !c.mu.TryLock() {
		return guestbridge.TerminalSeal{}, fsapi.WouldBlock
	}
	defer c.mu.Unlock()
	if c.poisoned.Load() || c.closed.Load() || c.state.Recovery {
		return guestbridge.TerminalSeal{}, fsapi.Unavailable
	}
	state, err := c.rawState()
	if err != nil {
		return guestbridge.TerminalSeal{}, err
	}
	if err = VerifyRetainedSources(c.factsContext(ctx), c.config.Service, state); err != nil {
		return guestbridge.TerminalSeal{}, err
	}
	snapshot, err := c.terminalSnapshot(scope)
	if err != nil {
		return guestbridge.TerminalSeal{}, err
	}
	seal, err := a.Seal(ctx, "CONTROLLER", hash(snapshot))
	if err != nil {
		return seal, err
	}
	if saved, ok := c.state.Terminal[scope.Session]; ok {
		if !equal(saved.Seal, seal) || string(saved.Snapshot) != string(snapshot) {
			return seal, fsapi.Stale
		}
		return seal, nil
	}
	if c.state.Terminal == nil {
		c.state.Terminal = map[string]terminalScope{}
	}
	c.state.Terminal[scope.Session] = terminalScope{seal, snapshot}
	if err = c.save(); err != nil {
		return seal, err
	}
	c.sealed.Store(scope.Session, seal)
	return seal, c.fail("terminal-controller-sealed")
}

// VerifyTerminalSeal checks the actual still-live locked owner and protected
// scope snapshot. A detached seal or reopened ledger supplies no authority.
func (c *Controller) VerifyTerminalSeal(ctx context.Context, a *guestbridge.TerminalAttempt, seal guestbridge.TerminalSeal) error {
	_, scope, err := a.Attempt(ctx)
	if err != nil {
		return err
	}
	if !c.mu.TryLock() {
		return fsapi.WouldBlock
	}
	defer c.mu.Unlock()
	if c.closed.Load() || c.poisoned.Load() {
		return fsapi.Unavailable
	}
	saved, ok := c.state.Terminal[scope.Session]
	if !ok || !equal(saved.Seal, seal) {
		return fsapi.Stale
	}
	snapshot, err := c.terminalSnapshot(scope)
	if err != nil {
		return err
	}
	if string(saved.Snapshot) != string(snapshot) || hash(snapshot) != seal.StateHash {
		return fsapi.Stale
	}
	return nil
}

func (c *Controller) terminalSnapshot(scope guestbridge.Context) (json.RawMessage, error) {
	snapshot := terminalSnapshot{Scope: scope, Families: map[string]family{}, Tickets: map[string]ticketState{}, Leases: map[string]leaseState{}, Reservations: map[string]reservationState{}}
	families := map[string]guestbridge.Context{}
	for key, t := range c.state.Tickets {
		_, familyKey := ticketKeys(t.Scope, t.Request)
		if old, ok := families[familyKey]; ok && old != t.Scope {
			return nil, fsapi.Permission
		}
		families[familyKey] = t.Scope
		if t.Scope == scope {
			f, ok := c.state.Families[familyKey]
			if !ok || f.High != f.Floor || t.Request.Number > f.Floor || !c.retired(t) {
				return nil, fsapi.WouldBlock
			}
			snapshot.Families[familyKey] = f
			snapshot.Tickets[key] = t
		}
	}
	for key, f := range c.state.Families {
		owner, ok := families[key]
		if !ok && (f.High != 0 || f.Floor != 0) {
			return nil, fsapi.Permission
		}
		if owner == scope && f.High != f.Floor {
			return nil, fsapi.WouldBlock
		}
	}
	for key, l := range c.state.Leases {
		if l.Lease.Scope == scope {
			if l.Active {
				return nil, fsapi.WouldBlock
			}
			snapshot.Leases[key] = l
		}
	}
	for key, r := range c.state.Reservations {
		if r.Reservation.Scope == scope {
			if r.Status != "CLOSED" {
				return nil, fsapi.WouldBlock
			}
			snapshot.Reservations[key] = r
		}
	}
	if r := c.state.Recall; r != nil {
		if _, ok := r.Events[scope.Session]; ok {
			return nil, fsapi.WouldBlock
		}
		if _, ok := r.Acks[scope.Session]; ok {
			return nil, fsapi.WouldBlock
		}
		if _, ok := r.Manifests[scope.Session]; ok {
			return nil, fsapi.WouldBlock
		}
	}
	if d, ok := c.state.Domains[scope.Session]; ok {
		if d.Domain.Scope != scope {
			return nil, fsapi.Stale
		}
		snapshot.Domain = &d
	}
	return canonical(snapshot)
}

func (c *Controller) terminalIngress(ctx context.Context, scope guestbridge.Context, op string) error {
	value, ok := c.sealed.Load(scope.Session)
	if !ok {
		return nil
	}
	seal := value.(guestbridge.TerminalSeal)
	observed, verified := guestbridge.VerifiedRequest(ctx)
	if !verified || op != "SESSION_RETIRE" || observed.Operation != op || observed.Scope != scope || observed.Protocol == nil || *observed.Protocol != guestbridge.TerminalProtocol() || observed.Controller.String() != seal.Controller {
		return fsapi.Permission
	}
	return nil
}
