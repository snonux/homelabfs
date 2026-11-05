/* SPDX-License-Identifier: BSD-2-Clause
 * Explicit sysctl boundary fixtures using the actual selected FreeBSD headers.
 * This harness never starts processes and is not native observation evidence.
 */
#include <sys/types.h>
#include <sys/param.h>
#include <sys/sysctl.h>
#include <sys/user.h>
#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int fixture_sysctl(const int *, u_int, void *, size_t *,
    const void *, size_t);
#define sysctl fixture_sysctl
#include "../process_freebsd.h"
#undef sysctl

typedef struct {
   struct kinfo_proc info[2];
   uint32_t groups[2][HFS_PROCESS_GROUPS];
   size_t group_bytes[2];
   char path[2][PATH_MAX];
   size_t path_bytes[2];
   unsigned int calls, infos, group_calls, paths, fail_at;
   size_t info_bytes;
} Fixture;

static Fixture fixture;

static void
reset_fixture(void)
{
   memset(&fixture, 0, sizeof(fixture));
   fixture.info_bytes = sizeof(struct kinfo_proc);
   for (unsigned int i = 0; i < 2; i++) {
      fixture.info[i].ki_structsize = sizeof(struct kinfo_proc);
      fixture.info[i].ki_pid = 123;
      fixture.info[i].ki_stat = 1;
      fixture.info[i].ki_uid = 1000;
      fixture.info[i].ki_ruid = 1000;
      fixture.info[i].ki_svuid = 1000;
      fixture.info[i].ki_ngroups = 2;
      fixture.info[i].ki_groups[0] = 1001;
      fixture.info[i].ki_rgid = 1001;
      fixture.info[i].ki_svgid = 1001;
      fixture.info[i].ki_textvp = (struct vnode *)(uintptr_t)0x100;
      fixture.info[i].ki_start.tv_sec = 100;
      fixture.info[i].ki_start.tv_usec = 200;
      fixture.groups[i][0] = 1001;
      fixture.groups[i][1] = 1002;
      fixture.group_bytes[i] = 2 * sizeof(gid_t);
      strcpy(fixture.path[i], "/owned/exporter");
      fixture.path_bytes[i] = strlen(fixture.path[i]) + 1;
   }
}

static int
fixture_sysctl(const int *mib, u_int count, void *out, size_t *size,
    const void *input, size_t input_size)
{
   const void *source;
   size_t length;

   fixture.calls++;
   if (fixture.calls == fixture.fail_at) {
      errno = EPERM;
      return (-1);
   }
   if (count != 4 || mib[0] != CTL_KERN || mib[1] != KERN_PROC ||
       mib[3] != 123 || input != NULL || input_size != 0)
      abort();
   switch (mib[2]) {
   case KERN_PROC_PID:
      if (fixture.infos >= 2) abort();
      source = &fixture.info[fixture.infos++];
      length = fixture.info_bytes;
      break;
   case KERN_PROC_GROUPS:
      if (fixture.group_calls >= 2) abort();
      source = fixture.groups[fixture.group_calls];
      length = fixture.group_bytes[fixture.group_calls++];
      break;
   case KERN_PROC_PATHNAME:
      if (fixture.paths >= 2) abort();
      source = fixture.path[fixture.paths];
      length = fixture.path_bytes[fixture.paths++];
      break;
   default:
      abort();
   }
   if (length > *size) {
      errno = ENOMEM;
      return (-1);
   }
   memcpy(out, source, length);
   *size = length;
   return (0);
}

static void
expect_refusal(const char *name)
{
   HfsProcess observed;

   if (hfs_process(123, &observed) == 0) {
      fprintf(stderr, "accepted invalid boundary: %s\n", name);
      exit(1);
   }
}

