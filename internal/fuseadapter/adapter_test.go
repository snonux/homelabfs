//go:build linux || freebsd

package fuseadapter

import (
	"context"
	"errors"
	"math"
	"os"
	"path"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/snonux/homelabfs/internal/apply"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/fsservice"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/vfs"
)

type credentialFunc func(context.Context, Caller) (Identity, error)

func (f credentialFunc) Snapshot(ctx context.Context, c Caller) (Identity, error) { return f(ctx, c) }

type dispatchFunc func(context.Context, *fsapi.Request, fsapi.Command) error

func (f dispatchFunc) Dispatch(ctx context.Context, r *fsapi.Request, c fsapi.Command) error {
	return f(ctx, r, c)
}

func testID(n byte) fsapi.UUID { return fsapi.UUID{0: n, 15: n} }
func testHeader(node, process uint64) fuse.InHeader {
	return fuse.InHeader{NodeId: node, Caller: fuse.Caller{Owner: fuse.Owner{Uid: 1000, Gid: 2000}, Pid: uint32(process)}}
}
func testCredentials(ctx context.Context, c Caller) (Identity, error) {
	if err := ctx.Err(); err != nil {
		return Identity{}, err
	}
	return Identity{Credentials: fsapi.Cred{UID: c.UID, GID: c.GID, PID: c.Thread, Groups: []uint32{2000, 3000}}, Process: uint64(c.Thread), ProcessID: c.Thread}, nil
}

func serviceFixture(t *testing.T, disk string, credentials Credentials) (*Adapter, *fsservice.Service) {
	t.Helper()
	s := serviceWithIO(t, disk, nativeRequestBytes)
	a, err := New(Config{Service: s, Validation: s.FrontendInfo().Validation, Credentials: credentials})
	if err != nil {
		t.Fatal(err)
	}
	return a, s
}

func serviceWithIO(t *testing.T, disk string, ioBytes int) *fsservice.Service {
	return startService(t, serviceConfiguration(t, disk, ioBytes))
}

func serviceConfiguration(t *testing.T, disk string, ioBytes int) fsservice.Config {
	t.Helper()
	ctx := t.Context()
	volume := testID(1)
	root := fsapi.ObjectID{Home: volume, Allocation: testID(4)}
	fresh := func(name string) storage.Filesystem {
		if disk == "" {
			f, err := vfs.NewSimulator(vfs.Config{})
			if err != nil {
				t.Fatal(err)
			}
			return f
		}
		dir := path.Join(disk, name)
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		f, err := vfs.NewOS(ctx, dir)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := f.Close(); err != nil {
				t.Error(err)
			}
		})
		return f
	}
	policy, err := fsservice.NewUnixPermissions(volume)
	if err != nil {
		t.Fatal(err)
	}
	c := fsservice.Config{Source: fresh("source"), Reservations: fresh("reservations"), Stream: segment.Stream{Volume: volume, Generation: testID(2), Configuration: testID(3), Epoch: 1, Kind: 1}, Storage: storage.Config{Volume: volume, Root: root}, Workspace: fsservice.Workspace{ID: testID(11), Data: fresh("data"), Journal: fresh("journal"), Progress: fresh("progress")}, Validation: &fsservice.ValidationProfile{Disposable: true, Name: "fuseadapter synthetic validation"}, Permissions: policy, Limits: fsservice.Limits{LogicalBytes: 4 << 20, IOBytes: ioBytes}}
	if disk != "" {
		c.Storage.OwnerUID = uint32(os.Getuid())
	}
	c.Authority = record.Authority{Volume: volume, Generation: testID(2), Configuration: testID(3), Epoch: 1, Owner: testID(6), Partner: testID(8), Members: []fsapi.UUID{testID(6), testID(8), testID(9)}, CoordinatorTerm: 1, OwnerIncarnation: testID(7), PartnerIncarnation: testID(10)}
	dir := "."
	for _, part := range splitPath(c.Stream.Directory()) {
		next := path.Join(dir, part)
		if err := c.Source.Mkdir(ctx, next, 0700); err != nil {
			t.Fatal(err)
		}
		if err := c.Source.SyncDir(ctx, dir); err != nil {
			t.Fatal(err)
		}
		dir = next
	}
	h := record.Header{Volume: volume, Generation: testID(2), Configuration: testID(3), Epoch: 1, Operation: testID(90), Incarnation: testID(7)}
	w, err := segment.NewWriter(ctx, c.Source, c.Stream, segment.Options{MaximumBytes: 1 << 20, Structural: func(uint64) record.Header { return h }, NewSegment: func() fsapi.UUID { return testID(80) }})
	if err != nil {
		t.Fatal(err)
	}
	uid, gid := uint32(1000), uint32(2000)
	if disk != "" {
		uid, gid = uint32(os.Geteuid()), uint32(os.Getegid())
	}
	init := record.VolumeInit{Root: root, Initial: record.Metadata{Revision: 1, Type: 2, UID: uid, GID: gid, Mode: 0777, Nlink: 2}, RootParent: record.ParentState{Revision: 1, Parent: root}, RootLedger: record.LedgerEdit{Object: root, ResultingRevision: 1}, Authority: c.Authority, Authorization: record.Artifact{Kind: 6, ID: testID(70), Length: 1}}
	h.Operation = testID(91)
	receipt, err := w.Append(ctx, record.Record{Header: h, Payload: init})
	if err != nil {
		t.Fatal(err)
	}
	if err = receipt.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	c.Selection = apply.Selection{Through: receipt.Reference, Effects: []record.FrameRef{receipt.Reference}, Proof: []byte("explicit local validation selection")}
	return c
}

