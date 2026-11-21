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
static struct hfs_json *other_target_event; // 1h4: a recall that moves the target
int hfs_view_target(struct hfs_session *session,const struct hfs_json *event,int token,uuid_t *visibility,u8 hash[32])
{
 assert(session==test_session && (event==target_event || event==other_target_event) && token==hfs_json_field(event,0,"target"));
 if(invalid_source)return -EACCES;
 *visibility=generation;memset(hash,0,32);hash[0]=event==other_target_event;return 0;
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
/* 2f4: HOOK_PASSED is hfs_lease_acquire_ex's -EXDEV: a GRANTED that a later
 * recall passed before the view settled (hfs_view_grant_settle), handed out
 * with its lease for release.
 */
/* 1h4: HOOK_REINSTALL_BLOCKED is R19-1's shape: the acquisition was PENDING
 * while a recall reinstalled another target (the mount accepted its
 * REOPENED ACK), and re-admission answers BLOCKED for the old expectation.
 */
enum retry_hook { HOOK_NONE, HOOK_RECALL_BEFORE, HOOK_RECALL_AFTER, HOOK_FENCE, HOOK_BLOCK, HOOK_STUCK, HOOK_RETIRED_ID, HOOK_PASSED, HOOK_REINSTALL_BLOCKED };
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
 * BLOCKED retirements. retire_busy (1h4): that many rounds of another
 * thread (e.g. the REOPENED kick's) hold the real session->retirement mutex
 * (busy_start), so hfs_view_retire_try's trylock fails (-EBUSY, no round
 * ran); a blocking hfs_view_retire waits them out. busy_round_end, as
 * view_wait_hook, ends one per wake-up (every view_wait_step ms); with
 * busy_round_frees the last one advances a floor. Every round, busy or this
 * call's, ends through the actual hfs_retirement_unlock (hfs_view.c: unlock,
 * then view->retirement_releases, then the wake-up; review rounds 1 and 2),
 * as every production holder does; the rest of hfs_view_retire_mode is this
 * host model.
 * retire_ran_busy: that many rounds that run return -EBUSY themselves (a
 * retired ticket still in use). retire_jiffies:
 * a round's duration. The cleanup worker (run_cleanups, the retire work) retires
 * after each packet as hfs_mmap_cleanup does; its calls are counted apart
 * (worker_retires), and test_pause_hook lets it run during a back-off pause.
 */
#define HOST_TICKETS 16U
static bool host_tickets_full, host_always_blocked, host_recall_running, retire_queued, in_worker;
static unsigned int retire_busy, worker_retires;
static bool busy_round_frees;
static unsigned int retire_ran_busy;
static bool reinstall_always; // 1h4: every acquisition is HOOK_REINSTALL_BLOCKED's
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
static void target_recall(struct hfs_view *view,struct hfs_json *event)
{
 u64 seq=view->sequence+1;int target=hfs_json_field(event,0,"target");
 recall.b[0]++;
 assert(!hfs_view_stop(view,&generation,&recall,seq++));assert(!hfs_view_seal(view));
 for(unsigned int i=0;i<nr_host_leases;i++)host_leases[i].active=false;
 assert(!hfs_mmap_install(view,event,target,&recall,seq++,false));
 assert(!hfs_mmap_finish_drained(view));
 assert(!hfs_mmap_install(view,event,target,&recall,seq++,true));
 assert(!hfs_view_reopen(view,&recall,seq));host_fence++;
}
static void same_target_recall(struct hfs_view *view) { target_recall(view,target_event); }
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
 else if(hook==HOOK_REINSTALL_BLOCKED || reinstall_always){
  target_recall(view,view->target_hash[0] ? target_event : other_target_event);hfs_view_reopen_acked(view);
 }
 if(host_always_blocked || hook==HOOK_REINSTALL_BLOCKED || reinstall_always ||
    (host_tickets_full && acquire_calls>=host_full_from)){
  free(lease);acquire_calls++;*out=NULL;*blocked=true;return -EAGAIN; // BLOCKED: no lease
 }
 if(hook==HOOK_RETIRED_ID){free(lease);acquire_calls++;*out=NULL;return -EAGAIN;} // no lease
 kref_init(&lease->refs);memcpy(lease->target_hash,view->target_hash,32);
 if(acquire_calls<stale_targets)lease->target_hash[0]^=1;
 lease->id[0]=(u8)host_grant(lease->target_hash[0]);lease->fence=host_of(lease)->fence;
 if(hook==HOOK_RECALL_AFTER || hook==HOOK_PASSED)same_target_recall(view);
 else if(hook==HOOK_STUCK)view->phase=HFS_VIEW_RECALLING;
 lease->unadopted=hook==HOOK_PASSED; // 2f4: release only, as hfs_lease_acquire_ex builds it
 granted[acquire_calls++]=lease;leases_live++;*out=lease;return hook==HOOK_PASSED ? -EXDEV : 0;
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
static void host_floor_advance(struct hfs_view *view)
{ host_tickets_used=0;host_tickets_full=false;view->retire_progress++; }
/* 1h4: another thread's rounds start holding the mutex. */
static void busy_start(unsigned int rounds)
{ assert(!retire_busy && rounds);retire_busy=rounds;mutex_lock(&test_session->retirement); }
/* 1h4: another thread's busy round ends (view_wait_hook); the next starts. */
static void busy_round_end(void)
{
 struct hfs_view *view=test_session->view;
 if(!retire_busy)return;
 retire_busy--;
 if(!retire_busy && busy_round_frees)host_floor_advance(view);
 hfs_retirement_unlock(test_session);
 if(retire_busy)mutex_lock(&test_session->retirement);
}
/* 1h4: end the other thread's remaining rounds at once, freeing nothing. */
static void busy_stop(void)
{ if(retire_busy){retire_busy=1;busy_round_frees=false;busy_round_end();} }
/* 1h4 review round 1: what a thread reads that fails its trylock against a
 * round just before that round unlocks (unlock_probe).
 */
