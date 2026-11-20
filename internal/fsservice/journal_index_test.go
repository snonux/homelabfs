package fsservice

import (
	"context"
	"fmt"
	"os"
	"testing"
	"unsafe"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/segment"
)

// captureSource takes one controlled source capture on the worker.
func (f *fixture) captureSource() ControlledViewSource {
	f.t.Helper()
	var source ControlledViewSource
	if err := f.service.Controlled(f.t.Context(), *f.c.Validation, func(ctx context.Context, cut *ControlledCut) error {
		var err error
		source, err = cut.ViewSource(ctx, 1<<20)
		return err
	}); err != nil {
		f.t.Fatal(err)
	}
	return source
}

// verifyReference runs VerifyViewReference on the worker.
func (f *fixture) verifyReference(source ControlledViewSource) error {
	return f.service.Controlled(f.t.Context(), *f.c.Validation, func(ctx context.Context, cut *ControlledCut) error {
		return cut.VerifyViewReference(ctx, source.Through)
	})
}

// journalFixture is a started service that admits creates (as in
// TestControlledSourceProofRetainsActualHistoricalBytes).
func journalFixture(t *testing.T) *fixture {
	t.Helper()
	f := setup(t, "os")
	policy, err := NewUnixPermissions(uid(60))
	if err != nil {
		t.Fatal(err)
	}
	f.c.Permissions = policy
	f.start()
	return f
}

