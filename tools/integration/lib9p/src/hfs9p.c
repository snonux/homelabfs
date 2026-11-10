/*:*
 * Dedicated retained-9p exporter. SPDX-License-Identifier: BSD-2-Clause
 * Copyright (c) 2026 HomelabFS contributors.
 *:*/
#include "hfs9p.h"
#include "hfs9p_json.h"

#include <sys/stat.h>
#include <sys/types.h>
#ifdef __FreeBSD__
#include <sys/capsicum.h>
#include <sys/mount.h>
#include <sha256.h>
#else
#include <openssl/sha.h>
#include <openssl/evp.h>
#endif

#include <errno.h>
#include <fcntl.h>
#include <pthread.h>
#include <stdio.h>
#include <stdatomic.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

#define HFS9P_SESSIONS 16u
#define HFS9P_MEMORY (UINT64_C(64) << 20)
#define HFS9P_ORDINARY_MEMORY (UINT64_C(48) << 20)
#define HFS9P_ENTRIES (HFS9P_CELLS + 17u)

typedef struct {
   uint64_t first;
   uint64_t last;
} Hfs9pRange;

typedef struct Hfs9pTerminalPackage_ Hfs9pTerminalPackage;

typedef struct {
   uint8_t id[16];
   uint8_t *context;
   size_t length;
   uint64_t high;
   Hfs9pRange seen[256];
   uint16_t seen_count;
   bool hello;
   uint32_t features;
   uint8_t controller[16];
   Hfs9pTerminalPackage *terminal_package;
   atomic_uchar terminal_phase;
   const Hfs9pConnection *terminal_channel;
   uint64_t terminal_guest, terminal_native;
   uint8_t minor;
   /* Guest-order floor (e64, not persisted): every guest cell at or below it
    * was forwarded, failed back or given up after the order hold. A higher
    * original is forwarded only after all lower cells above the floor. */
   uint64_t order_high;
} Hfs9pSession;

typedef struct Hfs9pPoll_ {
   struct Hfs9pPoll_ *previous;
   uint8_t *request;
   uint8_t *response;
   uint32_t request_length;
   uint32_t response_length;
   uint64_t cell;
   uint16_t flags;
   uint8_t response_hash[32];
   bool acknowledged;
} Hfs9pPoll;

/* One bounded native-owner record arena, charged to terminal control staging.
 * Hashes bind each old cell's original full command (including its principal)
 * and both immutable full replies; old bodies retain their existing owners. */
typedef struct {
   uint64_t guest_cell;
   uint64_t native_cell;
   uint8_t guest_hash[32];
   uint8_t native_hash[32];
   uint8_t mapping_hash[32];
   uint8_t response_guest_hash[32];
   uint8_t response_native_hash[32];
   uint8_t entry;
   bool control;
} Hfs9pTerminalOld;

typedef struct {
   Hfs9pTerminalOld old[HFS9P_CELLS];
   uint8_t settled_hash[32];
   uint8_t count;
   uint8_t funding; /* 255 denotes a genuinely empty old cut. */
   uint8_t ordinary;
   uint8_t control;
   bool removed;
} Hfs9pTerminal;

_Static_assert(sizeof(Hfs9pTerminal) <= 32768,
    "terminal native records exceed their charged arena");

/* Source-derived canonical maxima: full1024-group credentials, complete scope,
 * maximal scalar widths,79 settled Acks and one funding Ack. These are bounds
 * on the closed terminal grammar, not changes to the general1MiB body limit. */
#define HFS9P_TERMINAL_GUEST 21224u
#define HFS9P_TERMINAL_NATIVE 21449u
#define HFS9P_TERMINAL_REPLY 15535u

struct Hfs9pTerminalPackage_ {
   Hfs9pTerminal state;
   uint8_t guest[HFS9P_TERMINAL_GUEST];
   uint8_t native[HFS9P_TERMINAL_NATIVE];
   uint8_t response_guest[HFS9P_TERMINAL_REPLY];
   uint8_t response_native[HFS9P_TERMINAL_REPLY];
};

typedef struct {
   uint8_t *guest;
   uint8_t *native;
   uint8_t *response_guest;
   uint8_t *response_native;
   uint32_t total;
   uint32_t native_total;
   uint32_t response_total;
   uint32_t native_response_total;
   uint32_t chunks;
   uint32_t received;
   uint32_t fetched;
   uint8_t guest_hash[32];
   uint8_t native_hash[32];
   uint8_t response_guest_hash[32];
   uint8_t response_native_hash[32];
   uint64_t guest_cell;
   uint64_t native_cell;
   uint16_t flags;
   uint8_t session;
   uint8_t minor;
   uint8_t kind;
   bool complete;
   bool issued;
   bool uncertain;
   bool responded;
   bool terminal;
   bool terminal_funded;
   /* An attempt of this candidate wrote, or was about to write, its final
    * native chunk (w64). Only then can an earlier attempt have been
    * admitted, so only then does a final-chunk refusal follow the replay
    * rules (_permanent_refusal). Persisted as bit 4096; see _final_mark for
    * when it is protected and _final_restore for a crash replay. */
   bool final_sent;
   Hfs9pTerminal *terminal_state;
   uint64_t guest_watermark;
   bool release;
   bool control;
   bool hello;
   bool assembly_control;
   bool wait;
   bool final;
   uint8_t poll_count;
   uint64_t deadline;
   uint64_t next_poll;
   Hfs9pPoll *polls;
   int donor;
   /* When this original stops waiting for lower guest cells (e64, ms, armed
    * on its first hold, not persisted: a restored entry re-arms it). */
   uint64_t order_deadline;
} Hfs9pEntry;

struct Hfs9p_ {
   pthread_mutex_t mutex;
   pthread_t worker;
   bool stopping;
   bool recovery_paused;
   uint8_t recovery_hash[32];
   int fd[3];
   Hfs9pEndpoint endpoint;
   Hfs9pVerifier verifier;
   Hfs9pSession sessions[HFS9P_SESSIONS];
   Hfs9pEntry entries[HFS9P_ENTRIES];
   uint64_t high;
   uint8_t session_count;
   unsigned ordinary;
   unsigned control;
   int pending;
   /* Set while _guest_order walks one session's lower cells (e64), so a
    * nested call for a lower cell only checks that cell's own gaps. */
   bool ordering;
   bool revoked;
   Hfs9pToken tokens[HFS9P_TOKENS];
   uint8_t scratch[HFS9P_HEADER + HFS9P_CHUNK];
};

struct Hfs9pConnection_ {
   Hfs9p *endpoint;
   unsigned session;
   uint8_t minor;
   atomic_bool negotiated;
};

static void *_wait_worker_cb(void *);
static int _snapshot(Hfs9p *, uint8_t **, size_t *);
static int _wait_step(Hfs9p *, Hfs9pEntry *);
static int _terminal_final(Hfs9p *, Hfs9pEntry *);
static int _terminal_manifest(Hfs9p *, Hfs9pJson *, uint32_t,
    Hfs9pEntry *, bool);
static int _terminal_capture(Hfs9p *, Hfs9pEntry *);
static int _mapping_donor(Hfs9p *, Hfs9pEntry *, Hfs9pTerminalOld *);
static int _snapshot_cut(Hfs9p *, uint8_t **, size_t *, Hfs9pEntry *);
static uint32_t _terminal_usage(const Hfs9p *, unsigned);

static uint64_t
_now(void) {
   struct timespec ts;
   if (clock_gettime(CLOCK_MONOTONIC, &ts))
      return (0);
   return ((uint64_t)ts.tv_sec * 1000 + (uint64_t)ts.tv_nsec / 1000000);
}

static uint64_t
_get(const uint8_t *p, unsigned n) {
   uint64_t value = 0;
   for (unsigned i = 0; i < n; i++)
      value |= (uint64_t)p[i] << (8 * i);
   return (value);
}

static void
_put(uint8_t *p, uint64_t value, unsigned n) {
   for (unsigned i = 0; i < n; i++)
      p[i] = (uint8_t)(value >> (8 * i));
}

static void
_hash(const uint8_t *p, size_t n, uint8_t digest[32]) {
#ifdef __FreeBSD__
   SHA256_CTX ctx;
   SHA256_Init(&ctx);
   SHA256_Update(&ctx, p, n);
   SHA256_Final(digest, &ctx);
#else
   (void)SHA256(p, n, digest);
#endif
}

static bool
_zero(const uint8_t *p, size_t n) {
   uint8_t bits = 0;
   for (size_t i = 0; i < n; i++)
      bits |= p[i];
   return (bits == 0);
}

static int
_hex_digit(uint8_t c) {
   if (c >= '0' && c <= '9')
      return (c - '0');
   if (c >= 'a' && c <= 'f')
      return (c - 'a' + 10);
   return (-1);
}

static int
_hex(const Hfs9pJson *j, uint32_t id, uint8_t *out, size_t n, bool uuid) {
   Hfs9pToken t = j->tokens[id];
   size_t pos = t.start + 1;
   if (t.type != '"' || t.end - t.start != n * 2 + (uuid ? 6 : 2))
      return (EINVAL);
   for (size_t i = 0; i < n; i++) {
      if (uuid && (i == 4 || i == 6 || i == 8 || i == 10)) {
         if (j->bytes[pos++] != '-')
            return (EINVAL);
      }
      int a = _hex_digit(j->bytes[pos++]);
      int b = _hex_digit(j->bytes[pos++]);
      if (a < 0 || b < 0)
         return (EINVAL);
      out[i] = (uint8_t)(a * 16 + b);
   }
   return (uuid && _zero(out, n) ? EINVAL : 0);
}

static int
_fields(const Hfs9pJson *j, uint32_t obj, const char *const *keys,
    size_t count, uint32_t *values) {
   if (j->tokens[obj].type != '{' || j->tokens[obj].count != count)
      return (EINVAL);
   for (size_t i = 0; i < count; i++) {
      if (hfs9p_json_key(j, obj, keys[i], &values[i]))
         return (EINVAL);
   }
   return (0);
}

static int
_context(Hfs9pJson *j, uint32_t obj, uint8_t session[16]) {
   const char *const names[] = { "authority_hash", "cluster", "configuration",
      "export", "exporter_incarnation", "fuse_session", "generation",
      "guest_boot", "guest_mount", "origin_incarnation", "origin_journal",
      "origin_node", "policy_hash", "registry_hash", "session", "vm",
      "volume" };
   uint32_t v[17];
   uint8_t data[32];
   if (_fields(j, obj, names, 17, v))
      return (EINVAL);
   for (unsigned i = 0; i < 17; i++) {
      bool uuid = i != 0 && i != 12 && i != 13;
      if (_hex(j, v[i], data, uuid ? 16 : 32, uuid))
         return (EINVAL);
      if (i == 14)
         memcpy(session, data, 16);
   }
   return (0);
}

static int
_frame(const uint8_t *p, size_t n, uint16_t flags, uint16_t kind) {
   uint32_t total, offset, length;
   if (n < HFS9P_HEADER || memcmp(p, "HFS9BR1\0", 8) ||
       _get(p + 8, 2) != 1 || _get(p + 10, 2) > 4 ||
       _get(p + 12, 2) != kind || _get(p + 14, 2) != flags ||
       _get(p + 28, 4) != 0 || _zero(p + 32, 16) ||
       _get(p + 48, 8) == 0 || _get(p + 48, 8) > HFS9P_MAX_CELL)
      return (EINVAL);
   total = (uint32_t)_get(p + 16, 4);
   offset = (uint32_t)_get(p + 20, 4);
   length = (uint32_t)_get(p + 24, 4);
   if (total == 0 || total > HFS9P_BODY || offset >= total ||
       offset % HFS9P_CHUNK || length > HFS9P_CHUNK ||
       length != (total - offset < HFS9P_CHUNK ? total - offset :
       HFS9P_CHUNK) || n != HFS9P_HEADER + length)
      return (EINVAL);
   return (0);
}

static void
_make_frame(uint8_t *p, Hfs9pEntry *e, uint32_t offset, bool response) {
   uint32_t total = response ? e->response_total : e->native_total;
   uint32_t length = total - offset;
   if (length > HFS9P_CHUNK)
      length = HFS9P_CHUNK;
   memset(p, 0, HFS9P_HEADER);
   memcpy(p, "HFS9BR1\0", 8);
   _put(p + 8, 1, 2);
   _put(p + 10, e->minor, 2);
   _put(p + 12, response ? e->kind : 1, 2);
   _put(p + 14, response ? 0 : e->flags, 2);
   _put(p + 16, total, 4);
   _put(p + 20, offset, 4);
   _put(p + 24, length, 4);
   _put(p + 48, response ? e->guest_cell : e->native_cell, 8);
   memcpy(p + 56, response ? e->response_guest_hash : e->native_hash, 32);
   memcpy(p + HFS9P_HEADER,
       (response ? e->response_guest : e->native) + offset, length);
}

static uint32_t
_terminal_usage(const Hfs9p *h, unsigned session) {
   uint32_t used = 0;
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      const Hfs9pEntry *e = &h->entries[i];
      if (e->session != session || !e->terminal_funded)
         continue;
      used += e->guest ? HFS9P_TERMINAL_GUEST : 0;
      used += e->native ? HFS9P_TERMINAL_NATIVE : 0;
      used += e->response_guest ? HFS9P_TERMINAL_REPLY : 0;
      used += e->response_native ? HFS9P_TERMINAL_REPLY : 0;
      used += e->terminal_state ? sizeof(Hfs9pTerminal) : 0;
   }
   return (used);
}

static bool
_memory(Hfs9p *h, size_t extra, bool control) {
   uint64_t total = sizeof(*h), ordinary = 0;
   for (unsigned i = 0; i < h->session_count; i++)
      total += h->sessions[i].length +
          (h->sessions[i].terminal_package ? sizeof(Hfs9pTerminalPackage) : 0);
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      Hfs9pEntry *e = &h->entries[i];
      uint64_t bytes = (e->guest ? e->total : 0) +
          (e->native ? e->native_total : 0) +
          (e->response_guest ? e->response_total : 0) +
          (e->response_native ? e->native_response_total : 0);
      for (Hfs9pPoll *p = e->polls; p; p = p->previous)
         bytes += sizeof(*p) + p->request_length + p->response_length;
      if (e->terminal_funded)
         bytes = 0; /* Already physically preowned in this scope's package. */
      total += bytes;
      if (!(e->complete ? e->control : e->assembly_control))
         ordinary += bytes;
   }
   return (extra <= HFS9P_MEMORY - (total > HFS9P_MEMORY ?
       HFS9P_MEMORY : total) && (control || extra <=
       HFS9P_ORDINARY_MEMORY - (ordinary > HFS9P_ORDINARY_MEMORY ?
       HFS9P_ORDINARY_MEMORY : ordinary)));
}

static int
_verify(Hfs9p *h) {
   if (h->revoked)
      return (ESTALE);
   for (unsigned i = 0; i < 3; i++) {
      struct stat st;
      if (fstat(h->fd[i], &st) || !S_ISREG(st.st_mode) ||
          (uint64_t)st.st_ino != h->endpoint.node[i] ||
          (uint64_t)st.st_size != HFS9P_VSIZE) {
         h->revoked = true;
         return (ESTALE);
      }
   }
   int error = h->verifier.verify_cb(h->verifier.arg, &h->endpoint, h->fd);
   for (unsigned i = 0; !error && i < h->session_count; i++) {
      Hfs9pSession *s = &h->sessions[i];
      if (s->features != 32767u)
         continue;
      uint8_t controller[16];
      if (!h->verifier.controller_cb)
         error = EACCES;
      else
         error = h->verifier.controller_cb(h->verifier.arg, &h->endpoint,
             h->fd, controller);
      if (!error && memcmp(controller, s->controller, 16))
         error = ESTALE;
   }
   if (error)
      h->revoked = true;
   return (error);
}

static void
_entry_clear(Hfs9pEntry *e) {
   while (e->polls) {
      Hfs9pPoll *p = e->polls;
      e->polls = p->previous;
      free(p->request);
      free(p->response);
      free(p);
   }
   if (!e->terminal_funded) {
      free(e->terminal_state);
      free(e->guest);
      free(e->native);
      free(e->response_guest);
      free(e->response_native);
   }
   memset(e, 0, sizeof(*e));
   e->donor = -1;
}

static int
_construct(Hfs9p **out, const int fd[3], const Hfs9pEndpoint *endpoint,
    Hfs9pVerifier verifier) {
   Hfs9p *h;
   if (!out || !fd || !endpoint || !verifier.verify_cb ||
       !verifier.claim_cb || !verifier.protect_cb ||
       (!!verifier.begin_io_cb != !!verifier.end_io_cb) ||
       _zero(endpoint->session, 16))
      return (EINVAL);
   *out = NULL;
   for (unsigned i = 0; i < 3; i++) {
      if (fd[i] < 0 || endpoint->node[i] == 0)
         return (EINVAL);
      for (unsigned j = 0; j < i; j++) {
         if (fd[i] == fd[j] || endpoint->node[i] == endpoint->node[j])
            return (EINVAL);
      }
   }
   h = calloc(1, sizeof(*h));
   if (!h)
      return (ENOMEM);
   memcpy(h->fd, fd, sizeof(h->fd));
   h->endpoint = *endpoint;
   h->verifier = verifier;
   h->pending = -1;
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++)
      h->entries[i].donor = -1;
   int error = pthread_mutex_init(&h->mutex, NULL);
   if (!error)
      error = _verify(h);
   uint8_t fresh[32] = {0};
   if (!error)
      error = verifier.claim_cb(verifier.arg, endpoint, fresh);
   if (error) {
      free(h);
      return (error);
   }
   error = pthread_create(&h->worker, NULL, _wait_worker_cb, h);
   if (error) {
      pthread_mutex_destroy(&h->mutex);
      free(h);
      return (error);
   }
   *out = h;
   return (0);
}

static bool
_fuse_type(const char *name) {
   return (name && (!strcmp(name, "fusefs") ||
       (!strncmp(name, "fusefs.", 7) && name[7] != '\0')));
}

static int
_native_fds(const int fd[3]) {
#ifndef __FreeBSD__
   (void)_fuse_type;
   (void)fd;
   return (ENOTSUP);
#else
   struct statfs fs;
   if (!fd)
      return (EINVAL);
   for (unsigned i = 0; i < 3; i++) {
      int flags = fcntl(fd[i], F_GETFL);
      if (flags < 0 || (flags & O_APPEND) || !(flags & O_DIRECT) ||
          (flags & O_ACCMODE) != (i == 0 ? O_WRONLY : O_RDONLY) ||
          fstatfs(fd[i], &fs) || !_fuse_type(fs.f_fstypename))
         return (EACCES);
      cap_rights_t rights;
      cap_rights_init(&rights, CAP_FSTAT, CAP_FSTATFS, CAP_FCNTL, CAP_SEEK,
          i == 0 ? CAP_PWRITE : CAP_PREAD);
      if (cap_fcntls_limit(fd[i], CAP_FCNTL_GETFL) ||
          cap_rights_limit(fd[i], &rights))
         return (errno);
   }
   return (0);
#endif
}

int
hfs9p_new(Hfs9p **out, const int fd[3], const Hfs9pEndpoint *endpoint,
    Hfs9pVerifier verifier) {
   if (!out)
      return (EINVAL);
   *out = NULL;
   int error = _native_fds(fd);
   if (error)
      return (error);
   return (_construct(out, fd, endpoint, verifier));
}

int
hfs9p_delete(Hfs9p *h) {
   if (!h)
      return (EINVAL);
   if (pthread_mutex_trylock(&h->mutex))
      return (EAGAIN);
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      if (h->entries[i].guest) {
         pthread_mutex_unlock(&h->mutex);
         return (EBUSY);
      }
   }
   h->stopping = true;
   pthread_mutex_unlock(&h->mutex);
   (void)pthread_join(h->worker, NULL);
   pthread_mutex_lock(&h->mutex);
   for (unsigned i = 0; i < h->session_count; i++) {
      free(h->sessions[i].context);
      free(h->sessions[i].terminal_package);
   }
   for (unsigned i = 0; i < 3; i++)
      (void)close(h->fd[i]);
   pthread_mutex_unlock(&h->mutex);
   pthread_mutex_destroy(&h->mutex);
   free(h);
   return (0);
}

