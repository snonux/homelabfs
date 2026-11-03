// SPDX-License-Identifier: GPL-2.0-only
/* Same-source guest and backend; only kernel/native I/O observations are
 * primitives. The IPC peer uses the real host Transport, Service and provider.
 */
int primitive_fstat(int, struct stat *);
#define fstat primitive_fstat
#define pread primitive_pread
#define pwrite primitive_pwrite
#include "tools/integration/lib9p/src/hfs9p.c"
#undef fstat
#undef pread
#undef pwrite
#include "hfs_policy.h"
#include "hfs_protocol.h"
#include "kernel_format.h"
#include "tools/integration/lib9p/src/hfs9p_ledger.h"
#include <assert.h>
unsigned int hfs_test_alloc_call, hfs_test_alloc_fail;
size_t hfs_test_last_allocation_size;
struct mutex {
   pthread_mutex_t native;
};
typedef int spinlock_t;
struct kref {
   unsigned refcount;
};
struct list_head {
   struct list_head *next, *prev;
};
struct user_namespace;
struct module;
struct cred;
struct file;
struct vm_area_struct;
struct workqueue_struct;
struct hfs_view;
struct hfs_session;
struct hfs_cap;
struct hfs_request;
struct hfs_view_stamp {
   uuid_t generation;
   u64 revision;
};
struct hfs_view_family {
   struct hfs_buf credentials;
};
struct hfs_view_ticket {
   bool control;
   struct hfs_session *session;
   struct hfs_view_family *family;
   u32 users;
};
struct p9_client {
   int unused;
};
#define HFS_CONTEXT_MAX 4096U
#define HFS_CAP_MAX 8192U
#define HZ 100UL
#define P9_NOTAG 65535
#define READ_ONCE(x) (x)
#define WRITE_ONCE(x, v) ((x) = (v))
#define time_after_eq(a, b) ((long)((a) - (b)) >= 0)
#define msecs_to_jiffies(ms) (((ms) * HZ + 999) / 1000)
#define kmalloc kvmalloc
#define WARN_ON_ONCE(x) ((x) ? (assert(!(x)), 1) : 0)
#define EXPORT_SYMBOL_GPL(x)
#define container_of(p, t, m) ((t *)((char *)(p) - offsetof(t, m)))
#define INIT_LIST_HEAD(h)                                                      \
   do {                                                                        \
      (h)->next = (h);                                                         \
      (h)->prev = (h);                                                         \
   } while (0)
#define list_empty(h) ((h)->next == (h))
#define list_for_each_entry(p, h, m)                                           \
   for (p = container_of((h)->next, __typeof__(*p), m); &p->m != (h);          \
        p = container_of(p->m.next, __typeof__(*p), m))
static void list_add_tail(struct list_head *p, struct list_head *h) {
   p->prev = h->prev;
   p->next = h;
   h->prev->next = p;
   h->prev = p;
}
static void list_del(struct list_head *p) {
   p->prev->next = p->next;
   p->next->prev = p->prev;
}
#define mutex_init(p) assert(!pthread_mutex_init(&(p)->native, NULL))
#define mutex_lock(p) assert(!pthread_mutex_lock(&(p)->native))
#define mutex_unlock(p) assert(!pthread_mutex_unlock(&(p)->native))
enum hfs_view_scope {
   HFS_SCOPE_NORMAL_PUBLIC = 1,
   HFS_SCOPE_CONTROLLED_LOCAL = 2
};
#include "types.inc"
static unsigned long jiffies;
static Hfs9p *backend;
static Hfs9pConnection *connection;
static bool lose_allocation = true, lose_gap = true;
static char active_op[32];
static unsigned allocations, calls;
static u8 allocation_frame[HFS_HEADER_SIZE + HFS_CHUNK_MAX];
static u32 allocation_frame_len;
static void usleep_range(unsigned int a, unsigned int b) {
   (void)a;
   (void)b;
   jiffies++;
   usleep(1000);
}
static void uuid_gen(uuid_t *id) {
   static u8 next = 90;
   memset(id, 0, sizeof(*id));
   id->b[15] = next++;
}
static int hfs_retire(struct hfs_session *s) {
   (void)s;
   return -EOPNOTSUPP;
}
static struct hfs_view *hfs_view_get(struct hfs_session *s) {
   (void)s;
   return NULL;
}
static void hfs_view_put(struct hfs_view *v) {
   (void)v;
}
static int hfs_view_snapshot(struct hfs_view *v, struct hfs_view_stamp *s) {
   (void)v;
   (void)s;
   return -EOPNOTSUPP;
}
static void hfs_mmap_progress(struct hfs_session *s) {
   (void)s;
}
/* Source/publication verification remains unavailable, as in production before
 * C6. Refusal lets this fixture observe actual CALL without minting authority.
 */
