//go:build freebsd && cgo

package controlledruntime

/*
#include "process_freebsd.h"
*/
import "C"

import (
	"fmt"
	"slices"
	"syscall"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func observeProcess(pid int) (processIdentity, error) {
	if pid <= 0 || int64(pid) > 1<<31-1 {
		return processIdentity{}, fsapi.Invalid
	}
	var native C.HfsProcess
	if code := C.hfs_process(C.pid_t(pid), &native); code != 0 {
		return processIdentity{}, syscall.Errno(code)
	}
	identity := processIdentity{UID: uint32(native.uid), GID: uint32(native.gid), PID: uint32(native.pid), Incarnation: fmt.Sprintf("freebsd:%d:%d:%d:%x", pid, int64(native.seconds), int64(native.micros), uint64(native.text_vnode)), ExecutablePath: C.GoString(&native.executable[0]), TextVnode: uint64(native.text_vnode)}
	for _, group := range native.groups[:int(native.ngroups)] {
		identity.Groups = append(identity.Groups, uint32(group))
	}
	slices.Sort(identity.Groups)
	return identity, nil
}
