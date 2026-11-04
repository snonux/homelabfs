#ifndef _GNU_SOURCE
#define _GNU_SOURCE
#endif
/* SPDX-License-Identifier: BSD-2-Clause
 * Actual supervised child/backend/ledger/recovery carrier. Only native FUSE
 * fstat/pread/pwrite and guest scheduling are portable IPC primitives. */
#include <sys/stat.h>
int restart_fstat(int, struct stat *);
#define fstat restart_fstat
#define pread restart_pread
#define pwrite restart_pwrite
#include "../src/hfs9p.c"
#undef fstat
#undef pread
#undef pwrite
#define _get management_get
#define _put management_put
#define _now management_now
#include "../src/hfs9p_supervisor.c"
#undef _get
#undef _put
#undef _now
#include <assert.h>

static int native_socket;
static pthread_mutex_t native_mutex = PTHREAD_MUTEX_INITIALIZER;

static void
all(int fd, void *buffer, size_t length, bool writing) {
   uint8_t *p = buffer;
   while (length) {
      ssize_t n = writing ? write(fd, p, length) : read(fd, p, length);
      if (n < 0 && errno == EINTR) continue;
      if (n <= 0) exit(0);
      p += n; length -= (size_t)n;
   }
}
static uint8_t *
packet(int fd, uint8_t *method, uint32_t *length) {
   uint8_t header[5]; all(fd, header, 5, false);
   *method = header[0]; *length = (uint32_t)management_get(header + 1, 4);
   assert(*length <= HFS9P_BODY + 32);
   uint8_t *p = calloc(1, *length + 1); assert(p);
   all(fd, p, *length, false); return (p);
}
static void
send_packet(int fd, uint8_t method, const void *body, uint32_t length) {
   uint8_t header[5] = {method}; management_put(header + 1, length, 4);
   all(fd, header, 5, true); if (length) all(fd, (void *)body, length, true);
}
static int
rpc(uint8_t method, uint64_t position, void *buffer, uint32_t length,
    uint32_t *returned) {
   pthread_mutex_lock(&native_mutex);
   uint8_t *request = malloc(12 + length); assert(request);
   management_put(request, position, 8); management_put(request + 8, length, 4);
   uint32_t n = 12;
   if (method == 'W') {memcpy(request + 12, buffer, length); n += length;}
   send_packet(native_socket, method, request, n); free(request);
   uint8_t op; uint32_t size; uint8_t *reply = packet(native_socket, &op, &size);
   assert(op == 'D' && size >= 4);
   int error = (int)management_get(reply, 4);
   *returned = size - 4;
   if (!error && method == 'W') {
      assert(size == 8); *returned = (uint32_t)management_get(reply + 4, 4);
   } else if (!error && method == 'R') {
      assert(size - 4 <= length); memcpy(buffer, reply + 4, size - 4);
   }
   free(reply); pthread_mutex_unlock(&native_mutex); return (error);
}
int
restart_fstat(int fd, struct stat *st) {
   memset(st, 0, sizeof(*st)); st->st_mode = S_IFREG | 0600;
   st->st_ino = fd - 98; st->st_size = HFS9P_VSIZE; return (0);
}
ssize_t
restart_pwrite(int fd, const void *body, size_t length, off_t offset) {
   assert(fd == 100); uint32_t returned;
   int error = rpc('W', (uint64_t)offset, (void *)body, (uint32_t)length, &returned);
   if (error) {errno = error; return (-1);} return ((ssize_t)returned);
}
ssize_t
restart_pread(int fd, void *body, size_t length, off_t offset) {
   assert(fd == 101 || fd == 102); uint32_t returned;
   int error = rpc('R', (uint64_t)offset | ((uint64_t)(fd == 102) << 63), body, (uint32_t)length, &returned);
   if (error) {errno = error; return (-1);} return ((ssize_t)returned);
}
static int
native_check(void *arg, const Hfs9pEndpoint *endpoint, const int fd[3]) {
   (void)arg; (void)endpoint; (void)fd; uint32_t returned;
   return (rpc('V', 0, NULL, 0, &returned));
}
static int
claim(void *arg, const Hfs9pEndpoint *endpoint, const uint8_t digest[32]) {
   Hfs9pSupervisor *s = arg; return (hfs9p_ledger_claim(s->ledger, endpoint, digest));
}
static int
controller(void *arg, const Hfs9pEndpoint *endpoint, const int fd[3], uint8_t id[16]) {
   Hfs9pSupervisor *s = arg;
   int error = native_check(arg, endpoint, fd);
   if (!error) memcpy(id, s->tuple + 40, 16);
   return (error);
}
static int
connect_local(const char *name, uint8_t kind) {
   int fd = socket(AF_UNIX, SOCK_STREAM, 0); assert(fd >= 0);
   struct sockaddr_un a = {0}; a.sun_family = AF_UNIX;
   assert(strlen(name) < sizeof(a.sun_path)); strcpy(a.sun_path, name);
   assert(!connect(fd, (struct sockaddr *)&a, sizeof(a)));
   all(fd, &kind, 1, true); return (fd);
}
static void *
monitor(void *unused) {
   (void)unused; uint8_t byte; (void)read(3, &byte, 1); exit(0);
}
static void
result(int command, int status, const void *body, uint32_t n) {
   uint8_t *p = malloc(n + 4); assert(p); management_put(p, (unsigned)status, 4);
   if (n) memcpy(p + 4, body, n);
   send_packet(command, 'D', p, n + 4); free(p);
}
int
main(int argc, char **argv) {
   assert(argc == 3); int command = connect_local(argv[1], 'C');
   native_socket = connect_local(argv[1], 'N');
   pthread_t guard; assert(!pthread_create(&guard, NULL, monitor, NULL));
   uint8_t op; uint32_t n; uint8_t *initial = packet(command, &op, &n); assert(op == 'I');
   Hfs9pToken *tokens = calloc(HFS9P_TOKENS, sizeof(*tokens)); assert(tokens);
   Hfs9pJson json = {initial, n, tokens, 0}; assert(!hfs9p_json_parse(&json));
   uint32_t contexts, key; assert(!hfs9p_json_key(&json, 0, "contexts", &contexts));
   uint32_t first = contexts + 1;
   Hfs9pEndpoint endpoint = {{0}, {2,3,4}};
   assert(!hfs9p_json_key(&json, first, "fuse_session", &key));
   assert(!_hex(&json, key, endpoint.session, 16, true));
   memcpy(OWNER.tuple, endpoint.session, 16);
   for (unsigned i = 0; i < 3; i++) management_put(OWNER.tuple + 16 + 8*i, endpoint.node[i], 8);
   assert(!hfs9p_json_key(&json, 0, "controller", &key));
   assert(!_hex(&json, key, OWNER.tuple + 40, 16, true));
   int directory = open(argv[2], O_RDONLY | O_DIRECTORY); assert(directory >= 0);
   assert(!hfs9p_ledger_new(&OWNER.ledger, directory, "forwarding")); close(directory);
   int fd[3] = {100,101,102};
   Hfs9pVerifier verifier = {&OWNER,claim,_protect_cb,native_check,NULL,NULL,controller};
   assert(!_construct(&OWNER.backend, fd, &endpoint, verifier));
   for (uint32_t i = first; i < tokens[contexts].next; i = tokens[i].next) {
      assert(tokens[i].type == '{');
      assert(!hfs9p_add_session_tuple(OWNER.backend, initial + tokens[i].start,
          tokens[i].end - tokens[i].start, 4, 32767));
   }
   free(tokens); free(initial); assert(!_recovery_socket());
   pthread_t recovery; assert(!pthread_create(&recovery, NULL, _recovery_cb, &OWNER));
   result(command, 0, NULL, 0);
   Hfs9pConnection *connections[16] = {0};
   for (unsigned i = 0; i < OWNER.backend->session_count; i++) {
      assert(!hfs9p_connection_new(OWNER.backend, OWNER.backend->sessions[i].id, &connections[i]));
      atomic_store(&connections[i]->negotiated, true); /* OS guest-session setup primitive. */
   }
   for (;;) {
      uint8_t *p = packet(command, &op, &n);
      if (op == 'Q') {free(p); return (0);}
      assert(n >= 1 && p[0] < OWNER.backend->session_count);
      unsigned si = p[0]; Hfs9p *h = OWNER.backend;
      uint8_t *answer = malloc(HFS9P_BODY); assert(answer); uint32_t used = 0; int error = 0;
      if (op == 'S') {
         Hfs9pToken *scratch = calloc(HFS9P_TOKENS, sizeof(*scratch)); assert(scratch);
         Hfs9pJson j = {p + 1, n - 1, scratch, 0}; assert(!hfs9p_json_parse(&j));
         uint32_t cell, operation; assert(!hfs9p_json_key(&j,0,"cell",&cell));
         assert(!hfs9p_json_key(&j,0,"op",&operation));
         uint64_t guest; assert(!hfs9p_json_u64(&j,cell,&guest));
         bool wait_op = hfs9p_json_equal(&j,operation,"VIEW_WAIT");
         bool control_op = !(hfs9p_json_equal(&j,operation,"HELLO") || hfs9p_json_equal(&j,operation,"ALLOCATE") || hfs9p_json_equal(&j,operation,"CALL") || hfs9p_json_equal(&j,operation,"VIEW_ACQUIRE") || hfs9p_json_equal(&j,operation,"DIRTY_RESERVE"));
         Hfs9pEntry frame_owner = {.native=p+1,.native_total=n-1,.native_cell=guest,.minor=4};
         _hash(p+1,n-1,frame_owner.native_hash);
         uint8_t wire[HFS9P_MSIZE], reply[HFS9P_MSIZE];
         for (uint32_t offset=0; !error && offset<n-1; offset+=HFS9P_CHUNK) {
            uint32_t length=n-1-offset; if(length>HFS9P_CHUNK)length=HFS9P_CHUNK;
            memset(wire,0,21);_put(wire,21+HFS9P_HEADER+length,4);wire[4]=240;
            _put(wire+5,wait_op?65534:control_op?65533:42,2);wire[7]=1;wire[8]=1;_put(wire+9,guest,8);_put(wire+17,offset/HFS9P_CHUNK,4);
            _make_frame(wire+21,&frame_owner,offset,false);memcpy(wire+21+32,h->sessions[si].id,16);
            size_t got=0;error=hfs9p_exchange(connections[si],wire,21+HFS9P_HEADER+length,reply,sizeof(reply),&got);
            if(!error && (got!=8 || reply[7]!=1))error=EAGAIN;
         }
         free(scratch);
      } else {
         assert(op=='F' && n==9);uint64_t guest=management_get(p+1,8);
         bool wait_op = false;
         pthread_mutex_lock(&h->mutex);
         for (unsigned k=0;k<HFS9P_ENTRIES;k++) if(h->entries[k].guest && h->entries[k].session==si && h->entries[k].guest_cell==guest) wait_op=h->entries[k].wait;
         pthread_mutex_unlock(&h->mutex);
         for(uint32_t chunk=0; !error;chunk++) {
            uint8_t wire[21]={0},reply[HFS9P_MSIZE];_put(wire,21,4);wire[4]=240;_put(wire+5,42,2);wire[7]=2;wire[8]=wait_op?3:2;_put(wire+9,guest,8);_put(wire+17,chunk,4);
            size_t got=0;error=hfs9p_exchange(connections[si],wire,sizeof(wire),reply,sizeof(reply),&got);
            if(!error && (got<8+HFS9P_HEADER || reply[7]!=3))error=EAGAIN;
            if(error)break;
            uint32_t length=(uint32_t)_get(reply+8+24,4),total=(uint32_t)_get(reply+8+16,4);
            assert(used+length<=HFS9P_BODY);memcpy(answer+used,reply+8+HFS9P_HEADER,length);used+=length;
            if(used==total)break;
         }
      }
      result(command,error,answer,error?0:used);free(answer);free(p);
   }
}
