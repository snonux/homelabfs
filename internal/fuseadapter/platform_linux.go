//go:build linux

package fuseadapter

import (
	"github.com/snonux/homelabfs/internal/fsapi"
	"golang.org/x/sys/unix"
)

const dataSyncFlag = unix.O_DSYNC

func platformMountOptions() []string {
	return []string{"default_permissions", "nosuid", "nodev"}
}

func mountedIdentity(fd int) ([]byte, error) {
	var st unix.Statfs_t
	if err := unix.Fstatfs(fd, &st); err != nil {
		return nil, err
	}
	if st.Type != unix.FUSE_SUPER_MAGIC {
		return nil, fsapi.Stale
	}
	buf := make([]byte, 256)
	n, err := unix.Fgetxattr(fd, identityAttribute, buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

func multiuserAvailable() bool { return unix.Geteuid() == 0 }