int
hfs9p_add_session_tuple(Hfs9p *h, const uint8_t *context, size_t length,
    uint16_t minor, uint32_t features) {
   Hfs9pJson j;
   uint8_t id[16], fuse[16], controller[16] = {0};
   uint32_t f;
   int error;
   if (!h || !context || length > 4096 || minor > 4 ||
       (features != (minor == 4 ? 16383u : minor == 3 ? 8191u :
       minor == 2 ? 4095u : minor == 1 ? 2047u : 255u) &&
       !(minor == 4 && features == 32767u)))
      return (EINVAL);
   if (pthread_mutex_trylock(&h->mutex))
      return (EAGAIN);
   j = (Hfs9pJson){context, length, h->tokens, 0};
   error = h->recovery_paused ? EAGAIN : hfs9p_json_parse(&j);
   if (!error)
      error = _context(&j, 0, id);
   if (!error && (hfs9p_json_key(&j, 0, "fuse_session", &f) ||
       _hex(&j, f, fuse, 16, true) || memcmp(fuse, h->endpoint.session, 16)))
      error = EACCES;
   for (unsigned i = 0; !error && i < h->session_count; i++) {
      if (memcmp(id, h->sessions[i].id, 16) == 0)
         error = EEXIST;
   }
   if (!error && h->session_count == HFS9P_SESSIONS)
      error = ENOSPC;
   if (!error)
      error = _verify(h);
   if (!error && features == 32767u) {
      if (!h->verifier.controller_cb)
         error = EACCES;
      else
         error = h->verifier.controller_cb(h->verifier.arg, &h->endpoint,
             h->fd, controller);
      if (!error && _zero(controller, sizeof(controller)))
         error = EACCES;
   }
   if (!error) {
      Hfs9pSession *s = &h->sessions[h->session_count];
      size_t reserve = features == 32767u ? sizeof(Hfs9pTerminalPackage) : 0;
      bool terminal_budget = reserve != 0;
      for (unsigned i = 0; i < h->session_count; i++)
         terminal_budget |= h->sessions[i].terminal_package != NULL;
      if (terminal_budget && !_memory(h, reserve + length, true)) {
         error = ENOSPC;
      } else {
         s->context = malloc(length);
         if (reserve)
            s->terminal_package = calloc(1, reserve);
         if (!s->context || (reserve && !s->terminal_package)) {
            free(s->context);
            free(s->terminal_package);
            s->context = NULL;
            s->terminal_package = NULL;
            error = ENOMEM;
         }
      }
      if (!error) {
         memcpy(s->context, context, length);
         memcpy(s->id, id, 16);
         s->length = length;
         s->minor = (uint8_t)minor;
         s->features = features;
         memcpy(s->controller, controller, 16);
         atomic_init(&s->terminal_phase, 0);
         h->session_count++;
      }
   }
   pthread_mutex_unlock(&h->mutex);
   return (error);
}

int
hfs9p_add_session_protocol(Hfs9p *h, const uint8_t *context, size_t length,
    uint16_t minor) {
   uint32_t features = minor == 4 ? 16383u : minor == 3 ? 8191u :
       minor == 2 ? 4095u : minor == 1 ? 2047u : 255u;
   return (hfs9p_add_session_tuple(h, context, length, minor, features));
}

int
hfs9p_add_session(Hfs9p *h, const uint8_t *context, size_t length) {
   return (hfs9p_add_session_protocol(h, context, length, 0));
}

static const char *
_protocol_tuple(uint16_t minor, uint32_t features) {
   if (minor == 4 && features == 32767u)
      return ("{\"body_schema\":5,\"bridge_major\":1,"
          "\"bridge_minor\":4,\"features\":32767,\"msize\":65536,"
          "\"normal_major\":3,\"normal_minor\":0}");
   if (minor == 4)
      return ("{\"body_schema\":5,\"bridge_major\":1,"
          "\"bridge_minor\":4,\"features\":16383,\"msize\":65536,"
          "\"normal_major\":3,\"normal_minor\":0}");
   if (minor == 3)
      return ("{\"body_schema\":4,\"bridge_major\":1,"
          "\"bridge_minor\":3,\"features\":8191,\"msize\":65536,"
          "\"normal_major\":3,\"normal_minor\":0}");
   if (minor == 2)
      return ("{\"body_schema\":3,\"bridge_major\":1,"
          "\"bridge_minor\":2,\"features\":4095,\"msize\":65536,"
          "\"normal_major\":3,\"normal_minor\":0}");
   return (minor ? "{\"body_schema\":2,\"bridge_major\":1,"
       "\"bridge_minor\":1,\"features\":2047,\"msize\":65536,"
       "\"normal_major\":3,\"normal_minor\":0}" :
       "{\"body_schema\":1,\"bridge_major\":1,"
       "\"bridge_minor\":0,\"features\":255,\"msize\":65536,"
       "\"normal_major\":3,\"normal_minor\":0}");
}

static const char *
_protocol(uint16_t minor) {
   return (_protocol_tuple(minor, 0));
}

static int
_protocol_body(const Hfs9pJson *j, uint32_t token, uint16_t minor, uint32_t features) {
   const char *expected = features == 32767u ?
       _protocol_tuple(minor, features) : _protocol(minor);
   Hfs9pToken t = j->tokens[token];
   return (t.end - t.start != strlen(expected) ||
       memcmp(j->bytes + t.start, expected, strlen(expected)) ? EINVAL : 0);
}

static int
_body_version_tuple(Hfs9p *h, const uint8_t *bytes, uint32_t length,
    uint16_t minor, uint32_t features, bool response) {
   const char *const command[] = {"cell", "context", "op", "payload",
      "reclaim", "protocol", "delegation"};
   const char *const reply[] = {"cell", "context", "op", "payload",
      "protocol", "verification"};
   uint32_t fields[7];
   Hfs9pJson j = {bytes, length, h->tokens, 0};
   unsigned count = (response ? 4 : 5) + (minor ? 1 : 0) +
       (response && minor >= 2 ? 1 : 0) + (!response && minor == 4);
   if (hfs9p_json_parse(&j) || _fields(&j, 0,
       response ? reply : command, count, fields) ||
       (minor && _protocol_body(&j, fields[response ? 4 : 5], minor, features)))
      return (EINVAL);
   bool terminal = hfs9p_json_equal(&j, fields[2], "SESSION_RETIRE");
   if (terminal && (minor != 4 || features != 32767u))
      return (EINVAL);
   if (response && minor >= 2 && j.tokens[fields[5]].type == '{') {
      uint32_t kind;
      if (!hfs9p_json_key(&j, fields[5], "kind", &kind) &&
          hfs9p_json_equal(&j, kind, "SESSION_RETIRED") && !terminal)
         return (EINVAL);
   }
   return (0);
}

static int
_body_version(Hfs9p *h, const uint8_t *bytes, uint32_t length,
    uint16_t minor, bool response) {
   return (_body_version_tuple(h, bytes, length, minor, 0, response));
}

static int
_claim(const Hfs9pJson *j, uint32_t obj) {
   const char *const names[] = { "anchor", "nonce", "number",
      "original_hash", "realm", "subject" };
   uint32_t v[6];
   uint8_t bytes[32];
   uint64_t number;
   if (_fields(j, obj, names, 6, v) ||
       _hex(j, v[0], bytes, 16, true) ||
       _hex(j, v[1], bytes, 16, true) ||
       hfs9p_json_u64(j, v[2], &number) || number == 0 ||
       _hex(j, v[3], bytes, 32, false) ||
       _hex(j, v[4], bytes, 16, true))
      return (EINVAL);
   Hfs9pToken t = j->tokens[v[5]];
   if (t.type != '"' || t.end - t.start < 4 ||
       t.end - t.start > 2050 || (t.end - t.start) % 2)
      return (EINVAL);
   for (uint32_t i = t.start + 1; i < t.end - 1; i++) {
      if (_hex_digit(j->bytes[i]) < 0)
         return (EINVAL);
   }
   return (0);
}

static int
_credentials(const Hfs9pJson *j, uint32_t obj) {
   const char *const names[] = { "credential_revision", "fsgid", "fsuid",
      "groups", "idmap_hash", "process", "user_namespace" };
   uint32_t v[7];
   uint8_t bytes[32];
   uint64_t value, last = 0;
   if (_fields(j, obj, names, 7, v) ||
       hfs9p_json_u64(j, v[0], &value) || value == 0 ||
       hfs9p_json_u64(j, v[1], &value) || value > UINT32_MAX ||
       hfs9p_json_u64(j, v[2], &value) || value > UINT32_MAX ||
       j->tokens[v[3]].type != '[' || j->tokens[v[3]].count > 1024 ||
       _hex(j, v[4], bytes, 32, false) ||
       _hex(j, v[5], bytes, 16, true) ||
       _hex(j, v[6], bytes, 16, true))
      return (EINVAL);
   uint32_t i = v[3] + 1;
   for (unsigned n = 0; n < j->tokens[v[3]].count; n++) {
      if (hfs9p_json_u64(j, i, &value) || value > UINT32_MAX ||
          (n && value <= last))
         return (EINVAL);
      last = value;
      i = j->tokens[i].next;
   }
   return (0);
}

/* Validate the fields needed to route safely. Capabilities, times, view unions
 * and Unix semantics are revalidated by the host's closed command parser and
 * service before effects. They are never interpreted as native POSIX requests. */
static int
_args(const Hfs9pJson *j, uint32_t opcode, uint32_t args) {
   static const struct {
      const char *op;
      const char *keys;
   } operations[] = {
      {"ROOT", ""}, {"LOOKUP", "parent name"},
      {"GETATTR", "object"}, {"READLINK", "object"},
      {"CREATE", "parent name mode umask flags"},
      {"MKDIR", "parent name mode umask"},
      {"SYMLINK", "parent name target"}, {"OPEN", "object flags"},
      {"READ", "description offset count"},
      {"WRITE", "description offset data max_end"},
      {"TRUNCATE", "object description size times"},
      {"SETATTR", "object attributes"},
      {"RENAME", "source_parent source_name destination_parent "
         "destination_name no_replace"},
      {"LINK", "object parent name"}, {"UNLINK", "parent name"},
      {"RMDIR", "parent name"},
      {"READDIR", "object after revision limit"}, {"STATFS", "root"},
      {"SYNC", "object description scope"},
      {"DESCRIPTION_CLOSE", "description process close_sequence"},
      {"DESCRIPTION_RELEASE", "description release_sequence"},
      {"FORGET", "object count"}, {"PROCESS_EXIT", "process exit_sequence"},
      {"DRAIN_WRITE", "description offset data recall dirty_entry reservation"}
   };
   const char *keys = NULL;
   char name[32];
   uint32_t token;
   uint64_t value;
   unsigned fields = 0;
   for (unsigned i = 0; i < sizeof(operations) / sizeof(operations[0]); i++) {
      if (hfs9p_json_equal(j, opcode, operations[i].op)) {
         keys = operations[i].keys;
         break;
      }
   }
   if (!keys || j->tokens[args].type != '{')
      return (EINVAL);
   while (*keys) {
      size_t n = strcspn(keys, " ");
      if (n >= sizeof(name))
         return (EINVAL);
      memcpy(name, keys, n);
      name[n] = 0;
      keys += n;
      if (*keys)
         keys++;
      fields++;
      if (hfs9p_json_key(j, args, name, &token))
         return (EINVAL);
      if (!strcmp(name, "mode") || !strcmp(name, "umask") ||
          !strcmp(name, "offset") || !strcmp(name, "size") ||
          !strcmp(name, "max_end")) {
         uint64_t max = !strcmp(name, "mode") ? 4095 :
             (!strcmp(name, "umask") ? 511 : INT64_MAX);
         if (hfs9p_json_u64(j, token, &value) || value > max)
            return (EINVAL);
      }
      if (!strcmp(name, "data")) {
         Hfs9pToken t = j->tokens[token];
         if (t.type != '"' || t.end - t.start > 131074 ||
             (t.end - t.start) % 2)
            return (EINVAL);
         for (uint32_t i = t.start + 1; i < t.end - 1; i++) {
            if (_hex_digit(j->bytes[i]) < 0)
               return (EINVAL);
         }
      }
   }
   return (j->tokens[args].count == fields ? 0 : EINVAL);
}

static int
_ack(Hfs9p *h, Hfs9pJson *j, uint32_t token, unsigned session,
    int *donor) {
   const char *const names[] = { "cell", "response_hash" };
   uint32_t v[2];
   uint64_t cell;
   uint8_t hash[32];
   if (_fields(j, token, names, 2, v) ||
       hfs9p_json_u64(j, v[0], &cell) || !cell ||
       _hex(j, v[1], hash, 32, false))
      return (EINVAL);
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      Hfs9pEntry *e = &h->entries[i];
      uint32_t count = (e->response_total + HFS9P_CHUNK - 1) / HFS9P_CHUNK;
      uint32_t all = count == 32 ? UINT32_MAX : (UINT32_C(1) << count) - 1;
      if (e->guest && e->session == session && e->guest_cell == cell &&
          e->responded && !e->terminal && e->fetched == all &&
          !memcmp(hash, e->response_guest_hash, 32)) {
         *donor = (int)i;
         return (0);
      }
   }
   return (EACCES);
}

static int
_terminal_manifest(Hfs9p *h, Hfs9pJson *j, uint32_t array,
    Hfs9pEntry *e, bool capture) {
   Hfs9pToken list = j->tokens[array];
   int entries[HFS9P_CELLS];
   unsigned count = 0;
   uint64_t previous = 0;
   if (list.type != '[' || list.count > HFS9P_CELLS - 1 ||
       (e->donor < 0 && list.count))
      return (EINVAL);
   if (e->donor >= 0)
      entries[count++] = e->donor;
   uint32_t token = array + 1;
   for (unsigned i = 0; i < list.count; i++) {
      int index = -1;
      int error = _ack(h, j, token, e->session, &index);
      if (error)
         return (error);
      Hfs9pEntry *old = &h->entries[index];
      if (index == e->donor || old->guest_cell <= previous ||
          old->guest_cell >= e->guest_cell)
         return (EINVAL);
      previous = old->guest_cell;
      entries[count++] = index;
      token = j->tokens[token].next;
   }
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      Hfs9pEntry *old = &h->entries[i];
      if (!old->guest || old == e || old->session != e->session)
         continue;
      bool included = false;
      for (unsigned k = 0; k < count; k++)
         included |= entries[k] == (int)i;
      if (!included || !old->complete || !old->issued || !old->responded ||
          old->terminal || (old->wait && !old->final) ||
          old->guest_cell > e->guest_watermark ||
          h->pending == (int)i)
         return (EACCES);
   }
   if (!count && (!e->control || e->donor >= 0))
      return (EINVAL);
   if (!h->sessions[e->session].hello &&
       (count || e->guest_watermark))
      return (EACCES);
   if (!capture)
      return (0);
   Hfs9pTerminalPackage *package = h->sessions[e->session].terminal_package;
   if (!e->terminal_funded || !package || e->terminal_state)
      return (EACCES);
   Hfs9pTerminal *state = &package->state;
   memset(state, 0, sizeof(*state));
   state->count = (uint8_t)count;
   state->funding = 255;
   _hash(j->bytes + list.start, list.end - list.start, state->settled_hash);
   /* Canonical record order is the entire old guest-cell union. */
   for (unsigned i = 0; i < count; i++) {
      for (unsigned k = i + 1; k < count; k++) {
         if (h->entries[entries[k]].guest_cell <
             h->entries[entries[i]].guest_cell) {
            int swap = entries[i];
            entries[i] = entries[k];
            entries[k] = swap;
         }
      }
      Hfs9pEntry *old = &h->entries[entries[i]];
      Hfs9pTerminalOld *record = &state->old[i];
      int error = _mapping_donor(h, old, record);
      if (error) {
         memset(state, 0, sizeof(*state));
         return (error);
      }
      record->entry = (uint8_t)entries[i];
      record->control = old->control;
      state->control += old->control;
      state->ordinary += !old->control;
      if (entries[i] == e->donor)
         state->funding = (uint8_t)i;
   }
   e->terminal_state = state;
   return (0);
}

static int
_terminal_capture(Hfs9p *h, Hfs9pEntry *e) {
   Hfs9pJson j = {e->guest, e->total, h->tokens, 0};
   uint32_t payload, manifest;
   if (hfs9p_json_parse(&j) ||
       hfs9p_json_key(&j, 0, "payload", &payload) ||
       hfs9p_json_key(&j, payload, "settled_cells", &manifest))
      return (EINVAL);
   return (_terminal_manifest(h, &j, manifest, e, true));
}

/* The complete closed terminal grammar fits one normal transport chunk. Classify
 * that immutable packet under the backend lock before copying any body bytes;
 * no ordinary/control header may borrow this session's preowned package. */
static bool
_terminal_input(Hfs9p *h, unsigned session, const uint8_t *frame,
    uint32_t total) {
   const char *const envelope[] = {"cell", "context", "delegation", "op",
      "payload", "protocol", "reclaim"};
   const char *const keys[] = {"closure_hash", "credentials", "guest_through",
      "observer_revision", "retirement_id", "settled_cells"};
   Hfs9pSession *s = &h->sessions[session];
   Hfs9pJson j = {frame + HFS9P_HEADER, total, h->tokens, 0};
   uint32_t v[7], p[6];
   uint64_t number;
   uint8_t hash[32];
   uint64_t cell = _get(frame + 48, 8);
   if (s->features != 32767u || total > HFS9P_TERMINAL_GUEST ||
       hfs9p_json_parse(&j) || _fields(&j, 0, envelope, 7, v) ||
       hfs9p_json_u64(&j, v[0], &number) || number != cell ||
       cell <= s->high || j.tokens[v[2]].type != 'n' ||
       !hfs9p_json_equal(&j, v[3], "SESSION_RETIRE") ||
       _protocol_body(&j, v[5], 4, 32767u) ||
       _fields(&j, v[4], keys, 6, p) ||
       _hex(&j, p[0], hash, 32, false) || _credentials(&j, p[1]) ||
       hfs9p_json_u64(&j, p[2], &number) || number != s->high ||
       hfs9p_json_u64(&j, p[3], &number) || !number ||
       _hex(&j, p[4], hash, 16, true))
      return (false);
   Hfs9pToken context = j.tokens[v[1]];
   if (context.end - context.start != s->length ||
       memcmp(j.bytes + context.start, s->context, s->length))
      return (false);
   _hash(j.bytes, total, hash);
   if (memcmp(hash, frame + 56, 32))
      return (false);
   Hfs9pEntry candidate = {.total = total,
      .guest_cell = cell, .guest_watermark = s->high,
      .session = (uint8_t)session, .minor = 4, .control = true, .donor = -1};
   if (j.tokens[v[6]].type != 'n') {
      if (_ack(h, &j, v[6], session, &candidate.donor))
         return (false);
      candidate.control = h->entries[candidate.donor].control;
   }
   return (!_terminal_manifest(h, &j, p[5], &candidate, false));
}

/* Replacements refer only to top-level routing tokens. Every other byte,
 * including claim/credential/intent/capability scope, is copied unchanged. */
typedef struct {
   uint32_t start;
   uint32_t end;
   const uint8_t *bytes;
   size_t length;
} Hfs9pReplacement;

static int
_rewrite_owned(const Hfs9pJson *j, Hfs9pReplacement *r, unsigned count,
    uint8_t **out, uint32_t *length, uint8_t *owned, size_t capacity) {
   size_t n = j->length, source = 0, dest = 0;
   for (unsigned i = 0; i < count; i++) {
      for (unsigned k = i + 1; k < count; k++) {
         if (r[k].start < r[i].start) {
            Hfs9pReplacement tmp = r[k];
            r[k] = r[i];
            r[i] = tmp;
         }
      }
      if (r[i].end < r[i].start || r[i].end > j->length ||
          (i && r[i].start < r[i - 1].end))
         return (EINVAL);
      n = n - (r[i].end - r[i].start) + r[i].length;
   }
   if (n == 0 || n > HFS9P_BODY)
      return (E2BIG);
   if (owned && n > capacity)
      return (E2BIG);
   uint8_t *p = owned ? owned : malloc(n);
   if (!p)
      return (ENOMEM);
   for (unsigned i = 0; i < count; i++) {
      memcpy(p + dest, j->bytes + source, r[i].start - source);
      dest += r[i].start - source;
      memcpy(p + dest, r[i].bytes, r[i].length);
      dest += r[i].length;
      source = r[i].end;
   }
   memcpy(p + dest, j->bytes + source, j->length - source);
   *out = p;
   *length = (uint32_t)n;
   return (0);
}

