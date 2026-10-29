//go:build linux

package fuseadapter

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/snonux/homelabfs/internal/fsapi"
	"golang.org/x/sys/unix"
)

// Participants authenticates only this fixed-credential process and trusted
// helpers it directly spawns. Helpers must never change IDs/groups, exec another
// program, share their file table or transfer descriptors outside this fixture.
// This trust requirement is intentional; proc snapshots are not an arbitrary
// workload credential provider. Owned children remain unreaped until revoked,
// and retained proc directories prevent process-ID reuse from changing identity.
// One provider belongs to exactly one volume/generation/connection session.
type Participants struct {
	mu        sync.Mutex
	mount     MountIdentity
	next      uint64
	processes map[uint64]*participant
	closed    bool
	sealed    bool
}

type participant struct {
	process  *os.File
	pid      uint32
	uid, gid uint32
	groups   []uint32
}

// ParticipantSpec describes a trusted helper. Credentials, when supplied, are
// installed by exec before the helper runs. Stdin should keep it idle until
// Start returns, allowing the parent to establish all fixture admission first.
// No daemon status/readiness protocol is required of an ordinary helper.
type ParticipantSpec struct {
	Executable     string
	Args, Env      []string
	Credentials    *syscall.Credential
	Stdin          io.Reader
	Stdout, Stderr io.Writer
}

// Participant is a child owned by a Participants provider. Wait revokes request
// admission before reaping it. The caller must not reap it through other APIs.
type Participant struct {
	owner   *Participants
	token   uint64
	command *exec.Cmd
	once    sync.Once
	waitErr error
}

var _ Credentials = (*Participants)(nil)

// NewParticipants captures the parent process's exact fixed credentials.
func NewParticipants() (*Participants, error) {
	groups, err := os.Getgroups()
	if err != nil {
		return nil, err
	}
	p := &Participants{processes: make(map[uint64]*participant), next: 1}
	entry := &participant{pid: uint32(os.Getpid()), uid: uint32(os.Geteuid()), gid: uint32(os.Getegid())}
	for _, group := range groups {
		entry.groups = append(entry.groups, uint32(group))
	}
	slices.Sort(entry.groups)
	entry.process, err = os.Open("/proc/self")
	if err != nil {
		return nil, err
	}
	if _, err = entry.snapshot(uint32(os.Getpid())); err != nil {
		_ = entry.process.Close()
		return nil, err
	}
	p.processes[1] = entry
	return p, nil
}

// BindMount permanently restricts the provider to one exact native connection.
// New calls this before exposing a buffered adapter; rebinding always fails.
func (p *Participants) BindMount(id MountIdentity) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.mount.Session.Valid() || !id.Volume.Valid() || !id.Generation.Valid() || !id.Session.Valid() {
		return fsapi.Stale
	}
	p.mount = id
	return nil
}

// Start launches a trusted fixed-credential helper and retains its process
// incarnation before returning. All helpers must be started idle before Mount;
// spawning after native publication is denied. Unknown processes are never
// discovered by PID.
func (p *Participants) Start(ctx context.Context, spec ParticipantSpec) (*Participant, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.sealed || !p.mount.Session.Valid() {
		return nil, fsapi.Stale
	}
	if p.next >= 64 || spec.Executable == "" {
		return nil, fsapi.Invalid
	}
	command := exec.CommandContext(ctx, spec.Executable, spec.Args...)
	command.Stdin, command.Stdout, command.Stderr = spec.Stdin, spec.Stdout, spec.Stderr
	command.Env = spec.Env
	entry := &participant{uid: uint32(os.Geteuid()), gid: uint32(os.Getegid()), groups: slices.Clone(p.processes[1].groups)}
	if spec.Credentials != nil {
		c := *spec.Credentials
		c.Groups = slices.Clone(c.Groups)
		if c.NoSetGroups {
			return nil, fsapi.Unsupported
		}
		entry.uid, entry.gid, entry.groups = c.Uid, c.Gid, slices.Clone(c.Groups)
		command.SysProcAttr = &syscall.SysProcAttr{Credential: &c}
	}
	slices.Sort(entry.groups)
	if len(entry.groups) > 1024 {
		return nil, fsapi.Invalid
	}
	if err := command.Start(); err != nil {
		return nil, err
	}
	entry.pid = uint32(command.Process.Pid)
	// The direct child is still unreaped, even if already exited. Its PID cannot
	// be reassigned while we open its process directory. No PID lookup fallback.
	var err error
	entry.process, err = os.Open(fmt.Sprintf("/proc/%d", entry.pid))
	if err != nil {
		killErr := command.Process.Kill()
		waitErr := command.Wait()
		return nil, errors.Join(err, killErr, waitErr)
	}
	p.next++
	p.processes[p.next] = entry
	return &Participant{owner: p, token: p.next, command: command}, nil
}

