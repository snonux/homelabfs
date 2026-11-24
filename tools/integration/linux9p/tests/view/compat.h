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
/* 5f4: a wait queue counts its wake-ups (wake_up_all), so a case can
 * require that a stop wakes a parked waiter (view_wait_wakes below). An
 * entry records the queue it was added to (hfs_view_watch_stops).
 */
typedef struct { unsigned long wakes; } wait_queue_head_t;
struct wait_queue_entry { wait_queue_head_t *head; };
struct list_head { struct list_head *next, *prev; };
struct kref { atomic_int refcount; };
struct address_space { int id; bool exiting; const struct address_space_operations *a_ops; };
typedef int gfp_t;
struct page { bool locked, compound, anon, marker, uptodate, mapped, writeback, pinned, dirty; void *private; int refs; };
struct folio { struct page page; struct address_space *mapping; };
struct file { struct inode *inode; void *private_data; };
/* json and rights (as in hfs.h) serve hfs_cap_write and
 * hfs_lease_acquire_valid in view --pressure (ch4).
 */
struct hfs_json;
struct hfs_cap { struct hfs_json *json; u32 rights; u8 token[32]; };
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
/* i_lock, i_state and i_data, and hfs_inode's object and forget, serve the
 * ie4 forget-record checks (hfs_inode_get_fail, hfs_evict_inode; view
 * --pressure). A zeroed spinlock_t is an unlocked glibc mutex. view_mmapped
 * mirrors hfs.h (tj4: hfs_view_mmap_inode's mark). wj4: run.py's
 * hfs_inode_layout_guard refuses a struct hfs_inode whose members differ
 * from fs/9p/hfs.h by name or order.
 */
struct inode { struct address_space *i_mapping; int refs; spinlock_t i_lock; unsigned long i_state; struct address_space i_data; };
struct hfs_forget;
struct hfs_inode { struct list_head view_list; bool view_mmapped; struct inode inode; struct hfs_cap *object; struct hfs_forget *forget; };
struct hfs_view;
/* leave: hfs_mmap_end's provider exit (ie4, view --pressure). */
struct hfs_provider {
   int (*reserve)(void *, u64, bool, void **);
   void (*unreserve)(void *, void *);
   void (*leave)(void *, void *);
};
/* bridge_minor (u16, as in hfs.h)/closing/blocked only let the minor4
 * controlled-session gate in hfs_view_refused (hfs_view_try_enter_locked and
 * its wait) compile. The view fixtures keep bridge_minor == 0, so that gate
 * is not exercised there.
 * enrollment.session and block_reported feed hfs_mmap_fail's
 * once-per-session line (1a4, view --pressure), and exporter_error its raw
 * exporter ERROR reason (oc4). transport_failed (oc4) ends hfs_mmap_begin's
 * re-check retries (ie4, view --pressure); the ie4 cases set bridge_minor 4
 * only for their blocked-session check. forgets and control (the FORGET
 * queue) serve the ie4 forget-record checks. domain and
 * enrollment.context (ch4) serve the actual hfs_lease_acquire_ex.
 */
struct hfs_session {
   struct hfs_view *view; struct hfs_provider *provider; struct mutex lock;
   u16 bridge_minor; bool closing; bool blocked;
   struct hfs_cap *domain;
   struct { uuid_t session; char context[64]; u32 context_len; } enrollment;
   unsigned long block_reported;
   u16 exporter_error; bool transport_failed;
   struct list_head forgets; void *control;
   struct mutex retirement; /* 1h4: hfs_retirement_unlock (hfs_view.c) */
   wait_queue_head_t stop_wait; /* 5f4: hfs_view_watch_stops (hfs_view.c) */
};
/* hh4: the actual hfs_session_block (view --pressure) queues the abandon of
 * the session's stranded claims. These fixtures hold no claims, so nothing
 * is queued; mount_hello and controlled_received compile the real
 * hfs_session_abandon_queue.
 */
