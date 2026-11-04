/*:*
 * Durable endpoint forwarding ledger. SPDX-License-Identifier: BSD-2-Clause
 * Copyright (c) 2026 HomelabFS contributors.
 *:*/
#include "hfs9p_ledger.h"

#include <sys/file.h>
#include <sys/stat.h>
#ifdef __FreeBSD__
#include <sha256.h>
#else
#include <openssl/sha.h>
#endif

#include <errno.h>
#include <fcntl.h>
#include <stdbool.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#define LEDGER_MAX (UINT64_C(65) << 20)
struct Hfs9pLedger_ {
   int directory;
   int lock;
   char name[65];
   char next[71];
};

static void
_ledger_hash(const uint8_t *bytes, size_t length, uint8_t hash[32]) {
#ifdef __FreeBSD__
   SHA256_CTX ctx;
   SHA256_Init(&ctx);
   SHA256_Update(&ctx, bytes, length);
   SHA256_Final(hash, &ctx);
#else
   (void)SHA256(bytes, length, hash);
#endif
}

static int
_ledger_check(const uint8_t *bytes, size_t length) {
   uint8_t hash[32];
   if (!bytes || length < 64 || length > LEDGER_MAX ||
       (memcmp(bytes, "H9LEDG1\0", 8) &&
       memcmp(bytes, "H9LEDG2\0", 8) &&
       memcmp(bytes, "H9LEDG3\0", 8)))
      return (EINVAL);
   _ledger_hash(bytes, length - 32, hash);
   return (memcmp(hash, bytes + length - 32, 32) ? EINVAL : 0);
}

int
hfs9p_ledger_new(Hfs9pLedger **out, int directory, const char *name) {
   struct stat st;
   char lock_name[71];
   if (!out || !name || !*name || strlen(name) > 64 ||
       !strcmp(name, ".") || !strcmp(name, "..") || strchr(name, '/') ||
       fstat(directory, &st) || !S_ISDIR(st.st_mode) ||
       st.st_uid != geteuid() || (st.st_mode & 077))
      return (EINVAL);
   *out = NULL;
   Hfs9pLedger *l = calloc(1, sizeof(*l));
   if (!l)
      return (ENOMEM);
   l->directory = fcntl(directory, F_DUPFD_CLOEXEC, 3);
   if (l->directory < 0) {
      int error = errno;
      free(l);
      return (error);
   }
   (void)snprintf(l->name, sizeof(l->name), "%s", name);
   (void)snprintf(l->next, sizeof(l->next), "%s.next", name);
   (void)snprintf(lock_name, sizeof(lock_name), "%s.lock", name);
   l->lock = openat(l->directory, lock_name,
       O_RDWR | O_CREAT | O_NOFOLLOW | O_CLOEXEC, 0600);
   int error = l->lock < 0 ? errno : 0;
   if (!error && (fstat(l->lock, &st) || !S_ISREG(st.st_mode) ||
       st.st_nlink != 1 || st.st_uid != geteuid() ||
       (st.st_mode & 077) != 0))
      error = EACCES;
   if (!error && flock(l->lock, LOCK_EX | LOCK_NB))
      error = errno;
   if (error) {
      if (l->lock >= 0)
         (void)close(l->lock);
      (void)close(l->directory);
      free(l);
      return (error);
   }
   *out = l;
   return (0);
}

void
hfs9p_ledger_delete(Hfs9pLedger *l) {
   if (!l)
      return;
   (void)close(l->lock);
   (void)close(l->directory);
   free(l);
}

int
hfs9p_ledger_load(Hfs9pLedger *l, uint8_t **out, size_t *length) {
   if (!l || !out || !length)
      return (EINVAL);
   *out = NULL;
   *length = 0;
   int fd = openat(l->directory, l->name, O_RDONLY | O_NOFOLLOW | O_CLOEXEC);
   if (fd < 0)
      return (errno);
   struct stat st;
   int error = 0;
   if (fstat(fd, &st) || !S_ISREG(st.st_mode) || st.st_nlink != 1 ||
       st.st_uid != geteuid() || (st.st_mode & 077) || st.st_size < 64 || (uint64_t)st.st_size > LEDGER_MAX)
      error = EINVAL;
   uint8_t *bytes = error ? NULL : malloc((size_t)st.st_size);
   if (!error && !bytes)
      error = ENOMEM;
   size_t done = 0;
   while (!error && done < (size_t)st.st_size) {
      ssize_t n = read(fd, bytes + done, (size_t)st.st_size - done);
      if (n < 0 && errno == EINTR)
         continue;
      if (n <= 0)
         error = n < 0 ? errno : EIO;
      else
         done += (size_t)n;
   }
   if (close(fd) && !error)
      error = errno;
   if (!error)
      error = _ledger_check(bytes, done);
   if (error) {
      free(bytes);
      return (error);
   }
   *out = bytes;
   *length = done;
   return (0);
}

int
hfs9p_ledger_claim(Hfs9pLedger *l, const Hfs9pEndpoint *endpoint,
    const uint8_t digest[32]) {
   uint8_t *bytes = NULL, zero[32] = {0};
   size_t length = 0;
   if (!l || !endpoint || !digest)
      return (EINVAL);
   int error = hfs9p_ledger_load(l, &bytes, &length);
   if (error == ENOENT && !memcmp(digest, zero, 32)) {
      /* Freshness still requires the independent supervised host enrollment
       * provider to reject an issued endpoint whose local bytes were lost. */
      struct stat st;
      if (!fstatat(l->directory, l->next, &st, AT_SYMLINK_NOFOLLOW))
         return (ESTALE);
      return (errno == ENOENT ? 0 : errno);
   }
   if (!error && (memcmp(bytes + 8, endpoint->session, 16) ||
       memcmp(bytes + length - 32, digest, 32)))
      error = ESTALE;
   if (!error) {
      for (unsigned i = 0; i < 3; i++) {
         uint64_t node = 0;
         for (unsigned k = 0; k < 8; k++)
            node |= (uint64_t)bytes[24 + i * 8 + k] << (8 * k);
         if (node != endpoint->node[i])
            error = ESTALE;
      }
   }
   free(bytes);
   return (error);
}

int
hfs9p_ledger_protect(Hfs9pLedger *l, const uint8_t *bytes, size_t length) {
   if (!l)
      return (EINVAL);
   int error = _ledger_check(bytes, length);
   if (error)
      return (error);
   uint8_t *previous = NULL;
   size_t previous_length = 0;
   int prior_error = hfs9p_ledger_load(l, &previous, &previous_length);
   if (!prior_error && previous[6] > bytes[6])
      prior_error = ESTALE;
   free(previous);
   if (prior_error && prior_error != ENOENT)
      return (prior_error);
   int fd = openat(l->directory, l->next,
       O_WRONLY | O_CREAT | O_EXCL | O_NOFOLLOW | O_CLOEXEC, 0600);
   if (fd < 0)
      return (errno);
   size_t done = 0;
   while (done < length) {
      ssize_t n = write(fd, bytes + done, length - done);
      if (n < 0 && errno == EINTR)
         continue;
      if (n <= 0) {
         error = n < 0 ? errno : EIO;
         break;
      }
      done += (size_t)n;
   }
   if (!error && fsync(fd))
      error = errno;
   if (close(fd) && !error)
      error = errno;
   if (!error && renameat(l->directory, l->next, l->directory, l->name))
      error = errno;
   if (!error && fsync(l->directory))
      error = errno;
   /* Failed .next bytes are deliberately preserved. No caller may delete
    * them to turn uncertainty into a fresh endpoint or native admission. */
   return (error);
}
