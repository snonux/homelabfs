package segment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"testing"

	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/vfs"
)

type countedBackend struct {
	Backend
	calls, bytes int
	readHook     func(context.Context, vfs.File, []byte, int64) (int, error)
}

type countedFile struct {
	vfs.File
	backend *countedBackend
}

func (b *countedBackend) Open(ctx context.Context, name string, flags int, mode fs.FileMode) (vfs.File, error) {
	f, err := b.Backend.Open(ctx, name, flags, mode)
	if err != nil {
		return nil, err
	}
	return &countedFile{File: f, backend: b}, nil
}

func (f *countedFile) ReadAt(ctx context.Context, data []byte, offset int64) (int, error) {
	b := f.backend
	b.calls++
	var n int
	var err error
	if b.readHook != nil {
		n, err = b.readHook(ctx, f.File, data, offset)
	} else {
		n, err = f.File.ReadAt(ctx, data, offset)
	}
	b.bytes += n
	return n, err
}

func windowFixture(t *testing.T, count int) (*countedBackend, Stream, string, []byte) {
	t.Helper()
	ctx := context.Background()
	s := testStream(1)
	osfs, err := vfs.NewOS(ctx, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = osfs.Close() })
	mkdirStream(t, osfs, s)
	o := testOptions(s)
	o.MaximumBytes = 1 << 20
	w := mustWriter(t, osfs, s, o)
	for range count {
		mustAppend(t, w, s)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Recover(ctx, osfs, s, RecoveryOptions{})
	if err != nil || len(r.Segments) != 1 {
		t.Fatalf("fixture recovery: %v, segments %d", err, len(r.Segments))
	}
	name := r.Segments[0].Name
	return &countedBackend{Backend: osfs}, s, name, fileBytes(t, osfs, name)
}

func TestScanWindowRealVFSReadAmplification(t *testing.T) {
	for _, count := range []int{64, 256, 1024} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			b, s, _, stored := windowFixture(t, count)
			for scan := range 2 {
				b.calls, b.bytes = 0, 0
				entries := readAll(t, b, s)
				var got []byte
				for _, e := range entries {
					got = append(got, e.Bytes...)
				}
				if len(entries) != count+1 || !bytes.Equal(got, stored) {
					t.Fatal("full prefix/owned exact bytes changed")
				}
				wantCalls := 2 * ((len(stored) + scanWindowBytes - 1) / scanWindowBytes)
				t.Logf("scan=%d frames=%d stored=%d read calls=%d bytes=%d; unbuffered calls=%d", scan, len(entries), len(stored), b.calls, b.bytes, 4*len(entries))
				// OpenReader recovery and Next independently read the full prefix.
				if b.calls != wantCalls || b.bytes != 2*len(stored) {
					t.Fatalf("want %d reads and %d bytes, got %d and %d", wantCalls, 2*len(stored), b.calls, b.bytes)
				}
			}
		})
	}
}

func TestScanWindowCorruptionAndFreshReader(t *testing.T) {
	b, s, name, original := windowFixture(t, 1024)
	_ = readAll(t, b, s) // Populate and release the previous reader's windows.
	for _, offset := range []int{500, scanWindowBytes - 1, scanWindowBytes, scanWindowBytes + 1, 2*scanWindowBytes - 1, 2 * scanWindowBytes} {
		t.Run(fmt.Sprint(offset), func(t *testing.T) {
			bad := bytes.Clone(original)
			bad[offset] ^= 1
			replaceFile(t, b, name, bad)
			defer replaceFile(t, b, name, original)
			repaired := false
			_, err := OpenReader(context.Background(), b, s, RecoveryOptions{Repair: func(context.Context, Tail) error {
				repaired = true
				return nil
			}}, s.position(1))
			if err == nil || repaired {
				t.Fatalf("new reader accepted/repaired changed prefix: %v, repair=%v", err, repaired)
			}
			if !bytes.Equal(fileBytes(t, b, name), bad) {
				t.Fatal("corruption evidence changed")
			}
		})
	}
}

