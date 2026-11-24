/* s64: fstat, fchmod, fchown and futimens through the open file description.
 *
 * The actual hfs_getattr_once and hfs_setattr with their s64 helpers
 * (fdattr.inc) run against the logging hfs_call. Each case checks every
 * CALL's exact bytes in order: the described forms put "description" between
 * "attributes" and "object" (canonical sorted keys), an hfs_fdattr-less
 * mount sends only the original path shapes, and the borrowed walk is
 * bounded and skips descriptions of other objects.
 */
#define CHMOD_ATTRS "\"attributes\":{" NO_TIMES ",\"gid\":null,\"mode\":384," \
	NO_MTIME ",\"uid\":null}"
#define OWN ",\"description\":{\"d\":2}"
#define OTHER ",\"description\":{\"d\":3}"
#define OBJ ",\"object\":{\"o\":1}}"
static const char chmod_path[] = "{" CHMOD_ATTRS OBJ;
static const char chmod_own[] = "{" CHMOD_ATTRS OWN OBJ;
static const char chmod_other[] = "{" CHMOD_ATTRS OTHER OBJ;
static const char getattr_path[] = "{\"object\":{\"o\":1}}";
static const char getattr_own[] = "{\"description\":{\"d\":2},\"object\":{\"o\":1}}";
static const char getattr_other[] = "{\"description\":{\"d\":3},\"object\":{\"o\":1}}";

struct logged { const char *op, *args; };

/* Exactly these CALLs in this order, each hfs_begin balanced. */
static void expect_log(unsigned int n, const struct logged *want)
{
	unsigned int i;

	if (seen.calls != n)
		fprintf(stderr, "calls %u, want %u\n", seen.calls, n);
	CHECK(seen.calls == n && seen.begin == seen.end);
	for (i = 0; i < n; i++) {
		if (strcmp(seen.log[i], want[i].args))
			fprintf(stderr, "call %u got  %s\n        want %s\n", i,
				seen.log[i], want[i].args);
		CHECK(!strcmp(seen.ops[i], want[i].op) && !strcmp(seen.log[i], want[i].args));
	}
}

static void script(const int *errs, unsigned int n)
{
	memcpy(seen.errs, errs, n * sizeof(*errs));
	seen.scripted = n;
}

/* fchmod(fd, 0600): chmod_common's request (no ATTR_FILE). */
static int vfs_fchmod(struct fixture *f)
{
	struct iattr attr = { .ia_valid = ATTR_MODE | ATTR_CTIME, .ia_mode = 0600 };

	return hfs_setattr(&init_user_ns, &f->dentry, &attr);
}

/* fstat(fd): vfs_getattr(&file->f_path) reaches ->getattr with the path. */
static int vfs_fstat(struct fixture *f)
{
	struct path path = { &f->dentry };

	return hfs_getattr_once(&path);
}

/* Another listed description; it goes before the fixture's own one, as an
 * earlier open by another process would. allocation != 0: another object.
 */
static void add_description(struct fixture *f, struct hfs_description *d,
			    const char *json, u8 token, u8 allocation)
{
	d->cap = test_cap(json);
	d->cap->token[0] = token;
	d->cap->allocation.b[0] = allocation;
	list_del(&f->description.list);
	list_add_tail(&d->list, &f->inode.session.descriptions);
	list_add_tail(&f->description.list, &f->inode.session.descriptions);
}

/* Without hfs_fdattr nothing changes: the path forms only, even for an
 * unlinked inode or ATTR_FILE, and a host Stale stays -ESTALE.
 */
static void test_fdattr_off(void)
{
	static const struct logged set[] = { { "SETATTR", chmod_path } };
	static const struct logged get[] = { { "GETATTR", getattr_path } };
	struct fixture f;
	struct iattr attr;

	fixture_init(&f, S_IFREG | 0644);
	f.inode.i_nlink = 0;
	seen.call_err = -ESTALE;
	CHECK(vfs_fchmod(&f) == -ESTALE);
	expect_log(1, set);
	seen.calls = 0;
	CHECK(vfs_fstat(&f) == -ESTALE);
	expect_log(1, get);
	seen.calls = 0;
	attr = (struct iattr){ .ia_valid = ATTR_MODE | ATTR_CTIME | ATTR_FILE,
			       .ia_mode = 0600, .ia_file = &f.file };
	CHECK(hfs_setattr(&init_user_ns, &f.dentry, &attr) == -ESTALE);
	expect_log(1, set);
	fixture_free(&f);
}

