package apply

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/vfs"
)

type authorizeFunc func(context.Context, Authorization) error

func (f authorizeFunc) Authorize(ctx context.Context, a Authorization) error { return f(ctx, a) }

type modelObject struct {
	meta   record.Metadata
	refs   []record.Reference
	parent *record.ParentState
	data   []byte
	ledger uint64
}
type fixture struct {
	t           *testing.T
	source      storage.Filesystem
	writer      *segment.Writer
	stream      segment.Stream
	selection   Selection
	config      Config
	objects     map[fsapi.ObjectID]modelObject
	names       map[string]record.EntryState
	next        uint64
	journalHook func(string) error
}

func uid(n uint64) fsapi.UUID {
	var id fsapi.UUID
	id[0] = byte(n >> 8)
	id[15] = byte(n)
	id[6] = 0x40
	id[8] = 0x80
	return id
}
func oid(n uint64) fsapi.ObjectID { return fsapi.ObjectID{Home: uid(1), Allocation: uid(n)} }
func fresh(t *testing.T, backend string, c vfs.Config) storage.Filesystem {
	t.Helper()
	if backend == "sim" {
		s, err := vfs.NewSimulator(c)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	f, err := vfs.NewOS(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}
func newFixture(t *testing.T, backend string, modes ...vfs.Config) *fixture {
	t.Helper()
	mode := vfs.Config{}
	if len(modes) > 0 {
		mode = modes[0]
	}
	f := &fixture{t: t, source: fresh(t, backend, mode), objects: map[fsapi.ObjectID]modelObject{}, names: map[string]record.EntryState{}, next: 100}
	f.stream = segment.Stream{Volume: uid(1), Generation: uid(2), Configuration: uid(3), Epoch: 1, Kind: 1}
	f.config = Config{Storage: storage.Config{Volume: uid(1), Root: oid(4)}, Workspace: uid(1000), Authorizer: authorizeFunc(func(_ context.Context, a Authorization) error {
		if string(a.Selection.Proof) != "test-only externally selected complete history" {
			return fsapi.Permission
		}
		return nil
	})}
	if backend == "os" {
		f.config.Storage.OwnerUID = uint32(os.Getuid())
	}
	dir := "."
	for _, part := range strings.Split(f.stream.Directory(), "/") {
		next := path.Join(dir, part)
		if err := f.source.Mkdir(t.Context(), next, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := f.source.SyncDir(t.Context(), dir); err != nil {
			t.Fatal(err)
		}
		dir = next
	}
	var segmentID uint64 = 10000
	var err error
	f.writer, err = segment.NewWriter(t.Context(), f.source, f.stream, segment.Options{MaximumBytes: 4000, Failpoint: func(point string) error {
		if f.journalHook != nil {
			return f.journalHook(point)
		}
		return nil
	}, NewSegment: func() fsapi.UUID { segmentID++; return uid(segmentID) }, Structural: func(uint64) record.Header { return f.header(uid(90)) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.writer.Close() })
	m := record.Metadata{Revision: 1, Type: 2, Mode: 0755, Nlink: 2, UID: 123, GID: 456, Atime: record.Time{Seconds: -9, Nanoseconds: 5}}
	p := record.ParentState{Revision: 1, Parent: oid(4)}
	f.objects[oid(4)] = modelObject{meta: m, parent: &p, ledger: 1}
	f.selection.Proof = []byte("test-only externally selected complete history")
	f.append(uid(99), record.VolumeInit{Root: oid(4), Initial: m, RootParent: p, RootLedger: record.LedgerEdit{Object: oid(4), ResultingRevision: 1}, Authority: f.authority(), Authorization: record.Artifact{Kind: 6, ID: uid(80), Length: 1}}, true)
	return f
}
func (f *fixture) header(id fsapi.UUID) record.Header {
	return record.Header{Volume: f.stream.Volume, Generation: f.stream.Generation, Configuration: f.stream.Configuration, Epoch: 1, Operation: id, Incarnation: uid(7)}
}
func (f *fixture) authority() record.Authority {
	return record.Authority{Volume: uid(1), Generation: uid(2), Configuration: uid(3), Epoch: 1, Owner: uid(6), Partner: uid(8), Members: []fsapi.UUID{uid(6), uid(8), uid(9)}, CoordinatorTerm: 1, OwnerIncarnation: uid(7), PartnerIncarnation: uid(10)}
}
func (f *fixture) append(id fsapi.UUID, p any, selected bool) record.FrameRef {
	f.t.Helper()
	receipt, err := f.writer.Append(f.t.Context(), record.Record{Header: f.header(id), Payload: p})
	if err != nil {
		f.t.Fatal(err)
	}
	if err = receipt.Wait(f.t.Context()); err != nil {
		f.t.Fatal(err)
	}
	if selected {
		f.selection.Effects = append(f.selection.Effects, receipt.Reference)
	}
	f.selection.Through = receipt.Reference
	return receipt.Reference
}
func (f *fixture) commit(args any, kind uint16, redo record.Redo) {
	f.t.Helper()
	f.next++
	id := uid(f.next)
	sort.Slice(redo.Objects, func(i, j int) bool { return compareID(redo.Objects[i].Object, redo.Objects[j].Object) < 0 })
	sort.Slice(redo.Ledgers, func(i, j int) bool { return compareID(redo.Ledgers[i].Object, redo.Ledgers[j].Object) < 0 })
	sort.Slice(redo.Entries, func(i, j int) bool {
		a, b := redo.Entries[i], redo.Entries[j]
		if n := compareID(a.Parent, b.Parent); n != 0 {
			return n < 0
		}
		if len(a.Name) != len(b.Name) {
			return len(a.Name) < len(b.Name)
		}
		return a.Name < b.Name
	})
	m := record.Mutation{Intent: record.Intent{Kind: kind, Arguments: args, SelectedMetadata: slices.Clone(redo.Objects)}, Redo: redo}
	ref := f.append(id, m, false)
	f.append(id, record.Commit{Mutation: ref, Authority: f.authority()}, true)
}
func (f *fixture) change(id fsapi.ObjectID) record.ObjectEdit {
	o := f.objects[id]
	before := o.meta.Revision
	o.meta.Revision++
	o.meta.Ctime = record.Time{Seconds: int64(f.next), Nanoseconds: 11}
	f.objects[id] = o
	return record.ObjectEdit{Object: id, ExpectedRevision: before, After: o.meta}
}
func (f *fixture) entry(name string, target *fsapi.ObjectID) record.EntryEdit {
	old := f.names[name]
	next := record.EntryState{Revision: old.Revision + 1, Target: target}
	f.names[name] = next
	return record.EntryEdit{Parent: oid(4), Name: name, Before: old, After: next}
}
func (f *fixture) create(name string, id fsapi.ObjectID) {
	f.t.Helper()
	entry := f.entry(name, &id)
	m := record.Metadata{Revision: 1, Type: 1, UID: 1000, GID: 2000, Mode: 0644, Nlink: 1}
	ref := record.Reference{Parent: oid(4), Name: name, EntryRevision: entry.After.Revision}
	f.objects[id] = modelObject{meta: m, refs: []record.Reference{ref}, ledger: 1}
	f.commit(record.Create{Parent: oid(4), Name: name, Object: id, Exclusive: true, RequestedMode: 0644}, 1, record.Redo{Objects: []record.ObjectEdit{f.change(oid(4)), {Object: id, After: m}}, Entries: []record.EntryEdit{entry}, Ledgers: []record.LedgerEdit{{Object: id, ResultingRevision: 1, ResultingNlink: 1, Add: []record.Reference{ref}}}})
}
func (f *fixture) write(id fsapi.ObjectID, off int, data []byte) {
	o := f.objects[id]
	size := max(len(o.data), off+len(data))
	buf := make([]byte, size)
	copy(buf, o.data)
	copy(buf[off:], data)
	o.data = buf
	o.meta.Size = uint64(size)
	f.objects[id] = o
	f.commit(record.Write{Object: id, Offset: uint64(off), Data: data}, 8, record.Redo{Objects: []record.ObjectEdit{f.change(id)}})
}
func (f *fixture) truncate(id fsapi.ObjectID, size int) {
	o := f.objects[id]
	data := make([]byte, size)
	copy(data, o.data)
	o.data = data
	o.meta.Size = uint64(size)
	f.objects[id] = o
	f.commit(record.Truncate{Object: id, Size: uint64(size)}, 9, record.Redo{Objects: []record.ObjectEdit{f.change(id)}})
}
func (f *fixture) ledger(id fsapi.ObjectID, remove, add []record.Reference) record.LedgerEdit {
	o := f.objects[id]
	before := uint64(len(o.refs))
	revision := o.ledger
	for _, r := range remove {
		n := slices.Index(o.refs, r)
		if n < 0 {
			f.t.Fatal("oracle remove")
		}
		o.refs = slices.Delete(o.refs, n, n+1)
	}
	o.refs = append(o.refs, add...)
	sort.Slice(o.refs, func(i, j int) bool { return o.refs[i].Name < o.refs[j].Name })
	if o.meta.Type != 2 {
		o.meta.Nlink = uint64(len(o.refs))
	}
	o.ledger++
	f.objects[id] = o
	return record.LedgerEdit{Object: id, ExpectedRevision: revision, ResultingRevision: revision + 1, ExpectedNlink: before, ResultingNlink: uint64(len(o.refs)), Remove: remove, Add: add}
}

func (f *fixture) unlink(name string) {
	id := *f.names[name].Target
	old := f.names[name]
	entry := f.entry(name, nil)
	ledger := f.ledger(id, []record.Reference{{Parent: oid(4), Name: name, EntryRevision: old.Revision}}, nil)
	f.commit(record.Unlink{Parent: oid(4), Name: name, Removed: id}, 5, record.Redo{Objects: []record.ObjectEdit{f.change(oid(4)), f.change(id)}, Entries: []record.EntryEdit{entry}, Ledgers: []record.LedgerEdit{ledger}})
}
func (f *fixture) rename(from, to string) {
	id := *f.names[from].Target
	src, dst := f.names[from], f.names[to]
	remove := f.entry(from, nil)
	add := f.entry(to, &id)
	ledgers := []record.LedgerEdit{f.ledger(id, []record.Reference{{Parent: oid(4), Name: from, EntryRevision: src.Revision}}, []record.Reference{{Parent: oid(4), Name: to, EntryRevision: add.After.Revision}})}
	objects := []record.ObjectEdit{f.change(oid(4)), f.change(id)}
	if dst.Target != nil {
		replaced := *dst.Target
		ledgers = append(ledgers, f.ledger(replaced, []record.Reference{{Parent: oid(4), Name: to, EntryRevision: dst.Revision}}, nil))
		objects = append(objects, f.change(replaced))
	}
	f.commit(record.Rename{SourceParent: oid(4), SourceName: from, Source: id, DestinationParent: oid(4), DestinationName: to, Policy: 1, Replaced: dst.Target}, 7, record.Redo{Objects: objects, Entries: []record.EntryEdit{remove, add}, Ledgers: ledgers})
}
func (f *fixture) position(n uint64) record.Position {
	return record.Position{Volume: uid(1), Generation: uid(2), Configuration: uid(3), Epoch: 1, Sequence: n}
}
func (f *fixture) reader() *segment.Reader {
	f.t.Helper()
	r, err := segment.OpenReader(f.t.Context(), f.source, f.stream, segment.RecoveryOptions{}, f.position(1))
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = r.Close() })
	return r
}
func (f *fixture) replay(d, j, p storage.Filesystem, c Config) (Result, error) {
	return Replay(f.t.Context(), d, j, p, f.reader(), f.selection, c)
}
func (f *fixture) oracle(s *storage.Store) {
	f.t.Helper()
	for id, o := range f.objects {
		actual, err := s.Stat(f.t.Context(), id)
		if err != nil {
			f.t.Fatal(err)
		}
		if actual.LedgerRevision != o.ledger || actual.Attr != metadata(id, o.meta) {
			f.t.Fatalf("metadata %v: got %+v want %+v", id, actual.Attr, o.meta)
		}

		refs := make([]storage.Reference, 0, len(o.refs))
		for _, ref := range o.refs {
			refs = append(refs, storage.Reference{Parent: ref.Parent, Name: ref.Name, Revision: ref.EntryRevision})
		}
		sort.Slice(refs, func(i, j int) bool {
			if n := compareID(refs[i].Parent, refs[j].Parent); n != 0 {
				return n < 0
			}
			return refs[i].Name < refs[j].Name
		})
		if !slices.Equal(actual.References, refs) || !bytes.Equal(actual.Text, o.meta.SymlinkText) {
			f.t.Fatalf("object identity ledger/text mismatch: %+v", actual)
		}
		if o.parent != nil {
			if actual.Parent == nil || actual.Parent.Revision != o.parent.Revision || actual.Parent.Object != o.parent.Parent || actual.Parent.EntryRevision != o.parent.ParentEntryRevision {
				f.t.Fatalf("parent mismatch %+v != %+v", actual.Parent, o.parent)
			}
		} else if actual.Parent != nil {
			f.t.Fatal("unexpected parent")
		}
		if actual.Attr.Kind == fsapi.Regular {
			buf := make([]byte, len(o.data))
			if len(buf) > 0 {
				n, err := s.Read(f.t.Context(), id, buf, 0)
				if err != nil || n != len(buf) {
					f.t.Fatalf("read %v %d %v", id, n, err)
				}
			}
			if !bytes.Equal(buf, o.data) {
				f.t.Fatalf("payload %v: %x != %x", id, buf, o.data)
			}
		}
	}
	for name, want := range f.names {
		got, err := s.Lookup(f.t.Context(), oid(4), name)
		if want.Target == nil {
			if !errors.Is(err, fsapi.NotFound) {
				f.t.Fatalf("removed name %q: %v", name, err)
			}
		} else if err != nil || got.Target == nil || *got.Target != *want.Target || got.Revision != want.Revision {
			f.t.Fatalf("name %q: %+v %v != %+v", name, got, err, want)
		}
	}
}
func (f *fixture) history() {
	f.create("old", oid(20))
	f.write(oid(20), 0, []byte("old retained payload"))
	f.create("new", oid(21))
	f.write(oid(21), 0, []byte("replacement source"))
	f.next++
	f.append(uid(f.next), record.HandlePin{Pin: uid(70), Object: oid(20), ServingGeneration: uid(71), Incarnation: uid(7), Reasons: 7}, true)
	f.rename("new", "old")
	f.create("new", oid(22))
	f.write(oid(22), 2, []byte("new occupant"))
	f.truncate(oid(21), 3)
	f.truncate(oid(21), 12)
	f.unlink("old")
	// The descriptor retained from the replaced name still addresses the old ID.
	f.write(oid(20), 4, []byte("OPEN"))
	// A candidate with no selected commit must never change published bytes.
	o := f.objects[oid(22)]
	m := o.meta
	m.Revision++
	m.Size = 100
	f.append(uid(400), record.Mutation{Intent: record.Intent{Kind: 8, Arguments: record.Write{Object: oid(22), Data: []byte("uncommitted")}, SelectedMetadata: []record.ObjectEdit{{Object: oid(22), ExpectedRevision: o.meta.Revision, After: m}}}, Redo: record.Redo{Objects: []record.ObjectEdit{{Object: oid(22), ExpectedRevision: o.meta.Revision, After: m}}}}, false)
}
func TestReplayOSAndSimulator(t *testing.T) {
	for _, backend := range []string{"os", "sim"} {
		t.Run(backend, func(t *testing.T) {
			f := newFixture(t, backend)
			f.history()
			p := fresh(t, backend, vfs.Config{})
			d, j := fresh(t, backend, vfs.Config{}), fresh(t, backend, vfs.Config{})
			result, err := f.replay(d, j, p, f.config)
			if err != nil {
				t.Fatal(err)
			}
			f.oracle(result.Store)
			image, err := result.Store.Export(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if len(image.Pins) != 1 || !image.Pins[0].Active {
				t.Fatal("lost conservative open pin")
			}
			f.config.Workspace = uid(1001)
			again, err := f.replay(fresh(t, backend, vfs.Config{}), fresh(t, backend, vfs.Config{}), p, f.config)
			if err != nil {
				t.Fatal(err)
			}
			f.oracle(again.Store)
			if again.Slots != result.Slots || again.Applied != result.Applied {
				t.Fatal("replay changed progress")
			}
			if _, err = p.Stat(t.Context(), "CURRENT"); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("apply manufactured checkpoint CURRENT: %v", err)
			}
		})
	}
}
func TestSelectionFailsClosedBeforeFormatting(t *testing.T) {
	for _, test := range []string{"no-authority", "no-proof", "deny", "missing-effect", "wrong-hash", "gap-selection", "missing-genesis", "wrong-through", "limit"} {
		t.Run(test, func(t *testing.T) {
			f := newFixture(t, "sim")
			f.create("a", oid(20))
			switch test {
			case "no-authority":
				f.config.Authorizer = nil
			case "no-proof":
				f.selection.Proof = nil
			case "deny":
				f.config.Authorizer = authorizeFunc(func(context.Context, Authorization) error { return fsapi.Permission })
			case "missing-effect":
				f.selection.Effects = append(f.selection.Effects, record.FrameRef{StreamKind: 1, Position: f.position(999)})
			case "wrong-hash":
				f.selection.Effects[1].SHA256[0] ^= 1
			case "gap-selection":
				f.selection.Effects[1], f.selection.Effects[0] = f.selection.Effects[0], f.selection.Effects[1]
			case "missing-genesis":
				f.selection.Effects = f.selection.Effects[1:]
			case "wrong-through":
				f.selection.Through.SHA256[0] ^= 1
			case "limit":
				f.config.MaxFrames = 1
			}
			d := fresh(t, "sim", vfs.Config{})
			_, err := f.replay(d, fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), f.config)
			if err == nil {
				t.Fatal("accepted invalid selection")
			}
			if _, err = d.Stat(t.Context(), "format.json"); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalid authorization mutated destination")
			}
		})
	}
}
func TestPublishedPrefixCannotBeDiscarded(t *testing.T) {
	f := newFixture(t, "sim")
	f.create("a", oid(20))
	p := fresh(t, "sim", vfs.Config{})
	if _, err := f.replay(fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), p, f.config); err != nil {
		t.Fatal(err)
	}
	f.config.Workspace = uid(1001)
	f.selection.Effects = f.selection.Effects[:1]
	if _, err := f.replay(fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), p, f.config); !errors.Is(err, fsapi.Stale) {
		t.Fatalf("forgot published effect: %v", err)
	}
}
func TestProgressBinaryStrict(t *testing.T) {
	p := progressRecord{workspace: uid(8), index: 1, slots: 1, source: record.FrameRef{StreamKind: 1, Position: record.Position{Volume: uid(1), Generation: uid(2), Configuration: uid(3), Epoch: 1, Sequence: 8}}, imageBytes: 123}
	raw := encodeProgress(p)
	if len(raw) != progressSize {
		t.Fatalf("progress bytes %d", len(raw))
	}
	got, err := decodeProgress(raw)
	if err != nil || !reflect.DeepEqual(got, p) {
		t.Fatalf("progress roundtrip %+v %v", got, err)
	}
	for n := range raw {
		bad := slices.Clone(raw)
		bad[n] ^= 1
		if _, err := decodeProgress(bad); err == nil {
			t.Fatalf("accepted bit flip %d", n)
		}
	}
}

