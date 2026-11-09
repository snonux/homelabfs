// SPDX-License-Identifier: GPL-2.0-only
/* Actual net/9p fixed-tag exchange with two concurrent owners (task 964).
 *
 * client.inc holds the verbatim tag allocator, request lifetime, receive
 * callback, RPC wait and hfs exchange from net/9p/client.c. This file only
 * substitutes kernel primitives and a virtio-like transport whose receive
 * thread runs the actual p9_client_cb. Like a preempted vCPU between wake_up
 * and p9_req_put, the substituted wake_up holds the callback's last reference
 * until another owner probes the same tag (or 50 ms pass). That makes the
 * zq3-r5 interleaving deterministic instead of timing dependent.
 */
#define _GNU_SOURCE
#include <errno.h>
#include <pthread.h>
#include <stdarg.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <time.h>
#include <unistd.h>

typedef uint8_t u8;
typedef uint16_t u16;
typedef uint32_t u32;
typedef uint64_t u64;
typedef int8_t s8;
typedef int16_t s16;
typedef int32_t s32;
typedef unsigned int uint;

#define CHECK(e) do { if (!(e)) { fprintf(stderr, "%d: %s\n", __LINE__, #e); exit(1); } } while (0)
#define MAX_ERRNO 4095
#define ERESTARTSYS 512 /* kernel-internal errno */
#define IS_ERR(p) ((unsigned long)(p) >= (unsigned long)-MAX_ERRNO)
#define ERR_PTR(e) ((void *)(long)(e))
#define PTR_ERR(p) ((long)(p))
#define min_t(t, a, b) ((t)(a) < (t)(b) ? (t)(a) : (t)(b))
#define likely(p) (p)
#define unlikely(p) (p)
#define EXPORT_SYMBOL(name)
#define EXPORT_SYMBOL_GPL(name)
#define p9_debug(...) ((void)0)
#define trace_9p_client_req(...) ((void)0)
#define trace_9p_client_res(...) ((void)0)
#define trace_9p_protocol_dump(...) ((void)0)
#define GFP_NOFS 0
#define GFP_NOWAIT 0
#define READ_ONCE(x) __atomic_load_n(&(x), __ATOMIC_ACQUIRE)
#define WRITE_ONCE(x, v) __atomic_store_n(&(x), (v), __ATOMIC_RELEASE)
#define smp_wmb() __atomic_thread_fence(__ATOMIC_RELEASE)
#define smp_rmb() __atomic_thread_fence(__ATOMIC_ACQUIRE)

/* Clock: HZ 1000, jiffies in milliseconds. */
#define HZ 1000UL
#define time_after_eq(a, b) ((long)((a) - (b)) >= 0)
static unsigned long now_ms(void)
{
	struct timespec t;

	CHECK(!clock_gettime(CLOCK_MONOTONIC, &t));
	return t.tv_sec * 1000UL + t.tv_nsec / 1000000;
}
#define jiffies now_ms()
#define usleep_range(low, high) usleep(low)

/* Locks. */
typedef pthread_mutex_t spinlock_t;
static void spin_lock_irq(spinlock_t *lock) { CHECK(!pthread_mutex_lock(lock)); }
static void spin_unlock_irq(spinlock_t *lock) { CHECK(!pthread_mutex_unlock(lock)); }
#define spin_lock_irqsave(l, f) ((void)(f), spin_lock_irq(l))
#define spin_unlock_irqrestore(l, f) ((void)(f), spin_unlock_irq(l))
struct mutex { pthread_mutex_t native; };
#define mutex_lock(m) CHECK(!pthread_mutex_lock(&(m)->native))
#define mutex_unlock(m) CHECK(!pthread_mutex_unlock(&(m)->native))

/* Signals never pend in this harness (hfs waits are uninterruptible). */
struct sighand_struct { spinlock_t siglock; };
struct task_struct { struct sighand_struct *sighand; };
static struct sighand_struct test_sighand = { PTHREAD_MUTEX_INITIALIZER };
static struct task_struct test_task = { &test_sighand };
#define current (&test_task)
#define signal_pending(t) ((void)(t), 0)
#define fatal_signal_pending(t) ((void)(t), 0)
#define clear_thread_flag(f) ((void)0)
#define recalc_sigpending() ((void)0)

/* Reference counts and wait queues. */
typedef struct { int refs; } refcount_t;
#define refcount_set(r, v) __atomic_store_n(&(r)->refs, (v), __ATOMIC_SEQ_CST)
#define refcount_dec_and_test(r) (__atomic_sub_fetch(&(r)->refs, 1, __ATOMIC_SEQ_CST) == 0)
typedef struct { pthread_mutex_t lock; pthread_cond_t cond; } wait_queue_head_t;
static void init_waitqueue_head(wait_queue_head_t *q)
{
	CHECK(!pthread_mutex_init(&q->lock, NULL) && !pthread_cond_init(&q->cond, NULL));
}
static void wake_up(wait_queue_head_t *q);
#define wait_event(q, condition) do {						\
	struct timespec until_;							\
	CHECK(!pthread_mutex_lock(&(q).lock));					\
	while (!(condition)) {							\
		CHECK(!clock_gettime(CLOCK_REALTIME, &until_));			\
		until_.tv_nsec += 1000000;					\
		if (until_.tv_nsec >= 1000000000) {				\
			until_.tv_sec++;					\
			until_.tv_nsec -= 1000000000;				\
		}								\
		pthread_cond_timedwait(&(q).cond, &(q).lock, &until_);		\
	}									\
	CHECK(!pthread_mutex_unlock(&(q).lock));				\
} while (0)
#define wait_event_killable(q, condition) ({ wait_event(q, condition); 0; })
struct list_head { struct list_head *next, *prev; };
#define INIT_LIST_HEAD(h) ((h)->next = (h)->prev = (h))

