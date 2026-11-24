// SPDX-License-Identifier: GPL-2.0-only
/* Actual hfs_setattr and hfs_dentry_attach (vf4, zq3-r18 R18-1 and G2).
 *
 * The VFS callers are modelled from Linux 6.1: chown_common (fs/open.c)
 * adds ATTR_KILL_SUID|ATTR_KILL_PRIV and setattr_should_drop_sgid for every
 * non-directory; do_truncate (fs/open.c) and __remove_privs (fs/inode.c) add
 * setattr_should_drop_suidgid's kill bits (and ATTR_FORCE) for a caller
 * without CAP_FSETID; notify_change (fs/attr.c) drops ATTR_KILL_PRIV when no
 * security.capability xattr exists (hfs has no xattr handlers) and turns a
 * kill bit whose mode bit is set into ATTR_MODE. hfs_call records the exact
 * request instead of sending it.
 */
#include "hfs_protocol.h"
#include "attr.inc"
unsigned int hfs_test_alloc_call, hfs_test_alloc_fail;
size_t hfs_test_last_allocation_size;
#define HFS_CAP_MAX 8192U
#define HFS_IO_MAX 65536U
#define LOOKUP_RCU 0x0040
#define CHECK(x)                                                               \
	do {                                                                   \
		if (!(x)) {                                                    \
			fprintf(stderr, "%d: %s\n", __LINE__, #x);             \
			exit(1);                                               \
		}                                                              \
	} while (0)

typedef unsigned short umode_t;
typedef struct { unsigned int val; } kuid_t;
typedef struct { unsigned int val; } kgid_t;
struct timespec64 { long long tv_sec; long tv_nsec; };
struct user_namespace { int unused; };
static struct user_namespace init_user_ns;
static unsigned int from_kuid(struct user_namespace *ns, kuid_t uid) { return uid.val; }
static unsigned int from_kgid(struct user_namespace *ns, kgid_t gid) { return gid.val; }
#define spin_lock(x) ((void)(x))
#define spin_unlock(x) ((void)(x))
#include "cap.inc"

/* s64: the list, lock and flag primitives the described GETATTR/SETATTR walk
 * uses. The mutex fails the test on recursion; hfs_call checks it is free.
 */
struct list_head { struct list_head *next, *prev; };
static void INIT_LIST_HEAD(struct list_head *list) { list->next = list->prev = list; }
static void list_add_tail(struct list_head *entry, struct list_head *head)
{
	entry->prev = head->prev;
	entry->next = head;
	head->prev->next = entry;
	head->prev = entry;
}
static void list_del(struct list_head *entry)
{
	entry->prev->next = entry->next;
	entry->next->prev = entry->prev;
}
#define container_of(ptr, type, member) ((type *)((char *)(ptr) - offsetof(type, member)))
#define list_for_each_entry(pos, head, member)                                  \
	for (pos = container_of((head)->next, typeof(*pos), member);           \
	     &pos->member != (head);                                           \
	     pos = container_of(pos->member.next, typeof(*pos), member))
struct mutex { bool held; };
static void mutex_lock(struct mutex *lock) { CHECK(!lock->held); lock->held = true; }
static void mutex_unlock(struct mutex *lock) { CHECK(lock->held); lock->held = false; }
#define READ_ONCE(x) (x)
#define xchg(ptr, value) __atomic_exchange_n((ptr), (value), __ATOMIC_SEQ_CST)
static bool uuid_equal(const uuid_t *a, const uuid_t *b) { return !memcmp(a, b, sizeof(*a)); }
static unsigned int warnings;
#define pr_warn_ratelimited(...) ((void)warnings++)

struct hfs_session {
	struct mutex lock;
	struct list_head descriptions;
	struct { uuid_t session; } enrollment;
	u16 bridge_minor;
	bool fd_attr, transport_failed, blocked;
};
struct hfs_cred { int unused; };
struct hfs_inode { struct hfs_cap *object; };
struct inode {
	struct hfs_inode hfs;
	umode_t i_mode;
	unsigned int i_nlink;
	struct hfs_session session;
};
#define HFS_I(inode) (&(inode)->hfs)
#define HFS_S(inode) (&(inode)->session)
struct dentry;
struct dentry_operations {
	int (*d_revalidate)(struct dentry *, unsigned int);
	void (*d_release)(struct dentry *);
};
struct qstr { const unsigned char *name; u32 len; };
struct dentry {
	int d_lock;
	struct inode *d_inode;
	void *d_fsdata;
	const struct dentry_operations *d_op;
	struct qstr d_name;
	struct dentry *d_parent;
	void *d_sb;
	bool hashed; /* !d_unhashed(): set by d_splice_alias, cleared by d_drop */
};
static struct inode *d_inode(const struct dentry *dentry) { return dentry->d_inode; }
static unsigned int d_set_d_op_calls;
/* fs/dcache.c d_set_d_op WARNs (lines 1924/1925) on a dentry with ops. */
static void d_set_d_op(struct dentry *dentry, const struct dentry_operations *op)
{
	CHECK(!dentry->d_op);
	d_set_d_op_calls++;
	dentry->d_op = op;
}
/* A foreign d_op would WARN in the kernel; here it fails the test. */
#define WARN_ON_ONCE(condition) CHECK(!(condition))
#include "dentry.inc"

struct hfs_description { struct list_head list; struct hfs_cap *cap; };
struct path { struct dentry *dentry; };
struct file {
	void *private_data;
	struct path f_path;
};
struct iattr {
	unsigned int ia_valid;
	umode_t ia_mode;
	kuid_t ia_uid;
	kgid_t ia_gid;
	long long ia_size; /* loff_t */
	struct timespec64 ia_atime, ia_mtime, ia_ctime;
	struct file *ia_file;
};

/* Substitutes: what hfs_setattr asked of the VFS and the host. s64: every
 * CALL is logged (log[i], ops[i]) and may get a scripted result (errs[i]
 * for i < scripted, else call_err) and a scripted install result.
 */
#define MAX_CALLS 16
static struct {
	unsigned int prepare, begin, reads, end, install, calls, scripted;
	unsigned int prepared_valid;
	int call_err;
	bool not_owner; /* setattr_prepare's inode_owner_or_capable fails */
	bool owner_lost; /* s64: it fails from the second setattr_prepare on */
	char opcode[16];
	char args[4096];
	int errs[MAX_CALLS], install_errs[MAX_CALLS];
	char ops[MAX_CALLS][16];
	char log[MAX_CALLS][4096];
} seen;
static int setattr_prepare(struct user_namespace *ns, struct dentry *dentry,
			   struct iattr *attr)
{
	bool not_owner = seen.not_owner || (seen.owner_lost && seen.prepare);

	seen.prepare++;
	seen.prepared_valid = attr->ia_valid;
	/* fs/attr.c: ATTR_MODE needs inode_owner_or_capable unless ATTR_FORCE. */
	if (not_owner && (attr->ia_valid & ATTR_MODE) &&
	    !(attr->ia_valid & ATTR_FORCE))
		return -EPERM;
	return 0;
}
/* GETATTR (s64 fallback and probe) begins as a read, SETATTR as a mutation. */
static int hfs_begin(struct inode *inode, bool mutation, struct hfs_cred **cred,
		     void **cookie)
{
	static struct hfs_cred issuer;

	seen.begin++;
	seen.reads += !mutation;
	*cred = &issuer;
	*cookie = inode;
	return 0;
}
static void hfs_end(struct inode *inode, struct hfs_cred *cred, void *cookie)
{
	CHECK(cookie == inode);
	seen.end++;
}
static int hfs_call(struct hfs_session *session, const char *opcode,
		    const struct hfs_buf *args, const struct hfs_cred *issuer,
		    bool control, struct hfs_json **reply)
{
	static const char body[] = "{\"attributes\":{}}";
	unsigned int i = seen.calls++;

	/* s64: a listed cap is copied under session->lock, never sent under it. */
	CHECK(!session->lock.held);
	CHECK(!control && !args->error && args->len < sizeof(seen.args));
	CHECK(i < MAX_CALLS);
	snprintf(seen.opcode, sizeof(seen.opcode), "%s", opcode);
	memcpy(seen.args, args->data, args->len);
	seen.args[args->len] = 0;
	memcpy(seen.ops[i], seen.opcode, sizeof(seen.opcode));
	memcpy(seen.log[i], seen.args, args->len + 1);
	CHECK(!hfs_json_parse(body, sizeof(body) - 1, reply));
	return i < seen.scripted ? seen.errs[i] : seen.call_err;
}
static int hfs_attr_install(struct inode *inode, const struct hfs_json *json,
			    int token)
{
	CHECK(token > 0);
	seen.install++;
	return seen.calls && seen.calls <= MAX_CALLS ? seen.install_errs[seen.calls - 1] : 0;
}
#include "fdattr.inc"
#include "setattr.inc"
#include "model.inc"
#include "links.inc"
#include "cases.inc"
#include "fdattr_cases.inc"
