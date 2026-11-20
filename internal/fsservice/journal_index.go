package fsservice

import (
	"context"
	"crypto/sha256"
	"hash"
	"sync"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
)

// journalIndex is the service's verified view of its own original journal for
// controlled source capture and verification (task te4). Before te4 every
// ViewSource, VerifyControlledSource and VerifyViewReference opened a fresh
// segment.Reader, which runs Recover over the whole stream and then decodes
// it again: two complete verifications per call, several calls per controlled
// operation and one per retained source, so a run's work grew with the square
// of its length (zq3: ~600-900 s CPU for TestZQ3RunWorkloadFitsLifetimeBounds).
//
// The index keeps a segment.Cursor, which on every refresh proves the already
// verified bytes unchanged (names, sizes and a raw SHA-256 pass) and decodes
// only appended frames through the same checks; any difference reruns the
// complete verification and rebuilds the index. So tampering with, truncating
// or reordering verified records is still detected on the very next call, as
// before. Per frame it keeps the reference, header incarnation, whether the
// frame bytes hash to the reference, and the cumulative SHA-256 of all frame
// bytes through that frame (the source "prefix" digest), which answers each
// caller's former full-stream loop by position lookup.
type journalIndex struct {
	mu     sync.Mutex
	cursor *segment.Cursor
	frames []indexedFrame
	prefix hash.Hash
	// goodHash counts the leading frames whose bytes hash to their reference.
	goodHash int
	// uniform counts the leading frames sharing frames[0]'s incarnation.
	uniform int
}

type indexedFrame struct {
	ref         record.FrameRef
	incarnation fsapi.UUID
	hashOK      bool
	prefix      [32]byte
}

// journalView is an immutable answer derived from one refresh; it is valid
// only while the caller still excludes journal appends.
type journalView struct {
	frames            []indexedFrame
	goodHash, uniform int
}

// refreshJournal verifies the journal (incrementally when possible) and
// returns the current view. Callers exclude appends exactly as they excluded
// them around the former full reread (sourceCut or the service worker).
func (s *Service) refreshJournal(ctx context.Context) (journalView, error) {
	x := &s.journalIndex
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.cursor == nil {
		x.cursor = segment.NewCursor(s.config.Source, s.config.Stream, s.config.Segment.Recovery)
	}
	err := x.cursor.Refresh(ctx, x.reset, x.add)
	if err != nil {
		// Never answer from a partially extended index.
		x.reset()
		x.cursor = nil
		return journalView{}, err
	}
	return journalView{frames: x.frames[:len(x.frames):len(x.frames)], goodHash: x.goodHash, uniform: x.uniform}, nil
}

func (x *journalIndex) reset() {
	x.frames, x.prefix, x.goodHash, x.uniform = nil, sha256.New(), 0, 0
}

// add records one newly verified frame, exactly the per-frame facts the
// former readers checked.
func (x *journalIndex) add(e segment.Entry) error {
	_, _ = x.prefix.Write(e.Bytes)
	f := indexedFrame{ref: e.Reference, incarnation: e.Record.Header.Incarnation, hashOK: record.Hash(sha256.Sum256(e.Bytes)) == e.Reference.SHA256, prefix: [32]byte(x.prefix.Sum(nil))}
	if f.hashOK && x.goodHash == len(x.frames) {
		x.goodHash++
	}
	if x.uniform == len(x.frames) && (len(x.frames) == 0 || f.incarnation == x.frames[0].incarnation) {
		x.uniform++
	}
	x.frames = append(x.frames, f)
	return nil
}

// find returns the index of the first frame at position p, or -1. The owner
// stream starts at sequence 1 (Config refuses a PreviousSeal), so the index
// is the sequence minus one; the full position is still compared.
func (v journalView) find(p fsapi.Position) int {
	i := p.Sequence - 1
	if p.Sequence == 0 || i >= uint64(len(v.frames)) || v.frames[i].ref.Position != p {
		return -1
	}
	return int(i)
}
