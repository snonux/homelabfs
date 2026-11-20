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
struct host_lease;
static struct host_lease *host_of(const struct hfs_lease *lease);
static void host_release(struct host_lease *lease);
static unsigned int sync_releases;
static int hfs_lease_release(struct hfs_session *session,struct hfs_lease *lease)
{
 struct hfs_view *view=session->view;u64 seq=view->sequence+1;int target=hfs_json_field(target_event,0,"target"),err;
 if(lease<leases || lease>=leases+ARRAY_SIZE(leases)){ // re4: a held observation lease
  assert(!lease->published && !lease->released);
  host_release(host_of(lease));lease->released=true;sync_releases++;return 0;
 }
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
 * acquisition and release are a model of the host controller
 * (internal/viewcontrol): a recall deactivates every lease of the domain
 * (quiesceParticipants) and advances its fence; a grant shares the active
 * lease of the same lineage (fence included, grantLease -> shareLease) or
 * makes a new one; a release of a shared lease only returns a holder
 * (releaseHolder), and any other release starts a recall while an active
 * lease of its lineage remains (releaseNeedsRecall), counted in
 * host_release_recalls. Such a recall runs while the next VIEW_ACQUIRE is
 * PENDING (pending_recall), as the release recall of R15-1 did. retry_hook
 * plays what else happens around one acquisition (a recall before or after
 * the grant, a fence, a session block, a recall that never ends); the first
 * stale_targets grants carry another target than the installed one. Queued
 * cleanup packets are recorded and released by run_cleanups, which plays
 * hfs_mmap_cleanup.
 */
enum retry_hook { HOOK_NONE, HOOK_RECALL_BEFORE, HOOK_RECALL_AFTER, HOOK_FENCE, HOOK_BLOCK, HOOK_STUCK, HOOK_RETIRED_ID };
struct host_lease { u64 fence; u8 target; bool active; int sharers, holders; };
static struct host_lease host_leases[32];
static unsigned int nr_host_leases, host_release_recalls;
static u64 host_fence=1;
static bool pending_recall;
static enum retry_hook retry_hook;
static unsigned int acquire_calls, stale_targets, nr_queued, queued_at_acquire[64];
static unsigned long acquire_jiffies;
static int leases_live, control_refs;
static struct hfs_lease *granted[64];
static struct hfs_cache_cleanup *queued[64];
static struct hfs_mmap_control control_fixture;
static int provider_leaves;
/* re4: the host's capacity model. While host_tickets_full, every
 * acquisition from host_full_from on is answered BLOCKED;
 * host_always_blocked answers BLOCKED whatever is retired. Every acquisition,
 * BLOCKED included, takes one of HOST_TICKETS ticket numbers
 * (host_tickets_used, admitNew) until a VIEW_RETIRE passes it. VIEW_RETIRE is
 * BLOCKED (retirementBlocked) while a recall runs (host_recall_running, or a
 * release recall pending) or a lease with holders is active (one credential
 * in this model); blocked_until_retires: the recall ends after that many
 * BLOCKED retirements. retire_busy: that many hfs_view_retire_try calls find
 * another round holding the retirement mutex (-EBUSY). retire_jiffies: a
 * round's duration. The cleanup worker (run_cleanups, the retire work) retires
 * after each packet as hfs_mmap_cleanup does; its calls are counted apart
 * (worker_retires), and test_pause_hook lets it run during a back-off pause.
 */
#define HOST_TICKETS 16U
static bool host_tickets_full, host_always_blocked, host_recall_running, retire_queued, in_worker;
static unsigned int retire_busy, worker_retires;
static unsigned int host_full_from, retire_calls, retire_blocked_calls, blocked_until_retires;
static unsigned int host_tickets_used;
static unsigned long retire_jiffies;
static struct hfs_mmap_control *hfs_mmap_control_get(struct hfs_view *view)
{ (void)view;control_refs++;return &control_fixture; }
static void hfs_mmap_control_put(struct hfs_mmap_control *control)
{ assert(control==&control_fixture && control_refs>0);control_refs--; }
static void hfs_mmap_cleanup(struct work_struct *work) { (void)work;abort(); }
static bool test_queue_cleanup(struct delayed_work *work)
{
 struct hfs_cache_cleanup *cleanup=container_of(work,struct hfs_cache_cleanup,work);
 if(work==&control_fixture.retire){ // re4: the retire work, queued at most once
  if(retire_queued)return false;
  retire_queued=true;return true;
 }
 for(unsigned int i=0;i<nr_queued;i++)assert(queued[i]!=cleanup); // never queued twice
 assert(nr_queued<ARRAY_SIZE(queued));queued[nr_queued++]=cleanup;return true;
}
static void provider_leave(void *session,void *cookie) { assert(session==test_session && !cookie);provider_leaves++; }
static struct host_lease *host_of(const struct hfs_lease *lease) { return &host_leases[lease->id[0]]; }
/* A complete recall that reinstalls the same target (as hfs_lease_release's
 * fixture), with the host's lease deactivation and fence advance.
 */
static void same_target_recall(struct hfs_view *view)
{
 u64 seq=view->sequence+1;int target=hfs_json_field(target_event,0,"target");
 recall.b[0]++;
 assert(!hfs_view_stop(view,&generation,&recall,seq++));assert(!hfs_view_seal(view));
 for(unsigned int i=0;i<nr_host_leases;i++)host_leases[i].active=false;
 assert(!hfs_mmap_install(view,target_event,target,&recall,seq++,false));
 assert(!hfs_mmap_finish_drained(view));
 assert(!hfs_mmap_install(view,target_event,target,&recall,seq++,true));
 assert(!hfs_view_reopen(view,&recall,seq));host_fence++;
}
static unsigned int host_grant(u8 target)
{
 for(unsigned int i=0;i<nr_host_leases;i++)
  if(host_leases[i].active && host_leases[i].fence==host_fence && host_leases[i].target==target){host_leases[i].sharers++;host_leases[i].holders++;return i;}
 assert(nr_host_leases<ARRAY_SIZE(host_leases));
 host_leases[nr_host_leases]=(struct host_lease){.fence=host_fence,.target=target,.active=true,.holders=1};
 return nr_host_leases++;
}
static void host_release(struct host_lease *lease)
{
 assert(lease->holders>0);lease->holders--;
 if(lease->sharers){lease->sharers--;return;}
 for(unsigned int i=0;i<nr_host_leases;i++)
  if(host_leases[i].active && host_leases[i].fence==lease->fence && host_leases[i].target==lease->target){host_release_recalls++;pending_recall=true;return;}
}
/* hfs_lease_acquire_ex's host: -EAGAIN with no lease is BLOCKED (*blocked)
 * or RETIRED_ID (not *blocked).
 */
int hfs_lease_acquire_ex(struct hfs_session *session,const struct hfs_cred *cred,
			 const struct hfs_cap *object,const struct hfs_cap *description,
			 u64 offset,bool dirty,const struct hfs_buf *expected_target,
			 struct hfs_lease **out,bool *blocked)
{
 struct hfs_view *view=session->view;struct hfs_lease *lease=calloc(1,sizeof(*lease));
 enum retry_hook hook=acquire_calls ? HOOK_NONE : retry_hook;
 assert(session==test_session && cred && !object && !description && !offset && !dirty && blocked);
 assert(expected_target && expected_target->len && lease && acquire_calls<ARRAY_SIZE(granted));
 queued_at_acquire[acquire_calls]=nr_queued;*blocked=false;
 assert(++host_tickets_used<=HOST_TICKETS); // never past the host's ticket budget
 jiffies+=acquire_jiffies;
 if(hook==HOOK_BLOCK){session->bridge_minor=4;session->blocked=true;}
 // A recall runs on the host whatever the next acquisition is answered.
 if(pending_recall || hook==HOOK_RECALL_BEFORE){pending_recall=false;same_target_recall(view);}
 else if(hook==HOOK_FENCE)hfs_view_fence(view);
 if(host_always_blocked || (host_tickets_full && acquire_calls>=host_full_from)){
  free(lease);acquire_calls++;*out=NULL;*blocked=true;return -EAGAIN; // BLOCKED: no lease
 }
 if(hook==HOOK_RETIRED_ID){free(lease);acquire_calls++;*out=NULL;return -EAGAIN;} // no lease
 kref_init(&lease->refs);memcpy(lease->target_hash,view->target_hash,32);
 if(acquire_calls<stale_targets)lease->target_hash[0]^=1;
 lease->id[0]=(u8)host_grant(lease->target_hash[0]);lease->fence=host_of(lease)->fence;
 if(hook==HOOK_RECALL_AFTER)same_target_recall(view);
 else if(hook==HOOK_STUCK)view->phase=HFS_VIEW_RECALLING;
 granted[acquire_calls++]=lease;leases_live++;*out=lease;return 0;
}
static bool host_retire_refused(void)
{
 if(host_recall_running || pending_recall)return true;
 for(unsigned int i=0;i<nr_host_leases;i++)if(host_leases[i].active && host_leases[i].holders)return true;
 return false;
}
/* hfs_view_retire's host (re4): a refusal is recorded in
 * view->retire_blocked, as the production pass does; otherwise the tickets
 * retire and their slots are free again (0 only when a floor advanced).
 */
static int host_retire(struct hfs_session *session,bool try)
{
 assert(session==test_session);
 if(in_worker)worker_retires++;else retire_calls++;
 if(try && retire_busy){retire_busy--;return -EBUSY;}
 jiffies+=retire_jiffies;
 if(host_retire_refused()){
  if(!in_worker)retire_blocked_calls++;
  session->view->retire_blocked=true;
  if(blocked_until_retires && retire_blocked_calls==blocked_until_retires)host_recall_running=false;
  return -EAGAIN;
 }
 session->view->retire_blocked=false;
 if(!host_tickets_used)return -EAGAIN;
 host_tickets_used=0;host_tickets_full=false;session->view->retire_progress++;return 0;
}
int hfs_view_retire(struct hfs_session *session) { return host_retire(session,false); }
int hfs_view_retire_try(struct hfs_session *session) { return host_retire(session,true); }
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
   if(!cleanup->lease->released)host_release(host_of(cleanup->lease)); // re4: released at once
   free(cleanup->lease);leases_live--;
  }
  list_del(&cleanup->list);unreserve(test_session,cleanup->credit);free(cleanup);
  in_worker=true;hfs_view_retire(test_session);in_worker=false; // as hfs_mmap_cleanup
  hfs_mmap_control_put(&control_fixture);
 }
 nr_queued=0;
}
static int retry_begin(enum retry_hook hook,unsigned int stale,void **cookie)
{
 static struct inode inode;static int cred;
 retry_hook=hook;stale_targets=stale;acquire_calls=0;host_release_recalls=0;
 retire_calls=retire_blocked_calls=test_sleeps=sync_releases=host_tickets_used=worker_retires=0;
 return hfs_mmap_begin(&inode,false,(const struct hfs_cred *)(void *)&cred,NULL,cookie);
}
/* The published observation holds an active host lease at the view's
 * current stamp.
 */