int
main(void)
{
   HfsProcess observed;
   unsigned int negatives = 0;

   reset_fixture();
   if (hfs_process(123, &observed) != 0 || observed.pid != 123 ||
       observed.uid != 1000 || observed.gid != 1001 || observed.ngroups != 2 ||
       observed.groups[1] != 1002 || observed.seconds != 100 ||
       observed.micros != 200 || observed.text_vnode != 0x100 ||
       strcmp(observed.executable, "/owned/exporter") != 0)
      return (1);
   reset_fixture();
   for (unsigned int i = 0; i < 2; i++) {
      fixture.group_bytes[i] = sizeof(fixture.groups[i]);
      for (unsigned int j = 1; j < HFS_PROCESS_GROUPS; j++)
         fixture.groups[i][j] = 2000 + j;
   }
   if (hfs_process(123, &observed) != 0 ||
       observed.ngroups != HFS_PROCESS_GROUPS ||
       observed.groups[HFS_PROCESS_GROUPS - 1] != 2000 + HFS_PROCESS_GROUPS - 1)
      return (1);
   for (unsigned int sample = 0; sample < 2; sample++) {
      for (unsigned int kind = 0; kind < 16; kind++) {
         reset_fixture();
         switch (kind) {
         case 0: fixture.info[sample].ki_pid++; break;
         case 1: fixture.info[sample].ki_structsize--; break;
         case 2: fixture.info[sample].ki_stat = SZOMB; break;
         case 3: fixture.info[sample].ki_ruid++; break;
         case 4: fixture.info[sample].ki_svuid++; break;
         case 5: fixture.info[sample].ki_rgid++; break;
         case 6: fixture.info[sample].ki_svgid++; break;
         case 7: fixture.info[sample].ki_textvp = NULL; break;
         case 8: fixture.info[sample].ki_start.tv_sec = 0; break;
         case 9: fixture.info[sample].ki_start.tv_usec = -1; break;
         case 10: fixture.info[sample].ki_start.tv_usec = 1000000; break;
         case 11: fixture.group_bytes[sample] = 0; break;
         case 12: fixture.group_bytes[sample]--; break;
         case 13: fixture.group_bytes[sample] = sizeof(fixture.groups[sample]) + 4; break;
         case 14: fixture.groups[sample][0]++; break;
         case 15: fixture.info[sample].ki_ngroups = 0; break;
         }
         expect_refusal("credentials/state/groups");
         negatives++;
      }
      for (unsigned int kind = 0; kind < 5; kind++) {
         reset_fixture();
         switch (kind) {
         case 0: fixture.path_bytes[sample] = 0; break;
         case 1: fixture.path_bytes[sample] = PATH_MAX + 1; break;
         case 2: fixture.path[sample][0] = 'x'; break;
         case 3: fixture.path[sample][2] = '\0'; break;
         case 4: fixture.path[sample][fixture.path_bytes[sample] - 1] = 'x'; break;
         }
         expect_refusal("pathname");
         negatives++;
      }
   }
   for (unsigned int kind = 0; kind < 8; kind++) {
      reset_fixture();
      switch (kind) {
      case 0: fixture.info[1].ki_start.tv_sec++; break;
      case 1: fixture.info[1].ki_start.tv_usec++; break;
      case 2: fixture.info[1].ki_textvp = (struct vnode *)(uintptr_t)0x200; break;
      case 3: fixture.groups[1][1]++; break;
      case 4: fixture.group_bytes[1] = sizeof(gid_t); break;
      case 5: fixture.path[1][1] = 'x'; break;
      case 7: fixture.info[1].ki_ngroups++; break;
      case 6:
         fixture.info[1].ki_uid++;
         fixture.info[1].ki_ruid++;
         fixture.info[1].ki_svuid++;
         break;
      }
      expect_refusal("stale identity");
      negatives++;
   }
   for (unsigned int call = 1; call <= 6; call++) {
      reset_fixture();
      fixture.fail_at = call;
      expect_refusal("sysctl error");
      negatives++;
   }
   reset_fixture();
   fixture.info_bytes--;
   expect_refusal("ABI length");
   negatives++;
   reset_fixture();
   if (hfs_process(0, &observed) == 0 || hfs_process(-1, &observed) == 0 ||
       fixture.calls != 0)
      return (1);
   printf("{\"boundary_fixture\":true,\"positive_cases\":2,"
       "\"negative_cases\":%u,\"native_observation\":false}\n", negatives + 2);
   return (0);
}
