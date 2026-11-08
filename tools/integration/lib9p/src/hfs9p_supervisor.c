/* SPDX-License-Identifier: BSD-2-Clause */
#ifndef _GNU_SOURCE
#define _GNU_SOURCE
#endif
#include "hfs9p_supervisor.h"
#include "hfs9p_ledger.h"
#include "hfs9p_json.h"

#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#ifdef __FreeBSD__
#include <sys/capsicum.h>
#include <sys/ucred.h>
#include <sys/mount.h>
#else
#include <sys/vfs.h>
#endif
#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <poll.h>
#include <pthread.h>
#include <stdbool.h>
#include <stdatomic.h>
#include <stddef.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

#define CONTROL 3
#define PAYLOAD 184

typedef struct {
   pthread_mutex_t mutex;
   Hfs9pEndpoint endpoint;
   uint8_t tuple[56];
   int fd[3];
   uint32_t sequence;
   dev_t socket_device;
   ino_t socket_inode;
   bool fenced;
   bool witness;
   bool connected;
   bool ready;
   Hfs9pLedger *ledger;
   Hfs9p *backend;
   uint8_t protected_digest[32];
   size_t protected_length;
   uint8_t session[16];
   char name[65];
   unsigned session_count;
   uint8_t sessions[16][16];
   char names[16][65];
   bool connections[16];
} Hfs9pSupervisor;

static Hfs9pSupervisor OWNER = {.mutex = PTHREAD_MUTEX_INITIALIZER,
    .fd = {-1, -1, -1}, .sequence = 1};
static bool ATTEMPTED;

static void
_put(uint8_t *p, uint64_t v, unsigned n) {
   while (n) { p[--n] = (uint8_t)v; v >>= 8; }
}

static uint64_t
_get(const uint8_t *p, unsigned n) {
   uint64_t v = 0;
   for (unsigned i = 0; i < n; i++) v = (v << 8) | p[i];
   return (v);
}

static int64_t
_now(void) {
   struct timespec t;
   if (clock_gettime(CLOCK_MONOTONIC, &t)) return (-1);
   return ((int64_t)t.tv_sec * 1000 + t.tv_nsec / 1000000);
}

static int
_ready(short events, int64_t deadline) {
   for (;;) {
      int64_t now = _now();
      if (now < 0 || now >= deadline) return (ETIMEDOUT);
      struct pollfd p = {CONTROL, events, 0};
      int n = poll(&p, 1, (int)(deadline - now));
      if (n < 0 && errno == EINTR) continue;
      if (n < 0) return (errno);
      if (!n) return (ETIMEDOUT);
      if (p.revents & (POLLERR | POLLNVAL)) return (EPIPE);
      if (p.revents & events) return (0);
      return (EPIPE);
   }
}

/* Every receive uses recvmsg, including replies, to refuse hidden rights.
 * MSG_CTRUNC is fatal; the kernel closes unreturned excess descriptors. */
static int
_receive(uint8_t *bytes, size_t length, int *fds, int64_t deadline) {
   size_t done = 0;
   unsigned count = 0;
   int error = 0;
   while (done < length && !error) {
      error = _ready(POLLIN, deadline);
      if (error) break;
      union { struct cmsghdr align; char bytes[CMSG_SPACE(16 * sizeof(int))]; } ancillary;
      struct iovec iov = {bytes + done, length - done};
      struct msghdr m = {0};
      m.msg_iov = &iov; m.msg_iovlen = 1;
      m.msg_control = ancillary.bytes; m.msg_controllen = sizeof(ancillary);
      ssize_t n = recvmsg(CONTROL, &m, MSG_DONTWAIT);
      if (n < 0 && (errno == EINTR || errno == EAGAIN)) continue;
      if (n <= 0) { error = n ? errno : EPIPE; break; }
      if (m.msg_flags & (MSG_CTRUNC | MSG_TRUNC)) error = EPROTO;
      for (struct cmsghdr *c = CMSG_FIRSTHDR(&m); c;
          c = CMSG_NXTHDR(&m, c)) {
         if (c->cmsg_level != SOL_SOCKET || c->cmsg_type != SCM_RIGHTS ||
             c->cmsg_len < CMSG_LEN(0) ||
             (c->cmsg_len - CMSG_LEN(0)) % sizeof(int)) {
            error = EPROTO; continue;
         }
         size_t nf = (c->cmsg_len - CMSG_LEN(0)) / sizeof(int);
         for (size_t j = 0; j < nf; j++) {
            int fd;
            memcpy(&fd, (char *)CMSG_DATA(c) + j * sizeof(fd), sizeof(fd));
            if (fds && count < 3 && fd >= 4 &&
                fcntl(fd, F_SETFD, FD_CLOEXEC) == 0 &&
                fcntl(fd, F_GETFD) == FD_CLOEXEC) fds[count++] = fd;
            else { (void)close(fd); error = EPROTO; }
         }
      }
      done += (size_t)n;
   }
   if (!error && fds && count != 3) error = EPROTO;
   if (error && fds) {
      for (unsigned i = 0; i < count; i++) { close(fds[i]); fds[i] = -1; }
   }
   return (error);
}