static void retry_published(struct hfs_view *view,void *cookie)
{
 struct hfs_observation *observation=cookie;
 assert(observation && observation->lease && observation->lease->published);
 assert(host_of(observation->lease)->active && observation->stamp.revision==view->revision);
 assert(view->nr_observations==1);
}
static void retry_end(void *cookie)
{
 unsigned int before=nr_queued;
 hfs_mmap_end(cookie);assert(nr_queued==before+1);run_cleanups();
}
static void retry_no_leaks(int reserved)
{
 struct hfs_view *view=test_session->view;
 // A release recall nobody waited for still runs before the next case, and
 // its REOPENED kick re-sends a refused retirement (hfs_mmap_process); the
 // worker also runs a retire work an observation queued.
 if(pending_recall){pending_recall=false;same_target_recall(view);}
 hfs_mmap_retire_kick(view,true);
 if(retire_queued){
  retire_queued=false;in_worker=true;
  hfs_mmap_retire_work((struct work_struct *)(void *)&control_fixture.retire);in_worker=false;
 }
 assert(!nr_queued && !leases_live && !control_refs && reservations==reserved && !view->retire_blocked);
 assert(list_empty(&control_fixture.cleanup) && !view->nr_observations);
}
/* R15-1's case: a recall completes while VIEW_ACQUIRE is PENDING and the
 * host grants the lease after it. Before ie4 this was -ESTALE. Now the
 * observation acquires again, sharing that lease, and only then releases
 * the first reference, which starts no recall.
 */
