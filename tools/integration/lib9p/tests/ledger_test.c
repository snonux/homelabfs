/* SPDX-License-Identifier: BSD-2-Clause
 * Real temporary-file ledger durability/reopen and injected fsync failure.
 */
#include "../src/hfs9p_ledger.h"

#include <sys/stat.h>
#include <sys/wait.h>
#include <assert.h>
#include <errno.h>
#include <fcntl.h>
#ifdef __FreeBSD__
#include <sha256.h>
#else
#include <openssl/sha.h>
#endif
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

static bool fail_sync;
int __real_fsync(int);
int
__wrap_fsync(int fd) {
   if (fail_sync) {
      fail_sync = false;
      errno = EIO;
      return (-1);
   }
   return (__real_fsync(fd));
}

static void
payload_version(uint8_t bytes[128], unsigned serial, unsigned version) {
   memset(bytes, 0, 128);
   memcpy(bytes, "H9LEDG1\0", 8);
   bytes[6] = (uint8_t)('0' + version);
   bytes[23] = 5;
   bytes[24] = 110;
   bytes[32] = 111;
   bytes[40] = 112;
   bytes[60] = (uint8_t)serial;
#ifdef __FreeBSD__
   SHA256_CTX ctx;
   SHA256_Init(&ctx);
   SHA256_Update(&ctx, bytes, 96);
   SHA256_Final(bytes + 96, &ctx);
#else
   (void)SHA256(bytes, 96, bytes + 96);
#endif
}

int
main(int argc, char **argv) {
   assert(argc == 2);
   assert(!mkdir(argv[1], 0700));
   int directory = open(argv[1], O_RDONLY | O_DIRECTORY);
   assert(directory >= 0);
   Hfs9pLedger *l, *other = NULL;
   assert(!hfs9p_ledger_new(&l, directory, "endpoint"));
   assert(hfs9p_ledger_new(&other, directory, "endpoint") == EWOULDBLOCK);
   assert(!other);
   Hfs9pEndpoint ep = {{0}, {110, 111, 112}};
   ep.session[15] = 5;
   uint8_t zero[32] = {0}, first[128], second[128];
   assert(!hfs9p_ledger_claim(l, &ep, zero));
   payload_version(first, 1, 1);
   payload_version(second, 2, 1);
   assert(!hfs9p_ledger_protect(l, first, sizeof(first)));
   assert(hfs9p_ledger_claim(l, &ep, zero) == ESTALE);
   assert(!hfs9p_ledger_claim(l, &ep, first + 96));
   ep.node[0]++;
   assert(hfs9p_ledger_claim(l, &ep, first + 96) == ESTALE);
   ep.node[0]--;
   fail_sync = true;
   assert(hfs9p_ledger_protect(l, second, sizeof(second)) == EIO);
   uint8_t *loaded;
   size_t length;
   assert(!hfs9p_ledger_load(l, &loaded, &length));
   assert(length == sizeof(first) && !memcmp(loaded, first, length));
   free(loaded);
   assert(hfs9p_ledger_protect(l, second, sizeof(second)) == EEXIST);
   struct stat st;
   assert(!fstatat(directory, "endpoint.next", &st, 0));
   hfs9p_ledger_delete(l);
   /* Explicit test cleanup of this run-owned injected failure artifact only. */
   assert(!unlinkat(directory, "endpoint.next", 0));
   pid_t child = fork();
   assert(child >= 0);
   if (!child) {
      Hfs9pLedger *writer;
      assert(!hfs9p_ledger_new(&writer, directory, "endpoint"));
      assert(!hfs9p_ledger_protect(writer, second, sizeof(second)));
      _exit(0); /* crash-like process exit: no adapter destructor */
   }
   int status;
   assert(waitpid(child, &status, 0) == child);
   assert(WIFEXITED(status) && WEXITSTATUS(status) == 0);
   assert(!hfs9p_ledger_new(&l, directory, "endpoint"));
   assert(!hfs9p_ledger_load(l, &loaded, &length));
   assert(length == sizeof(second) && !memcmp(loaded, second, length));
   free(loaded);
   assert(!hfs9p_ledger_claim(l, &ep, second + 96));
   uint8_t extension[128];
   payload_version(extension, 3, 2);
   assert(!hfs9p_ledger_protect(l, extension, sizeof(extension)));
   assert(hfs9p_ledger_protect(l, second, sizeof(second)) == ESTALE);
   hfs9p_ledger_delete(l);
   assert(!hfs9p_ledger_new(&l, directory, "endpoint"));
   assert(!hfs9p_ledger_load(l, &loaded, &length));
   assert(length == sizeof(extension) && !memcmp(loaded, extension, length));
   free(loaded);
   assert(!hfs9p_ledger_claim(l, &ep, extension + 96));
   assert(hfs9p_ledger_protect(l, first, sizeof(first)) == ESTALE);
   hfs9p_ledger_delete(l);
   assert(!close(directory));
   puts("PASS: real local ledger fsync/rename/reopen, lock, process exit, failure");
   return (0);
}