func TestScanWindowReadFaultsNeverRepair(t *testing.T) {
	fault := errors.New("injected read failure")
	for _, mode := range []string{"partial-error", "full-error", "short-eof", "short-nil", "second-window"} {
		t.Run(mode, func(t *testing.T) {
			b, s, name, original := windowFixture(t, 1024)
			want := fault
			if mode == "short-eof" {
				want = io.EOF
			}
			if mode == "short-nil" {
				want = io.ErrUnexpectedEOF
			}
			b.readHook = func(ctx context.Context, f vfs.File, data []byte, offset int64) (int, error) {
				if mode == "second-window" && offset == 0 {
					return f.ReadAt(ctx, data, offset)
				}
				length := len(data) / 2
				if mode == "full-error" {
					length = len(data)
				}
				n, err := f.ReadAt(ctx, data[:length], offset)
				if err != nil {
					return n, err
				}
				if mode == "short-nil" {
					return n, nil
				}
				return n, want
			}
			repaired := false
			_, err := Recover(context.Background(), b, s, RecoveryOptions{Repair: func(context.Context, Tail) error {
				repaired = true
				return nil
			}})
			if !errors.Is(err, want) || repaired {
				t.Fatalf("read error hidden/repaired: %v, repair=%v", err, repaired)
			}
			b.readHook = nil
			if !bytes.Equal(fileBytes(t, b, name), original) {
				t.Fatal("read fault changed evidence")
			}
		})
	}
}

func TestScanWindowCancellationOnCachedReads(t *testing.T) {
	b, s, _, _ := windowFixture(t, 256)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	b.readHook = func(ctx context.Context, f vfs.File, data []byte, offset int64) (int, error) {
		n, err := f.ReadAt(ctx, data, offset)
		cancel() // Header succeeded; the body would be a cache hit.
		return n, err
	}
	if _, err := Recover(ctx, b, s, RecoveryOptions{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("recovery cached body ignored cancellation: %v", err)
	}
	b.readHook = nil
	r, err := OpenReader(context.Background(), b, s, RecoveryOptions{}, s.position(1))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	if _, err := r.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	calls := b.calls
	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	if _, err := r.Next(ctx); !errors.Is(err, context.Canceled) || b.calls != calls {
		t.Fatalf("reader cache hit ignored cancellation: %v", err)
	}
}

func TestScanWindowLargeFrameAndSeal(t *testing.T) {
	b, s, _, _ := windowFixture(t, 1)
	o := testOptions(s)
	o.MaximumBytes = 1 << 20
	w := mustWriter(t, b, s, o)
	data := bytes.Repeat([]byte{0, 0xff, 0x23}, scanWindowBytes)
	object := record.ObjectID{Home: s.Volume, Allocation: id(8)}
	selected := []record.ObjectEdit{{Object: object, ExpectedRevision: 1, After: record.Metadata{Revision: 2, Type: 1, Mode: 0o600, Size: uint64(len(data)), Nlink: 1}}}
	raw := testRecord(s)
	raw.Payload = record.Mutation{
		Intent: record.Intent{Kind: 8, Arguments: record.Write{Object: object, Data: data}, SelectedMetadata: selected},
		Redo:   record.Redo{Objects: selected},
	}
	if _, err := w.Append(context.Background(), raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Seal(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Recover(context.Background(), b, s, RecoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	stored := fileBytes(t, b, r.Segments[0].Name)
	b.calls, b.bytes = 0, 0
	entries := readAll(t, b, s)
	var got []byte
	for _, e := range entries {
		got = append(got, e.Bytes...)
	}
	if len(entries) != 4 || !bytes.Equal(got, stored) {
		t.Fatal("large frame or seal changed")
	}
	mutation, ok := entries[2].Record.Payload.(record.Mutation)
	if !ok || !bytes.Equal(mutation.Intent.Arguments.(record.Write).Data, data) {
		t.Fatal("large payload changed")
	}
	if b.bytes != 2*len(stored) || b.calls != 2*((len(stored)+scanWindowBytes-1)/scanWindowBytes) {
		t.Fatalf("large frame read amplification: calls=%d bytes=%d", b.calls, b.bytes)
	}
}

func TestScanWindowReaderFaultStopsIteration(t *testing.T) {
	b, s, _, _ := windowFixture(t, 1024)
	r, err := OpenReader(context.Background(), b, s, RecoveryOptions{}, s.position(1))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = r.Close() }()
	fault := errors.New("reader second-window fault")
	b.readHook = func(ctx context.Context, f vfs.File, data []byte, offset int64) (int, error) {
		if offset == 0 {
			return f.ReadAt(ctx, data, offset)
		}
		return 0, fault
	}
	seen := 0
	for {
		_, err := r.Next(context.Background())
		if err != nil {
			if !errors.Is(err, fault) || seen == 0 || seen >= 1025 {
				t.Fatalf("reader fault lost: seen=%d err=%v", seen, err)
			}
			break
		}
		seen++
	}
	calls := b.calls
	b.readHook = nil
	if _, err := r.Next(context.Background()); !errors.Is(err, fault) || calls != b.calls {
		t.Fatalf("failed reader resumed: %v", err)
	}
}
