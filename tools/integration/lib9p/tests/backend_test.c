/* SPDX-License-Identifier: BSD-2-Clause
 * Component boundary tests with a native TRANSPORT fixture. No semantic
 * executor, real FUSE connection, native enrollment or publication is tested.
 */
#define fstat fixture_fstat
#define pread fixture_pread
#define pwrite fixture_pwrite
#include "../src/hfs9p.c"
#undef fstat
#undef pread
#undef pwrite

#include <assert.h>
#include <stdatomic.h>
static atomic_uint io_begins, io_ends;
static _Thread_local bool io_active;

static int
begin_io_cb(void *arg, const Hfs9pEndpoint *e, const int fd[3]) {
   (void)arg; (void)e; (void)fd;
   assert(!io_active); io_active = true;
   atomic_fetch_add(&io_begins, 1);
   return (0);
}

static void
end_io_cb(void *arg) {
   (void)arg; assert(io_active); io_active = false;
   atomic_fetch_add(&io_ends, 1);
}

#include "fixtures.h"
#include "../src/hfs9p_ledger.h"

typedef struct {
   uint8_t *request;
   uint8_t *reply;
   uint32_t total;
   uint32_t reply_total;
   uint16_t flags;
   uint16_t minor;
   uint32_t received;
   uint8_t hash[32];
   uint8_t session[16];
   bool ready;
} Fixture;

static Fixture fixtures[512];
static unsigned writes, protects, verifies;
static unsigned poll_count;
static unsigned event_after;
static bool lose_receipt;
static bool wrong_flags;
static bool eof;
static bool deny;
static bool fail_protect;
static bool pending_read;
/* Nonzero: every well-formed native write is refused with this errno, as the
 * runtime refuses a cell before admission (e64 refusal classification). */
static int refuse_errno;
static unsigned refused_writes;
/* Nonzero (p64): every ordinary non-donor chunk (flags 0) at or above byte
 * refuse_ordinary_from of its cell is refused with this errno. EAGAIN models
 * the runtime's credit refusal before it allocates the native number, unless
 * refuse_allocated is set (a full dispatch queue after allocation). Any
 * other errno models a refusal after allocation: the runtime keeps the cell
 * in its table, so the frame is recorded and a later different frame at
 * the same native number is refused EINVAL, as the runtime does. */
static int refuse_ordinary_errno;
static uint32_t refuse_ordinary_from;
static bool refuse_allocated;
/* Default preserves deliberate short-read coverage; pressure tests can select
 * bounded full chunks without hashing a whole large reply per seven bytes. */
static size_t fixture_read_limit = 7;
static bool hold_receipts;
static uint8_t last_accepted[HFS9P_HEADER + HFS9P_CHUNK];
static size_t last_accepted_length;
static off_t last_accepted_position;
static unsigned rejected_writes;

int
fixture_fstat(int fd, struct stat *st) {
   memset(st, 0, sizeof(*st));
   st->st_mode = S_IFREG | 0600;
   st->st_ino = (ino_t)(fd + 100);
   st->st_size = (off_t)HFS9P_VSIZE;
   return (0);
}

static int
verify_cb(void *arg, const Hfs9pEndpoint *endpoint, const int fd[3]) {
   (void)arg;
   (void)endpoint;
   (void)fd;
   verifies++;
   return (deny ? EACCES : 0);
}

static int
claim_cb(void *arg, const Hfs9pEndpoint *endpoint, const uint8_t hash[32]) {
   return (arg ? hfs9p_ledger_claim(arg, endpoint, hash) : 0);
}

static int
protect_cb(void *arg, const uint8_t *bytes, size_t length) {
   (void)arg;
   assert(length > 64 && (!memcmp(bytes, "H9LEDG1\0", 8) ||
       !memcmp(bytes, "H9LEDG2\0", 8)));
   uint8_t hash[32];
   _hash(bytes, length - 32, hash);
   assert(!memcmp(hash, bytes + length - 32, 32));
   protects++;
   return (fail_protect ? EIO :
       (arg ? hfs9p_ledger_protect(arg, bytes, length) : 0));
}

ssize_t
fixture_pwrite(int fd, const void *buffer, size_t length, off_t position) {
   const uint8_t *p = buffer;
   unsigned cell = (unsigned)((uint64_t)position / HFS9P_STRIDE);
   uint16_t flags = length >= HFS9P_HEADER ? (uint16_t)_get(p + 14, 2) : 0;
   if (fd != 10 || position < 0 || !cell || cell >= 512 || flags > 3 ||
       _frame(p, length, flags, 1) || _get(p + 48, 8) != cell) {
      rejected_writes++;
      errno = EINVAL;
      return (-1);
   }
   unsigned offset = (unsigned)_get(p + 20, 4);
   if ((uint64_t)position % HFS9P_STRIDE !=
       (offset / HFS9P_CHUNK) * (HFS9P_HEADER + HFS9P_CHUNK)) {
      rejected_writes++;
      errno = EINVAL;
      return (-1);
   }
   if (refuse_errno) {
      /* e64: the runtime refused this chunk before admission; no bytes kept. */
      refused_writes++;
      errno = refuse_errno;
      return (-1);
   }
   bool refuse_ordinary = refuse_ordinary_errno && !flags &&
       offset >= refuse_ordinary_from;
   if (refuse_ordinary && refuse_ordinary_errno == EAGAIN &&
       !refuse_allocated) {
      refused_writes++;
      errno = EAGAIN;
      return (-1);
   }
   Fixture *f = &fixtures[cell];
   if (f->request && (f->total != _get(p + 16, 4) || f->flags != flags ||
       memcmp(f->session, p + 32, 16) || memcmp(f->hash, p + 56, 32))) {
      rejected_writes++;
      errno = EINVAL;
      return (-1);
   }
   if (!f->request) {
      f->total = (uint32_t)_get(p + 16, 4);
      f->request = calloc(1, f->total);
      assert(f->request);
      f->flags = flags;
      f->minor = (uint16_t)_get(p + 10, 2);
      memcpy(f->session, p + 32, 16);
      memcpy(f->hash, p + 56, 32);
   }
   if (refuse_ordinary) {
      refused_writes++;
      errno = refuse_ordinary_errno;
      return (-1);
   }
   uint32_t bit = UINT32_C(1) << (offset / HFS9P_CHUNK);
   if ((f->received & bit) && memcmp(f->request + offset,
       p + HFS9P_HEADER, length - HFS9P_HEADER)) {
      rejected_writes++;
      errno = EINVAL;
      return (-1);
   }
   if (!(f->received & bit)) {
      memcpy(f->request + offset, p + HFS9P_HEADER, length - HFS9P_HEADER);
      f->received |= bit;
   }
   memcpy(last_accepted, p, length);
   last_accepted_length = length;
   last_accepted_position = position;
   writes++;
   unsigned chunks = (f->total + HFS9P_CHUNK - 1) / HFS9P_CHUNK;
   uint32_t all = chunks == 32 ? UINT32_MAX : (UINT32_C(1) << chunks) - 1;
   if (f->received == all && !f->ready) {
      uint8_t digest[32];
      _hash(f->request, f->total, digest);
      assert(!memcmp(digest, f->hash, 32));
      Hfs9pToken tokens[HFS9P_TOKENS];
      Hfs9pJson j = {f->request, f->total, tokens, 0};
      assert(!hfs9p_json_parse(&j));
      uint32_t context, op, payload;
      assert(!hfs9p_json_key(&j, 0, "context", &context));
      assert(!hfs9p_json_key(&j, 0, "op", &op));
      assert(!hfs9p_json_key(&j, 0, "payload", &payload));
      const char *result = "{\"status\":\"RELEASED\"}";
      char hello[256];
      if (hfs9p_json_equal(&j, op, "HELLO")) {
         Hfs9pToken t = tokens[payload];
         (void)snprintf(hello, sizeof(hello),
             "{\"selection\":%.*s,\"status\":\"ACCEPTED\"}",
             (int)(t.end - t.start), f->request + t.start);
         result = hello;
      } else if (hfs9p_json_equal(&j, op, "VIEW_POLL")) {
         poll_count++;
         result = event_after && poll_count >= event_after ?
             "{\"event\":{},\"status\":\"EVENT\"}" :
             "{\"event\":null,\"status\":\"EMPTY\"}";
      }
      Hfs9pToken c = tokens[context], o = tokens[op];
      f->reply = malloc(HFS9P_BODY);
      int n = snprintf((char *)f->reply, HFS9P_BODY,
          "{\"cell\":%u,\"context\":%.*s,\"op\":%.*s,\"payload\":%s}",
          cell, (int)(c.end - c.start), f->request + c.start,
          (int)(o.end - o.start), f->request + o.start, result);
      if (f->minor) {
         n = snprintf((char *)f->reply, HFS9P_BODY,
             "{\"cell\":%u,\"context\":%.*s,\"op\":%.*s,\"payload\":%s,"
             "\"protocol\":%s}", cell, (int)(c.end - c.start),
             f->request + c.start, (int)(o.end - o.start),
             f->request + o.start, result, _protocol(f->minor));
         if (f->minor >= 2) {
            assert(n > 0 && n + strlen(controlled_receipt) + 17 < HFS9P_BODY);
            n--;
            n += snprintf((char *)f->reply + n, HFS9P_BODY - n,
                ",\"verification\":%s}", controlled_receipt);
         }
      }
      assert(n > 0 && n < (int)HFS9P_BODY);
      f->reply_total = (uint32_t)n;
      f->ready = true;
   }
   if (lose_receipt || hold_receipts) {
      lose_receipt = false;
      errno = EIO;
      return (-1);
   }
   return ((ssize_t)length);
}

ssize_t
fixture_pread(int fd, void *buffer, size_t length, off_t position) {
   assert(io_active);
   assert(fd == 11 || fd == 12);
   if (pending_read) {
      errno = EAGAIN;
      return (-1);
   }
   if (eof)
      return (0);
   uint64_t cell = (uint64_t)position / HFS9P_STRIDE;
   assert(cell > 0 && cell < 512);
   Fixture *f = &fixtures[cell];
   if (!f->ready) {
      errno = EAGAIN;
      return (-1);
   }
   uint64_t within = (uint64_t)position % HFS9P_STRIDE;
   uint32_t chunk = (uint32_t)(within / (HFS9P_HEADER + HFS9P_CHUNK));
   size_t start = (size_t)(within % (HFS9P_HEADER + HFS9P_CHUNK));
   Hfs9pEntry e = {0};
   e.minor = (uint8_t)f->minor;
   e.response_guest = f->reply;
   e.response_total = f->reply_total;
   e.guest_cell = cell;
   e.kind = fd == 12 ? 3 : 2;
   _hash(f->reply, f->reply_total, e.response_guest_hash);
   uint8_t wire[HFS9P_HEADER + HFS9P_CHUNK];
   _make_frame(wire, &e, chunk * HFS9P_CHUNK, true);
   _put(wire + 14, wrong_flags ? f->flags ^ 1 : f->flags, 2);
   memcpy(wire + 32, f->session, 16);
   size_t size = HFS9P_HEADER + _get(wire + 24, 4);
   assert(start < size);
   if (length > fixture_read_limit)
      length = fixture_read_limit; /* Exercise actual pread accumulation. */
   if (length > size - start)
      length = size - start;
   memcpy(buffer, wire + start, length);
   return ((ssize_t)length);
}

static void
pause_retry(void) {
   struct timespec delay = {0, 1000000};
   (void)nanosleep(&delay, NULL);
}

