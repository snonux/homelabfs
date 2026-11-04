/* SPDX-License-Identifier: BSD-2-Clause
 * Real inherited HSB1 parser/startup thread/pending vqueue refusal. Linux's
 * production native constructor still returns ENOTSUP; no FUSE proof. */
#include "../src/hfs9p_supervisor.c"
#include <assert.h>

static void
_write_all(int fd, const void *data, size_t length) {
   const uint8_t *p = data;
   while (length) {
      ssize_t n = write(fd, p, length);
      assert(n > 0); p += n; length -= (size_t)n;
   }
}

int
main(void) {
   int descriptors[2]; assert(!pipe(descriptors));
   int writer = fcntl(descriptors[1], F_DUPFD_CLOEXEC, 20);
   assert(writer >= 20);
   assert(dup2(descriptors[0], 6) == 6);
   if (descriptors[0] != 6) close(descriptors[0]);
   if (descriptors[1] != 6) close(descriptors[1]);
   Hfs9pPending *pending[16] = {0}, *other = NULL;
   int64_t start = _now();
   for (unsigned i = 0; i < 16; i++) {
      char name[37];
      (void)snprintf(name, sizeof(name),
          "00000000-0000-0000-0000-%012x", i + 1);
      assert(!hfs9p_supervisor_prepare(name, &pending[i]));
      assert(pending[i]);
   }
   assert(_now() - start < 1000); /* does not wait on the bootstrap pipe */
   assert(hfs9p_supervisor_prepare("00000000-0000-0000-0000-000000000001",
       &other) != 0 && !other);
   assert(hfs9p_supervisor_prepare("00000000-0000-0000-0000-000000000017",
       &other) != 0 && !other);
   assert(hfs9p_supervisor_prepare("path/from/guest", &other) == EINVAL);
   uint8_t request[7] = {7,0,0,0,100,0x34,0x12}, reply[11];
   size_t returned = 0;
   assert(!hfs9p_pending_exchange(pending[0], request, sizeof(request),
       reply, sizeof(reply), &returned));
   assert(returned == 11 && reply[4] == 7 && reply[5] == 0x34 &&
       reply[6] == 0x12 && reply[7] == 11);
   assert(hfs9p_pending_exchange(pending[0], request, sizeof(request),
       reply, 10, &returned) == EMSGSIZE);
   /* Valid framing but duplicate scope inventory refuses before native startup.
    * The VM process remains alive and future dedicated requests return EIO. */
   const char scope[] = "{\"session\":\"00000000-0000-0000-0000-000000000001\"}";
   uint8_t header[16] = {'H','S','B','1'};
   _put(header + 4, 2, 2); _put(header + 6, 4, 2);
   _put(header + 8, 32767, 4);
   _put(header + 12, 2 * (sizeof(scope) - 1 + 2), 4);
   _write_all(writer, header, sizeof(header));
   uint8_t size[2]; _put(size, sizeof(scope) - 1, 2);
   for (unsigned i = 0; i < 2; i++) {
      _write_all(writer, size, 2); _write_all(writer, scope, sizeof(scope)-1);
   }
   close(writer);
   for (unsigned i = 0; i < 200 && !atomic_load(&PENDING_FAILED); i++) {
      struct timespec wait = {0, 1000000}; nanosleep(&wait, NULL);
   }
   assert(atomic_load(&PENDING_FAILED));
   assert(!hfs9p_pending_exchange(pending[1], request, sizeof(request),
       reply, sizeof(reply), &returned));
   assert(returned == 11 && reply[7] == 5);
   assert(!OWNER.ready && !OWNER.backend);
   assert(hfs9p_supervisor_bootstrap() == EALREADY);
   puts("PASS: real bounded asynchronous pending devices and duplicate-inventory refusal; VM process survives");
   return (0);
}
