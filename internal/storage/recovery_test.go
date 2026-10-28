package storage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/vfs"
)

type faultFS struct {
	Filesystem
	at, count         int
	failDir, failFile string
	failed            bool
}
type faultFile struct {
	vfs.File
	owner *faultFS
	path  string
}

func (f *faultFS) step() error {
	f.count++
	if f.at > 0 && f.at == f.count {
		return fsapi.IO
	}
	return nil
}
func (f *faultFS) Open(ctx context.Context, p string, flag int, mode fs.FileMode) (vfs.File, error) {
	if flag&os.O_CREATE != 0 {
		if err := f.step(); err != nil {
			return nil, err
		}
	}
	file, err := f.Filesystem.Open(ctx, p, flag, mode)
	if err != nil {
		return nil, err
	}
	return &faultFile{file, f, p}, nil
}
func (f *faultFS) Mkdir(ctx context.Context, p string, mode fs.FileMode) error {
	if err := f.step(); err != nil {
		return err
	}
	return f.Filesystem.Mkdir(ctx, p, mode)
}
func (f *faultFS) Rename(ctx context.Context, a, b string) error {
	if err := f.step(); err != nil {
		return err
	}
	return f.Filesystem.Rename(ctx, a, b)
}
func (f *faultFS) Unlink(ctx context.Context, p string) error {
	if err := f.step(); err != nil {
		return err
	}
	return f.Filesystem.Unlink(ctx, p)
}
func (f *faultFS) SyncDir(ctx context.Context, p string) error {
	if !f.failed && p == f.failDir {
		f.failed = true
		return fsapi.IO
	}
	if err := f.step(); err != nil {
		return err
	}
	return f.Filesystem.SyncDir(ctx, p)
}
func (f *faultFile) WriteAt(ctx context.Context, b []byte, off int64) (int, error) {
	if err := f.owner.step(); err != nil {
		return 0, err
	}
	return f.File.WriteAt(ctx, b, off)
}
func (f *faultFile) Truncate(ctx context.Context, size int64) error {
	if err := f.owner.step(); err != nil {
		return err
	}
	return f.File.Truncate(ctx, size)
}
func (f *faultFile) Sync(ctx context.Context) error {
	if !f.owner.failed && f.path == f.owner.failFile {
		f.owner.failed = true
		return fsapi.IO
	}
	if err := f.owner.step(); err != nil {
		return err
	}
	return f.File.Sync(ctx)
}