func TestOldCommitRetryCannotRetargetReusedName(t *testing.T) {
	f := newFixture(t, "sim")
	f.create("a", oid(20))
	original := f.selection.Effects[1]
	reader := f.reader()
	var command record.Record
	for {
		entry, err := reader.Next(t.Context())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if entry.Reference == original {
			command = entry.Record
		}
	}
	f.write(oid(20), 0, []byte("old"))
	f.unlink("a")
	f.create("a", oid(21))
	f.write(oid(21), 0, []byte("replacement"))
	f.append(command.Header.Operation, command.Payload, true)
	result, err := f.replay(fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), fresh(t, "sim", vfs.Config{}), f.config)
	if err != nil {
		t.Fatal(err)
	}
	f.oracle(result.Store)
	if result.Slots != 5 {
		t.Fatalf("duplicate consumed backing slot: %d", result.Slots)
	}
}

func TestAppliedPublicationFollowsDurableEffects(t *testing.T) {
	f := newFixture(t, "sim")
	f.create("a", oid(20))
	f.write(oid(20), 0, []byte("0123456789"))
	f.truncate(oid(20), 2)
	f.truncate(oid(20), 14)
	for nth := 1; nth <= len(f.selection.Effects); nth++ {
		t.Run(fmt.Sprintf("effect%d", nth), func(t *testing.T) {
			c := vfs.Config{Seed: 912, DataLoss: vfs.Lose, NamespaceLoss: vfs.Lose}
			d, j, p := fresh(t, "sim", c), fresh(t, "sim", c), fresh(t, "sim", c)
			config := f.config
			count := 0
			stop := errors.New("published cut crash")
			config.Hook = func(_ context.Context, b Boundary) error {
				if b == AppliedDurable {
					count++
					if count == nth {
						return stop
					}
				}
				return nil
			}
			if _, err := f.replay(d, j, p, config); !errors.Is(err, stop) {
				t.Fatal(err)
			}
			for _, root := range []storage.Filesystem{d, j, p} {
				root.(*vfs.Simulator).Crash()
			}
			raw, err := readFile(t.Context(), p, "APPLIED", 0, progressSize)
			if err != nil {
				t.Fatal(err)
			}
			position, err := decodeProgress(raw)
			if err != nil {
				t.Fatal(err)
			}
			if position.index != uint64(nth-1) {
				t.Fatal("wrong durable applied effect")
			}
			imageBytes, err := readFile(t.Context(), p, imageName(position.workspace, position.index), 0, maxImageBytes(config))
			if err != nil {
				t.Fatal(err)
			}
			image, err := storage.DecodeImage(imageBytes, config.Storage.Limits)
			if err != nil {
				t.Fatal(err)
			}
			reopened, err := storage.Open(t.Context(), d, j, config.Storage, image)
			if err != nil {
				t.Fatalf("APPLIED preceded durable effects: %v", err)
			}
			actual, err := reopened.Export(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			encoded, err := storage.EncodeImage(actual, config.Storage.Limits)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(encoded, imageBytes) {
				t.Fatal("durable applied image changed after restart")
			}
		})
	}
}
