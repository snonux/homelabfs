package viewcontrol

import (
	"encoding/hex"
	"strings"

	"github.com/snonux/homelabfs/internal/guestbridge"
)

// Syscall layer of the zq3 workload model: each method is one guest syscall
// mapped to the CALLs the guest kernel issues for it (zq3_workload_model_test.go
// gives the mapping rules and their accuracy).

func flags(access string, create bool) map[string]any {
	return map[string]any{"access": access, "append": false, "create": create, "exclusive": create, "sync": "ORDINARY", "truncate": false}
}

func (g *zq3Guest) getattr(p *zq3Proc, object guestbridge.Capability) result {
	return g.call(p, "GETATTR", map[string]any{"object": object})
}

// invalidate unhashes a cached dentry before a fresh LOOKUP of its name.
func (g *zq3Guest) invalidate(dir guestbridge.Capability, n string) {
	key := dentryKey(dir, n)
	if e := g.dcache[key]; e != nil {
		delete(g.dcache, key)
		e.dead = true
		g.evict(e)
	}
}

// evict FORGETs a dead dentry's grant once no open file pins it. The
// FORGET carries the lookup's original issuer, as the guest's forget record.
func (g *zq3Guest) evict(e *zq3Dentry) {
	if e.dead && e.pins == 0 {
		g.call(e.issuer, "FORGET", map[string]any{"object": e.cap, "count": 1})
	}
}

// walk is one path component: GETATTR of dir, then a fresh LOOKUP.
func (g *zq3Guest) walk(p *zq3Proc, dir guestbridge.Capability, n string) guestbridge.Capability {
	g.getattr(p, dir)
	g.invalidate(dir, n)
	res := g.call(p, "LOOKUP", map[string]any{"parent": dir, "name": name(n)})
	e := &zq3Dentry{cap: res.Capabilities[0], issuer: p}
	g.dcache[dentryKey(dir, n)] = e
	return e.cap
}

// walkPath walks a slash-separated relative path below dir.
func (g *zq3Guest) walkPath(p *zq3Proc, dir guestbridge.Capability, path string) (guestbridge.Capability, guestbridge.Capability, string) {
	parts := strings.Split(path, "/")
	for _, part := range parts[:len(parts)-1] {
		dir = g.walk(p, dir, part)
	}
	last := parts[len(parts)-1]
	return dir, g.walk(p, dir, last), last
}

// missing is the walk of a name that must not exist yet (create paths).
func (g *zq3Guest) missing(p *zq3Proc, dir guestbridge.Capability, n string) {
	g.getattr(p, dir)
	g.invalidate(dir, n)
	g.call(p, "LOOKUP", map[string]any{"parent": dir, "name": name(n)}, "NotFound")
}

// stat is fstatat(dir, path, AT_SYMLINK_NOFOLLOW): the walk plus GETATTR.
func (g *zq3Guest) stat(p *zq3Proc, dir guestbridge.Capability, path string) {
	_, object, _ := g.walkPath(p, dir, path)
	g.getattr(p, object)
}

// openRoot opens the mount root itself (mountpoint crossing, no LOOKUP).
func (g *zq3Guest) openRoot(p *zq3Proc) *zq3File {
	g.getattr(p, g.root)
	res := g.call(p, "OPEN", map[string]any{"object": g.root, "flags": flags("READ", false)})
	return &zq3File{obj: g.root, desc: res.Capabilities[0], opener: p}
}

// open opens an existing entry: walk, may_open GETATTR, OPEN. A denied
// open ends after its GETATTR with Permission from the bridge's search check
// or the service, as the kernel's EACCES.
func (g *zq3Guest) open(p *zq3Proc, dir guestbridge.Capability, path, access string) *zq3File {
	parent, object, last := g.walkPath(p, dir, path)
	g.getattr(p, object)
	res := g.call(p, "OPEN", map[string]any{"object": object, "flags": flags(access, false)})
	e := g.dcache[dentryKey(parent, last)]
	e.pins++
	return &zq3File{obj: object, desc: res.Capabilities[0], opener: p, dentry: e}
}

