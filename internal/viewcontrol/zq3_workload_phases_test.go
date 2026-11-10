package viewcontrol

import (
	"fmt"
	"testing"
)

// Phases of one zq3 run as the guest tools drive them (task o64). Each
// function follows its tool's syscalls in order: guest.py for T1-T5 and their
// releases (tools/lab/retained9p/guest.py), bootstrap_remote.py guest-stat for
// the k64 usable gate, guest-lock-probe.py for T6, pod_workload.py for T7 and
// guest.py's lifecycle steps for T9-T11. Lock syscalls are guest-local (hfs
// has no lock operation), so they cost no CALL.

const zq3Run = "zq3-zq3-r8-"

var t1Groups = []uint32{42001, 42002, 42003}

// fullGroups is T1's complete 1024-entry group vector ([42001, 43000..44022]).
func fullGroups(n int) []uint32 {
	groups := []uint32{42001}
	for g := uint32(43000); len(groups) < n; g++ {
		groups = append(groups, g)
	}
	return groups
}

// guestPy is one guest.py request process: admission opens and fstats both
// mount roots, the body runs, the roots close and the process exits.
func (w *zq3World) guestPy(body func(p *zq3Proc, roots [2]*zq3File)) {
	p := w.spawn(0, 0, nil)
	var roots [2]*zq3File
	for d, g := range w.g {
		roots[d] = g.openRoot(p)
		g.getattr(p, g.root)
	}
	body(p, roots)
	for d, g := range w.g {
		g.close(p, roots[d])
	}
	w.exit(p)
}

// child is guest.py's forked credential child: its own process, one action.
func (w *zq3World) child(uid, gid uint32, groups []uint32, action func(c *zq3Proc)) {
	c := w.spawn(uid, gid, groups)
	action(c)
	w.exit(c)
}

// inventory is Retention.inventory: open_leaf, lstat of every payload entry,
// fstat, listdir(fd) (a dup'ed description) and close.
func (w *zq3World) inventory(p *zq3Proc, leaf string, entries []string) {
	m0 := w.g[0]
	fd := m0.open(p, m0.root, leaf, "READ")
	for _, entry := range entries {
		m0.stat(p, fd.obj, entry)
	}
	m0.getattr(p, fd.obj)
	m0.listdir(p, fd)
	m0.closeDup(p, fd)
	m0.close(p, fd)
}

// guestCase runs one T1-T5 case process and then its release process.
func (w *zq3World) guestCase(name string, dirs, files []string, body func(p *zq3Proc, leaf string, d *zq3File)) {
	leaf := zq3Run + name
	entries := append(append([]string{}, dirs...), files...)
	w.begin("T " + name)
	w.guestPy(func(p *zq3Proc, roots [2]*zq3File) {
		m0 := w.g[0]
		m0.mkdir(p, m0.root, leaf, 0777)
		d := m0.open(p, m0.root, leaf, "READ")
		body(p, leaf, d)
		m0.close(p, d)
		w.inventory(p, leaf, entries)
	})
	w.begin("T " + name + " release")
	w.guestPy(func(p *zq3Proc, roots [2]*zq3File) {
		m0 := w.g[0]
		w.inventory(p, leaf, entries)
		fd := m0.open(p, m0.root, leaf, "READ")
		for _, f := range files {
			m0.remove(p, fd.obj, f, false)
		}
		for _, d := range dirs {
			m0.remove(p, fd.obj, d, true)
		}
		m0.close(p, fd)
		m0.remove(p, m0.root, leaf, true)
	})
}

// bootstrap: mount(8) exits after each mount (its ROOT is claim 1), then the
// k64 usable gate lstats both roots twice, each from its own child process.
func (w *zq3World) bootstrap() {
	w.begin("bootstrap mount exits")
	for d := range 2 {
		w.exit(w.mounts[d])
	}
	w.begin("k64 usable gate x2")
	for range 2 {
		for _, g := range w.g {
			w.child(0, 0, nil, func(c *zq3Proc) { g.getattr(c, g.root) })
		}
	}
}

