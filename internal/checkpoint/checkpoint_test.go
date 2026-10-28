package checkpoint

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/snonux/homelabfs/internal/apply"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/storage"
	"github.com/snonux/homelabfs/internal/vfs"
)

type testAuthority struct {
	trusted map[[32]byte]bool
	blocked bool
}

func (a *testAuthority) Authorize(_ context.Context, p apply.Authorization) error {
	if len(p.Selection.Proof) == 0 {
		return fsapi.Permission
	}
	return nil
}
func (a *testAuthority) AuthorizeBaseline(_ context.Context, p apply.BaselineAuthorization) error {
	if !a.trusted[p.SHA256] {
		return fsapi.Permission
	}
	return nil
}
func (a *testAuthority) Enter(_ context.Context, m Manifest) (func(), error) {
	if a.blocked || !a.trusted[m.BaselineSHA256] {
		return nil, fsapi.Permission
	}
	return func() {}, nil
}

type testDependencies struct {
	missing string
	active  bool
}

func (d *testDependencies) Acquire(_ context.Context, m Manifest) (Grant, error) {
	raw, err := encode(m)
	if err != nil {
		return Grant{}, err
	}
	d.active = true
	g := Grant{CheckpointSHA256: sha256.Sum256(raw), Through: m.Through, ReplicasKnown: true, PinsKnown: true, HoldsKnown: true, RecoveryPointsKnown: true, Proof: []byte("explicit disposable single-node fixture"), Release: func() { d.active = false }}
	switch d.missing {
	case "replicas":
		g.ReplicasKnown = false
	case "pins":
		g.PinsKnown = false
	case "holds":
		g.HoldsKnown = false
	case "points":
		g.RecoveryPointsKnown = false
	case "binding":
		g.CheckpointSHA256[0] ^= 1
	case "proof":
		g.Proof = nil
	}
	return g, nil
}

type fixture struct {
	t            *testing.T
	backend      string
	mode         vfs.Config
	source, root storage.Filesystem
	writer       *segment.Writer
	stream       segment.Stream
	selection    apply.Selection
	config       Config
	base         apply.Baseline
	next         uint64
	meta         record.Metadata
	expected     []byte
}