static int
exchange_retry(Hfs9pConnection *c, const uint8_t *req, size_t length,
    uint8_t *out, size_t capacity, size_t *n) {
   int error;
   unsigned attempts = 0;
   for (;;) {
      error = hfs9p_exchange(c, req, length, out, capacity, n);
      bool capacity_retry = !error && length >= 21 && req[4] == 240 &&
          req[7] == 1 && *n == 10 && out[4] == 241 && out[7] == 4 &&
          _get(out + 8, 2) == 3;
      if (error != EAGAIN && !capacity_retry)
         return (error);
      assert(attempts++ < 5000);
      pause_retry();
   }
}

static int
add_session_retry(Hfs9p *h, const uint8_t *context, size_t length) {
   int error;
   while ((error = hfs9p_add_session(h, context, length)) == EAGAIN)
      pause_retry();
   return (error);
}

static int
connection_retry(Hfs9p *h, const uint8_t id[16], Hfs9pConnection **out) {
   int error;
   while ((error = hfs9p_connection_new(h, id, out)) == EAGAIN)
      pause_retry();
   return (error);
}

static Hfs9p *
new_endpoint_with_ledger(Hfs9pLedger *ledger) {
   int fd[3] = {10, 11, 12};
   Hfs9pEndpoint ep = {{0}, {110, 111, 112}};
   ep.session[15] = 5;
   Hfs9pVerifier v = {ledger, claim_cb, protect_cb, verify_cb, begin_io_cb, end_io_cb, NULL};
   Hfs9p *h = NULL;
   Hfs9pVerifier half = v;
   half.end_io_cb = NULL;
   assert(_construct(&h, fd, &ep, half) == EINVAL);
   assert(!_construct(&h, fd, &ep, v));
   return (h);
}

static Hfs9p *
new_endpoint(void) {
   return (new_endpoint_with_ledger(NULL));
}

static void
shutdown_endpoint(Hfs9p *h) {
   pthread_mutex_lock(&h->mutex);
   h->stopping = true;
   pthread_mutex_unlock(&h->mutex);
   assert(!pthread_join(h->worker, NULL));
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++)
      _entry_clear(&h->entries[i]);
   for (unsigned i = 0; i < h->session_count; i++) {
      free(h->sessions[i].context);
      free(h->sessions[i].terminal_package);
   }
   pthread_mutex_destroy(&h->mutex);
   free(h);
}

static void
reset_fixtures(void) {
   for (unsigned i = 0; i < 512; i++) {
      free(fixtures[i].request);
      free(fixtures[i].reply);
   }
   memset(fixtures, 0, sizeof(fixtures));
   writes = protects = verifies = poll_count = event_after = 0;
   lose_receipt = wrong_flags = eof = deny = fail_protect = false;
   pending_read = false;
   fixture_read_limit = 7;
   hold_receipts = false;
   rejected_writes = 0;
   refuse_errno = 0;
   refused_writes = 0;
   refuse_ordinary_errno = 0;
   refuse_ordinary_from = 0;
   refuse_allocated = false;
   last_accepted_length = 0;
}

static Hfs9pConnection *
connection_protocol(Hfs9p *h, const char *body, uint16_t minor) {
   Hfs9pToken tokens[HFS9P_TOKENS];
   Hfs9pJson j = {(const uint8_t *)body, strlen(body), tokens, 0};
   uint32_t context;
   uint8_t id[16];
   assert(!hfs9p_json_parse(&j));
   assert(!hfs9p_json_key(&j, 0, "context", &context));
   assert(!_context(&j, context, id));
   Hfs9pToken c = tokens[context];
   int added;
   if (!minor) {
      added = add_session_retry(h, j.bytes + c.start, c.end - c.start);
   } else {
      while ((added = hfs9p_add_session_protocol(h, j.bytes + c.start,
          c.end - c.start, minor)) == EAGAIN)
         pause_retry();
   }
   assert(!added);
   Hfs9pConnection *conn;
   assert(!connection_retry(h, id, &conn));
   uint8_t request[64] = {0}, reply[HFS9P_MSIZE];
   size_t n;
   const char *version = minor == 3 ? "9P2000.L.hfs1.3" :
       minor == 2 ? "9P2000.L.hfs1.2" : minor ? "9P2000.L.hfs1.1" : "9P2000.L.hfs1";
   _put(request, 13 + strlen(version), 4);
   request[4] = 100;
   _put(request + 5, 65535, 2);
   _put(request + 7, HFS9P_MSIZE, 4);
   _put(request + 11, strlen(version), 2);
   memcpy(request + 13, version, strlen(version));
   assert(!exchange_retry(conn, request, 13 + strlen(version), reply,
       sizeof(reply), &n));
   assert(reply[4] == 101);
   return (conn);
}

static Hfs9pConnection *
connection(Hfs9p *h, const char *body) {
   return (connection_protocol(h, body, 0));
}

static unsigned
send_body(Hfs9pConnection *c, const char *body, uint64_t cell) {
   Hfs9pEntry e = {0};
   e.minor = c->endpoint->sessions[c->session].minor;
   e.native = (uint8_t *)body;
   e.native_total = (uint32_t)strlen(body);
   e.native_cell = cell;
   _hash((const uint8_t *)body, strlen(body), e.native_hash);
   uint8_t request[HFS9P_MSIZE], response[HFS9P_MSIZE];
   size_t n;
   unsigned status = 0;
   for (uint32_t offset = 0; offset < e.native_total; offset += HFS9P_CHUNK) {
      uint32_t amount = e.native_total - offset;
      if (amount > HFS9P_CHUNK)
         amount = HFS9P_CHUNK;
      _make_frame(request + 21, &e, offset, false);
      memcpy(request + 21 + 32, c->endpoint->sessions[c->session].id, 16);
      _put(request, 21 + HFS9P_HEADER + amount, 4);
      request[4] = 240;
      _put(request + 5, strstr(body, "\"op\":\"VIEW_WAIT\"") ? 65534 : 1, 2);
      request[7] = 1;
      request[8] = 1;
      _put(request + 9, cell, 8);
      _put(request + 17, offset / HFS9P_CHUNK, 4);
      assert(!exchange_retry(c, request, 21 + HFS9P_HEADER + amount,
          response, sizeof(response), &n));
      status = response[7];
   }
   return (status);
}

static unsigned
fetch(Hfs9pConnection *c, uint64_t cell, unsigned channel) {
   uint8_t request[21] = {0}, response[HFS9P_MSIZE];
   size_t n;
   _put(request, 21, 4);
   request[4] = 240;
   _put(request + 5, 1, 2);
   request[7] = 2;
   request[8] = (uint8_t)channel;
   _put(request + 9, cell, 8);
   unsigned attempts = 0;
   for (;;) {
      assert(!exchange_retry(c, request, sizeof(request), response,
          sizeof(response), &n));
      if (channel != 2 || response[7] != 2)
         break;
      assert(attempts++ < 5000);
      pause_retry();
   }
   if (response[7] == 3) {
      assert(!_frame(response + 8, n - 8, 0, (uint16_t)channel));
      assert(_get(response + 8 + 48, 8) == cell);
   }
   return (response[7]);
}

static Hfs9pEntry *
entry(Hfs9p *h, uint64_t cell) {
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      if (h->entries[i].guest && h->entries[i].guest_cell == cell)
         return (&h->entries[i]);
   }
   return (NULL);
}

/* One FETCH on the ordinary result channel; returns the reply kind. */
static unsigned
fetch_once(Hfs9pConnection *c, uint64_t cell) {
   uint8_t request[21] = {0}, response[HFS9P_MSIZE];
   size_t n;
   _put(request, 21, 4);
   request[4] = 240;
   _put(request + 5, 1, 2);
   request[7] = 2;
   request[8] = 2;
   _put(request + 9, cell, 8);
   assert(!exchange_retry(c, request, sizeof(request), response,
       sizeof(response), &n));
   return (response[7]);
}

static Hfs9pEntry *
session_entry(Hfs9p *h, unsigned session, uint64_t cell) {
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      Hfs9pEntry *e = &h->entries[i];
      if (e->guest && e->session == session && e->guest_cell == cell)
         return (e);
   }
   return (NULL);
}

/* Ends every armed or future guest-order hold (e64) as if it had run out. */
static void
expire_order(Hfs9p *h) {
   pthread_mutex_lock(&h->mutex);
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      Hfs9pEntry *e = &h->entries[i];
      if (e->guest && !e->issued)
         e->order_deadline = _now() - 1;
   }
   pthread_mutex_unlock(&h->mutex);
}

static void
parser_tests(void) {
   assert(_fuse_type("fusefs"));
   assert(_fuse_type("fusefs.homelabf"));
   assert(_fuse_type("fusefs.t"));
   assert(!_fuse_type(NULL));
   assert(!_fuse_type(""));
   assert(!_fuse_type("fusefs."));
   assert(!_fuse_type("fusefsXYZ"));
   assert(!_fuse_type("fusefsX.homelabf"));
   assert(!_fuse_type("nfs"));
   assert(!_fuse_type("zfs"));
   static const char *bad[] = {"{\"a\":1,\"a\":2}", "{\"b\":1,\"a\":2}",
      "[01]", "[-0]", "[1.0]", "[1e1]", "[18446744073709551616]",
      "[-9223372036854775809]", "[\"\\u0041\"]", "{} ", "[true,]"};
   Hfs9pToken tokens[HFS9P_TOKENS];
   for (unsigned i = 0; i < sizeof(bad) / sizeof(bad[0]); i++) {
      Hfs9pJson j = {(const uint8_t *)bad[i], strlen(bad[i]), tokens, 0};
      assert(hfs9p_json_parse(&j));
   }
}

static void
core_tests(void) {
   Hfs9p *h = new_endpoint();
   Hfs9pConnection *c = connection(h, golden_hello);
   assert(send_body(c, golden_hello, 1) == 1);
   assert(fetch(c, 1, 2) == 3);
   assert(h->sessions[0].hello);
   assert(send_body(c, golden_create, 2) == 1);
   assert(fetch(c, 2, 2) == 3);
   Hfs9pEntry *e = entry(h, 2);
   assert(e && e->flags == 0);
   unsigned before = writes;
   assert(send_body(c, golden_create, 2) == 1);
   assert(writes == before); /* No second admission or changed request. */
   wrong_flags = true;
   Hfs9pEntry *hello = entry(h, 1);
   assert(hello);
   hello->responded = false;
   assert(fetch(c, 1, 2) == 4);
   wrong_flags = false;
   hello->responded = true;
   uint8_t *snapshot;
   size_t length;
   assert(!hfs9p_snapshot(h, &snapshot, &length));
   Hfs9p restored = {0};
   restored.endpoint = h->endpoint;
   assert(!_restore_bytes(&restored, snapshot, length));
   assert(restored.high == h->high && restored.ordinary == 2);
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++)
      _entry_clear(&restored.entries[i]);
   for (unsigned i = 0; i < restored.session_count; i++)
      free(restored.sessions[i].context);
   Hfs9p *malformed = calloc(1, sizeof(*malformed));
   assert(malformed);
   malformed->endpoint = h->endpoint;
   assert(length > 61);
   snapshot[60] = HFS9P_SESSIONS + 1;
   _hash(snapshot, length - 32, snapshot + length - 32);
   assert(_restore_bytes(malformed, snapshot, length) == EINVAL);
   assert(malformed->session_count == HFS9P_SESSIONS + 1);
   _discard_recovery(malformed); /* Exercise the actual failure cleanup. */
   free(snapshot);
   assert(protects && verifies > writes);
   hfs9p_connection_delete(c);
   shutdown_endpoint(h);
   reset_fixtures();
}

