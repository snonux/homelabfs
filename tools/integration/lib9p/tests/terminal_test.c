/* SPDX-License-Identifier: BSD-2-Clause
 * Actual owned exporter/sole-writer ledger, transport-fixture native I/O.
 * These tests prove fencing/retry/record refusal, not terminal signature
 * authority, real FUSE, guest physical closure, or successful native handoff.
 */
#define main baseline_backend_main
#include "backend_test.c"
#undef main
#include "terminal_fixtures.h"

static uint8_t terminal_controller = 42;

static int
terminal_controller_cb(void *arg, const Hfs9pEndpoint *endpoint,
    const int fd[3], uint8_t controller[16]) {
   (void)arg;
   (void)endpoint;
   (void)fd;
   memset(controller, 0, 16);
   controller[15] = terminal_controller;
   return (0);
}

static int
terminal_protect_cb(void *arg, const uint8_t *bytes, size_t length) {
   assert(length > 64 && !memcmp(bytes, "H9LEDG3\0", 8));
   protects++;
   return (fail_protect ? EIO : hfs9p_ledger_protect(arg, bytes, length));
}

static char *
terminal_body(Hfs9p *h, unsigned session, uint64_t cell, uint64_t through,
    const char *ack) {
   Hfs9pJson j = {(const uint8_t *)controlled_allocate,
      strlen(controlled_allocate), h->tokens, 0};
   uint32_t payload, credentials;
   assert(!hfs9p_json_parse(&j));
   assert(!hfs9p_json_key(&j, 0, "payload", &payload));
   assert(!hfs9p_json_key(&j, payload, "credentials", &credentials));
   Hfs9pToken c = j.tokens[credentials];
   char *body = malloc(16384);
   assert(body);
   int n = snprintf(body, 16384,
       "{\"cell\":%llu,\"context\":%.*s,\"delegation\":null,"
       "\"op\":\"SESSION_RETIRE\",\"payload\":{\"closure_hash\":\"%064u\","
       "\"credentials\":%.*s,\"guest_through\":%llu,"
       "\"observer_revision\":1,"
       "\"retirement_id\":\"00000000-0000-0000-0000-000000000099\",\"settled_cells\":[]},"
       "\"protocol\":%s,\"reclaim\":%s}",
       (unsigned long long)cell, (int)h->sessions[session].length,
       h->sessions[session].context, 1u, (int)(c.end - c.start),
       j.bytes + c.start, (unsigned long long)through,
       _protocol_tuple(4, h->sessions[session].features), ack);
   assert(n > 0 && n < 16384);
   return (body);
}

static Hfs9pConnection *
terminal_connection(Hfs9p *h, uint32_t features) {
   Hfs9pJson j = {(const uint8_t *)controlled_hello,
      strlen(controlled_hello), h->tokens, 0};
   uint32_t context;
   uint8_t id[16];
   assert(!hfs9p_json_parse(&j));
   assert(!hfs9p_json_key(&j, 0, "context", &context));
   assert(!_context(&j, context, id));
   Hfs9pToken c = j.tokens[context];
   h->verifier.controller_cb = terminal_controller_cb;
   assert(!hfs9p_add_session_tuple(h, j.bytes + c.start,
       c.end - c.start, 4, features));
   Hfs9pConnection *connection;
   assert(!hfs9p_connection_new(h, id, &connection));
   /* Routing fixture only: no claim that this is real HELLO provenance. */
   h->sessions[0].hello = true;
   atomic_store(&connection->negotiated, true);
   return (connection);
}

