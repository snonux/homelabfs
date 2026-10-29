//go:build freebsd

package admin

import (
	"net"
	"runtime"
	"unsafe"

	"golang.org/x/sys/unix"
)

// FreeBSD LOCAL_PEERCRED supplies xucred including cr_pid. x/sys hides the
// final union, so request the exact supported ABI and check its returned size.
// No untrusted pointer is interpreted: the buffer is fixed, kernel-filled data.
func credentials(c *net.UnixConn) (Peer, error) {
	var peer Peer
	release, err := unix.SysctlUint32("kern.osreldate")
	if err != nil {
		return peer, err
	}
	raw, err := c.SyscallConn()
	if err != nil {
		return peer, err
	}
	var observed error
	err = raw.Control(func(fd uintptr) {
		buffer := [88]byte{}
		length := uint32(len(buffer))
		_, _, errno := unix.Syscall6(unix.SYS_GETSOCKOPT, fd, unix.SOL_LOCAL, unix.LOCAL_PEERCRED, uintptr(unsafe.Pointer(&buffer[0])), uintptr(unsafe.Pointer(&length)), 0)
		if errno != 0 {
			observed = errno
			return
		}
		if length != uint32(len(buffer)) {
			observed = unix.EINVAL
			return
		}
		peer, observed = decodeFreeBSDPeer(buffer[:], release, runtime.GOARCH)
	})
	if err != nil {
		return peer, err
	}
	return peer, observed
}
