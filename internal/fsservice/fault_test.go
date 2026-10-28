package fsservice

import (
	"context"
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/apply"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/vfs"
)

type failingProtocol struct {
	contractProtocol
	captured Proposal
}

func (p *failingProtocol) Protect(_ context.Context, v Proposal) ([]byte, error) {
	p.captured = cloneProposal(v)
	return nil, fsapi.Unavailable
}

type uuidStream struct {
	prefix []byte
	n      uint64
}

func (r *uuidStream) Read(p []byte) (int, error) {
	if len(r.prefix) > 0 {
		n := copy(p, r.prefix)
		r.prefix = r.prefix[n:]
		return n, nil
	}
	r.n++
	id := uid(30000 + r.n)
	for i := range p {
		p[i] = id[i%16]
	}
	return len(p), nil
}

func TestUnselectedAllocationReservationsSurviveNewWorkspace(t *testing.T) {
	f := setup(t, "sim")
	f.c.Segment.MaximumBytes = 1 << 20
	f.c.Validation = nil
	protocol := &failingProtocol{}
	f.c.Protocol = protocol
	f.c.Permissions = allowPolicy()
	f.start()
	result := f.request(fsapi.Command{Operation: fsapi.Create, Parent: f.c.Storage.Root, Name: "uncertain", Mode: 0o644, Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true}})
	if result.Code != fsapi.Unavailable || f.service.State() != Blocked {
		t.Fatal(result)
	}
	failed := protocol.captured.Frames[0]
	mutation := failed.Record.Payload.(record.Mutation)
	reserved := mutation.Intent.Arguments.(record.Create).Object
	if err := f.service.Close(); err != nil {
		t.Fatal(err)
	}
	f.service = nil
	for _, root := range f.roots {
		root.(*vfs.Simulator).Crash()
	}
	f.workspace()
	f.c.Protocol = &contractProtocol{}
	// An explicit external test selection excludes the unprotected local COMMIT;
	// durable bytes alone do not select it. Keep the full source horizon.
	f.c.Selection.Through = protocol.captured.Through
	prefix := append([]byte{}, failed.Record.Header.Operation[:]...)
	freshOp := uid(31000)
	prefix = append(prefix, freshOp[:]...)
	prefix = append(prefix, reserved.Allocation[:]...)
	freshAllocation := uid(31001)
	prefix = append(prefix, freshAllocation[:]...)
	f.c.Random = &uuidStream{prefix: prefix}
	f.start()
	if got := f.request(fsapi.Command{Operation: fsapi.Lookup, Parent: f.c.Storage.Root, Name: "uncertain"}); got.Code != fsapi.NotFound {
		t.Fatal("local COMMIT treated as accepted", got)
	}
	created := f.create(f.c.Storage.Root, "new")
	if created.Attr.Object == reserved || created.Attr.Object.Allocation != freshAllocation {
		t.Fatalf("reservation reused or collision not retried: old %v new %v", reserved, created.Attr.Object)
	}
	// Reservation bytes remain durable and independently readable after changing
	// reconstruction roots; the selected catalog never contained the old object.
	name := "allocation-" + reserved.Allocation.String() + ".hfr"
	if _, err := f.c.Reservations.Stat(t.Context(), name); err != nil {
		t.Fatal(err)
	}
}

func TestCrashAfterProtectedApplyFailureRebuildsExactBytes(t *testing.T) {
	for _, boundary := range []apply.Boundary{apply.AfterApply, apply.EffectsDurable, apply.AfterRename} {
		t.Run(string(boundary), func(t *testing.T) {
			f := setup(t, "sim")
			f.c.Segment.MaximumBytes = 1 << 20
			armed := false
			f.c.Apply.Hook = func(_ context.Context, b apply.Boundary) error {
				if armed && b == boundary {
					armed = false
					return errors.New("injected interrupted installation")
				}
				return nil
			}
			f.start()
			a := f.create(f.c.Storage.Root, "file")
			f.ok(fsapi.Command{Operation: fsapi.Write, Handle: a.Handle, Data: []byte("original-payload")})
			armed = true
			result := f.request(fsapi.Command{Operation: fsapi.Write, Handle: a.Handle, Offset: 3, Data: []byte("NEW")})
			if result.Code != fsapi.IO || f.service.State() != Blocked {
				t.Fatal("uncertain apply was served", result)
			}
			rejected := fsapi.NewRequest(fsapi.Cred{})
			if err := f.service.Dispatch(t.Context(), rejected, fsapi.Command{Operation: fsapi.Statfs}); err != fsapi.Unavailable {
				t.Fatal("blocked read admitted", err)
			}
			if err := f.service.Close(); err != nil {
				t.Fatal(err)
			}
			f.service = nil
			progress := f.c.Workspace.Progress
			for _, root := range f.roots {
				root.(*vfs.Simulator).Crash()
			}
			f.workspace()
			f.c.Workspace.Progress = progress
			f.start()
			object := f.ok(fsapi.Command{Operation: fsapi.Lookup, Parent: f.c.Storage.Root, Name: "file"})
			if object.Attr.Object != a.Attr.Object {
				t.Fatal("recovery changed allocation")
			}
			h := f.ok(fsapi.Command{Operation: fsapi.Open, Object: object.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadOnly}})
			f.data(h.Handle, []byte("oriNEWal-payload"))
		})
	}
}