static bool unlock_probe;
static long probed_rounds;
static void test_mutex_unlocking(struct mutex *m)
{
 if(!unlock_probe || m!=&test_session->retirement)return;
 assert(!mutex_trylock(m)); // still held: a waiter's try is -EBUSY now
 probed_rounds=atomic_long_read(&test_session->view->retirement_releases);
}
static int host_retire_round(struct hfs_session *session)
{
 jiffies+=retire_jiffies;
 if(host_retire_refused()){
  if(!in_worker)retire_blocked_calls++;
  session->view->retire_blocked=true;
  if(blocked_until_retires && retire_blocked_calls==blocked_until_retires)host_recall_running=false;
  return -EAGAIN;
 }
 session->view->retire_blocked=false;
 if(!host_tickets_used)return -EAGAIN;
 host_floor_advance(session->view);return 0;
}
static int host_retire(struct hfs_session *session,bool *ran)
{
 int err;
 assert(session==test_session);
 if(in_worker)worker_retires++;else retire_calls++;
 assert(retire_calls<1000); // a back-off never runs rounds without bound
 if(ran){ // hfs_view_retire_try
  if(!mutex_trylock(&session->retirement))return -EBUSY;
  *ran=true;
 } else {
  while(retire_busy)busy_round_end(); // hfs_view_retire's mutex_lock waits them out
  mutex_lock(&session->retirement);
 }
 if(retire_ran_busy){retire_ran_busy--;err=-EBUSY;}
 else err=host_retire_round(session);
 hfs_retirement_unlock(session);return err;
}
int hfs_view_retire(struct hfs_session *session) { return host_retire(session,NULL); }
int hfs_view_retire_try(struct hfs_session *session,bool *ran) { *ran=false;return host_retire(session,ran); }
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
 warning_log[0]=0;
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
/* 2f4 (zq3-r17 R17-1): the observation's VIEW_ACQUIRE returns -EXDEV with
 * a lease that a later recall passed before the view settled. The
 * observation holds it unpublished, acquires again at the new fence and
 * only then queues the passed lease, whose release starts no recall (it is
 * no longer active on the host). Pre-2f4 there was no such return: the
 * session was blocked instead.
 */
