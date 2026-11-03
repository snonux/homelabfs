// SPDX-License-Identifier: GPL-2.0-only
/* Production decoder/consumer with exact host literal and transport primitive.
 */
#include "hfs_policy.h"
unsigned int hfs_test_alloc_call, hfs_test_alloc_fail;
size_t hfs_test_last_allocation_size;
#define HFS_CAP_MAX 8192U
#define HFS_IO_MAX 65536U
#define CHECK(x)                                                               \
   do {                                                                        \
      if (!(x)) {                                                              \
         fprintf(stderr, "%d: %s\n", __LINE__, #x);                            \
         exit(1);                                                              \
      }                                                                        \
   } while (0)
static inline bool uuid_equal(const uuid_t *a, const uuid_t *b) {
   return !memcmp(a, b, sizeof(*a));
}
struct list_head {
   struct list_head *next, *prev;
};
#define INIT_LIST_HEAD(h)                                                      \
   do {                                                                        \
      (h)->next = (h);                                                         \
      (h)->prev = (h);                                                         \
   } while (0)
static void list_add_tail(struct list_head *node, struct list_head *head) {
   node->next = head;
   node->prev = head->prev;
   head->prev->next = node;
   head->prev = node;
}
static void list_del(struct list_head *node) {
   node->prev->next = node->next;
   node->next->prev = node->prev;
}
static void list_del_init(struct list_head *node) {
   list_del(node);
   INIT_LIST_HEAD(node);
}
#define mutex_lock(x) ((void)(x))
#define mutex_unlock(x) ((void)(x))
#define spin_lock(x) ((void)(x))
#define spin_unlock(x) ((void)(x))
#define WRITE_ONCE(x, v) ((x) = (v))
#define container_of(p, t, m) ((t *)((char *)(p) - offsetof(t, m)))
struct work_struct {
   void (*run)(struct work_struct *);
};
#define INIT_WORK(w, f) ((w)->run = (f))
struct hfs_session;
struct hfs_provider {
   int (*publication)(struct hfs_session *, const struct hfs_json *, int);
};
struct hfs_session {
   int lock, control;
   u16 bridge_minor;
   bool blocked;
   struct list_head forgets;
   struct {
      char context[4096];
      u32 context_len;
   } enrollment;
   const struct hfs_provider *provider;
};
struct hfs_cred {
   const char *canonical;
};
#include "fixture.h"
#include "result.inc"
static const struct hfs_cred *expected_issuer;
static const struct hfs_json *expected_result;
static unsigned calls, adopted;
static int transport_error;
static const struct hfs_cap *expected_caps[2];
static bool inode_mode;
static int hfs_call(struct hfs_session *session, const char *opcode,
                    const struct hfs_buf *args, const struct hfs_cred *issuer,
                    bool control, struct hfs_json **reply) {
   struct hfs_json *command = NULL;
   struct hfs_cap *cap = NULL;
   int entries = inode_mode ? 0 : hfs_json_field(expected_result, 0, "entries");
   int child = entries + 1, token;
   u32 index = 0;
   CHECK(issuer == expected_issuer &&
         !strcmp(issuer->canonical, fixture_credentials));
   CHECK(!strcmp(opcode, "FORGET") && control);
   CHECK(!args->error && !hfs_json_parse(args->data, args->len, &command));
   CHECK(!hfs_json_exact(command, 0, "count,object"));
   CHECK(hfs_json_is(command, hfs_json_field(command, 0, "count"), "1"));
   for (; !inode_mode && child < (int)expected_result->count; child++) {
      if (expected_result->tokens[child].parent != entries)
         continue;
      if (index++ == calls)
         break;
   }
   if (!inode_mode)
      CHECK(child < (int)expected_result->count);
   if (inode_mode) {
      CHECK(calls < 2);
      token = hfs_json_field(command, 0, "object");
      CHECK(command->tokens[token].end - command->tokens[token].start ==
            expected_caps[calls]->json->len);
      CHECK(!memcmp(command->data + command->tokens[token].start,
                    expected_caps[calls]->json->data,
                    expected_caps[calls]->json->len));
      hfs_json_free(command);
      calls++;
      *reply = NULL;
      return transport_error;
   }
   token = hfs_json_field(expected_result, child, "capability");
   CHECK(!hfs_cap_parse(session, expected_result, token, "PATH", &cap));
   token = hfs_json_field(command, 0, "object");
   CHECK(command->tokens[token].end - command->tokens[token].start ==
         cap->json->len);
   CHECK(!memcmp(command->data + command->tokens[token].start, cap->json->data,
                 cap->json->len));
   hfs_cap_free(cap);
   hfs_json_free(command);
   calls++;
   *reply = NULL;
   return transport_error;
}
static void hfs_result_adopt(struct hfs_session *session,
                             const struct hfs_json *result) {
   (void)session;
   CHECK(result == expected_result);
   adopted++;
}
/* This legacy1.0/1.1 primitive never selects the new minor3 branch; the
 * production successor helper is exercised over live host IPC separately.
 */
static int hfs_readdir_cleanup(struct hfs_session *session,
                               const struct hfs_json *result,
                               const struct hfs_cred *issuer) {
   (void)session; (void)result; (void)issuer;
   return -EOPNOTSUPP;
}
#include "consumer.inc"
static int publication(struct hfs_session *session, const struct hfs_json *json,
                       int token) {
   (void)session;
   return hfs_json_is(json, token, "null") ? 0 : -EPROTO;
}
#include "inode_test.inc"
int main(void) {
   struct hfs_provider provider = {publication};
   struct hfs_session session = {.provider = &provider};
   struct hfs_cred cred = {fixture_credentials};
   struct hfs_buf credentials = {.data = (char *)fixture_credentials,
                                 .len = sizeof(fixture_credentials) - 1};
   struct hfs_json *result = NULL;
   int err;
   memcpy(session.enrollment.context, fixture_context,
          sizeof(fixture_context) - 1);
   session.enrollment.context_len = sizeof(fixture_context) - 1;
   CHECK(!hfs_json_parse(fixture_result, strlen(fixture_result), &result));
   CHECK(!hfs_result_validate(&session, result, "READDIR", &credentials));
   CHECK(hfs_result_needs_adoption(result));
   expected_result = result;
   expected_issuer = &cred;
   CHECK(!hfs_readdir_forget(&session, result, &cred));
   CHECK(calls == 1 && adopted == 1);
   calls = adopted = 0;
   transport_error = -EIO;
   CHECK(hfs_readdir_forget(&session, result, &cred) == -EIO);
   CHECK(calls == 1 && adopted == 0);
   calls = 0;
   transport_error = 0;
   hfs_test_alloc_fail = hfs_test_alloc_call + 1;
   err = hfs_readdir_forget(&session, result, &cred);
   CHECK(err == -ENOMEM && !calls && !adopted);
   hfs_test_alloc_fail = 0;
   hfs_json_free(result);
   test_inode_tokens(&session, &cred);
   puts("PASS: actual service/journal READDIR literal through production "
        "decoder and exact-token/count1/original-issuer FORGET consumer; "
        "failed transport/allocation never adopts result");
   return 0;
}
