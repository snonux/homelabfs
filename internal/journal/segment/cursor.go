package segment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"os"

	"github.com/snonux/homelabfs/internal/journal/record"
)

// Cursor is a reusable verified view of one stream for a long-lived reader
// (task te4). The first Refresh runs the complete Recover verification and then
// decodes every frame once; later calls decode only frames appended since.
//
// Integrity is not weakened by remembering the verified prefix: every Refresh
// first proves that the already verified bytes are unchanged, by rediscovering
// the directory (same segment names and sizes, only the last segment may grow
// or be renamed by its SEAL publication) and rehashing the raw bytes of every
// verified segment against the SHA-256 recorded when they were decoded. Equal
// bytes under the same stream and predecessor make every structural check
// (sequence, OPEN/SEAL, digest, stream role, capacity) give the same result,
// so only appended bytes need decoding, and they pass through the exact
// consume() checks Recover and Reader use. Any difference at all (rewrite,
// truncation, reordering, renaming, a torn tail, a decode failure) discards
// the cursor and reruns the complete verification, so the caller observes
// exactly what a fresh OpenReader would. The prefix check is one sequential
// SHA-256 pass without decoding, allocation per frame or reference building.
//
// Callers must exclude stream mutation for the duration of each Refresh, as
// for Recover and Reader. A Cursor is not safe for concurrent use.
type Cursor struct {
	fs       Backend
	stream   Stream
	options  RecoveryOptions
	verified []verifiedSegment
	state    scanState
	summary  Summary
	raw      hash.Hash
	previous *record.FrameRef
	next     uint64
	ids      map[record.UUID]struct{}
	valid    bool
}

// verifiedSegment pins the exact raw bytes [0,size) decoded so far, plus the
// filename they were read under.
type verifiedSegment struct {
	name segmentName
	size int64
	raw  [32]byte
}

// NewCursor binds a cursor to a stream; nothing is read until Refresh.
func NewCursor(backend Backend, stream Stream, options RecoveryOptions) *Cursor {
	return &Cursor{fs: backend, stream: stream, options: options}
}

// Refresh verifies the stream and calls visit for every frame not yet
// visited, in stream order. reset is called first whenever the cursor had to
// start over (first use or any change to verified bytes); the caller must
// then forget every frame it was previously given. On error the cursor is
// discarded, so the next call reverifies everything.
func (c *Cursor) Refresh(ctx context.Context, reset func(), visit func(Entry) error) error {
	if c.valid {
		names, err := discover(ctx, c.fs, c.stream.Directory(), c.options.Limits)
		if err == nil && c.prefixIntact(ctx, names) {
			if err = c.scan(ctx, names, visit); err == nil {
				return nil
			}
		}
	}
	return c.rebuild(ctx, reset, visit)
}

// rebuild is the complete verification: Recover (with its repair and SEAL
// publication side effects, exactly as OpenReader) and then a decode of every
// frame from the first retained segment.
func (c *Cursor) rebuild(ctx context.Context, reset func(), visit func(Entry) error) error {
	c.valid = false
	if _, err := Recover(ctx, c.fs, c.stream, c.options); err != nil {
		return err
	}
	names, err := discover(ctx, c.fs, c.stream.Directory(), c.options.Limits)
	if err != nil {
		return err
	}
	c.verified, c.ids, c.raw = nil, map[record.UUID]struct{}{}, nil
	c.previous, c.next = cloneReference(c.options.PreviousSeal), 1
	if c.previous != nil {
		c.next = c.previous.Position.Sequence + 1
	}
	reset()
	if err = c.scan(ctx, names, visit); err != nil {
		return err
	}
	return nil
}

// prefixIntact proves the verified bytes are still exactly on disk. Only the
// last verified segment may have grown, or been renamed from its open name to
// the closed name its SEAL publishes.
func (c *Cursor) prefixIntact(ctx context.Context, names []segmentName) bool {
	if len(names) < len(c.verified) {
		return false
	}
	last := len(c.verified) - 1
	for i, v := range c.verified {
		n := names[i]
		renamed := i == last && v.name.open && !n.open && n.first == v.name.first
		if n.base != v.name.base && !renamed || n.size < v.size || i < last && n.size != v.size {
			return false
		}
		if !c.rawMatches(ctx, n.base, v) {
			return false
		}
	}
	return true
}

