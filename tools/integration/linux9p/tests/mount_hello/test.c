// SPDX-License-Identifier: GPL-2.0-only
/* Actual v9fs_hfs_mount ordering: cache option, enroll, validate, HELLO,
 * selection check; then the mounted superblock's mountinfo options (g64).
 * hfs_session_block's once-per-session log is checked directly, also under a
 * dropping ratelimit and through the controlled stop fence (m64), and so is
 * the enrollment Bounds Sources floor (hfs_go_bounds, m64).
 *
 * mount.inc, validate.inc, before_send.inc, show_options.inc, stop.inc and
 * bounds.inc are verbatim production definitions; bounds_fixtures.inc holds
 * the daemon's Sources floor constants and real Bounds (see run.py). The VFS/module/tracepoint/p9 primitives, the
 * controlled registry lookups and the native exchange are explicit
 * substitutions. The exchange applies the real controlled pre-send HELLO
 * check and then echoes the sent payload as the runtime selection, exactly
 * like internal/guestbridge/service.go. Each case runs in a forked child so a
 * crash in one case is reported without hiding the others.
 */
#include "hfs_policy.h"
#include <assert.h>
#include <sys/wait.h>
#include <unistd.h>

unsigned int hfs_test_alloc_call, hfs_test_alloc_fail;
size_t hfs_test_last_allocation_size;
#define CHECK(e) do { if (!(e)) { fprintf(stderr, "%d: %s\n", __LINE__, #e); _exit(1); } } while (0)

/* Kernel/VFS primitives used by the mount and by show_options. */
struct module { int unused; };
struct user_namespace { int unused; };
struct file_system_type { int unused; };
struct super_block { void *s_fs_info; };
struct dentry { struct super_block *d_sb; };
struct workqueue_struct { int unused; };
struct tracepoint { int unused; };
struct mutex { int held; };
typedef int spinlock_t;
typedef int atomic_t;
struct list_head { struct list_head *next, *prev; };
/* include/net/9p/client.h order: stock versions first, then the hfs ones. */
enum p9_proto_versions {
	p9_proto_legacy, p9_proto_2000u, p9_proto_2000L,
	p9_proto_hfs1, p9_proto_hfs11, p9_proto_hfs12, p9_proto_hfs13, p9_proto_hfs14,
};
struct seq_file { char buf[512]; size_t len; };
struct p9_client;
struct p9_trans_module {
	const char *name;
	int (*show_options)(struct seq_file *m, struct p9_client *client);
};
struct p9_client { int proto_version; unsigned int msize; struct p9_trans_module *trans_mod; };
#define DEFAULT_MSIZE (128 * 1024) /* net/9p/client.c in 6.1.190 */
/* Kernel log substitute: count and keep the last message, still print it. */
static unsigned int warnings;
static char last_warning[256];
static __attribute__((format(printf, 1, 2))) void test_warn(const char *f, ...)
{
	va_list args;

	va_start(args, f);
	vsnprintf(last_warning, sizeof(last_warning), f, args);
	va_end(args);
	warnings++;
	fputs(last_warning, stderr);
}
/* printk_ratelimited substitute. With ratelimit_drops set it drops the line,
 * as the kernel does inside a burst; hfs_session_block must not depend on it
 * (m64), since its once bit would otherwise hide the session's only line.
 */
