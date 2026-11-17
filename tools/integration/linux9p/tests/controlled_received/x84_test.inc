/* SPDX-License-Identifier: GPL-2.0-only */
/* x84 (zq3-r10 R10-1): hfs_call admission while another request's ALLOCATE
 * is pending. Before x84 any such request got an immediate local -EAGAIN and
 * hfs_release blocked the session; now a still-clearing fence is waited for
 * (bounded, FIFO) and a truly uncertain or failed allocation still refuses
 * at once.
 *
 * The decision matrix and the wait use the actual extracted
 * hfs_call_admit_locked/hfs_call_admit and hfs_request_allocate. The wrapper
 * cases run the unchanged hfs_release in disposable children: a concurrent
 * thread stands in for the fence owner (as the PROCESS_EXIT work is between
 * its own numbering and its ALLOCATE acknowledgement) and clears it. After
 * the wait an injected allocation failure ends the release locally, before
 * any native byte, so these cases need no host; CLEANUP_CALLER repeats the
 * wait end to end.
 */
struct x84_clearer {
 struct hfs_session *session;
 unsigned delay_ms;
 bool arm_allocation_failure;
 pthread_t thread;
 _Atomic bool cleared;
};

/* The fence owner's ALLOCATE acknowledgement: clear pending under the lock,
 * as hfs_request_allocate does (epoch bump, wake), and record it. */
static void *x84_clear(void *arg) {
 struct x84_clearer *c=arg;
 usleep(c->delay_ms*1000U);
 mutex_lock(&c->session->lock);
 c->session->allocation_pending=NULL;
 hfs_allocation_changed_locked(c->session);
 if(c->arm_allocation_failure)hfs_test_alloc_fail=hfs_test_alloc_call+1;
 atomic_store(&c->cleared,true);
 mutex_unlock(&c->session->lock);
 wake_up_all(&c->session->allocation_wait);
 return NULL;
}

static void x84_clear_thread(struct x84_clearer *c,struct hfs_session *s,unsigned delay_ms,bool arm) {
 c->session=s;c->delay_ms=delay_ms;c->arm_allocation_failure=arm;atomic_store(&c->cleared,false);
 assert(!pthread_create(&c->thread,NULL,x84_clear,c));
}

static void x84_clear_start(struct x84_clearer *c,struct hfs_session *s,struct hfs_request *pending,unsigned delay_ms,bool arm) {
 mutex_lock(&s->lock);s->allocation_pending=pending;mutex_unlock(&s->lock);
 x84_clear_thread(c,s,delay_ms,arm);
}

static void x84_clear_join(struct x84_clearer *c) { assert(!pthread_join(c->thread,NULL)); }

static void x84_session(struct hfs_session *s) {
 memset(s,0,sizeof(*s));mutex_init(&s->lock);s->next_claim=1;s->bridge_minor=4;
 INIT_LIST_HEAD(&s->allocation_waiters);init_waitqueue_head(&s->allocation_wait);
}

