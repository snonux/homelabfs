package transactions

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/journal/segment"
)

func (e *Engine) addGroup(ctx context.Context, g Group) error {
	var err error
	g, err = e.storageGroup(g)
	if err != nil {
		return err
	}
	a := g.Binding.Authority
	if g.Backend == nil || g.Writer.GroupCommit || g.Stream.Kind != 1 || g.Stream.Volume != a.Volume || g.Stream.Generation != a.Generation || g.Stream.Configuration != a.Configuration || g.Stream.Epoch != a.Epoch || !g.Binding.Session.Valid() || !g.Binding.Connection.Valid() || e.groups[a.Volume].Backend != nil || !validAuthority(a) {
		return missing("installed owner stream/session")
	}
	if e.opts.Recovery == nil {
		if err := e.opts.Protector.Check(ctx, cloneBinding(g.Binding)); err != nil {
			return fmt.Errorf("%w: installed authority: %w", ErrEvidence, err)
		}
	}
	g.Binding = cloneBinding(g.Binding)
	w, err := segment.NewWriter(ctx, g.Backend, g.Stream, g.Writer)
	if err != nil {
		return err
	}
	e.groups[a.Volume] = g
	e.streams[streamKey(g.Stream)] = g
	e.writers[a.Volume] = w
	return nil
}

func (e *Engine) storageGroup(g Group) (Group, error) {
	if g.Writer.Recovery.PreviousSeal != nil || g.Writer.Recovery.Repair != nil {
		return g, missing("untrimmed immutable history required")
	}
	if g.Writer.Recovery.Limits.Bytes == 0 || g.Writer.Recovery.Limits.Bytes > int64(e.opts.MaximumBytes) {
		g.Writer.Recovery.Limits.Bytes = int64(e.opts.MaximumBytes)
	}
	if g.Writer.Recovery.Limits.Segments == 0 || g.Writer.Recovery.Limits.Segments > e.opts.MaximumFrames {
		g.Writer.Recovery.Limits.Segments = e.opts.MaximumFrames
	}
	return g, nil
}

func (e *Engine) read(ctx context.Context, g Group) ([]segment.Entry, error) {
	p := record.Position{Volume: g.Stream.Volume, Generation: g.Stream.Generation, Configuration: g.Stream.Configuration, Epoch: g.Stream.Epoch, Sequence: 1}
	if g.Writer.Recovery.PreviousSeal != nil {
		return nil, missing("checkpoint replacement replay is not installed")
	}
	r, err := segment.OpenReader(ctx, g.Backend, g.Stream, g.Writer.Recovery, p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	var entries []segment.Entry
	total := 0
	for {
		entry, err := r.Next(ctx)
		if errors.Is(err, io.EOF) {
			return entries, nil
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
		total += len(entry.Bytes)
		if len(entries) > e.opts.MaximumFrames || total > e.opts.MaximumBytes {
			return nil, ErrLimit
		}
	}
}

func (e *Engine) replay(ctx context.Context, pending [][]segment.Entry) error {
	count := 0
	for _, entries := range pending {
		count += len(entries)
	}
	if count > e.opts.MaximumFrames {
		return ErrLimit
	}
	for count > 0 {
		progress := false
		for i, entries := range pending {
			if len(entries) == 0 {
				continue
			}
			entry := entries[0]
			// Drain predecessor owner histories before replaying successor effects.
			// An abort in the successor can legitimately have no prepare references,
			// but must not make an older participant prepare appear newly authored.
			if !structural(entry) && entry.Reference.StreamKind == 1 && entryStream(entry) == streamKey(e.groups[entry.Record.Header.Volume].Stream) && pendingHistoricalOwners(e, pending) {
				continue
			}
			if err := e.current(entry); err != nil {
				return err
			}
			if err := e.state.admit(entry, false); err != nil {
				if errors.Is(err, ErrEvidence) {
					continue
				}
				return err
			}
			if recoveryPayload(entry.Record.Payload) || entryStream(entry) != streamKey(e.groups[entry.Record.Header.Volume].Stream) {
				if err := e.verifyRecovery(ctx, entry); err != nil {
					return err
				}
			}
			if err := e.state.admit(entry, true); err != nil {
				return err
			}
			if err := e.capacity(len(entry.Bytes)); err != nil {
				return err
			}
			e.bytes += len(entry.Bytes)
			e.entries = append(e.entries, entry)
			pending[i] = entries[1:]
			count--
			progress = true
		}
		if !progress {
			return missing("missing/cyclic replay dependency")
		}
	}
	return nil
}

func pendingHistoricalOwners(e *Engine, pending [][]segment.Entry) bool {
	for _, entries := range pending {
		if len(entries) == 0 {
			continue
		}
		entry := entries[0]
		if entry.Reference.StreamKind == 1 && entryStream(entry) != streamKey(e.groups[entry.Record.Header.Volume].Stream) {
			return true
		}
	}
	return false
}
