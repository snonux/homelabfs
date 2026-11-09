//go:build linux || freebsd

// Package runtimeprep prepares the inert inputs of one disposable controlled
// runtime run (zq3 gap G2): the controlled-runtime/1 configuration, fresh
// per-session host/guest Ed25519 receipt keys, the volume-init plan consumed by
// the separate source-matching G3 helper, and the request templates that the
// external signer (hfs-retained9p-operator) consumes once the runtime has
// emitted its actual proposal and controller.
//
// It never opens the operator private key (only its path is recorded for the
// signer), never contacts a host, starts a runtime, initialises a volume or
// signs anything. Its outputs carry no native, guest or readiness authority.
package runtimeprep

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/snonux/homelabfs/internal/controlledruntime"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/viewcontrol"
)

const (
	// SpecVersion names the closed operator spec schema read by Generate.
	SpecVersion = "homelabfs-runtime-prep-spec/1"
	// TemplateVersion names the emitted request template schema.
	TemplateVersion = "homelabfs-runtime-request-template/1"
	// VolumeInitVersion names the plan consumed by tools/lab/runtime-volume-init.py.
	VolumeInitVersion = "homelabfs-runtime-volume-init/1"
	// RecordVersion names the emitted generation record.
	RecordVersion = "homelabfs-runtime-prep-record/1"
	// ObserverVersion names the closed guest observer input of enroll-request.
	ObserverVersion = "homelabfs-runtime-observer-input/1"

	// sessionCount is fixed by the external signer's base gate, which accepts
	// exactly two original guest domains (one VM, two independent mounts).
	sessionCount = 2
	// storageFileBytes is the storage.Limits.FileBytes default that daemoncli
	// leaves in place; the view controller charges 8 of them at enrollment.
	storageFileBytes = 16 << 20
	// socketPathLimit is sockaddr_un on FreeBSD (104 bytes including NUL).
	socketPathLimit = 103
)

var runIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,23}$`)

// Spec is the closed operator input. Every capacity is explicit: like the
// runtime itself, the generator selects no defaults for budgets or limits.
// Paths name the FreeBSD runtime host (f3), except OperatorKey, which names
// the signer host and is recorded only, never opened.
type Spec struct {
	Version                                                 string
	RunID                                                   string
	Volume                                                  VolumeLayout
	OwnerDirectory, EnrollmentFile                          string
	Exporter                                                ExporterSpec
	InitHelper                                              PinnedExecutable
	OperatorPublicKey, OperatorKey                          string
	Views                                                   viewcontrol.Bounds
	Service                                                 fsservice.Limits
	Providers                                               controlledruntime.ProviderLimits
	Capabilities, CapabilityIdentities, CredentialRevisions int
	Budget                                                  controlledruntime.RuntimeBudget
	BootstrapSeconds                                        int
}

// VolumeLayout names the daemoncli volume configuration file and its three
// administrative directories, all created later by the G3 helper on the host.
type VolumeLayout struct {
	Config, ControlDirectory, VolumeDirectory, Mountpoint string
}

// ExporterSpec pins the staged exporter (patched bhyve) and its argv template.
// Each controlled 9p device names its session as the placeholder
// "{{session:N}}"; Generate substitutes the fresh session UUIDs.
type ExporterSpec struct {
	Path, SHA256 string
	Args         []string
}

// PinnedExecutable is an absolute host path plus its expected SHA256 (hex).
type PinnedExecutable struct {
	Path, SHA256 string
}

// validateSpec checks generator-owned invariants, including the guest slot
// and byte floors (guestfloor.go). Runtime-owned config rules are checked separately by
// the actual runtime validation (see validate_*.go).
func validateSpec(s Spec) (ed25519.PublicKey, error) {
	if s.Version != SpecVersion || !runIDPattern.MatchString(s.RunID) {
		return nil, fmt.Errorf("spec requires Version %q and a run ID matching %s", SpecVersion, runIDPattern)
	}
	operator, err := decodeHex(s.OperatorPublicKey, ed25519.PublicKeySize, "OperatorPublicKey")
	if err != nil {
		return nil, err
	}
	if !cleanAbsolute(s.OperatorKey) {
		return nil, fmt.Errorf("OperatorKey must be the clean absolute signer-host path of the operator key")
	}
	if err := validatePaths(s); err != nil {
		return nil, err
	}
	for _, pin := range []PinnedExecutable{{s.Exporter.Path, s.Exporter.SHA256}, s.InitHelper} {
		if _, err := decodeHex(pin.SHA256, 32, pin.Path+" SHA256"); err != nil {
			return nil, err
		}
	}
	if err := checkControllerWork(s.Views); err != nil {
		return nil, err
	}
	// The runtime accepts any guest count above zero; the guest provider
	// refuses one below its slot floor only once the signed provision reaches
	// it, and a too-small byte budget only at mount (zq3-r3 failed there).
	// Refuse both here first.
	if err := checkGuestFloor(s.Budget.Guest, sessionCount); err != nil {
		return nil, err
	}
	return ed25519.PublicKey(operator), nil
}

// validatePaths keeps every runtime/volume path clean, absolute and disjoint,
// mirroring the runtime's own overlap refusals so a bad layout fails before
// any host step instead of at runtime start.
func validatePaths(s Spec) error {
	roots := []string{s.Volume.Config, s.Volume.ControlDirectory, s.Volume.VolumeDirectory,
		s.Volume.Mountpoint, s.OwnerDirectory, s.EnrollmentFile}
	for i, p := range roots {
		if !cleanAbsolute(p) {
			return fmt.Errorf("runtime path must be clean and absolute below /: %q", p)
		}
		for _, other := range roots[:i] {
			if nested(p, other) {
				return fmt.Errorf("runtime paths overlap: %s and %s", p, other)
			}
		}
	}
	for _, executable := range []string{s.Exporter.Path, s.InitHelper.Path} {
		if !cleanAbsolute(executable) {
			return fmt.Errorf("executable path must be clean and absolute: %q", executable)
		}
		for _, root := range roots {
			if nested(executable, root) {
				return fmt.Errorf("executable %s lies inside runtime path %s", executable, root)
			}
		}
	}
	// The runtime refuses this too, but only once started on the host.
	if len(filepath.Join(s.OwnerDirectory, controlledruntime.LifecycleSocketName)) > socketPathLimit {
		return fmt.Errorf("OwnerDirectory too long for the private lifecycle socket (sockaddr_un)")
	}
	return nil
}

// checkControllerWork repeats the view controller's enrollment-time work check
// (WorkBytes >= 3*Ledger + 6*Source + 8*metadata, metadata being the 16 MiB
// storage file bound). The runtime applies it only after the operator signed
// the enrollment, i.e. inside the bootstrap window, so it is caught here first.
func checkControllerWork(b viewcontrol.Bounds) error {
	needed := 3*int64(b.LedgerBytes) + 6*b.SourceBytes + 8*int64(storageFileBytes)
	if b.WorkBytes < needed {
		return fmt.Errorf("Views.WorkBytes %d below the controller enrollment requirement %d", b.WorkBytes, needed)
	}
	return nil
}

func nested(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func decodeHex(text string, size int, name string) ([]byte, error) {
	raw, err := hex.DecodeString(text)
	if err != nil || len(raw) != size || hex.EncodeToString(raw) != text {
		return nil, fmt.Errorf("%s must be %d bytes of lowercase hex", name, size)
	}
	return raw, nil
}
