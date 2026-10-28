package segment

import (
	"context"
	"crypto/sha256"

	"github.com/snonux/homelabfs/internal/journal/record"
)

// VerifyFrames verifies an exact complete genesis prefix ending on SEAL. It
// preserves original bytes and verifies every OPEN/SEAL predecessor, count and
// checksum. This is structural validation only, never commitment authority.
// The caller bounds frame count and total bytes before allocating this input.
func VerifyFrames(ctx context.Context, stream Stream, frames [][]byte) ([]Entry, error) {
	if !stream.valid() || len(frames) == 0 {
		return nil, corrupt("invalid archived prefix")
	}
	var summary Summary
	var previous *record.FrameRef
	state := scanState{digest: sha256.New()}
	entries := make([]Entry, 0, len(frames))
	segments := map[record.UUID]bool{}
	for i, raw := range frames {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		r, err := record.Decode(raw)
		if err != nil {
			return nil, err
		}
		if r.Header.Sequence != uint64(i)+1 {
			return nil, corrupt("archived prefix sequence")
		}
		if summary.Seal != nil {
			previous = cloneReference(summary.Seal)
			summary = Summary{}
			state = scanState{digest: sha256.New()}
		}
		if state.count == 0 {
			p, ok := r.Payload.(record.SegmentOpen)
			if !ok || segments[p.Segment] {
				return nil, corrupt("archived segment identity")
			}
			segments[p.Segment] = true
		}
		if err = consume(stream, &summary, &state, r, raw, previous); err != nil {
			return nil, err
		}
		var ref record.FrameRef
		if summary.Seal != nil {
			ref = *cloneReference(summary.Seal)
		} else {
			ref, err = record.ReferenceTo(r, raw)
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, Entry{Bytes: append([]byte(nil), raw...), Record: r, Reference: ref})
	}
	if summary.Seal == nil {
		return nil, corrupt("archived prefix must end on SEAL")
	}
	return entries, nil
}
