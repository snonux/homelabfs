package behavior

import (
	"fmt"
	"syscall"

	"golang.org/x/sys/unix"
)

func executableCredential(c *syscall.Credential) (*syscall.Credential, error) {
	return copyExecutableCredential(c), nil
}

func mountIdentity(fd int) (string, error) {
	var stat unix.Statfs_t
	if err := unix.Fstatfs(fd, &stat); err != nil {
		return "", err
	}
	if stat.Type != unix.FUSE_SUPER_MAGIC {
		return "", fmt.Errorf("target is not FUSE")
	}
	data := make([]byte, 256)
	n, err := unix.Fgetxattr(fd, "user.homelabfs.identity", data)
	return string(data[:max(n, 0)]), err
}

func dataSync(fd int) error { return unix.Fdatasync(fd) }
