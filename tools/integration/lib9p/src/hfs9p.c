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

typedef struct {
   uint8_t id[16];
   uint8_t *context;
   size_t length;
   uint64_t high;
   Hfs9pRange seen[256];
   uint16_t seen_count;
   bool hello;
   uint8_t minor;
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
} Hfs9pEntry;

struct Hfs9p_ {
   pthread_mutex_t mutex;
   pthread_t worker;
   bool stopping;
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
       _get(p + 8, 2) != 1 || _get(p + 10, 2) > 1 ||
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

static bool
_memory(Hfs9p *h, size_t extra, bool control) {
   uint64_t total = sizeof(*h), ordinary = 0;
   for (unsigned i = 0; i < h->session_count; i++)
      total += h->sessions[i].length;
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      Hfs9pEntry *e = &h->entries[i];
      uint64_t bytes = (e->guest ? e->total : 0) +
          (e->native ? e->native_total : 0) +
          (e->response_guest ? e->response_total : 0) +
          (e->response_native ? e->native_response_total : 0);
      for (Hfs9pPoll *p = e->polls; p; p = p->previous)
         bytes += sizeof(*p) + p->request_length + p->response_length;
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
   free(e->guest);
   free(e->native);
   free(e->response_guest);
   free(e->response_native);
   memset(e, 0, sizeof(*e));
   e->donor = -1;
}

static int
_construct(Hfs9p **out, const int fd[3], const Hfs9pEndpoint *endpoint,
    Hfs9pVerifier verifier) {
   Hfs9p *h;
   if (!out || !fd || !endpoint || !verifier.verify_cb ||
       !verifier.claim_cb || !verifier.protect_cb ||
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
      cap_rights_init(&rights, CAP_FSTAT, CAP_FSTATFS, CAP_SEEK,
          i == 0 ? CAP_PWRITE : CAP_PREAD);
      if (cap_rights_limit(fd[i], &rights))
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
   for (unsigned i = 0; i < h->session_count; i++)
      free(h->sessions[i].context);
   for (unsigned i = 0; i < 3; i++)
      (void)close(h->fd[i]);
   pthread_mutex_unlock(&h->mutex);
   pthread_mutex_destroy(&h->mutex);
   free(h);
   return (0);
}

int
hfs9p_add_session_protocol(Hfs9p *h, const uint8_t *context, size_t length,
    uint16_t minor) {
   Hfs9pJson j;
   uint8_t id[16], fuse[16];
   uint32_t f;
   int error;
   if (!h || !context || length > 4096 || minor > 1)
      return (EINVAL);
   if (pthread_mutex_trylock(&h->mutex))
      return (EAGAIN);
   j = (Hfs9pJson){context, length, h->tokens, 0};
   error = hfs9p_json_parse(&j);
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
   if (!error) {
      Hfs9pSession *s = &h->sessions[h->session_count];
      s->context = malloc(length);
      if (!s->context) {
         error = ENOMEM;
      } else {
         memcpy(s->context, context, length);
         memcpy(s->id, id, 16);
         s->length = length;
         s->minor = (uint8_t)minor;
         h->session_count++;
      }
   }
   pthread_mutex_unlock(&h->mutex);
   return (error);
}

int
hfs9p_add_session(Hfs9p *h, const uint8_t *context, size_t length) {
   return (hfs9p_add_session_protocol(h, context, length, 0));
}

static const char *
_protocol(uint16_t minor) {
   return (minor ? "{\"body_schema\":2,\"bridge_major\":1,"
       "\"bridge_minor\":1,\"features\":2047,\"msize\":65536,"
       "\"normal_major\":3,\"normal_minor\":0}" :
       "{\"body_schema\":1,\"bridge_major\":1,"
       "\"bridge_minor\":0,\"features\":255,\"msize\":65536,"
       "\"normal_major\":3,\"normal_minor\":0}");
}

static int
_protocol_body(const Hfs9pJson *j, uint32_t token, uint16_t minor) {
   const char *expected = _protocol(minor);
   Hfs9pToken t = j->tokens[token];
   return (t.end - t.start != strlen(expected) ||
       memcmp(j->bytes + t.start, expected, strlen(expected)) ? EINVAL : 0);
}

/* Only an actual externally integrated supervisor may replace this refusal. */
__attribute__((weak)) int
hfs9p_controller_claim(const char *name, Hfs9pConnection **out) {
   (void)name;
   if (out)
      *out = NULL;
   return (ENOTSUP);
}

static int
_body_version(Hfs9p *h, const uint8_t *bytes, uint32_t length,
    uint16_t minor, bool response) {
   const char *const command[] = {"cell", "context", "op", "payload",
      "reclaim", "protocol"};
   const char *const reply[] = {"cell", "context", "op", "payload",
      "protocol"};
   uint32_t fields[6];
   Hfs9pJson j = {bytes, length, h->tokens, 0};
   unsigned count = (response ? 4 : 5) + (minor ? 1 : 0);
   return (hfs9p_json_parse(&j) || _fields(&j, 0,
       response ? reply : command, count, fields) ||
       (minor && _protocol_body(&j, fields[count - 1], minor)) ?
       EINVAL : 0);
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
          e->responded && e->fetched == all &&
          !memcmp(hash, e->response_guest_hash, 32)) {
         *donor = (int)i;
         return (0);
      }
   }
   return (EACCES);
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
_rewrite(const Hfs9pJson *j, Hfs9pReplacement *r, unsigned count,
    uint8_t **out, uint32_t *length) {
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
   uint8_t *p = malloc(n);
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
_command(Hfs9p *h, Hfs9pEntry *e) {
   const char *const names[] = { "cell", "context", "op", "payload",
      "reclaim", "protocol" };
   uint32_t v[6], a[3], in[2], claim_hash;
   const char *const call_keys[] = { "claim", "credentials", "intent" };
   const char *const intent_keys[] = { "args", "opcode" };
   Hfs9pJson j = { e->guest, e->total, h->tokens, 0 };
   uint64_t cell;
   uint8_t hash[32], expected[32];
   uint16_t minor = h->sessions[e->session].minor;
   if (e->minor != minor || hfs9p_json_parse(&j) ||
       _fields(&j, 0, names, minor ? 6 : 5, v) ||
       (minor && _protocol_body(&j, v[5], minor)) ||
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
      if (_protocol_body(&j, v[3], minor))
         return (EINVAL);
      e->control = false;
      e->hello = true;
   } else if (hfs9p_json_equal(&j, v[2], "CALL")) {
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
      e->control = hfs9p_json_equal(&j, in[1], "DRAIN_WRITE");
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
   if (!e->hello && !s->hello)
      return (EACCES);
   if (j.tokens[v[4]].type != 'n') {
      int error = _ack(h, &j, v[4], e->session, &e->donor);
      if (error)
         return (error);
      Hfs9pEntry *d = &h->entries[e->donor];
      if (d == e || (!e->release && e->control != d->control))
         return (EACCES);
      if (e->release)
         e->control = d->control;
   } else if (e->release) {
      return (EINVAL);
   }
   e->flags = (e->control ? 1 : 0) | (e->donor >= 0 ? 2 : 0);
   return (0);
}

static int
_native_body(Hfs9p *h, Hfs9pEntry *e) {
   uint32_t cell, reclaim, target, target_hash, payload, op;
   Hfs9pJson j = { e->guest, e->total, h->tokens, 0 };
   Hfs9pReplacement r[6];
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
   uint32_t length;
   if (!_memory(h, e->total + 256, e->control))
      return (ENOSPC);
   int error = _rewrite(&j, r, count, &e->native, &length);
   if (error)
      return (error);
   /* Native cell digit expansion may create one extra final chunk. */
   e->native_total = length;
   _hash(e->native, e->native_total, e->native_hash);
   return (0);
}

/* The sole framing writer never holds a vnode lock across semantic work. All
 * native callbacks are bounded copy/readiness operations. EAGAIN on a first
 * ordinary write consumes no native number, so controls may bypass it. Any
 * uncertain/partially copied candidate pins the writer to its exact bytes. */
static int
_forward(Hfs9p *h, Hfs9pEntry *e) {
   unsigned index = (unsigned)(e - h->entries);
   if (e->issued)
      return (0);
   if (h->pending >= 0 && (unsigned)h->pending != index)
      return (EAGAIN);
   if (!e->native) {
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
      if (error) {
         e->native_cell = 0;
         return (error);
      }
   }
   /* Protect exact candidate and donor snapshots before any possible native
    * effect. The persisted pending candidate is replayed unchanged on recovery. */
   h->pending = (int)index;
   uint8_t *ledger = NULL;
   size_t ledger_length = 0;
   int protection = _snapshot(h, &ledger, &ledger_length);
   if (!protection)
      protection = h->verifier.protect_cb(h->verifier.arg, ledger, ledger_length);
   free(ledger);
   if (protection)
      return (protection);
   for (uint32_t offset = 0; offset < e->native_total;
       offset += HFS9P_CHUNK) {
      uint32_t length = e->native_total - offset;
      if (length > HFS9P_CHUNK)
         length = HFS9P_CHUNK;
      _make_frame(h->scratch, e, offset, false);
      memcpy(h->scratch + 32, h->sessions[e->session].id, 16);
      int error = _verify(h);
      if (error)
         return (error);
      ssize_t written = pwrite(h->fd[0], h->scratch, HFS9P_HEADER + length,
          (off_t)(e->native_cell * HFS9P_STRIDE +
          (offset / HFS9P_CHUNK) * (HFS9P_HEADER + HFS9P_CHUNK)));
      if (written != (ssize_t)(HFS9P_HEADER + length)) {
         error = written < 0 ? errno : EIO;
         if (offset == 0 && !e->uncertain && e->donor < 0 && !e->polls &&
             written < 0 && (error == EAGAIN || error == EWOULDBLOCK)) {
            free(e->native);
            e->native = NULL;
            e->native_cell = 0;
            h->pending = -1;
            return (EAGAIN);
         }
         /* Even an error can lose a transport receipt. Keep exact candidate
          * bytes and writer ownership; never allocate its number to others. */
         h->pending = (int)index;
         e->uncertain = true;
         return (EAGAIN);
      }
      h->pending = (int)index;
      e->uncertain = true;
   }
   h->high = e->native_cell;
   h->pending = -1;
   e->issued = true;
   if (e->wait)
      e->poll_count++;
   if (e->polls) {
      e->polls->acknowledged = true;
   } else if (e->donor >= 0) {
      _entry_clear(&h->entries[e->donor]);
      e->donor = -1;
   } else if (e->control) {
      h->control++;
   } else {
      h->ordinary++;
   }
   return (0);
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
      ssize_t got = pread(h->fd[fd], bytes + done, n, (off_t)(offset + done));
      if (got < 0)
         return (errno);
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
      return (0);
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
            if (!_memory(h, total, e->control))
               return (ENOSPC);
            e->response_native = malloc(total);
            if (!e->response_native)
               return (ENOMEM);
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
      "protocol" };
   uint32_t v[5], op;
   uint64_t cell;
   if (hfs9p_json_parse(&response) ||
       _fields(&response, 0, fields, e->minor ? 5 : 4, v) ||
       (e->minor && _protocol_body(&response, v[4], e->minor)) ||
       hfs9p_json_u64(&response, v[0], &cell) || cell != e->native_cell)
      return (EINVAL);
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
   if (!_memory(h, total + 32, e->control))
      return (ENOSPC);
   int error = _rewrite(&response, r, e->wait ? 2 : 1, &e->response_guest,
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
   return (0);
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
      if (_seen(&h->sessions[session], cell))
         return (ESTALE);
      if (_account(h, &accounting))
         return (EINVAL);
      if (free_index == HFS9P_ENTRIES || accounting.candidates >= 17 ||
          accounting.per_session[session] >= 16 ||
          (h->sessions[session].minor && tag < 65533 &&
          accounting.per_session_ordinary[session] >= 12) ||
          (accounting.ordinary_candidates >= 16 && tag < 65533) ||
          !_memory(h, total, tag >= 65533))
         return (ENOSPC);
      e = &h->entries[free_index];
      e->guest = calloc(1, total);
      if (!e->guest)
         return (ENOMEM);
      e->total = total;
      e->assembly_control = tag >= 65533;
      e->guest_cell = cell;
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
      if (error)
         return (error);
      if ((tag == 65534 && !e->wait) ||
          (tag == 65533 && !e->control && !e->hello) ||
          (e->wait && tag != 65534) ||
          (e->assembly_control && !e->control && !e->hello))
         return (EINVAL);
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
      const char *version = connection->minor ?
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
   int error = index == h->session_count ? EACCES : _verify(h);
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
            if (e->guest && e->complete && e->wait && !e->final)
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

static int
_snapshot(Hfs9p *h, uint8_t **out, size_t *length) {
   Hfs9pAccounting accounting;
   if (_account(h, &accounting) || accounting.ordinary != h->ordinary ||
       accounting.control != h->control)
      return (EINVAL);
   Hfs9pBuffer b = {0};
   bool extension = false;
   for (unsigned i = 0; i < h->session_count; i++)
      extension |= h->sessions[i].minor != 0;
   _pack_bytes(&b, extension ? "H9LEDG2\0" : "H9LEDG1\0", 8);
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
   }
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      Hfs9pEntry *e = &h->entries[i];
      _pack(&b, e->guest != NULL, 1);
      if (!e->guest)
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
          e->assembly_control << 9;
      _pack(&b, bits, 4);
      _pack(&b, (uint32_t)e->donor, 4);
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

static const uint8_t *
_unpack_bytes(Hfs9pBuffer *b, size_t length) {
   if (b->error || length > b->length - b->cursor) {
      b->error = EINVAL;
      return (NULL);
   }
   const uint8_t *p = b->bytes + b->cursor;
   b->cursor += length;
   return (p);
}

static uint64_t
_unpack(Hfs9pBuffer *b, unsigned length) {
   const uint8_t *p = _unpack_bytes(b, length);
   return (p ? _get(p, length) : 0);
}

static uint8_t *
_unpack_blob(Hfs9pBuffer *b, uint32_t *length, uint32_t max) {
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
   uint8_t *copy = malloc(*length);
   if (!copy) {
      b->error = ENOMEM;
      return (NULL);
   }
   memcpy(copy, p, *length);
   return (copy);
}

static void
_unpack_hash(Hfs9pBuffer *b, uint8_t out[32]) {
   const uint8_t *p = _unpack_bytes(b, 32);
   if (p)
      memcpy(out, p, 32);
}

/* Pure ledger decoder. Its bytes become authority only through claim_cb's
 * protected-storage and real-host-ledger verification, never their checksum. */
static int
_restore_bytes(Hfs9p *h, const uint8_t *bytes, size_t length) {
   Hfs9pBuffer b = {(uint8_t *)bytes, length - 32, 0, 8, 0};
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
      s->minor = !memcmp(bytes, "H9LEDG2\0", 8) ?
          (uint8_t)_unpack(&b, 1) : 0;
      if (s->minor > 1)
         return (EINVAL);
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
      e->guest = _unpack_blob(&b, &e->total, HFS9P_BODY);
      e->native = _unpack_blob(&b, &e->native_total, HFS9P_BODY);
      e->response_guest = _unpack_blob(&b, &e->response_total, HFS9P_BODY);
      e->response_native = _unpack_blob(&b, &e->native_response_total,
          HFS9P_BODY);
      unsigned count = (unsigned)_unpack(&b, 1);
      if (count > 15 || e->poll_count > 16 || bits > 1023 ||
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
             _body_version(h, p->request, p->request_length,
             e->minor, false) || _body_version(h, p->response,
             p->response_length, e->minor, true))
            return (EINVAL);
         previous = p->cell;
      }
      if (b.error)
         return (b.error);
      uint8_t hash[32];
      if (e->complete) {
         _hash(e->guest, e->total, hash);
         if (memcmp(hash, e->guest_hash, 32) ||
             _body_version(h, e->guest, e->total, e->minor, false))
            return (EINVAL);
      }
      if (e->native) {
         _hash(e->native, e->native_total, hash);
         if (memcmp(hash, e->native_hash, 32) ||
             _body_version(h, e->native, e->native_total, e->minor, false))
            return (EINVAL);
      }
      if (e->responded) {
         if (!e->response_guest || !e->response_native)
            return (EINVAL);
         _hash(e->response_guest, e->response_total, hash);
         if (memcmp(hash, e->response_guest_hash, 32) ||
             _body_version(h, e->response_guest, e->response_total,
             e->minor, true))
            return (EINVAL);
         _hash(e->response_native, e->native_response_total, hash);
         if (memcmp(hash, e->response_native_hash, 32) ||
             _body_version(h, e->response_native, e->native_response_total,
             e->minor, true))
            return (EINVAL);
      }
   }
   Hfs9pAccounting accounting;
   if (_account(h, &accounting))
      return (EINVAL);
   h->ordinary = accounting.ordinary;
   h->control = accounting.control;
   if (b.error || b.cursor != b.length || !_memory(h, 0, true) ||
       (h->pending >= 0 && !h->entries[h->pending].native))
      return (EINVAL);
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
   free(h);
}

int
hfs9p_restore(Hfs9p **out, const int fd[3], const Hfs9pEndpoint *endpoint,
    Hfs9pVerifier verifier, const uint8_t *bytes, size_t length) {
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
       (memcmp(bytes, "H9LEDG1\0", 8) &&
       memcmp(bytes, "H9LEDG2\0", 8)))
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
