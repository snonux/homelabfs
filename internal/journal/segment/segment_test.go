package segment

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"os"
	"path"
	"strings"
	"sync"
	"testing"

	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/vfs"
)

func id(n byte) record.UUID { return record.UUID{0: n, 15: n} }

func testStream(kind uint8) Stream {
	s := Stream{Volume: id(1), Generation: id(2), Configuration: id(3), Epoch: 1, Kind: kind}
	if kind == 2 {
		s.Reporter = id(4)
		s.ReporterIncarnation = id(5)
	}
	return s
}

func testOptions(s Stream) Options {
	n := byte(10)
	return Options{MaximumBytes: 1200, NewSegment: func() record.UUID { n++; return id(n) }, Structural: func(seq uint64) record.Header { return testRecord(s).Header }}
}

func testRecord(s Stream) record.Record {
	h := record.Header{Volume: s.Volume, Generation: s.Generation, Configuration: s.Configuration, Epoch: s.Epoch, Operation: id(6), Incarnation: id(5)}
	var p any = record.HandlePin{Pin: id(7), Object: record.ObjectID{Home: s.Volume, Allocation: id(8)}, ServingGeneration: id(9), Incarnation: id(5), Reasons: 1}
	if s.Kind == 2 {
		p = record.TailReport{RecoveryID: id(9), Reporter: s.Reporter, ReporterIncarnation: s.ReporterIncarnation}
	}
	return record.Record{Header: h, Payload: p}
}

func mkdirStream(t *testing.T, b Backend, s Stream) {
	t.Helper()
	ctx := context.Background()
	dir := "."
	for _, part := range strings.Split(s.Directory(), "/") {
		next := path.Join(dir, part)
		if err := b.Mkdir(ctx, next, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
			t.Fatal(err)
		}
		if err := b.SyncDir(ctx, dir); err != nil {
			t.Fatal(err)
		}
		dir = next
	}
}

func simulator(t *testing.T, c vfs.Config, s Stream) *vfs.Simulator {
	t.Helper()
	sim, err := vfs.NewSimulator(c)
	if err != nil {
		t.Fatal(err)
	}
	mkdirStream(t, sim, s)
	return sim
}