static int hfs_result_validate(struct hfs_session *s, struct hfs_json *j,
                               const char *op, const struct hfs_buf *c) {
   (void)s;
   (void)op;
   (void)c;
   assert(hfs_json_is(j, hfs_json_field(j, 0, "publication"), "null"));
   return -EOPNOTSUPP;
}
static int p9_client_hfs_exchange(struct p9_client *client, u16 tag, u8 method,
                                  u8 channel, u64 cell, u32 chunk,
                                  const void *frame, u32 frame_len,
                                  void *packet, u32 *len) {
   (void)client;
   uint8_t request[HFS9P_MSIZE], reply[HFS9P_MSIZE];
   size_t n = 0;
   _put(request, 21 + frame_len, 4);
   request[4] = 240;
   _put(request + 5, tag == 65535 ? 1 : tag, 2);
   request[7] = method;
   request[8] = channel;
   _put(request + 9, cell, 8);
   _put(request + 17, chunk, 4);
   if (frame_len)
      memcpy(request + 21, frame, frame_len);
   int error = hfs9p_exchange(connection, request, 21 + frame_len, reply,
                              sizeof(reply), &n);
   if (error) {
      fprintf(stderr, "backend exchange method=%u channel=%u cell=%llu error=%d\n",
              method, channel, (unsigned long long)cell, error);
      return -error;
   }
   assert(n >= 8 && *len >= n - 7);
   memcpy(packet, reply + 7, n - 7);
   if (method == 1 && *(u8 *)packet != 1) {
      fprintf(stderr,"backend SEND refusal minor=%u tag=%u cell=%llu packet=",
              backend->sessions[0].minor, tag, (unsigned long long)cell);
      for (size_t x = 7; x < n; x++) fprintf(stderr,"%02x",reply[x]);
      fprintf(stderr,"\n");
   }
   *len = n - 7;
   if (method == 1 && frame_len && *(u8 *)packet == 1) {
      Hfs9pJson j = {(const uint8_t *)frame + HFS_HEADER_SIZE,
                     frame_len - HFS_HEADER_SIZE, backend->tokens, 0};
      uint32_t op;
      /* All selected component commands fit one chunk; the actual framing
       * parser remains responsible for arbitrary split commands in backend
       * regressions. */
      assert(!hfs9p_json_parse(&j) && !hfs9p_json_key(&j, 0, "op", &op));
      Hfs9pToken t = j.tokens[op];
      assert(t.end - t.start < sizeof(active_op));
      memcpy(active_op, j.bytes + t.start + 1, t.end - t.start - 2);
      active_op[t.end - t.start - 2] = 0;
      if (!strcmp(active_op, "ALLOCATE")) {
         if (!allocation_frame_len) {
            memcpy(allocation_frame, frame, frame_len);
            allocation_frame_len = frame_len;
         } else {
            assert(allocation_frame_len == frame_len &&
                   !memcmp(allocation_frame, frame, frame_len));
         }
         allocations++;
      }
      if (!strcmp(active_op, "CALL"))
         calls++;
   }
   if (method == 2 && *(u8 *)packet == 3 &&
       ((!strcmp(active_op, "ALLOCATE") && lose_allocation) ||
        (!strcmp(active_op, "GAP_RESOLVE") && lose_gap))) {
      if (!strcmp(active_op, "ALLOCATE"))
         lose_allocation = false;
      else
         lose_gap = false;
      return -EIO; /* genuine complete host response was received, then lost */
   }
   return 0;
}
static bool uuid_is_null(const uuid_t *id) {
   static const u8 zero[16];
   return !memcmp(id->b, zero, 16);
}
int hfs_uuid(const struct hfs_json *, int, uuid_t *);
#include "decoder.inc"
#include "dirty-fixture.inc"
#include "production.inc"

