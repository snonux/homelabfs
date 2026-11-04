//go:build linux || freebsd

package controlledruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/guestbridge"
	"golang.org/x/sys/unix"
)

const physicalContract = "homelabfs-stop-adoption/1"

//go:embed probe/installed.py
var installedProbe string

// GuestObserver pins the operator's trusted guest-root channel and installed
// artifact manifest. Observe executes the fixed read-only probe, never accepts
// a caller's claimed observation. The manifest describes every loaded module.
type GuestObserver struct {
	SSH, KnownHosts, Identity             string
	SSHHash, KnownHostsHash, ManifestHash [32]byte
	Host                                  string
}

type kernelAdmission struct {
	Version          string                          `json:"version"`
	BootID           string                          `json:"boot_id"`
	ProviderNonce    string                          `json:"provider_nonce"`
	PhysicalContract string                          `json:"physical_contract"`
	Protocols        map[string]guestbridge.Protocol `json:"domain_protocols"`
	Domains          []guestbridge.Context           `json:"domains"`
	Observations     []kernelDomainObservation       `json:"observations"`
}
type kernelDomainObservation struct {
	Scope               guestbridge.Context           `json:"scope"`
	Revision            uint64                        `json:"revision"`
	Fence               uint64                        `json:"fence"`
	Phase               string                        `json:"phase"`
	EventHash           string                        `json:"event_hash"`
	AcknowledgementHash string                        `json:"acknowledgement_hash"`
	Terminal            *guestbridge.TerminalSnapshot `json:"terminal,omitempty"`
}

type installedObservation struct {
	Version       string            `json:"version"`
	ManifestHash  string            `json:"manifest_hash"`
	BootID        string            `json:"boot_id"`
	KernelBuildID string            `json:"kernel_build_id"`
	Modules       map[string]string `json:"modules"`
	ABI           string            `json:"abi"`
	Admission     kernelAdmission   `json:"admission"`
}

// Admission owns one immutable admitted boot/provider/domain inventory. Refresh
// requires the same complete observation; change or uncertainty fences it. The
// protected file is evidence only: reopening cannot restore live authority.
type Admission struct {
	mu         sync.Mutex
	supervisor *Supervisor
	observer   GuestObserver
	installed  installedObservation
	endpoint   guestbridge.Endpoint
	process    fsapi.UUID
	refreshed  time.Time
	fenced     bool
	stopped    map[string]uint64
}

