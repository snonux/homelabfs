package logicalview

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/snonux/homelabfs/internal/admission"
	"github.com/snonux/homelabfs/internal/fsapi"
	"github.com/snonux/homelabfs/internal/journal/record"
	"github.com/snonux/homelabfs/internal/storage"
)

// New reserves ordinary and isolated recovery arenas before copying any image.
// A failed verifier or incomplete source/baseline leaves the gate unopened.
func New(ctx context.Context, config Config) (*Store, error) {
	if err := configure(&config); err != nil {
		return nil, err
	}
	used, identities, err := baselineBounds(config)
	if err != nil {
		return nil, err
	}
	normal, recovery, err := reserve(config)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			releaseArenas(normal)
			releaseArenas(recovery)
		}
	}()
	authority, err := ownedAuthority(config.Authority)
	if err != nil {
		return nil, err
	}
	config.Authority = authority
	hash, err := record.NormalAuthorityHash(authority)
	if err != nil {
		return nil, err
	}
	config.Sources = cloneSources(config.Sources)
	config.Domains = slices.Clone(config.Domains)
	s := &Store{config: config, hash: hash, arena: normal, recovery: recovery, claims: map[fsapi.UUID]Claim{}, used: used, handles: map[fsapi.OpenHandle]openHandle{}, identities: identities, allocations: map[fsapi.ObjectID]fsapi.UUID{}}
	v := &version{images: map[fsapi.UUID]storage.Snapshot{}}
	for _, baseline := range config.Baseline {
		image, err := storage.Project(ctx, baseline, nil, config.Limits.Storage, config.Limits.MaxSnapshotBytes)
		if err != nil {
			return nil, err
		}
		if _, found := v.images[image.Image.Volume]; found {
			return nil, fsapi.Invalid
		}
		if !s.hasVolume(image.Image.Volume, image.Image.Position) {
			return nil, fsapi.Stale
		}
		v.images[image.Image.Volume] = image
	}
	if len(v.images) != len(authority.Volumes) {
		return nil, fsapi.Invalid
	}
	if err = validateClosure(v.images); err != nil {
		return nil, err
	}
	// The verifier sees independent owned baseline bytes; it cannot mutate the
	// live versions, and the transient arena includes this full copy.
	s.config.Baseline = nil
	if err = s.verifyBaseline(ctx, v); err != nil {
		return nil, err
	}
	s.versions = []*version{v}
	s.selected = v
	s.visible = v
	keep = true
	return s, nil
}

// Begin captures the visible cut. It never reads candidates or backing files.
func (s *Store) Begin(ctx context.Context) (*Observation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.closed {
		return nil, fsapi.Unavailable
	}
	if err := s.verify(ctx, ReadObservation, Stage{}, Evidence{}); err != nil {
		return nil, err
	}
	if s.readers >= s.config.Limits.Readers {
		return nil, fsapi.NoSpace
	}
	s.readers++
	return &Observation{store: s, version: s.visible}, nil
}

// Stage owns a complete replayable candidate at the current committed cut. It
// reserves its immutable claim/allocation identities before returning success.
// A single outstanding slot includes selected-but-unpublished outcomes.
func (s *Store) Stage(ctx context.Context, stage Stage) (record.Hash, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.validateStage(ctx, stage); err != nil {
		return record.Hash{}, err
	}
	stage = cloneStage(stage)
	digest, size, err := digestStage(stage)
	if err != nil {
		return record.Hash{}, err
	}
	if size > s.config.Limits.MaxClaimBytes {
		return record.Hash{}, fsapi.NoSpace
	}
	next, err := s.project(ctx, stage.Operations)
	if err != nil {
		return record.Hash{}, err
	}
	for _, dependency := range stage.Dependencies {
		ref, err := record.NormalReference(dependency.Bytes)
		if err != nil || ref != dependency.Ref {
			return record.Hash{}, fsapi.Invalid
		}
		if !sourceCovers(s.config.Sources, ref) {
			return record.Hash{}, fsapi.Unavailable
		}
	}
	if err = s.verify(ctx, Candidate, stage, Evidence{Digest: digest}); err != nil {
		return record.Hash{}, err
	}
	claim := Claim{Stage: stage, Digest: digest}
	s.identities[stage.Claim] = 2
	s.claims[stage.Claim] = claim
	s.pending = &pending{claim: claim, next: next}
	for _, op := range stage.Operations {
		for _, c := range op.Objects {
			if c.Before == 0 {
				s.used[c.After.Attr.Object] = true
			}
		}
	}
	return digest, nil
}

