// SPDX-License-Identifier: GPL-2.0-only
/* Actual pressure/mkwrite/stop/install C. PTE, VFS cache, source verification and
 * transport result primitives are fixtures, never running-kernel proof.
 */
unsigned int hfs_test_alloc_call,hfs_test_alloc_fail;
size_t hfs_test_last_allocation_size;
#include "production.c"
static struct folio folios[128];
static struct hfs_lease leases[128];
static struct hfs_view_ticket release_credits[128];
static uuid_t generation={{1}},recall={{2}};
static int releases,invalid_source,mapping_error;
static struct hfs_lease *last_release;
static struct hfs_json *target_event;
static struct vm_area_struct *test_vma;
static int reserve(void *s,u64 bytes,bool control,void **out)
{
 (void)control;assert(s==test_session && bytes);*out=malloc(1);assert(*out);atomic_fetch_add(&reservations,1);return 0;
}
static void unreserve(void *s,void *credit)
{ assert(s==test_session && credit);free(credit);atomic_fetch_sub(&reservations,1); }
static void unmap_mapping_range(struct address_space *mapping,int start,int length,int cows)
{
 assert(mapping && !start && !length && !cows && observed_view->phase==HFS_VIEW_RECALLING && !observed_view->active);
 for(unsigned int i=0;i<ARRAY_SIZE(folios);i++)if(folios[i].mapping==mapping)folios[i].page.mapped=false;
 atomic_fetch_add(&unmaps,1);
}
static int invalidate_inode_pages2(struct address_space *mapping)
{
 for(unsigned int i=0;i<ARRAY_SIZE(folios);i++) {
  struct folio *f=&folios[i];if(f->mapping!=mapping || !f->page.private)continue;
  assert(!f->page.locked);lock_page(&f->page);
  if(f->page.dirty) {int err=hfs_cache_aops.launder_folio(f);if(err){unlock_page(&f->page);return err;}}
  hfs_cache_aops.invalidate_folio(f,0,PAGE_SIZE);f->mapping=NULL;
  hfs_cache_aops.free_folio(f);unlock_page(&f->page);
 }
 return 0;
}
int hfs_view_target(struct hfs_session *session,const struct hfs_json *event,int token,uuid_t *visibility,u8 hash[32])
{
 assert(session==test_session && event==target_event && token==hfs_json_field(event,0,"target"));
 if(invalid_source)return -EACCES;
 *visibility=generation;memset(hash,0,32);return 0;
}
/* 1a4: hfs_mmap_fail's once-per-session line names the session, the worker
 * step, the host event and ACK phases, the host's reply status, the view
 * phase and the recall. zq3-r11's case: DRAINED for an INSTALL_DRAIN event,
 * answered STALE (-116). The event executor's record is filled by the actual
 * hfs_mmap_note_event and hfs_mmap_word, as hfs_mmap_events and hfs_mmap_ack
 * do; the ACK exchange itself is not run here.
 */