static void
_header(uint8_t *p, uint8_t op, uint32_t seq, uint32_t length) {
   memset(p, 0, 16); memcpy(p, "HSM1", 4); p[4] = 1; p[5] = op;
   _put(p + 8, seq, 4); _put(p + 12, length, 4);
}

static int
_request(Hfs9pSupervisor *s, uint8_t op, const uint8_t *payload, size_t length) {
   if (s->fenced || s->sequence == UINT32_MAX || length > PAYLOAD)
      return (ESTALE);
   struct stat socket_st;
   if (s->socket_inode && (fstat(CONTROL, &socket_st) ||
       socket_st.st_dev != s->socket_device ||
       socket_st.st_ino != s->socket_inode)) {
      s->fenced = true;
      return (ESTALE);
   }
   uint8_t frame[16 + PAYLOAD], reply[20], expected[16];
   uint32_t seq = s->sequence++;
   _header(frame, op, seq, (uint32_t)length);
   memcpy(frame + 16, payload, length);
   int64_t deadline = _now() + 4000;
   size_t done = 0;
   int error = 0;
   while (done < length + 16 && !error) {
      error = _ready(POLLOUT, deadline);
      if (error) break;
      ssize_t n = send(CONTROL, frame + done, length + 16 - done,
          MSG_DONTWAIT | MSG_NOSIGNAL);
      if (n < 0 && (errno == EINTR || errno == EAGAIN)) continue;
      if (n <= 0) { error = n ? errno : EPIPE; break; }
      done += (size_t)n;
   }
   if (!error) error = _receive(reply, sizeof(reply), NULL, deadline);
   _header(expected, op | 0x80, seq, 4);
   if (!error && (memcmp(reply, expected, 16) || _get(reply + 16, 4)))
      error = EACCES;
   if (error) { s->fenced = true; (void)shutdown(CONTROL, SHUT_RDWR); }
   return (error);
}

/* Unnamed AF_UNIX carrier policy. Linux reports an unnamed socket as
 * sun_family only (n == offsetof(sun_path)) and an abstract name as a longer
 * address whose first path byte is NUL, so Linux admits only the short form.
 * FreeBSD instead copies its sun_noname placeholder: a 16-byte address with
 * sun_len 16, AF_UNIX and all-zero path bytes. FreeBSD has no abstract
 * namespace and bind(2) refuses an empty path, so NUL-only returned path bytes
 * mean unnamed there. Any nonzero byte (pathname or abstract) is refused. The
 * policy is a parameter so the FreeBSD form is testable on every platform. */
#ifdef __FreeBSD__
#define NUL_PATH_UNNAMED true
#else
#define NUL_PATH_UNNAMED false
#endif

static bool
_unix_unnamed_as(const struct sockaddr_un *address, socklen_t n,
    bool nul_path_unnamed) {
   size_t path = offsetof(struct sockaddr_un, sun_path);
   if (n < path || (size_t)n > sizeof(*address) ||
       address->sun_family != AF_UNIX) return (false);
   if (n == path) return (true);
   if (!nul_path_unnamed) return (false);
   for (size_t i = path; i < (size_t)n; i++)
      if (((const unsigned char *)address)[i]) return (false);
   return (true);
}

