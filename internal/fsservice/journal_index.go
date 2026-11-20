package fsservice

import (
	"context"
	"crypto/sha256"
	"errors"
	"hash"
	"io"
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
// of its length (zq3: ~470 s CPU for TestZQ3RunWorkloadFitsLifetimeBounds).
//
// The index keeps a segment.Cursor, which on every refresh proves the already
// verified bytes unchanged (names, sizes and a raw SHA-256 pass) and decodes
// only appended frames through the same checks; any difference reruns the
// complete verification and rebuilds the index. So tampering with, truncating
// or reordering verified records is still detected on the very next call, as
// before.
//
// Memory: per frame only the reference hash and the cumulative SHA-256 of all
// frame bytes through it (the source "prefix" digest) are kept, 64 bytes. The
// full reference is rebuilt from the owner stream and the frame's sequence;
// the rare frames whose reference, incarnation or hash check differ from
// that common case are kept in small exception maps. The owner stream is
// never retired (up to segment Limits.Bytes, 1 GiB by default), so the index
// is capped at journalIndexFrames (4 MiB). Above it the index is dropped for
// the rest of the service lifetime and every call streams the journal as
// before te4 (journalStream: O(1) memory, O(journal) time), which gives the
// same answers.
type journalIndex struct {
	mu       sync.Mutex
	cursor   *segment.Cursor
	overflow bool
	stream   segment.Stream
	frames   []indexedFrame
	running  hash.Hash
	first    fsapi.UUID
	foreign  map[int]fsapi.UUID
	badHash  map[int]struct{}
	odd      map[int]record.FrameRef
	// goodHash counts the leading frames whose bytes hash to their reference.
	goodHash int
	// uniform counts the leading frames carrying the first frame's incarnation.
	uniform int
}

type indexedFrame struct{ sha, prefix [32]byte }

// journalIndexFrames is the index ceiling (64 B per frame). A variable only
// so tests can exercise the streaming fallback.
var journalIndexFrames = 1 << 16

var errJournalIndexFull = errors.New("journal index frame ceiling")

// frameFacts is everything a caller needs about one journal frame: its
// reference and incarnation, whether its bytes hash to the reference, the
// prefix digest through it, and whether every frame through it hashes
// correctly and carries the first frame's incarnation (first).
type frameFacts struct {
	ref                          record.FrameRef
	incarnation, first           fsapi.UUID
	prefix                       [32]byte
	hashOK, clean, uniform, live bool
}

// journalFrame verifies the journal and returns the facts of the frame at
// *at, or of the last frame when at is nil; live is false when there is no
// such frame. Callers exclude appends exactly as they excluded them around
// the former full reread (sourceCut or the service worker).
func (s *Service) journalFrame(ctx context.Context, at *fsapi.Position) (frameFacts, error) {
	x := &s.journalIndex
	x.mu.Lock()
	defer x.mu.Unlock()
	if !x.overflow {
		err := x.refresh(ctx, s)
		if err == nil {
			return x.facts(at), nil
		}
		if !errors.Is(err, errJournalIndexFull) {
			return frameFacts{}, err
		}
		x.overflow = true
	}
	return s.journalStream(ctx, at)
}

// refresh extends the index through the cursor. On any error the index and
// cursor are discarded, so a partially extended index is never answered from.
func (x *journalIndex) refresh(ctx context.Context, s *Service) error {
	if x.cursor == nil {
		x.stream = s.config.Stream
		x.cursor = segment.NewCursor(s.config.Source, s.config.Stream, s.config.Segment.Recovery)
	}
	if err := x.cursor.Refresh(ctx, x.reset, x.add); err != nil {
		x.reset()
		x.cursor = nil
		return err
	}
	return nil
}

func (x *journalIndex) reset() {
	x.frames, x.running, x.first = nil, sha256.New(), fsapi.UUID{}
	x.foreign, x.badHash, x.odd = map[int]fsapi.UUID{}, map[int]struct{}{}, map[int]record.FrameRef{}
	x.goodHash, x.uniform = 0, 0
}

// add records one newly verified frame: exactly the per-frame facts the
// former readers checked, stored compactly.
func (x *journalIndex) add(e segment.Entry) error {
	i := len(x.frames)
	if i >= journalIndexFrames {
		return errJournalIndexFull
	}
	_, _ = x.running.Write(e.Bytes)
	if record.Hash(sha256.Sum256(e.Bytes)) != e.Reference.SHA256 {
		x.badHash[i] = struct{}{}
	} else if x.goodHash == i {
		x.goodHash++
	}
	incarnation := e.Record.Header.Incarnation
	if i == 0 {
		x.first = incarnation
	}
	if incarnation != x.first {
		x.foreign[i] = incarnation
	} else if x.uniform == i {
		x.uniform++
	}
	// != compares the Reporter/ReporterIncarnation pointers by address. That
	// is safe only because Config (service.go) forces stream kind 1, whose
	// references carry nil pointers; an evidence stream would land every
	// frame in odd. The exception maps are bounded by the frame ceiling
	// (<= journalIndexFrames = 65536 entries) and empty in the normal case.
	if e.Reference != x.reference(i, e.Reference.SHA256) {
		x.odd[i] = e.Reference
	}
	x.frames = append(x.frames, indexedFrame{sha: e.Reference.SHA256, prefix: [32]byte(x.running.Sum(nil))})
	return nil
}

// reference rebuilds frame i's owner-stream reference (sequence i+1).
func (x *journalIndex) reference(i int, sha [32]byte) record.FrameRef {
	p := record.Position{Volume: x.stream.Volume, Generation: x.stream.Generation, Configuration: x.stream.Configuration, Epoch: x.stream.Epoch, Sequence: uint64(i) + 1}
	return record.FrameRef{StreamKind: x.stream.Kind, Position: p, SHA256: sha}
}

// facts answers journalFrame from the index. The owner stream starts at
// sequence 1 (Config refuses a PreviousSeal), so frame i has sequence i+1;
// the full position is still compared.
func (x *journalIndex) facts(at *fsapi.Position) frameFacts {
	i := len(x.frames) - 1
	if at != nil {
		if at.Sequence == 0 || at.Sequence > uint64(len(x.frames)) {
			return frameFacts{}
		}
		i = int(at.Sequence - 1)
	}
	if i < 0 {
		return frameFacts{}
	}
	f := frameFacts{ref: x.reference(i, x.frames[i].sha), incarnation: x.first, first: x.first, prefix: x.frames[i].prefix, live: true}
	if ref, ok := x.odd[i]; ok {
		f.ref = ref
	}
	if inc, ok := x.foreign[i]; ok {
		f.incarnation = inc
	}
	_, bad := x.badHash[i]
	f.hashOK, f.clean, f.uniform = !bad, i < x.goodHash, i < x.uniform
	if at != nil && f.ref.Position != *at {
		return frameFacts{}
	}
	return f
}

// journalStream is the pre-te4 path above the index ceiling: a fresh
// complete Reader pass that stops at *at (or reads to the end for the
// tail), computing the same facts with O(1) memory.
func (s *Service) journalStream(ctx context.Context, at *fsapi.Position) (frameFacts, error) {
	reader, err := s.reader(ctx)
	if err != nil {
		return frameFacts{}, err
	}
	defer func() { _ = reader.Close() }()
	prefix := sha256.New()
	var last frameFacts
	for n := 0; ; n++ {
		e, err := reader.Next(ctx)
		if errors.Is(err, io.EOF) {
			if at != nil {
				return frameFacts{}, nil
			}
			return last, nil
		}
		if err != nil {
			return frameFacts{}, err
		}
		_, _ = prefix.Write(e.Bytes)
		f := frameFacts{ref: e.Reference, incarnation: e.Record.Header.Incarnation, first: last.first, prefix: [32]byte(prefix.Sum(nil)), live: true}
		if n == 0 {
			f.first = f.incarnation
		}
		f.hashOK = record.Hash(sha256.Sum256(e.Bytes)) == e.Reference.SHA256
		f.clean = f.hashOK && (n == 0 || last.clean)
		f.uniform = f.incarnation == f.first && (n == 0 || last.uniform)
		if at != nil && f.ref.Position == *at {
			return f, nil
		}
		last = f
	}
}
