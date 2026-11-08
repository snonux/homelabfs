//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"path/filepath"
	"time"

	"github.com/snonux/homelabfs/internal/admin"
	"github.com/snonux/homelabfs/internal/fuseadapter"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// runtimeAdmitted is the verified result of the staged operator enrollment:
// the exact signed proposal, its handoff, scopes, measured Admission and the
// credential attestor derived from them. No field is caller-asserted.
type runtimeAdmitted struct {
	handoff  RuntimeEnrollment
	proposal viewcontrol.Enrollment
	scopes   []guestbridge.Context
	admitted *Admission
	attestor *kernelAttestor
}

// lifecycleBinding is announced in READY so an external harness can address
// the private lifecycle endpoint. It is a target, not an authorization.
type lifecycleBinding struct {
	Socket  string `json:"socket"`
	Runtime string `json:"runtime"`
	Cut     string `json:"cut"`
	Epoch   uint64 `json:"epoch"`
}

// runtimeBase owns the runtime funding, the exclusive local volume service and
// the supervisor. Once the semantic/exporter lease is attached to the
// supervisor it is never released here: Close is not retirement of uncertain
// native/guest ownership. Only the separately funded lifecycle endpoint lease is
// returned, after that endpoint joined.
type runtimeBase struct {
	funding    *runtimeFunding
	local      *runtimeLocal
	supervisor *Supervisor
	attached   bool
}

// runRuntimeWithOS composes the staged disposable runtime. Each phase keeps
// failures fenced and retains files/native/VM resources; defers close only
// local owners and never infer retirement.
func runRuntimeWithOS(ctx context.Context, c RuntimeConfig, output io.Writer, platform runtimeOS) (retErr error) {
	if err := validateRuntimeConfig(ctx, c); err != nil {
		return err
	}
	base, err := openRuntimeBase(ctx, c, platform)
	// Runs last: assembly and any lifecycle endpoint have already closed/joined.
	defer func() { retErr = errors.Join(retErr, base.close()) }()
	if err != nil {
		return err
	}
	local, supervisor, funding := base.local, base.supervisor, base.funding
	proposal, err := startNativeShell(ctx, c, platform, local, supervisor, output)
	if err != nil {
		return err
	}
	bootstrap, cancel := context.WithTimeout(ctx, time.Duration(c.BootstrapSeconds)*time.Second)
	defer cancel()
	admitted, err := admitOperatorEnrollment(bootstrap, c, supervisor, proposal)
	if err != nil {
		return err
	}
	assembly, err := Assemble(bootstrap, AssemblyConfig{Directory: c.OwnerDirectory, Service: local.service, Supervisor: supervisor, Admissions: []*Admission{admitted.admitted}, Authorization: admitted.handoff.Authorization, Providers: admitted.handoff.Providers, Operator: c.Operator, HostKeys: c.HostKeys, Attestor: admitted.attestor, ProviderLimits: c.Providers, Capabilities: c.Capabilities, CapabilityIdentities: c.CapabilityIdentities, CredentialRevisions: c.CredentialRevisions, Budget: funding.transport})
	defer func() { retErr = errors.Join(retErr, assembly.Close()) }()
	if err != nil {
		return err
	}
	if err = platform.bind(bootstrap, supervisor, assembly.Transport); err != nil {
		return err
	}
	controlCtx, stopControl := context.WithCancel(ctx)
	defer stopControl()
	control := make(chan error, 1)
	go func() { control <- supervisor.ServeControl(controlCtx) }()
	if err = bindGuestSessions(bootstrap, supervisor, assembly, admitted, control, output); err != nil {
		return err
	}
	return serveReady(ctx, c, platform, supervisor, local, assembly, admitted.admitted, control, output)
}

// openRuntimeBase funds every owner, opens the local service and creates the
// supervisor, attaching the retained lease only after its budget is protected.
// A partial base is returned on error so close can release what exists.
func openRuntimeBase(ctx context.Context, c RuntimeConfig, platform runtimeOS) (*runtimeBase, error) {
	funding, err := fundRuntime(c.Budget, c.Providers, c.Views, len(c.Scopes))
	if err != nil {
		return nil, err
	}
	b := &runtimeBase{funding: funding}
	if b.local, err = platform.open(ctx, c); err != nil {
		return b, err
	}
	if b.supervisor, err = platform.create(ctx, c.OwnerDirectory); err != nil {
		return b, err
	}
	if err = protectRuntimeBudget(b.supervisor, c); err != nil {
		return b, err
	}
	b.supervisor.runtimeFunding = funding
	b.attached = true
	return b, nil
}