// ObserveAdmission measures an actual running guest through the pinned SSH
// executable/known-host channel, then protects its exact binding to the live
// supervisor exporter and endpoint. Missing kernel sysfs support refuses.
func ObserveAdmission(ctx context.Context, s *Supervisor, observer GuestObserver, domains []guestbridge.Context) (*Admission, error) {
	if s == nil || len(domains) == 0 || len(domains) > 64 {
		return nil, fsapi.Invalid
	}
	s.mu.Lock()
	if err := s.checkLive(); err != nil {
		s.mu.Unlock()
		return nil, err
	}
	endpoint, process := s.record.Endpoint, s.record.ProcessID
	s.mu.Unlock()
	observed, err := observer.observe(ctx)
	if err != nil {
		return nil, err
	}
	a := &Admission{supervisor: s, observer: observer, endpoint: endpoint, process: process, stopped: map[string]uint64{}}
	if err = a.accept(observed, domains); err != nil {
		return nil, err
	}
	for _, state := range observed.Admission.Observations {
		if state.Phase != "LIVE" || state.Revision != 1 || state.Fence != 1 {
			return nil, fsapi.Unavailable
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.Endpoint != endpoint || s.record.ProcessID != process || s.checkLive() != nil {
		return nil, fsapi.Stale
	}
	raw, err := json.Marshal(struct {
		Endpoint    guestbridge.Endpoint
		Process     fsapi.UUID
		Observation installedObservation
	}{endpoint, process, observed})
	if err != nil {
		return nil, err
	}
	f, err := ownedFile(s.directory, "guest-admission.json", unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL, 0600)
	if err != nil {
		return nil, err
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
	if err != nil {
		return nil, err
	}
	a.installed, a.refreshed = observed, time.Now()
	return a, nil
}

// Refresh performs read-only guest I/O before entering any controller or service
// lock. A changed boot, module, inventory or native owner permanently fences it;
// no time limit or disconnect retires its protected obligations.
func (a *Admission) Refresh(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.fenced {
		return fsapi.Unavailable
	}
	observed, err := a.observer.observePhase(ctx)
	if err == nil {
		err = a.accept(observed, a.installed.Admission.Domains)
	}
	oldIdentity, newIdentity := a.installed, observed
	oldIdentity.Admission.Observations, newIdentity.Admission.Observations = nil, nil
	if err != nil || !sameJSON(newIdentity, oldIdentity) {
		a.fenced = true
		if err != nil {
			return err
		}
		return fsapi.Stale
	}
	if err = a.advance(observed); err != nil {
		a.fenced = true
		return err
	}
	if err = a.native(); err != nil {
		a.fenced = true
		return err
	}
	a.installed, a.refreshed = observed, time.Now()
	return nil
}

func (o GuestObserver) observe(ctx context.Context) (installedObservation, error) {
	return o.runProbe(ctx, "")
}
func (o GuestObserver) observePhase(ctx context.Context) (installedObservation, error) {
	return o.runProbe(ctx, " phase")
}
func (o GuestObserver) runProbe(ctx context.Context, phaseArgument string) (installedObservation, error) {
	var result installedObservation
	if !validSSHHost(o.Host) || o.ManifestHash == ([32]byte{}) || !filepath.IsAbs(o.Identity) {
		return result, fsapi.Invalid
	}
	ssh, err := pinnedRead(o.SSH, o.SSHHash, 64<<20, false)
	if err != nil {
		return result, err
	}
	defer func() { _ = ssh.Close() }()
	hosts, err := pinnedRead(o.KnownHosts, o.KnownHostsHash, 1<<20, true)
	if err != nil {
		return result, err
	}
	defer func() { _ = hosts.Close() }()
	key, err := os.OpenFile(o.Identity, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return result, err
	}
	defer func() { _ = key.Close() }()
	if err = checkOwned(key, false); err != nil {
		return result, err
	}
	// OpenSSH closes inherited descriptors during startup. Copy the pinned
	// host/key bytes into one private bounded run directory instead of relying
	// on /dev/fd paths that would silently stop pinning after closefrom().
	stage, err := os.MkdirTemp("/tmp", "hfs-observe-")
	if err != nil {
		return result, err
	}
	defer func() { _ = os.RemoveAll(stage) }()
	for name, source := range map[string]*os.File{"known_hosts": hosts, "identity": key} {
		data, err := io.ReadAll(io.LimitReader(source, (1<<20)+1))
		if err != nil {
			return result, err
		}
		if len(data) > 1<<20 {
			return result, fsapi.NoSpace
		}
		if err = os.WriteFile(filepath.Join(stage, name), data, 0600); err != nil {
			return result, err
		}
	}
	args := []string{"-F", "/dev/null", "-T", "-oBatchMode=yes", "-oStrictHostKeyChecking=yes", "-oGlobalKnownHostsFile=/dev/null", "-oUserKnownHostsFile=" + filepath.Join(stage, "known_hosts"), "-oIdentitiesOnly=yes", "-oIdentityFile=" + filepath.Join(stage, "identity"), "-oConnectTimeout=5", "-oServerAliveInterval=2", "-oServerAliveCountMax=2", "-oClearAllForwardings=yes"}

	args = append(args, "--", "root@"+o.Host, "python3 -I -c '"+strings.ReplaceAll(installedProbe, "'", "'\\''")+"'"+phaseArgument)
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(bounded, "/dev/fd/3", args...)
	command.ExtraFiles = []*os.File{ssh}
	command.Env = []string{"PATH=/usr/bin:/bin", "LC_ALL=C"}
	command.WaitDelay = time.Second
	var output boundedProbeOutput
	command.Stdout = &output
	command.Stderr = io.Discard
	if err = command.Run(); err != nil {
		return result, fmt.Errorf("installed guest observation: %w", err)
	}
	if err = strictJSON(output.Bytes(), &result); err != nil {
		return result, err
	}
	if result.ManifestHash != hex.EncodeToString(o.ManifestHash[:]) {
		return result, fsapi.Permission
	}
	return result, nil
}

type boundedProbeOutput struct{ bytes.Buffer }

func (b *boundedProbeOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 256<<10 {
		return 0, fsapi.NoSpace
	}
	return b.Buffer.Write(p)
}

func (a *Admission) accept(o installedObservation, domains []guestbridge.Context) error {
	if o.Version != "homelabfs-installed-observation/1" || o.Admission.Version != "homelabfs-kernel-admission/1" || o.ABI != physicalContract || o.Admission.PhysicalContract != physicalContract || o.BootID != o.Admission.BootID || !validUUIDString(o.BootID) || !validUUIDString(o.Admission.ProviderNonce) || !validBuildID(o.KernelBuildID) || len(o.Modules) > 256 || len(domains) != len(o.Admission.Domains) || len(domains) != len(o.Admission.Observations) || len(domains) != len(o.Admission.Protocols) {
		return fsapi.Permission
	}
	for _, id := range o.Modules {
		if !validBuildID(id) {
			return fsapi.Permission
		}
	}
	seen := map[string]bool{}
	for _, scope := range o.Admission.Domains {
		if !runtimeProtocol(o.Admission.Protocols[scope.Session]) || seen[scope.Session] || scope.GuestBoot != o.BootID || scope.FuseSession != a.endpoint.FuseSession || scope.ExporterIncarnation != a.process.String() {
			return fsapi.Permission
		}
		found := false
		for _, expected := range domains {
			if expected == scope {
				found = true
			}
		}
		if !found {
			return fsapi.Permission
		}
		seen[scope.Session] = true
	}
	seen = map[string]bool{}
	for _, observation := range o.Admission.Observations {
		if seen[observation.Scope.Session] || observation.Revision == 0 || observation.Fence == 0 {
			return fsapi.Permission
		}
		found := false
		for _, scope := range domains {
			if observation.Scope == scope {
				found = true
			}
		}
		if !found {
			return fsapi.Permission
		}
		if observation.Terminal != nil {
			protocol := o.Admission.Protocols[observation.Scope.Session]
			if protocol != guestbridge.TerminalProtocol() || observation.Terminal.Validate() != nil || observation.Terminal.Scope != observation.Scope || observation.Terminal.Revision != observation.Revision || (observation.Phase != "TERMINAL_PENDING" && observation.Phase != "TERMINAL_RETIRED") {
				return fsapi.Permission
			}
		} else if observation.Phase == "TERMINAL_PENDING" || observation.Phase == "TERMINAL_RETIRED" {
			return fsapi.Permission
		}
		switch observation.Phase {
		case "TERMINAL_PENDING", "TERMINAL_RETIRED":
			if (observation.EventHash == "") != (observation.AcknowledgementHash == "") {
				return fsapi.Permission
			}
		case "LIVE":
			if observation.EventHash != "" || observation.AcknowledgementHash != "" {
				return fsapi.Permission
			}
		case "STOPPED", "INSTALLED_DRAIN", "DRAINED", "INSTALLED_FINAL", "REOPENED":
			if len(observation.EventHash) != 64 || len(observation.AcknowledgementHash) != 64 || !isHex(observation.EventHash) || !isHex(observation.AcknowledgementHash) {
				return fsapi.Permission
			}
		default:
			return fsapi.Permission
		}
		seen[observation.Scope.Session] = true
	}
	return nil
}

func (a *Admission) native() error {
	s := a.supervisor
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.record.Endpoint != a.endpoint || s.record.ProcessID != a.process {
		return fsapi.Stale
	}
	return s.checkLive()
}

func (a *Admission) check(scope guestbridge.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.fenced || time.Since(a.refreshed) > 15*time.Second {
		return fsapi.Unavailable
	}
	if err := a.native(); err != nil {
		return err
	}
	for _, enrolled := range a.installed.Admission.Domains {
		if enrolled == scope {
			return nil
		}
	}
	return fsapi.Permission
}

func pinnedRead(path string, want [32]byte, maximum int64, private bool) (*os.File, error) {
	if !filepath.IsAbs(path) || want == ([32]byte{}) {
		return nil, fsapi.Invalid
	}
	f, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	stat, err := f.Stat()
	if err == nil && (!stat.Mode().IsRegular() || stat.Mode().Perm()&0022 != 0 || stat.Size() > maximum) {
		err = fsapi.Permission
	}
	if err == nil && private {
		err = checkOwned(f, false)
	}
	if err == nil {
		h := sha256.New()
		_, err = io.Copy(h, io.LimitReader(f, maximum+1))
		if err == nil && !bytes.Equal(h.Sum(nil), want[:]) {
			err = fsapi.Permission
		}
	}
	if err == nil {
		_, err = f.Seek(0, io.SeekStart)
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func strictJSON(raw []byte, into any) error {
	if _, err := guestbridge.Canonical(raw); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(into); err != nil {
		return err
	}
	return nil
}

func validSSHHost(host string) bool {
	return regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.-]{0,252}$`).MatchString(host)
}
func validBuildID(id string) bool {
	return len(id) >= 16 && len(id) <= 128 && len(id)%2 == 0 && isHex(id)
}
func isHex(id string) bool {
	b, err := hex.DecodeString(id)
	return err == nil && hex.EncodeToString(b) == id
}
func validUUIDString(s string) bool { _, err := fsapi.ParseUUID(s); return err == nil }
func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return bytes.Equal(x, y)
}

// advance retains a bounded high-water for each original domain. A measured
// old-revision reopen cannot validate universal unadopted exclusion.
func (a *Admission) advance(next installedObservation) error {
	for _, state := range next.Admission.Observations {
		var prior kernelDomainObservation
		for _, old := range a.installed.Admission.Observations {
			if old.Scope == state.Scope {
				prior = old
			}
		}
		if prior.Terminal != nil {
			if state.Terminal == nil || !sameJSON(prior.Terminal, state.Terminal) || state.Phase != "TERMINAL_PENDING" && state.Phase != "TERMINAL_RETIRED" || prior.Phase == "TERMINAL_RETIRED" && state.Phase != "TERMINAL_RETIRED" {
				return fsapi.Stale
			}
		}
		if prior.Revision == 0 || state.Revision < prior.Revision || state.Fence < prior.Fence {
			return fsapi.Stale
		}
		stopped := a.stopped[state.Scope.Session]
		if state.Phase == "STOPPED" {
			if a.stopped == nil {
				a.stopped = map[string]uint64{}
			}
			a.stopped[state.Scope.Session] = state.Revision
		}
		if state.Phase == "REOPENED" && (stopped == 0 || state.Revision <= stopped) {
			return fsapi.Stale
		}
		if state.Phase == "LIVE" && stopped != 0 && state.Revision <= stopped {
			return fsapi.Stale
		}
	}
	return nil
}