static void
pending_test(const char *directory) {
   assert(!mkdir(directory, 0700));
   int fd = open(directory, O_RDONLY | O_DIRECTORY);
   assert(fd >= 0);
   Hfs9pLedger *ledger;
   assert(!hfs9p_ledger_new(&ledger, fd, "terminal"));
   Hfs9p *h = new_endpoint_with_ledger(ledger);
   h->verifier.protect_cb = terminal_protect_cb;
   Hfs9pConnection *c = terminal_connection(h, 32767);
   Hfs9pConnection *other;
   assert(!hfs9p_connection_new(h, h->sessions[0].id, &other));
   atomic_store(&other->negotiated, true);
   char *body = terminal_body(h, 0, 1, 0, "null");
   lose_receipt = true;
   assert(send_body(c, body, 1) == 1);
   Hfs9pEntry *e = entry(h, 1);
   assert(e && e->terminal && !e->issued && e->uncertain);
   assert(h->sessions[0].terminal_phase == 1 && h->pending >= 0);
   uint64_t native_cell = e->native_cell;
   uint8_t original_hash[32], native_hash[32];
   memcpy(original_hash, e->guest_hash, 32);
   memcpy(native_hash, e->native_hash, 32);
   uint8_t *bytes;
   size_t length;
   assert(!hfs9p_ledger_load(ledger, &bytes, &length));
   assert(!hfs9p_terminal_evidence_check(&h->endpoint, bytes, length));
   Hfs9p *decoded = calloc(1, sizeof(*decoded));
   assert(decoded);
   decoded->endpoint = h->endpoint;
   assert(!_restore_bytes(decoded, bytes, length));
   assert(decoded->sessions[0].terminal_channel == NULL);
   Hfs9pEntry *staged = &decoded->entries[(unsigned)decoded->pending];
   uint8_t *saved_native = staged->native;
   uint64_t saved_cell = staged->native_cell;
   uint32_t saved_total = staged->native_total;
   int saved_pending = decoded->pending;
   staged->native = NULL;
   staged->native_cell = 0;
   staged->native_total = 0;
   staged->uncertain = false;
   decoded->pending = -1;
   decoded->sessions[0].terminal_phase = 0;
   decoded->sessions[0].terminal_guest = 0;
   decoded->sessions[0].terminal_native = 0;
   assert(!_terminal_records(decoded)); /* OPEN capacity-staged assembly. */
   staged->native = saved_native;
   staged->native_cell = saved_cell;
   staged->native_total = saved_total;
   staged->uncertain = true;
   decoded->pending = saved_pending;
   decoded->sessions[0].terminal_phase = 1;
   decoded->sessions[0].terminal_guest = staged->guest_cell;
   decoded->sessions[0].terminal_native = saved_cell;
   decoded->entries[(unsigned)decoded->pending].terminal = false;
   assert(_terminal_records(decoded) == EINVAL); /* Missing candidate tag. */
   decoded->entries[(unsigned)decoded->pending].terminal = true;
   decoded->sessions[0].terminal_phase = 0;
   assert(_terminal_records(decoded) == EINVAL); /* Missing fence. */
   decoded->sessions[0].terminal_phase = 3;
   assert(_terminal_records(decoded) == EINVAL); /* No final response. */
   decoded->sessions[0].terminal_phase = 1;
   decoded->sessions[0].terminal_native++;
   assert(_terminal_records(decoded) == EINVAL);
   _discard_recovery(decoded);
   free(bytes);
   /* An existing alternate connection cannot take over the pending owner. */
   uint8_t packet[21] = {0}, reply[HFS9P_MSIZE];
   size_t reply_length;
   _put(packet, sizeof(packet), 4);
   packet[4] = 240;
   _put(packet + 5, 1, 2);
   packet[7] = 2;
   packet[8] = 2;
   _put(packet + 9, 1, 8);
   assert(hfs9p_exchange(other, packet, sizeof(packet), reply,
       sizeof(reply), &reply_length) == 0);
   assert(reply[7] == 4);
   hfs9p_connection_delete(other);
   assert(hfs9p_connection_new(h, h->sessions[0].id, &other) == EACCES);
   assert(send_body(c, body, 1) == 1);
   assert(e->issued && e->native_cell == native_cell && h->high == native_cell);
   assert(!memcmp(original_hash, e->guest_hash, 32));
   assert(!memcmp(native_hash, e->native_hash, 32));
   assert(h->control == 1 && h->ordinary == 0);
   char *fresh = terminal_body(h, 0, 2, 1, "null");
   assert(send_body(c, fresh, 2) == 4);
   assert(h->sessions[0].high == 1 && h->high == native_cell);
   /* Fixture's ordinary RELEASED payload cannot authorize terminal exposure. */
   assert(fetch(c, 1, 2) == 4);
   assert(h->sessions[0].terminal_phase == 1 && h->control == 1);
   free(fresh);
   free(body);
   hfs9p_connection_delete(c);
   shutdown_endpoint(h);
   hfs9p_ledger_delete(ledger);
   close(fd);
   reset_fixtures();
}