// Select installs the externally verified selected cut in owned RAM. It still
// exposes no effects. Missing/failed evidence retains the unresolved candidate.
func (s *Store) Select(ctx context.Context, e Evidence) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed || s.pending == nil {
		return fsapi.Stale
	}
	p := s.pending
	if e.Digest != p.claim.Digest || !validCut(e.Selected) || e.Selected.Slot != p.claim.Stage.Slot {
		return fsapi.Stale
	}
	if p.next.cut != nil {
		if *p.next.cut == e.Selected && p.claim.Evidence.NoEffect == e.NoEffect && slices.Equal(p.claim.Evidence.Result, e.Result) {
			return nil
		}
		return fsapi.Stale
	}
	if err := s.validateEvidence(p.claim.Stage, e); err != nil {
		return err
	}
	if err := s.verify(ctx, Selection, p.claim.Stage, e); err != nil {
		return err
	}
	merged := mergeSources(s.config.Sources, e.Sources)
	if err := evidenceSize(Evidence{Sources: merged}, s.config.Limits); err != nil {
		return err
	}
	e = cloneEvidence(e)
	if e.NoEffect {
		p.next = &version{images: s.selected.images}
	}
	p.next.cut = cloneCut(&e.Selected)
	p.claim.Evidence = &e
	s.claims[p.claim.Stage.Claim] = p.claim
	s.selected = p.next
	s.versions = append(s.versions, p.next)
	s.frontiers.Selected = cloneCut(&e.Selected)
	s.config.Sources = merged
	return nil
}

// Publish atomically opens the selected generation only after all previous
// observations close and every inventoried domain acknowledges the exact cut.
// Disk application and owner source persistence are deliberately absent here.
func (s *Store) Publish(ctx context.Context, e Evidence) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed || s.pending == nil || s.pending.next.cut == nil {
		return fsapi.Stale
	}
	p := s.pending
	if e.Digest != p.claim.Digest || e.Selected != *p.next.cut {
		return fsapi.Stale
	}
	if s.readers != 0 {
		return fsapi.Busy
	}
	if len(e.Result) == 0 || !slices.Equal(e.Result, p.claim.Evidence.Result) || !sameDomains(e.Acks, s.config.Domains) {
		return fsapi.Unavailable
	}
	if err := s.verify(ctx, Publication, p.claim.Stage, e); err != nil {
		return err
	}
	s.visible = s.selected
	s.frontiers.Visible = cloneCut(s.selected.cut)
	s.pending = nil
	return nil
}

// Frontiers returns independently owned distinct source and projection cuts.
func (s *Store) Frontiers() Frontiers {
	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneFrontiers(s.frontiers)
}

// ObserveSource records actual received/durable coverage without selecting or
// exposing anything. Complete stream identity is checked against authority.
func (s *Store) ObserveSource(f SourceFrontier) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || !s.validOrigin(f.Stream) || f.Durable > f.Received {
		return fsapi.Invalid
	}
	for i, old := range s.frontiers.Sources {
		if old.Stream == f.Stream {
			if f.Received < old.Received || f.Durable < old.Durable {
				return fsapi.Stale
			}
			s.frontiers.Sources[i] = f
			return nil
		}
	}
	if len(s.frontiers.Sources) >= s.config.Limits.Dependencies {
		return fsapi.NoSpace
	}
	s.frontiers.Sources = append(s.frontiers.Sources, f)
	return nil
}

// MarkBacking records a separately verified exact selected cut. A durable mark
// cannot exceed installed coverage; neither mark changes visible RAM.
func (s *Store) MarkBacking(ctx context.Context, cut record.SelectedRef, durable bool, proof []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || !s.retainedCut(cut) {
		return fsapi.Stale
	}
	old := s.frontiers.Installed
	if durable {
		old = s.frontiers.Durable
		if s.frontiers.Installed == nil || cut.Slot.Serial > s.frontiers.Installed.Slot.Serial {
			return fsapi.Stale
		}
	}
	if old != nil && cut.Slot.Serial < old.Slot.Serial {
		return fsapi.Stale
	}
	phase := BackingInstalled
	if durable {
		phase = BackingDurable
	}
	if err := s.verify(ctx, phase, Stage{}, Evidence{Selected: cut, Proof: proof}); err != nil {
		return err
	}
	if durable {
		s.frontiers.Durable = cloneCut(&cut)
	} else {
		s.frontiers.Installed = cloneCut(&cut)
	}
	return nil
}

