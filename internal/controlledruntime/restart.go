//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// Restart is a private, single-use barrier over the actual surviving native
// supervisor, paused exporter, complete C8 cut and original semantic ledgers.
// Opening serialized state or constructing this zero value grants no authority.
type Restart struct {
	mu                    sync.Mutex
	supervisor            *Supervisor
	enrollment            viewcontrol.Enrollment
	admissions            []*Admission
	keys                  map[string]ed25519.PrivateKey
	predecessor           *guestbridge.Transport
	bridge                guestbridge.Config
	funding               *guestbridge.RestorationFunding
	providers             map[string]*controlledvalidation.Restart
	controller            *viewcontrol.Restart
	service               *fsservice.ControlledRestart
	barrier               fsapi.UUID
	exporterDigest        [32]byte
	exporter              *os.File
	exporterBytes         int64
	bound                 []byte
	physical              string
	hello                 map[string]bool
	expires               time.Time
	used, ready, enrolled bool
	assembly              *RestoredEndpoint
}

// RestoredEndpoint owns freshly reopened actual service/provider/controller/
// bridge/transport components. Native activation is a separate final handoff.
// Reopen may return a partial assembly with an error: close each nonnil owner,
// preserving its files for inspection; Providers contains only successful owners.
type RestoredEndpoint struct {
	Service    *fsservice.Service
	Providers  map[string]*controlledvalidation.Provider
	Controller *viewcontrol.Controller
	Producer   *Producer
	Bridge     *guestbridge.Service
	Transport  *guestbridge.Transport
	Facts      *Facts
}

var _ guestbridge.EndpointRestoreVerifier = (*Supervisor)(nil)