static void retry_recall_before_grant(struct hfs_view *view,int reserved)
{
 void *cookie;u64 before=view->revision;
 assert(!retry_begin(HOOK_RECALL_BEFORE,0,&cookie));retry_published(view,cookie);
 assert(acquire_calls==2 && view->revision>before && !queued_at_acquire[1]);
 assert(granted[0]->id[0]==granted[1]->id[0] && host_of(granted[1])->sharers==1);
 assert(nr_queued==1 && queued[0]->lease==granted[0]);
 assert(strstr(last_warning,"observation re-check failed (view moved past the granted lease); acquiring again before releasing it"));
 run_cleanups();assert(!host_release_recalls && host_of(granted[1])->active);
 retry_end(cookie);assert(provider_leaves==1 && host_release_recalls==1);retry_no_leaks(reserved);
}
/* Review round 1: a recall between the grant and the re-check quiesces the
 * unpublished lease on the host although its target is unchanged. It must
 * not be published: the observation acquires again (a new lease at the new
 * fence) and the quiesced lease's release starts no recall.
 */
static void retry_recall_after_grant(struct hfs_view *view,int reserved)
{
 void *cookie;
 assert(!retry_begin(HOOK_RECALL_AFTER,0,&cookie));retry_published(view,cookie);
 assert(acquire_calls==2 && !host_of(granted[0])->active && granted[1]->fence==granted[0]->fence+1);
 assert(nr_queued==1 && queued[0]->lease==granted[0] && !granted[0]->published);
 run_cleanups();assert(!host_release_recalls);
 retry_end(cookie);retry_no_leaks(reserved);
}
/* The self-recall loop: each operation's final release starts a recall that
 * runs during the next operation's VIEW_ACQUIRE. Each operation needs two
 * acquisitions and its stale reference's release starts no recall, so the
 * recalls never feed back into the same operation.
 */
