package fsservice

import (
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/vfs"
)

func TestTruncateTimesRejectBeforeEffect(t *testing.T) {
	f := unixFixture(t)
	f.start()
	file := f.create(f.c.Storage.Root, "file")
	other := f.create(f.c.Storage.Root, "other")
	read := f.ok(fsapi.Command{Operation: fsapi.Open, Object: file.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadOnly}})
	f.ok(fsapi.Command{Operation: fsapi.Write, Handle: file.Handle, Data: []byte("original")})
	mode, ownerID, groupID := uint16(0o600), uint32(2), uint32(3)
	good, bad := fsapi.Timestamp{Seconds: 10, Nanoseconds: 20}, fsapi.Timestamp{Nanoseconds: 1_000_000_000}
	for _, test := range []struct {
		name string
		c    fsapi.Command
		want fsapi.Errno
	}{
		{"readonly", fsapi.Command{Operation: fsapi.Truncate, Handle: read.Handle}, fsapi.Permission},
		{"wrong-object", fsapi.Command{Operation: fsapi.Truncate, Object: other.Attr.Object, Handle: file.Handle}, fsapi.Stale},
		{"wrong-generation", fsapi.Command{Operation: fsapi.Truncate, Handle: file.Handle, Generation: uid(99)}, fsapi.Stale},
		{"bad-atime", fsapi.Command{Operation: fsapi.Truncate, Handle: file.Handle, TruncateTimes: fsapi.TruncateTimes{Atime: &bad}}, fsapi.Invalid},
		{"bad-mtime", fsapi.Command{Operation: fsapi.Truncate, Handle: file.Handle, TruncateTimes: fsapi.TruncateTimes{Mtime: &bad}}, fsapi.Invalid},
		{"bad-ctime", fsapi.Command{Operation: fsapi.Truncate, Handle: file.Handle, TruncateTimes: fsapi.TruncateTimes{Ctime: &bad}}, fsapi.Invalid},
		{"ambiguous-atime", fsapi.Command{Operation: fsapi.Truncate, Handle: file.Handle, TruncateTimes: fsapi.TruncateTimes{Atime: &good, AtimeNow: true}}, fsapi.Invalid},
		{"ambiguous-mtime", fsapi.Command{Operation: fsapi.Truncate, Handle: file.Handle, TruncateTimes: fsapi.TruncateTimes{Mtime: &good, MtimeNow: true}}, fsapi.Invalid},
		{"mode", fsapi.Command{Operation: fsapi.Truncate, Handle: file.Handle, Attributes: fsapi.Attributes{Mode: &mode}}, fsapi.Unsupported},
		{"uid", fsapi.Command{Operation: fsapi.Truncate, Handle: file.Handle, Attributes: fsapi.Attributes{UID: &ownerID}}, fsapi.Unsupported},
		{"gid", fsapi.Command{Operation: fsapi.Truncate, Handle: file.Handle, Attributes: fsapi.Attributes{GID: &groupID}}, fsapi.Unsupported},
		{"wrong-operation", fsapi.Command{Operation: fsapi.Setattr, Object: file.Attr.Object, TruncateTimes: fsapi.TruncateTimes{Ctime: &good}}, fsapi.Invalid},
	} {
		t.Run(test.name, func(t *testing.T) {
			before := *f.ok(fsapi.Command{Operation: fsapi.Getattr, Object: file.Attr.Object}).Attr
			cut := f.service.selection.Through
			if got := f.request(test.c); got.Code != test.want {
				t.Fatalf("got %v want %v", got.Code, test.want)
			}
			after := *f.ok(fsapi.Command{Operation: fsapi.Getattr, Object: file.Attr.Object}).Attr
			if before != after || cut != f.service.selection.Through || f.service.State() != Serving {
				t.Fatal("rejection changed committed metadata/journal or serving state")
			}
			f.data(file.Handle, []byte("original"))
		})
	}
}

func TestTruncateTimesDescriptorGrantAndReplay(t *testing.T) {
	for _, backend := range []string{"sim", "os"} {
		t.Run(backend, func(t *testing.T) {
			f := setup(t, backend)
			policy, err := NewUnixPermissions(uid(50))
			if err != nil {
				t.Fatal(err)
			}
			f.c.Permissions = policy
			f.c.Segment.MaximumBytes = 1 << 20
			f.start()
			file := f.create(f.c.Storage.Root, "file")
			f.ok(fsapi.Command{Operation: fsapi.Write, Handle: file.Handle, Data: []byte("original")})
			chmod(f, file.Attr.Object, 0o6000)
			before := *f.ok(fsapi.Command{Operation: fsapi.Getattr, Object: file.Attr.Object}).Attr
			at, mt, ct := fsapi.Timestamp{Seconds: 10, Nanoseconds: 20}, fsapi.Timestamp{Seconds: 30, Nanoseconds: 40}, fsapi.Timestamp{Seconds: 50, Nanoseconds: 60}
			// A transferred writable description retains its grant after chmod;
			// a caller without that description cannot truncate the object.
			caller := fsapi.Cred{UID: 9000, GID: 9000}
			c := fsapi.Command{Operation: fsapi.Truncate, Object: file.Attr.Object, Size: 3, TruncateTimes: fsapi.TruncateTimes{Atime: &at, Mtime: &mt, Ctime: &ct}}
			if r := as(f, caller, c); r.Code != fsapi.Permission {
				t.Fatal(r)
			}
			c.Handle = file.Handle
			r := as(f, caller, c)
			if r.Code != fsapi.OK || r.Attr.Revision != before.Revision+1 || r.Attr.Atime != at || r.Attr.Mtime != mt || r.Attr.Ctime != ct || r.Attr.Mode != 0 || r.Attr.Size != 3 {
				t.Fatalf("atomic truncate times/grant/set-ID: %+v", r)
			}
			want := *r.Attr
			f.data(file.Handle, []byte("ori"))
			if err := f.service.Close(); err != nil {
				t.Fatal(err)
			}
			f.service = nil
			if backend == "sim" {
				for _, root := range f.roots {
					root.(*vfs.Simulator).Crash()
				}
			}
			f.workspace()
			f.start()
			if got := f.ok(fsapi.Command{Operation: fsapi.Getattr, Object: file.Attr.Object}).Attr; *got != want {
				t.Fatalf("replay changed exact truncate metadata: got %+v want %+v", *got, want)
			}
		})
	}
}