// rawMatches rehashes the verified byte range of one segment file.
func (c *Cursor) rawMatches(ctx context.Context, base string, v verifiedSegment) bool {
	f, err := c.fs.Open(ctx, c.stream.Directory()+"/"+base, os.O_RDONLY, 0)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	digest := sha256.New()
	buffer := make([]byte, scanWindowBytes)
	for offset := int64(0); offset < v.size; {
		chunk := buffer[:min(int64(len(buffer)), v.size-offset)]
		n, err := f.ReadAt(ctx, chunk, offset)
		if err != nil && !(errors.Is(err, io.EOF) && n == len(chunk)) || n != len(chunk) {
			return false
		}
		_, _ = digest.Write(chunk)
		offset += int64(n)
	}
	return bytes.Equal(digest.Sum(nil), v.raw[:])
}

// scan decodes every byte after the verified prefix: first the tail of the
// last verified segment (unless sealed), then each new segment.
func (c *Cursor) scan(ctx context.Context, names []segmentName, visit func(Entry) error) error {
	c.valid = false
	start := len(c.verified)
	if start > 0 && c.summary.Seal == nil {
		start--
	}
	for i := start; i < len(names); i++ {
		if err := c.scanSegment(ctx, names, i, visit); err != nil {
			return err
		}
	}
	c.valid = true
	return nil
}

// scanSegment decodes names[i] from the cursor's position in it: resuming
// the last verified (unsealed) segment, or starting a new one with the gap,
// interior-OPEN and identity checks Recover applies.
func (c *Cursor) scanSegment(ctx context.Context, names []segmentName, i int, visit func(Entry) error) error {
	n := names[i]
	if n.open && i != len(names)-1 {
		return corrupt("interior open segment")
	}
	if i == len(c.verified) {
		if n.first != c.next {
			return corrupt("segment gap/overlap at %s", n.base)
		}
		c.summary, c.state, c.raw = Summary{}, scanState{digest: sha256.New()}, sha256.New()
		c.verified = append(c.verified, verifiedSegment{})
	}
	c.summary.Name, c.summary.Size = c.stream.Directory()+"/"+n.base, n.size
	f, err := c.fs.Open(ctx, c.summary.Name, os.O_RDONLY, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	window := newScanWindow(f, n.size)
	for c.state.offset < n.size {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := c.scanFrame(ctx, window, n, visit); err != nil {
			return err
		}
	}
	return c.finishSegment(n, i)
}

// scanFrame reads, verifies and visits one frame, mirroring Reader.next.
// A torn or corrupt frame is an error here: only the complete verification
// (rebuild) decides between repair and refusal.
func (c *Cursor) scanFrame(ctx context.Context, window *scanWindow, n segmentName, visit func(Entry) error) error {
	data, frame, err := readFrame(ctx, window, c.state.offset, n.size)
	if err != nil {
		return err
	}
	first := c.state.count == 0
	if err = consume(c.stream, &c.summary, &c.state, frame, data, c.previous); err != nil {
		return err
	}
	if first {
		if c.summary.Open.First != n.first {
			return corrupt("OPEN differs from filename")
		}
		if _, exists := c.ids[c.summary.Open.Segment]; exists {
			return corrupt("reused segment identity")
		}
		c.ids[c.summary.Open.Segment] = struct{}{}
	}
	if c.summary.Seal != nil && c.state.offset != n.size {
		return corrupt("bytes after SEAL")
	}
	_, _ = c.raw.Write(data)
	ref := c.summary.Seal
	if ref == nil {
		computed, err := record.ReferenceTo(frame, data)
		if err != nil {
			return err
		}
		ref = &computed
	}
	return visit(Entry{Bytes: data, Record: frame, Reference: *cloneReference(ref)})
}

// finishSegment applies Recover's whole-file checks and pins the segment's
// verified bytes. A SEAL still under its open name is an unfinished
// publication that only the complete verification may complete.
func (c *Cursor) finishSegment(n segmentName, i int) error {
	if c.state.count == 0 {
		return corrupt("missing/incomplete OPEN")
	}
	if !n.open && (c.summary.Seal == nil || n.last != c.summary.Last) {
		return corrupt("closed filename/seal mismatch")
	}
	if n.open && c.summary.Seal != nil {
		return corrupt("unpublished SEAL")
	}
	if c.summary.Last == math.MaxUint64 {
		return fmt.Errorf("sequence exhausted: %w", ErrLimit)
	}
	c.verified[i] = verifiedSegment{name: n, size: n.size, raw: [32]byte(c.raw.Sum(nil))}
	c.next = c.summary.Last + 1
	if c.summary.Seal != nil {
		c.previous = cloneReference(c.summary.Seal)
	}
	return nil
}
