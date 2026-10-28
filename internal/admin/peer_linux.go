//go:build linux

package admin

import (
	"golang.org/x/sys/unix"
	"net"
)

func credentials(c *net.UnixConn) (Peer, error) {
	var peer Peer
	raw, err := c.SyscallConn()
	if err != nil {
		return peer, err
	}
	var observed error
	err = raw.Control(func(fd uintptr) {
		cred, e := unix.GetsockoptUcred(int(fd), unix.SOL_SOCKET, unix.SO_PEERCRED)
		observed = e
		if e == nil {
			pid := cred.Pid
			peer = Peer{UID: cred.Uid, GID: cred.Gid, PID: &pid}
		}
	})
	if err != nil {
		return peer, err
	}
	return peer, observed
}
