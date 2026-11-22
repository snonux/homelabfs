/* vf4 cases: each request's exact host CALL, or none. */
#define NO_TIMES "\"atime\":null,\"atime_now\":false"
#define NO_MTIME "\"mtime\":null,\"mtime_now\":false"
#define OBJECT ",\"object\":{\"o\":1}}"

/* One CALL with these exact bytes, after setattr_prepare, inside begin/end. */
static void expect_call(const char *opcode, const char *args)
{
	if (strcmp(seen.args, args))
		fprintf(stderr, "got  %s\nwant %s\n", seen.args, args);
	CHECK(!strcmp(seen.opcode, opcode) && !strcmp(seen.args, args));
	CHECK(seen.prepare == 1 && seen.begin == 1 && seen.end == 1 && seen.install == 1);
}

#define expect_no_call() CHECK(!seen.opcode[0] && !seen.begin && !seen.end && !seen.install)

static const char chown_args[] = "{\"attributes\":{" NO_TIMES ",\"gid\":42002,"
	"\"mode\":null," NO_MTIME ",\"uid\":41002}" OBJECT;

/* R18-1: T1's fchown(fd, 41002, 42002) of a fresh 0640 file. Pre-vf4 it
 * returned EOPNOTSUPP before any CALL.
 */
static void test_chown_plain(void)
{
	struct fixture f;

	fixture_init(&f, S_IFREG | 0640);
	CHECK(vfs_chown(&f, 41002, 42002, true, false) == 0);
	expect_call("SETATTR", chown_args);
	fixture_free(&f);
	/* A surviving ATTR_KILL_PRIV is accepted too. */
	fixture_init(&f, S_IFREG | 0640);
	CHECK(vfs_chown(&f, 41002, 42002, true, true) == 0);
	expect_call("SETATTR", chown_args);
	fixture_free(&f);
}

/* suid, group-executable sgid, and sgid kept by Linux (caller in the
 * non-executable group) with suid: notify_change derives a MODE each time; it
 * is dropped, the host clears 06000 on the ownership change. The last shape
 * (MODE still holding S_ISGID next to UID/GID) would otherwise hit the kept
 * refusal.
 */
static void test_chown_setid(void)
{
	static const umode_t modes[] = { 04755, 02755, 06755, 06740, 02740 };
	struct fixture f;
	unsigned int i;

	for (i = 0; i < sizeof(modes) / sizeof(modes[0]); i++) {
		fixture_init(&f, S_IFREG | modes[i]);
		CHECK(vfs_chown(&f, 41002, 42002, true, false) == 0);
		expect_call("SETATTR", chown_args);
		/* setattr_prepare checked the request with its derived MODE
		 * (02740 with the caller in its group keeps sgid: no MODE).
		 */
		CHECK(!!(seen.prepared_valid & ATTR_MODE) == (modes[i] != 02740));
		fixture_free(&f);
	}
	/* Review 1: a non-owner with CAP_CHOWN only (no CAP_FOWNER) gets EPERM
	 * from the derived MODE's owner check, as in Linux; nothing is sent.
	 * Without setid bits there is no MODE, and the chown goes through.
	 */
	fixture_init(&f, S_IFREG | 04755);
	seen.not_owner = true;
	CHECK(vfs_chown(&f, 41002, 42002, true, false) == -EPERM);
	expect_no_call();
	fixture_free(&f);
	fixture_init(&f, S_IFREG | 0755);
	seen.not_owner = true;
	CHECK(vfs_chown(&f, 41002, 42002, true, false) == 0);
	expect_call("SETATTR", chown_args);
	fixture_free(&f);
	/* chgrp only, caller outside the group: KILL_SGID derived, dropped. */
	fixture_init(&f, S_IFREG | 02740);
	CHECK(vfs_chown(&f, NO_ID, 42002, false, false) == 0);
	expect_call("SETATTR", "{\"attributes\":{" NO_TIMES ",\"gid\":42002,"
		    "\"mode\":null," NO_MTIME ",\"uid\":null}" OBJECT);
	fixture_free(&f);
}

/* chown(-1, -1): the derived MODE of a setuid file is not sent either
 * (review 1: the cached mode may be stale), so like a plain file only
 * ATTR_CTIME is left, which stays unsupported (no ctime-only host SETATTR).
 */
static void test_chown_no_owner(void)
{
	static const umode_t modes[] = { 04755, 0644 };
	struct fixture f;
	unsigned int i;

	for (i = 0; i < sizeof(modes) / sizeof(modes[0]); i++) {
		fixture_init(&f, S_IFREG | modes[i]);
		CHECK(vfs_chown(&f, NO_ID, NO_ID, true, false) == -EOPNOTSUPP);
		expect_no_call();
		CHECK(!seen.prepare);
		fixture_free(&f);
	}
}

