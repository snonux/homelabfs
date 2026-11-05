/* Narrow FreeBSD SYSTEM namespace helper for an explicitly approved window.
 * Compile against the selected native headers. No exec wrapper, process signal,
 * mountd notification, module load/unload, persistent policy or forced unmount.
 */
#include <sys/param.h>
#include <sys/mount.h>
#include <sys/stat.h>
#include <sys/uio.h>

#include <err.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static int
_current(struct statfs *found)
{
   struct statfs *mounts;
   int count, actual, matches = 0;

   count = getfsstat(NULL, 0, MNT_NOWAIT);
   if (count < 0)
      err(1, "getfsstat count");
   if (count > 4096)
      errx(1, "mount inventory bound");
   mounts = calloc((size_t)count + 1, sizeof(*mounts));
   if (mounts == NULL)
      err(1, "calloc");
   actual = getfsstat(mounts, ((size_t)count + 1) * sizeof(*mounts),
      MNT_NOWAIT);
   if (actual < 0)
      err(1, "getfsstat");
   if (actual > count)
      errx(1, "mount inventory changed");
   for (int index = 0; index < actual; ++index) {
      if (strncmp(mounts[index].f_mntonname, "/dev/fd/", 8) == 0)
         errx(1, "foreign descendant /dev/fd mount refused");
      if (strcmp(mounts[index].f_mntonname, "/dev/fd") != 0)
         continue;
      *found = mounts[index];
      ++matches;
   }
   free(mounts);
   if (matches > 1)
      errx(1, "stacked /dev/fd mounts refused");
   return (matches);
}

static void
_directory(void)
{
   struct stat info;

   if (lstat("/dev/fd", &info) != 0)
      err(1, "lstat /dev/fd");
   if (!S_ISDIR(info.st_mode) || info.st_uid != 0 ||
       (info.st_mode & 0022) != 0)
      errx(1, "protected existing /dev/fd directory required");
}

static void
_mount(void)
{
   struct statfs current;
   struct xvfsconf configuration;
   struct iovec options[] = {
      {"fstype", sizeof("fstype")}, {"fdescfs", sizeof("fdescfs")},
      {"fspath", sizeof("fspath")}, {"/dev/fd", sizeof("/dev/fd")},
      {"from", sizeof("from")}, {"fdescfs", sizeof("fdescfs")},
      {"nodup", sizeof("nodup")}, {NULL, 0},
      /* Generic vfs_donmount option sets 64-bit MNT_NOCOVER atomically.
       * nmount's int flags argument cannot carry that flag directly. */
      {"nocover", sizeof("nocover")}, {NULL, 0}
   };

   _directory();
   if (_current(&current) != 0)
      errx(1, "existing /dev/fd mount refused");
   /* getvfsbyname only queries the registered VFS. Never vfsload here. */
   if (getvfsbyname("fdescfs", &configuration) != 0)
      err(1, "fdescfs must already be registered");
   if (nmount(options, sizeof(options) / sizeof(options[0]), MNT_NOSUID) != 0)
      err(1, "nmount fdescfs nodup,nosuid /dev/fd");
   if (_current(&current) != 1 || strcmp(current.f_fstypename, "fdescfs") != 0)
      errx(1, "mount succeeded without complete ownership observation");
   printf("{\"mounted_fsid\":\"%08x:%08x\"}\n",
      (uint32_t)current.f_fsid.val[0], (uint32_t)current.f_fsid.val[1]);
}

static void
_unmount(const char *identity)
{
   struct statfs current;
   unsigned first, second;
   char tail;
   char selector[64];
   int length;

   if (strlen(identity) != 17 || identity[8] != ':' ||
       sscanf(identity, "%8x:%8x%c", &first, &second, &tail) != 2)
      errx(1, "exact FSID required");
   _directory();
   if (_current(&current) != 1 || strcmp(current.f_fstypename, "fdescfs") != 0 ||
       (uint32_t)current.f_fsid.val[0] != first ||
       (uint32_t)current.f_fsid.val[1] != second)
      errx(1, "owned FSID changed; no unmount");
   /* Select the exact FSID inside the kernel, rather than re-resolving a path
    * that another mount might cover between observation and unmount. */
   length = snprintf(selector, sizeof(selector), "FSID:%d:%d",
      (int32_t)first, (int32_t)second);
   if (length < 0 || (size_t)length >= sizeof(selector))
      errx(1, "FSID selector overflow");
   if (unmount(selector, MNT_BYFSID) != 0)
      err(1, "normal exact-FSID unmount /dev/fd");
   if (_current(&current) != 0)
      errx(1, "unmount returned without absent /dev/fd mount");
   printf("{\"unmounted_fsid\":\"%08x:%08x\"}\n", first, second);
}

int
main(int argc, char **argv)
{
   if (getuid() != 0 || geteuid() != 0)
      errx(1, "actual root required");
   if (argc == 2 && strcmp(argv[1], "mount") == 0)
      _mount();
   else if (argc == 3 && strcmp(argv[1], "unmount") == 0)
      _unmount(argv[2]);
   else
      errx(1, "usage: fdescfs-namespace mount | unmount EXACT_FSID");
   return (0);
}