static int
_rewrite(const Hfs9pJson *j, Hfs9pReplacement *r, unsigned count,
    uint8_t **out, uint32_t *length) {
   return (_rewrite_owned(j, r, count, out, length, NULL, 0));
}

static int
_command(Hfs9p *h, Hfs9pEntry *e) {
   const char *const names[] = { "cell", "context", "op", "payload",
      "reclaim", "protocol", "delegation" };
   uint32_t v[7], a[3], in[2], claim_hash;
   const char *const call_keys[] = { "claim", "credentials", "intent" };
   const char *const intent_keys[] = { "args", "opcode" };
   Hfs9pJson j = { e->guest, e->total, h->tokens, 0 };
   uint64_t cell;
   uint8_t hash[32], expected[32];
   uint16_t minor = h->sessions[e->session].minor;
   if (e->minor != minor || hfs9p_json_parse(&j) ||
       _fields(&j, 0, names, minor == 4 ? 7 : minor ? 6 : 5, v) ||
       (minor && _protocol_body(&j, v[5], minor, h->sessions[e->session].features)) ||
       (minor == 4 && j.tokens[v[6]].type != 'n') ||
       hfs9p_json_u64(&j, v[0], &cell) || cell != e->guest_cell)
      return (EINVAL);
   Hfs9pSession *s = &h->sessions[e->session];
   Hfs9pToken context = j.tokens[v[1]];
   if (context.end - context.start != s->length ||
       memcmp(j.bytes + context.start, s->context, s->length))
      return (EACCES);
   e->control = true;
   e->release = false;
   e->hello = false;
   e->donor = -1;
   if (hfs9p_json_equal(&j, v[2], "HELLO")) {
      if (_protocol_body(&j, v[3], minor, s->features))
         return (EINVAL);
      e->control = false;
      e->hello = true;
   } else if (hfs9p_json_equal(&j, v[2], "CALL") ||
       (minor >= 2 && (hfs9p_json_equal(&j, v[2], "ALLOCATE") ||
       hfs9p_json_equal(&j, v[2], "GAP_RESOLVE")))) {
      if (_fields(&j, v[3], call_keys, 3, a) || _claim(&j, a[0]) ||
          _credentials(&j, a[1]) ||
          _fields(&j, a[2], intent_keys, 2, in) || _args(&j, in[1], in[0]) ||
          hfs9p_json_key(&j, a[0], "original_hash", &claim_hash) ||
          _hex(&j, claim_hash, expected, 32, false))
         return (EINVAL);
      Hfs9pToken intent = j.tokens[a[2]];
      _hash(j.bytes + intent.start, intent.end - intent.start, hash);
      if (memcmp(hash, expected, 32))
         return (EINVAL);
      bool call = hfs9p_json_equal(&j, v[2], "CALL");
      bool drain = hfs9p_json_equal(&j, in[1], "DRAIN_WRITE");
      if (!call && drain)
         return (EINVAL);
      e->control = (call && drain) ||
          hfs9p_json_equal(&j, v[2], "GAP_RESOLVE");
   } else if (minor >= 3 &&
       hfs9p_json_equal(&j, v[2], "READDIR_CLEANUP")) {
      const char *const keys[] = {"capabilities", "claim", "credentials", "result_id"};
      uint32_t cleanup[4];
      if (_fields(&j, v[3], keys, 4, cleanup) ||
          j.tokens[cleanup[0]].type != '[' || _claim(&j, cleanup[1]) ||
          _credentials(&j, cleanup[2]) || _hex(&j, cleanup[3], hash, 32, false))
         return (EINVAL);
      e->control = true;
   } else if (hfs9p_json_equal(&j, v[2], "SESSION_RETIRE")) {
      const char *const keys[] = {"closure_hash", "credentials", "guest_through",
         "observer_revision", "retirement_id", "settled_cells"};
      uint32_t values[6];
      uint64_t through, revision;
      if (minor != 4 || s->features != 32767u ||
          _fields(&j, v[3], keys, 6, values) ||
          _hex(&j, values[0], hash, 32, false) ||
          _credentials(&j, values[1]) ||
          hfs9p_json_u64(&j, values[2], &through) ||
          hfs9p_json_u64(&j, values[3], &revision) || !revision ||
          _hex(&j, values[4], hash, 16, true) ||
          through != e->guest_watermark || through >= e->guest_cell ||
          s->high != e->guest_cell)
         return (EINVAL);
      e->terminal = true;
   } else if (hfs9p_json_equal(&j, v[2], "RELEASE_CELL")) {
      Hfs9pToken p = j.tokens[v[3]], r = j.tokens[v[4]];
      if (p.end - p.start != r.end - r.start ||
          memcmp(j.bytes + p.start, j.bytes + r.start, p.end - p.start))
         return (EINVAL);
      e->release = true;
   } else if (hfs9p_json_equal(&j, v[2], "QUERY") ||
       hfs9p_json_equal(&j, v[2], "CANCEL")) {
      const char *const keys[] = { "claim" };
      if (_fields(&j, v[3], keys, 1, a) || _claim(&j, a[0]))
         return (EINVAL);
   } else if (hfs9p_json_equal(&j, v[2], "VIEW_WAIT")) {
      const char *const keys[] = { "after_sequence", "domain" };
      uint32_t p[2], kind;
      uint64_t after;
      if (e->total > 16384 || _fields(&j, v[3], keys, 2, p) ||
          hfs9p_json_u64(&j, p[0], &after) ||
          hfs9p_json_key(&j, p[1], "kind", &kind) ||
          !hfs9p_json_equal(&j, kind, "DOMAIN"))
         return (EINVAL);
      e->wait = true;
      e->deadline = _now() + 1000;
      e->next_poll = _now();
   } else if (hfs9p_json_equal(&j, v[2], "VIEW_POLL")) {
      return (EINVAL);
   } else if (minor && (hfs9p_json_equal(&j, v[2], "VIEW_ACQUIRE") ||
       hfs9p_json_equal(&j, v[2], "DIRTY_RESERVE"))) {
      /* Acquisitions never consume the independently reserved drain lane.
       * The host checks the closed schema and current provenance before effects. */
      e->control = false;
   } else if (minor && (hfs9p_json_equal(&j, v[2], "VIEW_RELEASE") ||
       hfs9p_json_equal(&j, v[2], "VIEW_RENEW") ||
       hfs9p_json_equal(&j, v[2], "VIEW_QUERY") ||
       hfs9p_json_equal(&j, v[2], "VIEW_CANCEL") ||
       hfs9p_json_equal(&j, v[2], "VIEW_RETIRE") ||
       hfs9p_json_equal(&j, v[2], "DIRTY_FREEZE"))) {
      e->control = true;
   } else if (!hfs9p_json_equal(&j, v[2], "RETIRE") &&
       !hfs9p_json_equal(&j, v[2], "RESUME") &&
       !hfs9p_json_equal(&j, v[2], "VIEW_ACK")) {
      return (EINVAL);
   }
   if (!e->hello && !s->hello &&
       !(e->terminal && !e->guest_watermark))
      return (EACCES);
   if (j.tokens[v[4]].type != 'n') {
      int error = _ack(h, &j, v[4], e->session, &e->donor);
      if (error)
         return (error);
      Hfs9pEntry *d = &h->entries[e->donor];
      if (d == e || (!e->release && !e->terminal && e->control != d->control))
         return (EACCES);
      if (e->release || e->terminal)
         e->control = d->control;
   } else if (e->release) {
      return (EINVAL);
   }
   if (e->terminal) {
      uint32_t manifest;
      if (hfs9p_json_key(&j, v[3], "settled_cells", &manifest))
         return (EINVAL);
      int error = _terminal_manifest(h, &j, manifest, e, false);
      if (error)
         return (error);
   }
   e->flags = (e->control ? 1 : 0) | (e->donor >= 0 ? 2 : 0);
   return (0);
}

static int
_native_body(Hfs9p *h, Hfs9pEntry *e) {
   uint32_t cell, reclaim, target, target_hash, payload, op;
   Hfs9pJson j = { e->guest, e->total, h->tokens, 0 };
   Hfs9pReplacement r[7];
   char native[24], donor[24], hash[67];
   unsigned count = 0;
   if (hfs9p_json_parse(&j) || hfs9p_json_key(&j, 0, "cell", &cell))
      return (EINVAL);
   int n = snprintf(native, sizeof(native), "%llu",
       (unsigned long long)e->native_cell);
   r[count++] = (Hfs9pReplacement){j.tokens[cell].start, j.tokens[cell].end,
      (uint8_t *)native, (size_t)n};
   if (e->wait) {
      if (hfs9p_json_key(&j, 0, "op", &op))
         return (EINVAL);
      r[count++] = (Hfs9pReplacement){j.tokens[op].start,
         j.tokens[op].end, (const uint8_t *)"\"VIEW_POLL\"", 11};
   }
   char internal_ack[128];
   if (e->wait && e->polls) {
      Hfs9pPoll *p = e->polls;
      char digest[65];
      for (unsigned i = 0; i < 32; i++)
         (void)snprintf(digest + i * 2, 3, "%02x", p->response_hash[i]);
      n = snprintf(internal_ack, sizeof(internal_ack),
          "{\"cell\":%llu,\"response_hash\":\"%s\"}",
          (unsigned long long)p->cell, digest);
      if (hfs9p_json_key(&j, 0, "reclaim", &reclaim))
         return (EINVAL);
      r[count++] = (Hfs9pReplacement){j.tokens[reclaim].start,
         j.tokens[reclaim].end, (uint8_t *)internal_ack, (size_t)n};
   } else if (e->donor >= 0) {
      Hfs9pEntry *d = &h->entries[e->donor];
      n = snprintf(donor, sizeof(donor), "%llu",
          (unsigned long long)d->native_cell);
      hash[0] = '"';
      for (unsigned i = 0; i < 32; i++)
         (void)snprintf(hash + 1 + i * 2, 3, "%02x", d->response_native_hash[i]);
      hash[65] = '"';
      hash[66] = 0;
      if (hfs9p_json_key(&j, 0, "reclaim", &reclaim))
         return (EINVAL);
      for (unsigned i = 0; i < (e->release ? 2u : 1u); i++) {
         uint32_t obj = reclaim;
         if (i && hfs9p_json_key(&j, 0, "payload", &payload))
            return (EINVAL);
         if (i)
            obj = payload;
         if (hfs9p_json_key(&j, obj, "cell", &target) ||
             hfs9p_json_key(&j, obj, "response_hash", &target_hash))
            return (EINVAL);
         r[count++] = (Hfs9pReplacement){j.tokens[target].start,
            j.tokens[target].end, (uint8_t *)donor, (size_t)n};
         r[count++] = (Hfs9pReplacement){j.tokens[target_hash].start,
            j.tokens[target_hash].end, (uint8_t *)hash, 66};
      }
   }
   /* Borrow the immutable original Ack/op, never an internal poll's Ack.
    * Metadata is a candidate; only authenticated host ingress admits it. */
   char delegation[512];
   if (e->minor == 4) {
      uint32_t metadata;
      if (hfs9p_json_key(&j, 0, "delegation", &metadata) ||
          j.tokens[metadata].type != 'n' ||
          hfs9p_json_key(&j, 0, "op", &op) ||
          hfs9p_json_key(&j, 0, "reclaim", &reclaim) ||
          e->poll_count >= 16)
         return (EINVAL);
      Hfs9pToken gop = j.tokens[op], ack = j.tokens[reclaim];
      int written = snprintf(delegation, sizeof(delegation),
          "{\"guest_cell\":%llu,\"guest_op\":%.*s,"
          "\"guest_reclaim\":%.*s,\"poll_index\":%u,"
          "\"version\":\"homelabfs-native-delegation/1\"}",
          (unsigned long long)e->guest_cell,
          (int)(gop.end - gop.start), j.bytes + gop.start,
          (int)(ack.end - ack.start), j.bytes + ack.start, e->poll_count);
      if (written < 0 || (size_t)written >= sizeof(delegation))
         return (E2BIG);
      r[count++] = (Hfs9pReplacement){j.tokens[metadata].start,
         j.tokens[metadata].end, (const uint8_t *)delegation, (size_t)written};
   }
   uint32_t length;
   Hfs9pTerminalPackage *package = h->sessions[e->session].terminal_package;
   if (!e->terminal_funded && !_memory(h, e->total +
       (e->minor == 4 ? sizeof(delegation) : 256), e->control))
      return (ENOSPC);
   int error = e->terminal_funded ?
       _rewrite_owned(&j, r, count, &e->native, &length,
       package->native, sizeof(package->native)) :
       _rewrite(&j, r, count, &e->native, &length);
   if (error)
      return (error);
   /* Preflight the actual rewritten token bound before issuance/input ACK.
    * Reuse the existing arena; no command or parser budget is enlarged. */
   Hfs9pJson preflight = {e->native, length, h->tokens, 0};
   if (e->minor == 4 && hfs9p_json_parse(&preflight)) {
      if (!e->terminal_funded)
         free(e->native);
      e->native = NULL;
      return (E2BIG);
   }
   /* Native cell digit expansion may create one extra final chunk. */
   e->native_total = length;
   _hash(e->native, e->native_total, e->native_hash);
   return (0);
}

/* Guest order (e64, zq3-r6 R6-2). The guest allocates a session's cells in
 * order under its writer mutex, but a SEND refused with CAPACITY (endpoint
 * mutex contention) or EBUSY (a fixed tag still held, task 964) yields that
 * writer for 10-12 ms before the retry. A later cell of the same session can
 * then be copied completely first. Inside the exporter a complete cell could
 * also be overtaken: one blocked by another candidate's writer pin was
 * retried only by its own FETCH or the wait worker, while a later cell's SEND
 * or FETCH found the writer free. The runtime captures the session's guest
 * high-water at each native cell's first header and refuses an original at
 * or below it unless it is a hole of its 16-cell reorder window, so arrival
 * order made it refuse VIEW_WAIT guest cell 48 as native 108 after QUERY
 * guest cell 49 as native 107.
 *
 * Native numbers therefore go to one session's originals in guest order:
 *  - A complete lower original is forwarded first, in the same call. If a
 *    pinned writer blocks it, this cell is blocked by the same pin, and the
 *    first call after the pin clears forwards the lower cell first.
 *  - A lower cell that was never seen, or is still incomplete, is waited for,
 *    at most HFS9P_ORDER_HOLD_MS. An abandoned or never completed cell (the
 *    guest's own SEND deadline is 30 s) must not stall the session.
 *  - A complete lower cell that cannot go for its own reasons (no ordinary
 *    credit, EAGAIN on the first chunk of a multi-chunk cell, an error before
 *    its write) is not waited for.
 *    Control, replacement and donor traffic keeps bypassing ordinary traffic,
 *    as before e64.
 * A bypassed or given-up cell that arrives below the high-water relies on the
 * runtime's reorder window (it admits an unissued cell once, inside 16 cells
 * below the high-water). Once a cell 16 or more guest numbers above it was
 * admitted, the runtime refuses it as stale, and the exporter fails it back
 * to the guest instead of pinning the writer (p64, _permanent_original).
 * Replacement polls and replays of an existing candidate are not originals
 * and never wait. */
#define HFS9P_ORDER_HOLD_MS 1000u

/* Lowest present original of e's session above after and below e that is not
 * issued and not a replacement poll. A pinned candidate qualifies: it already
 * holds its native number but has not been issued. */
static Hfs9pEntry *
_order_lower(Hfs9p *h, const Hfs9pEntry *e, uint64_t after) {
   Hfs9pEntry *lowest = NULL;
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      Hfs9pEntry *l = &h->entries[i];
      if (!l->guest || l == e || l->session != e->session || l->issued ||
          l->polls || l->guest_cell <= after || l->guest_cell >= e->guest_cell)
         continue;
      if (!lowest || l->guest_cell < lowest->guest_cell)
         lowest = l;
   }
   return (lowest);
}

/* True when some guest cell strictly between from and to was never seen in
 * this session, or is present but still incomplete. */
static bool
_order_missing(Hfs9p *h, const Hfs9pEntry *e, uint64_t from) {
   const Hfs9pSession *s = &h->sessions[e->session];
   uint64_t next = from + 1;
   for (unsigned i = 0; i < s->seen_count && next < e->guest_cell; i++) {
      if (s->seen[i].last < next)
         continue;
      if (s->seen[i].first > next)
         return (true);
      next = s->seen[i].last + 1;
   }
   if (next < e->guest_cell)
      return (true);
   for (Hfs9pEntry *l = _order_lower(h, e, from); l;
       l = _order_lower(h, e, l->guest_cell)) {
      if (!l->complete)
         return (true);
   }
   return (false);
}

/* Arms e's hold on its first wait; true once the hold has run out. */
static bool
_order_expired(Hfs9pEntry *e) {
   uint64_t now = _now();
   if (!e->order_deadline)
      e->order_deadline = now + HFS9P_ORDER_HOLD_MS;
   return (now >= e->order_deadline);
}

static int _forward(Hfs9p *, Hfs9pEntry *);

/* 0 when original e may take the next native number of its session, EAGAIN
 * while it waits for a missing or incomplete lower cell. Complete lower cells
 * are offered the writer first, lowest first. An expired e hands its deadline
 * down, so a lower cell that only waits for a gap below it still goes first.
 * The nested _guest_order of such a lower cell only checks its own gaps
 * (h->ordering): this walk already offers every complete cell below it, so
 * each cell is tried once per call and the recursion is one level deep. */
static int
_guest_order(Hfs9p *h, Hfs9pEntry *e) {
   Hfs9pSession *s = &h->sessions[e->session];
   if (e->guest_cell <= s->order_high)
      return (0); /* A late cell: nothing below it is still awaited. */
   bool expired = _order_expired(e);
   if (!h->ordering) {
      h->ordering = true;
      uint64_t after = s->order_high;
      Hfs9pEntry *l;
      while ((l = _order_lower(h, e, after)) != NULL) {
         after = l->guest_cell;
         if (expired && (!l->order_deadline ||
             l->order_deadline > e->order_deadline))
            l->order_deadline = e->order_deadline;
         if (l->complete)
            (void)_forward(h, l); /* It may fail back and be cleared. */
         if (h->pending >= 0)
            break; /* A pinned writer blocks every later cell, e too. */
      }
      h->ordering = false;
   }
   if (!expired && _order_missing(h, e, s->order_high))
      return (EAGAIN);
   return (0);
}

/* Builds e's native candidate: guest order, donor recheck, credit and the
 * next native number. Nothing here writes to the native endpoint. */
static int
_forward_candidate(Hfs9p *h, Hfs9pEntry *e) {
   unsigned index = (unsigned)(e - h->entries);
   if (e->terminal && !e->terminal_state) {
      int error = _terminal_capture(h, e);
      if (error)
         return (error);
   }
   if (!e->polls) {
      int error = _guest_order(h, e);
      if (error)
         return (error);
      /* Forwarding a lower cell may have left it pinned as the writer. */
      if (h->pending >= 0 && (unsigned)h->pending != index)
         return (EAGAIN);
   }
   if (e->donor >= 0) {
      Hfs9pJson j = {e->guest, e->total, h->tokens, 0};
      uint32_t reclaim;
      int donor = -1;
      if (hfs9p_json_parse(&j) ||
          hfs9p_json_key(&j, 0, "reclaim", &reclaim) ||
          _ack(h, &j, reclaim, e->session, &donor) || donor != e->donor)
         return (EACCES);
   }
   if (e->donor < 0 && !e->polls &&
       (e->control ? h->control >= 16 : h->ordinary >= 64))
      return (EAGAIN);
   if (h->high == HFS9P_MAX_CELL)
      return (ENOSPC);
   e->native_cell = h->high + 1;
   int error = _native_body(h, e);
   if (error)
      e->native_cell = 0;
   return (error);
}

/* Protects a fresh snapshot of the whole exporter state. */
static int
_protect_snapshot(Hfs9p *h) {
   uint8_t *ledger = NULL;
   size_t ledger_length = 0;
   int protection = _snapshot(h, &ledger, &ledger_length);
   if (!protection)
      protection = h->verifier.protect_cb(h->verifier.arg, ledger, ledger_length);
   free(ledger);
   return (protection);
}