func uuid(n byte) fsapi.UUID       { var id fsapi.UUID; id[0] = n; id[6] = 0x40; id[8] = 0x80; return id }
func object(n byte) fsapi.ObjectID { return fsapi.ObjectID{Home: uuid(1), Allocation: uuid(n)} }
func fresh(t *testing.T, backend string, mode vfs.Config) storage.Filesystem {
	t.Helper()
	if backend == "sim" {
		s, err := vfs.NewSimulator(mode)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	p := t.TempDir()
	if err := os.Chmod(p, 0700); err != nil {
		t.Fatal(err)
	}
	s, err := vfs.NewOS(t.Context(), p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
func newFixture(t *testing.T, backend string, mode vfs.Config) *fixture {
	t.Helper()
	f := &fixture{t: t, backend: backend, mode: mode, source: fresh(t, backend, mode), root: fresh(t, backend, mode), next: 30}
	a := &testAuthority{trusted: map[[32]byte]bool{}}
	f.stream = segment.Stream{Volume: uuid(1), Generation: uuid(2), Configuration: uuid(3), Epoch: 1, Kind: 1}
	f.config = Config{Apply: apply.Config{Storage: storage.Config{Volume: uuid(1), Root: object(4)}, Workspace: uuid(10), Authorizer: a}, Authority: a, Dependencies: &testDependencies{}}
	if backend == "os" {
		f.config.Apply.Storage.OwnerUID = uint32(os.Getuid())
	}
	dir := "."
	for _, part := range strings.Split(f.stream.Directory(), "/") {
		next := path.Join(dir, part)
		if err := f.source.Mkdir(t.Context(), next, 0700); err != nil {
			t.Fatal(err)
		}
		if err := f.source.SyncDir(t.Context(), dir); err != nil {
			t.Fatal(err)
		}
		dir = next
	}
	nextSegment := byte(100)
	var err error
	f.writer, err = segment.NewWriter(t.Context(), f.source, f.stream, segment.Options{MaximumBytes: 4000, NewSegment: func() fsapi.UUID { nextSegment++; return uuid(nextSegment) }, Structural: func(uint64) record.Header { return f.header(uuid(20)) }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.writer.Close() })
	f.selection.Proof = []byte("explicit disposable selected source")
	root := record.Metadata{Revision: 1, Type: 2, Mode: 0755, Nlink: 2}
	f.append(uuid(21), record.VolumeInit{Root: object(4), Initial: root, RootParent: record.ParentState{Revision: 1, Parent: object(4)}, RootLedger: record.LedgerEdit{Object: object(4), ResultingRevision: 1}, Authority: f.authority(), Authorization: record.Artifact{Kind: 6, ID: uuid(22), Length: 1}}, true)
	f.meta = record.Metadata{Revision: 1, Type: 1, Mode: 0600, Nlink: 1}
	rootAfter := root
	rootAfter.Revision++
	ref := record.Reference{Parent: object(4), Name: "a", EntryRevision: 1}
	f.commit(record.Create{Parent: object(4), Name: "a", Object: object(5), Exclusive: true, RequestedMode: 0600}, 1, record.Redo{Objects: []record.ObjectEdit{{Object: object(4), ExpectedRevision: 1, After: rootAfter}, {Object: object(5), After: f.meta}}, Entries: []record.EntryEdit{{Parent: object(4), Name: "a", After: record.EntryState{Revision: 1, Target: ptr(object(5))}}}, Ledgers: []record.LedgerEdit{{Object: object(5), ResultingRevision: 1, ResultingNlink: 1, Add: []record.Reference{ref}}}})
	for i := range 4 {
		f.write([]byte(fmt.Sprintf("baseline-payload-%d", i)), int64(i*19))
	}
	if err = f.writer.Seal(t.Context()); err != nil {
		t.Fatal(err)
	}
	reader := f.reader(false)
	for {
		e, err := reader.Next(t.Context())
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		f.selection.Through = e.Reference
	}
	if err = reader.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := apply.Replay(t.Context(), fresh(t, backend, mode), fresh(t, backend, mode), fresh(t, backend, mode), f.reader(false), f.selection, f.config.Apply)
	if err != nil {
		t.Fatal(err)
	}
	f.base, err = r.Capture(t.Context(), f.stream, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := apply.EncodeBaseline(f.base)
	if err != nil {
		t.Fatal(err)
	}
	a.trusted[sha256.Sum256(raw)] = true
	// A surviving selected suffix changes bytes and size beyond the immutable base.
	f.write([]byte("suffix!"), 3)
	return f
}
func ptr(id fsapi.ObjectID) *fsapi.ObjectID { return &id }
func (f *fixture) header(id fsapi.UUID) record.Header {
	return record.Header{Volume: uuid(1), Generation: uuid(2), Configuration: uuid(3), Epoch: 1, Operation: id, Incarnation: uuid(7)}
}
func (f *fixture) authority() record.Authority {
	return record.Authority{Volume: uuid(1), Generation: uuid(2), Configuration: uuid(3), Epoch: 1, Owner: uuid(6), Partner: uuid(8), Members: []fsapi.UUID{uuid(6), uuid(8), uuid(9)}, CoordinatorTerm: 1, OwnerIncarnation: uuid(7), PartnerIncarnation: uuid(10)}
}
func (f *fixture) append(id fsapi.UUID, p any, selected bool) record.FrameRef {
	f.t.Helper()
	r, err := f.writer.Append(f.t.Context(), record.Record{Header: f.header(id), Payload: p})
	if err != nil {
		f.t.Fatal(err)
	}
	if err = r.Wait(f.t.Context()); err != nil {
		f.t.Fatal(err)
	}
	if selected {
		f.selection.Effects = append(f.selection.Effects, r.Reference)
	}
	f.selection.Through = r.Reference
	return r.Reference
}
func (f *fixture) commit(args any, kind uint16, redo record.Redo) {
	f.next++
	id := uuid(byte(f.next))
	ref := f.append(id, record.Mutation{Intent: record.Intent{Kind: kind, Arguments: args, SelectedMetadata: redo.Objects}, Redo: redo}, false)
	f.append(id, record.Commit{Mutation: ref, Authority: f.authority()}, true)
}
func (f *fixture) write(data []byte, off int64) {
	f.t.Helper()
	old := f.meta.Revision
	f.meta.Revision++
	size := max(len(f.expected), int(off)+len(data))
	next := make([]byte, size)
	copy(next, f.expected)
	copy(next[off:], data)
	f.expected = next
	f.meta.Size = uint64(size)
	f.commit(record.Write{Object: object(5), Offset: uint64(off), Data: data}, 8, record.Redo{Objects: []record.ObjectEdit{{Object: object(5), ExpectedRevision: old, After: f.meta}}})
}
func (f *fixture) reader(suffix bool) *segment.Reader {
	f.t.Helper()
	var r *segment.Reader
	var err error
	if suffix {
		r, err = segment.OpenCheckpointSuffix(f.t.Context(), f.source, f.stream, segment.Limits{}, f.base.Frames)
	} else {
		r, err = segment.OpenReader(f.t.Context(), f.source, f.stream, segment.RecoveryOptions{}, record.Position{Volume: uuid(1), Generation: uuid(2), Configuration: uuid(3), Epoch: 1, Sequence: 1})
	}
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = r.Close() })
	return r
}
func (f *fixture) assertRecovery() {
	f.t.Helper()
	_, base, err := Recover(f.t.Context(), f.root, f.source, f.config)
	if err != nil {
		f.t.Fatal(err)
	}
	f.base = base
	c := f.config.Apply
	c.Workspace = uuid(11)
	result, err := apply.Resume(f.t.Context(), fresh(f.t, f.backend, f.mode), fresh(f.t, f.backend, f.mode), fresh(f.t, f.backend, f.mode), f.reader(true), f.selection, c, base, 1<<20, f.config.Authority)
	if err != nil {
		f.t.Fatal(err)
	}
	actual := make([]byte, len(f.expected))
	n, err := result.Store.Read(f.t.Context(), object(5), actual, 0)
	if err != nil && err != io.EOF {
		f.t.Fatal(err)
	}
	if n != len(actual) || !bytes.Equal(actual, f.expected) {
		f.t.Fatalf("recovered payload %q want %q", actual, f.expected)
	}
	entry, err := result.Store.Lookup(f.t.Context(), object(4), "a")
	if err != nil || entry.Target == nil || *entry.Target != object(5) {
		f.t.Fatalf("recovered namespace %+v %v", entry, err)
	}
	attr, err := result.Store.Stat(f.t.Context(), object(5))
	if err != nil || attr.Attr.Revision != f.meta.Revision {
		f.t.Fatalf("recovered revision %+v %v", attr, err)
	}
}

func TestPublishTrimAndSuffixRecovery(t *testing.T) {
	for _, backend := range []string{"os", "sim"} {
		t.Run(backend, func(t *testing.T) {
			f := newFixture(t, backend, vfs.Config{})
			if _, err := Publish(t.Context(), f.root, f.source, f.base, uuid(12), []byte("checkpoint proof"), f.config); err != nil {
				t.Fatal(err)
			}
			n, err := Trim(t.Context(), f.root, f.source, f.config)
			if err != nil || n < 2 {
				t.Fatalf("trim %d %v", n, err)
			}
			if backend == "sim" {
				f.root.(*vfs.Simulator).Crash()
				f.source.(*vfs.Simulator).Crash()
			}
			f.assertRecovery()
			n, err = Trim(t.Context(), f.root, f.source, f.config)
			if err != nil || n != 0 {
				t.Fatalf("idempotent trim %d %v", n, err)
			}
		})
	}
}

func TestUnknownRetentionAndAuthorityBlockDeletion(t *testing.T) {
	for _, missing := range []string{"replicas", "pins", "holds", "points", "binding", "proof", "nil", "authority"} {
		t.Run(missing, func(t *testing.T) {
			f := newFixture(t, "sim", vfs.Config{})
			if _, err := Publish(t.Context(), f.root, f.source, f.base, uuid(12), []byte("proof"), f.config); err != nil {
				t.Fatal(err)
			}
			switch missing {
			case "nil":
				f.config.Dependencies = nil
			case "authority":
				f.config.Authority.(*testAuthority).blocked = true
			default:
				f.config.Dependencies.(*testDependencies).missing = missing
			}
			n, err := Trim(t.Context(), f.root, f.source, f.config)
			if err == nil || n != 0 {
				t.Fatalf("missing dependency permitted trim: %d %v", n, err)
			}
			r, err := segment.Recover(t.Context(), f.source, f.stream, segment.RecoveryOptions{})
			if err != nil || len(r.Segments) < 3 {
				t.Fatalf("source not retained %+v %v", r, err)
			}
		})
	}
}

func TestEveryCheckpointAndTrimBoundaryCrash(t *testing.T) {
	fault := errors.New("checkpoint crash")
	probe := newFixture(t, "sim", vfs.Config{})
	seals := 0
	for _, raw := range probe.base.Frames {
		r, err := record.Decode(raw)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := r.Payload.(record.SegmentSeal); ok {
			seals++
		}
	}
	for _, data := range []vfs.LossMode{vfs.Lose, vfs.Keep, vfs.Tear} {
		for _, ns := range []vfs.LossMode{vfs.Lose, vfs.Keep} {
			for _, boundary := range []Boundary{BaselineDurable, ManifestDurable, BeforePublish, AfterPublish, Published, BoundaryDurable, AfterUnlink, TrimDurable} {
				maxNth := 1
				if boundary == AfterUnlink || boundary == TrimDurable {
					maxNth = seals
				}
				for nth := 1; nth <= maxNth; nth++ {
					t.Run(fmt.Sprintf("data%d/ns%d/%s/%d", data, ns, boundary, nth), func(t *testing.T) {
						mode := vfs.Config{Seed: 0xc0ffee + uint64(nth), DataLoss: data, NamespaceLoss: ns}
						f := newFixture(t, "sim", mode)
						hits := 0
						c := f.config
						c.Hook = func(_ context.Context, b Boundary) error {
							if b == boundary {
								hits++
								if hits == nth {
									return fault
								}
							}
							return nil
						}
						_, err := Publish(t.Context(), f.root, f.source, f.base, uuid(12), []byte("proof"), c)
						if err == nil {
							_, err = Trim(t.Context(), f.root, f.source, c)
						}
						if !errors.Is(err, fault) {
							t.Fatalf("boundary not fired %d: %v", hits, err)
						}
						f.root.(*vfs.Simulator).Crash()
						f.source.(*vfs.Simulator).Crash()
						if _, err = f.root.Stat(t.Context(), "CURRENT"); errors.Is(err, os.ErrNotExist) {
							// No publication means no deletion; full genesis remains recoverable.
							r, err := apply.Replay(t.Context(), fresh(t, "sim", mode), fresh(t, "sim", mode), fresh(t, "sim", mode), f.reader(false), f.selection, f.config.Apply)
							if err != nil {
								t.Fatal(err)
							}
							actual := make([]byte, len(f.expected))
							if _, err = r.Store.Read(t.Context(), object(5), actual, 0); err != nil && err != io.EOF {
								t.Fatal(err)
							}
							if !bytes.Equal(actual, f.expected) {
								t.Fatal("prepublication crash lost genesis recovery")
							}
							return
						} else if err != nil {
							t.Fatal(err)
						}
						f.assertRecovery()
						if _, err = Trim(t.Context(), f.root, f.source, f.config); err != nil {
							t.Fatal(err)
						}
						f.source.(*vfs.Simulator).Crash()
						f.assertRecovery()
					})
				}
			}
		}
	}
}

func TestPublishedCorruptionBlocksTrim(t *testing.T) {
	for _, kind := range []string{"CURRENT", "manifest", "baseline", "source"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "sim", vfs.Config{})
			if _, err := Publish(t.Context(), f.root, f.source, f.base, uuid(12), []byte("proof"), f.config); err != nil {
				t.Fatal(err)
			}
			target := f.root
			name := kind
			switch kind {
			case "manifest":
				name = manifestName(uuid(12))
			case "baseline":
				name = baselineName(uuid(12))
			case "source":
				r, err := segment.Recover(t.Context(), f.source, f.stream, segment.RecoveryOptions{})
				if err != nil {
					t.Fatal(err)
				}
				target = f.source
				name = r.Segments[0].Name
			}
			file, err := target.Open(t.Context(), name, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = file.WriteAt(t.Context(), []byte{0xff}, 0); err != nil {
				t.Fatal(err)
			}
			if err = file.Close(); err != nil {
				t.Fatal(err)
			}
			n, err := Trim(t.Context(), f.root, f.source, f.config)
			if err == nil || n != 0 {
				t.Fatalf("corrupt %s allowed deletion %d %v", kind, n, err)
			}
		})
	}
}

