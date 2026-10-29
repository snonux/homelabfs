package behavior

import (
	"bytes"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func executableCredential(c *syscall.Credential) (*syscall.Credential, error) {
	return freeBSDExecutableCredential(c, syscall.SYS_SETGROUPS)
}

func mountIdentity(fd int) (string, error) {
	var stat unix.Statfs_t
	if err := unix.Fstatfs(fd, &stat); err != nil {
		return "", err
	}
	if string(bytes.TrimRight(stat.Fstypename[:], "\x00")) != "fusefs.homelabf" {
		return "", fmt.Errorf("target is not HomelabFS FUSE")
	}
	data := make([]byte, 256)
	n, err := unix.ExtattrGetFd(fd, unix.EXTATTR_NAMESPACE_USER, "homelabfs.identity", uintptr(unsafe.Pointer(&data[0])), len(data))
	return string(data[:max(n, 0)]), err
}

func dataSync(fd int) error {
	_, _, errno := unix.Syscall(unix.SYS_FDATASYNC, uintptr(fd), 0, 0)
	if errno != 0 {
		return errno
	}
	return nil
}
