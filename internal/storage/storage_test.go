package storage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/vfs"
)

type fixture struct {
	t                   *testing.T
	store               *Store
	data, journal       Filesystem
	config              Config
	dataDir, journalDir string
	next                uint64
}

func fixtureFor(t *testing.T, backend string) *fixture {
	t.Helper()
	f := &fixture{t: t, next: 10}
	if backend == "os" {
		f.dataDir = t.TempDir()
		f.journalDir = t.TempDir()
		for _, p := range []string{f.dataDir, f.journalDir} {
			if err := os.Chmod(p, 0o700); err != nil {
				t.Fatal(err)
			}
		}
		d, e := vfs.NewOS(t.Context(), f.dataDir)
		if e != nil {
			t.Fatal(e)
		}
		j, e := vfs.NewOS(t.Context(), f.journalDir)
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { _ = d.Close(); _ = j.Close() })
		f.data = d
		f.journal = j
	} else {
		d, e := vfs.NewSimulator(vfs.Config{})
		if e != nil {
			t.Fatal(e)
		}
		j, e := vfs.NewSimulator(vfs.Config{})
		if e != nil {
			t.Fatal(e)
		}
		f.data = d
		f.journal = j
	}
	f.config = Config{Volume: uuid(1), Root: objectID(1), OwnerUID: 0}
	if backend == "os" {
		f.config.OwnerUID = uint32(os.Getuid())
	}
	root := Object{Descriptor: Descriptor{objectID(1), fsapi.Directory, uuid(4)}, Attr: attrs(objectID(1), fsapi.Directory, 2), LedgerRevision: 1, Parent: &Parent{Revision: 1, Object: objectID(1)}}
	p := fsapi.Position{Volume: uuid(1), Generation: uuid(2), Configuration: uuid(3), Epoch: 1}
	var err error
	f.store, err = Format(t.Context(), f.data, f.journal, f.config, root, p)
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func uuid(n uint64) fsapi.UUID {
	var u fsapi.UUID
	for i := 0; i < 8; i++ {
		u[15-i] = byte(n)
		n >>= 8
	}
	u[6] = 0x40
	u[8] = 0x80
	return u
}
func objectID(n uint64) fsapi.ObjectID { return fsapi.ObjectID{Home: uuid(1), Allocation: uuid(n)} }
func attrs(id fsapi.ObjectID, k fsapi.Kind, nlink uint64) fsapi.Attr {
	return fsapi.Attr{Object: id, Kind: k, UID: 1234, GID: 2345, Mode: 0o6751, Nlink: nlink, Revision: 1, Atime: fsapi.Timestamp{Seconds: -4, Nanoseconds: 17}, Mtime: fsapi.Timestamp{Seconds: 7, Nanoseconds: 55}, Ctime: fsapi.Timestamp{Seconds: 9, Nanoseconds: 4}}
}
func (f *fixture) op(k Kind, id fsapi.ObjectID) Operation {
	f.next++
	p := f.store.position
	p.Sequence++
	op := Operation{ID: uuid(f.next), Position: p, Kind: k, Object: id, Participants: []fsapi.UUID{f.config.Volume}}
	if k == Reclaim {
		o := f.get(id)
		op.ExpectedRevision = o.Attr.Revision
		op.ExpectedLedgerRevision = o.LedgerRevision
	}
	return op
}