// credentials is T1 (guest.py credentials()).
func (w *zq3World) credentials() {
	m0, m1 := w.g[0], w.g[1]
	w.guestCase("credentials", []string{"setgid", "sticky"}, []string{"setgid/created", "group", "group-tail", "sticky/owned"}, func(p *zq3Proc, leaf string, d *zq3File) {
		m0.setattr(p, d, 0777, nil, nil)
		w.groupFile(p, d, "group", 42002, []uint32{42001, 42003}, t1Groups)
		full := fullGroups(1024)
		w.groupFile(p, d, "group-tail", 44022, full[:1023], full)
		w.setgidCreate(p, d)
		m1.stat(p, m1.root, leaf+"/setgid/created")
		w.stickyDenial(p, d)
		owner := m0.open(p, d.obj, "group", "READWRITE")
		m0.setattr(p, owner, 06750, nil, nil)
		m0.getattr(p, owner.obj)
		m0.setattr(p, owner, nil, 41001, 42001)
		m0.getattr(p, owner.obj)
		m0.close(p, owner)
	})
}

// groupFile creates a group-readable file and checks it from two children:
// one with the group (reads it) and one without (refused locally).
func (w *zq3World) groupFile(p *zq3Proc, d *zq3File, file string, gid uint32, without, with []uint32) {
	m0 := w.g[0]
	f := m0.create(p, d.obj, file, 0640, 022)
	m0.setattr(p, f, nil, 41002, gid)
	m0.setattr(p, f, 0640, nil, nil)
	m0.write(p, f, []byte(file+"-secret"), 0)
	m0.close(p, f)
	w.child(41001, 42001, with, func(c *zq3Proc) {
		fd := m0.open(c, d.obj, file, "READ")
		m0.read(c, fd, 0, 64)
		m0.close(c, fd)
	})
	w.child(41001, 42001, without, func(c *zq3Proc) { m0.denied(c, d.obj, file, false) })
}

func (w *zq3World) setgidCreate(p *zq3Proc, d *zq3File) {
	m0 := w.g[0]
	m0.mkdir(p, d.obj, "setgid", 02777)
	sg := m0.open(p, d.obj, "setgid", "READ")
	m0.setattr(p, sg, nil, 0, 42002)
	m0.setattr(p, sg, 02777, nil, nil)
	w.child(41001, 42001, []uint32{42001, 42002}, func(c *zq3Proc) {
		f := m0.create(c, sg.obj, "created", 0666, 027)
		m0.getattr(c, f.obj)
		m0.close(c, f)
	})
	m0.close(p, sg)
}

func (w *zq3World) stickyDenial(p *zq3Proc, d *zq3File) {
	m0 := w.g[0]
	m0.mkdir(p, d.obj, "sticky", 01777)
	st := m0.open(p, d.obj, "sticky", "READ")
	m0.setattr(p, st, 01777, nil, nil)
	owned := m0.create(p, st.obj, "owned", 0666, 022)
	m0.setattr(p, owned, nil, 41002, 42002)
	m0.close(p, owned)
	w.child(41001, 42001, []uint32{42001, 42002}, func(c *zq3Proc) { m0.denied(c, st.obj, "owned", true) })
	m0.close(p, st)
}

// sync is T2 (guest.py sync()); its one mapped page is the two-domain recall.
func (w *zq3World) sync() {
	m0 := w.g[0]
	w.guestCase("sync", nil, []string{"ordinary", "sync", "dsync", "mapped"}, func(p *zq3Proc, leaf string, d *zq3File) {
		for _, file := range []string{"ordinary", "sync", "dsync"} {
			f := m0.create(p, d.obj, file, 0600, 022)
			m0.write(p, f, []byte(file+"-receipt"), 0)
			m0.fsync(p, f, "DATA")
			m0.fsync(p, f, "FILE")
			m0.read(p, f, 0, 64)
			m0.close(p, f)
		}
		m0.fsync(p, d, "DIRECTORY")
		f := m0.create(p, d.obj, "mapped", 0600, 022)
		m0.truncate(p, f, 4096)
		w.mappedRecall(p, leaf, f)
		m0.fsync(p, f, "FILE")
		m0.read(p, f, 0, 17)
		m0.close(p, f)
	})
}

