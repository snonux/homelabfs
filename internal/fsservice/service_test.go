package fsservice

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/snonux/homelabfs/internal/apply"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/vfs"
)

type fixedClock struct{}

func (fixedClock) Now() fsapi.Timestamp { return fsapi.Timestamp{Seconds: 123, Nanoseconds: 456} }

type fixture struct {
	t         *testing.T
	backend   string
	c         Config
	service   *Service
	counter   uint64
	roots     []storage.Filesystem
	simConfig vfs.Config
}

func uid(n uint64) fsapi.UUID {
	var id fsapi.UUID
	id[0] = byte(n >> 8)
	id[15] = byte(n)
	id[6] = 0x40
	id[8] = 0x80
	return id
}
func (f *fixture) fresh() storage.Filesystem {
	f.t.Helper()
	var root storage.Filesystem
	if f.backend == "sim" {
		v, err := vfs.NewSimulator(f.simConfig)
		if err != nil {
			f.t.Fatal(err)
		}
		root = v
	} else {
		dir := f.t.TempDir()
		if err := os.Chmod(dir, 0o700); err != nil {
			f.t.Fatal(err)
		}
		v, err := vfs.NewOS(f.t.Context(), dir)
		if err != nil {
			f.t.Fatal(err)
		}
		f.t.Cleanup(func() { _ = v.Close() })
		root = v
	}
	f.roots = append(f.roots, root)
	return root
}
func setup(t *testing.T, backend string) *fixture { return setupMode(t, backend, vfs.Config{}) }
func setupMode(t *testing.T, backend string, simConfig vfs.Config) *fixture {
	t.Helper()
	f := &fixture{t: t, backend: backend, counter: 20000, simConfig: simConfig}
	volume := uid(1)
	root := fsapi.ObjectID{Home: volume, Allocation: uid(4)}
	f.c = Config{Stream: segment.Stream{Volume: volume, Generation: uid(2), Configuration: uid(3), Epoch: 1, Kind: 1}, Storage: storage.Config{Volume: volume, Root: root}, Clock: fixedClock{}, Validation: &ValidationProfile{Disposable: true, Name: "local fsapi contract test"}, Segment: segment.Options{MaximumBytes: 5000}}
	f.c.Authority = record.Authority{Volume: volume, Generation: uid(2), Configuration: uid(3), Epoch: 1, Owner: uid(6), Partner: uid(8), Members: []fsapi.UUID{uid(6), uid(8), uid(9)}, CoordinatorTerm: 1, OwnerIncarnation: uid(7), PartnerIncarnation: uid(10)}
	if backend == "os" {
		f.c.Storage.OwnerUID = uint32(os.Getuid())
	}
	f.c.Source = f.fresh()
	f.c.Reservations = f.fresh()
	f.workspace()
	dir := "."
	for _, part := range strings.Split(f.c.Stream.Directory(), "/") {
		next := path.Join(dir, part)
		if err := f.c.Source.Mkdir(t.Context(), next, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := f.c.Source.SyncDir(t.Context(), dir); err != nil {
			t.Fatal(err)
		}
		dir = next
	}
	h := record.Header{Volume: volume, Generation: uid(2), Configuration: uid(3), Epoch: 1, Operation: uid(90), Incarnation: uid(7)}
	w, err := segment.NewWriter(t.Context(), f.c.Source, f.c.Stream, segment.Options{MaximumBytes: 5000, Structural: func(uint64) record.Header { return h }, NewSegment: func() fsapi.UUID { return uid(80) }})
	if err != nil {
		t.Fatal(err)
	}
	init := record.VolumeInit{Root: root, Initial: record.Metadata{Revision: 1, Type: 2, UID: 100, GID: 200, Mode: 0o2775, Nlink: 2}, RootParent: record.ParentState{Revision: 1, Parent: root}, RootLedger: record.LedgerEdit{Object: root, ResultingRevision: 1}, Authority: f.c.Authority, Authorization: record.Artifact{Kind: 6, ID: uid(70), Length: 1}}
	h.Operation = uid(91)
	receipt, err := w.Append(t.Context(), record.Record{Header: h, Payload: init})
	if err != nil {
		t.Fatal(err)
	}
	if err = receipt.Wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	f.c.Selection = apply.Selection{Through: receipt.Reference, Effects: []record.FrameRef{receipt.Reference}, Proof: []byte("contract-test-external-proof")}
	t.Cleanup(func() {
		if f.service != nil {
			_ = f.service.Close()
		}
	})
	return f
}
func (f *fixture) workspace() {
	f.counter++
	f.c.Workspace = Workspace{ID: uid(f.counter), Data: f.fresh(), Journal: f.fresh(), Progress: f.fresh()}
}
func (f *fixture) start() {
	f.t.Helper()
	s, err := New(f.t.Context(), f.c)
	if err != nil {
		f.t.Fatal(err)
	}
	f.service = s
}
func (f *fixture) request(c fsapi.Command) fsapi.Result {
	f.t.Helper()
	c.Mount = f.c.Storage.Volume
	if (c.Handle != 0 || c.DirectoryRevision != 0) && !c.Generation.Valid() {
		c.Generation = f.c.Workspace.ID
	}
	if c.Operation == fsapi.RenameOp || c.Operation == fsapi.LinkOp {
		if !c.DestinationMount.Valid() {
			c.DestinationMount = c.Mount
		}
	}
	r := fsapi.NewRequest(fsapi.Cred{UID: 1000, GID: 1001, Groups: []uint32{200, 300}})
	var api fsapi.Filesystem = f.service
	if err := api.Dispatch(f.t.Context(), r, c); err != nil {
		f.t.Fatal(err)
	}
	result, err := r.Completion().Wait(f.t.Context())
	if err != nil {
		f.t.Fatal(err)
	}
	return result
}
func (f *fixture) ok(c fsapi.Command) fsapi.Result {
	f.t.Helper()
	r := f.request(c)
	if r.Code != fsapi.OK {
		f.t.Fatalf("op %d: %v", c.Operation, r.Code)
	}
	return r
}
func (f *fixture) create(parent fsapi.ObjectID, name string) fsapi.Result {
	return f.ok(fsapi.Command{Operation: fsapi.Create, Parent: parent, Name: name, Mode: 0o666, Umask: 0o027, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true, Exclusive: true}})
}
func (f *fixture) data(handle fsapi.OpenHandle, want []byte) {
	f.t.Helper()
	r := f.ok(fsapi.Command{Operation: fsapi.Read, Handle: handle, Size: 1024})
	if !bytes.Equal(r.Data, want) {
		f.t.Fatalf("read %x, want %x", r.Data, want)
	}
}

