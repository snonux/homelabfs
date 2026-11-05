/* SPDX-License-Identifier: BSD-2-Clause */
#ifndef HFS_PROCESS_FREEBSD_H
#define HFS_PROCESS_FREEBSD_H

#include <sys/types.h>
#include <sys/param.h>
#include <sys/sysctl.h>
#include <sys/proc.h>
#include <sys/user.h>
#include <errno.h>
#include <stdint.h>
#include <string.h>

#define HFS_PROCESS_GROUPS 1024

_Static_assert(sizeof(uid_t) == sizeof(uint32_t), "unsupported uid_t ABI");
_Static_assert(sizeof(pid_t) == sizeof(int32_t), "unsupported pid_t ABI");
_Static_assert(sizeof(gid_t) == sizeof(uint32_t), "unsupported gid_t ABI");

typedef struct {
   uint32_t uid, gid, pid;
   uint32_t groups[HFS_PROCESS_GROUPS];
   size_t ngroups;
   int64_t seconds, micros;
   uintptr_t text_vnode;
   char executable[PATH_MAX];
} HfsProcess;

static int
hfs_process_info(pid_t pid, struct kinfo_proc *info)
{
   int mib[4] = {CTL_KERN, KERN_PROC, KERN_PROC_PID, pid};
   size_t size = sizeof(*info);

   memset(info, 0, sizeof(*info));
   if (sysctl(mib, 4, info, &size, NULL, 0) == -1)
      return (errno);
   if (size != sizeof(*info) || (size_t)info->ki_structsize != sizeof(*info) ||
       info->ki_pid != pid || info->ki_stat == SZOMB ||
       info->ki_uid != info->ki_ruid || info->ki_uid != info->ki_svuid ||
       info->ki_ngroups < 1 || info->ki_groups[0] != info->ki_rgid ||
       info->ki_groups[0] != info->ki_svgid || info->ki_textvp == NULL ||
       info->ki_start.tv_sec <= 0 || info->ki_start.tv_usec < 0 ||
       info->ki_start.tv_usec >= 1000000)
      return (EACCES);
   return (0);
}

static int
hfs_process_groups(pid_t pid, HfsProcess *out)
{
   int mib[4] = {CTL_KERN, KERN_PROC, KERN_PROC_GROUPS, pid};
   size_t size = sizeof(out->groups);

   if (sysctl(mib, 4, out->groups, &size, NULL, 0) == -1)
      return (errno);
   if (size == 0 || size > sizeof(out->groups) || size % sizeof(gid_t) != 0)
      return (EACCES);
   out->ngroups = size / sizeof(gid_t);
   return (0);
}

static int
hfs_process_path(pid_t pid, HfsProcess *out)
{
   int mib[4] = {CTL_KERN, KERN_PROC, KERN_PROC_PATHNAME, pid};
   size_t size = sizeof(out->executable);

   if (sysctl(mib, 4, out->executable, &size, NULL, 0) == -1)
      return (errno);
   if (size < 2 || size > sizeof(out->executable) ||
       out->executable[0] != '/' || out->executable[size - 1] != '\0' ||
       strnlen(out->executable, size) != size - 1)
      return (EACCES);
   return (0);
}

/* Take complete independent group/path samples between the process
 * snapshots. A size-limited sysctl failure is never a truncated success. */
static int
hfs_process(pid_t pid, HfsProcess *out)
{
   struct kinfo_proc before, after;
   HfsProcess second = {0};
   int error;

   if (pid <= 0)
      return (EINVAL);
   memset(out, 0, sizeof(*out));
   if ((error = hfs_process_info(pid, &before)) != 0 ||
       (error = hfs_process_groups(pid, out)) != 0 ||
       (error = hfs_process_path(pid, out)) != 0 ||
       (error = hfs_process_groups(pid, &second)) != 0 ||
       (error = hfs_process_path(pid, &second)) != 0 ||
       (error = hfs_process_info(pid, &after)) != 0)
      return (error);
   if (before.ki_uid != after.ki_uid ||
       before.ki_ngroups != after.ki_ngroups ||
       before.ki_groups[0] != after.ki_groups[0] ||
       before.ki_textvp != after.ki_textvp ||
       before.ki_start.tv_sec != after.ki_start.tv_sec ||
       before.ki_start.tv_usec != after.ki_start.tv_usec ||
       out->ngroups != second.ngroups ||
       memcmp(out->groups, second.groups, out->ngroups * sizeof(gid_t)) != 0 ||
       out->groups[0] != before.ki_groups[0] ||
       strcmp(out->executable, second.executable) != 0)
      return (EACCES);
   out->uid = before.ki_uid;
   out->gid = out->groups[0];
   out->pid = pid;
   out->seconds = before.ki_start.tv_sec;
   out->micros = before.ki_start.tv_usec;
   out->text_vnode = (uintptr_t)before.ki_textvp;
   return (0);
}

#endif