// requireFactsEqualStream compares the incremental index's answer for every
// frame and for the tail with a fresh complete Reader pass (journalStream,
// the pre-te4 path that is also the fallback above the index ceiling).
func requireFactsEqualStream(t *testing.T, s *Service) frameFacts {
	t.Helper()
	tail, err := s.journalFrame(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	streamed, err := s.journalStream(t.Context(), nil)
	if err != nil || tail != streamed || !tail.live {
		t.Fatalf("tail: indexed %+v, streamed %+v (%v)", tail, streamed, err)
	}
	for seq := uint64(0); seq <= tail.ref.Position.Sequence+1; seq++ {
		p := tail.ref.Position
		p.Sequence = seq
		indexed, err := s.journalFrame(t.Context(), &p)
		if err != nil {
			t.Fatal(err)
		}
		if streamed, err = s.journalStream(t.Context(), &p); err != nil || indexed != streamed {
			t.Fatalf("sequence %d: indexed %+v, streamed %+v (%v)", seq, indexed, streamed, err)
		}
	}
	return tail
}

// TestJournalIndexIncrementalEqualsFull grows the journal across segment
// rotations (MaximumBytes 5000) with a capture after each step: the
// incrementally extended index must answer every frame exactly as a complete
// fresh Reader pass, and each capture's tail and prefix digest must be the
// last frame's. It also pins the 64-byte per-frame cost the ceiling assumes.
func TestJournalIndexIncrementalEqualsFull(t *testing.T) {
	if size := unsafe.Sizeof(indexedFrame{}); size != 64 {
		t.Fatalf("indexed frame costs %d bytes, the ceiling assumes 64", size)
	}
	f := journalFixture(t)
	for i := range 12 {
		f.create(f.c.Storage.Root, fmt.Sprint("file", i))
		source := f.captureSource()
		tail := requireFactsEqualStream(t, f.service)
		if source.Through != tail.ref || source.seal.prefix != tail.prefix {
			t.Fatalf("step %d: capture is not the verified tail", i)
		}
	}
	if r, err := segment.Recover(t.Context(), f.c.Source, f.c.Stream, segment.RecoveryOptions{}); err != nil || len(r.Segments) < 2 {
		t.Fatalf("test did not cover a segment rotation: %v", err)
	}
	if f.service.journalIndex.overflow || len(f.service.journalIndex.frames) == 0 {
		t.Fatal("index unexpectedly bypassed")
	}
}

// TestJournalIndexCeilingFallsBackToStreaming lowers the frame ceiling: once
// the journal exceeds it the index must hold no frames, and captures, source
// proofs, view references and tamper detection must keep working through the
// streaming path.
func TestJournalIndexCeilingFallsBackToStreaming(t *testing.T) {
	saved := journalIndexFrames
	journalIndexFrames = 4
	t.Cleanup(func() { journalIndexFrames = saved })
	f := journalFixture(t)
	for i := range 6 {
		f.create(f.c.Storage.Root, fmt.Sprint("file", i))
	}
	source := f.captureSource()
	x := &f.service.journalIndex
	if !x.overflow || x.frames != nil || x.cursor != nil {
		t.Fatalf("index above its ceiling kept state (overflow=%t, %d frames)", x.overflow, len(x.frames))
	}
	if err := f.service.VerifyControlledSource(t.Context(), *f.c.Validation, source); err != nil {
		t.Fatal(err)
	}
	if err := f.verifyReference(source); err != nil {
		t.Fatal(err)
	}
	r, err := segment.Recover(t.Context(), f.c.Source, f.c.Stream, segment.RecoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	flipJournalByte(t, f, r.Segments[0].Name, r.Segments[0].Size/2)
	if err = f.service.VerifyControlledSource(t.Context(), *f.c.Validation, source); err == nil {
		t.Fatal("streaming fallback accepted a tampered record")
	}
}

// flipJournalByte toggles one byte of a stored segment file in place.
func flipJournalByte(t *testing.T, f *fixture, name string, at int64) {
	t.Helper()
	file, err := f.c.Source.Open(t.Context(), name, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()
	var b [1]byte
	if _, err = file.ReadAt(t.Context(), b[:], at); err != nil {
		t.Fatal(err)
	}
	b[0] ^= 1
	if _, err = file.WriteAt(t.Context(), b[:], at); err != nil {
		t.Fatal(err)
	}
}

// TestJournalIndexDetectsTamperingOfVerifiedRecords damages an interior,
// already verified and indexed record of a sealed segment. Every entry point
// (source capture, source proof, view reference) must refuse on its very next
// call, as the former full reread did; repairing the byte must restore them
// through a complete reverification.
func TestJournalIndexDetectsTamperingOfVerifiedRecords(t *testing.T) {
	f := journalFixture(t)
	for i := range 8 {
		f.create(f.c.Storage.Root, fmt.Sprint("file", i))
	}
	source := f.captureSource()
	if err := f.service.VerifyControlledSource(t.Context(), *f.c.Validation, source); err != nil {
		t.Fatal(err)
	}
	r, err := segment.Recover(t.Context(), f.c.Source, f.c.Stream, segment.RecoveryOptions{})
	if err != nil || len(r.Segments) < 2 || r.Segments[0].Seal == nil {
		t.Fatalf("fixture needs a sealed segment: %v", err)
	}
	name, at := r.Segments[0].Name, r.Segments[0].Size/2
	flipJournalByte(t, f, name, at)
	if err = f.service.VerifyControlledSource(t.Context(), *f.c.Validation, source); err == nil {
		t.Fatal("source proof accepted a tampered verified record")
	}
	if err = f.verifyReference(source); err == nil {
		t.Fatal("view reference accepted a tampered verified record")
	}
	if err = f.service.Controlled(t.Context(), *f.c.Validation, func(ctx context.Context, cut *ControlledCut) error {
		_, err := cut.ViewSource(ctx, 1<<20)
		return err
	}); err == nil {
		t.Fatal("source capture accepted a tampered verified record")
	}
	flipJournalByte(t, f, name, at)
	if err = f.service.VerifyControlledSource(t.Context(), *f.c.Validation, source); err != nil {
		t.Fatalf("repaired journal not reverified: %v", err)
	}
	if err = f.verifyReference(source); err != nil {
		t.Fatal(err)
	}
	if f.service.State() != Serving {
		t.Fatal(fsapi.Unavailable)
	}
}
