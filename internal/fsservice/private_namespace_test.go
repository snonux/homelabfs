package fsservice

import (
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/storage"
)

func TestPrivateControlNamespaceReservationAndExistingConflict(t *testing.T) {
	f := setup(t, "sim")
	f.start()
	s := f.service
	root := f.c.Storage.Root
	if err := s.CheckPrivateControlNamespace(t.Context()); err != nil {
		t.Fatal(err)
	}
	file := f.create(root, "ordinary")
	for _, command := range []fsapi.Command{
		{Operation: fsapi.Create, Parent: root, Name: ".homelabfs-control-v1", Mode: 0600, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}},
		{Operation: fsapi.RenameOp, Parent: root, Name: "ordinary", Destination: root, NewName: ".homelabfs-control-v1"},
		{Operation: fsapi.LinkOp, Object: file.Attr.Object, Destination: root, NewName: ".homelabfs-control-v1"},
	} {
		if r := f.request(command); r.Code != fsapi.Invalid {
			t.Fatal(command.Operation, r.Code)
		}
	}
	// A historical initialized workload may already contain the newly reserved
	// name. Reconstruct that synthetic image directly; no service operation may
	// create this entry now. Constructor preflight must refuse the conflict.
	snapshot, err := s.applied.Store.Capture(t.Context(), 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	for i := range snapshot.Image.Entries {
		if snapshot.Image.Entries[i].Name == "ordinary" {
			snapshot.Image.Entries[i].Name = ".homelabfs-control-v1"
		}
	}
	for i := range snapshot.Image.Objects {
		for j := range snapshot.Image.Objects[i].References {
			if snapshot.Image.Objects[i].References[j].Name == "ordinary" {
				snapshot.Image.Objects[i].References[j].Name = ".homelabfs-control-v1"
			}
		}
	}
	restored, err := storage.Restore(t.Context(), f.fresh(), f.fresh(), f.c.Storage, snapshot, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	original := s.applied.Store
	s.applied.Store = restored
	defer func() { s.applied.Store = original }()
	if err = s.CheckPrivateControlNamespace(t.Context()); !errors.Is(err, fsapi.Exists) {
		t.Fatal(err)
	}
}