static void
old_tuple_test(void) {
   Hfs9p *h = new_endpoint();
   Hfs9pConnection *c = terminal_connection(h, 16383);
   char *body = terminal_body(h, 0, 1, 0, "null");
   assert(send_body(c, body, 1) == 4);
   assert(!writes && !protects && !h->sessions[0].terminal_phase);
   assert(strstr(_protocol(4), "\"features\":16383"));
   assert(!strcmp(_protocol(4), _protocol_tuple(4, 16383)));
   assert(hfs9p_add_session_tuple(h, h->sessions[0].context,
       h->sessions[0].length, 4, 65535) == EINVAL);
   free(body);
   hfs9p_connection_delete(c);
   shutdown_endpoint(h);
   reset_fixtures();
}

static void
install_native_reply(Hfs9p *h, uint64_t guest_cell, const char *expected,
    const char *response) {
   Hfs9pEntry *e = entry(h, guest_cell);
   assert(e && e->native && e->native_cell < 512);
   assert(e->native_total == strlen(expected));
   assert(!memcmp(e->native, expected, e->native_total));
   Fixture *f = &fixtures[e->native_cell];
   free(f->reply);
   f->reply = (uint8_t *)strdup(response);
   assert(f->reply);
   f->reply_total = (uint32_t)strlen(response);
}

static void
negative_manifest_checks(Hfs9p *h) {
   pthread_mutex_lock(&h->mutex);
   uint64_t high = h->sessions[0].high;
   h->sessions[0].high = 81;
   Hfs9pEntry candidate = {.guest_cell = 81, .guest_watermark = 80,
      .session = 0, .minor = 4, .donor = -1};
   unsigned before = writes;
   for (unsigned i = 0; i < sizeof(terminal_bad_guest) /
       sizeof(terminal_bad_guest[0]); i++) {
      candidate.guest = (uint8_t *)terminal_bad_guest[i];
      candidate.total = (uint32_t)strlen(terminal_bad_guest[i]);
      assert(_command(h, &candidate));
   }
   candidate.guest = (uint8_t *)terminal_full_guest;
   candidate.total = (uint32_t)strlen(terminal_full_guest);
   Hfs9pEntry *old = entry(h, 1);
   uint32_t fetched = old->fetched;
   old->fetched = 0;
   assert(_command(h, &candidate) == EACCES);
   old->fetched = fetched;
   old->wait = true;
   assert(_command(h, &candidate) == EACCES);
   old->wait = false;
   old->issued = false;
   assert(_command(h, &candidate) == EACCES);
   old->issued = true;
   assert(!_command(h, &candidate));
   assert(writes == before);
   h->sessions[0].high = high;
   pthread_mutex_unlock(&h->mutex);
}

static void
package_counter_negatives(const Hfs9pEndpoint *endpoint, uint8_t *bytes,
    size_t length) {
   Hfs9pReader reader = {bytes, length - 32, 61, 0};
   (void)_unpack_bytes(&reader, 16);
   uint32_t context = (uint32_t)_unpack(&reader, 4);
   (void)_unpack_bytes(&reader, context);
   (void)_unpack(&reader, 8);
   unsigned ranges = (unsigned)_unpack(&reader, 2);
   (void)_unpack_bytes(&reader, ranges * 16 + 1 + 1 + 4 + 16);
   size_t offset = reader.cursor;
   uint8_t original[12];
   memcpy(original, bytes + offset, sizeof(original));
   _put(bytes + offset, 0, 4); /* Missing permanent reservation. */
   _hash(bytes, length - 32, bytes + length - 32);
   assert(hfs9p_terminal_evidence_check(endpoint, bytes, length) == EINVAL);
   memcpy(bytes + offset, original, sizeof(original));
   _put(bytes + offset + 4, 0, 4);
   _put(bytes + offset + 8, sizeof(Hfs9pTerminalPackage), 4);
   _hash(bytes, length - 32, bytes + length - 32);
   assert(hfs9p_terminal_evidence_check(endpoint, bytes, length) == EINVAL);
   memcpy(bytes + offset, original, sizeof(original));
   _put(bytes + offset, sizeof(Hfs9pTerminalPackage) * 2, 4);
   _hash(bytes, length - 32, bytes + length - 32);
   assert(hfs9p_terminal_evidence_check(endpoint, bytes, length) == EINVAL);
   memcpy(bytes + offset, original, sizeof(original));
   _hash(bytes, length - 32, bytes + length - 32);
   assert(!hfs9p_terminal_evidence_check(endpoint, bytes, length));
}