static bool ratelimit_drops;
static unsigned int ratelimit_dropped;
#define pr_warn_ratelimited(...) (ratelimit_drops ? (void)ratelimit_dropped++ : test_warn(__VA_ARGS__))
#define pr_warn test_warn
#define test_and_set_bit(n, p) ((__atomic_fetch_or((p), 1UL << (n), __ATOMIC_SEQ_CST) >> (n)) & 1UL)
static void seq_puts(struct seq_file *m, const char *s)
{
	size_t n = strlen(s);

	CHECK(m->len + n < sizeof(m->buf));
	memcpy(m->buf + m->len, s, n + 1);
	m->len += n;
}
static __attribute__((format(printf, 2, 3))) void seq_printf(struct seq_file *m, const char *f, ...)
{
	va_list args;
	int n;

	va_start(args, f);
	n = vsnprintf(m->buf + m->len, sizeof(m->buf) - m->len, f, args);
	va_end(args);
	CHECK(n >= 0 && m->len + n < sizeof(m->buf));
	m->len += n;
}
#define SB_POSIXACL (1 << 16)
#define WQ_MEM_RECLAIM 8
#define THIS_MODULE ((struct module *)NULL)
#define WRITE_ONCE(x, v) ((x) = (v))
#define READ_ONCE(x) (x)
#define ERR_PTR(e) ((void *)(intptr_t)(e))
#define PTR_ERR(p) ((long)(intptr_t)(p))
#define IS_ERR(p) ((uintptr_t)(p) >= (uintptr_t)-4095)
#define INIT_LIST_HEAD(h) do { (h)->next = (h); (h)->prev = (h); } while (0)
#define spin_lock_init(l) (*(l) = 0)
#define spin_lock_irqsave(l, f) do { (f) = 0; CHECK(!*(l)); *(l) = 1; } while (0)
#define spin_unlock_irqrestore(l, f) do { (void)(f); CHECK(*(l)); *(l) = 0; } while (0)
#define atomic_inc(a) (++*(a))
#define atomic_dec(a) (--*(a))
#define mutex_init(m) ((m)->held = 0)
static inline void mutex_lock(struct mutex *m) { CHECK(!m->held); m->held = 1; }
static inline void mutex_unlock(struct mutex *m) { CHECK(m->held); m->held = 0; }
static inline void list_add_tail(struct list_head *p, struct list_head *h)
{
	p->prev = h->prev; p->next = h; h->prev->next = p; h->prev = p;
}
static inline void list_del_init(struct list_head *p)
{
	p->prev->next = p->next; p->next->prev = p->prev; INIT_LIST_HEAD(p);
}
static inline bool uuid_equal(const uuid_t *a, const uuid_t *b) { return !memcmp(a, b, sizeof(*a)); }
static inline bool uuid_is_null(const uuid_t *a) { static const uuid_t zero; return uuid_equal(a, &zero); }

#include "enrollment_struct.inc"

struct hfs_session;
struct hfs_provider {
	struct module *owner;
	u32 features;
	int (*enroll)(const char *device, struct hfs_enrollment *enrollment);
	void (*retain)(struct hfs_session *session);
};
/* Only the fields the mount and the controlled pre-send check touch. */
struct hfs_session {
	const struct hfs_provider *provider;
	u16 bridge_minor;
	struct hfs_enrollment enrollment;
	struct mutex lock, writer, retirement;
	spinlock_t processes_lock;
	struct list_head processes, requests, uncertain, descriptions, forgets;
	struct list_head object_numbers, view_tickets, view_families, global;
	u64 next_claim, next_cell, next_inode, next_close, next_release;
	u32 nr_requests;
	struct workqueue_struct *control;
	struct p9_client *client;
	void *control_completion, *retirement_completion;
	bool closing, blocked, retained, terminal_pending;
	unsigned long block_reported;
};
struct hfs_controlled_mount { struct super_block *super; bool stopping; u32 cookies; };
/* Only the hfs_view fields hfs_controlled_stop reads (m64). */
typedef int wait_queue_head_t;
enum hfs_view_phase { HFS_VIEW_UNBOUND, HFS_VIEW_QUIESCENT };
struct hfs_view {
	spinlock_t lock;
	wait_queue_head_t wait;
	u32 active, nr_dirty, nr_cached, nr_observations, nr_mappings;
	bool quiescent;
	enum hfs_view_phase phase;
};
#define wake_up_all(w) ((void)(w))

/* Per-case fixture state; each case is a fresh forked child. */
enum echo { ECHO_SENT, ECHO_MAPPED, ECHO_TERMINAL };
static struct {
	u32 features;
	enum echo echo;
	unsigned int msize; /* negotiated msize the substituted client reports */
	bool enrolled, bound, created, mounted, retained;
	unsigned int hellos;
	char sent[256];
	const char *client_options; /* what p9_client_create received */
} fixture;