// SuspendEndpoint creates one bounded restart barrier. The actual exporter is
// paused first, then old host workers join before owner snapshots or closes.
// Failures preserve paused/fenced ownership; there is no rollback/reset path.
func (s *Supervisor) SuspendEndpoint(ctx context.Context, p *Producer, t *guestbridge.Transport) (*Restart, error) {
	if p == nil || t == nil {
		return nil, fsapi.Invalid
	}
	binding, err := t.VerificationBinding(p)
	if err != nil {
		return nil, err
	}
	c, err := t.RestorationConfig()
	if err != nil {
		return nil, err
	}
	if binding.Native != s || binding.Views != p.controller || p.facts == nil {
		return nil, fsapi.Permission
	}
	s.mu.Lock()
	if s.restartAttempted || s.checkLive() != nil || s.record.Stage != "claimed" || binding.Controller != s.record.Controller || binding.Endpoint != s.record.Endpoint {
		s.mu.Unlock()
		return nil, fsapi.Permission
	}
	s.restartAttempted = true
	r := &Restart{supervisor: s, enrollment: p.facts.enrollment, admissions: append([]*Admission(nil), p.facts.admissions...), keys: map[string]ed25519.PrivateKey{}, predecessor: t, bridge: c, providers: map[string]*controlledvalidation.Restart{}, expires: restartDeadline(ctx)}
	for id, key := range p.keys {
		r.keys[id] = append(ed25519.PrivateKey(nil), key...)
	}
	r.bridge.Claims = nil
	r.bridge.Execution = nil
	r.bridge.Views = nil
	r.bridge.ViewProtocol = nil
	r.bridge.Verification = nil
	complete := false
	defer func() {
		if !complete {
			s.fence()
		}
	}()
	if _, err = rand.Read(r.barrier[:]); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	s.mu.Unlock()
	if err = r.pauseExporter(ctx); err != nil {
		return nil, fmt.Errorf("exporter pause: %w", err)
	}
	r.funding, err = t.Suspend()
	if err != nil {
		return nil, fmt.Errorf("transport suspension: %w", err)
	}
	cells, err := r.readCells()
	if err != nil {
		return nil, err
	}
	if err = reconcileExporter(r.exporter, r.exporterBytes, r.exporterDigest, r.funding.State(), cells, r.hello); err != nil {
		return nil, fmt.Errorf("paired exporter/C8 cut: %w", err)
	}
	if err = r.refresh(ctx); err != nil {
		return nil, fmt.Errorf("physical refresh: %w", err)
	}
	if err = r.checkTerminals(ctx, cells, p.providers, p.controller); err != nil {
		return nil, err
	}
	if err = r.bindExporter(ctx); err != nil {
		return nil, fmt.Errorf("exporter cut binding: %w", err)
	}
	r.controller, err = p.controller.SuspendRestart(ctx)
	if err != nil {
		return nil, fmt.Errorf("controller suspension: %w", err)
	}
	for session, provider := range p.providers {
		r.providers[session], err = provider.SuspendRestart(ctx)
		if err != nil {
			return nil, err
		}
	}
	r.service, err = p.facts.service.SuspendControlled(ctx, p.facts.enrollment.Bounds.SourceBytes)
	if err != nil {
		return nil, fmt.Errorf("service suspension: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.checkLive() != nil || time.Now().After(r.expires) {
		return nil, fsapi.Unavailable
	}
	s.restart = r
	complete = true
	return r, nil
}

// Reopen reconstructs all semantic components from their real protected files.
// Original controller/capability/source identities are admitted only after the
// actual paired cut, original files and fresh full physical inventory agree.
// Unsupported source history, lifetime tables or partial work stays BLOCKED.
func (r *Restart) Reopen(ctx context.Context) (*RestoredEndpoint, error) {
	if r == nil {
		return nil, fsapi.Permission
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.used {
		return nil, fsapi.Permission
	}
	r.used = true
	complete := false
	defer func() {
		if !complete && r.supervisor != nil {
			r.supervisor.fence()
		}
	}()
	if err := r.check(ctx); err != nil {
		return nil, err
	}
	a := &RestoredEndpoint{Providers: map[string]*controlledvalidation.Provider{}}
	// Retain partial actual owners for inspection rather than deleting evidence.
	r.assembly = a
	var err error
	a.Service, err = r.service.Reopen(ctx)
	if err != nil {
		return a, fmt.Errorf("service reopen: %w", err)
	}
	if err = r.refresh(ctx); err != nil {
		return a, err
	}
	a.Facts, err = NewFacts(ctx, a.Service, r.enrollment, r.admissions)
	if err != nil {
		return a, err
	}
	a.Facts.restart = r
	for session, token := range r.providers {
		provider, err := token.Reopen(ctx, a.Service, a.Facts)
		if err != nil {
			return a, fmt.Errorf("provider reopen %s: %w", session, err)
		}
		a.Providers[session] = provider
	}
	a.Controller, err = r.controller.Reopen(ctx, a.Service, a.Providers, a.Facts)
	if err != nil {
		return a, fmt.Errorf("controller reopen: %w", err)
	}
	keys := map[string]ed25519.PrivateKey{}
	for session, key := range r.keys {
		keys[session] = append(ed25519.PrivateKey(nil), key...)
	}
	a.Producer, err = NewProducer(ctx, a.Facts, a.Controller, a.Providers, keys)
	if err != nil {
		return a, err
	}
	routes := map[guestbridge.Context]*controlledvalidation.Provider{}
	for _, domain := range a.Facts.enrollment.Domains {
		routes[domain.Scope] = a.Providers[domain.Scope.Session]
	}
	router, err := NewProviderRouter(ctx, a.Service, routes)
	if err != nil {
		return a, err
	}
	c := r.bridge
	c.Claims = router
	c.Execution = router
	c.Views = a.Controller
	c.ViewProtocol = a.Controller
	c.Verification = a.Producer
	cells, err := r.readCells()
	if err != nil {
		return a, err
	}
	if err = reconcileExporter(r.exporter, r.exporterBytes, r.exporterDigest, r.funding.State(), cells, r.hello); err != nil {
		return a, err
	}
	if err = r.checkTerminals(ctx, cells, a.Providers, a.Controller); err != nil {
		return a, err
	}
	if err = r.refresh(ctx); err != nil {
		return a, err
	}
	r.supervisor.mu.Lock()
	r.ready = true
	r.supervisor.mu.Unlock()
	a.Bridge, a.Transport, err = guestbridge.RestoreDurableEndpoint(ctx, c, r.funding.Root(), r.funding)
	if err != nil {
		return a, fmt.Errorf("endpoint reconstruction: %w", err)
	}
	complete = true
	return a, nil
}

// Activate replaces only the original shell's joined frontend, then releases
// the exact same paused exporter. A replacement FUSE connection always refuses.
func (r *Restart) Activate(ctx context.Context, a *RestoredEndpoint) error {
	if r == nil {
		return fsapi.Permission
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if a == nil || a != r.assembly || a.Transport == nil {
		return fsapi.Permission
	}
	if err := r.check(ctx); err != nil {
		return err
	}
	if err := r.refresh(ctx); err != nil {
		return err
	}
	s := r.supervisor
	s.mu.Lock()
	if s.restart != r || !r.enrolled || s.checkLive() != nil || s.mount.shell == nil {
		s.mu.Unlock()
		return fsapi.Unavailable
	}
	shell := s.mount.shell
	s.mu.Unlock()
	if err := shell.ReplaceEndpoint(r.predecessor, a.Transport); err != nil {
		return err
	}
	if err := r.resumeExporter(ctx); err != nil {
		s.fence()
		return err
	}
	return nil
}

// RestoreEndpoint implements the additional trusted constructor boundary. It
// consumes only this supervisor's actual, complete, fresh single-use grant;
// Enroll stays once-only and Open cannot mint a grant from retained files.
func (s *Supervisor) RestoreEndpoint(ctx context.Context, state guestbridge.ProtectedTransportState, config guestbridge.Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.restart
	if r == nil || !r.ready || r.enrolled || time.Now().After(r.expires) || s.checkLive() != nil || state.Endpoint != s.record.Endpoint || state.Controller != s.record.Controller || state.Digest != r.funding.State().Digest {
		return fsapi.Permission
	}
	a := r.assembly
	if a == nil || a.Producer == nil || config.Native != s || config.Verification != a.Producer || config.Views != a.Controller || config.ViewProtocol != a.Controller {
		return fsapi.Permission
	}
	binding := guestbridge.VerificationBinding{Native: config.Native, Endpoint: config.Endpoint, Controller: state.Controller, Claims: config.Claims, Execution: config.Execution, Views: config.Views, ViewProtocol: config.ViewProtocol, Scopes: config.Scopes}
	for _, scope := range config.Scopes {
		if !a.Producer.providerBinding(binding, scope, a.Providers[scope.Session]) {
			return fsapi.Permission
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	r.enrolled = true
	return nil
}

func (r *Restart) readCells() (map[uint64]guestbridge.ProtectedTransportCell, error) {
	cells := map[uint64]guestbridge.ProtectedTransportCell{}
	r.hello = map[string]bool{}
	index, err := guestbridge.InspectRestorationEndpoint(r.funding.Root(), func(c guestbridge.ProtectedTransportCell) error {
		if c.Command == nil || c.JobPhase != "responded" || c.Response == nil {
			return fsapi.Unavailable
		}
		cells[c.Header.Cell] = c
		return nil
	}, func(scope guestbridge.Context, row guestbridge.ProtectedBridgeSession) error {
		var state struct{ Hello bool }
		if err := json.Unmarshal(row.State, &state); err != nil {
			return err
		}
		r.hello[scope.Session] = state.Hello
		return nil
	})
	if err != nil {
		return nil, err
	}
	if index.Digest != r.funding.State().Digest || index.Workspace || !index.BridgeSessions {
		return nil, fsapi.Stale
	}
	return cells, nil
}
func (r *Restart) check(ctx context.Context) error {
	if r == nil || r.supervisor == nil || r.funding == nil || r.controller == nil || r.service == nil || time.Now().After(r.expires) {
		return fsapi.Unavailable
	}
	s := r.supervisor
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.restart != r || s.checkLive() != nil || s.record.Controller != r.funding.State().Controller {
		return fsapi.Permission
	}
	return ctx.Err()
}
func (r *Restart) refresh(ctx context.Context) error {
	for _, a := range r.admissions {
		if a.supervisor != r.supervisor {
			return fsapi.Permission
		}
		if err := a.Refresh(ctx); err != nil {
			return err
		}
	}
	raw := []kernelAdmission{}
	for _, a := range r.admissions {
		a.mu.Lock()
		raw = append(raw, a.installed.Admission)
		a.mu.Unlock()
	}
	value, err := canonicalJSON(raw)
	if err != nil {
		return err
	}
	r.physical = hashBytes(value)
	return nil
}
func (r *Restart) checkPhysical(ctx context.Context) error {
	if err := r.check(ctx); err != nil {
		return err
	}
	raw := []kernelAdmission{}
	for _, a := range r.admissions {
		a.mu.Lock()
		domains := append([]guestbridge.Context(nil), a.installed.Admission.Domains...)
		raw = append(raw, a.installed.Admission)
		a.mu.Unlock()
		for _, scope := range domains {
			if err := a.check(scope); err != nil {
				return err
			}
		}
	}
	value, err := canonicalJSON(raw)
	if err != nil {
		return err
	}
	if hashBytes(value) != r.physical {
		return fsapi.Stale
	}
	return nil
}
func (r *Restart) checkTerminals(ctx context.Context, cells map[uint64]guestbridge.ProtectedTransportCell, providers map[string]*controlledvalidation.Provider, controller *viewcontrol.Controller) error {
	for _, cell := range cells {
		if cell.Terminal == nil {
			continue
		}
		scope := cell.Command.Context
		if providers[scope.Session] == nil {
			return fsapi.Stale
		}
		if err := providers[scope.Session].CheckRestoredTerminal(ctx, scope, cell.Terminal.Seals); err != nil {
			return err
		}
		if err := controller.CheckRestoredTerminal(ctx, scope, cell.Terminal.Seals); err != nil {
			return err
		}
		a := r.admission(scope)
		if a == nil {
			return fsapi.Stale
		}
		a.mu.Lock()
		var observed *guestbridge.TerminalSnapshot
		for _, row := range a.installed.Admission.Observations {
			if row.Scope == scope && (row.Phase == "TERMINAL_PENDING" || row.Phase == "TERMINAL_RETIRED") {
				observed = row.Terminal
			}
		}
		a.mu.Unlock()
		if observed == nil || observed.Validate() != nil {
			return fsapi.Stale
		}
		raw, err := canonicalJSON(observed)
		if err != nil {
			return err
		}
		request := cell.Terminal.Request
		if hashBytes(raw) != request.ClosureHash || observed.Controller != r.funding.State().Controller.String() || observed.RetirementID != request.RetirementID || observed.Revision != request.ObserverRevision || observed.GuestThrough != request.GuestThrough || !sameJSON(observed.Credentials, request.Credentials) || !sameJSON(observed.Donor, cell.Mapping.GuestReclaim) || !sameJSON(observed.SettledCells, request.SettledCells) {
			return fsapi.Stale
		}
	}
	return nil
}

func (r *Restart) admission(scope guestbridge.Context) *Admission {
	for _, a := range r.admissions {
		a.mu.Lock()
		domains := append([]guestbridge.Context(nil), a.installed.Admission.Domains...)
		a.mu.Unlock()
		for _, old := range domains {
			if old == scope {
				return a
			}
		}
	}
	return nil
}

func (r *Restart) checkIdlePhysical(l factsLedger) error {
	// This implementation reopens an intact idle current cut. In-progress recalls
	// and disconnected/recovery owners require additional fresh ACK/exclusion work.
	if l.Recall != nil || l.Recovery {
		return fsapi.Unavailable
	}
	index := r.funding.State()
	for _, a := range r.admissions {
		a.mu.Lock()
		for _, row := range a.installed.Admission.Observations {
			switch row.Phase {
			case "LIVE":
				if row.Fence > l.Fence {
					a.mu.Unlock()
					return fsapi.Stale
				}
				if _, known := l.Domains[row.Scope.Session]; known && row.Fence != l.Fence {
					a.mu.Unlock()
					return fsapi.Stale
				}
			case "TERMINAL_PENDING", "TERMINAL_RETIRED":
				if index.Terminal[row.Scope.Session] == 0 {
					a.mu.Unlock()
					return fsapi.Stale
				}
			default:
				a.mu.Unlock()
				return fsapi.Unavailable
			}
		}
		a.mu.Unlock()
	}
	return nil
}