static void
manifest_case(const char *directory, unsigned count, const char *command,
    const char *native, const char *reply, bool lose_final) {
   assert(!mkdir(directory, 0700));
   int directory_fd = open(directory, O_RDONLY | O_DIRECTORY);
   assert(directory_fd >= 0);
   Hfs9pLedger *ledger;
   assert(!hfs9p_ledger_new(&ledger, directory_fd, "terminal"));
   Hfs9p *h = new_endpoint_with_ledger(ledger);
   h->verifier.protect_cb = terminal_protect_cb;
   Hfs9pConnection *c = terminal_connection(h, 32767);
   if (!count)
      h->sessions[0].hello = false; /* Exact clean never-HELLO exception. */
   for (unsigned i = 0; i < count; i++) {
      assert(send_body(c, terminal_old_guest[i], i + 1) == 1);
      install_native_reply(h, i + 1, terminal_old_native[i], terminal_old_reply[i]);
      assert(fetch(c, i + 1, 2) == 3);
   }
   unsigned ordinary = count < 64 ? count : 64;
   unsigned control = count - ordinary;
   assert(h->ordinary == ordinary && h->control == control);
   uint64_t cell = count + 1;
   if (count == 80)
      negative_manifest_checks(h);
   assert(send_body(c, command, cell) == 1);
   Hfs9pEntry *e = entry(h, cell);
   assert(e && e->issued && e->terminal_state->count == count);
   assert(h->ordinary == ordinary && h->control == control + !count);
   for (unsigned i = 0; i < count; i++)
      assert(entry(h, i + 1)); /* Input ACK cannot transfer old ownership. */
   uint8_t *bytes;
   size_t length;
   assert(!hfs9p_ledger_load(ledger, &bytes, &length));
   assert(!hfs9p_terminal_evidence_check(&h->endpoint, bytes, length));
   free(bytes);
   install_native_reply(h, cell, native, reply);
   fail_protect = lose_final;
   unsigned status = fetch(c, cell, 2);
   if (lose_final) {
      assert(status == 4 && h->revoked);
      assert(!e->terminal_state->removed && h->sessions[0].terminal_phase == 1);
      assert(h->ordinary == ordinary && h->control == control + !count);
      for (unsigned i = 0; i < count; i++)
         assert(entry(h, i + 1));
   } else {
      assert(status == 3 && h->sessions[0].terminal_phase == 3);
      assert(e->terminal_state->removed);
      assert(h->ordinary == (unsigned)!e->control && h->control == e->control);
      for (unsigned i = 0; i < count; i++)
         assert(!entry(h, i + 1));
      unsigned protected = protects;
      uint64_t high = h->high;
      assert(fetch(c, cell, 2) == 3);
      assert(send_body(c, command, cell) == 1);
      assert(h->high == high && protects == protected);
      assert(e->response_total == strlen(reply));
      assert(!memcmp(e->response_guest, reply, e->response_total));
      Hfs9pConnection *other = connection(h, golden_write);
      char hello[8192];
      int n = snprintf(hello, sizeof(hello),
          "{\"cell\":1,\"context\":%.*s,\"op\":\"HELLO\","
          "\"payload\":%s,\"reclaim\":null}",
          (int)h->sessions[other->session].length,
          h->sessions[other->session].context, _protocol(0));
      assert(n > 0 && (size_t)n < sizeof(hello));
      assert(send_body(other, hello, 1) == 1 && fetch(other, 1, 2) == 3);
      assert(h->high == high + 1 && e->native_cell == high);
      hfs9p_connection_delete(other);
   }
   assert(!hfs9p_ledger_load(ledger, &bytes, &length));
   assert(!hfs9p_terminal_evidence_check(&h->endpoint, bytes, length));
   Hfs9p *decoded = calloc(1, sizeof(*decoded));
   assert(decoded);
   decoded->endpoint = h->endpoint;
   assert(!_restore_bytes(decoded, bytes, length));
   if (!count)
      package_counter_negatives(&h->endpoint, bytes, length);
   Hfs9pEntry *final = NULL;
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      if (decoded->entries[i].guest && decoded->entries[i].terminal)
         final = &decoded->entries[i];
   }
   assert(final && final->terminal_state && final->terminal_state->count == count);
   if (count) {
      final->terminal_state->count--;
      assert(_terminal_records(decoded) == EINVAL); /* Partial old cut. */
      final->terminal_state->count++;
      final->terminal_state->old[final->terminal_state->funding].control ^= 1;
      assert(_terminal_records(decoded) == EINVAL); /* Lost funding class. */
      final->terminal_state->old[final->terminal_state->funding].control ^= 1;
   }
   if (!lose_final) {
      /* Even self-consistent full hashes cannot hide changed guest routing. */
      assert(final->response_guest[8] >= '1' && final->response_guest[8] <= '9');
      final->response_guest[8] = final->response_guest[8] == '9' ? '8' : '9';
      _hash(final->response_guest, final->response_total, final->response_guest_hash);
      assert(_terminal_records(decoded) == EINVAL);
   }
   _discard_recovery(decoded);
   free(bytes);
   hfs9p_connection_delete(c);
   shutdown_endpoint(h);
   hfs9p_ledger_delete(ledger);
   close(directory_fd);
   reset_fixtures();
}