static void retry_grant_passed(struct hfs_view *view,int reserved)
{
 void *cookie;
 assert(!retry_begin(HOOK_PASSED,0,&cookie));retry_published(view,cookie);
 assert(acquire_calls==2 && !host_of(granted[0])->active && granted[1]->fence==granted[0]->fence+1);
 assert(nr_queued==1 && queued[0]->lease==granted[0] && !granted[0]->published);
 assert(!queued_at_acquire[1] && ((struct hfs_observation *)cookie)->lease==granted[1]);
 assert(strstr(last_warning,"observation re-check failed (view moved past the granted lease); acquiring again before releasing it"));
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
 * 1h4: that recall runs during the third acquisition, so its BLOCKED comes
 * after a reinstall and is re-checked at once (one spare ticket, no pause);
 * the fourth is BLOCKED on an unchanged view and backs off as before.
 */
static void retry_blocked_held(struct hfs_view *view,int reserved)
{
 void *cookie;
 host_tickets_full=true;host_full_from=1;
 assert(!retry_begin(HOOK_NONE,1,&cookie));retry_published(view,cookie);
 assert(acquire_calls==5 && sync_releases==1 && granted[0]->released && host_release_recalls==1);
 assert(retire_calls==2 && retire_blocked_calls==1 && test_sleeps==2 && !view->retire_blocked);
 assert(!queued_at_acquire[1] && queued_at_acquire[2]==2 && queued_at_acquire[3]==3);
 assert(nr_queued==4 && queued[0]->lease==granted[0] && !granted[0]->published);
 assert(!queued[1]->lease && !queued[2]->lease && !queued[3]->lease);
 assert(((struct hfs_observation *)cookie)->lease==granted[4]);
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
 * credentials holds an active lease (retirementBlocked), or the retirement
 * round ran past the deadline. (A busy mutex is waited for since 1h4:
 * retry_blocked_busy_*.)
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
 host_tickets_full=true;test_pause_hook=run_cleanups;busy_start(1);
 assert(!retry_begin(HOOK_NONE,0,&cookie));retry_published(view,cookie);
 assert(acquire_calls==2 && test_sleeps==1 && worker_retires==1 && retire_calls==1 && !retire_busy);
 test_pause_hook=NULL;run_cleanups();retry_end(cookie);retry_no_leaks(reserved);
 // 1h4: a busy try without progress yet waits for that round (not another
 // pause, pre-1h4 two); the round ends without freeing, the next try retires.
 host_tickets_full=true;busy_start(1);view_wait_step=100;view_wait_hook=busy_round_end;
 assert(!retry_begin(HOOK_NONE,0,&cookie));retry_published(view,cookie);
 assert(acquire_calls==2 && test_sleeps==1 && retire_calls==2 && !retire_busy);
 view_wait_step=0;view_wait_hook=NULL;
 run_cleanups();retry_end(cookie);retry_no_leaks(reserved);
}
/* 1h4 (zq3-r19 R19-1): the acquisition was PENDING while a recall
 * reinstalled another target, and re-admission answers BLOCKED for the old
 * expectation. The retirement mutex stays busy for 500 ms (the REOPENED
 * kick's round). The view moved since the capture, so the BLOCKED is a
 * re-check: acquired again at once at the new target, no pause, no
 * retirement try, 2 host tickets. Pre-1h4 the busy rounds counted as
 * attempts: -EAGAIN after about 0.35 s, 1 ticket. A view reinstalled before
 * every answer still stops at the ticket cap (4 BLOCKED tickets, no pause).
 */
static void retry_blocked_reinstalled(struct hfs_view *view,int reserved)
{
 void *cookie;unsigned long start=jiffies;u64 before=view->revision;
 busy_start(5);view_wait_step=100;view_wait_hook=busy_round_end;
 assert(!retry_begin(HOOK_REINSTALL_BLOCKED,0,&cookie));retry_published(view,cookie);
 assert(acquire_calls==2 && host_tickets_used==2 && view->revision>before && view->target_hash[0]==1);
 assert(!test_sleeps && !retire_calls && jiffies==start && retire_busy==5);
 assert(nr_queued==1 && !queued[0]->lease && ((struct hfs_observation *)cookie)->lease==granted[1]);
 assert(strstr(warning_log,"observation re-check failed (VIEW_ACQUIRE BLOCKED for the target before a reinstall); acquiring again at once"));
 assert(!strstr(warning_log,"BLOCKED by the host") && !strstr(warning_log,"busy"));
 view_wait_step=0;view_wait_hook=NULL;busy_stop();
 run_cleanups();retry_end(cookie);retry_no_leaks(reserved);
 reinstall_always=true;cookie=(void *)1;
 assert(retry_begin(HOOK_NONE,0,&cookie)==-EAGAIN && !cookie && acquire_calls==HFS_BLOCKED_RETRIES_MAX+1);
 assert(!test_sleeps && !retire_calls && nr_queued==acquire_calls);
 assert(strstr(last_warning,"observation VIEW_ACQUIRE BLOCKED, retry limit reached after 4 attempts (-EAGAIN)"));
 reinstall_always=false;run_cleanups();retry_no_leaks(reserved);
 same_target_recall(view);assert(!view->target_hash[0]); // the first target again
}
/* 1h4: a capacity BLOCKED on an unchanged view still needs progress. The
 * retirement mutex is busy for 2 rounds that free nothing, and this call's
 * own try is refused (another observation of the same credentials holds an
 * active lease): -EAGAIN after one acquisition and one pause, with the local
 * busy line apart from the host BLOCKED line. No re-check acquisition.
 * Pre-1h4: each busy try paused again, ending at the retry limit.
 */
static void retry_blocked_busy_capacity(struct hfs_view *view,int reserved)
{
 void *holder,*cookie=(void *)1;unsigned long start;
 assert(!retry_begin(HOOK_NONE,0,&holder));retry_published(view,holder);
 host_tickets_full=true;busy_start(2);view_wait_step=100;view_wait_hook=busy_round_end;start=jiffies;
 assert(retry_begin(HOOK_NONE,0,&cookie)==-EAGAIN && !cookie && acquire_calls==1);
 assert(test_sleeps==1 && retire_calls==3 && retire_blocked_calls==1 && !retire_busy && jiffies-start==250);
 assert(strstr(warning_log,"observation VIEW_ACQUIRE BLOCKED by the host (attempt 1); retiring, then retrying after 50 ms if that frees anything"));
 assert(strstr(warning_log,"observation VIEW_ACQUIRE BLOCKED (attempt 1): local retirement mutex busy; waiting for its holder (up to 29950 ms)"));
 assert(strstr(last_warning,"observation VIEW_ACQUIRE BLOCKED, no retirement progress after 1 attempts (-EAGAIN)"));
 assert(!strstr(warning_log,"re-check failed") && view->retire_blocked && nr_queued==1);
 view_wait_step=0;view_wait_hook=NULL;
 run_cleanups();retry_end(holder);retry_no_leaks(reserved);
}
/* 1h4: the retirement mutex stays busy for 5 rounds; the fifth advances a
 * floor. The observation waits through them (one pause, no cap), sees the
 * progress right at the wake (no further try) and is granted on its second
 * acquisition. Pre-1h4: the retry limit after 3 busy
 * pauses (-EAGAIN).
 */
static void retry_blocked_busy_rounds(struct hfs_view *view,int reserved)
{
 void *cookie;unsigned long start=jiffies;
 host_tickets_full=true;busy_start(5);busy_round_frees=true;view_wait_step=100;view_wait_hook=busy_round_end;
 assert(!retry_begin(HOOK_NONE,0,&cookie));retry_published(view,cookie);
 assert(acquire_calls==2 && test_sleeps==1 && retire_calls==5 && !retire_busy && jiffies-start==550); // progress seen at the wake: no sixth try
 assert(strstr(warning_log,"local retirement mutex busy") && !strstr(warning_log,"(attempt 2)"));
 assert(nr_queued==1 && !queued[0]->lease);
 busy_round_frees=false;view_wait_step=0;view_wait_hook=NULL;
 run_cleanups();retry_end(cookie);retry_no_leaks(reserved);
}
/* 1h4: a retirement round that never ends: the observation waits until the
 * call's deadline and returns the host's -EAGAIN with the deadline line,
 * after one acquisition and one pause. Pre-1h4: the retry limit after 350 ms.
 */
static void retry_blocked_busy_deadline(struct hfs_view *view,int reserved)
{
 void *cookie=(void *)1;unsigned long start=jiffies;
 host_tickets_full=true;busy_start(~0U);view_wait_step=0;view_wait_hook=NULL;
 assert(retry_begin(HOOK_NONE,0,&cookie)==-EAGAIN && !cookie && acquire_calls==1);
 // One try per HFS_RETIRE_BUSY_SLICE_MS slice of the 29950 ms after the
 // pause (round 2), one local-busy line, none counted.
 assert(test_sleeps==1 && retire_calls==150 && jiffies-start==HFS_VIEW_ENTER_WAIT_MS);
 assert(strstr(last_warning,"observation VIEW_ACQUIRE BLOCKED, deadline exhausted after 1 attempts (-EAGAIN)"));
 { const char *first=strstr(warning_log,"local retirement mutex busy");
   assert(first && !strstr(first+1,"local retirement mutex busy")); }
 busy_stop();run_cleanups();retry_no_leaks(reserved);
}
/* 1h4 review round 1: the actual hfs_retirement_unlock increments the
 * release count only after the unlock. A thread whose trylock fails just
 * before the unlock read the count while the round still held the mutex, so
 * the count then differs from what it read and its wait ends. Incremented
 * before the unlock (the first 1h4 commit), the thread would read the new
 * count, fail its trylock, and sleep to its deadline.
 */
static void retire_unlock_order(void)
{
 long before;
 busy_start(2);unlock_probe=true;before=atomic_long_read(&test_session->view->retirement_releases);
 busy_round_end();
 assert(probed_rounds==before && atomic_long_read(&test_session->view->retirement_releases)==before+1);
 busy_round_end();
 assert(probed_rounds==before+1 && atomic_long_read(&test_session->view->retirement_releases)==before+2);
 unlock_probe=false;assert(!retire_busy && mutex_trylock(&test_session->retirement));
 mutex_unlock(&test_session->retirement);
}
/* 1h4 review round 1: a round of this call that ran and returned -EBUSY (a
 * retired ticket still in use) is no held mutex: it counts as an attempt and
 * pauses again, up to the cap (3 pauses, the retry-limit line), instead of
 * looping on the changed round count.
 */
static void retry_blocked_ran_busy(int reserved)
{
 void *cookie=(void *)1;unsigned long start=jiffies;
 host_tickets_full=true;retire_ran_busy=~0U;
 assert(retry_begin(HOOK_NONE,0,&cookie)==-EAGAIN && !cookie && acquire_calls==1);
 assert(test_sleeps==3 && retire_calls==3 && jiffies-start==350);
 assert(strstr(last_warning,"observation VIEW_ACQUIRE BLOCKED, retry limit reached after 4 attempts (-EAGAIN)"));
 assert(!strstr(warning_log,"local retirement mutex busy"));
 retire_ran_busy=0;run_cleanups();retry_no_leaks(reserved);
}
/* 1h4 review round 1: the busy round ends while the transport fails. The
 * woken observation checks its stops before trying another round: -ESTALE,
 * one retirement try (not a second round and "no retirement progress").
 */
static void busy_end_transport_failed(void)
{ busy_round_end();test_session->transport_failed=true; }
static void retry_blocked_busy_stop(int reserved)
{
 void *cookie=(void *)1;
 host_tickets_full=true;busy_start(1);view_wait_step=100;view_wait_hook=busy_end_transport_failed;
 assert(retry_begin(HOOK_NONE,0,&cookie)==-ESTALE && !cookie && acquire_calls==1);
 assert(test_sleeps==1 && retire_calls==1 && !retire_busy);
 assert(!strstr(warning_log,"no retirement progress"));
 test_session->transport_failed=false;view_wait_step=0;view_wait_hook=NULL;
 host_tickets_full=false;run_cleanups();retry_no_leaks(reserved);
}
/* 1h4 review round 2: the holder is no view round but ordinary request
 * retirement (hfs_retire) or session destruction, which also hold
 * session->retirement and release it through the actual
 * hfs_retirement_unlock. The waiter wakes when it releases (100 ms in), not
 * at the end of its wait slice, and its own round then frees the tickets.
 * Before round 2 hfs_retire released with a plain mutex_unlock: no count, no
 * wake-up, and the waiter slept to its 30 s deadline (-EAGAIN).
 */
static bool foreign_held;
static void foreign_release(void)
{ if(foreign_held){foreign_held=false;hfs_retirement_unlock(test_session);} }
static void foreign_plain_release(void)
{ if(foreign_held){foreign_held=false;assert(pthread_mutex_unlock(&test_session->retirement.native)==0);} }
static void retry_blocked_foreign_holder(struct hfs_view *view,int reserved)
{
 void *cookie;unsigned long start=jiffies;
 host_tickets_full=true;mutex_lock(&test_session->retirement);foreign_held=true;
 view_wait_step=100;view_wait_hook=foreign_release;
 assert(!retry_begin(HOOK_NONE,0,&cookie));retry_published(view,cookie);
 assert(acquire_calls==2 && test_sleeps==1 && retire_calls==2 && !foreign_held && jiffies-start==150);
 run_cleanups();retry_end(cookie);retry_no_leaks(reserved);
 // A holder that released without the helper (none in production) costs
 // one HFS_RETIRE_BUSY_SLICE_MS slice, not the deadline.
 start=jiffies;host_tickets_full=true;mutex_lock(&test_session->retirement);foreign_held=true;
 view_wait_hook=foreign_plain_release;
 assert(!retry_begin(HOOK_NONE,0,&cookie));retry_published(view,cookie);
 assert(acquire_calls==2 && test_sleeps==1 && retire_calls==2 && jiffies-start==50+HFS_RETIRE_BUSY_SLICE_MS);
 view_wait_step=0;view_wait_hook=NULL;
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
 retry_grant_passed(view,reserved);
 retry_self_recall(view,reserved);
 retry_moved_target(view,reserved);
 retry_host_blocked(reserved);
 retry_blocked_tickets(view,reserved);
 retry_blocked_held(view,reserved);
 retry_blocked_persistent(reserved);
 retry_blocked_no_progress(view,reserved);
 retry_blocked_worker(view,reserved);
 retry_blocked_reinstalled(view,reserved);
 retry_blocked_busy_capacity(view,reserved);
 retry_blocked_busy_rounds(view,reserved);
 retry_blocked_busy_deadline(view,reserved);
 retire_unlock_order();
 retry_blocked_ran_busy(reserved);
 retry_blocked_busy_stop(reserved);
 retry_blocked_foreign_holder(view,reserved);
 retry_blocked_stops(view,reserved);
 retry_blocked_retire(view,reserved);
 retry_refused(view,reserved);
 retry_deadline(view,reserved);
 retry_attr(view);
 retry_attr_enter(view);
 retry_lookup_forget();
 retry_reissue(session);
}
/* 2f4 (zq3-r17 R17-1): the actual hfs_view_exchange on a GRANTED reply that
 * is verified but not current. The host grants a PENDING VIEW_ACQUIRE lazily
 * on the first VIEW_QUERY after the recall (readmit, tickets.go), right after
 * it accepted this mount's REOPENED ACK; the guest raises its fence only when
 * that ACK's reply arrives. Fixtures: xq_steps is the host's answer per
 * exchange (a re-sent VIEW_QUERY of a GRANTED ticket repeats the stored
 * reply); hfs_control_flagged captures the view at send as
 * hfs_received_capture does; test_received_source_ex is a model of patch
 * 0005's receipt (hfs_received_source_current: a lease is current at the
 * mount's fence, at the captured revision and phase, LIVE or UNBOUND; a
 * reservation or entry while QUIESCENT/STOPPED); xq_fence is the mount's
 * current_fence; view_wait_hook plays the REOPENED ACK's reply
 * (hfs_view_reopen_acked) while the exchange waits.
 */
struct xq_step { const char *status; const char *source; u64 fence; void (*hook)(struct hfs_view *); };
static const struct xq_step *xq_steps;
static unsigned int xq_nr, xq_calls, xq_source_calls, xq_blocks;
static u64 xq_fence, xq_capture_revision;
static enum hfs_view_phase xq_capture_phase;
static bool xq_unverified, xq_never_current;
static int xq_block_err;
static struct hfs_view_ticket xq_ticket;
static struct hfs_view_family xq_family;
int hfs_control_flagged(struct hfs_session *session,const char *operation,const struct hfs_buf *payload,
                        bool control,bool after_block,struct hfs_json **out)
{
 const struct xq_step *step;char text[256],source[32];struct hfs_view *view=session->view;
 assert(session==test_session && xq_calls<xq_nr && !after_block);
 step=&xq_steps[xq_calls];
 if(xq_ticket.admitted)assert(!strcmp(operation,"VIEW_QUERY") && payload==&xq_ticket.query && control);
 else assert(!strcmp(operation,xq_ticket.operation) && payload==&xq_ticket.payload);
 xq_capture_revision=view->revision;xq_capture_phase=view->phase; // hfs_received_capture
 if(step->hook)step->hook(view);
 xq_calls++;
 snprintf(source,sizeof(source),"{\"fence\":%llu}",(unsigned long long)step->fence);
 snprintf(text,sizeof(text),"{\"payload\":{\"entry\":%s,\"lease\":%s,\"reservation\":%s,\"status\":\"%s\"}}",
          step->source && !strcmp(step->source,"entry")?source:"null",
          step->source && !strcmp(step->source,"lease")?source:"null",
          step->source && !strcmp(step->source,"reservation")?source:"null",step->status);
 return hfs_json_parse(text,strlen(text),out);
}
static int hfs_ticket_reply(struct hfs_view_ticket *ticket,const struct hfs_json *json,int payload,const char *wire_operation)
{
 assert(ticket==&xq_ticket && payload==hfs_json_field(json,0,"payload"));
 assert(!strcmp(wire_operation,ticket->admitted?"VIEW_QUERY":ticket->operation));
 return 0;
}
bool hfs_after_block_operation(const char *operation)
{ assert(!strcmp(operation,"VIEW_ACQUIRE") || !strcmp(operation,"DIRTY_RESERVE"));return false; }
u64 hfs_controlled_current_fence(struct hfs_session *session) { assert(session==test_session);return xq_fence; }
int hfs_session_poll_halt(struct hfs_session *session,bool exempt)
{ return session->transport_failed || (session->blocked && !exempt) ? -EHOSTDOWN : 0; }
void hfs_session_block(struct hfs_session *session,const char *reason,const char *operation,int err)
{ assert(!strcmp(reason,"uncertain view ticket") && operation);session->blocked=true;xq_blocks++;xq_block_err=err; }
static int test_received_source_ex(struct hfs_session *session,const struct hfs_json *json,int token,bool *not_current)
{
 struct hfs_view *view=session->view;int body=hfs_json_field(json,0,"payload"),lease=hfs_json_field(json,body,"lease");
 bool captured=view->revision==xq_capture_revision && view->phase==xq_capture_phase,fresh;u64 fence=0;
 *not_current=false;xq_source_calls++;
 assert(session->bridge_minor==4 && token>=0);
 if(xq_unverified)return -EKEYREJECTED;
 if(token==lease){
  assert(!hfs_json_u64(json,hfs_json_field(json,lease,"fence"),&fence));
  fresh=captured && fence==xq_fence && (view->phase==HFS_VIEW_LIVE || view->phase==HFS_VIEW_UNBOUND);
 } else fresh=captured && (view->phase==HFS_VIEW_QUIESCENT || view->phase==HFS_VIEW_STOPPED);
 if(fresh && !xq_never_current)return 0;
 *not_current=true;return -ESTALE;
}
static unsigned long xq_acks_at; // view_wait_sleeps at which the ACK's reply arrives
static u64 xq_acked_fence;       // the fence that ACK raises the mount to
static void xq_ack_hook(void)
{
 struct hfs_view *view=test_session->view;
 if(view_wait_sleeps!=xq_acks_at)return;
 if(view->phase!=HFS_VIEW_LIVE){view->phase=HFS_VIEW_LIVE;view->revision++;} // the REOPEN
 xq_fence=xq_acked_fence;hfs_view_reopen_acked(view);
}
static void xq_reopened_in_flight(struct hfs_view *view) { view->revision++; }
static void xq_recall_in_flight(struct hfs_view *view) { view->phase=HFS_VIEW_RECALLING;view->revision++; }
static void xq_setup(const char *operation,bool admitted,const struct xq_step *steps,unsigned int nr,u64 fence)
{
 memset(&xq_ticket,0,sizeof(xq_ticket));memset(&xq_family,0,sizeof(xq_family));
 mutex_init(&xq_family.dispatch);xq_ticket.family=&xq_family;xq_ticket.session=test_session;
 xq_ticket.number=1;xq_ticket.control=hfs_view_command_control_family(operation);xq_ticket.bound=true;
 xq_ticket.admitted=admitted;strcpy(xq_ticket.operation,operation);
 assert(!hfs_buf_init(&xq_ticket.payload,64) && !hfs_buf_init(&xq_ticket.query,64));
 hfs_buf_literal(&xq_ticket.payload,"{}");hfs_buf_literal(&xq_ticket.query,"{\"request\":1}");
 xq_steps=steps;xq_nr=nr;xq_calls=xq_source_calls=xq_blocks=0;xq_block_err=0;xq_fence=fence;
 xq_unverified=xq_never_current=false;xq_acks_at=0;view_wait_sleeps=0;view_wait_step=10;view_wait_hook=xq_ack_hook;
 test_session->bridge_minor=4;test_session->blocked=false;warnings=0;
}
static int xq_run(struct hfs_json **response)
{
 return hfs_view_exchange(test_session,&xq_ticket,xq_ticket.operation,&xq_ticket.payload,response);
}
static bool xq_granted(const struct hfs_json *json)
{ return json && hfs_json_is(json,hfs_json_field(json,0,"status"),"\"GRANTED\""); }
static void xq_teardown(struct hfs_json *response)
{
 hfs_json_free(response);hfs_json_free(xq_ticket.response);hfs_json_free(xq_ticket.initial_response);
 hfs_buf_free(&xq_ticket.payload);hfs_buf_free(&xq_ticket.query);
 assert(!pthread_mutex_destroy(&xq_family.dispatch.native));
 view_wait_hook=NULL;view_wait_step=0;view_wait_fatal=false;
 test_session->bridge_minor=0;test_session->blocked=false;test_session->transport_failed=false;
 test_session->view->phase=HFS_VIEW_LIVE;
}
/* R17-1's shape: PENDING, then a GRANTED at fence 6 while the mount is still
 * at 5 (the host accepted the REOPENED ACK; its reply is not back yet). The
 * exchange waits for the ACK's reply, queries again and gets the same
 * stored GRANTED, now current. Pre-2f4: blocked by "uncertain view ticket".
 */
static void xq_grant_before_fence(void)
{
 static const struct xq_step steps[]={{"PENDING",NULL,0,NULL},{"GRANTED","lease",6,NULL},{"GRANTED","lease",6,NULL}};
 struct hfs_json *response=NULL;unsigned long start=jiffies;
 xq_setup("VIEW_ACQUIRE",false,steps,3,5);xq_acks_at=3;xq_acked_fence=6;
 assert(!xq_run(&response) && xq_granted(response) && xq_calls==3 && xq_source_calls==2);
 assert(xq_ticket.terminal && !xq_ticket.uncertain && xq_granted(xq_ticket.response) && !xq_blocks && !test_session->blocked);
 assert(view_wait_sleeps==3 && jiffies-start>=30 && jiffies-start<100 && warnings==1);
 assert(strstr(last_warning,"VIEW_ACQUIRE GRANTED at fence 6 is not current (mount fence 5, view LIVE); querying again once the view settles"));
 assert(hfs_json_is(xq_ticket.initial_response,hfs_json_field(xq_ticket.initial_response,0,"status"),"\"PENDING\""));
 xq_teardown(response);
}
/* The GRANTED was captured at the revision before this mount's REOPEN (the
 * reopen happened while it was in flight), the fence already matches: query
 * again at once, no wait; the fresh capture makes it current.
 */
static void xq_grant_old_revision(void)
{
 static const struct xq_step steps[]={{"GRANTED","lease",6,xq_reopened_in_flight},{"GRANTED","lease",6,NULL}};
 struct hfs_json *response=NULL;
 xq_setup("VIEW_ACQUIRE",true,steps,2,6);
 assert(!xq_run(&response) && xq_granted(response) && xq_calls==2 && !view_wait_sleeps);
 assert(xq_ticket.terminal && !xq_ticket.uncertain && !xq_blocks && warnings==1);
 xq_teardown(response);
}
/* Not current at consumption: the reply left the host current, but a later
 * recall started before the guest consumed it and its REOPENED ACK raised
 * the mount past the lease's fence. The lease is handed out with -EXDEV
 * (ie4 releases and acquires again), the ticket is terminal, not uncertain,
 * and the session is not blocked. A mount already past the fence returns
 * -EXDEV without waiting.
 */
static void xq_grant_passed(void)
{
 static const struct xq_step steps[]={{"GRANTED","lease",6,xq_recall_in_flight}};
 struct hfs_json *response=NULL;
 xq_setup("VIEW_ACQUIRE",true,steps,1,6);xq_acks_at=2;xq_acked_fence=7;
 assert(xq_run(&response)==-EXDEV && xq_granted(response) && xq_calls==1 && view_wait_sleeps==2);
 assert(xq_ticket.terminal && !xq_ticket.uncertain && xq_granted(xq_ticket.response) && !xq_blocks && !test_session->blocked);
 assert(strstr(last_warning,"VIEW_ACQUIRE GRANTED at fence 6 was passed by a later recall; releasing it and acquiring again (-EXDEV)"));
 xq_teardown(response);response=NULL;
 xq_setup("VIEW_ACQUIRE",true,steps,1,7);
 assert(xq_run(&response)==-EXDEV && xq_granted(response) && !view_wait_sleeps && !xq_blocks);
 xq_teardown(response);
}
/* No REOPENED ACK reply within the exchange's 30 s: -ETIMEDOUT, a fatal
 * signal (-EINTR), a blocked session or a failed transport (-EHOSTDOWN, not
 * -ESTALE, which hfs_lease_acquire_ex would block on: review 1) stop the
 * wait without a block. The verified GRANTED is still handed out in the
 * response with the ticket terminal, not uncertain: the host holds that
 * lease and its release credit, so the caller must release it.
 */
static void xq_stop_case(int expected,bool timed)
{
 static const struct xq_step steps[]={{"GRANTED","lease",6,NULL}};
 struct hfs_json *response=NULL;unsigned long start=jiffies;
 xq_setup("VIEW_ACQUIRE",true,steps,1,5);
 if(expected==-EINTR)view_wait_fatal=true;
 else if(expected==-EHOSTDOWN && !timed)test_session->blocked=true;
 else if(expected==-EHOSTDOWN)test_session->transport_failed=true;
 assert(xq_run(&response)==expected && xq_granted(response) && xq_calls==1 && !xq_blocks);
 assert(xq_ticket.terminal && !xq_ticket.uncertain && xq_granted(xq_ticket.response));
 if(expected==-ETIMEDOUT){
  assert(jiffies-start>=30*HZ);
  assert(strstr(last_warning,"VIEW_ACQUIRE GRANTED at fence 6: view did not settle before the exchange deadline (-ETIMEDOUT)"));
 } else if(expected==-EHOSTDOWN)assert(!view_wait_sleeps);
 xq_teardown(response);
}
static void xq_grant_stops(void)
{
 xq_stop_case(-ETIMEDOUT,true);
 xq_stop_case(-EINTR,false);
 xq_stop_case(-EHOSTDOWN,false); // blocked session: a refused view
 xq_stop_case(-EHOSTDOWN,true);  // failed transport: the poll halt
}
/* 2f4 review 1: an unadopted lease (handed out only for release) is no
 * authority: no initial bind and no page lease take it.
 */
static void xq_unadopted_refused(void)
{
 struct hfs_view *view=test_session->view;struct hfs_view_stamp stamp;struct hfs_cache_page *entry;
 static struct hfs_view_ticket credit={.control=true};
 struct hfs_lease lease={0};
 assert(!hfs_json_parse("{\"target\":{}}",13,&lease.json));
 kref_init(&lease.refs);lease.release_ticket=&credit;lease.unadopted=true;lease.visibility=generation;
 memcpy(lease.target_hash,view->target_hash,32);
 assert(hfs_view_bind_initial(view,&lease)==-ESTALE);
 assert(!hfs_view_snapshot(view,&stamp) && !hfs_view_page_prepare(view,&stamp,&entry));
 assert(hfs_view_page_lease(entry,&lease)==-ESTALE && !entry->lease);
 lease.unadopted=false;assert(!hfs_view_page_lease(entry,&lease) && entry->lease==&lease);
 entry->lease=NULL;hfs_view_page_put(entry);hfs_json_free(lease.json);
}
/* Fail closed as before 2f4: an unverified GRANTED (-EKEYREJECTED), one that
 * stays not current at a settled view after its re-query (-EPROTO), and a
 * DIRTY_RESERVE reservation that is not current (its source needs
 * QUIESCENT/STOPPED, so it never waits for LIVE) block the session at once.
 * A current DIRTY_RESERVE still passes.
 */
static void xq_fail_closed(void)
{
 static const struct xq_step lease[]={{"GRANTED","lease",6,NULL},{"GRANTED","lease",6,NULL}};
 static const struct xq_step dirty[]={{"GRANTED","reservation",1,NULL}};
 struct hfs_json *response=NULL;
 xq_setup("VIEW_ACQUIRE",true,lease,2,6);xq_unverified=true;
 assert(xq_run(&response)==-EKEYREJECTED && !response && xq_calls==1 && xq_blocks==1 && xq_block_err==-EKEYREJECTED);
 assert(xq_ticket.uncertain && !warnings);xq_teardown(NULL);
 xq_setup("VIEW_ACQUIRE",true,lease,2,6);xq_never_current=true;
 assert(xq_run(&response)==-EPROTO && xq_calls==2 && !view_wait_sleeps && xq_blocks==1 && xq_ticket.uncertain);
 xq_teardown(NULL);
 xq_setup("DIRTY_RESERVE",true,dirty,1,6);
 assert(xq_run(&response)==-ESTALE && !response && xq_calls==1 && xq_blocks==1 && xq_block_err==-ESTALE);
 assert(!view_wait_sleeps && !warnings && xq_ticket.uncertain);xq_teardown(NULL);
 xq_setup("DIRTY_RESERVE",true,dirty,1,6);test_session->view->phase=HFS_VIEW_QUIESCENT;
 assert(!xq_run(&response) && response && xq_calls==1 && !xq_blocks && xq_ticket.terminal);
 xq_teardown(response);
}
static void grant_settle_checks(void)
{
 struct hfs_view *view=test_session->view;
 assert(view->phase==HFS_VIEW_LIVE && !view->active);
 xq_grant_before_fence();
 xq_grant_old_revision();
 xq_grant_passed();
 xq_grant_stops();
 xq_fail_closed();
 xq_unadopted_refused();
}
int main(void)
{
 struct hfs_provider provider={reserve,unreserve,provider_leave};struct hfs_session session={.provider=&provider};
 struct address_space mapping={.id=1,.a_ops=&hfs_cache_aops};struct hfs_inode inode={.inode={&mapping,1}};
 struct hfs_cap cap={.token={1}};struct hfs_description desc={.cap=&cap};struct file file={&inode.inode,&desc};struct vm_area_struct vma={&file};
 test_vma=&vma;test_session=&session;mutex_init(&session.lock);mutex_init(&session.retirement);INIT_LIST_HEAD(&inode.view_list);
 assert(!hfs_view_new(&session,&generation));observed_view=session.view;assert(!hfs_view_register_inode(&inode.inode));
 assert(!hfs_json_parse("{\"target\":{}}",13,&target_event));
 assert(!hfs_json_parse("{\"target\":{\"t\":2}}",18,&other_target_event)); // 1h4
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
 // 2f4: the actual hfs_view_exchange on a verified GRANTED that is not current.
 grant_settle_checks();
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
 hfs_json_free(target_event);hfs_json_free(other_target_event);hfs_view_unregister_inode(&inode.inode);hfs_view_fence(session.view);assert(!hfs_view_free(&session));
 assert(!reservations && !module_refs);
 puts("PASS: actual128-clean pressure/129th admission,mkwrite release/stop/install/reopen,in-flight observation lease never released,background pass held (bounded) while observing,observation re-check retry (recall before/after grant re-acquires,no release recall,no self-recall loop,moved target,refusals,deadline,no leak,RETIRED_ID passed through,host BLOCKED retried only after retirement progress (held lease released first,at most 3 retries,ticket budget kept),progress by the cleanup worker during the pause or after a busy try,1h4 BLOCKED after a reinstall re-checked at once (2 tickets),busy retirement waited for not counted (5 busy rounds granted,never ending -EAGAIN at the deadline,capacity BLOCKED still needs progress,release count after the unlock,request-retirement holder wakes the waiter,wait slice bounds an unknown holder,a round that ran -EBUSY counted,stops checked after a wake),no progress/deadline -EAGAIN at once,BLOCKED retire re-sent after recall/next observation (rate-limited),attr re-stamp/re-issue,moved LOOKUP FORGET balanced,2f4 passed grant re-acquired),2f4 exchange: GRANTED before the fence raise re-queried after the REOPENED ACK,old revision re-queried at once,passed by a later recall -EXDEV,deadline/signal/block/transport stops hand the GRANTED out without a block,unadopted lease never bound or page-leased,unverified/never-current/DIRTY fail closed,source denial,short/error neverDRAINED; primitive VFS/PTE/source boundaries only");return 0;
}