static inline void hfs_session_abandon_queue(struct hfs_session *session) { (void)session; }

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
#define mutex_trylock(p) (pthread_mutex_trylock(&(p)->native) == 0)
/* 1h4: view->retirement_releases. */
typedef atomic_long atomic_long_t;
#define atomic_long_read(p) atomic_load(p)
#define atomic_long_inc(p) ((void)atomic_fetch_add((p), 1))
#define spin_lock(p) assert(pthread_mutex_lock(p) == 0)
#define spin_unlock(p) assert(pthread_mutex_unlock(p) == 0)
#define spin_lock_init(p) assert(pthread_mutex_init(p, NULL) == 0)
#define spin_lock_irqsave(p, flags) ((void)(flags), assert(pthread_mutex_lock(p) == 0))
#define spin_unlock_irqrestore(p, flags) ((void)(flags), assert(pthread_mutex_unlock(p) == 0))
#define init_waitqueue_head(p) ((p)->wakes = 0)
#define wake_up_all(p) ((void)__atomic_fetch_add(&(p)->wakes, 1, __ATOMIC_SEQ_CST))
/* 5f4: the queues a waiter watches besides the one it parks on
 * (add_wait_queue; hfs_view_watch_stops), innermost last. The entry must be
 * removed again before the waiter returns (view_wait_nr_watched back to 0).
 * current is NULL here, as in pressure/compat_extra.h.
 */
#define current NULL
static wait_queue_head_t *view_wait_watched[4];
static unsigned int view_wait_nr_watched;
#define init_waitqueue_entry(e, task) ((void)(task), (e)->head = NULL)
static inline void add_wait_queue(wait_queue_head_t *q, struct wait_queue_entry *e) {
   assert(view_wait_nr_watched < 4 && !e->head);
   e->head = q; view_wait_watched[view_wait_nr_watched++] = q;
}
static inline void remove_wait_queue(wait_queue_head_t *q, struct wait_queue_entry *e) {
   assert(view_wait_nr_watched && e->head == q && view_wait_watched[view_wait_nr_watched - 1] == q);
   view_wait_nr_watched--; e->head = NULL;
}
/* Wake-ups so far of q and of every watched queue. */
static inline unsigned long view_wait_woken(wait_queue_head_t *q) {
   unsigned long n = __atomic_load_n(&q->wakes, __ATOMIC_SEQ_CST);
   for (unsigned int i = 0; i < view_wait_nr_watched; i++)
      n += __atomic_load_n(&view_wait_watched[i]->wakes, __ATOMIC_SEQ_CST);
   return n;
}
#define wait_event(q, cond) do { (void)(q); while (!(cond)) usleep(100); } while (0)
/* view_wait_bounded: a case that requires a bounded wait (1a4) fails at once
 * on an untimed sleep instead of spinning until the stage limit.
 */
static bool view_wait_bounded __attribute__((unused));
#define wait_event_killable(q, cond) ({ \
   if (view_wait_bounded && !(cond)) { fprintf(stderr, "untimed wait_event_killable\n"); abort(); } \
   wait_event(q, cond); 0; })
/* Settable clock (p84's pressure hold, 1a4's hfs_view_enter bound). */
static unsigned long jiffies;
#define time_before(a,b) ((long)((a)-(b))<0)
#define msecs_to_jiffies(m) ((unsigned long)(m))
#define ERESTARTSYS 512
/* wait_event_killable_timeout substitute (1a4). Nothing wakes a test sleeper,
 * so each simulated sleep advances jiffies by view_wait_step (0: the whole
 * remaining timeout) and then calls view_wait_hook, which may change the
 * view like a recall step would. view_wait_fatal stands for a pending fatal
 * signal. A wait that keeps sleeping (a deadline restarted on every wake-up)
 * aborts after 100000 sleeps. The result follows the kernel macro: -ERESTARTSYS, 0 when the
 * condition is still false at the timeout, otherwise the jiffies left (at
 * least 1).
 *
 * 5f4: with view_wait_wakes set, a sleep ends early only if its hook woke q
 * or a watched queue (view_wait_woken), as in the kernel; otherwise it runs
 * to the timeout whatever the condition says. Without it, every step
 * re-checks the condition (the pre-5f4 model).
 */
static unsigned long view_wait_step, view_wait_sleeps;
static bool view_wait_fatal, view_wait_wakes;
static void (*view_wait_hook)(void);
#define wait_event_killable_timeout(q, cond, timeout) ({ \
   long __left = (long)(timeout); \
   while (!(cond)) { \
      unsigned long __step, __woken = view_wait_woken(&(q)); \
      if (view_wait_fatal) { __left = -ERESTARTSYS; break; } \
      if (__left <= 0) { __left = 0; break; } \
      __step = view_wait_step && view_wait_step < (unsigned long)__left ? \
               view_wait_step : (unsigned long)__left; \
      jiffies += __step; __left -= (long)__step; \
      if (++view_wait_sleeps > 100000) { fprintf(stderr, "unbounded wait\n"); abort(); } \
      if (view_wait_hook) view_wait_hook(); \
      if (view_wait_wakes && view_wait_woken(&(q)) == __woken) { jiffies += (unsigned long)__left; __left = 0; } \
   } \
   if (__left == 0 && (cond)) __left = 1; \
   __left; })
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