static void
maximum_package_test(void) {
   assert(strlen(terminal_maximum_guest) == HFS9P_TERMINAL_GUEST);
   assert(strlen(terminal_maximum_native) == HFS9P_TERMINAL_NATIVE);
   assert(strlen(terminal_maximum_reply) == HFS9P_TERMINAL_REPLY);
   assert(sizeof(Hfs9pTerminalPackage) >= 73743 + sizeof(Hfs9pTerminal));
   assert(sizeof(Hfs9pTerminal) <= 32768);
   printf("terminal package %zu bytes: guest%u native%u replies%u+%u records%zu\n",
       sizeof(Hfs9pTerminalPackage), HFS9P_TERMINAL_GUEST,
       HFS9P_TERMINAL_NATIVE, HFS9P_TERMINAL_REPLY,
       HFS9P_TERMINAL_REPLY, sizeof(Hfs9pTerminal));
}

static void
pressure_test(const char *directory) {
   fixture_read_limit = HFS9P_CHUNK;
   assert(!mkdir(directory, 0700));
   int fd = open(directory, O_RDONLY | O_DIRECTORY);
   assert(fd >= 0);
   Hfs9pLedger *ledger;
   assert(!hfs9p_ledger_new(&ledger, fd, "terminal"));
   Hfs9p *h = new_endpoint_with_ledger(ledger);
   h->verifier.protect_cb = terminal_protect_cb;
   Hfs9pConnection *c = terminal_connection(h, 32767);
   assert(h->sessions[0].terminal_package && !_terminal_usage(h, 0));
   assert(send_body(c, terminal_old_guest[0], 1) == 1);
   install_native_reply(h, 1, terminal_old_native[0], terminal_old_reply[0]);
   assert(fetch(c, 1, 2) == 3);
   Hfs9pEntry *funding = entry(h, 1);
   size_t old_bytes = funding->total + funding->native_total +
       funding->response_total + funding->native_response_total;
   Hfs9pConnection *other = connection(h, golden_write);
   h->sessions[other->session].hello = true; /* Other-scope routing fixture. */
   for (unsigned i = 0; i < terminal_pressure_rows; i++) {
      Hfs9pJson j = {(const uint8_t *)terminal_pressure_ordinary,
         strlen(terminal_pressure_ordinary), h->tokens, 0};
      uint32_t cell;
      assert(!hfs9p_json_parse(&j));
      assert(!hfs9p_json_key(&j, 0, "cell", &cell));
      char number[24];
      int n = snprintf(number, sizeof(number), "%u", i + 1);
      Hfs9pReplacement replacement = {j.tokens[cell].start,
         j.tokens[cell].end, (uint8_t *)number, (size_t)n};
      uint8_t *command;
      uint32_t length;
      assert(!_rewrite(&j, &replacement, 1, &command, &length));
      /* send_body consumes a C string; this is test input, never backend state. */
      char *terminated = malloc((size_t)length + 1);
      assert(terminated);
      memcpy(terminated, command, length);
      terminated[length] = 0;
      free(command);
      unsigned status = send_body(other, terminated, i + 1);
      if (status != 1)
         fprintf(stderr, "pressure setup i%u status%u ordinary%u control%u high%llu total%u\n",
             i, status, h->ordinary, h->control, (unsigned long long)h->high, length);
      assert(status == 1);
      free(terminated);
      Hfs9pEntry *current = NULL;
      for (unsigned k = 0; k < HFS9P_ENTRIES; k++) {
         Hfs9pEntry *entry = &h->entries[k];
         if (entry->guest && entry->session == other->session && entry->guest_cell == i + 1)
            current = entry;
      }
      assert(current && current->native_cell == i + 2);
      Hfs9pJson response = {(const uint8_t *)terminal_pressure_ordinary_reply,
         strlen(terminal_pressure_ordinary_reply), h->tokens, 0};
      assert(!hfs9p_json_parse(&response));
      assert(!hfs9p_json_key(&response, 0, "cell", &cell));
      n = snprintf(number, sizeof(number), "%llu", (unsigned long long)current->native_cell);
      replacement = (Hfs9pReplacement){response.tokens[cell].start,
         response.tokens[cell].end, (uint8_t *)number, (size_t)n};
      assert(!_rewrite(&response, &replacement, 1, &command, &length));
      Fixture *native = &fixtures[current->native_cell];
      free(native->reply);
      native->reply = command;
      native->reply_total = length;
      assert(fetch(other, i + 1, 2) == 3);
   }
   assert(h->ordinary == terminal_pressure_rows + 1 && h->high == terminal_pressure_rows + 1);
   unsigned headers = 0;
   while (_memory(h, 1, true)) {
      uint32_t low = 0, high = HFS9P_BODY;
      while (low < high) {
         uint32_t mid = low + (high - low + 1) / 2;
         if (_memory(h, mid, true))
            low = mid;
         else
            high = mid - 1;
      }
      assert(low > HFS9P_CHUNK && headers < 16);
      uint8_t frame[HFS9P_HEADER + HFS9P_CHUNK] = {0};
      memcpy(frame, "HFS9BR1\0", 8);
      _put(frame + 8, 1, 2);
      _put(frame + 12, 1, 2);
      _put(frame + 16, low, 4);
      _put(frame + 24, HFS9P_CHUNK, 4);
      memcpy(frame + 32, h->sessions[other->session].id, 16);
      _put(frame + 48, 100 + headers, 8);
      assert(!_send(h, other->session, 65533, frame, sizeof(frame)));
      headers++;
   }
   assert(headers && !_memory(h, 1, true) && _memory(h, 0, true));
   /* No byte of the physically owned terminal package was spent by headers. */
   assert(!_terminal_usage(h, 0) && h->control == 0);
   unsigned sessions = h->session_count;
   Hfs9pJson context = {h->sessions[0].context, h->sessions[0].length, h->tokens, 0};
   uint32_t session;
   assert(!hfs9p_json_parse(&context));
   assert(!hfs9p_json_key(&context, 0, "session", &session));
   const char uuid[] = "\"00000000-0000-0000-0000-0000000000ee\"";
   Hfs9pReplacement replacement = {context.tokens[session].start,
      context.tokens[session].end, (const uint8_t *)uuid, sizeof(uuid) - 1};
   uint8_t *extra_scope;
   uint32_t extra_length;
   assert(!_rewrite(&context, &replacement, 1, &extra_scope, &extra_length));
   assert(hfs9p_add_session_tuple(h, extra_scope, extra_length, 4, 32767) == ENOSPC);
   free(extra_scope);
   assert(h->session_count == sessions && !h->sessions[sessions].context &&
       !h->sessions[sessions].terminal_package);
   /* Ordinary funding class retains its count credit; all semantic buffers
    * come from preowned control bytes despite both global memory limits. */
   assert(send_body(c, terminal_pressure_guest, 2) == 1);
   Hfs9pEntry *final = NULL;
   for (unsigned i = 0; i < HFS9P_ENTRIES; i++) {
      if (h->entries[i].guest && h->entries[i].terminal)
         final = &h->entries[i];
   }
   assert(final && final->terminal_funded && !final->control);
   assert(final->native_cell == terminal_pressure_rows + 2 && final->terminal_state->count == 1);
   Fixture *native = &fixtures[terminal_pressure_rows + 2];
   assert(final->native_total == strlen(terminal_pressure_native));
   assert(!memcmp(final->native, terminal_pressure_native, final->native_total));
   free(native->reply);
   native->reply = (uint8_t *)strdup(terminal_pressure_reply);
   assert(native->reply);
   native->reply_total = (uint32_t)strlen(terminal_pressure_reply);
   assert(fetch(c, 2, 2) == 3);
   assert(final->response_total == strlen(terminal_pressure_guest_reply));
   assert(!memcmp(final->response_guest, terminal_pressure_guest_reply,
       final->response_total));
   assert(h->ordinary == terminal_pressure_rows + 1 && h->control == 0 &&
       h->high == terminal_pressure_rows + 2);
   /* Final deletion releases precisely the old donor bodies. The entire
    * terminal package remains charged, including its permanently unused pad. */
   assert(_memory(h, old_bytes, true) && !_memory(h, old_bytes + 1, true));
   assert(_terminal_usage(h, 0) == 73743 + sizeof(Hfs9pTerminal));
   uint8_t *bytes;
   size_t length;
   assert(!hfs9p_ledger_load(ledger, &bytes, &length));
   assert(!hfs9p_terminal_evidence_check(&h->endpoint, bytes, length));
   Hfs9p *decoded = calloc(1, sizeof(*decoded));
   assert(decoded);
   decoded->endpoint = h->endpoint;
   assert(!_restore_bytes(decoded, bytes, length));
   Hfs9pTerminalPackage *package = decoded->sessions[0].terminal_package;
   decoded->sessions[0].terminal_package = NULL;
   assert(_terminal_records(decoded) == EINVAL);
   decoded->sessions[0].terminal_package = package;
   _discard_recovery(decoded);
   free(bytes);
   hfs9p_connection_delete(c);
   hfs9p_connection_delete(other);
   shutdown_endpoint(h);
   hfs9p_ledger_delete(ledger);
   close(fd);
   reset_fixtures();
}

