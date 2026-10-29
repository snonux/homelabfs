//go:build freebsd

package fuseadapter

import (
	"runtime"
	"syscall"

	"github.com/snonux/homelabfs/internal/fsapi"
	"golang.org/x/sys/unix"
)

type participantPlatform struct {
	start [2]uint64 // Process start timeval; direct children remain unreaped.
}

func (p *participant) capture() error {
	release, err := unix.Sysctl("kern.osrelease")
	if err != nil {
		return err
	}
	if !supportedFreeBSDParticipantABI(release, runtime.GOARCH) || syscall.SYS_GETGROUPS != 79 || syscall.SYS_SETGROUPS != 80 {
		return fsapi.Unsupported
	}
	start, err := p.readStart()
	if err != nil {
		return err
	}
	p.start = start
	return nil
}
func (p *participant) close() error { return nil }
func (p *participant) readStart() ([2]uint64, error) {
	raw, err := unix.SysctlRaw("kern.proc.pid", int(p.pid))
	if err != nil {
		return [2]uint64{}, err
	}
	return decodeFreeBSDParticipant(raw, p.pid, p.uid, p.gid, p.groups)
}
func (p *participant) snapshot(pid uint32) (fsapi.Cred, error) {
	// FreeBSD FUSE headers carry the process PID, never a Linux-style TID.
	if pid != p.pid {
		return fsapi.Cred{}, fsapi.Permission
	}
	start, err := p.readStart()
	if err != nil {
		return fsapi.Cred{}, err
	}
	if start != p.start {
		return fsapi.Cred{}, fsapi.Permission
	}
	return fsapi.Cred{UID: p.uid, GID: p.gid, PID: pid, Groups: append([]uint32(nil), p.groups...)}, nil
}
func waitParticipantExit(pid int) error {
	// FreeBSD wait6(P_PID=0, pid, NULL, WEXITED|WNOWAIT, NULL, NULL).
	// The direct child remains unreaped until admission is revoked by Wait.
	for {
		_, _, errno := unix.Syscall6(unix.SYS_WAIT6, 0, uintptr(pid), 0, unix.WEXITED|unix.WNOWAIT, 0, 0)
		if errno == unix.EINTR {
			continue
		}
		if errno != 0 {
			return errno
		}
		return nil
	}
}

func participantGroups(groups []int, gid uint32) ([]uint32, error) {
	return freeBSDParticipantGroups(groups, gid)
}
func participantCredential(c syscall.Credential) syscall.Credential {
	return freeBSDParticipantCredential(c)
}
