//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/snonux/homelabfs/internal/controlledvalidation"
	"github.com/snonux/homelabfs/internal/daemoncli"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/fuseadapter"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

// Run is an explicit foreground disposable runtime. Linux refuses native startup;
// the local assembly tests have separate, unexported OS primitive substitutions.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	f := flag.NewFlagSet("hfs-controlled-runtime", flag.ContinueOnError)
	f.SetOutput(stderr)
	qualify := f.Bool("check-process-observer", false, "read-only native process observer qualification; no runtime startup")
	config := f.String("config", "", "private controlled-runtime/1 config")
	disposable := f.Bool("disposable-validation", false, "explicit controlled local runtime; publication remains null")
	if err := f.Parse(args); err != nil {
		return err
	}
	if *qualify {
		if f.NFlag() != 1 || f.NArg() != 0 {
			return fmt.Errorf("--check-process-observer must be used alone")
		}
		return qualifyProcessObserver(stdout)
	}
	if !*disposable || *config == "" || f.NArg() != 0 {
		return fmt.Errorf("requires --config FILE --disposable-validation")
	}
	var c RuntimeConfig
	if err := readRuntimeFile(*config, &c); err != nil {
		return err
	}
	if err := validateRuntimeConfig(ctx, c); err != nil {
		return err
	}
	if runtime.GOOS != "freebsd" {
		return fmt.Errorf("native controlled runtime requires actual FreeBSD FUSE: %w", fsapi.Unsupported)
	}
	if _, err := checkProcessObserver(observeProcess); err != nil {
		return fmt.Errorf("process observer preflight: %w", err)
	}
	return runRuntime(ctx, c, stdout)
}

type runtimeLocal struct {
	service    *fsservice.Service
	mountpoint string
	close      func() error
}
type runtimeOS struct {
	open   func(context.Context, RuntimeConfig) (*runtimeLocal, error)
	create func(context.Context, string) (*Supervisor, error)
	start  func(context.Context, *Supervisor, Executable) error
	mount  func(context.Context, *Supervisor, string, *fuseadapter.Shell) (guestbridge.Endpoint, error)
	bind   func(context.Context, *Supervisor, *guestbridge.Transport) error
}

func nativeRuntimeOS() runtimeOS {
	return runtimeOS{
		open: func(ctx context.Context, c RuntimeConfig) (*runtimeLocal, error) {
			l, err := daemoncli.OpenControlled(ctx, c.VolumeConfig, c.Service)
			if err != nil {
				return nil, err
			}
			if err = l.CheckRuntimePaths(c.OwnerDirectory, c.EnrollmentFile); err != nil {
				_ = l.Close()
				return nil, err
			}
			return &runtimeLocal{l.Service(), l.Mountpoint(), l.Close}, nil
		},
		create: Create,
		start:  func(ctx context.Context, s *Supervisor, e Executable) error { return s.Start(ctx, e) },
		mount: func(ctx context.Context, s *Supervisor, path string, shell *fuseadapter.Shell) (guestbridge.Endpoint, error) {
			return s.MountShell(ctx, path, shell)
		},
		bind: func(ctx context.Context, s *Supervisor, t *guestbridge.Transport) error { return s.Bind(ctx, t) },
	}
}
func runRuntime(ctx context.Context, c RuntimeConfig, output io.Writer) error {
	return runRuntimeWithOS(ctx, c, output, nativeRuntimeOS())
}

