package logicalview

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"slices"

	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/storage"
)

// RecoveryVersion contains complete immutable RAM data, metadata and pins at
// one cut. It is component recovery input, never a distributed checkpoint.
type RecoveryVersion struct {
	Cut    *record.SelectedRef
	Images []storage.Snapshot
	Pins   int
}

// RecoveryIdentity retains admitted and permanently retired operation IDs.
type RecoveryIdentity struct {
	ID    fsapi.UUID
	State uint8
}

// RecoveryAllocation preserves every issued allocation, even failed grants.
type RecoveryAllocation struct {
	Object fsapi.ObjectID
	Claim  fsapi.UUID
}

// RecoveryHandle binds an existing descriptor to its original home and rights.
type RecoveryHandle struct {
	ID     fsapi.OpenHandle
	Object fsapi.ObjectID
	Flags  fsapi.OpenFlags
}

// RecoveryPending retains both unknown and selected/unpublished candidates.
type RecoveryPending struct {
	Claim fsapi.UUID
	Next  RecoveryVersion
}

// RecoveryState is a bounded local transfer representation. Restore requires a
// verifier for actual complete durable sources, issued frontiers, old lifetime
// exclusion and any retained continuity; serialized bytes alone authorize none.
type RecoveryState struct {
	Version           uint16
	Authority         record.Hash
	Generation        fsapi.UUID
	Versions          []RecoveryVersion
	Selected, Visible int
	Pending           *RecoveryPending
	Claims            []Claim
	Identities        []RecoveryIdentity
	Allocations       []RecoveryAllocation
	Used              []fsapi.ObjectID
	Handles           []RecoveryHandle
	NextHandle        fsapi.OpenHandle
	Sources           []record.SourceCoverage
	Frontiers         Frontiers
}

// WriteRecovery writes the complete retained component state while its gates
// are held. The destination belongs to the caller's reserved recovery storage;
// this method supplies neither disk sync nor source-release authority. Errors
// preserve all live state. Old versions, candidates and handles are included.
func (s *Store) WriteRecovery(ctx context.Context, w io.Writer) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed || w == nil {
		return fsapi.Invalid
	}
	state := s.recoveryState()
	writer := &boundedWriter{writer: w, left: s.recoveryLimit()}
	return json.NewEncoder(writer).Encode(state)
}

