package storage

import (
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/vfs"
)

func TestDirectoryLinkAccountingIndependentLedger(t *testing.T) {
	for _, backend := range []string{"simulator", "os"} {
		t.Run(backend, func(t *testing.T) {
			f := fixtureFor(t, backend)
			baseline := f.image()
			root, left, right, moved, child := f.config.Root, objectID(2), objectID(3), objectID(4), objectID(5)
			var suffix []Operation
			apply := func(op Operation) { f.apply(op); suffix = append(suffix, cloneOperation(op)) }
			apply(f.create("left", fsapi.Directory, left))
			checkDirectoryState(f, root, 3, 2, 1, root, 1)
			apply(f.create("right", fsapi.Directory, right))
			checkDirectoryState(f, root, 4, 3, 1, root, 1)
			apply(f.createAt(left, "moved", fsapi.Directory, moved))
			checkDirectoryState(f, left, 3, 2, 1, root, 1)
			apply(f.createAt(moved, "child", fsapi.Directory, child))
			checkDirectoryState(f, moved, 3, 2, 1, left, 1)
			apply(f.renameBetween(left, "moved", right, "renamed"))
			checkDirectoryState(f, left, 2, 3, 1, root, 1)
			checkDirectoryState(f, right, 3, 2, 1, root, 1)
			checkDirectoryState(f, moved, 3, 3, 2, right, 2)
			checkDirectoryState(f, child, 2, 1, 1, moved, 1)
			reopenDirectoryImage(f, f.image())
			apply(f.unlinkAt(moved, "child"))
			checkDirectoryState(f, moved, 2, 4, 2, right, 2)
			checkDirectoryState(f, child, 0, 2, 2, moved, 1)
			apply(f.unlinkAt(right, "renamed"))
			checkDirectoryState(f, right, 2, 3, 1, root, 1)
			checkDirectoryState(f, moved, 0, 5, 3, right, 2)
			apply(f.unlink("left"))
			checkDirectoryState(f, root, 3, 4, 1, root, 1)
			apply(f.unlink("right"))
			checkDirectoryState(f, root, 2, 5, 1, root, 1)
			final := f.image()
			reopenDirectoryImage(f, final)
			for _, op := range suffix {
				f.apply(op)
			}
			// Replay the complete ordered suffix against its original baseline,
			// including creation, independent count changes and the parent move.
			reopenDirectoryImage(f, baseline)
			for _, op := range suffix {
				f.apply(op)
			}
			if got := f.image(); !reflect.DeepEqual(got, final) {
				t.Fatal("directory replay changed the exported catalog")
			}
		})
	}
}

func TestDirectoryReplacementIndependentLedger(t *testing.T) {
	for _, backend := range []string{"simulator", "os"} {
		for _, crossParent := range []bool{false, true} {
			name := "same-parent"
			if crossParent {
				name = "cross-parent"
			}
			t.Run(backend+"/"+name, func(t *testing.T) {
				f := fixtureFor(t, backend)
				root, parent, source, replaced := f.config.Root, objectID(2), objectID(3), objectID(4)
				f.apply(f.create("parent", fsapi.Directory, parent))
				f.apply(f.create("source", fsapi.Directory, source))
				destination := root
				if crossParent {
					destination = parent
				}
				f.apply(f.createAt(destination, "replaced", fsapi.Directory, replaced))
				f.apply(f.renameBetween(root, "source", destination, "replaced"))
				if crossParent {
					checkDirectoryState(f, root, 3, 4, 1, root, 1)
					checkDirectoryState(f, parent, 3, 3, 1, root, 1)
				} else {
					checkDirectoryState(f, root, 4, 5, 1, root, 1)
				}
				checkDirectoryState(f, source, 2, 2, 2, destination, 2)
				checkDirectoryState(f, replaced, 0, 2, 2, destination, 1)
				reopenDirectoryImage(f, f.image())
			})
		}
	}
}

