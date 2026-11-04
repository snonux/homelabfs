/* Private inherited fd5 recovery carrier. Included by supervisor.c, never a
 * guest wire API. HSR1 frames retain the existing184-byte management bound. */
#define RECOVERY 5

static int
_recovery_transfer(void *buffer, size_t length, bool writing) {
   uint8_t *bytes = buffer;
   size_t done = 0;
   int64_t deadline = _now() + 10000;
   while (done < length) {
      int64_t now = _now();
      if (now < 0 || now >= deadline) return (ETIMEDOUT);
      struct pollfd p = {RECOVERY, writing ? POLLOUT : POLLIN, 0};
      int ready = poll(&p, 1, (int)(deadline - now));
      if (ready < 0 && errno == EINTR) continue;
      if (ready <= 0) return (ready ? errno : ETIMEDOUT);
      if (p.revents & (POLLERR | POLLNVAL)) return (EPIPE);
      if (!(p.revents & p.events)) return (EPIPE);
      ssize_t n;
      if (writing) n = send(RECOVERY, bytes + done, length - done,
          MSG_DONTWAIT | MSG_NOSIGNAL);
      else {
         /* No descriptor or ancillary authority is accepted on this channel. */
         union { struct cmsghdr align; char b[CMSG_SPACE(sizeof(int))]; } c;
         struct iovec iov = {bytes + done, length - done};
         struct msghdr m = {0}; m.msg_iov = &iov; m.msg_iovlen = 1;
         m.msg_control = c.b; m.msg_controllen = sizeof(c);
         n = recvmsg(RECOVERY, &m, MSG_DONTWAIT);
         bool ancillary = (m.msg_flags & (MSG_TRUNC | MSG_CTRUNC)) != 0;
         for (struct cmsghdr *a = CMSG_FIRSTHDR(&m); a;
             a = CMSG_NXTHDR(&m, a)) {
            if (a->cmsg_level == SOL_SOCKET && a->cmsg_type == SCM_RIGHTS) {
               if (a->cmsg_len < CMSG_LEN(0)) return (EPROTO);
               size_t count = (a->cmsg_len - CMSG_LEN(0)) / sizeof(int);
               for (size_t i = 0; i < count; i++) {
                  int fd; memcpy(&fd, (char *)CMSG_DATA(a) + i * sizeof(int), sizeof(fd));
                  (void)close(fd);
               }
            }
            ancillary = true;
         }
         if (ancillary) return (EPROTO);
      }
      if (n < 0 && (errno == EINTR || errno == EAGAIN)) continue;
      if (n <= 0) return (n ? errno : EPIPE);
      done += (size_t)n;
   }
   return (0);
}

static int
_recovery_frame(uint8_t op, uint32_t sequence, const uint8_t *payload,
    size_t length) {
   if (length > PAYLOAD) return (EINVAL);
   uint8_t frame[16 + PAYLOAD];
   _header(frame, op, sequence, (uint32_t)length);
   memcpy(frame, "HSR1", 4); memcpy(frame + 16, payload, length);
   return (_recovery_transfer(frame, 16 + length, true));
}

static int
_recovery_request(uint8_t *op, uint32_t sequence, uint8_t payload[PAYLOAD],
    size_t *length) {
   uint8_t header[16];
   int error = _recovery_transfer(header, 16, false);
   if (error) return (error);
   *length = (size_t)_get(header + 12, 4); *op = header[5];
   if (memcmp(header, "HSR1", 4) || header[4] != 1 || header[6] || header[7] ||
       _get(header + 8, 4) != sequence || *length > PAYLOAD)
      return (EPROTO);
   return (_recovery_transfer(payload, *length, false));
}

static int
_recovery_socket(void) {
   struct sockaddr_un address;
   socklen_t n = sizeof(address);
   int type; socklen_t size = sizeof(type);
   if (getsockname(RECOVERY, (struct sockaddr *)&address, &n) ||
       address.sun_family != AF_UNIX || n > offsetof(struct sockaddr_un, sun_path) ||
       getsockopt(RECOVERY, SOL_SOCKET, SO_TYPE, &type, &size) || type != SOCK_STREAM)
      return (EACCES);
   n = sizeof(address);
   if (getpeername(RECOVERY, (struct sockaddr *)&address, &n) ||
       address.sun_family != AF_UNIX || n > offsetof(struct sockaddr_un, sun_path))
      return (EACCES);
#ifdef __FreeBSD__
   struct xucred cred; size = sizeof(cred);
   if (getsockopt(RECOVERY, SOL_LOCAL, LOCAL_PEERCRED, &cred, &size) ||
       size != sizeof(cred) || cred.cr_version != XUCRED_VERSION ||
       cred.cr_uid != geteuid() || cred.cr_pid != getppid()) return (EACCES);
#else
   struct ucred cred; size = sizeof(cred);
   if (getsockopt(RECOVERY, SOL_SOCKET, SO_PEERCRED, &cred, &size) ||
       size != sizeof(cred) || cred.uid != geteuid() || cred.pid != getppid())
      return (EACCES);
#endif
   if (fcntl(RECOVERY, F_SETFD, FD_CLOEXEC) ||
       fcntl(RECOVERY, F_GETFD) != FD_CLOEXEC) return (EACCES);
   return (0);
}