static void emit_hex(const void *bytes, size_t n) {
   const uint8_t *p = bytes;
   for (size_t i = 0; i < n; i++)
      printf("%02x", p[i]);
}
static char *read_line(void) {
   char *p = NULL;
   size_t n = 0;
   ssize_t z = getline(&p, &n, stdin);
   assert(z > 0);
   if (p[z - 1] == '\n')
      p[z - 1] = 0;
   return p;
}
static size_t decode_hex(const char *p, void *out, size_t cap) {
   size_t n = strlen(p) / 2;
   assert(strlen(p) % 2 == 0 && n <= cap);
   uint8_t *b = out;
   for (size_t i = 0; i < n; i++) {
      int a = _hex_digit(p[i * 2]), c = _hex_digit(p[i * 2 + 1]);
      assert(a >= 0 && c >= 0);
      b[i] = a * 16 + c;
   }
   return n;
}
int primitive_fstat(int fd, struct stat *st) {
   memset(st, 0, sizeof(*st));
   st->st_mode = S_IFREG | 0600;
   st->st_ino = fd + 100;
   st->st_size = HFS9P_VSIZE;
   return 0;
}
ssize_t primitive_pwrite(int fd, const void *bytes, size_t n, off_t offset) {
   assert(fd == 10);
   printf("W %llu ", (unsigned long long)offset);
   emit_hex(bytes, n);
   puts("");
   fflush(stdout);
   char *line = read_line();
   ssize_t result = -1;
   if (!strncmp(line, "OK ", 3))
      result = strtoll(line + 3, NULL, 10);
   else
      errno = EIO;
   free(line);
   return result;
}
ssize_t primitive_pread(int fd, void *bytes, size_t n, off_t offset) {
   printf("R %d %llu %zu\n", fd, (unsigned long long)offset, n);
   fflush(stdout);
   char *line = read_line();
   ssize_t result = -1;
   if (!strncmp(line, "DATA ", 5))
      result = decode_hex(line + 5, bytes, n);
   else
      errno = EAGAIN;
   free(line);
   return result;
}
static int primitive_verify(void *a, const Hfs9pEndpoint *e, const int fd[3]) {
   (void)a;
   (void)e;
   (void)fd;
   return 0;
}
static int fixture_claim_cb(void *arg, const Hfs9pEndpoint *endpoint,
                            const uint8_t hash[32]) {
   /* Live native ownership remains a primitive; local protection is real. */
   return hfs9p_ledger_claim(arg, endpoint, hash);
}
static int fixture_protect_cb(void *arg, const uint8_t *bytes, size_t length) {
   return hfs9p_ledger_protect(arg, bytes, length);
}
static int primitive_reserve(struct hfs_session *s, u64 n, bool control,
                             void **out) {
   (void)s;
   (void)n;
   (void)control;
   *out = malloc(1);
   return *out ? 0 : -ENOMEM;
}
static void primitive_unreserve(struct hfs_session *s, void *p) {
   (void)s;
   free(p);
}
static void copy_buf(struct hfs_json *j, int obj, const char *name,
                     struct hfs_buf *out) {
   int t = hfs_json_field(j, obj, name);
   assert(t >= 0);
   assert(!hfs_buf_init(out, HFS_BODY_MAX));
   hfs_buf_token(out, j, t);
   assert(!out->error);
}
static void field_uuid(struct hfs_json *j, int obj, const char *name,
                       uuid_t *out) {
   assert(!hfs_policy_uuid(j, hfs_json_field(j, obj, name), out));
}
/* Execute the exact production DIRTY_RESERVE decode/pending-clear block.
 * Literal controller bytes and primitive physical state supply no authority.
 */