func TestJournalFailurePreservesAcknowledgedPrefix(t *testing.T) {
	cases := []struct {
		name, point string
		loss        vfs.LossMode
		newData     bool
	}{
		{"candidate-retained", "after-append", vfs.Keep, false},
		{"flush-unsynced-lost", "before-sync", vfs.Lose, false},
		{"flush-unsynced-retained", "before-sync", vfs.Keep, true},
		{"flush-durable-error", "after-sync", vfs.Lose, true},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			f := setupMode(t, "sim", vfs.Config{DataLoss: test.loss})
			f.c.Segment.MaximumBytes = 1 << 20
			armed := false
			f.c.Segment.Failpoint = func(point string) error {
				if armed && point == test.point {
					armed = false
					return errors.New("journal boundary")
				}
				return nil
			}
			f.start()
			a := f.create(f.c.Storage.Root, "a")
			f.ok(fsapi.Command{Operation: fsapi.Write, Handle: a.Handle, Data: []byte("old")})
			armed = true
			r := f.request(fsapi.Command{Operation: fsapi.Write, Handle: a.Handle, Data: []byte("new")})
			if r.Code != fsapi.IO || f.service.State() != Blocked {
				t.Fatal(r)
			}
			if err := f.service.Close(); err != nil {
				t.Fatal(err)
			}
			f.service = nil
			for _, root := range f.roots {
				root.(*vfs.Simulator).Crash()
			}
			f.workspace()
			f.start()
			h := f.ok(fsapi.Command{Operation: fsapi.Open, Object: a.Attr.Object, Flags: fsapi.OpenFlags{Access: fsapi.ReadOnly}})
			want := []byte("old")
			if test.newData {
				want = []byte("new")
			}
			f.data(h.Handle, want)
		})
	}
}

func TestReservationCorruptionFailsBeforeReplay(t *testing.T) {
	f := setup(t, "sim")
	f.start()
	a := f.create(f.c.Storage.Root, "a")
	if err := f.service.Close(); err != nil {
		t.Fatal(err)
	}
	f.service = nil
	name := "allocation-" + a.Attr.Object.Allocation.String() + ".hfr"
	file, err := f.c.Reservations.Open(t.Context(), name, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.WriteAt(t.Context(), []byte{9}, 0); err != nil {
		t.Fatal(err)
	}
	if err = file.Sync(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	f.workspace()
	if _, err = New(t.Context(), f.c); !errors.Is(err, fsapi.Invalid) {
		t.Fatal("corrupt reservation admitted", err)
	}
}

func TestHandlePinFailureDoesNotExposeSuccessfulCreate(t *testing.T) {
	f := setup(t, "sim")
	f.c.Segment.MaximumBytes = 1 << 20
	effects := 0
	armed := false
	f.c.Apply.Hook = func(_ context.Context, b apply.Boundary) error {
		if armed && b == apply.BeforeApply {
			effects++
			if effects == 2 {
				return fsapi.IO
			}
		}
		return nil
	}
	f.start()
	armed = true
	r := f.request(fsapi.Command{Operation: fsapi.Create, Parent: f.c.Storage.Root, Name: "pinned", Flags: fsapi.OpenFlags{Access: fsapi.ReadWrite, Create: true}})
	if r.Code != fsapi.IO || r.Handle != 0 || r.Attr != nil || f.service.State() != Blocked {
		t.Fatal(r)
	}
}

func TestIncrementalSelectionDoesNotReapplyOldWrite(t *testing.T) {
	f := setup(t, "sim")
	f.start()
	a := f.create(f.c.Storage.Root, "a")
	f.ok(fsapi.Command{Operation: fsapi.Write, Handle: a.Handle, Data: []byte("first")})
	f.ok(fsapi.Command{Operation: fsapi.Write, Handle: a.Handle, Data: []byte("later")})
	before := f.service.applied.Slots
	// An exact already-selected history with a newer control horizon consumes no
	// extra backing slot, and cannot recreate an old WRITE over current payloads.
	r, err := f.service.reader(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.applied.Advance(t.Context(), r, cloneSelection(f.service.selection)); err != nil {
		t.Fatal(err)
	}
	if err = r.Close(); err != nil {
		t.Fatal(err)
	}
	if f.service.applied.Slots != before {
		t.Fatal("retry consumed slot")
	}
	f.data(a.Handle, []byte("later"))
}
