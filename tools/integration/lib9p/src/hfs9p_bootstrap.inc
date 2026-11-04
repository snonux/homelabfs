/* SPDX-License-Identifier: BSD-2-Clause
 * Included only by the source-pinned strong supervisor adapter. */
static atomic_bool BOOTSTRAPPED;
static atomic_bool BOOTSTRAP_ATTEMPTED;

static int
_bootstrap_session(const uint8_t *scope, size_t length, uint8_t session[16]) {
   Hfs9pToken *tokens = calloc(HFS9P_TOKENS, sizeof(*tokens));
   if (!tokens) return (ENOMEM);
   Hfs9pJson json = {scope, length, tokens, 0};
   uint32_t id;
   int error = hfs9p_json_parse(&json) ||
       hfs9p_json_key(&json, 0, "session", &id) ? EINVAL : 0;
   if (!error) {
      Hfs9pToken t = tokens[id];
      const uint8_t *p = scope + t.start + 1;
      if (t.type != '"' || t.end - t.start != 38) error = EINVAL;
      for (unsigned i = 0; !error && i < 16; i++) {
         if (i == 4 || i == 6 || i == 8 || i == 10) p++;
         unsigned value = 0;
         for (unsigned j = 0; j < 2; j++, p++) {
            int digit = *p >= '0' && *p <= '9' ? *p - '0' :
                (*p >= 'a' && *p <= 'f' ? *p - 'a' + 10 : -1);
            if (digit < 0) { error = EINVAL; break; }
            value = value * 16 + (unsigned)digit;
         }
         session[i] = (uint8_t)value;
      }
   }
   free(tokens);
   return (error);
}

static int
_bootstrap_read(uint8_t *bytes, size_t length) {
   int64_t deadline = _now() + 180000;
   while (length) {
      int64_t now = _now();
      if (now < 0 || now >= deadline) return (ETIMEDOUT);
      struct pollfd p = {6, POLLIN, 0};
      int n = poll(&p, 1, (int)(deadline - now));
      if (n < 0 && errno == EINTR) continue;
      if (n <= 0) return (n ? errno : ETIMEDOUT);
      ssize_t amount = read(6, bytes, length);
      if (amount < 0 && errno == EINTR) continue;
      if (amount <= 0) return (EPIPE);
      bytes += amount;
      length -= (size_t)amount;
   }
   return (0);
}

int
hfs9p_supervisor_bootstrap(void) {
   if (BOOTSTRAPPED) return (0);
   if (atomic_exchange(&BOOTSTRAP_ATTEMPTED, true)) return (EALREADY);
   uint8_t header[16];
   int error = _bootstrap_read(header, sizeof(header));
   unsigned count = error ? 0 : (unsigned)_get(header + 4, 2);
   size_t length = error ? 0 : (size_t)_get(header + 12, 4);
   if (!error && (memcmp(header, "HSB1", 4) || count < 1 || count > 16 ||
       _get(header + 6, 2) != 4 || _get(header + 8, 4) != 32767 ||
       length < 3 || length > 16 * 4098)) error = EINVAL;
   uint8_t *bytes = error ? NULL : malloc(length);
   if (!error && !bytes) error = ENOMEM;
   if (!error) error = _bootstrap_read(bytes, length);
   const uint8_t *scopes[16] = {0};
   size_t lengths[16] = {0}, offset = 0;
   uint8_t sessions[16][16] = {{0}};
   for (unsigned i = 0; !error && i < count; i++) {
      if (length - offset < 2) { error = EINVAL; break; }
      size_t size = (size_t)_get(bytes + offset, 2); offset += 2;
      if (!size || size > 4096 || size > length - offset) {
         error = EINVAL; break;
      }
      scopes[i] = bytes + offset; lengths[i] = size; offset += size;
      error = _bootstrap_session(scopes[i], size, sessions[i]);
      for (unsigned j = 0; !error && j < i; j++)
         if (!memcmp(sessions[i], sessions[j], 16)) error = EINVAL;
   }
   if (!error && offset != length) error = EINVAL;
   (void)close(6);
   /* The sole real strong startup consumes inherited parent/name/scope inputs.
    * Exact terminal tuple is explicit, not an old protocol reinterpretation. */
   if (!error) error = hfs9p_supervisor_start_tuple(7, "forwarder",
       scopes[0], lengths[0], 4, 32767);
   Hfs9pSupervisor *s = &OWNER;
   if (!error) {
      pthread_mutex_lock(&s->mutex);
      s->ready = false;
      pthread_mutex_unlock(&s->mutex);
      for (unsigned i = 0; !error && i < count; i++) {
         if (i) error = hfs9p_add_session_tuple(s->backend,
             scopes[i], lengths[i], 4, 32767);
         if (error) break;
         memcpy(s->sessions[i], sessions[i], 16);
         const uint8_t *id = sessions[i];
         (void)snprintf(s->names[i], sizeof(s->names[i]),
             "%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-"
             "%02x%02x%02x%02x%02x%02x", id[0], id[1], id[2], id[3],
             id[4], id[5], id[6], id[7], id[8], id[9], id[10], id[11],
             id[12], id[13], id[14], id[15]);
      }
      pthread_mutex_lock(&s->mutex);
      if (!error) { s->session_count = count; s->ready = true; }
      else { s->fenced = true; (void)shutdown(CONTROL, SHUT_RDWR); }
      pthread_mutex_unlock(&s->mutex);
   }
   (void)close(7);
   free(bytes);
   BOOTSTRAPPED = !error;
   return (error);
}

