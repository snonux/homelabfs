/* SPDX-License-Identifier: GPL-2.0-only */
#ifndef HFS_REQUEST_TEST_COMPAT_H
#define HFS_REQUEST_TEST_COMPAT_H
#include "hfs_codec_compat.h"
#include "kernel_format.h"
#include <assert.h>
#include <pthread.h>
#include <stdatomic.h>
#include <stdio.h>
#include <unistd.h>
struct mutex { pthread_mutex_t native; };
typedef int spinlock_t;
typedef int wait_queue_head_t;
struct kref { unsigned int refcount; };
struct list_head { struct list_head *next, *prev; };
#define container_of(p, t, m) ((t *)((char *)(p) - offsetof(t, m)))
#define INIT_LIST_HEAD(h) do { (h)->next = (h); (h)->prev = (h); } while (0)
#define list_empty(h) ((h)->next == (h))
#define list_for_each_entry(p, h, m) \
   for (p = container_of((h)->next, __typeof__(*p), m); \
        &p->m != (h); p = container_of(p->m.next, __typeof__(*p), m))
#define list_for_each_entry_safe(p, n, h, m) \
   for (p = container_of((h)->next, __typeof__(*p), m), \
        n = container_of(p->m.next, __typeof__(*p), m); \
        &p->m != (h); p = n, n = container_of(n->m.next, __typeof__(*n), m))
static inline void list_add_tail(struct list_head *p, struct list_head *h) {
   p->prev = h->prev; p->next = h; h->prev->next = p; h->prev = p;
}
static inline void list_del(struct list_head *p) {
   p->prev->next = p->next; p->next->prev = p->prev;
}
#define mutex_init(p) assert(!pthread_mutex_init(&(p)->native, NULL))
#define mutex_lock(p) assert(!pthread_mutex_lock(&(p)->native))
#define mutex_unlock(p) assert(!pthread_mutex_unlock(&(p)->native))
#define WARN_ON_ONCE(x) ((x) ? (assert(!(x)), 1) : 0)
#define EXPORT_SYMBOL_GPL(x)
struct hfs_request;
struct hfs_session;
struct hfs_provider {
   int (*reserve)(struct hfs_session *, u64, bool, void **);
   void (*unreserve)(struct hfs_session *, void *);
   int (*retirement)(struct hfs_session *, u64, const u8 *);
   int (*resolve_gap)(struct hfs_session *, struct hfs_request *);
};
struct hfs_session {
   struct mutex lock, retirement;
   struct list_head requests;
   u32 nr_requests, nr_control_requests;
   struct {
      uuid_t anchor, nonce, realm;
      u8 subject[1024];
      u32 subject_len;
   } enrollment;
   struct hfs_provider *provider;
};
struct hfs_buf;
struct hfs_json;
static int hfs_control(struct hfs_session *, const char *, const struct hfs_buf *,
                       bool, struct hfs_json **);
#endif
