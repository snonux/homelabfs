package guestbridge

import "testing"

// Hex-encoded raw names used by the sj4 tests below.
const (
	sj4Leaf  = "6c656166"   // "leaf"
	sj4Group = "67726f7570" // "group"
	sj4Moved = "6d6f766564" // "moved"
)

// sj4Tree is the zq3 T1 shape: a 0700 volume root owned by uid 0, a 0770
// leaf directory and a 0640 file "group" in it, both of group 42002.
type sj4Tree struct {
	top, leaf, file any
	root            Credentials
}

// sj4Setattr changes only the given mode, uid and gid (nil keeps a field).
func sj4Setattr(mode, uid, gid any) map[string]any {
	return map[string]any{"mode": mode, "uid": uid, "gid": gid, "atime": nil, "mtime": nil, "atime_now": false, "mtime_now": false}
}

// newSJ4Tree builds sj4Tree as uid 0, closing the volume root last so the
// later non-owner calls run strictly below a root they cannot search.
func newSJ4Tree(t *testing.T, f *bridgeFixture) sj4Tree {
	t.Helper()
	cred := f.cred()
	cred.FSUID = 0
	_, root := f.call(t, cred, "ROOT", map[string]any{})
	tree := sj4Tree{top: capabilityAt(t, root, 0), root: cred}
	_, made := f.call(t, cred, "MKDIR", map[string]any{"parent": tree.top, "name": sj4Leaf, "mode": 0o770, "umask": 0})
	if made["code"] != "OK" {
		t.Fatal("mkdir:", made)
	}
	tree.leaf = capabilityAt(t, made, 0)
	flags := map[string]any{"access": "READWRITE", "append": false, "create": true, "exclusive": true, "sync": "ORDINARY", "truncate": false}
	_, created := f.call(t, cred, "CREATE", map[string]any{"parent": tree.leaf, "name": sj4Group, "mode": 0o640, "umask": 0, "flags": flags})
	if created["code"] != "OK" {
		t.Fatal("create:", created)
	}
	tree.file = capabilityAt(t, created, 0)
	for _, step := range []struct {
		object any
		attrs  map[string]any
	}{{tree.leaf, sj4Setattr(nil, nil, 42002)}, {tree.file, sj4Setattr(nil, nil, 42002)}, {tree.top, sj4Setattr(0o700, 0, nil)}} {
		if _, r := f.call(t, cred, "SETATTR", map[string]any{"object": step.object, "attributes": step.attrs}); r["code"] != "OK" {
			t.Fatal("setattr:", r)
		}
	}
	return tree
}

// sj4Member is the T1 access child: not the owner of anything, but a
// supplementary member of group 42002.
func sj4Member(f *bridgeFixture, revision uint64, groups ...uint32) Credentials {
	cred := f.cred()
	cred.FSUID, cred.FSGID, cred.Groups, cred.Revision = 41001, 42001, groups, revision
	return cred
}

var sj4Read = map[string]any{"access": "READ", "append": false, "create": false, "exclusive": false, "sync": "ORDINARY", "truncate": false}

// TestNonOwnerBelowClosedRootUsesImmediateChecks pins sj4 (zq3-r20 R20-1):
// a path grant's re-walk no longer requires search on every recorded
// ancestor. Before sj4 the GETATTR of the leaf below the 0700 root was
// Permission, so the group check never ran. Now GETATTR, LOOKUP and OPEN
// succeed through group 42002, while the immediate checks fsservice keeps
// (search on the LOOKUP parent, read on the OPEN file) still refuse a caller
// without that group.
func TestNonOwnerBelowClosedRootUsesImmediateChecks(t *testing.T) {
	f := newBridge(t)
	f.hello(t)
	tree := newSJ4Tree(t, f)
	member := sj4Member(f, 2, 42001, 42002, 42003)
	if _, r := f.call(t, member, "GETATTR", map[string]any{"object": tree.leaf}); r["code"] != "OK" {
		t.Fatal("member getattr of leaf:", r)
	}
	_, looked := f.call(t, member, "LOOKUP", map[string]any{"parent": tree.leaf, "name": sj4Group})
	if looked["code"] != "OK" {
		t.Fatal("member lookup:", looked)
	}
	file := capabilityAt(t, looked, 0)
	if _, r := f.call(t, member, "OPEN", map[string]any{"object": file, "flags": sj4Read}); r["code"] != "OK" {
		t.Fatal("member open:", r)
	}
	outsider := sj4Member(f, 3, 42001, 42003)
	if _, r := f.call(t, outsider, "OPEN", map[string]any{"object": file, "flags": sj4Read}); r["code"] != "Permission" {
		t.Fatal("wrong-groups open:", r)
	}
	if _, r := f.call(t, outsider, "LOOKUP", map[string]any{"parent": tree.leaf, "name": sj4Group}); r["code"] != "Permission" {
		t.Fatal("wrong-groups lookup:", r)
	}
}

// TestNonOwnerBelowClosedRootStillSeesStaleAncestry: dropping the ancestor
// search check keeps the identity re-walk, so a renamed or removed ancestor
// is still Stale for the non-owner, never OK and never Permission.
func TestNonOwnerBelowClosedRootStillSeesStaleAncestry(t *testing.T) {
	for _, change := range []string{"ANCESTOR_RENAME", "ANCESTOR_RMDIR"} {
		t.Run(change, func(t *testing.T) {
			f := newBridge(t)
			f.hello(t)
			tree := newSJ4Tree(t, f)
			steps := [][2]any{{"RENAME", map[string]any{"source_parent": tree.top, "source_name": sj4Leaf, "destination_parent": tree.top, "destination_name": sj4Moved, "no_replace": false}}}
			if change == "ANCESTOR_RMDIR" {
				steps = [][2]any{{"UNLINK", map[string]any{"parent": tree.leaf, "name": sj4Group}}, {"RMDIR", map[string]any{"parent": tree.top, "name": sj4Leaf}}}
			}
			for _, step := range steps {
				if _, r := f.call(t, tree.root, step[0].(string), step[1].(map[string]any)); r["code"] != "OK" {
					t.Fatal(step[0], r)
				}
			}
			member := sj4Member(f, 2, 42001, 42002, 42003)
			for name, object := range map[string]any{"leaf": tree.leaf, "file": tree.file} {
				if _, r := f.call(t, member, "GETATTR", map[string]any{"object": object}); r["code"] != "Stale" {
					t.Fatal("getattr of", name, r)
				}
			}
			if _, r := f.call(t, member, "LOOKUP", map[string]any{"parent": tree.leaf, "name": sj4Group}); r["code"] != "Stale" {
				t.Fatal("lookup under vanished leaf:", r)
			}
		})
	}
}
