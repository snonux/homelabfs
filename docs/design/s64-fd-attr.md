# s64: GETATTR and SETATTR through the open file description (design)

Status: the host side is implemented (Go, this task): the schema, the grant
checks and dispatch in `internal/guestbridge`, and the spec text in
[Retained-9p-protocol.md](../spec/Retained-9p-protocol.md). fsservice needed
no change. The guest kernel (patch 0003) and lib9p forwarder changes are
implemented as specified below and compiled, with local harness tests
(see "Guest and forwarder tests" at the end). They have not run in a guest
or the lab. The lab tooling (bl4) sets `hfs_fdattr` only for a candidate
listed in `controller.py` `FD_ATTR`, which holds the r26 rebuild
`80cd01bb…` (the hp4 daemon and the gn4 kernel rebuilt for hp4's
comment-only patches, with the s64 exporter of the second r21 rebuild
`f21450ec…`, the first candidate with all three s64 parts; it, the r22
rebuild `2c85b230…`, the r23 rebuild `21c49a55…`, the r24 rebuild
`c9500232…` and the r25 rebuild `a3eac923…` are now superseded; see "Lab
tooling" at the end).

## Problem

The r64 analysis found a POSIX gap. GETATTR and SETATTR carry only `object`,
a PATH capability. The bridge re-walks its recorded ancestry on every call
(`revalidatePath`). After the open file is renamed (by any client) or
unlinked, that re-walk answers Stale. The guest kernel's `hfs_getattr` and
`hfs_setattr` always send the inode's or dentry's PATH capability, so:

- `fstat(fd)` gets ESTALE;
- `fchmod(fd)`, `fchown(fd)` and `futimens(fd)` get ESTALE.

POSIX requires all of them to work on the open file description, including
an unlinked file with nlink 0. `ftruncate` and `fsync` already work: r64 gave
TRUNCATE and SYNC an optional `description`, and with it the description is
the authority instead of the path.

## Choice: the r64 pattern for GETATTR and SETATTR

GETATTR and SETATTR get a second closed Args shape with a mandatory DESCRIPTION
capability. The host checks it as it checks TRUNCATE's and SYNC's description.

```
GETATTR  {object:Cap}                                     path authority (unchanged)
GETATTR  {description:Cap, object:Cap}                    description authority (s64)
SETATTR  {attributes:Attributes, object:Cap}              path authority (unchanged)
SETATTR  {attributes:Attributes, description:Cap, object:Cap}   description authority (s64)
```

Keys are listed in canonical (sorted) order, as the guest must emit them.

- `description` in the new shape must be a nonnull DESCRIPTION capability. A
  `null` is Invalid. TRUNCATE and SYNC keep their nullable field, but here
  the original shape already is the path form, so each meaning has exactly
  one encoding and one `original_hash`.
- The original shapes keep their exact bytes and meaning. An old guest never
  notices the extension.
- No other opcode gains a description. `pathAuthority` lists the four
  exempt opcodes by name (TRUNCATE, SYNC, GETATTR, SETATTR), so a later
  schema change cannot quietly turn OPEN's, LINK's or READDIR's object into
  possession authority.

Rejected alternatives:

- **A nullable `description` on both opcodes (exactly the TRUNCATE shape).**
  It changes the bytes of every GETATTR and SETATTR, so every old guest would
  be refused by a new host and every new guest by an old one. The additive
  shape breaks only the new form on an old host.
- **Description only, no `object`.** The guest always has the inode's
  object capability. Keeping it means the host still checks a live, unforged,
  in-scope PATH grant (`liveGrant`), and that it names the description's
  object (Stale otherwise). This is the r64 rule unchanged.
- **Guest-side caching of attributes (no protocol change).** It cannot
  serve fchmod or fchown, and cached attributes after another client's change
  are not POSIX `fstat` results.

## Host semantics (implemented)

Dispatch (`internal/guestbridge/dispatch.go`):

1. `resolveInputs` checks `object` with `liveGrant` (issued, unreleased,
   unaltered, this scope; else Permission). For a described GETATTR or
   SETATTR, `pathAuthority` skips `revalidatePath`.
2. `attachDescription` checks `description` with `liveGrant` as a
   DESCRIPTION grant. A released, unknown, altered or foreign-session
   description is Permission, as for READ, WRITE and TRUNCATE. The grant must
   name the same object as `object`, else Stale. It sets `Command.Handle` to
   the description's service handle.
3. fsservice resolves the handle against its open-handle table (local,
   logical and controlled cuts alike: `execute.go`, `logical.go`,
   `controlled.go`). A handle that is gone, or that names another object, is
   Stale. Getattr and Setattr then run on the object ID.

Rights:

| Call | Description rights needed | Unix rules (fsservice `UnixPermissions.Check`) |
| --- | --- | --- |
| GETATTR (fstat) | none | none on the object itself (as stat) |
| SETATTR mode (fchmod) | none | owner or root; setgid only for a member group; sticky only for directories |
| SETATTR uid/gid (fchown) | none | root, or owner keeping uid and choosing a member gid |
| SETATTR explicit times (futimens with times) | none | owner or root |
| SETATTR current times (futimens(fd, NULL)) | none | owner, root, or write permission on the current mode |
| TRUNCATE (ftruncate, r64) | write | none beyond the write description |

The description proves that the caller holds the open file. It does not prove
ownership. POSIX `fchmod`/`fchown`/`futimens` check the file's current owner
and mode, never the fd's access mode. A read-only description therefore lets
its owner fchmod, and a read-write description gives a non-owner no chmod.
SETATTR never carries a size (size changes are TRUNCATE), so it never needs a
write description. The guest still runs `setattr_prepare` first. The host
check is authoritative because the guest's cached owner can be stale.

Unlinked objects: an open file keeps its service handle, its handle pin
(record.HandlePin) and its object after its last name is removed. Getattr
returns nlink 0, and a Setattr is journaled on the object like the existing
TRUNCATE of an unlinked file. fsservice already supported this.

Lifetime and claims:

- A described call is an ordinary CALL. Its intent, including the
  description bytes, is part of `original_hash`. Replays, QUERY, CANCEL,
  GAP_RESOLVE and RETIRE work unchanged. On the controlled path (1.2 to 1.4),
  ALLOCATE carries the same Call and its schema check is the same
  `validateArgs`. The new shape is allocatable, and a malformed one
  (`description: null`) is refused before a number is issued.
- After PROCESS_EXIT, the exited-issuer exception still covers only FORGET
  and the final DESCRIPTION_RELEASE. A described GETATTR or SETATTR from an
  exited issuer refuses STALE at ALLOCATE, like any other call.
- SETATTR stays a protected mutation (`protectedMutation`). GETATTR stays a
  read (`readOperation` in viewcontrol, used for recall conflicts). Neither
  classification depends on the args, so view leases, recalls and the
  self-observation exemption (vj4) are unchanged.
- OBSERVE evidence (controlledruntime lifecycle) reports claims and
  outcomes by opcode and code. A described call is one more claim and adds
  no record kind.
- The description is not closed, released or sequenced by these calls.
  `lifetime` acts only for DESCRIPTION_CLOSE, DESCRIPTION_RELEASE and
  PROCESS_EXIT.

Safety argument:

- **No new authority.** The described form reaches only an object for
  which the session holds a live DESCRIPTION grant. That grant came from an
  OPEN or CREATE that passed the Unix open check. It already authorizes
  READ, WRITE, TRUNCATE and SYNC on this object after rename or unlink
  ("DESCRIPTION grants retain issued rights across chmod/rename/unlink").
  Reading attributes through it reveals no more than READ. Changing
  attributes through it still needs the owner rules on the current
  attributes, which do not depend on the path.
- **No confused deputy across objects.** `object` must be a live grant of
  this scope and must name the description's object. The service handle must
  name the same object. Any mismatch is Stale, and a forged or foreign token
  is Permission.
- **Path semantics stay where POSIX needs them.** stat(2), chmod(2) and
  friends by name keep the original shape and its re-walk. A stale PATH
  capability alone still answers Stale (test
  `TestDescribedAttrCallsSurviveRenameAndUnlink`, first half).
- **Revocation unchanged.** Generation or session revocation and
  DESCRIPTION_RELEASE end the description's authority (Permission after
  release). The mount-scoped, credential-independent use of a description
  matches r64. The trusted guest kernel is the adapter, as for every grant.

