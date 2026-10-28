package apply

import (
	"bytes"
	"context"
	"fmt"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/segment"
	"github.com/snonux/homelabfs/internal/storage"
)

// Advance verifies an append-only extension of this Replay result and installs
// only newly selected effects into the same workspace. The caller must exclude
// reads, journal mutation and all other use of Result/Store throughout this call,
// retaining external authority/closure gates. Any failure poisons this result;
// recovery then requires Replay with fresh roots, never storage.Open on its image.
// This deliberately scans the complete bounded genesis stream; it is not a
// checkpoint, tail follower, retention decision or normal acknowledgement policy.
func (r *Result) Advance(ctx context.Context, source *segment.Reader, selection Selection) (err error) {
	if r.blocked || r.Store == nil || len(r.selection.Effects) == 0 || source == nil {
		r.blocked = true
		return fsapi.Unavailable
	}
	defer func() {
		if err != nil {
			r.blocked = true
		}
	}()
	selection = cloneSelection(selection)
	h, err := readHistory(ctx, source, selection, r.config)
	if err != nil {
		return err
	}
	if len(h.entries) < len(r.history.entries) || len(selection.Effects) < len(r.selection.Effects) {
		return fsapi.Stale
	}
	for i, e := range r.history.entries {
		if !bytes.Equal(e.Bytes, h.entries[i].Bytes) {
			return fmt.Errorf("%w: source prefix changed", fsapi.Stale)
		}
	}
	for i, e := range r.selection.Effects {
		if e != selection.Effects[i] {
			return fmt.Errorf("%w: selected prefix changed", fsapi.Stale)
		}
	}
	if len(selection.Proof) == 0 || len(selection.Proof) > 1<<20 {
		return fsapi.Invalid
	}
	if err = r.config.Authorizer.Authorize(ctx, Authorization{cloneSelection(selection), h.sum}); err != nil {
		return err
	}
	seen := make(map[fsapi.UUID][32]byte)
	for _, index := range r.history.selected {
		e := r.history.entries[index]
		sum, eerr := effectIdentity(e.Record)
		if eerr != nil {
			return eerr
		}
		seen[e.Record.Header.Operation] = sum
	}
	budget := r.config.MaxProgressBytes
	if budget == 0 {
		budget = 256 << 20
	}
	for n := len(r.selection.Effects); n < len(h.selected); n++ {
		e := h.entries[h.selected[n]]
		if err = hit(ctx, r.config, BeforeApply); err != nil {
			return err
		}
		sum, eerr := effectIdentity(e.Record)
		if eerr != nil {
			return eerr
		}
		old, duplicate := seen[e.Record.Header.Operation]
		if duplicate && old != sum {
			return fsapi.Stale
		}
		if !duplicate {
			op, eerr := operation(ctx, r.Store, h, e, r.Slots+1)
			if eerr != nil {
				return eerr
			}
			if op != nil {
				if err = r.Store.Apply(ctx, *op); err != nil {
					return err
				}
				r.Slots++
			}
		}
		seen[e.Record.Header.Operation] = sum
		if err = hit(ctx, r.config, AfterApply); err != nil {
			return err
		}
		image, eerr := r.Store.Export(ctx)
		if eerr != nil {
			return eerr
		}
		if err = hit(ctx, r.config, EffectsDurable); err != nil {
			return err
		}
		encoded, eerr := storage.EncodeImage(image, r.config.Storage.Limits)
		raw := int64(len(encoded)) + progressSize
		if eerr != nil {
			return eerr
		}
		if raw > budget-r.used {
			return fsapi.NoSpace
		}
		written, eerr := publish(ctx, r.progress, r.config, e.Reference, uint64(n), r.Slots, h.prefixes[n], image)
		if eerr != nil {
			return eerr
		}
		r.used += written
		r.Applied = e.Reference
	}
	r.selection, r.history = selection, h
	return nil
}
