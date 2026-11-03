/* SPDX-License-Identifier: BSD-2-Clause
 * Portable real socket/fd primitives only; never native FUSE evidence. */
#include "../src/hfs9p_supervisor.c"
#include <assert.h>
#include <sys/wait.h>
#ifdef __FreeBSD__
#include <sha256.h>
#else
#include <openssl/sha.h>
#endif

static bool FAIL_SYNC;
int __real_fsync(int);
int __wrap_fsync(int fd) {
   if (FAIL_SYNC) { FAIL_SYNC = false; errno = EIO; return (-1); }
   return (__real_fsync(fd));
}


static void
_pair(int pair[2]) {
   int raw[2];
   assert(socketpair(AF_UNIX, SOCK_STREAM, 0, raw) == 0);
   for (unsigned i = 0; i < 2; i++) {
      pair[i] = fcntl(raw[i], F_DUPFD_CLOEXEC, 10);
      assert(pair[i] >= 10);
      close(raw[i]);
   }
   assert(dup2(pair[0], CONTROL) == CONTROL);
   close(pair[0]);
}

static void
_rights(unsigned count, bool truncate) {
   int pair[2]; _pair(pair);
   uint8_t frame[72]; _header(frame, 4, 0, 56);
   memset(frame + 16, 1, 56);
   int file = open("/dev/null", O_RDWR | O_CLOEXEC);
   assert(file >= 0);
   union { struct cmsghdr align; char data[CMSG_SPACE(20 * sizeof(int))]; } c;
   memset(&c, 0, sizeof(c));
   struct iovec iov = {frame, truncate ? 20 : sizeof(frame)};
   struct msghdr m = {0}; m.msg_iov = &iov; m.msg_iovlen = 1;
   m.msg_control = c.data; m.msg_controllen = CMSG_SPACE(count * sizeof(int));
   struct cmsghdr *h = CMSG_FIRSTHDR(&m);
   h->cmsg_level = SOL_SOCKET; h->cmsg_type = SCM_RIGHTS;
   h->cmsg_len = CMSG_LEN(count * sizeof(int));
   for (unsigned i = 0; i < count; i++)
      memcpy((char *)CMSG_DATA(h) + sizeof(int) * i, &file, sizeof(file));
   assert(sendmsg(pair[1], &m, 0) == (ssize_t)iov.iov_len);
   close(pair[1]); close(file);
   int fd[3] = {-1, -1, -1}; uint8_t received[72];
   int error = _receive(received, sizeof(received), fd, _now() + 100);
   assert((error == 0) == (count == 3 && !truncate));
   if (!error) {
      assert(!memcmp(frame, received, sizeof(frame)));
      for (unsigned i = 0; i < 3; i++) {
         assert(fcntl(fd[i], F_GETFD) == FD_CLOEXEC);
         close(fd[i]);
      }
   }
   close(CONTROL);
}

static void
_reply(unsigned variant) {
   int pair[2]; _pair(pair);
   pid_t child = fork(); assert(child >= 0);
   if (!child) {
      close(CONTROL);
      uint8_t req[88]; size_t n = 0;
      while (n < sizeof(req)) {
         ssize_t got = read(pair[1], req + n, sizeof(req) - n);
         if (got <= 0) _exit(1);
         n += (size_t)got;
      }
      if (memcmp(req, "HSM1\1\1\0\0\0\0\0\1\0\0\0\110", 16)) _exit(2);
      uint8_t reply[20] = {0}; _header(reply, 0x81, 1, 4);
      if (variant == 1) reply[8] = 1;
      if (variant == 2) reply[4] = 2;
      if (variant == 3) reply[12] = 1;
      if (variant == 4) reply[19] = 3;
      if (variant == 5) reply[6] = 1;
      assert(write(pair[1], reply, variant == 6 ? 19 : 20) ==
          (variant == 6 ? 19 : 20));
      close(pair[1]); _exit(0);
   }
   close(pair[1]);
   Hfs9pSupervisor s = {.sequence = 1}; uint8_t claim[72] = {0};
   int error = _request(&s, 1, claim, sizeof(claim));
   assert((error == 0) == (variant == 0));
   assert(s.fenced == (variant != 0));
   int status; assert(waitpid(child, &status, 0) == child);
   assert(WIFEXITED(status) && WEXITSTATUS(status) == 0);
   close(CONTROL);
}

