//go:build freebsd

package controlledruntime

import (
	"bytes"
	"fmt"

	"github.com/snonux/homelabfs/internal/fsapi"
	"golang.org/x/sys/unix"
)

func descriptorExecMount(report *descriptorExecReport) error {
	var mount unix.Statfs_t
	if err := unix.Statfs("/dev/fd", &mount); err != nil {
		return err
	}
	report.MountType = string(bytes.TrimRight(mount.Fstypename[:], "\x00"))
	report.MountID = fmt.Sprintf("%08x:%08x", uint32(mount.Fsid.Val[0]), uint32(mount.Fsid.Val[1]))
	report.Flags = mount.Flags
	if report.MountType != "fdescfs" || string(bytes.TrimRight(mount.Mntonname[:], "\x00")) != "/dev/fd" || mount.Flags&unix.MNT_NOEXEC != 0 {
		return fsapi.Unsupported
	}
	return nil
}

func descriptorExecAlias(path string, st *unix.Stat_t) error {
	// nodup returns the held vnode itself. Default fdescfs returns VCHR;
	// rdlnk returns a pathname symlink and would discard the FD pinning guarantee.
	return unix.Lstat(path, st)
}

func descriptorExecFile(fd int) error {
	var mount unix.Statfs_t
	if err := unix.Fstatfs(fd, &mount); err != nil {
		return err
	}
	if mount.Flags&unix.MNT_NOEXEC != 0 {
		return fsapi.Unsupported
	}
	return nil
}
