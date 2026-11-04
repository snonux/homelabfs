/* SPDX-License-Identifier: GPL-2.0-only */
/* Explicit portable kernel primitive boundary, never a proof implementation. */
#ifndef HFS_C6_RUNTIME_COMPAT_H
#define HFS_C6_RUNTIME_COMPAT_H
#include "hfs_codec_compat.h"
#include "kernel_format.h"
#include <assert.h>
#include <pthread.h>
#include <stdatomic.h>
#include <time.h>
#include <unistd.h>
#include <sys/types.h>
#include <sys/stat.h>
#include <openssl/evp.h>
#include <openssl/rand.h>
typedef atomic_int atomic_t;
struct file_system_type;
#define MAX_NON_LFS 0x7fffffff
static inline void sort(void *p,size_t n,size_t s,int (*cmp)(const void *,const void *),void *swap) { (void)swap;qsort(p,n,s,cmp); }
typedef int8_t s8;
typedef pthread_mutex_t spinlock_t;
struct mutex { pthread_mutex_t native; };
struct list_head { struct list_head *next, *prev; };
struct kref { atomic_int refcount; };
typedef struct { int unused; } wait_queue_head_t;
struct work_struct { int unused; };
struct module;
struct inode;
struct pid;
struct cred;
struct device;
struct user_namespace;
struct super_block { void *s_fs_info; dev_t s_dev; struct user_namespace *s_user_ns; };
struct inode { struct address_space *i_mapping; struct super_block *i_sb; int refs; };
struct file { struct inode *inode; void *private_data; };
struct vm_area_struct { struct file *vm_file; };
struct address_space { bool exiting; const struct address_space_operations *a_ops; };
struct page { bool locked,compound,anon,marker,uptodate,mapped,writeback,pinned,dirty; void *private; int refs;u8 data[4096]; };
struct folio { struct page page; struct address_space *mapping; };
struct device { unsigned references; bool registered; const char *name; };
struct p9_client { void *trans; void *trans_mod; int status; int proto_version;u32 msize;struct mutex hfs_control_tag,hfs_wait_tag; };
#define container_of(p,t,m) ((t *)((char *)(p)-offsetof(t,m)))
#define LIST_HEAD(name) struct list_head name = { &name, &name }
#define INIT_LIST_HEAD(h) ((h)->next=(h)->prev=(h))
#define list_empty(h) ((h)->next==(h))
#define list_for_each_entry(p,h,m) for(p=container_of((h)->next,__typeof__(*p),m); &p->m!=(h); p=container_of(p->m.next,__typeof__(*p),m))
#define list_for_each_entry_safe(p,n,h,m) for(p=container_of((h)->next,__typeof__(*p),m),n=container_of(p->m.next,__typeof__(*p),m); &p->m!=(h); p=n,n=container_of(n->m.next,__typeof__(*n),m))
static inline void list_add_tail(struct list_head *p,struct list_head *h) { p->prev=h->prev;p->next=h;h->prev->next=p;h->prev=p; }
static inline void list_del(struct list_head *p) { p->prev->next=p->next;p->next->prev=p->prev; }
static inline void list_move_tail(struct list_head *p,struct list_head *h) { list_del(p);list_add_tail(p,h); }
static inline void list_del_init(struct list_head *p) { list_del(p);INIT_LIST_HEAD(p); }
#define DEFINE_MUTEX(name) struct mutex name = { PTHREAD_MUTEX_INITIALIZER }
#define mutex_init(m) assert(!pthread_mutex_init(&(m)->native,NULL))
#define mutex_lock(m) assert(!pthread_mutex_lock(&(m)->native))
extern void primitive_after_mutex_unlock(struct mutex *);
#define mutex_unlock(m) do { assert(!pthread_mutex_unlock(&(m)->native)); primitive_after_mutex_unlock(m); } while(0)
#define spin_lock_init(m) assert(!pthread_mutex_init(m,NULL))
#define spin_lock(m) assert(!pthread_mutex_lock(m))
#define spin_unlock(m) assert(!pthread_mutex_unlock(m))
#define spin_lock_irqsave(m,f) ((void)(f),spin_lock(m))
#define spin_unlock_irqrestore(m,f) ((void)(f),spin_unlock(m))
#define READ_ONCE(p) __atomic_load_n(&(p),__ATOMIC_ACQUIRE)
#define WRITE_ONCE(p,v) __atomic_store_n(&(p),v,__ATOMIC_RELEASE)
#define WARN_ON_ONCE(p) ((p)?(fprintf(stderr,"unexpected WARN %s\n",#p),abort(),1):0)
#define EXPORT_SYMBOL_GPL(name)
#define __init
#define late_initcall(name)
#define THIS_MODULE NULL
#define try_module_get(m) ((void)(m),true)
#define module_put(m) ((void)(m))
#define might_sleep() ((void)0)
#define likely(p) (p)
#define unlikely(p) (p)
#define check_add_overflow(a,b,out) __builtin_add_overflow(a,b,out)
#define kcalloc(n,s,g) kvcalloc(n,s,g)
#define kmalloc(n,g) kvmalloc(n,g)
#define kmemdup(p,n,g) ({ void *q=kvmalloc(n,g);if(q)memcpy(q,p,n);q; })
#define P9_NOTAG 65535U
#define HZ 1000UL
#define msecs_to_jiffies(n) (n)
#define time_after_eq(a,b) ((long)((a)-(b))>=0)
static inline unsigned long primitive_milliseconds(void) { struct timespec t;assert(!clock_gettime(CLOCK_MONOTONIC,&t));return t.tv_sec*1000UL+t.tv_nsec/1000000; }
#define jiffies primitive_milliseconds()
void primitive_sleep(unsigned long,unsigned long);
#define usleep_range(a,b) primitive_sleep(a,b)
static inline void kref_init(struct kref *k) { atomic_init(&k->refcount,1); }
static inline void kref_get(struct kref *k) { atomic_fetch_add(&k->refcount,1); }
static inline void kref_put(struct kref *k,void (*release)(struct kref *)) { if(atomic_fetch_sub(&k->refcount,1)==1)release(k); }
#define refcount_read(p) atomic_load(p)
#define init_waitqueue_head(q) ((void)(q))
#define wake_up_all(q) ((void)(q))
#define wait_event(q,p) do { (void)(q);while(!(p))usleep(100); } while(0)
#define wait_event_killable(q,p) ({wait_event(q,p);0;})
typedef int gfp_t;
enum migrate_mode { MIGRATE_ASYNC };
struct address_space_operations {
 bool (*dirty_folio)(struct address_space *,struct folio *);
 int (*launder_folio)(struct folio *);
 void (*invalidate_folio)(struct folio *,size_t,size_t);
 bool (*release_folio)(struct folio *,gfp_t);
 void (*free_folio)(struct folio *);
 int (*migrate_folio)(struct address_space *,struct folio *,struct folio *,enum migrate_mode);
};
#define PageLocked(p) ((p)->locked)
#define PageCompound(p) ((p)->compound)
#define PageAnon(p) ((p)->anon)
#define PageDirty(p) ((p)->dirty)
#define PageWriteback(p) ((p)->writeback)
#define PagePrivate(p) ((p)->private != NULL)
#define PageHfsnogup(p) ((p)->marker)
#define SetPageHfsnogup(p) ((p)->marker=true)
#define ClearPageHfsnogup(p) ((p)->marker=false)
#define get_page(p) ((p)->refs++)
#define put_page(p) ((p)->refs--)
#define lock_page(p) ((p)->locked=true)
#define unlock_page(p) ((p)->locked=false)
#define page_mapped(p) ((p)->mapped)
#define page_mapping(p) (page_folio(p)->mapping)
#define kmap_local_page(p) ((p)->data)
#define kunmap_local(p) ((void)(p))
#define mapping_set_error(mapping,error) ((void)(mapping),(void)(error))
#define page_folio(p) container_of(p,struct folio,page)
#define folio_mapping(f) ((f)->mapping)
#define folio_mapped(f) ((f)->page.mapped)
#define folio_maybe_dma_pinned(f) ((f)->page.pinned)
#define folio_get_private(f) ((f)->page.private)
#define folio_clear_uptodate(f) ((f)->page.uptodate=false)
#define folio_test_dirty(f) ((f)->page.dirty)
#define folio_test_reclaim(f) ((void)(f),false)
#define folio_clear_reclaim(f) ((void)(f))
#define folio_test_set_dirty(f) __atomic_exchange_n(&(f)->page.dirty,true,__ATOMIC_RELAXED)
static inline void folio_attach_private(struct folio *f,void *p) { assert(!f->page.private);f->page.private=p;f->page.refs++; }
static inline void folio_detach_private(struct folio *f) { assert(f->page.private);f->page.private=NULL;f->page.refs--; }
static inline bool clear_page_dirty_for_io(struct page *p) { bool d=p->dirty;p->dirty=false;return d; }
#define mapping_exiting(m) ((m)->exiting)
#define file_inode(f) ((f)->inode)
static inline struct inode *igrab(struct inode *i) { i->refs++;return i; }
static inline void iput(struct inode *i) { i->refs--; }
static inline void unmap_mapping_range(struct address_space *m,int a,int b,int c) { (void)m;(void)a;(void)b;(void)c; }
static inline int invalidate_inode_pages2(struct address_space *m) { return m->exiting?-EBUSY:0; }
static inline bool noop_dirty_folio(struct address_space *m,struct folio *f) { (void)m;return !f->page.dirty; }
static inline bool folio_mark_dirty(struct folio *f) { return folio_test_set_dirty(f); }
static inline bool set_page_dirty(struct page *p) { bool old=p->dirty;p->dirty=true;return !old; }
#define PAGE_SIZE 4096U
struct cred { struct user_namespace *user_ns; };
#define get_cred(c) (c)
#define put_cred(c) ((void)(c))
static inline struct device *get_device(struct device *d) { if(d)d->references++;return d; }
static inline void put_device(struct device *d) { if(d){assert(d->references);d->references--;} }
int p9_client_hfs_exchange(struct p9_client *,u16,u8,u8,u64,u32,const void *,u32,void *,u32 *);
int p9_client_hfs_virtio_identity(struct p9_client *,struct device **,char *,size_t,char *,size_t);
#endif