/* Directories carry no kill bits; explicit requests are unchanged. */
static void test_unchanged_requests(void)
{
	struct fixture f;
	struct iattr attr;

	fixture_init(&f, S_IFDIR | 02775);
	CHECK(vfs_chown(&f, 41002, 42002, true, false) == 0);
	expect_call("SETATTR", chown_args);
	fixture_free(&f);
	/* chmod 04755: ATTR_MODE|ATTR_CTIME, mode kept with its suid bit. */
	fixture_init(&f, S_IFREG | 0755);
	attr = (struct iattr){ .ia_valid = ATTR_MODE | ATTR_CTIME, .ia_mode = 04755 };
	CHECK(hfs_setattr(&init_user_ns, &f.dentry, &attr) == 0);
	expect_call("SETATTR", "{\"attributes\":{" NO_TIMES ",\"gid\":null,"
		    "\"mode\":2541," NO_MTIME ",\"uid\":null}" OBJECT);
	fixture_free(&f);
	/* utimensat(atime explicit, mtime now). */
	fixture_init(&f, S_IFREG | 0644);
	attr = (struct iattr){ .ia_valid = ATTR_ATIME | ATTR_ATIME_SET | ATTR_MTIME |
			       ATTR_CTIME, .ia_atime = { 100, 7 } };
	CHECK(hfs_setattr(&init_user_ns, &f.dentry, &attr) == 0);
	expect_call("SETATTR", "{\"attributes\":{\"atime\":{\"nanos\":7,\"seconds\":100},"
		    "\"atime_now\":false,\"gid\":null,\"mode\":null,\"mtime\":null,"
		    "\"mtime_now\":true,\"uid\":null}" OBJECT);
	fixture_free(&f);
}

/* tg4: utimensat(NULL)/futimens(fd, NULL) and both UTIME_NOW carry
 * ATTR_TOUCH; before tg4 hfs_setattr_supported refused it with EOPNOTSUPP
 * before any CALL. Both now send host-current atime and mtime; an explicit
 * pair (ATTR_TIMES_SET, no ATTR_TOUCH) is sent unchanged.
 */
static void test_touch(void)
{
	static const char now_args[] = "{\"attributes\":{\"atime\":null,"
		"\"atime_now\":true,\"gid\":null,\"mode\":null,\"mtime\":null,"
		"\"mtime_now\":true,\"uid\":null}" OBJECT;
	const struct timespec64 both_now[2] = { { 0, UTIME_NOW }, { 0, UTIME_NOW } };
	const struct timespec64 explicit[2] = { { 100, 7 }, { 200, 9 } };
	struct fixture f;

	fixture_init(&f, S_IFREG | 0644);
	CHECK(vfs_utimes(&f, NULL) == 0);
	CHECK(seen.prepared_valid & ATTR_TOUCH);
	expect_call("SETATTR", now_args);
	fixture_free(&f);
	fixture_init(&f, S_IFREG | 0644);
	CHECK(vfs_utimes(&f, both_now) == 0);
	CHECK(seen.prepared_valid & ATTR_TOUCH);
	expect_call("SETATTR", now_args);
	fixture_free(&f);
	fixture_init(&f, S_IFREG | 0644);
	CHECK(vfs_utimes(&f, explicit) == 0);
	CHECK(!(seen.prepared_valid & ATTR_TOUCH));
	expect_call("SETATTR", "{\"attributes\":{\"atime\":{\"nanos\":7,\"seconds\":100},"
		    "\"atime_now\":false,\"gid\":null,\"mode\":null,"
		    "\"mtime\":{\"nanos\":9,\"seconds\":200},\"mtime_now\":false,"
		    "\"uid\":null}" OBJECT);
	fixture_free(&f);
}

/* The refusals vf4 keeps: an explicit MODE with suid/sgid next to UID/GID,
 * and MODE next to SIZE, both without kill bits.
 */
static void test_kept_refusals(void)
{
	struct fixture f;
	struct iattr attr;

	fixture_init(&f, S_IFREG | 0755);
	attr = (struct iattr){ .ia_valid = ATTR_MODE | ATTR_UID | ATTR_CTIME,
			       .ia_mode = 04755, .ia_uid = { 41002 } };
	CHECK(hfs_setattr(&init_user_ns, &f.dentry, &attr) == -EOPNOTSUPP);
	expect_no_call();
	attr = (struct iattr){ .ia_valid = ATTR_MODE | ATTR_SIZE, .ia_mode = 0644 };
	CHECK(hfs_setattr(&init_user_ns, &f.dentry, &attr) == -EOPNOTSUPP);
	expect_no_call();
	CHECK(!seen.prepare);
	fixture_free(&f);
}

#define CTIME_NOW "\"ctime\":{\"nanos\":5,\"seconds\":1700000000}"

