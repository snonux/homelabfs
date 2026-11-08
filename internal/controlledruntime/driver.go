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
	"runtime"
	"time"

	"github.com/snonux/homelabfs/internal/admin"
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
	descriptors := f.Bool("check-descriptor-exec", false, "read-only descriptor execution prerequisite report; no child or runtime startup")
	config := f.String("config", "", "private controlled-runtime/1 config")
	disposable := f.Bool("disposable-validation", false, "explicit controlled local runtime; publication remains null")
	client := lifecycleClientFlags(f)
	if err := f.Parse(args); err != nil {
		return err
	}
	if client.socket != "" {
		return runLifecycleClient(ctx, f, client, stdout)
	}
	if *descriptors {
		if f.NFlag() != 1 || f.NArg() != 0 {
			return fmt.Errorf("--check-descriptor-exec must be used alone")
		}
		return reportDescriptorExec(stdout)
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
	if _, err := checkDescriptorExec(); err != nil {
		return err
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
	// replace is the native FUSE shell-dispatch switch used by C9 Activate;
	// nil leaves the private RESTART operation unsupported before any effect.
	replace shellSwitch
	// peer reads kernel peer credentials for the private lifecycle endpoint.
	peer peerCredentials
	// accept is the lifecycle listener's accept call; nil uses AcceptUnix.
	accept acceptUnix
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
		bind:    func(ctx context.Context, s *Supervisor, t *guestbridge.Transport) error { return s.Bind(ctx, t) },
		replace: (*fuseadapter.Shell).ReplaceEndpoint,
		peer:    admin.PeerCredentials,
	}
}
func runRuntime(ctx context.Context, c RuntimeConfig, output io.Writer) error {
	return runRuntimeWithOS(ctx, c, output, nativeRuntimeOS())
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
		Budget      RuntimeBudget
		Providers   ProviderLimits
		Service     fsservice.Limits
		Views       viewcontrol.Bounds
		Domains     int
		Diagnostics diagnosticBudget
		Control     lifecycleBudget
	}{c.Budget, c.Providers, c.Service, c.Views, len(c.Scopes), fixedDiagnosticBudget(), fixedLifecycleBudget(c.Views)})
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