static void check_dirty_decoder(void) {
   struct hfs_json *original = NULL;
   assert(!hfs_json_parse(dirty_original, strlen(dirty_original), &original));
   int reservation = hfs_json_field(original, 0, "reservation");
   for (unsigned mode = 0; mode < ARRAY_SIZE(dirty_bad) + 3; mode++) {
      struct hfs_session session = {.bridge_minor = 3};
      struct hfs_request request = {.session = &session, .earmarked = true},
                         other = {};
      struct hfs_lease lease = {};
      struct hfs_cache_page entry = {.lease = &lease};
      struct hfs_cap description = {};
      struct hfs_dirty dirty = {.request = &request};
      struct hfs_json *reply = NULL;
      mutex_init(&session.lock);
      session.allocation_pending = mode == 1 ? &other : &request;
      copy_buf(original, reservation, "claim_number", &request.earmark_number);
      copy_buf(original, reservation, "credentials", &request.credentials);
      copy_buf(original, reservation, "scope", &request.original_context);
      struct hfs_buf cap = {};
      copy_buf(original, reservation, "description", &cap);
      assert(!hfs_json_parse(cap.data, cap.len, &description.json));
      hfs_buf_free(&cap);
      u32 n = 32;
      assert(!hfs_json_hex(original,
                           hfs_json_field(original, reservation, "lease_id"),
                           lease.id, &n));
      u64 offset;
      assert(!hfs_json_u64(
          original, hfs_json_field(original, reservation, "offset"), &offset));
      const char *raw = mode < 3 ? dirty_original : dirty_bad[mode - 3];
      assert(!hfs_json_parse(raw, strlen(raw), &reply));
      if (mode == 2)
         hfs_test_alloc_fail = hfs_test_alloc_call + 2;
      int error = production_dirty_decoder(&session, &dirty, &entry,
                                           &description, offset, reply);
      hfs_test_alloc_fail = 0;
      if (mode < 2) {
         assert(!error && dirty.reservation && !session.blocked);
         assert(session.allocation_pending == (mode == 1 ? &other : NULL));
      } else {
         assert(error == (mode == 2 ? -ENOMEM : -EPROTO));
         assert(!dirty.reservation && session.allocation_pending == &request);
      }
      assert(request.earmarked && !request.allocated && !request.claim.len);
      hfs_json_free(dirty.reservation);
      hfs_json_free(description.json);
      hfs_buf_free(&request.earmark_number);
      hfs_buf_free(&request.credentials);
      hfs_buf_free(&request.original_context);
      assert(!pthread_mutex_destroy(&session.lock.native));
   }
   hfs_json_free(original);
   fprintf(stderr,
           "PASS: actual typed reservation decoder exact binding, wrong owner, "
           "8 mutations and allocation failure retain/clear pending correctly; "
           "literal/physical primitives only\n");
}