static void view_worker_log_checks(struct hfs_session *session)
{
 static const char event[]="{\"payload\":{\"event\":{\"phase\":\"INSTALL_DRAIN\"}}}";
 static const char stale[]="{\"payload\":{\"status\":\"STALE\"}}";
 static const char hostile[]="{\"payload\":{\"status\":\"OK fake%n\"}}";
 struct hfs_mmap_control control={.view=session->view};
 struct hfs_json *json;int body;
 uuid_t recall_id={{0x12,0x34}};
 session->enrollment.session=(uuid_t){{0xab,0xcd,[15]=0xef}};
 session->block_reported=0;session->blocked=false;warnings=0;
 assert(!hfs_json_parse(event,strlen(event),&json));
 hfs_mmap_note_event(&control,json);hfs_json_free(json);
 assert(!strcmp(control.event_phase,"INSTALL_DRAIN") && !control.ack_phase && !control.ack_status[0]);
 control.ack_phase="DRAINED";
 assert(!hfs_json_parse(stale,strlen(stale),&json));body=hfs_json_field(json,0,"payload");
 hfs_mmap_word(json,hfs_json_field(json,body,"status"),control.ack_status,sizeof(control.ack_status));hfs_json_free(json);
 control.recall=recall_id;control.have_recall=true;
 hfs_mmap_fail_event(&control,-ESTALE,"DRAINED");
 assert(warnings==1 && control.error==-ESTALE && session->blocked && session->view->phase==HFS_VIEW_FENCED);
 assert(!strcmp(last_warning,"9p-hfs: session abcd0000-0000-0000-0000-0000000000ef view control worker stopped, error -116, exporter ERROR -, at DRAINED (event INSTALL_DRAIN, ack DRAINED, host status STALE, view phase INSTALLED_DRAIN, recall 12340000-0000-0000-0000-000000000000); later operations on this mount fail\n"));
 // Only its own bit: a later independent hfs_session_block still logs.
 assert(session->block_reported==2UL);
 // Once per session: another worker's stop logs nothing.
 hfs_mmap_fail(&control,-ETIMEDOUT,&(struct hfs_mmap_where){.step="VIEW_WAIT"});
 assert(warnings==1 && control.error==-ETIMEDOUT);
 // A session that already logged a block still logs its worker stop. A
 // VIEW_WAIT failure without a reply prints "-" for what it lacks, and host
 // text that is not a plain [A-Z_] word is never copied into the log.
 session->block_reported=1;
 char status[24]="";
 assert(!hfs_json_parse(hostile,strlen(hostile),&json));body=hfs_json_field(json,0,"payload");
 hfs_mmap_word(json,hfs_json_field(json,body,"status"),status,sizeof(status));
 assert(!strcmp(status,"?"));
 hfs_mmap_word(json,hfs_json_field(json,body,"missing"),status,sizeof(status));hfs_json_free(json);
 assert(!strcmp(status,"?"));
 status[0]=0;
 hfs_mmap_fail(&control,-ETIMEDOUT,&(struct hfs_mmap_where){.step="VIEW_WAIT",.status=status});
 assert(warnings==2 && session->block_reported==3UL);
 assert(!strcmp(last_warning,"9p-hfs: session abcd0000-0000-0000-0000-0000000000ef view control worker stopped, error -110, exporter ERROR -, at VIEW_WAIT (event -, ack -, host status -, view phase FENCED, recall -); later operations on this mount fail\n"));
 // oc4: the raw reason of the session's last exporter ERROR (UNAVAILABLE,
 // the host transport down, as in zq3-r12 R12-1) follows the errno.
 session->block_reported=1;session->exporter_error=5; // HFS_ERROR_UNAVAILABLE (hfs_policy.h, not included here)
 hfs_mmap_fail(&control,-EHOSTDOWN,&(struct hfs_mmap_where){.step="VIEW_WAIT"});
 assert(warnings==3 && session->block_reported==3UL);
 assert(!strcmp(last_warning,"9p-hfs: session abcd0000-0000-0000-0000-0000000000ef view control worker stopped, error -112, exporter ERROR 5, at VIEW_WAIT (event -, ack -, host status -, view phase FENCED, recall -); later operations on this mount fail\n"));
 session->exporter_error=0;
 // 1a4 review: a committed CREATE/MKDIR/SYMLINK whose initial attributes
 // timed out names the session and the opcode from the request's intent,
 // without blocking the session; a non-word opcode prints "?".
 struct hfs_request committed={0};
 static char mkdir_intent[]="{\"args\":{},\"opcode\":\"MKDIR\"}",odd_intent[]="{\"opcode\":\"mk\"}";
 session->blocked=false;
 committed.intent=(struct hfs_buf){.data=mkdir_intent,.len=sizeof(mkdir_intent)-1,.cap=sizeof(mkdir_intent)};
 hfs_attr_timeout_warn(session,&committed);
 assert(warnings==4 && !session->blocked);
 assert(!strcmp(last_warning,"9p-hfs: session abcd0000-0000-0000-0000-0000000000ef MKDIR committed but its initial attributes timed out behind a recall (-ETIMEDOUT); result retained unadopted, retirement floor pinned\n"));
 committed.intent=(struct hfs_buf){.data=odd_intent,.len=sizeof(odd_intent)-1,.cap=sizeof(odd_intent)};
 hfs_attr_timeout_warn(session,&committed);
 assert(warnings==5 && strstr(last_warning," ? committed "));
}
static void hfs_lease_get(struct hfs_lease *lease) { kref_get(&lease->refs); }
static void hfs_lease_free(struct hfs_lease *lease) { assert(atomic_fetch_sub(&lease->refs.refcount,1)>1); }
static int hfs_lease_release(struct hfs_session *session,struct hfs_lease *lease)
{
 struct hfs_view *view=session->view;u64 seq=view->sequence+1;int target=hfs_json_field(target_event,0,"target"),err;
 assert(lease->published && !lease->dirty && lease->release_ticket && lease->release_ticket->control);
 assert(!view->active && !pthread_mutex_trylock(&view->transitions.native));pthread_mutex_unlock(&view->transitions.native);
 assert(!pthread_mutex_trylock(&view->lock));pthread_mutex_unlock(&view->lock);
 for(unsigned int i=0;i<ARRAY_SIZE(folios);i++)assert(!folios[i].page.locked);
 releases++;last_release=lease;recall.b[0]++;
 err=hfs_view_stop(view,&generation,&recall,seq++);if(err)return err;
 err=hfs_view_seal(view);if(err)return err;
 err=hfs_mmap_install(view,target_event,target,&recall,seq++,false);if(err)return err;
 err=hfs_mmap_finish_drained(view);if(err)return err;
 err=hfs_mmap_install(view,target_event,target,&recall,seq++,true);if(err)return err;
 err=hfs_view_reopen(view,&recall,seq);if(err)return err;
 assert(lease->revoked);lease->released=true;return 0;
}
static void hfs_request_unhold(struct hfs_session *s,struct hfs_request *r) { (void)s;(void)r;abort(); }
void hfs_view_ticket_put(struct hfs_view_ticket *ticket) { (void)ticket;abort(); }
static int hfs_request_drain(struct hfs_session *s,struct hfs_request *r,struct hfs_json **out) { (void)s;(void)r;(void)out;abort(); }
static void hfs_dirty_mapping_error(struct hfs_view *v,struct hfs_dirty *d,int err) { (void)v;(void)d;mapping_error=err; }
static void clean_page(struct hfs_view *view,struct address_space *mapping,unsigned int index)
{
 struct hfs_view_stamp stamp;struct hfs_cache_page *entry;struct hfs_lease *lease=&leases[index];void *cookie;
 assert(!hfs_view_snapshot(view,&stamp));assert(!hfs_view_page_prepare(view,&stamp,&entry));
 memset(lease,0,sizeof(*lease));kref_init(&lease->refs);release_credits[index].control=true;
 lease->release_ticket=&release_credits[index];lease->description[0]=1;lease->visibility=generation;entry->lease=lease;
 folios[index]=(struct folio){.page={.locked=true,.uptodate=true,.refs=1},.mapping=mapping};
 assert(!hfs_view_page_attach(entry,&folios[index].page));
 assert(!hfs_view_page_enter(test_vma,&folios[index].page,false,&cookie));hfs_view_leave(cookie);
 folios[index].page.mapped=true;unlock_page(&folios[index].page);hfs_view_page_put(entry);
}
/* ie4 (zq3-r15 R15-1): hfs_mmap_begin's re-check after VIEW_ACQUIRE. The
 * acquisition is a fixture: retry_hook plays what the host and the event
 * worker do while VIEW_ACQUIRE is PENDING (a whole recall, a fence, a
 * session block, a recall that never ends), and the first stale_targets
 * grants carry another target than the installed one. Queued cleanup
 * packets are recorded and released by run_cleanups, which plays
 * hfs_mmap_cleanup; with release_recalls each release starts a recall of its
 * own, like the host's scoped release recall (task 784).
 */
