package segment

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/snonux/homelabfs/internal/journal/record"
)

// NewWriter recovers an exclusively owned stream, then opens or creates its
// writable segment. The directory hierarchy must already be durable. Append
// accepts newly authored records only; replica import must preserve creator
// bytes and structural frames through a separate protocol, not this allocator.
func NewWriter(ctx context.Context, backend Backend, stream Stream, options Options) (*Writer, error) {
	if options.Structural == nil || options.NewSegment == nil || options.MaximumBytes == 0 || options.MaximumBytes > 268435456 {
		return nil, fmt.Errorf("invalid writer options")
	}
	if options.Recovery.Failpoint == nil {
		options.Recovery.Failpoint = options.Failpoint
	}
	options.Recovery.Limits = defaultLimits(options.Recovery.Limits)
	r, state, err := recoverStream(ctx, backend, stream, options.Recovery)
	if err != nil {
		return nil, err
	}
	// MaxUint64 cannot be consumed by this writer: advancing beyond that
	// structural position would wrap Next and make the stream unrecoverable.
	if r.Next == math.MaxUint64 {
		return nil, ErrLimit
	}
	w := &Writer{fs: backend, stream: stream, opts: options, next: r.Next, state: state, previous: cloneReference(options.Recovery.PreviousSeal), segments: make(map[record.UUID]struct{}, len(r.Segments)+1)}
	for _, s := range r.Segments {
		w.segments[s.Open.Segment] = struct{}{}
		w.bytes += s.Size
	}
	if len(r.Segments) > 0 {
		w.current = r.Segments[len(r.Segments)-1]
		w.previous = cloneReference(w.current.Seal)
		if w.current.Seal == nil {
			if sealBytes > w.opts.Recovery.Limits.Bytes-w.bytes {
				return nil, ErrLimit
			}
			w.file, err = backend.Open(ctx, w.current.Name, os.O_RDWR, 0)
			if err != nil {
				return nil, err
			}
			if err := syncFile(ctx, w.file, options.Failpoint); err != nil {
				_ = w.file.Close()
				return nil, err
			}
			if err := syncDirectory(ctx, backend, stream.Directory(), options.Failpoint); err != nil {
				_ = w.file.Close()
				return nil, err
			}
			return w, nil
		}
	}
	if err := w.create(ctx); err != nil {
		if w.file != nil {
			_ = w.file.Close()
		}
		return nil, err
	}
	return w, nil
}

// Append assigns the next position to a newly authored record with Sequence=0.
// All other header bytes must already be chosen by its authorized creator and
// match this stream. Input bytes from an existing position must never use this
// method. Rotation may consume OPEN/SEAL positions before the new record.
// Default mode returns only after file sync; group mode returns a pending receipt
// and the caller must arrange Flush. No distributed acknowledgement is implied.
func (w *Writer) Append(ctx context.Context, r record.Record) (Receipt, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.usable(ctx); err != nil {
		return Receipt{}, err
	}
	if !w.stream.matches(r.Header) || r.Header.Sequence != 0 {
		return Receipt{}, fmt.Errorf("append requires newly authored sequence-zero record in this history")
	}
	switch r.Payload.(type) {
	case record.SegmentOpen, record.SegmentSeal:
		return Receipt{}, fmt.Errorf("structural frames are writer-owned")
	}
	if w.current.Seal != nil {
		if err := w.create(ctx); err != nil {
			return Receipt{}, w.fail(err)
		}
	}
	if len(w.pending) >= 1024 {
		return Receipt{}, ErrLimit
	}
	if w.next >= math.MaxUint64-1 {
		return Receipt{}, ErrLimit
	}
	r.Header.Sequence = w.next
	data, err := record.Encode(r)
	if err != nil {
		return Receipt{}, err
	}
	ref, err := record.ReferenceTo(r, data)
	if err != nil {
		return Receipt{}, err
	}
	if !w.stream.referenceMatches(ref) {
		return Receipt{}, fmt.Errorf("record has wrong owner/reporter role")
	}
	if int64(len(data))+sealBytes > w.opts.Recovery.Limits.Bytes-w.bytes {
		return Receipt{}, ErrLimit
	}
	if uint64(w.state.offset)+uint64(len(data))+sealBytes > w.current.Open.MaximumBytes {
		// A successor OPEN includes the prior seal reference and may be larger.
		if uint64(len(data))+sealBytes+uint64(w.successorOpenBytes()) > w.opts.MaximumBytes {
			return Receipt{}, ErrLimit
		}
		if len(w.segments) >= w.opts.Recovery.Limits.Segments || int64(len(data)+2*sealBytes+w.successorOpenBytes()) > w.opts.Recovery.Limits.Bytes-w.bytes {
			return Receipt{}, ErrLimit
		}
		if err := w.seal(ctx); err != nil {
			return Receipt{}, w.fail(err)
		}
		if err := w.create(ctx); err != nil {
			return Receipt{}, w.fail(err)
		}
		if w.next >= math.MaxUint64-1 {
			return Receipt{}, w.fail(ErrLimit)
		}
		r.Header.Sequence = w.next
		data, err = record.Encode(r)
		if err != nil {
			return Receipt{}, w.fail(err)
		}
		ref, err = record.ReferenceTo(r, data)
		if err != nil {
			return Receipt{}, w.fail(err)
		}
	}
	if err := w.write(ctx, data); err != nil {
		return Receipt{}, w.fail(err)
	}
	_, _ = w.state.digest.Write(data)
	w.state.count++
	w.state.last = w.next
	w.state.offset += int64(len(data))
	w.bytes += int64(len(data))
	w.current.Last = w.next
	w.current.Size = w.state.offset
	w.next++
	c := &completion{done: make(chan struct{})}
	w.pending = append(w.pending, c)
	receipt := Receipt{Reference: ref, state: c}
	if !w.opts.GroupCommit {
		if err := w.flush(ctx); err != nil {
			return receipt, w.fail(err)
		}
	}
	return receipt, nil
}