int main(void) {
   const char *legacy = getenv("HFS_CONTROLLED_LEGACY");
   unsigned minor = legacy ? (unsigned)strtoul(legacy, NULL, 10) : 3;
   assert(minor <= 3);
   if (!legacy)
      check_dirty_decoder();
   char *line = read_line();
   struct hfs_json *fixture = NULL, *result = NULL;
   assert(!hfs_json_parse(line, strlen(line), &fixture));
   free(line);
   int call = hfs_json_field(fixture, 0, "call"),
       c = hfs_json_field(fixture, call, "credentials"),
       claim = hfs_json_field(fixture, call, "claim");
   struct hfs_session session = {
       .bridge_minor = minor, .next_claim = 1, .next_cell = 1};
   struct hfs_provider provider = {.reserve = primitive_reserve,
                                   .unreserve = primitive_unreserve};
   session.provider = &provider;
   mutex_init(&session.lock);
   mutex_init(&session.writer);
   mutex_init(&session.retirement);
   INIT_LIST_HEAD(&session.requests);
   INIT_LIST_HEAD(&session.uncertain);
   struct hfs_buf context = {};
   copy_buf(fixture, 0, "context", &context);
   memcpy(session.enrollment.context, context.data, context.len);
   session.enrollment.context_len = context.len;
   int scope = hfs_json_field(fixture, 0, "context");
   field_uuid(fixture, scope, "session", &session.enrollment.session);
   field_uuid(fixture, claim, "realm", &session.enrollment.realm);
   field_uuid(fixture, claim, "anchor", &session.enrollment.anchor);
   field_uuid(fixture, claim, "nonce", &session.enrollment.nonce);
   session.enrollment.subject_len = sizeof(session.enrollment.subject);
   assert(!hfs_json_hex(fixture, hfs_json_field(fixture, claim, "subject"),
                        session.enrollment.subject,
                        &session.enrollment.subject_len));
   struct hfs_cred cred = {.ngroups = 1};
   u64 number;
   assert(!hfs_json_u64(fixture, hfs_json_field(fixture, c, "fsuid"), &number));
   cred.uid = number;
   assert(!hfs_json_u64(fixture, hfs_json_field(fixture, c, "fsgid"), &number));
   cred.gid = number;
   assert(!hfs_json_u64(fixture,
                        hfs_json_field(fixture, c, "credential_revision"),
                        &cred.revision));
   int groups = hfs_json_field(fixture, c, "groups");
   assert(!hfs_json_u64(fixture, groups + 1, &number));
   cred.groups[0] = number;
   field_uuid(fixture, c, "process", &cred.process);
   field_uuid(fixture, c, "user_namespace", &cred.user_namespace);
   u32 hashlen = 32;
   assert(!hfs_json_hex(fixture, hfs_json_field(fixture, c, "idmap_hash"),
                        cred.idmap_hash, &hashlen));
   Hfs9pEndpoint endpoint = {{0}, {110, 111, 112}};
   int fds[3] = {10, 11, 12};
   uuid_t fuse;
   field_uuid(fixture, scope, "fuse_session", &fuse);
   memcpy(endpoint.session, fuse.b, 16);
   const char *ledger_path = getenv("HFS_CONTROLLED_BACKEND_LEDGER");
   assert(ledger_path);
   int ledger_directory = open(ledger_path, O_RDONLY | O_DIRECTORY);
   assert(ledger_directory >= 0);
   Hfs9pLedger *ledger = NULL;
   assert(!hfs9p_ledger_new(&ledger, ledger_directory, "transport"));
   Hfs9pVerifier verifier = {ledger, fixture_claim_cb, fixture_protect_cb,
                             primitive_verify, NULL, NULL};
   assert(!_construct(&backend, fds, &endpoint, verifier));
   int err;
   while (
       (err = hfs9p_add_session_protocol(backend, (const uint8_t *)context.data,
                                         context.len, minor)) == EAGAIN) {
      usleep(1000);
   }
   assert(!err);
   while ((err = hfs9p_connection_new(backend, session.enrollment.session.b,
                                      &connection)) == EAGAIN) {
      usleep(1000);
   }
   assert(!err);
   uint8_t version[64], vreply[64];
   size_t vlen = 0;
   char protocol_token[32];
   if (minor)
      snprintf(protocol_token, sizeof(protocol_token), "9P2000.L.hfs1.%u", minor);
   else
      snprintf(protocol_token, sizeof(protocol_token), "9P2000.L.hfs1");
   const char *token = protocol_token;
   size_t tn = strlen(token);
   _put(version, 13 + tn, 4);
   version[4] = 100;
   _put(version + 5, 65535, 2);
   _put(version + 7, 65536, 4);
   _put(version + 11, tn, 2);
   memcpy(version + 13, token, tn);
   assert(!hfs9p_exchange(connection, version, 13 + tn, vreply, sizeof(vreply),
                          &vlen));
   struct hfs_buf hello = {}, args = {};
   assert(!hfs_buf_init(&hello, 256));
   hfs_buf_literal(&hello, hfs_protocol_text(minor));
   assert(!hfs_control(&session, "HELLO", &hello, production_hello_reserved(&session), &result));
   hfs_json_free(result);
   result = NULL;
   assert(!hfs_buf_init(&args, 16));
   hfs_buf_literal(&args, "{}");
   if (legacy) {
      /* These fixture claims are explicitly issued by the real legacy provider;
       * legacy wire tuples have no ALLOCATE. Publication still refuses. */
      int error = hfs_call(&session, "ROOT", &args, &cred,
                           getenv("HFS_CONTROLLED_CALL_CONTROL") != NULL,
                           &result);
      fprintf(stderr, "LEGACY minor=%u ROOT err=%d calls=%u allocations=%u\n",
              minor, error, calls, allocations);
      assert(error == -EOPNOTSUPP && calls == 1 && !allocations);
      /* Corpus drain arguments carry no authority in this fixture. The real
       * host must receive the control CALL and refuse its unissued claim. */
      args.len = 0;
      hfs_buf_free(&args);
      assert(!hfs_buf_init(&args, sizeof(legacy_drain_args)));
      hfs_buf_literal(&args, legacy_drain_args);
      error = hfs_call(&session, "DRAIN_WRITE", &args, &cred, true, &result);
      fprintf(stderr, "LEGACY DRAIN_WRITE unissued claim err=%d calls=%u\n", error, calls);
      assert(error == -EACCES && calls == 2 && !allocations);
      fprintf(stderr, "PASS: legacy production HELLO/CALL/QUERY plus control DRAIN_WRITE unissued-claim refusal; publication refused\n");
      return 0;
   }
   assert(hfs_call(&session, "DRAIN_WRITE", &args, &cred, true, &result) ==
              -EOPNOTSUPP &&
          session.next_claim == 1 && !allocations);
   err = hfs_call(&session, "ROOT", &args, &cred, false, &result);
   fprintf(stderr, "first call err=%d allocations=%u calls=%u next=%llu\n", err,
           allocations, calls, session.next_claim);
   assert(err == -EIO);
   assert(allocations == 1 && !calls && session.next_claim == 2 &&
          session.allocation_pending && session.allocation_uncertain);
   struct hfs_request *request = session.allocation_pending, *dirty = NULL;
   assert(request->original_context.len == context.len &&
          !memcmp(request->original_context.data, context.data, context.len));
   struct hfs_view_family family = {};
   assert(!hfs_buf_init(&family.credentials, 16384));
   hfs_cred_write(&family.credentials, &cred);
   struct hfs_view_ticket ticket = {.session = &session, .family = &family};
   assert(hfs_request_earmark(&session, &cred, &ticket, &dirty) == -EAGAIN &&
          !dirty);
   cred.gid++; /* new credentials cannot replace the pending original */
   assert(hfs_call(&session, "ROOT", &args, &cred, true, &result) == -EAGAIN &&
          session.next_claim == 2 && !calls);
   cred.gid--;
   struct hfs_buf payload = {};
   assert(!hfs_buf_init(&payload, HFS_BODY_MAX));
   assert(!hfs_request_allocate(&session, request, &payload));
   assert(request->allocated && request->allocation_receipt &&
          !session.allocation_pending && !session.allocation_uncertain &&
          allocations == 2 && !calls);
   if (getenv("HFS_CONTROLLED_GAP")) {
      assert(hfs_request_resolve_gap(&session, request) == -EIO);
      assert(!hfs_request_resolve_gap(&session, request));
      assert(request->gap && !calls);
      assert(request->gap_response && request->gap_response->transported &&
             hfs_json_is(request->gap_response,
                         hfs_json_field(request->gap_response, 0, "op"),
                         "\"GAP_RESOLVE\""));
   } else {
      assert(!hfs_request_earmark(&session, &cred, &ticket, &dirty));
      assert(dirty->number == 2 && dirty->earmarked && !dirty->claim.len &&
             !dirty->allocated && session.next_claim == 3);
      assert(session.allocation_pending == dirty);
      assert(hfs_call(&session, "ROOT", &args, &cred, true, &result) ==
                 -EAGAIN &&
             session.next_claim == 3 && allocations == 2);
      assert(hfs_request_run(&session, request, "ROOT", &payload, &result) ==
             -EOPNOTSUPP);
      assert(calls == 1 && allocations == 2);
      assert(request->response && request->response->transported &&
             hfs_json_is(request->response,
                         hfs_json_field(request->response, 0, "verification"),
                         "null"));
      struct hfs_json *retained = request->response;
      u8 before[32], after[32];
      hfs_hash(retained->data, retained->len, before);
      assert(hfs_request_run(&session, request, "ROOT", &payload, &result) ==
             -EOPNOTSUPP);
      hfs_hash(request->response->data, request->response->len, after);
      assert(request->response == retained && !memcmp(before, after, 32) &&
             calls == 1 && allocations == 2 && session.next_claim == 3);
   }
   fprintf(stderr,
           "PASS: production guest pre-send exact allocation retry, no later "
           "numbering while uncertain, typed earmark ownership, real host "
           "forwarding; no source/native/provider authority\n");
   /* Retained uncertain requests intentionally live until process exit. */
   return 0;
}