enum retry_hook { HOOK_NONE, HOOK_RECALL, HOOK_FENCE, HOOK_BLOCK, HOOK_STUCK };
static enum retry_hook retry_hook;
static unsigned int acquire_calls, stale_targets, nr_queued, queued_at_acquire[8];
static unsigned long acquire_jiffies;
static int leases_live, control_refs;
static bool release_recalls;
static struct hfs_lease *granted[8];
static struct hfs_cache_cleanup *queued[8];
static struct hfs_mmap_control control_fixture;
static int provider_leaves;
static struct hfs_mmap_control *hfs_mmap_control_get(struct hfs_view *view)
{ (void)view;control_refs++;return &control_fixture; }
static void hfs_mmap_control_put(struct hfs_mmap_control *control)
{ assert(control==&control_fixture && control_refs>0);control_refs--; }
static void hfs_mmap_cleanup(struct work_struct *work) { (void)work;abort(); }
static bool test_queue_cleanup(struct delayed_work *work)
{
 struct hfs_cache_cleanup *cleanup=container_of(work,struct hfs_cache_cleanup,work);
 for(unsigned int i=0;i<nr_queued;i++)assert(queued[i]!=cleanup); // never queued twice
 assert(nr_queued<ARRAY_SIZE(queued));queued[nr_queued++]=cleanup;return true;
}
static void provider_leave(void *session,void *cookie) { assert(session==test_session && !cookie);provider_leaves++; }
/* A complete recall that reinstalls the same target (as hfs_lease_release's fixture). */
static void same_target_recall(struct hfs_view *view)
{
 u64 seq=view->sequence+1;int target=hfs_json_field(target_event,0,"target");
 recall.b[0]++;
 assert(!hfs_view_stop(view,&generation,&recall,seq++));assert(!hfs_view_seal(view));
 assert(!hfs_mmap_install(view,target_event,target,&recall,seq++,false));
 assert(!hfs_mmap_finish_drained(view));
 assert(!hfs_mmap_install(view,target_event,target,&recall,seq++,true));
 assert(!hfs_view_reopen(view,&recall,seq));
}
int hfs_lease_acquire(struct hfs_session *session,const struct hfs_cred *cred,
		      const struct hfs_cap *object,const struct hfs_cap *description,
		      u64 offset,bool dirty,const struct hfs_buf *expected_target,
		      struct hfs_lease **out)
{
 struct hfs_view *view=session->view;struct hfs_lease *lease=calloc(1,sizeof(*lease));
 assert(session==test_session && cred && !object && !description && !offset && !dirty);
 assert(expected_target && expected_target->len && lease && acquire_calls<ARRAY_SIZE(granted));
 queued_at_acquire[acquire_calls]=nr_queued;
 jiffies+=acquire_jiffies;
 if(retry_hook==HOOK_RECALL)same_target_recall(view);
 else if(retry_hook==HOOK_FENCE)hfs_view_fence(view);
 else if(retry_hook==HOOK_BLOCK){session->bridge_minor=4;session->blocked=true;}
 else if(retry_hook==HOOK_STUCK)view->phase=HFS_VIEW_RECALLING;
 kref_init(&lease->refs);memcpy(lease->target_hash,view->target_hash,32);
 if(acquire_calls<stale_targets)lease->target_hash[0]^=1;
 granted[acquire_calls++]=lease;leases_live++;*out=lease;return 0;
}
/* hfs_mmap_cleanup's part for an observation packet: release the lease,
 * return the credit and the control reference, free the packet.
 */