// PinVersion retains an internal exact version without permitting old frontend
// reads. Publication may proceed; reclamation waits for Release.
func (s *Store) PinVersion(cut *record.SelectedRef) (*VersionPin, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, fsapi.Unavailable
	}
	for _, v := range s.versions {
		if sameCut(v.cut, cut) {
			if s.pinCount >= s.config.Limits.Identities {
				return nil, fsapi.NoSpace
			}
			s.pinCount++
			v.pins++
			return &VersionPin{store: s, version: v}, nil
		}
	}
	return nil, fsapi.Stale
}

// Retire releases only old unpinned versions and exact claims covered by a
// verified durable replacement. It never treats disk apply as source coverage.
func (s *Store) Retire(ctx context.Context, through record.SelectedRef, claims []fsapi.UUID, proof []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.frontiers.Durable == nil || through.Slot.Authority != s.hash || through.Slot.Serial > s.frontiers.Durable.Slot.Serial || !s.retainedCut(through) {
		return fsapi.Stale
	}
	if len(claims) > s.config.Limits.Claims {
		return fsapi.NoSpace
	}
	seen := map[fsapi.UUID]bool{}
	for _, id := range claims {
		if seen[id] {
			return fsapi.Invalid
		}
		seen[id] = true
		c, ok := s.claims[id]
		if !ok || c.Evidence == nil || c.Evidence.Selected.Slot.Serial > through.Slot.Serial {
			return fsapi.Stale
		}
	}
	if err := s.verify(ctx, Retirement, Stage{}, Evidence{Selected: through, Proof: proof, Claims: slices.Clone(claims)}); err != nil {
		return err
	}
	kept := s.versions[:0]
	for _, v := range s.versions {
		if v == s.selected || v == s.visible || v.pins > 0 || v.cut != nil && v.cut.Slot.Serial > through.Slot.Serial {
			kept = append(kept, v)
		}
	}
	s.versions = kept
	// Allocation identities deliberately survive claim retirement.
	for _, id := range claims {
		delete(s.claims, id)
		s.identities[id] = 3
	}
	return nil
}

// Claims returns an immutable recovery ledger, including uncertain candidates.
func (s *Store) Claims() []Claim {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Claim, 0, len(s.claims))
	for _, c := range s.claims {
		out = append(out, cloneClaim(c))
	}
	return out
}

// Close releases a local arena only after all protocol claims, handles and
// observations are resolved. Uncertain state stays retained for inspection.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	if s.pending != nil || len(s.claims) > 0 || len(s.handles) > 0 || s.readers > 0 {
		return fsapi.Busy
	}
	for _, v := range s.versions {
		if v.pins > 0 {
			return fsapi.Busy
		}
	}
	releaseArenas(s.arena)
	releaseArenas(s.recovery)
	s.closed = true
	s.versions = nil
	s.visible = nil
	s.selected = nil
	s.used = nil
	s.identities = nil
	s.allocations = nil
	s.handles = nil
	s.config.Sources = nil
	s.frontiers = Frontiers{}
	return nil
}