// Flush establishes a file barrier covering all previously returned receipts.
// Later appends serialize behind this barrier and need their own covering sync.
func (w *Writer) Flush(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.usable(ctx); err != nil {
		return err
	}
	if err := w.flush(ctx); err != nil {
		return w.fail(err)
	}
	return nil
}

// Seal writes and syncs the seal, publishes the closed name and syncs its parent.
// A subsequent Append creates a new segment. Close alone never seals or syncs.
func (w *Writer) Seal(ctx context.Context) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.usable(ctx); err != nil {
		return err
	}
	if w.current.Seal != nil {
		return nil
	}
	if err := w.seal(ctx); err != nil {
		return w.fail(err)
	}
	return nil
}

// Close releases the descriptor and fails pending unsynced receipts. It adds no
// barrier. After any uncertain failure, recover through a fresh writer.
func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return nil
	}
	w.closed = true
	w.finish(ErrClosed)
	if w.file != nil {
		return w.file.Close()
	}
	return nil
}

func (w *Writer) create(ctx context.Context) error {
	if w.next >= math.MaxUint64-1 || len(w.segments) >= w.opts.Recovery.Limits.Segments {
		return ErrLimit
	}
	open := record.SegmentOpen{Segment: w.opts.NewSegment(), StreamKind: w.stream.Kind, First: w.next, PreviousSeal: cloneReference(w.previous), MaximumBytes: w.opts.MaximumBytes}
	if _, exists := w.segments[open.Segment]; exists {
		return fmt.Errorf("segment identity reused")
	}
	if w.stream.Kind == 2 {
		reporter, inc := w.stream.Reporter, w.stream.ReporterIncarnation
		open.Reporter = &reporter
		open.ReporterIncarnation = &inc
	}
	r := record.Record{Header: w.structuralHeader(w.next), Payload: open}
	data, err := record.Encode(r)
	if err != nil {
		return err
	}
	if int64(len(data))+sealBytes > w.opts.Recovery.Limits.Bytes-w.bytes {
		return ErrLimit
	}
	name := fmt.Sprintf("%s/%020d-open.hfj", w.stream.Directory(), w.next)
	if err := hit(w.opts.Failpoint, "before-create"); err != nil {
		return err
	}
	f, err := w.fs.Open(ctx, name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	w.file = f
	w.segments[open.Segment] = struct{}{}
	if err := hit(w.opts.Failpoint, "after-create"); err != nil {
		return err
	}
	w.state = scanState{digest: sha256.New()}
	if err := w.write(ctx, data); err != nil {
		return err
	}
	if err := syncFile(ctx, w.file, w.opts.Failpoint); err != nil {
		return err
	}
	if err := syncDirectory(ctx, w.fs, w.stream.Directory(), w.opts.Failpoint); err != nil {
		return err
	}
	w.current = Summary{Name: name, Open: open, Last: w.next, Size: int64(len(data))}
	w.bytes += int64(len(data))
	_, _ = w.state.digest.Write(data)
	w.state.count = 1
	w.state.last = w.next
	w.state.offset = int64(len(data))
	w.next++
	return nil
}

func (w *Writer) seal(ctx context.Context) error {
	if w.current.Seal != nil {
		return nil
	}
	if w.next == math.MaxUint64 || sealBytes > w.opts.Recovery.Limits.Bytes-w.bytes {
		return ErrLimit
	}
	var digest record.Hash
	copy(digest[:], w.state.digest.Sum(nil))
	p := record.SegmentSeal{Segment: w.current.Open.Segment, First: w.current.Open.First, LastBeforeSeal: w.state.last, FrameCount: w.state.count, ByteCount: uint64(w.state.offset), ContentSHA256: digest}
	r := record.Record{Header: w.structuralHeader(w.next), Payload: p}
	data, err := record.Encode(r)
	if err != nil {
		return err
	}
	ref, err := record.SealReference(r, data, w.current.Open)
	if err != nil {
		return err
	}
	if !w.stream.referenceMatches(ref) {
		return fmt.Errorf("invalid structural stream role")
	}
	if err := w.write(ctx, data); err != nil {
		return err
	}
	if err := syncFile(ctx, w.file, w.opts.Failpoint); err != nil {
		return err
	}
	// Once the covering sync succeeds, these receipts are durable even when
	// publication later fails. No next segment is admitted until name barriers.
	w.finish(nil)
	closed := w.stream.Directory() + "/" + closedName(w.current.Open.First, w.next)
	if err := publish(ctx, w.fs, w.current.Name, closed, w.opts.Failpoint); err != nil {
		return err
	}
	if err := w.file.Close(); err != nil {
		return err
	}
	w.file = nil
	w.current.Name = closed
	w.current.Seal = &ref
	w.current.Last = w.next
	w.current.Size = w.state.offset + int64(len(data))
	w.bytes += int64(len(data))
	w.previous = cloneReference(&ref)
	w.next++
	return nil
}

func (w *Writer) flush(ctx context.Context) error {
	if w.current.Seal != nil || len(w.pending) == 0 {
		return nil
	}
	if err := syncFile(ctx, w.file, w.opts.Failpoint); err != nil {
		return err
	}
	w.finish(nil)
	return nil
}

func (w *Writer) write(ctx context.Context, data []byte) error {
	if err := hit(w.opts.Failpoint, "before-append"); err != nil {
		return err
	}
	n, err := w.file.WriteAt(ctx, data, w.state.offset)
	if err != nil {
		return err
	}
	if n != len(data) {
		return io.ErrShortWrite
	}
	return hit(w.opts.Failpoint, "after-append")
}

func (w *Writer) structuralHeader(sequence uint64) record.Header {
	h := w.opts.Structural(sequence)
	h.Volume = w.stream.Volume
	h.Generation = w.stream.Generation
	h.Configuration = w.stream.Configuration
	h.Epoch = w.stream.Epoch
	h.Sequence = sequence
	return h
}

func (w *Writer) successorOpenBytes() int {
	// OPEN has 36 payload bytes plus owner/reporter optionals and predecessor.
	// A predecessor owner ref is 99 bytes, a reporter ref 131 bytes.
	if w.stream.Kind == 2 {
		return 148 + 68 + 131
	}
	return 148 + 36 + 99
}

func (w *Writer) usable(ctx context.Context) error {
	if w.closed {
		return ErrClosed
	}
	if w.err != nil {
		return w.err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (w *Writer) fail(err error) error {
	w.err = fmt.Errorf("journal writer requires recovery: %w", err)
	w.finish(w.err)
	return w.err
}

func (w *Writer) finish(err error) {
	for _, c := range w.pending {
		c.err = err
		close(c.done)
	}
	w.pending = nil
}

func cloneReference(r *record.FrameRef) *record.FrameRef {
	if r == nil {
		return nil
	}
	c := *r
	if r.Reporter != nil {
		id := *r.Reporter
		c.Reporter = &id
	}
	if r.ReporterIncarnation != nil {
		id := *r.ReporterIncarnation
		c.ReporterIncarnation = &id
	}
	return &c
}

func cloneOpen(open record.SegmentOpen) record.SegmentOpen {
	if open.Reporter != nil {
		id := *open.Reporter
		open.Reporter = &id
	}
	if open.ReporterIncarnation != nil {
		id := *open.ReporterIncarnation
		open.ReporterIncarnation = &id
	}
	open.PreviousSeal = cloneReference(open.PreviousSeal)
	return open
}
