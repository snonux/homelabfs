package fsservice

import (
	"context"
	"fmt"
	"os"
	"testing"

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

// fullJournalView verifies the journal from scratch with a fresh cursor.
func fullJournalView(t *testing.T, s *Service) journalView {
	t.Helper()
	var x journalIndex
	x.cursor = segment.NewCursor(s.config.Source, s.config.Stream, s.config.Segment.Recovery)
	if err := x.cursor.Refresh(t.Context(), x.reset, x.add); err != nil {
		t.Fatal(err)
	}
	return journalView{frames: x.frames, goodHash: x.goodHash, uniform: x.uniform}
}

// TestJournalIndexIncrementalEqualsFull grows the journal across segment
// rotations (MaximumBytes 5000) with a capture after each step: the
// incrementally extended index must equal one built by a complete fresh
// verification, and each capture's tail and prefix digest must be the last
// indexed frame's.
func TestJournalIndexIncrementalEqualsFull(t *testing.T) {
	f := journalFixture(t)
	for i := range 12 {
		f.create(f.c.Storage.Root, fmt.Sprint("file", i))
		source := f.captureSource()
		full := fullJournalView(t, f.service)
		f.service.journalIndex.mu.Lock()
		incremental := journalView{frames: f.service.journalIndex.frames, goodHash: f.service.journalIndex.goodHash, uniform: f.service.journalIndex.uniform}
		f.service.journalIndex.mu.Unlock()
		if len(incremental.frames) != len(full.frames) || incremental.goodHash != full.goodHash || incremental.uniform != full.uniform {
			t.Fatalf("step %d: incremental index differs from full verification", i)
		}
		for j := range full.frames {
			if incremental.frames[j] != full.frames[j] {
				t.Fatalf("step %d frame %d differs", i, j)
			}
		}
		last := full.frames[len(full.frames)-1]
		if source.Through != last.ref || source.seal.prefix != last.prefix {
			t.Fatalf("step %d: capture is not the verified tail", i)
		}
	}
	if r, err := segment.Recover(t.Context(), f.c.Source, f.c.Stream, segment.RecoveryOptions{}); err != nil || len(r.Segments) < 2 {
		t.Fatalf("test did not cover a segment rotation: %v", err)
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