/* Protect exact candidate and donor snapshots before any possible native
 * effect. The persisted pending candidate is replayed unchanged on recovery. */
static int
_forward_protect(Hfs9p *h, Hfs9pEntry *e) {
   h->pending = (int)(e - h->entries);
   if (e->terminal) {
      Hfs9pSession *session = &h->sessions[e->session];
      if (session->terminal_phase &&
          (session->terminal_guest != e->guest_cell || session->terminal_native != e->native_cell))
         return (EACCES);
      session->terminal_phase = 1;
      session->terminal_guest = e->guest_cell;
      session->terminal_native = e->native_cell;
   }
   int protection = _protect_snapshot(h);
   if (protection && e->terminal)
      h->revoked = true;
   return (protection);
}

/* Permanent refusals of original cells (e64 for donor sends, p64 for the
 * rest). Replacement polls and terminal cells are never classified and keep
 * the pre-e64 pin. The runtime is internal/guestbridge Transport.Write,
 * reached through the FUSE adapter's errno map in internal/fuseadapter.
 *
 * What both kinds share: an admitted cell stays in the runtime's table,
 * unreleased until the exporter reads its response and a later cell
 * reclaims it, and an identical resend of an admitted cell returns the full
 * length. So ESTALE (fsapi.Stale) and EINVAL (fsapi.Invalid) never mean
 * "admitted, receipt lost", with two exceptions:
 *  - A ledger protection failure after admission can wrap Invalid, but it
 *    also fences the whole runtime transport and the cell is not
 *    dispatched; its durable ledger never held the cell.
 *  - Authentication runs before the cell lookup. On FreeBSD the native
 *    connection check returns fsapi.Stale when the mount on the endpoint
 *    path changed (controlledruntime nativeConnection.check). The adapter's
 *    own credential snapshot runs the same check first and maps its failure
 *    to EACCES, so this ESTALE needs the mount to change between two checks
 *    of one request. A resend of an admitted cell could see it.
 * EACCES (fsapi.Permission) also comes from those checks before the lookup,
 * and from the adapter when its per-request credential snapshot fails
 * (PrivateControl.native), which may be transient. EPERM is never produced
 * (the adapter maps Permission to EACCES), so its meaning is unknown.
 * EAGAIN (WouldBlock) is transient. EHOSTDOWN, ENOSPC, EIO, EINTR and every
 * other errno are uncertain: a protection or transport failure may follow
 * admission. Transient and uncertain outcomes keep the exact candidate
 * pinned.
 *
 * Donor send (e64): reclaim, poll index 0, not SESSION_RETIRE. The runtime
 * builds it in its reclaim workspace and advances its native high-water
 * only on admission, so a refusal never consumes the native number. Since
 * n64 it also frees the workspace when it refuses the complete send ESTALE,
 * EINVAL or EACCES, so the next cell can take that number.
 * Permanent, on the final chunk only (w64, see _permanent_refusal), are
 * EINVAL on any attempt, and ESTALE (stale guest cell, released or spent
 * native cell) and EACCES only on a first attempt. A
 * replay could see the authentication ESTALE or EACCES of a send that an
 * earlier attempt admitted, so a replay stays pinned (p64 narrowed e64's
 * ESTALE rule to first attempts). Failing a first-attempt EACCES back is
 * safe, since nothing was admitted, but it drops a cell that a retry might
 * have delivered. */
static bool
_permanent_donor(int error, bool replay) {
   if (error == EINVAL)
      return (true);
   return ((error == ESTALE || error == EACCES) && !replay);
}

/* Non-donor original (p64), for example a complete ordinary cell that later
 * control or donor traffic bypassed and that the runtime then refused as a
 * stale guest cell (prepareMapping: the cell fell out of the 16-cell guest
 * reorder window). The runtime allocates such a cell's native number at its
 * first chunk, before any content check. Since n64 it removes a complete
 * cell refused this way with its credit but keeps the number spent (before,
 * the cell stayed incomplete in its table for good). A fail back must
 * therefore know that the number was spent, or the next cell reuses a spent
 * number or leaves a gap, and every later cell would then be refused too.
 * Permanent, with the number spent, on the final chunk only (w64, see
 * _permanent_refusal), are:
 *  - ESTALE on a first attempt. It comes from the guest freshness check or
 *    a duplicate guest cell (after allocation), or from a native number at
 *    or below the runtime's high-water (spent already). A replay could see
 *    the authentication ESTALE of an admitted cell, so a replay stays
 *    pinned. A first attempt's ESTALE can also be that race (the mount
 *    changes before the second or third check of one request). The number
 *    was then not spent, but the cell is failed back and the high-water
 *    moves past it anyway, so the next cell's first write is refused EINVAL
 *    for the gap and stays pinned for good. The race is narrow but real.
 *  - EINVAL once an earlier chunk of this write was accepted (inserted):
 *    the runtime allocated the number at that chunk. On the first chunk
 *    EINVAL is either a content refusal after allocation or a frame or
 *    numbering refusal before it, which the exporter cannot tell apart, so
 *    a single-chunk cell refused EINVAL stays pinned.
 * EACCES is never classified here: it is either an authentication refusal
 * before allocation (possibly transient) or a content refusal after it. */
static bool
_permanent_original(int error, bool replay, bool inserted) {
   if (error == ESTALE)
      return (!replay);
   return (error == EINVAL && inserted);
}

/* True when the refused candidate e is failed back instead of pinned. offset
 * is the refused chunk. replay is true when an earlier attempt wrote the
 * final chunk (final_sent before this attempt reached it, w64 review): only
 * such an attempt can have been admitted, so only then can this refusal be
 * the authentication ESTALE or EACCES of an admitted cell. An attempt that
 * is the first to reach the final chunk uses the first-attempt rules even
 * after earlier attempts failed on an earlier chunk; otherwise a validation
 * refusal there (the runtime discards the cell, whatever the attempt) would
 * stay pinned for good, refused ESTALE at chunk 0 or, for a donor, at the
 * final chunk on every replay.
 *
 * Only a refusal of the final chunk qualifies (w64). The runtime validates a
 * cell, and cleans up after a permanent refusal (discardRefused in
 * internal/guestbridge/refusal.go), only once every chunk is in, which is
 * the final chunk of an in-order write. A refusal of an earlier chunk came
 * before validation: from authentication (the mount-change ESTALE), the
 * adapter's credential snapshot (EACCES), or a frame, cell or chunk check.
 * The runtime then still holds the chunks accepted so far: a donor send's
 * half-filled reclaim workspace, on which the next cell at that number gets
 * WouldBlock (the n64 wedge), or a numbered cell that stays incomplete with
 * its credit while a fail-back would move the high-water past it (p64's
 * stuck credit). So such a refusal keeps the exact candidate pinned, and the
 * replay resends every chunk from the first into what the runtime kept.
 * This also pins the first chunk of a multi-chunk cell, which can only be
 * refused before allocation: a donor send has nothing in the workspace yet,
 * and a non-donor ESTALE there cannot prove its number spent (except
 * _unallocated_refusal's EAGAIN, which drops the candidate). A single-chunk
 * cell's only chunk is its final chunk, so its rules are unchanged.
 *
 * On the final chunk the errno rules below match discardRefused: the same
 * excluded commands (pinnedByExporter), the donor errnos (donorPermanent),
 * and for a numbered cell ESTALE or EINVAL after an earlier chunk
 * (numberedPermanent, Offset > 0). A residual race stays: the authentication
 * ESTALE or credential EACCES can also hit the final chunk, before
 * validation, and looks the same. A first attempt then fails back although
 * the runtime kept the other chunks; the window is one chunk write instead
 * of every chunk after the first. A final-chunk EINVAL from a frame, cell
 * or chunk check (writeFrame, writeCell, storeChunk) also fails back
 * without the runtime discarding anything; the exporter's frames are
 * deterministic, so only an exporter bug produces one.
 *
 * On a replay an earlier chunk can also revalidate: a resend of a stored
 * chunk of a complete runtime cell that is still unadmitted (kept after
 * EAGAIN, for example) validates it again, and the runtime may then discard
 * it. That needs no fail-back here: final_sent is set (the cell was
 * complete), so a refusal stays pinned under the replay rules, as before
 * w64, and after a donor replay EINVAL on an earlier chunk the next replay
 * rebuilds the freed workspace and meets the same refusal on the final
 * chunk, which then fails back. */
static bool
_permanent_refusal(const Hfs9pEntry *e, int error, bool replay,
    uint32_t offset) {
   if (e->polls || e->terminal || e->terminal_funded)
      return (false);
   if (e->native_total - offset > HFS9P_CHUNK)
      return (false);
   if (e->donor >= 0)
      return (_permanent_donor(error, replay));
   return (_permanent_original(error, replay, offset > 0));
}

/* Fails a permanently refused cell back to the guest instead of pinning the
 * native writer on bytes the runtime will never admit. The guest assembly is
 * dropped but its seen mark stays, so the guest's FETCH or resend of the
 * cell gets ESTALE and the cell number is never reused. A donor send's donor
 * entry stays (e64): the runtime did not reclaim it and did not spend the
 * native number. A non-donor original's native number was spent (p64, see
 * _permanent_original), so the native high-water moves to it. Since n64 the
 * runtime removes such a refused cell with its ordinary or control credit
 * (internal/guestbridge/refusal.go), so neither side counts that credit any
 * more; the guest treats the ERROR as an uncertain result and keeps that
 * cell's slot reserved. A fresh
 * snapshot is protected so that recovery does not replay the refused
 * candidate. */
static int
_fail_back(Hfs9p *h, Hfs9pEntry *e, int error) {
   Hfs9pSession *s = &h->sessions[e->session];
   if (e->guest_cell > s->order_high)
      s->order_high = e->guest_cell;
   if (e->donor < 0 && e->native_cell > h->high)
      h->high = e->native_cell;
   h->pending = -1;
   _entry_clear(e);
   int protection = _protect_snapshot(h);
   return (protection ? protection : error);
}

/* One positional native chunk write between the paired I/O hooks. The return
 * value is a verify or hook refusal from before pwrite. *refusal is 0 after a
 * complete write, else the native errno (EIO for a short write). */
static int
_native_write(Hfs9p *h, Hfs9pEntry *e, uint32_t offset, int *refusal) {
   uint32_t length = e->native_total - offset;
   if (length > HFS9P_CHUNK)
      length = HFS9P_CHUNK;
   *refusal = 0;
   _make_frame(h->scratch, e, offset, false);
   memcpy(h->scratch + 32, h->sessions[e->session].id, 16);
   int error = _verify(h);
   if (error)
      return (error);
   if (h->verifier.begin_io_cb &&
       (error = h->verifier.begin_io_cb(h->verifier.arg,
       &h->endpoint, h->fd)))
      return (error);
   ssize_t written = pwrite(h->fd[0], h->scratch, HFS9P_HEADER + length,
       (off_t)(e->native_cell * HFS9P_STRIDE +
       (offset / HFS9P_CHUNK) * (HFS9P_HEADER + HFS9P_CHUNK)));
   int native_errno = errno;
   if (h->verifier.end_io_cb)
      h->verifier.end_io_cb(h->verifier.arg);
   if (written != (ssize_t)(HFS9P_HEADER + length))
      *refusal = written < 0 ? native_errno : EIO;
   return (0);
}

/* True when a refused native write certainly left the runtime without
 * this candidate's native number, so the candidate may be dropped and later
 * cells may take the number. That holds only for WouldBlock (EAGAIN) on the
 * first chunk of a first, non-donor, non-terminal, non-poll write of a cell
 * with more than one chunk. Transport.Write allocates a non-donor cell's
 * number at its first chunk, after the credit, incomplete-cell,
 * recent-marker and workspace checks that return WouldBlock, and validates
 * the command only once every chunk is in. A single-chunk cell is validated
 * in the same call, after allocation, and can then also get WouldBlock, when
 * its dispatch queue is full (the drain and acquisition queues are smaller
 * than the credits that can reach them) or for the recent-marker limit. The
 * exporter cannot tell the two apart: the same errno, and a result-node read
 * of the number returns WouldBlock both for an allocated cell without a
 * response and for an unallocated number above the high-water. So a
 * single-chunk cell refused EAGAIN stays pinned and replays its exact bytes
 * with the same number (p64). If the number was allocated, the resend
 * revalidates the stored cell; if not, it allocates the number again. The
 * cost: such a cell no longer lets control or donor traffic bypass it while
 * the runtime refuses it, and a refusal that persists blocks the writer.
 * (Before n64 cells failed back after allocation kept their runtime credit
 * and caused exactly such persistent refusals; the runtime now removes
 * them.)
 *
 * "First write" needs replay as well as e->uncertain: _forward_protect
 * persists the candidate before its write, so after an exporter restart a
 * pinned candidate comes back with uncertain false and only pending marks
 * it. If an earlier attempt's later chunks completed the runtime cell, an
 * identical first chunk revalidates it and can get WouldBlock after
 * allocation, so a replay is never dropped. */
static bool
_unallocated_refusal(const Hfs9pEntry *e, uint32_t offset, int refusal,
    bool replay) {
   return (offset == 0 && e->native_total > HFS9P_CHUNK && !e->terminal &&
       !replay && !e->uncertain && e->donor < 0 && !e->polls &&
       (refusal == EAGAIN || refusal == EWOULDBLOCK));
}

/* Marks e's final chunk as sent before it is written and returns whether an
 * earlier attempt sent it already (*earlier). A replay that is the first to
 * reach the final chunk protects the mark before the write, so a restored
 * replay that finds it clear knows that no attempt after the first reached
 * the final chunk (_final_restore). A first attempt saves that protection:
 * its mark reaches the ledger with the next snapshot, and a crash before
 * one is covered by _final_restore. */
static int
_final_mark(Hfs9p *h, Hfs9pEntry *e, bool replay, bool *earlier) {
   *earlier = e->final_sent;
   if (e->final_sent)
      return (0);
   e->final_sent = true;
   int error = replay ? _protect_snapshot(h) : 0;
   if (error)
      e->final_sent = false;
   return (error);
}

/* Pins the refused candidate e: even an error can lose a transport receipt,
 * so the exact candidate bytes and writer ownership stay, and its number is
 * never allocated to others. The pin is protected (w64 review round 2): it
 * makes uncertain and the in-memory final_sent durable, so a restart before
 * the next attempt restores exactly how far the attempts got
 * (_final_restore). Without it a first attempt refused on a middle chunk was
 * restored as if it had sent its final chunk, and a later validation
 * refusal there stayed pinned for good. One protection per refusal; a
 * protection failure is returned instead of EAGAIN and keeps the pin. */
static int
_forward_pin(Hfs9p *h, Hfs9pEntry *e) {
   e->uncertain = true;
   int protection = _protect_snapshot(h);
   return (protection ? protection : EAGAIN);
}

/* After a restore, the pending candidate's final_sent is exact whenever the
 * restored uncertain flag is set: every ended attempt that wrote a chunk
 * protected its pin (_forward_pin), and a replay protects the mark before
 * its final chunk (_final_mark). A pending candidate restored without
 * uncertain was interrupted during its first attempt's writes (a crash or
 * an exit inside _forward_write, before the pin or issue), or stopped before
 * writing anything; the ledger cannot show whether its final chunk went
 * out, so it is treated as sent and keeps the replay rules. */
static void
_final_restore(Hfs9p *h) {
   if (h->pending >= 0 && !h->entries[h->pending].uncertain)
      h->entries[h->pending].final_sent = true;
}

/* Writes every chunk of the protected candidate, always from the first. A
 * native refusal drops the candidate without a native number when
 * _unallocated_refusal says nothing was allocated, fails it back when it is
 * permanent (only on the final chunk, w64), and otherwise pins the exact
 * uncertain candidate. replay is true when an earlier attempt already
 * reached protection (pending, also after a restart); the final-chunk
 * classification asks instead whether an earlier attempt wrote the final
 * chunk (_final_mark). A refusal after an
 * accepted chunk of this write (offset > 0) proves that the runtime
 * allocated the candidate's native number (p64, _permanent_original). */
static int
_forward_write(Hfs9p *h, Hfs9pEntry *e, bool replay) {
   int index = (int)(e - h->entries);
   for (uint32_t offset = 0; offset < e->native_total;
       offset += HFS9P_CHUNK) {
      int refusal;
      bool final = e->native_total - offset <= HFS9P_CHUNK, earlier = false;
      int error = final ? _final_mark(h, e, replay, &earlier) : 0;
      if (!error)
         error = _native_write(h, e, offset, &refusal);
      if (error) {
         if (final && !earlier)
            e->final_sent = false; /* Nothing was written. */
         return (error);
      }
      h->pending = index;
      if (!refusal) {
         e->uncertain = true;
         continue;
      }
      if (_unallocated_refusal(e, offset, refusal, replay)) {
         free(e->native);
         e->native = NULL;
         e->native_cell = 0;
         h->pending = -1;
         return (EAGAIN);
      }
      if (_permanent_refusal(e, refusal, earlier, offset))
         return (_fail_back(h, e, refusal));
      return (_forward_pin(h, e));
   }
   return (0);
}

/* Commits a fully written candidate: native high-water, the session's guest
 * order floor, and the credit, poll or donor transition. */
static void
_forward_issued(Hfs9p *h, Hfs9pEntry *e) {
   Hfs9pSession *s = &h->sessions[e->session];
   h->high = e->native_cell;
   h->pending = -1;
   e->issued = true;
   e->final_sent = false;
   if (e->guest_cell > s->order_high)
      s->order_high = e->guest_cell;
   if (e->wait)
      e->poll_count++;
   if (e->terminal && e->terminal_state->count) {
      /* Funding remains in transition; all old exporter rows and class
       * charges survive input ACK until the protected full native proof. */
   } else if (e->polls) {
      e->polls->acknowledged = true;
   } else if (e->donor >= 0) {
      _entry_clear(&h->entries[e->donor]);
      e->donor = -1;
   } else if (e->control) {
      h->control++;
   } else {
      h->ordinary++;
   }
}

/* The sole framing writer never holds a vnode lock across semantic work. All
 * native callbacks are bounded copy/readiness operations. EAGAIN on the
 * first chunk of a multi-chunk non-donor write consumes no native number, so
 * controls may bypass it (_unallocated_refusal). Any
 * uncertain/partially copied candidate pins the writer to its exact bytes,
 * except an original cell whose final chunk is refused permanently, which
 * fails back (e64 for donor sends, p64 for the rest, w64 for the final-chunk
 * rule, see _permanent_refusal). */
static int
_forward(Hfs9p *h, Hfs9pEntry *e) {
   unsigned index = (unsigned)(e - h->entries);
   if (e->issued)
      return (0);
   if (h->pending >= 0 && (unsigned)h->pending != index)
      return (EAGAIN);
   bool replay = h->pending == (int)index || e->uncertain;
   if (!e->native) {
      int error = _forward_candidate(h, e);
      if (error)
         return (error);
   }
   int error = _forward_protect(h, e);
   if (!error)
      error = _forward_write(h, e, replay);
   if (!error)
      _forward_issued(h, e);
   return (error);
}

static int
_native_read(Hfs9p *h, unsigned fd, uint8_t *bytes, size_t length,
    uint64_t offset) {
   size_t done = 0;
   while (done < length) {
      size_t n = length - done;
      if (n > HFS9P_CHUNK)
         n = HFS9P_CHUNK;
      int error = _verify(h);
      if (error)
         return (error);
      if (h->verifier.begin_io_cb &&
          (error = h->verifier.begin_io_cb(h->verifier.arg,
          &h->endpoint, h->fd)))
         return (error);
      ssize_t got = pread(h->fd[fd], bytes + done, n, (off_t)(offset + done));
      int native_errno = errno;
      if (h->verifier.end_io_cb)
         h->verifier.end_io_cb(h->verifier.arg);
      if (got < 0)
         return (native_errno);
      if (got == 0)
         return (EIO);
      done += (size_t)got;
   }
   return (0);
}

