package segment

import (
	"bytes"
	"context"
	"fmt"
	"io"

	"github.com/snonux/homelabfs/internal/journal/record"
)

// VerifyCheckpointSource verifies surviving source bytes against an immutable
// complete sealed genesis archive. Missing leading archived segments are legal;
// interior gaps, conflicting surviving prefix bytes and broken suffix chains
// fail closed. Deletion must proceed in order, syncing after each unlink.
func VerifyCheckpointSource(ctx context.Context, backend Backend, stream Stream, limits Limits, frames [][]byte) (Recovery, error) {
	r, _, err := checkpointSource(ctx, backend, stream, limits, frames)
	return r, err
}

// OpenCheckpointSuffix revalidates the remaining prefix/archive and opens only
// frames after its final seal. It handles a crash leaving any leading portion
// of the obsolete prefix removed; no source bytes are repaired or rewritten.
func OpenCheckpointSuffix(ctx context.Context, backend Backend, stream Stream, limits Limits, frames [][]byte) (*Reader, error) {
	_, options, err := checkpointSource(ctx, backend, stream, limits, frames)
	if err != nil {
		return nil, err
	}
	return OpenReader(ctx, backend, stream, options, stream.position(uint64(len(frames))+1))
}

func checkpointSource(ctx context.Context, backend Backend, stream Stream, limits Limits, frames [][]byte) (Recovery, RecoveryOptions, error) {
	entries, err := VerifyFrames(ctx, stream, frames)
	if err != nil {
		return Recovery{}, RecoveryOptions{}, err
	}
	// The retained suffix is scanned independently of the archive. Preserve the
	// archive identity namespace as well: a trimmed segment must never become a
	// fresh suffix segment merely because its original file is absent.
	archivedIDs := make(map[record.UUID]uint64)
	for _, entry := range entries {
		if open, ok := entry.Record.Payload.(record.SegmentOpen); ok {
			archivedIDs[open.Segment] = open.First
		}
	}
	names, err := discover(ctx, backend, stream.Directory(), limits)
	if err != nil {
		return Recovery{}, RecoveryOptions{}, err
	}
	first := uint64(len(frames)) + 1
	if len(names) > 0 {
		first = names[0].first
	}
	if first == 0 || first > uint64(len(frames))+1 {
		return Recovery{}, RecoveryOptions{}, corrupt("missing checkpoint suffix")
	}
	options := RecoveryOptions{Limits: limits}
	if first > 1 {
		if _, ok := entries[first-2].Record.Payload.(record.SegmentSeal); !ok {
			return Recovery{}, options, corrupt("missing prefix does not end on archived seal")
		}
		ref := entries[first-2].Reference
		options.PreviousSeal = &ref
	}
	recovery, err := Recover(ctx, backend, stream, options)
	if err != nil {
		return recovery, options, err
	}
	for _, segment := range recovery.Segments {
		if archivedFirst, exists := archivedIDs[segment.Open.Segment]; exists && segment.Open.First != archivedFirst {
			return recovery, options, corrupt("suffix reuses archived segment identity")
		}
	}
	reader, err := OpenReader(ctx, backend, stream, options, stream.position(first))
	if err != nil {
		return recovery, options, err
	}
	defer func() { _ = reader.Close() }()
	for {
		e, err := reader.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return recovery, options, err
		}
		seq := e.Reference.Position.Sequence
		if seq <= uint64(len(frames)) && !bytes.Equal(e.Bytes, frames[seq-1]) {
			return recovery, options, corrupt("source differs from checkpoint archive at %d", seq)
		}
	}
	if recovery.Next < uint64(len(frames))+1 {
		return recovery, options, fmt.Errorf("%w: source ends before checkpoint", ErrCorrupt)
	}
	return recovery, options, nil
}
