//go:build linux || freebsd

package controlledruntime

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"

	"github.com/snonux/homelabfs/internal/fsapi"
	"golang.org/x/sys/unix"
)

type descriptorExecReport struct {
	Version     string `json:"version"`
	Platform    string `json:"platform"`
	MountType   string `json:"mount_type"`
	MountID     string `json:"mount_id"`
	Flags       uint64 `json:"mount_flags"`
	Supported   bool   `json:"supported"`
	Failure     string `json:"failure,omitempty"`
	Publication any    `json:"publication"`
}

// This is a read-only prerequisite check, not proof of a child exec or READY.
// It opens only this process's executable and checks the kernel's FD alias.
func checkDescriptorExec() (descriptorExecReport, error) {
	r := descriptorExecReport{Version: "homelabfs-descriptor-exec/1", Platform: runtime.GOOS + "/" + runtime.GOARCH}
	if err := descriptorExecMount(&r); err != nil {
		return r, fmt.Errorf("descriptor execution requires an admitted /dev/fd: %w", err)
	}
	path, err := os.Executable()
	if err != nil {
		return r, err
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return r, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer func() { _ = f.Close() }()
	var original, alias unix.Stat_t
	if err = unix.Fstat(fd, &original); err != nil {
		return r, err
	}
	if err = descriptorExecFile(fd); err != nil {
		return r, err
	}
	name := "/dev/fd/" + strconv.Itoa(fd)
	if err = descriptorExecAlias(name, &alias); err != nil {
		return r, err
	}
	if uint32(original.Mode)&unix.S_IFMT != unix.S_IFREG || original.Mode&0111 == 0 ||
		uint32(alias.Mode)&unix.S_IFMT != unix.S_IFREG || original.Dev != alias.Dev || original.Ino != alias.Ino {
		return r, fmt.Errorf("/dev/fd must resolve directly to the held executable vnode: %w", fsapi.Unsupported)
	}
	r.Supported = true
	return r, nil
}

func reportDescriptorExec(output io.Writer) error {
	r, err := checkDescriptorExec()
	if err != nil {
		r.Failure = err.Error()
	}
	if writeErr := json.NewEncoder(output).Encode(r); writeErr != nil {
		return writeErr
	}
	return err
}