// Snapshot checks every request against registered, still-owned process task
// directories and exact expected IDs/groups. It never infers groups from UID.
func (p *Participants) Snapshot(ctx context.Context, caller Caller) (Identity, error) {
	if err := ctx.Err(); err != nil {
		return Identity{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || !p.mount.Session.Valid() || caller.Thread == 0 {
		return Identity{}, fsapi.Permission
	}
	for token, process := range p.processes {
		if caller.UID != process.uid || caller.GID != process.gid {
			continue
		}
		cred, err := process.snapshot(caller.Thread)
		if err == nil {
			return Identity{Credentials: cred, Process: token, ProcessID: process.pid}, nil
		}
	}
	return Identity{}, fsapi.Permission
}

// Close revokes all admission. Every owned child must be Waited first; live or
// exited-but-unreaped children are retained for explicit inspection/cleanup.
func (p *Participants) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	if len(p.processes) == 0 {
		return nil
	}
	if len(p.processes) != 1 {
		return fsapi.Busy
	}
	err := p.processes[1].process.Close()
	delete(p.processes, 1)
	return err
}

// PID exposes the unreaped child PID for bounded signal tests, not identity.
func (p *Participant) PID() int { return p.command.Process.Pid }

// Signal sends a signal while the child remains owned and unreaped.
func (p *Participant) Signal(signal os.Signal) error { return p.command.Process.Signal(signal) }

// Wait observes process exit without reaping, then revokes this process before
// the sole reap. Previously issued service handles
// retain their own kernel lifetime until FLUSH/RELEASE or session teardown.
func (p *Participant) Wait() error {
	p.once.Do(func() {
		// Observe exit without reaping: kernel close callbacks retain admission
		// until the process has actually exited. cmd.Wait then owns the only reap.
		for {
			var info unix.Siginfo
			err := unix.Waitid(unix.P_PID, p.PID(), &info, unix.WEXITED|unix.WNOWAIT, nil)
			if errors.Is(err, unix.EINTR) {
				continue
			}
			if err != nil {
				p.waitErr = err
				return
			}
			break
		}
		p.owner.mu.Lock()
		entry := p.owner.processes[p.token]
		delete(p.owner.processes, p.token)
		p.owner.mu.Unlock()
		p.waitErr = p.command.Wait()
		if entry != nil {
			p.waitErr = errors.Join(p.waitErr, entry.process.Close())
		}
	})
	return p.waitErr
}

func (p *participant) snapshot(tid uint32) (fsapi.Cred, error) {
	fd, err := unix.Openat(int(p.process.Fd()), fmt.Sprintf("task/%d/status", tid), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return fsapi.Cred{}, err
	}
	file := os.NewFile(uintptr(fd), "participant task status")
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, 65537))
	if err != nil || len(raw) > 65536 {
		return fsapi.Cred{}, fsapi.Permission
	}
	var havePID, haveUID, haveGID, haveGroups bool
	var groups []uint32
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "Tgid:":
			havePID = len(fields) == 2 && fields[1] == strconv.FormatUint(uint64(p.pid), 10)
		case "Uid:", "Gid:":
			expected := p.uid
			if fields[0] == "Gid:" {
				expected = p.gid
			}
			if len(fields) != 5 {
				return fsapi.Cred{}, fsapi.Permission
			}
			for _, field := range fields[1:] {
				if field != strconv.FormatUint(uint64(expected), 10) {
					return fsapi.Cred{}, fsapi.Permission
				}
			}
			if fields[0] == "Uid:" {
				haveUID = true
			} else {
				haveGID = true
			}
		case "Groups:":
			haveGroups = true
			for _, field := range fields[1:] {
				id, err := strconv.ParseUint(field, 10, 32)
				if err != nil {
					return fsapi.Cred{}, fsapi.Permission
				}
				groups = append(groups, uint32(id))
			}
		}
	}
	slices.Sort(groups)
	if !havePID || !haveUID || !haveGID || !haveGroups || !slices.Equal(groups, p.groups) {
		return fsapi.Cred{}, fsapi.Permission
	}
	return fsapi.Cred{UID: p.uid, GID: p.gid, PID: tid, Groups: slices.Clone(p.groups)}, nil
}

// seal prevents fork/exec after native publication: inherited FUSE fds can
// generate child close callbacks before exec.Cmd.Start returns its PID. All
// trusted helpers must be spawned idle before mounting, then released by their
// private pipes. This avoids admitting an unknown pre-exec process by PID.
func (p *Participants) seal() { p.mu.Lock(); p.sealed = true; p.mu.Unlock() }