// mappedRecall stands in for T2's MAP_SHARED page: an EXCLUSIVE_DIRTY lease
// on m0, a conflicting READ from m1 and the clean two-domain recall. The view
// tickets carry the lease holders' credentials.
func (w *zq3World) mappedRecall(p *zq3Proc, leaf string, f *zq3File) {
	m1 := w.g[1]
	reader := w.spawn(0, 0, nil)
	other := m1.open(reader, m1.root, leaf+"/mapped", "READ")
	w.r.credentials[0], w.r.credentials[1] = p.cred, reader.cred
	if _, dirty := w.r.acquire(0, page(f.desc, 0), "EXCLUSIVE_DIRTY"); dirty.Status != "GRANTED" {
		w.r.t.Fatalf("T2 write lease: %+v", dirty)
	}
	if _, pending := w.r.acquire(1, page(other.desc, 0), "READ"); pending.Status != "PENDING" {
		w.r.t.Fatalf("T2 conflicting READ was not parked: %+v", pending)
	}
	w.r.completeCleanRecall()
	m1.close(reader, other)
	w.exit(reader)
}

// metadata is T3 (guest.py metadata()).
func (w *zq3World) metadata() {
	m0, m1 := w.g[0], w.g[1]
	w.guestCase("metadata", nil, []string{"renamed", "linked"}, func(p *zq3Proc, leaf string, d *zq3File) {
		f := m0.create(p, d.obj, "original", 0600, 022)
		m0.write(p, f, []byte("metadata-receipt"), 0)
		m0.rename(p, d.obj, "original", "renamed")
		m0.link(p, d.obj, "renamed", "linked")
		// The open file's dentry was renamed but keeps its pre-rename
		// capability (hfs_rename installs no new one, hfs_setattr writes the
		// dentry's), so the host refuses this ftruncate as Stale: guest.py's
		// "truncate" step would fail T3 here. Not a budget wall; the model
		// counts the CALL and continues so the later budgets are measured.
		m0.truncate(p, f, 8, "Stale")
		m1.stat(p, m1.root, leaf+"/linked")
		m0.fsync(p, d, "DIRECTORY")
		m0.close(p, f)
		u := m0.create(p, d.obj, "unlinked", 0600, 022)
		m0.remove(p, d.obj, "unlinked", false)
		m0.write(p, u, []byte("retained"), 0)
		// Same for fsync of the open-unlinked file: SYNC names the inode's
		// object capability, whose ancestry ended with the unlink.
		m0.fsync(p, u, "FILE", "Stale")
		m0.read(p, u, 0, 8)
		m0.close(p, u)
	})
}

// namespace is T4: the child in a new user namespace is refused by the
// guest before any CALL (foreign namespace), after its walk.
func (w *zq3World) namespace() {
	m0 := w.g[0]
	w.guestCase("namespace", nil, []string{"namespace"}, func(p *zq3Proc, leaf string, d *zq3File) {
		m0.close(p, m0.create(p, d.obj, "namespace", 0666, 022))
		w.child(0, 0, nil, func(c *zq3Proc) { m0.getattr(c, d.obj) })
	})
}

// mappedPressure is T5 without its mapped dirtying (see the model notes):
// the file, its 4 MiB TRUNCATE, MS_SYNC/fsync and the cross-domain read of
// all 4 MiB through m1 in 64 KiB READs.
func (w *zq3World) mappedPressure() {
	m0, m1 := w.g[0], w.g[1]
	w.guestCase("mapped-pressure", nil, []string{"full"}, func(p *zq3Proc, leaf string, d *zq3File) {
		f := m0.create(p, d.obj, "full", 0600, 022)
		m0.truncate(p, f, 4<<20)
		m0.fsync(p, f, "FILE")
		other := m1.open(p, m1.root, leaf+"/full", "READ")
		for offset := 0; offset < 4<<20; offset += 65536 {
			m1.read(p, other, offset, 65536)
		}
		m1.close(p, other)
		m0.close(p, f)
	})
}