type artifactFaultFS struct {
	storage.Filesystem
	site  string
	fault error
}
type artifactFaultFile struct {
	vfs.File
	kind, site string
	fault      error
}

func (f artifactFaultFS) Open(ctx context.Context, name string, flag int, perm os.FileMode) (vfs.File, error) {
	file, err := f.Filesystem.Open(ctx, name, flag, perm)
	if err != nil {
		return nil, err
	}
	kind := ""
	if strings.HasPrefix(name, "baseline-") {
		kind = "baseline"
	}
	if strings.HasPrefix(name, "manifest-") {
		kind = "manifest"
	}
	if strings.HasPrefix(name, "CURRENT.tmp-") {
		kind = "pointer"
	}
	return artifactFaultFile{file, kind, f.site, f.fault}, nil
}
func (f artifactFaultFile) WriteAt(ctx context.Context, p []byte, off int64) (int, error) {
	n, err := f.File.WriteAt(ctx, p, off)
	if err == nil && f.site == f.kind+".write" {
		err = f.fault
	}
	return n, err
}
func (f artifactFaultFile) Sync(ctx context.Context) error {
	if err := f.File.Sync(ctx); err != nil {
		return err
	}
	if f.site == f.kind+".sync" {
		return f.fault
	}
	return nil
}