static char *
wait_body(Hfs9p *h, uint64_t cell) {
   Hfs9pToken tokens[HFS9P_TOKENS];
   Hfs9pJson j = {(const uint8_t *)golden_create, strlen(golden_create), tokens, 0};
   uint32_t payload, intent, args, parent;
   assert(!hfs9p_json_parse(&j));
   assert(!hfs9p_json_key(&j, 0, "payload", &payload));
   assert(!hfs9p_json_key(&j, payload, "intent", &intent));
   assert(!hfs9p_json_key(&j, intent, "args", &args));
   assert(!hfs9p_json_key(&j, args, "parent", &parent));
   Hfs9pToken p = tokens[parent];
   char *cap = malloc(p.end - p.start + 16);
   memcpy(cap, j.bytes + p.start, p.end - p.start);
   cap[p.end - p.start] = 0;
   char *kind = strstr(cap, "\"kind\":\"ROOT\"");
   assert(kind);
   size_t suffix = strlen(kind + 13);
   memmove(kind + 15, kind + 13, suffix + 1);
   memcpy(kind, "\"kind\":\"DOMAIN\"", 15);
   char *body = malloc(16384);
   (void)snprintf(body, 16384,
       "{\"cell\":%llu,\"context\":%.*s,\"op\":\"VIEW_WAIT\","
       "\"payload\":{\"after_sequence\":0,\"domain\":%s},\"reclaim\":null}",
       (unsigned long long)cell, (int)h->sessions[0].length,
       h->sessions[0].context, cap);
   free(cap);
   return (body);
}

static void
wait_tests(void) {
   Hfs9p *h = new_endpoint();
   Hfs9pConnection *c = connection(h, golden_hello);
   assert(send_body(c, golden_hello, 1) == 1);
   assert(fetch(c, 1, 2) == 3);
   char *body = wait_body(h, 2);
   event_after = 3;
   assert(send_body(c, body, 2) == 1);
   unsigned status = 2;
   for (unsigned i = 0; i < 300 && status == 2; i++) {
      status = fetch(c, 2, 3);
      struct timespec delay = {0, 4000000};
      (void)nanosleep(&delay, NULL);
   }
   assert(status == 3);
   Hfs9pEntry *e = entry(h, 2);
   assert(e && e->wait && e->final && e->poll_count == 3);
   assert(h->control == 1 && e->polls && e->polls->acknowledged);
   unsigned count = poll_count;
   assert(fetch(c, 2, 3) == 3 && poll_count == count);
   assert(fetch(c, 2, 2) == 4); /* Guest recall uses kind/channel3. */
   free(body);
   hfs9p_connection_delete(c);
   shutdown_endpoint(h);
   reset_fixtures();
}

static void
routing_cases(void) {
   const char *bodies[] = {golden_mode, large_write, large_drain,
      bad_umask, bad_mode, missing_max_end, bad_max_end};
   int expected[] = {0, 0, 0, EINVAL, EINVAL, EINVAL, EINVAL};
   for (unsigned i = 0; i < sizeof(bodies) / sizeof(bodies[0]); i++) {
      Hfs9p *h = new_endpoint();
      Hfs9pConnection *c = connection(h, bodies[i]);
      h->sessions[0].hello = true; /* Routing test, not HELLO/runtime proof. */
      Hfs9pEntry e = {0};
      e.guest = (uint8_t *)bodies[i];
      e.total = (uint32_t)strlen(bodies[i]);
      Hfs9pJson j = {e.guest, e.total, h->tokens, 0};
      uint32_t cell;
      assert(!hfs9p_json_parse(&j));
      assert(!hfs9p_json_key(&j, 0, "cell", &cell));
      assert(!hfs9p_json_u64(&j, cell, &e.guest_cell));
      assert(_command(h, &e) == expected[i]);
      if (!expected[i])
         assert(e.flags == (i == 2 ? 1 : 0));
      assert(writes == 0);
      hfs9p_connection_delete(c);
      shutdown_endpoint(h);
      reset_fixtures();
   }
}

static char *
rewrite_body(const char *body, uint64_t number, const char *reclaim) {
   Hfs9pToken tokens[HFS9P_TOKENS];
   Hfs9pJson j = {(const uint8_t *)body, strlen(body), tokens, 0};
   uint32_t cell, ack;
   assert(!hfs9p_json_parse(&j));
   assert(!hfs9p_json_key(&j, 0, "cell", &cell));
   assert(!hfs9p_json_key(&j, 0, "reclaim", &ack));
   char id[24];
   int n = snprintf(id, sizeof(id), "%llu", (unsigned long long)number);
   Hfs9pReplacement r[2] = {
      {tokens[cell].start, tokens[cell].end, (uint8_t *)id, (size_t)n},
      {tokens[ack].start, tokens[ack].end, (const uint8_t *)reclaim,
         reclaim ? strlen(reclaim) : 0}
   };
   uint8_t *out;
   uint32_t length;
   assert(!_rewrite(&j, r, reclaim ? 2 : 1, &out, &length));
   char *terminated = realloc(out, length + 1);
   assert(terminated);
   terminated[length] = 0;
   return (terminated);
}

static void
ack_string(Hfs9pEntry *e, char ack[128]) {
   char hash[65];
   for (unsigned i = 0; i < 32; i++)
      (void)snprintf(hash + i * 2, 3, "%02x", e->response_guest_hash[i]);
   (void)snprintf(ack, 128, "{\"cell\":%llu,\"response_hash\":\"%s\"}",
       (unsigned long long)e->guest_cell, hash);
}

static void
capacity_tests(void) {
   Hfs9p *h = new_endpoint();
   Hfs9pConnection *c = connection(h, golden_hello);
   assert(send_body(c, golden_hello, 1) == 1);
   assert(fetch(c, 1, 2) == 3);
   for (uint64_t i = 2; i <= 64; i++) {
      char *body = rewrite_body(golden_create, i, NULL);
      assert(send_body(c, body, i) == 1);
      free(body);
   }
   /* Use actual QUERY envelopes, not synthetic credit counters. */
   Hfs9pToken tokens[HFS9P_TOKENS];
   Hfs9pJson j = {(const uint8_t *)golden_create, strlen(golden_create), tokens, 0};
   uint32_t payload, claim;
   assert(!hfs9p_json_parse(&j));
   assert(!hfs9p_json_key(&j, 0, "payload", &payload));
   assert(!hfs9p_json_key(&j, payload, "claim", &claim));
   Hfs9pToken t = tokens[claim];
   char query[16384];
   for (uint64_t i = 65; i <= 80; i++) {
      (void)snprintf(query, sizeof(query),
          "{\"cell\":%llu,\"context\":%.*s,\"op\":\"QUERY\","
          "\"payload\":{\"claim\":%.*s},\"reclaim\":null}",
          (unsigned long long)i, (int)h->sessions[0].length,
          h->sessions[0].context, (int)(t.end - t.start), j.bytes + t.start);
      assert(send_body(c, query, i) == 1);
   }
   assert(h->ordinary == 64 && h->control == 16 && h->high == 80);
   assert(fetch(c, 2, 2) == 3);
   char ack[128];
   ack_string(entry(h, 2), ack);
   char release[16384];
   (void)snprintf(release, sizeof(release),
       "{\"cell\":81,\"context\":%.*s,\"op\":\"RELEASE_CELL\","
       "\"payload\":%s,\"reclaim\":%s}",
       (int)h->sessions[0].length, h->sessions[0].context, ack, ack);
   lose_receipt = true;
   assert(send_body(c, release, 81) == 1);
   assert(h->pending >= 0 && h->high == 80);
   assert(fetch(c, 81, 2) == 3);
   assert(h->high == 81 && h->ordinary == 64 && h->control == 16);
   assert(fetch(c, 2, 2) == 4);
   ack_string(entry(h, 81), ack);
   char *next = rewrite_body(golden_create, 82, ack);
   assert(send_body(c, next, 82) == 1);
   assert(fetch(c, 82, 2) == 3);
   free(next);
   assert(fetch(c, 65, 2) == 3);
   ack_string(entry(h, 65), ack);
   char *wait = wait_body(h, 83);
   next = rewrite_body(wait, 83, ack);
   event_after = 3;
   assert(send_body(c, next, 83) == 1);
   unsigned status = 2;
   for (unsigned i = 0; i < 300 && status == 2; i++) {
      status = fetch(c, 83, 3);
      struct timespec delay = {0, 4000000};
      (void)nanosleep(&delay, NULL);
   }
   assert(status == 3 && h->control == 16 && h->ordinary == 64);
   free(next);
   free(wait);
   hfs9p_connection_delete(c);
   shutdown_endpoint(h);
   reset_fixtures();
}

static void
protected_retry_test(const char *directory_name) {
   assert(!mkdir(directory_name, 0700));
   int directory = open(directory_name, O_RDONLY | O_DIRECTORY);
   assert(directory >= 0);
   Hfs9pLedger *ledger;
   assert(!hfs9p_ledger_new(&ledger, directory, "bridge"));
   Hfs9p *h = new_endpoint_with_ledger(ledger);
   Hfs9pConnection *c = connection(h, golden_hello);
   assert(send_body(c, golden_hello, 1) == 1);
   assert(fetch(c, 1, 2) == 3);
   lose_receipt = true;
   assert(send_body(c, golden_create, 2) == 1);
   assert(h->pending >= 0 && h->high == 1);
   uint8_t *bytes;
   size_t length;
   assert(!hfs9p_ledger_load(ledger, &bytes, &length));
   Hfs9pEndpoint endpoint = h->endpoint;
   Hfs9pEntry *original = entry(h, 2);
   assert(original && original->native && original->native_total < HFS9P_CHUNK);
   uint32_t original_length = original->native_total;
   uint8_t *original_body = malloc(original_length);
   assert(original_body);
   memcpy(original_body, original->native, original_length);
   uint8_t original_frame[HFS9P_HEADER + HFS9P_CHUNK];
   _make_frame(original_frame, original, 0, false);
   memcpy(original_frame + 32, h->sessions[original->session].id, 16);
   size_t frame_length = HFS9P_HEADER + original_length;
   assert(last_accepted_length == frame_length &&
       !memcmp(last_accepted, original_frame, frame_length));
   hfs9p_connection_delete(c);
   shutdown_endpoint(h);
   hfs9p_ledger_delete(ledger);
   assert(!hfs9p_ledger_new(&ledger, directory, "bridge"));
   assert(hfs9p_ledger_claim(ledger, &endpoint, (uint8_t[32]){0}) == ESTALE);
   h = calloc(1, sizeof(*h));
   assert(h);
   h->endpoint = endpoint;
   h->verifier = (Hfs9pVerifier){ledger, claim_cb, protect_cb, verify_cb, begin_io_cb, end_io_cb, NULL};
   h->fd[0] = 10;
   h->fd[1] = 11;
   h->fd[2] = 12;
   assert(!_restore_bytes(h, bytes, length));
   assert(!hfs9p_ledger_claim(ledger, &endpoint, bytes + length - 32));
   free(bytes);
   assert(!pthread_mutex_init(&h->mutex, NULL));
   assert(!pthread_create(&h->worker, NULL, _wait_worker_cb, h));
   assert(!connection_retry(h, h->sessions[0].id, &c));
   c->negotiated = true; /* Device negotiation test is separate. */
   Hfs9pEntry *restored = entry(h, 2);
   assert(restored && restored->native_total == original_length &&
       !memcmp(restored->native, original_body, original_length));
   assert(fetch(c, 2, 2) == 3);
   assert(h->high == 2 && h->pending == -1 && writes == 3);
   assert(last_accepted_length == frame_length &&
       last_accepted_position == (off_t)(2 * HFS9P_STRIDE) &&
       !memcmp(last_accepted, original_frame, frame_length));
   assert(fixtures[2].total == original_length &&
       !memcmp(fixtures[2].request, original_body, original_length));
   /* Each negative control preserves the original cell and immutable reply. */
   uint8_t changed[HFS9P_HEADER + HFS9P_CHUNK];
   for (unsigned mutation = 0; mutation < 6; mutation++) {
      memcpy(changed, original_frame, frame_length);
      size_t changed_length = frame_length;
      if (mutation == 0)
         changed[HFS9P_HEADER + original_length - 2] ^= 1;
      if (mutation == 1) {
         _put(changed + 16, original_length + 1, 4);
         _put(changed + 24, original_length + 1, 4);
         changed[changed_length++] = ' ';
      }
      if (mutation == 2)
         changed[47] ^= 1;
      if (mutation == 3)
         changed[56] ^= 1;
      if (mutation == 4)
         changed[14] ^= 1;
      if (mutation == 5)
         _put(changed + 48, 3, 8);
      unsigned accepted = writes;
      assert(fixture_pwrite(10, changed, changed_length,
          (off_t)(2 * HFS9P_STRIDE)) == -1 && errno == EINVAL);
      assert(writes == accepted && fixtures[2].ready &&
          !memcmp(fixtures[2].request, original_body, original_length));
   }
   assert(rejected_writes == 6);
   free(original_body);
   hfs9p_connection_delete(c);
   shutdown_endpoint(h);
   hfs9p_ledger_delete(ledger);
   assert(!close(directory));
   reset_fixtures();
}

