package segment

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"hash"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/vfs"
)

type frameReader interface {
	ReadAt(context.Context, []byte, int64) (int, error)
}

type scanState struct {
	digest hash.Hash
	count  uint64
	last   uint64
	offset int64
}

type segmentName struct {
	base        string
	first, last uint64
	open        bool
	size        int64
}

// Recover verifies the entire bounded retained suffix before repairing a tail
// or completing publication of a sealed open-name file. The caller must exclude
// all mutations, including other backend instances, for this operation and the
// subsequent writer/reader lifetime. The stream directory must already exist.
func Recover(ctx context.Context, backend Backend, stream Stream, options RecoveryOptions) (Recovery, error) {
	r, _, err := recoverStream(ctx, backend, stream, options)
	return r, err
}

func recoverStream(ctx context.Context, backend Backend, stream Stream, options RecoveryOptions) (Recovery, scanState, error) {
	r := Recovery{Stream: stream, Next: 1}
	var state scanState
	if !stream.valid() {
		return r, state, fmt.Errorf("invalid stream")
	}
	previous := options.PreviousSeal
	if previous != nil {
		if !stream.referenceMatches(*previous) || previous.Position.Sequence == 0 || previous.Position.Sequence == math.MaxUint64 {
			return r, state, fmt.Errorf("invalid predecessor anchor")
		}
		r.Next = previous.Position.Sequence + 1
	}
	names, err := discover(ctx, backend, stream.Directory(), options.Limits)
	if err != nil {
		return r, state, err
	}
	var tail *Tail
	identities := make(map[record.UUID]struct{}, len(names))
	for i, name := range names {
		if name.first != r.Next {
			return r, state, corrupt("segment gap/overlap at %s", name.base)
		}
		if name.open && i != len(names)-1 {
			return r, state, corrupt("interior open segment")
		}
		summary, scanned, incomplete, err := scanFile(ctx, backend, stream, name, previous)
		if err != nil {
			return r, state, err
		}
		state = scanned
		if _, exists := identities[summary.Open.Segment]; exists {
			return r, state, corrupt("reused segment identity")
		}
		identities[summary.Open.Segment] = struct{}{}
		if incomplete != nil {
			if !name.open || i != len(names)-1 {
				return r, state, corrupt("closed/interior incomplete frame")
			}
			tail = incomplete
		}
		if summary.Last == math.MaxUint64 {
			return r, state, fmt.Errorf("sequence exhausted: %w", ErrLimit)
		}
		r.Next = summary.Last + 1
		previous = summary.Seal
		r.Segments = append(r.Segments, summary)
	}
	if tail != nil {
		if options.Repair == nil {
			return r, state, &TornTail{*tail}
		}
		if err := options.Repair(ctx, *tail); err != nil {
			return r, state, fmt.Errorf("tail authority/evidence: %w", err)
		}
		if err := truncateTail(ctx, backend, *tail, options.Failpoint); err != nil {
			return r, state, err
		}
		r.Segments[len(r.Segments)-1].Size = tail.Boundary
	}
	for i := range r.Segments {
		s := &r.Segments[i]
		if s.Seal != nil && strings.HasSuffix(s.Name, "-open.hfj") {
			closed := stream.Directory() + "/" + closedName(s.Open.First, s.Last)
			if err := syncExisting(ctx, backend, s.Name, options.Failpoint); err != nil {
				return r, state, err
			}
			if err := publish(ctx, backend, s.Name, closed, options.Failpoint); err != nil {
				return r, state, err
			}
			s.Name = closed
		}
	}
	return r, state, nil
}

func discover(ctx context.Context, backend Backend, dir string, limits Limits) ([]segmentName, error) {
	limits = defaultLimits(limits)
	if limits.Segments < 1 || limits.Bytes < 1 {
		return nil, fmt.Errorf("invalid recovery limits")
	}
	d, err := backend.OpenDir(ctx, dir)
	if err != nil {
		return nil, fmt.Errorf("discover journal: %w", err)
	}
	defer func() { _ = d.Close() }()
	var names []segmentName
	var total int64
	for {
		entries, err := d.ReadDir(ctx, 64)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("discover journal: %w", err)
		}
		for _, entry := range entries {
			if len(names) >= limits.Segments {
				return nil, ErrLimit
			}
			if !entry.Info.Mode.IsRegular() || entry.Info.Links != 1 || entry.Info.Size < 0 || entry.Info.Size > 268435456 || entry.Info.Size > limits.Bytes-total {
				return nil, fmt.Errorf("journal entry %s: %w", entry.Name, ErrLimit)
			}
			n, parseErr := parseName(entry.Name)
			if parseErr != nil {
				return nil, parseErr
			}
			n.size = entry.Info.Size
			total += n.size
			names = append(names, n)
		}
		if errors.Is(err, io.EOF) {
			break
		}
	}
	sort.Slice(names, func(i, j int) bool { return names[i].first < names[j].first })
	return names, nil
}