static int
_response(Hfs9p *h, Hfs9pEntry *e) {
   uint32_t offset = 0, total = 0;
   uint8_t expected[32];
   if (e->responded)
      return (e->terminal ? _terminal_final(h, e) : 0);
   for (;;) {
      uint64_t position = e->native_cell * HFS9P_STRIDE +
          (offset / HFS9P_CHUNK) * (HFS9P_HEADER + HFS9P_CHUNK);
      int error = _native_read(h, e->wait ? 2 : 1, h->scratch, HFS9P_HEADER, position);
      if (error)
         return (error);
      uint32_t length = (uint32_t)_get(h->scratch + 24, 4);
      /* Validate all routing metadata BEFORE using header values to allocate
       * or read body bytes. The body length is capped again by _frame. */
      if (length == 0 || length > HFS9P_CHUNK)
         return (EINVAL);
      error = _native_read(h, e->wait ? 2 : 1, h->scratch + HFS9P_HEADER, length,
          position + HFS9P_HEADER);
      if (error)
         return (error);
      if (_frame(h->scratch, HFS9P_HEADER + length, e->flags, e->wait ? 3 : 2) ||
          _get(h->scratch + 10, 2) != e->minor ||
          _get(h->scratch + 48, 8) != e->native_cell ||
          _get(h->scratch + 20, 4) != offset ||
          memcmp(h->scratch + 32, h->sessions[e->session].id, 16))
         return (EINVAL);
      if (offset == 0) {
         total = (uint32_t)_get(h->scratch + 16, 4);
         memcpy(expected, h->scratch + 56, 32);
         if (!e->response_native) {
            if (e->terminal_funded) {
               Hfs9pTerminalPackage *package =
                   h->sessions[e->session].terminal_package;
               if (total > sizeof(package->response_native))
                  return (E2BIG);
               e->response_native = package->response_native;
            } else {
               if (!_memory(h, total, e->control))
                  return (ENOSPC);
               e->response_native = malloc(total);
               if (!e->response_native)
                  return (ENOMEM);
            }
            e->response_total = total;
            e->native_response_total = total;
            memcpy(e->response_native_hash, expected, 32);
         } else if (total != e->native_response_total ||
             memcmp(expected, e->response_native_hash, 32)) {
            return (EINVAL);
         }
      } else if (_get(h->scratch + 16, 4) != total ||
          memcmp(expected, h->scratch + 56, 32)) {
         return (EINVAL);
      }
      memcpy(e->response_native + offset, h->scratch + HFS9P_HEADER, length);
      offset += length;
      if (offset == total)
         break;
   }
   uint8_t hash[32];
   _hash(e->response_native, total, hash);
   if (memcmp(hash, expected, 32))
      return (EINVAL);
   Hfs9pJson response = {e->response_native, total, h->tokens, 0};
   const char *const fields[] = { "cell", "context", "op", "payload",
      "protocol", "verification" };
   uint32_t v[6], op;
   uint64_t cell;
   if (hfs9p_json_parse(&response) ||
       _fields(&response, 0, fields, e->minor >= 2 ? 6 : e->minor ? 5 : 4, v) ||
       (e->minor && _protocol_body(&response, v[4], e->minor, h->sessions[e->session].features)) ||
       hfs9p_json_u64(&response, v[0], &cell) || cell != e->native_cell)
      return (EINVAL);
   if (e->minor >= 2 && !e->terminal &&
       response.tokens[v[5]].type == '{') {
      uint32_t kind;
      if (!hfs9p_json_key(&response, v[5], "kind", &kind) &&
          hfs9p_json_equal(&response, kind, "SESSION_RETIRED"))
         return (EINVAL);
   }
   Hfs9pSession *s = &h->sessions[e->session];
   Hfs9pToken context = response.tokens[v[1]], response_op = response.tokens[v[2]];
   if (context.end - context.start != s->length ||
       memcmp(response.bytes + context.start, s->context, s->length))
      return (EACCES);
   /* Save offsets before reusing the single parser arena for the guest body. */
   uint32_t cell_start = response.tokens[v[0]].start;
   uint32_t cell_end = response.tokens[v[0]].end;
   uint32_t op_start = response_op.start, op_length = response_op.end - op_start;
   Hfs9pJson request = {e->guest, e->total, h->tokens, 0};
   if (hfs9p_json_parse(&request) || hfs9p_json_key(&request, 0, "op", &op))
      return (EINVAL);
   Hfs9pToken request_op = request.tokens[op];
   if ((!e->wait && (request_op.end - request_op.start != op_length ||
       memcmp(request.bytes + request_op.start,
       e->response_native + op_start, op_length))) ||
       (e->wait && (op_length != 11 ||
       memcmp(e->response_native + op_start, "\"VIEW_POLL\"", 11))))
      return (EINVAL);
   /* The actual host generated this result. No backend return value or timer
    * can fabricate syscall success, terminal claim status or no-effect. */
   char guest[24];
   int n = snprintf(guest, sizeof(guest), "%llu",
       (unsigned long long)e->guest_cell);
   Hfs9pReplacement r[2] = {
      {cell_start, cell_end, (uint8_t *)guest, (size_t)n},
      {op_start, op_start + op_length, (const uint8_t *)"\"VIEW_WAIT\"", 11}
   };
   response.tokens = h->tokens;
   uint32_t translated_total;
   if (!e->terminal_funded && !_memory(h, total + 32, e->control))
      return (ENOSPC);
   Hfs9pTerminalPackage *package = h->sessions[e->session].terminal_package;
   int error = e->terminal_funded ?
       _rewrite_owned(&response, r, 1, &e->response_guest, &translated_total,
       package->response_guest, sizeof(package->response_guest)) :
       _rewrite(&response, r, e->wait ? 2 : 1, &e->response_guest,
       &translated_total);
   if (error)
      return (error);
   e->response_total = translated_total;
   _hash(e->response_guest, translated_total, e->response_guest_hash);
   e->responded = true;
   e->kind = e->wait ? 3 : 2;
   if (e->hello) {
      /* Exact HELLO selection/status check, not a substring/boolean oracle. */
      Hfs9pJson reply = {e->response_guest, translated_total, h->tokens, 0};
      uint32_t payload, status, selection;
      const char *const keys[] = { "selection", "status" };
      uint32_t p[2];
      if (hfs9p_json_parse(&reply) ||
          hfs9p_json_key(&reply, 0, "payload", &payload) ||
          _fields(&reply, payload, keys, 2, p))
         return (EINVAL);
      selection = p[0];
      status = p[1];
      Hfs9pToken selected = reply.tokens[selection];
      uint32_t selected_start = selected.start, selected_size =
          selected.end - selected.start;
      bool accepted = hfs9p_json_equal(&reply, status, "ACCEPTED");
      if (hfs9p_json_parse(&request) ||
          hfs9p_json_key(&request, 0, "payload", &payload))
         return (EINVAL);
      Hfs9pToken proposed = request.tokens[payload];
      if (accepted && proposed.end - proposed.start == selected_size &&
          !memcmp(request.bytes + proposed.start,
          e->response_guest + selected_start, selected_size))
         s->hello = true;
   }
   return (e->terminal ? _terminal_final(h, e) : 0);
}

/* Compare immutable token slices without a second parser arena or body copy. */
static bool
_same_token(const uint8_t *a, Hfs9pToken x, const uint8_t *b,
    Hfs9pToken y) {
   return (x.end - x.start == y.end - y.start &&
       !memcmp(a + x.start, b + y.start, x.end - x.start));
}

static int
_translation_matches(Hfs9p *h, Hfs9pEntry *e) {
   const char *const keys[] = {"cell", "context", "op", "payload",
      "protocol", "verification"};
   uint32_t values[6];
   Hfs9pToken native[6];
   Hfs9pJson j = {e->response_native, e->native_response_total, h->tokens, 0};
   uint64_t cell;
   if (hfs9p_json_parse(&j) || _fields(&j, 0, keys, 6, values) ||
       hfs9p_json_u64(&j, values[0], &cell) || cell != e->native_cell)
      return (EINVAL);
   for (unsigned i = 0; i < 6; i++)
      native[i] = j.tokens[values[i]];
   j.bytes = e->response_guest;
   j.length = e->response_total;
   if (hfs9p_json_parse(&j) || _fields(&j, 0, keys, 6, values) ||
       hfs9p_json_u64(&j, values[0], &cell) || cell != e->guest_cell)
      return (EINVAL);
   for (unsigned i = 1; i < 6; i++) {
      if (i == 2 && e->wait) {
         if (native[i].end - native[i].start != 11 ||
             memcmp(e->response_native + native[i].start, "\"VIEW_POLL\"", 11) ||
             !hfs9p_json_equal(&j, values[i], "VIEW_WAIT"))
            return (EINVAL);
      } else if (!_same_token(e->response_native, native[i],
          e->response_guest, j.tokens[values[i]])) {
         return (EINVAL);
      }
   }
   return (0);
}

/* Canonical verification is the last envelope field. Hash its exact omission,
 * including the closing brace, without another body-sized staging buffer. */
static int
_terminal_base(const uint8_t *bytes, size_t length, Hfs9pToken receipt,
    uint8_t hash[32]) {
   const char marker[] = ",\"verification\":";
   size_t n = sizeof(marker) - 1;
   if (receipt.start < n || receipt.end + 1 != length ||
       bytes[length - 1] != '}' ||
       memcmp(bytes + receipt.start - n, marker, n))
      return (EINVAL);
#ifdef __FreeBSD__
   SHA256_CTX ctx;
   SHA256_Init(&ctx);
   SHA256_Update(&ctx, bytes, receipt.start - n);
   SHA256_Update(&ctx, "}", 1);
   SHA256_Final(hash, &ctx);
#else
   EVP_MD_CTX *ctx = EVP_MD_CTX_new();
   if (!ctx)
      return (ENOMEM);
   int ok = EVP_DigestInit_ex(ctx, EVP_sha256(), NULL) &&
       EVP_DigestUpdate(ctx, bytes, receipt.start - n) &&
       EVP_DigestUpdate(ctx, "}", 1) &&
       EVP_DigestFinal_ex(ctx, hash, NULL);
   EVP_MD_CTX_free(ctx);
   if (!ok)
      return (EIO);
#endif
   return (0);
}

static void
_hex_string(const uint8_t *bytes, size_t length, char *text) {
   static const char digits[] = "0123456789abcdef";
   for (size_t i = 0; i < length; i++) {
      text[i * 2] = digits[bytes[i] >> 4];
      text[i * 2 + 1] = digits[bytes[i] & 15];
   }
   text[length * 2] = 0;
}

static void
_uuid_string(const uint8_t bytes[16], char text[37]) {
   char hex[33];
   _hex_string(bytes, 16, hex);
   (void)snprintf(text, 37, "%.8s-%.4s-%.4s-%.4s-%.12s",
       hex, hex + 8, hex + 12, hex + 16, hex + 20);
}

static int
_mapping_leg(Hfs9p *h, Hfs9pEntry *e, const uint8_t *request,
    uint32_t request_length, const uint8_t *response, uint32_t response_length,
    uint64_t cell, unsigned index, uint8_t chain[32]) {
   Hfs9pJson j = {response, response_length, h->tokens, 0};
   uint32_t verification, v;
   uint8_t base[32], command[32], observed[32];
   uint64_t number;
   if (hfs9p_json_parse(&j) ||
       hfs9p_json_key(&j, 0, "verification", &verification) ||
       hfs9p_json_key(&j, 0, "cell", &v) ||
       hfs9p_json_u64(&j, v, &number) || number != cell ||
       _terminal_base(response, response_length, j.tokens[verification], base))
      return (EINVAL);
   _hash(request, request_length, command);
   char prior[65], command_text[65], base_text[65], encoded[512];
   _hex_string(chain, 32, prior);
   _hex_string(command, 32, command_text);
   _hex_string(base, 32, base_text);
   int n = snprintf(encoded, sizeof(encoded),
       "{\"domain\":\"homelabfs-mapping-leg/1\",\"index\":%u,"
       "\"native_cell\":%llu,\"native_command_hash\":\"%s\","
       "\"native_response_hash\":\"%s\",\"previous\":\"%s\"}",
       index, (unsigned long long)cell, command_text, base_text, prior);
   if (n < 0 || (size_t)n >= sizeof(encoded))
      return (E2BIG);
   _hash((const uint8_t *)encoded, (size_t)n, chain);
   if (j.tokens[verification].type == 'n')
      return (0);
   if (j.tokens[verification].type != '{' ||
       hfs9p_json_key(&j, verification, "version", &v) ||
       !hfs9p_json_equal(&j, v, "homelabfs-controlled-verification/2") ||
       hfs9p_json_key(&j, verification, "mapping_hash", &v) ||
       _hex(&j, v, observed, 32, false) || memcmp(observed, chain, 32) ||
       hfs9p_json_key(&j, verification, "controller", &v) ||
       _hex(&j, v, observed, 16, true) ||
       memcmp(observed, h->sessions[e->session].controller, 16) ||
       hfs9p_json_key(&j, verification, "guest_cell", &v) ||
       hfs9p_json_u64(&j, v, &number) || number != e->guest_cell ||
       hfs9p_json_key(&j, verification, "cell", &v) ||
       hfs9p_json_u64(&j, v, &number) || number != cell ||
       hfs9p_json_key(&j, verification, "poll_count", &v) ||
       hfs9p_json_u64(&j, v, &number) || number != index + 1 ||
       hfs9p_json_key(&j, verification, "command_hash", &v) ||
       _hex(&j, v, observed, 32, false) || memcmp(observed, command, 32) ||
       hfs9p_json_key(&j, verification, "response_hash", &v) ||
       _hex(&j, v, observed, 32, false) || memcmp(observed, base, 32) ||
       hfs9p_json_key(&j, verification, "guest_command_hash", &v) ||
       _hex(&j, v, observed, 32, false) || memcmp(observed, e->guest_hash, 32))
      return (EINVAL);
   return (0);
}

/* Reconstruct C8's exact chain from actual authenticated native I/O and the
 * supervised immutable controller. No receipt is required for null HELLO; no
 * public mapping hash or caller UUID is an observation of that native tuple. */
static int
_mapping_donor(Hfs9p *h, Hfs9pEntry *e, Hfs9pTerminalOld *record) {
   Hfs9pSession *s = &h->sessions[e->session];
   if (e->minor != 4 || s->features != 32767u ||
       _zero(s->controller, 16) || !e->guest || !e->native ||
       !e->response_native || !e->response_guest || !e->issued ||
       !e->responded || (e->wait && !e->final))
      return (EACCES);
   uint32_t chunks = (e->response_total + HFS9P_CHUNK - 1) / HFS9P_CHUNK;
   uint32_t all = chunks == 32 ? UINT32_MAX : (UINT32_C(1) << chunks) - 1;
   if ((!e->terminal && e->fetched != all) || _translation_matches(h, e))
      return (EACCES);
   char controller[37], fuse[37], original[65], seed[8192];
   _uuid_string(s->controller, controller);
   _uuid_string(h->endpoint.session, fuse);
   _hex_string(e->guest_hash, 32, original);
   int n = snprintf(seed, sizeof(seed),
       "{\"controller\":\"%s\",\"domain\":\"homelabfs-mapping-chain/1\","
       "\"endpoint\":{\"command_node\":%llu,\"fuse_session\":\"%s\","
       "\"recall_node\":%llu,\"result_node\":%llu},\"guest_cell\":%llu,"
       "\"guest_command_hash\":\"%s\",\"scope\":%.*s}", controller,
       (unsigned long long)h->endpoint.node[0], fuse,
       (unsigned long long)h->endpoint.node[2],
       (unsigned long long)h->endpoint.node[1],
       (unsigned long long)e->guest_cell, original, (int)s->length, s->context);
   if (n < 0 || (size_t)n >= sizeof(seed))
      return (E2BIG);
   uint8_t chain[32];
   _hash((const uint8_t *)seed, (size_t)n, chain);
   Hfs9pPoll *polls[16];
   unsigned count = 0;
   for (Hfs9pPoll *p = e->polls; p; p = p->previous) {
      if (count == 15)
         return (EINVAL);
      polls[count++] = p;
   }
   for (unsigned i = 0; i < count; i++) {
      Hfs9pPoll *p = polls[count - i - 1];
      int error = _mapping_leg(h, e, p->request, p->request_length,
          p->response, p->response_length, p->cell, i, chain);
      if (error)
         return (error);
   }
   int error = _mapping_leg(h, e, e->native, e->native_total,
       e->response_native, e->native_response_total,
       e->native_cell, count, chain);
   if (error)
      return (error);
   record->guest_cell = e->guest_cell;
   record->native_cell = e->native_cell;
   memcpy(record->guest_hash, e->guest_hash, 32);
   memcpy(record->native_hash, e->native_hash, 32);
   memcpy(record->mapping_hash, chain, 32);
   memcpy(record->response_guest_hash, e->response_guest_hash, 32);
   memcpy(record->response_native_hash, e->response_native_hash, 32);
   return (0);
}

/* Structural/native binding checks only. The enrolled guest kernel verifies
 * the signature and creates private Received; parsing creates no authority. */
static int
_terminal_response(Hfs9p *h, Hfs9pEntry *e) {
   const char *const request_keys[] = {"retirement_id", "credentials",
      "guest_through", "observer_revision", "closure_hash", "settled_cells"};
   const char *const response_keys[] = {"retirement_id", "guest_through",
      "observer_revision", "closure_hash", "ownership_hash", "status",
      "settled_hash"};
   const char *const evidence_keys[] = {"retirement_id", "credentials",
      "guest_through", "observer_revision", "closure_hash",
      "ownership_hash", "retained_owner", "settled_hash"};
   const char *const receipt_keys[] = {"version", "kind", "scope",
      "endpoint", "controller", "serving_generation", "cell",
      "command_hash", "response_hash", "evidence", "signature",
      "guest_cell", "guest_command_hash", "guest_response_hash",
      "poll_count", "mapping_hash"};
   Hfs9pJson j = {e->guest, e->total, h->tokens, 0};
   uint32_t payload, values[16], p[7], evidence[8], verification;
   Hfs9pToken original[5];
   uint8_t hash[64], base[32];
   uint64_t number;
   if (hfs9p_json_parse(&j) ||
       hfs9p_json_key(&j, 0, "payload", &payload) ||
       _fields(&j, payload, request_keys, 6, values))
      return (EINVAL);
   for (unsigned i = 0; i < 5; i++)
      original[i] = j.tokens[values[i]];
   Hfs9pToken manifest = j.tokens[values[5]];
   uint8_t settled_hash[32];
   _hash(j.bytes + manifest.start, manifest.end - manifest.start, settled_hash);
   if (!e->terminal_state ||
       memcmp(settled_hash, e->terminal_state->settled_hash, 32))
      return (EINVAL);
   j.bytes = e->response_native;
   j.length = e->native_response_total;
   if (hfs9p_json_parse(&j) ||
       hfs9p_json_key(&j, 0, "payload", &payload) ||
       hfs9p_json_key(&j, 0, "verification", &verification) ||
       _fields(&j, payload, response_keys, 7, p) ||
       !hfs9p_json_equal(&j, p[5], "RETIRED") ||
       _hex(&j, p[4], hash, 32, false) ||
       _fields(&j, verification, receipt_keys, 16, values) ||
       !hfs9p_json_equal(&j, values[0],
       "homelabfs-controlled-verification/2") ||
       !hfs9p_json_equal(&j, values[1], "SESSION_RETIRED") ||
       _fields(&j, values[9], evidence_keys, 8, evidence) ||
       !hfs9p_json_equal(&j, evidence[6], "C8_TERMINAL_CELL") ||
       _hex(&j, evidence[7], hash, 32, false) ||
       memcmp(hash, settled_hash, 32) ||
       _hex(&j, p[6], hash, 32, false) || memcmp(hash, settled_hash, 32) ||
       _hex(&j, values[10], hash, 64, false) ||
       _hex(&j, values[4], hash, 16, true) ||
       memcmp(hash, h->sessions[e->session].controller, 16) ||
       _hex(&j, values[5], hash, 16, true) ||
       _hex(&j, values[15], hash, 32, false) || _zero(hash, 32) ||
       hfs9p_json_u64(&j, values[6], &number) ||
       number != e->native_cell ||
       hfs9p_json_u64(&j, values[11], &number) ||
       number != e->guest_cell ||
       hfs9p_json_u64(&j, values[14], &number) || number != 1 ||
       _hex(&j, values[7], hash, 32, false) ||
       memcmp(hash, e->native_hash, 32) ||
       _hex(&j, values[12], hash, 32, false) ||
       memcmp(hash, e->guest_hash, 32))
      return (EINVAL);
   Hfs9pSession *s = &h->sessions[e->session];
   Hfs9pToken scope = j.tokens[values[2]];
   if (scope.end - scope.start != s->length ||
       memcmp(j.bytes + scope.start, s->context, s->length))
      return (EACCES);
   const char *const endpoint_keys[] = {"fuse_session", "command_node",
      "result_node", "recall_node"};
   uint32_t endpoint[4];
   if (_fields(&j, values[3], endpoint_keys, 4, endpoint) ||
       _hex(&j, endpoint[0], hash, 16, true) ||
       memcmp(hash, h->endpoint.session, 16))
      return (EINVAL);
   for (unsigned i = 0; i < 3; i++) {
      if (hfs9p_json_u64(&j, endpoint[i + 1], &number) ||
          number != h->endpoint.node[i])
         return (EINVAL);
   }
   for (unsigned i = 0; i < 5; i++) {
      if (!_same_token(e->guest, original[i], j.bytes,
          j.tokens[evidence[i]]))
         return (EINVAL);
      if (i != 1 && !_same_token(e->guest, original[i], j.bytes,
          j.tokens[p[i ? i - 1 : 0]]))
         return (EINVAL);
   }
   if (!_same_token(j.bytes, j.tokens[p[4]], j.bytes,
       j.tokens[evidence[5]]) ||
       _terminal_base(j.bytes, j.length, j.tokens[verification], base) ||
       _hex(&j, values[8], hash, 32, false) || memcmp(hash, base, 32))
      return (EINVAL);
   uint8_t guest_base_expected[32];
   if (_hex(&j, values[13], guest_base_expected, 32, false))
      return (EINVAL);
   j.bytes = e->response_guest;
   j.length = e->response_total;
   if (hfs9p_json_parse(&j) ||
       hfs9p_json_key(&j, 0, "verification", &verification) ||
       _terminal_base(j.bytes, j.length, j.tokens[verification], base) ||
       memcmp(base, guest_base_expected, 32) || _translation_matches(h, e))
      return (EINVAL);
   Hfs9pTerminalOld actual;
   return (_mapping_donor(h, e, &actual));
}

