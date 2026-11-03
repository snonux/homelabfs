// SPDX-License-Identifier: GPL-2.0-only
/* Actual retirement/result-pin C; transport/proof are explicit test boundaries.
 * This is not a runtime verifier or filesystem/source-closure acceptance.
 */
#include "compat.h"
unsigned int hfs_test_alloc_call, hfs_test_alloc_fail;
size_t hfs_test_last_allocation_size;
#include "production.c"

static int calls, fact_calls, gap_calls, released;
static int fact_error, response_mode;
static const char result_id[] = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
static const char expected[] =
   "{\"anchor\":\"00000000-0000-0000-0000-000000000002\","
   "\"nonce\":\"00000000-0000-0000-0000-000000000003\","
   "\"realm\":\"00000000-0000-0000-0000-000000000001\","
   "\"result_hashes\":[\"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\"],"
   "\"subject\":\"a1b2\",\"through\":1}";

static int hfs_control(struct hfs_session *s, const char *operation,
                       const struct hfs_buf *payload, bool control,
                       struct hfs_json **out)
{
   const char *reply;
   (void)s;
   assert(!strcmp(operation, "RETIRE") && control);
   assert(payload->len == strlen(expected) && !memcmp(payload->data, expected, payload->len));
   calls++;
   if (response_mode == 1)
      reply = "{\"payload\":{\"floor\":2,\"proof_hash\":\"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\",\"status\":\"RETIRED\"}}";
   else if (response_mode == 2)
      reply = "{\"payload\":{\"floor\":0,\"proof_hash\":null,\"status\":\"BLOCKED\"}}";
   else
      reply = "{\"payload\":{\"floor\":1,\"proof_hash\":\"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\",\"status\":\"RETIRED\"}}";
   return hfs_json_parse(reply, strlen(reply), out);
}
static void unreserve(struct hfs_session *s, void *credit)
{
   (void)s; assert(credit); free(credit); released++;
}
static int retirement(struct hfs_session *s, u64 through, const u8 *proof)
{
   (void)s; assert(through == 1);
   for (int i = 0; i < 32; i++) assert(proof[i] == 0xbb);
   fact_calls++;
   return fact_error;
}
static int gap(struct hfs_session *s, struct hfs_request *request)
{
   (void)s; (void)request; gap_calls++; return -EAGAIN;
}
struct attempt { struct hfs_session *session; int result; };
static void *try_retire(void *arg)
{
   struct attempt *a = arg;
   a->result = hfs_retire(a->session);
   return NULL;
}
int main(void)
{
   struct hfs_provider provider = {.unreserve=unreserve, .retirement=retirement, .resolve_gap=gap};
   struct hfs_session session = {.provider=&provider};
   struct hfs_request *request = calloc(1, sizeof(*request));
   struct hfs_json *returned;
   char private_result[90];
   pthread_t worker;
   struct attempt attempt = {&session, 99};
   assert(request);
   mutex_init(&session.lock); mutex_init(&session.retirement);
   INIT_LIST_HEAD(&session.requests);
   session.enrollment.realm.b[15]=1;
   session.enrollment.anchor.b[15]=2;
   session.enrollment.nonce.b[15]=3;
   session.enrollment.subject[0]=0xa1; session.enrollment.subject[1]=0xb2;
   session.enrollment.subject_len=2;
   request->session=&session; request->number=1;
   request->terminal=true; request->adopted=true; request->users=1;
   request->reservation=malloc(1); assert(request->reservation);
   snprintf(private_result, sizeof(private_result), "{\"id\":\"%s\"}", result_id);
   assert(!hfs_json_parse(private_result, strlen(private_result), &request->result));
   list_add_tail(&request->list, &session.requests); session.nr_requests=1;
   assert(!hfs_json_parse("{}", 2, &returned));
   returned->owner=request; returned->owner_put=hfs_request_result_put;
   /* Another real retirement caller cannot free a result's request owner. */
   assert(!pthread_create(&worker, NULL, try_retire, &attempt));
   assert(!pthread_join(worker, NULL));
   assert(attempt.result == -EAGAIN && calls == 0 && released == 0);
   assert(request->users == 1 && session.nr_requests == 1);
   hfs_json_free(returned);
   assert(request->users == 0);
   request->held=true;
   assert(hfs_retire(&session) == -EAGAIN && calls == 0);
   request->held=false;
   fact_error=-EACCES;
   assert(hfs_retire(&session) == -EACCES && fact_calls == 1 && !released);
   response_mode=1;
   assert(hfs_retire(&session) == -EPROTO && fact_calls == 1 && !released);
   response_mode=2;
   assert(hfs_retire(&session) == -EAGAIN && !released);
   request->terminal=false; request->gap=true;
   assert(hfs_retire(&session) == -EAGAIN && gap_calls == 1 && !released);
   request->terminal=true;
   response_mode=0; fact_error=0;
   assert(hfs_retire(&session) == 0 && fact_calls == 2 && released == 1);
   assert(session.nr_requests == 0 && list_empty(&session.requests));
   puts("PASS: production result-owner pin + concurrent retirement, held drain, wrong floor, blocked/gap and denied proof; no VFS/runtime proof");
   return 0;
}
