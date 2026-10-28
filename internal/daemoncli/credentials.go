package daemoncli

import (
	"context"
	"fmt"
	"os"
	"slices"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fuseadapter"
)

// selfCredentials deliberately admits only this still-running executable's
// built-in client. This binary never changes credentials, shares descriptors
// with external processes or forks workload clients. It is not a general
// credential provider and cannot be selected for normal publication.
type selfCredentials struct {
	uid, gid, pid uint32
	groups        []uint32
}

var _ fuseadapter.Credentials = selfCredentials{}

func newSelfCredentials() (selfCredentials, error) {
	groups, err := os.Getgroups()
	if err != nil {
		return selfCredentials{}, err
	}
	c := selfCredentials{uid: uint32(os.Geteuid()), gid: uint32(os.Getegid()), pid: uint32(os.Getpid())}
	for _, group := range groups {
		c.groups = append(c.groups, uint32(group))
	}
	slices.Sort(c.groups)
	if len(c.groups) > 1024 {
		return c, fmt.Errorf("validation group limit exceeded")
	}
	return c, nil
}

func (c selfCredentials) Snapshot(ctx context.Context, caller fuseadapter.Caller) (fuseadapter.Identity, error) {
	if err := ctx.Err(); err != nil {
		return fuseadapter.Identity{}, err
	}
	if caller.UID != c.uid || caller.GID != c.gid || caller.Thread == 0 {
		return fuseadapter.Identity{}, fsapi.Permission
	}
	if err := c.verifyCaller(caller); err != nil {
		return fuseadapter.Identity{}, err
	}
	return fuseadapter.Identity{Credentials: fsapi.Cred{UID: c.uid, GID: c.gid, PID: caller.Thread, Groups: slices.Clone(c.groups)}, Process: 1, ProcessID: c.pid}, nil
}