func TestArtifactWriteAndSyncCrashesPreserveGenesis(t *testing.T) {
	fault := errors.New("artifact syscall crash")
	for _, data := range []vfs.LossMode{vfs.Lose, vfs.Keep, vfs.Tear} {
		for _, ns := range []vfs.LossMode{vfs.Lose, vfs.Keep} {
			for _, site := range []string{"baseline.write", "baseline.sync", "manifest.write", "manifest.sync", "pointer.write", "pointer.sync"} {
				t.Run(fmt.Sprintf("data%d/ns%d/%s", data, ns, site), func(t *testing.T) {
					mode := vfs.Config{Seed: 1029, DataLoss: data, NamespaceLoss: ns}
					f := newFixture(t, "sim", mode)
					_, err := Publish(t.Context(), artifactFaultFS{f.root, site, fault}, f.source, f.base, uuid(12), []byte("proof"), f.config)
					if !errors.Is(err, fault) {
						t.Fatalf("fault did not fire: %v", err)
					}
					f.root.(*vfs.Simulator).Crash()
					f.source.(*vfs.Simulator).Crash()
					if _, err = f.root.Stat(t.Context(), "CURRENT"); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("incomplete artifact was published: %v", err)
					}
					result, err := apply.Replay(t.Context(), fresh(t, "sim", mode), fresh(t, "sim", mode), fresh(t, "sim", mode), f.reader(false), f.selection, f.config.Apply)
					if err != nil {
						t.Fatal(err)
					}
					actual := make([]byte, len(f.expected))
					if _, err = result.Store.Read(t.Context(), object(5), actual, 0); err != nil && err != io.EOF {
						t.Fatal(err)
					}
					if !bytes.Equal(actual, f.expected) {
						t.Fatal("incomplete checkpoint lost genesis recovery")
					}
				})
			}
		}
	}
}