static void
deadline_tests(void) {
   Hfs9p *h = new_endpoint();
   Hfs9pConnection *c = connection(h, golden_hello);
   assert(send_body(c, golden_hello, 1) == 1);
   assert(fetch(c, 1, 2) == 3);
   char *body = wait_body(h, 2);
   event_after = 1;
   pending_read = true;
   lose_receipt = true;
   assert(send_body(c, body, 2) == 1);
   pthread_mutex_lock(&h->mutex);
   Hfs9pEntry *e = entry(h, 2);
   assert(e);
   e->deadline = _now() - 1;
   pthread_mutex_unlock(&h->mutex);
   assert(fetch(c, 2, 3) == 2);
   assert(!e->final && !e->responded); /* Timer cannot invent EMPTY. */
   pending_read = false;
   assert(fetch(c, 2, 3) == 3);
   assert(e->final && poll_count == 1); /* Real late EVENT survives. */
   free(body);
   hfs9p_connection_delete(c);
   shutdown_endpoint(h);
   reset_fixtures();

   h = new_endpoint();
   c = connection(h, golden_hello);
   assert(send_body(c, golden_hello, 1) == 1);
   assert(fetch(c, 1, 2) == 3);
   body = wait_body(h, 2);
   assert(send_body(c, body, 2) == 1);
   unsigned status = 2;
   for (unsigned i = 0; i < 400 && status == 2; i++) {
      status = fetch(c, 2, 3);
      struct timespec delay = {0, 4000000};
      (void)nanosleep(&delay, NULL);
   }
   assert(status == 3);
   e = entry(h, 2);
   assert(e->final && e->poll_count <= 16 && e->poll_count > 1);
   free(body);
   hfs9p_connection_delete(c);
   shutdown_endpoint(h);
   reset_fixtures();
}

static void
session_tests(void) {
   Hfs9p *h = new_endpoint();
   Hfs9pConnection *one = connection(h, golden_hello);
   assert(send_body(one, golden_hello, 1) == 1);
   assert(fetch(one, 1, 2) == 3);
   Hfs9pConnection *two = connection(h, golden_write);
   Hfs9pToken tokens[HFS9P_TOKENS];
   Hfs9pJson j = {(const uint8_t *)golden_hello, strlen(golden_hello), tokens, 0};
   uint32_t payload;
   assert(!hfs9p_json_parse(&j));
   assert(!hfs9p_json_key(&j, 0, "payload", &payload));
   Hfs9pToken p = tokens[payload];
   char hello[8192];
   (void)snprintf(hello, sizeof(hello),
       "{\"cell\":1,\"context\":%.*s,\"op\":\"HELLO\","
       "\"payload\":%.*s,\"reclaim\":null}",
       (int)h->sessions[1].length, h->sessions[1].context,
       (int)(p.end - p.start), j.bytes + p.start);
   assert(send_body(two, hello, 1) == 1);
   assert(fetch(two, 1, 2) == 3);
   Hfs9pEntry *first = NULL, *second = NULL;
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      Hfs9pEntry *e = &h->entries[i];
      if (e->guest && e->guest_cell == 1) {
         if (e->session == 0)
            first = e;
         else
            second = e;
      }
   }
   assert(first && second && first->native_cell != second->native_cell);
   assert(memcmp(first->response_guest_hash, second->response_guest_hash, 32));
   char ack[128], release[8192];
   ack_string(first, ack);
   (void)snprintf(release, sizeof(release),
       "{\"cell\":2,\"context\":%.*s,\"op\":\"RELEASE_CELL\","
       "\"payload\":%s,\"reclaim\":%s}",
       (int)h->sessions[1].length, h->sessions[1].context, ack, ack);
   unsigned before = writes;
   assert(send_body(two, release, 2) == 4);
   assert(writes == before && first->responded && second->responded);
   char *body = rewrite_body(golden_create, 9, NULL);
   assert(send_body(one, body, 9) == 1);
   free(body);
   body = rewrite_body(golden_create, 3, NULL);
   assert(send_body(one, body, 3) == 1); /* Out-of-order first cells. */
   free(body);
   /* e64: arrival is accepted out of order, but both cells wait for the
    * never-seen guest cell 2. Once the hold runs out, 9's expiry first
    * forwards the lower 3, so native order still follows guest order. */
   Hfs9pEntry *third = entry(h, 3), *ninth = entry(h, 9);
   assert(h->high == 2 && third && ninth && !third->issued && !ninth->issued);
   expire_order(h);
   assert(fetch(one, 9, 2) == 3);
   assert(third->native_cell == 3 && ninth->native_cell == 4);
   assert(h->high == 4);
   uint8_t request[21] = {0}, response[16];
   size_t n;
   _put(request, 21, 4);
   request[4] = 240;
   request[7] = 2;
   request[8] = 2;
   _put(request + 9, 1, 8);
   memset(response, 0xa5, sizeof(response));
   assert(!exchange_retry(one, request, sizeof(request), response, 10, &n));
   assert(n == 10 && response[7] == 4 && response[10] == 0xa5);
   request[4] = 104; /* Stock attach cannot bypass the dedicated backend. */
   assert(exchange_retry(one, request, sizeof(request), response,
       sizeof(response), &n) == EINVAL);
   hfs9p_connection_delete(one);
   hfs9p_connection_delete(two);
   shutdown_endpoint(h);
   reset_fixtures();

   Hfs9pSession sparse = {0};
   for (uint64_t i = 1; i <= 511; i += 2)
      assert(!_mark_seen(&sparse, i));
   assert(_mark_seen(&sparse, 513) == ENOSPC);
   assert(!_seen(&sparse, 513));
   assert(!_mark_seen(&sparse, 2));
   assert(!_mark_seen(&sparse, 513));
   assert(_mark_seen(&sparse, 1) == ESTALE);
}

static size_t
chunk_packet(Hfs9pConnection *c, const char *body, uint64_t cell,
    uint32_t offset, uint8_t packet[HFS9P_MSIZE]) {
   Hfs9pEntry e = {0};
   e.native = (uint8_t *)body;
   e.native_total = (uint32_t)strlen(body);
   e.native_cell = cell;
   _hash((const uint8_t *)body, e.native_total, e.native_hash);
   _make_frame(packet + 21, &e, offset, false);
   memcpy(packet + 53, c->endpoint->sessions[c->session].id, 16);
   size_t n = 21 + HFS9P_HEADER + _get(packet + 21 + 24, 4);
   _put(packet, n, 4);
   packet[4] = 240;
   _put(packet + 5, 1234, 2);
   packet[7] = 1;
   packet[8] = 1;
   _put(packet + 9, cell, 8);
   _put(packet + 17, offset / HFS9P_CHUNK, 4);
   return (n);
}

static void
contention_tests(void) {
   Hfs9p *h = new_endpoint();
   Hfs9pConnection *c = connection(h, large_write);
   h->sessions[0].hello = true; /* Real routing with transport-only fixtures. */
   h->sessions[0].order_high = 2; /* Guest cells 1-2 settled (e64 order). */
   Hfs9pConnection *version;
   assert(!connection_retry(h, h->sessions[0].id, &version));
   uint8_t packet[HFS9P_MSIZE], reply[HFS9P_MSIZE], saved[HFS9P_MSIZE];
   size_t length, n;
   length = chunk_packet(c, large_write, 3, 0, packet);
   pthread_mutex_lock(&h->mutex);
   assert(!hfs9p_exchange(c, packet, length, reply, sizeof(reply), &n));
   assert(n == 10 && _get(reply, 4) == 10 && reply[4] == 241 &&
       _get(reply + 5, 2) == 1234 && reply[7] == 4 &&
       _get(reply + 8, 2) == 3);
   assert(!entry(h, 3) && writes == 0 && h->high == 0);
   pthread_mutex_unlock(&h->mutex);
   assert(!exchange_retry(c, packet, length, reply, sizeof(reply), &n));
   assert(n == 8 && reply[7] == 1);
   Hfs9pEntry *e = entry(h, 3);
   assert(e && e->received == 1 && !e->complete && writes == 0);

   /* The later-chunk case must be a real wire retry, never zero-length I/O,
    * invented COPIED, reset assembly, changed native number or new claim. */
   length = chunk_packet(c, large_write, 3, HFS9P_CHUNK, packet);
   memcpy(saved, packet, length);
   pthread_mutex_lock(&h->mutex);
   assert(!hfs9p_exchange(c, packet, length, reply, sizeof(reply), &n));
   assert(n == 10 && _get(reply, 4) == 10 && reply[4] == 241 &&
       _get(reply + 5, 2) == 1234 && reply[7] == 4 &&
       _get(reply + 8, 2) == 3);
   assert(e->received == 1 && !e->complete && !e->native &&
       writes == 0 && h->high == 0 && h->pending == -1);
   packet[21 + 14] = 1; /* Malformed guest flags stay rejected under pressure. */
   assert(hfs9p_exchange(c, packet, length, reply, sizeof(reply), &n) == EINVAL);
   assert(n == 0 && e->received == 1);
   memcpy(packet, saved, length);

   uint8_t fetch_packet[21] = {0};
   _put(fetch_packet, 21, 4);
   fetch_packet[4] = 240;
   _put(fetch_packet + 5, 4321, 2);
   fetch_packet[7] = 2;
   fetch_packet[8] = 2;
   _put(fetch_packet + 9, 3, 8);
   assert(!hfs9p_exchange(c, fetch_packet, sizeof(fetch_packet), reply,
       sizeof(reply), &n));
   assert(n == 8 && _get(reply, 4) == 8 && reply[4] == 241 &&
       _get(reply + 5, 2) == 4321 && reply[7] == 2);

   uint8_t version_packet[64] = {0};
   const char *selection = "9P2000.L.hfs1";
   size_t version_length = 13 + strlen(selection);
   _put(version_packet, version_length, 4);
   version_packet[4] = 100;
   _put(version_packet + 5, 65535, 2);
   _put(version_packet + 7, HFS9P_MSIZE - 1, 4);
   _put(version_packet + 11, strlen(selection), 2);
   memcpy(version_packet + 13, selection, strlen(selection));
   assert(hfs9p_exchange(version, version_packet, version_length, reply,
       sizeof(reply), &n) == EINVAL);
   assert(!atomic_load(&version->negotiated));
   _put(version_packet + 7, HFS9P_MSIZE, 4);
   assert(!hfs9p_exchange(version, version_packet, version_length, reply,
       sizeof(reply), &n));
   assert(n == version_length && reply[4] == 101 &&
       _get(reply + 5, 2) == 65535 &&
       !memcmp(reply + 7, version_packet + 7, version_length - 7));
   assert(e->received == 1 && h->high == 0 && writes == 0);
   pthread_mutex_unlock(&h->mutex);

   assert(!exchange_retry(c, packet, length, reply, sizeof(reply), &n));
   assert(n == 8 && reply[7] == 1 && e->received == 3 && writes == 0);
   for (uint32_t offset = 2 * HFS9P_CHUNK;
       offset < strlen(large_write); offset += HFS9P_CHUNK) {
      length = chunk_packet(c, large_write, 3, offset, packet);
      assert(!exchange_retry(c, packet, length, reply, sizeof(reply), &n));
      assert(n == 8 && reply[7] == 1);
   }
   assert(e->complete && e->issued && e->native_cell == 1 && h->high == 1);
   assert(fetch(c, 3, 2) == 3);
   /* A resource refusal AFTER guest assembly owns the bytes is different:
    * preserve COPIED and the retained mapping rather than claiming no copy. */
   pthread_mutex_lock(&h->mutex);
   h->high = HFS9P_MAX_CELL;
   unsigned before = writes;
   pthread_mutex_unlock(&h->mutex);
   char *blocked = rewrite_body(large_write, 4, NULL);
   assert(send_body(c, blocked, 4) == 1);
   Hfs9pEntry *retained = entry(h, 4);
   assert(retained && retained->complete && !retained->issued &&
       retained->guest && !retained->native && writes == before);
   free(blocked);
   hfs9p_connection_delete(version);
   hfs9p_connection_delete(c);
   shutdown_endpoint(h);
   reset_fixtures();
}

