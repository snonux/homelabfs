/* SPDX-License-Identifier: GPL-2.0-only */
#ifndef HFS_VIEW_TEST_COMPAT_H
#define HFS_VIEW_TEST_COMPAT_H
#include <assert.h>
#include "hfs_codec_compat.h"
#include <errno.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdatomic.h>
#include <stddef.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <pthread.h>
#include <unistd.h>
#include <limits.h>

typedef pthread_mutex_t spinlock_t;
struct mutex { pthread_mutex_t native; };
typedef struct { int unused; } wait_queue_head_t;
struct list_head { struct list_head *next, *prev; };
struct kref { atomic_int refcount; };
struct address_space { int id; bool exiting; const struct address_space_operations *a_ops; };
typedef int gfp_t;
struct page { bool locked, compound, anon, marker, uptodate, mapped, writeback, pinned, dirty; void *private; int refs; };
struct folio { struct page page; struct address_space *mapping; };
struct file { struct inode *inode; void *private_data; };
struct hfs_cap { u8 token[32]; };
struct hfs_description { struct hfs_cap *cap; };
struct vm_area_struct { struct file *vm_file; };
enum migrate_mode { MIGRATE_ASYNC };
struct address_space_operations {
   bool (*dirty_folio)(struct address_space *, struct folio *);
   int (*launder_folio)(struct folio *);
   void (*invalidate_folio)(struct folio *, size_t, size_t);
   bool (*release_folio)(struct folio *, gfp_t);
   void (*free_folio)(struct folio *);
   int (*migrate_folio)(struct address_space *, struct folio *, struct folio *, enum migrate_mode);
};
bool noop_dirty_folio(struct address_space *,struct folio *);
bool folio_mark_dirty(struct folio *);
bool set_page_dirty(struct page *);
#define likely(x) (x)
#define folio_mapping(f) ((f)->mapping)
#define folio_test_reclaim(f) ((void)(f),false)
#define folio_clear_reclaim(f) ((void)(f))
#define folio_test_dirty(f) __atomic_load_n(&(f)->page.dirty,__ATOMIC_RELAXED)
#define folio_test_set_dirty(f) __atomic_exchange_n(&(f)->page.dirty,true,__ATOMIC_RELAXED)
#define PAGE_SIZE 4096U
#define U64_MAX UINT64_MAX
#define PageLocked(p) ((p)->locked)
#define PageCompound(p) ((p)->compound)
#define get_page(p) ((p)->refs++)
#define put_page(p) ((p)->refs--)
#define lock_page(p) ((p)->locked = true)
#define unlock_page(p) ((p)->locked = false)
#define page_mapped(p) ((p)->mapped)
#define PageWriteback(p) ((p)->writeback)
static inline bool clear_page_dirty_for_io(struct page *page) {
   bool old = page->dirty; page->dirty = false; return old;
}
#define folio_maybe_dma_pinned(f) ((f)->page.pinned)
#define page_mapping(p) ((p)->private ? (void *)1 : NULL)
#define PagePrivate(p) ((p)->private != NULL)
#define PageAnon(p) ((p)->anon)
#define PageHfsnogup(p) ((p)->marker)
#define SetPageHfsnogup(p) ((p)->marker = true)
#define ClearPageHfsnogup(p) ((p)->marker = false)
#define page_folio(p) container_of(p, struct folio, page)
#define file_inode(f) ((f)->inode)
#define mapping_exiting(m) ((m)->exiting)
#define folio_mapped(f) ((f)->page.mapped)
#define folio_get_private(f) ((f)->page.private)
#define folio_clear_uptodate(f) ((f)->page.uptodate = false)
static inline void folio_attach_private(struct folio *f, void *p) {
   assert(!f->page.private); f->page.private = p; f->page.refs++;
}
static inline void folio_detach_private(struct folio *f) {
   assert(f->page.private); f->page.private = NULL; f->page.refs--;
}
struct inode { struct address_space *i_mapping; int refs; };
struct hfs_inode { struct list_head view_list; struct inode inode; };
struct hfs_view;
struct hfs_provider {
   int (*reserve)(void *, u64, bool, void **);
   void (*unreserve)(void *, void *);
};
struct hfs_session { struct hfs_view *view; struct hfs_provider *provider; struct mutex lock; };

