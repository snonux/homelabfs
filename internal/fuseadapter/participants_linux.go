//go:build linux

package fuseadapter

import (
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/snonux/homelabfs/internal/fsapi"
	"golang.org/x/sys/unix"
)

type participantPlatform struct {
	process *os.File
}

func (p *participant) capture() error {
	var err error
	p.process, err = os.Open(fmt.Sprintf("/proc/%d", p.pid))
	return err
}
func (p *participant) close() error {
	if p.process == nil {
		return nil
	}
	return p.process.Close()
}
func waitParticipantExit(pid int) error {
	for {
		var info unix.Siginfo
		err := unix.Waitid(unix.P_PID, pid, &info, unix.WEXITED|unix.WNOWAIT, nil)
		if errors.Is(err, unix.EINTR) {
			continue
		}
		return err
	}
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

func participantGroups(groups []int, gid uint32) ([]uint32, error) {
	result := make([]uint32, len(groups))
	for i, g := range groups {
		if g < 0 || uint64(g) > uint64(^uint32(0)) {
			return nil, fsapi.Permission
		}
		result[i] = uint32(g)
	}
	slices.Sort(result)
	return result, nil
}
func participantCredential(c syscall.Credential) syscall.Credential {
	c.Groups = slices.Clone(c.Groups)
	return c
}