/* Read-only validation of the complete pre/post owner cut. Old records remain
 * inert commitments after deletion; decoding them never enrolls a controller. */
static int
_terminal_records(Hfs9p *h) {
   for (unsigned si = 0; si < h->session_count; si++) {
      Hfs9pSession *s = &h->sessions[si];
      unsigned terminals = 0;
      if ((s->features == 32767u) == _zero(s->controller, 16) ||
          (s->features == 32767u) != (s->terminal_package != NULL) ||
          _terminal_usage(h, si) > sizeof(Hfs9pTerminalPackage))
         return (EINVAL);
      for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
         Hfs9pEntry *e = &h->entries[i];
         if (!e->guest || e->session != si)
            continue;
         Hfs9pTerminalPackage *package = s->terminal_package;
         if (e->terminal_funded && (!package ||
             e->guest != package->guest || e->total > sizeof(package->guest) ||
             (e->native && e->native != package->native) ||
             e->native_total > sizeof(package->native) ||
             (e->response_guest && e->response_guest != package->response_guest) ||
             e->response_total > sizeof(package->response_guest) ||
             (e->response_native && e->response_native != package->response_native) ||
             e->native_response_total > sizeof(package->response_native) ||
             (e->terminal_state && e->terminal_state != &package->state)))
            return (EINVAL);
         Hfs9pJson j = {e->guest, e->total, h->tokens, 0};
         uint32_t op;
         if (e->complete && (hfs9p_json_parse(&j) ||
             hfs9p_json_key(&j, 0, "op", &op) ||
             !!hfs9p_json_equal(&j, op, "SESSION_RETIRE") != e->terminal))
            return (EINVAL);
         if (!e->terminal) {
            if (e->terminal_state)
               return (EINVAL);
            continue;
         }
         terminals++;
         if (s->minor != 4 || s->features != 32767u || !e->terminal_funded ||
             !e->complete || e->release || e->hello || e->wait || e->polls ||
             e->poll_count || e->guest_watermark >= e->guest_cell ||
             s->high != e->guest_cell)
            return (EINVAL);
         bool staged = !e->native && !e->native_total && !e->native_cell &&
             !e->issued && !e->uncertain && !e->responded &&
             !s->terminal_phase && h->pending != (int)i;
         Hfs9pTerminal *state = e->terminal_state;
         if (!staged && (!state || !e->native || !e->native_cell ||
             !s->terminal_phase || s->terminal_guest != e->guest_cell ||
             s->terminal_native != e->native_cell ||
             (s->terminal_phase == 3 && (!e->issued || !e->responded)) ||
             (!e->issued && h->pending != (int)i)))
            return (EINVAL);
         const char *const names[] = {"cell", "context", "op", "payload",
            "reclaim", "protocol", "delegation"};
         const char *const keys[] = {"retirement_id", "credentials",
            "guest_through", "observer_revision", "closure_hash", "settled_cells"};
         const char *const ack_keys[] = {"cell", "response_hash"};
         uint32_t v[7], p[6], a[2];
         Hfs9pToken original[7];
         uint64_t number, cells[80];
         uint8_t hash[32], hashes[80][32];
         unsigned count = 0;
         uint64_t funding_cell = 0;
         if (hfs9p_json_parse(&j) || _fields(&j, 0, names, 7, v) ||
             !hfs9p_json_equal(&j, v[2], "SESSION_RETIRE") ||
             j.tokens[v[6]].type != 'n' ||
             hfs9p_json_u64(&j, v[0], &number) || number != e->guest_cell ||
             _protocol_body(&j, v[5], 4, 32767u) ||
             _fields(&j, v[3], keys, 6, p) ||
             _hex(&j, p[0], hash, 16, true) || _credentials(&j, p[1]) ||
             hfs9p_json_u64(&j, p[2], &number) || number != e->guest_watermark ||
             hfs9p_json_u64(&j, p[3], &number) || !number ||
             _hex(&j, p[4], hash, 32, false) ||
             j.tokens[p[5]].type != '[' || j.tokens[p[5]].count > 79)
            return (EINVAL);
         Hfs9pToken manifest = j.tokens[p[5]];
         _hash(j.bytes + manifest.start, manifest.end - manifest.start, hash);
         if (state && memcmp(hash, state->settled_hash, 32))
            return (EINVAL);
         uint32_t token = p[5] + 1;
         for (unsigned k = 0; k < manifest.count; k++) {
            if (_fields(&j, token, ack_keys, 2, a) ||
                hfs9p_json_u64(&j, a[0], &cells[count]) ||
                !cells[count] || cells[count] > e->guest_watermark ||
                (count && cells[count] <= cells[count - 1]) ||
                _hex(&j, a[1], hashes[count], 32, false))
               return (EINVAL);
            count++;
            token = j.tokens[token].next;
         }
         if (j.tokens[v[4]].type != 'n') {
            if (_fields(&j, v[4], ack_keys, 2, a) ||
                hfs9p_json_u64(&j, a[0], &funding_cell) ||
                !funding_cell || funding_cell > e->guest_watermark ||
                _hex(&j, a[1], hashes[count], 32, false))
               return (EINVAL);
            cells[count++] = funding_cell;
            for (unsigned k = count - 1; k && cells[k] < cells[k - 1]; k--) {
               uint64_t swap = cells[k];
               cells[k] = cells[k - 1];
               cells[k - 1] = swap;
               memcpy(hash, hashes[k], 32);
               memcpy(hashes[k], hashes[k - 1], 32);
               memcpy(hashes[k - 1], hash, 32);
            }
            for (unsigned k = 1; k < count; k++) {
               if (cells[k] == cells[k - 1])
                  return (EINVAL);
            }
         } else if (count || !e->control || e->donor >= 0) {
            return (EINVAL);
         }
         if (!s->hello && (count || e->guest_watermark))
            return (EINVAL);
         for (unsigned k = 0; k < 7; k++)
            original[k] = j.tokens[v[k]];
         if (original[1].end - original[1].start != s->length ||
             memcmp(e->guest + original[1].start, s->context, s->length))
            return (EINVAL);
         if (staged && !state) {
            if (_terminal_manifest(h, &j, p[5], e, false))
               return (EINVAL);
            continue;
         }
         if (!state || state->count != count ||
             state->ordinary + state->control != count ||
             state->ordinary > 64 || state->control > 16 ||
             (state->removed != (s->terminal_phase == 3)))
            return (EINVAL);
         unsigned ordinary = 0, control = 0, funding = 255;
         for (unsigned k = 0; k < count; k++) {
            Hfs9pTerminalOld *record = &state->old[k];
            if (record->entry >= HFS9P_ENTRIES ||
                record->guest_cell != cells[k] || !record->native_cell ||
                record->native_cell > h->high ||
                (!staged && record->native_cell >= e->native_cell) ||
                memcmp(record->response_guest_hash, hashes[k], 32) ||
                _zero(record->guest_hash, 32) || _zero(record->native_hash, 32) ||
                _zero(record->mapping_hash, 32))
               return (EINVAL);
            for (unsigned q = 0; q < k; q++) {
               if (state->old[q].native_cell == record->native_cell ||
                   state->old[q].entry == record->entry)
                  return (EINVAL);
            }
            if (record->guest_cell == funding_cell)
               funding = k;
            ordinary += !record->control;
            control += record->control;
            if (!state->removed) {
               Hfs9pEntry *old = &h->entries[record->entry];
               Hfs9pTerminalOld actual = {0};
               if (!old->guest || old->session != si || old->terminal ||
                   old->control != record->control ||
                   _mapping_donor(h, old, &actual) ||
                   actual.guest_cell != record->guest_cell ||
                   actual.native_cell != record->native_cell ||
                   memcmp(actual.guest_hash, record->guest_hash, 32) ||
                   memcmp(actual.native_hash, record->native_hash, 32) ||
                   memcmp(actual.mapping_hash, record->mapping_hash, 32) ||
                   memcmp(actual.response_guest_hash, record->response_guest_hash, 32) ||
                   memcmp(actual.response_native_hash, record->response_native_hash, 32))
                  return (EINVAL);
            }
         }
         if (ordinary != state->ordinary || control != state->control ||
             state->funding != funding ||
             (count && (funding == 255 ||
              e->control != state->old[funding].control)) ||
             (state->removed && e->donor >= 0) ||
             (!state->removed && count &&
              e->donor != state->old[funding].entry))
            return (EINVAL);
         for (unsigned k = 0; k < HFS9P_ENTRIES; k++) {
            Hfs9pEntry *old = &h->entries[k];
            if (!old->guest || old == e || old->session != si)
               continue;
            bool found = false;
            for (unsigned q = 0; q < count; q++)
               found |= state->old[q].entry == k;
            if (state->removed || !found)
               return (EINVAL);
         }
         if (staged)
            continue;
         j.bytes = e->native;
         j.length = e->native_total;
         if (hfs9p_json_parse(&j) || _fields(&j, 0, names, 7, v) ||
             hfs9p_json_u64(&j, v[0], &number) || number != e->native_cell)
            return (EINVAL);
         for (unsigned k = 1; k <= 5; k++) {
            if (k != 4 && !_same_token(e->guest, original[k], j.bytes,
                j.tokens[v[k]]))
               return (EINVAL);
         }
         const char *const delegation[] = {"guest_cell", "guest_op",
            "guest_reclaim", "poll_index", "version"};
         if (_fields(&j, v[6], delegation, 5, p) ||
             hfs9p_json_u64(&j, p[0], &number) || number != e->guest_cell ||
             !_same_token(e->guest, original[2], j.bytes, j.tokens[p[1]]) ||
             !_same_token(e->guest, original[4], j.bytes, j.tokens[p[2]]) ||
             hfs9p_json_u64(&j, p[3], &number) || number ||
             !hfs9p_json_equal(&j, p[4], "homelabfs-native-delegation/1"))
            return (EINVAL);
         if (funding == 255) {
            if (j.tokens[v[4]].type != 'n')
               return (EINVAL);
         } else if (_fields(&j, v[4], ack_keys, 2, a) ||
             hfs9p_json_u64(&j, a[0], &number) ||
             number != state->old[funding].native_cell ||
             _hex(&j, a[1], hash, 32, false) ||
             memcmp(hash, state->old[funding].response_native_hash, 32)) {
            return (EINVAL);
         }
         if (e->responded && _terminal_response(h, e))
            return (EINVAL);
      }
      if (terminals > 1 || (s->terminal_phase && terminals != 1) ||
          (!s->terminal_phase && (s->terminal_guest || s->terminal_native)) ||
          (s->terminal_phase != 0 && s->terminal_phase != 1 &&
           s->terminal_phase != 3))
         return (EINVAL);
   }
   return (0);
}

static int
_terminal_final(Hfs9p *h, Hfs9pEntry *e) {
   Hfs9pSession *s = &h->sessions[e->session];
   if (!e->terminal || !e->issued || !e->responded ||
       s->features != 32767u || s->terminal_guest != e->guest_cell ||
       s->terminal_native != e->native_cell ||
       (s->terminal_phase != 1 && s->terminal_phase != 3))
      return (EACCES);
   if (s->terminal_phase == 3)
      return (0);
   int error = _terminal_response(h, e);
   if (error)
      return (error);
   Hfs9pTerminal *state = e->terminal_state;
   uint8_t *ledger = NULL;
   size_t length = 0;
   /* Encode the complete post-cut without mutating a single precommit owner. */
   error = _snapshot_cut(h, &ledger, &length, e);
   if (!error)
      error = h->verifier.protect_cb(h->verifier.arg, ledger, length);
   free(ledger);
   if (error) {
      h->revoked = true;
      return (error);
   }
   for (unsigned k = 0; k < state->count; k++)
      _entry_clear(&h->entries[state->old[k].entry]);
   if (state->count) {
      h->ordinary = h->ordinary - state->ordinary + !e->control;
      h->control = h->control - state->control + e->control;
   }
   state->removed = true;
   e->donor = -1;
   s->terminal_phase = 3;
   return (error);
}

static bool
_seen(const Hfs9pSession *s, uint64_t cell) {
   for (unsigned i = 0; i < s->seen_count; i++) {
      if (cell >= s->seen[i].first && cell <= s->seen[i].last)
         return (true);
   }
   return (false);
}

static int
_mark_seen(Hfs9pSession *s, uint64_t cell) {
   unsigned i = 0;
   while (i < s->seen_count && s->seen[i].last < cell)
      i++;
   if (i < s->seen_count && s->seen[i].first <= cell)
      return (ESTALE);
   bool left = i && s->seen[i - 1].last + 1 == cell;
   bool right = i < s->seen_count && s->seen[i].first == cell + 1;
   if (left && right) {
      s->seen[i - 1].last = s->seen[i].last;
      memmove(&s->seen[i], &s->seen[i + 1],
          (s->seen_count - i - 1) * sizeof(s->seen[0]));
      s->seen_count--;
   } else if (left) {
      s->seen[i - 1].last = cell;
   } else if (right) {
      s->seen[i].first = cell;
   } else {
      if (s->seen_count == 256)
         return (ENOSPC);
      memmove(&s->seen[i + 1], &s->seen[i],
          (s->seen_count - i) * sizeof(s->seen[0]));
      s->seen[i] = (Hfs9pRange){cell, cell};
      s->seen_count++;
   }
   if (cell > s->high)
      s->high = cell;
   return (0);
}

/* New assemblies/candidates have not acquired their own endpoint credit.
 * A wait preparing a replacement poll keeps its existing control credit even
 * while !issued; its retained poll trail owns that credit across uncertainty.
 * Use this same classification for live admission, protection and recovery. */
typedef struct {
   unsigned ordinary;
   unsigned control;
   unsigned candidates;
   unsigned ordinary_candidates;
   unsigned per_session[HFS9P_SESSIONS];
   unsigned per_session_ordinary[HFS9P_SESSIONS];
} Hfs9pAccounting;

static bool
_owns_credit(const Hfs9pEntry *e) {
   if (e->terminal_state && e->terminal_state->count &&
       !e->terminal_state->removed)
      return (false);
   return (e->issued || (e->wait && e->polls));
}

static int
_account(const Hfs9p *h, Hfs9pAccounting *a) {
   memset(a, 0, sizeof(*a));
   if (h->session_count > HFS9P_SESSIONS)
      return (EINVAL);
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      const Hfs9pEntry *e = &h->entries[i];
      if (!e->guest)
         continue;
      if (e->session >= h->session_count ||
          (e->polls && (!e->wait || !e->complete || !e->control)) ||
          (e->native && !e->native_cell))
         return (EINVAL);
      if (_owns_credit(e)) {
         if (e->control)
            a->control++;
         else
            a->ordinary++;
      } else {
         a->candidates++;
         a->per_session[e->session]++;
         if (!e->assembly_control) {
            a->ordinary_candidates++;
            a->per_session_ordinary[e->session]++;
         }
      }
   }
   if (a->ordinary > 64 || a->control > 16 || a->candidates > 17 ||
       a->ordinary_candidates > 16)
      return (EINVAL);
   for (unsigned i = 0; i < h->session_count; i++) {
      if (a->per_session[i] > 16 || (h->sessions[i].minor &&
          a->per_session_ordinary[i] > 12))
         return (EINVAL);
   }
   return (0);
}

static int
_send(Hfs9p *h, unsigned bound_session, uint16_t tag,
    const uint8_t *frame, size_t length) {
   Hfs9pEntry *e = NULL;
   unsigned session;
   Hfs9pAccounting accounting;
   unsigned free_index = HFS9P_ENTRIES;
   uint64_t cell = _get(frame + 48, 8);
   uint32_t offset = (uint32_t)_get(frame + 20, 4);
   uint32_t total = (uint32_t)_get(frame + 16, 4);
   if (_frame(frame, length, 0, 1))
      return (EINVAL);
   for (session = 0; session < h->session_count; session++) {
      if (!memcmp(h->sessions[session].id, frame + 32, 16))
         break;
   }
   if (session == h->session_count || session != bound_session ||
       _get(frame + 10, 2) != h->sessions[session].minor)
      return (EACCES);
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      Hfs9pEntry *p = &h->entries[i];
      if (!p->guest) {
         free_index = i;
      } else {
         if (p->guest_cell == cell && p->session == session)
            e = p;
      }
   }
   if (!e) {
      bool terminal_input = false;
      Hfs9pSession *scope = &h->sessions[session];
      if (scope->features == 32767u && !offset &&
          length == HFS9P_HEADER + total && total <= HFS9P_TERMINAL_GUEST) {
         terminal_input = _terminal_input(h, session, frame, total);
      }
      if (h->sessions[session].terminal_phase)
         return (EACCES);
      for (unsigned i = 0; i < HFS9P_ENTRIES; i++)
         if (h->entries[i].guest && h->entries[i].session == session && h->entries[i].terminal)
            return (EACCES);
      if (_seen(&h->sessions[session], cell))
         return (ESTALE);
      if (_account(h, &accounting))
         return (EINVAL);
      if (free_index == HFS9P_ENTRIES || accounting.candidates >= 17 ||
          accounting.per_session[session] >= 16 ||
          (h->sessions[session].minor && tag < 65533 &&
          accounting.per_session_ordinary[session] >= 12) ||
          (accounting.ordinary_candidates >= 16 && tag < 65533) ||
          (!terminal_input && !_memory(h, total, tag >= 65533)))
         return (ENOSPC);
      e = &h->entries[free_index];
      if (terminal_input) {
         if (!scope->terminal_package || _terminal_usage(h, session))
            return (EACCES);
         e->terminal_funded = true;
         e->guest = scope->terminal_package->guest;
      } else {
         e->guest = calloc(1, total);
         if (!e->guest)
            return (ENOMEM);
      }
      e->total = total;
      e->assembly_control = tag >= 65533;
      e->guest_cell = cell;
      e->guest_watermark = h->sessions[session].high;
      e->session = (uint8_t)session;
      e->minor = h->sessions[session].minor;
      memcpy(e->guest_hash, frame + 56, 32);
      int error = _mark_seen(&h->sessions[session], cell);
      if (error) {
         _entry_clear(e);
         return (error);
      }
      e->chunks = (total + HFS9P_CHUNK - 1) / HFS9P_CHUNK;
   }
   if (e->total != total || memcmp(e->guest_hash, frame + 56, 32))
      return (EINVAL);
   uint32_t mask = UINT32_C(1) << (offset / HFS9P_CHUNK);
   if (e->received & mask) {
      if (memcmp(e->guest + offset, frame + HFS9P_HEADER,
          length - HFS9P_HEADER))
         return (EINVAL);
   } else {
      memcpy(e->guest + offset, frame + HFS9P_HEADER, length - HFS9P_HEADER);
      e->received |= mask;
   }
   uint32_t all = e->chunks == 32 ? UINT32_MAX :
       (UINT32_C(1) << e->chunks) - 1;
   if (e->received != all)
      return (0);
   if (!e->complete) {
      uint8_t digest[32];
      _hash(e->guest, total, digest);
      int error = memcmp(digest, e->guest_hash, 32) ? EINVAL : _command(h, e);
      if (error) {
         e->terminal = false; /* No complete candidate/fence was accepted. */
         return (error);
      }
      if ((tag == 65534 && !e->wait) ||
          (tag == 65533 && !e->control && !e->hello && !e->terminal) ||
          (e->wait && tag != 65534) ||
          (e->assembly_control && !e->control && !e->hello && !e->terminal)) {
         e->terminal = false;
         return (EINVAL);
      }
      e->complete = true;
   }
   int error = _forward(h, e);
   /* The current packet is already owned in the complete guest assembly.
    * Resource backpressure during native forwarding must retain COPIED, not
    * falsely label it a no-copy SEND/CAPACITY refusal. FETCH can retry native
    * progress using this same immutable binding. */
   return (error == EAGAIN || error == EWOULDBLOCK || error == ENOSPC ||
       error == ENOMEM ? 0 : error);
}

