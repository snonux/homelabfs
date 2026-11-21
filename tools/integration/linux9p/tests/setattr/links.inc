/* vf4 review 1: the actual hfs_lookup, hfs_relookup, hfs_link and
 * hfs_create_exists on a dentry an earlier hfs_lookup already hashed.
 *
 * d_splice_alias starts with BUG_ON(!d_unhashed(dentry)) (fs/dcache.c:3150);
 * the substitute fails the test there and otherwise hashes the dentry, as
 * __d_add does for both an in-lookup dentry (hfs_atomic_open) and a
 * d_alloc'ed one (filename_create's lookup_one_qstr_excl before link(2)).
 * hfs_lookup_once (the LOOKUP exchange) and hfs_reissue are substituted.
 */
#define jiffies 0UL
#define msecs_to_jiffies(ms) ((unsigned long)(ms))
#define HFS_VIEW_ENTER_WAIT_MS 30000
#define ERR_PTR(err) ((void *)(long)(err))
#define PTR_ERR(ptr) ((long)(ptr))
#define IS_ERR(ptr) ((unsigned long)(ptr) >= (unsigned long)-4095)
#define IS_ERR_OR_NULL(ptr) (!(ptr) || IS_ERR(ptr))

static struct {
	int err;
	struct inode *inode;
	bool cap;
	unsigned int calls;
} lookup_reply;

static int hfs_lookup_once(struct inode *dir, struct dentry *dentry,
			   struct hfs_cap **capp, struct inode **inodep)
{
	lookup_reply.calls++;
	if (lookup_reply.err)
		return lookup_reply.err;
	*capp = lookup_reply.cap ? test_cap("{\"p\":5}") : NULL;
	*inodep = lookup_reply.inode;
	return 0;
}
static bool hfs_reissue(struct hfs_session *session, int *err,
			unsigned long deadline, const char *opcode)
{
	return false;
}
static struct dentry *d_splice_alias(struct inode *inode, struct dentry *dentry)
{
	CHECK(!dentry->hashed); /* BUG_ON(!d_unhashed(dentry)) */
	dentry->d_inode = inode;
	dentry->hashed = true;
	return NULL;
}
static void d_drop(struct dentry *dentry) { dentry->hashed = false; }
static void dput(struct dentry *dentry) { CHECK(dentry); }
static int finish_no_open(struct file *file, struct dentry *dentry)
{
	if (IS_ERR(dentry))
		return PTR_ERR(dentry);
	file->f_path.dentry = dentry;
	return 0;
}
#include "lookup.inc"

/* A directory with its grant, the link source and a fresh target name. */
struct names {
	struct fixture f; /* the existing file: f.dentry is the link source */
	struct inode dir;
	struct dentry parent, target;
};

static void names_init(struct names *n)
{
	static const unsigned char name[] = "group";

	fixture_init(&n->f, S_IFREG | 0640);
	memset(&n->dir, 0, sizeof(n->dir));
	memset(&n->parent, 0, sizeof(n->parent));
	memset(&n->target, 0, sizeof(n->target));
	n->dir.i_mode = S_IFDIR | 0755;
	n->parent.d_inode = &n->dir;
	n->parent.d_fsdata = test_cap("{\"P\":0}");
	n->target.d_name = (struct qstr){ name, sizeof(name) - 1 };
	n->target.d_parent = &n->parent;
	n->f.dentry.d_sb = n->target.d_sb = &n->dir;
	memset(&lookup_reply, 0, sizeof(lookup_reply));
	d_set_d_op_calls = 0;
}

static void names_free(struct names *n)
{
	hfs_cap_free(n->parent.d_fsdata);
	hfs_cap_free(n->target.d_fsdata);
	fixture_free(&n->f);
}

/* The negative lookup that precedes link(2) and the O_CREAT open. */
static void names_lookup_negative(struct names *n)
{
	CHECK(hfs_lookup(&n->dir, &n->target, 0) == NULL);
	CHECK(n->target.hashed && !n->target.d_inode && d_set_d_op_calls == 1);
}

/* link(2) (T4 `namespace`, guest.py os.link): pre-review every successful
 * LINK hit the BUG_ON in its second hfs_lookup.
 */
static void test_link_relookup(void)
{
	struct names n;

	names_init(&n);
	names_lookup_negative(&n);
	lookup_reply.inode = &n.f.inode;
	lookup_reply.cap = true;
	CHECK(hfs_link(&n.f.dentry, &n.dir, &n.target) == 0);
	CHECK(!strcmp(seen.opcode, "LINK") && seen.begin == 1 && seen.end == 1);
	CHECK(!strcmp(seen.args, "{\"name\":\"67726f7570\",\"object\":{\"o\":1},"
		      "\"parent\":{\"P\":0}}"));
	CHECK(n.target.hashed && n.target.d_inode == &n.f.inode);
	CHECK(n.target.d_fsdata && d_set_d_op_calls == 1 && lookup_reply.calls == 2);
	names_free(&n);
	/* The name resolving to another object is -ESTALE, still no BUG. */
	names_init(&n);
	names_lookup_negative(&n);
	lookup_reply.inode = &n.dir;
	CHECK(hfs_link(&n.f.dentry, &n.dir, &n.target) == -ESTALE);
	names_free(&n);
}

/* hfs_atomic_open's CREATE lost to EEXIST without O_EXCL: the in-lookup
 * dentry its first hfs_lookup hashed is looked up again and handed to the
 * normal open (finish_no_open with no alias); a lookup error is returned.
 */
static void test_create_exists(void)
{
	struct names n;
	struct file file = { 0 };

	names_init(&n);
	names_lookup_negative(&n);
	lookup_reply.inode = &n.f.inode;
	lookup_reply.cap = true;
	file.f_path.dentry = (struct dentry *)&file; /* DENTRY_NOT_SET stand-in */
	CHECK(hfs_create_exists(&n.dir, &n.target, &file) == 0);
	CHECK(!file.f_path.dentry && n.target.hashed && n.target.d_inode == &n.f.inode);
	CHECK(d_set_d_op_calls == 1);
	names_free(&n);
	names_init(&n);
	names_lookup_negative(&n);
	lookup_reply.err = -EAGAIN;
	CHECK(hfs_create_exists(&n.dir, &n.target, &file) == -EAGAIN);
	CHECK(!n.target.hashed);
	names_free(&n);
}
