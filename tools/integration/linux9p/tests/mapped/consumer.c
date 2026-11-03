// SPDX-License-Identifier: GPL-2.0-only
/* Actual owned carrier and guest pure verifier. Native fd observations and
 * scheduling are explicit portable primitives, not native runtime evidence. */
#include <sys/stat.h>
int primitive_fstat(int, struct stat *);
#define fstat primitive_fstat
#define pread primitive_pread
#define pwrite primitive_pwrite
#include "tools/integration/lib9p/src/hfs9p.c"
#undef fstat
#undef pread
#undef pwrite
#include "tools/integration/lib9p/src/hfs9p_ledger.h"
#include "hfs_protocol.h"
#include <openssl/evp.h>
#include <assert.h>
unsigned int hfs_test_alloc_call, hfs_test_alloc_fail;
size_t hfs_test_last_allocation_size;

static char *read_line(void) {
   char *p = NULL;
   size_t n = 0;
   ssize_t z = getline(&p, &n, stdin);
   assert(z > 0);
   if (p[z - 1] == '\n') p[z - 1] = 0;
   return p;
}
static void emit_hex(const void *raw, size_t n) {
   const u8 *p = raw;
   for (size_t i = 0; i < n; i++) printf("%02x", p[i]);
}
static size_t decode_hex(const char *raw, void *out, size_t cap) {
   size_t n = strlen(raw) / 2;
   assert(strlen(raw) % 2 == 0 && n <= cap);
   u8 *p = out;
   for (size_t i = 0; i < n; i++) {
      int a = _hex_digit(raw[2*i]), b = _hex_digit(raw[2*i+1]);
      assert(a >= 0 && b >= 0);
      p[i] = a * 16 + b;
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
ssize_t primitive_pwrite(int fd, const void *raw, size_t n, off_t offset) {
   assert(fd == 10);
   printf("W %llu ", (unsigned long long)offset);
   emit_hex(raw, n); puts(""); fflush(stdout);
   char *line = read_line();
   ssize_t value = -1;
   if (!strncmp(line, "OK ", 3)) value = strtoll(line + 3, NULL, 10);
   else errno = EIO;
   free(line);
   static bool lost;
   if (value >= 0 && !lost) { lost = true; errno = EIO; return -1; }
   return value;
}
ssize_t primitive_pread(int fd, void *raw, size_t n, off_t offset) {
   printf("R %d %llu %zu\n", fd, (unsigned long long)offset, n);
   fflush(stdout);
   char *line = read_line();
   ssize_t value = -1;
   if (!strncmp(line, "DATA ", 5)) value = decode_hex(line + 5, raw, n);
   else errno = EAGAIN;
   free(line);
   return value;
}
static int primitive_verify(void *a, const Hfs9pEndpoint *e, const int fd[3]) {
   (void)a; (void)e; (void)fd; return 0;
}
static int claim_cb(void *a, const Hfs9pEndpoint *e, const u8 hash[32]) {
   return hfs9p_ledger_claim(a, e, hash);
}
static int protect_cb(void *a, const u8 *p, size_t n) {
   return hfs9p_ledger_protect(a, p, n);
}
static int verify_ed25519(const u8 key[32], const void *message, u32 len,
    const u8 signature[64]) {
   EVP_PKEY *pkey = EVP_PKEY_new_raw_public_key(EVP_PKEY_ED25519, NULL, key, 32);
   EVP_MD_CTX *ctx = EVP_MD_CTX_new();
   int result = -EACCES;
   if (pkey && ctx && EVP_DigestVerifyInit(ctx, NULL, NULL, NULL, pkey) == 1 &&
       EVP_DigestVerify(ctx, signature, 64, message, len) == 1) result = 0;
   EVP_MD_CTX_free(ctx); EVP_PKEY_free(pkey);
   return result;
}
static void copy_json(struct hfs_json *j, int object, const char *name,
    struct hfs_json **out) {
   int t = hfs_json_field(j, object, name);
   assert(t >= 0);
   assert(!hfs_json_parse(j->data + j->tokens[t].start,
       j->tokens[t].end - j->tokens[t].start, out));
}
#include "builder.inc"
static void check_builder(char *line) {
   struct hfs_json *j=NULL,*original=NULL,*context=NULL,*payload=NULL;
   assert(!hfs_json_parse(line,strlen(line),&j));
   copy_json(j,0,"original",&original);
   copy_json(original,0,"context",&context);
   copy_json(original,0,"payload",&payload);
   u64 minor,cell;
   assert(!hfs_json_u64(j,hfs_json_field(j,0,"minor"),&minor));
   assert(!hfs_json_u64(original,hfs_json_field(original,0,"cell"),&cell));
   struct builder_session session={.bridge_minor=minor,
       .enrollment={.context=context->data,.context_len=context->len}};
   struct builder_slot slot={0};
   int ack=hfs_json_field(original,0,"reclaim");
   if (!hfs_json_is(original,ack,"null")) {
      slot.ack_ready=true;u32 n=32;
      assert(!hfs_json_u64(original,hfs_json_field(original,ack,"cell"),&slot.number));
      assert(!hfs_json_hex(original,hfs_json_field(original,ack,"response_hash"),slot.hash,&n));
   }
   struct hfs_buf body={.data=payload->data,.len=payload->len},out={0};
   assert(!hfs_buf_init(&out,HFS_BODY_MAX));
   char op[32];
   assert(hfs_json_string(original,hfs_json_field(original,0,"op"),op,sizeof(op))>0);
   production_build(&session,&slot,cell,op,&body,&out);
   assert(!out.error && out.len==original->len && !memcmp(out.data,original->data,out.len));
   hfs_buf_free(&out);hfs_json_free(j);hfs_json_free(original);
   hfs_json_free(context);hfs_json_free(payload);puts("D 0");
}
static void verification(char *line) {
   struct hfs_json *j = NULL, *original = NULL, *response = NULL, *context = NULL;
   assert(!hfs_json_parse(line, strlen(line), &j));
   /* Harness IPC uses separate bounded bodies, never an oversized aggregate
    * command/response JSON envelope that would bypass the1MiB parser bound. */
   u8 *raw=malloc(HFS_BODY_MAX);assert(raw);
   char *body=read_line();u32 size=decode_hex(body,raw,HFS_BODY_MAX);
   assert(!hfs_json_parse(raw,size,&original));free(body);
   body=read_line();size=decode_hex(body,raw,HFS_BODY_MAX);
   assert(!hfs_json_parse(raw,size,&response));free(body);free(raw);
   copy_json(j, 0, "binding", &context);
   struct hfs_mapped_binding binding = {.context = context,
       .verify_ed25519 = verify_ed25519};
   u32 n = 32;
   assert(!hfs_json_hex(j, hfs_json_field(j, 0, "key"), binding.public_key, &n));
   assert(n == 32);
   struct hfs_mapped_attestation inert = {0};
   hfs_test_last_allocation_size = 0;
   int err = hfs_mapped_receipt_verify(&binding, original, response, &inert);
   assert(hfs_test_last_allocation_size <= 16384);
   binding.verify_ed25519 = NULL;
   assert(hfs_mapped_receipt_verify(&binding, original, response, &inert) == -EOPNOTSUPP);
   printf("D %d %llu\n", err, (unsigned long long)inert.poll_count);
   hfs_json_free(j); hfs_json_free(original); hfs_json_free(response);
   hfs_json_free(context);
}
/* Exact production preflight at both fixed limits. Only the candidate number
 * is assigned; the actual endpoint allocator and original owner stay intact. */
static void preflight_bounds(Hfs9p *h) {
   Hfs9pEntry e = {.minor = 4, .guest_cell = 333,
       .native_cell = HFS9P_MAX_CELL, .donor = -1, .control = true};
   u64 before = h->high;
   for (unsigned mode = 0; mode < 3; mode++) {
      struct hfs_buf raw = {0};
      assert(!hfs_buf_init(&raw,HFS_BODY_MAX));
      hfs_buf_literal(&raw,"{\"cell\":333,\"context\":");
      hfs_buf_add(&raw,h->sessions[0].context,h->sessions[0].length);
      hfs_buf_literal(&raw,",\"delegation\":null,\"op\":\"RETIRE\",\"payload\":");
      if (mode < 2) {
         hfs_buf_literal(&raw,"\"");
         u32 target = HFS_BODY_MAX - (mode ? 5 : 600);
         const char *tail = "\",\"protocol\":";
         u32 suffix = strlen(tail)+strlen(_protocol(4))+strlen(",\"reclaim\":null}");
         while (raw.len + suffix < target) hfs_buf_literal(&raw,"x");
         hfs_buf_literal(&raw,tail);
      } else {
         hfs_buf_literal(&raw,"[] ,\"protocol\":");
         /* Replace the provisional payload below using its actual base token
          * count so the original fits but added delegation nodes do not. */
         raw.len -= strlen("[] ,\"protocol\":");
         hfs_buf_literal(&raw,"[");
         for (unsigned i = 0; i < HFS9P_TOKENS-200; i++)
            hfs_buf_literal(&raw,i ? ",0" : "0");
         hfs_buf_literal(&raw,"],\"protocol\":");
      }
      hfs_buf_literal(&raw,_protocol(4));
      hfs_buf_literal(&raw,",\"reclaim\":null}");
      assert(!raw.error);
      e.guest = (u8 *)raw.data; e.total = raw.len;
      Hfs9pJson j = {e.guest,e.total,h->tokens,0};
      assert(!hfs9p_json_parse(&j));
      if (mode == 2) {
         /* Increase the array to exactly the final five available tokens. */
         u32 missing = HFS9P_TOKENS-5-j.count;
         assert(raw.len+2*missing < HFS_BODY_MAX);
         u32 payload; assert(!hfs9p_json_key(&j,0,"payload",&payload));
         u32 pos = j.tokens[payload].end-1;
         memmove(raw.data+pos+2*missing,raw.data+pos,raw.len-pos);
         for (u32 i=0;i<missing;i++) memcpy(raw.data+pos+2*i,",0",2);
         raw.len+=2*missing;e.total=raw.len;
         j.length=e.total;assert(!hfs9p_json_parse(&j));
         assert(j.count==HFS9P_TOKENS-5);
      }
      u8 original[32],after[32];_hash(e.guest,e.total,original);
      int err = _native_body(h,&e);
      assert(mode ? err==E2BIG : !err);
      _hash(e.guest,e.total,after);
      assert(h->high==before && !memcmp(original,after,32));
      if (mode) assert(!e.native);
      free(e.native);e.native=NULL;hfs_buf_free(&raw);
   }
}

int main(void) {
   char *line = read_line();
   struct hfs_json *initial = NULL;
   assert(!hfs_json_parse(line, strlen(line), &initial)); free(line);
   Hfs9pEndpoint endpoint = {{0}, {110,111,112}};
   int fds[3] = {10,11,12};
   int contexts = hfs_json_field(initial, 0, "contexts");
   assert(contexts >= 0);
   int first = contexts + 1;
   u32 n = 16;
   char uuid[64];
   assert(hfs_json_string(initial, hfs_json_field(initial, first, "fuse_session"), uuid, sizeof(uuid)) == 36);
   unsigned index = 0;
   for (unsigned i = 0; uuid[i]; i++) {
      if (uuid[i] == '-') continue;
      int a = _hex_digit(uuid[i]), b = _hex_digit(uuid[++i]);
      assert(a >= 0 && b >= 0 && index < n);
      endpoint.session[index++] = a * 16 + b;
   }
   assert(index == n);
   int directory = open(getenv("HFS_MAPPED_LEDGER"), O_RDONLY | O_DIRECTORY);
   assert(directory >= 0);
   Hfs9pLedger *ledger = NULL;
   assert(!hfs9p_ledger_new(&ledger, directory, "carrier"));
   Hfs9pVerifier verifier = {ledger,claim_cb,protect_cb,primitive_verify,NULL,NULL};
   Hfs9p *h = NULL;
   assert(!_construct(&h, fds, &endpoint, verifier));
   /* Deterministic portable scheduling of the same _fetch/_wait_step helpers.
    * No thread or timer is used as proof of a native kernel observation. */
   pthread_mutex_lock(&h->mutex); h->stopping = true;
   pthread_mutex_unlock(&h->mutex); assert(!pthread_join(h->worker, NULL));
   u64 base;
   assert(!hfs_json_u64(initial, hfs_json_field(initial, 0, "base"), &base));
   h->high = base;
   for (int i = first; i < (int)initial->count; i++) {
      if (initial->tokens[i].parent != (u32)contexts || initial->tokens[i].kind != HFS_OBJECT) continue;
      assert(!hfs9p_add_session_protocol(h,
          (u8 *)initial->data + initial->tokens[i].start,
          initial->tokens[i].end - initial->tokens[i].start, 4));
   }
   hfs_json_free(initial);
   preflight_bounds(h);
   puts("READY"); fflush(stdout);
   for (;;) {
      line = read_line();
      if (!strcmp(line,"END")) { free(line); break; }
      if (line[0] == 'B') { check_builder(line + 2); free(line); fflush(stdout); continue; }
      if (line[0] == 'V') { verification(line + 2); free(line); fflush(stdout); continue; }
      char *space = strchr(line + 2, ' ');
      assert(space); *space = 0;
      unsigned session = strtoul(line + 2, NULL, 10);
      assert(session < h->session_count);
      if (line[0] == 'S') {
         u8 *raw = malloc(HFS_BODY_MAX), *frame = malloc(HFS_HEADER_SIZE + HFS_CHUNK_MAX);
         assert(raw && frame);
         u32 size = decode_hex(space + 1, raw, HFS_BODY_MAX);
         struct hfs_json *j = NULL;
         assert(!hfs_json_parse(raw,size,&j));
         u64 cell; u32 frame_size;
         assert(!hfs_json_u64(j,hfs_json_field(j,0,"cell"),&cell));
         bool wait = hfs_json_is(j,hfs_json_field(j,0,"op"),"\"VIEW_WAIT\"");
         bool ordinary = hfs_json_is(j,hfs_json_field(j,0,"op"),"\"HELLO\"") ||
             hfs_json_is(j,hfs_json_field(j,0,"op"),"\"VIEW_ACQUIRE\"") ||
             hfs_json_is(j,hfs_json_field(j,0,"op"),"\"RELEASE_CELL\"");
         int err = 0;
         for (u32 chunk = 0; chunk * HFS_CHUNK_MAX < size && !err; chunk++) {
            frame_size = HFS_HEADER_SIZE + HFS_CHUNK_MAX;
            assert(!hfs_frame_encode_version(4,1,(uuid_t *)h->sessions[session].id,
                cell,raw,size,chunk,frame,&frame_size));
            err = _send(h,session,wait ? 65534 : ordinary ? 42 : 65533,frame,frame_size);
            if (!err) /* lose SEND acknowledgement, retry exact owned bytes */
               err = _send(h,session,wait ? 65534 : ordinary ? 42 : 65533,frame,frame_size);
         }
         printf("D %d %llu\n",err,(unsigned long long)h->high);
         free(raw); free(frame); hfs_json_free(j);
      } else {
         assert(line[0] == 'F');
         u64 cell = strtoull(space + 1,NULL,10);
         Hfs9pEntry *e = NULL;
         for (unsigned i = 0; i < HFS9P_ENTRIES; i++)
            if (h->entries[i].guest && h->entries[i].session == session && h->entries[i].guest_cell == cell) e = &h->entries[i];
         assert(e);
         u8 *frame = malloc(HFS9P_HEADER + HFS9P_CHUNK);
         struct hfs_buf result = {0};
         assert(frame && !hfs_buf_init(&result,HFS_BODY_MAX));
         int err = 0;
         for (u32 chunk = 0; !err; chunk++) {
            size_t size;
            err = _fetch(h,session,cell,chunk,e->wait ? 3 : 2,frame,HFS9P_HEADER+HFS9P_CHUNK,&size);
            if (err) break;
            hfs_buf_add(&result,frame+HFS9P_HEADER,size-HFS9P_HEADER);
            if (result.len == e->response_total) break;
         }
         printf("D %d ",err); if (!err) emit_hex(result.data,result.len); puts("");
         hfs_buf_free(&result); free(frame);
      }
      free(line); fflush(stdout);
   }
   /* These retained disposable rows are deliberately preserved by the ledger;
    * process exit is not retirement or authority to release a guest owner. */
   return 0;
}