## Version negotiation and old peers

The retained-9p family has no optional-feature channel. HELLO selects an
exact tuple, the guest checks the enrollment's key set exactly, and results
are closed unions. Any new advertisement field would break the old peer. So:

| Guest | Forwarder and host | Behaviour |
| --- | --- | --- |
| pre-s64 | any | sends only the original shapes; an s64 host accepts them unchanged |
| s64, `hfs_fdattr` off (default) | any | sends only the original shapes, exactly today's bytes |
| s64, `hfs_fdattr` on | s64 | described forms used as below |
| s64, `hfs_fdattr` on | pre-s64 (misconfiguration) | refused before admission, no effect (below) |

How an old peer refuses: the old lib9p `_args` (`hfs9p.c`) compares the key
count with the fixed set, so the extra `description` key makes `_command`
return EINVAL when the cell completes. Nothing reaches the host. A pre-s64
Go host reached directly refuses the shape in `validateArgs` (an unknown key
is Invalid) before claim admission. Either way no claim is admitted or
allocated on the host, and nothing has an effect.

That refusal is safe but is not a negotiation. On a controlled session
(minor 2 to 4, the lab's 1.4) the refused exchange is an ALLOCATE. The guest
treats every failed ALLOCATE as uncertain: it keeps `allocation_pending` and
sets `allocation_failed` (x84), so later ordinary issuance on that session
stops. That fails closed but costs the session. Therefore:

- **Negotiation is explicit configuration**, a new guest mount option
  `hfs_fdattr`. The lab tooling (`tools/lab/retained9p`, bl4) sets it only
  when the plan names a candidate whose guest kernel, lib9p forwarder and
  host all carry s64 (`controller.py` `FD_ATTR`). Without it the guest keeps the path form. That is the
  fallback.
- On a minor-0 or minor-1 session (no ALLOCATE), a described call refused
  with -EPROTO/-EINVAL at SEND clears the session's `fd_attr` flag and
  returns the path call's error. This is cleanup for a misconfigured mount.
  It is not relied on.
- A proper in-band negotiation means a new exact tuple: feature bit 15 on top
  of the terminal tuple (features 65535), enrolled, signed and checked like
  32767. That touches enrollment, controlledruntime, viewcontrol and the
  lib9p session-features checks. It should come with the next tuple
  revision, not with this gap fix. If it is added, `hfs_fdattr` becomes
  "enrolled features include bit 15".

## Guest kernel change (`0003-hfs1-guest-vfs.patch`)

### What the VFS hands the filesystem

In the guest's 5.x/6.x VFS (the `struct user_namespace *` inode operations):

- `fstat`/`statx(fd, "", AT_EMPTY_PATH)` → `vfs_getattr(&file->f_path, ...)`.
  `->getattr` receives only the `path`. It cannot see the `struct file`,
  and `flags` does not tell fstat from stat.
- `fchmod` → `chmod_common(&file->f_path)`, `fchown` →
  `chown_common(&file->f_path)` and `futimens` → `vfs_utimes(&file->f_path)`.
  All of them call `notify_change` **without** `ATTR_FILE`.
- Only `do_truncate` from `ftruncate`, and `open(O_TRUNC)`, set
  `ATTR_FILE | ia_file`. Both carry `ATTR_SIZE`, which `hfs_setattr`
  already sends as a described TRUNCATE.

So "use the description when called with a file" only covers ftruncate,
which is already done. For fstat, fchmod, fchown and futimens the guest must
find a description of the same inode itself. The rule below is POSIX-correct
for both fd and name callers:

1. If `ATTR_FILE` is set and `ia_file->private_data` has a cap, use that
   description (today: TRUNCATE only).
2. Otherwise, if `inode->i_nlink == 0` (the host reported the object
   unlinked) and `session->fd_attr` is set, send the described form with a
   borrowed description (below). The path form would be Stale anyway.
3. Otherwise send the path form. If it fails with -ESTALE from a host
   `Stale` result (not `session->transport_failed` and not a blocked
   session), and `session->fd_attr` is set, retry with borrowed descriptions
   (below). If no description of this object is open, return -ESTALE as
   today.

**Invariant that makes step 3 safe.** `hfs_d_revalidate` returns 0 outside
RCU walk (patch 0003, `hfs_d_revalidate`). The guest therefore keeps no
dentry across walks: every name walk does a fresh LOOKUP, and the path
capability that GETATTR or SETATTR sends was issued inside the same syscall.
A host Stale on the path form means one of two things. Either the caller
reached the inode through an fd (or cwd) whose dentry predates a rename or
unlink, which is exactly the fstat/fchmod case. Or a rename or unlink raced
with this one syscall, between its LOOKUP and its GETATTR/SETATTR. In both
cases, completing the call on the object the walk resolved is what a local
filesystem does: notify_change and vfs_getattr act on the resolved inode,
whatever happens to its name later. The retry is a new claim with a new
intent, never a replay of the refused one, and the refused call is a normal
terminal Stale with no effect.

If hfs ever keeps dentries (a `d_revalidate` that returns 1, or any dentry
or attribute cache), this no longer holds. A name walk could then reuse a
dentry whose name was renamed away long before the syscall. The fallback
would hide that real staleness and act on an object the caller can no longer
name, where the VFS would have relooked up the name after ESTALE
(LOOKUP_REVAL). When the guest change is built, the comment on
`hfs_d_revalidate` must state this dependency: "s64: the Stale fallback in
hfs_getattr/hfs_setattr relies on this returning 0 (no dentry outlives a
walk); caching dentries requires limiting that fallback to fd callers
first." A test that keeps an fd open, renames the file from another client
and then runs `stat(old name)` (expected ENOENT, never the old file's
attributes) should guard it. That test needs a guest VM; bl4 added it to the
lab's T3 `metadata` case for `fd_attr` runs (see "Lab tooling" below).

### Borrowing a description

`session->descriptions` (under `session->lock`) lists every open
description. Its `cap` is set once OPEN returned (`hfs_description_bind`),
and `hfs_description_discard` unlinks the entry under `session->lock` before
it frees the cap. Copying the cap's bytes under that lock is therefore safe.

An fd caller's own description cannot leave the list during its call:
`fstat`, `fchmod`, `fchown` and `futimens` hold the file through `fdget`,
and `hfs_release` runs only on the final `fput`. Another process's
description of the same object can be released at any moment, though. The
host then answers Permission (released grant) or Stale (handle gone). So
the guest tries matching descriptions one at a time. It skips every
description already refused in this syscall, so a closing description is
never retried, and the walk always reaches the caller's own live one.

```c
/* s64: kernel-internal "no open description of this object is left to try"
 * (never returned to userspace; distinct from a host NotFound, -ENOENT).
 * Above the kernel's internal 512-530, like HFS_ETARGET_MOVED. */
#define HFS_ENODESC 601
/* s64: borrowed descriptions tried per syscall; bounds the walk. */
#define HFS_FD_ATTR_TRIES 8

struct hfs_borrow {
	u8 tried[HFS_FD_ATTR_TRIES][32];	/* tokens refused in this syscall */
	u32 count;
};

/* s64: append "description":<cap>, for an open description of inode's
 * object that this syscall has not tried yet, and remember its token.
 * Returns 0 or -HFS_ENODESC. The cap bytes are copied under session->lock:
 * hfs_description_discard unlinks the description there before it frees
 * description->cap, so a listed cap is live while the lock is held. */
static int hfs_args_borrowed_description(struct hfs_buf *args,
					 struct inode *inode,
					 struct hfs_borrow *borrow)
{
	struct hfs_session *session = HFS_S(inode);
	const struct hfs_cap *object = HFS_I(inode)->object;
	struct hfs_description *description;
	int err = -HFS_ENODESC;
	u32 i;

	if (borrow->count == HFS_FD_ATTR_TRIES)
		return err;
	mutex_lock(&session->lock);
	list_for_each_entry(description, &session->descriptions, list) {
		const struct hfs_cap *cap = description->cap;

		if (!cap || !uuid_equal(&cap->home, &object->home) ||
		    !uuid_equal(&cap->allocation, &object->allocation))
			continue;
		for (i = 0; i < borrow->count; i++)
			if (!memcmp(borrow->tried[i], cap->token, 32))
				break;
		if (i < borrow->count)
			continue;
		memcpy(borrow->tried[borrow->count++], cap->token, 32);
		hfs_buf_literal(args, "\"description\":");
		hfs_cap_write(args, cap);
		hfs_buf_literal(args, ",");
		err = 0;
		break;
	}
	mutex_unlock(&session->lock);
	return err;
}
```

### hfs_getattr

```c
/* s64: GETATTR through the path (borrow == NULL) or through an untried open
 * description of the same object (fstat after rename/unlink). Key order is
 * canonical: "description" < "object". */
static int hfs_getattr_send(const struct path *path, struct hfs_borrow *borrow)
{
	struct inode *inode = d_inode(path->dentry);
	struct hfs_cred *cred;
	struct hfs_buf args;
	struct hfs_json *result = NULL;
	void *cookie;
	int err = hfs_begin(inode, false, &cred, &cookie);

	if (err)
		return err;
	err = hfs_args_init(&args);
	if (!err) {
		hfs_buf_literal(&args, "{");
		if (borrow)
			err = hfs_args_borrowed_description(&args, inode, borrow);
		hfs_buf_literal(&args, "\"object\":");
		hfs_dcap_write(&args, path->dentry);
		hfs_buf_literal(&args, "}");
	}
	if (!err)
		err = hfs_call(HFS_S(inode), "GETATTR", &args, cred, false, &result);
	if (!err)
		err = hfs_attr_install(inode, result,
				       hfs_json_field(result, 0, "attributes"));
	hfs_json_free(result);
	hfs_buf_free(&args);
	hfs_end(inode, cred, cookie);
	return err;
}

/* s64: try untried descriptions until one answers. A described GETATTR
 * has no owner rule, so -EACCES (released grant) and -ESTALE (gone or
 * mismatched handle) only mean "this description is dead": try the next. */
static int hfs_getattr_borrowed(const struct path *path, int fallback)
{
	struct hfs_session *session = HFS_S(d_inode(path->dentry));
	struct hfs_borrow borrow = {};
	int err;

	do {
		err = hfs_getattr_send(path, &borrow);
	} while (err == -EACCES || err == -ESTALE);
	return hfs_fd_attr_result(session, err, fallback);
}

static int hfs_getattr_once(const struct path *path)
{
	struct inode *inode = d_inode(path->dentry);
	struct hfs_session *session = HFS_S(inode);
	bool fd_attr = READ_ONCE(session->fd_attr);
	int err;

	if (fd_attr && !inode->i_nlink)
		return hfs_getattr_borrowed(path, -ESTALE);
	err = hfs_getattr_send(path, NULL);
	if (err != -ESTALE || !fd_attr || READ_ONCE(session->transport_failed) ||
	    READ_ONCE(session->blocked))
		return err;
	return hfs_getattr_borrowed(path, err);
}
```

`hfs_fd_attr_result(session, err, fallback)` turns the end of a borrowed
walk into the syscall's answer:

- `0` and real failures (EINTR, EHOSTDOWN, ETIMEDOUT, EIO, a host
  NotFound's -ENOENT, ...) are returned as they are;
- `-HFS_ENODESC` (nothing open is left to try) returns `fallback`, the path
  form's -ESTALE;
- `-EPROTO` or `-EINVAL` on a session with `bridge_minor < 2` clears
  `session->fd_attr` (old forwarder or host, see negotiation) and returns
  `fallback`; on a controlled session it is returned as it is.

`hfs_getattr` keeps its `hfs_reissue` loop around `hfs_getattr_once`.

### hfs_setattr

Split the CALL part of today's `hfs_setattr` out as
`hfs_setattr_send(dentry, attr, mode, borrow)`. The mode is PATH, FILE
(`attr->ia_file`'s own description, today only TRUNCATE) or BORROWED
(`hfs_args_borrowed_description`). The SETATTR args writer emits
`"attributes":{...},`, then, when described, `"description":<cap>,`, then
`"object":<dcap>`. That order is canonical. `hfs_setattr` then:

- runs `hfs_setattr_sent`, `hfs_setattr_supported` and `setattr_prepare`
  exactly as now, so the guest's owner and capability checks still run first
  on the cached attributes;
- for `ATTR_SIZE` (TRUNCATE) keeps today's behaviour: FILE when `ATTR_FILE`
  is set, else a null description;
- for SETATTR follows steps 1 to 3 above, with the borrowed walk below. If
  `ATTR_FILE` is ever set without `ATTR_SIZE`, its own description is used
  (FILE), and its Permission is a real refusal: the caller holds that file.

**Keeping a real refusal real.** For a described SETATTR, a host Permission
(-EACCES) has two causes: the borrowed description was released, or
fsservice's owner rule refused the change (a non-owner chmod or chown, or
"now" times without write access). A dead description must lead to the next
one, while an owner refusal must reach the caller, never -ESTALE. The
borrowed walk tells them apart with a described GETATTR through the **same**
description, which has no owner rule:

```c
/* s64: SETATTR through untried descriptions. After -EACCES, a GETATTR
 * through the same description (same borrow slot, so not re-borrowed)
 * tells a dead description (-EACCES/-ESTALE: try the next) from an owner
 * refusal (OK: return the refusal). That GETATTR also installs fresh
 * attributes. */
static int hfs_setattr_borrowed(struct dentry *dentry, struct iattr *attr,
				int fallback)
{
	struct hfs_session *session = HFS_S(d_inode(dentry));
	struct hfs_borrow borrow = {};
	int err, probe;

	for (;;) {
		err = hfs_setattr_send(dentry, attr, HFS_SETATTR_BORROWED, &borrow);
		if (err == -ESTALE)
			continue;
		if (err != -EACCES)
			break;
		probe = hfs_getattr_send_last(dentry, &borrow);
		if (!probe)
			return hfs_setattr_refusal(d_inode(dentry), attr, err);
		if (probe != -EACCES && probe != -ESTALE)
			return probe;
	}
	return hfs_fd_attr_result(session, err, fallback);
}
```

- `hfs_getattr_send_last` sends a described GETATTR with
  `borrow->tried[borrow->count - 1]`, the description the SETATTR just used.
  It looks the token up again under `session->lock` and fails with
  -HFS_ENODESC if that description has left the list (dead, so the walk
  goes on). The walk stays bounded by `HFS_FD_ATTR_TRIES`.
- `hfs_setattr_refusal(inode, attr, err)` returns what the path form would
  have returned for the same refusal: the host code's errno (-EACCES, today's
  mapping of `Permission` for any SETATTR). With the fresh attributes the
  probe installed, it returns -EPERM where Linux's `setattr_prepare` would:
  mode, uid, gid or explicit times by a caller that is not the owner (and
  lacks the capability). That is the errno chmod(2) and chown(2) give on a
  local filesystem.
- A race where an owner-refused description is released between the SETATTR
  and the probe only moves the walk to the next description. That
  description gives the same owner refusal, and the fd caller's own
  description is always live (above), so an fd caller always gets its real
  refusal.

Every helper stays under 50 lines, as the existing ones do.

### Mount option

`hfs_fdattr` is parsed next to `cache=mmap` (`hfs_cache_option`'s loop) into
`session->fd_attr` (a `bool`, read with `READ_ONCE`). `hfs_show_options`
prints it, and the lab's mountinfo checks (`tools/lab/retained9p/guest.py`
and `bootstrap_remote.py`, bl4) require `,cache=mmap,hfs_fdattr` exactly
when the plan asks for the option (`pod_fixture.py`'s subset check on m0
accepts it either way). The
option is refused (EINVAL) together with a non-hfs selector, like
`cache=mmap`.

### Not covered

A process whose cwd was renamed by another client still gets ESTALE on
relative walks (v64 annotation on s64). A cwd holds no open description, and
`hfs_permission` → `hfs_refresh` uses the inode's object capability. That
needs a separate directory-handle design.

## lib9p forwarder change (`tools/integration/lib9p/src/hfs9p.c`)

`_args` routes on a single fixed key string per opcode and requires the exact
field count. Add the described alternatives:

```c
   /* s64: GETATTR/SETATTR may also carry the fd's DESCRIPTION. The host's
    * closed parser checks it is a nonnull DESCRIPTION capability naming the
    * object's own object; the forwarder only routes. */
   static const struct {
      const char *op;
      const char *keys;
   } described[] = {
      {"GETATTR", "description object"},
      {"SETATTR", "attributes description object"},
   };
   ...
   if (!keys || j->tokens[args].type != '{')
      return (EINVAL);
   if (!hfs9p_json_key(j, args, "description", &token)) {
      for (unsigned i = 0; i < sizeof(described) / sizeof(described[0]); i++) {
         if (hfs9p_json_equal(j, opcode, described[i].op))
            keys = described[i].keys;
      }
   }
```

TRUNCATE, SYNC and the description-only opcodes already list `description`
and do not match `described[]`. Any other opcode with an extra `description`
still fails the count check. Add a `backend_test.c` case: described GETATTR
and SETATTR are forwarded, while `{"object":..,"description":..}` on
READLINK and a SETATTR missing `attributes` are EINVAL.

## Tests (host, this task)

`internal/guestbridge/fd_attr_test.go` (logical planner):

- `TestDescribedAttrCallsSurviveRenameAndUnlink`: after rename and after
  unlink (old name reused by another file), the path GETATTR and SETATTR are
  Stale. fchmod, fchown, futimens(NULL), ftruncate and fstat through the
  description succeed for the non-root owner. fstat shows the new mode, size
  and gid, and nlink 0 after unlink.
- `TestDescribedSetattrKeepsOwnerRules`: a non-owner holding the owner's
  read-write description or its own read-only one may fstat but gets
  Permission for chmod, chown (uid and gid), explicit times and current times
  without write permission. The owner may fchmod through a read-only
  description, and root may fchown.
- `TestDescribedAttrCallsRefuseDeadOrMismatchedDescriptions`: another file's
  description is Stale, a forged object is Permission, and a released
  description is Permission.
- `TestDescribedAttrCallsRefuseForeignSessionDescription`: another session's
  description, as issued or re-scoped, is Permission.
- `TestDescribedAttrSchema`: both shapes validate. A null or PATH-kind
  description, a missing object, and a description on READLINK or OPEN are
  Invalid.
- `TestDescribedAttrOldVersionBehaviour`: the pre-s64 closed field sets
  refuse the described shape (how an old host refuses), and the original
  shapes are unchanged and still path-authorized on the s64 host.
- `TestDescribedAttrDecodesUnderControlledAllocation`: a controlled ALLOCATE
  (minor 2) with the described GETATTR decodes, and its null variant is
  Invalid.
- `TestPathAuthorityExemptsOnlyDescribedFourOpcodes` (updated): exactly
  TRUNCATE, SYNC, GETATTR and SETATTR are exempt when described.

`internal/controlledvalidation/fd_attr_test.go` (durable controlled provider,
ControlledCut, journaled SETATTR):

- `TestBridgeDescribedAttrCallsAfterRenameUnlink`: the path forms are Stale;
  described fchmod and fstat work (mode, nlink 0 after unlink); a released
  description is Permission.
- `TestBridgeDescribedSetattrOwnerRulesMismatchAndForeignScope`: on an
  unlinked open file, a non-owner with the owner's description or its own
  read-only one may fstat but gets Permission for chmod and chown. Another
  file's description is Stale. A description re-scoped to another session,
  or with an altered token, is Permission.
- `TestBridgeDescribedCallReplayAfterRelease`: resending a described CALL
  with its original claim after DESCRIPTION_RELEASE returns the recorded
  terminal result, byte for byte.
- `TestBridgeDescribedSetattrUnlinkedRestartReplay`: a described SETATTR on
  an nlink-0 object crashes after its journaled effect and before its
  outcome. After a provider and service restart, `RecoverOutcome` gives its
  exact protected result, and the replayed object keeps mode 0604 and
  nlink 0.
- `TestBridgeDescribedAttrStaleWhenServiceHandleIsGone`: the description
  grant is still live in the bridge, but its fsservice handle was released
  directly through the controlled cut. Described GETATTR and SETATTR are
  Stale.

## Guest and forwarder tests

The guest code follows the sketches above, with these differences:

- One `enum hfs_attr_authority` (PATH, FILE, BORROWED, LAST) selects the
  description key for both opcodes (`hfs_args_description`).
  `hfs_args_listed_description` serves BORROWED and LAST (the
  `hfs_getattr_send_last` of the sketch).
- `hfs_description_publish` sets `description->cap` under `session->lock`
  for OPEN and CREATE, so the walk never reads a cap that is being
  published.
- The walk also stops on a failed transport or blocked session
  (`hfs_fd_attr_dead`), and `hfs_setattr_refusal` reruns `setattr_prepare`
  on the probe's fresh attributes.
- A stock (non-hfs) 9p mount naming `hfs_fdattr` is refused with EINVAL
  (`v9fs_hfs_only_option` in `v9fs_mount`), since stock 9p would ignore it.

Local tests (no kernel, VM or lab):

- `tools/integration/linux9p/tests/setattr` (`fdattr_cases.inc`) runs the
  actual `hfs_getattr_once`, `hfs_setattr` and every s64 helper against a
  logging `hfs_call`. It checks the exact bytes of every CALL in order:
  an `hfs_fdattr`-less mount sends only the path forms, even for nlink 0 or
  ATTR_FILE. It checks the rename fallback for fchmod, fchown,
  futimens(NULL) and fstat, the direct described form for nlink 0, and the
  walk past a dead description of the same object (with a SETATTR probe),
  never sending another object's description. It also checks the owner
  refusal (-EPERM from fresh attributes, -EACCES for current times, and a
  moved-target probe) and that no description left gives -ESTALE, bounded
  at `HFS_FD_ATTR_TRIES`. An old peer on minor 0/1 clears `fd_attr` once;
  on minor 4 the error is returned. A failed transport or blocked session
  gets no fallback. ATTR_FILE uses its own description. ftruncate is
  unchanged. `hfs_call` checks that `session->lock` is free.
- `tests/mount_hello`: `hfs_fdattr` mounts and shows
  `,cache=mmap,hfs_fdattr,...`. A repeated option, `hfs_fdattr=1` or an
  empty value is refused before enroll, and `v9fs_hfs_only_option` is
  checked.
- lib9p `backend_test` (`described_attr_args_test`): both described shapes
  route. A description on READLINK or OPEN, a described SETATTR without
  `attributes`, a GETATTR without `object`, and extra keys are EINVAL.

Not covered: a guest run.

## Lab tooling (bl4)

`tools/lab/retained9p` (README section "`hfs_fdattr` mounts"): the
bootstrap plan's optional `fd_attr_candidate_sha256` and the batch plan's
optional `"fd_attr": true` must name a candidate in `controller.py`
`FD_ATTR`; the mounts then add `hfs_fdattr`, `bootstrap_remote.py` and
`guest.py` require `,cache=mmap,hfs_fdattr` in mountinfo (and refuse it on a
plan without the field), and `fill_plan.py` refuses a template that
disagrees with the bootstrap's mounts. Plans without the fields keep their
bytes and behaviour. On an `fd_attr` request the T3 `metadata` case keeps
an fd open on m0, renames the file through m1, and requires `stat(old name)`
through m0 to be ENOENT and `fstat(fd)` to describe the same file. Its local
test runs the case on one directory opened twice; the fallback it guards is
only exercised in a guest.

bl4 also corrected two comments in patch 0003: the cap publisher named by
`hfs_args_listed_description` is `hfs_description_publish`, and
`hfs_fd_attr_result` records two accepted edges of the direct nlink-0 path
(a real host Invalid on a minor-0/1 session clears `fd_attr` and returns
-ESTALE; a dead object PATH grant makes every described try -EACCES, so the
walk ends in -ESTALE where the path form gives -EACCES).
