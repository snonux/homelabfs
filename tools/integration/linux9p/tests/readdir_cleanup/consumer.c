// SPDX-License-Identifier: GPL-2.0-only
/* Production parent-funded consumer; the IPC peer executes the real host bridge.
 * Allocation/transport primitives supply no native or physical source proof.
 */
#include "hfs_protocol.h"
unsigned int hfs_test_alloc_call, hfs_test_alloc_fail;
size_t hfs_test_last_allocation_size;
struct hfs_session { int unused; };
struct hfs_cred { const char *bytes; u32 len; };
struct hfs_request {
 struct hfs_session *session;
 u32 users;
 bool terminal;
 struct hfs_json *result;
 struct hfs_buf claim, credentials, readdir_cleanup;
};
static unsigned adopted, exchanges;
static bool fail_queue;
static void hfs_cred_write(struct hfs_buf *out, const struct hfs_cred *cred)
{
 hfs_buf_add(out, cred->bytes, cred->len);
}
static void hfs_result_adopt(struct hfs_session *session,
                             const struct hfs_json *result)
{
 (void)session; (void)result; adopted++;
}
static int read_json(struct hfs_json **out)
{
 char *line = NULL;
 size_t cap = 0;
 ssize_t len = getline(&line, &cap, stdin);
 int err;
 if (len < 1) { free(line); return -EIO; }
 if (line[len - 1] == '\n') len--;
 err = hfs_json_parse(line, len, out);
 free(line);
 return err;
}
static int hfs_control(struct hfs_session *session, const char *op,
                       const struct hfs_buf *payload, bool control,
                       struct hfs_json **reply)
{
 (void)session;
 if (fail_queue) { fail_queue=false; return -EAGAIN; }
 if (!control || strcmp(op,"READDIR_CLEANUP")) return -EPROTO;
 if (fwrite(payload->data,1,payload->len,stdout)!=payload->len ||
     fputc('\n',stdout)==EOF || fflush(stdout)) return -EIO;
 {
  int err=read_json(reply);
  exchanges++;
  if (!err && exchanges==1 && getenv("HFS_CONSUMER_LOST_REPLY")) {
   hfs_json_free(*reply); *reply=NULL;
   return -EIO;
  }
  return err;
 }
}
#include "consumer.inc"
static int copy_field(struct hfs_json *json, const char *key,
                       struct hfs_buf *out)
{
 int token=hfs_json_field(json,0,key),err;
 if (token<0) return -EPROTO;
 err=hfs_buf_init(out,json->tokens[token].end-json->tokens[token].start+1);
 if (err) return err;
 hfs_buf_add(out,json->data+json->tokens[token].start,
              json->tokens[token].end-json->tokens[token].start);
 return out->error;
}
int main(void)
{
 struct hfs_session session={};
 struct hfs_json *fixture=NULL,*result=NULL;
 struct hfs_request request={.session=&session,.users=1,.terminal=true};
 struct hfs_buf encoded={};
 struct hfs_cred cred;
 int err=read_json(&fixture);
 if (err) return 2;
 if (copy_field(fixture,"claim",&request.claim) ||
     copy_field(fixture,"credentials",&request.credentials) ||
     copy_field(fixture,"result",&encoded) ||
     hfs_json_parse(encoded.data,encoded.len,&result) ||
     hfs_buf_init(&request.readdir_cleanup,HFS_BODY_MAX)) return 3;
 cred.bytes=request.credentials.data; cred.len=request.credentials.len;
 request.result=result; result->owner=&request;
 /* Missing preowned storage, held-owner mismatch and wrong full issuer all
  * fail before any transport work and cannot adopt the parent result.
  */
 request.readdir_cleanup.cap=0;
 if (hfs_readdir_cleanup(&session,result,&cred)!=-ENOSPC || adopted || exchanges) return 5;
 request.readdir_cleanup.cap=HFS_BODY_MAX;
 request.users=0;
 if (hfs_readdir_cleanup(&session,result,&cred)!=-ESTALE || adopted || exchanges) return 6;
 request.users=1;
 {
  struct hfs_cred wrong={.bytes="{}",.len=2};
  if (hfs_readdir_cleanup(&session,result,&wrong)!=-EACCES || adopted || exchanges) return 7;
 }
 fail_queue=true;
 if (hfs_readdir_cleanup(&session,result,&cred)!=-EAGAIN || adopted || exchanges || !request.readdir_cleanup.len) return 10;
 err=hfs_readdir_cleanup(&session,result,&cred);
 if (getenv("HFS_CONSUMER_LOST_REPLY")) {
  if (err!=-EIO || adopted || exchanges!=1 || !request.readdir_cleanup.len) return 8;
  err=hfs_readdir_cleanup(&session,result,&cred);
  if (exchanges!=2) return 9;
 }
 result->owner=NULL;
 if (!err && adopted!=1) err=-EPROTO;
 hfs_json_free(result); hfs_json_free(fixture);
 hfs_buf_free(&request.claim); hfs_buf_free(&request.credentials);
 hfs_buf_free(&request.readdir_cleanup); hfs_buf_free(&encoded);
 return err ? 4 : 0;
}
