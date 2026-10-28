package segment

import (
	"bytes"
	"context"
	"errors"
	"io"
	"runtime"
	"sync"
	"testing"

	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/vfs"
)

func TestReaderEntriesOwnReporterMetadata(t *testing.T) {
	for _, concurrent := range []bool{false, true} {
		name := "sequential"
		if concurrent {
			name = "concurrent"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			s := testStream(2)
			sim := simulator(t, vfs.Config{}, s)
			w := mustWriter(t, sim, s, testOptions(s))
			for range 40 {
				mustAppend(t, w, s)
			}
			if err := w.Seal(ctx); err != nil {
				t.Fatal(err)
			}
			if err := w.Close(); err != nil {
				t.Fatal(err)
			}
			original := readAll(t, sim, s)
			r, err := OpenReader(ctx, sim, s, RecoveryOptions{}, s.position(1))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = r.Close() }()
			var wg sync.WaitGroup
			defer wg.Wait()
			opens, seals, predecessors := 0, 0, 0
			for i, want := range original {
				entry, err := r.Next(ctx)
				if err != nil {
					t.Fatalf("entry%d after caller mutation: %v", i, err)
				}
				if !bytes.Equal(entry.Bytes, want.Bytes) || !sameReference(&entry.Reference, &want.Reference) {
					t.Fatalf("entry%d bytes/reference changed", i)
				}
				switch p := entry.Record.Payload.(type) {
				case record.SegmentOpen:
					opens++
					if p.PreviousSeal != nil {
						predecessors++
					}
				case record.SegmentSeal:
					seals++
				}
				if concurrent {
					wg.Go(func() {
						for range 32 {
							mutateOwnedEntry(entry)
							runtime.Gosched()
						}
					})
				} else {
					mutateOwnedEntry(entry)
				}
			}
			if _, err := r.Next(ctx); !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
			if opens < 2 || seals != opens || predecessors == 0 {
				t.Fatal("test did not cover reporter segment boundaries")
			}
			// Returned byte slices are independent of both the incremental hash
			// chain and stored bytes; caller changes must affect neither.
			after := readAll(t, sim, s)
			if len(after) != len(original) {
				t.Fatal("stream length changed")
			}
			for i := range original {
				if !bytes.Equal(after[i].Bytes, original[i].Bytes) {
					t.Fatalf("stored frame%d changed", i)
				}
			}
		})
	}
}

func mutateOwnedEntry(entry Entry) {
	for i := range entry.Bytes {
		entry.Bytes[i] ^= 0xff
	}
	mutateReference(&entry.Reference)
	if open, ok := entry.Record.Payload.(record.SegmentOpen); ok {
		if open.Reporter != nil {
			open.Reporter[0] ^= 0xff
		}
		if open.ReporterIncarnation != nil {
			open.ReporterIncarnation[0] ^= 0xff
		}
		if open.PreviousSeal != nil {
			mutateReference(open.PreviousSeal)
		}
	}
}

func mutateReference(ref *record.FrameRef) {
	if ref.Reporter != nil {
		ref.Reporter[0] ^= 0xff
	}
	if ref.ReporterIncarnation != nil {
		ref.ReporterIncarnation[0] ^= 0xff
	}
	ref.Position.Sequence ^= 1
	ref.SHA256[0] ^= 0xff
}
