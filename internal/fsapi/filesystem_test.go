package fsapi_test

import (
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
)

func TestFilesystemCommandOwnsAllMutableArguments(t *testing.T) {
	mode := uint16(0o640)
	uid, gid := uint32(100), uint32(200)
	at, mt := fsapi.Timestamp{Seconds: 1}, fsapi.Timestamp{Seconds: 2}
	c := fsapi.Command{Data: []byte("owned"), Attributes: fsapi.Attributes{Mode: &mode, UID: &uid, GID: &gid, Atime: &at, Mtime: &mt}}
	clone := c.Clone()
	c.Data[0] = 'x'
	mode = 0
	uid = 0
	gid = 0
	at.Seconds = 0
	mt.Seconds = 0
	if string(clone.Data) != "owned" || *clone.Attributes.Mode != 0o640 || *clone.Attributes.UID != 100 || *clone.Attributes.GID != 200 || clone.Attributes.Atime.Seconds != 1 || clone.Attributes.Mtime.Seconds != 2 {
		t.Fatal("borrowed command buffers", clone)
	}
}
func TestFilesystemResultValidationAndCopies(t *testing.T) {
	bad := []fsapi.Result{
		{Handle: 1},
		{DirectoryRevision: 1},
		{Code: fsapi.IO, Generation: uuid(1)},
		{Code: fsapi.IO, Capacity: &fsapi.Capacity{}},
		{Code: fsapi.IO, DirectoryRevision: 1},
		{Code: fsapi.IO, Position: fsapi.Position{Volume: uuid(1)}},
		{Capacity: &fsapi.Capacity{Objects: 1, ObjectsFree: 2}},
		{Capacity: &fsapi.Capacity{Bytes: 1, BytesFree: 2}},
		{Position: fsapi.Position{Volume: uuid(1)}},
		{Entries: []fsapi.DirEntry{{Name: "a", Object: object(1), Revision: 1, Kind: fsapi.Regular}}},
		{Generation: uuid(1), DirectoryRevision: 1, Entries: []fsapi.DirEntry{{Name: "../x", Object: object(1), Revision: 1, Kind: fsapi.Regular}}},
		{Generation: uuid(1), DirectoryRevision: 1, Entries: []fsapi.DirEntry{{Name: "a", Object: object(1), Kind: fsapi.Regular}}},
		{Generation: uuid(1), DirectoryRevision: 1, Entries: []fsapi.DirEntry{{Name: "a", Revision: 1, Kind: fsapi.Regular}}},
		{Generation: uuid(1), DirectoryRevision: 1, Entries: []fsapi.DirEntry{{Name: "a", Object: object(1), Revision: 1}}},
	}
	for i, result := range bad {
		if err := fsapi.NewCompletion[fsapi.Result]().Complete(result); !errors.Is(err, fsapi.Invalid) {
			t.Fatalf("bad result %d: %v", i, err)
		}
	}
	source := fsapi.Result{Generation: uuid(1), DirectoryRevision: 1, Entries: []fsapi.DirEntry{{Name: "raw-\xff", Object: object(1), Revision: 1, Kind: fsapi.Regular}}, Capacity: &fsapi.Capacity{Bytes: 10, BytesFree: 5}}
	c := fsapi.NewCompletion[fsapi.Result]()
	if err := c.Complete(source); err != nil {
		t.Fatal(err)
	}
	source.Entries[0].Name = "changed"
	source.Capacity.BytesFree = 0
	first, err := c.Wait(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	first.Entries[0].Name = "changed again"
	first.Capacity.BytesFree = 1
	second, err := c.Wait(t.Context())
	if err != nil || second.Entries[0].Name != "raw-\xff" || second.Capacity.BytesFree != 5 {
		t.Fatal(second, err)
	}
}

func TestFilesystemRequestAdmissionValidation(t *testing.T) {
	for _, r := range []*fsapi.Request{nil, {}, fsapi.NewRequest(fsapi.Cred{Groups: make([]uint32, 1025)})} {
		if err := r.Validate(); !errors.Is(err, fsapi.Invalid) {
			t.Fatal("unbounded/unconstructed request accepted", err)
		}
	}
	if err := fsapi.NewRequest(fsapi.Cred{Groups: make([]uint32, 1024)}).Validate(); err != nil {
		t.Fatal(err)
	}
}
