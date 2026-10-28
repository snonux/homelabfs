// Package segment persists and verifies local journal streams. It does not
// establish distributed authority, commitment, replay eligibility or retention.
package segment

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/vfs"
)

const sealBytes = 228

var (
	// ErrCorrupt marks a malformed stream, including a closed/interior tear.
	ErrCorrupt = errors.New("journal stream corruption")
	// ErrLimit marks an admission or scan budget limit.
	ErrLimit = errors.New("journal segment limit")
	// ErrClosed marks a closed writer.
	ErrClosed = errors.New("journal writer closed")
)

// Backend supplies persistence and bounded directory discovery.
type Backend interface {
	vfs.FS
	Stat(context.Context, string) (vfs.Info, error)
	OpenDir(context.Context, string) (vfs.Dir, error)
}

// Stream is a complete, caller-authorized stream key. Owner streams have zero
// Reporter and ReporterIncarnation; evidence streams require both.
type Stream struct {
	Volume, Generation, Configuration record.UUID
	Epoch                             uint64
	Kind                              uint8
	Reporter, ReporterIncarnation     record.UUID
}

// Limits bounds the complete scan, not just directory pages. Zero fields use
// 1024 segments and 1 GiB. No segment may exceed the format's 256 MiB limit.
type Limits struct {
	Segments int
	Bytes    int64
}

// Tail describes only a provably incomplete final frame after a verified OPEN.
// Approval must preserve the original bytes outside this stream directory and
// establish authority to discard the suffix. It is never an absence proof.
type Tail struct {
	Name           string
	Boundary, Size int64
}

// TornTail preserves a repairable tail unless an explicit Repair callback admits
// its truncation. Complete corrupt frames never produce this error.
type TornTail struct{ Tail }

// RecoveryOptions controls bounded scans and explicitly authorized repair.
type RecoveryOptions struct {
	Limits Limits
	// PreviousSeal anchors a retained suffix. Nil requires the stream to start at 1.
	PreviousSeal *record.FrameRef
	Repair       func(context.Context, Tail) error
	Failpoint    func(string) error
}

// Options controls a newly authored local stream. Structural supplies stable
// operation/incarnation/time metadata for each newly generated OPEN or SEAL.
// NewSegment supplies a fresh nonzero segment identity. Neither callback grants
// distributed authority. The caller owns all returned metadata.
type Options struct {
	MaximumBytes uint64
	Structural   func(uint64) record.Header
	NewSegment   func() record.UUID
	GroupCommit  bool
	Failpoint    func(string) error
	Recovery     RecoveryOptions
}

// Receipt identifies immutable stored bytes. Wait reports local durability only;
// in group mode a subsequent Flush (or rotation/seal) must cover it.
type Receipt struct {
	Reference record.FrameRef
	state     *completion
}

type completion struct {
	done chan struct{}
	err  error
}

// Summary describes a fully verified segment; it is not commitment evidence.
type Summary struct {
	Name string
	Open record.SegmentOpen
	Last uint64
	Size int64
	Seal *record.FrameRef
}

// Recovery is a validated contiguous stream, with at most one unsealed final
// segment. Values contain no complete frame buffers.
type Recovery struct {
	Stream   Stream
	Segments []Summary
	Next     uint64
}

// Writer serializes local appends and barriers. Any I/O/failpoint error poisons
// it until close and a fresh recovery; uncertain operations are never retried.
type Writer struct {
	mu       sync.Mutex
	fs       Backend
	stream   Stream
	opts     Options
	file     vfs.File
	current  Summary
	next     uint64
	previous *record.FrameRef
	segments map[record.UUID]struct{}
	bytes    int64
	state    scanState
	pending  []*completion
	err      error
	closed   bool
}

// Directory returns the canonical journal-format path for this stream.
func (s Stream) Directory() string {
	base := fmt.Sprintf("segments/%s/%s/%s/e%020d/", s.Volume, s.Generation, s.Configuration, s.Epoch)
	if s.Kind == 1 {
		return base + "owner"
	}
	return fmt.Sprintf("%sevidence/%s/%s", base, s.Reporter, s.ReporterIncarnation)
}

// Wait waits for a barrier covering this receipt. Cancellation does not cancel
// the append or establish that its bytes were lost.
func (r Receipt) Wait(ctx context.Context) error {
	if r.state == nil {
		return fmt.Errorf("invalid receipt")
	}
	select {
	case <-r.state.done:
		return r.state.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Error describes the preserved incomplete suffix.
func (t *TornTail) Error() string {
	return fmt.Sprintf("incomplete journal tail %s at %d of %d", t.Name, t.Boundary, t.Size)
}

func (s Stream) valid() bool {
	if !s.Volume.Valid() || !s.Generation.Valid() || !s.Configuration.Valid() || s.Epoch == 0 {
		return false
	}
	return s.Kind == 1 && !s.Reporter.Valid() && !s.ReporterIncarnation.Valid() || s.Kind == 2 && s.Reporter.Valid() && s.ReporterIncarnation.Valid()
}

func (s Stream) matches(h record.Header) bool {
	return h.Volume == s.Volume && h.Generation == s.Generation && h.Configuration == s.Configuration && h.Epoch == s.Epoch
}

func (s Stream) position(n uint64) record.Position {
	return record.Position{Volume: s.Volume, Generation: s.Generation, Configuration: s.Configuration, Epoch: s.Epoch, Sequence: n}
}

func (s Stream) referenceMatches(r record.FrameRef) bool {
	return r.StreamKind == s.Kind && r.Position == s.position(r.Position.Sequence) && ((s.Kind == 1 && r.Reporter == nil && r.ReporterIncarnation == nil) || (s.Kind == 2 && r.Reporter != nil && r.ReporterIncarnation != nil && *r.Reporter == s.Reporter && *r.ReporterIncarnation == s.ReporterIncarnation))
}

func sameReference(a, b *record.FrameRef) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.StreamKind != b.StreamKind || a.Position != b.Position || a.SHA256 != b.SHA256 {
		return false
	}
	return sameUUID(a.Reporter, b.Reporter) && sameUUID(a.ReporterIncarnation, b.ReporterIncarnation)
}

func sameUUID(a, b *record.UUID) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func hit(f func(string) error, name string) error {
	if f == nil {
		return nil
	}
	return f(name)
}

func corrupt(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrCorrupt, fmt.Sprintf(format, args...))
}
