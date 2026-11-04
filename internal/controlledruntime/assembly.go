//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// ProviderLimits are explicit per-domain capacities; zero never selects defaults.
type ProviderLimits struct {
	Claims                                                          int
	RetainedBytes, SourceBytes, ClaimCreditBytes, SourceCreditBytes uint64
}

// AssemblyConfig binds already measured native and guest owners to their signed
// complete inventory. It cannot create native/physical evidence from config.
type AssemblyConfig struct {
	Directory                                               string
	Service                                                 *fsservice.Service
	Supervisor                                              *Supervisor
	Admissions                                              []*Admission
	Authorization                                           viewcontrol.Authorization
	Providers                                               []controlledvalidation.Authorization
	Operator                                                ed25519.PublicKey
	HostKeys                                                map[string]ed25519.PrivateKey
	Attestor                                                guestbridge.Attestor
	ProviderLimits                                          ProviderLimits
	Capabilities, CapabilityIdentities, CredentialRevisions int
	Budget                                                  *admission.Budget
}

// Assembly owns real semantic components, including the durable C8 endpoint.
// Construction alone does not bind a shell or establish native readiness.
type Assembly struct {
	Facts      *Facts
	Providers  map[string]*controlledvalidation.Provider
	Controller *viewcontrol.Controller
	Producer   *Producer
	Bridge     *guestbridge.Service
	Transport  *guestbridge.Transport
}

// Assemble never accepts replacement claims/execution/source implementations.
// Each route is the actual provider owned by the one admitted service and Facts.
// Partial owners are returned on error and must be closed, retaining all files.
func Assemble(ctx context.Context, c AssemblyConfig) (*Assembly, error) {
	if c.Service == nil || !c.Service.ControlledViewsRequired() || c.Supervisor == nil || len(c.Admissions) == 0 || c.Attestor == nil || c.Budget == nil || !filepath.IsAbs(c.Directory) || len(c.Operator) != ed25519.PublicKeySize {
		return nil, fsapi.Invalid
	}
	e := c.Authorization.Enrollment
	if len(e.Domains) < 1 || len(e.Domains) > 16 || len(c.Providers) != len(e.Domains) || len(c.HostKeys) != len(e.Domains) {
		return nil, fsapi.Permission
	}
	l := c.ProviderLimits
	if !validProviderLimits(l) || c.Capabilities < 1 || c.CapabilityIdentities < c.Capabilities || c.CredentialRevisions < 2 {
		return nil, fsapi.Invalid
	}
	for i, d := range e.Domains {
		if d.Protocol == nil || !guestbridge.IsMappedProtocol(*d.Protocol) || !sameJSON(d, c.Providers[i].Enrollment) || d.Endpoint != e.Domains[0].Endpoint {
			return nil, fsapi.Permission
		}
	}
	a := &Assembly{Providers: map[string]*controlledvalidation.Provider{}}
	var err error
	a.Facts, err = NewFacts(ctx, c.Service, e, c.Admissions)
	if err != nil {
		return a, fmt.Errorf("live Facts: %w", err)
	}
	routes := map[guestbridge.Context]*controlledvalidation.Provider{}
	for i, domain := range e.Domains {
		dir := filepath.Join(c.Directory, fmt.Sprintf("provider-%d", i))
		if err = privateAssemblyDirectory(dir); err != nil {
			return a, err
		}
		p, err := controlledvalidation.New(ctx, controlledvalidation.Config{Directory: dir, Initialize: true, Service: c.Service, Operator: c.Operator, Authorization: c.Providers[i], Facts: a.Facts, MaxClaims: l.Claims, MaxRetainedBytes: l.RetainedBytes, MaxSourceBytes: l.SourceBytes, ClaimCreditBytes: l.ClaimCreditBytes, SourceCreditBytes: l.SourceCreditBytes})
		if err != nil {
			return a, fmt.Errorf("provider %d: %w", i, err)
		}
		a.Providers[domain.Scope.Session] = p
		routes[domain.Scope] = p
	}
	dir := filepath.Join(c.Directory, "controller")
	if err = privateAssemblyDirectory(dir); err != nil {
		return a, err
	}
	a.Controller, err = viewcontrol.New(ctx, viewcontrol.Config{Directory: dir, Initialize: true, Service: c.Service, Providers: a.Providers, Operator: c.Operator, Authorization: c.Authorization, Facts: a.Facts})
	if err != nil {
		return a, fmt.Errorf("controller: %w", err)
	}
	a.Producer, err = NewProducer(ctx, a.Facts, a.Controller, a.Providers, c.HostKeys)
	if err != nil {
		return a, err
	}
	router, err := NewProviderRouter(ctx, c.Service, routes)
	if err != nil {
		return a, err
	}
	scopes := make([]guestbridge.Context, len(e.Domains))
	protocols := map[string]guestbridge.Protocol{}
	for i, d := range e.Domains {
		scopes[i] = d.Scope
		protocols[d.Scope.Session] = *d.Protocol
	}
	profile := e.Domains[0].Profile
	a.Bridge, err = guestbridge.New(ctx, guestbridge.Config{Validation: &profile, SessionProtocols: protocols, Endpoint: e.Domains[0].Endpoint, Native: c.Supervisor, Attestor: c.Attestor, Claims: router, Execution: router, Views: a.Controller, ViewProtocol: a.Controller, Verification: a.Producer, Scopes: scopes, MaxCapabilities: c.Capabilities, MaxCapabilityIdentities: c.CapabilityIdentities, MaxCredentialRevisions: c.CredentialRevisions})
	if err != nil {
		return a, fmt.Errorf("bridge: %w", err)
	}
	a.Transport, err = guestbridge.NewDurableTransport(a.Bridge, c.Budget, filepath.Join(c.Directory, "transport"))
	if err != nil {
		return a, fmt.Errorf("durable endpoint funding: %w", err)
	}
	return a, nil
}

func privateAssemblyDirectory(path string) error {
	if err := os.Mkdir(path, 0700); err != nil {
		return err
	}
	f, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return f.Sync()
}

// Close joins endpoint workers before closing the semantic owners. No ledger is
// removed and no guest/session is inferred retired by this local close.
func (a *Assembly) Close() error {
	if a == nil {
		return nil
	}
	var err error
	if a.Transport != nil {
		a.Transport.Close()
	}
	if a.Controller != nil {
		err = errors.Join(err, a.Controller.Close())
	}
	for _, p := range a.Providers {
		err = errors.Join(err, p.Close())
	}
	return err
}

func validProviderLimits(l ProviderLimits) bool {
	return l.Claims >= 1 && l.Claims <= 65536 && l.RetainedBytes > 0 && l.RetainedBytes <= 1<<30 && l.SourceBytes > 0 && l.SourceBytes <= 1<<30 && l.ClaimCreditBytes >= 4096 && l.SourceCreditBytes >= 4096 && l.ClaimCreditBytes <= l.RetainedBytes && l.SourceCreditBytes <= l.SourceBytes
}