// lockProbe is T6 (guest-lock-probe.py on m0): per lock family a file, the
// holder's opens and a contending child per check; locks are guest-local.
func (w *zq3World) lockProbe() {
	m0 := w.g[0]
	w.begin("T6 lock probe")
	p := w.spawn(0, 0, nil)
	root := m0.openRoot(p)
	leaf := zq3Run + "locks"
	m0.mkdir(p, m0.root, leaf, 0700)
	dir := m0.open(p, m0.root, leaf, "READ")
	for _, family := range []string{"posix", "flock"} {
		f := m0.create(p, dir.obj, family, 0600, 022)
		m0.getattr(p, f.obj)
		for range 4 {
			w.child(0, 0, nil, func(c *zq3Proc) {
				other := m0.open(c, dir.obj, family, "READWRITE")
				m0.getattr(c, other.obj)
				m0.close(c, other)
			})
		}
		m0.close(p, f)
	}
	m0.close(p, dir)
	m0.close(p, root)
	w.exit(p)
}

// podFiles are pod_workload.py's FILES: numeric owner, group and mode.
var podFiles = []struct {
	name       string
	uid, gid   uint32
	mode       int
	allow, not []uint32
}{
	{"group", 41002, 42002, 0640, []uint32{42001, 42002}, []uint32{42001}},
	{"group-tail", 41002, 44022, 0640, fullGroups(1024), fullGroups(1023)},
	{"uid-only", 41001, 41001, 0600, []uint32{41001}, []uint32{42002}},
}

// pod is T7 (pod_workload.py w0/w1 on binds of m0, then the driver's payload
// removal): w0 makes the leaf and the numeric-owner files, runs the six
// access children and the changing-groups child, then holds both lock files
// that w1 opens through its own bind; after w1 exits w0 reopens them.
func (w *zq3World) pod() {
	m0 := w.g[0]
	w.begin("T7 pod fixture")
	w0, w1 := w.spawn(0, 0, nil), w.spawn(0, 0, nil)
	leaf := zq3Run + "pod"
	m0.getattr(w0, m0.root)
	root := m0.openRoot(w0)
	m0.mkdir(w0, m0.root, leaf, 0755)
	dir := m0.open(w0, m0.root, leaf, "READ")
	m0.setattr(w0, dir, 0755, nil, nil)
	for _, f := range podFiles {
		file := m0.create(w0, dir.obj, f.name, 0600, 022)
		m0.write(w0, file, []byte(f.name), 0)
		m0.setattr(w0, file, nil, f.uid, f.gid)
		m0.setattr(w0, file, f.mode, nil, nil)
		m0.fsync(w0, file, "FILE")
		m0.close(w0, file)
	}
	w.podMatrix(dir)
	locks := []*zq3File{m0.create(w0, dir.obj, "lock-posix", 0644, 022), m0.create(w0, dir.obj, "lock-flock", 0644, 022)}
	m0.getattr(w1, m0.root)
	root1 := m0.openRoot(w1)
	dir1 := m0.open(w1, m0.root, leaf, "READ")
	theirs := []*zq3File{m0.open(w1, dir1.obj, "lock-posix", "READWRITE"), m0.open(w1, dir1.obj, "lock-flock", "READWRITE")}
	for i := range locks {
		m0.close(w0, locks[i])
		m0.close(w1, theirs[i])
	}
	m0.close(w1, dir1)
	m0.close(w1, root1)
	w.exit(w1)
	for _, file := range []string{"lock-posix", "lock-flock"} {
		m0.close(w0, m0.open(w0, dir.obj, file, "READWRITE"))
	}
	m0.close(w0, dir)
	m0.close(w0, root)
	w.exit(w0)
	w.podCleanup(leaf)
}