func scanFile(ctx context.Context, backend Backend, stream Stream, name segmentName, previous *record.FrameRef) (Summary, scanState, *Tail, error) {
	full := stream.Directory() + "/" + name.base
	summary := Summary{Name: full, Size: name.size}
	state := scanState{digest: sha256.New()}
	f, err := backend.Open(ctx, full, os.O_RDONLY, 0)
	if err != nil {
		return summary, state, nil, err
	}
	defer func() { _ = f.Close() }()
	window := newScanWindow(f, name.size)
	for state.offset < name.size {
		data, r, err := readFrame(ctx, window, state.offset, name.size)
		if err != nil {
			var frameErr *record.Error
			if errors.As(err, &frameErr) && frameErr.Kind == record.Torn && state.count > 0 && name.open {
				if err := validateTail(stream, summary, state, data); err != nil {
					return summary, state, nil, err
				}
				return summary, state, &Tail{Name: full, Boundary: state.offset, Size: name.size}, nil
			}
			if errors.As(err, &frameErr) && frameErr.Kind == record.Torn {
				return summary, state, nil, corrupt("closed/interior truncation or interrupted OPEN in %s", full)
			}
			return summary, state, nil, fmt.Errorf("%s at %d: %w", full, state.offset, err)
		}
		if err := consume(stream, &summary, &state, r, data, previous); err != nil {
			return summary, state, nil, err
		}
		if summary.Open.First != name.first {
			return summary, state, nil, corrupt("OPEN differs from filename")
		}
		if summary.Seal != nil && state.offset != name.size {
			return summary, state, nil, corrupt("bytes after SEAL")
		}
	}
	if state.count == 0 {
		return summary, state, nil, corrupt("missing/incomplete OPEN")
	}
	if summary.Open.First != name.first {
		return summary, state, nil, corrupt("OPEN differs from filename")
	}
	if !name.open && (summary.Seal == nil || name.last != summary.Last) {
		return summary, state, nil, corrupt("closed filename/seal mismatch")
	}
	return summary, state, nil, nil
}

func consume(stream Stream, summary *Summary, state *scanState, r record.Record, data []byte, previous *record.FrameRef) error {
	if summary.Seal != nil {
		return corrupt("bytes after SEAL")
	}
	if !stream.matches(r.Header) {
		return corrupt("frame history differs from stream")
	}
	if state.count == 0 {
		open, ok := r.Payload.(record.SegmentOpen)
		if !ok || open.First != r.Header.Sequence || !sameReference(open.PreviousSeal, previous) {
			return corrupt("OPEN/predecessor mismatch")
		}
		ref, err := record.ReferenceTo(r, data)
		if err != nil {
			return err
		}
		if !stream.referenceMatches(ref) {
			return corrupt("OPEN stream role")
		}
		// The reader returns the decoded frame to its caller. Keep independent
		// pointer fields for later SEAL validation and predecessor checks.
		summary.Open = cloneOpen(open)
	} else {
		if state.last == math.MaxUint64 || r.Header.Sequence != state.last+1 {
			return corrupt("noncontiguous sequence")
		}
		switch p := r.Payload.(type) {
		case record.SegmentOpen:
			return corrupt("interior OPEN")
		case record.SegmentSeal:
			var digest record.Hash
			copy(digest[:], state.digest.Sum(nil))
			if p.Segment != summary.Open.Segment || p.First != summary.Open.First || p.LastBeforeSeal != state.last || p.FrameCount != state.count || p.ByteCount != uint64(state.offset) || p.ContentSHA256 != digest {
				return corrupt("SEAL hash/count/identity")
			}
			ref, err := record.SealReference(r, data, summary.Open)
			if err != nil {
				return err
			}
			if !stream.referenceMatches(ref) {
				return corrupt("SEAL stream role")
			}
			summary.Seal = &ref
		default:
			ref, err := record.ReferenceTo(r, data)
			if err != nil {
				return err
			}
			if !stream.referenceMatches(ref) {
				return corrupt("record stream role")
			}
		}
	}
	if uint64(state.offset)+uint64(len(data)) > summary.Open.MaximumBytes {
		return corrupt("segment exceeds OPEN maximum")
	}
	if summary.Seal == nil && uint64(state.offset)+uint64(len(data))+sealBytes > summary.Open.MaximumBytes {
		return corrupt("missing reserved SEAL space")
	}
	if summary.Seal == nil {
		_, _ = state.digest.Write(data)
		state.count++
	}
	state.last = r.Header.Sequence
	state.offset += int64(len(data))
	summary.Last = state.last
	return nil
}

func defaultLimits(limits Limits) Limits {
	if limits.Segments == 0 {
		limits.Segments = 1024
	}
	if limits.Bytes == 0 {
		limits.Bytes = 1 << 30
	}
	return limits
}

