package segment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"testing"

	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/vfs"
)

func TestSequenceExhaustedSuffixStaysReadableAndUnchanged(t *testing.T) {
	for _, kind := range []uint8{1, 2} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			ctx := context.Background()
			s := testStream(kind)
			sim := simulator(t, vfs.Config{}, s)
			anchor := record.FrameRef{StreamKind: kind, Position: s.position(math.MaxUint64 - 3)}
			if kind == 2 {
				reporter, inc := s.Reporter, s.ReporterIncarnation
				anchor.Reporter = &reporter
				anchor.ReporterIncarnation = &inc
			}
			opts := testOptions(s)
			opts.Recovery.PreviousSeal = &anchor
			open := record.SegmentOpen{Segment: id(11), StreamKind: kind, First: math.MaxUint64 - 2, PreviousSeal: &anchor, MaximumBytes: opts.MaximumBytes, Reporter: anchor.Reporter, ReporterIncarnation: anchor.ReporterIncarnation}
			r := testRecord(s)
			r.Header.Sequence = open.First
			r.Payload = open
			data, err := record.Encode(r)
			if err != nil {
				t.Fatal(err)
			}
			r = testRecord(s)
			r.Header.Sequence = math.MaxUint64 - 1
			frame, err := record.Encode(r)
			if err != nil {
				t.Fatal(err)
			}
			data = append(data, frame...)
			name := fmt.Sprintf("%s/%020d-open.hfj", s.Directory(), open.First)
			f, err := sim.Open(ctx, name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.WriteAt(ctx, data, 0); err != nil {
				t.Fatal(err)
			}
			if err := f.Sync(ctx); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
			if err := sim.SyncDir(ctx, s.Directory()); err != nil {
				t.Fatal(err)
			}
			recovery, state, err := recoverStream(ctx, sim, s, opts.Recovery)
			if err != nil {
				t.Fatal(err)
			}
			if recovery.Next != math.MaxUint64 {
				t.Fatal("unexpected next position")
			}
			if _, err := NewWriter(ctx, sim, s, opts); !errors.Is(err, ErrLimit) {
				t.Fatalf("exhausted writer admitted: %v", err)
			}
			if !bytes.Equal(data, fileBytes(t, sim, name)) {
				t.Fatal("writer admission changed retained bytes")
			}
			reader, err := OpenReader(ctx, sim, s, opts.Recovery, s.position(open.First))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = reader.Close() }()
			for _, sequence := range []uint64{open.First, math.MaxUint64 - 1} {
				entry, err := reader.Next(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if entry.Reference.Position.Sequence != sequence {
					t.Fatal("reader wrapped sequence")
				}
			}
			if _, err := reader.Next(ctx); !errors.Is(err, io.EOF) {
				t.Fatal(err)
			}
			// Also protect Seal itself, independently of constructor admission.
			f, err = sim.Open(ctx, name, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			opts.Recovery.Limits = defaultLimits(opts.Recovery.Limits)
			writer := &Writer{fs: sim, stream: s, opts: opts, file: f, current: recovery.Segments[0], state: state, next: recovery.Next, bytes: int64(len(data))}
			defer func() { _ = writer.Close() }()
			if err := writer.Seal(ctx); !errors.Is(err, ErrLimit) {
				t.Fatalf("sequence-exhausted seal admitted: %v", err)
			}
			if writer.next != math.MaxUint64 || !bytes.Equal(data, fileBytes(t, sim, name)) {
				t.Fatal("seal wrapped sequence or modified retained bytes")
			}
		})
	}
}
