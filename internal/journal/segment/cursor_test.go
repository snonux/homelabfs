package segment

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/snonux/homelabfs/internal/vfs"
)

// cursorLog accumulates what a Cursor caller sees: reset forgets everything.
type cursorLog struct {
	entries []Entry
	resets  int
}

func (l *cursorLog) refresh(ctx context.Context, c *Cursor) error {
	return c.Refresh(ctx, func() { l.entries, l.resets = nil, l.resets+1 }, func(e Entry) error {
		l.entries = append(l.entries, e)
		return nil
	})
}

// requireSameAsReader proves the incremental view equals a fresh complete
// OpenReader pass, frame by frame.
func requireSameAsReader(t *testing.T, b Backend, s Stream, got []Entry) {
	t.Helper()
	want := readAll(t, b, s)
	if len(got) != len(want) {
		t.Fatalf("cursor saw %d frames, full reader %d", len(got), len(want))
	}
	for i := range want {
		if !bytes.Equal(got[i].Bytes, want[i].Bytes) || !sameReference(&got[i].Reference, &want[i].Reference) || got[i].Record.Header != want[i].Record.Header {
			t.Fatalf("frame %d differs from full verification", i)
		}
	}
}

// TestCursorIncrementalEqualsFullVerification appends across several segment
// rotations (OPEN, SEAL and the open->closed rename) and refreshes after each
// append: the cursor must verify everything once, then only extend, and its
// frames must always equal the complete reader's.
func TestCursorIncrementalEqualsFullVerification(t *testing.T) {
	for _, kind := range []uint8{1, 2} {
		t.Run(fmt.Sprint(kind), func(t *testing.T) {
			ctx := context.Background()
			s := testStream(kind)
			sim := simulator(t, vfs.Config{}, s)
			w := mustWriter(t, sim, s, testOptions(s))
			c, log := NewCursor(sim, s, RecoveryOptions{}), &cursorLog{}
			for i := range 30 {
				mustAppend(t, w, s)
				if i%11 == 10 {
					if err := w.Seal(ctx); err != nil {
						t.Fatal(err)
					}
				}
				if err := log.refresh(ctx, c); err != nil {
					t.Fatalf("append %d: %v", i, err)
				}
				requireSameAsReader(t, sim, s, log.entries)
			}
			if r, err := Recover(ctx, sim, s, RecoveryOptions{}); err != nil || len(r.Segments) < 3 {
				t.Fatalf("test did not cover rotations: %v", err)
			}
			if log.resets != 1 {
				t.Fatalf("pure appends restarted verification %d times", log.resets)
			}
		})
	}
}