func mustWriter(t *testing.T, b Backend, s Stream, o Options) *Writer {
	t.Helper()
	w, err := NewWriter(context.Background(), b, s, o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func mustAppend(t *testing.T, w *Writer, s Stream) Receipt {
	t.Helper()
	r, err := w.Append(context.Background(), testRecord(s))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func readAll(t *testing.T, b Backend, s Stream) []Entry {
	t.Helper()
	ctx := context.Background()
	r, err := OpenReader(ctx, b, s, RecoveryOptions{}, s.position(1))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	var entries []Entry
	for {
		e, err := r.Next(ctx)
		if errors.Is(err, io.EOF) {
			return entries
		}
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, e)
	}
}

func TestRoundTripBothBackendsAndStreams(t *testing.T) {
	for _, kind := range []uint8{1, 2} {
		for _, backend := range []string{"os", "sim"} {
			t.Run(fmt.Sprintf("%s/%d", backend, kind), func(t *testing.T) {
				ctx := context.Background()
				s := testStream(kind)
				var b Backend
				if backend == "sim" {
					b = simulator(t, vfs.Config{}, s)
				} else {
					osfs, err := vfs.NewOS(ctx, t.TempDir())
					if err != nil {
						t.Fatal(err)
					}
					defer func() { _ = osfs.Close() }()
					b = osfs
					mkdirStream(t, b, s)
				}
				o := testOptions(s)
				w := mustWriter(t, b, s, o)
				var refs []record.FrameRef
				for range 8 {
					receipt := mustAppend(t, w, s)
					if err := receipt.Wait(ctx); err != nil {
						t.Fatal(err)
					}
					refs = append(refs, receipt.Reference)
				}
				if err := w.Seal(ctx); err != nil {
					t.Fatal(err)
				}
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				if sim, ok := b.(*vfs.Simulator); ok {
					sim.Crash()
				}
				entries := readAll(t, b, s)
				seen := 0
				var previous uint64
				for _, entry := range entries {
					if entry.Reference.Position.Sequence != previous+1 || !s.referenceMatches(entry.Reference) {
						t.Fatal("stream/sequence mismatch")
					}
					previous++
					switch entry.Record.Payload.(type) {
					case record.SegmentOpen, record.SegmentSeal:
					default:
						if !sameReference(&entry.Reference, &refs[seen]) {
							t.Fatal("append bytes/reference changed")
						}
						seen++
					}
				}
				if seen != len(refs) || len(entries) <= 10 {
					t.Fatal("rotation did not retain all records and structural positions")
				}
				// Resume a sealed stream and preserve its exact predecessor chain.
				w = mustWriter(t, b, s, o)
				mustAppend(t, w, s)
				if err := w.Close(); err != nil {
					t.Fatal(err)
				}
				_ = readAll(t, b, s)
			})
		}
	}
}

func TestGroupCommitCoveringBoundary(t *testing.T) {
	ctx := context.Background()
	s := testStream(1)
	sim := simulator(t, vfs.Config{}, s)
	o := testOptions(s)
	o.GroupCommit = true
	o.MaximumBytes = 1 << 20
	w := mustWriter(t, sim, s, o)
	receipts := []Receipt{mustAppend(t, w, s), mustAppend(t, w, s)}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	for _, r := range receipts {
		if !errors.Is(r.Wait(canceled), context.Canceled) {
			t.Fatal("released before covering sync")
		}
	}
	if err := w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	for _, r := range receipts {
		if err := r.Wait(ctx); err != nil {
			t.Fatal(err)
		}
	}
	later := mustAppend(t, w, s)
	if !errors.Is(later.Wait(canceled), context.Canceled) {
		t.Fatal("old sync covered later append")
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(later.Wait(ctx), ErrClosed) {
		t.Fatal("unsynced receipt on close")
	}
	sim.Crash()
	entries := readAll(t, sim, s)
	if len(entries) != 3 {
		t.Fatalf("durable prefix length %d", len(entries))
	}
}

func TestConcurrentAppendFlushAndWait(t *testing.T) {
	ctx := context.Background()
	s := testStream(2)
	sim := simulator(t, vfs.Config{}, s)
	o := testOptions(s)
	o.GroupCommit = true
	o.MaximumBytes = 1 << 20
	w := mustWriter(t, sim, s, o)
	var wg sync.WaitGroup
	failures := make(chan error, 32)
	for range 16 {
		wg.Go(func() {
			r, err := w.Append(ctx, testRecord(s))
			if err == nil {
				err = w.Flush(ctx)
			}
			if err == nil {
				err = r.Wait(ctx)
			}
			if err != nil {
				failures <- err
			}
		})
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	sim.Crash()
	if len(readAll(t, sim, s)) != 17 {
		t.Fatal("concurrent stream lost a frame")
	}
}

func replaceFile(t *testing.T, b Backend, name string, data []byte) {
	t.Helper()
	ctx := context.Background()
	f, err := b.Open(ctx, name, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if err := f.Truncate(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(ctx, data, 0); err != nil {
		t.Fatal(err)
	}
	if err := f.Sync(ctx); err != nil {
		t.Fatal(err)
	}
}

func fileBytes(t *testing.T, b Backend, name string) []byte {
	t.Helper()
	ctx := context.Background()
	info, err := b.Stat(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	f, err := b.Open(ctx, name, os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	data := make([]byte, info.Size)
	if len(data) > 0 {
		if _, err := f.ReadAt(ctx, data, 0); err != nil {
			t.Fatal(err)
		}
	}
	return data
}

func TestEveryFinalFrameTruncationAndPreservedEvidence(t *testing.T) {
	for _, kind := range []uint8{1, 2} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			ctx := context.Background()
			s := testStream(kind)
			sim := simulator(t, vfs.Config{}, s)
			w := mustWriter(t, sim, s, testOptions(s))
			mustAppend(t, w, s)
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			r, err := Recover(ctx, sim, s, RecoveryOptions{})
			if err != nil {
				t.Fatal(err)
			}
			name := r.Segments[0].Name
			original := fileBytes(t, sim, name)
			boundary := int64(binary.BigEndian.Uint32(original[20:24]))
			for cut := boundary + 1; cut < int64(len(original)); cut++ {
				replaceFile(t, sim, name, original[:cut])
				_, err := Recover(ctx, sim, s, RecoveryOptions{})
				var torn *TornTail
				if !errors.As(err, &torn) || torn.Boundary != boundary {
					t.Fatalf("cut %d: %v", cut, err)
				}
				if !bytes.Equal(fileBytes(t, sim, name), original[:cut]) {
					t.Fatal("default recovery modified evidence")
				}
				preserved := false
				_, err = Recover(ctx, sim, s, RecoveryOptions{Repair: func(_ context.Context, tail Tail) error {
					preserved = bytes.Equal(fileBytes(t, sim, tail.Name), original[:cut])
					return nil
				}})
				if err != nil || !preserved {
					t.Fatalf("authorized cut %d: %v", cut, err)
				}
				if !bytes.Equal(fileBytes(t, sim, name), original[:boundary]) {
					t.Fatal("repair discarded verified prefix")
				}
			}
		})
	}
}

func TestCorruptionNeverRepairs(t *testing.T) {
	ctx := context.Background()
	s := testStream(1)
	sim := simulator(t, vfs.Config{}, s)
	w := mustWriter(t, sim, s, testOptions(s))
	mustAppend(t, w, s)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Recover(ctx, sim, s, RecoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	name := r.Segments[0].Name
	original := fileBytes(t, sim, name)
	for bit := range len(original) * 8 {
		bad := bytes.Clone(original)
		bad[bit/8] ^= 1 << uint(bit%8)
		replaceFile(t, sim, name, bad)
		_, err := Recover(ctx, sim, s, RecoveryOptions{})
		if err == nil {
			t.Fatalf("accepted bit flip %d", bit)
		}
		if !bytes.Equal(fileBytes(t, sim, name), bad) {
			t.Fatal("corrupt bytes modified")
		}
	}
}

func TestClosedTruncationsAndSealIntegrity(t *testing.T) {
	ctx := context.Background()
	s := testStream(1)
	sim := simulator(t, vfs.Config{}, s)
	w := mustWriter(t, sim, s, testOptions(s))
	mustAppend(t, w, s)
	if err := w.Seal(ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Recover(ctx, sim, s, RecoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	name := r.Segments[0].Name
	original := fileBytes(t, sim, name)
	for cut := range len(original) {
		replaceFile(t, sim, name, original[:cut])
		called := false
		_, err := Recover(ctx, sim, s, RecoveryOptions{Repair: func(context.Context, Tail) error { called = true; return nil }})
		if err == nil || called {
			t.Fatalf("closed cut %d repaired", cut)
		}
	}
	replaceFile(t, sim, name, original)
	// Valid CRC but invalid seal hash must be rejected.
	bad := bytes.Clone(original)
	seal := len(bad) - sealBytes
	bad[seal+144+48] ^= 1
	binary.BigEndian.PutUint32(bad[len(bad)-4:], crc32.Checksum(bad[seal:len(bad)-4], crc32.MakeTable(crc32.Castagnoli)))
	replaceFile(t, sim, name, bad)
	if _, err := Recover(ctx, sim, s, RecoveryOptions{}); err == nil {
		t.Fatal("accepted forged seal content hash")
	}
}

func TestRetainedSuffixAnchorAndReaderStart(t *testing.T) {
	ctx := context.Background()
	s := testStream(2)
	sim := simulator(t, vfs.Config{}, s)
	o := testOptions(s)
	w := mustWriter(t, sim, s, o)
	for range 8 {
		mustAppend(t, w, s)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Recover(ctx, sim, s, RecoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	anchor := r.Segments[0].Seal
	if anchor == nil {
		t.Fatal("missing sealed predecessor")
	}
	if err := sim.Unlink(ctx, r.Segments[0].Name); err != nil {
		t.Fatal(err)
	}
	if _, err := Recover(ctx, sim, s, RecoveryOptions{}); err == nil {
		t.Fatal("missing predecessor accepted")
	}
	opts := RecoveryOptions{PreviousSeal: anchor}
	r, err = Recover(ctx, sim, s, opts)
	if err != nil {
		t.Fatal(err)
	}
	start := r.Segments[0].Open.First + 1
	reader, err := OpenReader(ctx, sim, s, opts, s.position(start))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reader.Close() }()
	e, err := reader.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if e.Reference.Position.Sequence != start || !s.referenceMatches(e.Reference) {
		t.Fatal("start/stream reference mismatch")
	}
	bad := cloneReference(anchor)
	bad.SHA256[0] ^= 1
	if _, err := Recover(ctx, sim, s, RecoveryOptions{PreviousSeal: bad}); err == nil {
		t.Fatal("wrong predecessor hash accepted")
	}
}

func TestWriterAdmissionAndRejectedTailRepair(t *testing.T) {
	ctx := context.Background()
	s := testStream(1)
	sim := simulator(t, vfs.Config{}, s)
	o := testOptions(s)
	o.Recovery.Limits.Segments = 1
	w := mustWriter(t, sim, s, o)
	positioned := testRecord(s)
	positioned.Header.Sequence = 2
	if _, err := w.Append(ctx, positioned); err == nil {
		t.Fatal("historical record renumbered")
	}
	wrong := testRecord(testStream(2))
	wrong.Header = w.structuralHeader(0)
	if _, err := w.Append(ctx, wrong); err == nil {
		t.Fatal("reporter evidence in owner stream")
	}
	for range 3 {
		mustAppend(t, w, s)
	}
	if _, err := w.Append(ctx, testRecord(s)); !errors.Is(err, ErrLimit) {
		t.Fatalf("segment recovery count not reserved: %v", err)
	}
	if err := w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Recover(ctx, sim, s, o.Recovery)
	if err != nil {
		t.Fatal(err)
	}
	name := r.Segments[0].Name
	data := fileBytes(t, sim, name)
	replaceFile(t, sim, name, data[:len(data)-1])
	before := fileBytes(t, sim, name)
	denied := errors.New("protected prefix unresolved")
	_, err = Recover(ctx, sim, s, RecoveryOptions{Repair: func(context.Context, Tail) error { return denied }})
	if !errors.Is(err, denied) || !bytes.Equal(before, fileBytes(t, sim, name)) {
		t.Fatal("repair denial changed bytes")
	}
}