static void retry_self_recall(struct hfs_view *view,int reserved)
{
 void *cookie;
 for(int i=0;i<3;i++) {
  assert(!retry_begin(HOOK_NONE,0,&cookie));retry_published(view,cookie);
  assert(acquire_calls==(i?2U:1U) && nr_queued==(i?1U:0U));
  run_cleanups();assert(!host_release_recalls);
  retry_end(cookie);assert(host_release_recalls==1 && pending_recall);
 }
 retry_no_leaks(reserved);
}
/* A grant at another target than the installed one: release and acquire
 * again, the stale lease going only after the retry's own acquisition. The
 * retry's lease is another lineage (another target), so in this model the
 * still-active stale lease is its own lineage's last holder and its release
 * starts a recall; it comes after this observation was published.
 */
static void retry_moved_target(struct hfs_view *view,int reserved)
{
 void *cookie;
 assert(!retry_begin(HOOK_NONE,1,&cookie));retry_published(view,cookie);
 assert(acquire_calls==2 && !queued_at_acquire[0] && !queued_at_acquire[1]);
 assert(nr_queued==1 && queued[0]->lease==granted[0] && !granted[0]->published);
 assert(((struct hfs_observation *)cookie)->lease==granted[1] && granted[0]->id[0]!=granted[1]->id[0]);
 run_cleanups();assert(host_release_recalls==1);
 retry_end(cookie);retry_no_leaks(reserved);
}
/* Final review S1, kept by re4: a RETIRED_ID (-EAGAIN, no lease, not BLOCKED)
 * is returned at once: no re-check line, no back-off, no retirement, no
 * back-to-back VIEW_ACQUIRE.
 */
static void retry_host_blocked(int reserved)
{
 void *cookie=(void *)1;unsigned int lines=warnings;
 assert(retry_begin(HOOK_RETIRED_ID,0,&cookie)==-EAGAIN && !cookie && acquire_calls==1);
 assert(nr_queued==1 && !queued[0]->lease && warnings==lines && !test_sleeps && !retire_calls);
 run_cleanups();retry_no_leaks(reserved);
}
/* re4 (zq3-r16 R16-1): the host answers BLOCKED for tickets that a
 * VIEW_RETIRE can free. The observation pauses 50 ms, retires (progress) and
 * acquires again within the same deadline (pre-re4: -EAGAIN at once).
 */
static void retry_blocked_tickets(struct hfs_view *view,int reserved)
{
 void *cookie;unsigned long start=jiffies;
 host_tickets_full=true;host_full_from=0;
 assert(!retry_begin(HOOK_NONE,0,&cookie));retry_published(view,cookie);
 assert(acquire_calls==2 && retire_calls==1 && test_sleeps==1 && jiffies-start==50);
 assert(nr_queued==1 && !queued[0]->lease && queued_at_acquire[1]==1);
 assert(strstr(last_warning,"observation VIEW_ACQUIRE BLOCKED by the host (attempt 1); retiring, then retrying after 50 ms if that frees anything"));
 run_cleanups();retry_end(cookie);retry_no_leaks(reserved);
}
/* R16-1's exact shape: ie4 holds a stale lease while it acquires again and
 * that acquisition is BLOCKED. The held lease is released at once (its
 * release starts a recall, which refuses the first retirement; the completed
 * release is the progress), queued before the next acquisition, and the
 * retirement after the recall frees the tickets. Every packet is queued once.
 */
static void retry_blocked_held(struct hfs_view *view,int reserved)
{
 void *cookie;
 host_tickets_full=true;host_full_from=1;
 assert(!retry_begin(HOOK_NONE,1,&cookie));retry_published(view,cookie);
 assert(acquire_calls==4 && sync_releases==1 && granted[0]->released && host_release_recalls==1);
 assert(retire_calls==2 && retire_blocked_calls==1 && test_sleeps==2 && !view->retire_blocked);
 assert(!queued_at_acquire[1] && queued_at_acquire[2]==2);
 assert(nr_queued==3 && queued[0]->lease==granted[0] && !granted[0]->published);
 assert(!queued[1]->lease && !queued[2]->lease);
 assert(((struct hfs_observation *)cookie)->lease==granted[3]);
 run_cleanups();retry_end(cookie);retry_no_leaks(reserved);host_full_from=0;
}
/* A host that keeps answering BLOCKED although each retirement advances: at
 * most HFS_BLOCKED_RETRIES_MAX retries (50+100+200 ms), then the host's
 * -EAGAIN with one line; every packet queued once, nothing leaked, and the
 * acquisition fixture asserts the 16-ticket budget throughout.
 */