static char *
scope_body(Hfs9pConnection *c, const char *body) {
   Hfs9pToken tokens[HFS9P_TOKENS];
   Hfs9pJson j = {(const uint8_t *)body, strlen(body), tokens, 0};
   uint32_t context, op, payload, domain, scope;
   assert(!hfs9p_json_parse(&j));
   assert(!hfs9p_json_key(&j, 0, "context", &context));
   assert(!hfs9p_json_key(&j, 0, "op", &op));
   Hfs9pSession *session = &c->endpoint->sessions[c->session];
   Hfs9pReplacement replacements[2] = {
      {tokens[context].start, tokens[context].end,
         session->context, session->length}
   };
   unsigned count = 1;
   if (hfs9p_json_equal(&j, op, "VIEW_WAIT")) {
      assert(!hfs9p_json_key(&j, 0, "payload", &payload));
      assert(!hfs9p_json_key(&j, payload, "domain", &domain));
      assert(!hfs9p_json_key(&j, domain, "scope", &scope));
      replacements[count++] = (Hfs9pReplacement){tokens[scope].start,
         tokens[scope].end, session->context, session->length};
   }
   uint8_t *bytes;
   uint32_t length;
   assert(!_rewrite(&j, replacements, count, &bytes, &length));
   char *terminated = realloc(bytes, length + 1);
   assert(terminated);
   terminated[length] = 0;
   return (terminated);
}

/* Called with the actual endpoint mutex held, as the public exchange does.
 * Only packet construction and native I/O are fixtures; admission/polling and
 * snapshot/restore are the production state machine, not synthesized entries. */
static int
send_locked(Hfs9pConnection *c, const char *body, uint64_t cell,
    uint16_t tag, bool partial) {
   uint8_t packet[HFS9P_MSIZE];
   size_t length = chunk_packet(c, body, cell, 0, packet);
   assert(!partial || strlen(body) > HFS9P_CHUNK);
   return (_send(c->endpoint, c->session, tag, packet + 21, length - 21));
}

static void
snapshot_roundtrip(Hfs9p *h, unsigned candidates, unsigned controls,
    unsigned transitions) {
   Hfs9pAccounting accounting;
   assert(!_account(h, &accounting));
   assert(accounting.candidates == candidates && accounting.control == controls);
   unsigned actual_transitions = 0;
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      Hfs9pEntry *e = &h->entries[i];
      if (e->guest && !e->issued && _owns_credit(e))
         actual_transitions++;
   }
   assert(actual_transitions == transitions);
   uint8_t *bytes = NULL, *again = NULL;
   size_t length, again_length;
   assert(!_snapshot(h, &bytes, &length));
   Hfs9p *restored = calloc(1, sizeof(*restored));
   assert(restored);
   restored->endpoint = h->endpoint;
   assert(!_restore_bytes(restored, bytes, length));
   assert(!_account(restored, &accounting));
   assert(accounting.candidates == candidates && accounting.control == controls);
   assert(!_snapshot(restored, &again, &again_length));
   assert(length == again_length && !memcmp(bytes, again, length));
   _discard_recovery(restored);
   free(again);
   free(bytes);
}

static void
assembly_accounting_tests(void) {
   Hfs9p *h = new_endpoint();
   Hfs9pConnection *one = connection(h, golden_hello);
   Hfs9pConnection *two = connection(h, golden_write);
   pthread_mutex_lock(&h->mutex);
   /* Reserved tags cannot multiply the endpoint's single extra workspace. */
   for (uint64_t cell = 1; cell <= 16; cell++) {
      char *body = rewrite_body(large_write, cell, NULL);
      char *scoped = scope_body(one, body);
      assert(!send_locked(one, scoped, cell, 65533, true));
      free(scoped);
      free(body);
   }
   char *body = rewrite_body(large_write, 1, NULL);
   char *scoped = scope_body(two, body);
   assert(!send_locked(two, scoped, 1, 65534, true));
   snapshot_roundtrip(h, 17, 0, 0);
   free(scoped);
   free(body);
   body = rewrite_body(large_write, 2, NULL);
   scoped = scope_body(two, body);
   unsigned before = writes;
   assert(send_locked(two, scoped, 2, 65534, true) == ENOSPC);
   assert(writes == before && !_seen(&h->sessions[two->session], 2));
   snapshot_roundtrip(h, 17, 0, 0);
   free(scoped);
   free(body);
   pthread_mutex_unlock(&h->mutex);
   hfs9p_connection_delete(one);
   hfs9p_connection_delete(two);
   shutdown_endpoint(h);
   reset_fixtures();

   h = new_endpoint();
   one = connection(h, golden_hello);
   two = connection(h, golden_write);
   pthread_mutex_lock(&h->mutex);
   h->sessions[0].hello = h->sessions[1].hello = true;
   char *wait = wait_body(h, 1);
   char *wait_one = scope_body(one, wait);
   char *wait_two = scope_body(two, wait);
   assert(!send_locked(one, wait_one, 1, 65534, false));
   assert(!send_locked(two, wait_two, 1, 65534, false));
   Hfs9pEntry *waits[2] = {NULL, NULL};
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      Hfs9pEntry *e = &h->entries[i];
      if (e->guest && e->guest_cell == 1)
         waits[e->session] = e;
   }
   assert(waits[0] && waits[1] && h->control == 2);
   hold_receipts = true;
   assert(!send_locked(one, golden_create, 2, 1234, false));
   int pending = h->pending;
   assert(pending >= 0 && h->high == 2);
   assert(_wait_step(h, waits[0]) == EAGAIN);
   assert(_wait_step(h, waits[1]) == EAGAIN);
   assert(h->pending == pending && !waits[0]->issued && !waits[1]->issued &&
       waits[0]->polls && waits[1]->polls && !waits[0]->native &&
       !waits[1]->native && h->control == 2);
   /* Two real poll transitions retain credits. They do not consume the 16+1
    * new-assembly/candidate budget, including its one reserved replacement. */
   for (uint64_t cell = 10; cell < 25; cell++) {
      body = rewrite_body(large_write, cell, NULL);
      scoped = scope_body(one, body);
      assert(!send_locked(one, scoped, cell, 1234, true));
      free(scoped);
      free(body);
   }
   body = rewrite_body(large_write, 10, NULL);
   scoped = scope_body(two, body);
   assert(!send_locked(two, scoped, 10, 65533, true));
   free(scoped);
   free(body);
   snapshot_roundtrip(h, 17, 2, 2); /* 19 !issued entries are valid here. */
   body = rewrite_body(large_write, 11, NULL);
   scoped = scope_body(two, body);
   assert(send_locked(two, scoped, 11, 65533, true) == ENOSPC);
   assert(!_seen(&h->sessions[two->session], 11));
   snapshot_roundtrip(h, 17, 2, 2);
   /* The same invariant gates snapshot protection, not just admission. */
   Hfs9pEntry *reserve = NULL;
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      Hfs9pEntry *e = &h->entries[i];
      if (e->guest && e->session == two->session && e->guest_cell == 10)
         reserve = e;
   }
   assert(reserve);
   reserve->assembly_control = false;
   uint8_t *invalid = NULL;
   size_t invalid_length = 0;
   assert(_snapshot(h, &invalid, &invalid_length) == EINVAL && !invalid);
   reserve->assembly_control = true;
   snapshot_roundtrip(h, 17, 2, 2);
   free(scoped);
   free(body);
   free(wait_one);
   free(wait_two);
   free(wait);
   pthread_mutex_unlock(&h->mutex);
   hfs9p_connection_delete(one);
   hfs9p_connection_delete(two);
   shutdown_endpoint(h);
   reset_fixtures();
}