func readFrame(ctx context.Context, f frameReader, offset, size int64) ([]byte, record.Record, error) {
	var r record.Record
	remaining := size - offset
	header := make([]byte, min(remaining, 144))
	if _, err := f.ReadAt(ctx, header, offset); err != nil {
		return nil, r, err
	}
	_, err := record.Decode(header)
	var frameErr *record.Error
	if !errors.As(err, &frameErr) || frameErr.Kind != record.Torn {
		return nil, r, err
	}
	if len(header) < 144 {
		return header, r, err
	}
	total := int64(binary.BigEndian.Uint32(header[20:]))
	// Decode already validated prefix, header identities and bounded lengths.
	if total > remaining {
		return header, r, err
	}
	data := make([]byte, total)
	copy(data, header)
	if _, err := f.ReadAt(ctx, data[144:], offset+144); err != nil {
		return nil, r, err
	}
	r, err = record.Decode(data)
	// Torn is meaningful only when the outer physical frame is incomplete.
	// Nested evidence/parser failures in complete bytes are never repairable.
	if errors.As(err, &frameErr) && frameErr.Kind == record.Torn {
		return data, r, corrupt("complete outer frame rejected: %v", err)
	}
	return data, r, err
}

func validateTail(stream Stream, summary Summary, state scanState, header []byte) error {
	if len(header) < 144 {
		return nil
	}
	var h record.Header
	copy(h.Volume[:], header[32:48])
	copy(h.Generation[:], header[48:64])
	copy(h.Configuration[:], header[64:80])
	h.Epoch = binary.BigEndian.Uint64(header[80:88])
	h.Sequence = binary.BigEndian.Uint64(header[88:96])
	copy(h.Incarnation[:], header[112:128])
	if !stream.matches(h) || state.last == math.MaxUint64 || h.Sequence != state.last+1 || stream.Kind == 2 && h.Incarnation != stream.ReporterIncarnation {
		return corrupt("incomplete frame has conflicting stream identity/sequence")
	}
	kind := binary.BigEndian.Uint16(header[14:16])
	if kind == 67 || stream.Kind == 1 && kind >= 48 && kind <= 50 || stream.Kind == 2 && kind != 68 && (kind < 48 || kind > 50) {
		return corrupt("incomplete frame has wrong stream role")
	}
	total := uint64(binary.BigEndian.Uint32(header[20:24]))
	reserve := uint64(sealBytes)
	if kind == 68 {
		reserve = 0
		if total != sealBytes {
			return corrupt("incomplete SEAL length")
		}
	}
	if uint64(state.offset)+total+reserve > summary.Open.MaximumBytes {
		return corrupt("incomplete frame exceeds segment capacity")
	}
	return nil
}

func truncateTail(ctx context.Context, backend Backend, tail Tail, fail func(string) error) error {
	f, err := backend.Open(ctx, tail.Name, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if err := hit(fail, "before-truncate"); err != nil {
		return err
	}
	if err := f.Truncate(ctx, tail.Boundary); err != nil {
		return err
	}
	if err := hit(fail, "after-truncate"); err != nil {
		return err
	}
	return syncFile(ctx, f, fail)
}

func syncExisting(ctx context.Context, backend Backend, name string, fail func(string) error) error {
	f, err := backend.Open(ctx, name, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return syncFile(ctx, f, fail)
}

func parseName(base string) (segmentName, error) {
	n := segmentName{base: base}
	if len(base) < 29 || base[20] != '-' || !strings.HasSuffix(base, ".hfj") {
		return n, corrupt("invalid segment filename %s", base)
	}
	first, err := strconv.ParseUint(base[:20], 10, 64)
	if err != nil || first == 0 || fmt.Sprintf("%020d", first) != base[:20] {
		return n, corrupt("invalid first sequence")
	}
	n.first = first
	if base[21:] == "open.hfj" {
		n.open = true
		return n, nil
	}
	if len(base) != 45 {
		return n, corrupt("invalid closed filename")
	}
	last, err := strconv.ParseUint(base[21:41], 10, 64)
	if err != nil || last <= first || fmt.Sprintf("%020d", last) != base[21:41] {
		return n, corrupt("invalid final sequence")
	}
	n.last = last
	return n, nil
}

func closedName(first, last uint64) string { return fmt.Sprintf("%020d-%020d.hfj", first, last) }

func publish(ctx context.Context, backend Backend, oldname, newname string, fail func(string) error) error {
	if _, err := backend.Stat(ctx, newname); !errors.Is(err, os.ErrNotExist) {
		if err != nil {
			return err
		}
		return corrupt("publication destination already exists")
	}
	if err := hit(fail, "before-rename"); err != nil {
		return err
	}
	if err := backend.Rename(ctx, oldname, newname); err != nil {
		return err
	}
	if err := hit(fail, "after-rename"); err != nil {
		return err
	}
	return syncDirectory(ctx, backend, newname[:strings.LastIndexByte(newname, '/')], fail)
}

func syncFile(ctx context.Context, file vfs.File, fail func(string) error) error {
	if err := hit(fail, "before-sync"); err != nil {
		return err
	}
	if err := file.Sync(ctx); err != nil {
		return err
	}
	return hit(fail, "after-sync")
}

func syncDirectory(ctx context.Context, backend Backend, dir string, fail func(string) error) error {
	if err := hit(fail, "before-directory-sync"); err != nil {
		return err
	}
	if err := backend.SyncDir(ctx, dir); err != nil {
		return err
	}
	return hit(fail, "after-directory-sync")
}
