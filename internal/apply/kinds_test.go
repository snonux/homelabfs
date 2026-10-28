package apply

import (
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/vfs"
)

func TestDirectoryAndSymlinkKinds(t *testing.T) {
	f := newFixture(t, "sim")
	directory := oid(30)
	entry := f.entry("d", &directory)
	ref := record.Reference{Parent: oid(4), Name: "d", EntryRevision: entry.After.Revision}
	parent := record.ParentState{Revision: 1, Parent: oid(4), ParentEntryRevision: entry.After.Revision}
	m := record.Metadata{Revision: 1, Type: 2, Mode: 0755, Nlink: 2}
	f.objects[directory] = modelObject{meta: m, refs: []record.Reference{ref}, parent: &parent, ledger: 1}
	root := f.objects[oid(4)]
	root.meta.Nlink++
	f.objects[oid(4)] = root
	f.commit(record.Mkdir{Parent: oid(4), Name: "d", Object: directory, RequestedMode: 0755}, 2, record.Redo{Objects: []record.ObjectEdit{f.change(oid(4)), {Object: directory, After: m}}, Entries: []record.EntryEdit{entry}, Ledgers: []record.LedgerEdit{{Object: directory, ResultingRevision: 1, ResultingNlink: 1, Add: []record.Reference{ref}}}, Parents: []record.ParentEdit{{Directory: directory, After: parent}}})
	// Move the directory without changing its descendant homes or own link count.
	remove := f.entry("d", nil)
	add := f.entry("moved", &directory)
	ledger := f.ledger(directory, []record.Reference{ref}, []record.Reference{{Parent: oid(4), Name: "moved", EntryRevision: add.After.Revision}})
	nextParent := record.ParentState{Revision: 2, Parent: oid(4), ParentEntryRevision: add.After.Revision}
	obj := f.objects[directory]
	obj.parent = &nextParent
	f.objects[directory] = obj
	f.commit(record.Rename{SourceParent: oid(4), SourceName: "d", Source: directory, DestinationParent: oid(4), DestinationName: "moved", Policy: 1}, 7, record.Redo{Objects: []record.ObjectEdit{f.change(oid(4)), f.change(directory)}, Entries: []record.EntryEdit{remove, add}, Ledgers: []record.LedgerEdit{ledger}, Parents: []record.ParentEdit{{Directory: directory, Before: &parent, After: nextParent}}})
	removed := f.entry("moved", nil)
	ledger = f.ledger(directory, []record.Reference{{Parent: oid(4), Name: "moved", EntryRevision: add.After.Revision}}, nil)
	obj = f.objects[directory]
	obj.meta.Nlink = 0
	f.objects[directory] = obj
	root = f.objects[oid(4)]
	root.meta.Nlink--
	f.objects[oid(4)] = root
	f.commit(record.Rmdir{Parent: oid(4), Name: "moved", Removed: directory}, 6, record.Redo{Objects: []record.ObjectEdit{f.change(oid(4)), f.change(directory)}, Entries: []record.EntryEdit{removed}, Ledgers: []record.LedgerEdit{ledger}})
	link := oid(31)
	symlinkEntry := f.entry("symlink", &link)
	text := []byte("../raw-\xff-target")
	lm := record.Metadata{Revision: 1, Type: 3, Mode: 0777, Nlink: 1, Size: uint64(len(text)), SymlinkText: text}
	lref := record.Reference{Parent: oid(4), Name: "symlink", EntryRevision: symlinkEntry.After.Revision}
	f.objects[link] = modelObject{meta: lm, refs: []record.Reference{lref}, ledger: 1}
	f.commit(record.Symlink{Parent: oid(4), Name: "symlink", Object: link, Text: text}, 3, record.Redo{Objects: []record.ObjectEdit{f.change(oid(4)), {Object: link, After: lm}}, Entries: []record.EntryEdit{symlinkEntry}, Ledgers: []record.LedgerEdit{{Object: link, ResultingRevision: 1, ResultingNlink: 1, Add: []record.Reference{lref}}}})
	result, err := f.replay(fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), f.config)
	if err != nil {
		t.Fatal(err)
	}
	f.oracle(result.Store)
	got, err := result.Store.Stat(t.Context(), oid(4))
	if err != nil || got.LedgerRevision != 1 || got.Attr.Nlink != 2 {
		t.Fatalf("directory ledger changed with child metadata: %+v %v", got, err)
	}
}
func TestLinkSetattrNoopPinReleaseAndReclaim(t *testing.T) {
	f := newFixture(t, "sim")
	f.create("a", oid(20))
	f.write(oid(20), 0, []byte("retained"))
	id := oid(20)
	entry := f.entry("alias", &id)
	ledger := f.ledger(id, nil, []record.Reference{{Parent: oid(4), Name: "alias", EntryRevision: entry.After.Revision}})
	f.commit(record.Link{Source: id, TargetParent: oid(4), TargetName: "alias"}, 4, record.Redo{Objects: []record.ObjectEdit{f.change(oid(4)), f.change(id)}, Entries: []record.EntryEdit{entry}, Ledgers: []record.LedgerEdit{ledger}})
	f.commit(record.Rename{SourceParent: oid(4), SourceName: "a", Source: id, DestinationParent: oid(4), DestinationName: "alias", Replaced: &id, Noop: true, Policy: 1}, 7, record.Redo{})
	mode := uint16(0600)
	obj := f.objects[id]
	obj.meta.Mode = mode
	f.objects[id] = obj
	f.commit(record.Setattr{Object: id, Mask: 1, Mode: &mode}, 10, record.Redo{Objects: []record.ObjectEdit{f.change(id)}})
	f.next++
	f.append(uid(f.next), record.HandlePin{Pin: uid(70), Object: id, ServingGeneration: uid(71), Incarnation: uid(7), Reasons: 1}, true)
	f.unlink("a")
	f.unlink("alias")
	f.next++
	f.append(uid(f.next), record.HandleRelease{Pin: uid(70), Object: id, ServingGeneration: uid(71), Incarnation: uid(7), Reason: 1}, true)
	obj = f.objects[id]
	f.next++
	f.append(uid(f.next), record.Reclaim{Object: id, ExpectedObjectRevision: obj.meta.Revision, ExpectedLedgerRevision: obj.ledger, Retention: f.selection.Effects[0]}, true)
	delete(f.objects, id)
	result, err := f.replay(fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), f.config)
	if err != nil {
		t.Fatal(err)
	}
	f.oracle(result.Store)
	if _, err = result.Store.Stat(t.Context(), id); !errors.Is(err, fsapi.NotFound) {
		t.Fatalf("reclaimed object retained: %v", err)
	}
	image, err := result.Store.Export(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(image.Retired) != 1 || len(image.Pins) != 1 || image.Pins[0].Active {
		t.Fatalf("lost lifetime evidence: %+v", image)
	}
}

var _ storage.Filesystem = crashFS{}

func TestNoopRenameChecksProtectedOccupants(t *testing.T) {
	f := newFixture(t, "sim")
	f.create("a", oid(20))
	f.create("b", oid(21))
	id := oid(20)
	f.commit(record.Rename{SourceParent: oid(4), SourceName: "a", Source: id, DestinationParent: oid(4), DestinationName: "b", Replaced: &id, Noop: true, Policy: 1}, 7, record.Redo{})
	if _, err := f.replay(fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), f.config); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("no-op ignored replacement identity: %v", err)
	}
}