static void retry_blocked_persistent(int reserved)
{
 void *cookie=(void *)1;unsigned long start=jiffies;
 host_always_blocked=true;
 assert(retry_begin(HOOK_NONE,0,&cookie)==-EAGAIN && !cookie);
 assert(acquire_calls==HFS_BLOCKED_RETRIES_MAX+1 && nr_queued==acquire_calls);
 assert(retire_calls==3 && test_sleeps==3 && jiffies-start==350);
 for(unsigned int i=0;i<nr_queued;i++)assert(!queued[i]->lease);
 assert(strstr(last_warning,"observation VIEW_ACQUIRE BLOCKED, retry limit reached after 4 attempts (-EAGAIN)"));
 host_always_blocked=false;run_cleanups();retry_no_leaks(reserved);
}
/* Review round 1: a BLOCKED that retirement cannot clear is -EAGAIN after one
 * acquisition and one short pause: another observation of the same
 * credentials holds an active lease (retirementBlocked), the retirement mutex
 * is busy (never waited for), or the retirement round ran past the deadline.
 */
static void retry_blocked_no_progress(struct hfs_view *view,int reserved)
{
 void *holder,*cookie=(void *)1;unsigned long start;
 assert(!retry_begin(HOOK_NONE,0,&holder));retry_published(view,holder);
 host_tickets_full=true;start=jiffies;
 assert(retry_begin(HOOK_NONE,0,&cookie)==-EAGAIN && !cookie && acquire_calls==1);
 assert(retire_calls==1 && retire_blocked_calls==1 && test_sleeps==1 && jiffies-start==50);
 assert(strstr(last_warning,"observation VIEW_ACQUIRE BLOCKED, no retirement progress after 1 attempts (-EAGAIN)"));
 assert(view->retire_blocked && nr_queued==1);
 run_cleanups();retry_end(holder);retry_no_leaks(reserved);
 // A retirement mutex that stays busy: each busy try pauses again, against
 // the cap; still one acquisition.
 retire_busy=~0U;host_tickets_full=true;
 assert(retry_begin(HOOK_NONE,0,&cookie)==-EAGAIN && acquire_calls==1 && retire_calls==3 && test_sleeps==3);
 assert(strstr(last_warning,"observation VIEW_ACQUIRE BLOCKED, retry limit reached after 4 attempts (-EAGAIN)"));
 retire_busy=0;run_cleanups();retry_no_leaks(reserved);
 retire_jiffies=HFS_VIEW_ENTER_WAIT_MS;host_tickets_full=true;
 assert(retry_begin(HOOK_NONE,0,&cookie)==-EAGAIN && acquire_calls==1 && retire_calls==1 && !host_tickets_full);
 assert(strstr(last_warning,"observation VIEW_ACQUIRE BLOCKED, deadline exhausted after 1 attempts (-EAGAIN)"));
 retire_jiffies=0;run_cleanups();retry_no_leaks(reserved);
}
/* Final review S1: the BLOCKED attempt's own packet reaches the cleanup
 * worker before the pause ends, and the worker's retirement frees the
 * tickets. The back-off's own try then finds nothing left (-EAGAIN) or the
 * mutex busy (-EBUSY), but the floor advanced since the acquisition
 * (view->retire_progress), so it retries and is granted.
 */
static void retry_blocked_worker(struct hfs_view *view,int reserved)
{
 void *cookie;
 host_tickets_full=true;test_pause_hook=run_cleanups;
 assert(!retry_begin(HOOK_NONE,0,&cookie));retry_published(view,cookie);
 assert(acquire_calls==2 && test_sleeps==1 && worker_retires==1 && retire_calls==1 && !nr_queued);
 test_pause_hook=NULL;run_cleanups();retry_end(cookie);retry_no_leaks(reserved);
 host_tickets_full=true;test_pause_hook=run_cleanups;retire_busy=1;
 assert(!retry_begin(HOOK_NONE,0,&cookie));retry_published(view,cookie);
 assert(acquire_calls==2 && test_sleeps==1 && worker_retires==1 && retire_calls==1 && !retire_busy);
 test_pause_hook=NULL;run_cleanups();retry_end(cookie);retry_no_leaks(reserved);
 // A busy try without progress yet pauses again; the next try retires.
 host_tickets_full=true;retire_busy=1;
 assert(!retry_begin(HOOK_NONE,0,&cookie));retry_published(view,cookie);
 assert(acquire_calls==2 && test_sleeps==2 && retire_calls==2 && !retire_busy);
 run_cleanups();retry_end(cookie);retry_no_leaks(reserved);
}
/* oc4's stop conditions end the back-off before any pause or retirement: a
 * fatal signal (-EINTR), transport_failed and a blocked hfs1.4 session
 * (-ESTALE).
 */