static struct mutex hfs_provider_lock;
static const struct hfs_provider *hfs_provider;
static atomic_t hfs_active_sessions;
static bool hfs_trace_registered;
static struct tracepoint exit_tracepoint, *hfs_exit_tracepoint;
static struct list_head hfs_sessions = { &hfs_sessions, &hfs_sessions };
static spinlock_t hfs_sessions_lock;
static struct mutex hfs_controlled_lock;
static struct hfs_controlled_mount controlled_mount;
static struct user_namespace init_user_ns;
static struct workqueue_struct workqueue;
static struct p9_trans_module virtio = { "virtio", NULL }; /* trans_virtio has no show_options */
static struct p9_client client = { p9_proto_hfs14, 65536, &virtio };
static struct super_block super;
static struct dentry root = { &super };
static struct file_system_type fs_type;

static struct user_namespace *current_user_ns(void) { return &init_user_ns; }
static bool try_module_get(struct module *owner) { (void)owner; return true; }
static void module_put(struct module *owner) { (void)owner; }
#define __module_get(owner) ((void)(owner))
static struct workqueue_struct *alloc_workqueue(const char *name, int flags, int max)
{
	return &workqueue;
}
static void hfs_process_exit(void *data, void *task) { }
static void check_trace_callback_type_sched_process_exit(void (*fn)(void *, void *)) { }
static void hfs_find_exit_tracepoint(struct tracepoint *tp, void *priv) { }
static void for_each_kernel_tracepoint(void (*fn)(struct tracepoint *, void *), void *priv)
{
	*(struct tracepoint **)priv = &exit_tracepoint;
}
static int tracepoint_probe_register(struct tracepoint *tp, void (*fn)(void *, void *), void *data)
{
	return 0;
}
static int hfs_fill_super(struct super_block *sb, void *data, int silent) { abort(); }
/* fill_super's only effect show_options needs: sb->s_fs_info = session. */
static struct dentry *mount_nodev(struct file_system_type *type, int flags, void *data,
				  int (*fill)(struct super_block *, void *, int))
{
	fixture.mounted = true;
	super.s_fs_info = data;
	return &root;
}
static int hfs_session_free(struct hfs_session *session) { free(session); return 0; }
/* The real parse_opts/p9_client_version are not compiled here. For the zq3
 * options they set trans virtio, exact version hfs1.4 and the requested msize;
 * the client reports exactly that (msize from the case, see DEFAULT_MSIZE).
 */
static struct p9_client *p9_client_create(const char *device, void *options)
{
	fixture.created = true;
	fixture.client_options = options;
	client.msize = fixture.msize;
	return &client;
}
static int hfs_controlled_bind_transport(struct hfs_session *session)
{
	fixture.bound = true;
	return 0;
}
static void fixture_retain(struct hfs_session *session)
{
	fixture.retained = true;
	free(session);
}
static int fixture_enroll(const char *device, struct hfs_enrollment *enrollment);
static const struct hfs_provider controlled_provider = {
	.features = HFS_MAPPED_FEATURES, .enroll = fixture_enroll, .retain = fixture_retain,
};

/* Controlled registry lookups; the pre-send HELLO comparison itself is real.
 * A successful enroll claims the mount, so a later failure is "consumed".
 */
static bool hfs_controlled_owns(struct hfs_session *session)
{
	return session && session->provider == &controlled_provider;
}
static bool hfs_controlled_consumed(struct hfs_session *session) { return fixture.enrolled; }
static struct hfs_controlled_mount *hfs_controlled_mount(struct hfs_session *session)
{
	return &controlled_mount;
}
static int hfs_controlled_live(struct hfs_controlled_mount *mount, bool super)
{
	CHECK(mount == &controlled_mount);
	return 0;
}
static int hfs_controlled_cleanup_live(struct hfs_controlled_mount *mount) { return -ESTALE; }
static bool hfs_controlled_cleanup_command(struct hfs_session *session, const char *operation,
					   const struct hfs_buf *payload)
{
	return false;
}
#include "before_send.inc"

/* Native exchange: real controlled pre-send check, then the runtime's echo
 * {"selection": <sent HELLO payload>, "status": "ACCEPTED"}. ECHO_MAPPED and
 * ECHO_TERMINAL replace the echo to prove the selection check is exact.
 */