func TestNamespaceAndDataThroughFSAPI(t *testing.T) {
	for _, backend := range []string{"os", "sim"} {
		t.Run(backend, func(t *testing.T) {
			f := setup(t, backend)
			f.start()
			root := f.c.Storage.Root
			file := f.create(root, "raw-\xff")
			id := file.Attr.Object
			if file.Attr.Mode != 0o640 || file.Attr.UID != 1000 || file.Attr.GID != 200 || file.Attr.Nlink != 1 {
				t.Fatal("creation metadata", file.Attr)
			}
			f.ok(fsapi.Command{Operation: fsapi.Write, Handle: file.Handle, Offset: 5, Data: []byte("abc")})
			f.data(file.Handle, []byte{0, 0, 0, 0, 0, 'a', 'b', 'c'})
			f.ok(fsapi.Command{Operation: fsapi.LinkOp, Object: id, Destination: root, NewName: "alias"})
			alias := f.ok(fsapi.Command{Operation: fsapi.Lookup, Parent: root, Name: "alias"})
			if alias.Attr.Object != id || alias.Attr.Nlink != 2 {
				t.Fatal(alias)
			}
			before := f.ok(fsapi.Command{Operation: fsapi.Readdir, Object: root, Limit: 100})
			replacement := f.create(root, "target")
			f.ok(fsapi.Command{Operation: fsapi.Write, Handle: replacement.Handle, Data: []byte("old")})
			f.ok(fsapi.Command{Operation: fsapi.RenameOp, Parent: root, Name: "raw-\xff", Destination: root, NewName: "target"})
			f.data(replacement.Handle, []byte("old"))
			got := f.ok(fsapi.Command{Operation: fsapi.Lookup, Parent: root, Name: "target"})
			if got.Attr.Object != id {
				t.Fatal("rename changed identity")
			}
			if f.request(fsapi.Command{Operation: fsapi.Readdir, Object: root, Limit: 100, DirectoryRevision: before.DirectoryRevision}).Code != fsapi.Stale {
				t.Fatal("stale directory cursor accepted")
			}
			f.ok(fsapi.Command{Operation: fsapi.Unlink, Parent: root, Name: "alias"})
			f.ok(fsapi.Command{Operation: fsapi.Unlink, Parent: root, Name: "target"})
			f.ok(fsapi.Command{Operation: fsapi.Truncate, Handle: file.Handle, Size: 2})
			f.ok(fsapi.Command{Operation: fsapi.Truncate, Handle: file.Handle, Size: 9})
			f.data(file.Handle, make([]byte, 9))
			mode := uint16(0o600)
			at := fsapi.Timestamp{Seconds: -10, Nanoseconds: 99}
			attr := f.ok(fsapi.Command{Operation: fsapi.Setattr, Object: id, Attributes: fsapi.Attributes{Mode: &mode, Atime: &at, MtimeNow: true}}).Attr
			if attr.Nlink != 0 || attr.Mode != mode || attr.Atime != at || attr.Mtime != (fixedClock{}).Now() {
				t.Fatal(attr)
			}
			f.ok(fsapi.Command{Operation: fsapi.Sync, Handle: file.Handle, Durability: fsapi.DataDurability})
			f.ok(fsapi.Command{Operation: fsapi.Sync, Object: root, Durability: fsapi.DirectoryDurability})
			link := f.ok(fsapi.Command{Operation: fsapi.SymlinkOp, Parent: root, Name: "sym", Data: []byte("../\xff")})
			text := f.ok(fsapi.Command{Operation: fsapi.Readlink, Object: link.Attr.Object})
			if !bytes.Equal(text.Data, []byte("../\xff")) {
				t.Fatal(text)
			}
			d := f.ok(fsapi.Command{Operation: fsapi.Mkdir, Parent: root, Name: "dir", Mode: 0o755}).Attr.Object
			child := f.create(d, "child")
			if f.request(fsapi.Command{Operation: fsapi.Rmdir, Parent: root, Name: "dir"}).Code != fsapi.NotEmpty {
				t.Fatal("removed nonempty directory")
			}
			e := f.ok(fsapi.Command{Operation: fsapi.Mkdir, Parent: root, Name: "other", Mode: 0o755}).Attr.Object
			f.ok(fsapi.Command{Operation: fsapi.RenameOp, Parent: root, Name: "dir", Destination: e, NewName: "moved"})
			up := f.ok(fsapi.Command{Operation: fsapi.Lookup, Parent: d, Name: ".."})
			if up.Attr.Object != e {
				t.Fatal("parent relation")
			}
			if f.request(fsapi.Command{Operation: fsapi.RenameOp, Parent: root, Name: "other", Destination: d, NewName: "cycle"}).Code != fsapi.Invalid {
				t.Fatal("accepted cycle")
			}
			f.ok(fsapi.Command{Operation: fsapi.Unlink, Parent: d, Name: "child"})
			f.ok(fsapi.Command{Operation: fsapi.Rmdir, Parent: e, Name: "moved"})
			f.ok(fsapi.Command{Operation: fsapi.Rmdir, Parent: root, Name: "other"})
			f.ok(fsapi.Command{Operation: fsapi.Release, Handle: child.Handle})
			f.ok(fsapi.Command{Operation: fsapi.Forget, Object: child.Attr.Object, Count: 1})
			f.ok(fsapi.Command{Operation: fsapi.Release, Handle: file.Handle})
			if f.request(fsapi.Command{Operation: fsapi.Read, Handle: file.Handle, Size: 1}).Code != fsapi.Stale {
				t.Fatal("released handle reused")
			}
			if f.ok(fsapi.Command{Operation: fsapi.Statfs}).Capacity.ObjectsFree == 0 {
				t.Fatal("capacity")
			}
		})
	}
}