static void retry_blocked_stops(struct hfs_view *view,int reserved)
{
 void *cookie=(void *)1;
 host_always_blocked=true;test_fatal_signal=true;
 assert(retry_begin(HOOK_NONE,0,&cookie)==-EINTR && !cookie && acquire_calls==1);
 assert(!test_sleeps && !retire_calls && nr_queued==1);
 test_fatal_signal=false;run_cleanups();retry_no_leaks(reserved);
 test_session->transport_failed=true;
 assert(retry_begin(HOOK_NONE,0,&cookie)==-ESTALE && acquire_calls==1 && !test_sleeps && nr_queued==1);
 test_session->transport_failed=false;run_cleanups();retry_no_leaks(reserved);
 assert(retry_begin(HOOK_BLOCK,0,&cookie)==-ESTALE && acquire_calls==1 && !test_sleeps && nr_queued==1);
 test_session->bridge_minor=0;test_session->blocked=false;
 host_always_blocked=false;run_cleanups();retry_no_leaks(reserved);
 assert(view->phase==HFS_VIEW_LIVE);
}
static void run_retire_work(void)
{
 assert(retire_queued);retire_queued=false;
 hfs_mmap_retire_work((struct work_struct *)(void *)&control_fixture.retire);
}
/* A BLOCKED VIEW_RETIRE (a recall runs) is offered again, not only at the
 * next cleanup: after the recall's REOPENED (hfs_mmap_process's forced
 * kick) and by the next observation (at most once per HFS_RETIRE_KICK_MS).
 */
static void retry_blocked_retire(struct hfs_view *view,int reserved)
{
 void *cookie=(void *)1;
 // BLOCKED during a recall: the retirement is refused too, so -EAGAIN at
 // once with the refusal recorded; REOPENED's kick then retires, and the
 // next observation is granted.
 host_tickets_full=true;host_recall_running=true;
 assert(retry_begin(HOOK_NONE,0,&cookie)==-EAGAIN && acquire_calls==1 && view->retire_blocked);
 run_cleanups();host_recall_running=false;
 hfs_mmap_retire_kick(view,true);hfs_mmap_retire_kick(view,true); // a second kick drops its reference
 assert(retire_queued && control_refs==1 && retire_calls==1);
 run_retire_work();assert(retire_calls==2 && !view->retire_blocked && !control_refs && !host_tickets_full);
 hfs_mmap_retire_kick(view,true);assert(!retire_queued && !control_refs); // nothing owed
 assert(!retry_begin(HOOK_NONE,0,&cookie));retry_published(view,cookie);
 assert(acquire_calls==1 && !retire_queued);
 run_cleanups();retry_end(cookie);retry_no_leaks(reserved);
 // Unforced kicks are rate-limited while the refusal persists.
 host_recall_running=true;retire_calls=0;
 assert(hfs_view_retire(test_session)==-EAGAIN && view->retire_blocked);
 jiffies+=HFS_RETIRE_KICK_MS;hfs_mmap_retire_kick(view,false);
 run_retire_work();assert(retire_calls==2 && view->retire_blocked && !control_refs);
 hfs_mmap_retire_kick(view,false);assert(!retire_queued && !control_refs);
 jiffies+=HFS_RETIRE_KICK_MS;hfs_mmap_retire_kick(view,false);assert(retire_queued);
 // A stopping control drops the work's reference without an RPC.
 control_fixture.stopping=true;run_retire_work();
 assert(retire_calls==2 && !control_refs);control_fixture.stopping=false;
 // The next observation (a second later) kicks it before its acquisition.
 host_recall_running=false;jiffies+=HFS_RETIRE_KICK_MS;
 assert(!retry_begin(HOOK_NONE,0,&cookie));retry_published(view,cookie);
 assert(retire_queued && acquire_calls==1 && control_refs==2);
 run_retire_work();assert(retire_calls==1 && control_refs==1);
 retry_end(cookie);retry_no_leaks(reserved);view->retire_blocked=false;
}
/* Refusals keep -ESTALE (a fenced view, a blocked hfs1.4 session, a failed
 * transport), and their one lease is released once.
 */