func runRuntimeWithOS(ctx context.Context, c RuntimeConfig, output io.Writer, platform runtimeOS) (retErr error) {
	if err := validateRuntimeConfig(ctx, c); err != nil {
		return err
	}
	funding, err := fundRuntime(c.Budget, c.Providers, c.Views, len(c.Scopes))
	if err != nil {
		return err
	}
	attached := false
	defer func() {
		if !attached {
			retErr = errors.Join(retErr, funding.owner.Release())
		}
	}()
	local, err := platform.open(ctx, c)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, local.close()) }()
	supervisor, err := platform.create(ctx, c.OwnerDirectory)
	if err != nil {
		return err
	}
	defer func() { retErr = errors.Join(retErr, supervisor.Close()) }()
	if err = protectRuntimeBudget(supervisor, c); err != nil {
		return err
	}
	supervisor.runtimeFunding = funding
	attached = true
	c.Exporter.ControlledBootstrap = true
	if err = platform.start(ctx, supervisor, c.Exporter); err != nil {
		return err
	}
	shell, err := fuseadapter.NewShell(100, 101, 102, 103)
	if err != nil {
		return err
	}
	endpoint, err := platform.mount(ctx, supervisor, local.mountpoint, shell)
	if err != nil {
		return err
	}
	proposal, err := runtimeProposal(ctx, c, local.service, supervisor, endpoint)
	if err != nil {
		return err
	}
	if err = protectProposal(supervisor, proposal); err != nil {
		return err
	}
	// This record is deliberately not readiness and contains no private keys.
	if err = json.NewEncoder(output).Encode(struct {
		Stage       string `json:"stage"`
		Proposal    string `json:"proposal"`
		Publication any    `json:"publication"`
	}{"AWAITING_OPERATOR_ENROLLMENT", filepath.Join(c.OwnerDirectory, "proposal.json"), nil}); err != nil {
		return err
	}
	bootstrap, cancel := context.WithTimeout(ctx, time.Duration(c.BootstrapSeconds)*time.Second)
	defer cancel()
	handoff, err := waitRuntimeEnrollment(bootstrap, c.EnrollmentFile)
	if err != nil {
		return err
	}
	proposal, err = bindRuntimeProposal(proposal, handoff.Authorization.Enrollment)
	if err != nil {
		return err
	}
	if _, err = guestAuthorization(handoff.Authorization, c.Operator); err != nil {
		return err
	}
	scopes := make([]guestbridge.Context, len(proposal.Domains))
	for i, d := range proposal.Domains {
		scopes[i] = d.Scope
	}
	admitted, err := ObserveAdmission(bootstrap, supervisor, handoff.Observer, scopes)
	if err != nil {
		return err
	}
	attestor, err := newKernelAttestor(admitted, handoff.Authorization, handoff.Provision, c.Operator, c.Budget.Guest)
	if err != nil {
		return err
	}
	assembly, err := Assemble(bootstrap, AssemblyConfig{Directory: c.OwnerDirectory, Service: local.service, Supervisor: supervisor, Admissions: []*Admission{admitted}, Authorization: handoff.Authorization, Providers: handoff.Providers, Operator: c.Operator, HostKeys: c.HostKeys, Attestor: attestor, ProviderLimits: c.Providers, Capabilities: c.Capabilities, CapabilityIdentities: c.CapabilityIdentities, CredentialRevisions: c.CredentialRevisions, Budget: funding.transport})
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
	if err = supervisor.supplyBootstrap(bootstrap, scopes, guestbridge.TerminalProtocol()); err != nil {
		return err
	}
	if err = supervisor.waitBootstrap(bootstrap); err != nil {
		select {
		case readerErr := <-control:
			return fmt.Errorf("controlled C bootstrap: %v; management: %w", err, readerErr)
		default:
		}
		return fmt.Errorf("controlled C bootstrap: %w", err)
	}
	var provision guestProvisionArtifact
	if err = json.Unmarshal(handoff.Provision, &provision); err != nil {
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
	if err = waitRuntimeSessions(bootstrap, assembly.Transport, supervisor.record.Controller, scopes); err != nil {
		return err
	}
	// Fresh measurement and source/enrollment checks after all real C devices
	// have claimed their exact session over the one inherited native endpoint.
	if err = admitted.Refresh(bootstrap); err != nil {
		return err
	}
	if err = assembly.Facts.CheckEnrollment(bootstrap, proposal); err != nil {
		return err
	}
	if err = json.NewEncoder(output).Encode(struct {
		Stage       string `json:"stage"`
		Profile     string `json:"profile"`
		Domains     int    `json:"domains"`
		Publication any    `json:"publication"`
	}{"READY", "CONTROLLED_LOCAL", len(scopes), nil}); err != nil {
		return err
	}
	select {
	case err = <-control:
		local.service.BlockAdmission("controlled-native-owner-lost")
		return err
	case <-ctx.Done():
		local.service.BlockAdmission("controlled-runtime-stopping-retained")
		// No session retirement or replacement-native recovery is inferred. The
		// unpublished mount/guest obligations remain for explicit inspection.
		return nil
	}
}