static bool
_unix_unnamed(const struct sockaddr_un *address, socklen_t n) {
   return (_unix_unnamed_as(address, n, NUL_PATH_UNNAMED));
}

/* Both local and peer addresses must be unnamed: a named end could have been
 * reached by an unrelated connect(2) rather than inherited from socketpair. */
static int
_unnamed_stream(int fd) {
   struct sockaddr_un address;
   socklen_t n = sizeof(address);
   int type;
   socklen_t size = sizeof(type);
   memset(&address, 0, sizeof(address));
   if (getsockname(fd, (struct sockaddr *)&address, &n) ||
       !_unix_unnamed(&address, n) ||
       getsockopt(fd, SOL_SOCKET, SO_TYPE, &type, &size) ||
       type != SOCK_STREAM) return (EACCES);
   memset(&address, 0, sizeof(address));
   n = sizeof(address);
   if (getpeername(fd, (struct sockaddr *)&address, &n) ||
       !_unix_unnamed(&address, n)) return (EACCES);
   return (0);
}

/* The peer must be our parent supervisor running as our effective uid. */
static int
_parent_peer(int fd) {
#ifdef __FreeBSD__
   struct xucred cred;
   socklen_t size = sizeof(cred);
   if (getsockopt(fd, SOL_LOCAL, LOCAL_PEERCRED, &cred, &size) ||
       size != sizeof(cred) || cred.cr_version != XUCRED_VERSION ||
       cred.cr_uid != geteuid() || cred.cr_pid != getppid())
      return (EACCES);
#else
   struct ucred cred;
   socklen_t size = sizeof(cred);
   if (getsockopt(fd, SOL_SOCKET, SO_PEERCRED, &cred, &size) ||
       size != sizeof(cred) || cred.uid != geteuid() ||
       cred.pid != getppid()) return (EACCES);
#endif
   return (0);
}

/* Shared CONTROL (fd3) and RECOVERY (fd5) admission: an inherited unnamed
 * stream socketpair end whose peer is the parent, kept close-on-exec. */
static int
_parent_socket(int fd) {
   if (_unnamed_stream(fd) || _parent_peer(fd)) return (EACCES);
   if (fcntl(fd, F_SETFD, FD_CLOEXEC) ||
       fcntl(fd, F_GETFD) != FD_CLOEXEC) return (EACCES);
   return (0);
}

static int
_socket(void) {
   return (_parent_socket(CONTROL));
}

static int
_observe(Hfs9pSupervisor *s, uint8_t out[184]) {
   memcpy(out, s->tuple, 56);
   _put(out + 56, s->sequence, 8);
   for (unsigned i = 0; i < 3; i++) {
      struct stat st;
      struct statfs fs;
      int fd = s->fd[i], flags = fcntl(fd, F_GETFL);
      if (fd < 4 || fstat(fd, &st) || fstatfs(fd, &fs) || flags < 0 ||
          !S_ISREG(st.st_mode) || (uint64_t)st.st_size != HFS9P_VSIZE ||
          (uint64_t)st.st_ino != s->endpoint.node[i] ||
          (flags & O_ACCMODE) != (i ? O_RDONLY : O_WRONLY) ||
          !(flags & O_DIRECT) || (flags & (O_APPEND | O_NONBLOCK)) ||
          fcntl(fd, F_GETFD) != FD_CLOEXEC) return (EACCES);
#ifdef __FreeBSD__
      cap_rights_t actual, want;
      uint32_t fcntls;
      cap_rights_init(&want, CAP_FSTAT, CAP_FSTATFS, CAP_SEEK, CAP_FCNTL,
          i ? CAP_PREAD : CAP_PWRITE);
      if (strncmp(fs.f_fstypename, "fusefs", 6) ||
          (fs.f_fstypename[6] && fs.f_fstypename[6] != '.') ||
          cap_rights_get(fd, &actual) || !cap_rights_contains(&actual, &want) ||
          !cap_rights_contains(&want, &actual) ||
          cap_fcntls_get(fd, &fcntls) || fcntls != CAP_FCNTL_GETFL)
         return (EACCES);
#else
      /* Linux files/socket tests never become native endpoint evidence. */
      return (ENOTSUP);
#endif
      uint8_t *p = out + 64 + 40 * i;
      memset(p, 0, 40); _put(p, (unsigned)fd, 4);
      _put(p + 4, (i ? 2 : 1) | 4, 4);
      _put(p + 8, (uint64_t)st.st_dev, 8);
      _put(p + 16, (uint64_t)st.st_ino, 8);
      _put(p + 24, (uint64_t)st.st_size, 8); _put(p + 32, 1, 4);
   }
   return (0);
}

