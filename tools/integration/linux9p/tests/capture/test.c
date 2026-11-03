// SPDX-License-Identifier: GPL-2.0-only
/* Actual capture/family code; kernel credentials and source mapper are fixtures. */
#include "compat.h"
unsigned int hfs_test_alloc_call,hfs_test_alloc_fail;
size_t hfs_test_last_allocation_size;
#include "production.c"
static atomic_int credits,reserves,deny_credit;
static int reserve(struct hfs_session *s,u64 bytes,bool control,void **out)
{
 (void)s;(void)control;assert(!spin_depth && bytes);
 if(atomic_load(&deny_credit))return -ENOSPC;
 *out=malloc(sizeof(u64));assert(*out);*(u64 *)*out=bytes;
 atomic_fetch_add(&credits,1);atomic_fetch_add(&reserves,1);return 0;
}
static void unreserve(struct hfs_session *s,void *credit)
{ (void)s;assert(!spin_depth && credit);free(credit);assert(atomic_fetch_sub(&credits,1)>0); }
static int map(struct hfs_session *s,const struct cred *c,struct user_namespace *mnt,uuid_t *id,u8 *hash)
{
 (void)s;assert(!spin_depth && c==current_cred());
 memset(id,0,sizeof(*id));id->b[0]=(u8)c->namespace_id;
 memset(hash,c->idmap_id,32);hash[31]^=(u8)mnt->number;
 return c->map_error;
}
static struct hfs_provider provider={map,reserve,unreserve};
static struct group_info groups={.ngroups=3,.gid={2001,7,1002}};
static struct cred base={.fsuid=1001,.fsgid=1002,.group_info=&groups,.refs=ATOMIC_VAR_INIT(1),.namespace_id=1,.idmap_id=2};
static struct pid pid={.refs=ATOMIC_VAR_INIT(1),.number=17};
static struct task_struct task={.pid=&pid,.cred=&base,.real=&base};
static struct hfs_cred *capture(struct hfs_session *s,const struct cred *native)
{
 struct hfs_cred *out=NULL;test_current=&task;task.cred=task.real=native;
 assert(!hfs_capture(s,&init_user_ns,&out) && out);return out;
}
static void clean(struct hfs_session *s)
{
 struct hfs_process *p,*next;
 hfs_view_tickets_destroy(s);
 list_for_each_entry_safe(p,next,&s->processes,list) {
  list_del(&p->list);hfs_cred_free(p->last);put_pid(p->pid);unreserve(s,p->reservation);free(p);
 }
}
static void init(struct hfs_session *s)
{
 memset(s,0,sizeof(*s));s->provider=&provider;s->bridge_minor=1;
 mutex_init(&s->lock);spin_lock_init(&s->processes_lock);
 INIT_LIST_HEAD(&s->processes);INIT_LIST_HEAD(&s->view_tickets);INIT_LIST_HEAD(&s->view_families);
 s->enrollment.nonce.b[0]=1;s->enrollment.view_control_nonce.b[0]=2;
}
struct concurrent {struct hfs_session *s;struct pid *pid;const struct cred *native;u64 revision;struct hfs_cred *snapshot;};
static void *thread(void *arg)
{
 struct concurrent *a=arg;struct task_struct local={.pid=a->pid,.cred=a->native,.real=a->native};
 test_current=&local;
 for(int i=0;i<100;i++) {
  struct hfs_cred *out=NULL;assert(!hfs_capture(a->s,&init_user_ns,&out));
  assert(out->uid==a->native->fsuid && out->ngroups==3);
  if(!a->revision)a->revision=out->revision;else assert(a->revision==out->revision);
  hfs_cred_free(out);
 }
 return NULL;
}
static void *thread_once(void *arg)
{
 struct concurrent *a=arg;struct task_struct local={.pid=a->pid,.cred=a->native,.real=a->native};
 test_current=&local;
 assert(!hfs_capture(a->s,&init_user_ns,&a->snapshot));
 return NULL;
}
int main(void)
{
 struct hfs_session s;init(&s);struct hfs_cred *old=capture(&s,&base);
 assert(old->revision==1 && s.nr_processes==1);
 struct hfs_process *process=hfs_process_find(&s,&pid);assert(process && process->last!=old);
 assert(*(u64 *)process->reservation==sizeof(*process)+sizeof(*old)+sizeof(struct cred)+sizeof(struct group_info)+HFS_GROUPS_MAX*sizeof(kgid_t)+sizeof(struct pid)+sizeof(struct user_namespace));
 struct hfs_view_ticket *historic;assert(!hfs_view_ticket_prepare(&s,old,false,&historic));
 struct hfs_view_family *family=historic->family;
 /* Fixture existing monotone floor/counter; this is not a retirement proof. */
 family->next=6;family->floor=5;
 for(int i=0;i<4100;i++) {
  struct hfs_cred *out=capture(&s,&base);struct hfs_view_ticket *ticket;
  assert(out->revision==1 && out->native==&base);
  assert(!hfs_view_ticket_prepare(&s,out,false,&ticket));assert(ticket->family==family);
  assert(family->next==6 && family->floor==5);hfs_view_ticket_put(ticket);
  assert(!hfs_view_ticket_prepare(&s,out,true,&ticket));hfs_view_ticket_put(ticket);
  hfs_cred_free(out);
 }
 assert(s.nr_view_families==2 && s.nr_view_tickets==1 && process->revision==1);
 /* Unchanged captures need no additional cached-process credit. */
 atomic_store(&deny_credit,1);struct hfs_cred *out=capture(&s,&base);hfs_cred_free(out);atomic_store(&deny_credit,0);
 struct cred equivalent=base;atomic_init(&equivalent.refs,1);
 struct group_info reordered={.ngroups=4,.gid={1002,2001,7,7}};equivalent.group_info=&reordered;
 out=capture(&s,&equivalent);assert(out->revision==1);hfs_cred_free(out);
 struct group_info changed=groups;changed.gid[0]=2002;
 struct cred other=base;atomic_init(&other.refs,1);other.group_info=&changed;
 out=capture(&s,&other);assert(out->revision==2);hfs_cred_free(out);
 out=capture(&s,&base);assert(out->revision==3);hfs_cred_free(out); /* A->B->A */
 other.group_info=&groups;other.fsuid=12001;
 out=capture(&s,&other);assert(out->revision==4);hfs_cred_free(out);
 other.fsgid=53000;out=capture(&s,&other);assert(out->revision==5);hfs_cred_free(out);
 other.namespace_id=2;out=capture(&s,&other);assert(out->revision==6);hfs_cred_free(out);
 other.idmap_id=3;out=capture(&s,&other);assert(out->revision==7);hfs_cred_free(out);
 init_user_ns.number=1;out=capture(&s,&other);assert(out->revision==8);hfs_cred_free(out);
 assert(old->revision==1 && old->uid==1001 && old->groups[2]==2001 && old->native==&base);
 struct hfs_buf expected;assert(!hfs_buf_init(&expected,16384));hfs_cred_write(&expected,old);
 assert(expected.len==family->credentials.len && !memcmp(expected.data,family->credentials.data,expected.len));hfs_buf_free(&expected);
 other.map_error=-EACCES;out=(void *)1;assert(hfs_capture(&s,&init_user_ns,&out)==-EACCES && !out && process->revision==8);other.map_error=0;
 other.namespace_id=0;assert(hfs_capture(&s,&init_user_ns,&out)==-EACCES && process->revision==8);other.namespace_id=2;
 struct group_info excessive={.ngroups=1025};other.group_info=&excessive;
 assert(hfs_capture(&s,&init_user_ns,&out)==-E2BIG && process->revision==8);other.group_info=&groups;
 task.flags=PF_KTHREAD;assert(hfs_capture(&s,&init_user_ns,&out)==-EACCES);task.flags=0;
 task.real=&base;assert(hfs_capture(&s,&init_user_ns,&out)==-EACCES);task.real=&other;
 other.fsuid=U32_MAX;assert(hfs_capture(&s,&init_user_ns,&out)==-EOVERFLOW && process->revision==8);other.fsuid=12001;
 for(int n=1;n<=2;n++) {
  atomic_store(&capture_fail_at,atomic_load(&capture_allocations)+n);
  assert(hfs_capture(&s,&init_user_ns,&out)==-ENOMEM && !out && process->revision==8);
  atomic_store(&capture_fail_at,0);
 }
 /* Concurrent equal captures linearize to one epoch, with no extra cache. */
 pthread_t threads[8];struct concurrent args[8];
 for(int i=0;i<8;i++) {args[i]=(struct concurrent){&s,&pid,&other,0,NULL};assert(!pthread_create(&threads[i],NULL,thread,&args[i]));}
 for(int i=0;i<8;i++) {assert(!pthread_join(threads[i],NULL));assert(args[i].revision==8);}
 process->revision=process->last->revision=U64_MAX;
 out=capture(&s,&other);assert(out->revision==U64_MAX);hfs_cred_free(out);
 other.fsuid++;assert(hfs_capture(&s,&init_user_ns,&out)==-EOVERFLOW && !out);other.fsuid--;
 hfs_view_ticket_put(historic);hfs_cred_free(old);clean(&s);
 assert(!atomic_load(&credits) && atomic_load(&base.refs)==1 && atomic_load(&other.refs)==1 && atomic_load(&equivalent.refs)==1 && atomic_load(&pid.refs)==1);
 /* New-process concurrent admission races retain one credited cache. */
 init(&s);init_user_ns.number=0;
 for(int i=0;i<8;i++) {args[i]=(struct concurrent){&s,&pid,&base,0,NULL};assert(!pthread_create(&threads[i],NULL,thread,&args[i]));}
 for(int i=0;i<8;i++) {assert(!pthread_join(threads[i],NULL));assert(args[i].revision==1);}
 assert(s.nr_processes==1 && atomic_load(&credits)==1);
 args[0]=(struct concurrent){&s,&pid,&base,0,NULL};args[1]=(struct concurrent){&s,&pid,&other,0,NULL};
 for(int i=0;i<2;i++)assert(!pthread_create(&threads[i],NULL,thread_once,&args[i]));
 for(int i=0;i<2;i++)assert(!pthread_join(threads[i],NULL));
 assert(args[0].snapshot->revision!=args[1].snapshot->revision);
 assert(args[0].snapshot->uid==1001 && args[1].snapshot->uid==12001);
 assert(args[0].snapshot->groups[2]==2001 && args[1].snapshot->gid==53000);
 hfs_cred_free(args[0].snapshot);hfs_cred_free(args[1].snapshot);
 clean(&s);
 init(&s);test_current=&task;task.cred=task.real=&base;atomic_store(&deny_credit,1);
 assert(hfs_capture(&s,&init_user_ns,&out)==-ENOSPC && !out && !s.nr_processes);atomic_store(&deny_credit,0);
 assert(!atomic_load(&credits) && atomic_load(&base.refs)==1 && atomic_load(&pid.refs)==1);
 puts("PASS: actual stable capture4100 repeats+ticket families, numeric/group/namespace/idmap changes, A-B-A, immutable historical grants, concurrent captures, credits/refs/errors; no running-kernel identity proof");
 return 0;
}