static void run_cleanups(void)
{
 for(unsigned int i=0;i<nr_queued;i++) {
  struct hfs_cache_cleanup *cleanup=queued[i];
  assert(cleanup->control==&control_fixture && !cleanup->pending && list_empty(&cleanup->readers));
  if(cleanup->lease){
   assert(!cleanup->lease->published || cleanup->lease->revoked);
   free(cleanup->lease);leases_live--;
   if(release_recalls)same_target_recall(test_session->view);
  }
  list_del(&cleanup->list);unreserve(test_session,cleanup->credit);free(cleanup);
  hfs_mmap_control_put(&control_fixture);
 }
 nr_queued=0;
}
static int retry_begin(enum retry_hook hook,unsigned int stale,void **cookie)
{
 static struct inode inode;static int cred;
 retry_hook=hook;stale_targets=stale;acquire_calls=0;
 return hfs_mmap_begin(&inode,false,(const struct hfs_cred *)(void *)&cred,NULL,cookie);
}
static void retry_end(void *cookie)
{
 unsigned int before=nr_queued;
 hfs_mmap_end(cookie);assert(nr_queued==before+1);run_cleanups();
}
static void retry_no_leaks(int reserved)
{
 assert(!nr_queued && !leases_live && !control_refs && reservations==reserved);
 assert(list_empty(&control_fixture.cleanup) && !test_session->view->nr_observations);
}
/* R15-1's case: a recall completes while VIEW_ACQUIRE is PENDING and the
 * host grants at the same target. Before ie4 this was -ESTALE. Now the
 * granted lease is kept and re-stamped: one acquisition, nothing released
 * during the call, the observation carries the new revision.
 */
