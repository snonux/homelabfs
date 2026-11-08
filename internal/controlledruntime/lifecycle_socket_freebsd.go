//go:build freebsd

package controlledruntime

import "golang.org/x/sys/unix"

// chmodSocketNoFollow changes the mode of the socket name in dirfd without
// following a symlink: FreeBSD fchmodat(AT_SYMLINK_NOFOLLOW) acts on the name
// itself. The caller re-checks the recorded socket identity and exact mode
// afterwards, so a name swapped before this call is refused.
func chmodSocketNoFollow(dirfd int, name string, _, _ uint64, mode uint32) error {
	return unix.Fchmodat(dirfd, name, mode, unix.AT_SYMLINK_NOFOLLOW)
}