static int hfs_control(struct hfs_session *session, const char *operation,
		       const struct hfs_buf *payload, bool reserved, struct hfs_json **out)
{
	struct hfs_buf reply;
	const char *selection;
	int err;

	*out = NULL;
	CHECK(!strcmp(operation, "HELLO") && fixture.enrolled && fixture.bound);
	err = hfs_controlled_before_send(session, operation, payload, reserved);
	if (err)
		return err;
	fixture.hellos++;
	CHECK(payload->len < sizeof(fixture.sent));
	memcpy(fixture.sent, payload->data, payload->len);
	fixture.sent[payload->len] = 0;
	selection = fixture.echo == ECHO_SENT ? fixture.sent :
		    fixture.echo == ECHO_MAPPED ? hfs_protocol_text(4) :
		    hfs_protocol_selected_text(4, HFS_TERMINAL_FEATURES);
	CHECK(!hfs_buf_init(&reply, 512));
	hfs_buf_literal(&reply, "{\"payload\":{\"selection\":");
	hfs_buf_literal(&reply, selection);
	hfs_buf_literal(&reply, ",\"status\":\"ACCEPTED\"}}");
	CHECK(!reply.error);
	err = hfs_json_parse(reply.data, reply.len, out);
	hfs_buf_free(&reply);
	return err;
}

#include "validate.inc"

static void uuid_fill(uuid_t *id, u8 seed) { memset(id->b, seed, sizeof(id->b)); }
static void uuid_text(struct hfs_buf *buf, u8 seed)
{
	hfs_buf_printf(buf, "\"%02x%02x%02x%02x-%02x%02x-%02x%02x-%02x%02x-%02x%02x%02x%02x%02x%02x\"",
		       seed, seed, seed, seed, seed, seed, seed, seed,
		       seed, seed, seed, seed, seed, seed, seed, seed);
}

/* A complete valid lab-shaped context whose session equals the enrollment. */
static void fixture_context(struct hfs_enrollment *enrollment)
{
	static const char *const keys[] = {
		"authority_hash", "cluster", "configuration", "export", "exporter_incarnation",
		"fuse_session", "generation", "guest_boot", "guest_mount", "origin_incarnation",
		"origin_journal", "origin_node", "policy_hash", "registry_hash", "session", "vm",
		"volume",
	};
	struct hfs_buf buf;
	u32 i;

	CHECK(!hfs_buf_init(&buf, HFS_CONTEXT_MAX));
	for (i = 0; i < ARRAY_SIZE(keys); i++) {
		hfs_buf_printf(&buf, "%s\"%s\":", i ? "," : "{", keys[i]);
		if (strstr(keys[i], "_hash"))
			hfs_buf_printf(&buf, "\"%064x\"", 0x5a + i);
		else
			uuid_text(&buf, !strcmp(keys[i], "session") ? 0x10 : 0x20 + i);
	}
	hfs_buf_literal(&buf, "}");
	CHECK(!buf.error && buf.len < HFS_CONTEXT_MAX);
	memcpy(enrollment->context, buf.data, buf.len);
	enrollment->context_len = buf.len;
	hfs_buf_free(&buf);
}

/* Signed controlled enrollment substitute: it supplies the case's features
 * (the lab domain enrolls 32767) only when the mount calls provider->enroll.
 */
static int fixture_enroll(const char *device, struct hfs_enrollment *enrollment)
{
	CHECK(!fixture.enrolled && !strcmp(device, "hfs-test"));
	CHECK(!enrollment->protocol_features);
	enrollment->view_scope = HFS_SCOPE_CONTROLLED_LOCAL;
	enrollment->protocol_features = fixture.features;
	fixture_context(enrollment);
	uuid_fill(&enrollment->session, 0x10);
	uuid_fill(&enrollment->realm, 0x11);
	uuid_fill(&enrollment->anchor, 0x12);
	uuid_fill(&enrollment->nonce, 0x13);
	uuid_fill(&enrollment->view_control_nonce, 0x14);
	uuid_fill(&enrollment->acquisition_anchor, 0x15);
	uuid_fill(&enrollment->acquisition_nonce, 0x16);
	uuid_fill(&enrollment->control_anchor, 0x17);
	memcpy(enrollment->subject, "lab", 3);
	enrollment->subject_len = 3;
	fixture.enrolled = true;
	return 0;
}