static void x84_decision_matrix(void) {
 struct hfs_session s;struct hfs_request owner={0};bool wait;
 struct hfs_allocation_waiter me,head;
 x84_session(&s);INIT_LIST_HEAD(&me.list);INIT_LIST_HEAD(&head.list);
 const char *cleanup[]={"DESCRIPTION_CLOSE","DESCRIPTION_RELEASE","FORGET","PROCESS_EXIT"};
 for(unsigned i=0;i<ARRAY_SIZE(cleanup);i++)assert(hfs_cleanup_opcode(cleanup[i]));
 assert(!hfs_cleanup_opcode("ROOT")&&!hfs_cleanup_opcode("GETATTR")&&!hfs_cleanup_opcode("DRAIN_WRITE"));
 /* No fence: the unchanged request admission. */
 mutex_lock(&s.lock);
 assert(!hfs_call_admit_locked(&s,false,false,&me,&wait)&&!wait);
 s.blocked=true;assert(hfs_call_admit_locked(&s,false,false,&me,&wait)==-EHOSTDOWN&&!wait);
 assert(!hfs_call_admit_locked(&s,true,true,&me,&wait)&&!wait);s.blocked=false;
 /* An ordinary owner mid-ALLOCATE: every caller waits. */
 s.allocation_pending=&owner;
 assert(hfs_call_admit_locked(&s,false,false,&me,&wait)==-EAGAIN&&wait);
 assert(hfs_call_admit_locked(&s,true,true,&me,&wait)==-EAGAIN&&wait);
 /* An earmark: only cleanup waits; ordinary callers may hold VFS locks. */
 owner.earmarked=true;
 assert(hfs_call_admit_locked(&s,false,false,&me,&wait)==-EAGAIN&&!wait);
 assert(hfs_call_admit_locked(&s,true,false,&me,&wait)==-EAGAIN&&!wait);
 assert(hfs_call_admit_locked(&s,true,true,&me,&wait)==-EAGAIN&&wait);
 owner.earmarked=false;
 /* Uncertain, failed or closing: nothing clears the fence, refuse at once. */
 s.allocation_uncertain=true;
 assert(hfs_call_admit_locked(&s,true,true,&me,&wait)==-EAGAIN&&!wait);
 s.allocation_uncertain=false;owner.allocation_failed=true;
 assert(hfs_call_admit_locked(&s,true,true,&me,&wait)==-EAGAIN&&!wait);
 owner.allocation_failed=false;s.closing=true;
 assert(hfs_call_admit_locked(&s,true,true,&me,&wait)==-EAGAIN&&!wait);
 s.closing=false;s.allocation_pending=NULL;
 /* FIFO: with the fence free only the queue head may number; the others
  * (queued or new, ordinary or cleanup) wait behind it. */
 list_add_tail(&head.list,&s.allocation_waiters);
 assert(hfs_call_admit_locked(&s,false,false,&me,&wait)==-EAGAIN&&wait);
 list_add_tail(&me.list,&s.allocation_waiters);
 assert(hfs_call_admit_locked(&s,true,true,&me,&wait)==-EAGAIN&&wait);
 assert(!hfs_call_admit_locked(&s,true,true,&head,&wait)&&!wait);
 s.closing=true;
 assert(hfs_call_admit_locked(&s,true,true,&me,&wait)==-EAGAIN&&!wait);
 mutex_unlock(&s.lock);
 assert(!pthread_mutex_destroy(&s.lock.native));
}

static void x84_bounded_wait(void) {
 struct hfs_session s;struct hfs_request owner={0};struct x84_clearer c;struct hfs_allocation_waiter me;
 x84_session(&s);INIT_LIST_HEAD(&me.list);
 /* Waits until the owner clears, then admits as queue head. */
 unsigned long start=jiffies;
 x84_clear_start(&c,&s,&owner,40,false);
 assert(!hfs_call_admit(&s,true,true,&me,start+msecs_to_jiffies(HFS_ALLOCATION_WAIT_MS)));
 assert(atomic_load(&c.cleared)&&jiffies-start>=40);
 x84_clear_join(&c);
 assert(s.allocation_waiters.next==&me.list);
 hfs_call_dequeue(&s,&me);assert(list_empty(&s.allocation_waiters));
 /* At the deadline the old -EAGAIN comes back, the fence stays and the
  * waiter has left the queue. */
 s.allocation_pending=&owner;
 assert(hfs_call_admit(&s,true,true,&me,jiffies)==-EAGAIN&&s.allocation_pending==&owner);
 assert(list_empty(&s.allocation_waiters));
 /* A truly uncertain allocation refuses without waiting. */
 s.allocation_uncertain=true;start=jiffies;
 assert(hfs_call_admit(&s,true,true,&me,start+msecs_to_jiffies(HFS_ALLOCATION_WAIT_MS))==-EAGAIN);
 assert(jiffies-start<1000&&list_empty(&s.allocation_waiters));
 assert(!pthread_mutex_destroy(&s.lock.native));
}

/* The fence owner's ALLOCATE fails locally (its payload does not fit): the
 * actual hfs_request_allocate marks it failed and wakes the queue, so a
 * waiter returns -EAGAIN at once instead of sleeping to its deadline. */
struct x84_failing_owner { struct hfs_session *session;struct hfs_request *owner;unsigned delay_ms;int err; };
static void *x84_fail_allocate(void *arg) {
 struct x84_failing_owner *o=arg;struct hfs_buf payload={0};
 usleep(o->delay_ms*1000U);
 assert(!hfs_buf_init(&payload,1));
 o->err=hfs_request_allocate(o->session,o->owner,&payload);
 hfs_buf_free(&payload);
 return NULL;
}