func configure(c *Config) error {
	l := &c.Limits
	if l.MaxSnapshotBytes == 0 {
		l.MaxSnapshotBytes = 128 << 10
	}
	if l.MaxClaimBytes == 0 {
		l.MaxClaimBytes = 128 << 10
	}
	if l.Versions == 0 {
		l.Versions = 4
	}
	if l.Claims == 0 {
		l.Claims = 8
	}
	if l.Handles == 0 {
		l.Handles = 1024
	}
	if l.Readers == 0 {
		l.Readers = 2
	}
	if l.Identities == 0 {
		l.Identities = 512
	}
	if l.Dependencies == 0 {
		l.Dependencies = 256
	}
	if c.Verifier == nil || c.Budget == nil || !c.Generation.Valid() || len(c.Baseline) == 0 || len(c.Baseline) > 64 || len(c.Sources) > l.Dependencies || len(c.Domains) > l.Dependencies {
		return fsapi.Invalid
	}
	if l.MaxSnapshotBytes < 1 || l.MaxSnapshotBytes > 1<<30 || l.MaxClaimBytes < 1 || l.MaxClaimBytes > 8<<20 || l.Versions < 2 || l.Versions > 64 || l.Claims < 1 || l.Claims > 4096 || l.Handles < 1 || l.Handles > 1<<20 || l.Readers < 1 || l.Readers > 1024 || l.Dependencies < 1 || l.Dependencies > 4096 || l.Identities < 1 || l.Identities > 1<<20 {
		return fsapi.Invalid
	}
	if len(c.Authority.Volumes) < 1 || len(c.Authority.Volumes) > 64 || len(c.Authority.Admitted) > 4096 || len(c.Authority.FenceLineage) > 4096 {
		return fsapi.Invalid
	}
	cost := int64(len(c.Authority.Admitted))*16 + int64(len(c.Authority.FenceLineage))*32 + int64(len(c.Authority.Volumes))*128
	for _, v := range c.Authority.Volumes {
		if len(v.Replicas) > 4096 {
			return fsapi.Invalid
		}
		cost += int64(len(v.Replicas)) * 16
	}
	if cost > l.MaxClaimBytes {
		return fsapi.NoSpace
	}
	if err := evidenceSize(Evidence{Sources: c.Sources}, *l); err != nil {
		return err
	}
	if !sameDomains(c.Domains, c.Domains) {
		return fsapi.Invalid
	}
	return nil
}
func baselineBounds(c Config) (map[fsapi.ObjectID]bool, map[fsapi.UUID]uint8, error) {
	used := map[fsapi.ObjectID]bool{}
	identities := map[fsapi.UUID]uint8{}
	var total int64
	for _, baseline := range c.Baseline {
		n, err := snapshotBytes(baseline, c.Limits.Storage, c.Limits.MaxSnapshotBytes-total)
		if err != nil {
			return nil, nil, err
		}
		total += n
		for _, o := range baseline.Image.Objects {
			used[o.Attr.Object] = true
			if len(used) > c.Limits.Identities {
				return nil, nil, fsapi.NoSpace
			}
		}
		for _, d := range baseline.Image.Retired {
			used[d.Object] = true
			if len(used) > c.Limits.Identities {
				return nil, nil, fsapi.NoSpace
			}
		}
		for _, r := range baseline.Image.Receipts {
			identities[r.ID] = 3
			if len(identities) > c.Limits.Identities {
				return nil, nil, fsapi.NoSpace
			}
		}
	}
	return used, identities, nil
}

func reserve(c Config) ([]*admission.Lease, []*admission.Lease, error) {
	l := c.Limits
	count := uint64(l.Versions + 4 + 2*l.Readers)
	snapshot := uint64(l.MaxSnapshotBytes)
	overhead := uint64(l.Identities)*1024 + uint64(l.Handles)*256 + uint64(l.Dependencies)*512 + uint64(l.MaxClaimBytes)*2
	claim := uint64(l.MaxClaimBytes)*uint64(2*l.Claims+8) + overhead
	if snapshot > math.MaxUint64/count || snapshot*count > math.MaxUint64-claim {
		return nil, nil, fsapi.NoSpace
	}
	bytes := snapshot*count + claim
	var normal, recovery []*admission.Lease
	for _, volume := range c.Authority.Volumes {
		request := admission.Request{Peer: c.Authority.Sequencer.Node.String(), Volume: volume.Volume.String(), Closure: c.Authority.Closure.String(), Class: admission.Memory, Cost: admission.Resources{Bytes: bytes, OwnerRAM: bytes, Operations: 1}}
		lease, err := c.Budget.Reserve(request)
		if err != nil {
			releaseArenas(normal)
			releaseArenas(recovery)
			return nil, nil, fmt.Errorf("%w: logical arena: %w", fsapi.NoSpace, err)
		}
		normal = append(normal, lease)
		request.Class = admission.Recovery
		request.Cost.Bytes = 4 * (snapshot*uint64(l.Versions+2) + uint64(l.MaxClaimBytes)*uint64(2*l.Claims) + overhead)
		request.Cost.OwnerRAM = request.Cost.Bytes
		lease, err = c.Budget.Reserve(request)
		if err != nil {
			releaseArenas(normal)
			releaseArenas(recovery)
			return nil, nil, fmt.Errorf("%w: logical recovery reserve: %w", fsapi.NoSpace, err)
		}
		recovery = append(recovery, lease)
	}
	return normal, recovery, nil
}
func releaseArenas(leases []*admission.Lease) {
	for _, lease := range leases {
		_ = lease.Release()
	}
}