func TestDurableCheckpointPublication(t *testing.T) {
	for _, backend := range []string{"simulator", "os"} {
		t.Run(backend, func(t *testing.T) {
			f := fixtureFor(t, backend)
			id := objectID(2)
			f.apply(f.create("x", fsapi.Regular, id))
			f.apply(f.write(id, 0, []byte("checkpoint")))
			image := f.image()
			checkpoint := uuid(500)
			for range 2 {
				if err := f.store.StageCheckpoint(t.Context(), checkpoint, image, []byte("retained external decision evidence")); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.store.Apply(t.Context(), f.write(id, 0, []byte("blocked"))); !errors.Is(err, fsapi.Busy) {
				t.Fatalf("checkpoint did not freeze cut: %v", err)
			}
			for range 2 {
				if err := f.store.PublishCheckpoint(t.Context(), checkpoint); err != nil {
					t.Fatal(err)
				}
			}
			for range 2 {
				if err := f.store.PublishCurrent(t.Context(), checkpoint, uuid(501)); err != nil {
					t.Fatal(err)
				}
			}
			if sim, ok := f.data.(*vfs.Simulator); ok {
				sim.Crash()
				f.journal.(*vfs.Simulator).Crash()
			}
			opened, err := Open(t.Context(), f.data, f.journal, f.config, image)
			if err != nil {
				t.Fatal(err)
			}
			f.store = opened
			selected, got, err := f.store.ReadCurrent(t.Context())
			if err != nil || selected != checkpoint || got.Position != image.Position {
				t.Fatalf("CURRENT: %v %v", selected, err)
			}
			f.apply(f.write(id, 0, []byte("new")))
			if err = f.store.PublishCurrent(t.Context(), checkpoint, uuid(502)); !errors.Is(err, fsapi.Stale) {
				t.Fatalf("published older cut over later payload: %v", err)
			}
		})
	}
}

func TestPublicationCrashWindows(t *testing.T) {
	for at := 1; at <= 50; at++ {
		t.Run(fmt.Sprint(at), func(t *testing.T) {
			f := fixtureFor(t, "simulator")
			id := objectID(2)
			f.apply(f.create("x", fsapi.Regular, id))
			f.apply(f.write(id, 0, []byte("baseline")))
			image := f.image()
			old, newID := uuid(600), uuid(601)
			if err := f.store.StageCheckpoint(t.Context(), old, image, nil); err != nil {
				t.Fatal(err)
			}
			if err := f.store.PublishCheckpoint(t.Context(), old); err != nil {
				t.Fatal(err)
			}
			if err := f.store.PublishCurrent(t.Context(), old, uuid(602)); err != nil {
				t.Fatal(err)
			}
			fault := &faultFS{Filesystem: f.data, at: at}
			f.store.data = fault
			err := f.store.StageCheckpoint(t.Context(), newID, image, []byte("new evidence"))
			if err == nil {
				err = f.store.PublishCheckpoint(t.Context(), newID)
			}
			if err == nil {
				err = f.store.PublishCurrent(t.Context(), newID, uuid(603))
			}
			succeeded := err == nil
			f.data.(*vfs.Simulator).Crash()
			f.journal.(*vfs.Simulator).Crash()
			opened, e := Open(t.Context(), f.data, f.journal, f.config, image)
			if e != nil {
				t.Fatal("lost clean baseline", e)
			}
			selected, _, e := opened.ReadCurrent(t.Context())
			if e != nil {
				t.Fatal("lost predecessor or published incomplete pointer", e)
			}
			if selected != old && selected != newID {
				t.Fatal("unexpected pointer")
			}
			if succeeded && selected != newID {
				t.Fatal("successful publication not durable")
			}
		})
	}
}

func TestCrashPayloadBaselineContamination(t *testing.T) {
	for _, mode := range []vfs.LossMode{vfs.Lose, vfs.Keep, vfs.Tear} {
		t.Run(fmt.Sprint(mode), func(t *testing.T) {
			f := fixtureFor(t, "simulator")
			root := Object{Descriptor: Descriptor{objectID(1), fsapi.Directory, uuid(4)}, Attr: attrs(objectID(1), fsapi.Directory, 2), LedgerRevision: 1, Parent: &Parent{Revision: 1, Object: objectID(1)}}
			p := f.store.position
			j, e := vfs.NewSimulator(vfs.Config{})
			if e != nil {
				t.Fatal(e)
			}
			f.journal = j
			sim, e := vfs.NewSimulator(vfs.Config{DataLoss: mode, NamespaceLoss: vfs.Keep, Seed: 13})
			if e != nil {
				t.Fatal(e)
			}
			f.data = sim
			f.store, e = Format(t.Context(), f.data, f.journal, f.config, root, p)
			if e != nil {
				t.Fatal(e)
			}
			id := objectID(2)
			f.apply(f.create("x", fsapi.Regular, id))
			f.apply(f.write(id, 0, []byte("AAAAAAAAAAAA")))
			image := f.image()
			write := f.write(id, 0, []byte("BBBBBBBBBBBB"))
			f.apply(write)
			sim.Crash()
			j.Crash()
			opened, e := Open(t.Context(), f.data, f.journal, f.config, image)
			if mode == vfs.Lose {
				if e != nil {
					t.Fatal(e)
				}
				f.store = opened
				f.apply(write)
				if got := f.read(id); string(got) != "BBBBBBBBBBBB" {
					t.Fatal(string(got))
				}
			} else if e == nil {
				t.Fatal("older catalog accepted contaminated payload")
			} else if !errors.Is(e, fsapi.Unavailable) {
				t.Fatal(e)
			}
		})
	}
}

func TestCompleteOrderedSuffixReinitializesOnlyNewObjects(t *testing.T) {
	f := fixtureFor(t, "simulator")
	baseline := f.image()
	id := objectID(2)
	var suffix []Operation
	apply := func(op Operation) { f.apply(op); suffix = append(suffix, cloneOperation(op)) }
	apply(f.create("x", fsapi.Regular, id))
	apply(f.write(id, 0, []byte("old long data")))
	apply(f.truncate(id, 0))
	apply(f.write(id, 0, []byte("new")))
	f.data.(*vfs.Simulator).Crash()
	f.journal.(*vfs.Simulator).Crash()
	opened, err := Open(t.Context(), f.data, f.journal, f.config, baseline)
	if err != nil {
		t.Fatal(err)
	}
	f.store = opened
	for _, op := range suffix {
		f.apply(op)
	}
	if got := f.read(id); string(got) != "new" {
		t.Fatalf("incomplete ordered replay: %q", got)
	}
	image := f.image()
	if len(image.Receipts) != len(suffix) {
		t.Fatal("duplicate suffix receipts")
	}
}

func TestFailedApplyClosesReadGateAndPreservesEvidence(t *testing.T) {
	for at := 1; at <= 18; at++ {
		t.Run(fmt.Sprint(at), func(t *testing.T) {
			f := fixtureFor(t, "simulator")
			before := f.image()
			op := f.create("x", fsapi.Regular, objectID(2))
			fault := &faultFS{Filesystem: f.data, at: at}
			f.store.data = fault
			err := f.store.Apply(t.Context(), op)
			if err == nil {
				return
			}
			if _, err = f.store.Stat(t.Context(), f.config.Root); !errors.Is(err, fsapi.Unavailable) {
				t.Fatal("partial apply remained readable", err)
			}
			f.data.(*vfs.Simulator).Crash()
			f.journal.(*vfs.Simulator).Crash()
			opened, e := Open(t.Context(), f.data, f.journal, f.config, before)
			if e != nil {
				if !errors.Is(e, os.ErrNotExist) {
					t.Fatal(e)
				}
				return /* incomplete private object is preserved and requires recovery discovery */
			}
			f.store = opened
			f.apply(op)
			if _, e = f.store.Lookup(t.Context(), f.config.Root, "x"); e != nil {
				t.Fatal(e)
			}
		})
	}
}

func TestAllocationCollisionAndPersistentReservation(t *testing.T) {
	f := fixtureFor(t, "simulator")
	op := uuid(701)
	first := bytes.Repeat([]byte{0x55}, 16)
	second := bytes.Repeat([]byte{0x66}, 16)
	f.store.config.Random = bytes.NewReader(append(append(append([]byte(nil), first...), first...), second...))
	a, err := f.store.Allocate(t.Context(), op)
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.store.Allocate(t.Context(), op)
	if err != nil || a != again {
		t.Fatal("allocation retry", err)
	}
	b, err := f.store.Allocate(t.Context(), uuid(702))
	if err != nil || b == a || a.Allocation[6]>>4 != 4 || a.Allocation[8]>>6 != 2 {
		t.Fatal("collision or UUIDv4", err)
	}
	image := f.image()
	f.data.(*vfs.Simulator).Crash()
	f.journal.(*vfs.Simulator).Crash()
	opened, err := Open(t.Context(), f.data, f.journal, f.config, image)
	if err != nil {
		t.Fatal(err)
	}
	if got, e := opened.Allocate(t.Context(), op); e != nil || got != a {
		t.Fatal("lost durable reservation", e)
	}
	create := f.create("conflict", fsapi.Regular, a)
	if err = opened.Apply(t.Context(), create); !errors.Is(err, fsapi.Stale) {
		t.Fatal("stole another operation's reservation", err)
	}
}

func TestDurablePublicationRetryAfterParentSyncFailure(t *testing.T) {
	f := fixtureFor(t, "simulator")
	image := f.image()
	id := uuid(800)
	if err := f.store.StageCheckpoint(t.Context(), id, image, nil); err != nil {
		t.Fatal(err)
	}
	fault := &faultFS{Filesystem: f.data, failDir: "checkpoints"}
	f.store.data = fault
	if err := f.store.PublishCheckpoint(t.Context(), id); !errors.Is(err, fsapi.IO) {
		t.Fatalf("expected sync failure: %v", err)
	}
	if !fault.failed {
		t.Fatal("fault not reached")
	}
	f.store.data = f.data
	if err := f.store.PublishCheckpoint(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	f.data.(*vfs.Simulator).Crash()
	if _, err := f.data.Stat(t.Context(), "checkpoints/"+id.String()); err != nil {
		t.Fatal("successful retry lost renamed directory", err)
	}
	if _, err := f.data.Stat(t.Context(), "staging/"+id.String()); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("successful retry left old name", err)
	}
}

func TestDurableAllocationRetryAfterSyncFailure(t *testing.T) {
	for _, kind := range []string{"file", "directory"} {
		t.Run(kind, func(t *testing.T) {
			f := fixtureFor(t, "simulator")
			image := f.image()
			op := uuid(810)
			dir := "staging/" + op.String()
			fault := &faultFS{Filesystem: f.data}
			if kind == "file" {
				fault.failFile = dir + "/allocation.json"
			} else {
				fault.failDir = dir
			}
			f.store.data = fault
			if _, err := f.store.Allocate(t.Context(), op); !errors.Is(err, fsapi.IO) {
				t.Fatalf("expected allocation sync failure: %v", err)
			}
			if !fault.failed {
				t.Fatal("fault not reached")
			}
			f.store.data = f.data
			id, err := f.store.Allocate(t.Context(), op)
			if err != nil {
				t.Fatal(err)
			}
			f.data.(*vfs.Simulator).Crash()
			opened, err := Open(t.Context(), f.data, f.journal, f.config, image)
			if err != nil {
				t.Fatal(err)
			}
			again, err := opened.Allocate(t.Context(), op)
			if err != nil || again != id {
				t.Fatal("successful retry lost reservation", err)
			}
		})
	}
}

func TestDurableCreationRetryAfterIdentitySyncFailure(t *testing.T) {
	f := fixtureFor(t, "simulator")
	base := f.image()
	op := f.create("x", fsapi.Regular, objectID(2))
	fault := &faultFS{Filesystem: f.data, failFile: objectPath(op.Object) + "/identity.json"}
	f.store.data = fault
	if err := f.store.Apply(t.Context(), op); !errors.Is(err, fsapi.IO) {
		t.Fatal(err)
	}
	if !fault.failed {
		t.Fatal("fault not reached")
	}
	opened, err := Open(t.Context(), f.data, f.journal, f.config, base)
	if err != nil {
		t.Fatal(err)
	}
	f.store = opened
	f.apply(op)
	// Capture the now-published logical state without performing Export's syncs,
	// so only the successful CREATE retry can have persisted its identity.
	image := Image{Version: 1, Volume: f.config.Volume, Root: f.config.Root, Position: f.store.position}
	for _, o := range f.store.objects {
		image.Objects = append(image.Objects, cloneObject(o))
		if o.Attr.Kind == fsapi.Regular {
			image.Payloads = append(image.Payloads, Payload{Object: o.Attr.Object, Size: 0, SHA256: sha256.Sum256(nil)})
		}
	}
	for _, e := range f.store.entries {
		image.Entries = append(image.Entries, e)
	}
	for _, r := range f.store.receipts {
		image.Receipts = append(image.Receipts, r)
	}
	sortImage(&image)
	f.data.(*vfs.Simulator).Crash()
	if _, err = Open(t.Context(), f.data, f.journal, f.config, image); err != nil {
		t.Fatal("successful retry lost identity", err)
	}
}

func TestCheckpointIdentityCannotBeReused(t *testing.T) {
	f := fixtureFor(t, "simulator")
	id := uuid(820)
	base := f.image()
	if err := f.store.StageCheckpoint(t.Context(), id, base, []byte("original")); err != nil {
		t.Fatal(err)
	}
	if err := f.store.PublishCheckpoint(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if err := f.store.PublishCurrent(t.Context(), id, uuid(821)); err != nil {
		t.Fatal(err)
	}
	if err := f.store.StageCheckpoint(t.Context(), id, base, []byte("conflict")); !errors.Is(err, fsapi.Stale) {
		t.Fatal("checkpoint identity reused", err)
	}
	f.apply(f.create("still-live", fsapi.Regular, objectID(2)))
}