// podMatrix is run_matrix's access children: each allowed one opens and
// reads its file, each denied one is refused after its GETATTR; the
// changing-groups child tries "group" three times (allow, deny, allow).
func (w *zq3World) podMatrix(dir *zq3File) {
	m0 := w.g[0]
	read := func(c *zq3Proc, file string) {
		fd := m0.open(c, dir.obj, file, "READ")
		m0.read(c, fd, 0, 64)
		m0.close(c, fd)
	}
	for _, f := range podFiles {
		w.child(41001, 42001, f.allow, func(c *zq3Proc) { read(c, f.name) })
		w.child(41002, 42002, f.not, func(c *zq3Proc) { m0.denied(c, dir.obj, f.name, false) })
	}
	w.child(41001, 42001, []uint32{42001, 42002}, func(c *zq3Proc) {
		read(c, "group")
		m0.denied(c, dir.obj, "group", false)
		read(c, "group")
	})
}

// podCleanup is the driver's normal payload removal after t7_pass: one guest
// process lists the leaf, unlinks every file and removes the leaf.
func (w *zq3World) podCleanup(leaf string) {
	m0 := w.g[0]
	w.step(func(p *zq3Proc) {
		dir := m0.open(p, m0.root, leaf, "READ")
		m0.listdir(p, dir)
		for _, f := range podFiles {
			m0.remove(p, dir.obj, f.name, false)
		}
		for _, file := range []string{"lock-posix", "lock-flock"} {
			m0.remove(p, dir.obj, file, false)
		}
		m0.close(p, dir)
		m0.remove(p, m0.root, leaf, true)
	})
}

// lifecycle is T9 (one MKDIR in m0, then re-reads through m1 and m0), T10's
// quiesce (syncfs of both roots, drop_caches twice: every unpinned lookup
// FORGETs) and T11 (lstat, listdir and an exclusive create in m1).
func (w *zq3World) lifecycle() {
	m0, m1 := w.g[0], w.g[1]
	w.begin("T9 lost reply")
	leaf := zq3Run + "lost-reply"
	w.step(func(p *zq3Proc) { m0.mkdir(p, m0.root, leaf, 0755) })
	w.step(func(p *zq3Proc) {
		for _, g := range []*zq3Guest{m1, m0} {
			g.stat(p, g.root, leaf)
			root := g.openRoot(p)
			g.listdir(p, root)
			g.close(p, root)
		}
	})
	w.begin("T10 quiesce")
	w.step(func(p *zq3Proc) {
		// syncfs only writes back mapped pages: hfs has no sync_fs, so it
		// sends no CALL; the root's open and close do.
		for _, g := range w.g {
			g.close(p, g.openRoot(p))
		}
		for _, g := range w.g {
			g.dropCaches()
		}
	})
	w.begin("T11 after loss")
	w.step(func(p *zq3Proc) {
		m1.getattr(p, m1.root)
		root := m1.openRoot(p)
		m1.listdir(p, root)
		m1.close(p, root)
		m1.close(p, m1.create(p, m1.root, zq3Run+"after-loss", 0600, 022))
	})
}

// step is one guest.py lifecycle step process (no root fds, no children).
func (w *zq3World) step(body func(p *zq3Proc)) {
	p := w.spawn(0, 0, nil)
	body(p)
	w.exit(p)
}

// unmountM1 is T8's umount of m1: its cached dentries are evicted.
func (w *zq3World) unmountM1() {
	w.begin("T8 umount m1")
	w.g[1].dropCaches()
}

// runMatrix is one full pass of the zq3 gate matrix after bootstrap.
func (w *zq3World) runMatrix() {
	w.credentials()
	w.sync()
	w.metadata()
	w.namespace()
	w.mappedPressure()
	w.lockProbe()
	w.pod()
	w.lifecycle()
}