func (f *fixture) get(id fsapi.ObjectID) Object {
	f.t.Helper()
	o, err := f.store.Stat(f.t.Context(), id)
	if err != nil {
		f.t.Fatal(err)
	}
	return o
}
func edit(o Object) ObjectChange {
	after := cloneObject(o)
	after.Attr.Revision++
	return ObjectChange{Before: o.Attr.Revision, After: after}
}
func (f *fixture) entry(name string, target *fsapi.ObjectID) EntryChange {
	before, ok := f.store.entries[entryKey{f.config.Root, name}]
	if !ok {
		before = Entry{Parent: f.config.Root, Name: name}
	}
	return EntryChange{Before: cloneEntry(before), After: Entry{Parent: f.config.Root, Name: name, Revision: before.Revision + 1, Target: target}}
}
func (f *fixture) create(name string, kind fsapi.Kind, id fsapi.ObjectID) Operation {
	op := f.op(Create, id)
	if kind == fsapi.Directory {
		op.Kind = Mkdir
	}
	if kind == fsapi.Symlink {
		op.Kind = Symlink
	}
	e := f.entry(name, &id)
	parent := edit(f.get(f.config.Root))
	child := Object{Descriptor: Descriptor{id, kind, op.ID}, Attr: attrs(id, kind, 1), LedgerRevision: 1, References: []Reference{{f.config.Root, name, e.After.Revision}}}
	if kind == fsapi.Directory {
		child.Attr.Nlink = 2
		child.Parent = &Parent{Revision: 1, Object: f.config.Root, EntryRevision: e.After.Revision}
		parent.After.Attr.Nlink++
		parent.After.LedgerRevision++
	}
	if kind == fsapi.Symlink {
		child.Text = []byte{0xff, '/', '.', '.', '/', 'x'}
		child.Attr.Size = int64(len(child.Text))
	}
	op.Objects = []ObjectChange{parent, {After: child}}
	op.Entries = []EntryChange{e}
	return op
}
func (f *fixture) apply(op Operation) {
	f.t.Helper()
	if err := f.store.Apply(f.t.Context(), op); err != nil {
		f.t.Fatalf("apply kind=%d: %v", op.Kind, err)
	}
	if err := f.store.Apply(f.t.Context(), op); err != nil {
		f.t.Fatalf("duplicate kind=%d: %v", op.Kind, err)
	}
}
func (f *fixture) write(id fsapi.ObjectID, offset int64, data []byte) Operation {
	op := f.op(Write, id)
	op.Offset = offset
	op.Data = data
	c := edit(f.get(id))
	c.After.Attr.Size = max(c.After.Attr.Size, offset+int64(len(data)))
	c.After.Attr.Mtime = fsapi.Timestamp{Seconds: 123, Nanoseconds: 567}
	op.Objects = []ObjectChange{c}
	return op
}
func (f *fixture) truncate(id fsapi.ObjectID, size int64) Operation {
	op := f.op(Truncate, id)
	op.Size = size
	c := edit(f.get(id))
	c.After.Attr.Size = size
	op.Objects = []ObjectChange{c}
	return op
}
func (f *fixture) link(id fsapi.ObjectID, name string) Operation {
	op := f.op(Link, id)
	e := f.entry(name, &id)
	c := edit(f.get(id))
	c.After.References = append(c.After.References, Reference{f.config.Root, name, e.After.Revision})
	slices.SortFunc(c.After.References, compareRef)
	c.After.LedgerRevision++
	c.After.Attr.Nlink++
	op.Objects = []ObjectChange{edit(f.get(f.config.Root)), c}
	op.Entries = []EntryChange{e}
	return op
}
func (f *fixture) unlink(name string) Operation {
	e := f.store.entries[entryKey{f.config.Root, name}]
	id := *e.Target
	c := edit(f.get(id))
	op := f.op(Unlink, id)
	c.After.References = slices.DeleteFunc(c.After.References, func(r Reference) bool { return r.Parent == f.config.Root && r.Name == name })
	c.After.LedgerRevision++
	c.After.Attr.Nlink--
	parent := edit(f.get(f.config.Root))
	if c.After.Attr.Kind == fsapi.Directory {
		op.Kind = Rmdir
		c.After.Attr.Nlink = 0
		parent.After.Attr.Nlink--
		parent.After.LedgerRevision++
	}
	op.Objects = []ObjectChange{parent, c}
	op.Entries = []EntryChange{f.entry(name, nil)}
	return op
}
func (f *fixture) rename(old, new string) Operation {
	source := f.store.entries[entryKey{f.config.Root, old}]
	id := *source.Target
	op := f.op(Rename, id)
	from := f.entry(old, nil)
	to := f.entry(new, &id)
	c := edit(f.get(id))
	c.After.References = slices.DeleteFunc(c.After.References, func(r Reference) bool { return r.Name == old && r.Parent == f.config.Root })
	c.After.References = append(c.After.References, Reference{f.config.Root, new, to.After.Revision})
	slices.SortFunc(c.After.References, compareRef)
	c.After.LedgerRevision++
	if c.After.Parent != nil {
		c.After.Parent.Revision++
		c.After.Parent.EntryRevision = to.After.Revision
	}
	parent := edit(f.get(f.config.Root))
	op.Objects = []ObjectChange{parent, c}
	if to.Before.Target != nil {
		replaced := edit(f.get(*to.Before.Target))
		replaced.After.References = slices.DeleteFunc(replaced.After.References, func(r Reference) bool { return r.Parent == f.config.Root && r.Name == new })
		replaced.After.LedgerRevision++
		replaced.After.Attr.Nlink--
		if replaced.After.Attr.Kind == fsapi.Directory {
			replaced.After.Attr.Nlink = 0
			op.Objects[0].After.Attr.Nlink--
			op.Objects[0].After.LedgerRevision++
		}
		op.Objects = append(op.Objects, replaced)
	}
	op.Entries = []EntryChange{from, to}
	return op
}
func (f *fixture) image() Image {
	f.t.Helper()
	i, err := f.store.Export(f.t.Context())
	if err != nil {
		f.t.Fatal(err)
	}
	raw, err := EncodeImage(i, f.config.Limits)
	if err != nil {
		f.t.Fatal(err)
	}
	j, err := DecodeImage(raw, f.config.Limits)
	if err != nil {
		f.t.Fatal(err)
	}
	again, err := EncodeImage(j, f.config.Limits)
	if err != nil || !bytes.Equal(raw, again) {
		f.t.Fatalf("image roundtrip: %v", err)
	}
	return j
}
func (f *fixture) read(id fsapi.ObjectID) []byte {
	f.t.Helper()
	o := f.get(id)
	p := make([]byte, o.Attr.Size)
	n, err := f.store.Read(f.t.Context(), id, p, 0)
	if err != nil || n != len(p) {
		f.t.Fatalf("read: n=%d err=%v", n, err)
	}
	return p
}

