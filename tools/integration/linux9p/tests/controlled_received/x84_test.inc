/* SPDX-License-Identifier: GPL-2.0-only */
/* x84 (zq3-r10 R10-1): hfs_call admission while another request's ALLOCATE
 * is pending. Before x84 any such request got an immediate local -EAGAIN and
 * hfs_release blocked the session; now a still-clearing fence is waited for
 * (bounded) and a truly uncertain allocation still refuses at once.
 *
 * The decision matrix and the wait use the actual extracted
 * hfs_call_admit_locked/hfs_call_admit. The wrapper cases run the unchanged
 * hfs_release in disposable children: a concurrent thread stands in for the
 * fence owner (as the PROCESS_EXIT work is between its own numbering and its
 * ALLOCATE acknowledgement) and clears it. After the wait an injected
 * allocation failure ends the release locally, before any native byte, so
 * these cases need no host; CLEANUP_CALLER repeats the wait end to end.
 */
struct x84_clearer {
 struct hfs_session *session;
 unsigned delay_ms;
 bool arm_allocation_failure;
 pthread_t thread;
 _Atomic bool cleared;
};

/* The fence owner's ALLOCATE acknowledgement: clear pending under the lock,
 * as hfs_request_allocate does, and record that it happened. */
static void *x84_clear(void *arg) {
 struct x84_clearer *c=arg;
 usleep(c->delay_ms*1000U);
 mutex_lock(&c->session->lock);
 c->session->allocation_pending=NULL;
 if(c->arm_allocation_failure)hfs_test_alloc_fail=hfs_test_alloc_call+1;
 atomic_store(&c->cleared,true);
 mutex_unlock(&c->session->lock);
 return NULL;
}

static void x84_clear_start(struct x84_clearer *c,struct hfs_session *s,struct hfs_request *pending,unsigned delay_ms,bool arm) {
 c->session=s;c->delay_ms=delay_ms;c->arm_allocation_failure=arm;atomic_store(&c->cleared,false);
 mutex_lock(&s->lock);s->allocation_pending=pending;mutex_unlock(&s->lock);
 assert(!pthread_create(&c->thread,NULL,x84_clear,c));
}

static void x84_clear_join(struct x84_clearer *c) { assert(!pthread_join(c->thread,NULL)); }

static void x84_decision_matrix(void) {
 struct hfs_session s={0};struct hfs_request owner={0};bool wait;
 mutex_init(&s.lock);s.next_claim=1;
 const char *cleanup[]={"DESCRIPTION_CLOSE","DESCRIPTION_RELEASE","FORGET","PROCESS_EXIT"};
 for(unsigned i=0;i<ARRAY_SIZE(cleanup);i++)assert(hfs_cleanup_opcode(cleanup[i]));
 assert(!hfs_cleanup_opcode("ROOT")&&!hfs_cleanup_opcode("GETATTR")&&!hfs_cleanup_opcode("DRAIN_WRITE"));
 /* No fence: the unchanged request admission. */
 mutex_lock(&s.lock);
 assert(!hfs_call_admit_locked(&s,false,false,&wait)&&!wait);
 s.blocked=true;assert(hfs_call_admit_locked(&s,false,false,&wait)==-EHOSTDOWN&&!wait);
 assert(!hfs_call_admit_locked(&s,true,true,&wait)&&!wait);s.blocked=false;
 /* An ordinary owner mid-ALLOCATE: every caller waits. */
 s.allocation_pending=&owner;
 assert(hfs_call_admit_locked(&s,false,false,&wait)==-EAGAIN&&wait);
 assert(hfs_call_admit_locked(&s,true,true,&wait)==-EAGAIN&&wait);
 /* An earmark: only cleanup waits; ordinary callers may hold VFS locks. */
 owner.earmarked=true;
 assert(hfs_call_admit_locked(&s,false,false,&wait)==-EAGAIN&&!wait);
 assert(hfs_call_admit_locked(&s,true,false,&wait)==-EAGAIN&&!wait);
 assert(hfs_call_admit_locked(&s,true,true,&wait)==-EAGAIN&&wait);
 owner.earmarked=false;
 /* Uncertain, failed or closing: nothing clears the fence, refuse at once. */
 s.allocation_uncertain=true;
 assert(hfs_call_admit_locked(&s,true,true,&wait)==-EAGAIN&&!wait);
 s.allocation_uncertain=false;owner.allocation_failed=true;
 assert(hfs_call_admit_locked(&s,true,true,&wait)==-EAGAIN&&!wait);
 owner.allocation_failed=false;s.closing=true;
 assert(hfs_call_admit_locked(&s,true,true,&wait)==-EAGAIN&&!wait);
 mutex_unlock(&s.lock);
 assert(!pthread_mutex_destroy(&s.lock.native));
}

static void x84_bounded_wait(void) {
 struct hfs_session s={0};struct hfs_request owner={0};struct x84_clearer c;
 mutex_init(&s.lock);s.next_claim=1;
 /* Waits until the owner clears, then admits. */
 unsigned long start=jiffies;
 x84_clear_start(&c,&s,&owner,40,false);
 assert(!hfs_call_admit(&s,true,true,start+msecs_to_jiffies(HFS_ALLOCATION_WAIT_MS)));
 assert(atomic_load(&c.cleared)&&jiffies-start>=40);
 x84_clear_join(&c);
 /* At the deadline the old -EAGAIN comes back and the fence stays. */
 s.allocation_pending=&owner;
 assert(hfs_call_admit(&s,true,true,jiffies)==-EAGAIN&&s.allocation_pending==&owner);
 /* A truly uncertain allocation refuses without waiting. */
 s.allocation_uncertain=true;start=jiffies;
 assert(hfs_call_admit(&s,true,true,start+msecs_to_jiffies(HFS_ALLOCATION_WAIT_MS))==-EAGAIN);
 assert(jiffies-start<1000);
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

static void x84_admission_regression(struct fixture_session *f) {
 x84_decision_matrix();
 x84_bounded_wait();
 bool passed=regression_child("x84-release-waits",x84_release_waits,f);
 passed&=regression_child("x84-release-uncertain",x84_release_uncertain,f);
 assert(passed);
 fprintf(stderr,"PASS: x84 pending-allocation admission: decision matrix, bounded wait, hfs_release waits for a clearing owner and still refuses an uncertain one\n");
}
