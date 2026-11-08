//go:build linux || freebsd

package admin

import "net"

// PeerCredentials returns the kernel-supplied credentials of a connected local
// stream socket peer (Linux SO_PEERCRED, FreeBSD LOCAL_PEERCRED with its checked
// xucred ABI). It is the narrow helper reused by other private local endpoints;
// it grants no administrative operation, audit or recovery authority by itself.
// Callers still decide which UID is authorized.
func PeerCredentials(c *net.UnixConn) (Peer, error) {
	return credentials(c)
}