#include "mount.inc"
#include "show_options.inc"

/* hfs_controlled_stop's view: one quiescent unbound view, so a stop with no
 * cookies succeeds and only its blocked fence is observed.
 */
static struct hfs_view stop_view = { .quiescent = true, .phase = HFS_VIEW_UNBOUND };
static struct hfs_view *hfs_view_get(struct hfs_session *session) { return &stop_view; }
static void hfs_view_put(struct hfs_view *view) { CHECK(view == &stop_view); }
#include "stop.inc"
#include "bounds.inc"
#include "bounds_fixtures.inc"

/* The zq3 mount (tools/lab/retained9p/bootstrap.py MOUNT_OPTIONS) and the
 * super options hfs_show_options must append to the VFS's "rw" for it.
 */
#define ZQ3_OPTIONS "trans=virtio,version=9P2000.L.hfs1.4,cache=mmap,msize=65536"
#define ZQ3_SHOWN ",cache=mmap,msize=65536,trans=virtio,version=9P2000.L.hfs1.4"
#define HFS14 "trans=virtio,version=9P2000.L.hfs1.4,"

struct mount_case {
	const char *name;
	const char *options;
	unsigned int msize; /* what the substituted client negotiates */
	u32 features;
	enum echo echo;
	int error;          /* expected mount result: 0 or a negative errno */
	bool enroll;        /* false: refused by the cache= check before enroll */
	bool hello;         /* whether a HELLO must reach the native exchange */
	const char *shown;  /* exact hfs_show_options output after a mount */
};

static const struct mount_case cases[] = {
	{ "control-16383", ZQ3_OPTIONS, 65536, HFS_MAPPED_FEATURES, ECHO_SENT, 0, true, true,
	  ZQ3_SHOWN },
	{ "terminal-32767", ZQ3_OPTIONS, 65536, HFS_TERMINAL_FEATURES, ECHO_SENT, 0, true, true,
	  ZQ3_SHOWN },
	/* msize equal to DEFAULT_MSIZE is still printed for hfs. */
	{ "terminal-32767-default-msize", HFS14 "cache=mmap,msize=131072", 131072,
	  HFS_TERMINAL_FEATURES, ECHO_SENT, 0, true, true,
	  ",cache=mmap,msize=131072,trans=virtio,version=9P2000.L.hfs1.4" },
	{ "terminal-32767-default-echo", ZQ3_OPTIONS, 65536, HFS_TERMINAL_FEATURES, ECHO_MAPPED,
	  -EPROTO, true, true, NULL },
	{ "control-16383-terminal-echo", ZQ3_OPTIONS, 65536, HFS_MAPPED_FEATURES, ECHO_TERMINAL,
	  -EPROTO, true, true, NULL },
	{ "unselected-features", ZQ3_OPTIONS, 65536, 12345, ECHO_SENT, -EPROTO, true, false, NULL },
	/* g64: anything but exactly one cache=mmap is refused before enroll. */
	{ "cache-missing", HFS14 "msize=65536", 65536, HFS_TERMINAL_FEATURES, ECHO_SENT,
	  -EINVAL, false, false, NULL },
	{ "no-options", NULL, 65536, HFS_TERMINAL_FEATURES, ECHO_SENT, -EINVAL, false, false, NULL },
	{ "cache-none", HFS14 "cache=none,msize=65536", 65536, HFS_TERMINAL_FEATURES, ECHO_SENT,
	  -EINVAL, false, false, NULL },
	{ "cache-loose", HFS14 "cache=loose,msize=65536", 65536, HFS_TERMINAL_FEATURES,
	  ECHO_SENT, -EINVAL, false, false, NULL },
	{ "cache-fscache", HFS14 "cache=fscache,msize=65536", 65536, HFS_TERMINAL_FEATURES,
	  ECHO_SENT, -EINVAL, false, false, NULL },
	{ "cache-mmap-prefix", HFS14 "cache=mmapx,msize=65536", 65536, HFS_TERMINAL_FEATURES,
	  ECHO_SENT, -EINVAL, false, false, NULL },
	{ "cache-mmap-repeated", "cache=mmap," ZQ3_OPTIONS, 65536, HFS_TERMINAL_FEATURES,
	  ECHO_SENT, -EINVAL, false, false, NULL },
	{ "legacy-loose", ZQ3_OPTIONS ",loose", 65536, HFS_TERMINAL_FEATURES, ECHO_SENT,
	  -EINVAL, false, false, NULL },
	{ "legacy-fscache", ZQ3_OPTIONS ",fscache", 65536, HFS_TERMINAL_FEATURES, ECHO_SENT,
	  -EINVAL, false, false, NULL },
	{ "legacy-mmap-alias", HFS14 "mmap,msize=65536", 65536, HFS_TERMINAL_FEATURES,
	  ECHO_SENT, -EINVAL, false, false, NULL },
	{ "cachetag", ZQ3_OPTIONS ",cachetag=zq3", 65536, HFS_TERMINAL_FEATURES, ECHO_SENT,
	  -EINVAL, false, false, NULL },
};

