/* SPDX-License-Identifier: BSD-2-Clause
 * Native production object linkage and raw-backing refusal, without mounting.
 */
#include "../src/hfs9p.h"
#include "../src/hfs9p_supervisor.h"
#include <sys/stat.h>
#include <stdlib.h>

#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>

static int
claim_cb(void *arg, const Hfs9pEndpoint *ep, const uint8_t hash[32]) {
   (void)arg;
   (void)ep;
   (void)hash;
   return (ENOTSUP);
}
static int
protect_cb(void *arg, const uint8_t *bytes, size_t length) {
   (void)arg;
   (void)bytes;
   (void)length;
   return (ENOTSUP);
}
static int
verify_cb(void *arg, const Hfs9pEndpoint *ep, const int fd[3]) {
   (void)arg;
   (void)ep;
   (void)fd;
   return (ENOTSUP);
}
int
main(void) {
   int fd[3] = {-1, -1, -1};
   Hfs9pEndpoint ep = {{0}, {1, 2, 3}};
   ep.session[15] = 1;
   Hfs9pVerifier verifier = {NULL, claim_cb, protect_cb, verify_cb, NULL, NULL};
   Hfs9p *endpoint = NULL;
   Hfs9pConnection *connection = NULL;
   assert(hfs9p_controller_claim("unimplemented", &connection) == EACCES);
   assert(!connection);
#ifndef __FreeBSD__
   char path[] = "/tmp/hfs-q04-production-XXXXXX";
   assert(mkdtemp(path));
   int directory = open(path, O_RDONLY | O_DIRECTORY | O_CLOEXEC);
   assert(directory >= 0);
   assert(hfs9p_supervisor_start(directory, "native", (uint8_t *)"{}", 2, 3)
       == ENOTSUP);
   struct stat st;
   assert(fstatat(directory, "native", &st, 0) == -1 && errno == ENOENT);
   close(directory);
#endif
   for (unsigned i = 0; i < 3; i++) {
      FILE *file = tmpfile();
      assert(file);
      fd[i] = dup(fileno(file));
      assert(fd[i] >= 0);
      assert(!fclose(file));
   }
   assert(hfs9p_new(&endpoint, fd, &ep, verifier) != 0);
   assert(!endpoint);
   for (unsigned i = 0; i < 3; i++)
      assert(!close(fd[i]));
   puts("PASS: production objects linked; absent enrollment/raw files refused");
   return (0);
}