/* Private native syscall adapters. The production object has no setter or
 * configuration path for these bindings; portable source-inclusion tests replace
 * only this operating-system boundary in their separate translation unit. */
static int (*NATIVE_OBSERVE)(Hfs9pSupervisor *, uint8_t[184]) = _observe;
static int (*NATIVE_CONSTRUCT)(Hfs9p **, const int[3], const Hfs9pEndpoint *,
    Hfs9pVerifier) = hfs9p_new;
static int
_native_platform(void) {
#ifndef __FreeBSD__
   return (ENOTSUP);
#else
   return (0);
#endif
}
static int (*NATIVE_PLATFORM)(void) = _native_platform;

static int
_verify_locked(Hfs9pSupervisor *s) {
   uint8_t payload[184];
   int error = s->fenced ? ESTALE : NATIVE_OBSERVE(s, payload);
   if (!error) error = _request(s, 3, payload, sizeof(payload));
   if (error) { s->fenced = true; (void)shutdown(CONTROL, SHUT_RDWR); }
   return (error);
}

static bool
_matches(Hfs9pSupervisor *s, const Hfs9pEndpoint *e, const int fd[3]) {
   return (!memcmp(e->session, s->endpoint.session, 16) &&
       !memcmp(e->node, s->endpoint.node, sizeof(e->node)) &&
       !memcmp(fd, s->fd, sizeof(s->fd)));
}

/* The begin/end callback pair deliberately retains the mutex on success.
 * Describe that contract to the native pthread lock analysis. */
#ifdef __FreeBSD__
static int _begin_io_cb(void *arg, const Hfs9pEndpoint *e, const int fd[3])
    __trylocks_exclusive(0, ((Hfs9pSupervisor *)arg)->mutex);
static void _end_io_cb(void *arg)
    __unlocks(((Hfs9pSupervisor *)arg)->mutex);
#endif

static int
_begin_io_cb(void *arg, const Hfs9pEndpoint *e, const int fd[3]) {
   Hfs9pSupervisor *s = arg;
   pthread_mutex_lock(&((Hfs9pSupervisor *)arg)->mutex);
   int error = _matches(s, e, fd) ? _verify_locked(s) : EACCES;
   if (error) pthread_mutex_unlock(&((Hfs9pSupervisor *)arg)->mutex);
   return (error);
}

static void
_end_io_cb(void *arg) {
   pthread_mutex_unlock(&((Hfs9pSupervisor *)arg)->mutex);
}

static int
_verify_cb(void *arg, const Hfs9pEndpoint *e, const int fd[3]) {
   int error = _begin_io_cb(arg, e, fd);
   if (!error) _end_io_cb(arg);
   return (error);
}

static int
_controller_cb(void *arg, const Hfs9pEndpoint *e, const int fd[3],
    uint8_t controller[16]) {
   Hfs9pSupervisor *s = arg;
   int error = _begin_io_cb(arg, e, fd);
   if (!error) {
      memcpy(controller, s->tuple + 40, 16);
      _end_io_cb(arg);
   }
   return (error);
}

static int
_claim_cb(void *arg, const Hfs9pEndpoint *e, const uint8_t digest[32]) {
   Hfs9pSupervisor *s = arg;
   uint8_t zero[32] = {0};
   if (!s->witness || memcmp(digest, zero, 32) ||
       !_matches(s, e, s->fd)) return (EACCES);
   s->witness = false;
   return (hfs9p_ledger_claim(s->ledger, e, digest));
}

