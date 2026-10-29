//go:build linux || freebsd

package fuseadapter

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"slices"
	"sync"
	"syscall"

	"github.com/snonux/homelabfs/internal/fsapi"
)

// Participants authenticates only this fixed-credential process and trusted
// helpers it directly spawns. Helpers must never change IDs/groups, exec another
// program, share their file table or transfer descriptors outside this fixture.
// This trust requirement is intentional; proc snapshots are not an arbitrary
// workload credential provider. Owned children remain unreaped until revoked,
// and Linux retained proc directories/FreeBSD start-time evidence preserve the
// exact incarnation until revocation.
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
	participantPlatform
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
	entry.groups, err = participantGroups(groups, entry.gid)
	if err != nil {
		return nil, err
	}
	err = entry.capture()
	if err != nil {
		return nil, err
	}
	if _, err = entry.snapshot(uint32(os.Getpid())); err != nil {
		_ = entry.close()
		return nil, err
	}
	p.processes[1] = entry
	return p, nil
}

// BindMount permanently restricts the provider to one exact native connection.
// New calls this for buffered or fixed unprivileged validation; a direct-only
// embedding must bind explicitly before starting helpers. Rebinding fails.
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
		native := participantCredential(c)
		command.SysProcAttr = &syscall.SysProcAttr{Credential: &native}
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
	// be reassigned while capturing its platform evidence. No discovery fallback.
	var err error
	err = entry.capture()
	if err == nil {
		_, err = entry.snapshot(entry.pid)
	}
	if err != nil {
		killErr := command.Process.Kill()
		waitErr := command.Wait()
		return nil, errors.Join(err, killErr, waitErr, entry.close())
	}
	p.next++
	p.processes[p.next] = entry
	return &Participant{owner: p, token: p.next, command: command}, nil
}

// Snapshot checks every request against a registered, still-owned process
// incarnation and exact expected IDs/groups. It never infers groups from UID.
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
	err := p.processes[1].close()
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
		if err := waitParticipantExit(p.PID()); err != nil {
			p.waitErr = err
			return
		}
		p.owner.mu.Lock()
		entry := p.owner.processes[p.token]
		delete(p.owner.processes, p.token)
		p.owner.mu.Unlock()
		p.waitErr = p.command.Wait()
		if entry != nil {
			p.waitErr = errors.Join(p.waitErr, entry.close())
		}
	})
	return p.waitErr
}

// seal prevents fork/exec after native publication: inherited FUSE fds can
// generate child close callbacks before exec.Cmd.Start returns its PID. All
// trusted helpers must be spawned idle before mounting, then released by their
// private pipes. This avoids admitting an unknown pre-exec process by PID.
func (p *Participants) seal() { p.mu.Lock(); p.sealed = true; p.mu.Unlock() }