// Restore validates a bounded serialized state into a newly reserved store.
// Config.Baseline must independently be an admitted baseline. Recovery then
// verifies the complete replacement and opens only its exact prior visible cut.
// No unselected candidate becomes visible and no backing disk is consulted.
func Restore(ctx context.Context, config Config, r io.Reader, proof []byte) (*Store, error) {
	s, err := New(ctx, config)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			releaseArenas(s.arena)
			releaseArenas(s.recovery)
		}
	}()
	if r == nil || int64(len(proof)) > s.config.Limits.MaxClaimBytes {
		return nil, fsapi.Invalid
	}
	raw, err := io.ReadAll(io.LimitReader(r, s.recoveryLimit()+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > s.recoveryLimit() {
		return nil, fsapi.NoSpace
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var state RecoveryState
	if err = dec.Decode(&state); err != nil {
		return nil, err
	}
	canonical := &matchingWriter{expected: raw}
	if err = json.NewEncoder(canonical).Encode(state); err != nil || canonical.position != len(raw) {
		return nil, fsapi.Invalid
	}

	if err = s.restoreState(ctx, state); err != nil {
		return nil, err
	}
	authority, err := ownedAuthority(s.config.Authority)
	if err != nil {
		return nil, err
	}
	// The decoded transfer is separate from the freshly copied live versions.
	if err = s.config.Verifier.Check(ctx, Check{Phase: Recovery, Authority: authority, Recovery: &state, Evidence: Evidence{Proof: slices.Clone(proof)}, Domains: slices.Clone(s.config.Domains)}); err != nil {
		return nil, err
	}
	keep = true
	return s, nil
}

// ReleaseRecoveredPins reconciles explicit internal pins after verified recovery
// handoff. A restarted daemon or a new UUID alone cannot erase those obligations.
func (s *Store) ReleaseRecoveredPins(ctx context.Context, cut *record.SelectedRef, count int, proof []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if count < 1 {
		return fsapi.Invalid
	}
	for _, v := range s.versions {
		if sameCut(v.cut, cut) {
			if count > v.recoveredPins {
				return fsapi.Invalid
			}
			e := Evidence{Proof: proof}
			if cut != nil {
				e.Selected = *cut
			}
			if err := s.verify(ctx, Retirement, Stage{}, e); err != nil {
				return err
			}
			v.pins -= count
			v.recoveredPins -= count
			s.pinCount -= count
			return nil
		}
	}
	return fsapi.Stale
}

type boundedWriter struct {
	writer io.Writer
	left   int64
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.left {
		return 0, fsapi.NoSpace
	}
	n, err := w.writer.Write(p)
	w.left -= int64(n)
	if err == nil && n != len(p) {
		err = io.ErrShortWrite
	}
	return n, err
}

func (s *Store) recoveryState() RecoveryState {
	state := RecoveryState{Version: 1, Authority: s.hash, Generation: s.config.Generation, Sources: s.config.Sources, Frontiers: s.frontiers, NextHandle: s.nextHandle}
	for i, v := range s.versions {
		if v == s.selected {
			state.Selected = i
		}
		if v == s.visible {
			state.Visible = i
		}
		state.Versions = append(state.Versions, recoveryVersion(v))
	}
	if s.pending != nil {
		state.Pending = &RecoveryPending{Claim: s.pending.claim.Stage.Claim, Next: recoveryVersion(s.pending.next)}
	}
	for _, c := range s.claims {
		state.Claims = append(state.Claims, c)
	}
	for id, status := range s.identities {
		state.Identities = append(state.Identities, RecoveryIdentity{ID: id, State: status})
	}
	for object, id := range s.allocations {
		state.Allocations = append(state.Allocations, RecoveryAllocation{Object: object, Claim: id})
	}
	for id := range s.used {
		state.Used = append(state.Used, id)
	}
	for id, h := range s.handles {
		state.Handles = append(state.Handles, RecoveryHandle{ID: id, Object: h.Object, Flags: h.Flags})
	}
	return state
}
func recoveryVersion(v *version) RecoveryVersion {
	state := RecoveryVersion{Cut: v.cut, Pins: v.pins}
	for _, image := range v.images {
		state.Images = append(state.Images, image)
	}
	return state
}
func (s *Store) recoveryLimit() int64 {
	return 2 * (s.config.Limits.MaxSnapshotBytes*int64(s.config.Limits.Versions+2) + s.config.Limits.MaxClaimBytes*int64(2*s.config.Limits.Claims) + int64(s.config.Limits.Identities)*1024 + int64(s.config.Limits.Handles)*256 + int64(s.config.Limits.Dependencies)*512 + 2*s.config.Limits.MaxClaimBytes)
}

func (s *Store) restoreState(ctx context.Context, state RecoveryState) error {
	l := s.config.Limits
	if state.Version != 1 || state.Authority != s.hash || len(state.Versions) < 1 || len(state.Versions) > l.Versions || state.Selected < 0 || state.Selected >= len(state.Versions) || state.Visible < 0 || state.Visible >= len(state.Versions) || len(state.Claims) > l.Claims || len(state.Identities) > l.Identities || len(state.Used) > l.Identities || len(state.Allocations) > l.Identities || len(state.Handles) > l.Handles || len(state.Sources) > l.Dependencies || len(state.Frontiers.Sources) > l.Dependencies {
		return fsapi.Invalid
	}
	versions := make([]*version, 0, len(state.Versions))
	var previous uint64
	totalPins := 0
	for i, input := range state.Versions {
		v, err := s.restoreVersion(ctx, input)
		if err != nil {
			return err
		}
		if v.cut != nil {
			if i > 0 && v.cut.Slot.Serial <= previous {
				return fsapi.Stale
			}
			previous = v.cut.Slot.Serial
		} else if i != 0 {
			return fsapi.Stale
		}
		totalPins += v.pins
		if totalPins > l.Identities {
			return fsapi.NoSpace
		}
		versions = append(versions, v)
	}
	selected, visible := versions[state.Selected], versions[state.Visible]
	if state.Visible > state.Selected || state.Selected != len(versions)-1 || !sameCut(state.Frontiers.Selected, selected.cut) || !sameCut(state.Frontiers.Visible, visible.cut) {
		return fsapi.Stale
	}
	claims := map[fsapi.UUID]Claim{}
	for _, c := range state.Claims {
		if !c.Stage.Claim.Valid() || claims[c.Stage.Claim].Stage.Claim.Valid() {
			return fsapi.Invalid
		}
		digest, n, err := digestStage(c.Stage)
		if err != nil {
			return err
		}
		if digest != c.Digest || n > l.MaxClaimBytes {
			return fmt.Errorf("claim digest/size: %w", fsapi.Stale)
		}
		if c.Evidence != nil {
			if c.Evidence.Digest != digest || c.Evidence.Selected.Slot != c.Stage.Slot || !validCut(c.Evidence.Selected) {
				return fsapi.Stale
			}
			if err = s.validateEvidence(c.Stage, *c.Evidence); err != nil {
				return err
			}
		}
		claims[c.Stage.Claim] = cloneClaim(c)
	}
	var pendingCandidate *pending
	if state.Pending != nil {
		c, ok := claims[state.Pending.Claim]
		if !ok {
			return fsapi.Stale
		}
		next, err := s.restoreVersion(ctx, state.Pending.Next)
		if err != nil {
			return err
		}
		if c.Evidence != nil {
			if !sameCut(next.cut, &c.Evidence.Selected) || !sameCut(next.cut, selected.cut) {
				return fsapi.Stale
			}
			if next.pins != selected.pins {
				return fmt.Errorf("pending selected pins: %w", fsapi.Stale)
			}
			if err := equalImages(next.images, selected.images, s.config.Limits.Storage); err != nil {
				return fmt.Errorf("pending selected snapshot: %w", err)
			}
			next = selected
		} else if next.cut != nil || next.pins != 0 || selected != visible {
			return fsapi.Stale
		}
		// Replay the immutable candidate against its visible predecessor. A
		// self-consistent but unrelated afterimage cannot replace admitted redo.
		base := visible
		if !sameCut(c.Stage.Previous, base.cut) {
			return fsapi.Stale
		}
		saved := s.selected
		s.selected = base
		expected, err := s.project(ctx, c.Stage.Operations)
		s.selected = saved
		if err != nil {
			return err
		}
		if c.Evidence != nil && c.Evidence.NoEffect {
			expected = base
		}
		if err := equalImages(expected.images, next.images, s.config.Limits.Storage); err != nil {
			return fmt.Errorf("pending redo projection: %w", err)
		}
		pendingCandidate = &pending{claim: c, next: next}
	} else if selected != visible {
		return fsapi.Stale
	}
	identities := map[fsapi.UUID]uint8{}
	for _, id := range state.Identities {
		if !id.ID.Valid() || id.State < 1 || id.State > 3 || identities[id.ID] != 0 {
			return fsapi.Invalid
		}
		identities[id.ID] = id.State
	}
	for id := range claims {
		if identities[id] != 2 {
			return fsapi.Stale
		}
	}
	used := map[fsapi.ObjectID]bool{}
	for _, id := range state.Used {
		if !id.Valid() || used[id] {
			return fsapi.Invalid
		}
		used[id] = true
	}
	allocations := map[fsapi.ObjectID]fsapi.UUID{}
	for _, a := range state.Allocations {
		if !used[a.Object] || identities[a.Claim] == 0 || allocations[a.Object].Valid() {
			return fsapi.Stale
		}
		allocations[a.Object] = a.Claim
	}
	for _, v := range versions {
		for _, image := range v.images {
			for _, o := range image.Image.Objects {
				if !used[o.Attr.Object] {
					return fsapi.Stale
				}
			}
			for _, d := range image.Image.Retired {
				if !used[d.Object] {
					return fsapi.Stale
				}
			}
		}
	}
	handles := map[fsapi.OpenHandle]openHandle{}
	if len(state.Handles) > 0 && state.Generation != s.config.Generation {
		return fsapi.Stale
	}
	for _, h := range state.Handles {
		if h.ID == 0 || h.ID > state.NextHandle || handles[h.ID].Object.Valid() || h.Flags.Validate() != nil {
			return fsapi.Invalid
		}
		found := false
		for _, o := range visible.images[h.Object.Home].Image.Objects {
			found = found || o.Attr.Object == h.Object
		}
		if !found {
			return fsapi.Stale
		}
		handles[h.ID] = openHandle{Object: h.Object, Flags: h.Flags}
	}
	if err := s.validateRestoredFrontiers(state.Frontiers, selected.cut); err != nil {
		return err
	}
	s.pinCount = totalPins
	s.versions = versions
	s.selected = selected
	s.visible = visible
	s.pending = pendingCandidate
	s.claims = claims
	s.identities = identities
	s.used = used
	s.allocations = allocations
	s.handles = handles
	s.nextHandle = state.NextHandle
	s.frontiers = cloneFrontiers(state.Frontiers)
	if err := evidenceSize(Evidence{Sources: state.Sources}, l); err != nil {
		return err
	}
	s.config.Sources = cloneSources(state.Sources)
	return nil
}
func (s *Store) restoreVersion(ctx context.Context, input RecoveryVersion) (*version, error) {
	if input.Pins < 0 || input.Pins > s.config.Limits.Identities || len(input.Images) != len(s.config.Authority.Volumes) || input.Cut != nil && (!validCut(*input.Cut) || input.Cut.Slot.Authority != s.hash) {
		return nil, fsapi.Invalid
	}
	v := &version{cut: cloneCut(input.Cut), pins: input.Pins, recoveredPins: input.Pins, images: map[fsapi.UUID]storage.Snapshot{}}
	var total int64
	for _, image := range input.Images {
		if !s.hasVolume(image.Image.Volume, image.Image.Position) {
			return nil, fsapi.Stale
		}
		if _, ok := v.images[image.Image.Volume]; ok {
			return nil, fsapi.Invalid
		}
		n, err := snapshotBytes(image, s.config.Limits.Storage, s.config.Limits.MaxSnapshotBytes-total)
		if err != nil {
			return nil, err
		}
		if n > s.config.Limits.MaxSnapshotBytes-total {
			return nil, fsapi.NoSpace
		}
		total += n
		owned, err := storage.Project(ctx, image, nil, s.config.Limits.Storage, s.config.Limits.MaxSnapshotBytes)
		if err != nil {
			return nil, err
		}
		v.images[image.Image.Volume] = owned
	}
	if err := validateClosure(v.images); err != nil {
		return nil, err
	}
	return v, nil
}
func (s *Store) validateRestoredFrontiers(f Frontiers, selected *record.SelectedRef) error {
	for _, cut := range []*record.SelectedRef{f.Installed, f.Durable} {
		if cut != nil && (selected == nil || !validCut(*cut) || cut.Slot.Authority != s.hash || cut.Slot.Serial > selected.Slot.Serial) {
			return fsapi.Stale
		}
	}
	if f.Durable != nil && (f.Installed == nil || f.Durable.Slot.Serial > f.Installed.Slot.Serial) {
		return fsapi.Stale
	}
	for _, source := range f.Sources {
		if !s.validOrigin(source.Stream) || source.Durable > source.Received {
			return fsapi.Invalid
		}
	}
	return nil
}

var _ io.Writer = (*boundedWriter)(nil)

type matchingWriter struct {
	expected []byte
	position int
}

func (w *matchingWriter) Write(p []byte) (int, error) {
	if len(p) > len(w.expected)-w.position || !bytes.Equal(p, w.expected[w.position:w.position+len(p)]) {
		return 0, fsapi.Invalid
	}
	w.position += len(p)
	return len(p), nil
}

// equalImages compares exact canonical logical catalog and payload bytes. The
// catalog codec deliberately normalizes empty Go slices; pointer/nil allocation
// details are not part of a filesystem version or its recovery identity.
func equalImages(a, b map[fsapi.UUID]storage.Snapshot, limits storage.Limits) error {
	if len(a) != len(b) {
		return fsapi.Stale
	}
	for volume, left := range a {
		right, ok := b[volume]
		if !ok {
			return fsapi.Stale
		}
		lraw, err := storage.EncodeImage(left.Image, limits)
		if err != nil {
			return err
		}
		rraw, err := storage.EncodeImage(right.Image, limits)
		if err != nil {
			return err
		}
		if !bytes.Equal(lraw, rraw) || len(left.Data) != len(right.Data) {
			return fsapi.Stale
		}
		for i, data := range left.Data {
			if !bytes.Equal(data, right.Data[i]) {
				return fsapi.Stale
			}
		}
	}
	return nil
}