func TestRestartRebuildAndHandleGeneration(t *testing.T) {
	for _, backend := range []string{"os", "sim"} {
		t.Run(backend, func(t *testing.T) {
			f := setup(t, backend)
			f.start()
			root := f.c.Storage.Root
			a := f.create(root, "a")
			f.ok(fsapi.Command{Operation: fsapi.Write, Handle: a.Handle, Data: []byte("abcdefgh")})
			f.ok(fsapi.Command{Operation: fsapi.Truncate, Handle: a.Handle, Size: 3})
			f.ok(fsapi.Command{Operation: fsapi.RenameOp, Parent: root, Name: "a", Destination: root, NewName: "b"})
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
			if f.request(fsapi.Command{Operation: fsapi.Read, Handle: a.Handle, Size: 3}).Code != fsapi.Stale {
				t.Fatal("old handle survived generation")
			}
			b := f.ok(fsapi.Command{Operation: fsapi.Lookup, Parent: root, Name: "b"})
			if b.Attr.Object != a.Attr.Object {
				t.Fatal("restart changed identity")
			}
			h := f.ok(fsapi.Command{Operation: fsapi.Open, Object: b.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadOnly}})
			f.data(h.Handle, []byte("abc"))
		})
	}
}

// contractProtocol checks the external seam only. It models no real partner,
// normal durability, network, fencing or distributed evidence validation.
type contractProtocol struct {
	entered chan Proposal
	release chan struct{}
	checks  atomic.Int64
	deny    atomic.Bool
	once    sync.Once
}

