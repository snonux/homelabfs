package segment

import (
	"errors"
	"testing"

	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/vfs"
)

func TestCheckpointRejectsArchivedSegmentIdentityReuse(t *testing.T) {
	ctx := t.Context()
	s := testStream(1)
	b := simulator(t, vfs.Config{}, s)
	o := testOptions(s)
	w := mustWriter(t, b, s, o)
	mustAppend(t, w, s)
	if err := w.Seal(ctx); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	entries := readAll(t, b, s)
	frames := make([][]byte, len(entries))
	for i, entry := range entries {
		frames[i] = entry.Bytes
	}
	r, err := Recover(ctx, b, s, RecoveryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	archived := r.Segments[0]
	if _, err := VerifyCheckpointSource(ctx, b, s, Limits{}, frames); err != nil {
		t.Fatalf("unchanged surviving archive rejected: %v", err)
	}
	if err := b.Unlink(ctx, archived.Name); err != nil {
		t.Fatal(err)
	}
	if err := b.SyncDir(ctx, s.Directory()); err != nil {
		t.Fatal(err)
	}
	// The generic suffix writer only knows its predecessor seal. A checkpoint
	// caller must additionally exclude archived IDs from its allocator; simulate
	// an invalid importer/allocator to exercise independent recovery validation.
	o.Recovery.PreviousSeal = archived.Seal
	o.NewSegment = func() record.UUID { return archived.Open.Segment }
	w = mustWriter(t, b, s, o)
	mustAppend(t, w, s)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := VerifyCheckpointSource(ctx, b, s, Limits{}, frames); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("archived segment identity accepted by verification: %v", err)
	}
	reader, err := OpenCheckpointSuffix(ctx, b, s, Limits{}, frames)
	if reader != nil {
		_ = reader.Close()
	}
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("archived segment identity accepted by suffix reader: %v", err)
	}
}
