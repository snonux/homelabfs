//go:build linux

package controlledruntime

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/snonux/homelabfs/internal/fsapi"
	"golang.org/x/sys/unix"
)

// Linux is a process/fd primitive fixture boundary, never native FreeBSD proof.
func observeProcess(pid int) (processIdentity, error) {
	var result processIdentity
	status, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return result, err
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(status), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "Uid:", "Gid:":
			if len(fields) != 5 {
				return result, fsapi.Permission
			}
			// Refuse mixed real/effective/saved/filesystem credentials.
			for _, field := range fields[2:] {
				if field != fields[1] {
					return result, fsapi.Permission
				}
			}
			n, e := strconv.ParseUint(fields[1], 10, 32)
			if e != nil {
				return result, e
			}
			if fields[0] == "Uid:" {
				result.UID = uint32(n)
			} else {
				result.GID = uint32(n)
			}
			seen[fields[0]] = true
		case "Groups:":
			if len(fields) > 1025 {
				return result, fsapi.NoSpace
			}
			for _, field := range fields[1:] {
				n, e := strconv.ParseUint(field, 10, 32)
				if e != nil {
					return result, e
				}
				result.Groups = append(result.Groups, uint32(n))
			}
			seen[fields[0]] = true
		}
	}
	// Empty supplementary group lists are valid and still complete.
	if !strings.Contains(string(status), "Groups:\t") || !seen["Uid:"] || !seen["Gid:"] {
		return result, fsapi.Permission
	}
	slices.Sort(result.Groups)
	stat, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return result, err
	}
	end := strings.LastIndexByte(string(stat), ')')
	if end < 0 {
		return result, fsapi.Permission
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 20 || fields[0] == "Z" {
		return result, fsapi.Unavailable
	}
	result.PID = uint32(pid)
	result.Incarnation = fmt.Sprintf("linux:%d:%s", pid, fields[19])
	var st unix.Stat_t
	if err = unix.Stat(fmt.Sprintf("/proc/%d/exe", pid), &st); err != nil {
		return result, err
	}
	result.Device = uint64(st.Dev)
	result.Inode = st.Ino
	return result, nil
}