func (p *contractProtocol) Check(context.Context, fsapi.Position) error {
	p.checks.Add(1)
	if p.deny.Load() {
		return fsapi.Unavailable
	}
	return nil
}
func (p *contractProtocol) Authorize(_ context.Context, a apply.Authorization) error {
	if string(a.Selection.Proof) != "contract-test-external-proof" {
		return fsapi.Permission
	}
	return nil
}
func (p *contractProtocol) Protect(_ context.Context, proposal Proposal) ([]byte, error) {
	p.once.Do(func() {
		if p.entered != nil {
			p.entered <- proposal
		}
		if p.release != nil {
			<-p.release
		}
	})
	return []byte("contract-test-external-proof"), nil
}

type permissionFunc func(context.Context, fsapi.Cred, PermissionContext) (PermissionDecision, error)

func (p permissionFunc) Check(ctx context.Context, c fsapi.Cred, v PermissionContext) (PermissionDecision, error) {
	return p(ctx, c, v)
}
func allowPolicy() PermissionPolicy {
	return permissionFunc(func(context.Context, fsapi.Cred, PermissionContext) (PermissionDecision, error) {
		return PermissionDecision{View: uid(50), Revision: 1}, nil
	})
}

func TestExternalGateCancellationQueueAndOwnership(t *testing.T) {
	f := setup(t, "sim")
	f.c.Validation = nil
	p := &contractProtocol{entered: make(chan Proposal, 1), release: make(chan struct{})}
	f.c.Protocol = p
	f.c.Permissions = allowPolicy()
	f.c.Limits.Queue = 1
	f.start()
	r := fsapi.NewRequest(fsapi.Cred{UID: 123})
	c := fsapi.Command{Operation: fsapi.Create, Mount: f.c.Storage.Volume, Parent: f.c.Storage.Root, Name: "accepted", Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true}, Mode: 0o644}
	if err := f.service.Dispatch(t.Context(), r, c); err != nil {
		t.Fatal(err)
	}
	proposal := <-p.entered
	if len(proposal.Frames) != 2 {
		t.Fatal("mutation/commit seam")
	}
	select {
	case <-r.Completion().Done():
		t.Fatal("replied before external protection")
	default:
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := r.Completion().Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	read := fsapi.NewRequest(fsapi.Cred{})
	if err := f.service.Dispatch(t.Context(), read, fsapi.Command{Operation: fsapi.Lookup, Mount: c.Mount, Parent: c.Parent, Name: c.Name}); err != nil {
		t.Fatal(err)
	}
	overflow := fsapi.NewRequest(fsapi.Cred{})
	if err := f.service.Dispatch(t.Context(), overflow, fsapi.Command{Operation: fsapi.Statfs, Mount: c.Mount}); err != fsapi.NoSpace {
		t.Fatal("queue not bounded", err)
	}
	close(p.release)
	result, err := r.Completion().Wait(t.Context())
	if err != nil || result.Code != fsapi.OK {
		t.Fatal(result, err)
	}
	seen, err := read.Completion().Wait(t.Context())
	if err != nil || seen.Attr == nil || seen.Attr.Object != result.Attr.Object {
		t.Fatal(seen, err)
	}
	data := []byte("owned")
	wr := fsapi.NewRequest(fsapi.Cred{UID: 123})
	if err = f.service.Dispatch(t.Context(), wr, fsapi.Command{Operation: fsapi.Write, Mount: c.Mount, Handle: result.Handle, Generation: f.c.Workspace.ID, Data: data}); err != nil {
		t.Fatal(err)
	}
	copy(data, "other")
	written, err := wr.Completion().Wait(t.Context())
	if err != nil || written.Code != fsapi.OK {
		t.Fatal(written, err)
	}
	f.data(result.Handle, []byte("owned"))
	p.deny.Store(true)
	r2 := f.request(fsapi.Command{Operation: fsapi.Getattr, Object: result.Attr.Object})
	if r2.Code != fsapi.Unavailable || f.service.State() != Blocked {
		t.Fatal("lost authority served", r2)
	}
}