static void
_durable_callbacks(void) {
   char path[] = "/tmp/hfs-q04-supervisor-XXXXXX";
   assert(mkdtemp(path));
   int directory = open(path, O_RDONLY | O_DIRECTORY | O_CLOEXEC);
   assert(directory >= 0);
   Hfs9pSupervisor s = {0};
   assert(!hfs9p_ledger_new(&s.ledger, directory, "forwarding"));
   Hfs9pLedger *second = NULL;
   assert(hfs9p_ledger_new(&second, directory, "forwarding") == EWOULDBLOCK);
   s.endpoint.session[15] = 5;
   s.endpoint.node[0] = 110; s.endpoint.node[1] = 111; s.endpoint.node[2] = 112;
   s.witness = true;
   uint8_t zero[32] = {0};
   assert(!_claim_cb(&s, &s.endpoint, zero));
   assert(!s.witness && _claim_cb(&s, &s.endpoint, zero) == EACCES);
   uint8_t payload[128] = {0};
   memcpy(payload, "H9LEDG1\0", 8);
   payload[23] = 5; payload[24] = 110; payload[32] = 111; payload[40] = 112;
#ifdef __FreeBSD__
   SHA256_CTX ctx;
   SHA256_Init(&ctx); SHA256_Update(&ctx, payload, 96);
   SHA256_Final(payload + 96, &ctx);
#else
   (void)SHA256(payload, 96, payload + 96);
#endif
   assert(!_protect_cb(&s, payload, sizeof(payload)));
   FAIL_SYNC = true;
   assert(_protect_cb(&s, payload, sizeof(payload)) == EIO);
   uint8_t *loaded = NULL; size_t length = 0;
   assert(!hfs9p_ledger_load(s.ledger, &loaded, &length));
   assert(length == sizeof(payload) && !memcmp(loaded, payload, length));
   free(loaded);
   struct stat st;
   assert(!fstatat(directory, "forwarding.next", &st, 0));
   assert(_protect_cb(&s, payload, sizeof(payload)) == EEXIST);
   hfs9p_ledger_delete(s.ledger); close(directory);
   printf("Preserved adapter ledger/fsync failure evidence: %s\n", path);
}

int
main(void) {
   _durable_callbacks();
   for (unsigned i = 1; i <= 4; i++) _rights(i, false);
   _rights(3, true);
   _rights(20, false);
   for (unsigned i = 0; i <= 6; i++) _reply(i);
   int pair[2]; _pair(pair);
   /* Socketpair created by ourselves is not our authenticated parent. */
   assert(_socket() == EACCES);
   pid_t child = fork(); assert(child >= 0);
   if (!child) {
      close(pair[1]);
      _exit(_socket() == 0 && fcntl(CONTROL, F_GETFD) == FD_CLOEXEC ? 0 : 1);
   }
   int status; assert(waitpid(child, &status, 0) == child);
   assert(WIFEXITED(status) && WEXITSTATUS(status) == 0);
   Hfs9pSupervisor replacement = {.sequence = 1, .socket_inode = (ino_t)-1};
   uint8_t claim[72] = {0};
   assert(_request(&replacement, 1, claim, sizeof(claim)) == ESTALE);
   Hfs9pSupervisor exhausted = {.sequence = UINT32_MAX};
   assert(_request(&exhausted, 1, claim, sizeof(claim)) == ESTALE);
   Hfs9pSupervisor s = {.sequence = 1};
   s.fd[0] = s.fd[1] = s.fd[2] = -1;
   uint8_t observation[184];
   assert(_observe(&s, observation) != 0);
   s.witness = true;
   uint8_t digest[32] = {1};
   assert(_claim_cb(&s, &s.endpoint, digest) == EACCES);
   assert(s.witness);
   s.witness = false; memset(digest, 0, sizeof(digest));
   assert(_claim_cb(&s, &s.endpoint, digest) == EACCES);
   close(pair[1]); close(CONTROL);
   puts("PASS: portable management/socket/rights refusals; no native proof");
   return (0);
}
