//go:build freebsd && cgo

package controlledruntime

/*
#include <sys/types.h>
#include <sys/param.h>
#include <sys/sysctl.h>
#include <sys/user.h>
#include <sys/socket.h>
#include <sys/capsicum.h>
#include <sys/stat.h>
#include <fcntl.h>
#include <errno.h>
#include <stddef.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

#define HFS_GROUPS 1024

typedef struct {
 uint32_t uid, gid, pid, groups[HFS_GROUPS];
 size_t ngroups;
 int64_t seconds, micros;
 uintptr_t text_vnode;
 char executable[PATH_MAX];
} HfsProcess;

static int
hfs_process(pid_t pid, HfsProcess *out)
{
 int mib[4] = {CTL_KERN, KERN_PROC, KERN_PROC_PID, pid};
 struct kinfo_proc before, after;
 size_t size = sizeof(before);
 if (sysctl(mib, 4, &before, &size, NULL, 0) == -1) return errno;
 if (size != sizeof(before) || before.ki_pid != pid ||
     before.ki_stat == SZOMB || before.ki_uid != before.ki_ruid ||
     before.ki_uid != before.ki_svuid) return EACCES;
 int groups_mib[4] = {CTL_KERN, KERN_PROC, KERN_PROC_GROUPS, pid};
 size = sizeof(out->groups);
 if (sysctl(groups_mib, 4, out->groups, &size, NULL, 0) == -1) return errno;
 if (size == 0 || size % sizeof(gid_t) != 0) return EACCES;
 out->ngroups = size / sizeof(gid_t);
 if (out->ngroups > HFS_GROUPS || out->groups[0] != before.ki_rgid ||
     out->groups[0] != before.ki_svgid) return EACCES;
 out->uid = before.ki_uid; out->gid = out->groups[0]; out->pid = pid;
 out->seconds = before.ki_start.tv_sec; out->micros = before.ki_start.tv_usec;
 char path[PATH_MAX];
 int path_mib[4] = {CTL_KERN, KERN_PROC, KERN_PROC_PATHNAME, pid};
 size = sizeof(path);
 if (sysctl(path_mib, 4, path, &size, NULL, 0) == -1) return errno;
 if (size == 0 || size > sizeof(path) || path[size - 1] != '\0') return EACCES;
 memcpy(out->executable, path, size);
 out->text_vnode = (uintptr_t)before.ki_textvp;
 if (out->text_vnode == 0) return EACCES;
 size = sizeof(after);
 if (sysctl(mib, 4, &after, &size, NULL, 0) == -1) return errno;
 if (size != sizeof(after) || before.ki_pid != after.ki_pid ||
     before.ki_uid != after.ki_uid || before.ki_textvp != after.ki_textvp || before.ki_start.tv_sec != after.ki_start.tv_sec ||
     before.ki_start.tv_usec != after.ki_start.tv_usec) return EACCES;
 return 0;
}

*/
import "C"

import (
	"fmt"
	"slices"
	"syscall"
)

func observeProcess(pid int) (processIdentity, error) {
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