static int
_protect_cb(void *arg, const uint8_t *bytes, size_t length) {
   Hfs9pSupervisor *s = arg;
   int error = hfs9p_ledger_protect(s->ledger, bytes, length);
   if (!error) {
      memcpy(s->protected_digest, bytes + length - 32, 32);
      s->protected_length = length;
   }
   return (error);
}

static void *
_heartbeat_cb(void *arg) {
   Hfs9pSupervisor *s = arg;
   for (;;) {
      struct timespec pause = {1, 0};
      (void)nanosleep(&pause, NULL);
      pthread_mutex_lock(&s->mutex);
      int error = _verify_locked(s);
      pthread_mutex_unlock(&s->mutex);
      if (error) return (NULL);
   }
}

#include "hfs9p_recovery.inc"
#include "hfs9p_bootstrap.inc"

int
hfs9p_supervisor_start_tuple(int parent, const char *name, const uint8_t *scope,
    size_t length, uint16_t minor, uint32_t features) {
   Hfs9pSupervisor *s = &OWNER;
   struct stat st;
   pthread_mutex_lock(&s->mutex);
   bool attempted = ATTEMPTED;
   ATTEMPTED = true;
   pthread_mutex_unlock(&s->mutex);
   if (attempted) return (EALREADY);
   if (!name || !*name || strlen(name) > 64 || strchr(name, '/') ||
       !strcmp(name, ".") || !strcmp(name, "..") || !scope || !length ||
       fstat(parent, &st) || !S_ISDIR(st.st_mode) || st.st_uid != geteuid() ||
       (st.st_mode & 077)) return (EINVAL);
   int error = NATIVE_PLATFORM();
   if (error) return (error);
   error = _socket();
   if (!error) error = _recovery_socket();
   if (error) return (error);
   if (fstat(CONTROL, &st)) return (errno);
   s->socket_device = st.st_dev; s->socket_inode = st.st_ino;
   /* fd4 is the inherited pinned executable; close before receiving rights. */
   if (close(4)) return (errno);
   if (mkdirat(parent, name, 0700)) return (errno);
   if (fsync(parent)) return (errno);
   int directory = openat(parent, name, O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
   if (directory < 0) return (errno);
   error = hfs9p_ledger_new(&s->ledger, directory, "forwarding");
   uint8_t tuple[72], expected[16];
   if (!error) error = _receive(tuple, sizeof(tuple), s->fd, _now() + 4000);
   _header(expected, 4, 0, 56);
   if (!error && memcmp(tuple, expected, 16)) error = EPROTO;
   if (!error) {
      memcpy(s->tuple, tuple + 16, 56);
      memcpy(s->endpoint.session, s->tuple, 16);
      uint8_t zeros[16] = {0};
      if (!memcmp(s->tuple, zeros, 16) || !memcmp(s->tuple + 40, zeros, 16))
         error = EPROTO;
      for (unsigned i = 0; i < 3; i++) {
         s->endpoint.node[i] = _get(s->tuple + 16 + 8 * i, 8);
         if (!s->endpoint.node[i]) error = EPROTO;
         for (unsigned j = 0; j < i; j++)
            if (s->endpoint.node[i] == s->endpoint.node[j]) error = EPROTO;
      }
   }
   /* Permanent issuance record is actual process/socket/tuple observation.
    * Exclusive directory and lock are never deleted after a failed attempt. */
   if (!error) {
      struct stat socket_st = {0};
      if (fstat(CONTROL, &socket_st)) error = errno;
      uint8_t record[104] = {0};
      memcpy(record, s->tuple, 56);
      _put(record + 56, (uint64_t)getpid(), 8);
      _put(record + 64, (uint64_t)getppid(), 8);
      _put(record + 72, (uint64_t)socket_st.st_dev, 8);
      _put(record + 80, (uint64_t)socket_st.st_ino, 8);
      for (unsigned i = 0; i < 3; i++) _put(record + 88 + 4 * i, s->fd[i], 4);
      int fd = error ? -1 : openat(directory, "issued", O_WRONLY | O_CREAT |
          O_EXCL | O_CLOEXEC | O_NOFOLLOW, 0600);
      if (!error && fd < 0) error = errno;
      if (!error && write(fd, record, sizeof(record)) != sizeof(record)) error = EIO;
      if (!error && fsync(fd)) error = errno;
      if (fd >= 0 && close(fd) && !error) error = errno;
      if (!error && fsync(directory)) error = errno;
   }
   if (!error) {
      uint8_t claim[72] = {0}; memcpy(claim, s->tuple, 40);
      error = _request(s, 1, claim, sizeof(claim));
      s->witness = !error;
   }
#ifdef __FreeBSD__
   /* Attenuate before first VERIFY: constructor repeats the same limits. */
   for (unsigned i = 0; !error && i < 3; i++) {
      cap_rights_t rights;
      cap_rights_init(&rights, CAP_FSTAT, CAP_FSTATFS, CAP_FCNTL, CAP_SEEK,
          i ? CAP_PREAD : CAP_PWRITE);
      if (cap_fcntls_limit(s->fd[i], CAP_FCNTL_GETFL) ||
          cap_rights_limit(s->fd[i], &rights)) error = errno;
   }
#endif
   if (!error) error = _verify_locked(s);
   Hfs9pVerifier verifier = {s, _claim_cb, _protect_cb, _verify_cb,
       _begin_io_cb, _end_io_cb, _controller_cb};
   if (!error) error = NATIVE_CONSTRUCT(&s->backend, s->fd, &s->endpoint, verifier);
   if (!error) error = hfs9p_add_session_tuple(s->backend, scope, length, minor, features);
   if (!error) {
      error = _bootstrap_session(scope, length, s->session);
      strcpy(s->name, name);
      s->session_count = 1;
      memcpy(s->sessions[0], s->session, 16);
      strcpy(s->names[0], name);
      pthread_t heartbeat;
      pthread_attr_t attributes;
      bool initialized = false;
      if (!error) {
         error = pthread_attr_init(&attributes);
         initialized = !error;
      }
      /* One 128 KiB stack, fixed <=200-byte control buffers, no parser arena. */
      if (!error) error = pthread_attr_setstacksize(&attributes, 128 * 1024);
      if (!error) error = pthread_attr_setdetachstate(&attributes,
          PTHREAD_CREATE_DETACHED);
      if (!error) error = pthread_create(&heartbeat, &attributes,
          _heartbeat_cb, s);
      if (!error) error = pthread_create(&heartbeat, &attributes,
          _recovery_cb, s);
      if (initialized) (void)pthread_attr_destroy(&attributes);
   }
   (void)close(directory);
   pthread_mutex_lock(&s->mutex);
   if (error) { s->fenced = true; (void)shutdown(CONTROL, SHUT_RDWR); }
   else s->ready = true;
   pthread_mutex_unlock(&s->mutex);
   return (error);
}

int
hfs9p_supervisor_start(int parent, const char *name, const uint8_t *scope,
    size_t length, uint16_t minor) {
   uint32_t features = minor == 4 ? 16383u : minor == 3 ? 8191u :
       minor == 2 ? 4095u : minor == 1 ? 2047u : 255u;
   return (hfs9p_supervisor_start_tuple(parent, name, scope, length,
       minor, features));
}

int
hfs9p_controller_claim(const char *name, Hfs9pConnection **out) {
   Hfs9pSupervisor *s = &OWNER;
   if (!out) return (EINVAL);
   *out = NULL;
   pthread_mutex_lock(&s->mutex);
   unsigned index = s->session_count;
   for (unsigned i = 0; name && i < s->session_count; i++)
      if (!strcmp(name, s->names[i])) index = i;
   int error = (!s->ready || !s->backend || s->fenced ||
       index >= s->session_count || s->connections[index]) ?
       EACCES : _verify_locked(s);
   bool granted = !error;
   if (granted) {
      s->connections[index] = true;
      s->connected = true;
   }
   pthread_mutex_unlock(&s->mutex);
   if (granted) error = hfs9p_connection_new(s->backend,
       s->sessions[index], out);
   if (error && granted) {
      pthread_mutex_lock(&s->mutex);
      s->fenced = true;
      (void)shutdown(CONTROL, SHUT_RDWR);
      pthread_mutex_unlock(&s->mutex);
   }
   return (error);
}
