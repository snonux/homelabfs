//go:build linux || freebsd

package fuseadapter

import (
	"context"
	"syscall"
	"testing"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/fsapi"
)

// FreeBSD 15.1 fuse_internal_setattr folds dirty cached timestamps into a
// truncate, including ctime with the negotiated 7.28 protocol. A preceding
// read dirties atime even when the file has already been unlinked.
func TestNativeTruncateDirtyTimestamps(t *testing.T) {
	for _, test := range []struct {
		name  string
		valid uint32
	}{
		{"read-atime", fuse.FATTR_ATIME},
		{"read-write-times", fuse.FATTR_ATIME | fuse.FATTR_MTIME | fuse.FATTR_CTIME},
	} {
		t.Run(test.name, func(t *testing.T) {
			a, _ := serviceFixture(t, "", credentialFunc(testCredentials))
			h := testHeader(1, 100)
			var file fuse.CreateOut
			if s := a.Create(nil, &fuse.CreateIn{InHeader: h, Flags: syscall.O_CREAT | syscall.O_RDWR, Mode: 0600}, "original", &file); s != fuse.OK {
				t.Fatal(s)
			}
			fh := testHeader(file.NodeId, 100)
			if n, s := a.Write(nil, &fuse.WriteIn{InHeader: fh, Fh: file.Fh, Size: 8}, []byte("original")); s != fuse.OK || n != 8 {
				t.Fatal(n, s)
			}
			if s := a.Unlink(nil, &h, "original"); s != fuse.OK {
				t.Fatal(s)
			}
			change := fuse.SetAttrIn{SetAttrInCommon: fuse.SetAttrInCommon{InHeader: fh, Valid: fuse.FATTR_SIZE | fuse.FATTR_FH | test.valid, Fh: file.Fh, Size: 3, Atime: 12, Atimensec: 34, Mtime: 56, Mtimensec: 78, Ctime: 90, Ctimensec: 12}}
			service, calls := a.service, 0
			a.service = dispatchFunc(func(ctx context.Context, r *fsapi.Request, c fsapi.Command) error {
				calls++
				if c.Operation != fsapi.Truncate {
					t.Errorf("split native truncate into %v", c.Operation)
				}
				return service.Dispatch(ctx, r, c)
			})
			var out fuse.AttrOut
			if s := a.SetAttr(nil, &change, &out); s != fuse.OK || out.Size != 3 || out.Atime != 12 || out.Atimensec != 34 || out.Nlink != 0 || out.Mode&07777 != 0600 {
				t.Fatalf("native truncate: %v %+v", s, out)
			}
			a.service = service
			if calls != 1 {
				t.Fatalf("native truncate used %d service calls", calls)
			}
			if test.valid&fuse.FATTR_MTIME != 0 && (out.Mtime != 56 || out.Mtimensec != 78 || out.Ctime != 90 || out.Ctimensec != 12) {
				t.Fatalf("dirty timestamps lost: %+v", out)
			}
			read, s := a.Read(nil, &fuse.ReadIn{InHeader: fh, Fh: file.Fh, Size: 8}, nil)
			if s != fuse.OK {
				t.Fatal(s)
			}
			data, s := read.Bytes(nil)
			read.Done()
			if s != fuse.OK || string(data) != "ori" {
				t.Fatal(string(data), s)
			}
			change.Valid = fuse.FATTR_SIZE | fuse.FATTR_FH | fuse.FATTR_ATIME | fuse.FATTR_ATIME_NOW | fuse.FATTR_MTIME | fuse.FATTR_MTIME_NOW
			change.Size = 9
			if s := a.SetAttr(nil, &change, &out); s != fuse.OK || out.Size != 9 || out.Atime == 12 || out.Mtime == 56 {
				t.Fatalf("extend with now times: %v %+v", s, out)
			}
			read, s = a.Read(nil, &fuse.ReadIn{InHeader: fh, Fh: file.Fh, Offset: 3, Size: 6}, nil)
			if s != fuse.OK {
				t.Fatal(s)
			}
			data, s = read.Bytes(nil)
			read.Done()
			if s != fuse.OK || string(data) != string(make([]byte, 6)) {
				t.Fatal("extension did not zero-fill", data, s)
			}
		})
	}
}

func TestNativeTruncateRejectBeforeEffect(t *testing.T) {
	a, _ := serviceFixture(t, "", credentialFunc(testCredentials))
	h := testHeader(1, 100)
	var file fuse.CreateOut
	if s := a.Create(nil, &fuse.CreateIn{InHeader: h, Flags: syscall.O_CREAT | syscall.O_RDWR, Mode: 0600}, "file", &file); s != fuse.OK {
		t.Fatal(s)
	}
	fh := testHeader(file.NodeId, 100)
	for _, test := range []struct {
		name  string
		valid uint32
	}{
		{"mode", fuse.FATTR_MODE},
		{"uid", fuse.FATTR_UID},
		{"gid", fuse.FATTR_GID},
		{"unknown", 1 << 31},
		{"linux-kill-privilege", fuse.FATTR_KILL_SUIDGID},
	} {
		t.Run(test.name, func(t *testing.T) {
			change := fuse.SetAttrIn{SetAttrInCommon: fuse.SetAttrInCommon{InHeader: fh, Valid: fuse.FATTR_SIZE | fuse.FATTR_FH | test.valid, Fh: file.Fh, Size: 99, Mode: 0777, Owner: fuse.Owner{Uid: 12, Gid: 34}}}
			if s := a.SetAttr(nil, &change, &fuse.AttrOut{}); s != status(fsapi.Unsupported) {
				t.Fatal(s)
			}
			var out fuse.AttrOut
			if s := a.GetAttr(nil, &fuse.GetAttrIn{InHeader: fh}, &out); s != fuse.OK || out.Size != 0 || out.Mode&07777 != 0600 || out.Uid != 1000 || out.Gid != 2000 {
				t.Fatal("rejected request changed object", s, out)
			}
		})
	}
	for _, change := range []fuse.SetAttrInCommon{
		{InHeader: fh, Valid: fuse.FATTR_SIZE | fuse.FATTR_FH, Fh: file.Fh + 1, Size: 99},
		{InHeader: h, Valid: fuse.FATTR_SIZE | fuse.FATTR_FH, Fh: file.Fh, Size: 99},
	} {
		if s := a.SetAttr(nil, &fuse.SetAttrIn{SetAttrInCommon: change}, &fuse.AttrOut{}); s != status(fsapi.Stale) {
			t.Fatal("accepted wrong handle/object", s)
		}
	}
	change := fuse.SetAttrIn{SetAttrInCommon: fuse.SetAttrInCommon{InHeader: fh, Valid: fuse.FATTR_CTIME, Ctime: 90}}
	if s := a.SetAttr(nil, &change, &fuse.AttrOut{}); s != status(fsapi.Unsupported) {
		t.Fatal("standalone ctime was admitted", s)
	}
	a.mu.Lock()
	d := a.handles[file.Fh]
	generation := d.generation
	d.generation = testID(99)
	a.mu.Unlock()
	change.Valid, change.Fh, change.Size = fuse.FATTR_SIZE|fuse.FATTR_FH, file.Fh, 99
	if s := a.SetAttr(nil, &change, &fuse.AttrOut{}); s != status(fsapi.Stale) {
		t.Fatal("accepted wrong description generation", s)
	}
	a.mu.Lock()
	d.generation = generation
	a.mu.Unlock()
}