// close closes the supervisor (fencing, never killing) and the local service,
// then releases only unattached owner funding and the lifecycle lease.
func (b *runtimeBase) close() error {
	if b == nil {
		return nil
	}
	var err error
	if b.supervisor != nil {
		err = errors.Join(err, b.supervisor.Close())
	}
	if b.local != nil {
		err = errors.Join(err, b.local.close())
	}
	if !b.attached {
		err = errors.Join(err, b.funding.owner.Release())
	}
	return errors.Join(err, b.funding.releaseControl())
}

// startNativeShell starts the exact exporter, mounts the unpublished shell and
// protects the inert proposal. Its output record is deliberately not readiness.
func startNativeShell(ctx context.Context, c RuntimeConfig, platform runtimeOS, local *runtimeLocal, supervisor *Supervisor, output io.Writer) (viewcontrol.Enrollment, error) {
	c.Exporter.ControlledBootstrap = true
	if err := platform.start(ctx, supervisor, c.Exporter); err != nil {
		return viewcontrol.Enrollment{}, err
	}
	shell, err := fuseadapter.NewShell(100, 101, 102, 103)
	if err != nil {
		return viewcontrol.Enrollment{}, err
	}
	endpoint, err := platform.mount(ctx, supervisor, local.mountpoint, shell)
	if err != nil {
		return viewcontrol.Enrollment{}, err
	}
	proposal, err := runtimeProposal(ctx, c, local.service, supervisor, endpoint)
	if err != nil {
		return proposal, err
	}
	if err = protectProposal(supervisor, proposal); err != nil {
		return proposal, err
	}
	// This record contains no private keys.
	return proposal, json.NewEncoder(output).Encode(struct {
		Stage       string `json:"stage"`
		Proposal    string `json:"proposal"`
		Publication any    `json:"publication"`
	}{"AWAITING_OPERATOR_ENROLLMENT", filepath.Join(c.OwnerDirectory, "proposal.json"), nil})
}

// admitOperatorEnrollment waits for the staged signed handoff, binds it to the
// exact proposal, verifies all signatures and measures the actual guest.
func admitOperatorEnrollment(ctx context.Context, c RuntimeConfig, supervisor *Supervisor, proposal viewcontrol.Enrollment) (runtimeAdmitted, error) {
	var r runtimeAdmitted
	handoff, err := waitRuntimeEnrollment(ctx, c.EnrollmentFile)
	if err != nil {
		return r, err
	}
	r.handoff = handoff
	r.proposal, err = bindRuntimeProposal(proposal, handoff.Authorization.Enrollment)
	if err != nil {
		return r, err
	}
	if _, err = guestAuthorization(handoff.Authorization, c.Operator); err != nil {
		return r, err
	}
	r.scopes = make([]guestbridge.Context, len(r.proposal.Domains))
	for i, d := range r.proposal.Domains {
		r.scopes[i] = d.Scope
	}
	r.admitted, err = ObserveAdmission(ctx, supervisor, handoff.Observer, r.scopes)
	if err != nil {
		return r, err
	}
	r.attestor, err = newKernelAttestor(r.admitted, handoff.Authorization, handoff.Provision, c.Operator, c.Budget.Guest)
	return r, err
}

