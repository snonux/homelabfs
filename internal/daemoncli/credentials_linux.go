//go:build linux

package daemoncli

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fuseadapter"
)

func (c selfCredentials) verifyCaller(caller fuseadapter.Caller) error {
	// The self/task namespace cannot resolve a foreign process. Linux FUSE
	// headers carry the requesting TID and filesystem IDs. The controlled
	// process never changes IDs/groups, so later thread snapshots cannot confer
	// authority from a different account or process incarnation.
	raw, err := os.ReadFile(fmt.Sprintf("/proc/self/task/%d/status", caller.Thread))
	if err != nil {
		return err
	}
	var tgid, uid, gid uint32
	var groups []uint32
	var haveTgid, haveUID, haveGID, haveGroups bool
	for _, line := range strings.Split(string(raw), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch fields[0] {
		case "Tgid:":
			if len(fields) != 2 {
				return fsapi.Permission
			}
			tgid, err = parseID(fields[1])
			haveTgid = err == nil
		case "Uid:", "Gid:":
			if len(fields) != 5 {
				return fsapi.Permission
			}
			if fields[0] == "Uid:" {
				uid, err = parseID(fields[4])
				haveUID = err == nil
			} else {
				gid, err = parseID(fields[4])
				haveGID = err == nil
			}
		case "Groups:":
			haveGroups = true
			for _, raw := range fields[1:] {
				var group uint32
				group, err = parseID(raw)
				if err != nil {
					return err
				}
				groups = append(groups, group)
			}
		}
		if err != nil {
			return err
		}
	}
	slices.Sort(groups)
	if !haveTgid || !haveUID || !haveGID || !haveGroups || tgid != c.pid || uid != c.uid || gid != c.gid || !slices.Equal(groups, c.groups) {
		return fsapi.Permission
	}
	return nil
}

func parseID(raw string) (uint32, error) {
	n, err := strconv.ParseUint(raw, 10, 32)
	return uint32(n), err
}