func TestPrimitiveContract(t *testing.T) {
	for _, backend := range []string{"simulator", "os"} {
		t.Run(backend, func(t *testing.T) {
			f := fixtureFor(t, backend)
			file := objectID(2)
			create := f.create("raw-\xff", fsapi.Regular, file)
			f.apply(create)
			write := f.write(file, 2, []byte("hello"))
			f.apply(write)
			if got := f.read(file); !bytes.Equal(got, []byte{0, 0, 'h', 'e', 'l', 'l', 'o'}) {
				t.Fatalf("sparse write %q", got)
			}
			f.apply(f.truncate(file, 3))
			f.apply(f.truncate(file, 6))
			if got := f.read(file); !bytes.Equal(got, []byte{0, 0, 'h', 0, 0, 0}) {
				t.Fatalf("truncate %q", got)
			}
			f.apply(f.link(file, "alias"))
			f.apply(f.rename("alias", "renamed"))
			f.apply(f.unlink("raw-\xff"))
			before := f.get(file)
			setattr := f.op(Setattr, file)
			c := edit(before)
			c.After.Attr.Mode = 0o400
			c.After.Attr.UID = 0xffffffff
			c.After.Attr.GID = 7
			c.After.Attr.Atime = fsapi.Timestamp{Seconds: -77, Nanoseconds: 999999999}
			setattr.Objects = []ObjectChange{c}
			f.apply(setattr)
			if got := f.get(file); got.Attr.UID != 0xffffffff || got.Attr.Mode != 0o400 || got.Attr.Atime != c.After.Attr.Atime {
				t.Fatal("logical attributes changed")
			}
			dir := objectID(3)
			f.apply(f.create("dir", fsapi.Directory, dir))
			f.apply(f.rename("dir", "moved-dir"))
			f.apply(f.unlink("moved-dir"))
			symlink := objectID(4)
			f.apply(f.create("symlink", fsapi.Symlink, symlink))
			if got := f.get(symlink).Text; !bytes.Equal(got, []byte{0xff, '/', '.', '.', '/', 'x'}) {
				t.Fatalf("symlink text %x", got)
			}
			if _, err := f.data.Stat(t.Context(), objectPath(symlink)+"/payload"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("symlink host payload: %v", err)
			}
			entries, err := f.store.ReadDir(t.Context(), f.config.Root, "", 1)
			if err != nil || len(entries) != 1 || entries[0].Name != "renamed" {
				t.Fatalf("readdir page: %v %v", entries, err)
			}
			entries, err = f.store.ReadDir(t.Context(), f.config.Root, "renamed", 1)
			if err != nil || len(entries) != 1 || entries[0].Name != "symlink" {
				t.Fatal("cursor")
			}
			if _, err = f.store.Lookup(t.Context(), f.config.Root, "renamed"); err != nil {
				t.Fatal(err)
			}
			if _, err = f.store.Lookup(t.Context(), f.config.Root, "raw-\xff"); !errors.Is(err, fsapi.NotFound) {
				t.Fatal(err)
			}
			image := f.image()
			reopened, err := Open(t.Context(), f.data, f.journal, f.config, image)
			if err != nil {
				t.Fatal(err)
			}
			f.store = reopened
			if err = f.store.Apply(t.Context(), create); err != nil {
				t.Fatalf("old CREATE duplicate: %v", err)
			}
			if err = f.store.Apply(t.Context(), write); err != nil {
				t.Fatalf("old WRITE duplicate: %v", err)
			}
			if got := f.read(file); !bytes.Equal(got, []byte{0, 0, 'h', 0, 0, 0}) {
				t.Fatalf("old redo changed payload %q", got)
			}
			if backend == "os" {
				if _, err = os.Lstat(filepath.Join(f.dataDir, "renamed")); !errors.Is(err, os.ErrNotExist) {
					t.Fatal("host namespace mirror exists")
				}
			}
		})
	}
}

