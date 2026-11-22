package guestviewmodel

import "testing"

// TestCachelessRules holds both rules to the guest states T1 produces
// (guest.py's mount roots, its two root fds and the case leaf fd stay
// registered, never mmapped) and to the states that must withhold advance
// under either rule: an mmapped inode, page-cache, dirty or grant state, an
// entered view and a published observation.
func TestCachelessRules(t *testing.T) {
	cases := []struct {
		name     string
		build    func(v *View)
		tj4, r20 bool
		advance  bool // tj4 rule with the advance flag on
	}{
		{"mount root only", func(v *View) {}, true, false, true},
		{"root fds and leaf fd held", func(v *View) { v.Register("m0-root-fd"); v.Register("leaf") }, true, false, true},
		{"mmapped inode", func(v *View) { v.Register("file"); v.Mmap("file") }, false, false, false},
		{"mmapped inode evicted", func(v *View) { v.Register("file"); v.Mmap("file"); v.Evict("file") }, true, false, true},
		{"cached page", func(v *View) { v.Cached = 1 }, false, false, false},
		{"dirty page", func(v *View) { v.Dirty = 1 }, false, false, false},
		{"dirty package", func(v *View) { v.DirtyPackages = 1 }, false, false, false},
		{"grant", func(v *View) { v.Grants = 1 }, false, false, false},
		{"entered", func(v *View) { v.Active = 1 }, false, false, false},
		{"published observation", func(v *View) { v.Observations = 1 }, false, false, false},
		{"root evicted (no live mount)", func(v *View) { v.Evict("root") }, true, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := New("root")
			c.build(v)
			if got := Cacheless(v); got != c.tj4 {
				t.Errorf("Cacheless = %v, want %v", got, c.tj4)
			}
			if got := CachelessR20(v); got != c.r20 {
				t.Errorf("CachelessR20 = %v, want %v", got, c.r20)
			}
			if got := v.Advance(Cacheless, true); got != c.advance {
				t.Errorf("Advance(tj4, on) = %v, want %v", got, c.advance)
			}
			if v.Advance(Cacheless, false) || v.Advance(CachelessR20, false) {
				t.Error("advance sent with the flag off")
			}
		})
	}
}

// TestMmapUnregisteredPanics keeps nr_mmapped <= nr_mappings in the model.
func TestMmapUnregisteredPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Mmap of an unregistered inode did not panic")
		}
	}()
	New("root").Mmap("file")
}
