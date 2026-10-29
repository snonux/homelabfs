package labrunner

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/snonux/homelabfs/internal/admin"
)

// SupervisorConfig enrolls a foreground child in a private run control directory.
// The binary must not daemonize, fork a replacement or exec a different image.
// Daemon launch argv is reviewed as part of the pinned scenario driver.
type SupervisorConfig struct {
	Role                                  string
	TerminateIntent                       func(FaultProcess) error
	RunID, Target, Node, Volume, PlanHash string
	Directory, Executable, SHA256, Handle string
	Args                                  []string
	MaxLogBytes                           int64
}

// FaultSupervisor exclusively owns an unreaped direct child. Signal admission
// and gate closure share a mutex. Wait is called only after that gate closes;
// an exited child's zombie therefore reserves its PID throughout admission.
// No detached process or PID discovered through ps can enter this supervisor.
type FaultSupervisor struct {
	mu               sync.Mutex
	config           SupervisorConfig
	launchGeneration string
	child            *exec.Cmd
	process          FaultProcess
	admission        bool
	closed           bool
	exited           func() (bool, error)
	closeObservation func() error
	server           *admin.Server
	audit            *admin.AuditLog
	log              *os.File
	closeOnce        sync.Once
	closeErr         error
}

type supervisorLog struct {
	mu  sync.Mutex
	log boundedLog
}

// StartFaultSupervisor starts only a caller-approved enrolled foreground child.
// It does not approve a run; the pinned driver must first validate the exact
// Stage 1 approval and local guest identity. On partial failure a nonnil result
// retains its child for explicit inspection and Close, never a detached repair.
func StartFaultSupervisor(ctx context.Context, c SupervisorConfig) (*FaultSupervisor, error) {
	if c.Role != "" && c.Role != "private-etcd" {
		return nil, errors.New("unknown supervised process role")
	}
	if c.Role == "private-etcd" && (c.Node != c.Target || c.Volume != "private-etcd" || c.Handle != "etcd") {
		return nil, errors.New("private etcd process role requires exact member identity")
	}
	if !validID(c.RunID) || !nativeGuest(c.Target) || !validID(c.Handle) || c.Node == "" || c.Volume == "" || !sha256Text(c.PlanHash) || c.MaxLogBytes <= 0 || !sha256Text(c.SHA256) || !cleanAbsolute(c.Directory) || !stringsWithin(c.Directory, "/var/tmp/hfs-run-"+c.RunID) || !stringsWithin(c.Executable, "/var/tmp/hfs-run-"+c.RunID) {
		return nil, errors.New("supervisor requires explicit run-owned identities and bounds")
	}
	if err := privateDirectory(c.Directory); err != nil {
		return nil, err
	}
	if err := verifyExecutable(c.Executable, c.SHA256); err != nil {
		return nil, err
	}
	log, err := os.OpenFile(filepath.Join(c.Directory, c.Handle+".daemon.log"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		_ = log.Close()
		return nil, err
	}
	s := &FaultSupervisor{config: c, log: log, launchGeneration: hex.EncodeToString(nonce)}
	audit, err := admin.OpenAudit(ctx, filepath.Join(c.Directory, c.Handle+".audit.jsonl"))
	if err != nil {
		return s, err
	}
	s.audit = audit
	server, err := admin.Listen(ctx, admin.Config{SocketPath: s.Socket(), NodeID: c.Node, VolumeID: c.Volume, Generation: s.launchGeneration, EnableTestHooks: true, Audit: audit.Record, ToggleFault: s.toggle, Status: s.status})
	if err != nil {
		return s, err
	}
	s.server = server
	child := exec.Command(c.Executable, c.Args...)
	child.Env = []string{"PATH=/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/usr/local/sbin"}
	if c.Role == "private-etcd" {
		child.Env = append(child.Env, "GOMAXPROCS=1", "GOMEMLIMIT=128MiB")
	}
	child.Stdin = nil
	output := &supervisorLog{log: boundedLog{file: log, remaining: c.MaxLogBytes}}
	child.Stdout = output
	child.Stderr = output
	if err := ctx.Err(); err != nil {
		return s, err
	}
	if err := child.Start(); err != nil {
		return s, err
	}
	s.child = child
	s.process = FaultProcess{PID: child.Process.Pid, Birth: s.launchGeneration, Executable: c.Executable, SHA256: c.SHA256, Handle: c.Handle}
	exited, closeObservation, err := observeChild(child.Process.Pid)
	if err != nil {
		return s, err
	}
	s.exited = exited
	s.closeObservation = closeObservation
	if err := WriteJSON(s.Manifest(), s.process); err != nil {
		return s, err
	}
	// etcd has no HomelabFS workspace socket. Its direct-child identity is
	// sufficient only for process control, never HFS serving readiness.
	if c.Role == "private-etcd" {
		s.mu.Lock()
		s.admission = true
		s.mu.Unlock()
	}
	return s, nil
}

// Socket returns the private supervisor endpoint, separate from daemon admin.
func (s *FaultSupervisor) Socket() string {
	return filepath.Join(s.config.Directory, "supervisor-"+s.config.Handle+".sock")
}

// Manifest returns the immutable run-owned launch identity record.
func (s *FaultSupervisor) Manifest() string {
	return filepath.Join(s.config.Directory, "supervisor-"+s.config.Handle+".json")
}

// Process returns the child identity, never a PID from external discovery.
func (s *FaultSupervisor) Process() FaultProcess { s.mu.Lock(); defer s.mu.Unlock(); return s.process }

// Close closes signal admission and drains controls before any reap. It waits
// for child exit and deliberately never kills or resumes a preserved child.
// The caller must explicitly send the approved CONT/KILL first when needed.
func (s *FaultSupervisor) Close() error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.admission = false
		s.closed = true
		s.mu.Unlock()
		if s.server != nil {
			s.closeErr = errors.Join(s.closeErr, s.server.Close())
		}
		if s.child != nil {
			s.closeErr = errors.Join(s.closeErr, s.child.Wait())
		}
		if s.closeObservation != nil {
			s.closeErr = errors.Join(s.closeErr, s.closeObservation())
		}
		if s.audit != nil {
			s.closeErr = errors.Join(s.closeErr, s.audit.Close())
		}
		if s.log != nil {
			s.closeErr = errors.Join(s.closeErr, s.log.Sync(), s.log.Close())
		}
	})
	return s.closeErr
}