/* The guest.py check_tuple / pod_fixture.py MOUNT_OPTIONS set must be a
 * subset of the super options "rw" + show_options for the zq3 mount.
 */
static void check_guest_tuple(const char *shown)
{
	static const char *const required[] = {
		"rw", "cache=mmap", "version=9P2000.L.hfs1.4", "msize=65536",
	};
	char line[600], padded[620];
	unsigned int i;

	snprintf(line, sizeof(line), "rw%s", shown);
	snprintf(padded, sizeof(padded), ",%s,", line);
	for (i = 0; i < ARRAY_SIZE(required); i++) {
		char token[64];

		snprintf(token, sizeof(token), ",%s,", required[i]);
		CHECK(strstr(padded, token));
	}
}

/* The mounted superblock's mountinfo super options (after "rw"). */
static void check_shown(const struct mount_case *c)
{
	struct seq_file m = { .len = 0 };

	m.buf[0] = 0;
	CHECK(!hfs_show_options(&m, &root));
	if (strcmp(m.buf, c->shown)) {
		fprintf(stderr, "show_options \"%s\" != \"%s\"\n", m.buf, c->shown);
		_exit(1);
	}
	printf("%s: rw%s\n", c->name, m.buf);
	if (c->msize == 65536)
		check_guest_tuple(m.buf);
}

/* Child body: one real mount attempt and its exact observable outcome. */
static void run_case(const struct mount_case *c)
{
	struct dentry *result;
	char expected[32];

	fixture.features = c->features;
	fixture.echo = c->echo;
	fixture.msize = c->msize;
	hfs_provider = &controlled_provider;
	result = v9fs_hfs_mount(&fs_type, 0, "hfs-test", (void *)c->options);
	CHECK(fixture.enrolled == c->enroll && !hfs_provider_lock.held);
	CHECK(fixture.hellos == (c->hello ? 1U : 0U));
	CHECK(fixture.created == c->hello && fixture.bound == c->hello);
	if (c->hello) {
		/* The HELLO is the enrolled selection, not the kzalloc'd default. */
		CHECK(!strcmp(fixture.sent, hfs_protocol_selected_text(4, c->features)));
		snprintf(expected, sizeof(expected), "\"features\":%u,", c->features);
		CHECK(strstr(fixture.sent, expected));
		/* The p9 client still receives the unmodified option string. */
		CHECK(fixture.client_options == c->options);
	}
	if (!c->enroll) {
		/* Refused before any session: nothing created, consumed or retained. */
		CHECK(IS_ERR(result) && PTR_ERR(result) == c->error);
		CHECK(!fixture.mounted && !fixture.retained && !hfs_active_sessions);
		return;
	}
	if (!c->error) {
		CHECK(result == &root && fixture.mounted && !fixture.retained);
		check_shown(c);
		return;
	}
	CHECK(IS_ERR(result) && PTR_ERR(result) == c->error);
	/* The claimed controlled identity stays owned after the failure. */
	CHECK(!fixture.mounted && fixture.retained);
}