static int
_fetch(Hfs9p *h, unsigned session, uint64_t cell, uint32_t chunk, unsigned channel,
    uint8_t *out, size_t capacity, size_t *length) {
   Hfs9pEntry *e = NULL;
   /* FETCH is implicitly scoped by its enrolled device connection. */
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      if (h->entries[i].guest && h->entries[i].guest_cell == cell &&
          h->entries[i].session == session) {
         if (e)
            return (EACCES);
         e = &h->entries[i];
      }
   }
   if (!e)
      return (_seen(&h->sessions[session], cell) ? ESTALE : EAGAIN);
   if (!e->complete)
      return (EAGAIN);
   if (channel != (e->wait ? 3u : 2u))
      return (EINVAL);
   int error;
   if (e->wait) {
      error = _wait_step(h, e);
      if (!error && !e->final)
         error = EAGAIN;
   } else {
      error = _forward(h, e);
      if (!error)
         error = _response(h, e);
   }
   if (error)
      return (error);
   uint64_t offset = (uint64_t)chunk * HFS9P_CHUNK;
   if (offset >= e->response_total)
      return (EINVAL);
   uint32_t n = e->response_total - (uint32_t)offset;
   if (n > HFS9P_CHUNK)
      n = HFS9P_CHUNK;
   if (capacity < HFS9P_HEADER + n)
      return (ENOBUFS);
   _make_frame(out, e, (uint32_t)offset, true);
   memcpy(out + 32, h->sessions[e->session].id, 16);
   e->fetched |= UINT32_C(1) << chunk;
   *length = HFS9P_HEADER + n;
   return (0);
}

int
hfs9p_exchange(Hfs9pConnection *connection, const uint8_t *request, size_t length,
    uint8_t *reply, size_t capacity, size_t *reply_length) {
   int error;
   Hfs9p *h = connection ? connection->endpoint : NULL;
   if (reply_length)
      *reply_length = 0;
   if (!h || !request || !reply || !reply_length || length < 7 ||
       length > HFS9P_MSIZE || capacity < 10 ||
       _get(request, 4) != length)
      return (EINVAL);
   /* Version selection belongs to this authenticated device connection, not
    * the endpoint allocator. It changes no native/claim/session authority and
    * must remain usable while the endpoint polling worker owns its mutex. */
   if (request[4] == 100) {
      if (atomic_load(&h->sessions[connection->session].terminal_phase))
         return (EACCES);
      const char *version = connection->minor == 4 ? "9P2000.L.hfs1.4" :
          connection->minor == 3 ? "9P2000.L.hfs1.3" :
          connection->minor == 2 ? "9P2000.L.hfs1.2" : connection->minor ?
          "9P2000.L.hfs1.1" : "9P2000.L.hfs1";
      size_t n = strlen(version);
      bool expected = false;
      if (length != 13 + n || _get(request + 5, 2) != 65535 ||
          _get(request + 7, 4) != HFS9P_MSIZE ||
          _get(request + 11, 2) != n || memcmp(request + 13, version, n))
         return (EINVAL);
      if (capacity < length)
         return (ENOBUFS);
      if (!atomic_compare_exchange_strong(&connection->negotiated,
          &expected, true))
         return (EINVAL);
      memcpy(reply, request, length);
      reply[4] = 101;
      *reply_length = length;
      return (0);
   }
   if (!atomic_load(&connection->negotiated) || request[4] != 240 ||
       length < 21 || _get(request + 5, 2) == 65535)
      return (EINVAL);
   unsigned method = request[7], channel = request[8];
   uint64_t cell = _get(request + 9, 8);
   uint32_t chunk = (uint32_t)_get(request + 17, 4);
   if (!cell || cell > HFS9P_MAX_CELL || chunk >= 32)
      return (EINVAL);
   bool send = method == 1 && channel == 1 && length >= 21 + HFS9P_HEADER;
   bool fetch = method == 2 && (channel == 2 || channel == 3) && length == 21;
   if (send) {
      if (_frame(request + 21, length - 21, 0, 1) ||
          _get(request + 21 + 48, 8) != cell ||
          _get(request + 21 + 20, 4) / HFS9P_CHUNK != chunk ||
          memcmp(request + 21 + 32,
          h->sessions[connection->session].id, 16))
         return (EINVAL);
   } else if (!fetch) {
      return (EINVAL);
   }
   if (pthread_mutex_trylock(&h->mutex)) {
      /* This packet has not copied or changed any endpoint bytes. Refuse SEND
       * at packet granularity, including nonzero chunks; the consumer must
       * retry the identical packet without advancing its message/claim. FETCH
       * gets ordinary AGAIN. No valid contention becomes a zero-length reply. */
      reply[4] = 241;
      memcpy(reply + 5, request + 5, 2);
      reply[7] = send ? 4 : 2;
      *reply_length = send ? 10 : 8;
      if (send)
         _put(reply + 8, 3, 2); /* CAPACITY, not claim rejection. */
      _put(reply, *reply_length, 4);
      return (0);
   }
   error = _verify(h);
   if (error)
      goto done;
   size_t body_length = 0;
   if (h->recovery_paused) { error = EAGAIN; goto encode; }
   Hfs9pSession *bound = &h->sessions[connection->session];
   if (bound->terminal_phase && (bound->terminal_channel != connection ||
       cell != bound->terminal_guest || channel == 3)) {
      error = EACCES;
      goto encode;
   }
   /* Bind before SEND can protect a terminal fence; never restore this pointer. */
   if (!bound->terminal_phase)
      bound->terminal_channel = connection;
   if (method == 1 && channel == 1 && length >= 21 + HFS9P_HEADER &&
       _get(request + 21 + 48, 8) == cell &&
       _get(request + 21 + 20, 4) / HFS9P_CHUNK == chunk) {
      error = _send(h, connection->session, (uint16_t)_get(request + 5, 2),
          request + 21, length - 21);
   } else if (method == 2 && (channel == 2 || channel == 3) && length == 21) {
      error = _fetch(h, connection->session, cell, chunk, channel, reply + 8, capacity - 8,
          &body_length);
   } else {
      error = EINVAL;
   }
 encode:
   reply[4] = 241;
   memcpy(reply + 5, request + 5, 2);
   if (!error) {
      reply[7] = method == 1 ? 1 : 3;
      *reply_length = 8 + body_length;
   } else if (error == EAGAIN || error == EWOULDBLOCK) {
      reply[7] = 2;
      *reply_length = 8;
      error = 0;
   } else {
      reply[7] = 4;
      _put(reply + 8, error == EACCES || error == ESTALE ?
          (error == ESTALE ? 4 : 2) : (error == ENOSPC ? 3 : 1), 2);
      *reply_length = 10;
      error = 0;
   }
   _put(reply, *reply_length, 4);
 done:
   pthread_mutex_unlock(&h->mutex);
   return (error);
}

int
hfs9p_connection_new(Hfs9p *h, const uint8_t session[16],
    Hfs9pConnection **out) {
   if (!h || !session || !out)
      return (EINVAL);
   *out = NULL;
   if (pthread_mutex_trylock(&h->mutex))
      return (EAGAIN);
   unsigned index;
   for (index = 0; index < h->session_count; index++) {
      if (!memcmp(session, h->sessions[index].id, 16))
         break;
   }
   int error = index == h->session_count || h->sessions[index].terminal_phase ? EACCES : _verify(h);
   if (!error) {
      *out = calloc(1, sizeof(**out));
      if (!*out) {
         error = ENOMEM;
      } else {
         atomic_init(&(*out)->negotiated, false);
         (*out)->endpoint = h;
         (*out)->session = index;
         (*out)->minor = h->sessions[index].minor;
      }
   }
   pthread_mutex_unlock(&h->mutex);
   return (error);
}

void
hfs9p_connection_delete(Hfs9pConnection *connection) {
   /* Disconnect does not reset endpoint state, retire claims, acknowledge
    * snapshots, close private fds or assert a guest cache-domain exclusion. */
   free(connection);
}

/* Runs on an independent bounded control worker as well as nonblocking FETCH.
 * It never holds a native vnode while waiting for a recall, grant or deadline. */
static int
_wait_step(Hfs9p *h, Hfs9pEntry *e) {
   if (e->final)
      return (0);
   int error = _forward(h, e);
   if (!error)
      error = _response(h, e);
   if (error)
      return (error);
   Hfs9pJson j = {e->response_guest, e->response_total, h->tokens, 0};
   uint32_t payload, status, event;
   const char *const keys[] = { "event", "status" };
   uint32_t p[2];
   if (hfs9p_json_parse(&j) ||
       hfs9p_json_key(&j, 0, "payload", &payload) ||
       _fields(&j, payload, keys, 2, p))
      return (EINVAL);
   event = p[0];
   status = p[1];
   if (hfs9p_json_equal(&j, status, "EVENT")) {
      if (j.tokens[event].type != '{')
         return (EINVAL);
      e->final = true;
      return (0);
   }
   if (!hfs9p_json_equal(&j, status, "EMPTY") ||
       j.tokens[event].type != 'n')
      return (EINVAL);
   uint64_t now = _now();
   if (now >= e->deadline) {
      e->final = true;
      return (0);
   }
   if (e->poll_count >= 16 || now < e->next_poll)
      return (EAGAIN);
   Hfs9pPoll *poll = calloc(1, sizeof(*poll));
   if (!poll)
      return (ENOMEM);
   poll->request = e->native;
   poll->request_length = e->native_total;
   poll->response = e->response_native;
   /* Native and translated top-level cell have different decimal lengths. */
   /* response_native length is separately retained below, never strlen on wire. */
   poll->response_length = e->native_response_total;
   poll->cell = e->native_cell;
   poll->flags = e->flags;
   memcpy(poll->response_hash, e->response_native_hash, 32);
   poll->previous = e->polls;
   e->polls = poll;
   e->native = NULL;
   e->response_native = NULL;
   free(e->response_guest);
   e->response_guest = NULL;
   e->response_total = 0;
   e->native_response_total = 0;
   e->responded = false;
   e->issued = false;
   e->uncertain = false;
   e->final_sent = false;
   e->flags = 3;
   e->native_cell = 0;
   e->next_poll = now + 67;
   return (_forward(h, e));
}

static void *
_wait_worker_cb(void *arg) {
   Hfs9p *h = arg;
   for (;;) {
      if (!pthread_mutex_trylock(&h->mutex)) {
         if (h->stopping) {
            pthread_mutex_unlock(&h->mutex);
            break;
         }
         for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
            Hfs9pEntry *e = &h->entries[i];
            if (!h->recovery_paused && e->guest && e->complete && e->wait && !e->final)
               (void)_wait_step(h, e);
         }
         pthread_mutex_unlock(&h->mutex);
      }
      struct timespec delay = {0, 4000000};
      (void)nanosleep(&delay, NULL);
   }
   return (NULL);
}

#define HFS9P_LEDGER_MAX (UINT64_C(65) << 20)
typedef struct {
   uint8_t *bytes;
   size_t length;
   size_t capacity;
   size_t cursor;
   int error;
} Hfs9pBuffer;

static void
_pack_bytes(Hfs9pBuffer *b, const void *bytes, size_t length) {
   if (b->error)
      return;
   if (length > HFS9P_LEDGER_MAX - b->length) {
      b->error = E2BIG;
      return;
   }
   if (b->length + length > b->capacity) {
      size_t capacity = b->length + length;
      uint8_t *p = realloc(b->bytes, capacity);
      if (!p) {
         b->error = ENOMEM;
         return;
      }
      b->bytes = p;
      b->capacity = capacity;
   }
   if (length)
      memcpy(b->bytes + b->length, bytes, length);
   b->length += length;
}

static void
_pack(Hfs9pBuffer *b, uint64_t value, unsigned bytes) {
   uint8_t number[8];
   _put(number, value, bytes);
   _pack_bytes(b, number, bytes);
}

static void
_pack_blob(Hfs9pBuffer *b, const uint8_t *p, uint32_t n) {
   _pack(b, p ? n : 0, 4);
   if (p)
      _pack_bytes(b, p, n);
}

static bool
_cut_old_entry(const Hfs9pEntry *cut, unsigned index) {
   if (cut && cut->terminal_state) {
      for (unsigned k = 0; k < cut->terminal_state->count; k++) {
         if (cut->terminal_state->old[k].entry == index)
            return (true);
      }
   }
   return (false);
}

static int
_snapshot_cut(Hfs9p *h, uint8_t **out, size_t *length, Hfs9pEntry *cut) {
   Hfs9pAccounting accounting;
   if (_terminal_records(h) || _account(h, &accounting) || accounting.ordinary != h->ordinary ||
       accounting.control != h->control)
      return (EINVAL);
   Hfs9pBuffer b = {0};
   bool extension = false, terminal_format = false;
   for (unsigned i = 0; i < h->session_count; i++) {
      extension |= h->sessions[i].minor != 0;
      terminal_format |= h->sessions[i].features == 32767u;
   }
   _pack_bytes(&b, terminal_format ? "H9LEDG3\0" : extension ? "H9LEDG2\0" : "H9LEDG1\0", 8);
   _pack_bytes(&b, h->endpoint.session, 16);
   for (unsigned i = 0; i < 3; i++)
      _pack(&b, h->endpoint.node[i], 8);
   _pack(&b, h->high, 8);
   _pack(&b, (uint32_t)h->pending, 4);
   _pack(&b, h->session_count, 1);
   for (unsigned i = 0; i < h->session_count; i++) {
      Hfs9pSession *s = &h->sessions[i];
      _pack_bytes(&b, s->id, 16);
      _pack_blob(&b, s->context, (uint32_t)s->length);
      _pack(&b, s->high, 8);
      _pack(&b, s->seen_count, 2);
      for (unsigned k = 0; k < s->seen_count; k++) {
         _pack(&b, s->seen[k].first, 8);
         _pack(&b, s->seen[k].last, 8);
      }
      _pack(&b, s->hello, 1);
      if (extension)
         _pack(&b, s->minor, 1);
      if (terminal_format) {
         _pack(&b, s->features, 4);
         _pack_bytes(&b, s->controller, 16);
         uint32_t reserve = s->terminal_package ? sizeof(Hfs9pTerminalPackage) : 0;
         uint32_t used = _terminal_usage(h, i);
         _pack(&b, reserve, 4);
         _pack(&b, used, 4);
         _pack(&b, reserve - used, 4);
         _pack(&b, cut && cut->session == i ? 3 : s->terminal_phase, 1);
         _pack(&b, s->terminal_guest, 8);
         _pack(&b, s->terminal_native, 8);
      }
   }
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      Hfs9pEntry *e = &h->entries[i];
      bool present = e->guest && !_cut_old_entry(cut, i);
      _pack(&b, present, 1);
      if (!present)
         continue;
      _pack(&b, e->guest_cell, 8);
      _pack(&b, e->native_cell, 8);
      _pack(&b, e->chunks, 4);
      _pack(&b, e->received, 4);
      _pack(&b, e->fetched, 4);
      _pack(&b, e->flags, 2);
      _pack(&b, e->session, 1);
      _pack(&b, e->kind, 1);
      uint32_t bits = e->complete | e->issued << 1 | e->uncertain << 2 |
          e->responded << 3 | e->release << 4 | e->control << 5 |
          e->hello << 6 | e->wait << 7 | e->final << 8 |
          e->assembly_control << 9 | e->terminal << 10 | e->terminal_funded << 11 |
          e->final_sent << 12;
      _pack(&b, bits, 4);
      if (terminal_format) {
         _pack(&b, e->guest_watermark, 8);
         Hfs9pTerminal *state = e->terminal_state;
         _pack(&b, state != NULL, 1);
         if (state) {
            _pack(&b, state->count, 1);
            _pack(&b, state->funding, 1);
            _pack(&b, state->ordinary, 1);
            _pack(&b, state->control, 1);
            _pack(&b, cut == e || state->removed, 1);
            _pack_bytes(&b, state->settled_hash, 32);
            for (unsigned k = 0; k < state->count; k++) {
               Hfs9pTerminalOld *old = &state->old[k];
               _pack(&b, old->guest_cell, 8);
               _pack(&b, old->native_cell, 8);
               _pack(&b, old->entry, 1);
               _pack(&b, old->control, 1);
               _pack_bytes(&b, old->guest_hash, 32);
               _pack_bytes(&b, old->native_hash, 32);
               _pack_bytes(&b, old->mapping_hash, 32);
               _pack_bytes(&b, old->response_guest_hash, 32);
               _pack_bytes(&b, old->response_native_hash, 32);
            }
         }
      }
      _pack(&b, cut == e ? UINT32_MAX : (uint32_t)e->donor, 4);
      _pack(&b, e->deadline, 8);
      _pack(&b, e->next_poll, 8);
      _pack(&b, e->poll_count, 1);
      _pack_bytes(&b, e->guest_hash, 32);
      _pack_bytes(&b, e->native_hash, 32);
      _pack_bytes(&b, e->response_guest_hash, 32);
      _pack_bytes(&b, e->response_native_hash, 32);
      _pack_blob(&b, e->guest, e->total);
      _pack_blob(&b, e->native, e->native_total);
      _pack_blob(&b, e->response_guest, e->response_total);
      _pack_blob(&b, e->response_native, e->native_response_total);
      unsigned count = 0;
      for (Hfs9pPoll *p = e->polls; p; p = p->previous)
         count++;
      _pack(&b, count, 1);
      for (Hfs9pPoll *p = e->polls; p; p = p->previous) {
         _pack(&b, p->cell, 8);
         _pack(&b, p->flags, 2);
         _pack(&b, p->acknowledged, 1);
         _pack_bytes(&b, p->response_hash, 32);
         _pack_blob(&b, p->request, p->request_length);
         _pack_blob(&b, p->response, p->response_length);
      }
   }
   uint8_t digest[32];
   if (!b.error) {
      _hash(b.bytes, b.length, digest);
      _pack_bytes(&b, digest, 32);
   }
   if (b.error) {
      free(b.bytes);
      return (b.error);
   }
   *out = b.bytes;
   *length = b.length;
   return (0);
}

static int
_snapshot(Hfs9p *h, uint8_t **out, size_t *length) {
   return (_snapshot_cut(h, out, length, NULL));
}

int
hfs9p_snapshot(Hfs9p *h, uint8_t **out, size_t *length) {
   if (!h || !out || !length)
      return (EINVAL);
   *out = NULL;
   *length = 0;
   if (pthread_mutex_trylock(&h->mutex))
      return (EAGAIN);
   int error = _verify(h);
   if (!error)
      error = _snapshot(h, out, length);
   pthread_mutex_unlock(&h->mutex);
   return (error);
}

/* Trusted inherited-management operations only. Pause owns the entire endpoint,
 * including FETCH coverage and asynchronous polls; no guest wire selects it. */
