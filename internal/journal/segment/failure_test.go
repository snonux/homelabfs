package segment

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"testing"

	"github.com/snonux/homelabfs/internal/vfs"
)

type shortBackend struct {
	Backend
	armed bool
}
type shortFile struct {
	vfs.File
	backend *shortBackend
}

func (b *shortBackend) Open(ctx context.Context, name string, flags int, mode fs.FileMode) (vfs.File, error) {
	f, err := b.Backend.Open(ctx, name, flags, mode)
	if err != nil {
		return nil, err
	}
	return &shortFile{File: f, backend: b}, nil
}

func (f *shortFile) WriteAt(ctx context.Context, data []byte, offset int64) (int, error) {
	if f.backend.armed {
		f.backend.armed = false
		return f.File.WriteAt(ctx, data[:len(data)/2], offset)
	}
	return f.File.WriteAt(ctx, data, offset)
}

func TestShortWritePoisonsAndPreservesTail(t *testing.T) {
	ctx := context.Background()
	s := testStream(1)
	sim := simulator(t, vfs.Config{DataLoss: vfs.Keep}, s)
	b := &shortBackend{Backend: sim}
	w := mustWriter(t, b, s, testOptions(s))
	b.armed = true
	if _, err := w.Append(ctx, testRecord(s)); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal(err)
	}
	if _, err := w.Append(ctx, testRecord(s)); !errors.Is(err, io.ErrShortWrite) {
		t.Fatal("failed writer admitted a new record", err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	sim.Crash()
	_, err := Recover(ctx, sim, s, RecoveryOptions{})
	var tail *TornTail
	if !errors.As(err, &tail) {
		t.Fatalf("short-write evidence not preserved: %v", err)
	}
}

func TestGroupCommitFailureAndRotationCoverage(t *testing.T) {
	for _, point := range []string{"before-sync", "after-sync", "rotation"} {
		t.Run(point, func(t *testing.T) {
			ctx := context.Background()
			s := testStream(1)
			sim := simulator(t, vfs.Config{}, s)
			o := testOptions(s)
			o.GroupCommit = true
			w := mustWriter(t, sim, s, o)
			receipts := []Receipt{mustAppend(t, w, s), mustAppend(t, w, s), mustAppend(t, w, s)}
			if point == "rotation" {
				last := mustAppend(t, w, s)
				for _, r := range receipts {
					if err := r.Wait(ctx); err != nil {
						t.Fatal(err)
					}
				}
				canceled, cancel := context.WithCancel(ctx)
				cancel()
				if !errors.Is(last.Wait(canceled), context.Canceled) {
					t.Fatal("new segment append covered by old seal")
				}
				if err := w.Flush(ctx); err != nil {
					t.Fatal(err)
				}
				if err := last.Wait(ctx); err != nil {
					t.Fatal(err)
				}
				return
			}
			fault := errors.New("sync fault")
			w.opts.Failpoint = func(stage string) error {
				if stage == point {
					return fault
				}
				return nil
			}
			if err := w.Flush(ctx); !errors.Is(err, fault) {
				t.Fatal(err)
			}
			for _, r := range receipts {
				if !errors.Is(r.Wait(ctx), fault) {
					t.Fatal("waiter falsely succeeded")
				}
			}
			if _, err := w.Append(ctx, testRecord(s)); !errors.Is(err, fault) {
				t.Fatal("poisoned writer admitted append")
			}
		})
	}
}