func TestOSInterruptedCheckpointAndTrimRecovery(t *testing.T) {
	fault := errors.New("OS interrupted publication or trim")
	for _, boundary := range []Boundary{BaselineDurable, ManifestDurable, BeforePublish, AfterPublish, Published, BoundaryDurable, AfterUnlink, TrimDurable} {
		t.Run(string(boundary), func(t *testing.T) {
			f := newFixture(t, "os", vfs.Config{})
			c := f.config
			c.Hook = func(_ context.Context, b Boundary) error {
				if b == boundary {
					return fault
				}
				return nil
			}
			_, err := Publish(t.Context(), f.root, f.source, f.base, uuid(12), []byte("proof"), c)
			if err == nil {
				_, err = Trim(t.Context(), f.root, f.source, c)
			}
			if !errors.Is(err, fault) {
				t.Fatalf("boundary did not fire: %v", err)
			}
			if _, err = f.root.Stat(t.Context(), "CURRENT"); errors.Is(err, os.ErrNotExist) {
				result, err := apply.Replay(t.Context(), fresh(t, "os", vfs.Config{}), fresh(t, "os", vfs.Config{}), fresh(t, "os", vfs.Config{}), f.reader(false), f.selection, f.config.Apply)
				if err != nil {
					t.Fatal(err)
				}
				actual := make([]byte, len(f.expected))
				if _, err = result.Store.Read(t.Context(), object(5), actual, 0); err != nil && err != io.EOF {
					t.Fatal(err)
				}
				if !bytes.Equal(actual, f.expected) {
					t.Fatal("interrupted OS publication lost original recovery")
				}
			} else if err != nil {
				t.Fatal(err)
			} else {
				f.assertRecovery()
				if _, err = Trim(t.Context(), f.root, f.source, f.config); err != nil {
					t.Fatal(err)
				}
				f.assertRecovery()
			}
		})
	}
}

