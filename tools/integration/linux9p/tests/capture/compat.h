/* SPDX-License-Identifier: GPL-2.0-only */
#ifndef HFS_CAPTURE_COMPAT_H
#define HFS_CAPTURE_COMPAT_H
#include "hfs_codec_compat.h"
#include <assert.h>
#include <pthread.h>
#include <stdatomic.h>
#include <unistd.h>
#include "hfs_policy.h"
#define HFS_IDENTITIES_MAX 4096U
#define HFS_GROUPS_MAX 1024U
#define PF_KTHREAD 1U
struct mutex { pthread_mutex_t native; };
typedef pthread_mutex_t spinlock_t;
typedef int wait_queue_head_t;
typedef atomic_int atomic_t;
struct list_head { struct list_head *next,*prev; };
struct work_struct { int unused; };
struct pid { atomic_int refs; int number; };
struct user_namespace { int number; };
typedef u32 kgid_t;
struct group_info { int ngroups; kgid_t gid[1025]; };
struct cred { u32 fsuid,fsgid; struct group_info *group_info; atomic_int refs; int namespace_id,idmap_id,map_error; };
struct task_struct { unsigned int flags; struct pid *pid; const struct cred *cred,*real; };
static _Thread_local struct task_struct *test_current;
static _Thread_local int spin_depth;
#define current test_current
static struct user_namespace init_user_ns;
#define current_cred() (current->cred)
#define current_real_cred() (current->real)
#define task_tgid(t) ((t)->pid)
#define READ_ONCE(x) __atomic_load_n(&(x),__ATOMIC_ACQUIRE)
#define atomic_read(p) atomic_load(p)
#define from_kuid(ns,v) ((void)(ns),(v))
#define from_kgid(ns,v) ((void)(ns),(v))
#define uid_valid(v) ((v)!=U32_MAX)
#define gid_valid(v) ((v)!=U32_MAX)
#define INIT_WORK(w,fn) ((void)(w),(void)(fn))
#define INIT_LIST_HEAD(h) ((h)->next=(h)->prev=(h))
#define container_of(p,t,m) ((t *)((char *)(p)-offsetof(t,m)))
#define list_empty(h) ((h)->next==(h))
#define list_for_each_entry(p,h,m) for(p=container_of((h)->next,__typeof__(*p),m);&p->m!=(h);p=container_of(p->m.next,__typeof__(*p),m))
#define list_for_each_entry_safe(p,n,h,m) for(p=container_of((h)->next,__typeof__(*p),m),n=container_of(p->m.next,__typeof__(*p),m);&p->m!=(h);p=n,n=container_of(n->m.next,__typeof__(*n),m))
static inline void list_add_tail(struct list_head *p,struct list_head *h) { p->prev=h->prev;p->next=h;h->prev->next=p;h->prev=p; }
static inline void list_del(struct list_head *p) { p->prev->next=p->next;p->next->prev=p->prev; }
#define mutex_init(m) assert(!pthread_mutex_init(&(m)->native,NULL))
#define mutex_lock(m) assert(!pthread_mutex_lock(&(m)->native))
#define mutex_unlock(m) assert(!pthread_mutex_unlock(&(m)->native))
#define spin_lock_init(p) assert(!pthread_mutex_init(p,NULL))
#define spin_lock_irqsave(p,f) ((void)(f),assert(!pthread_mutex_lock(p)),spin_depth++)
#define spin_unlock_irqrestore(p,f) ((void)(f),spin_depth--,assert(!pthread_mutex_unlock(p)))
#define init_waitqueue_head(p) ((void)(p))
#define WARN_ON_ONCE(x) ((x)?(abort(),1):0)
static inline bool uuid_equal(const uuid_t *a,const uuid_t *b) { return !memcmp(a,b,sizeof(*a)); }
static inline void uuid_gen(uuid_t *id) { static atomic_int next=10;memset(id,0,sizeof(*id));unsigned int n=atomic_fetch_add(&next,1);memcpy(id->b,&n,sizeof(n)); }
static inline struct pid *get_pid(struct pid *p) { atomic_fetch_add(&p->refs,1);return p; }
static inline void put_pid(struct pid *p) { if(p) assert(atomic_fetch_sub(&p->refs,1)>1); }
static inline const struct cred *get_cred(const struct cred *c) { assert(atomic_fetch_add((atomic_int *)&c->refs,1)>0);return c; }
static inline void put_cred(const struct cred *c) { assert(!spin_depth);assert(atomic_fetch_sub((atomic_int *)&c->refs,1)>1); }
#define get_current_cred() get_cred(current_cred())
static atomic_int capture_allocations,capture_fail_at;
static void *capture_zalloc(size_t n,int flags) { (void)flags;int number=atomic_fetch_add(&capture_allocations,1)+1;return number==atomic_load(&capture_fail_at)?NULL:calloc(1,n); }
#define kzalloc capture_zalloc
struct hfs_session;
struct hfs_provider {
 int (*map)(struct hfs_session *,const struct cred *,struct user_namespace *,uuid_t *,u8 *);
 int (*reserve)(struct hfs_session *,u64,bool,void **);
 void (*unreserve)(struct hfs_session *,void *);
};
struct hfs_session {
 struct hfs_provider *provider;
 struct mutex lock;
 spinlock_t processes_lock;
 struct list_head processes,view_families,view_tickets;
 unsigned int nr_processes,nr_view_families,nr_view_tickets,nr_view_control_tickets;
 bool blocked,closing;
 u16 bridge_minor;
 struct { uuid_t nonce,view_control_nonce; } enrollment;
};
static void hfs_process_exit_work(struct work_struct *work) { (void)work;abort(); }
#endif