/* Fixed table and exactly one 128 KiB startup stack, independent of vqueue
 * threads. Pending devices cannot reach stock filesystem callbacks. */
struct Hfs9pPending_ {
   char name[37];
   _Atomic(Hfs9pConnection *) connection;
};
static Hfs9pPending PENDING[16];
static unsigned PENDING_COUNT;
static pthread_mutex_t PENDING_MUTEX = PTHREAD_MUTEX_INITIALIZER;
static bool PENDING_STARTED;
static atomic_bool PENDING_FAILED;

static void *
_bootstrap_thread_cb(void *unused) {
   (void)unused;
   int error = hfs9p_supervisor_bootstrap();
   pthread_mutex_lock(&PENDING_MUTEX);
   if (!error && PENDING_COUNT != OWNER.session_count) error = EINVAL;
   for (unsigned i = 0; !error && i < PENDING_COUNT; i++) {
      Hfs9pConnection *connection = NULL;
      error = hfs9p_controller_claim(PENDING[i].name, &connection);
      if (!error) {
         atomic_store(&PENDING[i].connection, connection);
         pthread_mutex_lock(&OWNER.mutex);
         uint8_t bound[72]; memcpy(bound, OWNER.tuple, 56);
         memcpy(bound + 56, OWNER.sessions[i], 16);
         /* Labels may be registered in a different order from signed scopes. */
         unsigned route = OWNER.session_count;
         for (unsigned j = 0; j < OWNER.session_count; j++)
            if (!strcmp(PENDING[i].name, OWNER.names[j])) route = j;
         if (route == OWNER.session_count) error = EINVAL;
         else { memcpy(bound + 56, OWNER.sessions[route], 16);
            error = _request(&OWNER, 5, bound, sizeof(bound)); }
         pthread_mutex_unlock(&OWNER.mutex);
      }
   }
   if (error) {
      fprintf(stderr, "HomelabFS controlled bootstrap refused: %d\n", error);
      atomic_store(&PENDING_FAILED, true);
      pthread_mutex_lock(&OWNER.mutex);
      OWNER.fenced = true;
      (void)shutdown(CONTROL, SHUT_RDWR);
      pthread_mutex_unlock(&OWNER.mutex);
   }
   pthread_mutex_unlock(&PENDING_MUTEX);
   return (NULL);
}

int
hfs9p_supervisor_prepare(const char *name, Hfs9pPending **out) {
   if (!out) return (EINVAL);
   *out = NULL;
   if (!name || strlen(name) != 36) return (EINVAL);
   for (unsigned i = 0; i < 36; i++) {
      if (i == 8 || i == 13 || i == 18 || i == 23) {
         if (name[i] != '-') return (EINVAL);
      } else if (!((name[i] >= '0' && name[i] <= '9') ||
          (name[i] >= 'a' && name[i] <= 'f'))) return (EINVAL);
   }
   pthread_mutex_lock(&PENDING_MUTEX);
   int error = PENDING_COUNT == 16 || BOOTSTRAPPED ||
       atomic_load(&PENDING_FAILED) ? EACCES : 0;
   for (unsigned i = 0; !error && i < PENDING_COUNT; i++)
      if (!strcmp(name, PENDING[i].name)) error = EEXIST;
   if (!error) {
      Hfs9pPending *p = &PENDING[PENDING_COUNT++];
      strcpy(p->name, name); atomic_init(&p->connection, NULL); *out = p;
      if (!PENDING_STARTED) {
         PENDING_STARTED = true;
         pthread_attr_t attr; bool initialized = false;
         error = pthread_attr_init(&attr); initialized = !error;
         if (!error) error = pthread_attr_setstacksize(&attr, 128 * 1024);
         if (!error) error = pthread_attr_setdetachstate(&attr,
             PTHREAD_CREATE_DETACHED);
         pthread_t thread;
         if (!error) error = pthread_create(&thread, &attr,
             _bootstrap_thread_cb, NULL);
         if (initialized) (void)pthread_attr_destroy(&attr);
         if (error) { atomic_store(&PENDING_FAILED, true); *out = NULL; }
      }
   }
   pthread_mutex_unlock(&PENDING_MUTEX);
   return (error);
}

int
hfs9p_pending_exchange(Hfs9pPending *p, const uint8_t *request, size_t length,
    uint8_t *response, size_t capacity, size_t *returned) {
   if (!p || !request || length < 7 || !response || !returned)
      return (EINVAL);
   Hfs9pConnection *connection = atomic_load(&p->connection);
   bool failed = atomic_load(&PENDING_FAILED);
   if (connection && !failed)
      return (hfs9p_exchange(connection, request, length, response,
          capacity, returned));
   /* Bounded 9P2000.L Rlerror, preserving the original tag. */
   if (capacity < 11) return (EMSGSIZE);
   memset(response, 0, 11); response[0] = 11; response[4] = 7;
   response[5] = request[5]; response[6] = request[6];
   /* 9P2000.L uses Linux errno values, even on the FreeBSD exporter. */
   uint32_t status = failed ? 5u : 11u;
   for (unsigned i = 0; i < 4; i++) response[7 + i] = status >> (8 * i);
   *returned = 11;
   return (0);
}