// denied is a syscall that the guest's own permission check refuses after
// refreshing the target (open of a file without access, or a sticky-bit
// unlink, refreshed through its parent): the walk and the GETATTR reach the
// host, the operation itself never does.
func (g *zq3Guest) denied(p *zq3Proc, dir guestbridge.Capability, path string, parent bool) {
	dir, object, _ := g.walkPath(p, dir, path)
	if parent {
		object = dir
	}
	g.getattr(p, object)
}

// create is open(O_CREAT|O_EXCL): GETATTR, LOOKUP NotFound, GETATTR, CREATE.
func (g *zq3Guest) create(p *zq3Proc, dir guestbridge.Capability, n string, mode, umask int) *zq3File {
	g.missing(p, dir, n)
	g.getattr(p, dir)
	res := g.call(p, "CREATE", map[string]any{"parent": dir, "name": name(n), "mode": mode, "umask": umask, "flags": flags("READWRITE", true)})
	e := &zq3Dentry{cap: res.Capabilities[0], issuer: p, pins: 1}
	g.dcache[dentryKey(dir, n)] = e
	return &zq3File{obj: res.Capabilities[0], desc: res.Capabilities[1], opener: p, dentry: e}
}

// mkdir is GETATTR, LOOKUP NotFound, GETATTR, MKDIR; the new dentry stays
// cached (unpinned) until the name is looked up again or removed.
func (g *zq3Guest) mkdir(p *zq3Proc, dir guestbridge.Capability, n string, mode int) guestbridge.Capability {
	g.missing(p, dir, n)
	g.getattr(p, dir)
	res := g.call(p, "MKDIR", map[string]any{"parent": dir, "name": name(n), "mode": mode, "umask": 0})
	g.dcache[dentryKey(dir, n)] = &zq3Dentry{cap: res.Capabilities[0], issuer: p}
	return res.Capabilities[0]
}

// close is flush (DESCRIPTION_CLOSE by the closing process) plus the last
// fput (DESCRIPTION_RELEASE by the opener); a dead dentry is then evicted.
func (g *zq3Guest) close(p *zq3Proc, f *zq3File) {
	g.closes++
	g.call(p, "DESCRIPTION_CLOSE", map[string]any{"description": f.desc, "process": p.cred.Process, "close_sequence": g.closes})
	g.releases++
	g.call(f.opener, "DESCRIPTION_RELEASE", map[string]any{"description": f.desc, "release_sequence": g.releases})
	if f.dentry != nil {
		f.dentry.pins--
		g.evict(f.dentry)
	}
}

// closeDup is the close of a dup'ed descriptor (Python's listdir(fd)): a
// flush only; the description stays open.
func (g *zq3Guest) closeDup(p *zq3Proc, f *zq3File) {
	g.closes++
	g.call(p, "DESCRIPTION_CLOSE", map[string]any{"description": f.desc, "process": p.cred.Process, "close_sequence": g.closes})
}

func (g *zq3Guest) setattr(p *zq3Proc, f *zq3File, mode, owner, group any) {
	attributes := map[string]any{"mode": mode, "uid": owner, "gid": group, "atime": nil, "mtime": nil, "atime_now": false, "mtime_now": false}
	g.call(p, "SETATTR", map[string]any{"object": f.obj, "attributes": attributes})
}

// write is one pwrite of at most 64 KiB (HFS_IO_MAX); larger ones split.
func (g *zq3Guest) write(p *zq3Proc, f *zq3File, data []byte, offset int) {
	for len(data) > 0 {
		chunk := data[:min(len(data), 65536)]
		g.call(p, "WRITE", map[string]any{"description": f.desc, "offset": offset, "data": hex.EncodeToString(chunk), "max_end": 1 << 40})
		data, offset = data[len(chunk):], offset+len(chunk)
	}
}