/* After a rename the path form is Stale; the fd's own description serves
 * fchmod, fchown, futimens(NULL) and fstat. A path form that works is
 * the only CALL.
 */
static void test_fdattr_rename(void)
{
	static const int stale_then_ok[] = { -ESTALE, 0 };
	static const struct logged set[] = { { "SETATTR", chmod_path }, { "SETATTR", chmod_own } };
	static const struct logged get[] = { { "GETATTR", getattr_path }, { "GETATTR", getattr_own } };
	static const struct logged chown[] = {
		{ "SETATTR", "{\"attributes\":{" NO_TIMES ",\"gid\":42002,\"mode\":null,"
		  NO_MTIME ",\"uid\":41002}" OBJ },
		{ "SETATTR", "{\"attributes\":{" NO_TIMES ",\"gid\":42002,\"mode\":null,"
		  NO_MTIME ",\"uid\":41002}" OWN OBJ } };
	static const struct logged touch[] = {
		{ "SETATTR", "{\"attributes\":{\"atime\":null,\"atime_now\":true,\"gid\":null,"
		  "\"mode\":null,\"mtime\":null,\"mtime_now\":true,\"uid\":null}" OBJ },
		{ "SETATTR", "{\"attributes\":{\"atime\":null,\"atime_now\":true,\"gid\":null,"
		  "\"mode\":null,\"mtime\":null,\"mtime_now\":true,\"uid\":null}" OWN OBJ } };
	struct fixture f;

	fixture_init(&f, S_IFREG | 0644);
	f.inode.session.fd_attr = true;
	CHECK(vfs_fchmod(&f) == 0);
	expect_log(1, set);
	seen.calls = seen.begin = seen.end = 0;
	script(stale_then_ok, 2);
	CHECK(vfs_fchmod(&f) == 0);
	expect_log(2, set);
	CHECK(!seen.reads && seen.prepare == 2);
	seen.calls = seen.begin = seen.end = seen.reads = 0;
	CHECK(vfs_fstat(&f) == 0);
	expect_log(2, get);
	CHECK(seen.reads == 2);
	seen.calls = 0;
	CHECK(vfs_chown(&f, 41002, 42002, true, false) == 0);
	expect_log(2, chown);
	seen.calls = 0;
	CHECK(vfs_utimes(&f, NULL) == 0);
	expect_log(2, touch);
	fixture_free(&f);
}

/* An unlinked inode (nlink 0) goes to a description at once. */
static void test_fdattr_unlinked(void)
{
	static const struct logged set[] = { { "SETATTR", chmod_own } };
	static const struct logged get[] = { { "GETATTR", getattr_own } };
	struct fixture f;

	fixture_init(&f, S_IFREG | 0644);
	f.inode.session.fd_attr = true;
	f.inode.i_nlink = 0;
	CHECK(vfs_fchmod(&f) == 0);
	expect_log(1, set);
	seen.calls = 0;
	CHECK(vfs_fstat(&f) == 0);
	expect_log(1, get);
	fixture_free(&f);
}

/* Another process's description of the same object, listed first, was
 * released (Permission) or its handle is gone (Stale): the walk moves on to
 * the fd's own one. A description of another object is never sent. A
 * SETATTR Permission is probed with a GETATTR through the same description.
 */
static void test_fdattr_walk(void)
{
	static const int set_errs[] = { -ESTALE, -EACCES, -EACCES, 0 };
	static const int get_errs[] = { -ESTALE, -ESTALE, 0 };
	static const struct logged set[] = {
		{ "SETATTR", chmod_path }, { "SETATTR", chmod_other },
		{ "GETATTR", getattr_other }, { "SETATTR", chmod_own } };
	static const struct logged get[] = {
		{ "GETATTR", getattr_path }, { "GETATTR", getattr_other },
		{ "GETATTR", getattr_own } };
	struct hfs_description other, foreign;
	struct fixture f;

	fixture_init(&f, S_IFREG | 0644);
	f.inode.session.fd_attr = true;
	add_description(&f, &foreign, "{\"d\":9}", 9, 1);
	add_description(&f, &other, "{\"d\":3}", 3, 0);
	script(set_errs, 4);
	CHECK(vfs_fchmod(&f) == 0);
	expect_log(4, set);
	CHECK(seen.prepare == 1);
	seen.calls = 0;
	script(get_errs, 3);
	CHECK(vfs_fstat(&f) == 0);
	expect_log(3, get);
	fixture_free(&f);
	hfs_cap_free(other.cap);
	hfs_cap_free(foreign.cap);
}