func TestNilSourceRejected(t *testing.T) {
	f := newFixture(t, "sim", vfs.Config{})
	if _, err := Publish(t.Context(), f.root, nil, f.base, uuid(12), []byte("proof"), f.config); !errors.Is(err, fsapi.Invalid) {
		t.Fatal(err)
	}
	if _, _, err := Recover(t.Context(), f.root, nil, f.config); !errors.Is(err, fsapi.Invalid) {
		t.Fatal(err)
	}
	if _, err := Trim(t.Context(), f.root, nil, f.config); !errors.Is(err, fsapi.Invalid) {
		t.Fatal(err)
	}
}

func TestPublishPreflightRejectsOversizedInputs(t *testing.T) {
	for _, kind := range []string{"frames", "effects", "payload-count", "source-bytes", "payload-bytes", "catalog", "proof", "aggregate", "encoded"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, "sim", vfs.Config{})
			b := f.base
			c := f.config
			switch kind {
			case "frames":
				c.Apply.MaxFrames = len(b.Frames) - 1
			case "effects":
				b.Selection.Effects = append(b.Selection.Effects, make([]record.FrameRef, 4097)...)
			case "payload-count":
				c.Apply.Storage.Limits.Objects = 1
				b.Data = [][]byte{{}, {}}
			case "source-bytes":
				c.Apply.MaxBytes = 1
			case "payload-bytes":
				c.MaxPayloadBytes = 1
			case "catalog":
				c.Apply.Storage.Limits.FileBytes = 1
			case "proof":
				b.Selection.Proof = make([]byte, (1<<20)+1)
			case "aggregate":
				c.MaxArtifactBytes = 1
			case "encoded":
				// Raw content fits; JSON/base64 overhead must still fail the exact check.
				n := len(b.Catalog) + len(b.Selection.Proof) + len(b.Data) + len(b.Frames) + len(b.Selection.Effects)
				for _, raw := range b.Data {
					n += len(raw)
				}
				for _, raw := range b.Frames {
					n += len(raw)
				}
				c.MaxArtifactBytes = int64(n)
			}
			if _, err := Publish(t.Context(), f.root, f.source, b, uuid(12), []byte("proof"), c); !errors.Is(err, fsapi.NoSpace) {
				t.Fatalf("oversized %s admitted: %v", kind, err)
			}
			dir, err := f.root.OpenDir(t.Context(), ".")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = dir.Close() }()
			entries, err := dir.ReadDir(t.Context(), 1)
			if err != nil && err != io.EOF {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatal("oversized input wrote checkpoint artifacts")
			}
		})
	}
}