/* ftruncate(fd, 0) of a suid file by a caller without CAP_FSETID: do_truncate
 * adds KILL_SUID|ATTR_FORCE, notify_change derives MODE next to SIZE (pre-vf4
 * EOPNOTSUPP). The MODE is dropped; TRUNCATE clears 06000 for a non-root
 * caller. With CAP_FSETID (root) no kill bits are added at all.
 */
static void test_truncate(void)
{
	static const char ftruncate_args[] = "{\"description\":{\"d\":2},"
		"\"object\":{\"o\":1},\"size\":0,\"times\":{" NO_TIMES ","
		CTIME_NOW ",\"mtime\":null,\"mtime_now\":true}}";
	static const umode_t modes[] = { 0644, 04755, 06755, 02750 };
	struct fixture f;
	unsigned int i;

	for (i = 0; i < sizeof(modes) / sizeof(modes[0]); i++) {
		fixture_init(&f, S_IFREG | modes[i]);
		CHECK(vfs_truncate(&f, 0, ATTR_MTIME | ATTR_CTIME, true, false) == 0);
		expect_call("TRUNCATE", ftruncate_args);
		fixture_free(&f);
	}
	fixture_init(&f, S_IFREG | 04755);
	CHECK(vfs_truncate(&f, 0, ATTR_MTIME | ATTR_CTIME, true, true) == 0);
	expect_call("TRUNCATE", ftruncate_args);
	fixture_free(&f);
	/* truncate(path, 4096): no time bits, no description. */
	fixture_init(&f, S_IFREG | 04755);
	CHECK(vfs_truncate(&f, 4096, 0, false, false) == 0);
	expect_call("TRUNCATE", "{\"description\":null,\"object\":{\"o\":1},"
		    "\"size\":4096,\"times\":{" NO_TIMES ",\"ctime\":null," NO_MTIME "}}");
	fixture_free(&f);
	/* open(O_TRUNC) of a sgid, group-executable file. */
	fixture_init(&f, S_IFREG | 02755);
	CHECK(vfs_truncate(&f, 0, ATTR_MTIME | ATTR_CTIME | ATTR_OPEN, true, false) == 0);
	expect_call("TRUNCATE", ftruncate_args);
	fixture_free(&f);
}

/* file_remove_privs before a write by a caller without CAP_FSETID: a local
 * no-op (no setattr_prepare, no CALL); WRITE clears 06000 on the host.
 */
static void test_remove_privs(void)
{
	static const umode_t modes[] = { 04755, 02755, 06755 };
	struct fixture f;
	unsigned int i;

	for (i = 0; i < sizeof(modes) / sizeof(modes[0]); i++) {
		fixture_init(&f, S_IFREG | modes[i]);
		CHECK(vfs_remove_privs(&f, false) == 0);
		expect_no_call();
		CHECK(!seen.prepare);
		fixture_free(&f);
	}
	/* Only ATTR_KILL_PRIV (or ATTR_FORCE alone, once notify_change dropped it). */
	fixture_init(&f, S_IFREG | 0644);
	CHECK(vfs_remove_privs(&f, true) == 0);
	expect_no_call();
	CHECK(!seen.prepare);
	{
		struct iattr attr = { .ia_valid = ATTR_FORCE };

		CHECK(hfs_setattr(&init_user_ns, &f.dentry, &attr) == 0);
		expect_no_call();
		CHECK(!seen.prepare);
	}
	fixture_free(&f);
}

/* G2: the operations are set once (d_set_d_op fails the test where the kernel
 * would WARN), and each attach replaces the grant (ASan reports a leak).
 */
static void test_dentry_attach(void)
{
	struct dentry dentry = { 0 };

	d_set_d_op_calls = 0;
	hfs_dentry_attach(&dentry, NULL);
	CHECK(dentry.d_op == &hfs_dops && d_set_d_op_calls == 1 && !dentry.d_fsdata);
	hfs_dentry_attach(&dentry, test_cap("{\"p\":3}"));
	CHECK(d_set_d_op_calls == 1 && dentry.d_fsdata);
	hfs_dentry_attach(&dentry, test_cap("{\"p\":4}"));
	CHECK(d_set_d_op_calls == 1);
	CHECK(hfs_dops.d_revalidate(&dentry, LOOKUP_RCU) == -ECHILD);
	CHECK(hfs_dops.d_revalidate(&dentry, 0) == 0);
	hfs_dops.d_release(&dentry);
}

int main(void)
{
	test_chown_plain();
	test_chown_setid();
	test_chown_no_owner();
	test_unchanged_requests();
	test_touch();
	test_kept_refusals();
	test_truncate();
	test_remove_privs();
	test_dentry_attach();
	test_link_relookup();
	test_create_exists();
	printf("PASS: actual hfs_setattr kill bits (chown, truncate, remove_privs), "
	       "utimensat(NULL)/UTIME_NOW touch, kept refusals, hfs_dentry_attach, hfs_link and EEXIST relookup; "
	       "no kernel/VM/runtime proof\n");
	return 0;
}