/* Stock 9p keeps eliding the default msize; only hfs always prints it. */
static void run_stock_case(void)
{
	struct p9_client stock = { p9_proto_2000L, DEFAULT_MSIZE, &virtio };
	struct seq_file m = { .len = 0 };

	m.buf[0] = 0;
	CHECK(!p9_show_client_options(&m, &stock));
	CHECK(!strcmp(m.buf, ",trans=virtio"));
}

/* hfs_session_block logs the first block of each session once, with its
 * reason, operation and errno (zq3-r7 R7-2 had none), then stays silent.
 */
static void run_block_case(void)
{
	struct hfs_session *first = calloc(1, sizeof(*first));
	struct hfs_session *second = calloc(1, sizeof(*second));

	CHECK(first && second);
	hfs_session_block(first, "QUERY timeout", "GETATTR", -ETIMEDOUT);
	CHECK(first->blocked && warnings == 1);
	CHECK(strstr(last_warning, "blocked by QUERY timeout on GETATTR (-110)"));
	hfs_session_block(first, "process-exit failure", "PROCESS_EXIT", -EHOSTDOWN);
	CHECK(first->blocked && warnings == 1);
	hfs_session_block(second, "process-exit failure", "PROCESS_EXIT", -EHOSTDOWN);
	CHECK(second->blocked && warnings == 2 && strstr(last_warning, "process-exit failure"));
	hfs_session_block(second, "uncertain claim result", NULL, -EPROTO);
	CHECK(warnings == 2);
	free(first);
	free(second);
}

/* m64: the once-per-session line survives a ratelimit burst. The pre-m64
 * pr_warn_ratelimited dropped it here and the once bit then hid it for good.
 */
static void run_block_ratelimit_case(void)
{
	struct hfs_session *session = calloc(1, sizeof(*session));

	CHECK(session);
	ratelimit_drops = true;
	hfs_session_block(session, "QUERY timeout", "GETATTR", -ETIMEDOUT);
	CHECK(session->blocked && warnings == 1 && !ratelimit_dropped);
	CHECK(strstr(last_warning, "blocked by QUERY timeout on GETATTR (-110)"));
	free(session);
}

/* m64: a formerly silent site. The controlled stop fence logs once on a
 * session that never faulted; after an earlier fault the first reason stays
 * the only line, and the stop still fences the session.
 */
static void run_stop_block_case(void)
{
	struct hfs_session *clean = calloc(1, sizeof(*clean));
	struct hfs_session *faulted = calloc(1, sizeof(*faulted));

	CHECK(clean && faulted);
	CHECK(!hfs_controlled_stop(clean));
	CHECK(clean->blocked && controlled_mount.stopping && warnings == 1);
	CHECK(strstr(last_warning, "blocked by controlled stop on STOP (0)"));
	hfs_session_block(faulted, "uncertain view ticket", "VIEW_QUERY", -EPROTO);
	CHECK(warnings == 2);
	CHECK(!hfs_controlled_stop(faulted));
	CHECK(faulted->blocked && warnings == 2);
	CHECK(strstr(last_warning, "blocked by uncertain view ticket on VIEW_QUERY (-71)"));
	free(clean);
	free(faulted);
}

/* hfs_go_bounds on a Bounds JSON object for an enrollment of `domains`,
 * parsed as production parses the signed enrollment (original Go key order).
 */
static int bounds_check(const char *text, u32 domains)
{
	struct hfs_json *json;
	int err;

	CHECK(!hfs_json_parse_go_enrollment(text, strlen(text), &json));
	err = hfs_go_bounds(json, 0, domains);
	hfs_json_free(json);
	return err;
}

/* The same Bounds with only Sources replaced. */
static void with_sources(char *out, size_t size, const char *bounds, u32 sources)
{
	const char *field = strstr(bounds, "\"Sources\":");
	const char *rest;

	CHECK(field);
	rest = field + strlen("\"Sources\":");
	while (*rest >= '0' && *rest <= '9')
		rest++;
	CHECK(snprintf(out, size, "%.*s\"Sources\":%u%s", (int)(field - bounds), bounds,
		       sources, rest) < (int)size);
}

/* viewcontrol.MinSources, from the Go constants run.py read. */
static u32 go_min_sources(u32 domains)
{
	return GO_BASELINESOURCES + 2 * domains + GO_ADMITTEDSOURCES + GO_SOURCERESERVE;
}