static void
view_protocol_tests(void) {
   Hfs9p *h = new_endpoint();
   Hfs9pConnection *c = connection_protocol(h, view_hello, 1);
   assert(send_body(c, view_hello, 1) == 1);
   assert(fetch(c, 1, 2) == 3);
   assert(h->sessions[0].hello && h->sessions[0].minor == 1);
   /* Negotiation uses copied enrollment and refuses the old Tversion even
    * without acquiring the endpoint mutex or changing any ledger state. */
   Hfs9pConnection *unnegotiated;
   assert(!connection_retry(h, h->sessions[0].id, &unnegotiated));
   uint8_t version_request[64] = {0}, version_reply[64];
   size_t version_size;
   const char *old_version = "9P2000.L.hfs1";
   _put(version_request, 13 + strlen(old_version), 4);
   version_request[4] = 100;
   _put(version_request + 5, 65535, 2);
   _put(version_request + 7, HFS9P_MSIZE, 4);
   _put(version_request + 11, strlen(old_version), 2);
   memcpy(version_request + 13, old_version, strlen(old_version));
   assert(hfs9p_exchange(unnegotiated, version_request,
       13 + strlen(old_version), version_reply, sizeof(version_reply),
       &version_size) == EINVAL);
   assert(!atomic_load(&unnegotiated->negotiated));
   hfs9p_connection_delete(unnegotiated);
   Hfs9pConnection *legacy = connection(h, golden_write);
   assert(h->sessions[1].minor == 0);
   assert(send_body(c, view_dirty_page, 4) == 1);
   Hfs9pEntry *e = entry(h, 4);
   assert(e && !e->control && e->minor == 1);
   pthread_mutex_lock(&h->mutex);
   /* Structurally parsed/routed byte requests: host supplies semantic proof. */
   Hfs9pEntry candidate = {0};
   candidate.session = 0;
   candidate.minor = 1;
   candidate.guest_cell = 5;
   candidate.guest = (uint8_t *)view_reserve;
   candidate.total = (uint32_t)strlen(view_reserve);
   assert(!_command(h, &candidate) && !candidate.control);
   candidate.guest_cell = 6;
   candidate.guest = (uint8_t *)view_freeze;
   candidate.total = (uint32_t)strlen(view_freeze);
   assert(!_command(h, &candidate) && candidate.control);
   char *bad_tuple = strdup(view_freeze);
   assert(bad_tuple);
   char *feature = strstr(bad_tuple, "\"features\":2047");
   assert(feature);
   memcpy(feature + strlen("\"features\":"), "4095", 4);
   candidate.guest = (uint8_t *)bad_tuple;
   assert(_command(h, &candidate) == EINVAL);
   free(bad_tuple);
   candidate.guest = (uint8_t *)view_freeze;
   /* Envelope tuple is independent of syntactically valid payload bytes. */
   h->sessions[0].minor = 0;
   assert(_command(h, &candidate) == EINVAL);
   h->sessions[0].minor = 1;
   assert(!_body_version(h, (const uint8_t *)view_freeze,
       (uint32_t)strlen(view_freeze), 1, false));
   assert(_body_version(h, (const uint8_t *)view_freeze,
       (uint32_t)strlen(view_freeze), 0, false));
   assert(_body_version(h, (const uint8_t *)golden_hello,
       (uint32_t)strlen(golden_hello), 1, false));
   uint8_t *snapshot;
   size_t length;
   assert(!_snapshot(h, &snapshot, &length));
   assert(!memcmp(snapshot, "H9LEDG2\0", 8));
   pthread_mutex_unlock(&h->mutex);
   Hfs9p *restored = calloc(1, sizeof(*restored));
   restored->endpoint = h->endpoint;
   assert(!_restore_bytes(restored, snapshot, length));
   assert(restored->sessions[0].minor == 1 && restored->sessions[0].hello);
   assert(restored->session_count == 2 && restored->sessions[1].minor == 0);
   _discard_recovery(restored);
   /* Flipping persisted minor1->0 while keeping retained1.1 bytes must fail,
    * even when an attacker recomputes the non-authoritative checksum. */
   Hfs9pReader reader = {snapshot, length - 32, 61, 0};
   (void)_unpack_bytes(&reader, 16);
   uint32_t context_length = (uint32_t)_unpack(&reader, 4);
   (void)_unpack_bytes(&reader, context_length);
   (void)_unpack(&reader, 8);
   unsigned ranges = (unsigned)_unpack(&reader, 2);
   (void)_unpack_bytes(&reader, ranges * 16);
   (void)_unpack(&reader, 1);
   assert(snapshot[reader.cursor] == 1);
   snapshot[reader.cursor] = 0;
   _hash(snapshot, length - 32, snapshot + length - 32);
   restored = calloc(1, sizeof(*restored));
   restored->endpoint = h->endpoint;
   assert(_restore_bytes(restored, snapshot, length) == EINVAL);
   _discard_recovery(restored);
   free(snapshot);
   hfs9p_connection_delete(legacy);
   hfs9p_connection_delete(c);
   shutdown_endpoint(h);
   reset_fixtures();
   h = new_endpoint();
   c = connection(h, golden_hello);
   assert(send_body(c, golden_hello, 1) == 1);
   assert(fetch(c, 1, 2) == 3);
   pthread_mutex_lock(&h->mutex);
   h->sessions[0].minor = 1;
   assert(!_snapshot(h, &snapshot, &length));
   pthread_mutex_unlock(&h->mutex);
   restored = calloc(1, sizeof(*restored));
   restored->endpoint = h->endpoint;
   assert(_restore_bytes(restored, snapshot, length) == EINVAL);
   _discard_recovery(restored);
   free(snapshot);
   hfs9p_connection_delete(c);
   shutdown_endpoint(h);
   reset_fixtures();
}

/* Literal successor routing and exact receipt-byte preservation. The literal
 * signature is not authenticated here, and translated guest cells do not mint
 * native provenance or source/event/floor authority (followup y04/C6).
 */
static void
controlled_protocol_tests(void) {
   for (uint16_t minor = 2; minor <= 3; minor++) {
      Hfs9p *h = new_endpoint();
      const char *hello = minor == 3 ? cleanup_hello : controlled_hello;
      Hfs9pConnection *c = connection_protocol(h, hello, minor);
      assert(send_body(c, hello, 1) == 1 && fetch(c, 1, 2) == 3);
      char *allocate = rewrite_body(minor == 2 ? controlled_allocate :
          controlled_allocate3, 97, NULL);
      assert(send_body(c, allocate, 97) == 1 && fetch(c, 97, 2) == 3);
      free(allocate);
      Hfs9pEntry *response = entry(h, 97);
      assert(response->native_cell == 2 && response->guest_cell == 97);
      Hfs9pJson j = {response->response_guest, response->response_total, h->tokens, 0};
      uint32_t verification;
      assert(!hfs9p_json_parse(&j) &&
          !hfs9p_json_key(&j, 0, "verification", &verification));
      Hfs9pToken receipt = j.tokens[verification];
      assert(receipt.end - receipt.start == strlen(controlled_receipt) &&
          !memcmp(j.bytes + receipt.start, controlled_receipt, strlen(controlled_receipt)));
      uint32_t bound_cell, command_hash;
      uint64_t native_cell;
      uint8_t original_hash[32];
      assert(!hfs9p_json_key(&j, verification, "cell", &bound_cell) &&
          !hfs9p_json_u64(&j, bound_cell, &native_cell) && native_cell == 2);
      assert(!hfs9p_json_key(&j, verification, "command_hash", &command_hash) &&
          !_hex(&j, command_hash, original_hash, 32, false));
      if (minor == 2)
         assert(!memcmp(original_hash, response->native_hash, 32));
      /* Minor3 forwards this literal unchanged too; its minor2 command hash
       * cannot authenticate minor3. This forwarder never grants that authority. */

      pthread_mutex_lock(&h->mutex);
      const char *inputs[] = {controlled_allocate, controlled_gap_resolve,
          cleanup_cleanup_empty};
      for (unsigned k = 0; k < 3; k++) {
         Hfs9pJson command = {(const uint8_t *)inputs[k], strlen(inputs[k]), h->tokens, 0};
         uint32_t cell;
         uint64_t number;
         assert(!hfs9p_json_parse(&command) && !hfs9p_json_key(&command, 0, "cell", &cell) &&
             !hfs9p_json_u64(&command, cell, &number));
         Hfs9pEntry candidate = {.session=0, .minor=minor, .guest_cell=number,
             .guest=(uint8_t *)inputs[k], .total=(uint32_t)strlen(inputs[k])};
         bool selected = k == 2 ? minor == 3 : minor == 2;
         int error = _command(h, &candidate);
         assert((error == 0) == selected);
         if (selected)
            assert(candidate.control == (k != 0));
      }
      uint8_t *snapshot;
      size_t length;
      assert(!_snapshot(h, &snapshot, &length));
      pthread_mutex_unlock(&h->mutex);
      Hfs9p *restored = calloc(1, sizeof(*restored));
      assert(restored);
      restored->endpoint = h->endpoint;
      assert(!_restore_bytes(restored, snapshot, length));
      assert(restored->sessions[0].minor == minor);
      _discard_recovery(restored);
      free(snapshot);
      hfs9p_connection_delete(c);
      shutdown_endpoint(h);
      reset_fixtures();
   }
}

/* Session two (golden_write's scope) with its own HELLO at guest cell 1, as
 * session_tests builds it. */
static Hfs9pConnection *
second_session(Hfs9p *h) {
   Hfs9pConnection *two = connection(h, golden_write);
   Hfs9pToken tokens[HFS9P_TOKENS];
   Hfs9pJson j = {(const uint8_t *)golden_hello, strlen(golden_hello), tokens, 0};
   uint32_t payload;
   assert(!hfs9p_json_parse(&j) && !hfs9p_json_key(&j, 0, "payload", &payload));
   Hfs9pToken p = tokens[payload];
   char hello[8192];
   (void)snprintf(hello, sizeof(hello),
       "{\"cell\":1,\"context\":%.*s,\"op\":\"HELLO\","
       "\"payload\":%.*s,\"reclaim\":null}",
       (int)h->sessions[two->session].length, h->sessions[two->session].context,
       (int)(p.end - p.start), j.bytes + p.start);
   assert(send_body(two, hello, 1) == 1 && fetch(two, 1, 2) == 3);
   return (two);
}

static Hfs9pConnection *
hello_session(Hfs9p *h) {
   Hfs9pConnection *c = connection(h, golden_hello);
   assert(send_body(c, golden_hello, 1) == 1 && fetch(c, 1, 2) == 3);
   return (c);
}

static void
send_cell(Hfs9pConnection *c, const char *template, uint64_t cell,
    const char *reclaim, unsigned status) {
   char *body = rewrite_body(template, cell, reclaim);
   assert(send_body(c, body, cell) == status);
   free(body);
}

/* e64, inside the exporter: a complete cell blocked by another session's
 * pinned writer must not be overtaken by a later cell of its own session
 * whose FETCH finds the writer free first. Before e64, guest cell 3 took
 * the lower native number and cell 2 stayed unforwarded. */
static void
order_pinned_writer_test(void) {
   Hfs9p *h = new_endpoint();
   Hfs9pConnection *one = hello_session(h), *two = second_session(h);
   hold_receipts = true;
   send_cell(two, golden_write, 2, NULL, 1);
   hold_receipts = false;
   Hfs9pEntry *pin = session_entry(h, two->session, 2);
   assert(pin && h->pending == (int)(pin - h->entries));
   send_cell(one, golden_create, 2, NULL, 1);
   send_cell(one, golden_create, 3, NULL, 1);
   Hfs9pEntry *second = session_entry(h, one->session, 2);
   Hfs9pEntry *third = session_entry(h, one->session, 3);
   assert(second && third && second->complete && third->complete &&
       !second->issued && !third->issued);
   assert(fetch(two, 2, 2) == 3 && h->pending == -1 && !second->issued);
   assert(fetch(one, 3, 2) == 3);
   assert(second->issued && third->issued &&
       second->native_cell < third->native_cell);
   hfs9p_connection_delete(one);
   hfs9p_connection_delete(two);
   shutdown_endpoint(h);
   reset_fixtures();
}

typedef struct {
   Hfs9pConnection *connection;
   pthread_barrier_t *start;
   uint64_t cell;
} OrderSender;

/* One guest worker: waits for the others, then sends its own cell. Higher
 * cells start a little earlier, as a guest whose lower cell's SEND yielded
 * the writer after CAPACITY or a busy fixed tag. */
static void *
order_sender_cb(void *arg) {
   OrderSender *s = arg;
   (void)pthread_barrier_wait(s->start);
   struct timespec delay = {0, (long)(9 - s->cell) * 300000L};
   (void)nanosleep(&delay, NULL);
   send_cell(s->connection, golden_create, s->cell, NULL, 1);
   return (NULL);
}

/* e64: concurrent SENDs of one session's guest cells 2..9 arrive in any
 * order and contend for the endpoint mutex (CAPACITY retries), yet native
 * numbers follow guest order. Before e64 each cell was forwarded when its
 * own SEND completed, so the native order followed arrival. */
