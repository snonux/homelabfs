/* Linux 6.1 VFS callers of ->setattr that carry ATTR_KILL_* bits (vf4). */
#define NO_ID ((unsigned int)-1)
static const struct timespec64 test_now = { 1700000000, 5 };

struct fixture {
	struct inode inode;
	struct dentry dentry;
	struct hfs_description description;
	struct file file;
};

static struct hfs_cap *test_cap(const char *json)
{
	struct hfs_cap *cap = calloc(1, sizeof(*cap));

	CHECK(cap && !hfs_json_parse(json, strlen(json), &cap->json));
	return cap;
}

/* A regular file (or another type) with its object grant and an open
 * description, as after a LOOKUP and an OPEN.
 */
static void fixture_init(struct fixture *f, umode_t mode)
{
	memset(f, 0, sizeof(*f));
	memset(&seen, 0, sizeof(seen));
	f->inode.i_mode = mode;
	f->inode.hfs.object = test_cap("{\"o\":1}");
	f->dentry.d_inode = &f->inode;
	f->description.cap = test_cap("{\"d\":2}");
	f->file.private_data = &f->description;
}

static void fixture_free(struct fixture *f)
{
	hfs_cap_free(f->inode.hfs.object);
	hfs_cap_free(f->description.cap);
	hfs_cap_free(f->dentry.d_fsdata);
}

/* setattr_should_drop_sgid (fs/attr.c): a group-executable sgid file always,
 * otherwise only when the caller is outside the file's group.
 */
static unsigned int drop_sgid(umode_t mode, bool in_group)
{
	if (!(mode & S_ISGID))
		return 0;
	return (mode & S_IXGRP) || !in_group ? ATTR_KILL_SGID : 0;
}

/* setattr_should_drop_suidgid (fs/attr.c): only for a regular file and a
 * caller without CAP_FSETID.
 */
static unsigned int drop_suidgid(umode_t mode, bool in_group, bool fsetid)
{
	unsigned int kill = (mode & S_ISUID ? ATTR_KILL_SUID : 0) |
		drop_sgid(mode, in_group);

	return kill && !fsetid && S_ISREG(mode) ? kill : 0;
}

/* notify_change (fs/attr.c:431-466). keep_priv models a file whose
 * security.capability exists; hfs has no xattr handlers, so the real
 * security_inode_need_killpriv is 0 and ATTR_KILL_PRIV is dropped.
 */
static int notify(struct fixture *f, struct iattr *attr, bool keep_priv)
{
	unsigned int valid = attr->ia_valid;
	umode_t mode = f->inode.i_mode;

	if (!keep_priv)
		valid &= ~ATTR_KILL_PRIV;
	CHECK(!((valid & (ATTR_KILL_SUID | ATTR_KILL_SGID)) && (valid & ATTR_MODE)));
	if ((valid & ATTR_KILL_SUID) && (mode & S_ISUID)) {
		valid |= ATTR_MODE;
		attr->ia_mode = mode & ~S_ISUID;
	}
	if ((valid & ATTR_KILL_SGID) && (mode & S_ISGID)) {
		if (!(valid & ATTR_MODE)) {
			valid |= ATTR_MODE;
			attr->ia_mode = mode;
		}
		attr->ia_mode &= ~S_ISGID;
	}
	attr->ia_valid = valid;
	if (!(valid & ~(ATTR_KILL_SUID | ATTR_KILL_SGID)))
		return 0;
	attr->ia_ctime = test_now;
	return hfs_setattr(&init_user_ns, &f->dentry, attr);
}

/* chown_common (fs/open.c:702-745); fchown and fchownat reach the same
 * function with the file's path, so one model covers chown and fchown.
 */
static int vfs_chown(struct fixture *f, unsigned int uid, unsigned int gid,
		     bool in_group, bool keep_priv)
{
	struct iattr attr = { .ia_valid = ATTR_CTIME };

	if (uid != NO_ID) {
		attr.ia_valid |= ATTR_UID;
		attr.ia_uid.val = uid;
	}
	if (gid != NO_ID) {
		attr.ia_valid |= ATTR_GID;
		attr.ia_gid.val = gid;
	}
	if (!S_ISDIR(f->inode.i_mode))
		attr.ia_valid |= ATTR_KILL_SUID | ATTR_KILL_PRIV |
			drop_sgid(f->inode.i_mode, in_group);
	return notify(f, &attr, keep_priv);
}

/* do_truncate (fs/open.c:39-65): truncate(2) passes no time bits and no
 * file, ftruncate(2) ATTR_MTIME|ATTR_CTIME and the file, open(O_TRUNC) also
 * ATTR_OPEN.
 */
static int vfs_truncate(struct fixture *f, long long size, unsigned int times,
			bool with_file, bool fsetid)
{
	struct iattr attr = { .ia_valid = ATTR_SIZE | times, .ia_size = size };
	unsigned int kill = drop_suidgid(f->inode.i_mode, true, fsetid);

	if (with_file) {
		attr.ia_file = &f->file;
		attr.ia_valid |= ATTR_FILE;
	}
	if (kill)
		attr.ia_valid |= kill | ATTR_FORCE;
	return notify(f, &attr, false);
}

/* __file_remove_privs and __remove_privs (fs/inode.c:2063-2097), before a
 * write by a caller without CAP_FSETID. keep_priv adds ATTR_KILL_PRIV, as
 * dentry_needs_remove_privs would for a file with security.capability.
 */
static int vfs_remove_privs(struct fixture *f, bool keep_priv)
{
	unsigned int kill = drop_suidgid(f->inode.i_mode, true, false) |
		(keep_priv ? ATTR_KILL_PRIV : 0);
	struct iattr attr = { .ia_valid = ATTR_FORCE | kill };

	if (!S_ISREG(f->inode.i_mode) || !kill)
		return 0;
	return notify(f, &attr, keep_priv);
}
