// Package guestviewmodel models the guest kernel's view state that decides
// whether an observation's VIEW_ACQUIRE carries "advance": true (task xf4,
// docs/design/xf4-view-advance.md). It exists for the host-side replays
// (internal/controlledruntime's T1 replay, internal/viewcontrol's
// advanceReplay) and is used only by tests.
//
// Why (task uj4, zq3-r20): the replays sent advance whenever their flag was
// on and reported 37 advances, while the real guest sent none, because its
// cacheless test counted every registered inode, the mount root included.
// The replays must therefore ask the same question the guest asks:
// hfs_view_capture_ex samples hfs_view_cacheless_locked (patch 0004) and
// hfs_mmap_observe_lease requests advance only when it held. Rule is that
// predicate; Cacheless mirrors tj4's version, CachelessR20 the r20 one, so a
// test can show that a guest which never sends advance fails the replay.
package guestviewmodel

// View is the part of one mount's struct hfs_view the cacheless test reads.
// Inodes are keyed by the caller (an object's allocation, say); the
// counters stand for the guest's page cache, dirty and grant state, which
// only the mmap fault path creates (hfs_mmap_ops), and for in-flight view
// entries and published observations.
type View struct {
	registered map[string]bool // view->mappings / nr_mappings: every live inode
	mmapped    map[string]bool // tj4's per-inode mark / nr_mmapped
	// Cached, Dirty, DirtyPackages and Grants are nr_cached, nr_dirty,
	// nr_dirty_packages and nr_grants (cache and dirty lists included).
	Cached, Dirty, DirtyPackages, Grants int
	// Active is view->active (hfs_view_enter until hfs_view_leave);
	// Observations is nr_observations (hfs_mmap_publish until hfs_mmap_end).
	Active, Observations int
}

// Rule is a guest-side cacheless predicate: true lets the guest's next
// observation request an advance.
type Rule func(*View) bool

// New returns the view of a fresh mount: hfs_inode_get registers the mount
// root, which stays registered (never mmapped) as long as the mount lives.
func New(root string) *View {
	v := &View{registered: map[string]bool{}, mmapped: map[string]bool{}}
	v.Register(root)
	return v
}

// Register records an inode the guest instantiated (hfs_inode_get calls
// hfs_view_register_inode for every inode, mmapped or not).
func (v *View) Register(inode string) { v.registered[inode] = true }

// Evict drops an inode at eviction (its FORGET): it leaves view->mappings
// and, when it was mmapped, nr_mmapped (hfs_view_unregister_inode).
func (v *View) Evict(inode string) {
	delete(v.registered, inode)
	delete(v.mmapped, inode)
}

// Mmap marks a registered inode mmapped (hfs_view_mmap_inode from
// hfs_mmap_file). The mark is sticky until eviction, as in the guest.
// Mmapping an unregistered inode is a model bug and panics: the guest marks
// only inodes on view->mappings, so nr_mmapped <= nr_mappings.
func (v *View) Mmap(inode string) {
	if !v.registered[inode] {
		panic("guestviewmodel: mmap of an unregistered inode " + inode)
	}
	v.mmapped[inode] = true
}

// Registered is nr_mappings; Mmapped is nr_mmapped.
func (v *View) Registered() int { return len(v.registered) }

// Mmapped is nr_mmapped.
func (v *View) Mmapped() int { return len(v.mmapped) }

// Advance reports whether an observation acquired now carries "advance":
// true: the caller's guest supports and enables advance (flag) and rule
// holds for the view, sampled before the acquisition as
// hfs_view_capture_ex does. The guest's per-call no_advance latch after a
// refused advance is not modelled; the replays never see a refusal.
func (v *View) Advance(rule Rule, flag bool) bool { return flag && rule(v) }

// targetFree is the part both rules share: no page-cache, dirty or grant
// state, nothing entered and no published observation.
func (v *View) targetFree() bool {
	return v.Cached == 0 && v.Dirty == 0 && v.DirtyPackages == 0 && v.Grants == 0 &&
		v.Active == 0 && v.Observations == 0
}

// Cacheless is tj4's hfs_view_cacheless_locked: registered inodes (the
// mount root, the inodes behind held fds and cached dentries) do not count,
// only mmapped ones, because a never-mmapped inode holds nothing bound to
// the target (read and write go over the bridge, and only the mmap fault
// path inserts pages or takes PAGE grants).
func Cacheless(v *View) bool { return v.targetFree() && v.Mmapped() == 0 }

// CachelessR20 is the predicate zq3-r20 ran (before tj4): any registered
// inode makes the view non-cacheless, so with the mount root always
// registered it never holds on a live mount and advance is never sent.
// Kept as the mutant the replays must detect.
func CachelessR20(v *View) bool { return v.targetFree() && v.Registered() == 0 }