static void
order_concurrent_test(void) {
   Hfs9p *h = new_endpoint();
   Hfs9pConnection *one = hello_session(h);
   pthread_barrier_t start;
   pthread_t threads[8];
   OrderSender senders[8];
   assert(!pthread_barrier_init(&start, NULL, 8));
   for (unsigned i = 0; i < 8; i++) {
      senders[i] = (OrderSender){one, &start, 9 - i};
      assert(!pthread_create(&threads[i], NULL, order_sender_cb, &senders[i]));
   }
   for (unsigned i = 0; i < 8; i++)
      assert(!pthread_join(threads[i], NULL));
   assert(!pthread_barrier_destroy(&start));
   for (uint64_t cell = 9; cell >= 2; cell--)
      assert(fetch(one, cell, 2) == 3);
   for (uint64_t cell = 2; cell < 9; cell++)
      assert(entry(h, cell)->native_cell < entry(h, cell + 1)->native_cell);
   assert(h->high == 9);
   hfs9p_connection_delete(one);
   shutdown_endpoint(h);
   reset_fixtures();
}

/* e64, the zq3-r6 R6-2 shape: a later cell arrives before an earlier donor
 * send of its session and waits for it; an unseen cell is waited for only
 * until the hold runs out, and then arrives late without a hold. */
static void
order_gap_test(void) {
   Hfs9p *h = new_endpoint();
   Hfs9pConnection *c = hello_session(h);
   send_cell(c, golden_create, 2, NULL, 1);
   assert(fetch(c, 2, 2) == 3);
   char ack[128];
   ack_string(entry(h, 2), ack);
   send_cell(c, golden_create, 4, NULL, 1); /* The QUERY 49 of R6-2. */
   assert(!entry(h, 4)->issued && h->high == 2);
   send_cell(c, golden_create, 3, ack, 1); /* The donor VIEW_WAIT 48. */
   assert(entry(h, 3)->issued && entry(h, 3)->native_cell == 3 && !entry(h, 2));
   assert(fetch(c, 4, 2) == 3 && entry(h, 4)->native_cell == 4);
   send_cell(c, golden_create, 6, NULL, 1);
   assert(!entry(h, 6)->issued && h->high == 4);
   expire_order(h);
   assert(fetch(c, 6, 2) == 3 && entry(h, 6)->native_cell == 5);
   send_cell(c, golden_create, 5, NULL, 1);
   assert(entry(h, 5)->issued && entry(h, 5)->native_cell == 6);
   hfs9p_connection_delete(c);
   shutdown_endpoint(h);
   reset_fixtures();
}

/* Sends a control QUERY (golden_create's claim) as guest cell `cell` of c's
 * session and expects COPIED. */
static void
send_query(Hfs9pConnection *c, uint64_t cell) {
   const Hfs9pSession *s = &c->endpoint->sessions[c->session];
   Hfs9pToken tokens[HFS9P_TOKENS];
   Hfs9pJson j = {(const uint8_t *)golden_create, strlen(golden_create), tokens, 0};
   uint32_t payload, claim;
   assert(!hfs9p_json_parse(&j) && !hfs9p_json_key(&j, 0, "payload", &payload) &&
       !hfs9p_json_key(&j, payload, "claim", &claim));
   char query[16384];
   (void)snprintf(query, sizeof(query),
       "{\"cell\":%llu,\"context\":%.*s,\"op\":\"QUERY\","
       "\"payload\":{\"claim\":%.*s},\"reclaim\":null}",
       (unsigned long long)cell, (int)s->length, s->context,
       (int)(tokens[claim].end - tokens[claim].start), j.bytes + tokens[claim].start);
   assert(send_body(c, query, cell) == 1);
}

/* e64 review: a control behind a complete ordinary cell that has no credit
 * is not held. It bypasses the ordinary cell at once, as before e64, and
 * the bypassed cell relies on the runtime's guest reorder window. */
static void
order_credit_bypass_test(void) {
   Hfs9p *h = new_endpoint();
   Hfs9pConnection *c = hello_session(h);
   for (uint64_t cell = 2; cell <= 64; cell++)
      send_cell(c, golden_create, cell, NULL, 1);
   assert(h->ordinary == 64 && h->high == 64);
   send_cell(c, golden_create, 65, NULL, 1);
   assert(!entry(h, 65)->issued && h->high == 64);
   uint64_t before = _now();
   send_query(c, 66);
   assert(_now() - before < HFS9P_ORDER_HOLD_MS);
   assert(entry(h, 66)->control && entry(h, 66)->issued &&
       entry(h, 66)->native_cell == 65 && !entry(h, 65)->issued);
   hfs9p_connection_delete(c);
   shutdown_endpoint(h);
   reset_fixtures();
}

static Hfs9pLedger *
refusal_ledger(const char *base, const char *name, int *directory) {
   char path[4096];
   (void)snprintf(path, sizeof(path), "%s-%s", base, name);
   assert(!mkdir(path, 0700));
   *directory = open(path, O_RDONLY | O_DIRECTORY);
   assert(*directory >= 0);
   Hfs9pLedger *ledger;
   assert(!hfs9p_ledger_new(&ledger, *directory, "bridge"));
   return (ledger);
}

/* Restores the last protected ledger bytes into a detached decoder. */
static Hfs9p *
restored_ledger(Hfs9pLedger *ledger, const Hfs9p *h) {
   uint8_t *bytes;
   size_t length;
   assert(!hfs9p_ledger_load(ledger, &bytes, &length));
   Hfs9p *restored = calloc(1, sizeof(*restored));
   assert(restored);
   restored->endpoint = h->endpoint;
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++)
      restored->entries[i].donor = -1;
   assert(!_restore_bytes(restored, bytes, length));
   free(bytes);
   return (restored);
}

/* Session with HELLO 1 and a responded guest cell 2, the donor that guest
 * cell 3 reclaims. */
static Hfs9pConnection *
donor_session(Hfs9p *h, char ack[128]) {
   Hfs9pConnection *c = hello_session(h);
   send_cell(c, golden_create, 2, NULL, 1);
   assert(fetch(c, 2, 2) == 3);
   ack_string(entry(h, 2), ack);
   return (c);
}

/* e64: ESTALE, EINVAL and a first-attempt EACCES for a donor send fail the
 * guest cell back. The writer is free, the donor and the native number stay,
 * the cell is refused for good, and the ledger no longer replays it. */
static void
refusal_permanent_test(const char *base) {
   const int errors[] = {ESTALE, EINVAL, EACCES};
   const char *names[] = {"estale", "einval", "eacces"};
   for (unsigned k = 0; k < 3; k++) {
      int directory;
      Hfs9pLedger *ledger = refusal_ledger(base, names[k], &directory);
      Hfs9p *h = new_endpoint_with_ledger(ledger);
      char ack[128];
      Hfs9pConnection *c = donor_session(h, ack);
      refuse_errno = errors[k];
      send_cell(c, golden_create, 3, ack, 4);
      refuse_errno = 0;
      assert(refused_writes == 1 && !entry(h, 3) && h->pending == -1);
      assert(h->high == 2 && entry(h, 2) && entry(h, 2)->responded);
      assert(_seen(&h->sessions[0], 3) && fetch(c, 3, 2) == 4);
      Hfs9p *restored = restored_ledger(ledger, h);
      assert(restored->pending == -1 && !entry(restored, 3) &&
          entry(restored, 2) && restored->high == 2 &&
          _seen(&restored->sessions[0], 3));
      _discard_recovery(restored);
      send_cell(c, golden_create, 4, ack, 1);
      assert(entry(h, 4)->issued && entry(h, 4)->native_cell == 3 && !entry(h, 2));
      hfs9p_connection_delete(c);
      shutdown_endpoint(h);
      hfs9p_ledger_delete(ledger);
      assert(!close(directory));
      reset_fixtures();
   }
}

/* e64: a transient (EAGAIN) or uncertain (EIO, EHOSTDOWN, unclassified
 * EPERM) refusal, and EACCES on a replay, keep the exact candidate pinned
 * and protected; the identical replay is then admitted. p64: ESTALE on a
 * replay stays pinned too, since it could be the authentication ESTALE of
 * a send an earlier attempt admitted; e64 failed it back. */
static void
refusal_transient_test(const char *base) {
   const int errors[] = {EAGAIN, EIO, EHOSTDOWN, EPERM};
   const char *names[] = {"eagain", "eio", "ehostdown", "eperm"};
   for (unsigned k = 0; k < 4; k++) {
      int directory;
      Hfs9pLedger *ledger = refusal_ledger(base, names[k], &directory);
      Hfs9p *h = new_endpoint_with_ledger(ledger);
      char ack[128];
      Hfs9pConnection *c = donor_session(h, ack);
      refuse_errno = errors[k];
      send_cell(c, golden_create, 3, ack, 1);
      Hfs9pEntry *e = entry(h, 3);
      assert(e && e->uncertain && !e->issued && e->native_cell == 3);
      assert(h->pending == (int)(e - h->entries) && h->high == 2);
      refuse_errno = EACCES; /* A replay may follow a lost admission. */
      assert(fetch_once(c, 3) == 2 && h->pending == (int)(e - h->entries));
      refuse_errno = ESTALE;
      assert(fetch_once(c, 3) == 2 && h->pending == (int)(e - h->entries));
      refuse_errno = 0;
      Hfs9p *restored = restored_ledger(ledger, h);
      Hfs9pEntry *kept = entry(restored, 3);
      assert(kept && restored->pending == (int)(kept - restored->entries) &&
          kept->native_total == e->native_total &&
          !memcmp(kept->native, e->native, e->native_total));
      _discard_recovery(restored);
      assert(fetch(c, 3, 2) == 3 && e->issued && !entry(h, 2) &&
          h->high == 3 && h->pending == -1);
      hfs9p_connection_delete(c);
      shutdown_endpoint(h);
      hfs9p_ledger_delete(ledger);
      assert(!close(directory));
      reset_fixtures();
   }
}

/* Bypasses c's ordinary multi-chunk guest cell 2 (p64): the first chunk of
 * its native write is refused EAGAIN (no runtime credit, so no native
 * number), and control QUERY guest cell 3 then takes the next native
 * number. Cell 2 stays complete and unforwarded, with no candidate. */
static void
bypass_ordinary(Hfs9pConnection *c) {
   Hfs9p *h = c->endpoint;
   char *body = rewrite_body(large_write, 2, NULL);
   char *scoped = scope_body(c, body);
   refuse_ordinary_errno = EAGAIN;
   assert(send_body(c, scoped, 2) == 1);
   send_query(c, 3);
   refuse_ordinary_errno = 0;
   free(scoped);
   free(body);
   Hfs9pEntry *bypassed = session_entry(h, c->session, 2);
   Hfs9pEntry *query = session_entry(h, c->session, 3);
   assert(bypassed && bypassed->complete && !bypassed->issued &&
       !bypassed->native && !bypassed->uncertain);
   assert(query && query->issued && query->native_cell == h->high);
   assert(h->pending == -1 && refused_writes == 2);
   refused_writes = 0;
}

/* p64: a bypassed ordinary cell is refused as a stale guest cell when it is
 * finally written. In the lab the runtime does that once a cell 16 or more
 * guest numbers above it was admitted; here the fixture refuses it after
 * allocating its native number, as the runtime does. The cell fails back to
 * the guest: the writer is free, the other session proceeds, the spent
 * native number is not reused (the fixture would refuse a different frame
 * there EINVAL), and the ledger no longer replays the cell. Before p64 the
 * exporter pinned the refused cell as its writer for good. */