static void retry_refused(struct hfs_view *view,int reserved)
{
 void *cookie=(void *)1;
 assert(retry_begin(HOOK_FENCE,0,&cookie)==-ESTALE && !cookie);
 assert(acquire_calls==1 && nr_queued==1 && queued[0]->lease==granted[0] && !granted[0]->published);
 run_cleanups();view->phase=HFS_VIEW_LIVE;retry_no_leaks(reserved); // fixture: unfence
 assert(retry_begin(HOOK_BLOCK,0,&cookie)==-ESTALE && acquire_calls==1 && nr_queued==1);
 run_cleanups();test_session->bridge_minor=0;test_session->blocked=false;retry_no_leaks(reserved);
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
 assert(retry_begin(HOOK_STUCK,0,&cookie)==-ETIMEDOUT && acquire_calls==1 && nr_queued==2);
 assert(queued[0]->lease==granted[0] && !queued[1]->lease);
 assert(strstr(last_warning,"observation retries exhausted after 1 failed re-checks"));
 run_cleanups();view->phase=HFS_VIEW_LIVE;retry_no_leaks(reserved); // fixture: end the recall
}
/* hfs_attr_install's cache-observation re-check: a request stamped before a
 * recall that reinstalled the same target is re-stamped and installs; a
 * moved target asks for a re-issue (-HFS_ETARGET_MOVED) without an entry
 * held; a fenced view is -ESTALE without a line.
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
 assert(hfs_attr_restamp(test_session,view,&request,&cookie)==-HFS_ETARGET_MOVED && !cookie && !view->active);
 assert(strstr(last_warning,"(target moved); not installed, re-issuing"));
 view->phase=HFS_VIEW_FENCED;lines=warnings;
 assert(hfs_attr_restamp(test_session,view,&request,&cookie)==-ESTALE && !cookie && warnings==lines);
 view->phase=HFS_VIEW_LIVE; // fixture: unfence
}
/* hfs_attr_enter: a mutation's delayed result with a stale stamp is skipped
 * (1); a cache observation's same-target recall is re-stamped (0).
 */
static void retry_attr_enter(struct hfs_view *view)
{
 struct hfs_request request={.view_stamped=true};struct inode inode={0};void *cookie;
 assert(!hfs_view_snapshot_target(view,&request.view_stamp,request.view_target));
 same_target_recall(view);
 assert(hfs_attr_enter(&inode,test_session,view,&request,&cookie)==1);
 request.cache_observation=true;
 assert(!hfs_attr_enter(&inode,test_session,view,&request,&cookie) && cookie);
 hfs_view_leave(cookie);
}
/* Final review S2: a LOOKUP whose target moved is re-issued; its first
 * result's host lookup count must be balanced by a FORGET, not dropped.
 */
static unsigned int cap_frees,cred_frees,nr_forgets_queued;
static struct hfs_forget *forgets_queued[4];
static void hfs_cap_free(struct hfs_cap *cap) { (void)cap;cap_frees++; }
static void hfs_cred_free(struct hfs_cred *cred) { (void)cred;cred_frees++; }
static void hfs_forget_work(struct work_struct *work) { (void)work;abort(); }
static bool test_queue_forget(struct work_struct *work)
{
 assert(nr_forgets_queued<ARRAY_SIZE(forgets_queued));
 forgets_queued[nr_forgets_queued++]=container_of(work,struct hfs_forget,work);return true;
}
static struct hfs_inode_seed forget_seed(struct hfs_cap *cap)
{
 struct hfs_forget *forget=calloc(1,sizeof(*forget));
 assert(forget);INIT_LIST_HEAD(&forget->list);forget->cap=cap;forget->count=1;
 forget->issuer=(struct hfs_cred *)(void *)cap; // an opaque non-NULL issuer
 return (struct hfs_inode_seed){.forget=forget,.cap=cap,.issuer=forget->issuer};
}
/* The queued FORGETs ran: each drops its record, balancing one lookup. */
static void forgets_sent(unsigned int expected)
{
 assert(nr_forgets_queued==expected);
 for(unsigned int i=0;i<nr_forgets_queued;i++) {
  struct hfs_forget *forget=forgets_queued[i];
  assert(forget->count==1 && forget->session==test_session && !forget->next);
  list_del(&forget->list);free(forget);
 }
 nr_forgets_queued=0;assert(list_empty(&test_session->forgets));
}
static void retry_lookup_forget(void)
{
 static struct hfs_cap cap;struct hfs_inode node={0};struct hfs_inode_seed seed;
 struct hfs_session *session=test_session;
 INIT_LIST_HEAD(&session->forgets);INIT_LIST_HEAD(&node.view_list);
 // A new inode: the moved LOOKUP's record is linked, and iget_failed's
 // eviction sends its FORGET at once; nothing is freed unsent.
 seed=forget_seed(&cap);node.forget=seed.forget;node.inode.i_state=I_NEW;
 hfs_inode_get_fail(&node.inode,&seed,session,-HFS_ETARGET_MOVED);
 assert(!cap_frees && !cred_frees && !node.forget);forgets_sent(1);
 // Any other failure of a new inode drops the unlinked seed without FORGET.
 seed=forget_seed(&cap);node.forget=seed.forget;
 hfs_inode_get_fail(&node.inode,&seed,session,-ESTALE);
 assert(cap_frees==1 && cred_frees==1 && !nr_forgets_queued);
 // An existing inode keeps the moved LOOKUP's record with its own; its
 // eviction sends both FORGETs: two lookups, two FORGETs.
 struct hfs_inode_seed first=forget_seed(&cap);
 node.inode.i_state=0;node.inode.refs=2;node.forget=first.forget;
 hfs_inode_link_forget(&node.inode,first.forget,session);
 seed=forget_seed(&cap);
 hfs_inode_get_fail(&node.inode,&seed,session,-HFS_ETARGET_MOVED);
 assert(node.inode.refs==1 && node.forget==seed.forget && seed.forget->next==first.forget);
 assert(cap_frees==1 && !nr_forgets_queued);
 hfs_evict_inode(&node.inode);forgets_sent(2);
 // An existing inode's other failure frees the seed it never linked.
 seed=forget_seed(&cap);node.inode.refs=1;
 hfs_inode_get_fail(&node.inode,&seed,session,-ESTALE);
 assert(!node.inode.refs && cap_frees==2 && cred_frees==2 && !nr_forgets_queued);
}
/* hfs_reissue: only -HFS_ETARGET_MOVED is re-issued, until the deadline, a
 * failed transport or a fatal signal.
 */