#define HFS_IDENTITIES_MAX 4096U
#define THIS_MODULE NULL
#define WRITE_ONCE(p, v) __atomic_store_n(&(p), (v), __ATOMIC_RELEASE)
#define READ_ONCE(x) __atomic_load_n(&(x), __ATOMIC_ACQUIRE)
#define WARN_ON_ONCE(x) ((x) ? (fprintf(stderr, "unexpected WARN: %s\n", #x), abort(), 1) : 0)
#define container_of(p, t, m) ((t *)((char *)(p) - offsetof(t, m)))
#define HFS_I(p) container_of(p, struct hfs_inode, inode)
#define HFS_S(p) ((void)(p), test_session)
#define INIT_LIST_HEAD(h) ((h)->next = (h)->prev = (h))
#define list_empty(h) ((h)->next == (h))
#define list_for_each_entry(p, h, member) \
   for (p = container_of((h)->next, __typeof__(*p), member); \
        &p->member != (h); p = container_of(p->member.next, __typeof__(*p), member))
#define mutex_init(p) assert(pthread_mutex_init(&(p)->native, NULL) == 0)
#define mutex_lock(p) assert(pthread_mutex_lock(&(p)->native) == 0)
#define mutex_unlock(p) assert(pthread_mutex_unlock(&(p)->native) == 0)
#define spin_lock(p) assert(pthread_mutex_lock(p) == 0)
#define spin_unlock(p) assert(pthread_mutex_unlock(p) == 0)
#define spin_lock_init(p) assert(pthread_mutex_init(p, NULL) == 0)
#define spin_lock_irqsave(p, flags) ((void)(flags), assert(pthread_mutex_lock(p) == 0))
#define spin_unlock_irqrestore(p, flags) ((void)(flags), assert(pthread_mutex_unlock(p) == 0))
#define init_waitqueue_head(p) ((void)(p))
#define wake_up_all(p) ((void)(p))
#define wait_event(q, cond) do { (void)(q); while (!(cond)) usleep(100); } while (0)
#define wait_event_killable(q, cond) ({ wait_event(q, cond); 0; })
#define refcount_read(p) atomic_load(p)
static struct hfs_session *test_session;
static atomic_int module_refs, unmaps, reservations;
static struct hfs_view *observed_view;
static inline bool uuid_is_null(const uuid_t *u) {
   uuid_t zero = {{0}}; return memcmp(u, &zero, sizeof(*u)) == 0;
}
static inline bool uuid_equal(const uuid_t *a, const uuid_t *b) {
   return memcmp(a, b, sizeof(*a)) == 0;
}
static inline bool try_module_get(void *p) {
   (void)p; atomic_fetch_add(&module_refs, 1); return true;
}
static inline void module_put(void *p) { (void)p; atomic_fetch_sub(&module_refs, 1); }
static inline void kref_init(struct kref *k) { atomic_init(&k->refcount, 1); }
static inline void kref_get(struct kref *k) { atomic_fetch_add(&k->refcount, 1); }
static inline void kref_put(struct kref *k, void (*release)(struct kref *)) {
   if (atomic_fetch_sub(&k->refcount, 1) == 1) release(k);
}
static inline void list_add_tail(struct list_head *item, struct list_head *head) {
   item->prev = head->prev; item->next = head;
   head->prev->next = item; head->prev = item;
}
static inline void list_del_init(struct list_head *item) {
   item->next->prev = item->prev; item->prev->next = item->next; INIT_LIST_HEAD(item);
}
static inline struct inode *igrab(struct inode *i) { i->refs++; return i; }
static inline void iput(struct inode *i) { i->refs--; }
static void unmap_mapping_range(struct address_space *, int, int, int);
#endif