func TestReplacementAndUnlinkedLifetime(t *testing.T) {
	for _, backend := range []string{"simulator", "os"} {
		t.Run(backend, func(t *testing.T) {
			f := fixtureFor(t, backend)
			old, replacement := objectID(2), objectID(3)
			f.apply(f.create("old", fsapi.Regular, old))
			f.apply(f.write(old, 0, []byte("original")))
			f.apply(f.create("replacement", fsapi.Regular, replacement))
			f.apply(f.write(replacement, 0, []byte("replacement")))
			pin := Pin{ID: uuid(400), Object: replacement, Generation: uuid(401), Incarnation: uuid(402), Active: true}
			p := f.op(HandlePin, replacement)
			p.Pins = []PinChange{{After: pin}}
			f.apply(p)
			rename := f.rename("old", "replacement")
			f.apply(rename)
			if got := f.read(replacement); string(got) != "replacement" {
				t.Fatalf("replaced descriptor retargeted: %q", got)
			}
			if got := f.read(old); string(got) != "original" {
				t.Fatal(string(got))
			}
			f.apply(f.write(replacement, 0, []byte("open")))
			if f.get(replacement).Attr.Nlink != 0 {
				t.Fatal("unlinked link count")
			}
			reclaim := f.op(Reclaim, replacement)
			if err := f.store.Apply(t.Context(), reclaim); !errors.Is(err, fsapi.Busy) {
				t.Fatalf("reclaimed pinned object: %v", err)
			}
			release := f.op(HandleRelease, replacement)
			released := pin
			released.Active = false
			release.Pins = []PinChange{{Before: &pin, After: released}}
			f.apply(release)
			f.apply(f.op(Reclaim, replacement))
			if _, err := f.store.Stat(t.Context(), replacement); !errors.Is(err, fsapi.NotFound) {
				t.Fatal(err)
			}
			if _, err := f.data.Stat(t.Context(), objectPath(replacement)+"/identity.json"); err != nil {
				t.Fatal("allocation tombstone lost", err)
			}
			image := f.image()
			if _, err := Open(t.Context(), f.data, f.journal, f.config, image); err != nil {
				t.Fatal(err)
			}
			again := f.create("reuse", fsapi.Regular, replacement)
			if err := f.store.Apply(t.Context(), again); err == nil {
				t.Fatal("reused allocation")
			}
			if err := f.store.Apply(t.Context(), rename); err != nil {
				t.Fatal(err)
			}
			e, err := f.store.Lookup(t.Context(), f.config.Root, "replacement")
			if err != nil || *e.Target != old {
				t.Fatal("old rename touched replacement")
			}
		})
	}
}

func TestNameBytesAndDuplicateDigest(t *testing.T) {
	f := fixtureFor(t, "simulator")
	first := f.create("\xff", fsapi.Regular, objectID(2))
	f.apply(first)
	second := f.create("\xfe", fsapi.Regular, objectID(3))
	f.apply(second)
	for _, name := range []string{"\xff", "\xfe"} {
		if _, err := f.store.Lookup(t.Context(), f.config.Root, name); err != nil {
			t.Fatal(err)
		}
	}
	conflict := cloneOperation(first)
	conflict.Entries[0].Before.Name = "\xfe"
	conflict.Entries[0].After.Name = "\xfe"
	if err := f.store.Apply(t.Context(), conflict); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("conflated raw names: %v", err)
	}
	image := f.image()
	opened, err := Open(t.Context(), f.data, f.journal, f.config, image)
	if err != nil {
		t.Fatal(err)
	}
	if err = opened.Apply(t.Context(), first); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentReadAndExport(t *testing.T) {
	f := fixtureFor(t, "simulator")
	id := objectID(2)
	f.apply(f.create("file", fsapi.Regular, id))
	f.apply(f.write(id, 0, []byte("data")))
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 20 {
				p := make([]byte, 4)
				if _, err := f.store.Read(t.Context(), id, p, 0); err != nil {
					t.Error(err)
				}
				if _, err := f.store.Export(t.Context()); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
}

func TestReadBoundsAndMetadataIsolation(t *testing.T) {
	f := fixtureFor(t, "simulator")
	id := objectID(2)
	f.apply(f.create("x", fsapi.Regular, id))
	f.apply(f.write(id, 0, []byte("abc")))
	buf := make([]byte, 8)
	n, err := f.store.Read(t.Context(), id, buf, 1)
	if n != 2 || !errors.Is(err, io.EOF) || string(buf[:n]) != "bc" {
		t.Fatalf("n=%d err=%v data=%q", n, err, buf[:n])
	}
	before := f.get(id)
	returned := f.get(id)
	returned.References[0].Name = "changed"
	if !reflect.DeepEqual(before, f.get(id)) {
		t.Fatal("shared catalog alias")
	}
	after := f.get(id)
	if before.Attr.Atime != after.Attr.Atime {
		t.Fatal("read mutated atime")
	}
	if _, err = f.store.Read(context.Background(), id, buf, -1); !errors.Is(err, fsapi.Invalid) {
		t.Fatal(err)
	}
}