func TestReferenceEditsRequireLedgerAdvance(t *testing.T) {
	for _, backend := range []string{"simulator", "os"} {
		for _, kind := range []fsapi.Kind{fsapi.Regular, fsapi.Directory, fsapi.Symlink} {
			for _, primitive := range []string{"rename", "remove"} {
				t.Run(backend+"/"+strconv.Itoa(int(kind))+"/"+primitive, func(t *testing.T) {
					f := fixtureFor(t, backend)
					id := objectID(2)
					f.apply(f.create("original", kind, id))
					op := f.unlink("original")
					if primitive == "rename" {
						op = f.rename("original", "renamed")
					}
					for i := range op.Objects {
						if op.Objects[i].After.Attr.Object == id {
							op.Objects[i].After.LedgerRevision = f.get(id).LedgerRevision
						}
					}
					assertRejectedCatalog(f, op, fsapi.Stale)
				})
			}
		}
	}
}

func TestNondirectoryLinkCountGuards(t *testing.T) {
	for _, backend := range []string{"simulator", "os"} {
		for _, kind := range []fsapi.Kind{fsapi.Regular, fsapi.Symlink} {
			for _, advanceLedger := range []bool{false, true} {
				name := "unchanged-ledger"
				if advanceLedger {
					name = "advanced-ledger"
				}
				t.Run(backend+"/"+strconv.Itoa(int(kind))+"/"+name, func(t *testing.T) {
					f := fixtureFor(t, backend)
					id := objectID(2)
					f.apply(f.create("file", kind, id))
					// A count-only edit must fail the unchanged-ledger guard.
					// A rename with the next ledger revision must still preserve
					// the exact count of resulting references.
					op := f.op(Rename, id)
					change := edit(f.get(id))
					change.After.Attr.Nlink++
					want := fsapi.Stale
					if advanceLedger {
						op = f.rename("file", "renamed")
						change = op.Objects[1]
						change.After.Attr.Nlink++
						want = fsapi.Invalid
						op.Objects[1] = change
					} else {
						op.Objects = []ObjectChange{change}
					}
					assertRejectedCatalog(f, op, want)
				})
			}
		}
	}
}

func TestDirectoryAccountingRevisionGuards(t *testing.T) {
	for _, backend := range []string{"simulator", "os"} {
		for _, fault := range []string{"missing-count", "wrong-count", "object-revision", "ledger-gap"} {
			t.Run(backend+"/"+fault, func(t *testing.T) {
				f := fixtureFor(t, backend)
				op := f.create("directory", fsapi.Directory, objectID(2))
				parent := &op.Objects[0]
				want := fsapi.Invalid
				switch fault {
				case "missing-count":
					parent.After.Attr.Nlink--
				case "wrong-count":
					parent.After.Attr.Nlink++
				case "object-revision":
					parent.After.Attr.Revision--
					want = fsapi.Stale
				case "ledger-gap":
					parent.After.LedgerRevision += 2
					want = fsapi.Stale
				}
				assertRejectedCatalog(f, op, want)
			})
		}
	}
}

func checkDirectoryState(f *fixture, id fsapi.ObjectID, nlink, revision, ledger uint64, parent fsapi.ObjectID, parentRevision uint64) {
	f.t.Helper()
	o := f.get(id)
	if o.Attr.Nlink != nlink || o.Attr.Revision != revision || o.LedgerRevision != ledger || o.Parent == nil || o.Parent.Object != parent || o.Parent.Revision != parentRevision {
		f.t.Fatalf("directory %v: nlink/revision/ledger/parent = %d/%d/%d/%+v", id, o.Attr.Nlink, o.Attr.Revision, o.LedgerRevision, o.Parent)
	}
}

func reopenDirectoryImage(f *fixture, image Image) {
	f.t.Helper()
	if sim, ok := f.data.(*vfs.Simulator); ok {
		sim.Crash()
		f.journal.(*vfs.Simulator).Crash()
	}
	opened, err := Open(f.t.Context(), f.data, f.journal, f.config, image)
	if err != nil {
		f.t.Fatal(err)
	}
	f.store = opened
	if got := f.image(); !reflect.DeepEqual(got, image) {
		f.t.Fatal("reopen changed independent directory revisions")
	}
}

func assertRejectedCatalog(f *fixture, op Operation, want error) {
	f.t.Helper()
	before := f.image()
	if err := f.store.Apply(f.t.Context(), op); !errors.Is(err, want) {
		f.t.Fatalf("invalid operation: got %v, want %v", err, want)
	}
	if after := f.image(); !reflect.DeepEqual(before, after) {
		f.t.Fatal("rejected operation changed catalog")
	}
}
