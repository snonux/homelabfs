//go:build linux

package controlledruntime

import (
	"errors"
	"strconv"

	"github.com/snonux/homelabfs/internal/fsapi"
	"golang.org/x/sys/unix"
)

// chmodSocketNoFollow changes the mode of the socket name in dirfd without
// following a symlink. It prefers fchmodat2(AT_SYMLINK_NOFOLLOW) (Linux 6.6+).
// That call reports EOPNOTSUPP both on older kernels and for a symlink name;
// either way the pinned-handle path below decides, and it refuses anything
// but the recorded socket inode.
func chmodSocketNoFollow(dirfd int, name string, dev, ino uint64, mode uint32) error {
	err := unix.Fchmodat(dirfd, name, mode, unix.AT_SYMLINK_NOFOLLOW)
	if !errors.Is(err, unix.EOPNOTSUPP) {
		return err
	}
	return chmodSocketByHandle(dirfd, name, dev, ino, mode)
}

// chmodSocketByHandle pins the name's own inode with an O_PATH|O_NOFOLLOW
// handle, requires it to be the recorded socket and changes the mode through
// that handle's /proc/self/fd link, which names exactly the pinned inode (the
// same technique glibc uses for fchmodat(AT_SYMLINK_NOFOLLOW)).
func chmodSocketByHandle(dirfd int, name string, dev, ino uint64, mode uint32) error {
	fd, err := unix.Openat(dirfd, name, unix.O_PATH|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer func() { _ = unix.Close(fd) }()
	var st unix.Stat_t
	if err = unix.Fstat(fd, &st); err != nil {
		return err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFSOCK || uint64(st.Dev) != dev || uint64(st.Ino) != ino {
		return fsapi.Permission
	}
	return unix.Chmod("/proc/self/fd/"+strconv.Itoa(fd), mode)
}
