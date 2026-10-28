//go:build linux || freebsd

package fuseadapter

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"syscall"
	"testing"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
)

func TestPublicNewRequiresNativeIOCapacity(t *testing.T) {
	for _, limit := range []int{4096, nativeRequestBytes - 1, nativeRequestBytes} {
		t.Run(fmt.Sprintf("limit-%d", limit), func(t *testing.T) {
			s := serviceWithIO(t, "", limit)
			if got := s.FrontendInfo().IOBytes; got != limit {
				t.Fatalf("admitted cap %d, want %d", got, limit)
			}
			a, err := New(Config{Service: s, Validation: s.FrontendInfo().Validation, Credentials: credentialFunc(testCredentials)})
			if limit < nativeRequestBytes {
				if !errors.Is(err, fsapi.Unsupported) || a != nil {
					t.Fatalf("lower-cap service published: %v", err)
				}
				detach, err := s.AttachFrontend()
				if err != nil {
					t.Fatalf("rejected constructor retained publication: %v", err)
				}
				detach()
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if mountOptions().MaxWrite != nativeRequestBytes {
				t.Fatal("native request cap differs from admitted boundary")
			}
			var created fuse.CreateOut
			if code := a.Create(nil, &fuse.CreateIn{InHeader: testHeader(1, 100), Flags: syscall.O_CREAT | syscall.O_RDWR, Mode: 0600}, "boundary", &created); code != fuse.OK {
				t.Fatal(code)
			}
			h := testHeader(created.NodeId, 100)
			data := bytes.Repeat([]byte{0xa5}, nativeRequestBytes)
			if n, code := a.Write(nil, &fuse.WriteIn{InHeader: h, Fh: created.Fh, Size: uint32(len(data))}, data); code != fuse.OK || n != uint32(len(data)) {
				t.Fatal(n, code)
			}
			read, code := a.Read(nil, &fuse.ReadIn{InHeader: h, Fh: created.Fh, Size: uint32(len(data))}, nil)
			if code != fuse.OK {
				t.Fatal(code)
			}
			got, code := read.Bytes(nil)
			read.Done()
			if code != fuse.OK || !bytes.Equal(got, data) {
				t.Fatalf("boundary read: %d bytes, %v", len(got), code)
			}
			a.OnUnmount()
			if err := a.Err(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCapacityFailureDrainsAcceptedCreateReferences(t *testing.T) {
	info := fsservice.FrontendInfo{Volume: testID(1), Generation: testID(2), Root: fsapi.ObjectID{Home: testID(1), Allocation: testID(3)}}
	created := fsapi.Attr{Object: fsapi.ObjectID{Home: info.Volume, Allocation: testID(4)}, Kind: fsapi.Regular, Mode: 0600, Revision: 1, Nlink: 1}
	var forget, release int
	service := dispatchFunc(func(_ context.Context, r *fsapi.Request, c fsapi.Command) error {
		result := fsapi.Result{Generation: info.Generation}
		switch c.Operation {
		case fsapi.Create:
			result.Attr = &created
			result.Handle = 7
		case fsapi.Forget:
			if c.Object != created.Object || c.Count != 1 {
				t.Error(c)
			}
			forget++
		case fsapi.Release:
			if c.Handle != 7 || !c.DescriptionOnly {
				t.Error(c)
			}
			release++
		default:
			t.Errorf("unexpected command %v", c.Operation)
		}
		return r.Completion().Complete(result)
	})
	a, err := newAdapter(Config{Credentials: credentialFunc(testCredentials), MaxNodes: 1}, service, info)
	if err != nil {
		t.Fatal(err)
	}
	if s := a.Create(nil, &fuse.CreateIn{InHeader: testHeader(1, 100), Flags: syscall.O_RDWR | syscall.O_CREAT, Mode: 0600}, "new", &fuse.CreateOut{}); s != status(fsapi.NoSpace) {
		t.Fatal(s)
	}
	if forget != 1 || release != 1 || len(a.handles) != 0 || a.Err() != nil {
		t.Fatal(forget, release, a.Err())
	}
}

func TestCredentialSnapshotsAndNativeOwnersFailClosed(t *testing.T) {
	a, _ := serviceFixture(t, "", credentialFunc(testCredentials))
	h := testHeader(1, 100)
	for _, change := range []func(*Identity){
		func(i *Identity) { i.Credentials.UID++ }, func(i *Identity) { i.Credentials.GID++ }, func(i *Identity) { i.Credentials.PID++ }, func(i *Identity) { i.Process = 0 }, func(i *Identity) { i.Credentials.Groups = make([]uint32, 1025) },
	} {
		a.credentials = credentialFunc(func(ctx context.Context, c Caller) (Identity, error) {
			i, err := testCredentials(ctx, c)
			change(&i)
			return i, err
		})
		if _, err := a.identity(context.Background(), &h); !errors.Is(err, fsapi.Permission) {
			t.Fatal(err)
		}
	}
	a.credentials = credentialFunc(testCredentials)
	one, _ := a.identity(context.Background(), &h)
	if err := a.bindPOSIXOwner(900, one); err != nil {
		t.Fatal(err)
	}
	other := one
	other.Process++
	if err := a.bindPOSIXOwner(900, other); !errors.Is(err, fsapi.Stale) {
		t.Fatal(err)
	}
	if err := a.bindPOSIXOwner(901, one); !errors.Is(err, fsapi.Unsupported) {
		t.Fatal(err)
	}
	// Every semantic command carries the issuing generation, even object refs.
	var command fsapi.Command
	a.service = dispatchFunc(func(_ context.Context, r *fsapi.Request, c fsapi.Command) error {
		command = c
		return r.Completion().Complete(fsapi.Result{Generation: testID(99)})
	})
	if _, s := a.call(nil, &h, fsapi.Command{Operation: fsapi.Getattr, Object: a.info.Root}); s != status(fsapi.Stale) || command.Generation != a.info.Generation || !a.blocked.Load() {
		t.Fatal(s, command)
	}
}

func TestBlockingLockCancellationDefinitiveOutcomes(t *testing.T) {
	for _, code := range []fsapi.Errno{fsapi.Interrupted, fsapi.OK} {
		t.Run(code.Error(), func(t *testing.T) {
			info := fsservice.FrontendInfo{Volume: testID(1), Generation: testID(2), Root: fsapi.ObjectID{Home: testID(1), Allocation: testID(3)}}
			admitted := make(chan struct{})
			var original *fsapi.Request
			canceled := false
			service := dispatchFunc(func(_ context.Context, r *fsapi.Request, c fsapi.Command) error {
				if c.Operation == fsapi.WaitLock {
					original = r
					close(admitted)
					return nil
				}
				if c.Operation != fsapi.CancelLock {
					return fsapi.Invalid
				}
				canceled = true
				v := fsapi.Result{Code: code}
				if code == fsapi.OK {
					v.Generation = info.Generation
				}
				if err := original.Completion().Complete(v); err != nil {
					return err
				}
				return r.Completion().Complete(fsapi.Result{Generation: info.Generation})
			})
			a, err := newAdapter(Config{Credentials: credentialFunc(testCredentials)}, service, info)
			if err != nil {
				t.Fatal(err)
			}
			a.handles[1] = &description{object: info.Root, handle: 1, generation: info.Generation}
			cancel := make(chan struct{})
			done := make(chan fuse.Status, 1)
			go func() {
				done <- a.SetLkw(cancel, &fuse.LkIn{InHeader: testHeader(1, 100), Fh: 1, Owner: 123, Lk: fuse.FileLock{Typ: syscall.F_WRLCK, End: math.MaxInt64}})
			}()
			<-admitted
			close(cancel)
			if s := <-done; s != status(code) || !canceled {
				t.Fatal(s, canceled)
			}
		})
	}
}

func TestReaddirRevisionAndUnmountDrain(t *testing.T) {
	a, _ := serviceFixture(t, "", credentialFunc(testCredentials))
	h := testHeader(1, 100)
	var dir fuse.OpenOut
	if s := a.OpenDir(nil, &fuse.OpenIn{InHeader: h}, &dir); s != fuse.OK {
		t.Fatal(s)
	}
	var created fuse.CreateOut
	if s := a.Create(nil, &fuse.CreateIn{InHeader: h, Flags: syscall.O_CREAT | syscall.O_RDWR, Mode: 0600}, "a", &created); s != fuse.OK {
		t.Fatal(s)
	}
	if s := a.ReadDir(nil, &fuse.ReadIn{InHeader: h, Fh: dir.Fh}, fuse.NewDirEntryList(make([]byte, 256), 0)); s != fuse.OK {
		t.Fatal(s)
	}
	if s := a.Mkdir(nil, &fuse.MkdirIn{InHeader: h, Mode: 0700}, "b", &fuse.EntryOut{}); s != fuse.OK {
		t.Fatal(s)
	}
	if s := a.ReadDir(nil, &fuse.ReadIn{InHeader: h, Fh: dir.Fh, Offset: 1}, fuse.NewDirEntryList(make([]byte, 256), 1)); s != status(fsapi.Stale) {
		t.Fatal(s)
	}
	a.OnUnmount()
	if err := a.Err(); err != nil || len(a.handles) != 0 {
		t.Fatal(err, len(a.handles))
	}
}

func TestDuplicatePublicationRejected(t *testing.T) {
	a, s := serviceFixture(t, "", credentialFunc(testCredentials))
	detach, err := s.AttachFrontend()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Mount(t.TempDir()); !errors.Is(err, fsapi.Busy) {
		t.Fatal(err)
	}
	if _, err := s.AttachFrontend(); !errors.Is(err, fsapi.Busy) {
		t.Fatal(err)
	}
	detach() // models the native owner after verified unmount + reference drain
	second, err := s.AttachFrontend()
	if err != nil {
		t.Fatal(err)
	}
	detach() // a stale lease cannot detach the new frontend
	if _, err := s.AttachFrontend(); !errors.Is(err, fsapi.Busy) {
		t.Fatal(err)
	}
	second()
}

func TestMetadataAndRawSymlinkTranslation(t *testing.T) {
	a, _ := serviceFixture(t, "", credentialFunc(testCredentials))
	h := testHeader(1, 100)
	var link fuse.EntryOut
	if s := a.Symlink(nil, &h, "../raw-\xff", "link-\xff", &link); s != fuse.OK {
		t.Fatal(s)
	}
	lh := testHeader(link.NodeId, 100)
	if data, s := a.Readlink(nil, &lh); s != fuse.OK || string(data) != "../raw-\xff" {
		t.Fatal(data, s)
	}
	if s := a.Unlink(nil, &h, "link-\xff"); s != fuse.OK {
		t.Fatal(s)
	}
	a.Forget(link.NodeId, 1)
	var file fuse.CreateOut
	if s := a.Create(nil, &fuse.CreateIn{InHeader: h, Flags: syscall.O_CREAT | syscall.O_RDWR, Mode: 0600}, "file", &file); s != fuse.OK {
		t.Fatal(s)
	}
	fh := testHeader(file.NodeId, 100)
	var out fuse.AttrOut
	change := fuse.SetAttrIn{SetAttrInCommon: fuse.SetAttrInCommon{InHeader: fh, Valid: fuse.FATTR_ATIME | fuse.FATTR_MTIME, Atime: 12, Atimensec: 34, Mtime: 56, Mtimensec: 78}}
	if s := a.SetAttr(nil, &change, &out); s != fuse.OK || out.Atime != 12 || out.Atimensec != 34 || out.Mtime != 56 || out.Mtimensec != 78 {
		t.Fatal(s, out)
	}
	change.Valid = fuse.FATTR_SIZE | fuse.FATTR_MODE
	change.Size = 99
	change.Mode = 0644
	if s := a.SetAttr(nil, &change, &out); s != status(fsapi.Unsupported) {
		t.Fatal(s)
	}
	if s := a.GetAttr(nil, &fuse.GetAttrIn{InHeader: fh}, &out); s != fuse.OK || out.Size != 0 || out.Mode&07777 != 0600 {
		t.Fatal(s, out)
	}
	change.Valid = fuse.FATTR_SIZE | fuse.FATTR_FH
	change.Fh = file.Fh
	change.Size = 99
	if s := a.SetAttr(nil, &change, &out); s != fuse.OK || out.Size != 99 {
		t.Fatal(s, out)
	}
	if s := a.Flush(nil, &fuse.FlushIn{InHeader: fh, Fh: file.Fh}); s != fuse.OK {
		t.Fatal(s)
	}
	a.Release(nil, &fuse.ReleaseIn{InHeader: fh, Fh: file.Fh})
	a.Forget(file.NodeId, 1)
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
}