func TestValidationIsExplicitAndPermissionsFailClosed(t *testing.T) {
	f := setup(t, "sim")
	f.c.Validation = nil
	if _, err := New(t.Context(), f.c); !errors.Is(err, fsapi.Unavailable) {
		t.Fatal("normal mode lacks mandatory trust hooks", err)
	}
	f.c.Protocol = &contractProtocol{}
	f.c.Permissions = permissionFunc(func(context.Context, fsapi.Cred, PermissionContext) (PermissionDecision, error) {
		return PermissionDecision{}, fsapi.Permission
	})
	f.start()
	if f.request(fsapi.Command{Operation: fsapi.Create, Parent: f.c.Storage.Root, Name: "denied", Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true}}).Code != fsapi.Permission {
		t.Fatal("permission bypass")
	}
	if f.service.State() != Serving {
		t.Fatal("policy denial poisoned service")
	}
}

func TestCrossVolumeAndRejectedOperations(t *testing.T) {
	f := setup(t, "sim")
	f.start()
	root := f.c.Storage.Root
	a := f.create(root, "a")
	cases := []struct {
		c    fsapi.Command
		want fsapi.Errno
	}{
		{fsapi.Command{Operation: fsapi.RenameOp, Parent: root, Name: "a", Destination: root, NewName: "b", DestinationMount: uid(99)}, fsapi.CrossDevice},
		{fsapi.Command{Operation: fsapi.LinkOp, Object: a.Attr.Object, Destination: root, NewName: "b", DestinationMount: uid(99)}, fsapi.CrossDevice},
		{fsapi.Command{Operation: fsapi.Write, Object: fsapi.ObjectID{Home: uid(99), Allocation: uid(98)}, Data: []byte("x")}, fsapi.Unsupported},
		{fsapi.Command{Operation: fsapi.Mkdir, Parent: root, Name: "../escape"}, fsapi.Invalid},
		{fsapi.Command{Operation: fsapi.LinkOp, Object: root, Destination: root, NewName: "directory-link"}, fsapi.Unsupported},
		{fsapi.Command{Operation: fsapi.Rmdir, Parent: root, Name: "a"}, fsapi.NotDirectory},
		{fsapi.Command{Operation: fsapi.Read, Handle: a.Handle, Offset: -1, Size: 1}, fsapi.Invalid},
	}
	for _, test := range cases {
		if got := f.request(test.c); got.Code != test.want {
			t.Fatalf("op %d got %v want %v", test.c.Operation, got.Code, test.want)
		}
	}
	if f.service.State() != Serving {
		t.Fatal("rejected preflight blocked serving")
	}
	f.ok(fsapi.Command{Operation: fsapi.Lookup, Parent: root, Name: "a"})
}