/* The host's owner rule refused a described SETATTR, and the probe answers
 * through the same description: the refusal is real. With fresh attributes
 * (the cached owner was stale) setattr_prepare gives -EPERM for a mode
 * change, as chmod(2) does; current times without write access stay the
 * host's -EACCES. A probe whose view target moved still answered.
 */
static void test_fdattr_owner_refusal(void)
{
	static const int refused[] = { -EACCES, 0 };
	static const struct logged set[] = { { "SETATTR", chmod_own }, { "GETATTR", getattr_own } };
	struct fixture f;

	fixture_init(&f, S_IFREG | 0644);
	f.inode.session.fd_attr = true;
	f.inode.i_nlink = 0;
	seen.owner_lost = true;
	script(refused, 2);
	CHECK(vfs_fchmod(&f) == -EPERM);
	expect_log(2, set);
	CHECK(seen.prepare == 2);
	seen.calls = seen.prepare = seen.begin = seen.end = 0;
	CHECK(vfs_utimes(&f, NULL) == -EACCES);
	CHECK(seen.calls == 2 && !strcmp(seen.ops[1], "GETATTR") &&
	      !strcmp(seen.log[1], getattr_own));
	seen.calls = seen.prepare = seen.begin = seen.end = 0;
	seen.install_errs[1] = -HFS_ETARGET_MOVED;
	CHECK(vfs_fchmod(&f) == -EPERM);
	expect_log(2, set);
	fixture_free(&f);
}

/* No description of the object is open (or every one is dead): the path
 * form's -ESTALE, as before s64. The walk is bounded by HFS_FD_ATTR_TRIES.
 */
static void test_fdattr_none_left(void)
{
	static const struct logged set[] = { { "SETATTR", chmod_path } };
	struct hfs_description many[HFS_FD_ATTR_TRIES + 2];
	struct fixture f;
	unsigned int i;

	fixture_init(&f, S_IFREG | 0644);
	f.inode.session.fd_attr = true;
	list_del(&f.description.list);
	INIT_LIST_HEAD(&f.inode.session.descriptions);
	seen.call_err = -ESTALE;
	CHECK(vfs_fchmod(&f) == -ESTALE);
	expect_log(1, set);
	seen.calls = seen.begin = seen.end = 0;
	f.inode.i_nlink = 0;
	CHECK(vfs_fstat(&f) == -ESTALE);
	CHECK(!seen.calls && seen.begin == seen.end);
	for (i = 0; i < HFS_FD_ATTR_TRIES + 2; i++) {
		many[i].cap = test_cap("{\"d\":4}");
		many[i].cap->token[0] = 10 + i;
		list_add_tail(&many[i].list, &f.inode.session.descriptions);
	}
	seen.calls = 0;
	seen.call_err = -EACCES;
	CHECK(vfs_fstat(&f) == -ESTALE);
	CHECK(seen.calls == HFS_FD_ATTR_TRIES);
	seen.calls = 0;
	CHECK(vfs_fchmod(&f) == -ESTALE);
	CHECK(seen.calls == 2 * HFS_FD_ATTR_TRIES);
	for (i = 0; i < HFS_FD_ATTR_TRIES + 2; i++)
		hfs_cap_free(many[i].cap);
	fixture_free(&f);
}

/* An old forwarder or host refuses the described shape. On a minor-0/1
 * session (no ALLOCATE) hfs_fdattr is cleared once and the path error
 * stands; later calls send the path form only. On a controlled session the
 * error is returned as it is.
 */
