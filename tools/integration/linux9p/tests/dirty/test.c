// SPDX-License-Identifier: GPL-2.0-only
/* Production freeze/claim/ticket code. Page, clock and transport boundaries are
 * fixtures; this does not execute VFS or validate platform/source authority.
 */
#include "compat.h"
unsigned int hfs_test_alloc_call, hfs_test_alloc_fail;
size_t hfs_test_last_allocation_size;
static unsigned long jiffies;
static int calls, mode;
#include "production.c"
#include "fixture.h"

static int hfs_control(struct hfs_session *session, const char *operation,
                       const struct hfs_buf *payload, bool control,
                       struct hfs_json **out)
{
   const char *reply;
   (void)session;
   assert(control);
   if (calls++ == 0) {
      assert(!strcmp(operation,"DIRTY_FREEZE"));
      assert(payload->len == strlen(golden_payload));
      assert(!memcmp(payload->data,golden_payload,payload->len));
      reply=golden_pending;
   } else {
      assert(!strcmp(operation,"VIEW_QUERY"));
      assert(payload->len == strlen(golden_query));
      assert(!memcmp(payload->data,golden_query,payload->len));
      reply=mode ? corrupt_frozen : golden_frozen;
   }
   return hfs_json_parse(reply,strlen(reply),out);
}
static void uuid(uuid_t *out, const char *text)
{
   unsigned int index=0, high=0;
   for (;*text;text++) {
      if (*text=='-') continue;
      unsigned int nibble=*text>='a' ? *text-'a'+10 : *text-'0';
      assert(nibble<16);
      if (!(index&1)) high=nibble<<4;
      else out->b[index/2]=(u8)(high|nibble);
      index++;
   }
   assert(index==32);
}
static void run_case(int corrupt)
{
   struct hfs_session session={0};
   struct hfs_view view={.session=&session,.phase=HFS_VIEW_QUIESCENT,.quiescent=true};
   struct hfs_view_family family={.control=true,.next=5,.prepared=1,.floor=4};
   struct hfs_view_ticket ticket={.session=&session,.family=&family,.control=true,.users=1};
   struct hfs_request request={.session=&session,.earmarked=true,.control=true,.number=9};
   struct page page={0};
   struct hfs_cache_page entry={.page=&page,.view=&view};
   struct hfs_dirty dirty={.entry=&entry,.freeze_ticket=&ticket,.request=&request,
       .adopted=true,.length=4,.offset=4096};
   int err;
   calls=0;mode=corrupt;jiffies=0;
   mutex_init(&session.lock);mutex_init(&family.dispatch);spin_lock_init(&view.lock);
   INIT_LIST_HEAD(&session.view_tickets);INIT_LIST_HEAD(&ticket.list);
   list_add_tail(&ticket.list,&session.view_tickets);
   uuid(&session.enrollment.realm,realm);
   uuid(&session.enrollment.anchor,anchor);
   uuid(&session.enrollment.nonce,claim_nonce);
   session.enrollment.subject[0]='a';session.enrollment.subject[1]='b';session.enrollment.subject[2]='c';
   session.enrollment.subject_len=3;
   uuid(&family.nonce,control_nonce);uuid(&view.recall,recall);
   assert(!hfs_buf_init(&family.credentials,16384));
   hfs_buf_literal(&family.credentials,golden_credentials);
   assert(!hfs_buf_init(&ticket.request,32768));assert(!hfs_buf_init(&ticket.query,32768));
   assert(!hfs_buf_init(&ticket.payload,HFS_VIEW_BODY_MAX));assert(!hfs_buf_init(&ticket.original,HFS_VIEW_BODY_MAX));
   ticket.scratch=calloc(4096,sizeof(*ticket.scratch));assert(ticket.scratch);
   assert(!hfs_buf_init(&request.claim,4096));assert(!hfs_buf_init(&request.intent,32768));
   assert(!hfs_buf_init(&dirty.freeze_payload,HFS_VIEW_BODY_MAX));
   assert(!hfs_buf_init(&dirty.drain_args,32768));
   assert(!hfs_json_parse(golden_reservation,strlen(golden_reservation),&dirty.reservation));
   u32 len=32;
   assert(!hfs_json_hex(dirty.reservation,hfs_json_field(dirty.reservation,0,"id"),dirty.id,&len));
   memcpy(page.bytes,"ABCD",4);
   err=hfs_dirty_freeze(&view,&dirty);
   assert(calls==2 && ticket.number==5 && family.next==6 && family.prepared==0 && !family.initial);
   assert(ticket.admitted && ticket.initial_response && ticket.response);
   assert(hfs_json_is(ticket.initial_response,hfs_json_field(ticket.initial_response,0,"status"),"\"PENDING\""));
   assert(request.bound && dirty.captured && !memcmp(dirty.data,"ABCD",4));
   assert(request.claim.len==strlen(golden_claim) && !memcmp(request.claim.data,golden_claim,request.claim.len));
   assert(dirty.drain_args.len==strlen(golden_args) && !memcmp(dirty.drain_args.data,golden_args,dirty.drain_args.len));
   if (corrupt) {
      assert(err==-EPROTO && session.blocked && !dirty.frozen && !dirty.frozen_entry);
   } else {
      assert(!err && dirty.frozen && dirty.frozen_entry);
      assert(dirty.frozen_entry->len==strlen(golden_entry));
      assert(!memcmp(dirty.frozen_entry->data,golden_entry,dirty.frozen_entry->len));
      memcpy(page.bytes,"ZZZZ",4);
      assert(!hfs_dirty_freeze(&view,&dirty) && calls==2 && !memcmp(dirty.data,"ABCD",4));
      printf("FREEZE_PAYLOAD %.*s\n",ticket.payload.len,ticket.payload.data);
   }
   /* Fixture teardown does not stand in for a live source retirement proof. */
   hfs_json_free(ticket.response);hfs_json_free(ticket.initial_response);
   hfs_json_free(dirty.reservation);hfs_json_free(dirty.frozen_entry);
   hfs_buf_free(&dirty.freeze_payload);hfs_buf_free(&dirty.drain_args);
   hfs_buf_free(&request.claim);hfs_buf_free(&request.intent);
   hfs_buf_free(&ticket.request);hfs_buf_free(&ticket.query);hfs_buf_free(&ticket.payload);hfs_buf_free(&ticket.original);
   hfs_buf_free(&family.credentials);free(ticket.scratch);
   assert(!pthread_mutex_destroy(&session.lock.native));assert(!pthread_mutex_destroy(&family.dispatch.native));
   assert(!pthread_mutex_destroy(&view.lock));
}
int main(void)
{
   run_case(0);run_case(1);
   puts("PASS: actual freeze/claim/activation/exchange + pending QUERY lineage and wrapper-rehashed entry mismatch; no VFS/runtime/source proof");
   return 0;
}