func TestAppendConcurrentBarrierAndResultCopies(t *testing.T) {
	f := setup(t, "sim")
	f.start()
	a := f.create(f.c.Storage.Root, "a")
	appendHandle := f.ok(fsapi.Command{Operation: fsapi.Open, Object: a.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Append: true, Sync: fsapi.IntegrityWrite}}).Handle
	requests := make([]*fsapi.Request, 4)
	for i := range requests {
		requests[i] = fsapi.NewRequest(fsapi.Cred{UID: 1000})
		if err := f.service.Dispatch(t.Context(), requests[i], fsapi.Command{Operation: fsapi.Write, Mount: f.c.Storage.Volume, Handle: appendHandle, Generation: f.c.Workspace.ID, Offset: 900, Data: []byte{byte('a' + i)}}); err != nil {
			t.Fatal(err)
		}
	}
	f.ok(fsapi.Command{Operation: fsapi.Sync, Handle: appendHandle, Durability: fsapi.FileDurability})
	for _, r := range requests {
		v, e := r.Completion().Wait(t.Context())
		if e != nil || v.Code != fsapi.OK || v.Count != 1 {
			t.Fatal(v, e)
		}
	}
	f.data(a.Handle, []byte("abcd"))
	request := fsapi.NewRequest(fsapi.Cred{})
	if err := f.service.Dispatch(t.Context(), request, fsapi.Command{Operation: fsapi.Readdir, Mount: f.c.Storage.Volume, Object: f.c.Storage.Root, Limit: 10}); err != nil {
		t.Fatal(err)
	}
	one, e := request.Completion().Wait(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	one.Entries[0].Name = "mutated"
	two, e := request.Completion().Wait(t.Context())
	if e != nil || two.Entries[0].Name != "a" {
		t.Fatal(two, e)
	}
}

func TestCreationGroupsSetIDAndCombinedSetattr(t *testing.T) {
	f := setup(t, "sim")
	f.start()
	root := f.c.Storage.Root
	ordinary := uint16(0o775)
	f.ok(fsapi.Command{Operation: fsapi.Setattr, Object: root, Attributes: fsapi.Attributes{Mode: &ordinary}})
	child := f.create(root, "plain")
	if child.Attr.GID != 200 {
		t.Fatal("FreeBSD parent-group inheritance changed", child.Attr)
	}
	special := uint16(0o6755)
	f.ok(fsapi.Command{Operation: fsapi.Setattr, Object: child.Attr.Object, Attributes: fsapi.Attributes{Mode: &special}})
	written := f.ok(fsapi.Command{Operation: fsapi.Write, Handle: child.Handle, Data: []byte("x")})
	if written.Attr.Mode&0o6000 != 0 {
		t.Fatal("non-root write kept special bits")
	}
	f.ok(fsapi.Command{Operation: fsapi.Setattr, Object: child.Attr.Object, Attributes: fsapi.Attributes{Mode: &special}})
	newUID := uint32(4321)
	changed := f.ok(fsapi.Command{Operation: fsapi.Setattr, Object: child.Attr.Object, Attributes: fsapi.Attributes{UID: &newUID}})
	if changed.Attr.Mode&0o6000 != 0 {
		t.Fatal("chown kept special bits")
	}
	if got := f.request(fsapi.Command{Operation: fsapi.Setattr, Object: child.Attr.Object, Attributes: fsapi.Attributes{Mode: &special, UID: &newUID}}); got.Code != fsapi.Unsupported || f.service.State() != Serving {
		t.Fatal("conflicting combined setattr entered journal", got)
	}
	setgid := uint16(0o2775)
	f.ok(fsapi.Command{Operation: fsapi.Setattr, Object: root, Attributes: fsapi.Attributes{Mode: &setgid}})
	directory := f.ok(fsapi.Command{Operation: fsapi.Mkdir, Parent: root, Name: "dir", Mode: 0o755}).Attr
	if directory.GID != 200 || directory.Mode&0o2000 == 0 {
		t.Fatal("directory did not inherit parent group/setgid", directory)
	}
}