static void retry_same_target(struct hfs_view *view,int reserved)
{
 struct hfs_observation *observation;void *cookie;u64 before=view->revision;unsigned int lines=warnings;
 assert(!retry_begin(HOOK_RECALL,0,&cookie));observation=cookie;
 assert(acquire_calls==1 && !nr_queued && view->revision>before);
 assert(observation->stamp.revision==view->revision && observation->lease==granted[0]);
 assert(granted[0]->published && view->nr_observations==1 && warnings==lines+1);
 assert(strstr(last_warning,"observation re-check failed (recall completed at the same target); re-stamped under the granted lease"));
 retry_end(cookie);assert(provider_leaves==1);retry_no_leaks(reserved);
}
/* The self-recall loop: every release starts a recall and every acquisition
 * waits for one. Releasing and acquiring again would fail each attempt the
 * same way until -ETIMEDOUT; each operation needs one acquisition here.
 */
static void retry_self_recall(int reserved)
{
 void *cookie;
 release_recalls=true;
 for(int i=0;i<3;i++) {
  assert(!retry_begin(HOOK_RECALL,0,&cookie));
  assert(acquire_calls==1 && !nr_queued);retry_end(cookie);
 }
 release_recalls=false;retry_no_leaks(reserved);
}
/* A grant at another target than the installed one: release and acquire
 * again, but the stale lease goes only after the retry's own acquisition,
 * so its release is not the domain's last holder.
 */
static void retry_moved_target(int reserved)
{
 void *cookie;struct hfs_observation *observation;
 assert(!retry_begin(HOOK_NONE,1,&cookie));observation=cookie;
 assert(acquire_calls==2 && !queued_at_acquire[0] && !queued_at_acquire[1]);
 assert(nr_queued==1 && queued[0]->lease==granted[0] && !granted[0]->published);
 assert(observation->lease==granted[1] && granted[1]->published);
 assert(strstr(last_warning,"(lease target differs from the view); acquiring again before releasing it"));
 retry_end(cookie);retry_no_leaks(reserved);
}
/* Refusals keep -ESTALE (a fenced view, a blocked hfs1.4 session, a failed
 * transport), and their one lease is released once.
 */
static void retry_refused(struct hfs_view *view,int reserved)
{
 void *cookie=(void *)1;
 assert(retry_begin(HOOK_FENCE,0,&cookie)==-ESTALE && !cookie);
 assert(acquire_calls==1 && nr_queued==1 && queued[0]->lease==granted[0] && !granted[0]->published);
 run_cleanups();retry_no_leaks(reserved);view->phase=HFS_VIEW_LIVE; // fixture: unfence
 assert(retry_begin(HOOK_BLOCK,0,&cookie)==-ESTALE && acquire_calls==1 && nr_queued==1);
 run_cleanups();retry_no_leaks(reserved);test_session->bridge_minor=0;test_session->blocked=false;
 test_session->transport_failed=true;
 assert(retry_begin(HOOK_NONE,8,&cookie)==-ESTALE && acquire_calls==1 && nr_queued==1);
 run_cleanups();retry_no_leaks(reserved);test_session->transport_failed=false;
}
/* The retries share one HFS_VIEW_ENTER_WAIT_MS deadline: three stale grants
 * 10 s apart, or one recall that never ends, are -ETIMEDOUT with one line,
 * and every attempt's packet is queued exactly once.
 */