int
hfs9p_pause_owned(Hfs9p *h, uint8_t **bytes, size_t *length) {
   if (!h || !bytes || !length) return (EINVAL);
   if (pthread_mutex_trylock(&h->mutex)) return (EAGAIN);
   int error = h->recovery_paused ? EALREADY : _verify(h);
   if (!error) {
      h->recovery_paused = true;
      error = _snapshot(h, bytes, length);
      if (!error) memcpy(h->recovery_hash, *bytes + *length - 32, 32);
   }
   pthread_mutex_unlock(&h->mutex);
   return (error);
}

int
hfs9p_resume_owned(Hfs9p *h, const uint8_t hash[32]) {
   if (!h || !hash) return (EINVAL);
   if (pthread_mutex_trylock(&h->mutex)) return (EAGAIN);
   int error = !h->recovery_paused || memcmp(hash, h->recovery_hash, 32) ? ESTALE : _verify(h);
   if (!error) h->recovery_paused = false;
   pthread_mutex_unlock(&h->mutex);
   return (error);
}

/* Decoding borrows immutable authenticated bytes; unlike the encoder, this
 * reader neither grows nor owns the underlying allocation. */
typedef struct {
   const uint8_t *bytes;
   size_t length;
   size_t cursor;
   int error;
} Hfs9pReader;

static const uint8_t *
_unpack_bytes(Hfs9pReader *b, size_t length) {
   if (b->error || length > b->length - b->cursor) {
      b->error = EINVAL;
      return (NULL);
   }
   const uint8_t *p = b->bytes + b->cursor;
   b->cursor += length;
   return (p);
}

static uint64_t
_unpack(Hfs9pReader *b, unsigned length) {
   const uint8_t *p = _unpack_bytes(b, length);
   return (p ? _get(p, length) : 0);
}

static uint8_t *
_unpack_blob_owned(Hfs9pReader *b, uint32_t *length, uint32_t max,
    uint8_t *owned) {
   *length = (uint32_t)_unpack(b, 4);
   if (*length > max) {
      b->error = EINVAL;
      return (NULL);
   }
   if (!*length)
      return (NULL);
   const uint8_t *p = _unpack_bytes(b, *length);
   if (!p)
      return (NULL);
   uint8_t *copy = owned ? owned : malloc(*length);
   if (!copy) {
      b->error = ENOMEM;
      return (NULL);
   }
   memcpy(copy, p, *length);
   return (copy);
}

static uint8_t *
_unpack_blob(Hfs9pReader *b, uint32_t *length, uint32_t max) {
   return (_unpack_blob_owned(b, length, max, NULL));
}

static void
_unpack_hash(Hfs9pReader *b, uint8_t out[32]) {
   const uint8_t *p = _unpack_bytes(b, 32);
   if (p)
      memcpy(out, p, 32);
}

/* The guest order floor is not persisted (e64), so it is rebuilt from what
 * the ledger proves. A restored original without a native number keeps the
 * floor below itself, so restored cells are still forwarded in guest order.
 * Without one, every seen cell is settled and the floor is the session's
 * highest seen cell. An unseen cell below the floor is no longer awaited;
 * if it arrives it relies on the runtime's bounded guest reorder window. */
static void
_order_restore(Hfs9p *h) {
   for (unsigned i = 0; i < h->session_count; i++)
      h->sessions[i].order_high = h->sessions[i].high;
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      Hfs9pEntry *e = &h->entries[i];
      if (!e->guest || e->issued || e->polls || e->session >= h->session_count)
         continue;
      Hfs9pSession *s = &h->sessions[e->session];
      if (e->guest_cell - 1 < s->order_high)
         s->order_high = e->guest_cell - 1;
   }
}

/* Pure ledger decoder. Its bytes become authority only through claim_cb's
 * protected-storage and real-host-ledger verification, never their checksum. */
static int
_restore_bytes(Hfs9p *h, const uint8_t *bytes, size_t length) {
   bool terminal_format = !memcmp(bytes, "H9LEDG3\0", 8);
   uint32_t used[HFS9P_SESSIONS] = {0}, remaining[HFS9P_SESSIONS] = {0};
   Hfs9pReader b = {.bytes = bytes, .length = length - 32, .cursor = 8};
   const uint8_t *session = _unpack_bytes(&b, 16);
   if (!session || memcmp(session, h->endpoint.session, 16))
      return (EINVAL);
   for (unsigned i = 0; i < 3; i++) {
      if (_unpack(&b, 8) != h->endpoint.node[i])
         return (EINVAL);
   }
   h->high = _unpack(&b, 8);
   h->pending = (int32_t)_unpack(&b, 4);
   h->session_count = (uint8_t)_unpack(&b, 1);
   if (h->session_count > HFS9P_SESSIONS || h->high > HFS9P_MAX_CELL ||
       h->pending < -1 || h->pending >= (int)HFS9P_ENTRIES)
      return (EINVAL);
   for (unsigned i = 0; i < h->session_count; i++) {
      Hfs9pSession *s = &h->sessions[i];
      const uint8_t *id = _unpack_bytes(&b, 16);
      if (!id)
         return (EINVAL);
      memcpy(s->id, id, 16);
      uint32_t n;
      s->context = _unpack_blob(&b, &n, 4096);
      s->length = n;
      s->high = _unpack(&b, 8);
      s->seen_count = (uint16_t)_unpack(&b, 2);
      if (s->seen_count > 256)
         return (EINVAL);
      for (unsigned k = 0; k < s->seen_count; k++) {
         s->seen[k].first = _unpack(&b, 8);
         s->seen[k].last = _unpack(&b, 8);
         if (!s->seen[k].first || s->seen[k].first > s->seen[k].last ||
             s->seen[k].last > s->high || (k &&
             s->seen[k - 1].last + 1 >= s->seen[k].first))
            return (EINVAL);
      }
      if ((s->seen_count && s->seen[s->seen_count - 1].last != s->high) ||
          (!s->seen_count && s->high))
         return (EINVAL);
      s->hello = _unpack(&b, 1) != 0;
      s->minor = (!memcmp(bytes, "H9LEDG2\0", 8) || terminal_format) ?
          (uint8_t)_unpack(&b, 1) : 0;
      if (s->minor > 4)
         return (EINVAL);
      s->features = s->minor == 4 ? 16383u : s->minor == 3 ? 8191u :
          s->minor == 2 ? 4095u : s->minor == 1 ? 2047u : 255u;
      if (terminal_format) {
         uint32_t features = (uint32_t)_unpack(&b, 4);
         if (features != s->features && !(s->minor == 4 && features == 32767u))
            return (EINVAL);
         s->features = features;
         const uint8_t *controller = _unpack_bytes(&b, 16);
         if (!controller)
            return (EINVAL);
         memcpy(s->controller, controller, 16);
         uint32_t reserve = (uint32_t)_unpack(&b, 4);
         used[i] = (uint32_t)_unpack(&b, 4);
         remaining[i] = (uint32_t)_unpack(&b, 4);
         uint32_t expected = features == 32767u ? sizeof(Hfs9pTerminalPackage) : 0;
         if (reserve != expected || used[i] > reserve ||
             remaining[i] != reserve - used[i])
            return (EINVAL);
         if (reserve) {
            if (!_memory(h, reserve, true))
               return (ENOSPC);
            s->terminal_package = calloc(1, reserve);
            if (!s->terminal_package)
               return (ENOMEM);
         }
         s->terminal_phase = (uint8_t)_unpack(&b, 1);
         s->terminal_guest = _unpack(&b, 8);
         s->terminal_native = _unpack(&b, 8);
         if (s->terminal_phase != 0 && s->terminal_phase != 1 && s->terminal_phase != 3)
            return (EINVAL);
      }
      Hfs9pJson j = {s->context, n, h->tokens, 0};
      uint8_t context_session[16];
      if (b.error || hfs9p_json_parse(&j) ||
          _context(&j, 0, context_session) ||
          memcmp(context_session, s->id, 16) || s->high > HFS9P_MAX_CELL)
         return (EINVAL);
      for (unsigned k = 0; k < i; k++) {
         if (!memcmp(s->id, h->sessions[k].id, 16))
            return (EINVAL);
      }
   }
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      Hfs9pEntry *e = &h->entries[i];
      if (!_unpack(&b, 1))
         continue;
      e->guest_cell = _unpack(&b, 8);
      e->native_cell = _unpack(&b, 8);
      e->chunks = (uint32_t)_unpack(&b, 4);
      e->received = (uint32_t)_unpack(&b, 4);
      e->fetched = (uint32_t)_unpack(&b, 4);
      e->flags = (uint16_t)_unpack(&b, 2);
      e->session = (uint8_t)_unpack(&b, 1);
      if (e->session >= h->session_count)
         return (EINVAL);
      e->minor = h->sessions[e->session].minor;
      e->kind = (uint8_t)_unpack(&b, 1);
      uint32_t bits = (uint32_t)_unpack(&b, 4);
      e->terminal = bits & 1024;
      e->terminal_funded = bits & 2048;
      e->final_sent = bits & 4096;
      Hfs9pTerminalPackage *package = h->sessions[e->session].terminal_package;
      if (e->terminal_funded && (!package || _terminal_usage(h, e->session)))
         return (EINVAL);
      if (terminal_format) {
         e->guest_watermark = _unpack(&b, 8);
         uint64_t present = _unpack(&b, 1);
         if (present > 1)
            return (EINVAL);
         if (present) {
            if (!e->terminal_funded || !package)
               return (EINVAL);
            Hfs9pTerminal *state = &package->state;
            memset(state, 0, sizeof(*state));
            e->terminal_state = state;
            state->count = (uint8_t)_unpack(&b, 1);
            state->funding = (uint8_t)_unpack(&b, 1);
            state->ordinary = (uint8_t)_unpack(&b, 1);
            state->control = (uint8_t)_unpack(&b, 1);
            uint64_t removed = _unpack(&b, 1);
            if (state->count > 80 || removed > 1)
               return (EINVAL);
            state->removed = removed;
            _unpack_hash(&b, state->settled_hash);
            for (unsigned k = 0; k < state->count; k++) {
               Hfs9pTerminalOld *old = &state->old[k];
               old->guest_cell = _unpack(&b, 8);
               old->native_cell = _unpack(&b, 8);
               old->entry = (uint8_t)_unpack(&b, 1);
               uint64_t control = _unpack(&b, 1);
               if (control > 1)
                  return (EINVAL);
               old->control = control;
               _unpack_hash(&b, old->guest_hash);
               _unpack_hash(&b, old->native_hash);
               _unpack_hash(&b, old->mapping_hash);
               _unpack_hash(&b, old->response_guest_hash);
               _unpack_hash(&b, old->response_native_hash);
            }
         }
      }
      e->complete = bits & 1;
      e->issued = bits & 2;
      e->uncertain = bits & 4;
      e->responded = bits & 8;
      e->release = bits & 16;
      e->control = bits & 32;
      e->hello = bits & 64;
      e->wait = bits & 128;
      e->final = bits & 256;
      e->assembly_control = bits & 512;
      e->donor = (int32_t)_unpack(&b, 4);
      e->deadline = _unpack(&b, 8);
      e->next_poll = _unpack(&b, 8);
      e->poll_count = (uint8_t)_unpack(&b, 1);
      _unpack_hash(&b, e->guest_hash);
      _unpack_hash(&b, e->native_hash);
      _unpack_hash(&b, e->response_guest_hash);
      _unpack_hash(&b, e->response_native_hash);
      if (e->terminal_funded) {
         e->guest = _unpack_blob_owned(&b, &e->total,
             sizeof(package->guest), package->guest);
         e->native = _unpack_blob_owned(&b, &e->native_total,
             sizeof(package->native), package->native);
         e->response_guest = _unpack_blob_owned(&b, &e->response_total,
             sizeof(package->response_guest), package->response_guest);
         e->response_native = _unpack_blob_owned(&b, &e->native_response_total,
             sizeof(package->response_native), package->response_native);
      } else {
         e->guest = _unpack_blob(&b, &e->total, HFS9P_BODY);
         e->native = _unpack_blob(&b, &e->native_total, HFS9P_BODY);
         e->response_guest = _unpack_blob(&b, &e->response_total, HFS9P_BODY);
         e->response_native = _unpack_blob(&b, &e->native_response_total,
             HFS9P_BODY);
      }
      unsigned count = (unsigned)_unpack(&b, 1);
      /* Bit 4096 is final_sent (w64), valid in every format but only on the
       * pending candidate: _forward_issued and _fail_back clear it. An
       * exporter from before w64 refuses a ledger that carries it. */
      if ((bits & 4096u && (int)i != h->pending) ||
          (bits & ~4096u) > (terminal_format ? 4095u : 1023u))
         return (EINVAL);
      if (count > 15 || e->poll_count > 16 ||
          e->session >= h->session_count || !e->guest_cell ||
          !_seen(&h->sessions[e->session], e->guest_cell) ||
          e->native_cell > h->high + 1 || e->donor < -1 ||
          e->donor >= (int)HFS9P_ENTRIES || !e->total ||
          e->chunks != (e->total + HFS9P_CHUNK - 1) / HFS9P_CHUNK ||
          e->flags > 3 || (e->flags & 1) != e->control)
         return (EINVAL);
      if (e->issued && (!e->native || e->native_cell == 0 ||
          e->native_cell > h->high))
         return (EINVAL);
      for (unsigned k = 0; k < i; k++) {
         Hfs9pEntry *prior = &h->entries[k];
         if (prior->guest &&
             ((e->native_cell && prior->native_cell == e->native_cell) ||
             (prior->session == e->session &&
              prior->guest_cell == e->guest_cell)))
            return (EINVAL);
      }
      Hfs9pPoll **tail = &e->polls;
      uint64_t previous = e->native_cell ? e->native_cell : h->high + 1;
      for (unsigned k = 0; k < count; k++) {
         Hfs9pPoll *p = calloc(1, sizeof(*p));
         if (!p)
            return (ENOMEM);
         *tail = p;
         tail = &p->previous;
         p->cell = _unpack(&b, 8);
         p->flags = (uint16_t)_unpack(&b, 2);
         p->acknowledged = _unpack(&b, 1) != 0;
         _unpack_hash(&b, p->response_hash);
         p->request = _unpack_blob(&b, &p->request_length, 16384);
         p->response = _unpack_blob(&b, &p->response_length, HFS9P_BODY);
         if (!p->cell || p->cell >= previous || p->flags > 3 ||
             !p->request || !p->response ||
             (!p->acknowledged && (k || e->issued || !e->wait)))
            return (EINVAL);
         uint8_t hash[32];
         _hash(p->response, p->response_length, hash);
         if (memcmp(hash, p->response_hash, 32) ||
             _body_version_tuple(h, p->request, p->request_length,
             e->minor, h->sessions[e->session].features, false) || _body_version_tuple(h, p->response,
             p->response_length, e->minor, h->sessions[e->session].features, true))
            return (EINVAL);
         previous = p->cell;
      }
      if (b.error)
         return (b.error);
      uint8_t hash[32];
      if (e->complete) {
         _hash(e->guest, e->total, hash);
         if (memcmp(hash, e->guest_hash, 32) ||
             _body_version_tuple(h, e->guest, e->total, e->minor, h->sessions[e->session].features, false))
            return (EINVAL);
      }
      if (e->native) {
         _hash(e->native, e->native_total, hash);
         if (memcmp(hash, e->native_hash, 32) ||
             _body_version_tuple(h, e->native, e->native_total, e->minor, h->sessions[e->session].features, false))
            return (EINVAL);
      }
      if (e->responded) {
         if (!e->response_guest || !e->response_native)
            return (EINVAL);
         _hash(e->response_guest, e->response_total, hash);
         if (memcmp(hash, e->response_guest_hash, 32) ||
             _body_version_tuple(h, e->response_guest, e->response_total,
             e->minor, h->sessions[e->session].features, true))
            return (EINVAL);
         _hash(e->response_native, e->native_response_total, hash);
         if (memcmp(hash, e->response_native_hash, 32) ||
             _body_version_tuple(h, e->response_native, e->native_response_total,
             e->minor, h->sessions[e->session].features, true))
            return (EINVAL);
      }
   }
   for (unsigned i = 0; i < h->session_count; i++) {
      uint32_t reserve = h->sessions[i].terminal_package ? sizeof(Hfs9pTerminalPackage) : 0;
      if (used[i] != _terminal_usage(h, i) ||
          used[i] > reserve || remaining[i] != reserve - used[i])
         return (EINVAL);
   }
   Hfs9pAccounting accounting;
   if (_account(h, &accounting))
      return (EINVAL);
   h->ordinary = accounting.ordinary;
   h->control = accounting.control;
   if (b.error || b.cursor != b.length || _terminal_records(h) ||
       !_memory(h, 0, true) ||
       (h->pending >= 0 && !h->entries[h->pending].native))
      return (EINVAL);
   _final_restore(h);
   _order_restore(h);
   return (0);
}

static void
_discard_recovery(Hfs9p *h) {
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++)
      _entry_clear(&h->entries[i]);
   /* Rejected decoded counts are untrusted even during error cleanup. */
   for (unsigned i = 0; i < h->session_count &&
       i < HFS9P_SESSIONS; i++)
      free(h->sessions[i].context);
   for (unsigned i = 0; i < h->session_count && i < HFS9P_SESSIONS; i++)
      free(h->sessions[i].terminal_package);
   free(h);
}

int
hfs9p_terminal_evidence_check(const Hfs9pEndpoint *endpoint,
    const uint8_t *bytes, size_t length) {
   if (!endpoint || !bytes || length < 64 ||
       length > HFS9P_LEDGER_MAX || memcmp(bytes, "H9LEDG3\0", 8))
      return (EINVAL);
   uint8_t hash[32];
   _hash(bytes, length - 32, hash);
   if (memcmp(hash, bytes + length - 32, 32))
      return (EINVAL);
   Hfs9p *h = calloc(1, sizeof(*h));
   if (!h)
      return (ENOMEM);
   h->endpoint = *endpoint;
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++)
      h->entries[i].donor = -1;
   int error = _restore_bytes(h, bytes, length);
   _discard_recovery(h);
   return (error);
}

int
hfs9p_restore(Hfs9p **out, const int fd[3], const Hfs9pEndpoint *endpoint,
    Hfs9pVerifier verifier, const uint8_t *bytes, size_t length) {
   (void)_body_version;
#ifndef __FreeBSD__
   (void)_restore_bytes;
   (void)_discard_recovery;
   (void)fd;
   (void)endpoint;
   (void)verifier;
   (void)bytes;
   (void)length;
   if (out)
      *out = NULL;
   return (ENOTSUP);
#else
   if (!out || !fd || !endpoint || !bytes || length < 64 ||
       length > HFS9P_LEDGER_MAX || !verifier.claim_cb ||
       !verifier.verify_cb || !verifier.protect_cb ||
       (!!verifier.begin_io_cb != !!verifier.end_io_cb) ||
       (memcmp(bytes, "H9LEDG1\0", 8) &&
       memcmp(bytes, "H9LEDG2\0", 8) && memcmp(bytes, "H9LEDG3\0", 8)))
      return (EINVAL);
   *out = NULL;
   uint8_t hash[32];
   _hash(bytes, length - 32, hash);
   if (memcmp(hash, bytes + length - 32, 32))
      return (EINVAL);
   Hfs9p *h = calloc(1, sizeof(*h));
   if (!h)
      return (ENOMEM);
   h->endpoint = *endpoint;
   h->verifier = verifier;
   memcpy(h->fd, fd, sizeof(h->fd));
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++)
      h->entries[i].donor = -1;
   int error = _native_fds(fd);
   if (!error)
      error = _restore_bytes(h, bytes, length);
   if (!error) {
      for (unsigned i = 0; i < h->session_count; i++)
         if (h->sessions[i].features == 32767u) error = ENOTSUP;
   }
   if (!error)
      error = _verify(h);
   if (!error)
      error = verifier.claim_cb(verifier.arg, endpoint, hash);
   if (!error)
      error = pthread_mutex_init(&h->mutex, NULL);
   if (!error)
      error = pthread_create(&h->worker, NULL, _wait_worker_cb, h);
   if (error) {
      _discard_recovery(h);
      return (error);
   }
   /* This only restores backend transport state. Host RESUME remains BLOCKED
    * until its real service/capability/claim/domain ledgers are reconciled. */
   *out = h;
   return (0);
#endif
}