static void x84_failed_allocation(void) {
 struct hfs_session s;struct hfs_request owner={0};struct hfs_allocation_waiter me;pthread_t thread;
 x84_session(&s);INIT_LIST_HEAD(&me.list);
 assert(!hfs_buf_init(&owner.claim,64)&&!hfs_buf_init(&owner.credentials,64)&&!hfs_buf_init(&owner.intent,64));
 hfs_buf_literal(&owner.claim,"{\"number\":1}");hfs_buf_literal(&owner.credentials,"{}");hfs_buf_literal(&owner.intent,"{}");
 owner.session=&s;s.allocation_pending=&owner;
 struct x84_failing_owner o={&s,&owner,40,0};
 unsigned long start=jiffies;
 assert(!pthread_create(&thread,NULL,x84_fail_allocate,&o));
 assert(hfs_call_admit(&s,true,true,&me,start+msecs_to_jiffies(5000))==-EAGAIN);
 assert(!pthread_join(thread,NULL));
 assert(o.err&&owner.allocation_failed&&!owner.allocating&&s.allocation_pending==&owner);
 assert(jiffies-start<2000&&list_empty(&s.allocation_waiters));
 hfs_buf_free(&owner.claim);hfs_buf_free(&owner.credentials);hfs_buf_free(&owner.intent);
 assert(!pthread_mutex_destroy(&s.lock.native));
}

/* A description the unchanged hfs_release can encode: any cap JSON will do,
 * the release ends locally before the host would judge it. */
static struct hfs_description *x84_description(struct fixture_session *f) {
 struct hfs_description *d=calloc(1,sizeof(*d));struct hfs_cap *cap=calloc(1,sizeof(*cap));
 struct hfs_cred *issuer=calloc(1,sizeof(*issuer));assert(d&&cap&&issuer);
 const char *json="{\"kind\":\"DESCRIPTION\"}";
 assert(!hfs_json_parse(json,strlen(json),&cap->json));
 *issuer=f->credential;INIT_LIST_HEAD(&d->list);d->cap=cap;d->issuer=issuer;
 return d;
}

/* Before x84: -EAGAIN at once and a blocked session. Now hfs_release waits
 * for the concurrent owner and reaches its own numbering, where the injected
 * allocation failure stops it without any claim, cell or native byte. */
static void x84_release_waits(struct fixture_session *f) {
 struct hfs_session *s=f->session;struct hfs_request owner={.session=s};struct x84_clearer c;
 struct inode inode={.i_sb=&f->super};struct file file={.inode=&inode,.private_data=x84_description(f)};
 u64 claim=s->next_claim,cell=s->next_cell;unsigned long start=jiffies;
 x84_clear_start(&c,s,&owner,50,true);
 int err=hfs_release(&inode,&file);
 assert(atomic_load(&c.cleared)&&jiffies-start>=50);
 x84_clear_join(&c);
 assert(err==-ENOMEM&&s->blocked&&file.private_data);
 assert(s->next_claim==claim&&s->next_cell==cell&&!s->allocation_pending);
 assert(list_empty(&s->allocation_waiters));
}

/* A foreign fence set between hfs_call's two admission checks (injected in
 * the middle of request preparation): hfs_call_once drops its request,
 * keeps its queue place, waits for the fence and starts over, where the
 * injected allocation failure ends it. Without the retry it would number
 * over the fence or return -EAGAIN. */
static struct x84_clearer x84_inject_clearer;
static struct hfs_request x84_inject_owner;
static struct hfs_session *x84_inject_session;
static unsigned x84_inject_fired;
static void x84_inject_fence(size_t length) {
 if(length!=HFS_BODY_MAX||x84_inject_fired)return;
 x84_inject_fired++;
 x84_inject_owner.session=x84_inject_session;
 x84_inject_session->allocation_pending=&x84_inject_owner; /* the caller holds no lock here */
 x84_clear_thread(&x84_inject_clearer,x84_inject_session,40,true);
}
static void x84_release_retries(struct fixture_session *f) {
 struct hfs_session *s=f->session;
 struct inode inode={.i_sb=&f->super};struct file file={.inode=&inode,.private_data=x84_description(f)};
 u64 claim=s->next_claim,cell=s->next_cell;
 x84_inject_session=s;x84_allocation_hook=x84_inject_fence;
 int err=hfs_release(&inode,&file);
 x84_allocation_hook=NULL;
 assert(x84_inject_fired==1&&atomic_load(&x84_inject_clearer.cleared));
 x84_clear_join(&x84_inject_clearer);
 assert(err==-ENOMEM&&s->next_claim==claim&&s->next_cell==cell&&!s->allocation_pending);
 assert(list_empty(&s->allocation_waiters));
}

