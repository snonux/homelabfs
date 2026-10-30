//go:build freebsd

package fuseadapter

import (
	"bytes"
	"os"
	"unsafe"

	"github.com/snonux/homelabfs/internal/fsapi"
	"golang.org/x/sys/unix"
)

// FreeBSD has no distinct O_DSYNC in the selected Go syscall ABI.
const dataSyncFlag = 0

func platformMountOptions() []string {
	// mount_fusefs rejects nodev. Device nodes remain unsupported: Mknod is
	// ENOSYS and service metadata can only describe files, directories or links.
	// Keep kernel permission checks and set-ID suppression mandatory.
	return []string{"default_permissions", "nosuid"}
}

func mountedIdentity(fd int) ([]byte, error) {
	var st unix.Statfs_t
	if err := unix.Fstatfs(fd, &st); err != nil {
		return nil, err
	}
	// Native FreeBSD truncates the subtype in its 16-byte filesystem type.
	if string(bytes.TrimRight(st.Fstypename[:], "\x00")) != "fusefs.homelabf" {
		return nil, fsapi.Stale
	}
	buf := make([]byte, 256)
	n, err := unix.ExtattrGetFd(fd, unix.EXTATTR_NAMESPACE_USER, "homelabfs.identity", uintptr(unsafe.Pointer(&buf[0])), len(buf))
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

// Explicit controlled participants supply fixed native credential evidence.
// This admits only the disposable bound fixtures; native runtime is a gate.
func multiuserAvailable() bool { return os.Geteuid() == 0 }