static void
controller_binding_test(void) {
   Hfs9p *h = new_endpoint();
   Hfs9pJson j = {(const uint8_t *)controlled_hello,
      strlen(controlled_hello), h->tokens, 0};
   uint32_t context;
   assert(!hfs9p_json_parse(&j));
   assert(!hfs9p_json_key(&j, 0, "context", &context));
   Hfs9pToken token = j.tokens[context];
   assert(hfs9p_add_session_tuple(h, j.bytes + token.start,
       token.end - token.start, 4, 32767) == EACCES);
   h->verifier.controller_cb = terminal_controller_cb;
   terminal_controller = 0;
   assert(hfs9p_add_session_tuple(h, j.bytes + token.start,
       token.end - token.start, 4, 32767) == EACCES);
   terminal_controller = 42;
   assert(!hfs9p_add_session_tuple(h, j.bytes + token.start,
       token.end - token.start, 4, 32767));
   terminal_controller = 43;
   assert(_verify(h) == ESTALE && h->revoked);
   terminal_controller = 42;
   shutdown_endpoint(h);
   reset_fixtures();
}

int
main(int argc, char **argv) {
   assert(argc == 2 || argc == 3);
   if (argc == 3) {
      pressure_test(argv[1]);
      return (0);
   }
   maximum_package_test();
   controller_binding_test();
   old_tuple_test();
   char path[4096];
   snprintf(path, sizeof(path), "%s-pending", argv[1]);
   pending_test(path);
   snprintf(path, sizeof(path), "%s-empty", argv[1]);
   manifest_case(path, 0, terminal_empty_guest, terminal_empty_native,
       terminal_empty_reply, false);
   snprintf(path, sizeof(path), "%s-sole", argv[1]);
   manifest_case(path, 1, terminal_sole_guest, terminal_sole_native,
       terminal_sole_reply, false);
   snprintf(path, sizeof(path), "%s-ordinary", argv[1]);
   manifest_case(path, 65, terminal_ordinary_guest, terminal_ordinary_native,
       terminal_ordinary_reply, false);
   snprintf(path, sizeof(path), "%s-full", argv[1]);
   manifest_case(path, 80, terminal_full_guest, terminal_full_native,
       terminal_full_reply, false);
   snprintf(path, sizeof(path), "%s-uncertain", argv[1]);
   manifest_case(path, 80, terminal_full_guest, terminal_full_native,
       terminal_full_reply, true);
   snprintf(path, sizeof(path), "%s-pressure", argv[1]);
   pressure_test(path);
   puts("PASS: owned terminal empty/mixed/80-cell pending/final/retry/ledger fences; fixture native I/O, invalid signatures");
   return (0);
}