func startService(t *testing.T, c fsservice.Config) *fsservice.Service {
	t.Helper()
	s, err := fsservice.New(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func splitPath(p string) []string {
	var parts []string
	for p != "." {
		parts = append([]string{path.Base(p)}, parts...)
		p = path.Dir(p)
	}
	return parts
}

func TestRawNamespaceHandlesAndPermissions(t *testing.T) {
	a, _ := serviceFixture(t, "", credentialFunc(testCredentials))
	h := testHeader(1, 100)
	var created fuse.CreateOut
	if s := a.Create(nil, &fuse.CreateIn{InHeader: h, Flags: syscall.O_RDWR | syscall.O_CREAT, Mode: 0600}, "raw-\xff", &created); s != fuse.OK {
		t.Fatal(s)
	}
	file := testHeader(created.NodeId, 100)
	if n, s := a.Write(nil, &fuse.WriteIn{InHeader: file, Fh: created.Fh, Size: 3}, []byte("abc")); s != fuse.OK || n != 3 {
		t.Fatal(n, s)
	}
	if s := a.Rename(nil, &fuse.RenameIn{InHeader: h, Newdir: 1}, "raw-\xff", "renamed"); s != fuse.OK {
		t.Fatal(s)
	}
	var linked fuse.EntryOut
	if s := a.Link(nil, &fuse.LinkIn{InHeader: h, Oldnodeid: created.NodeId}, "alias", &linked); s != fuse.OK || linked.NodeId != created.NodeId {
		t.Fatal(s, linked)
	}
	a.mu.Lock()
	refs := a.nodes[created.NodeId].lookups
	a.mu.Unlock()
	if refs != 2 {
		t.Fatalf("LINK lookup count %d", refs)
	}
	other := file
	other.Uid = 1001
	other.Pid = 101
	if s := a.Open(nil, &fuse.OpenIn{InHeader: other, Flags: syscall.O_RDONLY}, &fuse.OpenOut{}); s != fuse.EACCES {
		t.Fatalf("mode denial: %v", s)
	}
	if s := a.SetAttr(nil, &fuse.SetAttrIn{SetAttrInCommon: fuse.SetAttrInCommon{InHeader: file, Valid: fuse.FATTR_MODE, Mode: 0}}, &fuse.AttrOut{}); s != fuse.OK {
		t.Fatal(s)
	}
	if s := a.Unlink(nil, &h, "renamed"); s != fuse.OK {
		t.Fatal(s)
	}
	if s := a.Unlink(nil, &h, "alias"); s != fuse.OK {
		t.Fatal(s)
	}
	a.Forget(created.NodeId, 2)
	r, s := a.Read(nil, &fuse.ReadIn{InHeader: file, Fh: created.Fh, Size: 10}, nil)
	if s != fuse.OK {
		t.Fatal(s)
	}
	data, s := r.Bytes(nil)
	r.Done()
	if s != fuse.OK || string(data) != "abc" {
		t.Fatal(string(data), s)
	}
	if s := a.Fsync(nil, &fuse.FsyncIn{InHeader: file, Fh: created.Fh}); s != fuse.OK {
		t.Fatal(s)
	}
	if s := a.Flush(nil, &fuse.FlushIn{InHeader: file, Fh: created.Fh}); s != fuse.OK {
		t.Fatal(s)
	}
	a.Release(nil, &fuse.ReleaseIn{InHeader: file, Fh: created.Fh})
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
	if _, s := a.Read(nil, &fuse.ReadIn{InHeader: file, Fh: created.Fh, Size: 1}, nil); s != status(fsapi.Stale) {
		t.Fatal(s)
	}
}

func TestInterruptedAdmissionAndDefinitiveAcceptedLookup(t *testing.T) {
	info := fsservice.FrontendInfo{Volume: testID(1), Generation: testID(2), Root: fsapi.ObjectID{Home: testID(1), Allocation: testID(3)}}
	accepted := make(chan *fsapi.Request, 1)
	var commands int
	service := dispatchFunc(func(ctx context.Context, r *fsapi.Request, c fsapi.Command) error {
		commands++
		accepted <- r
		return nil
	})
	a, err := newAdapter(Config{Credentials: credentialFunc(testCredentials)}, service, info)
	if err != nil {
		t.Fatal(err)
	}
	h := testHeader(1, 100)
	cancel := make(chan struct{})
	close(cancel)
	if s := a.Lookup(cancel, &h, "x", &fuse.EntryOut{}); s != fuse.EINTR || commands != 0 {
		t.Fatal(s, commands)
	}
	cancel = make(chan struct{})
	done := make(chan fuse.Status, 1)
	var out fuse.EntryOut
	go func() { done <- a.Lookup(cancel, &h, "x", &out) }()
	r := <-accepted
	close(cancel)
	select {
	case <-done:
		t.Fatal("accepted reference wait abandoned")
	default:
	}
	attr := fsapi.Attr{Object: fsapi.ObjectID{Home: info.Volume, Allocation: testID(4)}, Kind: fsapi.Regular, Mode: 0600, Revision: 1, Nlink: 1}
	if err := r.Completion().Complete(fsapi.Result{Attr: &attr, Generation: info.Generation}); err != nil {
		t.Fatal(err)
	}
	if s := <-done; s != fuse.OK || out.NodeId == 0 {
		t.Fatal(s, out)
	}
	if err := r.Completion().Complete(fsapi.Result{}); !errors.Is(err, fsapi.Exists) {
		t.Fatal(err)
	}
	if a.nodes[out.NodeId].lookups != 1 {
		t.Fatal("reference accounting")
	}
}

func TestLockCancellationCloseAndConversion(t *testing.T) {
	a, _ := serviceFixture(t, "", credentialFunc(testCredentials))
	h := testHeader(1, 100)
	var created fuse.CreateOut
	if s := a.Create(nil, &fuse.CreateIn{InHeader: h, Flags: syscall.O_RDWR | syscall.O_CREAT, Mode: 0666}, "locks", &created); s != fuse.OK {
		t.Fatal(s)
	}
	one := testHeader(created.NodeId, 100)
	two := testHeader(created.NodeId, 200)
	var opened fuse.OpenOut
	if s := a.Open(nil, &fuse.OpenIn{InHeader: two, Flags: syscall.O_RDWR}, &opened); s != fuse.OK {
		t.Fatal(s)
	}
	lk := fuse.LkIn{InHeader: one, Fh: created.Fh, Owner: 100, Lk: fuse.FileLock{Start: 0, End: math.MaxInt64, Typ: syscall.F_WRLCK}}
	if s := a.SetLk(nil, &lk); s != fuse.OK {
		t.Fatal(s)
	}
	contender := lk
	contender.InHeader = two
	contender.Owner = 200
	contender.Fh = opened.Fh
	if s := a.SetLk(nil, &contender); s != fuse.EAGAIN {
		t.Fatal(s)
	}
	cancel := make(chan struct{})
	done := make(chan fuse.Status, 1)
	admitted := make(chan struct{})
	service := a.service
	a.service = dispatchFunc(func(ctx context.Context, r *fsapi.Request, c fsapi.Command) error {
		err := service.Dispatch(ctx, r, c)
		if err == nil && c.Operation == fsapi.WaitLock {
			close(admitted)
		}
		return err
	})
	go func() { done <- a.SetLkw(cancel, &contender) }()
	select {
	case <-admitted:
	case <-time.After(time.Second):
		t.Fatal("waiter not admitted")
	}
	close(cancel)
	if s := <-done; s != fuse.EINTR {
		t.Fatal(s)
	}
	if s := a.Flush(nil, &fuse.FlushIn{InHeader: one, Fh: created.Fh, LockOwner: 100}); s != fuse.OK {
		t.Fatal(s)
	}
	if s := a.SetLk(nil, &contender); s != fuse.OK {
		t.Fatalf("per-fd flush did not drop fcntl: %v", s)
	}
	// Original description remains usable after descriptor close/dup semantics.
	if n, s := a.Write(nil, &fuse.WriteIn{InHeader: one, Fh: created.Fh, Size: 1}, []byte("x")); s != fuse.OK || n != 1 {
		t.Fatal(n, s)
	}
	a.Release(nil, &fuse.ReleaseIn{InHeader: one, Fh: created.Fh})
	a.Release(nil, &fuse.ReleaseIn{InHeader: two, Fh: opened.Fh})
	a.Forget(created.NodeId, 1)
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestAsyncReleaseDoesNotRepeatDescriptorClose(t *testing.T) {
	a, _ := serviceFixture(t, "", credentialFunc(testCredentials))
	h := testHeader(1, 100)
	var first fuse.CreateOut
	if s := a.Create(nil, &fuse.CreateIn{InHeader: h, Flags: syscall.O_RDWR | syscall.O_CREAT, Mode: 0666}, "locks", &first); s != fuse.OK {
		t.Fatal(s)
	}
	caller := testHeader(first.NodeId, 100)
	var second fuse.OpenOut
	if s := a.Open(nil, &fuse.OpenIn{InHeader: caller, Flags: syscall.O_RDWR}, &second); s != fuse.OK {
		t.Fatal(s)
	}
	lock := fuse.LkIn{InHeader: caller, Fh: second.Fh, Owner: 100, Lk: fuse.FileLock{End: math.MaxInt64, Typ: syscall.F_WRLCK}}
	if s := a.SetLk(nil, &lock); s != fuse.OK {
		t.Fatal(s)
	}
	if s := a.Flush(nil, &fuse.FlushIn{InHeader: caller, Fh: first.Fh, LockOwner: 100}); s != fuse.OK {
		t.Fatal(s)
	}
	if s := a.SetLk(nil, &lock); s != fuse.OK {
		t.Fatal(s)
	} // reacquired on another fd
	control := fuse.ReleaseIn{InHeader: fuse.InHeader{NodeId: first.NodeId}, Fh: first.Fh}
	a.Release(nil, &control) // real kernel async event has no task credentials
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
	other := caller
	other.Pid = 200
	var third fuse.OpenOut
	if s := a.Open(nil, &fuse.OpenIn{InHeader: other, Flags: syscall.O_RDWR}, &third); s != fuse.OK {
		t.Fatal(s)
	}
	contender := lock
	contender.InHeader = other
	contender.Owner = 200
	contender.Fh = third.Fh
	if s := a.SetLk(nil, &contender); s != fuse.EAGAIN {
		t.Fatalf("async release erased reacquired process lock: %v", s)
	}
	if s := a.Flush(nil, &fuse.FlushIn{InHeader: caller, Fh: second.Fh, LockOwner: 100}); s != fuse.OK {
		t.Fatal(s)
	}
	control.Fh = second.Fh
	a.Release(nil, &control)
	control.Fh = third.Fh
	a.Release(nil, &control)
	a.Forget(first.NodeId, 1)
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}
}

func TestErrnoCacheAndMountGates(t *testing.T) {
	seen := map[fuse.Status]bool{}
	for _, code := range fsapi.Errnos() {
		s := status(code)
		if code != fsapi.IO && code != fsapi.OK && s == fuse.EIO {
			t.Fatalf("missing mapping %v", code)
		}
		if seen[s] {
			t.Fatalf("duplicate mapping %v", code)
		}
		seen[s] = true
	}
	if status(fsapi.Errno(255)) != fuse.EIO {
		t.Fatal("unknown status")
	}
	opts := mountOptions()
	if opts.EnableSymlinkCaching || opts.EnableAcl || opts.AllowOther || opts.IDMappedMount || !opts.DisableReadDirPlus || opts.FsName != "homelabfs" || opts.Name != "homelabfs" {
		t.Fatal(opts)
	}
	a, s := serviceFixture(t, "", credentialFunc(testCredentials))
	if _, err := New(Config{Service: s, Credentials: credentialFunc(testCredentials)}); !errors.Is(err, fsapi.Unsupported) {
		t.Fatal(err)
	}
	if a.Capabilities().Mmap || a.Capabilities().Writeback || a.Capabilities().NormalPublication {
		t.Fatal(a.Capabilities())
	}
	if err := CheckMount(t.TempDir(), a.Identity()); !errors.Is(err, fsapi.Stale) {
		t.Fatal(err)
	}
}

// Keep the race detector exercising concurrent immutable identity access.
func TestIdentityConcurrent(t *testing.T) {
	a, _ := serviceFixture(t, "", credentialFunc(testCredentials))
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				_ = a.Identity()
				_ = a.Capabilities()
			}
		})
	}
	wg.Wait()
}
