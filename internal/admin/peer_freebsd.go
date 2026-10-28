//go:build freebsd

package admin

import (
	"fmt"
	"golang.org/x/sys/unix"
	"net"
)

// FreeBSD libc getpeereid uses SOL_LOCAL/LOCAL_PEERCRED and checks cr_version.
// Calling the same kernel primitive avoids a cgo/libc dependency.
func credentials(c *net.UnixConn) (Peer, error) {
	var peer Peer
	raw, err := c.SyscallConn()
	if err != nil {
		return peer, err
	}
	var observed error
	err = raw.Control(func(fd uintptr) {
		cred, e := unix.GetsockoptXucred(int(fd), unix.SOL_LOCAL, unix.LOCAL_PEERCRED)
		observed = e
		if e == nil {
			if cred.Version != 0 /* XUCRED_VERSION from sys/ucred.h */ || cred.Ngroups < 1 {
				observed = fmt.Errorf("invalid peer credential version/groups")
				return
			}
			peer = Peer{UID: cred.Uid, GID: cred.Groups[0]}
		}
	})
	if err != nil {
		return peer, err
	}
	return peer, observed
}