// TestZQ3RunWorkloadFitsLifetimeBounds drives one zq3 run's CALLs (bootstrap,
// the k64 usable gate, T1-T11 with their releases, a two-domain recall and
// every process exit) through the real provider, bridge and view controller
// at the committed spec's exact Views, Providers and bridge values, with the
// guest retiring as the kernel does (zq3GuestWindow). Every refusal fails it
// at the CALL that hit the wall. It then requires every bound to hold at
// least twice the run's measured need (assertMargins).
//
// Before task o64 it failed at m0's 17th CALL (T1's first GETATTR after the
// leaf mkdir): provider credits were lifetime budgets (16 MiB / 1 MiB source
// credit), and behind them CapabilityIdentities 128, CredentialRevisions 32
// and Capabilities 64 (every READDIR reserves 256) would have refused T1.
func TestZQ3RunWorkloadFitsLifetimeBounds(t *testing.T) {
	if testing.Short() {
		t.Skip("drives about 1300 CALLs through the real daemon stack")
	}
	limits, bounds := zq3SpecLimits(t), zq3SpecBounds(t)
	r := newRigWith(t, limits, func(c *Config) { c.Authorization.Enrollment.Bounds = bounds })
	w := newZQ3World(r, zq3GuestWindow)
	w.bootstrap()
	w.runMatrix()
	w.unmountM1()
	w.report(t)
	w.assertMargins(t, limits, bounds)
}

// zq3GuestWindow is the modeled guest window: the guest retires only when
// its own reserve fails. Budget.Guest's 512 MiB ordinary partition holds
// about 14 request owners (36.6 MB each, measured, guestfloor.go) and the
// control partition about 6 above its fixed lanes, so at most about 20
// unretired requests exist guest-wide; 24 rounds that up. The provider's
// unretired capacity must be at least twice this plus the snapshot owner.
const zq3GuestWindow = 24

// zq3ReaddirLimit is the guest kernel's fixed READDIR limit (hfs_iterate
// sends "limit":256), which the bridge reserves against both grant bounds
// while the READDIR runs (capNeeds).
const zq3ReaddirLimit = 256

// assertMargins requires each bound to hold twice the run's measured need.
// Lifetime bounds (credential rows, the provider's processes bucket, view
// ticket identities and UUIDs) take the whole run's count; window bounds
// (credits, MaxClaims, grant rows) take peaks, because retirement recycles.
func (w *zq3World) assertMargins(t *testing.T, l *rigLimits, b Bounds) {
	t.Helper()
	window := min(int(l.RetainedBytes/l.ClaimCreditBytes), int(l.SourceBytes/l.SourceCreditBytes), l.Claims)
	check := func(name string, need, bound int) {
		t.Helper()
		if 2*need > bound {
			t.Errorf("%s: twice the need %d exceeds the bound %d", name, need, bound)
		}
	}
	for d, g := range w.g {
		s := g.stats
		prefix := fmt.Sprintf("m%d ", d)
		// +1: retirement keeps the session's bridge snapshot owner.
		check(prefix+"unretired claims (provider credits and MaxClaims)", s.peakUnretired+1, window)
		check(prefix+"processes bucket (MaxClaims, lifetime)", s.processes, l.Claims)
		// -1: the terminal tuple keeps one credential slot for its observer.
		check(prefix+"credential rows (CredentialRevisions, lifetime)", s.peakCredentials, l.CredentialRevisions-1)
		check(prefix+"close rows (CredentialRevisions)", s.peakCloses, l.CredentialRevisions)
		check(prefix+"grant rows + READDIR reservation (CapabilityIdentities)", s.peakGrants+zq3ReaddirLimit, l.CapabilityIdentities)
		check(prefix+"live grants + READDIR reservation (Capabilities)", s.peakLive+zq3ReaddirLimit, l.Capabilities)
	}
	identities, uuids, _ := w.viewUsage()
	check("view ticket identities (Views.Identities, lifetime)", identities, b.Identities)
	check("view lease/reservation UUIDs (2 x Views.Identities, lifetime)", uuids, 2*b.Identities)
	check("retained source cuts (Views.Sources)", w.peakSources, b.Sources)
}