/* idr: one slot per 16-bit tag; callers hold c->lock as in the kernel. */
static unsigned long tag_probes; /* idr_alloc/idr_find calls, any tag */
struct idr { void *slots[65536]; };
#define idr_preload(g) ((void)0)
#define idr_preload_end() ((void)0)
static int idr_alloc(struct idr *idr, void *ptr, int start, int end, int gfp)
{
	int id;

	__atomic_add_fetch(&tag_probes, 1, __ATOMIC_SEQ_CST);
	for (id = start; id < end; id++)
		if (!idr->slots[id]) {
			idr->slots[id] = ptr;
			return id;
		}
	return -ENOSPC;
}
static void *idr_find(struct idr *idr, unsigned long id)
{
	__atomic_add_fetch(&tag_probes, 1, __ATOMIC_SEQ_CST);
	return idr->slots[id];
}
static void idr_remove(struct idr *idr, unsigned long id)
{
	idr->slots[id] = NULL;
}

/* Slab: counted so the test can prove every request was freed. */
static long live_objects;
struct kmem_cache { size_t size; };
static void *kmem_cache_alloc(struct kmem_cache *cache, int gfp)
{
	__atomic_add_fetch(&live_objects, 1, __ATOMIC_SEQ_CST);
	return malloc(cache->size);
}
static void kmem_cache_free(struct kmem_cache *cache, void *object)
{
	__atomic_sub_fetch(&live_objects, 1, __ATOMIC_SEQ_CST);
	free(object);
}
static void *kmalloc(size_t size, int gfp)
{
	__atomic_add_fetch(&live_objects, 1, __ATOMIC_SEQ_CST);
	return malloc(size);
}
static void kfree(const void *object)
{
	if (object)
		__atomic_sub_fetch(&live_objects, 1, __ATOMIC_SEQ_CST);
	free((void *)object);
}

/* include/net/9p subset (values from 9p.h/client.h). */
enum p9_msg_t {
	P9_RLERROR = 7, P9_TVERSION = 100, P9_RVERSION = 101, P9_RERROR = 107,
	P9_TFLUSH = 108, P9_TCLUNK = 120, P9_TBRIDGE = 240, P9_RBRIDGE = 241,
};
#define P9_NOTAG ((u16)(~0))
enum p9_proto_versions {
	p9_proto_legacy, p9_proto_2000u, p9_proto_2000L, p9_proto_hfs1,
	p9_proto_hfs11, p9_proto_hfs12, p9_proto_hfs13, p9_proto_hfs14,
};
enum p9_trans_status { Connected, BeginDisconnect, Disconnected, Hung };
enum p9_req_status_t {
	REQ_STATUS_ALLOC, REQ_STATUS_UNSENT, REQ_STATUS_SENT, REQ_STATUS_RCVD,
	REQ_STATUS_FLSHD, REQ_STATUS_ERROR,
};
struct p9_fcall {
	u32 size;
	u8 id;
	u16 tag;
	size_t offset;
	size_t capacity;
	struct kmem_cache *cache;
	u8 *sdata;
};
struct p9_req_t {
	int status;
	int t_err;
	refcount_t refcount;
	wait_queue_head_t wq;
	struct p9_fcall tc;
	struct p9_fcall rc;
	struct list_head req_list;
};
struct p9_client;
struct p9_trans_module {
	bool pooled_rbuffers;
	int (*request)(struct p9_client *c, struct p9_req_t *req);
	int (*cancel)(struct p9_client *c, struct p9_req_t *req);
	int (*cancelled)(struct p9_client *c, struct p9_req_t *req);
};
struct p9_client {
	spinlock_t lock;
	unsigned int msize;
	unsigned char proto_version;
	struct p9_trans_module *trans_mod;
	enum p9_trans_status status;
	struct kmem_cache *fcall_cache;
	struct idr reqs;
	struct mutex hfs_control_tag;
	struct mutex hfs_wait_tag;
};
static struct kmem_cache req_cache = { sizeof(struct p9_req_t) };
static struct kmem_cache *p9_req_cache = &req_cache;

static int p9_is_proto_dotl(struct p9_client *c) { return c->proto_version >= p9_proto_2000L; }
static int p9_is_proto_dotu(struct p9_client *c) { return c->proto_version == p9_proto_2000u; }
static int p9_errstr2errno(char *name, int len) { return -EIO; }
/* Buffer sizing heuristic only; TBRIDGE frames here are far smaller. */
static int p9_msg_buf_size(struct p9_client *c, int8_t type, const char *fmt, va_list ap)
{
	return 8192;
}

#include "pdu.inc"

static struct p9_req_t *
p9_client_rpc(struct p9_client *c, int8_t type, const char *fmt, ...);

#include "client.inc"

#include "harness.inc"
