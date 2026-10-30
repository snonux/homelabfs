package segment

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/vfs"
)

// Entry owns one complete immutable frame and its decoded representation.
// Its buffers and pointer fields are independent of reader verification state.
type Entry struct {
	Bytes     []byte
	Record    record.Record
	Reference record.FrameRef
}

// Reader iterates a fixed, fully verified retained stream snapshot. Exclude
// mutation and retention of its files for its lifetime. Memory is bounded by the
// recovery directory budget, one decoded frame and a 64 KiB read window, never
// the complete stream.
type Reader struct {
	mu       sync.Mutex
	fs       Backend
	recovery Recovery
	index    int
	file     vfs.File
	window   *scanWindow
	state    scanState
	summary  Summary
	previous *record.FrameRef
	first    uint64
	err      error
	closed   bool
}

// OpenReader verifies the complete retained suffix before yielding frames at or
// after first (inclusive). Structural frames are included. first must name this
// stream history; reporter identity comes from stream, not Position alone.
func OpenReader(ctx context.Context, backend Backend, stream Stream, options RecoveryOptions, first record.Position) (*Reader, error) {
	if first.Sequence == 0 || first != stream.position(first.Sequence) {
		return nil, fmt.Errorf("invalid reader position")
	}
	r, err := Recover(ctx, backend, stream, options)
	if err != nil {
		return nil, err
	}
	start := uint64(1)
	if options.PreviousSeal != nil {
		start = options.PreviousSeal.Position.Sequence + 1
	}
	if first.Sequence < start || first.Sequence > r.Next {
		return nil, fmt.Errorf("reader position outside retained stream")
	}
	return &Reader{fs: backend, recovery: r, first: first.Sequence, previous: cloneReference(options.PreviousSeal)}, nil
}

// Next returns exact stored bytes and a stream-aware reference, or io.EOF.
// Errors stop iteration; frames are never skipped and no implicit repair occurs.
func (r *Reader) Next(ctx context.Context) (Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return Entry{}, ErrClosed
	}
	if r.err != nil {
		return Entry{}, r.err
	}
	for {
		entry, err := r.next(ctx)
		if err != nil {
			r.err = err
			return Entry{}, err
		}
		if entry.Record.Header.Sequence >= r.first {
			return entry, nil
		}
	}
}

// Close releases the current file descriptor.
func (r *Reader) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	if r.file != nil {
		return r.file.Close()
	}
	return nil
}

func (r *Reader) next(ctx context.Context) (Entry, error) {
	if err := ctx.Err(); err != nil {
		return Entry{}, err
	}
	for r.file == nil || r.state.offset == r.summary.Size {
		if r.file != nil {
			if err := r.file.Close(); err != nil {
				return Entry{}, err
			}
			r.file = nil
			r.window = nil
			r.previous = cloneReference(r.summary.Seal)
		}
		if r.index == len(r.recovery.Segments) {
			return Entry{}, io.EOF
		}
		r.summary = r.recovery.Segments[r.index]
		r.index++
		f, err := r.fs.Open(ctx, r.summary.Name, os.O_RDONLY, 0)
		if err != nil {
			return Entry{}, err
		}
		r.file = f
		r.window = newScanWindow(f, r.summary.Size)
		r.state = scanState{digest: sha256.New()}
		// Revalidate content as it is read, keeping recovered metadata separate.
		r.summary.Open = record.SegmentOpen{}
		r.summary.Seal = nil
	}
	data, frame, err := readFrame(ctx, r.window, r.state.offset, r.summary.Size)
	if err != nil {
		return Entry{}, err
	}
	if err := consume(r.recovery.Stream, &r.summary, &r.state, frame, data, r.previous); err != nil {
		return Entry{}, err
	}
	var ref record.FrameRef
	if r.summary.Seal != nil {
		ref = *r.summary.Seal
	} else {
		ref, err = record.ReferenceTo(frame, data)
	}
	if err != nil {
		return Entry{}, err
	}
	// In particular SealReference uses the internally verified OPEN pointers.
	// Returned references must never expose those to an owned mutable entry.
	ref = *cloneReference(&ref)
	return Entry{Bytes: data, Record: frame, Reference: ref}, nil
}