func (s *FaultSupervisor) toggle(ctx context.Context, name string, enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !enabled || !s.admission || s.child == nil {
		return errors.New("signal admission closed")
	}
	signal := syscall.Signal(0)
	for _, candidate := range []struct {
		name   string
		signal syscall.Signal
	}{{"daemon-kill", syscall.SIGKILL}, {"daemon-stop", syscall.SIGSTOP}, {"daemon-continue", syscall.SIGCONT}} {
		if name == signalToken(candidate.name, s.process) {
			signal = candidate.signal
		}
	}
	if s.config.Role == "private-etcd" && name == signalToken("etcd-terminate", s.process) {
		if s.config.TerminateIntent != nil {
			if err := s.config.TerminateIntent(s.process); err != nil {
				return err
			}
		}
		signal = syscall.SIGTERM
	}
	if signal == 0 {
		return errors.New("signal target identity mismatch")
	}
	exited, err := s.exited()
	if err != nil {
		return err
	}
	if exited {
		return errors.New("owned child already exited; preserved unreaped")
	}
	// The child can exit between observation and Signal, but cannot be reaped or
	// have its PID reused until this lock's admission gate is permanently closed.
	return s.child.Process.Signal(signal)
}
func (s *FaultSupervisor) status(ctx context.Context) (admin.Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return admin.Status{}, err
	}
	profile := "native-run-supervisor"
	if s.config.Role == "private-etcd" {
		profile = "private-etcd-process"
	}
	return admin.Status{NodeID: s.config.Node, TestHooksEnabled: s.admission, Volumes: []admin.VolumeStatus{{VolumeID: s.config.Volume, Generation: s.launchGeneration, State: "SUPERVISED", Profile: profile, NormalPublication: false}}}, nil
}
func (w *supervisorLog) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.log.Write(data)
}
func signalToken(action string, p FaultProcess) string {
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(raw)
	return action + "-" + hex.EncodeToString(sum[:16])
}
func stringsWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, "../") && filepath.Clean(path) == path
}
func privateDirectory(path string) error {
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("run control ancestors must be real directories")
		}
		if current == path {
			stat, ok := info.Sys().(*syscall.Stat_t)
			if !ok || stat.Uid != uint32(os.Geteuid()) || info.Mode().Perm() != 0700 {
				return errors.New("private run control directory must be owned mode 0700")
			}
		}
		if current == "/" {
			return nil
		}
	}
}
func verifyExecutable(path, digest string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 {
		return errors.New("supervised executable must be a real non-group-writable file")
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != digest {
		return fmt.Errorf("supervised executable checksum mismatch")
	}
	return nil
}