// cursorFixture is a stream of several sealed segments plus an open one,
// already verified by a warm cursor.
func cursorFixture(t *testing.T) (*vfs.Simulator, Stream, *Cursor, *cursorLog, Recovery) {
	t.Helper()
	ctx := context.Background()
	s := testStream(1)
	sim := simulator(t, vfs.Config{}, s)
	w := mustWriter(t, sim, s, testOptions(s))
	for range 12 {
		mustAppend(t, w, s)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	c, log := NewCursor(sim, s, RecoveryOptions{}), &cursorLog{}
	if err := log.refresh(ctx, c); err != nil {
		t.Fatal(err)
	}
	r, err := Recover(ctx, sim, s, RecoveryOptions{})
	if err != nil || len(r.Segments) < 3 {
		t.Fatalf("fixture needs several segments: %v", err)
	}
	return sim, s, c, log, r
}

// TestCursorDetectsTamperingOfVerifiedBytes flips every byte of every
// already verified segment (sealed and open): each flip must fail the next
// Refresh, as it fails a fresh Recover, and restoring the bytes must recover
// through a complete reverification with identical frames.
func TestCursorDetectsTamperingOfVerifiedBytes(t *testing.T) {
	ctx := context.Background()
	sim, s, c, log, r := cursorFixture(t)
	for _, seg := range r.Segments {
		original := fileBytes(t, sim, seg.Name)
		for at := range original {
			bad := bytes.Clone(original)
			bad[at] ^= 0x10
			replaceFile(t, sim, seg.Name, bad)
			if err := log.refresh(ctx, c); err == nil {
				t.Fatalf("%s byte %d: tampered verified byte accepted", seg.Name, at)
			}
			replaceFile(t, sim, seg.Name, original)
			resets := log.resets
			if err := log.refresh(ctx, c); err != nil || log.resets != resets+1 {
				t.Fatalf("%s byte %d: restore not fully reverified: %v", seg.Name, at, err)
			}
		}
	}
	requireSameAsReader(t, sim, s, log.entries)
}

// TestCursorDetectsTruncationAndReordering covers verified bytes that vanish
// or move: a sealed segment truncated, two segments' contents swapped and an
// interior segment moved away; restoring everything must reverify fully.
func TestCursorDetectsTruncationAndReordering(t *testing.T) {
	ctx := context.Background()
	sim, s, c, log, r := cursorFixture(t)
	first, second := r.Segments[0].Name, r.Segments[1].Name
	a, b := fileBytes(t, sim, first), fileBytes(t, sim, second)
	replaceFile(t, sim, first, a[:len(a)-1])
	if err := log.refresh(ctx, c); err == nil {
		t.Fatal("truncated sealed segment accepted")
	}
	replaceFile(t, sim, first, b)
	replaceFile(t, sim, second, a)
	if err := log.refresh(ctx, c); err == nil {
		t.Fatal("reordered segment contents accepted")
	}
	replaceFile(t, sim, first, a)
	replaceFile(t, sim, second, b)
	// Renaming the second sealed segment away leaves a sequence gap.
	if err := sim.Rename(ctx, second, second+".moved"); err != nil {
		t.Fatal(err)
	}
	if err := log.refresh(ctx, c); err == nil {
		t.Fatal("missing interior segment accepted")
	}
	if err := sim.Rename(ctx, second+".moved", second); err != nil {
		t.Fatal(err)
	}
	resets := log.resets
	if err := log.refresh(ctx, c); err != nil || log.resets != resets+1 {
		t.Fatalf("restored stream not fully reverified: %v", err)
	}
	requireSameAsReader(t, sim, s, log.entries)
}

// TestCursorShorterOpenSegmentRestarts cuts the open segment back to its
// previous frame boundary: Recover accepts that stream, so the cursor must
// reset and report exactly the shorter stream, never its stale frames.
func TestCursorShorterOpenSegmentRestarts(t *testing.T) {
	ctx := context.Background()
	sim, s, c, log, r := cursorFixture(t)
	open := r.Segments[len(r.Segments)-1]
	data := fileBytes(t, sim, open.Name)
	last := log.entries[len(log.entries)-1]
	if last.Record.Header.Sequence != open.Last || len(data) <= len(last.Bytes) {
		t.Fatal("fixture open segment has no removable frame")
	}
	replaceFile(t, sim, open.Name, data[:len(data)-len(last.Bytes)])
	resets := log.resets
	if err := log.refresh(ctx, c); err != nil || log.resets != resets+1 {
		t.Fatalf("shorter stream not reverified: %v", err)
	}
	requireSameAsReader(t, sim, s, log.entries)
	if n := len(log.entries); log.entries[n-1].Record.Header.Sequence != open.Last-1 {
		t.Fatal("cursor kept a removed frame")
	}
}

// TestCursorRefusesBytesAfterVerifiedSeal appends a frame's worth of bytes
// to a segment the cursor already verified up to and including its SEAL.
// Recover refuses that ("bytes after SEAL"); Refresh must refuse too rather
// than skip the sealed segment, and must recover once the bytes are gone.
func TestCursorRefusesBytesAfterVerifiedSeal(t *testing.T) {
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
	c, log := NewCursor(sim, s, RecoveryOptions{}), &cursorLog{}
	if err := log.refresh(ctx, c); err != nil {
		t.Fatal(err)
	}
	r, err := Recover(ctx, sim, s, RecoveryOptions{})
	if err != nil || len(r.Segments) != 1 || r.Segments[0].Seal == nil {
		t.Fatalf("fixture needs one sealed segment as the last one: %v", err)
	}
	name := r.Segments[0].Name
	original := fileBytes(t, sim, name)
	grown := append(bytes.Clone(original), log.entries[1].Bytes...)
	replaceFile(t, sim, name, grown)
	if _, err := Recover(ctx, sim, s, RecoveryOptions{}); err == nil {
		t.Fatal("fixture: Recover accepted bytes after SEAL")
	}
	for range 2 {
		if err := log.refresh(ctx, c); err == nil {
			t.Fatal("cursor accepted bytes after a verified SEAL")
		}
	}
	replaceFile(t, sim, name, original)
	if err := log.refresh(ctx, c); err != nil {
		t.Fatal(err)
	}
	requireSameAsReader(t, sim, s, log.entries)
}

// TestCursorDetectsTamperingWhileOpenSegmentGrows changes an already
// verified byte of the open last segment and appends a new frame in the same
// interval: the growth must not hide the rewrite, so Refresh must fail like
// Recover, and after restoring the byte it must fully reverify and include
// the new frame.
func TestCursorDetectsTamperingWhileOpenSegmentGrows(t *testing.T) {
	ctx := context.Background()
	s := testStream(1)
	sim := simulator(t, vfs.Config{}, s)
	w := mustWriter(t, sim, s, testOptions(s))
	mustAppend(t, w, s)
	c, log := NewCursor(sim, s, RecoveryOptions{}), &cursorLog{}
	if err := log.refresh(ctx, c); err != nil {
		t.Fatal(err)
	}
	mustAppend(t, w, s)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Recover(ctx, sim, s, RecoveryOptions{})
	if err != nil || len(r.Segments) != 1 || r.Segments[0].Seal != nil {
		t.Fatalf("fixture needs one open segment: %v", err)
	}
	name := r.Segments[0].Name
	original := fileBytes(t, sim, name)
	verified := len(log.entries[0].Bytes) + len(log.entries[1].Bytes)
	if len(original) <= verified {
		t.Fatal("fixture open segment did not grow")
	}
	bad := bytes.Clone(original)
	bad[verified/2] ^= 0x10
	replaceFile(t, sim, name, bad)
	if _, err := Recover(ctx, sim, s, RecoveryOptions{}); err == nil {
		t.Fatal("fixture: Recover accepted the tampered prefix")
	}
	if err := log.refresh(ctx, c); err == nil {
		t.Fatal("growth hid a tampered verified byte of the open segment")
	}
	replaceFile(t, sim, name, original)
	resets := log.resets
	if err := log.refresh(ctx, c); err != nil || log.resets != resets+1 {
		t.Fatalf("restored stream not fully reverified: %v", err)
	}
	requireSameAsReader(t, sim, s, log.entries)
}