static void test_fdattr_old_peer(void)
{
	static const int refused[] = { -ESTALE, -EINVAL };
	static const int protocol[] = { -ESTALE, -EPROTO };
	static const struct logged set[] = { { "SETATTR", chmod_path }, { "SETATTR", chmod_own } };
	static const struct logged get[] = { { "GETATTR", getattr_path }, { "GETATTR", getattr_own } };
	struct fixture f;

	fixture_init(&f, S_IFREG | 0644);
	f.inode.session.fd_attr = true;
	f.inode.session.bridge_minor = 1;
	script(refused, 2);
	CHECK(vfs_fchmod(&f) == -ESTALE);
	expect_log(2, set);
	CHECK(!f.inode.session.fd_attr && warnings == 1);
	seen.calls = seen.begin = seen.end = 0;
	seen.scripted = 0;
	seen.call_err = -ESTALE;
	CHECK(vfs_fchmod(&f) == -ESTALE && seen.calls == 1);
	fixture_free(&f);
	fixture_init(&f, S_IFREG | 0644);
	f.inode.session.fd_attr = true;
	script(protocol, 2);
	CHECK(vfs_fstat(&f) == -ESTALE);
	expect_log(2, get);
	CHECK(!f.inode.session.fd_attr && warnings == 1);
	fixture_free(&f);
	fixture_init(&f, S_IFREG | 0644);
	f.inode.session.fd_attr = true;
	f.inode.session.bridge_minor = 4;
	script(refused, 2);
	CHECK(vfs_fchmod(&f) == -EINVAL);
	expect_log(2, set);
	CHECK(f.inode.session.fd_attr && !warnings);
	fixture_free(&f);
}

/* A Stale from a failed transport or a blocked session is the session's
 * answer, not the path's: no fallback.
 */
static void test_fdattr_session_stale(void)
{
	static const struct logged set[] = { { "SETATTR", chmod_path } };
	static const struct logged get[] = { { "GETATTR", getattr_path } };
	struct fixture f;
	unsigned int i;

	for (i = 0; i < 2; i++) {
		fixture_init(&f, S_IFREG | 0644);
		f.inode.session.fd_attr = true;
		f.inode.session.transport_failed = !i;
		f.inode.session.blocked = i;
		seen.call_err = -ESTALE;
		CHECK(vfs_fchmod(&f) == -ESTALE);
		expect_log(1, set);
		seen.calls = seen.begin = seen.end = 0;
		CHECK(vfs_fstat(&f) == -ESTALE);
		expect_log(1, get);
		fixture_free(&f);
	}
}

/* ATTR_FILE without ATTR_SIZE uses the file's own description directly, and
 * its Permission is the caller's real answer (it holds that file).
 * ftruncate's TRUNCATE is unchanged by hfs_fdattr, unlinked or not.
 */
static void test_fdattr_file(void)
{
	static const struct logged set[] = { { "SETATTR", chmod_own } };
	static const char ftruncate_args[] = "{\"description\":{\"d\":2},"
		"\"object\":{\"o\":1},\"size\":0,\"times\":{" NO_TIMES ","
		CTIME_NOW ",\"mtime\":null,\"mtime_now\":true}}";
	struct fixture f;
	struct iattr attr;

	fixture_init(&f, S_IFREG | 0644);
	f.inode.session.fd_attr = true;
	attr = (struct iattr){ .ia_valid = ATTR_MODE | ATTR_CTIME | ATTR_FILE,
			       .ia_mode = 0600, .ia_file = &f.file };
	CHECK(hfs_setattr(&init_user_ns, &f.dentry, &attr) == 0);
	expect_log(1, set);
	seen.calls = seen.begin = seen.end = 0;
	seen.call_err = -EACCES;
	attr = (struct iattr){ .ia_valid = ATTR_MODE | ATTR_CTIME | ATTR_FILE,
			       .ia_mode = 0600, .ia_file = &f.file };
	CHECK(hfs_setattr(&init_user_ns, &f.dentry, &attr) == -EACCES);
	expect_log(1, set);
	fixture_free(&f);
	fixture_init(&f, S_IFREG | 0644);
	f.inode.session.fd_attr = true;
	f.inode.i_nlink = 0;
	CHECK(vfs_truncate(&f, 0, ATTR_MTIME | ATTR_CTIME, true, false) == 0);
	expect_call("TRUNCATE", ftruncate_args);
	fixture_free(&f);
}

static void test_fdattr(void)
{
	test_fdattr_off();
	test_fdattr_rename();
	test_fdattr_unlinked();
	test_fdattr_walk();
	test_fdattr_owner_refusal();
	test_fdattr_none_left();
	test_fdattr_old_peer();
	test_fdattr_session_stale();
	test_fdattr_file();
}