/* The kernel accepts Sources exactly in [MinSources(domains), MaxSources]. */
static void check_sources(const char *name, const char *bounds, u32 domains, u32 sources)
{
	bool accept = sources >= go_min_sources(domains) && sources <= GO_MAXSOURCES;
	char text[512];
	int err;

	with_sources(text, sizeof(text), bounds, sources);
	err = bounds_check(text, domains);
	printf("bounds %s domains=%u Sources=%u: %s\n", name, domains, sources,
	       err ? "refused" : "accepted");
	if (accept ? err != 0 : err != -EPROTO) {
		fprintf(stderr, "bounds %s domains=%u Sources=%u: got %d\n",
			name, domains, sources, err);
		_exit(1);
	}
}

/* m64: hfs_go_bounds mirrors viewcontrol.CheckSourceFloor. The real zq3 spec
 * Views and the guest-artifact enrollments are accepted at their own domain
 * counts; the floor is accepted and floor - 1 refused for 2 and 3 domains
 * (and 1, 62, 63), and MaxSources + 1 is refused.
 */
static void run_bounds_case(void)
{
	static const u32 domain_counts[] = { 1, 2, 3, 62, 63 };
	const char *artifacts = NULL;
	unsigned int i;

	CHECK(go_min_sources(2) == 8 && go_min_sources(3) == 10);
	for (i = 0; i < ARRAY_SIZE(real_bounds); i++) {
		CHECK(!bounds_check(real_bounds[i].bounds, real_bounds[i].domains));
		printf("bounds %s domains=%u: accepted as signed\n",
		       real_bounds[i].name, real_bounds[i].domains);
		if (!strcmp(real_bounds[i].name, "zq3-spec")) {
			CHECK(real_bounds[i].domains == 2);
			check_sources("zq3-spec", real_bounds[i].bounds, 2, go_min_sources(2));
			check_sources("zq3-spec", real_bounds[i].bounds, 2, go_min_sources(2) - 1);
		} else {
			CHECK(real_bounds[i].domains == 3);
			artifacts = real_bounds[i].bounds;
		}
	}
	CHECK(artifacts);
	for (i = 0; i < ARRAY_SIZE(domain_counts); i++) {
		u32 floor = go_min_sources(domain_counts[i]);

		check_sources("guest-artifacts", artifacts, domain_counts[i], floor - 1);
		check_sources("guest-artifacts", artifacts, domain_counts[i], floor);
		check_sources("guest-artifacts", artifacts, domain_counts[i], GO_MAXSOURCES);
		check_sources("guest-artifacts", artifacts, domain_counts[i], GO_MAXSOURCES + 1);
	}
}

/* Fork one child per case so a crash is reported without hiding the rest. */
static bool run_child(const char *name, const struct mount_case *c, void (*body)(void))
{
	int status;
	pid_t child;

	fflush(NULL);
	child = fork();
	CHECK(child >= 0);
	if (!child) {
		if (c)
			run_case(c);
		else
			body();
		fflush(NULL);
		_exit(0);
	}
	CHECK(waitpid(child, &status, 0) == child);
	if (WIFEXITED(status) && !WEXITSTATUS(status)) {
		printf("PASS %s\n", name);
		return true;
	}
	printf("FAIL %s (status 0x%x)\n", name, status);
	return false;
}

int main(void)
{
	unsigned int i, failures = 0;

	for (i = 0; i < ARRAY_SIZE(cases); i++)
		failures += !run_child(cases[i].name, &cases[i], NULL);
	failures += !run_child("stock-default-msize", NULL, run_stock_case);
	failures += !run_child("session-block-once", NULL, run_block_case);
	failures += !run_child("session-block-ratelimit", NULL, run_block_ratelimit_case);
	failures += !run_child("controlled-stop-block", NULL, run_stop_block_case);
	failures += !run_child("enrollment-sources-floor", NULL, run_bounds_case);
	printf("mount_hello cases=%zu failures=%u\n", ARRAY_SIZE(cases) + 5, failures);
	return failures ? 1 : 0;
}