static int
_recovery_protected(Hfs9pSupervisor *s) {
   uint8_t *bytes = NULL; size_t length = 0;
   int error = hfs9p_ledger_load(s->ledger, &bytes, &length);
   if (!error && (length != s->protected_length ||
       memcmp(bytes + length - 32, s->protected_digest, 32))) error = ESTALE;
   free(bytes); return (error);
}

static void *
_recovery_cb(void *arg) {
   Hfs9pSupervisor *s = arg;
   uint8_t op, payload[PAYLOAD], barrier[16], digest[32], cut[32];
   size_t length;
   struct stat original, observed_socket;
   int error = fstat(RECOVERY, &original) ? errno : 0;
   /* No request is pending during ordinary operation. This one waiting thread
    * owns fd5 for the whole lifetime; frame reads themselves are bounded. */
   struct pollfd wait = {RECOVERY, POLLIN, 0};
   while (!error && poll(&wait, 1, -1) < 0) if (errno != EINTR) error = errno;
   if (!error) error = _recovery_request(&op, 1, payload, &length);
   if (!error && (op != 1 || length != 72 || memcmp(payload, s->tuple, 56))) error = EACCES;
   if (!error) {
      memcpy(barrier, payload + 56, 16);
      uint8_t zero[16] = {0}; if (!memcmp(barrier, zero, 16)) error = EINVAL;
   }
   uint8_t *bytes = NULL; size_t total = 0;
   if (!error) error = hfs9p_pause_owned(s->backend, &bytes, &total);
   /* FETCH coverage can advance after the last ordinary protection. First
    * require the exact last protected file owned by this live backend, then
    * checkpoint the fully paused current cut, without executing any effect. */
   if (!error) error = _recovery_protected(s);
   if (!error) error = _protect_cb(s, bytes, total);
   if (!error) error = hfs9p_ledger_matches(s->ledger, bytes, total);
   if (!error && (fstat(RECOVERY, &observed_socket) || original.st_dev != observed_socket.st_dev ||
       original.st_ino != observed_socket.st_ino)) error = ESTALE;
   if (!error) {
      memcpy(digest, bytes + total - 32, 32);
      _put(payload, total, 8); memcpy(payload + 8, digest, 32);
      error = _recovery_frame(0x81, 1, payload, 40);
   }
   for (size_t offset = 0; !error && offset < total;) {
      size_t n = total - offset; if (n > PAYLOAD - 8) n = PAYLOAD - 8;
      _put(payload, offset, 8); memcpy(payload + 8, bytes + offset, n);
      error = _recovery_frame(0x83, 1, payload, n + 8); offset += n;
   }
   free(bytes); /* Single existing snapshot staging copy, no second65MiB copy. */
   if (!error) error = _recovery_request(&op, 2, payload, &length);
   if (!error && (op != 2 || length != 80 || memcmp(payload, barrier, 16) ||
       memcmp(payload + 48, digest, 32))) error = ESTALE;
   if (!error) error = _recovery_protected(s);
   if (!error) {
      memcpy(cut, payload + 16, 32);
      error = _recovery_frame(0x82, 2, payload, 80);
   }
   if (!error) error = _recovery_request(&op, 3, payload, &length);
   if (!error && (op != 3 || length != 80 || memcmp(payload, barrier, 16) ||
       memcmp(payload + 16, cut, 32) || memcmp(payload + 48, digest, 32))) error = ESTALE;
   if (!error) error = _recovery_protected(s);
   if (!error) error = hfs9p_resume_owned(s->backend, digest);
   if (!error) error = _recovery_frame(0x84, 3, payload, 80);
   /* No retry/replacement channel is accepted. A failed barrier remains paused
    * and owns all original ledger/resources. Successful fd5 is also one-shot. */
   (void)shutdown(RECOVERY, SHUT_RDWR);
   return (NULL);
}