static void
refusal_original_permanent_test(const char *base) {
   int directory;
   Hfs9pLedger *ledger = refusal_ledger(base, "original-estale", &directory);
   Hfs9p *h = new_endpoint_with_ledger(ledger);
   Hfs9pConnection *one = hello_session(h), *two = second_session(h);
   bypass_ordinary(one);
   assert(h->high == 3);
   refuse_ordinary_errno = ESTALE;
   assert(fetch_once(one, 2) == 4);
   refuse_ordinary_errno = 0;
   assert(refused_writes == 1 && h->pending == -1 && h->high == 4);
   assert(!session_entry(h, one->session, 2) &&
       _seen(&h->sessions[one->session], 2) && fetch_once(one, 2) == 4);
   Hfs9p *restored = restored_ledger(ledger, h);
   assert(restored->pending == -1 && restored->high == 4 &&
       !session_entry(restored, one->session, 2));
   _discard_recovery(restored);
   send_cell(two, golden_write, 2, NULL, 1);
   assert(fetch(two, 2, 2) == 3 &&
       session_entry(h, two->session, 2)->native_cell == 5);
   send_cell(one, golden_create, 4, NULL, 1);
   assert(fetch(one, 4, 2) == 3 &&
       session_entry(h, one->session, 4)->native_cell == 6);
   hfs9p_connection_delete(one);
   hfs9p_connection_delete(two);
   shutdown_endpoint(h);
   hfs9p_ledger_delete(ledger);
   assert(!close(directory));
   reset_fixtures();
}

/* p64: EINVAL for a later chunk of an ordinary cell proves that the runtime
 * allocated the cell's native number at its accepted first chunk, so the
 * cell fails back at once and the next cell takes the following number. */
static void
refusal_original_inserted_test(void) {
   Hfs9p *h = new_endpoint();
   Hfs9pConnection *c = hello_session(h);
   char *body = rewrite_body(large_write, 2, NULL);
   char *scoped = scope_body(c, body);
   refuse_ordinary_errno = EINVAL;
   refuse_ordinary_from = HFS9P_CHUNK;
   assert(send_body(c, scoped, 2) == 4);
   refuse_ordinary_errno = 0;
   assert(refused_writes == 1 && h->pending == -1 && h->high == 2 &&
       !entry(h, 2) && _seen(&h->sessions[c->session], 2));
   send_cell(c, golden_create, 3, NULL, 1);
   assert(fetch(c, 3, 2) == 3 && entry(h, 3)->native_cell == 3);
   free(scoped);
   free(body);
   hfs9p_connection_delete(c);
   shutdown_endpoint(h);
   reset_fixtures();
}

/* p64: a single-chunk ordinary cell refused WouldBlock may already hold its
 * native number (the fixture models a full dispatch queue after
 * allocation), so it stays pinned and replays with the same number, and a
 * later control does not take that number. Before p64 the exporter dropped
 * the candidate and the control took the spent number. */
static void
refusal_single_chunk_eagain_test(void) {
   Hfs9p *h = new_endpoint();
   Hfs9pConnection *c = hello_session(h);
   refuse_ordinary_errno = EAGAIN;
   refuse_allocated = true;
   send_cell(c, golden_create, 2, NULL, 1);
   Hfs9pEntry *e = entry(h, 2);
   assert(e && e->native && e->native_cell == 2 && e->uncertain &&
       h->pending == (int)(e - h->entries) && h->high == 1);
   send_query(c, 3);
   assert(!entry(h, 3)->issued && h->high == 1);
   refuse_ordinary_errno = 0;
   assert(fetch(c, 2, 2) == 3 && e->issued && e->native_cell == 2);
   assert(fetch(c, 3, 2) == 3 && entry(h, 3)->native_cell == 3);
   assert(rejected_writes == 0);
   hfs9p_connection_delete(c);
   shutdown_endpoint(h);
   reset_fixtures();
}

/* p64: a bypassed ordinary cell keeps the writer pinned when its refusal is
 * transient, uncertain or leaves its native number unknown: EIO, EHOSTDOWN,
 * EPERM, EACCES (an authentication refusal before allocation or a content
 * refusal after it), EINVAL on the first chunk (a frame refusal before
 * allocation or a content refusal after it), and then ESTALE on a replay
 * (possibly the authentication ESTALE of an admitted cell). The identical
 * replay is admitted with the same native number. This is a no-change
 * guard: the pre-p64 exporter pinned every non-donor refusal. */
static void
refusal_original_pinned_test(const char *base) {
   const int errors[] = {EIO, EHOSTDOWN, EPERM, EACCES, EINVAL};
   const char *names[] = {"original-eio", "original-ehostdown",
       "original-eperm", "original-eacces", "original-einval"};
   for (unsigned k = 0; k < 5; k++) {
      int directory;
      Hfs9pLedger *ledger = refusal_ledger(base, names[k], &directory);
      Hfs9p *h = new_endpoint_with_ledger(ledger);
      Hfs9pConnection *c = hello_session(h);
      bypass_ordinary(c);
      refuse_ordinary_errno = errors[k];
      assert(fetch_once(c, 2) == 2);
      Hfs9pEntry *e = entry(h, 2);
      assert(e && e->uncertain && !e->issued && e->native_cell == 3);
      assert(h->pending == (int)(e - h->entries) && h->high == 2);
      refuse_ordinary_errno = ESTALE;
      assert(fetch_once(c, 2) == 2 && h->pending == (int)(e - h->entries));
      refuse_ordinary_errno = 0;
      Hfs9p *restored = restored_ledger(ledger, h);
      Hfs9pEntry *kept = entry(restored, 2);
      assert(kept && restored->pending == (int)(kept - restored->entries) &&
          !memcmp(kept->native, e->native, e->native_total));
      _discard_recovery(restored);
      assert(fetch(c, 2, 2) == 3 && e->issued && e->native_cell == 3 &&
          h->high == 3 && h->pending == -1);
      hfs9p_connection_delete(c);
      shutdown_endpoint(h);
      hfs9p_ledger_delete(ledger);
      assert(!close(directory));
      reset_fixtures();
   }
}

/* Restarts the exporter from h's last protected ledger, as
 * protected_retry_test does: the old endpoint is shut down, the ledger is
 * reopened and claimed, and a new endpoint restores the protected bytes.
 * The native fixtures (the runtime's state) are kept. *c becomes a new
 * negotiated connection of session 0. */
static Hfs9p *
restart_endpoint(Hfs9p *h, Hfs9pLedger **ledger, int directory,
    Hfs9pConnection **c) {
   uint8_t *bytes;
   size_t length;
   assert(!hfs9p_ledger_load(*ledger, &bytes, &length));
   Hfs9pEndpoint endpoint = h->endpoint;
   hfs9p_connection_delete(*c);
   shutdown_endpoint(h);
   hfs9p_ledger_delete(*ledger);
   assert(!hfs9p_ledger_new(ledger, directory, "bridge"));
   assert(hfs9p_ledger_claim(*ledger, &endpoint, (uint8_t[32]){0}) == ESTALE);
   h = calloc(1, sizeof(*h));
   assert(h);
   h->endpoint = endpoint;
   h->verifier = (Hfs9pVerifier){*ledger, claim_cb, protect_cb, verify_cb,
       begin_io_cb, end_io_cb, NULL};
   h->fd[0] = 10;
   h->fd[1] = 11;
   h->fd[2] = 12;
   assert(!_restore_bytes(h, bytes, length));
   assert(!hfs9p_ledger_claim(*ledger, &endpoint, bytes + length - 32));
   free(bytes);
   assert(!pthread_mutex_init(&h->mutex, NULL));
   assert(!pthread_create(&h->worker, NULL, _wait_worker_cb, h));
   assert(!connection_retry(h, h->sessions[0].id, c));
   (*c)->negotiated = true;
   return (h);
}

/* p64 review: a multi-chunk cell whose later chunk got WouldBlock after
 * allocation is pinned. After an exporter restart the restored candidate
 * has uncertain false and only pending marks it. Its replayed first chunk
 * revalidates the complete runtime cell and gets WouldBlock again; it must
 * stay pinned with its number, not be dropped for a later cell to reuse. */
static void
refusal_restart_eagain_test(const char *base) {
   int directory;
   Hfs9pLedger *ledger = refusal_ledger(base, "restart-eagain", &directory);
   Hfs9p *h = new_endpoint_with_ledger(ledger);
   Hfs9pConnection *c = hello_session(h);
   char *body = rewrite_body(large_write, 2, NULL);
   char *scoped = scope_body(c, body);
   refuse_ordinary_errno = EAGAIN;
   refuse_allocated = true;
   refuse_ordinary_from = HFS9P_CHUNK;
   assert(send_body(c, scoped, 2) == 1);
   assert(entry(h, 2)->uncertain && entry(h, 2)->native_cell == 2);
   h = restart_endpoint(h, &ledger, directory, &c);
   Hfs9pEntry *e = entry(h, 2);
   assert(e && !e->uncertain && h->pending == (int)(e - h->entries));
   refuse_ordinary_from = 0;
   assert(fetch_once(c, 2) == 2);
   assert(e->native && e->native_cell == 2 &&
       h->pending == (int)(e - h->entries) && h->high == 1);
   send_query(c, 3);
   assert(!entry(h, 3)->issued);
   refuse_ordinary_errno = 0;
   assert(fetch(c, 2, 2) == 3 && e->issued && e->native_cell == 2);
   assert(fetch(c, 3, 2) == 3 && entry(h, 3)->native_cell == 3);
   assert(rejected_writes == 0);
   free(scoped);
   free(body);
   hfs9p_connection_delete(c);
   shutdown_endpoint(h);
   hfs9p_ledger_delete(ledger);
   assert(!close(directory));
   reset_fixtures();
}

/* e64/p64: EINVAL fails a donor send back on a replay too. An identical
 * resend of an admitted send returns the full length, and authentication
 * never returns Invalid, so a replay EINVAL cannot hide an admission. The
 * donor entry and the native high-water stay. */
static void
refusal_donor_replay_einval_test(void) {
   Hfs9p *h = new_endpoint();
   char ack[128];
   Hfs9pConnection *c = donor_session(h, ack);
   refuse_errno = EIO;
   send_cell(c, golden_create, 3, ack, 1);
   Hfs9pEntry *e = entry(h, 3);
   assert(e && e->uncertain && h->pending == (int)(e - h->entries));
   refuse_errno = EINVAL;
   assert(fetch_once(c, 3) == 4);
   refuse_errno = 0;
   assert(!entry(h, 3) && h->pending == -1 && h->high == 2 &&
       entry(h, 2) && _seen(&h->sessions[0], 3));
   send_cell(c, golden_create, 4, ack, 1);
   assert(entry(h, 4)->issued && entry(h, 4)->native_cell == 3);
   hfs9p_connection_delete(c);
   shutdown_endpoint(h);
   reset_fixtures();
}

int
main(int argc, char **argv) {
   assert(argc == 2);
   controlled_protocol_tests();
   view_protocol_tests();
   parser_tests();
   core_tests();
   routing_cases();
   wait_tests();
   capacity_tests();
   protected_retry_test(argv[1]);
   deadline_tests();
   session_tests();
   contention_tests();
   assembly_accounting_tests();
   order_pinned_writer_test();
   order_concurrent_test();
   order_gap_test();
   order_credit_bypass_test();
   refusal_permanent_test(argv[1]);
   refusal_transient_test(argv[1]);
   refusal_original_permanent_test(argv[1]);
   refusal_original_inserted_test();
   refusal_original_pinned_test(argv[1]);
   refusal_single_chunk_eagain_test();
   refusal_restart_eagain_test(argv[1]);
   refusal_donor_replay_einval_test();
   assert(atomic_load(&io_begins) > 0);
   assert(atomic_load(&io_begins) == atomic_load(&io_ends));
   puts("PASS: C parser/backend transport boundaries (no FUSE/runtime proof)");
   return (0);
}