func (g *zq3Guest) read(p *zq3Proc, f *zq3File, offset, count int) {
	g.call(p, "READ", map[string]any{"description": f.desc, "offset": offset, "count": count})
}

// fsync is SYNC; scope DATA (fdatasync), FILE (fsync) or DIRECTORY.
func (g *zq3Guest) fsync(p *zq3Proc, f *zq3File, scope string, want ...string) {
	g.call(p, "SYNC", map[string]any{"object": f.obj, "description": f.desc, "scope": scope}, want...)
}

func (g *zq3Guest) truncate(p *zq3Proc, f *zq3File, size int, want ...string) {
	times := map[string]any{"atime": nil, "mtime": nil, "ctime": nil, "atime_now": false, "mtime_now": false}
	g.call(p, "TRUNCATE", map[string]any{"object": f.obj, "description": f.desc, "size": size, "times": times}, want...)
}

// remove is unlinkat/rmdir: the walk, GETATTR of the parent (may_delete),
// UNLINK or RMDIR; the victim's inode then FORGETs its grant. A refused
// removal (sticky bit) keeps the dentry.
func (g *zq3Guest) remove(p *zq3Proc, dir guestbridge.Capability, path string, rmdir bool, want ...string) {
	parent, _, last := g.walkPath(p, dir, path)
	g.getattr(p, parent)
	op := "UNLINK"
	if rmdir {
		op = "RMDIR"
	}
	res := g.call(p, op, map[string]any{"parent": parent, "name": name(last)}, want...)
	if res.Code == "OK" {
		g.invalidate(parent, last)
	}
}

// rename is renameat in one directory: walk source, LOOKUP NotFound of the
// target, GETATTRs of the parent for may_delete and may_create, RENAME.
func (g *zq3Guest) rename(p *zq3Proc, dir guestbridge.Capability, from, to string) {
	g.walk(p, dir, from)
	g.missing(p, dir, to)
	g.getattr(p, dir)
	g.getattr(p, dir)
	g.call(p, "RENAME", map[string]any{"source_parent": dir, "source_name": name(from), "destination_parent": dir, "destination_name": name(to), "no_replace": false})
	if e := g.dcache[dentryKey(dir, from)]; e != nil {
		delete(g.dcache, dentryKey(dir, from))
		g.dcache[dentryKey(dir, to)] = e
	}
}

// link is linkat in one directory: walk source, LOOKUP NotFound, GETATTR, LINK.
func (g *zq3Guest) link(p *zq3Proc, dir guestbridge.Capability, from, to string) {
	object := g.walk(p, dir, from)
	g.missing(p, dir, to)
	g.getattr(p, dir)
	g.call(p, "LINK", map[string]any{"object": object, "parent": dir, "name": name(to)})
}

// listdir is getdents on f: one READDIR page plus the end-of-directory page
// (an extra GETATTR here); each entry's transient grant is FORGOTTEN, which
// stands in for the terminal tuple's single READDIR_CLEANUP.
func (g *zq3Guest) listdir(p *zq3Proc, f *zq3File) int {
	res := g.call(p, "READDIR", map[string]any{"object": f.obj, "after": "", "revision": 0, "limit": 64})
	g.getattr(p, f.obj)
	for _, entry := range res.Entries {
		if entry.Capability.Token != "" {
			g.call(p, "FORGET", map[string]any{"object": entry.Capability, "count": 1})
		}
	}
	return len(res.Entries)
}

// dropCaches is the quiesce's drop_caches: every unpinned cached dentry is
// evicted (deepest first, as dentries go before their parents).
func (g *zq3Guest) dropCaches() {
	keys := sortedKeys(g.dcache)
	for i := len(keys) - 1; i >= 0; i-- {
		if e := g.dcache[keys[i]]; e.pins == 0 {
			delete(g.dcache, keys[i])
			e.dead = true
			g.evict(e)
		}
	}
}