static void retry_deadline(struct hfs_view *view,int reserved)
{
 void *cookie;
 acquire_jiffies=10000;
 assert(retry_begin(HOOK_NONE,8,&cookie)==-ETIMEDOUT && acquire_calls==3 && nr_queued==3);
 assert(queued[2]->lease==granted[2] && queued[0]->lease==granted[0] && queued[1]->lease==granted[1]);
 assert(strstr(last_warning,"observation retries exhausted after 3 failed re-checks (-ETIMEDOUT)"));
 run_cleanups();retry_no_leaks(reserved);acquire_jiffies=0;
 view_wait_step=0;
 assert(retry_begin(HOOK_STUCK,0,&cookie)==-ETIMEDOUT && acquire_calls==1 && nr_queued==1);
 assert(strstr(last_warning,"observation retries exhausted after 1 failed re-checks"));
 run_cleanups();retry_no_leaks(reserved);view->phase=HFS_VIEW_LIVE; // fixture: end the recall
}
/* hfs_attr_install's cache-observation re-check: a request stamped before a
 * recall that reinstalled the same target is re-stamped and installs; a
 * moved target or a fenced view stays -ESTALE without an entry.
 */
static void retry_attr(struct hfs_view *view)
{
 struct hfs_request request={0};void *cookie;unsigned int lines;
 assert(!hfs_view_snapshot_target(view,&request.view_stamp,request.view_target));
 same_target_recall(view);
 assert(hfs_view_enter(view,&request.view_stamp,false,&cookie)==-ESTALE);
 assert(!hfs_attr_restamp(test_session,view,&request,&cookie) && cookie && view->active==1);
 assert(strstr(last_warning,"attribute re-check failed (recall completed at the same target); re-stamped"));
 hfs_view_leave(cookie);
 request.view_target[0]^=1;
 assert(hfs_attr_restamp(test_session,view,&request,&cookie)==-ESTALE && !cookie && !view->active);
 assert(strstr(last_warning,"(target moved); not installed (-ESTALE)"));
 view->phase=HFS_VIEW_FENCED;lines=warnings;
 assert(hfs_attr_restamp(test_session,view,&request,&cookie)==-ESTALE && !cookie && warnings==lines);
 view->phase=HFS_VIEW_LIVE; // fixture: unfence
}
static void observation_retry_checks(struct hfs_session *session)
{
 struct hfs_view *view=session->view;int reserved=atomic_load(&reservations);
 spin_lock_init(&control_fixture.lock);INIT_LIST_HEAD(&control_fixture.cleanup);
 assert(view->phase==HFS_VIEW_LIVE && view->target.len && !view->active && !view->nr_observations);
 retry_same_target(view,reserved);
 retry_self_recall(reserved);
 retry_moved_target(reserved);
 retry_refused(view,reserved);
 retry_deadline(view,reserved);
 retry_attr(view);
}
int main(void)
{
 struct hfs_provider provider={reserve,unreserve,provider_leave};struct hfs_session session={.provider=&provider};
 struct address_space mapping={.id=1,.a_ops=&hfs_cache_aops};struct hfs_inode inode={.inode={&mapping,1}};
 struct hfs_cap cap={.token={1}};struct hfs_description desc={.cap=&cap};struct file file={&inode.inode,&desc};struct vm_area_struct vma={&file};
 test_vma=&vma;test_session=&session;mutex_init(&session.lock);INIT_LIST_HEAD(&inode.view_list);
 assert(!hfs_view_new(&session,&generation));observed_view=session.view;assert(!hfs_view_register_inode(&inode.inode));
 assert(!hfs_json_parse("{\"target\":{}}",13,&target_event));
 // p84 (zq3-r9 R9-1): an in-flight request's published DOMAIN observation
 // lease is never a pressure victim. With no cache lease pressure is a no-op;
 // only hfs_mmap_end's cleanup releases it once the request has ended.
 // Of the p84 observation-lease checks only this first assertion fails on the
 // pre-p84 source; the later ones pin the recall semantics around it.
 static struct hfs_lease observed_lease;static struct hfs_view_ticket observed_credit={.control=true};
 struct hfs_observation observation={.view=session.view,.lease=&observed_lease};
 kref_init(&observed_lease.refs);observed_lease.release_ticket=&observed_credit;observed_lease.published=true;
 INIT_LIST_HEAD(&observation.list);list_add_tail(&observation.list,&session.view->observations);session.view->nr_observations++;
 assert(hfs_mmap_pressure(session.view,false)==-EAGAIN && !releases && !observed_lease.released && !observed_lease.revoked);
 for(unsigned int i=0;i<128;i++)clean_page(session.view,&mapping,i);
 assert(session.view->nr_cached==128);struct hfs_view_stamp old_stamp;struct hfs_cache_page *extra=NULL;
 // p84 review: a transport-triggered (background) pass releases no cache
 // lease either while that observation is in flight; it would recall the
 // domain under the request. This fails on the first p84 commit. The hold
 // is bounded: it still holds one jiffy before HFS_PRESSURE_HOLD_MS.
 jiffies=1000;
 assert(hfs_mmap_pressure(session.view,true)==-EAGAIN && !releases && session.view->nr_cached==128 && !observed_lease.revoked);
 assert(session.view->pressure_holding && session.view->pressure_held_since==1000);
 jiffies=1000+msecs_to_jiffies(HFS_PRESSURE_HOLD_MS)-1;
 assert(hfs_mmap_pressure(session.view,true)==-EAGAIN && !releases && session.view->nr_cached==128);
 // A capacity caller (129th page ENOSPC) ignores the hold and releases now.
 assert(!hfs_view_snapshot(session.view,&old_stamp));assert(hfs_view_page_prepare(session.view,&old_stamp,&extra)==-ENOSPC && !extra);
 assert(hfs_mmap_fault_error(session.view,-ENOSPC)==VM_FAULT_NOPAGE);
 assert(releases==1 && session.view->phase==HFS_VIEW_LIVE && !session.view->nr_cached && session.view->revision>old_stamp.revision);
 // Pressure released a cache lease. The recall it started still stopped the
 // view and revoked the in-flight observation without waiting for it or
 // releasing it: STOP never depends on an observation's request ending.
 assert(last_release!=&observed_lease && !observed_lease.released && observed_lease.revoked);
 assert(!hfs_view_snapshot(session.view,&old_stamp));assert(!hfs_view_page_prepare(session.view,&old_stamp,&extra));hfs_view_page_put(extra);
 // p84 round 2: past the bound the held background pass releases one cache
 // lease although the observation is still in flight (host acquisition
 // capacity may depend on it), never the observation lease, and restarts.
 jiffies=1000+msecs_to_jiffies(HFS_PRESSURE_HOLD_MS);clean_page(session.view,&mapping,0);
 assert(!hfs_mmap_pressure(session.view,true) && releases==2 && last_release==&leases[0] && !session.view->nr_cached);
 assert(!session.view->pressure_holding && !observed_lease.released);
 // Read-first page_mkwrite uses the exact original page, unlocks before its
 // funded RELEASE, and retries only after actual stopped cache invalidation.
 // The observation is still in flight, so a background flag on mkwrite would
 // hold here and fail.
 clean_page(session.view,&mapping,0);struct vm_fault vmf={&vma,&folios[0].page};struct page *original=vmf.page;
 assert(hfs_mmap_mkwrite(&vmf)==VM_FAULT_NOPAGE && vmf.page==original);
 assert(releases==3 && session.view->phase==HFS_VIEW_LIVE && !session.view->nr_cached && !folios[0].page.private);
 list_del(&observation.list);session.view->nr_observations--;
 // With no observation in flight the background pass keeps its purpose and
 // releases a published cache lease at once, starting the same recall.
 clean_page(session.view,&mapping,0);
 assert(!hfs_mmap_pressure(session.view,true) && releases==4 && last_release==&leases[0] && !session.view->nr_cached);
 // ie4: hfs_mmap_begin's re-check retry and hfs_attr_restamp, on this
 // LIVE view with no cache page and no observation in flight.
 observation_retry_checks(&session);
 // A source verifier refusal cannot manufacture DRAINED or reopen.
 clean_page(session.view,&mapping,0);invalid_source=1;
 assert(hfs_mmap_mkwrite(&vmf)==VM_FAULT_SIGBUS && session.view->phase==HFS_VIEW_STOPPED);
 assert(releases==5 && session.view->nr_cached==1);invalid_source=0;
 // Explicit fixture teardown of the denied stopped cache, not authority proof.
 assert(!invalidate_inode_pages2(&mapping));
 // Actual drain result validation and actual DRAINED registry gate: short and
 // error results retain the immutable obligation and can never close it.
 struct hfs_request request={.terminal=true};struct hfs_cache_page entry={0};
 struct hfs_dirty dirty={.adopted=true,.frozen=true,.request=&request,.entry=&entry,.length=4};
 INIT_LIST_HEAD(&dirty.list);list_add_tail(&dirty.list,&session.view->dirty);session.view->nr_dirty=1;
 session.view->phase=HFS_VIEW_INSTALLED_DRAIN;
 assert(!hfs_json_parse("{\"code\":\"OK\",\"count\":3}",strlen("{\"code\":\"OK\",\"count\":3}"),&request.result));
 assert(hfs_dirty_drain(session.view,&dirty)==-EIO && mapping_error==-EIO && !dirty.resolved);
 assert(hfs_mmap_finish_drained(session.view)==-EBUSY && session.view->phase==HFS_VIEW_INSTALLED_DRAIN);
 hfs_json_free(request.result);assert(!hfs_json_parse("{\"code\":\"IO\"}",13,&request.result));
 assert(hfs_dirty_drain(session.view,&dirty)==-EIO && !dirty.resolved);
 assert(hfs_mmap_finish_drained(session.view)==-EBUSY && session.view->phase!=HFS_VIEW_DRAINED);
 hfs_json_free(request.result);list_del_init(&dirty.list);session.view->nr_dirty=0;
 // 1a4: a fault parked behind a recall that never finishes gets SIGBUS once
 // hfs_view_enter times out; retrying it would restore the unbounded wait.
 assert(hfs_mmap_fault_error(session.view,-ETIMEDOUT)==VM_FAULT_SIGBUS);
 // 1a4 review (pre-existing): a refusal no retry can cure gets SIGBUS, not
 // NOPAGE. Before, a fault on a fenced view retried forever, and a kernel
 // uaccess fault could not even be killed. A transient one still retries.
 assert(hfs_mmap_fault_error(session.view,-ESTALE)==VM_FAULT_NOPAGE);
 assert(hfs_mmap_fault_error(session.view,-EAGAIN)==VM_FAULT_NOPAGE);
 test_fatal_signal=true;
 assert(hfs_mmap_fault_error(session.view,-ERESTARTSYS)==VM_FAULT_SIGBUS);
 assert(hfs_mmap_fault_error(session.view,-ESTALE)==VM_FAULT_SIGBUS);
 test_fatal_signal=false;
 session.blocked=true;assert(hfs_mmap_fault_error(session.view,-ESTALE)==VM_FAULT_SIGBUS);session.blocked=false;
 session.closing=true;assert(hfs_mmap_fault_error(session.view,-EAGAIN)==VM_FAULT_SIGBUS);session.closing=false;
 {
  enum hfs_view_phase phase=session.view->phase;int before=releases;
  session.view->phase=HFS_VIEW_FENCED;
  assert(hfs_mmap_fault_error(session.view,-ESTALE)==VM_FAULT_SIGBUS);
  assert(hfs_mmap_fault_error(session.view,-ENOSPC)==VM_FAULT_SIGBUS && releases==before);
  session.view->phase=phase;
 }
 view_worker_log_checks(&session);
 hfs_json_free(target_event);hfs_view_unregister_inode(&inode.inode);hfs_view_fence(session.view);assert(!hfs_view_free(&session));
 assert(!reservations && !module_refs);
 puts("PASS: actual128-clean pressure/129th admission,mkwrite release/stop/install/reopen,in-flight observation lease never released,background pass held (bounded) while observing,observation re-check retry (same-target re-stamp,no self-recall loop,moved target,refusals,deadline,no leak),source denial,short/error neverDRAINED; primitive VFS/PTE/source boundaries only");return 0;
}