func runtimeProposal(ctx context.Context, c RuntimeConfig, service *fsservice.Service, s *Supervisor, endpoint guestbridge.Endpoint) (viewcontrol.Enrollment, error) {
	info := service.FrontendInfo()
	journal, err := service.ControlledJournalInfo(ctx, info.Validation)
	if err != nil {
		return viewcontrol.Enrollment{}, err
	}
	e := viewcontrol.Enrollment{Version: viewcontrol.EnrollmentVersion, PageSize: 4096, Bounds: c.Views}
	if _, err = rand.Read(e.ID[:]); err != nil {
		return e, err
	}
	protocol := guestbridge.TerminalProtocol()
	for _, template := range c.Scopes {
		scope := template
		if scope.Volume != info.Volume.String() {
			return e, fsapi.Permission
		}
		scope.ExporterIncarnation = s.record.ProcessID.String()
		scope.FuseSession = endpoint.FuseSession
		scope.Generation = journal.Stream.Generation.String()
		scope.Configuration = journal.Stream.Configuration.String()
		scope.OriginJournal = journal.Incarnation.String()
		var ledger fsapi.UUID
		if _, err = rand.Read(ledger[:]); err != nil {
			return e, err
		}
		e.Domains = append(e.Domains, controlledvalidation.Enrollment{Protocol: &protocol, Profile: info.Validation, Scope: scope, Endpoint: endpoint, ServingGeneration: info.Generation, Ledger: ledger, Journal: journal, HostKey: c.HostKeys[scope.Session].Public().(ed25519.PublicKey), GuestKey: c.GuestKeys[scope.Session]})
		var ids [4]fsapi.UUID
		for i := range ids {
			if _, err = rand.Read(ids[i][:]); err != nil {
				return e, err
			}
		}
		e.TicketFamilies = append(e.TicketFamilies, viewcontrol.TicketFamilies{Acquisition: viewcontrol.TicketFamily{Anchor: ids[0].String(), Nonce: ids[1].String()}, Control: viewcontrol.TicketFamily{Anchor: ids[2].String(), Nonce: ids[3].String()}})
	}
	err = service.Controlled(ctx, info.Validation, func(ctx context.Context, cut *fsservice.ControlledCut) error {
		source, err := cut.ViewSource(ctx, c.Views.SourceBytes)
		if err == nil {
			e.Baseline = source.Through
			e.BaselineStateHash = hashBytes(source.State)
		}
		return err
	})
	return e, err
}

func protectProposal(s *Supervisor, e viewcontrol.Enrollment) error {
	raw, err := json.Marshal(e)
	if err != nil {
		return err
	}
	f, err := ownedFile(s.directory, "proposal.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = s.directory.Sync()
	}
	return err
}

func waitRuntimeEnrollment(ctx context.Context, path string) (RuntimeEnrollment, error) {
	timer := time.NewTicker(25 * time.Millisecond)
	defer timer.Stop()
	for {
		var e RuntimeEnrollment
		err := readRuntimeFile(path, &e)
		if err == nil {
			return e, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			return e, err
		}
		select {
		case <-ctx.Done():
			return e, ctx.Err()
		case <-timer.C:
		}
	}
}

// Only an explicitly unknown guest boot can be filled by the external signer;
// the actual observer subsequently measures it. All other proposal bytes stay exact.
func bindRuntimeProposal(proposed, signed viewcontrol.Enrollment) (viewcontrol.Enrollment, error) {
	if len(proposed.Domains) != len(signed.Domains) || len(proposed.Domains) == 0 {
		return proposed, fsapi.Permission
	}
	proposed.Domains = append([]controlledvalidation.Enrollment(nil), proposed.Domains...)
	boot := signed.Domains[0].Scope.GuestBoot
	if !validUUIDString(boot) {
		return proposed, fsapi.Permission
	}
	for i := range proposed.Domains {
		if signed.Domains[i].Scope.GuestBoot != boot {
			return proposed, fsapi.Permission
		}
		if proposed.Domains[i].Scope.GuestBoot == "" {
			proposed.Domains[i].Scope.GuestBoot = boot
		}
	}
	if !sameJSON(proposed, signed) {
		return proposed, fmt.Errorf("signed enrollment changed exact prepared proposal: %w", fsapi.Permission)
	}
	return proposed, nil
}

func waitRuntimeSessions(ctx context.Context, t *guestbridge.Transport, controller fsapi.UUID, scopes []guestbridge.Context) error {
	timer := time.NewTicker(10 * time.Millisecond)
	defer timer.Stop()
	for {
		err := t.CheckControlledSessions(ctx, controller, scopes, guestbridge.TerminalProtocol())
		if err == nil {
			return nil
		}
		if !errors.Is(err, fsapi.WouldBlock) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func protectRuntimeBudget(s *Supervisor, c RuntimeConfig) error {
	raw, err := json.Marshal(struct {
		Budget    RuntimeBudget
		Providers ProviderLimits
		Service   fsservice.Limits
		Views     viewcontrol.Bounds
		Domains   int
	}{c.Budget, c.Providers, c.Service, c.Views, len(c.Scopes)})
	if err != nil {
		return err
	}
	f, err := ownedFile(s.directory, "runtime-budget.json", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if _, err = f.Write(raw); err == nil {
		err = f.Sync()
	}
	if e := f.Close(); err == nil {
		err = e
	}
	if err == nil {
		err = s.directory.Sync()
	}
	return err
}
