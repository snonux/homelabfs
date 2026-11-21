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

struct hfs_session { int unused; };
struct hfs_cred { int unused; };
struct hfs_inode { struct hfs_cap *object; };
struct inode { struct hfs_inode hfs; umode_t i_mode; struct hfs_session session; };
#define HFS_I(inode) (&(inode)->hfs)
#define HFS_S(inode) (&(inode)->session)
struct dentry;
struct dentry_operations {
	int (*d_revalidate)(struct dentry *, unsigned int);
	void (*d_release)(struct dentry *);
};
struct dentry {
	int d_lock;
	struct inode *d_inode;
	void *d_fsdata;
	const struct dentry_operations *d_op;
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
#include "dentry.inc"

struct hfs_description { struct hfs_cap *cap; };
struct file { void *private_data; };
struct iattr {
	unsigned int ia_valid;
	umode_t ia_mode;
	kuid_t ia_uid;
	kgid_t ia_gid;
	long long ia_size; /* loff_t */
	struct timespec64 ia_atime, ia_mtime, ia_ctime;
	struct file *ia_file;
};

/* Substitutes: what hfs_setattr asked of the VFS and the host. */
static struct {
	unsigned int prepare, begin, end, install;
	unsigned int prepared_valid;
	int prepare_err, call_err;
	char opcode[16];
	char args[4096];
} seen;
static int setattr_prepare(struct user_namespace *ns, struct dentry *dentry,
			   struct iattr *attr)
{
	seen.prepare++;
	seen.prepared_valid = attr->ia_valid;
	return seen.prepare_err;
}
static int hfs_begin(struct inode *inode, bool mutation, struct hfs_cred **cred,
		     void **cookie)
{
	static struct hfs_cred issuer;

	CHECK(mutation);
	seen.begin++;
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

	CHECK(!control && !args->error && args->len < sizeof(seen.args));
	snprintf(seen.opcode, sizeof(seen.opcode), "%s", opcode);
	memcpy(seen.args, args->data, args->len);
	seen.args[args->len] = 0;
	CHECK(!hfs_json_parse(body, sizeof(body) - 1, reply));
	return seen.call_err;
}
static int hfs_attr_install(struct inode *inode, const struct hfs_json *json,
			    int token)
{
	CHECK(token > 0);
	seen.install++;
	return 0;
}
#include "setattr.inc"
#include "model.inc"
#include "cases.inc"