/* A still-uncertain allocation keeps the old refusal: immediate -EAGAIN and
 * the release blocks the session, retaining the description. */
static void x84_release_uncertain(struct fixture_session *f) {
 struct hfs_session *s=f->session;struct hfs_request owner={.session=s};
 struct inode inode={.i_sb=&f->super};struct file file={.inode=&inode,.private_data=x84_description(f)};
 u64 claim=s->next_claim,cell=s->next_cell;
 s->allocation_pending=&owner;s->allocation_uncertain=true;
 unsigned long start=jiffies;
 assert(hfs_release(&inode,&file)==-EAGAIN&&jiffies-start<1000);
 assert(s->blocked&&file.private_data&&s->allocation_pending==&owner);
 assert(s->next_claim==claim&&s->next_cell==cell);
}

/* Task 194 asks whether the guest can send DESCRIPTION_CLOSE for a process
 * after its PROCESS_EXIT (the host refuses that with STALE at ALLOCATE).
 * hfs_process_exit marks the process exited before it queues PROCESS_EXIT,
 * and hfs_capture then refuses that process with -ESTALE, so a later
 * hfs_flush sends nothing and leaves the session usable; the last exiting
 * thread skips the close altogether. */
static void x84_flush_after_exit(struct fixture_session *f) {
 struct hfs_session *s=f->session;struct hfs_process *process;unsigned exited=0;
 struct inode inode={.i_sb=&f->super};struct file file={.inode=&inode,.private_data=x84_description(f)};
 struct signal_struct signal;struct hfs_cred *captured=NULL;
 /* Register the closing process as live, as its earlier syscalls did. */
 fixture_set_current(f);atomic_init(&signal.live,1);current->signal=&signal;
 if(list_empty(&s->processes)){primitive_process_uuid=f->credential.process;primitive_process_uuid_pending=true;}
 assert(!hfs_capture(s,&init_user_ns,&captured));hfs_cred_free(captured);
 u64 claim=s->next_claim,cell=s->next_cell,close=s->next_close;
 list_for_each_entry(process,&s->processes,list){atomic_store(&process->exited,1);exited++;}
 assert(exited);
 assert(hfs_flush(&file,NULL)==-ESTALE);
 /* A non-last thread exiting with its own file table takes the same path. */
 unsigned flags=current->flags;current->flags|=PF_EXITING;
 assert(atomic_load(&current->signal->live)&&hfs_flush(&file,NULL)==-ESTALE);
 /* The last thread: the close is the process event's, nothing is sent. */
 int live=atomic_load(&current->signal->live);atomic_store(&current->signal->live,0);
 assert(!hfs_flush(&file,NULL));
 atomic_store(&current->signal->live,live);current->flags=flags;
 assert(s->next_claim==claim&&s->next_cell==cell&&s->next_close==close);
 assert(!s->blocked&&!s->allocation_pending&&file.private_data);
}

static void x84_admission_regression(struct fixture_session *f) {
 x84_decision_matrix();
 x84_bounded_wait();
 x84_failed_allocation();
 bool passed=regression_child("x84-release-waits",x84_release_waits,f);
 passed&=regression_child("x84-release-retries",x84_release_retries,f);
 passed&=regression_child("x84-release-uncertain",x84_release_uncertain,f);
 passed&=regression_child("x84-flush-after-exit",x84_flush_after_exit,f);
 assert(passed);
 fprintf(stderr,"PASS: x84 pending-allocation admission: decision matrix and FIFO, bounded wait, failed ALLOCATE wakes waiters, hfs_release waits for a clearing owner, retries after a fence between its checks, still refuses an uncertain one, and sends no DESCRIPTION_CLOSE after PROCESS_EXIT\n");
}
