// SPDX-License-Identifier: GPL-2.0-only
/* Actual v9fs_hfs_mount ordering: enroll, validate, HELLO, selection check.
 *
 * mount.inc, validate.inc and before_send.inc are verbatim production
 * definitions. The VFS/module/tracepoint/p9 primitives, the controlled
 * registry lookups and the native exchange are explicit substitutions. The
 * exchange applies the real controlled pre-send HELLO check and then echoes
 * the sent payload as the runtime selection, exactly like
 * internal/guestbridge/service.go. Each case runs in a forked child so a crash
 * in one case is reported without hiding the others.
 */
#include "hfs_policy.h"
#include <assert.h>
#include <sys/wait.h>
#include <unistd.h>

unsigned int hfs_test_alloc_call, hfs_test_alloc_fail;
size_t hfs_test_last_allocation_size;
#define CHECK(e) do { if (!(e)) { fprintf(stderr, "%d: %s\n", __LINE__, #e); _exit(1); } } while (0)

/* Kernel/VFS primitives used by the mount. */
struct module { int unused; };
struct user_namespace { int unused; };
struct file_system_type { int unused; };
struct super_block { int unused; };
struct dentry { int unused; };
struct workqueue_struct { int unused; };
struct tracepoint { int unused; };
struct mutex { int held; };
typedef int spinlock_t;
typedef int atomic_t;
struct list_head { struct list_head *next, *prev; };
enum { p9_proto_hfs1 = 10, p9_proto_hfs11, p9_proto_hfs12, p9_proto_hfs13, p9_proto_hfs14 };
struct p9_client { int proto_version; };
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
};
struct hfs_controlled_mount { struct super_block *super; };

/* Per-case fixture state; each case is a fresh forked child. */
enum echo { ECHO_SENT, ECHO_MAPPED, ECHO_TERMINAL };
static struct {
	u32 features;
	enum echo echo;
	bool enrolled, bound, created, mounted, retained;
	unsigned int hellos;
	char sent[256];
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
static struct p9_client client = { p9_proto_hfs14 };
static struct dentry root;
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
static struct dentry *mount_nodev(struct file_system_type *type, int flags, void *data,
				  int (*fill)(struct super_block *, void *, int))
{
	fixture.mounted = true;
	return &root;
}
static int hfs_session_free(struct hfs_session *session) { free(session); return 0; }
static struct p9_client *p9_client_create(const char *device, void *options)
{
	fixture.created = true;
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

struct mount_case {
	const char *name;
	u32 features;
	enum echo echo;
	int error;   /* expected mount result: 0 or a negative errno */
	bool hello;  /* whether a HELLO must reach the native exchange */
};

static const struct mount_case cases[] = {
	{ "control-16383", HFS_MAPPED_FEATURES, ECHO_SENT, 0, true },
	{ "terminal-32767", HFS_TERMINAL_FEATURES, ECHO_SENT, 0, true },
	{ "terminal-32767-default-echo", HFS_TERMINAL_FEATURES, ECHO_MAPPED, -EPROTO, true },
	{ "control-16383-terminal-echo", HFS_MAPPED_FEATURES, ECHO_TERMINAL, -EPROTO, true },
	{ "unselected-features", 12345, ECHO_SENT, -EPROTO, false },
};

/* Child body: one real mount attempt and its exact observable outcome. */
static void run_case(const struct mount_case *c)
{
	struct dentry *result;
	char expected[32];

	fixture.features = c->features;
	fixture.echo = c->echo;
	hfs_provider = &controlled_provider;
	result = v9fs_hfs_mount(&fs_type, 0, "hfs-test", "version=9P2000.L.hfs1.4");
	CHECK(fixture.enrolled && !hfs_provider_lock.held);
	CHECK(fixture.hellos == (c->hello ? 1U : 0U));
	CHECK(fixture.created == c->hello && fixture.bound == c->hello);
	if (c->hello) {
		/* The HELLO is the enrolled selection, not the kzalloc'd default. */
		CHECK(!strcmp(fixture.sent, hfs_protocol_selected_text(4, c->features)));
		snprintf(expected, sizeof(expected), "\"features\":%u,", c->features);
		CHECK(strstr(fixture.sent, expected));
	}
	if (!c->error) {
		CHECK(result == &root && fixture.mounted && !fixture.retained);
		return;
	}
	CHECK(IS_ERR(result) && PTR_ERR(result) == c->error);
	/* The claimed controlled identity stays owned after the failure. */
	CHECK(!fixture.mounted && fixture.retained);
}

int main(void)
{
	unsigned int i, failures = 0;
	int status;
	pid_t child;

	for (i = 0; i < ARRAY_SIZE(cases); i++) {
		fflush(NULL);
		child = fork();
		CHECK(child >= 0);
		if (!child) {
			run_case(&cases[i]);
			fflush(NULL);
			_exit(0);
		}
		CHECK(waitpid(child, &status, 0) == child);
		if (WIFEXITED(status) && !WEXITSTATUS(status)) {
			printf("PASS %s\n", cases[i].name);
		} else {
			printf("FAIL %s (status 0x%x)\n", cases[i].name, status);
			failures++;
		}
	}
	printf("mount_hello cases=%zu failures=%u\n", ARRAY_SIZE(cases), failures);
	return failures ? 1 : 0;
}
