//go:build linux || freebsd

package controlledruntime

import (
	"encoding/hex"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Guest VFS shapes of the T1 replay (recall_workload_t1_test.go), the same
// RPC sequences internal/viewcontrol's advanceReplay sends
// (zq3_advance_replay_test.go), here inside measured guest syscalls.

// invalidate unhashes mount i's cached dentry of n in dir before a fresh
// LOOKUP (hfs_d_revalidate returns 0): an unpinned dentry's inode is
// evicted at once and FORGETs its grant.
func (r *t1Replay) invalidate(i int, dir guestbridge.Capability, n string) {
	key := t1Key(dir, n)
	if e := r.dcache[i][key]; e != nil {
		delete(r.dcache[i], key)
		e.dead = true
		r.evict(i, e)
	}
}

// evict FORGETs a dead dentry's grant once no open file pins it.
func (r *t1Replay) evict(i int, e *t1Dentry) {
	if e.dead && e.pins == 0 {
		r.do(i, "FORGET", map[string]any{"object": e.cap, "count": 1})
	}
}

// creating is open(O_CREAT|O_EXCL) or mkdir in dir on m0: GETATTR, LOOKUP
// NotFound and GETATTR observations, then the creating CALL while the last
// observation's release is still queued, which starts the CALL_CONFLICT
// recall that holds the CALL. The new dentry is cached, pinned by the
// created file's description.
func (r *t1Replay) creating(op string, dir guestbridge.Capability, n string, args map[string]any) []guestbridge.Capability {
	r.observe(0, "GETATTR", map[string]any{"object": dir})
	r.invalidate(0, dir, n)
	r.observe(0, "LOOKUP", map[string]any{"parent": dir, "name": t1Name(n)})
	queued, rec := r.acquire(0)
	r.do(0, "GETATTR", map[string]any{"object": dir})
	args["parent"], args["name"] = dir, t1Name(n)
	res := r.do(0, op, args)
	if res.Code != "OK" {
		r.m.t.Fatalf("%s %s: %+v", op, n, res)
	}
	r.release(0, queued, rec)
	e := &t1Dentry{cap: res.Capabilities[0]}
	r.dcache[0][t1Key(dir, n)] = e
	if op == "CREATE" {
		e.pins = 1
		r.files[res.Capabilities[1].Token] = e
	}
	return res.Capabilities
}

// mkdir creates directory n in dir. Its grant is not returned: the caller's
// next open walks to n again, which unhashes the unpinned new dentry and
// FORGETs that grant (invalidate), so every later use goes through the
// opened object.
func (r *t1Replay) mkdir(dir guestbridge.Capability, n string, mode int) {
	r.syscall("mkdir "+n, func() {
		r.creating("MKDIR", dir, n, map[string]any{"mode": mode, "umask": 0})
	})
}

// create creates file n in dir and returns its object and description.
func (r *t1Replay) create(dir guestbridge.Capability, n string, mode, umask int) (guestbridge.Capability, guestbridge.Capability) {
	var caps []guestbridge.Capability
	r.syscall("create-exclusive "+n, func() {
		caps = r.creating("CREATE", dir, n, map[string]any{"mode": mode, "umask": umask, "flags": t1Flags("READWRITE", true)})
	})
	return caps[0], caps[1]
}

// open walks to n in dir on mount i and opens it (one syscall): the walk's
// observations, then OPEN, whose description pins the dentry.
func (r *t1Replay) open(i int, dir guestbridge.Capability, n, access string) (guestbridge.Capability, guestbridge.Capability) {
	var object, description guestbridge.Capability
	r.syscall(t1Label(i, "open %s %s", n, access), func() {
		object = r.walk(i, dir, n)
		description = r.do(i, "OPEN", map[string]any{"object": object, "flags": t1Flags(access, false)}).Capabilities[0]
		e := r.dcache[i][t1Key(dir, n)]
		e.pins++
		r.files[description.Token] = e
	})
	return object, description
}

// stat is one stat(2) of n in dir on mount i.
func (r *t1Replay) stat(i int, dir guestbridge.Capability, n string) guestbridge.Capability {
	var object guestbridge.Capability
	r.syscall(t1Label(i, "stat %s", n), func() { object = r.walk(i, dir, n) })
	return object
}

// walk is GETATTR of dir, a fresh LOOKUP of n and GETATTR of it.
func (r *t1Replay) walk(i int, dir guestbridge.Capability, n string) guestbridge.Capability {
	r.observe(i, "GETATTR", map[string]any{"object": dir})
	r.invalidate(i, dir, n)
	object := r.observe(i, "LOOKUP", map[string]any{"parent": dir, "name": t1Name(n)}).Capabilities[0]
	r.dcache[i][t1Key(dir, n)] = &t1Dentry{cap: object}
	r.observe(i, "GETATTR", map[string]any{"object": object})
	return object
}

// chmod is one fchmod (or fchown, replayed as fchmod): the permission
// refresh, then SETATTR of the mode.
func (r *t1Replay) chmod(call string, object guestbridge.Capability, n string, mode int) {
	r.syscall(call+" "+n, func() {
		r.observe(0, "GETATTR", map[string]any{"object": object})
		attributes := map[string]any{"mode": mode, "uid": nil, "gid": nil, "atime": nil, "mtime": nil, "atime_now": false, "mtime_now": false}
		if res := r.do(0, "SETATTR", map[string]any{"object": object, "attributes": attributes}); res.Code != "OK" {
			r.m.t.Fatalf("SETATTR %o: %+v", mode, res)
		}
	})
}

// fstat is one fstat of an open object on m0.
func (r *t1Replay) fstat(object guestbridge.Capability, n string) {
	r.syscall("fstat "+n, func() { r.observe(0, "GETATTR", map[string]any{"object": object}) })
}

// close is one close(2) on mount i: DESCRIPTION_CLOSE, then the last fput's
// DESCRIPTION_RELEASE and the FORGET of a dentry unhashed meanwhile.
func (r *t1Replay) close(i int, description guestbridge.Capability, n string) {
	r.syscall(t1Label(i, "close %s", n), func() {
		r.closes[i]++
		r.do(i, "DESCRIPTION_CLOSE", map[string]any{"description": description, "process": r.p.cred.Process, "close_sequence": r.closes[i]})
		r.do(i, "DESCRIPTION_RELEASE", map[string]any{"description": description, "release_sequence": r.closes[i]})
		if e := r.files[description.Token]; e != nil {
			delete(r.files, description.Token)
			e.pins--
			r.evict(i, e)
		}
	})
}

// groupFile is create_owned_file plus T1's two group children: create,
// fchown and fchmod, write, close; the allowed child opens and reads it, the
// denied child is refused after its walk.
func (r *t1Replay) groupFile(dir guestbridge.Capability, file string) {
	object, description := r.create(dir, file, 0640, 022)
	r.chmod("fchown", object, file, 0640)
	r.chmod("fchmod", object, file, 0640)
	r.syscall("write "+file, func() {
		data := hex.EncodeToString([]byte(file + "-secret"))
		r.do(0, "WRITE", map[string]any{"description": description, "offset": 0, "data": data, "max_end": 1 << 40})
	})
	r.close(0, description, file)
	_, read := r.open(0, dir, file, "READ")
	r.syscall("read "+file, func() {
		r.observe(0, "READ", map[string]any{"description": read, "offset": 0, "count": 64})
	})
	r.close(0, read, file)
	r.stat(0, dir, file)
}

// setgid is credentials_setgid: mkdir, open, fchown and fchmod, the child's
// create and fstat, the closes, then the stat of the created file through
// m1 (one path walk).
func (r *t1Replay) setgid(leaf, m1Root guestbridge.Capability) {
	r.mkdir(leaf, "setgid", 02777)
	dir, description := r.open(0, leaf, "setgid", "READ")
	r.chmod("fchown", dir, "setgid", 02777)
	r.chmod("fchmod", dir, "setgid", 02777)
	created, createdDescription := r.create(dir, "created", 0666, 027)
	r.fstat(created, "created")
	r.close(0, createdDescription, "created")
	r.close(0, description, "setgid")
	r.syscall("m1 stat-cross-domain credentials/setgid/created", func() {
		object := r.walk(1, m1Root, "credentials")
		object = r.walk(1, object, "setgid")
		r.walk(1, object, "created")
	})
}

// sticky is credentials_sticky: mkdir, open, fchmod, create and fchown of
// the owned file, close, the child's refused unlink after its walk, close.
func (r *t1Replay) sticky(leaf guestbridge.Capability) {
	r.mkdir(leaf, "sticky", 01777)
	dir, description := r.open(0, leaf, "sticky", "READ")
	r.chmod("fchmod", dir, "sticky", 01777)
	owned, ownedDescription := r.create(dir, "owned", 0666, 022)
	r.chmod("fchown", owned, "owned", 0644)
	r.close(0, ownedDescription, "owned")
	r.stat(0, dir, "owned")
	r.close(0, description, "sticky")
}

// specialBits is credentials_special_bits: open group, the setuid/setgid
// fchmod and its fstat, the clearing fchown and its fstat, close.
func (r *t1Replay) specialBits(leaf guestbridge.Capability) {
	group, description := r.open(0, leaf, "group", "READWRITE")
	r.chmod("chmod-special-bits", group, "group", 06750)
	r.fstat(group, "group")
	r.chmod("chown-clears-special-bits", group, "group", 0750)
	r.fstat(group, "group")
	r.close(0, description, "group")
}

// body replays credentials() after admission: the case leaf, its fchmod,
// the four stages and the leaf's close.
func (r *t1Replay) body() {
	root := r.m.mounts[0].root
	r.mkdir(root, "credentials", 0777)
	leaf, leafDescription := r.open(0, root, "credentials", "READ")
	r.chmod("fchmod-leaf", leaf, "credentials", 0777)
	r.groupFile(leaf, "group")
	r.groupFile(leaf, "group-tail")
	r.setgid(leaf, r.m.mounts[1].root)
	r.sticky(leaf)
	r.specialBits(leaf)
	r.close(0, leafDescription, "credentials")
}
