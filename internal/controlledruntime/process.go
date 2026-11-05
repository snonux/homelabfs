//go:build linux || freebsd

package controlledruntime

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"slices"

	"github.com/snonux/homelabfs/internal/fsapi"
	"golang.org/x/sys/unix"
)

// Executable pins the exact admitted exporter bytes; arguments are host policy,
// never guest input. The child inherits only management fd 3, not a socket name.
type Executable struct {
	Path   string
	SHA256 [32]byte
	Args   []string
	// ControlledBootstrap adds private inherited bootstrap fd6 and owned parent fd7.
	ControlledBootstrap bool
}

type processIdentity struct {
	PublicID       fsapi.UUID
	UID, GID, PID  uint32
	Groups         []uint32
	Incarnation    string
	Device, Inode  uint64
	ExecutablePath string
	TextVnode      uint64
}

type exporter struct {
	nativeFDs           [3]int
	observationConsumed bool
	observedUnique      uint64
	tupleVerified       bool
	command             *exec.Cmd
	executable          *os.File
	identity            processIdentity
	control             *net.UnixConn
	recovery            *net.UnixConn
	bootstrap           *os.File
	exited              chan struct{}
}

// Start launches one exact owned executable copy. Any attempted start consumes
// this root; failed resources are preserved, never silently retried or replaced.
func (s *Supervisor) Start(ctx context.Context, executable Executable) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.record.Stage != "created" || s.child != nil {
		return fsapi.Unavailable
	}
	s.record.Stage = "starting"
	if err := s.protect(); err != nil {
		return err
	}
	child, err := s.launch(ctx, executable)
	// Retain every actually started child, including one whose OS observation
	// failed. It must be fenced/inspectable, never silently replaced or killed.
	if child != nil {
		s.child = child
	}
	if err != nil {
		s.record.Stage = "start-failed"
		if e := s.protect(); e != nil {
			return fmt.Errorf("start: %v; preserve failure: %w", err, e)
		}
		return err
	}
	s.child = child
	s.record.Process = child.identity.Incarnation
	s.record.ProcessID = child.identity.PublicID
	s.record.Stage = "started"
	if err = s.protect(); err != nil {
		_ = child.control.Close()
		return err
	}
	return nil
}

func (s *Supervisor) launch(ctx context.Context, spec Executable) (*exporter, error) {
	return s.launchObserved(ctx, spec, observeProcess)
}

// The observer argument is an internal OS boundary, never a runtime option.
func (s *Supervisor) launchObserved(ctx context.Context, spec Executable, observe func(int) (processIdentity, error)) (*exporter, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Refuse missing build/OS capabilities before pinning files, allocating
	// inherited channels, or creating any native exporter child.
	if _, err := checkProcessObserver(observe); err != nil {
		return nil, fmt.Errorf("process observer preflight: %w", err)
	}
	pinned, err := s.pinExecutable(spec)
	if err != nil {
		return nil, err
	}
	parent, child, err := controlPair()
	if err != nil {
		_ = pinned.Close()
		return nil, err
	}
	defer func() { _ = child.Close() }()
	recoveryParent, recoveryChild, err := controlPair()
	if err != nil {
		_ = parent.Close()
		_ = pinned.Close()
		return nil, err
	}
	defer func() { _ = recoveryChild.Close() }()
	command := exec.Command("/dev/fd/4", spec.Args...)
	command.Env = []string{"HFS_SUPERVISOR_FD=3"}
	command.ExtraFiles = []*os.File{child, pinned, recoveryChild}
	var bootstrapWriter *os.File
	if spec.ControlledBootstrap {
		reader, writer, pipeErr := os.Pipe()
		if pipeErr != nil {
			_ = recoveryParent.Close()
			_ = parent.Close()
			_ = pinned.Close()
			return nil, pipeErr
		}
		defer func() { _ = reader.Close() }()
		bootstrapWriter = writer
		command.ExtraFiles = append(command.ExtraFiles, reader, s.directory)
	}
	if err = command.Start(); err != nil {
		if bootstrapWriter != nil {
			_ = bootstrapWriter.Close()
		}
		_ = recoveryParent.Close()
		_ = parent.Close()
		_ = pinned.Close()
		return nil, err
	}
	result := &exporter{command: command, executable: pinned, control: parent, recovery: recoveryParent, bootstrap: bootstrapWriter, exited: make(chan struct{})}
	go func() { _ = command.Wait(); close(result.exited) }()
	identity, err := observe(command.Process.Pid)
	if err == nil {
		err = sameExecutable(pinned, identity)
	}
	if err != nil {
		_ = recoveryParent.Close()
		_ = parent.Close()
		if bootstrapWriter != nil {
			_ = bootstrapWriter.Close()
		}
		return result, fmt.Errorf("observe exporter: %w", err)
	}
	if _, err = rand.Read(identity.PublicID[:]); err != nil {
		_ = recoveryParent.Close()
		_ = parent.Close()
		if bootstrapWriter != nil {
			_ = bootstrapWriter.Close()
		}
		return result, err
	}
	result.identity = identity
	return result, nil
}

