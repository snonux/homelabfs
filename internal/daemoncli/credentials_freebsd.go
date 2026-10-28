//go:build freebsd

package daemoncli

import (
	"os"
	"slices"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fuseadapter"
)

func (c selfCredentials) verifyCaller(caller fuseadapter.Caller) error {
	// Pinned FreeBSD fuse_ipc.c fdisp_make[_vp] uses td->td_proc->p_pid,
	// cr_uid and cr_gid. Only our still-running process is admitted. No
	// PID/account lookup is used, and this executable never changes groups.
	if caller.Thread != c.pid || uint32(os.Geteuid()) != c.uid || uint32(os.Getegid()) != c.gid {
		return fsapi.Permission
	}
	groups, err := os.Getgroups()
	if err != nil {
		return err
	}
	got := make([]uint32, len(groups))
	for i, g := range groups {
		got[i] = uint32(g)
	}
	slices.Sort(got)
	if !slices.Equal(got, c.groups) {
		return fsapi.Permission
	}
	return nil
}