func TestGenerationScopesHandlesAndDirectoryCursors(t *testing.T) {
	f := setup(t, "sim")
	f.start()
	a := f.create(f.c.Storage.Root, "a")
	oldGeneration := f.c.Workspace.ID
	cursor := f.ok(fsapi.Command{Operation: fsapi.Readdir, Object: f.c.Storage.Root, Limit: 10})
	if err := f.service.Close(); err != nil {
		t.Fatal(err)
	}
	f.service = nil
	f.workspace()
	f.start()
	opened := f.ok(fsapi.Command{Operation: fsapi.Open, Object: a.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite}})
	// Numeric handle values may repeat in a new generation; the full reference
	// cannot. Old-generation commands fail even after a new handle is installed.
	for _, c := range []fsapi.Command{{Operation: fsapi.Read, Handle: a.Handle, Generation: oldGeneration, Size: 1}, {Operation: fsapi.Readdir, Object: f.c.Storage.Root, DirectoryRevision: cursor.DirectoryRevision, Generation: oldGeneration, Limit: 10}} {
		if f.request(c).Code != fsapi.Stale {
			t.Fatal("old generation accepted")
		}
	}
	f.ok(fsapi.Command{Operation: fsapi.Write, Handle: opened.Handle, Data: []byte("new")})
}

func TestSourceRepairAndSuffixOnlyRecoveryRejected(t *testing.T) {
	f := setup(t, "sim")
	f.c.Segment.Recovery.Repair = func(context.Context, segment.Tail) error { return nil }
	if _, err := New(t.Context(), f.c); !errors.Is(err, fsapi.Invalid) {
		t.Fatal("repair callback admitted", err)
	}
	f.c.Segment.Recovery.Repair = nil
	ref := f.c.Selection.Through
	f.c.Segment.Recovery.PreviousSeal = &ref
	if _, err := New(t.Context(), f.c); !errors.Is(err, fsapi.Invalid) {
		t.Fatal("non-genesis recovery admitted", err)
	}
}

func TestRequiredCutWaitsBehindProtectedApply(t *testing.T) {
	f := setup(t, "sim")
	f.c.Validation = nil
	p := &contractProtocol{entered: make(chan Proposal, 1)}
	f.c.Protocol = p
	f.c.Permissions = allowPolicy()
	armed := false
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f.c.Apply.Hook = func(_ context.Context, b apply.Boundary) error {
		if armed && b == apply.BeforeApply {
			once.Do(func() { close(entered); <-release })
		}
		return nil
	}
	f.start()
	armed = true
	mutation := fsapi.NewRequest(fsapi.Cred{UID: 1000})
	if err := f.service.Dispatch(t.Context(), mutation, fsapi.Command{Operation: fsapi.Create, Mount: f.c.Storage.Volume, Parent: f.c.Storage.Root, Name: "waiting", Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true}}); err != nil {
		t.Fatal(err)
	}
	proposal := <-p.entered
	<-entered
	read := fsapi.NewRequest(fsapi.Cred{})
	if err := f.service.Dispatch(t.Context(), read, fsapi.Command{Operation: fsapi.Lookup, Mount: f.c.Storage.Volume, Parent: f.c.Storage.Root, Name: "waiting", Required: proposal.Through.Position}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-read.Completion().Done():
		t.Fatal("read crossed protected apply gate")
	default:
	}
	close(release)
	result, err := read.Completion().Wait(t.Context())
	if err != nil || result.Code != fsapi.OK || result.Attr == nil {
		t.Fatal(result, err)
	}
	unknown := result.Position
	unknown.Sequence += 100
	if got := f.request(fsapi.Command{Operation: fsapi.Getattr, Object: result.Attr.Object, Required: unknown}); got.Code != fsapi.Unavailable || f.service.State() != Serving {
		t.Fatal("unknown cut blocked worker", got)
	}
	f.ok(fsapi.Command{Operation: fsapi.Getattr, Object: result.Attr.Object})
}