static void retry_reissue(struct hfs_session *session)
{
 int err=-ESTALE;unsigned long deadline=jiffies+10;
 assert(!hfs_reissue(session,&err,deadline,"GETATTR") && err==-ESTALE);
 err=-HFS_ETARGET_MOVED;assert(hfs_reissue(session,&err,deadline,"GETATTR"));
 session->transport_failed=true;
 assert(!hfs_reissue(session,&err,deadline,"GETATTR") && err==-ESTALE);session->transport_failed=false;
 err=-HFS_ETARGET_MOVED;test_fatal_signal=true;
 assert(!hfs_reissue(session,&err,deadline,"LOOKUP") && err==-EINTR);test_fatal_signal=false;
 err=-HFS_ETARGET_MOVED;jiffies=deadline;
 assert(!hfs_reissue(session,&err,deadline,"LOOKUP") && err==-ETIMEDOUT);
 assert(strstr(last_warning,"LOOKUP re-issues exhausted after the view target kept moving (-ETIMEDOUT)"));
}
static void observation_retry_checks(struct hfs_session *session)
{
 struct hfs_view *view=session->view;int reserved=atomic_load(&reservations);
 spin_lock_init(&control_fixture.lock);INIT_LIST_HEAD(&control_fixture.cleanup);control_fixture.view=view;
 assert(view->phase==HFS_VIEW_LIVE && view->target.len && !view->active && !view->nr_observations);
 retry_recall_before_grant(view,reserved);
 retry_recall_after_grant(view,reserved);
 retry_self_recall(view,reserved);
 retry_moved_target(view,reserved);
 retry_host_blocked(reserved);
 retry_blocked_tickets(view,reserved);
 retry_blocked_held(view,reserved);
 retry_blocked_persistent(reserved);
 retry_blocked_no_progress(view,reserved);
 retry_blocked_worker(view,reserved);
 retry_blocked_stops(view,reserved);
 retry_blocked_retire(view,reserved);
 retry_refused(view,reserved);
 retry_deadline(view,reserved);
 retry_attr(view);
 retry_attr_enter(view);
 retry_lookup_forget();
 retry_reissue(session);
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
 puts("PASS: actual128-clean pressure/129th admission,mkwrite release/stop/install/reopen,in-flight observation lease never released,background pass held (bounded) while observing,observation re-check retry (recall before/after grant re-acquires,no release recall,no self-recall loop,moved target,refusals,deadline,no leak,RETIRED_ID passed through,host BLOCKED retried only after retirement progress (held lease released first,at most 3 retries,ticket budget kept),progress by the cleanup worker during the pause or after a busy try,no progress/deadline -EAGAIN at once,BLOCKED retire re-sent after recall/next observation (rate-limited),attr re-stamp/re-issue,moved LOOKUP FORGET balanced),source denial,short/error neverDRAINED; primitive VFS/PTE/source boundaries only");return 0;
}