// bindGuestSessions supplies the exact scopes to the C backend, emits the inert
// guest-bind stage and waits for every funded protected HELLO, then refreshes
// actual measurements and Facts once more before readiness may be claimed.
func bindGuestSessions(ctx context.Context, supervisor *Supervisor, assembly *Assembly, r runtimeAdmitted, control <-chan error, output io.Writer) error {
	if err := supervisor.supplyBootstrap(ctx, r.scopes, guestbridge.TerminalProtocol()); err != nil {
		return err
	}
	if err := supervisor.waitBootstrap(ctx); err != nil {
		select {
		case readerErr := <-control:
			return fmt.Errorf("controlled C bootstrap: %v; management: %w", err, readerErr)
		default:
		}
		return fmt.Errorf("controlled C bootstrap: %w", err)
	}
	var provision guestProvisionArtifact
	if err := json.Unmarshal(r.handoff.Provision, &provision); err != nil {
		return err
	}
	provisionPayload, err := canonicalJSON(provision.Payload)
	if err != nil {
		return err
	}
	if err = json.NewEncoder(output).Encode(struct {
		Stage         string `json:"stage"`
		Controller    string `json:"controller"`
		ProvisionHash string `json:"provision_hash"`
		Publication   any    `json:"publication"`
	}{"AWAITING_GUEST_BIND", supervisor.record.Controller.String(), hashBytes(provisionPayload), nil}); err != nil {
		return err
	}
	if err = waitRuntimeSessions(ctx, assembly.Transport, supervisor.record.Controller, r.scopes); err != nil {
		return err
	}
	// Fresh measurement and source/enrollment checks after all real C devices
	// have claimed their exact session over the one inherited native endpoint.
	if err = r.admitted.Refresh(ctx); err != nil {
		return err
	}
	return assembly.Facts.CheckEnrollment(ctx, r.proposal)
}

// serveReady opens the private lifecycle endpoint over the actual READY owners,
// announces READY with its server-issued binding and waits for owner loss,
// endpoint loss or cancellation (awaitRuntimeEnd). The endpoint is closed (worker joined) before admission is
// blocked, so no lifecycle command races shutdown. No session retirement or
// replacement-native recovery is inferred; obligations remain for inspection.
func serveReady(ctx context.Context, c RuntimeConfig, platform runtimeOS, s *Supervisor, local *runtimeLocal, a *Assembly, admitted *Admission, control <-chan error, output io.Writer) (retErr error) {
	lifecycle, err := newRuntimeLifecycle(s, local.service, a, []*Admission{admitted}, platform.replace, c.Views.SourceBytes)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, lifecycle.close()) }()
	server, err := listenLifecycle(ctx, c.OwnerDirectory, lifecycle, platform.lifecycleBoundary())
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, server.close()) }()
	runtime, cut, epoch, _ := lifecycle.binding()
	if err = json.NewEncoder(output).Encode(struct {
		Stage       string           `json:"stage"`
		Profile     string           `json:"profile"`
		Domains     int              `json:"domains"`
		Publication any              `json:"publication"`
		Control     lifecycleBinding `json:"control"`
	}{"READY", "CONTROLLED_LOCAL", len(lifecycle.sessions), nil, lifecycleBinding{server.path, runtime.String(), cut.String(), epoch}}); err != nil {
		return err
	}
	return awaitRuntimeEnd(ctx, server, lifecycle, control)
}

// lifecycleBoundary returns the endpoint's OS boundaries, substituting the
// native kernel peer-credential and accept calls for unset test hooks.
func (p runtimeOS) lifecycleBoundary() lifecycleOS {
	boundary := lifecycleOS{peer: p.peer, accept: p.accept}
	if boundary.peer == nil {
		boundary.peer = admin.PeerCredentials
	}
	if boundary.accept == nil {
		boundary.accept = (*net.UnixListener).AcceptUnix
	}
	return boundary
}

// awaitRuntimeEnd waits for native owner loss, loss of the lifecycle endpoint
// or cancellation. Every path joins the endpoint worker before blocking
// admission. A permanent accept failure ended the only control worker; the
// runtime then ends as on owner loss, retaining every owner, and returns that
// failure (server.close reports it) instead of serving uncontrollably.
func awaitRuntimeEnd(ctx context.Context, server *lifecycleServer, lifecycle *runtimeLifecycle, control <-chan error) error {
	select {
	case err := <-control:
		closeErr := server.close()
		lifecycle.blockAdmission("controlled-native-owner-lost")
		if lifecycle.fenced() {
			err = fmt.Errorf("controlled C9 restart fenced the native owner: %w", err)
		}
		return errors.Join(err, closeErr)
	case <-server.done:
		closeErr := server.close()
		lifecycle.blockAdmission("controlled-lifecycle-endpoint-lost")
		return closeErr
	case <-ctx.Done():
		closeErr := server.close()
		lifecycle.blockAdmission("controlled-runtime-stopping-retained")
		return closeErr
	}
}