func (s *Supervisor) pinExecutable(spec Executable) (*os.File, error) {
	if spec.SHA256 == ([32]byte{}) || len(spec.Args) > 64 {
		return nil, fsapi.Invalid
	}
	argumentBytes := 0
	for _, arg := range spec.Args {
		argumentBytes += len(arg)
		if argumentBytes > 65536 {
			return nil, fsapi.Invalid
		}
	}
	fd, err := unix.Open(spec.Path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	source := os.NewFile(uintptr(fd), spec.Path)
	defer func() { _ = source.Close() }()
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return nil, err
	}
	if uint32(st.Mode)&unix.S_IFMT != unix.S_IFREG || st.Mode&0022 != 0 || (st.Uid != 0 && st.Uid != uint32(os.Geteuid())) || st.Size < 1 || st.Size > maxExecutable {
		return nil, fsapi.Permission
	}
	out, err := ownedFile(s.directory, "exporter", unix.O_RDWR|unix.O_CREAT|unix.O_EXCL, 0700)
	if err != nil {
		return nil, err
	}
	defer func() { _ = out.Close() }()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, hash), io.LimitReader(source, maxExecutable+1))
	if err != nil {
		return nil, err
	}
	if n != st.Size || !slices.Equal(hash.Sum(nil), spec.SHA256[:]) {
		return nil, fmt.Errorf("exporter digest %s: %w", hex.EncodeToString(hash.Sum(nil)), fsapi.Permission)
	}
	if err = out.Chmod(0500); err != nil {
		return nil, err
	}
	if err = out.Sync(); err != nil {
		return nil, err
	}
	if err = s.directory.Sync(); err != nil {
		return nil, err
	}
	return ownedFile(s.directory, "exporter", unix.O_RDONLY, 0)
}

func (e *exporter) live() error {
	select {
	case <-e.exited:
		return fsapi.Unavailable
	default:
	}
	now, err := observeProcess(e.command.Process.Pid)
	if err != nil {
		return err
	}
	if now.UID != e.identity.UID || now.GID != e.identity.GID || now.PID != e.identity.PID || now.Incarnation != e.identity.Incarnation || !slices.Equal(now.Groups, e.identity.Groups) {
		return fsapi.Permission
	}
	return sameExecutable(e.executable, now)
}

func sameExecutable(f *os.File, identity processIdentity) error {
	var st unix.Stat_t
	if err := unix.Fstat(int(f.Fd()), &st); err != nil {
		return err
	}
	if identity.ExecutablePath != "" {
		if identity.ExecutablePath != f.Name() || identity.TextVnode == 0 {
			return fsapi.Permission
		}
	} else if uint64(st.Dev) != identity.Device || st.Ino != identity.Inode {
		return fsapi.Permission
	}
	if st.Mode&0222 != 0 || st.Nlink != 1 {
		return fsapi.Permission
	}
	return nil
}

func controlPair() (*net.UnixConn, *os.File, error) {
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	parent := os.NewFile(uintptr(pair[0]), "supervisor-control")
	child := os.NewFile(uintptr(pair[1]), "exporter-control")
	conn, err := net.FileConn(parent)
	_ = parent.Close()
	if err != nil {
		_ = child.Close()
		return nil, nil, err
	}
	return conn.(*net.UnixConn), child, nil
}

func (e *exporter) consumeObservation(unique uint64) bool {
	if !e.tupleVerified || e.observationConsumed || unique == 0 || unique == e.observedUnique {
		return false
	}
	e.observationConsumed = true
	e.observedUnique = unique
	return true
}
